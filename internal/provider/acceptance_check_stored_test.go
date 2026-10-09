package provider_test

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// NodePing stores some attributes only on the check types that use them, and
// drops them from a create or update of any other type (finding 36). The mock
// does the same, from the probe's own list. Refresh reads NodePing as it is,
// so such an attribute in a configuration would plan again after every apply;
// the plan fails instead, pointing at the attribute.

// wrapped matches text that Terraform may have wrapped across lines.
func wrapped(text string) string {
	return strings.ReplaceAll(regexp.QuoteMeta(text), " ", `\s+`)
}

// A new HTTPADV check with servername, which only SSL stores, fails the plan
// at the servername line.
func TestAccCheckResource_refusesAnAttributeTheTypeDoesNotStore(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(mock.URL()) + `
resource "nodeping_check" "test" {
  type       = "HTTPADV"
  target     = "https://example.com/health"
  servername = "sni.example.com"
}
`,
				ExpectError: regexp.MustCompile(`(?s)Error: servername is not stored for HTTPADV checks` +
					`.*in resource "nodeping_check" "test":\s+\d+:\s+servername = "sni.example.com"` +
					`.*` + wrapped("NodePing does not store servername on HTTPADV checks: it accepts the value and drops it") +
					`.*` + wrapped("The check types that store servername: SSL.")),
			},
		},
	})
	if n := len(mock.Requests()); n != 0 {
		t.Errorf("sent %d requests to NodePing, want none", n)
	}
}

// An empty value passes: Kahoot's discover-page HTTP checks say regex = false,
// which HTTP does not store, and are created and plan clean.
func TestAccCheckResource_allowsAnEmptyValueTheTypeDoesNotStore(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := providerConfig(mock.URL()) + `
resource "nodeping_check" "test" {
  type          = "HTTP"
  target        = "https://example.com/discover"
  contentstring = "Kahoot"
  invert        = false
  regex         = false
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.test", "regex", "false"),
					expectStored(mock, "nodeping_check.test", "param", "regex", ``),
				),
			},
			{Config: config, PlanOnly: true},
		},
	})
}

// heldFieldsConfig is an HTTP check holding fields from an earlier HTTPPARSE
// life, as LYAFQPUK does in production.
func heldFieldsConfig(url, typ, max string) string {
	return providerConfig(url) + fmt.Sprintf(`
resource "nodeping_check" "test" {
  type     = %q
  target   = "https://example.com/variables.json"
  label    = "acc-held-fields"
  enabled  = true
  interval = 1

  fields = {
    A = { name = "status", min = 200, max = 200 }
    B = { name = "content.400.defaultOutput", min = 1, max = %s }
  }
}
`, typ, max)
}

// seedHeldFields puts an HTTP check that holds HTTPPARSE fields into the mock,
// as if its type had been changed in the web interface.
func seedHeldFields(mock *testutil.MockNodePingServer) {
	mock.AddCheck("HELD-FIELDS", map[string]interface{}{
		"_id":      "HELD-FIELDS",
		"type":     "HTTP",
		"label":    "acc-held-fields",
		"enable":   "active",
		"interval": 1,
		"parameters": map[string]interface{}{
			"target":    "https://example.com/variables.json",
			"threshold": 5,
			"sens":      2,
			"fields": map[string]interface{}{
				"A": map[string]interface{}{"name": "status", "min": 200, "max": 200},
				"B": map[string]interface{}{"name": "content.400.defaultOutput", "min": 1, "max": 99},
			},
		},
	})
}

// An HTTP check holding fields, which HTTP does not store, imports and plans
// clean while the configuration keeps them as they are. Changing one fails the
// plan: an update of an HTTP check cannot change them. Changing the type to
// HTTPPARSE, which stores them, updates the check in place, fields and all.
func TestAccCheckResource_heldValueTheTypeDoesNotStore(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	seedHeldFields(mock)

	held := heldFieldsConfig(mock.URL(), "HTTP", "99")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				// An import block plans no change.
				Config:          held,
				ResourceName:    "nodeping_check.test",
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				ImportStateId:   "HELD-FIELDS",
			},
			{
				Config:             held,
				ResourceName:       "nodeping_check.test",
				ImportState:        true,
				ImportStateId:      "HELD-FIELDS",
				ImportStatePersist: true,
			},
			{
				Config: held,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config: heldFieldsConfig(mock.URL(), "HTTP", "98"),
				ExpectError: regexp.MustCompile(`(?s)Error: fields is not stored for HTTP checks` +
					`.*in resource "nodeping_check" "test":\s+\d+:\s+fields = \{` +
					`.*` + wrapped("so an update cannot add or change it") +
					`.*` + wrapped("The check types that store fields: HTTPPARSE,")),
			},
			{
				Config: heldFieldsConfig(mock.URL(), "HTTPPARSE", "98"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("nodeping_check.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.test", "id", "HELD-FIELDS"),
					resource.TestCheckResourceAttr("nodeping_check.test", "fields.B.max", "98"),
					expectStored(mock, "nodeping_check.test", "top", "type", `"HTTPPARSE"`),
					expectStored(mock, "nodeping_check.test", "param", "fields",
						`{"A":{"max":200,"min":200,"name":"status"},"B":{"max":98,"min":1,"name":"content.400.defaultOutput"}}`),
				),
			},
		},
	})
}

// Removing fields replaces a check (NodePing cannot remove them), and a
// replacement is a create: removing all of them from an HTTP check that holds
// them plans the replacement, while removing one fails the plan, because the
// new HTTP check would not store the other.
func TestAccCheckResource_replacingAHeldValueIsACreate(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	seedHeldFields(mock)

	without := func(fields string) string {
		return providerConfig(mock.URL()) + `
resource "nodeping_check" "test" {
  type     = "HTTP"
  target   = "https://example.com/variables.json"
  label    = "acc-held-fields"
  enabled  = true
  interval = 1
` + fields + `
}
`
	}

	var id string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:             heldFieldsConfig(mock.URL(), "HTTP", "99"),
				ResourceName:       "nodeping_check.test",
				ImportState:        true,
				ImportStateId:      "HELD-FIELDS",
				ImportStatePersist: true,
			},
			{
				Config: without(`
  fields = {
    A = { name = "status", min = 200, max = 200 }
  }
`),
				ExpectError: regexp.MustCompile(`(?s)Error: fields is not stored for HTTP checks.*` +
					wrapped("This plan replaces the check with a new one") + `.*` + wrapped("it accepts the value and drops it")),
			},
			{
				Config: without(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("nodeping_check.test", plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					storeCheckID("nodeping_check.test", &id),
					resource.TestCheckNoResourceAttr("nodeping_check.test", "fields"),
					expectStored(mock, "nodeping_check.test", "param", "fields", ``),
					func(*terraform.State) error {
						if _, ok := mock.GetCheck("HELD-FIELDS"); ok {
							return fmt.Errorf("the old check is still there")
						}
						if id == "HELD-FIELDS" {
							return fmt.Errorf("the check was not replaced")
						}
						return nil
					},
				),
			},
		},
	})
}

// A value not known until apply passes the plan, and fails the apply before
// anything is sent: Terraform plans the check once more during the apply,
// with the value known by then.
func TestAccCheckResource_refusesAnUnknownValueAtApply(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(mock.URL()) + `
resource "terraform_data" "sni" {
  input = "sni.example.com"
}

resource "nodeping_check" "test" {
  type       = "HTTPADV"
  target     = "https://example.com/health"
  servername = terraform_data.sni.output
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectUnknownValue("nodeping_check.test", tfjsonpath.New("servername")),
					},
				},
				ExpectError: regexp.MustCompile(`(?s)Error running apply.*Error: servername is not stored for HTTPADV checks`),
			},
		},
	})
	for _, r := range mock.Requests() {
		if r.Method != http.MethodGet {
			t.Errorf("sent %s %s to NodePing, want nothing written", r.Method, r.Path)
		}
	}
}

// expectReplacePath checks that the plan replaces a resource because of the
// attribute: Terraform lists it in the change's replace_paths.
type expectReplacePath struct{ address, attribute string }

func (e expectReplacePath) CheckPlan(_ context.Context, req plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
	for _, rc := range req.Plan.ResourceChanges {
		if rc.Address != e.address {
			continue
		}
		for _, p := range rc.Change.ReplacePaths {
			if steps, ok := p.([]interface{}); ok && len(steps) == 1 && steps[0] == e.attribute {
				return
			}
		}
		resp.Error = fmt.Errorf("%s: replace_paths %v lack %s", e.address, rc.Change.ReplacePaths, e.attribute)
		return
	}
	resp.Error = fmt.Errorf("%s is not in the plan", e.address)
}

// An HTTP check holding regex = true from an earlier HTTPCONTENT life cannot
// have it removed by an update: HTTP does not store regex, and NodePing
// ignores a change to it. Removing it from the configuration replaces the
// check instead (with a warning), and the new check does not hold it.
func TestAccCheckResource_removingAHeldValueReplacesTheCheck(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)
	mock.AddCheck("HELD-REGEX", map[string]interface{}{
		"_id":      "HELD-REGEX",
		"type":     "HTTP",
		"label":    "acc-held-regex",
		"enable":   "active",
		"interval": 1,
		"parameters": map[string]interface{}{
			"target":        "https://example.com/discover",
			"threshold":     5,
			"sens":          2,
			"contentstring": "Kahoot",
			"regex":         true,
		},
	})

	config := func(regex string) string {
		return providerConfig(mock.URL()) + `
resource "nodeping_check" "test" {
  type          = "HTTP"
  target        = "https://example.com/discover"
  label         = "acc-held-regex"
  enabled       = true
  interval      = 1
  contentstring = "Kahoot"
` + regex + `
}
`
	}

	var id string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:             config(`  regex         = true`),
				ResourceName:       "nodeping_check.test",
				ImportState:        true,
				ImportStateId:      "HELD-REGEX",
				ImportStatePersist: true,
			},
			{
				Config: config(`  regex         = true`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				// The step's own empty plan after the apply shows the new
				// check plans clean.
				Config: config(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("nodeping_check.test", plancheck.ResourceActionDestroyBeforeCreate),
						expectReplacePath{"nodeping_check.test", "regex"},
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					storeCheckID("nodeping_check.test", &id),
					resource.TestCheckNoResourceAttr("nodeping_check.test", "regex"),
					expectStored(mock, "nodeping_check.test", "param", "regex", ``),
					expectStored(mock, "nodeping_check.test", "param", "contentstring", `"Kahoot"`),
					func(*terraform.State) error {
						if _, ok := mock.GetCheck("HELD-REGEX"); ok {
							return fmt.Errorf("the old check is still there")
						}
						if id == "HELD-REGEX" {
							return fmt.Errorf("the check was not replaced")
						}
						return nil
					},
				),
			},
		},
	})
}
