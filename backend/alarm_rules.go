package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Alarm severities are SignalK's notification states verbatim (ADR 0038), so
// rules Helmcentral raises and notifications other SignalK producers raise are
// the same vocabulary rather than two parallel ones.
const (
	alarmStateNormal    = "normal"
	alarmStateAlert     = "alert"
	alarmStateWarn      = "warn"
	alarmStateAlarm     = "alarm"
	alarmStateEmergency = "emergency"
)

// alarmStateRank orders severities for worst-wins merging when several rules
// cover one path. normal is the floor and is never raised by a rule.
var alarmStateRank = map[string]int{
	alarmStateNormal:    0,
	alarmStateAlert:     1,
	alarmStateWarn:      2,
	alarmStateAlarm:     3,
	alarmStateEmergency: 4,
}

const (
	alarmOpAbove    = "above"
	alarmOpBelow    = "below"
	alarmOpEqual    = "equal"
	alarmOpNotEqual = "notEqual"
	// alarmOpStale fires on absence rather than on a value: the path stopped
	// reporting. Under a delta stream a value persists until superseded, so a
	// dead sensor is otherwise indistinguishable from a steady one.
	alarmOpStale = "stale"
)

var alarmOperators = map[string]bool{
	alarmOpAbove:    true,
	alarmOpBelow:    true,
	alarmOpEqual:    true,
	alarmOpNotEqual: true,
	alarmOpStale:    true,
}

// SignalK's notification methods. "sound" and "visual" are the spec's values.
var alarmMethods = map[string]bool{"sound": true, "visual": true}

type alarmRule struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`

	// Path is a dotted SignalK path, e.g. "environment.depth.belowTransducer".
	Path  string `json:"path"`
	Label string `json:"label"`

	Op    string  `json:"op"`
	Value float64 `json:"value"`

	// Hysteresis is the deadband a value must re-cross before the alarm clears,
	// and DwellSeconds how long the condition must hold before it fires. Both
	// exist to stop a value hovering at the threshold producing an alarm storm
	// — the most common reason people switch marine alarms off entirely.
	Hysteresis   float64 `json:"hysteresis"`
	DwellSeconds int     `json:"dwell_seconds"`

	// StaleAfterSeconds applies to alarmOpStale only.
	StaleAfterSeconds int `json:"stale_after_seconds"`

	State   string   `json:"state"`
	Methods []string `json:"methods"`
	Notify  []string `json:"notify"`

	EscalateAfterSeconds int `json:"escalate_after_seconds"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type alarmRulesFile struct {
	Rules []*alarmRule `json:"rules"`
}

var (
	alarmRulesMu    sync.RWMutex
	alarmRulesState = map[string]*alarmRule{}
)

// Rules live in their own file rather than settings.yaml deliberately: POST
// /api/settings rebuilds its sections wholesale, so a new top-level key there
// would be silently destroyed on the next settings save.
func alarmRulesFilePath() string {
	return cacheFilePath("ALARM_RULES_FILE", "data/alarm-rules.json")
}

func loadAlarmRules() error {
	path := alarmRulesFilePath()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// A fresh install has no rules; that is not a failure.
			alarmRulesMu.Lock()
			alarmRulesState = map[string]*alarmRule{}
			alarmRulesMu.Unlock()
			return nil
		}
		return fmt.Errorf("reading alarm rules: %w", err)
	}

	var file alarmRulesFile
	if len(data) > 0 {
		if err := json.Unmarshal(data, &file); err != nil {
			return fmt.Errorf("parsing alarm rules: %w", err)
		}
	}

	next := make(map[string]*alarmRule, len(file.Rules))
	for _, rule := range file.Rules {
		if rule == nil || strings.TrimSpace(rule.ID) == "" {
			continue
		}
		next[rule.ID] = rule
	}

	alarmRulesMu.Lock()
	alarmRulesState = next
	alarmRulesMu.Unlock()
	return nil
}

func saveAlarmRulesLocked() error {
	list := make([]*alarmRule, 0, len(alarmRulesState))
	for _, rule := range alarmRulesState {
		list = append(list, rule)
	}
	sortAlarmRules(list)
	return writeJSONFileAtomic(alarmRulesFilePath(), alarmRulesFile{Rules: list})
}

// sortAlarmRules gives both the file and the API a stable order. CreatedAt
// alone is not enough — rules created in the same millisecond would tie.
func sortAlarmRules(list []*alarmRule) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].CreatedAt.Equal(list[j].CreatedAt) {
			return list[i].ID < list[j].ID
		}
		return list[i].CreatedAt.Before(list[j].CreatedAt)
	})
}

func listAlarmRules() []alarmRule {
	alarmRulesMu.RLock()
	defer alarmRulesMu.RUnlock()

	list := make([]*alarmRule, 0, len(alarmRulesState))
	for _, rule := range alarmRulesState {
		list = append(list, rule)
	}
	sortAlarmRules(list)

	out := make([]alarmRule, 0, len(list))
	for _, rule := range list {
		out = append(out, *rule)
	}
	return out
}

func getAlarmRule(id string) (alarmRule, bool) {
	alarmRulesMu.RLock()
	defer alarmRulesMu.RUnlock()

	rule, ok := alarmRulesState[id]
	if !ok {
		return alarmRule{}, false
	}
	return *rule, true
}

func createAlarmRule(rule alarmRule) (alarmRule, error) {
	if err := validateAlarmRule(&rule); err != nil {
		return alarmRule{}, err
	}

	now := time.Now().UTC()
	rule.ID = uuid.NewString()
	rule.CreatedAt = now
	rule.UpdatedAt = now

	alarmRulesMu.Lock()
	defer alarmRulesMu.Unlock()

	alarmRulesState[rule.ID] = &rule
	if err := saveAlarmRulesLocked(); err != nil {
		delete(alarmRulesState, rule.ID)
		return alarmRule{}, err
	}
	return rule, nil
}

func updateAlarmRule(id string, next alarmRule) (alarmRule, error) {
	if err := validateAlarmRule(&next); err != nil {
		return alarmRule{}, err
	}

	alarmRulesMu.Lock()
	defer alarmRulesMu.Unlock()

	existing, ok := alarmRulesState[id]
	if !ok {
		return alarmRule{}, fmt.Errorf("no alarm rule with id %q", id)
	}

	// Identity and creation time belong to the server, not the payload.
	previous := *existing
	next.ID = existing.ID
	next.CreatedAt = existing.CreatedAt
	next.UpdatedAt = time.Now().UTC()

	alarmRulesState[id] = &next
	if err := saveAlarmRulesLocked(); err != nil {
		alarmRulesState[id] = &previous
		return alarmRule{}, err
	}
	return next, nil
}

func deleteAlarmRule(id string) error {
	alarmRulesMu.Lock()
	defer alarmRulesMu.Unlock()

	existing, ok := alarmRulesState[id]
	if !ok {
		return fmt.Errorf("no alarm rule with id %q", id)
	}

	delete(alarmRulesState, id)
	if err := saveAlarmRulesLocked(); err != nil {
		alarmRulesState[id] = existing
		return err
	}
	return nil
}

// validateAlarmRule normalizes in place and reports the first problem. It is
// deliberately strict: a rule that does not do what its author meant is worse
// than no rule, because it is trusted and silent.
func validateAlarmRule(rule *alarmRule) error {
	rule.Path = strings.TrimSpace(rule.Path)
	if rule.Path == "" {
		return fmt.Errorf("alarm rule requires a SignalK path")
	}

	rule.Label = strings.TrimSpace(rule.Label)
	if rule.Label == "" {
		return fmt.Errorf("alarm rule requires a label")
	}

	rule.Op = strings.TrimSpace(rule.Op)
	if !alarmOperators[rule.Op] {
		return fmt.Errorf("unknown alarm operator %q", rule.Op)
	}

	rule.State = strings.TrimSpace(rule.State)
	if _, ok := alarmStateRank[rule.State]; !ok {
		return fmt.Errorf("unknown alarm state %q", rule.State)
	}
	if rule.State == alarmStateNormal {
		return fmt.Errorf("%q is the cleared state and cannot be raised by a rule", alarmStateNormal)
	}

	if rule.Hysteresis < 0 {
		return fmt.Errorf("hysteresis cannot be negative")
	}
	if rule.DwellSeconds < 0 {
		return fmt.Errorf("dwell seconds cannot be negative")
	}
	if rule.StaleAfterSeconds < 0 {
		return fmt.Errorf("stale seconds cannot be negative")
	}
	if rule.EscalateAfterSeconds < 0 {
		return fmt.Errorf("escalate seconds cannot be negative")
	}

	for _, method := range rule.Methods {
		if !alarmMethods[method] {
			return fmt.Errorf("unknown notification method %q", method)
		}
	}
	if len(rule.Methods) == 0 {
		// An alarm nobody can see or hear is not an alarm.
		rule.Methods = []string{"visual"}
	}

	if rule.Op == alarmOpStale && rule.StaleAfterSeconds == 0 {
		rule.StaleAfterSeconds = 60
	}

	return nil
}
