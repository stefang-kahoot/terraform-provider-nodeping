package provider_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// An update replaces a contact's addresses only when it sends `addresses`:
// without the key NodePing keeps them all and adds any `newaddresses`. And
// nothing can take a contact's last address away; NodePing refuses that
// update (the mock does the same).

// lastAddressError matches the plan error for removing a contact's last
// address. Terraform wraps long lines, so any run of whitespace matches a space.
var lastAddressError = regexp.MustCompile(`NodePing\s+cannot\s+remove\s+a\s+contact's\s+last\s+address`)

// lastWriteSent checks the contact's latest create or update in the mock: its
// method, how many addresses it sent under `addresses` and how many under
// `newaddresses`, where -1 means the key was left out altogether.
func lastWriteSent(mock *testutil.MockNodePingServer, method string, addresses, newAddresses int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		writes := mock.ContactWrites()
		if len(writes) == 0 {
			return fmt.Errorf("no contact was written")
		}
		last := writes[len(writes)-1]
		if last.Method != method {
			return fmt.Errorf("the last contact write was a %s, want a %s", last.Method, method)
		}
		for key, want := range map[string]int{"addresses": addresses, "newaddresses": newAddresses} {
			raw, sent := last.Body[key]
			got := -1
			switch v := raw.(type) {
			case map[string]interface{}:
				got = len(v)
			case []interface{}:
				got = len(v)
			default:
				if sent {
					return fmt.Errorf("the last %s sent %s as %T", method, key, raw)
				}
			}
			if got != want {
				return fmt.Errorf("the last %s sent %d under %s, want %d (-1: left out)", method, got, key, want)
			}
		}
		return nil
	}
}

// contactWrites checks how many creates and updates the mock has received.
func contactWrites(mock *testutil.MockNodePingServer, want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := len(mock.ContactWrites()); got != want {
			return fmt.Errorf("the mock received %d contact writes, want %d", got, want)
		}
		return nil
	}
}

// snapshotContact records the contact as the mock holds it, to compare later.
func snapshotContact(mock *testutil.MockNodePingServer, into *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		contactID, _, err := contactAddressesInState(s)
		if err != nil {
			return err
		}
		contact, ok := mock.GetContact(contactID)
		if !ok {
			return fmt.Errorf("contact %s is not in the mock", contactID)
		}
		body, err := json.Marshal(contact)
		*into = string(body)
		return err
	}
}

func contactUnchanged(mock *testutil.MockNodePingServer, snapshot *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		var now string
		if err := snapshotContact(mock, &now)(s); err != nil {
			return err
		}
		if now != *snapshot {
			return fmt.Errorf("the contact changed in the mock")
		}
		return nil
	}
}

// Replacing every address with one of another type used to fail the apply:
// no planned ID was kept, so the update sent only `newaddresses`, NodePing
// kept the webhook next to the new email, and the result had one address more
// than the plan ("Provider produced inconsistent result after apply").
func TestAccContactResource_replaceEveryAddress(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	before := map[string]string{}
	replaced := addressIDsConfig(mock.URL(), "acc-replace", accEmailA)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: addressIDsConfig(mock.URL(), "acc-replace", accHookOne),
				Check:  resource.ComposeAggregateTestCheckFunc(recordAddressIDs(before), stateMatchesMock(mock)),
			},
			{
				Config: replaced,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addressIDsContact, "address.#", "1"),
					lastWriteSent(mock, http.MethodPut, 0, 1),
					getsNewID(before, accEmailA),
					goneFromMock(mock, before, accHookOne),
					stateMatchesMock(mock),
				),
			},
			{Config: replaced, PlanOnly: true},
		},
	})
}

// NodePing refuses an update that would leave a contact with no address, so
// the plan fails instead, before anything is sent, and says what to do.
func TestAccContactResource_removeLastAddressFailsAtPlan(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	before := map[string]string{}
	var snapshot string
	withAddress := addressIDsConfig(mock.URL(), "acc-last", accEmailA)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: withAddress,
				Check: resource.ComposeAggregateTestCheckFunc(
					recordAddressIDs(before),
					stateMatchesMock(mock),
					snapshotContact(mock, &snapshot),
				),
			},
			{
				Config:      addressIDsConfig(mock.URL(), "acc-last"),
				ExpectError: lastAddressError,
			},
			{
				Config: withAddress,
				Check: resource.ComposeAggregateTestCheckFunc(
					keepsID(before, accEmailA, accEmailA),
					stateMatchesMock(mock),
					contactUnchanged(mock, &snapshot),
					// Only the create: neither the failed plan nor this
					// one sent anything.
					contactWrites(mock, 1),
				),
			},
		},
	})
}

// The address list can also be empty only once it is known, at apply time:
// here it comes from a resource created in the same apply. The plan cannot
// tell, but the apply re-plans with the list known and fails the same way.
func TestAccContactResource_removeLastAddressKnownAtApply(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	var snapshot string
	withAddress := addressIDsConfig(mock.URL(), "acc-last", accEmailA)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: withAddress,
				Check:  resource.ComposeAggregateTestCheckFunc(stateMatchesMock(mock), snapshotContact(mock, &snapshot)),
			},
			{
				Config: providerConfig(mock.URL()) + `
resource "terraform_data" "addresses" {
  input = []
}

resource "nodeping_contact" "ids" {
  name = "acc-last"

  dynamic "address" {
    for_each = terraform_data.addresses.output
    content {
      type    = address.value.type
      address = address.value.address
    }
  }
}
`,
				ExpectError: lastAddressError,
			},
			{
				Config: withAddress,
				Check: resource.ComposeAggregateTestCheckFunc(
					stateMatchesMock(mock),
					contactUnchanged(mock, &snapshot),
					contactWrites(mock, 1),
				),
			},
		},
	})
}

// A contact need not have an address: NodePing creates one without, if the
// create leaves `newaddresses` out, and an update that sends no address keys
// leaves it that way. It can gain an address later -- and then not lose its
// last one again.
func TestAccContactResource_withoutAddresses(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	before := map[string]string{}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: addressIDsConfig(mock.URL(), "acc-bare"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addressIDsContact, "address.#", "0"),
					lastWriteSent(mock, http.MethodPost, -1, -1),
					stateMatchesMock(mock),
				),
			},
			{
				Config: addressIDsConfig(mock.URL(), "acc-bare-renamed"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addressIDsContact, "name", "acc-bare-renamed"),
					resource.TestCheckResourceAttr(addressIDsContact, "address.#", "0"),
					lastWriteSent(mock, http.MethodPut, -1, -1),
					stateMatchesMock(mock),
				),
			},
			{
				Config: addressIDsConfig(mock.URL(), "acc-bare-renamed", accEmailA),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addressIDsContact, "address.#", "1"),
					lastWriteSent(mock, http.MethodPut, 0, 1),
					recordAddressIDs(before),
					stateMatchesMock(mock),
				),
			},
			{
				Config: addressIDsConfig(mock.URL(), "acc-bare-again", accEmailA),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addressIDsContact, "name", "acc-bare-again"),
					lastWriteSent(mock, http.MethodPut, 1, -1),
					keepsID(before, accEmailA, accEmailA),
					stateMatchesMock(mock),
				),
			},
		},
	})
}
