package check

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// An answer is suspect when its modified is no later than the check's before
// the update, and only when there is a modified to tell by.
func TestAnswerMayBeStale(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		before   types.Int64
		modified int64
		want     bool
	}{
		{"the same modified as before", types.Int64Value(1000), 1000, true},
		{"an earlier modified", types.Int64Value(1000), 999, true},
		{"no modified in the answer", types.Int64Value(1000), 0, true},
		{"a later modified", types.Int64Value(1000), 1001, false},
		{"no modified before", types.Int64Value(0), 1000, false},
		{"a null modified before", types.Int64Null(), 1000, false},
		{"an unknown modified before", types.Int64Unknown(), 1000, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := answerMayBeStale(tt.before, &client.Check{Modified: tt.modified}); got != tt.want {
				t.Errorf("answerMayBeStale(%s, %d) = %v, want %v", tt.before, tt.modified, got, tt.want)
			}
		})
	}
}

// An applied value agrees with the plan when it equals it wherever the plan
// has a known value, as Terraform requires of an apply.
func TestAgreesWithPlan(t *testing.T) {
	t.Parallel()

	listType := tftypes.List{ElementType: tftypes.String}
	objectType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"label": tftypes.String,
		"tags":  listType,
	}}
	object := func(label, tags tftypes.Value) tftypes.Value {
		return tftypes.NewValue(objectType, map[string]tftypes.Value{"label": label, "tags": tags})
	}
	str := func(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }
	list := func(elements ...tftypes.Value) tftypes.Value { return tftypes.NewValue(listType, elements) }
	unknownString := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	unknownList := tftypes.NewValue(listType, tftypes.UnknownValue)
	nullString := tftypes.NewValue(tftypes.String, nil)

	tests := []struct {
		name    string
		plan    tftypes.Value
		applied tftypes.Value
		want    bool
	}{
		{"equal", object(str("a"), list(str("x"))), object(str("a"), list(str("x"))), true},
		{"a known value differs", object(str("a"), list(str("x"))), object(str("b"), list(str("x"))), false},
		{"a list element differs", object(str("a"), list(str("x"))), object(str("a"), list(str("y"))), false},
		{"null planned, a value applied", object(nullString, list()), object(str("a"), list()), false},
		{"an unknown takes any value", object(unknownString, list(str("x"))), object(str("b"), list(str("x"))), true},
		{"an unknown list takes any list", object(str("a"), unknownList), object(str("a"), list(str("y"), str("z"))), true},
		{"an unknown element takes any value", object(str("a"), list(unknownString)), object(str("a"), list(str("y"))), true},
		{"an unknown element does not take a longer list", object(str("a"), list(unknownString)), object(str("a"), list(str("y"), str("z"))), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := agreesWithPlan(tt.plan, tt.applied); got != tt.want {
				t.Errorf("agreesWithPlan() = %v, want %v", got, tt.want)
			}
		})
	}
}

// givesThePlan judges a model the way Terraform judges the state Update
// reports, through the resource's own schema.
func TestGivesThePlan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := &CheckResource{}

	answer := func(label string) *client.Check {
		return &client.Check{
			ID:         "MOCK-1",
			Type:       "HTTP",
			Label:      label,
			Enabled:    "active",
			Modified:   1000,
			Parameters: client.CheckParameters{Target: "https://example.com"},
		}
	}
	model := func(label string) CheckResourceModel {
		var m CheckResourceModel
		var diags diag.Diagnostics
		r.mapCheckToModel(ctx, answer(label), &m, nothing, &diags)
		if diags.HasError() {
			t.Fatalf("mapping raised %v", diags.Errors())
		}
		return m
	}
	planOf := func(m CheckResourceModel) tfsdk.Plan {
		plan := tfsdk.Plan{Schema: CheckSchema()}
		if diags := plan.Set(ctx, &m); diags.HasError() {
			t.Fatalf("setting the plan raised %v", diags.Errors())
		}
		return plan
	}

	planned := model("renamed")
	if !givesThePlan(ctx, planOf(planned), model("renamed")) {
		t.Error("the planned values do not give the plan")
	}
	if givesThePlan(ctx, planOf(planned), model("before")) {
		t.Error("the label as it was before the update gives the plan")
	}

	planned.Label = types.StringUnknown()
	if !givesThePlan(ctx, planOf(planned), model("before")) {
		t.Error("a label planned unknown does not take NodePing's")
	}
}

// Mapped onto the plan, an answer gets the planned modified and state back,
// as Update reports them; NodePing's answer has its own of both.
func TestUpdatedModelKeepsThePlannedComputedValues(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := &CheckResource{}

	var plan CheckResourceModel
	var diags diag.Diagnostics
	r.mapCheckToModel(ctx, &client.Check{
		ID: "MOCK-1", Type: "HTTP", Label: "renamed", Enabled: "active", Modified: 1000, State: 1,
		Parameters: client.CheckParameters{Target: "https://example.com"},
	}, &plan, nothing, &diags)

	got := r.updatedModel(ctx, &client.Check{
		ID: "MOCK-1", Type: "HTTP", Label: "renamed", Enabled: "active", Modified: 2000, State: 0,
		Parameters: client.CheckParameters{Target: "https://example.com/"},
	}, plan, false, &diags)

	if diags.HasError() {
		t.Fatalf("mapping raised %v", diags.Errors())
	}
	if got.Modified.ValueInt64() != 1000 || got.State.ValueInt64() != 1 {
		t.Errorf("modified, state = %s, %s; want the planned 1000, 1", got.Modified, got.State)
	}
	if got.Target.ValueString() != "https://example.com" {
		t.Errorf("target = %s, want the planned one without NodePing's trailing slash", got.Target)
	}
	if plan.Modified.ValueInt64() != 1000 || plan.Label.ValueString() != "renamed" {
		t.Errorf("the plan passed in changed: modified %s, label %s", plan.Modified, plan.Label)
	}
}

// readUpdateBack takes the first read that shows the update: one with a later
// modified, even if it differs from the plan (that fails the apply as it
// always has), or one with the planned values. If no read does, it fails
// naming the check, after one read per wait.
func TestReadUpdateBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	waits := []time.Duration{time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond}

	tests := []struct {
		name string
		// The check as NodePing shows it on every read.
		modified int64
		label    string

		wantLabel string
		wantReads int
		wantError string
	}{
		{name: "a later modified", modified: 2000, label: "renamed", wantLabel: "renamed", wantReads: 1},
		{name: "a later modified, not the planned values", modified: 2000, label: "other", wantLabel: "other", wantReads: 1},
		{name: "the planned values, the old modified", modified: 1000, label: "renamed", wantLabel: "renamed", wantReads: 1},
		{name: "the check as it was before", modified: 1000, label: "before", wantReads: 3, wantError: `MOCK-1 ("renamed")`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := testutil.NewMockNodePingServer()
			t.Cleanup(mock.Close)
			mock.AddCheck("MOCK-1", map[string]interface{}{
				"_id":        "MOCK-1",
				"type":       "HTTP",
				"label":      tt.label,
				"enable":     "active",
				"modified":   tt.modified,
				"parameters": map[string]interface{}{"target": "https://example.com"},
			})
			r := &CheckResource{client: client.NewClient(client.ClientConfig{
				APIToken:      "test-token",
				BaseURL:       mock.URL(),
				RateLimit:     1000,
				ReadBackWaits: waits,
			})}

			var plan CheckResourceModel
			var diags diag.Diagnostics
			r.mapCheckToModel(ctx, &client.Check{
				ID: "MOCK-1", Type: "HTTP", Label: "renamed", Enabled: "active", Modified: 1000,
				Parameters: client.CheckParameters{Target: "https://example.com"},
			}, &plan, nothing, &diags)
			planned := tfsdk.Plan{Schema: CheckSchema()}
			diags.Append(planned.Set(ctx, &plan)...)
			if diags.HasError() {
				t.Fatalf("setting up the plan raised %v", diags.Errors())
			}

			got := r.readUpdateBack(ctx, planned, plan, "MOCK-1", "renamed", 1000, false, &diags)

			reads := 0
			for _, req := range mock.Requests() {
				if req.Method == "GET" && req.Path == "/checks/MOCK-1" {
					reads++
				}
			}
			if reads != tt.wantReads {
				t.Errorf("read the check %d times, want %d", reads, tt.wantReads)
			}
			if tt.wantError != "" {
				if !diags.HasError() || !strings.Contains(diags.Errors()[0].Detail(), tt.wantError) {
					t.Errorf("diagnostics = %v, want an error naming %s", diags, tt.wantError)
				}
				return
			}
			if diags.HasError() {
				t.Fatalf("unexpected error: %v", diags.Errors())
			}
			if got.Label.ValueString() != tt.wantLabel {
				t.Errorf("label = %s, want %q", got.Label, tt.wantLabel)
			}
			if got.Modified.ValueInt64() != 1000 {
				t.Errorf("modified = %s, want the planned 1000", got.Modified)
			}
		})
	}
}
