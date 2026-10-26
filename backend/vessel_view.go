package main

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// vessel_view.go resolves what the rest of the backend reads as "the vessel":
// which propulsion instances are engines, which battery is the house bank,
// what the operator calls each battery, solar controller and tank, and which
// alternators, charger, inverter and generator the electrical tiles read.
//
// None of it is stored here. Equipment items own their SignalK source
// (equipment_source.go) and the roles block names which item plays each
// single-choice part (vessel_roles.go); vesselView is computed from the two on
// every read (ADR 0187). Nothing ships a default for any field: an item with
// no source, a role left blank, or a role whose item has gone away resolves
// to the zero value, which is the "not set up" state, not a guess. A role
// that names an item that no longer fits is logged once and left unset, so
// that figure shows no data rather than another bank's.

// vesselView is the resolved vessel. Its field types keep the names they had
// when they were stored settings, so every reader (the electrical state, the
// anomaly detectors, the sensor namer) is unchanged.
type vesselView struct {
	Engines   []vesselEngineSetting
	HouseBank *vesselHouseBankSetting
	// HouseBankUnresolved is true when the house bank role holds an item id
	// but that item has no SignalK source (or no longer exists), so HouseBank
	// is nil though the operator did choose one.
	HouseBankUnresolved bool
	// Batteries are the mapped items' names for electrical.batteries.<id>
	// instances (sensor_names.go). An instance with no item has no operator
	// name.
	Batteries []vesselBatterySetting
	// Alternators says which electrical.alternator.<id> instances are the
	// port and starboard alternators. Empty means not set.
	Alternators vesselAlternatorSettings
	// Electrical says which generator, AC meter, shore charger, inverter and
	// voltage-readout battery the electrical tiles and the overnight model
	// read. Empty means not set.
	Electrical vesselElectricalSettings
	// Solar are the mapped items' names for electrical.solar.<id> controllers.
	Solar []vesselSolarSetting
	// Tanks maps "<category>.<id>" (fuel.2), lower-cased, to the mapped item's
	// name. The tank tile lists a tank only when it has one.
	Tanks map[string]string
	// RolesNone holds the roles (by field name, "generator_ac_instance"
	// included) the operator answered "this boat has none". It reads as unset
	// everywhere else in this view; only the setup status looks at it.
	RolesNone map[string]bool
}

// vesselElectricalSettings holds bare SignalK instance ids as strings, so "0"
// stays distinct from unset.
type vesselElectricalSettings struct {
	// GeneratorInstance is the <id> in electrical.generator.<id>.
	GeneratorInstance string
	// GeneratorACInstance is the <id> in electrical.ac.<id>, the AC input
	// that measures the generator's real power.
	GeneratorACInstance string
	// ShoreChargerInstance is the <id> in electrical.chargers.<id>.
	ShoreChargerInstance string
	// InverterInstance is the <id> in electrical.inverters.<id>.
	InverterInstance string
	// VoltageReadoutBattery is the <id> in electrical.batteries.<id> whose
	// voltage the Battery & Power tile shows as its extra voltage row.
	VoltageReadoutBattery string
}

// vesselSolarSetting names one electrical.solar.<id> controller.
type vesselSolarSetting struct {
	Instance string
	Name     string
}

// solarName is the operator's name for solar controller id, or "".
func (v vesselView) solarName(id string) string {
	for _, s := range v.Solar {
		if s.Instance == id {
			return strings.TrimSpace(s.Name)
		}
	}
	return ""
}

// tankName is the mapped item's name for the tank "<category>.<id>", or "".
func (v vesselView) tankName(category, id string) string {
	return v.Tanks[strings.ToLower(strings.TrimSpace(category))+"."+strings.ToLower(strings.TrimSpace(id))]
}

// vesselAlternatorSettings holds the port and starboard alternator instance
// ids, as strings so "0" stays distinct from unset.
type vesselAlternatorSettings struct {
	PortInstance      string
	StarboardInstance string
}

// alternatorInstanceIDRe is a plain SignalK instance id: no dots, slashes or
// other path characters.
var alternatorInstanceIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// vesselBatterySetting names one electrical.batteries.<id> instance, e.g.
// instance "3" as "Port Engine Starter Battery".
type vesselBatterySetting struct {
	Instance string
	Name     string
}

// vesselEngineSetting is one propulsion.<id> instance with an engine item
// mapped to it. Name is the item's name (e.g. "Port Engine"); EquipmentID is
// the item, which carries the engine's equipment profile (engine_profiles.go).
type vesselEngineSetting struct {
	Instance    string
	Name        string
	EquipmentID string
}

// vesselHouseBankSetting is the house bank. Path is the SignalK
// electrical.batteries.<id> node it reads live SoC/voltage/current from;
// EquipmentID is the item carrying the battery profile (chemistry, per-cell
// thresholds). CapacityAh and Cells are the item's own figures (a pack's
// capacity and cell count are not something SignalK reliably publishes).
// WarnVoltage/HighVoltage are optional per-pack overrides of the profile's own
// per-cell x Cells thresholds -- zero means "use the profile's numbers", not
// "threshold zero".
type vesselHouseBankSetting struct {
	Path        string
	EquipmentID string
	CapacityAh  float64
	Cells       int
	WarnVoltage float64
	HighVoltage float64
}

// sourceItemsTTL is how long the list of items that own a source is reused.
// Tick builders resolve the view several times a second; an edit shows within
// a second, and the item and role writers drop the list at once.
const sourceItemsTTL = time.Second

var sourceItemsCache = struct {
	mu    sync.Mutex
	store *documentStore
	at    time.Time
	items []sourceItem
	// generation counts invalidations, so a read that straddled one is not cached.
	generation uint64
}{}

// invalidateSourceItems drops the cached list of items that own a source.
func invalidateSourceItems() {
	sourceItemsCache.mu.Lock()
	sourceItemsCache.generation++
	sourceItemsCache.items = nil
	sourceItemsCache.at = time.Time{}
	sourceItemsCache.mu.Unlock()
	invalidateEquipmentRefs()
}

// listSourceItems is every item that owns a SignalK source, sorted by group
// then instance. With no store open there are none.
func listSourceItems() ([]sourceItem, error) {
	store := globalDocumentStore
	if store == nil {
		return nil, nil
	}
	sourceItemsCache.mu.Lock()
	if sourceItemsCache.store == store && sourceItemsCache.items != nil && time.Since(sourceItemsCache.at) < sourceItemsTTL {
		items := sourceItemsCache.items
		sourceItemsCache.mu.Unlock()
		return items, nil
	}
	generation := sourceItemsCache.generation
	sourceItemsCache.mu.Unlock()

	items, err := store.SourceItems()
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []sourceItem{}
	}
	sourceItemsCache.mu.Lock()
	// An invalidate during the read means these items may predate the edit.
	if sourceItemsCache.generation == generation {
		sourceItemsCache.store, sourceItemsCache.items, sourceItemsCache.at = store, items, time.Now()
	}
	sourceItemsCache.mu.Unlock()
	return items, nil
}

// SourceItems reads every item that owns a SignalK source.
func (s *documentStore) SourceItems() ([]sourceItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, name, signalk_source, profile_id, capacity_ah, cells, warn_voltage, high_voltage,
			tank_warn_percent, tank_alarm_percent, tank_alarms_off
		FROM equipment WHERE signalk_source <> ''`)
	if err != nil {
		return nil, fmt.Errorf("list source items: %w", err)
	}
	defer rows.Close()
	var items []sourceItem
	for rows.Next() {
		var it equipmentItem
		var alarmsOff int
		if err := rows.Scan(&it.ID, &it.Name, &it.SignalKSource, &it.ProfileID, &it.CapacityAh, &it.Cells, &it.WarnVoltage, &it.HighVoltage,
			&it.TankWarnPercent, &it.TankAlarmPercent, &alarmsOff); err != nil {
			return nil, fmt.Errorf("list source items: %w", err)
		}
		it.TankAlarmsOff = alarmsOff != 0
		if si, ok := newSourceItem(it); ok {
			items = append(items, si)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list source items: %w", err)
	}
	sortSourceItems(items)
	return items, nil
}

// loadVesselRoles reads the "roles:" block from settingsPath. A missing file
// or block loads to the zero value (nothing set) with no error. A block that
// fails to parse is a corrupt section, not an absent one, and fails fast.
//
// The block's YAML is cached with readSettings' parse (settings_cache.go),
// keyed on the same modification time and size, so a build that runs every
// tick skips the whole-document copy and re-marshal until settings.yaml
// changes.
func loadVesselRoles(settingsPath string) (vesselRoles, error) {
	info, statErr := os.Stat(settingsPath)
	if statErr == nil {
		if buf, ok := cachedRolesYAMLFor(settingsPath, info); ok {
			return decodeRolesYAML(buf)
		}
	}
	settings, err := readSettings(settingsPath)
	if err != nil {
		return vesselRoles{}, err
	}
	buf, err := rolesBlockYAML(settings)
	if err != nil {
		return vesselRoles{}, err
	}
	roles, err := decodeRolesYAML(buf)
	if err != nil {
		return vesselRoles{}, err
	}
	if statErr == nil {
		storeRolesYAMLCache(settingsPath, info, buf)
	}
	return roles, nil
}

// loadVesselView resolves the vessel from the roles block of settingsPath and
// the items that own a source.
func loadVesselView(settingsPath string) (vesselView, error) {
	roles, err := loadVesselRoles(settingsPath)
	if err != nil {
		return vesselView{}, err
	}
	items, err := listSourceItems()
	if err != nil {
		return vesselView{}, err
	}
	return resolveVesselView(roles, items), nil
}

// resolveVesselView is the whole of how items and roles become a vesselView.
func resolveVesselView(roles vesselRoles, items []sourceItem) vesselView {
	var v vesselView
	byID := map[string]sourceItem{}
	for _, it := range items {
		byID[it.ID] = it
		switch it.Group {
		case sourceEngine:
			v.Engines = append(v.Engines, vesselEngineSetting{Instance: it.Instance, Name: it.Name, EquipmentID: it.ID})
		case sourceBattery:
			v.Batteries = append(v.Batteries, vesselBatterySetting{Instance: it.Instance, Name: it.Name})
		case sourceSolar:
			v.Solar = append(v.Solar, vesselSolarSetting{Instance: it.Instance, Name: it.Name})
		case sourceTank:
			if v.Tanks == nil {
				v.Tanks = map[string]string{}
			}
			v.Tanks[strings.ToLower(it.Instance)] = it.Name
		}
	}

	// pick resolves a role to its item, or logs once and returns false.
	pick := func(spec roleSpec) (sourceItem, bool) {
		id := *spec.get(&roles)
		if id == "" || id == roleNone {
			noteVesselRead("role:"+spec.field, nil, "")
			return sourceItem{}, false
		}
		it, ok := byID[id]
		if ok && it.Group == spec.group {
			noteVesselRead("role:"+spec.field, nil, "")
			return it, true
		}
		noteVesselRead("role:"+spec.field, fmt.Errorf("item %s is not assigned to a %s source any more", id, spec.group),
			"vessel: "+spec.label+" is unset until you choose it again: %v")
		return sourceItem{}, false
	}

	for _, spec := range roleSpecs {
		it, ok := pick(spec)
		if !ok {
			continue
		}
		switch spec.field {
		case "house_bank":
			v.HouseBank = &vesselHouseBankSetting{
				Path: it.Source, EquipmentID: it.ID,
				CapacityAh: it.CapacityAh, Cells: it.Cells,
				WarnVoltage: it.WarnVoltage, HighVoltage: it.HighVoltage,
			}
		case "port_alternator":
			v.Alternators.PortInstance = it.Instance
		case "starboard_alternator":
			v.Alternators.StarboardInstance = it.Instance
		case "voltage_readout_battery":
			v.Electrical.VoltageReadoutBattery = it.Instance
		case "shore_charger":
			v.Electrical.ShoreChargerInstance = it.Instance
		case "inverter":
			v.Electrical.InverterInstance = it.Instance
		case "generator":
			v.Electrical.GeneratorInstance = it.Instance
		}
	}
	if id := roles.HouseBank; id != "" && id != roleNone && v.HouseBank == nil {
		v.HouseBankUnresolved = true
	}
	// With no generator there is no generator AC input, whatever an earlier
	// choice left in the block.
	if roles.GeneratorACInstance != roleNone && roles.Generator != roleNone {
		v.Electrical.GeneratorACInstance = roles.GeneratorACInstance
	}
	for _, spec := range roleSpecs {
		if *spec.get(&roles) == roleNone {
			v.markRoleNone(spec.field)
		}
	}
	if roles.GeneratorACInstance == roleNone {
		v.markRoleNone("generator_ac_instance")
	}
	return v
}

func (v *vesselView) markRoleNone(field string) {
	if v.RolesNone == nil {
		v.RolesNone = map[string]bool{}
	}
	v.RolesNone[field] = true
}

// houseBankPath is the house bank's SignalK path, trimmed, or "" when no bank
// is set. Every reader of the bank goes through here.
func (v vesselView) houseBankPath() string {
	if v.HouseBank == nil {
		return ""
	}
	return strings.TrimSpace(v.HouseBank.Path)
}

// houseBankSocPath is the bank's state-of-charge path, or "" with no bank.
func (v vesselView) houseBankSocPath() string {
	bank := v.houseBankPath()
	if bank == "" {
		return ""
	}
	return bank + ".capacity.stateOfCharge"
}

// vesselReadLog remembers the last unreadable-vessel error per call site, so a
// state that stays broken is logged once rather than on every tick. A clean
// read forgets it, so the next failure is logged again.
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
