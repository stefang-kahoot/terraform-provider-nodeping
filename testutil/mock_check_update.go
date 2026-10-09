package testutil

// NodePing's PUT /checks/<id> merges the request into the stored check
// rather than replacing it: whatever an update leaves out keeps its stored
// value. The probes of 2026-10-08 (finding 28, 30 and 31, test SubAccount)
// showed how each part merges, and mergeCheckUpdate does the same, so that a
// provider that leaves a removed value out of its update fails here as it
// does against NodePing.
//
// What it models, top level:
//   - Every key sent replaces the stored one, except as below.
//   - notifications, runlocations and tags are lists and replaced as a
//     whole; [] empties them. null (and "" for notifications) is ignored.
//     notifications: {} empties them too; runlocations: "" stores false.
//   - dep: "", null or false stores false, which is how a dependency is
//     removed.
//   - description: "", null, false and 0 are ignored.
//   - public: a boolean false (or 0) is ignored; only the strings "false" and
//     "0" switch it off. true, "true" and "1" switch it on.
//
// And in parameters, everything that is not a top-level key:
//   - Every parameter sent replaces the stored one; those left out stay.
//   - sendheaders, receiveheaders and data merge per key. A key sent as null
//     or "" is deleted; the others are set. A smaller map, {}, null, "" or
//     false keeps every stored key.
//   - fields merge per key, and each field per property. No key is ever
//     removed. A key whose value is not an object is ignored (NodePing
//     stored junk for some of those; the provider must never send one). A
//     min or max sent as null is stored as 0.
//
// Values are stored as sent otherwise: NodePing's normalisation of the shapes
// the provider never sends (a null contentstring stored as "", a statuscode of
// 0 stored as "", ...) is not modelled.
func mergeCheckUpdate(check, req map[string]interface{}) {
	params := copyObject(check["parameters"])

	for key, value := range req {
		if !isCheckTopLevelField(key) {
			mergeParameter(params, key, value)
			continue
		}

		switch key {
		case "enabled":
			// Stored as "enable", which is what the API answers with.
			check["enable"] = value
			check[key] = value
		case "tags":
			if value != nil {
				check[key] = value
			}
		case "notifications":
			switch v := value.(type) {
			case []interface{}:
				check[key] = v
			case map[string]interface{}:
				if len(v) == 0 {
					check[key] = []interface{}{}
				}
			}
		case "runlocations":
			switch v := value.(type) {
			case []interface{}:
				check[key] = v
			case string:
				if v == "" {
					check[key] = false
				}
			}
		case "dep":
			if value == nil || value == "" || value == false {
				check[key] = false
			} else {
				check[key] = value
			}
		case "description":
			if value == nil || value == "" || value == false || value == float64(0) {
				continue
			}
			check[key] = value
		case "public":
			switch value {
			case true, "true", "1":
				check[key] = true
			case "false", "0":
				check[key] = false
			}
		default:
			check[key] = value
		}
	}

	check["parameters"] = params
}

func isCheckTopLevelField(key string) bool {
	for _, k := range checkTopLevelFields {
		if k == key {
			return true
		}
	}
	return false
}

// mergeParameter merges one parameter of an update into params.
func mergeParameter(params map[string]interface{}, key string, value interface{}) {
	switch key {
	case "sendheaders", "receiveheaders", "data":
		sent, ok := value.(map[string]interface{})
		if !ok {
			return
		}
		merged := copyObject(params[key])
		for name, v := range sent {
			if v == nil || v == "" {
				delete(merged, name)
			} else {
				merged[name] = v
			}
		}
		if _, stored := params[key]; stored || len(merged) > 0 {
			params[key] = merged
		}
	case "fields":
		sent, ok := value.(map[string]interface{})
		if !ok {
			return
		}
		merged := copyObject(params[key])
		for fieldKey, v := range sent {
			field, ok := v.(map[string]interface{})
			if !ok {
				continue
			}
			stored := copyObject(merged[fieldKey])
			for prop, pv := range field {
				if pv == nil && (prop == "min" || prop == "max") {
					pv = float64(0)
				}
				stored[prop] = pv
			}
			merged[fieldKey] = stored
		}
		if _, stored := params[key]; stored || len(merged) > 0 {
			params[key] = merged
		}
	default:
		params[key] = value
	}
}

// copyObject returns a shallow copy of a JSON object, or an empty one for
// anything else. Merges always build new maps: the maps an update was decoded
// into are also kept, as sent, by CheckUpdates.
func copyObject(v interface{}) map[string]interface{} {
	in, _ := v.(map[string]interface{})
	out := make(map[string]interface{}, len(in))
	for k, val := range in {
		out[k] = val
	}
	return out
}

// SetCheckParameter sets one of a check's parameters the way someone editing
// it in the NodePing web interface would: behind Terraform's back.
func (m *MockNodePingServer) SetCheckParameter(id, key string, value interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	check, ok := m.checks[id]
	if !ok {
		return
	}
	params := copyObject(check["parameters"])
	params[key] = value
	check["parameters"] = params
}
