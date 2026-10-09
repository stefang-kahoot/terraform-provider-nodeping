package provider_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// NodePing changes two of a check's computed attributes on its own: state
// whenever the check goes down or comes back up, and modified whenever
// anything writes to the check. A plan pins both to what the refresh read.
var checkChangesBetweenPlanAndApply = []struct {
	name          string
	attribute     string
	before, after int64
}{
	{name: "check goes down", attribute: "state", before: 1, after: 0},
	{name: "check comes back up", attribute: "state", before: 0, after: 1},
	{name: "check modified elsewhere", attribute: "modified", before: 1609459200000, after: 1791462833954},
}

// changeCheckBeforeApply sets a field of the check in NodePing after the plan
// is made and before it is applied. The test harness applies the saved plan
// without refreshing, so the apply starts from a plan that knows nothing of
// the change, as a check does when it goes down while a long apply works
// through the checks before it.
type changeCheckBeforeApply struct {
	mock  *testutil.MockNodePingServer
	id    *string
	field string
	value interface{}
}

func (c changeCheckBeforeApply) CheckPlan(context.Context, plancheck.CheckPlanRequest, *plancheck.CheckPlanResponse) {
	c.mock.SetCheckField(*c.id, c.field, c.value)
}

// expectMockCheckField checks that the state holds a top-level field of the
// check as the mock holds it.
func expectMockCheckField(mock *testutil.MockNodePingServer, id *string, field string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		check, ok := mock.GetCheck(*id)
		if !ok {
			return fmt.Errorf("check %s is not in the mock", *id)
		}
		return resource.TestCheckResourceAttr("nodeping_check.test", field, fmt.Sprint(check[field]))(s)
	}
}

// An update of a check whose state or modified NodePing changed after the plan
// was made keeps the planned value, so the apply does not fail with "Provider
// produced inconsistent result after apply". The next refresh reads the new
// value and plans nothing.
func TestAccCheckResource_updateKeepsValuesNodePingChangedAfterThePlan(t *testing.T) {
	for _, tt := range checkChangesBetweenPlanAndApply {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)

			config := func(label string) string {
				return providerConfig(mock.URL()) + fmt.Sprintf(`
resource "nodeping_check" "test" {
  type   = "HTTP"
  target = "https://example.com"
  label  = %q
}
`, label)
			}

			var id string
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: config("acc-state"),
						Check:  storeCheckID("nodeping_check.test", &id),
					},
					{
						PreConfig: func() { mock.SetCheckField(id, tt.attribute, tt.before) },
						Config:    config("acc-state-renamed"),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{
								plancheck.ExpectResourceAction("nodeping_check.test", plancheck.ResourceActionUpdate),
								plancheck.ExpectKnownValue("nodeping_check.test", tfjsonpath.New(tt.attribute), knownvalue.Int64Exact(tt.before)),
								changeCheckBeforeApply{mock: mock, id: &id, field: tt.attribute, value: tt.after},
							},
						},
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr("nodeping_check.test", "label", "acc-state-renamed"),
							// The planned value, until the next refresh.
							resource.TestCheckResourceAttr("nodeping_check.test", tt.attribute, strconv.FormatInt(tt.before, 10)),
						),
					},
					{
						Config: config("acc-state-renamed"),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
						// What NodePing holds: tt.after, or for modified the
						// time of the update made after it.
						Check: expectMockCheckField(mock, &id, tt.attribute),
					},
				},
			})
		})
	}
}
