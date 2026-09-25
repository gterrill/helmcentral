package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// capturingWeatherProvider is a local weatherProvider test double (like
// stubWeatherProvider in weather_providers_test.go) that additionally
// records the days argument it was called with, so get_wind_forecast tests
// can assert the clamped/default day count actually reaches the provider.
type capturingWeatherProvider struct {
	bundle weatherForecastBundle
	err    error

	gotDays int
	gotTZ   string
}

func (c *capturingWeatherProvider) ID() string   { return "capturing-weather" }
func (c *capturingWeatherProvider) Name() string { return "Capturing Weather" }
func (c *capturingWeatherProvider) Description() string {
	return "Capturing weather provider for tests"
}
func (c *capturingWeatherProvider) TTLSeconds() int64 { return 900 }
func (c *capturingWeatherProvider) FetchForecast(lat, lon float64, days int, timezone string) (weatherForecastBundle, error) {
	c.gotDays = days
	c.gotTZ = timezone
	if c.err != nil {
		return weatherForecastBundle{}, c.err
	}
	return c.bundle, nil
}

// ── fake place-names provider for find_places ──────────────────────────────

// fakeSearchPlacesProvider is an injectable placeNameProvider for
// find_places tests (ADR 0101: find_places calls whatever place-names
// provider is configured, never a backend Overpass client of its own).
// executeFindPlaces never calls PlaceNameAt itself (that's place_name.go's
// concern), so it errors if exercised; SearchPlaces returns one canned
// placeSearchResult (or error) and records every call's input, so a test
// can assert exactly what executeFindPlaces asked for - in particular the
// Broad flag, which is how the host tells the provider whether a saved
// route waypoint already answered the query.
type fakeSearchPlacesProvider struct {
	id string

	mu     sync.Mutex
	calls  []placeSearchInput
	result placeSearchResult
	err    error
}

func (f *fakeSearchPlacesProvider) ID() string   { return f.id }
func (f *fakeSearchPlacesProvider) Name() string { return f.id }

func (f *fakeSearchPlacesProvider) PlaceNameAt(lat, lon float64, radiusM int) (placeNameResult, error) {
	return placeNameResult{}, fmt.Errorf("fakeSearchPlacesProvider %q: PlaceNameAt not exercised by find_places", f.id)
}

func (f *fakeSearchPlacesProvider) SearchPlaces(input placeSearchInput) (placeSearchResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, input)
	f.mu.Unlock()
	if f.err != nil {
		return placeSearchResult{}, f.err
	}
	return f.result, nil
}

func (f *fakeSearchPlacesProvider) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeSearchPlacesProvider) lastCall() placeSearchInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

// findPlacesDeps builds the minimal assistantToolDeps find_places needs: a
// fixed vessel position, the given fake provider wired as the configured
// place-names provider, and the given routes.
func findPlacesDeps(vesselLat, vesselLon float64, provider placeNameProvider, routes func() []routeData) assistantToolDeps {
	return assistantToolDeps{
		vesselState: func() (vesselStateData, error) {
			return vesselStateData{Latitude: vesselLat, Longitude: vesselLon}, nil
		},
		placeNames: func() (placeNameProvider, string, error) { return provider, provider.ID(), nil },
		routes:     routes,
	}
}

// ── find_places ─────────────────────────────────────────────────────────

func TestExecuteFindPlaces_MergesRouteWaypointDedupesAndSorts(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10

	// The provider's own copy of the same Tongue Bay, ~110m from the
	// waypoint - well inside the 500m dedupe radius, so it must collapse
	// into the waypoint entry (added first) rather than appearing a second
	// time.
	nearDupLat, nearDupLon := vesselLat+0.001, vesselLon

	// A second, distinct real-world place that happens to share the exact
	// name "Tongue Bay" (duplicate place names are common enough), ~11km
	// (~6nm) away, so sort-by-distance has something to prove.
	farLat, farLon := vesselLat+0.1, vesselLon

	provider := &fakeSearchPlacesProvider{id: "fake-places", result: placeSearchResult{
		Search:   "exact",
		RadiusNm: 100,
		Results: []placeSearchMatch{
			{Name: "Tongue Bay", Kind: "bay", Lat: nearDupLat, Lon: nearDupLon},
			{Name: "Tongue Bay", Kind: "anchorage", Lat: farLat, Lon: farLon},
		},
	}}
	routes := func() []routeData {
		return []routeData{{
			Name: "Whitsundays Loop",
			Waypoints: []routeWaypoint{
				{Name: "Tongue Bay", Lat: vesselLat, Lon: vesselLon},
			},
		}}
	}
	deps := findPlacesDeps(vesselLat, vesselLon, provider, routes)

	// Query exactly matches the waypoint name, so it's a waypoint hit -
	// suppressing the provider's own broader rung (broad=false) - but the
	// provider's cheap exact-name search still always runs.
	raw, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Tongue Bay"}`))
	if err != nil {
		t.Fatalf("execute find_places: %v", err)
	}
	if provider.callCount() != 1 {
		t.Fatalf("expected exactly one search_places call, got %d", provider.callCount())
	}
	if provider.lastCall().Broad {
		t.Errorf("expected broad=false: the query already matched a saved waypoint")
	}

	var result assistantFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v (raw: %s)", err, raw)
	}

	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results (the near duplicate deduped, the far one distinct), got %d: %+v", len(result.Results), result.Results)
	}
	if result.Results[0].Name != "Tongue Bay" {
		t.Errorf("expected the nearer Tongue Bay first, got %q", result.Results[0].Name)
	}
	if result.Results[0].Source != "route:Whitsundays Loop" {
		t.Errorf("expected the route waypoint to win the dedupe (added first), got source %q", result.Results[0].Source)
	}
	if result.Results[0].DistanceNm != 0 {
		t.Errorf("expected the waypoint's distance to be ~0nm, got %v", result.Results[0].DistanceNm)
	}
	if result.Results[1].DistanceNm <= result.Results[0].DistanceNm {
		t.Errorf("expected the far result to be sorted after the near one")
	}
	if result.Results[1].BearingDeg < 0 || result.Results[1].BearingDeg > 360 {
		t.Errorf("expected a valid compass bearing, got %d", result.Results[1].BearingDeg)
	}
	if result.Results[1].Source != "fake-places" {
		t.Errorf("expected the far, non-waypoint result's source to be the provider id, got %q", result.Results[1].Source)
	}
	if result.Results[1].Kind != "anchorage" {
		t.Errorf("expected kind=anchorage as returned by the provider, got %q", result.Results[1].Kind)
	}
	if result.Search != "waypoints" {
		t.Errorf("expected search=waypoints, got %q", result.Search)
	}
	if result.RadiusNm != 100 {
		t.Errorf("expected radius_nm=100 (as returned by the provider), got %v", result.RadiusNm)
	}
}

func TestExecuteFindPlaces_NoWaypointHitRunsBroadSearch(t *testing.T) {
	vesselLat, vesselLon := -20.1, 149.1
	provider := &fakeSearchPlacesProvider{id: "fake-places", result: placeSearchResult{
		Search:   "exact",
		RadiusNm: 100,
		Results:  []placeSearchMatch{{Name: "Shag Cove", Kind: "bay", Lat: -20.11, Lon: 149.1}},
	}}
	deps := findPlacesDeps(vesselLat, vesselLon, provider, func() []routeData { return nil })

	raw, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Shag Cove"}`))
	if err != nil {
		t.Fatalf("execute find_places: %v", err)
	}
	if provider.callCount() != 1 {
		t.Fatalf("expected exactly one search_places call, got %d", provider.callCount())
	}
	if !provider.lastCall().Broad {
		t.Errorf("expected broad=true: no saved waypoint matched the query")
	}

	var result assistantFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	if len(result.Results) != 1 || result.Results[0].Name != "Shag Cove" {
		t.Fatalf("expected the provider's hit, got %+v", result.Results)
	}
	if result.Results[0].Kind != "bay" {
		t.Errorf("expected kind=bay as returned by the provider, got %q", result.Results[0].Kind)
	}
	if result.Search != "exact" {
		t.Errorf("expected search=exact, got %q", result.Search)
	}
}

func TestExecuteFindPlaces_WaypointHitStillIncludesProviderResults(t *testing.T) {
	vesselLat, vesselLon := -20.1, 149.1
	provider := &fakeSearchPlacesProvider{id: "fake-places", result: placeSearchResult{Search: "none", RadiusNm: 100}}
	routes := func() []routeData {
		return []routeData{{
			Name: "Whitsundays Loop",
			Waypoints: []routeWaypoint{
				{Name: "Tongue Bay", Lat: vesselLat, Lon: vesselLon},
			},
		}}
	}
	deps := findPlacesDeps(vesselLat, vesselLon, provider, routes)

	raw, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Tongue"}`))
	if err != nil {
		t.Fatalf("execute find_places: %v", err)
	}
	if provider.callCount() != 1 {
		t.Fatalf("expected the provider's exact-name rung to still run even after a waypoint hit, got %d calls", provider.callCount())
	}
	if provider.lastCall().Broad {
		t.Errorf("expected broad=false: the query already matched a saved waypoint")
	}

	var result assistantFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	if len(result.Results) != 1 || result.Results[0].Source != "route:Whitsundays Loop" {
		t.Fatalf("expected only the waypoint hit, got %+v", result.Results)
	}
	if result.Search != "waypoints" {
		t.Errorf("expected search=waypoints, got %q", result.Search)
	}
}

func TestExecuteFindPlaces_NoteMentionsRadiusWhenNothingFound(t *testing.T) {
	provider := &fakeSearchPlacesProvider{id: "fake-places", result: placeSearchResult{Search: "none", RadiusNm: 20}}
	deps := findPlacesDeps(-20.1, 149.1, provider, func() []routeData { return nil })

	raw, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Shag Cove"}`))
	if err != nil {
		t.Fatalf("execute find_places: %v", err)
	}
	var result assistantFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	want := `no named feature matching "Shag Cove" found within 20 nm of the search centre`
	if result.Note != want {
		t.Errorf("note = %q, want %q", result.Note, want)
	}
	if result.Search != "none" {
		t.Errorf("expected search=none, got %q", result.Search)
	}
}

func TestExecuteFindPlaces_ProviderNoteIsPreservedEvenWithResults(t *testing.T) {
	provider := &fakeSearchPlacesProvider{id: "fake-places", result: placeSearchResult{
		Search:   "regex",
		RadiusNm: 20,
		Results:  []placeSearchMatch{{Name: "Tongue Point", Kind: "cape", Lat: -20.15, Lon: 149.12}},
		Note:     "widened past the usual radius",
	}}
	deps := findPlacesDeps(-20.1, 149.1, provider, func() []routeData { return nil })

	raw, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Tongue"}`))
	if err != nil {
		t.Fatalf("execute find_places: %v", err)
	}
	var result assistantFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	if result.Note != "widened past the usual radius" {
		t.Errorf("expected the provider's own note to pass through untouched, got %q", result.Note)
	}
}

func TestExecuteFindPlaces_ProviderErrorIsNeverSwallowed(t *testing.T) {
	provider := &fakeSearchPlacesProvider{id: "fake-places", err: fmt.Errorf("network unreachable")}
	deps := findPlacesDeps(-20.1, 149.1, provider, func() []routeData { return nil })

	_, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Bay"}`))
	if err == nil {
		t.Fatalf("expected the provider's search_places error to surface as an error, not be swallowed")
	}
}

// ── query bounding (M-4: query reaches a third-party host verbatim) ──────
//
// find_places.query is the only text besides openrouter.ai itself that an
// injected document can get the model to send somewhere else - the
// configured place-names provider forwards it to Overpass or Google Places
// (ADR 0101). Both tests below check the query never reaches
// provider.SearchPlaces at all once it is too long or carries characters a
// place name search has no business containing.

// TestExecuteFindPlaces_OverlongQueryRejectedBeforeReachingTheProvider
// checks a query far longer than any real place name (built to look like
// an attempt to smuggle other data through it - a coordinate pair and
// filler text) is rejected outright, and never reaches the provider.
func TestExecuteFindPlaces_OverlongQueryRejectedBeforeReachingTheProvider(t *testing.T) {
	provider := &fakeSearchPlacesProvider{id: "fake-places", result: placeSearchResult{Search: "exact"}}
	deps := findPlacesDeps(-20.1, 149.1, provider, func() []routeData { return nil })

	longQuery := "Bona Bay " + strings.Repeat("smuggled anchorage name filler text ", 5)
	if len(longQuery) <= assistantFindPlacesMaxQueryRunes {
		t.Fatalf("test fixture bug: longQuery (%d runes) must exceed assistantFindPlacesMaxQueryRunes (%d)", len(longQuery), assistantFindPlacesMaxQueryRunes)
	}

	raw, err := json.Marshal(map[string]string{"query": longQuery})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	_, err = deps.execute(context.Background(), "find_places", raw)
	if err == nil {
		t.Fatalf("expected an overlong query to be rejected")
	}
	if !strings.Contains(err.Error(), "find_places") {
		t.Fatalf("expected the error to name find_places, got: %v", err)
	}
	if provider.callCount() != 0 {
		t.Fatalf("expected the overlong query to never reach the provider, got %d calls", provider.callCount())
	}
}

// TestExecuteFindPlaces_QueryWithDisallowedCharactersRejected checks a
// query carrying characters no real place name search needs (here, angle
// brackets and a semicolon - the kind of bytes an injected document might
// ask the model to encode) is rejected outright rather than passed through
// to the provider.
func TestExecuteFindPlaces_QueryWithDisallowedCharactersRejected(t *testing.T) {
	provider := &fakeSearchPlacesProvider{id: "fake-places", result: placeSearchResult{Search: "exact"}}
	deps := findPlacesDeps(-20.1, 149.1, provider, func() []routeData { return nil })

	raw, err := json.Marshal(map[string]string{"query": "Bay <script>alert(1)</script>; lat=-20.1"})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	_, err = deps.execute(context.Background(), "find_places", raw)
	if err == nil {
		t.Fatalf("expected a query with disallowed characters to be rejected")
	}
	if provider.callCount() != 0 {
		t.Fatalf("expected the invalid query to never reach the provider, got %d calls", provider.callCount())
	}
}

// TestExecuteFindPlaces_OrdinaryCompoundQueryStillAccepted is the negative
// check alongside the two tests above: the "Name, Qualifier" query form the
// system prompt's own rung-2 fallback relies on (assistant_prompt.go) must
// still pass the new bounds - this used to be a plain end-to-end assertion
// in TestExecuteFindPlaces_CentredOnEchoesProviderQualifierHit, but that
// test doesn't isolate query validation from the rest of the qualifier-
// resolution behaviour the way this one does.
func TestExecuteFindPlaces_OrdinaryCompoundQueryStillAccepted(t *testing.T) {
	provider := &fakeSearchPlacesProvider{id: "fake-places", result: placeSearchResult{Search: "exact"}}
	deps := findPlacesDeps(-20.1, 149.1, provider, func() []routeData { return nil })

	_, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Bona Bay, Gloucester Island"}`))
	if err != nil {
		t.Fatalf("expected the ordinary comma-qualified query to be accepted, got: %v", err)
	}
	if provider.callCount() != 1 {
		t.Fatalf("expected the query to reach the provider, got %d calls", provider.callCount())
	}
}

// A resolver failure - the configured place-names provider isn't
// installed, or doesn't support place names - must surface as a
// find_places error naming the problem, never silently return an empty
// result (AGENTS.md's fail-fast / no-masking-fallback policy).
func TestExecuteFindPlaces_ProviderResolutionErrorSurfaces(t *testing.T) {
	deps := assistantToolDeps{
		vesselState: func() (vesselStateData, error) { return vesselStateData{Latitude: -20.1, Longitude: 149.1}, nil },
		placeNames: func() (placeNameProvider, string, error) {
			return nil, "", fmt.Errorf("unknown place-names provider configured: %q", "no-such-plugin")
		},
		routes: func() []routeData { return nil },
	}

	_, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Bay"}`))
	if err == nil {
		t.Fatalf("expected a provider-resolution failure to surface as an error")
	}
	if !strings.Contains(err.Error(), "no-such-plugin") {
		t.Fatalf("expected the error to name the misconfigured provider, got: %v", err)
	}
}

func TestExecuteFindPlaces_CentredOnEchoesProviderQualifierHit(t *testing.T) {
	vesselLat, vesselLon := -20.1, 149.1
	qualifierLat, qualifierLon := -19.8, 148.5 // "Gloucester Island", well outside the vessel's own vicinity
	provider := &fakeSearchPlacesProvider{id: "fake-places", result: placeSearchResult{
		Search:    "regex",
		RadiusNm:  20,
		CentredOn: &placeSearchCentre{Name: "Gloucester Island", Lat: qualifierLat, Lon: qualifierLon},
		Results:   []placeSearchMatch{{Name: "Bona Bay", Kind: "bay", Lat: qualifierLat, Lon: qualifierLon}},
	}}
	deps := findPlacesDeps(vesselLat, vesselLon, provider, func() []routeData { return nil })

	raw, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Bona Bay, Gloucester Island"}`))
	if err != nil {
		t.Fatalf("execute find_places: %v", err)
	}
	var result assistantFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	if result.CentredOn != "Gloucester Island" {
		t.Errorf("expected centred_on=%q, got %q", "Gloucester Island", result.CentredOn)
	}
	if result.Centre == nil || result.Centre.Lat != qualifierLat || result.Centre.Lon != qualifierLon {
		t.Errorf("expected centre to be the provider's qualifier hit (%v,%v), got %+v", qualifierLat, qualifierLon, result.Centre)
	}
}

func TestExecuteFindPlaces_NoCentredOnKeepsVesselCentre(t *testing.T) {
	vesselLat, vesselLon := -20.1, 149.1
	provider := &fakeSearchPlacesProvider{id: "fake-places", result: placeSearchResult{Search: "none", RadiusNm: 20}}
	deps := findPlacesDeps(vesselLat, vesselLon, provider, func() []routeData { return nil })

	raw, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Bona Bay, Gloucester Island"}`))
	if err != nil {
		t.Fatalf("execute find_places: %v", err)
	}
	var result assistantFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	if result.CentredOn != "" {
		t.Errorf("expected no centred_on when the provider doesn't report one, got %q", result.CentredOn)
	}
	if result.Centre == nil || result.Centre.Lat != vesselLat || result.Centre.Lon != vesselLon {
		t.Errorf("expected centre to remain the vessel position, got %+v", result.Centre)
	}
}

func TestExecuteFindPlaces_NoFixAndNoNearArgsReturnsNoteNotError(t *testing.T) {
	provider := &fakeSearchPlacesProvider{id: "fake-places"}
	deps := assistantToolDeps{
		vesselState: func() (vesselStateData, error) { return vesselStateData{Latitude: -1, Longitude: -1}, nil },
		placeNames:  func() (placeNameProvider, string, error) { return provider, provider.ID(), nil },
		routes:      func() []routeData { return nil },
	}

	raw, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Tongue Bay"}`))
	if err != nil {
		t.Fatalf("execute find_places: %v", err)
	}

	var result assistantFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.Results) != 0 {
		t.Fatalf("expected an empty results array, got %+v", result.Results)
	}
	if !strings.Contains(result.Note, "vessel position unknown") {
		t.Fatalf("expected a note about unknown vessel position, got %q", result.Note)
	}
	if provider.callCount() != 0 {
		t.Fatalf("expected no provider call when there is no usable fix, got %d", provider.callCount())
	}
}

// ── get_wind_forecast ───────────────────────────────────────────────────

func TestExecuteGetWindForecast_RoundsFiltersNullsAndJoinsWaves(t *testing.T) {
	// now truncates to 2026-09-12T00:00Z; the vessel's local zone at
	// lon=149.1 is UTC+10, so local hours run 10 hours ahead of these UTC
	// timestamps.
	now := time.Date(2026, 9, 12, 0, 30, 0, 0, time.UTC)
	lat, lon := -20.1, 149.1

	weatherStub := &capturingWeatherProvider{
		bundle: weatherForecastBundle{
			Hourly: []weatherHourPoint{
				// 2026-09-11T20:00Z -> local 06:00, %3==0, but before nowHour: dropped.
				{Time: time.Date(2026, 9, 11, 20, 0, 0, 0, time.UTC), WindSpeedMS: 5.0, WindGustMS: 6.0, WindDirectionDeg: 135},
				// local 10:00 and 11:00, not on a 3-hour boundary: dropped.
				{Time: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC), WindSpeedMS: 5.0},
				{Time: time.Date(2026, 9, 12, 1, 0, 0, 0, time.UTC), WindSpeedMS: 5.0},
				// local 12:00, kept: the all-zero sentinel row.
				{Time: time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC)},
				// local 15:00, kept: real wind, joined to a wave hour below.
				{Time: time.Date(2026, 9, 12, 5, 0, 0, 0, time.UTC), WindSpeedMS: 5.0, WindGustMS: 6.0, WindDirectionDeg: 135},
			},
			Cached:   true,
			CachedAt: now,
		},
	}
	waveStub := &stubWaveProvider{
		id: "open-meteo-marine",
		bundle: waveForecastBundle{
			Hourly: []waveHourPoint{
				{Time: time.Date(2026, 9, 12, 5, 0, 0, 0, time.UTC), WaveHeightM: 0.8, WavePeriodS: 6, WaveDirectionDeg: 90},
			},
		},
	}

	deps := assistantToolDeps{
		now:     func() time.Time { return now },
		weather: func() (weatherProvider, string, error) { return weatherStub, "capturing-weather", nil },
		waves:   func() (waveProvider, string, error) { return waveStub, "open-meteo-marine", nil },
	}

	raw, err := deps.execute(context.Background(), "get_wind_forecast", json.RawMessage(fmt.Sprintf(`{"lat":%f,"lon":%f,"days":2}`, lat, lon)))
	if err != nil {
		t.Fatalf("execute get_wind_forecast: %v", err)
	}

	var result assistantWindForecastResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}

	wantTZ := vesselLocalTimezoneName(lon)
	if result.Timezone != wantTZ {
		t.Errorf("expected timezone %q, got %q", wantTZ, result.Timezone)
	}
	if weatherStub.gotTZ != wantTZ {
		t.Errorf("expected the provider to be called with timezone %q, got %q", wantTZ, weatherStub.gotTZ)
	}
	if weatherStub.gotDays != 2 {
		t.Errorf("expected days=2 to pass straight through, got %d", weatherStub.gotDays)
	}

	if len(result.Hourly) != 2 {
		t.Fatalf("expected exactly 2 kept hourly rows (3-hourly, no past hours), got %d: %+v", len(result.Hourly), result.Hourly)
	}

	zeroRow := result.Hourly[0]
	if zeroRow.WindKts != nil || zeroRow.GustKts != nil || zeroRow.Dir != nil || zeroRow.DirDeg != nil {
		t.Errorf("expected the all-zero sentinel row to null every wind field, got %+v", zeroRow)
	}
	if zeroRow.WaveM != nil || zeroRow.WaveS != nil || zeroRow.WaveDir != nil {
		t.Errorf("expected no wave data for an hour with no matching wave timestamp, got %+v", zeroRow)
	}

	windyRow := result.Hourly[1]
	if windyRow.WindKts == nil || *windyRow.WindKts != 10 {
		t.Errorf("expected wind_kts=10 (5.0 m/s rounded), got %v", windyRow.WindKts)
	}
	if windyRow.GustKts == nil || *windyRow.GustKts != 12 {
		t.Errorf("expected gust_kts=12 (6.0 m/s rounded), got %v", windyRow.GustKts)
	}
	if windyRow.Dir == nil || *windyRow.Dir != "SE" {
		t.Errorf("expected dir=SE, got %v", windyRow.Dir)
	}
	if windyRow.DirDeg == nil || *windyRow.DirDeg != 135 {
		t.Errorf("expected dir_deg=135, got %v", windyRow.DirDeg)
	}
	if windyRow.WaveM == nil || *windyRow.WaveM != 0.8 {
		t.Errorf("expected wave_m=0.8 joined by unix timestamp, got %v", windyRow.WaveM)
	}
	if windyRow.WaveS == nil || *windyRow.WaveS != 6 {
		t.Errorf("expected wave_s=6, got %v", windyRow.WaveS)
	}
	if windyRow.WaveDir == nil || *windyRow.WaveDir != "E" {
		t.Errorf("expected wave_dir=E, got %v", windyRow.WaveDir)
	}

	if result.WaveProvider != "open-meteo-marine" {
		t.Errorf("expected wave_provider=open-meteo-marine, got %q", result.WaveProvider)
	}
	if result.WavesError != "" {
		t.Errorf("expected no waves_error on a successful wave fetch, got %q", result.WavesError)
	}
	if result.WeatherProvider != "capturing-weather" {
		t.Errorf("expected weather_provider=capturing-weather, got %q", result.WeatherProvider)
	}
	if !result.Cached {
		t.Errorf("expected cached=true to pass through from the bundle")
	}
}

func TestExecuteGetWindForecast_WaveErrorStillReturnsWind(t *testing.T) {
	now := time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC) // local 12:00 at lon 149.1, %3==0
	weatherStub := &capturingWeatherProvider{
		bundle: weatherForecastBundle{
			Hourly: []weatherHourPoint{
				{Time: now, WindSpeedMS: 5.0, WindGustMS: 6.0, WindDirectionDeg: 135},
			},
		},
	}
	deps := assistantToolDeps{
		now:     func() time.Time { return now },
		weather: func() (weatherProvider, string, error) { return weatherStub, "capturing-weather", nil },
		waves:   func() (waveProvider, string, error) { return nil, "", fmt.Errorf("no wave plugin installed") },
	}

	raw, err := deps.execute(context.Background(), "get_wind_forecast", json.RawMessage(`{"lat":-20.1,"lon":149.1}`))
	if err != nil {
		t.Fatalf("execute get_wind_forecast: %v", err)
	}
	var result assistantWindForecastResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.WavesError == "" {
		t.Fatalf("expected a waves_error when the wave provider fails to resolve")
	}
	if len(result.Hourly) != 1 || result.Hourly[0].WindKts == nil {
		t.Fatalf("expected wind data still present despite the wave failure: %+v", result)
	}
}

func TestExecuteGetWindForecast_ClampsDays(t *testing.T) {
	weatherStub := &capturingWeatherProvider{}
	deps := assistantToolDeps{
		now:     time.Now,
		weather: func() (weatherProvider, string, error) { return weatherStub, "capturing-weather", nil },
		waves:   func() (waveProvider, string, error) { return nil, "", fmt.Errorf("no wave plugin installed") },
	}

	cases := []struct {
		body string
		want int
	}{
		{`{"lat":-20.1,"lon":149.1}`, 3},
		{`{"lat":-20.1,"lon":149.1,"days":0}`, 3},
		{`{"lat":-20.1,"lon":149.1,"days":99}`, assistantMaxForecastDays},
		{`{"lat":-20.1,"lon":149.1,"days":5}`, 5},
	}
	for _, tc := range cases {
		if _, err := deps.execute(context.Background(), "get_wind_forecast", json.RawMessage(tc.body)); err != nil {
			t.Fatalf("execute(%s): %v", tc.body, err)
		}
		if weatherStub.gotDays != tc.want {
			t.Errorf("body %s: expected clamped days=%d, got %d", tc.body, tc.want, weatherStub.gotDays)
		}
	}
}

func TestExecuteGetWindForecast_CourseDegAddsRelativeAngles(t *testing.T) {
	// Local time at lon 149.1 is UTC+10; both kept hours land on
	// 2026-09-12, a 3-hour boundary apart, so a single day summary covers
	// both.
	now := time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC) // local 12:00, %3==0
	lat, lon := -20.1, 149.1
	courseDeg := 303.0

	weatherStub := &capturingWeatherProvider{
		bundle: weatherForecastBundle{
			Hourly: []weatherHourPoint{
				// local 12:00: real wind, 168 degrees off a 303 degree
				// course - a following wind, not the "port beam to port
				// quarter" the model once called it.
				{Time: now, WindSpeedMS: 5.0, WindGustMS: 6.0, WindDirectionDeg: 135},
				// local 15:00: the all-zero sentinel row, wind direction
				// unknown - the relative fields must stay nil, not fold to
				// a fabricated "head".
				{Time: now.Add(3 * time.Hour)},
			},
		},
	}
	waveStub := &stubWaveProvider{
		id: "open-meteo-marine",
		bundle: waveForecastBundle{
			Hourly: []waveHourPoint{
				// 45 degrees (NE): forward of a 303 degree course's
				// quarter, not "on or abaft the beam".
				{Time: now, WaveHeightM: 1.0, WavePeriodS: 6, WaveDirectionDeg: 45},
			},
		},
	}

	deps := assistantToolDeps{
		now:     func() time.Time { return now },
		weather: func() (weatherProvider, string, error) { return weatherStub, "capturing-weather", nil },
		waves:   func() (waveProvider, string, error) { return waveStub, "open-meteo-marine", nil },
	}

	raw, err := deps.execute(context.Background(), "get_wind_forecast", json.RawMessage(
		fmt.Sprintf(`{"lat":%f,"lon":%f,"days":1,"course_deg":%f}`, lat, lon, courseDeg)))
	if err != nil {
		t.Fatalf("execute get_wind_forecast: %v", err)
	}

	var result assistantWindForecastResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}

	if result.CourseDeg == nil || *result.CourseDeg != courseDeg {
		t.Fatalf("expected course_deg=%g echoed in the header, got %v", courseDeg, result.CourseDeg)
	}

	if len(result.Hourly) != 2 {
		t.Fatalf("expected 2 kept hourly rows, got %d: %+v", len(result.Hourly), result.Hourly)
	}

	windyRow := result.Hourly[0]
	if windyRow.RelWindDeg == nil || *windyRow.RelWindDeg != 168 {
		t.Errorf("expected rel_wind_deg=168, got %v", windyRow.RelWindDeg)
	}
	if windyRow.RelWind == nil || *windyRow.RelWind != "following" {
		t.Errorf("expected rel_wind=following, got %v", windyRow.RelWind)
	}
	if windyRow.RelWaveDeg == nil || *windyRow.RelWaveDeg != 102 {
		t.Errorf("expected rel_wave_deg=102, got %v", windyRow.RelWaveDeg)
	}
	if windyRow.RelWave == nil || *windyRow.RelWave != "quarter" {
		t.Errorf("expected rel_wave=quarter, got %v", windyRow.RelWave)
	}

	sentinelRow := result.Hourly[1]
	if sentinelRow.RelWindDeg != nil || sentinelRow.RelWind != nil {
		t.Errorf("expected nil rel_wind fields when wind direction is unknown, got %+v", sentinelRow)
	}
	if sentinelRow.RelWaveDeg != nil || sentinelRow.RelWave != nil {
		t.Errorf("expected nil rel_wave fields when there is no matching wave row, got %+v", sentinelRow)
	}

	if len(result.Days) != 1 {
		t.Fatalf("expected 1 day summary, got %d: %+v", len(result.Days), result.Days)
	}
	if result.Days[0].RelWindPrevailing == nil || *result.Days[0].RelWindPrevailing != "following" {
		t.Errorf("expected rel_wind_prevailing=following, got %v", result.Days[0].RelWindPrevailing)
	}
}

func TestExecuteGetWindForecast_NoCourseDegOmitsRelativeAngles(t *testing.T) {
	now := time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC) // local 12:00, %3==0
	weatherStub := &capturingWeatherProvider{
		bundle: weatherForecastBundle{
			Hourly: []weatherHourPoint{
				{Time: now, WindSpeedMS: 5.0, WindGustMS: 6.0, WindDirectionDeg: 135},
			},
		},
	}
	deps := assistantToolDeps{
		now:     func() time.Time { return now },
		weather: func() (weatherProvider, string, error) { return weatherStub, "capturing-weather", nil },
		waves:   func() (waveProvider, string, error) { return nil, "", fmt.Errorf("no wave plugin installed") },
	}

	raw, err := deps.execute(context.Background(), "get_wind_forecast", json.RawMessage(`{"lat":-20.1,"lon":149.1}`))
	if err != nil {
		t.Fatalf("execute get_wind_forecast: %v", err)
	}

	// course_deg was never given, so the output must be byte-for-byte what
	// it was before this field existed: no rel_wind/rel_wave keys at all,
	// not even a null one.
	for _, notWant := range []string{"rel_wind", "rel_wave", "course_deg"} {
		if strings.Contains(raw, notWant) {
			t.Errorf("expected no %q in the result when course_deg is omitted, got %s", notWant, raw)
		}
	}

	var result assistantWindForecastResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.CourseDeg != nil {
		t.Errorf("expected course_deg=nil in the header, got %v", *result.CourseDeg)
	}
	if len(result.Hourly) != 1 || result.Hourly[0].RelWind != nil {
		t.Errorf("expected rel_wind=nil on the row, got %+v", result.Hourly[0])
	}
}

// ── get_tides ───────────────────────────────────────────────────────────

func TestExecuteGetTides_NearestStationWindowedExtremesAustraliaBrisbane(t *testing.T) {
	now := time.Date(2026, 9, 12, 3, 0, 0, 0, time.UTC)

	brisbane := tideStation{StationID: "BRIS", Name: "Brisbane Bar", State: "QLD", Lat: -27.36, Lon: 153.17, Timezone: "Australia/Brisbane"}
	farAway := tideStation{StationID: "FAR", Name: "Far Away Station", Lat: 10.0, Lon: 10.0, Timezone: "UTC"}

	// Australia/Brisbane is UTC+10 with no DST, so local midnight on
	// 2026-09-12 is 2026-09-11T14:00:00Z.
	localMidnightUTC := time.Date(2026, 9, 11, 14, 0, 0, 0, time.UTC)
	extremes := []tideExtremePoint{
		{Time: localMidnightUTC.Add(-1 * time.Hour), HeightM: 1.0, High: false}, // before the window: dropped
		{Time: localMidnightUTC.Add(5 * time.Hour), HeightM: 2.4, High: true},   // inside the 2-day window: kept
		{Time: localMidnightUTC.Add(48 * time.Hour), HeightM: 2.5, High: true},  // exactly at the window end: dropped
	}

	provider := &stubTideProvider{
		id:       "bom",
		stations: []tideStation{farAway, brisbane},
		result: tideChartResult{
			Extremes:       extremes,
			CurrentHeightM: 1.8,
			Direction:      "Rising",
			Cached:         true,
			CachedAt:       now,
		},
	}

	deps := assistantToolDeps{
		now:   func() time.Time { return now },
		tides: func() (tideProvider, string, error) { return provider, "bom", nil },
	}

	raw, err := deps.execute(context.Background(), "get_tides", json.RawMessage(`{"lat":-27.4,"lon":153.1,"days":2}`))
	if err != nil {
		t.Fatalf("execute get_tides: %v", err)
	}

	var result assistantGetTidesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}

	if result.Station.ID != "BRIS" {
		t.Fatalf("expected the nearest station (Brisbane), got %q", result.Station.ID)
	}
	if result.TimeBasis != "Australia/Brisbane" {
		t.Errorf("expected time_basis=Australia/Brisbane (proves tzdata resolves it), got %q", result.TimeBasis)
	}
	if len(result.Extremes) != 1 {
		t.Fatalf("expected exactly 1 extreme inside the 2-day window, got %d: %+v", len(result.Extremes), result.Extremes)
	}
	if result.Extremes[0].Type != "high" {
		t.Errorf("expected the kept extreme to be a high, got %q", result.Extremes[0].Type)
	}
	if result.Extremes[0].HeightM != 2.4 {
		t.Errorf("expected height_m=2.4, got %v", result.Extremes[0].HeightM)
	}
	if result.Now.HeightM != 1.8 || result.Now.Direction != "Rising" {
		t.Errorf("expected now to echo the current height/direction, got %+v", result.Now)
	}
	if result.Provider != "bom" {
		t.Errorf("expected provider=bom, got %q", result.Provider)
	}
	if !result.Cached {
		t.Errorf("expected cached=true to pass through")
	}
	if result.Note != "" {
		t.Errorf("expected no note when extremes are found, got %q", result.Note)
	}
}

func TestExecuteGetTides_NoStationsIsError(t *testing.T) {
	provider := &stubTideProvider{id: "bom", stations: nil}
	deps := assistantToolDeps{
		now:   time.Now,
		tides: func() (tideProvider, string, error) { return provider, "bom", nil },
	}

	_, err := deps.execute(context.Background(), "get_tides", json.RawMessage(`{"lat":-27.4,"lon":153.1}`))
	if err == nil {
		t.Fatalf("expected an error when the provider has no stations")
	}
	if !strings.Contains(err.Error(), "no stations") {
		t.Errorf("expected the error to mention no stations, got %q", err.Error())
	}
}

func TestExecuteGetTides_EmptyWindowAddsNote(t *testing.T) {
	now := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	station := tideStation{StationID: "S1", Name: "Somewhere", Lat: -27.0, Lon: 153.0, Timezone: "Australia/Brisbane"}
	provider := &stubTideProvider{
		id:       "bom",
		stations: []tideStation{station},
		result:   tideChartResult{Extremes: nil},
	}
	deps := assistantToolDeps{
		now:   func() time.Time { return now },
		tides: func() (tideProvider, string, error) { return provider, "bom", nil },
	}

	raw, err := deps.execute(context.Background(), "get_tides", json.RawMessage(`{"lat":-27.0,"lon":153.0}`))
	if err != nil {
		t.Fatalf("execute get_tides: %v", err)
	}
	var result assistantGetTidesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(result.Note, "BOM gives about 8 days") {
		t.Errorf("expected the empty-window note, got %q", result.Note)
	}
}

// ── estimate_passage ────────────────────────────────────────────────────

func TestFuelRateInstancesFromSnapshot_DiscoversInstanceNames(t *testing.T) {
	withGlobalSnapshot(t, snapshotWithSelfValues(map[string]any{
		"propulsion.port.fuel.rate":      1e-05,
		"propulsion.starboard.fuel.rate": 1e-05,
	}))

	got := fuelRateInstancesFromSnapshot()
	if len(got) != 2 || got[0] != "port" || got[1] != "starboard" {
		t.Fatalf("expected [port starboard], got %v", got)
	}
}

func TestFuelRateInstancesFromSnapshot_NoTreeIsNil(t *testing.T) {
	withGlobalSnapshot(t, newSignalKSnapshot())

	if got := fuelRateInstancesFromSnapshot(); got != nil {
		t.Fatalf("expected nil when the snapshot has no self tree, got %v", got)
	}
}

// stubInfluxRangeByPath returns an injectable influxRange func backed by a
// fixed map of path -> series, for estimate_passage tests that need no real
// InfluxDB connection at all. A path with no entry returns an empty series
// (not an error) - queryInfluxPathRange's own "no data" contract.
func stubInfluxRangeByPath(series map[string][]telemetryPoint) func(path string, start, stop time.Time, every string) ([]telemetryPoint, error) {
	return func(path string, start, stop time.Time, every string) ([]telemetryPoint, error) {
		return series[path], nil
	}
}

// singleBandPerformanceSeries builds a fixed three-sample series for sog,
// port and starboard fuel rate, and port revolutions that buildPerformanceTable
// turns into exactly one surviving band: 5.0 m/s (~9.7kts, band 9),
// 0.000013 m3/s per engine (93.6 L/h summed), 30.25 Hz (1815 rpm).
func singleBandPerformanceSeries() map[string][]telemetryPoint {
	points := []telemetryPoint{
		{Timestamp: time.Unix(0, 0).UTC(), Value: 0},
		{Timestamp: time.Unix(600, 0).UTC(), Value: 0},
		{Timestamp: time.Unix(1200, 0).UTC(), Value: 0},
	}
	sog := make([]telemetryPoint, len(points))
	fuel := make([]telemetryPoint, len(points))
	rpm := make([]telemetryPoint, len(points))
	for i, p := range points {
		sog[i] = telemetryPoint{Timestamp: p.Timestamp, Value: 5.0}
		fuel[i] = telemetryPoint{Timestamp: p.Timestamp, Value: 0.000013}
		rpm[i] = telemetryPoint{Timestamp: p.Timestamp, Value: 30.25}
	}
	return map[string][]telemetryPoint{
		"navigation.speedOverGround":     sog,
		"propulsion.port.fuel.rate":      fuel,
		"propulsion.starboard.fuel.rate": fuel,
		"propulsion.port.revolutions":    rpm,
	}
}

func TestExecuteEstimatePassage_RequestedSpeed(t *testing.T) {
	deps := assistantToolDeps{
		now:               func() time.Time { return time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC) },
		influxRange:       stubInfluxRangeByPath(singleBandPerformanceSeries()),
		fuelRateInstances: func() []string { return []string{"port", "starboard"} },
	}

	raw, err := deps.execute(context.Background(), "estimate_passage", json.RawMessage(`{"distance_nm":20,"speed_kts":10}`))
	if err != nil {
		t.Fatalf("execute estimate_passage: %v", err)
	}

	var result assistantEstimatePassageResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}

	if result.DistanceNm != 20 {
		t.Errorf("expected distance_nm=20, got %v", result.DistanceNm)
	}
	if result.SpeedKts != 10 {
		t.Errorf("expected speed_kts=10, got %v", result.SpeedKts)
	}
	if result.SpeedSource != "requested" {
		t.Errorf("expected speed_source=requested, got %q", result.SpeedSource)
	}
	if result.Hours != 2.0 {
		t.Errorf("expected hours=2.0 (20nm at 10kts), got %v", result.Hours)
	}
	if result.Litres != 187 {
		t.Errorf("expected litres=187 (2h at 93.6 L/h, rounded), got %v", result.Litres)
	}
	if result.LPerH != 93.6 {
		t.Errorf("expected l_per_h=93.6 (two engines summed), got %v", result.LPerH)
	}
	if result.RPM != 1815 {
		t.Errorf("expected rpm=1815, got %v", result.RPM)
	}
	if result.HistoryDays != 90 {
		t.Errorf("expected the default history_days=90, got %d", result.HistoryDays)
	}
	if result.Samples != 3 {
		t.Errorf("expected samples=3, got %d", result.Samples)
	}
	if len(result.Instances) != 2 || result.Instances[0] != "port" || result.Instances[1] != "starboard" {
		t.Errorf("expected instances=[port starboard], got %v", result.Instances)
	}
	if result.InstancesAssumed {
		t.Errorf("expected instances_assumed=false when the snapshot named the instances")
	}
	if len(result.Table) != 1 || result.Table[0].SOGKtsMin != 9 {
		t.Errorf("expected a single band 9 in the table, got %+v", result.Table)
	}
	if !strings.Contains(result.Note, "head seas add time and fuel") {
		t.Errorf("expected the observed-conditions note, got %q", result.Note)
	}
}

func TestExecuteEstimatePassage_OmittedSpeedUsesMostSampledBand(t *testing.T) {
	deps := assistantToolDeps{
		now:               func() time.Time { return time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC) },
		influxRange:       stubInfluxRangeByPath(singleBandPerformanceSeries()),
		fuelRateInstances: func() []string { return []string{"port", "starboard"} },
	}

	// 97nm at the single band's 9.7kts mean is exactly 10.0 hours, chosen so
	// the expected value is exact rather than needing its own rounding.
	raw, err := deps.execute(context.Background(), "estimate_passage", json.RawMessage(`{"distance_nm":97}`))
	if err != nil {
		t.Fatalf("execute estimate_passage: %v", err)
	}

	var result assistantEstimatePassageResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}

	if result.SpeedSource != "most_sampled_band" {
		t.Errorf("expected speed_source=most_sampled_band, got %q", result.SpeedSource)
	}
	if result.SpeedKts != 9.7 {
		t.Errorf("expected speed_kts=9.7 (the single band's mean), got %v", result.SpeedKts)
	}
	if result.Hours != 10.0 {
		t.Errorf("expected hours=10.0, got %v", result.Hours)
	}
	if result.Litres != 936 {
		t.Errorf("expected litres=936 (10h at 93.6 L/h), got %v", result.Litres)
	}
}

func TestExecuteEstimatePassage_NoUnderwayHistoryIsError(t *testing.T) {
	deps := assistantToolDeps{
		now:               func() time.Time { return time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC) },
		influxRange:       stubInfluxRangeByPath(map[string][]telemetryPoint{}), // every path comes back empty
		fuelRateInstances: func() []string { return []string{"port", "starboard"} },
	}

	_, err := deps.execute(context.Background(), "estimate_passage", json.RawMessage(`{"distance_nm":20,"days":30}`))
	if err == nil {
		t.Fatalf("expected an error when there is no underway history in the window")
	}
	if !strings.Contains(err.Error(), "no underway history in the last 30 days") {
		t.Errorf("expected the error to name the window, got %q", err.Error())
	}
}

func TestExecuteEstimatePassage_UnknownInstancesFallsBackAndFlagsAssumed(t *testing.T) {
	deps := assistantToolDeps{
		now:               func() time.Time { return time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC) },
		influxRange:       stubInfluxRangeByPath(singleBandPerformanceSeries()),
		fuelRateInstances: func() []string { return nil }, // snapshot has no propulsion tree yet
	}

	raw, err := deps.execute(context.Background(), "estimate_passage", json.RawMessage(`{"distance_nm":20,"speed_kts":10}`))
	if err != nil {
		t.Fatalf("execute estimate_passage: %v", err)
	}

	var result assistantEstimatePassageResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	if !result.InstancesAssumed {
		t.Errorf("expected instances_assumed=true when the snapshot named no instances")
	}
	if len(result.Instances) != 2 || result.Instances[0] != "port" || result.Instances[1] != "starboard" {
		t.Errorf("expected the port/starboard fallback, got %v", result.Instances)
	}
	// The fallback names must actually be the ones queried, not just
	// reported: singleBandPerformanceSeries only has data under
	// propulsion.port.* and propulsion.starboard.*, so a wrong guess would
	// have produced the empty-table error instead of a result.
	if len(result.Table) == 0 {
		t.Fatalf("expected a non-empty table, proving the fallback names were actually queried")
	}
}

func TestExecuteEstimatePassage_FuelAboardKnownAddsMargin(t *testing.T) {
	deps := assistantToolDeps{
		now:               func() time.Time { return time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC) },
		influxRange:       stubInfluxRangeByPath(singleBandPerformanceSeries()),
		fuelRateInstances: func() []string { return []string{"port", "starboard"} },
		fuelAboardM3:      func() (float64, bool) { return 0.6, true }, // 600 L aboard
	}

	raw, err := deps.execute(context.Background(), "estimate_passage", json.RawMessage(`{"distance_nm":20,"speed_kts":10}`))
	if err != nil {
		t.Fatalf("execute estimate_passage: %v", err)
	}

	var result assistantEstimatePassageResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}

	if result.FuelAboardL == nil || *result.FuelAboardL != 600 {
		t.Fatalf("expected fuel_aboard_l=600 (0.6 m3), got %v", result.FuelAboardL)
	}
	// litres=187 (asserted in TestExecuteEstimatePassage_RequestedSpeed for
	// the same inputs), so fuel_after_l = 600 - 187 = 413.
	if result.FuelAfterL == nil || *result.FuelAfterL != 600-result.Litres {
		t.Fatalf("expected fuel_after_l=aboard-litres (%v), got %v", 600-result.Litres, result.FuelAfterL)
	}
	if strings.Contains(result.Note, "fuel aboard unknown") {
		t.Errorf("expected no 'fuel aboard unknown' note when fuel aboard is known, got %q", result.Note)
	}
}

func TestExecuteEstimatePassage_FuelAboardUnknownOmitsFieldsAndNotesIt(t *testing.T) {
	deps := assistantToolDeps{
		now:               func() time.Time { return time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC) },
		influxRange:       stubInfluxRangeByPath(singleBandPerformanceSeries()),
		fuelRateInstances: func() []string { return []string{"port", "starboard"} },
		fuelAboardM3:      func() (float64, bool) { return 0, false },
	}

	raw, err := deps.execute(context.Background(), "estimate_passage", json.RawMessage(`{"distance_nm":20,"speed_kts":10}`))
	if err != nil {
		t.Fatalf("execute estimate_passage: %v", err)
	}

	// Check the raw JSON, not just the decoded struct, so an accidental
	// "fuel_aboard_l":0 (rather than a genuinely absent key) would fail
	// this test - omitempty on a nil *float64 must drop the key entirely.
	var raw2 map[string]any
	if err := json.Unmarshal([]byte(raw), &raw2); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, present := raw2["fuel_aboard_l"]; present {
		t.Errorf("expected fuel_aboard_l to be absent from the JSON, got %v", raw2["fuel_aboard_l"])
	}
	if _, present := raw2["fuel_after_l"]; present {
		t.Errorf("expected fuel_after_l to be absent from the JSON, got %v", raw2["fuel_after_l"])
	}

	var result assistantEstimatePassageResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	if result.FuelAboardL != nil || result.FuelAfterL != nil {
		t.Fatalf("expected both fuel fields nil, got aboard=%v after=%v", result.FuelAboardL, result.FuelAfterL)
	}
	if !strings.Contains(result.Note, "fuel aboard unknown") {
		t.Errorf("expected the note to mention fuel aboard is unknown, got %q", result.Note)
	}
}

func TestExecuteEstimatePassage_DistanceRequired(t *testing.T) {
	deps := assistantToolDeps{now: time.Now}
	_, err := deps.execute(context.Background(), "estimate_passage", json.RawMessage(`{"distance_nm":0}`))
	if err == nil {
		t.Fatalf("expected an error when distance_nm is missing/zero")
	}
}

func TestExecuteEstimatePassage_NonPositiveSpeedIsError(t *testing.T) {
	deps := assistantToolDeps{now: time.Now}
	_, err := deps.execute(context.Background(), "estimate_passage", json.RawMessage(`{"distance_nm":20,"speed_kts":0}`))
	if err == nil {
		t.Fatalf("expected an error for a non-positive speed_kts")
	}
}

// ── get_nearby_vessels (ADR 0128) ──────────────────────────────────────

// f64Ptr is a small pointer helper for building nearbyVessel fixtures whose
// optional float64 fields (SogKnots, CpaM, TcpaSeconds) are pointers.
func f64Ptr(v float64) *float64 { return &v }

// nearbyVesselsDeps builds the minimal assistantToolDeps get_nearby_vessels
// needs: a fixed vessel position, a fixed clock, and a fixed live-AIS list -
// contacts and signalKPositionHistory are left unset, which the tool must
// treat as "no sighting log"/"no history available" rather than panicking
// (same optional-dependency contract as d.documents/d.help).
func nearbyVesselsDeps(vesselLat, vesselLon float64, live []nearbyVessel) assistantToolDeps {
	return assistantToolDeps{
		now: func() time.Time { return time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC) },
		vesselState: func() (vesselStateData, error) {
			return vesselStateData{Latitude: vesselLat, Longitude: vesselLon}, nil
		},
		nearbyVessels: func(selfLat, selfLon float64, now time.Time, limit int) ([]nearbyVessel, error) {
			return live, nil
		},
	}
}

// ── assistantNameOrExactIDMatches (shared by find_places, matchAssistantNearbyVessels, latestContactsByName) ──

func TestAssistantNameOrExactIDMatches(t *testing.T) {
	cases := []struct {
		name, id, query string
		want            bool
	}{
		{"HOT CHILLI", "234567890", "", true},          // empty query matches everything
		{"HOT CHILLI", "234567890", "chilli", true},    // case-insensitive substring
		{"HOT CHILLI", "234567890", "CHILLI", true},    // case-insensitive both directions
		{"HOT CHILLI", "234567890", "234567890", true}, // exact id match
		{"HOT CHILLI", "234567890", "234", false},      // id match must be exact, not substring
		{"HOT CHILLI", "", "234567890", false},         // no id at all (e.g. a waypoint) never id-matches
		{"Tongue Bay", "", "Tongue", true},             // find_places' own case: no id, substring only
		{"HOT CHILLI", "234567890", "solaris", false},  // no match at all
	}
	for _, tc := range cases {
		if got := assistantNameOrExactIDMatches(tc.name, tc.id, tc.query); got != tc.want {
			t.Errorf("assistantNameOrExactIDMatches(%q, %q, %q) = %v, want %v", tc.name, tc.id, tc.query, got, tc.want)
		}
	}
}

func TestExecuteGetNearbyVessels_ListsLiveVesselWithRangeBearingAndSpeed(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	live := []nearbyVessel{
		// Due north of the vessel (same longitude, less negative latitude in
		// the southern hemisphere) - bearing_deg must come out ~0.
		{
			Name: "HOT CHILLI", Mmsi: "234567890",
			RangeM: 1852.0, AgeSeconds: 12, SogKnots: f64Ptr(4.2),
			Lat: vesselLat + 0.05, Lon: vesselLon,
			CpaM: f64Ptr(500.0), TcpaSeconds: f64Ptr(300.0),
		},
	}
	deps := nearbyVesselsDeps(vesselLat, vesselLon, live)

	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute get_nearby_vessels: %v", err)
	}

	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	if len(result.Vessels) != 1 {
		t.Fatalf("expected 1 vessel, got %d: %+v", len(result.Vessels), result.Vessels)
	}
	v := result.Vessels[0]
	if v.Name != "HOT CHILLI" || v.Mmsi != "234567890" {
		t.Errorf("expected HOT CHILLI/234567890, got %q/%q", v.Name, v.Mmsi)
	}
	if v.RangeNm != 1.0 {
		t.Errorf("expected range_nm=1.0 (1852m), got %v", v.RangeNm)
	}
	if v.BearingDeg < -1 || v.BearingDeg > 1 {
		t.Errorf("expected bearing_deg ~0 for a due-north target, got %d", v.BearingDeg)
	}
	if v.SogKts == nil || *v.SogKts != 4.2 {
		t.Errorf("expected sog_kts=4.2, got %v", v.SogKts)
	}
	if v.CpaNm == nil || *v.CpaNm != roundTo2(500.0/metersPerNauticalMile) {
		t.Errorf("expected cpa_nm derived from cpa_m, got %v", v.CpaNm)
	}
	if v.TcpaMin == nil || *v.TcpaMin != 5.0 {
		t.Errorf("expected tcpa_min=5.0 (300s), got %v", v.TcpaMin)
	}
	if v.PositionAgeS != 12 {
		t.Errorf("expected position_age_s=12, got %d", v.PositionAgeS)
	}
	if result.Note == "" {
		t.Errorf("expected a top-level note explaining in_range_since")
	}
	if !strings.Contains(result.Note, "lower bound") {
		t.Errorf("expected the note to call in_range_since a lower bound, got %q", result.Note)
	}
}

func TestExecuteGetNearbyVessels_MaxResultsDefaultAndClamp(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	live := make([]nearbyVessel, 0, 15)
	for i := 0; i < 15; i++ {
		live = append(live, nearbyVessel{
			Name: fmt.Sprintf("V%d", i), Mmsi: fmt.Sprintf("%09d", i),
			RangeM: float64(100 * (i + 1)), Lat: vesselLat, Lon: vesselLon,
		})
	}

	// Default (no max_results given): 10.
	deps := nearbyVesselsDeps(vesselLat, vesselLon, live)
	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute (default): %v", err)
	}
	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal (default): %v", err)
	}
	if len(result.Vessels) != 10 {
		t.Fatalf("expected the default max_results=10, got %d", len(result.Vessels))
	}
	// A code-review finding (2026-09-25): cutting 15 matches down to the
	// default 10 used to leave truncated=false and no note at all -
	// indistinguishable from "these are the only 10 vessels in range."
	if !result.Truncated {
		t.Fatalf("expected truncated=true once max_results(10) cuts 15 matched vessels down")
	}
	if !strings.Contains(result.Note, "15") || !strings.Contains(result.Note, "10") {
		t.Fatalf("expected the note to name both the total matched (15) and the shown count (10), got %q", result.Note)
	}

	// max_results above the 25 ceiling clamps to 25 (only 15 live, so this
	// also confirms the ceiling doesn't truncate below what's available).
	raw, err = deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{"max_results":100}`))
	if err != nil {
		t.Fatalf("execute (clamped): %v", err)
	}
	// A fresh result, not a reuse of the one above: Truncated is
	// `omitempty`, so unmarshalling a false value into an already-true
	// field would silently leave the stale true in place.
	var clampedResult assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &clampedResult); err != nil {
		t.Fatalf("unmarshal (clamped): %v", err)
	}
	if len(clampedResult.Vessels) != 15 {
		t.Fatalf("expected all 15 live vessels once max_results is clamped to 25, got %d", len(clampedResult.Vessels))
	}
	if clampedResult.Truncated {
		t.Fatalf("expected truncated=false when every matched vessel fits under max_results")
	}
}

func TestExecuteGetNearbyVessels_NameFilterMatchesSubstringCaseInsensitive(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	live := []nearbyVessel{
		{Name: "HOT CHILLI", Mmsi: "234567890", RangeM: 500, Lat: vesselLat, Lon: vesselLon},
		{Name: "SOLARIS", Mmsi: "111222333", RangeM: 800, Lat: vesselLat, Lon: vesselLon},
	}
	deps := nearbyVesselsDeps(vesselLat, vesselLon, live)

	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{"name":"chilli"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Vessels) != 1 || result.Vessels[0].Name != "HOT CHILLI" {
		t.Fatalf("expected only HOT CHILLI to match \"chilli\" case-insensitively, got %+v", result.Vessels)
	}
}

func TestExecuteGetNearbyVessels_NameFilterMatchesExactMMSI(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	live := []nearbyVessel{
		{Name: "HOT CHILLI", Mmsi: "234567890", RangeM: 500, Lat: vesselLat, Lon: vesselLon},
		{Name: "SOLARIS", Mmsi: "111222333", RangeM: 800, Lat: vesselLat, Lon: vesselLon},
	}
	deps := nearbyVesselsDeps(vesselLat, vesselLon, live)

	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{"name":"111222333"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Vessels) != 1 || result.Vessels[0].Name != "SOLARIS" {
		t.Fatalf("expected an exact MMSI match to find SOLARIS, got %+v", result.Vessels)
	}
}

// TestExecuteGetNearbyVessels_NameFilterFindsVesselBeyondTheNormalSearchCap
// is a code-review finding: get_nearby_vessels' own initial fetch is capped
// at assistantNearbyVesselsMaxMaxResults (25), sorted by range, the same way
// the map tile's own fetch is capped at 10. In a crowded anchorage with more
// than 25 AIS targets within range, a vessel ranked 26th by range would
// otherwise never be found by name at all - it would wrongly fall through to
// the sighting-log's not_in_range path (a stale last-seen date) even though
// it is genuinely live right now. A specific name lookup must search past
// that cap rather than silently miss a vessel that is actually in range.
func TestExecuteGetNearbyVessels_NameFilterFindsVesselBeyondTheNormalSearchCap(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	const liveCount = 26
	live := make([]nearbyVessel, 0, liveCount)
	for i := 0; i < liveCount; i++ {
		name := fmt.Sprintf("V%02d", i)
		if i == liveCount-1 {
			name = "FARAWAY SOLARIS"
		}
		live = append(live, nearbyVessel{
			Name: name, Mmsi: fmt.Sprintf("%09d", i),
			RangeM: float64(100 * (i + 1)), // ascending range: index i is the (i+1)th closest
			Lat:    vesselLat, Lon: vesselLon,
		})
	}

	deps := assistantToolDeps{
		now: func() time.Time { return time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC) },
		vesselState: func() (vesselStateData, error) {
			return vesselStateData{Latitude: vesselLat, Longitude: vesselLon}, nil
		},
		// Mimics fetchSignalKNearbyVesselsLimit's real contract: sorted by
		// range, trimmed to whatever limit is asked for - including
		// nearbyVesselsUnlimited (-1), which means "every in-range vessel".
		nearbyVessels: func(selfLat, selfLon float64, now time.Time, limit int) ([]nearbyVessel, error) {
			if limit < 0 || limit > len(live) {
				limit = len(live)
			}
			return live[:limit], nil
		},
	}

	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{"name":"solaris"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	if len(result.NotInRange) != 0 {
		t.Fatalf("expected the 26th-closest vessel not to fall through to not_in_range, got %+v", result.NotInRange)
	}
	if len(result.Vessels) != 1 || result.Vessels[0].Name != "FARAWAY SOLARIS" {
		t.Fatalf("expected FARAWAY SOLARIS to be found beyond the normal 25-vessel search cap, got %+v", result.Vessels)
	}
}

// TestExecuteGetNearbyVessels_NameFilterNoLiveMatchFallsBackToSightingLog is
// "when did we last see X" for a boat that has left range entirely: no live
// AIS target matches, so the sighting log is searched by name instead.
func TestExecuteGetNearbyVessels_NameFilterNoLiveMatchFallsBackToSightingLog(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	store := newTestNearbyContactStore(t)
	seenAt := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)
	if err := store.recordContactIfNew("234567890", "Hot Chilli", -20.2, 149.2, "Nara Inlet", "anchored", seenAt, seenAt); err != nil {
		t.Fatalf("recordContactIfNew: %v", err)
	}

	deps := nearbyVesselsDeps(vesselLat, vesselLon, nil) // nothing currently in range
	deps.contacts = func() *nearbyContactStore { return store }

	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{"name":"chilli"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	if len(result.Vessels) != 0 {
		t.Fatalf("expected no live vessels, got %+v", result.Vessels)
	}
	if len(result.NotInRange) != 1 {
		t.Fatalf("expected 1 not_in_range entry, got %d: %+v", len(result.NotInRange), result.NotInRange)
	}
	got := result.NotInRange[0]
	if got.Name != "Hot Chilli" || got.Mmsi != "234567890" {
		t.Fatalf("expected Hot Chilli/234567890, got %+v", got)
	}
	wantSeenAt := seenAt.UTC().Format(time.RFC3339)
	if got.LastSeenAt != wantSeenAt {
		t.Fatalf("expected last_seen_at=%s, got %s", wantSeenAt, got.LastSeenAt)
	}
}

// TestExecuteGetNearbyVessels_InRangeSinceFromNewestConfirmedSighting is the
// ordinary case: a live vessel whose sighting log has a confirmed row
// reports that row's seen_at as in_range_since.
func TestExecuteGetNearbyVessels_InRangeSinceFromNewestConfirmedSighting(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	store := newTestNearbyContactStore(t) // dwell 0: confirms on the first tick
	seenAt := time.Date(2026, 9, 24, 6, 30, 0, 0, time.UTC)
	if err := store.recordContactIfNew("234567890", "HOT CHILLI", -20.2, 149.2, "Nara Inlet", "anchored", seenAt, seenAt); err != nil {
		t.Fatalf("recordContactIfNew: %v", err)
	}

	live := []nearbyVessel{{Name: "HOT CHILLI", Mmsi: "234567890", RangeM: 500, Lat: vesselLat, Lon: vesselLon}}
	deps := nearbyVesselsDeps(vesselLat, vesselLon, live)
	deps.contacts = func() *nearbyContactStore { return store }

	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Vessels) != 1 {
		t.Fatalf("expected 1 vessel, got %d", len(result.Vessels))
	}
	v := result.Vessels[0]
	if v.InRangeSince == nil {
		t.Fatalf("expected in_range_since to be set for a confirmed sighting")
	}
	want := seenAt.UTC().Format(time.RFC3339)
	if *v.InRangeSince != want {
		t.Fatalf("expected in_range_since=%s, got %s", want, *v.InRangeSince)
	}
	if v.PreviousSightingsCount != 0 {
		t.Fatalf("expected previous_sightings_count=0 for a single, still-ongoing encounter, got %d", v.PreviousSightingsCount)
	}
}

// TestExecuteGetNearbyVessels_InRangeSinceAbsentWhilePending is the case
// isPending exists for: a vessel that has just come into range and has not
// yet sat through the confirmation dwell must not report in_range_since at
// all, since the only row on file (if any) belongs to a different, already-
// ended encounter.
func TestExecuteGetNearbyVessels_InRangeSinceAbsentWhilePending(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	store := newTestNearbyContactStoreWithDwell(t, contactConfirmDwell)
	tick := time.Date(2026, 9, 24, 6, 30, 0, 0, time.UTC)
	if err := store.recordContactIfNew("234567890", "HOT CHILLI", -20.2, 149.2, "Nara Inlet", "anchored", tick, tick); err != nil {
		t.Fatalf("recordContactIfNew: %v", err)
	}
	if !store.isPending("234567890") {
		t.Fatalf("test setup: expected the vessel to still be pending")
	}

	live := []nearbyVessel{{Name: "HOT CHILLI", Mmsi: "234567890", RangeM: 500, Lat: vesselLat, Lon: vesselLon}}
	deps := nearbyVesselsDeps(vesselLat, vesselLon, live)
	deps.contacts = func() *nearbyContactStore { return store }

	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Vessels) != 1 {
		t.Fatalf("expected 1 vessel, got %d", len(result.Vessels))
	}
	if result.Vessels[0].InRangeSince != nil {
		t.Fatalf("expected in_range_since to be absent while the encounter is still pending confirmation, got %s", *result.Vessels[0].InRangeSince)
	}
}

// TestExecuteGetNearbyVessels_ReturningVesselPendingKeepsPriorEncounterAsHistory
// is the sharper version of the isPending gate: a vessel with one already-
// confirmed encounter on file that has since left and come back is, on its
// return, a pending candidate again. in_range_since must stay absent (the
// old row is NOT the current encounter), but that old row must still count
// as a previous sighting - it genuinely happened - rather than being lost.
func TestExecuteGetNearbyVessels_ReturningVesselPendingKeepsPriorEncounterAsHistory(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	store := newTestNearbyContactStoreWithDwell(t, contactConfirmDwell)
	const vesselKey = "234567890"

	// First encounter: tick continuously through the dwell with a refreshed
	// position, so it actually confirms (see
	// TestRecordContactIfNew_DwellConfirmationBackdatesToFirstTick for the
	// same pattern).
	firstStart := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	posA, posB := firstStart, firstStart.Add(2*time.Minute)
	for elapsed := 0 * time.Second; elapsed <= contactConfirmDwell; elapsed += 5 * time.Second {
		tick := firstStart.Add(elapsed)
		pos := posA
		if elapsed >= 2*time.Minute {
			pos = posB
		}
		if err := store.recordContactIfNew(vesselKey, "HOT CHILLI", -20.3, 149.3, "Cid Harbour", "anchored", pos, tick); err != nil {
			t.Fatalf("recordContactIfNew (1st encounter, tick +%s): %v", elapsed, err)
		}
	}
	if store.isPending(vesselKey) {
		t.Fatalf("test setup: expected the first encounter to have confirmed")
	}

	// The vessel leaves for well over contactSessionMaxGapForPositionOverride
	// (24h) and returns: a single tick makes it a fresh pending candidate,
	// not a continuation.
	returnTick := firstStart.Add(48 * time.Hour)
	if err := store.recordContactIfNew(vesselKey, "HOT CHILLI", -20.2, 149.2, "Nara Inlet", "anchored", returnTick, returnTick); err != nil {
		t.Fatalf("recordContactIfNew (return): %v", err)
	}
	if !store.isPending(vesselKey) {
		t.Fatalf("test setup: expected the vessel to be pending again after returning")
	}

	live := []nearbyVessel{{Name: "HOT CHILLI", Mmsi: vesselKey, RangeM: 500, Lat: vesselLat, Lon: vesselLon}}
	deps := nearbyVesselsDeps(vesselLat, vesselLon, live)
	deps.contacts = func() *nearbyContactStore { return store }

	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{"name":"chilli"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	v := result.Vessels[0]
	if v.InRangeSince != nil {
		t.Fatalf("expected in_range_since absent for a not-yet-confirmed return visit, got %s", *v.InRangeSince)
	}
	if v.PreviousSightingsCount != 1 {
		t.Fatalf("expected the earlier, already-confirmed encounter to still count as 1 previous sighting, got %d", v.PreviousSightingsCount)
	}
	if len(v.PastSightings) != 1 || v.PastSightings[0].Geoname != "Cid Harbour" {
		t.Fatalf("expected the Cid Harbour encounter in past_sightings, got %+v", v.PastSightings)
	}
}

// TestExecuteGetNearbyVessels_DuplicateMMSIOnlyAttachesMatchingVesselsHistory
// is a code-review finding: if two live AIS targets ever report the same
// MMSI (a data-quality fault upstream), both would resolve to the same
// sighting-log vessel_key. Only the one whose live name actually matches the
// log's most recently recorded name for that key may be enriched with its
// in_range_since/past_sightings - the other must get none of it rather than
// borrowing a different vessel's history.
func TestExecuteGetNearbyVessels_DuplicateMMSIOnlyAttachesMatchingVesselsHistory(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	store := newTestNearbyContactStore(t)
	seenAt := time.Date(2026, 9, 24, 6, 30, 0, 0, time.UTC)
	// The sighting log's own record was made under "HOT CHILLI".
	if err := store.recordContactIfNew("234567890", "HOT CHILLI", -20.2, 149.2, "Nara Inlet", "anchored", seenAt, seenAt); err != nil {
		t.Fatalf("recordContactIfNew: %v", err)
	}

	// Two live targets somehow share the same MMSI with different names.
	live := []nearbyVessel{
		{Name: "HOT CHILLI", Mmsi: "234567890", RangeM: 500, Lat: vesselLat, Lon: vesselLon},
		{Name: "GHOST DUPLICATE", Mmsi: "234567890", RangeM: 900, Lat: vesselLat, Lon: vesselLon},
	}
	deps := nearbyVesselsDeps(vesselLat, vesselLon, live)
	deps.contacts = func() *nearbyContactStore { return store }

	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Vessels) != 2 {
		t.Fatalf("expected both live vessels listed, got %d", len(result.Vessels))
	}

	var chilli, ghost *assistantNearbyVesselOut
	for i := range result.Vessels {
		switch result.Vessels[i].Name {
		case "HOT CHILLI":
			chilli = &result.Vessels[i]
		case "GHOST DUPLICATE":
			ghost = &result.Vessels[i]
		}
	}
	if chilli == nil || ghost == nil {
		t.Fatalf("expected both HOT CHILLI and GHOST DUPLICATE in the result, got %+v", result.Vessels)
	}
	if chilli.InRangeSince == nil {
		t.Fatalf("expected HOT CHILLI (the name that actually matches the sighting log) to get in_range_since")
	}
	if ghost.InRangeSince != nil {
		t.Fatalf("expected GHOST DUPLICATE not to borrow HOT CHILLI's sighting history just because they share an MMSI, got in_range_since=%s", *ghost.InRangeSince)
	}
	if ghost.PreviousSightingsCount != 0 {
		t.Fatalf("expected GHOST DUPLICATE's previous_sightings_count to stay 0, got %d", ghost.PreviousSightingsCount)
	}
}

// TestExecuteGetNearbyVessels_PlaceholderStoredNameIsNotTreatedAsAMismatch is
// the direct regression test for a code-review finding (2026-09-25): the
// same-MMSI name guard above discarded a vessel's ENTIRE sighting history
// whenever the name recorded at encounter start differs from the live
// vessel's current name - which is the ordinary case, not the rare fault
// the guard exists for. recordNearbyVesselContacts (tracks.go) falls back to
// compactVesselID - the MMSI itself - as a vessel's Name when its AIS static
// data (the real name) has not arrived by the time the sighting log's
// 5-minute confirmation dwell elapses; the real name often arrives only
// afterward, on the SAME continuous encounter. A stored name that is just
// the vessel_key must not disqualify what is otherwise an exact MMSI match.
func TestExecuteGetNearbyVessels_PlaceholderStoredNameIsNotTreatedAsAMismatch(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	store := newTestNearbyContactStore(t)
	seenAt := time.Date(2026, 9, 24, 6, 30, 0, 0, time.UTC)
	// The row was written before AIS static data had arrived, so its Name
	// is the placeholder compactVesselID falls back to: the MMSI itself.
	if err := store.recordContactIfNew("234567890", "234567890", -20.2, 149.2, "Nara Inlet", "anchored", seenAt, seenAt); err != nil {
		t.Fatalf("recordContactIfNew: %v", err)
	}

	// By the time get_nearby_vessels runs, AIS static data has arrived and
	// the live target now reports its real name.
	live := []nearbyVessel{{Name: "HOT CHILLI", Mmsi: "234567890", RangeM: 500, Lat: vesselLat, Lon: vesselLon}}
	deps := nearbyVesselsDeps(vesselLat, vesselLon, live)
	deps.contacts = func() *nearbyContactStore { return store }

	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{"name":"chilli"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Vessels) != 1 {
		t.Fatalf("expected 1 vessel, got %d: %+v", len(result.Vessels), result.Vessels)
	}
	v := result.Vessels[0]
	if v.InRangeSince == nil {
		t.Fatalf("expected in_range_since to still be populated: a placeholder stored name (the MMSI itself) must not be treated as a mismatch against the vessel's later-arrived real name")
	}
}

// TestExecuteGetNearbyVessels_PastSightingsIncludedWithNameDefault confirms
// include_history defaults to true when name is given: past_sightings
// carries the vessel's prior (non-current) encounters.
func TestExecuteGetNearbyVessels_PastSightingsIncludedWithNameDefault(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	store := newTestNearbyContactStore(t)
	first := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	second := first.Add(72 * time.Hour)
	if err := store.recordContactIfNew("234567890", "HOT CHILLI", -20.3, 149.3, "Cid Harbour", "anchored", first, first); err != nil {
		t.Fatalf("recordContactIfNew (1st): %v", err)
	}
	if err := store.recordContactIfNew("234567890", "HOT CHILLI", -20.2, 149.2, "Nara Inlet", "anchored", second, second); err != nil {
		t.Fatalf("recordContactIfNew (2nd): %v", err)
	}

	live := []nearbyVessel{{Name: "HOT CHILLI", Mmsi: "234567890", RangeM: 500, Lat: vesselLat, Lon: vesselLon}}
	deps := nearbyVesselsDeps(vesselLat, vesselLon, live)
	deps.contacts = func() *nearbyContactStore { return store }

	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{"name":"chilli"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Vessels) != 1 {
		t.Fatalf("expected 1 vessel, got %d", len(result.Vessels))
	}
	v := result.Vessels[0]
	if v.PreviousSightingsCount != 1 {
		t.Fatalf("expected previous_sightings_count=1, got %d", v.PreviousSightingsCount)
	}
	if len(v.PastSightings) != 1 {
		t.Fatalf("expected 1 past sighting (the Cid Harbour encounter), got %d: %+v", len(v.PastSightings), v.PastSightings)
	}
	if v.PastSightings[0].Geoname != "Cid Harbour" {
		t.Fatalf("expected the past sighting to be the Cid Harbour encounter, got %+v", v.PastSightings[0])
	}
}

// TestExecuteGetNearbyVessels_HistoryOmittedWithoutNameByDefault confirms
// include_history defaults to false with no name filter: no past_sightings,
// and the (expensive, per-vessel) SignalK position-history lookup is never
// even called.
func TestExecuteGetNearbyVessels_HistoryOmittedWithoutNameByDefault(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	store := newTestNearbyContactStore(t)
	seenAt := time.Date(2026, 9, 24, 6, 30, 0, 0, time.UTC)
	if err := store.recordContactIfNew("234567890", "HOT CHILLI", -20.2, 149.2, "Nara Inlet", "anchored", seenAt, seenAt); err != nil {
		t.Fatalf("recordContactIfNew: %v", err)
	}

	live := []nearbyVessel{{Name: "HOT CHILLI", Mmsi: "234567890", RangeM: 500, Lat: vesselLat, Lon: vesselLon}}
	deps := nearbyVesselsDeps(vesselLat, vesselLon, live)
	deps.contacts = func() *nearbyContactStore { return store }
	historyCalls := 0
	deps.signalKPositionHistory = func(mmsi string, from, to time.Time, resolutionSeconds int) ([]signalKHistoryPoint, error) {
		historyCalls++
		return nil, nil
	}

	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Vessels[0].PastSightings) != 0 {
		t.Fatalf("expected no past_sightings without an explicit name filter, got %+v", result.Vessels[0].PastSightings)
	}
	if result.Vessels[0].InRangeSince == nil {
		t.Fatalf("expected in_range_since to still be populated even with history omitted")
	}
	if historyCalls != 0 {
		t.Fatalf("expected signalKPositionHistory to never be called when include_history is false, got %d calls", historyCalls)
	}
}

// TestExecuteGetNearbyVessels_PositionHistorySkippedWithoutNameEvenWhenIncludeHistoryExplicitlyTrue
// is a code-review finding: get_nearby_vessels' JSON schema lets the model
// pass include_history:true on a bare "who's nearby" call with no name at
// all, which would otherwise fan out one sequential SignalK History API
// HTTP round trip per matched vessel (up to 25) before the tool result could
// return. The SignalK position-history lookup - unlike past_sightings,
// which is one cheap local SQLite read - only ever runs when a name filter
// has actually narrowed the match set to the vessel(s) the question is
// about.
func TestExecuteGetNearbyVessels_PositionHistorySkippedWithoutNameEvenWhenIncludeHistoryExplicitlyTrue(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	live := []nearbyVessel{
		{Name: "HOT CHILLI", Mmsi: "234567890", RangeM: 500, Lat: vesselLat, Lon: vesselLon},
		{Name: "SOLARIS", Mmsi: "111222333", RangeM: 800, Lat: vesselLat, Lon: vesselLon},
	}
	deps := nearbyVesselsDeps(vesselLat, vesselLon, live)
	historyCalls := 0
	deps.signalKPositionHistory = func(mmsi string, from, to time.Time, resolutionSeconds int) ([]signalKHistoryPoint, error) {
		historyCalls++
		return nil, nil
	}

	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{"include_history":true}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Vessels) != 2 {
		t.Fatalf("expected 2 vessels, got %d", len(result.Vessels))
	}
	if historyCalls != 0 {
		t.Fatalf("expected signalKPositionHistory to never be called without a name filter, even with include_history:true, got %d calls", historyCalls)
	}
	if !strings.Contains(result.Note, "position history") || !strings.Contains(result.Note, "name") {
		t.Fatalf("expected the result note to say position history needs a vessel name, got %q", result.Note)
	}
}

// TestAssistantToolDefinitions_GetNearbyVesselsIncludeHistoryDescribesNameRequirement
// is the schema half of the fix above: the model should not need to
// discover by trial that include_history's position-history half only runs
// with a name filter - the parameter description says so.
func TestAssistantToolDefinitions_GetNearbyVesselsIncludeHistoryDescribesNameRequirement(t *testing.T) {
	for _, tool := range assistantToolDefinitions() {
		if tool.Function.Name != "get_nearby_vessels" {
			continue
		}
		params := string(tool.Function.Parameters)
		if !strings.Contains(params, "only") || !strings.Contains(params, "name") {
			t.Fatalf("expected the include_history parameter description to state it needs a name, got: %s", params)
		}
		return
	}
	t.Fatal("get_nearby_vessels tool definition not found")
}

// TestExecuteGetNearbyVessels_IncludeHistoryFalseExplicitlyOmitsPastSightings
// confirms an explicit include_history:false overrides the name-given
// default of true.
func TestExecuteGetNearbyVessels_IncludeHistoryFalseExplicitlyOmitsPastSightings(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	store := newTestNearbyContactStore(t)
	first := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	second := first.Add(72 * time.Hour)
	if err := store.recordContactIfNew("234567890", "HOT CHILLI", -20.3, 149.3, "Cid Harbour", "anchored", first, first); err != nil {
		t.Fatalf("recordContactIfNew (1st): %v", err)
	}
	if err := store.recordContactIfNew("234567890", "HOT CHILLI", -20.2, 149.2, "Nara Inlet", "anchored", second, second); err != nil {
		t.Fatalf("recordContactIfNew (2nd): %v", err)
	}

	live := []nearbyVessel{{Name: "HOT CHILLI", Mmsi: "234567890", RangeM: 500, Lat: vesselLat, Lon: vesselLon}}
	deps := nearbyVesselsDeps(vesselLat, vesselLon, live)
	deps.contacts = func() *nearbyContactStore { return store }

	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{"name":"chilli","include_history":false}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	v := result.Vessels[0]
	if len(v.PastSightings) != 0 {
		t.Fatalf("expected no past_sightings when include_history is explicitly false, got %+v", v.PastSightings)
	}
	if v.PreviousSightingsCount != 1 {
		t.Fatalf("expected previous_sightings_count to still be populated (it's cheap), got %d", v.PreviousSightingsCount)
	}
}

// TestExecuteGetNearbyVessels_StationarySinceWalksBackToLastMovement is
// stationary_since's core case: several consecutive older points far from
// the settled position (a genuine relocation, not a single noisy fix),
// followed by a cluster of recent points near it. stationary_since must
// land on the first point after that move, not the oldest point in the
// window. Three consecutive far points are used, not one, because
// computeStationarySince (ADR 0128 amendment, code-review finding
// 2026-09-25) requires assistantStationaryConsecutiveOutliers consecutive
// far points before treating a run as a real move rather than GPS noise.
func TestExecuteGetNearbyVessels_StationarySinceWalksBackToLastMovement(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	moved1 := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	moved2 := moved1.Add(10 * time.Minute)
	moved3 := moved2.Add(10 * time.Minute)
	settled := moved3.Add(10 * time.Minute)
	stillA := settled.Add(1 * time.Hour)
	stillB := settled.Add(2 * time.Hour)

	points := []signalKHistoryPoint{
		{Time: moved1, Lat: -19.0, Lon: 148.0},       // far away: the vessel was elsewhere
		{Time: moved2, Lat: -19.001, Lon: 148.001},   // still elsewhere
		{Time: moved3, Lat: -19.002, Lon: 148.002},   // still elsewhere - 3rd consecutive far point
		{Time: settled, Lat: -20.20, Lon: 149.20},    // arrives at the mooring
		{Time: stillA, Lat: -20.2001, Lon: 149.2001}, // within 100m: anchor swing
		{Time: stillB, Lat: -20.2000, Lon: 149.2000}, // the latest fix
	}

	live := []nearbyVessel{{Name: "HOT CHILLI", Mmsi: "234567890", RangeM: 500, Lat: vesselLat, Lon: vesselLon}}
	deps := nearbyVesselsDeps(vesselLat, vesselLon, live)
	deps.signalKPositionHistory = func(mmsi string, from, to time.Time, resolutionSeconds int) ([]signalKHistoryPoint, error) {
		return points, nil
	}

	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{"name":"chilli"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	v := result.Vessels[0]
	if v.StationarySince == nil {
		t.Fatalf("expected stationary_since to be set")
	}
	want := settled.UTC().Format(time.RFC3339)
	if *v.StationarySince != want {
		t.Fatalf("expected stationary_since=%s (the first point after the >100m move), got %s", want, *v.StationarySince)
	}
	if v.HistoryCoversFrom == nil {
		t.Fatalf("expected history_covers_from to be set")
	}
}

// TestExecuteGetNearbyVessels_StationarySinceAllWithinThresholdUsesOldestPoint
// confirms that when every returned point is within the stationary
// threshold of the latest one, stationary_since falls back to the oldest
// point in the window - a lower bound, per history_covers_from, rather than
// a claim the vessel was never anywhere else.
func TestExecuteGetNearbyVessels_StationarySinceAllWithinThresholdUsesOldestPoint(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	oldest := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	points := []signalKHistoryPoint{
		{Time: oldest, Lat: -20.2000, Lon: 149.2000},
		{Time: oldest.Add(1 * time.Hour), Lat: -20.2001, Lon: 149.2001},
		{Time: oldest.Add(2 * time.Hour), Lat: -20.2000, Lon: 149.2000},
	}

	live := []nearbyVessel{{Name: "HOT CHILLI", Mmsi: "234567890", RangeM: 500, Lat: vesselLat, Lon: vesselLon}}
	deps := nearbyVesselsDeps(vesselLat, vesselLon, live)
	deps.signalKPositionHistory = func(mmsi string, from, to time.Time, resolutionSeconds int) ([]signalKHistoryPoint, error) {
		return points, nil
	}

	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{"name":"chilli"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	v := result.Vessels[0]
	if v.StationarySince == nil {
		t.Fatalf("expected stationary_since to be set")
	}
	want := oldest.UTC().Format(time.RFC3339)
	if *v.StationarySince != want {
		t.Fatalf("expected stationary_since to fall back to the oldest point (%s), got %s", want, *v.StationarySince)
	}
}

// ── computeStationarySince (the stationary_since algorithm itself) ────────

// TestComputeStationarySince_SwingingOnMooringNeverTriggersFalseMovement is
// a code-review finding: the original algorithm measured every point
// against the single latest fix, so ordinary mooring/anchor swing - a boat
// whose position genuinely oscillates between two points more than
// assistantStationaryThresholdMeters apart, without ever actually relocating
// - could report a recent "movement" purely because the latest fix happened
// to land on one side of the swing. computeStationarySince's requirement of
// assistantStationaryConsecutiveOutliers CONSECUTIVE far points defeats
// this: an alternating swing pattern never strings together that many far
// points in a row.
func TestComputeStationarySince_SwingingOnMooringNeverTriggersFalseMovement(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	// ~60m either side of a centre point - about 120m apart from each other
	// (over the 100m threshold), simulating mooring swing rather than a
	// relocation.
	north := struct{ lat, lon float64 }{-20.20 + 0.00054, 149.20}
	south := struct{ lat, lon float64 }{-20.20 - 0.00054, 149.20}

	points := make([]signalKHistoryPoint, 0, 12)
	for i := 0; i < 12; i++ {
		p := north
		if i%2 == 1 {
			p = south
		}
		points = append(points, signalKHistoryPoint{Time: base.Add(time.Duration(i) * time.Hour), Lat: p.lat, Lon: p.lon})
	}

	got, stationary := computeStationarySince(points)
	if !stationary {
		t.Fatalf("expected mooring swing to still be reported as stationary, got stationary=false")
	}
	if !got.Equal(points[0].Time) {
		t.Fatalf("expected mooring swing (never %d consecutive far points) to report stationary since the oldest point (%s), got %s", assistantStationaryConsecutiveOutliers, points[0].Time, got)
	}
}

// TestComputeStationarySince_SingleOutlierAtLatestFixIgnored is the other
// code-review finding: the original algorithm's reference point was the
// single latest fix, so one noisy GPS reading at the very end of the series
// - the most consequential place for one to land - would report the vessel
// as having "just" arrived, discarding the whole settled history behind it.
// Measuring against the median of the most recent points instead absorbs a
// lone outlier automatically (a median of several values ignores the one
// that doesn't belong).
func TestComputeStationarySince_SingleOutlierAtLatestFixIgnored(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	const settledLat, settledLon = -20.20, 149.20
	const glitchLat, glitchLon = -20.2027, 149.20 // ~300m south: a single bad fix

	points := []signalKHistoryPoint{
		{Time: base, Lat: settledLat, Lon: settledLon},
		{Time: base.Add(1 * time.Hour), Lat: settledLat, Lon: settledLon},
		{Time: base.Add(2 * time.Hour), Lat: settledLat, Lon: settledLon},
		{Time: base.Add(3 * time.Hour), Lat: settledLat, Lon: settledLon},
		{Time: base.Add(4 * time.Hour), Lat: glitchLat, Lon: glitchLon}, // the latest fix
	}

	got, stationary := computeStationarySince(points)
	if !stationary {
		t.Fatalf("expected a single noisy latest fix still to be reported as stationary, got stationary=false")
	}
	if !got.Equal(points[0].Time) {
		t.Fatalf("expected a single noisy latest fix not to be mistaken for a real move, got stationary_since=%s want=%s", got, points[0].Time)
	}
}

// TestComputeStationarySince_GenuineMoveIsDetected is the positive case:
// several consecutive points far from where the vessel has since settled -
// a real relocation - correctly moves stationary_since forward to just
// after that run, not the oldest point in the window.
func TestComputeStationarySince_GenuineMoveIsDetected(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	points := []signalKHistoryPoint{
		{Time: base, Lat: -19.0, Lon: 148.0},
		{Time: base.Add(1 * time.Hour), Lat: -19.001, Lon: 148.001},
		{Time: base.Add(2 * time.Hour), Lat: -19.002, Lon: 148.002},
		{Time: base.Add(3 * time.Hour), Lat: -20.20, Lon: 149.20},
		{Time: base.Add(4 * time.Hour), Lat: -20.2001, Lon: 149.2001},
		{Time: base.Add(5 * time.Hour), Lat: -20.2000, Lon: 149.2000},
	}

	got, stationary := computeStationarySince(points)
	if !stationary {
		t.Fatalf("expected the vessel to be reported as stationary once settled, got stationary=false")
	}
	want := base.Add(3 * time.Hour)
	if !got.Equal(want) {
		t.Fatalf("expected stationary_since=%s (first point after the 3 consecutive far points), got %s", want, got)
	}
}

// TestComputeStationarySince_UnsortedInputIsSortedFirst confirms the
// function does not assume its input already arrives in time order.
func TestComputeStationarySince_UnsortedInputIsSortedFirst(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	inOrder := []signalKHistoryPoint{
		{Time: base, Lat: -20.20, Lon: 149.20},
		{Time: base.Add(1 * time.Hour), Lat: -20.2001, Lon: 149.2001},
		{Time: base.Add(2 * time.Hour), Lat: -20.2000, Lon: 149.2000},
	}
	shuffled := []signalKHistoryPoint{inOrder[2], inOrder[0], inOrder[1]}

	got, stationary := computeStationarySince(shuffled)
	if !stationary {
		t.Fatalf("expected a tightly-clustered history to be reported as stationary, got stationary=false")
	}
	if !got.Equal(inOrder[0].Time) {
		t.Fatalf("expected the oldest point's time (%s) regardless of input order, got %s", inOrder[0].Time, got)
	}
}

// TestComputeStationarySince_ContinuousMotionNeverReportsFalseStationary is
// the direct regression test for a code-review finding (2026-09-25):
// computeStationarySince took the median of the most recent
// assistantStationaryCentreWindow points as "current position" without ever
// confirming the vessel is STILL there, so a vessel continuously under way
// in a roughly straight line got a false, recent stationary_since. The
// points below are evenly spaced along a line, each ~1.4km from the next -
// far more than assistantStationaryThresholdMeters - so the window's own
// median lands almost exactly on the middle sample (w[2] of the 5-point
// window) purely because the path is linear and the window length is odd:
// the exact coincidence the previous version mistook for "arrived here."
func TestComputeStationarySince_ContinuousMotionNeverReportsFalseStationary(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	points := []signalKHistoryPoint{
		{Time: base, Lat: -20.20, Lon: 149.20},
		{Time: base.Add(10 * time.Minute), Lat: -20.19, Lon: 149.21},
		{Time: base.Add(20 * time.Minute), Lat: -20.18, Lon: 149.22},
		{Time: base.Add(30 * time.Minute), Lat: -20.17, Lon: 149.23},
		{Time: base.Add(40 * time.Minute), Lat: -20.16, Lon: 149.24},
	}

	_, stationary := computeStationarySince(points)
	if stationary {
		t.Fatalf("expected a vessel continuously under way in a straight line never to be reported as stationary")
	}
}

// TestExecuteGetNearbyVessels_UnderWayVesselIsNotReportedStationary is
// TestComputeStationarySince_ContinuousMotionNeverReportsFalseStationary's
// end-to-end counterpart through get_nearby_vessels: a vessel under way
// gets position_history="under way - not currently stationary", never a
// stationary_since.
func TestExecuteGetNearbyVessels_UnderWayVesselIsNotReportedStationary(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	points := []signalKHistoryPoint{
		{Time: base, Lat: -20.20, Lon: 149.20},
		{Time: base.Add(10 * time.Minute), Lat: -20.19, Lon: 149.21},
		{Time: base.Add(20 * time.Minute), Lat: -20.18, Lon: 149.22},
		{Time: base.Add(30 * time.Minute), Lat: -20.17, Lon: 149.23},
		{Time: base.Add(40 * time.Minute), Lat: -20.16, Lon: 149.24},
	}

	live := []nearbyVessel{{Name: "HOT CHILLI", Mmsi: "234567890", RangeM: 500, Lat: vesselLat, Lon: vesselLon}}
	deps := nearbyVesselsDeps(vesselLat, vesselLon, live)
	deps.signalKPositionHistory = func(mmsi string, from, to time.Time, resolutionSeconds int) ([]signalKHistoryPoint, error) {
		return points, nil
	}

	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{"name":"chilli"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	v := result.Vessels[0]
	if v.StationarySince != nil {
		t.Fatalf("expected no stationary_since for a vessel under way, got %s", *v.StationarySince)
	}
	if v.PositionHistory != "under way - not currently stationary" {
		t.Fatalf(`expected position_history="under way - not currently stationary", got %q`, v.PositionHistory)
	}
}

// TestExecuteGetNearbyVessels_JustArrivedVesselIsReportedStationarySinceArrival
// is the "just arrived" counterpart: a genuine relocation (3+ consecutive
// far points) immediately followed by the vessel settling, checked right at
// the edge of the position-history window rather than hours into the settled
// period (TestExecuteGetNearbyVessels_StationarySinceWalksBackToLastMovement
// already covers that case) - confirming the new "is it still there"
// check (computeStationarySince's recentFar guard) does not itself demand
// more settled samples than the existing consecutive-outliers rule already
// requires.
func TestExecuteGetNearbyVessels_JustArrivedVesselIsReportedStationarySinceArrival(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	base := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	arrived := base.Add(30 * time.Minute)
	points := []signalKHistoryPoint{
		{Time: base, Lat: -19.0, Lon: 148.0},
		{Time: base.Add(10 * time.Minute), Lat: -19.001, Lon: 148.001},
		{Time: base.Add(20 * time.Minute), Lat: -19.002, Lon: 148.002},
		{Time: arrived, Lat: -20.20, Lon: 149.20},
		{Time: arrived.Add(10 * time.Minute), Lat: -20.2001, Lon: 149.2001},
		{Time: arrived.Add(20 * time.Minute), Lat: -20.2000, Lon: 149.2000},
	}

	live := []nearbyVessel{{Name: "HOT CHILLI", Mmsi: "234567890", RangeM: 500, Lat: vesselLat, Lon: vesselLon}}
	deps := nearbyVesselsDeps(vesselLat, vesselLon, live)
	deps.signalKPositionHistory = func(mmsi string, from, to time.Time, resolutionSeconds int) ([]signalKHistoryPoint, error) {
		return points, nil
	}

	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{"name":"chilli"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	v := result.Vessels[0]
	if v.StationarySince == nil {
		t.Fatalf("expected stationary_since to be set for a vessel that has genuinely just arrived and settled")
	}
	want := arrived.UTC().Format(time.RFC3339)
	if *v.StationarySince != want {
		t.Fatalf("expected stationary_since=%s (the moment it arrived), got %s", want, *v.StationarySince)
	}
}

func TestExecuteGetNearbyVessels_PositionHistoryEmptyReportsNoneRecorded(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	live := []nearbyVessel{{Name: "HOT CHILLI", Mmsi: "234567890", RangeM: 500, Lat: vesselLat, Lon: vesselLon}}
	deps := nearbyVesselsDeps(vesselLat, vesselLon, live)
	deps.signalKPositionHistory = func(mmsi string, from, to time.Time, resolutionSeconds int) ([]signalKHistoryPoint, error) {
		return nil, nil
	}

	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{"name":"chilli"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	v := result.Vessels[0]
	if v.PositionHistory != "none recorded" {
		t.Fatalf(`expected position_history="none recorded", got %q`, v.PositionHistory)
	}
	if v.StationarySince != nil {
		t.Fatalf("expected no stationary_since when no history was recorded")
	}
}

func TestExecuteGetNearbyVessels_PositionHistoryErrorSurfacesPerVessel(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	live := []nearbyVessel{{Name: "HOT CHILLI", Mmsi: "234567890", RangeM: 500, Lat: vesselLat, Lon: vesselLon}}
	deps := nearbyVesselsDeps(vesselLat, vesselLon, live)
	deps.signalKPositionHistory = func(mmsi string, from, to time.Time, resolutionSeconds int) ([]signalKHistoryPoint, error) {
		return nil, fmt.Errorf("signalk history endpoint returned status 502: bad gateway")
	}

	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{"name":"chilli"}`))
	if err != nil {
		t.Fatalf("execute: %v (a per-vessel history failure must not fail the whole tool call)", err)
	}
	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	v := result.Vessels[0]
	if v.PositionHistoryError == "" {
		t.Fatalf("expected position_history_error to be set")
	}
	if v.PositionHistory != "" {
		t.Fatalf("expected position_history to stay empty when there was an error, got %q", v.PositionHistory)
	}
}

// TestExecuteGetNearbyVessels_HistoryLookupsRunConcurrentlyAndAreAttributedCorrectly
// is a code-review finding: fillAssistantPositionHistory used to run once
// per matched vessel in a plain sequential loop, each a real blocking HTTP
// round trip, so total latency scaled with the matched-vessel count. This
// confirms three vessels' lookups (a loose filter matching all three) run
// concurrently - total wall time close to one lookup's delay, not the sum of
// three - and that each vessel still gets its own, correctly-attributed
// result (never another vessel's).
func TestExecuteGetNearbyVessels_HistoryLookupsRunConcurrentlyAndAreAttributedCorrectly(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10
	live := []nearbyVessel{
		{Name: "FLEET ONE", Mmsi: "111111111", RangeM: 500, Lat: vesselLat, Lon: vesselLon},
		{Name: "FLEET TWO", Mmsi: "222222222", RangeM: 600, Lat: vesselLat, Lon: vesselLon},
		{Name: "FLEET THREE", Mmsi: "333333333", RangeM: 700, Lat: vesselLat, Lon: vesselLon},
	}
	deps := nearbyVesselsDeps(vesselLat, vesselLon, live)

	const perCallDelay = 60 * time.Millisecond
	// Each mmsi's fake history resolves to a distinct, verifiable
	// stationary_since - not derived from anything already fixed per
	// vessel (like Lat/Lon) - so a result actually landing on the wrong
	// vessel (a goroutine writing to another's slice index) is caught by
	// checking each vessel's own stationary_since against its own mmsi's
	// expected value, not just by counting results.
	byMmsi := map[string]time.Time{
		"111111111": time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		"222222222": time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC),
		"333333333": time.Date(2020, 1, 3, 0, 0, 0, 0, time.UTC),
	}
	deps.signalKPositionHistory = func(mmsi string, from, to time.Time, resolutionSeconds int) ([]signalKHistoryPoint, error) {
		time.Sleep(perCallDelay)
		ts, ok := byMmsi[mmsi]
		if !ok {
			t.Errorf("unexpected mmsi %q", mmsi)
		}
		return []signalKHistoryPoint{{Time: ts, Lat: vesselLat, Lon: vesselLon}}, nil
	}

	start := time.Now()
	raw, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{"name":"fleet"}`))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if elapsed >= 3*perCallDelay {
		t.Fatalf("expected the 3 lookups to run concurrently (elapsed well under %s), took %s", 3*perCallDelay, elapsed)
	}

	var result assistantGetNearbyVesselsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Vessels) != 3 {
		t.Fatalf("expected all 3 fleet vessels, got %d", len(result.Vessels))
	}
	for _, v := range result.Vessels {
		if v.PositionHistoryError != "" {
			t.Fatalf("expected no error for %s, got %q", v.Name, v.PositionHistoryError)
		}
		if v.StationarySince == nil {
			t.Fatalf("expected stationary_since for %s", v.Name)
		}
		want := byMmsi[v.Mmsi].UTC().Format(time.RFC3339)
		if *v.StationarySince != want {
			t.Fatalf("attribution mismatch for mmsi %s: expected stationary_since=%s, got %s (a result landed on the wrong vessel)", v.Mmsi, want, *v.StationarySince)
		}
	}
}

func TestExecuteGetNearbyVessels_VesselStateErrorIsToolError(t *testing.T) {
	deps := assistantToolDeps{
		now:         time.Now,
		vesselState: func() (vesselStateData, error) { return vesselStateData{}, fmt.Errorf("signalk unreachable") },
	}
	_, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{}`))
	if err == nil {
		t.Fatalf("expected an error when the vessel's own position can't be read")
	}
}

func TestExecuteGetNearbyVessels_UnsetNearbyVesselsDependencyReturnsErrorNotPanic(t *testing.T) {
	deps := assistantToolDeps{
		now:         time.Now,
		vesselState: func() (vesselStateData, error) { return vesselStateData{Latitude: -20.1, Longitude: 149.1}, nil },
	}
	_, err := deps.execute(context.Background(), "get_nearby_vessels", json.RawMessage(`{}`))
	if err == nil {
		t.Fatalf("expected an error when d.nearbyVessels is unset")
	}
	if !strings.Contains(err.Error(), "nearby vessel") {
		t.Fatalf("expected a plain \"nearby vessel\" error, got %v", err)
	}
}

// ── describeAssistantToolCall ───────────────────────────────────────────

func TestDescribeAssistantToolCall(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		{"find_places", `{"query":"Tongue Bay"}`, "Looking up Tongue Bay…"},
		{"get_wind_forecast", `{"lat":-20.1,"lon":149.1,"name":"Blue Pearl Bay"}`, "Fetching wind forecast for Blue Pearl Bay…"},
		{"get_wind_forecast", `{"lat":-20.1234,"lon":149.5678}`, "Fetching wind forecast for -20.1234,149.5678…"},
		{"get_tides", `{"lat":-20.1,"lon":149.1,"name":"Blue Pearl Bay"}`, "Fetching tides near Blue Pearl Bay…"},
		{"estimate_passage", `{"distance_nm":42,"speed_kts":8.5}`, "Estimating 42 nm at 8.5 kts from the log…"},
		{"estimate_passage", `{"distance_nm":42}`, "Estimating 42 nm at cruising speed from the log…"},
		{"read_help", `{"page":"features/forecast"}`, "Reading the help: features/forecast…"},
		{"search_documents", `{"query":"impeller"}`, `Searching documents for "impeller"…`},
		{"read_document", `{"document_id":"doc-1"}`, "Reading document doc-1…"},
		{"get_nearby_vessels", `{"name":"Hot Chilli"}`, "Looking up Hot Chilli…"},
		{"get_nearby_vessels", `{}`, "Checking nearby vessels…"},
	}
	for _, tc := range cases {
		got := describeAssistantToolCall(tc.name, json.RawMessage(tc.args))
		if got != tc.want {
			t.Errorf("describeAssistantToolCall(%q, %s) = %q, want %q", tc.name, tc.args, got, tc.want)
		}
	}
}

func TestAssistantToolDefinitions_FindPlacesDescriptionMentionsBareFeatureName(t *testing.T) {
	for _, tool := range assistantToolDefinitions() {
		if tool.Function.Name != "find_places" {
			continue
		}
		if !strings.Contains(tool.Function.Description, "bare feature name") {
			t.Fatalf("expected the find_places description to mention %q, got: %s", "bare feature name", tool.Function.Description)
		}
		return
	}
	t.Fatal("find_places tool definition not found")
}

func TestAssistantToolDefinitions_GetWindForecastDescribesCourseDeg(t *testing.T) {
	for _, tool := range assistantToolDefinitions() {
		if tool.Function.Name != "get_wind_forecast" {
			continue
		}
		if !strings.Contains(string(tool.Function.Parameters), "course_deg") {
			t.Fatalf("expected the get_wind_forecast parameters to mention %q, got: %s", "course_deg", tool.Function.Parameters)
		}
		return
	}
	t.Fatal("get_wind_forecast tool definition not found")
}

func TestAssistantToolDefinitions_GetNearbyVesselsDescriptionMentionsCollisionRisk(t *testing.T) {
	for _, tool := range assistantToolDefinitions() {
		if tool.Function.Name != "get_nearby_vessels" {
			continue
		}
		if !strings.Contains(tool.Function.Description, "collision risk") {
			t.Fatalf("expected the get_nearby_vessels description to mention %q, got: %s", "collision risk", tool.Function.Description)
		}
		return
	}
	t.Fatal("get_nearby_vessels tool definition not found")
}

func TestAssistantToolDefinitions_ElevenToolsIncludingDiagnostics(t *testing.T) {
	tools := assistantToolDefinitions()
	if len(tools) != 11 {
		t.Fatalf("expected 11 tool definitions, got %d: %+v", len(tools), tools)
	}

	var names []string
	for _, tool := range tools {
		names = append(names, tool.Function.Name)
	}
	for _, want := range []string{
		"find_places", "get_wind_forecast", "get_tides", "estimate_passage", "read_help",
		"search_documents", "read_document", "get_nearby_vessels",
		"check_signalk_paths", "get_last_recorded", "get_path_history",
	} {
		found := false
		for _, name := range names {
			if name == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected a %q tool definition, got %v", want, names)
		}
	}
}

// ── dispatch / truncation ───────────────────────────────────────────────

func TestExecute_UnknownToolIsError(t *testing.T) {
	deps := assistantToolDeps{}
	_, err := deps.execute(context.Background(), "not_a_real_tool", json.RawMessage(`{}`))
	if err == nil {
		t.Fatalf("expected an error for an unknown tool name")
	}
}

func TestCapToolResultJSON_TruncatesAtTheCap(t *testing.T) {
	type item struct {
		Filler string `json:"filler"`
	}
	type payload struct {
		Items     []item `json:"items"`
		Truncated bool   `json:"truncated,omitempty"`
	}

	filler := strings.Repeat("x", 500)
	items := make([]item, 40) // ~40 * 510 bytes, comfortably over the 12000-char cap
	for i := range items {
		items[i] = item{Filler: filler}
	}
	result := payload{Items: items}

	shrinkCalls := 0
	shrink := func() bool {
		if len(result.Items) == 0 {
			return false
		}
		result.Items = result.Items[:len(result.Items)-1]
		result.Truncated = true
		shrinkCalls++
		return true
	}

	raw, err := capToolResultJSON(&result, shrink)
	if err != nil {
		t.Fatalf("capToolResultJSON: %v", err)
	}
	if len(raw) > assistantMaxToolResultChars {
		t.Fatalf("expected the result to fit within %d chars, got %d", assistantMaxToolResultChars, len(raw))
	}
	if shrinkCalls == 0 {
		t.Fatalf("expected shrink to be called at least once")
	}

	var decoded payload
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("expected valid JSON after truncation, got error: %v (raw: %s)", err, raw)
	}
	if !decoded.Truncated {
		t.Fatalf("expected truncated=true in the trimmed result")
	}
	if len(decoded.Items) >= 40 {
		t.Fatalf("expected fewer items after truncation, got %d", len(decoded.Items))
	}
}

func TestCapToolResultJSON_FallsBackWhenNothingLeftToShrink(t *testing.T) {
	type payload struct {
		Filler string `json:"filler"`
	}
	result := payload{Filler: strings.Repeat("x", assistantMaxToolResultChars*2)}

	raw, err := capToolResultJSON(&result, nil)
	if err != nil {
		t.Fatalf("capToolResultJSON: %v", err)
	}
	if len(raw) > assistantMaxToolResultChars {
		t.Fatalf("expected the fallback to fit within the cap, got %d chars", len(raw))
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("expected valid JSON fallback, got error: %v", err)
	}
	if decoded["truncated"] != true {
		t.Fatalf("expected truncated=true in the fallback, got %v", decoded)
	}
}

func TestAssistantToolDeps_ExecuteReturnsContextErrorWhenAlreadyCancelled(t *testing.T) {
	deps := assistantToolDeps{help: func() []helpPage {
		return []helpPage{{ID: "features/forecast", Title: "Forecast", Body: "# Forecast"}}
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := deps.execute(ctx, "read_help", json.RawMessage(`{"page":"features/forecast"}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected a cancelled context to short-circuit the tool, got %v", err)
	}
}

// ── search_documents / read_document (ADR 0106) ─────────────────────────

// documentToolDeps builds the minimal assistantToolDeps search_documents and
// read_document need: a real, t.TempDir()-backed *documentStore (these two
// tools' only real dependency, beyond ctx), the same store construction
// documents_store_test.go's own tests use.
func documentToolDeps(t *testing.T) (assistantToolDeps, *documentStore) {
	t.Helper()
	store := newTestDocumentStore(t)
	return assistantToolDeps{documents: func() *documentStore { return store }}, store
}

func insertSearchableDocument(t *testing.T, store *documentStore, sha, filename string, folderID *string, text string) document {
	t.Helper()
	doc, err := store.Insert(document{SHA256: sha, Filename: filename, MIME: "application/pdf", FolderID: folderID})
	if err != nil {
		t.Fatalf("Insert(%q): %v", filename, err)
	}
	if err := store.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", PageStart: 3, Text: text},
	}); err != nil {
		t.Fatalf("ReplaceChunks(%q): %v", filename, err)
	}
	return doc
}

func TestExecuteSearchDocuments_ReturnsShapeWithFolderPathAndCleanSnippet(t *testing.T) {
	deps, store := documentToolDeps(t)

	manuals, err := store.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	engine, err := store.CreateFolder("Engine", &manuals.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	doc := insertSearchableDocument(t, store, "sha-impeller", "yanmar-4jh.pdf", &engine.ID,
		"Replace the raw water impeller every 200 hours of running time.")

	raw, err := deps.execute(context.Background(), "search_documents", json.RawMessage(`{"query":"impeller"}`))
	if err != nil {
		t.Fatalf("execute search_documents: %v", err)
	}

	var result assistantSearchDocumentsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v (raw: %s)", err, raw)
	}
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(result.Results), result.Results)
	}
	hit := result.Results[0]
	if hit.DocumentID != doc.ID {
		t.Fatalf("expected document_id %q, got %q", doc.ID, hit.DocumentID)
	}
	if hit.Filename != "yanmar-4jh.pdf" {
		t.Fatalf("expected filename yanmar-4jh.pdf, got %q", hit.Filename)
	}
	if hit.FolderPath != "Manuals/Engine" {
		t.Fatalf("expected folder_path Manuals/Engine, got %q", hit.FolderPath)
	}
	if hit.Status != "pending" {
		t.Fatalf("expected status pending (no SetIndexed called), got %q", hit.Status)
	}
	if hit.Page != 3 {
		t.Fatalf("expected page 3, got %d", hit.Page)
	}
	if strings.ContainsAny(hit.Snippet, "\x02\x03") {
		t.Fatalf("expected the snippet's \\x02/\\x03 markers to be stripped, got %q", hit.Snippet)
	}
	if !strings.Contains(hit.Snippet, "impeller") {
		t.Fatalf("expected the snippet to contain the matched word, got %q", hit.Snippet)
	}
}

// TestExecuteSearchDocuments_SemanticallyConfiguredFindsVectorOnlyDocument
// is search_documents' own coverage of E1d (documents_hybrid.go): once
// documentSearchReadiness is wired (assistantProductionToolDeps' own wiring,
// checkAssistantReadiness under the hood), the tool finds a document only
// its stored vector matches - the same "a model asking about the marine
// supplies receipt" case the brief gives for why this tool is worth routing
// through hybridDocumentSearch at all - while still returning its
// pre-existing result shape (no mode/semantic_problem field, per
// search_documents' own spec). executeSearchDocuments never sets
// documentSearchParams.Doer itself, so hybridDocumentSearch's
// cachedQueryEmbedding falls back to the package-level openRouterHTTPClient -
// swapped here for the fake, the same idiom
// documents_handlers_test.go's withTestOpenRouterHTTPClient uses for the
// HTTP handler's own equivalent test.
func TestExecuteSearchDocuments_SemanticallyConfiguredFindsVectorOnlyDocument(t *testing.T) {
	resetDocumentQueryEmbedCache(t)
	deps, store := documentToolDeps(t)
	const model = "openai/text-embedding-3-small"
	const dims = 4

	keywordHit := insertSearchableDocument(t, store, "sha-tool-kw", "receipt.pdf", nil, "Whitsunday Marine Supplies receipt AUD 205.45")
	vectorOnly := insertSearchableDocument(t, store, "sha-tool-vec", "notes.txt", nil, "paid at the chandlery on the way south")

	queryVec := []float32{1, 2, 3, 4}
	embedDocChunk(t, store, vectorOnly.ID, model, dims, queryVec)

	doer := &fakeOpenRouterDoer{responses: []*http.Response{queryEmbeddingResponse(t, queryVec)}}
	prevDoer := openRouterHTTPClient
	openRouterHTTPClient = doer
	t.Cleanup(func() { openRouterHTTPClient = prevDoer })
	deps.documentSearchReadiness = hybridTestReadiness(model, dims)

	raw, err := deps.execute(context.Background(), "search_documents", json.RawMessage(`{"query":"receipt"}`))
	if err != nil {
		t.Fatalf("execute search_documents: %v", err)
	}
	var result assistantSearchDocumentsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v (raw: %s)", err, raw)
	}
	ids := map[string]bool{}
	for _, r := range result.Results {
		ids[r.DocumentID] = true
	}
	if !ids[keywordHit.ID] {
		t.Fatalf("expected the keyword hit present, got %+v", result.Results)
	}
	if !ids[vectorOnly.ID] {
		t.Fatalf("expected the vector-only hit (no shared keyword with %q) present, got %+v", "receipt", result.Results)
	}
	if strings.Contains(raw, `"mode"`) || strings.Contains(raw, `"semantic_problem"`) {
		t.Fatalf("expected search_documents' own result shape to carry neither mode nor semantic_problem, got %s", raw)
	}
	if len(doer.requests) != 1 {
		t.Fatalf("expected exactly one embeddings request, got %d", len(doer.requests))
	}
}

func TestExecuteSearchDocuments_FolderArgumentScopesRecursively(t *testing.T) {
	deps, store := documentToolDeps(t)

	manuals, err := store.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	engine, err := store.CreateFolder("Engine", &manuals.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	receipts, err := store.CreateFolder("Receipts", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	inFolder := insertSearchableDocument(t, store, "sha-in", "in-manuals.pdf", &engine.ID, "impeller service notes")
	insertSearchableDocument(t, store, "sha-out", "in-receipts.pdf", &receipts.ID, "impeller purchase receipt")

	raw, err := deps.execute(context.Background(), "search_documents", json.RawMessage(`{"query":"impeller","folder":"manuals"}`))
	if err != nil {
		t.Fatalf("execute search_documents: %v", err)
	}
	var result assistantSearchDocumentsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.Results) != 1 || result.Results[0].DocumentID != inFolder.ID {
		t.Fatalf("expected only the document under Manuals (case-insensitive path match, recursive into Engine), got %+v", result.Results)
	}
}

func TestExecuteSearchDocuments_UnknownFolderNamesTopLevelFolders(t *testing.T) {
	deps, store := documentToolDeps(t)
	if _, err := store.CreateFolder("Manuals", nil); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := store.CreateFolder("Receipts", nil); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	_, err := deps.execute(context.Background(), "search_documents", json.RawMessage(`{"query":"impeller","folder":"Nope"}`))
	if err == nil {
		t.Fatalf("expected an error for an unknown folder")
	}
	if !strings.Contains(err.Error(), "Manuals") || !strings.Contains(err.Error(), "Receipts") {
		t.Fatalf("expected the error to name the top-level folders, got %v", err)
	}
}

func TestExecuteSearchDocuments_LimitCappedAtTen(t *testing.T) {
	deps, store := documentToolDeps(t)
	for i := 0; i < 15; i++ {
		insertSearchableDocument(t, store, fmt.Sprintf("sha-limit-%d", i), fmt.Sprintf("doc-%d.pdf", i), nil, "impeller service manual")
	}

	raw, err := deps.execute(context.Background(), "search_documents", json.RawMessage(`{"query":"impeller","limit":50}`))
	if err != nil {
		t.Fatalf("execute search_documents: %v", err)
	}
	var result assistantSearchDocumentsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.Results) != 10 {
		t.Fatalf("expected the limit capped at 10, got %d", len(result.Results))
	}
}

func TestExecuteSearchDocuments_DefaultLimitIsFive(t *testing.T) {
	deps, store := documentToolDeps(t)
	for i := 0; i < 8; i++ {
		insertSearchableDocument(t, store, fmt.Sprintf("sha-default-%d", i), fmt.Sprintf("doc-%d.pdf", i), nil, "impeller service manual")
	}

	raw, err := deps.execute(context.Background(), "search_documents", json.RawMessage(`{"query":"impeller"}`))
	if err != nil {
		t.Fatalf("execute search_documents: %v", err)
	}
	var result assistantSearchDocumentsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.Results) != 5 {
		t.Fatalf("expected the default limit of 5, got %d", len(result.Results))
	}
}

// TestExecuteSearchDocuments_TwoHitsInSameNestedFolderResolvePathsCorrectly
// pins the review finding at assistant_tools.go:1625: documentFolderPathString
// used to run a Get+FolderPath per search hit; the fix carries FolderID on
// the search row itself (documentSearchResult) and resolves each distinct
// folder id once, caching within the call. Two documents sharing the same
// nested folder is exactly the case that exercises the cache - this asserts
// on the actual resolved path for both (nested two levels deep), not on an
// internal call count, so a wrong cache key or an off-by-one in the chain
// join would still be caught.
func TestExecuteSearchDocuments_TwoHitsInSameNestedFolderResolvePathsCorrectly(t *testing.T) {
	deps, store := documentToolDeps(t)

	manuals, err := store.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	engine, err := store.CreateFolder("Engine", &manuals.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	docA := insertSearchableDocument(t, store, "sha-nested-a", "impeller-a.pdf", &engine.ID, "impeller service notes A")
	docB := insertSearchableDocument(t, store, "sha-nested-b", "impeller-b.pdf", &engine.ID, "impeller service notes B")

	raw, err := deps.execute(context.Background(), "search_documents", json.RawMessage(`{"query":"impeller","limit":10}`))
	if err != nil {
		t.Fatalf("execute search_documents: %v", err)
	}
	var result assistantSearchDocumentsResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.Results) != 2 {
		t.Fatalf("expected both documents in the Engine folder, got %d: %+v", len(result.Results), result.Results)
	}
	seen := map[string]string{}
	for _, hit := range result.Results {
		seen[hit.DocumentID] = hit.FolderPath
	}
	if seen[docA.ID] != "Manuals/Engine" {
		t.Fatalf("expected doc A's folder_path Manuals/Engine, got %q", seen[docA.ID])
	}
	if seen[docB.ID] != "Manuals/Engine" {
		t.Fatalf("expected doc B's folder_path Manuals/Engine, got %q", seen[docB.ID])
	}
}

// TestExecuteSearchDocuments_FolderPathStoreErrorSurfacesAsToolError pins
// the other half of the same finding: a FolderPath failure used to
// collapse to folder_path="" (documentFolderPathString's old doc comment
// called this "decoration" not worth failing the search over) - masking a
// genuine database failure as an empty, silently-wrong result. Dropping
// document_folders out from under a live search forces FolderPath itself to
// fail (Search's own query never touches that table when no folder
// argument is given, so this isolates the failure to path resolution).
func TestExecuteSearchDocuments_FolderPathStoreErrorSurfacesAsToolError(t *testing.T) {
	deps, store := documentToolDeps(t)

	manuals, err := store.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	insertSearchableDocument(t, store, "sha-broken-folder", "impeller.pdf", &manuals.ID, "impeller service notes")

	// foreign_keys is off only for this one DROP - store.db is capped at a
	// single pooled connection (newDocumentStore's SetMaxOpenConns(1)), so
	// this and every query after it on the same *documentStore share it,
	// same as production would.
	if _, err := store.db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatalf("disable foreign_keys: %v", err)
	}
	if _, err := store.db.Exec(`DROP TABLE document_folders`); err != nil {
		t.Fatalf("drop document_folders: %v", err)
	}

	_, err = deps.execute(context.Background(), "search_documents", json.RawMessage(`{"query":"impeller"}`))
	if err == nil {
		t.Fatalf("expected a store error to surface as a tool error")
	}
}

// ── read_document ────────────────────────────────────────────────────────

func TestExecuteReadDocument_DefaultStartsAfterMetaChunk(t *testing.T) {
	deps, store := documentToolDeps(t)
	doc, err := store.Insert(document{SHA256: "sha-read-1", Filename: "manual.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := store.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Heading: "Maintenance", Text: "first chunk"},
		{Seq: 2, Source: "local", Text: "second chunk"},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	raw, err := deps.execute(context.Background(), "read_document", json.RawMessage(fmt.Sprintf(`{"document_id":%q}`, doc.ID)))
	if err != nil {
		t.Fatalf("execute read_document: %v", err)
	}
	var result assistantReadDocumentResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v (raw: %s)", err, raw)
	}
	if result.DocumentID != doc.ID || result.Filename != "manual.pdf" {
		t.Fatalf("unexpected document_id/filename: %+v", result)
	}
	if len(result.Chunks) != 2 {
		t.Fatalf("expected 2 body chunks (meta chunk seq 0 excluded), got %d: %+v", len(result.Chunks), result.Chunks)
	}
	if result.Chunks[0].Seq != 1 || result.Chunks[0].Heading != "Maintenance" || result.Chunks[0].Text != "first chunk" {
		t.Fatalf("unexpected first chunk: %+v", result.Chunks[0])
	}
	if result.Truncated {
		t.Fatalf("expected no truncation when everything fits")
	}
	if result.NextChunk != 0 {
		t.Fatalf("expected no next_chunk when everything fit, got %d", result.NextChunk)
	}
}

func TestExecuteReadDocument_FromChunkResumesAtGivenSeq(t *testing.T) {
	deps, store := documentToolDeps(t)
	doc, err := store.Insert(document{SHA256: "sha-read-2", Filename: "manual.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := store.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "first"},
		{Seq: 2, Source: "local", Text: "second"},
		{Seq: 3, Source: "local", Text: "third"},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	raw, err := deps.execute(context.Background(), "read_document", json.RawMessage(fmt.Sprintf(`{"document_id":%q,"from_chunk":3}`, doc.ID)))
	if err != nil {
		t.Fatalf("execute read_document: %v", err)
	}
	var result assistantReadDocumentResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.Chunks) != 1 || result.Chunks[0].Text != "third" {
		t.Fatalf("expected only the chunk at seq 3, got %+v", result.Chunks)
	}
}

func TestExecuteReadDocument_TruncatesAndReportsNextChunk(t *testing.T) {
	deps, store := documentToolDeps(t)
	doc, err := store.Insert(document{SHA256: "sha-read-3", Filename: "big.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	// 10 chunks of 2000 chars each (~20000 bytes of JSON once encoded),
	// comfortably over assistantMaxToolResultChars (12000) - enough to force
	// at least one halving.
	chunks := make([]documentChunk, 10)
	for i := range chunks {
		chunks[i] = documentChunk{Seq: i + 1, Source: "local", Text: strings.Repeat("x", 2000)}
	}
	if err := store.ReplaceChunks(doc.ID, []string{"local"}, chunks); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	raw, err := deps.execute(context.Background(), "read_document", json.RawMessage(fmt.Sprintf(`{"document_id":%q}`, doc.ID)))
	if err != nil {
		t.Fatalf("execute read_document: %v", err)
	}
	if len(raw) > assistantMaxToolResultChars {
		t.Fatalf("expected the result to fit within %d chars, got %d", assistantMaxToolResultChars, len(raw))
	}
	var result assistantReadDocumentResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v (raw: %s)", err, raw)
	}
	if !result.Truncated {
		t.Fatalf("expected truncated=true, got %+v", result)
	}
	if len(result.Chunks) == 0 || len(result.Chunks) >= 10 {
		t.Fatalf("expected fewer than 10 chunks after truncation, got %d", len(result.Chunks))
	}
	if result.NextChunk <= result.Chunks[len(result.Chunks)-1].Seq {
		t.Fatalf("expected next_chunk to point past the last returned chunk, got next_chunk=%d last=%d", result.NextChunk, result.Chunks[len(result.Chunks)-1].Seq)
	}
}

func TestExecuteReadDocument_UnknownDocumentIDIsError(t *testing.T) {
	deps, _ := documentToolDeps(t)
	_, err := deps.execute(context.Background(), "read_document", json.RawMessage(`{"document_id":"does-not-exist"}`))
	if err == nil {
		t.Fatalf("expected an error for an unknown document id")
	}
}

// ── nil dependency func fields report a plain error, not a panic ─────────

// TestExecuteSearchDocuments_UnsetDependencyReturnsErrorNotPanic pins the
// review finding at assistant_tools.go:1571: d.documents is a func field
// (assistantToolDeps' zero value leaves it nil, a real possibility per its
// own doc comment), and calling a nil func panics - the panic used to only
// be caught by the goroutine recover further up assistant_run.go's call
// stack, killing the run rather than reporting the plain "not available"
// error the doc comment promises.
func TestExecuteSearchDocuments_UnsetDependencyReturnsErrorNotPanic(t *testing.T) {
	deps := assistantToolDeps{}
	_, err := deps.execute(context.Background(), "search_documents", json.RawMessage(`{"query":"impeller"}`))
	if err == nil {
		t.Fatalf("expected an error when d.documents is unset")
	}
	if !strings.Contains(err.Error(), "document library") {
		t.Fatalf("expected a plain \"document library\" error, got %v", err)
	}
}

// TestExecuteReadDocument_UnsetDependencyReturnsErrorNotPanic is
// read_document's half of the same finding - d.documents is called at
// assistant_tools.go:1702 the same way search_documents calls it at 1571.
func TestExecuteReadDocument_UnsetDependencyReturnsErrorNotPanic(t *testing.T) {
	deps := assistantToolDeps{}
	_, err := deps.execute(context.Background(), "read_document", json.RawMessage(`{"document_id":"doc-1"}`))
	if err == nil {
		t.Fatalf("expected an error when d.documents is unset")
	}
	if !strings.Contains(err.Error(), "document library") {
		t.Fatalf("expected a plain \"document library\" error, got %v", err)
	}
}

// TestExecuteReadHelp_UnsetDependencyReturnsErrorNotPanic pins the
// review finding at assistant_tools.go:1475-1476: d.help is a func field
// too, and executeReadHelp called it unconditionally before ever checking
// whether it was set.
func TestExecuteReadHelp_UnsetDependencyReturnsErrorNotPanic(t *testing.T) {
	deps := assistantToolDeps{}
	_, err := deps.execute(context.Background(), "read_help", json.RawMessage(`{"page":"features/forecast"}`))
	if err == nil {
		t.Fatalf("expected an error when d.help is unset")
	}
	if !strings.Contains(err.Error(), "help") {
		t.Fatalf("expected a plain \"help\" error, got %v", err)
	}
}
