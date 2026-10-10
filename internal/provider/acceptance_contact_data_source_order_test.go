package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// Regression test for #26: NodePing returns contacts, and a contact's
// addresses, as objects keyed by ID, and the contact data sources used to
// list them in Go's random map order. Each step reads both data sources again
// and expects every list in ID order.
func TestAccContactDataSources_listInIDOrder(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	addresses := func(contact string) map[string]interface{} {
		out := make(map[string]interface{})
		for _, id := range []string{"D4", "A1", "C3", "B2"} {
			out[id] = map[string]interface{}{
				"type":    "email",
				"address": fmt.Sprintf("%s-%s@example.com", contact, id),
			}
		}
		return out
	}
	for _, id := range []string{"MOCK-CONTACT-C", "MOCK-CONTACT-A", "MOCK-CONTACT-B"} {
		contact := map[string]interface{}{
			"_id":         id,
			"type":        "contact",
			"customer_id": "MOCK-CUSTOMER",
			"name":        id,
			"custrole":    "notify",
			"addresses":   addresses(id),
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
		resource.TestCheckResourceAttr("data.nodeping_contacts.all", "contacts.#", "3"),
		resource.TestCheckResourceAttr("data.nodeping_contact.one", "addresses.#", "4"),
	}
	for i, contact := range []string{"MOCK-CONTACT-A", "MOCK-CONTACT-B", "MOCK-CONTACT-C"} {
		checks = append(checks,
			resource.TestCheckResourceAttr("data.nodeping_contacts.all", fmt.Sprintf("contacts.%d.id", i), contact),
			resource.TestCheckResourceAttr("data.nodeping_contacts.all", fmt.Sprintf("contacts.%d.name", i), contact),
		)
		for j, addr := range []string{"A1", "B2", "C3", "D4"} {
			checks = append(checks,
				resource.TestCheckResourceAttr("data.nodeping_contacts.all", fmt.Sprintf("contacts.%d.addresses.%d.id", i, j), addr),
				resource.TestCheckResourceAttr("data.nodeping_contacts.all", fmt.Sprintf("contacts.%d.addresses.%d.address", i, j), contact+"-"+addr+"@example.com"),
			)
		}
	}
	for j, addr := range []string{"A1", "B2", "C3", "D4"} {
		checks = append(checks,
			resource.TestCheckResourceAttr("data.nodeping_contact.one", fmt.Sprintf("addresses.%d.id", j), addr),
			resource.TestCheckResourceAttr("data.nodeping_contact.one", fmt.Sprintf("addresses.%d.address", j), "MOCK-CONTACT-A-"+addr+"@example.com"),
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
