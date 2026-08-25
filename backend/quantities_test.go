package main

import (
	"math"
	"testing"
)

// The values here are deliberately the same ones frontend/src/test/quantities.test.ts
// asserts on. The two unit tables cannot drift without one of these failing.
func TestConvertToSI(t *testing.T) {
	cases := []struct {
		name     string
		quantity string
		unit     string
		display  float64
		si       float64
	}{
		{"psi to pascals", "pressure", "psi", 35.0, 241316.495},
		{"kPa to pascals", "pressure", "kPa", 241.325, 241325},
		{"bar to pascals", "pressure", "bar", 2.41325, 241325},
		{"pascals are already SI", "pressure", "Pa", 241325, 241325},
		{"celsius to kelvin", "temperature", "C", 25.0, 298.15},
		{"fahrenheit to kelvin", "temperature", "F", 77.0, 298.15},
		{"kelvin is already SI", "temperature", "K", 298.15, 298.15},
		{"litres per hour to cubic metres per second", "volumetricFlow", "Lph", 20.0, 20.0 / 1000 / 3600},
		{"hours to seconds", "duration", "h", 1204, 1204 * 3600},
		{"minutes to seconds", "duration", "min", 90, 5400},
		{"percent to ratio", "ratio", "percent", 75, 0.75},
		{"litres to cubic metres", "volume", "L", 1000, 1.0},
		{"knots to metres per second", "speed", "kn", 1.943844, 1.0},
		{"feet to metres", "length", "ft", 3.28084, 1.0},
		{"rpm to hertz", "frequency", "rpm", 1800, 30},
		{"hertz is already SI", "frequency", "Hz", 30, 30},
		{"unitless passes through", "raw", "raw", 42, 42},
	}

	for _, tc := range cases {
		got, err := convertToSI(tc.display, tc.quantity, tc.unit)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
		if math.Abs(got-tc.si) > math.Abs(tc.si)*1e-6+1e-9 {
			t.Fatalf("%s: expected %v, got %v", tc.name, tc.si, got)
		}
	}
}

// An unknown quantity or unit must be an error rather than a silent
// pass-through: a zone threshold left in psi but compared against pascals
// produces an alarm that never fires, and nothing says why.
func TestConvertToSIRejectsUnknownUnits(t *testing.T) {
	if _, err := convertToSI(1, "pressure", "furlongs"); err == nil {
		t.Fatal("expected an unknown unit to be rejected")
	}
	if _, err := convertToSI(1, "spiciness", "scoville"); err == nil {
		t.Fatal("expected an unknown quantity to be rejected")
	}
}

// Round-tripping catches an inverse that was written backwards.
func TestConvertToSIRoundTripsFromSI(t *testing.T) {
	for _, q := range siQuantities {
		for _, u := range q.Units {
			si, err := convertToSI(u.FromSI(1234.5), q.ID, u.ID)
			if err != nil {
				t.Fatalf("%s/%s: unexpected error: %v", q.ID, u.ID, err)
			}
			if math.Abs(si-1234.5) > 1e-6 {
				t.Fatalf("%s/%s: round trip gave %v, want 1234.5", q.ID, u.ID, si)
			}
		}
	}
}

// SignalK publishes electrical readings in volts, amps and watts. Without
// these a generator's output renders as a bare number with no unit.
func TestConvertToSIElectricalQuantities(t *testing.T) {
	cases := []struct {
		name     string
		quantity string
		unit     string
		display  float64
		si       float64
	}{
		{"kilowatts to watts", "power", "kW", 13.5, 13500},
		{"watts are already SI", "power", "W", 7564, 7564},
		{"volts are already SI", "potential", "V", 232, 232},
		{"amps are already SI", "current", "A", 39, 39},
	}

	for _, tc := range cases {
		got, err := convertToSI(tc.display, tc.quantity, tc.unit)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
		if math.Abs(got-tc.si) > math.Abs(tc.si)*1e-9+1e-9 {
			t.Fatalf("%s: expected %v, got %v", tc.name, tc.si, got)
		}
	}
}

/*
SignalK publishes propulsion.<id>.fuel.economy in metres per cubic metre.

Verified against a live vessel rather than read off a spec: at 10.21 kn with
that engine burning 23.9 L/h, SOG over burn gives 0.4272 nm/L and the published
792950.7 converts to 0.4281 — a 0.2% match, which settles both the unit and
that the figure is per-engine, not per-vessel.
*/
func TestConvertToSIFuelEconomy(t *testing.T) {
	cases := []struct {
		name    string
		unit    string
		display float64
		si      float64
	}{
		{"nautical miles per litre", "nmpl", 0.4281, 792841.2},
		{"nautical miles per US gallon", "nmpg", 1.6205, 792824.0},
		{"metres per cubic metre are already SI", "m/m3", 792950.7, 792950.7},
	}

	for _, tc := range cases {
		got, err := convertToSI(tc.display, "fuelEconomy", tc.unit)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
		if math.Abs(got-tc.si) > 200 {
			t.Fatalf("%s: expected about %v, got %v", tc.name, tc.si, got)
		}
	}
}
