package provider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// NodePing keeps a webhook's action and a Pushover address's priority when an
// update leaves them out, so removing either failed the apply with "Provider
// produced inconsistent result after apply" (#33). Probed on 2026-10-12:
// nothing clears an action ("", null, false and 0 keep it), while null clears
// a priority. The tests use the contact of acceptance_contact_address_clear_test.go,
// nodeping_contact.clear, and its helpers.

// addrActionError matches the plan error for removing a webhook's action.
// Terraform wraps long lines, so any run of whitespace matches a space.
var addrActionError = regexp.MustCompile(`NodePing\s+cannot\s+clear\s+a\s+webhook's\s+action`)

// addrPriorityConfig is a contact with one Pushover address whose block holds
// the given attribute lines.
func addrPriorityConfig(url, attrs string) string {
	return providerConfig(url) + fmt.Sprintf(`
resource "nodeping_contact" "clear" {
  name = "acc-address-clear"

  address {
    type    = "pushover"
    address = "FAKE-pushover-user-key"
%s  }
}
`, attrs)
}

// Removing action fails the plan, before anything is sent, and says to set the
// method wanted. Changing it to another method applies.
func TestAccContactResource_addressActionRemovalFailsAtPlan(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	var snapshot string
	post := addrClearConfig(mock.URL(), "    action = \"post\"\n")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: post,
				Check: resource.ComposeAggregateTestCheckFunc(
					addrClearStored(mock, "action", `"post"`),
					addrClearSnapshot(mock, &snapshot),
				),
			},
			{
				Config:      addrClearConfig(mock.URL(), ""),
				ExpectError: addrActionError,
			},
			{
				Config: post,
				Check: resource.ComposeAggregateTestCheckFunc(
					addrClearUnchanged(mock, &snapshot),
					// Only the create: the failed plan sent nothing.
					contactWrites(mock, 1),
				),
			},
			{
				Config: addrClearConfig(mock.URL(), "    action = \"get\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addrClearContact, "address.0.action", "get"),
					addrClearSent(mock, "action", `"get"`),
					addrClearStored(mock, "action", `"get"`),
				),
			},
			addrClearImport(),
		},
	})
}

// Removing priority sends null, which leaves the address holding none. A
// later update leaves it out again.
func TestAccContactResource_addressPriorityRemoved(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: addrPriorityConfig(mock.URL(), "    priority = 1\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addrClearContact, "address.0.priority", "1"),
					addrClearStored(mock, "priority", `1`),
				),
			},
			{
				Config: addrPriorityConfig(mock.URL(), ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(addrClearContact, "address.0.priority"),
					addrClearSent(mock, "priority", `null`),
					addrClearStored(mock, "priority", `null`),
				),
			},
			addrClearImport(),
			{
				Config: addrPriorityConfig(mock.URL(), "    suppress_up = true\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					addrClearSent(mock, "priority", addrClearLeftOut),
					addrClearStored(mock, "priority", `null`),
				),
			},
		},
	})
}
