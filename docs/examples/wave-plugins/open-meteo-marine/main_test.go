package main

import (
	"encoding/json"
	"errors"
	"os"
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

// currents_four_points.json is a real Open-Meteo response (timezone=GMT,
// past_days=1, forecast_days=1) for four locations: Hervey Bay, off Fraser Island, near
// Bundaberg and an inland point whose hours are all null. It has no
// location_id on the first location, as the live API sends it.
func readCurrentsFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/currents_four_points.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestCurrentsURL_OneRequestForEveryPoint(t *testing.T) {
	u := currentsURL([]currentsPoint{{-25.0, 152.6}, {-25.0, 153.6}, {-24.7, 152.5}}, 3)
	if strings.Contains(u, "models=") {
		t.Errorf("GFS-Wave carries no currents, so the request must not pin it: %s", u)
	}
	for _, want := range []string{
		"latitude=-25.0000,-25.0000,-24.7000",
		"longitude=152.6000,153.6000,152.5000",
		"hourly=ocean_current_velocity,ocean_current_direction&",
		"timezone=GMT", "forecast_days=3", "past_days=1",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("currents request must contain %q: %s", want, u)
		}
	}
	if strings.Contains(u, "wave_") || strings.Contains(u, "timezone=auto") {
		t.Errorf("the request asks for the two current variables only, in GMT: %s", u)
	}
}

func TestValidateCurrentsInput(t *testing.T) {
	ok := []currentsPoint{{-25, 152}}
	if err := validateCurrentsInput(ok); err != nil {
		t.Errorf("one point is fine: %v", err)
	}
	many := make([]currentsPoint, maxCurrentsPoints)
	for i := range many {
		many[i] = currentsPoint{-25, 152}
	}
	if err := validateCurrentsInput(many); err != nil {
		t.Errorf("exactly the cap is fine: %v", err)
	}
	cases := map[string][]currentsPoint{
		"no points":     {},
		"over the cap":  append(many, currentsPoint{-25, 152}),
		"bad latitude":  {{95, 152}},
		"bad longitude": {{-25, 181}},
	}
	for name, pts := range cases {
		if err := validateCurrentsInput(pts); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestParseOpenMeteoCurrentsResponse_RealMultiLocationFixture(t *testing.T) {
	got, err := parseOpenMeteoCurrentsResponse(readCurrentsFixture(t), 4)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("one series per point, in order; got %d", len(got))
	}
	for i, hours := range got {
		if len(hours) != 48 {
			t.Errorf("point %d: expected yesterday and today, 48 hours, got %d", i, len(hours))
		}
	}
	if got[0][0].Time != "2026-11-10T00:00:00Z" {
		t.Errorf("timezone=GMT means the times are UTC as given: %s", got[0][0].Time)
	}
	// Off Fraser is the East Australian Current: 4.2 km/h is 1.17 m/s toward 155.
	h := got[1][24] // 2026-11-11T00:00Z, after yesterday's 24 hours
	if h.CurrentVelocityMS == nil || *h.CurrentVelocityMS < 1.15 || *h.CurrentVelocityMS > 1.18 || h.CurrentSetDeg == nil || *h.CurrentSetDeg != 155 {
		t.Errorf("4.2 km/h toward 155 expected, got %+v", h)
	}
	// Inland: every hour null, no keys.
	for _, hr := range got[3] {
		if hr.CurrentVelocityMS != nil || hr.CurrentSetDeg != nil {
			t.Fatalf("a null hour carries neither key: %+v", hr)
		}
	}
	out, err := json.Marshal(fetchCurrentsOutput{Points: toPointSeries(got)})
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Points []struct {
			Hourly []map[string]any `json:"hourly"`
		} `json:"points"`
	}
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if _, ok := back.Points[3].Hourly[0]["current_velocity_ms"]; ok {
		t.Errorf("a null hour must not emit the key: %v", back.Points[3].Hourly[0])
	}
}

func toPointSeries(in [][]currentHourOutput) []currentPointOutput {
	out := make([]currentPointOutput, len(in))
	for i, h := range in {
		out[i] = currentPointOutput{Hourly: h}
	}
	return out
}

// With one location Open-Meteo answers with a single object, not an array.
func TestParseOpenMeteoCurrentsResponse_SingleLocationIsAnObject(t *testing.T) {
	var arr []json.RawMessage
	if err := json.Unmarshal(readCurrentsFixture(t), &arr); err != nil {
		t.Fatal(err)
	}
	got, err := parseOpenMeteoCurrentsResponse(arr[0], 1)
	if err != nil || len(got) != 1 {
		t.Fatalf("a single object is one location: %v %d", err, len(got))
	}
}

func TestParseOpenMeteoCurrentsResponse_AZeroIsKept(t *testing.T) {
	raw := `[{"utc_offset_seconds":0,"hourly_units":{"ocean_current_velocity":"km/h"},"hourly":{"time":["2026-11-11T00:00"],"ocean_current_velocity":[0],"ocean_current_direction":[0]}}]`
	got, err := parseOpenMeteoCurrentsResponse([]byte(raw), 1)
	if err != nil {
		t.Fatal(err)
	}
	if got[0][0].CurrentVelocityMS == nil || *got[0][0].CurrentVelocityMS != 0 || got[0][0].CurrentSetDeg == nil || *got[0][0].CurrentSetDeg != 0 {
		t.Errorf("a reported 0 is a real slack figure: %+v", got[0][0])
	}
}

// Anything that is not a clean answer is an error for the host to report. Only
// a JSON null in both arrays for an hour means "no current that hour".
func TestParseOpenMeteoCurrentsResponse_MalformedResponsesAreErrors(t *testing.T) {
	loc := func(offset, units, hourly string) string {
		return `{` + offset + units + `"hourly":{` + hourly + `}}`
	}
	off := `"utc_offset_seconds":0,`
	units := `"hourly_units":{"ocean_current_velocity":"km/h"},`
	one := func(hourly string) string { return `[` + loc(off, units, hourly) + `]` }
	t1 := `"time":["2026-11-11T00:00"],`
	cases := map[string]string{
		"not json":                   `nope`,
		"velocity array missing":     one(t1 + `"ocean_current_direction":[90]`),
		"direction array missing":    one(t1 + `"ocean_current_velocity":[1]`),
		"velocity not an array":      one(t1 + `"ocean_current_velocity":"x","ocean_current_direction":[90]`),
		"units missing":              `[` + loc(off, ``, t1+`"ocean_current_velocity":[1],"ocean_current_direction":[90]`) + `]`,
		"units not km/h":             `[` + loc(off, `"hourly_units":{"ocean_current_velocity":"kn"},`, t1+`"ocean_current_velocity":[1],"ocean_current_direction":[90]`) + `]`,
		"offset missing":             `[` + loc(``, units, t1+`"ocean_current_velocity":[1],"ocean_current_direction":[90]`) + `]`,
		"offset not zero":            `[` + loc(`"utc_offset_seconds":36000,`, units, t1+`"ocean_current_velocity":[1],"ocean_current_direction":[90]`) + `]`,
		"arrays of unequal length":   one(`"time":["2026-11-11T00:00","2026-11-11T01:00"],"ocean_current_velocity":[1],"ocean_current_direction":[90,90]`),
		"unparseable time":           one(`"time":["yesterday"],"ocean_current_velocity":[1],"ocean_current_direction":[90]`),
		"time not a string":          one(`"time":[5],"ocean_current_velocity":[1],"ocean_current_direction":[90]`),
		"negative velocity":          one(t1 + `"ocean_current_velocity":[-1],"ocean_current_direction":[90]`),
		"direction above 360":        one(t1 + `"ocean_current_velocity":[1],"ocean_current_direction":[400]`),
		"direction below 0":          one(t1 + `"ocean_current_velocity":[1],"ocean_current_direction":[-5]`),
		"non-numeric velocity":       one(t1 + `"ocean_current_velocity":["fast"],"ocean_current_direction":[90]`),
		"velocity without direction": one(t1 + `"ocean_current_velocity":[1],"ocean_current_direction":[null]`),
		"direction without velocity": one(t1 + `"ocean_current_velocity":[null],"ocean_current_direction":[90]`),
		"fewer locations than asked": one(t1 + `"ocean_current_velocity":[1],"ocean_current_direction":[90]`),
		"hourly missing":             `[{"utc_offset_seconds":0,` + units + `"x":1}]`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			n := 1
			if name == "fewer locations than asked" {
				n = 2
			}
			if _, err := parseOpenMeteoCurrentsResponse([]byte(raw), n); err == nil {
				t.Errorf("expected an error")
			}
		})
	}
}

func TestWaveHoursCarryNoCurrentFields(t *testing.T) {
	out, err := json.Marshal(waveHourOutput{Time: "2026-11-10T14:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "current") {
		t.Errorf("fetch_waves leaves currents to fetch_currents: %s", out)
	}
	if strings.Contains(marineWaveURLFmt, "ocean_current") {
		t.Errorf("the wave request must not ask for currents: %s", marineWaveURLFmt)
	}
}
