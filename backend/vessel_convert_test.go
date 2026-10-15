package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legacySettingsFixture is a settings.yaml shaped like the boat's, with the old
// vessel: block fully filled in and the tank labels the Signal K section used to
// hold. Keys are the ones the old Settings -> Vessel page wrote.
func legacySettingsFixture(portID, stbdID, bankID, genID string) string {
	return fmt.Sprintf(`boat:
    model: Test Boat
    vessel_prefix: M/V
signalk:
    address: 127.0.0.1
    port: 3000
ui:
    tide_provider: bom
    tank_labels:
        blackWater.1: PORT
        fuel.2: STBD FWD
        fuel.5: PORT FWD
vessel:
    engines:
        - instance: port
          name: Port
          equipment_id: %s
        - instance: starboard
          name: Starboard
          equipment_id: %s
        - instance: centre
          name: Centre
          equipment_id: ""
    house_bank:
        path: electrical.batteries.512
        equipment_id: %s
        capacity_ah: 1440
        cells: 8
        warn_voltage: 28.4
    batteries:
        - instance: "3"
          name: Port Engine Starter Battery
    alternators:
        port_instance: "2"
        starboard_instance: "12"
    electrical:
        generator_instance: "3"
        generator_ac_instance: "5"
        shore_charger_instance: "276"
        inverter_instance: "5"
        voltage_readout_battery: "239"
    solar:
        - instance: "289"
          name: Port array
units: metric
`, portID, stbdID, bankID)
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

type convertFixture struct {
	store                        *documentStore
	path                         string
	port, stbd, bank, gen, spare string
}

func newConvertFixture(t *testing.T) convertFixture {
	t.Helper()
	store := vesselFixtureStore(t)
	mk := func(name string) string {
		item, err := store.CreateEquipment(equipmentItem{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		return item.ID
	}
	f := convertFixture{store: store, port: mk("Port engine"), stbd: mk("Starboard engine"), bank: mk("Lithium bank"), gen: mk("Genset"), spare: mk("Spare impeller")}
	// The genset had a hand-typed hour meter and no vessel entry; the spare
	// has a path nothing can cover.
	if _, err := store.db.Exec(`UPDATE equipment SET hour_meter_path = 'electrical.generator.3.runtime', category = 'mechanical' WHERE id = ?`, f.gen); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE equipment SET hour_meter_path = 'watermaker.runtime', category = 'mechanical' WHERE id = ?`, f.spare); err != nil {
		t.Fatal(err)
	}
	f.path = filepath.Join(t.TempDir(), "settings.yaml")
	if err := os.WriteFile(f.path, []byte(legacySettingsFixture(f.port, f.stbd, f.bank, f.gen)), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SETTINGS_FILE", f.path)
	return f
}

func TestConvertLegacyVesselMapsItemsAndWritesRoles(t *testing.T) {
	f := newConvertFixture(t)
	withTempAlarmRules(t)
	logs := captureLog(t)

	// A band rule on the old house bank's state of charge, and one on another bank.
	soc, err := createAlarmRule(alarmRule{Label: "House low", Path: "electrical.batteries.512.capacity.stateOfCharge", Op: "below", Value: 0.2, State: "warn", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	other, err := createAlarmRule(alarmRule{Label: "Starter low", Path: "electrical.batteries.3.capacity.stateOfCharge", Op: "below", Value: 0.5, State: "warn", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	if err := convertLegacyVessel(f.path, f.store); err != nil {
		t.Fatalf("convert: %v", err)
	}

	source := func(id string) string {
		item, err := f.store.GetEquipment(id)
		if err != nil {
			t.Fatal(err)
		}
		return item.SignalKSource
	}
	if got := source(f.port); got != "propulsion.port" {
		t.Errorf("port engine source = %q", got)
	}
	if got := source(f.stbd); got != "propulsion.starboard" {
		t.Errorf("starboard engine source = %q", got)
	}
	if got := source(f.bank); got != "electrical.batteries.512" {
		t.Errorf("house bank source = %q", got)
	}
	bank, _ := f.store.GetEquipment(f.bank)
	if bank.CapacityAh != 1440 || bank.Cells != 8 || bank.WarnVoltage != 28.4 || bank.HighVoltage != 0 {
		t.Errorf("bank figures should move onto the item: %+v", bank)
	}
	if bank.Category != "general" {
		t.Errorf("a battery has no hour meter: %q", bank.Category)
	}
	// A hand-typed hour meter that named the generator's runtime counter becomes its source.
	gen, _ := f.store.GetEquipment(f.gen)
	if gen.SignalKSource != "electrical.generator.3" || gen.HourMeterSource != "electrical.generator.3.runtime" || gen.Category != "mechanical" {
		t.Errorf("genset: %+v", gen)
	}
	// One no source covers is retired and said so.
	spare, _ := f.store.GetEquipment(f.spare)
	if spare.SignalKSource != "" || spare.Category != "general" {
		t.Errorf("spare: %+v", spare)
	}
	var leftover int
	if err := f.store.db.QueryRow(`SELECT COUNT(*) FROM equipment WHERE hour_meter_path <> ''`).Scan(&leftover); err != nil || leftover != 0 {
		t.Errorf("hour meter paths should all be retired, %d left (%v)", leftover, err)
	}

	roles, err := loadVesselRoles(f.path)
	if err != nil {
		t.Fatal(err)
	}
	want := vesselRoles{HouseBank: f.bank, GeneratorACInstance: "5"}
	if roles != want {
		t.Errorf("roles = %+v, want %+v (no alternator, charger, inverter or readout item existed to choose)", roles, want)
	}

	settings, _ := readSettings(f.path)
	if _, still := settings["vessel"]; still {
		t.Errorf("the vessel block should be gone")
	}
	ui, _ := settings["ui"].(map[string]any)
	if _, still := ui["tank_labels"]; still {
		t.Errorf("tank labels should be gone")
	}
	if ui["tide_provider"] != "bom" {
		t.Errorf("other ui settings must survive: %+v", ui)
	}
	boat, _ := settings["boat"].(map[string]any)
	if boat["model"] != "Test Boat" {
		t.Errorf("other sections must survive: %+v", boat)
	}

	followed, _ := getAlarmRule(soc.ID)
	if followed.PathRole != alarmPathRoleHouseBankSoC {
		t.Errorf("the band on the house bank should now follow the role: %+v", followed)
	}
	untouched, _ := getAlarmRule(other.ID)
	if untouched.PathRole != "" {
		t.Errorf("a rule on another battery stays on its path: %+v", untouched)
	}

	out := logs.String()
	for _, want := range []string{
		`engine "Centre" (propulsion.centre) had no equipment item`,
		"the port alternator was electrical.alternator.2; no equipment item is assigned to it now",
		"the inverter was electrical.inverters.5",
		`battery "Port Engine Starter Battery" (electrical.batteries.3) has no equipment item`,
		`tank "PORT FWD" (tanks.fuel.5) has no equipment item`,
		`solar controller "Port array" (electrical.solar.289) has no equipment item`,
		"watermaker.runtime",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log should say %q, got:\n%s", want, out)
		}
	}
}

func TestConvertLegacyVesselRunsOnce(t *testing.T) {
	f := newConvertFixture(t)
	withTempAlarmRules(t)
	if err := convertLegacyVessel(f.path, f.store); err != nil {
		t.Fatal(err)
	}

	// The operator has since chosen the port alternator; a second start must not undo it.
	alt, err := f.store.CreateEquipment(equipmentItem{Name: "Port alternator", SignalKSource: "electrical.alternator.2"})
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := readSettings(f.path)
	block, _ := rolesBlockMap(vesselRoles{HouseBank: f.bank, PortAlternator: alt.ID})
	settings["roles"] = block
	if err := writeSettings(f.path, settings); err != nil {
		t.Fatal(err)
	}

	logs := captureLog(t)
	if err := convertLegacyVessel(f.path, f.store); err != nil {
		t.Fatal(err)
	}
	roles, _ := loadVesselRoles(f.path)
	if roles.PortAlternator != alt.ID || roles.HouseBank != f.bank {
		t.Errorf("a second run changed the roles: %+v", roles)
	}
	if strings.Contains(logs.String(), "moved the vessel settings") {
		t.Errorf("a second run has nothing to do: %s", logs.String())
	}
}

// A source another item already owns is never taken from it.
func TestConvertLegacyVesselDoesNotStealASource(t *testing.T) {
	f := newConvertFixture(t)
	withTempAlarmRules(t)
	if _, err := f.store.CreateEquipment(equipmentItem{Name: "Old port engine", SignalKSource: "propulsion.port"}); err != nil {
		t.Fatal(err)
	}
	logs := captureLog(t)

	if err := convertLegacyVessel(f.path, f.store); err != nil {
		t.Fatal(err)
	}
	port, _ := f.store.GetEquipment(f.port)
	if port.SignalKSource != "" {
		t.Errorf("propulsion.port belongs to the other item: %+v", port)
	}
	if !strings.Contains(logs.String(), `propulsion.port already belongs to "Old port engine"`) {
		t.Errorf("the log should name the item holding the source: %s", logs.String())
	}
}

func TestConvertLegacyVesselWithNothingToConvertDoesNothing(t *testing.T) {
	store := vesselFixtureStore(t)
	path := filepath.Join(t.TempDir(), "settings.yaml")
	body := "boat:\n    model: Test Boat\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := convertLegacyVessel(path, store); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != body {
		t.Errorf("settings.yaml should be untouched, got %q", got)
	}
}

// The dev checkout's settings.yaml carries an empty block: engines: [] and
// house_bank: null. It converts to an empty roles block and the key goes.
func TestConvertLegacyVesselEmptyBlock(t *testing.T) {
	store := vesselFixtureStore(t)
	path := filepath.Join(t.TempDir(), "settings.yaml")
	if err := os.WriteFile(path, []byte("boat:\n    model: Test Boat\nvessel:\n    engines: []\n    house_bank: null\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := convertLegacyVessel(path, store); err != nil {
		t.Fatal(err)
	}
	settings, _ := readSettings(path)
	if _, still := settings["vessel"]; still {
		t.Errorf("the vessel block should be gone")
	}
	if _, ok := settings["roles"]; !ok {
		t.Errorf("an empty roles block should be written")
	}
}

func TestConvertLegacyVesselCorruptBlockFailsFast(t *testing.T) {
	store := vesselFixtureStore(t)
	path := filepath.Join(t.TempDir(), "settings.yaml")
	if err := os.WriteFile(path, []byte("vessel:\n    engines: not-a-list\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := convertLegacyVessel(path, store); err == nil {
		t.Fatal("a vessel block that does not parse must stop startup, not read as empty")
	}
	settings, _ := readSettings(path)
	if _, still := settings["vessel"]; !still {
		t.Errorf("a block that failed to convert must be left in place")
	}
}
