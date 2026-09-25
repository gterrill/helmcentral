package main

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"
)

type alarmPhase string

const (
	alarmPhaseNormal alarmPhase = "normal"
	// alarmPhasePending means the condition holds but has not held long enough
	// to satisfy the rule's dwell.
	alarmPhasePending      alarmPhase = "pending"
	alarmPhaseActive       alarmPhase = "active"
	alarmPhaseAcknowledged alarmPhase = "acknowledged"
)

const (
	alarmEventRaised    = "raised"
	alarmEventCleared   = "cleared"
	alarmEventEscalated = "escalated"
)

// alarmSample is one reading of a path. Present distinguishes "no such path" —
// which must never satisfy a threshold — from a real zero. LastSeen is when the
// path last carried an update; the reader reports it as a fact and the engine
// decides what counts as stale, because the threshold is per-rule.
type alarmSample struct {
	Value    float64
	Present  bool
	LastSeen time.Time
}

// alarmSampleStale applies a rule's staleness threshold.
//
// Zero means the rule does not gate on staleness at all, not "stale after zero
// seconds" — otherwise every sample would be stale the instant it was read, and
// no threshold rule could ever fire. validateAlarmRule guarantees a staleness
// rule has a non-zero threshold; threshold rules opt in explicitly.
//
// A path that has never reported counts as stale: for a staleness rule that is
// the same fault as one that stopped, and it surfaces a mistyped path rather
// than hiding it.
func alarmSampleStale(rule alarmRule, sample alarmSample, now time.Time) bool {
	if rule.StaleAfterSeconds <= 0 {
		return false
	}
	if !sample.Present || sample.LastSeen.IsZero() {
		return true
	}
	return now.Sub(sample.LastSeen) > time.Duration(rule.StaleAfterSeconds)*time.Second
}

// alarmSampleAge reports how long before now a sample was last seen, in
// seconds, rounded to one decimal place. -1 when it was never seen: an
// unknown age must never be presented as freshly measured.
//
// Ages riding the gauge-values stream (ADR 0083) are computed from this same
// LastSeen fact alarmSampleStale above already uses, so the stream and the
// alarm engine can never disagree about what counts as frozen.
func alarmSampleAge(sample alarmSample, now time.Time) float64 {
	if sample.LastSeen.IsZero() {
		return -1
	}
	age := now.Sub(sample.LastSeen).Seconds()
	if age < 0 {
		// A concurrent update landed between the payload's now and this
		// sample's LastSeen; it is current, not negative.
		return 0
	}
	return roundTo1(age)
}

type alarmReader func(path string) alarmSample

type alarmStatus struct {
	RuleID  string     `json:"rule_id"`
	Label   string     `json:"label"`
	Path    string     `json:"path"`
	Phase   alarmPhase `json:"phase"`
	State   string     `json:"state"`
	Value   float64    `json:"value"`
	Message string     `json:"message"`

	// Op, Threshold, Hysteresis and ClearValue exist so the frontend can render
	// "clears above/below Y" without re-deriving the engine's own arithmetic.
	// They describe a rule this engine holds, so they are set only for
	// rule-driven alarms, never for a bus notification raised by something
	// else on the network. Threshold and Hysteresis are pointers, and
	// ClearValue is always a pointer, because 0 is a legitimate threshold and
	// omitempty on a bare float64 would swallow it along with the genuinely
	// absent case.
	Op         string   `json:"op,omitempty"`
	Threshold  *float64 `json:"threshold,omitempty"`
	Hysteresis *float64 `json:"hysteresis,omitempty"`
	// ClearValue is the value at which a live alarm lets go, derived from
	// alarmConditionCleared's own boundary (see alarmClearValueFor) so the two
	// can never disagree. nil for equal, notEqual and stale, none of which
	// clears at a single number the way above/below do.
	ClearValue *float64 `json:"clear_value,omitempty"`

	// Unit is the SI unit for the alarm's path, when known -- set for rule
	// alarms and for bus notifications alike, since an operator reading
	// "-0.03" needs to know it is pascals per second whichever raised it.
	Unit string `json:"unit,omitempty"`

	// Encounter is the COLREGS "situation + role" line for a collision
	// alarm -- collision-only, set by signalKCollisionNotifications and left
	// empty for every other alarm source (ADR 0098). It is computed live on
	// every read rather than frozen at raise: unlike ADR 0090 §4's evidence
	// figures, this line exists to guide what happens next, so it should
	// keep following the target if she alters. That is safe because the bus
	// watcher's raise/clear keys on RuleID alone (alarm_bus_watch.go), so a
	// changing Encounter on an already-raised alarm never produces a second
	// raise event.
	Encounter string `json:"encounter,omitempty"`

	// Evidence is the "why" sentence behind an anomaly-detection alarm
	// (anomaly_detector.go's anomalyReading.Evidence, e.g. "House bank 96%
	// SoC, 28.90 V, charging 42 A"), and empty for every other alarm source.
	// Unlike Encounter above, it is frozen at raise (evidenceFor(rule.Path),
	// captured once in advanceAlarmRule) rather than recomputed on every
	// read: an anomaly reading is a point-in-time snapshot of what tripped
	// the alarm, not a running commentary that should keep changing while
	// the alarm stays up, and the bus watcher's raise/clear still keys on
	// RuleID alone, so freezing it here never affects that.
	Evidence string `json:"evidence,omitempty"`

	// LiveEvidence is set only for the three sensor-health count alarms
	// (frozen/impossible/silent-source), filled in by activeAlarms
	// (withLiveSensorEvidence) rather than by the engine itself. Unlike
	// Evidence above, it re-reads the anomaly detector's CURRENT tick on
	// every list, the same "computed live" contract Encounter documents --
	// because the alarm card's "Ignore this sensor" action needs to offer
	// whatever is actually failing right now, not whichever sensors were
	// behind the count the instant it first crossed the threshold. A count
	// alarm can stay continuously active for a long time while its specific
	// offenders drift (one clears, another starts failing) without the
	// count itself ever dropping enough to clear and re-raise, so Evidence
	// alone would leave the action offering a sensor that already recovered
	// while never offering the one that is actually failing (code review
	// finding 9). Evidence itself keeps meaning "why this first fired" --
	// this field exists so the ignore action does not have to overload that
	// meaning into "what is failing at this exact moment" too.
	LiveEvidence string `json:"live_evidence,omitempty"`

	// Silenced and the two capability flags mirror the SignalK Notifications
	// API's own status object. Silencing is not acknowledging — a silenced
	// alarm has stopped sounding but is still demanding attention — so it is a
	// flag alongside Phase rather than another phase. The capabilities are the
	// server's answer about a given notification, not something inferred.
	Silenced       bool `json:"silenced"`
	CanSilence     bool `json:"can_silence"`
	CanAcknowledge bool `json:"can_acknowledge"`

	SinceTrue time.Time `json:"-"`
	RaisedAt  time.Time `json:"-"`
	AckedAt   time.Time `json:"-"`

	escalated bool
}

// MarshalJSON omits the timestamps when unset. encoding/json's omitempty does
// not apply to time.Time, so without this an un-acknowledged alarm reports
// "acked_at":"0001-01-01T00:00:00Z", which reads as acknowledged in 1 AD.
func (s alarmStatus) MarshalJSON() ([]byte, error) {
	type wire alarmStatus
	payload := map[string]any{}

	encoded, err := json.Marshal(wire(s))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return nil, err
	}

	if !s.RaisedAt.IsZero() {
		payload["raised_at"] = s.RaisedAt.UTC()
	}
	if !s.AckedAt.IsZero() {
		payload["acked_at"] = s.AckedAt.UTC()
	}
	return json.Marshal(payload)
}

type alarmEvent struct {
	Kind   string
	Rule   alarmRule
	Status alarmStatus

	// Source distinguishes a bus notification (alarmSourceSignalK) from a
	// locally evaluated rule. Empty means rule-sourced, so every existing
	// caller that builds an alarmEvent without setting it keeps working
	// unchanged.
	Source string
}

type alarmEngine struct {
	mu       sync.RWMutex
	statuses map[string]*alarmStatus

	// unitFor resolves a rule's path to its SI unit, for status.Unit and for
	// the raised message. nil in a bare newAlarmEngine() (every unit-testing
	// caller in this package drives the engine with alarmReader funcs and no
	// snapshot at all, so there is nothing to resolve against); production
	// wires it in evaluateAlarmsOnce, re-read every tick for the same reason
	// globalBusNotificationWatcher.snapshot is: tests substitute
	// globalSignalKSnapshot per case, and this must never resolve against a
	// snapshot from a previous one.
	unitFor func(path string) string
}

func newAlarmEngine() *alarmEngine {
	return &alarmEngine{statuses: map[string]*alarmStatus{}}
}

// unitForPath is nil-safe: a bare engine (every test in this package but the
// ones exercising the unit fields themselves) has no lookup wired at all, and
// "no unit known" is the correct answer for that, not a panic.
func (e *alarmEngine) unitForPath(path string) string {
	if e.unitFor == nil {
		return ""
	}
	return e.unitFor(path)
}

// evaluate advances every rule's state machine and returns the transitions that
// happened. Only transitions are returned: a still-active alarm produces
// nothing, so notifiers never have to de-duplicate.
func (e *alarmEngine) evaluate(rules []alarmRule, read alarmReader, now time.Time) []alarmEvent {
	e.mu.Lock()
	defer e.mu.Unlock()

	var events []alarmEvent
	seen := make(map[string]bool, len(rules))

	for _, rule := range rules {
		if !rule.Enabled {
			// A disabled rule keeps no state, so re-enabling starts clean
			// rather than resuming a stale dwell.
			delete(e.statuses, rule.ID)
			continue
		}
		seen[rule.ID] = true

		status, ok := e.statuses[rule.ID]
		if !ok {
			status = &alarmStatus{RuleID: rule.ID, Phase: alarmPhaseNormal, State: alarmStateNormal}
			e.statuses[rule.ID] = status
		}
		status.Label = rule.Label
		status.Path = rule.Path
		status.Op = rule.Op
		status.Unit = e.unitForPath(rule.Path)
		if rule.Op == alarmOpStale {
			// StaleAfterSeconds is the only number that governs a stale rule,
			// and it is already in the message; a threshold and hysteresis
			// left over from a previous op would be pure confusion.
			status.Threshold = nil
			status.Hysteresis = nil
		} else {
			threshold := rule.Value
			hysteresis := rule.Hysteresis
			status.Threshold = &threshold
			status.Hysteresis = &hysteresis
		}
		status.ClearValue = alarmClearValueFor(rule)

		sample := read(rule.Path)
		status.Value = sample.Value

		if event, emitted := advanceAlarmRule(rule, status, sample, now); emitted {
			events = append(events, event)
		}
	}

	// A rule that was deleted or disabled must not leave a live alarm behind.
	for id := range e.statuses {
		if !seen[id] {
			delete(e.statuses, id)
		}
	}

	return events
}

// advanceAlarmRule is the per-rule state machine:
//
//	normal → pending (dwell) → active → acknowledged
//	                             ↓          ↓
//	                           normal ← (hysteresis clears)
func advanceAlarmRule(rule alarmRule, status *alarmStatus, sample alarmSample, now time.Time) (alarmEvent, bool) {
	stale := alarmSampleStale(rule, sample, now)
	live := status.Phase == alarmPhaseActive || status.Phase == alarmPhaseAcknowledged

	if live {
		// Clearing uses the deadband, not the raise threshold, so a value
		// hovering at the boundary cannot flap the alarm.
		if alarmConditionCleared(rule, sample, stale) {
			status.Phase = alarmPhaseNormal
			status.State = alarmStateNormal
			status.SinceTrue = time.Time{}
			status.RaisedAt = time.Time{}
			status.AckedAt = time.Time{}
			status.escalated = false
			status.Message = fmt.Sprintf("%s cleared", rule.Label)
			status.Evidence = ""
			return alarmEvent{Kind: alarmEventCleared, Rule: rule, Status: *status}, true
		}

		if shouldEscalateAlarm(rule, status, now) {
			status.escalated = true
			status.Message = fmt.Sprintf("%s still unacknowledged", rule.Label)
			return alarmEvent{Kind: alarmEventEscalated, Rule: rule, Status: *status}, true
		}

		return alarmEvent{}, false
	}

	if !alarmConditionMet(rule, sample, stale) {
		// Recovery before the dwell elapsed restarts the timer; a condition
		// that keeps flickering never accumulates enough time to fire.
		status.Phase = alarmPhaseNormal
		status.SinceTrue = time.Time{}
		return alarmEvent{}, false
	}

	if status.SinceTrue.IsZero() {
		status.SinceTrue = now
	}
	status.Phase = alarmPhasePending

	if now.Sub(status.SinceTrue) < time.Duration(rule.DwellSeconds)*time.Second {
		return alarmEvent{}, false
	}

	status.Phase = alarmPhaseActive
	status.State = rule.State
	status.RaisedAt = now
	status.AckedAt = time.Time{}
	status.escalated = false
	status.Evidence = evidenceFor(rule.Path, now)
	status.Message = alarmMessageFor(rule, sample, status.Unit, status.Evidence)
	return alarmEvent{Kind: alarmEventRaised, Rule: rule, Status: *status}, true
}

func shouldEscalateAlarm(rule alarmRule, status *alarmStatus, now time.Time) bool {
	if rule.EscalateAfterSeconds <= 0 || status.escalated {
		return false
	}
	// Acknowledging is the operator saying they have seen it; escalating past
	// that would be nagging, not safety.
	if status.Phase != alarmPhaseActive {
		return false
	}
	return now.Sub(status.RaisedAt) >= time.Duration(rule.EscalateAfterSeconds)*time.Second
}

// alarmConditionMet reports whether the raise condition holds.
func alarmConditionMet(rule alarmRule, sample alarmSample, stale bool) bool {
	if rule.Op == alarmOpStale {
		return stale
	}
	// An absent path is unknown, not safe: treating it as zero would fire every
	// threshold rule on a boat that has just booted. A stale value is equally
	// untrustworthy — under a delta stream it persists until superseded.
	if !sample.Present || stale {
		return false
	}

	switch rule.Op {
	case alarmOpAbove:
		return sample.Value > rule.Value
	case alarmOpBelow:
		return sample.Value < rule.Value
	case alarmOpEqual:
		return sample.Value == rule.Value
	case alarmOpNotEqual:
		return sample.Value != rule.Value
	}
	return false
}

// alarmConditionCleared applies the hysteresis deadband. It is deliberately not
// the negation of alarmConditionMet: the value has to travel back past the
// threshold by Hysteresis before the alarm lets go.
func alarmConditionCleared(rule alarmRule, sample alarmSample, stale bool) bool {
	if rule.Op == alarmOpStale {
		return !stale
	}
	// The path vanished or went quiet while the alarm was live. Clearing on
	// that would silence an alarm because its sensor died, so hold it.
	if !sample.Present || stale {
		return false
	}

	switch rule.Op {
	case alarmOpAbove:
		return sample.Value <= rule.Value-rule.Hysteresis
	case alarmOpBelow:
		return sample.Value >= rule.Value+rule.Hysteresis
	case alarmOpEqual:
		return sample.Value != rule.Value
	case alarmOpNotEqual:
		return sample.Value == rule.Value
	}
	return true
}

// alarmClearValueFor is the value at which a live alarm will let go, derived
// from alarmConditionCleared's own boundary so the two can never disagree:
// below clears once the value rises back to threshold+hysteresis, above once
// it falls back to threshold-hysteresis. equal, notEqual and stale have no
// single number worth surfacing this way -- equal clears on any change,
// notEqual on hitting one exact value, stale on data resuming -- so they
// report nil rather than a value that would misdescribe how they clear.
func alarmClearValueFor(rule alarmRule) *float64 {
	switch rule.Op {
	case alarmOpBelow:
		v := rule.Value + rule.Hysteresis
		return &v
	case alarmOpAbove:
		v := rule.Value - rule.Hysteresis
		return &v
	default:
		return nil
	}
}

// alarmMessageFor builds the one-line summary an operator reads on the alarm
// banner. Unit is passed in rather than looked up here so this stays testable
// with a bare rule and sample -- no snapshot required -- while the caller
// (the engine, which does have a unit lookup wired) decides what "known" means.
// alarmMessageFor builds the plain-text sentence every non-browser
// notification (ntfy, email, the SignalK bus) actually carries -- the
// frontend re-derives its own richer sentence from the structured fields
// instead (alarm-display.ts's alarmConditionSentence). evidence, when
// non-empty, is appended as its own clause so those transports carry the
// same "why" an anomaly-detection alarm's card shows, e.g. "House bank
// overcharge risk: 2, clears below 1.5. House bank 96% SoC, 28.90 V,
// charging 42 A."
func alarmMessageFor(rule alarmRule, sample alarmSample, unit, evidence string) string {
	message := alarmConditionMessageFor(rule, sample, unit)
	if evidence == "" {
		return message
	}
	return message + " " + evidence
}

// alarmConditionMessageFor is alarmMessageFor's own arithmetic, split out so
// a caller (or a test) that only cares about the condition sentence itself
// does not have to pass -- and strip back off -- an evidence clause.
func alarmConditionMessageFor(rule alarmRule, sample alarmSample, unit string) string {
	if rule.Op == alarmOpStale {
		return fmt.Sprintf("%s: no data for %ds", rule.Label, rule.StaleAfterSeconds)
	}

	valueText := formatAlarmReading(sample.Value, unit)

	switch rule.Op {
	case alarmOpBelow, alarmOpAbove:
		// A below rule clears by rising, so what the operator needs to see it
		// travel back UP past is worded "clears above"; above is the mirror.
		direction := "above"
		if rule.Op == alarmOpAbove {
			direction = "below"
		}
		clearText := formatAlarmReading(*alarmClearValueFor(rule), unit)
		return fmt.Sprintf("%s: %s, clears %s %s", rule.Label, valueText, direction, clearText)
	default: // equal, notEqual
		// Neither clears at a single crossing point, so there is no clear
		// clause to add -- just the label and the value that tripped it.
		return fmt.Sprintf("%s: %s", rule.Label, valueText)
	}
}

// evidenceFor is the "why" sentence behind path's current anomaly-detection
// reading, or "" for a path the anomaly detector does not own (every
// non-anomaly rule) or when the slot is stale (anomalySlotMaxAge) --
// exactly the same staleness rule addAnomalyValues applies to the derived
// path values themselves, so an alarm can never carry evidence describing a
// reading the derived path itself is simultaneously reporting absent. now
// is the caller's own tick time (advanceAlarmRule already has one), not
// time.Now(), so this stays testable against a fixed clock like every other
// staleness check in this file.
func evidenceFor(path string, now time.Time) string {
	reading, ok := globalAnomalySlot.get()
	if !ok {
		return ""
	}
	if now.Sub(reading.ComputedAt) > anomalySlotMaxAge {
		return ""
	}
	return reading.Evidence[path]
}

// sensorHealthCountPaths are the three anomaly detector paths the alarm
// card's "Ignore this sensor" action targets (anomaly_sensor_health.go;
// alarm-display.ts's IGNORABLE_SENSOR_PATHS mirrors this list client-side).
var sensorHealthCountPaths = map[string]bool{
	anomalySensorFrozenCountPath:       true,
	anomalySensorOutOfRangeCountPath:   true,
	anomalySensorSilentSourceCountPath: true,
}

// withLiveSensorEvidence fills LiveEvidence for any status whose Path is
// one of the three sensor-health count alarms, from the SAME evidenceFor
// lookup Evidence itself used at raise -- but called fresh here, every
// time activeAlarms builds its list, rather than once. See LiveEvidence's
// own doc comment for why Evidence can't serve this on its own (code
// review finding 9). Every other status (a different rule-driven alarm, or
// a bus/collision notification, neither of which has a Path matching one
// of these three) passes through unchanged.
func withLiveSensorEvidence(statuses []alarmStatus, now time.Time) []alarmStatus {
	for i := range statuses {
		if !sensorHealthCountPaths[statuses[i].Path] {
			continue
		}
		statuses[i].LiveEvidence = evidenceFor(statuses[i].Path, now)
	}
	return statuses
}

// formatAlarmValue renders a value the way an operator wants to read it, not
// the way a converted unit happens to come out of a float64. A threshold
// carried over from millibars or knots is routinely a repeating decimal, and
// nobody needs 17 digits of that on an alarm banner. Round to two decimal
// places and drop trailing zeros, so 11.0 reads as "11" and not "11.00".
func formatAlarmValue(v float64) string {
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}

// acknowledge silences a live alarm without resolving it. It reports false when
// there is nothing to acknowledge, or when the alarm is an emergency — SignalK
// is explicit that emergencies cannot be silenced whatever canSilence says.
func (e *alarmEngine) acknowledge(ruleID string, now time.Time) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	status, ok := e.statuses[ruleID]
	if !ok || status.Phase != alarmPhaseActive {
		return false
	}
	if status.State == alarmStateEmergency {
		return false
	}

	status.Phase = alarmPhaseAcknowledged
	status.AckedAt = now
	return true
}

func (e *alarmEngine) statusFor(ruleID string) alarmStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if status, ok := e.statuses[ruleID]; ok {
		return status.withRuleCapabilities()
	}
	return alarmStatus{RuleID: ruleID, Phase: alarmPhaseNormal, State: alarmStateNormal}
}

// withRuleCapabilities reports what a rule-driven alarm supports. The engine
// has exactly one action — acknowledge — and no notion of silencing separate
// from it, so unlike a bus notification it never offers CanSilence. An
// emergency offers nothing (ADR 0038).
func (s alarmStatus) withRuleCapabilities() alarmStatus {
	s.CanAcknowledge = s.Phase == alarmPhaseActive && s.State != alarmStateEmergency
	s.CanSilence = false
	return s
}

// active returns every alarm currently raised, acknowledged included — an
// acknowledged alarm is still a live condition.
func (e *alarmEngine) active() []alarmStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var out []alarmStatus
	for _, status := range e.statuses {
		if status.Phase == alarmPhaseActive || status.Phase == alarmPhaseAcknowledged {
			out = append(out, status.withRuleCapabilities())
		}
	}
	return out
}

// worstActiveState merges live severities worst-wins, mirroring
// escalateValidation's rule in gnss_validation.go. The UI shows one severity
// for the vessel and it must be the most serious live one.
func (e *alarmEngine) worstActiveState() string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	worst := alarmStateNormal
	for _, status := range e.statuses {
		if status.Phase != alarmPhaseActive && status.Phase != alarmPhaseAcknowledged {
			continue
		}
		if alarmStateRank[status.State] > alarmStateRank[worst] {
			worst = status.State
		}
	}
	return worst
}
