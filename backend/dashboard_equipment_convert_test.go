package main

import (
	"database/sql"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture is shaped like the boat's own dashboard-pages.json: twin engine
// clusters with oil zones, a wall copy with extra RPM zones and an exhaust
// telltale under propulsion.0, a fuel rail on tanks that do and do not have an
// item, an alternator gauge group, a standalone economy gauge and a ribbon
// holding an engine lamp, an unowned alternator lamp and a bilge float.
const tileConvertFixture = `{
  "pages": [
    {"id":"p1","name":"Underway","position":0,"widgets":[
      {"id":"cluster:port00001","x":0,"y":0,"w":5,"h":7,"cluster":{"title":"Port",
        "ring":{"path":"propulsion.port.revolutions","label":"RPM","display":"radial","quantity":"frequency","unit":"rpm","min":0,"max":3000,"ringStyle":"instrument","labelDivisor":100},
        "centre":{"path":"propulsion.port.runTime","label":"Hours","display":"numeric","quantity":"duration","unit":"h"},
        "corners":[{"label":"Oil","rows":[{"path":"propulsion.port.oilPressure","label":"Oil Press","display":"radial","quantity":"pressure","unit":"psi","min":0,"max":100,"zones":[{"from":40,"to":100,"state":"normal"}]}]}],
        "telltales":[{"path":"propulsion.port.transmission.oilTemperature","label":"Gearbox","display":"numeric","quantity":"temperature","unit":"C","min":0,"max":120,"zones":[{"from":0,"to":104,"state":"normal"}]}]}},
      {"id":"cluster:star0001","x":7,"y":0,"w":5,"h":7,"cluster":{"title":"Starboard",
        "ring":{"path":"propulsion.starboard.revolutions","label":"RPM","display":"radial","quantity":"frequency","unit":"rpm","min":0,"max":3000},
        "centre":{"path":"propulsion.starboard.runTime","label":"Hours","display":"numeric","quantity":"duration","unit":"h"},
        "corners":[{"label":"Oil","rows":[{"path":"propulsion.starboard.oilPressure","label":"Oil Press","display":"radial","quantity":"pressure","unit":"psi","min":0,"max":100,"zones":[{"from":40,"to":100,"state":"normal"}]}]}]}},
      {"id":"gauge-group:alt00001","x":8,"y":14,"w":2,"h":6,"gaugeGroup":{"title":"Port Alternator","hero":0,"gauges":[
        {"path":"electrical.alternator.0.power","label":"Output","display":"numeric","quantity":"power","unit":"W"},
        {"path":"electrical.alternator.0.current","label":"Amps","display":"numeric","quantity":"current","unit":"A","min":0,"max":400,"zones":[{"from":320,"to":400,"state":"warn"}]}]}},
      {"id":"gauge:econ0001","x":5,"y":0,"w":2,"h":2,"gauge":{"path":"helmcentral.propulsion.fuelEconomy","label":"fuelEconomy","display":"numeric","quantity":"fuelEconomy","unit":"nmpl"}}
    ],"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"},
    {"id":"p2","name":"Wall: Engines","position":1,"widgets":[
      {"id":"cluster:preview01","x":0,"y":0,"w":5,"h":6,"cluster":{"title":"Port",
        "ring":{"path":"propulsion.port.revolutions","label":"RPM","display":"radial","quantity":"frequency","unit":"rpm","min":0,"max":3300,"zones":[{"from":0,"to":3000,"state":"normal"},{"from":3000,"to":3300,"state":"alarm"}]},
        "centre":{"path":"propulsion.port.runTime","label":"Hours","display":"numeric","quantity":"duration","unit":"h"},
        "corners":[{"label":"Oil","rows":[{"path":"propulsion.port.oilPressure","label":"Oil","display":"numeric","quantity":"pressure","unit":"psi","min":0,"max":100,"zones":[{"from":40,"to":100,"state":"normal"}]}]}],
        "telltales":[{"path":"propulsion.0.exhaustTemperature","label":"Exhaust","display":"numeric","quantity":"temperature","unit":"C","min":0,"max":600}],
        "fuel":{"side":"left","bars":[
          {"level":{"path":"tanks.fuel.5.currentLevel","label":"Fwd","display":"bar","quantity":"ratio","unit":"percent"},"capacity":{"path":"tanks.fuel.5.capacity","label":"","display":"numeric","quantity":"volume","unit":"L"}},
          {"level":{"path":"tanks.fuel.2.currentLevel","label":"Aft","display":"bar","quantity":"ratio","unit":"percent"},"capacity":{"path":"tanks.fuel.2.capacity","label":"","display":"numeric","quantity":"volume","unit":"L"}}]}}}
    ],"created_at":"2026-01-02T00:00:00Z","updated_at":"2026-01-02T00:00:00Z"},
    {"id":"p3","name":"Wall: Nearby","position":2,"widgets":[{"id":"wind","x":0,"y":0,"w":4,"h":4}],
     "created_at":"2026-01-03T00:00:00Z","updated_at":"2026-01-03T00:00:00Z"}
  ],
  "ribbon": {"title":"Indicators","showCheck":true,"lamps":[
    {"path":"propulsion.port.revolutions","label":"Port","group":"Propulsion","icon":"engine"},
    {"path":"electrical.alternator.7.chargingModeNumber","label":"Alt 7","icon":"alternator"},
    {"path":"electrical.switches.bilge","label":"Bilge","invert":true}]}
}`

func mustJSONDecodeProfile(t *testing.T) engineProfile {
	t.Helper()
	min, max := 0.0, 100.0
	rpmMax, gbMax := 3300.0, 140.0
	gearbox := 120.0
	return engineProfile{
		SchemaVersion: 1, Kind: profileKindEngine, ID: "yanmar-4jh", Name: "Yanmar 4JH",
		Gauges: []engineProfileGauge{
			{PathSuffix: "oilPressure", Label: "Oil", Display: "radial", Quantity: "pressure", Unit: "psi", Min: &min, Max: &max},
			{PathSuffix: "revolutions", Label: "RPM", Display: "radial", Quantity: "frequency", Unit: "rpm", Min: &min, Max: &rpmMax},
			{PathSuffix: "transmission.oilTemperature", Label: "Gearbox", Display: "numeric", Quantity: "temperature", Unit: "C", Min: &min, Max: &gbMax,
				Zones: []engineProfileZone{{Direction: "above", Threshold: &gearbox, State: alarmStateWarn}}},
		},
	}
}

func TestConvertDashboardTilesToEquipment(t *testing.T) {
	setupDashboardPagesTest(t)
	path := filepath.Join(t.TempDir(), "dashboard-pages.json")
	t.Setenv("DASHBOARD_PAGES_FILE", path)
	if err := os.WriteFile(path, []byte(tileConvertFixture), 0o644); err != nil {
		t.Fatal(err)
	}

	store := vesselFixtureStore(t)
	ps, err := newProfileStore(store)
	if err != nil {
		t.Fatal(err)
	}
	prevStore := globalProfileStore
	globalProfileStore = ps
	t.Cleanup(func() { globalProfileStore = prevStore })
	if err := ps.inTx(func(tx *sql.Tx) error { return insertProfileTx(tx, mustJSONDecodeProfile(t), profileBasedOn{}) }); err != nil {
		t.Fatal(err)
	}
	loadEngineProfiles()
	t.Cleanup(loadEngineProfiles)

	port := newTileItem(t, "Port engine", "propulsion.port", "yanmar-4jh")
	star := newTileItem(t, "Starboard engine", "propulsion.starboard", "yanmar-4jh")
	alt := newTileItem(t, "Port alternator", "electrical.alternator.0", "")
	fwd := newTileItem(t, "Port forward fuel", "tanks.fuel.5", "")

	loadDashboardPages()
	if err := convertDashboardTilesToEquipment(); err != nil {
		t.Fatalf("convert: %v", err)
	}

	widget := func(page, id string) dashboardLayoutItem {
		t.Helper()
		dashboardPagesMu.RLock()
		defer dashboardPagesMu.RUnlock()
		for _, p := range dashboardPagesState {
			if p.ID != page {
				continue
			}
			for _, w := range p.Widgets {
				if w.ID == id {
					return w
				}
			}
		}
		t.Fatalf("widget %s not found", id)
		return dashboardLayoutItem{}
	}

	pc := widget("p1", "cluster:port00001").Cluster
	if pc.EquipmentID != port || pc.Title != "Port" {
		t.Errorf("port cluster should reference the port engine and keep its title: %+v", pc)
	}
	if pc.Ring.Path != "" || pc.Ring.Suffix != "revolutions" || pc.Ring.LabelDivisor == nil || pc.Ring.RingStyle != "instrument" {
		t.Errorf("ring should become a suffix and keep its display choices: %+v", pc.Ring)
	}
	if pc.Corners[0].Rows[0].Suffix != "oilPressure" || len(pc.Corners[0].Rows[0].Zones) != 0 {
		t.Errorf("oil row should hold a suffix and no zones: %+v", pc.Corners[0].Rows[0])
	}
	if sc := widget("p1", "cluster:star0001").Cluster; sc.EquipmentID != star {
		t.Errorf("starboard cluster should reference the starboard engine: %+v", sc)
	}

	g := widget("p1", "gauge-group:alt00001").GaugeGroup
	if g.EquipmentKind != "alternator" || g.EquipmentID != alt || g.Gauges[1].Suffix != "current" || len(g.Gauges[1].Zones) != 1 {
		t.Errorf("alternator group should become an equipment group that keeps its Amps zone while its item has no profile: %+v", g)
	}
	if econ := widget("p1", "gauge:econ0001").Gauge; econ.Path != "helmcentral.propulsion.fuelEconomy" {
		t.Errorf("a single-instance gauge keeps its path: %+v", econ)
	}

	wc := widget("p2", "cluster:preview01").Cluster
	if wc.EquipmentID != port || len(wc.Telltales) != 0 {
		t.Errorf("the exhaust telltale reads outside propulsion.port and is dropped: %+v", wc)
	}
	if len(wc.Fuel.Bars) != 2 || wc.Fuel.Bars[0].TankID != fwd || wc.Fuel.Bars[0].Label != "Fwd" || wc.Fuel.Bars[0].Level != nil {
		t.Errorf("a tank with an item becomes a reference: %+v", wc.Fuel.Bars[0])
	}
	if wc.Fuel.Bars[1].TankID != "" || wc.Fuel.Bars[1].Label != "Aft" {
		t.Errorf("a tank with no item keeps its label and asks for one: %+v", wc.Fuel.Bars[1])
	}

	// The gearbox zone the tile held differs from the profile's own, so it stays
	// on the tile, inert, until the operator chooses.
	if gb := pc.Footer.TransTemp; gb == nil || gb.Suffix != "transmission.oilTemperature" || len(gb.Zones) != 1 {
		t.Errorf("the gearbox zone stays on the tile: %+v", gb)
	}

	// Zones moved to the profile where it had none, and the profile's own
	// gearbox zone won over the tile's.
	profiles, _ := engineProfiles()
	var got engineProfile
	for _, p := range profiles {
		if p.ID == "yanmar-4jh" {
			got = p
		}
	}
	zones := func(suffix string) []engineProfileZone {
		g, _ := profileGaugeFor(got, suffix)
		return g.Zones
	}
	if z := zones("oilPressure"); len(z) != 1 || z[0].Direction != "above" || *z[0].Threshold != 40 || z[0].State != alarmStateNormal {
		t.Errorf("oil zones should have moved to the profile: %+v", z)
	}
	if z := zones("revolutions"); len(z) != 2 {
		t.Errorf("rpm zones should have moved to the profile: %+v", z)
	}
	if z := zones("transmission.oilTemperature"); len(z) != 1 || z[0].State != alarmStateWarn || *z[0].Threshold != 120 {
		t.Errorf("the profile's own gearbox zone must stay: %+v", z)
	}

	// Ribbon: the engine lamp follows the item, the unowned alternator and the
	// bilge float keep their paths.
	dashboardPagesMu.RLock()
	lamps := dashboardRibbonState.Lamps
	marker := dashboardEquipmentConverted
	dashboardPagesMu.RUnlock()
	if lamps[0].EquipmentID != port || lamps[0].Suffix != "revolutions" || lamps[0].Path != "" {
		t.Errorf("engine lamp should reference the port engine: %+v", lamps[0])
	}
	if lamps[1].Path == "" || lamps[2].Path != "electrical.switches.bilge" {
		t.Errorf("unowned and single-instance lamps keep their paths: %+v %+v", lamps[1], lamps[2])
	}
	if !marker {
		t.Error("the conversion should record that it has run")
	}

	// Survives a reload and does not run twice.
	before, _ := os.ReadFile(path)
	loadDashboardPages()
	if err := convertDashboardTilesToEquipment(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Errorf("a second start rewrote the file:\n%s\n---\n%s", before, after)
	}

	// The converted file must pass the same validation a save does.
	dashboardPagesMu.RLock()
	defer dashboardPagesMu.RUnlock()
	for _, p := range dashboardPagesState {
		if msg := validateDashboardWidgets(p.Widgets, ""); msg != "" {
			t.Errorf("page %s no longer validates: %s", p.Name, msg)
		}
	}
	if msg := validateLampStripConfig(dashboardRibbonState, "ribbon"); msg != "" {
		t.Errorf("ribbon no longer validates: %s", msg)
	}
}

// A tile that matches no item keeps its display choices and asks for one.
func TestConvertDashboardTilesLeavesUnmatchedTilesWaitingForAnItem(t *testing.T) {
	setupDashboardPagesTest(t)
	path := filepath.Join(t.TempDir(), "dashboard-pages.json")
	t.Setenv("DASHBOARD_PAGES_FILE", path)
	if err := os.WriteFile(path, []byte(tileConvertFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	vesselFixtureStore(t)
	loadDashboardPages()
	if err := convertDashboardTilesToEquipment(); err != nil {
		t.Fatal(err)
	}

	dashboardPagesMu.RLock()
	defer dashboardPagesMu.RUnlock()
	for _, p := range dashboardPagesState {
		for _, w := range p.Widgets {
			if w.Cluster == nil {
				continue
			}
			if w.Cluster.EquipmentID != "" || w.Cluster.Ring.Path != "" || w.Cluster.Ring.Suffix != "revolutions" {
				t.Errorf("%s should wait for an item and keep its reading: %+v", w.ID, w.Cluster.Ring)
			}
		}
	}
	if msg := validateDashboardWidgets(dashboardPagesState["p1"].Widgets, ""); msg != "" {
		t.Errorf("a tile waiting for an item must still save: %s", msg)
	}
}

const unitsAndFuelFixture = `{"pages":[{"id":"p1","name":"Engines","position":0,"widgets":[
  {"id":"cluster:port00001","x":0,"y":0,"w":5,"h":7,"cluster":{"title":"Port",
    "ring":{"path":"propulsion.port.oilPressure","label":"Oil","display":"radial","quantity":"pressure","unit":"psi","zones":[{"from":0,"to":14.5038,"state":"alarm"}]},
    "centre":{"path":"propulsion.port.runTime","label":"Hours","display":"numeric","quantity":"duration","unit":"h"},
    "fuel":{"side":"left","bars":[{"level":{"path":"tanks.fuel.5.currentLevel","label":"Fwd","display":"bar","quantity":"ratio","unit":"percent","zones":[{"from":0,"to":15,"state":"warn"}]},"capacity":{"path":"tanks.fuel.5.capacity","label":"","display":"numeric","quantity":"volume","unit":"L"}}]}}}
 ],"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]}`

// A tile whose unit differs from the profile's moves its zones in the
// profile's unit, and a fuel bar keeps its zones, which raise the low-fuel
// alarm, because a tank has no profile to hold them.
func TestConvertDashboardTilesConvertsUnitsAndKeepsFuelBarZones(t *testing.T) {
	setupDashboardPagesTest(t)
	path := filepath.Join(t.TempDir(), "dashboard-pages.json")
	t.Setenv("DASHBOARD_PAGES_FILE", path)
	if err := os.WriteFile(path, []byte(unitsAndFuelFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	store := vesselFixtureStore(t)
	ps, err := newProfileStore(store)
	if err != nil {
		t.Fatal(err)
	}
	prev := globalProfileStore
	globalProfileStore = ps
	t.Cleanup(func() { globalProfileStore = prev })
	if err := ps.inTx(func(tx *sql.Tx) error { return insertProfileTx(tx, barProfileWithoutZones(), profileBasedOn{}) }); err != nil {
		t.Fatal(err)
	}
	loadEngineProfiles()
	t.Cleanup(loadEngineProfiles)
	port := newTileItem(t, "Port engine", "propulsion.port", "bar-engine")
	tank := newTileItem(t, "Port forward fuel", "tanks.fuel.5", "")

	loadDashboardPages()
	if err := convertDashboardTilesToEquipment(); err != nil {
		t.Fatal(err)
	}

	profiles, _ := engineProfiles()
	zones := profiles[0].Gauges[0].Zones
	if len(zones) != 1 || zones[0].Direction != "below" || zones[0].Threshold == nil || *zones[0].Threshold < 0.99 || *zones[0].Threshold > 1.01 {
		t.Errorf("14.5 psi is 1 bar on the profile, got %+v", zones)
	}

	dashboardPagesMu.RLock()
	bar := dashboardPagesState["p1"].Widgets[0].Cluster.Fuel.Bars[0]
	dashboardPagesMu.RUnlock()
	if bar.TankID != tank || len(bar.Zones) != 1 || bar.Zones[0].To != 15 || bar.Zones[0].State != alarmStateWarn {
		t.Errorf("the fuel bar keeps its low-fuel zone: %+v", bar)
	}
	rules := zoneDerivedAlarmRules()
	var low bool
	for _, r := range rules {
		if r.ID == "zone:tank:"+tank+":0" {
			low = true
		}
	}
	if !low {
		t.Errorf("the low-fuel alarm must survive the conversion, got %+v (engine %s)", rules, port)
	}
}

func barProfileWithoutZones() engineProfile {
	p := barProfile()
	p.Gauges[0].Zones = nil
	return p
}

// The boat's engines and alternators have no equipment item yet when the
// dashboard is converted. Their zones must go on raising the same alarms, from
// the tile, until equipment is picked.
func TestConvertDashboardTilesKeepsZonesOnTilesWithNoItem(t *testing.T) {
	setupDashboardPagesTest(t)
	path := filepath.Join(t.TempDir(), "dashboard-pages.json")
	t.Setenv("DASHBOARD_PAGES_FILE", path)
	if err := os.WriteFile(path, []byte(tileConvertFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	vesselFixtureStore(t)
	logs := captureLog(t)
	loadDashboardPages()
	if err := convertDashboardTilesToEquipment(); err != nil {
		t.Fatal(err)
	}

	dashboardPagesMu.RLock()
	wall := dashboardPagesState["p2"].Widgets[0].Cluster
	group := dashboardPagesState["p1"].Widgets[2].GaugeGroup
	dashboardPagesMu.RUnlock()
	if wall.EquipmentID != "" || wall.Source != "propulsion.port" {
		t.Errorf("a tile with no item remembers the source it read: %+v", wall)
	}
	if len(wall.Ring.Zones) != 2 || len(wall.Corners[0].Rows[0].Zones) != 1 {
		t.Errorf("the zones stay on the tile: ring %+v oil %+v", wall.Ring.Zones, wall.Corners[0].Rows[0].Zones)
	}
	if group.Source != "electrical.alternator.0" || len(group.Gauges[1].Zones) != 1 {
		t.Errorf("the alternator group keeps its source and its Amps zone: %+v", group)
	}
	if strings.Contains(logs.String(), "dropped the zones") {
		t.Errorf("the conversion never drops a zone:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "kept the zones") {
		t.Errorf("what stayed on the tile is logged:\n%s", logs.String())
	}

	// The alarms keep running, under the ids they always had.
	byID := map[string]alarmRule{}
	for _, r := range zoneDerivedAlarmRules() {
		byID[r.ID] = r
	}
	rpm, ok := byID["zone:cluster:preview01:0:1"]
	if !ok || rpm.Path != "propulsion.port.revolutions" || rpm.State != alarmStateAlarm {
		t.Errorf("the overspeed alarm must survive under its old id, got %+v", byID)
	}
	amps, ok := byID["zone:gauge-group:alt00001:1:0"]
	if !ok || amps.Path != "electrical.alternator.0.current" || amps.State != alarmStateWarn {
		t.Errorf("the alternator amps alarm must survive under its old id, got %+v", byID)
	}
	if rpm.Label != "Port RPM" {
		t.Errorf("an alarm on a tile with no item is named for the tile, got %q", rpm.Label)
	}
}

// An item with no profile holds nothing, so the tile keeps its zones and they
// alarm on the item's source.
func TestTileZonesStayOnTheTileWhileTheItemHasNoProfile(t *testing.T) {
	itemID := newTileItem(t, "Port engine", "propulsion.port", "")
	oil := dashboardGaugeConfig{Suffix: "oilPressure", Label: "Oil", Display: "radial", Quantity: "pressure", Unit: "psi",
		Zones: []gaugeZone{{From: 0, To: 15, State: alarmStateAlarm}}}
	withPages(t, oilCluster(itemID, oil))
	rules := zoneDerivedAlarmRules()
	if len(rules) != 1 || rules[0].Path != "propulsion.port.oilPressure" || rules[0].Label != "Port engine Oil" {
		t.Fatalf("expected the tile's alarm on the item's source, got %+v", rules)
	}
	if msg := validateDashboardWidgets([]dashboardLayoutItem{oilCluster(itemID, oil)}, ""); msg != "" {
		t.Fatalf("a slot may hold zones while there is no profile to hold them: %s", msg)
	}
}

// When the profile has its own zones for the reading they apply and the tile's
// stay stored but inert until the operator chooses, so one alarm exists, not two.
func TestTheProfilesZonesWinOverZonesStillOnTheTile(t *testing.T) {
	useProfiles(t, barProfile())
	itemID := newTileItem(t, "Port engine", "propulsion.port", "bar-engine")
	slot := dashboardGaugeConfig{Suffix: "oilPressure", Label: "Oil", Display: "radial", Quantity: "pressure", Unit: "psi",
		Zones: []gaugeZone{{From: 0, To: 15, State: alarmStateWarn}}}
	withPages(t, oilCluster(itemID, slot))
	rules := zoneDerivedAlarmRules()
	if len(rules) != 1 || math.Abs(rules[0].Value-800000) > 1 {
		t.Fatalf("only the profile's alarm, got %+v", rules)
	}
}

// A profile with a gauge but no zones for it leaves the tile's zones in force,
// in the tile's own unit.
func TestTileZonesApplyWhenTheProfileGaugeHasNoZones(t *testing.T) {
	useProfiles(t, barProfileWithoutZones())
	itemID := newTileItem(t, "Port engine", "propulsion.port", "bar-engine")
	slot := dashboardGaugeConfig{Suffix: "oilPressure", Label: "Oil", Display: "radial", Quantity: "pressure", Unit: "psi",
		Zones: []gaugeZone{{From: 0, To: 14.5038, State: alarmStateAlarm}}}
	withPages(t, oilCluster(itemID, slot))
	rules := zoneDerivedAlarmRules()
	if len(rules) != 1 || math.Abs(rules[0].Value-100000) > 50 {
		t.Fatalf("14.5 psi is about 100000 Pa, got %+v", rules)
	}
	resolved, _, _ := currentEquipmentIndex().resolveSlot(itemID, slot)
	if resolved.Unit != "psi" || len(resolved.Zones) != 1 {
		t.Fatalf("a slot that still holds its zones keeps its unit, got %+v", resolved)
	}
}
