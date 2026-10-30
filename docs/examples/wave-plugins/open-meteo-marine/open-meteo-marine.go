// open-meteo-marine.go holds the parsing and HTTP logic for the Open-Meteo
// Marine wave-provider plugin, kept in a separate file from main.go
// deliberately: this file has no dependency on "github.com/extism/go-pdk",
// so it (and main_test.go, which exercises it) can be built and tested with
// the plain host Go toolchain (`go test ./...`, no TinyGo/wasm target
// needed) - see main.go's doc comment for why that split matters. main.go's
// //go:wasmexport functions call straight into these same functions; nothing
// here is reimplemented or duplicated there.
package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	// Marine wave data endpoint - pinned to NOAA's GFS-Wave (WaveWatch III)
	// model (modelGFSWave, appended by waveURL) so swell height, period and
	// direction line up with other WaveWatch III-based swell forecasts for a
	// given coastline; Open-Meteo's default "best_match" model blend runs
	// noticeably lower/different here.
	marineWaveURLFmt = "https://marine-api.open-meteo.com/v1/marine?latitude=%.4f&longitude=%.4f&hourly=wave_height,wave_direction,wave_period,wind_wave_height,wind_wave_direction,wind_wave_period,swell_wave_height,swell_wave_direction,swell_wave_period&timezone=auto&forecast_days=%d"

	// modelGFSWave is the model the wave fetch is pinned to. Its 0.25 degree
	// grid has land-masked cells, and over one Open-Meteo returns every wave
	// field as 0, not null.
	modelGFSWave = "ncep_gfswave025"

	// modelBlend names Open-Meteo's default model blend (no models= param),
	// which has a finer grid and real values over cells GFS-Wave masks.
	modelBlend = "best_match"

	// Marine sea temperature endpoint - Deliberately does not pin a models=
	// param (unlike fetchWaveData) - the wave-specific NOAA GFS-Wave model
	// that wave data is pinned to does not carry sea surface temperature at
	// all (returns null/"undefined" units); only Open-Meteo's default model
	// blend does.
	marineSeaTempURLFmt = "https://marine-api.open-meteo.com/v1/marine?latitude=%.4f&longitude=%.4f&current=sea_surface_temperature"

	// defaultForecastDays matches the original ported code's hardcoded window.
	defaultForecastDays = 10

	// maxForecastDays is Open-Meteo Marine API's documented maximum.
	maxForecastDays = 16
)

// waveURL builds the wave-data request. fallback leaves out models= so
// Open-Meteo answers with its default blend.
func waveURL(lat, lon float64, days int, fallback bool) string {
	u := fmt.Sprintf(marineWaveURLFmt, lat, lon, days)
	if !fallback {
		u += "&models=" + modelGFSWave
	}
	return u
}

// fallbackEnabled reads the use_blend_fallback config value. Anything but
// "true" is off.
func fallbackEnabled(v string) bool {
	return strings.EqualFold(strings.TrimSpace(v), "true")
}

// parseWaveBody takes the UTC offset from a wave response and parses its
// hourly series.
func parseWaveBody(body []byte) ([]waveHourOutput, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse wave response: %w", err)
	}
	offset := 0
	if o, ok := raw["utc_offset_seconds"].(float64); ok {
		offset = int(o)
	}
	return parseOpenMeteoWaveResponse(body, offset)
}

// hoursHaveNoWaves reports a series with no real wave reading: every hour's
// period is missing or 0. A land-masked GFS-Wave cell returns zeros for every
// field, and a wave with no period is not a wave.
func hoursHaveNoWaves(hours []waveHourOutput) bool {
	for _, h := range hours {
		if h.WavePeriodS != nil && *h.WavePeriodS > 0 {
			return false
		}
	}
	return true
}

// chooseWaveSeries keeps the GFS-Wave series unless it holds no waves and the
// operator turned the fallback on, in which case fetchBlend supplies the
// default-blend series. A failed blend fetch is an error: it does not drop
// back to the land cell as though the model had answered.
func chooseWaveSeries(primary []waveHourOutput, fallback bool, fetchBlend func() ([]waveHourOutput, error)) ([]waveHourOutput, string, bool, error) {
	if !fallback || !hoursHaveNoWaves(primary) {
		return primary, modelGFSWave, false, nil
	}
	blend, err := fetchBlend()
	if err != nil {
		return nil, "", false, err
	}
	return blend, modelBlend, true, nil
}

// parseOpenMeteoWaveResponse parses the raw JSON wave-data response from
// Open-Meteo Marine API into a flat chronological list of hourly wave
// forecasts (NOT bucketed by day - bucketing is host-side now). All times
// are converted from naive local-time strings (via utc_offset_seconds) to
// proper RFC3339 format per the plugin contract.
//
// IMPORTANT: Open-Meteo returns naive local time in "2006-01-02T15:04"
// format (e.g. "2026-07-19T00:00"), without UTC offset. The response's
// top-level utc_offset_seconds field (e.g. 36000 for +10:00, can be negative)
// tells us how many seconds ahead of UTC the local time is. To get the true
// UTC instant: parse the naive string with time.Parse("2006-01-02T15:04", s),
// which yields a time.Time whose wall-clock fields equal local time but whose
// Location is UTC; since local = UTC + offset, true UTC instant =
// parsed.Add(-time.Duration(utcOffsetSeconds) * time.Second). This is a
// deliberate correctness improvement over the original host code
// (weather_tide.go's parseOpenMeteoMarineResponse), which parsed the naive
// time AS-IS without offset correction - that worked there only because the
// old code bucketed by local calendar date, so the incorrect absolute instant
// didn't matter for its purposes. This plugin's contract requires correct
// RFC3339 instants, so we cannot repeat that omission.
//
// If the hourly object is missing from the response, this is a hard error
// (wave data is required). If hourly.time is missing or the arrays are
// misaligned, it's also a hard error. Null (or non-numeric) entries in the
// combined height, period and direction arrays, which Open-Meteo returns when
// queried over land with no wave model coverage, leave that figure out of the
// hour so the host reads it as absent. A reported 0 is a real figure and is
// kept. The per-component fields keep their zero encoding, which the host
// reads as "no such wave train".
func parseOpenMeteoWaveResponse(raw []byte, utcOffsetSeconds int) ([]waveHourOutput, error) {
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("failed to parse marine wave response: %w", err)
	}

	hourly, ok := result["hourly"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("hourly missing from marine wave response")
	}

	times, ok := hourly["time"].([]any)
	if !ok {
		return nil, fmt.Errorf("hourly.time missing from marine wave response")
	}

	heights, _ := hourly["wave_height"].([]any)
	periods, _ := hourly["wave_period"].([]any)
	directions, _ := hourly["wave_direction"].([]any)
	windWaveHeights, _ := hourly["wind_wave_height"].([]any)
	swellWaveHeights, _ := hourly["swell_wave_height"].([]any)

	// Per-component direction and period, so the host can tell a cross sea
	// from one system. Open-Meteo reports all three of a flat component's
	// fields as 0 rather than null, so a zero period is what marks a
	// component as absent - the host relies on that.
	windWaveDirections, _ := hourly["wind_wave_direction"].([]any)
	windWavePeriods, _ := hourly["wind_wave_period"].([]any)
	swellWaveDirections, _ := hourly["swell_wave_direction"].([]any)
	swellWavePeriods, _ := hourly["swell_wave_period"].([]any)

	hourly_out := make([]waveHourOutput, 0, len(times))
	for i, rawTime := range times {
		timeStr, ok := rawTime.(string)
		if !ok {
			continue
		}

		rfc3339Time, err := parseOpenMeteoLocalTime(timeStr, utcOffsetSeconds)
		if err != nil {
			continue
		}

		point := waveHourOutput{Time: rfc3339Time.UTC().Format(time.RFC3339)}
		if i < len(heights) {
			if h, ok := heights[i].(float64); ok {
				point.WaveHeightM = &h
			}
		}
		if i < len(periods) {
			if p, ok := periods[i].(float64); ok {
				point.WavePeriodS = &p
			}
		}
		if i < len(directions) {
			if d, ok := directions[i].(float64); ok {
				point.WaveDirectionDeg = &d
			}
		}
		if i < len(windWaveHeights) {
			if w, ok := windWaveHeights[i].(float64); ok {
				point.WindWaveHeightM = w
			}
		}
		if i < len(swellWaveHeights) {
			if s, ok := swellWaveHeights[i].(float64); ok {
				point.SwellWaveHeightM = s
			}
		}
		if i < len(windWaveDirections) {
			if d, ok := windWaveDirections[i].(float64); ok {
				point.WindWaveDirectionDeg = d
			}
		}
		if i < len(windWavePeriods) {
			if p, ok := windWavePeriods[i].(float64); ok {
				point.WindWavePeriodS = p
			}
		}
		if i < len(swellWaveDirections) {
			if d, ok := swellWaveDirections[i].(float64); ok {
				point.SwellWaveDirectionDeg = d
			}
		}
		if i < len(swellWavePeriods) {
			if p, ok := swellWavePeriods[i].(float64); ok {
				point.SwellWavePeriodS = p
			}
		}

		hourly_out = append(hourly_out, point)
	}

	return hourly_out, nil
}

// parseOpenMeteoSeaTemperatureResponse parses the raw JSON sea-temperature
// response from Open-Meteo Marine API. Returns a *float64 (nil when
// unavailable) rather than an error - sea temperature is optional per the
// plugin contract, so an absent/null value is not a failure condition.
func parseOpenMeteoSeaTemperatureResponse(raw []byte) (*float64, error) {
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("failed to parse sea temperature response: %w", err)
	}

	current, ok := result["current"].(map[string]any)
	if !ok {
		// No current object at all - treat as "no data"
		return nil, nil
	}

	// Check for sea_surface_temperature key
	tempRaw, ok := current["sea_surface_temperature"]
	if !ok {
		// Key not present - treat as "no data"
		return nil, nil
	}

	// If the key is present but null in JSON, tempRaw will be nil
	if tempRaw == nil {
		return nil, nil
	}

	temp, ok := tempRaw.(float64)
	if !ok {
		return nil, fmt.Errorf("sea_surface_temperature is not a number")
	}

	return &temp, nil
}

// parseOpenMeteoLocalTime converts an Open-Meteo naive local-time string
// (format "2006-01-02T15:04") and UTC offset (seconds) into a properly-offset
// RFC3339 time.Time in UTC. This is the corrected version - the original
// host code did NOT apply the offset, which worked only because it bucketed
// by local date (so the incorrect instant didn't matter). The plugin contract
// requires correct RFC3339 instants.
func parseOpenMeteoLocalTime(localStr string, utcOffsetSeconds int) (time.Time, error) {
	// Parse as if the string represents wall-clock local time, but in UTC location
	parsed, err := time.Parse("2006-01-02T15:04", localStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse time %q: %w", localStr, err)
	}

	// parsed is now a time.Time with wall-clock values equal to local time,
	// but Location=UTC. The actual UTC instant is:
	// utc = local - offset = parsed - offset_duration
	offset := time.Duration(utcOffsetSeconds) * time.Second
	utc := parsed.Add(-offset)

	return utc, nil
}

// waveHourOutput mirrors the host's wasmWaveHourOutput contract exactly
// (backend/wasm_wave_provider.go).
type waveHourOutput struct {
	Time string `json:"time"`

	// The combined figures are left out when the model has no value for the
	// hour. The host reads a missing key as absent, which is not 0 m, 0 s or a
	// sea from due north.
	WaveHeightM      *float64 `json:"wave_height_m,omitempty"`
	WavePeriodS      *float64 `json:"wave_period_s,omitempty"`
	WaveDirectionDeg *float64 `json:"wave_direction_deg,omitempty"`
	WindWaveHeightM  float64  `json:"wind_wave_height_m"`
	SwellWaveHeightM float64  `json:"swell_wave_height_m"`

	WindWaveDirectionDeg  float64 `json:"wind_wave_direction_deg"`
	WindWavePeriodS       float64 `json:"wind_wave_period_s"`
	SwellWaveDirectionDeg float64 `json:"swell_wave_direction_deg"`
	SwellWavePeriodS      float64 `json:"swell_wave_period_s"`
}

// fetchWavesOutput mirrors the host's wasmFetchWavesOutput contract
// (backend/wasm_wave_provider.go).
type fetchWavesOutput struct {
	Hourly                 []waveHourOutput `json:"hourly"`
	SeaSurfaceTemperatureC *float64         `json:"sea_surface_temperature_c,omitempty"`
	Model                  string           `json:"model,omitempty"`
	FallbackUsed           bool             `json:"fallback_used,omitempty"`
}

// clampForecastDays returns a clamped forecast_days value: default to 10 if
// <=0, cap at Open-Meteo's max of 16 otherwise.
func clampForecastDays(days int) int {
	if days <= 0 {
		return defaultForecastDays
	}
	if days > maxForecastDays {
		return maxForecastDays
	}
	return days
}
