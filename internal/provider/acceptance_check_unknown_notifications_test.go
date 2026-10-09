package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// A dynamic notifications block over the address IDs of a contact created in
// the same run can leave the whole list of notifications blocks unknown at
// plan time. A bare `for` over the addresses does not: it plans as many
// blocks as addresses, each with an unknown contact_id. But distinct() over
// unknown IDs, a map keyed by them, or an `if` on one cannot tell how many
// there will be, and the block list is unknown. ModifyPlan decoded the whole
// plan into a model whose notifications is a plain slice, so planning
// tags_all failed the plan with "Value Conversion Error ... Path:
// notifications". tags_all is still planned in full, with any default_tags.
//
// The first step creates both; the second adds a contact, again created in
// the same run, to an existing check's notifications, which plans an update
// with the notifications unknown.
func TestAccCheckResource_notificationsNotKnownUntilApply(t *testing.T) {
	const contacts = `
resource "nodeping_contact" "oncall" {
  name = "acc-oncall"

  address {
    type    = "email"
    address = "oncall@example.com"
  }

  address {
    type    = "sms"
    address = "+15550000001"
  }
}
`

	const backup = `
resource "nodeping_contact" "backup" {
  name = "acc-backup"

  address {
    type    = "email"
    address = "backup@example.com"
  }
}
`

	check := func(forEach string) string {
		return `
resource "nodeping_check" "test" {
  type   = "HTTP"
  target = "https://example.com"
  label  = "acc-unknown-notifications"
  tags   = ["website"]

  dynamic "notifications" {
    for_each = ` + forEach + `
    content {
      contact_id = notifications.value
      delay      = 5
    }
  }
}
`
	}

	const oncallIDs = `distinct([for a in nodeping_contact.oncall.address : a.id])`
	const allIDs = `distinct([for a in concat(nodeping_contact.oncall.address, nodeping_contact.backup.address) : a.id])`

	tests := []struct {
		name     string
		provider func(url string) string
		tagsAll  []string
	}{
		{
			name:     "without default_tags",
			provider: providerConfig,
			tagsAll:  []string{"website"},
		},
		{
			name: "with default_tags",
			provider: func(url string) string {
				return fmt.Sprintf(`
provider "nodeping" {
  api_token    = "acc-test-token"
  api_url      = %q
  default_tags = ["managed-by-terraform"]
}
`, url)
			},
			tagsAll: []string{"managed-by-terraform", "website"},
		},
		{
			// ignore_mute adds the other half of ModifyPlan, which plans the
			// prior state's mute on the update.
			name: "with default_tags and ignore_mute",
			provider: func(url string) string {
				return fmt.Sprintf(`
provider "nodeping" {
  api_token    = "acc-test-token"
  api_url      = %q
  default_tags = ["managed-by-terraform"]
  ignore_mute  = true
}
`, url)
			},
			tagsAll: []string{"managed-by-terraform", "website"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)

			tagsAll := make([]knownvalue.Check, 0, len(tt.tagsAll))
			for _, tag := range tt.tagsAll {
				tagsAll = append(tagsAll, knownvalue.StringExact(tag))
			}

			// The plan is only a test of the fix while it leaves the
			// notifications unknown; tags_all is planned in full anyway.
			plannedWithUnknownNotifications := []plancheck.PlanCheck{
				plancheck.ExpectUnknownValue("nodeping_check.test", tfjsonpath.New("notifications")),
				plancheck.ExpectKnownValue("nodeping_check.test", tfjsonpath.New("tags_all"), knownvalue.ListExact(tagsAll)),
			}

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: tt.provider(mock.URL()) + contacts + check(oncallIDs),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: append([]plancheck.PlanCheck{
								plancheck.ExpectResourceAction("nodeping_check.test", plancheck.ResourceActionCreate),
							}, plannedWithUnknownNotifications...),
							PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr("nodeping_check.test", "notifications.#", "2"),
							resource.TestCheckResourceAttrPair("nodeping_check.test", "notifications.0.contact_id", "nodeping_contact.oncall", "address.0.id"),
							resource.TestCheckResourceAttrPair("nodeping_check.test", "notifications.1.contact_id", "nodeping_contact.oncall", "address.1.id"),
							resource.TestCheckResourceAttr("nodeping_check.test", "notifications.0.delay", "5"),
							resource.TestCheckResourceAttr("nodeping_check.test", "notifications.0.schedule", "All"),
							resource.TestCheckResourceAttr("nodeping_check.test", "tags.#", "1"),
							resource.TestCheckResourceAttr("nodeping_check.test", "tags_all.#", fmt.Sprint(len(tt.tagsAll))),
						),
					},
					{
						Config: tt.provider(mock.URL()) + contacts + backup + check(allIDs),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: append([]plancheck.PlanCheck{
								plancheck.ExpectResourceAction("nodeping_check.test", plancheck.ResourceActionUpdate),
							}, plannedWithUnknownNotifications...),
							PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr("nodeping_check.test", "notifications.#", "3"),
							resource.TestCheckResourceAttrPair("nodeping_check.test", "notifications.0.contact_id", "nodeping_contact.oncall", "address.0.id"),
							resource.TestCheckResourceAttrPair("nodeping_check.test", "notifications.1.contact_id", "nodeping_contact.oncall", "address.1.id"),
							resource.TestCheckResourceAttrPair("nodeping_check.test", "notifications.2.contact_id", "nodeping_contact.backup", "address.0.id"),
							resource.TestCheckResourceAttr("nodeping_check.test", "tags_all.#", fmt.Sprint(len(tt.tagsAll))),
						),
					},
				},
			})
		})
	}
}
