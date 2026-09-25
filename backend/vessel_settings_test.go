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
	v, err := loadVesselSettings(path)
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
	v, err := loadVesselSettings(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v.Engines) != 0 || v.HouseBank != nil {
		t.Fatalf("expected zero value, got %+v", v)
	}
}

// TestSaveVesselSettingsRoundTrips asserts engines and a complete house bank
// survive a save/load cycle intact.
func TestSaveVesselSettingsRoundTrips(t *testing.T) {
	path := writeVesselTestSettings(t, "boat:\n  model: Test Boat\n")

	v := vesselSettings{
		Engines: []vesselEngineSetting{
			{Instance: "port", Name: "Port", EquipmentID: "eq-1"},
			{Instance: "starboard", Name: "Starboard", EquipmentID: "eq-2"},
		},
		HouseBank: &vesselHouseBankSetting{
			Path:        "electrical.batteries.512",
			EquipmentID: "eq-3",
			CapacityAh:  400,
			Cells:       8,
		},
	}
	if err := saveVesselSettings(path, v); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := loadVesselSettings(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Engines) != 2 || got.Engines[0].Instance != "port" || got.Engines[1].Instance != "starboard" {
		t.Fatalf("engines did not round-trip: %+v", got.Engines)
	}
	if got.HouseBank == nil || got.HouseBank.Path != "electrical.batteries.512" || got.HouseBank.CapacityAh != 400 || got.HouseBank.Cells != 8 {
		t.Fatalf("house bank did not round-trip: %+v", got.HouseBank)
	}
}

// TestSaveVesselSettingsPreservesOtherTopLevelKeys asserts writing the
// vessel block does not disturb sibling settings.yaml sections -- the same
// "stage by path" discipline the rest of settings.yaml's writers keep.
func TestSaveVesselSettingsPreservesOtherTopLevelKeys(t *testing.T) {
	path := writeVesselTestSettings(t, "boat:\n  model: Test Boat\n  vessel_prefix: M/V\n")

	if err := saveVesselSettings(path, vesselSettings{}); err != nil {
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

// TestLoadVesselSettingsMalformedFailsFast asserts a vessel block that does
// not parse as the expected shape surfaces an error rather than silently
// falling back to "not set up" -- AGENTS.md's fail-fast policy: a corrupt
// section must not be indistinguishable from a genuinely fresh install.
func TestLoadVesselSettingsMalformedFailsFast(t *testing.T) {
	path := writeVesselTestSettings(t, "vessel:\n  engines: \"not a list\"\n")
	if _, err := loadVesselSettings(path); err == nil {
		t.Fatalf("expected a malformed vessel block to surface an error")
	}
}

// --- House bank capacity: the 1440 default is gone -------------------------

// TestLoadHouseBatteryCapacityAhReadsFromVesselHouseBank asserts capacity now
// comes from vessel.house_bank.capacity_ah, not boat.house_battery_capacity_ah
// (the plan's Breaking change: the operator re-enters it under Settings ->
// Vessel -> Power).
func TestLoadHouseBatteryCapacityAhReadsFromVesselHouseBank(t *testing.T) {
	path := writeVesselTestSettings(t, `vessel:
  house_bank:
    path: electrical.batteries.512
    capacity_ah: 600
`)
	if got := loadHouseBatteryCapacityAh(path); got != 600 {
		t.Fatalf("loadHouseBatteryCapacityAh: got %v, want 600", got)
	}
}

// TestLoadHouseBatteryCapacityAhUnsetReturnsNegativeOne asserts an
// unconfigured house bank reports "not set" (-1), never a guessed number --
// this is the leak the plan calls out: defaultHouseBatteryCapacityAh no
// longer exists anywhere in this codebase.
func TestLoadHouseBatteryCapacityAhUnsetReturnsNegativeOne(t *testing.T) {
	path := writeVesselTestSettings(t, "boat:\n  model: Test Boat\n")
	if got := loadHouseBatteryCapacityAh(path); got != -1 {
		t.Fatalf("loadHouseBatteryCapacityAh with no vessel.house_bank: got %v, want -1", got)
	}
}

// TestLoadHouseBatteryCapacityAhIgnoresOldBoatField asserts the old
// boat.house_battery_capacity_ah field, if still present from before the
// upgrade, is no longer read at all -- it is genuinely moved, not merely
// duplicated (AGENTS.md's "no speculative compat fallbacks").
func TestLoadHouseBatteryCapacityAhIgnoresOldBoatField(t *testing.T) {
	path := writeVesselTestSettings(t, "boat:\n  house_battery_capacity_ah: 1440\n")
	if got := loadHouseBatteryCapacityAh(path); got != -1 {
		t.Fatalf("loadHouseBatteryCapacityAh should ignore the old boat field: got %v, want -1", got)
	}
}
