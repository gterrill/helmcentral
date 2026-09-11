package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// ── find_places ─────────────────────────────────────────────────────────

func TestExecuteFindPlaces_MergesRouteWaypointAndOSMBayDedupesAndSorts(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10

	// OSM's own copy of the same Tongue Bay, ~110m from the waypoint - well
	// inside the 500m dedupe radius, so it must collapse into the waypoint
	// entry (added first) rather than appearing a second time.
	tongueBayOSMLat, tongueBayOSMLon := vesselLat+0.001, vesselLon

	// A second, clearly distinct bay ~11km (~6nm) away, so sort-by-distance
	// has something to prove.
	bluePearlLat, bluePearlLon := vesselLat+0.1, vesselLon

	overpassBody := fmt.Sprintf(`{"elements":[
		{"type":"node","id":1,"lat":%.6f,"lon":%.6f,"tags":{"name":"Tongue Bay","natural":"bay"}},
		{"type":"node","id":2,"lat":%.6f,"lon":%.6f,"tags":{"name":"Blue Pearl Bay","natural":"bay"}}
	]}`, tongueBayOSMLat, tongueBayOSMLon, bluePearlLat, bluePearlLon)

	radiusMeters := int(assistantFindPlacesRadiusNm * metersPerNauticalMile)
	fetcher := &fakeOverpassFetcher{fixtures: map[int][]byte{radiusMeters: []byte(overpassBody)}}

	deps := assistantToolDeps{
		vesselState: func() (vesselStateData, error) {
			return vesselStateData{Latitude: vesselLat, Longitude: vesselLon}, nil
		},
		overpass: fetcher,
		routes: func() []routeData {
			return []routeData{{
				Name: "Whitsundays Loop",
				Waypoints: []routeWaypoint{
					{Name: "Tongue Bay", Lat: vesselLat, Lon: vesselLon},
				},
			}}
		},
	}

	raw, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Bay"}`))
	if err != nil {
		t.Fatalf("execute find_places: %v", err)
	}

	var result assistantFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v (raw: %s)", err, raw)
	}

	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results (Tongue Bay deduped, Blue Pearl Bay distinct), got %d: %+v", len(result.Results), result.Results)
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
	if result.Results[1].Name != "Blue Pearl Bay" {
		t.Errorf("expected Blue Pearl Bay second (farther away), got %q", result.Results[1].Name)
	}
	if result.Results[1].DistanceNm <= result.Results[0].DistanceNm {
		t.Errorf("expected Blue Pearl Bay to be sorted after the nearer Tongue Bay")
	}
	if result.Results[1].BearingDeg < 0 || result.Results[1].BearingDeg > 360 {
		t.Errorf("expected a valid compass bearing, got %d", result.Results[1].BearingDeg)
	}
	if result.Results[1].Source != "osm" {
		t.Errorf("expected the OSM-only result's source to be osm, got %q", result.Results[1].Source)
	}
}

func TestBuildOverpassNameSearchQuery_EscapesRegexMetacharacters(t *testing.T) {
	got := buildOverpassNameSearchQuery("Hook Reef (outer)", 185200, -20.1, 149.1)

	want := `Hook Reef \\(outer\\)`
	if !strings.Contains(got, want) {
		t.Fatalf("expected the query to contain the escaped literal %q, got: %s", want, got)
	}
	if strings.Contains(got, `"Hook Reef (outer)"`) {
		t.Fatalf("expected the parentheses to be escaped rather than passed through raw: %s", got)
	}
}

func TestExecuteFindPlaces_NoFixAndNoNearArgsReturnsNoteNotError(t *testing.T) {
	fetcher := &fakeOverpassFetcher{}
	deps := assistantToolDeps{
		vesselState: func() (vesselStateData, error) { return vesselStateData{Latitude: -1, Longitude: -1}, nil },
		overpass:    fetcher,
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
	if fetcher.callCount() != 0 {
		t.Fatalf("expected no overpass call when there is no usable fix, got %d", fetcher.callCount())
	}
}

func TestExecuteFindPlaces_OverpassRateLimitIsError(t *testing.T) {
	radiusMeters := int(assistantFindPlacesRadiusNm * metersPerNauticalMile)
	fetcher := &fakeOverpassFetcher{
		fixtures: map[int][]byte{radiusMeters: []byte(`<html><body>Dispatcher_Client::request_read_and_idx::rate_limited</body></html>`)},
		types:    map[int]string{radiusMeters: "text/html"},
	}
	deps := assistantToolDeps{
		vesselState: func() (vesselStateData, error) { return vesselStateData{Latitude: -20.1, Longitude: 149.1}, nil },
		overpass:    fetcher,
		routes:      func() []routeData { return nil },
	}

	_, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Bay"}`))
	if err == nil {
		t.Fatalf("expected an error for a rate-limited overpass response")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "rate limit") {
		t.Fatalf("expected the error to mention rate limiting, got %q", err.Error())
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
	}
	for _, tc := range cases {
		got := describeAssistantToolCall(tc.name, json.RawMessage(tc.args))
		if got != tc.want {
			t.Errorf("describeAssistantToolCall(%q, %s) = %q, want %q", tc.name, tc.args, got, tc.want)
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
