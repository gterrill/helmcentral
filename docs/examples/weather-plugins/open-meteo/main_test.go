package main

import (
	"strings"
	"testing"
	"time"
)

func TestWmoCodeToCondition(t *testing.T) {
	tests := []struct {
		code     int
		expected string
	}{
		// All codes from the table
		{0, "clear"},
		{1, "mostlyclear"},
		{2, "partlycloudy"},
		{3, "cloudy"},
		{45, "foggy"},
		{48, "foggy"},
		{51, "drizzle"},
		{53, "drizzle"},
		{55, "drizzle"},
		{56, "freezingdrizzle"},
		{57, "freezingdrizzle"},
		{61, "rain"},
		{63, "rain"},
		{65, "heavyrain"},
		{66, "freezingrain"},
		{67, "freezingrain"},
		{71, "snow"},
		{73, "snow"},
		{77, "snow"},
		{75, "heavysnow"},
		{80, "rain"},
		{81, "rain"},
		{82, "heavyrain"},
		{85, "snow"},
		{86, "heavysnow"},
		{95, "thunderstorms"},
		{96, "thunderstorms"},
		{99, "thunderstorms"},
		// Fallback case: unknown code
		{200, "cloudy"},
		{999, "cloudy"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := wmoCodeToCondition(tt.code)
			if got != tt.expected {
				t.Errorf("wmoCodeToCondition(%d) = %q, want %q", tt.code, got, tt.expected)
			}
		})
	}
}

func TestParseOpenMeteoLocalTime_PositiveOffset(t *testing.T) {
	// Sydney timezone: UTC+10:00 (36000 seconds)
	// Open-Meteo returns "2026-07-19T18:30" which is 18:30 local = 08:30 UTC
	localStr := "2026-07-19T18:30"
	offsetSeconds := 36000 // +10:00

	parsed, err := parseOpenMeteoLocalTime(localStr, "2006-01-02T15:04", offsetSeconds)
	if err != nil {
		t.Fatalf("parseOpenMeteoLocalTime failed: %v", err)
	}

	// Expected UTC time: 2026-07-19 08:30:00 UTC
	expected, _ := time.Parse(time.RFC3339, "2026-07-19T08:30:00Z")
	if !parsed.Equal(expected) {
		t.Errorf("parseOpenMeteoLocalTime(%q, +36000) = %v, want %v",
			localStr, parsed.Format(time.RFC3339), expected.Format(time.RFC3339))
	}
}

func TestParseOpenMeteoLocalTime_NegativeOffset(t *testing.T) {
	// US Eastern timezone: UTC-05:00 (-18000 seconds)
	// Open-Meteo returns "2026-07-19T14:30" which is 14:30 local = 19:30 UTC
	localStr := "2026-07-19T14:30"
	offsetSeconds := -18000 // -05:00

	parsed, err := parseOpenMeteoLocalTime(localStr, "2006-01-02T15:04", offsetSeconds)
	if err != nil {
		t.Fatalf("parseOpenMeteoLocalTime failed: %v", err)
	}

	// Expected UTC time: 2026-07-19 19:30:00 UTC
	expected, _ := time.Parse(time.RFC3339, "2026-07-19T19:30:00Z")
	if !parsed.Equal(expected) {
		t.Errorf("parseOpenMeteoLocalTime(%q, -18000) = %v, want %v",
			localStr, parsed.Format(time.RFC3339), expected.Format(time.RFC3339))
	}
}

func TestParseOpenMeteoLocalTime_DateOnly(t *testing.T) {
	// Daily times are date-only, representing local midnight
	// Sydney, UTC+10:00
	// "2026-07-19" means 2026-07-19 00:00 local = 2026-07-18 14:00 UTC
	localStr := "2026-07-19"
	offsetSeconds := 36000 // +10:00

	parsed, err := parseOpenMeteoLocalTime(localStr, "2006-01-02", offsetSeconds)
	if err != nil {
		t.Fatalf("parseOpenMeteoLocalTime failed: %v", err)
	}

	// Expected UTC time: 2026-07-18 14:00:00 UTC
	expected, _ := time.Parse(time.RFC3339, "2026-07-18T14:00:00Z")
	if !parsed.Equal(expected) {
		t.Errorf("parseOpenMeteoLocalTime(%q, +36000) = %v, want %v",
			localStr, parsed.Format(time.RFC3339), expected.Format(time.RFC3339))
	}
}

func TestParseOpenMeteoForecast_FullResponse(t *testing.T) {
	// Synthetic Open-Meteo response with 1 day of daily data and 2 hours of hourly data
	resp := &openMeteoResponse{
		Latitude:         -33.8688,
		Longitude:        151.2093,
		UTCOffsetSeconds: 36000, // Sydney: UTC+10:00
		Timezone:         "Australia/Sydney",
		Current: &struct {
			Time                     string  `json:"time"`
			Temperature2m            float64 `json:"temperature_2m"`
			WeatherCode              int     `json:"weather_code"`
			WindSpeed10m             float64 `json:"wind_speed_10m"`
			WindGusts10m             float64 `json:"wind_gusts_10m"`
			WindDirection10m         int     `json:"wind_direction_10m"`
			IsDay                    int     `json:"is_day"`
			PrecipitationProbability int     `json:"precipitation_probability"`
		}{
			Time:                     "2026-07-19T18:30",
			Temperature2m:            14.5,
			WeatherCode:              2, // partlycloudy
			WindSpeed10m:             5.5,
			WindGusts10m:             12.3,
			WindDirection10m:         180,
			IsDay:                    0,
			PrecipitationProbability: 25,
		},
		Daily: &struct {
			Time                        []string  `json:"time"`
			WeatherCode                 []int     `json:"weather_code"`
			Temperature2mMax            []float64 `json:"temperature_2m_max"`
			Temperature2mMin            []float64 `json:"temperature_2m_min"`
			WindSpeed10mMax             []float64 `json:"wind_speed_10m_max"`
			WindGusts10mMax             []float64 `json:"wind_gusts_10m_max"`
			WindDirection10mDominant    []int     `json:"wind_direction_10m_dominant"`
			PrecipitationProbabilityMax []int     `json:"precipitation_probability_max"`
			Sunrise                     []string  `json:"sunrise"`
			Sunset                      []string  `json:"sunset"`
		}{
			Time:                        []string{"2026-07-19"},
			WeatherCode:                 []int{3}, // cloudy
			Temperature2mMax:            []float64{22.0},
			Temperature2mMin:            []float64{12.0},
			WindSpeed10mMax:             []float64{8.0},
			WindGusts10mMax:             []float64{15.0},
			WindDirection10mDominant:    []int{190},
			PrecipitationProbabilityMax: []int{30},
			Sunrise:                     []string{"2026-07-19T06:45"},
			Sunset:                      []string{"2026-07-19T17:15"},
		},
		Hourly: &struct {
			Time                     []string  `json:"time"`
			Temperature2m            []float64 `json:"temperature_2m"`
			WeatherCode              []int     `json:"weather_code"`
			WindSpeed10m             []float64 `json:"wind_speed_10m"`
			WindGusts10m             []float64 `json:"wind_gusts_10m"`
			WindDirection10m         []int     `json:"wind_direction_10m"`
			PrecipitationProbability []int     `json:"precipitation_probability"`
			Precipitation            []float64 `json:"precipitation"`
			UVIndex                  []float64 `json:"uv_index"`
			IsDay                    []int     `json:"is_day"`
		}{
			Time:                     []string{"2026-07-19T17:00", "2026-07-19T18:00"},
			Temperature2m:            []float64{15.2, 14.5},
			WeatherCode:              []int{2, 3},
			WindSpeed10m:             []float64{5.0, 5.5},
			WindGusts10m:             []float64{11.0, 12.3},
			WindDirection10m:         []int{175, 180},
			PrecipitationProbability: []int{20, 25},
			Precipitation:            []float64{0.0, 0.1},
			UVIndex:                  []float64{0.0, 0.0},
			IsDay:                    []int{0, 0},
		},
	}

	out, err := parseOpenMeteoForecast(resp)
	if err != nil {
		t.Fatalf("parseOpenMeteoForecast failed: %v", err)
	}

	// Verify current conditions
	if out.Current.TemperatureC != 14.5 {
		t.Errorf("current.temperature_c = %v, want 14.5", out.Current.TemperatureC)
	}
	if out.Current.Condition != "partlycloudy" {
		t.Errorf("current.condition = %q, want partlycloudy", out.Current.Condition)
	}
	if out.Current.WindSpeedMS != 5.5 {
		t.Errorf("current.wind_speed_ms = %v, want 5.5", out.Current.WindSpeedMS)
	}
	if out.Current.WindGustMS != 12.3 {
		t.Errorf("current.wind_gust_ms = %v, want 12.3", out.Current.WindGustMS)
	}
	if out.Current.WindDirectionDeg != 180.0 {
		t.Errorf("current.wind_direction_deg = %v, want 180", out.Current.WindDirectionDeg)
	}
	if out.Current.PrecipitationChancePct != 25.0 {
		t.Errorf("current.precipitation_chance_pct = %v, want 25", out.Current.PrecipitationChancePct)
	}

	// Verify current.time is RFC3339 UTC (2026-07-19T18:30 Sydney = 2026-07-19T08:30 UTC)
	expectedCurrentTime := "2026-07-19T08:30:00Z"
	if out.Current.Time != expectedCurrentTime {
		t.Errorf("current.time = %q, want %q", out.Current.Time, expectedCurrentTime)
	}

	// Verify daily data
	if len(out.Days) != 1 {
		t.Fatalf("expected 1 day, got %d", len(out.Days))
	}

	day := out.Days[0]
	if day.Condition != "cloudy" {
		t.Errorf("days[0].condition = %q, want cloudy", day.Condition)
	}
	if day.TempMaxC != 22.0 {
		t.Errorf("days[0].temp_max_c = %v, want 22.0", day.TempMaxC)
	}
	if day.TempMinC != 12.0 {
		t.Errorf("days[0].temp_min_c = %v, want 12.0", day.TempMinC)
	}
	if day.WindSpeedMS != 8.0 {
		t.Errorf("days[0].wind_speed_ms = %v, want 8.0", day.WindSpeedMS)
	}
	if day.PrecipitationChancePct != 30.0 {
		t.Errorf("days[0].precipitation_chance_pct = %v, want 30", day.PrecipitationChancePct)
	}

	// Verify day.start is RFC3339 UTC (2026-07-19 midnight Sydney = 2026-07-18T14:00 UTC)
	expectedDayStart := "2026-07-18T14:00:00Z"
	if day.Start != expectedDayStart {
		t.Errorf("days[0].start = %q, want %q", day.Start, expectedDayStart)
	}

	// Verify sunrise/sunset are RFC3339 UTC
	// 2026-07-19T06:45 Sydney = 2026-07-18T20:45 UTC
	// 2026-07-19T17:15 Sydney = 2026-07-19T07:15 UTC
	expectedSunrise := "2026-07-18T20:45:00Z"
	expectedSunset := "2026-07-19T07:15:00Z"
	if day.Sunrise != expectedSunrise {
		t.Errorf("days[0].sunrise = %q, want %q", day.Sunrise, expectedSunrise)
	}
	if day.Sunset != expectedSunset {
		t.Errorf("days[0].sunset = %q, want %q", day.Sunset, expectedSunset)
	}

	// Verify hourly data
	if len(out.Hourly) != 2 {
		t.Fatalf("expected 2 hourly entries, got %d", len(out.Hourly))
	}

	hour0 := out.Hourly[0]
	if hour0.TemperatureC != 15.2 {
		t.Errorf("hourly[0].temperature_c = %v, want 15.2", hour0.TemperatureC)
	}
	if hour0.Condition != "partlycloudy" {
		t.Errorf("hourly[0].condition = %q, want partlycloudy", hour0.Condition)
	}
	if hour0.PrecipitationChancePct != 20.0 {
		t.Errorf("hourly[0].precipitation_chance_pct = %v, want 20", hour0.PrecipitationChancePct)
	}
	if hour0.PrecipitationMM != 0.0 {
		t.Errorf("hourly[0].precipitation_mm = %v, want 0.0", hour0.PrecipitationMM)
	}
	if hour0.UVIndex != 0.0 {
		t.Errorf("hourly[0].uv_index = %v, want 0.0", hour0.UVIndex)
	}
	if hour0.IsDaylight != false {
		t.Errorf("hourly[0].is_daylight = %v, want false", hour0.IsDaylight)
	}

	// Verify hourly[0].time is RFC3339 UTC (2026-07-19T17:00 Sydney = 2026-07-19T07:00 UTC)
	expectedHour0Time := "2026-07-19T07:00:00Z"
	if hour0.Time != expectedHour0Time {
		t.Errorf("hourly[0].time = %q, want %q", hour0.Time, expectedHour0Time)
	}

	hour1 := out.Hourly[1]
	if hour1.PrecipitationMM != 0.1 {
		t.Errorf("hourly[1].precipitation_mm = %v, want 0.1", hour1.PrecipitationMM)
	}

	// Verify hourly[1].time is RFC3339 UTC (2026-07-19T18:00 Sydney = 2026-07-19T08:00 UTC)
	expectedHour1Time := "2026-07-19T08:00:00Z"
	if hour1.Time != expectedHour1Time {
		t.Errorf("hourly[1].time = %q, want %q", hour1.Time, expectedHour1Time)
	}
}

func TestParseOpenMeteoForecast_MissingCurrent(t *testing.T) {
	resp := &openMeteoResponse{
		Latitude:         -33.8688,
		Longitude:        151.2093,
		UTCOffsetSeconds: 36000,
		Timezone:         "Australia/Sydney",
		Current:          nil, // Missing current
	}

	_, err := parseOpenMeteoForecast(resp)
	if err == nil {
		t.Fatalf("expected error for missing current, got nil")
	}
	if err.Error() != "missing current conditions in Open-Meteo response" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseOpenMeteoForecast_MalformedCurrentTime(t *testing.T) {
	resp := &openMeteoResponse{
		Latitude:         -33.8688,
		Longitude:        151.2093,
		UTCOffsetSeconds: 36000,
		Timezone:         "Australia/Sydney",
		Current: &struct {
			Time                     string  `json:"time"`
			Temperature2m            float64 `json:"temperature_2m"`
			WeatherCode              int     `json:"weather_code"`
			WindSpeed10m             float64 `json:"wind_speed_10m"`
			WindGusts10m             float64 `json:"wind_gusts_10m"`
			WindDirection10m         int     `json:"wind_direction_10m"`
			IsDay                    int     `json:"is_day"`
			PrecipitationProbability int     `json:"precipitation_probability"`
		}{
			Time: "not-a-valid-time",
		},
	}

	_, err := parseOpenMeteoForecast(resp)
	if err == nil {
		t.Fatalf("expected error for malformed time, got nil")
	}
	if err.Error() != "failed to parse current.time: parsing time \"not-a-valid-time\" as \"2006-01-02T15:04\": cannot parse \"not-a-valid-time\" as \"2006\"" {
		t.Logf("got error: %v", err)
	}
}

// --- request URL ---

// Open-Meteo rolls its daily[] arrays up on the timezone named in the
// request. `timezone=auto` picks the true IANA zone at the coordinates,
// which is NOT always the offset the host buckets its own day series on
// (vesselLocalLocation derives a fixed offset from longitude, so e.g. eastern
// Spain or western China disagree with their civil zone by hours). Where the
// two disagree, the day summary and the hourly series shown beside it
// describe different windows. The host now names the zone; the plugin must
// use it verbatim rather than asking Open-Meteo to guess.
func TestOpenMeteoRequestURL_UsesCallerTimezone(t *testing.T) {
	url := openMeteoRequestURL(wasmFetchForecastInput{Lat: -21.1113, Lon: 149.2277, Days: 10, Timezone: "Etc/GMT-10"})

	if !strings.Contains(url, "timezone=Etc%2FGMT-10") {
		t.Errorf("expected the caller's escaped timezone in the URL, got: %s", url)
	}
	if strings.Contains(url, "timezone=auto") {
		t.Errorf("expected timezone=auto to be gone, got: %s", url)
	}
	if !strings.Contains(url, "forecast_days=10") {
		t.Errorf("expected forecast_days to carry through, got: %s", url)
	}
}

// Days clamping is Open-Meteo's documented 1-16 range; 0 means "unset" and
// takes the plugin's 7-day default. This moved out of main.go with the URL
// builder, so it needs coverage here.
func TestOpenMeteoRequestURL_ClampsDaysToSupportedRange(t *testing.T) {
	for _, tc := range []struct {
		days int
		want string
	}{
		{0, "forecast_days=7"},
		{-3, "forecast_days=7"},
		{10, "forecast_days=10"},
		{99, "forecast_days=16"},
	} {
		url := openMeteoRequestURL(wasmFetchForecastInput{Lat: 1, Lon: 2, Days: tc.days, Timezone: "UTC"})
		if !strings.Contains(url, tc.want) {
			t.Errorf("days=%d: expected %q in URL, got: %s", tc.days, tc.want, url)
		}
	}
}
