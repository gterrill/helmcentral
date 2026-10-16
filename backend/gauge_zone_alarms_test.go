package main

import (
	"math"
	"net/http"
	"sort"
	"testing"
)

func zoneGaugeConfig(min, max float64, zones ...gaugeZone) *dashboardGaugeConfig {
	return &dashboardGaugeConfig{
		Path:     "propulsion.port.oilPressure",
		Label:    "Oil Press",
		Display:  "radial",
		Quantity: "pressure",
		Unit:     "psi",
		Min:      &min,
		Max:      &max,
		Zones:    zones,
	}
}

func withPages(t *testing.T, widgets ...dashboardLayoutItem) {
	t.Helper()
	dashboardPagesMu.Lock()
	previous := dashboardPagesState
	dashboardPagesState = map[string]*dashboardPageData{
		"page-a": {ID: "page-a", Name: "Underway", Widgets: widgets},
	}
	dashboardPagesMu.Unlock()
	t.Cleanup(func() {
		dashboardPagesMu.Lock()
		dashboardPagesState = previous
		dashboardPagesMu.Unlock()
	})
}

// A band at the bottom of the scale is a "below" threshold at its upper edge:
// low oil pressure is the alarm, and it fires as the value falls past 15 psi.
func TestZoneDerivedAlarmRulesLowerBand(t *testing.T) {
	withPages(t, gaugeWidget("gauge:abcd1234", zoneGaugeConfig(0, 100,
		gaugeZone{From: 0, To: 15, State: alarmStateAlarm},
	)))

	rules := zoneDerivedAlarmRules()
	if len(rules) != 1 {
		t.Fatalf("expected one derived rule, got %d", len(rules))
	}

	rule := rules[0]
	if rule.Op != alarmOpBelow {
		t.Fatalf("a band at the bottom of the scale is a below threshold, got %q", rule.Op)
	}
	// 15 psi in pascals — the conversion is the whole point.
	if math.Abs(rule.Value-103421.355) > 1 {
		t.Fatalf("expected the threshold converted to pascals, got %v", rule.Value)
	}
	if rule.Path != "propulsion.port.oilPressure" {
		t.Fatalf("unexpected path %q", rule.Path)
	}
	if rule.State != alarmStateAlarm {
		t.Fatalf("expected the zone's severity, got %q", rule.State)
	}
	if !rule.Enabled {
		t.Fatal("a derived rule is enabled; disabling it means removing the zone")
	}
	// Without a deadband and a dwell, a value hovering at the threshold is an
	// alarm storm — the reason people switch marine alarms off entirely.
	if rule.DwellSeconds <= 0 {
		t.Fatal("expected a non-zero dwell")
	}
	if rule.Hysteresis <= 0 {
		t.Fatal("expected a non-zero hysteresis")
	}
}

// A band at the top of the scale is an "above" threshold at its lower edge.
func TestZoneDerivedAlarmRulesUpperBand(t *testing.T) {
	withPages(t, gaugeWidget("gauge:abcd1234", zoneGaugeConfig(0, 100,
		gaugeZone{From: 80, To: 100, State: alarmStateWarn},
	)))

	rules := zoneDerivedAlarmRules()
	if len(rules) != 1 || rules[0].Op != alarmOpAbove {
		t.Fatalf("expected one above rule, got %+v", rules)
	}
	if math.Abs(rules[0].Value-551580.56) > 1 {
		t.Fatalf("expected 80 psi in pascals, got %v", rules[0].Value)
	}
}

func TestZoneDerivedAlarmRulesSkipsNormalBands(t *testing.T) {
	withPages(t, gaugeWidget("gauge:abcd1234", zoneGaugeConfig(0, 100,
		gaugeZone{From: 20, To: 80, State: alarmStateNormal},
	)))

	if rules := zoneDerivedAlarmRules(); len(rules) != 0 {
		t.Fatalf("a normal band is not an alarm, got %+v", rules)
	}
}

func TestZoneDerivedAlarmRulesCoversGroupMembers(t *testing.T) {
	withPages(t,
		gaugeWidget("gauge:aaaa1111", zoneGaugeConfig(0, 100, gaugeZone{From: 0, To: 15, State: alarmStateAlarm})),
		gaugeGroupWidget("gauge-group:bbbb2222", &dashboardGaugeGroupConfig{
			Title: "Port",
			Gauges: []dashboardGaugeConfig{
				*zoneGaugeConfig(0, 100, gaugeZone{From: 90, To: 100, State: alarmStateWarn}),
				*zoneGaugeConfig(0, 100),
			},
		}),
	)

	rules := zoneDerivedAlarmRules()
	if len(rules) != 2 {
		t.Fatalf("expected a rule from the standalone gauge and one from the group member, got %d", len(rules))
	}
}

// Ids have to survive a restart: an id that changes every tick would make the
// engine treat a steady alarm as a new one over and over.
func TestZoneDerivedAlarmRuleIDsAreStable(t *testing.T) {
	withPages(t, gaugeGroupWidget("gauge-group:bbbb2222", &dashboardGaugeGroupConfig{
		Title: "Port",
		Gauges: []dashboardGaugeConfig{
			*zoneGaugeConfig(0, 100, gaugeZone{From: 0, To: 15, State: alarmStateAlarm}, gaugeZone{From: 90, To: 100, State: alarmStateWarn}),
		},
	}))

	first, second := zoneDerivedAlarmRules(), zoneDerivedAlarmRules()
	if len(first) != 2 {
		t.Fatalf("expected two derived rules, got %d", len(first))
	}
	ids := func(rules []alarmRule) []string {
		out := []string{}
		for _, r := range rules {
			out = append(out, r.ID)
		}
		sort.Strings(out)
		return out
	}
	a, b := ids(first), ids(second)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("expected stable ids, got %v then %v", a, b)
		}
	}
	for _, id := range a {
		if !isZoneDerivedAlarmRuleID(id) {
			t.Fatalf("expected a recognisable derived id, got %q", id)
		}
	}
}

// A band in the middle of the range has no single-threshold equivalent, so it
// is rejected at the point of saving rather than silently never alarming.
func TestValidateGaugeConfigRejectsMidRangeZone(t *testing.T) {
	widget := gaugeWidget("gauge:abcd1234", zoneGaugeConfig(0, 100,
		gaugeZone{From: 40, To: 60, State: alarmStateWarn},
	))
	if msg := validateDashboardWidgets([]dashboardLayoutItem{widget}, ""); msg == "" {
		t.Fatal("expected a mid-range alarm band to be rejected")
	}
}

// A zone in a unit the backend cannot convert would compare display units
// against SI values, so it must not be saveable.
func TestValidateGaugeConfigRejectsUnconvertibleZoneUnit(t *testing.T) {
	config := zoneGaugeConfig(0, 100, gaugeZone{From: 0, To: 15, State: alarmStateAlarm})
	config.Unit = "furlongs"
	if msg := validateDashboardWidgets([]dashboardLayoutItem{gaugeWidget("gauge:abcd1234", config)}, ""); msg == "" {
		t.Fatal("expected an unconvertible zone unit to be rejected")
	}
}

// Derived rules are not stored, so the write handlers must refuse them rather
// than half-succeeding against a rule that does not exist in the store.
func TestAlarmRuleHandlersRefuseDerivedIDs(t *testing.T) {
	c, rec := newDashboardPagesRequest(t, http.MethodDelete, "/api/alarm-rules/zone:gauge:abcd1234:0:0", nil)
	c.SetParamNames("id")
	c.SetParamValues("zone:gauge:abcd1234:0:0")
	if err := deleteAlarmRuleHandler(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 deleting a derived rule, got %d", rec.Code)
	}
}

// engineProfileWithZones is a profile whose oil pressure gauge carries an
// alarm below 15 psi, and whose RPM gauge carries a warning above 2600.
func engineProfileWithZones() engineProfile {
	min, max := 0.0, 100.0
	rpmMax := 3000.0
	low, high := 15.0, 2600.0
	return engineProfile{
		SchemaVersion: 1, Kind: profileKindEngine, ID: "yanmar-4jh", Name: "Yanmar 4JH",
		Gauges: []engineProfileGauge{
			{PathSuffix: "oilPressure", Label: "Oil Press", Display: "radial", Quantity: "pressure", Unit: "psi", Min: &min, Max: &max,
				Zones: []engineProfileZone{{Direction: "below", Threshold: &low, State: alarmStateAlarm}}},
			{PathSuffix: "revolutions", Label: "RPM", Display: "radial", Quantity: "frequency", Unit: "rpm", Min: &min, Max: &rpmMax,
				Zones: []engineProfileZone{{Direction: "above", Threshold: &high, State: alarmStateWarn}}},
		},
	}
}

// A cluster slot's zones must alarm like any other gauge's, or ADR 0050's
// whole point - that a red band is the alarm - holds everywhere but here. The
// zones come from the item's profile (ADR 0187), so the tile carries none.
func TestZoneDerivedAlarmRulesCoversClusterSlotsFromTheProfile(t *testing.T) {
	useProfiles(t, engineProfileWithZones())
	itemID := newTileItem(t, "Port engine", "propulsion.port", "yanmar-4jh")

	withPages(t, dashboardLayoutItem{
		ID: "cluster:abcd1234", X: 0, Y: 0, W: 6, H: 10,
		Cluster: &dashboardClusterConfig{
			EquipmentID: itemID,
			Ring:        dashboardGaugeConfig{Suffix: "revolutions", Label: "RPM", Display: "radial", Quantity: "frequency", Unit: "rpm"},
			Centre:      dashboardGaugeConfig{Suffix: "fuel.rate", Display: "numeric", Quantity: "raw", Unit: "raw"},
			Corners: []dashboardClusterCorner{{Label: "Oil", Rows: []dashboardGaugeConfig{
				{Suffix: "oilPressure", Label: "Oil", Display: "numeric", Quantity: "pressure", Unit: "psi"},
			}}},
		},
	})

	rules := zoneDerivedAlarmRules()
	if len(rules) != 2 {
		t.Fatalf("expected a rule from the ring and one from the corner row, got %d: %+v", len(rules), rules)
	}
	byPath := map[string]alarmRule{}
	for _, r := range rules {
		byPath[r.Path] = r
	}
	oil, ok := byPath["propulsion.port.oilPressure"]
	if !ok || oil.Op != alarmOpBelow {
		t.Fatalf("expected a below rule on the resolved oil pressure path, got %+v", rules)
	}
	if oil.Label != "Port engine Oil Press" {
		t.Fatalf("the alarm names the engine by its item, got %q", oil.Label)
	}
	if rpm := byPath["propulsion.port.revolutions"]; rpm.Op != alarmOpAbove {
		t.Fatalf("expected an above rule on rpm, got %+v", rpm)
	}
}

// Two tiles showing one engine's oil pressure are one alarm, not two.
func TestZoneDerivedAlarmRulesAreOnePerItemReadingAcrossTiles(t *testing.T) {
	useProfiles(t, engineProfileWithZones())
	itemID := newTileItem(t, "Port engine", "propulsion.port", "yanmar-4jh")
	tile := func() *dashboardClusterConfig {
		return &dashboardClusterConfig{
			EquipmentID: itemID,
			Ring:        dashboardGaugeConfig{Suffix: "oilPressure", Display: "radial", Quantity: "pressure", Unit: "psi"},
			Centre:      dashboardGaugeConfig{Suffix: "fuel.rate", Display: "numeric", Quantity: "raw", Unit: "raw"},
		}
	}
	withPages(t,
		dashboardLayoutItem{ID: "cluster:abcd1234", X: 0, Y: 0, W: 6, H: 10, Cluster: tile()},
		dashboardLayoutItem{ID: "cluster:efgh5678", X: 6, Y: 0, W: 6, H: 10, Cluster: tile()},
		gaugeGroupWidget("gauge-group:bbbb2222", &dashboardGaugeGroupConfig{
			EquipmentKind: "engine", EquipmentID: itemID, Title: "Port",
			Gauges: []dashboardGaugeConfig{{Suffix: "oilPressure", Display: "numeric", Quantity: "pressure", Unit: "psi"}},
		}),
	)
	if rules := zoneDerivedAlarmRules(); len(rules) != 1 {
		t.Fatalf("expected one rule for one reading however many tiles show it, got %d: %+v", len(rules), rules)
	}
}

// The profile is read live: change its zones and the alarm follows, with no
// edit to any tile.
func TestZoneDerivedAlarmRulesFollowTheProfileLive(t *testing.T) {
	useProfiles(t, engineProfileWithZones())
	itemID := newTileItem(t, "Port engine", "propulsion.port", "yanmar-4jh")
	withPages(t, dashboardLayoutItem{
		ID: "cluster:abcd1234", X: 0, Y: 0, W: 6, H: 10,
		Cluster: &dashboardClusterConfig{
			EquipmentID: itemID,
			Ring:        dashboardGaugeConfig{Suffix: "oilPressure", Display: "radial", Quantity: "pressure", Unit: "psi"},
			Centre:      dashboardGaugeConfig{Suffix: "fuel.rate", Display: "numeric", Quantity: "raw", Unit: "raw"},
		},
	})
	first := zoneDerivedAlarmRules()
	if len(first) != 1 || math.Abs(first[0].Value-103421.355) > 1 {
		t.Fatalf("expected the 15 psi alarm, got %+v", first)
	}

	edited := engineProfileWithZones()
	moved := 25.0
	edited.Gauges[0].Zones[0].Threshold = &moved
	useProfiles(t, edited)
	second := zoneDerivedAlarmRules()
	if len(second) != 1 || math.Abs(second[0].Value-172368.9) > 1 {
		t.Fatalf("expected the alarm to move to 25 psi, got %+v", second)
	}
	if second[0].ID != first[0].ID {
		t.Fatalf("moving a threshold must keep the alarm's id: %q then %q", first[0].ID, second[0].ID)
	}

	useProfiles(t)
	if rules := zoneDerivedAlarmRules(); len(rules) != 0 {
		t.Fatalf("with no profile there are no zones, got %+v", rules)
	}
}

// A tank has no profile gauges, so a fuel bar raises nothing; a capacity is a
// constant an alarm would never clear.
func TestZoneDerivedAlarmRulesIgnoreFuelBars(t *testing.T) {
	useProfiles(t, engineProfileWithZones())
	tank := newTileItem(t, "Port forward fuel", "tanks.fuel.5", "")
	withPages(t, dashboardLayoutItem{
		ID: "cluster:abcd1234", X: 0, Y: 0, W: 6, H: 10,
		Cluster: &dashboardClusterConfig{
			Ring:   dashboardGaugeConfig{Display: "radial", Quantity: "raw", Unit: "raw"},
			Centre: dashboardGaugeConfig{Display: "numeric", Quantity: "raw", Unit: "raw"},
			Fuel:   &dashboardClusterFuelRail{Side: "left", Bars: []dashboardClusterFuelBar{{TankID: tank, Label: "Fwd"}}},
		},
	})
	if rules := zoneDerivedAlarmRules(); len(rules) != 0 {
		t.Fatalf("expected no rules from a fuel bar, got %+v", rules)
	}
}

// A tile with no item picked raises nothing: there is no source to read.
func TestZoneDerivedAlarmRulesSkipATileWithNoItem(t *testing.T) {
	useProfiles(t, engineProfileWithZones())
	withPages(t, dashboardLayoutItem{
		ID: "cluster:abcd1234", X: 0, Y: 0, W: 6, H: 10,
		Cluster: &dashboardClusterConfig{
			Ring:   dashboardGaugeConfig{Suffix: "oilPressure", Display: "radial", Quantity: "pressure", Unit: "psi"},
			Centre: dashboardGaugeConfig{Suffix: "fuel.rate", Display: "numeric", Quantity: "raw", Unit: "raw"},
		},
	})
	if rules := zoneDerivedAlarmRules(); len(rules) != 0 {
		t.Fatalf("expected no rules, got %+v", rules)
	}
}

// barProfile is an engine profile whose oil pressure is in bar on a 0..10 scale,
// with an alarm above 8 bar.
func barProfile() engineProfile {
	min, max, high := 0.0, 10.0, 8.0
	return engineProfile{
		SchemaVersion: 1, Kind: profileKindEngine, ID: "bar-engine", Name: "Bar engine",
		Gauges: []engineProfileGauge{{
			PathSuffix: "oilPressure", Label: "Oil Press", Display: "radial", Quantity: "pressure", Unit: "bar", Min: &min, Max: &max,
			Zones: []engineProfileZone{{Direction: "above", Threshold: &high, State: alarmStateAlarm}},
		}},
	}
}

func oilCluster(itemID string, slot dashboardGaugeConfig) dashboardLayoutItem {
	return dashboardLayoutItem{
		ID: "cluster:abcd1234", X: 0, Y: 0, W: 6, H: 10,
		Cluster: &dashboardClusterConfig{
			EquipmentID: itemID,
			Ring:        slot,
			Centre:      dashboardGaugeConfig{Suffix: "fuel.rate", Display: "numeric", Quantity: "raw", Unit: "raw"},
		},
	}
}

// A slot that sets no scale of its own takes the profile gauge's, for the
// alarm as well as the band. A 0..10 bar scale with a band from 8 is anchored
// to the top; judged against a default 0..100 scale it floats mid-scale and
// the alarm was skipped without a word.
func TestZoneDerivedAlarmRulesUseTheProfileScaleWhenTheSlotHasNone(t *testing.T) {
	useProfiles(t, barProfile())
	itemID := newTileItem(t, "Port engine", "propulsion.port", "bar-engine")
	withPages(t, oilCluster(itemID, dashboardGaugeConfig{Suffix: "oilPressure", Display: "radial", Quantity: "pressure", Unit: "bar"}))

	rules := zoneDerivedAlarmRules()
	if len(rules) != 1 {
		t.Fatalf("expected the alarm above 8 bar, got %+v", rules)
	}
	if math.Abs(rules[0].Value-800000) > 1 {
		t.Fatalf("8 bar is 800000 Pa, got %v", rules[0].Value)
	}

	ix := currentEquipmentIndex()
	resolved, _, ok := ix.resolveSlot(itemID, dashboardGaugeConfig{Suffix: "oilPressure", Display: "radial", Quantity: "pressure", Unit: "bar"})
	if !ok || resolved.Min == nil || resolved.Max == nil || *resolved.Min != 0 || *resolved.Max != 10 {
		t.Fatalf("the resolved slot carries the scale its zones were anchored to: %+v", resolved)
	}
}

// Profile thresholds are in the profile gauge's unit. A slot set to psi must
// not reinterpret 8 bar as 8 psi.
func TestZoneDerivedAlarmRulesReadProfileThresholdsInTheProfileUnit(t *testing.T) {
	useProfiles(t, barProfile())
	itemID := newTileItem(t, "Port engine", "propulsion.port", "bar-engine")
	psiSlot := dashboardGaugeConfig{Suffix: "oilPressure", Display: "radial", Quantity: "pressure", Unit: "psi"}
	withPages(t, oilCluster(itemID, psiSlot))

	rules := zoneDerivedAlarmRules()
	if len(rules) != 1 || math.Abs(rules[0].Value-800000) > 1 {
		t.Fatalf("the alarm stays at 8 bar whatever unit the slot was set to, got %+v", rules)
	}
	resolved, _, _ := currentEquipmentIndex().resolveSlot(itemID, psiSlot)
	if resolved.Unit != "bar" || resolved.Quantity != "pressure" {
		t.Fatalf("an equipment slot takes its unit from the profile, got %s %s", resolved.Quantity, resolved.Unit)
	}
}

// Two tiles that differ in label and scale give one alarm whose label, scale and
// unit come from the item and the profile, not from whichever tile was met first.
func TestZoneDerivedAlarmRulesDoNotDependOnWhichTileIsMetFirst(t *testing.T) {
	useProfiles(t, barProfile())
	itemID := newTileItem(t, "Port engine", "propulsion.port", "bar-engine")
	lo, hi := 0.0, 20.0
	a := oilCluster(itemID, dashboardGaugeConfig{Suffix: "oilPressure", Label: "Oil A", Display: "radial", Quantity: "pressure", Unit: "psi", Min: &lo, Max: &hi})
	b := oilCluster(itemID, dashboardGaugeConfig{Suffix: "oilPressure", Label: "Oil B", Display: "radial", Quantity: "pressure", Unit: "bar"})
	b.ID = "cluster:efgh5678"

	dashboardPagesMu.Lock()
	previous := dashboardPagesState
	dashboardPagesState = map[string]*dashboardPageData{
		"p1": {ID: "p1", Widgets: []dashboardLayoutItem{a}},
		"p2": {ID: "p2", Widgets: []dashboardLayoutItem{b}},
		"p3": {ID: "p3", Widgets: []dashboardLayoutItem{a}},
	}
	dashboardPagesMu.Unlock()
	t.Cleanup(func() {
		dashboardPagesMu.Lock()
		dashboardPagesState = previous
		dashboardPagesMu.Unlock()
	})

	for i := 0; i < 40; i++ {
		rules := zoneDerivedAlarmRules()
		if len(rules) != 1 {
			t.Fatalf("expected one rule, got %+v", rules)
		}
		if rules[0].Label != "Port engine Oil Press" || math.Abs(rules[0].Value-800000) > 1 {
			t.Fatalf("run %d: label and value must come from the item and profile, got %q %v", i, rules[0].Label, rules[0].Value)
		}
	}
}

// A fuel bar's zones stay on the tile record, because a tank has no profile,
// and raise a low-fuel alarm keyed by the tank item.
func TestZoneDerivedAlarmRulesRaiseLowFuelFromABarsOwnZones(t *testing.T) {
	tank := newTileItem(t, "Port forward fuel", "tanks.fuel.5", "")
	withPages(t, dashboardLayoutItem{
		ID: "cluster:abcd1234", X: 0, Y: 0, W: 6, H: 10,
		Cluster: &dashboardClusterConfig{
			Ring:   dashboardGaugeConfig{Display: "radial", Quantity: "raw", Unit: "raw"},
			Centre: dashboardGaugeConfig{Display: "numeric", Quantity: "raw", Unit: "raw"},
			Fuel: &dashboardClusterFuelRail{Side: "left", Bars: []dashboardClusterFuelBar{{
				TankID: tank, Label: "Fwd", Zones: []gaugeZone{{From: 0, To: 15, State: alarmStateWarn}},
			}}},
		},
	})
	rules := zoneDerivedAlarmRules()
	if len(rules) != 1 {
		t.Fatalf("expected the low fuel alarm, got %+v", rules)
	}
	r := rules[0]
	if r.Path != "tanks.fuel.5.currentLevel" || r.Op != alarmOpBelow || math.Abs(r.Value-0.15) > 1e-9 {
		t.Fatalf("below 15 percent on the tank's level, got %+v", r)
	}
	if r.ID != "zone:tank:"+tank+":0" || r.Label != "Port forward fuel level" {
		t.Fatalf("keyed by the tank item and named for it, got %q %q", r.ID, r.Label)
	}
	if msg := validateDashboardWidgets([]dashboardLayoutItem{{ID: "cluster:abcd1234", X: 0, Y: 0, W: 6, H: 10, Cluster: &dashboardClusterConfig{
		Ring:   dashboardGaugeConfig{Display: "radial", Quantity: "raw", Unit: "raw"},
		Centre: dashboardGaugeConfig{Display: "numeric", Quantity: "raw", Unit: "raw"},
		Fuel:   &dashboardClusterFuelRail{Side: "left", Bars: []dashboardClusterFuelBar{{TankID: tank, Zones: []gaugeZone{{From: 40, To: 60, State: alarmStateWarn}}}}},
	}}}, ""); msg == "" {
		t.Fatal("a mid-scale fuel zone cannot raise an alarm and must be refused")
	}
}
