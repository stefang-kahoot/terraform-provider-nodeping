package check

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// The NodePing API is inconsistent about how it encodes booleans: some
// endpoints return real JSON booleans, others strings, others 0/1 numbers.
// parseBoolInterface has to absorb all of them.
func TestParseBoolInterface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input interface{}
		want  bool
	}{
		{name: "nil", input: nil, want: false},

		{name: "bool true", input: true, want: true},
		{name: "bool false", input: false, want: false},

		{name: `string "true"`, input: "true", want: true},
		{name: `string "1"`, input: "1", want: true},
		{name: `string "false"`, input: "false", want: false},
		{name: `string "0"`, input: "0", want: false},
		{name: "empty string", input: "", want: false},
		// Deliberately case-sensitive: the API only ever sends lowercase.
		{name: `string "True" is not truthy`, input: "True", want: false},
		{name: "arbitrary string", input: "yes", want: false},

		// encoding/json decodes every number into float64.
		{name: "float64 1", input: float64(1), want: true},
		{name: "float64 0", input: float64(0), want: false},
		{name: "float64 negative", input: float64(-1), want: true},
		{name: "float64 fractional", input: float64(0.5), want: true},

		{name: "int 1", input: 1, want: true},
		{name: "int 0", input: 0, want: false},

		// Types the switch does not handle must fall through to false
		// rather than panic.
		{name: "unhandled type slice", input: []string{"true"}, want: false},
		{name: "unhandled type map", input: map[string]bool{"v": true}, want: false},
		{name: "unhandled type int64", input: int64(1), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := parseBoolInterface(tt.input); got != tt.want {
				t.Errorf("parseBoolInterface(%#v) = %v, want %v", tt.input, got, tt.want)
			}
		})
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
// configuration, which the schema rejects.
func TestWarningDaysFromAPI(t *testing.T) {
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
			if got := warningDaysFromAPI(tt.input); !got.Equal(tt.want) {
				t.Errorf("warningDaysFromAPI(%#v) = %v, want %v", tt.input, got, tt.want)
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
