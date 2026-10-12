package provider_test

import (
	"fmt"
	"regexp"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// NodePing stores any priority, 3 and -3 included (probed on 2026-10-12), so
// a priority outside Pushover's range applied cleanly. NodePing's docs give
// -2 to 2, and the plan now refuses anything else, on create and on update.
// The test uses nodeping_contact.clear, so that the helpers of
// acceptance_contact_address_clear_test.go can read the address.

// priorityRangeConfig is a contact with one Pushover address of the given
// priority.
func priorityRangeConfig(url string, priority int) string {
	return providerConfig(url) + fmt.Sprintf(`
resource "nodeping_contact" "clear" {
  name = "acc-address-clear"

  address {
    type     = "pushover"
    address  = "FAKE-pushover-user-key"
    priority = %d
  }
}
`, priority)
}

// priorityRangeError matches the plan error for a priority outside -2 to 2.
// Terraform wraps long lines, so any run of whitespace matches a space.
func priorityRangeError(priority int) *regexp.Regexp {
	return regexp.MustCompile(fmt.Sprintf(`address\[0\]\.priority\s+value\s+must\s+be\s+between\s+-2\s+and\s+2,\s+got:\s+%d`, priority))
}

func TestAccContactResource_addressPriorityRange(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	steps := []resource.TestStep{
		{Config: priorityRangeConfig(mock.URL(), 3), ExpectError: priorityRangeError(3)},
		{Config: priorityRangeConfig(mock.URL(), -3), ExpectError: priorityRangeError(-3)},
	}
	for _, priority := range []int{-2, -1, 0, 1, 2} {
		want := strconv.Itoa(priority)
		steps = append(steps, resource.TestStep{
			Config: priorityRangeConfig(mock.URL(), priority),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(addrClearContact, "address.0.priority", want),
				addrClearStored(mock, "priority", want),
			),
		})
	}
	steps = append(steps,
		resource.TestStep{Config: priorityRangeConfig(mock.URL(), 3), ExpectError: priorityRangeError(3)},
		resource.TestStep{
			Config: priorityRangeConfig(mock.URL(), 2),
			Check: resource.ComposeAggregateTestCheckFunc(
				addrClearStored(mock, "priority", `2`),
				// The create and the four updates to -1, 0, 1 and 2: no
				// refused plan sent anything.
				contactWrites(mock, 5),
			),
		},
	)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps:                    steps,
	})
}
