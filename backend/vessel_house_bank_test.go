package main

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"strings"
	"testing"
)

// Shape of the live tree: several batteries report SoC and current, and the
// operator's choice (51) is not the one with the highest current.
const houseBankTree = `{"electrical":{
	"batteries":{
		"51":{"capacity":{"stateOfCharge":{"value":0.64},"nominal":{"value":600}},"voltage":{"value":26.4},"current":{"value":-12.5},"power":{"value":-330}},
		"239":{"capacity":{"stateOfCharge":{"value":0.99},"nominal":{"value":900}},"voltage":{"value":28.1},"current":{"value":-80},"power":{"value":-2248}},
		"house":{"capacity":{"stateOfCharge":{"value":0.5}},"voltage":{"value":25.0},"current":{"value":-1}}
	},
	"chargers":{"276":{"capacity":{"stateOfCharge":{"value":0.33}}}},
	"venus":{"batteryVoltage":{"value":27.7}}
}}`

func useHouseBank(t *testing.T, bank *vesselHouseBankSetting) vesselSettings {
	t.Helper()
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	if bank != nil {
		if err := saveVesselSettings(settingsPath, vesselSettings{HouseBank: bank}); err != nil {
			t.Fatal(err)
		}
	}
	vessel, err := loadVesselSettings(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	return vessel
}

func TestElectricalStateReadsEveryBankFigureFromTheChosenHouseBank(t *testing.T) {
	vessel := useHouseBank(t, &vesselHouseBankSetting{Path: "electrical.batteries.51", CapacityAh: 450})
	seedSelfTree(t, houseBankTree)

	state, err := fetchSignalKElectricalState(vessel)
	if err != nil {
		t.Fatal(err)
	}
	if state.BatterySocPercent != 64 {
		t.Errorf("soc: got %v, want 64 from batteries.51", state.BatterySocPercent)
	}
	if state.BatteryCapacityAh != 600 {
		t.Errorf("capacity: got %v, want 600 from <path>.capacity.nominal, not the settings figure", state.BatteryCapacityAh)
	}
	if state.ChargingCurrentA == nil || *state.ChargingCurrentA != -12.5 {
		t.Errorf("current: got %v, want -12.5", state.ChargingCurrentA)
	}
	if state.ChargingPowerW == nil || *state.ChargingPowerW != -330 {
		t.Errorf("power: got %v, want -330", state.ChargingPowerW)
	}
}

func TestElectricalStateCapacityFallsBackToTheSettingsFigure(t *testing.T) {
	vessel := useHouseBank(t, &vesselHouseBankSetting{Path: "electrical.batteries.51", CapacityAh: 450})
	seedSelfTree(t, `{"electrical":{"batteries":{"51":{"capacity":{"stateOfCharge":{"value":0.64}},"voltage":{"value":26.4}}}}}`)

	state, err := fetchSignalKElectricalState(vessel)
	if err != nil {
		t.Fatal(err)
	}
	if state.BatteryCapacityAh != 450 {
		t.Fatalf("capacity: got %v, want 450 from vessel.house_bank.capacity_ah", state.BatteryCapacityAh)
	}
}

func TestElectricalStatePowerIsDerivedFromTheSameBank(t *testing.T) {
	vessel := useHouseBank(t, &vesselHouseBankSetting{Path: "electrical.batteries.51"})
	seedSelfTree(t, `{"electrical":{"batteries":{"51":{"voltage":{"value":26},"current":{"value":10}},"239":{"voltage":{"value":28},"current":{"value":99}}}}}`)

	state, err := fetchSignalKElectricalState(vessel)
	if err != nil {
		t.Fatal(err)
	}
	if state.ChargingPowerW == nil || *state.ChargingPowerW != 260 {
		t.Fatalf("power: got %v, want 260 (10 A x 26 V from batteries.51)", state.ChargingPowerW)
	}
}

func TestElectricalStateWithNoHouseBankReadsNoBankFigures(t *testing.T) {
	vessel := useHouseBank(t, nil)
	seedSelfTree(t, houseBankTree)

	state, err := fetchSignalKElectricalState(vessel)
	if err != nil {
		t.Fatal(err)
	}
	if state.BatterySocPercent != -1 {
		t.Errorf("soc: got %v, want -1 (no guessing from another battery or charger)", state.BatterySocPercent)
	}
	if state.BatteryCapacityAh != -1 {
		t.Errorf("capacity: got %v, want -1", state.BatteryCapacityAh)
	}
	if state.ChargingCurrentA != nil || state.ChargingPowerW != nil {
		t.Errorf("current/power: got %v / %v, want nil", state.ChargingCurrentA, state.ChargingPowerW)
	}
}

func TestElectricalStateDoesNotUseOtherBatteriesWhenTheBankDoesNotReport(t *testing.T) {
	vessel := useHouseBank(t, &vesselHouseBankSetting{Path: "electrical.batteries.7"})
	seedSelfTree(t, houseBankTree)

	state, err := fetchSignalKElectricalState(vessel)
	if err != nil {
		t.Fatal(err)
	}
	if state.BatterySocPercent != -1 || state.ChargingCurrentA != nil {
		t.Fatalf("a bank that is not reporting must read as no data, got soc %v current %v", state.BatterySocPercent, state.ChargingCurrentA)
	}
}

func TestElectricalPayloadLogsAnUnsetHouseBankOnce(t *testing.T) {
	useHouseBank(t, nil)
	seedSelfTree(t, houseBankTree)
	resetHouseBankUnsetLog()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	buildElectricalStatePayload()
	buildElectricalStatePayload()
	buildElectricalStatePayload()

	if n := strings.Count(buf.String(), "no house bank is set"); n != 1 {
		t.Fatalf("expected one log line, got %d in %q", n, buf.String())
	}
}

func TestElectricalStateCapacityAcceptsTheSameBanksTotal(t *testing.T) {
	vessel := useHouseBank(t, &vesselHouseBankSetting{Path: "electrical.batteries.51", CapacityAh: 450})
	seedSelfTree(t, `{"electrical":{"batteries":{"51":{"capacity":{"total":{"value":520}}},"239":{"capacity":{"total":{"value":999}}}}}}`)

	state, err := fetchSignalKElectricalState(vessel)
	if err != nil {
		t.Fatal(err)
	}
	if state.BatteryCapacityAh != 520 {
		t.Fatalf("capacity: got %v, want 520 from batteries.51 capacity.total", state.BatteryCapacityAh)
	}
}

func TestElectricalPayloadCarriesTheBanksSocPath(t *testing.T) {
	useHouseBank(t, &vesselHouseBankSetting{Path: " electrical.batteries.51 "})
	seedSelfTree(t, houseBankTree)
	if got := buildElectricalStatePayload()["soc_path"]; got != "electrical.batteries.51.capacity.stateOfCharge" {
		t.Fatalf("soc_path: got %v", got)
	}
}

func TestElectricalPayloadHasNoSocPathWithoutAHouseBank(t *testing.T) {
	useHouseBank(t, nil)
	seedSelfTree(t, houseBankTree)
	if _, has := buildElectricalStatePayload()["soc_path"]; has {
		t.Fatal("soc_path must be absent with no house bank")
	}
}

func TestUnreadableVesselBlockIsLoggedOncePerDistinctError(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := os.WriteFile(settingsPath, []byte("vessel: [not, a, block]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resetVesselReadLog()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	seedSelfTree(t, houseBankTree)

	for i := 0; i < 3; i++ {
		buildElectricalStatePayload()
	}
	if n := strings.Count(buf.String(), "vessel settings unreadable"); n != 1 {
		t.Fatalf("expected one log line for three ticks, got %d in %q", n, buf.String())
	}

	// Reading cleanly resets it, so the next failure is reported again.
	if err := os.WriteFile(settingsPath, []byte("vessel: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	buildElectricalStatePayload()
	if err := os.WriteFile(settingsPath, []byte("vessel: [not, a, block]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	buildElectricalStatePayload()
	if n := strings.Count(buf.String(), "vessel settings unreadable"); n != 2 {
		t.Fatalf("expected a second line after a clean read, got %d", n)
	}
}

func TestElectricalPayloadSaysWhetherAHouseBankIsPicked(t *testing.T) {
	useHouseBank(t, nil)
	seedSelfTree(t, houseBankTree)
	if got := buildElectricalStatePayload()["house_bank_configured"]; got != false {
		t.Fatalf("unset: got %v", got)
	}

	useHouseBank(t, &vesselHouseBankSetting{Path: "electrical.batteries.51"})
	if got := buildElectricalStatePayload()["house_bank_configured"]; got != true {
		t.Fatalf("picked: got %v", got)
	}
}

func TestElectricalPayloadHouseBankConfiguredDoesNotDependOnTheFeed(t *testing.T) {
	useHouseBank(t, &vesselHouseBankSetting{Path: "electrical.batteries.51"})
	withGlobalSnapshot(t, newSignalKSnapshot()) // SignalK has dropped: no self data
	if got := buildElectricalStatePayload()["house_bank_configured"]; got != true {
		t.Fatalf("a dropped feed must not read as not picked, got %v", got)
	}
}

func TestElectricalPayloadHouseBankConfiguredIsUnknownWhenSettingsAreUnreadable(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := os.WriteFile(settingsPath, []byte("vessel: [not, a, block]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resetVesselReadLog()
	seedSelfTree(t, houseBankTree)
	if body, _ := json.Marshal(buildElectricalStatePayload()); !strings.Contains(string(body), `"house_bank_configured":null`) {
		t.Fatalf("got %s", body)
	}
}
