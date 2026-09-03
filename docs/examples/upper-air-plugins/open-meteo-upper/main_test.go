package main

import (
	"strings"
	"testing"
)

func TestUpperAirRequestURL(t *testing.T) {
	url := upperAirRequestURL(-20.3463, 148.9503, 16)

	for _, want := range []string{
		"geopotential_height_500hPa",
		"geopotential_height_1000hPa",
		"wind_speed_500hPa",
		"temperature_500hPa",
		// Verified live: this applies to pressure-level winds too, which is
		// why nothing here converts wind speed.
		"wind_speed_unit=ms",
		// UTC, because the host does its own local-day bucketing.
		"timezone=UTC",
		"forecast_days=16",
	} {
		if !strings.Contains(url, want) {
			t.Fatalf("URL missing %q:\n%s", want, url)
		}
	}
}

func TestClampForecastDays(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{0, 1}, {-5, 1}, {10, 10}, {16, 16}, {30, 16}} {
		if got := clampForecastDays(tc.in); got != tc.want {
			t.Fatalf("clampForecastDays(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// Values captured from a live response at the vessel's position on
// 2026-09-03: 5878m at 500hPa over 186m at 1000hPa, wind 9.74 m/s, -2.1C.
func TestParseOpenMeteoUpperAir_RealResponse(t *testing.T) {
	raw := []byte(`{"hourly":{
		"time":["2026-09-03T00:00","2026-09-03T06:00"],
		"geopotential_height_500hPa":[5878.0,5875.0],
		"geopotential_height_1000hPa":[186.0,189.0],
		"wind_speed_500hPa":[9.74,5.0],
		"temperature_500hPa":[-2.1,-3.0]
	}}`)

	out, err := parseOpenMeteoUpperAir(raw)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 hours, got %d", len(out))
	}
	if out[0].Time != "2026-09-03T00:00:00Z" {
		t.Fatalf("time = %q, want an RFC3339 UTC stamp", out[0].Time)
	}
	if out[0].GeopotentialHeight500M != 5878 || out[0].GeopotentialHeight1000M != 186 {
		t.Fatalf("heights = %v / %v, want 5878 / 186", out[0].GeopotentialHeight500M, out[0].GeopotentialHeight1000M)
	}
	if out[0].WindSpeed500MS != 9.74 || out[0].Temperature500C != -2.1 {
		t.Fatalf("wind/temp = %v / %v, want 9.74 / -2.1", out[0].WindSpeed500MS, out[0].Temperature500C)
	}
}

// Open-Meteo returns null at the tail of a 16-day run, and omits the arrays
// altogether for a model with no pressure levels. Zero is the contract's
// marker for absence, so both must land there rather than panicking.
func TestParseOpenMeteoUpperAir_NullsAndMissingArrays(t *testing.T) {
	raw := []byte(`{"hourly":{
		"time":["2026-09-03T00:00","2026-09-03T01:00"],
		"geopotential_height_500hPa":[5878.0,null]
	}}`)

	out, err := parseOpenMeteoUpperAir(raw)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if out[0].GeopotentialHeight500M != 5878 {
		t.Fatalf("first hour lost its height: %v", out[0].GeopotentialHeight500M)
	}
	if out[1].GeopotentialHeight500M != 0 {
		t.Fatalf("a null must read as absent, got %v", out[1].GeopotentialHeight500M)
	}
	// Arrays absent entirely.
	if out[0].WindSpeed500MS != 0 || out[0].Temperature500C != 0 {
		t.Fatalf("missing arrays must read as absent, got %v / %v", out[0].WindSpeed500MS, out[0].Temperature500C)
	}
}

func TestParseOpenMeteoUpperAir_HardErrors(t *testing.T) {
	if _, err := parseOpenMeteoUpperAir([]byte(`not json`)); err == nil {
		t.Fatal("expected an error for unparseable JSON")
	}
	if _, err := parseOpenMeteoUpperAir([]byte(`{}`)); err == nil {
		t.Fatal("expected an error when hourly is missing")
	}
	if _, err := parseOpenMeteoUpperAir([]byte(`{"hourly":{}}`)); err == nil {
		t.Fatal("expected an error when hourly.time is missing")
	}
}
