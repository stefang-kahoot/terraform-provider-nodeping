// Package notblank refuses a contact's or contact group's name that is empty
// or only whitespace.
//
// NodePing cannot clear either name: an update that leaves `name` out, or
// sends "" or null, keeps it. The update request leaves an empty name out,
// so NodePing would keep the old one and Terraform would plan the change
// again on every run. A blank name fares no better: NodePing ignores " " in
// a group update, which fails the apply, and stores it for a contact, which
// leaves the contact without a visible name.
package notblank

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/helpers/validatordiag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

type notBlank struct{}

var _ validator.String = notBlank{}

// Validator refuses a string that is empty or holds only whitespace, as
// strings.TrimSpace defines it. A null or unknown value passes: Required
// covers the first, and the second is checked once it is known.
func Validator() validator.String {
	return notBlank{}
}

func (notBlank) Description(context.Context) string {
	return "value must not be empty or only whitespace"
}

func (v notBlank) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v notBlank) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if strings.TrimSpace(req.ConfigValue.ValueString()) == "" {
		resp.Diagnostics.Append(validatordiag.InvalidAttributeValueDiagnostic(
			req.Path,
			v.Description(ctx),
			req.ConfigValue.String(),
		))
	}
}
