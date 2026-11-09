package main

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestParseMarineReadsTheRealResponse(t *testing.T) {
	samples, err := parseMarine(readFixture(t, "marine_venice.json"))
	if err != nil {
		t.Fatalf("parseMarine: %v", err)
	}
	// past_days=1 + forecast_days=7 is eight days of hours.
	if len(samples) != 8*24 {
		t.Fatalf("samples = %d, want %d", len(samples), 8*24)
	}
	if samples[1].T.Sub(samples[0].T) != time.Hour {
		t.Errorf("samples are not hourly: %v then %v", samples[0].T, samples[1].T)
	}
	if samples[0].T.Location() != time.UTC {
		t.Errorf("times must be UTC, got %v", samples[0].T.Location())
	}
}

func TestParseMarineSkipsNullsButKeepsTheRest(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(readFixture(t, "marine_venice.json"), &doc); err != nil {
		t.Fatal(err)
	}
	hourly := doc["hourly"].(map[string]any)
	heights := hourly["sea_level_height_msl"].([]any)
	heights[5], heights[6] = nil, nil
	body, _ := json.Marshal(doc)

	samples, err := parseMarine(body)
	if err != nil {
		t.Fatalf("parseMarine: %v", err)
	}
	if len(samples) != 8*24-2 {
		t.Fatalf("samples = %d, want %d", len(samples), 8*24-2)
	}
}

func TestParseMarineAllNullIsAnError(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(readFixture(t, "marine_venice.json"), &doc); err != nil {
		t.Fatal(err)
	}
	hourly := doc["hourly"].(map[string]any)
	heights := hourly["sea_level_height_msl"].([]any)
	for i := range heights {
		heights[i] = nil
	}
	body, _ := json.Marshal(doc)

	if _, err := parseMarine(body); err == nil {
		t.Fatal("an all-null series must be an error, got none")
	}
}

func TestParseMarineEmptySeriesIsAnError(t *testing.T) {
	if _, err := parseMarine([]byte(`{"hourly":{"time":[],"sea_level_height_msl":[]}}`)); err == nil {
		t.Fatal("an empty series must be an error")
	}
}

func TestParseMarineSurfacesTheServiceReason(t *testing.T) {
	_, err := parseMarine(readFixture(t, "marine_error.json"))
	if err == nil {
		t.Fatal("expected an error for the service's error body")
	}
	if !strings.Contains(err.Error(), "Latitude must be in range") {
		t.Errorf("error should carry the service's reason, got %q", err)
	}
}

func TestParseMarineMismatchedLengthsIsAnError(t *testing.T) {
	_, err := parseMarine([]byte(`{"hourly":{"time":["2026-11-07T00:00","2026-11-07T01:00"],"sea_level_height_msl":[0.1]}}`))
	if err == nil {
		t.Fatal("mismatched time and height lengths must be an error")
	}
}

func TestDeriveExtremesAlternateOnEveryRealFixture(t *testing.T) {
	for _, name := range []string{"marine_venice.json", "marine_usvi.json", "marine_sydney.json"} {
		t.Run(name, func(t *testing.T) {
			samples, err := parseMarine(readFixture(t, name))
			if err != nil {
				t.Fatalf("parseMarine: %v", err)
			}
			extremes := deriveExtremes(samples)
			if len(extremes) < 8 {
				t.Fatalf("expected at least 8 extremes over eight days, got %d", len(extremes))
			}
			for i := 1; i < len(extremes); i++ {
				if extremes[i].High == extremes[i-1].High {
					t.Fatalf("extremes %d and %d are both %v", i-1, i, extremes[i].High)
				}
				if !extremes[i].T.After(extremes[i-1].T) {
					t.Fatalf("extremes not in time order at %d", i)
				}
			}
			first, last := samples[0].T, samples[len(samples)-1].T
			for _, e := range extremes {
				if e.T.Before(first) || e.T.After(last) {
					t.Errorf("extreme at %v is outside the series %v..%v", e.T, first, last)
				}
			}
		})
	}
}

func TestDeriveExtremesKeepsNegativeMSLHeights(t *testing.T) {
	// Venice ran below mean sea level all week; the heights must come out as
	// given, not clamped or shifted.
	samples, err := parseMarine(readFixture(t, "marine_venice.json"))
	if err != nil {
		t.Fatal(err)
	}
	extremes := deriveExtremes(samples)
	negative := 0
	for _, e := range extremes {
		if e.HeightM < 0 {
			negative++
		}
	}
	if negative == 0 {
		t.Fatal("expected negative extremes from the Venice series")
	}
}

func TestDeriveExtremesParabolaRefinesTimeAndHeight(t *testing.T) {
	// A parabola with its true peak at 1.25 h, height 2.0:
	// y = 2 - 0.5*(t-1.25)^2 sampled at t = 0,1,2,3.
	base := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	var samples []sample
	for h := 0; h < 4; h++ {
		x := float64(h) - 1.25
		samples = append(samples, sample{T: base.Add(time.Duration(h) * time.Hour), HeightM: 2 - 0.5*x*x})
	}
	got := deriveExtremes(samples)
	if len(got) != 1 || !got[0].High {
		t.Fatalf("want one high, got %+v", got)
	}
	wantT := base.Add(75 * time.Minute)
	if d := got[0].T.Sub(wantT); d < -time.Second || d > time.Second {
		t.Errorf("peak time = %v, want %v", got[0].T, wantT)
	}
	if math.Abs(got[0].HeightM-2.0) > 1e-9 {
		t.Errorf("peak height = %v, want 2.0", got[0].HeightM)
	}
}

func TestDeriveExtremesIgnoresASeriesWithAGap(t *testing.T) {
	// A null in the middle leaves two hours between neighbours; a parabola
	// through points that far apart would invent a time, so no extreme is
	// reported across the gap.
	base := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	samples := []sample{
		{T: base, HeightM: 0.0},
		{T: base.Add(1 * time.Hour), HeightM: 0.5},
		{T: base.Add(3 * time.Hour), HeightM: 0.4}, // gap: 2 h after the peak sample
		{T: base.Add(4 * time.Hour), HeightM: 0.0},
	}
	if got := deriveExtremes(samples); len(got) != 0 {
		t.Fatalf("no extreme may be derived across a gap, got %+v", got)
	}
}

func TestDeriveExtremesFlatTopGivesOneExtreme(t *testing.T) {
	base := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	heights := []float64{0, 1, 1, 0}
	var samples []sample
	for i, h := range heights {
		samples = append(samples, sample{T: base.Add(time.Duration(i) * time.Hour), HeightM: h})
	}
	got := deriveExtremes(samples)
	if len(got) != 1 || !got[0].High {
		t.Fatalf("a two-sample flat top must give one high, got %+v", got)
	}
	if want := base.Add(90 * time.Minute); !got[0].T.Equal(want) {
		t.Errorf("flat top is placed at %v, want its middle %v", got[0].T, want)
	}
}

func seriesOf(heights ...float64) []sample {
	base := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	var samples []sample
	for i, h := range heights {
		samples = append(samples, sample{T: base.Add(time.Duration(i) * time.Hour), HeightM: h})
	}
	return samples
}

func TestDeriveExtremesFlatStepOnASlopeIsNotATurn(t *testing.T) {
	// The service rounds to a centimetre, so a falling tide can hold one value
	// for two hours. That is a slope, not a low.
	if got := deriveExtremes(seriesOf(0.46, 0.44, 0.43, 0.43, 0.42, 0.41)); len(got) != 0 {
		t.Fatalf("a step on a slope is not an extreme, got %+v", got)
	}
}

func TestDeriveExtremesDropsACentimetreWiggle(t *testing.T) {
	// 0.50 high, a one-centimetre dip and recovery, then the real fall.
	got := deriveExtremes(seriesOf(0.30, 0.40, 0.50, 0.49, 0.50, 0.40, 0.30, 0.20, 0.30))
	if len(got) != 2 || !got[0].High || got[1].High {
		t.Fatalf("want one high then one low, got %+v", got)
	}
}

func TestParsePosition(t *testing.T) {
	lat, lon, err := parsePosition(" 45.43,12.34 ")
	if err != nil || lat != 45.43 || lon != 12.34 {
		t.Fatalf("got %v %v %v", lat, lon, err)
	}
	for _, bad := range []string{"", "45.43", "a,b", "91,0", "0,181", "NaN,0", "1,2,3"} {
		if _, _, err := parsePosition(bad); err == nil {
			t.Errorf("parsePosition(%q) should fail", bad)
		}
	}
}

func TestRequestURLAsksForSeaCellsAndGMT(t *testing.T) {
	u := requestURL(45.43, 12.34)
	for _, want := range []string{
		"https://marine-api.open-meteo.com/v1/marine?",
		"latitude=45.43", "longitude=12.34",
		"hourly=sea_level_height_msl", "timezone=GMT", "cell_selection=sea",
		"past_days=1", "forecast_days=7",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("url %q lacks %q", u, want)
		}
	}
}

func TestChartOutputCarriesTheExtremesAsRFC3339(t *testing.T) {
	out, err := buildChart("45.43,12.34", readFixture(t, "marine_venice.json"))
	if err != nil {
		t.Fatal(err)
	}
	if out.Station.StationID != "45.43,12.34" {
		t.Errorf("station id = %q", out.Station.StationID)
	}
	if len(out.Extremes) == 0 {
		t.Fatal("no extremes")
	}
	if _, err := time.Parse(time.RFC3339, out.Extremes[0].Time); err != nil {
		t.Errorf("time %q is not RFC 3339: %v", out.Extremes[0].Time, err)
	}
}
