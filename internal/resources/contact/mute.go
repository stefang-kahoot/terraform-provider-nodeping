package contact

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// The provider's ignore_mute leaves an address's mute to NodePing, as it does
// a check's, wherever the address block does not set mute: addresses are
// muted and unmuted in the NodePing web interface. Then
//
//   - ModifyPlan plans the mute the prior state has for the address, which
//     the last refresh read, so no plan changes it (plannedIgnoredMutes);
//   - Update leaves mute out for an existing address, and NodePing keeps the
//     mute it holds, even one set after the plan was made (addressRequest);
//   - after the update, state keeps the planned mute rather than NodePing's
//     answer, so a mute set between plan and apply does not make the result
//     inconsistent with the plan; the next refresh reads the real one, which,
//     being ignored, plans nothing (keepIgnoredMutes);
//   - a new address starts unmuted, and Read and import take the mute from
//     NodePing, as they always have.
//
// An address block that sets mute is managed as without ignore_mute.

// ignoredMutes reports, for each address block in the configuration, whether
// its mute is left to NodePing: the provider's ignore_mute is set and the block
// does not set mute. It is nil when ignore_mute is not set. The configuration
// and not the plan tells, since the schema default fills in the plan. Blocks
// are in the same order in the configuration as in the plan.
func (r *ContactResource) ignoredMutes(ctx context.Context, config tfsdk.Config, diags *diag.Diagnostics) []bool {
	if r.client == nil || !r.client.IgnoreMute() {
		return nil
	}

	var list types.List
	var blocks []AddressModel
	d := config.GetAttribute(ctx, path.Root("address"), &list)
	if !d.HasError() && !list.IsNull() && !list.IsUnknown() {
		d.Append(list.ElementsAs(ctx, &blocks, false)...)
	}
	diags.Append(d...)
	if d.HasError() {
		return nil
	}

	ignored := make([]bool, len(blocks))
	for i, block := range blocks {
		ignored[i] = block.Mute.IsNull()
	}
	return ignored
}

// muteIgnoredAt reports whether the mute of the block at position i is left to
// NodePing.
func muteIgnoredAt(ignored []bool, i int) bool {
	return i < len(ignored) && ignored[i]
}

// plannedIgnoredMutes returns, by position, the mute to plan for every block
// whose mute is left to NodePing and that keeps an existing address: the
// prior state's for that address. ids are the IDs plannedAddressIDs planned,
// so the mute follows an address that moves. A new address keeps the schema
// default and starts unmuted.
func plannedIgnoredMutes(prior []AddressModel, ids []types.String, ignored []bool) map[int]types.Bool {
	priorMute := make(map[string]types.Bool, len(prior))
	for _, old := range prior {
		if isKnown(old.ID) {
			priorMute[old.ID.ValueString()] = old.Mute
		}
	}

	planned := make(map[int]types.Bool)
	for i, id := range ids {
		if !muteIgnoredAt(ignored, i) || !isKnown(id) {
			continue
		}
		if mute, ok := priorMute[id.ValueString()]; ok {
			planned[i] = mute
		}
	}
	return planned
}

// keepIgnoredMutes gives every applied address whose mute is left to NodePing
// the mute it was planned with, in place of NodePing's answer. planned is the
// plan's address blocks, matched to the applied addresses by ID.
func keepIgnoredMutes(applied, planned []AddressModel, ignored []bool) {
	plannedMute := make(map[string]types.Bool)
	for i, block := range planned {
		if muteIgnoredAt(ignored, i) && isKnown(block.ID) {
			plannedMute[block.ID.ValueString()] = block.Mute
		}
	}
	for i := range applied {
		if mute, ok := plannedMute[applied[i].ID.ValueString()]; ok {
			applied[i].Mute = mute
		}
	}
}
