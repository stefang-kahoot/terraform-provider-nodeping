package check

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// An interval passes when it is 0.25, 0.5 or a whole number of at least 1,
// and fails naming the value otherwise. Whether the account's plan allows
// 0.25 and 0.5 is not the schema's business.
func TestIntervalValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value types.Float64
		// got is the value the error names, "" when the value passes.
		got string
	}{
		{"0.25", types.Float64Value(0.25), ""},
		{"0.5", types.Float64Value(0.5), ""},
		{"1", types.Float64Value(1), ""},
		{"15", types.Float64Value(15), ""},
		{"1440", types.Float64Value(1440), ""},
		{"null", types.Float64Null(), ""},
		{"unknown", types.Float64Unknown(), ""},
		{"0", types.Float64Value(0), "0"},
		{"0.3", types.Float64Value(0.3), "0.3"},
		{"0.75", types.Float64Value(0.75), "0.75"},
		{"1.7", types.Float64Value(1.7), "1.7"},
		{"2.5", types.Float64Value(2.5), "2.5"},
		{"-1", types.Float64Value(-1), "-1"},
		{"-0.25", types.Float64Value(-0.25), "-0.25"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp := &validator.Float64Response{}
			intervalValue{}.ValidateFloat64(context.Background(), validator.Float64Request{
				Path:        path.Root("interval"),
				ConfigValue: tt.value,
			}, resp)

			if tt.got == "" {
				if resp.Diagnostics.HasError() {
					t.Fatalf("interval %s failed: %v", tt.name, resp.Diagnostics)
				}
				return
			}
			if resp.Diagnostics.ErrorsCount() != 1 {
				t.Fatalf("interval %s gave %d errors, want 1: %v", tt.name, resp.Diagnostics.ErrorsCount(), resp.Diagnostics)
			}
			d := resp.Diagnostics.Errors()[0]
			if got, want := d.Summary(), "Invalid Attribute Value"; got != want {
				t.Errorf("summary = %q, want %q", got, want)
			}
			want := "Attribute interval value must be 0.25, 0.5, or a whole number of at least 1, got: " + tt.got
			if got := d.Detail(); got != want {
				t.Errorf("detail = %q, want %q", got, want)
			}
		})
	}
}

// The schema refuses the sens, threshold and interval values NodePing refuses,
// ignores or truncates, and passes the ones it stores.
func TestCheckSchemaValidatesSensThresholdAndInterval(t *testing.T) {
	t.Parallel()

	attrs := CheckSchema().Attributes

	for _, name := range []string{"sens", "threshold"} {
		validators := attrs[name].(schema.Int64Attribute).Validators
		for _, tt := range []struct {
			value int64
			ok    bool
		}{
			{-1, false}, {0, false}, {1, true}, {10, true}, {1000000, true},
		} {
			resp := &validator.Int64Response{}
			for _, v := range validators {
				v.ValidateInt64(context.Background(), validator.Int64Request{
					Path:        path.Root(name),
					ConfigValue: types.Int64Value(tt.value),
				}, resp)
			}
			if got := !resp.Diagnostics.HasError(); got != tt.ok {
				t.Errorf("%s = %d passes: %v, want %v (%v)", name, tt.value, got, tt.ok, resp.Diagnostics)
			}
		}
	}

	validators := attrs["interval"].(schema.Float64Attribute).Validators
	for _, tt := range []struct {
		value float64
		ok    bool
	}{
		{-1, false}, {0, false}, {0.3, false}, {2.5, false},
		{0.25, true}, {0.5, true}, {1, true}, {1440, true},
	} {
		resp := &validator.Float64Response{}
		for _, v := range validators {
			v.ValidateFloat64(context.Background(), validator.Float64Request{
				Path:        path.Root("interval"),
				ConfigValue: types.Float64Value(tt.value),
			}, resp)
		}
		if got := !resp.Diagnostics.HasError(); got != tt.ok {
			t.Errorf("interval = %g passes: %v, want %v (%v)", tt.value, got, tt.ok, resp.Diagnostics)
		}
	}
}
