package client

import (
	"encoding/json"
	"reflect"
	"testing"
)

// NodePing stores some headers as null -- {"Host": null} -- which means no
// such header. Decoded into a plain map[string]string, the null became "" and
// the check read back as sending an empty Host header.
func TestCheckParametersDropNullHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		parameters  string
		wantSend    map[string]string
		wantReceive map[string]string
	}{
		{
			name:       "a null-only map reads as no headers",
			parameters: `{"sendheaders": {"Host": null}, "receiveheaders": {"Server": null}}`,
		},
		{
			name:        "null values are dropped and real ones kept",
			parameters:  `{"sendheaders": {"Host": null, "Accept": "application/json"}, "receiveheaders": {"Server": null, "Content-Type": "text/html"}}`,
			wantSend:    map[string]string{"Accept": "application/json"},
			wantReceive: map[string]string{"Content-Type": "text/html"},
		},
		{
			// An empty string is a value, not an absence, and stays.
			name:        "an ordinary map decodes unchanged",
			parameters:  `{"sendheaders": {"Accept": "application/json", "X-Empty": ""}, "receiveheaders": {"Content-Type": "text/html"}}`,
			wantSend:    map[string]string{"Accept": "application/json", "X-Empty": ""},
			wantReceive: map[string]string{"Content-Type": "text/html"},
		},
		{
			name:       "an empty map reads as no headers",
			parameters: `{"sendheaders": {}, "receiveheaders": {}}`,
		},
		{
			name:       "a null map reads as no headers",
			parameters: `{"sendheaders": null, "receiveheaders": null}`,
		},
		{
			name:       "absent maps read as no headers",
			parameters: `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var check Check
			if err := json.Unmarshal([]byte(`{"_id": "CHK1", "parameters": `+tt.parameters+`}`), &check); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			assertHeaders(t, "sendheaders", check.Parameters.SendHeaders, tt.wantSend)
			assertHeaders(t, "receiveheaders", check.Parameters.ReceiveHeaders, tt.wantReceive)
		})
	}
}

// Anything other than a string or null is still rejected, as it was when the
// field was a plain map[string]string.
func TestCheckParametersRejectNonStringHeaders(t *testing.T) {
	t.Parallel()

	var check Check
	err := json.Unmarshal([]byte(`{"parameters": {"sendheaders": {"Host": 1}}}`), &check)
	if err == nil {
		t.Fatalf("expected an error, got sendheaders %#v", check.Parameters.SendHeaders)
	}
}

// Only reading changes. A check echoed back through the mock server or the
// client tests still encodes its headers as an ordinary object.
func TestHeaderMapEncodesAsAnObject(t *testing.T) {
	t.Parallel()

	got, err := json.Marshal(HeaderMap{"Accept": "application/json"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `{"Accept":"application/json"}`; string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func assertHeaders(t *testing.T, field string, got HeaderMap, want map[string]string) {
	t.Helper()

	if len(want) == 0 {
		if len(got) != 0 {
			t.Errorf("%s = %#v, want no headers", field, got)
		}
		return
	}
	if !reflect.DeepEqual(map[string]string(got), want) {
		t.Errorf("%s = %#v, want %#v", field, got, want)
	}
}
