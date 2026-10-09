package provider_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// NodePing merges an update into a contact's address field by field: what the
// update leaves out, it keeps (finding 32). Switching a suppress flag off, or
// removing an address's headers or query strings, therefore has to send
// false or {}; leaving them out failed the apply with "Provider produced
// inconsistent result after apply". The mock merges the same way. After every
// step the test framework also plans the same configuration again and fails
// unless the plan is empty.

const addrClearContact = "nodeping_contact.clear"

// addrClearLeftOut stands for a key the address does not have.
const addrClearLeftOut = "<left out>"

// addrClearConfig is a contact with one webhook address whose block holds the
// given attribute lines.
func addrClearConfig(url, attrs string) string {
	return providerConfig(url) + fmt.Sprintf(`
resource "nodeping_contact" "clear" {
  name = "acc-address-clear"

  address {
    type    = "webhook"
    address = "https://hooks.example.com/clear"
%s  }
}
`, attrs)
}

// addrClearIDs returns the contact's ID and its first address's ID from state.
func addrClearIDs(s *terraform.State) (string, string, error) {
	rs, ok := s.RootModule().Resources[addrClearContact]
	if !ok {
		return "", "", fmt.Errorf("%s is not in state", addrClearContact)
	}
	return rs.Primary.ID, rs.Primary.Attributes["address.0.id"], nil
}

// addrClearJSON renders a value as JSON, or addrClearLeftOut if it is absent.
func addrClearJSON(v interface{}, present bool) (string, error) {
	if !present {
		return addrClearLeftOut, nil
	}
	out, err := json.Marshal(v)
	return string(out), err
}

// addrClearStored checks one field of the address as the mock holds it, as
// JSON.
func addrClearStored(mock *testutil.MockNodePingServer, key, want string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		contactID, addrID, err := addrClearIDs(s)
		if err != nil {
			return err
		}
		contact, ok := mock.GetContact(contactID)
		if !ok {
			return fmt.Errorf("contact %s is not in the mock", contactID)
		}
		addresses, _ := contact["addresses"].(map[string]interface{})
		addr, ok := addresses[addrID].(map[string]interface{})
		if !ok {
			return fmt.Errorf("the mock holds no address %s", addrID)
		}
		v, present := addr[key]
		got, err := addrClearJSON(v, present)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("the mock holds %s %s, want %s", key, got, want)
		}
		return nil
	}
}

// addrClearSent checks one field of the address as the last update sent it
// under the address's ID, as JSON.
func addrClearSent(mock *testutil.MockNodePingServer, key, want string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		_, addrID, err := addrClearIDs(s)
		if err != nil {
			return err
		}
		writes := mock.ContactWrites()
		if len(writes) == 0 {
			return fmt.Errorf("no contact was written")
		}
		last := writes[len(writes)-1]
		addresses, _ := last.Body["addresses"].(map[string]interface{})
		addr, ok := addresses[addrID].(map[string]interface{})
		if !ok {
			return fmt.Errorf("the last %s did not send address %s under its ID", last.Method, addrID)
		}
		v, present := addr[key]
		got, err := addrClearJSON(v, present)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("the last %s sent %s %s, want %s", last.Method, key, got, want)
		}
		return nil
	}
}

// addrClearImport imports the contact and compares it with the state the
// previous step left.
func addrClearImport() resource.TestStep {
	return resource.TestStep{
		ResourceName:      addrClearContact,
		ImportState:       true,
		ImportStateVerify: true,
	}
}

var addrClearFlags = []struct{ attr, key string }{
	{"suppress_up", "suppressup"},
	{"suppress_down", "suppressdown"},
	{"suppress_first", "suppressfirst"},
	{"suppress_diag", "suppressdiag"},
	{"suppress_all", "suppressall"},
}

// Switching suppress flags off -- by removing them, which plans their default
// false -- sends false for each, and NodePing stores it. Left out, NodePing
// kept true: "address[0].suppress_up: was cty.False, but now cty.True".
func TestAccContactResource_addressSuppressFlagsSwitchedOff(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	var on string
	var stored, cleared, sent []resource.TestCheckFunc
	for _, f := range addrClearFlags {
		on += fmt.Sprintf("    %s = true\n", f.attr)
		stored = append(stored,
			resource.TestCheckResourceAttr(addrClearContact, "address.0."+f.attr, "true"),
			addrClearStored(mock, f.key, `true`))
		cleared = append(cleared,
			resource.TestCheckResourceAttr(addrClearContact, "address.0."+f.attr, "false"),
			addrClearStored(mock, f.key, `false`))
		sent = append(sent, addrClearSent(mock, f.key, `false`))
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: addrClearConfig(mock.URL(), on),
				Check:  resource.ComposeAggregateTestCheckFunc(stored...),
			},
			{
				Config: addrClearConfig(mock.URL(), ""),
				Check:  resource.ComposeAggregateTestCheckFunc(append(cleared, sent...)...),
			},
			addrClearImport(),
		},
	})
}

// Headers and query strings are replaced as a whole: a smaller map leaves out
// the removed entries, and removing the attribute sends {}. Left out,
// NodePing kept them: "address[0].headers: was null, but now ...". The
// cleared address holds {}, which reads back as no headers.
func TestAccContactResource_addressMapsRemoved(t *testing.T) {
	for _, attr := range []string{"headers", "querystrings"} {
		t.Run(attr, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: addrClearConfig(mock.URL(), fmt.Sprintf("    %s = {\n      one = \"1\"\n      two = \"2\"\n    }\n", attr)),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr(addrClearContact, "address.0."+attr+".%", "2"),
							addrClearStored(mock, attr, `{"one":"1","two":"2"}`),
						),
					},
					{
						Config: addrClearConfig(mock.URL(), fmt.Sprintf("    %s = {\n      one = \"1\"\n    }\n", attr)),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr(addrClearContact, "address.0."+attr+".%", "1"),
							addrClearSent(mock, attr, `{"one":"1"}`),
							addrClearStored(mock, attr, `{"one":"1"}`),
						),
					},
					{
						Config: addrClearConfig(mock.URL(), ""),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckNoResourceAttr(addrClearContact, "address.0."+attr+".%"),
							addrClearSent(mock, attr, `{}`),
							addrClearStored(mock, attr, `{}`),
						),
					},
					addrClearImport(),
				},
			})
		})
	}
}

// An address with no headers is read the way its block writes it: NodePing
// holds none either as {} or with no key at all, and `headers = {}` must read
// back as {} rather than null, and a removed attribute as null.
func TestAccContactResource_addressEmptyHeaders(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	const empty = "    headers = {}\n"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				// A new address leaves an empty map out.
				Config: addrClearConfig(mock.URL(), empty),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addrClearContact, "address.0.headers.%", "0"),
					addrClearStored(mock, "headers", addrClearLeftOut),
				),
			},
			{
				Config: addrClearConfig(mock.URL(), "    headers = {\n      one = \"1\"\n    }\n"),
				Check:  addrClearStored(mock, "headers", `{"one":"1"}`),
			},
			{
				Config: addrClearConfig(mock.URL(), empty),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addrClearContact, "address.0.headers.%", "0"),
					addrClearSent(mock, "headers", `{}`),
					addrClearStored(mock, "headers", `{}`),
				),
			},
			{
				// Nothing to clear: the update leaves headers out.
				Config: addrClearConfig(mock.URL(), ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(addrClearContact, "address.0.headers.%"),
					addrClearSent(mock, "headers", addrClearLeftOut),
					addrClearStored(mock, "headers", `{}`),
				),
			},
		},
	})
}
