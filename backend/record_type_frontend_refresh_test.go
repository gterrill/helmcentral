package main

import (
	"os"
	"regexp"
	"testing"
)

// Apply on a Mate proposal makes the open pages read again what the record
// type changed. The table that says which lives in the frontend, so a record
// type added here without an entry there leaves its page stale after Apply.
func TestEveryRecordTypeHasAFrontendRefreshEntry(t *testing.T) {
	src, err := os.ReadFile("../frontend/src/lib/assistant-proposal-actions.ts")
	if err != nil {
		t.Fatal(err)
	}
	// Only the PROPOSAL_REFRESH object body counts, so a key elsewhere in the
	// file cannot satisfy the check.
	body := regexp.MustCompile(`(?s)PROPOSAL_REFRESH[^\n]*= \{(.*?)\n\}`).FindSubmatch(src)
	if body == nil {
		t.Fatal("PROPOSAL_REFRESH object not found in frontend/src/lib/assistant-proposal-actions.ts")
	}
	for _, name := range defaultRecordRegistry.names() {
		key := regexp.MustCompile(`(?m)^\s+` + regexp.QuoteMeta(name) + `:`)
		if !key.Match(body[1]) {
			t.Errorf("record type %q has no entry in PROPOSAL_REFRESH (frontend/src/lib/assistant-proposal-actions.ts)", name)
		}
	}
}
