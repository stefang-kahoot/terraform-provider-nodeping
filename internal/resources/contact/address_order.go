package contact

import (
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// onlyAddressOrderChanged reports whether the plan differs from the prior
// state in nothing but the order of the contact's addresses. Update then
// sends nothing: NodePing holds a contact's addresses keyed by ID, with no
// order, so the update would write the same addresses under the same IDs.
//
// An import has no configuration to order the addresses by and reads them in
// ID order (mapAddressesToModel), and Terraform compares the address blocks
// by position. A configuration that lists them in another order therefore
// plans an update right after the import, and applying it used to write to
// NodePing (#30). ModifyPlan has planned each block its own address's ID
// (plannedAddressIDs), which is what the addresses are matched by here.
//
// The order is the only difference when
//
//   - the contact's own attributes are equal;
//   - every planned address has the ID of a prior address, no two the same,
//     and there are as many of them as prior addresses: none is added or
//     removed;
//   - every attribute of a planned address, its computed ID and mute
//     included, equals that of the prior address with its ID.
//
// Equal means equal as state holds the values, not as NodePing might take
// them: a null map is not an empty one, and a missing priority is not 0. The
// one exception is data, where the same JSON written differently is equal,
// as it is to the data plan modifier; see sameData. A value not known until
// apply is never equal. Anything in doubt reports false, and the update is
// sent: a needless update is harmless, while a skipped one would leave
// NodePing out of step with state.
func onlyAddressOrderChanged(plan, prior ContactResourceModel) bool {
	if !same(plan.ID, prior.ID) || !same(plan.CustomerID, prior.CustomerID) ||
		!same(plan.Name, prior.Name) || !same(plan.CustRole, prior.CustRole) ||
		len(plan.Addresses) != len(prior.Addresses) {
		return false
	}

	byID := make(map[string]AddressModel, len(prior.Addresses))
	for _, old := range prior.Addresses {
		if !isKnown(old.ID) || old.ID.ValueString() == "" {
			return false
		}
		byID[old.ID.ValueString()] = old
	}
	if len(byID) != len(prior.Addresses) {
		return false
	}

	// Each planned address takes its prior address out of byID, so a second
	// block with the same ID finds none.
	for _, addr := range plan.Addresses {
		if !isKnown(addr.ID) {
			return false
		}
		old, ok := byID[addr.ID.ValueString()]
		if !ok || !sameAddress(addr, old) {
			return false
		}
		delete(byID, addr.ID.ValueString())
	}
	return true
}

// sameAddress reports whether every attribute of a planned address equals the
// prior one's; see onlyAddressOrderChanged.
func sameAddress(planned, prior AddressModel) bool {
	return same(planned.ID, prior.ID) &&
		same(planned.Type, prior.Type) &&
		same(planned.Address, prior.Address) &&
		same(planned.SuppressUp, prior.SuppressUp) &&
		same(planned.SuppressDown, prior.SuppressDown) &&
		same(planned.SuppressFirst, prior.SuppressFirst) &&
		same(planned.SuppressDiag, prior.SuppressDiag) &&
		same(planned.SuppressAll, prior.SuppressAll) &&
		same(planned.Mute, prior.Mute) &&
		same(planned.Action, prior.Action) &&
		same(planned.Headers, prior.Headers) &&
		same(planned.QueryStrings, prior.QueryStrings) &&
		sameData(planned.Data, prior.Data) &&
		same(planned.Priority, prior.Priority)
}

// sameData reports whether planned data is the same as the prior data: equal,
// or the same JSON written differently, which the data plan modifier takes
// for no change (JSONSemanticEqual). That modifier keeps the prior text only
// for a block at its own position, as Terraform accepts no planned value but
// the configuration's or the prior one at the same position. A reordered
// block therefore plans the configuration's text, and is compared here with
// its own address's prior value. The next refresh reads NodePing's text, and
// the modifier keeps it.
func sameData(planned, prior types.String) bool {
	if same(planned, prior) {
		return true
	}
	return isKnown(planned) && isKnown(prior) && jsonSemanticEqual(planned.ValueString(), prior.ValueString())
}

// same reports whether a planned value is known and equal to the prior one:
// both null, or both set to the same value, a map with the same entries.
// Equal on its own takes two unknown values for equal; prior state holds
// none, but this does not rely on it.
func same(planned, prior attr.Value) bool {
	return fullyKnown(planned) && planned.Equal(prior)
}

// fullyKnown reports whether v is known and, for a map, every entry of it.
func fullyKnown(v attr.Value) bool {
	if v.IsUnknown() {
		return false
	}
	if m, ok := v.(types.Map); ok {
		for _, elem := range m.Elements() {
			if !fullyKnown(elem) {
				return false
			}
		}
	}
	return true
}
