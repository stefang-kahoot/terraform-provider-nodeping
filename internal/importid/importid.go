// Package importid parses the ID handed to `terraform import`.
//
// All three NodePing resources used to accept a "customer_id:id" form for
// importing out of a SubAccount, and it was documented in the README and in
// every resource's docs. It never worked past the first request. The customer
// ID scoped the import's own GET and nothing else, so the Read that follows
// the import -- and every later Update and Delete -- addressed the base
// account, got a 404, and left Terraform proposing to recreate a resource
// that already existed somewhere else. There was nowhere to pin the SubAccount
// either: customer_id is Computed on all three resources, so a configuration
// cannot set it.
//
// A SubAccount is addressed by a provider instance instead. The provider's own
// customer_id is threaded through every request the client makes rather than
// just the first one, and Terraform routes an import through the provider
// configured for the target resource, so `provider = nodeping.subaccount` on
// the resource is all an import needs.
package importid

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// Parse returns the ID to import. It reports false, having appended an error
// to diags, when the ID carries the withdrawn SubAccount prefix.
//
// typeName is the resource type without the provider prefix, e.g. "check".
//
// NodePing IDs are of the form 201205050153W2Q4C-0J2HSIRF and never contain a
// colon, so a colon is unambiguously the old form rather than part of an ID.
func Parse(id, typeName string, diags *diag.Diagnostics) (string, bool) {
	if id == "" {
		// Left unchecked this reaches GET /<resources>/, the *list* endpoint,
		// which answers 200 with every resource and decodes into an empty
		// one -- so the import appears to succeed and writes a junk entry to
		// state.
		diags.AddError(
			"Invalid Import ID",
			fmt.Sprintf(
				"An import ID is required. Pass the %[1]s's NodePing ID:\n\n"+
					"  terraform import nodeping_%[1]s.example <%[1]s_id>",
				typeName,
			),
		)
		return "", false
	}

	customerID, bareID, found := strings.Cut(id, ":")
	if !found {
		return id, true
	}

	if customerID == "" {
		customerID = "SUBACCOUNT_ID"
	}
	if bareID == "" {
		bareID = "RESOURCE_ID"
	}

	diags.AddError(
		"Invalid Import ID",
		fmt.Sprintf(
			"Expected a plain %[1]s ID, got %[2]q.\n\n"+
				"The \"customer_id:id\" form is no longer accepted. It only ever scoped the "+
				"import's own request: the read that follows an import, and every later "+
				"update and delete, went to the base account instead, where the %[1]s does "+
				"not exist. Terraform then proposed recreating it in the wrong account.\n\n"+
				"Import a SubAccount %[1]s through a provider instance configured for that "+
				"account, which applies the customer ID to every request:\n\n"+
				"  provider \"nodeping\" {\n"+
				"    alias       = \"subaccount\"\n"+
				"    api_token   = var.nodeping_token\n"+
				"    customer_id = %[3]q\n"+
				"  }\n\n"+
				"  resource \"nodeping_%[1]s\" \"example\" {\n"+
				"    provider = nodeping.subaccount\n"+
				"    # ...\n"+
				"  }\n\n"+
				"Then import with the plain ID:\n\n"+
				"  terraform import nodeping_%[1]s.example %[4]s",
			typeName, id, customerID, bareID,
		),
	)
	return "", false
}
