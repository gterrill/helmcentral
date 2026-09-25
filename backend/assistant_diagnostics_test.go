package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// errNoVesselState stands in for "the vessel's position/state is not
// currently known" in get_path_history tests that don't care about the
// vessel's own local timezone - they just need vesselState to fail
// cleanly rather than panic on an unset dep.
var errNoVesselState = errors.New("vessel state unavailable in test")

// This file tests the three Mate diagnostic tools (ADR 0131):
// check_signalk_paths (executeCheckSignalKPaths, assistant_diagnostics.go),
// get_last_recorded and get_path_history, plus the Flux-building helpers in
// influx.go they depend on. Every test injects a fake dependency - no live
// SignalK connection or InfluxDB required, the same idiom
// assistant_tools_test.go already uses for the other tools.

// ── check_signalk_paths ─────────────────────────────────────────────────

// fixedSignalKDiagnostics returns a signalKDiagnostics dep func that ignores
// its `now` argument and always returns snap - a test double for
// executeCheckSignalKPaths that never touches globalSignalKSnapshot.
func fixedSignalKDiagnostics(snap signalKDiagnosticsSnapshot) func(time.Time) signalKDiagnosticsSnapshot {
	return func(time.Time) signalKDiagnosticsSnapshot { return snap }
}

func TestExecuteCheckSignalKPaths_NoSelfTreeErrorsExplicitly(t *testing.T) {
	deps := assistantToolDeps{
		now:                func() time.Time { return time.Now() },
		signalKDiagnostics: fixedSignalKDiagnostics(signalKDiagnosticsSnapshot{HasSelfTree: false}),
	}

	_, err := deps.execute(context.Background(), "check_signalk_paths", json.RawMessage(`{}`))
	if err == nil {
		t.Fatalf("expected an error when the snapshot has no self tree")
	}
	if !strings.Contains(err.Error(), "no self tree") {
		t.Errorf("expected the error to say there is no self tree, got %q", err.Error())
	}
}

func TestExecuteCheckSignalKPaths_NoFilterShowsConnectionAndSourcesOnly(t *testing.T) {
	now := time.Date(2026, 9, 25, 6, 0, 0, 0, time.UTC)
	lastMessage := now.Add(-3 * time.Second)
	snap := signalKDiagnosticsSnapshot{
		Connected:   true,
		LastMessage: lastMessage,
		HasSelfTree: true,
		Samples: []signalKPathSample{
			{Path: "tanks.fuel.2.currentLevel", Source: "YachtDevices.6", AgeSeconds: 4 * 24 * 3600},
			{Path: "navigation.speedOverGround", Source: "venus.com.victronenergy.gps", AgeSeconds: 1},
		},
	}
	deps := assistantToolDeps{
		now:                func() time.Time { return now },
		signalKDiagnostics: fixedSignalKDiagnostics(snap),
	}

	raw, err := deps.execute(context.Background(), "check_signalk_paths", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute check_signalk_paths: %v", err)
	}
	var result assistantCheckSignalKPathsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !result.Connection.Connected {
		t.Errorf("expected connected true")
	}
	if result.Connection.LastMessageAgoS == nil || *result.Connection.LastMessageAgoS != 3 {
		t.Errorf("expected last_message_ago_s 3, got %+v", result.Connection.LastMessageAgoS)
	}
	if len(result.Matches) != 0 {
		t.Errorf("expected no matches with no filter given, got %+v", result.Matches)
	}
	if result.Note == "" {
		t.Errorf("expected a note explaining no filter was given")
	}
	if len(result.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d: %+v", len(result.Sources), result.Sources)
	}
	// Sorted oldest (quietest) first.
	if result.Sources[0].Source != "YachtDevices.6" {
		t.Errorf("expected the quiet source first, got %+v", result.Sources)
	}
	if !result.Sources[0].Stale {
		t.Errorf("expected the 4-day-old source to be marked stale")
	}
	if result.Sources[1].Stale {
		t.Errorf("expected the 1-second-old source to not be stale")
	}
}

func TestExecuteCheckSignalKPaths_FilterMatchesPathsAcrossTree(t *testing.T) {
	now := time.Date(2026, 9, 25, 6, 0, 0, 0, time.UTC)
	snap := signalKDiagnosticsSnapshot{
		HasSelfTree: true,
		Samples: []signalKPathSample{
			{Path: "tanks.fuel.2.currentLevel", Value: 0.31, Units: "ratio", Source: "YachtDevices.6", Timestamp: "2026-09-21T00:30:00Z", AgeSeconds: 4 * 24 * 3600},
			{Path: "tanks.fuel.2.capacity", Value: 400.0, Source: "YachtDevices.6", AgeSeconds: 4 * 24 * 3600},
			{Path: "navigation.speedOverGround", Value: 6.2, Source: "venus.com.victronenergy.gps", AgeSeconds: 1},
		},
	}
	deps := assistantToolDeps{
		now:                func() time.Time { return now },
		signalKDiagnostics: fixedSignalKDiagnostics(snap),
	}

	raw, err := deps.execute(context.Background(), "check_signalk_paths", json.RawMessage(`{"filter":"tanks"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantCheckSignalKPathsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if result.MatchCount != 2 {
		t.Fatalf("expected 2 matches, got %d: %+v", result.MatchCount, result.Matches)
	}
	for _, m := range result.Matches {
		if !strings.Contains(m.Path, "tanks") {
			t.Errorf("expected only tanks.* paths, got %q", m.Path)
		}
		if !m.Stale {
			t.Errorf("expected %q to be marked stale at 4 days old", m.Path)
		}
	}
}

func TestExecuteCheckSignalKPaths_NoMatchSaysSoExplicitly(t *testing.T) {
	now := time.Date(2026, 9, 25, 6, 0, 0, 0, time.UTC)
	snap := signalKDiagnosticsSnapshot{
		HasSelfTree: true,
		Samples: []signalKPathSample{
			{Path: "navigation.speedOverGround", Source: "venus.com.victronenergy.gps", AgeSeconds: 1},
		},
	}
	deps := assistantToolDeps{
		now:                func() time.Time { return now },
		signalKDiagnostics: fixedSignalKDiagnostics(snap),
	}

	raw, err := deps.execute(context.Background(), "check_signalk_paths", json.RawMessage(`{"filter":"exhaust"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantCheckSignalKPathsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if result.MatchCount != 0 {
		t.Fatalf("expected no matches, got %d", result.MatchCount)
	}
	if !strings.Contains(result.Note, "not in the live SignalK tree") && !strings.Contains(result.Note, "no path containing") {
		t.Errorf("expected an explicit not-in-the-tree note, got %q", result.Note)
	}
}

func TestExecuteCheckSignalKPaths_SourceFilterIsCaseInsensitiveSubstring(t *testing.T) {
	now := time.Date(2026, 9, 25, 6, 0, 0, 0, time.UTC)
	snap := signalKDiagnosticsSnapshot{
		HasSelfTree: true,
		Samples: []signalKPathSample{
			{Path: "tanks.fuel.2.currentLevel", Source: "YachtDevices.6", AgeSeconds: 1},
			{Path: "navigation.speedOverGround", Source: "venus.com.victronenergy.gps", AgeSeconds: 1},
		},
	}
	deps := assistantToolDeps{
		now:                func() time.Time { return now },
		signalKDiagnostics: fixedSignalKDiagnostics(snap),
	}

	raw, err := deps.execute(context.Background(), "check_signalk_paths", json.RawMessage(`{"source":"yachtdevices"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantCheckSignalKPathsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.MatchCount != 1 || result.Matches[0].Path != "tanks.fuel.2.currentLevel" {
		t.Fatalf("expected exactly the YachtDevices path, got %+v", result.Matches)
	}
}

func TestExecuteCheckSignalKPaths_TruncatesAtMaxMatches(t *testing.T) {
	now := time.Date(2026, 9, 25, 6, 0, 0, 0, time.UTC)
	samples := make([]signalKPathSample, 0, assistantCheckSignalKPathsMaxMatches+5)
	for i := 0; i < assistantCheckSignalKPathsMaxMatches+5; i++ {
		samples = append(samples, signalKPathSample{Path: "electrical.batteries.test", AgeSeconds: 1})
	}
	deps := assistantToolDeps{
		now:                func() time.Time { return now },
		signalKDiagnostics: fixedSignalKDiagnostics(signalKDiagnosticsSnapshot{HasSelfTree: true, Samples: samples}),
	}

	raw, err := deps.execute(context.Background(), "check_signalk_paths", json.RawMessage(`{"filter":"electrical"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantCheckSignalKPathsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.MatchCount != assistantCheckSignalKPathsMaxMatches+5 {
		t.Errorf("expected match_count to report the true total, got %d", result.MatchCount)
	}
	if len(result.Matches) != assistantCheckSignalKPathsMaxMatches {
		t.Errorf("expected matches capped at %d, got %d", assistantCheckSignalKPathsMaxMatches, len(result.Matches))
	}
	if !result.Truncated {
		t.Errorf("expected truncated true")
	}
}

func TestSummarizeSignalKSources_SortsQuietSourcesFirstAndHandlesUnknownAge(t *testing.T) {
	samples := []signalKPathSample{
		{Path: "a", Source: "venus.com.victronenergy.gps", AgeSeconds: 1},
		{Path: "b", Source: "YachtDevices.6", AgeSeconds: 4 * 24 * 3600},
		{Path: "c", Source: "YachtDevices.6", AgeSeconds: 5 * 24 * 3600}, // freshest of this source's own paths wins
		{Path: "d", Source: "", AgeSeconds: -1},                          // no source declared, no evidence of age
	}
	out := summarizeSignalKSources(samples, time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC))

	if len(out) != 3 {
		t.Fatalf("expected 3 distinct source labels, got %d: %+v", len(out), out)
	}
	if out[0].Source != "YachtDevices.6" {
		t.Fatalf("expected YachtDevices.6 first (oldest), got %+v", out)
	}
	if out[0].AgeSeconds == nil || *out[0].AgeSeconds != 4*24*3600 {
		t.Errorf("expected the FRESHEST of YachtDevices.6's own paths (4 days, not 5), got %+v", out[0].AgeSeconds)
	}
	if out[0].PathCount != 2 {
		t.Errorf("expected path_count 2 for YachtDevices.6, got %d", out[0].PathCount)
	}
	if !out[0].Stale {
		t.Errorf("expected YachtDevices.6 to be stale")
	}
	last := out[len(out)-1]
	if last.Source != "(unknown source)" {
		t.Errorf("expected the no-evidence source to sort last, got %+v", out)
	}
	if last.AgeSeconds != nil {
		t.Errorf("expected no age for a source with no timestamp evidence at all, got %v", *last.AgeSeconds)
	}
}

// ── collectSignalKPathSamples: node timestamp over pathSeen ─────────────

// TestCollectSignalKPathSamples_PrefersNodeTimestampOverPathSeen exercises
// the exact scenario signalk_paths.go's pathAge comment documents: SignalK
// replays its retained model on reconnect, which resets pathSeen (arrival
// time) to "now" for a path that has not actually reported since - the
// node's own declared timestamp must win, or a source dead for days would
// read as freshly updated on every reconnect.
func TestCollectSignalKPathSamples_PrefersNodeTimestampOverPathSeen(t *testing.T) {
	snapshot := newSignalKSnapshot()
	oldTimestamp := "2026-09-21T00:30:00.000Z"
	// The delta's own receive time ("now" at apply time) simulates a replay
	// arriving well after the path's last real update - pathSeen records
	// this arrival time, but the node's own timestamp says otherwise.
	replayArrival := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{
			SourceRef: "YachtDevices.6",
			Timestamp: oldTimestamp,
			Values:    []signalKValue{{Path: "tanks.fuel.2.currentLevel", Value: 0.31}},
		}},
	}, replayArrival)
	snapshot.setSelfContext("vessels.self")

	now := time.Date(2026, 9, 25, 6, 0, 0, 0, time.UTC)
	samples := collectSignalKPathSamples(snapshot, "vessels.self", snapshot.selfTree(), now)
	if len(samples) != 1 {
		t.Fatalf("expected 1 sample, got %d", len(samples))
	}
	s := samples[0]

	// If pathSeen (the replay arrival time) won, the age would be about 6
	// hours (now - replayArrival). The node's own timestamp is ~4.2 days old.
	wantAge := now.Sub(mustParseRFC3339(t, oldTimestamp)).Seconds()
	if s.AgeSeconds < wantAge-1 || s.AgeSeconds > wantAge+1 {
		t.Errorf("expected age from the node's own timestamp (~%.0fs), got %.0fs - pathSeen may have won instead", wantAge, s.AgeSeconds)
	}
}

func mustParseRFC3339(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return parsed
}

// ── get_last_recorded ────────────────────────────────────────────────────

func TestExecuteGetLastRecorded_RequiresPathPrefixOrSource(t *testing.T) {
	deps := assistantToolDeps{now: func() time.Time { return time.Now() }}
	_, err := deps.execute(context.Background(), "get_last_recorded", json.RawMessage(`{}`))
	if err == nil {
		t.Fatalf("expected an error when neither path_prefix nor source is given")
	}
	if !strings.Contains(err.Error(), "path_prefix or source") {
		t.Errorf("expected the error to name the missing arguments, got %q", err.Error())
	}
}

func TestExecuteGetLastRecorded_ReturnsRowsSortedOldestFirst(t *testing.T) {
	now := time.Date(2026, 9, 25, 0, 34, 0, 0, time.UTC)
	stopped := time.Date(2026, 9, 21, 0, 34, 0, 0, time.UTC) // 4 days before now
	stillFresh := now.Add(-2 * time.Minute)

	var captured struct {
		prefix, source string
		lookback       int
	}
	deps := assistantToolDeps{
		now: func() time.Time { return now },
		influxLastRecorded: func(pathPrefix, source string, lookbackDays int) ([]influxLastRecordedRow, error) {
			captured.prefix, captured.source, captured.lookback = pathPrefix, source, lookbackDays
			return []influxLastRecordedRow{
				{Path: "tanks.fuel.7.currentLevel", Source: "YachtDevices.7", Time: stopped, Value: 0.42},
				{Path: "tanks.freshWater.0.currentLevel", Source: "YachtDevices.6", Time: stillFresh, Value: 0.88},
			}, nil
		},
	}

	raw, err := deps.execute(context.Background(), "get_last_recorded", json.RawMessage(`{"source":"YachtDevices"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if captured.source != "YachtDevices" {
		t.Errorf("expected source %q passed through, got %q", "YachtDevices", captured.source)
	}
	if captured.lookback != assistantLastRecordedDefaultLookbackDays {
		t.Errorf("expected default lookback %d, got %d", assistantLastRecordedDefaultLookbackDays, captured.lookback)
	}

	var result assistantGetLastRecordedResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result.Rows))
	}
	if result.Rows[0].Path != "tanks.fuel.7.currentLevel" {
		t.Errorf("expected the 4-day-stale row first (oldest), got %+v", result.Rows)
	}
	if result.Rows[0].AgeDays < 3.9 || result.Rows[0].AgeDays > 4.1 {
		t.Errorf("expected age_days ~4, got %v", result.Rows[0].AgeDays)
	}
	if result.Rows[0].LastSeenAt != stopped.Format(time.RFC3339) {
		t.Errorf("expected last_seen_at %q, got %q", stopped.Format(time.RFC3339), result.Rows[0].LastSeenAt)
	}
}

func TestExecuteGetLastRecorded_ClampsLookbackDaysToMax(t *testing.T) {
	var captured int
	deps := assistantToolDeps{
		now: func() time.Time { return time.Now() },
		influxLastRecorded: func(pathPrefix, source string, lookbackDays int) ([]influxLastRecordedRow, error) {
			captured = lookbackDays
			return nil, nil
		},
	}
	_, err := deps.execute(context.Background(), "get_last_recorded", json.RawMessage(`{"path_prefix":"tanks","lookback_days":100000}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if captured != assistantLastRecordedMaxLookbackDays {
		t.Errorf("expected lookback clamped to %d, got %d", assistantLastRecordedMaxLookbackDays, captured)
	}
}

func TestExecuteGetLastRecorded_InfluxNotConfiguredReturnsExplicitError(t *testing.T) {
	deps := assistantToolDeps{
		now: func() time.Time { return time.Now() },
		influxLastRecorded: func(pathPrefix, source string, lookbackDays int) ([]influxLastRecordedRow, error) {
			return nil, errInfluxNotConfiguredForTest
		},
	}
	_, err := deps.execute(context.Background(), "get_last_recorded", json.RawMessage(`{"path_prefix":"tanks"}`))
	if err == nil {
		t.Fatalf("expected an error when influx is not configured")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("expected the error to say influx is not configured, got %q", err.Error())
	}
}

func TestExecuteGetLastRecorded_NoRowsAddsExplicitNote(t *testing.T) {
	deps := assistantToolDeps{
		now: func() time.Time { return time.Now() },
		influxLastRecorded: func(pathPrefix, source string, lookbackDays int) ([]influxLastRecordedRow, error) {
			return nil, nil
		},
	}
	raw, err := deps.execute(context.Background(), "get_last_recorded", json.RawMessage(`{"path_prefix":"nonexistent"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetLastRecordedResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Note == "" {
		t.Errorf("expected a note when nothing matched")
	}
}

var errInfluxNotConfiguredForTest = &influxNotConfiguredTestError{}

type influxNotConfiguredTestError struct{}

func (e *influxNotConfiguredTestError) Error() string { return "influxdb is not configured" }

// ── get_path_history ─────────────────────────────────────────────────────

// stubInfluxPathHistoryStat builds a get_path_history dep from a fixed
// min/mean/max series per aggFn ("min"/"mean"/"max"), and records the last
// call's arguments for assertions.
type capturedPathHistoryCall struct {
	path, source string
	start, stop  time.Time
	every, aggFn string
}

func stubInfluxPathHistoryStat(series map[string][]telemetryPoint, calls *[]capturedPathHistoryCall) func(path, source string, start, stop time.Time, every, aggFn string) ([]telemetryPoint, error) {
	return func(path, source string, start, stop time.Time, every, aggFn string) ([]telemetryPoint, error) {
		if calls != nil {
			*calls = append(*calls, capturedPathHistoryCall{path, source, start, stop, every, aggFn})
		}
		return series[aggFn], nil
	}
}

func TestExecuteGetPathHistory_RequiresPath(t *testing.T) {
	deps := assistantToolDeps{now: func() time.Time { return time.Now() }}
	_, err := deps.execute(context.Background(), "get_path_history", json.RawMessage(`{}`))
	if err == nil {
		t.Fatalf("expected an error when path is missing")
	}
}

func TestExecuteGetPathHistory_DefaultsToLast24Hours(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	var calls []capturedPathHistoryCall
	deps := assistantToolDeps{
		now:                   func() time.Time { return now },
		vesselState:           func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
		influxPathHistoryStat: stubInfluxPathHistoryStat(nil, &calls),
	}
	_, err := deps.execute(context.Background(), "get_path_history", json.RawMessage(`{"path":"tanks.fuel.2.currentLevel"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("expected 3 stat calls (min/mean/max), got %d", len(calls))
	}
	for _, c := range calls {
		if !c.start.Equal(now.Add(-24 * time.Hour)) {
			t.Errorf("expected start 24h before now, got %v", c.start)
		}
		if !c.stop.Equal(now) {
			t.Errorf("expected stop == now, got %v", c.stop)
		}
		if c.every != "15m" {
			t.Errorf("expected 15m bucket width for a 24h span, got %q", c.every)
		}
	}
}

func TestExecuteGetPathHistory_HoursBackOverridesDefault(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	var calls []capturedPathHistoryCall
	deps := assistantToolDeps{
		now:                   func() time.Time { return now },
		vesselState:           func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
		influxPathHistoryStat: stubInfluxPathHistoryStat(nil, &calls),
	}
	_, err := deps.execute(context.Background(), "get_path_history", json.RawMessage(`{"path":"tanks.fuel.2.currentLevel","hours_back":2}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !calls[0].start.Equal(now.Add(-2 * time.Hour)) {
		t.Errorf("expected start 2h before now, got %v", calls[0].start)
	}
	if calls[0].every != "1m" {
		t.Errorf("expected 1m bucket width for a 2h span, got %q", calls[0].every)
	}
}

func TestExecuteGetPathHistory_StartEndMustBeRFC3339(t *testing.T) {
	deps := assistantToolDeps{now: func() time.Time { return time.Now() }}
	_, err := deps.execute(context.Background(), "get_path_history", json.RawMessage(`{"path":"x","start":"not-a-time","end":"2026-09-22T00:00:00Z"}`))
	if err == nil {
		t.Fatalf("expected an error for a malformed start time")
	}
	if !strings.Contains(err.Error(), "RFC3339") {
		t.Errorf("expected the error to mention RFC3339, got %q", err.Error())
	}
}

func TestExecuteGetPathHistory_EndMustBeAfterStart(t *testing.T) {
	deps := assistantToolDeps{now: func() time.Time { return time.Now() }}
	_, err := deps.execute(context.Background(), "get_path_history", json.RawMessage(
		`{"path":"x","start":"2026-09-22T00:00:00Z","end":"2026-09-21T00:00:00Z"}`))
	if err == nil {
		t.Fatalf("expected an error when end is before start")
	}
}

func TestExecuteGetPathHistory_ClampsSpanToMax90Days(t *testing.T) {
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	var calls []capturedPathHistoryCall
	deps := assistantToolDeps{
		now:                   func() time.Time { return now },
		vesselState:           func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
		influxPathHistoryStat: stubInfluxPathHistoryStat(nil, &calls),
	}
	start := "2020-01-01T00:00:00Z"
	end := now.Format(time.RFC3339)
	_, err := deps.execute(context.Background(), "get_path_history", json.RawMessage(`{"path":"x","start":"`+start+`","end":"`+end+`"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	span := calls[0].stop.Sub(calls[0].start)
	if span > assistantPathHistoryMaxSpan {
		t.Errorf("expected span capped at %v, got %v", assistantPathHistoryMaxSpan, span)
	}
}

func TestExecuteGetPathHistory_RejectsFluxInjectionInPathAndSource(t *testing.T) {
	deps := assistantToolDeps{now: func() time.Time { return time.Now() }}
	for _, args := range []string{
		`{"path":"tanks.${r._measurement}"}`,
		`{"path":"tanks.fuel.2.currentLevel","source":"${evil}"}`,
	} {
		_, err := deps.execute(context.Background(), "get_path_history", json.RawMessage(args))
		if err == nil {
			t.Errorf("expected an error for hostile input %s", args)
		}
	}
}

func TestAssistantPathHistoryBucketWidth_FixedAllowlist(t *testing.T) {
	cases := []struct {
		span time.Duration
		want string
	}{
		{time.Hour, "1m"},
		{3 * time.Hour, "1m"},
		{4 * time.Hour, "5m"},
		{12 * time.Hour, "5m"},
		{13 * time.Hour, "15m"},
		{24 * time.Hour, "15m"},
		{2 * 24 * time.Hour, "30m"},
		{3 * 24 * time.Hour, "30m"},
		{5 * 24 * time.Hour, "1h"},
		{7 * 24 * time.Hour, "1h"},
		{20 * 24 * time.Hour, "4h"},
		{30 * 24 * time.Hour, "4h"},
		{91 * 24 * time.Hour, "12h"},
	}
	for _, c := range cases {
		got, _ := assistantPathHistoryBucketWidth(c.span)
		if got != c.want {
			t.Errorf("span %v: expected bucket %q, got %q", c.span, c.want, got)
		}
	}
}

func TestMergePathHistoryPoints_JoinsByTimestamp(t *testing.T) {
	t0 := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	minPts := []telemetryPoint{{Timestamp: t0, Value: 1}, {Timestamp: t1, Value: 2}}
	meanPts := []telemetryPoint{{Timestamp: t0, Value: 1.5}, {Timestamp: t1, Value: 2.5}}
	maxPts := []telemetryPoint{{Timestamp: t0, Value: 2}} // t1 missing from max on purpose

	merged := mergePathHistoryPoints(minPts, meanPts, maxPts)
	if len(merged) != 2 {
		t.Fatalf("expected 2 merged buckets, got %d", len(merged))
	}
	if merged[0].Min == nil || *merged[0].Min != 1 || merged[0].Mean == nil || *merged[0].Mean != 1.5 || merged[0].Max == nil || *merged[0].Max != 2 {
		t.Errorf("bucket 0 not merged correctly: %+v", merged[0])
	}
	if merged[1].Max != nil {
		t.Errorf("expected bucket 1's max to stay nil (no max point at t1), got %v", *merged[1].Max)
	}
}

func TestComputePathHistoryGaps_FindsGapsAndTrueCount(t *testing.T) {
	start := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	stop := start.Add(4 * time.Hour)
	width := time.Hour
	// Present at hour 0 and hour 3; missing hours 1 and 2 (one gap of 2 steps).
	points := []assistantPathHistoryPoint{
		{T: start},
		{T: start.Add(3 * time.Hour)},
	}
	gaps, count := computePathHistoryGaps(start, stop, width, points)
	if count != 2 {
		t.Errorf("expected gap_count 2, got %d", count)
	}
	if len(gaps) != 1 {
		t.Fatalf("expected 1 gap range, got %d: %+v", len(gaps), gaps)
	}
	wantFrom := start.Add(time.Hour).UTC().Format(time.RFC3339)
	wantTo := start.Add(3 * time.Hour).UTC().Format(time.RFC3339)
	if gaps[0].From != wantFrom || gaps[0].To != wantTo {
		t.Errorf("expected gap [%s, %s), got [%s, %s)", wantFrom, wantTo, gaps[0].From, gaps[0].To)
	}
}

func TestExecuteGetPathHistory_ComputesOverallStatsAndFirstLastSeen(t *testing.T) {
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	t0 := now.Add(-2 * time.Hour)
	t1 := now.Add(-time.Hour)

	series := map[string][]telemetryPoint{
		"min":  {{Timestamp: t0, Value: 0.30}, {Timestamp: t1, Value: 0.28}},
		"mean": {{Timestamp: t0, Value: 0.31}, {Timestamp: t1, Value: 0.29}},
		"max":  {{Timestamp: t0, Value: 0.32}, {Timestamp: t1, Value: 0.30}},
	}
	deps := assistantToolDeps{
		now:                   func() time.Time { return now },
		vesselState:           func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
		influxPathHistoryStat: stubInfluxPathHistoryStat(series, nil),
	}

	raw, err := deps.execute(context.Background(), "get_path_history", json.RawMessage(`{"path":"tanks.fuel.2.currentLevel","hours_back":3}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetPathHistoryResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.OverallMin == nil || *result.OverallMin != 0.28 {
		t.Errorf("expected overall_min 0.28, got %+v", result.OverallMin)
	}
	if result.OverallMax == nil || *result.OverallMax != 0.32 {
		t.Errorf("expected overall_max 0.32, got %+v", result.OverallMax)
	}
	if result.OverallMean == nil || *result.OverallMean != 0.30 {
		t.Errorf("expected overall_mean 0.30, got %+v", result.OverallMean)
	}
	if result.FirstSeenISO != t0.UTC().Format(time.RFC3339) {
		t.Errorf("expected first_seen_iso %s, got %s", t0.UTC().Format(time.RFC3339), result.FirstSeenISO)
	}
	if result.LastSeenISO != t1.UTC().Format(time.RFC3339) {
		t.Errorf("expected last_seen_iso %s, got %s", t1.UTC().Format(time.RFC3339), result.LastSeenISO)
	}
	if len(result.Buckets) != 2 {
		t.Errorf("expected 2 buckets, got %d", len(result.Buckets))
	}
}

// TestExecuteGetPathHistory_NoGapsWhenFullyPopulated is the regression test
// for the timeSrc: "_start" fix in buildInfluxPathStatFlux (a code-review
// finding, 2026-09-25): InfluxDB's aggregateWindow labels each bucket with
// its STOP time by default, one whole bucket width later than
// computePathHistoryGaps' own boundary convention (a point's timestamp
// truncated down, starting from range.start) - without requesting
// timeSrc: "_start" explicitly, this test would see a spurious gap_count of
// 1 (a false gap covering the range's first bucket) even though every
// bucket in [start, stop) has a point, because none of the stub's points
// would land on the boundary the gap loop's first step checks.
func TestExecuteGetPathHistory_NoGapsWhenFullyPopulated(t *testing.T) {
	now := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)
	start := now.Add(-time.Hour) // hours_back:1 -> "1m" bucket width
	width := time.Minute

	// One point at every bucket START boundary from start up to (but not
	// including) stop - exactly what a fully-populated real InfluxDB series
	// looks like once buildInfluxPathStatFlux's timeSrc: "_start" request is
	// honoured.
	var pts []telemetryPoint
	for t := start; t.Before(now); t = t.Add(width) {
		pts = append(pts, telemetryPoint{Timestamp: t, Value: 1})
	}
	series := map[string][]telemetryPoint{"min": pts, "mean": pts, "max": pts}

	deps := assistantToolDeps{
		now:                   func() time.Time { return now },
		vesselState:           func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
		influxPathHistoryStat: stubInfluxPathHistoryStat(series, nil),
	}
	raw, err := deps.execute(context.Background(), "get_path_history", json.RawMessage(`{"path":"x","hours_back":1}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetPathHistoryResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.GapCount != 0 {
		t.Errorf("expected gap_count 0 for a fully-populated range, got %d: %+v", result.GapCount, result.Gaps)
	}
	if len(result.Gaps) != 0 {
		t.Errorf("expected no gaps reported, got %+v", result.Gaps)
	}
}

func TestExecuteGetPathHistory_NoDataAddsExplicitNote(t *testing.T) {
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	deps := assistantToolDeps{
		now:                   func() time.Time { return now },
		vesselState:           func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
		influxPathHistoryStat: stubInfluxPathHistoryStat(nil, nil),
	}
	raw, err := deps.execute(context.Background(), "get_path_history", json.RawMessage(`{"path":"tanks.fuel.2.currentLevel"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetPathHistoryResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Note == "" {
		t.Errorf("expected a note when no data was found")
	}
	if result.FirstSeen != "" || result.LastSeen != "" {
		t.Errorf("expected no first/last seen when there is no data, got %+v / %+v", result.FirstSeen, result.LastSeen)
	}
}

// ── Flux builders (unit-tested independently of a live InfluxDB) ────────

func TestBuildInfluxLastRecordedFlux_IncludesHasPrefixForPathAndSource(t *testing.T) {
	flux, err := buildInfluxLastRecordedFlux("SignalK_Data", "value", "tanks", "YachtDevices", 30)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, want := range []string{
		`import "strings"`,
		`strings.hasPrefix(v: r._measurement, prefix: "tanks")`,
		`strings.hasPrefix(v: r.source, prefix: "YachtDevices")`,
		`range(start: -30d)`,
		`group(columns: ["_measurement", "source"])`,
		`|> last()`,
	} {
		if !strings.Contains(flux, want) {
			t.Errorf("expected flux to contain %q, got:\n%s", want, flux)
		}
	}
}

func TestBuildInfluxLastRecordedFlux_RequiresPathPrefixOrSource(t *testing.T) {
	_, err := buildInfluxLastRecordedFlux("SignalK_Data", "value", "", "", 30)
	if err == nil {
		t.Fatalf("expected an error with neither path_prefix nor source")
	}
}

func TestBuildInfluxLastRecordedFlux_RejectsHostileInterpolationSyntax(t *testing.T) {
	for _, tc := range []struct{ prefix, source string }{
		{"tanks.${r._measurement}", ""},
		{"", "${evil}"},
	} {
		_, err := buildInfluxLastRecordedFlux("SignalK_Data", "value", tc.prefix, tc.source, 30)
		if err == nil {
			t.Errorf("expected an error for hostile input %+v", tc)
		}
	}
}

func TestBuildInfluxPathStatFlux_IncludesSourceFilterOnlyWhenGiven(t *testing.T) {
	start := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	stop := start.Add(time.Hour)

	withSource, err := buildInfluxPathStatFlux("SignalK_Data", "value", "tanks.fuel.2.currentLevel", "YachtDevices.6", start, stop, "1m", "mean")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(withSource, `r.source == "YachtDevices.6"`) {
		t.Errorf("expected an exact source filter, got:\n%s", withSource)
	}
	if !strings.Contains(withSource, `fn: mean`) {
		t.Errorf("expected fn: mean, got:\n%s", withSource)
	}
	// timeSrc: "_start" is load-bearing, not cosmetic: Flux's own default
	// (timeSrc: "_stop") labels each bucket with its STOP time, one whole
	// bucket width later than what computePathHistoryGaps assumes when it
	// truncates a point's own timestamp down to a boundary - without this,
	// every get_path_history call reports a false gap covering the first
	// bucket of the range, regardless of actual data completeness (see
	// TestExecuteGetPathHistory_NoGapsWhenFullyPopulated).
	if !strings.Contains(withSource, `timeSrc: "_start"`) {
		t.Errorf(`expected aggregateWindow to request timeSrc: "_start", got:\n%s`, withSource)
	}

	withoutSource, err := buildInfluxPathStatFlux("SignalK_Data", "value", "tanks.fuel.2.currentLevel", "", start, stop, "1m", "max")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if strings.Contains(withoutSource, "r.source") {
		t.Errorf("expected no source filter when source is empty, got:\n%s", withoutSource)
	}
}

func TestBuildInfluxPathStatFlux_RejectsHostileInterpolationInPath(t *testing.T) {
	start := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	_, err := buildInfluxPathStatFlux("SignalK_Data", "value", "tanks.${r._measurement}", "", start, start.Add(time.Hour), "1m", "mean")
	if err == nil {
		t.Fatalf("expected an error for hostile path input")
	}
}
