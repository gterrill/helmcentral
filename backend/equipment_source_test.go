package main

import (
	"errors"
	"net/http"
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

// ── swapping a source between two items ──────────────────────────────────

func twoTanks(t *testing.T) (*documentStore, equipmentItem, equipmentItem) {
	t.Helper()
	store := newTestDocumentStore(t)
	a, err := store.CreateEquipment(equipmentItem{Name: "Fuel starboard", SignalKSource: "tanks.fuel.2"})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := store.CreateEquipment(equipmentItem{Name: "Fuel port", SignalKSource: "tanks.fuel.5"})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	return store, a, b
}

func TestTakingASourceFromAnotherItemSwapsWhenTheKindsMatch(t *testing.T) {
	store, a, b := twoTanks(t)
	a.SignalKSource = "tanks.fuel.5"
	updated, err := store.UpdateEquipmentTakingSource(a.ID, a, b.ID, "swap")
	if err != nil {
		t.Fatalf("swap: %v", err)
	}
	if updated.SignalKSource != "tanks.fuel.5" {
		t.Fatalf("a: %+v", updated)
	}
	got, err := store.GetEquipment(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SignalKSource != "tanks.fuel.2" {
		t.Fatalf("b should hold a's old source, got %q", got.SignalKSource)
	}
}

func TestTakingASourceMovesItWhenTheTakerHasNoSource(t *testing.T) {
	store, _, b := twoTanks(t)
	c, err := store.CreateEquipment(equipmentItem{Name: "Spare tank"})
	if err != nil {
		t.Fatal(err)
	}
	c.SignalKSource = "tanks.fuel.5"
	if _, err := store.UpdateEquipmentTakingSource(c.ID, c, b.ID, "move"); err != nil {
		t.Fatalf("move: %v", err)
	}
	got, _ := store.GetEquipment(b.ID)
	if got.SignalKSource != "" {
		t.Fatalf("b should have none, got %q", got.SignalKSource)
	}
}

func TestTakingASourceMovesItWhenTheOldSourceIsADifferentKind(t *testing.T) {
	store := newTestDocumentStore(t)
	engine, _ := store.CreateEquipment(equipmentItem{Name: "Port engine", SignalKSource: "propulsion.port"})
	tank, _ := store.CreateEquipment(equipmentItem{Name: "Fuel port", SignalKSource: "tanks.fuel.5"})
	engine.SignalKSource = "tanks.fuel.5"
	if _, err := store.UpdateEquipmentTakingSource(engine.ID, engine, tank.ID, "move"); err != nil {
		t.Fatalf("take: %v", err)
	}
	got, _ := store.GetEquipment(tank.ID)
	if got.SignalKSource != "" {
		t.Fatalf("an engine source must not land on a tank, got %q", got.SignalKSource)
	}
}

func TestTakingASourceRefusesAStaleOwner(t *testing.T) {
	store, a, b := twoTanks(t)
	b.SignalKSource = ""
	if _, err := store.UpdateEquipment(b.ID, b); err != nil {
		t.Fatal(err)
	}
	a.SignalKSource = "tanks.fuel.5"
	_, err := store.UpdateEquipmentTakingSource(a.ID, a, b.ID, "swap")
	if !errors.Is(err, errEquipmentSourceMoved) {
		t.Fatalf("expected the source-moved error, got %v", err)
	}
	got, _ := store.GetEquipment(a.ID)
	if got.SignalKSource != "tanks.fuel.2" {
		t.Fatalf("nothing should have changed, a has %q", got.SignalKSource)
	}
}

func TestTakingASourceFromYourselfIsRefused(t *testing.T) {
	store, a, _ := twoTanks(t)
	_, err := store.UpdateEquipmentTakingSource(a.ID, a, a.ID, "swap")
	if !errors.Is(err, errEquipmentSourceInvalid) {
		t.Fatalf("expected invalid, got %v", err)
	}
}

func TestAnOrdinaryUpdateStillRefusesATakenSource(t *testing.T) {
	store, a, _ := twoTanks(t)
	a.SignalKSource = "tanks.fuel.5"
	_, err := store.UpdateEquipment(a.ID, a)
	if !isEquipmentSourceTaken(err) {
		t.Fatalf("expected taken, got %v", err)
	}
	want := `tanks.fuel.5 is already on "Fuel port". To give it to another item, pick it on that item's Source list.`
	if err.Error() != want {
		t.Fatalf("message: %q", err.Error())
	}
}

func TestSwappingSourcesMovesTheTankAlarmRules(t *testing.T) {
	tankTest(t)
	a := newTankItem(t, "Fuel starboard", "tanks.fuel.2", 0, 0, false)
	b := newTankItem(t, "Fuel port", "tanks.fuel.5", 0, 0, false)
	body := `{"name":"Fuel starboard","signalk_source":"tanks.fuel.5","take_source_from":"` + b + `","take_source_mode":"swap"}`
	c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/equipment/"+a, body, a)
	if err := updateEquipmentHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	paths := map[string]string{}
	for _, r := range tankDerivedAlarmRules() {
		paths[r.ID] = r.Path
	}
	if paths["tank:"+a] != "tanks.fuel.5.currentLevel" || paths["tank:"+b] != "tanks.fuel.2.currentLevel" {
		t.Fatalf("rules did not follow the swap: %+v", paths)
	}
}

func TestAStaleTakeIsAConflictOverHTTP(t *testing.T) {
	tankTest(t)
	a := newTankItem(t, "Fuel starboard", "tanks.fuel.2", 0, 0, false)
	b := newTankItem(t, "Fuel port", "tanks.fuel.5", 0, 0, false)
	body := `{"name":"Fuel starboard","signalk_source":"tanks.fuel.9","take_source_from":"` + b + `","take_source_mode":"swap"}`
	c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/equipment/"+a, body, a)
	if err := updateEquipmentHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
}

func TestASwapKeepsEachItemsOwnFigures(t *testing.T) {
	store := newTestDocumentStore(t)
	a, _ := store.CreateEquipment(equipmentItem{Name: "Fuel starboard", SignalKSource: "tanks.fuel.2", TankWarnPercent: 25, TankAlarmPercent: 12})
	b, _ := store.CreateEquipment(equipmentItem{Name: "Fuel port", SignalKSource: "tanks.fuel.5", TankWarnPercent: 30, TankAlarmsOff: true})
	a.SignalKSource = "tanks.fuel.5"
	if _, err := store.UpdateEquipmentTakingSource(a.ID, a, b.ID, "swap"); err != nil {
		t.Fatal(err)
	}
	gotA, _ := store.GetEquipment(a.ID)
	gotB, _ := store.GetEquipment(b.ID)
	if gotA.TankWarnPercent != 25 || gotA.TankAlarmPercent != 12 || gotB.TankWarnPercent != 30 || !gotB.TankAlarmsOff {
		t.Fatalf("figures must stay with their items: a=%+v b=%+v", gotA, gotB)
	}
}

func TestAMoveClearsTheOwnersFigures(t *testing.T) {
	store := newTestDocumentStore(t)
	b, _ := store.CreateEquipment(equipmentItem{Name: "Fuel port", SignalKSource: "tanks.fuel.5", TankWarnPercent: 30, TankAlarmsOff: true})
	c, _ := store.CreateEquipment(equipmentItem{Name: "Spare tank"})
	c.SignalKSource = "tanks.fuel.5"
	if _, err := store.UpdateEquipmentTakingSource(c.ID, c, b.ID, "move"); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetEquipment(b.ID)
	if got.SignalKSource != "" || got.TankWarnPercent != 0 || got.TankAlarmsOff {
		t.Fatalf("owner after a move: %+v", got)
	}
}

func TestATakeWhoseModeDiffersFromTheServersIsRefused(t *testing.T) {
	store, a, b := twoTanks(t)
	a.SignalKSource = "tanks.fuel.5"
	// The kinds match, so the server computes a swap; the client said move.
	_, err := store.UpdateEquipmentTakingSource(a.ID, a, b.ID, "move")
	if !errors.Is(err, errEquipmentSourceChanged) {
		t.Fatalf("expected the source-changed error, got %v", err)
	}
	got, _ := store.GetEquipment(a.ID)
	if got.SignalKSource != "tanks.fuel.2" {
		t.Fatalf("nothing should have changed, a has %q", got.SignalKSource)
	}
	// And the other way round.
	engine, _ := store.CreateEquipment(equipmentItem{Name: "Port engine", SignalKSource: "propulsion.port"})
	engine.SignalKSource = "tanks.fuel.5"
	if _, err := store.UpdateEquipmentTakingSource(engine.ID, engine, b.ID, "swap"); !errors.Is(err, errEquipmentSourceChanged) {
		t.Fatalf("a client swap the server would make a move: %v", err)
	}
	if _, err := store.UpdateEquipmentTakingSource(a.ID, a, b.ID, ""); !errors.Is(err, errEquipmentSourceInvalid) {
		t.Fatalf("a missing mode is invalid, got %v", err)
	}
}

func TestDiscoveryTellsWhichFiguresAnOwnerWouldLose(t *testing.T) {
	snapshot := newVesselCandidatesSnapshot(t)
	items := []sourceItem{
		{ID: "e1", Name: "Port engine", Source: "propulsion.port", Group: sourceEngine, Instance: "port"},
		{ID: "b1", Name: "House bank", Source: "electrical.batteries.0", Group: sourceBattery, Instance: "0", CapacityAh: 400},
	}
	for _, r := range discoveryGroupRows(vesselDiscovery(snapshot, vesselView{}, items, sourceRecord{}), "engine") {
		if r.Item != nil && r.Item.Figures != "" {
			t.Errorf("an engine has no figures: %+v", r.Item)
		}
	}
	found := false
	for _, r := range discoveryGroupRows(vesselDiscovery(snapshot, vesselView{}, items, sourceRecord{}), "battery") {
		if r.Item != nil && r.Item.ID == "b1" {
			found = true
			if r.Item.Figures != "bank" {
				t.Errorf("figures: %+v", r.Item)
			}
		}
	}
	if !found {
		t.Skip("fixture does not list battery 0")
	}
}

// Tanks of one kind are listed by name, so port and starboard sit together
// whatever order the instruments numbered them in.
func TestTanksWithinACategoryAreOrderedByLabel(t *testing.T) {
	telemetry := fakeTelemetry{Samples: map[string]telemetrySample{
		"tanks.fuel.2.currentLevel": {Present: true, Value: 0.5},
		"tanks.fuel.3.currentLevel": {Present: true, Value: 0.5},
		"tanks.fuel.5.currentLevel": {Present: true, Value: 0.5},
		"tanks.fuel.6.currentLevel": {Present: true, Value: 0.5},
	}, Timestamps: map[string]time.Time{}}
	overrides := map[string]string{
		"fuel.2": "Fuel Stbd Aft",
		"fuel.3": "fuel Port Fwd",
		"fuel.5": "Fuel Stbd Fwd",
		"fuel.6": "Fuel Port Aft",
	}
	tanks, _ := tanksFromTelemetry(telemetry, overrides, time.Now())
	want := []string{"Fuel Port Aft", "fuel Port Fwd", "Fuel Stbd Aft", "Fuel Stbd Fwd"}
	if len(tanks) != len(want) {
		t.Fatalf("tanks: %+v", tanks)
	}
	for i, label := range want {
		if tanks[i].Label != label {
			t.Fatalf("position %d: want %q, got %+v", i, label, tanks)
		}
	}
}
