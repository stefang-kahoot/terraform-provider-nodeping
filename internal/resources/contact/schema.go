package contact

import (
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/notblank"
)

type ContactResourceModel struct {
	ID         types.String   `tfsdk:"id"`
	CustomerID types.String   `tfsdk:"customer_id"`
	Name       types.String   `tfsdk:"name"`
	CustRole   types.String   `tfsdk:"custrole"`
	Addresses  []AddressModel `tfsdk:"address"`
}

type AddressModel struct {
	ID            types.String `tfsdk:"id"`
	Type          types.String `tfsdk:"type"`
	Address       types.String `tfsdk:"address"`
	SuppressUp    types.Bool   `tfsdk:"suppress_up"`
	SuppressDown  types.Bool   `tfsdk:"suppress_down"`
	SuppressFirst types.Bool   `tfsdk:"suppress_first"`
	SuppressDiag  types.Bool   `tfsdk:"suppress_diag"`
	SuppressAll   types.Bool   `tfsdk:"suppress_all"`
	Mute          types.Bool   `tfsdk:"mute"`
	Action        types.String `tfsdk:"action"`
	Headers       types.Map    `tfsdk:"headers"`
	QueryStrings  types.Map    `tfsdk:"querystrings"`
	Data          types.String `tfsdk:"data"`
	Priority      types.Int64  `tfsdk:"priority"`
}

func ContactSchema() schema.Schema {
	return schema.Schema{
		Description: "Manages a NodePing contact.",
		MarkdownDescription: `
Manages a NodePing contact.

Contacts are used to receive notifications when checks fail or recover. Each contact can have multiple addresses (email, SMS, webhook, etc.).

## Example Usage

` + "```hcl" + `
resource "nodeping_contact" "example" {
  name     = "John Doe"
  custrole = "notify"

  address {
    type    = "email"
    address = "john@example.com"
  }

  address {
    type    = "webhook"
    address = "https://hooks.example.com/notify"
    action  = "post"
    headers = {
      "Content-Type" = "application/json"
    }
  }
}
` + "```" + `

### Webhook with JSON Body (MS Teams, Slack, etc.)

When using webhooks with JSON bodies that contain newlines or special characters,
use heredoc syntax with ` + "`chomp()`" + ` to avoid trailing newline issues:

` + "```hcl" + `
resource "nodeping_contact" "msteams" {
  name     = "MS Teams Notifications"
  custrole = "notify"

  address {
    type    = "webhook"
    address = "https://outlook.office.com/webhook/..."
    action  = "post"
    headers = {
      "content-type" = "application/json"
    }
    querystrings = {
      "key" = "1"
    }
    # Use chomp() with heredoc to match exact API format
    data = chomp(<<-EOT
{"@context": "https://schema.org/extensions","@type": "MessageCard","title": "Host {label} : {type} is {event}","text": "Host {label} : {type} is {event} {if downtime}after {downtime} {if downminutes}minutes{else}minute{/if} of downtime {/if}as of {checktime}: 
 {message}"}
EOT
)
  }
}
` + "```" + `

**Note:** The ` + "`data`" + ` field stores the exact string as provided. When importing
existing contacts, the API may return JSON with specific formatting (spaces after
colons, embedded newlines). Use heredoc with ` + "`chomp()`" + ` to match the exact format
and avoid unnecessary plan diffs.


## Import

Contacts can be imported using the contact ID:

` + "```shell" + `
terraform import nodeping_contact.example 201205050153W2Q4C-BKPGH
` + "```" + `

A contact that NodePing holds without a name, created outside Terraform,
imports with an empty name. The plan after the import then updates it in
place to set the configuration's ` + "`name`" + `.

To import a contact from a SubAccount, give the resource a provider
configured with the SubAccount's ` + "`customer_id`" + ` and import it by its
plain ID, as above. That provider sends the customer ID with every request,
including the reads, updates and deletes after the import:

` + "```hcl" + `
provider "nodeping" {
  alias       = "subaccount"
  api_token   = var.nodeping_token
  customer_id = "SUBACCOUNT_ID"
}

resource "nodeping_contact" "example" {
  provider = nodeping.subaccount
  # ...
}
` + "```" + `

An import reads a contact's addresses in the order of their IDs, since
NodePing keeps them in no order. If the configuration lists the ` + "`address`" + `
blocks in another order, the plan after the import shows a one-time in-place
update: Terraform compares the blocks by position. Applying it sends nothing
to NodePing when the order is the only difference; the configuration's order
is written to state, each address under its own ID. The same goes for blocks
reordered later. If anything else changes as well, the update is sent as
usual.
`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier of the contact.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"customer_id": schema.StringAttribute{
				Description: "The customer ID (account ID) that owns this contact.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			// Required and not blank: NodePing keeps a contact's name when an
			// update leaves it out or sends "" or null, so it cannot be
			// removed. See the notblank package.
			"name": schema.StringAttribute{
				Description: "The name of the contact. Used as a label. It cannot be empty or only whitespace: NodePing can change a contact's name, but not remove it.",
				Required:    true,
				Validators: []validator.String{
					notblank.Validator(),
				},
			},
			"custrole": schema.StringAttribute{
				Description: "The permission role for this contact. Valid values: 'edit', 'view', 'notify'. Defaults to 'notify'.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("notify"),
				Validators: []validator.String{
					stringvalidator.OneOf("edit", "view", "notify"),
				},
			},
		},
		Blocks: map[string]schema.Block{
			"address": schema.ListNestedBlock{
				Description: "Contact addresses for receiving notifications.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						// No UseStateForUnknown: it carries the ID over by list
						// position. ContactResource.ModifyPlan plans it instead.
						"id": schema.StringAttribute{
							Description: "The unique identifier of the address.",
							Computed:    true,
						},
						"type": schema.StringAttribute{
							Description: "The type of address. Valid values: 'email', 'sms', 'webhook', 'slack', 'hipchat', 'pushover', 'pagerduty', 'voice'.",
							Required:    true,
							Validators: []validator.String{
								stringvalidator.OneOf("email", "sms", "webhook", "slack", "hipchat", "pushover", "pagerduty", "voice"),
							},
						},
						"address": schema.StringAttribute{
							Description: "The address value (email, phone number, webhook URL, etc.).",
							Required:    true,
							Sensitive:   true,
						},
						"suppress_up": schema.BoolAttribute{
							Description: "Suppress 'up' notifications to this address.",
							Optional:    true,
							Computed:    true,
							Default:     booldefault.StaticBool(false),
						},
						"suppress_down": schema.BoolAttribute{
							Description: "Suppress 'down' notifications to this address.",
							Optional:    true,
							Computed:    true,
							Default:     booldefault.StaticBool(false),
						},
						"suppress_first": schema.BoolAttribute{
							Description: "Suppress 'first result' notifications to this address.",
							Optional:    true,
							Computed:    true,
							Default:     booldefault.StaticBool(false),
						},
						"suppress_diag": schema.BoolAttribute{
							Description: "Suppress diagnostic notifications to this address.",
							Optional:    true,
							Computed:    true,
							Default:     booldefault.StaticBool(false),
						},
						"suppress_all": schema.BoolAttribute{
							Description: "Suppress all notifications to this address.",
							Optional:    true,
							Computed:    true,
							Default:     booldefault.StaticBool(false),
						},
						"mute": schema.BoolAttribute{
							Description: "Mute all notifications to this address. With the provider's ignore_mute set and mute left out, the address's mute is left to NodePing.",
							Optional:    true,
							Computed:    true,
							Default:     booldefault.StaticBool(false),
						},
						"action": schema.StringAttribute{
							Description: "HTTP method for webhook addresses. Valid values: 'get', 'put', 'post', 'head', 'delete'. " +
								"NodePing's docs say it uses 'get' when none is set; NodePing stores none. " +
								"NodePing cannot clear it, so removing it from an existing address fails the plan.",
							Optional: true,
							Validators: []validator.String{
								stringvalidator.OneOf("get", "put", "post", "head", "delete"),
							},
						},
						"headers": schema.MapAttribute{
							Description: "HTTP headers for webhook addresses.",
							Optional:    true,
							ElementType: types.StringType,
						},
						"querystrings": schema.MapAttribute{
							Description: "Query string parameters for webhook addresses.",
							Optional:    true,
							ElementType: types.StringType,
						},
						"data": schema.StringAttribute{
							Description: "Request body for webhook addresses (POST/PUT).",
							Optional:    true,
							PlanModifiers: []planmodifier.String{
								JSONSemanticEqual(),
							},
						},
						"priority": schema.Int64Attribute{
							Description: "Priority for Pushover addresses, from -2 to 2. Removing it clears it. " +
								"NodePing's docs say it uses 2 when none is set, but NodePing stores none.",
							Optional: true,
						},
					},
				},
			},
		},
	}
}
