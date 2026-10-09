package check

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/datasources/checkattr"
	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/importid"
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

	r.mapCheckToModel(ctx, check, &plan, thePlan, &resp.Diagnostics)
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
		if _, ok := errors.AsType[*client.NotFoundError](err); ok {
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

	r.mapCheckToModel(ctx, check, &state, thePriorState, &resp.Diagnostics)
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

	updateReq := r.buildUpdateRequest(ctx, &plan, &state, &resp.Diagnostics)
	ignoreMute := r.muteIgnored(ctx, req.Config, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Left out, NodePing keeps the check's mute as it is, including a mute set
	// after the plan was made.
	if ignoreMute {
		updateReq.Mute = nil
	}

	check, err := r.client.UpdateCheck(ctx, state.ID.ValueString(), updateReq)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Check",
			"Could not update check ID "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	applied := r.updatedModel(ctx, check, plan, ignoreMute, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// NodePing sometimes answers with the check as it was before the update;
	// see stale_answer.go. The check's modified before the update is the
	// prior state's, which the refresh before this plan read from NodePing.
	// The plan holds the same value, pinned by UseStateForUnknown. An apply
	// keeps the modified it planned (see updatedModel), so only a refresh
	// brings the state's up to date: after -refresh=false it may be older
	// than NodePing's, and a stale answer then looks fresh and fails as it
	// always has.
	if answerMayBeStale(state.Modified, check) && !givesThePlan(ctx, req.Plan, applied) {
		label := plan.Label
		if label.IsUnknown() {
			label = state.Label
		}
		applied = r.readUpdateBack(ctx, req.Plan, plan, state.ID.ValueString(), label.ValueString(),
			state.Modified.ValueInt64(), ignoreMute, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	tflog.Debug(ctx, "Updated check", map[string]interface{}{
		"id": check.ID,
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &applied)...)
}

// updatedModel maps NodePing's answer to an update onto the plan, the way
// Update reports it.
func (r *CheckResource) updatedModel(ctx context.Context, check *client.Check, plan CheckResourceModel, ignoreMute bool, diags *diag.Diagnostics) CheckResourceModel {
	// Preserve the original target from plan if API normalized it
	originalTarget := plan.Target
	// Preserve computed fields from plan to avoid "inconsistent result after apply" errors
	// These fields change on every API call but Terraform expects the planned values
	plannedModified := plan.Modified
	plannedState := plan.State
	plannedTagsAll := plan.TagsAll
	plannedPassword := plan.Password
	plannedMute := plan.Mute

	r.mapCheckToModel(ctx, check, &plan, thePlan, diags)
	if diags.HasError() {
		return plan
	}
	preservePassword(&plan, plannedPassword)

	// NodePing answers with the mute it holds, which can be one set after the
	// plan was made. An ignored mute keeps the planned value, so the apply
	// stays consistent with its plan; the next refresh reads the real one,
	// and since it is ignored, plans nothing.
	if ignoreMute {
		plan.Mute = plannedMute
	}

	// Restore original target if it's semantically equivalent (trailing slash difference)
	if normalizeURL(originalTarget.ValueString()) == normalizeURL(plan.Target.ValueString()) {
		plan.Target = originalTarget
	}

	// Restore planned modified value - API always returns new timestamp but Terraform
	// expects the value from the plan (UseStateForUnknown preserves it)
	if !plannedModified.IsUnknown() {
		plan.Modified = plannedModified
	}

	// NodePing answers with the check's current state, which flips whenever
	// the check goes down or comes back up -- also after the plan was made.
	// Keep the planned one, as for modified; the next refresh reads the real
	// one. id, customer_id and created never change, so need no such guard.
	if !plannedState.IsUnknown() {
		plan.State = plannedState
	}

	// tags_all is Computed, so the applied value has to match what was planned
	// even if the API echoes the list back in another order or not at all. The
	// next Read refreshes it from the API.
	if !plannedTagsAll.IsNull() && !plannedTagsAll.IsUnknown() {
		plan.TagsAll = plannedTagsAll
	}

	return plan
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
		if _, ok := errors.AsType[*client.NotFoundError](err); ok {
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

	// r.client, as for every other request: a SubAccount is reached through a
	// provider instance carrying its customer_id. See the importid package.
	check, err := r.client.GetCheck(ctx, checkID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Check",
			"Could not import check: "+err.Error(),
		)
		return
	}

	var state CheckResourceModel
	r.mapCheckToModel(ctx, check, &state, nothing, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// mapCheckToModel only fills tags_all, because on a refresh tags belongs to
	// the configuration. An import has no configuration to read, so reconstruct
	// tags as the half of tags_all that is not a provider default -- that is
	// what the configuration would have to say to produce this check.
	resp.Diagnostics.Append(setImportedTags(ctx, &state, r.client.GetDefaultTags())...)

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

	r.planTagsAll(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}

	r.planIgnoredMute(ctx, req, resp)
}

// planIgnoredMute plans the prior state's mute for a check whose mute is left
// to NodePing (see muteIgnored), so that no plan changes it. The prior state
// holds what the last refresh read from NodePing. A new check has no prior
// state and starts unmuted, as it always has.
func (r *CheckResource) planIgnoredMute(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || !r.muteIgnored(ctx, req.Config, &resp.Diagnostics) {
		return
	}

	var priorMute types.Bool
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("mute"), &priorMute)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("mute"), priorMute)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Before this ran, the schema default had planned mute false. A plan that
	// differs from the prior state is one in which the framework marks every
	// Computed attribute the configuration leaves unset as unknown -- label,
	// on a check without one -- so a check muted in NodePing would still plan
	// an update, of nothing but those unknowns. If nothing else changes, plan
	// the prior state as it is.
	if unchangedButForUnknowns(resp.Plan.Raw, req.Config.Raw, req.State.Raw) {
		resp.Plan.Raw = req.State.Raw
	}
}

// unchangedButForUnknowns reports whether plan is prior with nothing changed
// but values left unknown where the configuration has none.
func unchangedButForUnknowns(plan, config, prior tftypes.Value) bool {
	filled, err := tftypes.Transform(plan, func(p *tftypes.AttributePath, v tftypes.Value) (tftypes.Value, error) {
		if v.IsKnown() {
			return v, nil
		}
		if configured, ok := valueAt(config, p); !ok || !configured.IsNull() {
			return v, nil
		}
		if before, ok := valueAt(prior, p); ok {
			return before, nil
		}
		return v, nil
	})
	return err == nil && filled.Equal(prior)
}

// valueAt returns the value at a path, if there is one.
func valueAt(v tftypes.Value, p *tftypes.AttributePath) (tftypes.Value, bool) {
	got, _, err := tftypes.WalkAttributePath(v, p)
	if err != nil {
		return tftypes.Value{}, false
	}
	value, ok := got.(tftypes.Value)
	return value, ok
}

// muteIgnored reports whether the provider's ignore_mute leaves the check's
// mute to NodePing: it does, unless the configuration sets mute itself. The
// configuration and not the plan tells, since the schema default fills in the
// plan.
func (r *CheckResource) muteIgnored(ctx context.Context, config tfsdk.Config, diags *diag.Diagnostics) bool {
	if r.client == nil || !r.client.IgnoreMute() {
		return false
	}

	var mute types.Bool
	diags.Append(config.GetAttribute(ctx, path.Root("mute"), &mute)...)
	return mute.IsNull()
}

// planTagsAll plans tags_all: the check's tags merged with the provider's
// default_tags.
//
// It reads only tags and writes only tags_all. The whole plan does not decode
// into CheckResourceModel while the list of notifications blocks is unknown
// -- a dynamic block over IDs not known until apply -- because the model
// holds notifications as a plain slice.
func (r *CheckResource) planTagsAll(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	tagsAllPath := path.Root("tags_all")

	var tags types.List
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("tags"), &tags)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// tags is the configuration's own value and must be left exactly as it is:
	// it is Optional, so Terraform rejects a plan that changes it. The merge
	// with default_tags lands in tags_all instead.
	//
	// tags_all stays unknown while tags is, so an unknown tag list does not get
	// silently flattened into the defaults alone -- and while any one tag is,
	// such as a tag taken from another resource's attribute, which would not
	// convert to a string.
	if tags.IsUnknown() || hasUnknownElement(tags) {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, tagsAllPath, types.ListUnknown(types.StringType))...)
		return
	}

	var configuredTags []string
	if !tags.IsNull() {
		resp.Diagnostics.Append(tags.ElementsAs(ctx, &configuredTags, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	tagsAll, diags := types.ListValueFrom(ctx, types.StringType, mergeTags(r.client.GetDefaultTags(), configuredTags))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, tagsAllPath, tagsAll)...)
}

// hasUnknownElement reports whether a list holds a value that is not known
// until apply.
func hasUnknownElement(list types.List) bool {
	for _, element := range list.Elements() {
		if element.IsUnknown() {
			return true
		}
	}
	return false
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
	// unknown when tags itself is. Empty, it stays an empty list rather than
	// nil, which an update sends to remove the check's last tag.
	if !plan.TagsAll.IsNull() && !plan.TagsAll.IsUnknown() {
		tags := []string{}
		diags.Append(plan.TagsAll.ElementsAs(ctx, &tags, false)...)
		req.Tags = tags
	}

	req.ContentString = nonEmpty(plan.ContentString)

	if !plan.Regex.IsNull() {
		req.Regex = plan.Regex.ValueBool()
	}

	if !plan.Invert.IsNull() {
		req.Invert = plan.Invert.ValueBool()
	}

	if !plan.Follow.IsNull() {
		req.Follow = plan.Follow.ValueBool()
	}

	req.Method = nonEmpty(plan.Method)

	if !plan.StatusCode.IsNull() {
		req.StatusCode = int(plan.StatusCode.ValueInt64())
	}

	req.SendHeaders = headersToAPI(ctx, plan.SendHeaders, diags)
	req.ReceiveHeaders = headersToAPI(ctx, plan.ReceiveHeaders, diags)

	req.PostData = nonEmpty(plan.PostData)

	if !plan.Fields.IsNull() && !plan.Fields.IsUnknown() {
		var fields map[string]checkattr.FieldModel
		diags.Append(plan.Fields.ElementsAs(ctx, &fields, false)...)
		req.Fields = fieldsToAPI(fields)
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

	req.ServerName = nonEmpty(plan.ServerName)

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
// not: a plan or prior state to stay consistent with, and credentials the API
// does not echo. holds says which of the two the model carries; see keep.
func (r *CheckResource) mapCheckToModel(ctx context.Context, check *client.Check, model *CheckResourceModel, holds modelHolds, diags *diag.Diagnostics) {
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
	// overwrite a planned value with null, nor its empty value a null one.
	model.Target = keep(holds, model.Target, a.Target)
	model.Label = keep(holds, model.Label, a.Label)
	model.Dep = keep(holds, model.Dep, a.Dep)
	model.Description = keep(holds, model.Description, a.Description)
	model.Interval = keep(holds, model.Interval, a.Interval)
	model.Threshold = keep(holds, model.Threshold, a.Threshold)
	model.Sens = keep(holds, model.Sens, a.Sens)
	model.RunLocations = keep(holds, model.RunLocations, a.RunLocations)
	model.HomeLoc = keep(holds, model.HomeLoc, a.HomeLoc)

	model.ContentString = keep(holds, model.ContentString, a.ContentString)
	model.Regex = keep(holds, model.Regex, a.Regex)
	model.Invert = keep(holds, model.Invert, a.Invert)
	model.Follow = keep(holds, model.Follow, a.Follow)
	model.Method = keep(holds, model.Method, a.Method)
	model.StatusCode = keep(holds, model.StatusCode, a.StatusCode)
	model.SendHeaders = keep(holds, model.SendHeaders, a.SendHeaders)
	model.ReceiveHeaders = keep(holds, model.ReceiveHeaders, a.ReceiveHeaders)
	model.PostData = keep(holds, model.PostData, a.PostData)
	model.Fields = keep(holds, model.Fields, a.Fields)

	model.Port = keep(holds, model.Port, a.Port)
	model.Username = keep(holds, model.Username, a.Username)
	model.Secure = keep(holds, model.Secure, a.Secure)
	model.Verify = keep(holds, model.Verify, a.Verify)
	model.IPv6 = keep(holds, model.IPv6, a.IPv6)
	model.ServerName = keep(holds, model.ServerName, a.ServerName)
	model.Transport = keep(holds, model.Transport, a.Transport)

	model.DNSType = keep(holds, model.DNSType, a.DNSType)
	model.DNSToResolve = keep(holds, model.DNSToResolve, a.DNSToResolve)
	model.DNSSection = keep(holds, model.DNSSection, a.DNSSection)
	model.DNSRD = keep(holds, model.DNSRD, a.DNSRD)

	model.WarningDays = keep(holds, model.WarningDays, a.WarningDays)
	model.ClientCert = keep(holds, model.ClientCert, a.ClientCert)

	model.Email = keep(holds, model.Email, a.Email)
	model.Database = keep(holds, model.Database, a.Database)
	model.Query = keep(holds, model.Query, a.Query)
	model.Namespace = keep(holds, model.Namespace, a.Namespace)
	model.SSHKey = keep(holds, model.SSHKey, a.SSHKey)
	model.SNMPv = keep(holds, model.SNMPv, a.SNMPv)

	model.VerifyVolume = keep(holds, model.VerifyVolume, a.VerifyVolume)
	model.VolumeMin = keep(holds, model.VolumeMin, a.VolumeMin)

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

// modelHolds is what the model handed to mapCheckToModel holds beforehand.
type modelHolds int

const (
	// thePlan: Create and Update map NodePing's answer onto the plan, which
	// the applied value has to equal.
	thePlan modelHolds = iota
	// thePriorState: Read maps the check onto the state of the last refresh
	// or apply.
	thePriorState
	// nothing: ImportState maps the check onto an empty model. There is no
	// configuration to agree with, so every attribute reads as NodePing
	// stores it -- a false as false, which is why a configuration that
	// imports clean says follow = false where NodePing holds one.
	nothing
)

// keep resolves one attribute: the API's value when it has one, whatever the
// caller already held when it does not, and the caller's when both are empty.
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
// The reverse holds too. A value removed from the configuration is cleared in
// NodePing by sending its empty value (see clearRemoved), and NodePing then
// holds that: false for regex, invert, follow and ipv6, which never go away
// once set. To NodePing the empty value and no value are the same, so where
// the caller holds null or empty and NodePing holds empty, the caller's value
// stands. Otherwise every removed boolean would read back false against a
// planned null. An import has nothing to hold and reads NodePing's values as
// they are; see nothing.
//
// Drift is still reported whenever the API has an opinion -- a value it
// returns always beats the one in state. What is given up is noticing that a
// parameter disappeared at NodePing altogether, which is the same trade the
// hand-written guards already made.
//
// An unknown value is never kept: it has to be resolved to something concrete
// before the apply ends, so the API's value is the right answer there.
func keep[T attr.Value](holds modelHolds, current, fromAPI T) T {
	if holds == nothing || current.IsUnknown() {
		return fromAPI
	}
	if isEmpty(current) && isEmpty(fromAPI) {
		return current
	}
	if fromAPI.IsNull() && !current.IsNull() {
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

// fieldsToAPI converts the configured fields to the request shape. A null min,
// max or match is left out of the request rather than sent as zero.
func fieldsToAPI(in map[string]checkattr.FieldModel) map[string]client.CheckField {
	if len(in) == 0 {
		return nil
	}

	out := make(map[string]client.CheckField, len(in))
	for key, f := range in {
		field := client.CheckField{
			Name:  f.Name.ValueString(),
			Match: f.Match.ValueString(),
		}
		if !f.Min.IsNull() && !f.Min.IsUnknown() {
			field.Min = f.Min.ValueFloat64()
		}
		if !f.Max.IsNull() && !f.Max.IsUnknown() {
			field.Max = f.Max.ValueFloat64()
		}
		out[key] = field
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
