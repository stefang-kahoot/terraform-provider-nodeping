package contact

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// jsonSemanticEqualModifier implements a plan modifier that suppresses
// differences when two JSON strings are semantically equivalent.
//
// State holds a webhook's data as NodePing returns it, which is as it was
// sent (readData). After an import, or once a body is rewritten outside
// Terraform, that can be the same JSON as the configuration's written
// differently, and this keeps it from planning an update.
type jsonSemanticEqualModifier struct{}

func JSONSemanticEqual() planmodifier.String {
	return jsonSemanticEqualModifier{}
}

func (m jsonSemanticEqualModifier) Description(_ context.Context) string {
	return "Suppresses diff when JSON values are semantically equal"
}

func (m jsonSemanticEqualModifier) MarkdownDescription(_ context.Context) string {
	return "Suppresses diff when JSON values are semantically equal"
}

func (m jsonSemanticEqualModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Do nothing if there is no state value (new resource)
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}

	// Do nothing if there is no plan value
	if req.PlanValue.IsNull() || req.PlanValue.IsUnknown() {
		return
	}

	// Get the state and plan values
	stateVal := req.StateValue.ValueString()
	planVal := req.PlanValue.ValueString()

	// If they're exactly equal, nothing to do
	if stateVal == planVal {
		return
	}

	// Try to parse both as JSON and compare semantically
	if jsonSemanticEqual(stateVal, planVal) {
		// They're semantically equal, use the state value to suppress the diff
		resp.PlanValue = types.StringValue(stateVal)
	}
}

// jsonSemanticEqual compares two JSON strings for semantic equality.
// Returns true if both strings represent the same JSON value: see decodeJSON
// and sameJSON. Text that decodeJSON does not decode, as it is not JSON or
// has an object with a duplicate key, is equal only to the same text.
func jsonSemanticEqual(a, b string) bool {
	aVal, ok := decodeJSON(a)
	if !ok {
		return a == b
	}
	bVal, ok := decodeJSON(b)
	if !ok {
		return a == b
	}
	return sameJSON(aVal, bVal)
}
