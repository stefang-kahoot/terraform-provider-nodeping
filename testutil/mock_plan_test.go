package testutil

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
)

// storedJSON returns what obj holds under key, as JSON, or "" for no such key.
func storedJSON(t *testing.T, obj map[string]interface{}, key string) string {
	t.Helper()
	v, ok := obj[key]
	if !ok {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The probed account accepts autodiag and holds false, on create and update,
// whatever was sent. A check created without it holds none.
func TestMockDropsAutoDiag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		create, update string
		want           string
	}{
		{"created true", `,"autodiag":true`, ``, `false`},
		{`created "true"`, `,"autodiag":"true"`, ``, `false`},
		{"created without it", ``, ``, ``},
		{`updated to "true"`, ``, `"autodiag":"true"`, `false`},
		{"updated to true", `,"autodiag":false`, `"autodiag":true`, `false`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := NewMockNodePingServer()
			t.Cleanup(m.Close)

			id := createCheck(t, m, `{"type":"HTTP","target":"https://example.com"`+tt.create+`}`)
			answer := map[string]interface{}{}
			if err := json.Unmarshal([]byte(send(t, m, http.MethodGet, "/checks/"+id, "")), &answer); err != nil {
				t.Fatal(err)
			}
			if tt.update != "" {
				if err := json.Unmarshal([]byte(send(t, m, http.MethodPut, "/checks/"+id, "{"+tt.update+"}")), &answer); err != nil {
					t.Fatal(err)
				}
			}

			if got := storedJSON(t, answer, "autodiag"); got != tt.want {
				t.Errorf("answered autodiag = %s, want %s", got, tt.want)
			}
			check, _ := m.GetCheck(id)
			if got := storedJSON(t, check, "autodiag"); got != tt.want {
				t.Errorf("stored autodiag = %s, want %s", got, tt.want)
			}
		})
	}
}

// The create and update bodies the mock records are the ones sent, not what
// the account took of them.
func TestMockRecordsAutoDiagAsSent(t *testing.T) {
	t.Parallel()
	m := NewMockNodePingServer()
	t.Cleanup(m.Close)

	id := createCheck(t, m, `{"type":"HTTP","target":"https://example.com","autodiag":"true"}`)
	send(t, m, http.MethodPut, "/checks/"+id, `{"autodiag":"true"}`)

	creates := m.CheckCreates()
	if len(creates) != 1 || creates[0]["autodiag"] != "true" {
		t.Errorf("recorded creates %v, want one with autodiag as sent", creates)
	}
	updates := m.CheckUpdates(id)
	if len(updates) != 1 || updates[0]["autodiag"] != "true" {
		t.Errorf("recorded updates %v, want one with autodiag as sent", updates)
	}
}

// sendStatus sends a request to the mock and returns the status and body of
// its answer.
func sendStatus(t *testing.T, m *MockNodePingServer, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, m.URL()+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, strings.TrimSpace(string(out))
}

// The probed account holds homeloc as false, drops verifyvolume and
// volumemin, and refuses a sub-minute interval with nothing applied. With
// the Premiere plan the mock stores them all as sent.
func TestMockCheckPlanFeatures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		create, update string
		// where is "top" or "param", key the value read back, want it on
		// the probed account and premiere on a Premiere one, as JSON, ""
		// for no such key.
		where, key, want, premiere string
	}{
		{"homeloc on create", `,"homeloc":"nam"`, ``, "top", "homeloc", `false`, `"nam"`},
		{"homeloc on update", ``, `"homeloc":"roam"`, "top", "homeloc", `false`, `"roam"`},
		{"no homeloc", ``, ``, "top", "homeloc", ``, ``},
		{"verifyvolume on create", `,"verifyvolume":true`, ``, "param", "verifyvolume", ``, `true`},
		{"verifyvolume on update", ``, `"verifyvolume":true`, "param", "verifyvolume", ``, `true`},
		{"volumemin on create", `,"volumemin":-40`, ``, "param", "volumemin", ``, `-40`},
		{"volumemin on update", ``, `"volumemin":-40`, "param", "volumemin", ``, `-40`},
		{"autodiag on update", ``, `"autodiag":"true"`, "top", "autodiag", `false`, `"true"`},
		{"interval 1 on update", ``, `"interval":1`, "top", "interval", `1`, `1`},
	}

	for _, tt := range tests {
		for _, premiere := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, premiere %t", tt.name, premiere), func(t *testing.T) {
				t.Parallel()
				m := NewMockNodePingServer()
				t.Cleanup(m.Close)
				if premiere {
					m.UsePremierePlan()
				}

				id := createCheck(t, m, `{"type":"AUDIO","target":"https://example.com/stream.mp3","interval":15`+tt.create+`}`)
				if tt.update != "" {
					send(t, m, http.MethodPut, "/checks/"+id, `{"type":"AUDIO",`+tt.update+`}`)
				}

				check, _ := m.GetCheck(id)
				src := check
				if tt.where == "param" {
					src, _ = check["parameters"].(map[string]interface{})
				}
				want := tt.want
				if premiere {
					want = tt.premiere
				}
				if got := storedJSON(t, src, tt.key); got != want {
					t.Errorf("stored %s = %s, want %s", tt.key, got, want)
				}
			})
		}
	}
}

// A sub-minute interval is refused with HTTP 200 and NodePing's error, on
// create and update; the update applies nothing, its label included.
func TestMockRefusesSubMinuteIntervals(t *testing.T) {
	t.Parallel()

	for _, interval := range []string{"0.25", "0.5"} {
		t.Run(interval, func(t *testing.T) {
			t.Parallel()
			m := NewMockNodePingServer()
			t.Cleanup(m.Close)

			status, body := sendStatus(t, m, http.MethodPost, "/checks", `{"type":"HTTP","target":"https://example.com","interval":`+interval+`}`)
			if status != http.StatusOK || body != `{"error":"Invalid check interval"}` {
				t.Errorf("create answered %d %s", status, body)
			}
			if listed := send(t, m, http.MethodGet, "/checks", ""); listed != `{}` {
				t.Errorf("the refused create left %s", listed)
			}

			id := createCheck(t, m, `{"type":"HTTP","target":"https://example.com","label":"before","interval":15}`)
			status, body = sendStatus(t, m, http.MethodPut, "/checks/"+id, `{"label":"after","interval":`+interval+`}`)
			if status != http.StatusOK || body != `{"error":"Invalid check interval"}` {
				t.Errorf("update answered %d %s", status, body)
			}
			check, _ := m.GetCheck(id)
			if check["label"] != "before" || check["interval"] != float64(15) {
				t.Errorf("the refused update left label %v and interval %v", check["label"], check["interval"])
			}

			p := NewMockNodePingServer()
			t.Cleanup(p.Close)
			p.UsePremierePlan()
			stored := createCheck(t, p, `{"type":"HTTP","target":"https://example.com","interval":`+interval+`}`)
			if check, _ := p.GetCheck(stored); storedJSON(t, check, "interval") != interval {
				t.Errorf("premiere stored interval %v, want %s", check["interval"], interval)
			}
		})
	}
}

// A voice address is refused with HTTP 400, on create and on an update that
// adds one, and nothing is applied. The writes are recorded as sent.
func TestMockRefusesVoiceAddresses(t *testing.T) {
	t.Parallel()
	m := NewMockNodePingServer()
	t.Cleanup(m.Close)

	const voice = `{"type":"voice","address":"+15550000001"}`
	status, body := sendStatus(t, m, http.MethodPost, "/contacts", `{"name":"a","newaddresses":[`+voice+`]}`)
	if status != http.StatusBadRequest || body != `{"error":"Illegal address type."}` {
		t.Errorf("create answered %d %s", status, body)
	}
	if listed := send(t, m, http.MethodGet, "/contacts", ""); listed != `{}` {
		t.Errorf("the refused create left %s", listed)
	}

	var created map[string]interface{}
	if err := json.Unmarshal([]byte(send(t, m, http.MethodPost, "/contacts", `{"name":"a","newaddresses":[{"type":"email","address":"a@example.com"}]}`)), &created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["_id"].(string)
	status, body = sendStatus(t, m, http.MethodPut, "/contacts/"+id, `{"name":"b","newaddresses":[`+voice+`]}`)
	if status != http.StatusBadRequest || body != `{"error":"Illegal address type."}` {
		t.Errorf("update answered %d %s", status, body)
	}
	if contact, _ := m.GetContact(id); contact["name"] != "a" {
		t.Errorf("the refused update renamed the contact to %v", contact["name"])
	}
	if writes := m.ContactWrites(); len(writes) != 3 {
		t.Errorf("recorded %d writes, want 3", len(writes))
	}

	p := NewMockNodePingServer()
	t.Cleanup(p.Close)
	p.UsePremierePlan()
	send(t, p, http.MethodPost, "/contacts", `{"name":"a","newaddresses":[`+voice+`]}`)
	if addresses, _ := storedAddresses(t, p, onlyContact(t, p)); len(addresses) != 1 || addresses[0] != "voice:+15550000001" {
		t.Errorf("premiere stored %v", addresses)
	}
}

// onlyContact returns the ID of the mock's only contact.
func onlyContact(t *testing.T, m *MockNodePingServer) string {
	t.Helper()
	var contacts map[string]interface{}
	if err := json.Unmarshal([]byte(send(t, m, http.MethodGet, "/contacts", "")), &contacts); err != nil {
		t.Fatal(err)
	}
	if len(contacts) != 1 {
		t.Fatalf("%d contacts, want 1", len(contacts))
	}
	for id := range contacts {
		return id
	}
	return ""
}

// suppressdiag is dropped from every address on the probed account, on
// create and update, true or false, and stored as sent on a Premiere one; the
// other flags are stored either way. The writes are recorded as sent.
func TestMockSuppressDiagByPlan(t *testing.T) {
	t.Parallel()

	for _, premiere := range []bool{false, true} {
		t.Run(fmt.Sprintf("premiere %t", premiere), func(t *testing.T) {
			t.Parallel()
			m := NewMockNodePingServer()
			t.Cleanup(m.Close)
			if premiere {
				m.UsePremierePlan()
			}

			send(t, m, http.MethodPost, "/contacts", `{"name":"a","newaddresses":[`+
				`{"type":"webhook","address":"https://hooks.example.com/a","suppressdiag":true,"suppressup":true},`+
				`{"type":"email","address":"b@example.com","suppressdiag":false}]}`)
			id := onlyContact(t, m)
			contact, _ := m.GetContact(id)
			addresses, _ := contact["addresses"].(map[string]interface{})
			var addrIDs []string
			for addrID := range addresses {
				addrIDs = append(addrIDs, addrID)
			}
			sort.Strings(addrIDs)
			send(t, m, http.MethodPut, "/contacts/"+id, `{"addresses":{`+
				`"`+addrIDs[0]+`":{"type":"webhook","address":"https://hooks.example.com/a","suppressdiag":false},`+
				`"`+addrIDs[1]+`":{"type":"email","address":"b@example.com","suppressdiag":true}}}`)

			contact, _ = m.GetContact(id)
			addresses, _ = contact["addresses"].(map[string]interface{})
			first, _ := addresses[addrIDs[0]].(map[string]interface{})
			second, _ := addresses[addrIDs[1]].(map[string]interface{})
			want := map[bool][2]string{false: {``, ``}, true: {`false`, `true`}}[premiere]
			if got := storedJSON(t, first, "suppressdiag"); got != want[0] {
				t.Errorf("first address holds suppressdiag %s, want %s", got, want[0])
			}
			if got := storedJSON(t, second, "suppressdiag"); got != want[1] {
				t.Errorf("second address holds suppressdiag %s, want %s", got, want[1])
			}
			if got := storedJSON(t, first, "suppressup"); got != `true` {
				t.Errorf("first address holds suppressup %s, want true", got)
			}

			writes := m.ContactWrites()
			sent, _ := writes[0].Body["newaddresses"].([]interface{})
			if a, _ := sent[0].(map[string]interface{}); a["suppressdiag"] != true {
				t.Errorf("the create was recorded with %v, want it as sent", sent[0])
			}
		})
	}
}
