package check

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
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

func TestNewCheckResourceMetadata(t *testing.T) {
	t.Parallel()

	r := NewCheckResource()

	resp := &fwresource.MetadataResponse{}
	r.Metadata(context.Background(), fwresource.MetadataRequest{ProviderTypeName: "nodeping"}, resp)

	if want := "nodeping_check"; resp.TypeName != want {
		t.Errorf("TypeName = %q, want %q", resp.TypeName, want)
	}
}
