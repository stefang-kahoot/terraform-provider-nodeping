package check

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nodeping/terraform-provider-nodeping/internal/client"
)

// The boolean decoding this package used to carry its own copy of now lives
// in checkattr.Bool, which checkattr_test covers; the cases that were only
// here moved across with it.

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
			r.mapCheckToModel(context.Background(), check, &model, &diags)
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
