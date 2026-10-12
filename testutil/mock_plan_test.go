package testutil

import (
	"encoding/json"
	"net/http"
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
