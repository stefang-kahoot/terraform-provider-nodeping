package check

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
)

// NodePing sometimes answers an update of a check with the check as it was
// before the update: the old modified and the old values, although it applied
// the update. A read seconds later shows it (finding 41: 1 in 33 full updates
// more than 4 s after the create; 5 in ~180 sent 2 s apart). Mapped onto the
// plan, such an answer failed the apply with "Provider produced inconsistent
// result after apply".
//
// Every fresh answer seen had a modified later than the check's before the
// update. So an answer whose modified is not later is suspect, and if it does
// not already give the planned values, Update reads the check again until it
// shows the update; see readUpdateBack. One that gives them is used as it is,
// since NodePing may not move modified for an update that changes nothing it
// stores. An answer with a later modified that still differs from the plan is
// another matter (a value NodePing ignored, say) and fails as before.

// answerMayBeStale reports whether NodePing's answer to an update may be the
// check as it was before the update: its modified is no later than before,
// the check's modified before the update. Without a before there is nothing
// to tell by, and the answer is taken as it is.
func answerMayBeStale(before types.Int64, answer *client.Check) bool {
	if before.IsNull() || before.IsUnknown() || before.ValueInt64() <= 0 {
		return false
	}
	return answer.Modified <= before.ValueInt64()
}

// givesThePlan reports whether model, as Update would report it, is what
// Terraform requires of an apply: the planned value wherever the plan has a
// known one.
func givesThePlan(ctx context.Context, plan tfsdk.Plan, model CheckResourceModel) bool {
	applied := tfsdk.State{Schema: plan.Schema}
	if diags := applied.Set(ctx, &model); diags.HasError() {
		return false
	}
	return agreesWithPlan(plan.Raw, applied.Raw)
}

// agreesWithPlan reports whether applied equals plan once each value the plan
// leaves unknown is taken from applied.
func agreesWithPlan(plan, applied tftypes.Value) bool {
	filled, err := tftypes.Transform(plan, func(p *tftypes.AttributePath, v tftypes.Value) (tftypes.Value, error) {
		if v.IsKnown() {
			return v, nil
		}
		if got, ok := valueAt(applied, p); ok {
			return got, nil
		}
		return v, nil
	})
	return err == nil && filled.Equal(applied)
}

// readUpdateBack reads the check id again, after NodePing answered its update
// with the check as it was before, until an answer shows the update -- its
// modified is later than before, or it gives the planned values -- and
// returns that answer mapped onto the plan, as Update reports it.
//
// If none shows it in time, it fails naming the check. The state then stays
// as it was before the update, which is what the framework keeps for a
// failed update, so the next plan reads the check afresh.
func (r *CheckResource) readUpdateBack(ctx context.Context, planned tfsdk.Plan, plan CheckResourceModel, id, label string, before int64, ignoreMute bool, diags *diag.Diagnostics) CheckResourceModel {
	var applied CheckResourceModel
	var mapDiags diag.Diagnostics
	_, err := r.client.ReadCheckBack(ctx, id, func(check *client.Check) bool {
		mapDiags = nil
		applied = r.updatedModel(ctx, check, plan, ignoreMute, &mapDiags)
		if mapDiags.HasError() {
			// Taken, so that the mapping's error is reported.
			return true
		}
		return check.Modified > before || givesThePlan(ctx, planned, applied)
	})
	diags.Append(mapDiags...)
	if err == nil {
		return applied
	}

	name := id
	if label != "" {
		name = fmt.Sprintf("%s (%q)", id, label)
	}
	if notShown, ok := errors.AsType[*client.UpdateNotShownError](err); ok {
		diags.AddError(
			"NodePing Has Not Shown the Update Yet",
			fmt.Sprintf("NodePing answered the update of check %s with the check as it was before "+
				"the update, and %d reads over %s since have shown the same. The update was most "+
				"likely applied regardless: NodePing has been seen to show an update a few seconds "+
				"after answering it.\n\n"+
				"Run terraform plan again. It either shows no changes for this check, or plans the "+
				"same update again, which is safe to apply.",
				name, notShown.Reads, notShown.Waited),
		)
		return applied
	}
	diags.AddError(
		"Error Reading Check After Update",
		fmt.Sprintf("NodePing answered the update of check %s with the check as it was before "+
			"the update, and reading it again failed: %s\n\n"+
			"The update was most likely applied regardless. Run terraform plan again.",
			name, err),
	)
	return applied
}
