package check

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/datasources/checkattr"
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
	plannedTagsAll := plan.TagsAll
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

	// tags_all is Computed, so the applied value has to match what was planned
	// even if the API echoes the list back in another order or not at all. The
	// next Read refreshes it from the API.
	if !plannedTagsAll.IsNull() && !plannedTagsAll.IsUnknown() {
		plan.TagsAll = plannedTagsAll
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
	plannedTagsAll := plan.TagsAll
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

	// tags_all is Computed, so the applied value has to match what was planned
	// even if the API echoes the list back in another order or not at all. The
	// next Read refreshes it from the API.
	if !plannedTagsAll.IsNull() && !plannedTagsAll.IsUnknown() {
		plan.TagsAll = plannedTagsAll
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
	r.mapCheckToModel(ctx, check, &state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// mapCheckToModel only fills tags_all, because on a refresh tags belongs to
	// the configuration. An import has no configuration to read, so reconstruct
	// tags as the half of tags_all that is not a provider default -- that is
	// what the configuration would have to say to produce this check.
	resp.Diagnostics.Append(setImportedTags(ctx, &state, c.GetDefaultTags())...)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// setImportedTags derives tags from an imported check's tags_all by removing
// the provider's default tags. A check carrying nothing but defaults gets a
// null tags, matching a configuration that omits the argument entirely.
func setImportedTags(ctx context.Context, state *CheckResourceModel, defaultTags []string) diag.Diagnostics {
	var diags diag.Diagnostics

	if state.TagsAll.IsNull() || state.TagsAll.IsUnknown() {
		state.Tags = types.ListNull(types.StringType)
		return diags
	}

	var all []string
	diags.Append(state.TagsAll.ElementsAs(ctx, &all, false)...)
	if diags.HasError() {
		return diags
	}

	isDefault := make(map[string]bool, len(defaultTags))
	for _, tag := range defaultTags {
		isDefault[tag] = true
	}

	own := make([]string, 0, len(all))
	for _, tag := range all {
		if !isDefault[tag] {
			own = append(own, tag)
		}
	}

	if len(own) == 0 {
		state.Tags = types.ListNull(types.StringType)
		return diags
	}

	tags, d := types.ListValueFrom(ctx, types.StringType, own)
	diags.Append(d...)
	state.Tags = tags

	return diags
}

func (r *CheckResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// Skip if destroying or client not configured
	if req.Plan.Raw.IsNull() || r.client == nil {
		return
	}

	var plan CheckResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// tags is the configuration's own value and must be left exactly as it is:
	// it is Optional, so Terraform rejects a plan that changes it. The merge
	// with default_tags lands in tags_all instead.
	//
	// tags_all stays unknown while tags is, so an unknown tag list does not get
	// silently flattened into the defaults alone.
	if plan.Tags.IsUnknown() {
		plan.TagsAll = types.ListUnknown(types.StringType)
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return
	}

	var configuredTags []string
	if !plan.Tags.IsNull() {
		resp.Diagnostics.Append(plan.Tags.ElementsAs(ctx, &configuredTags, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	tagsAll, diags := types.ListValueFrom(ctx, types.StringType, mergeTags(r.client.GetDefaultTags(), configuredTags))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.TagsAll = tagsAll
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

// mergeTags returns the provider's default tags followed by the check's own,
// with duplicates removed and order preserved. The result is always non-nil so
// that a check with no tags at all plans to an empty list rather than null --
// tags_all is Computed and may not stay unknown after apply.
func mergeTags(defaultTags, configuredTags []string) []string {
	merged := make([]string, 0, len(defaultTags)+len(configuredTags))
	seen := make(map[string]bool, len(defaultTags)+len(configuredTags))

	for _, tag := range append(append([]string{}, defaultTags...), configuredTags...) {
		if seen[tag] {
			continue
		}
		seen[tag] = true
		merged = append(merged, tag)
	}

	return merged
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

	// tags_all, not tags: it is what ModifyPlan merged with the provider's
	// default_tags, and so what the check should actually carry. It is only
	// unknown when tags itself is.
	if !plan.TagsAll.IsNull() && !plan.TagsAll.IsUnknown() {
		var tags []string
		diags.Append(plan.TagsAll.ElementsAs(ctx, &tags, false)...)
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

	// The envelope. The API always answers with these, so there is never a
	// previous value to fall back on.
	model.ID = a.ID
	model.CustomerID = a.CustomerID
	model.Type = a.Type
	model.Enabled = a.Enabled
	model.Public = a.Public
	model.Mute = a.Mute
	model.AutoDiag = a.AutoDiag
	model.State = a.State
	model.Created = a.Created
	model.Modified = a.Modified

	// Everything else resolves through keep(). See its comment: the API's
	// value wins whenever it has one, but its silence must not be allowed to
	// overwrite a planned value with null.
	model.Target = keep(model.Target, a.Target)
	model.Label = keep(model.Label, a.Label)
	model.Dep = keep(model.Dep, a.Dep)
	model.Description = keep(model.Description, a.Description)
	model.Interval = keep(model.Interval, a.Interval)
	model.Threshold = keep(model.Threshold, a.Threshold)
	model.Sens = keep(model.Sens, a.Sens)
	model.RunLocations = keep(model.RunLocations, a.RunLocations)
	model.HomeLoc = keep(model.HomeLoc, a.HomeLoc)

	model.ContentString = keep(model.ContentString, a.ContentString)
	model.Regex = keep(model.Regex, a.Regex)
	model.Invert = keep(model.Invert, a.Invert)
	model.Follow = keep(model.Follow, a.Follow)
	model.Method = keep(model.Method, a.Method)
	model.StatusCode = keep(model.StatusCode, a.StatusCode)
	model.SendHeaders = keep(model.SendHeaders, a.SendHeaders)
	model.ReceiveHeaders = keep(model.ReceiveHeaders, a.ReceiveHeaders)
	model.PostData = keep(model.PostData, a.PostData)

	model.Port = keep(model.Port, a.Port)
	model.Username = keep(model.Username, a.Username)
	model.Secure = keep(model.Secure, a.Secure)
	model.Verify = keep(model.Verify, a.Verify)
	model.IPv6 = keep(model.IPv6, a.IPv6)
	model.ServerName = keep(model.ServerName, a.ServerName)
	model.Transport = keep(model.Transport, a.Transport)

	model.DNSType = keep(model.DNSType, a.DNSType)
	model.DNSToResolve = keep(model.DNSToResolve, a.DNSToResolve)
	model.DNSSection = keep(model.DNSSection, a.DNSSection)
	model.DNSRD = keep(model.DNSRD, a.DNSRD)

	model.WarningDays = keep(model.WarningDays, a.WarningDays)
	model.ClientCert = keep(model.ClientCert, a.ClientCert)

	model.Email = keep(model.Email, a.Email)
	model.Database = keep(model.Database, a.Database)
	model.Query = keep(model.Query, a.Query)
	model.Namespace = keep(model.Namespace, a.Namespace)
	model.SSHKey = keep(model.SSHKey, a.SSHKey)
	model.SNMPv = keep(model.SNMPv, a.SNMPv)

	model.VerifyVolume = keep(model.VerifyVolume, a.VerifyVolume)
	model.VolumeMin = keep(model.VolumeMin, a.VolumeMin)

	// Not keep(): notifications is a block, and a check that genuinely
	// notifies nobody has to come back empty rather than retaining whatever
	// the caller held.
	model.Notifications = notificationsToModel(a.Notifications)

	// Only tags_all is refreshed from the API. tags is the configuration's own
	// value: overwriting it with the API's list would fold default_tags into it
	// and produce a permanent diff against the configuration. No tags reads as
	// an empty list rather than null, matching what ModifyPlan plans.
	if a.Tags.IsNull() {
		model.TagsAll = types.ListValueMust(types.StringType, []attr.Value{})
	} else {
		model.TagsAll = a.Tags
	}

	// tags itself is left exactly as the caller had it -- except in a zero
	// model, as on import, where it would otherwise have no element type and
	// make the state unwritable. ImportState derives the real value from
	// tags_all afterwards; see setImportedTags.
	if model.Tags.ElementType(ctx) == nil {
		model.Tags = types.ListNull(types.StringType)
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

// keep resolves one attribute: the API's value when it has one, whatever the
// caller already held when it does not.
//
// Most check attributes are Optional and not Computed, so Terraform requires
// the value an apply produces to equal the value it planned, exactly.
// mapCheckToModel runs against the *plan* on create and update, so writing
// null over a planned value merely because the response did not mention the
// field fails the apply outright with "Provider produced inconsistent result
// after apply" -- and NodePing does leave a parameter out of its answer when
// the check type does not use it. The mapping this package used to carry
// guarded seven attributes against exactly that, by hand, under the comment
// "these fields are check-type specific and may not be returned by the API".
// checkattr has no equivalent, and correctly so: a data source has no plan to
// contradict. This is that guard, generalised to every attribute rather than
// the seven someone happened to hit.
//
// Drift is still reported whenever the API has an opinion -- a value it
// returns always beats the one in state. What is given up is noticing that a
// parameter disappeared at NodePing altogether, which is the same trade the
// hand-written guards already made.
//
// An unknown value is never kept: it has to be resolved to something concrete
// before the apply ends, so the API's null is the right answer there.
func keep[T interface {
	IsNull() bool
	IsUnknown() bool
}](current, fromAPI T) T {
	if fromAPI.IsNull() && !current.IsNull() && !current.IsUnknown() {
		return current
	}
	return fromAPI
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
