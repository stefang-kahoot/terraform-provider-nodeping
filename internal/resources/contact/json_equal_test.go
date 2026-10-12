package contact

import (
	"encoding/json"
	"testing"
)

// Two number literals share a key exactly when they are the same number.
func TestNumberKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		literal string
		want    string
	}{
		{"0", "0"},
		{"-0", "0"},
		{"0.000", "0"},
		{"0e5", "0"},
		{"1", "1e0"},
		{"1.0", "1e0"},
		{"10e-1", "1e0"},
		{"0.1e1", "1e0"},
		{"1E+0", "1e0"},
		{"100", "1e2"},
		{"1e2", "1e2"},
		{"0.5", "5e-1"},
		{"5e-1", "5e-1"},
		{"-1.50", "-15e-1"},
		{"1.05", "105e-2"},
		{"12345678901234567890", "1234567890123456789e1"},
		{"12345678901234567891", "12345678901234567891e0"},
		{"9007199254740993", "9007199254740993e0"},
		{"0.10000000000000000001", "10000000000000000001e-20"},
		// An exponent too large to work with is kept as written.
		{"1e99999999999999999999", "=1e99999999999999999999"},
		{"1e1073741825", "=1e1073741825"},
		{"1e1073741824", "1e1073741824"},
	}

	for _, tt := range tests {
		if got := numberKey(json.Number(tt.literal)); got != tt.want {
			t.Errorf("numberKey(%s) = %s, want %s", tt.literal, got, tt.want)
		}
	}
}

// decodeJSON decodes exactly one JSON value, numbers as written, and refuses
// an object with a duplicate key, at any depth.
func TestDecodeJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text string
		ok   bool
	}{
		{`{"a":1,"b":[1,{"c":null}],"d":"x","e":true}`, true},
		{` [1, 2] `, true},
		{`"text"`, true},
		{`12345678901234567890`, true},
		{`null`, true},
		{`{"a":1,"a":1}`, false},
		{`[{"x":{"a":1,"a":2}}]`, false},
		{`{"a":1,"a":2}`, false},
		{`{"a":1} x`, false},
		{`{"a":1}{"a":1}`, false},
		{`{"a":1,}`, false},
		{`{a:1}`, false},
		{`text={label}`, false},
		{``, false},
		{` `, false},
	}

	for _, tt := range tests {
		if _, ok := decodeJSON(tt.text); ok != tt.ok {
			t.Errorf("decodeJSON(%q) ok = %v, want %v", tt.text, ok, tt.ok)
		}
	}

	v, _ := decodeJSON(`{"id":12345678901234567890,"n":[1.0]}`)
	object, _ := v.(map[string]interface{})
	if object["id"] != json.Number("12345678901234567890") {
		t.Errorf("id decoded as %#v, want the number as written", object["id"])
	}
	if n, _ := object["n"].([]interface{}); len(n) != 1 || n[0] != json.Number("1.0") {
		t.Errorf("n decoded as %#v, want [1.0] as written", object["n"])
	}
}
