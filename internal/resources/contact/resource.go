package contact

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/nodeping/terraform-provider-nodeping/internal/client"
	"github.com/nodeping/terraform-provider-nodeping/internal/importid"
)

var (
	_ resource.Resource                = &ContactResource{}
	_ resource.ResourceWithConfigure   = &ContactResource{}
	_ resource.ResourceWithImportState = &ContactResource{}
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
		newAddr := client.NewAddress{
			Address:       addr.Address.ValueString(),
			Type:          addr.Type.ValueString(),
			SuppressUp:    addr.SuppressUp.ValueBool(),
			SuppressDown:  addr.SuppressDown.ValueBool(),
			SuppressFirst: addr.SuppressFirst.ValueBool(),
			SuppressDiag:  addr.SuppressDiag.ValueBool(),
			SuppressAll:   addr.SuppressAll.ValueBool(),
			Mute:          addr.Mute.ValueBool(),
		}

		if !addr.Action.IsNull() {
			newAddr.Action = addr.Action.ValueString()
		}
		if !addr.Data.IsNull() {
			newAddr.Data = addr.Data.ValueString()
		}
		if !addr.Priority.IsNull() {
			priority := int(addr.Priority.ValueInt64())
			newAddr.Priority = &priority
		}
		if !addr.Headers.IsNull() {
			headers := make(map[string]string)
			resp.Diagnostics.Append(addr.Headers.ElementsAs(ctx, &headers, false)...)
			if resp.Diagnostics.HasError() {
				return
			}
			newAddr.Headers = headers
		}
		if !addr.QueryStrings.IsNull() {
			qs := make(map[string]string)
			resp.Diagnostics.Append(addr.QueryStrings.ElementsAs(ctx, &qs, false)...)
			if resp.Diagnostics.HasError() {
				return
			}
			newAddr.QueryStrings = qs
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
		if _, ok := err.(*client.NotFoundError); ok {
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

	existingAddressIDs := make(map[string]bool)
	for _, addr := range state.Addresses {
		if !addr.ID.IsNull() && !addr.ID.IsUnknown() {
			existingAddressIDs[addr.ID.ValueString()] = true
		}
	}

	updateReq.Addresses = make(map[string]client.ContactAddress)
	for _, addr := range plan.Addresses {
		if !addr.ID.IsNull() && !addr.ID.IsUnknown() && existingAddressIDs[addr.ID.ValueString()] {
			addrUpdate := client.ContactAddress{
				Address:       addr.Address.ValueString(),
				Type:          addr.Type.ValueString(),
				SuppressUp:    addr.SuppressUp.ValueBool(),
				SuppressDown:  addr.SuppressDown.ValueBool(),
				SuppressFirst: addr.SuppressFirst.ValueBool(),
				SuppressDiag:  addr.SuppressDiag.ValueBool(),
				SuppressAll:   addr.SuppressAll.ValueBool(),
			}

			if addr.Mute.ValueBool() {
				addrUpdate.Mute = []byte("true")
			}

			if !addr.Action.IsNull() {
				addrUpdate.Action = addr.Action.ValueString()
			}
			if !addr.Data.IsNull() {
				addrUpdate.Data = addr.Data.ValueString()
			}
			if !addr.Priority.IsNull() {
				priority := int(addr.Priority.ValueInt64())
				addrUpdate.Priority = &priority
			}
			if !addr.Headers.IsNull() {
				headers := make(map[string]string)
				resp.Diagnostics.Append(addr.Headers.ElementsAs(ctx, &headers, false)...)
				if resp.Diagnostics.HasError() {
					return
				}
				addrUpdate.Headers = headers
			}
			if !addr.QueryStrings.IsNull() {
				qs := make(map[string]string)
				resp.Diagnostics.Append(addr.QueryStrings.ElementsAs(ctx, &qs, false)...)
				if resp.Diagnostics.HasError() {
					return
				}
				addrUpdate.QueryStrings = qs
			}

			updateReq.Addresses[addr.ID.ValueString()] = addrUpdate
		} else {
			newAddr := client.NewAddress{
				Address:       addr.Address.ValueString(),
				Type:          addr.Type.ValueString(),
				SuppressUp:    addr.SuppressUp.ValueBool(),
				SuppressDown:  addr.SuppressDown.ValueBool(),
				SuppressFirst: addr.SuppressFirst.ValueBool(),
				SuppressDiag:  addr.SuppressDiag.ValueBool(),
				SuppressAll:   addr.SuppressAll.ValueBool(),
				Mute:          addr.Mute.ValueBool(),
			}

			if !addr.Action.IsNull() {
				newAddr.Action = addr.Action.ValueString()
			}
			if !addr.Data.IsNull() {
				newAddr.Data = addr.Data.ValueString()
			}
			if !addr.Priority.IsNull() {
				priority := int(addr.Priority.ValueInt64())
				newAddr.Priority = &priority
			}
			if !addr.Headers.IsNull() {
				headers := make(map[string]string)
				resp.Diagnostics.Append(addr.Headers.ElementsAs(ctx, &headers, false)...)
				if resp.Diagnostics.HasError() {
					return
				}
				newAddr.Headers = headers
			}
			if !addr.QueryStrings.IsNull() {
				qs := make(map[string]string)
				resp.Diagnostics.Append(addr.QueryStrings.ElementsAs(ctx, &qs, false)...)
				if resp.Diagnostics.HasError() {
					return
				}
				newAddr.QueryStrings = qs
			}

			updateReq.NewAddresses = append(updateReq.NewAddresses, newAddr)
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

	plan.Addresses = mapAddressesToModel(ctx, contact.Addresses, plan.Addresses, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Updated contact", map[string]interface{}{
		"id": contact.ID,
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
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
		if _, ok := err.(*client.NotFoundError); ok {
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

	// Deliberately r.client: a SubAccount is reached through a provider
	// instance carrying customer_id, so the same client serves the import and
	// every request after it. See the importid package.
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
// Addresses are therefore matched back to the planned blocks by
// (type, address), the pair that identifies an address to someone reading the
// configuration. The ID cannot serve: NodePing assigns it, so it is unknown
// for a block being created. Whatever the plan does not account for follows
// in ID order -- an address added outside Terraform, or every address when
// there is no plan to match against, as on import.
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

	ordered := make([]string, 0, len(ids))
	claimed := make(map[string]bool, len(ids))
	for _, planned := range planAddresses {
		key := addressKey(planned.Type.ValueString(), planned.Address.ValueString())
		queue := byKey[key]
		if len(queue) == 0 {
			continue
		}
		byKey[key] = queue[1:]
		claimed[queue[0]] = true
		ordered = append(ordered, queue[0])
	}
	for _, id := range ids {
		if !claimed[id] {
			ordered = append(ordered, id)
		}
	}

	result := make([]AddressModel, 0, len(ordered))
	for _, id := range ordered {
		result = append(result, addressToModel(ctx, id, apiAddresses[id], diags))
	}
	return result
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
