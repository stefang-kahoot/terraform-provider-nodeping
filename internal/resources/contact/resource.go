package contact

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
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
	idParts := strings.Split(req.ID, ":")

	var contactID string
	var customerID string

	if len(idParts) == 2 {
		customerID = idParts[0]
		contactID = idParts[1]
	} else if len(idParts) == 1 {
		contactID = idParts[0]
	} else {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			fmt.Sprintf("Expected import ID in format 'contact_id' or 'customer_id:contact_id', got: %s", req.ID),
		)
		return
	}

	tflog.Debug(ctx, "Importing contact", map[string]interface{}{
		"contact_id":  contactID,
		"customer_id": customerID,
	})

	c := r.client
	if customerID != "" {
		c = c.WithCustomerID(customerID)
	}

	contact, err := c.GetContact(ctx, contactID)
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

func mapAddressesToModel(ctx context.Context, apiAddresses map[string]client.ContactAddress, planAddresses []AddressModel, diags *diag.Diagnostics) []AddressModel {
	if len(apiAddresses) == 0 {
		return nil
	}

	result := make([]AddressModel, 0, len(apiAddresses))

	planAddrByAddress := make(map[string]AddressModel)
	for _, addr := range planAddresses {
		key := addr.Type.ValueString() + ":" + addr.Address.ValueString()
		planAddrByAddress[key] = addr
	}

	for id, addr := range apiAddresses {
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
			headers, _ := types.MapValueFrom(ctx, types.StringType, addr.Headers)
			model.Headers = headers
		} else {
			model.Headers = types.MapNull(types.StringType)
		}

		if len(addr.QueryStrings) > 0 {
			qs, _ := types.MapValueFrom(ctx, types.StringType, addr.QueryStrings)
			model.QueryStrings = qs
		} else {
			model.QueryStrings = types.MapNull(types.StringType)
		}

		result = append(result, model)
	}

	return result
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
