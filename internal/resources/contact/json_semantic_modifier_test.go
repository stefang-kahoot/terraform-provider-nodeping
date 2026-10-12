package contact

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestJSONSemanticEqual(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{
			name: "identical strings",
			a:    `{"a":1}`,
			b:    `{"a":1}`,
			want: true,
		},
		{
			name: "key order does not matter",
			a:    `{"a":1,"b":2}`,
			b:    `{"b":2,"a":1}`,
			want: true,
		},
		{
			name: "whitespace does not matter",
			a:    `{"a": 1,  "b":  2}`,
			b:    `{"a":1,"b":2}`,
			want: true,
		},
		{
			name: "nested objects compared deeply",
			a:    `{"x":{"y":[1,2,{"z":true}]}}`,
			b:    `{"x":{"y":[1,2,{"z":true}]}}`,
			want: true,
		},
		{
			name: "different values are not equal",
			a:    `{"a":1}`,
			b:    `{"a":2}`,
			want: false,
		},
		{
			name: "array order matters",
			a:    `[1,2,3]`,
			b:    `[3,2,1]`,
			want: false,
		},
		{
			name: "missing key is not equal",
			a:    `{"a":1,"b":2}`,
			b:    `{"a":1}`,
			want: false,
		},
		{
			name: "the same number written differently is equal",
			a:    `{"a":1}`,
			b:    `{"a":1.0}`,
			want: true,
		},
		{
			name: "a number with an exponent is the same number without",
			a:    `[100, 0.5, -1.50, 0]`,
			b:    `[1e2, 5E-1, -15e-1, -0.0]`,
			want: true,
		},
		// Decoded as float64, integers beyond 2^53 round, and two different
		// ones could compare equal: a change to one planned no change.
		{
			name: "large integers that differ are not equal",
			a:    `{"id":12345678901234567890}`,
			b:    `{"id":12345678901234567891}`,
			want: false,
		},
		{
			name: "integers just beyond 2^53 that differ are not equal",
			a:    `9007199254740993`,
			b:    `9007199254740992`,
			want: false,
		},
		{
			name: "the same large integer is equal",
			a:    `{"id": 12345678901234567890}`,
			b:    `{"id":12345678901234567890}`,
			want: true,
		},
		{
			name: "fractions that differ beyond float64's precision are not equal",
			a:    `0.10000000000000000001`,
			b:    `0.1`,
			want: false,
		},
		{
			name: "the same number with a huge exponent is equal",
			a:    `[1e99999999999999999999]`,
			b:    `[ 1e99999999999999999999 ]`,
			want: true,
		},
		{
			name: "different huge exponents are not equal",
			a:    `1e99999999999999999999`,
			b:    `1e99999999999999999998`,
			want: false,
		},
		// Decoding keeps the last of duplicate keys, so text with them is
		// only the same as the same text.
		{
			name: "duplicate keys are not the last value alone",
			a:    `{"a":1,"a":2}`,
			b:    `{"a":2}`,
			want: false,
		},
		{
			name: "duplicate keys spelled differently are still duplicates",
			a:    `{"a":1,"\u0061":2}`,
			b:    `{"a":2}`,
			want: false,
		},
		{
			name: "nested duplicate keys are not the last value alone",
			a:    `{"x":{"a":1,"a":2}}`,
			b:    `{"x":{"a":2}}`,
			want: false,
		},
		{
			name: "duplicate keys are the same text",
			a:    `{"a":1,"a":2}`,
			b:    `{"a":1,"a":2}`,
			want: true,
		},
		{
			name: "the same key in two objects is not a duplicate",
			a:    `[{"a":1},{"a":1}]`,
			b:    `[ {"a": 1}, {"a": 1} ]`,
			want: true,
		},
		{
			name: "strings escaped differently are equal",
			a:    `{"\u0061":"\u00e9\n"}`,
			b:    "{\"a\":\"\u00e9\\n\"}",
			want: true,
		},
		{
			name: "text after the JSON is not JSON",
			a:    `{"a":1} x`,
			b:    `{"a":1}`,
			want: false,
		},
		{
			name: "whitespace alone is not JSON",
			a:    " ",
			b:    "",
			want: false,
		},
		{
			name: "number is not the string of that number",
			a:    `{"a":1}`,
			b:    `{"a":"1"}`,
			want: false,
		},
		{
			name: "null is not absent",
			a:    `{"a":null}`,
			b:    `{}`,
			want: false,
		},
		// When either side fails to parse, the function must fall back to a
		// plain string comparison instead of reporting a false match.
		{
			name: "both invalid but identical falls back to string equality",
			a:    "not json",
			b:    "not json",
			want: true,
		},
		{
			name: "both invalid and different",
			a:    "not json",
			b:    "also not json",
			want: false,
		},
		{
			name: "one side invalid is never equal",
			a:    `{"a":1}`,
			b:    "not json",
			want: false,
		},
		{
			name: "empty strings are equal via fallback",
			a:    "",
			b:    "",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := jsonSemanticEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("jsonSemanticEqual(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// jsonSemanticEqual must be symmetric, otherwise the plan modifier would
// behave differently depending on which side Terraform happens to pass.
func TestJSONSemanticEqualIsSymmetric(t *testing.T) {
	t.Parallel()

	pairs := [][2]string{
		{`{"a":1,"b":2}`, `{"b":2,"a":1}`},
		{`{"a":1}`, `{"a":2}`},
		{`{"a":1}`, "not json"},
		{"not json", "not json"},
		{`{"a":1,"a":2}`, `{"a":2}`},
		{`12345678901234567890`, `12345678901234567891`},
		{`1e2`, `100`},
	}

	for _, p := range pairs {
		ab := jsonSemanticEqual(p[0], p[1])
		ba := jsonSemanticEqual(p[1], p[0])
		if ab != ba {
			t.Errorf("asymmetric for (%q, %q): a==b is %v but b==a is %v", p[0], p[1], ab, ba)
		}
	}
}

func TestPlanModifyString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		state      types.String
		plan       types.String
		wantPlan   types.String
		wantReason string
	}{
		{
			name:       "semantically equal collapses plan onto state",
			state:      types.StringValue(`{"a":1,"b":2}`),
			plan:       types.StringValue(`{"b":2,"a":1}`),
			wantPlan:   types.StringValue(`{"a":1,"b":2}`),
			wantReason: "reordered keys must not produce a diff",
		},
		{
			name:       "genuinely different value is left alone",
			state:      types.StringValue(`{"a":1}`),
			plan:       types.StringValue(`{"a":2}`),
			wantPlan:   types.StringValue(`{"a":2}`),
			wantReason: "a real change must still show up as a diff",
		},
		{
			name:       "identical values are left alone",
			state:      types.StringValue(`{"a":1}`),
			plan:       types.StringValue(`{"a":1}`),
			wantPlan:   types.StringValue(`{"a":1}`),
			wantReason: "no change, nothing to suppress",
		},
		{
			name:       "null state (resource creation) is left alone",
			state:      types.StringNull(),
			plan:       types.StringValue(`{"a":1}`),
			wantPlan:   types.StringValue(`{"a":1}`),
			wantReason: "there is no prior value to compare against",
		},
		{
			name:       "unknown state is left alone",
			state:      types.StringUnknown(),
			plan:       types.StringValue(`{"a":1}`),
			wantPlan:   types.StringValue(`{"a":1}`),
			wantReason: "unknown state cannot be compared",
		},
		{
			name:       "null plan is left alone",
			state:      types.StringValue(`{"a":1}`),
			plan:       types.StringNull(),
			wantPlan:   types.StringNull(),
			wantReason: "removing the value is a real change",
		},
		{
			name:       "unknown plan is left alone",
			state:      types.StringValue(`{"a":1}`),
			plan:       types.StringUnknown(),
			wantPlan:   types.StringUnknown(),
			wantReason: "unknown plan cannot be compared",
		},
		{
			name:       "a large integer changed is a change",
			state:      types.StringValue(`{"id":12345678901234567890}`),
			plan:       types.StringValue(`{"id": 12345678901234567891}`),
			wantPlan:   types.StringValue(`{"id": 12345678901234567891}`),
			wantReason: "two integers float64 rounds to one are still two",
		},
		{
			name:       "invalid JSON on both sides with different text stays different",
			state:      types.StringValue("not json"),
			plan:       types.StringValue("also not json"),
			wantPlan:   types.StringValue("also not json"),
			wantReason: "non-JSON values fall back to string comparison",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := JSONSemanticEqual()

			req := planmodifier.StringRequest{
				StateValue: tt.state,
				PlanValue:  tt.plan,
			}
			resp := &planmodifier.StringResponse{PlanValue: tt.plan}

			m.PlanModifyString(context.Background(), req, resp)

			if !resp.PlanValue.Equal(tt.wantPlan) {
				t.Errorf("PlanValue = %v, want %v (%s)", resp.PlanValue, tt.wantPlan, tt.wantReason)
			}
			if resp.Diagnostics.HasError() {
				t.Errorf("unexpected diagnostics: %+v", resp.Diagnostics)
			}
		})
	}
}

func TestJSONSemanticEqualModifierDescriptions(t *testing.T) {
	t.Parallel()

	m := JSONSemanticEqual()
	ctx := context.Background()

	if m.Description(ctx) == "" {
		t.Error("Description() must not be empty")
	}
	if m.MarkdownDescription(ctx) == "" {
		t.Error("MarkdownDescription() must not be empty")
	}
}
