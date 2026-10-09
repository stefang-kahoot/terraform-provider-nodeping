package provider_test

import (
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/provider"
	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// NodePing sometimes answers an update of a check with the check as it was
// before the update, old modified and all, although it applied the update; a
// read seconds later shows it (finding 41). Mapped onto the plan, such an
// answer failed the apply with "Provider produced inconsistent result after
// apply".

// staleReadBackWaits stand in for the 1, 2, 4 and 8 seconds the provider
// waits for NodePing to show an update.
var staleReadBackWaits = []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond}

func staleUpdateProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"nodeping": providerserver.NewProtocol6WithError(provider.NewWithReadBackWaits("test", staleReadBackWaits...)()),
	}
}

func staleUpdateConfig(url, label string, interval int) string {
	return providerConfig(url) + fmt.Sprintf(`
resource "nodeping_check" "test" {
  type     = "HTTP"
  target   = "https://example.com"
  label    = %q
  interval = %d
}
`, label, interval)
}

// readsAfterUpdate counts the reads of check id since its last update.
func readsAfterUpdate(mock *testutil.MockNodePingServer, id string) int {
	path := "/checks/" + id
	reads := 0
	for _, r := range mock.Requests() {
		switch {
		case r.Path != path:
		case r.Method == "PUT":
			reads = 0
		case r.Method == "GET":
			reads++
		}
	}
	return reads
}

// expectReadsAfterUpdate checks how often the apply read the check after
// updating it. Step checks run straight after the apply, before the harness
// plans again.
func expectReadsAfterUpdate(mock *testutil.MockNodePingServer, id *string, want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := readsAfterUpdate(mock, *id); got != want {
			return fmt.Errorf("the check was read %d times after its update, want %d", got, want)
		}
		return nil
	}
}

// A stale answer is read again until NodePing shows the update: the apply
// succeeds with the planned values and the next plan is empty.
func TestAccCheckResource_updateReadsAStaleAnswerAgain(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	var id string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: staleUpdateProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: staleUpdateConfig(mock.URL(), "acc-stale", 5),
				Check:  storeCheckID("nodeping_check.test", &id),
			},
			{
				PreConfig: func() { mock.AnswerStaleUpdates(id, 1) },
				Config:    staleUpdateConfig(mock.URL(), "acc-stale-renamed", 10),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.test", "label", "acc-stale-renamed"),
					resource.TestCheckResourceAttr("nodeping_check.test", "interval", "10"),
					expectReadsAfterUpdate(mock, &id, 1),
				),
			},
		},
	})
}

// A stale answer that already gives the planned values is used as it is:
// NodePing already held them, so there is nothing to wait for.
func TestAccCheckResource_updateUsesAStaleAnswerThatGivesThePlan(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	var id string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: staleUpdateProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: staleUpdateConfig(mock.URL(), "acc-stale", 5),
				Check:  storeCheckID("nodeping_check.test", &id),
			},
			{
				PreConfig: func() { mock.AnswerStaleUpdates(id, 1) },
				Config:    staleUpdateConfig(mock.URL(), "acc-stale-renamed", 5),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("nodeping_check.test", plancheck.ResourceActionUpdate),
						// The same rename, made in the web interface after
						// the plan: the update then changes nothing.
						changeCheckBeforeApply{mock: mock, id: &id, field: "label", value: "acc-stale-renamed"},
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.test", "label", "acc-stale-renamed"),
					expectReadsAfterUpdate(mock, &id, 0),
				),
			},
		},
	})
}

// When NodePing does not show the update in time, the apply fails with an
// error that names the check and says what to do, and leaves the state as it
// was. The next plan reads the check afresh: here NodePing has applied the
// update by then, so it plans nothing.
func TestAccCheckResource_updateNotShownInTime(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	var id string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: staleUpdateProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: staleUpdateConfig(mock.URL(), "acc-stale", 5),
				Check:  storeCheckID("nodeping_check.test", &id),
			},
			{
				PreConfig: func() {
					mock.AnswerStaleUpdates(id, 1)
					mock.AnswerStaleReads(id, len(staleReadBackWaits))
				},
				Config: staleUpdateConfig(mock.URL(), "acc-stale-renamed", 10),
				// Terraform wraps the lines of an error where it likes.
				ExpectError: regexp.MustCompile(`NodePing\s+Has\s+Not\s+Shown\s+the\s+Update\s+Yet[\s\S]*` +
					`check\s+MOCK-CHECK-\d+\s+\("acc-stale-renamed"\)[\s\S]*` +
					`3\s+reads\s+over\s+70ms[\s\S]*` +
					`Run\s+terraform\s+plan\s+again`),
			},
			{
				PreConfig: func() {
					if got := readsAfterUpdate(mock, id); got != len(staleReadBackWaits) {
						t.Errorf("the check was read %d times after its update, want %d", got, len(staleReadBackWaits))
					}
				},
				Config: staleUpdateConfig(mock.URL(), "acc-stale-renamed", 10),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.test", "label", "acc-stale-renamed"),
					resource.TestCheckResourceAttr("nodeping_check.test", "interval", "10"),
				),
			},
		},
	})
}

// A fresh answer is used as it is, without reading the check again.
func TestAccCheckResource_updateTrustsAFreshAnswer(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	var id string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: staleUpdateProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: staleUpdateConfig(mock.URL(), "acc-stale", 5),
				Check:  storeCheckID("nodeping_check.test", &id),
			},
			{
				Config: staleUpdateConfig(mock.URL(), "acc-stale-renamed", 10),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.test", "label", "acc-stale-renamed"),
					expectReadsAfterUpdate(mock, &id, 0),
				),
			},
		},
	})
}
