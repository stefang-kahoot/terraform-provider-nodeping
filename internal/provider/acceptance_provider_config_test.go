package provider_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// An api_token that comes from another resource is not known while the
// resource is still to be created. The plan fails, saying so, rather than
// reading the token as "" and calling it missing: NODEPING_API_TOKEN does not
// stand in for it, since the configured token overrides it at apply. Once its
// source is applied the token is known, and the same configuration plans.
func TestAccProvider_unknownAPIToken(t *testing.T) {
	t.Setenv("NODEPING_API_TOKEN", "acc-test-token")
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	const token = `
resource "terraform_data" "token" {
  input = "acc-test-token"
}
`
	config := token + fmt.Sprintf(`
provider "nodeping" {
  api_token = terraform_data.token.output
  api_url   = %q
}

resource "nodeping_contactgroup" "test" {
  name = "acc-unknown-token"
}
`, mock.URL())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				ExpectError: regexp.MustCompile(`Error:\s+Unknown\s+NodePing\s+Provider\s+Setting[\s\S]*` +
					`api_token\s+is\s+known\s+only\s+after\s+apply`),
			},
			{Config: token},
			{
				Config: config,
				Check:  resource.TestCheckResourceAttr("nodeping_contactgroup.test", "name", "acc-unknown-token"),
			},
		},
	})
}

// failingAPI answers every request with a server error, counting them.
func failingAPI(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, `{"error":"Internal Server Error"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

// max_retries = 0 sends a request that fails once and no more. The client
// used to take 0 for unset and retry it 3 times all the same. The waits are
// 0 too, which the client also took for unset, so the retries of
// max_retries = 1 come at once.
func TestAccProvider_maxRetries(t *testing.T) {
	for _, retries := range []int{0, 1} {
		t.Run(fmt.Sprintf("max_retries=%d", retries), func(t *testing.T) {
			server, requests := failingAPI(t)

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`
provider "nodeping" {
  api_token      = "acc-test-token"
  api_url        = %q
  max_retries    = %d
  retry_wait_min = 0
  retry_wait_max = 0
}

data "nodeping_contactgroups" "all" {}
`, server.URL, retries),
						ExpectError: regexp.MustCompile(`Error Reading Contact Groups`),
					},
				},
			})

			if got, want := requests.Load(), int64(retries+1); got != want {
				t.Errorf("NodePing got %d requests, want %d", got, want)
			}
		})
	}
}

// A setting the client cannot use fails the plan, rather than being taken as
// unset: max_retries and the waits can be 0 but not less, and rate_limit must
// allow some requests.
func TestAccProvider_invalidSettings(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	tests := []struct {
		setting string
		error   string
	}{
		{"max_retries = -1", `max_retries\s+value\s+must\s+be\s+at\s+least\s+0,\s+got:\s+-1`},
		{"retry_wait_min = -1", `retry_wait_min\s+value\s+must\s+be\s+at\s+least\s+0,\s+got:\s+-1`},
		{"retry_wait_max = -1", `retry_wait_max\s+value\s+must\s+be\s+at\s+least\s+0,\s+got:\s+-1`},
		{"rate_limit = 0", `rate_limit\s+value\s+must\s+be\s+greater\s+than\s+0,\s+got:\s+0`},
		{"rate_limit = -1", `rate_limit\s+value\s+must\s+be\s+greater\s+than\s+0,\s+got:\s+-1`},
	}

	steps := make([]resource.TestStep, 0, len(tests))
	for _, tt := range tests {
		steps = append(steps, resource.TestStep{
			Config: fmt.Sprintf(`
provider "nodeping" {
  api_token = "acc-test-token"
  api_url   = %q
  %s
}

data "nodeping_contactgroups" "all" {}
`, mock.URL(), tt.setting),
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(tt.error),
		})
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps:                    steps,
	})
}
