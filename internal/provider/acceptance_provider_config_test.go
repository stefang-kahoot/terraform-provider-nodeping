package provider_test

import (
	"fmt"
	"regexp"
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
