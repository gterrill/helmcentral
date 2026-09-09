package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

/*
Background fetcher for the forecast-warnings-as-alarms plan (ADR 0087).

Until now an official met-service marine warning for the vessel's zone (ADR
0019's WASM forecast-warnings plugin) reached the operator only as a
dismissible dashboard banner on a ten-minute browser poll: no severity, no
occurrence log, no off-boat delivery, and a dead provider cleared the banner
in silence. This file is the background half of folding it into the same
derived-paths-plus-seeded-rules pattern ADR 0055 and ADR 0070 already use, so
dwell, worst-wins, acknowledge, the log and every notification transport
come for free.

The fetcher polls independently of the SignalK delta stream, on its own
ten-minute cadence, because a marine warning bulletin changes far slower
than the boat's live data and there is nothing to gain from hammering the
upstream service on the same cadence computeDerivedPaths itself runs (once a
second). It writes into globalForecastWarningsSlot, a small mutex-guarded
value that derived_paths.go's addForecastWarningValues reads on every tick
without repeating any of the mapping work done here.

forecastWarningsReadingFrom below maps a fetched bundle onto the wind ladder
and the surf flag. forecastWarningsFetcher.fetchOnce is the fetch loop
itself, including the stale-served detection that keeps a dead upstream
provider from reading as a quiet sea (the WASM adapter's own stale-on-error
cache fallback returns err == nil, which would otherwise look identical to a
genuinely fresh "nothing in force" result).
*/

// forecastWarningsFetchInterval is how often the fetcher actually calls the
// provider once it has a usable position. Ten minutes is short enough that a
// newly issued gale warning reaches the alarm centre promptly, and long
// enough that polling a marine warning service this often is not itself
// something an operator has to think about.
const forecastWarningsFetchInterval = 10 * time.Minute

// forecastWarningsCheckInterval is the ticker's own period: how often
// startForecastWarningsFetcher asks the fetcher whether a real provider call
// is due. It is much shorter than forecastWarningsFetchInterval so a
// fetcher waiting on a usable vessel position (no fix yet, or the -1,-1 /
// 0,0 sentinel pairs hasUsableVesselPosition rejects) retries promptly once
// one appears, rather than sitting idle for up to ten minutes after the
// boat gets a GNSS fix.
const forecastWarningsCheckInterval = 30 * time.Second

// forecastWarningsMaxAge is how old a reading sitting in the slot can be
// before the derived paths it feeds go absent rather than merely aged.
// Three fetch intervals: one missed fetch is a blip the boat's own network
// can explain away; three in a row means the fetcher itself is in trouble,
// which is what the "Forecast warnings unavailable" rule
// (alarm_seed_forecast_warnings.go) exists to say once its own dwell
// confirms it is not a blip either.
const forecastWarningsMaxAge = 3 * forecastWarningsFetchInterval

// forecastWarningsReading is the mapped, host-computed result of one
// successful fetch: the wind ladder's level and the surf flag, plus when it
// was fetched. The slot stores this rather than the raw bundle, so
// computeDerivedPaths - which runs every second - never repeats the string
// comparisons forecastWarningsReadingFrom does on a tick where nothing new
// has arrived from the provider.
type forecastWarningsReading struct {
	WindLevel int
	Surf      bool
	FetchedAt time.Time
}

// forecastWarningsSlot is the single piece of state the fetcher goroutine
// writes and every computeDerivedPaths call reads, guarded by its own mutex
// since the two run on different goroutines. ok distinguishes "no fetch has
// ever landed" from "the last fetch reported no warnings in force", which is
// itself a legitimate and very common result (forecastWarningsReadingFrom's
// zero value): a boat that has never fetched must read as absent, not as a
// quiet sea.
type forecastWarningsSlot struct {
	mu      sync.RWMutex
	reading forecastWarningsReading
	ok      bool
}

func (s *forecastWarningsSlot) set(reading forecastWarningsReading) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reading = reading
	s.ok = true
}

func (s *forecastWarningsSlot) get() (forecastWarningsReading, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.reading, s.ok
}

func (s *forecastWarningsSlot) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reading = forecastWarningsReading{}
	s.ok = false
}

// globalForecastWarningsSlot is the production slot startForecastWarningsFetcher
// writes into and addForecastWarningValues (derived_paths.go) reads from.
// Tests construct their own *forecastWarningsSlot instead of touching this
// one wherever a value can be threaded through directly; production code on
// both ends has no such thread, so this is where they meet.
var globalForecastWarningsSlot = &forecastWarningsSlot{}

// forecastWindWarningLevelFor ranks one section's warning_type onto the wind
// ladder. First match wins, checked in this order deliberately: "watch" is
// tested before "gale"/"storm"/"hurricane" so a Gale Watch reads as the
// lighter level-1 advisory it actually is, not a level-2 gale in force.
//
// known is false for a wind-category warning_type this ladder does not
// recognise. That is not treated as absence: the plugin already asserted the
// bulletin is active and in the wind category, so discarding it because the
// host's own vocabulary is incomplete would be exactly the masking fallback
// this repo's fallback policy forbids. It ranks at the ladder's floor (1)
// instead, and the caller logs the unrecognised type so the ladder can be
// extended.
func forecastWindWarningLevelFor(warningType string) (level int, known bool) {
	lower := strings.ToLower(warningType)
	switch {
	case strings.Contains(lower, "watch"):
		return 1, true
	case strings.Contains(lower, "hurricane"), strings.Contains(lower, "storm"):
		return 3, true
	case strings.Contains(lower, "gale"):
		return 2, true
	case strings.Contains(lower, "strong wind"), strings.Contains(lower, "small craft"),
		strings.Contains(lower, "wind advisory"), strings.Contains(lower, "brisk wind"):
		return 1, true
	default:
		return 1, false
	}
}

// forecastWarningsReadingFrom maps a bundle's active bulletins onto the wind
// ladder and the surf flag, and reports every unrecognised wind warning_type
// it met along the way so the caller can log them. FetchedAt is deliberately
// left unset here - the fetcher stamps that against its own wall-clock now,
// not the bundle's CachedAt, which the WASM adapter can carry from well in
// the past on a stale-served response (see forecastWarningsFetcher.fetchOnce).
//
// A bundle with no bulletins at all - the common case on a quiet day - maps
// to the zero-value reading, level 0 and no surf, and that is a genuine
// result, not an absence: the caller stores it in the slot exactly like any
// other successful fetch.
func forecastWarningsReadingFrom(bundle forecastWarningsBundle) (forecastWarningsReading, []string) {
	var reading forecastWarningsReading
	var unknownTypes []string

	for _, bulletin := range bundle.Bulletins {
		switch strings.ToLower(strings.TrimSpace(bulletin.Category)) {
		case "wind":
			for _, section := range bulletin.Sections {
				level, known := forecastWindWarningLevelFor(section.WarningType)
				if !known {
					unknownTypes = append(unknownTypes, section.WarningType)
				}
				if level > reading.WindLevel {
					reading.WindLevel = level
				}
			}
		case "surf":
			if len(bulletin.Sections) > 0 {
				reading.Surf = true
			}
		}
	}

	return reading, unknownTypes
}

// forecastWarningsFetcher is the ctx+ticker background fetch loop, shaped
// after startAnchorDragWatcher (alarm_anchor.go) rather than the ctx-less
// tide updater: this is state an alarm rule reads, so like the anchor watch
// it has to stop cleanly on shutdown.
//
// resolve and position are injected func fields, this repo's idiom for
// making a goroutine's real-world dependencies swappable in a test without a
// live SignalK connection or a registered plugin.
type forecastWarningsFetcher struct {
	slot     *forecastWarningsSlot
	interval time.Duration

	resolve  func() (forecastWarningsProvider, string, error)
	position func() (lat, lon float64, ok bool)

	// nextDue is when the next real provider call is allowed. Its zero value
	// means "due now": the very first check() call always attempts a fetch,
	// so a freshly started boat does not sit for a full interval before its
	// first attempt.
	nextDue time.Time

	// lastLoggedError and waitingForPositionLogged each de-duplicate one
	// failure mode's log line across repeated 30s ticks: without them a dead
	// provider or a boat with no fix yet would write a new log line every
	// half minute for as long as the condition lasted.
	lastLoggedError          string
	waitingForPositionLogged bool
}

// newForecastWarningsFetcher wires the production dependencies: settings.yaml
// via resolveForecastWarningsProvider, and position via
// fetchSignalKVesselState + hasUsableVesselPosition, the same pair
// updateNearestTideStation (tide_auto_update.go) uses.
func newForecastWarningsFetcher(slot *forecastWarningsSlot, interval time.Duration) *forecastWarningsFetcher {
	return &forecastWarningsFetcher{
		slot:     slot,
		interval: interval,
		resolve: func() (forecastWarningsProvider, string, error) {
			return resolveForecastWarningsProvider(getEnv("SETTINGS_FILE", "../settings.yaml"))
		},
		position: func() (float64, float64, bool) {
			state, err := fetchSignalKVesselState()
			if err != nil {
				return 0, 0, false
			}
			return state.Latitude, state.Longitude, hasUsableVesselPosition(state.Latitude, state.Longitude)
		},
	}
}

// check is called on every 30s tick and only calls the provider when a fetch
// is actually due, per nextDue.
func (f *forecastWarningsFetcher) check(now time.Time) {
	if !f.nextDue.IsZero() && now.Before(f.nextDue) {
		return
	}
	f.fetchOnce(now)
}

// fetchOnce is one attempt: resolve the configured provider, confirm a
// usable position, fetch, and either store a new reading or leave the slot
// exactly as it was. Every path out of this function sets nextDue, so a boat
// waiting on a GPS fix or with no plugin installed always has a next
// scheduled attempt rather than retrying every 30s tick forever.
func (f *forecastWarningsFetcher) fetchOnce(now time.Time) {
	lat, lon, ok := f.position()
	if !ok {
		if !f.waitingForPositionLogged {
			log.Printf("forecast warnings: no usable vessel position yet, retrying every %s until one arrives", forecastWarningsCheckInterval)
			f.waitingForPositionLogged = true
		}
		f.nextDue = now.Add(forecastWarningsCheckInterval)
		return
	}
	f.waitingForPositionLogged = false

	provider, id, err := f.resolve()
	if err != nil {
		f.logFetchError(fmt.Sprintf("forecast warnings: %v", err))
		f.nextDue = now.Add(f.interval)
		return
	}

	bundle, err := provider.FetchWarnings(lat, lon)
	if err == nil && forecastWarningsBundleIsStaleServed(bundle, provider.TTLSeconds(), now) {
		// The WASM adapter's own stale-on-error cache fallback
		// (wasm_forecast_warnings_provider.go) returns err == nil with an old
		// CachedAt when the upstream is unreachable, so a dead provider looks
		// exactly like a healthy cache hit unless this is checked explicitly.
		// Treating it as a failure here, not a value, is what keeps a dead
		// BOM from reading as "no warnings in force".
		err = fmt.Errorf("provider %q served a cached bundle from %s, older than its own %ds TTL: the upstream fetch is failing", id, bundle.CachedAt.UTC().Format(time.RFC3339), provider.TTLSeconds())
	}
	if err != nil {
		f.logFetchError(fmt.Sprintf("forecast warnings provider %q error: %v", id, err))
		f.nextDue = now.Add(f.interval)
		return
	}
	f.clearFetchError()

	reading, unknownTypes := forecastWarningsReadingFrom(bundle)
	reading.FetchedAt = now
	f.slot.set(reading)

	for _, warningType := range unknownTypes {
		log.Printf("forecast warnings: %q is not on the wind ladder, ranked at its floor (level 1) rather than dropped", warningType)
	}

	f.nextDue = now.Add(f.interval)
}

// forecastWarningsBundleIsStaleServed reports whether bundle can only have
// come from the WASM adapter's stale-on-error cache fallback: Cached, with a
// known CachedAt older than the provider's own advertised TTL. A cache hit
// still inside the TTL is a legitimately fresh answer and must not be
// flagged.
func forecastWarningsBundleIsStaleServed(bundle forecastWarningsBundle, ttlSeconds int64, now time.Time) bool {
	if !bundle.Cached || bundle.CachedAt.IsZero() {
		return false
	}
	return now.Sub(bundle.CachedAt) > time.Duration(ttlSeconds)*time.Second
}

func (f *forecastWarningsFetcher) logFetchError(msg string) {
	if f.lastLoggedError == msg {
		return
	}
	log.Print(msg)
	f.lastLoggedError = msg
}

// clearFetchError logs a single recovery line when a run of failures ends,
// so the log shows an outage's whole shape - when it started and that it
// ended - rather than only its start.
func (f *forecastWarningsFetcher) clearFetchError() {
	if f.lastLoggedError != "" {
		log.Print("forecast warnings: fetch recovered")
		f.lastLoggedError = ""
	}
}

// startForecastWarningsFetcher runs the fetch loop until ctx is cancelled,
// mirroring startAnchorDragWatcher's shape: a ticker at the check cadence,
// not the fetch cadence, so check() itself decides when a real provider call
// is due.
func startForecastWarningsFetcher(ctx context.Context, interval time.Duration) {
	fetcher := newForecastWarningsFetcher(globalForecastWarningsSlot, interval)

	ticker := time.NewTicker(forecastWarningsCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			fetcher.check(now.UTC())
		}
	}
}
