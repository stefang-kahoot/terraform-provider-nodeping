package testutil

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
)

// NodePing stores some check parameters only on the check types that use
// them. A create or an update of any other type accepts the parameter,
// answers without it and never stores it. A probe of every one of the 33
// check types on 2026-10-09 (finding 36, test SubAccount) found which, and
// create and update alike; testdata/not_stored.json is its output, copied as
// it was. Its "not_stored" lists, per type, the parameters NodePing dropped.
//
// The mock drops the same ones, from the probe's file rather than from the
// provider's own table, so that a provider whose table is wrong fails here as
// it would against NodePing:
//   - A create stores none of the parameters its type does not store.
//   - An update leaves out those its type -- the type it sends, which is the
//     check's type after the update -- does not store. What the check already
//     holds of them stays as it is: a check whose type was changed keeps the
//     old type's values, and no update can change or clear them (probed:
//     HTTP holding HTTPPARSE fields kept them through updates of the fields).
//     An update that changes the type back to one that stores them changes
//     them along with it.
//
//go:embed testdata/not_stored.json
var notStoredJSON []byte

// notStored holds testdata/not_stored.json's "not_stored": per check type,
// the parameters NodePing does not store.
var notStored = func() map[string]map[string]bool {
	var probe struct {
		NotStored map[string][]string `json:"not_stored"`
	}
	if err := json.Unmarshal(notStoredJSON, &probe); err != nil {
		panic(fmt.Sprintf("testdata/not_stored.json: %v", err))
	}
	out := make(map[string]map[string]bool, len(probe.NotStored))
	for typ, names := range probe.NotStored {
		out[typ] = make(map[string]bool, len(names))
		for _, name := range names {
			out[typ][name] = true
		}
	}
	return out
}()

// NotStored returns, per check type, the parameters NodePing does not store,
// sorted, as the probe of 2026-10-09 found them.
func NotStored() map[string][]string {
	out := make(map[string][]string, len(notStored))
	for typ, names := range notStored {
		for name := range names {
			out[typ] = append(out[typ], name)
		}
		sort.Strings(out[typ])
	}
	return out
}

// storesParameter reports whether NodePing stores the parameter key on a
// check of type typ. A type the probe did not cover stores everything.
func storesParameter(typ interface{}, key string) bool {
	t, _ := typ.(string)
	return !notStored[t][key]
}
