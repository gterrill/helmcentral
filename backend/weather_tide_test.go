package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestWriteJSONFileAtomic_RoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "data.json")
	want := map[string]int{"a": 1, "b": 2}

	if err := writeJSONFileAtomic(path, want); err != nil {
		t.Fatalf("writeJSONFileAtomic: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	var got map[string]int
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal written file: %v", err)
	}
	if got["a"] != 1 || got["b"] != 2 {
		t.Fatalf("unexpected content: %+v", got)
	}

	// No leftover temp file in the same directory - CreateTemp's file must
	// have been renamed away, not merely written and left behind.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "data.json" {
		t.Fatalf("expected exactly one file (data.json) in the directory, got %+v", entries)
	}
}

// TestWriteJSONFileAtomic_ConcurrentWritesNeverCorruptTheFile is the direct
// regression test for the write race this fix addresses: the old
// implementation copied under a read lock, then wrote to a FIXED
// "path+.tmp" name with no lock held during the write itself, so two
// concurrent writers could interleave their bytes into the same temp file
// before either renamed - reproducing the corrupt
// weather_wasm_weatherkit_cache.json ("Extra data" partway through) found on
// the laptop. Every writer here targets the exact same path concurrently;
// the file must always be valid, parseable JSON afterward, whichever
// writer's content happened to win the last rename.
func TestWriteJSONFileAtomic_ConcurrentWritesNeverCorruptTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.json")

	const writers = 50
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// A payload large enough that a naive interleaved write (rather
			// than each writer getting its own temp file) would be very
			// likely to produce visibly truncated/mixed JSON.
			values := make([]int, 500)
			for j := range values {
				values[j] = n*1000 + j
			}
			if err := writeJSONFileAtomic(path, map[string]any{"writer": n, "values": values}); err != nil {
				t.Errorf("writeJSONFileAtomic: %v", err)
			}
		}(i)
	}
	wg.Wait()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read final file: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("final file is not valid JSON (corrupted by a concurrent write): %v\ncontent: %s", err, raw)
	}
	if _, ok := decoded["writer"]; !ok {
		t.Fatalf("expected a complete object with a \"writer\" key, got %+v", decoded)
	}
}

func writeTideTodaySettings(t *testing.T, provider, stationID string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.yaml")
	content := "ui:\n  tide_provider: " + provider + "\n  tide_station_id: " + stationID + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test settings file: %v", err)
	}
	return path
}

func TestTideToday_ReturnsBadGatewayWhenProviderFetchFails(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(&stubTideProvider{id: "bom", fetchErr: errors.New("simulated fetch failure")})

	settingsPath := writeTideTodaySettings(t, "bom", "DOES_NOT_EXIST_12345")
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/tide-today", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := tideToday(c); err != nil {
		t.Fatalf("tideToday returned error: %v", err)
	}

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d when no real tide data is available, got %d (body: %s)", http.StatusBadGateway, rec.Code, rec.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected a non-empty error message, got %+v", payload)
	}
}

func TestTideToday_ReturnsOKWithProviderData(t *testing.T) {
	withCleanTideProviderRegistry(t)

	station := tideStation{StationID: "TEST_STATION", Name: "Test Harbour"}
	now := time.Now().UTC()
	fakeResult := tideChartResult{
		Station: station,
		Extremes: []tideExtremePoint{
			{Time: now.Add(-2 * time.Hour), HeightM: 1.0, High: false},
			{Time: now.Add(4 * time.Hour), HeightM: 2.0, High: true},
		},
		CurrentHeightM: 1.5,
		Direction:      "Rising",
		CachedAt:       now,
	}
	registerTideProvider(&stubTideProvider{id: "bom", result: fakeResult})

	settingsPath := writeTideTodaySettings(t, "bom", station.StationID)
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/tide-today", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := tideToday(c); err != nil {
		t.Fatalf("tideToday returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var payload tideTodayResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if payload.Provider != "bom" {
		t.Fatalf("expected provider bom, got %q", payload.Provider)
	}
	if payload.StationName != station.Name {
		t.Fatalf("expected station name %q, got %q", station.Name, payload.StationName)
	}
}

// TestTideToday_ReportsAZeroMetreLowWithoutBeingOverwritten guards the fix
// for the old `state.LowTideHeightFt == 0` "not found yet" sentinel: a real
// 0.0 m low is indistinguishable from "not found" under that check, so the
// *next* future extreme in the list (here a second, later low) would
// overwrite it even though the first low was already found. Found state now
// tracked with an explicit bool, so the first future low found wins and
// keeps its own height and time regardless of what it's worth.
func TestTideToday_ReportsAZeroMetreLowWithoutBeingOverwritten(t *testing.T) {
	withCleanTideProviderRegistry(t)

	station := tideStation{StationID: "TEST_STATION", Name: "Test Harbour"}
	now := time.Now().UTC()
	firstLowTime := now.Add(1 * time.Hour)
	secondLowTime := now.Add(3 * time.Hour)
	fakeResult := tideChartResult{
		Station: station,
		Extremes: []tideExtremePoint{
			{Time: firstLowTime, HeightM: 0.0, High: false},
			{Time: secondLowTime, HeightM: 1.0, High: false},
		},
		CurrentHeightM: 1.5,
		Direction:      "Falling",
		CachedAt:       now,
	}
	registerTideProvider(&stubTideProvider{id: "bom", result: fakeResult})

	settingsPath := writeTideTodaySettings(t, "bom", station.StationID)
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/tide-today", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := tideToday(c); err != nil {
		t.Fatalf("tideToday returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var payload tideTodayResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if payload.LowTideHeightFt == nil || *payload.LowTideHeightFt != 0 {
		t.Fatalf("expected the real 0.0 m low to be reported as 0, got %v", payload.LowTideHeightFt)
	}
	// RFC3339 (the wire format) only carries second precision, so compare at
	// that precision rather than against firstLowTime's raw nanoseconds.
	if payload.LowTideTime != firstLowTime.Format(time.RFC3339) {
		t.Fatalf("expected low_tide_time to stay the first future low (%s), got %s — the second low overwrote it", firstLowTime.Format(time.RFC3339), payload.LowTideTime)
	}
}

// TestTideToday_ReportsSentinelWhenNoFutureLowExists guards the other half
// of the same fix: when the extremes list has no future low at all, the
// response must send the -1 sentinel useTideToday already treats as "not
// published," not 0 at a made-up time 24h out — the old default a real 0.0 m
// low was indistinguishable from.
func TestTideToday_ReportsSentinelWhenNoFutureLowExists(t *testing.T) {
	withCleanTideProviderRegistry(t)

	station := tideStation{StationID: "TEST_STATION", Name: "Test Harbour"}
	now := time.Now().UTC()
	fakeResult := tideChartResult{
		Station: station,
		Extremes: []tideExtremePoint{
			// Only a future high and a past low - no future low at all.
			{Time: now.Add(-2 * time.Hour), HeightM: 1.0, High: false},
			{Time: now.Add(4 * time.Hour), HeightM: 2.0, High: true},
		},
		CurrentHeightM: 1.5,
		Direction:      "Rising",
		CachedAt:       now,
	}
	registerTideProvider(&stubTideProvider{id: "bom", result: fakeResult})

	settingsPath := writeTideTodaySettings(t, "bom", station.StationID)
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/tide-today", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := tideToday(c); err != nil {
		t.Fatalf("tideToday returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var payload tideTodayResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if payload.LowTideHeightFt != nil {
		t.Fatalf("expected null when no future low exists, got %v", *payload.LowTideHeightFt)
	}
}

// TestTideToday_SendsEmptyTimeWhenNoFutureExtremeExists is the other half of
// the code-review finding TestTideToday_ReportsSentinelWhenNoFutureLowExists
// only partly covered: the height sentinel was fixed, but low_tide_time (and
// high_tide_time, symmetrically) still fell back to a fabricated `now` /
// `now+24h` rather than staying blank - a time an operator reads as real (a
// fake "Low <tomorrow>" on the Depth & Tide tile, a fake "High · <now>" on
// the Anchor Watch header). A missing extreme must report an empty time
// string alongside its -1 height, not an invented one.
func TestTideToday_SendsEmptyTimeWhenNoFutureExtremeExists(t *testing.T) {
	withCleanTideProviderRegistry(t)

	station := tideStation{StationID: "TEST_STATION", Name: "Test Harbour"}
	now := time.Now().UTC()
	fakeResult := tideChartResult{
		Station: station,
		Extremes: []tideExtremePoint{
			// Only a future high and a past low - no future low at all.
			{Time: now.Add(-2 * time.Hour), HeightM: 1.0, High: false},
			{Time: now.Add(4 * time.Hour), HeightM: 2.0, High: true},
		},
		CurrentHeightM: 1.5,
		Direction:      "Rising",
		CachedAt:       now,
	}
	registerTideProvider(&stubTideProvider{id: "bom", result: fakeResult})

	settingsPath := writeTideTodaySettings(t, "bom", station.StationID)
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/tide-today", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := tideToday(c); err != nil {
		t.Fatalf("tideToday returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var payload tideTodayResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if payload.LowTideTime != "" {
		t.Fatalf("expected an empty low_tide_time when no future low exists, got %q", payload.LowTideTime)
	}
	// The future high in this fixture DOES exist, so it must still report a
	// real time - this guards against a fix that blanks both fields
	// unconditionally instead of tracking found/not-found per extreme.
	if payload.HighTideTime == "" {
		t.Fatalf("expected a real high_tide_time since a future high exists, got empty")
	}
}

func TestTideToday_ReturnsBadGatewayForUnknownProvider(t *testing.T) {
	withCleanTideProviderRegistry(t)
	settingsPath := writeTideTodaySettings(t, "not-a-real-provider", "ANY")
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/tide-today", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := tideToday(c); err != nil {
		t.Fatalf("tideToday returned error: %v", err)
	}

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d for an unknown configured provider, got %d (body: %s)", http.StatusBadGateway, rec.Code, rec.Body.String())
	}
}

// TestTideToday_ReturnsBadGatewayWhenNoTideProvidersRegistered guards the
// intended default state for a fresh install: tides are WASM-plugin-only
// (see backend/tide_providers.go), so with no plugins/tides/*.wasm present
// the registry is empty - not silently populated by any built-in provider -
// and configuring a real-looking provider id (e.g. "bom", before its plugin
// is actually installed) must still 502 with a clear, actionable message
// rather than crash or silently succeed.
func TestTideToday_ReturnsBadGatewayWhenNoTideProvidersRegistered(t *testing.T) {
	withCleanTideProviderRegistry(t)

	if len(tideProviderRegistry) != 0 {
		t.Fatalf("expected an empty tide provider registry with no plugins loaded, got %d entries", len(tideProviderRegistry))
	}

	settingsPath := writeTideTodaySettings(t, "bom", "ANY_STATION")
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/tide-today", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := tideToday(c); err != nil {
		t.Fatalf("tideToday returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d when the tide provider registry is empty, got %d (body: %s)", http.StatusBadGateway, rec.Code, rec.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if !strings.Contains(payload["error"], "unknown tide provider configured") {
		t.Fatalf("expected a clear 'unknown tide provider' error, got %q", payload["error"])
	}
}

// TestTideToday_ReturnsBadGatewayWhenProviderEmpty guards the fix for the
// bug where an empty ui.tide_provider silently defaulted to a hardcoded
// provider id, which then 502'd anyway on any install lacking that
// provider's credentials, with no indication of what to actually configure.
// Empty is now treated the same as "not configured", with an actionable
// error.
func TestTideToday_ReturnsBadGatewayWhenProviderEmpty(t *testing.T) {
	withCleanTideProviderRegistry(t)

	path := filepath.Join(t.TempDir(), "settings.yaml")
	if err := os.WriteFile(path, []byte("ui:\n  tide_station_id: ANY\n"), 0o644); err != nil {
		t.Fatalf("failed to write test settings file: %v", err)
	}
	t.Setenv("SETTINGS_FILE", path)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/tide-today", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := tideToday(c); err != nil {
		t.Fatalf("tideToday returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d when no tide provider is configured, got %d (body: %s)", http.StatusBadGateway, rec.Code, rec.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected a non-empty, actionable error message, got %+v", payload)
	}
}

// TestTideToday_ReturnsBadGatewayWhenStationEmpty guards the fix for a live
// bug: ui.tide_station_id used to silently default to a hardcoded
// pseudo-station id regardless of which provider was actually configured,
// so a BOM/NOAA user who hadn't picked a real station got a confusing
// "unknown BOM tide station: <that id>" error instead of a clear "you
// haven't picked a station yet" message. Now that every registered provider
// is station-based (tides are WASM-plugin-only, and every plugin
// implementation is a real station catalog, never a pseudo-station), an
// empty station id must always 502 with an actionable message.
func TestTideToday_ReturnsBadGatewayWhenStationEmpty(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(&stubTideProvider{id: "bom"})

	settingsPath := writeTideTodaySettings(t, "bom", "")
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/tide-today", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := tideToday(c); err != nil {
		t.Fatalf("tideToday returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d when no tide station is configured, got %d (body: %s)", http.StatusBadGateway, rec.Code, rec.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected a non-empty, actionable error message, got %+v", payload)
	}
}

func TestForecastStartUsesLocalTimezoneForDateLabels(t *testing.T) {
	loc := time.FixedZone("AEST", 10*60*60)
	forecastStart := "2026-06-14T00:30:00Z"
	parsed, err := time.Parse(time.RFC3339, forecastStart)
	if err != nil {
		t.Fatalf("expected valid RFC3339 timestamp: %v", err)
	}

	localized := parsed.In(loc)
	if got := localized.Format("Jan 2"); got != "Jun 14" {
		t.Fatalf("expected date label Jun 14, got %s", got)
	}
	if got := localized.Weekday().String(); got != "Sunday" {
		t.Fatalf("expected weekday Sunday, got %s", got)
	}
}

func TestMapWeatherHourlyEntryResponse_IncludesWindFields(t *testing.T) {
	entries := []weatherHourlyEntryData{
		{Label: "Now", Condition: "Clear", TemperatureF: 68, WindSpeedKts: 10, WindGustKts: 15, WindDirection: "E", WindDirectionDeg: 90, Kind: "forecast"},
	}

	response := mapWeatherHourlyEntryResponse(entries)

	if len(response) != 1 {
		t.Fatalf("expected 1 response entry, got %d", len(response))
	}
	if response[0].WindSpeedKts != 10 || response[0].WindGustKts != 15 {
		t.Fatalf("expected wind speed/gust to carry through, got %+v", response[0])
	}
	if response[0].WindDirection != "E" || response[0].WindDirectionDeg != 90 {
		t.Fatalf("expected wind direction to carry through, got %+v", response[0])
	}
}

func TestSummarizeHourlyForecast_IncludesTypicalWindAndGust(t *testing.T) {
	entries := []weatherHourlyEntryData{
		{Label: "Now", Condition: "Mostly Sunny", WindSpeedKts: 10, WindGustKts: 18, Kind: "forecast"},
		{Label: "11AM", Condition: "Mostly Sunny", WindSpeedKts: 12, WindGustKts: 20, Kind: "forecast"},
		{Label: "12PM", Condition: "Mostly Sunny", WindSpeedKts: 11, WindGustKts: 19, Kind: "forecast"},
		{Label: "5:09PM", Condition: "Sunset", Kind: "sunset"},
	}

	summary := summarizeHourlyForecast(entries)
	expected := "Mostly Sunny conditions will continue all day. Winds around 11 kts with gusts up to 20 kts."
	if summary != expected {
		t.Fatalf("expected %q, got %q", expected, summary)
	}
}

// nov9WindFixture is the real 9 November hourly forecast: north-westerly
// overnight, calm at 4 AM, a south-easterly change at 5 AM, then easterly
// building through the evening.
func nov9WindFixture() []weatherHourlyWindData {
	rows := []struct {
		label string
		hour  int
		speed float64
		gust  float64
		dir   string
		deg   float64
	}{
		{"12 AM", 0, 4.74, 10.50, "NNW", 337}, {"1 AM", 1, 4.65, 8.75, "NNW", 334},
		{"2 AM", 2, 5.00, 9.33, "NW", 323}, {"3 AM", 3, 3.23, 9.14, "NW", 314},
		{"4 AM", 4, 0.43, 5.05, "WSW", 243}, {"5 AM", 5, 3.17, 5.44, "SSE", 157},
		{"6 AM", 6, 4.37, 9.72, "SSE", 163}, {"7 AM", 7, 5.19, 11.86, "SSE", 157},
		{"8 AM", 8, 3.52, 12.44, "SE", 141}, {"9 AM", 9, 3.05, 11.08, "E", 99},
		{"10 AM", 10, 4.84, 14.00, "ENE", 75}, {"11 AM", 11, 5.62, 15.16, "E", 80},
		{"12 PM", 12, 5.66, 16.52, "E", 94}, {"1 PM", 13, 6.03, 16.33, "E", 93},
		{"2 PM", 14, 6.82, 17.88, "E", 86}, {"3 PM", 15, 6.75, 18.08, "ESE", 102},
		{"4 PM", 16, 7.13, 17.30, "ESE", 105}, {"5 PM", 17, 8.30, 19.63, "ESE", 108},
		{"6 PM", 18, 6.40, 19.24, "ESE", 118}, {"7 PM", 19, 5.42, 12.83, "ESE", 111},
		{"8 PM", 20, 5.62, 11.08, "ESE", 108}, {"9 PM", 21, 5.99, 12.44, "E", 97},
		{"10 PM", 22, 5.77, 11.66, "E", 96}, {"11 PM", 23, 5.25, 11.47, "E", 88},
	}
	out := make([]weatherHourlyWindData, 0, len(rows))
	for _, r := range rows {
		out = append(out, weatherHourlyWindData{Label: r.label, HourOfDay: r.hour, WindSpeedKts: r.speed, WindGustKts: r.gust, WindDirection: r.dir, WindDirectionDeg: r.deg})
	}
	return out
}

func windHours(label0 int, deg float64, speeds ...float64) []weatherHourlyWindData {
	out := make([]weatherHourlyWindData, 0, len(speeds))
	for i, s := range speeds {
		out = append(out, weatherHourlyWindData{
			Label: windLabel(label0 + i), HourOfDay: label0 + i,
			WindSpeedKts: s, WindGustKts: s, WindDirection: degreesToDirection(deg), WindDirectionDeg: deg,
		})
	}
	return out
}

func TestBuildWindSummary_Nov9NamesTheSoutheasterlyChange(t *testing.T) {
	summary := buildWindSummary(nov9WindFixture())
	for _, want := range []string{"NNW to NW 3 to 5 kts", "going light around 4 AM", "turning SSE around 5 AM and settling E 3 to 8 kts", "Gusts to 20 kts around 5 PM."} {
		if !strings.Contains(summary, want) {
			t.Fatalf("expected %q in %q", want, summary)
		}
	}
	for _, bad := range []string{"0 to", "backing", "veering", "NNW-E"} {
		if strings.Contains(summary, bad) {
			t.Fatalf("did not expect %q in %q", bad, summary)
		}
	}
	if sentences := strings.Count(summary, ". "); sentences > 1 {
		t.Fatalf("expected at most two sentences, got %q", summary)
	}
}

func TestBuildWindSummary_WrapAroundIsNotAChange(t *testing.T) {
	hourly := append(windHours(0, 350, 8, 9), windHours(2, 10, 9, 8)...)
	summary := buildWindSummary(hourly)
	if strings.Contains(summary, "turning") {
		t.Fatalf("350 to 10 degrees is a 20 degree shift, got %q", summary)
	}
	if !strings.HasPrefix(summary, "Winds 8 to 9 kts from the N") || strings.Count(summary, ".") != 1 {
		t.Fatalf("expected one short steady sentence, got %q", summary)
	}
}

func TestBuildWindSummary_TurnWithoutCalmNamesTheTime(t *testing.T) {
	hourly := append(windHours(0, 270, 10, 10, 11), windHours(3, 0, 12, 13, 12)...)
	summary := buildWindSummary(hourly)
	for _, want := range []string{"W 10 to 11 kts", "turning N 12 to 13 kts around 3 AM"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("expected %q in %q", want, summary)
		}
	}
	if strings.Contains(summary, "going light") {
		t.Fatalf("no calm in this day, got %q", summary)
	}
}

func TestBuildWindSummary_SingleHourBlipIsFoldedButSharpTurnIsKept(t *testing.T) {
	blip := append(append(windHours(0, 90, 8, 8), windHours(2, 140, 8)...), windHours(3, 90, 8, 8)...)
	if summary := buildWindSummary(blip); strings.Contains(summary, "turning") {
		t.Fatalf("a 45 degree one-hour wobble should fold away, got %q", summary)
	}
	shift := append(append(windHours(0, 90, 8, 8), windHours(2, 270, 8)...), windHours(3, 90, 8, 8)...)
	summary := buildWindSummary(shift)
	if strings.Contains(summary, "turning") || !strings.Contains(summary, "variable around 2 AM") {
		t.Fatalf("a one-hour swing with the wind back after it is a variable hour, not a change, got %q", summary)
	}
	startSwing := append(windHours(0, 270, 8), windHours(1, 90, 8, 8, 8)...)
	if summary := buildWindSummary(startSwing); !strings.Contains(summary, "turning E") {
		t.Fatalf("a swing at the very start of the day may be a change, got %q", summary)
	}
}

func windLabel(hour int) string {
	return boatAxisClock(time.Date(2026, 11, 9, hour, 0, 0, 0, time.UTC), time.UTC)
}

func TestWindDirectionsThatCancelHaveNoMean(t *testing.T) {
	if _, ok := meanOfDegs(90, 270); ok {
		t.Fatal("opposed directions have no mean")
	}
	if deg, ok := meanOfDegs(350, 10); !ok || (deg > 1 && deg < 359) {
		t.Fatalf("350 and 10 average to north, got %v %v", deg, ok)
	}
	if _, _, ok := windSpanDegs([]windDir{{0, 90}, {1, 270}, {2, 90}, {3, 270}}); ok {
		t.Fatal("a span whose ends cancel has no direction")
	}
	p := &windPeriod{}
	p.add(0, 90, true)
	p.add(1, 270, true)
	if p.hasDir() {
		t.Fatal("a period whose directions cancel has no usable direction")
	}
}

// nov8WindFixture is a north-westerly day whose last evening hours wobble: a
// single south-easterly hour at 9 PM, then two north-easterly ones.
func nov8WindFixture() []weatherHourlyWindData {
	early := []float64{6, 7, 5, 6, 7, 8, 9, 8, 7, 8, 9, 8, 9, 8, 7, 6, 6, 5}
	earlyDeg := []float64{345, 350, 340, 355, 348, 342, 350, 356, 345, 352, 347, 341, 350, 346, 344, 350, 348, 352}
	var out []weatherHourlyWindData
	for h, s := range early {
		gust := s + 8
		if h == 13 {
			gust = 22
		}
		out = append(out, weatherHourlyWindData{Label: windLabel(h), HourOfDay: h, WindSpeedKts: s, WindGustKts: gust, WindDirection: degreesToDirection(earlyDeg[h]), WindDirectionDeg: earlyDeg[h]})
	}
	late := []struct{ speed, deg float64 }{{4.4, 348}, {4.1, 342}, {3.4, 348}, {4.0, 133}, {2.0, 39}, {5.4, 8}}
	for i, r := range late {
		h := 18 + i
		out = append(out, weatherHourlyWindData{Label: windLabel(h), HourOfDay: h, WindSpeedKts: r.speed, WindGustKts: r.speed + 6, WindDirection: degreesToDirection(r.deg), WindDirectionDeg: r.deg})
	}
	return out
}

// nov13WindFixture is a day that backs through south-west to south-east and
// ends easterly. Its 24 kt gust first reaches 24 at 3 PM, and a raw 24.4 at
// 5 PM must not take the time.
func nov13WindFixture() []weatherHourlyWindData {
	rows := []struct{ speed, deg float64 }{
		{5.8, 325}, {4.1, 313}, {2.7, 289}, {2.2, 247}, {2.5, 212}, {3.0, 187},
		{3.6, 167}, {4.4, 151}, {5.4, 139}, {6.3, 132}, {7.1, 126}, {7.8, 122},
		{8.4, 118}, {9.0, 114}, {9.3, 111}, {9.6, 107}, {9.5, 104}, {9.1, 99},
		{8.4, 93}, {7.6, 85}, {6.9, 76}, {6.4, 68}, {6.2, 63}, {6.0, 64},
	}
	var out []weatherHourlyWindData
	for h, r := range rows {
		gust := 17.0 + float64(h%4)
		if h == 15 {
			gust = 24.2
		}
		if h == 17 {
			gust = 24.4
		}
		out = append(out, weatherHourlyWindData{Label: windLabel(h), HourOfDay: h, WindSpeedKts: r.speed, WindGustKts: gust, WindDirection: degreesToDirection(r.deg), WindDirectionDeg: r.deg})
	}
	return out
}

func TestBuildWindSummary_Nov8OneEveningHourIsVariableNotAChange(t *testing.T) {
	summary := buildWindSummary(nov8WindFixture())
	if strings.Contains(summary, "turning") {
		t.Fatalf("a single hour of south-easterly is not a change, got %q", summary)
	}
	for _, want := range []string{"NNW to NNE 2 to 9 kts", "variable around 9 PM", "Gusts to 22 kts around 1 PM."} {
		if !strings.Contains(summary, want) {
			t.Fatalf("expected %q in %q", want, summary)
		}
	}
}

func TestBuildWindSummary_Nov13ChangeKeepsWholeGroupSpeedAndPeakTime(t *testing.T) {
	summary := buildWindSummary(nov13WindFixture())
	for _, want := range []string{"NW to WNW 3 to 6 kts", "turning WSW around 3 AM and settling S 2 to 3 kts", "turning SSE around 6 AM and settling ENE 4 to 10 kts", "Gusts to 24 kts around 3 PM."} {
		if !strings.Contains(summary, want) {
			t.Fatalf("expected %q in %q", want, summary)
		}
	}
	// SSE drifting to ENE over fifteen hours is one group, not a second turn.
	if strings.Contains(summary, "from 9 PM") {
		t.Fatalf("slow drift split into its own clause in %q", summary)
	}
	if strings.Contains(summary, "around 6 kts") || strings.Contains(summary, "E to SE") {
		t.Fatalf("doubled around or backwards span in %q", summary)
	}
}

func TestBuildWindSummary_PeriodClauseDropsAroundWhenSpeedsMatch(t *testing.T) {
	hourly := append(windHours(0, 270, 10, 10, 10), windHours(3, 0, 6, 6.2, 6)...)
	summary := buildWindSummary(hourly)
	if !strings.Contains(summary, "turning N 6 kts around 3 AM") || strings.Contains(summary, "around 6 kts") {
		t.Fatalf("got %q", summary)
	}
}

func TestBuildWindSummary_StepwiseVeerNamesTheTurn(t *testing.T) {
	var hourly []weatherHourlyWindData
	for i, deg := range []float64{20, 65, 110, 155, 200} {
		hourly = append(hourly, windHours(i*3, deg, 10, 10, 10)...)
	}
	summary := buildWindSummary(hourly)
	if !strings.Contains(summary, "turning ESE around 6 AM and settling SSW") {
		t.Fatalf("a 90 degree turn within three hours must be named with its time, got %q", summary)
	}
}

func TestBuildWindSummary_CalmInTheMiddleEndsTheGroup(t *testing.T) {
	hourly := append(windHours(0, 45, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10), windHours(12, 45, 0.5, 0.5)...)
	hourly = append(hourly, windHours(14, 45, 15, 15, 15)...)
	summary := buildWindSummary(hourly)
	want := "NE 10 kts, going light around 12 PM, then NE 15 kts again from 2 PM."
	if summary != want {
		t.Fatalf("expected %q, got %q", want, summary)
	}
}

func TestBuildWindSummary_SwingBesideACalmIsLightAndVariable(t *testing.T) {
	hourly := append(windHours(0, 340, 8, 8, 8), windHours(3, 340, 0.5)...)
	hourly = append(hourly, windHours(4, 135, 4)...)
	hourly = append(hourly, windHours(5, 340, 0.5)...)
	hourly = append(hourly, windHours(6, 340, 8, 8, 8)...)
	summary := buildWindSummary(hourly)
	if !strings.Contains(summary, "light and variable around 3 AM") || strings.Contains(summary, "turning") || strings.Contains(summary, "going light") {
		t.Fatalf("got %q", summary)
	}
	// A swing on the very first or last hour is still a change.
	last := append(windHours(0, 340, 8, 8, 8), windHours(3, 340, 0.5)...)
	last = append(last, windHours(4, 135, 5)...)
	if summary := buildWindSummary(last); !strings.Contains(summary, "turning SE") {
		t.Fatalf("a swing on the last hour may be a change, got %q", summary)
	}
}

func TestBuildWindSummary_StrongSwingBesideACalmIsNotLight(t *testing.T) {
	hourly := append(windHours(0, 340, 10, 10, 10, 10), windHours(4, 340, 1)...)
	hourly = append(hourly, windHours(5, 135, 25)...)
	hourly = append(hourly, windHours(6, 340, 1)...)
	hourly = append(hourly, windHours(7, 340, 10)...)
	summary := buildWindSummary(hourly)
	want := "NNW 10 to 25 kts, variable around 5 AM, then NNW 10 kts again from 7 AM."
	if summary != want {
		t.Fatalf("expected %q, got %q", want, summary)
	}
}

func TestBuildWindSummary_BackToBackOddHoursAreOneVariableStretch(t *testing.T) {
	hourly := append(windHours(0, 90, 8, 8, 8, 8), windHours(4, 270, 8)...)
	hourly = append(hourly, windHours(5, 0, 8)...)
	hourly = append(hourly, windHours(6, 90, 8, 8, 8, 8)...)
	summary := buildWindSummary(hourly)
	want := "E 8 kts, briefly variable around 4 AM."
	if summary != want {
		t.Fatalf("expected %q, got %q", want, summary)
	}
	// Two coherent hours are a period, not a stretch of odd hours.
	two := append(windHours(0, 90, 8, 8, 8, 8), windHours(4, 270, 8, 8)...)
	two = append(two, windHours(6, 90, 8, 8, 8, 8)...)
	if got := buildWindSummary(two); !strings.Contains(got, "turning W") {
		t.Fatalf("a two-hour turn is a period, got %q", got)
	}
}

func TestBuildWindSummary_VariableHourMidGroupIsBrieflyVariable(t *testing.T) {
	summary := buildWindSummary(nov8WindFixture())
	if !strings.Contains(summary, "NNW to NNE 2 to 9 kts, briefly variable around 9 PM.") {
		t.Fatalf("got %q", summary)
	}
}

func TestBuildWindSummary_AgainOnlyWhenTheDirectionIsBack(t *testing.T) {
	hourly := append(windHours(0, 45, 10, 10, 10), windHours(3, 45, 0.5)...)
	hourly = append(hourly, windHours(4, 100, 15, 15, 15)...)
	summary := buildWindSummary(hourly)
	want := "NE 10 kts, going light around 3 AM, then E 15 kts from 4 AM."
	if summary != want {
		t.Fatalf("expected %q, got %q", want, summary)
	}
}

// threeHourly builds entries three hours apart, as a coarse provider sends.
func threeHourly(startHour int, deg float64, speeds ...float64) []weatherHourlyWindData {
	out := make([]weatherHourlyWindData, 0, len(speeds))
	for i, s := range speeds {
		h := startHour + 3*i
		out = append(out, weatherHourlyWindData{Label: windLabel(h), HourOfDay: h, WindSpeedKts: s, WindGustKts: s, WindDirection: degreesToDirection(deg), WindDirectionDeg: deg})
	}
	return out
}

func TestBuildWindSummary_CoarseEntriesCountHoursNotSlots(t *testing.T) {
	// A calm entry at 6 AM, then wind from the south-east at 12 PM: nine hours
	// after the last north-westerly entry, so not a change.
	hourly := append(threeHourly(0, 315, 8, 8), threeHourly(6, 315, 0.5)...)
	hourly = append(hourly, threeHourly(12, 135, 8, 8)...)
	summary := buildWindSummary(hourly)
	if strings.Contains(summary, "turning") || !strings.Contains(summary, "then SE 8 kts from 12 PM") {
		t.Fatalf("a nine hour gap is a later turn, got %q", summary)
	}
	// One three-hour entry is not a one-hour swing.
	hourly = append(threeHourly(0, 90, 8, 8, 8), threeHourly(9, 270, 8)...)
	hourly = append(hourly, threeHourly(12, 90, 8, 8, 8)...)
	summary = buildWindSummary(hourly)
	if strings.Contains(summary, "variable") || !strings.Contains(summary, "turning W 8 kts around 9 AM") {
		t.Fatalf("a single 3-hour entry must not read as variable, got %q", summary)
	}
}

func TestBuildWindSummary_ShortSpanUsesFirstAndLastHour(t *testing.T) {
	hourly := []weatherHourlyWindData{
		{Label: "1 AM", WindSpeedKts: 6, WindGustKts: 6, WindDirection: "SE", WindDirectionDeg: 135},
		{Label: "2 AM", WindSpeedKts: 6, WindGustKts: 6, WindDirection: "E", WindDirectionDeg: 90},
	}
	if got := buildWindSummary(hourly); got != "Winds around 6 kts from the SE to E." {
		t.Fatalf("got %q", got)
	}
}

func TestBuildWindSummary_SpanNamesStartThenEnd(t *testing.T) {
	hourly := append(windHours(0, 135, 6, 6, 6), windHours(3, 90, 6, 6, 6)...)
	summary := buildWindSummary(hourly)
	if !strings.Contains(summary, "from the SE to E") || strings.Contains(summary, "E to SE") {
		t.Fatalf("a wind that went SE then E must read SE to E, got %q", summary)
	}
}

func TestBuildWindSummary_AllCalmDay(t *testing.T) {
	summary := buildWindSummary(windHours(0, 180, 0.4, 1.2, 0.8, 1.9))
	if summary != "Light winds all day." {
		t.Fatalf("got %q", summary)
	}
}

func TestBuildWindSummary_GustTimeUsesFirstOfTies(t *testing.T) {
	hourly := windHours(0, 180, 5, 5, 5)
	hourly[1].WindGustKts = 15
	hourly[2].WindGustKts = 15
	hourly = append(hourly, windHours(3, 0, 5)...)
	hourly[3].WindGustKts = 6
	summary := buildWindSummary(hourly)
	if !strings.Contains(summary, "Gusts to 15 kts around 1 AM.") {
		t.Fatalf("expected the first tied hour, got %q", summary)
	}
}

func TestBuildWindSummary_FormatsRangeDirectionAndGust(t *testing.T) {
	hourly := []weatherHourlyWindData{
		{Label: "1 PM", WindSpeedKts: 19.0, WindGustKts: 24.0, WindDirection: "S", WindDirectionDeg: 180},
		{Label: "2 PM", WindSpeedKts: 22.0, WindGustKts: 29.4, WindDirection: "SSE", WindDirectionDeg: 157},
		{Label: "3 PM", WindSpeedKts: 20.0, WindGustKts: 25.0, WindDirection: "SE", WindDirectionDeg: 135},
	}

	summary := buildWindSummary(hourly)
	expected := "Winds 19 to 22 kts from the S to SE, gusting to 29 kts around 2 PM."
	if summary != expected {
		t.Fatalf("expected %q, got %q", expected, summary)
	}
}

func TestBuildWindSummary_OmitsGustWhenNotAboveSustained(t *testing.T) {
	hourly := []weatherHourlyWindData{
		{WindSpeedKts: 12.0, WindGustKts: 12.0, WindDirection: "NE", WindDirectionDeg: 45},
		{WindSpeedKts: 12.0, WindGustKts: 12.0, WindDirection: "NE", WindDirectionDeg: 45},
	}

	summary := buildWindSummary(hourly)
	expected := "Winds around 12 kts from the NE."
	if summary != expected {
		t.Fatalf("expected %q, got %q", expected, summary)
	}
}

func TestBuildWindSummary_EmptyWhenNoHourlyData(t *testing.T) {
	if summary := buildWindSummary(nil); summary != "" {
		t.Fatalf("expected empty summary, got %q", summary)
	}

	hourly := []weatherHourlyWindData{{WindSpeedKts: -1, WindGustKts: -1}}
	if summary := buildWindSummary(hourly); summary != "" {
		t.Fatalf("expected empty summary for all-missing data, got %q", summary)
	}
}

func TestBuildWindSummary_MissingDirectionOmitsTheDirectionPhrase(t *testing.T) {
	hourly := []weatherHourlyWindData{
		{WindSpeedKts: 6, WindGustKts: 6, WindDirection: "—", WindDirectionDeg: -1},
		{WindSpeedKts: 8, WindGustKts: 8, WindDirection: "—", WindDirectionDeg: -1},
	}
	if summary := buildWindSummary(hourly); summary != "Winds 6 to 8 kts." {
		t.Fatalf("got %q", summary)
	}
}

func TestBuildPrecipitationSummary_LittleToNoRain(t *testing.T) {
	hourly := []weatherHourlyPrecipitationData{
		{Label: "12AM", PrecipitationChancePct: 5, PrecipitationIntensityMm: 0},
		{Label: "1AM", PrecipitationChancePct: 10, PrecipitationIntensityMm: 0},
		{Label: "2AM", PrecipitationChancePct: 20, PrecipitationIntensityMm: 0},
	}

	summary := buildPrecipitationSummary(hourly)
	expected := "Little to no rain is expected."
	if summary != expected {
		t.Fatalf("expected %q, got %q", expected, summary)
	}
}

func TestBuildPrecipitationSummary_SlightChanceAfterHour(t *testing.T) {
	hourly := []weatherHourlyPrecipitationData{
		{Label: "12AM", PrecipitationChancePct: 10, PrecipitationIntensityMm: 0},
		{Label: "1AM", PrecipitationChancePct: 15, PrecipitationIntensityMm: 0},
		{Label: "5PM", PrecipitationChancePct: 45, PrecipitationIntensityMm: 1.0},
	}

	summary := buildPrecipitationSummary(hourly)
	expected := "Slight chance of rain after 5PM."
	if summary != expected {
		t.Fatalf("expected %q, got %q", expected, summary)
	}
}

func TestBuildPrecipitationSummary_RainThroughoutTheDay(t *testing.T) {
	lightHourly := []weatherHourlyPrecipitationData{
		{Label: "12AM", PrecipitationChancePct: 70, PrecipitationIntensityMm: 1.0},
		{Label: "1AM", PrecipitationChancePct: 65, PrecipitationIntensityMm: 0.5},
	}

	lightSummary := buildPrecipitationSummary(lightHourly)
	expectedLight := "Showers expected throughout the day."
	if lightSummary != expectedLight {
		t.Fatalf("expected %q, got %q", expectedLight, lightSummary)
	}

	moderateHourly := []weatherHourlyPrecipitationData{
		{Label: "12AM", PrecipitationChancePct: 80, PrecipitationIntensityMm: 4.0},
	}

	moderateSummary := buildPrecipitationSummary(moderateHourly)
	expectedModerate := "Rain expected throughout the day."
	if moderateSummary != expectedModerate {
		t.Fatalf("expected %q, got %q", expectedModerate, moderateSummary)
	}
}

func TestBuildPrecipitationSummary_HeavyRainAfterHour(t *testing.T) {
	hourly := []weatherHourlyPrecipitationData{
		{Label: "12AM", PrecipitationChancePct: 10, PrecipitationIntensityMm: 0},
		{Label: "12PM", PrecipitationChancePct: 20, PrecipitationIntensityMm: 0.5},
		{Label: "1PM", PrecipitationChancePct: 80, PrecipitationIntensityMm: 8.0},
	}

	summary := buildPrecipitationSummary(hourly)
	expected := "Heavy rain expected after 1PM."
	if summary != expected {
		t.Fatalf("expected %q, got %q", expected, summary)
	}
}

func TestBuildPrecipitationSummary_EmptyWhenNoHourlyData(t *testing.T) {
	if summary := buildPrecipitationSummary(nil); summary != "" {
		t.Fatalf("expected empty summary, got %q", summary)
	}
}
