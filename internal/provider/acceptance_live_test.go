package provider_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
)

// These tests run against the real NodePing API. Every other acceptance test
// runs against the mock, and stays green for as long as the mock answers the
// way NodePing does (docs/nodeping-api-quirks.md lists what it copies). These
// are what notice when it no longer does: each creates the smallest object of
// its type, updates it in place, imports it and destroys it.
//
// They write to the account, so they are opt-in. They run only with TF_ACC=1
// and NODEPING_API_TOKEN_ACCEPTANCE_TESTS set to the token of a dedicated test
// SubAccount, whose customer ID NODEPING_ACCEPTANCE_TESTS_CUSTOMER_ID gives.
// Without the token they skip, so `make test-acceptance` and CI pass without
// it; with the token but no customer ID they fail. CONTRIBUTING.md says how to
// run them.
//
// Before writing anything, each test refuses an account that holds another
// account's objects (checkLiveAccount), and its first step checks that what
// it created is in the account too (liveAccount.owns). It changes nothing it
// did not create. Its objects are named tf-acc-live-<random>, so that a
// leftover of a failed run is easy to find and delete by hand.

const (
	liveTokenEnv      = "NODEPING_API_TOKEN_ACCEPTANCE_TESTS" //nolint:gosec // G101: the name of a variable, not a token
	liveCustomerIDEnv = "NODEPING_ACCEPTANCE_TESTS_CUSTOMER_ID"
)

// liveProviderConfig configures nothing: the provider reads the token from the
// environment, which useOnlyLiveToken has set, so it never appears in a
// configuration.
const liveProviderConfig = `
provider "nodeping" {}
`

// liveAccount is the account the tests write to.
type liveAccount struct {
	token      string
	customerID string
}

// newLiveAccount returns the account of NODEPING_API_TOKEN_ACCEPTANCE_TESTS,
// once checkLiveAccount has passed it, and leaves the provider nothing else to
// reach. It skips the test without TF_ACC or the token, and fails it without a
// customer ID or when the account is not the one the customer ID names.
func newLiveAccount(t *testing.T) liveAccount {
	t.Helper()

	// resource.Test skips without TF_ACC too, but only after the guard below
	// would have listed the account.
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skipf("Acceptance tests skipped unless env '%s' set", resource.EnvTfAcc)
	}
	token := os.Getenv(liveTokenEnv)
	if token == "" {
		t.Skipf("Tests against the real NodePing API skipped unless env '%s' and '%s' set", liveTokenEnv, liveCustomerIDEnv)
	}
	customerID := strings.TrimSpace(os.Getenv(liveCustomerIDEnv))
	if customerID == "" {
		t.Fatalf("%s is set but %s is not: set it to the customer ID of the account the token belongs to", liveTokenEnv, liveCustomerIDEnv)
	}

	a := liveAccount{token: token, customerID: customerID}
	useOnlyLiveToken(t, token)

	if err := checkLiveAccount(context.Background(), a.apiClient(), customerID); err != nil {
		t.Fatalf("Refusing to write to the account of %s: %s", liveTokenEnv, a.redact(err))
	}
	return a
}

// useOnlyLiveToken leaves the provider, which runs inside this test, the
// acceptance token and nothing else. It reads its configuration from the
// environment, where a developer's own NODEPING_API_TOKEN, NODEPING_API_KEY,
// NODEPING_CUSTOMER_ID or NODEPING_API_URL would send it to another account
// or another API. They are back when the test ends.
func useOnlyLiveToken(t *testing.T, token string) {
	t.Helper()

	t.Setenv("NODEPING_API_TOKEN", token)
	for _, name := range []string{"NODEPING_API_KEY", "NODEPING_CUSTOMER_ID", "NODEPING_API_URL"} {
		// The provider takes "" for unset, but unset is what it is meant
		// to see. t.Setenv restores the developer's value afterwards.
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unsetting %s: %v", name, err)
		}
	}
}

// apiClient is a client for the account, configured as the provider is: the
// default API URL, no customer ID and the default retries.
func (a liveAccount) apiClient() *client.Client {
	return client.NewClient(client.ClientConfig{
		APIToken:     a.token,
		MaxRetries:   client.DefaultMaxRetries,
		RetryMinWait: client.DefaultRetryMinWait,
		RetryMaxWait: client.DefaultRetryMaxWait,
	})
}

// redact returns err's message without the token, should NodePing ever
// repeat it.
func (a liveAccount) redact(err error) string {
	return strings.ReplaceAll(err.Error(), a.token, "<token>")
}

// owns checks that the object at address has an ID in the account. An empty
// account passes checkLiveAccount whatever account it is, so this is what
// tells that the token opens the account the customer ID names. If it fails,
// the test still destroys what it created.
func (a liveAccount) owns(address string) resource.TestCheckFunc {
	prefix := a.customerID + "-"
	return resource.TestCheckResourceAttrWith(address, "id", func(id string) error {
		if !strings.HasPrefix(id, prefix) {
			return fmt.Errorf("%s has ID %q, which is not in account %s", address, id, a.customerID)
		}
		return nil
	})
}

// checkDestroyed confirms through NodePing that every check, contact and
// contact group in s is gone. NodePing never answers 404 for an ID it does
// not have, so it takes the client's word for it: a NotFoundError, which the
// client gives only once the list of the type lacks the ID as well.
func (a liveAccount) checkDestroyed(s *terraform.State) error {
	ctx := context.Background()
	c := a.apiClient()

	var errs []error
	for address, rs := range s.RootModule().Resources {
		if rs.Primary == nil {
			continue
		}
		id := rs.Primary.ID

		var err error
		switch rs.Type {
		case "nodeping_check":
			_, err = c.GetCheck(ctx, id)
		case "nodeping_contact":
			_, err = c.GetContact(ctx, id)
		case "nodeping_contactgroup":
			_, err = c.GetContactGroup(ctx, id)
		default:
			continue
		}

		if err == nil {
			errs = append(errs, fmt.Errorf("%s %s is still in NodePing after the destroy", address, id))
		} else if _, gone := errors.AsType[*client.NotFoundError](err); !gone {
			errs = append(errs, fmt.Errorf("could not tell whether %s %s is gone: %s", address, id, a.redact(err)))
		}
	}
	return errors.Join(errs...)
}

// liveName names an object a test creates: tf-acc-live- and 8 random
// letters and digits.
func liveName() string {
	return "tf-acc-live-" + strings.ToLower(rand.Text()[:8])
}

func TestAccLiveContactResource_lifecycle(t *testing.T) {
	acc := newLiveAccount(t)
	name := liveName()

	// RFC 2606 reserves example.com, so nothing is ever delivered.
	config := func(label string, suppressUp bool) string {
		return liveProviderConfig + fmt.Sprintf(`
resource "nodeping_contact" "live" {
  name = %q

  address {
    type        = "email"
    address     = %q
    suppress_up = %t
  }
}
`, label, name+"@example.com", suppressUp)
	}

	// The update has to change the address under the ID it was created
	// with: a contact group names its addresses by ID.
	var addressID string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             acc.checkDestroyed,
		Steps: []resource.TestStep{
			{
				Config: config(name, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					acc.owns("nodeping_contact.live"),
					resource.TestCheckResourceAttr("nodeping_contact.live", "name", name),
					resource.TestCheckResourceAttr("nodeping_contact.live", "address.#", "1"),
					resource.TestCheckResourceAttr("nodeping_contact.live", "address.0.suppress_up", "false"),
					resource.TestCheckResourceAttrWith("nodeping_contact.live", "address.0.id", func(id string) error {
						addressID = id
						return nil
					}),
				),
			},
			{
				Config: config(name+"-updated", true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("nodeping_contact.live", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_contact.live", "name", name+"-updated"),
					resource.TestCheckResourceAttr("nodeping_contact.live", "address.#", "1"),
					resource.TestCheckResourceAttr("nodeping_contact.live", "address.0.suppress_up", "true"),
					resource.TestCheckResourceAttrWith("nodeping_contact.live", "address.0.id", func(id string) error {
						if id != addressID {
							return fmt.Errorf("the address has ID %q after the update, %q before", id, addressID)
						}
						return nil
					}),
				),
			},
			{
				Config:            config(name+"-updated", true),
				ResourceName:      "nodeping_contact.live",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// A contact group holds contact address IDs, so the test creates a contact
// for it to hold.
func TestAccLiveContactGroupResource_lifecycle(t *testing.T) {
	acc := newLiveAccount(t)
	name := liveName()

	config := func(label string) string {
		return liveProviderConfig + fmt.Sprintf(`
resource "nodeping_contact" "member" {
  name = %q

  address {
    type    = "email"
    address = %q
  }
}

resource "nodeping_contactgroup" "live" {
  name    = %q
  members = [nodeping_contact.member.address[0].id]
}
`, name+"-member", name+"@example.com", label)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             acc.checkDestroyed,
		Steps: []resource.TestStep{
			{
				Config: config(name),
				Check: resource.ComposeAggregateTestCheckFunc(
					acc.owns("nodeping_contact.member"),
					acc.owns("nodeping_contactgroup.live"),
					resource.TestCheckResourceAttr("nodeping_contactgroup.live", "name", name),
					resource.TestCheckResourceAttr("nodeping_contactgroup.live", "members.#", "1"),
					resource.TestCheckResourceAttrPair(
						"nodeping_contactgroup.live", "members.0",
						"nodeping_contact.member", "address.0.id",
					),
				),
			},
			{
				Config: config(name + "-updated"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("nodeping_contactgroup.live", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("nodeping_contact.member", plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_contactgroup.live", "name", name+"-updated"),
					resource.TestCheckResourceAttr("nodeping_contactgroup.live", "members.#", "1"),
				),
			},
			{
				Config:            config(name + "-updated"),
				ResourceName:      "nodeping_contactgroup.live",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// The check is disabled, so it never runs and never alerts, and it notifies
// nobody.
func TestAccLiveCheckResource_lifecycle(t *testing.T) {
	acc := newLiveAccount(t)
	name := liveName()

	config := func(label string, interval int) string {
		return liveProviderConfig + fmt.Sprintf(`
resource "nodeping_check" "live" {
  type     = "HTTP"
  target   = "https://example.com/"
  label    = %q
  enabled  = false
  interval = %d
}
`, label, interval)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             acc.checkDestroyed,
		Steps: []resource.TestStep{
			{
				Config: config(name, 15),
				Check: resource.ComposeAggregateTestCheckFunc(
					acc.owns("nodeping_check.live"),
					resource.TestCheckResourceAttr("nodeping_check.live", "label", name),
					resource.TestCheckResourceAttr("nodeping_check.live", "enabled", "false"),
					resource.TestCheckResourceAttr("nodeping_check.live", "interval", "15"),
					resource.TestCheckNoResourceAttr("nodeping_check.live", "notifications.#"),
				),
			},
			{
				Config: config(name+"-updated", 30),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("nodeping_check.live", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nodeping_check.live", "label", name+"-updated"),
					resource.TestCheckResourceAttr("nodeping_check.live", "enabled", "false"),
					resource.TestCheckResourceAttr("nodeping_check.live", "interval", "30"),
				),
			},
			// An update leaves the check's modified in state as planned, the
			// value from before the update, until the next refresh reads
			// NodePing's (see CheckResource.updatedModel). The import reads
			// NodePing's, so it is compared with a refreshed state.
			{RefreshState: true},
			{
				Config:            config(name+"-updated", 30),
				ResourceName:      "nodeping_check.live",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
