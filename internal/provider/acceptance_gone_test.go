package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// Something deleted in the NodePing web interface has to leave state, so that
// the next plan creates it again. NodePing answers a read of a deleted ID with
// 200 and an "error" body (a check) or {} (a contact or contact group), never
// with 404, and the mock answers the same way.
func TestAccResource_deletedOutsideTerraform(t *testing.T) {
	tests := []struct {
		name    string
		address string
		config  string
		remove  func(*testutil.MockNodePingServer, string)
	}{
		{
			name:    "check",
			address: "nodeping_check.gone",
			config: `
resource "nodeping_check" "gone" {
  type     = "HTTP"
  target   = "https://example.com"
  label    = "acc-gone"
  interval = 5
}
`,
			remove: (*testutil.MockNodePingServer).RemoveCheck,
		},
		{
			name:    "contact",
			address: "nodeping_contact.gone",
			config: `
resource "nodeping_contact" "gone" {
  name = "acc-gone"

  address {
    type    = "email"
    address = "gone@example.com"
  }
}
`,
			remove: (*testutil.MockNodePingServer).RemoveContact,
		},
		{
			name:    "contact group",
			address: "nodeping_contactgroup.gone",
			config: `
resource "nodeping_contactgroup" "gone" {
  name = "acc-gone"
}
`,
			remove: (*testutil.MockNodePingServer).RemoveContactGroup,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)
			config := providerConfig(mock.URL()) + tt.config

			var id string
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: config,
						Check: func(s *terraform.State) error {
							rs, ok := s.RootModule().Resources[tt.address]
							if !ok {
								return fmt.Errorf("%s is not in state", tt.address)
							}
							id = rs.Primary.ID
							return nil
						},
					},
					{
						PreConfig: func() { tt.remove(mock, id) },
						Config:    config,
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{
								plancheck.ExpectResourceAction(tt.address, plancheck.ResourceActionCreate),
							},
						},
					},
				},
			})
		})
	}
}
