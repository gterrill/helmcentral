package main

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// vessel_roles.go owns the "roles:" block of settings.yaml (ADR 0185). A role
// is a single-choice answer to "which of my equipment items is the ...": the
// house bank, the port alternator, the shore charger. Each is the id of an
// equipment item that owns a SignalK source of the right kind, so changing an
// item's source moves the role with it and nothing here holds a SignalK path.
//
// Nothing ships a default for any role: an absent block, or a role left blank,
// means that figure has no data. Generator AC input is the one role that is
// not an item: it is an electrical.ac.<id> instance (an AC meter), which no
// item kind owns, so it stays the bare instance id it always was.

type vesselRoles struct {
	// HouseBank is the battery item whose state of charge, voltage and current
	// the Battery & Power tile, the overnight model and the anomaly checks read.
	HouseBank string `yaml:"house_bank" json:"house_bank"`
	// PortAlternator and StarboardAlternator are alternator items. They must
	// differ.
	PortAlternator      string `yaml:"port_alternator" json:"port_alternator"`
	StarboardAlternator string `yaml:"starboard_alternator" json:"starboard_alternator"`
	// VoltageReadoutBattery is the battery item whose voltage the Battery &
	// Power tile shows as its extra voltage row.
	VoltageReadoutBattery string `yaml:"voltage_readout_battery" json:"voltage_readout_battery"`
	// ShoreCharger, Inverter and Generator are items of that kind.
	ShoreCharger string `yaml:"shore_charger" json:"shore_charger"`
	Inverter     string `yaml:"inverter" json:"inverter"`
	Generator    string `yaml:"generator" json:"generator"`
	// GeneratorACInstance is the <id> in electrical.ac.<id>, the AC input that
	// measures the generator's real power.
	GeneratorACInstance string `yaml:"generator_ac_instance" json:"generator_ac_instance"`
}

// roleGroup is the kind of item each role accepts.
type roleSpec struct {
	field string // the JSON/YAML key, which is also the field named in an error
	label string // how an error names it
	group sourceGroup
	get   func(*vesselRoles) *string
}

var roleSpecs = []roleSpec{
	{"house_bank", "the house bank", sourceBattery, func(r *vesselRoles) *string { return &r.HouseBank }},
	{"port_alternator", "the port alternator", sourceAlternator, func(r *vesselRoles) *string { return &r.PortAlternator }},
	{"starboard_alternator", "the starboard alternator", sourceAlternator, func(r *vesselRoles) *string { return &r.StarboardAlternator }},
	{"voltage_readout_battery", "the voltage readout battery", sourceBattery, func(r *vesselRoles) *string { return &r.VoltageReadoutBattery }},
	{"shore_charger", "the shore charger", sourceCharger, func(r *vesselRoles) *string { return &r.ShoreCharger }},
	{"inverter", "the inverter", sourceInverter, func(r *vesselRoles) *string { return &r.Inverter }},
	{"generator", "the generator", sourceGenerator, func(r *vesselRoles) *string { return &r.Generator }},
}

func normalizeVesselRoles(r vesselRoles) vesselRoles {
	for _, spec := range roleSpecs {
		p := spec.get(&r)
		*p = strings.TrimSpace(*p)
	}
	r.GeneratorACInstance = strings.TrimSpace(r.GeneratorACInstance)
	return r
}

// validateVesselRoles checks a roles block against the items that own a source:
// each role names an item that exists, owns a source, and is of the right kind;
// the two alternators differ. A blank role is always valid (not set). items is
// every item that currently owns a source.
//
// current, when not nil, is the block already saved. A role left exactly as it
// is not checked again: an item that lost its source since the role was chosen
// leaves that role unset in the vessel (loudly, in the log), and must not stop
// the operator saving an unrelated setting. Only a role being changed has to
// name a fitting item.
func validateVesselRoles(r vesselRoles, items []sourceItem, current *vesselRoles) *settingsValidationError {
	byID := map[string]sourceItem{}
	for _, it := range items {
		byID[it.ID] = it
	}
	for _, spec := range roleSpecs {
		id := *spec.get(&r)
		if id == "" {
			continue
		}
		if current != nil && *spec.get(current) == id {
			continue
		}
		it, ok := byID[id]
		if !ok {
			return &settingsValidationError{Field: "roles." + spec.field, Message: spec.label + " must be an equipment item that is assigned a SignalK source"}
		}
		if it.Group != spec.group {
			return &settingsValidationError{Field: "roles." + spec.field, Message: fmt.Sprintf("%s must be a %s item, and %q is assigned to %s", spec.label, spec.group, it.Name, it.Source)}
		}
	}
	if r.PortAlternator != "" && r.PortAlternator == r.StarboardAlternator {
		return &settingsValidationError{Field: "roles.starboard_alternator", Message: "port and starboard must be different alternators"}
	}
	if r.GeneratorACInstance != "" && !alternatorInstanceIDRe.MatchString(r.GeneratorACInstance) {
		return &settingsValidationError{Field: "roles.generator_ac_instance", Message: "the generator's AC input must be an instance id (letters, digits, _ or -)"}
	}
	return nil
}

// rolesBlockYAML is the "roles" key of an already-read settings map as YAML,
// nil when the key is absent or null.
func rolesBlockYAML(settings map[string]any) ([]byte, error) {
	raw, ok := settings["roles"]
	if !ok || raw == nil {
		return nil, nil
	}
	buf, err := yaml.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("roles settings: re-marshalling roles block: %w", err)
	}
	return buf, nil
}

// decodeRolesYAML decodes a roles block; nil is the zero value (nothing set).
// A block that does not parse is an error, not "nothing set".
func decodeRolesYAML(buf []byte) (vesselRoles, error) {
	var result vesselRoles
	if buf == nil {
		return result, nil
	}
	if err := yaml.Unmarshal(buf, &result); err != nil {
		return vesselRoles{}, fmt.Errorf("roles settings: parsing roles block: %w", err)
	}
	return normalizeVesselRoles(result), nil
}

// parseRolesBlock decodes the "roles" key of an already-read settings map.
func parseRolesBlock(settings map[string]any) (vesselRoles, error) {
	buf, err := rolesBlockYAML(settings)
	if err != nil {
		return vesselRoles{}, err
	}
	return decodeRolesYAML(buf)
}

// rolesBlockMap turns r into the generic map settings.yaml stores under
// "roles".
func rolesBlockMap(r vesselRoles) (map[string]any, error) {
	buf, err := yaml.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("roles settings: marshalling roles block: %w", err)
	}
	var generic map[string]any
	if err := yaml.Unmarshal(buf, &generic); err != nil {
		return nil, fmt.Errorf("roles settings: round-tripping roles block: %w", err)
	}
	return generic, nil
}
