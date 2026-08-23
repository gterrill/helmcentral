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
