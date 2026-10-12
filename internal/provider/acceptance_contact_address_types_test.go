package provider_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// NodePing stores a webhook's action, and each value of its headers and query
// strings, exactly as sent, whatever its JSON type (probed 2026-10-12, #46).
// Anyone can set one through the API. The client decoded them as a string and
// maps of strings, so one such address failed every read of its contact, and
// every list of contacts in the account:
//
//	failed to unmarshal response: json: cannot unmarshal number into Go
//	struct field Contact.addresses.Plain.action of type string
//
// An action that is not a string now reads as none, and a header or query
// string value as its JSON text.

const (
	retypedContactID = "RETYPED-FIELDS"
	retypedContact   = "nodeping_contact.retyped"
)

// seedRetypedAddressFields adds a contact with a webhook whose action is the
// number 1 and whose headers and query strings hold no string, as NodePing
// would store them if sent, and an email address.
func seedRetypedAddressFields(mock *testutil.MockNodePingServer) {
	mock.AddContact(retypedContactID, map[string]interface{}{
		"_id":         retypedContactID,
		"type":        "contact",
		"customer_id": "MOCK-CUSTOMER",
		"name":        "acc-retyped-fields",
		"custrole":    "notify",
		"addresses": map[string]interface{}{
			"HOOK": map[string]interface{}{
				"type":    "webhook",
				"address": "https://hooks.example.com/retyped",
				"action":  1,
				"headers": map[string]interface{}{"a": 1, "b": true, "c": map[string]interface{}{"x": "y"}},
				// json.Number is sent as written: 1.0, not 1.
				"querystrings": map[string]interface{}{
					"n":   json.Number("1.0"),
					"big": json.Number("12345678901234567890"),
				},
			},
			"MAIL": map[string]interface{}{"type": "email", "address": "retyped@example.com"},
		},
	})
}

// retypedConfig imports the seeded contact into a configuration named name
// whose webhook sets the given action line, or none, and its headers and
// query strings as their JSON text.
func retypedConfig(url, name, action string) string {
	return providerConfig(url) + fmt.Sprintf(`
import {
  to = nodeping_contact.retyped
  id = %q
}

resource "nodeping_contact" "retyped" {
  name = %q

  address {
    type    = "webhook"
    address = "https://hooks.example.com/retyped"
    %s
    headers = {
      a = "1"
      b = "true"
      c = jsonencode({ x = "y" })
    }
    querystrings = {
      n   = "1.0"
      big = "12345678901234567890"
    }
  }

  address {
    type    = "email"
    address = "retyped@example.com"
  }
}
`, retypedContactID, name, action)
}

// retypedRead checks that state holds the seeded webhook as read: no action,
// and each header and query string as its JSON text, digits as written.
var retypedRead = resource.ComposeAggregateTestCheckFunc(
	resource.TestCheckResourceAttr(retypedContact, "address.0.id", "HOOK"),
	resource.TestCheckNoResourceAttr(retypedContact, "address.0.action"),
	resource.TestCheckResourceAttr(retypedContact, "address.0.headers.%", "3"),
	resource.TestCheckResourceAttr(retypedContact, "address.0.headers.a", "1"),
	resource.TestCheckResourceAttr(retypedContact, "address.0.headers.b", "true"),
	resource.TestCheckResourceAttr(retypedContact, "address.0.headers.c", `{"x":"y"}`),
	resource.TestCheckResourceAttr(retypedContact, "address.0.querystrings.%", "2"),
	resource.TestCheckResourceAttr(retypedContact, "address.0.querystrings.n", "1.0"),
	resource.TestCheckResourceAttr(retypedContact, "address.0.querystrings.big", "12345678901234567890"),
	resource.TestCheckResourceAttr(retypedContact, "address.1.id", "MAIL"),
)

// retypedPUTs checks how many updates of the seeded contact the mock has
// received.
func retypedPUTs(mock *testutil.MockNodePingServer, want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		got := 0
		for _, r := range mock.Requests() {
			if r.Method == http.MethodPut && r.Path == "/contacts/"+retypedContactID {
				got++
			}
		}
		if got != want {
			return fmt.Errorf("the mock received %d PUTs for contact %s, want %d", got, retypedContactID, want)
		}
		return nil
	}
}

// retypedHookHolds checks the webhook's fields the mock holds, as JSON.
func retypedHookHolds(mock *testutil.MockNodePingServer, want map[string]string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		contact, ok := mock.GetContact(retypedContactID)
		if !ok {
			return fmt.Errorf("contact %s is not in the mock", retypedContactID)
		}
		addresses, _ := contact["addresses"].(map[string]interface{})
		hook, _ := addresses["HOOK"].(map[string]interface{})
		got := make(map[string]string, len(want))
		for field := range want {
			text, err := json.Marshal(hook[field])
			if err != nil {
				return err
			}
			got[field] = string(text)
		}
		if !reflect.DeepEqual(got, want) {
			return fmt.Errorf("the mock holds %v, want %v", got, want)
		}
		return nil
	}
}

// The import and every refresh after it read the contact. A configuration
// that sets action then plans an update, which writes the action and the
// headers and query strings as strings, and the plan after it is empty.
func TestAccContactResource_readsRetypedAddressFields(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	seedRetypedAddressFields(mock)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: retypedConfig(mock.URL(), "acc-retyped-fields", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(retypedContact, plancheck.ResourceActionNoop)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(retypedRead, retypedPUTs(mock, 0)),
			},
			{
				RefreshState: true,
				Check:        resource.ComposeAggregateTestCheckFunc(retypedRead, retypedPUTs(mock, 0)),
			},
			{
				Config: retypedConfig(mock.URL(), "acc-retyped-fields", `action = "post"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(retypedContact, plancheck.ResourceActionUpdate),
						plancheck.ExpectKnownValue(retypedContact, tfjsonpath.New("address").AtSliceIndex(0).AtMapKey("action"), knownvalue.StringExact("post")),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(retypedContact, "address.0.action", "post"),
					resource.TestCheckResourceAttr(retypedContact, "address.0.headers.c", `{"x":"y"}`),
					resource.TestCheckResourceAttr(retypedContact, "address.0.querystrings.n", "1.0"),
					retypedPUTs(mock, 1),
					retypedHookHolds(mock, map[string]string{
						"action":       `"post"`,
						"headers":      `{"a":"1","b":"true","c":"{\"x\":\"y\"}"}`,
						"querystrings": `{"big":"12345678901234567890","n":"1.0"}`,
					}),
				),
			},
		},
	})
}

// An action that reads as none leaves state holding none, so #45's refusal to
// remove an action does not apply: an update that leaves action out goes
// through, and NodePing keeps the action it holds.
func TestAccContactResource_retypedActionLeftOut(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	seedRetypedAddressFields(mock)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: retypedConfig(mock.URL(), "acc-retyped-fields", ""),
				Check:  retypedRead,
			},
			{
				Config: retypedConfig(mock.URL(), "acc-retyped-fields-renamed", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(retypedContact, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(retypedContact, "name", "acc-retyped-fields-renamed"),
					resource.TestCheckNoResourceAttr(retypedContact, "address.0.action"),
					retypedPUTs(mock, 1),
					retypedHookHolds(mock, map[string]string{"action": "1"}),
				),
			},
		},
	})
}

// Both contact data sources read the contact. The list failed for every
// contact in the account.
func TestAccContactDataSources_readRetypedAddressFields(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	seedRetypedAddressFields(mock)

	tests := []struct {
		name   string
		config string
		check  resource.TestCheckFunc
	}{
		{
			name: "nodeping_contact",
			config: fmt.Sprintf(`
data "nodeping_contact" "one" {
  id = %q
}
`, retypedContactID),
			check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.nodeping_contact.one", "addresses.#", "2"),
				resource.TestCheckResourceAttr("data.nodeping_contact.one", "addresses.0.id", "HOOK"),
				resource.TestCheckResourceAttr("data.nodeping_contact.one", "addresses.0.type", "webhook"),
			),
		},
		{
			name: "nodeping_contacts",
			config: `
data "nodeping_contacts" "all" {}
`,
			check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.nodeping_contacts.all", "contacts.#", "1"),
				resource.TestCheckResourceAttr("data.nodeping_contacts.all", "contacts.0.addresses.#", "2"),
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{Config: providerConfig(mock.URL()) + tt.config, Check: tt.check},
				},
			})
		})
	}
}
