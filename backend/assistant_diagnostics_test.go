package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
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
// min/mean/max series per aggFn ("min"/"mean"/"max"), and records each
// call's arguments for assertions. executeGetPathHistory now fires its
// min/mean/max (and first/last) queries concurrently (a code-review
// finding, 2026-09-25), so appends to calls go through a mutex - a plain
// slice append from concurrent goroutines is a data race.
type capturedPathHistoryCall struct {
	path, source string
	start, stop  time.Time
	every, aggFn string
}

func stubInfluxPathHistoryStat(series map[string][]telemetryPoint, calls *[]capturedPathHistoryCall) func(ctx context.Context, path, source string, start, stop time.Time, every, aggFn string) ([]telemetryPoint, error) {
	var mu sync.Mutex
	return func(ctx context.Context, path, source string, start, stop time.Time, every, aggFn string) ([]telemetryPoint, error) {
		if calls != nil {
			mu.Lock()
			*calls = append(*calls, capturedPathHistoryCall{path, source, start, stop, every, aggFn})
			mu.Unlock()
		}
		return series[aggFn], nil
	}
}

// stubInfluxPathHistoryFirstLast builds a get_path_history
// influxPathHistoryFirstLast dependency returning a fixed first/last pair -
// the actual-sample-time counterpart to stubInfluxPathHistoryStat's bucketed
// series (see queryInfluxPathFirstLast's own doc comment, influx.go, for why
// get_path_history needs both, and TestExecuteGetPathHistory_
// ComputesOverallStatsAndFirstLastSeen for why they can legitimately differ).
func stubInfluxPathHistoryFirstLast(first, last time.Time, found bool) func(ctx context.Context, path, source string, start, stop time.Time) (time.Time, time.Time, bool, error) {
	return func(ctx context.Context, path, source string, start, stop time.Time) (time.Time, time.Time, bool, error) {
		return first, last, found, nil
	}
}

// stubInfluxPathHistoryFirstLastNotFound is stubInfluxPathHistoryFirstLast's
// "no raw samples in range" case, for tests that only care about the
// bucketed series and not first/last seen.
func stubInfluxPathHistoryFirstLastNotFound(ctx context.Context, path, source string, start, stop time.Time) (time.Time, time.Time, bool, error) {
	return time.Time{}, time.Time{}, false, nil
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
		now:                        func() time.Time { return now },
		vesselState:                func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
		influxPathHistoryStat:      stubInfluxPathHistoryStat(nil, &calls),
		influxPathHistoryFirstLast: stubInfluxPathHistoryFirstLastNotFound,
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
		if c.every != "30m" {
			t.Errorf("expected 30m bucket width for a 24h span, got %q", c.every)
		}
	}
}

func TestExecuteGetPathHistory_HoursBackOverridesDefault(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	var calls []capturedPathHistoryCall
	deps := assistantToolDeps{
		now:                        func() time.Time { return now },
		vesselState:                func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
		influxPathHistoryStat:      stubInfluxPathHistoryStat(nil, &calls),
		influxPathHistoryFirstLast: stubInfluxPathHistoryFirstLastNotFound,
	}
	_, err := deps.execute(context.Background(), "get_path_history", json.RawMessage(`{"path":"tanks.fuel.2.currentLevel","hours_back":2}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !calls[0].start.Equal(now.Add(-2 * time.Hour)) {
		t.Errorf("expected start 2h before now, got %v", calls[0].start)
	}
	if calls[0].every != "5m" {
		t.Errorf("expected 5m bucket width for a 2h span, got %q", calls[0].every)
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
		now:                        func() time.Time { return now },
		vesselState:                func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
		influxPathHistoryStat:      stubInfluxPathHistoryStat(nil, &calls),
		influxPathHistoryFirstLast: stubInfluxPathHistoryFirstLastNotFound,
	}
	start := "2020-01-01T00:00:00Z"
	end := now.Format(time.RFC3339)
	raw, err := deps.execute(context.Background(), "get_path_history", json.RawMessage(`{"path":"x","start":"`+start+`","end":"`+end+`"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	span := calls[0].stop.Sub(calls[0].start)
	if span > assistantPathHistoryMaxSpan {
		t.Errorf("expected span capped at %v, got %v", assistantPathHistoryMaxSpan, span)
	}

	// A code-review finding (2026-09-25): the clamp used to happen silently -
	// the model asked for a 2020 start and had no way to tell it got 2026-06-27
	// instead short of noticing the returned start_iso didn't match its own
	// request.
	var result assistantGetPathHistoryResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(result.Note, "2020-01-01T00:00:00Z") {
		t.Errorf("expected the note to name the originally-requested start, got %q", result.Note)
	}
	if !strings.Contains(result.Note, result.StartISO) {
		t.Errorf("expected the note to name the moved-to start %s, got %q", result.StartISO, result.Note)
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
		{3 * time.Hour, "5m"},
		{12 * time.Hour, "15m"},
		{13 * time.Hour, "30m"},
		{24 * time.Hour, "30m"},
		{2 * 24 * time.Hour, "2h"},
		{3 * 24 * time.Hour, "2h"},
		{5 * 24 * time.Hour, "4h"},
		{7 * 24 * time.Hour, "4h"},
		{20 * 24 * time.Hour, "12h"},
		{30 * 24 * time.Hour, "12h"},
		{91 * 24 * time.Hour, "2d"},
	}
	for _, c := range cases {
		got, _ := assistantPathHistoryBucketWidth(c.span)
		if got != c.want {
			t.Errorf("span %v: expected bucket %q, got %q", c.span, c.want, got)
		}
	}
}

// TestAssistantPathHistoryBucketWidth_NeverExceeds60BucketsAtTierUpperBound
// is the direct regression test for the code-review finding (2026-09-25)
// that the old allowlist could produce far more buckets than fit
// assistantMaxToolResultChars (a 3h request alone produced 180 buckets,
// ~25KB) - checked at every tier's own upper-bound span, the worst case for
// that tier's chosen width.
func TestAssistantPathHistoryBucketWidth_NeverExceeds60BucketsAtTierUpperBound(t *testing.T) {
	const maxBuckets = 60
	upperBounds := []time.Duration{
		time.Hour, 3 * time.Hour, 12 * time.Hour, 24 * time.Hour,
		3 * 24 * time.Hour, 7 * 24 * time.Hour, 30 * 24 * time.Hour, assistantPathHistoryMaxSpan,
	}
	for _, span := range upperBounds {
		_, width := assistantPathHistoryBucketWidth(span)
		count := int(span / width)
		if count > maxBuckets {
			t.Errorf("span %v: width %v produces %d buckets, over the %d-bucket target", span, width, count, maxBuckets)
		}
	}
}

// TestDropOldestPathHistoryBucket_DropsFromTheFrontNotTheBack is the direct
// regression test for a code-review finding (2026-09-25): the original
// get_path_history shrink dropped buckets from the END of the
// chronologically-ascending list, discarding the MOST RECENT part of the
// range first - exactly backwards for a tool whose whole point is "what did
// this do right before it stopped".
func TestDropOldestPathHistoryBucket_DropsFromTheFrontNotTheBack(t *testing.T) {
	buckets := []assistantPathHistoryBucket{
		{T: "2026-09-21T00:00:00Z"},
		{T: "2026-09-21T01:00:00Z"},
		{T: "2026-09-22T00:00:00Z"}, // the most recent - must survive a shrink
	}
	got := dropOldestPathHistoryBucket(buckets)
	if len(got) != 2 {
		t.Fatalf("expected exactly one bucket dropped, got %d remaining: %+v", len(got), got)
	}
	if got[len(got)-1].T != "2026-09-22T00:00:00Z" {
		t.Errorf("expected the most recent bucket to survive, got %+v", got)
	}
	for _, b := range got {
		if b.T == "2026-09-21T00:00:00Z" {
			t.Errorf("expected the OLDEST bucket to be the one dropped, but it is still present: %+v", got)
		}
	}
}

// parseFluxEveryDurationForTest parses an "every" string the way Go's
// time.ParseDuration would, except it also accepts Flux's own "d" (day)
// suffix, which Go's parser does not -
// assistantPathHistoryBucketWidth's longest tier uses "2d".
func parseFluxEveryDurationForTest(t *testing.T, every string) time.Duration {
	t.Helper()
	if strings.HasSuffix(every, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(every, "d"))
		if err != nil {
			t.Fatalf("parse %q as a day count: %v", every, err)
		}
		return time.Duration(n) * 24 * time.Hour
	}
	d, err := time.ParseDuration(every)
	if err != nil {
		t.Fatalf("parse %q as a Go duration: %v", every, err)
	}
	return d
}

// stubInfluxPathHistoryStatFullyPopulated returns an influxPathHistoryStat
// dependency that generates one point at every bucket-start boundary across
// whatever [start, stop) range and every width it is actually called with
// (min/mean/max all identical), so a test can assert against a
// fully-populated series without hardcoding the exact bucket width an
// allowlist will choose ahead of time.
func stubInfluxPathHistoryStatFullyPopulated(t *testing.T) func(ctx context.Context, path, source string, start, stop time.Time, every, aggFn string) ([]telemetryPoint, error) {
	return func(ctx context.Context, path, source string, start, stop time.Time, every, aggFn string) ([]telemetryPoint, error) {
		width := parseFluxEveryDurationForTest(t, every)
		var pts []telemetryPoint
		for at := start.Truncate(width); at.Before(stop); at = at.Add(width) {
			pts = append(pts, telemetryPoint{Timestamp: at, Value: 1})
		}
		return pts, nil
	}
}

// TestExecuteGetPathHistory_FitsUnderCapWithNoTruncationForRepresentativeSpans
// is the direct regression test for the code-review finding (2026-09-25)
// that the bucket allowlist could produce results far larger than
// assistantMaxToolResultChars, and that when a shrink is needed it must not
// discard the most recent (final) bucket.
func TestExecuteGetPathHistory_FitsUnderCapWithNoTruncationForRepresentativeSpans(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	for _, hoursBack := range []float64{3, 24 * 7} {
		deps := assistantToolDeps{
			now:                        func() time.Time { return now },
			vesselState:                func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
			influxPathHistoryStat:      stubInfluxPathHistoryStatFullyPopulated(t),
			influxPathHistoryFirstLast: stubInfluxPathHistoryFirstLastNotFound,
		}

		args := fmt.Sprintf(`{"path":"tanks.fuel.2.currentLevel","hours_back":%v}`, hoursBack)
		raw, err := deps.execute(context.Background(), "get_path_history", json.RawMessage(args))
		if err != nil {
			t.Fatalf("hours_back=%v: execute: %v", hoursBack, err)
		}
		if len(raw) > assistantMaxToolResultChars {
			t.Errorf("hours_back=%v: result is %d chars, over the %d-char cap", hoursBack, len(raw), assistantMaxToolResultChars)
		}

		var result assistantGetPathHistoryResult
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			t.Fatalf("hours_back=%v: unmarshal: %v", hoursBack, err)
		}
		if result.Truncated {
			t.Errorf("hours_back=%v: expected no truncation, got %d buckets with truncated=true", hoursBack, len(result.Buckets))
		}
		if len(result.Buckets) == 0 {
			t.Fatalf("hours_back=%v: expected at least one bucket", hoursBack)
		}

		// "include the final bucket": recompute the same boundary the stub's
		// own generator loop would have produced last, and check it survived.
		stop := now
		start := now.Add(-time.Duration(hoursBack * float64(time.Hour)))
		_, width := assistantPathHistoryBucketWidth(stop.Sub(start))
		var wantLast time.Time
		for at := start.Truncate(width); at.Before(stop); at = at.Add(width) {
			wantLast = at
		}
		gotLast := result.Buckets[len(result.Buckets)-1].T
		if gotLast != wantLast.UTC().Format(time.RFC3339) {
			t.Errorf("hours_back=%v: expected the final bucket %s, got %s - was the tail dropped?",
				hoursBack, wantLast.UTC().Format(time.RFC3339), gotLast)
		}
	}
}

// TestExecuteGetPathHistory_RoundsBucketValuesForCompactness guards the
// other half of "keep bucket entries compact": InfluxDB's own float
// precision (seen live against the boat, e.g. 0.30452000000000856) must not
// ride straight through into the tool result once per bucket, per source
// value, three times per bucket (min/mean/max).
func TestExecuteGetPathHistory_RoundsBucketValuesForCompactness(t *testing.T) {
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	t0 := now.Add(-time.Hour)
	series := map[string][]telemetryPoint{
		"min":  {{Timestamp: t0, Value: 0.30452000000000856}},
		"mean": {{Timestamp: t0, Value: 0.31017691464369324}},
		"max":  {{Timestamp: t0, Value: 0.750686467065874}},
	}
	deps := assistantToolDeps{
		now:                        func() time.Time { return now },
		vesselState:                func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
		influxPathHistoryStat:      stubInfluxPathHistoryStat(series, nil),
		influxPathHistoryFirstLast: stubInfluxPathHistoryFirstLastNotFound,
	}
	raw, err := deps.execute(context.Background(), "get_path_history", json.RawMessage(`{"path":"tanks.fuel.2.currentLevel","hours_back":1}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(raw, "00000000856") || strings.Contains(raw, "1464369324") {
		t.Errorf("expected bucket values rounded for compactness, got raw InfluxDB precision in: %s", raw)
	}
	var result assistantGetPathHistoryResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Buckets) != 1 || result.Buckets[0].Min == nil || *result.Buckets[0].Min != 0.3 {
		t.Errorf("expected bucket min rounded to 0.3, got %+v", result.Buckets)
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

func TestExecuteGetPathHistory_ComputesOverallStats(t *testing.T) {
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	t0 := now.Add(-2 * time.Hour)
	t1 := now.Add(-time.Hour)

	series := map[string][]telemetryPoint{
		"min":  {{Timestamp: t0, Value: 0.30}, {Timestamp: t1, Value: 0.28}},
		"mean": {{Timestamp: t0, Value: 0.31}, {Timestamp: t1, Value: 0.29}},
		"max":  {{Timestamp: t0, Value: 0.32}, {Timestamp: t1, Value: 0.30}},
	}
	deps := assistantToolDeps{
		now:                        func() time.Time { return now },
		vesselState:                func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
		influxPathHistoryStat:      stubInfluxPathHistoryStat(series, nil),
		influxPathHistoryFirstLast: stubInfluxPathHistoryFirstLastNotFound,
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
	if len(result.Buckets) != 2 {
		t.Errorf("expected 2 buckets, got %d", len(result.Buckets))
	}
}

// TestExecuteGetPathHistory_FirstLastSeenComesFromActualSampleTimesNotBucketStarts
// is the direct regression test for a code-review finding (2026-09-25):
// first_seen/last_seen used to come from merged's own endpoints, which are
// bucket-START boundaries (buildInfluxPathStatFlux's timeSrc: "_start" doc
// comment), not the real first/last recorded sample - on a 90-day range
// bucketed to 2-day buckets, that is up to two days off from when a source
// actually stopped. The bucketed series here deliberately uses DIFFERENT
// timestamps (t0/t1) than the injected "actual" first/last sample times
// (realFirst/realLast) so the two can never accidentally agree; the result
// must reflect realFirst/realLast, not t0/t1.
func TestExecuteGetPathHistory_FirstLastSeenComesFromActualSampleTimesNotBucketStarts(t *testing.T) {
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	t0 := now.Add(-2 * time.Hour)
	t1 := now.Add(-time.Hour)
	// The real first/last sample times deliberately fall INSIDE their
	// respective buckets rather than on the bucket boundary, so a
	// regression back to using merged[0].T/merged[-1].T would fail loudly
	// rather than by coincidence.
	realFirst := t0.Add(17 * time.Minute)
	realLast := t1.Add(42 * time.Minute)

	series := map[string][]telemetryPoint{
		"min":  {{Timestamp: t0, Value: 0.30}, {Timestamp: t1, Value: 0.28}},
		"mean": {{Timestamp: t0, Value: 0.31}, {Timestamp: t1, Value: 0.29}},
		"max":  {{Timestamp: t0, Value: 0.32}, {Timestamp: t1, Value: 0.30}},
	}
	deps := assistantToolDeps{
		now:                        func() time.Time { return now },
		vesselState:                func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
		influxPathHistoryStat:      stubInfluxPathHistoryStat(series, nil),
		influxPathHistoryFirstLast: stubInfluxPathHistoryFirstLast(realFirst, realLast, true),
	}

	raw, err := deps.execute(context.Background(), "get_path_history", json.RawMessage(`{"path":"tanks.fuel.2.currentLevel","hours_back":3}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetPathHistoryResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.FirstSeenISO != realFirst.UTC().Format(time.RFC3339) {
		t.Errorf("expected first_seen_iso %s (the real first sample), got %s", realFirst.UTC().Format(time.RFC3339), result.FirstSeenISO)
	}
	if result.LastSeenISO != realLast.UTC().Format(time.RFC3339) {
		t.Errorf("expected last_seen_iso %s (the real last sample), got %s", realLast.UTC().Format(time.RFC3339), result.LastSeenISO)
	}
	if result.FirstSeenISO == t0.UTC().Format(time.RFC3339) {
		t.Errorf("first_seen_iso must not be the bucket-start boundary %s", t0.UTC().Format(time.RFC3339))
	}
	if result.LastSeenISO == t1.UTC().Format(time.RFC3339) {
		t.Errorf("last_seen_iso must not be the bucket-start boundary %s", t1.UTC().Format(time.RFC3339))
	}
}

// TestExecuteGetPathHistory_NoFirstLastSampleLeavesFirstLastSeenEmpty covers
// influxPathHistoryFirstLast's found=false case (no raw sample at all in
// range, e.g. Influx has the measurement but nothing in this window) even
// while the bucketed series is non-empty - first_seen/last_seen must stay
// unset rather than falling back to a bucket boundary.
func TestExecuteGetPathHistory_NoFirstLastSampleLeavesFirstLastSeenEmpty(t *testing.T) {
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	t0 := now.Add(-2 * time.Hour)
	series := map[string][]telemetryPoint{
		"min":  {{Timestamp: t0, Value: 0.30}},
		"mean": {{Timestamp: t0, Value: 0.31}},
		"max":  {{Timestamp: t0, Value: 0.32}},
	}
	deps := assistantToolDeps{
		now:                        func() time.Time { return now },
		vesselState:                func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
		influxPathHistoryStat:      stubInfluxPathHistoryStat(series, nil),
		influxPathHistoryFirstLast: stubInfluxPathHistoryFirstLastNotFound,
	}
	raw, err := deps.execute(context.Background(), "get_path_history", json.RawMessage(`{"path":"tanks.fuel.2.currentLevel","hours_back":3}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetPathHistoryResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.FirstSeen != "" || result.LastSeen != "" {
		t.Errorf("expected first/last seen to stay empty when influxPathHistoryFirstLast found nothing, got %+v / %+v", result.FirstSeen, result.LastSeen)
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
		now:                        func() time.Time { return now },
		vesselState:                func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
		influxPathHistoryStat:      stubInfluxPathHistoryStat(series, nil),
		influxPathHistoryFirstLast: stubInfluxPathHistoryFirstLastNotFound,
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
		now:                        func() time.Time { return now },
		vesselState:                func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
		influxPathHistoryStat:      stubInfluxPathHistoryStat(nil, nil),
		influxPathHistoryFirstLast: stubInfluxPathHistoryFirstLastNotFound,
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

// TestBuildInfluxLastRecordedFlux_UsesAnchoredRegexForPushdown is the direct
// regression test for a code-review finding (2026-09-25): strings.hasPrefix
// cannot be pushed down to InfluxDB's storage engine, so a source filter
// like "YachtDevices" over a 30-180 day lookback would scan every point in
// the bucket and risk the query's own timeout. An anchored regex (=~ /^.../)
// against a tag DOES push down.
func TestBuildInfluxLastRecordedFlux_UsesAnchoredRegexForPushdown(t *testing.T) {
	flux, err := buildInfluxLastRecordedFlux("SignalK_Data", "value", "tanks", "YachtDevices", 30)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, want := range []string{
		`r._measurement =~ /^tanks/`,
		`r.source =~ /^YachtDevices/`,
		`range(start: -30d)`,
		`group(columns: ["_measurement", "source"])`,
		`sort(columns: ["_time"])`,
	} {
		if !strings.Contains(flux, want) {
			t.Errorf("expected flux to contain %q, got:\n%s", want, flux)
		}
	}
	if strings.Contains(flux, "strings.hasPrefix") {
		t.Errorf("expected no strings.hasPrefix (does not push down to storage), got:\n%s", flux)
	}
	if strings.Contains(flux, `import "strings"`) {
		t.Errorf(`expected no import "strings" now that hasPrefix is gone, got:\n%s`, flux)
	}
}

// TestBuildInfluxLastRecordedFlux_TakesLastPerSeriesBeforeRegroupingAndSorting
// is the direct regression test for a code-review finding (2026-09-25):
// group()ing by measurement+source and then calling last() with no sort in
// between can return the end of whichever raw series (distinguished by a tag
// this query does not group on, e.g. context) Flux happens to concatenate
// last, not the actually-newest point across all of them. last() must run
// once per raw series first (pushable), then again after group()+sort().
func TestBuildInfluxLastRecordedFlux_TakesLastPerSeriesBeforeRegroupingAndSorting(t *testing.T) {
	flux, err := buildInfluxLastRecordedFlux("SignalK_Data", "value", "tanks", "", 30)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	firstLast := strings.Index(flux, "|> last()")
	group := strings.Index(flux, "group(columns:")
	sortIdx := strings.Index(flux, "sort(columns:")
	secondLast := strings.LastIndex(flux, "|> last()")

	if firstLast < 0 || group < 0 || sortIdx < 0 || secondLast < 0 {
		t.Fatalf("expected last()/group()/sort()/last() all present, got:\n%s", flux)
	}
	if firstLast == secondLast {
		t.Fatalf("expected two distinct last() calls (per raw series, then overall), got only one in:\n%s", flux)
	}
	if !(firstLast < group && group < sortIdx && sortIdx < secondLast) {
		t.Errorf("expected order filter -> last() -> group() -> sort() -> last(), got:\n%s", flux)
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

// TestBuildInfluxLastRecordedFlux_RejectsInvalidCharactersInPathPrefixAndSource
// is the direct regression test for a code-review finding (2026-09-25): a
// Flux regex literal is delimited by '/', so a path_prefix/source containing
// '/' would otherwise break out of the literal it is embedded in. Rejecting
// outright at a strict character allowlist, rather than trying to escape '/'
// into something inert, is what makes '/' - and everything else outside the
// allowlist - impossible after validation.
func TestBuildInfluxLastRecordedFlux_RejectsInvalidCharactersInPathPrefixAndSource(t *testing.T) {
	for _, tc := range []struct {
		name           string
		prefix, source string
	}{
		{"slash in prefix", "tanks/evil", ""},
		{"slash in source", "", "Yacht/Devices"},
		{"backslash in source", "", `Yacht\Devices`},
		{"newline in prefix", "tanks\nevil", ""},
		{"space in source", "", "Yacht Devices"},
	} {
		_, err := buildInfluxLastRecordedFlux("SignalK_Data", "value", tc.prefix, tc.source, 30)
		if err == nil {
			t.Errorf("%s: expected an error, got none", tc.name)
		}
	}
}

// TestBuildInfluxLastRecordedFlux_AcceptsRealFleetSourceLabels checks the
// validation allowlist against every $source label actually seen on the
// boat (from live verification against the production box and its
// signalk-to-influxdb2 plugin), so the fix does not accidentally reject a
// legitimate source it was never tested against.
func TestBuildInfluxLastRecordedFlux_AcceptsRealFleetSourceLabels(t *testing.T) {
	for _, source := range []string{
		"YachtDevices.6", "YachtDevices.7", "YachtDevices.128", "YachtDevices.129",
		"venus.com.victronenergy.gps", "Vesper_Cortex", "WLN10.GP",
	} {
		if _, err := buildInfluxLastRecordedFlux("SignalK_Data", "value", "", source, 30); err != nil {
			t.Errorf("expected real source label %q to be accepted, got %v", source, err)
		}
	}
	for _, prefix := range []string{"tanks", "tanks.fuel.2", "propulsion.0.exhaustTemperature"} {
		if _, err := buildInfluxLastRecordedFlux("SignalK_Data", "value", prefix, "", 30); err != nil {
			t.Errorf("expected real path prefix %q to be accepted, got %v", prefix, err)
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

// TestExecuteGetPathHistory_CapsGapsToMostRecentIncludingOpenGap is the
// direct regression test for a code-review finding (2026-09-25): capping
// the reported gap list used to keep gaps[:N] - the OLDEST N ranges - which
// meant a path still down at the end of the requested range had its own
// still-open gap (the actual answer to "when did it die") silently dropped
// once gap_count exceeded the cap, while several long-past, less relevant
// gaps were kept instead.
func TestExecuteGetPathHistory_CapsGapsToMostRecentIncludingOpenGap(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	const hoursBack = 24.0
	start := now.Add(-hoursBack * time.Hour)
	const width = 30 * time.Minute
	const totalBuckets = int(24 * time.Hour / width) // 48, matching the "30m" tier at span==24h

	// 12 scattered single-bucket gaps across the older three quarters of the
	// range, then every bucket from index 40 onward missing - an "open" gap
	// still running at `stop`.
	missing := map[int]bool{}
	for _, i := range []int{1, 3, 5, 7, 9, 11, 13, 15, 17, 19, 21, 23} {
		missing[i] = true
	}
	const openGapStartIndex = 40
	for i := openGapStartIndex; i < totalBuckets; i++ {
		missing[i] = true
	}

	var pts []telemetryPoint
	for i := 0; i < totalBuckets; i++ {
		if missing[i] {
			continue
		}
		pts = append(pts, telemetryPoint{Timestamp: start.Add(time.Duration(i) * width), Value: 1})
	}
	series := map[string][]telemetryPoint{"min": pts, "mean": pts, "max": pts}

	deps := assistantToolDeps{
		now:                        func() time.Time { return now },
		vesselState:                func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
		influxPathHistoryStat:      stubInfluxPathHistoryStat(series, nil),
		influxPathHistoryFirstLast: stubInfluxPathHistoryFirstLastNotFound,
	}
	raw, err := deps.execute(context.Background(), "get_path_history", json.RawMessage(fmt.Sprintf(`{"path":"x","hours_back":%v}`, hoursBack)))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetPathHistoryResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(result.Gaps) != assistantPathHistoryMaxReportedGaps {
		t.Fatalf("expected exactly %d reported gaps, got %d: %+v", assistantPathHistoryMaxReportedGaps, len(result.Gaps), result.Gaps)
	}

	wantOpenFrom := start.Add(openGapStartIndex * width).UTC().Format(time.RFC3339)
	wantOpenTo := now.UTC().Format(time.RFC3339)
	last := result.Gaps[len(result.Gaps)-1]
	if last.From != wantOpenFrom || last.To != wantOpenTo {
		t.Errorf("expected the final reported gap to be the still-open one [%s, %s), got [%s, %s)", wantOpenFrom, wantOpenTo, last.From, last.To)
	}

	oldestFrom := start.Add(1 * width).UTC().Format(time.RFC3339)
	for _, g := range result.Gaps {
		if g.From == oldestFrom {
			t.Errorf("expected the oldest gap %s to be dropped once gap_count exceeds the cap, but it is still reported: %+v", oldestFrom, result.Gaps)
		}
	}

	if !strings.Contains(result.Note, "most recent") {
		t.Errorf(`expected the note to say the MOST RECENT gaps are shown, got %q`, result.Note)
	}
}

// ── get_last_recorded value types (influx.go) ────────────────────────────

// TestInfluxRecordValueOK_AcceptsEveryTypeSignalkToInfluxdb2Writes is the
// direct regression test for a code-review finding (2026-09-25):
// queryInfluxLastRecorded used to accept only a float64 record value,
// silently dropping the row otherwise - which made every string path (a
// mode/state enum), boolean path (an alarm flag), or whole-number path
// decoded as an integer type look permanently unrecorded to get_last_recorded.
//
// signalk-to-influxdb2 (the upstream plugin that writes this bucket -
// tkurki/signalk-to-influxdb2, src/influx.ts, checked live 2026-09-25 via
// `gh api repos/tkurki/signalk-to-influxdb2/contents/src/influx.ts`) always
// writes to a field literally named "value" regardless of type:
// point.floatField('value', v) for a number (JsValueType.number),
// point.stringField('value', v) for a string, point.booleanField('value', v)
// for a boolean, and point.stringField('value', JSON.stringify(v)) for
// anything else - so the existing _field == "value" filter this file already
// uses is correct for every type; the bug was entirely the Go-side type
// assertion. int64/uint64 are accepted defensively even though no writer in
// this fleet's own path produces them today.
func TestInfluxRecordValueOK_AcceptsEveryTypeSignalkToInfluxdb2Writes(t *testing.T) {
	cases := []struct {
		name string
		in   any
		ok   bool
	}{
		{"number (floatField)", 12.5, true},
		{"string (stringField - a mode/state enum)", "charging", true},
		{"bool (booleanField - an alarm flag)", true, true},
		{"int64 (defensive)", int64(3), true},
		{"uint64 (defensive)", uint64(3), true},
		{"nil (no value at all)", nil, false},
	}
	for _, c := range cases {
		got, ok := influxRecordValueOK(c.in)
		if ok != c.ok {
			t.Errorf("%s: expected ok=%v, got %v", c.name, c.ok, ok)
			continue
		}
		if ok && got != c.in {
			t.Errorf("%s: expected the value passed through unchanged, got %v", c.name, got)
		}
	}
}

// ── first/last seen (influx.go) ───────────────────────────────────────────

// TestBuildInfluxFirstLastFlux_GroupsBeforeFirstAndLast is
// buildInfluxLastRecordedFlux's own group()-then-sort()-then-last() reasoning
// applied here: first()/last() must run on the raw series REGROUPED into one
// table (group(), bare), not per-source, or a path written by several
// $source labels would report one arbitrary source's own first/last point
// rather than the path's genuine overall first/last sample.
func TestBuildInfluxFirstLastFlux_GroupsBeforeFirstAndLast(t *testing.T) {
	start := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	stop := start.Add(time.Hour)
	flux, err := buildInfluxFirstLastFlux("SignalK_Data", "value", "tanks.fuel.2.currentLevel", "", start, stop)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(flux, "|> group()") {
		t.Errorf("expected a bare group() before first()/last(), got:\n%s", flux)
	}
	if !strings.Contains(flux, "|> first()") || !strings.Contains(flux, "|> last()") {
		t.Errorf("expected both first() and last(), got:\n%s", flux)
	}
	if !strings.Contains(flux, "union(tables:") {
		t.Errorf("expected the first/last tables combined with union(), got:\n%s", flux)
	}
}

func TestBuildInfluxFirstLastFlux_IncludesSourceFilterOnlyWhenGiven(t *testing.T) {
	start := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	stop := start.Add(time.Hour)

	withSource, err := buildInfluxFirstLastFlux("SignalK_Data", "value", "tanks.fuel.2.currentLevel", "YachtDevices.6", start, stop)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(withSource, `r.source == "YachtDevices.6"`) {
		t.Errorf("expected an exact source filter, got:\n%s", withSource)
	}

	withoutSource, err := buildInfluxFirstLastFlux("SignalK_Data", "value", "tanks.fuel.2.currentLevel", "", start, stop)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if strings.Contains(withoutSource, "r.source") {
		t.Errorf("expected no source filter when source is empty, got:\n%s", withoutSource)
	}
}

func TestBuildInfluxFirstLastFlux_RejectsHostileInterpolationInPath(t *testing.T) {
	start := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	_, err := buildInfluxFirstLastFlux("SignalK_Data", "value", "tanks.${r._measurement}", "", start, start.Add(time.Hour))
	if err == nil {
		t.Fatalf("expected an error for hostile path input")
	}
}
