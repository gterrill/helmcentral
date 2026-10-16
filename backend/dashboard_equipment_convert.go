package main

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"strings"
)

// dashboard_equipment_convert.go is the one-off conversion of dashboard tiles
// from full SignalK paths to equipment items (ADR 0185, phase 2). It runs at
// startup after the vessel conversion has given items their sources, and
// finishes by recording that it has, so a plain group of paths made later is
// never touched.
//
// A tile whose paths sit under the source of a mapped item becomes a reference
// to that item and keeps its display choices. A tile that matches no item
// keeps those choices as readings under no item, and asks for one ("Pick
// equipment"); nothing is guessed. Zones stored on a tile move to the item's
// profile when the profile has none for that reading, and are dropped (and
// logged) when it has its own, because a profile is the one place a zone now
// lives.

type tileConverter struct {
	ix     equipmentIndex
	edited map[string]bool // profile ids whose zones changed
	logged map[string]bool
}

func (c *tileConverter) logOnce(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if c.logged[msg] {
		return
	}
	c.logged[msg] = true
	log.Printf("dashboard conversion: %s", msg)
}

// convertDashboardTilesToEquipment converts every tile that still holds paths.
// Fail fast: a profile or the pages file that cannot be written is returned
// and stops startup, because a half-converted dashboard would read as one the
// operator broke.
func convertDashboardTilesToEquipment() error {
	dashboardPagesMu.Lock()
	defer dashboardPagesMu.Unlock()
	if dashboardEquipmentConverted {
		return nil
	}

	invalidateEquipmentRefs()
	refs, err := listEquipmentRefs()
	if err != nil {
		return fmt.Errorf("convert dashboard tiles: %w", err)
	}
	profiles, _ := engineProfiles()
	c := &tileConverter{
		ix:     newEquipmentIndex(refs, profiles),
		edited: map[string]bool{},
		logged: map[string]bool{},
	}

	for _, page := range orderedDashboardPagesLocked() {
		for i := range page.Widgets {
			w := &page.Widgets[i]
			switch {
			case w.Cluster != nil:
				c.convertCluster(page.Name, w.ID, w.Cluster)
			case w.GaugeGroup != nil:
				c.convertGaugeGroup(page.Name, w.ID, w.GaugeGroup)
			case w.Lamps != nil:
				c.convertLamps(page.Name+" "+w.ID, w.Lamps.Lamps)
			}
		}
	}
	if dashboardRibbonState != nil {
		c.convertLamps("the ribbon", dashboardRibbonState.Lamps)
	}

	if len(c.edited) > 0 {
		if globalProfileStore == nil {
			return fmt.Errorf("convert dashboard tiles: the profile store is not open")
		}
		err := globalProfileStore.inTx(func(tx *sql.Tx) error {
			for id := range c.edited {
				if err := updateProfileTx(tx, c.ix.profiles[id]); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("convert dashboard tiles: save profile zones: %w", err)
		}
		loadEngineProfiles()
	}

	dashboardEquipmentConverted = true
	if err := saveDashboardPagesLocked(); err != nil {
		return fmt.Errorf("convert dashboard tiles: save pages: %w", err)
	}
	return nil
}

// convertSlot rewrites one path slot into a suffix of src. It reports false for
// a slot that cannot stay: one that reads outside the tile's source, which a
// tile showing one item has no way to say.
func (c *tileConverter) convertSlot(slot *dashboardGaugeConfig, src string, item equipmentRef, found bool, where string) bool {
	path := strings.TrimSpace(slot.Path)
	if path == "" {
		return true
	}
	source, suffix, ok := splitSourcePath(path)
	if !ok || source != src {
		c.logOnce("%s: dropped the reading %q (%s), which is not under %s", where, slot.Label, path, src)
		return false
	}
	slot.Path, slot.Suffix = "", suffix
	if len(slot.Zones) > 0 && c.moveZones(slot, item, found, where) {
		slot.Zones = nil
	}
	return true
}

// moveZones carries a slot's zones to the gauge of the item's profile that
// reads the same suffix, when that gauge has none of its own, and reports
// whether the slot's zones can be cleared (they moved, or the profile already
// holds exactly the same). When they cannot move, they stay on the tile, which
// keeps raising their alarms, and the log says why.
func (c *tileConverter) moveZones(slot *dashboardGaugeConfig, item equipmentRef, found bool, where string) bool {
	label := firstNonEmpty(slot.Label, slot.Suffix)
	keep := func(format string, args ...any) bool {
		c.logOnce("%s: kept the zones on %q on the tile; "+format, append([]any{where, label}, args...)...)
		return false
	}
	if !found {
		return keep("the tile shows no equipment item yet, so there is no profile to hold them")
	}
	profile, ok := c.ix.profiles[item.ProfileID]
	if !ok {
		return keep("%s has no equipment profile to hold them", item.Name)
	}
	index := -1
	for i, g := range profile.Gauges {
		if g.PathSuffix == slot.Suffix {
			index = i
			break
		}
	}
	if index < 0 {
		return keep("profile %q has no %s gauge to hold them", profile.Name, slot.Suffix)
	}

	gauge := profile.Gauges[index]
	// The tile's thresholds are in the tile's unit and its scale; the profile
	// holds them in its gauge's unit. Convert, or keep them on the tile.
	// The tile judged its bands against its own scale, 0..100 when it set none,
	// so that is the scale to read them against here.
	min, max := gaugeScale(slot.Min, slot.Max, nil)
	tileZones, unrepresentable := storedZonesToProfile(slot.Zones, min, max)
	if unrepresentable {
		return keep("a band floats mid-scale, and a profile zone is a direction and a threshold")
	}
	if slot.Quantity != gauge.Quantity || slot.Unit != gauge.Unit {
		converted := make([]engineProfileZone, 0, len(tileZones))
		for _, z := range tileZones {
			v, err := convertBetweenUnits(*z.Threshold, slot.Quantity, slot.Unit, gauge.Quantity, gauge.Unit)
			if err != nil {
				return keep("its unit %s %s does not convert to the profile's %s %s: %v", slot.Quantity, slot.Unit, gauge.Quantity, gauge.Unit, err)
			}
			z.Threshold = &v
			converted = append(converted, z)
		}
		tileZones = converted
		// A scale put on a profile gauge that has none is in the profile's unit.
		pmin, err1 := convertBetweenUnits(min, slot.Quantity, slot.Unit, gauge.Quantity, gauge.Unit)
		pmax, err2 := convertBetweenUnits(max, slot.Quantity, slot.Unit, gauge.Quantity, gauge.Unit)
		if err1 == nil && err2 == nil {
			min, max = pmin, pmax
		}
	}

	var existing []engineProfileZone
	var slots []engineProfileZone
	for _, z := range gauge.Zones {
		if z.Threshold == nil {
			slots = append(slots, z)
		} else {
			existing = append(existing, z)
		}
	}
	switch {
	case sameProfileZones(existing, tileZones):
		return true
	case len(existing) > 0:
		return keep("profile %q already has different zones for it, which apply until you choose which set to keep", profile.Name)
	}

	gauge.Zones = append(slots, tileZones...)
	if gauge.Min == nil || gauge.Max == nil {
		gauge.Min, gauge.Max = &min, &max
	}
	next := profile
	next.Gauges = append([]engineProfileGauge(nil), profile.Gauges...)
	next.Gauges[index] = gauge
	if err := validateEngineProfile(next); err != nil {
		return keep("profile %q would not accept them: %v", profile.Name, err)
	}
	c.ix.profiles[profile.ID] = next
	c.edited[profile.ID] = true
	return true
}

// storedZonesToProfile turns bands into the direction and threshold a profile
// holds. A band touching neither end of the scale has no such form and is
// reported rather than guessed at.
func storedZonesToProfile(zones []gaugeZone, min, max float64) (out []engineProfileZone, unrepresentable bool) {
	epsilon := (max - min) * 0.001
	for _, z := range zones {
		threshold := 0.0
		direction := ""
		switch {
		case z.From <= min+epsilon && z.To < max-epsilon:
			direction, threshold = "below", z.To
		case z.To >= max-epsilon && z.From > min+epsilon:
			direction, threshold = "above", z.From
		default:
			unrepresentable = true
			continue
		}
		t := threshold
		out = append(out, engineProfileZone{Direction: direction, Threshold: &t, State: z.State})
	}
	return out, unrepresentable
}

func sameProfileZones(a, b []engineProfileZone) bool {
	if len(a) != len(b) {
		return false
	}
	used := make([]bool, len(b))
	for _, x := range a {
		match := false
		for j, y := range b {
			if used[j] || x.Direction != y.Direction || x.State != y.State || x.Threshold == nil || y.Threshold == nil {
				continue
			}
			if math.Abs(*x.Threshold-*y.Threshold) < 1e-9 {
				used[j], match = true, true
				break
			}
		}
		if !match {
			return false
		}
	}
	return true
}

func (c *tileConverter) convertCluster(page, id string, cl *dashboardClusterConfig) {
	where := fmt.Sprintf("%s, %s", page, id)
	src, _, ok := splitSourcePath(cl.Ring.Path)
	item, found := c.ix.bySource[src]
	if !ok || (found && item.Group != sourceEngine) {
		found = false
		if ok {
			c.logOnce("%s: %s is not an engine, so the cluster asks for one", where, src)
		}
	}
	if found {
		cl.EquipmentID = item.ID
	} else if ok {
		// The tile remembers what it read, so the zones it keeps still alarm.
		cl.Source = src
		c.logOnce("%s: no equipment item owns %s, so the cluster asks for one", where, src)
	}

	keep := func(slot *dashboardGaugeConfig) bool {
		if !ok {
			// No source to hold the slots to: keep each reading's suffix when
			// it has a recognisable one, else leave it blank to be chosen.
			if _, suffix, split := splitSourcePath(slot.Path); split {
				slot.Suffix = suffix
			}
			slot.Path, slot.Zones = "", nil
			return true
		}
		return c.convertSlot(slot, src, item, found, where)
	}

	// The ring and centre always exist, so a reading that cannot stay is
	// blanked to be chosen again rather than removed.
	for _, slot := range []*dashboardGaugeConfig{&cl.Ring, &cl.Centre} {
		if !keep(slot) {
			slot.Path, slot.Suffix, slot.Zones = "", "", nil
		}
	}
	for ci := range cl.Corners {
		rows := cl.Corners[ci].Rows[:0]
		for ri := range cl.Corners[ci].Rows {
			row := cl.Corners[ci].Rows[ri]
			if keep(&row) {
				rows = append(rows, row)
			}
		}
		cl.Corners[ci].Rows = rows
	}
	// A corner left with no rows is not a valid corner.
	corners := cl.Corners[:0]
	for _, corner := range cl.Corners {
		if len(corner.Rows) > 0 {
			corners = append(corners, corner)
		}
	}
	cl.Corners = corners
	telltales := cl.Telltales[:0]
	for ti := range cl.Telltales {
		t := cl.Telltales[ti]
		if keep(&t) {
			telltales = append(telltales, t)
		}
	}
	cl.Telltales = telltales

	if cl.Fuel != nil {
		for bi := range cl.Fuel.Bars {
			bar := &cl.Fuel.Bars[bi]
			if bar.Level == nil {
				continue
			}
			bar.Label = bar.Level.Label
			tank, _, split := splitSourcePath(bar.Level.Path)
			if owner, owned := c.ix.bySource[tank]; split && owned && owner.Group == sourceTank {
				bar.TankID = owner.ID
			} else {
				if split {
					bar.Source = tank
				}
				c.logOnce("%s: no equipment item owns the tank %q, so its fuel bar asks for one", where, bar.Level.Path)
			}
			if len(bar.Level.Zones) > 0 {
				bar.Zones = c.fuelZonesInPercent(where, bar.Level)
			}
			bar.Level, bar.Capacity = nil, nil
		}
	}
}

func (c *tileConverter) convertGaugeGroup(page, id string, g *dashboardGaugeGroupConfig) {
	if g.EquipmentKind != "" {
		return
	}
	where := fmt.Sprintf("%s, %s", page, id)
	src := ""
	for _, gauge := range g.Gauges {
		source, _, ok := splitSourcePath(gauge.Path)
		if !ok || (src != "" && source != src) {
			return // a plain group of paths stays one
		}
		src = source
	}
	if src == "" {
		return
	}
	group, _, _ := parseSource(src)
	g.EquipmentKind = string(group)
	item, found := c.ix.bySource[src]
	if found {
		g.EquipmentID = item.ID
	} else {
		g.Source = src
		c.logOnce("%s: no equipment item owns %s, so the group asks for one", where, src)
	}
	for i := range g.Gauges {
		c.convertSlot(&g.Gauges[i], src, item, found, where)
	}
}

func (c *tileConverter) convertLamps(where string, lamps []dashboardLamp) {
	for i := range lamps {
		l := &lamps[i]
		if l.EquipmentID != "" || strings.TrimSpace(l.Path) == "" {
			continue
		}
		source, suffix, ok := splitSourcePath(l.Path)
		if !ok {
			continue
		}
		item, found := c.ix.bySource[source]
		if !found {
			c.logOnce("%s: lamp %q still reads %s; no equipment item owns it", where, l.Label, l.Path)
			continue
		}
		l.EquipmentID, l.Suffix, l.Path = item.ID, suffix, ""
	}
}

// fuelZonesInPercent carries a fuel bar's zones onto the bar. A tank has no
// profile, so the bar keeps them (as bands of the level in percent), and the
// low-fuel alarm they raise is kept. A unit other than percent is converted.
func (c *tileConverter) fuelZonesInPercent(where string, level *dashboardGaugeConfig) []gaugeZone {
	if level.Quantity == "ratio" && level.Unit == "percent" {
		return level.Zones
	}
	var out []gaugeZone
	for _, z := range level.Zones {
		from, err1 := convertBetweenUnits(z.From, level.Quantity, level.Unit, "ratio", "percent")
		to, err2 := convertBetweenUnits(z.To, level.Quantity, level.Unit, "ratio", "percent")
		if err1 != nil || err2 != nil {
			c.logOnce("%s: dropped a zone on the fuel bar %q; its unit %s %s does not convert to percent", where, level.Label, level.Quantity, level.Unit)
			continue
		}
		out = append(out, gaugeZone{From: from, To: to, State: z.State})
	}
	return out
}
