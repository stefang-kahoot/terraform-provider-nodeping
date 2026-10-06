package contact

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nodeping/terraform-provider-nodeping/internal/client"
)

// `address` is an ordered list block, so state.address[i] has to stay the
// address the configuration wrote at position i. The API answers with a map
// and Go randomises map iteration, so every case here runs repeatedly: a
// single pass can agree with the expectation by luck.
const orderingRuns = 25

func planned(pairs ...string) []AddressModel {
	out := make([]AddressModel, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, AddressModel{
			Type:    types.StringValue(pairs[i]),
			Address: types.StringValue(pairs[i+1]),
		})
	}
	return out
}

func apiAddresses(trios ...string) map[string]client.ContactAddress {
	out := make(map[string]client.ContactAddress, len(trios)/3)
	for i := 0; i < len(trios); i += 3 {
		out[trios[i]] = client.ContactAddress{Type: trios[i+1], Address: trios[i+2]}
	}
	return out
}

func gotIDs(t *testing.T, got []AddressModel) []string {
	t.Helper()
	out := make([]string, 0, len(got))
	for _, a := range got {
		out = append(out, a.ID.ValueString())
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestMapAddressesToModelOrdering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		plan []AddressModel
		api  map[string]client.ContactAddress
		want []string
	}{
		{
			name: "plan order wins over ID order",
			plan: planned("email", "c@x", "email", "a@x", "email", "b@x"),
			api: apiAddresses(
				"id-1", "email", "a@x",
				"id-2", "email", "b@x",
				"id-3", "email", "c@x",
			),
			want: []string{"id-3", "id-1", "id-2"},
		},
		{
			name: "no plan falls back to ID order",
			plan: nil,
			api: apiAddresses(
				"id-3", "email", "c@x",
				"id-1", "email", "a@x",
				"id-2", "email", "b@x",
			),
			want: []string{"id-1", "id-2", "id-3"},
		},
		{
			name: "the same address under two types is disambiguated",
			plan: planned("sms", "shared", "email", "shared"),
			api: apiAddresses(
				"id-1", "email", "shared",
				"id-2", "sms", "shared",
			),
			want: []string{"id-2", "id-1"},
		},
		{
			name: "two identical blocks claim one ID each",
			plan: planned("email", "dup@x", "email", "dup@x"),
			api: apiAddresses(
				"id-1", "email", "dup@x",
				"id-2", "email", "dup@x",
			),
			want: []string{"id-1", "id-2"},
		},
		{
			name: "an address added outside Terraform lands last",
			plan: planned("email", "a@x"),
			api: apiAddresses(
				"id-1", "email", "a@x",
				"id-2", "email", "added-in-the-ui@x",
			),
			want: []string{"id-1", "id-2"},
		},
		{
			name: "fewer addresses than planned blocks does not pad",
			plan: planned("email", "a@x", "email", "b@x"),
			api:  apiAddresses("id-1", "email", "a@x"),
			want: []string{"id-1"},
		},
		{
			// The quiet one. If the renamed address is appended rather than
			// left in the position that owns its ID, every later block shifts
			// up one and the next update writes each block's address under
			// its neighbour's ID -- repointing any contact group that names
			// them, since groups reference address IDs.
			name: "an address renamed outside Terraform keeps its position",
			plan: planned("email", "a@x", "email", "b@x"),
			api: apiAddresses(
				"id-1", "email", "RENAMED@x",
				"id-2", "email", "b@x",
			),
			want: []string{"id-1", "id-2"},
		},
		{
			name: "two renames keep both positions",
			plan: planned("email", "a@x", "email", "b@x", "email", "c@x"),
			api: apiAddresses(
				"id-1", "email", "RENAMED-1@x",
				"id-2", "email", "b@x",
				"id-3", "email", "RENAMED-3@x",
			),
			want: []string{"id-1", "id-2", "id-3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for run := 0; run < orderingRuns; run++ {
				var diags diag.Diagnostics
				got := mapAddressesToModel(context.Background(), tt.api, tt.plan, &diags)

				if diags.HasError() {
					t.Fatalf("run %d raised %v", run, diags.Errors())
				}
				if ids := gotIDs(t, got); !equal(ids, tt.want) {
					t.Fatalf("run %d: ids = %v, want %v", run, ids, tt.want)
				}
			}
		})
	}
}

// Whatever the plan says, the result is always a permutation of what the API
// returned: nothing invented, nothing dropped, nothing emitted twice.
func TestMapAddressesToModelNeverDropsOrDuplicates(t *testing.T) {
	t.Parallel()

	api := apiAddresses(
		"id-1", "email", "a@x",
		"id-2", "sms", "+1555",
		"id-3", "webhook", "https://x/y",
		"id-4", "email", "a@x",
	)

	plans := [][]AddressModel{
		nil,
		planned("email", "a@x"),
		planned("email", "a@x", "email", "a@x"),
		planned("webhook", "https://x/y", "email", "nope@x"),
		planned("email", "nope@x", "email", "also-nope@x", "email", "still-nope@x"),
	}

	for i, plan := range plans {
		var diags diag.Diagnostics
		got := mapAddressesToModel(context.Background(), api, plan, &diags)

		if len(got) != len(api) {
			t.Errorf("plan %d: got %d addresses, want all %d", i, len(got), len(api))
		}
		seen := make(map[string]bool, len(got))
		for _, a := range got {
			id := a.ID.ValueString()
			if seen[id] {
				t.Errorf("plan %d: %s emitted twice", i, id)
			}
			seen[id] = true
			if _, ok := api[id]; !ok {
				t.Errorf("plan %d: %s was never returned by the API", i, id)
			}
		}
	}
}

func TestMapAddressesToModelEmptyAPIReturnsNil(t *testing.T) {
	t.Parallel()

	var diags diag.Diagnostics
	if got := mapAddressesToModel(context.Background(), nil, planned("email", "a@x"), &diags); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}
