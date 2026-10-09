package testutil

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"
	"testing"
)

// send makes a request to the mock and returns the body it answered with.
func send(t *testing.T, m *MockNodePingServer, method, path, body string) string {
	t.Helper()
	req, err := http.NewRequest(method, m.URL()+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s %s: status %d", method, path, resp.StatusCode)
	}
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// storedAddresses returns the contact's addresses as sorted type:address
// pairs, and whether it has an `addresses` key at all.
func storedAddresses(t *testing.T, m *MockNodePingServer, id string) ([]string, bool) {
	t.Helper()
	contact, ok := m.GetContact(id)
	if !ok {
		t.Fatalf("contact %s is not in the mock", id)
	}
	raw, present := contact["addresses"]
	addresses, _ := raw.(map[string]interface{})
	out := make([]string, 0, len(addresses))
	for _, a := range addresses {
		addr := a.(map[string]interface{})
		out = append(out, addr["type"].(string)+":"+addr["address"].(string))
	}
	sort.Strings(out)
	return out, present
}

func createContact(t *testing.T, m *MockNodePingServer, body string) string {
	t.Helper()
	var created map[string]interface{}
	if err := json.Unmarshal([]byte(send(t, m, http.MethodPost, "/contacts", body)), &created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["_id"].(string)
	if id == "" {
		t.Fatalf("create answered %v", created)
	}
	return id
}

// The mock answers contact address writes the way NodePing did when probed:
// a contact can be created without addresses, but nothing can leave one with
// none, and NodePing says so with an "error" in a 200.
func TestMockContactAddressWrites(t *testing.T) {
	t.Parallel()

	const (
		oneAddress = `{"name":"before","newaddresses":[{"type":"webhook","address":"https://hooks.example.com/one"}]}`
		emailX     = `{"type":"email","address":"x@example.com"}`
	)

	tests := []struct {
		name      string
		create    string
		update    string
		wantError bool
		wantName  string
		want      []string
	}{
		{"an empty addresses object is refused", oneAddress, `{"name":"after","addresses":{}}`, true, "before", []string{"webhook:https://hooks.example.com/one"}},
		{"an empty addresses list is refused", oneAddress, `{"name":"after","addresses":[]}`, true, "before", []string{"webhook:https://hooks.example.com/one"}},
		{"empty addresses and newaddresses are refused", oneAddress, `{"name":"after","addresses":{},"newaddresses":[]}`, true, "before", []string{"webhook:https://hooks.example.com/one"}},
		{"null addresses are ignored", oneAddress, `{"name":"after","addresses":null}`, false, "after", []string{"webhook:https://hooks.example.com/one"}},
		{"an empty addresses string is ignored", oneAddress, `{"name":"after","addresses":""}`, false, "after", []string{"webhook:https://hooks.example.com/one"}},
		{"leaving addresses out keeps them and adds new ones", oneAddress, `{"newaddresses":[` + emailX + `]}`, false, "before", []string{"email:x@example.com", "webhook:https://hooks.example.com/one"}},
		{"empty addresses with new ones replace them all", oneAddress, `{"addresses":{},"newaddresses":[` + emailX + `]}`, false, "before", []string{"email:x@example.com"}},
		{"an address-less contact can be renamed", `{"name":"before"}`, `{"name":"after"}`, false, "after", nil},
		{"an address-less contact can gain an address", `{"name":"before"}`, `{"addresses":{},"newaddresses":[` + emailX + `]}`, false, "before", []string{"email:x@example.com"}},
		{"an address-less contact refuses empty addresses", `{"name":"before"}`, `{"name":"after","addresses":{}}`, true, "before", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := NewMockNodePingServer()
			t.Cleanup(m.Close)

			id := createContact(t, m, tt.create)
			answer := send(t, m, http.MethodPut, "/contacts/"+id, tt.update)

			if gotError := answer == noAddressesError; gotError != tt.wantError {
				t.Errorf("update answered %s, want the error: %v", answer, tt.wantError)
			}
			contact, _ := m.GetContact(id)
			if contact["name"] != tt.wantName {
				t.Errorf("name is %v, want %q", contact["name"], tt.wantName)
			}
			got, present := storedAddresses(t, m, id)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("addresses are %v, want %v", got, tt.want)
			}
			if present != (len(tt.want) > 0) {
				t.Errorf("addresses key present: %v, with %d addresses", present, len(got))
			}
			if writes := m.ContactWrites(); len(writes) != 2 || writes[1].Method != http.MethodPut {
				t.Errorf("recorded %d writes, want the create and the update", len(writes))
			}
		})
	}
}

func TestMockContactCreateWithoutAddresses(t *testing.T) {
	t.Parallel()
	m := NewMockNodePingServer()
	t.Cleanup(m.Close)

	id := createContact(t, m, `{"name":"bare"}`)
	if body := send(t, m, http.MethodGet, "/contacts/"+id, ""); strings.Contains(body, `"addresses"`) {
		t.Errorf("an address-less contact reads back with an addresses key: %s", body)
	}

	if answer := send(t, m, http.MethodPost, "/contacts", `{"name":"empty","newaddresses":[]}`); answer != noAddressesError {
		t.Errorf("a create with empty newaddresses answered %s", answer)
	}
	var all map[string]interface{}
	if err := json.Unmarshal([]byte(send(t, m, http.MethodGet, "/contacts", "")), &all); err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("the mock holds %d contacts, want only the address-less one", len(all))
	}
}

// The mock keeps each object in the account whose customerid created it, and
// answers a request for it from any other account the way NodePing answers an
// ID it does not have.
func TestMockKeepsObjectsInTheirAccount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		create  string
		missing string // the answer to a read of an ID NodePing does not have
		gone    string // the answer to a delete of one
	}{
		{"check", "/checks", `{"type":"HTTP","target":"https://example.com"}`, `{"error":"Error fetching check."}`, `{"error":"Unable to find that check"}`},
		{"contact", "/contacts", `{"name":"sub"}`, `{}`, `{"error":"Unable to find that contact"}`},
		{"contact group", "/contactgroups", `{"name":"sub"}`, `{}`, `{"error":"Unable to find group"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := NewMockNodePingServer()
			t.Cleanup(m.Close)

			var created map[string]interface{}
			if err := json.Unmarshal([]byte(send(t, m, http.MethodPost, tt.path+"?customerid=SUB", tt.create)), &created); err != nil {
				t.Fatal(err)
			}
			id, _ := created["_id"].(string)
			if id == "" {
				t.Fatalf("create answered %v", created)
			}
			if created["customer_id"] != "SUB" {
				t.Errorf("customer_id is %v, want the SubAccount", created["customer_id"])
			}
			item := tt.path + "/" + id

			if got := send(t, m, http.MethodGet, item, ""); got != tt.missing {
				t.Errorf("the parent account read %s", got)
			}
			if got := send(t, m, http.MethodGet, item+"?customerid=OTHER", ""); got != tt.missing {
				t.Errorf("another SubAccount read %s", got)
			}
			if got := send(t, m, http.MethodGet, item+"?customerid=SUB", ""); !strings.Contains(got, id) {
				t.Errorf("the SubAccount read %s", got)
			}
			if got := send(t, m, http.MethodGet, tt.path, ""); got != "{}" {
				t.Errorf("the parent account listed %s", got)
			}
			if got := send(t, m, http.MethodGet, tt.path+"?customerid=SUB", ""); !strings.Contains(got, id) {
				t.Errorf("the SubAccount listed %s", got)
			}
			if got := send(t, m, http.MethodDelete, item, ""); got != tt.gone {
				t.Errorf("the parent account's delete answered %s", got)
			}
			if got := send(t, m, http.MethodDelete, item+"?customerid=SUB", ""); strings.Contains(got, "error") {
				t.Errorf("the SubAccount's delete answered %s", got)
			}

			want := []Request{
				{http.MethodPost, tt.path, "SUB"},
				{http.MethodGet, item, ""},
				{http.MethodGet, item, "OTHER"},
				{http.MethodGet, item, "SUB"},
				{http.MethodGet, tt.path, ""},
				{http.MethodGet, tt.path, "SUB"},
				{http.MethodDelete, item, ""},
				{http.MethodDelete, item, "SUB"},
			}
			if got := m.Requests(); !slices.Equal(got, want) {
				t.Errorf("recorded requests\n%v\nwant\n%v", got, want)
			}
		})
	}
}

// AddCheck seeds the parent account, and SetAccount moves an object between
// accounts.
func TestMockSetAccount(t *testing.T) {
	t.Parallel()
	m := NewMockNodePingServer()
	t.Cleanup(m.Close)

	m.AddCheck("SEEDED", map[string]interface{}{"_id": "SEEDED", "type": "HTTP"})
	const missing = `{"error":"Error fetching check."}`

	m.SetAccount("SEEDED", "SUB")
	if got := send(t, m, http.MethodGet, "/checks/SEEDED", ""); got != missing {
		t.Errorf("the parent account read a SubAccount's check: %s", got)
	}
	if got := send(t, m, http.MethodGet, "/checks/SEEDED?customerid=SUB", ""); !strings.Contains(got, "SEEDED") {
		t.Errorf("the SubAccount read %s", got)
	}

	m.SetAccount("SEEDED", "")
	if got := send(t, m, http.MethodGet, "/checks/SEEDED", ""); !strings.Contains(got, "SEEDED") {
		t.Errorf("the parent account read %s", got)
	}
	if got := send(t, m, http.MethodGet, "/checks/SEEDED?customerid=SUB", ""); got != missing {
		t.Errorf("the SubAccount read the parent account's check: %s", got)
	}
}
