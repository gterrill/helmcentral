package main

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

/*
Benchmarks and regression tests for backend-perf-audit.md Tier 1 #2 ("whole-
tree deep copies, dozens a second") and #3 ("the snapshot never forgets").

buildSyntheticSelfSnapshot stands in for a real boat's self tree: 3,000
leaves spread across the categories a real vessel actually publishes under
(navigation, environment, electrical, propulsion, tanks, steering), plus a
two-engine fuel layout and three hours of barometer history so the derived
fuel-economy and pressure-rate paths have real inputs to compute from --
exactly what an alarm rule bound to one of them evaluates against in
production, not an empty tree that would make computeDerivedPaths' fuel and
barometer branches no-ops.
*/

const perfSyntheticLeafCount = 3000

var perfSyntheticCategories = []string{
	"navigation", "environment", "electrical", "propulsion", "tanks", "steering",
}

// perfSyntheticLeafPaths returns the same n deterministic dotted paths
// buildSyntheticSelfSnapshot populates, so a caller choosing which ones to
// bind a rule or a gauge to always names paths that actually exist.
func perfSyntheticLeafPaths(n int) []string {
	paths := make([]string, n)
	for i := 0; i < n; i++ {
		category := perfSyntheticCategories[i%len(perfSyntheticCategories)]
		paths[i] = fmt.Sprintf("%s.group%d.metric%d", category, i/25, i%25)
	}
	return paths
}

func buildSyntheticSelfSnapshot(tb testing.TB, leafCount int) *signalKSnapshot {
	tb.Helper()

	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")

	now := time.Now().UTC()
	entries := make([]signalKValue, 0, leafCount+5)
	for i, path := range perfSyntheticLeafPaths(leafCount) {
		entries = append(entries, signalKValue{Path: path, Value: float64(i)})
	}
	// A realistic two-engine fuel layout so vesselFuelEconomyPath and
	// fuelVolumePath have real inputs, the same as an alarm rule bound to one
	// of them would read in evaluateAlarmsOnce.
	entries = append(entries,
		signalKValue{Path: "navigation.speedOverGround", Value: 5.0},
		signalKValue{Path: "propulsion.port.fuel.rate", Value: 6.639e-06},
		signalKValue{Path: "propulsion.stbd.fuel.rate", Value: 6.528e-06},
		signalKValue{Path: "tanks.fuel.0.currentLevel", Value: 0.7},
		signalKValue{Path: "tanks.fuel.0.capacity", Value: 0.4},
	)

	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Timestamp: now.Format(time.RFC3339), Values: entries}},
	}, now)

	// Three hours of barometer history so pressureRatePath resolves to a
	// real value rather than staying absent for lack of a trend to compute.
	origBarometer := barometerHistory
	barometerHistory = newTelemetryRingBuffer(4096)
	tb.Cleanup(func() { barometerHistory = origBarometer })
	for i := 0; i <= 180; i++ {
		barometerHistory.record(101500-float64(i)*300.0/180.0, now.Add(-3*time.Hour+time.Duration(i)*time.Minute))
	}

	return snapshot
}

// syntheticAlarmRules returns derivedCount rules bound to helmcentral.*
// derived paths (capped at the two this package computes from the synthetic
// tree's fuel layout and barometer history) followed by plainCount rules
// bound to ordinary synthetic leaf paths -- backend-perf-audit.md's own
// "~15 enabled rules" scale by default.
func syntheticAlarmRules(derivedCount, plainCount int) []alarmRule {
	derivedPaths := []string{vesselFuelEconomyPath, pressureRatePath}
	rules := make([]alarmRule, 0, derivedCount+plainCount)

	for i := 0; i < derivedCount; i++ {
		rules = append(rules, alarmRule{
			ID: fmt.Sprintf("derived-%d", i), Enabled: true,
			Path: derivedPaths[i%len(derivedPaths)], Label: "synthetic derived",
			Op: alarmOpAbove, Value: -1e9, Hysteresis: 0, DwellSeconds: 0, State: alarmStateAlarm,
		})
	}

	leafPaths := perfSyntheticLeafPaths(plainCount)
	for i := 0; i < plainCount; i++ {
		rules = append(rules, alarmRule{
			ID: fmt.Sprintf("plain-%d", i), Enabled: true,
			Path: leafPaths[i], Label: "synthetic plain",
			Op: alarmOpAbove, Value: 1e9, Hysteresis: 0, DwellSeconds: 0, State: alarmStateWarn,
		})
	}
	return rules
}

// withSyntheticGlobalSnapshot points globalSignalKSnapshot at snapshot for
// the caller's duration. computeDerivedPaths (called from inside
// derivedAwareAlarmReader and buildGaugeValuesPayload) always resolves
// against the global, the same reason every derived-path test in
// derived_paths_test.go does this rather than passing a snapshot through.
func withSyntheticGlobalSnapshot(tb testing.TB, snapshot *signalKSnapshot) {
	tb.Helper()
	original := globalSignalKSnapshot
	globalSignalKSnapshot = snapshot
	tb.Cleanup(func() { globalSignalKSnapshot = original })
}

// installSyntheticGaugeBoundPaths is setPagesWithGaugePaths
// (signalk_paths_test.go) generalised to testing.TB so a benchmark can use
// it too.
func installSyntheticGaugeBoundPaths(tb testing.TB, paths []string) {
	tb.Helper()

	widgets := make([]dashboardLayoutItem, 0, len(paths))
	for i, path := range paths {
		widgets = append(widgets, gaugeWidget(fmt.Sprintf("gauge:perf%04d", i+1), &dashboardGaugeConfig{
			Path: path, Display: "numeric", Quantity: "raw", Unit: "raw",
		}))
	}

	dashboardPagesMu.Lock()
	previous := dashboardPagesState
	dashboardPagesState = map[string]*dashboardPageData{"a": {ID: "a", Widgets: widgets}}
	dashboardPagesMu.Unlock()
	tb.Cleanup(func() {
		dashboardPagesMu.Lock()
		dashboardPagesState = previous
		dashboardPagesMu.Unlock()
	})
}

// ── regression: copy counts must not scale with rule/path count ───────────────

// A tick with several helmcentral.* rules enabled used to rerun
// computeDerivedPaths -- two whole-tree copies each -- once per derived-path
// rule read (backend-perf-audit.md Tier 1 #2). This proves the fix by
// comparing the copy cost of a tick with 2 derived rules against one with 8:
// under the old code the second would cost four times as much; now both cost
// the same, because derivedAwareAlarmReader computes the pass once per
// reader instance and shares it with every rule read from that instance.
func TestEvaluateAlarmsWholeTreeCopiesDoNotScaleWithDerivedRuleCount(t *testing.T) {
	snapshot := buildSyntheticSelfSnapshot(t, perfSyntheticLeafCount)
	withSyntheticGlobalSnapshot(t, snapshot)
	now := time.Now().UTC()

	copiesFor := func(rules []alarmRule) int64 {
		before := atomic.LoadInt64(&snapshotWholeTreeCopies)
		newAlarmEngine().evaluate(rules, derivedAwareAlarmReader(snapshot), now)
		return atomic.LoadInt64(&snapshotWholeTreeCopies) - before
	}

	few := copiesFor(syntheticAlarmRules(2, 13))
	many := copiesFor(syntheticAlarmRules(8, 7))

	if few != many {
		t.Fatalf("whole-tree copies must not scale with the number of derived-path rules: 2 derived -> %d copies, 8 derived -> %d copies", few, many)
	}
	// One copy building the reader's own published() lookup, one running
	// computeDerivedPaths -- see alarmReaderFromTree and
	// derivedAwareAlarmReader's sync.Once.
	if few > 2 {
		t.Fatalf("expected at most 2 whole-tree copies for a tick with derived rules enabled, got %d", few)
	}
}

// A tick with no derived-path rules at all must never trigger
// computeDerivedPaths -- tracks.go relies on exactly this to read three
// ordinary paths through this same reader for free.
func TestEvaluateAlarmsWithNoDerivedRulesNeverRunsComputeDerivedPaths(t *testing.T) {
	snapshot := buildSyntheticSelfSnapshot(t, perfSyntheticLeafCount)
	withSyntheticGlobalSnapshot(t, snapshot)
	now := time.Now().UTC()

	before := atomic.LoadInt64(&snapshotWholeTreeCopies)
	newAlarmEngine().evaluate(syntheticAlarmRules(0, 15), derivedAwareAlarmReader(snapshot), now)
	got := atomic.LoadInt64(&snapshotWholeTreeCopies) - before

	if got != 1 {
		t.Fatalf("expected exactly 1 whole-tree copy (the reader's own published() lookup) with no derived rules, got %d", got)
	}
}

// buildGaugeValuesPayload used to fetch the self tree twice per build --
// once for computeDerivedPaths, again for its own path reader -- regardless
// of how many paths were bound. This asserts it now costs exactly one,
// whether 1 path is bound or 50.
func TestBuildGaugeValuesPayloadCopiesTheTreeOnceRegardlessOfBoundPathCount(t *testing.T) {
	snapshot := buildSyntheticSelfSnapshot(t, perfSyntheticLeafCount)
	withSyntheticGlobalSnapshot(t, snapshot)

	copiesFor := func(paths []string) int64 {
		installSyntheticGaugeBoundPaths(t, paths)
		before := atomic.LoadInt64(&snapshotWholeTreeCopies)
		buildGaugeValuesPayload()
		return atomic.LoadInt64(&snapshotWholeTreeCopies) - before
	}

	leafPaths := perfSyntheticLeafPaths(perfSyntheticLeafCount)
	few := copiesFor(leafPaths[:1])
	many := copiesFor(leafPaths[:50])

	if few != 1 || many != 1 {
		t.Fatalf("expected exactly 1 whole-tree copy per build regardless of bound path count, got %d (1 path) and %d (50 paths)", few, many)
	}
}

// ── benchmarks: before/after numbers for the report ────────────────────────────

// BenchmarkEvaluateAlarmsAgainstLargeSnapshot is one alarm-evaluation pass
// (backend-perf-audit.md's own "~15 enabled rules and a ~3,000-leaf tree")
// -- run against this step's code and, via `git stash`, against the
// pre-fix code with the same rule/path generators, to get real before/after
// numbers rather than an estimate.
func BenchmarkEvaluateAlarmsAgainstLargeSnapshot(b *testing.B) {
	snapshot := buildSyntheticSelfSnapshot(b, perfSyntheticLeafCount)
	withSyntheticGlobalSnapshot(b, snapshot)
	rules := syntheticAlarmRules(2, 13)
	engine := newAlarmEngine()
	now := time.Now().UTC()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.evaluate(rules, derivedAwareAlarmReader(snapshot), now)
	}
}

// BenchmarkBuildGaugeValuesPayloadAgainstLargeSnapshot is one gauge-values
// build against 50 bound paths on the same 3,000-leaf synthetic tree.
func BenchmarkBuildGaugeValuesPayloadAgainstLargeSnapshot(b *testing.B) {
	snapshot := buildSyntheticSelfSnapshot(b, perfSyntheticLeafCount)
	withSyntheticGlobalSnapshot(b, snapshot)
	installSyntheticGaugeBoundPaths(b, perfSyntheticLeafPaths(perfSyntheticLeafCount)[:50])

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buildGaugeValuesPayload()
	}
}
