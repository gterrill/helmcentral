//go:build !std
// +build !std

// wavevalid is a minimal but complete wave-provider WASM fixture,
// implementing the full plugin contract (id/name/ttl_seconds/fetch_waves)
// with small, fixed test data: 4 hourly entries spanning 2 local days
// (mirroring weathervalid's 2026-06-14/2026-06-15 split), including
// sea_surface_temperature_c so the happy path (sea temp present) is
// covered. See backend/wasm_wave_provider_test.go for the regeneration
// command; the "sea temp entirely absent" case is covered separately via a
// direct JSON-unmarshal unit test (no second fixture needed - a Go
// *float64 field naturally stays nil when a JSON key is omitted).
package main

import (
	"github.com/extism/go-pdk"
)

type fetchWavesInput struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Days int     `json:"days"`
}

type hourOut struct {
	Time             string  `json:"time"`
	WaveHeightM      float64 `json:"wave_height_m"`
	WavePeriodS      float64 `json:"wave_period_s"`
	WaveDirectionDeg float64 `json:"wave_direction_deg"`
	WindWaveHeightM  float64 `json:"wind_wave_height_m"`
	SwellWaveHeightM float64 `json:"swell_wave_height_m"`
}

type fetchWavesOutput struct {
	Hourly                 []hourOut `json:"hourly"`
	SeaSurfaceTemperatureC float64   `json:"sea_surface_temperature_c"`
}

//go:wasmexport id
func id() int32 {
	pdk.OutputString("wave-valid-fixture")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("Wave Valid Fixture Provider")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	pdk.OutputString("900")
	return 0
}

//go:wasmexport fetch_waves
func fetchWaves() int32 {
	var input fetchWavesInput
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return -1
	}

	out := fetchWavesOutput{
		Hourly: []hourOut{
			{
				Time: "2026-06-14T22:00:00Z", WaveHeightM: 1.2, WavePeriodS: 8.5,
				WaveDirectionDeg: 140, WindWaveHeightM: 0.4, SwellWaveHeightM: 1.0,
			},
			{
				Time: "2026-06-14T23:00:00Z", WaveHeightM: 1.3, WavePeriodS: 8.7,
				WaveDirectionDeg: 145, WindWaveHeightM: 0.4, SwellWaveHeightM: 1.1,
			},
			{
				Time: "2026-06-15T00:00:00Z", WaveHeightM: 1.4, WavePeriodS: 9.0,
				WaveDirectionDeg: 150, WindWaveHeightM: 0.5, SwellWaveHeightM: 1.2,
			},
			{
				Time: "2026-06-15T09:00:00Z", WaveHeightM: 1.1, WavePeriodS: 8.2,
				WaveDirectionDeg: 135, WindWaveHeightM: 0.3, SwellWaveHeightM: 0.9,
			},
		},
		SeaSurfaceTemperatureC: 21.5,
	}

	if err := pdk.OutputJSON(out); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

func main() {}
