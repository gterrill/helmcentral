package main

import (
	"context"
	"sync"
	"time"
)

/*
Before this, buildVesselStatePayload's gust ladder and buildSolarStatePayload's
four solar figures ran their Influx queries inline on every single call --
the gust ladder alone is four serial Flux max() queries, one of them a raw
24h scan, with a 4s timeout each. With the old per-call Influx client (see
newInfluxClient in influx.go) that meant a fresh TCP connection too. Every
stream client rebuilding vessel-state every second and solar-state every 10s,
plus every GET /api/vessel-state and /api/solar-state, paid that cost
directly and serially.

This moves the Influx-backed part of both onto one background ticker
(startTelemetryInfluxTicker, started from main.go alongside the other
background loops) that refreshes a shared, mutex-guarded result on
telemetryInfluxRefreshInterval. buildVesselStatePayload and
buildSolarStatePayload now just read the stored result instead of querying
Influx themselves -- the same tradeoff computeMaxGustKtsFor already makes
between the Influx and in-memory gust sources, just moved up a level.

The in-memory (non-Influx) gust path is unaffected: it stays computed on
demand in computeMaxGustKtsFor, since it is cheap (an in-process ring
buffer, no network).
*/

// telemetryInfluxRefreshInterval is the ticker's cadence. 30s is frequent
// enough that the gust ladder and solar figures do not look stuck to an
// operator watching the dashboard, and infrequent enough that four serial
// Flux queries plus four more no longer happen on every stream tick.
const telemetryInfluxRefreshInterval = 30 * time.Second

// telemetryInfluxStaleAfter bounds how long a stored result may be served
// before builders must treat it as unavailable rather than current -- the
// fallback policy's "a cached value must not outlive its freshness" applied
// here: three missed ticks means the ticker itself is stuck or Influx has
// been down for longer than a blip, not a fluke worth quietly papering over
// with old numbers. Same "3 x interval" shape as forecastWarningsMaxAge
// (forecast_warnings_fetcher.go).
const telemetryInfluxStaleAfter = 3 * telemetryInfluxRefreshInterval

// telemetryInfluxResult is everything one ticker pass refreshes, stamped
// with a single fetchedAt: the gust ladder and the four solar figures are
// queried back-to-back in the same tick, so one timestamp covers all of them
// rather than each field aging independently.
type telemetryInfluxResult struct {
	gustKts           map[string]float64
	solarTodayKWh     float64
	solarYesterdayKWh float64
	solarPeakTodayW   float64
	solarTrend24h     []solarTrendPoint
	fetchedAt         time.Time
}

// telemetryInfluxSlot is the mutex-guarded value the ticker writes and the
// two builders read, mirroring forecastWarningsSlot's shape
// (forecast_warnings_fetcher.go).
type telemetryInfluxSlot struct {
	mu     sync.RWMutex
	result telemetryInfluxResult
}

func (s *telemetryInfluxSlot) set(result telemetryInfluxResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.result = result
}

func (s *telemetryInfluxSlot) get() telemetryInfluxResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.result
}

// globalTelemetryInfluxSlot is the production slot startTelemetryInfluxTicker
// writes into and computeMaxGustKtsFor/applyInfluxSolarOverride read from.
var globalTelemetryInfluxSlot = &telemetryInfluxSlot{}

// telemetryInfluxFetcher runs the actual queries. The five query* fields are
// injected funcs (this repo's idiom for making a background loop's real
// dependencies swappable in tests without a live Influx connection -- see
// forecastWarningsFetcher.resolve/position), defaulting to the real
// influx.go query functions.
type telemetryInfluxFetcher struct {
	slot *telemetryInfluxSlot

	queryGust           func(windows []string) map[string]float64
	querySolarToday     func(now time.Time) float64
	querySolarYesterday func(now time.Time) float64
	querySolarPeak      func(now time.Time) float64
	querySolarTrend     func(now time.Time) []solarTrendPoint
}

func newTelemetryInfluxFetcher(slot *telemetryInfluxSlot) *telemetryInfluxFetcher {
	return &telemetryInfluxFetcher{
		slot:                slot,
		queryGust:           queryInfluxMaxWindGustKtsFor,
		querySolarToday:     queryInfluxSolarTodayKWh,
		querySolarYesterday: queryInfluxSolarYesterdayKWh,
		querySolarPeak:      queryInfluxSolarPeakTodayW,
		querySolarTrend:     queryInfluxSolarTrend24h,
	}
}

// refresh runs one pass unconditionally, the same way updateNearestTideStation
// (tide_auto_update.go) always runs and lets its own settings check no-op
// cheaply: when Influx isn't configured, queryInfluxMaxWindGustKtsFor and the
// solar queries already return their -1/nil sentinels immediately via
// newInfluxClient's ok=false, and nothing downstream reads this slot's
// result unless influxTelemetryConfigured() is also true (computeMaxGustKtsFor,
// applyInfluxSolarOverride) -- so there is no configuration branch to
// duplicate here.
func (f *telemetryInfluxFetcher) refresh(now time.Time) {
	f.slot.set(telemetryInfluxResult{
		gustKts:           f.queryGust(gustWindowLadder),
		solarTodayKWh:     f.querySolarToday(now),
		solarYesterdayKWh: f.querySolarYesterday(now),
		solarPeakTodayW:   f.querySolarPeak(now),
		solarTrend24h:     f.querySolarTrend(now),
		fetchedAt:         now,
	})
}

// startTelemetryInfluxTicker runs refresh once immediately -- so the first
// stream client and the first /api/vessel-state or /api/solar-state request
// after startup do not sit behind a zero-value (and therefore "unavailable")
// slot for a full interval -- and then on telemetryInfluxRefreshInterval
// until ctx is cancelled.
func startTelemetryInfluxTicker(ctx context.Context) {
	fetcher := newTelemetryInfluxFetcher(globalTelemetryInfluxSlot)
	fetcher.refresh(time.Now().UTC())

	ticker := time.NewTicker(telemetryInfluxRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			fetcher.refresh(now.UTC())
		}
	}
}

// cachedMaxGustKtsFor reads the slot for computeMaxGustKtsFor's Influx
// branch, collapsing every window to -1 once the stored result is older
// than telemetryInfluxStaleAfter -- the same "unavailable", not "stale data
// presented as current," contract a failed query already returns.
func cachedMaxGustKtsFor(windows []string, now time.Time) map[string]float64 {
	result := globalTelemetryInfluxSlot.get()
	stale := result.fetchedAt.IsZero() || now.Sub(result.fetchedAt) > telemetryInfluxStaleAfter

	values := make(map[string]float64, len(windows))
	for _, window := range windows {
		if stale {
			values[window] = -1
			continue
		}
		if v, ok := result.gustKts[window]; ok {
			values[window] = v
		} else {
			values[window] = -1
		}
	}
	return values
}

// cachedInfluxSolarOverride reads the slot for applyInfluxSolarOverride,
// applying the same staleness rule as cachedMaxGustKtsFor: a stale result
// reports the query-failure sentinels (-1/-1/-1/nil), not the last good
// numbers.
func cachedInfluxSolarOverride(state solarStateData, now time.Time) solarStateData {
	result := globalTelemetryInfluxSlot.get()
	if result.fetchedAt.IsZero() || now.Sub(result.fetchedAt) > telemetryInfluxStaleAfter {
		state.TodayKWh = -1
		state.YesterdayKWh = -1
		state.PeakTodayW = -1
		state.Trend24hTotal = nil
		return state
	}

	state.TodayKWh = result.solarTodayKWh
	state.YesterdayKWh = result.solarYesterdayKWh
	state.PeakTodayW = result.solarPeakTodayW
	state.Trend24hTotal = result.solarTrend24h
	return state
}
