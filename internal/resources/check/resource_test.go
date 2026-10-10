package check

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/datasources/checkattr"
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
// This cannot be covered by the acceptance suite: its mock answers with less
// than it was given only for an attribute the check's type does not store,
// and the plan refuses those before anything is sent (see refuseUnstored).
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
			// Not 2: a check without sens reads as 2 (checkattr.Sens), which
			// would pass whether or not the planned value were kept.
			Sens: types.Int64Value(3),
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
	r.mapCheckToModel(context.Background(), bare, got, thePlan, &diags)

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
	r.mapCheckToModel(context.Background(), drifted, state, thePriorState, &diags)

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
	r.mapCheckToModel(context.Background(), imported, &model, nothing, &diags)

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

// A check without sens reads as NodePing's default, 2 (checkattr.Sens), on an
// import and on a refresh alike. A check imported before that holds null in
// state, and its next refresh has to read 2, or a configuration that leaves
// sens out plans to write it. Only an apply's answer without sens keeps the
// planned value; see TestMapCheckToModelKeepsPlannedValuesTheAPIOmits.
func TestMapCheckToModelReadsMissingSensAsDefault(t *testing.T) {
	t.Parallel()

	noSens := &client.Check{
		ID:         "MOCK-1",
		Type:       "HTTP",
		Enabled:    "active",
		Parameters: client.CheckParameters{Target: "https://example.com/", Threshold: float64(5)},
	}

	tests := []struct {
		name  string
		holds modelHolds
		held  types.Int64
	}{
		{name: "import", holds: nothing, held: types.Int64Null()},
		{name: "refresh of a null sens", holds: thePriorState, held: types.Int64Null()},
		{name: "refresh of another sens", holds: thePriorState, held: types.Int64Value(3)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			model := CheckResourceModel{Sens: tt.held}
			r := &CheckResource{}
			var diags diag.Diagnostics
			r.mapCheckToModel(context.Background(), noSens, &model, tt.holds, &diags)

			if diags.HasError() {
				t.Fatalf("mapping raised %v", diags.Errors())
			}
			if want := types.Int64Value(2); !model.Sens.Equal(want) {
				t.Errorf("sens = %s, want %s", model.Sens, want)
			}
		})
	}
}

// tags is the configuration's own list and tags_all the merged one the API
// stores. Refreshing tags from the API would fold the provider's default_tags
// into it, and every plan would then propose to remove them again.
func TestMapCheckToModelLeavesTagsToTheConfiguration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	own, _ := types.ListValueFrom(ctx, types.StringType, []string{"own"})
	model := CheckResourceModel{Tags: own}

	r := &CheckResource{}
	var diags diag.Diagnostics
	r.mapCheckToModel(ctx, &client.Check{
		ID:   "MOCK-1",
		Type: "HTTP",
		Tags: []string{"managed-by-terraform", "own"},
	}, &model, thePriorState, &diags)
	if diags.HasError() {
		t.Fatalf("mapping raised %v", diags.Errors())
	}

	if !model.Tags.Equal(own) {
		t.Errorf("tags = %s, want the configured %s", model.Tags, own)
	}
	want, _ := types.ListValueFrom(ctx, types.StringType, []string{"managed-by-terraform", "own"})
	if !model.TagsAll.Equal(want) {
		t.Errorf("tags_all = %s, want the API's %s", model.TagsAll, want)
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

// A check with no warning period reaches the provider as "", as no field at
// all, or -- once saved in the web interface with the field left empty -- as 0.
// All of them must read as null: 0 in state would demand `warningdays = 0` in
// configuration, which the schema rejects. The rule lives in
// checkattr.OptionalWarningDays; this pins that the resource goes through it.
func TestMapCheckToModelWarningDays(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input interface{}
		want  types.Int64
	}{
		{name: "positive number", input: float64(21), want: types.Int64Value(21)},
		{name: "zero from the web interface", input: float64(0), want: types.Int64Null()},
		{name: "empty string from the API", input: "", want: types.Int64Null()},
		{name: "field absent", input: nil, want: types.Int64Null()},
		{name: "negative number", input: float64(-1), want: types.Int64Null()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := &CheckResource{}
			check := &client.Check{Parameters: client.CheckParameters{WarningDays: tt.input}}
			var model CheckResourceModel
			var diags diag.Diagnostics
			r.mapCheckToModel(context.Background(), check, &model, nothing, &diags)
			if diags.HasError() {
				t.Fatalf("mapCheckToModel: %v", diags)
			}
			if !model.WarningDays.Equal(tt.want) {
				t.Errorf("warningdays %#v read as %v, want %v", tt.input, model.WarningDays, tt.want)
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

func TestMergeTags(t *testing.T) {
	tests := []struct {
		name        string
		defaultTags []string
		configured  []string
		want        []string
	}{
		{
			name: "no tags at all yields an empty list, not nil",
			want: []string{},
		},
		{
			name:        "defaults come first, then the check's own",
			defaultTags: []string{"managed-by-terraform", "owner-team-sre"},
			configured:  []string{"website"},
			want:        []string{"managed-by-terraform", "owner-team-sre", "website"},
		},
		{
			name:       "no defaults configured leaves the check's tags untouched",
			configured: []string{"website", "critical"},
			want:       []string{"website", "critical"},
		},
		{
			name:        "a tag repeated in both appears once, in the default's position",
			defaultTags: []string{"managed-by-terraform", "shared"},
			configured:  []string{"shared", "website"},
			want:        []string{"managed-by-terraform", "shared", "website"},
		},
		{
			name:        "duplicates within one list collapse too",
			defaultTags: []string{"a", "a"},
			configured:  []string{"b", "b"},
			want:        []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeTags(tt.defaultTags, tt.configured)

			if got == nil {
				t.Fatal("mergeTags returned nil; tags_all is Computed and may not be null")
			}
			if len(got) != len(tt.want) {
				t.Fatalf("mergeTags() = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("mergeTags()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestMergeTagsDoesNotAliasInputs(t *testing.T) {
	defaultTags := []string{"managed-by-terraform"}
	configured := []string{"website"}

	got := mergeTags(defaultTags, configured)
	got[0] = "mutated"

	if defaultTags[0] != "managed-by-terraform" {
		t.Errorf("mergeTags aliased its defaultTags argument: %v", defaultTags)
	}
	if configured[0] != "website" {
		t.Errorf("mergeTags aliased its configuredTags argument: %v", configured)
	}
}

// A field's min, max and match are each optional: a null one is left out of
// the request, while 0 is a real bound and has to be sent.
func TestFieldsToAPI(t *testing.T) {
	t.Parallel()

	got := fieldsToAPI(map[string]checkattr.FieldModel{
		"A": {
			Name:  types.StringValue("status"),
			Min:   types.Float64Value(200),
			Max:   types.Float64Value(200),
			Match: types.StringNull(),
		},
		"B": {
			Name:  types.StringValue("errors"),
			Min:   types.Float64Value(0),
			Max:   types.Float64Null(),
			Match: types.StringValue("none"),
		},
	})

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"A":{"name":"status","min":200,"max":200},"B":{"name":"errors","min":0,"match":"none"}}`
	if string(body) != want {
		t.Errorf("fields = %s\nwant     %s", body, want)
	}

	if fieldsToAPI(nil) != nil {
		t.Error("no fields must leave the request's fields out")
	}
}

// fields is only refreshed from the API, never invented: a configuration
// that leaves it out of a check carrying fields has to see them in state, so
// the plan proposes removing them instead of silently dropping them on the
// next unrelated write.
func TestMapCheckToModelReadsFieldsTheConfigurationLacks(t *testing.T) {
	t.Parallel()

	check := &client.Check{
		ID:      "MOCK-1",
		Type:    "HTTP",
		Enabled: "active",
		Parameters: client.CheckParameters{
			Target: "https://example.com/variables.json",
			Fields: map[string]client.CheckField{
				"F7L814": {Name: "status", Min: float64(200), Max: float64(200)},
			},
		},
	}

	model := CheckResourceModel{Fields: types.MapNull(checkattr.FieldType)}
	r := &CheckResource{}
	var diags diag.Diagnostics
	r.mapCheckToModel(context.Background(), check, &model, thePriorState, &diags)

	if diags.HasError() {
		t.Fatalf("mapping raised %v", diags.Errors())
	}
	if model.Fields.IsNull() || len(model.Fields.Elements()) != 1 {
		t.Errorf("fields = %s, want the one field NodePing stores", model.Fields)
	}
}

// With ignore_mute, a check muted in NodePing plans the prior state's mute,
// but the schema default has by then made the framework mark label unknown on
// a check without one. Only when nothing else changes may the plan go back to
// the prior state as it is.
func TestUnchangedButForUnknowns(t *testing.T) {
	objectType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"label":  tftypes.String,
		"target": tftypes.String,
		"mute":   tftypes.Bool,
		"tags":   tftypes.List{ElementType: tftypes.String},
	}}
	object := func(label, target, mute, tags tftypes.Value) tftypes.Value {
		return tftypes.NewValue(objectType, map[string]tftypes.Value{
			"label": label, "target": target, "mute": mute, "tags": tags,
		})
	}
	str := func(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }
	unknownString := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	nullString := tftypes.NewValue(tftypes.String, nil)
	muted := tftypes.NewValue(tftypes.Bool, true)
	unmuted := tftypes.NewValue(tftypes.Bool, false)
	nullBool := tftypes.NewValue(tftypes.Bool, nil)
	tags := func(elements ...tftypes.Value) tftypes.Value {
		return tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, elements)
	}

	prior := object(str("example.com"), str("https://example.com"), muted, tags(str("web")))

	tests := []struct {
		name   string
		plan   tftypes.Value
		config tftypes.Value
		want   bool
	}{
		{
			name:   "nothing changed",
			plan:   prior,
			config: object(nullString, str("https://example.com"), nullBool, tags(str("web"))),
			want:   true,
		},
		{
			name:   "only a label the configuration leaves unset is unknown",
			plan:   object(unknownString, str("https://example.com"), muted, tags(str("web"))),
			config: object(nullString, str("https://example.com"), nullBool, tags(str("web"))),
			want:   true,
		},
		{
			name:   "the target changes too",
			plan:   object(unknownString, str("https://example.org"), muted, tags(str("web"))),
			config: object(nullString, str("https://example.org"), nullBool, tags(str("web"))),
			want:   false,
		},
		{
			name:   "the configuration's own label is unknown",
			plan:   object(unknownString, str("https://example.com"), muted, tags(str("web"))),
			config: object(unknownString, str("https://example.com"), nullBool, tags(str("web"))),
			want:   false,
		},
		{
			name:   "a configured tag is unknown",
			plan:   object(str("example.com"), str("https://example.com"), muted, tags(unknownString)),
			config: object(nullString, str("https://example.com"), nullBool, tags(unknownString)),
			want:   false,
		},
		{
			name:   "mute changes",
			plan:   object(str("example.com"), str("https://example.com"), unmuted, tags(str("web"))),
			config: object(nullString, str("https://example.com"), nullBool, tags(str("web"))),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unchangedButForUnknowns(tt.plan, tt.config, prior); got != tt.want {
				t.Errorf("unchangedButForUnknowns() = %v, want %v", got, tt.want)
			}
		})
	}
}
