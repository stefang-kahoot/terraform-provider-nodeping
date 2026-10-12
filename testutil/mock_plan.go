package testutil

// Some NodePing features come only with some of its plans. The mock answers
// the way the account the probes ran on did, a test SubAccount of a
// grandfathered Business plan:
//
//   - autodiag: a create or an update accepts it and holds false, on every
//     check type, whether it was sent true or "true" (finding 36). A check
//     created without it holds no autodiag at all.

// planCheckWrite returns a check create or update as the account takes it,
// without changing req. The caller holds m.mu.
func (m *MockNodePingServer) planCheckWrite(req map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(req))
	for k, v := range req {
		out[k] = v
	}
	if _, ok := out["autodiag"]; ok {
		out["autodiag"] = false
	}
	return out
}

// CheckCreates returns the body of every create request for a check, in
// order, as sent.
func (m *MockNodePingServer) CheckCreates() []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]map[string]interface{}(nil), m.checkCreates...)
}
