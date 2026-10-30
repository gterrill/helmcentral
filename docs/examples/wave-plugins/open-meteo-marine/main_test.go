package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseOpenMeteoWaveResponse_FlatChronologicalHourly(t *testing.T) {
	// Fixture adapted from original TestParseOpenMeteoMarineResponse_BucketsHoursByLocalDate,
	// but now expecting a flat list (not bucketed by day) with RFC3339 times.
	// The fixture has utc_offset_seconds=36000 (UTC+10:00, Australian Eastern Standard Time).
	waveRaw := []byte(`{
		"latitude": -34.0,
		"longitude": 151.25,
		"utc_offset_seconds": 36000,
		"timezone": "Australia/Sydney",
		"hourly": {
			"time": ["2026-06-14T23:00", "2026-06-15T00:00"],
			"wave_height": [1.2, 1.5],
			"wave_period": [6.5, 7.0],
			"wave_direction": [135.0, 140.0],
			"wind_wave_height": [0.4, 0.5],
			"swell_wave_height": [0.9, 1.1]
		}
	}`)

	hourly, err := parseOpenMeteoWaveResponse(waveRaw, 36000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(hourly) != 2 {
		t.Fatalf("expected 2 hourly entries, got %d", len(hourly))
	}

	// First entry: 2026-06-14T23:00 local (UTC+10) = 2026-06-14T13:00 UTC
	first := hourly[0]
	if first.Time != "2026-06-14T13:00:00Z" {
		t.Errorf("expected first time 2026-06-14T13:00:00Z, got %s", first.Time)
	}
	if first.WaveHeightM == nil || *first.WaveHeightM != 1.2 {
		t.Errorf("expected wave height 1.2, got %v", first.WaveHeightM)
	}
	if first.WavePeriodS == nil || *first.WavePeriodS != 6.5 {
		t.Errorf("expected wave period 6.5, got %v", first.WavePeriodS)
	}
	if first.WaveDirectionDeg == nil || *first.WaveDirectionDeg != 135.0 {
		t.Errorf("expected wave direction 135, got %v", first.WaveDirectionDeg)
	}
	if first.WindWaveHeightM != 0.4 {
		t.Errorf("expected wind wave height 0.4, got %f", first.WindWaveHeightM)
	}
	if first.SwellWaveHeightM != 0.9 {
		t.Errorf("expected swell wave height 0.9, got %f", first.SwellWaveHeightM)
	}

	// Second entry: 2026-06-15T00:00 local (UTC+10) = 2026-06-14T14:00 UTC
	second := hourly[1]
	if second.Time != "2026-06-14T14:00:00Z" {
		t.Errorf("expected second time 2026-06-14T14:00:00Z, got %s", second.Time)
	}
	if second.WaveHeightM == nil || *second.WaveHeightM != 1.5 {
		t.Errorf("expected wave height 1.5, got %v", second.WaveHeightM)
	}

	// Verify times are in chronological order (they should be)
	t1, _ := time.Parse(time.RFC3339, first.Time)
	t2, _ := time.Parse(time.RFC3339, second.Time)
	if t2.Before(t1) {
		t.Errorf("hourly entries not sorted by time")
	}
}

func TestParseOpenMeteoWaveResponse_RequiresHourlyField(t *testing.T) {
	raw := []byte(`{"utc_offset_seconds": 0}`)
	if _, err := parseOpenMeteoWaveResponse(raw, 0); err == nil {
		t.Fatalf("expected error for missing hourly field")
	}
}

func TestParseOpenMeteoWaveResponse_RequiresTimeArray(t *testing.T) {
	raw := []byte(`{"hourly": {"wave_height": [1.0]}}`)
	if _, err := parseOpenMeteoWaveResponse(raw, 0); err == nil {
		t.Fatalf("expected error for missing hourly.time")
	}
}

func TestParseOpenMeteoSeaTemperatureResponse_ReturnsCurrentValue(t *testing.T) {
	// Fixture adapted from original TestParseOpenMeteoSeaTemperatureResponse_ReturnsCurrentValue.
	raw := []byte(`{
		"current": {
			"time": "2026-06-19T04:45",
			"sea_surface_temperature": 19.3
		}
	}`)

	temp, err := parseOpenMeteoSeaTemperatureResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if temp == nil {
		t.Fatalf("expected non-nil temp")
	}
	if *temp != 19.3 {
		t.Fatalf("expected temperature 19.3, got %f", *temp)
	}
}

func TestParseOpenMeteoSeaTemperatureResponse_MissingCurrentField(t *testing.T) {
	// When current field is missing entirely, treat as "no data" (nil, not error).
	raw := []byte(`{}`)
	temp, err := parseOpenMeteoSeaTemperatureResponse(raw)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if temp != nil {
		t.Fatalf("expected nil temp for missing current field, got %v", temp)
	}
}

func TestParseOpenMeteoSeaTemperatureResponse_NullSeaSurfaceTemperature(t *testing.T) {
	// Fixture from original test: sea_surface_temperature can be null when
	// the underlying model has no data for a location (e.g. inland).
	raw := []byte(`{
		"current": {
			"time": "2026-07-19T08:30",
			"sea_surface_temperature": null
		}
	}`)

	temp, err := parseOpenMeteoSeaTemperatureResponse(raw)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if temp != nil {
		t.Fatalf("expected nil temp for null sea_surface_temperature, got %v", temp)
	}
}

func TestParseOpenMeteoSeaTemperatureResponse_MissingSeaSurfaceTemperatureKey(t *testing.T) {
	// current exists but sea_surface_temperature key is absent - treat as "no data".
	raw := []byte(`{
		"current": {
			"time": "2026-07-19T08:30"
		}
	}`)

	temp, err := parseOpenMeteoSeaTemperatureResponse(raw)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if temp != nil {
		t.Fatalf("expected nil temp for missing sea_surface_temperature key, got %v", temp)
	}
}

func TestParseOpenMeteoLocalTime_PositiveOffset(t *testing.T) {
	// UTC+10:00 (e.g. Australian Eastern Standard Time)
	rfc, err := parseOpenMeteoLocalTime("2026-06-14T23:00", 36000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 2026-06-14T23:00 local + UTC+10 = 2026-06-14T13:00 UTC
	expected := "2026-06-14T13:00:00Z"
	if rfc.UTC().Format(time.RFC3339) != expected {
		t.Errorf("expected %s, got %s", expected, rfc.UTC().Format(time.RFC3339))
	}
}

func TestParseOpenMeteoLocalTime_NegativeOffset(t *testing.T) {
	// UTC-5:00 (e.g. US Eastern Standard Time)
	rfc, err := parseOpenMeteoLocalTime("2026-06-14T13:00", -18000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 2026-06-14T13:00 local - UTC-5 (i.e., UTC+5 when subtracting the negative offset) = 2026-06-14T18:00 UTC
	expected := "2026-06-14T18:00:00Z"
	if rfc.UTC().Format(time.RFC3339) != expected {
		t.Errorf("expected %s, got %s", expected, rfc.UTC().Format(time.RFC3339))
	}
}

func TestClampForecastDays_DefaultWhenZeroOrNegative(t *testing.T) {
	if got := clampForecastDays(0); got != 10 {
		t.Errorf("expected 10 for days=0, got %d", got)
	}
	if got := clampForecastDays(-1); got != 10 {
		t.Errorf("expected 10 for days=-1, got %d", got)
	}
}

func TestClampForecastDays_CapAtMaximum(t *testing.T) {
	if got := clampForecastDays(20); got != 16 {
		t.Errorf("expected 16 for days=20, got %d", got)
	}
	if got := clampForecastDays(16); got != 16 {
		t.Errorf("expected 16 for days=16, got %d", got)
	}
}

func TestClampForecastDays_PassThrough(t *testing.T) {
	if got := clampForecastDays(10); got != 10 {
		t.Errorf("expected 10 for days=10, got %d", got)
	}
	if got := clampForecastDays(1); got != 1 {
		t.Errorf("expected 1 for days=1, got %d", got)
	}
}

// Captured from a live Open-Meteo Marine response at the vessel's position on
// 2026-09-03, not hand-written. The point it pins is the encoding of an
// absent component: at 00:00 there was no swell, and the provider reported
// its height, period and direction all as 0 rather than null. The host reads
// a zero period as "no such wave train", so this parse must carry those zeros
// through faithfully instead of substituting anything.
func TestParseOpenMeteoWaveResponse_CarriesPerComponentDirectionAndPeriod(t *testing.T) {
	raw := []byte(`{"hourly":{
		"time":["2026-09-03T00:00","2026-09-03T18:00"],
		"wave_height":[0.6,0.5],
		"wave_period":[7.4,7.7],
		"wave_direction":[79,71],
		"wind_wave_height":[0.6,0.26],
		"wind_wave_direction":[79,109],
		"wind_wave_period":[7.4,2.35],
		"swell_wave_height":[0.0,0.44],
		"swell_wave_direction":[0,65],
		"swell_wave_period":[0.0,7.7]
	}}`)

	points, err := parseOpenMeteoWaveResponse(raw, 0)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("expected 2 points, got %d", len(points))
	}

	// The hour with no swell in it.
	if points[0].SwellWavePeriodS != 0 || points[0].SwellWaveDirectionDeg != 0 {
		t.Fatalf("a flat swell must stay zeroed, got period %v direction %v",
			points[0].SwellWavePeriodS, points[0].SwellWaveDirectionDeg)
	}
	if points[0].WindWaveDirectionDeg != 79 || points[0].WindWavePeriodS != 7.4 {
		t.Fatalf("wind wave = %v deg at %vs, want 79 at 7.4",
			points[0].WindWaveDirectionDeg, points[0].WindWavePeriodS)
	}

	// The genuine two-system hour.
	if points[1].WindWaveDirectionDeg != 109 || points[1].WindWavePeriodS != 2.35 {
		t.Fatalf("wind wave = %v deg at %vs, want 109 at 2.35",
			points[1].WindWaveDirectionDeg, points[1].WindWavePeriodS)
	}
	if points[1].SwellWaveDirectionDeg != 65 || points[1].SwellWavePeriodS != 7.7 {
		t.Fatalf("swell = %v deg at %vs, want 65 at 7.7",
			points[1].SwellWaveDirectionDeg, points[1].SwellWavePeriodS)
	}
}

// A null or missing upstream value is a gap in the model, not a calm sea or a
// wave from due north. The combined figures are left out of the hour, and the
// JSON the host reads carries no key for them.
func TestParseOpenMeteoWaveResponse_NullCombinedFiguresAreOmitted(t *testing.T) {
	raw := []byte(`{"hourly":{
		"time":["2026-09-03T00:00","2026-09-03T01:00","2026-09-03T02:00"],
		"wave_height":[null,0.0,1.4],
		"wave_period":[null,0.0,"n/a"],
		"wave_direction":[null,0,null]
	}}`)
	points, err := parseOpenMeteoWaveResponse(raw, 0)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(points) != 3 {
		t.Fatalf("expected 3 points, got %d", len(points))
	}
	if points[0].WaveHeightM != nil || points[0].WavePeriodS != nil || points[0].WaveDirectionDeg != nil {
		t.Errorf("null entries must be absent: %+v", points[0])
	}
	if points[1].WaveHeightM != nil || points[1].WavePeriodS != nil || points[1].WaveDirectionDeg != nil {
		t.Errorf("a zero period is no wave, so the hour is left out: %+v", points[1])
	}
	if points[2].WaveHeightM == nil || *points[2].WaveHeightM != 1.4 || points[2].WavePeriodS != nil || points[2].WaveDirectionDeg != nil {
		t.Errorf("height only: %+v", points[2])
	}

	out, err := json.Marshal(fetchWavesOutput{Hourly: points})
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Hourly []map[string]any `json:"hourly"`
	}
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"wave_height_m", "wave_period_s", "wave_direction_deg"} {
		if _, ok := back.Hourly[0][key]; ok {
			t.Errorf("hour 0 must not carry %s", key)
		}
		if _, ok := back.Hourly[1][key]; ok {
			t.Errorf("hour 1 must not carry %s", key)
		}
	}
}

func TestWaveURL_PinsGFSWaveUnlessFallingBack(t *testing.T) {
	pinned := waveURL(-20.2666, 148.7099, 10, false)
	if !strings.Contains(pinned, "models=ncep_gfswave025") {
		t.Errorf("default fetch must pin GFS-Wave: %s", pinned)
	}
	blend := waveURL(-20.2666, 148.7099, 10, true)
	if strings.Contains(blend, "models=") {
		t.Errorf("fallback fetch must leave the model to Open-Meteo's blend: %s", blend)
	}
	if !strings.Contains(blend, "latitude=-20.2666") || !strings.Contains(blend, "forecast_days=10") {
		t.Errorf("fallback fetch must keep the point and window: %s", blend)
	}
}

func f64(v float64) *float64 { return &v }

func TestHoursHaveNoWaves(t *testing.T) {
	land := []waveHourOutput{{Time: "a"}, {Time: "b"}}
	if !hoursHaveNoWaves(land) {
		t.Error("no period throughout is a land cell")
	}
	if hoursHaveNoWaves(append(land, waveHourOutput{Time: "c", WavePeriodS: f64(3)})) {
		t.Error("one real hour means the model has data here")
	}
	if !hoursHaveNoWaves(nil) {
		t.Error("an empty series has no waves")
	}
}

func TestChooseWaveSeries(t *testing.T) {
	land := []waveHourOutput{{Time: "a"}}
	blend := []waveHourOutput{{Time: "a", WaveHeightM: f64(0.16), WavePeriodS: f64(3)}}
	real := []waveHourOutput{{Time: "a", WaveHeightM: f64(0.66), WavePeriodS: f64(5)}}
	never := func() ([]waveHourOutput, error) { t.Fatal("fallback fetched when it should not be"); return nil, nil }

	hours, model, used, err := chooseWaveSeries(real, true, never)
	if err != nil || used || model != modelGFSWave || len(hours) != 1 || *hours[0].WaveHeightM != 0.66 {
		t.Errorf("real data is kept: %v %v %v %v", hours, model, used, err)
	}

	hours, model, used, err = chooseWaveSeries(land, false, never)
	if err != nil || used || model != modelGFSWave || hours[0].WavePeriodS != nil {
		t.Errorf("flag off leaves the land cell for the host to report: %v %v %v %v", hours, model, used, err)
	}

	hours, model, used, err = chooseWaveSeries(land, true, func() ([]waveHourOutput, error) { return blend, nil })
	if err != nil || !used || model != modelBlend || *hours[0].WaveHeightM != 0.16 {
		t.Errorf("flag on swaps to the blend: %v %v %v %v", hours, model, used, err)
	}

	_, _, _, err = chooseWaveSeries(land, true, func() ([]waveHourOutput, error) { return nil, errors.New("boom") })
	if err == nil {
		t.Error("a failed fallback fetch must surface, not fall back to the land cell")
	}
}

func TestParseWaveBody_TakesTheOffsetFromTheResponse(t *testing.T) {
	body := []byte(`{"utc_offset_seconds":36000,"hourly":{"time":["2026-06-15T00:00"],"wave_height":[1.5],"wave_period":[7]}}`)
	hours, err := parseWaveBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if hours[0].Time != "2026-06-14T14:00:00Z" {
		t.Errorf("got %s", hours[0].Time)
	}
}

func TestFallbackEnabled(t *testing.T) {
	for in, want := range map[string]bool{"true": true, "TRUE": true, "false": false, "": false, "yes": false} {
		if got := fallbackEnabled(in); got != want {
			t.Errorf("%q: got %v want %v", in, got, want)
		}
	}
}

func TestParseOpenMeteoWaveResponse_ZeroPeriodHourIsLeftOut(t *testing.T) {
	raw := []byte(`{"hourly":{"time":["2026-06-15T00:00","2026-06-15T01:00"],
		"wave_height":[0,0.6],"wave_period":[0,7.4],"wave_direction":[0,79]}}`)
	hours, err := parseOpenMeteoWaveResponse(raw, 0)
	if err != nil {
		t.Fatal(err)
	}
	if hours[0].WaveHeightM != nil || hours[0].WavePeriodS != nil || hours[0].WaveDirectionDeg != nil {
		t.Errorf("a zero-period hour has no combined figures: %+v", hours[0])
	}
	if hours[1].WavePeriodS == nil || *hours[1].WavePeriodS != 7.4 {
		t.Errorf("a real hour is kept: %+v", hours[1])
	}
}
