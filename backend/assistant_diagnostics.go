package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// This file implements the three read-only diagnostic tools (ADR 0131) that
// let Mate answer "when did X stop updating" and "is X still live" itself,
// instead of sending the operator to the SignalK admin console:
//
//   - check_signalk_paths: the live snapshot right now - connection status,
//     which sources are updating and which have gone quiet, and the
//     current value/age of any path matching a filter.
//   - get_last_recorded: InfluxDB's most recent recorded point for every
//     measurement/source matching a filter, over a lookback window - this
//     is what actually answers "when did it stop", since a source that has
//     been dead since before this process last restarted may not be in the
//     live snapshot at all (see signalKDiagnosticsSnapshot's own doc
//     comment).
//   - get_path_history: one path's InfluxDB history over an explicit range,
//     bucketed to min/mean/max, with gaps - what the value actually did
//     around when it stopped, not just the single last point.
//
// All three are read-only, following the same injectable-dependency idiom
// assistant_tools.go's own doc comment describes: assistantToolDeps carries
// every real dependency, production wires the live snapshot/InfluxDB,
// tests inject fakes.

// ── shared thresholds ───────────────────────────────────────────────────

// assistantDiagnosticsStaleAfter is the staleness threshold check_signalk_paths
// (and get_last_recorded/get_path_history's own age reporting) uses to mark a
// path or source as stale - deliberately the SAME threshold derivedInputMaxAge
// (derived_paths.go) already uses to decide when a derived figure goes absent,
// which itself must agree with the frontend's STALE_AFTER_SECONDS
// (lib/staleness.ts). Reusing that constant, rather than picking a new
// number, means Mate's answer about a path's staleness can never disagree
// with what the tile showing that same path already displays.
const assistantDiagnosticsStaleAfter = derivedInputMaxAge

// ── check_signalk_paths ─────────────────────────────────────────────────

// assistantCheckSignalKPathsMaxMatches caps how many matching paths
// check_signalk_paths returns in one call - a bare "tanks" filter on a
// vessel with dozens of tank-related leaves (level, capacity, per instance)
// is exactly the case this exists to bound; the sources summary (always
// returned in full) is usually enough on its own to answer "which source
// went quiet", with matches narrowing to the specific paths worth quoting.
const assistantCheckSignalKPathsMaxMatches = 40

// signalKDiagnosticsSnapshot is check_signalk_paths' whole view of the live
// SignalK snapshot: connection status, last message time, and every leaf
// currently in the self tree with its value/source/declared timestamp.
//
// HasSelfTree distinguishes "SignalK has never sent a hello frame at all"
// from "the self tree exists but happens to be empty" - the same
// distinction signalKSnapshot.selfTree()'s own nil return already carries.
// check_signalk_paths fails explicitly on the former (AGENTS.md's fallback
// policy) rather than reporting a lookalike empty result.
//
// A path that stopped reporting can be missing from Samples entirely, not
// merely stale: this snapshot only ever holds what THIS PROCESS has
// received over the delta stream since it last started (signalk_stream.go
// replays the server's retained model on reconnect, but a source that never
// sends another delta after a backend restart leaves nothing to replay).
// That is exactly why get_last_recorded and get_path_history exist:
// InfluxDB remembers what the live snapshot has already forgotten.
type signalKDiagnosticsSnapshot struct {
	Connected   bool
	LastMessage time.Time
	HasSelfTree bool
	Samples     []signalKPathSample
}

// signalKDiagnosticsFromGlobalSnapshot is check_signalk_paths' production
// dependency: globalSignalKSnapshot read fresh on every call, the same
// "read the global, inject the function" idiom fuelRateInstancesFromSnapshot
// (assistant_tools.go) already uses for estimate_passage.
func signalKDiagnosticsFromGlobalSnapshot(now time.Time) signalKDiagnosticsSnapshot {
	connected, lastMessage := globalSignalKSnapshot.status()
	context := globalSignalKSnapshot.selfContext()
	tree := globalSignalKSnapshot.selfTree()
	return signalKDiagnosticsSnapshot{
		Connected:   connected,
		LastMessage: lastMessage,
		HasSelfTree: tree != nil,
		Samples:     collectSignalKPathSamples(globalSignalKSnapshot, context, tree, now),
	}
}

type assistantCheckSignalKPathsArgs struct {
	Filter string `json:"filter"`
	Source string `json:"source"`
}

type assistantSignalKConnection struct {
	Connected       bool     `json:"connected"`
	LastMessageAgoS *float64 `json:"last_message_ago_s,omitempty"`
	LastMessageAt   string   `json:"last_message_at,omitempty"`
}

type assistantSignalKPathMatch struct {
	Path       string   `json:"path"`
	Value      any      `json:"value"`
	Units      string   `json:"units,omitempty"`
	Source     string   `json:"source,omitempty"`
	Timestamp  string   `json:"timestamp,omitempty"`
	AgeSeconds *float64 `json:"age_seconds,omitempty"`
	Stale      bool     `json:"stale"`
}

// assistantSignalKSourceSummary is one $source label's standing across the
// WHOLE self tree, regardless of any filter/source argument the call
// carried - see executeCheckSignalKPaths' own comment on why sources is
// never itself filtered.
type assistantSignalKSourceSummary struct {
	Source       string   `json:"source"`
	PathCount    int      `json:"path_count"`
	NewestUpdate string   `json:"newest_update,omitempty"`
	AgeSeconds   *float64 `json:"age_seconds,omitempty"`
	Stale        bool     `json:"stale"`
}

type assistantCheckSignalKPathsResult struct {
	Connection assistantSignalKConnection      `json:"connection"`
	Filter     string                          `json:"filter,omitempty"`
	Source     string                          `json:"source,omitempty"`
	Matches    []assistantSignalKPathMatch     `json:"matches"`
	MatchCount int                             `json:"match_count"`
	Truncated  bool                            `json:"truncated,omitempty"`
	Sources    []assistantSignalKSourceSummary `json:"sources"`
	Note       string                          `json:"note,omitempty"`
}

// executeCheckSignalKPaths answers check_signalk_paths: the live snapshot's
// connection status, a source summary built from every leaf currently in the
// self tree (never itself filtered - a "tanks" filter must not hide that
// YachtDevices.6 has gone quiet just because none of its OTHER paths matched
// "tanks"), and, when filter and/or source narrow it, the matching paths
// themselves with value/units/source/timestamp/age/stale.
func (d assistantToolDeps) executeCheckSignalKPaths(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var args assistantCheckSignalKPathsArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse check_signalk_paths arguments: %w", err)
	}
	filter := strings.ToLower(strings.TrimSpace(args.Filter))
	sourceFilter := strings.ToLower(strings.TrimSpace(args.Source))

	now := d.now()
	snap := d.signalKDiagnostics(now)
	if !snap.HasSelfTree {
		return "", fmt.Errorf("check_signalk_paths: no self tree yet - SignalK is not connected, or this vessel's context is not yet known")
	}

	result := assistantCheckSignalKPathsResult{
		Connection: assistantSignalKConnection{Connected: snap.Connected},
		Filter:     strings.TrimSpace(args.Filter),
		Source:     strings.TrimSpace(args.Source),
		Matches:    make([]assistantSignalKPathMatch, 0),
		Sources:    make([]assistantSignalKSourceSummary, 0),
	}
	if !snap.LastMessage.IsZero() {
		ago := roundTo1(now.Sub(snap.LastMessage).Seconds())
		result.Connection.LastMessageAgoS = &ago
		result.Connection.LastMessageAt = snap.LastMessage.UTC().Format(time.RFC3339)
	}

	result.Sources = summarizeSignalKSources(snap.Samples, now)

	if filter == "" && sourceFilter == "" {
		result.Note = "no filter given: showing connection status and the source summary only. Pass filter and/or source to see matching paths."
		return capToolResultJSON(&result, nil)
	}

	var matches []assistantSignalKPathMatch
	matchCount := 0
	for _, s := range snap.Samples {
		if filter != "" && !strings.Contains(strings.ToLower(s.Path), filter) {
			continue
		}
		if sourceFilter != "" && !strings.Contains(strings.ToLower(s.Source), sourceFilter) {
			continue
		}
		matchCount++

		m := assistantSignalKPathMatch{
			Path:      s.Path,
			Value:     s.Value,
			Units:     s.Units,
			Source:    s.Source,
			Timestamp: s.Timestamp,
		}
		if s.AgeSeconds >= 0 {
			age := s.AgeSeconds
			m.AgeSeconds = &age
			m.Stale = age > assistantDiagnosticsStaleAfter.Seconds()
		}
		matches = append(matches, m)
	}
	result.MatchCount = matchCount

	truncated := false
	if len(matches) > assistantCheckSignalKPathsMaxMatches {
		matches = matches[:assistantCheckSignalKPathsMaxMatches]
		truncated = true
	}
	result.Matches = matches
	result.Truncated = truncated

	switch {
	case matchCount == 0:
		var parts []string
		if filter != "" {
			parts = append(parts, fmt.Sprintf("path containing %q", args.Filter))
		}
		if sourceFilter != "" {
			parts = append(parts, fmt.Sprintf("source containing %q", args.Source))
		}
		result.Note = fmt.Sprintf(
			"no %s is in the live SignalK tree right now - it may never have reported since this connection started, or it went quiet before that. Check get_last_recorded or get_path_history for its InfluxDB history.",
			strings.Join(parts, " and "),
		)
	case truncated:
		result.Note = fmt.Sprintf("%d paths matched; showing the first %d. Narrow filter/source to see the rest.", matchCount, assistantCheckSignalKPathsMaxMatches)
	}

	shrink := func() bool {
		if len(result.Matches) == 0 {
			return false
		}
		result.Matches = result.Matches[:len(result.Matches)-1]
		result.Truncated = true
		return true
	}
	return capToolResultJSON(&result, shrink)
}

// summarizeSignalKSources groups samples by their declared $source (an empty
// source labelled "(unknown source)" rather than dropped, since that itself
// is diagnostically interesting - a path with no declared source at all) and
// reports, per source, how many paths it carries and how long ago the
// freshest one of them last updated - freshestAge's own "the freshest
// (minimum) of whichever inputs are actually known" reduction, generalised
// here from "several inputs feeding one derived figure" to "every path one
// $source label has ever contributed to this tree".
//
// Sorted oldest-first (largest age first, unknown last) so a source that has
// gone quiet sorts above every source still updating every few seconds -
// the whole point of a diagnostic answering "which source stopped".
func summarizeSignalKSources(samples []signalKPathSample, now time.Time) []assistantSignalKSourceSummary {
	type aggregate struct {
		count int
		ages  []float64
	}
	bySource := map[string]*aggregate{}
	var order []string
	for _, s := range samples {
		label := s.Source
		if label == "" {
			label = "(unknown source)"
		}
		agg, ok := bySource[label]
		if !ok {
			agg = &aggregate{}
			bySource[label] = agg
			order = append(order, label)
		}
		agg.count++
		agg.ages = append(agg.ages, s.AgeSeconds)
	}

	out := make([]assistantSignalKSourceSummary, 0, len(order))
	for _, label := range order {
		agg := bySource[label]
		summary := assistantSignalKSourceSummary{Source: label, PathCount: agg.count}
		if age := freshestAge(agg.ages...); age >= 0 {
			a := age
			summary.AgeSeconds = &a
			summary.NewestUpdate = now.Add(-time.Duration(age * float64(time.Second))).UTC().Format(time.RFC3339)
			summary.Stale = age > assistantDiagnosticsStaleAfter.Seconds()
		}
		out = append(out, summary)
	}

	sort.Slice(out, func(i, j int) bool {
		ai, aj := -1.0, -1.0
		if out[i].AgeSeconds != nil {
			ai = *out[i].AgeSeconds
		}
		if out[j].AgeSeconds != nil {
			aj = *out[j].AgeSeconds
		}
		if ai != aj {
			return ai > aj
		}
		return out[i].Source < out[j].Source
	})
	return out
}

// ── get_last_recorded ───────────────────────────────────────────────────

const (
	// assistantLastRecordedDefaultLookbackDays and
	// ...MaxLookbackDays bound get_last_recorded's lookback_days. 30 is
	// plenty for the ordinary "did this stop recently" question; the ceiling
	// is a conservative guess, not a read of the InfluxDB bucket's actual
	// configured retention (this backend has no way to introspect that - see
	// ADR 0131) - asking past the bucket's real retention simply finds
	// nothing that far back, which InfluxDB treats as an empty range rather
	// than an error, so a ceiling wider than the real retention is safe, just
	// sometimes pointless.
	assistantLastRecordedDefaultLookbackDays = 30
	assistantLastRecordedMaxLookbackDays     = 180

	// assistantLastRecordedMaxRows caps how many measurement/source rows
	// get_last_recorded returns - a bare source:"YachtDevices" filter can
	// match a couple of dozen distinct paths (tanks, engine, exhaust) on a
	// vessel with a lot of N2K gear behind that gateway.
	assistantLastRecordedMaxRows = 40
)

type assistantGetLastRecordedArgs struct {
	PathPrefix   string `json:"path_prefix"`
	Source       string `json:"source"`
	LookbackDays int    `json:"lookback_days"`
}

type assistantLastRecordedRow struct {
	Path       string  `json:"path"`
	Source     string  `json:"source,omitempty"`
	LastSeenAt string  `json:"last_seen_at"`
	AgeDays    float64 `json:"age_days"`
	LastValue  float64 `json:"last_value"`
}

type assistantGetLastRecordedResult struct {
	PathPrefix   string                     `json:"path_prefix,omitempty"`
	Source       string                     `json:"source,omitempty"`
	LookbackDays int                        `json:"lookback_days"`
	Rows         []assistantLastRecordedRow `json:"rows"`
	Truncated    bool                       `json:"truncated,omitempty"`
	Note         string                     `json:"note,omitempty"`
}

// executeGetLastRecorded answers "when did X stop": InfluxDB's most recent
// recorded point for every measurement (SignalK path) and source matching
// path_prefix and/or source, over the lookback window. This is the tool that
// actually answers the question when the live snapshot no longer carries the
// path at all (signalKDiagnosticsSnapshot's own doc comment) - InfluxDB
// remembers what a backend restart since made the live tree forget.
func (d assistantToolDeps) executeGetLastRecorded(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var args assistantGetLastRecordedArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse get_last_recorded arguments: %w", err)
	}

	pathPrefix := strings.TrimSpace(args.PathPrefix)
	source := strings.TrimSpace(args.Source)
	if pathPrefix == "" && source == "" {
		return "", fmt.Errorf("get_last_recorded: at least one of path_prefix or source is required")
	}

	lookbackDays := clampAssistantDays(args.LookbackDays, assistantLastRecordedDefaultLookbackDays, assistantLastRecordedMaxLookbackDays)

	raws, err := d.influxLastRecorded(pathPrefix, source, lookbackDays)
	if err != nil {
		return "", fmt.Errorf("get_last_recorded: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	now := d.now()
	rows := make([]assistantLastRecordedRow, 0, len(raws))
	for _, r := range raws {
		rows = append(rows, assistantLastRecordedRow{
			Path:       r.Path,
			Source:     r.Source,
			LastSeenAt: r.Time.UTC().Format(time.RFC3339),
			AgeDays:    roundTo1(now.Sub(r.Time).Hours() / 24),
			LastValue:  r.Value,
		})
	}

	// Oldest (most overdue) first - the same "quiet sources stand out" sort
	// check_signalk_paths' own source summary uses, since this is the tool
	// answering exactly that question for a path/source no longer live at
	// all.
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].AgeDays != rows[j].AgeDays {
			return rows[i].AgeDays > rows[j].AgeDays
		}
		if rows[i].Path != rows[j].Path {
			return rows[i].Path < rows[j].Path
		}
		return rows[i].Source < rows[j].Source
	})

	result := assistantGetLastRecordedResult{
		PathPrefix:   pathPrefix,
		Source:       source,
		LookbackDays: lookbackDays,
		Rows:         rows,
	}

	if len(rows) == 0 {
		result.Note = fmt.Sprintf("nothing recorded matching this filter in the last %d days", lookbackDays)
	} else if len(rows) > assistantLastRecordedMaxRows {
		result.Rows = rows[:assistantLastRecordedMaxRows]
		result.Truncated = true
		result.Note = fmt.Sprintf("%d rows matched; showing the %d most overdue. Narrow path_prefix/source to see the rest.", len(rows), assistantLastRecordedMaxRows)
	}

	shrink := func() bool {
		if len(result.Rows) == 0 {
			return false
		}
		result.Rows = result.Rows[:len(result.Rows)-1]
		result.Truncated = true
		return true
	}
	return capToolResultJSON(&result, shrink)
}

// ── get_path_history ────────────────────────────────────────────────────

const (
	// assistantPathHistoryDefaultHoursBack is get_path_history's default
	// window when neither hours_back nor start/end is given - a day is
	// enough to see whether a value has been flat, noisy or simply absent
	// recently, without the model having to think of a window first.
	assistantPathHistoryDefaultHoursBack = 24.0

	// assistantPathHistoryMaxSpan bounds the requested range so one call
	// cannot ask for, say, five years of history - 90 days is long enough to
	// see well past a source going quiet.
	assistantPathHistoryMaxSpan = 90 * 24 * time.Hour

	// assistantPathHistoryMaxReportedGaps caps how many gap ranges
	// get_path_history lists explicitly - past this, gap_count alone (the
	// true total) says enough; a path that has been down for 80 of its 90
	// requested days does not need 80 individual gap entries to make that
	// point.
	assistantPathHistoryMaxReportedGaps = 10
)

type assistantGetPathHistoryArgs struct {
	Path      string  `json:"path"`
	Source    string  `json:"source"`
	Start     string  `json:"start"`
	End       string  `json:"end"`
	HoursBack float64 `json:"hours_back"`
}

type assistantPathHistoryBucket struct {
	Time string   `json:"time"`
	ISO  string   `json:"iso"`
	Min  *float64 `json:"min,omitempty"`
	Mean *float64 `json:"mean,omitempty"`
	Max  *float64 `json:"max,omitempty"`
}

type assistantPathHistoryGap struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type assistantGetPathHistoryResult struct {
	Path         string                       `json:"path"`
	Source       string                       `json:"source,omitempty"`
	Timezone     string                       `json:"timezone"`
	Start        string                       `json:"start"`
	StartISO     string                       `json:"start_iso"`
	End          string                       `json:"end"`
	EndISO       string                       `json:"end_iso"`
	Bucket       string                       `json:"bucket"`
	Buckets      []assistantPathHistoryBucket `json:"buckets"`
	OverallMin   *float64                     `json:"overall_min,omitempty"`
	OverallMean  *float64                     `json:"overall_mean,omitempty"`
	OverallMax   *float64                     `json:"overall_max,omitempty"`
	FirstSeen    string                       `json:"first_seen,omitempty"`
	FirstSeenISO string                       `json:"first_seen_iso,omitempty"`
	LastSeen     string                       `json:"last_seen,omitempty"`
	LastSeenISO  string                       `json:"last_seen_iso,omitempty"`
	Gaps         []assistantPathHistoryGap    `json:"gaps,omitempty"`
	GapCount     int                          `json:"gap_count,omitempty"`
	Truncated    bool                         `json:"truncated,omitempty"`
	Note         string                       `json:"note,omitempty"`
}

// assistantPathHistoryPoint is one merged bucket - the min/mean/max InfluxDB
// queries all share the same range and every, so their points line up on
// the same bucket timestamps; mergePathHistoryPoints joins them by that
// shared timestamp.
type assistantPathHistoryPoint struct {
	T    time.Time
	Min  *float64
	Mean *float64
	Max  *float64
}

// assistantPathHistoryBucketWidth picks the aggregation bucket from the
// requested span's length alone, out of a fixed allowlist - never from any
// model-supplied text, per AGENTS.md's fallback policy and the same reason
// telemetryHistoryWindows (telemetry_history_api.go) is itself an allowlist:
// every is interpolated into the Flux query unquoted. Chosen so a bucket
// count from 1h up to the 90-day cap stays in roughly the 100-250 range,
// comfortably inside assistantMaxToolResultChars even before
// capToolResultJSON's own shrink.
func assistantPathHistoryBucketWidth(span time.Duration) (label string, width time.Duration) {
	switch {
	case span <= 3*time.Hour:
		return "1m", time.Minute
	case span <= 12*time.Hour:
		return "5m", 5 * time.Minute
	case span <= 24*time.Hour:
		return "15m", 15 * time.Minute
	case span <= 3*24*time.Hour:
		return "30m", 30 * time.Minute
	case span <= 7*24*time.Hour:
		return "1h", time.Hour
	case span <= 30*24*time.Hour:
		return "4h", 4 * time.Hour
	default:
		return "12h", 12 * time.Hour
	}
}

// mergePathHistoryPoints joins the three same-range, same-bucket aggregate
// series (min/mean/max, each queried separately since Flux's aggregateWindow
// takes one fn) into one row per bucket timestamp, ordered chronologically.
// A bucket missing from one series (possible if, say, a null crept into one
// aggregate but not another) simply leaves that one field nil rather than
// dropping the whole bucket.
func mergePathHistoryPoints(minPts, meanPts, maxPts []telemetryPoint) []assistantPathHistoryPoint {
	byUnix := map[int64]*assistantPathHistoryPoint{}
	var order []int64

	get := func(unix int64, t time.Time) *assistantPathHistoryPoint {
		p, ok := byUnix[unix]
		if !ok {
			p = &assistantPathHistoryPoint{T: t}
			byUnix[unix] = p
			order = append(order, unix)
		}
		return p
	}
	for _, pt := range minPts {
		v := pt.Value
		get(pt.Timestamp.Unix(), pt.Timestamp).Min = &v
	}
	for _, pt := range meanPts {
		v := pt.Value
		get(pt.Timestamp.Unix(), pt.Timestamp).Mean = &v
	}
	for _, pt := range maxPts {
		v := pt.Value
		get(pt.Timestamp.Unix(), pt.Timestamp).Max = &v
	}

	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	out := make([]assistantPathHistoryPoint, 0, len(order))
	for _, unix := range order {
		out = append(out, *byUnix[unix])
	}
	return out
}

// computePathHistoryGaps walks [start, stop) at width-sized steps and
// reports every run of consecutive steps with no bucket in points as one gap
// range, plus the true total number of missing steps (gapCount), which stays
// accurate even once the reported gap list itself is capped by the caller.
func computePathHistoryGaps(start, stop time.Time, width time.Duration, points []assistantPathHistoryPoint) (gaps []assistantPathHistoryGap, gapCount int) {
	if width <= 0 {
		return nil, 0
	}
	present := make(map[int64]bool, len(points))
	for _, p := range points {
		present[p.T.Truncate(width).Unix()] = true
	}

	var gapStart time.Time
	inGap := false
	for t := start.Truncate(width); t.Before(stop); t = t.Add(width) {
		if present[t.Unix()] {
			if inGap {
				gaps = append(gaps, assistantPathHistoryGap{From: gapStart.UTC().Format(time.RFC3339), To: t.UTC().Format(time.RFC3339)})
				inGap = false
			}
			continue
		}
		gapCount++
		if !inGap {
			gapStart = t
			inGap = true
		}
	}
	if inGap {
		gaps = append(gaps, assistantPathHistoryGap{From: gapStart.UTC().Format(time.RFC3339), To: stop.UTC().Format(time.RFC3339)})
	}
	return gaps, gapCount
}

// executeGetPathHistory answers "what did X actually do around when it
// stopped": one path's InfluxDB history over an explicit range, bucketed to
// min/mean/max per bucket (a flat mean with min==max across many buckets is
// as telling as an outright gap - a frozen sensor still "updating" the same
// stale value), overall min/mean/max, first/last seen, and gaps.
func (d assistantToolDeps) executeGetPathHistory(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var args assistantGetPathHistoryArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse get_path_history arguments: %w", err)
	}

	path := strings.TrimSpace(args.Path)
	if path == "" {
		return "", fmt.Errorf("get_path_history: path is required")
	}
	if containsFluxInterpolationSyntax(path) {
		return "", fmt.Errorf("get_path_history: path must not contain '$' or '{'")
	}
	source := strings.TrimSpace(args.Source)
	if containsFluxInterpolationSyntax(source) {
		return "", fmt.Errorf("get_path_history: source must not contain '$' or '{'")
	}

	now := d.now()
	var start, stop time.Time
	switch {
	case args.HoursBack > 0:
		stop = now
		start = now.Add(-time.Duration(args.HoursBack * float64(time.Hour)))
	case strings.TrimSpace(args.Start) != "" || strings.TrimSpace(args.End) != "":
		s, err := time.Parse(time.RFC3339, strings.TrimSpace(args.Start))
		if err != nil {
			return "", fmt.Errorf("get_path_history: start must be RFC3339 (e.g. 2026-09-21T00:00:00Z): %w", err)
		}
		e, err := time.Parse(time.RFC3339, strings.TrimSpace(args.End))
		if err != nil {
			return "", fmt.Errorf("get_path_history: end must be RFC3339 (e.g. 2026-09-22T00:00:00Z): %w", err)
		}
		start, stop = s, e
	default:
		stop = now
		start = now.Add(-time.Duration(assistantPathHistoryDefaultHoursBack * float64(time.Hour)))
	}

	if !stop.After(start) {
		return "", fmt.Errorf("get_path_history: end must be after start")
	}
	if stop.Sub(start) > assistantPathHistoryMaxSpan {
		start = stop.Add(-assistantPathHistoryMaxSpan)
	}

	every, width := assistantPathHistoryBucketWidth(stop.Sub(start))

	if err := ctx.Err(); err != nil {
		return "", err
	}

	minPts, err := d.influxPathHistoryStat(path, source, start, stop, every, "min")
	if err != nil {
		return "", fmt.Errorf("get_path_history: %w", err)
	}
	meanPts, err := d.influxPathHistoryStat(path, source, start, stop, every, "mean")
	if err != nil {
		return "", fmt.Errorf("get_path_history: %w", err)
	}
	maxPts, err := d.influxPathHistoryStat(path, source, start, stop, every, "max")
	if err != nil {
		return "", fmt.Errorf("get_path_history: %w", err)
	}

	loc := time.UTC
	tzLabel := "UTC"
	if state, verr := d.vesselState(); verr == nil && hasUsableVesselPosition(state.Latitude, state.Longitude) {
		loc = vesselLocalLocation(state.Longitude)
		tzLabel = assistantTimeZoneLabel(state.Longitude)
	}

	merged := mergePathHistoryPoints(minPts, meanPts, maxPts)

	buckets := make([]assistantPathHistoryBucket, 0, len(merged))
	var overallMin, overallMax *float64
	var meanSum float64
	var meanCount int
	for _, m := range merged {
		buckets = append(buckets, assistantPathHistoryBucket{
			Time: m.T.In(loc).Format("Mon 2 Jan 15:04"),
			ISO:  m.T.UTC().Format(time.RFC3339),
			Min:  m.Min, Mean: m.Mean, Max: m.Max,
		})
		if m.Min != nil && (overallMin == nil || *m.Min < *overallMin) {
			v := *m.Min
			overallMin = &v
		}
		if m.Max != nil && (overallMax == nil || *m.Max > *overallMax) {
			v := *m.Max
			overallMax = &v
		}
		if m.Mean != nil {
			meanSum += *m.Mean
			meanCount++
		}
	}
	var overallMean *float64
	if meanCount > 0 {
		v := roundTo2(meanSum / float64(meanCount))
		overallMean = &v
	}

	result := assistantGetPathHistoryResult{
		Path:     path,
		Source:   source,
		Timezone: tzLabel,
		Start:    start.In(loc).Format("Mon 2 Jan 15:04"),
		StartISO: start.UTC().Format(time.RFC3339),
		End:      stop.In(loc).Format("Mon 2 Jan 15:04"),
		EndISO:   stop.UTC().Format(time.RFC3339),
		Bucket:   every,
		Buckets:  buckets,

		OverallMin:  overallMin,
		OverallMean: overallMean,
		OverallMax:  overallMax,
	}

	if len(merged) == 0 {
		result.Note = "no data recorded for this path in the requested range"
	} else {
		first, last := merged[0].T, merged[len(merged)-1].T
		result.FirstSeen = first.In(loc).Format("Mon 2 Jan 15:04")
		result.FirstSeenISO = first.UTC().Format(time.RFC3339)
		result.LastSeen = last.In(loc).Format("Mon 2 Jan 15:04")
		result.LastSeenISO = last.UTC().Format(time.RFC3339)
	}

	gaps, gapCount := computePathHistoryGaps(start, stop, width, merged)
	result.GapCount = gapCount
	if len(gaps) > assistantPathHistoryMaxReportedGaps {
		gaps = gaps[:assistantPathHistoryMaxReportedGaps]
		if result.Note == "" {
			result.Note = fmt.Sprintf("%d gaps found; showing the first %d.", gapCount, assistantPathHistoryMaxReportedGaps)
		}
	}
	result.Gaps = gaps

	shrink := func() bool {
		if len(result.Buckets) == 0 {
			return false
		}
		result.Buckets = result.Buckets[:len(result.Buckets)-1]
		result.Truncated = true
		return true
	}
	return capToolResultJSON(&result, shrink)
}
