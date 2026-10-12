package provider_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

const dataNumbersContact = "nodeping_contact.numbers"

func dataNumbersConfig(url, id string) string {
	return providerConfig(url) + fmt.Sprintf(`
resource "nodeping_contact" "numbers" {
  name = "acc-data-numbers"

  address {
    type    = "webhook"
    address = "https://hooks.example.com/numbers"
    action  = "post"
    data    = "{\"id\":%s}"
  }
}
`, id)
}

// mockHoldsOnlyData checks the data of the contact's only address in the
// mock.
func mockHoldsOnlyData(mock *testutil.MockNodePingServer, want string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[dataNumbersContact]
		if !ok {
			return fmt.Errorf("%s is not in state", dataNumbersContact)
		}
		contact, _ := mock.GetContact(rs.Primary.ID)
		addresses, _ := contact["addresses"].(map[string]interface{})
		if len(addresses) != 1 {
			return fmt.Errorf("the mock holds %d addresses, want 1", len(addresses))
		}
		for _, addr := range addresses {
			if got := addr.(map[string]interface{})["data"]; got != want {
				return fmt.Errorf("the mock holds data %v, want %s", got, want)
			}
		}
		return nil
	}
}

// A webhook body's numbers are compared exactly. Decoded as float64, two
// integers beyond 2^53 that differ compared equal, so changing one planned no
// change, and NodePing kept the old one.
func TestAccContactResource_webhookDataLargeInteger(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: dataNumbersConfig(mock.URL(), "12345678901234567890"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.TestCheckResourceAttr(dataNumbersContact, "address.0.data", `{"id":12345678901234567890}`),
			},
			{
				Config: dataNumbersConfig(mock.URL(), "12345678901234567891"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(dataNumbersContact, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(dataNumbersContact, "address.0.data", `{"id":12345678901234567891}`),
					mockHoldsOnlyData(mock, `{"id":12345678901234567891}`),
				),
			},
			{
				Config:            dataNumbersConfig(mock.URL(), "12345678901234567891"),
				ResourceName:      dataNumbersContact,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// A body NodePing holds as an object, which only a write outside Terraform
// leaves, is read with its numbers as written. Read as float64, a large
// integer in it rounded, and the import of a configuration that writes the
// same body planned an update.
func TestAccContactResource_importWebhookDataObjectWithLargeInteger(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	mock.AddContact("ACC-NUMBERS", map[string]interface{}{
		"_id":         "ACC-NUMBERS",
		"type":        "contact",
		"customer_id": "MOCK-CUSTOMER",
		"name":        "acc-data-numbers",
		"custrole":    "notify",
		"addresses": map[string]interface{}{
			"ACC-NUMBERS-W1": map[string]interface{}{
				"type": "webhook", "address": "https://hooks.example.com/numbers", "action": "post",
				"data": map[string]interface{}{"id": json.Number("12345678901234567890")},
			},
		},
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: dataNumbersConfig(mock.URL(), "12345678901234567890") + `
import {
  to = nodeping_contact.numbers
  id = "ACC-NUMBERS"
}
`,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(dataNumbersContact, plancheck.ResourceActionNoop)},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			Check: resource.TestCheckResourceAttr(dataNumbersContact, "address.0.data", `{"id":12345678901234567890}`),
		}},
	})
}
