package checkattr

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
)

func TestBool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input interface{}
		want  bool
	}{
		{name: "nil", input: nil, want: false},
		{name: "bool true", input: true, want: true},
		{name: "bool false", input: false, want: false},
		{name: `string "true"`, input: "true", want: true},
		{name: `string "1"`, input: "1", want: true},
		{name: `string "false"`, input: "false", want: false},
		{name: `string "0"`, input: "0", want: false},
		{name: "empty string", input: "", want: false},
		// The API only ever sends lowercase, so the comparison stays exact.
		{name: `string "True" is not truthy`, input: "True", want: false},
		{name: "arbitrary string", input: "yes", want: false},
		{name: "float64 1", input: float64(1), want: true},
		{name: "float64 0", input: float64(0), want: false},
		{name: "float64 negative", input: float64(-1), want: true},
		{name: "float64 fractional", input: float64(0.5), want: true},
		{name: "int 1", input: 1, want: true},
		{name: "int 0", input: 0, want: false},
		{name: "unhandled type does not panic", input: []string{"true"}, want: false},
		{name: "unhandled type map", input: map[string]bool{"v": true}, want: false},
		{name: "unhandled type int64", input: int64(1), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Bool(tt.input); got != tt.want {
				t.Errorf("Bool(%#v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// An absent field must stay null. Reporting false would make "this check type
// does not use the flag" indistinguishable from "the flag is off".
func TestOptionalBool(t *testing.T) {
	t.Parallel()

	if got := OptionalBool(nil); !got.IsNull() {
		t.Errorf("OptionalBool(nil) = %v, want null", got)
	}
	if got := OptionalBool(false); got.IsNull() || got.ValueBool() {
		t.Errorf("OptionalBool(false) = %v, want a non-null false", got)
	}
	if got := OptionalBool("true"); got.IsNull() || !got.ValueBool() {
		t.Errorf(`OptionalBool("true") = %v, want a non-null true`, got)
	}
}

func TestOptionalInt64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    interface{}
		wantNull bool
		want     int64
	}{
		{name: "nil is null", input: nil, wantNull: true},
		{name: "float64", input: float64(42), want: 42},
		{name: "float64 truncates", input: float64(42.9), want: 42},
		{name: "int", input: 7, want: 7},
		{name: "int64", input: int64(9), want: 9},
		// NodePing stringifies some numbers, e.g. threshold and sens.
		{name: "numeric string", input: "5", want: 5},
		{name: "negative numeric string", input: "-40", want: -40},
		{name: "non-numeric string is null", input: "abc", wantNull: true},
		{name: "unhandled type is null", input: []int{1}, wantNull: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := OptionalInt64(tt.input)
			if tt.wantNull {
				if !got.IsNull() {
					t.Errorf("OptionalInt64(%#v) = %v, want null", tt.input, got)
				}
				return
			}
			if got.IsNull() {
				t.Fatalf("OptionalInt64(%#v) = null, want %d", tt.input, tt.want)
			}
			if got.ValueInt64() != tt.want {
				t.Errorf("OptionalInt64(%#v) = %d, want %d", tt.input, got.ValueInt64(), tt.want)
			}
		})
	}
}

func TestOptionalWarningDays(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    interface{}
		wantNull bool
		want     int64
	}{
		{name: "number", input: float64(21), want: 21},
		{name: "numeric string", input: "30", want: 30},
		// Saved in the web interface with the field left empty.
		{name: "zero is null", input: float64(0), wantNull: true},
		{name: "zero string is null", input: "0", wantNull: true},
		// Written through the API without a warning period.
		{name: "empty string is null", input: "", wantNull: true},
		{name: "nil is null", input: nil, wantNull: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := OptionalWarningDays(tt.input)
			if tt.wantNull {
				if !got.IsNull() {
					t.Errorf("OptionalWarningDays(%#v) = %v, want null", tt.input, got)
				}
				return
			}
			if got.IsNull() {
				t.Fatalf("OptionalWarningDays(%#v) = null, want %d", tt.input, tt.want)
			}
			if got.ValueInt64() != tt.want {
				t.Errorf("OptionalWarningDays(%#v) = %d, want %d", tt.input, got.ValueInt64(), tt.want)
			}
		})
	}
}

func TestOptionalString(t *testing.T) {
	t.Parallel()

	if got := OptionalString(""); !got.IsNull() {
		t.Errorf(`OptionalString("") = %v, want null`, got)
	}
	if got := OptionalString("x"); got.IsNull() || got.ValueString() != "x" {
		t.Errorf(`OptionalString("x") = %v, want "x"`, got)
	}
}

// sshkey, clientcert, secure and homeloc are typed interface{} because the API
// answers with `false` when they are unset.
func TestStringFromInterface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    interface{}
		wantNull bool
		want     string
	}{
		{name: "string passes through", input: "KEY123", want: "KEY123"},
		{name: "false becomes null", input: false, wantNull: true},
		{name: "nil becomes null", input: nil, wantNull: true},
		{name: "empty string becomes null", input: "", wantNull: true},
		{name: "number becomes null", input: float64(0), wantNull: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := stringFromInterface(tt.input)
			if tt.wantNull {
				if !got.IsNull() {
					t.Errorf("got %v, want null", got)
				}
				return
			}
			if got.ValueString() != tt.want {
				t.Errorf("got %q, want %q", got.ValueString(), tt.want)
			}
		})
	}
}

func TestRunLocations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    interface{}
		wantNull bool
		want     []string
	}{
		{
			name:  "decoded JSON array",
			input: []interface{}{"nam", "eur"},
			want:  []string{"nam", "eur"},
		},
		{
			name:  "already a string slice",
			input: []string{"nam"},
			want:  []string{"nam"},
		},
		{
			// This is the shape the API uses for "no locations pinned".
			name:     "false becomes null",
			input:    false,
			wantNull: true,
		},
		{name: "nil becomes null", input: nil, wantNull: true},
		{name: "empty array becomes null", input: []interface{}{}, wantNull: true},
		{
			name:  "non-string elements are skipped",
			input: []interface{}{"nam", 42},
			want:  []string{"nam"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var diags diag.Diagnostics
			got := RunLocations(context.Background(), tt.input, &diags)

			if diags.HasError() {
				t.Fatalf("unexpected diagnostics: %+v", diags)
			}
			if tt.wantNull {
				if !got.IsNull() {
					t.Errorf("got %v, want null", got)
				}
				return
			}

			var elems []string
			if d := got.ElementsAs(context.Background(), &elems, false); d.HasError() {
				t.Fatalf("failed to read list: %+v", d)
			}
			if len(elems) != len(tt.want) {
				t.Fatalf("got %#v, want %#v", elems, tt.want)
			}
			for i := range tt.want {
				if elems[i] != tt.want[i] {
					t.Errorf("index %d: got %q, want %q", i, elems[i], tt.want[i])
				}
			}
		})
	}
}

func TestFromAPIMapsTypeSpecificParameters(t *testing.T) {
	t.Parallel()

	check := &client.Check{
		ID:         "CHK1",
		CustomerID: "CUST1",
		Type:       "HTTPCONTENT",
		Label:      "example",
		Enabled:    "active",
		Interval:   json.Number("5"),
		State:      1,
		Parameters: client.CheckParameters{
			Target:        "https://example.com",
			Threshold:     float64(5),
			Sens:          "2",
			ContentString: "all good",
			Regex:         "false",
			Follow:        true,
			Method:        "GET",
			StatusCode:    float64(200),
			SendHeaders:   map[string]string{"accept": "application/json"},
			Port:          float64(8443),
			Username:      "svc",
			Password:      "must-not-surface",
			SNMPCom:       "must-not-surface",
			SSHKey:        false,
			WarningDays:   float64(30),
		},
	}

	var diags diag.Diagnostics
	m := FromAPI(context.Background(), check, &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}

	if m.Target.ValueString() != "https://example.com" {
		t.Errorf("Target = %q", m.Target.ValueString())
	}
	if m.ContentString.ValueString() != "all good" {
		t.Errorf("ContentString = %q", m.ContentString.ValueString())
	}
	if m.Threshold.ValueInt64() != 5 {
		t.Errorf("Threshold = %d", m.Threshold.ValueInt64())
	}
	// Sens arrives as a string and has to survive as a number.
	if m.Sens.ValueInt64() != 2 {
		t.Errorf("Sens = %d", m.Sens.ValueInt64())
	}
	if m.StatusCode.ValueInt64() != 200 {
		t.Errorf("StatusCode = %d", m.StatusCode.ValueInt64())
	}
	if m.Port.ValueInt64() != 8443 {
		t.Errorf("Port = %d", m.Port.ValueInt64())
	}
	if m.WarningDays.ValueInt64() != 30 {
		t.Errorf("WarningDays = %d", m.WarningDays.ValueInt64())
	}
	if m.Regex.IsNull() || m.Regex.ValueBool() {
		t.Errorf(`Regex = %v, want non-null false (API sent "false")`, m.Regex)
	}
	if m.Follow.IsNull() || !m.Follow.ValueBool() {
		t.Errorf("Follow = %v, want non-null true", m.Follow)
	}
	// Not set by this check type, so it must be null rather than false.
	if !m.Invert.IsNull() {
		t.Errorf("Invert = %v, want null", m.Invert)
	}
	if !m.SSHKey.IsNull() {
		t.Errorf("SSHKey = %v, want null when the API sends false", m.SSHKey)
	}
	if m.Username.ValueString() != "svc" {
		t.Errorf("Username = %q", m.Username.ValueString())
	}
	if !m.Enabled.ValueBool() {
		t.Error("Enabled should be true for enable=active")
	}
}

// The model must not carry a field for any credential, so a secret cannot end
// up in state through a data source.
func TestModelHasNoCredentialFields(t *testing.T) {
	t.Parallel()

	attrs := Attributes()
	for _, forbidden := range []string{"password", "snmpcom", "checktoken"} {
		if _, ok := attrs[forbidden]; ok {
			t.Errorf("attribute %q must not be exposed by a data source", forbidden)
		}
	}
}

// Every attribute needs a description; they become the registry documentation.
func TestAttributesAreDocumentedAndComputed(t *testing.T) {
	t.Parallel()

	for name, attr := range Attributes() {
		if attr.GetDescription() == "" && attr.GetMarkdownDescription() == "" {
			t.Errorf("attribute %q has no description", name)
		}
		if !attr.IsComputed() {
			t.Errorf("attribute %q must be Computed", name)
		}
	}
}

func TestNotifications(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []map[string]interface{}
		want []NotificationModel
	}{
		{name: "nil is nil", in: nil, want: nil},
		{name: "empty is nil", in: []map[string]interface{}{}, want: nil},
		{
			name: "single entry",
			in: []map[string]interface{}{
				{"CONTACT-1": map[string]interface{}{"delay": float64(0), "schedule": "All"}},
			},
			want: []NotificationModel{
				{ContactID: types.StringValue("CONTACT-1"), Delay: types.Int64Value(0), Schedule: types.StringValue("All")},
			},
		},
		{
			// The API's outer ordering is meaningful and must survive.
			name: "outer order is preserved",
			in: []map[string]interface{}{
				{"ZZZ": map[string]interface{}{"delay": float64(5), "schedule": "Days"}},
				{"AAA": map[string]interface{}{"delay": float64(0), "schedule": "All"}},
			},
			want: []NotificationModel{
				{ContactID: types.StringValue("ZZZ"), Delay: types.Int64Value(5), Schedule: types.StringValue("Days")},
				{ContactID: types.StringValue("AAA"), Delay: types.Int64Value(0), Schedule: types.StringValue("All")},
			},
		},
		{
			// Several keys in one entry would otherwise reorder between reads,
			// because Go randomises map iteration.
			name: "multiple keys in one entry are sorted",
			in: []map[string]interface{}{
				{
					"BBB": map[string]interface{}{"delay": float64(1), "schedule": "All"},
					"AAA": map[string]interface{}{"delay": float64(2), "schedule": "All"},
				},
			},
			want: []NotificationModel{
				{ContactID: types.StringValue("AAA"), Delay: types.Int64Value(2), Schedule: types.StringValue("All")},
				{ContactID: types.StringValue("BBB"), Delay: types.Int64Value(1), Schedule: types.StringValue("All")},
			},
		},
		{
			name: "duplicates are dropped",
			in: []map[string]interface{}{
				{"CONTACT-1": map[string]interface{}{"delay": float64(0), "schedule": "All"}},
				{"CONTACT-1": map[string]interface{}{"delay": float64(0), "schedule": "All"}},
			},
			want: []NotificationModel{
				{ContactID: types.StringValue("CONTACT-1"), Delay: types.Int64Value(0), Schedule: types.StringValue("All")},
			},
		},
		{
			name: "missing delay and schedule fall back to 0 and All",
			in: []map[string]interface{}{
				{"CONTACT-1": map[string]interface{}{}},
			},
			want: []NotificationModel{
				{ContactID: types.StringValue("CONTACT-1"), Delay: types.Int64Value(0), Schedule: types.StringValue("All")},
			},
		},
		{
			name: "a delay sent as a string still parses",
			in: []map[string]interface{}{
				{"CONTACT-1": map[string]interface{}{"delay": "15", "schedule": "Nights"}},
			},
			want: []NotificationModel{
				{ContactID: types.StringValue("CONTACT-1"), Delay: types.Int64Value(15), Schedule: types.StringValue("Nights")},
			},
		},
		{
			// Older checks store the schedule itself in place of the object,
			// with no delay. Reading it as no notification made an import
			// claim that nobody is told when the check fails.
			name: "short form is the schedule with no delay",
			in: []map[string]interface{}{
				{"CONTACT-1": "All"},
				{"CONTACT-2": "Nights"},
			},
			want: []NotificationModel{
				{ContactID: types.StringValue("CONTACT-1"), Delay: types.Int64Value(0), Schedule: types.StringValue("All")},
				{ContactID: types.StringValue("CONTACT-2"), Delay: types.Int64Value(0), Schedule: types.StringValue("Nights")},
			},
		},
		{
			name: "empty short form falls back to All",
			in: []map[string]interface{}{
				{"CONTACT-1": ""},
			},
			want: []NotificationModel{
				{ContactID: types.StringValue("CONTACT-1"), Delay: types.Int64Value(0), Schedule: types.StringValue("All")},
			},
		},
		{
			// Both shapes in one check keep the API's outer order, and the
			// keys inside one entry are still sorted.
			name: "short and object forms mix in one check",
			in: []map[string]interface{}{
				{"ZZZ": "Days"},
				{"MMM": map[string]interface{}{"delay": float64(5), "schedule": "Nights"}},
				{
					"BBB": map[string]interface{}{"delay": float64(1), "schedule": "All"},
					"AAA": "Weekends",
				},
			},
			want: []NotificationModel{
				{ContactID: types.StringValue("ZZZ"), Delay: types.Int64Value(0), Schedule: types.StringValue("Days")},
				{ContactID: types.StringValue("MMM"), Delay: types.Int64Value(5), Schedule: types.StringValue("Nights")},
				{ContactID: types.StringValue("AAA"), Delay: types.Int64Value(0), Schedule: types.StringValue("Weekends")},
				{ContactID: types.StringValue("BBB"), Delay: types.Int64Value(1), Schedule: types.StringValue("All")},
			},
		},
		{
			// The short form and the object form with delay 0 are the same
			// notification, so the second is a duplicate.
			name: "short form deduplicates against the equivalent object",
			in: []map[string]interface{}{
				{"CONTACT-1": "All"},
				{"CONTACT-1": map[string]interface{}{"delay": float64(0), "schedule": "All"}},
				{"CONTACT-1": ""},
			},
			want: []NotificationModel{
				{ContactID: types.StringValue("CONTACT-1"), Delay: types.Int64Value(0), Schedule: types.StringValue("All")},
			},
		},
		{
			name: "short form does not swallow an object with a different delay",
			in: []map[string]interface{}{
				{"CONTACT-1": "All"},
				{"CONTACT-1": map[string]interface{}{"delay": float64(5), "schedule": "All"}},
			},
			want: []NotificationModel{
				{ContactID: types.StringValue("CONTACT-1"), Delay: types.Int64Value(0), Schedule: types.StringValue("All")},
				{ContactID: types.StringValue("CONTACT-1"), Delay: types.Int64Value(5), Schedule: types.StringValue("All")},
			},
		},
		{
			name: "values that are neither an object nor a string are skipped",
			in: []map[string]interface{}{
				{"CONTACT-1": nil},
				{"CONTACT-2": float64(0)},
				{"CONTACT-3": true},
				{"CONTACT-4": []interface{}{"All"}},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := notifications(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d entries, want %d: %#v", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if !got[i].ContactID.Equal(tt.want[i].ContactID) {
					t.Errorf("index %d: ContactID = %v, want %v", i, got[i].ContactID, tt.want[i].ContactID)
				}
				if !got[i].Delay.Equal(tt.want[i].Delay) {
					t.Errorf("index %d: Delay = %v, want %v", i, got[i].Delay, tt.want[i].Delay)
				}
				if !got[i].Schedule.Equal(tt.want[i].Schedule) {
					t.Errorf("index %d: Schedule = %v, want %v", i, got[i].Schedule, tt.want[i].Schedule)
				}
			}
		})
	}
}

// A header NodePing stores as null is absent, so a check whose only header is
// null reads back with no sendheaders at all rather than {"Host" = ""}.
func TestFromAPIReadsNullHeadersAsAbsent(t *testing.T) {
	t.Parallel()

	var check client.Check
	body := `{"_id": "CHK1", "type": "HTTPADV", "parameters": {
		"sendheaders": {"Host": null},
		"receiveheaders": {"Server": null, "Content-Type": "text/html"}
	}}`
	if err := json.Unmarshal([]byte(body), &check); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	var diags diag.Diagnostics
	m := FromAPI(context.Background(), &check, &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}

	if !m.SendHeaders.IsNull() {
		t.Errorf("SendHeaders = %v, want null", m.SendHeaders)
	}
	want := types.MapValueMust(types.StringType, map[string]attr.Value{
		"Content-Type": types.StringValue("text/html"),
	})
	if !m.ReceiveHeaders.Equal(want) {
		t.Errorf("ReceiveHeaders = %v, want %v", m.ReceiveHeaders, want)
	}
}

func TestOptionalFloat64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    interface{}
		wantNull bool
		want     float64
	}{
		{name: "nil is null", input: nil, wantNull: true},
		{name: "float64", input: float64(200), want: 200},
		{name: "fractional", input: float64(0.5), want: 0.5},
		{name: "zero is a value", input: float64(0), want: 0},
		{name: "int", input: 3, want: 3},
		{name: "int64", input: int64(999999999), want: 999999999},
		{name: "numeric string", input: "1.5", want: 1.5},
		{name: "non-numeric string is null", input: "high", wantNull: true},
		{name: "empty string is null", input: "", wantNull: true},
		{name: "unhandled type is null", input: true, wantNull: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := OptionalFloat64(tt.input)
			if tt.wantNull {
				if !got.IsNull() {
					t.Errorf("OptionalFloat64(%#v) = %v, want null", tt.input, got)
				}
				return
			}
			if got.IsNull() || got.ValueFloat64() != tt.want {
				t.Errorf("OptionalFloat64(%#v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// fields reads back keyed exactly as NodePing stores it. The keys are
// arbitrary, but a configuration has to name the same ones, so they cannot be
// dropped or renumbered.
func TestFromAPIMapsFields(t *testing.T) {
	t.Parallel()

	var check client.Check
	body := `{"_id": "CHK1", "type": "HTTPPARSE", "parameters": {
		"target": "https://example.com/stats.json",
		"fields": {
			"F7L814": {"name": "status", "min": 200, "max": 200},
			"A":      {"name": "load", "min": "0.5"},
			"B":      {"name": "rows", "max": 0, "match": "ok"}
		}
	}}`
	if err := json.Unmarshal([]byte(body), &check); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	var diags diag.Diagnostics
	m := FromAPI(context.Background(), &check, &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}

	field := func(name string, min, max types.Float64, match types.String) attr.Value {
		return types.ObjectValueMust(FieldType.AttrTypes, map[string]attr.Value{
			"name": types.StringValue(name), "min": min, "max": max, "match": match,
		})
	}
	want := types.MapValueMust(FieldType, map[string]attr.Value{
		"F7L814": field("status", types.Float64Value(200), types.Float64Value(200), types.StringNull()),
		"A":      field("load", types.Float64Value(0.5), types.Float64Null(), types.StringNull()),
		"B":      field("rows", types.Float64Null(), types.Float64Value(0), types.StringValue("ok")),
	})
	if !m.Fields.Equal(want) {
		t.Errorf("Fields = %v, want %v", m.Fields, want)
	}
}

// A check without fields reads as a typed null, so state can still be written.
func TestFromAPIReadsNoFieldsAsNull(t *testing.T) {
	t.Parallel()

	var diags diag.Diagnostics
	m := FromAPI(context.Background(), &client.Check{ID: "CHK1", Type: "HTTP"}, &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}
	if !m.Fields.IsNull() {
		t.Errorf("Fields = %v, want null", m.Fields)
	}
	if !m.Fields.ElementType(context.Background()).Equal(FieldType) {
		t.Errorf("Fields element type = %v, want %v", m.Fields.ElementType(context.Background()), FieldType)
	}
}
