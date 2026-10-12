package check

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
)

// autodiag goes out as the string NodePing documents, on create and update
// alike, and not at all when it is not known.
func TestAutoDiagIsSentAsAString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		autodiag types.Bool
		want     string
	}{
		{types.BoolValue(true), `"true"`},
		{types.BoolValue(false), `"false"`},
		{types.BoolUnknown(), ""},
		{types.BoolNull(), ""},
	}
	for _, tt := range tests {
		model := unset()
		model.AutoDiag = tt.autodiag
		r := &CheckResource{}
		var diags diag.Diagnostics

		create := requestJSON(t, r.buildCreateRequest(context.Background(), model, &diags))
		update := requestJSON(t, r.buildUpdateRequest(context.Background(), model, unset(), &diags))
		if diags.HasError() {
			t.Fatalf("build: %v", diags)
		}
		if create["autodiag"] != tt.want {
			t.Errorf("autodiag %s: create sent %q, want %q", tt.autodiag, create["autodiag"], tt.want)
		}
		if update["autodiag"] != tt.want {
			t.Errorf("autodiag %s: update sent %q, want %q", tt.autodiag, update["autodiag"], tt.want)
		}
	}
}

// NodePing answers autodiag = true with false on an account whose plan lacks
// Automated Diagnostics. An apply keeps the planned value, as keep does for a
// value an answer leaves out; it used to fail with "inconsistent result after
// apply". A refresh and an import read NodePing as it is, in either JSON
// shape.
func TestMapCheckToModelAutoDiag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		holds  modelHolds
		held   types.Bool
		answer interface{}
		want   types.Bool
	}{
		{"an apply of true answered false", thePlan, types.BoolValue(true), false, types.BoolValue(true)},
		{"an apply of true answered without it", thePlan, types.BoolValue(true), nil, types.BoolValue(true)},
		{"an apply of true answered true", thePlan, types.BoolValue(true), true, types.BoolValue(true)},
		{`an apply of true answered "true"`, thePlan, types.BoolValue(true), "true", types.BoolValue(true)},
		{"an apply of false answered false", thePlan, types.BoolValue(false), false, types.BoolValue(false)},
		{`an apply of false answered "false"`, thePlan, types.BoolValue(false), "false", types.BoolValue(false)},
		{"an apply of false answered true", thePlan, types.BoolValue(false), true, types.BoolValue(true)},
		{"an apply of a value not known answered false", thePlan, types.BoolUnknown(), false, types.BoolValue(false)},
		{"a refresh of true reading false", thePriorState, types.BoolValue(true), false, types.BoolValue(false)},
		{`a refresh of false reading "true"`, thePriorState, types.BoolValue(false), "true", types.BoolValue(true)},
		{"a refresh of false reading false", thePriorState, types.BoolValue(false), false, types.BoolValue(false)},
		{`an import reading "true"`, nothing, types.BoolNull(), "true", types.BoolValue(true)},
		{"an import reading false", nothing, types.BoolNull(), false, types.BoolValue(false)},
		{"an import reading nothing", nothing, types.BoolNull(), nil, types.BoolValue(false)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			model := unset()
			model.AutoDiag = tt.held
			answer := &client.Check{ID: "MOCK-1", Type: "HTTP", Enabled: "active", AutoDiag: tt.answer}
			var diags diag.Diagnostics
			(&CheckResource{}).mapCheckToModel(context.Background(), answer, model, tt.holds, &diags)
			if diags.HasError() {
				t.Fatalf("mapping raised %v", diags)
			}
			if !model.AutoDiag.Equal(tt.want) {
				t.Errorf("autodiag = %s, want %s", model.AutoDiag, tt.want)
			}
		})
	}
}
