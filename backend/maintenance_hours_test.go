package main

import (
	"testing"
	"time"
)

// TestOffsetInForceAt replaces the old TestLatestMeterOffsetHours (code
// review, cleanup item): offsetInForceAt trusts its input to already be
// ChangedAt-descending, most-recent-first, id-descending on a tie - exactly
// the order ListHourMeterResets' own `ORDER BY changed_at DESC, id DESC`
// returns (its own doc comment) - so every case below gives resets in that
// order, same as every real caller already does, and offsetInForceAt does
// no sorting or pairwise comparison of its own. The "resets given out of
// order" guarantee the old function made is deliberately dropped along with
// it: re-sorting a list the store already ordered was the duplication this
// cleanup removes.
func TestOffsetInForceAt(t *testing.T) {
	cases := []struct {
		name   string
		resets []hourMeterReset
		atDate time.Time
		want   float64
	}{
		{name: "no resets at all is zero offset", resets: nil, atDate: mustParseDate(t, "2026-06-01"), want: 0},
		{
			name: "one reset before atDate: old minus new",
			resets: []hourMeterReset{
				{ID: "a", OldReading: 5000, NewReading: 0, ChangedAt: "2026-01-01"},
			},
			atDate: mustParseDate(t, "2026-06-01"),
			want:   5000,
		},
		{
			name: "only the reset in force at atDate applies, not a sum of all of them",
			resets: []hourMeterReset{
				{ID: "b", OldReading: 5200, NewReading: 10, ChangedAt: "2026-03-01"},
				{ID: "a", OldReading: 5000, NewReading: 0, ChangedAt: "2026-01-01"},
			},
			atDate: mustParseDate(t, "2026-06-01"),
			want:   5190, // the second reset's own old_reading already carries the first reset's history
		},
		{
			// The whole point of the ORIGINAL fix this test replaces: a
			// reset recorded (CreatedAt) well after another one, but whose
			// own ChangedAt (the date the replacement actually happened) is
			// EARLIER, must not win just because it was entered into
			// Helmcentral later. Only ChangedAt decides order - CreatedAt
			// plays no part - and ListHourMeterResets already returns rows
			// in that order.
			name: "a back-filled reset entered later but dated earlier does not win",
			resets: []hourMeterReset{
				{ID: "a", OldReading: 5000, NewReading: 0, ChangedAt: "2026-01-01", CreatedAt: mustParseDate(t, "2026-01-01")},
				// Entered into the app on 2026-04-01 (CreatedAt), but the
				// operator says it actually happened back on 2025-06-01
				// (ChangedAt) - a back-filled history entry.
				{ID: "b", OldReading: 4000, NewReading: 0, ChangedAt: "2025-06-01", CreatedAt: mustParseDate(t, "2026-04-01")},
			},
			atDate: mustParseDate(t, "2026-06-01"),
			want:   5000, // reset "a" (2026-01-01) is still the most recent by date
		},
		{
			name: "two resets on the same date: the given order's first entry wins (the store's own id-descending tie-break)",
			resets: []hourMeterReset{
				{ID: "z-later-id", OldReading: 100, NewReading: 0, ChangedAt: "2026-01-01"},
				{ID: "a-earlier-id", OldReading: 200, NewReading: 0, ChangedAt: "2026-01-01"},
			},
			atDate: mustParseDate(t, "2026-06-01"),
			want:   100,
		},
		{
			// This is what offsetInForceAt exists for (code-review finding
			// 4): a completion or reading dated before the most recent
			// reset must use the offset that was actually in force on ITS
			// OWN date, not today's - the most recent reset is excluded
			// because it had not happened yet as of atDate.
			name: "a reset dated after atDate has not happened yet and is excluded",
			resets: []hourMeterReset{
				{ID: "b", OldReading: 5200, NewReading: 10, ChangedAt: "2026-03-01"},
				{ID: "a", OldReading: 5000, NewReading: 0, ChangedAt: "2026-01-01"},
			},
			atDate: mustParseDate(t, "2026-02-01"), // before reset "b"
			want:   5000,                           // only reset "a" was in force by then
		},
		{
			name:   "atDate exactly on a reset's own changed_at counts it as already in force",
			resets: []hourMeterReset{{ID: "a", OldReading: 5000, NewReading: 0, ChangedAt: "2026-01-01"}},
			atDate: mustParseDate(t, "2026-01-01"),
			want:   5000,
		},
		{
			name:   "atDate before every reset on file: no offset was in force yet",
			resets: []hourMeterReset{{ID: "a", OldReading: 5000, NewReading: 0, ChangedAt: "2026-01-01"}},
			atDate: mustParseDate(t, "2025-01-01"),
			want:   0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := offsetInForceAt(tc.resets, tc.atDate)
			if got != tc.want {
				t.Errorf("offsetInForceAt() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestGaugeToTrueHours pins code-review finding 4: the operator always
// types a GAUGE (raw meter) reading, and the server is the one place that
// converts it to true hours, using the offset that was in force on the
// date the reading is FROM - never today's offset for a back-dated entry.
func TestGaugeToTrueHours(t *testing.T) {
	resets := []hourMeterReset{
		{ID: "b", OldReading: 5200, NewReading: 10, ChangedAt: "2026-03-01"},
		{ID: "a", OldReading: 5000, NewReading: 0, ChangedAt: "2026-01-01"},
	}

	t.Run("adds the offset in force at the given date to a gauge reading", func(t *testing.T) {
		got := gaugeToTrueHours(resets, 20, mustParseDate(t, "2026-06-01"))
		if got != 5210 { // 20 + (5200-10)
			t.Errorf("gaugeToTrueHours() = %v, want 5210", got)
		}
	})

	// The whole point of this fix: a completion dated before the latest
	// meter reset must use the EARLIER offset, not today's - otherwise a
	// back-filled service logged before a meter replacement reads as
	// hugely overdue or wildly ahead once converted.
	t.Run("a completion dated before the latest reset uses the earlier offset", func(t *testing.T) {
		got := gaugeToTrueHours(resets, 900, mustParseDate(t, "2026-02-01"))
		if got != 5900 { // 900 + (5000-0), reset "b" hadn't happened yet
			t.Errorf("gaugeToTrueHours() = %v, want 5900", got)
		}
	})

	t.Run("no resets at all: gauge and true are the same figure", func(t *testing.T) {
		got := gaugeToTrueHours(nil, 42, mustParseDate(t, "2026-06-01"))
		if got != 42 {
			t.Errorf("gaugeToTrueHours() = %v, want 42", got)
		}
	})
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
		if got.Gauge != 100 {
			t.Errorf("Gauge = %v, want 100 (raw, with no offset applied)", got.Gauge)
		}
	})

	// Code-review finding: an engine's runtime path only changes while the
	// engine is running - it stops publishing entirely, not slowly, the
	// moment the engine is off. Age must never make an hour reading
	// "unknown": the last value received IS the current reading, however
	// old, and only its AsOf timestamp reports how long ago that was.
	t.Run("an old value is still known - age never makes an hour reading unknown", func(t *testing.T) {
		snapshot := newSignalKSnapshot()
		oldTime := now.Add(-6 * time.Hour)
		reader := func(path string) alarmSample {
			return alarmSample{Present: true, Value: 3600 * 100, LastSeen: oldTime}
		}
		got := currentEquipmentHours(reader, snapshot, "propulsion.main.runTime", 0, now)
		if !got.Known {
			t.Fatalf("expected Known=true regardless of age, got %+v", got)
		}
		if got.Hours != 100 {
			t.Errorf("Hours = %v, want 100", got.Hours)
		}
		if got.AsOf == nil || !got.AsOf.Equal(oldTime) {
			t.Fatalf("expected AsOf = %v, got %v", oldTime, got.AsOf)
		}
	})

	t.Run("a value with no timestamp evidence at all is still known, with no AsOf", func(t *testing.T) {
		snapshot := newSignalKSnapshot()
		reader := func(path string) alarmSample {
			return alarmSample{Present: true, Value: 3600 * 100}
		}
		got := currentEquipmentHours(reader, snapshot, "propulsion.main.runTime", 0, now)
		if !got.Known {
			t.Fatalf("expected Known=true, got %+v", got)
		}
		if got.AsOf != nil {
			t.Fatalf("expected no AsOf when no timestamp evidence exists, got %v", got.AsOf)
		}
	})

	t.Run("no path bound reports no AsOf at all", func(t *testing.T) {
		reader := func(path string) alarmSample {
			t.Fatalf("reader should not be called with no path")
			return alarmSample{}
		}
		got := currentEquipmentHours(reader, nil, "", 0, now)
		if got.AsOf != nil {
			t.Fatalf("expected no AsOf with no path bound, got %v", got.AsOf)
		}
	})
}
