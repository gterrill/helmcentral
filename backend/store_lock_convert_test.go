package main

import "testing"

// freeKeyBudget empties the process-wide key budget, which the rest of the
// test run may have used up (2,000 keys), so a key recorded here is admitted
// rather than folded into <overflow>.
func freeKeyBudget() {
	serverVitals.keys.mu.Lock()
	defer serverVitals.keys.mu.Unlock()
	serverVitals.keys.known = map[string]struct{}{}
}

func TestLockHeld_ConvertedStoresRecordUnderTheirOwnKeys(t *testing.T) {
	freeKeyBudget()
	docs := newTestDocumentStore(t)
	if _, err := docs.Count(); err != nil {
		t.Fatal(err)
	}
	if _, err := docs.FolderTree(); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{
		"tx.lock_wait.documents.Count", "tx.lock_hold.documents.Count",
		"tx.lock_wait.documents.FolderTree", "tx.lock_hold.documents.FolderTree",
	} {
		if _, ok := serverVitals.timing(k); !ok {
			t.Errorf("%s not recorded", k)
		}
	}
}

func TestLockHeld_OtherConvertedStoresRecordUnderTheirOwnKeys(t *testing.T) {
	freeKeyBudget()
	if _, err := newTestAlarmLog(t).QueueDepth(); err != nil {
		t.Fatal(err)
	}
	if _, err := newTestPushStore(t).Count(); err != nil {
		t.Fatal(err)
	}
	if _, err := newTestAssistantStore(t).ListConversations(); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"alarm_log.QueueDepth", "webpush.Count", "assistant.ListConversations"} {
		for _, prefix := range []string{"tx.lock_wait.", "tx.lock_hold."} {
			if _, ok := serverVitals.timing(prefix + op); !ok {
				t.Errorf("%s%s not recorded", prefix, op)
			}
		}
	}
}
