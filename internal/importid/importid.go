// Package importid parses the ID handed to an import of a check, contact or
// contact group.
//
// All three used to accept "customer_id:id" to import from a SubAccount, and
// the README and their docs advertised it. The customer ID reached only the
// import's own read. The read Terraform makes right after an import, and
// every later update and delete, went to the provider's own account, where
// the object does not exist: the import failed, or Terraform planned to
// create the object again. A configuration could not pin the account either,
// as customer_id is computed on all three resources.
//
// A SubAccount is addressed by a provider instance instead. The provider's
// customer_id goes with every request its client makes, and Terraform imports
// a resource through the provider the resource names, so
// `provider = nodeping.subaccount` on the resource is all an import needs.
package importid

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// Parse returns the ID to import. It reports false, having added an error to
// diags, when the ID carries the withdrawn SubAccount prefix.
//
// typeName is the resource type without the provider prefix, e.g. "check".
//
// NodePing IDs are of the form 201205050153W2Q4C-0J2HSIRF and never contain a
// colon, so a colon is the old form rather than part of an ID.
func Parse(id, typeName string, diags *diag.Diagnostics) (string, bool) {
	parts := strings.Split(id, ":")
	if len(parts) == 1 {
		return id, true
	}

	// Show the parts in the example where they are unambiguous, and
	// placeholders where they are not.
	customerID, bareID := "SUBACCOUNT_ID", "RESOURCE_ID"
	if len(parts) == 2 {
		if parts[0] != "" {
			customerID = parts[0]
		}
		if parts[1] != "" {
			bareID = parts[1]
		}
	}

	diags.AddError(
		"Invalid Import ID",
		fmt.Sprintf(
			"Expected a plain %[1]s ID, got %[2]q.\n\n"+
				"The \"customer_id:id\" form is no longer accepted. The customer ID reached "+
				"only the import's own request: the read that follows an import, and every "+
				"later update and delete, went to the provider's own account instead, where "+
				"the %[1]s does not exist.\n\n"+
				"Import a SubAccount's %[1]s through a provider instance configured for that "+
				"account, which sends its customer ID with every request:\n\n"+
				"  provider \"nodeping\" {\n"+
				"    alias       = \"subaccount\"\n"+
				"    api_token   = var.nodeping_token\n"+
				"    customer_id = %[3]q\n"+
				"  }\n\n"+
				"  resource \"nodeping_%[1]s\" \"example\" {\n"+
				"    provider = nodeping.subaccount\n"+
				"    # ...\n"+
				"  }\n\n"+
				"Then import it by its plain ID, in an import block or with:\n\n"+
				"  terraform import nodeping_%[1]s.example %[4]s",
			typeName, id, customerID, bareID,
		),
	)
	return "", false
}
