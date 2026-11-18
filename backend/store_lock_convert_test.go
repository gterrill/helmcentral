package main

import (
	"strings"
	"testing"
	"time"
)

// recordLocksInto points every store lock and transaction at a fresh registry
// for the test, so a key recorded by an earlier test cannot satisfy it.
func recordLocksInto(t *testing.T) *vitalsRegistry {
	t.Helper()
	reg := newVitalsRegistry(time.Now)
	var rec vitalsRecorder = reg
	lockRecorderOverride.Store(&rec)
	t.Cleanup(func() { lockRecorderOverride.Store(nil) })
	return reg
}

func wantLockKeys(t *testing.T, reg *vitalsRegistry, ops ...string) {
	t.Helper()
	for _, op := range ops {
		for _, prefix := range []string{"tx.lock_wait.", "tx.lock_hold."} {
			if _, ok := reg.timing(prefix + op); !ok {
				t.Errorf("%s%s not recorded", prefix, op)
			}
		}
	}
}

func TestLockHeld_ConvertedStoresRecordUnderTheirOwnKeys(t *testing.T) {
	reg := recordLocksInto(t)
	docs := newTestDocumentStore(t)
	if _, err := docs.Count(); err != nil {
		t.Fatal(err)
	}
	if _, err := docs.FolderTree(); err != nil {
		t.Fatal(err)
	}
	wantLockKeys(t, reg, "documents.Count", "documents.FolderTree")
}

func TestLockHeld_OtherConvertedStoresRecordUnderTheirOwnKeys(t *testing.T) {
	reg := recordLocksInto(t)
	if _, err := newTestAlarmLog(t).QueueDepth(); err != nil {
		t.Fatal(err)
	}
	if _, err := newTestPushStore(t).Count(); err != nil {
		t.Fatal(err)
	}
	if _, err := newTestAssistantStore(t).ListConversations(); err != nil {
		t.Fatal(err)
	}
	wantLockKeys(t, reg, "alarm_log.QueueDepth", "webpush.Count", "assistant.ListConversations")
}

// A helper shared by several public methods must not merge them into one key:
// each public method records under its own name.
func TestLockHeld_EachPublicMethodHasItsOwnKey(t *testing.T) {
	reg := recordLocksInto(t)
	log := newTestAlarmLog(t)
	id, err := log.RecordRaised(raisedEntry("rule-keys", alarmNow))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := log.Get(id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := log.LatestForRule("rule-keys"); err != nil {
		t.Fatal(err)
	}
	if err := log.MarkAcknowledged("rule-keys", alarmNow); err != nil {
		t.Fatal(err)
	}
	if err := log.MarkCleared("rule-keys", alarmNow); err != nil {
		t.Fatal(err)
	}
	docs := newTestDocumentStore(t)
	if _, err := docs.DocumentsTaggedWith("Some Marina", 3); err != nil {
		t.Fatal(err)
	}
	if _, err := docs.documentsWithOperatorTag("Some Marina"); err != nil {
		t.Fatal(err)
	}
	wantLockKeys(t, reg,
		"alarm_log.Get", "alarm_log.LatestForRule", "alarm_log.MarkAcknowledged", "alarm_log.MarkCleared",
		"documents.DocumentsTaggedWith", "documents.documentsWithOperatorTag")
}

// A method that runs a multi-statement transaction records tx.time.<op> like
// every other transaction, not just the lock it held.
func TestAssistantStoreTransactionsRecordTxTime(t *testing.T) {
	reg := recordLocksInto(t)
	store := newTestAssistantStore(t)
	_, answer := feedbackFixture(t, store) // AppendMessage, SaveRun
	if _, err := store.SetMessageFeedback(answer.ID, "up", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.ClearMessageFeedback(answer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PruneRuns(); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteConversation(answer.ConversationID); err != nil {
		t.Fatal(err)
	}
	asst, id := pdsSeed(t)
	if _, err := asst.ApplyPassageDecisionUpdates([]decisionUpdate{{ID: id, State: "went", RowDate: "2026-10-23", PassageID: "p1"}}); err != nil {
		t.Fatal(err)
	}
	date := "2026-10-23"
	if _, err := asst.SetOperatorPassageDecision(id, "went", &date); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{
		"assistant.AppendMessage", "assistant.SaveRun", "assistant.SetMessageFeedback",
		"assistant.ClearMessageFeedback", "assistant.PruneRuns", "assistant.DeleteConversation",
		"assistant.ApplyPassageDecisionUpdates", "assistant.SetOperatorPassageDecision",
	} {
		if _, ok := reg.timing("tx.time." + op); !ok {
			t.Errorf("tx.time.%s not recorded", op)
		}
	}
}

// A write that takes its lock and runs its transaction records both under one
// op: the lock keys and tx.time.<op> carry the same name, not a lock under
// "documents.CreateFolder" and a transaction under free text.
func TestWrites_LockAndTransactionShareOneName(t *testing.T) {
	reg := recordLocksInto(t)
	docs := newTestDocumentStore(t)
	folder, err := docs.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := docs.FindOrCreateRootFolder("Papers"); err != nil {
		t.Fatal(err)
	}
	renamed := "Handbooks"
	if err := docs.PatchFolder(folder.ID, &renamed, nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := docs.CreateManual("Engine"); err != nil {
		t.Fatal(err)
	}
	if _, err := docs.SetVesselParticulars(vesselParticulars{Model: "Test 40"}); err != nil {
		t.Fatal(err)
	}
	wanted := []string{
		"documents.CreateFolder", "documents.FindOrCreateRootFolder", "documents.PatchFolder",
		"documents.CreateManual", "documents.SetVesselParticulars",
	}
	wantLockKeys(t, reg, wanted...)
	for _, op := range wanted {
		if _, ok := reg.timing("tx.time." + op); !ok {
			t.Errorf("tx.time.%s not recorded", op)
		}
	}
	// Every transaction key has a lock behind it under the same name.
	for key := range reg.snapshot().SinceStart.Timings {
		op, ok := strings.CutPrefix(key, "tx.time.")
		if !ok {
			continue
		}
		if _, held := reg.timing("tx.lock_hold." + op); !held {
			t.Errorf("%s recorded with no tx.lock_hold.%s: the transaction is named apart from its lock", key, op)
		}
	}
}
