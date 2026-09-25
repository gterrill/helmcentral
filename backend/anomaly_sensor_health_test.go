package main

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// --- 1. classifyRange: physical-limits sanity table -------------------------

// loadSignalKPathsFixture reads a {"paths": [...]} capture in the same shape
// signalk_paths.go's own picker emits, shared with the frontend fixture of
// the same name (frontend/src/test/fixtures/signalk-paths-2026-09-08.json --
// captured while Pikorua's engines were running, unlike the engines-off
// 2026-09-25 snapshot under testdata/anomaly/).
func loadSignalKPathsFixture(t *testing.T, path string) []signalKPath {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}
	var wrapper struct {
		Paths []signalKPath `json:"paths"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		t.Fatalf("unmarshalling fixture %s: %v", path, err)
	}
	return wrapper.Paths
}

// TestClassifyRangeAcceptsRealFixtureValues asserts classifyRange never
// calls a real, live captured reading out of range: every engine and
// battery value in the fixture is a genuine reading Pikorua published while
// under way, so any of them landing on rangeOutOfRange would mean the table
// itself is wrong, not the data.
func TestClassifyRangeAcceptsRealFixtureValues(t *testing.T) {
	paths := loadSignalKPathsFixture(t, "../frontend/src/test/fixtures/signalk-paths-2026-09-08.json")

	checked := 0
	for _, p := range paths {
		if p.Value == nil {
			continue
		}
		if verdict := classifyRange(p.Path, *p.Value); verdict == rangeOutOfRange {
			t.Errorf("classifyRange(%q, %v) = rangeOutOfRange, want valid or not-applicable", p.Path, *p.Value)
		}
		checked++
	}
	if checked == 0 {
		t.Fatalf("fixture carried no values to check")
	}
}

func TestClassifyRangeFlagsImpossibleReadings(t *testing.T) {
	cases := []struct {
		name  string
		path  string
		value float64
	}{
		{"negative rpm", "propulsion.port.revolutions", -1},
		{"absolute zero coolant", "propulsion.port.temperature", 0},
		{"negative oil pressure", "propulsion.port.oilPressure", -1000},
		{"engine load over 100%", "propulsion.port.engineLoad", 3.0},
		{"pack voltage impossibly high", "electrical.batteries.512.voltage", 400},
		{"SoC over 100%", "electrical.batteries.512.capacity.stateOfCharge", 4.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyRange(tc.path, tc.value); got != rangeOutOfRange {
				t.Fatalf("classifyRange(%q, %v) = %v, want rangeOutOfRange", tc.path, tc.value, got)
			}
		})
	}
}

func TestClassifyRangeNotApplicableForUnknownPath(t *testing.T) {
	if got := classifyRange("navigation.speedOverGround", 5.0); got != rangeNotApplicable {
		t.Fatalf("classifyRange on a quantity with no limits table entry: got %v, want rangeNotApplicable", got)
	}
}

// --- 2. frozenVerdict --------------------------------------------------------

func steadyFrozenWindow() frozenWindow {
	return frozenWindow{
		UpdateCount:   90,
		ValueChanged:  false,
		StillUpdating: true,
		MinEngineHz:   600.0 / 60,
		MaxEngineHz:   1000.0 / 60,
	}
}

func TestFrozenVerdictFiresOnFlatValueWhileRPMSwings(t *testing.T) {
	if !frozenVerdict("propulsion.port.temperature", steadyFrozenWindow(), nil) {
		t.Fatalf("expected a frozen verdict for a flat value while rpm swung 400rpm above 600rpm")
	}
}

func TestFrozenVerdictNoVerdictWithEnginesOff(t *testing.T) {
	w := steadyFrozenWindow()
	w.MinEngineHz = 0
	w.MaxEngineHz = 0
	if frozenVerdict("propulsion.port.temperature", w, nil) {
		t.Fatalf("expected no verdict with engines off")
	}
}

func TestFrozenVerdictNoVerdictWithConstantRPM(t *testing.T) {
	w := steadyFrozenWindow()
	w.MinEngineHz = 20 // 1200rpm
	w.MaxEngineHz = 20
	if frozenVerdict("propulsion.port.temperature", w, nil) {
		t.Fatalf("expected no verdict when rpm never varied")
	}
}

func TestFrozenVerdictNoVerdictWithMovingValue(t *testing.T) {
	w := steadyFrozenWindow()
	w.ValueChanged = true
	if frozenVerdict("propulsion.port.temperature", w, nil) {
		t.Fatalf("expected no verdict when the value itself moved")
	}
}

func TestFrozenVerdictNoVerdictWhenPathStopsUpdating(t *testing.T) {
	w := steadyFrozenWindow()
	w.StillUpdating = false
	if frozenVerdict("propulsion.port.temperature", w, nil) {
		t.Fatalf("expected no verdict once the path itself stopped updating")
	}
}

func TestFrozenVerdictNoVerdictBelowMinUpdates(t *testing.T) {
	w := steadyFrozenWindow()
	w.UpdateCount = 59
	if frozenVerdict("propulsion.port.temperature", w, nil) {
		t.Fatalf("expected no verdict with only 59 updates (< 60 over the window)")
	}
}

func TestFrozenVerdictSkipsExcludedPaths(t *testing.T) {
	excluded := map[string]bool{"propulsion.port.temperature": true}
	if frozenVerdict("propulsion.port.temperature", steadyFrozenWindow(), excluded) {
		t.Fatalf("expected an excluded path to never get a frozen verdict")
	}
}

// --- 3. silentSources --------------------------------------------------------

func TestSilentSourcesFiresForAWatchedQuietSource(t *testing.T) {
	now := time.Date(2026, 9, 25, 7, 0, 0, 0, time.UTC)
	sources := []sourceHealth{
		{Source: "venus.battery.512", First: now.Add(-10 * time.Minute), Last: now.Add(-3 * time.Minute), Count: 200},
	}
	got := silentSources(sources, now, 2*time.Second, nil)
	if len(got) != 1 || got[0] != "venus.battery.512" {
		t.Fatalf("silentSources: got %v, want [venus.battery.512]", got)
	}
}

func TestSilentSourcesNotWatchedYet(t *testing.T) {
	now := time.Date(2026, 9, 25, 7, 0, 0, 0, time.UTC)
	// Seen for only 1 minute (< 5 min watch-in period), even though it has
	// been quiet for well over 120s.
	sources := []sourceHealth{
		{Source: "new.device", First: now.Add(-1 * time.Minute), Last: now.Add(-45 * time.Second), Count: 5},
	}
	if got := silentSources(sources, now, 2*time.Second, nil); len(got) != 0 {
		t.Fatalf("expected an unwatched (too-new) source not to fire, got %v", got)
	}
}

func TestSilentSourcesNotEnoughUpdatesYet(t *testing.T) {
	now := time.Date(2026, 9, 25, 7, 0, 0, 0, time.UTC)
	// Seen for 6 minutes but only a handful of updates (< 30).
	sources := []sourceHealth{
		{Source: "slow.device", First: now.Add(-6 * time.Minute), Last: now.Add(-3 * time.Minute), Count: 10},
	}
	if got := silentSources(sources, now, 2*time.Second, nil); len(got) != 0 {
		t.Fatalf("expected a source under 30 updates not to be watched yet, got %v", got)
	}
}

func TestSilentSourcesStillFreshDoesNotFire(t *testing.T) {
	now := time.Date(2026, 9, 25, 7, 0, 0, 0, time.UTC)
	sources := []sourceHealth{
		{Source: "venus.battery.512", First: now.Add(-10 * time.Minute), Last: now.Add(-30 * time.Second), Count: 200},
	}
	if got := silentSources(sources, now, 2*time.Second, nil); len(got) != 0 {
		t.Fatalf("expected a source quiet for only 30s not to fire, got %v", got)
	}
}

func TestSilentSourcesSuppressedWhenStreamItselfIsStale(t *testing.T) {
	now := time.Date(2026, 9, 25, 7, 0, 0, 0, time.UTC)
	sources := []sourceHealth{
		{Source: "venus.battery.512", First: now.Add(-10 * time.Minute), Last: now.Add(-3 * time.Minute), Count: 200},
	}
	// The whole stream is 15s old -- a dead SignalK connection, a different
	// and already-alarmed failure, not this source specifically going quiet.
	if got := silentSources(sources, now, 15*time.Second, nil); len(got) != 0 {
		t.Fatalf("expected silentSources to suppress everything while the stream itself is stale, got %v", got)
	}
}

// TestSilentSourcesToleratesASourceThatOnlyReportsOnChange covers code
// review finding 8: silentSourceQuietFor used to be one flat 120s for every
// source, so a source that genuinely, healthily reports only every couple
// of minutes -- an on-change value like a switch or alarm state, not a
// periodic sensor -- would trip "silent" on its own ordinary cadence. The
// threshold must scale with what this source has actually been observed
// doing: 10x its own average gap (first-to-last spread over update count),
// floored at the plan's 120s so a source with a fast or noisy cadence keeps
// the original number.
func TestSilentSourcesToleratesASourceThatOnlyReportsOnChange(t *testing.T) {
	now := time.Date(2026, 9, 25, 7, 0, 0, 0, time.UTC)
	sources := []sourceHealth{
		// Average gap: (60min-10min)/(31-1) = 50min/30 = 100s. Scaled
		// threshold: 100s x 10 = 1000s (~16.7min). Quiet for 10 minutes
		// (600s) is well past the flat 120s floor but still under this
		// source's own scaled threshold.
		{Source: "n2k.switch.bilge-pump", First: now.Add(-60 * time.Minute), Last: now.Add(-10 * time.Minute), Count: 31},
	}
	if got := silentSources(sources, now, 2*time.Second, nil); len(got) != 0 {
		t.Fatalf("expected an on-change source quiet for only 10 minutes (under its own ~16.7min scaled threshold) not to fire, got %v", got)
	}
}

func TestSilentSourcesStillFiresOnceQuietPastItsOwnScaledCadence(t *testing.T) {
	now := time.Date(2026, 9, 25, 7, 0, 0, 0, time.UTC)
	sources := []sourceHealth{
		// Same 50-minute First-to-Last spread as the tolerant case above, so
		// the same ~100s average gap and ~1000s scaled threshold, but quiet
		// for 20 minutes (1200s) since Last -- genuinely gone, not just
		// between two ordinary updates.
		{Source: "n2k.switch.bilge-pump", First: now.Add(-70 * time.Minute), Last: now.Add(-20 * time.Minute), Count: 31},
	}
	got := silentSources(sources, now, 2*time.Second, nil)
	if len(got) != 1 || got[0] != "n2k.switch.bilge-pump" {
		t.Fatalf("expected a source quiet well past its own scaled cadence to still fire, got %v", got)
	}
}

// TestSilentSourcesFiresForATightlyClusteredButStaleBurst used to be
// TestSilentSourcesDoesNotFireForAConnectTimeBurstFollowedByQuiet, guarding
// the opposite outcome under the old, now-removed silentSourceBurstSpread
// gate (code review finding 8, prior cycle): a burst-then-quiet on-change
// source's near-zero ARRIVAL-based average gap floored its threshold to the
// flat 120s and fired 5 minutes after every restart regardless of health.
// That defect is fixed at its root now -- First/Last come from the source's
// own declared timestamps (signalk_snapshot.go's applyDelta), not arrival --
// so a burst this tight is exactly what a source already dead before the
// backend even started looks like (code review finding, the 2026-09-21
// YachtDevices gateway outage): there is no other history it will ever get
// to earn a wider spread from, and it must fire once watched, not hide
// behind the very tightness of its own last gasp.
func TestSilentSourcesFiresForATightlyClusteredButStaleBurst(t *testing.T) {
	now := time.Date(2026, 9, 25, 7, 0, 0, 0, time.UTC)
	burstAt := now.Add(-6 * time.Minute)
	sources := []sourceHealth{
		// The whole 40-update burst carries declared timestamps within 30ms
		// of each other, six minutes ago -- past silentSourceWatchAfter and
		// silentSourceMinUpdates on the burst alone -- and nothing since.
		{Source: "n2k.switch.bilge-pump", First: burstAt, Last: burstAt.Add(30 * time.Millisecond), Count: 40},
	}
	got := silentSources(sources, now, 2*time.Second, nil)
	if len(got) != 1 || got[0] != "n2k.switch.bilge-pump" {
		t.Fatalf("expected a tightly-clustered but stale burst to fire once watched, got %v", got)
	}
}

func TestSilentSourcesExcludesEngineBoundSources(t *testing.T) {
	now := time.Date(2026, 9, 25, 7, 0, 0, 0, time.UTC)
	sources := []sourceHealth{
		{Source: "n2k.engine.port", First: now.Add(-10 * time.Minute), Last: now.Add(-3 * time.Minute), Count: 200},
	}
	excluded := map[string]bool{"n2k.engine.port": true}
	if got := silentSources(sources, now, 2*time.Second, excluded); len(got) != 0 {
		t.Fatalf("expected an excluded engine-bound source never to fire, got %v", got)
	}
}

// TestSilentSourcesAgainstCapturedFixture replays the real ~10 minute
// captured delta stream (backend/testdata/anomaly/signalk-deltas-2026-09-25.ndjson,
// carrying $source for battery.512, vebus.276 and alternator.0/1) and
// confirms silentSources reports nothing silent evaluated right at the last
// delta's own timestamp: this is live, continuously-flowing real data, so a
// false positive here would mean the check itself is wrong.
func TestSilentSourcesAgainstCapturedFixture(t *testing.T) {
	snapshot, lastDeltaAt := replayAnomalyDeltaFixture(t)

	sources := snapshot.sourcesFor(snapshot.selfContext())
	if len(sources) == 0 {
		t.Fatalf("expected the captured fixture to populate at least one tracked source")
	}

	var health []sourceHealth
	for source, entry := range sources {
		health = append(health, sourceHealth{Source: source, First: entry.First, Last: entry.Last, Count: entry.Count})
	}

	got := silentSources(health, lastDeltaAt, 0, nil)
	if len(got) != 0 {
		t.Fatalf("expected no silent sources evaluated at the fixture's own last timestamp, got %v", got)
	}
}

// TestApplyDeltaFlagsASourceDeadBeforeBackendStart is the integration
// regression case for code review finding 1: the 2026-09-21 YachtDevices
// gateway outage left a source dead well before this backend's next
// restart. SignalK replays that dead source's whole retained state at
// connect -- every path arrives "now", but each still carries its own
// original (long-stale) declared timestamp (signalk_paths.go's pathAge
// comment: the node's own timestamp survives a replay; arrival time does
// not). Before this fix, applyDelta tracked sourceSeen's First/Last from
// arrival alone, so a source whose ENTIRE history is that replay burst
// never satisfied the old burst-spread gate and was never watched --
// silent, forever, with nothing on screen to say so. This exercises the
// real ingestion path (applyDelta -> sourcesFor), not just silentSources in
// isolation.
func TestApplyDeltaFlagsASourceDeadBeforeBackendStart(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()

	const deadSource = "yachtdevices.6"
	const deadTimestamp = "2026-09-21T10:34:00Z" // the real outage
	restartAt := time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC)

	// The gateway's whole retained tank state, replayed within
	// milliseconds of this backend's restart, four days later -- 40
	// distinct readings, each keeping its own long-dead declared timestamp.
	for i := 0; i < 40; i++ {
		arrival := restartAt.Add(time.Duration(i) * time.Millisecond)
		snapshot.applyDelta(signalKDelta{
			Context: "vessels.self",
			Updates: []signalKUpdate{{
				SourceRef: deadSource,
				Timestamp: deadTimestamp,
				Values:    []signalKValue{{Path: fmt.Sprintf("tanks.freshWater.%d.currentLevel", i), Value: 0.5}},
			}},
		}, arrival)
	}

	sources := snapshot.sourcesFor(snapshot.selfContext())
	entry, ok := sources[deadSource]
	if !ok {
		t.Fatalf("expected the dead source to be tracked at all")
	}
	health := []sourceHealth{{Source: deadSource, First: entry.First, Last: entry.Last, Count: entry.Count}}

	got := silentSources(health, restartAt.Add(45*time.Millisecond), 0, nil)
	if len(got) != 1 || got[0] != deadSource {
		t.Fatalf("expected the dead-before-start source flagged silent immediately, got %v (tracked entry=%+v)", got, entry)
	}
}

// TestApplyDeltaDoesNotFlagAHealthyOnChangeSourceReplayedAtRestart is the
// companion integration case: a switch/alarm-state gateway that last
// genuinely changed a few minutes before an ordinary backend restart must
// not read as freshly silent just because the restart's replay burst
// delivered everything within milliseconds of "now" by arrival clock. Its
// own declared timestamps say it was only quiet a few minutes, well inside
// the scaled threshold its own on-change cadence earns it -- restarting the
// backend must never, by itself, make a healthy source look worse than it
// is.
func TestApplyDeltaDoesNotFlagAHealthyOnChangeSourceReplayedAtRestart(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()

	const source = "n2k.switch.panel"
	restartAt := time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC)

	// 31 distinct switch states, each replayed within milliseconds of the
	// restart by arrival clock but keeping its own last-genuinely-changed
	// declared timestamp: the oldest 70 minutes before the restart, the
	// newest only 3 minutes before it -- the same on-change cadence
	// TestSilentSourcesToleratesASourceThatOnlyReportsOnChange already
	// accepts at the pure-function level.
	for i := 0; i < 31; i++ {
		arrival := restartAt.Add(time.Duration(i) * time.Millisecond)
		changedAt := restartAt.Add(-70 * time.Minute).Add(time.Duration(i) * (67 * time.Minute / 30))
		snapshot.applyDelta(signalKDelta{
			Context: "vessels.self",
			Updates: []signalKUpdate{{
				SourceRef: source,
				Timestamp: changedAt.UTC().Format(time.RFC3339),
				Values:    []signalKValue{{Path: fmt.Sprintf("electrical.switches.panel.%d.state", i), Value: 1.0}},
			}},
		}, arrival)
	}

	sources := snapshot.sourcesFor(snapshot.selfContext())
	entry, ok := sources[source]
	if !ok {
		t.Fatalf("expected the source to be tracked at all")
	}

	// Evaluated 6 minutes after the restart -- past silentSourceWatchAfter,
	// exactly the window the old arrival-based design falsely fired in.
	evalAt := restartAt.Add(6 * time.Minute)
	health := []sourceHealth{{Source: source, First: entry.First, Last: entry.Last, Count: entry.Count}}
	got := silentSources(health, evalAt, 0, nil)
	if len(got) != 0 {
		t.Fatalf("expected a healthy on-change source not to false-alarm 6 minutes after an ordinary restart, got %v (tracked entry=%+v)", got, entry)
	}
}

// --- 4. inputValidity --------------------------------------------------------

func TestInputValidityFalseForAbsentPath(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")
	if inputValidity(snapshot, "propulsion.port.temperature", testNow) {
		t.Fatalf("expected an absent path to be invalid")
	}
}

func TestInputValidityFalseForOutOfRangeValue(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{
			Timestamp: "2026-08-12T10:00:00.000Z",
			SourceRef: "n2k.1",
			Values:    []signalKValue{{Path: "propulsion.port.temperature", Value: 0.0}},
		}},
	}, testNow)

	if inputValidity(snapshot, "propulsion.port.temperature", testNow) {
		t.Fatalf("expected an out-of-range value to be invalid")
	}
}

func TestInputValidityTrueForFreshInRangeValue(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{
			Timestamp: "2026-08-12T10:00:00.000Z",
			SourceRef: "n2k.1",
			Values:    []signalKValue{{Path: "propulsion.port.temperature", Value: 348.1}},
		}},
	}, testNow)

	if !inputValidity(snapshot, "propulsion.port.temperature", testNow) {
		t.Fatalf("expected a fresh, in-range value to be valid")
	}
}
