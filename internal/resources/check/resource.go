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

	r.mapCheckToModel(ctx, check, &plan)
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

	r.mapCheckToModel(ctx, check, &state)
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

	r.mapCheckToModel(ctx, check, &plan)
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
	idParts := strings.Split(req.ID, ":")

	var checkID string
	var customerID string

	if len(idParts) == 2 {
		customerID = idParts[0]
		checkID = idParts[1]
	} else if len(idParts) == 1 {
		checkID = idParts[0]
	} else {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			fmt.Sprintf("Expected import ID in format 'check_id' or 'customer_id:check_id', got: %s", req.ID),
		)
		return
	}

	tflog.Debug(ctx, "Importing check", map[string]interface{}{
		"check_id":    checkID,
		"customer_id": customerID,
	})

	c := r.client
	if customerID != "" {
		c = c.WithCustomerID(customerID)
	}

	check, err := c.GetCheck(ctx, checkID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Check",
			"Could not import check: "+err.Error(),
		)
		return
	}

	var state CheckResourceModel
	r.mapCheckToModel(ctx, check, &state)

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

func (r *CheckResource) mapCheckToModel(ctx context.Context, check *client.Check, model *CheckResourceModel) {
	model.ID = types.StringValue(check.ID)
	model.CustomerID = types.StringValue(check.CustomerID)
	model.Type = types.StringValue(check.Type)
	model.Label = types.StringValue(check.Label)

	if check.Enabled == "active" {
		model.Enabled = types.BoolValue(true)
	} else {
		model.Enabled = types.BoolValue(false)
	}

	model.Public = types.BoolValue(check.Public)
	model.AutoDiag = types.BoolValue(check.AutoDiag)

	if interval, err := check.Interval.Float64(); err == nil {
		model.Interval = types.Float64Value(interval)
	}

	model.State = types.Int64Value(int64(check.State))
	model.Created = types.Int64Value(check.Created)
	model.Modified = types.Int64Value(check.Modified)

	if check.Description != "" {
		model.Description = types.StringValue(check.Description)
	} else {
		model.Description = types.StringNull()
	}

	if check.Tags != nil {
		tags, _ := types.ListValueFrom(ctx, types.StringType, check.Tags)
		model.Tags = tags
	} else {
		model.Tags = types.ListNull(types.StringType)
	}

	// Handle RunLocations - API returns false when not set, or []string when set
	switch rl := check.RunLocations.(type) {
	case []interface{}:
		locations := make([]string, 0, len(rl))
		for _, loc := range rl {
			if s, ok := loc.(string); ok {
				locations = append(locations, s)
			}
		}
		if len(locations) > 0 {
			runLocs, _ := types.ListValueFrom(ctx, types.StringType, locations)
			model.RunLocations = runLocs
		} else {
			model.RunLocations = types.ListNull(types.StringType)
		}
	case []string:
		if len(rl) > 0 {
			runLocs, _ := types.ListValueFrom(ctx, types.StringType, rl)
			model.RunLocations = runLocs
		} else {
			model.RunLocations = types.ListNull(types.StringType)
		}
	default:
		model.RunLocations = types.ListNull(types.StringType)
	}

	model.Target = types.StringValue(check.Parameters.Target)

	if threshold, ok := check.Parameters.Threshold.(float64); ok {
		model.Threshold = types.Int64Value(int64(threshold))
	} else if threshold, ok := check.Parameters.Threshold.(string); ok {
		var t int
		fmt.Sscanf(threshold, "%d", &t)
		model.Threshold = types.Int64Value(int64(t))
	}

	if sens, ok := check.Parameters.Sens.(float64); ok {
		model.Sens = types.Int64Value(int64(sens))
	} else if sens, ok := check.Parameters.Sens.(string); ok {
		var s int
		fmt.Sscanf(sens, "%d", &s)
		model.Sens = types.Int64Value(int64(s))
	}

	// ContentString: only set if API returns non-empty value
	// Empty string from API should be treated as null to match TF config expectations
	if check.Parameters.ContentString != "" {
		model.ContentString = types.StringValue(check.Parameters.ContentString)
	} else {
		model.ContentString = types.StringNull()
	}

	// Map boolean fields from API - only set if API returns a value
	// These fields are check-type specific and may not be returned by the API
	if check.Parameters.Regex != nil {
		model.Regex = types.BoolValue(parseBoolInterface(check.Parameters.Regex))
	}
	if check.Parameters.Invert != nil {
		model.Invert = types.BoolValue(parseBoolInterface(check.Parameters.Invert))
	}
	if check.Parameters.Follow != nil {
		model.Follow = types.BoolValue(parseBoolInterface(check.Parameters.Follow))
	}
	if check.Parameters.IPv6 != nil {
		model.IPv6 = types.BoolValue(parseBoolInterface(check.Parameters.IPv6))
	}
	if check.Parameters.Verify != nil {
		model.Verify = types.BoolValue(parseBoolInterface(check.Parameters.Verify))
	}
	if check.Parameters.DNSRD != nil {
		model.DNSRD = types.BoolValue(parseBoolInterface(check.Parameters.DNSRD))
	}
	// Mute is a top-level field that the API always returns
	model.Mute = types.BoolValue(parseBoolInterface(check.Mute))

	// Dep is a top-level field for notification dependency
	if dep, ok := check.Dep.(string); ok && dep != "" {
		model.Dep = types.StringValue(dep)
	} else {
		model.Dep = types.StringNull()
	}

	// Map statuscode from API - only set if API returns a value
	if check.Parameters.StatusCode != nil {
		if statusCode, ok := check.Parameters.StatusCode.(float64); ok {
			model.StatusCode = types.Int64Value(int64(statusCode))
		} else if statusCode, ok := check.Parameters.StatusCode.(string); ok {
			var sc int
			fmt.Sscanf(statusCode, "%d", &sc)
			if sc > 0 {
				model.StatusCode = types.Int64Value(int64(sc))
			}
		}
	}

	if check.Parameters.Method != "" {
		model.Method = types.StringValue(check.Parameters.Method)
	} else {
		model.Method = types.StringNull()
	}

	if check.Parameters.DNSType != "" {
		model.DNSType = types.StringValue(check.Parameters.DNSType)
	} else {
		model.DNSType = types.StringNull()
	}

	if check.Parameters.DNSToResolve != "" {
		model.DNSToResolve = types.StringValue(check.Parameters.DNSToResolve)
	} else {
		model.DNSToResolve = types.StringNull()
	}

	if check.Parameters.ServerName != "" {
		model.ServerName = types.StringValue(check.Parameters.ServerName)
	} else {
		model.ServerName = types.StringNull()
	}

	if check.Parameters.VerifyVolume != nil {
		model.VerifyVolume = types.BoolValue(parseBoolInterface(check.Parameters.VerifyVolume))
	} else {
		model.VerifyVolume = types.BoolNull()
	}

	if volumeMin, ok := check.Parameters.VolumeMin.(float64); ok {
		model.VolumeMin = types.Int64Value(int64(volumeMin))
	} else {
		model.VolumeMin = types.Int64Null()
	}

	model.WarningDays = warningDaysFromAPI(check.Parameters.WarningDays)

	if port, ok := check.Parameters.Port.(float64); ok {
		model.Port = types.Int64Value(int64(port))
	} else {
		model.Port = types.Int64Null()
	}

	if check.Parameters.Username != "" {
		model.Username = types.StringValue(check.Parameters.Username)
	} else {
		model.Username = types.StringNull()
	}

	// The API never returns the password, so there is nothing to map. Callers
	// must restore the configured value; see preservePassword.
	model.Password = types.StringNull()

	if len(check.Parameters.SendHeaders) > 0 {
		headers, _ := types.MapValueFrom(ctx, types.StringType, check.Parameters.SendHeaders)
		model.SendHeaders = headers
	} else {
		model.SendHeaders = types.MapNull(types.StringType)
	}

	if len(check.Parameters.ReceiveHeaders) > 0 {
		headers, _ := types.MapValueFrom(ctx, types.StringType, check.Parameters.ReceiveHeaders)
		model.ReceiveHeaders = headers
	} else {
		model.ReceiveHeaders = types.MapNull(types.StringType)
	}

	// SSHKey and ClientCert can be string or bool from API
	if sshKey, ok := check.Parameters.SSHKey.(string); ok && sshKey != "" {
		model.SSHKey = types.StringValue(sshKey)
	} else {
		model.SSHKey = types.StringNull()
	}

	if clientCert, ok := check.Parameters.ClientCert.(string); ok && clientCert != "" {
		model.ClientCert = types.StringValue(clientCert)
	} else {
		model.ClientCert = types.StringNull()
	}

	if len(check.Notifications) > 0 {
		model.Notifications = make([]NotificationModel, 0, len(check.Notifications))
		// Track seen notifications to avoid duplicates
		seen := make(map[string]bool)
		for _, n := range check.Notifications {
			for contactID, config := range n {
				if configMap, ok := config.(map[string]interface{}); ok {
					var delay int64
					if d, ok := configMap["delay"].(float64); ok {
						delay = int64(d)
					}
					schedule := "All"
					if s, ok := configMap["schedule"].(string); ok {
						schedule = s
					}
					// Create unique key for deduplication
					key := fmt.Sprintf("%s:%d:%s", contactID, delay, schedule)
					if seen[key] {
						continue // Skip duplicate
					}
					seen[key] = true

					notif := NotificationModel{
						ContactID: types.StringValue(contactID),
						Delay:     types.Int64Value(delay),
						Schedule:  types.StringValue(schedule),
					}
					model.Notifications = append(model.Notifications, notif)
				}
			}
		}
	} else {
		model.Notifications = nil
	}
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

// warningDaysFromAPI maps the API's warningdays onto the model. A check that
// should fail only once its certificate expires has no warning period, and the
// API reports that in three ways: "" or no field at all for checks written
// through the API, and 0 for any check saved in the web interface with the
// field left empty. All three read as null. Reading 0 as a number would put a
// value in state that the schema's AtLeast(1) validator rejects in
// configuration, so such a check could be neither imported nor planned
// without a permanent diff.
func warningDaysFromAPI(v interface{}) types.Int64 {
	if days, ok := v.(float64); ok && days > 0 {
		return types.Int64Value(int64(days))
	}
	return types.Int64Null()
}

func normalizeURL(u string) string {
	return strings.TrimSuffix(u, "/")
}

// parseBoolInterface converts various interface{} types to bool.
// NodePing API returns booleans as bool, string ("true"/"false"), or numbers (0/1).
func parseBoolInterface(v interface{}) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true" || val == "1"
	case float64:
		return val != 0
	case int:
		return val != 0
	default:
		return false
	}
}
