package main

import (
	"encoding/json"
	"fmt"
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

type alarmReader func(path string) alarmSample

type alarmStatus struct {
	RuleID  string     `json:"rule_id"`
	Label   string     `json:"label"`
	Path    string     `json:"path"`
	Phase   alarmPhase `json:"phase"`
	State   string     `json:"state"`
	Value   float64    `json:"value"`
	Message string     `json:"message"`

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
}

type alarmEngine struct {
	mu       sync.RWMutex
	statuses map[string]*alarmStatus
}

func newAlarmEngine() *alarmEngine {
	return &alarmEngine{statuses: map[string]*alarmStatus{}}
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
	status.Message = alarmMessageFor(rule, sample)
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

func alarmMessageFor(rule alarmRule, sample alarmSample) string {
	if rule.Op == alarmOpStale {
		return fmt.Sprintf("%s: no data for %ds", rule.Label, rule.StaleAfterSeconds)
	}
	return fmt.Sprintf("%s: %s %g (%.4g)", rule.Label, rule.Op, rule.Value, sample.Value)
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
