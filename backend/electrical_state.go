package main

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
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
	SocPath             string // the house bank's state-of-charge path; "" with no bank
	BatteryCapacityAh   float64
	ChargingCurrentA    *float64 // nil when unreported; signed, so no sentinel number fits
	ChargingPowerW      *float64
	SolarOutputW        float64
	ACOutputW           float64
	DC12VPowerW         float64
	DC12VCurrentA       float64
	VoltageReadoutV     float64 // the chosen voltage readout battery; -1 when unset or silent
	VoltageReadoutLabel string
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
		VoltageReadoutV:     -1,
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

	// A vessel block that cannot be read is logged, and the alternators and
	// the house bank figures then read as not set. Every other electrical
	// figure is unaffected.
	vessel, vesselErr := loadVesselView(settingsPath)
	noteVesselRead("electrical state", vesselErr, "electrical state: vessel unreadable, alternator, electrical instances and house bank show no data: %v")
	if vesselErr != nil {
		vessel = vesselView{}
	} else {
		noteHouseBank(vessel.HouseBank)
		noteElectricalInstances(vessel)
	}

	// Whether a generator is chosen comes from the settings alone, so a
	// SignalK drop never reads as "not set up". It is null (unknown) when the
	// vessel block cannot be read.
	var generatorConfigured any
	// generator_none is the operator's answer "this boat has none": unlike a
	// blank role it is not a gap to fix, so the tile says so plainly.
	generatorNone := false
	if vesselErr == nil {
		generatorConfigured = vessel.Electrical.GeneratorInstance != ""
		generatorNone = vessel.RolesNone["generator"]
	}

	// Likewise for the house bank: a chosen bank comes from the roles alone,
	// so a missing state-of-charge reading cannot tell "no bank chosen" from
	// "bank not reporting". Null when the vessel block cannot be read.
	var houseBankConfigured any
	if vesselErr == nil {
		houseBankConfigured = vessel.houseBankPath() != ""
	}

	if signalkURL != "" {
		electrical, fetchErr := fetchSignalKElectricalState(vessel)
		if fetchErr == nil {
			state = electrical
			source = "signalk"
		}
	}

	payload := map[string]any{
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
		"voltage_readout_v":          state.VoltageReadoutV,
		"voltage_readout_label":      state.VoltageReadoutLabel,
		"ac_loads_w":                 state.ACLoadsW,
		"generator_real_power_w":     state.GeneratorRealPowerW,
		"generator_configured":       generatorConfigured,
		"generator_none":             generatorNone,
		"house_bank_configured":      houseBankConfigured,
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
	// The house bank's state-of-charge path, for the tile's bands. Absent
	// with no bank, and independent of position or the dawn projection.
	if state.SocPath != "" {
		payload["soc_path"] = state.SocPath
	}
	return payload
}

// houseBankUnsetLogged makes the "no house bank" line appear once, not on
// every tick. It resets when a bank is set, so removing the bank later logs
// again.
var houseBankUnsetLogged atomic.Bool

func resetHouseBankUnsetLog() { houseBankUnsetLogged.Store(false) }

func noteHouseBank(bank *vesselHouseBankSetting) {
	if bank != nil && strings.TrimSpace(bank.Path) != "" {
		houseBankUnsetLogged.Store(false)
		return
	}
	if houseBankUnsetLogged.CompareAndSwap(false, true) {
		log.Printf("electrical state: no house bank is chosen under Settings > SignalK > Roles, so the Battery & Power bank figures show no data")
	}
}

// electricalUnsetLogged remembers which electrical instances the last log
// line named as unset, so an unchanged gap is logged once, not every tick. A
// different set logs again; nothing unset forgets it.
var electricalUnsetLogged = struct {
	mu   sync.Mutex
	last string
}{}

func resetElectricalUnsetLog() {
	electricalUnsetLogged.mu.Lock()
	defer electricalUnsetLogged.mu.Unlock()
	electricalUnsetLogged.last = ""
}

// electricalUnsetLine names, comma separated, the generator, generator AC
// input, shore charger, inverter and voltage readout battery that are not
// chosen under Settings > SignalK > Roles. A role answered "This boat has none"
// is an answer, not a gap, and is left out.
func electricalUnsetLine(v vesselView) string {
	e := v.Electrical
	var unset []string
	for _, item := range []struct{ name, field, value string }{
		{"generator", "generator", e.GeneratorInstance},
		{"generator AC input", "generator_ac_instance", e.GeneratorACInstance},
		{"shore charger", "shore_charger", e.ShoreChargerInstance},
		{"inverter", "inverter", e.InverterInstance},
		{"voltage readout battery", "voltage_readout_battery", e.VoltageReadoutBattery},
	} {
		if strings.TrimSpace(item.value) == "" && !v.RolesNone[item.field] {
			unset = append(unset, item.name)
		}
	}
	return strings.Join(unset, ", ")
}

// noteElectricalInstances logs, once, which of those roles are not chosen, and
// so show no data.
func noteElectricalInstances(v vesselView) {
	line := electricalUnsetLine(v)

	electricalUnsetLogged.mu.Lock()
	defer electricalUnsetLogged.mu.Unlock()
	if line == electricalUnsetLogged.last {
		return
	}
	electricalUnsetLogged.last = line
	if line != "" {
		log.Printf("electrical state: not chosen under Settings > SignalK > Roles, so these show no data: %s", line)
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
