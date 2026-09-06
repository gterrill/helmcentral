package main

import "testing"

// These vectors are the same ones the frontend's alarm-display.spec would
// assert (frontend/src/lib/alarm-display.ts, formatAlarmReading), so a
// sailor gets the same sentence whether they read the dashboard card or an
// ntfy push. See alarm_units.go for why the two tables must move together.
func TestFormatAlarmReading(t *testing.T) {
	cases := []struct {
		value float64
		unit  string
		want  string
	}{
		{-0.03, "Pa/s", "-1.1 mb/hr"},
		{100, "Pa", "1.0 mb"},
		{300.15, "K", "27.0 °C"},
		{5.144, "m/s", "10.0 kts"},
		{0.5, "ratio", "50 %"},
		{50, "Hz", "3000 RPM"},
		{12.34, "V", "12.3 V"},
		{-150, "", "-150"},
		{-0.027777, "", "-0.03"},
		{-0.03, "furlongs", "-0.03"},
	}

	for _, tc := range cases {
		if got := formatAlarmReading(tc.value, tc.unit); got != tc.want {
			t.Fatalf("formatAlarmReading(%v, %q): got %q, want %q", tc.value, tc.unit, got, tc.want)
		}
	}
}

// operatorUnit is the lookup formatAlarmReading is built on; alarm_notify.go
// will need it directly nowhere, but the table itself is worth pinning on
// its own so a future edit to one entry can't silently drop another.
func TestOperatorUnitKnownAndUnknown(t *testing.T) {
	label, convert, decimals, ok := operatorUnit("Pa/s")
	if !ok {
		t.Fatalf("expected Pa/s to be a known unit")
	}
	if label != "mb/hr" {
		t.Fatalf("label: got %q, want mb/hr", label)
	}
	if decimals != 1 {
		t.Fatalf("decimals: got %d, want 1", decimals)
	}
	if got := convert(1); got != 36 {
		t.Fatalf("convert(1): got %v, want 36", got)
	}

	if _, _, _, ok := operatorUnit("furlongs"); ok {
		t.Fatalf("expected an unrecognised unit to report ok=false")
	}
	if _, _, _, ok := operatorUnit(""); ok {
		t.Fatalf("expected an empty unit to report ok=false")
	}
}
