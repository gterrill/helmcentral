package main

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

var alarmNow = time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)

// staticReader answers every path with one sample, which is all a
// single-rule test needs.
func staticReader(value float64) alarmReader {
	return func(string) alarmSample { return alarmSample{Value: value, Present: true, LastSeen: alarmNow} }
}

func absentReader() alarmReader {
	return func(string) alarmSample { return alarmSample{} }
}

// staleReader reports a path that exists but last updated long ago.
func staleReader() alarmReader {
	return func(string) alarmSample {
		return alarmSample{Value: 12.5, Present: true, LastSeen: alarmNow.Add(-10 * time.Minute)}
	}
}

func lowVoltageRule() alarmRule {
	rule := validRule() // below 11.8, hysteresis 0.3, dwell 30s
	rule.ID = "rule-1"
	return rule
}

func TestEngineRaisesOnceDwellHasElapsed(t *testing.T) {
	engine := newAlarmEngine()
	rules := []alarmRule{lowVoltageRule()}

	events := engine.evaluate(rules, staticReader(11.0), alarmNow)
	if len(events) != 0 {
		t.Fatalf("condition true but dwell not elapsed: expected no events, got %d", len(events))
	}
	if got := engine.statusFor("rule-1").Phase; got != alarmPhasePending {
		t.Fatalf("phase: got %q, want %q", got, alarmPhasePending)
	}

	events = engine.evaluate(rules, staticReader(11.0), alarmNow.Add(31*time.Second))
	if len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("expected one raised event after dwell, got %+v", events)
	}
	if got := engine.statusFor("rule-1").Phase; got != alarmPhaseActive {
		t.Fatalf("phase: got %q, want %q", got, alarmPhaseActive)
	}
}

func TestEngineDoesNotRaiseWhileConditionIsIntermittent(t *testing.T) {
	engine := newAlarmEngine()
	rules := []alarmRule{lowVoltageRule()}

	// Dips below, recovers before the dwell elapses, dips again. The dwell
	// timer must restart, so nothing is ever raised.
	engine.evaluate(rules, staticReader(11.0), alarmNow)
	engine.evaluate(rules, staticReader(12.5), alarmNow.Add(10*time.Second))
	engine.evaluate(rules, staticReader(11.0), alarmNow.Add(20*time.Second))
	events := engine.evaluate(rules, staticReader(11.0), alarmNow.Add(45*time.Second))

	if len(events) != 0 {
		t.Fatalf("dwell should have restarted after recovery, got %+v", events)
	}
}

// The deadband is what stops a value hovering at the threshold producing an
// alarm storm — the most common reason people switch marine alarms off.
func TestEngineHoldsActiveInsideHysteresisDeadband(t *testing.T) {
	engine := newAlarmEngine()
	rules := []alarmRule{lowVoltageRule()}

	engine.evaluate(rules, staticReader(11.0), alarmNow)
	engine.evaluate(rules, staticReader(11.0), alarmNow.Add(31*time.Second))

	// Back above the threshold but inside the 0.3 deadband: still active.
	events := engine.evaluate(rules, staticReader(11.9), alarmNow.Add(60*time.Second))
	if len(events) != 0 {
		t.Fatalf("inside the deadband nothing should change, got %+v", events)
	}
	if got := engine.statusFor("rule-1").Phase; got != alarmPhaseActive {
		t.Fatalf("phase inside deadband: got %q, want %q", got, alarmPhaseActive)
	}

	// Clear of the deadband (11.8 + 0.3): clears.
	events = engine.evaluate(rules, staticReader(12.2), alarmNow.Add(90*time.Second))
	if len(events) != 1 || events[0].Kind != alarmEventCleared {
		t.Fatalf("expected one cleared event past the deadband, got %+v", events)
	}
	if got := engine.statusFor("rule-1").Phase; got != alarmPhaseNormal {
		t.Fatalf("phase after clearing: got %q, want %q", got, alarmPhaseNormal)
	}
}

func TestEngineOscillationAtThresholdDoesNotStorm(t *testing.T) {
	engine := newAlarmEngine()
	rule := lowVoltageRule()
	rule.DwellSeconds = 0
	rules := []alarmRule{rule}

	raised := 0
	at := alarmNow
	for i := 0; i < 20; i++ {
		// Alternates either side of 11.8 but never leaves the deadband.
		value := 11.79
		if i%2 == 0 {
			value = 11.81
		}
		for _, event := range engine.evaluate(rules, staticReader(value), at) {
			if event.Kind == alarmEventRaised {
				raised++
			}
		}
		at = at.Add(time.Second)
	}

	if raised != 1 {
		t.Fatalf("oscillation inside the deadband raised %d times, want exactly 1", raised)
	}
}

func TestEngineAcknowledgeStopsReNotifyingButKeepsAlarmActive(t *testing.T) {
	engine := newAlarmEngine()
	rule := lowVoltageRule()
	rule.DwellSeconds = 0
	rules := []alarmRule{rule}

	engine.evaluate(rules, staticReader(11.0), alarmNow)
	if !engine.acknowledge("rule-1", alarmNow.Add(time.Second)) {
		t.Fatalf("expected acknowledge to succeed on an active alarm")
	}

	status := engine.statusFor("rule-1")
	if status.Phase != alarmPhaseAcknowledged {
		t.Fatalf("phase: got %q, want %q", status.Phase, alarmPhaseAcknowledged)
	}
	// Still a live condition — acknowledging silences, it does not resolve.
	if status.State != alarmStateAlarm {
		t.Fatalf("acknowledged alarm should keep its severity, got %q", status.State)
	}
}

// SignalK is explicit that an emergency cannot be silenced regardless of
// canSilence, so acknowledging one must be refused.
func TestEngineRefusesToAcknowledgeEmergency(t *testing.T) {
	engine := newAlarmEngine()
	rule := lowVoltageRule()
	rule.DwellSeconds = 0
	rule.State = alarmStateEmergency
	rules := []alarmRule{rule}

	engine.evaluate(rules, staticReader(11.0), alarmNow)
	if engine.acknowledge("rule-1", alarmNow.Add(time.Second)) {
		t.Fatalf("an emergency must not be acknowledgeable")
	}
	if got := engine.statusFor("rule-1").Phase; got != alarmPhaseActive {
		t.Fatalf("phase: got %q, want %q", got, alarmPhaseActive)
	}
}

func TestEngineReRaisesUnacknowledgedAfterClearing(t *testing.T) {
	engine := newAlarmEngine()
	rule := lowVoltageRule()
	rule.DwellSeconds = 0
	rules := []alarmRule{rule}

	engine.evaluate(rules, staticReader(11.0), alarmNow)
	engine.acknowledge("rule-1", alarmNow.Add(time.Second))
	engine.evaluate(rules, staticReader(12.5), alarmNow.Add(2*time.Second))

	events := engine.evaluate(rules, staticReader(11.0), alarmNow.Add(3*time.Second))
	if len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("a fresh occurrence must raise again, got %+v", events)
	}
	if got := engine.statusFor("rule-1").Phase; got != alarmPhaseActive {
		t.Fatalf("re-raised alarm must not stay acknowledged, got %q", got)
	}
}

func TestEngineDisabledRuleNeverFires(t *testing.T) {
	engine := newAlarmEngine()
	rule := lowVoltageRule()
	rule.DwellSeconds = 0
	rule.Enabled = false

	events := engine.evaluate([]alarmRule{rule}, staticReader(11.0), alarmNow)
	if len(events) != 0 {
		t.Fatalf("a disabled rule must not fire, got %+v", events)
	}
}

// An absent path is unknown, not safe. Treating "no data" as "below threshold"
// would fire every rule on a boat that has just booted.
func TestEngineAbsentValueDoesNotFireThresholdRule(t *testing.T) {
	engine := newAlarmEngine()
	rule := lowVoltageRule()
	rule.DwellSeconds = 0

	events := engine.evaluate([]alarmRule{rule}, absentReader(), alarmNow)
	if len(events) != 0 {
		t.Fatalf("an absent value must not satisfy a threshold, got %+v", events)
	}
}

// Staleness is the one rule that fires on absence: under a delta stream a value
// persists until superseded, so a dead sensor otherwise looks like a steady one.
func TestEngineStaleRuleFiresOnStalePath(t *testing.T) {
	engine := newAlarmEngine()
	rule := lowVoltageRule()
	rule.Op = alarmOpStale
	rule.DwellSeconds = 0
	rule.StaleAfterSeconds = 60
	rules := []alarmRule{rule}

	if events := engine.evaluate(rules, staticReader(12.5), alarmNow); len(events) != 0 {
		t.Fatalf("a fresh path must not trip a stale rule, got %+v", events)
	}

	events := engine.evaluate(rules, staleReader(), alarmNow.Add(time.Second))
	if len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("expected a stale rule to fire, got %+v", events)
	}
}

func TestEngineEscalatesAfterConfiguredDelayWhenUnacknowledged(t *testing.T) {
	engine := newAlarmEngine()
	rule := lowVoltageRule()
	rule.DwellSeconds = 0
	rule.EscalateAfterSeconds = 120
	rules := []alarmRule{rule}

	engine.evaluate(rules, staticReader(11.0), alarmNow)

	if events := engine.evaluate(rules, staticReader(11.0), alarmNow.Add(60*time.Second)); len(events) != 0 {
		t.Fatalf("too early to escalate, got %+v", events)
	}

	events := engine.evaluate(rules, staticReader(11.0), alarmNow.Add(121*time.Second))
	if len(events) != 1 || events[0].Kind != alarmEventEscalated {
		t.Fatalf("expected an escalation, got %+v", events)
	}

	// Escalation must not repeat every tick thereafter.
	if events := engine.evaluate(rules, staticReader(11.0), alarmNow.Add(122*time.Second)); len(events) != 0 {
		t.Fatalf("escalation must fire once, got %+v", events)
	}
}

func TestEngineDoesNotEscalateOnceAcknowledged(t *testing.T) {
	engine := newAlarmEngine()
	rule := lowVoltageRule()
	rule.DwellSeconds = 0
	rule.EscalateAfterSeconds = 60
	rules := []alarmRule{rule}

	engine.evaluate(rules, staticReader(11.0), alarmNow)
	engine.acknowledge("rule-1", alarmNow.Add(time.Second))

	if events := engine.evaluate(rules, staticReader(11.0), alarmNow.Add(120*time.Second)); len(events) != 0 {
		t.Fatalf("an acknowledged alarm must not escalate, got %+v", events)
	}
}

// Worst-wins, lifted from escalateValidation's merge rule: the banner shows one
// severity for the vessel, and it has to be the most serious live one.
func TestEngineWorstActiveStateWins(t *testing.T) {
	engine := newAlarmEngine()

	warn := lowVoltageRule()
	warn.ID = "warn-rule"
	warn.State = alarmStateWarn
	warn.DwellSeconds = 0

	alarm := lowVoltageRule()
	alarm.ID = "alarm-rule"
	alarm.State = alarmStateAlarm
	alarm.DwellSeconds = 0

	engine.evaluate([]alarmRule{warn, alarm}, staticReader(11.0), alarmNow)

	if got := engine.worstActiveState(); got != alarmStateAlarm {
		t.Fatalf("worst active state: got %q, want %q", got, alarmStateAlarm)
	}
}

// The Law of Storms ladder (ADR 0095) has four rules sharing pressureChange3h
// (up-6, down-6, up-10, down-10), one on pressureChange24h and two on the
// index paths. A 12mb rise satisfies both "above" thresholds on the shared
// path and nothing else: not the "below" rules on the same path, not the
// bomb rule (a different path, absent here), and not either index rule
// (also absent, and the storm-signature one disabled besides).
func TestLawOfStormsEngine_TwelveMillibarRiseRaisesTheUpRulesOnly(t *testing.T) {
	engine := newAlarmEngine()
	rules := lawOfStormsSeedRules()

	read := fakeReader(map[string]float64{
		pressureChange3hPath: 12 * pascalsPerMillibar, // 1200 Pa, a 12mb rise
	})

	t0 := time.Date(2026, 9, 12, 6, 0, 0, 0, time.UTC)
	events := engine.evaluate(rules, read, t0)
	if len(events) != 0 {
		t.Fatalf("expected no events before the 900s dwell elapses, got %+v", events)
	}

	events = engine.evaluate(rules, read, t0.Add(16*time.Minute))
	if len(events) != 2 {
		t.Fatalf("expected exactly 2 raised events (the two 'up' rules), got %d: %+v", len(events), events)
	}
	for _, event := range events {
		if event.Kind != alarmEventRaised {
			t.Fatalf("expected a raised event, got %q", event.Kind)
		}
		if event.Rule.Op != alarmOpAbove {
			t.Fatalf("a 12mb rise must only raise 'above' rules, got %q on %q", event.Rule.Op, event.Rule.Label)
		}
	}
}

func TestEngineForgetsStatusForDeletedRule(t *testing.T) {
	engine := newAlarmEngine()
	rule := lowVoltageRule()
	rule.DwellSeconds = 0

	engine.evaluate([]alarmRule{rule}, staticReader(11.0), alarmNow)
	engine.evaluate(nil, staticReader(11.0), alarmNow.Add(time.Second))

	if len(engine.active()) != 0 {
		t.Fatalf("a deleted rule must not leave a live alarm behind")
	}
}

// Zero staleness must mean "do not gate on staleness", not "stale immediately".
// Getting this wrong makes every sample stale the instant it is read and no
// threshold rule can ever fire.
func TestEngineZeroStaleThresholdDoesNotGateThresholdRules(t *testing.T) {
	engine := newAlarmEngine()
	rule := lowVoltageRule()
	rule.DwellSeconds = 0
	rule.StaleAfterSeconds = 0

	reader := func(string) alarmSample {
		return alarmSample{Value: 11.0, Present: true, LastSeen: alarmNow.Add(-time.Hour)}
	}

	events := engine.evaluate([]alarmRule{rule}, reader, alarmNow)
	if len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("expected the rule to fire with staleness gating off, got %+v", events)
	}
}

// With staleness configured, a threshold rule must not fire on a frozen
// reading: under a delta stream a dead sensor's last value persists forever.
func TestEngineThresholdRuleIgnoresStaleValueWhenGatingConfigured(t *testing.T) {
	engine := newAlarmEngine()
	rule := lowVoltageRule()
	rule.DwellSeconds = 0
	rule.StaleAfterSeconds = 60

	reader := func(string) alarmSample {
		return alarmSample{Value: 11.0, Present: true, LastSeen: alarmNow.Add(-time.Hour)}
	}

	events := engine.evaluate([]alarmRule{rule}, reader, alarmNow)
	if len(events) != 0 {
		t.Fatalf("a frozen reading must not satisfy a threshold, got %+v", events)
	}
}

// encoding/json's omitempty does not apply to time.Time, so an unset timestamp
// would otherwise serialize as year 1 and read as "acknowledged in 1 AD".
func TestAlarmStatusOmitsUnsetTimestamps(t *testing.T) {
	engine := newAlarmEngine()
	rule := lowVoltageRule()
	rule.DwellSeconds = 0
	engine.evaluate([]alarmRule{rule}, staticReader(11.0), alarmNow)

	encoded, err := json.Marshal(engine.statusFor("rule-1"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := payload["acked_at"]; present {
		t.Fatalf("acked_at must be absent until acknowledged: %s", encoded)
	}
	if _, present := payload["raised_at"]; !present {
		t.Fatalf("raised_at must be present on an active alarm: %s", encoded)
	}
}

// alarmMessageFor now carries the live reading and the point at which the
// alarm lets go, converted into the unit an operator actually reads --
// mb/hr, degrees C, knots -- rather than raw SI, because an operator
// acknowledging an alarm has no other way to learn what "clears" it. A unit
// this engine recognises (alarm_units.go, mirroring the frontend's own
// table) is converted; an unknown or absent one falls back to a bare number
// through formatAlarmValue (rounded to two decimal places, no padded ".00").
func TestAlarmMessageForCarriesLiveValueAndClearPoint(t *testing.T) {
	cases := []struct {
		name   string
		rule   alarmRule
		sample alarmSample
		unit   string
		want   string
	}{
		{
			name:   "below with hysteresis clears above the threshold, converted to mb/hr",
			rule:   alarmRule{Label: "Barometer falling", Op: alarmOpBelow, Value: -0.03, Hysteresis: 0.01},
			sample: alarmSample{Value: -0.05, Present: true},
			unit:   "Pa/s",
			want:   "Barometer falling: -1.8 mb/hr, clears above -0.7 mb/hr",
		},
		{
			name:   "above with hysteresis clears below the threshold, unit not in the table stays a bare number",
			rule:   alarmRule{Label: "Wind strong", Op: alarmOpAbove, Value: 30, Hysteresis: 2},
			sample: alarmSample{Value: 35, Present: true},
			unit:   "kn",
			want:   "Wind strong: 35, clears below 28",
		},
		{
			name:   "zero hysteresis clears back at the threshold itself, converted to mb/hr",
			rule:   alarmRule{Label: "Barometer falling", Op: alarmOpBelow, Value: -0.03, Hysteresis: 0},
			sample: alarmSample{Value: -0.05, Present: true},
			unit:   "Pa/s",
			want:   "Barometer falling: -1.8 mb/hr, clears above -1.1 mb/hr",
		},
		{
			name:   "the House bank low fixture in its new shape, converted to V",
			rule:   lowVoltageRule(), // below 11.8, hysteresis 0.3
			sample: alarmSample{Value: 11.0, Present: true},
			unit:   "V",
			want:   "House bank low: 11.0 V, clears above 12.1 V",
		},
		{
			name:   "unknown unit is omitted with no trailing space",
			rule:   alarmRule{Label: "Barometer falling", Op: alarmOpBelow, Value: -0.03, Hysteresis: 0.01},
			sample: alarmSample{Value: -0.05, Present: true},
			unit:   "",
			want:   "Barometer falling: -0.05, clears above -0.02",
		},
		{
			name:   "equal carries the value with no clear clause",
			rule:   alarmRule{Label: "Anchor watch", Op: alarmOpEqual, Value: 1},
			sample: alarmSample{Value: 1, Present: true},
			unit:   "",
			want:   "Anchor watch: 1",
		},
		{
			name:   "notEqual carries the bare value with no clear clause, unit not in the table is dropped",
			rule:   alarmRule{Label: "Autopilot mode", Op: alarmOpNotEqual, Value: 0},
			sample: alarmSample{Value: 2, Present: true},
			unit:   "mode",
			want:   "Autopilot mode: 2",
		},
		{
			name:   "stale is unchanged by any of this",
			rule:   alarmRule{Label: "Bilge sensor", Op: alarmOpStale, StaleAfterSeconds: 90},
			sample: alarmSample{},
			unit:   "V",
			want:   "Bilge sensor: no data for 90s",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := alarmMessageFor(tc.rule, tc.sample, tc.unit, ""); got != tc.want {
				t.Fatalf("alarmMessageFor: got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAlarmMessageForAppendsEvidence asserts a non-empty evidence string is
// appended as its own clause, and that an empty one (every non-anomaly
// alarm) leaves the message exactly as before.
func TestAlarmMessageForAppendsEvidence(t *testing.T) {
	rule := alarmRule{Label: "Charging into a full house bank", Op: alarmOpAbove, Value: 0.5, Hysteresis: 0}
	sample := alarmSample{Value: 1, Present: true}

	withEvidence := alarmMessageFor(rule, sample, "", "House bank 96% SoC, 28.90 V, charging 42 A")
	want := "Charging into a full house bank: 1, clears below 0.5 House bank 96% SoC, 28.90 V, charging 42 A"
	if withEvidence != want {
		t.Fatalf("alarmMessageFor with evidence:\n got  %q\n want %q", withEvidence, want)
	}

	noEvidence := alarmMessageFor(rule, sample, "", "")
	wantNoEvidence := "Charging into a full house bank: 1, clears below 0.5"
	if noEvidence != wantNoEvidence {
		t.Fatalf("alarmMessageFor with no evidence:\n got  %q\n want %q", noEvidence, wantNoEvidence)
	}
}

// The rule fields (op, threshold, hysteresis, clear_value, unit) let the
// frontend render "clears above/below Y" without duplicating the engine's own
// arithmetic. They come from the rule the engine already holds, refreshed
// every tick alongside Label and Path, so they can never drift from it.
func TestAlarmStatusJSONCarriesRuleFieldsForBelowRule(t *testing.T) {
	engine := newAlarmEngine()
	engine.unitFor = func(path string) string {
		if path == pressureRatePath {
			return "Pa/s"
		}
		return ""
	}

	rule := alarmRule{
		ID: "rule-1", Enabled: true, Path: pressureRatePath, Label: "Barometer falling",
		Op: alarmOpBelow, Value: -0.03, Hysteresis: 0.01, State: alarmStateWarn,
	}
	engine.evaluate([]alarmRule{rule}, staticReader(-0.05), alarmNow)

	payload := marshalStatusToMap(t, engine.statusFor("rule-1"))

	if got := payload["op"]; got != "below" {
		t.Fatalf("op: got %v, want %q", got, "below")
	}
	if got := payload["unit"]; got != "Pa/s" {
		t.Fatalf("unit: got %v, want %q", got, "Pa/s")
	}
	assertJSONNumberNear(t, payload, "threshold", -0.03)
	assertJSONNumberNear(t, payload, "hysteresis", 0.01)
	assertJSONNumberNear(t, payload, "clear_value", -0.02)
}

// above rule: clear_value is threshold minus hysteresis, the mirror image of
// below's threshold plus hysteresis.
func TestAlarmStatusJSONClearValueForAboveRuleIsThresholdMinusHysteresis(t *testing.T) {
	engine := newAlarmEngine()
	rule := alarmRule{
		ID: "rule-1", Enabled: true, Path: "environment.wind.speedApparent", Label: "Wind strong",
		Op: alarmOpAbove, Value: 0.05, Hysteresis: 0.02, State: alarmStateWarn,
	}
	engine.evaluate([]alarmRule{rule}, staticReader(0.2), alarmNow)

	payload := marshalStatusToMap(t, engine.statusFor("rule-1"))
	assertJSONNumberNear(t, payload, "clear_value", 0.03)
}

// equal, notEqual and stale have no single clearing value worth surfacing --
// equal clears on any change, notEqual on hitting one exact number, stale on
// data resuming -- none of which reads as "clears above/below N". Stale also
// has no threshold or hysteresis: StaleAfterSeconds is the only number that
// governs it, and it is already in the message.
func TestAlarmStatusJSONOmitsClearValueForEqualNotEqualAndStale(t *testing.T) {
	cases := []struct {
		name              string
		op                string
		wantThreshold     bool
		wantHysteresis    bool
		staleAfterSeconds int
	}{
		{name: "equal", op: alarmOpEqual, wantThreshold: true, wantHysteresis: true},
		{name: "notEqual", op: alarmOpNotEqual, wantThreshold: true, wantHysteresis: true},
		{name: "stale", op: alarmOpStale, wantThreshold: false, wantHysteresis: false, staleAfterSeconds: 60},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := newAlarmEngine()
			rule := alarmRule{
				ID: "rule-1", Enabled: true, Path: "electrical.batteries.house.voltage", Label: "Test",
				Op: tc.op, Value: 1, StaleAfterSeconds: tc.staleAfterSeconds, State: alarmStateWarn,
			}
			reader := staticReader(1)
			if tc.op == alarmOpStale {
				reader = staleReader()
			}
			engine.evaluate([]alarmRule{rule}, reader, alarmNow)

			payload := marshalStatusToMap(t, engine.statusFor("rule-1"))
			if _, present := payload["clear_value"]; present {
				t.Fatalf("%s: clear_value must be absent, got %v", tc.name, payload["clear_value"])
			}
			if _, present := payload["threshold"]; present != tc.wantThreshold {
				t.Fatalf("%s: threshold present=%v, want %v", tc.name, present, tc.wantThreshold)
			}
			if _, present := payload["hysteresis"]; present != tc.wantHysteresis {
				t.Fatalf("%s: hysteresis present=%v, want %v", tc.name, present, tc.wantHysteresis)
			}
			if got := payload["op"]; got != tc.op {
				t.Fatalf("op: got %v, want %q", got, tc.op)
			}
		})
	}
}

// clear_value must agree with alarmConditionCleared's own boundary, not a
// re-derivation of it: a sample exactly at clear_value clears, one short of it
// does not.
func TestAlarmClearValueAgreesWithConditionClearedBoundaryForBelow(t *testing.T) {
	rule := alarmRule{Op: alarmOpBelow, Value: 11.8, Hysteresis: 0.3}
	clear := alarmClearValueFor(rule)
	if clear == nil {
		t.Fatalf("expected a clear value for a below rule")
	}

	if !alarmConditionCleared(rule, alarmSample{Value: *clear, Present: true}, false) {
		t.Fatalf("a below rule must clear exactly at its clear value %v", *clear)
	}
	if alarmConditionCleared(rule, alarmSample{Value: *clear - 0.001, Present: true}, false) {
		t.Fatalf("a below rule must not clear just short of its clear value")
	}
}

func TestAlarmClearValueAgreesWithConditionClearedBoundaryForAbove(t *testing.T) {
	rule := alarmRule{Op: alarmOpAbove, Value: 30.0, Hysteresis: 2.0}
	clear := alarmClearValueFor(rule)
	if clear == nil {
		t.Fatalf("expected a clear value for an above rule")
	}

	if !alarmConditionCleared(rule, alarmSample{Value: *clear, Present: true}, false) {
		t.Fatalf("an above rule must clear exactly at its clear value %v", *clear)
	}
	if alarmConditionCleared(rule, alarmSample{Value: *clear + 0.001, Present: true}, false) {
		t.Fatalf("an above rule must not clear just short of its clear value")
	}
}

// marshalStatusToMap round-trips a status through its real MarshalJSON, the
// same path the API and the SSE stream use, so "present" and "absent" in
// these tests mean exactly what the frontend will see.
func marshalStatusToMap(t *testing.T, status alarmStatus) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return payload
}

func assertJSONNumberNear(t *testing.T, payload map[string]any, key string, want float64) {
	t.Helper()
	raw, present := payload[key]
	if !present {
		t.Fatalf("%s: expected present, was absent", key)
	}
	got, ok := raw.(float64)
	if !ok {
		t.Fatalf("%s: not a number: %v", key, raw)
	}
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s: got %v, want %v", key, got, want)
	}
}

// --- Evidence: frozen at raise from evidenceFor(path) -----------------------

// withAnomalySlot sets globalAnomalySlot to a single-path reading and
// restores whatever was there afterward, the "tests build their own,
// production has no thread to pass one through" split every slot in this
// codebase documents.
func withAnomalySlot(t *testing.T, path, evidence string, computedAt time.Time) {
	t.Helper()
	prev := globalAnomalySlot
	globalAnomalySlot = &anomalySlot{}
	globalAnomalySlot.set(anomalyReading{
		Evidence:   map[string]string{path: evidence},
		ComputedAt: computedAt,
	})
	t.Cleanup(func() { globalAnomalySlot = prev })
}

func TestEvidenceForReadsTheAnomalySlot(t *testing.T) {
	withAnomalySlot(t, anomalyBatteryFullBankChargingPath, "House bank 96% SoC, 28.90 V, charging 42 A", alarmNow)
	if got := evidenceFor(anomalyBatteryFullBankChargingPath, alarmNow); got != "House bank 96% SoC, 28.90 V, charging 42 A" {
		t.Fatalf("evidenceFor: got %q", got)
	}
}

func TestEvidenceForEmptyForANonAnomalyPath(t *testing.T) {
	withAnomalySlot(t, anomalyBatteryFullBankChargingPath, "House bank 96% SoC, 28.90 V, charging 42 A", alarmNow)
	if got := evidenceFor("electrical.batteries.house.voltage", alarmNow); got != "" {
		t.Fatalf("evidenceFor for a path the anomaly detector does not own: got %q, want empty", got)
	}
}

func TestEvidenceForEmptyWhenSlotIsStale(t *testing.T) {
	withAnomalySlot(t, anomalyBatteryFullBankChargingPath, "House bank 96% SoC, 28.90 V, charging 42 A", alarmNow.Add(-10*time.Second))
	if got := evidenceFor(anomalyBatteryFullBankChargingPath, alarmNow); got != "" {
		t.Fatalf("evidenceFor with a stale slot: got %q, want empty", got)
	}
}

func TestEvidenceForEmptyWithNoSlotAtAll(t *testing.T) {
	prev := globalAnomalySlot
	globalAnomalySlot = &anomalySlot{}
	t.Cleanup(func() { globalAnomalySlot = prev })
	if got := evidenceFor(anomalyBatteryFullBankChargingPath, alarmNow); got != "" {
		t.Fatalf("evidenceFor before any tick has ever landed: got %q, want empty", got)
	}
}

// TestEngineFreezesEvidenceAtRaise asserts an anomaly-backed rule's Evidence
// is captured once, at the moment it raises, and does not keep following
// the slot afterward -- unlike Encounter, which is deliberately live.
func TestEngineFreezesEvidenceAtRaise(t *testing.T) {
	rule := alarmRule{ID: "anomaly-battery", Label: "Charging into a full house bank", Enabled: true,
		Path: anomalyBatteryFullBankChargingPath, Op: alarmOpAbove, Value: 0.5, State: alarmStateWarn}

	withAnomalySlot(t, anomalyBatteryFullBankChargingPath, "House bank 96% SoC, 28.90 V, charging 42 A", alarmNow)

	engine := newAlarmEngine()
	events := engine.evaluate([]alarmRule{rule}, staticReader(1), alarmNow)
	if len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("expected one raised event, got %+v", events)
	}
	if got := engine.statusFor("anomaly-battery").Evidence; got != "House bank 96% SoC, 28.90 V, charging 42 A" {
		t.Fatalf("Evidence at raise: got %q", got)
	}
	if !strings.Contains(engine.statusFor("anomaly-battery").Message, "House bank 96% SoC") {
		t.Fatalf("expected the raised message to carry the evidence clause, got %q", engine.statusFor("anomaly-battery").Message)
	}

	// The slot moves on; the already-raised status must not follow it.
	globalAnomalySlot.set(anomalyReading{
		Evidence:   map[string]string{anomalyBatteryFullBankChargingPath: "a completely different reading"},
		ComputedAt: alarmNow.Add(1 * time.Second),
	})
	engine.evaluate([]alarmRule{rule}, staticReader(1), alarmNow.Add(2*time.Second))
	if got := engine.statusFor("anomaly-battery").Evidence; got != "House bank 96% SoC, 28.90 V, charging 42 A" {
		t.Fatalf("Evidence must stay frozen while the alarm is active: got %q", got)
	}
}

// TestEngineClearsEvidenceWhenTheAlarmClears asserts Evidence is reset once
// the alarm returns to normal, so a later, unrelated raise on the same rule
// never inherits stale evidence from a previous occurrence.
func TestEngineClearsEvidenceWhenTheAlarmClears(t *testing.T) {
	rule := alarmRule{ID: "anomaly-battery", Label: "Charging into a full house bank", Enabled: true,
		Path: anomalyBatteryFullBankChargingPath, Op: alarmOpAbove, Value: 0.5, State: alarmStateWarn}

	withAnomalySlot(t, anomalyBatteryFullBankChargingPath, "House bank 96% SoC, 28.90 V, charging 42 A", alarmNow)

	engine := newAlarmEngine()
	engine.evaluate([]alarmRule{rule}, staticReader(1), alarmNow)
	if engine.statusFor("anomaly-battery").Evidence == "" {
		t.Fatalf("expected evidence to be set once raised")
	}

	engine.evaluate([]alarmRule{rule}, staticReader(0), alarmNow.Add(1*time.Second))
	if got := engine.statusFor("anomaly-battery").Evidence; got != "" {
		t.Fatalf("expected evidence cleared once the alarm returns to normal, got %q", got)
	}
}
