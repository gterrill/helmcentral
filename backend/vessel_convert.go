package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// vessel_convert.go is the one-off conversion of the old "vessel:" settings
// block, ui.tank_labels and the per-item hour meter path into equipment items
// that own their SignalK source plus the roles block (ADR 0185). It runs at
// startup and is finished once the old keys are gone from settings.yaml, so
// every start after the first does nothing but one read.
//
// Fail-fast: a role whose item cannot be found is logged once, clearly, and
// left unset, so that figure shows no data -- which is what an unset role has
// always meant. A source already owned by another item is logged and skipped,
// never moved. Names of sources that have no item are not carried over (an
// item is the only place a name lives now) and each is logged so the operator
// can create the item.

// legacyVessel is the old "vessel:" block, as settings.yaml stored it.
type legacyVessel struct {
	Engines []struct {
		Instance    string `yaml:"instance"`
		Name        string `yaml:"name"`
		EquipmentID string `yaml:"equipment_id"`
	} `yaml:"engines"`
	HouseBank *struct {
		Path        string  `yaml:"path"`
		EquipmentID string  `yaml:"equipment_id"`
		CapacityAh  float64 `yaml:"capacity_ah"`
		Cells       int     `yaml:"cells"`
		WarnVoltage float64 `yaml:"warn_voltage"`
		HighVoltage float64 `yaml:"high_voltage"`
	} `yaml:"house_bank"`
	Batteries []struct {
		Instance string `yaml:"instance"`
		Name     string `yaml:"name"`
	} `yaml:"batteries"`
	Alternators struct {
		PortInstance      string `yaml:"port_instance"`
		StarboardInstance string `yaml:"starboard_instance"`
	} `yaml:"alternators"`
	Electrical struct {
		GeneratorInstance     string `yaml:"generator_instance"`
		GeneratorACInstance   string `yaml:"generator_ac_instance"`
		ShoreChargerInstance  string `yaml:"shore_charger_instance"`
		InverterInstance      string `yaml:"inverter_instance"`
		VoltageReadoutBattery string `yaml:"voltage_readout_battery"`
	} `yaml:"electrical"`
	Solar []struct {
		Instance string `yaml:"instance"`
		Name     string `yaml:"name"`
	} `yaml:"solar"`
}

var (
	legacyEngineHoursRe    = regexp.MustCompile(`^(propulsion\.[A-Za-z0-9_-]+)\.runTime$`)
	legacyGeneratorHoursRe = regexp.MustCompile(`^(electrical\.generator\.[A-Za-z0-9_-]+)\.runtime$`)
)

type convertItem struct {
	id, name, source string
}

// convertLegacyVessel converts settingsPath's old vessel block, tank labels and
// hour meter paths. It returns nil and changes nothing when there is nothing
// left to convert.
func convertLegacyVessel(settingsPath string, store *documentStore) error {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return fmt.Errorf("convert vessel settings: %w", err)
	}
	rawVessel, hasVessel := settings["vessel"]
	hasVessel = hasVessel && rawVessel != nil
	var tankLabels map[string]any
	hasLabelsKey := false
	if ui, ok := settings["ui"].(map[string]any); ok {
		tankLabels, _ = ui["tank_labels"].(map[string]any)
		_, hasLabelsKey = ui["tank_labels"]
	}

	var legacy legacyVessel
	if hasVessel {
		buf, err := yaml.Marshal(rawVessel)
		if err != nil {
			return fmt.Errorf("convert vessel settings: re-marshalling vessel block: %w", err)
		}
		if err := yaml.Unmarshal(buf, &legacy); err != nil {
			return fmt.Errorf("convert vessel settings: parsing vessel block: %w", err)
		}
	}

	var roles vesselRoles
	var oldSocPath string
	var notes []string
	note := func(format string, args ...any) { notes = append(notes, fmt.Sprintf(format, args...)) }

	err = store.RunTx("convert vessel settings", func(tx *sql.Tx, now time.Time) error {
		items, err := loadConvertItems(tx)
		if err != nil {
			return err
		}
		bySource := map[string]convertItem{}
		byID := map[string]convertItem{}
		for _, it := range items {
			byID[it.id] = it
			if it.source != "" {
				bySource[it.source] = it
			}
		}

		// assign gives item id the source, unless the item already has a
		// different one or another item owns it. It reports whether the item
		// now owns source.
		assign := func(id, source, why string) bool {
			it, ok := byID[id]
			if !ok {
				note("%s: the equipment item it was linked to no longer exists", why)
				return false
			}
			if owner, taken := bySource[source]; taken {
				if owner.id == id {
					return true
				}
				note("%s: %s already belongs to %q, so %q was not assigned to it", why, source, owner.name, it.name)
				return false
			}
			if it.source != "" && it.source != source {
				note("%s: %q already owns %s, so it was not assigned %s", why, it.name, it.source, source)
				return false
			}
			if err := validateSource(source); err != nil {
				note("%s: %v", why, err)
				return false
			}
			if _, err := tx.Exec(`UPDATE equipment SET signalk_source = ?, category = ?, updated_at = ? WHERE id = ?`,
				source, deriveEquipmentCategory(source), now.Unix(), id); err != nil {
				note("%s: %v", why, err)
				return false
			}
			it.source = source
			byID[id] = it
			bySource[source] = it
			return true
		}

		// find returns the item that owns source, noting a missing one.
		find := func(source, what string) string {
			it, ok := bySource[source]
			if !ok {
				note("%s was %s; no equipment item is assigned to it now, so that role is unset until you choose one", what, source)
				return ""
			}
			return it.id
		}

		for _, e := range legacy.Engines {
			instance := strings.TrimSpace(e.Instance)
			if instance == "" {
				continue
			}
			source := sourcePath(sourceEngine, instance)
			if strings.TrimSpace(e.EquipmentID) == "" {
				note("engine %q (%s) had no equipment item; create one for it in Settings > SignalK", e.Name, source)
				continue
			}
			assign(strings.TrimSpace(e.EquipmentID), source, fmt.Sprintf("engine %q", e.Name))
		}

		if hb := legacy.HouseBank; hb != nil && strings.TrimSpace(hb.Path) != "" {
			path := strings.TrimSpace(hb.Path)
			oldSocPath = path + ".capacity.stateOfCharge"
			switch {
			case strings.TrimSpace(hb.EquipmentID) == "":
				note("the house bank (%s) had no equipment item; create one for it in Settings > SignalK and choose it as the house bank", path)
			case assign(strings.TrimSpace(hb.EquipmentID), path, "the house bank"):
				if _, err := tx.Exec(`UPDATE equipment SET capacity_ah = ?, cells = ?, warn_voltage = ?, high_voltage = ? WHERE id = ?`,
					hb.CapacityAh, hb.Cells, hb.WarnVoltage, hb.HighVoltage, strings.TrimSpace(hb.EquipmentID)); err != nil {
					return fmt.Errorf("copy house bank figures: %w", err)
				}
				roles.HouseBank = strings.TrimSpace(hb.EquipmentID)
			}
		}

		// Names go onto an item that has none. Every item has a name, so this
		// only matters for a name the item lacks. A named source no item owns
		// gets a minimal item made for it (the name, the kind the source
		// belongs to, the source), so no name is lost; the operator can delete
		// any they do not want. A source an item already owns is never given a
		// second one. create is false for an engine row that was linked to an
		// item: that item exists and was dealt with above.
		nameOnto := func(source, name, what string, create bool) {
			name = strings.TrimSpace(name)
			if name == "" {
				return
			}
			it, ok := bySource[source]
			for known, candidate := range bySource {
				// Tank labels were keyed lower-case; the SignalK category keeps its case.
				if !ok && strings.EqualFold(known, source) {
					it, ok, source = candidate, true, known
				}
			}
			if !ok {
				if !create {
					return
				}
				group, _, valid := parseSource(source)
				if !valid {
					note("%s %q (%s) is not a source an item can own, so the name was not carried over", what, name, source)
					return
				}
				created, err := createEquipmentTx(tx, now, equipmentItem{
					Name: name, System: sourceSystem(group), Quantity: 1, SignalKSource: source,
				})
				if err != nil {
					note("%s %q (%s): could not create an equipment item: %v", what, name, source, err)
					return
				}
				ci := convertItem{id: created.ID, name: created.Name, source: source}
				byID[ci.id] = ci
				bySource[source] = ci
				note("created equipment item %q for %s (%s); delete it on the Equipment page if you do not want it", name, source, what)
				return
			}
			if strings.TrimSpace(it.name) == "" {
				if _, err := tx.Exec(`UPDATE equipment SET name = ? WHERE id = ?`, name, it.id); err == nil {
					it.name = name
					bySource[source] = it
					byID[it.id] = it
				}
			}
		}
		for _, b := range legacy.Batteries {
			nameOnto(sourcePath(sourceBattery, strings.TrimSpace(b.Instance)), b.Name, "battery", true)
		}
		for _, s := range legacy.Solar {
			nameOnto(sourcePath(sourceSolar, strings.TrimSpace(s.Instance)), s.Name, "solar controller", true)
		}
		for _, e := range legacy.Engines {
			nameOnto(sourcePath(sourceEngine, strings.TrimSpace(e.Instance)), e.Name, "engine", strings.TrimSpace(e.EquipmentID) == "")
		}
		for key, v := range tankLabels {
			label, _ := v.(string)
			nameOnto(sourcePath(sourceTank, strings.TrimSpace(key)), label, "tank", true)
		}

		// A hand-typed hour meter path becomes the item's source when it names
		// an engine's or generator's own runtime counter and the item has no
		// source yet; then the column is retired for every item.
		rows, err := tx.Query(`SELECT id, hour_meter_path FROM equipment WHERE hour_meter_path <> ''`)
		if err != nil {
			return fmt.Errorf("read hour meter paths: %w", err)
		}
		type meter struct{ id, path string }
		var meters []meter
		for rows.Next() {
			var m meter
			if err := rows.Scan(&m.id, &m.path); err != nil {
				rows.Close()
				return fmt.Errorf("read hour meter paths: %w", err)
			}
			meters = append(meters, m)
		}
		rows.Close()
		for _, m := range meters {
			it := byID[m.id]
			if it.source == "" {
				for _, re := range []*regexp.Regexp{legacyEngineHoursRe, legacyGeneratorHoursRe} {
					if match := re.FindStringSubmatch(m.path); match != nil {
						assign(m.id, match[1], fmt.Sprintf("the hour meter of %q (%s)", it.name, m.path))
					}
				}
			}
			it = byID[m.id]
			if it.source == "" {
				note("%q had the hour meter %s, which no source covers now; assign the item its engine or generator in Settings > SignalK to get its hours back", it.name, m.path)
			}
			if _, err := tx.Exec(`UPDATE equipment SET hour_meter_path = '', category = ? WHERE id = ?`, deriveEquipmentCategory(it.source), m.id); err != nil {
				return fmt.Errorf("retire hour meter path: %w", err)
			}
		}
		// Roles are picked last, once every source has been assigned: the hour
		// meter loop above is what gives a generator its source.
		el := legacy.Electrical
		pick := func(group sourceGroup, instance, what string) string {
			instance = strings.TrimSpace(instance)
			if instance == "" {
				return ""
			}
			return find(sourcePath(group, instance), what)
		}
		roles.PortAlternator = pick(sourceAlternator, legacy.Alternators.PortInstance, "the port alternator")
		roles.StarboardAlternator = pick(sourceAlternator, legacy.Alternators.StarboardInstance, "the starboard alternator")
		roles.Generator = pick(sourceGenerator, el.GeneratorInstance, "the generator")
		roles.ShoreCharger = pick(sourceCharger, el.ShoreChargerInstance, "the shore charger")
		roles.Inverter = pick(sourceInverter, el.InverterInstance, "the inverter")
		roles.VoltageReadoutBattery = pick(sourceBattery, el.VoltageReadoutBattery, "the voltage readout battery")
		roles.GeneratorACInstance = strings.TrimSpace(el.GeneratorACInstance)
		return nil
	})
	if err != nil {
		return fmt.Errorf("convert vessel settings: %w", err)
	}
	invalidateSourceItems()

	if !hasVessel && !hasLabelsKey {
		for _, n := range notes {
			log.Printf("vessel conversion: %s", n)
		}
		return nil
	}

	if hasVessel {
		block, err := rolesBlockMap(roles)
		if err != nil {
			return err
		}
		settings["roles"] = block
		delete(settings, "vessel")
	}
	if ui, ok := settings["ui"].(map[string]any); ok {
		delete(ui, "tank_labels")
	}
	if err := writeSettings(settingsPath, settings); err != nil {
		return fmt.Errorf("convert vessel settings: writing settings: %w", err)
	}
	for _, n := range notes {
		log.Printf("vessel conversion: %s", n)
	}
	log.Printf("vessel conversion: moved the vessel settings onto equipment items and roles (ADR 0185)")

	// Alarm rules on the old house bank's state of charge now follow the role.
	if oldSocPath != "" && roles.HouseBank != "" {
		if err := adoptHouseBankRules(oldSocPath); err != nil {
			return fmt.Errorf("convert vessel settings: %w", err)
		}
	}
	return nil
}

func loadConvertItems(tx *sql.Tx) ([]convertItem, error) {
	rows, err := tx.Query(`SELECT id, name, signalk_source FROM equipment`)
	if err != nil {
		return nil, fmt.Errorf("read equipment: %w", err)
	}
	defer rows.Close()
	var items []convertItem
	for rows.Next() {
		var it convertItem
		if err := rows.Scan(&it.id, &it.name, &it.source); err != nil {
			return nil, fmt.Errorf("read equipment: %w", err)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// adoptHouseBankRules makes every alarm rule that watches the old house bank's
// state of charge follow the house bank role instead of that literal path.
func adoptHouseBankRules(socPath string) error {
	alarmRulesMu.Lock()
	defer alarmRulesMu.Unlock()
	changed := false
	for _, rule := range alarmRulesState {
		if rule.PathRole == "" && rule.Path == socPath {
			rule.PathRole = alarmPathRoleHouseBankSoC
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if err := saveAlarmRulesLocked(); err != nil {
		return errors.New("saving alarm rules: " + err.Error())
	}
	return nil
}
