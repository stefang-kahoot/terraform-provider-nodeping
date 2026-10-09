package testutil

import (
	"encoding/json"
	"net/http"
	"testing"
)

func createCheck(t *testing.T, m *MockNodePingServer, body string) string {
	t.Helper()
	var created map[string]interface{}
	if err := json.Unmarshal([]byte(send(t, m, http.MethodPost, "/checks", body)), &created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["_id"].(string)
	if id == "" {
		t.Fatalf("create answered %v", created)
	}
	return id
}

// The mock merges a check update the way NodePing did when probed (findings
// 28, 30 and 31): what an update leaves out stays, and each kind of value has
// its own way of being cleared, or none.
func TestMockCheckUpdateMerges(t *testing.T) {
	t.Parallel()

	const base = `"type":"HTTPADV","target":"https://example.com"`

	tests := []struct {
		name   string
		create string
		update string
		// where is "top" for a top-level key, "param" for a parameter.
		where string
		key   string
		// want is the stored value as JSON, or "" for no such key.
		want string
	}{
		{"a parameter left out stays", `"contentstring":"Example"`, `"label":"x"`, "param", "contentstring", `"Example"`},
		{"a parameter sent replaces it", `"contentstring":"Example"`, `"contentstring":""`, "param", "contentstring", `""`},
		{"a boolean parameter sent false is false", `"follow":true`, `"follow":false`, "param", "follow", `false`},
		{"a label left out stays", `"label":"before"`, `"interval":5`, "top", "label", `"before"`},
		{"enabled is stored as enable", ``, `"enabled":"false"`, "top", "enable", `"false"`},

		{"a header left out stays", `"sendheaders":{"A":"1","B":"2"}`, `"sendheaders":{"A":"1"}`, "param", "sendheaders", `{"A":"1","B":"2"}`},
		{"a header sent as null is deleted", `"sendheaders":{"A":"1","B":"2"}`, `"sendheaders":{"A":"1","B":null}`, "param", "sendheaders", `{"A":"1"}`},
		{"a header sent as empty is deleted", `"receiveheaders":{"A":"1","B":"2"}`, `"receiveheaders":{"B":""}`, "param", "receiveheaders", `{"A":"1"}`},
		{"a header is changed and added", `"sendheaders":{"A":"1"}`, `"sendheaders":{"A":"9","C":"3"}`, "param", "sendheaders", `{"A":"9","C":"3"}`},
		{"every header sent as null leaves none", `"sendheaders":{"A":"1","B":"2"}`, `"sendheaders":{"A":null,"B":null}`, "param", "sendheaders", `{}`},
		{"empty headers keep them", `"sendheaders":{"A":"1"}`, `"sendheaders":{}`, "param", "sendheaders", `{"A":"1"}`},
		{"null headers keep them", `"sendheaders":{"A":"1"}`, `"sendheaders":null`, "param", "sendheaders", `{"A":"1"}`},

		{"a field key left out stays", `"fields":{"k1":{"name":"a","min":1},"k2":{"name":"b","max":2}}`, `"fields":{"k1":{"name":"a","min":1}}`, "param", "fields", `{"k1":{"min":1,"name":"a"},"k2":{"max":2,"name":"b"}}`},
		{"a field's property left out stays", `"fields":{"k1":{"name":"a","min":1,"max":2}}`, `"fields":{"k1":{"name":"a","max":3}}`, "param", "fields", `{"k1":{"max":3,"min":1,"name":"a"}}`},
		{"a field's min sent as null is 0", `"fields":{"k1":{"name":"a","min":1,"max":2}}`, `"fields":{"k1":{"name":"a","min":null,"max":2}}`, "param", "fields", `{"k1":{"max":2,"min":0,"name":"a"}}`},
		{"a field that is not an object is ignored", `"fields":{"k1":{"name":"a","min":1}}`, `"fields":{"k1":""}`, "param", "fields", `{"k1":{"min":1,"name":"a"}}`},
		{"empty fields keep them", `"fields":{"k1":{"name":"a","min":1}}`, `"fields":{}`, "param", "fields", `{"k1":{"min":1,"name":"a"}}`},
		{"a renamed field key keeps both", `"fields":{"k1":{"name":"a"}}`, `"fields":{"k1x":{"name":"a"}}`, "param", "fields", `{"k1":{"name":"a"},"k1x":{"name":"a"}}`},

		{"notifications left out stay", `"notifications":[{"G1":{"delay":0,"schedule":"All"}}]`, `"label":"x"`, "top", "notifications", `[{"G1":{"delay":0,"schedule":"All"}}]`},
		{"notifications are replaced", `"notifications":[{"G1":{"delay":0,"schedule":"All"}},{"G2":{"delay":0,"schedule":"All"}}]`, `"notifications":[{"G2":{"delay":5,"schedule":"All"}}]`, "top", "notifications", `[{"G2":{"delay":5,"schedule":"All"}}]`},
		{"empty notifications clear them", `"notifications":[{"G1":{"delay":0,"schedule":"All"}}]`, `"notifications":[]`, "top", "notifications", `[]`},
		{"null notifications keep them", `"notifications":[{"G1":{"delay":0,"schedule":"All"}}]`, `"notifications":null`, "top", "notifications", `[{"G1":{"delay":0,"schedule":"All"}}]`},
		{"runlocations are replaced", `"runlocations":["nam"]`, `"runlocations":["eur"]`, "top", "runlocations", `["eur"]`},
		{"empty runlocations clear them", `"runlocations":["nam"]`, `"runlocations":[]`, "top", "runlocations", `[]`},
		{"null runlocations keep them", `"runlocations":["nam"]`, `"runlocations":null`, "top", "runlocations", `["nam"]`},
		{"null tags keep them", `"tags":["a"]`, `"tags":null`, "top", "tags", `["a"]`},
		{"empty tags clear them", `"tags":["a"]`, `"tags":[]`, "top", "tags", `[]`},

		{"dep false removes the dependency", `"dep":"CHECK-1"`, `"dep":false`, "top", "dep", `false`},
		{"dep empty removes the dependency", `"dep":"CHECK-1"`, `"dep":""`, "top", "dep", `false`},
		{"dep left out stays", `"dep":"CHECK-1"`, `"label":"x"`, "top", "dep", `"CHECK-1"`},

		{"an empty description is ignored", `"description":"text"`, `"description":""`, "top", "description", `"text"`},
		{"a null description is ignored", `"description":"text"`, `"description":null`, "top", "description", `"text"`},
		{"a false description is ignored", `"description":"text"`, `"description":false`, "top", "description", `"text"`},
		{"a zero description is ignored", `"description":"text"`, `"description":0`, "top", "description", `"text"`},
		{"a blank description is stored", `"description":"text"`, `"description":" "`, "top", "description", `" "`},

		{"public false is ignored", `"public":true`, `"public":false`, "top", "public", `true`},
		{"public 0 is ignored", `"public":true`, `"public":0`, "top", "public", `true`},
		{"public \"false\" switches it off", `"public":true`, `"public":"false"`, "top", "public", `false`},
		{"public \"0\" switches it off", `"public":true`, `"public":"0"`, "top", "public", `false`},
		{"public \"true\" switches it on", `"public":false`, `"public":"true"`, "top", "public", `true`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := NewMockNodePingServer()
			t.Cleanup(m.Close)

			create := "{" + base
			if tt.create != "" {
				create += "," + tt.create
			}
			id := createCheck(t, m, create+"}")

			var answer map[string]interface{}
			if err := json.Unmarshal([]byte(send(t, m, http.MethodPut, "/checks/"+id, "{"+base+","+tt.update+"}")), &answer); err != nil {
				t.Fatal(err)
			}

			check, _ := m.GetCheck(id)
			for name, stored := range map[string]map[string]interface{}{"stored": check, "answered": answer} {
				src := stored
				if tt.where == "param" {
					src, _ = stored["parameters"].(map[string]interface{})
				}
				got := ""
				if v, ok := src[tt.key]; ok {
					b, err := json.Marshal(v)
					if err != nil {
						t.Fatal(err)
					}
					got = string(b)
				}
				if got != tt.want {
					t.Errorf("%s %s = %s, want %s", name, tt.key, got, tt.want)
				}
			}
		})
	}
}

// The update bodies the mock records are the ones sent, not what they were
// merged into.
func TestMockCheckUpdatesRecordWhatWasSent(t *testing.T) {
	t.Parallel()
	m := NewMockNodePingServer()
	t.Cleanup(m.Close)

	id := createCheck(t, m, `{"type":"HTTPADV","target":"https://example.com","sendheaders":{"A":"1"}}`)
	send(t, m, http.MethodPut, "/checks/"+id, `{"sendheaders":{"B":"2"}}`)
	send(t, m, http.MethodPut, "/checks/"+id, `{"sendheaders":{"B":null}}`)

	updates := m.CheckUpdates(id)
	if len(updates) != 2 {
		t.Fatalf("recorded %d updates, want 2", len(updates))
	}
	first, err := json.Marshal(updates[0]["sendheaders"])
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != `{"B":"2"}` {
		t.Errorf("first update recorded as %s, want it as sent", first)
	}
}
