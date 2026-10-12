package contact

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// notEmptyData refuses an address's data set to "". NodePing stores no data
// for an empty string (probed 2026-10-12), so a create with it read back
// without data and failed the apply with "inconsistent result after apply",
// and an update cannot clear data that way either (addressesDroppingData).
// An address without a body leaves data out. A null or unknown value passes:
// the second is checked once it is known.
type notEmptyData struct{}

var _ validator.String = notEmptyData{}

// NotEmptyData returns the validator; see notEmptyData.
func NotEmptyData() validator.String {
	return notEmptyData{}
}

func (notEmptyData) Description(context.Context) string {
	return `value must not be "": leave data out for an address without a body`
}

func (v notEmptyData) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (notEmptyData) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() || req.ConfigValue.ValueString() != "" {
		return
	}
	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Empty webhook data",
		`data is set to "", but NodePing stores no data for an empty string, so the address would read back without it. `+
			"Leave data out for an address without a body.",
	)
}
