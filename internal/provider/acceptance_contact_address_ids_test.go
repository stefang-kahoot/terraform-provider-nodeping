package provider_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// Contact groups and checks name contact addresses by their ID, so an address
// has to keep its ID for as long as the configuration still describes it,
// whatever happens to the blocks around it. Each test applies a contact,
// records the ID NodePing gave every address, changes the configuration and
// checks where the IDs went -- in state and in the mock. After every step the
// test framework also re-plans the same configuration and fails unless the
// plan is empty.

const addressIDsContact = "nodeping_contact.ids"

type accAddress struct{ typ, value string }

func (a accAddress) key() string { return a.typ + ":" + a.value }

func addressIDsConfig(url, name string, addrs ...accAddress) string {
	var b strings.Builder
	for _, a := range addrs {
		fmt.Fprintf(&b, "\n  address {\n    type    = %q\n    address = %q\n  }\n", a.typ, a.value)
	}
	return providerConfig(url) + fmt.Sprintf(`
resource "nodeping_contact" "ids" {
  name = %q
%s}
`, name, b.String())
}

type stateAddress struct{ key, id string }

// contactAddressesInState returns the contact's ID and its addresses in their
// configured order.
func contactAddressesInState(s *terraform.State) (string, []stateAddress, error) {
	rs, ok := s.RootModule().Resources[addressIDsContact]
	if !ok {
		return "", nil, fmt.Errorf("%s is not in state", addressIDsContact)
	}
	attrs := rs.Primary.Attributes
	var addrs []stateAddress
	for i := 0; ; i++ {
		typ, ok := attrs[fmt.Sprintf("address.%d.type", i)]
		if !ok {
			break
		}
		addrs = append(addrs, stateAddress{
			key: typ + ":" + attrs[fmt.Sprintf("address.%d.address", i)],
			id:  attrs[fmt.Sprintf("address.%d.id", i)],
		})
	}
	return rs.Primary.ID, addrs, nil
}

func idInState(addrs []stateAddress, key string) (string, error) {
	for _, a := range addrs {
		if a.key == key {
			return a.id, nil
		}
	}
	return "", fmt.Errorf("no address %s in state", key)
}

// recordAddressIDs remembers each address's ID under its type:address.
func recordAddressIDs(into map[string]string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		_, addrs, err := contactAddressesInState(s)
		if err != nil {
			return err
		}
		for _, a := range addrs {
			if a.id == "" {
				return fmt.Errorf("address %s has no ID", a.key)
			}
			into[a.key] = a.id
		}
		return nil
	}
}

// stateMatchesMock checks that NodePing holds exactly the addresses in state,
// each under the ID state gives it: no address left behind, none missing, and
// no ID pointing at a different address than state says.
func stateMatchesMock(mock *testutil.MockNodePingServer) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		contactID, addrs, err := contactAddressesInState(s)
		if err != nil {
			return err
		}
		contact, ok := mock.GetContact(contactID)
		if !ok {
			return fmt.Errorf("contact %s is not in the mock", contactID)
		}
		stored, _ := contact["addresses"].(map[string]interface{})
		if len(stored) != len(addrs) {
			return fmt.Errorf("the mock holds %d addresses, state %d", len(stored), len(addrs))
		}
		for _, a := range addrs {
			raw, ok := stored[a.id].(map[string]interface{})
			if !ok {
				return fmt.Errorf("state gives %s the ID %s, which the mock does not hold", a.key, a.id)
			}
			if got := fmt.Sprintf("%v:%v", raw["type"], raw["address"]); got != a.key {
				return fmt.Errorf("state gives %s the ID %s, which the mock holds as %s", a.key, a.id, got)
			}
		}
		return nil
	}
}

// keepsID checks that the address now configured as now has the ID the
// address configured as was had before -- the same address, or the same one
// edited in place.
func keepsID(before map[string]string, was, now accAddress) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		_, addrs, err := contactAddressesInState(s)
		if err != nil {
			return err
		}
		id, err := idInState(addrs, now.key())
		if err != nil {
			return err
		}
		if id != before[was.key()] {
			return fmt.Errorf("%s has the ID %s, want %s, the ID %s had", now.key(), id, before[was.key()], was.key())
		}
		return nil
	}
}

// getsNewID checks that the address has an ID none of the earlier addresses had.
func getsNewID(before map[string]string, a accAddress) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		_, addrs, err := contactAddressesInState(s)
		if err != nil {
			return err
		}
		id, err := idInState(addrs, a.key())
		if err != nil {
			return err
		}
		for key, old := range before {
			if id == old {
				return fmt.Errorf("%s has the ID %s, which belonged to %s", a.key(), id, key)
			}
		}
		return nil
	}
}

// goneFromMock checks that the ID an address had is no longer in NodePing.
func goneFromMock(mock *testutil.MockNodePingServer, before map[string]string, a accAddress) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		contactID, _, err := contactAddressesInState(s)
		if err != nil {
			return err
		}
		contact, _ := mock.GetContact(contactID)
		stored, _ := contact["addresses"].(map[string]interface{})
		if _, ok := stored[before[a.key()]]; ok {
			return fmt.Errorf("the ID %s that %s had is still in the mock", before[a.key()], a.key())
		}
		return nil
	}
}

var (
	accEmailA   = accAddress{"email", "a@example.com"}
	accEmailB   = accAddress{"email", "b@example.com"}
	accEmailC   = accAddress{"email", "c@example.com"}
	accSMS      = accAddress{"sms", "+15550000001"}
	accHookOne  = accAddress{"webhook", "https://hooks.example.com/one"}
	accHookTwo  = accAddress{"webhook", "https://hooks.example.com/two"}
	accSlackOne = accAddress{"slack", "https://hooks.example.com/one"}
)

// Adding an address used to fail the apply: the new block's ID was planned
// null instead of unknown, and NodePing then gave it one ("Provider produced
// inconsistent result after apply"). NodePing had added the address all the
// same.
func TestAccContactResource_addAddressKeepsIDs(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	before := map[string]string{}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: addressIDsConfig(mock.URL(), "acc-ids", accEmailA, accSMS),
				Check:  resource.ComposeAggregateTestCheckFunc(recordAddressIDs(before), stateMatchesMock(mock)),
			},
			{
				Config: addressIDsConfig(mock.URL(), "acc-ids", accEmailA, accSMS, accEmailC),
				Check: resource.ComposeAggregateTestCheckFunc(
					keepsID(before, accEmailA, accEmailA),
					keepsID(before, accSMS, accSMS),
					getsNewID(before, accEmailC),
					stateMatchesMock(mock),
				),
			},
		},
	})
}

// Removing the first of two addresses used to hand the remaining one the
// removed one's ID: the update wrote b under a's ID and NodePing deleted b's.
// The apply succeeded, and every contact group that named either ID now
// pointed somewhere else.
func TestAccContactResource_removeFirstAddressKeepsIDs(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	before := map[string]string{}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: addressIDsConfig(mock.URL(), "acc-ids", accEmailA, accEmailB),
				Check:  resource.ComposeAggregateTestCheckFunc(recordAddressIDs(before), stateMatchesMock(mock)),
			},
			{
				Config: addressIDsConfig(mock.URL(), "acc-ids", accEmailB),
				Check: resource.ComposeAggregateTestCheckFunc(
					keepsID(before, accEmailB, accEmailB),
					goneFromMock(mock, before, accEmailA),
					stateMatchesMock(mock),
				),
			},
		},
	})
}

// Editing an address in place -- a rotated webhook URL -- keeps its ID, which
// NodePing supports, so groups naming it keep notifying it.
func TestAccContactResource_changeAddressInPlaceKeepsID(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	before := map[string]string{}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: addressIDsConfig(mock.URL(), "acc-ids", accEmailA, accHookOne),
				Check:  resource.ComposeAggregateTestCheckFunc(recordAddressIDs(before), stateMatchesMock(mock)),
			},
			{
				Config: addressIDsConfig(mock.URL(), "acc-ids", accEmailA, accHookTwo),
				Check: resource.ComposeAggregateTestCheckFunc(
					keepsID(before, accEmailA, accEmailA),
					keepsID(before, accHookOne, accHookTwo),
					stateMatchesMock(mock),
				),
			},
		},
	})
}

// Reordering the blocks moves the addresses, not their IDs.
func TestAccContactResource_swapAddressesKeepsIDs(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	before := map[string]string{}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: addressIDsConfig(mock.URL(), "acc-ids", accEmailA, accEmailB),
				Check:  resource.ComposeAggregateTestCheckFunc(recordAddressIDs(before), stateMatchesMock(mock)),
			},
			{
				Config: addressIDsConfig(mock.URL(), "acc-ids", accEmailB, accEmailA),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addressIDsContact, "address.0.address", accEmailB.value),
					resource.TestCheckResourceAttr(addressIDsContact, "address.1.address", accEmailA.value),
					keepsID(before, accEmailA, accEmailA),
					keepsID(before, accEmailB, accEmailB),
					stateMatchesMock(mock),
				),
			},
		},
	})
}

// Changing an address's type replaces it: what NodePing does with a type
// changed in place has not been probed, so the provider does not ask it to.
func TestAccContactResource_changeAddressTypeGetsNewID(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	before := map[string]string{}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: addressIDsConfig(mock.URL(), "acc-ids", accEmailA, accHookOne),
				Check:  resource.ComposeAggregateTestCheckFunc(recordAddressIDs(before), stateMatchesMock(mock)),
			},
			{
				Config: addressIDsConfig(mock.URL(), "acc-ids", accEmailA, accSlackOne),
				Check: resource.ComposeAggregateTestCheckFunc(
					keepsID(before, accEmailA, accEmailA),
					getsNewID(before, accSlackOne),
					goneFromMock(mock, before, accHookOne),
					stateMatchesMock(mock),
				),
			},
		},
	})
}

// An update that touches only the contact itself plans every address ID
// unknown before the provider carries them over, so it must carry all of them.
func TestAccContactResource_renameKeepsAddressIDs(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	before := map[string]string{}
	renamed := addressIDsConfig(mock.URL(), "acc-ids-renamed", accEmailA, accSMS)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: addressIDsConfig(mock.URL(), "acc-ids", accEmailA, accSMS),
				Check:  resource.ComposeAggregateTestCheckFunc(recordAddressIDs(before), stateMatchesMock(mock)),
			},
			{
				Config: renamed,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addressIDsContact, "name", "acc-ids-renamed"),
					keepsID(before, accEmailA, accEmailA),
					keepsID(before, accSMS, accSMS),
					stateMatchesMock(mock),
				),
			},
			{Config: renamed, PlanOnly: true},
		},
	})
}

// Two identical addresses can only be told apart by their IDs. Here the
// second step leaves the higher ID first, and the third edits both blocks in
// place to the same value: each keeps the ID at its position, so the result
// has to be read back by ID, not by value, or the two swap.
func TestAccContactResource_identicalAddressesKeepTheirIDs(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	before := map[string]string{}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: addressIDsConfig(mock.URL(), "acc-ids", accEmailA, accEmailB),
				Check:  resource.ComposeAggregateTestCheckFunc(recordAddressIDs(before), stateMatchesMock(mock)),
			},
			{
				Config: addressIDsConfig(mock.URL(), "acc-ids", accEmailB, accEmailA),
				Check:  stateMatchesMock(mock),
			},
			{
				Config: addressIDsConfig(mock.URL(), "acc-ids", accEmailC, accEmailC),
				Check: resource.ComposeAggregateTestCheckFunc(
					func(s *terraform.State) error {
						_, addrs, err := contactAddressesInState(s)
						if err != nil {
							return err
						}
						if len(addrs) != 2 || addrs[0].id != before[accEmailB.key()] || addrs[1].id != before[accEmailA.key()] {
							return fmt.Errorf("address IDs are %v, want [%s %s]", addrs, before[accEmailB.key()], before[accEmailA.key()])
						}
						return nil
					},
					stateMatchesMock(mock),
				),
			},
		},
	})
}
