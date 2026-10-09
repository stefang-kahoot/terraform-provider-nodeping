package check

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/datasources/checkattr"
)

// buildUpdateRequest builds the update of a check from its plan, and from its
// prior state the values the plan removes.
//
// NodePing merges an update into the check: whatever it leaves out keeps its
// stored value (finding 28). The plan alone cannot say what to clear, since a
// removed attribute is simply null in it; the prior state says what the check
// had. Tags and notifications are lists NodePing replaces, and are always
// sent, empty when there are none.
func (r *CheckResource) buildUpdateRequest(ctx context.Context, plan, state *CheckResourceModel, diags *diag.Diagnostics) client.CheckUpdateRequest {
	req := r.buildCreateRequest(ctx, plan, diags)
	clearRemoved(&req, plan, state)

	// An update ignores public sent as the boolean false (or 0); only the
	// strings "false" and "0" switch public reports off, and "true" switches
	// them on (finding 30). A create is left as it was: it sends the boolean,
	// and a new check is not public unless told so.
	if !plan.Public.IsNull() && !plan.Public.IsUnknown() {
		req.Public = strconv.FormatBool(plan.Public.ValueBool())
	}

	notifications := req.Notifications
	if notifications == nil {
		notifications = []map[string]interface{}{}
	}
	return client.CheckUpdateRequest{CheckCreateRequest: req, Tags: req.Tags, Notifications: notifications}
}

// clearRemoved sets, on an update, the value that clears each attribute the
// prior state has and the plan has removed (or emptied). Something the check
// never had is never written. The values are the ones the NodePing API
// stored as cleared when probed on 2026-10-08:
//
//   - contentstring, method, postdata, servername: "".
//   - statuscode, warningdays: "".
//   - regex, invert, follow, ipv6: false. NodePing keeps the key, as false.
//   - dep: false, NodePing's documented way to remove a dependency.
//   - runlocations: [].
//   - sendheaders, receiveheaders: each removed header as null. NodePing
//     merges headers per name, so a smaller map, {} or none at all would
//     keep every header.
//   - description: " ". NodePing ignores "", null, false and 0, so nothing
//     clears a description; it can only be overwritten, and a single space
//     reads back as none (checkattr.ClearedDescription).
//
// The other attributes have not been probed and are left out as before, so
// removing one still keeps its value at NodePing.
func clearRemoved(req *client.CheckCreateRequest, plan, state *CheckResourceModel) {
	for _, s := range []struct {
		prior, planned attr.Value
		field          **string
	}{
		{state.ContentString, plan.ContentString, &req.ContentString},
		{state.Method, plan.Method, &req.Method},
		{state.PostData, plan.PostData, &req.PostData},
		{state.ServerName, plan.ServerName, &req.ServerName},
	} {
		if removed(s.prior, s.planned) {
			*s.field = new(string)
		}
	}

	if removed(state.Description, plan.Description) {
		req.Description = checkattr.ClearedDescription
	}

	for _, v := range []struct {
		prior, planned attr.Value
		field          *interface{}
		cleared        interface{}
	}{
		{state.StatusCode, plan.StatusCode, &req.StatusCode, ""},
		{state.WarningDays, plan.WarningDays, &req.WarningDays, ""},
		{state.Regex, plan.Regex, &req.Regex, false},
		{state.Invert, plan.Invert, &req.Invert, false},
		{state.Follow, plan.Follow, &req.Follow, false},
		{state.IPv6, plan.IPv6, &req.IPv6, false},
		{state.Dep, plan.Dep, &req.Dep, false},
		{state.RunLocations, plan.RunLocations, &req.RunLocations, []string{}},
	} {
		if removed(v.prior, v.planned) {
			*v.field = v.cleared
		}
	}

	req.SendHeaders = deleteRemovedKeys(req.SendHeaders, state.SendHeaders, plan.SendHeaders)
	req.ReceiveHeaders = deleteRemovedKeys(req.ReceiveHeaders, state.ReceiveHeaders, plan.ReceiveHeaders)
}

// deleteRemovedKeys adds to the headers an update sends each header the
// prior state has and the plan does not, as null, which deletes it.
func deleteRemovedKeys(sent map[string]*string, prior, planned types.Map) map[string]*string {
	if prior.IsNull() || prior.IsUnknown() || planned.IsUnknown() {
		return sent
	}
	kept := planned.Elements()
	for name := range prior.Elements() {
		if _, ok := kept[name]; ok {
			continue
		}
		if sent == nil {
			sent = make(map[string]*string)
		}
		sent[name] = nil
	}
	return sent
}

// headersToAPI converts configured headers to the request shape, or nil when
// there are none.
func headersToAPI(ctx context.Context, v types.Map, diags *diag.Diagnostics) map[string]*string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	headers := make(map[string]string)
	diags.Append(v.ElementsAs(ctx, &headers, false)...)
	out := make(map[string]*string, len(headers))
	for name, value := range headers {
		out[name] = &value
	}
	return out
}

// removed reports whether the prior state holds a value the plan no longer
// does.
func removed(prior, planned attr.Value) bool {
	return !isEmpty(prior) && isEmpty(planned)
}

// isEmpty reports whether a value is null or its type's empty value: "",
// false, or a list or map with nothing in it. NodePing treats those alike. A
// number is empty only when null: 0 is a real value for some, such as
// volumemin. An unknown value is not empty.
func isEmpty(v attr.Value) bool {
	if v.IsNull() {
		return true
	}
	if v.IsUnknown() {
		return false
	}
	switch v := v.(type) {
	case types.String:
		return v.ValueString() == ""
	case types.Bool:
		return !v.ValueBool()
	case types.List:
		return len(v.Elements()) == 0
	case types.Map:
		return len(v.Elements()) == 0
	}
	return false
}

// isEmptyDescription is isEmpty for a description, which is also empty when
// it is the value a description is cleared with: NodePing holds that, and it
// reads back as none (checkattr.ClearedDescription).
func isEmptyDescription(v attr.Value) bool {
	if s, ok := v.(types.String); ok && s.ValueString() == checkattr.ClearedDescription {
		return true
	}
	return isEmpty(v)
}

// nonEmpty returns a pointer to a string attribute's value, or nil when it is
// null or "", which leaves it out of a request.
func nonEmpty(v types.String) *string {
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return nil
	}
	s := v.ValueString()
	return &s
}
