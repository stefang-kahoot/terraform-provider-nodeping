package check

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/datasources/checkattr"
)

// replaceOnRemovedFields replaces a check whose plan removes a field, or a
// field's min, max or match.
//
// NodePing cannot remove either from an existing check (finding 31, probed
// 2026-10-08): it merges fields per key and each field per property, and
// every shape tried for the whole map or a single key -- "", {}, [], null,
// false, 0, "None", "delete", a type change and back -- kept it; renaming a
// key kept both. Sent as null inside a field, min is stored as 0, and a null
// name made NodePing drop the connection. An update would leave the field in
// place and fail the apply with "inconsistent result", every time.
//
// Replacing the check is the only way to get rid of one, and loses the
// check's ID and history, so the plan says so in a warning. Not an error:
// Terraform plans the update before it considers -replace, so an error would
// block `terraform apply -replace` as well.
func replaceOnRemovedFields() planmodifier.Map {
	return mapplanmodifier.RequiresReplaceIf(
		requiresReplaceForRemovedFields,
		"Removing a field, or a field's min, max or match, replaces the check: NodePing cannot remove them from an existing one.",
		"Removing a field, or a field's `min`, `max` or `match`, replaces the check: NodePing cannot remove them from an existing one.",
	)
}

func requiresReplaceForRemovedFields(ctx context.Context, req planmodifier.MapRequest, resp *mapplanmodifier.RequiresReplaceIfFuncResponse) {
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() || req.PlanValue.IsUnknown() {
		return
	}

	var prior, planned map[string]checkattr.FieldModel
	resp.Diagnostics.Append(req.StateValue.ElementsAs(ctx, &prior, false)...)
	if !req.PlanValue.IsNull() {
		resp.Diagnostics.Append(req.PlanValue.ElementsAs(ctx, &planned, false)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	gone := removedFields(prior, planned)
	if len(gone) == 0 {
		return
	}
	resp.RequiresReplace = true

	resp.Diagnostics.AddAttributeWarning(req.Path,
		"Removing fields replaces the check",
		fmt.Sprintf("The configuration of check %s no longer has %s. "+
			"NodePing cannot remove a field, or a field's min, max or match, from an existing check: an update keeps them. "+
			"Terraform will replace the check instead: delete it and create a new one with a new ID, "+
			"which starts without the old check's history and breaks anything that refers to the old ID, such as another check's dep. "+
			"To keep the check, put them back in the configuration.",
			checkName(ctx, req), strings.Join(gone, ", ")))
}

// removedFields lists, sorted, what the prior state's fields have and the
// planned ones do not: whole fields, and the min, max and match of a field
// kept. An unknown planned value is not removed.
func removedFields(prior, planned map[string]checkattr.FieldModel) []string {
	var gone []string
	for key, before := range prior {
		after, ok := planned[key]
		if !ok {
			gone = append(gone, fmt.Sprintf("field %q", key))
			continue
		}
		for _, p := range []struct {
			name          string
			before, after interface {
				IsNull() bool
				IsUnknown() bool
			}
		}{
			{"min", before.Min, after.Min},
			{"max", before.Max, after.Max},
			{"match", before.Match, after.Match},
		} {
			if !p.before.IsNull() && p.after.IsNull() {
				gone = append(gone, fmt.Sprintf("the %s of field %q", p.name, key))
			}
		}
	}
	sort.Strings(gone)
	return gone
}

// checkName names a check for a diagnostic by its label and ID, as far as the
// prior state has them.
func checkName(ctx context.Context, req planmodifier.MapRequest) string {
	var label, id types.String
	req.State.GetAttribute(ctx, path.Root("label"), &label)
	req.State.GetAttribute(ctx, path.Root("id"), &id)

	switch {
	case label.ValueString() != "" && id.ValueString() != "":
		return fmt.Sprintf("%q (%s)", label.ValueString(), id.ValueString())
	case id.ValueString() != "":
		return id.ValueString()
	default:
		return "this check"
	}
}
