package check

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nodeping/terraform-provider-nodeping/internal/client"
)

// The boolean decoding this package used to carry its own copy of now lives
// in checkattr.Bool, which checkattr_test covers; the cases that were only
// here moved across with it.

// NodePing leaves a parameter out of its answer when the check type does not
// use it. mapCheckToModel runs against the plan on create and update, and
// almost every check attribute is Optional without Computed, so Terraform
// demands the applied value equal the planned one exactly -- writing null
// over a planned value because the response was silent fails the apply with
// "Provider produced inconsistent result after apply".
//
// This cannot be covered by the acceptance suite: its mock echoes every
// request key straight back, so an API that answers with less than it was
// given is a shape those tests cannot express.
func TestMapCheckToModelKeepsPlannedValuesTheAPIOmits(t *testing.T) {
	t.Parallel()

	// A plan carrying a value for each attribute the old hand-written
	// guards protected, plus representatives of the newly mapped ones.
	planned := func() *CheckResourceModel {
		return &CheckResourceModel{
			Type:   types.StringValue("HTTP"),
			Target: types.StringValue("https://example.com"),
			Label:  types.StringValue("planned-label"),

			Regex:      types.BoolValue(true),
			Invert:     types.BoolValue(true),
			Follow:     types.BoolValue(true),
			IPv6:       types.BoolValue(true),
			Verify:     types.BoolValue(true),
			DNSRD:      types.BoolValue(true),
			StatusCode: types.Int64Value(200),

			Secure:     types.StringValue("false"),
			Database:   types.StringValue("app"),
			Query:      types.StringValue("SELECT 1"),
			HomeLoc:    types.StringValue("nam"),
			Transport:  types.StringValue("tcp"),
			DNSSection: types.StringValue("answer"),

			Interval:  types.Float64Value(15),
			Threshold: types.Int64Value(5),
			Sens:      types.Int64Value(2),
		}
	}

	// An answer with nothing but the envelope -- the shape a check type that
	// uses none of those parameters produces.
	bare := &client.Check{
		ID:         "MOCK-1",
		CustomerID: "CUST-1",
		Type:       "HTTP",
		Enabled:    "active",
	}

	r := &CheckResource{}
	var diags diag.Diagnostics

	got := planned()
	r.mapCheckToModel(context.Background(), bare, got, &diags)

	if diags.HasError() {
		t.Fatalf("mapping raised %v", diags.Errors())
	}

	want := planned()
	checks := []struct {
		name      string
		got, want attr.Value
	}{
		{"regex", got.Regex, want.Regex},
		{"invert", got.Invert, want.Invert},
		{"follow", got.Follow, want.Follow},
		{"ipv6", got.IPv6, want.IPv6},
		{"verify", got.Verify, want.Verify},
		{"dnsrd", got.DNSRD, want.DNSRD},
		{"statuscode", got.StatusCode, want.StatusCode},
		{"secure", got.Secure, want.Secure},
		{"database", got.Database, want.Database},
		{"query", got.Query, want.Query},
		{"homeloc", got.HomeLoc, want.HomeLoc},
		{"transport", got.Transport, want.Transport},
		{"dnssection", got.DNSSection, want.DNSSection},
		{"label", got.Label, want.Label},
		{"interval", got.Interval, want.Interval},
		{"threshold", got.Threshold, want.Threshold},
		{"sens", got.Sens, want.Sens},
	}

	for _, c := range checks {
		if !c.got.Equal(c.want) {
			t.Errorf("%s = %s, want the planned %s kept", c.name, c.got, c.want)
		}
	}
}

// The other half of the same rule: a value the API does return always beats
// the one already in the model, or drift would never surface.
func TestMapCheckToModelPrefersTheAPIOverState(t *testing.T) {
	t.Parallel()

	state := &CheckResourceModel{
		Type:     types.StringValue("MYSQL"),
		Database: types.StringValue("stale"),
		Label:    types.StringValue("stale-label"),
		Regex:    types.BoolValue(true),
	}

	drifted := &client.Check{
		ID:      "MOCK-1",
		Type:    "MYSQL",
		Enabled: "active",
		Label:   "changed-in-the-ui",
		Parameters: client.CheckParameters{
			Database: "changed-in-the-ui",
			Regex:    false,
		},
	}

	r := &CheckResource{}
	var diags diag.Diagnostics
	r.mapCheckToModel(context.Background(), drifted, state, &diags)

	if got, want := state.Database.ValueString(), "changed-in-the-ui"; got != want {
		t.Errorf("database = %q, want %q", got, want)
	}
	if got, want := state.Label.ValueString(), "changed-in-the-ui"; got != want {
		t.Errorf("label = %q, want %q", got, want)
	}
	if state.Regex.ValueBool() {
		t.Error("regex = true, want the API's false")
	}
}

// An import starts from a zero model, so there is nothing to keep and every
// attribute has to come from the response -- the case finding 1 was about.
func TestMapCheckToModelFillsAnEmptyModelOnImport(t *testing.T) {
	t.Parallel()

	imported := &client.Check{
		ID:      "MOCK-1",
		Type:    "MYSQL",
		Enabled: "active",
		Parameters: client.CheckParameters{
			Target:   "db.example.com",
			Database: "app",
			Query:    "SELECT 1",
			Port:     float64(3306),
		},
	}

	var model CheckResourceModel
	r := &CheckResource{}
	var diags diag.Diagnostics
	r.mapCheckToModel(context.Background(), imported, &model, &diags)

	if diags.HasError() {
		t.Fatalf("mapping raised %v", diags.Errors())
	}
	if got, want := model.Database.ValueString(), "app"; got != want {
		t.Errorf("database = %q, want %q", got, want)
	}
	if got, want := model.Query.ValueString(), "SELECT 1"; got != want {
		t.Errorf("query = %q, want %q", got, want)
	}
	if got, want := model.Port.ValueInt64(), int64(3306); got != want {
		t.Errorf("port = %d, want %d", got, want)
	}
	// A list the API said nothing about still has to carry its element type,
	// or writing the state back fails.
	if !model.Tags.IsNull() {
		t.Errorf("tags = %s, want null", model.Tags)
	}
	if model.Tags.ElementType(context.Background()) == nil {
		t.Error("tags lost its element type, which makes the state unwritable")
	}
}

func TestNormalizeURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "trailing slash removed", input: "https://example.com/", want: "https://example.com"},
		{name: "no trailing slash unchanged", input: "https://example.com", want: "https://example.com"},
		{name: "path with trailing slash", input: "https://example.com/api/", want: "https://example.com/api"},
		{name: "empty string", input: "", want: ""},
		{name: "only a slash", input: "/", want: ""},
		// TrimSuffix removes a single occurrence, not all of them.
		{name: "double trailing slash keeps one", input: "https://example.com//", want: "https://example.com/"},
		{name: "non-url passthrough", input: "1.2.3.4", want: "1.2.3.4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeURL(tt.input); got != tt.want {
				t.Errorf("normalizeURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNewCheckResourceMetadata(t *testing.T) {
	t.Parallel()

	r := NewCheckResource()

	resp := &fwresource.MetadataResponse{}
	r.Metadata(context.Background(), fwresource.MetadataRequest{ProviderTypeName: "nodeping"}, resp)

	if want := "nodeping_check"; resp.TypeName != want {
		t.Errorf("TypeName = %q, want %q", resp.TypeName, want)
	}
}
