package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// Shape of the live tree: several instances of each kind, and the ones the
// operator picks are not instance 0.
const electricalInstancesTree = `{"electrical":{
	"generator":{
		"0":{"state":{"value":"stopped"},"runtime":{"value":100}},
		"3":{"state":{"value":"running"},"manualStart":{"value":true},"manualStartTimer":{"value":600},"runningByCondition":{"value":"manual"},"runtime":{"value":7200}}
	},
	"ac":{
		"1":{"phase":{"A":{"realPower":{"value":300}}}},
		"5":{"phase":{"A":{"realPower":{"value":1250}}}}
	},
	"chargers":{
		"0":{"current":{"value":9},"acin":{"1":{"current":{"value":2}}}},
		"5":{"current":{"value":20},"acin":{"1":{"current":{"value":6}}},"chargingMode":{"value":"bulk"}}
	},
	"inverters":{
		"0":{"acout":{"power":{"value":111}}},
		"5":{"acout":{"power":{"value":420}}}
	},
	"batteries":{
		"239":{"voltage":{"value":27.9}},
		"51":{"voltage":{"value":26.1},"name":{"value":"Bus name"}}
	},
	"dc":{"24v":{"voltage":{"value":24.4}}}
}}`

func saveElectricalVessel(t *testing.T, v vesselSettings) vesselSettings {
	t.Helper()
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := saveVesselSettings(settingsPath, v); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadVesselSettings(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func TestValidateVesselElectrical(t *testing.T) {
	cases := []struct {
		name    string
		in      vesselElectricalSettings
		wantErr string
	}{
		{"all unset", vesselElectricalSettings{}, ""},
		{"zero is an instance", vesselElectricalSettings{GeneratorInstance: "0"}, ""},
		{"all set", vesselElectricalSettings{GeneratorInstance: "3", GeneratorACInstance: "5", ShoreChargerInstance: "276", InverterInstance: "5", VoltageReadoutBattery: "239"}, ""},
		{"dotted generator", vesselElectricalSettings{GeneratorInstance: "0.state"}, "vessel.electrical.generator_instance"},
		{"slash ac", vesselElectricalSettings{GeneratorACInstance: "a/b"}, "vessel.electrical.generator_ac_instance"},
		{"space charger", vesselElectricalSettings{ShoreChargerInstance: "a b"}, "vessel.electrical.shore_charger_instance"},
		{"dotted inverter", vesselElectricalSettings{InverterInstance: "5.x"}, "vessel.electrical.inverter_instance"},
		{"dotted readout", vesselElectricalSettings{VoltageReadoutBattery: "../x"}, "vessel.electrical.voltage_readout_battery"},
	}
	for _, c := range cases {
		got := validateVesselElectrical(c.in)
		switch {
		case c.wantErr == "" && got != nil:
			t.Errorf("%s: unexpected error %+v", c.name, got)
		case c.wantErr != "" && (got == nil || got.Field != c.wantErr):
			t.Errorf("%s: got %+v, want field %s", c.name, got, c.wantErr)
		}
	}
}

func TestUpdateSettingsSavesTrimsAndRejectsElectricalAndSolar(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	withTempAlarmRules(t)

	code, _ := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Vessel = &vesselSettings{
			Electrical: vesselElectricalSettings{GeneratorInstance: " 3 ", GeneratorACInstance: "5", ShoreChargerInstance: " 276", InverterInstance: "5 ", VoltageReadoutBattery: " 239 "},
			Solar:      []vesselSolarSetting{{Instance: " 289 ", Name: " Port "}, {Instance: "288", Name: "Starboard"}, {Instance: "290", Name: "  "}, {Instance: "288", Name: "Dup"}},
		}
	})
	if code != http.StatusOK {
		t.Fatalf("got %d", code)
	}
	saved, err := loadVesselSettings(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	want := vesselElectricalSettings{GeneratorInstance: "3", GeneratorACInstance: "5", ShoreChargerInstance: "276", InverterInstance: "5", VoltageReadoutBattery: "239"}
	if saved.Electrical != want {
		t.Fatalf("electrical: got %+v", saved.Electrical)
	}
	if len(saved.Solar) != 2 || saved.Solar[0] != (vesselSolarSetting{Instance: "288", Name: "Starboard"}) || saved.Solar[1] != (vesselSolarSetting{Instance: "289", Name: "Port"}) {
		t.Fatalf("solar names: got %+v", saved.Solar)
	}

	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Vessel = &vesselSettings{Electrical: vesselElectricalSettings{GeneratorInstance: "0.state"}}
	})
	if code == http.StatusOK {
		t.Fatalf("a dotted generator id must be rejected, body %v", body)
	}
	saved, _ = loadVesselSettings(settingsPath)
	if saved.Electrical.GeneratorInstance != "3" {
		t.Fatalf("a rejected save must not change the stored value, got %+v", saved.Electrical)
	}
}

func TestElectricalStateReadsTheConfiguredInstances(t *testing.T) {
	vessel := saveElectricalVessel(t, vesselSettings{
		Electrical: vesselElectricalSettings{GeneratorACInstance: "5", ShoreChargerInstance: "5", InverterInstance: "5", VoltageReadoutBattery: "239"},
		Batteries:  []vesselBatterySetting{{Instance: "239", Name: "Aft House"}},
	})
	seedSelfTree(t, electricalInstancesTree)

	state, err := fetchSignalKElectricalState(vessel)
	if err != nil {
		t.Fatal(err)
	}
	if state.GeneratorRealPowerW != 1250 {
		t.Errorf("generator power: got %v, want 1250 from ac.5", state.GeneratorRealPowerW)
	}
	if state.Charger0.CurrentA != 20 || state.Charger0.ACIn1CurrentA != 6 || state.Charger0.ChargingMode != "bulk" {
		t.Errorf("shore charger: got %+v, want chargers.5", state.Charger0)
	}
	if state.ACOutputW != 420 || state.ACLoadsW != 420 {
		t.Errorf("inverter output: got out %v loads %v, want 420 from inverters.5", state.ACOutputW, state.ACLoadsW)
	}
	if state.VoltageReadoutV != 27.9 || state.VoltageReadoutLabel != "Aft House" {
		t.Errorf("voltage readout: got %v %q", state.VoltageReadoutV, state.VoltageReadoutLabel)
	}
}

func TestElectricalStateWithNothingSetReadsNoInstanceFigures(t *testing.T) {
	vessel := saveElectricalVessel(t, vesselSettings{})
	seedSelfTree(t, electricalInstancesTree)

	state, err := fetchSignalKElectricalState(vessel)
	if err != nil {
		t.Fatal(err)
	}
	if state.GeneratorRealPowerW != -1 {
		t.Errorf("generator power: got %v", state.GeneratorRealPowerW)
	}
	if state.Charger0.CurrentA != -1 || state.Charger0.ACIn1CurrentA != -1 || state.Charger0.ChargingMode != "" {
		t.Errorf("charger: got %+v, want nothing (no instance 0 guess)", state.Charger0)
	}
	if state.ACOutputW != -1 || state.ACLoadsW != -1 {
		t.Errorf("inverter output: got out %v loads %v, want none (no inverters.0 guess)", state.ACOutputW, state.ACLoadsW)
	}
	if state.VoltageReadoutV != -1 || state.VoltageReadoutLabel != "" {
		t.Errorf("voltage readout: got %v %q, want hidden (no dc.24v or house fallback)", state.VoltageReadoutV, state.VoltageReadoutLabel)
	}
}

func TestElectricalStateInverterReadsOnlyTheChosenInstance(t *testing.T) {
	vessel := saveElectricalVessel(t, vesselSettings{Electrical: vesselElectricalSettings{InverterInstance: "9"}})
	seedSelfTree(t, electricalInstancesTree)
	state, err := fetchSignalKElectricalState(vessel)
	if err != nil {
		t.Fatal(err)
	}
	if state.ACOutputW != -1 {
		t.Fatalf("got %v: another inverter's output must not stand in for the chosen one", state.ACOutputW)
	}
}

func TestElectricalStateInverterAcceptsEachLeafVariant(t *testing.T) {
	for _, leaf := range []string{`"ac":{"output":{"power":{"value":77}}}`, `"acout":{"power":{"value":77}}`, `"acOutputPower":{"value":77}`} {
		vessel := saveElectricalVessel(t, vesselSettings{Electrical: vesselElectricalSettings{InverterInstance: "7"}})
		seedSelfTree(t, `{"electrical":{"inverters":{"7":{`+leaf+`}}}}`)
		state, err := fetchSignalKElectricalState(vessel)
		if err != nil {
			t.Fatal(err)
		}
		if state.ACOutputW != 77 {
			t.Errorf("%s: got %v", leaf, state.ACOutputW)
		}
	}
}

func TestElectricalStateVoltageReadoutLabelFallsBackToBusNameThenId(t *testing.T) {
	seedSelfTree(t, electricalInstancesTree)

	vessel := saveElectricalVessel(t, vesselSettings{Electrical: vesselElectricalSettings{VoltageReadoutBattery: "51"}})
	state, _ := fetchSignalKElectricalState(vessel)
	if state.VoltageReadoutV != 26.1 || state.VoltageReadoutLabel != "Bus name" {
		t.Errorf("bus name: got %v %q", state.VoltageReadoutV, state.VoltageReadoutLabel)
	}

	vessel = saveElectricalVessel(t, vesselSettings{Electrical: vesselElectricalSettings{VoltageReadoutBattery: "239"}})
	state, _ = fetchSignalKElectricalState(vessel)
	if state.VoltageReadoutLabel != "Battery 239" {
		t.Errorf("id label: got %q", state.VoltageReadoutLabel)
	}
}

func TestElectricalStateVoltageReadoutChosenButSilentIsHiddenNotSubstituted(t *testing.T) {
	vessel := saveElectricalVessel(t, vesselSettings{
		Electrical: vesselElectricalSettings{VoltageReadoutBattery: "999"},
		HouseBank:  &vesselHouseBankSetting{Path: "electrical.batteries.239"},
	})
	seedSelfTree(t, electricalInstancesTree)
	state, _ := fetchSignalKElectricalState(vessel)
	if state.VoltageReadoutV != -1 {
		t.Fatalf("got %v: house voltage must not stand in for a silent readout battery", state.VoltageReadoutV)
	}
}

func TestElectricalPayloadCarriesTheVoltageReadoutAndNotTheOld24VField(t *testing.T) {
	saveElectricalVessel(t, vesselSettings{
		Electrical: vesselElectricalSettings{VoltageReadoutBattery: "239"},
		Batteries:  []vesselBatterySetting{{Instance: "239", Name: "Aft House"}},
	})
	seedSelfTree(t, electricalInstancesTree)
	payload := buildElectricalStatePayload()
	if payload["voltage_readout_v"] != 27.9 || payload["voltage_readout_label"] != "Aft House" {
		t.Fatalf("got %v %v", payload["voltage_readout_v"], payload["voltage_readout_label"])
	}
	if _, has := payload["dc_24v_voltage_v"]; has {
		t.Fatal("dc_24v_voltage_v must be gone")
	}
}

func TestElectricalPayloadLogsUnsetElectricalInstancesOnce(t *testing.T) {
	saveElectricalVessel(t, vesselSettings{})
	resetVesselReadLog()
	resetElectricalUnsetLog()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	seedSelfTree(t, electricalInstancesTree)

	buildElectricalStatePayload()
	buildElectricalStatePayload()
	if n := strings.Count(buf.String(), "not set in Vessel settings"); n < 1 {
		t.Fatalf("expected a log line naming what is unset, got %q", buf.String())
	}
	first := strings.Count(buf.String(), "shore charger")
	if first != 1 {
		t.Fatalf("the shore charger line must appear once, appeared %d times in %q", first, buf.String())
	}
}

func TestVesselStateReadsTheConfiguredGenerator(t *testing.T) {
	saveElectricalVessel(t, vesselSettings{Electrical: vesselElectricalSettings{GeneratorInstance: "3"}})
	seedSelfTree(t, electricalInstancesTree)
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	state, _ := fetchSignalKVesselState()
	if state.GeneratorState != "running" || !state.GeneratorManualStart || state.GeneratorManualStartTimer != 600 ||
		state.GeneratorRunningByCondition != "manual" || state.GeneratorRuntime != 7200 {
		t.Fatalf("got %+v", state)
	}
}

func TestVesselStateWithNoGeneratorSetHasNoGeneratorFields(t *testing.T) {
	saveElectricalVessel(t, vesselSettings{})
	seedSelfTree(t, electricalInstancesTree)
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	state, _ := fetchSignalKVesselState()
	if state.GeneratorState != "" || state.GeneratorManualStart || state.GeneratorRuntime != 0 {
		t.Fatalf("instance 0 must not be guessed, got %+v", state)
	}
}

func TestGeneratorBasePath(t *testing.T) {
	got, problem := generatorBasePath(vesselSettings{Electrical: vesselElectricalSettings{GeneratorInstance: "3"}})
	if problem != "" || got != "/signalk/v1/api/vessels/self/electrical/generator/3" {
		t.Fatalf("got %q %q", got, problem)
	}
	if _, problem := generatorBasePath(vesselSettings{}); !strings.Contains(problem, "not set up in Vessel settings") {
		t.Fatalf("unset: got %q", problem)
	}
	if _, problem := generatorBasePath(vesselSettings{Electrical: vesselElectricalSettings{GeneratorInstance: "0/../../x"}}); problem == "" {
		t.Fatal("an id with path characters must be refused before it reaches a URL")
	}
}

func TestGeneratorStartAndStopAre409WhenNoGeneratorIsSet(t *testing.T) {
	saveElectricalVessel(t, vesselSettings{})
	for name, handler := range map[string]echo.HandlerFunc{"start": postGeneratorStartHandler, "stop": postGeneratorStopHandler} {
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/api/generator/"+name, strings.NewReader(`{}`)), rec)
		if err := handler(c); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusConflict {
			t.Fatalf("%s: got %d, want 409", name, rec.Code)
		}
		var body map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if !strings.Contains(body["error"], "not set up in Vessel settings") {
			t.Fatalf("%s: error %q", name, body["error"])
		}
	}
}

func TestSolarControllersAreNamedByIdFromVesselSettings(t *testing.T) {
	vessel := vesselSettings{Solar: []vesselSolarSetting{{Instance: "289", Name: "Starboard arch"}}}
	seedSelfTree(t, `{"electrical":{"solar":{
		"288":{"panelPower":{"value":100}},
		"289":{"panelPower":{"value":200}},
		"290":{"panelPower":{"value":300}},
		"291":{"panelPower":{"value":400}}}}}`)
	state, err := fetchSignalKSolarState(vessel)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{}
	for _, c := range state.Controllers {
		labels[c.ID] = c.Label
	}
	if labels["289"] != "Starboard arch" || labels["288"] != "Array 288" || labels["291"] != "Array 291" {
		t.Fatalf("got %v", labels)
	}
}

func TestSolarPayloadLogsAnUnreadableVesselBlock(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := os.WriteFile(settingsPath, []byte("vessel: [not, a, block]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resetVesselReadLog()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	seedSelfTree(t, `{"electrical":{"solar":{"288":{"panelPower":{"value":100}}}}}`)

	payload := buildSolarStatePayload()
	if !strings.Contains(buf.String(), "vessel settings unreadable") {
		t.Fatalf("expected a log line, got %q", buf.String())
	}
	controllers, _ := payload["controllers"].([]map[string]any)
	if len(controllers) != 1 || controllers[0]["label"] != "Array 288" {
		t.Fatalf("got %v", payload["controllers"])
	}
}

func TestVesselCandidatesListElectricalInstancesWithFreshReadings(t *testing.T) {
	fresh := time.Now().UTC()
	snapshot := newSignalKSnapshot()
	apply := func(at time.Time, values map[string]any) {
		var entries []signalKValue
		for p, v := range values {
			entries = append(entries, signalKValue{Path: p, Value: v})
		}
		snapshot.applyDelta(signalKDelta{Context: "vessels.self", Updates: []signalKUpdate{{Values: entries}}}, at)
	}
	apply(fresh, map[string]any{
		"electrical.generator.12.state":            "running",
		"electrical.generator.0.state":             "stopped",
		"electrical.ac.5.phase.A.realPower":        1250.0,
		"electrical.ac.40.realPower":               15.0,
		"electrical.chargers.276.current":          20.0,
		"electrical.chargers.276.acin.1.current":   6.0,
		"electrical.chargers.0.current":            3.0,
		"electrical.inverters.5.acout.power":       420.0,
		"electrical.solar.289.panelPower":          210.0,
		"electrical.solar.288.panelPower":          110.0,
		"electrical.inverters.276.ac.output.power": 5.0,
	})
	// An instance that stopped reporting an hour ago is still offered (the
	// operator may have picked it), but without a reading.
	apply(fresh.Add(-time.Hour), map[string]any{"electrical.solar.291.panelPower": 999.0, "electrical.generator.7.state": "running"})
	snapshot.setSelfContext("vessels.self")
	withGlobalSnapshot(t, snapshot)

	got := vesselCandidates(globalSignalKSnapshot, vesselSettings{})

	if len(got.Generators) != 3 || got.Generators[0].Instance != "0" || got.Generators[1].Instance != "7" || got.Generators[2].Instance != "12" {
		t.Fatalf("generators: %+v", got.Generators)
	}
	if got.Generators[2].State == nil || *got.Generators[2].State != "running" {
		t.Errorf("generator 12 state: %v", got.Generators[2].State)
	}
	if got.Generators[1].State != nil {
		t.Errorf("a stale generator state must not read as live, got %v", *got.Generators[1].State)
	}
	if len(got.AC) != 2 || got.AC[0].Instance != "5" || got.AC[1].Instance != "40" {
		t.Fatalf("ac: %+v", got.AC)
	}
	if got.AC[0].RealPower == nil || *got.AC[0].RealPower != 1250 || got.AC[1].RealPower == nil || *got.AC[1].RealPower != 15 {
		t.Errorf("ac power: %+v", got.AC)
	}
	if len(got.Chargers) != 2 || got.Chargers[0].Instance != "0" || got.Chargers[1].Instance != "276" {
		t.Fatalf("chargers: %+v", got.Chargers)
	}
	if got.Chargers[1].Current == nil || *got.Chargers[1].Current != 20 || got.Chargers[1].ACInCurrent == nil || *got.Chargers[1].ACInCurrent != 6 {
		t.Errorf("charger 276: %+v", got.Chargers[1])
	}
	if got.Chargers[0].ACInCurrent != nil {
		t.Errorf("charger 0 has no AC input reading, got %v", *got.Chargers[0].ACInCurrent)
	}
	if len(got.Inverters) != 2 || got.Inverters[0].Instance != "5" || got.Inverters[1].Instance != "276" {
		t.Fatalf("inverters: %+v", got.Inverters)
	}
	if got.Inverters[0].ACOut == nil || *got.Inverters[0].ACOut != 420 || got.Inverters[1].ACOut == nil || *got.Inverters[1].ACOut != 5 {
		t.Errorf("inverter output: %+v", got.Inverters)
	}
	if len(got.Solar) != 3 || got.Solar[0].Instance != "288" || got.Solar[1].Instance != "289" || got.Solar[2].Instance != "291" {
		t.Fatalf("solar: %+v", got.Solar)
	}
	if got.Solar[1].PanelPower == nil || *got.Solar[1].PanelPower != 210 {
		t.Errorf("solar 289: %+v", got.Solar[1])
	}
	if got.Solar[2].PanelPower != nil {
		t.Errorf("a stale solar reading must not read as live, got %v", *got.Solar[2].PanelPower)
	}
}

func TestVesselCandidatesElectricalListsAreEmptyArraysNotNull(t *testing.T) {
	seedSelfTree(t, `{}`)
	body, err := json.Marshal(vesselCandidates(globalSignalKSnapshot, vesselSettings{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"generators", "ac", "chargers", "inverters", "solar"} {
		if !strings.Contains(string(body), `"`+key+`":[]`) {
			t.Errorf("%s must serialise as [], got %s", key, body)
		}
	}
}

func TestElectricalPayloadSaysWhetherAGeneratorIsSetUp(t *testing.T) {
	seedSelfTree(t, electricalInstancesTree)

	saveElectricalVessel(t, vesselSettings{})
	if got := buildElectricalStatePayload()["generator_configured"]; got != false {
		t.Fatalf("unset: got %v", got)
	}

	saveElectricalVessel(t, vesselSettings{Electrical: vesselElectricalSettings{GeneratorInstance: "0"}})
	if got := buildElectricalStatePayload()["generator_configured"]; got != true {
		t.Fatalf("instance 0 is a real choice: got %v", got)
	}
}

func TestElectricalPayloadGeneratorConfiguredDoesNotDependOnTheFeed(t *testing.T) {
	saveElectricalVessel(t, vesselSettings{Electrical: vesselElectricalSettings{GeneratorInstance: "3"}})
	withGlobalSnapshot(t, newSignalKSnapshot()) // SignalK has dropped: no self data
	if got := buildElectricalStatePayload()["generator_configured"]; got != true {
		t.Fatalf("a dropped feed must not read as not set up, got %v", got)
	}
}

func TestElectricalPayloadGeneratorConfiguredIsUnknownWhenSettingsAreUnreadable(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := os.WriteFile(settingsPath, []byte("vessel: [not, a, block]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resetVesselReadLog()
	seedSelfTree(t, electricalInstancesTree)
	payload := buildElectricalStatePayload()
	v, has := payload["generator_configured"]
	if !has {
		t.Fatal("the key must be present")
	}
	if b, isBool := v.(bool); isBool {
		t.Fatalf("unreadable settings must send null, got %v", b)
	}
	if body, _ := json.Marshal(payload); !strings.Contains(string(body), `"generator_configured":null`) {
		t.Fatalf("got %s", body)
	}
}

func TestVesselCandidatesOfferOnlyACNodesThatCarryRealPower(t *testing.T) {
	seedSelfTree(t, `{"electrical":{"ac":{
		"5":{"phase":{"A":{"realPower":{"value":10}}}},
		"6":{"phaseA":{"realPower":{"value":null}}},
		"7":{"realPower":{"value":3}},
		"loads":{"total":{"power":{"value":99}}},
		"9":{"phase":{"A":{"current":{"value":2}}}}}}}`)
	got := vesselCandidates(globalSignalKSnapshot, vesselSettings{})
	var ids []string
	for _, a := range got.AC {
		ids = append(ids, a.Instance)
	}
	if strings.Join(ids, ",") != "5,6,7" {
		t.Fatalf("got %v, want 5,6,7", ids)
	}
}

func TestLoadVesselSettingsTrimsElectricalIds(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	if err := os.WriteFile(settingsPath, []byte("vessel:\n  electrical:\n    generator_instance: ' 3 '\n    voltage_readout_battery: ' 239'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := loadVesselSettings(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if v.Electrical.GeneratorInstance != "3" || v.Electrical.VoltageReadoutBattery != "239" {
		t.Fatalf("got %+v", v.Electrical)
	}
}
