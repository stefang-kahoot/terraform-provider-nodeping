package contact

import (
	"fmt"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// muteOf returns the address with its mute set.
func muteOf(a AddressModel, mute bool) AddressModel {
	a.Mute = types.BoolValue(mute)
	return a
}

// shownMutes renders planned mutes as position=value, in position order.
func shownMutes(planned map[int]types.Bool) string {
	positions := make([]int, 0, len(planned))
	for i := range planned {
		positions = append(positions, i)
	}
	sort.Ints(positions)
	out := ""
	for _, i := range positions {
		out += fmt.Sprintf("%d=%v ", i, planned[i].ValueBool())
	}
	return out
}

// Under ignore_mute, a block that leaves mute out plans the mute the prior
// state has for the address it keeps, wherever that address was.
func TestPlannedIgnoredMutes(t *testing.T) {
	t.Parallel()

	hook := prior("id-1", "webhook", "https://hooks.example.com/one")[0]
	email := prior("id-2", "email", "a@x")[0]
	ids := func(in ...string) []types.String {
		out := make([]types.String, len(in))
		for i, id := range in {
			out[i] = types.StringValue(id)
			if id == "" {
				out[i] = types.StringUnknown()
			}
		}
		return out
	}

	tests := []struct {
		name    string
		prior   []AddressModel
		ids     []types.String
		ignored []bool
		want    string
	}{
		{"a muted address keeps its mute", []AddressModel{muteOf(hook, true)}, ids("id-1"), []bool{true}, "0=true "},
		{"an unmuted address keeps it too", []AddressModel{muteOf(hook, false)}, ids("id-1"), []bool{true}, "0=false "},
		{"a block that sets mute is left alone", []AddressModel{muteOf(hook, true)}, ids("id-1"), []bool{false}, ""},
		{"without ignore_mute nothing is planned", []AddressModel{muteOf(hook, true)}, ids("id-1"), nil, ""},
		{"a new address keeps the default", []AddressModel{muteOf(hook, true)}, ids(""), []bool{true}, ""},
		{
			"the mute follows an address that moved",
			[]AddressModel{muteOf(hook, true), muteOf(email, false)},
			ids("id-2", "id-1"),
			[]bool{true, true},
			"0=false 1=true ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := shownMutes(plannedIgnoredMutes(tt.prior, tt.ids, tt.ignored)); got != tt.want {
				t.Errorf("planned mutes %q, want %q", got, tt.want)
			}
		})
	}
}

// After an update, an address whose mute is left to NodePing keeps the mute it
// was planned with, whatever NodePing answered; the others take NodePing's.
func TestKeepIgnoredMutes(t *testing.T) {
	t.Parallel()

	hook := prior("id-1", "webhook", "https://hooks.example.com/one")[0]
	email := prior("id-2", "email", "a@x")[0]

	planned := []AddressModel{muteOf(email, false), muteOf(hook, false)}
	applied := []AddressModel{muteOf(hook, true), muteOf(email, true)}
	keepIgnoredMutes(applied, planned, []bool{false, true})

	if got := applied[0].Mute.ValueBool(); got {
		t.Errorf("the ignored address's mute is %v, want the planned false", got)
	}
	if got := applied[1].Mute.ValueBool(); !got {
		t.Errorf("the managed address's mute is %v, want NodePing's true", got)
	}
}
