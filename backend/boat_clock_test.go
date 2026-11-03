package main

import (
	"testing"
	"time"
)

func TestBoatClockFormat(t *testing.T) {
	brisbane, err := time.LoadLocation("Australia/Brisbane")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"afternoon", time.Date(2026, 11, 2, 3, 30, 0, 0, time.UTC), "1.30 PM"},
		{"evening on the hour", time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC), "10.00 PM"},
		{"midnight", time.Date(2026, 11, 1, 14, 0, 0, 0, time.UTC), "12.00 AM"},
		{"noon", time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC), "12.00 PM"},
		{"morning with a zero minute digit", time.Date(2026, 11, 1, 23, 5, 0, 0, time.UTC), "9.05 AM"},
		{"just after midnight", time.Date(2026, 11, 1, 14, 5, 0, 0, time.UTC), "12.05 AM"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := boatClock(c.at, brisbane); got != c.want {
				t.Fatalf("boatClock = %q, want %q", got, c.want)
			}
		})
	}
}

func TestBoatDayClockAndZone(t *testing.T) {
	brisbane, _ := time.LoadLocation("Australia/Brisbane")
	at := time.Date(2026, 11, 2, 3, 30, 0, 0, time.UTC)
	if got, want := boatDayClock(at, brisbane), "Mon 2 Nov 1.30 PM"; got != want {
		t.Fatalf("boatDayClock = %q, want %q", got, want)
	}
	if got, want := boatDayClockWithZone(at, brisbane), "Mon 2 Nov 1.30 PM AEST"; got != want {
		t.Fatalf("boatDayClockWithZone = %q, want %q", got, want)
	}
}

func TestBoatTimeDayClockSaysUTCWhenNoFixEverSeen(t *testing.T) {
	brisbane, _ := time.LoadLocation("Australia/Brisbane")
	at := time.Date(2026, 11, 2, 3, 30, 0, 0, time.UTC)
	live := boatTime{placeZone: placeZone{Loc: brisbane, Name: "Australia/Brisbane"}}
	if got, want := live.DayClock(at), "Mon 2 Nov 1.30 PM"; got != want {
		t.Errorf("a boat with a zone: %q, want %q", got, want)
	}
	lastFix := boatTime{placeZone: live.placeZone, LastFix: true}
	if got, want := lastFix.DayClock(at), "Mon 2 Nov 1.30 PM"; got != want {
		t.Errorf("a boat on its last fix's zone: %q, want %q", got, want)
	}
	none := boatTime{placeZone: utcZone, NoFix: true}
	if got, want := none.DayClock(at), "Mon 2 Nov 3.30 AM UTC"; got != want {
		t.Errorf("a boat with no fix: %q, want %q", got, want)
	}
}

func TestBoatDayYearClockNamesAYearOnlyOutsideTheCurrentOne(t *testing.T) {
	sydney, _ := time.LoadLocation("Australia/Sydney")
	at := time.Date(2024, 9, 13, 23, 0, 0, 0, time.UTC)
	if got, want := boatDayYearClock(at, sydney, 2026), "Sat 14 Sep 2024 9.00 AM"; got != want {
		t.Errorf("other year: %q, want %q", got, want)
	}
	if got, want := boatDayYearClock(at, sydney, 2024), "Sat 14 Sep 9.00 AM"; got != want {
		t.Errorf("this year: %q, want %q", got, want)
	}
}

func TestBoatClockAcrossDaylightSaving(t *testing.T) {
	sydney, _ := time.LoadLocation("Australia/Sydney")
	// 2026-10-04 is the first Sunday of October: clocks go from 2.00 AM AEST to 3.00 AM AEDT.
	before := time.Date(2026, 10, 3, 15, 59, 0, 0, time.UTC) // 1.59 AM AEST
	after := time.Date(2026, 10, 3, 16, 0, 0, 0, time.UTC)   // 3.00 AM AEDT
	if got, want := boatDayClockWithZone(before, sydney), "Sun 4 Oct 1.59 AM AEST"; got != want {
		t.Fatalf("before = %q, want %q", got, want)
	}
	if got, want := boatDayClockWithZone(after, sydney), "Sun 4 Oct 3.00 AM AEDT"; got != want {
		t.Fatalf("after = %q, want %q", got, want)
	}
}

func TestBoatClockOffshoreZoneNamedByOffset(t *testing.T) {
	loc := time.FixedZone("+12", 12*3600)
	at := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	if got, want := boatDayClockWithZone(at, loc), "Mon 2 Nov 12.00 PM UTC+12"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBoatAxisClock(t *testing.T) {
	brisbane, _ := time.LoadLocation("Australia/Brisbane")
	on := time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)
	if got, want := boatAxisClock(on, brisbane), "1 PM"; got != want {
		t.Fatalf("on the hour = %q, want %q", got, want)
	}
	if got, want := boatAxisClock(on.Add(30*time.Minute), brisbane), "1.30 PM"; got != want {
		t.Fatalf("off the hour = %q, want %q", got, want)
	}
	if got, want := boatAxisClock(time.Date(2026, 11, 1, 14, 0, 0, 0, time.UTC), brisbane), "12 AM"; got != want {
		t.Fatalf("midnight = %q, want %q", got, want)
	}
}

func TestChartTimeLabel_AxisIsShort(t *testing.T) {
	brisbane, _ := time.LoadLocation("Australia/Brisbane")
	on := time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)
	if got, want := chartTimeLabel(on, 6*time.Hour, brisbane), "1 PM"; got != want {
		t.Fatalf("on the hour = %q, want %q", got, want)
	}
	if got, want := chartTimeLabel(on.Add(30*time.Minute), 6*time.Hour, brisbane), "1.30 PM"; got != want {
		t.Fatalf("off the hour = %q, want %q", got, want)
	}
	if got, want := chartTimeLabel(on, 72*time.Hour, brisbane), "Mon 1 PM"; got != want {
		t.Fatalf("multi-day = %q, want %q", got, want)
	}
}
