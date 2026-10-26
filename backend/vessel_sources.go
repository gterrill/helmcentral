package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// vessel_sources.go is the vocabulary of an equipment item's SignalK source
// (ADR 0187). A source is the one SignalK node an item owns, written as the
// path of the instance, for example propulsion.port or
// electrical.batteries.1. Only the multi-instance groups have one: engines,
// batteries, alternators, solar controllers, tanks, generators, chargers and
// inverters. The group a source belongs to is what the rest of the backend
// calls the item's kind, so no separate kind column exists to disagree with
// it.

type sourceGroup string

const (
	sourceEngine     sourceGroup = "engine"
	sourceBattery    sourceGroup = "battery"
	sourceAlternator sourceGroup = "alternator"
	sourceSolar      sourceGroup = "solar"
	sourceTank       sourceGroup = "tank"
	sourceGenerator  sourceGroup = "generator"
	sourceCharger    sourceGroup = "charger"
	sourceInverter   sourceGroup = "inverter"
)

// sourceGroupOrder is the order groups are listed in, in Discovery and
// anywhere else a list of kinds is shown.
var sourceGroupOrder = []sourceGroup{
	sourceEngine, sourceBattery, sourceAlternator, sourceSolar,
	sourceTank, sourceGenerator, sourceCharger, sourceInverter,
}

// sourceKindNouns is how a kind is worded in a sentence to the operator. The
// one table: the setup status and the one-off conversion both read it, and a
// test checks that every group has an entry.
var sourceKindNouns = map[sourceGroup]string{
	sourceEngine: "engine", sourceBattery: "battery", sourceAlternator: "alternator", sourceSolar: "solar controller",
	sourceTank: "tank", sourceGenerator: "generator", sourceCharger: "charger", sourceInverter: "inverter",
}

type sourceShape struct {
	group sourceGroup
	re    *regexp.Regexp
}

// Instance ids are plain: letters, digits, _ and -, no dots or slashes, since
// the id is spliced into SignalK paths.
var sourceShapes = []sourceShape{
	{sourceEngine, regexp.MustCompile(`^propulsion\.([A-Za-z0-9_-]+)$`)},
	{sourceBattery, regexp.MustCompile(`^electrical\.batteries\.([A-Za-z0-9_-]+)$`)},
	{sourceAlternator, regexp.MustCompile(`^electrical\.alternator\.([A-Za-z0-9_-]+)$`)},
	{sourceSolar, regexp.MustCompile(`^electrical\.solar\.([A-Za-z0-9_-]+)$`)},
	{sourceTank, regexp.MustCompile(`^tanks\.([A-Za-z0-9_-]+\.[A-Za-z0-9_-]+)$`)},
	{sourceGenerator, regexp.MustCompile(`^electrical\.generator\.([A-Za-z0-9_-]+)$`)},
	{sourceCharger, regexp.MustCompile(`^electrical\.chargers\.([A-Za-z0-9_-]+)$`)},
	{sourceInverter, regexp.MustCompile(`^electrical\.inverters\.([A-Za-z0-9_-]+)$`)},
}

// parseSource splits a source into its group and instance. For a tank the
// instance is "<category>.<id>" (fuel.2), the key the tank tile has always
// used. ok is false for anything that is not one of the eight shapes.
func parseSource(source string) (group sourceGroup, instance string, ok bool) {
	for _, shape := range sourceShapes {
		if m := shape.re.FindStringSubmatch(source); m != nil {
			return shape.group, m[1], true
		}
	}
	return "", "", false
}

// sourcePath builds the source of an instance in a group.
func sourcePath(group sourceGroup, instance string) string {
	switch group {
	case sourceEngine:
		return "propulsion." + instance
	case sourceBattery:
		return "electrical.batteries." + instance
	case sourceAlternator:
		return "electrical.alternator." + instance
	case sourceSolar:
		return "electrical.solar." + instance
	case sourceTank:
		return "tanks." + instance
	case sourceGenerator:
		return "electrical.generator." + instance
	case sourceCharger:
		return "electrical.chargers." + instance
	case sourceInverter:
		return "electrical.inverters." + instance
	}
	return ""
}

// validSourceGroup reports whether g is one of the eight groups.
func validSourceGroup(g sourceGroup) bool {
	for _, known := range sourceGroupOrder {
		if g == known {
			return true
		}
	}
	return false
}

// validateSource checks that a non-blank source is one of the eight shapes.
// A blank source means the item owns none and is always valid.
func validateSource(source string) error {
	if source == "" {
		return nil
	}
	if _, _, ok := parseSource(source); !ok {
		return fmt.Errorf("%q is not a SignalK source an item can own (an engine, battery, alternator, solar controller, tank, generator, charger or inverter)", source)
	}
	return nil
}

// sourceSystem is the inventory system a new item for a group starts in.
func sourceSystem(group sourceGroup) string {
	switch group {
	case sourceEngine:
		return "propulsion"
	case sourceTank:
		return "fuel"
	default:
		return "electrical"
	}
}

// sourceHourMeterPath is the live hour meter a source carries, or "". Engines
// publish propulsion.<id>.runTime and the generator electrical.generator.<id>.runtime
// (both in seconds); no other group has one.
func sourceHourMeterPath(source string) string {
	group, _, ok := parseSource(source)
	if !ok {
		return ""
	}
	switch group {
	case sourceEngine:
		return source + ".runTime"
	case sourceGenerator:
		return source + ".runtime"
	}
	return ""
}

// sourceItem is an equipment item that owns a SignalK source, reduced to what
// the rest of the backend reads off it.
type sourceItem struct {
	ID          string
	Name        string
	Source      string
	Group       sourceGroup
	Instance    string
	ProfileID   string
	CapacityAh  float64
	Cells       int
	WarnVoltage float64
	HighVoltage float64
	// A tank's own alarm levels (ADR 0214); zero is blank.
	TankWarnPercent  float64
	TankAlarmPercent float64
	TankAlarmsOff    bool
}

func newSourceItem(it equipmentItem) (sourceItem, bool) {
	group, instance, ok := parseSource(strings.TrimSpace(it.SignalKSource))
	if !ok {
		return sourceItem{}, false
	}
	return sourceItem{
		ID: it.ID, Name: strings.TrimSpace(it.Name), Source: it.SignalKSource,
		Group: group, Instance: instance, ProfileID: it.ProfileID,
		CapacityAh: it.CapacityAh, Cells: it.Cells,
		WarnVoltage: it.WarnVoltage, HighVoltage: it.HighVoltage,
		TankWarnPercent: it.TankWarnPercent, TankAlarmPercent: it.TankAlarmPercent, TankAlarmsOff: it.TankAlarmsOff,
	}, true
}

// sortSourceItems orders items by group, then instance (numeric ids by value).
func sortSourceItems(items []sourceItem) {
	rank := map[sourceGroup]int{}
	for i, g := range sourceGroupOrder {
		rank[g] = i
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Group != items[j].Group {
			return rank[items[i].Group] < rank[items[j].Group]
		}
		return instanceLess(items[i].Instance, items[j].Instance)
	})
}
