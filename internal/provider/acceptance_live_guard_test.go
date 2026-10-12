package provider_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// The TestAccLive tests write to whatever account their token opens, so a
// production token passed by mistake would have them create and delete
// objects next to real checks. checkLiveAccount is what stops that: before
// anything is written, it lists the account and refuses it unless every check,
// contact and contact group in it belongs to the customer ID the tests were
// given. NodePing prefixes every such ID with the customer ID of the account
// that holds it, and most records also carry that customer ID as customer_id.
//
// Schedules are left out: their IDs are names, such as "Weekends".
//
// An empty account passes trivially. The live tests therefore also check the
// ID of everything they create; see liveAccount.owns.

// checkLiveAccount returns an error unless every check, contact and contact
// group that c lists belongs to the account customerID. A failed list is an
// error as well: an account that cannot be listed is not known to be the
// right one.
func checkLiveAccount(ctx context.Context, c *client.Client, customerID string) error {
	if customerID == "" {
		return fmt.Errorf("no customer ID to compare the account against")
	}

	checks, err := c.ListChecks(ctx)
	if err != nil {
		return err
	}
	for id, check := range checks {
		if err := belongsTo(customerID, "check", id, check.ID, check.CustomerID); err != nil {
			return err
		}
	}

	contacts, err := c.ListContacts(ctx)
	if err != nil {
		return err
	}
	for id, contact := range contacts {
		if err := belongsTo(customerID, "contact", id, contact.ID, contact.CustomerID); err != nil {
			return err
		}
	}

	groups, err := c.ListContactGroups(ctx)
	if err != nil {
		return err
	}
	for id, group := range groups {
		if err := belongsTo(customerID, "contact group", id, group.ID, group.CustomerID); err != nil {
			return err
		}
	}

	return nil
}

// belongsTo returns an error unless an object listed under key, with the
// record's own _id and customer_id, is in the account customerID. The record
// may leave out _id and customer_id; the key is always there.
func belongsTo(customerID, kind, key, id, recordCustomerID string) error {
	prefix := customerID + "-"
	if !strings.HasPrefix(key, prefix) {
		return fmt.Errorf("%s %q is not in account %s: its ID does not start with %q", kind, key, customerID, prefix)
	}
	if id != "" && !strings.HasPrefix(id, prefix) {
		return fmt.Errorf("%s %q is not in account %s: its _id %q does not start with %q", kind, key, customerID, id, prefix)
	}
	if recordCustomerID != "" && recordCustomerID != customerID {
		return fmt.Errorf("%s %q is not in account %s: its customer_id is %q", kind, key, customerID, recordCustomerID)
	}
	return nil
}

// guardCustomer is the account the guard is told to expect in the tests
// below, and guardOther another one. guardToken is the token it lists with,
// which no error may show.
const (
	guardCustomer = "201203232048C76FH"
	guardOther    = "201205050153W2Q4C"
	guardToken    = "guard-test-token"
)

// guardSeed fills a mock with one check, one contact and one contact group,
// all in the account guardCustomer, and then applies change, if any, to the
// records.
func guardSeed(m *testutil.MockNodePingServer, change func(check, contact, group map[string]interface{})) {
	check := map[string]interface{}{
		"_id":         guardCustomer + "-0J2HSIRF",
		"customer_id": guardCustomer,
		"type":        "HTTP",
		"enable":      "inactive",
		"parameters":  map[string]interface{}{"target": "https://example.com/"},
	}
	contact := map[string]interface{}{
		"_id":         guardCustomer + "-BKPGH",
		"customer_id": guardCustomer,
		"type":        "contact",
		"name":        "guard-contact",
	}
	group := map[string]interface{}{
		"_id":         guardCustomer + "-G-1ZIYU",
		"customer_id": guardCustomer,
		"type":        "group",
		"name":        "guard-group",
		"members":     []interface{}{},
	}
	if change != nil {
		change(check, contact, group)
	}
	m.AddCheck(check["_id"].(string), check)
	m.AddContact(contact["_id"].(string), contact)
	m.AddContactGroup(group["_id"].(string), group)
}

func guardClient(url string) *client.Client {
	return client.NewClient(client.ClientConfig{
		APIToken: guardToken,
		BaseURL:  url,
		// A list that gets no answer is retried; not for seconds here.
		MaxRetries:   client.DefaultMaxRetries,
		RetryMinWait: time.Millisecond,
		RetryMaxWait: time.Millisecond,
	})
}

func TestCheckLiveAccount_passesAnAccountOfItsOwn(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	guardSeed(mock, nil)

	if err := checkLiveAccount(context.Background(), guardClient(mock.URL()), guardCustomer); err != nil {
		t.Fatalf("refused the account it was told to expect: %v", err)
	}
	if n := len(mock.Requests()); n != 3 {
		t.Errorf("sent %d requests, want 3: one list each of checks, contacts and contact groups", n)
	}
}

// Every record the guard reads can tell it the account is wrong, and each one
// has to: a token for another account lists that account's objects.
func TestCheckLiveAccount_refusesAnObjectOfAnotherAccount(t *testing.T) {
	tests := []struct {
		name string
		// change makes one record that of another account.
		change func(check, contact, group map[string]interface{})
		// want is the ID the error has to name.
		want string
	}{
		{
			name: "check ID",
			change: func(check, _, _ map[string]interface{}) {
				check["_id"] = guardOther + "-0J2HSIRF"
				check["customer_id"] = guardOther
			},
			want: guardOther + "-0J2HSIRF",
		},
		{
			name: "contact ID",
			change: func(_, contact, _ map[string]interface{}) {
				contact["_id"] = guardOther + "-BKPGH"
				contact["customer_id"] = guardOther
			},
			want: guardOther + "-BKPGH",
		},
		{
			name: "contact group ID",
			change: func(_, _, group map[string]interface{}) {
				group["_id"] = guardOther + "-G-1ZIYU"
				group["customer_id"] = guardOther
			},
			want: guardOther + "-G-1ZIYU",
		},
		{
			// The prefix ends at the dash: an account whose customer ID
			// merely starts with the expected one is another account.
			name: "ID of an account whose customer ID starts with the expected one",
			change: func(check, _, _ map[string]interface{}) {
				check["_id"] = guardCustomer + "X-0J2HSIRF"
				check["customer_id"] = guardCustomer + "X"
			},
			want: guardCustomer + "X-0J2HSIRF",
		},
		{
			name: "check customer_id",
			change: func(check, _, _ map[string]interface{}) {
				check["customer_id"] = guardOther
			},
			want: guardCustomer + "-0J2HSIRF",
		},
		{
			name: "contact customer_id",
			change: func(_, contact, _ map[string]interface{}) {
				contact["customer_id"] = guardOther
			},
			want: guardCustomer + "-BKPGH",
		},
		{
			name: "contact group customer_id",
			change: func(_, _, group map[string]interface{}) {
				group["customer_id"] = guardOther
			},
			want: guardCustomer + "-G-1ZIYU",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)
			guardSeed(mock, tt.change)

			err := checkLiveAccount(context.Background(), guardClient(mock.URL()), guardCustomer)
			if err == nil {
				t.Fatal("passed an account holding another account's object")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not name the object %s", err, tt.want)
			}
			if strings.Contains(err.Error(), guardToken) {
				t.Error("the error shows the token")
			}
		})
	}
}

// An account the guard cannot list is refused, whichever list fails and
// however. NodePing answers most errors with status 200 and an "error" body.
func TestCheckLiveAccount_refusesAnAccountItCannotList(t *testing.T) {
	answers := []struct {
		name   string
		answer http.HandlerFunc
	}{
		{"an error in a 200", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"error":"Authentication failed"}`))
		}},
		{"status 403", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		}},
	}

	for _, path := range []string{"/checks", "/contacts", "/contactgroups"} {
		for _, a := range answers {
			t.Run(path+" "+a.name, func(t *testing.T) {
				mock := testutil.NewMockNodePingServer()
				t.Cleanup(mock.Close)
				guardSeed(mock, nil)

				// The mock, but with path answered by a.
				failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == path {
						a.answer(w, r)
						return
					}
					mock.Server.Config.Handler.ServeHTTP(w, r)
				}))
				t.Cleanup(failing.Close)

				err := checkLiveAccount(context.Background(), guardClient(failing.URL), guardCustomer)
				if err == nil {
					t.Fatalf("passed an account whose %s failed with %s", path, a.name)
				}
				if strings.Contains(err.Error(), guardToken) {
					t.Error("the error shows the token")
				}
			})
		}
	}

	t.Run("no answer", func(t *testing.T) {
		gone := httptest.NewServer(http.NotFoundHandler())
		gone.Close()

		if err := checkLiveAccount(context.Background(), guardClient(gone.URL), guardCustomer); err == nil {
			t.Fatal("passed an account it could not reach")
		}
	})
}

func TestCheckLiveAccount_refusesWithoutACustomerID(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	if err := checkLiveAccount(context.Background(), guardClient(mock.URL()), ""); err == nil {
		t.Fatal("passed an account with no customer ID to compare it against")
	}
	if n := len(mock.Requests()); n != 0 {
		t.Errorf("sent %d requests, want none", n)
	}
}
