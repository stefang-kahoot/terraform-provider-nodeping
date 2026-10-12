package client

import (
	"encoding/json"
	"testing"
)

// A Nullable under omitzero is left out, sent as null, or sent with its value,
// a zero value included.
func TestNullable(t *testing.T) {
	t.Parallel()

	type request struct {
		Priority Nullable[int] `json:"priority,omitzero"`
	}

	tests := []struct {
		name string
		in   Nullable[int]
		want string
	}{
		{"the zero value is left out", Nullable[int]{}, `{}`},
		{"NullOf sends null", NullOf[int](), `{"priority":null}`},
		{"ValueOf sends the value", ValueOf(2), `{"priority":2}`},
		{"ValueOf sends 0", ValueOf(0), `{"priority":0}`},
		{"ValueOf sends a negative value", ValueOf(-2), `{"priority":-2}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := json.Marshal(request{Priority: tt.in})
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("sent %s, want %s", got, tt.want)
			}
		})
	}
}
