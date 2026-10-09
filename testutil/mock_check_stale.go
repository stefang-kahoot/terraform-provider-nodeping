package testutil

import (
	"encoding/json"
	"time"
)

// NodePing moves a check's modified to the time of every update, and
// sometimes answers an update with the check as it was before it: the old
// modified and the old values, although it stored the update. A read seconds
// later shows it (finding 41: 1 in 33 full updates of finding 36's probe,
// 5 in ~180 of finding 28's).
//
// The mock moves modified on every update. AnswerStaleUpdates and
// AnswerStaleReads make it answer the way NodePing sometimes does.

// staleCheck is how the mock answers one check whose answers are made stale.
type staleCheck struct {
	// updates and reads are how many update answers and reads are still to
	// show the check as it was before its last update.
	updates, reads int
	// before is the check as it was before its last update.
	before map[string]interface{}
}

// AnswerStaleUpdates makes the next n updates of check id answer with the
// check as it was before the update, its old modified included. The update is
// stored all the same.
func (m *MockNodePingServer) AnswerStaleUpdates(id string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.staleCheck(id).updates = n
}

// AnswerStaleReads makes the next n reads of check id after an update answer
// with the check as it was before that update, as if NodePing had not shown
// the update yet.
func (m *MockNodePingServer) AnswerStaleReads(id string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.staleCheck(id).reads = n
}

// staleCheck returns the staleness of check id. The caller holds m.mu.
func (m *MockNodePingServer) staleCheck(id string) *staleCheck {
	if m.stale == nil {
		m.stale = make(map[string]*staleCheck)
	}
	s, ok := m.stale[id]
	if !ok {
		s = &staleCheck{}
		m.stale[id] = s
	}
	return s
}

// updateAnswer moves the modified of check id, which has just stored an
// update, and returns what to answer the update with: the check, or the check
// as it was before, before. The caller holds m.mu.
func (m *MockNodePingServer) updateAnswer(id string, check, before map[string]interface{}) map[string]interface{} {
	check["modified"] = modifiedAfter(before["modified"])

	s, ok := m.stale[id]
	if !ok {
		return check
	}
	s.before = before
	if s.updates == 0 {
		return check
	}
	s.updates--
	return before
}

// readAnswer returns what to answer a read of check id with: the check, or
// the check as it was before its last update. The caller holds m.mu.
func (m *MockNodePingServer) readAnswer(id string, check map[string]interface{}) map[string]interface{} {
	s, ok := m.stale[id]
	if !ok || s.reads == 0 || s.before == nil {
		return check
	}
	s.reads--
	return s.before
}

// copyCheck returns a deep copy of a stored check, as JSON would carry it.
func copyCheck(check map[string]interface{}) map[string]interface{} {
	data, err := json.Marshal(check)
	if err != nil {
		panic(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		panic(err)
	}
	return out
}

// modifiedAfter returns the modified of an update made now: the time in epoch
// milliseconds, and always later than last, the check's modified before it.
func modifiedAfter(last interface{}) int64 {
	now := time.Now().UnixMilli()
	var prev int64
	switch v := last.(type) {
	case int:
		prev = int64(v)
	case int64:
		prev = v
	case float64:
		prev = int64(v)
	}
	if prev >= now {
		return prev + 1
	}
	return now
}
