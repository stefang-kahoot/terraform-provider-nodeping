package contact

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// withData returns the address with its data set to v.
func withData(a AddressModel, v types.String) AddressModel {
	a.Data = v
	return a
}

// NodePing cannot clear an address's data, so ModifyPlan refuses a plan that
// keeps an address but drops its data. It only goes by the address each block
// keeps, as plannedAddressIDs planned it.
func TestAddressesDroppingData(t *testing.T) {
	t.Parallel()

	body := types.StringValue(`{"text":"{label}"}`)
	hook := prior("id-1", "webhook", "https://hooks.example.com/one")[0]
	email := prior("id-2", "email", "a@x")[0]
	hookWith := func(v types.String) AddressModel { return withData(hook, v) }
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
		{"data removed", []AddressModel{hookWith(body)}, []AddressModel{hookWith(types.StringNull())}, known("id-1"), []int{0}},
		{"data set to an empty string", []AddressModel{hookWith(body)}, []AddressModel{hookWith(types.StringValue(""))}, known("id-1"), []int{0}},
		{"data changed", []AddressModel{hookWith(body)}, []AddressModel{hookWith(types.StringValue("other"))}, known("id-1"), nil},
		{"data set to a space, which replaces it", []AddressModel{hookWith(body)}, []AddressModel{hookWith(types.StringValue(" "))}, known("id-1"), nil},
		{"data not known until apply", []AddressModel{hookWith(body)}, []AddressModel{hookWith(types.StringUnknown())}, known("id-1"), nil},
		{"no data before or after", []AddressModel{hookWith(types.StringNull())}, []AddressModel{hookWith(types.StringNull())}, known("id-1"), nil},
		{"data added", []AddressModel{hookWith(types.StringNull())}, []AddressModel{hookWith(body)}, known("id-1"), nil},
		{"a new address in its place", []AddressModel{hookWith(body)}, []AddressModel{hookWith(types.StringNull())}, known(""), nil},
		{
			"the address that moved is the one checked",
			[]AddressModel{hookWith(body), withData(email, types.StringNull())},
			[]AddressModel{withData(email, types.StringNull()), hookWith(types.StringNull())},
			known("id-2", "id-1"),
			[]int{1},
		},
		{
			"only the address that drops it",
			[]AddressModel{hookWith(body), withData(email, body)},
			[]AddressModel{hookWith(body), withData(email, types.StringNull())},
			known("id-1", "id-2"),
			[]int{1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := addressesDroppingData(tt.prior, tt.planned, tt.ids); fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("addressesDroppingData = %v, want %v", got, tt.want)
			}
		})
	}
}
