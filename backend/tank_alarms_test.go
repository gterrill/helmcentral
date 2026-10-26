package main

import (
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// tankTest installs a clean defaults file and an empty equipment store.
func tankTest(t *testing.T) {
	t.Helper()
	t.Setenv("TANK_ALARM_DEFAULTS_FILE", filepath.Join(t.TempDir(), "tank-alarm-defaults.json"))
	if err := loadTankAlarmDefaults(); err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	vesselFixtureStore(t)
}

func newTankItem(t *testing.T, name, source string, warn, alarm float64, off bool) string {
	t.Helper()
	store := vesselFixtureStore(t)
	created, err := store.CreateEquipment(equipmentItem{
		Name: name, System: sourceSystem(sourceTank), Quantity: 1, SignalKSource: source,
		TankWarnPercent: warn, TankAlarmPercent: alarm, TankAlarmsOff: off,
	})
	if err != nil {
		t.Fatalf("create tank %q: %v", name, err)
	}
	invalidateSourceItems()
	return created.ID
}

func TestBuiltinTankAlarmDefaults(t *testing.T) {
	d := builtinTankAlarmDefaults()
	if d.Fuel != (tankLevelThresholds{Warn: 20, Alarm: 10}) {
		t.Errorf("fuel: %+v", d.Fuel)
	}
	if d.Water != (tankLevelThresholds{Warn: 20, Alarm: 10}) {
		t.Errorf("water: %+v", d.Water)
	}
	if d.Waste != (tankLevelThresholds{Warn: 80, Alarm: 90}) {
		t.Errorf("waste: %+v", d.Waste)
	}
}

func TestResolveTankLevels(t *testing.T) {
	d := builtinTankAlarmDefaults()
	got, off := resolveTankLevels("fuel", d, sourceItem{})
	if off || got != (tankLevelThresholds{Warn: 20, Alarm: 10}) {
		t.Errorf("blank item uses the kind default, got %+v off=%v", got, off)
	}
	got, _ = resolveTankLevels("fuel", d, sourceItem{TankWarnPercent: 30})
	if got != (tankLevelThresholds{Warn: 30, Alarm: 10}) {
		t.Errorf("warn override only: %+v", got)
	}
	got, _ = resolveTankLevels("waste", d, sourceItem{TankAlarmPercent: 95})
	if got != (tankLevelThresholds{Warn: 80, Alarm: 95}) {
		t.Errorf("alarm override only: %+v", got)
	}
	got, off = resolveTankLevels("water", d, sourceItem{TankAlarmsOff: true})
	if !off || got != (tankLevelThresholds{Warn: 20, Alarm: 10}) {
		t.Errorf("alarms off keeps the thresholds for colour: %+v off=%v", got, off)
	}
}

func TestValidateTankLevels(t *testing.T) {
	cases := []struct {
		kind string
		t    tankLevelThresholds
		ok   bool
	}{
		{"fuel", tankLevelThresholds{20, 10}, true},
		{"fuel", tankLevelThresholds{10, 20}, false},
		{"fuel", tankLevelThresholds{10, 10}, false},
		{"water", tankLevelThresholds{50, 0}, true},
		{"waste", tankLevelThresholds{80, 90}, true},
		{"waste", tankLevelThresholds{90, 80}, false},
		{"waste", tankLevelThresholds{80, 101}, false},
		{"fuel", tankLevelThresholds{101, 10}, false},
		{"fuel", tankLevelThresholds{20, -1}, false},
	}
	for _, c := range cases {
		err := validateTankLevels(c.kind, c.t)
		if (err == nil) != c.ok {
			t.Errorf("%s %+v: err=%v want ok=%v", c.kind, c.t, err, c.ok)
		}
	}
}

func TestTankAlarmDefaultsPersistAndRefuseBadInput(t *testing.T) {
	tankTest(t)
	next := builtinTankAlarmDefaults()
	next.Fuel = tankLevelThresholds{Warn: 25, Alarm: 12}
	if err := saveTankAlarmDefaults(next); err != nil {
		t.Fatal(err)
	}
	if err := loadTankAlarmDefaults(); err != nil {
		t.Fatal(err)
	}
	if got := currentTankAlarmDefaults(); got.Fuel != next.Fuel {
		t.Errorf("not persisted: %+v", got)
	}
	bad := next
	bad.Waste = tankLevelThresholds{Warn: 95, Alarm: 90}
	if err := saveTankAlarmDefaults(bad); err == nil {
		t.Error("a waste alarm below its warn must be refused")
	}
	if got := currentTankAlarmDefaults(); got.Waste != builtinTankAlarmDefaults().Waste {
		t.Errorf("a refused save changed state: %+v", got)
	}
}

func TestTankAlarmDefaultsCorruptFileFailsLoud(t *testing.T) {
	tankTest(t)
	if err := os.WriteFile(tankAlarmDefaultsFilePath(), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := loadTankAlarmDefaults(); err == nil {
		t.Fatal("a corrupt defaults file must not read as the built-in numbers")
	}
}

func TestTankDerivedAlarmRulesForAFuelTank(t *testing.T) {
	tankTest(t)
	id := newTankItem(t, "Fuel Port Fwd", "tanks.fuel.0", 0, 0, false)
	rules := tankDerivedAlarmRules()
	if len(rules) != 2 {
		t.Fatalf("a warn and an alarm rule, got %+v", rules)
	}
	warn, alarm := rules[0], rules[1]
	if warn.ID != "tank:"+id+":warn" || alarm.ID != "tank:"+id+":alarm" {
		t.Fatalf("ids: %q %q", warn.ID, alarm.ID)
	}
	for _, r := range rules {
		if r.Path != "tanks.fuel.0.currentLevel" || r.CapacityPath != "tanks.fuel.0.capacity" {
			t.Errorf("paths: %+v", r)
		}
		if r.Op != alarmOpBelow || r.Label != "Fuel Port Fwd Low" || !r.Enabled {
			t.Errorf("shape: %+v", r)
		}
		if r.DwellSeconds != 60 || math.Abs(r.Hysteresis-0.05) > 1e-9 || r.StaleAfterSeconds <= 0 {
			t.Errorf("dwell/hysteresis/stale: %+v", r)
		}
	}
	if warn.State != alarmStateWarn || math.Abs(warn.Value-0.20) > 1e-9 {
		t.Errorf("warn: %+v", warn)
	}
	if alarm.State != alarmStateAlarm || math.Abs(alarm.Value-0.10) > 1e-9 {
		t.Errorf("alarm: %+v", alarm)
	}
}

func TestTankDerivedAlarmRulesForWasteRaiseOnHigh(t *testing.T) {
	tankTest(t)
	newTankItem(t, "Black Water Stbd", "tanks.blackWater.1", 0, 0, false)
	rules := tankDerivedAlarmRules()
	if len(rules) != 2 {
		t.Fatalf("got %+v", rules)
	}
	for _, r := range rules {
		if r.Op != alarmOpAbove || r.Label != "Black Water Stbd High" {
			t.Errorf("waste is bad when high: %+v", r)
		}
	}
	if math.Abs(rules[0].Value-0.80) > 1e-9 || math.Abs(rules[1].Value-0.90) > 1e-9 {
		t.Errorf("values: %v %v", rules[0].Value, rules[1].Value)
	}
}

func TestTankDerivedAlarmRulesHonourOverridesAndKindDefaults(t *testing.T) {
	tankTest(t)
	newTankItem(t, "Fresh Water", "tanks.freshWater.0", 30, 0, false)
	d := builtinTankAlarmDefaults()
	d.Water = tankLevelThresholds{Warn: 40, Alarm: 15}
	if err := saveTankAlarmDefaults(d); err != nil {
		t.Fatal(err)
	}
	rules := tankDerivedAlarmRules()
	if len(rules) != 2 || math.Abs(rules[0].Value-0.30) > 1e-9 || math.Abs(rules[1].Value-0.15) > 1e-9 {
		t.Fatalf("item warn 30 over kind alarm 15, got %+v", rules)
	}
}

func TestTankDerivedAlarmRulesSkipAlarmsOffAndInvalidPairs(t *testing.T) {
	tankTest(t)
	newTankItem(t, "Off", "tanks.fuel.1", 0, 0, true)
	newTankItem(t, "Backwards", "tanks.fuel.2", 15, 0, false)
	// The kind default moves up under the item's own warn level: warn 15 over
	// an alarm of 16 is backwards, so that tank raises nothing.
	d := builtinTankAlarmDefaults()
	d.Fuel = tankLevelThresholds{Warn: 25, Alarm: 16}
	if err := saveTankAlarmDefaults(d); err != nil {
		t.Fatal(err)
	}
	if rules := tankDerivedAlarmRules(); len(rules) != 0 {
		t.Fatalf("alarms off and a backwards pair raise nothing, got %+v", rules)
	}
}

func TestTankDerivedAlarmRulesNeedAnItem(t *testing.T) {
	tankTest(t)
	if rules := tankDerivedAlarmRules(); len(rules) != 0 {
		t.Fatalf("a tank with no equipment item has no alarm, got %+v", rules)
	}
}

// levelReader reads a tank level (a ratio) and optional capacity (m3).
func levelReader(level float64, capacity float64, seen time.Time) alarmReader {
	return func(path string) alarmSample {
		switch {
		case strings.HasSuffix(path, ".currentLevel"):
			return alarmSample{Value: level, Present: true, LastSeen: seen}
		case strings.HasSuffix(path, ".capacity") && capacity > 0:
			return alarmSample{Value: capacity, Present: true, LastSeen: seen}
		}
		return alarmSample{}
	}
}

func TestTankAlarmDwellThenRaiseWithCardWording(t *testing.T) {
	tankTest(t)
	newTankItem(t, "Fuel Port Fwd", "tanks.fuel.0", 0, 0, false)
	rules := tankDerivedAlarmRules()
	engine := newAlarmEngine()
	read := levelReader(0.19, 1.2, alarmNow)

	if events := engine.evaluate(rules, read, alarmNow); len(events) != 0 {
		t.Fatalf("no alarm before the dwell: %+v", events)
	}
	later := alarmNow.Add(59 * time.Second)
	if events := engine.evaluate(rules, levelReader(0.19, 1.2, later), later); len(events) != 0 {
		t.Fatalf("59 s is not 60: %+v", events)
	}
	at := alarmNow.Add(60 * time.Second)
	events := engine.evaluate(rules, levelReader(0.19, 1.2, at), at)
	if len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("expected one raise at 60 s, got %+v", events)
	}
	st := events[0].Status
	if st.State != alarmStateWarn || st.Label != "Fuel Port Fwd Low" {
		t.Errorf("state/title: %+v", st)
	}
	if st.Message != "19% · 228 L" {
		t.Errorf("body: %q", st.Message)
	}
	if st.ClearValue == nil || math.Abs(*st.ClearValue-0.25) > 1e-9 {
		t.Errorf("clears above 25%%, got %v", st.ClearValue)
	}
	if st.Capacity == nil || *st.Capacity != 1.2 {
		t.Errorf("capacity is carried for the card: %v", st.Capacity)
	}
}

func TestTankAlarmBodyWithoutCapacityIsJustThePercentage(t *testing.T) {
	tankTest(t)
	newTankItem(t, "Fuel Port Fwd", "tanks.fuel.0", 0, 0, false)
	rules := tankDerivedAlarmRules()
	engine := newAlarmEngine()
	engine.evaluate(rules, levelReader(0.19, 0, alarmNow), alarmNow)
	at := alarmNow.Add(60 * time.Second)
	events := engine.evaluate(rules, levelReader(0.19, 0, at), at)
	if len(events) != 1 || events[0].Status.Message != "19%" {
		t.Fatalf("got %+v", events)
	}
}

func TestTankAlarmClearsFivePointsBackOnTheGoodSide(t *testing.T) {
	tankTest(t)
	newTankItem(t, "Fuel", "tanks.fuel.0", 0, 0, false)
	rules := tankDerivedAlarmRules()[:1] // warn only
	engine := newAlarmEngine()
	engine.evaluate(rules, levelReader(0.19, 0, alarmNow), alarmNow)
	at := alarmNow.Add(60 * time.Second)
	engine.evaluate(rules, levelReader(0.19, 0, at), at)
	// 24% is back over the warn point but inside the 5 point band.
	at = at.Add(time.Second)
	if events := engine.evaluate(rules, levelReader(0.24, 0, at), at); len(events) != 0 {
		t.Fatalf("still held at 24%%: %+v", events)
	}
	at = at.Add(time.Second)
	events := engine.evaluate(rules, levelReader(0.26, 0, at), at)
	if len(events) != 1 || events[0].Kind != alarmEventCleared {
		t.Fatalf("clears above 25%%: %+v", events)
	}
}

func TestWasteTankClearsFivePointsBelowItsWarnPoint(t *testing.T) {
	tankTest(t)
	newTankItem(t, "Black", "tanks.blackWater.0", 0, 0, false)
	rules := tankDerivedAlarmRules()[:1]
	engine := newAlarmEngine()
	engine.evaluate(rules, levelReader(0.82, 0, alarmNow), alarmNow)
	at := alarmNow.Add(60 * time.Second)
	if events := engine.evaluate(rules, levelReader(0.82, 0, at), at); len(events) != 1 {
		t.Fatalf("raise: %+v", events)
	}
	at = at.Add(time.Second)
	if events := engine.evaluate(rules, levelReader(0.76, 0, at), at); len(events) != 0 {
		t.Fatalf("76%% is not below 75%%: %+v", events)
	}
	at = at.Add(time.Second)
	if events := engine.evaluate(rules, levelReader(0.74, 0, at), at); len(events) != 1 || events[0].Kind != alarmEventCleared {
		t.Fatalf("clear below 75%%: %+v", events)
	}
}

func TestStaleTankReadingRaisesNothing(t *testing.T) {
	tankTest(t)
	newTankItem(t, "Fuel", "tanks.fuel.0", 0, 0, false)
	rules := tankDerivedAlarmRules()
	engine := newAlarmEngine()
	old := alarmNow.Add(-10 * time.Minute)
	for i := 0; i <= 3; i++ {
		at := alarmNow.Add(time.Duration(i*30) * time.Second)
		if events := engine.evaluate(rules, levelReader(0.05, 0, old), at); len(events) != 0 {
			t.Fatalf("a stopped sender must not raise a level alarm: %+v", events)
		}
	}
	if len(engine.active()) != 0 {
		t.Fatalf("nothing should be active: %+v", engine.active())
	}
}

func TestCollapseTankAlarmsKeepsTheWorstPerTank(t *testing.T) {
	statuses := []alarmStatus{
		{RuleID: "tank:a:warn", Path: "tanks.fuel.0.currentLevel", State: alarmStateWarn, Phase: alarmPhaseAcknowledged},
		{RuleID: "tank:a:alarm", Path: "tanks.fuel.0.currentLevel", State: alarmStateAlarm, Phase: alarmPhaseActive},
		{RuleID: "tank:b:warn", Path: "tanks.fuel.1.currentLevel", State: alarmStateWarn, Phase: alarmPhaseActive},
		{RuleID: "other", Path: "environment.depth", State: alarmStateWarn, Phase: alarmPhaseActive},
	}
	got := collapseTankAlarms(statuses)
	if len(got) != 3 {
		t.Fatalf("one card per tank: %+v", got)
	}
	ids := map[string]bool{}
	for _, s := range got {
		ids[s.RuleID] = true
	}
	if !ids["tank:a:alarm"] || ids["tank:a:warn"] || !ids["tank:b:warn"] || !ids["other"] {
		t.Errorf("wrong cards kept: %v", ids)
	}
}

func TestAcknowledgedWarnReRaisesAsAnAlarmAtTheAlarmLevel(t *testing.T) {
	tankTest(t)
	id := newTankItem(t, "Fuel", "tanks.fuel.0", 0, 0, false)
	rules := tankDerivedAlarmRules()
	engine := newAlarmEngine()
	at := alarmNow
	engine.evaluate(rules, levelReader(0.18, 0, at), at)
	at = at.Add(60 * time.Second)
	engine.evaluate(rules, levelReader(0.18, 0, at), at)
	if !engine.acknowledge("tank:"+id+":warn", at) {
		t.Fatal("warn should acknowledge")
	}
	at = at.Add(time.Second)
	engine.evaluate(rules, levelReader(0.09, 0, at), at)
	at = at.Add(60 * time.Second)
	engine.evaluate(rules, levelReader(0.09, 0, at), at)
	cards := collapseTankAlarms(engine.active())
	if len(cards) != 1 || cards[0].State != alarmStateAlarm || cards[0].Phase != alarmPhaseActive {
		t.Fatalf("an unacknowledged alarm card replaces the acknowledged warn: %+v", cards)
	}
}

func TestTankPathsAreOwnedByHelmcentral(t *testing.T) {
	tankTest(t)
	newTankItem(t, "Fuel", "tanks.fuel.0", 0, 0, false)
	newTankItem(t, "Silent", "tanks.fuel.1", 0, 0, true)
	owns := helmcentralOwnershipPredicate()
	if !owns("tanks.fuel.0.currentLevel") {
		t.Error("a tank with Helmcentral alarms owns its path, so a foreign notification there is superseded")
	}
	if owns("tanks.fuel.1.currentLevel") {
		t.Error("alarms off hands the path back")
	}
	if owns("tanks.fuel.9.currentLevel") {
		t.Error("an unmapped tank is not ours")
	}
}

func TestEquipmentStoresTankOverridesOnlyOnTanks(t *testing.T) {
	tankTest(t)
	id := newTankItem(t, "Fuel", "tanks.fuel.0", 25, 12, true)
	got, err := equipmentByID(vesselFixtureStore(t).db, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.TankWarnPercent != 25 || got.TankAlarmPercent != 12 || !got.TankAlarmsOff {
		t.Errorf("not stored: %+v", got)
	}
	_, err = vesselFixtureStore(t).CreateEquipment(equipmentItem{
		Name: "Pump", System: "plumbing", Quantity: 1, TankWarnPercent: 20,
	})
	if err == nil {
		t.Error("tank thresholds on an item that is not a tank must be refused")
	}
	_, err = vesselFixtureStore(t).CreateEquipment(equipmentItem{
		Name: "Fuel 2", System: sourceSystem(sourceTank), Quantity: 1, SignalKSource: "tanks.fuel.3",
		TankWarnPercent: 10, TankAlarmPercent: 20,
	})
	if err == nil {
		t.Error("a low-bad tank whose alarm is above its warn must be refused")
	}
	_, err = vesselFixtureStore(t).CreateEquipment(equipmentItem{
		Name: "Fuel 3", System: sourceSystem(sourceTank), Quantity: 1, SignalKSource: "tanks.fuel.4",
		TankWarnPercent: 120,
	})
	if err == nil {
		t.Error("over 100 percent must be refused")
	}
}

func TestTanksStatePayloadCarriesResolvedThresholds(t *testing.T) {
	tankTest(t)
	newTankItem(t, "Fuel Port Fwd", "tanks.fuel.0", 30, 0, false)
	newTankItem(t, "Fresh", "tanks.freshWater.0", 0, 0, true)
	tanks := []tankLevelData{
		{ID: "fuel.0", Kind: "fuel", Category: "fuel"},
		{ID: "freshWater.0", Kind: "water", Category: "freshWater"},
	}
	annotateTankLevels(tanks)
	if tanks[0].WarnPercent != 30 || tanks[0].AlarmPercent != 10 || tanks[0].AlarmsOff {
		t.Errorf("fuel: %+v", tanks[0])
	}
	if tanks[1].WarnPercent != 20 || tanks[1].AlarmPercent != 10 || !tanks[1].AlarmsOff {
		t.Errorf("water: %+v", tanks[1])
	}
}

func TestTankAlarmRuleHandlersRefuseDerivedIDs(t *testing.T) {
	c, rec := newDashboardPagesRequest(t, http.MethodDelete, "/api/alarm-rules/tank:abc:warn", nil)
	c.SetParamNames("id")
	c.SetParamValues("tank:abc:warn")
	if err := deleteAlarmRuleHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "tank") {
		t.Fatalf("expected 400 naming the tank, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestTankAlarmDefaultsHandlers(t *testing.T) {
	tankTest(t)
	e := echo.New()
	e.GET("/d", getTankAlarmDefaultsHandler)
	e.PUT("/d", setTankAlarmDefaultsHandler)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/d", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"fuel":{"warn":20,"alarm":10}`) {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body.String())
	}

	put := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/d", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		e.ServeHTTP(rec, req)
		return rec
	}
	if rec := put(`{"fuel":{"warn":30,"alarm":15},"water":{"warn":20,"alarm":10},"waste":{"warn":80,"alarm":90}}`); rec.Code != http.StatusOK {
		t.Fatalf("valid PUT: %d %s", rec.Code, rec.Body.String())
	}
	if rec := put(`{"fuel":{"warn":10,"alarm":15},"water":{"warn":20,"alarm":10},"waste":{"warn":80,"alarm":90}}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("backwards fuel must be refused: %d", rec.Code)
	}
	if got := currentTankAlarmDefaults().Fuel; got != (tankLevelThresholds{Warn: 30, Alarm: 15}) {
		t.Fatalf("the refused save changed state: %+v", got)
	}
}
