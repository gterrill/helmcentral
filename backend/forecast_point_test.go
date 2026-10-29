package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func forecastPointCtx(query string) echo.Context {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/x"+query, nil)
	return e.NewContext(req, httptest.NewRecorder())
}

func TestExplicitForecastPoint(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		present bool
		lat     float64
		lon     float64
		wantErr bool
	}{
		{"absent", "", false, 0, 0, false},
		{"valid", "?lat=-27.4&lon=153.1", true, -27.4, 153.1, false},
		{"lat only", "?lat=-27.4", false, 0, 0, true},
		{"lon only", "?lon=153.1", false, 0, 0, true},
		{"lat out of range", "?lat=91&lon=10", false, 0, 0, true},
		{"lon out of range", "?lat=10&lon=181", false, 0, 0, true},
		{"not a number", "?lat=abc&lon=10", false, 0, 0, true},
		{"NaN", "?lat=NaN&lon=10", false, 0, 0, true},
		{"empty values count as absent", "?lat=&lon=", false, 0, 0, false},
		{"equator and meridian is allowed", "?lat=0&lon=0", true, 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lat, lon, present, err := explicitForecastPoint(forecastPointCtx(tc.query))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got lat=%v lon=%v", lat, lon)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if present != tc.present || lat != tc.lat || lon != tc.lon {
				t.Fatalf("got (%v,%v,%v), want (%v,%v,%v)", lat, lon, present, tc.lat, tc.lon, tc.present)
			}
		})
	}
}

// --- handlers honour an explicit point and never ask SignalK ---

type pointWeather struct {
	stubWeatherProvider
	lat, lon float64
}

func (p *pointWeather) FetchForecast(lat, lon float64, days int, tz string) (weatherForecastBundle, error) {
	p.lat, p.lon = lat, lon
	return p.stubWeatherProvider.FetchForecast(lat, lon, days, tz)
}

func serveForecastPoint(t *testing.T, h echo.HandlerFunc, query string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/x"+query, nil)
	rec := httptest.NewRecorder()
	if err := h(e.NewContext(req, rec)); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return rec
}

func TestWeatherForecast_ExplicitPointSkipsVesselAndUsesPlaceTimezone(t *testing.T) {
	withCleanWeatherProviderRegistry(t)
	now := time.Now().UTC()
	p := &pointWeather{stubWeatherProvider: stubWeatherProvider{id: "w", name: "W", ttl: 60, bundle: weatherForecastBundle{
		Days:     []weatherDayPoint{{Start: now, Condition: "clear", TempMaxC: 20, TempMinC: 10}},
		CachedAt: now,
	}}}
	registerWeatherProvider(p)
	t.Setenv("SETTINGS_FILE", writeWeatherSettings(t, "w", "http://127.0.0.1:1"))

	rec := serveForecastPoint(t, weatherForecast, "?lat=-27.4&lon=150")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if p.lat != -27.4 || p.lon != 150 {
		t.Fatalf("provider asked for (%v,%v)", p.lat, p.lon)
	}
	if p.gotTimezone != "Australia/Brisbane" {
		t.Fatalf("expected the place's zone Australia/Brisbane, got %q", p.gotTimezone)
	}
}

func TestForecastEndpoints_BadPointIs400(t *testing.T) {
	withCleanWeatherProviderRegistry(t)
	withCleanWaveProviderRegistry(t)
	withCleanUpperAirProviderRegistry(t)
	withCleanForecastWarningsProviderRegistry(t)
	registerWeatherProvider(&stubWeatherProvider{id: "w", name: "W"})
	registerWaveProvider(&stubWaveProvider{id: "w", name: "W"})
	registerUpperAirProvider(&stubUpperAirProvider{id: "w"})
	registerForecastWarningsProvider(&stubForecastWarningsProvider{id: "w", name: "W"})
	origRegistry, origOrder := tideProviderRegistry, tideProviderOrder
	tideProviderRegistry = map[string]tideProvider{}
	tideProviderOrder = nil
	t.Cleanup(func() { tideProviderRegistry, tideProviderOrder = origRegistry, origOrder })
	registerTideProvider(&stubTideProvider{id: "w"})
	settings := filepath.Join(t.TempDir(), "settings.yaml")
	yaml := "ui:\n  weather_provider: w\n  wave_provider: w\n  upper_air_provider: w\n  forecast_warnings_provider: w\n"
	if err := os.WriteFile(settings, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SETTINGS_FILE", settings)

	handlers := map[string]echo.HandlerFunc{"today": weatherToday, "forecast": weatherForecast, "wave": waveForecast, "upper-air": upperAirForecast, "warnings": forecastWarningsHandler, "tide": tideNearestHandler}
	for name, h := range handlers {
		for _, q := range []string{"?provider=w&lat=10", "?provider=w&lon=10", "?provider=w&lat=95&lon=10", "?provider=w&lat=x&lon=10"} {
			if rec := serveForecastPoint(t, h, q); rec.Code != http.StatusBadRequest {
				t.Errorf("%s %s: expected 400, got %d: %s", name, q, rec.Code, rec.Body.String())
			}
		}
	}
}

func TestWeatherToday_ExplicitPoint(t *testing.T) {
	withCleanWeatherProviderRegistry(t)
	now := time.Now().UTC()
	p := &pointWeather{stubWeatherProvider: stubWeatherProvider{id: "w", name: "W", ttl: 60, bundle: weatherForecastBundle{
		Current:  weatherCurrentPoint{Time: now, TemperatureC: 20, Condition: "clear"},
		CachedAt: now,
	}}}
	registerWeatherProvider(p)
	t.Setenv("SETTINGS_FILE", writeWeatherSettings(t, "w", "http://127.0.0.1:1"))
	rec := serveForecastPoint(t, weatherToday, "?lat=51.5&lon=-1.2")
	if rec.Code != http.StatusOK || p.lat != 51.5 || p.lon != -1.2 {
		t.Fatalf("code %d, provider point (%v,%v): %s", rec.Code, p.lat, p.lon, rec.Body.String())
	}
}

type pointWave struct {
	stubWaveProvider
	lat, lon float64
}

func (p *pointWave) FetchWaves(lat, lon float64, days int) (waveForecastBundle, error) {
	p.lat, p.lon = lat, lon
	return p.stubWaveProvider.FetchWaves(lat, lon, days)
}

// The wave plugin reports a cell the model has no values for (a marina up a
// river, a lake) as a full hourly series with every wave value zeroed - null
// becomes 0 in its output. That is the one case an explicit place answers 200
// with no days.
func TestWaveForecast_ExplicitPointWithNullSeriesIsAnEmptyAnswer(t *testing.T) {
	withCleanWaveProviderRegistry(t)
	now := time.Now().UTC()
	hourly := []waveHourPoint{{Time: now}, {Time: now.Add(time.Hour)}, {Time: now.Add(24 * time.Hour)}}
	p := &pointWave{stubWaveProvider: stubWaveProvider{id: "w", name: "W", ttl: 60, bundle: waveForecastBundle{Hourly: hourly, CachedAt: now}}}
	registerWaveProvider(p)
	t.Setenv("SETTINGS_FILE", writeWaveSettings(t, "w", "http://127.0.0.1:1"))

	rec := serveForecastPoint(t, waveForecast, "?lat=-27.4&lon=153")
	if rec.Code != http.StatusOK {
		t.Fatalf("a place with no wave values is not a fault, got %d: %s", rec.Code, rec.Body.String())
	}
	var body waveForecastResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Days == nil || len(body.Days) != 0 {
		t.Fatalf("expected an empty days array, got %#v", body.Days)
	}
	if p.lat != -27.4 || p.lon != 153 {
		t.Fatalf("provider asked for (%v,%v)", p.lat, p.lon)
	}
}

func TestWaveForecast_ExplicitPointEmptyResponseIsAFault(t *testing.T) {
	withCleanWaveProviderRegistry(t)
	registerWaveProvider(&pointWave{stubWaveProvider: stubWaveProvider{id: "w", name: "W", ttl: 60, bundle: waveForecastBundle{CachedAt: time.Now().UTC()}}})
	t.Setenv("SETTINGS_FILE", writeWaveSettings(t, "w", "http://127.0.0.1:1"))
	rec := serveForecastPoint(t, waveForecast, "?lat=-27.4&lon=153")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("an empty provider response is a fault for a place too, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWaveForecast_ExplicitPointWithOnlyPastDaysIsAFault(t *testing.T) {
	withCleanWaveProviderRegistry(t)
	old := time.Now().UTC().AddDate(0, 0, -5)
	hourly := []waveHourPoint{{Time: old, WaveHeightM: pwF(1.2), WavePeriodS: pwF(8)}}
	registerWaveProvider(&pointWave{stubWaveProvider: stubWaveProvider{id: "w", name: "W", ttl: 60, bundle: waveForecastBundle{Hourly: hourly, CachedAt: time.Now().UTC()}}})
	t.Setenv("SETTINGS_FILE", writeWaveSettings(t, "w", "http://127.0.0.1:1"))
	rec := serveForecastPoint(t, waveForecast, "?lat=-27.4&lon=153")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("no days on or after today is a fault, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWaveForecast_OwnShipEmptyStaysAFault(t *testing.T) {
	withCleanWaveProviderRegistry(t)
	registerWaveProvider(&stubWaveProvider{id: "w", name: "W", ttl: 60, bundle: waveForecastBundle{CachedAt: time.Now().UTC()}})
	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()
	t.Setenv("SETTINGS_FILE", writeWaveSettings(t, "w", server.URL))
	rec := serveForecastPoint(t, waveForecast, "")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("own-ship empty wave response must stay 502, got %d", rec.Code)
	}
}

func TestUpperAirForecast_ExplicitPoint(t *testing.T) {
	withCleanUpperAirProviderRegistry(t)
	registerUpperAirProvider(&stubUpperAirProvider{id: "u", bundle: upperAirBundle{CachedAt: time.Now().UTC()}})
	t.Setenv("SETTINGS_FILE", writeUpperAirSettings(t, "u", "http://127.0.0.1:1"))
	rec := serveForecastPoint(t, upperAirForecast, "?lat=-27.4&lon=153")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

type pointWarnings struct {
	stubForecastWarningsProvider
	lat, lon float64
}

func (p *pointWarnings) FetchWarnings(lat, lon float64) (forecastWarningsBundle, error) {
	p.lat, p.lon = lat, lon
	return p.stubForecastWarningsProvider.FetchWarnings(lat, lon)
}

func TestForecastWarnings_ExplicitPoint(t *testing.T) {
	withCleanForecastWarningsProviderRegistry(t)
	p := &pointWarnings{stubForecastWarningsProvider: stubForecastWarningsProvider{id: "bom", name: "BOM", ttl: 60, bundle: forecastWarningsBundle{Region: "Gold Coast", CachedAt: time.Now().UTC()}}}
	registerForecastWarningsProvider(p)
	t.Setenv("SETTINGS_FILE", writeForecastWarningsSettings(t, "bom", "http://127.0.0.1:1"))
	rec := serveForecastPoint(t, forecastWarningsHandler, "?lat=-27.9&lon=153.4")
	if rec.Code != http.StatusOK || p.lat != -27.9 || p.lon != 153.4 {
		t.Fatalf("code %d, provider point (%v,%v): %s", rec.Code, p.lat, p.lon, rec.Body.String())
	}
}

func TestTideNearest_ExplicitPoint(t *testing.T) {
	origRegistry, origOrder := tideProviderRegistry, tideProviderOrder
	tideProviderRegistry = map[string]tideProvider{}
	tideProviderOrder = nil
	t.Cleanup(func() { tideProviderRegistry, tideProviderOrder = origRegistry, origOrder })
	registerTideProvider(&stubTideProvider{id: "fake", stations: []tideStation{
		{StationID: "SYD", Name: "Sydney", Lat: -33.8688, Lon: 151.2093},
		{StationID: "BRI", Name: "Brisbane Bar", Lat: -27.37, Lon: 153.17},
	}})
	rec := serveForecastPoint(t, tideNearestHandler, "?provider=fake&lat=-27.5&lon=153.0")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var st tideStation
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.StationID != "BRI" {
		t.Fatalf("expected the station nearest the place, got %q", st.StationID)
	}
}
