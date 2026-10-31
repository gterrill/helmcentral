package main

import (
	"fmt"
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

// ── retention (K-2 adjacent finding, backend security audit) ───────────────
//
// notification_queue already had DropQueuedOlderThan bounding queued
// deliveries (see alarm_dispatch.go's notifyMaxAge); alarm_log had no
// equivalent at all, so a flood of injected bus notifications --
// alarm_bus_watch.go records one row per raise, source=alarmSourceSignalK,
// and anything on the SignalK bus can raise one -- grew this table forever.

func TestAlarmLogDropOlderThanRemovesOnlyEntriesRaisedBeforeCutoff(t *testing.T) {
	store := newTestAlarmLog(t)

	store.RecordRaised(raisedEntry("rule-old", alarmNow.Add(-2*alarmLogRetention)))
	store.RecordRaised(raisedEntry("rule-new", alarmNow))

	dropped, err := store.DropLogOlderThan(alarmNow.Add(-alarmLogRetention))
	if err != nil {
		t.Fatalf("DropLogOlderThan: %v", err)
	}
	if dropped != 1 {
		t.Fatalf("expected 1 row dropped, got %d", dropped)
	}

	entries, err := store.Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 || entries[0].RuleID != "rule-new" {
		t.Fatalf("expected only the recent entry to survive retention, got %+v", entries)
	}
}

func TestAlarmLogDropOlderThanKeepsEverythingUnderRetention(t *testing.T) {
	store := newTestAlarmLog(t)
	store.RecordRaised(raisedEntry("rule-1", alarmNow))
	store.RecordRaised(raisedEntry("rule-2", alarmNow.Add(-time.Hour)))

	dropped, err := store.DropLogOlderThan(alarmNow.Add(-alarmLogRetention))
	if err != nil {
		t.Fatalf("DropLogOlderThan: %v", err)
	}
	if dropped != 0 {
		t.Fatalf("expected nothing dropped well within retention, got %d", dropped)
	}
}

// A nil store is inert, matching every other method on this type.
func TestAlarmLogDropOlderThanNilStoreIsInert(t *testing.T) {
	var store *alarmLogStore

	dropped, err := store.DropLogOlderThan(alarmNow)
	if dropped != 0 || err != nil {
		t.Fatalf("nil store DropLogOlderThan: dropped=%d err=%v", dropped, err)
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

func TestAlarmLogStore_LooksUpOccurrencesForAskMate(t *testing.T) {
	store, err := newAlarmLogStore(filepath.Join(t.TempDir(), "alarm-log.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	raise := func(id, rule string, at time.Time) {
		t.Helper()
		if _, err := store.RecordRaised(alarmLogEntry{ID: id, RuleID: rule, Label: rule, Path: "p", State: "warn", RaisedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	raise("a1", "r1", base.Add(-10*24*time.Hour))
	raise("a2", "r1", base.Add(-2*24*time.Hour))
	raise("a3", "r1", base)
	raise("b1", "r2", base.Add(5*time.Minute))
	if err := store.MarkCleared("r1", base.Add(20*time.Minute)); err != nil {
		t.Fatal(err)
	}

	got, ok, err := store.Get("a2")
	if err != nil || !ok || got.RuleID != "r1" {
		t.Fatalf("Get(a2) = %+v %v %v", got, ok, err)
	}
	if _, ok, _ := store.Get("nope"); ok {
		t.Error("Get of an unknown id must report not found")
	}
	latest, ok, err := store.LatestForRule("r1")
	if err != nil || !ok || latest.ID != "a3" || latest.ClearedAt == nil {
		t.Fatalf("LatestForRule = %+v %v %v", latest, ok, err)
	}
	n, err := store.RaisedCount("r1", base.Add(-7*24*time.Hour))
	if err != nil || n != 2 {
		t.Fatalf("RaisedCount = %d, %v; want 2 (the ten-day-old raise is outside)", n, err)
	}
	near, err := store.ChangedBetween(base.Add(-15*time.Minute), base.Add(15*time.Minute), "")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, e := range near {
		ids[e.ID] = true
	}
	if !ids["a3"] || !ids["b1"] || ids["a2"] {
		t.Errorf("ChangedBetween ids = %v, want a3 and b1 only", ids)
	}
	later, err := store.ChangedBetween(base.Add(15*time.Minute), base.Add(30*time.Minute), "")
	if err != nil || len(later) != 1 || later[0].ID != "a3" {
		t.Errorf("a clearing inside the window counts: %+v %v", later, err)
	}
}

// The alarm being asked about is left out in SQL, so its own repeats cannot
// use up the row limit and hide the neighbours.
func TestAlarmLogStore_ChangedBetweenExcludesTheRuleBeforeTheLimit(t *testing.T) {
	store, err := newAlarmLogStore(filepath.Join(t.TempDir(), "alarm-log.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 250; i++ {
		if _, err := store.RecordRaised(alarmLogEntry{ID: fmt.Sprintf("s%03d", i), RuleID: "subject", Label: "S", Path: "p", State: "warn", RaisedAt: base.Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.RecordRaised(alarmLogEntry{ID: "n1", RuleID: "neighbour", Label: "N", Path: "p", State: "warn", RaisedAt: base.Add(10 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	got, err := store.ChangedBetween(base.Add(-time.Minute), base.Add(15*time.Minute), "subject")
	if err != nil || len(got) != 1 || got[0].ID != "n1" {
		t.Fatalf("want only the neighbour, got %d rows, err %v", len(got), err)
	}
}

func TestAlarmLogStore_IndexesClearedAt(t *testing.T) {
	store, err := newAlarmLogStore(filepath.Join(t.TempDir(), "alarm-log.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='alarm_log_cleared_at'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("alarm_log_cleared_at index: %d, %v", n, err)
	}
}

func TestAlarmLog_RaisedSummaryCountsDistinctSourcesAndNamesTheBusiest(t *testing.T) {
	store := newTestAlarmLog(t)
	raise := func(mmsi, message string, n int) {
		for i := 0; i < n; i++ {
			if _, err := store.RecordRaised(alarmLogEntry{
				RuleID: "notifications:navigation.closestApproach@urn:mrn:imo:mmsi:" + mmsi, Source: alarmSourceSignalK,
				Label: "Collision Risk", Path: "navigation.closestApproach", State: "alarm", Message: message,
				RaisedAt: alarmNow.Add(time.Duration(i) * time.Minute),
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	raise("503049330", "WHITSUNDAY BULLET inside the CPA limit.", 4)
	raise("503111111", "ENCORE inside the CPA limit.", 3)
	raise("503222222", "Bella Breeza inside the CPA limit.", 2)
	raise("503333333", "SEA MIST inside the CPA limit.", 1)
	raise("319123600", "<319123600> inside the CPA limit.", 5)
	if _, err := store.RecordRaised(raisedEntry("rule-1", alarmNow)); err != nil {
		t.Fatal(err)
	}
	groups, err := store.RaisedSummary(alarmNow.Add(-time.Hour), alarmNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var risk, other *alarmRaisedGroup
	for i := range groups {
		if groups[i].Title == "Collision Risk" {
			risk = &groups[i]
		} else {
			other = &groups[i]
		}
	}
	if risk == nil || other == nil {
		t.Fatalf("groups %+v", groups)
	}
	if risk.Count != 15 || risk.Sources != 5 || risk.SourceKind != alarmSourceAISClosestApproach {
		t.Errorf("risk %+v", *risk)
	}
	want := []alarmSourceCount{{"Whitsunday Bullet", 4}, {"Encore", 3}, {"Bella Breeza", 2}}
	if len(risk.TopSources) != 3 {
		t.Fatalf("top %+v", risk.TopSources)
	}
	for i, w := range want {
		if risk.TopSources[i] != w {
			t.Errorf("top[%d] = %+v, want %+v", i, risk.TopSources[i], w)
		}
	}
	if other.Sources != 0 || other.SourceKind != "" || len(other.TopSources) != 0 {
		t.Errorf("an alarm with no @ context has no sources: %+v", *other)
	}
}

func raiseCollision(t *testing.T, store *alarmLogStore, mmsi, message string, n int, at time.Time) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := store.RecordRaised(alarmLogEntry{
			RuleID: "notifications:navigation.closestApproach@urn:mrn:imo:mmsi:" + mmsi, Source: alarmSourceSignalK,
			Label: "Collision Risk", Path: "navigation.closestApproach", State: "alarm", Message: message,
			RaisedAt: at.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func raisedRisk(t *testing.T, store *alarmLogStore) alarmRaisedGroup {
	t.Helper()
	groups, err := store.RaisedSummary(alarmNow.Add(-time.Hour), alarmNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if g.Title == "Collision Risk" {
			return g
		}
	}
	t.Fatalf("no Collision Risk in %+v", groups)
	return alarmRaisedGroup{}
}

func TestAlarmLog_RaisedSummaryTiesListAlphabetically(t *testing.T) {
	store := newTestAlarmLog(t)
	raiseCollision(t, store, "503049330", "WHITSUNDAY BULLET inside the CPA limit.", 4, alarmNow)
	raiseCollision(t, store, "503111111", "ENCORE inside the CPA limit.", 4, alarmNow)
	raiseCollision(t, store, "503222222", "BELLA BREEZA inside the CPA limit.", 4, alarmNow)
	raiseCollision(t, store, "503333333", "ALPHA inside the CPA limit.", 4, alarmNow)
	g := raisedRisk(t, store)
	want := []alarmSourceCount{{"Alpha", 4}, {"Bella Breeza", 4}, {"Encore", 4}}
	if len(g.TopSources) != 3 || g.TopSources[0] != want[0] || g.TopSources[1] != want[1] || g.TopSources[2] != want[2] {
		t.Errorf("top %+v, want %+v", g.TopSources, want)
	}
}

func TestAlarmLog_RaisedSummaryTwoVesselsWithOneNameStaySeparate(t *testing.T) {
	store := newTestAlarmLog(t)
	raiseCollision(t, store, "503111111", "ENCORE inside the CPA limit.", 3, alarmNow)
	raiseCollision(t, store, "503222222", "ENCORE inside the CPA limit.", 2, alarmNow)
	g := raisedRisk(t, store)
	if g.Sources != 2 || len(g.TopSources) != 2 || g.TopSources[0] != (alarmSourceCount{"Encore", 3}) || g.TopSources[1] != (alarmSourceCount{"Encore", 2}) {
		t.Errorf("%+v", g)
	}
}

func TestAlarmLog_RaisedSummaryOneVesselNamedAndUnnamedAcrossRowsIsNamed(t *testing.T) {
	store := newTestAlarmLog(t)
	raiseCollision(t, store, "503111111", "<503111111> inside the CPA limit.", 2, alarmNow)
	raiseCollision(t, store, "503111111", "12 BELLS inside the CPA limit.", 1, alarmNow.Add(time.Hour/2))
	raiseCollision(t, store, "503222222", "(TENDER) inside the CPA limit.", 1, alarmNow)
	g := raisedRisk(t, store)
	if g.Sources != 2 || len(g.TopSources) != 2 || g.TopSources[0] != (alarmSourceCount{"12 Bells", 3}) || g.TopSources[1] != (alarmSourceCount{"(Tender)", 1}) {
		t.Errorf("%+v", g)
	}
}

func TestAlarmLog_RaisedSummarySaysNothingOfSourcesWhenSomeRowsHaveNoContext(t *testing.T) {
	store := newTestAlarmLog(t)
	raiseCollision(t, store, "503111111", "ENCORE inside the CPA limit.", 2, alarmNow)
	if _, err := store.RecordRaised(alarmLogEntry{RuleID: "collision-rule", Source: alarmSourceRule, Label: "Collision Risk", State: "alarm", Message: "x", RaisedAt: alarmNow}); err != nil {
		t.Fatal(err)
	}
	g := raisedRisk(t, store)
	if g.Count != 3 || g.Sources != 0 || g.SourceKind != "" || len(g.TopSources) != 0 {
		t.Errorf("a label that mixes rows with and without a context makes no claim about sources: %+v", g)
	}
}

// One label can be raised by rule ids of different kinds. Labelling them all
// with the first row's kind would call every source a vessel.
func TestAlarmLog_RaisedSummaryMixedSourceKindsGetNoKind(t *testing.T) {
	store := newTestAlarmLog(t)
	raiseCollision(t, store, "503111111", "Encore inside the CPA limit.", 2, alarmNow)
	for i := 0; i < 2; i++ {
		if _, err := store.RecordRaised(alarmLogEntry{
			RuleID: "notifications:zz.fault@urn:mrn:signalk:uuid:9", Source: alarmSourceSignalK,
			Label: "Collision Risk", Path: "electrical.fault", State: "alarm", Message: "Something else.",
			RaisedAt: alarmNow.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}
	groups, err := store.RaisedSummary(alarmNow.Add(-time.Hour), alarmNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Count != 4 || groups[0].Sources != 2 {
		t.Fatalf("groups %+v", groups)
	}
	if groups[0].SourceKind != "" {
		t.Errorf("sources of different kinds must not all be called %q", groups[0].SourceKind.Label())
	}
}
