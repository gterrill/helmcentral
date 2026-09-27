package main

import (
	"testing"
	"time"
)

func TestLatestMeterOffsetHours(t *testing.T) {
	cases := []struct {
		name   string
		resets []hourMeterReset
		want   float64
	}{
		{name: "no resets at all is zero offset", resets: nil, want: 0},
		{
			name: "one reset: old minus new",
			resets: []hourMeterReset{
				{ID: "a", OldReading: 5000, NewReading: 0, ChangedAt: "2026-01-01"},
			},
			want: 5000,
		},
		{
			name: "only the MOST RECENT reset (by changed_at, the operator's own date) applies, not a sum of all of them",
			resets: []hourMeterReset{
				{ID: "a", OldReading: 5000, NewReading: 0, ChangedAt: "2026-01-01"},
				{ID: "b", OldReading: 5200, NewReading: 10, ChangedAt: "2026-03-01"},
			},
			want: 5190, // the second reset's own old_reading already carries the first reset's history
		},
		{
			name: "resets given out of order still pick the latest by changed_at, not by slice position",
			resets: []hourMeterReset{
				{ID: "b", OldReading: 5200, NewReading: 10, ChangedAt: "2026-03-01"},
				{ID: "a", OldReading: 5000, NewReading: 0, ChangedAt: "2026-01-01"},
			},
			want: 5190,
		},
		{
			// The whole point of the fix: a reset recorded (CreatedAt) well
			// after another one, but whose own ChangedAt (the date the
			// replacement actually happened) is EARLIER, must not win just
			// because it was entered into Helmcentral later. Only ChangedAt
			// decides "most recent" - CreatedAt plays no part at all.
			name: "a back-filled reset entered later but dated earlier does not win",
			resets: []hourMeterReset{
				{ID: "a", OldReading: 5000, NewReading: 0, ChangedAt: "2026-01-01", CreatedAt: mustParseDate(t, "2026-01-01")},
				// Entered into the app on 2026-04-01 (CreatedAt), but the
				// operator says it actually happened back on 2025-06-01
				// (ChangedAt) - a back-filled history entry.
				{ID: "b", OldReading: 4000, NewReading: 0, ChangedAt: "2025-06-01", CreatedAt: mustParseDate(t, "2026-04-01")},
			},
			want: 5000, // reset "a" (2026-01-01) is still the most recent by date
		},
		{
			name: "two resets on the same date tie-break on id, deterministically",
			resets: []hourMeterReset{
				{ID: "z-later-id", OldReading: 100, NewReading: 0, ChangedAt: "2026-01-01"},
				{ID: "a-earlier-id", OldReading: 200, NewReading: 0, ChangedAt: "2026-01-01"},
			},
			want: 100, // "z-later-id" > "a-earlier-id" lexicographically
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := latestMeterOffsetHours(tc.resets)
			if got != tc.want {
				t.Errorf("latestMeterOffsetHours() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCurrentEquipmentHours(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	t.Run("no path bound at all is unknown", func(t *testing.T) {
		reader := func(path string) alarmSample {
			t.Fatalf("reader should not be called with no path")
			return alarmSample{}
		}
		got := currentEquipmentHours(reader, nil, "", 0, now)
		if got.Known {
			t.Fatalf("expected Known=false with no path, got %+v", got)
		}
	})

	t.Run("path bound with no value at all is unknown", func(t *testing.T) {
		reader := func(path string) alarmSample { return alarmSample{Present: false} }
		got := currentEquipmentHours(reader, globalSignalKSnapshot, "propulsion.main.runTime", 0, now)
		if got.Known {
			t.Fatalf("expected Known=false with no sample, got %+v", got)
		}
	})

	t.Run("fresh value converts seconds to hours and applies the offset", func(t *testing.T) {
		snapshot := newSignalKSnapshot()
		reader := func(path string) alarmSample {
			return alarmSample{Present: true, Value: 3600 * 100, LastSeen: now}
		}
		got := currentEquipmentHours(reader, snapshot, "propulsion.main.runTime", 25, now)
		if !got.Known {
			t.Fatalf("expected Known=true, got %+v", got)
		}
		if got.Hours != 125 {
			t.Errorf("Hours = %v, want 125 (100 raw + 25 offset)", got.Hours)
		}
	})

	t.Run("stale value is unknown, never guessed at, and reports when it was last good", func(t *testing.T) {
		snapshot := newSignalKSnapshot()
		staleTime := now.Add(-maintenanceHoursStaleAfter - time.Minute)
		reader := func(path string) alarmSample {
			return alarmSample{Present: true, Value: 3600 * 100, LastSeen: staleTime}
		}
		got := currentEquipmentHours(reader, snapshot, "propulsion.main.runTime", 0, now)
		if got.Known {
			t.Fatalf("expected Known=false for a stale sample, got %+v", got)
		}
		if got.StaleSince == nil || !got.StaleSince.Equal(staleTime) {
			t.Fatalf("expected StaleSince = %v, got %v", staleTime, got.StaleSince)
		}
	})

	t.Run("no path bound reports no StaleSince at all", func(t *testing.T) {
		reader := func(path string) alarmSample {
			t.Fatalf("reader should not be called with no path")
			return alarmSample{}
		}
		got := currentEquipmentHours(reader, nil, "", 0, now)
		if got.StaleSince != nil {
			t.Fatalf("expected no StaleSince with no path bound, got %v", got.StaleSince)
		}
	})
}
