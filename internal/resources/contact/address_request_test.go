package contact

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// addressBlock is an address block as the schema plans one with only its
// type and address configured: every flag false, every optional value null.
func addressBlock(addrType, address string) AddressModel {
	return AddressModel{
		ID:            types.StringUnknown(),
		Type:          types.StringValue(addrType),
		Address:       types.StringValue(address),
		SuppressUp:    types.BoolValue(false),
		SuppressDown:  types.BoolValue(false),
		SuppressFirst: types.BoolValue(false),
		SuppressDiag:  types.BoolValue(false),
		SuppressAll:   types.BoolValue(false),
		Mute:          types.BoolValue(false),
		Action:        types.StringNull(),
		Headers:       types.MapNull(types.StringType),
		QueryStrings:  types.MapNull(types.StringType),
		Data:          types.StringNull(),
		Priority:      types.Int64Null(),
	}
}

func stringMapValue(t *testing.T, m map[string]string) types.Map {
	t.Helper()
	v, diags := types.MapValueFrom(context.Background(), types.StringType, m)
	if diags.HasError() {
		t.Fatalf("map value: %v", diags)
	}
	return v
}

// everySet is a webhook block with every attribute configured.
func everySet(t *testing.T) AddressModel {
	a := addressBlock("webhook", "https://hooks.example.com/one")
	a.SuppressUp = types.BoolValue(true)
	a.SuppressDown = types.BoolValue(true)
	a.SuppressFirst = types.BoolValue(true)
	a.SuppressDiag = types.BoolValue(true)
	a.SuppressAll = types.BoolValue(true)
	a.Mute = types.BoolValue(true)
	a.Action = types.StringValue("post")
	a.Headers = stringMapValue(t, map[string]string{"X-One": "1", "X-Two": "2"})
	a.QueryStrings = stringMapValue(t, map[string]string{"q1": "1"})
	a.Data = types.StringValue(`{"text":"{label}"}`)
	a.Priority = types.Int64Value(1)
	return a
}

// addressRequestJSON is what a create or update sends for the block: as a new
// address when prior is nil, as the existing address prior otherwise.
func addressRequestJSON(t *testing.T, addr AddressModel, prior *AddressModel) string {
	t.Helper()
	req, diags := addressRequest(context.Background(), addr, prior)
	if diags.HasError() {
		t.Fatalf("addressRequest: %v", diags)
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(body)
}

// The exact JSON for both shapes the helper builds: a new address, in a
// create's or an update's `newaddresses`, and an existing one, under its ID
// in an update's `addresses`.
func TestAddressRequest(t *testing.T) {
	t.Parallel()

	const everySetJSON = `{"address":"https://hooks.example.com/one","type":"webhook",` +
		`"suppressup":true,"suppressdown":true,"suppressfirst":true,"suppressdiag":true,"suppressall":true,` +
		`"mute":true,"action":"post","headers":{"X-One":"1","X-Two":"2"},"querystrings":{"q1":"1"},` +
		`"data":"{\"text\":\"{label}\"}","priority":1}`

	const (
		hook        = `"address":"https://hooks.example.com/one","type":"webhook"`
		flagsOff    = `"suppressup":false,"suppressdown":false,"suppressfirst":false,"suppressdiag":false,"suppressall":false`
		allCleared  = `{` + hook + `,` + flagsOff + `,"headers":{},"querystrings":{}}`
		nothingSent = `{` + hook + `,` + flagsOff + `}`
	)

	email := addressBlock("email", "a@example.com")
	bare := addressBlock("webhook", "https://hooks.example.com/one")
	emptyMaps := bare
	emptyMaps.Headers = stringMapValue(t, map[string]string{})
	emptyMaps.QueryStrings = stringMapValue(t, map[string]string{})
	priorEverySet := everySet(t)

	tests := []struct {
		name  string
		addr  AddressModel
		prior *AddressModel
		want  string
	}{
		{
			name: "a new address leaves out what is unset, but sends mute",
			addr: email,
			want: `{"address":"a@example.com","type":"email","mute":false}`,
		},
		{
			name:  "an existing address sends every suppress flag, and mute only when true",
			addr:  email,
			prior: &email,
			want:  `{"address":"a@example.com","type":"email",` + flagsOff + `}`,
		},
		{
			// Finding 32: left out, NodePing kept the flags, headers and
			// query strings.
			name:  "an existing address clears what the plan removed",
			addr:  bare,
			prior: &priorEverySet,
			want:  allCleared,
		},
		{
			name:  "an existing address sends {} for maps emptied in the configuration",
			addr:  emptyMaps,
			prior: &priorEverySet,
			want:  allCleared,
		},
		{
			name:  "an existing address with no maps before leaves empty ones out",
			addr:  emptyMaps,
			prior: &bare,
			want:  nothingSent,
		},
		{
			name: "a new address with every attribute set",
			addr: everySet(t),
			want: everySetJSON,
		},
		{
			name:  "an existing address with every attribute set",
			addr:  everySet(t),
			prior: &email,
			want:  everySetJSON,
		},
		{
			name: "a new address leaves out empty maps",
			addr: emptyMaps,
			want: `{` + hook + `,"mute":false}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := addressRequestJSON(t, tt.addr, tt.prior); got != tt.want {
				t.Errorf("sent\n  %s\nwant\n  %s", got, tt.want)
			}
		})
	}
}
