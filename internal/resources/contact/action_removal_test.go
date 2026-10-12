package contact

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// withAction returns the address with its action set to v.
func withAction(a AddressModel, v types.String) AddressModel {
	a.Action = v
	return a
}

// NodePing cannot clear a webhook's action, so ModifyPlan refuses a plan that
// keeps an address but drops its action. It goes by the address each block
// keeps, as plannedAddressIDs planned it.
func TestAddressesDroppingAction(t *testing.T) {
	t.Parallel()

	post := types.StringValue("post")
	hook := prior("id-1", "webhook", "https://hooks.example.com/one")[0]
	email := prior("id-2", "email", "a@x")[0]
	hookWith := func(v types.String) AddressModel { return withAction(hook, v) }
	known := func(ids ...string) []types.String {
		out := make([]types.String, len(ids))
		for i, id := range ids {
			if id == "" {
				out[i] = types.StringUnknown()
				continue
			}
			out[i] = types.StringValue(id)
		}
		return out
	}

	tests := []struct {
		name    string
		prior   []AddressModel
		planned []AddressModel
		ids     []types.String
		want    []int
	}{
		{"action removed", []AddressModel{hookWith(post)}, []AddressModel{hookWith(types.StringNull())}, known("id-1"), []int{0}},
		{"action changed", []AddressModel{hookWith(post)}, []AddressModel{hookWith(types.StringValue("get"))}, known("id-1"), nil},
		{"action kept", []AddressModel{hookWith(post)}, []AddressModel{hookWith(post)}, known("id-1"), nil},
		{"action not known until apply", []AddressModel{hookWith(post)}, []AddressModel{hookWith(types.StringUnknown())}, known("id-1"), nil},
		{"no action before or after", []AddressModel{hookWith(types.StringNull())}, []AddressModel{hookWith(types.StringNull())}, known("id-1"), nil},
		{"action added", []AddressModel{hookWith(types.StringNull())}, []AddressModel{hookWith(post)}, known("id-1"), nil},
		{"a new address in its place", []AddressModel{hookWith(post)}, []AddressModel{hookWith(types.StringNull())}, known(""), nil},
		{
			// NodePing stores any string, " " included, and keeps it too.
			"an action set to a space outside Terraform, removed",
			[]AddressModel{hookWith(types.StringValue(" "))},
			[]AddressModel{hookWith(types.StringNull())},
			known("id-1"),
			[]int{0},
		},
		{
			"the address that moved is the one checked",
			[]AddressModel{hookWith(post), email},
			[]AddressModel{email, hookWith(types.StringNull())},
			known("id-2", "id-1"),
			[]int{1},
		},
		{
			"only the address that drops it",
			[]AddressModel{hookWith(post), withAction(email, post)},
			[]AddressModel{hookWith(post), withAction(email, types.StringNull())},
			known("id-1", "id-2"),
			[]int{1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := addressesDroppingAction(tt.prior, tt.planned, tt.ids); fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("addressesDroppingAction = %v, want %v", got, tt.want)
			}
		})
	}
}
