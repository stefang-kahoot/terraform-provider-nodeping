package provider_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// NodePing stores whatever priority an address is sent, unvalidated: "",
// "abc" and "1" alike, which anyone can set through the API. The client
// decoded it as a number, so one such address failed every read of its
// contact, and every list of contacts in the account:
//
//	failed to unmarshal response: json: cannot unmarshal string into Go
//	struct field ContactAddress.addresses.priority of type int
//
// A numeric string now reads as its number, "" as none, and anything else as
// none with a warning in the log.

// seedUnreadablePriorities adds a contact whose Pushover addresses hold the
// priorities "abc", "1" and "", as NodePing would store them if sent.
func seedUnreadablePriorities(mock *testutil.MockNodePingServer) {
	pushover := func(key, priority string) map[string]interface{} {
		return map[string]interface{}{"type": "pushover", "address": key, "priority": priority}
	}
	mock.AddContact("UNREADABLE-PRIORITY", map[string]interface{}{
		"_id":         "UNREADABLE-PRIORITY",
		"type":        "contact",
		"customer_id": "MOCK-CUSTOMER",
		"name":        "acc-unreadable-priority",
		"custrole":    "notify",
		"addresses": map[string]interface{}{
			"P1": pushover("FAKE-pushover-user-key-1", "abc"),
			"P2": pushover("FAKE-pushover-user-key-2", "1"),
			"P3": pushover("FAKE-pushover-user-key-3", ""),
		},
	})
}

// The import plans no change: "abc" and "" read as none, and "1" as 1.
func TestAccContactResource_importsUnreadablePriority(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	seedUnreadablePriorities(mock)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(mock.URL()) + `
resource "nodeping_contact" "imported" {
  name = "acc-unreadable-priority"

  address {
    type    = "pushover"
    address = "FAKE-pushover-user-key-1"
  }

  address {
    type     = "pushover"
    address  = "FAKE-pushover-user-key-2"
    priority = 1
  }

  address {
    type    = "pushover"
    address = "FAKE-pushover-user-key-3"
  }
}
`,
				ResourceName:    "nodeping_contact.imported",
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				ImportStateId:   "UNREADABLE-PRIORITY",
			},
		},
	})
}

// Both contact data sources read the contact. The list failed for every
// contact in the account.
func TestAccContactDataSources_readUnreadablePriority(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	seedUnreadablePriorities(mock)

	tests := []struct {
		name   string
		config string
		check  resource.TestCheckFunc
	}{
		{
			name: "nodeping_contact",
			config: `
data "nodeping_contact" "one" {
  id = "UNREADABLE-PRIORITY"
}
`,
			check: resource.TestCheckResourceAttr("data.nodeping_contact.one", "addresses.#", "3"),
		},
		{
			name: "nodeping_contacts",
			config: `
data "nodeping_contacts" "all" {}
`,
			check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.nodeping_contacts.all", "contacts.#", "1"),
				resource.TestCheckResourceAttr("data.nodeping_contacts.all", "contacts.0.addresses.#", "3"),
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

// A priority set to "abc" behind Terraform's back reads as none on refresh,
// so the plan writes the configured one back.
func TestAccContactResource_unreadablePriorityIsWrittenBack(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	const name = "nodeping_contact.priority"
	config := providerConfig(mock.URL()) + `
resource "nodeping_contact" "priority" {
  name = "acc-priority-written-back"

  address {
    type     = "pushover"
    address  = "FAKE-pushover-user-key"
    priority = 1
  }
}
`
	var contactID, addrID string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: func(s *terraform.State) error {
					rs, ok := s.RootModule().Resources[name]
					if !ok {
						return fmt.Errorf("%s is not in state", name)
					}
					contactID, addrID = rs.Primary.ID, rs.Primary.Attributes["address.0.id"]
					return nil
				},
			},
			{
				// Through the API, as anyone could.
				PreConfig: func() {
					putAddressField(t, mock, contactID, addrID, `"priority":"abc"`)
				},
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "address.0.priority", "1"),
					func(*terraform.State) error {
						contact, _ := mock.GetContact(contactID)
						addresses, _ := contact["addresses"].(map[string]interface{})
						addr, _ := addresses[addrID].(map[string]interface{})
						if got := addr["priority"]; got != float64(1) {
							return fmt.Errorf("the mock holds priority %v, want 1", got)
						}
						return nil
					},
				),
			},
		},
	})
}

// putAddressField updates one address of a contact in the mock through its
// API, sending only the given JSON fields under the address's ID. The mock
// merges them into the address, as NodePing does.
func putAddressField(t *testing.T, mock *testutil.MockNodePingServer, contactID, addrID, fields string) {
	t.Helper()
	body := fmt.Sprintf(`{"addresses":{%q:{%s}}}`, addrID, fields)
	req, err := http.NewRequest(http.MethodPut, mock.URL()+"/contacts/"+contactID, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	answer, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || strings.Contains(string(answer), `"error"`) {
		t.Fatalf("PUT /contacts/%s: status %d", contactID, resp.StatusCode)
	}
}
