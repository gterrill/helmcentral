package main

import (
	"fmt"
	"log"

	"gopkg.in/yaml.v3"
)

// vessel_settings.go owns the "vessel:" block of settings.yaml: which
// propulsion instances count as engines for the differential detector, and
// which electrical.batteries.<id> is the house bank the full-bank-charging
// detector watches. See the anomaly-detection plan's "Vessel setup" section.
//
// Nothing here ships a default for any field: an absent or empty "vessel:"
// block loads to the zero value (no engines, no house bank), which is the
// plan's "not set up" state, not a guess. This is also where
// defaultHouseBatteryCapacityAh's old fallback (Pikorua's 1440 Ah baked into
// main.go) stops: loadHouseBatteryCapacityAh below now reads
// vessel.house_bank.capacity_ah only, and reports -1 ("not set") when it is
// unset, exactly like every other "-1 means unset" sentinel already used in
// this file's neighbours (electricalStateData's own fields).

// vesselSettings is the "vessel:" block, decoded independently of the large
// settingsPayload struct in signalk.go (the same narrow-reader pattern
// loadSettingString/loadSettingFloat already use) so this file has no
// compile-time dependency on that struct's already-sprawling surface.
type vesselSettings struct {
	Engines   []vesselEngineSetting   `yaml:"engines" json:"engines"`
	HouseBank *vesselHouseBankSetting `yaml:"house_bank" json:"house_bank"`
}

// vesselEngineSetting is one propulsion.<id> instance the operator has
// ticked "include" for. Name is the operator's own display name (e.g.
// "Port"); EquipmentID links to the inventory registry item that carries
// the engine's equipment profile (engine_profiles.go) -- the profile itself
// is never duplicated here, only referenced (the plan's "one home for the
// profile").
type vesselEngineSetting struct {
	Instance    string `yaml:"instance" json:"instance"`
	Name        string `yaml:"name" json:"name"`
	EquipmentID string `yaml:"equipment_id" json:"equipment_id"`
}

// vesselHouseBankSetting is the chosen house bank. Path is the SignalK
// electrical.batteries.<id> node it reads live SoC/voltage/current from;
// EquipmentID links to the inventory registry item carrying the battery
// profile (chemistry, per-cell thresholds). CapacityAh and Cells are entered
// by the operator (a pack's capacity and cell count are not something
// SignalK reliably publishes). WarnVoltage/HighVoltage are optional
// per-pack overrides of the profile's own per-cell x Cells thresholds --
// zero means "use the profile's numbers", not "threshold zero".
type vesselHouseBankSetting struct {
	Path        string  `yaml:"path" json:"path"`
	EquipmentID string  `yaml:"equipment_id" json:"equipment_id"`
	CapacityAh  float64 `yaml:"capacity_ah" json:"capacity_ah"`
	Cells       int     `yaml:"cells" json:"cells"`
	WarnVoltage float64 `yaml:"warn_voltage,omitempty" json:"warn_voltage,omitempty"`
	HighVoltage float64 `yaml:"high_voltage,omitempty" json:"high_voltage,omitempty"`
}

// loadVesselSettings reads the "vessel:" block from settingsPath. A missing
// file or a missing/empty "vessel" key both load to the zero value (fresh
// install, "not set up") with no error -- readSettings itself already gives
// that same "missing file, empty map, no error" contract. A "vessel" key
// that fails to parse as vesselSettings's own shape is a different case:
// that is a corrupt section, not an absent one, and fails fast rather than
// being silently treated as "not set up" (AGENTS.md's fail-fast policy).
func loadVesselSettings(settingsPath string) (vesselSettings, error) {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return vesselSettings{}, err
	}

	raw, ok := settings["vessel"]
	if !ok || raw == nil {
		return vesselSettings{}, nil
	}

	// readSettings already parsed the whole document with yaml.v3; re-marshal
	// just this branch and unmarshal it into the typed struct rather than
	// hand-walking the generic map[string]any/[]any tree a second time.
	buf, err := yaml.Marshal(raw)
	if err != nil {
		return vesselSettings{}, fmt.Errorf("vessel settings: re-marshalling vessel block: %w", err)
	}

	var result vesselSettings
	if err := yaml.Unmarshal(buf, &result); err != nil {
		return vesselSettings{}, fmt.Errorf("vessel settings: parsing vessel block: %w", err)
	}
	return result, nil
}

// saveVesselSettings writes v as the "vessel:" block of settingsPath,
// leaving every other top-level section (boat, signalk, ui, ...) exactly as
// it was -- the same read-whole-document/overwrite-one-key discipline
// updateSettingsHandler applies per top-level section in signalk.go.
func saveVesselSettings(settingsPath string, v vesselSettings) error {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return err
	}

	buf, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("vessel settings: marshalling vessel block: %w", err)
	}
	var generic map[string]any
	if err := yaml.Unmarshal(buf, &generic); err != nil {
		return fmt.Errorf("vessel settings: round-tripping vessel block: %w", err)
	}

	settings["vessel"] = generic
	return writeSettings(settingsPath, settings)
}

// loadHouseBatteryCapacityAh reads the configured house bank's capacity in
// amp-hours, or -1 when no house bank is configured -- the same sentinel
// electricalStateData.BatteryCapacityAh and its neighbours already use for
// "not known". This used to fall back to boat.house_battery_capacity_ah and,
// failing that, main.go's defaultHouseBatteryCapacityAh (Pikorua's own 1440
// Ah bank baked into shipped code); both are gone. A malformed vessel block
// is treated the same as unset here -- the caller (the overnight electrical
// estimate) has no better answer than "not set" for either, and
// loadVesselSettings's own error is for a caller that can act on it, not
// this narrow float reader.
func loadHouseBatteryCapacityAh(settingsPath string) float64 {
	v, err := loadVesselSettings(settingsPath)
	if err != nil {
		log.Printf("vessel settings: house bank capacity unavailable: %v", err)
		return -1
	}
	if v.HouseBank == nil || v.HouseBank.CapacityAh <= 0 {
		return -1
	}
	return v.HouseBank.CapacityAh
}
