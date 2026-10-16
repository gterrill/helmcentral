package main

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// equipment_dependants.go answers "what stops working, or loses its source, if
// this equipment item is unmapped or deleted?" before the operator does it.
// The editor, Discovery and the tile picker ask for it ahead of any change
// that takes a source away from an item, and show it as a list to confirm.
//
// It is one function over the real stores, so a new consumer of an item has
// one place to register: a tile kind in collectDependantTiles, a role in
// roleSpecs, a rule kind in collectDependantRules, or a backend feature in
// dependantFeatures below.

const (
	dependantRuleStateOfCharge = "state_of_charge"
	dependantRuleZone          = "zone"
	dependantRuleOther         = "rule"

	ribbonPageName = "Indicator ribbon"
	ribbonTileID   = "ribbon"
)

type dependantTile struct {
	PageID   string `json:"page_id"`
	PageName string `json:"page_name"`
	TileID   string `json:"tile_id"`
	Title    string `json:"title"`
}

type dependantRole struct {
	Role  string `json:"role"`
	Label string `json:"label"`
}

type dependantAlarmRule struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
}

// equipmentDependants is everything that reads or follows one item. Every
// group is a non-nil slice so the JSON is arrays, never null.
type equipmentDependants struct {
	ItemID     string               `json:"item_id"`
	Tiles      []dependantTile      `json:"tiles"`
	Roles      []dependantRole      `json:"roles"`
	AlarmRules []dependantAlarmRule `json:"alarm_rules"`
	Features   []string             `json:"features"`
}

// dependantFeature is a backend feature that reads every mapped item of the
// listed kinds, named in the operator's words.
type dependantFeature struct {
	groups []sourceGroup
	label  string
}

// dependantFeatures is where a feature that reads mapped items registers. It
// applies only while the item owns a source: an unmapped item feeds nothing.
var dependantFeatures = []dependantFeature{
	{[]sourceGroup{sourceEngine}, "Anchor auto-raise"},
	{[]sourceGroup{sourceEngine}, "GNSS position jump check"},
	{[]sourceGroup{sourceEngine}, "Engine checks"},
	{[]sourceGroup{sourceEngine}, "Mate's passage fuel estimate"},
	{[]sourceGroup{sourceBattery}, "Battery names on alarms and tiles"},
	{[]sourceGroup{sourceSolar}, "Solar controller names on alarms and tiles"},
	{[]sourceGroup{sourceTank}, "The Tanks tile"},
}

// equipmentDependantsFor computes the dependants of the item id from the
// dashboard pages and ribbon, the roles block, the alarm rules (including the
// ones raised from gauge zones) and the feature table. It returns the store's
// not-found error for an item that does not exist.
func equipmentDependantsFor(id string) (equipmentDependants, error) {
	out := equipmentDependants{ItemID: id,
		Tiles: []dependantTile{}, Roles: []dependantRole{}, AlarmRules: []dependantAlarmRule{}, Features: []string{}}
	item, err := globalDocumentStore.GetEquipment(id)
	if err != nil {
		return out, err
	}
	source := strings.TrimSpace(item.SignalKSource)
	group, _, _ := parseSource(source)

	out.Tiles = collectDependantTiles(id, item.Name)

	roles, err := loadVesselRoles(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if err != nil {
		return out, err
	}
	for _, spec := range roleSpecs {
		if *spec.get(&roles) == id {
			out.Roles = append(out.Roles, dependantRole{Role: spec.field, Label: spec.title})
		}
	}

	out.AlarmRules = collectDependantRules(source, roles.HouseBank == id)

	if source != "" {
		for _, f := range dependantFeatures {
			for _, g := range f.groups {
				if g == group {
					out.Features = append(out.Features, f.label)
				}
			}
		}
	}
	return out, nil
}

// collectDependantTiles lists, once each, the tiles that show the item: an
// engine cluster or gauge group that follows it, a cluster whose fuel bar
// reads it as a tank, and a lamp strip or the ribbon with a lamp for it.
func collectDependantTiles(id, itemName string) []dependantTile {
	tiles := []dependantTile{}
	dashboardPagesMu.RLock()
	defer dashboardPagesMu.RUnlock()

	for _, page := range orderedDashboardPagesLocked() {
		for _, w := range page.Widgets {
			title, uses := dependantTileTitle(w, id, itemName)
			if uses {
				tiles = append(tiles, dependantTile{PageID: page.ID, PageName: page.Name, TileID: w.ID, Title: title})
			}
		}
	}
	if r := dashboardRibbonState; r != nil && lampsUse(r.Lamps, id) {
		tiles = append(tiles, dependantTile{PageName: ribbonPageName, TileID: ribbonTileID, Title: firstNonEmpty(r.Title, "Indicator lamps")})
	}
	return tiles
}

func lampsUse(lamps []dashboardLamp, id string) bool {
	for _, l := range lamps {
		if l.EquipmentID == id {
			return true
		}
	}
	return false
}

// dependantTileTitle says whether widget w shows the item, and what the tile is
// called: its own title, else the item's name, else the kind of tile.
func dependantTileTitle(w dashboardLayoutItem, id, itemName string) (string, bool) {
	if c := w.Cluster; c != nil {
		uses := c.EquipmentID == id
		if c.Fuel != nil {
			for _, bar := range c.Fuel.Bars {
				if bar.TankID == id {
					uses = true
				}
			}
		}
		return firstNonEmpty(c.Title, titleIfOwner(c.EquipmentID == id, itemName), "Engine cluster"), uses
	}
	if g := w.GaugeGroup; g != nil {
		return firstNonEmpty(g.Title, titleIfOwner(g.EquipmentID == id, itemName), "Gauge group"), g.EquipmentKind != "" && g.EquipmentID == id
	}
	if l := w.Lamps; l != nil {
		return firstNonEmpty(l.Title, "Indicator lamps"), lampsUse(l.Lamps, id)
	}
	return "", false
}

func titleIfOwner(owner bool, name string) string {
	if owner {
		return name
	}
	return ""
}

// collectDependantRules lists the alarm rules that follow the item: rules bound
// to the house bank role when the item is the bank, then every rule, saved or
// raised from a gauge zone, whose path sits under the item's source.
func collectDependantRules(source string, isHouseBank bool) []dependantAlarmRule {
	rules := []dependantAlarmRule{}
	seen := map[string]bool{}
	add := func(r alarmRule, kind string) {
		if seen[r.ID] {
			return
		}
		seen[r.ID] = true
		rules = append(rules, dependantAlarmRule{ID: r.ID, Label: r.Label, Kind: kind})
	}
	underSource := func(r alarmRule) bool {
		return source != "" && (r.Path == source || strings.HasPrefix(r.Path, source+"."))
	}
	for _, r := range listAlarmRules() {
		switch {
		case isHouseBank && r.PathRole == alarmPathRoleHouseBankSoC:
			add(r, dependantRuleStateOfCharge)
		case underSource(r):
			add(r, dependantRuleOther)
		}
	}
	for _, r := range zoneDerivedAlarmRules() {
		if underSource(r) {
			add(r, dependantRuleZone)
		}
	}
	return rules
}

// GET /api/inventory/equipment/:id/dependants
func equipmentDependantsHandler(c echo.Context) error {
	dependants, err := equipmentDependantsFor(c.Param("id"))
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, dependants)
}
