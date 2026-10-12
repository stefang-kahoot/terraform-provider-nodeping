package testutil

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The mock stores whatever priority an update sends a Pushover address, the
// way NodePing did on the test SubAccount on 2026-10-12: it validates
// nothing, and keeps the priority when the update leaves it out.
func TestMockContactAddressPriorityMerge(t *testing.T) {
	t.Parallel()

	const pushover = `{"type":"pushover","address":"FAKE-pushover-user-key","priority":2}`

	tests := []struct {
		name string
		sent string
		want string
	}{
		{"a priority left out is kept", leftOut, `2`},
		{"a priority sent replaces it", `0`, `0`},
		{"a priority outside -2 to 2 is stored", `3`, `3`},
		{"a word is stored", `"abc"`, `"abc"`},
		{"a numeric string is stored", `"1"`, `"1"`},
		{"an empty string is stored", `""`, `""`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := NewMockNodePingServer()
			t.Cleanup(m.Close)

			id := createContact(t, m, `{"name":"merge","newaddresses":[`+pushover+`]}`)
			addrID := onlyAddressID(t, m, id)

			var addr map[string]interface{}
			if err := json.Unmarshal([]byte(pushover), &addr); err != nil {
				t.Fatal(err)
			}
			if tt.sent == leftOut {
				delete(addr, "priority")
			} else {
				addr["priority"] = json.RawMessage(tt.sent)
			}
			update, err := json.Marshal(map[string]interface{}{"addresses": map[string]interface{}{addrID: addr}})
			if err != nil {
				t.Fatal(err)
			}
			send(t, m, http.MethodPut, "/contacts/"+id, string(update))

			if got := storedAddressField(t, m, id, addrID, "priority"); got != tt.want {
				t.Errorf("priority sent %s: stored %s, want %s", tt.sent, got, tt.want)
			}
		})
	}
}
