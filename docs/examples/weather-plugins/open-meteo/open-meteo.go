// open-meteo.go holds the parsing logic for the Open-Meteo weather-provider
// plugin, kept in a separate file from main.go deliberately: this file has no
// dependency on "github.com/extism/go-pdk", so it (and main_test.go, which
// exercises it) can be built and tested with the plain host Go toolchain
// (`go test ./...`, no TinyGo/wasm target needed) - see main.go's doc comment
// for why that split matters. main.go's //go:wasmexport functions call
// straight into these same functions; nothing here is reimplemented or
// duplicated there.
package main

import (
	"fmt"
	neturl "net/url"
	"strings"
	"time"
)

// --- Open-Meteo API response shapes ---

type openMeteoResponse struct {
	Latitude         float64 `json:"latitude"`
	Longitude        float64 `json:"longitude"`
	UTCOffsetSeconds int     `json:"utc_offset_seconds"`
	Timezone         string  `json:"timezone"`
	Current          *struct {
		Time                     string  `json:"time"`
		Temperature2m            float64 `json:"temperature_2m"`
		WeatherCode              int     `json:"weather_code"`
		WindSpeed10m             float64 `json:"wind_speed_10m"`
		WindGusts10m             float64 `json:"wind_gusts_10m"`
		WindDirection10m         int     `json:"wind_direction_10m"`
		IsDay                    int     `json:"is_day"`
		PrecipitationProbability int     `json:"precipitation_probability"`
	} `json:"current"`
	Daily *struct {
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
	} `json:"daily"`
	Hourly *struct {
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
	} `json:"hourly"`
}

// --- Helmcentral plugin contract shapes (backend/wasm_weather_provider.go) ---

type wasmFetchForecastInput struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Days int     `json:"days"`
	// Timezone is the vessel's IANA local zone, supplied by the host, that
	// Open-Meteo must roll its daily[] arrays up on. See openMeteoRequestURL.
	Timezone string `json:"timezone"`
}

// openMeteoRequestURL builds the forecast request. It lives here rather than
// in main.go so it is covered by the plain-host `go test ./...` run (main.go
// is //go:build tinygo gated) - same split rationale as the weatherkit
// plugin's weatherKitRequestURL.
//
// timezone comes from the host (fetch_forecast's "timezone" input, from
// vesselLocalTimezoneName) instead of the previous `timezone=auto`. The host
// buckets and labels its own day series on vesselLocalLocation's
// longitude-derived fixed offset; letting Open-Meteo independently pick the
// civil IANA zone puts the daily summary and the hourly series displayed
// beside it on different windows wherever the two disagree. See
// docs/adr/0035-weather-local-day-boundaries.md.
//
// days is clamped to Open-Meteo's documented 1-16 range, with 0/negative
// meaning "unset" and taking a 7-day default.
//
// An absent timezone is rejected by validateFetchForecastInput before this
// is reached rather than defaulted here - silently defaulting is what
// produced misaligned day boundaries in the first place.
func openMeteoRequestURL(input wasmFetchForecastInput) string {
	days := input.Days
	if days <= 0 {
		days = 7
	} else if days > 16 {
		days = 16
	}

	return fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f"+
			"&current=temperature_2m,weather_code,wind_speed_10m,wind_gusts_10m,wind_direction_10m,is_day,precipitation_probability"+
			"&hourly=temperature_2m,weather_code,wind_speed_10m,wind_gusts_10m,wind_direction_10m,precipitation_probability,precipitation,uv_index,is_day"+
			"&daily=weather_code,temperature_2m_max,temperature_2m_min,wind_speed_10m_max,wind_gusts_10m_max,wind_direction_10m_dominant,precipitation_probability_max,sunrise,sunset"+
			"&wind_speed_unit=ms&timezone=%s&forecast_days=%d",
		input.Lat, input.Lon, neturl.QueryEscape(strings.TrimSpace(input.Timezone)), days,
	)
}

// validateFetchForecastInput rejects an input the host should never send.
// Mirrors the weatherkit plugin's identically-named check - see AGENTS.md's
// fallback policy on why this fails loudly instead of defaulting.
func validateFetchForecastInput(input wasmFetchForecastInput) error {
	if strings.TrimSpace(input.Timezone) == "" {
		return fmt.Errorf("open-meteo: fetch_forecast input missing required \"timezone\" (host must supply the vessel's IANA local zone)")
	}
	return nil
}

type wasmWeatherCurrentOutput struct {
	Time                   string  `json:"time"`
	TemperatureC           float64 `json:"temperature_c"`
	Condition              string  `json:"condition"`
	WindSpeedMS            float64 `json:"wind_speed_ms"`
	WindGustMS             float64 `json:"wind_gust_ms"`
	WindDirectionDeg       float64 `json:"wind_direction_deg"`
	PrecipitationChancePct float64 `json:"precipitation_chance_pct"`
}

type wasmWeatherDayOutput struct {
	Start                  string  `json:"start"`
	Condition              string  `json:"condition"`
	TempMaxC               float64 `json:"temp_max_c"`
	TempMinC               float64 `json:"temp_min_c"`
	WindSpeedMS            float64 `json:"wind_speed_ms"`
	WindGustMS             float64 `json:"wind_gust_ms"`
	WindDirectionDeg       float64 `json:"wind_direction_deg"`
	PrecipitationChancePct float64 `json:"precipitation_chance_pct"`
	Sunrise                string  `json:"sunrise"`
	Sunset                 string  `json:"sunset"`
}

type wasmWeatherHourOutput struct {
	Time                   string  `json:"time"`
	TemperatureC           float64 `json:"temperature_c"`
	Condition              string  `json:"condition"`
	WindSpeedMS            float64 `json:"wind_speed_ms"`
	WindGustMS             float64 `json:"wind_gust_ms"`
	WindDirectionDeg       float64 `json:"wind_direction_deg"`
	PrecipitationChancePct float64 `json:"precipitation_chance_pct"`
	PrecipitationMM        float64 `json:"precipitation_mm"`
	UVIndex                float64 `json:"uv_index"`
	IsDaylight             bool    `json:"is_daylight"`
}

type wasmFetchForecastOutput struct {
	Current wasmWeatherCurrentOutput `json:"current"`
	Days    []wasmWeatherDayOutput   `json:"days"`
	Hourly  []wasmWeatherHourOutput  `json:"hourly"`
}

// wmoCodeToCondition maps WMO weather codes to Helmcentral's canonical
// condition vocabulary. This is a deliberate reference-plugin simplification:
// the vocabulary doesn't distinguish all WMO subtypes (e.g. snow grains vs.
// other snow, heavy vs. moderate rain), and any code Open-Meteo might add in
// the future that isn't in this table falls back to "cloudy" rather than
// erroring. This is fine for a teaching example, not a claim of full WMO
// coverage. See weather_providers.go's doc comment for the full vocabulary.
func wmoCodeToCondition(code int) string {
	switch code {
	case 0:
		return "clear"
	case 1:
		return "mostlyclear"
	case 2:
		return "partlycloudy"
	case 3:
		return "cloudy"
	case 45, 48:
		return "foggy"
	case 51, 53, 55:
		return "drizzle"
	case 56, 57:
		return "freezingdrizzle"
	case 61, 63:
		return "rain"
	case 65:
		return "heavyrain"
	case 66, 67:
		return "freezingrain"
	case 71, 73, 77:
		return "snow"
	case 75:
		return "heavysnow"
	case 80, 81:
		return "rain"
	case 82:
		return "heavyrain"
	case 85:
		return "snow"
	case 86:
		return "heavysnow"
	case 95, 96, 99:
		return "thunderstorms"
	default:
		// Fall back to "cloudy" for unknown codes - a safe, non-alarming default
		return "cloudy"
	}
}

// parseOpenMeteoLocalTime converts Open-Meteo's naive local-time strings
// (and associated UTC offset) to RFC3339 UTC timestamps. Open-Meteo returns
// times like "2026-07-19T18:30" (local midnight is midnight local, not UTC).
// We parse the naive string into a time.Time whose wall-clock fields match
// the local time but whose Location is UTC (effectively treating the parsed
// value as UTC), then add the offset to get the true UTC instant.
//
// Example: Open-Meteo returns {"time":"2026-07-19T18:30", "utc_offset_seconds":36000}
// for Sydney (UTC+10:00). The naive parse yields a time whose wall reads
// 2026-07-19 18:30 in UTC. The true UTC instant is 10 hours earlier:
// 2026-07-19 08:30 UTC. We compute that by subtracting the offset from the
// naive time.
//
// offsetSeconds: the utc_offset_seconds field from the response (int, seconds,
// e.g. 36000 for +10:00, -18000 for -05:00).
func parseOpenMeteoLocalTime(localStr string, layout string, offsetSeconds int) (time.Time, error) {
	// Parse the naive local time string (no timezone info)
	parsed, err := time.Parse(layout, localStr)
	if err != nil {
		return time.Time{}, err
	}

	// parsed's wall-clock reads the local time, but its Location is UTC.
	// The true UTC instant is: local_time - offset_from_utc
	// offset_from_utc = utc_offset_seconds
	// So UTC instant = parsed - Duration(offsetSeconds)
	trueUTC := parsed.Add(-time.Duration(offsetSeconds) * time.Second)

	return trueUTC, nil
}

// parseOpenMeteoForecast converts a raw Open-Meteo response into Helmcentral's
// plugin output contract. It handles all timestamp conversions, condition
// mappings, and data validation.
func parseOpenMeteoForecast(resp *openMeteoResponse) (wasmFetchForecastOutput, error) {
	var out wasmFetchForecastOutput

	// Parse current conditions
	if resp.Current == nil {
		return out, fmt.Errorf("missing current conditions in Open-Meteo response")
	}

	currentTime, err := parseOpenMeteoLocalTime(resp.Current.Time, "2006-01-02T15:04", resp.UTCOffsetSeconds)
	if err != nil {
		return out, fmt.Errorf("failed to parse current.time: %w", err)
	}

	out.Current = wasmWeatherCurrentOutput{
		Time:                   currentTime.UTC().Format(time.RFC3339),
		TemperatureC:           resp.Current.Temperature2m,
		Condition:              wmoCodeToCondition(resp.Current.WeatherCode),
		WindSpeedMS:            resp.Current.WindSpeed10m,
		WindGustMS:             resp.Current.WindGusts10m,
		WindDirectionDeg:       float64(resp.Current.WindDirection10m),
		PrecipitationChancePct: float64(resp.Current.PrecipitationProbability),
	}

	// Parse daily data
	if resp.Daily != nil && len(resp.Daily.Time) > 0 {
		out.Days = make([]wasmWeatherDayOutput, 0, len(resp.Daily.Time))
		for i := 0; i < len(resp.Daily.Time); i++ {
			// Daily times are date-only ("2026-07-19"), representing local midnight
			dayStart, err := parseOpenMeteoLocalTime(resp.Daily.Time[i], "2006-01-02", resp.UTCOffsetSeconds)
			if err != nil {
				return out, fmt.Errorf("failed to parse daily.time[%d]: %w", i, err)
			}

			sunrise, err := parseOpenMeteoLocalTime(resp.Daily.Sunrise[i], "2006-01-02T15:04", resp.UTCOffsetSeconds)
			if err != nil {
				return out, fmt.Errorf("failed to parse daily.sunrise[%d]: %w", i, err)
			}

			sunset, err := parseOpenMeteoLocalTime(resp.Daily.Sunset[i], "2006-01-02T15:04", resp.UTCOffsetSeconds)
			if err != nil {
				return out, fmt.Errorf("failed to parse daily.sunset[%d]: %w", i, err)
			}

			day := wasmWeatherDayOutput{
				Start:                  dayStart.UTC().Format(time.RFC3339),
				Condition:              wmoCodeToCondition(resp.Daily.WeatherCode[i]),
				TempMaxC:               resp.Daily.Temperature2mMax[i],
				TempMinC:               resp.Daily.Temperature2mMin[i],
				WindSpeedMS:            resp.Daily.WindSpeed10mMax[i],
				WindGustMS:             resp.Daily.WindGusts10mMax[i],
				WindDirectionDeg:       float64(resp.Daily.WindDirection10mDominant[i]),
				PrecipitationChancePct: float64(resp.Daily.PrecipitationProbabilityMax[i]),
				Sunrise:                sunrise.UTC().Format(time.RFC3339),
				Sunset:                 sunset.UTC().Format(time.RFC3339),
			}
			out.Days = append(out.Days, day)
		}
	}

	// Parse hourly data
	if resp.Hourly != nil && len(resp.Hourly.Time) > 0 {
		out.Hourly = make([]wasmWeatherHourOutput, 0, len(resp.Hourly.Time))
		for i := 0; i < len(resp.Hourly.Time); i++ {
			hourTime, err := parseOpenMeteoLocalTime(resp.Hourly.Time[i], "2006-01-02T15:04", resp.UTCOffsetSeconds)
			if err != nil {
				return out, fmt.Errorf("failed to parse hourly.time[%d]: %w", i, err)
			}

			isDaylight := resp.Hourly.IsDay[i] == 1

			hour := wasmWeatherHourOutput{
				Time:                   hourTime.UTC().Format(time.RFC3339),
				TemperatureC:           resp.Hourly.Temperature2m[i],
				Condition:              wmoCodeToCondition(resp.Hourly.WeatherCode[i]),
				WindSpeedMS:            resp.Hourly.WindSpeed10m[i],
				WindGustMS:             resp.Hourly.WindGusts10m[i],
				WindDirectionDeg:       float64(resp.Hourly.WindDirection10m[i]),
				PrecipitationChancePct: float64(resp.Hourly.PrecipitationProbability[i]),
				PrecipitationMM:        resp.Hourly.Precipitation[i],
				UVIndex:                resp.Hourly.UVIndex[i],
				IsDaylight:             isDaylight,
			}
			out.Hourly = append(out.Hourly, hour)
		}
	}

	return out, nil
}
