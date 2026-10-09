package contact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/importid"
)

var (
	_ resource.Resource                = &ContactResource{}
	_ resource.ResourceWithConfigure   = &ContactResource{}
	_ resource.ResourceWithImportState = &ContactResource{}
	_ resource.ResourceWithModifyPlan  = &ContactResource{}
)

type ContactResource struct {
	client *client.Client
}

func NewContactResource() resource.Resource {
	return &ContactResource{}
}

func (r *ContactResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_contact"
}

func (r *ContactResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = ContactSchema()
}

func (r *ContactResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = c
}

func (r *ContactResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ContactResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Creating contact", map[string]interface{}{
		"name": plan.Name.ValueString(),
	})

	createReq := client.ContactCreateRequest{
		Name:     plan.Name.ValueString(),
		CustRole: plan.CustRole.ValueString(),
	}

	for _, addr := range plan.Addresses {
		newAddr, diags := addressRequest(ctx, addr, nil, false)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		createReq.NewAddresses = append(createReq.NewAddresses, newAddr)
	}

	contact, err := r.client.CreateContact(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Contact",
			"Could not create contact: "+err.Error(),
		)
		return
	}

	plan.ID = types.StringValue(contact.ID)
	plan.CustomerID = types.StringValue(contact.CustomerID)

	plan.Addresses = mapAddressesToModel(ctx, contact.Addresses, plan.Addresses, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Created contact", map[string]interface{}{
		"id": contact.ID,
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ContactResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ContactResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Reading contact", map[string]interface{}{
		"id": state.ID.ValueString(),
	})

	contact, err := r.client.GetContact(ctx, state.ID.ValueString())
	if err != nil {
		if _, ok := errors.AsType[*client.NotFoundError](err); ok {
			tflog.Debug(ctx, "Contact not found, removing from state", map[string]interface{}{
				"id": state.ID.ValueString(),
			})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Contact",
			"Could not read contact ID "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	state.ID = types.StringValue(contact.ID)
	state.CustomerID = types.StringValue(contact.CustomerID)
	state.Name = types.StringValue(contact.Name)
	state.CustRole = types.StringValue(contact.CustRole)

	state.Addresses = mapAddressesToModel(ctx, contact.Addresses, state.Addresses, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ContactResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ContactResourceModel
	var state ContactResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Updating contact", map[string]interface{}{
		"id": state.ID.ValueString(),
	})

	updateReq := client.ContactUpdateRequest{
		Name:     plan.Name.ValueString(),
		CustRole: plan.CustRole.ValueString(),
	}

	ignored := r.ignoredMutes(ctx, req.Config, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	priorAddresses := make(map[string]AddressModel, len(state.Addresses))
	for _, addr := range state.Addresses {
		if isKnown(addr.ID) {
			priorAddresses[addr.ID.ValueString()] = addr
		}
	}

	// Send `addresses` whenever the plan has an address, even if it keeps none
	// of the old ones: without the key NodePing keeps them all next to the new
	// ones. With no address, send neither key. The contact has none to remove
	// (ModifyPlan refuses to remove the last one), and NodePing refuses an
	// empty collection that would leave a contact without an address.
	if len(plan.Addresses) > 0 {
		updateReq.Addresses = make(map[string]client.AddressRequest)
	}
	for i, addr := range plan.Addresses {
		var prior *AddressModel
		if isKnown(addr.ID) {
			if old, ok := priorAddresses[addr.ID.ValueString()]; ok {
				prior = &old
			}
		}

		sent, diags := addressRequest(ctx, addr, prior, muteIgnoredAt(ignored, i))
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}

		if prior != nil {
			updateReq.Addresses[addr.ID.ValueString()] = sent
		} else {
			updateReq.NewAddresses = append(updateReq.NewAddresses, sent)
		}
	}

	contact, err := r.client.UpdateContact(ctx, state.ID.ValueString(), updateReq)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Contact",
			"Could not update contact ID "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	plan.ID = types.StringValue(contact.ID)
	plan.CustomerID = types.StringValue(contact.CustomerID)

	planned := plan.Addresses
	plan.Addresses = mapAddressesToModel(ctx, contact.Addresses, planned, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	keepIgnoredMutes(plan.Addresses, planned, ignored)

	tflog.Debug(ctx, "Updated contact", map[string]interface{}{
		"id": contact.ID,
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// ModifyPlan plans the ID of every address on an update; see
// plannedAddressIDs. A create has no IDs to carry over and a destroy no plan.
// It also refuses an update that removes a contact's last address, or the
// data of an address it keeps. Under the provider's ignore_mute, it plans the
// prior mute of every address whose mute is left to NodePing; see mute.go.
func (r *ContactResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	addressPath := path.Root("address")

	var plannedList, priorList types.List
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, addressPath, &plannedList)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, addressPath, &priorList)...)
	if resp.Diagnostics.HasError() || plannedList.IsUnknown() {
		// A dynamic block over a collection not known yet: there is no block
		// to plan an ID for until it is. Terraform plans again during the
		// apply, with the collection known.
		return
	}

	var planned, prior []AddressModel
	resp.Diagnostics.Append(plannedList.ElementsAs(ctx, &planned, false)...)
	resp.Diagnostics.Append(priorList.ElementsAs(ctx, &prior, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// NodePing refuses any update that would leave a contact with no address,
	// with the misleading "Account must have at least one 'owner' contact."
	// A contact created without an address is fine and can stay that way.
	if len(planned) == 0 && len(prior) > 0 {
		resp.Diagnostics.AddAttributeError(
			addressPath,
			"Cannot remove a contact's last address",
			fmt.Sprintf("The configuration removes every address of contact %s, but NodePing cannot remove a contact's last address. "+
				"Keep at least one address block, or delete the contact and create it again.", describeContact(ctx, req.State, &resp.Diagnostics)),
		)
		return
	}

	ids := plannedAddressIDs(prior, planned)
	for i, id := range ids {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, addressPath.AtListIndex(i).AtName("id"), id)...)
	}

	ignored := r.ignoredMutes(ctx, req.Config, &resp.Diagnostics)
	for i, mute := range plannedIgnoredMutes(prior, ids, ignored) {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, addressPath.AtListIndex(i).AtName("mute"), mute)...)
	}

	for _, i := range addressesDroppingData(prior, planned, ids) {
		resp.Diagnostics.AddAttributeError(
			addressPath.AtListIndex(i).AtName("data"),
			"Cannot remove an address's data",
			fmt.Sprintf("The configuration removes data from address[%d], a %s address of contact %s, but NodePing cannot clear an address's data: "+
				"an update can only replace it. Remove the address block in one apply, then add it back without data in the next; "+
				"NodePing gives it a new ID. If it is the contact's only address, add another one first: NodePing cannot remove a contact's last address.",
				i, planned[i].Type.ValueString(), describeContact(ctx, req.State, &resp.Diagnostics)),
		)
	}
}

// describeContact names the contact in a plan error: by name and ID, or by
// ID alone when it has no name.
func describeContact(ctx context.Context, state tfsdk.State, diags *diag.Diagnostics) string {
	var id, name types.String
	diags.Append(state.GetAttribute(ctx, path.Root("id"), &id)...)
	diags.Append(state.GetAttribute(ctx, path.Root("name"), &name)...)
	contact := "ID " + id.ValueString()
	if name.ValueString() != "" {
		contact = fmt.Sprintf("%q (%s)", name.ValueString(), contact)
	}
	return contact
}

// addressesDroppingData returns the position of every planned block that
// keeps an existing address -- its ID, from plannedAddressIDs, is known --
// but no longer sets the data that address has. NodePing cannot clear an
// address's data: "", null, false and 0 all keep it, and only a value such as
// {} or " " replaces it (finding 32). Data set to "" counts as removed, as
// it changes nothing in NodePing. A new address has nothing to clear, and
// data not known until apply cannot be told yet; Terraform plans again then.
func addressesDroppingData(prior, planned []AddressModel, ids []types.String) []int {
	priorData := make(map[string]types.String, len(prior))
	for _, old := range prior {
		if isKnown(old.ID) {
			priorData[old.ID.ValueString()] = old.Data
		}
	}

	var dropping []int
	for i, block := range planned {
		if !isKnown(ids[i]) || block.Data.IsUnknown() {
			continue
		}
		if old := priorData[ids[i].ValueString()]; old.ValueString() != "" && block.Data.ValueString() == "" {
			dropping = append(dropping, i)
		}
	}
	return dropping
}

// plannedAddressIDs decides, for each planned address block, which existing
// address it is, and returns that address's ID -- or unknown for an address
// NodePing has yet to create. Update sends a known ID under `addresses`, which
// keeps or edits that address, and an unknown one under `newaddresses`; an
// existing ID no block claims is left out, and NodePing deletes it.
//
// Terraform pairs list blocks with prior state by position, and so did
// UseStateForUnknown on address.id. Position is not what identifies an
// address: remove the first of two and the second inherited the first's ID,
// so the update wrote it under that ID and NodePing deleted its own. Contact
// groups and checks reference address IDs, so they were quietly repointed. A
// block added at the end had no prior position at all, and its ID was planned
// null rather than unknown, failing the apply.
//
// A block therefore takes, in this order:
//
//  1. the ID of the first unclaimed prior address with the same type and
//     address, wherever it sat -- reordered blocks keep their IDs, and
//     identical blocks claim identical addresses in order;
//  2. failing that, the ID of the prior address at its own position, if no
//     block claimed it and its type is the same -- an address edited in
//     place, such as a rotated webhook URL, keeps its ID, as NodePing allows.
//     A changed type is a new address instead: whether NodePing can change an
//     address's type in place is untested;
//  3. otherwise unknown.
//
// Every block is tried for 1 before any for 2, so a block that moved claims
// its own ID before a neighbour edited in place can take it by position. An
// address or type not known until apply (taken from another resource) cannot
// match by value; with a known type it can still keep its position's ID.
func plannedAddressIDs(prior, planned []AddressModel) []types.String {
	ids := make([]types.String, len(planned))
	for i := range ids {
		ids[i] = types.StringUnknown()
	}

	claimed := make([]bool, len(prior))
	claimable := func(j int) bool {
		return !claimed[j] && isKnown(prior[j].ID) && prior[j].ID.ValueString() != ""
	}

	for i, block := range planned {
		if !isKnown(block.Type) || !isKnown(block.Address) {
			continue
		}
		for j, old := range prior {
			if claimable(j) && old.Type.Equal(block.Type) && old.Address.Equal(block.Address) {
				claimed[j] = true
				ids[i] = old.ID
				break
			}
		}
	}

	for i, block := range planned {
		if !ids[i].IsUnknown() || i >= len(prior) || !claimable(i) || !isKnown(block.Type) {
			continue
		}
		if prior[i].Type.Equal(block.Type) {
			claimed[i] = true
			ids[i] = prior[i].ID
		}
	}

	return ids
}

func isKnown(s types.String) bool {
	return !s.IsNull() && !s.IsUnknown()
}

func (r *ContactResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ContactResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting contact", map[string]interface{}{
		"id": state.ID.ValueString(),
	})

	err := r.client.DeleteContact(ctx, state.ID.ValueString())
	if err != nil {
		if _, ok := errors.AsType[*client.NotFoundError](err); ok {
			return
		}
		resp.Diagnostics.AddError(
			"Error Deleting Contact",
			"Could not delete contact ID "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Deleted contact", map[string]interface{}{
		"id": state.ID.ValueString(),
	})
}

func (r *ContactResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	contactID, ok := importid.Parse(req.ID, "contact", &resp.Diagnostics)
	if !ok {
		return
	}

	tflog.Debug(ctx, "Importing contact", map[string]interface{}{
		"contact_id": contactID,
	})

	// r.client, as for every other request: a SubAccount is reached through a
	// provider instance carrying its customer_id. See the importid package.
	contact, err := r.client.GetContact(ctx, contactID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Contact",
			"Could not import contact: "+err.Error(),
		)
		return
	}

	state := ContactResourceModel{
		ID:         types.StringValue(contact.ID),
		CustomerID: types.StringValue(contact.CustomerID),
		Name:       types.StringValue(contact.Name),
		CustRole:   types.StringValue(contact.CustRole),
	}

	state.Addresses = mapAddressesToModel(ctx, contact.Addresses, nil, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// mapAddressesToModel turns the API's address map into the ordered list the
// schema declares.
//
// `address` is a list block, so Terraform compares it position by position:
// state.address[0] has to be the address the configuration wrote first. The
// API answers with a map and Go randomises map iteration, so the order used
// to change from one call to the next. A contact with a single address cannot
// show that, which is how it survived; with two or more the apply either
// fails outright with "inconsistent result after apply" or -- the quieter
// outcome -- each block binds to the wrong address.id, and the next update
// PUTs one address's fields under another's ID and corrupts both.
//
// Addresses are therefore matched back to the planned blocks: first by ID,
// where the block has one -- ModifyPlan planned it and Update sent the address
// under it, or on a read it is the ID the prior state recorded -- then by
// (type, address), the pair that identifies an address to someone reading the
// configuration. A block being created has no ID until NodePing assigns one,
// so it can only match by value; and two identical addresses can only be told
// apart by ID, so matching those by value could swap them. Whatever the plan
// does not account for follows in ID order -- an address added outside
// Terraform, or every address when there is no plan to match against, as on
// import. An address with no headers or query strings reads them as the block
// at its position has them; see emptyAsPlanned.
func mapAddressesToModel(ctx context.Context, apiAddresses map[string]client.ContactAddress, planAddresses []AddressModel, diags *diag.Diagnostics) []AddressModel {
	if len(apiAddresses) == 0 {
		return nil
	}

	ids := make([]string, 0, len(apiAddresses))
	for id := range apiAddresses {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// Queue the IDs under the key the configuration knows them by. Two
	// identical blocks hold two queued IDs and claim them in order, so they
	// stay put instead of both resolving to the same address.
	byKey := make(map[string][]string, len(ids))
	for _, id := range ids {
		key := addressKey(apiAddresses[id].Type, apiAddresses[id].Address)
		byKey[key] = append(byKey[key], id)
	}

	// A block planned with an ID is the address NodePing holds under it.
	matched := make([]string, len(planAddresses))
	claimed := make(map[string]bool, len(ids))
	for i, planned := range planAddresses {
		if !isKnown(planned.ID) {
			continue
		}
		if id := planned.ID.ValueString(); !claimed[id] {
			if _, ok := apiAddresses[id]; ok {
				claimed[id] = true
				matched[i] = id
			}
		}
	}

	// Give every other planned block the address it names, where the API
	// still has one. A block left empty here is one whose address changed
	// outside Terraform.
	for i, planned := range planAddresses {
		if matched[i] != "" {
			continue
		}
		key := addressKey(planned.Type.ValueString(), planned.Address.ValueString())
		queue := byKey[key]
		for len(queue) > 0 && claimed[queue[0]] {
			queue = queue[1:]
		}
		if len(queue) == 0 {
			continue
		}
		byKey[key] = queue[1:]
		claimed[queue[0]] = true
		matched[i] = queue[0]
	}

	leftover := make([]string, 0, len(ids))
	for _, id := range ids {
		if !claimed[id] {
			leftover = append(leftover, id)
		}
	}

	// An unmatched block still owns its position, so fill it from the
	// leftovers rather than appending them all at the end. Appending shifts
	// every later block up one: the renamed address lands on the following
	// block, and the next update then writes each block's address under its
	// neighbour's ID. Contact groups reference address IDs, so that quietly
	// repoints any group that named one of them.
	ordered := make([]string, 0, len(ids))
	blocks := make([]*AddressModel, 0, len(ids))
	next := 0
	for i, id := range matched {
		if id == "" {
			if next < len(leftover) {
				ordered = append(ordered, leftover[next])
				blocks = append(blocks, &planAddresses[i])
				next++
			}
			continue
		}
		ordered = append(ordered, id)
		blocks = append(blocks, &planAddresses[i])
	}
	for _, id := range leftover[next:] {
		ordered = append(ordered, id)
		blocks = append(blocks, nil)
	}

	result := make([]AddressModel, 0, len(ordered))
	for i, id := range ordered {
		model := addressToModel(ctx, id, apiAddresses[id], diags)
		if block := blocks[i]; block != nil {
			model.Headers = emptyAsPlanned(model.Headers, block.Headers)
			model.QueryStrings = emptyAsPlanned(model.QueryStrings, block.QueryStrings)
		}
		result = append(result, model)
	}
	return result
}

// emptyAsPlanned reads headers or query strings with no entries the way the
// block has them. NodePing holds none either as {} -- what an update that
// cleared them leaves -- or with no key at all, and addressToModel reads both
// as null, as it must for an import. A block that is `headers = {}` has to
// read back as {} instead; a block without headers stays null.
func emptyAsPlanned(read, planned types.Map) types.Map {
	if read.IsNull() && !planned.IsNull() && !planned.IsUnknown() && len(planned.Elements()) == 0 {
		return planned
	}
	return read
}

// addressKey identifies an address the way a configuration does. NodePing
// allows the same address under two types, so the type is part of the key.
func addressKey(addrType, address string) string {
	return addrType + ":" + address
}

func addressToModel(ctx context.Context, id string, addr client.ContactAddress, diags *diag.Diagnostics) AddressModel {
	model := AddressModel{
		ID:            types.StringValue(id),
		Type:          types.StringValue(addr.Type),
		Address:       types.StringValue(addr.Address),
		SuppressUp:    types.BoolValue(addr.SuppressUp),
		SuppressDown:  types.BoolValue(addr.SuppressDown),
		SuppressFirst: types.BoolValue(addr.SuppressFirst),
		SuppressDiag:  types.BoolValue(addr.SuppressDiag),
		SuppressAll:   types.BoolValue(addr.SuppressAll),
		Mute:          types.BoolValue(false),
	}

	if addr.Mute != nil {
		var muteVal interface{}
		if err := json.Unmarshal(addr.Mute, &muteVal); err == nil {
			switch v := muteVal.(type) {
			case bool:
				model.Mute = types.BoolValue(v)
			case float64:
				model.Mute = types.BoolValue(v > 0)
			}
		}
	}

	if addr.Action != "" {
		model.Action = types.StringValue(addr.Action)
	} else {
		model.Action = types.StringNull()
	}

	if addr.Data != nil {
		// Data can be a string or an object from the API
		// Always normalize to compact JSON for consistent comparison
		switch v := addr.Data.(type) {
		case string:
			if v != "" {
				// Try to normalize JSON string to compact form
				model.Data = types.StringValue(normalizeJSONString(v))
			} else {
				model.Data = types.StringNull()
			}
		case map[string]interface{}:
			// Convert object to compact JSON string
			jsonBytes, err := json.Marshal(v)
			if err == nil {
				model.Data = types.StringValue(string(jsonBytes))
			} else {
				model.Data = types.StringNull()
			}
		default:
			// Try to marshal whatever it is to compact JSON
			jsonBytes, err := json.Marshal(v)
			if err == nil {
				model.Data = types.StringValue(string(jsonBytes))
			} else {
				model.Data = types.StringNull()
			}
		}
	} else {
		model.Data = types.StringNull()
	}

	if addr.Priority != nil {
		model.Priority = types.Int64Value(int64(*addr.Priority))
	} else {
		model.Priority = types.Int64Null()
	}

	if len(addr.Headers) > 0 {
		headers, d := types.MapValueFrom(ctx, types.StringType, addr.Headers)
		diags.Append(d...)
		model.Headers = headers
	} else {
		model.Headers = types.MapNull(types.StringType)
	}

	if len(addr.QueryStrings) > 0 {
		qs, d := types.MapValueFrom(ctx, types.StringType, addr.QueryStrings)
		diags.Append(d...)
		model.QueryStrings = qs
	} else {
		model.QueryStrings = types.MapNull(types.StringType)
	}

	return model
}

// normalizeJSONString attempts to normalize a JSON string to compact form.
// If the string is valid JSON, it returns the compact representation.
// If not valid JSON, it returns the original string unchanged.
func normalizeJSONString(s string) string {
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		// Not valid JSON, return as-is
		return s
	}
	// Re-marshal to compact JSON
	compact, err := json.Marshal(v)
	if err != nil {
		return s
	}
	return string(compact)
}
