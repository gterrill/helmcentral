package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		{"read_manual", `{"page":"features/forecast"}`, "Reading the manual: features/forecast…"},
		{"search_documents", `{"query":"impeller"}`, `Searching documents for "impeller"…`},
		{"read_document", `{"document_id":"doc-1"}`, "Reading document doc-1…"},
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

func TestAssistantToolDefinitions_SevenToolsIncludingDocumentTools(t *testing.T) {
	tools := assistantToolDefinitions()
	if len(tools) != 7 {
		t.Fatalf("expected 7 tool definitions, got %d: %+v", len(tools), tools)
	}

	var names []string
	for _, tool := range tools {
		names = append(names, tool.Function.Name)
	}
	for _, want := range []string{
		"find_places", "get_wind_forecast", "get_tides", "estimate_passage", "read_manual",
		"search_documents", "read_document",
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
	deps := assistantToolDeps{manual: func() []manualPage {
		return []manualPage{{ID: "features/forecast", Title: "Forecast", Body: "# Forecast"}}
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := deps.execute(ctx, "read_manual", json.RawMessage(`{"page":"features/forecast"}`))
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

// TestExecuteReadManual_UnsetDependencyReturnsErrorNotPanic pins the
// review finding at assistant_tools.go:1475-1476: d.manual is a func field
// too, and executeReadManual called it unconditionally before ever checking
// whether it was set.
func TestExecuteReadManual_UnsetDependencyReturnsErrorNotPanic(t *testing.T) {
	deps := assistantToolDeps{}
	_, err := deps.execute(context.Background(), "read_manual", json.RawMessage(`{"page":"features/forecast"}`))
	if err == nil {
		t.Fatalf("expected an error when d.manual is unset")
	}
	if !strings.Contains(err.Error(), "manual") {
		t.Fatalf("expected a plain \"manual\" error, got %v", err)
	}
}
