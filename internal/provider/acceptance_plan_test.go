package provider_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// Some settings use a NodePing feature only some plans have. The provider's
// plan setting declares the account's plan; left out, it stands for an older
// or unknown plan that has none of them, such as the grandfathered Business
// plan the mock answers like by default. A configuration that uses a feature
// the declared plan lacks fails the plan. With a plan that has it, the
// configuration applies against the mock answering as a Premiere account
// would, and plans clean after.

// providerConfigOnPlan is providerConfig with the provider's plan set, or
// providerConfig itself for "".
func providerConfigOnPlan(url, plan string) string {
	if plan == "" {
		return providerConfig(url)
	}
	return fmt.Sprintf(`
provider "nodeping" {
  api_token = "acc-test-token"
  api_url   = %q
  plan      = %q
}
`, url, plan)
}

// words matches text with any whitespace between its words, as Terraform
// wraps diagnostics.
func words(text string) string {
	return strings.Join(strings.Fields(regexp.QuoteMeta(text)), `\s+`)
}

type planFeatureCase struct {
	name string
	// config is the resource, address its address.
	config, address string
	// summary is the error's summary, line the configuration line the
	// error points at, as a regular expression.
	summary, line string
	// lacking is a plan without the feature, "" if all three have it;
	// having the plans with it.
	lacking string
	having  []string
	// applied checks the state after the apply.
	applied resource.TestCheckFunc
}

var planFeatureCases = []planFeatureCase{
	{
		name: "homeloc",
		config: `
resource "nodeping_check" "test" {
  type    = "HTTP"
  target  = "https://example.com"
  label   = "acc-plan-homeloc"
  homeloc = "nam"
}
`,
		address: "nodeping_check.test",
		summary: "homeloc needs NodePing's Premiere plan",
		line:    `homeloc\s+=\s+"nam"`,
		lacking: "professional",
		having:  []string{"premiere"},
		applied: resource.TestCheckResourceAttr("nodeping_check.test", "homeloc", "nam"),
	},
	{
		name: "verifyvolume",
		config: `
resource "nodeping_check" "test" {
  type         = "AUDIO"
  target       = "https://example.com/stream.mp3"
  label        = "acc-plan-verifyvolume"
  verifyvolume = true
}
`,
		address: "nodeping_check.test",
		summary: "verifyvolume needs NodePing's Premiere plan",
		line:    `verifyvolume\s+=\s+true`,
		lacking: "personal",
		having:  []string{"premiere"},
		applied: resource.TestCheckResourceAttr("nodeping_check.test", "verifyvolume", "true"),
	},
	{
		name: "volumemin",
		config: `
resource "nodeping_check" "test" {
  type      = "AUDIO"
  target    = "https://example.com/stream.mp3"
  label     = "acc-plan-volumemin"
  volumemin = -40
}
`,
		address: "nodeping_check.test",
		summary: "volumemin needs NodePing's Premiere plan",
		line:    `volumemin\s+=\s+-40`,
		lacking: "professional",
		having:  []string{"premiere"},
		applied: resource.TestCheckResourceAttr("nodeping_check.test", "volumemin", "-40"),
	},
	{
		name: "a voice address",
		config: `
resource "nodeping_contact" "test" {
  name = "acc-plan-voice"

  address {
    type    = "email"
    address = "acc-plan@example.com"
  }

  address {
    type    = "voice"
    address = "+15550000001"
  }
}
`,
		address: "nodeping_contact.test",
		summary: `type = "voice" needs NodePing's Premiere plan`,
		line:    `type\s+=\s+"voice"`,
		lacking: "personal",
		having:  []string{"premiere"},
		applied: resource.TestCheckResourceAttr("nodeping_contact.test", "address.1.type", "voice"),
	},
	{
		name: "interval 0.25",
		config: `
resource "nodeping_check" "test" {
  type     = "HTTP"
  target   = "https://example.com"
  label    = "acc-plan-interval"
  interval = 0.25
}
`,
		address: "nodeping_check.test",
		summary: "interval = 0.25 needs NodePing's Professional or Premiere plan",
		line:    `interval\s+=\s+0\.25`,
		lacking: "personal",
		having:  []string{"professional", "premiere"},
		applied: resource.TestCheckResourceAttr("nodeping_check.test", "interval", "0.25"),
	},
	{
		name: "interval 0.5",
		config: `
resource "nodeping_check" "test" {
  type     = "HTTP"
  target   = "https://example.com"
  label    = "acc-plan-interval"
  interval = 0.5
}
`,
		address: "nodeping_check.test",
		summary: "interval = 0.5 needs NodePing's Professional or Premiere plan",
		line:    `interval\s+=\s+0\.5`,
		lacking: "personal",
		having:  []string{"professional", "premiere"},
		applied: resource.TestCheckResourceAttr("nodeping_check.test", "interval", "0.5"),
	},
	{
		name: "autodiag",
		config: `
resource "nodeping_check" "test" {
  type     = "HTTP"
  target   = "https://example.com"
  label    = "acc-plan-autodiag"
  autodiag = true
}
`,
		address: "nodeping_check.test",
		summary: "autodiag = true needs NodePing's Personal, Professional or Premiere plan",
		line:    `autodiag\s+=\s+true`,
		having:  []string{"personal", "premiere"},
		applied: resource.TestCheckResourceAttr("nodeping_check.test", "autodiag", "true"),
	},
	{
		name: "suppress_diag",
		config: `
resource "nodeping_contact" "test" {
  name = "acc-plan-suppress-diag"

  address {
    type          = "webhook"
    address       = "https://hooks.example.com/notify"
    suppress_diag = true
  }
}
`,
		address: "nodeping_contact.test",
		summary: "suppress_diag = true needs NodePing's Personal, Professional or Premiere plan",
		line:    `suppress_diag\s+=\s+true`,
		having:  []string{"personal", "premiere"},
		applied: resource.TestCheckResourceAttr("nodeping_contact.test", "address.0.suppress_diag", "true"),
	},
}

// Without plan, and with a plan that lacks the feature, the plan fails,
// naming the setting and the plans that have it, at the setting's line.
// Nothing is sent to NodePing.
func TestAccPlan_featureRefused(t *testing.T) {
	for _, tt := range planFeatureCases {
		plans := []string{""}
		if tt.lacking != "" {
			plans = append(plans, tt.lacking)
		}
		for _, plan := range plans {
			declared := `does\s+not\s+set\s+plan`
			if plan != "" {
				declared = fmt.Sprintf(`sets\s+plan\s+=\s+%q,\s+which\s+does\s+not\s+have\s+it`, plan)
			}
			t.Run(fmt.Sprintf("%s, plan %q", tt.name, plan), func(t *testing.T) {
				mock := testutil.NewMockNodePingServer()
				t.Cleanup(mock.Close)

				resource.Test(t, resource.TestCase{
					ProtoV6ProviderFactories: protoV6ProviderFactories(),
					Steps: []resource.TestStep{
						{
							Config:   providerConfigOnPlan(mock.URL(), plan) + tt.config,
							PlanOnly: true,
							ExpectError: regexp.MustCompile(`Error: ` + words(tt.summary) +
								`[\s\S]*\d+:\s+` + tt.line +
								`[\s\S]*` + declared +
								`[\s\S]*in\s+the\s+provider\s+block`),
						},
					},
				})

				for _, r := range mock.Requests() {
					if r.Method != "GET" {
						t.Errorf("the refused plan sent %s %s", r.Method, r.Path)
					}
				}
			})
		}
	}
}

// With a plan that has the feature, the configuration applies against the
// mock answering as a Premiere account would, and plans clean after.
func TestAccPlan_featureApplied(t *testing.T) {
	for _, tt := range planFeatureCases {
		for _, plan := range tt.having {
			t.Run(fmt.Sprintf("%s, plan %q", tt.name, plan), func(t *testing.T) {
				mock := testutil.NewMockNodePingServer()
				t.Cleanup(mock.Close)
				mock.UsePremierePlan()
				config := providerConfigOnPlan(mock.URL(), plan) + tt.config

				resource.Test(t, resource.TestCase{
					ProtoV6ProviderFactories: protoV6ProviderFactories(),
					Steps: []resource.TestStep{
						{Config: config, Check: tt.applied},
						{Config: config, PlanOnly: true},
					},
				})
			})
		}
	}
}

// plan takes only the three plans' names, checked before anything else.
func TestAccPlan_invalid(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfigOnPlan(mock.URL(), "bogus") + `
data "nodeping_contactgroups" "all" {}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`plan\s+value\s+must\s+be\s+one\s+of:\s+\["personal"\s+"professional"\s+"premiere"\],\s+got:\s+"bogus"`),
			},
		},
	})
}

// A plan known only after apply fails the plan, as every other setting does,
// rather than being read as no plan. Once its source is applied, it plans.
func TestAccPlan_unknown(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	mock.UsePremierePlan()

	const source = `
resource "terraform_data" "plan" {
  input = "premiere"
}
`
	config := source + fmt.Sprintf(`
provider "nodeping" {
  api_token = "acc-test-token"
  api_url   = %q
  plan      = terraform_data.plan.output
}

resource "nodeping_check" "test" {
  type    = "HTTP"
  target  = "https://example.com"
  label   = "acc-plan-unknown"
  homeloc = "nam"
}
`, mock.URL())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				ExpectError: regexp.MustCompile(`Error:\s+Unknown\s+NodePing\s+Provider\s+Setting[\s\S]*` +
					`plan\s+is\s+known\s+only\s+after\s+apply`),
			},
			{Config: source},
			{
				Config: config,
				Check:  resource.TestCheckResourceAttr("nodeping_check.test", "homeloc", "nam"),
			},
		},
	})
}

// Without plan, settings that use none of these features plan and apply as
// before: interval 1, autodiag and suppress_diag false, every address type
// but voice.
func TestAccPlan_noPlanNoFeature(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	var addresses string
	for _, typ := range []string{"email", "sms", "webhook", "slack", "hipchat", "pushover", "pagerduty"} {
		addresses += fmt.Sprintf(`
  address {
    type          = %q
    address       = "acc-plan-%s"
    suppress_diag = false
  }
`, typ, typ)
	}
	config := providerConfig(mock.URL()) + `
resource "nodeping_check" "test" {
  type     = "HTTP"
  target   = "https://example.com"
  label    = "acc-plan-none"
  interval = 1
  autodiag = false
}

resource "nodeping_contact" "test" {
  name = "acc-plan-none"
` + addresses + `}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.test", "interval", "1"),
					resource.TestCheckResourceAttr("nodeping_contact.test", "address.#", "7"),
				),
			},
			{Config: config, PlanOnly: true},
		},
	})
}

// suppress_diag = true on a plan that allows it, against a mock that drops
// suppressdiag, as the probed account does: the apply keeps the planned
// value, and the plan after shows it again. It used to fail with "Provider
// produced inconsistent result after apply: .address[0].suppress_diag: was
// cty.True, but now cty.False" (#32).
func TestAccPlan_suppressDiagDropped(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := func(attrs string) string {
		return providerConfigOnPlan(mock.URL(), "personal") + fmt.Sprintf(`
resource "nodeping_contact" "clear" {
  name = "acc-plan-suppress-diag"

  address {
    type    = "webhook"
    address = "https://hooks.example.com/notify"
%s  }
}
`, attrs)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config("    suppress_diag = true\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addrClearContact, "address.0.suppress_diag", "true"),
					addrClearStored(mock, "suppressdiag", addrClearLeftOut),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addrClearContact, plancheck.ResourceActionUpdate),
				}},
				ExpectNonEmptyPlan: true,
			},
			{
				// The update to switch it on again applies too.
				Config: config("    suppress_diag = true\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addrClearContact, "address.0.suppress_diag", "true"),
					addrClearSent(mock, "suppressdiag", `true`),
				),
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// Switching suppress_diag off sends false, which an account that stores it
// stores. The same for the other flags is in
// TestAccContactResource_addressSuppressFlagsSwitchedOff.
func TestAccPlan_suppressDiagSwitchedOff(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	mock.UsePremierePlan()

	config := func(attrs string) string {
		return providerConfigOnPlan(mock.URL(), "personal") + fmt.Sprintf(`
resource "nodeping_contact" "clear" {
  name = "acc-plan-suppress-diag"

  address {
    type    = "webhook"
    address = "https://hooks.example.com/notify"
%s  }
}
`, attrs)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config("    suppress_diag = true\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addrClearContact, "address.0.suppress_diag", "true"),
					addrClearStored(mock, "suppressdiag", `true`),
				),
			},
			{
				Config: config(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addrClearContact, "address.0.suppress_diag", "false"),
					addrClearSent(mock, "suppressdiag", `false`),
					addrClearStored(mock, "suppressdiag", `false`),
				),
			},
			addrClearImport(),
		},
	})
}

// A check with a preferred probe location imports with it, on a Premiere
// account.
func TestAccPlan_importHomeLoc(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	mock.UsePremierePlan()

	config := providerConfigOnPlan(mock.URL(), "premiere") + `
resource "nodeping_check" "imported" {
  type    = "HTTP"
  target  = "https://example.com"
  label   = "acc-import-homeloc"
  homeloc = "nam"
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				Config:            config,
				ResourceName:      "nodeping_check.imported",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
