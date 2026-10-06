package contactgroup

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/nodeping/terraform-provider-nodeping/internal/client"
	"github.com/nodeping/terraform-provider-nodeping/internal/importid"
)

var (
	_ resource.Resource                = &ContactGroupResource{}
	_ resource.ResourceWithConfigure   = &ContactGroupResource{}
	_ resource.ResourceWithImportState = &ContactGroupResource{}
)

type ContactGroupResource struct {
	client *client.Client
}

func NewContactGroupResource() resource.Resource {
	return &ContactGroupResource{}
}

func (r *ContactGroupResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_contactgroup"
}

func (r *ContactGroupResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = ContactGroupSchema()
}

func (r *ContactGroupResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// membersFromPlan reads the members list out of the plan. An unset list stays
// nil rather than becoming an empty slice, so an omitted attribute is not sent
// as "remove every member".
func membersFromPlan(ctx context.Context, list types.List, diags *diag.Diagnostics) []string {
	if list.IsNull() || list.IsUnknown() {
		return nil
	}

	var members []string
	diags.Append(list.ElementsAs(ctx, &members, false)...)
	return members
}

// membersToModel maps the API response back. The API omits `members` entirely
// for an empty group, which has to come back as an empty list rather than null
// when the configuration asked for one, otherwise Terraform sees a phantom
// diff.
func membersToModel(ctx context.Context, apiMembers []string, planned types.List, diags *diag.Diagnostics) types.List {
	if len(apiMembers) == 0 {
		if !planned.IsNull() && !planned.IsUnknown() {
			empty, d := types.ListValueFrom(ctx, types.StringType, []string{})
			diags.Append(d...)
			return empty
		}
		return types.ListNull(types.StringType)
	}

	list, d := types.ListValueFrom(ctx, types.StringType, apiMembers)
	diags.Append(d...)
	return list
}

// nameToModel keeps an unset name null. The API answers with an empty string
// for a group that has no name, and name is Optional rather than Computed, so
// writing that empty string back would turn a null config value into "" and
// fail the apply with an inconsistent-result error.
func nameToModel(apiName string, planned types.String) types.String {
	if apiName == "" && (planned.IsNull() || planned.IsUnknown()) {
		return types.StringNull()
	}
	return types.StringValue(apiName)
}

func (r *ContactGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ContactGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := client.ContactGroupCreateRequest{
		Name:    plan.Name.ValueString(),
		Members: membersFromPlan(ctx, plan.Members, &resp.Diagnostics),
	}
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Creating contact group", map[string]interface{}{
		"name":    createReq.Name,
		"members": len(createReq.Members),
	})

	group, err := r.client.CreateContactGroup(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Contact Group",
			"Could not create contact group: "+err.Error(),
		)
		return
	}

	plannedName := plan.Name
	plannedMembers := plan.Members

	plan.ID = types.StringValue(group.ID)
	plan.CustomerID = types.StringValue(group.CustomerID)
	plan.Name = nameToModel(group.Name, plannedName)
	plan.Members = membersToModel(ctx, group.Members, plannedMembers, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Created contact group", map[string]interface{}{"id": group.ID})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ContactGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ContactGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	group, err := r.client.GetContactGroup(ctx, state.ID.ValueString())
	if err != nil {
		// A group deleted outside Terraform must drop out of state rather than
		// fail the plan.
		if _, ok := err.(*client.NotFoundError); ok {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Contact Group",
			"Could not read contact group ID "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	state.CustomerID = types.StringValue(group.CustomerID)
	state.Name = nameToModel(group.Name, state.Name)
	state.Members = membersToModel(ctx, group.Members, state.Members, &resp.Diagnostics)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ContactGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ContactGroupResourceModel
	var state ContactGroupResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	members := membersFromPlan(ctx, plan.Members, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	// Members is non-omitempty in the update request, so a nil slice would
	// marshal as null. Send an empty array instead, which is what clearing a
	// group means.
	if members == nil {
		members = []string{}
	}

	updateReq := client.ContactGroupUpdateRequest{
		Name:    plan.Name.ValueString(),
		Members: members,
	}

	tflog.Debug(ctx, "Updating contact group", map[string]interface{}{
		"id": state.ID.ValueString(),
	})

	group, err := r.client.UpdateContactGroup(ctx, state.ID.ValueString(), updateReq)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Contact Group",
			"Could not update contact group ID "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	plannedName := plan.Name
	plannedMembers := plan.Members

	plan.ID = types.StringValue(group.ID)
	plan.CustomerID = types.StringValue(group.CustomerID)
	plan.Name = nameToModel(group.Name, plannedName)
	plan.Members = membersToModel(ctx, group.Members, plannedMembers, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ContactGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ContactGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting contact group", map[string]interface{}{
		"id": state.ID.ValueString(),
	})

	err := r.client.DeleteContactGroup(ctx, state.ID.ValueString())
	if err != nil {
		if _, ok := err.(*client.NotFoundError); ok {
			return
		}
		resp.Diagnostics.AddError(
			"Error Deleting Contact Group",
			"Could not delete contact group ID "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}
}

func (r *ContactGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	groupID, ok := importid.Parse(req.ID, "contactgroup", &resp.Diagnostics)
	if !ok {
		return
	}

	// Deliberately r.client: a SubAccount is reached through a provider
	// instance carrying customer_id, so the same client serves the import and
	// every request after it. See the importid package.
	group, err := r.client.GetContactGroup(ctx, groupID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Contact Group",
			"Could not import contact group: "+err.Error(),
		)
		return
	}

	state := ContactGroupResourceModel{
		ID:         types.StringValue(group.ID),
		CustomerID: types.StringValue(group.CustomerID),
		Name:       nameToModel(group.Name, types.StringNull()),
	}
	state.Members = membersToModel(ctx, group.Members, types.ListNull(types.StringType), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
