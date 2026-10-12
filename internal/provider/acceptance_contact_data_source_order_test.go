package provider_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// Regression test for #26: NodePing returns contacts, and a contact's
// addresses, as objects keyed by ID, and the contact data sources used to
// list them in Go's random map order. Each step reads both data sources again
// and expects every list in ID order.
//
// The mock sends JSON objects with their keys sorted, so each decoded map is
// filled in ID order. Go only rotates where it starts iterating a map of 8 or
// fewer entries, which leaves 3 entries in ID order 75% of the time. A map of
// 9 entries comes out in ID order about 1% of the time, so every list here
// has 9 entries and a missing sort fails the test.
func TestAccContactDataSources_listInIDOrder(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	// In ID order: MOCK-CONTACT-A to MOCK-CONTACT-I, and A1 to I9.
	var contactIDs, addressIDs []string
	for i := range 9 {
		contactIDs = append(contactIDs, fmt.Sprintf("MOCK-CONTACT-%c", 'A'+i))
		addressIDs = append(addressIDs, fmt.Sprintf("%c%d", 'A'+i, i+1))
	}
	address := func(contact, id string) string {
		return fmt.Sprintf("%s-%s@example.com", contact, id)
	}

	for _, id := range contactIDs {
		addresses := make(map[string]interface{})
		for _, addrID := range addressIDs {
			addresses[addrID] = map[string]interface{}{
				"type":    "email",
				"address": address(id, addrID),
			}
		}
		contact := map[string]interface{}{
			"_id":         id,
			"type":        "contact",
			"customer_id": "MOCK-CUSTOMER",
			"name":        id,
			"custrole":    "notify",
			"addresses":   addresses,
		}
		// The data source falls back to the map key for a contact that
		// comes without `_id`.
		if id == "MOCK-CONTACT-B" {
			delete(contact, "_id")
		}
		mock.AddContact(id, contact)
	}

	config := providerConfig(mock.URL()) + `
data "nodeping_contacts" "all" {}

data "nodeping_contact" "one" {
  id = "MOCK-CONTACT-A"
}
`

	checks := []resource.TestCheckFunc{
		resource.TestCheckResourceAttr("data.nodeping_contacts.all", "contacts.#", strconv.Itoa(len(contactIDs))),
		resource.TestCheckResourceAttr("data.nodeping_contact.one", "addresses.#", strconv.Itoa(len(addressIDs))),
	}
	for i, contact := range contactIDs {
		checks = append(checks,
			resource.TestCheckResourceAttr("data.nodeping_contacts.all", fmt.Sprintf("contacts.%d.id", i), contact),
			resource.TestCheckResourceAttr("data.nodeping_contacts.all", fmt.Sprintf("contacts.%d.name", i), contact),
			resource.TestCheckResourceAttr("data.nodeping_contacts.all", fmt.Sprintf("contacts.%d.addresses.#", i), strconv.Itoa(len(addressIDs))),
		)
		for j, addrID := range addressIDs {
			checks = append(checks,
				resource.TestCheckResourceAttr("data.nodeping_contacts.all", fmt.Sprintf("contacts.%d.addresses.%d.id", i, j), addrID),
				resource.TestCheckResourceAttr("data.nodeping_contacts.all", fmt.Sprintf("contacts.%d.addresses.%d.address", i, j), address(contact, addrID)),
			)
		}
	}
	for j, addrID := range addressIDs {
		checks = append(checks,
			resource.TestCheckResourceAttr("data.nodeping_contact.one", fmt.Sprintf("addresses.%d.id", j), addrID),
			resource.TestCheckResourceAttr("data.nodeping_contact.one", fmt.Sprintf("addresses.%d.address", j), address("MOCK-CONTACT-A", addrID)),
		)
	}

	step := resource.TestStep{
		Config: config,
		Check:  resource.ComposeAggregateTestCheckFunc(checks...),
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps:                    []resource.TestStep{step, step, step},
	})
}
