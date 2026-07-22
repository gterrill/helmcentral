package main

import (
	"math"
	"sync"
	"time"
)

const telemetryHistoryCapacity = 4320 // ~6h at the 5s poll cadence (main.go's startTrackPoller interval); longest window in active use today is depth-trend's "3h"

type telemetryPoint struct {
	Value     float64
	Timestamp time.Time
}

type telemetryRingBuffer struct {
	mu     sync.RWMutex
	points []telemetryPoint
	index  int
	full   bool
}

func newTelemetryRingBuffer(capacity int) *telemetryRingBuffer {
	return &telemetryRingBuffer{
		points: make([]telemetryPoint, capacity),
	}
}

func (b *telemetryRingBuffer) record(value float64, ts time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.points[b.index] = telemetryPoint{Value: value, Timestamp: ts}
	b.index = (b.index + 1) % len(b.points)
	if b.index == 0 {
		b.full = true
	}
}

// since returns all recorded points after the given timestamp, in
// chronological order.
func (b *telemetryRingBuffer) since(cutoff time.Time) []telemetryPoint {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var result []telemetryPoint
	if b.full {
		start := b.index
		for i := 0; i < len(b.points); i++ {
			p := b.points[(start+i)%len(b.points)]
			if p.Timestamp.After(cutoff) {
				result = append(result, p)
			}
		}
		return result
	}

	for i := 0; i < b.index; i++ {
		if b.points[i].Timestamp.After(cutoff) {
			result = append(result, b.points[i])
		}
	}
	return result
}

var (
	windGustHistory = newTelemetryRingBuffer(telemetryHistoryCapacity)
	depthHistory    = newTelemetryRingBuffer(telemetryHistoryCapacity)
)

// inMemoryMaxWindGustKts returns the max recorded wind speed in the given
// window, mirroring queryInfluxMaxWindGustKts's contract (-1 sentinel if no
// samples). Recorded values (state.WindSpeedApparentKts) are already in
// knots, unlike raw Influx data which is in m/s - do NOT re-apply
// metersPerSecondToKnots here.
func inMemoryMaxWindGustKts(window string) float64 {
	dur, err := time.ParseDuration(window)
	if err != nil {
		return -1
	}

	points := windGustHistory.since(time.Now().UTC().Add(-dur))
	if len(points) == 0 {
		return -1
	}

	maxKts := points[0].Value
	for _, p := range points[1:] {
		if p.Value > maxKts {
			maxKts = p.Value
		}
	}
	return math.Round(maxKts*10) / 10
}

// inMemoryDepthTrend returns 5-minute bucketed depth means in the given
// window, mirroring queryInfluxDepthTrend's contract (nil sentinel if no
// samples), feeding findLastTideTurningPoint unchanged.
func inMemoryDepthTrend(window string) []depthTrendPoint {
	dur, err := time.ParseDuration(window)
	if err != nil {
		return nil
	}

	points := depthHistory.since(time.Now().UTC().Add(-dur))
	if len(points) == 0 {
		return nil
	}

	const bucketSeconds = int64(5 * 60)
	var result []depthTrendPoint
	sum := 0.0
	count := 0
	bucketKey := points[0].Timestamp.Unix() / bucketSeconds
	bucketTime := points[0].Timestamp

	for _, p := range points {
		key := p.Timestamp.Unix() / bucketSeconds
		if key != bucketKey {
			result = append(result, depthTrendPoint{Time: bucketTime, DepthM: math.Round((sum/float64(count))*100) / 100})
			sum, count = 0, 0
			bucketKey = key
			bucketTime = p.Timestamp
		}
		sum += p.Value
		count++
	}
	result = append(result, depthTrendPoint{Time: bucketTime, DepthM: math.Round((sum/float64(count))*100) / 100})

	return result
}
