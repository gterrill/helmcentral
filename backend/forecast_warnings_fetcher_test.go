package main

import (
	"fmt"
	"testing"
	"time"
)

// --- forecastWindWarningLevelFor: the wind ladder ---

func TestForecastWindWarningLevelFor(t *testing.T) {
	cases := []struct {
		warningType string
		level       int
		known       bool
	}{
		{"Strong Wind Warning", 1, true},
		{"Gale Warning", 2, true},
		{"Storm Warning", 3, true},
		{"Small Craft Advisory", 1, true},
		{"Small Craft Advisory for Hazardous Seas", 1, true},
		{"Hurricane Force Wind Warning", 3, true},
		{"Tropical Storm Warning", 3, true},
		// "watch" is checked before "gale"/"storm"/"hurricane" deliberately: a
		// Gale Watch is the lighter advisory, not a gale in force.
		{"Gale Watch", 1, true},
		{"Brisk Wind Advisory", 1, true},
		{"gale", 2, true},
		{"Marine Weather Statement", 1, false},
	}

	for _, tc := range cases {
		level, known := forecastWindWarningLevelFor(tc.warningType)
		if level != tc.level || known != tc.known {
			t.Errorf("forecastWindWarningLevelFor(%q) = (%d, %v), want (%d, %v)", tc.warningType, level, known, tc.level, tc.known)
		}
	}
}

// --- forecastWarningsReadingFrom: bundle to reading ---

func TestForecastWarningsReadingFrom_MaxAcrossBulletins(t *testing.T) {
	bundle := forecastWarningsBundle{
		Bulletins: []forecastWarningBulletin{
			{Category: "wind", Sections: []forecastWarningSection{{WarningType: "Strong Wind Warning"}}},
			{Category: "wind", Sections: []forecastWarningSection{{WarningType: "Gale Warning"}}},
		},
	}

	reading, unknown := forecastWarningsReadingFrom(bundle)
	if reading.WindLevel != 2 {
		t.Fatalf("expected the higher of the two bulletins (gale, level 2), got %d", reading.WindLevel)
	}
	if reading.Surf {
		t.Fatalf("expected no surf warning")
	}
	if len(unknown) != 0 {
		t.Fatalf("expected no unknown types, got %v", unknown)
	}
}

func TestForecastWarningsReadingFrom_SurfSetsSurfNotWindLevel(t *testing.T) {
	bundle := forecastWarningsBundle{
		Bulletins: []forecastWarningBulletin{
			{Category: "surf", Sections: []forecastWarningSection{{WarningType: "Surf Warning"}}},
		},
	}

	reading, _ := forecastWarningsReadingFrom(bundle)
	if !reading.Surf {
		t.Fatalf("expected the surf flag to be set")
	}
	if reading.WindLevel != 0 {
		t.Fatalf("expected a surf bulletin to leave WindLevel at 0, got %d", reading.WindLevel)
	}
}

func TestForecastWarningsReadingFrom_WindBulletinWithNoSectionsContributesNothing(t *testing.T) {
	bundle := forecastWarningsBundle{
		Bulletins: []forecastWarningBulletin{
			{Category: "wind", Sections: nil},
		},
	}

	reading, unknown := forecastWarningsReadingFrom(bundle)
	if reading.WindLevel != 0 {
		t.Fatalf("expected a sectionless wind bulletin to contribute nothing, got level %d", reading.WindLevel)
	}
	if len(unknown) != 0 {
		t.Fatalf("expected no unknown types from a sectionless bulletin, got %v", unknown)
	}
}

// An empty bundle - no bulletins at all, the common case on a quiet day - is
// a legitimate "no warnings currently active" result, not an absence. The
// caller stores this in the slot exactly like any other successful fetch.
func TestForecastWarningsReadingFrom_EmptyBundleIsPresentNotAbsent(t *testing.T) {
	reading, unknown := forecastWarningsReadingFrom(forecastWarningsBundle{})
	if reading.WindLevel != 0 || reading.Surf {
		t.Fatalf("expected a zero-value reading for an empty bundle, got %+v", reading)
	}
	if len(unknown) != 0 {
		t.Fatalf("expected no unknown types for an empty bundle, got %v", unknown)
	}
}

// A wind-category warning_type the ladder does not recognise still ranks at
// the ladder's floor (level 1) rather than being dropped - the plugin
// already asserted the bulletin is active and in the wind category, so
// discarding it would be the masking fallback this repo's policy forbids.
// It is reported back so the caller can log it.
func TestForecastWarningsReadingFrom_UnknownTypesReturnedForLogging(t *testing.T) {
	bundle := forecastWarningsBundle{
		Bulletins: []forecastWarningBulletin{
			{Category: "wind", Sections: []forecastWarningSection{{WarningType: "Marine Weather Statement"}}},
		},
	}

	reading, unknown := forecastWarningsReadingFrom(bundle)
	if reading.WindLevel != 1 {
		t.Fatalf("expected the floor of the ladder (1) for an unrecognised type, got %d", reading.WindLevel)
	}
	if len(unknown) != 1 || unknown[0] != "Marine Weather Statement" {
		t.Fatalf("expected the unrecognised type reported for logging, got %v", unknown)
	}
}

// --- forecastWarningsFetcher: the background fetch loop ---

func TestForecastWarningsFetcher_StoresReadingAtFetchTime(t *testing.T) {
	slot := &forecastWarningsSlot{}
	provider := &stubForecastWarningsProvider{
		id: "bom", name: "BOM", ttl: 5400,
		bundle: forecastWarningsBundle{
			Bulletins: []forecastWarningBulletin{{Category: "wind", Sections: []forecastWarningSection{{WarningType: "Gale Warning"}}}},
		},
	}
	fetcher := &forecastWarningsFetcher{
		slot:     slot,
		interval: forecastWarningsFetchInterval,
		resolve:  func() (forecastWarningsProvider, string, error) { return provider, "bom", nil },
		position: func() (float64, float64, bool) { return -27.4, 153.0, true },
	}

	now := time.Now().UTC()
	fetcher.check(now)

	reading, ok := slot.get()
	if !ok {
		t.Fatal("expected a reading in the slot")
	}
	if reading.WindLevel != 2 {
		t.Fatalf("expected wind level 2 (gale), got %d", reading.WindLevel)
	}
	if !reading.FetchedAt.Equal(now) {
		t.Fatalf("expected FetchedAt to be the fetch's own now, got %v want %v", reading.FetchedAt, now)
	}
}

// No masking fallback: a failed fetch keeps the previous reading and its OLD
// timestamp, so the derived path ages out and goes absent on its own rather
// than a default value papering over the failure.
func TestForecastWarningsFetcher_KeepsPreviousReadingOnProviderError(t *testing.T) {
	slot := &forecastWarningsSlot{}
	firstNow := time.Now().UTC()
	slot.set(forecastWarningsReading{WindLevel: 1, FetchedAt: firstNow})

	provider := &stubForecastWarningsProvider{id: "bom", name: "BOM", ttl: 5400, err: fmt.Errorf("simulated upstream failure")}
	fetcher := &forecastWarningsFetcher{
		slot:     slot,
		interval: forecastWarningsFetchInterval,
		resolve:  func() (forecastWarningsProvider, string, error) { return provider, "bom", nil },
		position: func() (float64, float64, bool) { return -27.4, 153.0, true },
	}

	later := firstNow.Add(time.Minute)
	fetcher.check(later)

	reading, ok := slot.get()
	if !ok {
		t.Fatal("expected the previous reading to survive a failed fetch")
	}
	if reading.WindLevel != 1 || !reading.FetchedAt.Equal(firstNow) {
		t.Fatalf("expected the OLD reading and its OLD timestamp preserved, got %+v", reading)
	}
}

// The WASM adapter's stale-on-error fallback returns err == nil, Cached:
// true, with an old CachedAt when the upstream is dead - a bundle only that
// branch can produce. Treating it as a live value would mean a dead BOM
// never looks stale; it has to be treated as a fetch failure instead.
func TestForecastWarningsFetcher_TreatsStaleServedBundleAsFailure(t *testing.T) {
	slot := &forecastWarningsSlot{}
	firstNow := time.Now().UTC()
	slot.set(forecastWarningsReading{WindLevel: 1, FetchedAt: firstNow})

	provider := &stubForecastWarningsProvider{
		id: "bom", name: "BOM", ttl: 5400, // 90-minute TTL
		bundle: forecastWarningsBundle{
			Cached:   true,
			CachedAt: firstNow.Add(-3 * time.Hour), // well past the 90-minute TTL
			Bulletins: []forecastWarningBulletin{
				{Category: "wind", Sections: []forecastWarningSection{{WarningType: "Gale Warning"}}},
			},
		},
	}
	fetcher := &forecastWarningsFetcher{
		slot:     slot,
		interval: forecastWarningsFetchInterval,
		resolve:  func() (forecastWarningsProvider, string, error) { return provider, "bom", nil },
		position: func() (float64, float64, bool) { return -27.4, 153.0, true },
	}

	later := firstNow.Add(time.Minute)
	fetcher.check(later)

	reading, ok := slot.get()
	if !ok {
		t.Fatal("expected the previous reading preserved")
	}
	if reading.WindLevel != 1 || !reading.FetchedAt.Equal(firstNow) {
		t.Fatalf("a stale-served bundle (older than the provider's own TTL) must be treated as a fetch failure, not a live gale reading: got %+v", reading)
	}
}

// A freshly booted install with no plugin registered must not panic, and
// must schedule its next attempt rather than spinning.
func TestForecastWarningsFetcher_NoProviderLeavesSlotEmptyAndSchedulesNextFetch(t *testing.T) {
	slot := &forecastWarningsSlot{}
	fetcher := &forecastWarningsFetcher{
		slot:     slot,
		interval: forecastWarningsFetchInterval,
		resolve: func() (forecastWarningsProvider, string, error) {
			return nil, "bom", fmt.Errorf("unknown forecast warnings provider configured: %q", "bom")
		},
		position: func() (float64, float64, bool) { return -27.4, 153.0, true },
	}

	now := time.Now().UTC()
	fetcher.check(now)

	if _, ok := slot.get(); ok {
		t.Fatal("expected the slot to stay empty with no provider registered")
	}
	if !fetcher.nextDue.Equal(now.Add(forecastWarningsFetchInterval)) {
		t.Fatalf("expected the next attempt scheduled a full interval out, got %v", fetcher.nextDue)
	}
}

func TestForecastWarningsFetcher_WaitsForPositionWith30sRetry(t *testing.T) {
	slot := &forecastWarningsSlot{}
	fetcher := &forecastWarningsFetcher{
		slot:     slot,
		interval: forecastWarningsFetchInterval,
		resolve: func() (forecastWarningsProvider, string, error) {
			t.Fatal("must not resolve a provider before a usable position exists")
			return nil, "", nil
		},
		position: func() (float64, float64, bool) { return 0, 0, false },
	}

	now := time.Now().UTC()
	fetcher.check(now)

	want := now.Add(forecastWarningsCheckInterval)
	if !fetcher.nextDue.Equal(want) {
		t.Fatalf("expected a 30s retry while waiting for a position, got next due %v want %v", fetcher.nextDue, want)
	}
	if _, ok := slot.get(); ok {
		t.Fatal("expected no reading before a position is available")
	}
}

func TestForecastWarningsFetcher_HonoursIntervalBetweenFetches(t *testing.T) {
	slot := &forecastWarningsSlot{}
	calls := 0
	provider := &stubForecastWarningsProvider{id: "bom", name: "BOM", ttl: 5400}
	fetcher := &forecastWarningsFetcher{
		slot:     slot,
		interval: forecastWarningsFetchInterval,
		resolve: func() (forecastWarningsProvider, string, error) {
			calls++
			return provider, "bom", nil
		},
		position: func() (float64, float64, bool) { return -27.4, 153.0, true },
	}

	now := time.Now().UTC()
	fetcher.check(now)
	if calls != 1 {
		t.Fatalf("expected the first check to fetch immediately, got %d calls", calls)
	}

	fetcher.check(now.Add(5 * time.Minute))
	if calls != 1 {
		t.Fatalf("expected no fetch before the interval elapses, got %d calls", calls)
	}

	fetcher.check(now.Add(forecastWarningsFetchInterval).Add(time.Second))
	if calls != 2 {
		t.Fatalf("expected a fetch once the interval elapsed, got %d calls", calls)
	}
}

// --- engine-level: fire on gale and hold when the fetch goes stale (step 3) ---

// This is the integration point the whole plan is for: a reading in the
// slot has to reach the alarm engine through derivedAwareAlarmReader and the
// seeded rules, and a dead fetcher has to hold the alarm rather than
// silently clearing it.
func TestForecastWarningsEngine_FiresOnGaleAndHoldsWhenTheFetchGoesStale(t *testing.T) {
	slot := globalForecastWarningsSlot
	slot.clear()
	t.Cleanup(func() { slot.clear() })

	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")
	withGlobalSnapshot(t, snapshot)

	// now is a synthetic clock for the engine's own dwell/SinceTrue
	// bookkeeping, advanced in fixed offsets below. derivedAwareAlarmReader
	// does not take a "now" parameter and always reads the real wall clock
	// (matching every other caller of computeDerivedPaths), so the slot's
	// FetchedAt is set relative to the ACTUAL time.Now() right before each
	// read, kept in step with the engine's synthetic offsets.
	now := time.Now().UTC()
	slot.set(forecastWarningsReading{WindLevel: 2, Surf: false, FetchedAt: time.Now().UTC()})

	engine := newAlarmEngine()
	rules := forecastWarningsSeedRules()
	read := derivedAwareAlarmReader(snapshot)

	raisedLabels := func(events []alarmEvent, kind string) map[string]bool {
		out := map[string]bool{}
		for _, event := range events {
			if event.Kind == kind {
				out[event.Rule.Label] = true
			}
		}
		return out
	}

	raised := raisedLabels(engine.evaluate(rules, read, now), alarmEventRaised)
	if !raised["Forecast wind warning"] {
		t.Fatal("expected the wind-warning rule to raise on a gale")
	}
	if !raised["Forecast gale or storm warning"] {
		t.Fatal("expected the gale rule to raise on a gale")
	}
	if raised["Forecast surf warning"] {
		t.Fatal("did not expect the surf rule to raise with no surf warning in the slot")
	}

	// Move the slot 40 minutes stale, well past forecastWarningsMaxAge (30
	// minutes), so the derived path itself goes absent. This is also the
	// instant the unavailable rule's condition first goes true, so its own
	// 120s dwell has not elapsed yet - it must not raise on this tick.
	slot.set(forecastWarningsReading{WindLevel: 2, Surf: false, FetchedAt: time.Now().UTC().Add(-40 * time.Minute)})
	at40 := now.Add(40 * time.Minute)
	eventsAt40 := engine.evaluate(rules, read, at40)
	for _, event := range eventsAt40 {
		if event.Kind == alarmEventCleared && (event.Rule.Label == "Forecast wind warning" || event.Rule.Label == "Forecast gale or storm warning") {
			t.Fatalf("a stale fetch must hold %q, not clear it: no data is not the same fact as the sea calming down", event.Rule.Label)
		}
		if event.Kind == alarmEventRaised && event.Rule.Label == "Forecast warnings unavailable" {
			t.Fatal("did not expect the unavailable rule to raise before its dwell elapsed")
		}
	}

	// Two minutes later, comfortably past the 120s dwell, it raises.
	slot.set(forecastWarningsReading{WindLevel: 2, Surf: false, FetchedAt: time.Now().UTC().Add(-42 * time.Minute)})
	at42 := now.Add(42 * time.Minute)
	raisedAt42 := raisedLabels(engine.evaluate(rules, read, at42), alarmEventRaised)
	if !raisedAt42["Forecast warnings unavailable"] {
		t.Fatal("expected the unavailable rule to raise once its 120s dwell elapsed")
	}
	if raisedAt42["Forecast wind warning"] || raisedAt42["Forecast gale or storm warning"] {
		t.Fatal("the wind alarms were already active; a held alarm must not re-raise")
	}
}
