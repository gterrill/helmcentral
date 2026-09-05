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
	// The seed markers are part of the rules file's state, so a helper that
	// stands up a fresh install has to clear them too - otherwise one test's
	// seeding suppresses every later test's.
	alarmRulesSeededSets = nil
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

// --- heavy-weather seed set (ADR 0070) ---

func TestSeedHeavyWeatherRules_CreatesThemDisabled(t *testing.T) {
	withTempAlarmRules(t)

	if err := seedHeavyWeatherRules(); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}

	rules := listAlarmRules()
	if len(rules) == 0 {
		t.Fatal("expected the heavy-weather set to be created")
	}
	for _, rule := range rules {
		// Every threshold here is one crew's number from a 1999 book, not
		// something measured on this boat. Enabling them unasked would be
		// asserting a confidence nobody has earned yet.
		if rule.Enabled {
			t.Fatalf("seeded rule %q must ship disabled", rule.Label)
		}
		if rule.DwellSeconds <= 0 {
			t.Fatalf("seeded rule %q needs a dwell; alarm storms are why people switch alarms off", rule.Label)
		}
		if !isDerivedPath(rule.Path) {
			t.Fatalf("seeded rule %q binds %q, which is not a derived path", rule.Label, rule.Path)
		}
	}
}

// Seeding is keyed on a marker in the file, not on the file being empty, so
// a set the operator deleted on purpose never comes back. Unexpected state is
// usually deliberate.
func TestSeedHeavyWeatherRules_RunsOnceAndDoesNotResurrectDeleted(t *testing.T) {
	withTempAlarmRules(t)

	if err := seedHeavyWeatherRules(); err != nil {
		t.Fatalf("first seed failed: %v", err)
	}
	first := listAlarmRules()
	if len(first) < 2 {
		t.Fatalf("expected several seeded rules, got %d", len(first))
	}

	if err := deleteAlarmRule(first[0].ID); err != nil {
		t.Fatalf("deleting a seeded rule failed: %v", err)
	}
	remaining := len(listAlarmRules())

	if err := seedHeavyWeatherRules(); err != nil {
		t.Fatalf("second seed failed: %v", err)
	}
	if got := len(listAlarmRules()); got != remaining {
		t.Fatalf("re-seeding changed the rule count from %d to %d", remaining, got)
	}

	// And the marker survives a reload from disk.
	if err := loadAlarmRules(); err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if err := seedHeavyWeatherRules(); err != nil {
		t.Fatalf("third seed failed: %v", err)
	}
	if got := len(listAlarmRules()); got != remaining {
		t.Fatalf("seeding after a reload changed the rule count to %d, want %d", got, remaining)
	}
}

// The thresholds convert the book's millibars into the SI units the derived
// paths report, then round to two decimal places because that raw conversion
// is what the operator sees and edits in the rules list. Getting the
// underlying conversion wrong is the difference between a rule that fires on
// a gale and one that never fires at all; getting the rounding wrong is a
// seventeen-digit float staring back at whoever opens the rule.
func TestSeedHeavyWeatherRules_ThresholdsAreInSIUnits(t *testing.T) {
	withTempAlarmRules(t)
	if err := seedHeavyWeatherRules(); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}

	byLabel := map[string]alarmRule{}
	for _, rule := range listAlarmRules() {
		byLabel[rule.Label] = rule
	}

	falling, ok := byLabel["Barometer falling"]
	if !ok {
		t.Fatalf("expected a 'Barometer falling' rule, got %v", byLabel)
	}
	// 1 mb/hr is 100 Pa over 3600 s, which is -0.02777... Pa/s, rounded to -0.03.
	if falling.Value != -0.03 {
		t.Fatalf("falling threshold = %v Pa/s, want exactly -0.03", falling.Value)
	}
	if falling.Hysteresis != 0.01 {
		t.Fatalf("falling hysteresis = %v Pa/s, want exactly 0.01", falling.Hysteresis)
	}
	if falling.Op != alarmOpBelow {
		t.Fatalf("a falling barometer is a 'below' rule, got %q", falling.Op)
	}

	plummeting, ok := byLabel["Barometer plummeting"]
	if !ok {
		t.Fatalf("expected a 'Barometer plummeting' rule, got %v", byLabel)
	}
	// 2 mb/hr is -0.05555... Pa/s, rounded to -0.06.
	if plummeting.Value != -0.06 {
		t.Fatalf("plummeting threshold = %v Pa/s, want exactly -0.06", plummeting.Value)
	}
	if plummeting.Hysteresis != 0.01 {
		t.Fatalf("plummeting hysteresis = %v Pa/s, want exactly 0.01", plummeting.Hysteresis)
	}

	squash, ok := byLabel["Squash zone"]
	if !ok {
		t.Fatalf("expected a 'Squash zone' rule")
	}
	if squash.Op != alarmOpAbove || squash.Value != 0.5 {
		t.Fatalf("squash-zone rule should be 'above 0.5', got %q %v", squash.Op, squash.Value)
	}
}
