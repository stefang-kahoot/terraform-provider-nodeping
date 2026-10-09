package provider_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// accSubAccount is the SubAccount the tests below import from.
const accSubAccount = "201205050153W2Q4C"

// subAccountProviderConfig adds, next to the provider for the parent account,
// one for the SubAccount accSubAccount, both pointed at the mock.
func subAccountProviderConfig(url string) string {
	return providerConfig(url) + fmt.Sprintf(`
provider "nodeping" {
  alias       = "subaccount"
  api_token   = "acc-test-token"
  api_url     = %q
  customer_id = %q
}
`, url, accSubAccount)
}

// subAccountCase is one resource type, as the tests below import it from the
// SubAccount.
type subAccountCase struct {
	typeName string
	// id is the object's ID in the SubAccount.
	id string
	// seed adds the object to the mock's parent account; the tests then move
	// it to the SubAccount.
	seed func(m *testutil.MockNodePingServer)
	// exists reports whether the mock still holds the object.
	exists func(m *testutil.MockNodePingServer) bool
	// body is the resource's arguments, matching seed.
	body string
}

func (c subAccountCase) address() string { return c.typeName + ".sub" }

// resource is the resource block, through the provider named, or through
// the parent account's if provider is "".
func (c subAccountCase) resource(provider string) string {
	if provider != "" {
		provider = "\n  provider = " + provider + "\n"
	}
	return fmt.Sprintf("\nresource %q \"sub\" {%s%s}\n", c.typeName, provider, c.body)
}

var subAccountCases = []subAccountCase{
	{
		typeName: "nodeping_check",
		id:       accSubAccount + "-0J2HSIRF",
		seed: func(m *testutil.MockNodePingServer) {
			m.AddCheck(accSubAccount+"-0J2HSIRF", map[string]interface{}{
				"_id":         accSubAccount + "-0J2HSIRF",
				"customer_id": accSubAccount,
				"type":        "HTTP",
				"label":       "acc-subaccount-check",
				"enable":      "active",
				"interval":    15,
				"parameters": map[string]interface{}{
					"target":    "https://example.com",
					"threshold": 5,
					"sens":      2,
				},
			})
		},
		exists: func(m *testutil.MockNodePingServer) bool {
			_, ok := m.GetCheck(accSubAccount + "-0J2HSIRF")
			return ok
		},
		body: `
  type    = "HTTP"
  target  = "https://example.com"
  label   = "acc-subaccount-check"
  enabled = true
`,
	},
	{
		typeName: "nodeping_contact",
		id:       accSubAccount + "-BKPGH",
		seed: func(m *testutil.MockNodePingServer) {
			m.AddContact(accSubAccount+"-BKPGH", map[string]interface{}{
				"_id":         accSubAccount + "-BKPGH",
				"customer_id": accSubAccount,
				"type":        "contact",
				"name":        "acc-subaccount-contact",
				"custrole":    "notify",
				"addresses": map[string]interface{}{
					"ADDR1": map[string]interface{}{"type": "email", "address": "sub@example.com"},
				},
			})
		},
		exists: func(m *testutil.MockNodePingServer) bool {
			_, ok := m.GetContact(accSubAccount + "-BKPGH")
			return ok
		},
		body: `
  name = "acc-subaccount-contact"

  address {
    type    = "email"
    address = "sub@example.com"
  }
`,
	},
	{
		typeName: "nodeping_contactgroup",
		id:       accSubAccount + "-G-1ZIYU",
		seed: func(m *testutil.MockNodePingServer) {
			m.AddContactGroup(accSubAccount+"-G-1ZIYU", map[string]interface{}{
				"_id":         accSubAccount + "-G-1ZIYU",
				"customer_id": accSubAccount,
				"type":        "group",
				"name":        "acc-subaccount-group",
				"members":     []interface{}{},
			})
		},
		exists: func(m *testutil.MockNodePingServer) bool {
			_, ok := m.GetContactGroup(accSubAccount + "-G-1ZIYU")
			return ok
		},
		body: `
  name = "acc-subaccount-group"
`,
	},
}

// The "customer_id:id" import ID used to be accepted, and documented, for all
// three resources. The customer ID reached only the import's own read: the
// read Terraform makes right after the import, and every later update and
// delete, went to the provider's own account, where the object does not
// exist. Against a mock that keeps a SubAccount's objects to the SubAccount,
// the import fails with "Cannot import non-existent remote object".
//
// It now fails before any request, with a message that shows the provider
// alias to write instead. Both ways to import are tried: the command, which
// the docs showed, and an import block.
func TestAccResources_rejectTheSubAccountImportPrefix(t *testing.T) {
	kinds := []struct {
		name string
		kind resource.ImportStateKind
	}{
		{"command", resource.ImportCommandWithID},
		{"import block", resource.ImportBlockWithID},
	}

	for _, tt := range subAccountCases {
		for _, k := range kinds {
			t.Run(strings.TrimPrefix(tt.typeName, "nodeping_")+" "+k.name, func(t *testing.T) {
				mock := testutil.NewMockNodePingServer()
				t.Cleanup(mock.Close)
				tt.seed(mock)
				mock.SetAccount(tt.id, accSubAccount)

				resource.Test(t, resource.TestCase{
					ProtoV6ProviderFactories: protoV6ProviderFactories(),
					Steps: []resource.TestStep{
						{
							Config:          providerConfig(mock.URL()) + tt.resource(""),
							ResourceName:    tt.address(),
							ImportState:     true,
							ImportStateKind: k.kind,
							ImportStateId:   accSubAccount + ":" + tt.id,
							// Refusing is not enough: whoever hits this is
							// mid-migration and needs the way forward.
							ExpectError: regexp.MustCompile(`(?s)Invalid Import ID.*` +
								regexp.QuoteMeta(`customer_id = "`+accSubAccount+`"`) + `.*` +
								regexp.QuoteMeta(`provider = nodeping.subaccount`) + `.*` +
								regexp.QuoteMeta(`terraform import `+tt.typeName+`.example `+tt.id)),
						},
					},
				})

				if requests := mock.Requests(); len(requests) != 0 {
					t.Errorf("the rejected import sent %d requests, want none: %v", len(requests), requests)
				}
			})
		}
	}
}

// What works instead: a SubAccount's object imports by its plain ID through a
// provider carrying the SubAccount's customer_id, and plans no changes
// afterwards. Every request goes to the SubAccount: the import's, the reads
// after it, and the destroy at the end, which removes the object.
func TestAccResources_importFromASubAccountByPlainID(t *testing.T) {
	for _, tt := range subAccountCases {
		t.Run(strings.TrimPrefix(tt.typeName, "nodeping_"), func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)
			tt.seed(mock)
			mock.SetAccount(tt.id, accSubAccount)

			config := subAccountProviderConfig(mock.URL()) + tt.resource("nodeping.subaccount")

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{
						// An import block fails the step unless the plan
						// after the import is empty.
						Config:          config,
						ResourceName:    tt.address(),
						ImportState:     true,
						ImportStateKind: resource.ImportBlockWithID,
						ImportStateId:   tt.id,
					},
					{
						// The command, as the docs and the error message
						// show, kept in state for the steps after it.
						Config:             config,
						ResourceName:       tt.address(),
						ImportState:        true,
						ImportStateId:      tt.id,
						ImportStatePersist: true,
						ImportStateCheck: func(states []*terraform.InstanceState) error {
							if len(states) != 1 {
								return fmt.Errorf("imported %d resources, want 1", len(states))
							}
							if got := states[0].Attributes["customer_id"]; got != accSubAccount {
								return fmt.Errorf("customer_id is %q, want %q", got, accSubAccount)
							}
							return nil
						},
					},
					{
						// Refreshed and planned through the same provider:
						// nothing to change.
						Config:   config,
						PlanOnly: true,
					},
				},
			})

			requests := mock.Requests()
			if len(requests) == 0 {
				t.Fatal("no request reached the mock")
			}
			for _, r := range requests {
				if r.CustomerID != accSubAccount {
					t.Errorf("%s %s went to account %q, want the SubAccount %q", r.Method, r.Path, r.CustomerID, accSubAccount)
				}
			}
			if tt.exists(mock) {
				t.Error("the destroy left the object in the SubAccount")
			}
		})
	}
}
