package contact

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// decodeJSON decodes s as one JSON value, exactly, and reports false for text
// that is not one. json.Unmarshal into an interface is not exact: it reads
// every number as a float64, so integers beyond 2^53 round, and two that
// differ can read the same; and it keeps the last of an object's duplicate
// keys, so {"a":1,"a":2} reads as {"a":2}. Here numbers stay as written
// (json.Number), and an object with a duplicate key is not decoded at all:
// what such text means depends on who reads it, so it is only ever the same
// as itself, character for character.
func decodeJSON(s string) (interface{}, bool) {
	if !json.Valid([]byte(s)) {
		return nil, false
	}
	decoder := json.NewDecoder(strings.NewReader(s))
	decoder.UseNumber()
	v, err := decodeValue(decoder)
	return v, err == nil
}

var errDuplicateKey = errors.New("duplicate key")

// decodeValue decodes the next value from decoder, which holds valid JSON.
func decodeValue(decoder *json.Decoder) (interface{}, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		object := make(map[string]interface{})
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			name, _ := key.(string)
			if _, ok := object[name]; ok {
				return nil, errDuplicateKey
			}
			v, err := decodeValue(decoder)
			if err != nil {
				return nil, err
			}
			object[name] = v
		}
		_, err = decoder.Token()
		return object, err
	case json.Delim('['):
		array := []interface{}{}
		for decoder.More() {
			v, err := decodeValue(decoder)
			if err != nil {
				return nil, err
			}
			array = append(array, v)
		}
		_, err = decoder.Token()
		return array, err
	}
	// A string, a json.Number, a bool or nil.
	return token, nil
}

// sameJSON reports whether two values from decodeJSON are the same JSON:
// objects with the same keys holding the same values, in any order; arrays
// with the same values in the same order; the same strings, booleans and
// nulls; and the same numbers, by value at any precision, so 1, 1.0 and 1e0
// are one number, and 12345678901234567890 and 12345678901234567891 two.
func sameJSON(a, b interface{}) bool {
	switch x := a.(type) {
	case map[string]interface{}:
		y, ok := b.(map[string]interface{})
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, ok := y[k]
			if !ok || !sameJSON(v, w) {
				return false
			}
		}
		return true
	case []interface{}:
		y, ok := b.([]interface{})
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !sameJSON(x[i], y[i]) {
				return false
			}
		}
		return true
	case json.Number:
		y, ok := b.(json.Number)
		return ok && numberKey(x) == numberKey(y)
	default:
		return a == b
	}
}

// maxExponent bounds the exponents numberKey works out. Beyond it, a number
// is the same only as the same literal.
const maxExponent = 1 << 30

// numberKey returns a JSON number literal in a form that two literals share
// exactly when they are the same number: its significant digits, without
// leading or trailing zeros, and the power of ten of the last one. 1, 1.0,
// 10e-1 and 0.1e1 are all "1e0", 1e2 and 100 "1e2", and 0 and -0 "0". It
// works on the digits, so no number is rounded and none is too large.
func numberKey(n json.Number) string {
	literal := string(n)
	negative := strings.HasPrefix(literal, "-")
	mantissa, exponentText, hasExponent := strings.Cut(strings.ToLower(strings.TrimPrefix(literal, "-")), "e")
	whole, fraction, _ := strings.Cut(mantissa, ".")

	digits := strings.TrimLeft(whole+fraction, "0")
	if digits == "" {
		return "0"
	}

	exponent := 0
	if hasExponent {
		var err error
		exponent, err = strconv.Atoi(exponentText)
		if err != nil || exponent > maxExponent || exponent < -maxExponent {
			return "=" + literal
		}
	}
	significant := strings.TrimRight(digits, "0")
	exponent += len(digits) - len(significant) - len(fraction)

	sign := ""
	if negative {
		sign = "-"
	}
	return sign + significant + "e" + strconv.Itoa(exponent)
}
