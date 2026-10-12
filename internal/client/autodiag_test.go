package client

import (
	"encoding/json"
	"testing"
)

// NodePing documents autodiag as the string "true" or "false", and the
// provider sends it that way; the probed account answered the boolean false.
// A check holding either decodes. Typed bool, a string failed the read of the
// check, and of every list it is in.
func TestCheckDecodesAutoDiagInEitherShape(t *testing.T) {
	t.Parallel()

	for _, autodiag := range []string{`true`, `false`, `"true"`, `"false"`} {
		var check Check
		if err := json.Unmarshal([]byte(`{"_id": "CHK1", "autodiag": `+autodiag+`}`), &check); err != nil {
			t.Errorf("autodiag %s: %v", autodiag, err)
		}
	}
}
