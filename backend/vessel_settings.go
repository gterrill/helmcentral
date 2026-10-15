package main

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"

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
// main.go) stops: the capacity comes from vessel.house_bank only, and the
// electrical state reports -1 ("not set") when it is unset.

// vesselSettings is the "vessel:" block, decoded independently of the large
// settingsPayload struct in signalk.go (the same narrow-reader pattern
// loadSettingString/loadSettingFloat already use) so this file has no
// compile-time dependency on that struct's already-sprawling surface.
type vesselSettings struct {
	Engines   []vesselEngineSetting   `yaml:"engines" json:"engines"`
	HouseBank *vesselHouseBankSetting `yaml:"house_bank" json:"house_bank"`
	// Batteries are the operator's own names for electrical.batteries.<id>
	// instances (sensor_names.go). An instance with no entry, or an empty
	// name, simply has no operator name.
	Batteries []vesselBatterySetting `yaml:"batteries" json:"batteries"`
	// Alternators says which electrical.alternator.<id> instances are the
	// port and starboard alternators. Empty means not set: that side has no
	// data. There is no default, because the Victron GX numbers instances
	// per installation.
	Alternators vesselAlternatorSettings `yaml:"alternators" json:"alternators"`
	// Electrical says which generator, AC meter, shore charger, inverter and
	// voltage-readout battery the electrical tiles and the overnight model
	// read. Empty means not set: that figure has no data.
	Electrical vesselElectricalSettings `yaml:"electrical" json:"electrical"`
	// Solar are the operator's own names for electrical.solar.<id>
	// controllers, in the same shape as Batteries.
	Solar []vesselSolarSetting `yaml:"solar" json:"solar"`
}

// vesselElectricalSettings holds bare SignalK instance ids as strings, so "0"
// stays distinct from unset. There are no defaults: the Victron GX numbers
// instances per installation.
type vesselElectricalSettings struct {
	// GeneratorInstance is the <id> in electrical.generator.<id>.
	GeneratorInstance string `yaml:"generator_instance" json:"generator_instance"`
	// GeneratorACInstance is the <id> in electrical.ac.<id>, the AC input
	// that measures the generator's real power.
	GeneratorACInstance string `yaml:"generator_ac_instance" json:"generator_ac_instance"`
	// ShoreChargerInstance is the <id> in electrical.chargers.<id>.
	ShoreChargerInstance string `yaml:"shore_charger_instance" json:"shore_charger_instance"`
	// InverterInstance is the <id> in electrical.inverters.<id>.
	InverterInstance string `yaml:"inverter_instance" json:"inverter_instance"`
	// VoltageReadoutBattery is the <id> in electrical.batteries.<id> whose
	// voltage the Battery & Power tile shows as its extra voltage row.
	VoltageReadoutBattery string `yaml:"voltage_readout_battery" json:"voltage_readout_battery"`
}

// vesselSolarSetting names one electrical.solar.<id> controller.
type vesselSolarSetting struct {
	Instance string `yaml:"instance" json:"instance"`
	Name     string `yaml:"name" json:"name"`
}

// validateVesselElectrical checks that every chosen electrical instance is a
// plain id. The ids are spliced into SignalK paths, so anything with path
// characters is refused.
func validateVesselElectrical(e vesselElectricalSettings) *settingsValidationError {
	fields := []struct {
		field, label, value string
	}{
		{"vessel.electrical.generator_instance", "the generator", e.GeneratorInstance},
		{"vessel.electrical.generator_ac_instance", "the generator's AC input", e.GeneratorACInstance},
		{"vessel.electrical.shore_charger_instance", "the shore charger", e.ShoreChargerInstance},
		{"vessel.electrical.inverter_instance", "the inverter", e.InverterInstance},
		{"vessel.electrical.voltage_readout_battery", "the voltage readout battery", e.VoltageReadoutBattery},
	}
	for _, f := range fields {
		if f.value != "" && !alternatorInstanceIDRe.MatchString(f.value) {
			return &settingsValidationError{Field: f.field, Message: f.label + " must be an instance id (letters, digits, _ or -)"}
		}
	}
	return nil
}

// solarName is the operator's name for solar controller id, or "".
func (v vesselSettings) solarName(id string) string {
	for _, s := range v.Solar {
		if s.Instance == id {
			return strings.TrimSpace(s.Name)
		}
	}
	return ""
}

// normalizeVesselElectrical trims every chosen instance id.
func normalizeVesselElectrical(e vesselElectricalSettings) vesselElectricalSettings {
	return vesselElectricalSettings{
		GeneratorInstance:     strings.TrimSpace(e.GeneratorInstance),
		GeneratorACInstance:   strings.TrimSpace(e.GeneratorACInstance),
		ShoreChargerInstance:  strings.TrimSpace(e.ShoreChargerInstance),
		InverterInstance:      strings.TrimSpace(e.InverterInstance),
		VoltageReadoutBattery: strings.TrimSpace(e.VoltageReadoutBattery),
	}
}

// vesselAlternatorSettings holds the operator's port and starboard
// alternator instance ids, as strings so "0" stays distinct from unset.
type vesselAlternatorSettings struct {
	PortInstance      string `yaml:"port_instance" json:"port_instance"`
	StarboardInstance string `yaml:"starboard_instance" json:"starboard_instance"`
}

// alternatorInstanceIDRe is a plain SignalK instance id: no dots, slashes or
// other path characters.
var alternatorInstanceIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// validateVesselAlternators checks the saved alternator instances: each is a
// plain id and the two differ when both are set.
func validateVesselAlternators(a vesselAlternatorSettings) *settingsValidationError {
	if a.PortInstance != "" && !alternatorInstanceIDRe.MatchString(a.PortInstance) {
		return &settingsValidationError{Field: "vessel.alternators.port_instance", Message: "the port alternator must be an alternator instance id (letters, digits, _ or -)"}
	}
	if a.StarboardInstance != "" && !alternatorInstanceIDRe.MatchString(a.StarboardInstance) {
		return &settingsValidationError{Field: "vessel.alternators.starboard_instance", Message: "the starboard alternator must be an alternator instance id (letters, digits, _ or -)"}
	}
	if a.PortInstance != "" && a.PortInstance == a.StarboardInstance {
		return &settingsValidationError{Field: "vessel.alternators.starboard_instance", Message: "port and starboard must be different alternators"}
	}
	return nil
}

// vesselBatterySetting names one electrical.batteries.<id> instance, e.g.
// instance "3" as "Port Engine Starter Battery".
type vesselBatterySetting struct {
	Instance string `yaml:"instance" json:"instance"`
	Name     string `yaml:"name" json:"name"`
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

	return parseVesselBlock(settings)
}

// parseVesselBlock decodes the "vessel" key of an already-read settings map.
// Absent or nil is the zero value; a block that does not parse is an error.
func parseVesselBlock(settings map[string]any) (vesselSettings, error) {
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
	// Ids are trimmed once, here, so no reader has to.
	result.Electrical = normalizeVesselElectrical(result.Electrical)
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

	generic, err := vesselBlockMap(v)
	if err != nil {
		return err
	}

	settings["vessel"] = generic
	return writeSettings(settingsPath, settings)
}

// vesselBlockMap turns v into the generic map settings.yaml stores under
// "vessel", so POST /api/settings can set the key in the document it is
// already rewriting.
func vesselBlockMap(v vesselSettings) (map[string]any, error) {
	buf, err := yaml.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("vessel settings: marshalling vessel block: %w", err)
	}
	var generic map[string]any
	if err := yaml.Unmarshal(buf, &generic); err != nil {
		return nil, fmt.Errorf("vessel settings: round-tripping vessel block: %w", err)
	}
	return generic, nil
}

// houseBankPath is the chosen house bank's SignalK path, trimmed, or "" when
// no bank is set. Every reader of the bank goes through here.
func (v vesselSettings) houseBankPath() string {
	if v.HouseBank == nil {
		return ""
	}
	return strings.TrimSpace(v.HouseBank.Path)
}

// houseBankSocPath is the bank's state-of-charge path, or "" with no bank.
func (v vesselSettings) houseBankSocPath() string {
	bank := v.houseBankPath()
	if bank == "" {
		return ""
	}
	return bank + ".capacity.stateOfCharge"
}

// vesselReadLog remembers the last unreadable-vessel-block error per call
// site, so a settings file that stays broken is logged once rather than on
// every tick. A clean read forgets it, so the next failure is logged again.
var vesselReadLog = struct {
	mu   sync.Mutex
	last map[string]string
}{last: map[string]string{}}

func resetVesselReadLog() {
	vesselReadLog.mu.Lock()
	defer vesselReadLog.mu.Unlock()
	vesselReadLog.last = map[string]string{}
}

// noteVesselRead logs msg (with the error) when err differs from the last
// one seen at site, and clears the site on a clean read.
func noteVesselRead(site string, err error, format string) {
	vesselReadLog.mu.Lock()
	defer vesselReadLog.mu.Unlock()
	if err == nil {
		delete(vesselReadLog.last, site)
		return
	}
	if vesselReadLog.last[site] == err.Error() {
		return
	}
	vesselReadLog.last[site] = err.Error()
	log.Printf(format, err)
}
