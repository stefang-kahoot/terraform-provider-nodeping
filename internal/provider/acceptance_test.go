package provider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/nodeping/terraform-provider-nodeping/internal/provider"
	"github.com/nodeping/terraform-provider-nodeping/testutil"
)

// These tests drive a real `terraform plan`/`apply` cycle against an in-process
// mock of the NodePing API. They exercise the parts unit tests cannot reach:
// schema/state round-tripping, plan consistency and the full CRUD lifecycle.
//
// They only run when TF_ACC=1 and need a `terraform` binary on PATH (see
// `make test-acceptance`, which supplies one via Docker).

func protoV6ProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"nodeping": providerserver.NewProtocol6WithError(provider.New("test")()),
	}
}

// providerConfig points the provider at the mock server instead of the real
// NodePing API.
func providerConfig(url string) string {
	return fmt.Sprintf(`
provider "nodeping" {
  api_token = "acc-test-token"
  api_url   = %q
}
`, url)
}

func TestAccContactResource_lifecycle(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			// Create
			{
				Config: providerConfig(mock.URL()) + `
resource "nodeping_contact" "test" {
  name = "acc-contact"

  address {
    type    = "email"
    address = "acc@example.com"
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_contact.test", "name", "acc-contact"),
					resource.TestCheckResourceAttrSet("nodeping_contact.test", "id"),
					resource.TestCheckResourceAttr("nodeping_contact.test", "address.#", "1"),
					resource.TestCheckResourceAttr("nodeping_contact.test", "address.0.address", "acc@example.com"),
					resource.TestCheckResourceAttr("nodeping_contact.test", "address.0.type", "email"),
				),
			},
			// Update in place
			{
				Config: providerConfig(mock.URL()) + `
resource "nodeping_contact" "test" {
  name = "acc-contact-renamed"

  address {
    type    = "email"
    address = "acc@example.com"
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_contact.test", "name", "acc-contact-renamed"),
				),
			},
		},
	})
}

func TestAccCheckResource_lifecycle(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			// Create
			{
				Config: providerConfig(mock.URL()) + `
resource "nodeping_check" "test" {
  type     = "HTTP"
  target   = "https://example.com"
  label    = "acc-check"
  interval = 5
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.test", "label", "acc-check"),
					resource.TestCheckResourceAttr("nodeping_check.test", "type", "HTTP"),
					resource.TestCheckResourceAttr("nodeping_check.test", "target", "https://example.com"),
					resource.TestCheckResourceAttr("nodeping_check.test", "interval", "5"),
					resource.TestCheckResourceAttrSet("nodeping_check.test", "id"),
				),
			},
			// Update the label in place
			{
				Config: providerConfig(mock.URL()) + `
resource "nodeping_check" "test" {
  type     = "HTTP"
  target   = "https://example.com"
  label    = "acc-check-renamed"
  interval = 5
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.test", "label", "acc-check-renamed"),
				),
			},
		},
	})
}

// A second plan straight after apply must be empty. This is the check that
// catches the "provider produced inconsistent result after apply" class of bug,
// where the API echoes values back in a different shape than the config.
func TestAccCheckResource_planIsEmptyAfterApply(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := providerConfig(mock.URL()) + `
resource "nodeping_check" "idempotent" {
  type     = "HTTP"
  target   = "https://example.com"
  label    = "acc-idempotent"
  interval = 5
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
			},
			{
				Config:   config,
				PlanOnly: true, // fails the test if the plan is not empty
			},
		},
	})
}

func TestAccContactResource_planIsEmptyAfterApply(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := providerConfig(mock.URL()) + `
resource "nodeping_contact" "idempotent" {
  name = "acc-idempotent"

  address {
    type    = "email"
    address = "idem@example.com"
  }
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
			},
			{
				Config:   config,
				PlanOnly: true,
			},
		},
	})
}

// Schema validators must reject bad input at plan time, before any API call
// happens. Terraform never reaches the provider's CRUD code for these.
func TestAccCheckResource_rejectsInvalidValues(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	tests := []struct {
		name       string
		body       string
		wantErrRex *regexp.Regexp
	}{
		{
			name: "unknown check type",
			body: `
  type   = "NOT_A_REAL_TYPE"
  target = "https://example.com"
`,
			wantErrRex: regexp.MustCompile(`(?is)type`),
		},
		{
			name: "unsupported http method",
			body: `
  type   = "HTTP"
  target = "https://example.com"
  method = "FETCH"
`,
			wantErrRex: regexp.MustCompile(`(?is)method`),
		},
		{
			name: "warningdays below minimum",
			body: `
  type        = "SSL"
  target      = "https://example.com"
  warningdays = 0
`,
			wantErrRex: regexp.MustCompile(`(?is)warningdays`),
		},
		{
			// NodePing documents the acceptable range as -90 to 0.
			name: "volumemin below the documented range",
			body: `
  type         = "AUDIO"
  target       = "https://example.com/stream.mp3"
  verifyvolume = true
  volumemin    = -91
`,
			wantErrRex: regexp.MustCompile(`(?is)volumemin`),
		},
		{
			name: "volumemin above the documented range",
			body: `
  type         = "AUDIO"
  target       = "https://example.com/stream.mp3"
  verifyvolume = true
  volumemin    = 1
`,
			wantErrRex: regexp.MustCompile(`(?is)volumemin`),
		},
		{
			name: "unsupported snmp version",
			body: `
  type   = "SNMP"
  target = "1.2.3.4"
  snmpv  = "3"
`,
			wantErrRex: regexp.MustCompile(`(?is)snmpv`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: providerConfig(mock.URL()) + `
resource "nodeping_check" "invalid" {` + tt.body + `}
`,
						ExpectError: tt.wantErrRex,
					},
				},
			})
		})
	}
}

func TestAccContactResource_rejectsInvalidValues(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	tests := []struct {
		name       string
		config     string
		wantErrRex *regexp.Regexp
	}{
		{
			name: "unknown custrole",
			config: `
resource "nodeping_contact" "invalid" {
  name     = "bad"
  custrole = "superuser"

  address {
    type    = "email"
    address = "a@example.com"
  }
}
`,
			wantErrRex: regexp.MustCompile(`(?is)custrole`),
		},
		{
			name: "unknown address type",
			config: `
resource "nodeping_contact" "invalid" {
  name = "bad"

  address {
    type    = "carrier-pigeon"
    address = "a@example.com"
  }
}
`,
			wantErrRex: regexp.MustCompile(`(?is)type`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config:      providerConfig(mock.URL()) + tt.config,
						ExpectError: tt.wantErrRex,
					},
				},
			})
		})
	}
}

// Regression test for the unknown-value crash: a check created while the
// provider has no default_tags configured leaves `tags` unknown at plan time.
// Converting that unknown into []string used to abort the apply with
// "Value Conversion Error".
func TestAccCheckResource_appliesWithoutDefaultTags(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				// Note: no default_tags on the provider and no tags on the
				// resource. This is the default configuration.
				Config: providerConfig(mock.URL()) + `
resource "nodeping_check" "no_tags" {
  type   = "HTTP"
  target = "https://example.com"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("nodeping_check.no_tags", "id"),
				),
			},
		},
	})
}

// AUDIO checks with volume detection. Per the NodePing API, verifyvolume is an
// "optional boolean to enable the volume detection feature" and volumemin an
// "optional integer (acceptable range -90 to 0)".
func TestAccCheckResource_audioVolumeDetection(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := providerConfig(mock.URL()) + `
resource "nodeping_check" "audio" {
  type   = "AUDIO"
  target = "https://example.com/stream.mp3"
  label  = "acc-audio"

  verifyvolume = true
  volumemin    = -40
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.audio", "type", "AUDIO"),
					resource.TestCheckResourceAttr("nodeping_check.audio", "verifyvolume", "true"),
					resource.TestCheckResourceAttr("nodeping_check.audio", "volumemin", "-40"),
				),
			},
			{
				Config:   config,
				PlanOnly: true,
			},
		},
	})
}

// Contact groups bundle contact *address* IDs. The addresses are created via a
// nodeping_contact so the IDs are real ones the API handed out.
const accContactGroupBase = `
resource "nodeping_contact" "grp_a" {
  name = "acc-group-member-a"

  address {
    type    = "email"
    address = "a@example.com"
  }
}

resource "nodeping_contact" "grp_b" {
  name = "acc-group-member-b"

  address {
    type    = "email"
    address = "b@example.com"
  }
}
`

func TestAccContactGroupResource_lifecycle(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			// Create with a single member
			{
				Config: providerConfig(mock.URL()) + accContactGroupBase + `
resource "nodeping_contactgroup" "test" {
  name    = "acc-group"
  members = [nodeping_contact.grp_a.address[0].id]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_contactgroup.test", "name", "acc-group"),
					resource.TestCheckResourceAttrSet("nodeping_contactgroup.test", "id"),
					resource.TestCheckResourceAttrSet("nodeping_contactgroup.test", "customer_id"),
					resource.TestCheckResourceAttr("nodeping_contactgroup.test", "members.#", "1"),
					resource.TestCheckResourceAttrPair(
						"nodeping_contactgroup.test", "members.0",
						"nodeping_contact.grp_a", "address.0.id",
					),
				),
			},
			// Rename and add a second member
			{
				Config: providerConfig(mock.URL()) + accContactGroupBase + `
resource "nodeping_contactgroup" "test" {
  name = "acc-group-renamed"

  members = [
    nodeping_contact.grp_a.address[0].id,
    nodeping_contact.grp_b.address[0].id,
  ]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_contactgroup.test", "name", "acc-group-renamed"),
					resource.TestCheckResourceAttr("nodeping_contactgroup.test", "members.#", "2"),
				),
			},
			// Clearing the membership must actually clear it, not be dropped
			// from the request as an omitted field.
			{
				Config: providerConfig(mock.URL()) + accContactGroupBase + `
resource "nodeping_contactgroup" "test" {
  name    = "acc-group-renamed"
  members = []
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_contactgroup.test", "members.#", "0"),
				),
			},
		},
	})
}

func TestAccContactGroupResource_planIsEmptyAfterApply(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := providerConfig(mock.URL()) + accContactGroupBase + `
resource "nodeping_contactgroup" "idempotent" {
  name    = "acc-group-idempotent"
  members = [nodeping_contact.grp_a.address[0].id]
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{Config: config, PlanOnly: true},
		},
	})
}

// A group with neither a name nor members is valid per the API; both
// attributes are optional.
func TestAccContactGroupResource_minimal(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := providerConfig(mock.URL()) + `
resource "nodeping_contactgroup" "minimal" {
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("nodeping_contactgroup.minimal", "id"),
				),
			},
			{Config: config, PlanOnly: true},
		},
	})
}

func TestAccContactGroupDataSource_readsBack(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(mock.URL()) + accContactGroupBase + `
resource "nodeping_contactgroup" "src" {
  name    = "acc-group-ds"
  members = [nodeping_contact.grp_a.address[0].id]
}

data "nodeping_contactgroup" "by_id" {
  id = nodeping_contactgroup.src.id
}

data "nodeping_contactgroups" "all" {
  depends_on = [nodeping_contactgroup.src]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.nodeping_contactgroup.by_id", "name", "acc-group-ds"),
					resource.TestCheckResourceAttr("data.nodeping_contactgroup.by_id", "members.#", "1"),
					resource.TestCheckResourceAttrPair(
						"data.nodeping_contactgroup.by_id", "members.0",
						"nodeping_contactgroup.src", "members.0",
					),
					resource.TestCheckResourceAttr("data.nodeping_contactgroups.all", "contactgroups.#", "1"),
					resource.TestCheckResourceAttr("data.nodeping_contactgroups.all", "contactgroups.0.name", "acc-group-ds"),
				),
			},
		},
	})
}

// Regression test for the gap tracked in #5: the check data sources used to
// expose only the common envelope, so no check-type specific parameter could
// be read back.
func TestAccCheckDataSource_exposesTypeSpecificParameters(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(mock.URL()) + `
resource "nodeping_check" "src" {
  type          = "HTTPCONTENT"
  target        = "https://example.com/health"
  label         = "acc-ds-params"
  interval      = 5
  contentstring = "all good"
  method        = "GET"
  statuscode    = 200
  follow        = true
  port          = 8443
  username      = "svc"
  password      = "s3cret"
  warningdays   = 30
}

data "nodeping_check" "by_id" {
  id = nodeping_check.src.id
}

data "nodeping_checks" "all" {
  type       = "HTTPCONTENT"
  depends_on = [nodeping_check.src]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// The envelope still works.
					resource.TestCheckResourceAttr("data.nodeping_check.by_id", "label", "acc-ds-params"),
					resource.TestCheckResourceAttr("data.nodeping_check.by_id", "type", "HTTPCONTENT"),
					resource.TestCheckResourceAttr("data.nodeping_check.by_id", "target", "https://example.com/health"),

					// The point of the change: type-specific parameters.
					resource.TestCheckResourceAttr("data.nodeping_check.by_id", "contentstring", "all good"),
					resource.TestCheckResourceAttr("data.nodeping_check.by_id", "method", "GET"),
					resource.TestCheckResourceAttr("data.nodeping_check.by_id", "statuscode", "200"),
					resource.TestCheckResourceAttr("data.nodeping_check.by_id", "follow", "true"),
					resource.TestCheckResourceAttr("data.nodeping_check.by_id", "port", "8443"),
					resource.TestCheckResourceAttr("data.nodeping_check.by_id", "username", "svc"),
					resource.TestCheckResourceAttr("data.nodeping_check.by_id", "warningdays", "30"),

					// The plural carries the same shape.
					resource.TestCheckResourceAttr("data.nodeping_checks.all", "checks.#", "1"),
					resource.TestCheckResourceAttr("data.nodeping_checks.all", "checks.0.contentstring", "all good"),
					resource.TestCheckResourceAttr("data.nodeping_checks.all", "checks.0.statuscode", "200"),
					resource.TestCheckResourceAttr("data.nodeping_checks.all", "checks.0.port", "8443"),
				),
			},
		},
	})
}

// Credentials must never reach state through a data source, even when the
// resource that created the check set them.
func TestAccCheckDataSource_omitsCredentials(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(mock.URL()) + `
resource "nodeping_check" "creds" {
  type     = "SNMP"
  target   = "1.2.3.4"
  label    = "acc-ds-creds"
  username = "svc"
  password = "s3cret"
  snmpv    = "2c"
  snmpcom  = "public-but-secret"
}

data "nodeping_check" "creds" {
  id = nodeping_check.creds.id
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Non-secret neighbours are readable...
					resource.TestCheckResourceAttr("data.nodeping_check.creds", "username", "svc"),
					resource.TestCheckResourceAttr("data.nodeping_check.creds", "snmpv", "2c"),
					// ...while the secrets have no attribute at all.
					resource.TestCheckNoResourceAttr("data.nodeping_check.creds", "password"),
					resource.TestCheckNoResourceAttr("data.nodeping_check.creds", "snmpcom"),
				),
			},
		},
	})
}

// Regression test: a check with a password used to fail every apply with
// ".password: inconsistent values for sensitive attribute". NodePing does not
// echo credentials back, so mapping the response nulled the configured value.
// Broken since the initial commit; any check type using password auth
// (MYSQL, PGSQL, IMAP4, POP3, SMTP, FTP, SSH, HTTPADV) was affected.
func TestAccCheckResource_passwordSurvivesApply(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := providerConfig(mock.URL()) + `
resource "nodeping_check" "authed" {
  type     = "MYSQL"
  target   = "db.example.com"
  label    = "acc-password"
  port     = 3306
  username = "monitor"
  password = "s3cret"
  database = "app"
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.authed", "password", "s3cret"),
					resource.TestCheckResourceAttr("nodeping_check.authed", "username", "monitor"),
					resource.TestCheckResourceAttr("nodeping_check.authed", "database", "app"),
				),
			},
			// And it must not drift on the next plan either.
			{Config: config, PlanOnly: true},
		},
	})
}

// The notifications block is readable through the data sources too, so a
// config can discover who a check currently notifies.
func TestAccCheckDataSource_exposesNotifications(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(mock.URL()) + `
resource "nodeping_contact" "notify" {
  name = "acc-ds-notify"

  address {
    type    = "email"
    address = "notify@example.com"
  }
}

resource "nodeping_check" "notified" {
  type   = "HTTP"
  target = "https://example.com"
  label  = "acc-ds-notifications"

  notifications {
    contact_id = nodeping_contact.notify.address[0].id
    delay      = 5
    schedule   = "Days"
  }
}

data "nodeping_check" "notified" {
  id = nodeping_check.notified.id
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.nodeping_check.notified", "notifications.#", "1"),
					resource.TestCheckResourceAttr("data.nodeping_check.notified", "notifications.0.delay", "5"),
					resource.TestCheckResourceAttr("data.nodeping_check.notified", "notifications.0.schedule", "Days"),
					resource.TestCheckResourceAttrPair(
						"data.nodeping_check.notified", "notifications.0.contact_id",
						"nodeping_contact.notify", "address.0.id",
					),
				),
			},
		},
	})
}

// The provider's default_tags used to be merged into the check's own `tags`
// from ModifyPlan. `tags` is the configuration's value, so Terraform rejected
// the result outright -- any check that set tags of its own failed to plan
// with "Provider produced invalid plan". The merge now lands in `tags_all`,
// leaving `tags` as written.
func TestAccCheckResource_defaultTagsMergeIntoTagsAll(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	providerWithDefaults := fmt.Sprintf(`
provider "nodeping" {
  api_token    = "acc-test-token"
  api_url      = %q
  default_tags = ["managed-by-terraform", "owner-team-sre"]
}
`, mock.URL())

	config := providerWithDefaults + `
resource "nodeping_check" "tagged" {
  type   = "HTTP"
  target = "https://example.com"
  label  = "acc-tags"
  tags   = ["website"]
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					// tags stays exactly what the configuration said.
					resource.TestCheckResourceAttr("nodeping_check.tagged", "tags.#", "1"),
					resource.TestCheckResourceAttr("nodeping_check.tagged", "tags.0", "website"),
					// tags_all carries the defaults ahead of it.
					resource.TestCheckResourceAttr("nodeping_check.tagged", "tags_all.#", "3"),
					resource.TestCheckResourceAttr("nodeping_check.tagged", "tags_all.0", "managed-by-terraform"),
					resource.TestCheckResourceAttr("nodeping_check.tagged", "tags_all.1", "owner-team-sre"),
					resource.TestCheckResourceAttr("nodeping_check.tagged", "tags_all.2", "website"),
				),
			},
			{
				// The whole point: a second plan is empty.
				Config:   config,
				PlanOnly: true,
			},
		},
	})
}

// A check with no tags of its own still gets the provider's defaults, and
// tags_all must be a known empty list rather than null when there are none.
func TestAccCheckResource_defaultTagsWithoutCheckTags(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := fmt.Sprintf(`
provider "nodeping" {
  api_token    = "acc-test-token"
  api_url      = %q
  default_tags = ["managed-by-terraform"]
}

resource "nodeping_check" "untagged" {
  type   = "HTTP"
  target = "https://example.com"
}
`, mock.URL())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("nodeping_check.untagged", "tags.#"),
					resource.TestCheckResourceAttr("nodeping_check.untagged", "tags_all.#", "1"),
					resource.TestCheckResourceAttr("nodeping_check.untagged", "tags_all.0", "managed-by-terraform"),
				),
			},
			{
				Config:   config,
				PlanOnly: true,
			},
		},
	})
}
