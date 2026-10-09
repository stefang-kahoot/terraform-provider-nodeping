package testutil

import (
	"encoding/json"
	"net/http"
	"testing"
)

// leftOut, as a test's sent value, leaves the key out of the update.
const leftOut = "<left out>"

// The mock merges an update into a contact's address field by field, the way
// NodePing did for probes/probe_clear.py and probe_clear2.py on the test
// SubAccount: each update sends the address the contact was created with,
// with one key changed or left out, and the test reads back what is stored.
func TestMockContactAddressMerge(t *testing.T) {
	t.Parallel()

	const hook = `{"type":"webhook","address":"https://hooks.example.com/one","action":"post","data":"probe-body",` +
		`"headers":{"X-One":"1","X-Two":"2"},"querystrings":{"q1":"1","q2":"2"},"suppressup":true}`

	tests := []struct {
		name string
		key  string
		sent string
		want string
	}{
		{"a suppress flag left out stays true", "suppressup", leftOut, `true`},
		{"a suppress flag sent false is cleared", "suppressup", `false`, `false`},

		{"headers are replaced by a smaller map", "headers", `{"X-One":"1"}`, `{"X-One":"1"}`},
		{"a header sent as null is stored as null", "headers", `{"X-One":"1","X-Two":null}`, `{"X-One":"1","X-Two":null}`},
		{"headers sent as {} are cleared", "headers", `{}`, `{}`},
		{"headers sent as null are kept", "headers", `null`, `{"X-One":"1","X-Two":"2"}`},
		{"headers left out are kept", "headers", leftOut, `{"X-One":"1","X-Two":"2"}`},

		{"query strings are replaced by a smaller map", "querystrings", `{"q1":"1"}`, `{"q1":"1"}`},
		{"query strings sent as {} are cleared", "querystrings", `{}`, `{}`},
		{"query strings sent as null are kept", "querystrings", `null`, `{"q1":"1","q2":"2"}`},

		{"data left out is kept", "data", leftOut, `"probe-body"`},
		{"data sent as an empty string is kept", "data", `""`, `"probe-body"`},
		{"data sent as null is kept", "data", `null`, `"probe-body"`},
		{"data sent as false is kept", "data", `false`, `"probe-body"`},
		{"data sent as 0 is kept", "data", `0`, `"probe-body"`},
		{"data sent as a space replaces it", "data", `" "`, `" "`},
		{"data sent as {} replaces it", "data", `{}`, `{}`},

		{"an action sent replaces it", "action", `"get"`, `"get"`},
		{"an action left out is kept", "action", leftOut, `"post"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := NewMockNodePingServer()
			t.Cleanup(m.Close)

			id := createContact(t, m, `{"name":"merge","newaddresses":[`+hook+`]}`)
			addrID := onlyAddressID(t, m, id)

			var addr map[string]interface{}
			if err := json.Unmarshal([]byte(hook), &addr); err != nil {
				t.Fatal(err)
			}
			if tt.sent == leftOut {
				delete(addr, tt.key)
			} else {
				addr[tt.key] = json.RawMessage(tt.sent)
			}
			update, err := json.Marshal(map[string]interface{}{"addresses": map[string]interface{}{addrID: addr}})
			if err != nil {
				t.Fatal(err)
			}
			send(t, m, http.MethodPut, "/contacts/"+id, string(update))

			if got := storedAddressField(t, m, id, addrID, tt.key); got != tt.want {
				t.Errorf("%s sent %s: stored %s, want %s", tt.key, tt.sent, got, tt.want)
			}
			if got := storedAddressField(t, m, id, addrID, "address"); got != `"https://hooks.example.com/one"` {
				t.Errorf("the address is stored as %s", got)
			}
		})
	}
}

// onlyAddressID returns the ID of a contact's only address.
func onlyAddressID(t *testing.T, m *MockNodePingServer, id string) string {
	t.Helper()
	contact, _ := m.GetContact(id)
	addresses, _ := contact["addresses"].(map[string]interface{})
	if len(addresses) != 1 {
		t.Fatalf("contact %s has %d addresses, want 1", id, len(addresses))
	}
	for addrID := range addresses {
		return addrID
	}
	return ""
}

// storedAddressField returns one field of a stored address as JSON, or
// leftOut if the address has no such field.
func storedAddressField(t *testing.T, m *MockNodePingServer, id, addrID, key string) string {
	t.Helper()
	contact, _ := m.GetContact(id)
	addresses, _ := contact["addresses"].(map[string]interface{})
	addr, ok := addresses[addrID].(map[string]interface{})
	if !ok {
		t.Fatalf("contact %s has no address %s", id, addrID)
	}
	v, ok := addr[key]
	if !ok {
		return leftOut
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
