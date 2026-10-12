package contact

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// NodePing stores whatever priority an update sends and keeps it when the
// update leaves it out, so removing it went nowhere: the apply failed with
// ".address[0].priority: was null, but now cty.NumberIntVal(1)" for a removed
// priority of 1 (#33). null is the one value that leaves the address holding
// none.
func TestAddressRequestPriority(t *testing.T) {
	t.Parallel()

	const (
		key      = `"address":"FAKE-pushover-user-key","type":"pushover"`
		flagsOff = `"suppressup":false,"suppressdown":false,"suppressfirst":false,"suppressdiag":false,"suppressall":false`
	)

	withPriority := func(p types.Int64) AddressModel {
		a := addressBlock("pushover", "FAKE-pushover-user-key")
		a.Priority = p
		return a
	}
	none := withPriority(types.Int64Null())
	two := withPriority(types.Int64Value(2))
	zero := withPriority(types.Int64Value(0))

	tests := []struct {
		name  string
		addr  AddressModel
		prior *AddressModel
		want  string
	}{
		{
			name:  "a removed priority goes out as null",
			addr:  none,
			prior: &two,
			want:  `{` + key + `,` + flagsOff + `,"mute":false,"priority":null}`,
		},
		{
			name:  "a priority set to 0 goes out as 0, not as null",
			addr:  zero,
			prior: &two,
			want:  `{` + key + `,` + flagsOff + `,"mute":false,"priority":0}`,
		},
		{
			name:  "an address with no priority before or after leaves it out",
			addr:  none,
			prior: &none,
			want:  `{` + key + `,` + flagsOff + `,"mute":false}`,
		},
		{
			name:  "an added priority goes out",
			addr:  two,
			prior: &none,
			want:  `{` + key + `,` + flagsOff + `,"mute":false,"priority":2}`,
		},
		{
			name: "a new address leaves out an unset priority",
			addr: none,
			want: `{` + key + `,"mute":false}`,
		},
		{
			name: "a new address sends a priority of 0",
			addr: zero,
			want: `{` + key + `,"mute":false,"priority":0}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := addressRequestJSON(t, tt.addr, tt.prior, false); got != tt.want {
				t.Errorf("sent\n  %s\nwant\n  %s", got, tt.want)
			}
		})
	}
}
