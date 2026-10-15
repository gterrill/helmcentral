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

func saveElectricalVessel(t *testing.T, v vesselView) vesselView {
	t.Helper()
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := saveVesselFixture(t, settingsPath, v); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadVesselView(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func TestValidateVesselRolesElectrical(t *testing.T) {
	items := []sourceItem{
		{ID: "g3", Name: "Genset", Source: "electrical.generator.3", Group: sourceGenerator, Instance: "3"},
		{ID: "c276", Name: "Charger", Source: "electrical.chargers.276", Group: sourceCharger, Instance: "276"},
		{ID: "i5", Name: "Inverter", Source: "electrical.inverters.5", Group: sourceInverter, Instance: "5"},
		{ID: "b239", Name: "Aft house", Source: "electrical.batteries.239", Group: sourceBattery, Instance: "239"},
	}
	cases := []struct {
		name    string
		in      vesselRoles
		wantErr string
	}{
		{"all unset", vesselRoles{}, ""},
		{"all set", vesselRoles{Generator: "g3", GeneratorACInstance: "5", ShoreCharger: "c276", Inverter: "i5", VoltageReadoutBattery: "b239"}, ""},
		{"generator is a charger", vesselRoles{Generator: "c276"}, "roles.generator"},
		{"charger is a generator", vesselRoles{ShoreCharger: "g3"}, "roles.shore_charger"},
		{"inverter missing", vesselRoles{Inverter: "gone"}, "roles.inverter"},
		{"readout is an inverter", vesselRoles{VoltageReadoutBattery: "i5"}, "roles.voltage_readout_battery"},
		{"slash ac", vesselRoles{GeneratorACInstance: "a/b"}, "roles.generator_ac_instance"},
	}
	for _, c := range cases {
		got := validateVesselRoles(c.in, items)
		switch {
		case c.wantErr == "" && got != nil:
			t.Errorf("%s: unexpected error %+v", c.name, got)
		case c.wantErr != "" && (got == nil || got.Field != c.wantErr):
			t.Errorf("%s: got %+v, want field %s", c.name, got, c.wantErr)
		}
	}
}

func TestUpdateSettingsSavesAndRejectsElectricalRoles(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	withTempAlarmRules(t)
	store := vesselFixtureStore(t)
	mk := func(name, source string) string {
		item, err := store.CreateEquipment(equipmentItem{Name: name, SignalKSource: source})
		if err != nil {
			t.Fatal(err)
		}
		return item.ID
	}
	gen := mk("Genset", "electrical.generator.3")
	charger := mk("Shore charger", "electrical.chargers.276")
	inverter := mk("Inverter", "electrical.inverters.5")
	readout := mk("Aft house", "electrical.batteries.239")
	solar := mk("Port array", "electrical.solar.289")

	code, _ := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Roles = &vesselRoles{Generator: gen, GeneratorACInstance: " 5 ", ShoreCharger: charger, Inverter: inverter, VoltageReadoutBattery: readout}
	})
	if code != http.StatusOK {
		t.Fatalf("got %d", code)
	}
	saved, err := loadVesselView(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	want := vesselElectricalSettings{GeneratorInstance: "3", GeneratorACInstance: "5", ShoreChargerInstance: "276", InverterInstance: "5", VoltageReadoutBattery: "239"}
	if saved.Electrical != want {
		t.Fatalf("electrical: got %+v", saved.Electrical)
	}
	if len(saved.Solar) != 1 || saved.Solar[0] != (vesselSolarSetting{Instance: "289", Name: "Port array"}) {
		t.Fatalf("solar names come from the mapped item: got %+v", saved.Solar)
	}

	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Roles = &vesselRoles{Generator: solar}
	})
	if code == http.StatusOK {
		t.Fatalf("a solar controller cannot be the generator, body %v", body)
	}
	saved, _ = loadVesselView(settingsPath)
	if saved.Electrical.GeneratorInstance != "3" {
		t.Fatalf("a rejected save must not change the stored value, got %+v", saved.Electrical)
	}
}

func TestElectricalStateReadsTheConfiguredInstances(t *testing.T) {
	vessel := saveElectricalVessel(t, vesselView{
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
	vessel := saveElectricalVessel(t, vesselView{})
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
	vessel := saveElectricalVessel(t, vesselView{Electrical: vesselElectricalSettings{InverterInstance: "9"}})
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
		vessel := saveElectricalVessel(t, vesselView{Electrical: vesselElectricalSettings{InverterInstance: "7"}})
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

func TestElectricalStateVoltageReadoutLabelIsTheItemsName(t *testing.T) {
	seedSelfTree(t, electricalInstancesTree)

	vessel := saveElectricalVessel(t, vesselView{
		Electrical: vesselElectricalSettings{VoltageReadoutBattery: "51"},
		Batteries:  []vesselBatterySetting{{Instance: "51", Name: "Aft House"}},
	})
	state, _ := fetchSignalKElectricalState(vessel)
	if state.VoltageReadoutV != 26.1 || state.VoltageReadoutLabel != "Aft House" {
		t.Errorf("item name: got %v %q", state.VoltageReadoutV, state.VoltageReadoutLabel)
	}
}

// The label falls back to the name the bus publishes, then "Battery <id>", for
// a battery no item is mapped to.
func TestVoltageReadoutLabelFallsBackToBusNameThenId(t *testing.T) {
	seedSelfTree(t, electricalInstancesTree)
	telemetry := globalSignalKSnapshot.liveSelfTelemetry(time.Now().UTC())
	if got := voltageReadoutLabel(telemetry, vesselView{}, "51"); got != "Bus name" {
		t.Errorf("bus name: got %q", got)
	}
	if got := voltageReadoutLabel(telemetry, vesselView{}, "239"); got != "Battery 239" {
		t.Errorf("id label: got %q", got)
	}
}

func TestElectricalStateVoltageReadoutChosenButSilentIsHiddenNotSubstituted(t *testing.T) {
	vessel := saveElectricalVessel(t, vesselView{
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
	saveElectricalVessel(t, vesselView{
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
	saveElectricalVessel(t, vesselView{})
	resetVesselReadLog()
	resetElectricalUnsetLog()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	seedSelfTree(t, electricalInstancesTree)

	buildElectricalStatePayload()
	buildElectricalStatePayload()
	if n := strings.Count(buf.String(), "not chosen under Settings > SignalK > Roles"); n < 1 {
		t.Fatalf("expected a log line naming what is unset, got %q", buf.String())
	}
	first := strings.Count(buf.String(), "shore charger")
	if first != 1 {
		t.Fatalf("the shore charger line must appear once, appeared %d times in %q", first, buf.String())
	}
}

func TestVesselStateReadsTheConfiguredGenerator(t *testing.T) {
	saveElectricalVessel(t, vesselView{Electrical: vesselElectricalSettings{GeneratorInstance: "3"}})
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
	saveElectricalVessel(t, vesselView{})
	seedSelfTree(t, electricalInstancesTree)
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	state, _ := fetchSignalKVesselState()
	if state.GeneratorState != "" || state.GeneratorManualStart || state.GeneratorRuntime != 0 {
		t.Fatalf("instance 0 must not be guessed, got %+v", state)
	}
}

func TestGeneratorBasePath(t *testing.T) {
	got, problem := generatorBasePath(vesselView{Electrical: vesselElectricalSettings{GeneratorInstance: "3"}})
	if problem != "" || got != "/signalk/v1/api/vessels/self/electrical/generator/3" {
		t.Fatalf("got %q %q", got, problem)
	}
	if _, problem := generatorBasePath(vesselView{}); !strings.Contains(problem, "not chosen under Settings > SignalK > Roles") {
		t.Fatalf("unset: got %q", problem)
	}
	if _, problem := generatorBasePath(vesselView{Electrical: vesselElectricalSettings{GeneratorInstance: "0/../../x"}}); problem == "" {
		t.Fatal("an id with path characters must be refused before it reaches a URL")
	}
}

func TestGeneratorStartAndStopAre409WhenNoGeneratorIsSet(t *testing.T) {
	saveElectricalVessel(t, vesselView{})
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
		if !strings.Contains(body["error"], "not chosen under Settings > SignalK > Roles") {
			t.Fatalf("%s: error %q", name, body["error"])
		}
	}
}

func TestSolarControllersAreNamedByIdFromVesselSettings(t *testing.T) {
	vessel := vesselView{Solar: []vesselSolarSetting{{Instance: "289", Name: "Starboard arch"}}}
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
	if err := os.WriteFile(settingsPath, []byte("roles: [not, a, block]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resetVesselReadLog()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	seedSelfTree(t, `{"electrical":{"solar":{"288":{"panelPower":{"value":100}}}}}`)

	payload := buildSolarStatePayload()
	if !strings.Contains(buf.String(), "vessel unreadable") {
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

	got := vesselDiscovery(globalSignalKSnapshot, vesselView{}, nil)

	metric := func(r discoveryRow, label string) string {
		for _, m := range r.Metrics {
			if m.Label == label {
				return m.Text
			}
		}
		return ""
	}
	ids := func(rows []discoveryRow) string {
		var out []string
		for _, r := range rows {
			out = append(out, r.Instance)
		}
		return strings.Join(out, ",")
	}

	generators := discoveryGroupRows(got, "generator")
	if ids(generators) != "0,7,12" {
		t.Fatalf("generators: %+v", generators)
	}
	if metric(generators[2], "State") != "running" {
		t.Errorf("generator 12 state: %+v", generators[2].Metrics)
	}
	if metric(generators[1], "State") != "" {
		t.Errorf("a stale generator state must not read as live, got %q", metric(generators[1], "State"))
	}
	ac := discoveryGroupRows(got, "ac")
	if ids(ac) != "5,40" {
		t.Fatalf("ac: %+v", ac)
	}
	if metric(ac[0], "Real power") != "1250 W" || metric(ac[1], "Real power") != "15 W" {
		t.Errorf("ac power: %+v", ac)
	}
	chargers := discoveryGroupRows(got, "charger")
	if ids(chargers) != "0,276" {
		t.Fatalf("chargers: %+v", chargers)
	}
	if metric(chargers[1], "Output") != "20.0 A" || metric(chargers[1], "AC in") != "6.0 A" {
		t.Errorf("charger 276: %+v", chargers[1])
	}
	if metric(chargers[0], "AC in") != "" {
		t.Errorf("charger 0 has no AC input reading, got %q", metric(chargers[0], "AC in"))
	}
	inverters := discoveryGroupRows(got, "inverter")
	if ids(inverters) != "5,276" {
		t.Fatalf("inverters: %+v", inverters)
	}
	if metric(inverters[0], "AC out") != "420 W" || metric(inverters[1], "AC out") != "5 W" {
		t.Errorf("inverter output: %+v", inverters)
	}
	solar := discoveryGroupRows(got, "solar")
	if ids(solar) != "288,289,291" {
		t.Fatalf("solar: %+v", solar)
	}
	if metric(solar[1], "Panel power") != "210 W" {
		t.Errorf("solar 289: %+v", solar[1])
	}
	if metric(solar[2], "Panel power") != "" {
		t.Errorf("a stale solar reading must not read as live, got %q", metric(solar[2], "Panel power"))
	}
}

func TestVesselDiscoveryListsAreEmptyArraysNotNull(t *testing.T) {
	seedSelfTree(t, `{}`)
	body, err := json.Marshal(vesselDiscovery(globalSignalKSnapshot, vesselView{}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"rows":null`) || strings.Contains(string(body), `"metrics":null`) {
		t.Errorf("rows and metrics must serialise as [], got %s", body)
	}
}

func TestElectricalPayloadSaysWhetherAGeneratorIsSetUp(t *testing.T) {
	seedSelfTree(t, electricalInstancesTree)

	saveElectricalVessel(t, vesselView{})
	if got := buildElectricalStatePayload()["generator_configured"]; got != false {
		t.Fatalf("unset: got %v", got)
	}

	saveElectricalVessel(t, vesselView{Electrical: vesselElectricalSettings{GeneratorInstance: "0"}})
	if got := buildElectricalStatePayload()["generator_configured"]; got != true {
		t.Fatalf("instance 0 is a real choice: got %v", got)
	}
}

func TestElectricalPayloadGeneratorConfiguredDoesNotDependOnTheFeed(t *testing.T) {
	saveElectricalVessel(t, vesselView{Electrical: vesselElectricalSettings{GeneratorInstance: "3"}})
	withGlobalSnapshot(t, newSignalKSnapshot()) // SignalK has dropped: no self data
	if got := buildElectricalStatePayload()["generator_configured"]; got != true {
		t.Fatalf("a dropped feed must not read as not set up, got %v", got)
	}
}

func TestElectricalPayloadGeneratorConfiguredIsUnknownWhenSettingsAreUnreadable(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := os.WriteFile(settingsPath, []byte("roles: [not, a, block]\n"), 0o600); err != nil {
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

func TestVesselDiscoveryOffersOnlyACNodesThatCarryRealPower(t *testing.T) {
	seedSelfTree(t, `{"electrical":{"ac":{
		"5":{"phase":{"A":{"realPower":{"value":10}}}},
		"6":{"phaseA":{"realPower":{"value":null}}},
		"7":{"realPower":{"value":3}},
		"loads":{"total":{"power":{"value":99}}},
		"9":{"phase":{"A":{"current":{"value":2}}}}}}}`)
	got := vesselDiscovery(globalSignalKSnapshot, vesselView{}, nil)
	var ids []string
	for _, a := range discoveryGroupRows(got, "ac") {
		ids = append(ids, a.Instance)
	}
	if strings.Join(ids, ",") != "5,6,7" {
		t.Fatalf("got %v, want 5,6,7", ids)
	}
	for _, g := range got.Groups {
		if g.Kind == "ac" && g.Mappable {
			t.Fatalf("an AC input is not something an item can own")
		}
	}
}

func TestLoadVesselRolesTrimsTheACInstance(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	if err := os.WriteFile(settingsPath, []byte("roles:\n  generator_ac_instance: ' 5 '\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := loadVesselView(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if v.Electrical.GeneratorACInstance != "5" {
		t.Fatalf("got %+v", v.Electrical)
	}
}
