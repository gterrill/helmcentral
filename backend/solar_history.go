package main

import (
	"sync"
	"time"
)

const solarMaxSampleGap = 30 * time.Second // ~6x the 5s poll cadence; caps Riemann-sum
// integration across restarts/missed ticks so a stale interval isn't counted
// as continuous production.

// solarDayStats is a day-scoped Riemann-sum accumulator fed by sampleTracks
// on each 5s poll tick, giving solarState() an Influx-free default for
// today/yesterday energy and today's peak wattage.
type solarDayStats struct {
	mu           sync.Mutex
	day          string // UTC "2006-01-02"; "" until the first sample
	todayKWh     float64
	yesterdayKWh float64 // -1 sentinel until the first day rollover
	peakTodayW   float64 // -1 sentinel until the first sample of the day
	lastSampleAt time.Time
}

var solarStats = &solarDayStats{yesterdayKWh: -1, peakTodayW: -1}

func (s *solarDayStats) record(sampleW float64, ts time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	day := ts.UTC().Format("2006-01-02")
	if day != s.day {
		// No cross-midnight integration: the interval spanning the UTC day
		// boundary is dropped from both days rather than split at
		// midnight - an accepted simplification versus Influx's own
		// boundary-aware query.
		if s.day != "" {
			s.yesterdayKWh = s.todayKWh
		}
		s.day = day
		s.todayKWh = 0
		s.peakTodayW = sampleW
		s.lastSampleAt = ts
		return
	}

	elapsed := ts.Sub(s.lastSampleAt)
	if elapsed > 0 && elapsed <= solarMaxSampleGap {
		s.todayKWh += sampleW * elapsed.Hours() / 1000
		if sampleW > s.peakTodayW {
			s.peakTodayW = sampleW
		}
	}
	s.lastSampleAt = ts
}

func inMemorySolarTodayKWh() float64 {
	solarStats.mu.Lock()
	defer solarStats.mu.Unlock()
	if solarStats.day == "" {
		return -1
	}
	return roundTo3(solarStats.todayKWh)
}

func inMemorySolarYesterdayKWh() float64 {
	solarStats.mu.Lock()
	defer solarStats.mu.Unlock()
	return roundTo3(solarStats.yesterdayKWh)
}

func inMemorySolarPeakTodayW() float64 {
	solarStats.mu.Lock()
	defer solarStats.mu.Unlock()
	return roundTo1(solarStats.peakTodayW)
}

const solarTrendHistoryCapacity = 17280   // ~24h at 5s poll cadence (~550KB, negligible)
const solarTrendBucket = 15 * time.Minute // matches queryInfluxSolarTrend24h's aggregateWindow

var solarPowerHistory = newTelemetryRingBuffer(solarTrendHistoryCapacity)

// inMemorySolarTrend24h returns 15-min bucketed power means over the
// trailing 24h, mirroring queryInfluxSolarTrend24h's contract (nil sentinel
// if no samples). Copy-adapted from inMemoryDepthTrend's bucketing loop
// (telemetry_history.go) rather than shared, to keep this diff scoped.
func inMemorySolarTrend24h() []solarTrendPoint {
	points := solarPowerHistory.since(time.Now().UTC().Add(-24 * time.Hour))
	if len(points) == 0 {
		return nil
	}

	const bucketSeconds = int64(solarTrendBucket / time.Second)
	var result []solarTrendPoint
	sum := 0.0
	count := 0
	bucketKey := points[0].Timestamp.Unix() / bucketSeconds
	bucketTime := points[0].Timestamp

	for _, p := range points {
		key := p.Timestamp.Unix() / bucketSeconds
		if key != bucketKey {
			result = append(result, solarTrendPoint{Time: bucketTime, TotalW: roundTo1(sum / float64(count))})
			sum, count = 0, 0
			bucketKey = key
			bucketTime = p.Timestamp
		}
		sum += p.Value
		count++
	}
	result = append(result, solarTrendPoint{Time: bucketTime, TotalW: roundTo1(sum / float64(count))})

	return result
}
