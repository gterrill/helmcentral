//go:build !std
// +build !std

// weathervalid is a minimal but complete weather-provider WASM fixture,
// implementing the full plugin contract (id/name/ttl_seconds/fetch_forecast)
// with small, fixed test data: 2 days and 4 hourly entries, including a
// sunset near the hourly range (for a host-side sunset-splice test) and at
// least one hour with is_daylight:false. See
// backend/wasm_weather_provider_test.go for the regeneration command.
package main

import (
	"github.com/extism/go-pdk"
)

type fetchForecastInput struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Days int     `json:"days"`
}

type currentOut struct {
	Time                   string  `json:"time"`
	TemperatureC           float64 `json:"temperature_c"`
	Condition              string  `json:"condition"`
	WindSpeedMS            float64 `json:"wind_speed_ms"`
	WindGustMS             float64 `json:"wind_gust_ms"`
	WindDirectionDeg       float64 `json:"wind_direction_deg"`
	PrecipitationChancePct float64 `json:"precipitation_chance_pct"`
}

type dayOut struct {
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

type hourOut struct {
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

type fetchForecastOutput struct {
	Current currentOut `json:"current"`
	Days    []dayOut   `json:"days"`
	Hourly  []hourOut  `json:"hourly"`
}

//go:wasmexport id
func id() int32 {
	pdk.OutputString("weather-valid-fixture")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("Weather Valid Fixture Provider")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	pdk.OutputString("900")
	return 0
}

//go:wasmexport fetch_forecast
func fetchForecast() int32 {
	var input fetchForecastInput
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return -1
	}

	out := fetchForecastOutput{
		Current: currentOut{
			Time: "2026-06-14T23:00:00Z", TemperatureC: 20.0, Condition: "clear",
			WindSpeedMS: 9.26, WindGustMS: 13.89, WindDirectionDeg: 90,
			PrecipitationChancePct: 10,
		},
		Days: []dayOut{
			{
				Start: "2026-06-14T14:00:00Z", Condition: "clear",
				TempMaxC: 24.0, TempMinC: 15.0,
				WindSpeedMS: 5.1, WindGustMS: 11.0, WindDirectionDeg: 120,
				PrecipitationChancePct: 20,
				Sunrise:                "2026-06-14T20:38:00Z",
				Sunset:                 "2026-06-15T07:09:00Z",
			},
			{
				Start: "2026-06-15T14:00:00Z", Condition: "rain",
				TempMaxC: 22.0, TempMinC: 14.0,
				WindSpeedMS: 6.2, WindGustMS: 12.0, WindDirectionDeg: 180,
				PrecipitationChancePct: 70,
				Sunrise:                "2026-06-15T20:39:00Z",
				Sunset:                 "2026-06-16T07:09:00Z",
			},
		},
		Hourly: []hourOut{
			{
				Time: "2026-06-14T22:00:00Z", TemperatureC: 20.5, Condition: "clear",
				WindSpeedMS: 9.0, WindGustMS: 13.0, WindDirectionDeg: 90,
				PrecipitationChancePct: 10, PrecipitationMM: 0, UVIndex: 2.0, IsDaylight: true,
			},
			{
				Time: "2026-06-14T23:00:00Z", TemperatureC: 20.0, Condition: "clear",
				WindSpeedMS: 9.26, WindGustMS: 13.89, WindDirectionDeg: 90,
				PrecipitationChancePct: 10, PrecipitationMM: 0, UVIndex: 1.0, IsDaylight: true,
			},
			{
				Time: "2026-06-15T00:00:00Z", TemperatureC: 18.0, Condition: "cloudy",
				WindSpeedMS: 4.76, WindDirectionDeg: 180,
				PrecipitationChancePct: 20, PrecipitationMM: 0.1, UVIndex: 0, IsDaylight: false,
			},
			{
				Time: "2026-06-15T09:00:00Z", TemperatureC: 16.0, Condition: "rain",
				WindSpeedMS: 6.2, WindGustMS: 12.0, WindDirectionDeg: 180,
				PrecipitationChancePct: 70, PrecipitationMM: 2.5, UVIndex: 0, IsDaylight: false,
			},
		},
	}

	if err := pdk.OutputJSON(out); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

func main() {}
