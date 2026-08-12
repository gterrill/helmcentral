package main

import (
	"encoding/json"
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
