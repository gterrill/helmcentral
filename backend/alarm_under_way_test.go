package main

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// An alarm log row says whether the boat was under way when the alarm was
// raised, so the watch report can leave those alarms out of its finding
// (ADR 0255). Under way is SignalK's navigation.state saying motoring or
// sailing, with no anchor watch set.

func underWayPtr(b bool) *bool { return &b }

// stampFixture installs an alarm log, an empty live snapshot and no anchor
// watch, and puts them back when the test ends.
func stampFixture(t *testing.T) *alarmLogStore {
	t.Helper()
	store := newTestAlarmLog(t)
	original, originalSnapshot := globalAlarmLogStore, globalSignalKSnapshot
	globalAlarmLogStore = store
	globalSignalKSnapshot = newSignalKSnapshot()
	anchorWatchMu.Lock()
	prev := anchorWatchState
	anchorWatchState = nil
	anchorWatchMu.Unlock()
	t.Cleanup(func() {
		globalAlarmLogStore, globalSignalKSnapshot = original, originalSnapshot
		anchorWatchMu.Lock()
		anchorWatchState = prev
		anchorWatchMu.Unlock()
	})
	return store
}

// setNavigationState puts navigation.state on the live snapshot, with the
// stream connected or not.
func setNavigationState(state string, connected bool) {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: "navigation.state", Value: state}}}},
	}, alarmNow)
	snapshot.setSelfContext("vessels.self")
	snapshot.setConnected(connected)
	globalSignalKSnapshot = snapshot
}

func setAnchorWatchForTest(set bool) {
	anchorWatchMu.Lock()
	defer anchorWatchMu.Unlock()
	if set {
		anchorWatchState = &anchorWatchData{Lat: -21.1113, Lon: 149.2276, RadiusMeters: 30}
		return
	}
	anchorWatchState = nil
}

func raiseAndRead(t *testing.T, store *alarmLogStore) alarmLogEntry {
	t.Helper()
	recordAlarmEvent(alarmEvent{
		Kind:   alarmEventRaised,
		Rule:   alarmRule{ID: "rule-1", Label: "House bank low", Path: "electrical.batteries.house.voltage", State: alarmStateAlarm},
		Status: alarmStatus{State: alarmStateAlarm, Message: "House bank low"},
	}, alarmNow)
	entry, ok, err := store.LatestForRule("rule-1")
	if err != nil || !ok {
		t.Fatalf("raised row: %v %v", ok, err)
	}
	return entry
}

func TestRecordAlarmEvent_StampsFromNavigationState(t *testing.T) {
	for _, c := range []struct {
		state string
		want  bool
	}{{"motoring", true}, {"sailing", true}, {"moored", false}, {"anchored", false}, {"aground", false}} {
		t.Run(c.state, func(t *testing.T) {
			store := stampFixture(t)
			setNavigationState(c.state, true)
			if got := raiseAndRead(t, store).UnderWay; got == nil || *got != c.want {
				t.Fatalf("%s: want under way %v, got %v", c.state, c.want, got)
			}
		})
	}
}

// The codebase spells motoring and sailing more than one way (isMotoring in
// tracks.go), and the stamp accepts each of them.
func TestRecordAlarmEvent_StampsUnderWayForEverySpellingOfMotoringAndSailing(t *testing.T) {
	for _, state := range []string{" motoring ", "under way using engine", "under_way_sailing", "Sailing", "under way sailing", "under_way_using_engine"} {
		t.Run(state, func(t *testing.T) {
			store := stampFixture(t)
			setNavigationState(state, true)
			if got := raiseAndRead(t, store).UnderWay; got == nil || !*got {
				t.Fatalf("%q: want under way, got %v", state, got)
			}
		})
	}
}

func TestRecordAlarmEvent_StampsNotUnderWayWhenTheAnchorWatchIsSetWhileMotoring(t *testing.T) {
	store := stampFixture(t)
	setNavigationState("motoring", true)
	setAnchorWatchForTest(true)
	if got := raiseAndRead(t, store).UnderWay; got == nil || *got {
		t.Fatalf("an anchor watch set means at anchor, got %v", got)
	}
}

func TestRecordAlarmEvent_AnchorWatchSetNeedsNoNavigationState(t *testing.T) {
	store := stampFixture(t)
	setAnchorWatchForTest(true)
	if got := raiseAndRead(t, store).UnderWay; got == nil || *got {
		t.Fatalf("at anchor is known without the state, got %v", got)
	}
}

func TestRecordAlarmEvent_StampsNullWhenThereIsNoNavigationState(t *testing.T) {
	store := stampFixture(t)
	if got := raiseAndRead(t, store).UnderWay; got != nil {
		t.Fatalf("a missing state is stored as unknown, got %v", *got)
	}
}

// With the stream down the held value may be hours old, so it is not an answer.
func TestRecordAlarmEvent_StampsNullWhenTheNavigationStateIsStale(t *testing.T) {
	store := stampFixture(t)
	setNavigationState("motoring", false)
	if got := raiseAndRead(t, store).UnderWay; got != nil {
		t.Fatalf("a stale state is stored as unknown, got %v", *got)
	}
}

func TestAlarmLog_AnUnderWayStampSurvivesAReadBack(t *testing.T) {
	store := newTestAlarmLog(t)
	for _, c := range []struct {
		rule string
		in   *bool
	}{{"a", underWayPtr(true)}, {"b", underWayPtr(false)}, {"c", nil}} {
		e := raisedEntry(c.rule, alarmNow)
		e.UnderWay = c.in
		if _, err := store.RecordRaised(e); err != nil {
			t.Fatal(err)
		}
		got, ok, err := store.LatestForRule(c.rule)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		if (got.UnderWay == nil) != (c.in == nil) || (c.in != nil && *got.UnderWay != *c.in) {
			t.Errorf("rule %s: stored %v, read %v", c.rule, c.in, got.UnderWay)
		}
	}
}

// A log created before the column existed gains it when it is opened, and its
// rows stay unknown.
func TestAlarmLog_OldRowsGainTheColumnAsNull(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.sqlite")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE alarm_log (
		id TEXT PRIMARY KEY, rule_id TEXT NOT NULL, source TEXT NOT NULL, label TEXT NOT NULL,
		path TEXT NOT NULL, state TEXT NOT NULL, message TEXT NOT NULL, value_at_raise REAL NOT NULL,
		raised_at INTEGER NOT NULL, acked_at INTEGER, cleared_at INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO alarm_log VALUES ('old-1', 'rule-old', 'rule', 'Old', 'p', 'alarm', 'm', 1, ?, NULL, NULL)`, alarmNow.Unix()); err != nil {
		t.Fatal(err)
	}
	old.Close()

	store, err := newAlarmLogStore(path)
	if err != nil {
		t.Fatalf("opening a log that predates the column: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	got, ok, err := store.Get("old-1")
	if err != nil || !ok || got.UnderWay != nil {
		t.Fatalf("old row: %+v %v %v", got, ok, err)
	}
	// And reopening again is not an error.
	store.Close()
	again, err := newAlarmLogStore(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	again.Close()
}

// ── the report reads the stamp ──────────────────────────────────────────

func raiseStamped(t *testing.T, store *alarmLogStore, ruleID, label, message string, n int, at time.Time, underWay *bool) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := store.RecordRaised(alarmLogEntry{
			RuleID: ruleID, Source: alarmSourceSignalK, Label: label, State: "alarm", Message: message,
			RaisedAt: at.Add(time.Duration(i) * time.Minute), UnderWay: underWay,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func collisionRule(mmsi string) string {
	return "notifications:navigation.closestApproach@urn:mrn:imo:mmsi:" + mmsi
}

func summarise(t *testing.T, store *alarmLogStore, active map[string]bool) alarmRaisedGroup {
	t.Helper()
	groups, err := store.RaisedSummary(alarmNow.Add(-time.Hour), alarmNow.Add(2*time.Hour), active)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("groups %+v", groups)
	}
	return groups[0]
}

func TestAlarmLog_RaisedSummaryLeavesOutAlarmsRaisedUnderWay(t *testing.T) {
	store := newTestAlarmLog(t)
	raiseStamped(t, store, collisionRule("1"), "Collision Risk", "ALPHA inside the CPA limit.", 5, alarmNow, underWayPtr(true))
	raiseStamped(t, store, collisionRule("2"), "Collision Risk", "BRAVO inside the CPA limit.", 2, alarmNow.Add(30*time.Minute), underWayPtr(false))
	g := summarise(t, store, map[string]bool{})
	if g.Count != 7 || g.LeftOut != 5 || g.counted().Count != 2 {
		t.Fatalf("listing keeps all 7, finding counts 2: %+v", g)
	}
}

func TestAlarmLog_RaisedSummaryCountsRowsWithNoStamp(t *testing.T) {
	store := newTestAlarmLog(t)
	raiseStamped(t, store, collisionRule("1"), "Collision Risk", "ALPHA inside the CPA limit.", 4, alarmNow, nil)
	g := summarise(t, store, map[string]bool{})
	if g.LeftOut != 0 || g.counted().Count != 4 {
		t.Fatalf("rows raised before the column, or with an unknown state, count: %+v", g)
	}
}

func TestAlarmLog_RaisedSummaryCountsAnUnderWayAlarmThatIsStillActiveByRuleID(t *testing.T) {
	store := newTestAlarmLog(t)
	raiseStamped(t, store, collisionRule("1"), "Collision Risk", "ALPHA inside the CPA limit.", 3, alarmNow, underWayPtr(true))
	raiseStamped(t, store, collisionRule("2"), "Collision Risk", "BRAVO inside the CPA limit.", 4, alarmNow, underWayPtr(true))
	// Only vessel 1's alarm is active: the same title on vessel 2 is not. Of
	// vessel 1's three raises only the latest is the one still active.
	g := summarise(t, store, map[string]bool{collisionRule("1"): true})
	if g.Count != 7 || g.LeftOut != 6 || g.counted().Count != 1 || g.UnderWay != 7 {
		t.Fatalf("still-active is matched by rule id, not title, and is the latest raise only: %+v", g)
	}
}

// A rule that flapped under way and is active now counts its current raise, not
// the cleared ones before it.
func TestAlarmLog_RaisedSummaryStillActiveIsTheLatestRaiseOnly(t *testing.T) {
	store := newTestAlarmLog(t)
	raiseStamped(t, store, "bank-low", "House Bank Low", "House bank low.", 4, alarmNow, underWayPtr(true))
	g := summarise(t, store, map[string]bool{"bank-low": true})
	if g.Count != 4 || g.LeftOut != 3 || g.counted().Count != 1 {
		t.Fatalf("only the latest of four raises is exempt: %+v", g)
	}
	if want := alarmNow.Add(3 * time.Minute); !g.counted().Last.Equal(want) {
		t.Errorf("the counted raise is the latest, %v, got %v", want, g.counted().Last)
	}
}

// An open raise is the one still active. If the latest raise has cleared, the
// active set is out of step with the log and nothing of that rule is exempt.
func TestAlarmLog_RaisedSummaryExemptsOnlyAnOpenLatestRaise(t *testing.T) {
	store := newTestAlarmLog(t)
	raiseStamped(t, store, "bank-low", "House Bank Low", "House bank low.", 3, alarmNow, underWayPtr(true))
	if err := store.MarkCleared("bank-low", alarmNow.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	g := summarise(t, store, map[string]bool{"bank-low": true})
	if g.Count != 3 || g.LeftOut != 3 || g.counted().Count != 0 {
		t.Fatalf("the latest raise has cleared, so none of the rule is exempt: %+v", g)
	}
}

// A title is active now if any of its rule ids is, whatever the capped status
// list shows.
func TestAlarmLog_RaisedSummaryMarksATitleActiveByAnyOfItsRuleIDs(t *testing.T) {
	store := newTestAlarmLog(t)
	raiseStamped(t, store, collisionRule("1"), "Collision Risk", "ALPHA inside the CPA limit.", 1, alarmNow, underWayPtr(false))
	raiseStamped(t, store, collisionRule("2"), "Collision Risk", "BRAVO inside the CPA limit.", 1, alarmNow, underWayPtr(false))
	if g := summarise(t, store, map[string]bool{collisionRule("2"): true}); !g.ActiveNow {
		t.Errorf("vessel 2's alarm is active, so the title is: %+v", g)
	}
	if g := summarise(t, store, map[string]bool{collisionRule("9"): true}); g.ActiveNow {
		t.Errorf("another rule's alarm does not make the title active: %+v", g)
	}
}

// A flapping rule with a different message each time is one row per label, not
// one per wording; only rules with an @context group by message.
func TestAlarmLog_RaisedSummaryRowsAreBoundedForAFlappingRule(t *testing.T) {
	store := newTestAlarmLog(t)
	for i := 0; i < 50; i++ {
		raiseStamped(t, store, "bank-low", "House Bank Low", fmt.Sprintf("House bank at %d.%d V.", 12, i), 1, alarmNow.Add(time.Duration(i)*time.Minute), underWayPtr(false))
	}
	raiseStamped(t, store, collisionRule("1"), "Collision Risk", "ALPHA inside the CPA limit.", 2, alarmNow, underWayPtr(false))
	raiseStamped(t, store, collisionRule("1"), "Collision Risk", "BRAVO inside the CPA limit.", 2, alarmNow, underWayPtr(false))
	byLabel, err := store.raisedRows(alarmNow.Add(-time.Hour), alarmNow.Add(2*time.Hour), map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(byLabel["House Bank Low"]); n != 1 {
		t.Errorf("one row for a rule with no context, got %d", n)
	}
	if n := len(byLabel["Collision Risk"]); n != 2 {
		t.Errorf("a rule with a context keeps its wordings, got %d", n)
	}
	groups, err := store.RaisedSummary(alarmNow.Add(-time.Hour), alarmNow.Add(2*time.Hour), map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if g.Title == "House Bank Low" && g.Count != 50 {
			t.Errorf("the count stays whole: %+v", g)
		}
	}
}

func TestAlarmLog_RaisedSummaryLeavesNothingOutWhenTheActiveListIsUnknown(t *testing.T) {
	store := newTestAlarmLog(t)
	raiseStamped(t, store, collisionRule("1"), "Collision Risk", "ALPHA inside the CPA limit.", 3, alarmNow, underWayPtr(true))
	g := summarise(t, store, nil)
	if g.LeftOut != 0 || g.counted().Count != 3 {
		t.Fatalf("without the active list nothing can be told still active, so nothing is left out: %+v", g)
	}
}

func TestAlarmLog_RaisedSummarySourcesOfTheCountedRowsOnly(t *testing.T) {
	store := newTestAlarmLog(t)
	raiseStamped(t, store, collisionRule("1"), "Collision Risk", "ALPHA inside the CPA limit.", 20, alarmNow, underWayPtr(true))
	raiseStamped(t, store, collisionRule("2"), "Collision Risk", "BRAVO inside the CPA limit.", 2, alarmNow.Add(time.Hour), underWayPtr(false))
	raiseStamped(t, store, collisionRule("3"), "Collision Risk", "CHARLIE inside the CPA limit.", 1, alarmNow.Add(70*time.Minute), nil)
	g := summarise(t, store, map[string]bool{})
	if g.Sources != 3 {
		t.Errorf("the listing names every vessel: %+v", g)
	}
	c := g.counted()
	if c.Count != 3 || c.Sources != 2 || c.SourceKind != alarmSourceAISClosestApproach {
		t.Fatalf("counted %+v", c)
	}
	if len(c.TopSources) != 2 || c.TopSources[0] != (alarmSourceCount{"Bravo", 2}) || c.TopSources[1] != (alarmSourceCount{"Charlie", 1}) {
		t.Errorf("top sources over counted rows: %+v", c.TopSources)
	}
	if !c.Last.Equal(alarmNow.Add(70 * time.Minute).Truncate(time.Second)) {
		t.Errorf("counted last raise %v", c.Last)
	}
}

func TestWatchActiveRuleIDs_IsUncappedAndByRuleID(t *testing.T) {
	var list []alarmStatus
	for i := 0; i < 15; i++ {
		list = append(list, alarmStatus{RuleID: collisionRule(string(rune('a' + i))), Label: "Collision Risk"})
	}
	ids, problem := watchActiveRuleIDs(assistantToolDeps{activeAlarms: func() []alarmStatus { return list }})
	if problem != "" || len(ids) != 15 {
		t.Fatalf("the status list stops at ten, this one must not: %d %q", len(ids), problem)
	}
	ids, problem = watchActiveRuleIDs(assistantToolDeps{activeAlarms: func() []alarmStatus { return nil }})
	if problem != "" || ids == nil || len(ids) != 0 {
		t.Fatalf("a known empty list is an empty set, not unknown: %v %q", ids, problem)
	}
	ids, problem = watchActiveRuleIDs(assistantToolDeps{})
	if ids != nil || problem == "" {
		t.Fatalf("an unwired list is a reason: %v %q", ids, problem)
	}
}
