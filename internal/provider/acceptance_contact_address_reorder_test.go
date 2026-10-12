package provider_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// NodePing holds a contact's addresses as an object keyed by address ID, with
// no order, and an import reads them in ID order. Terraform compares the
// address blocks by position, so a configuration that lists them in another
// order plans an in-place update after the import, and applying it used to
// write the same addresses back under the same IDs (#30). When the order is
// the only difference, the apply now sends nothing.
//
// The mock hands out IDs that sort in creation order, so the imported contact
// is seeded with IDs that sort differently from the configuration's order.
// The resource is named as in acceptance_contact_address_ids_test.go, whose
// helpers read it from state.

const reorderContactID = "ACC-REORDER"

// reorderAddress is one address of the seeded contact: its ID, the address as
// NodePing holds it, and its block in the configuration.
type reorderAddress struct {
	id     string
	stored map[string]interface{}
	block  string
}

// The webhook's ID sorts first, the email's last, so an import reads them as
// webhook, pushover, email. The configuration lists them as email, webhook,
// pushover: every position differs.
var (
	reorderHook = reorderAddress{
		id: "ACC-REORDER-A1",
		stored: map[string]interface{}{
			"type":         "webhook",
			"address":      "https://hooks.example.com/reorder",
			"action":       "post",
			"headers":      map[string]interface{}{"content-type": "application/json"},
			"querystrings": map[string]interface{}{"key": "1"},
			"data":         `{"text":"{label} is {event}"}`,
			"suppressup":   true,
		},
		block: `
  address {
    type         = "webhook"
    address      = "https://hooks.example.com/reorder"
    action       = "post"
    headers      = { "content-type" = "application/json" }
    querystrings = { key = "1" }
    data         = "{\"text\":\"{label} is {event}\"}"
    suppress_up  = true
  }
`,
	}
	reorderPushover = reorderAddress{
		id: "ACC-REORDER-B2",
		stored: map[string]interface{}{
			"type":        "pushover",
			"address":     "acc-reorder-user-key",
			"priority":    1,
			"suppressall": true,
		},
		block: `
  address {
    type         = "pushover"
    address      = "acc-reorder-user-key"
    priority     = 1
    suppress_all = true
  }
`,
	}
	reorderEmail = reorderAddress{
		id: "ACC-REORDER-C3",
		stored: map[string]interface{}{
			"type":          "email",
			"address":       "reorder@example.com",
			"suppressdown":  true,
			"suppressfirst": true,
		},
		block: `
  address {
    type           = "email"
    address        = "reorder@example.com"
    suppress_down  = true
    suppress_first = true
  }
`,
	}

	// reorderConfigured is the order the configuration lists them in.
	reorderConfigured = []reorderAddress{reorderEmail, reorderHook, reorderPushover}
)

// seedReorderContact adds the contact to the mock as NodePing holds it.
func seedReorderContact(mock *testutil.MockNodePingServer) {
	addresses := make(map[string]interface{}, len(reorderConfigured))
	for _, a := range reorderConfigured {
		stored := make(map[string]interface{}, len(a.stored))
		for k, v := range a.stored {
			stored[k] = v
		}
		addresses[a.id] = stored
	}
	mock.AddContact(reorderContactID, map[string]interface{}{
		"_id":         reorderContactID,
		"type":        "contact",
		"customer_id": "MOCK-CUSTOMER",
		"name":        "acc-reorder",
		"custrole":    "notify",
		"addresses":   addresses,
	})
}

// reorderImportConfig imports the seeded contact into a configuration that
// names it name and holds blocks, in that order.
func reorderImportConfig(url, name string, blocks ...string) string {
	return providerConfig(url) + fmt.Sprintf(`
import {
  to = nodeping_contact.ids
  id = %q
}

resource "nodeping_contact" "ids" {
  name = %q
%s}
`, reorderContactID, name, strings.Join(blocks, ""))
}

func reorderBlocks(addrs []reorderAddress) []string {
	blocks := make([]string, 0, len(addrs))
	for _, a := range addrs {
		blocks = append(blocks, a.block)
	}
	return blocks
}

// planHasAddressIDs checks the address IDs the plan has, in order.
func planHasAddressIDs(ids ...string) []plancheck.PlanCheck {
	checks := make([]plancheck.PlanCheck, 0, len(ids))
	for i, id := range ids {
		checks = append(checks, plancheck.ExpectKnownValue(addressIDsContact,
			tfjsonpath.New("address").AtSliceIndex(i).AtMapKey("id"), knownvalue.StringExact(id)))
	}
	return checks
}

// stateAddressIDs checks the address IDs state has, in order.
func stateAddressIDs(ids ...string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		_, addrs, err := contactAddressesInState(s)
		if err != nil {
			return err
		}
		got := make([]string, 0, len(addrs))
		for _, a := range addrs {
			got = append(got, a.id)
		}
		if !reflect.DeepEqual(got, ids) {
			return fmt.Errorf("state has the address IDs %v, want %v", got, ids)
		}
		return nil
	}
}

// contactPUTs checks how many updates the mock has received for the contact
// in state, counted from its request log.
func contactPUTs(mock *testutil.MockNodePingServer, want int) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		contactID, _, err := contactAddressesInState(s)
		if err != nil {
			return err
		}
		got := 0
		for _, r := range mock.Requests() {
			if r.Method == http.MethodPut && r.Path == "/contacts/"+contactID {
				got++
			}
		}
		if got != want {
			return fmt.Errorf("the mock received %d PUTs for contact %s, want %d", got, contactID, want)
		}
		return nil
	}
}

// mockContactJSON is the contact id as the mock holds it, as JSON.
func mockContactJSON(mock *testutil.MockNodePingServer, id string) (string, error) {
	contact, ok := mock.GetContact(id)
	if !ok {
		return "", fmt.Errorf("contact %s is not in the mock", id)
	}
	body, err := json.Marshal(contact)
	return string(body), err
}

// mockContactUnchanged checks that the contact in state is held by the mock
// exactly as snapshot recorded it.
func mockContactUnchanged(mock *testutil.MockNodePingServer, snapshot *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		contactID, _, err := contactAddressesInState(s)
		if err != nil {
			return err
		}
		now, err := mockContactJSON(mock, contactID)
		if err != nil {
			return err
		}
		if now != *snapshot {
			return fmt.Errorf("the contact changed in the mock")
		}
		return nil
	}
}

// An import whose configuration lists the addresses in another order than
// their IDs plans an update, which applies without writing to NodePing. State
// takes the configuration's order, each address under its own ID, and the
// plan after it is empty.
func TestAccContactResource_importInAnotherOrderSendsNothing(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	seedReorderContact(mock)

	var snapshot string
	ids := []string{reorderEmail.id, reorderHook.id, reorderPushover.id}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				PreConfig: func() {
					var err error
					if snapshot, err = mockContactJSON(mock, reorderContactID); err != nil {
						t.Fatal(err)
					}
				},
				Config: reorderImportConfig(mock.URL(), "acc-reorder", reorderBlocks(reorderConfigured)...),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: append([]plancheck.PlanCheck{
						plancheck.ExpectResourceAction(addressIDsContact, plancheck.ResourceActionUpdate),
					}, planHasAddressIDs(ids...)...),
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					contactPUTs(mock, 0),
					stateAddressIDs(ids...),
					resource.TestCheckResourceAttr(addressIDsContact, "address.0.type", "email"),
					resource.TestCheckResourceAttr(addressIDsContact, "address.1.type", "webhook"),
					resource.TestCheckResourceAttr(addressIDsContact, "address.2.type", "pushover"),
					mockContactUnchanged(mock, &snapshot),
					stateMatchesMock(mock),
				),
			},
		},
	})
}

// A contact Terraform created, whose configuration then swaps two blocks,
// plans an update that sends nothing, also with ignore_mute and an address
// muted in NodePing until a time.
func TestAccContactResource_swapAddressesSendsNothing(t *testing.T) {
	const hook = `
  address {
    type         = "webhook"
    address      = "https://hooks.example.com/swap"
    action       = "post"
    headers      = { "content-type" = "application/json" }
    querystrings = { key = "1" }
    data         = "{\"text\":\"{label} is {event}\"}"
    suppress_up  = true
  }
`
	const email = `
  address {
    type          = "email"
    address       = "swap@example.com"
    suppress_down = true
  }
`

	tests := []struct {
		name     string
		provider func(url string) string
		// mute, when not nil, is set on the webhook in NodePing before the swap.
		mute interface{}
	}{
		{name: "plain", provider: providerConfig},
		{name: "ignore_mute with a muted address", provider: providerIgnoringMute, mute: int64(1791462833954)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)

			config := func(blocks ...string) string {
				return tt.provider(mock.URL()) + fmt.Sprintf(`
resource "nodeping_contact" "ids" {
  name = "acc-swap"
%s}
`, strings.Join(blocks, ""))
			}

			before := map[string]string{}
			var snapshot string
			var hookIDs addrMuteIDs

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: config(hook, email),
						Check: resource.ComposeAggregateTestCheckFunc(
							recordAddressIDs(before),
							func(s *terraform.State) error {
								contactID, addrs, err := contactAddressesInState(s)
								hookIDs = addrMuteIDs{contact: contactID}
								if err == nil && len(addrs) > 0 {
									hookIDs.address = addrs[0].id
								}
								return err
							},
						),
					},
					{
						PreConfig: func() {
							if tt.mute != nil {
								mock.SetContactAddressMute(hookIDs.contact, hookIDs.address, tt.mute)
							}
							var err error
							if snapshot, err = mockContactJSON(mock, hookIDs.contact); err != nil {
								t.Fatal(err)
							}
						},
						Config: config(email, hook),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{
								plancheck.ExpectResourceAction(addressIDsContact, plancheck.ResourceActionUpdate),
							},
							PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
						Check: resource.ComposeAggregateTestCheckFunc(
							contactPUTs(mock, 0),
							func(s *terraform.State) error {
								return stateAddressIDs(before["email:swap@example.com"], before["webhook:https://hooks.example.com/swap"])(s)
							},
							resource.TestCheckResourceAttr(addressIDsContact, "address.1.mute", fmt.Sprint(tt.mute != nil)),
							mockContactUnchanged(mock, &snapshot),
							stateMatchesMock(mock),
						),
					},
				},
			})
		})
	}
}

// A reorder along with a real change sends the update, once, carrying every
// address under its own ID and the change.
func TestAccContactResource_importInAnotherOrderWithAChangeSendsIt(t *testing.T) {
	changedHook := strings.Replace(reorderHook.block,
		`headers      = { "content-type" = "application/json" }`,
		`headers      = { "content-type" = "text/plain" }`, 1)

	tests := []struct {
		name    string
		contact string
		blocks  []string
		// headers is the webhook's headers the update must send.
		headers map[string]interface{}
	}{
		{
			name:    "the contact renamed",
			contact: "acc-reorder-renamed",
			blocks:  reorderBlocks(reorderConfigured),
			headers: map[string]interface{}{"content-type": "application/json"},
		},
		{
			name:    "an address changed",
			contact: "acc-reorder",
			blocks:  []string{reorderEmail.block, changedHook, reorderPushover.block},
			headers: map[string]interface{}{"content-type": "text/plain"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)
			seedReorderContact(mock)

			ids := []string{reorderEmail.id, reorderHook.id, reorderPushover.id}

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: reorderImportConfig(mock.URL(), tt.contact, tt.blocks...),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: append([]plancheck.PlanCheck{
								plancheck.ExpectResourceAction(addressIDsContact, plancheck.ResourceActionUpdate),
							}, planHasAddressIDs(ids...)...),
							PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
						Check: resource.ComposeAggregateTestCheckFunc(
							contactPUTs(mock, 1),
							reorderUpdateSent(mock, tt.contact, tt.headers),
							stateAddressIDs(ids...),
							resource.TestCheckResourceAttr(addressIDsContact, "name", tt.contact),
							resource.TestCheckResourceAttr(addressIDsContact, "address.1.headers.content-type", tt.headers["content-type"].(string)),
							stateMatchesMock(mock),
						),
					},
				},
			})
		})
	}
}

// reorderUpdateSent checks the only contact write the mock received: a PUT
// with the contact's name, every seeded address under its own ID with its
// type and address, nothing under `newaddresses`, and the webhook's headers.
func reorderUpdateSent(mock *testutil.MockNodePingServer, name string, headers map[string]interface{}) resource.TestCheckFunc {
	return func(*terraform.State) error {
		writes := mock.ContactWrites()
		if len(writes) != 1 || writes[0].Method != http.MethodPut {
			return fmt.Errorf("the mock received %d contact writes, want one PUT", len(writes))
		}
		body := writes[0].Body
		if body["name"] != name {
			return fmt.Errorf("the update sent the name %v, want %q", body["name"], name)
		}
		if _, ok := body["newaddresses"]; ok {
			return fmt.Errorf("the update sent newaddresses, want none")
		}
		sent, _ := body["addresses"].(map[string]interface{})
		if len(sent) != len(reorderConfigured) {
			return fmt.Errorf("the update sent %d addresses, want %d", len(sent), len(reorderConfigured))
		}
		for _, a := range reorderConfigured {
			addr, ok := sent[a.id].(map[string]interface{})
			if !ok {
				return fmt.Errorf("the update did not send address %s", a.id)
			}
			if addr["type"] != a.stored["type"] || addr["address"] != a.stored["address"] {
				return fmt.Errorf("the update sent address %s as %v:%v, want %v:%v",
					a.id, addr["type"], addr["address"], a.stored["type"], a.stored["address"])
			}
		}
		hook, _ := sent[reorderHook.id].(map[string]interface{})
		if !reflect.DeepEqual(hook["headers"], headers) {
			return fmt.Errorf("the update sent the webhook's headers as %v, want %v", hook["headers"], headers)
		}
		return nil
	}
}
