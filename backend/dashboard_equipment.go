package main

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// dashboard_equipment.go is how a tile that shows a piece of equipment finds
// its readings (ADR 0187, phase 2). The tile stores the id of an equipment
// item and, per slot, a suffix such as oilPressure or fuel.rate. The path is
// the item's SignalK source plus the suffix, worked out each time it is read,
// so mapping an item to another source moves every tile that shows it. Gauge
// zones are read the same way, from the gauge of the item's profile with the
// same suffix, so a tile and the alarm raised from its zones can never
// disagree.

// dashboardSuffixPattern is what a slot suffix may hold. It is spliced onto a
// source to make a SignalK path, so no leading dot, no spaces.
var dashboardSuffixPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`)

const dashboardSuffixMaxLen = 96

// equipmentRef is an equipment item as a tile sees it: its name, the source it
// owns (if any) and the profile it follows.
type equipmentRef struct {
	ID        string
	Name      string
	Source    string
	Group     sourceGroup
	ProfileID string
}

// equipmentIndex is every item by id plus every profile by id, read once and
// used for a whole walk of the dashboard pages.
type equipmentIndex struct {
	items    map[string]equipmentRef
	bySource map[string]equipmentRef
	profiles map[string]engineProfile
}

func newEquipmentIndex(refs []equipmentRef, profiles []engineProfile) equipmentIndex {
	ix := equipmentIndex{
		items:    make(map[string]equipmentRef, len(refs)),
		bySource: make(map[string]equipmentRef, len(refs)),
		profiles: make(map[string]engineProfile, len(profiles)),
	}
	for _, ref := range refs {
		ix.items[ref.ID] = ref
		if ref.Source != "" {
			ix.bySource[ref.Source] = ref
		}
	}
	for _, p := range profiles {
		ix.profiles[p.ID] = p
	}
	return ix
}

// EquipmentRefs reads every item's id, name, source and profile.
func (s *documentStore) EquipmentRefs() ([]equipmentRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, name, signalk_source, profile_id FROM equipment`)
	if err != nil {
		return nil, fmt.Errorf("list equipment refs: %w", err)
	}
	defer rows.Close()
	var refs []equipmentRef
	for rows.Next() {
		var ref equipmentRef
		if err := rows.Scan(&ref.ID, &ref.Name, &ref.Source, &ref.ProfileID); err != nil {
			return nil, fmt.Errorf("list equipment refs: %w", err)
		}
		ref.Name = strings.TrimSpace(ref.Name)
		ref.Source = strings.TrimSpace(ref.Source)
		if group, _, ok := parseSource(ref.Source); ok {
			ref.Group = group
		} else {
			ref.Source = ""
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list equipment refs: %w", err)
	}
	return refs, nil
}

var equipmentRefsCache = struct {
	mu    sync.Mutex
	store *documentStore
	at    time.Time
	refs  []equipmentRef
	ok    bool
}{}

func invalidateEquipmentRefs() {
	equipmentRefsCache.mu.Lock()
	equipmentRefsCache.ok = false
	equipmentRefsCache.refs = nil
	equipmentRefsCache.mu.Unlock()
}

func listEquipmentRefs() ([]equipmentRef, error) {
	store := globalDocumentStore
	if store == nil {
		return nil, nil
	}
	equipmentRefsCache.mu.Lock()
	if equipmentRefsCache.ok && equipmentRefsCache.store == store && time.Since(equipmentRefsCache.at) < sourceItemsTTL {
		refs := equipmentRefsCache.refs
		equipmentRefsCache.mu.Unlock()
		return refs, nil
	}
	equipmentRefsCache.mu.Unlock()

	refs, err := store.EquipmentRefs()
	if err != nil {
		return nil, err
	}
	equipmentRefsCache.mu.Lock()
	equipmentRefsCache.store, equipmentRefsCache.refs, equipmentRefsCache.at, equipmentRefsCache.ok = store, refs, time.Now(), true
	equipmentRefsCache.mu.Unlock()
	return refs, nil
}

// currentEquipmentIndex reads the items and profiles as they stand now. An
// unreadable store is logged once and reads as empty, so every tile shows no
// data rather than a stale answer.
func currentEquipmentIndex() equipmentIndex {
	refs, err := listEquipmentRefs()
	noteVesselRead("dashboard equipment", err, "dashboard tiles: could not read the equipment items: %v")
	profiles, _ := engineProfiles()
	return newEquipmentIndex(refs, profiles)
}

// splitSourcePath splits a SignalK path into the source of the instance it
// sits under and the rest, for example propulsion.port.fuel.rate into
// propulsion.port and fuel.rate. ok is false for a path under none of the
// eight multi-instance groups, or one that is only a source.
func splitSourcePath(path string) (source, suffix string, ok bool) {
	segments := strings.Split(strings.TrimSpace(path), ".")
	for n := 2; n <= 3 && n < len(segments); n++ {
		candidate := strings.Join(segments[:n], ".")
		if _, _, found := parseSource(candidate); found {
			return candidate, strings.Join(segments[n:], "."), true
		}
	}
	return "", "", false
}

// profileGaugeFor is the gauge of a profile that reads suffix.
func profileGaugeFor(p engineProfile, suffix string) (engineProfileGauge, bool) {
	for _, g := range p.Gauges {
		if g.PathSuffix == suffix {
			return g, true
		}
	}
	return engineProfileGauge{}, false
}

// profileZonesToStored turns a profile gauge's zones into the from/to bands a
// tile draws, anchored to the scale. A zone whose threshold is not set (a slot
// the manufacturer does not publish) is left out: it must never become a band
// at a number nobody chose.
func profileZonesToStored(zones []engineProfileZone, min, max float64) []gaugeZone {
	var out []gaugeZone
	for _, z := range zones {
		if z.Threshold == nil {
			continue
		}
		if z.Direction == "below" {
			out = append(out, gaugeZone{From: min, To: *z.Threshold, State: z.State})
		} else {
			out = append(out, gaugeZone{From: *z.Threshold, To: max, State: z.State})
		}
	}
	return out
}

// gaugeScale is the scale a slot's zones are anchored to: the slot's own
// minimum and maximum when it sets them, else the profile gauge's, else 0..100.
// It is the one place that rule lives; resolving a slot, building an alarm and
// moving a tile's zones onto a profile all call it.
func gaugeScale(slotMin, slotMax *float64, gauge *engineProfileGauge) (min, max float64) {
	min, max = 0, 100
	switch {
	case slotMin != nil:
		min = *slotMin
	case gauge != nil && gauge.Min != nil:
		min = *gauge.Min
	}
	switch {
	case slotMax != nil:
		max = *slotMax
	case gauge != nil && gauge.Max != nil:
		max = *gauge.Max
	}
	return min, max
}

// gaugeHasZones reports whether a profile gauge holds any zone with a
// threshold. A zone with none is a slot the manufacturer leaves blank and
// counts for nothing.
func gaugeHasZones(g engineProfileGauge) bool {
	for _, z := range g.Zones {
		if z.Threshold != nil {
			return true
		}
	}
	return false
}

// resolveSlot turns a slot on a tile that shows item equipmentID into a gauge
// with a path and the profile's zones. ok is false when the item does not
// exist, owns no source, or the slot names no reading.
//
// When the profile has a gauge for the reading and its zones apply (they win
// whenever the profile has any, and are all there is when the slot holds none),
// the quantity and unit are the profile's, because its thresholds are in that
// unit, and the returned slot carries the scale its zones were anchored to. A
// slot that still holds zones of its own, with none on the profile to hold
// them, keeps them, and its own unit and scale.
func (ix equipmentIndex) resolveSlot(equipmentID string, slot dashboardGaugeConfig) (dashboardGaugeConfig, equipmentRef, bool) {
	// A footer slot may read another item than the tile's own.
	if slot.EquipmentID != "" {
		equipmentID = slot.EquipmentID
	}
	item, found := ix.items[equipmentID]
	if !found || item.Source == "" || strings.TrimSpace(slot.Suffix) == "" {
		return dashboardGaugeConfig{}, item, false
	}
	suffix := strings.TrimSpace(slot.Suffix)
	out := slot
	out.Path = item.Source + "." + suffix
	// Zones the slot still holds stay unless the profile's replace them below.
	if profile, ok := ix.profiles[item.ProfileID]; ok {
		if gauge, ok := profileGaugeFor(profile, suffix); ok && (len(slot.Zones) == 0 || gaugeHasZones(gauge)) {
			min, max := gaugeScale(slot.Min, slot.Max, &gauge)
			out.Quantity, out.Unit = gauge.Quantity, gauge.Unit
			out.Min, out.Max = &min, &max
			out.Zones = profileZonesToStored(gauge.Zones, min, max)
		}
	}
	return out, item, true
}

// itemReading is the gauge an alarm is raised from for one reading of an item:
// built from the item and its profile's gauge alone, so what the alarm says and
// where it fires never depends on which tile happened to show the reading.
// ok is false when the item has no source or the profile has no such gauge.
func (ix equipmentIndex) itemReading(item equipmentRef, suffix string) (dashboardGaugeConfig, bool) {
	profile, found := ix.profiles[item.ProfileID]
	if !found || item.Source == "" {
		return dashboardGaugeConfig{}, false
	}
	gauge, found := profileGaugeFor(profile, suffix)
	if !found {
		return dashboardGaugeConfig{}, false
	}
	min, max := gaugeScale(nil, nil, &gauge)
	return dashboardGaugeConfig{
		Path:     item.Source + "." + suffix,
		Label:    strings.TrimSpace(item.Name + " " + firstNonEmpty(gauge.Label, suffix)),
		Display:  gauge.Display,
		Quantity: gauge.Quantity,
		Unit:     gauge.Unit,
		Min:      &min,
		Max:      &max,
		Zones:    profileZonesToStored(gauge.Zones, min, max),
	}, true
}

// resolveTank gives the level and capacity readings of the tank item tankID.
func (ix equipmentIndex) resolveTank(tankID string) (level, capacity string, ok bool) {
	item, found := ix.items[tankID]
	if !found || item.Source == "" || item.Group != sourceTank {
		return "", "", false
	}
	return item.Source + ".currentLevel", item.Source + ".capacity", true
}

// tankLevelReading is the gauge a low-fuel alarm is raised from: the tank's
// level as a percentage of full, with the zones the fuel bar carries. The tank
// is the item picked for the bar, or, while none is, the source the bar read.
func (ix equipmentIndex) tankLevelReading(bar dashboardClusterFuelBar) (key string, gauge dashboardGaugeConfig, ok bool) {
	min, max := 0.0, 100.0
	gauge = dashboardGaugeConfig{Display: "bar", Quantity: "ratio", Unit: "percent", Min: &min, Max: &max, Zones: bar.Zones}
	if level, _, resolved := ix.resolveTank(bar.TankID); resolved {
		gauge.Path, gauge.Label = level, ix.items[bar.TankID].Name+" level"
		return "tank:" + bar.TankID, gauge, true
	}
	if bar.Source != "" {
		gauge.Path = bar.Source + ".currentLevel"
		gauge.Label = firstNonEmpty(bar.Label, bar.Source) + " level"
		return "tank:" + bar.Source, gauge, true
	}
	return "", dashboardGaugeConfig{}, false
}

// resolveLamp gives the path a lamp reads: its own, or its item's source plus
// the suffix.
func (ix equipmentIndex) resolveLamp(l dashboardLamp) (string, bool) {
	if l.EquipmentID == "" {
		path := strings.TrimSpace(l.Path)
		return path, path != ""
	}
	item, found := ix.items[l.EquipmentID]
	if !found || item.Source == "" || strings.TrimSpace(l.Suffix) == "" {
		return "", false
	}
	return item.Source + "." + strings.TrimSpace(l.Suffix), true
}
