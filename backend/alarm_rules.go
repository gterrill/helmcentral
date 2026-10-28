package main

import (
	"database/sql"
	"errors"
	"fmt"
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

	// PathRole, when set, binds the rule to a role instead of a literal path
	// (ADR 0187): the server keeps Path equal to that role's current SignalK
	// path, so changing the house bank moves the rule with it. The only role
	// a rule can follow is house_bank_soc, the house bank's state of charge.
	// With the role unset the rule has no path and never fires.
	PathRole string `json:"path_role,omitempty"`

	Op    string  `json:"op"`
	Value float64 `json:"value"`

	// Hysteresis is the deadband a value must re-cross before the alarm clears,
	// and DwellSeconds how long the condition must hold before it fires. Both
	// exist to stop a value hovering at the threshold producing an alarm storm
	// — the most common reason people switch marine alarms off entirely.
	Hysteresis   float64 `json:"hysteresis"`
	DwellSeconds int     `json:"dwell_seconds"`

	// StaleAfterSeconds makes an alarmOpStale rule fire on silence. On a
	// threshold rule it is the age past which the reading is not trusted: no
	// raise from it, and a live alarm is held rather than cleared by it.
	StaleAfterSeconds int `json:"stale_after_seconds"`

	// AlarmLevelValue, on a tank level rule (ADR 0214), is a second threshold
	// further into the bad side than Value. The rule is one alarm whose state is
	// State while only Value is passed and "alarm" once this one is too, and
	// that falls back to State when the level comes back past it by Hysteresis.
	AlarmLevelValue *float64 `json:"alarm_level_value,omitempty"`

	// CapacityPath, on a tank level rule (ADR 0214), is the tank's capacity
	// path. The engine reads it so the card can give a volume beside the level.
	CapacityPath string `json:"capacity_path,omitempty"`

	State   string   `json:"state"`
	Methods []string `json:"methods"`
	Notify  []string `json:"notify"`

	EscalateAfterSeconds int `json:"escalate_after_seconds"`

	// SeededBy is the marker of the built-in set that created the rule, empty
	// for a rule the operator made. It is stored, never sent over the API.
	SeededBy string `json:"-"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// alarmRulesFile is the retired alarm-rules.json, read once by
// importLegacyAlarmRules (alarm_rules_import.go).
type alarmRulesFile struct {
	Rules []*alarmRule `json:"rules"`

	// SeededSets records which built-in rule sets had been offered, so the
	// import can carry the markers into alarm_rule_seeded_sets.
	SeededSets []string `json:"seeded_sets,omitempty"`
}

var (
	alarmRulesMu         sync.RWMutex
	alarmRulesState      = map[string]*alarmRule{}
	alarmRulesSeededSets []string
)

// alarmRulesFilePath is where releases before ADR 0224 kept the rules. It is
// only the source of the one-time import now.
func alarmRulesFilePath() string {
	return cacheFilePath("ALARM_RULES_FILE", "data/alarm-rules.json")
}

// loadAlarmRules fills the in-memory copy from the database.
func loadAlarmRules() error {
	store, err := alarmRuleStore()
	if err != nil {
		return err
	}
	return refreshAlarmRules(store)
}

// sortAlarmRules gives the API a stable order. CreatedAt
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
	store, err := alarmRuleStore()
	if err != nil {
		return alarmRule{}, err
	}
	err = store.writeAlarmRules("create alarm rule", func(tx *sql.Tx, now time.Time) error {
		rule.ID = uuid.NewString()
		rule.CreatedAt = now
		rule.UpdatedAt = now
		return insertAlarmRuleTx(tx, rule)
	})
	if err != nil {
		return alarmRule{}, err
	}
	return rule, nil
}

func updateAlarmRule(id string, next alarmRule) (alarmRule, error) {
	_, exists := getAlarmRule(id)
	if err := validateAlarmRuleFor(&next, exists); err != nil {
		return alarmRule{}, err
	}
	store, err := alarmRuleStore()
	if err != nil {
		return alarmRule{}, err
	}
	err = store.writeAlarmRules("update alarm rule", func(tx *sql.Tx, now time.Time) error {
		existing, err := alarmRuleByIDQ(tx, id)
		if errors.Is(err, errAlarmRuleNotFound) {
			return fmt.Errorf("no alarm rule with id %q", id)
		}
		if err != nil {
			return err
		}
		// Identity, origin and creation time belong to the server, not the payload.
		next.ID = existing.ID
		next.SeededBy = existing.SeededBy
		next.CreatedAt = existing.CreatedAt
		next.UpdatedAt = now
		return updateAlarmRuleTx(tx, next)
	})
	if err != nil {
		return alarmRule{}, err
	}
	return next, nil
}

func deleteAlarmRule(id string) error {
	store, err := alarmRuleStore()
	if err != nil {
		return err
	}
	return store.writeAlarmRules("delete alarm rule", func(tx *sql.Tx, _ time.Time) error {
		if err := deleteAlarmRuleTx(tx, id); errors.Is(err, errAlarmRuleNotFound) {
			return fmt.Errorf("no alarm rule with id %q", id)
		} else {
			return err
		}
	})
}

// validateAlarmRule normalizes in place and reports the first problem. It is
// deliberately strict: a rule that does not do what its author meant is worse
// than no rule, because it is trusted and silent.
func validateAlarmRule(rule *alarmRule) error {
	return validateAlarmRuleFor(rule, false)
}

// errHouseBankNotChosen is what resolving a role says when no house bank is set.
var errHouseBankNotChosen = errors.New("choose a house bank in Settings first: this rule follows it")

// validateAlarmRuleFor is validateAlarmRule, for a rule that may already
// exist. A rule that follows the house bank can be saved with no path when it
// is disabled, or when it is an existing rule being edited: it then simply
// does not evaluate, so it can still be switched off or reworded while the
// bank is unchosen. A new enabled rule is still refused, because it would watch
// nothing from the moment it was made.
func validateAlarmRuleFor(rule *alarmRule, existing bool) error {
	rule.Path = strings.TrimSpace(rule.Path)
	rule.PathRole = strings.TrimSpace(rule.PathRole)
	if rule.PathRole != "" {
		if rule.PathRole != alarmPathRoleHouseBankSoC {
			return fmt.Errorf("unknown path role %q", rule.PathRole)
		}
		path, err := alarmRolePath(rule.PathRole)
		switch {
		case errors.Is(err, errHouseBankNotChosen) && (existing || !rule.Enabled):
			path = ""
		case err != nil:
			return err
		}
		rule.Path = path
	} else if rule.Path == "" {
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

// alarmPathRoleHouseBankSoC is the one role a rule can follow: the house
// bank's state of charge.
const alarmPathRoleHouseBankSoC = "house_bank_soc"

// alarmRolePath is the SignalK path a role resolves to now, "" when the role is
// not set. A rule saved while the role is unset is refused: it would watch
// nothing, and a rule that watches nothing is trusted and silent.
func alarmRolePath(role string) (string, error) {
	vessel, err := loadVesselView(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", role, err)
	}
	path := vessel.houseBankSocPath()
	if path == "" {
		return "", errHouseBankNotChosen
	}
	return path, nil
}

// syncRoleBoundRules moves every rule that follows a role onto the role's
// current path. A role that is not set leaves its rules with no path, so they
// stop watching the bank they used to follow instead of watching a stale one.
// It writes only when something moved.
func syncRoleBoundRules(vessel vesselView) error {
	store, err := alarmRuleStore()
	if err != nil {
		return err
	}
	want := vessel.houseBankSocPath()
	moves := false
	for _, rule := range listAlarmRules() {
		if rule.PathRole == alarmPathRoleHouseBankSoC && rule.Path != want {
			moves = true
		}
	}
	if !moves {
		return nil
	}
	return store.writeAlarmRules("move role-bound alarm rules", func(tx *sql.Tx, now time.Time) error {
		rules, err := listAlarmRulesQ(tx)
		if err != nil {
			return err
		}
		for _, rule := range rules {
			if rule.PathRole != alarmPathRoleHouseBankSoC || rule.Path == want {
				continue
			}
			rule.Path = want
			rule.UpdatedAt = now
			if err := updateAlarmRuleTx(tx, rule); err != nil {
				return err
			}
		}
		return nil
	})
}
