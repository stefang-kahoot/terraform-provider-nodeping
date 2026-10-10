package check

import (
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/datasources/checkattr"
)

var ValidCheckTypes = []string{
	"AGENT", "AUDIO", "CLUSTER", "DOHDOT", "DNS", "FTP", "HTTP", "HTTPCONTENT",
	"HTTPPARSE", "HTTPADV", "IMAP4", "MONGODB", "MTR", "MYSQL", "NTP", "PGSQL",
	"PING", "POP3", "PORT", "PUSH", "RBL", "RDAP", "RDP", "REDIS", "SIP", "SMTP",
	"SNMP", "SPEC10DNS", "SPEC10RDDS", "SSH", "SSL", "WEBSOCKET", "WHOIS",
}

type CheckResourceModel struct {
	ID             types.String        `tfsdk:"id"`
	CustomerID     types.String        `tfsdk:"customer_id"`
	Type           types.String        `tfsdk:"type"`
	Target         types.String        `tfsdk:"target"`
	Label          types.String        `tfsdk:"label"`
	Enabled        types.Bool          `tfsdk:"enabled"`
	Public         types.Bool          `tfsdk:"public"`
	Interval       types.Float64       `tfsdk:"interval"`
	Threshold      types.Int64         `tfsdk:"threshold"`
	Sens           types.Int64         `tfsdk:"sens"`
	Mute           types.Bool          `tfsdk:"mute"`
	Dep            types.String        `tfsdk:"dep"`
	Description    types.String        `tfsdk:"description"`
	RunLocations   types.List          `tfsdk:"runlocations"`
	HomeLoc        types.String        `tfsdk:"homeloc"`
	AutoDiag       types.Bool          `tfsdk:"autodiag"`
	Tags           types.List          `tfsdk:"tags"`
	TagsAll        types.List          `tfsdk:"tags_all"`
	Notifications  []NotificationModel `tfsdk:"notifications"`
	State          types.Int64         `tfsdk:"state"`
	Created        types.Int64         `tfsdk:"created"`
	Modified       types.Int64         `tfsdk:"modified"`
	ContentString  types.String        `tfsdk:"contentstring"`
	Regex          types.Bool          `tfsdk:"regex"`
	Invert         types.Bool          `tfsdk:"invert"`
	Follow         types.Bool          `tfsdk:"follow"`
	Method         types.String        `tfsdk:"method"`
	StatusCode     types.Int64         `tfsdk:"statuscode"`
	SendHeaders    types.Map           `tfsdk:"sendheaders"`
	ReceiveHeaders types.Map           `tfsdk:"receiveheaders"`
	PostData       types.String        `tfsdk:"postdata"`
	Fields         types.Map           `tfsdk:"fields"`
	Port           types.Int64         `tfsdk:"port"`
	Username       types.String        `tfsdk:"username"`
	Password       types.String        `tfsdk:"password"`
	Secure         types.String        `tfsdk:"secure"`
	Verify         types.Bool          `tfsdk:"verify"`
	IPv6           types.Bool          `tfsdk:"ipv6"`
	DNSType        types.String        `tfsdk:"dnstype"`
	DNSToResolve   types.String        `tfsdk:"dnstoresolve"`
	DNSSection     types.String        `tfsdk:"dnssection"`
	DNSRD          types.Bool          `tfsdk:"dnsrd"`
	Transport      types.String        `tfsdk:"transport"`
	WarningDays    types.Int64         `tfsdk:"warningdays"`
	ServerName     types.String        `tfsdk:"servername"`
	Email          types.String        `tfsdk:"email"`
	Database       types.String        `tfsdk:"database"`
	Query          types.String        `tfsdk:"query"`
	Namespace      types.String        `tfsdk:"namespace"`
	SSHKey         types.String        `tfsdk:"sshkey"`
	ClientCert     types.String        `tfsdk:"clientcert"`
	SNMPv          types.String        `tfsdk:"snmpv"`
	SNMPCom        types.String        `tfsdk:"snmpcom"`
	VerifyVolume   types.Bool          `tfsdk:"verifyvolume"`
	VolumeMin      types.Int64         `tfsdk:"volumemin"`
}

type NotificationModel struct {
	ContactID types.String `tfsdk:"contact_id"`
	Delay     types.Int64  `tfsdk:"delay"`
	Schedule  types.String `tfsdk:"schedule"`
}

func CheckSchema() schema.Schema {
	return schema.Schema{
		Description: "Manages a NodePing check.",
		MarkdownDescription: `
Manages a NodePing check.

Checks monitor your services and send notifications when they fail or recover.

## Example Usage

### HTTP Check

` + "```hcl" + `
resource "nodeping_check" "http" {
  type    = "HTTP"
  target  = "https://example.com"
  label   = "Example Website"
  enabled = true

  interval  = 5
  threshold = 10
  sens      = 2

  runlocations = ["nam"]
}
` + "```" + `

### DNS Check

` + "```hcl" + `
resource "nodeping_check" "dns" {
  type          = "DNS"
  target        = "8.8.8.8"
  label         = "DNS Check"
  enabled       = true

  dnstype       = "A"
  dnstoresolve  = "example.com"
  contentstring = "93.184.216.34"
}
` + "```" + `

### SSL Certificate Check

` + "```hcl" + `
resource "nodeping_check" "ssl" {
  type        = "SSL"
  target      = "example.com"
  label       = "SSL Certificate"
  enabled     = true

  warningdays = 30
  servername  = "example.com"
}
` + "```" + `

### Audio Stream Check

` + "```hcl" + `
resource "nodeping_check" "audio" {
  type    = "AUDIO"
  target  = "https://example.com/stream.mp3"
  label   = "Audio Stream"
  enabled = true

  verifyvolume = true
  volumemin    = -40
}
` + "```" + `

## Import

Checks can be imported using the check ID:

` + "```shell" + `
terraform import nodeping_check.example 201205050153W2Q4C-0J2HSIRF
` + "```" + `

To import a check from a SubAccount, give the resource a provider
configured with the SubAccount's ` + "`customer_id`" + ` and import it by its
plain ID, as above. That provider sends the customer ID with every request,
including the reads, updates and deletes after the import:

` + "```hcl" + `
provider "nodeping" {
  alias       = "subaccount"
  api_token   = var.nodeping_token
  customer_id = "SUBACCOUNT_ID"
}

resource "nodeping_check" "example" {
  provider = nodeping.subaccount
  # ...
}
` + "```" + `
`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier of the check.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"customer_id": schema.StringAttribute{
				Description: "The customer ID (account ID) that owns this check.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"type": schema.StringAttribute{
				Description: "The type of check. See NodePing documentation for available types.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.OneOf(ValidCheckTypes...),
				},
			},
			"target": schema.StringAttribute{
				Description: "The target URL, hostname, or IP address to check.",
				Optional:    true,
			},
			"label": schema.StringAttribute{
				Description: "A label for the check. If not provided, the target will be used.",
				Optional:    true,
				Computed:    true,
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the check is enabled.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"public": schema.BoolAttribute{
				Description: "Whether public reports are enabled for this check.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"interval": schema.Float64Attribute{
				Description: "How often the check runs in minutes. Can be 0.25, 0.5, or any integer >= 1.",
				Optional:    true,
				Computed:    true,
				Default:     float64default.StaticFloat64(15),
			},
			"threshold": schema.Int64Attribute{
				Description: "Timeout in seconds for the check.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(5),
			},
			"sens": schema.Int64Attribute{
				Description: "Number of rechecks before status change.",
				Optional:    true,
				Computed:    true,
				// NodePing's default, which a check without sens also reads
				// as; see checkattr.Sens.
				Default: int64default.StaticInt64(checkattr.DefaultSens),
			},
			"mute": schema.BoolAttribute{
				Description: "Mute all notifications for this check. With the provider's ignore_mute set and mute left out, the check's mute is left to NodePing.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"dep": schema.StringAttribute{
				Description: "Check ID for notification dependency.",
				Optional:    true,
			},
			"description": schema.StringAttribute{
				Description: "Description or notes for the check (max 1000 characters).",
				Optional:    true,
			},
			"runlocations": schema.ListAttribute{
				Description: "Probe locations to run the check from.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"homeloc": schema.StringAttribute{
				Description: "Preferred probe location for the check.",
				Optional:    true,
			},
			"autodiag": schema.BoolAttribute{
				Description: "Enable automated diagnostics.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"tags": schema.ListAttribute{
				Description:         "Tags for grouping checks, as written in the configuration. The provider's default_tags are not included here; see tags_all for the tags actually applied to the check.",
				MarkdownDescription: "Tags for grouping checks, as written in the configuration. The provider's `default_tags` are not included here; see `tags_all` for the tags actually applied to the check.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"tags_all": schema.ListAttribute{
				Description:         "Every tag applied to the check: tags merged with the provider's default_tags, in that order and deduplicated.",
				MarkdownDescription: "Every tag applied to the check: `tags` merged with the provider's `default_tags`, in that order and deduplicated.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"state": schema.Int64Attribute{
				Description: "Current state of the check (0 = failing, 1 = passing).",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"created": schema.Int64Attribute{
				Description: "Timestamp when the check was created.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"modified": schema.Int64Attribute{
				Description: "Timestamp when the check was last modified.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"contentstring": schema.StringAttribute{
				Description: "String to match in the response.",
				Optional:    true,
			},
			"regex": schema.BoolAttribute{
				Description: "Treat contentstring as a regular expression.",
				Optional:    true,
			},
			"invert": schema.BoolAttribute{
				Description: "Invert the content match (does not contain).",
				Optional:    true,
			},
			"follow": schema.BoolAttribute{
				Description: "Follow redirects (HTTP checks).",
				Optional:    true,
			},
			"method": schema.StringAttribute{
				Description: "HTTP method for HTTPADV checks.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("GET", "POST", "PUT", "HEAD", "TRACE", "CONNECT"),
				},
			},
			"statuscode": schema.Int64Attribute{
				Description: "Expected HTTP status code.",
				Optional:    true,
			},
			"sendheaders": schema.MapAttribute{
				Description: "HTTP headers to send with the request.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"receiveheaders": schema.MapAttribute{
				Description: "Expected HTTP headers in the response.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"postdata": schema.StringAttribute{
				Description: "POST data for HTTPADV checks.",
				Optional:    true,
			},
			"fields": schema.MapNestedAttribute{
				Description:         "Values to parse out of the response and the range or string each must match, keyed by NodePing's key for the field (any string; the web interface makes up a random one). HTTPPARSE, PUSH, SNMP and the database types. Removing a field, or a field's min, max or match, replaces the check: NodePing cannot remove them from an existing one.",
				MarkdownDescription: "Values to parse out of the response and the range or string each must match, keyed by NodePing's key for the field (any string; the web interface makes up a random one). `HTTPPARSE`, `PUSH`, `SNMP` and the database types. Removing a field, or a field's `min`, `max` or `match`, replaces the check: NodePing cannot remove them from an existing one.",
				Optional:            true,
				PlanModifiers:       []planmodifier.Map{replaceOnRemovedFields()},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Description: "Name or path of the value to parse out of the response.",
							Required:    true,
						},
						"min": schema.Float64Attribute{
							Description: "Lowest acceptable value.",
							Optional:    true,
						},
						"max": schema.Float64Attribute{
							Description: "Highest acceptable value.",
							Optional:    true,
						},
						"match": schema.StringAttribute{
							Description: "String the value has to match. MYSQL, PGSQL and MONGODB only.",
							Optional:    true,
						},
					},
				},
			},
			"port": schema.Int64Attribute{
				Description: "Port number for the check.",
				Optional:    true,
			},
			"username": schema.StringAttribute{
				Description: "Username for authentication.",
				Optional:    true,
			},
			"password": schema.StringAttribute{
				Description: "Password for authentication.",
				Optional:    true,
				Sensitive:   true,
			},
			"secure": schema.StringAttribute{
				Description: "SSL/TLS mode: 'false', 'ssl', or 'starttls'.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("false", "ssl", "starttls"),
				},
			},
			"verify": schema.BoolAttribute{
				Description: "Verify SSL certificate or DNSSEC.",
				Optional:    true,
			},
			"ipv6": schema.BoolAttribute{
				Description: "Use IPv6 for the check.",
				Optional:    true,
			},
			"dnstype": schema.StringAttribute{
				Description: "DNS query type.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("ANY", "A", "AAAA", "CNAME", "MX", "NS", "PTR", "SOA", "SRV", "TXT"),
				},
			},
			"dnstoresolve": schema.StringAttribute{
				Description: "FQDN to resolve in DNS checks.",
				Optional:    true,
			},
			"dnssection": schema.StringAttribute{
				Description: "DNS reply section to check.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("answer", "authority", "additional", "edns_options"),
				},
			},
			"dnsrd": schema.BoolAttribute{
				Description: "DNS Recursion Desired bit.",
				Optional:    true,
			},
			"transport": schema.StringAttribute{
				Description: "Transport protocol for DNS/SIP checks.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("udp", "tcp", "tls", "ws", "wss"),
				},
			},
			"warningdays": schema.Int64Attribute{
				Description: "Days before certificate/domain expiry to fail.",
				Optional:    true,
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
			},
			"servername": schema.StringAttribute{
				Description: "Server name for SNI in SSL checks.",
				Optional:    true,
			},
			"email": schema.StringAttribute{
				Description: "Email address for SMTP checks.",
				Optional:    true,
			},
			"database": schema.StringAttribute{
				Description: "Database name for database checks.",
				Optional:    true,
			},
			"query": schema.StringAttribute{
				Description: "Query for database checks.",
				Optional:    true,
			},
			"namespace": schema.StringAttribute{
				Description: "MongoDB collection namespace.",
				Optional:    true,
			},
			"sshkey": schema.StringAttribute{
				Description: "SSH private key ID for SSH checks.",
				Optional:    true,
			},
			"clientcert": schema.StringAttribute{
				Description: "Client certificate ID for HTTPADV/DOHDOT checks.",
				Optional:    true,
			},
			"snmpv": schema.StringAttribute{
				Description: "SNMP version ('1' or '2c').",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("1", "2c"),
				},
			},
			"snmpcom": schema.StringAttribute{
				Description: "SNMP community string. Treated as a credential: it is redacted in plan output and is not exposed by the check data sources.",
				Optional:    true,
				Sensitive:   true,
			},
			"verifyvolume": schema.BoolAttribute{
				Description: "Enable the volume detection feature (AUDIO check only).",
				Optional:    true,
			},
			"volumemin": schema.Int64Attribute{
				Description: "Minimum acceptable volume threshold in dB, used by the volume detection feature (AUDIO check only). Range: -90 to 0.",
				Optional:    true,
				Validators: []validator.Int64{
					int64validator.Between(-90, 0),
				},
			},
		},
		Blocks: map[string]schema.Block{
			"notifications": schema.ListNestedBlock{
				Description: "Notification configuration for the check.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"contact_id": schema.StringAttribute{
							Description: "Contact or contact group ID to notify.",
							Required:    true,
						},
						"delay": schema.Int64Attribute{
							Description: "Delay in minutes before sending notification.",
							Optional:    true,
							Computed:    true,
							Default:     int64default.StaticInt64(0),
						},
						"schedule": schema.StringAttribute{
							// Without a default an unset schedule is sent as
							// "" and stored as "", while the check data
							// sources normalise the same notification to
							// "All" -- so the resource and the data source
							// disagreed about the same check.
							Description: `Notification schedule name. Defaults to "All", the schedule NodePing applies when a notification names none.`,
							Optional:    true,
							Computed:    true,
							Default:     stringdefault.StaticString("All"),
						},
					},
				},
			},
		},
	}
}
