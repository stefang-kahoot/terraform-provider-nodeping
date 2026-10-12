package contact

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/accountplan"
)

// refuseOffPlan fails the plan for each address block that uses a NodePing
// feature the provider's declared plan, accountPlan, lacks (see the
// accountplan package):
//
//   - type = "voice", which NodePing refuses with HTTP 400 "Illegal address
//     type." on the probed account, on create;
//   - suppress_diag = true, which NodePing drops there on every address type,
//     on create and on update (probed 2026-10-12). false is what NodePing
//     holds when it is not in use.
//
// A block, or a value, not known yet is let through: Terraform plans the
// contact again during the apply, with it known, and this fails the apply
// there, before anything is sent.
func refuseOffPlan(ctx context.Context, plan tfsdk.Plan, accountPlan string, diags *diag.Diagnostics) {
	addressPath := path.Root("address")

	var list types.List
	diags.Append(plan.GetAttribute(ctx, addressPath, &list)...)
	if diags.HasError() || list.IsNull() || list.IsUnknown() {
		return
	}
	var blocks []AddressModel
	diags.Append(list.ElementsAs(ctx, &blocks, false)...)
	if diags.HasError() {
		return
	}

	var uses []accountplan.Use
	for i, block := range blocks {
		at := addressPath.AtListIndex(i)
		if isKnown(block.Type) && block.Type.ValueString() == "voice" {
			uses = append(uses, accountplan.Use{
				Feature:   accountplan.Voice,
				Path:      at.AtName("type"),
				Setting:   `type = "voice"`,
				Otherwise: "Otherwise use another address type.",
			})
		}
		if !block.SuppressDiag.IsUnknown() && block.SuppressDiag.ValueBool() {
			uses = append(uses, accountplan.Use{
				Feature:   accountplan.AutomatedDiagnostics,
				Path:      at.AtName("suppress_diag"),
				Setting:   "suppress_diag = true",
				Otherwise: "Otherwise set suppress_diag to false, or leave it out.",
			})
		}
	}

	accountplan.Refuse(accountPlan, uses, diags)
}

// keepPlannedSuppressDiag gives every applied address planned with
// suppress_diag = true that value where NodePing's answer has it false.
//
// An account whose NodePing plan lacks Automated Diagnostics drops
// suppressdiag, on every address type, and the address then reads false
// (probed 2026-10-12, on a grandfathered Business plan). Whether an account
// with the feature stores it is untested. Either way, the apply keeps the
// planned value, so that it matches its plan instead of failing with
// "inconsistent result after apply"; the next refresh reads NodePing as it
// is. refuseOffPlan has already refused it on a plan without the feature.
//
// Addresses are matched by position: Terraform compares the applied blocks
// with the planned ones position by position, and mapAddressesToModel puts
// each address at its block's.
func keepPlannedSuppressDiag(applied, planned []AddressModel) {
	for i := range min(len(applied), len(planned)) {
		if planned[i].SuppressDiag.ValueBool() && !applied[i].SuppressDiag.ValueBool() {
			applied[i].SuppressDiag = planned[i].SuppressDiag
		}
	}
}
