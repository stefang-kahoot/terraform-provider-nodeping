package check

import (
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// sentAutoDiag is autodiag as a create or an update sends it: the string
// "true" or "false", which is how NodePing documents it. NodePing ignores
// public sent as the boolean false on an update (finding 30), so the
// documented form is the safe one here too. A value not known is left out.
func sentAutoDiag(v types.Bool) interface{} {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return strconv.FormatBool(v.ValueBool())
}

// answeredAutoDiag is the autodiag mapCheckToModel resolves through keep.
//
// An account whose NodePing plan lacks Automated Diagnostics drops autodiag:
// it accepts true and answers with false, on every check type (finding 36,
// on a grandfathered Business plan). So in an apply an answer of false says
// nothing about a true just sent, and keep holds the planned value, as it
// does for a value an answer leaves out. The apply then matches its plan; the
// next refresh reads NodePing as it is, and a check that dropped autodiag
// shows it as a change. A refresh and an import take the answer as it is.
func answeredAutoDiag(holds modelHolds, current, read types.Bool) types.Bool {
	if holds == thePlan && !current.IsUnknown() && !read.ValueBool() {
		return types.BoolNull()
	}
	return read
}
