package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// snapshotWithSelfValues builds a snapshot carrying several self paths at
// once, for the cases that need a whole derivation's inputs present.
//
// Received "just now" (time.Now(), not the fixed historical alarmNow other
// alarm-engine tests use) rather than a value with no timestamp of its own:
// callers exercise derivedAwareAlarmReader, which resolves a derived path
// through computeDerivedPaths(time.Now().UTC()) (ADR 0084's staleness
// guard), so a pathSeen record set to a fixed date in the past would read as
// stale by however far real "now" has drifted from it and the derived value
// would go absent for a reason this helper's callers are not testing.
func snapshotWithSelfValues(values map[string]any) *signalKSnapshot {
	snapshot := newSignalKSnapshot()
	entries := make([]signalKValue, 0, len(values))
	for path, value := range values {
		entries = append(entries, signalKValue{Path: path, Value: value})
	}
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: entries}},
	}, time.Now().UTC())
	snapshot.setSelfContext("vessels.self")
	return snapshot
}

func fakeReader(values map[string]float64) alarmReader {
	return func(path string) alarmSample {
		v, ok := values[path]
		if !ok {
			return alarmSample{}
		}
		return alarmSample{Value: v, Present: true}
	}
}

/*
Vessel fuel economy, derived because nothing publishes it.

SignalK's propulsion.<id>.fuel.economy is per engine: on a twin it reports that
engine's distance per unit volume, which is roughly twice the boat's. Range
planned off one engine's figure is out by the number of engines running.

The numbers below are a live reading: 10.21 kn over two engines burning 23.9
and 23.5 L/h.
*/
func TestVesselFuelEconomy(t *testing.T) {
	read := fakeReader(map[string]float64{
		"navigation.speedOverGround":     5.2524791084058196,
		"propulsion.port.fuel.rate":      6.638888888888889e-06,
		"propulsion.starboard.fuel.rate": 6.527777777777778e-06,
	})

	got, ok := vesselFuelEconomy(read, []string{"propulsion.port.fuel.rate", "propulsion.starboard.fuel.rate"})
	if !ok {
		t.Fatal("expected an economy figure")
	}

	// 5.2525 m/s over 1.31667e-05 m3/s, in metres per cubic metre.
	if math.Abs(got-398924) > 500 {
		t.Fatalf("expected about 398924 m/m3, got %v", got)
	}
	// And about half either engine's own figure, which is the whole point.
	if got > 500000 {
		t.Fatalf("a twin's vessel economy must be well below one engine's ~793000, got %v", got)
	}
}

func TestVesselFuelEconomySumsEveryRunningEngine(t *testing.T) {
	one, _ := vesselFuelEconomy(
		fakeReader(map[string]float64{
			"navigation.speedOverGround": 5.0,
			"propulsion.port.fuel.rate":  1e-05,
		}),
		[]string{"propulsion.port.fuel.rate"},
	)
	two, _ := vesselFuelEconomy(
		fakeReader(map[string]float64{
			"navigation.speedOverGround":     5.0,
			"propulsion.port.fuel.rate":      1e-05,
			"propulsion.starboard.fuel.rate": 1e-05,
		}),
		[]string{"propulsion.port.fuel.rate", "propulsion.starboard.fuel.rate"},
	)
	if math.Abs(two*2-one) > 1 {
		t.Fatalf("a second engine burning the same should halve economy: %v then %v", one, two)
	}
}

// An engine that is off contributes nothing rather than making the boat absent.
func TestVesselFuelEconomyIgnoresAnEngineThatIsNotReporting(t *testing.T) {
	got, ok := vesselFuelEconomy(
		fakeReader(map[string]float64{
			"navigation.speedOverGround": 5.0,
			"propulsion.port.fuel.rate":  1e-05,
		}),
		[]string{"propulsion.port.fuel.rate", "propulsion.starboard.fuel.rate"},
	)
	if !ok || math.Abs(got-500000) > 1 {
		t.Fatalf("expected the running engine's figure, got %v ok=%v", got, ok)
	}
}

/*
Absent, never zero and never infinite.

Zero economy reads as "this boat travels no distance per litre", which is a
measurement. Stopped at anchor it is simply not a defined figure.
*/
func TestVesselFuelEconomyIsAbsentWhenItCannotBeComputed(t *testing.T) {
	cases := map[string]map[string]float64{
		"no speed":       {"propulsion.port.fuel.rate": 1e-05},
		"no burn at all": {"navigation.speedOverGround": 5.0},
		"engines off":    {"navigation.speedOverGround": 5.0, "propulsion.port.fuel.rate": 0},
		"stopped":        {"navigation.speedOverGround": 0, "propulsion.port.fuel.rate": 1e-05},
	}
	for name, values := range cases {
		if got, ok := vesselFuelEconomy(fakeReader(values), []string{"propulsion.port.fuel.rate"}); ok {
			t.Fatalf("%s: expected no figure, got %v", name, got)
		}
	}
}

func TestDerivedPathsAreDiscoverableAndDistinct(t *testing.T) {
	if len(derivedPathIDs) == 0 {
		t.Fatal("expected at least one derived path")
	}
	for _, path := range derivedPathIDs {
		// Namespaced so a derived path can never shadow one the vessel publishes.
		if len(path) < len(derivedPathPrefix) || path[:len(derivedPathPrefix)] != derivedPathPrefix {
			t.Fatalf("derived path %q must carry the %q prefix", path, derivedPathPrefix)
		}
	}
}

// ADR 0055 states that a derived path can be bound anywhere a path can,
// naming alarm rules among them. That was not true: evaluateAlarmsOnce read
// through snapshotAlarmReader, which walks the SignalK tree only, so a rule
// naming helmcentral.* saw absence. Absence is never a value, so the rule
// silently never fired - the worst failure mode an alarm can have.
func TestDerivedAwareAlarmReader_ResolvesDerivedPaths(t *testing.T) {
	snapshot := snapshotWithSelfValues(map[string]any{
		"navigation.speedOverGround": 5.2525,
		"propulsion.port.fuel.rate":  6.639e-06,
		"propulsion.stbd.fuel.rate":  6.528e-06,
	})

	origSnapshot := globalSignalKSnapshot
	globalSignalKSnapshot = snapshot
	t.Cleanup(func() { globalSignalKSnapshot = origSnapshot })

	read := derivedAwareAlarmReader(snapshot)

	sample := read(vesselFuelEconomyPath)
	if !sample.Present {
		t.Fatalf("expected the derived fuel-economy path to be readable by an alarm rule")
	}
	// 5.2525 m/s over a combined 1.3167e-05 m3/s is roughly 398,900 m/m3.
	if sample.Value < 390000 || sample.Value > 410000 {
		t.Fatalf("derived value = %.0f, want ~398900", sample.Value)
	}

	// Ordinary SignalK paths still resolve through the same reader.
	if sog := read("navigation.speedOverGround"); !sog.Present || sog.Value != 5.2525 {
		t.Fatalf("expected the snapshot path to still read through, got %+v", sog)
	}
}

// An undefined derived value stays absent rather than reading as zero, so a
// "below" rule cannot fire on a figure that does not exist.
func TestDerivedAwareAlarmReader_AbsentDerivedStaysAbsent(t *testing.T) {
	snapshot := snapshotWithSelfValues(map[string]any{"navigation.speedOverGround": 0})

	origSnapshot := globalSignalKSnapshot
	globalSignalKSnapshot = snapshot
	t.Cleanup(func() { globalSignalKSnapshot = origSnapshot })

	if sample := derivedAwareAlarmReader(snapshot)(vesselFuelEconomyPath); sample.Present {
		t.Fatalf("expected absence when the vessel is stopped, got %.3f", sample.Value)
	}
}

// The heavy-weather paths have to be listed and carry units, or the path
// picker cannot offer them and an operator has nothing to bind a rule to.
func TestHeavyWeatherDerivedPathsAreListedWithUnits(t *testing.T) {
	for _, path := range []string{pressureRatePath, pressureChange3hPath, squashZoneIndexPath} {
		found := false
		for _, id := range derivedPathIDs {
			if id == path {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s is not listed in derivedPathIDs", path)
		}
		if _, ok := derivedPathUnits[path]; !ok {
			t.Fatalf("%s has no unit, so the picker cannot preselect a quantity", path)
		}
	}
}

// With no barometer history there is nothing to say, and saying "0 Pa/s" would
// read as a rock-steady barometer rather than as silence.
func TestHeavyWeatherDerivedPathsAbsentWithoutHistory(t *testing.T) {
	origBarometer := barometerHistory
	origWindSpeed := trueWindSpeedHistory
	origWindDirection := trueWindDirectionHistory
	barometerHistory = newTelemetryRingBuffer(8)
	trueWindSpeedHistory = newTelemetryRingBuffer(8)
	trueWindDirectionHistory = newTelemetryRingBuffer(8)
	t.Cleanup(func() {
		barometerHistory = origBarometer
		trueWindSpeedHistory = origWindSpeed
		trueWindDirectionHistory = origWindDirection
	})

	values := derivedPathValues()
	for _, path := range []string{pressureRatePath, pressureChange3hPath} {
		if value, ok := values[path]; !ok {
			t.Fatalf("%s must appear in the payload even when absent, so the stream carries a null", path)
		} else if value != nil {
			t.Fatalf("%s should be absent with no history, got %v", path, *value)
		}
	}
}

// A three-hour fall of 3mb is 1mb/hr, the rate the seeded "barometer falling"
// rule watches for.
func TestPressureRateDerivedPathReportsFallingBarometer(t *testing.T) {
	origBarometer := barometerHistory
	barometerHistory = newTelemetryRingBuffer(4096)
	t.Cleanup(func() { barometerHistory = origBarometer })

	now := time.Now().UTC()
	for i := 0; i <= 180; i++ {
		barometerHistory.record(101500-float64(i)*300.0/180.0, now.Add(-3*time.Hour+time.Duration(i)*time.Minute))
	}

	values := derivedPathValues()
	rate := values[pressureRatePath]
	if rate == nil {
		t.Fatalf("expected a pressure rate with three hours of history")
	}
	// -300 Pa over 3 hours is -100 Pa/hr, which is -1 mb/hr.
	if math.Abs(*rate*3600-(-100)) > 2 {
		t.Fatalf("rate = %.4f Pa/s (%.1f Pa/hr), want about -100 Pa/hr", *rate, *rate*3600)
	}

	change := values[pressureChange3hPath]
	if change == nil || math.Abs(*change-(-300)) > 5 {
		t.Fatalf("expected about -300 Pa of three-hour change, got %v", change)
	}
}

/*
Ages for the derived paths (ADR 0083). A figure computed from a frozen input
must carry that input's age, or a dead source reads as a live one exactly
where staleness matters most: a derived number nobody wired an age for.
*/

// The 20-hour-old fuel rate that produced a 0.02 nm/L Economy reading while
// SOG was current: the derived value's age must be the frozen input's own
// SignalK timestamp, not the fresher pathSeen record a resubscribe replay
// (a restart or an ordinary reconnect) would otherwise reset to now.
func TestVesselFuelEconomyWithAgeReportsOldestContributingInput(t *testing.T) {
	snapshot := newSignalKSnapshot()
	now := time.Now().UTC()
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: "navigation.speedOverGround", Value: 5.0}}}},
	}, now)
	oldTimestamp := now.Add(-20 * time.Hour).Format(time.RFC3339)
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{
			Timestamp: oldTimestamp,
			Values:    []signalKValue{{Path: "propulsion.port.fuel.rate", Value: 1e-05}},
		}},
	}, now) // arrival is "now": a replay, not a fresh reading from the source
	snapshot.setSelfContext("vessels.self")

	read := snapshotAlarmReader(snapshot)
	_, age, ok := vesselFuelEconomyWithAge(snapshot, read, []string{"propulsion.port.fuel.rate"}, now)
	if !ok {
		t.Fatal("expected an economy figure")
	}
	if math.Abs(age-20*3600) > 2 {
		t.Fatalf("expected ~20h (the frozen fuel-rate input's own timestamp), got %v", age)
	}
}

// The reverse case: SOG's own timestamp is the stale one, a freshly
// timestamped engine notwithstanding.
func TestVesselFuelEconomyWithAgeCountsSOGAsAnInputToo(t *testing.T) {
	snapshot := newSignalKSnapshot()
	now := time.Now().UTC()
	oldTimestamp := now.Add(-1 * time.Hour).Format(time.RFC3339)
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{
			Timestamp: oldTimestamp,
			Values:    []signalKValue{{Path: "navigation.speedOverGround", Value: 5.0}},
		}},
	}, now)
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{
			Timestamp: now.Format(time.RFC3339),
			Values:    []signalKValue{{Path: "propulsion.port.fuel.rate", Value: 1e-05}},
		}},
	}, now)
	snapshot.setSelfContext("vessels.self")

	read := snapshotAlarmReader(snapshot)
	_, age, ok := vesselFuelEconomyWithAge(snapshot, read, []string{"propulsion.port.fuel.rate"}, now)
	if !ok {
		t.Fatal("expected an economy figure")
	}
	if math.Abs(age-3600) > 2 {
		t.Fatalf("expected ~1h (SOG's own timestamp), got %v", age)
	}
}

func TestVesselFuelEconomyWithAgeIsUnknownWhenAbsent(t *testing.T) {
	read := fakeReader(map[string]float64{"navigation.speedOverGround": 0})
	if _, age, ok := vesselFuelEconomyWithAge(newSignalKSnapshot(), read, []string{"propulsion.port.fuel.rate"}, time.Now().UTC()); ok || age != -1 {
		t.Fatalf("expected no figure and -1 age, got age=%v ok=%v", age, ok)
	}
}

func TestDerivedPathAgesUnknownWithoutBarometerHistory(t *testing.T) {
	origBarometer := barometerHistory
	barometerHistory = newTelemetryRingBuffer(8)
	t.Cleanup(func() { barometerHistory = origBarometer })

	ages := derivedPathAges(time.Now().UTC())
	if ages[pressureRatePath] != -1 {
		t.Fatalf("expected -1 with no barometer history, got %v", ages[pressureRatePath])
	}
}

// A rate computed over the full three-hour window is only as fresh as the
// newest sample in the buffer: a buffer that stopped receiving new points 30
// minutes ago makes the slope 30 minutes stale, even though every point
// behind it still carries a real timestamp from inside the window.
func TestDerivedPathAgesReportsNewestBarometerSample(t *testing.T) {
	origBarometer := barometerHistory
	barometerHistory = newTelemetryRingBuffer(4096)
	t.Cleanup(func() { barometerHistory = origBarometer })

	now := time.Now().UTC()
	for i := 0; i <= 120; i++ {
		barometerHistory.record(101500, now.Add(-150*time.Minute+time.Duration(i)*time.Minute))
	}

	ages := derivedPathAges(now)
	age := ages[pressureRatePath]
	if math.Abs(age-1800) > 5 {
		t.Fatalf("expected about 1800s (30 minutes since the buffer's last sample), got %v", age)
	}
}

// squashZoneIndex depends on three ring buffers; its age is the oldest of
// the three, not just the barometer's.
func TestDerivedPathAgesSquashZoneReportsOldestOfItsThreeInputs(t *testing.T) {
	origBarometer := barometerHistory
	origWindSpeed := trueWindSpeedHistory
	origWindDirection := trueWindDirectionHistory
	barometerHistory = newTelemetryRingBuffer(4096)
	trueWindSpeedHistory = newTelemetryRingBuffer(4096)
	trueWindDirectionHistory = newTelemetryRingBuffer(4096)
	t.Cleanup(func() {
		barometerHistory = origBarometer
		trueWindSpeedHistory = origWindSpeed
		trueWindDirectionHistory = origWindDirection
	})

	now := time.Now().UTC()
	for i := 0; i <= 120; i++ {
		ts := now.Add(-120*time.Minute + time.Duration(i)*time.Minute)
		barometerHistory.record(101500, ts)
		trueWindSpeedHistory.record(12, ts)
	}
	// Direction stopped reporting 40 minutes ago, though its last two points
	// are still inside the three-hour window.
	trueWindDirectionHistory.record(180, now.Add(-100*time.Minute))
	trueWindDirectionHistory.record(185, now.Add(-40*time.Minute))

	ages := derivedPathAges(now)
	age := ages[squashZoneIndexPath]
	if math.Abs(age-2400) > 5 {
		t.Fatalf("expected ~2400s (wind direction's stalled 40 minutes), got %v", age)
	}
}

/*
Fuel volume, time to empty and range at current burn (ADR 0084).

Fixtures captured from the live vessel 2026-09-08 (backend/testdata/fuel/),
not assumed: tanks.fuel.{2,4,5,7} carry both currentLevel and capacity --
0.69556×1.2, 0.56092×1.3, 0.7416×1.2, 0.49612×1.3, summing to about 3.0987m3
-- tanks.fuel.{0,1} carry only currentLevel, and at capture time
propulsion.{port,starboard}.fuel.rate (4.1667e-7 m3/s each) were already
about 20 hours stale from 2026-09-07T00:59:20Z while the tank levels and SOG
(0.0607 m/s) were current at 2026-09-07T21:25Z. That is the frozen-input case
derivedInputMaxAge exists for.
*/

// loadFuelFixtureMap reads a captured SignalK REST-shaped fragment.
func loadFuelFixtureMap(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "fuel", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parsing fixture %s: %v", name, err)
	}
	return m
}

// restampFixtureTimestamps deep-copies a fixture fragment and overwrites
// every "timestamp" key found anywhere in it, so a test can hold the
// fixture's own values while controlling exactly how old they read against a
// given now, rather than the fixture's captured dates ageing out from under
// the test the further today gets from 2026-09-08.
func restampFixtureTimestamps(node map[string]any, ts string) map[string]any {
	copied := deepCopyMap(node)
	var walk func(any)
	walk = func(v any) {
		m, ok := v.(map[string]any)
		if !ok {
			return
		}
		if _, has := m["timestamp"]; has {
			m["timestamp"] = ts
		}
		for _, child := range m {
			walk(child)
		}
	}
	walk(copied)
	return copied
}

// fuelFixtureSnapshot builds a self tree from the three captured fixtures, at
// their own captured timestamps.
func fuelFixtureSnapshot(t *testing.T) *signalKSnapshot {
	t.Helper()
	snapshot := newSignalKSnapshot()
	snapshot.contexts["vessels.self"] = map[string]any{
		"tanks":      map[string]any{"fuel": loadFuelFixtureMap(t, "tanks_fuel.json")},
		"propulsion": loadFuelFixtureMap(t, "propulsion.json"),
		"navigation": map[string]any{"speedOverGround": loadFuelFixtureMap(t, "sog.json")},
	}
	snapshot.setSelfContext("vessels.self")
	return snapshot
}

// restampFuelRate mutates a fixture snapshot's two engine rate timestamps in
// place, so a test can move only the burn input into or out of the
// freshness window while leaving the tank and SOG timestamps alone.
func restampFuelRate(t *testing.T, snapshot *signalKSnapshot, ts string) {
	t.Helper()
	propulsion, ok := snapshot.contexts["vessels.self"]["propulsion"].(map[string]any)
	if !ok {
		t.Fatal("fixture snapshot has no propulsion tree")
	}
	for _, engine := range []string{"port", "starboard"} {
		rate, ok := propulsion[engine].(map[string]any)["fuel"].(map[string]any)["rate"].(map[string]any)
		if !ok {
			t.Fatalf("fixture snapshot has no %s fuel rate node", engine)
		}
		rate["timestamp"] = ts
	}
}

// Volume only depends on the tank inputs, which are current at the fixture's
// own now; time to empty and range both depend on the burn rate, which is
// about 20 hours stale at that same instant, so both go absent even though a
// naive computation from vesselFuelEconomy's old behaviour would still
// produce a number.
func TestFuelVolumePresentButBurnDerivedFiguresAbsentWhenBurnIsStale(t *testing.T) {
	snapshot := fuelFixtureSnapshot(t)
	withGlobalSnapshot(t, snapshot)

	now, err := time.Parse(time.RFC3339, "2026-09-07T21:25:30Z")
	if err != nil {
		t.Fatalf("parsing now: %v", err)
	}

	values, ages := computeDerivedPaths(now)

	volume := values[fuelVolumePath]
	if volume == nil {
		t.Fatal("expected a fuel volume figure")
	}
	if math.Abs(*volume-3.0987) > 0.001 {
		t.Fatalf("expected about 3.0987 m3 (tanks 2, 4, 5, 7 only), got %v", *volume)
	}
	if age := ages[fuelVolumePath]; age < 0 || age > 60 {
		t.Fatalf("expected the volume's age to be a handful of seconds (fresh tank timestamps), got %v", age)
	}

	if values[fuelTimeToEmptyPath] != nil {
		t.Fatalf("expected time to empty absent with the burn rate ~20h stale, got %v", *values[fuelTimeToEmptyPath])
	}
	if values[fuelRangeAtCurrentBurnPath] != nil {
		t.Fatalf("expected range absent with the burn rate ~20h stale, got %v", *values[fuelRangeAtCurrentBurnPath])
	}

	// The age still reports the reason, even though the value itself is absent.
	for _, path := range []string{fuelTimeToEmptyPath, fuelRangeAtCurrentBurnPath} {
		if age := ages[path]; math.Abs(age-73570) > 30 {
			t.Fatalf("%s: expected an age of about 73570s (the frozen burn rate), got %v", path, age)
		}
	}
}

// Move the burn rate's timestamps to within the freshness window and both
// figures that depend on it become computable.
func TestFuelTimeToEmptyAndRangeComputedWhenBurnIsFresh(t *testing.T) {
	snapshot := fuelFixtureSnapshot(t)
	withGlobalSnapshot(t, snapshot)

	now, err := time.Parse(time.RFC3339, "2026-09-07T21:25:30Z")
	if err != nil {
		t.Fatalf("parsing now: %v", err)
	}
	restampFuelRate(t, snapshot, now.Add(-10*time.Second).Format(time.RFC3339))

	values, _ := computeDerivedPaths(now)

	// Volume ~3.0987 m3 over a combined burn of 2×4.1667e-7 m3/s.
	timeToEmpty := values[fuelTimeToEmptyPath]
	if timeToEmpty == nil {
		t.Fatal("expected a time-to-empty figure with the burn rate fresh")
	}
	if math.Abs(*timeToEmpty-3.7185e6) > 5000 {
		t.Fatalf("expected about 3.7185e6 s, got %v", *timeToEmpty)
	}

	// Range = volume × (SOG / total burn).
	rangeM := values[fuelRangeAtCurrentBurnPath]
	if rangeM == nil {
		t.Fatal("expected a range figure with the burn rate fresh")
	}
	if math.Abs(*rangeM-2.257e5) > 2000 {
		t.Fatalf("expected about 2.257e5 m, got %v", *rangeM)
	}
}

// Range needs speed as well as burn; time to empty does not. Stopped with
// the engines idling in gear (a real liveaboard case at the dock) must blank
// range without blanking time to empty for a reason that has nothing to do
// with it.
func TestFuelRangeAbsentWhenStoppedEvenWithFreshBurnAndVolume(t *testing.T) {
	snapshot := fuelFixtureSnapshot(t)
	withGlobalSnapshot(t, snapshot)

	now, err := time.Parse(time.RFC3339, "2026-09-07T21:25:30Z")
	if err != nil {
		t.Fatalf("parsing now: %v", err)
	}
	fresh := now.Add(-10 * time.Second).Format(time.RFC3339)
	restampFuelRate(t, snapshot, fresh)

	sog, ok := snapshot.contexts["vessels.self"]["navigation"].(map[string]any)["speedOverGround"].(map[string]any)
	if !ok {
		t.Fatal("fixture snapshot has no speedOverGround node")
	}
	sog["value"] = 0.0
	sog["timestamp"] = fresh

	values, _ := computeDerivedPaths(now)

	if values[fuelTimeToEmptyPath] == nil {
		t.Fatal("expected time to empty to stay computable stopped: it does not depend on speed")
	}
	if values[fuelRangeAtCurrentBurnPath] != nil {
		t.Fatalf("expected range absent when stopped, got %v", *values[fuelRangeAtCurrentBurnPath])
	}
}

// tanks.fuel.0 and .1 on the reference vessel carry a level with no known
// capacity. With only those two present, nothing can be converted to a
// volume, so volume, time to empty and range are all absent.
func TestFuelVolumeAbsentWhenNoTankPublishesCapacity(t *testing.T) {
	now := time.Now().UTC()
	snapshot := newSignalKSnapshot()
	snapshot.contexts["vessels.self"] = map[string]any{
		"tanks": map[string]any{
			"fuel": map[string]any{
				"0": map[string]any{"currentLevel": map[string]any{"value": 0.0, "timestamp": now.Format(time.RFC3339)}},
				"1": map[string]any{"currentLevel": map[string]any{"value": 0.0, "timestamp": now.Format(time.RFC3339)}},
			},
		},
	}
	snapshot.setSelfContext("vessels.self")
	withGlobalSnapshot(t, snapshot)

	values, ages := computeDerivedPaths(now)
	if values[fuelVolumePath] != nil {
		t.Fatalf("expected no volume with no tank publishing a capacity, got %v", *values[fuelVolumePath])
	}
	if age := ages[fuelVolumePath]; age != -1 {
		t.Fatalf("expected -1 age with no contributing tank, got %v", age)
	}
	if values[fuelTimeToEmptyPath] != nil || values[fuelRangeAtCurrentBurnPath] != nil {
		t.Fatal("time to empty and range both need a volume; expected both absent")
	}
}

// buildTanksStatePayload has to expose the same computeDerivedPaths pass
// under the field names the Tanks tile footer reads, alongside the
// REST-fetched per-tank list, which stays independent of this (it fails over
// to backend-fallback in this test environment since SETTINGS_FILE points at
// nothing reachable).
func TestTanksStatePayloadCarriesFuelDerivedFields(t *testing.T) {
	t.Setenv("SETTINGS_FILE", filepath.Join(t.TempDir(), "settings.yaml"))

	now := time.Now().UTC()
	fresh := now.Add(-5 * time.Second).Format(time.RFC3339)

	snapshot := newSignalKSnapshot()
	snapshot.contexts["vessels.self"] = map[string]any{
		"tanks":      map[string]any{"fuel": restampFixtureTimestamps(loadFuelFixtureMap(t, "tanks_fuel.json"), fresh)},
		"propulsion": restampFixtureTimestamps(loadFuelFixtureMap(t, "propulsion.json"), fresh),
		"navigation": map[string]any{"speedOverGround": restampFixtureTimestamps(loadFuelFixtureMap(t, "sog.json"), fresh)},
	}
	snapshot.setSelfContext("vessels.self")
	withGlobalSnapshot(t, snapshot)

	payload := buildTanksStatePayload()

	volume, ok := payload["fuel_volume_m3"].(*float64)
	if !ok || volume == nil {
		t.Fatalf("expected fuel_volume_m3 to carry a number, got %#v", payload["fuel_volume_m3"])
	}
	if math.Abs(*volume-3.0987) > 0.001 {
		t.Fatalf("expected about 3.0987 m3, got %v", *volume)
	}

	if _, ok := payload["fuel_volume_age_s"].(float64); !ok {
		t.Fatalf("expected fuel_volume_age_s to be a number, got %#v", payload["fuel_volume_age_s"])
	}

	timeToEmpty, ok := payload["fuel_time_to_empty_s"].(*float64)
	if !ok || timeToEmpty == nil {
		t.Fatalf("expected fuel_time_to_empty_s to carry a number, got %#v", payload["fuel_time_to_empty_s"])
	}

	rangeM, ok := payload["fuel_range_m"].(*float64)
	if !ok || rangeM == nil {
		t.Fatalf("expected fuel_range_m to carry a number, got %#v", payload["fuel_range_m"])
	}

	derivedAge, ok := payload["fuel_derived_age_s"].(float64)
	if !ok || derivedAge < 0 || derivedAge > 60 {
		t.Fatalf("expected fuel_derived_age_s to be a small known age, got %#v", payload["fuel_derived_age_s"])
	}
}

/*
Forecast wind and surf warning derived paths (plan: fold the forecast
warning into the alarm system; ADR 0087). These read globalForecastWarningsSlot
(forecast_warnings_fetcher.go), the background fetcher's own state, rather
than the SignalK snapshot - so every test here manages the slot directly and
clears it afterwards.
*/

func withCleanForecastWarningsSlot(t *testing.T) {
	t.Helper()
	globalForecastWarningsSlot.clear()
	t.Cleanup(func() { globalForecastWarningsSlot.clear() })
}

func TestForecastWarningPathsAbsentWhenNeverFetched(t *testing.T) {
	withCleanForecastWarningsSlot(t)

	values, ages := computeDerivedPaths(time.Now().UTC())
	if values[forecastWindWarningLevelPath] != nil {
		t.Fatalf("expected no wind-warning value before any fetch, got %v", *values[forecastWindWarningLevelPath])
	}
	if values[forecastSurfWarningPath] != nil {
		t.Fatalf("expected no surf-warning value before any fetch, got %v", *values[forecastSurfWarningPath])
	}
	if ages[forecastWindWarningLevelPath] != -1 || ages[forecastSurfWarningPath] != -1 {
		t.Fatalf("expected -1 age before any fetch, got wind=%v surf=%v", ages[forecastWindWarningLevelPath], ages[forecastSurfWarningPath])
	}
}

func TestForecastWarningPathsPublishAFiveMinuteOldReading(t *testing.T) {
	withCleanForecastWarningsSlot(t)

	now := time.Now().UTC()
	globalForecastWarningsSlot.set(forecastWarningsReading{WindLevel: 2, Surf: true, FetchedAt: now.Add(-5 * time.Minute)})

	values, ages := computeDerivedPaths(now)
	if values[forecastWindWarningLevelPath] == nil || *values[forecastWindWarningLevelPath] != 2 {
		t.Fatalf("expected wind level 2, got %v", values[forecastWindWarningLevelPath])
	}
	if values[forecastSurfWarningPath] == nil || *values[forecastSurfWarningPath] != 1 {
		t.Fatalf("expected surf 1, got %v", values[forecastSurfWarningPath])
	}
	if age := ages[forecastWindWarningLevelPath]; math.Abs(age-300) > 2 {
		t.Fatalf("expected a wind-warning age of about 300s, got %v", age)
	}
	if age := ages[forecastSurfWarningPath]; math.Abs(age-300) > 2 {
		t.Fatalf("expected a surf-warning age of about 300s, got %v", age)
	}
}

// Past forecastWarningsMaxAge (30 minutes, three missed fetch intervals) the
// values go absent, but the age is still reported - the same
// freshEnoughToPublish shape the fuel paths use - so a staleness rule has
// something concrete to watch climb rather than the path vanishing with no
// trace of why.
func TestForecastWarningPathsAbsentPastMaxAgeButAgeStillReported(t *testing.T) {
	withCleanForecastWarningsSlot(t)

	now := time.Now().UTC()
	globalForecastWarningsSlot.set(forecastWarningsReading{WindLevel: 2, Surf: true, FetchedAt: now.Add(-40 * time.Minute)})

	values, ages := computeDerivedPaths(now)
	if values[forecastWindWarningLevelPath] != nil {
		t.Fatalf("expected the wind-warning value absent past max age, got %v", *values[forecastWindWarningLevelPath])
	}
	if values[forecastSurfWarningPath] != nil {
		t.Fatalf("expected the surf-warning value absent past max age, got %v", *values[forecastSurfWarningPath])
	}
	if age := ages[forecastWindWarningLevelPath]; math.Abs(age-2400) > 2 {
		t.Fatalf("expected the age still reported (~2400s), got %v", age)
	}
	if age := ages[forecastSurfWarningPath]; math.Abs(age-2400) > 2 {
		t.Fatalf("expected the age still reported (~2400s), got %v", age)
	}
}

func TestForecastWarningPathsListedWithUnits(t *testing.T) {
	for _, path := range []string{forecastWindWarningLevelPath, forecastSurfWarningPath} {
		found := false
		for _, id := range derivedPathIDs {
			if id == path {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s is not listed in derivedPathIDs", path)
		}
		if unit, ok := derivedPathUnits[path]; !ok || unit != "" {
			t.Fatalf("%s should declare an explicit empty unit, got %q ok=%v", path, unit, ok)
		}
	}
}

// A staleness rule bound to a derived path has to measure that path's own
// input going quiet, not the unrelated fact that the SignalK stream itself
// is still talking (this is what changed in derivedAwareAlarmReader). A
// five-minute-old slot must read as stale against a 240s threshold and fresh
// against a 600s one.
func TestDerivedAwareAlarmReader_LastSeenIsThePathsOwnAge(t *testing.T) {
	withCleanForecastWarningsSlot(t)

	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")
	withGlobalSnapshot(t, snapshot)

	now := time.Now().UTC()
	globalForecastWarningsSlot.set(forecastWarningsReading{WindLevel: 2, FetchedAt: now.Add(-5 * time.Minute)})

	sample := derivedAwareAlarmReader(snapshot)(forecastWindWarningLevelPath)
	if !sample.Present {
		t.Fatal("expected the wind-warning path to be present")
	}

	shortRule := alarmRule{Op: alarmOpStale, StaleAfterSeconds: 240}
	if !alarmSampleStale(shortRule, sample, now) {
		t.Fatalf("expected a 5-minute-old sample to be stale against a 240s threshold, LastSeen=%v now=%v", sample.LastSeen, now)
	}

	longRule := alarmRule{Op: alarmOpStale, StaleAfterSeconds: 600}
	if alarmSampleStale(longRule, sample, now) {
		t.Fatalf("expected a 5-minute-old sample to read fresh against a 600s threshold, LastSeen=%v now=%v", sample.LastSeen, now)
	}
}
