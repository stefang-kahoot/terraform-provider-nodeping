package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/nodeping/terraform-provider-nodeping/testutil"
)

// Import has to land the same state an apply would. Nothing in the suite
// compared the two before these tests, and that is exactly the gap a migration
// falls into: an attribute the read path forgets to map comes back null, the
// plan stays empty, and the omission looks like success. It surfaces much
// later, when an unrelated edit rebuilds the API request out of that state and
// the forgotten parameters are dropped from the live check.
//
// ImportStateVerify is what closes the gap. It re-reads the resource through
// ImportState and diffs the result against the state the preceding apply
// produced, so a missing field fails the test instead of the estate.

// checkImportCase is one check type whose defining parameters live outside the
// common envelope -- the types most exposed to an unmapped attribute.
type checkImportCase struct {
	name string
	body string
	// ignore lists attributes that cannot survive a round trip because the API
	// never echoes them back. Credentials only; anything else here is a bug
	// being papered over.
	ignore []string
}

func TestAccCheckResource_importRoundTripsTypeSpecificParameters(t *testing.T) {
	tests := []checkImportCase{
		{
			name: "MYSQL keeps database and query",
			body: `
  type     = "MYSQL"
  target   = "db.example.com"
  label    = "acc-import-mysql"
  port     = 3306
  username = "monitor"
  password = "s3cret"
  database = "app"
  query    = "SELECT 1"
`,
			ignore: []string{"password"},
		},
		{
			name: "SMTP keeps the envelope address and the TLS mode",
			body: `
  type     = "SMTP"
  target   = "mail.example.com"
  label    = "acc-import-smtp"
  port     = 587
  email    = "probe@example.com"
  secure   = "starttls"
  username = "probe"
  password = "s3cret"
`,
			ignore: []string{"password"},
		},
		{
			name: "DNS keeps the response section and the transport",
			body: `
  type         = "DNS"
  target       = "8.8.8.8"
  label        = "acc-import-dns"
  dnstype      = "A"
  dnstoresolve = "example.com"
  dnssection   = "answer"
  dnsrd        = true
  transport    = "tcp"
`,
		},
		{
			name: "HTTPADV keeps the request body",
			body: `
  type     = "HTTPADV"
  target   = "https://example.com/api"
  label    = "acc-import-httpadv"
  method   = "POST"
  postdata = "{\"ping\":true}"

  sendheaders = {
    "Content-Type" = "application/json"
  }
`,
		},
		{
			name: "MONGODB keeps the namespace",
			body: `
  type      = "MONGODB"
  target    = "mongo.example.com"
  label     = "acc-import-mongodb"
  namespace = "app.events"
`,
		},
		{
			name: "SNMP keeps the protocol version",
			body: `
  type    = "SNMP"
  target  = "1.2.3.4"
  label   = "acc-import-snmp"
  snmpv   = "2c"
  snmpcom = "public-but-secret"
`,
			// snmpcom is a shared secret and is treated like password: the
			// provider never reads it back out of an API response.
			ignore: []string{"snmpcom"},
		},
		{
			name: "HTTP keeps the preferred probe location",
			body: `
  type    = "HTTP"
  target  = "https://example.com"
  label   = "acc-import-homeloc"
  homeloc = "nam"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)

			config := providerConfig(mock.URL()) + `
resource "nodeping_check" "imported" {` + tt.body + `}
`

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{Config: config},
					{
						Config:                  config,
						ResourceName:            "nodeping_check.imported",
						ImportState:             true,
						ImportStateVerify:       true,
						ImportStateVerifyIgnore: tt.ignore,
					},
				},
			})
		})
	}
}

// A check that notifies someone has to import with that notification intact,
// including the schedule. An unset schedule is "All" to NodePing; reading it
// back as the empty string makes the resource and the data sources disagree
// about the same check.
func TestAccCheckResource_importRoundTripsNotifications(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := providerConfig(mock.URL()) + `
resource "nodeping_contact" "notify" {
  name = "acc-import-notify"

  address {
    type    = "email"
    address = "notify@example.com"
  }
}

resource "nodeping_check" "imported" {
  type   = "HTTP"
  target = "https://example.com"
  label  = "acc-import-notifications"

  notifications {
    contact_id = nodeping_contact.notify.address[0].id
    delay      = 5
    schedule   = "Days"
  }

  notifications {
    contact_id = nodeping_contact.notify.id
    delay      = 0
  }
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.imported", "notifications.0.schedule", "Days"),
					// Left unset in the configuration, so it has to settle on
					// the API's own default rather than "".
					resource.TestCheckResourceAttr("nodeping_check.imported", "notifications.1.schedule", "All"),
				),
			},
			{
				Config:            config,
				ResourceName:      "nodeping_check.imported",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// The "customer_id:id" import ID used to be accepted and documented for all
// three resources. It scoped the import's own GET and nothing else, so the
// read that follows an import went to the base account, 404'd, and left
// Terraform proposing to recreate a resource that already existed in the
// SubAccount. There is nowhere to pin the account either: customer_id is
// Computed, so a configuration cannot set it.
//
// It now fails at the import, pointing at the provider alias that does work.
// Failing is the whole point: half-importing into the wrong account is the
// outcome worth preventing.
func TestAccResources_rejectTheSubAccountImportPrefix(t *testing.T) {
	tests := []struct {
		name     string
		config   string
		resource string
		importID string
	}{
		{
			name: "check",
			config: `
resource "nodeping_check" "sub" {
  type   = "HTTP"
  target = "https://example.com"
  label  = "acc-subaccount-check"
}
`,
			resource: "nodeping_check.sub",
			importID: "201205050153W2Q4C:201205050153W2Q4C-0J2HSIRF",
		},
		{
			name: "contact",
			config: `
resource "nodeping_contact" "sub" {
  name = "acc-subaccount-contact"

  address {
    type    = "email"
    address = "sub@example.com"
  }
}
`,
			resource: "nodeping_contact.sub",
			importID: "201205050153W2Q4C:201205050153W2Q4C-BKPGH",
		},
		{
			name: "contactgroup",
			config: `
resource "nodeping_contactgroup" "sub" {
  name = "acc-subaccount-group"
}
`,
			resource: "nodeping_contactgroup.sub",
			importID: "201205050153W2Q4C:201205050153W2Q4C-G-1ZIYU",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)

			config := providerConfig(mock.URL()) + tt.config

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{Config: config},
					{
						Config:        config,
						ResourceName:  tt.resource,
						ImportState:   true,
						ImportStateId: tt.importID,
						// The message has to name the way forward, not just
						// refuse. Anyone hitting this is mid-migration.
						ExpectError: regexp.MustCompile(`(?s)no longer accepted.*nodeping\.subaccount`),
					},
				},
			})
		})
	}
}

func TestAccContactResource_importRoundTrips(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := providerConfig(mock.URL()) + `
resource "nodeping_contact" "imported" {
  name     = "acc-import-contact"
  custrole = "notify"

  address {
    type    = "email"
    address = "import@example.com"
  }
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				Config:            config,
				ResourceName:      "nodeping_contact.imported",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// Webhook addresses carry the attributes most likely to be dropped on the way
// back in: a method, headers, query strings and a JSON body.
func TestAccContactResource_importRoundTripsWebhookAddress(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := providerConfig(mock.URL()) + `
resource "nodeping_contact" "imported" {
  name = "acc-import-webhook"

  address {
    type    = "webhook"
    address = "https://hooks.example.com/notify"
    action  = "post"
    data    = "{\"text\":\"{label} is {event}\"}"

    headers = {
      "content-type" = "application/json"
    }

    querystrings = {
      "key" = "1"
    }

    suppress_up    = true
    suppress_first = true
  }
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				Config:            config,
				ResourceName:      "nodeping_contact.imported",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccContactGroupResource_importRoundTrips(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := providerConfig(mock.URL()) + accContactGroupBase + `
resource "nodeping_contactgroup" "imported" {
  name = "acc-import-group"

  members = [
    nodeping_contact.grp_a.address[0].id,
    nodeping_contact.grp_b.address[0].id,
  ]
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				Config:            config,
				ResourceName:      "nodeping_contactgroup.imported",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// A group with no name at all. The API answers with an empty string, which
// must not become a literal "" in the imported state when the configuration
// leaves the attribute unset.
func TestAccContactGroupResource_importRoundTripsUnnamed(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	config := providerConfig(mock.URL()) + `
resource "nodeping_contactgroup" "imported" {
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				Config:            config,
				ResourceName:      "nodeping_contactgroup.imported",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
