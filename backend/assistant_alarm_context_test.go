package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type alarmContextOut struct {
	Alarm struct {
		Title    string `json:"title"`
		Body     string `json:"body"`
		State    string `json:"state"`
		Active   bool   `json:"active"`
		RaisedAt string `json:"raised_at"`
		Cleared  string `json:"cleared_at"`
		Source   string `json:"source"`
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
		return []alarmStatus{{RuleID: "signalk:notifications.radar.guardZone1", Label: "Radar Guard Zone 1", Path: "notifications.radar.guardZone1",
			Phase: alarmPhaseActive, State: alarmStateAlarm, Message: "Target in guard zone 1", RaisedAt: raisedAt}}
	}
	_, out := runAlarmContext(t, deps, "signalk:notifications.radar.guardZone1")
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
	deps.influxPathHistoryStat = nil
	deps.activeAlarms = func() []alarmStatus {
		return []alarmStatus{{RuleID: rule.ID, Label: "House Bank Low", Path: testSoCPath, Phase: alarmPhaseActive, State: alarmStateWarn, RaisedAt: raisedAt}}
	}
	_, out := runAlarmContext(t, deps, rule.ID)
	if out.History != nil || !strings.Contains(out.HistoryAbsent, "InfluxDB") {
		t.Errorf("want a history_absent naming InfluxDB, got %q", out.HistoryAbsent)
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
