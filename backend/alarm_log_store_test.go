package main

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestAlarmLog(t *testing.T) *alarmLogStore {
	t.Helper()
	store, err := newAlarmLogStore(filepath.Join(t.TempDir(), "alarm-log.sqlite"))
	if err != nil {
		t.Fatalf("newAlarmLogStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func raisedEntry(ruleID string, at time.Time) alarmLogEntry {
	return alarmLogEntry{
		RuleID:       ruleID,
		Label:        "House bank low",
		Path:         "electrical.batteries.house.voltage",
		State:        alarmStateAlarm,
		Message:      "House bank low: below 11.8 (11)",
		ValueAtRaise: 11.0,
		RaisedAt:     at,
	}
}

func TestAlarmLogRecordsAndReadsBackAnOccurrence(t *testing.T) {
	store := newTestAlarmLog(t)

	if _, err := store.RecordRaised(raisedEntry("rule-1", alarmNow)); err != nil {
		t.Fatalf("RecordRaised: %v", err)
	}

	entries, err := store.Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].State != alarmStateAlarm || entries[0].ValueAtRaise != 11.0 {
		t.Fatalf("unexpected entry: %+v", entries[0])
	}
	if entries[0].Source != alarmSourceRule {
		t.Fatalf("source should default to %q, got %q", alarmSourceRule, entries[0].Source)
	}
	if entries[0].AckedAt != nil || entries[0].ClearedAt != nil {
		t.Fatalf("a freshly raised alarm is neither acked nor cleared: %+v", entries[0])
	}
}

func TestAlarmLogStampsAcknowledgeAndClear(t *testing.T) {
	store := newTestAlarmLog(t)
	if _, err := store.RecordRaised(raisedEntry("rule-1", alarmNow)); err != nil {
		t.Fatalf("RecordRaised: %v", err)
	}

	if err := store.MarkAcknowledged("rule-1", alarmNow.Add(time.Minute)); err != nil {
		t.Fatalf("MarkAcknowledged: %v", err)
	}
	if err := store.MarkCleared("rule-1", alarmNow.Add(2*time.Minute)); err != nil {
		t.Fatalf("MarkCleared: %v", err)
	}

	entries, _ := store.Recent(10)
	if entries[0].AckedAt == nil || !entries[0].AckedAt.Equal(alarmNow.Add(time.Minute)) {
		t.Fatalf("acked_at not stamped: %+v", entries[0])
	}
	if entries[0].ClearedAt == nil || !entries[0].ClearedAt.Equal(alarmNow.Add(2*time.Minute)) {
		t.Fatalf("cleared_at not stamped: %+v", entries[0])
	}
}

// A recurring condition is several occurrences, not one. Collapsing them would
// hide how often something is happening, which is usually the diagnosis.
func TestAlarmLogKeepsEachOccurrenceSeparate(t *testing.T) {
	store := newTestAlarmLog(t)

	store.RecordRaised(raisedEntry("rule-1", alarmNow))
	store.MarkCleared("rule-1", alarmNow.Add(time.Minute))
	store.RecordRaised(raisedEntry("rule-1", alarmNow.Add(2*time.Minute)))

	entries, _ := store.Recent(10)
	if len(entries) != 2 {
		t.Fatalf("expected 2 occurrences, got %d", len(entries))
	}
	// Newest first.
	if !entries[0].RaisedAt.After(entries[1].RaisedAt) {
		t.Fatalf("expected newest-first ordering, got %v then %v", entries[0].RaisedAt, entries[1].RaisedAt)
	}
	if entries[0].ClearedAt != nil {
		t.Fatalf("the new occurrence must still be open: %+v", entries[0])
	}
	if entries[1].ClearedAt == nil {
		t.Fatalf("the earlier occurrence should stay closed: %+v", entries[1])
	}
}

// Clearing must stamp only the open occurrence, never reopen or re-stamp one
// that has already closed.
func TestAlarmLogClearOnlyTouchesTheOpenOccurrence(t *testing.T) {
	store := newTestAlarmLog(t)

	store.RecordRaised(raisedEntry("rule-1", alarmNow))
	store.MarkCleared("rule-1", alarmNow.Add(time.Minute))
	firstClear := alarmNow.Add(time.Minute)

	// No open occurrence now; a stray clear must not rewrite history.
	store.MarkCleared("rule-1", alarmNow.Add(5*time.Minute))

	entries, _ := store.Recent(10)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if !entries[0].ClearedAt.Equal(firstClear) {
		t.Fatalf("cleared_at was rewritten: got %v, want %v", entries[0].ClearedAt, firstClear)
	}
}

func TestAlarmLogSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alarm-log.sqlite")

	store, err := newAlarmLogStore(path)
	if err != nil {
		t.Fatalf("newAlarmLogStore: %v", err)
	}
	store.RecordRaised(raisedEntry("rule-1", alarmNow))
	store.Close()

	reopened, err := newAlarmLogStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	entries, _ := reopened.Recent(10)
	if len(entries) != 1 {
		t.Fatalf("alarm history must survive a restart, got %d entries", len(entries))
	}
}

// ── OpenOccurrence: recovering a bus notification's raised_at ──────────────
//
// A bus notification carries no raised time of its own -- SignalK rewrites its
// timestamp to the moment of acknowledgement -- so the log row the bus
// watcher wrote when the notification first crossed its dwell is the only
// record of when it actually went live.

func TestOpenOccurrenceReturnsTheNewestUnclearedEntry(t *testing.T) {
	store := newTestAlarmLog(t)
	ruleID := "notifications:electrical.batteries.house.voltage"

	store.RecordRaised(alarmLogEntry{RuleID: ruleID, Label: "House bank low", RaisedAt: alarmNow})
	store.MarkCleared(ruleID, alarmNow.Add(time.Minute))
	store.RecordRaised(alarmLogEntry{RuleID: ruleID, Label: "House bank low", RaisedAt: alarmNow.Add(2 * time.Minute)})

	entry, ok, err := store.OpenOccurrence(ruleID)
	if err != nil {
		t.Fatalf("OpenOccurrence: %v", err)
	}
	if !ok {
		t.Fatalf("expected an open occurrence")
	}
	if !entry.RaisedAt.Equal(alarmNow.Add(2 * time.Minute)) {
		t.Fatalf("expected the newest open occurrence, got raised_at %v", entry.RaisedAt)
	}
}

func TestOpenOccurrenceReportsFalseWhenNoneIsOpen(t *testing.T) {
	store := newTestAlarmLog(t)
	ruleID := "notifications:mob"

	store.RecordRaised(alarmLogEntry{RuleID: ruleID, RaisedAt: alarmNow})
	store.MarkCleared(ruleID, alarmNow.Add(time.Minute))

	_, ok, err := store.OpenOccurrence(ruleID)
	if err != nil {
		t.Fatalf("OpenOccurrence: %v", err)
	}
	if ok {
		t.Fatalf("a cleared occurrence must not be reported as open")
	}

	if _, ok, err := store.OpenOccurrence("notifications:never-logged"); ok || err != nil {
		t.Fatalf("a rule with no log rows at all must report false with no error, got ok=%v err=%v", ok, err)
	}
}

func TestOpenOccurrenceIncludesAckedAtWhenPresent(t *testing.T) {
	store := newTestAlarmLog(t)
	ruleID := "notifications:arrivalCircleEntered"

	store.RecordRaised(alarmLogEntry{RuleID: ruleID, RaisedAt: alarmNow})
	store.MarkAcknowledged(ruleID, alarmNow.Add(30*time.Second))

	entry, ok, err := store.OpenOccurrence(ruleID)
	if err != nil || !ok {
		t.Fatalf("OpenOccurrence: ok=%v err=%v", ok, err)
	}
	if entry.AckedAt == nil || !entry.AckedAt.Equal(alarmNow.Add(30*time.Second)) {
		t.Fatalf("expected acked_at carried through, got %+v", entry.AckedAt)
	}
}

// A nil store is inert, matching every other method on this type.
func TestOpenOccurrenceNilStoreIsInert(t *testing.T) {
	var store *alarmLogStore

	_, ok, err := store.OpenOccurrence("notifications:mob")
	if ok || err != nil {
		t.Fatalf("nil store OpenOccurrence: ok=%v err=%v", ok, err)
	}
}

// A nil store is what tests and any not-yet-wired code path see; it must be
// inert rather than panic, matching how globalSecretsStore is handled.
func TestAlarmLogNilStoreIsInert(t *testing.T) {
	var store *alarmLogStore

	if _, err := store.RecordRaised(raisedEntry("rule-1", alarmNow)); err != nil {
		t.Fatalf("nil store RecordRaised: %v", err)
	}
	if err := store.MarkCleared("rule-1", alarmNow); err != nil {
		t.Fatalf("nil store MarkCleared: %v", err)
	}
	entries, err := store.Recent(10)
	if err != nil || entries != nil {
		t.Fatalf("nil store Recent: %v %v", entries, err)
	}
}
