package check

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/nodeping/terraform-provider-nodeping/internal/client"
	"github.com/nodeping/terraform-provider-nodeping/internal/datasources/checkattr"
	"github.com/nodeping/terraform-provider-nodeping/internal/importid"
)

var (
	_ resource.Resource                = &CheckResource{}
	_ resource.ResourceWithConfigure   = &CheckResource{}
	_ resource.ResourceWithImportState = &CheckResource{}
	_ resource.ResourceWithModifyPlan  = &CheckResource{}
)

type CheckResource struct {
	client *client.Client
}

func NewCheckResource() resource.Resource {
	return &CheckResource{}
}

func (r *CheckResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_check"
}

func (r *CheckResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = CheckSchema()
}

func (r *CheckResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T.", req.ProviderData),
		)
		return
	}

	r.client = c
}

func (r *CheckResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CheckResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Creating check", map[string]interface{}{
		"type":   plan.Type.ValueString(),
		"target": plan.Target.ValueString(),
	})

	createReq := r.buildCreateRequest(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	check, err := r.client.CreateCheck(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Check",
			"Could not create check: "+err.Error(),
		)
		return
	}

	// Preserve the original target from plan if API normalized it (e.g., added trailing slash)
	originalTarget := plan.Target
	plannedTags := plan.Tags
	plannedPassword := plan.Password

	r.mapCheckToModel(ctx, check, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	preservePassword(&plan, plannedPassword)

	// Restore original target if it's semantically equivalent (trailing slash difference)
	if normalizeURL(originalTarget.ValueString()) == normalizeURL(plan.Target.ValueString()) {
		plan.Target = originalTarget
	}

	// Restore planned tags if API returned null/empty but we sent tags
	if !plannedTags.IsNull() && !plannedTags.IsUnknown() {
		if plan.Tags.IsNull() {
			plan.Tags = plannedTags
		}
	}

	tflog.Debug(ctx, "Created check", map[string]interface{}{
		"id": check.ID,
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *CheckResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state CheckResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Reading check", map[string]interface{}{
		"id": state.ID.ValueString(),
	})

	check, err := r.client.GetCheck(ctx, state.ID.ValueString())
	if err != nil {
		if _, ok := err.(*client.NotFoundError); ok {
			tflog.Debug(ctx, "Check not found, removing from state", map[string]interface{}{
				"id": state.ID.ValueString(),
			})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Check",
			"Could not read check ID "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	// Preserve the original target from state if API normalized it
	originalTarget := state.Target
	statePassword := state.Password

	r.mapCheckToModel(ctx, check, &state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	preservePassword(&state, statePassword)

	// Restore original target if it's semantically equivalent (trailing slash difference)
	if normalizeURL(originalTarget.ValueString()) == normalizeURL(state.Target.ValueString()) {
		state.Target = originalTarget
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *CheckResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan CheckResourceModel
	var state CheckResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Updating check", map[string]interface{}{
		"id": state.ID.ValueString(),
	})

	createReq := r.buildCreateRequest(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	updateReq := client.CheckUpdateRequest{CheckCreateRequest: createReq}

	check, err := r.client.UpdateCheck(ctx, state.ID.ValueString(), updateReq)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Check",
			"Could not update check ID "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	// Preserve the original target from plan if API normalized it
	originalTarget := plan.Target
	// Preserve computed fields from plan to avoid "inconsistent result after apply" errors
	// These fields change on every API call but Terraform expects the planned values
	plannedModified := plan.Modified
	plannedContentString := plan.ContentString
	plannedTags := plan.Tags
	plannedPassword := plan.Password

	r.mapCheckToModel(ctx, check, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	preservePassword(&plan, plannedPassword)

	// Restore original target if it's semantically equivalent (trailing slash difference)
	if normalizeURL(originalTarget.ValueString()) == normalizeURL(plan.Target.ValueString()) {
		plan.Target = originalTarget
	}

	// Restore planned modified value - API always returns new timestamp but Terraform
	// expects the value from the plan (UseStateForUnknown preserves it)
	if !plannedModified.IsUnknown() {
		plan.Modified = plannedModified
	}

	// Restore planned contentstring if plan had a non-empty value but API returned empty
	// This handles the case where user explicitly set contentstring in config
	if !plannedContentString.IsNull() && plannedContentString.ValueString() != "" {
		// User explicitly set contentstring, keep their value if API returned empty
		if plan.ContentString.IsNull() || plan.ContentString.ValueString() == "" {
			plan.ContentString = plannedContentString
		}
	}

	// Restore planned tags if API returned null/empty but we sent tags
	// This can happen if the API doesn't immediately return the tags we just set
	if !plannedTags.IsNull() && !plannedTags.IsUnknown() {
		if plan.Tags.IsNull() {
			plan.Tags = plannedTags
		}
	}

	tflog.Debug(ctx, "Updated check", map[string]interface{}{
		"id": check.ID,
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *CheckResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state CheckResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting check", map[string]interface{}{
		"id": state.ID.ValueString(),
	})

	err := r.client.DeleteCheck(ctx, state.ID.ValueString())
	if err != nil {
		if _, ok := err.(*client.NotFoundError); ok {
			return
		}
		resp.Diagnostics.AddError(
			"Error Deleting Check",
			"Could not delete check ID "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Deleted check", map[string]interface{}{
		"id": state.ID.ValueString(),
	})
}

func (r *CheckResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	checkID, ok := importid.Parse(req.ID, "check", &resp.Diagnostics)
	if !ok {
		return
	}

	tflog.Debug(ctx, "Importing check", map[string]interface{}{
		"check_id": checkID,
	})

	// Deliberately r.client: a SubAccount is reached through a provider
	// instance carrying customer_id, so the same client serves the import and
	// every request after it. See the importid package.
	check, err := r.client.GetCheck(ctx, checkID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Check",
			"Could not import check: "+err.Error(),
		)
		return
	}

	var state CheckResourceModel
	r.mapCheckToModel(ctx, check, &state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *CheckResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// Skip if destroying or client not configured
	if req.Plan.Raw.IsNull() || r.client == nil {
		return
	}

	defaultTags := r.client.GetDefaultTags()
	if len(defaultTags) == 0 {
		return
	}

	var plan CheckResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get configured tags from plan
	var configuredTags []string
	if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
		resp.Diagnostics.Append(plan.Tags.ElementsAs(ctx, &configuredTags, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// Merge default tags with configured tags
	mergedTags := make([]string, 0, len(defaultTags)+len(configuredTags))
	mergedTags = append(mergedTags, defaultTags...)
	mergedTags = append(mergedTags, configuredTags...)

	// Deduplicate
	seen := make(map[string]bool)
	uniqueTags := []string{}
	for _, tag := range mergedTags {
		if !seen[tag] {
			seen[tag] = true
			uniqueTags = append(uniqueTags, tag)
		}
	}

	// Convert to types.List
	tagElements := make([]types.String, len(uniqueTags))
	for i, tag := range uniqueTags {
		tagElements[i] = types.StringValue(tag)
	}
	tagsList, diags := types.ListValueFrom(ctx, types.StringType, tagElements)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Tags = tagsList
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

func (r *CheckResource) buildCreateRequest(ctx context.Context, plan *CheckResourceModel, diags *diag.Diagnostics) client.CheckCreateRequest {
	req := client.CheckCreateRequest{
		Type:   plan.Type.ValueString(),
		Target: plan.Target.ValueString(),
	}

	// Optional+Computed attributes are unknown during plan when the user does
	// not set them. Sending the zero value would overwrite whatever the API
	// would otherwise pick, so skip them entirely.
	if !plan.Label.IsNull() && !plan.Label.IsUnknown() {
		req.Label = plan.Label.ValueString()
	}

	if !plan.Enabled.IsNull() {
		if plan.Enabled.ValueBool() {
			req.Enabled = "active"
		} else {
			req.Enabled = "false"
		}
	}

	if !plan.Public.IsNull() {
		req.Public = plan.Public.ValueBool()
	}

	if !plan.Interval.IsNull() {
		req.Interval = plan.Interval.ValueFloat64()
	}

	if !plan.Threshold.IsNull() {
		req.Threshold = int(plan.Threshold.ValueInt64())
	}

	if !plan.Sens.IsNull() {
		req.Sens = int(plan.Sens.ValueInt64())
	}

	if !plan.Mute.IsNull() {
		req.Mute = plan.Mute.ValueBool()
	}

	if !plan.Dep.IsNull() {
		req.Dep = plan.Dep.ValueString()
	}

	if !plan.Description.IsNull() {
		req.Description = plan.Description.ValueString()
	}

	if !plan.AutoDiag.IsNull() {
		req.AutoDiag = plan.AutoDiag.ValueBool()
	}

	if !plan.RunLocations.IsNull() && !plan.RunLocations.IsUnknown() {
		var locations []string
		diags.Append(plan.RunLocations.ElementsAs(ctx, &locations, false)...)
		if len(locations) > 0 {
			req.RunLocations = locations
		}
	}

	if !plan.HomeLoc.IsNull() {
		req.HomeLoc = plan.HomeLoc.ValueString()
	}

	// Tags are merged with default_tags in ModifyPlan, but ModifyPlan returns
	// early when the provider has no default_tags configured. In that case tags
	// stays unknown (it is Optional+Computed), and ElementsAs cannot convert an
	// unknown value into []string -- it fails the apply outright.
	if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
		var tags []string
		diags.Append(plan.Tags.ElementsAs(ctx, &tags, false)...)
		req.Tags = tags
	}

	if !plan.ContentString.IsNull() {
		req.ContentString = plan.ContentString.ValueString()
	}

	if !plan.Regex.IsNull() {
		req.Regex = plan.Regex.ValueBool()
	}

	if !plan.Invert.IsNull() {
		req.Invert = plan.Invert.ValueBool()
	}

	if !plan.Follow.IsNull() {
		req.Follow = plan.Follow.ValueBool()
	}

	if !plan.Method.IsNull() {
		req.Method = plan.Method.ValueString()
	}

	if !plan.StatusCode.IsNull() {
		req.StatusCode = int(plan.StatusCode.ValueInt64())
	}

	if !plan.SendHeaders.IsNull() {
		headers := make(map[string]string)
		diags.Append(plan.SendHeaders.ElementsAs(ctx, &headers, false)...)
		req.SendHeaders = headers
	}

	if !plan.ReceiveHeaders.IsNull() {
		headers := make(map[string]string)
		diags.Append(plan.ReceiveHeaders.ElementsAs(ctx, &headers, false)...)
		req.ReceiveHeaders = headers
	}

	if !plan.PostData.IsNull() {
		req.PostData = plan.PostData.ValueString()
	}

	if !plan.Port.IsNull() {
		req.Port = int(plan.Port.ValueInt64())
	}

	if !plan.Username.IsNull() {
		req.Username = plan.Username.ValueString()
	}

	if !plan.Password.IsNull() {
		req.Password = plan.Password.ValueString()
	}

	if !plan.Secure.IsNull() {
		req.Secure = plan.Secure.ValueString()
	}

	if !plan.Verify.IsNull() {
		req.Verify = plan.Verify.ValueBool()
	}

	if !plan.IPv6.IsNull() {
		req.IPv6 = plan.IPv6.ValueBool()
	}

	if !plan.DNSType.IsNull() {
		req.DNSType = plan.DNSType.ValueString()
	}

	if !plan.DNSToResolve.IsNull() {
		req.DNSToResolve = plan.DNSToResolve.ValueString()
	}

	if !plan.DNSSection.IsNull() {
		req.DNSSection = plan.DNSSection.ValueString()
	}

	if !plan.DNSRD.IsNull() {
		req.DNSRD = plan.DNSRD.ValueBool()
	}

	if !plan.Transport.IsNull() {
		req.Transport = plan.Transport.ValueString()
	}

	if !plan.WarningDays.IsNull() {
		req.WarningDays = int(plan.WarningDays.ValueInt64())
	}

	if !plan.ServerName.IsNull() {
		req.ServerName = plan.ServerName.ValueString()
	}

	if !plan.Email.IsNull() {
		req.Email = plan.Email.ValueString()
	}

	if !plan.Database.IsNull() {
		req.Database = plan.Database.ValueString()
	}

	if !plan.Query.IsNull() {
		req.Query = plan.Query.ValueString()
	}

	if !plan.Namespace.IsNull() {
		req.Namespace = plan.Namespace.ValueString()
	}

	if !plan.SSHKey.IsNull() {
		req.SSHKey = plan.SSHKey.ValueString()
	}

	if !plan.ClientCert.IsNull() {
		req.ClientCert = plan.ClientCert.ValueString()
	}

	if !plan.SNMPv.IsNull() {
		req.SNMPv = plan.SNMPv.ValueString()
	}

	if !plan.SNMPCom.IsNull() {
		req.SNMPCom = plan.SNMPCom.ValueString()
	}

	if !plan.VerifyVolume.IsNull() {
		req.VerifyVolume = plan.VerifyVolume.ValueBool()
	}

	if !plan.VolumeMin.IsNull() {
		req.VolumeMin = int(plan.VolumeMin.ValueInt64())
	}

	if len(plan.Notifications) > 0 {
		for _, n := range plan.Notifications {
			notif := map[string]interface{}{
				n.ContactID.ValueString(): map[string]interface{}{
					"delay":    int(n.Delay.ValueInt64()),
					"schedule": n.Schedule.ValueString(),
				},
			}
			req.Notifications = append(req.Notifications, notif)
		}
	}

	return req
}

// mapCheckToModel writes an API response onto the resource model.
//
// The mapping itself lives in checkattr, which both check data sources also
// use, so the resource and the data sources cannot end up disagreeing about
// what a check looks like. A second copy used to live here, and the drift
// between the two is what left `database`, `query`, `secure`, `email`,
// `postdata`, `transport`, `dnssection`, `namespace`, `homeloc` and `snmpv`
// unmapped on the resource -- silently, because an unmapped attribute reads
// back null and produces no plan.
//
// What remains below is only what a resource needs and a data source does
// not: a plan to stay consistent with, and credentials the API does not echo.
func (r *CheckResource) mapCheckToModel(ctx context.Context, check *client.Check, model *CheckResourceModel, diags *diag.Diagnostics) {
	a := checkattr.FromAPI(ctx, check, diags)

	model.ID = a.ID
	model.CustomerID = a.CustomerID
	model.Type = a.Type
	model.Target = a.Target
	model.Label = a.Label
	model.Enabled = a.Enabled
	model.Public = a.Public
	model.Mute = a.Mute
	model.AutoDiag = a.AutoDiag
	model.Dep = a.Dep
	model.State = a.State
	model.Created = a.Created
	model.Modified = a.Modified
	model.Description = a.Description
	model.Tags = a.Tags
	model.RunLocations = a.RunLocations
	model.HomeLoc = a.HomeLoc

	model.ContentString = a.ContentString
	model.Regex = a.Regex
	model.Invert = a.Invert
	model.Follow = a.Follow
	model.Method = a.Method
	model.StatusCode = a.StatusCode
	model.SendHeaders = a.SendHeaders
	model.ReceiveHeaders = a.ReceiveHeaders
	model.PostData = a.PostData

	model.Port = a.Port
	model.Username = a.Username
	model.Secure = a.Secure
	model.Verify = a.Verify
	model.IPv6 = a.IPv6
	model.ServerName = a.ServerName
	model.Transport = a.Transport

	model.DNSType = a.DNSType
	model.DNSToResolve = a.DNSToResolve
	model.DNSSection = a.DNSSection
	model.DNSRD = a.DNSRD

	model.WarningDays = a.WarningDays
	model.ClientCert = a.ClientCert

	model.Email = a.Email
	model.Database = a.Database
	model.Query = a.Query
	model.Namespace = a.Namespace
	model.SSHKey = a.SSHKey
	model.SNMPv = a.SNMPv

	model.VerifyVolume = a.VerifyVolume
	model.VolumeMin = a.VolumeMin

	model.Notifications = notificationsToModel(a.Notifications)

	// interval, threshold and sens are Optional+Computed and carry a schema
	// default, so a plan never holds null for any of them. The API omits a
	// field it has no value for, and writing that null over a planned 15 would
	// fail the apply with an inconsistent-result error rather than report
	// anything useful. Keep what the caller already had.
	if !a.Interval.IsNull() {
		model.Interval = a.Interval
	}
	if !a.Threshold.IsNull() {
		model.Threshold = a.Threshold
	}
	if !a.Sens.IsNull() {
		model.Sens = a.Sens
	}

	// NodePing never returns the password, so there is nothing to map here.
	// Callers restore the configured value; see preservePassword.
	model.Password = types.StringNull()

	// snmpcom is left exactly as the caller had it. It is an SNMP community
	// string -- a shared secret in all but name, which is why checkattr omits
	// it -- so Create, Read and Update keep the configured value and an import
	// leaves it null for the configuration to supply, the same bargain as
	// password.
}

// notificationsToModel converts the shared notification shape to the
// resource's own. The two structs carry identical fields; they are separate
// types only because the resource builds its block out of a schema.Block and
// the data sources out of a schema.ListNestedAttribute.
func notificationsToModel(in []checkattr.NotificationModel) []NotificationModel {
	if len(in) == 0 {
		return nil
	}

	out := make([]NotificationModel, 0, len(in))
	for _, n := range in {
		out = append(out, NotificationModel{
			ContactID: n.ContactID,
			Delay:     n.Delay,
			Schedule:  n.Schedule,
		})
	}
	return out
}

// preservePassword restores a write-only credential after mapCheckToModel.
// NodePing does not echo the password back, so mapping the response would
// replace the configured value with null and fail the apply with
// "inconsistent values for sensitive attribute".
func preservePassword(model *CheckResourceModel, configured types.String) {
	if !configured.IsNull() && !configured.IsUnknown() {
		model.Password = configured
	}
}

func normalizeURL(u string) string {
	return strings.TrimSuffix(u, "/")
}
