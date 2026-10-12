package provider_test

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// A contact and a contact group require a name that is not blank (#24, #25).
// NodePing cannot clear either: an update that leaves `name` out, or sends ""
// or null, keeps it. While the name was optional, an unnamed contact planned
// an update on every run, and removing a name failed the apply for a group
// and planned the removal again on every run for a contact. A group update
// ignores " " as well; see the notblank package.

var (
	missingNameError = regexp.MustCompile(wrapped(`The argument "name" is required, but no definition was found.`))
	emptyNameError   = regexp.MustCompile(wrapped(`Attribute name value must not be empty or only whitespace, got: ""`))
	blankNameError   = regexp.MustCompile(wrapped(`Attribute name value must not be empty or only whitespace, got: " "`))
)

func TestAccContactResource_nameIsRequired(t *testing.T) {
	testNameIsRequired(t, "nodeping_contact", `
  address {
    type    = "email"
    address = "named@example.com"
  }
`)
}

func TestAccContactGroupResource_nameIsRequired(t *testing.T) {
	testNameIsRequired(t, "nodeping_contactgroup", "")
}

// testNameIsRequired plans a resource of typeName without a name, with
// name = "" and with name = " ", which all fail; creates it with a name and
// plans clean; then removes, empties and blanks the name, which fail again
// and leave it as it was. Nothing but the create and the final destroy is
// written.
func testNameIsRequired(t *testing.T, typeName, body string) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	address := typeName + ".test"
	config := func(name string) string {
		return providerConfig(mock.URL()) + fmt.Sprintf(`
resource %q "test" {
%s
%s}
`, typeName, name, body)
	}
	named := config(`  name = "acc-named"`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config(""), ExpectError: missingNameError},
			{Config: config(`  name = ""`), ExpectError: emptyNameError},
			{Config: config(`  name = " "`), ExpectError: blankNameError},
			{
				Config: named,
				Check:  resource.TestCheckResourceAttr(address, "name", "acc-named"),
			},
			{Config: named, PlanOnly: true},
			// The object exists now, named.
			{Config: config(""), ExpectError: missingNameError},
			{Config: config(`  name = ""`), ExpectError: emptyNameError},
			{Config: config(`  name = " "`), ExpectError: blankNameError},
			// It is as it was, and the destroy at the end has a valid
			// configuration to run with.
			{Config: named, PlanOnly: true},
		},
	})

	for _, r := range mock.Requests() {
		if r.Method == http.MethodPut {
			t.Errorf("sent %s %s, want no update", r.Method, r.Path)
		}
	}
}

// A contact created without a name in NodePing holds no name key. It imports
// with an empty name, and the configuration's name is then an update of the
// name alone, which applies and plans clean after.
func TestAccContactResource_importUnnamed(t *testing.T) {
	mock := testutil.NewMockNodePingServer()
	t.Cleanup(mock.Close)

	mock.AddContact("UNNAMED", map[string]interface{}{
		"_id":         "UNNAMED",
		"type":        "contact",
		"customer_id": "MOCK-CUSTOMER",
		"custrole":    "notify",
		"addresses": map[string]interface{}{
			"UNNAMED-ADDR": map[string]interface{}{"type": "email", "address": "unnamed@example.com"},
		},
	})

	config := providerConfig(mock.URL()) + `
resource "nodeping_contact" "test" {
  name = "acc-named"

  address {
    type    = "email"
    address = "unnamed@example.com"
  }
}
`

	testImportUnnamed(t, config, "nodeping_contact.test", "UNNAMED",
		func() map[string]interface{} {
			contact, _ := mock.GetContact("UNNAMED")
			return contact
		},
		resource.TestCheckResourceAttr("nodeping_contact.test", "address.0.id", "UNNAMED-ADDR"),
	)
}

// A contact group created without a name holds no name key; one created with
// "" or null holds that (probed 2026-10-12). Each imports with an empty name,
// and the configuration's name is then an update of the name alone.
func TestAccContactGroupResource_importUnnamed(t *testing.T) {
	seeds := []struct {
		desc string
		name func(group map[string]interface{})
	}{
		{"no name key", func(map[string]interface{}) {}},
		{"empty name", func(group map[string]interface{}) { group["name"] = "" }},
		{"null name", func(group map[string]interface{}) { group["name"] = nil }},
	}

	for _, seed := range seeds {
		t.Run(seed.desc, func(t *testing.T) {
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)

			const id = "MOCK-CUSTOMER-G-UNNAMED"
			group := map[string]interface{}{
				"_id":         id,
				"type":        "group",
				"customer_id": "MOCK-CUSTOMER",
				"members":     []interface{}{"MEMBER-ADDR"},
			}
			seed.name(group)
			mock.AddContactGroup(id, group)

			config := providerConfig(mock.URL()) + `
resource "nodeping_contactgroup" "test" {
  name    = "acc-named"
  members = ["MEMBER-ADDR"]
}
`

			testImportUnnamed(t, config, "nodeping_contactgroup.test", id,
				func() map[string]interface{} {
					group, _ := mock.GetContactGroup(id)
					return group
				},
				resource.TestCheckResourceAttr("nodeping_contactgroup.test", "members.0", "MEMBER-ADDR"),
			)
		})
	}
}

// testImportUnnamed imports the object id, which NodePing holds without a
// name, into address, whose configuration names it "acc-named". By import
// block and after terraform import alike, the plan updates the name from ""
// and changes nothing else. The update applies, NodePing then holds the name
// (stored returns the object as the mock holds it), and the plan after is
// clean.
func testImportUnnamed(t *testing.T, config, address, id string, stored func() map[string]interface{}, check resource.TestCheckFunc) {
	nameOnly := []plancheck.PlanCheck{
		plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
		expectOnlyNameChange{address: address, from: "", to: "acc-named"},
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       address,
				ImportState:        true,
				ImportStateKind:    resource.ImportBlockWithID,
				ImportStateId:      id,
				ExpectNonEmptyPlan: true,
				ImportPlanChecks:   resource.ImportPlanChecks{PreApply: nameOnly},
			},
			{
				Config:             config,
				ResourceName:       address,
				ImportState:        true,
				ImportStateId:      id,
				ImportStatePersist: true,
			},
			{
				Config:           config,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: nameOnly},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "id", id),
					resource.TestCheckResourceAttr(address, "name", "acc-named"),
					check,
					func(*terraform.State) error {
						if got := stored()["name"]; got != "acc-named" {
							return fmt.Errorf("NodePing holds the name %#v, want %q", got, "acc-named")
						}
						return nil
					},
				),
			},
			{Config: config, PlanOnly: true},
		},
	})
}

// expectOnlyNameChange checks that the plan changes the resource's name from
// one value to another, and no other attribute.
type expectOnlyNameChange struct{ address, from, to string }

func (e expectOnlyNameChange) CheckPlan(_ context.Context, req plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
	for _, rc := range req.Plan.ResourceChanges {
		if rc.Address != e.address {
			continue
		}
		before, _ := rc.Change.Before.(map[string]interface{})
		after, _ := rc.Change.After.(map[string]interface{})
		if before["name"] != e.from || after["name"] != e.to {
			resp.Error = fmt.Errorf("%s plans the name %#v -> %#v, want %q -> %q",
				e.address, before["name"], after["name"], e.from, e.to)
			return
		}

		// An attribute planned unknown is missing from after, so it shows
		// here as a change too.
		var changed []string
		for _, values := range []map[string]interface{}{before, after} {
			for k := range values {
				if k != "name" && !reflect.DeepEqual(before[k], after[k]) && !slices.Contains(changed, k) {
					changed = append(changed, k)
				}
			}
		}
		if len(changed) > 0 {
			sort.Strings(changed)
			resp.Error = fmt.Errorf("%s plans changes to %v besides the name", e.address, changed)
		}
		return
	}
	resp.Error = fmt.Errorf("%s is not in the plan", e.address)
}
