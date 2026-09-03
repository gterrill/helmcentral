// open-meteo-upper's parsing and URL-building logic, deliberately free of any
// dependency on github.com/extism/go-pdk so this file and its tests build and
// run under the plain host Go toolchain. See main.go's header for why.
package main

import (
	"encoding/json"
	"fmt"
	"time"
)

// upperAirURLFmt asks Open-Meteo for the 500mb level plus the 1000mb level
// the thickness is measured against.
//
// No models= param: the default blend carries 500hPa across the full 16-day
// run, verified live (383 of 384 hourly steps non-null, the one null being
// the final hour). Pinning gfs_global works too but buys nothing here.
//
// wind_speed_unit=ms applies to pressure-level winds as well as the surface
// one, also verified live, so nothing in this plugin converts wind speed.
const upperAirURLFmt = "https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f" +
	"&hourly=geopotential_height_500hPa,geopotential_height_1000hPa,wind_speed_500hPa,temperature_500hPa" +
	"&wind_speed_unit=ms&timezone=UTC&forecast_days=%d"

// upperAirHourOutput mirrors the host contract
// (backend/wasm_upper_air_provider.go).
type upperAirHourOutput struct {
	Time                    string  `json:"time"`
	GeopotentialHeight500M  float64 `json:"geopotential_height_500_m"`
	GeopotentialHeight1000M float64 `json:"geopotential_height_1000_m"`
	WindSpeed500MS          float64 `json:"wind_speed_500_ms"`
	Temperature500C         float64 `json:"temperature_500_c"`
}

type fetchUpperAirOutput struct {
	Hourly []upperAirHourOutput `json:"hourly"`
}

// clampForecastDays keeps the request inside what the API will serve.
// Surviving the Storm asks for ten days to two weeks of upper-air watching;
// 16 is the model's limit and comfortably covers it.
func clampForecastDays(days int) int {
	if days < 1 {
		return 1
	}
	if days > 16 {
		return 16
	}
	return days
}

func upperAirRequestURL(lat, lon float64, days int) string {
	return fmt.Sprintf(upperAirURLFmt, lat, lon, clampForecastDays(days))
}

// parseOpenMeteoUpperAir turns the response into the flat hourly series the
// host expects.
//
// The response is requested in UTC, so the timestamps need no offset applied,
// unlike the weather and wave plugins which roll days up on a local boundary.
//
// A null in a numeric array (which Open-Meteo returns at the tail of a long
// run, and for any model without pressure levels) fails the float64 type
// assertion and stays zero. That is the contract's marker for absence: a 0m
// 500mb geopotential height is not a reading of anything.
func parseOpenMeteoUpperAir(raw []byte) ([]upperAirHourOutput, error) {
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("failed to parse upper-air response: %w", err)
	}

	hourly, ok := result["hourly"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("hourly missing from upper-air response")
	}

	times, ok := hourly["time"].([]any)
	if !ok {
		return nil, fmt.Errorf("hourly.time missing from upper-air response")
	}

	height500, _ := hourly["geopotential_height_500hPa"].([]any)
	height1000, _ := hourly["geopotential_height_1000hPa"].([]any)
	wind500, _ := hourly["wind_speed_500hPa"].([]any)
	temp500, _ := hourly["temperature_500hPa"].([]any)

	out := make([]upperAirHourOutput, 0, len(times))
	for i, rawTime := range times {
		timeStr, ok := rawTime.(string)
		if !ok {
			continue
		}
		parsed, err := time.Parse("2006-01-02T15:04", timeStr)
		if err != nil {
			continue
		}

		out = append(out, upperAirHourOutput{
			Time:                    parsed.UTC().Format(time.RFC3339),
			GeopotentialHeight500M:  numberAt(height500, i),
			GeopotentialHeight1000M: numberAt(height1000, i),
			WindSpeed500MS:          numberAt(wind500, i),
			Temperature500C:         numberAt(temp500, i),
		})
	}

	return out, nil
}

// numberAt reads an optional parallel array safely, so a short or absent one
// degrades to the contract's zero-means-absent rather than panicking.
func numberAt(values []any, i int) float64 {
	if i < 0 || i >= len(values) {
		return 0
	}
	v, ok := values[i].(float64)
	if !ok {
		return 0
	}
	return v
}
