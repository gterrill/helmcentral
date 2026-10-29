package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type alarmContextOut struct {
	Alarm struct {
		Title     string `json:"title"`
		Body      string `json:"body"`
		State     string `json:"state"`
		Active    bool   `json:"active"`
		RaisedAt  string `json:"raised_at"`
		Cleared   string `json:"cleared_at"`
		ClearNote string `json:"clear_note"`
		Source    string `json:"source"`
	} `json:"alarm"`
	Timezone string `json:"timezone"`
	Rule     *struct {
		Origin      string   `json:"origin"`
		Op          string   `json:"op"`
		Value       float64  `json:"value"`
		ClearValue  *float64 `json:"clear_value"`
		DisplayUnit string   `json:"display_unit"`
		DwellSecs   int      `json:"dwell_seconds"`
		Version     string   `json:"version"`
		ReadOnly    bool     `json:"read_only"`
	} `json:"rule"`
	RuleAbsent string `json:"rule_absent"`
	Trigger    struct {
		Path string `json:"path"`
	} `json:"trigger"`
	Live *struct {
		Value      float64 `json:"value"`
		Unit       string  `json:"unit"`
		AgeSeconds float64 `json:"age_seconds"`
		Stale      bool    `json:"stale"`
	} `json:"live"`
	LiveAbsent string `json:"live_absent"`
	History    *struct {
		Min       *float64 `json:"min"`
		Max       *float64 `json:"max"`
		Unit      string   `json:"unit"`
		StartISO  string   `json:"start_iso"`
		EndISO    string   `json:"end_iso"`
		FirstSeen string   `json:"first_seen_iso"`
		LastSeen  string   `json:"last_seen_iso"`
		GapCount  int      `json:"gap_count"`
		Mean      *float64 `json:"mean"`
		Start     string   `json:"start"`
		End       string   `json:"end"`
		FirstLoc  string   `json:"first_seen"`
		LastLoc   string   `json:"last_seen"`
		Shape     *struct {
			BucketCount       int      `json:"bucket_count"`
			BucketMinutes     float64  `json:"bucket_minutes"`
			MaxStep           *float64 `json:"max_step"`
			LongestFlatMinute float64  `json:"longest_flat_minutes"`
		} `json:"shape"`
	} `json:"history"`
	HistoryAbsent string `json:"history_absent"`
	Occurrences   *struct {
		Last7Days int `json:"raised_last_7_days"`
	} `json:"occurrences"`
	OccurrencesAbsent string `json:"occurrences_absent"`
	SameWindow        []struct {
		Title string `json:"title"`
		Event string `json:"event"`
	} `json:"same_window"`
	Siblings     []string `json:"sibling_paths"`
	SiblingsNote string   `json:"siblings_note"`
}

func askMateEnv(t *testing.T, raisedAt time.Time) (assistantToolDeps, *alarmLogStore) {
	t.Helper()
	store, err := newAlarmLogStore(filepath.Join(t.TempDir(), "alarm-log.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := raisedAt.Add(20 * time.Minute)
	series := map[string][]telemetryPoint{
		"min":  {{Timestamp: raisedAt.Add(-10 * time.Minute), Value: 0.62}, {Timestamp: raisedAt, Value: 0.48}},
		"mean": {{Timestamp: raisedAt.Add(-10 * time.Minute), Value: 0.66}, {Timestamp: raisedAt, Value: 0.5}},
		"max":  {{Timestamp: raisedAt.Add(-10 * time.Minute), Value: 0.7}, {Timestamp: raisedAt, Value: 0.52}},
	}
	deps := assistantToolDeps{
		now:                        func() time.Time { return now },
		vesselState:                func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
		alarmLog:                   func() *alarmLogStore { return store },
		activeAlarms:               func() []alarmStatus { return nil },
		influxPathHistoryStat:      stubInfluxPathHistoryStat(series, nil),
		influxPathHistoryFirstLast: stubInfluxPathHistoryFirstLast(raisedAt.Add(-10*time.Minute), now, true),
		signalKDiagnostics: func(time.Time) signalKDiagnosticsSnapshot {
			return signalKDiagnosticsSnapshot{Connected: true, HasSelfTree: true, Samples: []signalKPathSample{
				{Path: testSoCPath, Value: 0.49, Source: "bms.1", AgeSeconds: 2},
			}}
		},
	}
	return deps, store
}

func runAlarmContext(t *testing.T, deps assistantToolDeps, id string) (string, alarmContextOut) {
	t.Helper()
	raw, err := deps.execute(context.Background(), "get_alarm_context", json.RawMessage(`{"alarm_id":"`+id+`"}`))
	if err != nil {
		t.Fatalf("get_alarm_context: %v", err)
	}
	var out alarmContextOut
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, raw)
	}
	return raw, out
}

func TestGetAlarmContext_RuleAlarmCarriesTheCardTheRuleTheReadingAndTheWindow(t *testing.T) {
	alarmProposalEnv(t)
	rule := seedBuiltInRule(t, alarmRule{Enabled: true, Path: testSoCPath, Label: "House Bank Low", Op: alarmOpBelow, Value: 0.5, Hysteresis: 0.05, DwellSeconds: 30, State: alarmStateWarn})
	raisedAt := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	deps, store := askMateEnv(t, raisedAt)
	deps.activeAlarms = func() []alarmStatus {
		return []alarmStatus{{RuleID: rule.ID, Label: "House Bank Low", Path: testSoCPath, Phase: alarmPhaseActive, State: alarmStateWarn,
			Message: "49% · 12.4 V", Op: alarmOpBelow, RaisedAt: raisedAt}}
	}
	for _, at := range []time.Time{raisedAt.Add(-3 * 24 * time.Hour), raisedAt} {
		if _, err := store.RecordRaised(alarmLogEntry{RuleID: rule.ID, Label: "House Bank Low", Path: testSoCPath, State: "warn", RaisedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.RecordRaised(alarmLogEntry{RuleID: "other", Label: "Bilge Pump Running", Path: "x.y", State: "alert", RaisedAt: raisedAt.Add(4 * time.Minute)}); err != nil {
		t.Fatal(err)
	}

	raw, out := runAlarmContext(t, deps, rule.ID)

	if out.Alarm.Title != "House Bank Low" || out.Alarm.Body != "49% · 12.4 V" || out.Alarm.State != "warn" || !out.Alarm.Active || out.Alarm.RaisedAt == "" {
		t.Errorf("the alarm must be the card as presented, got %+v", out.Alarm)
	}
	if out.Rule == nil || out.Rule.Origin != "built-in" || out.Rule.Op != "below" || out.Rule.Value != 50 || out.Rule.DisplayUnit != "%" ||
		out.Rule.ClearValue == nil || *out.Rule.ClearValue != 55 || out.Rule.DwellSecs != 30 || out.Rule.Version == "" {
		t.Errorf("rule in display units, got %+v", out.Rule)
	}
	if out.Trigger.Path != testSoCPath {
		t.Errorf("trigger path %q", out.Trigger.Path)
	}
	if out.Live == nil || out.Live.Value != 49 || out.Live.Unit != "%" || out.Live.Stale {
		t.Errorf("live reading %+v", out.Live)
	}
	if out.History == nil || out.History.Min == nil || *out.History.Min != 48 || out.History.Max == nil || *out.History.Max != 70 || out.History.Unit != "%" {
		t.Errorf("history window in display units, got %+v\n%s", out.History, raw)
	}
	if out.History != nil {
		start, _ := time.Parse(time.RFC3339, out.History.StartISO)
		end, _ := time.Parse(time.RFC3339, out.History.EndISO)
		if !start.Before(raisedAt) || !end.After(raisedAt) {
			t.Errorf("history window %s - %s must straddle the raise", out.History.StartISO, out.History.EndISO)
		}
	}
	if out.Occurrences == nil || out.Occurrences.Last7Days != 2 {
		t.Errorf("occurrences %+v", out.Occurrences)
	}
	if len(out.SameWindow) != 1 || out.SameWindow[0].Title != "Bilge Pump Running" || out.SameWindow[0].Event != "raised" {
		t.Errorf("same window should list the other alarm only, got %+v", out.SameWindow)
	}
	if len(out.Siblings) != 0 || out.SiblingsNote == "" {
		t.Errorf("siblings are not built: expect an empty list and a reason, got %v %q", out.Siblings, out.SiblingsNote)
	}
}

func TestGetAlarmContext_ClearedAlarmIsFoundByLogIDAndSaysCleared(t *testing.T) {
	alarmProposalEnv(t)
	raisedAt := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	deps, store := askMateEnv(t, raisedAt)
	id, err := store.RecordRaised(alarmLogEntry{RuleID: "own-1", Label: "Cabin Hot", Path: "environment.outside.temperature", State: "warn", Message: "31 °C", RaisedAt: raisedAt})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkCleared("own-1", raisedAt.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	_, out := runAlarmContext(t, deps, id)
	if out.Alarm.Active || out.Alarm.Cleared == "" || out.Alarm.Title != "Cabin Hot" || out.Alarm.Body != "31 °C" {
		t.Errorf("cleared alarm %+v", out.Alarm)
	}
	if out.Rule != nil || out.RuleAbsent == "" {
		t.Errorf("rule no longer exists: want rule_absent with a reason, got %+v %q", out.Rule, out.RuleAbsent)
	}
}

func TestGetAlarmContext_NetworkAlarmWithNoRuleStillReturnsWhatIsKnown(t *testing.T) {
	raisedAt := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	deps, _ := askMateEnv(t, raisedAt)
	deps.pathHasData = func(string) bool { return false }
	deps.activeAlarms = func() []alarmStatus {
		return []alarmStatus{{RuleID: "notifications:radar.guardZone1", Label: "Radar Guard Zone 1", Path: "notifications.radar.guardZone1",
			Phase: alarmPhaseActive, State: alarmStateAlarm, Message: "Target in guard zone 1", RaisedAt: raisedAt}}
	}
	_, out := runAlarmContext(t, deps, "notifications:radar.guardZone1")
	if out.Alarm.Title != "Radar Guard Zone 1" || out.Alarm.Source == "" {
		t.Errorf("alarm %+v", out.Alarm)
	}
	if out.Rule != nil || out.RuleAbsent == "" {
		t.Errorf("no rule: need a reason, got %q", out.RuleAbsent)
	}
	if out.History != nil || out.HistoryAbsent == "" || out.Live != nil || out.LiveAbsent == "" {
		t.Errorf("no data path: history and live need reasons, got %q / %q", out.HistoryAbsent, out.LiveAbsent)
	}
}

func TestGetAlarmContext_HistoryFailureIsReportedNotHidden(t *testing.T) {
	alarmProposalEnv(t)
	rule := seedBuiltInRule(t, alarmRule{Enabled: true, Path: testSoCPath, Label: "House Bank Low", Op: alarmOpBelow, Value: 0.5, State: alarmStateWarn})
	raisedAt := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	deps, _ := askMateEnv(t, raisedAt)
	deps.influxPathHistoryStat = func(context.Context, string, string, time.Time, time.Time, string, string) ([]telemetryPoint, error) {
		return nil, errors.New("influx unreachable")
	}
	deps.activeAlarms = func() []alarmStatus {
		return []alarmStatus{{RuleID: rule.ID, Label: "House Bank Low", Path: testSoCPath, Phase: alarmPhaseActive, State: alarmStateWarn, RaisedAt: raisedAt}}
	}
	_, out := runAlarmContext(t, deps, rule.ID)
	if out.History != nil || !strings.Contains(out.HistoryAbsent, "influx unreachable") {
		t.Errorf("want the real query error in history_absent, got %q", out.HistoryAbsent)
	}
	if out.Rule == nil {
		t.Error("the rest of the context still returns")
	}
}

func TestGetAlarmContext_UnknownAlarmIsAnError(t *testing.T) {
	deps, _ := askMateEnv(t, time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC))
	if _, err := deps.execute(context.Background(), "get_alarm_context", json.RawMessage(`{"alarm_id":"nope"}`)); err == nil {
		t.Fatal("an id that matches nothing must be an error")
	}
	if _, err := deps.execute(context.Background(), "get_alarm_context", json.RawMessage(`{}`)); err == nil {
		t.Fatal("alarm_id is required")
	}
}

func socDeps(t *testing.T, series map[string][]telemetryPoint, raisedAt time.Time) (assistantToolDeps, string) {
	t.Helper()
	alarmProposalEnv(t)
	rule := seedBuiltInRule(t, alarmRule{Enabled: true, Path: testSoCPath, Label: "House Bank Low", Op: alarmOpBelow, Value: 0.5, State: alarmStateWarn})
	deps, _ := askMateEnv(t, raisedAt)
	deps.influxPathHistoryStat = stubInfluxPathHistoryStat(series, nil)
	deps.activeAlarms = func() []alarmStatus {
		return []alarmStatus{{RuleID: rule.ID, Label: "House Bank Low", Path: testSoCPath, Phase: alarmPhaseActive, State: alarmStateWarn, RaisedAt: raisedAt}}
	}
	return deps, rule.ID
}

// 0.4951 must read 49.51 %, not 50 %: the ratio is converted before it is rounded.
func TestGetAlarmContext_HistoryIsConvertedBeforeItIsRounded(t *testing.T) {
	raisedAt := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	series := map[string][]telemetryPoint{
		"min":  {{Timestamp: raisedAt, Value: 0.4951}},
		"mean": {{Timestamp: raisedAt, Value: 0.4951}},
		"max":  {{Timestamp: raisedAt, Value: 0.4951}},
	}
	deps, id := socDeps(t, series, raisedAt)
	_, out := runAlarmContext(t, deps, id)
	if out.History == nil || out.History.Min == nil || *out.History.Min != 49.51 || out.History.Mean == nil || *out.History.Mean != 49.51 {
		t.Fatalf("history %+v", out.History)
	}
}

func TestGetAlarmContext_HistoryHasShapeAndBoatLocalTimes(t *testing.T) {
	useFreshBoatZone(t)
	raisedAt := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	pt := func(m int, v float64) telemetryPoint {
		return telemetryPoint{Timestamp: raisedAt.Add(time.Duration(m) * time.Minute), Value: v}
	}
	vals := []float64{0.6, 0.6, 0.6, 0.5, 0.5}
	var pts []telemetryPoint
	for i, v := range vals {
		pts = append(pts, pt(i, v))
	}
	series := map[string][]telemetryPoint{"min": pts, "mean": pts, "max": pts}
	deps, id := socDeps(t, series, raisedAt)
	_, out := runAlarmContext(t, deps, id)
	h := out.History
	if h == nil || h.Shape == nil {
		t.Fatalf("history %+v", h)
	}
	if h.Shape.BucketCount != 5 || h.Shape.BucketMinutes != 1 || h.Shape.MaxStep == nil || *h.Shape.MaxStep != 10 || h.Shape.LongestFlatMinute != 3 {
		t.Errorf("shape %+v max_step %v", h.Shape, h.Shape.MaxStep)
	}
	if h.Start != "Tue 20 Oct 01:30" || h.End == "" || h.FirstLoc == "" || h.LastLoc == "" || out.Timezone == "" {
		t.Errorf("boat-local strings missing: %+v tz %q", h, out.Timezone)
	}
}

func TestGetAlarmContext_NeverClearedOldRowIsNotSwappedForTheLiveAlarm(t *testing.T) {
	alarmProposalEnv(t)
	raisedAt := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	deps, store := askMateEnv(t, raisedAt)
	for _, e := range []alarmLogEntry{
		{ID: "old", RuleID: "own-1", Label: "Cabin Hot", Path: "p", State: "warn", Message: "old", RaisedAt: raisedAt.Add(-72 * time.Hour)},
		{ID: "new", RuleID: "own-1", Label: "Cabin Hot", Path: "p", State: "warn", Message: "new", RaisedAt: raisedAt},
	} {
		if _, err := store.RecordRaised(e); err != nil {
			t.Fatal(err)
		}
	}
	deps.activeAlarms = func() []alarmStatus {
		return []alarmStatus{{RuleID: "own-1", Label: "Cabin Hot", Message: "live today", Phase: alarmPhaseActive, State: alarmStateWarn, RaisedAt: raisedAt}}
	}
	_, old := runAlarmContext(t, deps, "old")
	if old.Alarm.Active || old.Alarm.Body != "old" || !strings.Contains(old.Alarm.ClearNote, "no clear was recorded") {
		t.Errorf("old row must stay itself: %+v", old.Alarm)
	}
	_, cur := runAlarmContext(t, deps, "new")
	if !cur.Alarm.Active || cur.Alarm.Body != "live today" {
		t.Errorf("the open occurrence is the live alarm: %+v", cur.Alarm)
	}
}

func TestGetAlarmContext_HistoryWindowMaths(t *testing.T) {
	raisedAt := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		clearedIn time.Duration
		nowIn     time.Duration
		wantEnd   time.Duration
	}{
		{"default is thirty minutes after", 0, 3 * time.Hour, 30 * time.Minute},
		{"extends to the clear", 90 * time.Minute, 5 * time.Hour, 90 * time.Minute},
		{"caps at two hours", 5 * time.Hour, 9 * time.Hour, 2 * time.Hour},
		{"clamps at now", 0, 20 * time.Minute, 20 * time.Minute},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			alarmProposalEnv(t)
			deps, store := askMateEnv(t, raisedAt)
			// The first/last query gets the requested window; the stat queries
			// get it narrowed to where data exists.
			type win struct{ start, stop time.Time }
			var calls []win
			deps.influxPathHistoryFirstLast = func(_ context.Context, _, _ string, start, stop time.Time) (time.Time, time.Time, bool, error) {
				calls = append(calls, win{start, stop})
				return time.Time{}, time.Time{}, false, nil
			}
			deps.now = func() time.Time { return raisedAt.Add(c.nowIn) }
			id, err := store.RecordRaised(alarmLogEntry{RuleID: "own-1", Label: "X", Path: "environment.outside.temperature", State: "warn", RaisedAt: raisedAt})
			if err != nil {
				t.Fatal(err)
			}
			if c.clearedIn > 0 {
				if err := store.MarkCleared("own-1", raisedAt.Add(c.clearedIn)); err != nil {
					t.Fatal(err)
				}
			}
			runAlarmContext(t, deps, id)
			if len(calls) == 0 {
				t.Fatal("history was not queried")
			}
			if !calls[0].start.Equal(raisedAt.Add(-30*time.Minute)) || !calls[0].stop.Equal(raisedAt.Add(c.wantEnd)) {
				t.Errorf("window %v - %v, want end raise+%v", calls[0].start, calls[0].stop, c.wantEnd)
			}
		})
	}
}

func TestGetAlarmContext_BuiltInCheckIsNotReportedAsDeleted(t *testing.T) {
	raisedAt := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	deps, store := askMateEnv(t, raisedAt)
	for i := 0; i < 30; i++ {
		id := raisedAt.Add(-time.Duration(i) * 5 * time.Hour)
		if _, err := store.RecordRaised(alarmLogEntry{RuleID: collisionProfileRuleID, Label: "Collision Profile Not Changed", State: "warn", Message: "x", RaisedAt: id}); err != nil {
			t.Fatal(err)
		}
		if err := store.MarkCleared(collisionProfileRuleID, id.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.RecordRaised(alarmLogEntry{RuleID: collisionProfileRuleID, Label: "Collision Profile Not Changed", State: "warn", Message: "x", RaisedAt: raisedAt}); err != nil {
		t.Fatal(err)
	}
	raw, _ := runAlarmContext(t, deps, collisionProfileRuleID)
	if strings.Contains(raw, "deleted") {
		t.Errorf("built-in check must not be called deleted: %s", raw)
	}
	var generic map[string]any
	_ = json.Unmarshal([]byte(raw), &generic)
	check, _ := generic["built_in_check"].(map[string]any)
	if check["kind"] != "built_in" || check["note"] == "" {
		t.Errorf("want built_in_check, got %v", generic["built_in_check"])
	}
	if generic["rule_absent"] != nil {
		t.Errorf("no rule_absent for a built-in check: %v", generic["rule_absent"])
	}
	occ, _ := generic["occurrences"].(map[string]any)
	pattern, _ := occ["pattern"].(string)
	if !strings.Contains(pattern, "31 times in 6 days") || !strings.Contains(pattern, "about 5 a day") {
		t.Errorf("want a repeats pattern, got %q", pattern)
	}
}

func TestGetAlarmContext_FewRaisesCarryNoPattern(t *testing.T) {
	raisedAt := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	deps, store := askMateEnv(t, raisedAt)
	if _, err := store.RecordRaised(alarmLogEntry{RuleID: "own-1", Label: "Cabin Hot", State: "warn", RaisedAt: raisedAt}); err != nil {
		t.Fatal(err)
	}
	raw, _ := runAlarmContext(t, deps, "own-1")
	if strings.Contains(raw, "pattern") {
		t.Errorf("one raise is no pattern: %s", raw)
	}
}

// Raised often over a short span, the rate is not given as a daily figure.
func TestGetAlarmContext_ShortSpanPatternSaysSinceNotPerDay(t *testing.T) {
	raisedAt := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	deps, store := askMateEnv(t, raisedAt)
	for i := 0; i < 25; i++ {
		at := raisedAt.Add(-time.Duration(i) * time.Minute)
		if _, err := store.RecordRaised(alarmLogEntry{RuleID: collisionProfileRuleID, Label: "Collision Profile Not Changed", State: "warn", RaisedAt: at}); err != nil {
			t.Fatal(err)
		}
		if err := store.MarkCleared(collisionProfileRuleID, at.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := runAlarmContext(t, deps, collisionProfileRuleID)
	var generic map[string]any
	_ = json.Unmarshal([]byte(raw), &generic)
	occ, _ := generic["occurrences"].(map[string]any)
	pattern, _ := occ["pattern"].(string)
	if !strings.HasPrefix(pattern, "raised 25 times since ") || strings.Contains(pattern, "a day") {
		t.Errorf("short span pattern: %q", pattern)
	}
}

// Every event a Helmcentral check builds for the alarm log must use an id the
// built-in list knows, or get_alarm_context calls the check a deleted rule.
func TestBuiltInAlarmCheckListCoversTheEventBuilders(t *testing.T) {
	events := []alarmEvent{
		collisionProfileEvent(alarmEventRaised, alarmStateWarn, "x"),
		anchorWatchStateEvent(alarmEventRaised, alarmStateWarn, "x"),
	}
	for _, e := range events {
		if !isBuiltInAlarmCheck(e.Rule.ID) || !isBuiltInAlarmCheck(e.Status.RuleID) {
			t.Errorf("event id %q is not in builtInAlarmCheckIDs", e.Rule.ID)
		}
	}
	for _, id := range []string{watchdogRuleID, anchorDragRuleID} {
		if !isBuiltInAlarmCheck(id) {
			t.Errorf("%q is not in builtInAlarmCheckIDs", id)
		}
	}
}

// A failed read of the first raise is reported, never swapped for a plainer
// pattern that hides it.
func TestAlarmRepeatPattern_ReadFailureIsAnError(t *testing.T) {
	store := newTestAlarmLog(t)
	if err := store.db.Close(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	pattern, err := assistantToolDeps{}.alarmRepeatPattern(store, collisionProfileRuleID, 25, now, func(t time.Time) string { return t.String() })
	if err == nil {
		t.Fatalf("want an error, got pattern %q", pattern)
	}
	if pattern != "" {
		t.Errorf("no pattern on a failed read, got %q", pattern)
	}
}
