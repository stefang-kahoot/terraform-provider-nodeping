package testutil

import (
	"encoding/json"
	"fmt"
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
	// checkUpdates holds every update request body per check, as sent.
	checkUpdates  map[string][]map[string]interface{}
	contactWrites []ContactWrite
}

// ContactWrite is a create or update of a contact as the mock received it,
// whether or not it was accepted.
type ContactWrite struct {
	Method string
	Body   map[string]interface{}
}

func NewMockNodePingServer() *MockNodePingServer {
	m := &MockNodePingServer{
		contacts:      make(map[string]map[string]interface{}),
		checks:        make(map[string]map[string]interface{}),
		contactgroups: make(map[string]map[string]interface{}),
		checkUpdates:  make(map[string][]map[string]interface{}),
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
		m.contactWrites = append(m.contactWrites, ContactWrite{Method: r.Method, Body: req})

		// A contact may be created without addresses, by leaving
		// `newaddresses` out; it then reads back with no `addresses` key. An
		// empty list is refused.
		newAddrs, _ := req["newaddresses"].([]interface{})
		if newAddrs != nil && len(newAddrs) == 0 {
			writeOK(w, noAddressesError)
			return
		}

		id := "MOCK-CONTACT-" + generateID()
		contact := map[string]interface{}{
			"_id":         id,
			"type":        "contact",
			"customer_id": "MOCK-CUSTOMER",
			"name":        req["name"],
			"custrole":    req["custrole"],
		}

		if len(newAddrs) > 0 {
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
		m.contactWrites = append(m.contactWrites, ContactWrite{Method: r.Method, Body: req})

		addresses, ok := updatedAddresses(contact, req)
		if !ok {
			writeOK(w, noAddressesError)
			return
		}

		if name, ok := req["name"]; ok {
			contact["name"] = name
		}
		if custrole, ok := req["custrole"]; ok {
			contact["custrole"] = custrole
		}
		if len(addresses) > 0 {
			contact["addresses"] = addresses
		} else {
			delete(contact, "addresses")
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

// noAddressesError is NodePing's answer, with status 200, to a create or
// update that would leave a contact with no addresses. The wording is its
// own; the request has nothing to do with the account's owner.
const noAddressesError = `{"error":"Account must have at least one 'owner' contact."}`

// updatedAddresses returns a contact's addresses after an update's
// `addresses` and `newaddresses`, the way the API applies them, without
// changing the contact. ok is false if NodePing would refuse the update.
//
// `addresses` carries the surviving addresses keyed by their existing ID and
// replaces the stored set; `newaddresses` is a list with no IDs yet, and each
// entry is assigned one. An absent `addresses` key leaves what is already
// stored alone -- which is exactly what makes omitting it a bug rather than a
// no-op when every address has in fact been replaced. So does `null` or "",
// which NodePing ignores.
//
// No update can remove a contact's last address: an empty `addresses` ({} or
// []) or `newaddresses` that would leave none is refused with
// noAddressesError. An update that sends neither key leaves an address-less
// contact as it is.
func updatedAddresses(contact, req map[string]interface{}) (map[string]interface{}, bool) {
	stored, _ := contact["addresses"].(map[string]interface{})
	addresses := make(map[string]interface{}, len(stored))
	for id, addr := range stored {
		addresses[id] = addr
	}
	sentEmpty := false

	switch updated := req["addresses"].(type) {
	case map[string]interface{}:
		addresses = make(map[string]interface{}, len(updated))
		for id, addr := range updated {
			addresses[id] = addr
		}
		sentEmpty = len(updated) == 0
	case []interface{}:
		// Only an empty list has been tried; it empties the set.
		if len(updated) == 0 {
			addresses = make(map[string]interface{})
			sentEmpty = true
		}
	}

	if added, ok := req["newaddresses"].([]interface{}); ok {
		sentEmpty = sentEmpty || len(added) == 0
		for _, addr := range added {
			if addrMap, ok := addr.(map[string]interface{}); ok {
				addresses[generateID()] = addrMap
			}
		}
	}

	if sentEmpty && len(addresses) == 0 {
		return nil, false
	}
	return addresses, true
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

		sent := make(map[string]interface{}, len(req))
		for k, v := range req {
			sent[k] = v
		}
		m.checkUpdates[id] = append(m.checkUpdates[id], sent)

		// NodePing merges an update into the check: what it leaves out stays
		// as it was. See mergeCheckUpdate.
		mergeCheckUpdate(check, req)

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

// generateID hands out mock resource and address IDs.
//
// They have to sort in the order they were issued. The contact resource falls
// back to ID order for any address it cannot match against a plan, which is
// every address when there is no plan at all -- an import. An ID scheme whose
// lexical order diverged from its issue order therefore decided whether a
// multi-address import test passed, and idCounter is a package-level global
// shared by every test in the binary, so the answer moved whenever a test was
// added or removed.
//
// The previous scheme ('A'+n%26, 'A'+n/26%26, '0'+n%10) wrapped every 26 IDs
// and did exactly that: ...YA4, ZA5, AB6... sorts as AB6, YA4, ZA5.
func generateID() string {
	idMu.Lock()
	defer idMu.Unlock()
	idCounter++
	return fmt.Sprintf("%06d", idCounter)
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

// ContactWrites returns every create and update of a contact the mock has
// received, in order, including those it refused.
func (m *MockNodePingServer) ContactWrites() []ContactWrite {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]ContactWrite(nil), m.contactWrites...)
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

// SetCheckMute mutes or unmutes a check the way the NodePing web interface
// does, behind Terraform's back. The web interface's "mute until" stores a
// number, the time in epoch milliseconds, rather than true.
func (m *MockNodePingServer) SetCheckMute(id string, mute interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if check, ok := m.checks[id]; ok {
		check["mute"] = mute
	}
}

// CheckMute returns the mute a check holds, and whether it holds one.
func (m *MockNodePingServer) CheckMute(id string) (interface{}, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mute, ok := m.checks[id]["mute"]
	return mute, ok
}

// SetCheckField sets a top-level field of a check the way NodePing changes it
// on its own, behind Terraform's back: "state" when the check goes down or
// comes back up, "modified" when anything writes to the check.
func (m *MockNodePingServer) SetCheckField(id, field string, value interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if check, ok := m.checks[id]; ok {
		check[field] = value
	}
}

// CheckUpdates returns the body of every update request sent for a check, in
// order.
func (m *MockNodePingServer) CheckUpdates(id string) []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]map[string]interface{}(nil), m.checkUpdates[id]...)
}
