package contact

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// data = "" is refused; any other value, a space included, passes, and so do
// null and a value not known yet.
func TestNotEmptyData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc      string
		value     types.String
		wantError bool
	}{
		{"empty", types.StringValue(""), true},
		{"a space", types.StringValue(" "), false},
		{"JSON", types.StringValue(`{"a":1}`), false},
		{"text", types.StringValue("text={label}"), false},
		{"null", types.StringNull(), false},
		{"unknown", types.StringUnknown(), false},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			t.Parallel()
			at := path.Root("address").AtListIndex(0).AtName("data")
			resp := &validator.StringResponse{}
			NotEmptyData().ValidateString(context.Background(), validator.StringRequest{Path: at, ConfigValue: tt.value}, resp)

			if got := resp.Diagnostics.HasError(); got != tt.wantError {
				t.Fatalf("error = %v, want %v: %v", got, tt.wantError, resp.Diagnostics)
			}
			if tt.wantError {
				d := resp.Diagnostics.Errors()[0]
				if !strings.Contains(d.Detail(), "Leave data out") {
					t.Errorf("the error does not say to leave data out: %s", d.Detail())
				}
			}
		})
	}
}

// The schema puts the validator on data.
func TestSchemaRefusesEmptyData(t *testing.T) {
	t.Parallel()
	attr := ContactSchema().Blocks["address"].GetNestedObject().GetAttributes()["data"]
	validators := attr.(interface{ StringValidators() []validator.String }).StringValidators()
	for _, v := range validators {
		if _, ok := v.(notEmptyData); ok {
			return
		}
	}
	t.Error("address.data has no NotEmptyData validator")
}
