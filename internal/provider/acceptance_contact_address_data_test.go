package provider_test

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// NodePing holds a webhook's data exactly as sent, and the mock does too. The
// provider used to read it back as compact JSON with its keys sorted, so a
// body written any other way, such as { "a": 1 } or {"b":1,"a":2}, failed
// the apply with "Provider produced inconsistent result after apply" on the
// address's data (#40): on create, on an edit in place, and on a reorder
// together with another change. It now reads the body as NodePing returns
// it. The resource is named as in acceptance_contact_address_ids_test.go,
// whose helpers read it from state.

const (
	dataHookOne = "https://hooks.example.com/one"
	dataHookTwo = "https://hooks.example.com/two"

	dataCompact = `{"text":"{label} is {event}"}`
	dataSpaced  = `{ "text": "{label} is {event}" }`
	// dataMultiLine is the same JSON as dataCompact, as a heredoc writes it.
	dataMultiLine = "{\n  \"text\": \"{label} is {event}\"\n}\n"
	// dataUnsorted is compact, with its keys not in sorted order.
	dataUnsorted = `{"text":"{label} is {event}","channel":"#ops"}`
	// dataEdited is other JSON, written across lines, with a number.
	dataEdited = "{\n  \"text\": \"{label} went {event}\",\n  \"attempt\": 2\n}"
	dataTwo    = `{"msg":"{label} {event}"}`
)

// webhookBlock is an address block for a webhook with data.
func webhookBlock(address, data string) string {
	return fmt.Sprintf(`
  address {
    type    = "webhook"
    address = %q
    action  = "post"
    data    = %s
  }
`, address, strconv.Quote(data))
}

func dataConfig(url, name string, blocks ...string) string {
	return providerConfig(url) + dataContact(name, blocks...)
}

func dataContact(name string, blocks ...string) string {
	return fmt.Sprintf(`
resource "nodeping_contact" "ids" {
  name = %q
%s}
`, name, strings.Join(blocks, ""))
}

// stateData checks the data state holds for the address at position i.
func stateData(i int, want string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttr(addressIDsContact, fmt.Sprintf("address.%d.data", i), want)
}

// plannedData checks the data the plan has for the address at position i.
func plannedData(i int, want string) plancheck.PlanCheck {
	return plancheck.ExpectKnownValue(addressIDsContact,
		tfjsonpath.New("address").AtSliceIndex(i).AtMapKey("data"), knownvalue.StringExact(want))
}

// heldData checks the data the mock holds for the address at position i in
// state.
func heldData(mock *testutil.MockNodePingServer, i int, want string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		contactID, addrs, err := contactAddressesInState(s)
		if err != nil {
			return err
		}
		if i >= len(addrs) {
			return fmt.Errorf("state has %d addresses, want one at %d", len(addrs), i)
		}
		contact, _ := mock.GetContact(contactID)
		stored, _ := contact["addresses"].(map[string]interface{})
		addr, _ := stored[addrs[i].id].(map[string]interface{})
		if got, _ := addr["data"].(string); got != want {
			return fmt.Errorf("the mock holds data %v for address[%d], want %q", addr["data"], i, want)
		}
		return nil
	}
}

// recordAddressID remembers the ID of the address at position i in state.
func recordAddressID(i int, into *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		_, addrs, err := contactAddressesInState(s)
		if err != nil {
			return err
		}
		if i >= len(addrs) || addrs[i].id == "" {
			return fmt.Errorf("state has no address ID at %d", i)
		}
		*into = addrs[i].id
		return nil
	}
}

var emptyPlanAfterApply = resource.ConfigPlanChecks{
	PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
}

// 1. A contact created with bodies that are not compact JSON with sorted
// keys applies, holds each as written, and plans nothing after.
func TestAccContactResource_createWebhookDataAsWritten(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"spaced", dataSpaced},
		{"across lines", dataMultiLine},
		{"compact, keys not sorted", dataUnsorted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{{
					Config:           dataConfig(mock.URL(), "acc-data", webhookBlock(dataHookOne, tt.data), webhookBlock(dataHookTwo, dataTwo)),
					ConfigPlanChecks: emptyPlanAfterApply,
					Check: resource.ComposeAggregateTestCheckFunc(
						stateData(0, tt.data),
						stateData(1, dataTwo),
						heldData(mock, 0, tt.data),
					),
				}},
			})
		})
	}
}

// 2. Editing a body in place to JSON that is not compact applies, and the
// address keeps its ID.
func TestAccContactResource_editWebhookDataAsWritten(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	var id string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: dataConfig(mock.URL(), "acc-data", webhookBlock(dataHookOne, dataCompact)),
				Check:  recordAddressID(0, &id),
			},
			{
				Config: dataConfig(mock.URL(), "acc-data", webhookBlock(dataHookOne, dataEdited)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(addressIDsContact, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					stateData(0, dataEdited),
					heldData(mock, 0, dataEdited),
					func(s *terraform.State) error { return stateAddressIDs(id)(s) },
				),
			},
		},
	})
}

// Writing a body differently without changing its JSON is no change: the plan
// keeps the text state has, and nothing is sent. This is the data plan
// modifier's job, and worked before #40 too.
func TestAccContactResource_reformatWebhookDataPlansNothing(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: dataConfig(mock.URL(), "acc-data", webhookBlock(dataHookOne, dataCompact))},
			{
				Config: dataConfig(mock.URL(), "acc-data", webhookBlock(dataHookOne, dataMultiLine)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					stateData(0, dataCompact),
					contactPUTs(mock, 0),
				),
			},
		},
	})
}

// The contact of the import tests: two webhooks whose bodies NodePing holds
// as compact JSON. The first one's ID sorts first.
const (
	dataContactID = "ACC-DATA"
	dataOneID     = "ACC-DATA-W1"
	dataTwoID     = "ACC-DATA-W2"
)

func seedDataContact(mock *testutil.MockNodePingServer) {
	mock.AddContact(dataContactID, map[string]interface{}{
		"_id":         dataContactID,
		"type":        "contact",
		"customer_id": "MOCK-CUSTOMER",
		"name":        "acc-data",
		"custrole":    "notify",
		"addresses": map[string]interface{}{
			dataOneID: map[string]interface{}{"type": "webhook", "address": dataHookOne, "action": "post", "data": dataCompact},
			dataTwoID: map[string]interface{}{"type": "webhook", "address": dataHookTwo, "action": "post", "data": dataTwo},
		},
	})
}

// dataImportConfig imports the seeded contact into a configuration that names
// it name and holds blocks, in that order.
func dataImportConfig(url, name string, blocks ...string) string {
	return providerConfig(url) + fmt.Sprintf(`
import {
  to = nodeping_contact.ids
  id = %q
}
`, dataContactID) + dataContact(name, blocks...)
}

// 3. Reordering the webhooks and renaming the contact, with one body written
// differently from NodePing's, sends the update and applies. The data plan
// modifier compares by position, so the moved block plans the
// configuration's text, which the update sends. The case #42 left open.
func TestAccContactResource_reorderAndRenameWithWebhookDataAsWritten(t *testing.T) {
	tests := []struct {
		name string
		// steps seeds the mock or returns the steps that create the
		// contact, and returns the configuration that reorders and renames
		// it.
		steps func(mock *testutil.MockNodePingServer) ([]resource.TestStep, string)
	}{
		{
			name: "swap",
			steps: func(mock *testutil.MockNodePingServer) ([]resource.TestStep, string) {
				create := resource.TestStep{
					Config: dataConfig(mock.URL(), "acc-data", webhookBlock(dataHookOne, dataCompact), webhookBlock(dataHookTwo, dataTwo)),
				}
				return []resource.TestStep{create},
					dataConfig(mock.URL(), "acc-data-renamed", webhookBlock(dataHookTwo, dataTwo), webhookBlock(dataHookOne, dataSpaced))
			},
		},
		{
			name: "import",
			steps: func(mock *testutil.MockNodePingServer) ([]resource.TestStep, string) {
				seedDataContact(mock)
				return nil, dataImportConfig(mock.URL(), "acc-data-renamed", webhookBlock(dataHookTwo, dataTwo), webhookBlock(dataHookOne, dataSpaced))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)

			steps, reordered := tt.steps(mock)
			steps = append(steps, resource.TestStep{
				Config: reordered,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(addressIDsContact, plancheck.ResourceActionUpdate),
						plannedData(1, dataSpaced),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					contactPUTs(mock, 1),
					resource.TestCheckResourceAttr(addressIDsContact, "name", "acc-data-renamed"),
					resource.TestCheckResourceAttr(addressIDsContact, "address.0.address", dataHookTwo),
					resource.TestCheckResourceAttr(addressIDsContact, "address.1.address", dataHookOne),
					stateData(0, dataTwo),
					stateData(1, dataSpaced),
					heldData(mock, 1, dataSpaced),
					stateMatchesMock(mock),
				),
			})

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps:                    steps,
			})
		})
	}
}

// 4. An import of a contact whose body NodePing holds as compact JSON, into a
// configuration that writes it differently, is a clean import: nothing to
// change, and nothing sent. The data plan modifier keeps NodePing's text.
func TestAccContactResource_importWebhookDataWrittenDifferently(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	seedDataContact(mock)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: dataImportConfig(mock.URL(), "acc-data", webhookBlock(dataHookOne, dataMultiLine), webhookBlock(dataHookTwo, dataTwo)),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(addressIDsContact, plancheck.ResourceActionNoop)},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			Check: resource.ComposeAggregateTestCheckFunc(
				contactPUTs(mock, 0),
				stateAddressIDs(dataOneID, dataTwoID),
				stateData(0, dataCompact),
			),
		}},
	})
}

// 5. A body changed outside Terraform to other JSON shows in the next plan,
// and the apply writes the configuration's back. A body rewritten as the same
// JSON in another form, as text or as an object, is no change: a refresh
// reads NodePing's text, and the data plan modifier takes the configuration
// for the same.
func TestAccContactResource_webhookDataChangedOutsideTerraform(t *testing.T) {
	const otherJSON = `{"text":"{label} went {event}"}`
	tests := []struct {
		name string
		// held is the data NodePing holds after the change.
		held interface{}
		// refreshed is the data state holds after a refresh.
		refreshed string
		// action is what the next plan does, and puts how many updates it
		// sends.
		action plancheck.ResourceActionType
		puts   int
	}{
		{"other JSON", otherJSON, otherJSON, plancheck.ResourceActionUpdate, 1},
		{"the same JSON, compact", dataCompact, dataCompact, plancheck.ResourceActionNoop, 0},
		{"the same JSON, as an object", map[string]interface{}{"text": "{label} is {event}"}, dataCompact, plancheck.ResourceActionNoop, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)

			config := dataConfig(mock.URL(), "acc-data", webhookBlock(dataHookOne, dataSpaced))
			var contactID, addrID string
			// After the last apply, state holds the configuration's text if
			// it was sent, and NodePing's otherwise.
			final := tt.refreshed
			if tt.puts > 0 {
				final = dataSpaced
			}

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: config,
						Check: resource.ComposeAggregateTestCheckFunc(
							recordAddressID(0, &addrID),
							func(s *terraform.State) error {
								var err error
								contactID, _, err = contactAddressesInState(s)
								return err
							},
						),
					},
					{
						PreConfig:          func() { mock.SetContactAddressData(contactID, addrID, tt.held) },
						RefreshState:       true,
						ExpectNonEmptyPlan: tt.puts > 0,
						Check:              stateData(0, tt.refreshed),
					},
					{
						Config: config,
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(addressIDsContact, tt.action)},
							PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
						Check: resource.ComposeAggregateTestCheckFunc(
							stateData(0, final),
							contactPUTs(mock, tt.puts),
						),
					},
				},
			})
		})
	}
}

// 6. A body that is not JSON is held and read back exactly as written, on
// create and on update, whitespace included.
func TestAccContactResource_webhookDataNotJSON(t *testing.T) {
	const (
		form    = "text={label} is {event}"
		notJSON = `{ "text": {label} is {event} }`
		padded  = "  text={label} went {event}\n"
	)
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:           dataConfig(mock.URL(), "acc-data", webhookBlock(dataHookOne, form), webhookBlock(dataHookTwo, notJSON)),
				ConfigPlanChecks: emptyPlanAfterApply,
				Check: resource.ComposeAggregateTestCheckFunc(
					stateData(0, form),
					stateData(1, notJSON),
					heldData(mock, 0, form),
					heldData(mock, 1, notJSON),
				),
			},
			{
				Config:           dataConfig(mock.URL(), "acc-data", webhookBlock(dataHookOne, padded), webhookBlock(dataHookTwo, notJSON)),
				ConfigPlanChecks: emptyPlanAfterApply,
				Check: resource.ComposeAggregateTestCheckFunc(
					stateData(0, padded),
					heldData(mock, 0, padded),
					contactPUTs(mock, 1),
				),
			},
		},
	})
}

// emptyDataError is the plan's error for data = "".
var emptyDataError = regexp.MustCompile(`Empty\s+webhook\s+data[\s\S]*Leave\s+data\s+out\s+for\s+an\s+address\s+without\s+a\s+body`)

// webhookWithoutData is a webhook address block that leaves data out.
const webhookWithoutData = `
  address {
    type    = "webhook"
    address = "https://hooks.example.com/one"
    action  = "post"
  }
`

// data = "" fails the plan, on create and on update, and says to leave data
// out. NodePing stores no data for "", so the create used to fail at apply
// with "was cty.StringVal(""), but now null", and the update the same way.
// Leaving data out works. Nothing refused is sent.
func TestAccContactResource_emptyWebhookDataFailsAtPlan(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:      dataConfig(mock.URL(), "acc-data", webhookBlock(dataHookOne, "")),
				ExpectError: emptyDataError,
			},
			{
				Config:           dataConfig(mock.URL(), "acc-data", webhookWithoutData),
				ConfigPlanChecks: emptyPlanAfterApply,
				Check: resource.ComposeAggregateTestCheckFunc(
					contactWrites(mock, 1),
					resource.TestCheckNoResourceAttr(addressIDsContact, "address.0.data"),
				),
			},
			{
				Config:      dataConfig(mock.URL(), "acc-data", webhookBlock(dataHookOne, "")),
				ExpectError: emptyDataError,
			},
			{
				Config: dataConfig(mock.URL(), "acc-data", webhookBlock(dataHookOne, dataSpaced)),
				Check: resource.ComposeAggregateTestCheckFunc(
					// The create and this update: the refused plans sent
					// nothing.
					contactWrites(mock, 2),
					stateData(0, dataSpaced),
				),
			},
		},
	})
}
