package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
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
	// ignore lists attributes that cannot survive a round trip because the
	// provider does not read them back, although NodePing returns them.
	// Credentials only; anything else here is a bug being papered over.
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
			name: "HTTPPARSE keeps its fields",
			body: `
  type   = "HTTPPARSE"
  target = "https://example.com/stats.json"
  label  = "acc-import-httpparse"

  fields = {
    A = { name = "status", min = 200, max = 200 }
    B = { name = "load.avg", max = 2.5 }
    C = { name = "queue.length", min = 0 }
  }
`,
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

// Older checks store a notification in a short form, the contact mapped
// straight to its schedule ({"<contact>": "All"}) with no delay. Read as no
// notification at all, such a check could only be imported with a plan that
// writes, or with a configuration falsely claiming nobody is notified. The
// check is seeded the way NodePing returns one, and the configuration says
// what NodePing says, so the import must plan nothing.
func TestAccCheckResource_importReadsShortFormNotifications(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	mock.AddCheck("LEGACY-NOTIFY", map[string]interface{}{
		"_id":      "LEGACY-NOTIFY",
		"type":     "SSL",
		"label":    "acc-legacy-notifications",
		"enable":   "active",
		"interval": 60,
		"notifications": []interface{}{
			map[string]interface{}{"CONTACT-SHORT": "All"},
			map[string]interface{}{"CONTACT-OBJECT": map[string]interface{}{"delay": 5, "schedule": "Nights"}},
		},
		"parameters": map[string]interface{}{
			"target":      "https://example.com/",
			"threshold":   10,
			"sens":        2,
			"warningdays": 30,
		},
	})

	config := providerConfig(mock.URL()) + `
resource "nodeping_check" "imported" {
  type        = "SSL"
  target      = "https://example.com/"
  enabled     = true
  interval    = 60
  threshold   = 10
  sens        = 2
  warningdays = 30

  notifications {
    contact_id = "CONTACT-SHORT"
    delay      = 0
    schedule   = "All"
  }

  notifications {
    contact_id = "CONTACT-OBJECT"
    delay      = 5
    schedule   = "Nights"
  }
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
				ImportStateId:   "LEGACY-NOTIFY",
			},
		},
	})
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

// NodePing stores some headers as null -- {"Host": null} -- meaning no such
// header. Read as "", the check could not be imported without a plan: leaving
// sendheaders out planned {"Host" = ""} -> null, and { "Host" = null } an
// update with no visible difference. Only { "Host" = "" } planned clean, and
// that would write an empty Host header on the next apply. With the null
// dropped on read, a check whose only header is null has no sendheaders.
func TestAccCheckResource_importReadsNullHeadersAsAbsent(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	mock.AddCheck("NULL-HEADER", map[string]interface{}{
		"_id":      "NULL-HEADER",
		"type":     "HTTPADV",
		"label":    "acc-null-header",
		"enable":   "active",
		"interval": 1,
		"notifications": []interface{}{
			map[string]interface{}{"CONTACT-1": map[string]interface{}{"delay": 0, "schedule": "All"}},
		},
		"parameters": map[string]interface{}{
			"target":         "https://example.com/status",
			"threshold":      10,
			"sens":           2,
			"method":         "GET",
			"statuscode":     200,
			"follow":         false,
			"invert":         false,
			"ipv6":           false,
			"contentstring":  "",
			"postdata":       "",
			"data":           map[string]interface{}{},
			"sendheaders":    map[string]interface{}{"Host": nil},
			"receiveheaders": map[string]interface{}{"Server": nil},
		},
	})

	config := providerConfig(mock.URL()) + `
resource "nodeping_check" "imported" {
  type       = "HTTPADV"
  target     = "https://example.com/status"
  enabled    = true
  interval   = 1
  threshold  = 10
  sens       = 2
  method     = "GET"
  statuscode = 200
  follow     = false
  invert     = false
  ipv6       = false

  notifications {
    contact_id = "CONTACT-1"
    delay      = 0
    schedule   = "All"
  }
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
				ImportStateId:   "NULL-HEADER",
			},
		},
	})
}

// NodePing keeps a check's fields even on check types that ignore them: the
// split tool's variables.json check is an HTTP check that still carries the
// HTTPPARSE fields it was set up with. Before fields existed on the resource
// such a check imported "clean" with its fields invisible, and the first write
// would have dropped them. It has to import with them, keyed as NodePing
// stores them.
func TestAccCheckResource_importReadsFields(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	mock.AddCheck("HTTP-FIELDS", map[string]interface{}{
		"_id":      "HTTP-FIELDS",
		"type":     "HTTP",
		"label":    "acc-http-fields",
		"enable":   "active",
		"interval": 1,
		"parameters": map[string]interface{}{
			"target":    "https://example.com/variables.json",
			"threshold": 5,
			"sens":      2,
			"follow":    false,
			"fields": map[string]interface{}{
				"F7L814": map[string]interface{}{"name": "status", "min": 200, "max": 200},
				"FO7I39": map[string]interface{}{"name": "content.400.defaultOutput", "min": 1, "max": 99},
			},
		},
	})

	config := providerConfig(mock.URL()) + `
resource "nodeping_check" "imported" {
  type     = "HTTP"
  target   = "https://example.com/variables.json"
  label    = "acc-http-fields"
  enabled  = true
  interval = 1
  follow   = false

  fields = {
    F7L814 = { name = "status", min = 200, max = 200 }
    FO7I39 = { name = "content.400.defaultOutput", min = 1, max = 99 }
  }
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
				ImportStateId:   "HTTP-FIELDS",
			},
		},
	})
}

// A disabled check stores "queue": false, which the provider once failed to
// decode, so no disabled check could be imported. This one is shaped like the
// parent account's disabled HTTPPARSE checks, including the older notification
// form with a string delay and a named schedule.
func TestAccCheckResource_importReadsDisabledCheck(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	mock.AddCheck("DISABLED", map[string]interface{}{
		"_id":      "DISABLED",
		"type":     "HTTPPARSE",
		"label":    "acc-disabled",
		"enable":   "inactive",
		"interval": 1,
		"queue":    false,
		"dep":      false,
		"suspacct": false,
		"notifications": []interface{}{
			map[string]interface{}{"GROUP-1": map[string]interface{}{"delay": "0", "schedule": "All the time"}},
		},
		"parameters": map[string]interface{}{
			"target":    "http://example.com:4070/",
			"threshold": 5,
			"sens":      2,
			"fields": map[string]interface{}{
				"A": map[string]interface{}{"name": "rest_api", "min": 1, "max": 1},
			},
		},
	})

	config := providerConfig(mock.URL()) + `
resource "nodeping_check" "imported" {
  type     = "HTTPPARSE"
  target   = "http://example.com:4070/"
  label    = "acc-disabled"
  enabled  = false
  interval = 1

  fields = {
    A = { name = "rest_api", min = 1, max = 1 }
  }

  notifications {
    contact_id = "GROUP-1"
    delay      = 0
    schedule   = "All the time"
  }
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
				ImportStateId:   "DISABLED",
			},
		},
	})
}
