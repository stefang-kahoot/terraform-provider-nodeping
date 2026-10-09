package testutil

import (
	"encoding/json"
	"net/http"
	"testing"
)

// storedParameter returns a parameter the mock holds for a check, as JSON, or
// "" when it holds none.
func storedParameter(t *testing.T, m *MockNodePingServer, id, key string) string {
	t.Helper()
	check, ok := m.GetCheck(id)
	if !ok {
		t.Fatalf("no check %s", id)
	}
	params, _ := check["parameters"].(map[string]interface{})
	v, ok := params[key]
	if !ok {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A create stores only the parameters its type stores, as NodePing did when
// probed: HTTPADV drops servername, a fresh HTTP check drops regex, false
// included, and both keep what every type stores.
func TestMockCheckCreateDropsWhatTheTypeDoesNotStore(t *testing.T) {
	t.Parallel()
	m := NewMockNodePingServer()
	t.Cleanup(m.Close)

	adv := createCheck(t, m, `{"type":"HTTPADV","target":"https://example.com","servername":"sni.example.com","method":"POST"}`)
	plain := createCheck(t, m, `{"type":"HTTP","target":"https://example.com","regex":false,"invert":false,"contentstring":"ok"}`)

	for _, tt := range []struct {
		id, key, want string
	}{
		{adv, "servername", ``},
		{adv, "method", `"POST"`},
		{plain, "regex", ``},
		{plain, "invert", `false`},
		{plain, "contentstring", `"ok"`},
	} {
		if got := storedParameter(t, m, tt.id, tt.key); got != tt.want {
			t.Errorf("%s holds %s = %s, want %s", tt.id, tt.key, got, tt.want)
		}
	}
}

// An update drops the same parameters as a create.
func TestMockCheckUpdateDropsWhatTheTypeDoesNotStore(t *testing.T) {
	t.Parallel()
	m := NewMockNodePingServer()
	t.Cleanup(m.Close)

	id := createCheck(t, m, `{"type":"HTTPADV","target":"https://example.com"}`)
	send(t, m, http.MethodPut, "/checks/"+id, `{"type":"HTTPADV","servername":"sni.example.com","postdata":"a=1"}`)

	if got := storedParameter(t, m, id, "servername"); got != "" {
		t.Errorf("servername = %s, want none", got)
	}
	if got := storedParameter(t, m, id, "postdata"); got != `"a=1"` {
		t.Errorf("postdata = %s, want \"a=1\"", got)
	}
}

// A check whose type was changed keeps the old type's parameters, and no
// update of the new type changes them; one that changes the type back
// changes them along with it. The probe's follow-ups, step by step.
func TestMockCheckTypeChangeFreezesTheOldTypesParameters(t *testing.T) {
	t.Parallel()
	m := NewMockNodePingServer()
	t.Cleanup(m.Close)

	const fields = `{"k1":{"max":99,"min":1,"name":"a"}}`
	id := createCheck(t, m, `{"type":"HTTPPARSE","target":"https://example.com","fields":`+fields+`}`)

	steps := []struct {
		name, update, want string
	}{
		{"a type change without them keeps them", `{"type":"HTTP"}`, fields},
		{"an update of a field is ignored", `{"type":"HTTP","fields":{"k1":{"name":"a","min":1,"max":98}}}`, fields},
		{"a new field is ignored", `{"type":"HTTP","fields":{"k2":{"name":"b"}}}`, fields},
		{"a change back applies an update in the same request", `{"type":"HTTPPARSE","fields":{"k1":{"name":"a","min":1,"max":98}}}`, `{"k1":{"max":98,"min":1,"name":"a"}}`},
	}
	for _, step := range steps {
		send(t, m, http.MethodPut, "/checks/"+id, step.update)
		if got := storedParameter(t, m, id, "fields"); got != step.want {
			t.Fatalf("%s: fields = %s, want %s", step.name, got, step.want)
		}
	}

	regex := createCheck(t, m, `{"type":"HTTPCONTENT","target":"https://example.com","regex":true}`)
	send(t, m, http.MethodPut, "/checks/"+regex, `{"type":"HTTP"}`)
	send(t, m, http.MethodPut, "/checks/"+regex, `{"type":"HTTP","regex":false}`)
	if got := storedParameter(t, m, regex, "regex"); got != `true` {
		t.Errorf("an HTTP check holding regex from HTTPCONTENT: regex = %s, want true", got)
	}
}

// The probe's file covers every check type, and lists for HTTPADV the
// servername that started finding 36.
func TestNotStoredCoversEveryType(t *testing.T) {
	t.Parallel()
	got := NotStored()
	if len(got) != 33 {
		t.Errorf("%d types, want 33", len(got))
	}
	found := false
	for _, name := range got["HTTPADV"] {
		found = found || name == "servername"
	}
	if !found {
		t.Errorf("HTTPADV: %v lacks servername", got["HTTPADV"])
	}
}
