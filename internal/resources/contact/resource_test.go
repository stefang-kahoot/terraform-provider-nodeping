package contact

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
)

func TestMapAddressesToModelEmpty(t *testing.T) {
	t.Parallel()

	var diags diag.Diagnostics
	got := mapAddressesToModel(context.Background(), map[string]client.ContactAddress{}, nil, &diags)

	if got != nil {
		t.Errorf("expected nil for an empty address map, got %#v", got)
	}
	if diags.HasError() {
		t.Errorf("unexpected diagnostics: %+v", diags)
	}
}

func TestMapAddressesToModelBasicFields(t *testing.T) {
	t.Parallel()

	var diags diag.Diagnostics
	in := map[string]client.ContactAddress{
		"AD1": {
			Address:      "ops@example.com",
			Type:         "email",
			SuppressUp:   true,
			SuppressDown: false,
		},
	}

	got := mapAddressesToModel(context.Background(), in, nil, &diags)

	if len(got) != 1 {
		t.Fatalf("expected 1 address, got %d", len(got))
	}
	a := got[0]

	if a.ID.ValueString() != "AD1" {
		t.Errorf("ID = %q, want %q", a.ID.ValueString(), "AD1")
	}
	if a.Address.ValueString() != "ops@example.com" {
		t.Errorf("Address = %q", a.Address.ValueString())
	}
	if a.Type.ValueString() != "email" {
		t.Errorf("Type = %q", a.Type.ValueString())
	}
	if !a.SuppressUp.ValueBool() {
		t.Error("SuppressUp should be true")
	}
	if a.SuppressDown.ValueBool() {
		t.Error("SuppressDown should be false")
	}
	// An absent action must map to null, not to an empty string, so Terraform
	// does not see a phantom diff against an unset attribute.
	if !a.Action.IsNull() {
		t.Errorf("Action should be null when the API sends no action, got %v", a.Action)
	}
	if !a.Data.IsNull() {
		t.Errorf("Data should be null when the API sends no data, got %v", a.Data)
	}
	if !a.Priority.IsNull() {
		t.Errorf("Priority should be null when the API sends none, got %v", a.Priority)
	}
	if !a.Headers.IsNull() {
		t.Errorf("Headers should be null when empty, got %v", a.Headers)
	}
	if !a.QueryStrings.IsNull() {
		t.Errorf("QueryStrings should be null when empty, got %v", a.QueryStrings)
	}
}

// The NodePing API returns `mute` as either a boolean or a numeric timestamp.
func TestMapAddressesToModelMute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mute json.RawMessage
		want bool
	}{
		{name: "absent", mute: nil, want: false},
		{name: "bool true", mute: json.RawMessage(`true`), want: true},
		{name: "bool false", mute: json.RawMessage(`false`), want: false},
		{name: "positive number means muted", mute: json.RawMessage(`1699999999`), want: true},
		{name: "zero means not muted", mute: json.RawMessage(`0`), want: false},
		{name: "malformed json leaves the default", mute: json.RawMessage(`{`), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var diags diag.Diagnostics
			in := map[string]client.ContactAddress{
				"AD1": {Address: "a@example.com", Type: "email", Mute: tt.mute},
			}

			got := mapAddressesToModel(context.Background(), in, nil, &diags)
			if len(got) != 1 {
				t.Fatalf("expected 1 address, got %d", len(got))
			}
			if got[0].Mute.ValueBool() != tt.want {
				t.Errorf("Mute = %v, want %v", got[0].Mute.ValueBool(), tt.want)
			}
		})
	}
}

// NodePing returns `data` exactly as sent, and the provider sends a string,
// so a string reads as it is, JSON or not, byte for byte (#40). It used to be
// read as compact JSON, its keys sorted, and a body written any other way
// failed the apply. An object, which only a write outside Terraform can
// leave, reads as compact JSON.
func TestMapAddressesToModelData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		data     interface{}
		wantNull bool
		want     string
	}{
		{
			name: "compact JSON is read as returned",
			data: `{"a":1}`,
			want: `{"a":1}`,
		},
		{
			name: "JSON with spaces and a newline is read as returned",
			data: "{ \"a\": 1,\n  \"b\": [1, 2] }",
			want: "{ \"a\": 1,\n  \"b\": [1, 2] }",
		},
		{
			name: "keys out of order stay in their order",
			data: `{"b":1,"a":2}`,
			want: `{"b":1,"a":2}`,
		},
		{
			name: "numbers stay as written",
			data: `{"a":1.0,"b":1e2,"id":12345678901234567890}`,
			want: `{"a":1.0,"b":1e2,"id":12345678901234567890}`,
		},
		{
			name: "HTML characters are not escaped",
			data: `{"text":"<b>{label}</b> & {event}"}`,
			want: `{"text":"<b>{label}</b> & {event}"}`,
		},
		{
			name: "duplicate keys stay",
			data: `{"a":1,"a":2}`,
			want: `{"a":1,"a":2}`,
		},
		{
			name: "a trailing newline stays",
			data: "{\"a\":1}\n",
			want: "{\"a\":1}\n",
		},
		{
			name:     "empty string becomes null",
			data:     "",
			wantNull: true,
		},
		{
			name:     "absent becomes null",
			data:     nil,
			wantNull: true,
		},
		{
			name: "object is marshalled to compact json",
			data: map[string]interface{}{"b": float64(1), "a": "x"},
			want: `{"a":"x","b":1}`,
		},
		{
			name: "array is marshalled to compact json",
			data: []interface{}{float64(1), "x"},
			want: `[1,"x"]`,
		},
		{
			name: "non-json string passes through",
			data: "plain text",
			want: "plain text",
		},
		{
			name: "whitespace passes through",
			data: " ",
			want: " ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var diags diag.Diagnostics
			in := map[string]client.ContactAddress{
				"AD1": {Address: "https://example.com/hook", Type: "webhook", Data: tt.data},
			}

			got := mapAddressesToModel(context.Background(), in, nil, &diags)
			if len(got) != 1 {
				t.Fatalf("expected 1 address, got %d", len(got))
			}

			if tt.wantNull {
				if !got[0].Data.IsNull() {
					t.Errorf("Data should be null, got %v", got[0].Data)
				}
				return
			}
			if got[0].Data.ValueString() != tt.want {
				t.Errorf("Data = %q, want %q", got[0].Data.ValueString(), tt.want)
			}
		})
	}
}

// An object body is read with <, > and & as they are, not escaped as
// \u003c, \u003e and \u0026: the next update writes the body back as read.
// The JSON ends without the newline an encoder adds.
func TestReadDataObjectKeepsHTMLCharacters(t *testing.T) {
	t.Parallel()

	got := readData(map[string]interface{}{"text": "<b>{label}</b> & {event}", "n": float64(1)})
	if want := `{"n":1,"text":"<b>{label}</b> & {event}"}`; got.ValueString() != want {
		t.Errorf("data read as %q, want %q", got.ValueString(), want)
	}
	got = readData([]interface{}{"a>b"})
	if want := `["a>b"]`; got.ValueString() != want {
		t.Errorf("data read as %q, want %q", got.ValueString(), want)
	}
}

func TestMapAddressesToModelHeadersAndPriority(t *testing.T) {
	t.Parallel()

	prio := 2
	var diags diag.Diagnostics
	in := map[string]client.ContactAddress{
		"AD1": {
			Address:      "https://example.com/hook",
			Type:         "webhook",
			Action:       "post",
			Headers:      map[string]string{"content-type": "application/json"},
			QueryStrings: map[string]string{"key": "1"},
			Priority:     &prio,
		},
	}

	got := mapAddressesToModel(context.Background(), in, nil, &diags)
	if len(got) != 1 {
		t.Fatalf("expected 1 address, got %d", len(got))
	}
	a := got[0]

	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}
	if a.Action.ValueString() != "post" {
		t.Errorf("Action = %q, want %q", a.Action.ValueString(), "post")
	}
	if a.Priority.ValueInt64() != 2 {
		t.Errorf("Priority = %d, want 2", a.Priority.ValueInt64())
	}
	if a.Headers.IsNull() {
		t.Fatal("Headers should not be null")
	}
	if a.QueryStrings.IsNull() {
		t.Fatal("QueryStrings should not be null")
	}

	var headers map[string]string
	if d := a.Headers.ElementsAs(context.Background(), &headers, false); d.HasError() {
		t.Fatalf("failed to read headers: %+v", d)
	}
	if headers["content-type"] != "application/json" {
		t.Errorf("headers = %#v", headers)
	}
}

// Every address the API returns must appear in the model, regardless of how
// many there are; map iteration order must not drop entries.
func TestMapAddressesToModelPreservesAllEntries(t *testing.T) {
	t.Parallel()

	var diags diag.Diagnostics
	in := map[string]client.ContactAddress{
		"AD1": {Address: "a@example.com", Type: "email"},
		"AD2": {Address: "b@example.com", Type: "email"},
		"AD3": {Address: "https://example.com/h", Type: "webhook"},
	}

	got := mapAddressesToModel(context.Background(), in, nil, &diags)

	if len(got) != len(in) {
		t.Fatalf("expected %d addresses, got %d", len(in), len(got))
	}

	seen := make(map[string]bool, len(got))
	for _, a := range got {
		seen[a.ID.ValueString()] = true
	}
	for id := range in {
		if !seen[id] {
			t.Errorf("address %q is missing from the result", id)
		}
	}
}

func TestNewContactResourceMetadata(t *testing.T) {
	t.Parallel()

	resp := &fwresource.MetadataResponse{}
	NewContactResource().Metadata(context.Background(), fwresource.MetadataRequest{ProviderTypeName: "nodeping"}, resp)

	if want := "nodeping_contact"; resp.TypeName != want {
		t.Errorf("TypeName = %q, want %q", resp.TypeName, want)
	}
}
