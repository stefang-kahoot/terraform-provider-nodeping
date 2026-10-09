package check

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// storedBy lists, for each check attribute NodePing stores only on some check
// types, the types that store it. A create or an update of any other type
// accepts the attribute, answers without it and does not store it.
//
// From a probe of the NodePing API on 2026-10-09 (finding 36, test
// SubAccount): each of the 33 check types was created with every attribute
// the provider sends, a second check of each type was updated with all of
// them, and a third was created with other values. Create and update agreed
// everywhere. An attribute counts as stored by a type when all three checks
// held it. The list matches NodePing's API documentation, except that DOHDOT
// stores statuscode as well.
//
// Attributes not listed are stored by every type, whether or not the type
// uses them: contentstring, invert, follow, port, verify and ipv6 among them.
// autodiag, homeloc, verifyvolume and volumemin are not listed either, for
// the opposite reason: no type stored them on the probed account, so the
// probe cannot say which types take them.
var storedBy = map[string][]string{
	"regex":          {"HTTPCONTENT", "HTTPADV"},
	"method":         {"DOHDOT", "HTTPADV"},
	"statuscode":     {"DOHDOT", "HTTPADV"},
	"sendheaders":    {"DOHDOT", "HTTPPARSE", "HTTPADV"},
	"receiveheaders": {"HTTPADV"},
	"postdata":       {"HTTPADV"},
	"fields":         {"HTTPPARSE", "MONGODB", "MYSQL", "PGSQL", "PUSH", "SNMP"},
	"username":       {"FTP", "IMAP4", "MYSQL", "POP3", "SMTP", "SSH"},
	"password":       {"FTP", "IMAP4", "MYSQL", "POP3", "SMTP", "SSH"},
	"secure":         {"IMAP4", "MYSQL", "POP3", "SMTP"},
	"dnstype":        {"DOHDOT", "DNS"},
	"dnstoresolve":   {"DOHDOT", "DNS"},
	"dnssection":     {"DNS"},
	"dnsrd":          {"DNS"},
	"transport":      {"DNS", "SIP"},
	"warningdays":    {"IMAP4", "POP3", "RDAP", "SMTP", "SSL", "WHOIS"},
	"servername":     {"SSL"},
	"email":          {"SMTP"},
	"database":       {"MONGODB", "MYSQL"},
	"query":          {"MONGODB", "MYSQL", "PGSQL"},
	"namespace":      {"MONGODB"},
	"sshkey":         {"SSH"},
	"clientcert":     {"DOHDOT", "HTTPADV"},
	"snmpv":          {"SNMP"},
	"snmpcom":        {"SNMP"},
}

// stores reports whether NodePing stores the attribute name on checks of type
// typ.
func stores(typ, name string) bool {
	storers, listed := storedBy[name]
	return !listed || slices.Contains(storers, typ)
}

// refuseUnstored checks a plan against the attributes the planned check type
// does not store (see storedBy). NodePing accepts a value for one and drops
// it, so the plan after the apply would show the value again, every time.
//
//   - A create fails with any value but an empty one.
//   - An update fails when it adds or changes such a value. A check can hold
//     one nonetheless: NodePing keeps the parameters of a check's earlier type
//     when the type is changed, and returns them, but no update of the new
//     type changes or clears them. An update that leaves one as it is passes,
//     so a check imported with one plans clean.
//   - An update that removes one replaces the check instead, with a warning:
//     no update can (see replacement).
//   - A plan that replaces the check counts as a create. Terraform plans the
//     new check again later, with no prior state, and the outcome has to be
//     the same.
//
// The type is the planned one, since a type change is an update in place:
// NodePing applies the attributes of the type an update sends.
func refuseUnstored(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	var typ types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("type"), &typ)...)
	if resp.Diagnostics.HasError() || typ.IsNull() || typ.IsUnknown() {
		return
	}

	values := unstoredValues(ctx, req, typ.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	change := createsCheck
	if !req.State.Raw.IsNull() {
		replaces, removed := replacement(ctx, req, values, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
		change = updatesCheck
		if replaces {
			change = replacesCheck
		}
		for _, name := range removed {
			resp.RequiresReplace = append(resp.RequiresReplace, path.Root(name))
			addHeldRemovedWarning(ctx, req.State, &resp.Diagnostics, name, typ.ValueString())
		}
	}

	for _, v := range values {
		if sendsUnstored(ctx, v.planned, v.prior, change != updatesCheck) {
			addUnstoredError(&resp.Diagnostics, v.name, typ.ValueString(), change)
		}
	}
}

// checkChange is what a plan does to a check.
type checkChange int

const (
	createsCheck checkChange = iota
	replacesCheck
	updatesCheck
)

// unstoredValue is an attribute the planned check type does not store, with
// its planned value and, unless the check is new, the prior state's.
type unstoredValue struct {
	name           string
	planned, prior attr.Value
}

// unstoredValues reads, sorted by name, the attributes the check type typ does
// not store.
func unstoredValues(ctx context.Context, req resource.ModifyPlanRequest, typ string, diags *diag.Diagnostics) []unstoredValue {
	names := make([]string, 0, len(storedBy))
	for name := range storedBy {
		if !stores(typ, name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	values := make([]unstoredValue, 0, len(names))
	for _, name := range names {
		v := unstoredValue{name: name}
		diags.Append(req.Plan.GetAttribute(ctx, path.Root(name), &v.planned)...)
		if !req.State.Raw.IsNull() {
			diags.Append(req.State.GetAttribute(ctx, path.Root(name), &v.prior)...)
		}
		values = append(values, v)
	}
	return values
}

// replacement reports whether an update of a check replaces it instead, and
// lists the values it removes that the check holds although the planned type
// does not store them. The caller marks those as requiring replacement.
//
// Two removals replace a check, because no update can make them:
//   - A field, on any type: NodePing cannot remove one (finding 31).
//     replaceOnRemovedFields plans that and warns about it; the resource's
//     ModifyPlan does not see what the attributes' plan modifiers decided, so
//     this asks the same question again.
//   - A value the planned type does not store, made empty or left out: the
//     check holds it from an earlier type, and no update of this one changes
//     or clears it. fields is left to the first case, which covers it.
func replacement(ctx context.Context, req resource.ModifyPlanRequest, values []unstoredValue, diags *diag.Diagnostics) (bool, []string) {
	var removed []string
	for _, v := range values {
		if v.name != "fields" && blank(v.planned) && !blank(v.prior) {
			removed = append(removed, v.name)
		}
	}

	var prior, planned types.Map
	diags.Append(req.State.GetAttribute(ctx, path.Root("fields"), &prior)...)
	diags.Append(req.Plan.GetAttribute(ctx, path.Root("fields"), &planned)...)
	if diags.HasError() {
		return false, nil
	}
	fieldsRemoved := len(removedFieldsBetween(ctx, prior, planned, diags)) > 0

	return fieldsRemoved || len(removed) > 0, removed
}

// sendsUnstored reports whether a planned value for an attribute the check's
// type does not store would be sent to NodePing for nothing: on create, any
// value that is not empty; on update, one that differs from the prior
// state's, unless both are empty.
//
// A planned value not yet known is let through: there is nothing to compare.
// That is safe enough. Terraform plans the check once more during the apply,
// with the value known by then, and this fails the apply there, before
// anything is sent. Should a value stay unknown even then, NodePing drops it,
// so the apply changes nothing, and the next plan fails here.
func sendsUnstored(ctx context.Context, planned, prior attr.Value, create bool) bool {
	if planned.IsUnknown() {
		return false
	}
	if create {
		return !blank(planned)
	}
	if blank(planned) && blank(prior) {
		return false
	}
	if !fullyKnown(ctx, planned) {
		return false
	}
	return !planned.Equal(prior)
}

// blank reports whether a value is one NodePing treats as no value: null, "",
// false, 0, or a list or map with nothing in it. isEmpty, unlike this, holds
// 0 to be a value, since clearing one sends something else.
func blank(v attr.Value) bool {
	switch v := v.(type) {
	case types.Int64:
		if !v.IsNull() && !v.IsUnknown() && v.ValueInt64() == 0 {
			return true
		}
	case types.Float64:
		if !v.IsNull() && !v.IsUnknown() && v.ValueFloat64() == 0 {
			return true
		}
	}
	return isEmpty(v)
}

// fullyKnown reports whether a value holds nothing unknown, also inside a
// list or a map.
func fullyKnown(ctx context.Context, v attr.Value) bool {
	raw, err := v.ToTerraformValue(ctx)
	return err == nil && raw.IsFullyKnown()
}

// addUnstoredError adds the error for an attribute the check type typ does not
// store, on the attribute's path so that Terraform points at its line in the
// configuration.
func addUnstoredError(diags *diag.Diagnostics, name, typ string, change checkChange) {
	storers := storersOf(name)

	var detail string
	switch change {
	case createsCheck:
		detail = fmt.Sprintf("NodePing does not store %[1]s on %[2]s checks: it accepts the value and drops it, "+
			"so every plan after the apply would show %[1]s again.\n\n%[3]s\n\n"+
			"Remove %[1]s from the configuration, or use one of those types.", name, typ, storers)
	case replacesCheck:
		detail = fmt.Sprintf("This plan replaces the check with a new one, and NodePing does not store %[1]s on %[2]s checks: "+
			"it accepts the value and drops it, so every plan after the apply would show %[1]s again.\n\n%[3]s\n\n"+
			"Remove %[1]s from the configuration, or use one of those types.", name, typ, storers)
	default:
		detail = fmt.Sprintf("NodePing does not store %[1]s on %[2]s checks, so an update cannot add or change it: "+
			"every plan would show the change again. A check can hold such a value from before its type was changed; "+
			"it stays as long as the configuration has the same value.\n\n%[3]s\n\n"+
			"To change %[1]s, change the check's type to one of those as well. "+
			"Removing %[1]s from the configuration replaces the check.", name, typ, storers)
	}

	diags.AddAttributeError(path.Root(name), fmt.Sprintf("%s is not stored for %s checks", name, typ), detail)
}

// addHeldRemovedWarning says that the plan replaces the check because it
// removes name, which the check holds although NodePing does not store it on
// checks of type typ. In the spirit of replaceOnRemovedFields' warning.
func addHeldRemovedWarning(ctx context.Context, state tfsdk.State, diags *diag.Diagnostics, name, typ string) {
	diags.AddAttributeWarning(path.Root(name),
		fmt.Sprintf("Removing %s replaces the check", name),
		fmt.Sprintf("The configuration of check %[1]s removes %[2]s, which the check holds. "+
			"NodePing does not store %[2]s on %[3]s checks, and an update of one can neither change nor remove the value a check already holds. "+
			"Terraform will replace the check instead: delete it and create a new one with a new ID, "+
			"which starts without the old check's history and breaks anything that refers to the old ID, such as another check's dep. "+
			"To keep the check, put %[2]s back in the configuration as it is, "+
			"or change the check's type to one that stores it, in the same plan. %[4]s",
			checkName(ctx, state), name, typ, storersOf(name)))
}

// storersOf names the check types that store the attribute name.
func storersOf(name string) string {
	return "The check types that store " + name + ": " + strings.Join(storedBy[name], ", ") + "."
}
