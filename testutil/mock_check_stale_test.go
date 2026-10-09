package testutil

import (
	"encoding/json"
	"net/http"
	"testing"
)

// answeredCheck is the part of a check answer these tests look at.
type answeredCheck struct {
	Label    string `json:"label"`
	Modified int64  `json:"modified"`
}

func sendCheck(t *testing.T, m *MockNodePingServer, method, id, body string) answeredCheck {
	t.Helper()
	var got answeredCheck
	if err := json.Unmarshal([]byte(send(t, m, method, "/checks/"+id, body)), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

// Every update moves the check's modified, as NodePing's do.
func TestMockCheckUpdateMovesModified(t *testing.T) {
	t.Parallel()
	m := NewMockNodePingServer()
	t.Cleanup(m.Close)

	id := createCheck(t, m, `{"type":"HTTP","target":"https://example.com","label":"a"}`)
	created := sendCheck(t, m, http.MethodGet, id, "")

	first := sendCheck(t, m, http.MethodPut, id, `{"label":"b"}`)
	second := sendCheck(t, m, http.MethodPut, id, `{"label":"b"}`)

	if first.Modified <= created.Modified {
		t.Errorf("first update answered modified %d, want later than %d", first.Modified, created.Modified)
	}
	if second.Modified <= first.Modified {
		t.Errorf("second update answered modified %d, want later than %d", second.Modified, first.Modified)
	}
	if read := sendCheck(t, m, http.MethodGet, id, ""); read.Modified != second.Modified {
		t.Errorf("read modified %d, want the last update's %d", read.Modified, second.Modified)
	}
}

// A stale update answer is the check as it was before the update, while the
// update is stored; stale reads show the same until they run out.
func TestMockAnswersStaleUpdatesAndReads(t *testing.T) {
	t.Parallel()
	m := NewMockNodePingServer()
	t.Cleanup(m.Close)

	id := createCheck(t, m, `{"type":"HTTP","target":"https://example.com","label":"a"}`)
	before := sendCheck(t, m, http.MethodGet, id, "")

	m.AnswerStaleUpdates(id, 1)
	m.AnswerStaleReads(id, 2)

	if got := sendCheck(t, m, http.MethodPut, id, `{"label":"b"}`); got != before {
		t.Errorf("stale update answered %+v, want the check as it was, %+v", got, before)
	}
	for i := range 2 {
		if got := sendCheck(t, m, http.MethodGet, id, ""); got != before {
			t.Errorf("stale read %d answered %+v, want %+v", i+1, got, before)
		}
	}

	read := sendCheck(t, m, http.MethodGet, id, "")
	if read.Label != "b" || read.Modified <= before.Modified {
		t.Errorf("read after the stale ones answered %+v, want label b and a modified later than %d", read, before.Modified)
	}

	// The stale answers have run out.
	if got := sendCheck(t, m, http.MethodPut, id, `{"label":"c"}`); got.Label != "c" || got.Modified <= read.Modified {
		t.Errorf("next update answered %+v, want label c and a modified later than %d", got, read.Modified)
	}
}
