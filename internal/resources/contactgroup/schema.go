package contactgroup

import (
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ContactGroupResourceModel struct {
	ID         types.String `tfsdk:"id"`
	CustomerID types.String `tfsdk:"customer_id"`
	Name       types.String `tfsdk:"name"`
	Members    types.List   `tfsdk:"members"`
}

func ContactGroupSchema() schema.Schema {
	return schema.Schema{
		Description: "Manages a NodePing contact group.",
		MarkdownDescription: `
Manages a NodePing contact group.

A contact group bundles contact addresses so a check can notify all of them
through a single notification entry, instead of listing every address on every
check.

~> **Members are address IDs, not contact IDs.** A NodePing contact can hold
several addresses, and a group references those addresses individually. Use the
` + "`id`" + ` of the ` + "`address`" + ` block, not the ` + "`id`" + ` of the
` + "`nodeping_contact`" + ` resource.

## Example Usage

` + "```hcl" + `
resource "nodeping_contact" "oncall" {
  name = "On-call"

  address {
    type    = "email"
    address = "oncall@example.com"
  }
}

resource "nodeping_contact" "backup" {
  name = "Backup"

  address {
    type    = "email"
    address = "backup@example.com"
  }
}

resource "nodeping_contactgroup" "escalation" {
  name = "Escalation"

  members = [
    nodeping_contact.oncall.address[0].id,
    nodeping_contact.backup.address[0].id,
  ]
}

resource "nodeping_check" "site" {
  type   = "HTTP"
  target = "https://example.com"
  label  = "Website"

  notifications {
    contact_id = nodeping_contactgroup.escalation.id
    delay      = 0
    schedule   = "All"
  }
}
` + "```" + `

## Import

Contact groups can be imported using the group ID:

` + "```shell" + `
terraform import nodeping_contactgroup.example 201205050153W2Q4C-G-1ZIYU
` + "```" + `

To import a contact group from a SubAccount, give the resource a provider
configured with the SubAccount's ` + "`customer_id`" + ` and import it by its
plain ID, as above. That provider sends the customer ID with every request,
including the reads, updates and deletes after the import:

` + "```hcl" + `
provider "nodeping" {
  alias       = "subaccount"
  api_token   = var.nodeping_token
  customer_id = "SUBACCOUNT_ID"
}

resource "nodeping_contactgroup" "example" {
  provider = nodeping.subaccount
  # ...
}
` + "```" + `
`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier of the contact group.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"customer_id": schema.StringAttribute{
				Description: "The customer ID (account ID) that owns this contact group.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The display name of the contact group.",
				Optional:    true,
			},
			"members": schema.ListAttribute{
				Description:         "Contact address IDs belonging to this group. These are address IDs, not contact IDs.",
				MarkdownDescription: "Contact **address** IDs belonging to this group. These are the IDs of individual `address` blocks, not `nodeping_contact` IDs.",
				Optional:            true,
				ElementType:         types.StringType,
			},
		},
	}
}
