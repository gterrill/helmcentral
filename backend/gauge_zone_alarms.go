package main

import (
	"fmt"
	"sort"
	"strings"
)

// Gauge zones as the alarm source (ADR 0050).
//
// Before this, a gauge rendering a red band raised nothing and an alarm firing
// coloured no gauge — two threshold systems, neither aware of the other. An
// operator setting an engine-temperature red band reasonably expects that to
// be the alarm, which is how N2KView behaves and how the three-level scan
// (glance at colour, read the gauge, check history) is meant to work.
//
// Rules are derived at evaluation time rather than materialised into
// alarm-rules.json, following gaugeBoundPaths(): the backend already owns the
// page config, so there is nothing to keep in sync and no migration when a
// zone is edited.

const zoneDerivedAlarmRuleIDPrefix = "zone:"

// Where a cluster's arcs and footer sit in the flat index space of the ids of
// zones a slot still holds (see collectSlot). The ring, centre and corner rows
// come first. 18 is where the old telltales began (2 + 4 corners * 4 rows, before
// a corner held one row), a position that shipped; a telltale moved onto an arc
// keeps its own old index (dashboardGaugeConfig.ZoneIndex), so no issued id
// changes. The footer and both arcs' defaults are new in this release, so they
// follow without gaps and never share a telltale's: footer slot i is
// clusterFooterZoneIndexBase+i, in the order the frontend's
// CLUSTER_FOOTER_SLOTS lists (a test pins the two together).
const (
	clusterTelltaleZoneIndexBase = 18
	clusterFooterZoneIndexBase   = 24
	clusterFooterSlotCount       = 8
	clusterRightArcZoneIndex     = clusterFooterZoneIndexBase + clusterFooterSlotCount
	clusterLeftArcZoneIndex      = clusterRightArcZoneIndex + 1
)

const (
	// A value hovering at a threshold is the reason people switch marine
	// alarms off entirely. A zone carries no opinion about either of these, so
	// both get a defensible default rather than zero.
	zoneDerivedDwellSeconds = 10
	// As a fraction of the gauge's own range, so it scales with the scale.
	zoneDerivedHysteresisFraction = 0.02
)

func isZoneDerivedAlarmRuleID(id string) bool {
	return strings.HasPrefix(id, zoneDerivedAlarmRuleIDPrefix)
}

// zoneAlarmThreshold turns a band into the single threshold an alarmRule
// carries. A band touching the bottom of the scale fires as the value falls
// past its upper edge; one touching the top fires as it rises past its lower
// edge. A band floating in the middle has no single-threshold equivalent —
// reported here as not-ok, and rejected at save time so it can never be the
// silent reason an alarm does not fire.
func zoneAlarmThreshold(zone gaugeZone, min, max float64) (op string, threshold float64, ok bool) {
	// Touching within a hair of the end counts as touching it: an operator
	// dragging a band to the end of a 0-3000 scale should not have to land on
	// exactly 0.
	epsilon := (max - min) * 0.001

	atBottom := zone.From <= min+epsilon
	atTop := zone.To >= max-epsilon

	switch {
	case atBottom && atTop:
		// The band covers the whole scale, so it is always in force. That is
		// not a threshold, it is a mistake.
		return "", 0, false
	case atBottom:
		return alarmOpBelow, zone.To, true
	case atTop:
		return alarmOpAbove, zone.From, true
	default:
		return "", 0, false
	}
}

// gaugeZoneRule builds one derived rule, or reports why it cannot.
func gaugeZoneRule(id string, gauge dashboardGaugeConfig, zone gaugeZone, zoneIndex int) (alarmRule, error) {
	if zone.State == alarmStateNormal {
		return alarmRule{}, fmt.Errorf("normal bands are not alarms")
	}

	min, max := gaugeScale(gauge.Min, gauge.Max, nil)
	if max <= min {
		return alarmRule{}, fmt.Errorf("gauge range is not usable")
	}

	op, threshold, ok := zoneAlarmThreshold(zone, min, max)
	if !ok {
		return alarmRule{}, fmt.Errorf("zone %d is not anchored to either end of the scale", zoneIndex+1)
	}

	// Zones are authored in the display unit; alarmReader reads SI.
	value, err := convertToSI(threshold, gauge.Quantity, gauge.Unit)
	if err != nil {
		return alarmRule{}, err
	}
	hysteresisSI, err := zoneHysteresisSI(gauge, min, max)
	if err != nil {
		return alarmRule{}, err
	}

	label := strings.TrimSpace(gauge.Label)
	if label == "" {
		label = gauge.Path
	}

	return alarmRule{
		ID:           id,
		Enabled:      true,
		Path:         strings.TrimSpace(gauge.Path),
		Label:        label,
		Op:           op,
		Value:        value,
		Hysteresis:   hysteresisSI,
		DwellSeconds: zoneDerivedDwellSeconds,
		State:        zone.State,
		Methods:      []string{"visual", "sound"},
	}, nil
}

// zoneHysteresisSI expresses the deadband in SI. It is a span, not a point, so
// it is the difference between two converted values rather than one converted
// value — the distinction matters for temperature, where the scales are offset
// (2 °C of deadband is 2 K, not 275.15 K).
func zoneHysteresisSI(gauge dashboardGaugeConfig, min, max float64) (float64, error) {
	span := (max - min) * zoneDerivedHysteresisFraction

	zero, err := convertToSI(min, gauge.Quantity, gauge.Unit)
	if err != nil {
		return 0, err
	}
	offset, err := convertToSI(min+span, gauge.Quantity, gauge.Unit)
	if err != nil {
		return 0, err
	}

	hysteresis := offset - zero
	if hysteresis < 0 {
		hysteresis = -hysteresis
	}
	return hysteresis, nil
}

// zoneDerivedAlarmRules walks every gauge on every page and returns one rule per
// alarm-severity zone.
//
// A standalone gauge, or a member of a plain gauge group, carries its own zones
// and is keyed by its widget. A slot of a tile that shows an equipment item
// (ADR 0187) has none: its zones are read live from the gauge of the item's
// profile with the same suffix, the same ones the tile draws. Those rules are
// keyed by item and reading rather than by tile, so two tiles showing the same
// item's oil pressure raise one alarm, and editing a profile's zones changes
// the alarm and every tile at once.
//
// Zones that cannot become a rule are skipped rather than reported here:
// validateGaugeConfig rejects them at save time, so anything that reaches this
// point and still fails is a config written before that rule existed.
func zoneDerivedAlarmRules() []alarmRule {
	dashboardPagesMu.RLock()
	defer dashboardPagesMu.RUnlock()

	ix := currentEquipmentIndex()
	seen := map[string]bool{}
	var rules []alarmRule
	add := func(id string, gauge dashboardGaugeConfig) {
		for zoneIndex, zone := range gauge.Zones {
			ruleID := fmt.Sprintf("%s%s:%d", zoneDerivedAlarmRuleIDPrefix, id, zoneIndex)
			if seen[ruleID] {
				continue
			}
			rule, err := gaugeZoneRule(ruleID, gauge, zone, zoneIndex)
			if err != nil {
				continue
			}
			seen[ruleID] = true
			rules = append(rules, rule)
		}
	}
	collect := func(widgetID string, gaugeIndex int, gauge dashboardGaugeConfig) {
		add(fmt.Sprintf("%s:%d", widgetID, gaugeIndex), gauge)
	}
	// A slot's alarm comes from one of two places. When the profile has zones
	// for the reading they are the alarm: one per item and reading, built from
	// the item and the profile gauge alone (never from a slot), so its label,
	// scale and unit are the same however many tiles show the reading and
	// whichever the walk meets first. Otherwise any zones the slot itself still
	// holds (a tile converted before there was equipment or a profile to hold
	// them) are the alarm, under the id they have always had.
	seenReading := map[string]bool{}
	collectSlot := func(widgetID string, index int, equipmentID, tileSource, title string, slot dashboardGaugeConfig) {
		suffix := strings.TrimSpace(slot.Suffix)
		if suffix == "" {
			return
		}
		// A slot that names its own item reads that item or nothing: if it is gone
		// or unmapped there is no reading and no alarm, never the engine's source.
		if slot.EquipmentID != "" {
			if own, found := ix.items[slot.EquipmentID]; !found || own.Source == "" {
				return
			}
		}
		if slot.ZoneIndex != nil {
			index = *slot.ZoneIndex
		}
		item, hasItem := ix.items[equipmentID]
		if hasItem && item.Source != "" {
			if profile, ok := ix.profiles[item.ProfileID]; ok {
				if gauge, ok := profileGaugeFor(profile, suffix); ok && gaugeHasZones(gauge) {
					key := item.ID + "\x00" + suffix
					if seenReading[key] {
						return
					}
					seenReading[key] = true
					if reading, ok := ix.itemReading(item, suffix); ok {
						add(fmt.Sprintf("item:%s:%s", item.ID, suffix), reading)
					}
					return
				}
			}
		}
		if len(slot.Zones) == 0 {
			return
		}
		source, owner := tileSource, title
		if hasItem {
			owner = item.Name
			if item.Source != "" {
				source = item.Source
			}
		}
		if source == "" {
			return
		}
		held := slot
		held.Path = source + "." + suffix
		held.Label = strings.TrimSpace(owner + " " + firstNonEmpty(slot.Label, suffix))
		add(fmt.Sprintf("%s:%d", widgetID, index), held)
	}
	collectTank := func(bar dashboardClusterFuelBar) {
		if len(bar.Zones) == 0 {
			return
		}
		key, reading, ok := ix.tankLevelReading(bar)
		if !ok || seenReading[key] {
			return
		}
		seenReading[key] = true
		add(key, reading)
	}

	for _, page := range dashboardPagesState {
		for _, widget := range page.Widgets {
			if widget.Gauge != nil {
				collect(widget.ID, 0, *widget.Gauge)
			}
			if widget.GaugeGroup != nil {
				g := widget.GaugeGroup
				for i, gauge := range g.Gauges {
					if g.EquipmentKind == "" {
						collect(widget.ID, i, gauge)
					} else {
						collectSlot(widget.ID, i, g.EquipmentID, g.Source, g.Title, gauge)
					}
				}
			}
			// A cluster slot's zones alarm like any other gauge's. A fuel bar has
			// no profile to hold zones, so it keeps its own and they alarm on the
			// tank's level; a capacity is a constant an alarm would never clear.
			// The index space is flat and stable, so ids survive a restart: ring,
			// centre, then corner rows, with the telltales at a fixed base past
			// every corner row a cluster can hold.
			if widget.Cluster != nil {
				c := widget.Cluster
				collectSlot(widget.ID, 0, c.EquipmentID, c.Source, c.Title, c.Ring)
				collectSlot(widget.ID, 1, c.EquipmentID, c.Source, c.Title, c.Centre)
				// A row may carry the index it was issued (ZoneIndex); the
				// counter must not hand the same one to a row without.
				claimed := map[int]bool{}
				for _, corner := range c.Corners {
					for _, row := range corner.Rows {
						if row.ZoneIndex != nil {
							claimed[*row.ZoneIndex] = true
						}
					}
				}
				index := 2
				for _, corner := range c.Corners {
					for _, row := range corner.Rows {
						if row.ZoneIndex == nil {
							for claimed[index] {
								index++
							}
						}
						collectSlot(widget.ID, index, c.EquipmentID, c.Source, c.Title, row)
						if row.ZoneIndex == nil {
							index++
						}
					}
				}
				for i, slot := range c.Arcs.slots() {
					if slot == nil {
						continue
					}
					at := clusterLeftArcZoneIndex
					if i == 1 {
						at = clusterRightArcZoneIndex
					}
					collectSlot(widget.ID, at, slotItem(c.EquipmentID, *slot), c.Source, c.Title, *slot)
				}
				for i, slot := range c.Footer.slots() {
					if slot != nil {
						collectSlot(widget.ID, clusterFooterZoneIndexBase+i, slotItem(c.EquipmentID, *slot), c.Source, c.Title, *slot)
					}
				}
				if c.Fuel != nil {
					for _, bar := range c.Fuel.Bars {
						collectTank(bar)
					}
				}
			}
		}
	}

	// Stable order so the rule list does not reshuffle between ticks; ids are
	// unique by construction.
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	return rules
}

// slotItem is the item a slot reads: its own when it names one (a footer slot),
// else the tile's.
func slotItem(tileItem string, slot dashboardGaugeConfig) string {
	if slot.EquipmentID != "" {
		return slot.EquipmentID
	}
	return tileItem
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
