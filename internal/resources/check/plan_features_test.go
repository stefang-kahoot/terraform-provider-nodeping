package check

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Each setting that uses a feature some plans lack fails, at its own
// attribute, unless the declared plan has the feature. The schema defaults,
// interval 15 and autodiag false, pass on every plan, and so does a value not
// known yet.
func TestRefuseOffPlan(t *testing.T) {
	t.Parallel()

	premiereOnly := map[string]bool{"": true, "personal": true, "professional": true}
	subMinute := map[string]bool{"": true, "personal": true}
	noPlan := map[string]bool{"": true}
	none := map[string]bool{}

	tests := []struct {
		name    string
		planned map[string]attr.Value
		// at is the attribute refused, refused the plans that refuse it.
		at      string
		refused map[string]bool
	}{
		{"the schema defaults", nil, "", none},
		{"homeloc", map[string]attr.Value{"homeloc": types.StringValue("nam")}, "homeloc", premiereOnly},
		{"homeloc roaming", map[string]attr.Value{"homeloc": types.StringValue("roam")}, "homeloc", premiereOnly},
		{"homeloc empty", map[string]attr.Value{"homeloc": types.StringValue("")}, "homeloc", premiereOnly},
		{"homeloc not known yet", map[string]attr.Value{"homeloc": types.StringUnknown()}, "", none},
		{"verifyvolume", map[string]attr.Value{"type": types.StringValue("AUDIO"), "verifyvolume": types.BoolValue(true)}, "verifyvolume", premiereOnly},
		{"verifyvolume false", map[string]attr.Value{"type": types.StringValue("AUDIO"), "verifyvolume": types.BoolValue(false)}, "verifyvolume", premiereOnly},
		{"volumemin", map[string]attr.Value{"type": types.StringValue("AUDIO"), "volumemin": types.Int64Value(-40)}, "volumemin", premiereOnly},
		{"volumemin 0", map[string]attr.Value{"type": types.StringValue("AUDIO"), "volumemin": types.Int64Value(0)}, "volumemin", premiereOnly},
		{"interval 0.25", map[string]attr.Value{"interval": types.Float64Value(0.25)}, "interval", subMinute},
		{"interval 0.5", map[string]attr.Value{"interval": types.Float64Value(0.5)}, "interval", subMinute},
		{"interval 1", map[string]attr.Value{"interval": types.Float64Value(1)}, "", none},
		{"interval not known yet", map[string]attr.Value{"interval": types.Float64Unknown()}, "", none},
		{"autodiag true", map[string]attr.Value{"autodiag": types.BoolValue(true)}, "autodiag", noPlan},
		{"autodiag not known yet", map[string]attr.Value{"autodiag": types.BoolUnknown()}, "", none},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			values := map[string]attr.Value{
				"type":     types.StringValue("HTTP"),
				"target":   types.StringValue("https://example.com"),
				"interval": types.Float64Value(15),
				"autodiag": types.BoolValue(false),
			}
			for name, v := range tt.planned {
				values[name] = v
			}
			plan := tfsdk.Plan{Schema: CheckSchema(), Raw: checkObject(t, values)}

			for _, accountPlan := range []string{"", "personal", "professional", "premiere"} {
				var diags diag.Diagnostics
				refuseOffPlan(ctx, plan, accountPlan, &diags)
				if !tt.refused[accountPlan] {
					if diags.HasError() {
						t.Errorf("plan %q: refused with %v", accountPlan, diags)
					}
					continue
				}
				if len(diags) != 1 || !diags.HasError() {
					t.Errorf("plan %q: got %v, want one error", accountPlan, diags)
					continue
				}
				if got := diags[0].(diag.DiagnosticWithPath).Path().String(); got != tt.at {
					t.Errorf("plan %q: error at %s, want %s", accountPlan, got, tt.at)
				}
			}
		})
	}
}

// Every use is reported, not only the first.
func TestRefuseOffPlanReportsEveryUse(t *testing.T) {
	t.Parallel()

	plan := tfsdk.Plan{Schema: CheckSchema(), Raw: checkObject(t, map[string]attr.Value{
		"type":         types.StringValue("AUDIO"),
		"interval":     types.Float64Value(0.5),
		"autodiag":     types.BoolValue(true),
		"homeloc":      types.StringValue("roam"),
		"verifyvolume": types.BoolValue(true),
		"volumemin":    types.Int64Value(-40),
	})}

	var diags diag.Diagnostics
	refuseOffPlan(context.Background(), plan, "", &diags)
	got := map[string]bool{}
	for _, d := range diags {
		got[d.(diag.DiagnosticWithPath).Path().String()] = true
	}
	for _, name := range []string{"homeloc", "verifyvolume", "volumemin", "interval", "autodiag"} {
		if !got[name] {
			t.Errorf("%s not refused; got %v", name, diags)
		}
	}
	if len(diags) != 5 {
		t.Errorf("got %d errors, want 5: %v", len(diags), diags)
	}
}
