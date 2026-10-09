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

// Contact addresses are muted in the NodePing web interface, like checks, so
// the provider's ignore_mute leaves an address's mute to NodePing too, unless
// its block sets mute (finding 8's addendum). An address mute that Terraform
// manages can now be switched off: the update sends false, which it used to
// leave out.

const addrMuteContact = "nodeping_contact.mute"

// addrMuteIDs is the contact's ID and its first address's ID, once applied.
type addrMuteIDs struct{ contact, address string }

func (ids *addrMuteIDs) store() resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[addrMuteContact]
		if !ok {
			return fmt.Errorf("%s is not in state", addrMuteContact)
		}
		ids.contact, ids.address = rs.Primary.ID, rs.Primary.Attributes["address.0.id"]
		return nil
	}
}

// addrMuteConfig is a contact named name with a webhook address whose block
// holds attrs, and any further address blocks in more.
func addrMuteConfig(provider, name, attrs, more string) string {
	return provider + fmt.Sprintf(`
resource "nodeping_contact" "mute" {
  name = %q

  address {
    type    = "webhook"
    address = "https://hooks.example.com/mute"
%s  }
%s}
`, name, attrs, more)
}

const addrMuteEmail = `
  address {
    type    = "email"
    address = "a@example.com"
  }
`

// addrMuteHeld checks the mute NodePing holds for the address.
func addrMuteHeld(mock *testutil.MockNodePingServer, ids *addrMuteIDs, want interface{}) resource.TestCheckFunc {
	return func(*terraform.State) error {
		got, ok := mock.ContactAddressMute(ids.contact, ids.address)
		if !ok {
			return fmt.Errorf("NodePing holds no mute for the address, want %#v", want)
		}
		if !reflect.DeepEqual(got, want) {
			return fmt.Errorf("NodePing holds mute %#v, want %#v", got, want)
		}
		return nil
	}
}

// addrMuteSent checks the mute the last contact write sent for the address:
// under its ID in `addresses`, or as the only entry of `newaddresses` when
// the address was created. nil wants it left out.
func addrMuteSent(mock *testutil.MockNodePingServer, ids *addrMuteIDs, want interface{}) resource.TestCheckFunc {
	return func(*terraform.State) error {
		writes := mock.ContactWrites()
		if len(writes) == 0 {
			return fmt.Errorf("no contact was written")
		}
		last := writes[len(writes)-1]
		addresses, _ := last.Body["addresses"].(map[string]interface{})
		addr, ok := addresses[ids.address].(map[string]interface{})
		if !ok {
			added, _ := last.Body["newaddresses"].([]interface{})
			if len(added) != 1 {
				return fmt.Errorf("the last %s did not send the address", last.Method)
			}
			addr, _ = added[0].(map[string]interface{})
		}
		mute, sent := addr["mute"]
		switch {
		case want == nil && sent:
			return fmt.Errorf("the last %s sent mute %#v, want it left out", last.Method, mute)
		case want != nil && !reflect.DeepEqual(mute, want):
			return fmt.Errorf("the last %s sent mute %#v (sent: %v), want %#v", last.Method, mute, sent, want)
		}
		return nil
	}
}

// addrMutePlanned checks the mute a plan has for the contact's first address
// before and after.
type addrMutePlanned struct{ before, after bool }

func (c addrMutePlanned) CheckPlan(_ context.Context, req plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
	first := func(v interface{}) interface{} {
		obj, _ := v.(map[string]interface{})
		list, _ := obj["address"].([]interface{})
		if len(list) == 0 {
			return nil
		}
		addr, _ := list[0].(map[string]interface{})
		return addr["mute"]
	}
	for _, rc := range req.Plan.ResourceChanges {
		if rc.Address != addrMuteContact {
			continue
		}
		before, after := first(rc.Change.Before), first(rc.Change.After)
		if before != c.before || after != c.after {
			resp.Error = fmt.Errorf("the plan has the address's mute %v -> %v, want %v -> %v", before, after, c.before, c.after)
		}
		return
	}
	resp.Error = fmt.Errorf("%s is not in the plan", addrMuteContact)
}

// addrMuteBeforeApply mutes the address in NodePing after the plan is made
// and before it is applied, as muteBeforeApply does for a check.
type addrMuteBeforeApply struct {
	mock *testutil.MockNodePingServer
	ids  *addrMuteIDs
	mute interface{}
}

func (c addrMuteBeforeApply) CheckPlan(context.Context, plancheck.CheckPlanRequest, *plancheck.CheckPlanResponse) {
	c.mock.SetContactAddressMute(c.ids.contact, c.ids.address, c.mute)
}

// With ignore_mute, an address muted in NodePing plans nothing, and refresh
// and import still read the mute.
func TestAccContactResource_ignoreMuteKeepsAnAddressMuteSetInNodePing(t *testing.T) {
	for _, tt := range outOfBandMutes {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)
			config := addrMuteConfig(providerIgnoringMute(mock.URL()), "acc-mute", "", "")

			var ids addrMuteIDs
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: config,
						Check:  ids.store(),
					},
					{
						PreConfig: func() { mock.SetContactAddressMute(ids.contact, ids.address, tt.mute) },
						Config:    config,
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr(addrMuteContact, "address.0.mute", "true"),
							addrMuteHeld(mock, &ids, tt.mute),
							contactWrites(mock, 1),
						),
					},
					{
						Config:            config,
						ResourceName:      addrMuteContact,
						ImportState:       true,
						ImportStateVerify: true,
					},
				},
			})
		})
	}
}

// With ignore_mute, an update of a contact whose address is muted in NodePing
// leaves the address's mute out, so it stays muted as it was.
func TestAccContactResource_ignoreMuteLeavesAddressMuteOutOfAnUpdate(t *testing.T) {
	for _, tt := range outOfBandMutes {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)
			provider := providerIgnoringMute(mock.URL())

			var ids addrMuteIDs
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: addrMuteConfig(provider, "acc-mute", "", ""),
						Check:  ids.store(),
					},
					{
						PreConfig: func() { mock.SetContactAddressMute(ids.contact, ids.address, tt.mute) },
						Config:    addrMuteConfig(provider, "acc-mute-renamed", "", ""),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{
								plancheck.ExpectResourceAction(addrMuteContact, plancheck.ResourceActionUpdate),
								addrMutePlanned{before: true, after: true},
							},
						},
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr(addrMuteContact, "name", "acc-mute-renamed"),
							resource.TestCheckResourceAttr(addrMuteContact, "address.0.mute", "true"),
							addrMuteSent(mock, &ids, nil),
							addrMuteHeld(mock, &ids, tt.mute),
						),
					},
				},
			})
		})
	}
}

// With ignore_mute, an address muted in NodePing after the plan was made stays
// muted when the plan is applied, and the apply does not fail with "Provider
// produced inconsistent result after apply".
func TestAccContactResource_ignoreMuteKeepsAnAddressMuteSetBetweenPlanAndApply(t *testing.T) {
	for _, tt := range outOfBandMutes {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)
			provider := providerIgnoringMute(mock.URL())
			renamed := addrMuteConfig(provider, "acc-mute-renamed", "", "")

			var ids addrMuteIDs
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: addrMuteConfig(provider, "acc-mute", "", ""),
						Check:  ids.store(),
					},
					{
						Config: renamed,
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{
								plancheck.ExpectResourceAction(addrMuteContact, plancheck.ResourceActionUpdate),
								addrMutePlanned{before: false, after: false},
								addrMuteBeforeApply{mock: mock, ids: &ids, mute: tt.mute},
							},
						},
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr(addrMuteContact, "name", "acc-mute-renamed"),
							// The planned mute, until the next refresh.
							resource.TestCheckResourceAttr(addrMuteContact, "address.0.mute", "false"),
							addrMuteSent(mock, &ids, nil),
							addrMuteHeld(mock, &ids, tt.mute),
						),
					},
					{
						Config: renamed,
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr(addrMuteContact, "address.0.mute", "true"),
							addrMuteHeld(mock, &ids, tt.mute),
						),
					},
				},
			})
		})
	}
}

// With ignore_mute, a new address starts unmuted: on create, and when added to
// a contact whose other address is muted in NodePing, which stays muted.
func TestAccContactResource_ignoreMuteCreatesAddressesUnmuted(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	provider := providerIgnoringMute(mock.URL())

	var hook, email addrMuteIDs
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: addrMuteConfig(provider, "acc-mute", "", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					hook.store(),
					resource.TestCheckResourceAttr(addrMuteContact, "address.0.mute", "false"),
					addrMuteSent(mock, &hook, false),
					addrMuteHeld(mock, &hook, false),
				),
			},
			{
				PreConfig: func() { mock.SetContactAddressMute(hook.contact, hook.address, true) },
				Config:    addrMuteConfig(provider, "acc-mute", "", addrMuteEmail),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addrMuteContact, "address.0.mute", "true"),
					resource.TestCheckResourceAttr(addrMuteContact, "address.1.mute", "false"),
					addrMuteSent(mock, &hook, nil),
					addrMuteHeld(mock, &hook, true),
					func(s *terraform.State) error {
						rs := s.RootModule().Resources[addrMuteContact]
						email = addrMuteIDs{contact: rs.Primary.ID, address: rs.Primary.Attributes["address.1.id"]}
						return addrMuteHeld(mock, &email, false)(s)
					},
				),
			},
		},
	})
}

// An address whose block sets mute is still managed with ignore_mute:
// unmuting it in NodePing plans it back, and switching it off sends false.
func TestAccContactResource_ignoreMuteStillManagesAConfiguredAddressMute(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	provider := providerIgnoringMute(mock.URL())
	muted := addrMuteConfig(provider, "acc-mute", "    mute    = true\n", "")

	var ids addrMuteIDs
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: muted,
				Check: resource.ComposeAggregateTestCheckFunc(
					ids.store(),
					resource.TestCheckResourceAttr(addrMuteContact, "address.0.mute", "true"),
					addrMuteHeld(mock, &ids, true),
				),
			},
			{
				PreConfig: func() { mock.SetContactAddressMute(ids.contact, ids.address, false) },
				Config:    muted,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(addrMuteContact, plancheck.ResourceActionUpdate),
						addrMutePlanned{before: false, after: true},
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addrMuteContact, "address.0.mute", "true"),
					addrMuteSent(mock, &ids, true),
					addrMuteHeld(mock, &ids, true),
				),
			},
			{
				Config: addrMuteConfig(provider, "acc-mute", "    mute    = false\n", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{addrMutePlanned{before: true, after: false}},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addrMuteContact, "address.0.mute", "false"),
					addrMuteSent(mock, &ids, false),
					addrMuteHeld(mock, &ids, false),
				),
			},
		},
	})
}

// Without ignore_mute, Terraform manages an address's mute, and can now
// switch it off (finding 8): the update used to leave false out, NodePing
// kept the mute, and the apply failed with "Provider produced inconsistent
// result after apply". Both a mute Terraform set and one set in NodePing are
// switched off.
func TestAccContactResource_managesAddressMuteWithoutIgnoreMute(t *testing.T) {
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
				provider := p.config(mock.URL())
				unmuted := addrMuteConfig(provider, "acc-mute", "", "")

				var ids addrMuteIDs
				resource.Test(t, resource.TestCase{
					ProtoV6ProviderFactories: protoV6ProviderFactories(),
					Steps: []resource.TestStep{
						{
							Config: addrMuteConfig(provider, "acc-mute", "    mute    = true\n", ""),
							Check: resource.ComposeAggregateTestCheckFunc(
								ids.store(),
								addrMuteHeld(mock, &ids, true),
							),
						},
						{
							Config: unmuted,
							ConfigPlanChecks: resource.ConfigPlanChecks{
								PreApply: []plancheck.PlanCheck{addrMutePlanned{before: true, after: false}},
							},
							Check: resource.ComposeAggregateTestCheckFunc(
								resource.TestCheckResourceAttr(addrMuteContact, "address.0.mute", "false"),
								addrMuteSent(mock, &ids, false),
								addrMuteHeld(mock, &ids, false),
							),
						},
						{
							PreConfig: func() { mock.SetContactAddressMute(ids.contact, ids.address, tt.mute) },
							Config:    unmuted,
							ConfigPlanChecks: resource.ConfigPlanChecks{
								PreApply: []plancheck.PlanCheck{
									plancheck.ExpectResourceAction(addrMuteContact, plancheck.ResourceActionUpdate),
									addrMutePlanned{before: true, after: false},
								},
							},
							Check: resource.ComposeAggregateTestCheckFunc(
								resource.TestCheckResourceAttr(addrMuteContact, "address.0.mute", "false"),
								addrMuteSent(mock, &ids, false),
								addrMuteHeld(mock, &ids, false),
							),
						},
					},
				})
			})
		}
	}
}
