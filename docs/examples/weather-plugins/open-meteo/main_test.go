package main

import (
	"encoding/json"
	"os"
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
			Time                     []string   `json:"time"`
			Temperature2m            []float64  `json:"temperature_2m"`
			WeatherCode              []int      `json:"weather_code"`
			WindSpeed10m             []float64  `json:"wind_speed_10m"`
			WindGusts10m             []float64  `json:"wind_gusts_10m"`
			WindDirection10m         []int      `json:"wind_direction_10m"`
			PrecipitationProbability []int      `json:"precipitation_probability"`
			Precipitation            []float64  `json:"precipitation"`
			UVIndex                  []float64  `json:"uv_index"`
			IsDay                    []int      `json:"is_day"`
			RelativeHumidity2m       []*float64 `json:"relative_humidity_2m"`
			Visibility               []*float64 `json:"visibility"`
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
			// RelativeHumidity2m/Visibility deliberately omitted here - this
			// test predates humidity/visibility and pins the rest of the
			// mapping; see TestParseOpenMeteoForecast_MapsHumidityAndVisibility.
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

// minutely_15 is Open-Meteo's nowcast dataset - see next_hour's mapping
// below. forecast_minutely_15=8 bounds the response to 2 hours of 15-minute
// points (Open-Meteo's undocumented default is 288 points/3 days, confirmed
// via a live probe - far more than a next-hour nowcast needs), leaving
// enough cushion past 60 minutes that the host/frontend's "next 60 minutes
// from now" window is always covered even right at a 15-minute boundary.
func TestOpenMeteoRequestURL_RequestsMinutely15Precipitation(t *testing.T) {
	url := openMeteoRequestURL(wasmFetchForecastInput{Lat: -18.65, Lon: 146.48, Days: 7, Timezone: "Etc/GMT-10"})
	if !strings.Contains(url, "minutely_15=precipitation,precipitation_probability") {
		t.Errorf("expected minutely_15 precipitation variables in the URL, got: %s", url)
	}
	if !strings.Contains(url, "forecast_minutely_15=8") {
		t.Errorf("expected forecast_minutely_15=8 to bound the nowcast window, got: %s", url)
	}
}

// Confirmed via a live capture (docs/examples/weather-plugins/open-meteo/testdata/open_meteo_response_16day_sydney.json)
// that Open-Meteo requires these to be explicitly requested; they are not
// included in the plugin's pre-existing hourly parameter list.
func TestOpenMeteoRequestURL_RequestsHumidityAndVisibility(t *testing.T) {
	url := openMeteoRequestURL(wasmFetchForecastInput{Lat: 1, Lon: 2, Days: 7, Timezone: "UTC"})
	if !strings.Contains(url, "relative_humidity_2m") {
		t.Errorf("expected relative_humidity_2m in the hourly parameter list, got: %s", url)
	}
	if !strings.Contains(url, "visibility") {
		t.Errorf("expected visibility in the hourly parameter list, got: %s", url)
	}
}

// Open-Meteo reports relative_humidity_2m as a 0-100 percentage already (see
// hourly_units in the live fixture) and visibility in metres - both pass
// through unconverted onto this plugin's *float64 wire fields.
func TestParseOpenMeteoForecast_MapsHumidityAndVisibility(t *testing.T) {
	humidity := []float64{55.0, 62.0}
	visibility := []float64{24140.0, 18000.0}
	resp := &openMeteoResponse{
		UTCOffsetSeconds: 0,
		Current: &struct {
			Time                     string  `json:"time"`
			Temperature2m            float64 `json:"temperature_2m"`
			WeatherCode              int     `json:"weather_code"`
			WindSpeed10m             float64 `json:"wind_speed_10m"`
			WindGusts10m             float64 `json:"wind_gusts_10m"`
			WindDirection10m         int     `json:"wind_direction_10m"`
			IsDay                    int     `json:"is_day"`
			PrecipitationProbability int     `json:"precipitation_probability"`
		}{Time: "2026-07-19T18:30"},
		Hourly: &struct {
			Time                     []string   `json:"time"`
			Temperature2m            []float64  `json:"temperature_2m"`
			WeatherCode              []int      `json:"weather_code"`
			WindSpeed10m             []float64  `json:"wind_speed_10m"`
			WindGusts10m             []float64  `json:"wind_gusts_10m"`
			WindDirection10m         []int      `json:"wind_direction_10m"`
			PrecipitationProbability []int      `json:"precipitation_probability"`
			Precipitation            []float64  `json:"precipitation"`
			UVIndex                  []float64  `json:"uv_index"`
			IsDay                    []int      `json:"is_day"`
			RelativeHumidity2m       []*float64 `json:"relative_humidity_2m"`
			Visibility               []*float64 `json:"visibility"`
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
			RelativeHumidity2m:       []*float64{&humidity[0], &humidity[1]},
			Visibility:               []*float64{&visibility[0], &visibility[1]},
		},
	}

	out, err := parseOpenMeteoForecast(resp)
	if err != nil {
		t.Fatalf("parseOpenMeteoForecast failed: %v", err)
	}
	if len(out.Hourly) != 2 {
		t.Fatalf("expected 2 hourly entries, got %d", len(out.Hourly))
	}
	if out.Hourly[0].HumidityPct == nil || *out.Hourly[0].HumidityPct != 55.0 {
		t.Errorf("hourly[0].humidity_pct = %v, want 55.0", out.Hourly[0].HumidityPct)
	}
	if out.Hourly[0].VisibilityM == nil || *out.Hourly[0].VisibilityM != 24140.0 {
		t.Errorf("hourly[0].visibility_m = %v, want 24140.0", out.Hourly[0].VisibilityM)
	}
	if out.Hourly[1].HumidityPct == nil || *out.Hourly[1].HumidityPct != 62.0 {
		t.Errorf("hourly[1].humidity_pct = %v, want 62.0", out.Hourly[1].HumidityPct)
	}
}

// A live 16-day Sydney capture (see this package's testdata) showed
// visibility going null for the last several hours of the window while
// relative_humidity_2m stayed populated - a real null entry within an
// otherwise-full-length array. json.Unmarshal decodes a JSON null into a nil
// *float64 element, which must reach the wire contract as nil, not a
// fabricated 0.0.
func TestParseOpenMeteoForecast_NullVisibilityEntryBecomesNilNotZero(t *testing.T) {
	humidity := 70.0
	resp := &openMeteoResponse{
		Current: &struct {
			Time                     string  `json:"time"`
			Temperature2m            float64 `json:"temperature_2m"`
			WeatherCode              int     `json:"weather_code"`
			WindSpeed10m             float64 `json:"wind_speed_10m"`
			WindGusts10m             float64 `json:"wind_gusts_10m"`
			WindDirection10m         int     `json:"wind_direction_10m"`
			IsDay                    int     `json:"is_day"`
			PrecipitationProbability int     `json:"precipitation_probability"`
		}{Time: "2026-07-19T18:30"},
		Hourly: &struct {
			Time                     []string   `json:"time"`
			Temperature2m            []float64  `json:"temperature_2m"`
			WeatherCode              []int      `json:"weather_code"`
			WindSpeed10m             []float64  `json:"wind_speed_10m"`
			WindGusts10m             []float64  `json:"wind_gusts_10m"`
			WindDirection10m         []int      `json:"wind_direction_10m"`
			PrecipitationProbability []int      `json:"precipitation_probability"`
			Precipitation            []float64  `json:"precipitation"`
			UVIndex                  []float64  `json:"uv_index"`
			IsDay                    []int      `json:"is_day"`
			RelativeHumidity2m       []*float64 `json:"relative_humidity_2m"`
			Visibility               []*float64 `json:"visibility"`
		}{
			Time:                     []string{"2026-07-19T17:00"},
			Temperature2m:            []float64{15.2},
			WeatherCode:              []int{2},
			WindSpeed10m:             []float64{5.0},
			WindGusts10m:             []float64{11.0},
			WindDirection10m:         []int{175},
			PrecipitationProbability: []int{20},
			Precipitation:            []float64{0.0},
			UVIndex:                  []float64{0.0},
			IsDay:                    []int{0},
			RelativeHumidity2m:       []*float64{&humidity},
			Visibility:               []*float64{nil}, // real JSON null, not a shorter array
		},
	}

	out, err := parseOpenMeteoForecast(resp)
	if err != nil {
		t.Fatalf("parseOpenMeteoForecast failed: %v", err)
	}
	if out.Hourly[0].VisibilityM != nil {
		t.Fatalf("expected a JSON-null visibility entry to map to nil, got %v", *out.Hourly[0].VisibilityM)
	}
	if out.Hourly[0].HumidityPct == nil || *out.Hourly[0].HumidityPct != 70.0 {
		t.Fatalf("expected the populated humidity entry to pass through, got %v", out.Hourly[0].HumidityPct)
	}
}

// A live 16-day Sydney response can also return a visibility array SHORTER
// than the time array outright (see this file's sibling test's doc comment
// and the plan risk notes) - not just individual nulls within a full-length
// array. Indexing past the end of a shorter slice must degrade to nil, never
// panic.
func TestParseOpenMeteoForecast_GuardsVisibilityArrayShorterThanTime(t *testing.T) {
	humidity := []float64{70.0, 71.0, 72.0}
	visibility := 24140.0
	resp := &openMeteoResponse{
		Current: &struct {
			Time                     string  `json:"time"`
			Temperature2m            float64 `json:"temperature_2m"`
			WeatherCode              int     `json:"weather_code"`
			WindSpeed10m             float64 `json:"wind_speed_10m"`
			WindGusts10m             float64 `json:"wind_gusts_10m"`
			WindDirection10m         int     `json:"wind_direction_10m"`
			IsDay                    int     `json:"is_day"`
			PrecipitationProbability int     `json:"precipitation_probability"`
		}{Time: "2026-07-19T18:30"},
		Hourly: &struct {
			Time                     []string   `json:"time"`
			Temperature2m            []float64  `json:"temperature_2m"`
			WeatherCode              []int      `json:"weather_code"`
			WindSpeed10m             []float64  `json:"wind_speed_10m"`
			WindGusts10m             []float64  `json:"wind_gusts_10m"`
			WindDirection10m         []int      `json:"wind_direction_10m"`
			PrecipitationProbability []int      `json:"precipitation_probability"`
			Precipitation            []float64  `json:"precipitation"`
			UVIndex                  []float64  `json:"uv_index"`
			IsDay                    []int      `json:"is_day"`
			RelativeHumidity2m       []*float64 `json:"relative_humidity_2m"`
			Visibility               []*float64 `json:"visibility"`
		}{
			Time:                     []string{"2026-07-19T15:00", "2026-07-19T16:00", "2026-07-19T17:00"},
			Temperature2m:            []float64{15.0, 15.1, 15.2},
			WeatherCode:              []int{2, 2, 2},
			WindSpeed10m:             []float64{5.0, 5.0, 5.0},
			WindGusts10m:             []float64{11.0, 11.0, 11.0},
			WindDirection10m:         []int{175, 175, 175},
			PrecipitationProbability: []int{20, 20, 20},
			Precipitation:            []float64{0.0, 0.0, 0.0},
			UVIndex:                  []float64{0.0, 0.0, 0.0},
			IsDay:                    []int{0, 0, 0},
			RelativeHumidity2m:       []*float64{&humidity[0], &humidity[1], &humidity[2]},
			Visibility:               []*float64{&visibility}, // only 1 entry for 3 hours
		},
	}

	out, err := parseOpenMeteoForecast(resp)
	if err != nil {
		t.Fatalf("parseOpenMeteoForecast failed: %v", err)
	}
	if len(out.Hourly) != 3 {
		t.Fatalf("expected 3 hourly entries, got %d", len(out.Hourly))
	}
	if out.Hourly[0].VisibilityM == nil || *out.Hourly[0].VisibilityM != 24140.0 {
		t.Fatalf("expected hourly[0] visibility to pass through, got %v", out.Hourly[0].VisibilityM)
	}
	if out.Hourly[1].VisibilityM != nil {
		t.Fatalf("expected hourly[1] (past the end of the shorter visibility array) to be nil, not zero, got %v", *out.Hourly[1].VisibilityM)
	}
	if out.Hourly[2].VisibilityM != nil {
		t.Fatalf("expected hourly[2] (past the end of the shorter visibility array) to be nil, not zero, got %v", *out.Hourly[2].VisibilityM)
	}
	if out.Hourly[2].HumidityPct == nil || *out.Hourly[2].HumidityPct != 72.0 {
		t.Fatalf("expected humidity (full-length array) to still populate hourly[2], got %v", out.Hourly[2].HumidityPct)
	}
}

// --- minutely_15 (nowcast) ---

func minimalCurrentForNextHourTests() *struct {
	Time                     string  `json:"time"`
	Temperature2m            float64 `json:"temperature_2m"`
	WeatherCode              int     `json:"weather_code"`
	WindSpeed10m             float64 `json:"wind_speed_10m"`
	WindGusts10m             float64 `json:"wind_gusts_10m"`
	WindDirection10m         int     `json:"wind_direction_10m"`
	IsDay                    int     `json:"is_day"`
	PrecipitationProbability int     `json:"precipitation_probability"`
} {
	return &struct {
		Time                     string  `json:"time"`
		Temperature2m            float64 `json:"temperature_2m"`
		WeatherCode              int     `json:"weather_code"`
		WindSpeed10m             float64 `json:"wind_speed_10m"`
		WindGusts10m             float64 `json:"wind_gusts_10m"`
		WindDirection10m         int     `json:"wind_direction_10m"`
		IsDay                    int     `json:"is_day"`
		PrecipitationProbability int     `json:"precipitation_probability"`
	}{Time: "2026-07-19T18:30"}
}

// TestParseOpenMeteoForecast_MapsMinutely15IntoNextHour pins the happy path:
// each minutely_15 point's local time is converted to RFC3339 UTC (same
// parseOpenMeteoLocalTime as every other timestamp in this plugin) and then
// shifted BACK by one 15-minute step (Open-Meteo documents precipitation as
// a "preceding 15 minutes sum" - see open-meteo.go's doc comment on this
// mapping), precipitation_probability (a 0-100 percentage on the wire,
// though not independently documented at this resolution - see README.md)
// passes straight through, and precipitation (mm per 15 minutes) is
// converted to mm/h by multiplying by 4.
func TestParseOpenMeteoForecast_MapsMinutely15IntoNextHour(t *testing.T) {
	resp := &openMeteoResponse{
		UTCOffsetSeconds: 36000, // Sydney: UTC+10:00
		Current:          minimalCurrentForNextHourTests(),
		Minutely15: &struct {
			Time                     []string  `json:"time"`
			Precipitation            []float64 `json:"precipitation"`
			PrecipitationProbability []int     `json:"precipitation_probability"`
		}{
			Time:                     []string{"2026-07-19T18:15", "2026-07-19T18:30", "2026-07-19T18:45"},
			Precipitation:            []float64{0.0, 0.4, 0.6},
			PrecipitationProbability: []int{18, 36, 51},
		},
	}

	out, err := parseOpenMeteoForecast(resp)
	if err != nil {
		t.Fatalf("parseOpenMeteoForecast failed: %v", err)
	}
	if len(out.NextHour) != 3 {
		t.Fatalf("expected 3 next_hour points, got %d", len(out.NextHour))
	}
	// 2026-07-19T18:15 Sydney (+10:00) = 2026-07-19T08:15 UTC, shifted back
	// 15 minutes (preceding-window fix) = 2026-07-19T08:00 UTC.
	if out.NextHour[0].Time != "2026-07-19T08:00:00Z" {
		t.Errorf("expected next_hour[0].time=2026-07-19T08:00:00Z, got %q", out.NextHour[0].Time)
	}
	if out.NextHour[0].PrecipitationChancePct != 18 {
		t.Errorf("expected next_hour[0].precipitation_chance_pct=18, got %v", out.NextHour[0].PrecipitationChancePct)
	}
	if out.NextHour[0].PrecipitationMMPerH != 0 {
		t.Errorf("expected next_hour[0].precipitation_mm_per_h=0, got %v", out.NextHour[0].PrecipitationMMPerH)
	}
	// 0.4mm/15min * 4 = 1.6mm/h
	if out.NextHour[1].PrecipitationMMPerH != 1.6 {
		t.Errorf("expected next_hour[1].precipitation_mm_per_h=1.6, got %v", out.NextHour[1].PrecipitationMMPerH)
	}
	if out.NextHour[1].PrecipitationChancePct != 36 {
		t.Errorf("expected next_hour[1].precipitation_chance_pct=36, got %v", out.NextHour[1].PrecipitationChancePct)
	}
	// 0.6mm/15min * 4 = 2.4mm/h
	if out.NextHour[2].PrecipitationMMPerH != 2.4 {
		t.Errorf("expected next_hour[2].precipitation_mm_per_h=2.4, got %v", out.NextHour[2].PrecipitationMMPerH)
	}
	// resp.Latitude/Longitude are the zero value (0,0) in this synthetic
	// fixture - well outside both native-resolution boxes, so this must
	// read "hourly", not "nowcast".
	if out.NextHourSource != "hourly" {
		t.Errorf("expected next_hour_source=hourly for a (0,0) position, got %q", out.NextHourSource)
	}
}

// TestParseOpenMeteoForecast_NoMinutely15YieldsEmptyNextHour covers a
// request/response with no minutely_15 block at all (e.g. an older cached
// response, or a position where the underlying model has no 15-minute
// resolution) - AGENTS.md's fallback policy: no nowcast data means an
// empty next_hour, never a fabricated dry window.
func TestParseOpenMeteoForecast_NoMinutely15YieldsEmptyNextHour(t *testing.T) {
	resp := &openMeteoResponse{
		Current: minimalCurrentForNextHourTests(),
	}

	out, err := parseOpenMeteoForecast(resp)
	if err != nil {
		t.Fatalf("parseOpenMeteoForecast failed: %v", err)
	}
	if len(out.NextHour) != 0 {
		t.Fatalf("expected no next_hour points when minutely_15 is absent, got %d", len(out.NextHour))
	}
}

// TestParseOpenMeteoForecast_RealFixture_MackayDry replays a live capture
// (lat -18.65, lon 146.48 - the position this plugin's task brief asked to
// confirm minutely_15 against) taken 2026-09-23: dry at the time, so this
// pins the "coverage exists but reads dry" shape - 8 points (the
// forecast_minutely_15=8 window), all zero mm/h, and a genuine
// precipitation_probability around 80-84%.
func TestParseOpenMeteoForecast_RealFixture_MackayDry(t *testing.T) {
	body, err := os.ReadFile("testdata/open_meteo_response_minutely15_mackay.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	var resp openMeteoResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to unmarshal fixture: %v", err)
	}

	out, err := parseOpenMeteoForecast(&resp)
	if err != nil {
		t.Fatalf("parseOpenMeteoForecast failed: %v", err)
	}
	if len(out.NextHour) != 8 {
		t.Fatalf("expected 8 next_hour points from the live fixture, got %d", len(out.NextHour))
	}
	for i, p := range out.NextHour {
		if p.PrecipitationMMPerH != 0 {
			t.Errorf("next_hour[%d]: expected 0 mm/h in this dry capture, got %v", i, p.PrecipitationMMPerH)
		}
		if p.PrecipitationChancePct < 70 || p.PrecipitationChancePct > 90 {
			t.Errorf("next_hour[%d]: expected a real chance_pct in the 70-90 range from this capture, got %v", i, p.PrecipitationChancePct)
		}
	}
	// Mackay (lat -18.65, lon 146.48) is outside both native-resolution
	// regions (NOAA HRRR / DWD ICON-D2 + AROME) - this fixture's own
	// minutely_15.precipitation_probability values (stepping 84, 83, 83...
	// between the surrounding hourly readings) are exactly what motivated
	// this field in the first place (see README.md).
	if out.NextHourSource != "hourly" {
		t.Errorf("expected next_hour_source=hourly for the Mackay fixture, got %q", out.NextHourSource)
	}
}

// TestParseOpenMeteoForecast_RealFixtureRepositioned_NorthAmericaIsNowcast
// and its Central Europe sibling below reuse the real Mackay fixture's
// minutely_15 payload (a genuine, unedited API response shape) but override
// just its echoed latitude/longitude - next_hour_source classification is
// purely geometric (nextHourSourceForPosition), so this validates the
// region gate itself without needing a live capture from either region.
func TestParseOpenMeteoForecast_RealFixtureRepositioned_NorthAmericaIsNowcast(t *testing.T) {
	body, err := os.ReadFile("testdata/open_meteo_response_minutely15_mackay.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	var resp openMeteoResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to unmarshal fixture: %v", err)
	}
	// Denver, CO - well inside the documented NOAA HRRR CONUS grid.
	resp.Latitude = 39.7392
	resp.Longitude = -104.9903

	out, err := parseOpenMeteoForecast(&resp)
	if err != nil {
		t.Fatalf("parseOpenMeteoForecast failed: %v", err)
	}
	if out.NextHourSource != "nowcast" {
		t.Errorf("expected next_hour_source=nowcast for a Denver, CO position, got %q", out.NextHourSource)
	}
}

func TestParseOpenMeteoForecast_RealFixtureRepositioned_CentralEuropeIsNowcast(t *testing.T) {
	body, err := os.ReadFile("testdata/open_meteo_response_minutely15_mackay.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	var resp openMeteoResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to unmarshal fixture: %v", err)
	}
	// Frankfurt, Germany - well inside the documented DWD ICON-D2 grid.
	resp.Latitude = 50.1109
	resp.Longitude = 8.6821

	out, err := parseOpenMeteoForecast(&resp)
	if err != nil {
		t.Fatalf("parseOpenMeteoForecast failed: %v", err)
	}
	if out.NextHourSource != "nowcast" {
		t.Errorf("expected next_hour_source=nowcast for a Frankfurt, Germany position, got %q", out.NextHourSource)
	}
}

// TestNextHourSourceForPosition_OutsideBothBoxesIsHourly and its two
// "just inside the edge" siblings below pin the boundary behaviour of the
// classification function directly, independent of any fixture.
func TestNextHourSourceForPosition_OutsideBothBoxesIsHourly(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lat, lon float64
	}{
		{"Mackay, Australia", -18.65, 146.48},
		{"Singapore", 1.3708, 103.8024},
		{"mid-Atlantic", 30.0, -40.0},
		{"Null Island", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextHourSourceForPosition(tc.lat, tc.lon); got != "hourly" {
				t.Errorf("expected hourly for %s, got %q", tc.name, got)
			}
		})
	}
}

func TestNextHourSourceForPosition_InsideEitherBoxIsNowcast(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lat, lon float64
	}{
		{"Chicago, IL (North America box)", 41.8781, -87.6298},
		{"Munich, Germany (Central Europe box)", 48.1351, 11.5820},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextHourSourceForPosition(tc.lat, tc.lon); got != "nowcast" {
				t.Errorf("expected nowcast for %s, got %q", tc.name, got)
			}
		})
	}
}

// TestParseOpenMeteoForecast_RealFixture_SingaporeRain replays a live
// capture (Singapore, actively raining, 2026-09-23) demonstrating a genuine
// non-zero mm/h nowcast end to end from the real API response through this
// plugin's mapping.
func TestParseOpenMeteoForecast_RealFixture_SingaporeRain(t *testing.T) {
	body, err := os.ReadFile("testdata/open_meteo_response_minutely15_singapore_rain.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	var resp openMeteoResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to unmarshal fixture: %v", err)
	}

	out, err := parseOpenMeteoForecast(&resp)
	if err != nil {
		t.Fatalf("parseOpenMeteoForecast failed: %v", err)
	}
	if len(out.NextHour) != 8 {
		t.Fatalf("expected 8 next_hour points from the live fixture, got %d", len(out.NextHour))
	}
	// Fixture's first minutely_15 precipitation reading is 0.4mm/15min.
	if out.NextHour[0].PrecipitationMMPerH != 1.6 {
		t.Errorf("expected next_hour[0].precipitation_mm_per_h=1.6 (0.4mm/15min*4), got %v", out.NextHour[0].PrecipitationMMPerH)
	}
	if out.NextHour[0].PrecipitationChancePct != 18 {
		t.Errorf("expected next_hour[0].precipitation_chance_pct=18, got %v", out.NextHour[0].PrecipitationChancePct)
	}
	// Fixture's chance ramps up across the window - pin the last point too.
	last := out.NextHour[len(out.NextHour)-1]
	if last.PrecipitationChancePct != 66 {
		t.Errorf("expected the fixture's last next_hour chance_pct=66, got %v", last.PrecipitationChancePct)
	}
	if last.PrecipitationMMPerH != 2.4 {
		t.Errorf("expected the fixture's last next_hour mm_per_h=2.4 (0.6mm/15min*4), got %v", last.PrecipitationMMPerH)
	}
	// Singapore (lat 1.37, lon 103.80) is outside both native-resolution
	// regions.
	if out.NextHourSource != "hourly" {
		t.Errorf("expected next_hour_source=hourly for the Singapore fixture, got %q", out.NextHourSource)
	}
}
