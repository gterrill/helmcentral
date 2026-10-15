package main

import (
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

type alternatorInstanceData struct {
	CurrentA float64
	VoltageV float64
	PowerW   float64
	TempC    float64
}

type chargerInstanceData struct {
	CurrentA      float64
	ACIn1CurrentA float64
	ChargingMode  string
	Error         string
}

type electricalStateData struct {
	Datetime            time.Time
	BatterySocPercent   float64
	BatteryCapacityAh   float64
	ChargingCurrentA    *float64 // nil when unreported; signed, so no sentinel number fits
	ChargingPowerW      *float64
	SolarOutputW        float64
	ACOutputW           float64
	DC12VPowerW         float64
	DC12VCurrentA       float64
	DC24VVoltageV       float64
	ACLoadsW            float64
	GeneratorRealPowerW float64
	LastUpdateAge       float64
	Alternator0         alternatorInstanceData
	Alternator1         alternatorInstanceData
	Charger0            chargerInstanceData
}

// buildElectricalStatePayload produces the /api response body. Split from the handler so
// the SSE stream emits the identical shape without the two drifting apart.
func buildElectricalStatePayload() map[string]any {
	state := electricalStateData{
		Datetime:            time.Now().UTC(),
		BatterySocPercent:   -1,
		BatteryCapacityAh:   -1,
		SolarOutputW:        -1,
		ACOutputW:           -1,
		DC12VPowerW:         -1,
		DC12VCurrentA:       -1,
		DC24VVoltageV:       -1,
		ACLoadsW:            -1,
		GeneratorRealPowerW: -1,
		Alternator0:         alternatorInstanceData{CurrentA: -1, VoltageV: -1, PowerW: -1, TempC: -1},
		Alternator1:         alternatorInstanceData{CurrentA: -1, VoltageV: -1, PowerW: -1, TempC: -1},
		Charger0:            chargerInstanceData{CurrentA: -1, ACIn1CurrentA: -1, ChargingMode: "", Error: ""},
	}
	source := "backend-fallback"

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)

	// A vessel block that cannot be read is logged, and both alternator
	// sides then read as not set. Every other electrical figure is unaffected.
	alternators, altErr := loadVesselAlternators()
	if altErr != nil {
		log.Printf("electrical state: alternator instances unreadable, alternators show no data: %v", altErr)
	}

	if signalkURL != "" {
		electrical, fetchErr := fetchSignalKElectricalState(alternators)
		if fetchErr == nil {
			state = electrical
			source = "signalk"
		}
	}

	return map[string]any{
		"datetime":                   state.Datetime.Format(time.RFC3339),
		"last_update_age_s":          state.LastUpdateAge,
		"battery_soc_percent":        state.BatterySocPercent,
		"battery_capacity_ah":        state.BatteryCapacityAh,
		"charging_current_a":         state.ChargingCurrentA,
		"charging_power_w":           state.ChargingPowerW,
		"solar_output_w":             state.SolarOutputW,
		"ac_output_w":                state.ACOutputW,
		"dc_power_w":                 state.DC12VPowerW,
		"dc_current_a":               state.DC12VCurrentA,
		"dc_12v_power_w":             state.DC12VPowerW,
		"dc_12v_current_a":           state.DC12VCurrentA,
		"dc_24v_voltage_v":           state.DC24VVoltageV,
		"ac_loads_w":                 state.ACLoadsW,
		"generator_real_power_w":     state.GeneratorRealPowerW,
		"alternator_0_current_a":     state.Alternator0.CurrentA,
		"alternator_0_voltage_v":     state.Alternator0.VoltageV,
		"alternator_0_power_w":       state.Alternator0.PowerW,
		"alternator_0_temperature_c": state.Alternator0.TempC,
		"alternator_1_current_a":     state.Alternator1.CurrentA,
		"alternator_1_voltage_v":     state.Alternator1.VoltageV,
		"alternator_1_power_w":       state.Alternator1.PowerW,
		"alternator_1_temperature_c": state.Alternator1.TempC,
		"charger_0_current_a":        state.Charger0.CurrentA,
		"charger_0_acin_1_current_a": state.Charger0.ACIn1CurrentA,
		"charger_0_charging_mode":    state.Charger0.ChargingMode,
		"charger_0_error":            state.Charger0.Error,
		"source":                     source,
	}
}

func electricalState(c echo.Context) error {
	return c.JSON(http.StatusOK, buildElectricalStatePayload())
}

// electricalRoutes returns the /api routes for the electrical state payload and overnight summary.
// Each row carries its own auth tier (docs/adr/0177).
func electricalRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/electrical-state", tierRead, electricalState},
		{http.MethodGet, "/api/electrical/overnight", tierRead, electricalOvernightHandler},
	}
}
