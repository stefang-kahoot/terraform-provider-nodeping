package testutil

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The mock keeps a webhook's action the way NodePing did on the test
// SubAccount on 2026-10-12: an update that leaves it out, or sends "", null,
// false or 0, keeps it. Any other string is stored as sent, unvalidated.
func TestMockContactAddressActionMerge(t *testing.T) {
	t.Parallel()

	const hook = `{"type":"webhook","address":"https://hooks.example.com/one","action":"post"}`

	tests := []struct {
		name string
		sent string
		want string
	}{
		{"an action left out is kept", leftOut, `"post"`},
		{"an action sent as an empty string is kept", `""`, `"post"`},
		{"an action sent as null is kept", `null`, `"post"`},
		{"an action sent as false is kept", `false`, `"post"`},
		{"an action sent as 0 is kept", `0`, `"post"`},
		{"an action sent replaces it", `"get"`, `"get"`},
		{"a space is stored", `" "`, `" "`},
		{"any string is stored", `"bogus"`, `"bogus"`},
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
				delete(addr, "action")
			} else {
				addr["action"] = json.RawMessage(tt.sent)
			}
			update, err := json.Marshal(map[string]interface{}{"addresses": map[string]interface{}{addrID: addr}})
			if err != nil {
				t.Fatal(err)
			}
			send(t, m, http.MethodPut, "/contacts/"+id, string(update))

			if got := storedAddressField(t, m, id, addrID, "action"); got != tt.want {
				t.Errorf("action sent %s: stored %s, want %s", tt.sent, got, tt.want)
			}
		})
	}
}
