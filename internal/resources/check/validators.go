package check

import (
	"context"
	"fmt"
	"math"

	"github.com/hashicorp/terraform-plugin-framework-validators/helpers/validatordiag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// intervalValue refuses an interval other than 0.25, 0.5 or a whole number of
// at least 1: the intervals NodePing documents. NodePing does not refuse every
// other value. Probed on 2026-10-12 (docs/nodeping-api-quirks.md, quirk 13),
// it refused -1 with "Invalid check interval", but ignored 0 and 0.3, keeping
// the check's interval or giving a new check 15, and truncated 2.5 to 2. Both
// answer as if the value had been applied.
//
// Whether the account's NodePing plan allows 0.25 and 0.5 is not this
// validator's business.
type intervalValue struct{}

var _ validator.Float64 = intervalValue{}

func (intervalValue) Description(context.Context) string {
	return "value must be 0.25, 0.5, or a whole number of at least 1"
}

func (v intervalValue) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v intervalValue) ValidateFloat64(ctx context.Context, req validator.Float64Request, resp *validator.Float64Response) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	value := req.ConfigValue.ValueFloat64()
	if value == 0.25 || value == 0.5 || (value >= 1 && value == math.Trunc(value)) {
		return
	}
	resp.Diagnostics.Append(validatordiag.InvalidAttributeValueDiagnostic(
		req.Path,
		v.Description(ctx),
		fmt.Sprintf("%g", value),
	))
}
