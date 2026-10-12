package client

import (
	"encoding/json"
	"reflect"
	"testing"
)

// absent stands for a field NodePing's answer leaves out.
const absent = ""

func TestReadAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		raw        string
		want       string
		unreadable bool
	}{
		{"no action", absent, "", false},
		{"null", `null`, "", false},
		{"a method", `"post"`, "post", false},
		{"an empty string", `""`, "", false},
		{"any other string, which NodePing stores unvalidated", `"bogus"`, "bogus", false},
		{"a string with escapes", `"g\u0065t"`, "get", false},
		{"a string with spaces around the value", " \n\"put\"\t", "put", false},
		{"a number", `1`, "", true},
		{"zero", `0`, "", true},
		{"a fraction", `1.5`, "", true},
		{"true", `true`, "", true},
		{"false", `false`, "", true},
		{"an empty object", `{}`, "", true},
		{"an object", `{"method":"post"}`, "", true},
		{"an empty list", `[]`, "", true},
		{"a list", `["post"]`, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, unreadable := readAction(json.RawMessage(tt.raw))
			if got != tt.want || unreadable != tt.unreadable {
				t.Errorf("readAction(%s) = %q, %v; want %q, %v", tt.raw, got, unreadable, tt.want, tt.unreadable)
			}
		})
	}
}

func TestReadText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      string
		want     string
		isString bool
	}{
		{"a string", `"application/json"`, "application/json", true},
		{"an empty string", `""`, "", true},
		{"a string with escapes", `"a\"b\\c\u00e9\n"`, "a\"b\\c\u00e9\n", true},
		{"a string holding JSON", `"{\"x\":1}"`, `{"x":1}`, true},
		{"null, which a map of strings decodes as empty", `null`, "", true},
		{"a whole number", `1`, "1", false},
		{"zero", `0`, "0", false},
		{"a negative number", `-42`, "-42", false},
		{"minus zero", `-0`, "-0", false},
		{"a number with a zero fraction", `1.0`, "1.0", false},
		{"a fraction", `0.1`, "0.1", false},
		{"an exponent", `1e2`, "1e2", false},
		{"an exponent with a sign", `1.5E+20`, "1.5E+20", false},
		{"an integer beyond 64 bits", `12345678901234567890123`, "12345678901234567890123", false},
		{"an integer beyond float64's precision", `9007199254740993`, "9007199254740993", false},
		{"a long fraction", `3.14159265358979323846264338327950288`, "3.14159265358979323846264338327950288", false},
		{"true", `true`, "true", false},
		{"false", `false`, "false", false},
		{"an empty object", `{}`, "{}", false},
		{"an object, keys in their order", `{"b":1,"a":"2"}`, `{"b":1,"a":"2"}`, false},
		{"a nested object", `{"x":{"y":[1,"z",null,true,{"w":1.0}]}}`, `{"x":{"y":[1,"z",null,true,{"w":1.0}]}}`, false},
		{"an object with escapes, kept as returned", `{"k\u00e9":"\u003c\u0026\u003e"}`, `{"k\u00e9":"\u003c\u0026\u003e"}`, false},
		{"an empty list", `[]`, "[]", false},
		{"a list", `["x",1,false]`, `["x",1,false]`, false},
		{"a list of objects", `[{"a":[]},{}]`, `[{"a":[]},{}]`, false},
		{"a number with spaces around it", " 7.50 ", "7.50", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, isString := readText(json.RawMessage(tt.raw))
			if got != tt.want || isString != tt.isString {
				t.Errorf("readText(%s) = %q, %v; want %q, %v", tt.raw, got, isString, tt.want, tt.isString)
			}
		})
	}
}

func TestReadTextMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    map[string]string
		retyped retypedMap
	}{
		{name: "no field", raw: absent},
		{name: "null", raw: `null`},
		{name: "an empty object, which reads as empty rather than none", raw: `{}`, want: map[string]string{}},
		{name: "strings", raw: `{"Content-Type":"application/json","X-Empty":""}`,
			want: map[string]string{"Content-Type": "application/json", "X-Empty": ""}},
		{name: "a null value, which reads as an empty string", raw: `{"k":null}`, want: map[string]string{"k": ""}},
		{
			name: "values of every type",
			raw: `{"str":"s","null":null,"num":1.0,"big":12345678901234567890,"yes":true,"no":false,` +
				`"obj":{"x":{"y":[1,"z"]}},"list":["x",1],"empty":{},"none":[]}`,
			want: map[string]string{
				"str": "s", "null": "", "num": "1.0", "big": "12345678901234567890", "yes": "true", "no": "false",
				"obj": `{"x":{"y":[1,"z"]}}`, "list": `["x",1]`, "empty": "{}", "none": "[]",
			},
			retyped: retypedMap{keys: []string{"big", "empty", "list", "no", "none", "num", "obj", "yes"}},
		},
		{name: "a string", raw: `"x"`, retyped: retypedMap{notObject: true}},
		{name: "an empty string", raw: `""`, retyped: retypedMap{notObject: true}},
		{name: "a list", raw: `["x"]`, retyped: retypedMap{notObject: true}},
		{name: "an empty list", raw: `[]`, retyped: retypedMap{notObject: true}},
		{name: "a number", raw: `1`, retyped: retypedMap{notObject: true}},
		{name: "a boolean", raw: `true`, retyped: retypedMap{notObject: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, retyped := readTextMap(json.RawMessage(tt.raw))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("readTextMap(%s) read %#v, want %#v", tt.raw, got, tt.want)
			}
			if !reflect.DeepEqual(retyped, tt.retyped) {
				t.Errorf("readTextMap(%s) retyped %+v, want %+v", tt.raw, retyped, tt.retyped)
			}
		})
	}
}

// ContactAddress.UnmarshalJSON reads action, headers and query strings with
// the helpers, and every other field as before.
func TestContactAddressReadsRetypedFields(t *testing.T) {
	t.Parallel()

	body := `{"address":"https://hooks.example.com/x","type":"webhook","status":"new","suppressup":true,` +
		`"mute":true,"priority":"abc","action":1,"headers":{"a":1,"b":"two"},"querystrings":["x"]}`
	var addr ContactAddress
	if err := json.Unmarshal([]byte(body), &addr); err != nil {
		t.Fatalf("decoding %s: %v", body, err)
	}

	if addr.Action != "" || !reflect.DeepEqual(addr.Headers, map[string]string{"a": "1", "b": "two"}) || addr.QueryStrings != nil {
		t.Errorf("read action %q, headers %q, query strings %q", addr.Action, addr.Headers, addr.QueryStrings)
	}
	want := retypedFields{action: true, headers: retypedMap{keys: []string{"a"}}, queryStrings: retypedMap{notObject: true}}
	if !reflect.DeepEqual(addr.retyped, want) {
		t.Errorf("retyped = %+v, want %+v", addr.retyped, want)
	}
	if addr.Address != "https://hooks.example.com/x" || addr.Type != "webhook" || addr.Status != "new" ||
		!addr.SuppressUp || string(addr.Mute) != "true" || addr.Priority != nil || !addr.unreadablePriority {
		t.Errorf("the other fields decoded as %+v", addr)
	}
}

// Decoding into an address that already holds values replaces them, and what
// was read from another type.
func TestContactAddressRetypedFieldsReplaceHeldOnes(t *testing.T) {
	t.Parallel()

	var addr ContactAddress
	for _, body := range []string{
		`{"action":1,"headers":{"a":1},"querystrings":"x"}`,
		`{"action":"post","headers":{"b":"2"},"querystrings":{"q":"1"}}`,
	} {
		if err := json.Unmarshal([]byte(body), &addr); err != nil {
			t.Fatal(err)
		}
	}
	if addr.Action != "post" || !reflect.DeepEqual(addr.Headers, map[string]string{"b": "2"}) ||
		!reflect.DeepEqual(addr.QueryStrings, map[string]string{"q": "1"}) {
		t.Errorf("read action %q, headers %q, query strings %q", addr.Action, addr.Headers, addr.QueryStrings)
	}
	if !reflect.DeepEqual(addr.retyped, retypedFields{}) {
		t.Errorf("retyped = %+v, want nothing", addr.retyped)
	}
}
