package testutil

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The mock holds an address's data exactly as sent and answers with it, on
// create and on update, as NodePing did when probed on 2026-10-12: a string
// byte for byte, JSON or not, and an object as an object.
func TestMockHoldsDataAsSent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sent string // as JSON
	}{
		{"compact JSON", `"{\"a\":1}"`},
		{"JSON with spaces and a newline", `"{ \"a\": 1,\n  \"b\": [1, 2] }"`},
		{"keys out of order", `"{\"b\":1,\"a\":2}"`},
		{"not JSON", `"hello"`},
		{"an object", `{"a":1}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			address := func(data string) string {
				return `{"type":"webhook","address":"https://hooks.example.com/one","data":` + data + `}`
			}

			m := NewMockNodePingServer()
			t.Cleanup(m.Close)
			answer := send(t, m, http.MethodPost, "/contacts", `{"name":"data","newaddresses":[`+address(tt.sent)+`]}`)
			id := createdID(t, answer)
			addrID := onlyAddressID(t, m, id)
			if got := answeredData(t, answer, addrID); got != tt.sent {
				t.Errorf("create answered data %s, want %s", got, tt.sent)
			}
			if got := answeredData(t, send(t, m, http.MethodGet, "/contacts/"+id, ""), addrID); got != tt.sent {
				t.Errorf("a read after the create answered data %s, want %s", got, tt.sent)
			}

			answer = send(t, m, http.MethodPut, "/contacts/"+id, `{"addresses":{"`+addrID+`":`+address(`"before"`)+`}}`)
			if got := answeredData(t, answer, addrID); got != `"before"` {
				t.Fatalf("update answered data %s, want \"before\"", got)
			}
			answer = send(t, m, http.MethodPut, "/contacts/"+id, `{"addresses":{"`+addrID+`":`+address(tt.sent)+`}}`)
			if got := answeredData(t, answer, addrID); got != tt.sent {
				t.Errorf("update answered data %s, want %s", got, tt.sent)
			}
		})
	}
}

// SetContactAddressData changes what an address holds, and nothing else.
func TestMockSetContactAddressData(t *testing.T) {
	t.Parallel()
	m := NewMockNodePingServer()
	t.Cleanup(m.Close)

	id := createContact(t, m, `{"name":"data","newaddresses":[{"type":"webhook","address":"https://hooks.example.com/one","action":"post","data":"before"}]}`)
	addrID := onlyAddressID(t, m, id)
	m.SetContactAddressData(id, addrID, map[string]interface{}{"text": "after"})

	if got := storedAddressField(t, m, id, addrID, "data"); got != `{"text":"after"}` {
		t.Errorf("data = %s, want {\"text\":\"after\"}", got)
	}
	if got := storedAddressField(t, m, id, addrID, "action"); got != `"post"` {
		t.Errorf("action = %s, want \"post\"", got)
	}
}

func createdID(t *testing.T, answer string) string {
	t.Helper()
	var contact struct {
		ID string `json:"_id"`
	}
	if err := json.Unmarshal([]byte(answer), &contact); err != nil || contact.ID == "" {
		t.Fatalf("no contact ID in the answer: %v", err)
	}
	return contact.ID
}

// answeredData returns an address's data in a contact answer, as JSON.
func answeredData(t *testing.T, answer, addrID string) string {
	t.Helper()
	var contact struct {
		Addresses map[string]map[string]json.RawMessage `json:"addresses"`
	}
	if err := json.Unmarshal([]byte(answer), &contact); err != nil {
		t.Fatal(err)
	}
	return string(contact.Addresses[addrID]["data"])
}
