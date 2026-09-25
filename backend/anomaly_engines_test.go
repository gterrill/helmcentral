package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- Twin gate ---------------------------------------------------------

func steadyTwinPair() []engineTwinState {
	return []engineTwinState{
		{Name: "port", RPMHz: 2000.0 / 60, CoolantK: 348, RunningFor: 20 * time.Minute, RPMSteady: true, CoolantSteady: true},
		{Name: "starboard", RPMHz: 2020.0 / 60, CoolantK: 347, RunningFor: 20 * time.Minute, RPMSteady: true, CoolantSteady: true},
	}
}

func TestTwinGateHoldsForAMatchedSteadyPair(t *testing.T) {
	if !twinGateHolds(steadyTwinPair()) {
		t.Fatalf("expected the gate to hold for a matched, steady, warmed-up pair")
	}
}

func TestTwinGateFailsBelowMinRPM(t *testing.T) {
	engines := steadyTwinPair()
	engines[0].RPMHz = 800.0 / 60
	engines[1].RPMHz = 800.0 / 60
	if twinGateHolds(engines) {
		t.Fatalf("expected the gate to fail below 900 rpm")
	}
}

func TestTwinGateFailsOnRPMSpread(t *testing.T) {
	engines := steadyTwinPair()
	engines[0].RPMHz = 2000.0 / 60
	engines[1].RPMHz = 2100.0 / 60 // 100 rpm apart, over the 50 rpm spread
	if twinGateHolds(engines) {
		t.Fatalf("expected the gate to fail with a 100 rpm spread")
	}
}

func TestTwinGateFailsBelowMinCoolant(t *testing.T) {
	engines := steadyTwinPair()
	engines[0].CoolantK = 330 // ~57 degC, below the 65 degC floor
	if twinGateHolds(engines) {
		t.Fatalf("expected the gate to fail below 65 degC coolant")
	}
}

func TestTwinGateFailsBelowMinRunFor(t *testing.T) {
	engines := steadyTwinPair()
	engines[0].RunningFor = 2 * time.Minute
	if twinGateHolds(engines) {
		t.Fatalf("expected the gate to fail under 10 minutes' runtime")
	}
}

func TestTwinGateFailsWhenNotSteady(t *testing.T) {
	rpmUnsteady := steadyTwinPair()
	rpmUnsteady[0].RPMSteady = false
	if twinGateHolds(rpmUnsteady) {
		t.Fatalf("expected the gate to fail with rpm not yet steady")
	}

	coolantUnsteady := steadyTwinPair()
	coolantUnsteady[1].CoolantSteady = false
	if twinGateHolds(coolantUnsteady) {
		t.Fatalf("expected the gate to fail with coolant not yet steady")
	}
}

func TestTwinGateFailsForASingleEngine(t *testing.T) {
	if twinGateHolds(steadyTwinPair()[:1]) {
		t.Fatalf("expected the gate to fail for a single engine -- nothing to differ against")
	}
}

func TestTwinGateHoldsForThreeEngines(t *testing.T) {
	engines := []engineTwinState{
		{Name: "port", RPMHz: 1800.0 / 60, CoolantK: 350, RunningFor: 15 * time.Minute, RPMSteady: true, CoolantSteady: true},
		{Name: "center", RPMHz: 1820.0 / 60, CoolantK: 349, RunningFor: 15 * time.Minute, RPMSteady: true, CoolantSteady: true},
		{Name: "starboard", RPMHz: 1810.0 / 60, CoolantK: 351, RunningFor: 15 * time.Minute, RPMSteady: true, CoolantSteady: true},
	}
	if !twinGateHolds(engines) {
		t.Fatalf("expected the gate to hold for three matched engines")
	}
}

// --- Residual arithmetic -------------------------------------------------

func TestResidualQuantityAbsentWithNoLearnedOffset(t *testing.T) {
	if _, ok := residualQuantity(350, []float64{348}, 0, false); ok {
		t.Fatalf("expected residual to be absent with no learned offset")
	}
}

func TestResidualQuantitySubtractsPeerMedianAndOffset(t *testing.T) {
	// port 351 K, peers median 348 K -> raw diff 3 K, less a 1 K learned
	// offset -> 2 K residual.
	got, ok := residualQuantity(351, []float64{348}, 1, true)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if got != 2 {
		t.Fatalf("residual: got %v, want 2", got)
	}
}

func TestResidualQuantityUsesMedianOfMultiplePeers(t *testing.T) {
	// three-engine boat: one engine against the median of the other two.
	got, ok := residualQuantity(360, []float64{350, 352}, 0, true)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if got != 9 { // 360 - median(350,352)=351 = 9
		t.Fatalf("residual: got %v, want 9", got)
	}
}

// --- learnTwinBaseline: hand-built synthetic []telemetryPoint series -------

// buildSteadyTwinSeries constructs n minutes of hand-built, synthetic
// telemetryPoint data for two engines, gate-qualifying throughout (rpm
// steady around steadyRPM, coolant steady, both "running" from well before
// minute 0). Port's oil pressure runs offsetPa above starboard's the whole
// time -- the learned bias a real boat's port engine might genuinely carry.
// This is synthetic test data (per AGENTS.md/the anomaly-detection plan:
// algorithm tests for baseline learning use hand-built telemetryPoint
// series, not a captured Influx payload), not a claim about any real boat.
func buildSteadyTwinSeries(t *testing.T, n int, steadyRPM, offsetPa float64) []engineSeries {
	t.Helper()
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	rpmHz := steadyRPM / 60

	port := engineSeries{Name: "port"}
	stbd := engineSeries{Name: "starboard"}
	for i := 0; i < n; i++ {
		at := start.Add(time.Duration(i) * time.Minute)
		port.RPM = append(port.RPM, telemetryPoint{Value: rpmHz, Timestamp: at})
		stbd.RPM = append(stbd.RPM, telemetryPoint{Value: rpmHz, Timestamp: at})
		port.Coolant = append(port.Coolant, telemetryPoint{Value: 350, Timestamp: at})
		stbd.Coolant = append(stbd.Coolant, telemetryPoint{Value: 349, Timestamp: at})
		port.Quantity = append(port.Quantity, telemetryPoint{Value: 300000 + offsetPa, Timestamp: at})
		stbd.Quantity = append(stbd.Quantity, telemetryPoint{Value: 300000, Timestamp: at})
	}
	return []engineSeries{port, stbd}
}

func TestLearnTwinBaselineLearnsAConsistentOffset(t *testing.T) {
	series := buildSteadyTwinSeries(t, 50, 1800, 5000) // 50 min at 1800 rpm, port +5000 Pa

	result, err := learnTwinBaseline(series)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	portBuckets := result["port"]
	if len(portBuckets) != 1 {
		t.Fatalf("expected exactly one learned bucket for port, got %+v", portBuckets)
	}
	b := portBuckets[0]
	if b.RPMBucket != 1800 {
		t.Fatalf("bucket: got %d, want 1800 (1800 rpm floors to its own 200 rpm bucket)", b.RPMBucket)
	}
	if b.Median != 5000 {
		t.Fatalf("median: got %v, want 5000 (port always ran 5000 Pa above starboard)", b.Median)
	}
	if b.MAD != 0 {
		t.Fatalf("MAD: got %v, want 0 (perfectly consistent offset)", b.MAD)
	}
	// Of the 50 minutes, the first 10 don't clear twinGateMinRunFor (10 min)
	// yet, and steadyRun then needs 5 more consecutive qualifying minutes
	// before it counts the run as steady at all (engineBaselineMinRunMinutes):
	// 50 - 10 - 4 = 36 (the first qualifying minute at i=10 is itself the
	// 1st of the 5-minute ramp-up, so only 4 more are absorbed after it).
	if b.Minutes != 36 {
		t.Fatalf("minutes: got %d, want 36 (50 total, less a 10 min runtime ramp-up and a 5 min steady-run ramp-up that overlaps it by one minute)", b.Minutes)
	}

	stbdBuckets := result["starboard"]
	if len(stbdBuckets) != 1 || stbdBuckets[0].Median != -5000 {
		t.Fatalf("expected starboard's mirrored -5000 Pa offset, got %+v", stbdBuckets)
	}
}

// TestLearnTwinBaselineDropsThinBuckets asserts a bucket under
// engineBaselineMinMinutes (30) qualifying minutes is dropped entirely, not
// reported with a small sample.
func TestLearnTwinBaselineDropsThinBuckets(t *testing.T) {
	series := buildSteadyTwinSeries(t, engineBaselineMinRunMinutes+10, 1800, 5000) // 15 min: qualifies, but under 30
	result, err := learnTwinBaseline(series)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result["port"]) != 0 {
		t.Fatalf("expected no bucket under 30 qualifying minutes, got %+v", result["port"])
	}
}

// TestLearnTwinBaselineRequiresAFullRun asserts a qualifying stretch
// shorter than engineBaselineMinRunMinutes contributes nothing at all, even
// though the underlying instants individually clear the gate.
func TestLearnTwinBaselineRequiresAFullRun(t *testing.T) {
	series := buildSteadyTwinSeries(t, engineBaselineMinRunMinutes-1, 1800, 5000)
	result, err := learnTwinBaseline(series)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result["port"]) != 0 || len(result["starboard"]) != 0 {
		t.Fatalf("expected no buckets from a run shorter than the minimum, got port=%+v starboard=%+v", result["port"], result["starboard"])
	}
}

// TestLearnTwinBaselineExcludesNonQualifyingMinutes asserts a stretch below
// the gate's rpm floor contributes no samples at all, even surrounded by
// plenty of qualifying data on both sides -- a slow-down that drops the
// gate (without stopping the engine outright, so runtime keeps accumulating
// through it) must not silently count toward the learned offset.
func TestLearnTwinBaselineExcludesNonQualifyingMinutes(t *testing.T) {
	series := buildSteadyTwinSeries(t, 60, 1800, 5000)
	// Drop rpm below the 900 rpm gate floor (but well above the idle
	// runtime-reset cutoff) for a 5 minute stretch in the middle.
	for e := range series {
		for i := 25; i < 30; i++ {
			series[e].RPM[i].Value = 700.0 / 60
		}
	}

	result, err := learnTwinBaseline(series)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	total := 0
	for _, b := range result["port"] {
		total += b.Minutes
	}
	// If the 5 dropped-rpm minutes had counted, or the run-length reset
	// they cause had not cost anything either, this would land at 60 (or
	// 55). A materially lower total proves both the dip itself and the
	// steady-run ramp-up on either side of it were excluded, not merely
	// that the dip's own 5 minutes were skipped.
	if total == 0 {
		t.Fatalf("expected some qualifying minutes either side of the dip, got none: %+v", result["port"])
	}
	if total >= 55 {
		t.Fatalf("expected the rpm dip (and the ramp-up either side of it) to cost qualifying minutes, got %d of 60", total)
	}
}

func TestLearnTwinBaselineRejectsFewerThanTwoEngines(t *testing.T) {
	series := buildSteadyTwinSeries(t, 40, 1800, 5000)[:1]
	if _, err := learnTwinBaseline(series); err == nil {
		t.Fatalf("expected an error with only one engine's series")
	}
}

// TestLearnTwinBaselineToleratesMismatchedSeriesLengths covers the real
// case a strict index-aligned join used to reject outright: one engine's
// rpm fetch came back one point short (a real Influx chunk boundary drops
// or duplicates rows independently per query). learnTwinBaseline now joins
// on the minute, not on array position, so the one minute starboard's rpm
// is missing for simply falls out of the join -- the other 49 still learn
// a baseline, not an error.
func TestLearnTwinBaselineToleratesMismatchedSeriesLengths(t *testing.T) {
	series := buildSteadyTwinSeries(t, 50, 1800, 5000)
	series[1].RPM = series[1].RPM[:49] // drop the last point, so lengths no longer match

	result, err := learnTwinBaseline(series)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	portBuckets := result["port"]
	if len(portBuckets) != 1 {
		t.Fatalf("expected exactly one learned bucket for port, got %+v", portBuckets)
	}
	b := portBuckets[0]
	if b.RPMBucket != 1800 || b.Median != 5000 {
		t.Fatalf("got %+v, want RPMBucket 1800, Median 5000", b)
	}
	if b.Minutes != 35 {
		t.Fatalf("minutes: got %d, want 35 (49 joined minutes, minus the 14-minute warmup, same arithmetic as the 50-minute case)", b.Minutes)
	}
}

// TestLearnTwinBaselineTruncatesTimestampsToTheMinute covers the other
// half of the same join: two engines' own fetches are never sent by the
// gateway at the same wall-clock instant, so a few hundred milliseconds --
// or here, thirty seconds -- of jitter between them is normal, not a
// misalignment. bucketToMinutes floors every timestamp to its minute
// before joining, so this jitter must have no effect at all: the reading
// still lands in the same minute bucket as its peers, and the learned
// result is identical to the perfectly-aligned case.
func TestLearnTwinBaselineTruncatesTimestampsToTheMinute(t *testing.T) {
	series := buildSteadyTwinSeries(t, 50, 1800, 5000)
	series[1].RPM[10].Timestamp = series[1].RPM[10].Timestamp.Add(30 * time.Second)

	result, err := learnTwinBaseline(series)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	portBuckets := result["port"]
	if len(portBuckets) != 1 {
		t.Fatalf("expected exactly one learned bucket for port, got %+v", portBuckets)
	}
	b := portBuckets[0]
	if b.RPMBucket != 1800 || b.Median != 5000 || b.Minutes != 36 {
		t.Fatalf("got %+v, want RPMBucket 1800, Median 5000, Minutes 36 (same as the fully-aligned 50-minute case: 30s of jitter within a minute must not change the join)", b)
	}
}

func TestEngineBaselineOffsetForLooksUpByBucket(t *testing.T) {
	buckets := []engineBaselineBucket{
		{RPMBucket: 1600, Median: 3, Minutes: 40},
		{RPMBucket: 1800, Median: 5, Minutes: 40},
	}
	got, ok := engineBaselineOffsetFor(buckets, 1850.0/60)
	if !ok || got != 5 {
		t.Fatalf("got (%v, %v), want (5, true)", got, ok)
	}
	if _, ok := engineBaselineOffsetFor(buckets, 2200.0/60); ok {
		t.Fatalf("expected no offset for an rpm bucket that was never learned")
	}
}

// --- Baseline persistence --------------------------------------------------

func TestLoadEngineBaselineMissingFileIsZeroValueNoError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	b, err := loadEngineBaseline(path)
	if err != nil {
		t.Fatalf("unexpected error for a missing file: %v", err)
	}
	if len(b.Engines) != 0 {
		t.Fatalf("expected a zero-value baseline, got %+v", b)
	}
}

func TestSaveEngineBaselineRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	want := engineBaseline{
		Engines: map[string]map[string][]engineBaselineBucket{
			"port": {"oilPressure": {{RPMBucket: 1800, Median: 5000, MAD: 200, Minutes: 40}}},
		},
		ComputedAt:   time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC),
		LookbackDays: engineBaselineLookbackDays,
	}
	if err := saveEngineBaseline(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := loadEngineBaseline(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.LookbackDays != want.LookbackDays {
		t.Fatalf("LookbackDays: got %d, want %d", got.LookbackDays, want.LookbackDays)
	}
	if !got.ComputedAt.Equal(want.ComputedAt) {
		t.Fatalf("ComputedAt: got %v, want %v", got.ComputedAt, want.ComputedAt)
	}
	gotBucket := got.Engines["port"]["oilPressure"][0]
	wantBucket := want.Engines["port"]["oilPressure"][0]
	if gotBucket != wantBucket {
		t.Fatalf("bucket: got %+v, want %+v", gotBucket, wantBucket)
	}
}

// TestLoadEngineBaselineCorruptFileFailsFast is the plan's "a corrupt file
// is logged as an error and treated as no baseline, never as zero offsets":
// loadEngineBaseline itself must surface the error rather than quietly
// returning the zero value, so a caller that does treat a load failure as
// "no baseline" is making that choice deliberately (and can log it), not by
// accident.
func TestLoadEngineBaselineCorruptFileFailsFast(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := loadEngineBaseline(path); err == nil {
		t.Fatalf("expected a corrupt baseline file to surface an error")
	}
}
