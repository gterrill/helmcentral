package main

import (
	"testing"
	"time"
)

// useBoatDay fixes the boat's calendar day (YYYY-MM-DD) for one test.
func useBoatDay(t *testing.T, day string) {
	t.Helper()
	d, err := time.Parse("2006-01-02", day)
	if err != nil {
		t.Fatal(err)
	}
	prev := boatToday
	t.Cleanup(func() { boatToday = prev })
	boatToday = func() (time.Time, error) { return d, nil }
}

// useNoBoatDay leaves the boat's day unknown for one test, as before any fix.
func useNoBoatDay(t *testing.T) {
	t.Helper()
	prev := boatToday
	t.Cleanup(func() { boatToday = prev })
	boatToday = func() (time.Time, error) { return time.Time{}, errBoatDayUnknown }
}
