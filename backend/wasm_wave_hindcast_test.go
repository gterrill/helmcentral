package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

var hindcastTestPoints = []currentsPoint{{Lat: -24.75, Lon: 152.5}, {Lat: -20.25, Lon: 148.75}}

// A wave plugin built before the hindcast existed has no fetch_wave_hindcast.
// It still serves waves; asking it for a hindcast says what to install, using the
// plugin's own id, and never answers from the forecast.
func TestWasmWaveProvider_PluginWithoutHindcastSaysSo(t *testing.T) {
	p := mustNewWasmWaveProvider(t, waveValidFixtureWasm)
	if p.SupportsWaveHindcast() {
		t.Fatal("the fixture exports no fetch_wave_hindcast")
	}
	var hp waveHindcastProvider = p
	start := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	_, err := hp.FetchWaveHindcast(context.Background(), hindcastTestPoints, start, start.Add(5*time.Hour))
	if !errors.Is(err, errNoWaveHindcastExport) || !strings.Contains(err.Error(), "install the updated wave-valid-fixture plugin") {
		t.Fatalf("want the install hint naming the plugin, got %v", err)
	}
}

// The plugin says a 429 in words ("rate limited"); the host turns that into one
// error the sweep can recognise, and leaves every other failure as it was.
func TestRateLimitedPluginErrorIsRecognised(t *testing.T) {
	limited := markRateLimited(errors.New("plugin call failed: open-meteo marine hindcast API rate limited the request (429)"))
	if !errors.Is(limited, errWaveHindcastRateLimited) || !strings.Contains(limited.Error(), "429") {
		t.Errorf("a rate-limited error: %v", limited)
	}
	other := errors.New("open-meteo marine hindcast API returned 503")
	if got := markRateLimited(other); errors.Is(got, errWaveHindcastRateLimited) || got != other {
		t.Errorf("a 503 is not a rate limit: %v", got)
	}
	if markRateLimited(nil) != nil {
		t.Error("no error stays no error")
	}
}

func TestWasmWaveProvider_FetchWaveHindcastReturnsTheContextError(t *testing.T) {
	p := mustNewWasmWaveProvider(t, waveValidFixtureWasm)
	p.supportsWaveHindcast = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	if _, err := p.FetchWaveHindcast(ctx, hindcastTestPoints, start, start); err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestWasmWaveProvider_FetchWaveHindcastRefusesNoPointsAndTooManyPoints(t *testing.T) {
	p := mustNewWasmWaveProvider(t, waveValidFixtureWasm)
	p.supportsWaveHindcast = true
	start := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	if _, err := p.FetchWaveHindcast(context.Background(), nil, start, start); err == nil || !strings.Contains(err.Error(), "at least one point") {
		t.Errorf("no points: %v", err)
	}
	over := make([]currentsPoint, maxHindcastPoints+1)
	if _, err := p.FetchWaveHindcast(context.Background(), over, start, start); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Errorf("too many points: %v", err)
	}
}

func decodeHindcast(t *testing.T, body string) wasmFetchWaveHindcastOutput {
	t.Helper()
	var parsed wasmFetchWaveHindcastOutput
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatal(err)
	}
	return parsed
}

const hindcastTwoPoints = `{"points":[
	{"model":"ncep_gfswave025","model_label":"GFS-Wave","hourly":[
		{"time":"2026-11-10T00:00:00Z","wave_height_m":0.62,"wave_period_s":4.9,"wave_direction_deg":61,"wind_wave_height_m":0,"wind_wave_direction_deg":0,"wind_wave_period_s":0,"swell_wave_height_m":0.58,"swell_wave_direction_deg":56,"swell_wave_period_s":4.9},
		{"time":"2026-11-10T01:00:00Z","wave_height_m":0.6,"wave_period_s":4.9,"wave_direction_deg":61,"wind_wave_height_m":0.54,"wind_wave_direction_deg":54,"wind_wave_period_s":4.9,"swell_wave_height_m":0.16,"swell_wave_direction_deg":68,"swell_wave_period_s":10.9}]},
	{"model":"ncep_gfswave025","model_label":"GFS-Wave","hourly":[
		{"time":"2026-11-10T00:00:00Z","missing_reason":"GFS-Wave has no value at this cell."}]}]}`

func TestMapWasmWaveHindcastOutput_KeepsEachPointsModelHoursAndReason(t *testing.T) {
	got, err := mapWasmWaveHindcastOutput("wave-fixture", decodeHindcast(t, hindcastTwoPoints), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Points) != 2 {
		t.Fatalf("points %d", len(got.Points))
	}
	open, land := got.Points[0], got.Points[1]
	if open.Model != "ncep_gfswave025" || open.ModelLabel != "GFS-Wave" || len(open.Hourly) != 2 {
		t.Fatalf("open water: %+v", open)
	}
	// A component with no period is no train, as in the forecast.
	if open.Hourly[0].WindWaveHeightM != 0 || open.Hourly[0].WindWavePeriodS != 0 || open.Hourly[0].SwellWavePeriodS != 4.9 || open.Hourly[0].MissingReason != "" {
		t.Errorf("hour 0: %+v", open.Hourly[0])
	}
	if land.Hourly[0].MissingReason != "GFS-Wave has no value at this cell." || land.Hourly[0].WavePeriodS != nil {
		t.Errorf("the plugin's reason is kept as given: %+v", land.Hourly[0])
	}
}

// An answer that does not say which model gave it cannot be stored: the fit
// compares the model of the hindcast with the model of the forecast.
func TestMapWasmWaveHindcastOutput_APointWithoutAModelOrLabelIsAnError(t *testing.T) {
	for name, body := range map[string]string{
		"model":       `{"points":[{"model_label":"GFS-Wave","hourly":[{"time":"2026-11-10T00:00:00Z"}]}]}`,
		"model_label": `{"points":[{"model":"ncep_gfswave025","hourly":[{"time":"2026-11-10T00:00:00Z"}]}]}`,
	} {
		if _, err := mapWasmWaveHindcastOutput("wave-fixture", decodeHindcast(t, body), 1); err == nil || !strings.Contains(err.Error(), "model") {
			t.Errorf("%s missing: want an error naming the model, got %v", name, err)
		}
	}
}

// The plugin's definite refusal travels as the answer, with no points.
func TestMapWasmWaveHindcastOutput_ARefusalIsKeptAsTheAnswer(t *testing.T) {
	body := `{"refused":"Open-Meteo wouldn't give the sea for this hour: out of range."}`
	got, err := mapWasmWaveHindcastOutput("wave-fixture", decodeHindcast(t, body), 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.Refused != "Open-Meteo wouldn't give the sea for this hour: out of range." || len(got.Points) != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestMapWasmWaveHindcastOutput_AWrongNumberOfPointsIsAnError(t *testing.T) {
	if _, err := mapWasmWaveHindcastOutput("wave-fixture", decodeHindcast(t, hindcastTwoPoints), 3); err == nil || !strings.Contains(err.Error(), "answered 2 positions, asked for 3") {
		t.Fatalf("got %v", err)
	}
}
