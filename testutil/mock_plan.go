package testutil

import "net/http"

// Some NodePing features come only with some of its plans. By default the
// mock answers the way the account the probes ran on did, a test SubAccount
// of a grandfathered Business plan that has none of them (finding 36 and the
// probes of 2026-10-12):
//
//   - autodiag and homeloc: a create or an update accepts them and holds
//     false, on every check type, whatever was sent. A check created without
//     them holds neither.
//   - verifyvolume and volumemin: accepted and dropped, on every check type,
//     AUDIO included.
//   - interval 0.25 or 0.5: refused, on create and update, with
//     {"error":"Invalid check interval"} and HTTP 200. Nothing in the request
//     is applied.
//   - A voice address: a create that adds one is refused with HTTP 400
//     {"error":"Illegal address type."}. An update that adds one was not
//     probed; the mock refuses it the same way.
//   - suppressdiag: dropped on every address type, on create and update,
//     whether it was sent true or false. An address never holds it.
//
// UsePremierePlan makes the mock store all of these as sent, as an account
// with the Premiere plan presumably would. No such account has been probed.

// UsePremierePlan makes the mock answer as an account with NodePing's
// Premiere plan presumably would: it stores the settings the probed account
// dropped or refused, as they were sent.
func (m *MockNodePingServer) UsePremierePlan() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.premiere = true
}

// planCheckWrite returns a check create or update as the account takes it,
// without changing req, or the body NodePing refuses it with. The caller
// holds m.mu.
func (m *MockNodePingServer) planCheckWrite(req map[string]interface{}) (map[string]interface{}, string) {
	out := make(map[string]interface{}, len(req))
	for k, v := range req {
		out[k] = v
	}
	if m.premiere {
		return out, ""
	}

	if interval, ok := out["interval"].(float64); ok && (interval == 0.25 || interval == 0.5) {
		return nil, `{"error":"Invalid check interval"}`
	}
	for _, key := range []string{"autodiag", "homeloc"} {
		if _, ok := out[key]; ok {
			out[key] = false
		}
	}
	delete(out, "verifyvolume")
	delete(out, "volumemin")
	return out, ""
}

// planContactWrite returns a contact create or update as the account takes
// it, without changing req, or the status and body NodePing refuses it with.
// The caller holds m.mu.
func (m *MockNodePingServer) planContactWrite(req map[string]interface{}) (map[string]interface{}, int, string) {
	if m.premiere {
		return req, 0, ""
	}

	added, _ := req["newaddresses"].([]interface{})
	for _, addr := range added {
		if a, ok := addr.(map[string]interface{}); ok && a["type"] == "voice" {
			return nil, http.StatusBadRequest, `{"error":"Illegal address type."}`
		}
	}

	out := make(map[string]interface{}, len(req))
	for k, v := range req {
		out[k] = v
	}
	if added != nil {
		kept := make([]interface{}, len(added))
		for i, addr := range added {
			kept[i] = withoutSuppressDiag(addr)
		}
		out["newaddresses"] = kept
	}
	if updated, ok := req["addresses"].(map[string]interface{}); ok {
		kept := make(map[string]interface{}, len(updated))
		for id, addr := range updated {
			kept[id] = withoutSuppressDiag(addr)
		}
		out["addresses"] = kept
	}
	return out, 0, ""
}

// withoutSuppressDiag returns a copy of an address as sent, without
// suppressdiag.
func withoutSuppressDiag(addr interface{}) interface{} {
	a, ok := addr.(map[string]interface{})
	if !ok {
		return addr
	}
	out := make(map[string]interface{}, len(a))
	for k, v := range a {
		if k != "suppressdiag" {
			out[k] = v
		}
	}
	return out
}

// refuse answers a request NodePing refuses: with status and body, or with
// status 200 and body when status is 0.
func refuse(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	if status != 0 {
		w.WriteHeader(status)
	}
	_, _ = w.Write([]byte(body))
}

// CheckCreates returns the body of every create request for a check, in
// order, as sent.
func (m *MockNodePingServer) CheckCreates() []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]map[string]interface{}(nil), m.checkCreates...)
}
