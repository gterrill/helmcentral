package main

import (
	"math"
	"sort"
	"sync"
	"time"
)

const telemetryHistoryCapacity = 4320 // ~6h at the 5s poll cadence (main.go's startTrackPoller interval); sized for depth-trend's longest window, "3h"

const windGustHistoryCapacity = 17280 // ~24h at the 5s poll cadence — sized for the longest window in the gust ladder (see gustWindowLadder in main.go)

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
	windGustHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	depthHistory    = newTelemetryRingBuffer(telemetryHistoryCapacity)
)

// inMemoryMaxWindGustKts returns the max recorded wind speed in the given
// window, mirroring queryInfluxMaxWindGustKtsForWindow's per-window contract
// (-1 sentinel if no samples). Recorded values (state.WindSpeedApparentKts) are already in
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

// inMemoryMaxWindGustKtsFor computes inMemoryMaxWindGustKts for every
// requested window in a SINGLE backward walk of windGustHistory's ring
// buffer, instead of one since()-scan-plus-allocation per window (since()
// scans all windGustHistoryCapacity slots and allocates a result slice on
// every call).
//
// This is only correct because the gust ladder is NESTED: every longer
// window's time range is a superset of every shorter window's (e.g. 10m ⊂
// 30m ⊂ 1h ⊂ 24h). Walking the ring newest→oldest while keeping a running
// max, and snapshotting that running max each time the walk crosses a
// window's cutoff (shortest cutoff = most recent = crossed first), yields
// every window's answer in one pass.
//
// Because the returned value is a map keyed by window string, callers don't
// depend on input order — so rather than silently producing wrong answers
// for an out-of-order or non-nested ladder, this function sorts a local copy
// of windows by parsed duration (ascending) before the pass. (A ladder whose
// windows aren't actually nested — e.g. two disjoint windows — is still not
// something this function can detect; nesting-by-construction is the
// documented precondition. Sorting only guards against *order*, not against
// a genuinely non-nested set.)
func inMemoryMaxWindGustKtsFor(windows []string) map[string]float64 {
	out := make(map[string]float64, len(windows))

	type parsedWindow struct {
		window string
		dur    time.Duration
	}
	parsed := make([]parsedWindow, 0, len(windows))
	for _, w := range windows {
		dur, err := time.ParseDuration(w)
		if err != nil {
			out[w] = -1
			continue
		}
		out[w] = -1 // pre-seed the sentinel; overwritten below only if samples are found
		parsed = append(parsed, parsedWindow{window: w, dur: dur})
	}
	if len(parsed) == 0 {
		return out
	}

	sort.Slice(parsed, func(i, j int) bool { return parsed[i].dur < parsed[j].dur })

	now := time.Now().UTC()
	cutoffs := make([]time.Time, len(parsed))
	for i, pw := range parsed {
		cutoffs[i] = now.Add(-pw.dur)
	}

	windGustHistory.mu.RLock()
	defer windGustHistory.mu.RUnlock()

	b := windGustHistory
	n := len(b.points)
	count := n
	if !b.full {
		count = b.index
	}

	running := math.Inf(-1)
	wi := 0
	for i := 0; i < count; i++ {
		idx := ((b.index-1-i)%n + n) % n
		p := b.points[idx]
		for wi < len(parsed) && !p.Timestamp.After(cutoffs[wi]) {
			if !math.IsInf(running, -1) {
				out[parsed[wi].window] = math.Round(running*10) / 10
			}
			wi++
		}
		if wi >= len(parsed) {
			return out
		}
		if p.Value > running {
			running = p.Value
		}
	}
	for ; wi < len(parsed); wi++ {
		if !math.IsInf(running, -1) {
			out[parsed[wi].window] = math.Round(running*10) / 10
		}
	}

	return out
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
