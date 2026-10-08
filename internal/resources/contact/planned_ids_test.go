package contact

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
)

// prior builds state addresses from (id, type, address) trios.
func prior(trios ...string) []AddressModel {
	out := make([]AddressModel, 0, len(trios)/3)
	for len(trios) >= 3 {
		out = append(out, AddressModel{
			ID:      types.StringValue(trios[0]),
			Type:    types.StringValue(trios[1]),
			Address: types.StringValue(trios[2]),
		})
		trios = trios[3:]
	}
	return out
}

// plannedWithIDs builds planned blocks from (id, type, address) trios; an
// empty id is a block planned with an unknown ID.
func plannedWithIDs(trios ...string) []AddressModel {
	out := prior(trios...)
	for i := range out {
		if out[i].ID.ValueString() == "" {
			out[i].ID = types.StringUnknown()
		}
	}
	return out
}

// shown renders planned IDs with "?" for unknown.
func shown(ids []types.String) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id.IsUnknown() {
			out = append(out, "?")
			continue
		}
		out = append(out, id.ValueString())
	}
	return out
}

func TestPlannedAddressIDs(t *testing.T) {
	t.Parallel()

	unknownAddress := func(addrType string) AddressModel {
		return AddressModel{Type: types.StringValue(addrType), Address: types.StringUnknown()}
	}

	tests := []struct {
		name    string
		prior   []AddressModel
		planned []AddressModel
		want    []string
	}{
		{
			name:    "unchanged addresses keep their IDs",
			prior:   prior("id-1", "email", "a@x", "id-2", "sms", "+1555"),
			planned: planned("email", "a@x", "sms", "+1555"),
			want:    []string{"id-1", "id-2"},
		},
		{
			// Finding 26: the new block used to plan null, not unknown.
			name:    "an added address is unknown, the others keep theirs",
			prior:   prior("id-1", "email", "a@x", "id-2", "sms", "+1555"),
			planned: planned("email", "a@x", "sms", "+1555", "email", "c@x"),
			want:    []string{"id-1", "id-2", "?"},
		},
		{
			// Finding 27: b used to inherit a's ID by position.
			name:    "removing the first address leaves the second its own ID",
			prior:   prior("id-1", "email", "a@x", "id-2", "email", "b@x"),
			planned: planned("email", "b@x"),
			want:    []string{"id-2"},
		},
		{
			name:    "swapped addresses take their IDs with them",
			prior:   prior("id-1", "email", "a@x", "id-2", "email", "b@x"),
			planned: planned("email", "b@x", "email", "a@x"),
			want:    []string{"id-2", "id-1"},
		},
		{
			name:    "an address edited in place keeps its position's ID",
			prior:   prior("id-1", "email", "a@x", "id-2", "webhook", "https://x/one"),
			planned: planned("email", "a@x", "webhook", "https://x/two"),
			want:    []string{"id-1", "id-2"},
		},
		{
			name:    "an address whose type changed is a new address",
			prior:   prior("id-1", "email", "a@x", "id-2", "webhook", "https://x/one"),
			planned: planned("email", "a@x", "slack", "https://x/one"),
			want:    []string{"id-1", "?"},
		},
		{
			// Matching by value runs over every block before any falls back to
			// its position. Otherwise the new first block would take a's ID by
			// position, and a, moved down, b's.
			name:    "a moved address claims its ID before a neighbour can take it by position",
			prior:   prior("id-1", "email", "a@x", "id-2", "email", "b@x"),
			planned: planned("email", "c@x", "email", "a@x"),
			want:    []string{"?", "id-1"},
		},
		{
			name:    "identical addresses claim identical IDs in order",
			prior:   prior("id-1", "email", "d@x", "id-2", "email", "d@x"),
			planned: planned("email", "d@x", "email", "d@x"),
			want:    []string{"id-1", "id-2"},
		},
		{
			name:    "one identical address more than before is new",
			prior:   prior("id-1", "email", "d@x"),
			planned: planned("email", "d@x", "email", "d@x"),
			want:    []string{"id-1", "?"},
		},
		{
			name:    "an address not known until apply keeps its position's ID",
			prior:   prior("id-1", "email", "a@x"),
			planned: []AddressModel{unknownAddress("email")},
			want:    []string{"id-1"},
		},
		{
			name:    "an address not known until apply under another type is new",
			prior:   prior("id-1", "email", "a@x"),
			planned: []AddressModel{unknownAddress("sms")},
			want:    []string{"?"},
		},
		{
			name:  "a type not known until apply is new",
			prior: prior("id-1", "email", "a@x"),
			planned: []AddressModel{{
				Type:    types.StringUnknown(),
				Address: types.StringValue("a@x"),
			}},
			want: []string{"?"},
		},
		{
			name:    "a contact without addresses gets new ones",
			prior:   nil,
			planned: planned("email", "a@x"),
			want:    []string{"?"},
		},
		{
			name:    "no blocks, no IDs",
			prior:   prior("id-1", "email", "a@x"),
			planned: nil,
			want:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := shown(plannedAddressIDs(tt.prior, tt.planned)); !equal(got, tt.want) {
				t.Errorf("planned IDs = %v, want %v", got, tt.want)
			}
		})
	}
}

// A block planned with a known ID was sent to NodePing under that ID, so it
// has to come back with it. Matching by value alone cannot tell identical
// addresses apart, and hands them out in ID order.
func TestMapAddressesToModelMatchesByIDFirst(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		plan []AddressModel
		api  map[string]client.ContactAddress
		want []string
	}{
		{
			name: "identical addresses keep the IDs they were planned with",
			plan: plannedWithIDs("id-2", "email", "d@x", "id-1", "email", "d@x"),
			api: apiAddresses(
				"id-1", "email", "d@x",
				"id-2", "email", "d@x",
			),
			want: []string{"id-2", "id-1"},
		},
		{
			name: "an address keeps its ID beside a new identical one",
			plan: plannedWithIDs("id-9", "email", "d@x", "", "email", "d@x"),
			api: apiAddresses(
				"id-1", "email", "d@x",
				"id-9", "email", "d@x",
			),
			want: []string{"id-9", "id-1"},
		},
		{
			name: "an address edited in place keeps its ID",
			plan: plannedWithIDs("id-2", "email", "new@x", "id-1", "email", "a@x"),
			api: apiAddresses(
				"id-1", "email", "a@x",
				"id-2", "email", "new@x",
			),
			want: []string{"id-2", "id-1"},
		},
		{
			// A read after an address was deleted and added again outside
			// Terraform: the prior state's ID is gone, the value is not.
			name: "an ID the API no longer has falls back to the value",
			plan: plannedWithIDs("id-1", "email", "a@x", "id-2", "email", "b@x"),
			api: apiAddresses(
				"id-2", "email", "b@x",
				"id-3", "email", "a@x",
			),
			want: []string{"id-3", "id-2"},
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
