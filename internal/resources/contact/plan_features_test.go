package contact

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// contactPlan returns a contact's plan with the given address blocks.
func contactPlan(t *testing.T, blocks []AddressModel) tfsdk.Plan {
	t.Helper()
	ctx := context.Background()
	s := ContactSchema()
	plan := tfsdk.Plan{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	model := ContactResourceModel{
		ID:         types.StringUnknown(),
		CustomerID: types.StringUnknown(),
		Name:       types.StringValue("acc"),
		CustRole:   types.StringValue("notify"),
		Addresses:  blocks,
	}
	if diags := plan.Set(ctx, &model); diags.HasError() {
		t.Fatalf("plan: %v", diags)
	}
	return plan
}

// refusedPaths returns where refuseOffPlan put its errors.
func refusedPaths(t *testing.T, plan tfsdk.Plan, accountPlan string) []string {
	t.Helper()
	var diags diag.Diagnostics
	refuseOffPlan(context.Background(), plan, accountPlan, &diags)
	var paths []string
	for _, d := range diags {
		withPath, ok := d.(diag.DiagnosticWithPath)
		if !ok || d.Severity() != diag.SeverityError {
			t.Fatalf("unexpected diagnostic %v", d)
		}
		paths = append(paths, withPath.Path().String())
	}
	return paths
}

// A voice address needs Premiere, and suppress_diag = true any of the three
// plans; each fails at its own block's attribute. false and the other
// address types pass on any plan, and so does a value not known yet.
func TestRefuseOffPlanAddresses(t *testing.T) {
	t.Parallel()

	diagOff := addressBlock("email", "a@example.com")
	diagOn := addressBlock("email", "b@example.com")
	diagOn.SuppressDiag = types.BoolValue(true)
	voice := addressBlock("voice", "+15550000001")
	diagUnknown := addressBlock("webhook", "https://hooks.example.com/x")
	diagUnknown.SuppressDiag = types.BoolUnknown()
	typeUnknown := addressBlock("sms", "+15550000002")
	typeUnknown.Type = types.StringUnknown()

	var others []AddressModel
	for _, typ := range []string{"email", "sms", "webhook", "slack", "hipchat", "pushover", "pagerduty"} {
		others = append(others, addressBlock(typ, typ+"-address"))
	}

	tests := []struct {
		name   string
		blocks []AddressModel
		want   map[string][]string
	}{
		{
			name:   "every other address type, flags off",
			blocks: append([]AddressModel{diagOff}, others...),
			want:   map[string][]string{},
		},
		{
			name:   "suppress_diag on and a voice address",
			blocks: []AddressModel{diagOff, diagOn, voice},
			want: map[string][]string{
				"":             {"address[1].suppress_diag", "address[2].type"},
				"personal":     {"address[2].type"},
				"professional": {"address[2].type"},
			},
		},
		{
			name:   "values not known yet",
			blocks: []AddressModel{diagUnknown, typeUnknown},
			want:   map[string][]string{},
		},
		{
			name: "no address",
			want: map[string][]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := contactPlan(t, tt.blocks)
			for _, accountPlan := range []string{"", "personal", "professional", "premiere"} {
				got := refusedPaths(t, plan, accountPlan)
				want := tt.want[accountPlan]
				if len(got) != len(want) {
					t.Errorf("plan %q: refused %v, want %v", accountPlan, got, want)
					continue
				}
				for i := range got {
					if got[i] != want[i] {
						t.Errorf("plan %q: refused %v, want %v", accountPlan, got, want)
						break
					}
				}
			}
		})
	}
}

// A dynamic block over a collection not known yet has no block to check.
func TestRefuseOffPlanUnknownAddresses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	plan := contactPlan(t, nil)
	addressType := ContactSchema().Blocks["address"].Type().(types.ListType).ElemType
	if diags := plan.SetAttribute(ctx, path.Root("address"), types.ListUnknown(addressType)); diags.HasError() {
		t.Fatalf("plan: %v", diags)
	}
	if got := refusedPaths(t, plan, ""); len(got) != 0 {
		t.Errorf("refused %v, want nothing", got)
	}
}

// An apply keeps a planned suppress_diag = true that NodePing's answer drops,
// at the block's position. Everything else is NodePing's.
func TestKeepPlannedSuppressDiag(t *testing.T) {
	t.Parallel()

	on := func(a AddressModel) AddressModel {
		a.SuppressDiag = types.BoolValue(true)
		return a
	}
	planned := []AddressModel{
		on(addressBlock("email", "a@example.com")),
		addressBlock("email", "b@example.com"),
		on(addressBlock("webhook", "https://hooks.example.com/x")),
	}
	applied := []AddressModel{
		addressBlock("email", "a@example.com"),
		on(addressBlock("email", "b@example.com")),
		on(addressBlock("webhook", "https://hooks.example.com/x")),
		addressBlock("sms", "+15550000003"),
	}

	keepPlannedSuppressDiag(applied, planned)

	for i, want := range []bool{true, true, true, false} {
		if got := applied[i].SuppressDiag; got.IsNull() || got.ValueBool() != want {
			t.Errorf("address[%d].suppress_diag = %s, want %v", i, got, want)
		}
	}
}
