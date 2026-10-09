package contact

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
)

// An address with no headers is read the way its block has them: NodePing
// holds none as {} after an update cleared them, or with no key at all, and
// either must read back as the plan wrote it -- null without the attribute,
// {} for `headers = {}`. With no block to go by, as on import, it is null.
// Headers that are there are read as stored. Query strings work the same way.
func TestMapAddressesToModelEmptyMaps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		stored  map[string]string
		planned func(t *testing.T) types.Map // nil: no block, as on import
		want    string
	}{
		{"cleared to {}, planned null", map[string]string{}, nullMap, "null"},
		{"cleared to {}, planned {}", map[string]string{}, emptyMap, "{}"},
		{"never set, planned null", nil, nullMap, "null"},
		{"never set, planned {}", nil, emptyMap, "{}"},
		{"cleared to {}, imported", map[string]string{}, nil, "null"},
		{"never set, imported", nil, nil, "null"},
		{"set, planned null", map[string]string{"one": "1"}, nullMap, "1 entries"},
		{"set, planned {}", map[string]string{"one": "1"}, emptyMap, "1 entries"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			api := map[string]client.ContactAddress{
				"AD1": {Type: "webhook", Address: "https://hooks.example.com/one", Headers: tt.stored, QueryStrings: tt.stored},
			}
			var plan []AddressModel
			if tt.planned != nil {
				block := addressBlock("webhook", "https://hooks.example.com/one")
				block.ID = types.StringValue("AD1")
				block.Headers = tt.planned(t)
				block.QueryStrings = tt.planned(t)
				plan = []AddressModel{block}
			}

			var diags diag.Diagnostics
			got := mapAddressesToModel(context.Background(), api, plan, &diags)
			if diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			if len(got) != 1 {
				t.Fatalf("got %d addresses, want 1", len(got))
			}
			for name, m := range map[string]types.Map{"headers": got[0].Headers, "querystrings": got[0].QueryStrings} {
				if read := describeMap(m); read != tt.want {
					t.Errorf("%s read as %s, want %s", name, read, tt.want)
				}
			}
		})
	}
}

func nullMap(*testing.T) types.Map { return types.MapNull(types.StringType) }

func emptyMap(t *testing.T) types.Map { return stringMapValue(t, map[string]string{}) }

func describeMap(m types.Map) string {
	switch {
	case m.IsNull():
		return "null"
	case len(m.Elements()) == 0:
		return "{}"
	default:
		return fmt.Sprintf("%d entries", len(m.Elements()))
	}
}
