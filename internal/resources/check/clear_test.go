package check

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// requestJSON returns the top-level keys of a request as sent, with their
// JSON values.
func requestJSON(t *testing.T, req any) map[string]string {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		out[k] = string(v)
	}
	return out
}

// clearable lists the attributes clearRemoved clears, set to a value each, as
// a state or plan would hold them.
func clearable() *CheckResourceModel {
	return &CheckResourceModel{
		Type:          types.StringValue("HTTPADV"),
		Target:        types.StringValue("https://example.com"),
		ContentString: types.StringValue("Example"),
		Method:        types.StringValue("POST"),
		PostData:      types.StringValue("probe=1"),
		ServerName:    types.StringValue("example.com"),
		StatusCode:    types.Int64Value(201),
		WarningDays:   types.Int64Value(21),
		Regex:         types.BoolValue(true),
		Invert:        types.BoolValue(true),
		Follow:        types.BoolValue(true),
		IPv6:          types.BoolValue(true),
		Dep:           types.StringValue("CHECK-1"),
		RunLocations:  types.ListValueMust(types.StringType, []attr.Value{types.StringValue("nam")}),
	}
}

// unset is a plan or state without any of them.
func unset() *CheckResourceModel {
	return &CheckResourceModel{
		Type:   types.StringValue("HTTPADV"),
		Target: types.StringValue("https://example.com"),
	}
}

var clearableKeys = []string{
	"contentstring", "method", "postdata", "servername", "statuscode", "warningdays",
	"regex", "invert", "follow", "ipv6", "dep", "runlocations",
}

func TestBuildUpdateRequestClearsRemovedValues(t *testing.T) {
	t.Parallel()

	emptied := unset()
	emptied.ContentString = types.StringValue("")
	emptied.Method = types.StringNull()
	emptied.PostData = types.StringValue("")
	emptied.ServerName = types.StringValue("")
	emptied.Regex = types.BoolValue(false)
	emptied.Dep = types.StringValue("")
	emptied.RunLocations = types.ListValueMust(types.StringType, []attr.Value{})

	setFalse := unset()
	setFalse.Regex = types.BoolValue(false)
	setFalse.Invert = types.BoolValue(false)
	setFalse.Follow = types.BoolValue(false)
	setFalse.IPv6 = types.BoolValue(false)

	tests := []struct {
		name        string
		state, plan *CheckResourceModel
		want        map[string]string
	}{
		{
			name:  "removed values are sent as the value that clears each",
			state: clearable(),
			plan:  unset(),
			want: map[string]string{
				"contentstring": `""`, "method": `""`, "postdata": `""`, "servername": `""`,
				"statuscode": `""`, "warningdays": `""`,
				"regex": `false`, "invert": `false`, "follow": `false`, "ipv6": `false`,
				"dep": `false`, "runlocations": `[]`,
			},
		},
		{
			name:  "emptied values are cleared too",
			state: clearable(),
			plan:  emptied,
			want: map[string]string{
				"contentstring": `""`, "method": `""`, "postdata": `""`, "servername": `""`,
				"statuscode": `""`, "warningdays": `""`,
				"regex": `false`, "invert": `false`, "follow": `false`, "ipv6": `false`,
				"dep": `false`, "runlocations": `[]`,
			},
		},
		{
			name:  "values a check never had are left out",
			state: unset(),
			plan:  unset(),
			want:  map[string]string{},
		},
		{
			name:  "a configured value is sent as configured",
			state: unset(),
			plan:  clearable(),
			want: map[string]string{
				"contentstring": `"Example"`, "method": `"POST"`, "postdata": `"probe=1"`, "servername": `"example.com"`,
				"statuscode": `201`, "warningdays": `21`,
				"regex": `true`, "invert": `true`, "follow": `true`, "ipv6": `true`,
				"dep": `"CHECK-1"`, "runlocations": `["nam"]`,
			},
		},
		{
			name:  "a configured false is sent",
			state: clearable(),
			plan:  setFalse,
			want: map[string]string{
				"contentstring": `""`, "method": `""`, "postdata": `""`, "servername": `""`,
				"statuscode": `""`, "warningdays": `""`,
				"regex": `false`, "invert": `false`, "follow": `false`, "ipv6": `false`,
				"dep": `false`, "runlocations": `[]`,
			},
		},
		{
			// NodePing already holds false, which reads back as unset.
			name:  "a false removed is not sent",
			state: setFalse,
			plan:  unset(),
			want:  map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &CheckResource{}
			var diags diag.Diagnostics
			req := r.buildUpdateRequest(context.Background(), tt.plan, tt.state, &diags)
			if diags.HasError() {
				t.Fatalf("building the request raised %v", diags.Errors())
			}

			sent := requestJSON(t, req)
			for _, key := range clearableKeys {
				got, ok := sent[key]
				want, wantSent := tt.want[key]
				switch {
				case ok && !wantSent:
					t.Errorf("%s = %s sent, want it left out", key, got)
				case !ok && wantSent:
					t.Errorf("%s left out, want %s", key, want)
				case got != want:
					t.Errorf("%s = %s, want %s", key, got, want)
				}
			}
		})
	}
}

// A create has no prior state, so nothing it leaves unset is sent.
func TestBuildCreateRequestLeavesUnsetValuesOut(t *testing.T) {
	t.Parallel()

	r := &CheckResource{}
	var diags diag.Diagnostics
	emptied := unset()
	emptied.ContentString = types.StringValue("")
	emptied.RunLocations = types.ListValueMust(types.StringType, []attr.Value{})

	for _, model := range []*CheckResourceModel{unset(), emptied} {
		sent := requestJSON(t, r.buildCreateRequest(context.Background(), model, &diags))
		var got []string
		for _, key := range clearableKeys {
			if _, ok := sent[key]; ok {
				got = append(got, key)
			}
		}
		if len(got) > 0 {
			sort.Strings(got)
			t.Errorf("a create sent %s", strings.Join(got, ", "))
		}
	}
}

func TestKeep(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		holds         modelHolds
		current, from attr.Value
		want          attr.Value
	}{
		{"NodePing's value beats the plan's", thePlan, types.BoolValue(true), types.BoolValue(false), types.BoolValue(false)},
		{"NodePing's value beats a planned null", thePlan, types.BoolNull(), types.BoolValue(true), types.BoolValue(true)},
		{"a planned value NodePing leaves out stays", thePlan, types.StringValue("x"), types.StringNull(), types.StringValue("x")},
		{"a removed boolean stays removed", thePlan, types.BoolNull(), types.BoolValue(false), types.BoolNull()},
		{"a removed list stays removed", thePlan, types.ListNull(types.StringType), types.ListValueMust(types.StringType, []attr.Value{}), types.ListNull(types.StringType)},
		{"a planned empty string stays", thePlan, types.StringValue(""), types.StringNull(), types.StringValue("")},
		{"a planned false stays", thePlan, types.BoolValue(false), types.BoolValue(false), types.BoolValue(false)},
		{"an unknown takes NodePing's null", thePlan, types.StringUnknown(), types.StringNull(), types.StringNull()},
		{"an unknown takes NodePing's empty value", thePlan, types.BoolUnknown(), types.BoolValue(false), types.BoolValue(false)},
		{"a refresh reads a stored false as unset", thePriorState, types.BoolNull(), types.BoolValue(false), types.BoolNull()},
		{"a refresh keeps a false", thePriorState, types.BoolValue(false), types.BoolValue(false), types.BoolValue(false)},
		{"a refresh reads a change", thePriorState, types.BoolValue(true), types.BoolValue(false), types.BoolValue(false)},
		{"a refresh reads a value NodePing no longer has as removed", thePriorState, types.StringValue("x"), types.StringNull(), types.StringNull()},
		{"a refresh keeps an empty string NodePing has none for", thePriorState, types.StringValue(""), types.StringNull(), types.StringValue("")},
		{"an import reads a stored false as false", nothing, types.BoolNull(), types.BoolValue(false), types.BoolValue(false)},
		{"an import reads an empty list as NodePing has it", nothing, types.ListNull(types.StringType), types.ListNull(types.StringType), types.ListNull(types.StringType)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := keep(tt.holds, tt.current, tt.from); !got.Equal(tt.want) {
				t.Errorf("keep(%v, %s, %s) = %s, want %s", tt.holds, tt.current, tt.from, got, tt.want)
			}
		})
	}
}

func TestBuildUpdateRequestDeletesRemovedHeaders(t *testing.T) {
	t.Parallel()

	headers := func(h map[string]string) types.Map {
		elements := make(map[string]attr.Value, len(h))
		for name, value := range h {
			elements[name] = types.StringValue(value)
		}
		return types.MapValueMust(types.StringType, elements)
	}
	none := types.MapNull(types.StringType)

	tests := []struct {
		name          string
		before, after types.Map
		want          string
	}{
		{"one header of two removed", headers(map[string]string{"X-One": "1", "X-Two": "2"}), headers(map[string]string{"X-One": "1"}), `{"X-One":"1","X-Two":null}`},
		{"every header removed", headers(map[string]string{"X-One": "1", "X-Two": "2"}), none, `{"X-One":null,"X-Two":null}`},
		{"every header removed, leaving an empty map", headers(map[string]string{"X-One": "1"}), headers(map[string]string{}), `{"X-One":null}`},
		{"a header renamed", headers(map[string]string{"X-One": "1"}), headers(map[string]string{"X-Uno": "1"}), `{"X-One":null,"X-Uno":"1"}`},
		{"a header changed", headers(map[string]string{"X-One": "1"}), headers(map[string]string{"X-One": "9"}), `{"X-One":"9"}`},
		{"headers unchanged", headers(map[string]string{"X-One": "1"}), headers(map[string]string{"X-One": "1"}), `{"X-One":"1"}`},
		{"no headers before or after", none, none, ``},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, attribute := range []string{"sendheaders", "receiveheaders"} {
				state, plan := unset(), unset()
				state.SendHeaders, plan.SendHeaders = none, none
				state.ReceiveHeaders, plan.ReceiveHeaders = none, none
				if attribute == "sendheaders" {
					state.SendHeaders, plan.SendHeaders = tt.before, tt.after
				} else {
					state.ReceiveHeaders, plan.ReceiveHeaders = tt.before, tt.after
				}

				r := &CheckResource{}
				var diags diag.Diagnostics
				req := r.buildUpdateRequest(context.Background(), plan, state, &diags)
				if diags.HasError() {
					t.Fatalf("building the request raised %v", diags.Errors())
				}
				sent := requestJSON(t, req)
				if got := sent[attribute]; got != tt.want {
					t.Errorf("%s = %s, want %s", attribute, got, tt.want)
				}
			}
		})
	}
}

// NodePing replaces a check's notifications with the list an update sends,
// and keeps them when it is left out or null: an update always sends it.
func TestBuildUpdateRequestAlwaysSendsNotifications(t *testing.T) {
	t.Parallel()

	notify := func(ids ...string) []NotificationModel {
		var out []NotificationModel
		for _, id := range ids {
			out = append(out, NotificationModel{
				ContactID: types.StringValue(id),
				Delay:     types.Int64Value(0),
				Schedule:  types.StringValue("All"),
			})
		}
		return out
	}

	tests := []struct {
		name          string
		before, after []NotificationModel
		want          string
	}{
		{"the last one removed", notify("G1"), nil, `[]`},
		{"one of two removed", notify("G1", "G2"), notify("G2"), `[{"G2":{"delay":0,"schedule":"All"}}]`},
		{"none before or after", nil, nil, `[]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			state, plan := unset(), unset()
			state.Notifications, plan.Notifications = tt.before, tt.after

			r := &CheckResource{}
			var diags diag.Diagnostics
			req := r.buildUpdateRequest(context.Background(), plan, state, &diags)
			if got := requestJSON(t, req)["notifications"]; got != tt.want {
				t.Errorf("notifications = %s, want %s", got, tt.want)
			}

			if _, sent := requestJSON(t, r.buildCreateRequest(context.Background(), plan, &diags))["notifications"]; sent && len(tt.after) == 0 {
				t.Error("a create sent notifications it does not have")
			}
		})
	}
}

func TestBuildUpdateRequestClearsTheDescription(t *testing.T) {
	t.Parallel()

	described := func(d types.String) *CheckResourceModel {
		m := unset()
		m.Description = d
		return m
	}

	tests := []struct {
		name          string
		before, after types.String
		want          string
	}{
		{"removed", types.StringValue("text"), types.StringNull(), `" "`},
		{"emptied", types.StringValue("text"), types.StringValue(""), `" "`},
		{"changed", types.StringValue("text"), types.StringValue("other"), `"other"`},
		{"never had one", types.StringNull(), types.StringNull(), ``},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := &CheckResource{}
			var diags diag.Diagnostics
			req := r.buildUpdateRequest(context.Background(), described(tt.after), described(tt.before), &diags)
			if got := requestJSON(t, req)["description"]; got != tt.want {
				t.Errorf("description = %s, want %s", got, tt.want)
			}
		})
	}
}

// An update sends public as a string, which NodePing honours both ways; a
// create keeps the boolean.
func TestPublicIsSentAsAStringOnUpdate(t *testing.T) {
	t.Parallel()

	for _, public := range []bool{true, false} {
		model := unset()
		model.Public = types.BoolValue(public)
		r := &CheckResource{}
		var diags diag.Diagnostics

		update := requestJSON(t, r.buildUpdateRequest(context.Background(), model, unset(), &diags))
		if want := `"` + strconv.FormatBool(public) + `"`; update["public"] != want {
			t.Errorf("update sent public = %s, want %s", update["public"], want)
		}
		create := requestJSON(t, r.buildCreateRequest(context.Background(), model, &diags))
		if want := strconv.FormatBool(public); create["public"] != want {
			t.Errorf("create sent public = %s, want %s", create["public"], want)
		}
	}
}

// A description's empty value includes the one the provider clears it with,
// which reads back as none: configured as such, it stays.
func TestKeepDescription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		holds         modelHolds
		current, from types.String
		want          types.String
	}{
		{"a cleared description stays removed", thePlan, types.StringNull(), types.StringNull(), types.StringNull()},
		{"a blank description stays after an apply", thePlan, types.StringValue(" "), types.StringNull(), types.StringValue(" ")},
		{"a blank description stays after a refresh", thePriorState, types.StringValue(" "), types.StringNull(), types.StringValue(" ")},
		{"a description removed in NodePing reads as removed", thePriorState, types.StringValue("text"), types.StringNull(), types.StringNull()},
	}
	for _, tt := range tests {
		if got := keepIf(tt.holds, tt.current, tt.from, isEmptyDescription); !got.Equal(tt.want) {
			t.Errorf("%s: got %s, want %s", tt.name, got, tt.want)
		}
	}
}
