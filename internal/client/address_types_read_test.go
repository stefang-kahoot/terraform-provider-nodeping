package client

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-log/tflogtest"
)

// NodePing stores a webhook's action, and each value of its headers and query
// strings, exactly as sent, whatever its JSON type: a number, a boolean, an
// object or a list (probed 2026-10-12, #46). The client decoded them as a
// string and maps of strings, so one such address failed the read of its
// contact, and the list of every contact in the account:
//
//	failed to unmarshal response: json: cannot unmarshal number into Go
//	struct field Contact.addresses.Plain.action of type string
//
// An action that is not a string now reads as none, and a header or query
// string value as its JSON text, each with a warning in the log. Headers or
// query strings that are not an object read as none, with a warning too.

// retypedSecret is part of every value below that the log must leave out.
const retypedSecret = "4711"

// retypedValues are header or query string values of every JSON type, as
// NodePing returns them.
const retypedValues = `{"num":4711.0,"big":47110000000000000000001,"exp":4711e2,"neg":-0.4711,` +
	`"yes":true,"no":false,"obj":{"x":{"y":[4711,"z"]}},"list":["4711",1],"empty":{},` +
	`"null":null,"text":"4711"}`

// retypedText is retypedValues as read: each value as its JSON text, null as
// "" and a string as itself.
var retypedText = map[string]string{
	"num": "4711.0", "big": "47110000000000000000001", "exp": "4711e2", "neg": "-0.4711",
	"yes": "true", "no": "false", "obj": `{"x":{"y":[4711,"z"]}}`, "list": `["4711",1]`, "empty": "{}",
	"null": "", "text": "4711",
}

// retypedKeys are the keys of retypedValues that hold no string, nor null.
var retypedKeys = []string{"big", "empty", "exp", "list", "neg", "no", "num", "obj", "yes"}

// prefixed returns each key prefixed with field and a dot.
func prefixed(field string, keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = field + "." + k
	}
	return out
}

var retypedAddressTests = []struct {
	name string
	// fields are the fields of address A-BAD besides its type and address,
	// as NodePing returns them.
	fields                string
	action                string
	headers, queryStrings map[string]string
	// warnings are the warnings logged for A-BAD: the field, and the key
	// for a header or query string value.
	warnings []string
}{
	{
		name:     "an action that is a number",
		fields:   `"action":4711,"headers":{"h":"1"}`,
		headers:  map[string]string{"h": "1"},
		warnings: []string{"action"},
	},
	{
		name:     "an action that is a boolean",
		fields:   `"action":true`,
		warnings: []string{"action"},
	},
	{
		name:     "an action that is an object",
		fields:   `"action":{"method":"4711"}`,
		warnings: []string{"action"},
	},
	{
		name:     "an action that is a list",
		fields:   `"action":["4711"]`,
		warnings: []string{"action"},
	},
	{
		name:     "header values of every type",
		fields:   `"action":"post","headers":` + retypedValues,
		action:   "post",
		headers:  retypedText,
		warnings: prefixed("headers", retypedKeys),
	},
	{
		name:         "query string values of every type",
		fields:       `"action":"get","querystrings":` + retypedValues,
		action:       "get",
		queryStrings: retypedText,
		warnings:     prefixed("querystrings", retypedKeys),
	},
	{
		name:         "headers that are a string",
		fields:       `"action":"post","headers":"4711","querystrings":{"q":"1"}`,
		action:       "post",
		queryStrings: map[string]string{"q": "1"},
		warnings:     []string{"headers"},
	},
	{
		name:     "headers that are a list",
		fields:   `"headers":["4711"]`,
		warnings: []string{"headers"},
	},
	{
		name:     "query strings that are a string",
		fields:   `"querystrings":"4711"`,
		warnings: []string{"querystrings"},
	},
	{
		name:     "query strings that are a list",
		fields:   `"querystrings":["4711"]`,
		warnings: []string{"querystrings"},
	},
	{
		name:     "every field of another type at once",
		fields:   `"action":1,"headers":{"a":1,"b":true,"c":{"x":"4711"}},"querystrings":["4711"]`,
		headers:  map[string]string{"a": "1", "b": "true", "c": `{"x":"4711"}`},
		warnings: []string{"action", "headers.a", "headers.b", "headers.c", "querystrings"},
	},
}

// retypedContact is contact C-1 as NodePing returns it: address A-BAD with
// the fields given, and A-OK, an ordinary webhook.
func retypedContact(fields string) string {
	return `{"_id":"C-1","name":"On call","addresses":{` +
		`"A-BAD":{"type":"webhook","address":"https://hooks.example.com/bad","suppressup":true,` + fields + `},` +
		`"A-OK":{"type":"webhook","address":"https://hooks.example.com/ok","action":"put",` +
		`"headers":{"X-One":"1"},"querystrings":{"k":"v"}}}}`
}

// checkRetypedAddress checks A-BAD and A-OK of the contact read from
// retypedContact.
func checkRetypedAddress(t *testing.T, contact Contact, action string, headers, queryStrings map[string]string) {
	t.Helper()
	if len(contact.Addresses) != 2 {
		t.Fatalf("read %d addresses, want 2", len(contact.Addresses))
	}

	bad := contact.Addresses["A-BAD"]
	if bad.Type != "webhook" || bad.Address != "https://hooks.example.com/bad" || !bad.SuppressUp {
		t.Errorf("A-BAD's other fields read as type %q, address %q, suppressup %v", bad.Type, bad.Address, bad.SuppressUp)
	}
	if bad.Action != action {
		t.Errorf("A-BAD's action read as %q, want %q", bad.Action, action)
	}
	if !reflect.DeepEqual(bad.Headers, headers) {
		t.Errorf("A-BAD's headers read as %q, want %q", bad.Headers, headers)
	}
	if !reflect.DeepEqual(bad.QueryStrings, queryStrings) {
		t.Errorf("A-BAD's query strings read as %q, want %q", bad.QueryStrings, queryStrings)
	}

	ok := contact.Addresses["A-OK"]
	if ok.Action != "put" || !reflect.DeepEqual(ok.Headers, map[string]string{"X-One": "1"}) ||
		!reflect.DeepEqual(ok.QueryStrings, map[string]string{"k": "v"}) {
		t.Errorf("A-OK read as action %q, headers %q, query strings %q", ok.Action, ok.Headers, ok.QueryStrings)
	}
}

// checkRetypedWarnings checks that the log holds the warnings given, each for
// address A-BAD of contact C-1, and none of the values.
func checkRetypedWarnings(t *testing.T, logged *bytes.Buffer, want []string) {
	t.Helper()
	if strings.Contains(logged.String(), retypedSecret) {
		t.Errorf("the log holds a value:\n%s", logged.String())
	}

	var got []string
	for _, w := range priorityWarnings(t, logged) {
		if w["contact_id"] != "C-1" || w["contact_name"] != "On call" || w["address_id"] != "A-BAD" {
			t.Errorf("a warning names contact %v (%v) and address %v, want C-1 (On call) and A-BAD: %v",
				w["contact_id"], w["contact_name"], w["address_id"], w)
		}
		name := fmt.Sprint(w["field"])
		if key, ok := w["key"]; ok {
			name += "." + fmt.Sprint(key)
		}
		got = append(got, name)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("logged warnings for %q, want %q", got, want)
	}
}

// Every read of a contact reads an address holding fields of another type,
// and leaves the contact's other address as it is.
func TestContactReadsRetypedAddressFields(t *testing.T) {
	t.Parallel()

	reads := []struct {
		name string
		read func(ctx context.Context, c *Client) (Contact, error)
	}{
		{"GetContact", func(ctx context.Context, c *Client) (Contact, error) {
			contact, err := c.GetContact(ctx, "C-1")
			if err != nil {
				return Contact{}, err
			}
			return *contact, nil
		}},
		{"CreateContact", func(ctx context.Context, c *Client) (Contact, error) {
			contact, err := c.CreateContact(ctx, ContactCreateRequest{Name: "On call"})
			if err != nil {
				return Contact{}, err
			}
			return *contact, nil
		}},
		{"UpdateContact", func(ctx context.Context, c *Client) (Contact, error) {
			contact, err := c.UpdateContact(ctx, "C-1", ContactUpdateRequest{Name: "On call"})
			if err != nil {
				return Contact{}, err
			}
			return *contact, nil
		}},
	}

	for _, tt := range retypedAddressTests {
		for _, read := range reads {
			t.Run(tt.name+"/"+read.name, func(t *testing.T) {
				t.Parallel()

				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte(retypedContact(tt.fields)))
				}))
				t.Cleanup(server.Close)

				var logged bytes.Buffer
				ctx := tflogtest.RootLogger(context.Background(), &logged)
				c := NewClient(ClientConfig{APIToken: "test-token", BaseURL: server.URL})

				contact, err := read.read(ctx, c)
				if err != nil {
					t.Fatalf("%s: %v", read.name, err)
				}
				checkRetypedAddress(t, contact, tt.action, tt.headers, tt.queryStrings)
				checkRetypedWarnings(t, &logged, tt.warnings)
			})
		}
	}
}

// One address holding fields of another type leaves the rest of the list
// readable: its contact's other address, and every other contact.
func TestListContactsReadsRetypedAddressFields(t *testing.T) {
	t.Parallel()

	for _, tt := range retypedAddressTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"C-1":` + retypedContact(tt.fields) + `,` +
					`"C-2":{"_id":"C-2","name":"Other","addresses":{"A-2":{"type":"webhook",` +
					`"address":"https://hooks.example.com/other","action":"post","headers":{"X-Two":"2"}}}}}`))
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
			checkRetypedAddress(t, contacts["C-1"], tt.action, tt.headers, tt.queryStrings)
			other := contacts["C-2"].Addresses["A-2"]
			if other.Action != "post" || !reflect.DeepEqual(other.Headers, map[string]string{"X-Two": "2"}) {
				t.Errorf("C-2's address read as action %q, headers %q", other.Action, other.Headers)
			}
			checkRetypedWarnings(t, &logged, tt.warnings)
		})
	}
}
