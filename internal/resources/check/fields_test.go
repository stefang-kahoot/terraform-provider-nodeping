package check

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/datasources/checkattr"
)

func fieldsValue(t *testing.T, fields map[string]checkattr.FieldModel) types.Map {
	t.Helper()
	if fields == nil {
		return types.MapNull(checkattr.FieldType)
	}
	m, diags := types.MapValueFrom(context.Background(), checkattr.FieldType, fields)
	if diags.HasError() {
		t.Fatalf("fields: %v", diags)
	}
	return m
}

func field(name string, min, max *float64, match string) checkattr.FieldModel {
	f := checkattr.FieldModel{
		Name:  types.StringValue(name),
		Min:   types.Float64Null(),
		Max:   types.Float64Null(),
		Match: types.StringNull(),
	}
	if min != nil {
		f.Min = types.Float64Value(*min)
	}
	if max != nil {
		f.Max = types.Float64Value(*max)
	}
	if match != "" {
		f.Match = types.StringValue(match)
	}
	return f
}

func num(f float64) *float64 { return &f }

// priorState is a check's prior state holding a label and an ID, for the
// warning to name.
func priorState(t *testing.T) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	s := CheckSchema()
	state := tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	for name, value := range map[string]attr.Value{
		"label": types.StringValue("variables.json"),
		"id":    types.StringValue("CHECK-1"),
	} {
		if diags := state.SetAttribute(ctx, path.Root(name), value); diags.HasError() {
			t.Fatalf("state: %v", diags)
		}
	}
	return state
}

func TestRemovingFieldsRequiresReplacement(t *testing.T) {
	t.Parallel()

	a := field("status", num(200), num(200), "")
	b := field("load.avg", nil, num(2.5), "")

	tests := []struct {
		name          string
		before, after map[string]checkattr.FieldModel
		// gone is what the warning names, or empty for no replacement.
		gone string
	}{
		{"a field removed", map[string]checkattr.FieldModel{"A": a, "B": b}, map[string]checkattr.FieldModel{"A": a}, `field "B"`},
		{"every field removed", map[string]checkattr.FieldModel{"A": a, "B": b}, nil, `field "A", field "B"`},
		{"a field renamed", map[string]checkattr.FieldModel{"A": a}, map[string]checkattr.FieldModel{"A2": a}, `field "A"`},
		{"a min removed", map[string]checkattr.FieldModel{"A": a}, map[string]checkattr.FieldModel{"A": field("status", nil, num(200), "")}, `the min of field "A"`},
		{"a max removed", map[string]checkattr.FieldModel{"A": a}, map[string]checkattr.FieldModel{"A": field("status", num(200), nil, "")}, `the max of field "A"`},
		{"a match removed", map[string]checkattr.FieldModel{"A": field("n", nil, nil, "1")}, map[string]checkattr.FieldModel{"A": field("n", nil, nil, "")}, `the match of field "A"`},
		{"a value changed", map[string]checkattr.FieldModel{"A": a}, map[string]checkattr.FieldModel{"A": field("status", num(200), num(299), "")}, ``},
		{"a field added", map[string]checkattr.FieldModel{"A": a}, map[string]checkattr.FieldModel{"A": a, "B": b}, ``},
		{"a min added", map[string]checkattr.FieldModel{"B": b}, map[string]checkattr.FieldModel{"B": field("load.avg", num(0), num(2.5), "")}, ``},
		{"a field's name changed", map[string]checkattr.FieldModel{"A": a}, map[string]checkattr.FieldModel{"A": field("code", num(200), num(200), "")}, ``},
		{"no fields before", nil, map[string]checkattr.FieldModel{"A": a}, ``},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := planmodifier.MapRequest{
				Path:       path.Root("fields"),
				State:      priorState(t),
				StateValue: fieldsValue(t, tt.before),
				PlanValue:  fieldsValue(t, tt.after),
			}
			resp := &mapplanmodifier.RequiresReplaceIfFuncResponse{}
			requiresReplaceForRemovedFields(context.Background(), req, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("raised %v", resp.Diagnostics.Errors())
			}
			if want := tt.gone != ""; resp.RequiresReplace != want {
				t.Errorf("RequiresReplace = %v, want %v", resp.RequiresReplace, want)
			}

			warnings := resp.Diagnostics.Warnings()
			if tt.gone == "" {
				if len(warnings) > 0 {
					t.Errorf("warned %v without replacing", warnings)
				}
				return
			}
			if len(warnings) != 1 {
				t.Fatalf("got %d warnings, want 1", len(warnings))
			}
			w := warnings[0]
			if w.Severity() != diag.SeverityWarning || w.Summary() != "Removing fields replaces the check" {
				t.Errorf("warning %q", w.Summary())
			}
			for _, part := range []string{`check "variables.json" (CHECK-1)`, "no longer has " + tt.gone + ".", "NodePing cannot remove", "replace the check"} {
				if !strings.Contains(w.Detail(), part) {
					t.Errorf("warning detail lacks %q:\n%s", part, w.Detail())
				}
			}
		})
	}
}

// A plan whose fields are not known yet cannot tell what it removes, and
// does not replace the check on suspicion.
func TestUnknownFieldsDoNotRequireReplacement(t *testing.T) {
	t.Parallel()

	a := field("status", num(200), num(200), "")
	req := planmodifier.MapRequest{
		Path:       path.Root("fields"),
		State:      priorState(t),
		StateValue: fieldsValue(t, map[string]checkattr.FieldModel{"A": a}),
		PlanValue:  types.MapUnknown(checkattr.FieldType),
	}
	resp := &mapplanmodifier.RequiresReplaceIfFuncResponse{}
	requiresReplaceForRemovedFields(context.Background(), req, resp)
	if resp.RequiresReplace || len(resp.Diagnostics) > 0 {
		t.Errorf("RequiresReplace = %v, diagnostics %v", resp.RequiresReplace, resp.Diagnostics)
	}

	unknownMin := a
	unknownMin.Min = types.Float64Unknown()
	req.PlanValue = fieldsValue(t, map[string]checkattr.FieldModel{"A": unknownMin})
	resp = &mapplanmodifier.RequiresReplaceIfFuncResponse{}
	requiresReplaceForRemovedFields(context.Background(), req, resp)
	if resp.RequiresReplace {
		t.Error("an unknown min required replacement")
	}
}
