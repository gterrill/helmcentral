package main

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
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
	querySolarToday     func(now time.Time, loc *time.Location) float64
	querySolarYesterday func(now time.Time, loc *time.Location) float64
	querySolarPeak      func(now time.Time, loc *time.Location) float64
	querySolarTrend     func(now time.Time) []solarTrendPoint

	// vesselLocalLocation resolves the vessel's current local timezone, or
	// ok=false when no usable position is known yet. Injected for the same
	// testability reason as the query funcs above; defaults to
	// currentVesselLocalLocation, which reads globalSignalKSnapshot's cached
	// navigation.position rather than solarStats (solar_history.go's
	// in-memory day accumulator, fed only once a solar sample with a
	// position has been recorded) or a fresh SignalK fetch of its own.
	vesselLocalLocation func() (loc *time.Location, ok bool)

	// lastKnownLoc is the most recent *time.Location vesselLocalLocation
	// resolved from a real fix, kept across ticks (refresh runs serially on
	// one goroutine - startTelemetryInfluxTicker - so no lock is needed).
	// A boat's local zone does not change tick to tick just because the
	// -1,-1 unset-position sentinel flapped in at anchor or GNSS blinked;
	// this is what lets a tick with no current fix still report real solar
	// figures instead of -1, as long as a fix has been seen at least once.
	lastKnownLoc *time.Location
}

func newTelemetryInfluxFetcher(slot *telemetryInfluxSlot) *telemetryInfluxFetcher {
	return &telemetryInfluxFetcher{
		slot:                slot,
		queryGust:           queryInfluxMaxWindGustKtsFor,
		querySolarToday:     queryInfluxSolarTodayKWh,
		querySolarYesterday: queryInfluxSolarYesterdayKWh,
		querySolarPeak:      queryInfluxSolarPeakTodayW,
		querySolarTrend:     queryInfluxSolarTrend24h,
		vesselLocalLocation: currentVesselLocalLocation,
	}
}

// telemetryInfluxSolarPositionMissing gates the "no vessel position yet" log
// line to once per state change (in both directions), the same idiom
// tracksEndpointMissing (tracks.go) uses for its own 404 log line: refresh
// runs every telemetryInfluxRefreshInterval (30s), and a line every tick
// while waiting for the first GNSS fix would bury everything else in the log.
var telemetryInfluxSolarPositionMissing atomic.Bool

// currentVesselLocalLocation derives the vessel's local timezone from
// globalSignalKSnapshot's own cached navigation.position - the same
// delta-stream snapshot readOwnEncounterFacts (collision_ais.go) already
// reads position from via nodeAt, one small node copy under an RLock, not a
// fresh SignalK HTTP fetch. sampleTracks (tracks.go) already pays for one
// such fetch per 5s tick to seed solarStats.loc for the in-memory tier; this
// refresher must not add a second fetch of its own every
// telemetryInfluxRefreshInterval on top of it.
//
// ok is false when no usable position is known yet - absent, or one of
// hasUsableVesselPosition's rejected sentinel pairs (weather_providers.go),
// e.g. before the first GNSS fix after a restart. The caller must not fall
// back to UTC silently in that case (Fallback Policy): refresh reports the
// solar day-bounded fields as the -1 sentinel for that tick instead of
// guessing a UTC boundary.
func currentVesselLocalLocation() (*time.Location, bool) {
	node := globalSignalKSnapshot.nodeAt("navigation.position")
	if node == nil {
		return nil, false
	}
	lat, lon, ok := positionFromNode(node)
	if !ok || !hasUsableVesselPosition(lat, lon) {
		return nil, false
	}
	return vesselLocalLocation(lon), true
}

// refresh runs one pass unconditionally, the same way updateNearestTideStation
// (tide_auto_update.go) always runs and lets its own settings check no-op
// cheaply: when Influx isn't configured, queryInfluxMaxWindGustKtsFor and the
// solar queries already return their -1/nil sentinels immediately via
// newInfluxClient's ok=false, and nothing downstream reads this slot's
// result unless influxTelemetryConfigured() is also true (computeMaxGustKtsFor,
// applyInfluxSolarOverride) -- so there is no configuration branch to
// duplicate here.
//
// The gust ladder and the rolling 24h solar trend are unrelated to vessel
// position and always run. The three day-boundary solar queries
// (today/yesterday/peak) additionally need a usable position to resolve a
// zone: f.vesselLocalLocation reports the CURRENT tick's position, but this
// tick's fix flapping (the -1,-1 sentinel at anchor, or GNSS blinking) does
// not mean the boat changed timezone, so a tick with no current fix reuses
// f.lastKnownLoc from whichever earlier tick last resolved one instead of
// re-deriving from scratch. Only when no position has EVER been resolved -
// f.lastKnownLoc is still nil, which is true at most until the first fix
// after a restart - does this report the sentinel -1 rather than guessing a
// UTC boundary (Fallback Policy). See currentVesselLocalLocation's own doc
// comment for why the sentinel pair is rejected in the first place.
func (f *telemetryInfluxFetcher) refresh(now time.Time) {
	result := telemetryInfluxResult{
		gustKts:       f.queryGust(gustWindowLadder),
		solarTrend24h: f.querySolarTrend(now),
		fetchedAt:     now,
	}

	loc, positionOK := f.vesselLocalLocation()
	if positionOK {
		f.lastKnownLoc = loc
		if telemetryInfluxSolarPositionMissing.CompareAndSwap(true, false) {
			log.Printf("solar: vessel position known again; today_kwh/yesterday_kwh/peak_today_w resume using local-day boundaries")
		}
	} else if f.lastKnownLoc != nil {
		// No fix this tick, but a real one has been seen before: the solar
		// data is fine and the zone does not change tick to tick, so reuse
		// it rather than reporting unavailable.
		loc = f.lastKnownLoc
	} else {
		if telemetryInfluxSolarPositionMissing.CompareAndSwap(false, true) {
			log.Printf("solar: no vessel position known yet; today_kwh/yesterday_kwh/peak_today_w report unavailable (-1) rather than assuming a UTC day boundary")
		}
		result.solarTodayKWh = -1
		result.solarYesterdayKWh = -1
		result.solarPeakTodayW = -1
		f.slot.set(result)
		return
	}

	result.solarTodayKWh = f.querySolarToday(now, loc)
	result.solarYesterdayKWh = f.querySolarYesterday(now, loc)
	result.solarPeakTodayW = f.querySolarPeak(now, loc)
	f.slot.set(result)
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
