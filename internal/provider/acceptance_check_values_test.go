package provider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// NodePing refuses some values of sens, threshold and interval, and accepts
// others without applying them: it ignores them or truncates them
// (docs/nodeping-api-quirks.md, quirk 13). The plan refuses all of them,
// before anything is sent.

// checkValues are the sens, threshold and interval of a check, as written in
// its configuration.
type checkValues struct {
	sens, threshold, interval string
}

func checkValuesConfig(url string, v checkValues) string {
	return providerConfig(url) + fmt.Sprintf(`
resource "nodeping_check" "test" {
  type      = "HTTP"
  target    = "https://example.com"
  label     = "acc-check-values"
  sens      = %s
  threshold = %s
  interval  = %s
}
`, v.sens, v.threshold, v.interval)
}

// Each value fails the plan with the validator's message, naming the
// attribute and the value.
func TestAccCheckResource_refusesValuesNodePingRejects(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	valid := checkValues{sens: "2", threshold: "5", interval: "5"}
	atLeast1 := `value\s+must\s+be\s+at\s+least\s+1`
	interval := `value\s+must\s+be\s+0\.25,\s+0\.5,\s+or\s+a\s+whole\s+number\s+of\s+at\s+least\s+1`

	tests := []struct {
		attribute, value, message string
	}{
		{"sens", "0", atLeast1},
		{"sens", "-1", atLeast1},
		{"threshold", "0", atLeast1},
		{"threshold", "-1", atLeast1},
		{"interval", "0", interval},
		{"interval", "0.3", interval},
		{"interval", "2.5", interval},
		{"interval", "-1", interval},
	}

	for _, tt := range tests {
		t.Run(tt.attribute+"="+tt.value, func(t *testing.T) {
			v := valid
			switch tt.attribute {
			case "sens":
				v.sens = tt.value
			case "threshold":
				v.threshold = tt.value
			case "interval":
				v.interval = tt.value
			}

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{{
					Config:   checkValuesConfig(mock.URL(), v),
					PlanOnly: true,
					ExpectError: regexp.MustCompile(`Invalid\s+Attribute\s+Value[\s\S]*` +
						`Attribute\s+` + tt.attribute + `\s+` + tt.message + `,\s+got:\s+` + regexp.QuoteMeta(tt.value) + `(\s|$)`),
				}},
			})
		})
	}

	for _, r := range mock.Requests() {
		if r.Method != "GET" {
			t.Errorf("a refused value sent %s %s", r.Method, r.Path)
		}
	}
}

// The values NodePing stores plan and apply as before. 0.25 and 0.5 pass the
// schema as well (TestIntervalValue); whether an account may use them depends
// on its NodePing plan, which the schema does not know.
func TestAccCheckResource_acceptsValuesNodePingStores(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	expect := func(v checkValues) resource.TestCheckFunc {
		return resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr("nodeping_check.test", "sens", v.sens),
			resource.TestCheckResourceAttr("nodeping_check.test", "threshold", v.threshold),
			resource.TestCheckResourceAttr("nodeping_check.test", "interval", v.interval),
		)
	}

	lowest := checkValues{sens: "1", threshold: "1", interval: "1"}
	others := checkValues{sens: "10", threshold: "1", interval: "1440"}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: checkValuesConfig(mock.URL(), lowest), Check: expect(lowest)},
			{Config: checkValuesConfig(mock.URL(), others), Check: expect(others)},
		},
	})
}
