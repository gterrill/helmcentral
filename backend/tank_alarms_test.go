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
		// The clear point sits 5 points back, so it has to stay inside 0-100.
		{"fuel", tankLevelThresholds{96, 10}, false},
		{"fuel", tankLevelThresholds{95, 10}, true},
		{"waste", tankLevelThresholds{4, 90}, false},
		{"waste", tankLevelThresholds{5, 90}, true},
		{"waste", tankLevelThresholds{80, 100}, true},
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

func TestTankDerivedAlarmRuleForAFuelTankIsOneRuleWithTwoLevels(t *testing.T) {
	tankTest(t)
	id := newTankItem(t, "Fuel Port Fwd", "tanks.fuel.0", 0, 0, false)
	rules := tankDerivedAlarmRules()
	if len(rules) != 1 {
		t.Fatalf("one rule per tank, got %+v", rules)
	}
	r := rules[0]
	if r.ID != "tank:"+id {
		t.Fatalf("id: %q", r.ID)
	}
	if r.Path != "tanks.fuel.0.currentLevel" || r.CapacityPath != "tanks.fuel.0.capacity" {
		t.Errorf("paths: %+v", r)
	}
	if r.Op != alarmOpBelow || r.Label != "Fuel Port Fwd Low" || !r.Enabled || r.State != alarmStateWarn {
		t.Errorf("shape: %+v", r)
	}
	if r.DwellSeconds != 60 || math.Abs(r.Hysteresis-0.05) > 1e-9 || r.StaleAfterSeconds <= 0 {
		t.Errorf("dwell/hysteresis/stale: %+v", r)
	}
	if math.Abs(r.Value-0.20) > 1e-9 || r.AlarmLevelValue == nil || math.Abs(*r.AlarmLevelValue-0.10) > 1e-9 {
		t.Errorf("levels: %+v", r)
	}
}

func TestTankDerivedAlarmRulesForWasteRaiseOnHigh(t *testing.T) {
	tankTest(t)
	newTankItem(t, "Black Water Stbd", "tanks.blackWater.1", 0, 0, false)
	rules := tankDerivedAlarmRules()
	if len(rules) != 1 {
		t.Fatalf("got %+v", rules)
	}
	r := rules[0]
	if r.Op != alarmOpAbove || r.Label != "Black Water Stbd High" {
		t.Errorf("waste is bad when high: %+v", r)
	}
	if math.Abs(r.Value-0.80) > 1e-9 || r.AlarmLevelValue == nil || math.Abs(*r.AlarmLevelValue-0.90) > 1e-9 {
		t.Errorf("values: %+v", r)
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
	if len(rules) != 1 || math.Abs(rules[0].Value-0.30) > 1e-9 || math.Abs(*rules[0].AlarmLevelValue-0.15) > 1e-9 {
		t.Fatalf("item warn 30 over kind alarm 15, got %+v", rules)
	}
}

func TestTankDerivedAlarmRulesSkipAlarmsOff(t *testing.T) {
	tankTest(t)
	newTankItem(t, "Off", "tanks.fuel.1", 0, 0, true)
	if rules := tankDerivedAlarmRules(); len(rules) != 0 {
		t.Fatalf("alarms off raises nothing, got %+v", rules)
	}
}

// A kind default can no longer be saved so that a tank ends up backwards, so a
// backwards pair reaching the rules (a hand-edited file) raises nothing and is
// logged.
func TestTankDerivedAlarmRulesSkipABackwardsPair(t *testing.T) {
	tankTest(t)
	newTankItem(t, "Backwards", "tanks.fuel.2", 15, 0, false)
	tankAlarmDefaultsMu.Lock()
	tankAlarmDefaultsState.Fuel = tankLevelThresholds{Warn: 25, Alarm: 16}
	tankAlarmDefaultsMu.Unlock()
	if rules := tankDerivedAlarmRules(); len(rules) != 0 {
		t.Fatalf("a backwards pair raises nothing, got %+v", rules)
	}
}

func TestSavingDefaultsIsRefusedWhenItWouldInvertATanksPair(t *testing.T) {
	tankTest(t)
	newTankItem(t, "Fuel Port Fwd", "tanks.fuel.0", 15, 0, false) // warn 15 over default alarm 10
	newTankItem(t, "Fuel Stbd Fwd", "tanks.fuel.1", 0, 0, false)
	d := builtinTankAlarmDefaults()
	d.Fuel = tankLevelThresholds{Warn: 25, Alarm: 16}
	err := saveTankAlarmDefaults(d)
	if err == nil {
		t.Fatal("expected the save to be refused")
	}
	if !strings.Contains(err.Error(), "Fuel Port Fwd") || strings.Contains(err.Error(), "Fuel Stbd Fwd") {
		t.Errorf("name only the affected tank: %v", err)
	}
	if got := currentTankAlarmDefaults().Fuel; got != builtinTankAlarmDefaults().Fuel {
		t.Errorf("a refused save changed state: %+v", got)
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

// step runs the engine to `at` with the given level and returns the events.
func step(engine *alarmEngine, rules []alarmRule, level, capacity float64, at time.Time) []alarmEvent {
	return engine.evaluate(rules, levelReader(level, capacity, at), at)
}

func TestTankAlarmDwellThenRaiseWithCardWording(t *testing.T) {
	tankTest(t)
	newTankItem(t, "Fuel Port Fwd", "tanks.fuel.0", 0, 0, false)
	rules := tankDerivedAlarmRules()
	engine := newAlarmEngine()

	if events := step(engine, rules, 0.19, 1.2, alarmNow); len(events) != 0 {
		t.Fatalf("no alarm before the dwell: %+v", events)
	}
	if events := step(engine, rules, 0.19, 1.2, alarmNow.Add(59*time.Second)); len(events) != 0 {
		t.Fatalf("59 s is not 60: %+v", events)
	}
	events := step(engine, rules, 0.19, 1.2, alarmNow.Add(60*time.Second))
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
}

func TestTankAlarmBodyWithoutCapacityIsJustThePercentage(t *testing.T) {
	tankTest(t)
	newTankItem(t, "Fuel Port Fwd", "tanks.fuel.0", 0, 0, false)
	rules := tankDerivedAlarmRules()
	engine := newAlarmEngine()
	step(engine, rules, 0.19, 0, alarmNow)
	events := step(engine, rules, 0.19, 0, alarmNow.Add(60*time.Second))
	if len(events) != 1 || events[0].Status.Message != "19%" {
		t.Fatalf("got %+v", events)
	}
}

func TestTankAlarmRaisesStraightAtAlarmWhenBothLevelsHoldForTheDwell(t *testing.T) {
	tankTest(t)
	newTankItem(t, "Fuel", "tanks.fuel.0", 0, 0, false)
	rules := tankDerivedAlarmRules()
	engine := newAlarmEngine()
	step(engine, rules, 0.05, 0, alarmNow)
	events := step(engine, rules, 0.05, 0, alarmNow.Add(60*time.Second))
	if len(events) != 1 || events[0].Kind != alarmEventRaised || events[0].Status.State != alarmStateAlarm {
		t.Fatalf("one raise at alarm, got %+v", events)
	}
}

// One tank is one alarm: it escalates warn to alarm and back, and only clears
// when it is clear of the warn level too.
func TestTankAlarmEscalatesAndDeEscalatesAsOneAlarm(t *testing.T) {
	tankTest(t)
	id := newTankItem(t, "Fuel", "tanks.fuel.0", 0, 0, false)
	rules := tankDerivedAlarmRules()
	engine := newAlarmEngine()
	at := alarmNow
	step(engine, rules, 0.18, 0, at)
	at = at.Add(60 * time.Second)
	if ev := step(engine, rules, 0.18, 0, at); len(ev) != 1 || ev[0].Status.State != alarmStateWarn {
		t.Fatalf("raise at warn: %+v", ev)
	}

	// Down through the alarm level: it must hold 60 s there too.
	at = at.Add(time.Second)
	if ev := step(engine, rules, 0.09, 0, at); len(ev) != 0 {
		t.Fatalf("alarm level not yet held: %+v", ev)
	}
	at = at.Add(60 * time.Second)
	ev := step(engine, rules, 0.09, 0, at)
	if len(ev) != 1 || ev[0].Kind != alarmEventChanged || ev[0].Status.State != alarmStateAlarm || ev[0].Status.Message != "9%" {
		t.Fatalf("escalation is one changed event to alarm: %+v", ev)
	}
	if got := len(engine.active()); got != 1 {
		t.Fatalf("still one alarm, got %d", got)
	}

	// Back above 10 but short of the 5 point band: still alarm.
	at = at.Add(time.Second)
	if ev := step(engine, rules, 0.14, 0, at); len(ev) != 0 {
		t.Fatalf("held at 14%%: %+v", ev)
	}
	// Past 15: de-escalates to warn, never a clear.
	at = at.Add(time.Second)
	ev = step(engine, rules, 0.16, 0, at)
	if len(ev) != 1 || ev[0].Kind != alarmEventChanged || ev[0].Status.State != alarmStateWarn {
		t.Fatalf("de-escalation is a changed event to warn: %+v", ev)
	}
	// Past 25: the only clear.
	at = at.Add(time.Second)
	ev = step(engine, rules, 0.26, 0, at)
	if len(ev) != 1 || ev[0].Kind != alarmEventCleared {
		t.Fatalf("one clear: %+v", ev)
	}
	if len(engine.active()) != 0 {
		t.Fatal("nothing active after the clear")
	}
	_ = id
}

func TestAckAppliesToTheTankAlarmAndDeEscalationKeepsIt(t *testing.T) {
	tankTest(t)
	id := newTankItem(t, "Fuel", "tanks.fuel.0", 0, 0, false)
	rules := tankDerivedAlarmRules()
	engine := newAlarmEngine()
	at := alarmNow
	step(engine, rules, 0.05, 0, at)
	at = at.Add(60 * time.Second)
	step(engine, rules, 0.05, 0, at) // raised at alarm
	if !engine.acknowledge("tank:"+id, at) {
		t.Fatal("the tank alarm should acknowledge")
	}
	// Level recovers to between 15 and 20: de-escalates, stays acknowledged.
	at = at.Add(time.Second)
	ev := step(engine, rules, 0.17, 0, at)
	if len(ev) != 1 || ev[0].Kind != alarmEventChanged {
		t.Fatalf("de-escalation: %+v", ev)
	}
	if st := engine.statusFor("tank:" + id); st.Phase != alarmPhaseAcknowledged || st.State != alarmStateWarn {
		t.Fatalf("acked warn expected, got %+v", st)
	}
	// Re-escalation re-raises unacknowledged.
	at = at.Add(time.Second)
	step(engine, rules, 0.05, 0, at)
	at = at.Add(60 * time.Second)
	ev = step(engine, rules, 0.05, 0, at)
	if len(ev) != 1 || ev[0].Kind != alarmEventChanged || ev[0].Status.State != alarmStateAlarm {
		t.Fatalf("re-escalation: %+v", ev)
	}
	if st := engine.statusFor("tank:" + id); st.Phase != alarmPhaseActive {
		t.Fatalf("re-escalation is unacknowledged, got %+v", st)
	}
}

func TestTankAlarmClearsFivePointsBackOnTheGoodSide(t *testing.T) {
	tankTest(t)
	newTankItem(t, "Fuel", "tanks.fuel.0", 0, 0, false)
	rules := tankDerivedAlarmRules()
	engine := newAlarmEngine()
	step(engine, rules, 0.19, 0, alarmNow)
	at := alarmNow.Add(60 * time.Second)
	step(engine, rules, 0.19, 0, at)
	at = at.Add(time.Second)
	if events := step(engine, rules, 0.24, 0, at); len(events) != 0 {
		t.Fatalf("still held at 24%%: %+v", events)
	}
	at = at.Add(time.Second)
	events := step(engine, rules, 0.26, 0, at)
	if len(events) != 1 || events[0].Kind != alarmEventCleared {
		t.Fatalf("clears above 25%%: %+v", events)
	}
}

func TestWasteTankClearsFivePointsBelowItsWarnPoint(t *testing.T) {
	tankTest(t)
	newTankItem(t, "Black", "tanks.blackWater.0", 0, 0, false)
	rules := tankDerivedAlarmRules()
	engine := newAlarmEngine()
	step(engine, rules, 0.82, 0, alarmNow)
	at := alarmNow.Add(60 * time.Second)
	if events := step(engine, rules, 0.82, 0, at); len(events) != 1 {
		t.Fatalf("raise: %+v", events)
	}
	at = at.Add(time.Second)
	if events := step(engine, rules, 0.76, 0, at); len(events) != 0 {
		t.Fatalf("76%% is not below 75%%: %+v", events)
	}
	at = at.Add(time.Second)
	if events := step(engine, rules, 0.74, 0, at); len(events) != 1 || events[0].Kind != alarmEventCleared {
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

// The tile shows senders that publish 0-100 as well as 0-1; the alarm reads the
// same percentage.
func TestTankAlarmReaderNormalisesBothScales(t *testing.T) {
	read := tankLevelAlarmReader(func(path string) alarmSample {
		switch path {
		case "tanks.fuel.0.currentLevel":
			return alarmSample{Value: 0.19, Present: true}
		case "tanks.fuel.1.currentLevel":
			return alarmSample{Value: 45, Present: true}
		case "tanks.fuel.2.currentLevel":
			return alarmSample{Value: -1, Present: true}
		case "tanks.fuel.0.capacity":
			return alarmSample{Value: 1.2, Present: true}
		}
		return alarmSample{}
	})
	if v := read("tanks.fuel.0.currentLevel").Value; math.Abs(v-0.19) > 1e-9 {
		t.Errorf("ratio sender: %v", v)
	}
	if v := read("tanks.fuel.1.currentLevel").Value; math.Abs(v-0.45) > 1e-9 {
		t.Errorf("percent sender must read 0.45, got %v", v)
	}
	if read("tanks.fuel.2.currentLevel").Present {
		t.Error("a negative level is absent, as on the tile")
	}
	if v := read("tanks.fuel.0.capacity").Value; v != 1.2 {
		t.Errorf("capacity is left alone: %v", v)
	}
	if body := tankAlarmBody(0.45, nil); body != "45%" {
		t.Errorf("never 4500%%: %q", body)
	}
}

func TestTankAlarmBodiesAreSetOnTheServerLive(t *testing.T) {
	capacity := 1.2
	in := []alarmStatus{
		{RuleID: "tank:a", Value: 0.19, Capacity: &capacity, Message: "stale text"},
		{RuleID: "other", Value: 3, Message: "keep"},
	}
	out := withTankAlarmBodies(in)
	if out[0].Message != "19% · 228 L" || out[1].Message != "keep" {
		t.Errorf("got %+v", out)
	}
}

func TestAlarmLogFollowsAnEscalationInOneRow(t *testing.T) {
	store, err := newAlarmLogStore(filepath.Join(t.TempDir(), "log.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if _, err := store.RecordRaised(alarmLogEntry{RuleID: "tank:a", Label: "Fuel Low", Path: "p", State: alarmStateWarn, Message: "19%", RaisedAt: alarmNow}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkAcknowledged("tank:a", alarmNow.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	// De-escalation keeps the acknowledgement; escalation drops it.
	if err := store.RecordStateChange("tank:a", alarmStateAlarm, "9%", true); err != nil {
		t.Fatal(err)
	}
	entries, _ := store.Recent(10)
	if len(entries) != 1 || entries[0].State != alarmStateAlarm || entries[0].Message != "9%" || entries[0].AckedAt != nil {
		t.Fatalf("escalated row: %+v", entries)
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
	if err := annotateTankLevels(tanks); err != nil {
		t.Fatal(err)
	}
	if tanks[0].WarnPercent != 30 || tanks[0].AlarmPercent != 10 || tanks[0].AlarmsOff {
		t.Errorf("fuel: %+v", tanks[0])
	}
	if tanks[1].WarnPercent != 20 || tanks[1].AlarmPercent != 10 || !tanks[1].AlarmsOff {
		t.Errorf("water: %+v", tanks[1])
	}
}

// Equipment that cannot be read means alarms are dead; the tile must say so
// rather than colour by defaults.
func TestAnnotateTankLevelsFailsWhenTheItemsCannotBeRead(t *testing.T) {
	tankTest(t)
	store := vesselFixtureStore(t)
	_ = store.db.Close()
	invalidateSourceItems()
	if err := annotateTankLevels([]tankLevelData{{ID: "fuel.0", Kind: "fuel"}}); err == nil {
		t.Fatal("expected an error, not default levels")
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
