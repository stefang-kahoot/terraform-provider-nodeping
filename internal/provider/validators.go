package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/helpers/validatordiag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// positiveFloat64 refuses a number that is not greater than 0, which
// float64validator has no validator for: AtLeast lets the bound itself pass.
type positiveFloat64 struct{}

var _ validator.Float64 = positiveFloat64{}

func (positiveFloat64) Description(context.Context) string {
	return "value must be greater than 0"
}

func (v positiveFloat64) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v positiveFloat64) ValidateFloat64(ctx context.Context, req validator.Float64Request, resp *validator.Float64Response) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if value := req.ConfigValue.ValueFloat64(); value <= 0 {
		resp.Diagnostics.Append(validatordiag.InvalidAttributeValueDiagnostic(
			req.Path,
			v.Description(ctx),
			fmt.Sprintf("%g", value),
		))
	}
}
