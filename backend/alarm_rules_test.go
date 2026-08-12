package main

import (
	"os"
	"path/filepath"
	"testing"
)

func withTempAlarmRules(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "alarm-rules.json")
	t.Setenv("ALARM_RULES_FILE", path)

	alarmRulesMu.Lock()
	alarmRulesState = map[string]*alarmRule{}
	alarmRulesMu.Unlock()

	return path
}

func validRule() alarmRule {
	return alarmRule{
		Enabled:      true,
		Path:         "electrical.batteries.house.voltage",
		Label:        "House bank low",
		Op:           alarmOpBelow,
		Value:        11.8,
		Hysteresis:   0.3,
		DwellSeconds: 30,
		State:        alarmStateAlarm,
		Methods:      []string{"sound", "visual"},
	}
}

func TestCreateAlarmRuleAssignsIDAndPersists(t *testing.T) {
	path := withTempAlarmRules(t)

	created, err := createAlarmRule(validRule())
	if err != nil {
		t.Fatalf("createAlarmRule: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected a generated id")
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected rules file written to %s: %v", path, err)
	}

	// Reload from disk to prove persistence, not just in-memory state.
	alarmRulesMu.Lock()
	alarmRulesState = map[string]*alarmRule{}
	alarmRulesMu.Unlock()

	if err := loadAlarmRules(); err != nil {
		t.Fatalf("loadAlarmRules: %v", err)
	}
	reloaded, ok := getAlarmRule(created.ID)
	if !ok {
		t.Fatalf("rule did not survive a reload")
	}
	if reloaded.Path != created.Path || reloaded.Value != created.Value {
		t.Fatalf("reloaded rule differs: %+v vs %+v", reloaded, created)
	}
}

func TestLoadAlarmRulesTreatsMissingFileAsEmpty(t *testing.T) {
	withTempAlarmRules(t)

	if err := loadAlarmRules(); err != nil {
		t.Fatalf("a missing rules file is a fresh install, not an error: %v", err)
	}
	if len(listAlarmRules()) != 0 {
		t.Fatalf("expected no rules, got %d", len(listAlarmRules()))
	}
}

func TestListAlarmRulesIsOrderedForStableOutput(t *testing.T) {
	withTempAlarmRules(t)

	for _, label := range []string{"first", "second", "third"} {
		rule := validRule()
		rule.Label = label
		if _, err := createAlarmRule(rule); err != nil {
			t.Fatalf("createAlarmRule(%s): %v", label, err)
		}
	}

	first := listAlarmRules()
	second := listAlarmRules()
	if len(first) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(first))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("listAlarmRules order is not stable at %d", i)
		}
	}
}

func TestUpdateAlarmRuleRejectsUnknownID(t *testing.T) {
	withTempAlarmRules(t)

	if _, err := updateAlarmRule("does-not-exist", validRule()); err == nil {
		t.Fatalf("expected an error updating an unknown rule")
	}
}

func TestUpdateAlarmRulePreservesIDAndCreatedAt(t *testing.T) {
	withTempAlarmRules(t)

	created, err := createAlarmRule(validRule())
	if err != nil {
		t.Fatalf("createAlarmRule: %v", err)
	}

	next := validRule()
	next.ID = "attempted-override"
	next.Value = 12.2
	updated, err := updateAlarmRule(created.ID, next)
	if err != nil {
		t.Fatalf("updateAlarmRule: %v", err)
	}

	if updated.ID != created.ID {
		t.Fatalf("id must not be reassignable by the payload: got %q, want %q", updated.ID, created.ID)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("created_at must be preserved across updates")
	}
	if updated.Value != 12.2 {
		t.Fatalf("value: got %v, want 12.2", updated.Value)
	}
}

func TestDeleteAlarmRuleRemovesItFromDisk(t *testing.T) {
	withTempAlarmRules(t)

	created, err := createAlarmRule(validRule())
	if err != nil {
		t.Fatalf("createAlarmRule: %v", err)
	}
	if err := deleteAlarmRule(created.ID); err != nil {
		t.Fatalf("deleteAlarmRule: %v", err)
	}

	alarmRulesMu.Lock()
	alarmRulesState = map[string]*alarmRule{}
	alarmRulesMu.Unlock()
	if err := loadAlarmRules(); err != nil {
		t.Fatalf("loadAlarmRules: %v", err)
	}
	if _, ok := getAlarmRule(created.ID); ok {
		t.Fatalf("deleted rule came back after reload")
	}
}

func TestValidateAlarmRuleRejectsBadInput(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*alarmRule)
	}{
		{"blank path", func(r *alarmRule) { r.Path = "  " }},
		{"blank label", func(r *alarmRule) { r.Label = "" }},
		{"unknown operator", func(r *alarmRule) { r.Op = "sideways" }},
		{"unknown state", func(r *alarmRule) { r.State = "panic" }},
		{"normal is not a raisable state", func(r *alarmRule) { r.State = "normal" }},
		{"negative hysteresis", func(r *alarmRule) { r.Hysteresis = -1 }},
		{"negative dwell", func(r *alarmRule) { r.DwellSeconds = -5 }},
		{"unknown method", func(r *alarmRule) { r.Methods = []string{"telepathy"} }},
	}

	for _, tc := range cases {
		rule := validRule()
		tc.mutate(&rule)
		if err := validateAlarmRule(&rule); err == nil {
			t.Fatalf("%s: expected a validation error", tc.name)
		}
	}
}

func TestValidateAlarmRuleAcceptsStaleRuleWithoutThreshold(t *testing.T) {
	// A staleness rule fires on absence, so it has no threshold to compare.
	rule := validRule()
	rule.Op = alarmOpStale
	rule.Value = 0

	if err := validateAlarmRule(&rule); err != nil {
		t.Fatalf("stale rule should not require a threshold: %v", err)
	}
}

// Hysteresis exists to stop a value hovering at the threshold from producing an
// alarm storm, which is the most common reason people switch marine alarms off.
func TestValidateAlarmRuleTrimsAndDefaultsMethods(t *testing.T) {
	rule := validRule()
	rule.Path = "  environment.depth.belowTransducer  "
	rule.Label = "  Shallow  "
	rule.Methods = nil

	if err := validateAlarmRule(&rule); err != nil {
		t.Fatalf("validateAlarmRule: %v", err)
	}
	if rule.Path != "environment.depth.belowTransducer" {
		t.Fatalf("path not trimmed: %q", rule.Path)
	}
	if rule.Label != "Shallow" {
		t.Fatalf("label not trimmed: %q", rule.Label)
	}
	if len(rule.Methods) == 0 {
		t.Fatalf("expected a default notification method rather than a silent alarm")
	}
}
