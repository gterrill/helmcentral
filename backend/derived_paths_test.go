package main

import (
	"math"
	"testing"
	"time"
)

// snapshotWithSelfValues builds a snapshot carrying several self paths at
// once, for the cases that need a whole derivation's inputs present.
func snapshotWithSelfValues(values map[string]any) *signalKSnapshot {
	snapshot := newSignalKSnapshot()
	entries := make([]signalKValue, 0, len(values))
	for path, value := range values {
		entries = append(entries, signalKValue{Path: path, Value: value})
	}
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: entries}},
	}, alarmNow)
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
