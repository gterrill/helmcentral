package main

import (
	"errors"
	"testing"
	"time"
)

func TestParseSourceRecognisesTheEightShapes(t *testing.T) {
	cases := []struct {
		source   string
		group    sourceGroup
		instance string
		ok       bool
	}{
		{"propulsion.port", sourceEngine, "port", true},
		{"electrical.batteries.512", sourceBattery, "512", true},
		{"electrical.alternator.12", sourceAlternator, "12", true},
		{"electrical.solar.289", sourceSolar, "289", true},
		{"tanks.fuel.5", sourceTank, "fuel.5", true},
		{"tanks.blackWater.1", sourceTank, "blackWater.1", true},
		{"electrical.generator.0", sourceGenerator, "0", true},
		{"electrical.chargers.276", sourceCharger, "276", true},
		{"electrical.inverters.0", sourceInverter, "0", true},
		{"propulsion.port.revolutions", "", "", false},
		{"electrical.batteries", "", "", false},
		{"tanks.fuel", "", "", false},
		{"navigation.speedOverGround", "", "", false},
		{"electrical.ac.5", "", "", false},
		{"propulsion.", "", "", false},
	}
	for _, c := range cases {
		group, instance, ok := parseSource(c.source)
		if ok != c.ok || group != c.group || instance != c.instance {
			t.Errorf("%s: got %q %q %v", c.source, group, instance, ok)
		}
	}
}

func TestSourceHourMeterIsDerivedFromTheSource(t *testing.T) {
	cases := map[string]string{
		"propulsion.port":        "propulsion.port.runTime",
		"electrical.generator.3": "electrical.generator.3.runtime",
		"electrical.batteries.1": "",
		"tanks.fuel.2":           "",
		"":                       "",
		"not a source":           "",
	}
	for source, want := range cases {
		if got := sourceHourMeterPath(source); got != want {
			t.Errorf("%q: got %q, want %q", source, got, want)
		}
	}
}

func TestEquipmentSourceIsStoredDerivedAndUnique(t *testing.T) {
	store := newTestDocumentStore(t)

	engine, err := store.CreateEquipment(equipmentItem{Name: "Port engine", SignalKSource: " propulsion.port "})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	if engine.SignalKSource != "propulsion.port" || engine.SourceKind != "engine" || engine.HourMeterSource != "propulsion.port.runTime" || engine.Category != "mechanical" {
		t.Fatalf("an engine item: %+v", engine)
	}

	_, err = store.CreateEquipment(equipmentItem{Name: "Second port engine", SignalKSource: "propulsion.port"})
	var taken *equipmentSourceTakenError
	if !errors.As(err, &taken) || taken.ItemName != "Port engine" || taken.ItemID != engine.ID {
		t.Fatalf("a source belongs to one item, got %v", err)
	}

	// An item keeps its own source across a save.
	engine.Notes = "serviced"
	if _, err := store.UpdateEquipment(engine.ID, engine); err != nil {
		t.Fatalf("saving an item with its own source: %v", err)
	}

	// Unassigning frees the source for another item.
	engine.SignalKSource = ""
	if _, err := store.UpdateEquipment(engine.ID, engine); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if _, err := store.CreateEquipment(equipmentItem{Name: "Second port engine", SignalKSource: "propulsion.port"}); err != nil {
		t.Fatalf("a freed source can be assigned again: %v", err)
	}
}

func TestEquipmentSourceMustBeOneTheBoatCanReport(t *testing.T) {
	store := newTestDocumentStore(t)
	for _, source := range []string{"propulsion.port.runTime", "navigation.position", "free text", "electrical.ac.5"} {
		_, err := store.CreateEquipment(equipmentItem{Name: "X", SignalKSource: source})
		if !errors.Is(err, errEquipmentSourceInvalid) {
			t.Errorf("%q: expected the source to be refused, got %v", source, err)
		}
	}
}

func TestBankFiguresBelongToABatteryItem(t *testing.T) {
	store := newTestDocumentStore(t)
	if _, err := store.CreateEquipment(equipmentItem{Name: "Engine", SignalKSource: "propulsion.port", CapacityAh: 100}); !errors.Is(err, errEquipmentSourceInvalid) {
		t.Fatalf("capacity on an engine should be refused, got %v", err)
	}
	if _, err := store.CreateEquipment(equipmentItem{Name: "Unmapped", Cells: 8}); !errors.Is(err, errEquipmentSourceInvalid) {
		t.Fatalf("cells on an item with no source should be refused, got %v", err)
	}
	if _, err := store.CreateEquipment(equipmentItem{Name: "Bank", SignalKSource: "electrical.batteries.1", CapacityAh: -5}); !errors.Is(err, errEquipmentSourceInvalid) {
		t.Fatalf("a negative capacity should be refused, got %v", err)
	}
	bank, err := store.CreateEquipment(equipmentItem{Name: "Bank", SignalKSource: "electrical.batteries.1", CapacityAh: 1440, Cells: 8, WarnVoltage: 28.4})
	if err != nil {
		t.Fatalf("a battery item carries its bank figures: %v", err)
	}
	if bank.CapacityAh != 1440 || bank.Cells != 8 || bank.WarnVoltage != 28.4 || bank.SourceKind != "battery" {
		t.Fatalf("bank: %+v", bank)
	}
}

// The view names a source by the item that owns it, and reads the bank's
// figures off that item.
func TestViewReadsNamesAndBankFiguresFromItems(t *testing.T) {
	store := vesselFixtureStore(t)
	mk := func(item equipmentItem) string {
		created, err := store.CreateEquipment(item)
		if err != nil {
			t.Fatal(err)
		}
		return created.ID
	}
	bank := mk(equipmentItem{Name: "Lithium bank", SignalKSource: "electrical.batteries.512", CapacityAh: 1440, Cells: 8, HighVoltage: 28.9})
	mk(equipmentItem{Name: "Port array", SignalKSource: "electrical.solar.289"})
	mk(equipmentItem{Name: "PORT FWD", SignalKSource: "tanks.fuel.5"})
	engine := mk(equipmentItem{Name: "Port engine", SignalKSource: "propulsion.port"})

	items, err := store.SourceItems()
	if err != nil {
		t.Fatal(err)
	}
	v := resolveVesselView(vesselRoles{HouseBank: bank}, items)
	if v.HouseBank == nil || v.HouseBank.Path != "electrical.batteries.512" || v.HouseBank.CapacityAh != 1440 || v.HouseBank.Cells != 8 || v.HouseBank.HighVoltage != 28.9 || v.HouseBank.EquipmentID != bank {
		t.Fatalf("house bank: %+v", v.HouseBank)
	}
	if v.solarName("289") != "Port array" || v.tankName("fuel", "5") != "PORT FWD" {
		t.Fatalf("names: %+v %+v", v.Solar, v.Tanks)
	}
	if len(v.Engines) != 1 || v.Engines[0] != (vesselEngineSetting{Instance: "port", Name: "Port engine", EquipmentID: engine}) {
		t.Fatalf("engines: %+v", v.Engines)
	}
	if len(v.Batteries) != 1 || v.Batteries[0].Name != "Lithium bank" {
		t.Fatalf("batteries: %+v", v.Batteries)
	}
}

// The tanks tile lists a tank under the name of the item that owns it.
func TestTanksAreNamedByTheirItems(t *testing.T) {
	telemetry := fakeTelemetry{Samples: map[string]telemetrySample{
		"tanks.fuel.5.currentLevel": {Present: true, Value: 0.5},
		"tanks.fuel.6.currentLevel": {Present: true, Value: 0.4},
	}, Timestamps: map[string]time.Time{}}
	v := resolveVesselView(vesselRoles{}, []sourceItem{{ID: "t", Name: "PORT FWD", Source: "tanks.fuel.5", Group: sourceTank, Instance: "fuel.5"}})
	tanks, _ := tanksFromTelemetry(telemetry, v.Tanks, time.Now())
	if len(tanks) != 1 || tanks[0].Label != "PORT FWD" || tanks[0].ID != "fuel.5" {
		t.Fatalf("only the tank an item owns is listed, under the item's name: %+v", tanks)
	}
}
