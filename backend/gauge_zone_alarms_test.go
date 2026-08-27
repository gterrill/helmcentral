package main

import (
	"fmt"
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
	if msg := validateDashboardWidgets([]dashboardLayoutItem{widget}); msg == "" {
		t.Fatal("expected a mid-range alarm band to be rejected")
	}
}

// A zone in a unit the backend cannot convert would compare display units
// against SI values, so it must not be saveable.
func TestValidateGaugeConfigRejectsUnconvertibleZoneUnit(t *testing.T) {
	config := zoneGaugeConfig(0, 100, gaugeZone{From: 0, To: 15, State: alarmStateAlarm})
	config.Unit = "furlongs"
	if msg := validateDashboardWidgets([]dashboardLayoutItem{gaugeWidget("gauge:abcd1234", config)}); msg == "" {
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

// A cluster slot's zones must alarm like any other gauge's, or ADR 0050's
// whole point — that a red band is the alarm — holds everywhere but here.
func TestZoneDerivedAlarmRulesCoversClusterSlots(t *testing.T) {
	ring := *zoneGaugeConfig(0, 100, gaugeZone{From: 0, To: 15, State: alarmStateAlarm})
	corner := *zoneGaugeConfig(0, 100, gaugeZone{From: 90, To: 100, State: alarmStateWarn})

	withPages(t, dashboardLayoutItem{
		ID: "cluster:abcd1234", X: 0, Y: 0, W: 6, H: 10,
		Cluster: &dashboardClusterConfig{
			Title:   "Port",
			Ring:    ring,
			Centre:  dashboardGaugeConfig{Path: "propulsion.port.fuel.rate", Display: "numeric", Quantity: "raw", Unit: "raw"},
			Corners: []dashboardClusterCorner{{Label: "Oil", Rows: []dashboardGaugeConfig{corner}}},
		},
	})

	rules := zoneDerivedAlarmRules()
	if len(rules) != 2 {
		t.Fatalf("expected a rule from the ring and one from the corner row, got %d: %+v", len(rules), rules)
	}
}

/*
A fuel bar's zones alarm like any other gauge's (ADR 0050, ADR 0061).

The index is fixed rather than continuing the corner counter. Appending after
the corners actually present would keep ring, centre and corner ids stable, but
it would renumber the fuel bars the moment a corner row was added, so an
unrelated edit would silently orphan a low-fuel alarm's history.
*/
func TestZoneDerivedAlarmRulesCoversFuelBars(t *testing.T) {
	lowFuel := *zoneGaugeConfig(0, 100, gaugeZone{From: 0, To: 15, State: alarmStateWarn})
	lowFuel.Quantity = "ratio"
	lowFuel.Unit = "percent"

	withPages(t, dashboardLayoutItem{
		ID: "cluster:abcd1234", X: 0, Y: 0, W: 6, H: 10,
		Cluster: &dashboardClusterConfig{
			Title:  "Port",
			Ring:   dashboardGaugeConfig{Path: "propulsion.port.revolutions", Display: "radial", Quantity: "raw", Unit: "raw"},
			Centre: dashboardGaugeConfig{Path: "propulsion.port.fuel.rate", Display: "numeric", Quantity: "raw", Unit: "raw"},
			Fuel: &dashboardClusterFuelRail{
				Side: "left",
				Bars: []dashboardClusterFuelBar{{
					Level:    lowFuel,
					Capacity: dashboardGaugeConfig{Path: "tanks.fuel.5.capacity", Display: "numeric", Quantity: "volume", Unit: "L"},
				}},
			},
		},
	})

	rules := zoneDerivedAlarmRules()
	if len(rules) != 1 {
		t.Fatalf("expected one rule from the fuel bar's zone, got %d: %+v", len(rules), rules)
	}
	want := fmt.Sprintf("%scluster:abcd1234:%d:0", zoneDerivedAlarmRuleIDPrefix, clusterFuelZoneIndexBase)
	if rules[0].ID != want {
		t.Fatalf("expected the fuel bar at the fixed base index %q, got %q", want, rules[0].ID)
	}
}

// A capacity is a constant, not a reading. Zones on it would be meaningless,
// and an alarm derived from one would never clear.
func TestZoneDerivedAlarmRulesIgnoresFuelCapacities(t *testing.T) {
	capacity := *zoneGaugeConfig(0, 100, gaugeZone{From: 0, To: 15, State: alarmStateWarn})
	capacity.Quantity = "volume"
	capacity.Unit = "L"
	level := *zoneGaugeConfig(0, 100)
	level.Quantity = "ratio"
	level.Unit = "percent"

	withPages(t, dashboardLayoutItem{
		ID: "cluster:abcd1234", X: 0, Y: 0, W: 6, H: 10,
		Cluster: &dashboardClusterConfig{
			Title:  "Port",
			Ring:   dashboardGaugeConfig{Path: "propulsion.port.revolutions", Display: "radial", Quantity: "raw", Unit: "raw"},
			Centre: dashboardGaugeConfig{Path: "propulsion.port.fuel.rate", Display: "numeric", Quantity: "raw", Unit: "raw"},
			Fuel: &dashboardClusterFuelRail{
				Side: "left",
				Bars: []dashboardClusterFuelBar{{Level: level, Capacity: capacity}},
			},
		},
	})

	if rules := zoneDerivedAlarmRules(); len(rules) != 0 {
		t.Fatalf("expected no rules from a capacity's zones, got %+v", rules)
	}
}

/*
Adding a fuel rail must not renumber anything already derived.

An alarm id is what ties a firing alarm to its history and its acknowledgement,
so a config edit that quietly reissues them loses both.
*/
func TestZoneDerivedAlarmRuleIDsAreUnaffectedByAFuelRail(t *testing.T) {
	ring := *zoneGaugeConfig(0, 100, gaugeZone{From: 0, To: 15, State: alarmStateAlarm})
	corner := *zoneGaugeConfig(0, 100, gaugeZone{From: 90, To: 100, State: alarmStateWarn})
	base := func() *dashboardClusterConfig {
		return &dashboardClusterConfig{
			Title:   "Port",
			Ring:    ring,
			Centre:  dashboardGaugeConfig{Path: "propulsion.port.fuel.rate", Display: "numeric", Quantity: "raw", Unit: "raw"},
			Corners: []dashboardClusterCorner{{Label: "Oil", Rows: []dashboardGaugeConfig{corner}}},
		}
	}

	withPages(t, dashboardLayoutItem{ID: "cluster:abcd1234", X: 0, Y: 0, W: 6, H: 10, Cluster: base()})
	before := []string{}
	for _, r := range zoneDerivedAlarmRules() {
		before = append(before, r.ID)
	}

	level := *zoneGaugeConfig(0, 100, gaugeZone{From: 0, To: 15, State: alarmStateWarn})
	level.Quantity = "ratio"
	level.Unit = "percent"
	withFuel := base()
	withFuel.Fuel = &dashboardClusterFuelRail{
		Side: "left",
		Bars: []dashboardClusterFuelBar{{
			Level:    level,
			Capacity: dashboardGaugeConfig{Path: "tanks.fuel.5.capacity", Display: "numeric", Quantity: "volume", Unit: "L"},
		}},
	}
	withPages(t, dashboardLayoutItem{ID: "cluster:abcd1234", X: 0, Y: 0, W: 6, H: 10, Cluster: withFuel})

	after := map[string]bool{}
	for _, r := range zoneDerivedAlarmRules() {
		after[r.ID] = true
	}
	for _, id := range before {
		if !after[id] {
			t.Fatalf("adding a fuel rail reissued %q; ids: %v", id, after)
		}
	}
}
