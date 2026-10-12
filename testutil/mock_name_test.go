package testutil

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The mock keeps a contact's and a contact group's name the way NodePing did
// when probed on the test SubAccount on 2026-10-12: nothing an update sends
// clears a name, and the two disagree on what a create without one stores.

// storedName returns the name an object holds, and whether it holds the key.
type storedName func(m *MockNodePingServer, id string) (interface{}, bool)

func contactName(m *MockNodePingServer, id string) (interface{}, bool) {
	contact, _ := m.GetContact(id)
	name, ok := contact["name"]
	return name, ok
}

func contactGroupName(m *MockNodePingServer, id string) (interface{}, bool) {
	group, _ := m.GetContactGroup(id)
	name, ok := group["name"]
	return name, ok
}

func createContactGroup(t *testing.T, m *MockNodePingServer, body string) string {
	t.Helper()
	var created map[string]interface{}
	if err := json.Unmarshal([]byte(send(t, m, http.MethodPost, "/contactgroups", body)), &created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["_id"].(string)
	if id == "" {
		t.Fatalf("create answered %v", created)
	}
	return id
}

type nameCase struct {
	desc string
	body string
	want interface{}
	held bool // whether the object holds a name key at all
}

func TestMockContactName(t *testing.T) {
	t.Parallel()

	// A contact created without a name, or with "" or null, holds no name
	// key.
	creates := []nameCase{
		{"no name", `{}`, nil, false},
		{"empty name", `{"name":""}`, nil, false},
		{"null name", `{"name":null}`, nil, false},
		{"a name", `{"name":"x"}`, "x", true},
	}
	// An update keeps the name unless it sends a non-empty string; " " is
	// stored.
	updates := []nameCase{
		{"no name", `{}`, "before", true},
		{"empty name", `{"name":""}`, "before", true},
		{"null name", `{"name":null}`, "before", true},
		{"false", `{"name":false}`, "before", true},
		{"a space", `{"name":" "}`, " ", true},
		{"a new name", `{"name":"after"}`, "after", true},
	}

	testMockName(t, creates, updates, "/contacts/", contactName, func(t *testing.T, m *MockNodePingServer, body string) string {
		return createContact(t, m, body)
	})
}

func TestMockContactGroupName(t *testing.T) {
	t.Parallel()

	// A group created without a name holds no name key; "" and null are
	// stored as sent.
	creates := []nameCase{
		{"no name", `{}`, nil, false},
		{"empty name", `{"name":""}`, "", true},
		{"null name", `{"name":null}`, nil, true},
		{"a name", `{"name":"x"}`, "x", true},
	}
	// An update keeps the name unless it sends a string that is not blank.
	updates := []nameCase{
		{"no name", `{"members":[]}`, "before", true},
		{"empty name", `{"name":"","members":[]}`, "before", true},
		{"null name", `{"name":null,"members":[]}`, "before", true},
		{"a space", `{"name":" ","members":[]}`, "before", true},
		{"false", `{"name":false,"members":[]}`, "before", true},
		{"zero", `{"name":0,"members":[]}`, "before", true},
		{"a new name", `{"name":"after","members":[]}`, "after", true},
	}

	testMockName(t, creates, updates, "/contactgroups/", contactGroupName, createContactGroup)
}

// testMockName creates an object from each of creates and checks the name it
// holds, then creates one named "before" for each of updates, sends the
// update to path+id, and checks the name it holds after.
func testMockName(t *testing.T, creates, updates []nameCase, path string, name storedName, create func(*testing.T, *MockNodePingServer, string) string) {
	t.Helper()

	check := func(t *testing.T, m *MockNodePingServer, id string, tt nameCase) {
		t.Helper()
		got, held := name(m, id)
		if held != tt.held || got != tt.want {
			t.Errorf("holds the name %#v (key present: %v), want %#v (key present: %v)", got, held, tt.want, tt.held)
		}
	}

	for _, tt := range creates {
		t.Run("create with "+tt.desc, func(t *testing.T) {
			t.Parallel()
			m := NewMockNodePingServer()
			t.Cleanup(m.Close)

			check(t, m, create(t, m, tt.body), tt)
		})
	}

	for _, tt := range updates {
		t.Run("update with "+tt.desc, func(t *testing.T) {
			t.Parallel()
			m := NewMockNodePingServer()
			t.Cleanup(m.Close)

			id := create(t, m, `{"name":"before"}`)
			var answer map[string]interface{}
			if err := json.Unmarshal([]byte(send(t, m, http.MethodPut, path+id, tt.body)), &answer); err != nil {
				t.Fatal(err)
			}
			if answer["name"] != tt.want {
				t.Errorf("the update answered the name %#v, want %#v", answer["name"], tt.want)
			}
			check(t, m, id, tt)
		})
	}
}
