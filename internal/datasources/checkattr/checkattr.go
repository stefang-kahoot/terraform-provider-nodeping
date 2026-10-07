// Package checkattr holds the read-back shape of a check, shared by the
// nodeping_check and nodeping_checks data sources and by the nodeping_check
// resource, so the three cannot drift apart.
//
// Attributes() and the schema half serve the data sources. FromAPI and the
// Model serve all three: the resource copies the fields it shares onto its own
// model rather than keeping a second mapping. It once did keep one, and the
// drift between the two copies is what left ten check parameters unmapped on
// the resource -- invisibly, since an unmapped attribute reads back null and
// so produces no plan.
//
// Credentials are deliberately absent. `password` is a stored secret (and is
// marked sensitive on the resource) and `snmpcom` is an SNMP community string,
// which is a shared secret in all but name. A data source exists to be read,
// and its values land in state and in plan output, so neither belongs here;
// the resource carries both itself and restores them from configuration.
// `sshkey` and `clientcert` are included because the API returns NodePing's
// *identifiers* for stored keys, not the key material itself.
package checkattr

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
)

// Model is the full read-back shape of a check. Field names and tfsdk tags
// mirror the nodeping_check resource so a data source result can be used to
// write one.
type Model struct {
	// Envelope
	ID          types.String  `tfsdk:"id"`
	CustomerID  types.String  `tfsdk:"customer_id"`
	Type        types.String  `tfsdk:"type"`
	Target      types.String  `tfsdk:"target"`
	Label       types.String  `tfsdk:"label"`
	Enabled     types.Bool    `tfsdk:"enabled"`
	Public      types.Bool    `tfsdk:"public"`
	Interval    types.Float64 `tfsdk:"interval"`
	Threshold   types.Int64   `tfsdk:"threshold"`
	Sens        types.Int64   `tfsdk:"sens"`
	Mute        types.Bool    `tfsdk:"mute"`
	AutoDiag    types.Bool    `tfsdk:"autodiag"`
	Dep         types.String  `tfsdk:"dep"`
	State       types.Int64   `tfsdk:"state"`
	Created     types.Int64   `tfsdk:"created"`
	Modified    types.Int64   `tfsdk:"modified"`
	Description types.String  `tfsdk:"description"`
	Tags        types.List    `tfsdk:"tags"`

	RunLocations types.List   `tfsdk:"runlocations"`
	HomeLoc      types.String `tfsdk:"homeloc"`

	// HTTP family
	ContentString  types.String `tfsdk:"contentstring"`
	Regex          types.Bool   `tfsdk:"regex"`
	Invert         types.Bool   `tfsdk:"invert"`
	Follow         types.Bool   `tfsdk:"follow"`
	Method         types.String `tfsdk:"method"`
	StatusCode     types.Int64  `tfsdk:"statuscode"`
	SendHeaders    types.Map    `tfsdk:"sendheaders"`
	ReceiveHeaders types.Map    `tfsdk:"receiveheaders"`
	PostData       types.String `tfsdk:"postdata"`

	// Connection
	Port       types.Int64  `tfsdk:"port"`
	Username   types.String `tfsdk:"username"`
	Secure     types.String `tfsdk:"secure"`
	Verify     types.Bool   `tfsdk:"verify"`
	IPv6       types.Bool   `tfsdk:"ipv6"`
	ServerName types.String `tfsdk:"servername"`
	Transport  types.String `tfsdk:"transport"`

	// DNS
	DNSType      types.String `tfsdk:"dnstype"`
	DNSToResolve types.String `tfsdk:"dnstoresolve"`
	DNSSection   types.String `tfsdk:"dnssection"`
	DNSRD        types.Bool   `tfsdk:"dnsrd"`

	// SSL / certificates
	WarningDays types.Int64  `tfsdk:"warningdays"`
	ClientCert  types.String `tfsdk:"clientcert"`

	// Databases and services
	Email     types.String `tfsdk:"email"`
	Database  types.String `tfsdk:"database"`
	Query     types.String `tfsdk:"query"`
	Namespace types.String `tfsdk:"namespace"`
	SSHKey    types.String `tfsdk:"sshkey"`
	SNMPv     types.String `tfsdk:"snmpv"`

	// AUDIO
	VerifyVolume types.Bool  `tfsdk:"verifyvolume"`
	VolumeMin    types.Int64 `tfsdk:"volumemin"`

	Notifications []NotificationModel `tfsdk:"notifications"`
}

// NotificationModel mirrors the notifications block on the resource: who gets
// told when the check changes state.
type NotificationModel struct {
	ContactID types.String `tfsdk:"contact_id"`
	Delay     types.Int64  `tfsdk:"delay"`
	Schedule  types.String `tfsdk:"schedule"`
}

// Attributes returns every attribute as Computed. The singular data source
// overrides `id` to Required; the plural nests the whole map unchanged.
func Attributes() map[string]schema.Attribute {
	str := func(desc string) schema.Attribute {
		return schema.StringAttribute{Description: desc, Computed: true}
	}
	b := func(desc string) schema.Attribute {
		return schema.BoolAttribute{Description: desc, Computed: true}
	}
	i64 := func(desc string) schema.Attribute {
		return schema.Int64Attribute{Description: desc, Computed: true}
	}
	strMap := func(desc string) schema.Attribute {
		return schema.MapAttribute{Description: desc, Computed: true, ElementType: types.StringType}
	}
	strList := func(desc string) schema.Attribute {
		return schema.ListAttribute{Description: desc, Computed: true, ElementType: types.StringType}
	}

	return map[string]schema.Attribute{
		"id":          str("The unique identifier of the check."),
		"customer_id": str("The customer ID (account ID) that owns this check."),
		"type":        str("The check type, for example HTTP, PING or SSL."),
		"target":      str("The target the check runs against."),
		"label":       str("The display label of the check."),
		"enabled":     b("Whether the check is enabled."),
		"public":      b("Whether the check has a public reports page."),
		"interval":    schema.Float64Attribute{Description: "How often the check runs, in minutes.", Computed: true},
		"threshold":   i64("Timeout in seconds for the check."),
		"sens":        i64("Number of rechecks before the check is considered down."),
		"mute":        b("Whether notifications for this check are muted."),
		"autodiag":    b("Whether automatic diagnostics are enabled."),
		"dep":         str("ID of the check this one depends on for notifications."),
		"state":       i64("Current state of the check (0 = failing, 1 = passing)."),
		"created":     i64("Creation timestamp in milliseconds."),
		"modified":    i64("Last modification timestamp in milliseconds."),
		"description": str("Free-form description of the check."),
		"tags":        strList("Tags assigned to the check."),

		"runlocations": strList("Probe locations the check runs from."),
		"homeloc":      str("Preferred probe location for the check."),

		"contentstring":  str("String the response is checked for. HTTPCONTENT, HTTPPARSE and similar types."),
		"regex":          b("Whether contentstring is treated as a regular expression."),
		"invert":         b("Whether the content match is inverted."),
		"follow":         b("Whether redirects are followed."),
		"method":         str("HTTP method used by the check."),
		"statuscode":     i64("Expected HTTP status code."),
		"sendheaders":    strMap("Headers sent with the request."),
		"receiveheaders": strMap("Headers the response is checked for."),
		"postdata":       str("Body sent with the request."),

		"port":       i64("Port the check connects to."),
		"username":   str("Username used for authentication. The password is deliberately not exposed."),
		"secure":     str("Transport security mode: false, ssl or starttls."),
		"verify":     b("Whether the TLS certificate is verified."),
		"ipv6":       b("Whether the check resolves over IPv6."),
		"servername": str("Server name sent via SNI."),
		"transport":  str("Transport protocol: udp, tcp, tls, ws or wss."),

		"dnstype":      str("DNS record type queried."),
		"dnstoresolve": str("Name resolved by the DNS check."),
		"dnssection":   str("DNS response section inspected."),
		"dnsrd":        b("Whether the recursion-desired flag is set."),

		"warningdays": i64("Days before certificate expiry at which the check warns."),
		"clientcert":  str("Identifier of the stored client certificate. Not the certificate itself."),

		"email":     str("Email address used by the check."),
		"database":  str("Database name the check connects to."),
		"query":     str("Query the check executes."),
		"namespace": str("Namespace the check inspects."),
		"sshkey":    str("Identifier of the stored SSH key. Not the key itself."),
		"snmpv":     str("SNMP version: 1 or 2c. The community string is deliberately not exposed."),

		"verifyvolume": b("Whether volume detection is enabled. AUDIO checks only."),
		"volumemin":    i64("Minimum acceptable volume in dB. AUDIO checks only."),

		"notifications": schema.ListNestedAttribute{
			Description: "Who is notified when the check changes state, ordered as the API returns them.",
			Computed:    true,
			NestedObject: schema.NestedAttributeObject{
				Attributes: map[string]schema.Attribute{
					"contact_id": str("ID of the notified contact or contact group."),
					"delay":      i64("Minutes to wait before notifying."),
					"schedule":   str("Notification schedule the contact is notified on."),
				},
			},
		},
	}
}

// --- conversions -----------------------------------------------------------
//
// The NodePing API is inconsistent about how it encodes scalars: booleans come
// back as bool, as "true"/"false" strings or as 0/1 numbers, and integers as
// either JSON numbers or strings. These helpers absorb that in one place.

// Bool converts the shapes the API uses for booleans.
func Bool(v interface{}) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true" || val == "1"
	case float64:
		return val != 0
	case int:
		return val != 0
	default:
		return false
	}
}

// OptionalBool keeps an absent value null instead of reporting false, so a
// check type that simply does not use the field is distinguishable from one
// where it is switched off.
func OptionalBool(v interface{}) types.Bool {
	if v == nil {
		return types.BoolNull()
	}
	return types.BoolValue(Bool(v))
}

// OptionalInt64 accepts the JSON number and the stringified forms.
func OptionalInt64(v interface{}) types.Int64 {
	switch val := v.(type) {
	case nil:
		return types.Int64Null()
	case float64:
		return types.Int64Value(int64(val))
	case int:
		return types.Int64Value(int64(val))
	case int64:
		return types.Int64Value(val)
	case string:
		var parsed int64
		if _, err := fmt.Sscanf(val, "%d", &parsed); err != nil {
			return types.Int64Null()
		}
		return types.Int64Value(parsed)
	default:
		return types.Int64Null()
	}
}

// OptionalWarningDays maps warningdays like OptionalInt64, except that 0 is
// null too. The web interface saves an empty "days before expiration" field as
// 0, while checks written through the API carry "" -- both mean the check
// fails only once the certificate expires, and both read as null, matching
// the nodeping_check resource, which rejects 0 in configuration.
func OptionalWarningDays(v interface{}) types.Int64 {
	days := OptionalInt64(v)
	if !days.IsNull() && days.ValueInt64() <= 0 {
		return types.Int64Null()
	}
	return days
}

// OptionalString maps the API's empty string to null, matching how an unset
// attribute looks in a configuration.
func OptionalString(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// stringFromInterface handles sshkey and clientcert, which the API types as
// interface{} because it answers with `false` when they are unset.
func stringFromInterface(v interface{}) types.String {
	s, ok := v.(string)
	if !ok || s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

func stringList(ctx context.Context, items []string, diags *diag.Diagnostics) types.List {
	if len(items) == 0 {
		return types.ListNull(types.StringType)
	}
	list, d := types.ListValueFrom(ctx, types.StringType, items)
	diags.Append(d...)
	return list
}

func stringMap(ctx context.Context, m map[string]string, diags *diag.Diagnostics) types.Map {
	if len(m) == 0 {
		return types.MapNull(types.StringType)
	}
	out, d := types.MapValueFrom(ctx, types.StringType, m)
	diags.Append(d...)
	return out
}

// RunLocations normalises the field, which the API answers with `false` when
// unset and with a list of location codes otherwise.
func RunLocations(ctx context.Context, v interface{}, diags *diag.Diagnostics) types.List {
	switch rl := v.(type) {
	case []interface{}:
		locations := make([]string, 0, len(rl))
		for _, loc := range rl {
			if s, ok := loc.(string); ok {
				locations = append(locations, s)
			}
		}
		return stringList(ctx, locations, diags)
	case []string:
		return stringList(ctx, rl, diags)
	default:
		return types.ListNull(types.StringType)
	}
}

// FromAPI maps a check as returned by the API onto the shared model.
func FromAPI(ctx context.Context, check *client.Check, diags *diag.Diagnostics) Model {
	p := check.Parameters

	m := Model{
		ID:          types.StringValue(check.ID),
		CustomerID:  types.StringValue(check.CustomerID),
		Type:        types.StringValue(check.Type),
		Target:      OptionalString(p.Target),
		Label:       OptionalString(check.Label),
		Enabled:     types.BoolValue(check.Enabled == "active"),
		Public:      types.BoolValue(check.Public),
		Threshold:   OptionalInt64(p.Threshold),
		Sens:        OptionalInt64(p.Sens),
		Mute:        types.BoolValue(Bool(check.Mute)),
		AutoDiag:    types.BoolValue(check.AutoDiag),
		State:       types.Int64Value(int64(check.State)),
		Created:     types.Int64Value(check.Created),
		Modified:    types.Int64Value(check.Modified),
		Description: OptionalString(check.Description),

		HomeLoc: stringFromInterface(check.HomeLoc),

		ContentString:  OptionalString(p.ContentString),
		Regex:          OptionalBool(p.Regex),
		Invert:         OptionalBool(p.Invert),
		Follow:         OptionalBool(p.Follow),
		Method:         OptionalString(p.Method),
		StatusCode:     OptionalInt64(p.StatusCode),
		SendHeaders:    stringMap(ctx, p.SendHeaders, diags),
		ReceiveHeaders: stringMap(ctx, p.ReceiveHeaders, diags),
		PostData:       OptionalString(p.PostData),

		Port:       OptionalInt64(p.Port),
		Username:   OptionalString(p.Username),
		Secure:     stringFromInterface(p.Secure),
		Verify:     OptionalBool(p.Verify),
		IPv6:       OptionalBool(p.IPv6),
		ServerName: OptionalString(p.ServerName),
		Transport:  OptionalString(p.Transport),

		DNSType:      OptionalString(p.DNSType),
		DNSToResolve: OptionalString(p.DNSToResolve),
		DNSSection:   OptionalString(p.DNSSection),
		DNSRD:        OptionalBool(p.DNSRD),

		WarningDays: OptionalWarningDays(p.WarningDays),
		ClientCert:  stringFromInterface(p.ClientCert),

		Email:     OptionalString(p.Email),
		Database:  OptionalString(p.Database),
		Query:     OptionalString(p.Query),
		Namespace: OptionalString(p.Namespace),
		SSHKey:    stringFromInterface(p.SSHKey),
		SNMPv:     OptionalString(p.SNMPv),

		VerifyVolume: OptionalBool(p.VerifyVolume),
		VolumeMin:    OptionalInt64(p.VolumeMin),
	}

	if interval, err := check.Interval.Float64(); err == nil {
		m.Interval = types.Float64Value(interval)
	} else {
		m.Interval = types.Float64Null()
	}

	if dep, ok := check.Dep.(string); ok && dep != "" {
		m.Dep = types.StringValue(dep)
	} else {
		m.Dep = types.StringNull()
	}

	m.Tags = stringList(ctx, check.Tags, diags)
	m.RunLocations = RunLocations(ctx, check.RunLocations, diags)
	m.Notifications = notifications(check.Notifications)

	return m
}

// notifications flattens the API shape, which is a list of single-entry maps
// keyed by contact ID: [{"CONTACT-1": {"delay": 0, "schedule": "All"}}].
//
// The outer list order is the API's and is preserved. The inner map is sorted
// by contact ID, because Go randomises map iteration and an entry with more
// than one key would otherwise reorder between reads and show a phantom diff.
// Duplicates are dropped, matching how the resource reads the same field.
func notifications(raw []map[string]interface{}) []NotificationModel {
	if len(raw) == 0 {
		return nil
	}

	out := make([]NotificationModel, 0, len(raw))
	seen := make(map[string]bool)

	for _, entry := range raw {
		contactIDs := make([]string, 0, len(entry))
		for contactID := range entry {
			contactIDs = append(contactIDs, contactID)
		}
		sort.Strings(contactIDs)

		for _, contactID := range contactIDs {
			cfg, ok := entry[contactID].(map[string]interface{})
			if !ok {
				continue
			}

			delay := OptionalInt64(cfg["delay"])
			if delay.IsNull() {
				delay = types.Int64Value(0)
			}

			schedule := "All"
			if s, ok := cfg["schedule"].(string); ok && s != "" {
				schedule = s
			}

			key := fmt.Sprintf("%s:%d:%s", contactID, delay.ValueInt64(), schedule)
			if seen[key] {
				continue
			}
			seen[key] = true

			out = append(out, NotificationModel{
				ContactID: types.StringValue(contactID),
				Delay:     delay,
				Schedule:  types.StringValue(schedule),
			})
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}
