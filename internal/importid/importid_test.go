package importid

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

func TestParseAcceptsAPlainID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   string
	}{
		{name: "check", id: "201205050153W2Q4C-0J2HSIRF"},
		{name: "contact", id: "201205050153W2Q4C-BKPGH"},
		{name: "contact group", id: "201205050153W2Q4C-G-1ZIYU"},
		// Nothing about the ID is validated beyond the absence of a colon;
		// the API is the authority on whether it exists.
		{name: "unfamiliar shape", id: "whatever"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var diags diag.Diagnostics
			got, ok := Parse(tt.id, "check", &diags)

			if !ok {
				t.Fatalf("Parse(%q) rejected a plain ID", tt.id)
			}
			if got != tt.id {
				t.Errorf("Parse(%q) = %q, want it returned unchanged", tt.id, got)
			}
			if diags.HasError() {
				t.Errorf("Parse(%q) raised %v", tt.id, diags.Errors())
			}
		})
	}
}

// An empty ID would otherwise reach GET /<resources>/ -- the list endpoint --
// which answers 200 and decodes into an empty resource, so the import looks
// like it worked and leaves junk in state.
func TestParseRejectsAnEmptyID(t *testing.T) {
	t.Parallel()

	var diags diag.Diagnostics
	got, ok := Parse("", "check", &diags)

	if ok {
		t.Fatal("Parse(\"\") accepted an empty import ID")
	}
	if got != "" {
		t.Errorf("Parse(\"\") = %q, want empty", got)
	}
	if !diags.HasError() {
		t.Fatal("Parse(\"\") rejected the ID without saying why")
	}
	if detail := diags.Errors()[0].Detail(); !strings.Contains(detail, "<check_id>") {
		t.Errorf("message does not show what to pass instead:\n%s", detail)
	}
}

func TestParseRejectsTheSubAccountPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   string
		// Fragments the message has to carry for the reader to act on it.
		wantIn []string
	}{
		{
			name: "both halves present",
			id:   "201205050153W2Q4C:201205050153W2Q4C-0J2HSIRF",
			wantIn: []string{
				`customer_id = "201205050153W2Q4C"`,
				"terraform import nodeping_check.example 201205050153W2Q4C-0J2HSIRF",
			},
		},
		{
			// Still has to explain itself rather than suggest an empty alias.
			name: "empty customer id",
			id:   ":201205050153W2Q4C-0J2HSIRF",
			wantIn: []string{
				`customer_id = "SUBACCOUNT_ID"`,
				"terraform import nodeping_check.example 201205050153W2Q4C-0J2HSIRF",
			},
		},
		{
			name: "empty resource id",
			id:   "201205050153W2Q4C:",
			wantIn: []string{
				`customer_id = "201205050153W2Q4C"`,
				"terraform import nodeping_check.example RESOURCE_ID",
			},
		},
		{
			name: "more than one colon",
			id:   "a:b:c",
			wantIn: []string{
				"provider = nodeping.subaccount",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var diags diag.Diagnostics
			got, ok := Parse(tt.id, "check", &diags)

			if ok {
				t.Fatalf("Parse(%q) accepted the withdrawn SubAccount form", tt.id)
			}
			if got != "" {
				t.Errorf("Parse(%q) = %q, want an empty ID alongside the error", tt.id, got)
			}
			if !diags.HasError() {
				t.Fatalf("Parse(%q) rejected the ID without saying why", tt.id)
			}

			detail := diags.Errors()[0].Detail()
			for _, want := range tt.wantIn {
				if !strings.Contains(detail, want) {
					t.Errorf("Parse(%q) message is missing %q:\n%s", tt.id, want, detail)
				}
			}
		})
	}
}

// The message names the resource type so it can be pasted straight into a
// configuration.
func TestParseNamesTheResourceType(t *testing.T) {
	t.Parallel()

	for _, typeName := range []string{"check", "contact", "contactgroup"} {
		t.Run(typeName, func(t *testing.T) {
			t.Parallel()

			var diags diag.Diagnostics
			Parse("CUSTOMER:ID", typeName, &diags)

			if !diags.HasError() {
				t.Fatal("no error raised")
			}

			detail := diags.Errors()[0].Detail()
			want := "resource \"nodeping_" + typeName + "\" \"example\""
			if !strings.Contains(detail, want) {
				t.Errorf("message is missing %q:\n%s", want, detail)
			}
		})
	}
}
