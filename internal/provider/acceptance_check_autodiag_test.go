package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// autodiagConfig is a check with the given autodiag.
func autodiagConfig(url string, autodiag bool) string {
	return providerConfig(url) + fmt.Sprintf(`
resource "nodeping_check" "test" {
  type     = "HTTP"
  target   = "https://example.com"
  label    = "acc-autodiag"
  autodiag = %t
}
`, autodiag)
}

// expectAutoDiagSent checks the autodiag of the last create, or of the last
// update of the check, as sent.
func expectAutoDiagSent(mock *testutil.MockNodePingServer, update bool, want string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		writes := mock.CheckCreates()
		what := "create"
		if update {
			rs, ok := s.RootModule().Resources["nodeping_check.test"]
			if !ok {
				return fmt.Errorf("nodeping_check.test is not in the state")
			}
			writes = mock.CheckUpdates(rs.Primary.ID)
			what = "update"
		}
		if len(writes) == 0 {
			return fmt.Errorf("no %s was sent", what)
		}
		if got := writes[len(writes)-1]["autodiag"]; got != want {
			return fmt.Errorf("the %s sent autodiag = %#v, want %#v", what, got, want)
		}
		return nil
	}
}

// The mock drops autodiag, as the probed account did: it holds false
// whatever it is sent. Switching autodiag on applies, and the plan after
// shows it again. It used to fail the apply with "Provider produced
// inconsistent result after apply: .autodiag: was cty.True, but now
// cty.False". autodiag goes out as the string NodePing documents.
func TestAccCheckResource_autodiagDropped(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	stillOn := []plancheck.PlanCheck{
		plancheck.ExpectResourceAction("nodeping_check.test", plancheck.ResourceActionUpdate),
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: autodiagConfig(mock.URL(), false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.test", "autodiag", "false"),
					expectAutoDiagSent(mock, false, "false"),
				),
			},
			{
				Config: autodiagConfig(mock.URL(), true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.test", "autodiag", "true"),
					expectAutoDiagSent(mock, true, "true"),
					expectStored(mock, "nodeping_check.test", "top", "autodiag", `false`),
				),
				ConfigPlanChecks:   resource.ConfigPlanChecks{PostApplyPostRefresh: stillOn},
				ExpectNonEmptyPlan: true,
			},
			{
				// The refresh reads the false NodePing holds, so switching
				// autodiag off again changes nothing.
				Config: autodiagConfig(mock.URL(), false),
				Check:  resource.TestCheckResourceAttr("nodeping_check.test", "autodiag", "false"),
			},
		},
	})
}

// A check created with autodiag = true on such an account applies too, and
// plans the change again after.
func TestAccCheckResource_autodiagDroppedOnCreate(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: autodiagConfig(mock.URL(), true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.test", "autodiag", "true"),
					expectAutoDiagSent(mock, false, "true"),
					expectStored(mock, "nodeping_check.test", "top", "autodiag", `false`),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("nodeping_check.test", plancheck.ResourceActionUpdate),
				}},
				ExpectNonEmptyPlan: true,
			},
		},
	})
}
