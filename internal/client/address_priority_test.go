package client

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-log/tflogtest"
)

// NodePing stores whatever priority a create or update sends, unvalidated:
// "", "abc", "1", -3 and 3 alike. Decoded as a number, one address holding a
// string failed the read of its contact, and the list of every contact in the
// account.
func TestContactAddressPriority(t *testing.T) {
	t.Parallel()

	const none = "none"

	tests := []struct {
		name       string
		priority   string // as NodePing returns it; "" leaves the key out
		want       string // the priority read, or none
		unreadable bool
	}{
		{"a number", `2`, "2", false},
		{"a negative number", `-2`, "-2", false},
		{"zero", `0`, "0", false},
		{"a number outside Pushover's range", `3`, "3", false},
		{"a numeric string", `"1"`, "1", false},
		{"a negative numeric string", `"-2"`, "-2", false},
		{"no priority", ``, none, false},
		{"null, which an update that clears it leaves", `null`, none, false},
		{"an empty string", `""`, none, false},
		{"a word", `"abc"`, none, true},
		{"a number with spaces", `" 1"`, none, true},
		{"a fraction", `1.5`, none, true},
		{"a fraction as a string", `"1.5"`, none, true},
		{"a boolean", `true`, none, true},
		{"an object", `{}`, none, true},
		{"a list", `[1]`, none, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := `{"address":"FAKE-user-key","type":"pushover","action":"get","mute":true,"headers":{"X-One":"1"},"data":{"a":1}`
			if tt.priority != "" {
				body += `,"priority":` + tt.priority
			}
			body += `}`

			var addr ContactAddress
			if err := json.Unmarshal([]byte(body), &addr); err != nil {
				t.Fatalf("decoding %s: %v", body, err)
			}

			got := none
			if addr.Priority != nil {
				got = strconv.Itoa(*addr.Priority)
			}
			if got != tt.want {
				t.Errorf("priority %s read as %s, want %s", tt.priority, got, tt.want)
			}
			if addr.unreadablePriority != tt.unreadable {
				t.Errorf("priority %s: unreadable = %v, want %v", tt.priority, addr.unreadablePriority, tt.unreadable)
			}

			// The other fields decode as they did.
			data, _ := addr.Data.(map[string]interface{})
			if addr.Address != "FAKE-user-key" || addr.Type != "pushover" || addr.Action != "get" ||
				string(addr.Mute) != "true" || addr.Headers["X-One"] != "1" || data["a"] != float64(1) {
				t.Errorf("the other fields decoded as %+v", addr)
			}
		})
	}
}

// Decoding into an address that already holds a priority replaces it, as it
// did when Priority was a plain field.
func TestContactAddressPriorityReplacesHeldOne(t *testing.T) {
	t.Parallel()

	addr := ContactAddress{Type: "pushover"}
	for _, body := range []string{`{"priority":"abc"}`, `{"priority":2}`} {
		if err := json.Unmarshal([]byte(body), &addr); err != nil {
			t.Fatal(err)
		}
	}
	if addr.Priority == nil || *addr.Priority != 2 || addr.unreadablePriority {
		t.Errorf("priority read as %v, unreadable %v; want 2, false", addr.Priority, addr.unreadablePriority)
	}
	if addr.Type != "pushover" {
		t.Errorf("type = %q, want it kept", addr.Type)
	}
}

// unreadablePriorityContact is a contact as NodePing returns it with a
// Pushover address holding "abc", one holding a numeric string, and an email
// address.
const unreadablePriorityContact = `{"_id":"C-1","name":"On call","addresses":{` +
	`"A-BAD":{"type":"pushover","address":"FAKE-user-key-1","priority":"abc"},` +
	`"A-NUM":{"type":"pushover","address":"FAKE-user-key-2","priority":"1"},` +
	`"A-EMAIL":{"type":"email","address":"a@example.com"}}}`

// priorityWarnings returns the warnings logged, each as its fields.
func priorityWarnings(t *testing.T, logged *bytes.Buffer) []map[string]interface{} {
	t.Helper()
	entries, err := tflogtest.MultilineJSONDecode(logged)
	if err != nil {
		t.Fatalf("decoding the log: %v", err)
	}
	var warnings []map[string]interface{}
	for _, entry := range entries {
		if entry["@level"] == "warn" {
			warnings = append(warnings, entry)
		}
	}
	return warnings
}

// checkPriorityWarning checks that the log holds one warning, for address
// A-BAD of contact C-1, and not the value it holds.
func checkPriorityWarning(t *testing.T, logged *bytes.Buffer) {
	t.Helper()
	if strings.Contains(logged.String(), "abc") {
		t.Errorf("the log holds the priority's value:\n%s", logged.String())
	}
	warnings := priorityWarnings(t, logged)
	if len(warnings) != 1 {
		t.Fatalf("logged %d warnings, want 1: %v", len(warnings), warnings)
	}
	w := warnings[0]
	if w["contact_id"] != "C-1" || w["contact_name"] != "On call" || w["address_id"] != "A-BAD" {
		t.Errorf("the warning names contact %v (%v) and address %v, want C-1 (On call) and A-BAD", w["contact_id"], w["contact_name"], w["address_id"])
	}
}

// checkReadPriorities checks the addresses of the contact read from
// unreadablePriorityContact.
func checkReadPriorities(t *testing.T, contact Contact) {
	t.Helper()
	if len(contact.Addresses) != 3 {
		t.Fatalf("read %d addresses, want 3", len(contact.Addresses))
	}
	if p := contact.Addresses["A-BAD"].Priority; p != nil {
		t.Errorf("A-BAD's priority read as %d, want none", *p)
	}
	if p := contact.Addresses["A-NUM"].Priority; p == nil || *p != 1 {
		t.Errorf("A-NUM's priority read as %v, want 1", p)
	}
}

func TestGetContactReadsUnreadablePriority(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(unreadablePriorityContact))
	}))
	t.Cleanup(server.Close)

	var logged bytes.Buffer
	ctx := tflogtest.RootLogger(context.Background(), &logged)
	c := NewClient(ClientConfig{APIToken: "test-token", BaseURL: server.URL})

	contact, err := c.GetContact(ctx, "C-1")
	if err != nil {
		t.Fatalf("GetContact: %v", err)
	}
	checkReadPriorities(t, *contact)
	checkPriorityWarning(t, &logged)
}

// One contact's unreadable priority leaves the rest of the list readable.
func TestListContactsReadsUnreadablePriority(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"C-1":` + unreadablePriorityContact + `,` +
			`"C-2":{"_id":"C-2","addresses":{"A-OK":{"type":"pushover","address":"FAKE-user-key-3","priority":-2}}}}`))
	}))
	t.Cleanup(server.Close)

	var logged bytes.Buffer
	ctx := tflogtest.RootLogger(context.Background(), &logged)
	c := NewClient(ClientConfig{APIToken: "test-token", BaseURL: server.URL})

	contacts, err := c.ListContacts(ctx)
	if err != nil {
		t.Fatalf("ListContacts: %v", err)
	}
	if len(contacts) != 2 {
		t.Fatalf("listed %d contacts, want 2", len(contacts))
	}
	checkReadPriorities(t, contacts["C-1"])
	if p := contacts["C-2"].Addresses["A-OK"].Priority; p == nil || *p != -2 {
		t.Errorf("A-OK's priority read as %v, want -2", p)
	}
	checkPriorityWarning(t, &logged)
}

// An update's answer holds every address the contact keeps, including one
// whose priority was set outside Terraform.
func TestUpdateContactReadsUnreadablePriority(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(unreadablePriorityContact))
	}))
	t.Cleanup(server.Close)

	var logged bytes.Buffer
	ctx := tflogtest.RootLogger(context.Background(), &logged)
	c := NewClient(ClientConfig{APIToken: "test-token", BaseURL: server.URL})

	contact, err := c.UpdateContact(ctx, "C-1", ContactUpdateRequest{Name: "On call"})
	if err != nil {
		t.Fatalf("UpdateContact: %v", err)
	}
	checkReadPriorities(t, *contact)
	checkPriorityWarning(t, &logged)
}
