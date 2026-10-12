package client

import (
	"encoding/json"
	"reflect"
	"testing"
)

// An address's data decodes as NodePing holds it, with the numbers in it as
// written: as a float64, 12345678901234567890 read as 12345678901234567000.
func TestContactAddressData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string // as NodePing answers it; "" leaves it out
		want interface{}
	}{
		{"left out", "", nil},
		{"null", `null`, nil},
		{"a string", `"{ \"id\": 12345678901234567890 }"`, `{ "id": 12345678901234567890 }`},
		{"an object", `{"id":12345678901234567890,"n":[1.0,2e3]}`,
			map[string]interface{}{"id": json.Number("12345678901234567890"), "n": []interface{}{json.Number("1.0"), json.Number("2e3")}}},
		{"a number", `12345678901234567890`, json.Number("12345678901234567890")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := `{"type":"webhook","address":"https://hooks.example.com/one"`
			if tt.data != "" {
				body += `,"data":` + tt.data
			}
			body += `}`

			var addr ContactAddress
			if err := json.Unmarshal([]byte(body), &addr); err != nil {
				t.Fatalf("decoding %s: %v", body, err)
			}
			if !reflect.DeepEqual(addr.Data, tt.want) {
				t.Errorf("data %s read as %#v, want %#v", tt.data, addr.Data, tt.want)
			}
			if addr.Type != "webhook" || addr.Address != "https://hooks.example.com/one" {
				t.Errorf("the other fields decoded as %+v", addr)
			}
		})
	}
}

// Decoding into an address that already holds data replaces it, as it did
// when Data was a plain field. An answer without data leaves none, as one
// without a priority does; NodePing's addresses decode into new values
// anyway, as the entries of a map.
func TestContactAddressDataReplacesHeldData(t *testing.T) {
	t.Parallel()

	addr := ContactAddress{Data: "before"}
	if err := json.Unmarshal([]byte(`{"type":"webhook","data":"after"}`), &addr); err != nil {
		t.Fatal(err)
	}
	if addr.Data != "after" {
		t.Errorf("data = %#v, want \"after\"", addr.Data)
	}
	if err := json.Unmarshal([]byte(`{"type":"webhook"}`), &addr); err != nil {
		t.Fatal(err)
	}
	if addr.Data != nil {
		t.Errorf("data = %#v after an answer without it, want nil", addr.Data)
	}
}
