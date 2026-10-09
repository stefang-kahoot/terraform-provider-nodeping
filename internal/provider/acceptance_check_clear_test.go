package provider_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// NodePing merges an update into the check, keeping whatever the update leaves
// out (finding 28). These tests remove an attribute from the configuration
// and expect NodePing -- the mock, which merges the same way -- to end up
// holding the value that clears it, and the plan after the apply to be empty.

// storedCheckValue returns a value the mock holds for a check, as JSON, or ""
// when it holds none. where is "top" or "param".
func storedCheckValue(mock *testutil.MockNodePingServer, s *terraform.State, address, where, key string) (string, error) {
	rs, ok := s.RootModule().Resources[address]
	if !ok {
		return "", fmt.Errorf("%s is not in the state", address)
	}
	check, ok := mock.GetCheck(rs.Primary.ID)
	if !ok {
		return "", fmt.Errorf("check %s is not in the mock", rs.Primary.ID)
	}
	src := check
	if where == "param" {
		src, _ = check["parameters"].(map[string]interface{})
	}
	v, ok := src[key]
	if !ok {
		return "", nil
	}
	b, err := json.Marshal(v)
	return string(b), err
}

// expectStored checks the value the mock holds for a check.
func expectStored(mock *testutil.MockNodePingServer, address, where, key, want string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		got, err := storedCheckValue(mock, s, address, where, key)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("NodePing holds %s = %s, want %s", key, got, want)
		}
		return nil
	}
}

type clearCase struct {
	name string
	// typ is the check type; set is the attribute as configured before it is
	// removed.
	typ, set string
	// attribute is the attribute's name in state, where and key where
	// NodePing holds it, and cleared the value it holds once removed.
	attribute, where, key, cleared string
}

func testClearRemoved(t *testing.T, tt clearCase) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := func(attribute string) string {
		return providerConfig(mock.URL()) + fmt.Sprintf(`
resource "nodeping_check" "dep" {
  type   = "PING"
  target = "192.0.2.1"
}

resource "nodeping_check" "test" {
  type   = %q
  target = "https://example.com/health"
  label  = "acc-clear"
  %s
}
`, tt.typ, attribute)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config(tt.set),
				Check:  resource.TestCheckResourceAttrSet("nodeping_check.test", tt.attribute),
			},
			{
				// The step's own empty plan after the apply is the real test:
				// it fails if NodePing kept the value.
				Config: config(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("nodeping_check.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("nodeping_check.test", tt.attribute),
					expectStored(mock, "nodeping_check.test", tt.where, tt.key, tt.cleared),
				),
			},
		},
	})
}

func TestAccCheckResource_clearsRemovedValues(t *testing.T) {
	tests := []clearCase{
		{"postdata", "HTTPADV", `postdata = "probe=1"`, "postdata", "param", "postdata", `""`},
		{"contentstring", "HTTPADV", `contentstring = "Example"`, "contentstring", "param", "contentstring", `""`},
		{"method", "HTTPADV", `method = "POST"`, "method", "param", "method", `""`},
		{"statuscode", "HTTPADV", `statuscode = 201`, "statuscode", "param", "statuscode", `""`},
		{"regex", "HTTPCONTENT", `regex = true`, "regex", "param", "regex", `false`},
		{"invert", "HTTPCONTENT", `invert = true`, "invert", "param", "invert", `false`},
		{"follow", "HTTPADV", `follow = true`, "follow", "param", "follow", `false`},
		{"ipv6", "HTTPADV", `ipv6 = true`, "ipv6", "param", "ipv6", `false`},
		{"servername", "SSL", `servername = "example.com"`, "servername", "param", "servername", `""`},
		{"warningdays", "SSL", `warningdays = 21`, "warningdays", "param", "warningdays", `""`},
		{"dep", "HTTP", `dep = nodeping_check.dep.id`, "dep", "top", "dep", `false`},
		{"runlocations", "HTTP", `runlocations = ["nam"]`, "runlocations.#", "top", "runlocations", `[]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testClearRemoved(t, tt)
		})
	}
}

// A boolean configured false and then removed reads back as removed: NodePing
// keeps the key, as false, and false is what an unset boolean is to it.
func TestAccCheckResource_removesABooleanSetFalse(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := func(attributes string) string {
		return providerConfig(mock.URL()) + `
resource "nodeping_check" "test" {
  type   = "HTTPADV"
  target = "https://example.com/health"
` + attributes + `
}
`
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config(`
  follow = false
  invert = false
  ipv6   = false
  regex  = false
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.test", "follow", "false"),
					resource.TestCheckResourceAttr("nodeping_check.test", "regex", "false"),
				),
			},
			{
				Config: config(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("nodeping_check.test", "follow"),
					resource.TestCheckNoResourceAttr("nodeping_check.test", "invert"),
					resource.TestCheckNoResourceAttr("nodeping_check.test", "ipv6"),
					resource.TestCheckNoResourceAttr("nodeping_check.test", "regex"),
					expectStored(mock, "nodeping_check.test", "param", "follow", `false`),
				),
			},
		},
	})
}

// An update writes only what the configuration has and what it removed: a
// check that never had a value is not sent the one that clears it.
func TestAccCheckResource_updateLeavesOutValuesTheCheckNeverHad(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := func(label string) string {
		return providerConfig(mock.URL()) + fmt.Sprintf(`
resource "nodeping_check" "test" {
  type   = "HTTPADV"
  target = "https://example.com/health"
  label  = %q
}
`, label)
	}

	var id string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config("before"), Check: storeCheckID("nodeping_check.test", &id)},
			{
				Config: config("after"),
				Check: func(*terraform.State) error {
					updates := mock.CheckUpdates(id)
					if len(updates) != 1 {
						return fmt.Errorf("sent %d updates, want 1", len(updates))
					}
					for _, key := range []string{
						"postdata", "contentstring", "method", "statuscode", "regex", "invert",
						"follow", "ipv6", "servername", "warningdays", "dep", "runlocations",
						"description", "sendheaders", "receiveheaders", "fields",
					} {
						if v, ok := updates[0][key]; ok {
							return fmt.Errorf("the update sent %s = %v to a check that never had one", key, v)
						}
					}
					return nil
				},
			},
		},
	})
}

// NodePing can hold false for a boolean the configuration leaves out -- set so
// in the web interface, or cleared by an earlier apply. To NodePing that is
// the same as no value, so a refresh reads it as none rather than planning to
// remove a false.
func TestAccCheckResource_readsAStoredFalseAsUnset(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := providerConfig(mock.URL()) + `
resource "nodeping_check" "test" {
  type   = "HTTPADV"
  target = "https://example.com/health"
}
`

	var id string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config, Check: storeCheckID("nodeping_check.test", &id)},
			{
				PreConfig: func() {
					for _, key := range []string{"follow", "invert", "ipv6", "regex"} {
						mock.SetCheckParameter(id, key, false)
					}
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.TestCheckNoResourceAttr("nodeping_check.test", "follow"),
			},
		},
	})
}

// Kahoot's modules configure booleans NodePing stores as false explicitly
// (follow = false, regex = false), because an import has no configuration to
// compare with and has to read the check as it is.
func TestAccCheckResource_importReadsStoredFalseBooleans(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := providerConfig(mock.URL()) + `
resource "nodeping_check" "imported" {
  type          = "HTTPADV"
  target        = "https://example.com/health"
  label         = "acc-import-false"
  contentstring = "ok"
  follow        = false
  invert        = false
  ipv6          = false
  regex         = false
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

// NodePing merges sendheaders and receiveheaders per key: a smaller map, an
// empty one or none at all keeps every header, and only a header sent as null
// is deleted.
func TestAccCheckResource_removesHeaders(t *testing.T) {
	for _, attribute := range []string{"sendheaders", "receiveheaders"} {
		t.Run(attribute, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)

			config := func(headers string) string {
				return providerConfig(mock.URL()) + `
resource "nodeping_check" "test" {
  type   = "HTTPADV"
  target = "https://example.com/health"
` + headers + `
}
`
			}

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: config(attribute + ` = { "X-One" = "1", "X-Two" = "2" }`),
						Check:  resource.TestCheckResourceAttr("nodeping_check.test", attribute+".%", "2"),
					},
					{
						Config: config(attribute + ` = { "X-One" = "1" }`),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr("nodeping_check.test", attribute+".%", "1"),
							expectStored(mock, "nodeping_check.test", "param", attribute, `{"X-One":"1"}`),
						),
					},
					{
						Config: config(""),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckNoResourceAttr("nodeping_check.test", attribute+".%"),
							expectStored(mock, "nodeping_check.test", "param", attribute, `{}`),
						),
					},
				},
			})
		})
	}
}

// NodePing replaces a check's notifications with the list an update sends,
// and keeps them when the update leaves the list out. Removing some worked;
// removing the last one left the update without a list, and failed the apply
// with "block count changed from 0 to 1", as the last tag once did.
func TestAccCheckResource_removesTheLastNotification(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := func(notifications ...string) string {
		blocks := ""
		for _, id := range notifications {
			blocks += fmt.Sprintf(`
  notifications {
    contact_id = %q
  }
`, id)
		}
		return providerConfig(mock.URL()) + `
resource "nodeping_check" "test" {
  type   = "HTTP"
  target = "https://example.com/health"
` + blocks + `
}
`
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config("GROUP-1", "GROUP-2"),
				Check:  resource.TestCheckResourceAttr("nodeping_check.test", "notifications.#", "2"),
			},
			{
				Config: config("GROUP-2"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.test", "notifications.#", "1"),
					expectStored(mock, "nodeping_check.test", "top", "notifications", `[{"GROUP-2":{"delay":0,"schedule":"All"}}]`),
				),
			},
			{
				Config: config(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.test", "notifications.#", "0"),
					expectStored(mock, "nodeping_check.test", "top", "notifications", `[]`),
				),
			},
		},
	})
}

// NodePing ignores a description of "", null, false or 0, so a description
// can only be overwritten. The provider clears one by writing a single space,
// and reads that back as no description.
func TestAccCheckResource_clearsTheDescription(t *testing.T) {
	testClearRemoved(t, clearCase{
		name: "description", typ: "HTTP", set: `description = "Who to call when this fails"`,
		attribute: "description", where: "top", key: "description", cleared: `" "`,
	})
}

// A check whose description was cleared imports without one.
func TestAccCheckResource_importReadsAClearedDescriptionAsNone(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	mock.AddCheck("CLEARED", map[string]interface{}{
		"_id":         "CLEARED",
		"type":        "HTTP",
		"label":       "acc-cleared-description",
		"enable":      "active",
		"interval":    15,
		"description": " ",
		"parameters": map[string]interface{}{
			"target":    "https://example.com/health",
			"threshold": 5,
			"sens":      2,
		},
	})

	config := providerConfig(mock.URL()) + `
resource "nodeping_check" "imported" {
  type    = "HTTP"
  target  = "https://example.com/health"
  label   = "acc-cleared-description"
  enabled = true
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:          config,
				ResourceName:    "nodeping_check.imported",
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				ImportStateId:   "CLEARED",
			},
		},
	})
}

// NodePing ignores public = false sent as a boolean (finding 30); only the
// string "false" switches public reports off.
func TestAccCheckResource_switchesPublicOff(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := func(public string) string {
		return providerConfig(mock.URL()) + `
resource "nodeping_check" "test" {
  type   = "HTTP"
  target = "https://example.com/health"
  ` + public + `
}
`
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config(`public = true`),
				Check:  expectStored(mock, "nodeping_check.test", "top", "public", `true`),
			},
			{
				Config: config(`public = false`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.test", "public", "false"),
					expectStored(mock, "nodeping_check.test", "top", "public", `false`),
				),
			},
			{
				Config: config(`public = true`),
				Check:  expectStored(mock, "nodeping_check.test", "top", "public", `true`),
			},
			{
				// Left out, public is false by default.
				Config: config(``),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.test", "public", "false"),
					expectStored(mock, "nodeping_check.test", "top", "public", `false`),
				),
			},
		},
	})
}

// NodePing cannot remove a field, or a field's min, max or match, from a
// check: every shape tried keeps it (finding 31). Removing one replaces the
// check instead, and the plan says why in a warning. Changing a value and
// adding a field still update it in place; see TestAccCheckResource_fields.
func TestAccCheckResource_removingFieldsReplacesTheCheck(t *testing.T) {
	tests := []struct {
		name, before, after string
		// stored is the new check's fields as NodePing holds them.
		stored string
	}{
		{
			name:   "a field",
			before: `fields = { A = { name = "status", min = 200, max = 200 }, B = { name = "load.avg", max = 2.5 } }`,
			after:  `fields = { A = { name = "status", min = 200, max = 200 } }`,
			stored: `{"A":{"max":200,"min":200,"name":"status"}}`,
		},
		{
			name:   "a field's min",
			before: `fields = { A = { name = "status", min = 200, max = 200 } }`,
			after:  `fields = { A = { name = "status", max = 200 } }`,
			stored: `{"A":{"max":200,"name":"status"}}`,
		},
		{
			name:   "a field's match",
			before: `fields = { A = { name = "count", match = "1" } }`,
			after:  `fields = { A = { name = "count" } }`,
			stored: `{"A":{"name":"count"}}`,
		},
		{
			name:   "every field",
			before: `fields = { A = { name = "status", min = 200, max = 200 } }`,
			after:  ``,
			stored: ``,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)

			config := func(fields string) string {
				return providerConfig(mock.URL()) + `
resource "nodeping_check" "test" {
  type   = "HTTPPARSE"
  target = "https://example.com/stats.json"
  label  = "acc-fields"
  ` + fields + `
}
`
			}

			var before, after string
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{Config: config(tt.before), Check: storeCheckID("nodeping_check.test", &before)},
					{
						Config: config(tt.after),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{
								plancheck.ExpectResourceAction("nodeping_check.test", plancheck.ResourceActionDestroyBeforeCreate),
							},
						},
						Check: resource.ComposeAggregateTestCheckFunc(
							storeCheckID("nodeping_check.test", &after),
							expectStored(mock, "nodeping_check.test", "param", "fields", tt.stored),
							func(*terraform.State) error {
								if before == after {
									return fmt.Errorf("the check kept its ID %s, want a new check", before)
								}
								if _, ok := mock.GetCheck(before); ok {
									return fmt.Errorf("the old check %s is still in NodePing", before)
								}
								return nil
							},
						),
					},
				},
			})
		})
	}
}

// A value removed from a check in the NodePing web interface shows as drift,
// and the next apply puts it back (finding 36). Read used to keep the value
// it had in state whenever NodePing had none, so the plan stayed empty and
// the configuration was never applied again.
func TestAccCheckResource_valueRemovedInNodePingShowsAsDrift(t *testing.T) {
	tests := []struct {
		name      string
		attribute string
		remove    func(mock *testutil.MockNodePingServer, id string)
		where     string
		key, want string
	}{
		{
			name:      "description",
			attribute: "description",
			remove:    func(m *testutil.MockNodePingServer, id string) { m.SetCheckValue(id, "description", "") },
			where:     "top", key: "description", want: `"Who to call"`,
		},
		{
			name:      "dep",
			attribute: "dep",
			remove:    func(m *testutil.MockNodePingServer, id string) { m.SetCheckValue(id, "dep", false) },
			where:     "top", key: "dep", want: `"CHECK-0"`,
		},
		{
			name:      "contentstring",
			attribute: "contentstring",
			remove:    func(m *testutil.MockNodePingServer, id string) { m.SetCheckParameter(id, "contentstring", "") },
			where:     "param", key: "contentstring", want: `"ok"`,
		},
		{
			name:      "a header",
			attribute: "sendheaders.X-One",
			remove: func(m *testutil.MockNodePingServer, id string) {
				m.SetCheckParameter(id, "sendheaders", map[string]interface{}{})
			},
			where: "param", key: "sendheaders", want: `{"X-One":"1"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)

			config := providerConfig(mock.URL()) + `
resource "nodeping_check" "test" {
  type          = "HTTPADV"
  target        = "https://example.com/health"
  description   = "Who to call"
  dep           = "CHECK-0"
  contentstring = "ok"
  sendheaders   = { "X-One" = "1" }
}
`

			var id string
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{Config: config, Check: storeCheckID("nodeping_check.test", &id)},
					{
						PreConfig: func() { tt.remove(mock, id) },
						Config:    config,
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{
								plancheck.ExpectResourceAction("nodeping_check.test", plancheck.ResourceActionUpdate),
							},
						},
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttrSet("nodeping_check.test", tt.attribute),
							expectStored(mock, "nodeping_check.test", tt.where, tt.key, tt.want),
						),
					},
				},
			})
		})
	}
}

// A check configured with a description of a single space -- the value the
// provider clears a description with -- keeps it.
func TestAccCheckResource_blankDescriptionStays(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := providerConfig(mock.URL()) + `
resource "nodeping_check" "test" {
  type        = "HTTP"
  target      = "https://example.com/health"
  description = " "
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config, Check: resource.TestCheckResourceAttr("nodeping_check.test", "description", " ")},
			{Config: config, PlanOnly: true},
		},
	})
}
