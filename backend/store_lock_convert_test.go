package main

import "testing"

func TestLockHeld_ConvertedStoresRecordUnderTheirOwnKeys(t *testing.T) {
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

// A helper shared by several public methods must not merge them into one key:
// each public method records under its own name.
func TestLockHeld_EachPublicMethodHasItsOwnKey(t *testing.T) {
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
	for _, op := range []string{
		"alarm_log.Get", "alarm_log.LatestForRule", "alarm_log.MarkAcknowledged", "alarm_log.MarkCleared",
		"documents.DocumentsTaggedWith", "documents.documentsWithOperatorTag",
	} {
		for _, prefix := range []string{"tx.lock_wait.", "tx.lock_hold."} {
			if _, ok := serverVitals.timing(prefix + op); !ok {
				t.Errorf("%s%s not recorded", prefix, op)
			}
		}
	}
}

// A method that runs a multi-statement transaction records tx.time.<op> like
// every other transaction, not just the lock it held.
func TestAssistantStoreTransactionsRecordTxTime(t *testing.T) {
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
		if _, ok := serverVitals.timing("tx.time." + op); !ok {
			t.Errorf("tx.time.%s not recorded", op)
		}
	}
}
