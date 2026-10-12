package notblank

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestValidator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc      string
		value     types.String
		wantError bool
	}{
		{"a name", types.StringValue("On-call"), false},
		{"a name with spaces around it", types.StringValue(" On-call "), false},
		{"a single character", types.StringValue("x"), false},
		{"null", types.StringNull(), false},
		{"unknown", types.StringUnknown(), false},
		{"empty", types.StringValue(""), true},
		{"a space", types.StringValue(" "), true},
		{"tabs and newlines", types.StringValue("\t\n \r"), true},
		{"a no-break space", types.StringValue(" "), true},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			t.Parallel()

			req := validator.StringRequest{Path: path.Root("name"), ConfigValue: tt.value}
			var resp validator.StringResponse
			Validator().ValidateString(context.Background(), req, &resp)

			if got := resp.Diagnostics.HasError(); got != tt.wantError {
				t.Errorf("error: %v, want %v (diagnostics: %v)", got, tt.wantError, resp.Diagnostics)
			}
		})
	}
}

// The error reads like the framework's own validators', and shows the value.
func TestValidatorMessage(t *testing.T) {
	t.Parallel()

	req := validator.StringRequest{Path: path.Root("name"), ConfigValue: types.StringValue(" ")}
	var resp validator.StringResponse
	Validator().ValidateString(context.Background(), req, &resp)

	if len(resp.Diagnostics) != 1 {
		t.Fatalf("got %d diagnostics, want 1: %v", len(resp.Diagnostics), resp.Diagnostics)
	}
	d := resp.Diagnostics[0]
	if got, want := d.Summary(), "Invalid Attribute Value"; got != want {
		t.Errorf("summary %q, want %q", got, want)
	}
	if got, want := d.Detail(), `Attribute name value must not be empty or only whitespace, got: " "`; got != want {
		t.Errorf("detail %q, want %q", got, want)
	}
}
