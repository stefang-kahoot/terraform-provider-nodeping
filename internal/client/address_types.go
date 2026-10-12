package client

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// NodePing stores a webhook's action, and each value of its headers and query
// strings, exactly as a create or update sends it, whatever its JSON type: a
// number, a boolean, an object or a list (probed 2026-10-12, #46). Headers or
// query strings sent as a whole as a string or a list were ignored, but are
// read leniently all the same. Decoded as a string and maps of strings, one
// such address failed the read of its contact, and the list of every contact
// in the account. The provider sends only strings, so such values come from
// writes made outside Terraform.

// retypedFields records what an address held with another JSON type than the
// one it is read as, for warnRetypedFields to log.
type retypedFields struct {
	// action is whether the action was not a string, and read as none.
	action                bool
	headers, queryStrings retypedMap
}

// retypedMap records what readTextMap read from another JSON type.
type retypedMap struct {
	// notObject is whether the field was not an object, and read as none.
	notObject bool
	// keys are the keys whose values were not strings, read as their JSON
	// text, sorted.
	keys []string
}

// readAction reads a webhook's action as NodePing holds it. A string reads as
// itself; no action and null read as none. Anything else is unreadable, and
// reads as none too.
func readAction(raw json.RawMessage) (action string, unreadable bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	if err := json.Unmarshal(raw, &action); err != nil {
		return "", true
	}
	return action, false
}

// readTextMap reads headers or query strings as NodePing holds them. An object
// reads as a map of each key to readText's text of its value. No field and
// null read as none, and {} as an empty map. Anything else is not an object,
// and reads as none too.
func readTextMap(raw json.RawMessage) (map[string]string, retypedMap) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, retypedMap{}
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, retypedMap{notObject: true}
	}

	out := make(map[string]string, len(values))
	var retyped retypedMap
	for key, value := range values {
		text, isString := readText(value)
		out[key] = text
		if !isString {
			retyped.keys = append(retyped.keys, key)
		}
	}
	sort.Strings(retyped.keys)
	return out, retyped
}

// readText reads one header or query string value as text. A string reads as
// itself, and null as "", as a map of strings decodes it. Anything else
// reads as its JSON text as NodePing returns it: a number with its digits as
// written, never rounded, true or false, and an object or list as written,
// keys in their order. isString is false for those.
func readText(raw json.RawMessage) (text string, isString bool) {
	raw = bytes.TrimSpace(raw)
	if string(raw) == "null" {
		return "", true
	}
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &text); err == nil {
			return text, true
		}
	}
	return string(raw), false
}

// warnRetypedFields logs every action, header and query string of the
// contact's addresses that NodePing holds with another JSON type, and how it
// was read. The log names the contact, the address, the field and the key,
// but leaves the value out.
func warnRetypedFields(ctx context.Context, id string, contact Contact) {
	for addrID, addr := range contact.Addresses {
		where := map[string]interface{}{
			"contact_id":   id,
			"contact_name": contact.Name,
			"address_id":   addrID,
		}
		if addr.retyped.action {
			tflog.Warn(ctx, "NodePing holds a contact address action that is not a string; reading it as none", where,
				map[string]interface{}{"field": "action"})
		}
		warnRetypedMap(ctx, where, "headers", "header", addr.retyped.headers)
		warnRetypedMap(ctx, where, "querystrings", "query string", addr.retyped.queryStrings)
	}
}

// warnRetypedMap logs what readTextMap read from another JSON type in field,
// whose entries are each called a name.
func warnRetypedMap(ctx context.Context, where map[string]interface{}, field, name string, retyped retypedMap) {
	if retyped.notObject {
		tflog.Warn(ctx, "NodePing holds contact address "+name+"s that are not an object; reading them as none", where,
			map[string]interface{}{"field": field})
	}
	for _, key := range retyped.keys {
		tflog.Warn(ctx, "NodePing holds a contact address "+name+" that is not a string; reading it as its JSON text", where,
			map[string]interface{}{"field": field, "key": key})
	}
}
