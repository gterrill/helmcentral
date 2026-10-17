package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeVesselTestSettings(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	return path
}

// TestLoadVesselSettingsMissingFileIsZeroValue asserts a fresh install (no
// settings.yaml at all) loads to "not set up": no engines, no house bank --
// never an error, and never a guessed default.
func TestLoadVesselSettingsMissingFileIsZeroValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	v, err := loadVesselView(path)
	if err != nil {
		t.Fatalf("unexpected error for a missing file: %v", err)
	}
	if len(v.Engines) != 0 {
		t.Fatalf("expected no engines, got %+v", v.Engines)
	}
	if v.HouseBank != nil {
		t.Fatalf("expected a nil house bank, got %+v", v.HouseBank)
	}
}

// TestLoadVesselSettingsNoVesselKeyIsZeroValue asserts a settings.yaml with
// other blocks (boat, signalk) but no "vessel:" key at all -- e.g. every
// existing installation upgrading into this cycle -- also loads to "not set
// up" rather than erroring.
func TestLoadVesselSettingsNoVesselKeyIsZeroValue(t *testing.T) {
	path := writeVesselTestSettings(t, "boat:\n  model: Test Boat\n")
	v, err := loadVesselView(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v.Engines) != 0 || v.HouseBank != nil {
		t.Fatalf("expected zero value, got %+v", v)
	}
}

// TestVesselFixtureRoundTrips asserts engines and a complete house bank
// survive being set up as items and roles and read back as the vessel.
func TestVesselFixtureRoundTrips(t *testing.T) {
	path := writeVesselTestSettings(t, "boat:\n  model: Test Boat\n")

	v := vesselView{
		Engines: []vesselEngineSetting{
			{Instance: "port", Name: "Port"},
			{Instance: "starboard", Name: "Starboard"},
		},
		HouseBank: &vesselHouseBankSetting{
			Path:       "electrical.batteries.512",
			CapacityAh: 400,
			Cells:      8,
		},
	}
	if err := saveVesselFixture(t, path, v); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := loadVesselView(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Engines) != 2 || got.Engines[0].Instance != "port" || got.Engines[1].Instance != "starboard" || got.Engines[0].EquipmentID == "" {
		t.Fatalf("engines did not round-trip: %+v", got.Engines)
	}
	if got.HouseBank == nil || got.HouseBank.Path != "electrical.batteries.512" || got.HouseBank.CapacityAh != 400 || got.HouseBank.Cells != 8 || got.HouseBank.EquipmentID == "" {
		t.Fatalf("house bank did not round-trip: %+v", got.HouseBank)
	}
}

// Writing the roles block does not disturb sibling settings.yaml sections.
func TestVesselFixturePreservesOtherTopLevelKeys(t *testing.T) {
	path := writeVesselTestSettings(t, "boat:\n  model: Test Boat\n  vessel_prefix: M/V\n")

	if err := saveVesselFixture(t, path, vesselView{}); err != nil {
		t.Fatalf("save: %v", err)
	}

	raw, err := readSettings(path)
	if err != nil {
		t.Fatalf("read raw settings: %v", err)
	}
	boat, ok := raw["boat"].(map[string]any)
	if !ok {
		t.Fatalf("expected boat block to survive, got %+v", raw)
	}
	if coerceString(boat["model"]) != "Test Boat" {
		t.Fatalf("boat.model was disturbed: %+v", boat)
	}
}

// A roles block that does not parse surfaces an error rather than silently
// falling back to "not set up" -- AGENTS.md's fail-fast policy: a corrupt
// section must not be indistinguishable from a genuinely fresh install.
func TestLoadVesselRolesMalformedFailsFast(t *testing.T) {
	path := writeVesselTestSettings(t, "roles:\n  house_bank: [not, text]\n")
	if _, err := loadVesselView(path); err == nil {
		t.Fatalf("expected a malformed roles block to surface an error")
	}
}

// A role that names an item that has lost its source (or been deleted) leaves
// that figure unset, with one clear log line, instead of reading another bank.
func TestRoleWithoutAMatchingItemResolvesToNothing(t *testing.T) {
	items := []sourceItem{{ID: "gen", Name: "Genset", Source: "electrical.generator.0", Group: sourceGenerator, Instance: "0"}}
	v := resolveVesselView(vesselRoles{HouseBank: "gone", ShoreCharger: "gen"}, items)
	if v.HouseBank != nil {
		t.Fatalf("a deleted item must not leave a house bank, got %+v", v.HouseBank)
	}
	if v.Electrical.ShoreChargerInstance != "" {
		t.Fatalf("a generator item cannot be the shore charger, got %+v", v.Electrical)
	}
}

func TestTankNamesComeFromMappedItems(t *testing.T) {
	items := []sourceItem{
		{ID: "t1", Name: "PORT FWD", Source: "tanks.fuel.5", Group: sourceTank, Instance: "fuel.5"},
		{ID: "t2", Name: "PORT", Source: "tanks.blackWater.1", Group: sourceTank, Instance: "blackWater.1"},
	}
	v := resolveVesselView(vesselRoles{}, items)
	if v.tankName("fuel", "5") != "PORT FWD" || v.tankName("blackWater", "1") != "PORT" {
		t.Fatalf("got %+v", v.Tanks)
	}
	if v.tankName("fuel", "6") != "" {
		t.Fatalf("a tank no item owns has no name")
	}
}

// --- House bank capacity: the 1440 default is gone -------------------------

func capacityFromView(t *testing.T, v vesselView) float64 {
	t.Helper()
	path := writeVesselTestSettings(t, "boat:\n  model: Test Boat\n")
	if err := saveVesselFixture(t, path, v); err != nil {
		t.Fatal(err)
	}
	vessel, err := loadVesselView(path)
	if err != nil {
		t.Fatal(err)
	}
	state := electricalStateFromTelemetry(fakeTelemetry{}, electricalStateData{BatteryCapacityAh: -1, BatterySocPercent: -1}, vessel)
	return state.BatteryCapacityAh
}

// Capacity comes from the house bank item, not boat.house_battery_capacity_ah
// (the operator enters it on the bank's equipment item).
func TestHouseBankCapacityReadsFromTheBankItem(t *testing.T) {
	got := capacityFromView(t, vesselView{HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.512", CapacityAh: 600}})
	if got != 600 {
		t.Fatalf("capacity: got %v, want 600", got)
	}
}

// An unconfigured house bank reports "not set" (-1), never a guessed number.
func TestHouseBankCapacityUnsetIsNegativeOne(t *testing.T) {
	if got := capacityFromView(t, vesselView{}); got != -1 {
		t.Fatalf("capacity with no house bank: got %v, want -1", got)
	}
}

func TestRoleNamingAnItemWithoutASourceIsFlaggedUnresolved(t *testing.T) {
	v := resolveVesselView(vesselRoles{HouseBank: "gone"}, nil)
	if v.HouseBank != nil || !v.HouseBankUnresolved {
		t.Fatalf("got bank %+v unresolved %v", v.HouseBank, v.HouseBankUnresolved)
	}
	if v := resolveVesselView(vesselRoles{}, nil); v.HouseBankUnresolved {
		t.Fatal("a blank role is not unresolved")
	}
}

// A stale generator AC input saved before the generator was set to none must
// not keep feeding generator power.
func TestGeneratorNoneIgnoresAStaleACInput(t *testing.T) {
	v := resolveVesselView(vesselRoles{Generator: roleNone, GeneratorACInstance: "3"}, nil)
	if v.Electrical.GeneratorACInstance != "" {
		t.Fatalf("got AC input %q with no generator", v.Electrical.GeneratorACInstance)
	}
	if v := resolveVesselView(vesselRoles{GeneratorACInstance: "3"}, nil); v.Electrical.GeneratorACInstance != "3" {
		t.Fatalf("an unset generator keeps the AC input, got %q", v.Electrical.GeneratorACInstance)
	}
}
