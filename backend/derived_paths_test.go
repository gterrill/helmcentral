package main

import (
	"math"
	"testing"
)

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
