package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

type MockNodePingServer struct {
	Server        *httptest.Server
	mu            sync.RWMutex
	contacts      map[string]map[string]interface{}
	checks        map[string]map[string]interface{}
	contactgroups map[string]map[string]interface{}
}

func NewMockNodePingServer() *MockNodePingServer {
	m := &MockNodePingServer{
		contacts:      make(map[string]map[string]interface{}),
		checks:        make(map[string]map[string]interface{}),
		contactgroups: make(map[string]map[string]interface{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/contacts", m.handleContacts)
	mux.HandleFunc("/contacts/", m.handleContact)
	mux.HandleFunc("/checks", m.handleChecks)
	mux.HandleFunc("/checks/", m.handleCheck)
	mux.HandleFunc("/contactgroups", m.handleContactGroups)
	mux.HandleFunc("/contactgroups/", m.handleContactGroup)

	m.Server = httptest.NewServer(mux)
	return m
}

// writeOK answers with status 200 and body. NodePing never answers 404 for an
// ID it does not have: reads, updates and deletes of one get 200 with an
// "error" body, or {} for a read of a contact or contact group. The handlers
// use the answers the real API gave on 2026-10-08 (an update of an unknown
// contact or contact group was not tried, so those keep a 404).
func writeOK(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func (m *MockNodePingServer) Close() {
	m.Server.Close()
}

func (m *MockNodePingServer) URL() string {
	return m.Server.URL
}

func (m *MockNodePingServer) handleContacts(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(m.contacts)

	case http.MethodPost:
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error": "invalid JSON"}`, http.StatusBadRequest)
			return
		}

		id := "MOCK-CONTACT-" + generateID()
		contact := map[string]interface{}{
			"_id":         id,
			"type":        "contact",
			"customer_id": "MOCK-CUSTOMER",
			"name":        req["name"],
			"custrole":    req["custrole"],
			"addresses":   make(map[string]interface{}),
		}

		if newAddrs, ok := req["newaddresses"].([]interface{}); ok {
			addresses := make(map[string]interface{})
			for _, addr := range newAddrs {
				addrMap := addr.(map[string]interface{})
				addrID := generateID()
				addresses[addrID] = addrMap
			}
			contact["addresses"] = addresses
		}

		m.contacts[id] = contact
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(contact)

	default:
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (m *MockNodePingServer) handleContact(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := strings.TrimPrefix(r.URL.Path, "/contacts/")

	switch r.Method {
	case http.MethodGet:
		contact, ok := m.contacts[id]
		if !ok {
			writeOK(w, `{}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(contact)

	case http.MethodPut:
		contact, ok := m.contacts[id]
		if !ok {
			http.Error(w, `{"error": "contact not found"}`, http.StatusNotFound)
			return
		}

		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error": "invalid JSON"}`, http.StatusBadRequest)
			return
		}

		if name, ok := req["name"]; ok {
			contact["name"] = name
		}
		if custrole, ok := req["custrole"]; ok {
			contact["custrole"] = custrole
		}

		m.contacts[id] = contact
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(contact)

	case http.MethodDelete:
		if _, ok := m.contacts[id]; !ok {
			writeOK(w, `{"error":"Unable to find that contact"}`)
			return
		}
		delete(m.contacts, id)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": id})

	default:
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// The NodePing API takes check-type specific arguments at the top level of a
// create/update request but returns them nested under "parameters". Everything
// that is not one of the fields living at the top level of a check response is
// therefore echoed back as a parameter. Hard-coding a handful of them, as this
// mock did originally, made any acceptance test for the other attributes fail
// with "provider produced inconsistent result after apply" even though the
// provider was correct.
var checkTopLevelFields = []string{
	"type", "label", "enabled", "interval", "notifications", "dep", "mute",
	"description", "tags", "runlocations", "homeloc", "autodiag", "public",
}

func checkParametersFrom(req map[string]interface{}) map[string]interface{} {
	skip := make(map[string]bool, len(checkTopLevelFields))
	for _, k := range checkTopLevelFields {
		skip[k] = true
	}

	params := make(map[string]interface{}, len(req))
	for k, v := range req {
		if skip[k] {
			continue
		}
		params[k] = v
	}
	return params
}

func (m *MockNodePingServer) handleChecks(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(m.checks)

	case http.MethodPost:
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error": "invalid JSON"}`, http.StatusBadRequest)
			return
		}

		id := "MOCK-CHECK-" + generateID()
		check := map[string]interface{}{
			"_id":         id,
			"customer_id": "MOCK-CUSTOMER",
			"type":        req["type"],
			"label":       req["label"],
			"enable":      req["enabled"],
			"interval":    req["interval"],
			"state":       1,
			"created":     1609459200000,
			"modified":    1609459200000,
			"parameters":  checkParametersFrom(req),
		}
		for _, k := range checkTopLevelFields {
			if v, ok := req[k]; ok {
				check[k] = v
			}
		}

		m.checks[id] = check
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(check)

	default:
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (m *MockNodePingServer) handleCheck(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := strings.TrimPrefix(r.URL.Path, "/checks/")

	switch r.Method {
	case http.MethodGet:
		check, ok := m.checks[id]
		if !ok {
			writeOK(w, `{"error":"Error fetching check."}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(check)

	case http.MethodPut:
		check, ok := m.checks[id]
		if !ok {
			writeOK(w, `{"error":"Unable to load check."}`)
			return
		}

		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error": "invalid JSON"}`, http.StatusBadRequest)
			return
		}

		// NodePing ignores "tags": null on an update and keeps the check's
		// tags; only an empty list clears them.
		if v, ok := req["tags"]; ok && v == nil {
			delete(req, "tags")
		}

		if label, ok := req["label"]; ok {
			check["label"] = label
		}
		if enabled, ok := req["enabled"]; ok {
			check["enable"] = enabled
		}
		if interval, ok := req["interval"]; ok {
			check["interval"] = interval
		}
		for _, k := range checkTopLevelFields {
			if v, ok := req[k]; ok {
				check[k] = v
			}
		}
		// The real API replaces the stored parameters with what the update
		// sends, so an attribute removed from the config disappears from the
		// response too. Mirroring that is what makes update round-trips
		// meaningful to test.
		check["parameters"] = checkParametersFrom(req)

		m.checks[id] = check
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(check)

	case http.MethodDelete:
		if _, ok := m.checks[id]; !ok {
			writeOK(w, `{"error":"Unable to find that check"}`)
			return
		}
		delete(m.checks, id)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": id})

	default:
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// members always round-trips as a []string, even when the request omitted it,
// so an empty group comes back as an empty list rather than a missing key.
func contactGroupMembers(req map[string]interface{}) []interface{} {
	raw, ok := req["members"].([]interface{})
	if !ok || raw == nil {
		return []interface{}{}
	}
	return raw
}

func (m *MockNodePingServer) handleContactGroups(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(m.contactgroups)

	case http.MethodPost:
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error": "invalid JSON"}`, http.StatusBadRequest)
			return
		}

		id := "MOCK-CUSTOMER-G-" + generateID()
		group := map[string]interface{}{
			"_id":         id,
			"type":        "group",
			"customer_id": "MOCK-CUSTOMER",
			"name":        req["name"],
			"members":     contactGroupMembers(req),
		}

		m.contactgroups[id] = group
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(group)

	default:
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (m *MockNodePingServer) handleContactGroup(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := strings.TrimPrefix(r.URL.Path, "/contactgroups/")

	switch r.Method {
	case http.MethodGet:
		group, ok := m.contactgroups[id]
		if !ok {
			writeOK(w, `{}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(group)

	case http.MethodPut:
		group, ok := m.contactgroups[id]
		if !ok {
			http.Error(w, `{"error": "contact group not found"}`, http.StatusNotFound)
			return
		}

		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error": "invalid JSON"}`, http.StatusBadRequest)
			return
		}

		if name, ok := req["name"]; ok {
			group["name"] = name
		}
		// An update replaces the membership wholesale, so clearing a group has
		// to actually clear it.
		group["members"] = contactGroupMembers(req)

		m.contactgroups[id] = group
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(group)

	case http.MethodDelete:
		if _, ok := m.contactgroups[id]; !ok {
			writeOK(w, `{"error":"Unable to find group"}`)
			return
		}
		delete(m.contactgroups, id)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": id})

	default:
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

var idCounter int
var idMu sync.Mutex

func generateID() string {
	idMu.Lock()
	defer idMu.Unlock()
	idCounter++
	return string(rune('A'+idCounter%26)) + string(rune('A'+(idCounter/26)%26)) + string(rune('0'+idCounter%10))
}

func (m *MockNodePingServer) AddContact(id string, contact map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.contacts[id] = contact
}

func (m *MockNodePingServer) AddCheck(id string, check map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checks[id] = check
}

func (m *MockNodePingServer) GetContact(id string) (map[string]interface{}, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.contacts[id]
	return c, ok
}

func (m *MockNodePingServer) AddContactGroup(id string, group map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.contactgroups[id] = group
}

func (m *MockNodePingServer) GetContactGroup(id string) (map[string]interface{}, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	g, ok := m.contactgroups[id]
	return g, ok
}

// RemoveCheck, RemoveContact and RemoveContactGroup delete an object the way
// someone deleting it in the NodePing web interface would: behind Terraform's
// back.
func (m *MockNodePingServer) RemoveCheck(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.checks, id)
}

func (m *MockNodePingServer) RemoveContact(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.contacts, id)
}

func (m *MockNodePingServer) RemoveContactGroup(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.contactgroups, id)
}

func (m *MockNodePingServer) GetCheck(id string) (map[string]interface{}, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.checks[id]
	return c, ok
}
