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
		// RelativeHumidity2m/Visibility are POINTER slices, deliberately -
		// unlike every other hourly field above. Open-Meteo sends real JSON
		// nulls in these two (confirmed via a live 16-day capture, see this
		// package's testdata/open_meteo_response_16day_sydney.json - the
		// window's last few hours null out visibility while temperature/wind/
		// etc. stay populated the whole way). A []float64 would silently
		// decode each null to 0.0 - exactly the latent bug ADR 0035 flags as
		// an unfixed follow-up for this plugin's other []int/[]float64
		// fields. Do not add a third instance of it here.
		RelativeHumidity2m []*float64 `json:"relative_humidity_2m"`
		Visibility         []*float64 `json:"visibility"`
	} `json:"hourly"`
	// Minutely15 is Open-Meteo's 15-minute-resolution nowcast dataset,
	// mapped onto this plugin's next_hour output below. Requesting
	// `minutely_15=precipitation,precipitation_probability` never gets a 400
	// (an invalid variable name does) - but that does NOT mean both
	// variables are genuine 15-minute data everywhere. Per Open-Meteo's own
	// forecast API docs (https://open-meteo.com/en/docs, confirmed
	// 2026-09-23): "This data is based on NOAA HRRR model for North America
	// and DWD ICON-D2 and Météo-France AROME model for Central Europe. If
	// 15-minutely data is requested for other regions data is interpolated
	// from 1-hourly to 15-minutely." precipitation_probability additionally
	// is not listed in the 15-Minutely Weather Variables table AT ALL, in
	// any region - only the hourly resolution documents a
	// precipitation_probability ("Preceding hour probability"). Verified
	// live at Mackay (lat -18.65, lon 146.48, outside both native-resolution
	// regions, 2026-09-23): minutely_15.precipitation_probability stepped
	// smoothly between the surrounding hourly values (84, 82, 80 -> 84, 83,
	// 83, 82, 82, 81...), exactly the shape linear interpolation produces,
	// and minutely_15.precipitation read 0.0 at a point where the hourly
	// figure was 0.1 - both confirming this window's data was backfilled
	// from the hourly model, not independently observed/modelled at 15-
	// minute resolution. nextHourSourceForPosition below reports which case
	// applies for a given position ("nowcast" inside the two native-
	// resolution regions, "hourly" everywhere else) so the host/frontend can
	// caption it honestly instead of presenting interpolated data as a true
	// short-range nowcast (AGENTS.md fallback policy).
	// Precipitation/PrecipitationProbability are POINTER-ELEMENT slices,
	// deliberately - matching Hourly.RelativeHumidity2m/Visibility above,
	// and for the same reason: a plain []float64/[]int silently decodes a
	// real JSON null to 0.0/0, which is exactly wrong here. A null
	// precipitation_probability means "no probability at this resolution"
	// (the plugin's own existing -1 sentinel convention for that field
	// below); a null precipitation means "no reading for this point at
	// all", which is NOT the same as "0 mm/h falling" - see
	// parseOpenMeteoForecast's mapping below for how each is handled.
	Minutely15 *struct {
		Time                     []string   `json:"time"`
		Precipitation            []*float64 `json:"precipitation"`
		PrecipitationProbability []*int     `json:"precipitation_probability"`
	} `json:"minutely_15"`
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
			"&hourly=temperature_2m,weather_code,wind_speed_10m,wind_gusts_10m,wind_direction_10m,precipitation_probability,precipitation,uv_index,is_day,relative_humidity_2m,visibility"+
			"&daily=weather_code,temperature_2m_max,temperature_2m_min,wind_speed_10m_max,wind_gusts_10m_max,wind_direction_10m_dominant,precipitation_probability_max,sunrise,sunset"+
			"&minutely_15=precipitation,precipitation_probability&forecast_minutely_15=8"+
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
	// HumidityPct/VisibilityM are *float64 - see the doc comment on
	// openMeteoResponse.Hourly.RelativeHumidity2m/Visibility above. nil here
	// means "Open-Meteo had no value for this hour" (either a real JSON null
	// or an index past the end of a shorter-than-time array); the host maps
	// nil to its own -1 sentinel (sentinelHumidityPct/sentinelVisibilityNm in
	// backend/weather_providers.go), never a fabricated 0.
	HumidityPct *float64 `json:"humidity_pct"`
	VisibilityM *float64 `json:"visibility_m"`
}

// wasmWeatherNextHourOutput mirrors backend/wasm_weather_provider.go's
// wasmWeatherNextHourOutput - see that file's doc comment and
// backend/weather_providers.go's top doc comment for the full next_hour
// contract. PrecipitationChancePct keeps the same negative-is-absent
// convention as this contract's other precipitation_chance_pct fields:
// usually a plain 0-100 value straight from Open-Meteo's
// precipitation_probability, but -1 whenever that entry is a real JSON null
// (see openMeteoResponse.Minutely15's doc comment on the pointer-element
// slices this is parsed from) - whether a real (non-sentinel) value is
// genuine 15-minute data or interpolated from the hourly model is exactly
// what NextHourSource (below) reports, not something PrecipitationChancePct
// itself needs a second signal for.
type wasmWeatherNextHourOutput struct {
	Time                   string  `json:"time"`
	PrecipitationChancePct float64 `json:"precipitation_chance_pct"`
	PrecipitationMMPerH    float64 `json:"precipitation_mm_per_h"`
}

type wasmFetchForecastOutput struct {
	Current  wasmWeatherCurrentOutput    `json:"current"`
	Days     []wasmWeatherDayOutput      `json:"days"`
	Hourly   []wasmWeatherHourOutput     `json:"hourly"`
	NextHour []wasmWeatherNextHourOutput `json:"next_hour"`
	// NextHourSource is "nowcast" or "hourly" whenever NextHour is
	// non-empty, set by nextHourSourceForPosition below from the response's
	// own echoed latitude/longitude. See backend/weather_providers.go's top
	// doc comment's next_hour_source section.
	NextHourSource string `json:"next_hour_source,omitempty"`
}

// --- next_hour_source region classification ---
//
// Conservative bounding boxes for the two regions Open-Meteo documents as
// having native 15-minute-resolution minutely_15 data (see
// openMeteoResponse.Minutely15's doc comment for the exact quoted source).
// Both boxes are drawn slightly INSIDE each model's own published grid
// extent - a position near a model's edge is exactly where "nowcast" would
// be the least defensible claim, and the fallback policy's bias is toward
// the honest "hourly" label over the flattering one.
type geoBox struct{ MinLat, MaxLat, MinLon, MaxLon float64 }

func (b geoBox) contains(lat, lon float64) bool {
	return lat >= b.MinLat && lat <= b.MaxLat && lon >= b.MinLon && lon <= b.MaxLon
}

var (
	// North America / NOAA HRRR: documented CONUS grid roughly 21.1-52.6N,
	// 134.1-59.1W (registry.opendata.aws/noaa-hrrr-pds; gribstream.com/models/hrrr).
	northAmericaMinutely15Box = geoBox{MinLat: 22.0, MaxLat: 52.0, MinLon: -133.0, MaxLon: -60.0}
	// Central Europe / DWD ICON-D2 (+ Météo-France AROME): ICON-D2's
	// documented public regular-grid product spans 43.18-58.08N,
	// 3.94W-20.34E (dwd.de). AROME's own French domain is not separately
	// boxed here - Open-Meteo's docs name both models together as one
	// "Central Europe" coverage claim, and ICON-D2's grid is the one this
	// plugin has a precise documented extent for; erring toward "hourly" at
	// AROME's western fringe (which ICON-D2 doesn't reach) is the
	// conservative direction, not a gap.
	centralEuropeMinutely15Box = geoBox{MinLat: 43.5, MaxLat: 58.0, MinLon: -3.5, MaxLon: 20.0}
)

// nextHourSourceForPosition reports whether a position falls inside
// Open-Meteo's native 15-minute-resolution minutely_15 coverage ("nowcast")
// or is interpolated from the hourly model ("hourly") - see the two box
// definitions above and openMeteoResponse.Minutely15's doc comment for the
// documented source and the live Mackay verification.
func nextHourSourceForPosition(lat, lon float64) string {
	if northAmericaMinutely15Box.contains(lat, lon) || centralEuropeMinutely15Box.contains(lat, lon) {
		return "nowcast"
	}
	return "hourly"
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

			// Guarded on index bounds AND nil: a live 16-day Sydney capture
			// showed visibility both going null mid-array and, on other
			// requests, arriving as an array shorter than time outright - see
			// the doc comment on openMeteoResponse.Hourly.Visibility above.
			// Either case must degrade to nil, never a zero-value panic or a
			// fabricated 0.0.
			var humidityPct *float64
			if i < len(resp.Hourly.RelativeHumidity2m) {
				humidityPct = resp.Hourly.RelativeHumidity2m[i]
			}
			var visibilityM *float64
			if i < len(resp.Hourly.Visibility) {
				visibilityM = resp.Hourly.Visibility[i]
			}

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
				HumidityPct:            humidityPct,
				VisibilityM:            visibilityM,
			}
			out.Hourly = append(out.Hourly, hour)
		}
	}

	// Parse minutely_15 nowcast data. Nil/empty means this response has no
	// nowcast coverage (an older cached response, or a position whose
	// underlying model lacks 15-minute resolution) - out.NextHour simply
	// stays nil/empty, never backfilled from hourly data (AGENTS.md
	// fallback policy: "no nowcast" must be shown honestly).
	if resp.Minutely15 != nil && len(resp.Minutely15.Time) > 0 {
		out.NextHour = make([]wasmWeatherNextHourOutput, 0, len(resp.Minutely15.Time))
		for i := 0; i < len(resp.Minutely15.Time); i++ {
			rawPointTime, err := parseOpenMeteoLocalTime(resp.Minutely15.Time[i], "2006-01-02T15:04", resp.UTCOffsetSeconds)
			if err != nil {
				return out, fmt.Errorf("failed to parse minutely_15.time[%d]: %w", i, err)
			}
			// Open-Meteo documents minutely_15.precipitation as a "Preceding
			// 15 minutes sum" (https://open-meteo.com/en/docs, 15-Minutely
			// Weather Variables table, confirmed 2026-09-23): the value at
			// timestamp T covers [T-15min, T), not [T, T+15min). This
			// plugin's contract says the opposite - a next_hour point's
			// "time" marks the START of forward coverage (see
			// backend/weather_providers.go's next_hour doc comment) - so
			// Open-Meteo's own timestamp is one step LATE for this contract
			// and must be shifted back by one 15-minute step. Without this,
			// rain that has been falling since 12:00 (and is reported at
			// the T=12:15 point) reads as "expected in 10 minutes" at
			// 12:05 instead of "already falling".
			//
			// Open-Meteo does not separately document a convention for
			// minutely_15's precipitation_probability - it is not listed in
			// the 15-Minutely Weather Variables table at all (only the
			// hourly resolution's own precipitation_probability is
			// documented, as "Preceding hour probability"). Since this
			// contract carries one Time per point covering both fields,
			// shifting the point's Time is the only consistent treatment
			// available, and matches the "preceding window" convention
			// Open-Meteo uses everywhere else it documents one.
			pointTime := rawPointTime.Add(-15 * time.Minute)

			// A null precipitation reading means Open-Meteo has no data for
			// this point at all - not a genuine 0 mm/h. This plugin's
			// next_hour contract has no per-point "no data" flag for
			// PrecipitationMMPerH (unlike PrecipitationChancePct's own
			// negative-sentinel convention), so the only honest way to carry
			// "no data" through is to omit the point entirely - the
			// host/frontend already treat a gap between next_hour points as
			// "nothing known there", never as "confirmed dry" (see
			// lib/nowcast.ts's buildNowcastBars, which only ever draws bars
			// for the points it's actually given).
			if i >= len(resp.Minutely15.Precipitation) || resp.Minutely15.Precipitation[i] == nil {
				continue
			}
			// mm per 15 minutes -> mm/h: 4 quarter-hours per hour.
			mmPerH := *resp.Minutely15.Precipitation[i] * 4

			chancePct := -1.0
			if i < len(resp.Minutely15.PrecipitationProbability) && resp.Minutely15.PrecipitationProbability[i] != nil {
				chancePct = float64(*resp.Minutely15.PrecipitationProbability[i])
			}

			out.NextHour = append(out.NextHour, wasmWeatherNextHourOutput{
				Time:                   pointTime.UTC().Format(time.RFC3339),
				PrecipitationChancePct: chancePct,
				PrecipitationMMPerH:    mmPerH,
			})
		}
		// Only claim a source when at least one point survived the null
		// check above - a next_hour_source with zero actual points is
		// meaningless (the host doesn't look at it when next_hour is empty
		// either way, but there's no reason to set a stale/misleading value).
		if len(out.NextHour) > 0 {
			out.NextHourSource = nextHourSourceForPosition(resp.Latitude, resp.Longitude)
		}
	}

	return out, nil
}
