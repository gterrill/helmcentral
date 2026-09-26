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
				{OldReading: 5000, NewReading: 0, CreatedAt: mustParseDate(t, "2026-01-01")},
			},
			want: 5000,
		},
		{
			name: "only the MOST RECENT reset applies, not a sum of all of them",
			resets: []hourMeterReset{
				{OldReading: 5000, NewReading: 0, CreatedAt: mustParseDate(t, "2026-01-01")},
				{OldReading: 5200, NewReading: 10, CreatedAt: mustParseDate(t, "2026-03-01")},
			},
			want: 5190, // the second reset's own old_reading already carries the first reset's history
		},
		{
			name: "resets given out of order still pick the latest by time, not by slice position",
			resets: []hourMeterReset{
				{OldReading: 5200, NewReading: 10, CreatedAt: mustParseDate(t, "2026-03-01")},
				{OldReading: 5000, NewReading: 0, CreatedAt: mustParseDate(t, "2026-01-01")},
			},
			want: 5190,
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

	t.Run("stale value is unknown, never guessed at", func(t *testing.T) {
		snapshot := newSignalKSnapshot()
		staleTime := now.Add(-maintenanceHoursStaleAfter - time.Minute)
		reader := func(path string) alarmSample {
			return alarmSample{Present: true, Value: 3600 * 100, LastSeen: staleTime}
		}
		got := currentEquipmentHours(reader, snapshot, "propulsion.main.runTime", 0, now)
		if got.Known {
			t.Fatalf("expected Known=false for a stale sample, got %+v", got)
		}
	})
}
