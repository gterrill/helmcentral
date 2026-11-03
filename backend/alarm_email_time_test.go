package main

import (
	"testing"
	"time"
)

// An alarm email reads on the boat's clock in the form the app uses everywhere,
// not as an RFC3339 UTC stamp.
func TestAlarmEmailTimeIsBoatTime(t *testing.T) {
	useFreshBoatZone(t)
	// A fix counts only while the GNSS gate trusts it; another test's critical
	// gate would turn this position into "no fix".
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)
	orig := globalSignalKSnapshot
	globalSignalKSnapshot = snapshotWithSelfValues(map[string]any{
		"navigation.position": map[string]any{"latitude": -27.5, "longitude": 153.0},
	})
	t.Cleanup(func() { globalSignalKSnapshot = orig })

	msg := notificationMessage{At: time.Date(2026, 11, 2, 3, 30, 0, 0, time.UTC)}
	for _, f := range alarmEmailFields(msg) {
		if f[0] == "Time" {
			if want := "Mon 2 Nov 1.30 PM AEST"; f[1] != want {
				t.Fatalf("Time = %q, want %q", f[1], want)
			}
			return
		}
	}
	t.Fatal("no Time field")
}

func TestAlarmEmailTimeWithNoFixSaysUTC(t *testing.T) {
	useFreshBoatZone(t)
	orig := globalSignalKSnapshot
	globalSignalKSnapshot = snapshotWithSelfValues(map[string]any{})
	t.Cleanup(func() { globalSignalKSnapshot = orig })

	msg := notificationMessage{At: time.Date(2026, 11, 2, 3, 30, 0, 0, time.UTC)}
	for _, f := range alarmEmailFields(msg) {
		if f[0] == "Time" {
			if want := "Mon 2 Nov 3.30 AM UTC"; f[1] != want {
				t.Fatalf("Time = %q, want %q", f[1], want)
			}
			return
		}
	}
	t.Fatal("no Time field")
}
