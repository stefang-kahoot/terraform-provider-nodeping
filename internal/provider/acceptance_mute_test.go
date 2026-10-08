package provider_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// The NodePing web interface mutes a check with true, or until a time with a
// number, the time in epoch milliseconds.
var outOfBandMutes = []struct {
	name string
	mute interface{}
}{
	{name: "muted", mute: true},
	{name: "muted until a time", mute: int64(1791462833954)},
}

func providerIgnoringMute(url string) string {
	return fmt.Sprintf(`
provider "nodeping" {
  api_token   = "acc-test-token"
  api_url     = %q
  ignore_mute = true
}
`, url)
}

// storeCheckID saves the ID of the check at address once it is applied.
func storeCheckID(address string, id *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[address]
		if !ok {
			return fmt.Errorf("%s is not in state", address)
		}
		*id = rs.Primary.ID
		return nil
	}
}

// expectMockMute checks the mute NodePing holds for the check.
func expectMockMute(mock *testutil.MockNodePingServer, id *string, want interface{}) resource.TestCheckFunc {
	return func(*terraform.State) error {
		got, ok := mock.CheckMute(*id)
		if !ok {
			return fmt.Errorf("NodePing holds no mute for the check, want %#v", want)
		}
		if !reflect.DeepEqual(got, want) {
			return fmt.Errorf("NodePing holds mute %#v, want %#v", got, want)
		}
		return nil
	}
}

// expectLastUpdate checks the last update request sent for the check: that it
// left mute out, or sent the one given.
func expectLastUpdate(mock *testutil.MockNodePingServer, id *string, wantMute interface{}) resource.TestCheckFunc {
	return func(*terraform.State) error {
		updates := mock.CheckUpdates(*id)
		if len(updates) == 0 {
			return fmt.Errorf("no update was sent")
		}
		mute, sent := updates[len(updates)-1]["mute"]
		switch {
		case wantMute == nil && sent:
			return fmt.Errorf("the update sent mute %#v, want it left out", mute)
		case wantMute != nil && !sent:
			return fmt.Errorf("the update left mute out, want %#v", wantMute)
		case wantMute != nil && !reflect.DeepEqual(mute, wantMute):
			return fmt.Errorf("the update sent mute %#v, want %#v", mute, wantMute)
		}
		return nil
	}
}

// expectNoUpdate checks that no update request was sent for the check.
func expectNoUpdate(mock *testutil.MockNodePingServer, id *string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if n := len(mock.CheckUpdates(*id)); n != 0 {
			return fmt.Errorf("%d updates were sent, want none", n)
		}
		return nil
	}
}

// plannedMute checks the mute a plan has for a resource before and after.
type plannedMute struct {
	address       string
	before, after bool
}

func (c plannedMute) CheckPlan(_ context.Context, req plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
	for _, rc := range req.Plan.ResourceChanges {
		if rc.Address != c.address {
			continue
		}
		before, _ := rc.Change.Before.(map[string]interface{})
		after, _ := rc.Change.After.(map[string]interface{})
		if before["mute"] != c.before || after["mute"] != c.after {
			resp.Error = fmt.Errorf("%s plans mute %v -> %v, want %v -> %v",
				c.address, before["mute"], after["mute"], c.before, c.after)
		}
		return
	}
	resp.Error = fmt.Errorf("%s is not in the plan", c.address)
}

// muteBeforeApply mutes the check in NodePing after the plan is made and
// before it is applied. The test harness applies the saved plan without
// refreshing, so the apply starts from a plan that knows nothing of the mute,
// as when a saved plan is applied some while after it was made.
type muteBeforeApply struct {
	mock *testutil.MockNodePingServer
	id   *string
	mute interface{}
}

func (c muteBeforeApply) CheckPlan(context.Context, plancheck.CheckPlanRequest, *plancheck.CheckPlanResponse) {
	c.mock.SetCheckMute(*c.id, c.mute)
}

// With ignore_mute, a check created without mute starts unmuted, and the plan
// after it is empty.
func TestAccCheckResource_ignoreMuteCreatesUnmuted(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	var id string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerIgnoringMute(mock.URL()) + `
resource "nodeping_check" "test" {
  type   = "HTTP"
  target = "https://example.com"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					storeCheckID("nodeping_check.test", &id),
					resource.TestCheckResourceAttr("nodeping_check.test", "mute", "false"),
					expectMockMute(mock, &id, false),
				),
			},
		},
	})
}

// With ignore_mute, a check muted in NodePing plans nothing: neither mute nor,
// on a check without a label, the label NodePing gave it. Refresh and import
// still read the mute.
func TestAccCheckResource_ignoreMuteKeepsAMuteSetInNodePing(t *testing.T) {
	for _, tt := range outOfBandMutes {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)

			config := providerIgnoringMute(mock.URL()) + `
resource "nodeping_check" "test" {
  type   = "HTTP"
  target = "https://example.com"
}
`

			var id string
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: config,
						Check:  storeCheckID("nodeping_check.test", &id),
					},
					{
						PreConfig: func() { mock.SetCheckMute(id, tt.mute) },
						Config:    config,
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr("nodeping_check.test", "mute", "true"),
							expectMockMute(mock, &id, tt.mute),
							expectNoUpdate(mock, &id),
						),
					},
					{
						Config:            config,
						ResourceName:      "nodeping_check.test",
						ImportState:       true,
						ImportStateVerify: true,
					},
				},
			})
		})
	}
}

// With ignore_mute, an update of a check muted in NodePing leaves mute out of
// the request, so the check stays muted as it was.
func TestAccCheckResource_ignoreMuteLeavesMuteOutOfAnUpdate(t *testing.T) {
	for _, tt := range outOfBandMutes {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)

			config := func(label string) string {
				return providerIgnoringMute(mock.URL()) + fmt.Sprintf(`
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
						Config: config("acc-mute"),
						Check:  storeCheckID("nodeping_check.test", &id),
					},
					{
						PreConfig: func() { mock.SetCheckMute(id, tt.mute) },
						Config:    config("acc-mute-renamed"),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{
								plancheck.ExpectResourceAction("nodeping_check.test", plancheck.ResourceActionUpdate),
								plannedMute{address: "nodeping_check.test", before: true, after: true},
							},
						},
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr("nodeping_check.test", "label", "acc-mute-renamed"),
							resource.TestCheckResourceAttr("nodeping_check.test", "mute", "true"),
							expectLastUpdate(mock, &id, nil),
							expectMockMute(mock, &id, tt.mute),
						),
					},
				},
			})
		})
	}
}

// With ignore_mute, a check muted in NodePing after the plan was made stays
// muted when the plan is applied, and the apply does not fail with "Provider
// produced inconsistent result after apply".
func TestAccCheckResource_ignoreMuteKeepsAMuteSetBetweenPlanAndApply(t *testing.T) {
	for _, tt := range outOfBandMutes {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)

			config := func(label string) string {
				return providerIgnoringMute(mock.URL()) + fmt.Sprintf(`
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
						Config: config("acc-mute"),
						Check:  storeCheckID("nodeping_check.test", &id),
					},
					{
						Config: config("acc-mute-renamed"),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{
								plancheck.ExpectResourceAction("nodeping_check.test", plancheck.ResourceActionUpdate),
								plannedMute{address: "nodeping_check.test", before: false, after: false},
								muteBeforeApply{mock: mock, id: &id, mute: tt.mute},
							},
						},
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr("nodeping_check.test", "label", "acc-mute-renamed"),
							// The planned mute, until the next refresh.
							resource.TestCheckResourceAttr("nodeping_check.test", "mute", "false"),
							expectLastUpdate(mock, &id, nil),
							expectMockMute(mock, &id, tt.mute),
						),
					},
					{
						Config: config("acc-mute-renamed"),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr("nodeping_check.test", "mute", "true"),
							expectMockMute(mock, &id, tt.mute),
						),
					},
				},
			})
		})
	}
}

// A check that sets mute itself is still managed with ignore_mute: unmuting it
// in NodePing plans it back.
func TestAccCheckResource_ignoreMuteStillManagesAConfiguredMute(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := providerIgnoringMute(mock.URL()) + `
resource "nodeping_check" "test" {
  type   = "HTTP"
  target = "https://example.com"
  mute   = true
}
`

	var id string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					storeCheckID("nodeping_check.test", &id),
					resource.TestCheckResourceAttr("nodeping_check.test", "mute", "true"),
					expectMockMute(mock, &id, true),
				),
			},
			{
				PreConfig: func() { mock.SetCheckMute(id, false) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("nodeping_check.test", plancheck.ResourceActionUpdate),
						plannedMute{address: "nodeping_check.test", before: false, after: true},
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.test", "mute", "true"),
					expectLastUpdate(mock, &id, true),
					expectMockMute(mock, &id, true),
				),
			},
		},
	})
}

// Without ignore_mute, Terraform manages mute as it always has: a check muted
// in NodePing plans false and is unmuted.
func TestAccCheckResource_managesMuteWithoutIgnoreMute(t *testing.T) {
	providers := []struct {
		name   string
		config func(url string) string
	}{
		{name: "ignore_mute unset", config: providerConfig},
		{name: "ignore_mute false", config: func(url string) string {
			return fmt.Sprintf(`
provider "nodeping" {
  api_token   = "acc-test-token"
  api_url     = %q
  ignore_mute = false
}
`, url)
		}},
	}

	for _, p := range providers {
		for _, tt := range outOfBandMutes {
			t.Run(p.name+"/"+tt.name, func(t *testing.T) {
				mock := testutil.NewMockNodePingServer()
				t.Cleanup(mock.Close)

				config := p.config(mock.URL()) + `
resource "nodeping_check" "test" {
  type   = "HTTP"
  target = "https://example.com"
}
`

				var id string
				resource.Test(t, resource.TestCase{
					ProtoV6ProviderFactories: protoV6ProviderFactories(),
					Steps: []resource.TestStep{
						{
							Config: config,
							Check:  storeCheckID("nodeping_check.test", &id),
						},
						{
							PreConfig: func() { mock.SetCheckMute(id, tt.mute) },
							Config:    config,
							ConfigPlanChecks: resource.ConfigPlanChecks{
								PreApply: []plancheck.PlanCheck{
									plancheck.ExpectResourceAction("nodeping_check.test", plancheck.ResourceActionUpdate),
									plannedMute{address: "nodeping_check.test", before: true, after: false},
								},
							},
							Check: resource.ComposeAggregateTestCheckFunc(
								resource.TestCheckResourceAttr("nodeping_check.test", "mute", "false"),
								expectLastUpdate(mock, &id, false),
								expectMockMute(mock, &id, false),
							),
						},
					},
				})
			})
		}
	}
}
