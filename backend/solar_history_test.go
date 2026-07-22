package main

import (
	"testing"
	"time"
)

func TestSolarDayStats_FirstSampleBaseline(t *testing.T) {
	stats := &solarDayStats{yesterdayKWh: -1, peakTodayW: -1}
	base := time.Date(2026, time.July, 22, 6, 0, 0, 0, time.UTC)

	stats.record(500, base)

	if stats.todayKWh != 0 {
		t.Fatalf("expected todayKWh 0 on first sample, got %v", stats.todayKWh)
	}
	if stats.peakTodayW != 500 {
		t.Fatalf("expected peakTodayW 500 on first sample, got %v", stats.peakTodayW)
	}
	if stats.yesterdayKWh != -1 {
		t.Fatalf("expected yesterdayKWh to remain sentinel -1, got %v", stats.yesterdayKWh)
	}
	if stats.day != "2026-07-22" {
		t.Fatalf("expected day 2026-07-22, got %q", stats.day)
	}
}

func TestSolarDayStats_IntegratesKnownWattageOverKnownDuration(t *testing.T) {
	stats := &solarDayStats{yesterdayKWh: -1, peakTodayW: -1}
	base := time.Date(2026, time.July, 22, 6, 0, 0, 0, time.UTC)

	// First sample establishes the baseline (no elapsed interval yet).
	stats.record(500, base)

	// 720 further samples at a 5s cadence = exactly 1h of 500W: 0.5kWh.
	for i := 1; i <= 720; i++ {
		stats.record(500, base.Add(time.Duration(i)*5*time.Second))
	}

	// Compare after roundTo3 (the precision inMemorySolarTodayKWh applies)
	// since summing 720 float64 additions accrues sub-Wh rounding noise.
	if got := roundTo3(stats.todayKWh); got != 0.5 {
		t.Fatalf("expected exactly 0.5 kWh integrated, got %v (raw %v)", got, stats.todayKWh)
	}
	if stats.peakTodayW != 500 {
		t.Fatalf("expected peakTodayW 500, got %v", stats.peakTodayW)
	}
}

func TestSolarDayStats_CapsIntegrationAcrossLargeGap(t *testing.T) {
	stats := &solarDayStats{yesterdayKWh: -1, peakTodayW: -1}
	base := time.Date(2026, time.July, 22, 6, 0, 0, 0, time.UTC)

	stats.record(500, base)

	// A 5-minute gap (well beyond solarMaxSampleGap) must not be integrated
	// as if it were a continuous 500W interval.
	gapped := base.Add(5 * time.Minute)
	stats.record(900, gapped)

	if stats.todayKWh != 0 {
		t.Fatalf("expected todayKWh to stay 0 across a capped gap, got %v", stats.todayKWh)
	}

	// Normal cadence resumes from the gapped sample's timestamp.
	stats.record(900, gapped.Add(5*time.Second))

	wantKWh := 900 * (5.0 / 3600) / 1000
	if stats.todayKWh != wantKWh {
		t.Fatalf("expected todayKWh %v after cadence resumes, got %v", wantKWh, stats.todayKWh)
	}
}

func TestSolarDayStats_DayRolloverFreezesYesterdayAndResetsTodayAndPeak(t *testing.T) {
	stats := &solarDayStats{yesterdayKWh: -1, peakTodayW: -1}
	day1 := time.Date(2026, time.July, 22, 6, 0, 0, 0, time.UTC)

	stats.record(500, day1)
	stats.record(500, day1.Add(5*time.Second))
	day1TodayKWh := stats.todayKWh
	if day1TodayKWh <= 0 {
		t.Fatalf("expected some energy accumulated on day 1, got %v", day1TodayKWh)
	}

	day2 := time.Date(2026, time.July, 23, 0, 0, 5, 0, time.UTC)
	stats.record(300, day2)

	if stats.yesterdayKWh != day1TodayKWh {
		t.Fatalf("expected yesterdayKWh to freeze at day1's total %v, got %v", day1TodayKWh, stats.yesterdayKWh)
	}
	if stats.todayKWh != 0 {
		t.Fatalf("expected todayKWh reset to 0 on rollover (no cross-midnight integration), got %v", stats.todayKWh)
	}
	if stats.peakTodayW != 300 {
		t.Fatalf("expected peakTodayW reset to the new day's sample 300, got %v", stats.peakTodayW)
	}
	if stats.day != "2026-07-23" {
		t.Fatalf("expected day updated to 2026-07-23, got %q", stats.day)
	}
}

func TestInMemorySolarTodayKWh_NoSamplesReturnsSentinel(t *testing.T) {
	solarStats = &solarDayStats{yesterdayKWh: -1, peakTodayW: -1}

	if got := inMemorySolarTodayKWh(); got != -1 {
		t.Fatalf("expected sentinel -1, got %v", got)
	}
}

func TestInMemorySolarYesterdayKWh_NoRolloverReturnsSentinel(t *testing.T) {
	solarStats = &solarDayStats{yesterdayKWh: -1, peakTodayW: -1}
	solarStats.record(500, time.Now().UTC())

	if got := inMemorySolarYesterdayKWh(); got != -1 {
		t.Fatalf("expected sentinel -1 before first rollover, got %v", got)
	}
}

func TestInMemorySolarPeakTodayW_NoSamplesReturnsSentinel(t *testing.T) {
	solarStats = &solarDayStats{yesterdayKWh: -1, peakTodayW: -1}

	if got := inMemorySolarPeakTodayW(); got != -1 {
		t.Fatalf("expected sentinel -1, got %v", got)
	}
}

func TestInMemorySolarTodayYesterdayPeak_ReflectRecordedSamples(t *testing.T) {
	solarStats = &solarDayStats{yesterdayKWh: -1, peakTodayW: -1}
	base := time.Date(2026, time.July, 22, 6, 0, 0, 0, time.UTC)

	solarStats.record(500, base)
	solarStats.record(800, base.Add(5*time.Second))

	if got := inMemorySolarTodayKWh(); got <= 0 {
		t.Fatalf("expected positive today_kwh, got %v", got)
	}
	if got := inMemorySolarPeakTodayW(); got != 800 {
		t.Fatalf("expected peak_today_w 800, got %v", got)
	}
}

func TestInMemorySolarTrend24h_BucketsIntoFifteenMinuteMeans(t *testing.T) {
	solarPowerHistory = newTelemetryRingBuffer(solarTrendHistoryCapacity)
	base := time.Now().UTC().Add(-2 * time.Hour).Truncate(15 * time.Minute)

	solarPowerHistory.record(400, base)
	solarPowerHistory.record(420, base.Add(5*time.Minute))
	solarPowerHistory.record(600, base.Add(15*time.Minute))
	solarPowerHistory.record(620, base.Add(20*time.Minute))

	points := inMemorySolarTrend24h()
	if len(points) != 2 {
		t.Fatalf("expected 2 buckets, got %d: %+v", len(points), points)
	}
	if points[0].TotalW != 410 {
		t.Fatalf("expected first bucket mean 410, got %v", points[0].TotalW)
	}
	if points[1].TotalW != 610 {
		t.Fatalf("expected second bucket mean 610, got %v", points[1].TotalW)
	}
}

func TestInMemorySolarTrend24h_NoSamplesReturnsNil(t *testing.T) {
	solarPowerHistory = newTelemetryRingBuffer(solarTrendHistoryCapacity)

	if points := inMemorySolarTrend24h(); points != nil {
		t.Fatalf("expected nil for no samples, got %+v", points)
	}
}
