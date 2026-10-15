package main

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// dashboard_equipment.go is how a tile that shows a piece of equipment finds
// its readings (ADR 0185, phase 2). The tile stores the id of an equipment
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

// resolveSlot turns a slot on a tile that shows item equipmentID into a gauge
// with a path and the profile's zones. ok is false when the item does not
// exist, owns no source, or the slot names no reading.
func (ix equipmentIndex) resolveSlot(equipmentID string, slot dashboardGaugeConfig) (dashboardGaugeConfig, equipmentRef, bool) {
	item, found := ix.items[equipmentID]
	if !found || item.Source == "" || strings.TrimSpace(slot.Suffix) == "" {
		return dashboardGaugeConfig{}, item, false
	}
	out := slot
	out.Path = item.Source + "." + strings.TrimSpace(slot.Suffix)
	out.Zones = nil
	if profile, ok := ix.profiles[item.ProfileID]; ok {
		if gauge, ok := profileGaugeFor(profile, strings.TrimSpace(slot.Suffix)); ok {
			min, max := 0.0, 100.0
			switch {
			case slot.Min != nil:
				min = *slot.Min
			case gauge.Min != nil:
				min = *gauge.Min
			}
			switch {
			case slot.Max != nil:
				max = *slot.Max
			case gauge.Max != nil:
				max = *gauge.Max
			}
			out.Zones = profileZonesToStored(gauge.Zones, min, max)
		}
	}
	return out, item, true
}

// resolveTank gives the level and capacity readings of the tank item tankID.
func (ix equipmentIndex) resolveTank(tankID string) (level, capacity string, ok bool) {
	item, found := ix.items[tankID]
	if !found || item.Source == "" || item.Group != sourceTank {
		return "", "", false
	}
	return item.Source + ".currentLevel", item.Source + ".capacity", true
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
