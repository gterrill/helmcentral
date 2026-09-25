package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withTempAlarmRules(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "alarm-rules.json")
	t.Setenv("ALARM_RULES_FILE", path)

	alarmRulesMu.Lock()
	alarmRulesState = map[string]*alarmRule{}
	// The seed markers and ignored-sensor list are part of the rules file's
	// state, so a helper that stands up a fresh install has to clear them
	// too - otherwise one test's seeding/ignoring suppresses another's.
	alarmRulesSeededSets = nil
	alarmRulesIgnoredSensors = nil
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

// The three overlapping rate/tendency rules moved to the Law of Storms
// ladder (ADR 0095, alarm_seed_law_of_storms.go); only the two patterns
// nothing else covers -- the squash zone and the tropical anomaly -- stay
// seeded from here.
func TestSeedHeavyWeatherRules_KeepsSquashZoneAndTropicalAnomalyOnly(t *testing.T) {
	withTempAlarmRules(t)
	if err := seedHeavyWeatherRules(); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}

	rules := listAlarmRules()
	if len(rules) != 2 {
		t.Fatalf("expected exactly 2 heavy-weather rules, got %d: %+v", len(rules), rules)
	}

	byLabel := map[string]alarmRule{}
	for _, rule := range rules {
		byLabel[rule.Label] = rule
		if rule.Path == pressureRatePath {
			t.Fatalf("seeded rule %q still binds the retired pressureRate path", rule.Label)
		}
	}

	squash, ok := byLabel["Squash zone"]
	if !ok {
		t.Fatalf("expected a 'Squash zone' rule")
	}
	if squash.Op != alarmOpAbove || squash.Value != 0.5 {
		t.Fatalf("squash-zone rule should be 'above 0.5', got %q %v", squash.Op, squash.Value)
	}

	tropical, ok := byLabel["Tropical barometer anomaly"]
	if !ok {
		t.Fatalf("expected a 'Tropical barometer anomaly' rule")
	}
	if tropical.Op != alarmOpBelow || tropical.Value != -1.5*pascalsPerMillibar {
		t.Fatalf("tropical-anomaly rule should be 'below -150', got %q %v", tropical.Op, tropical.Value)
	}
	// 0.3 * pascalsPerMillibar is 30.000000000000004 in float64, not a clean
	// 30 -- an exact-equality check here would be testing a rounding
	// artifact of the multiplication, not the threshold itself.
	if math.Abs(tropical.Hysteresis-0.3*pascalsPerMillibar) > 1e-9 {
		t.Fatalf("tropical-anomaly hysteresis = %v, want ~%v", tropical.Hysteresis, 0.3*pascalsPerMillibar)
	}
}

// --- forecast-warnings seed set (plan: fold the forecast warning into the
// alarm system; ADR 0087) ---

func TestSeedForecastWarningsRules_CreatesFourEnabledRules(t *testing.T) {
	withTempAlarmRules(t)

	if err := seedForecastWarningsRules(); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}

	rules := listAlarmRules()
	if len(rules) != 4 {
		t.Fatalf("expected 4 forecast-warnings rules, got %d", len(rules))
	}

	byLabel := map[string]alarmRule{}
	for _, rule := range rules {
		byLabel[rule.Label] = rule
		// The source is the official warning itself, not an uncalibrated
		// heuristic (contrast the heavy-weather set above), so these ship live.
		if !rule.Enabled {
			t.Fatalf("seeded rule %q must ship enabled", rule.Label)
		}
		if rule.Hysteresis != 0 {
			t.Fatalf("%s: expected zero hysteresis, got %v", rule.Label, rule.Hysteresis)
		}
	}

	wind, ok := byLabel["Forecast wind warning"]
	if !ok {
		t.Fatal("expected a 'Forecast wind warning' rule")
	}
	if wind.Path != forecastWindWarningLevelPath || wind.Op != alarmOpAbove || wind.Value != 0.5 || wind.DwellSeconds != 0 || wind.State != alarmStateWarn {
		t.Fatalf("unexpected wind rule: %+v", wind)
	}

	gale, ok := byLabel["Forecast gale or storm warning"]
	if !ok {
		t.Fatal("expected a 'Forecast gale or storm warning' rule")
	}
	if gale.Path != forecastWindWarningLevelPath || gale.Op != alarmOpAbove || gale.Value != 1.5 || gale.DwellSeconds != 0 || gale.State != alarmStateAlarm {
		t.Fatalf("unexpected gale rule: %+v", gale)
	}

	surf, ok := byLabel["Forecast surf warning"]
	if !ok {
		t.Fatal("expected a 'Forecast surf warning' rule")
	}
	if surf.Path != forecastSurfWarningPath || surf.Op != alarmOpAbove || surf.Value != 0.5 || surf.DwellSeconds != 0 || surf.State != alarmStateAlert {
		t.Fatalf("unexpected surf rule: %+v", surf)
	}

	unavailable, ok := byLabel["Forecast warnings unavailable"]
	if !ok {
		t.Fatal("expected a 'Forecast warnings unavailable' rule")
	}
	if unavailable.Path != forecastWindWarningLevelPath || unavailable.Op != alarmOpStale || unavailable.StaleAfterSeconds != 1800 || unavailable.DwellSeconds != 120 || unavailable.State != alarmStateAlert {
		t.Fatalf("unexpected unavailable rule: %+v", unavailable)
	}
}

// Mirrors TestSeedHeavyWeatherRules_RunsOnceAndDoesNotResurrectDeleted:
// seeding is keyed on a marker in the file, not on the file being empty, so
// a rule the operator deletes on purpose never comes back.
func TestSeedForecastWarningsRules_RunsOnceAndDoesNotResurrectDeleted(t *testing.T) {
	withTempAlarmRules(t)

	if err := seedForecastWarningsRules(); err != nil {
		t.Fatalf("first seed failed: %v", err)
	}
	first := listAlarmRules()
	if len(first) != 4 {
		t.Fatalf("expected 4 seeded rules, got %d", len(first))
	}

	if err := deleteAlarmRule(first[0].ID); err != nil {
		t.Fatalf("deleting a seeded rule failed: %v", err)
	}
	remaining := len(listAlarmRules())

	if err := seedForecastWarningsRules(); err != nil {
		t.Fatalf("second seed failed: %v", err)
	}
	if got := len(listAlarmRules()); got != remaining {
		t.Fatalf("re-seeding changed the rule count from %d to %d", remaining, got)
	}

	// And the marker survives a reload from disk.
	if err := loadAlarmRules(); err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if err := seedForecastWarningsRules(); err != nil {
		t.Fatalf("third seed failed: %v", err)
	}
	if got := len(listAlarmRules()); got != remaining {
		t.Fatalf("seeding after a reload changed the rule count to %d, want %d", got, remaining)
	}
}

// The two seed sets are keyed on independent markers: an installation that
// already has the heavy-weather marker but not the forecast-warnings one
// must still get the forecast-warnings set on its next boot.
func TestSeedForecastWarningsRules_SeedsIndependentlyOfHeavyWeatherMarker(t *testing.T) {
	withTempAlarmRules(t)

	if err := seedHeavyWeatherRules(); err != nil {
		t.Fatalf("seeding heavy weather failed: %v", err)
	}
	afterHeavyWeather := len(listAlarmRules())

	if err := seedForecastWarningsRules(); err != nil {
		t.Fatalf("seeding forecast warnings failed: %v", err)
	}

	rules := listAlarmRules()
	if len(rules) != afterHeavyWeather+4 {
		t.Fatalf("expected the forecast-warnings set to add 4 rules on top of the heavy-weather set, got %d total (was %d)", len(rules), afterHeavyWeather)
	}
}

// --- Law of Storms seed set (ADR 0095) ---

func TestSeedLawOfStormsRules_CreatesSevenRulesWithTheStormSignatureDisabled(t *testing.T) {
	withTempAlarmRules(t)

	if err := seedLawOfStormsRules(); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}

	rules := listAlarmRules()
	if len(rules) != 7 {
		t.Fatalf("expected 7 law-of-storms rules, got %d", len(rules))
	}

	for _, rule := range rules {
		if rule.Label == "Storm signature" {
			if rule.Enabled {
				t.Fatalf("the storm-signature rule must ship disabled: the source page hedges it, and a 4mb fall under 1009mb is routine on a temperate coast")
			}
			continue
		}
		if !rule.Enabled {
			t.Fatalf("seeded rule %q must ship enabled", rule.Label)
		}
	}
}

// Getting a threshold's unit conversion wrong is the difference between a
// rule that fires on a gale and one that never fires at all, the same stakes
// TestSeedHeavyWeatherRules_KeepsSquashZoneAndTropicalAnomalyOnly's own
// comment describes.
func TestSeedLawOfStormsRules_ThresholdsAreInSIUnits(t *testing.T) {
	withTempAlarmRules(t)
	if err := seedLawOfStormsRules(); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}

	byLabel := map[string]alarmRule{}
	for _, rule := range listAlarmRules() {
		byLabel[rule.Label] = rule
	}

	up6, ok := byLabel["Barometer up 6 mb in three hours"]
	if !ok || up6.Path != pressureChange3hPath || up6.Op != alarmOpAbove || up6.Value != 600 || up6.Hysteresis != 50 || up6.DwellSeconds != 900 || up6.State != alarmStateWarn {
		t.Fatalf("unexpected up-6mb rule (ok=%v): %+v", ok, up6)
	}

	down6, ok := byLabel["Barometer down 6 mb in three hours"]
	if !ok || down6.Path != pressureChange3hPath || down6.Op != alarmOpBelow || down6.Value != -600 || down6.Hysteresis != 50 || down6.DwellSeconds != 900 || down6.State != alarmStateWarn {
		t.Fatalf("unexpected down-6mb rule (ok=%v): %+v", ok, down6)
	}

	up10, ok := byLabel["Barometer up 10 mb in three hours"]
	if !ok || up10.Path != pressureChange3hPath || up10.Op != alarmOpAbove || up10.Value != 1000 || up10.Hysteresis != 50 || up10.DwellSeconds != 900 || up10.State != alarmStateAlarm {
		t.Fatalf("unexpected up-10mb rule (ok=%v): %+v", ok, up10)
	}

	down10, ok := byLabel["Barometer down 10 mb in three hours"]
	if !ok || down10.Path != pressureChange3hPath || down10.Op != alarmOpBelow || down10.Value != -1000 || down10.Hysteresis != 50 || down10.DwellSeconds != 900 || down10.State != alarmStateAlarm {
		t.Fatalf("unexpected down-10mb rule (ok=%v): %+v", ok, down10)
	}

	storm, ok := byLabel["Storm signature"]
	if !ok || storm.Path != stormIndexPath || storm.Op != alarmOpAbove || storm.Value != 0.5 || storm.DwellSeconds != 1800 || storm.State != alarmStateAlert || storm.Enabled {
		t.Fatalf("unexpected storm-signature rule (ok=%v): %+v", ok, storm)
	}

	severe, ok := byLabel["Severe thunderstorm signature"]
	if !ok || severe.Path != severeThunderstormIndexPath || severe.Op != alarmOpAbove || severe.Value != 0.5 || severe.DwellSeconds != 900 || severe.State != alarmStateAlarm || !severe.Enabled {
		t.Fatalf("unexpected severe-thunderstorm rule (ok=%v): %+v", ok, severe)
	}

	bomb, ok := byLabel["Weather bomb"]
	if !ok || bomb.Path != pressureChange24hPath || bomb.Op != alarmOpBelow || bomb.Value != -2400 || bomb.Hysteresis != 100 || bomb.DwellSeconds != 1800 || bomb.State != alarmStateEmergency || !bomb.Enabled {
		t.Fatalf("unexpected weather-bomb rule (ok=%v): %+v", ok, bomb)
	}
}

// Mirrors TestSeedForecastWarningsRules_RunsOnceAndDoesNotResurrectDeleted:
// seeding is keyed on a marker in the file, not on the file being empty, so
// a rule the operator deletes on purpose never comes back.
func TestSeedLawOfStormsRules_RunsOnceAndDoesNotResurrectDeleted(t *testing.T) {
	withTempAlarmRules(t)

	if err := seedLawOfStormsRules(); err != nil {
		t.Fatalf("first seed failed: %v", err)
	}
	first := listAlarmRules()
	if len(first) != 7 {
		t.Fatalf("expected 7 seeded rules, got %d", len(first))
	}

	if err := deleteAlarmRule(first[0].ID); err != nil {
		t.Fatalf("deleting a seeded rule failed: %v", err)
	}
	remaining := len(listAlarmRules())

	if err := seedLawOfStormsRules(); err != nil {
		t.Fatalf("second seed failed: %v", err)
	}
	if got := len(listAlarmRules()); got != remaining {
		t.Fatalf("re-seeding changed the rule count from %d to %d", remaining, got)
	}

	// And the marker survives a reload from disk.
	if err := loadAlarmRules(); err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if err := seedLawOfStormsRules(); err != nil {
		t.Fatalf("third seed failed: %v", err)
	}
	if got := len(listAlarmRules()); got != remaining {
		t.Fatalf("seeding after a reload changed the rule count to %d, want %d", got, remaining)
	}
}

// The three seed sets are keyed on independent markers: an installation that
// already has the other two markers but not this one must still get the
// law-of-storms set on its next boot.
func TestSeedLawOfStormsRules_SeedsIndependentlyOfTheOtherMarkers(t *testing.T) {
	withTempAlarmRules(t)

	if err := seedHeavyWeatherRules(); err != nil {
		t.Fatalf("seeding heavy weather failed: %v", err)
	}
	if err := seedForecastWarningsRules(); err != nil {
		t.Fatalf("seeding forecast warnings failed: %v", err)
	}
	before := len(listAlarmRules())

	if err := seedLawOfStormsRules(); err != nil {
		t.Fatalf("seeding law-of-storms failed: %v", err)
	}

	rules := listAlarmRules()
	if len(rules) != before+7 {
		t.Fatalf("expected the law-of-storms set to add 7 rules on top of the others, got %d total (was %d)", len(rules), before)
	}
}

// findAlarmRule returns the persisted rule with the given id, or ok=false.
// Used by the anomaly seed tests below to check a rule survived seeding
// under its own fixed ID (code review finding 7), rather than under
// whatever ID createAlarmRule would otherwise have assigned it.
func findAlarmRule(t *testing.T, id string) (alarmRule, bool) {
	t.Helper()
	for _, rule := range listAlarmRules() {
		if rule.ID == id {
			return rule, true
		}
	}
	return alarmRule{}, false
}

// --- Anomaly detection seed set (anomaly-v1) --------------------------------

func TestSeedAnomalyRules_BaseSetNeedsNoVesselSetup(t *testing.T) {
	withTempAlarmRules(t)

	if err := seedAnomalyRules(vesselSettings{}); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}

	rules := listAlarmRules()
	if len(rules) != 2 {
		t.Fatalf("expected only the 2 base rules (impossible + silent source) with no vessel setup, got %d: %+v", len(rules), rules)
	}
	byID := map[string]alarmRule{}
	for _, rule := range rules {
		byID[rule.ID] = rule
		if rule.Path != anomalySensorOutOfRangeCountPath && rule.Path != anomalySensorSilentSourceCountPath {
			t.Fatalf("unexpected rule seeded with no vessel setup: %+v", rule)
		}
		if !rule.Enabled {
			t.Fatalf("%s must ship enabled -- it needs no setup and no tuning", rule.Label)
		}
	}
	// The rules must persist under their own fixed IDs (code review finding
	// 7), not whatever createAlarmRule would otherwise assign -- see
	// createSeededAlarmRule's own doc comment.
	if _, ok := byID[anomalyImpossibleRuleID]; !ok {
		t.Fatalf("expected a rule persisted under the fixed id %q, got %+v", anomalyImpossibleRuleID, rules)
	}
	if _, ok := byID[anomalySilentSourceRuleID]; !ok {
		t.Fatalf("expected a rule persisted under the fixed id %q, got %+v", anomalySilentSourceRuleID, rules)
	}
}

func TestSeedAnomalyRules_FrozenNeedsAtLeastOneEngine(t *testing.T) {
	withTempAlarmRules(t)

	if err := seedAnomalyRules(vesselSettings{
		Engines: []vesselEngineSetting{{Instance: "port", Name: "Port"}},
	}); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}

	rule, ok := findAlarmRule(t, anomalyFrozenRuleID)
	if !ok {
		t.Fatalf("expected the frozen rule persisted under its fixed id %q once at least one engine is configured", anomalyFrozenRuleID)
	}
	if rule.Path != anomalySensorFrozenCountPath || !rule.Enabled || rule.Op != alarmOpAbove {
		t.Fatalf("unexpected frozen rule: %+v", rule)
	}
}

func TestSeedAnomalyRules_BatteryNeedsACompleteHouseBank(t *testing.T) {
	withTempAlarmRules(t)

	// A house bank is chosen but has no linked profile at all yet.
	incomplete := vesselSettings{
		HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.0", CapacityAh: 400, Cells: 8},
	}
	if err := seedAnomalyRules(incomplete); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}
	for _, rule := range listAlarmRules() {
		if rule.ID == anomalyFullBankWarnRuleID {
			t.Fatalf("did not expect the full-bank-charging rule with no linked battery profile: %+v", rule)
		}
	}
}

func TestSeedAnomalyRules_BatterySeedsOnceTheProfileIsComplete(t *testing.T) {
	withTempAlarmRules(t)
	equipmentID := setupBatteryTestStore(t)
	setupBatteryProfileFixture(t)

	vessel := vesselSettings{
		HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.0", EquipmentID: equipmentID, CapacityAh: 400, Cells: 8},
	}
	if err := seedAnomalyRules(vessel); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}

	warn, ok := findAlarmRule(t, anomalyFullBankWarnRuleID)
	if !ok || !warn.Enabled || warn.Value != 0.5 || warn.Path != anomalyBatteryFullBankChargingPath || warn.Label != "Charging into a full house bank" {
		t.Fatalf("unexpected/missing full-bank warn rule under its fixed id %q: %+v", anomalyFullBankWarnRuleID, warn)
	}
	trip, ok := findAlarmRule(t, anomalyFullBankAlarmRuleID)
	if !ok || trip.Enabled || trip.Label != "House bank overcharge risk" {
		t.Fatalf("unexpected/missing near-trip rule under its fixed id %q: %+v", anomalyFullBankAlarmRuleID, trip)
	}
}

// TestSeedAnomalyRules_EngineResidualRulesKeepFixedIDs covers the dynamic
// (per-instance) half of code review finding 7: the twin-residual pair's
// IDs are built from the engine's own instance id (anomalyEngineSeedRules),
// and must survive seeding unchanged the same way the fixed top-level
// rules do -- not just for one arbitrarily-chosen quantity, but for all
// four seeded pairs, so re-seeding after (say) an app restart recognises
// the exact same rules rather than creating a duplicate set under fresh
// UUIDs (which alarmRulesSeededSets' own marker prevents by short-
// circuiting, but only ever worked because the marker doesn't depend on
// IDs -- this test is what actually pins the ID side down).
func TestSeedAnomalyRules_EngineResidualRulesKeepFixedIDs(t *testing.T) {
	withTempAlarmRules(t)

	vessel := vesselSettings{Engines: []vesselEngineSetting{
		{Instance: "port", Name: "Port"},
		{Instance: "starboard", Name: "Starboard"},
	}}
	if err := seedAnomalyRules(vessel); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}

	for _, seed := range anomalyResidualSeeds {
		highID := fmt.Sprintf("helmcentral:anomaly-%s-port-high", seed.IDSuffix)
		lowID := fmt.Sprintf("helmcentral:anomaly-%s-port-low", seed.IDSuffix)
		if _, ok := findAlarmRule(t, highID); !ok {
			t.Fatalf("expected a residual rule persisted under its fixed id %q", highID)
		}
		if _, ok := findAlarmRule(t, lowID); !ok {
			t.Fatalf("expected a residual rule persisted under its fixed id %q", lowID)
		}
	}
}

func TestSeedAnomalyRules_TwoEnginesSeedOnlyTheFirstEnginesResiduals(t *testing.T) {
	withTempAlarmRules(t)

	vessel := vesselSettings{Engines: []vesselEngineSetting{
		{Instance: "port", Name: "Port"},
		{Instance: "starboard", Name: "Starboard"},
	}}
	if err := seedAnomalyRules(vessel); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}

	portResidual := 0
	starboardResidual := 0
	for _, rule := range listAlarmRules() {
		switch {
		case strings.Contains(rule.Path, "anomaly.engines.port."):
			portResidual++
			if rule.Enabled {
				t.Fatalf("expected twin residual rules seeded disabled: %+v", rule)
			}
		case strings.Contains(rule.Path, "anomaly.engines.starboard."):
			starboardResidual++
		}
	}
	// 4 quantities x 2 (high/low) = 8.
	if portResidual != 8 {
		t.Fatalf("expected 8 residual rules for port, got %d", portResidual)
	}
	if starboardResidual != 0 {
		t.Fatalf("expected no residual rules seeded for starboard (mirror of port on a twin), got %d", starboardResidual)
	}
}

func TestSeedAnomalyRules_ThreeEnginesSeedEveryEnginesResiduals(t *testing.T) {
	withTempAlarmRules(t)

	vessel := vesselSettings{Engines: []vesselEngineSetting{
		{Instance: "port", Name: "Port"},
		{Instance: "center", Name: "Center"},
		{Instance: "starboard", Name: "Starboard"},
	}}
	if err := seedAnomalyRules(vessel); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}

	counts := map[string]int{}
	for _, rule := range listAlarmRules() {
		for _, instance := range []string{"port", "center", "starboard"} {
			if strings.Contains(rule.Path, "anomaly.engines."+instance+".") {
				counts[instance]++
			}
		}
	}
	for _, instance := range []string{"port", "center", "starboard"} {
		if counts[instance] != 8 {
			t.Fatalf("expected 8 residual rules for %s with 3 engines configured, got %d", instance, counts[instance])
		}
	}
}

func TestSeedAnomalyRules_RunsRepeatedlyWithoutDuplicating(t *testing.T) {
	withTempAlarmRules(t)
	vessel := vesselSettings{Engines: []vesselEngineSetting{
		{Instance: "port", Name: "Port"}, {Instance: "starboard", Name: "Starboard"},
	}}

	if err := seedAnomalyRules(vessel); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	first := len(listAlarmRules())

	if err := seedAnomalyRules(vessel); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if got := len(listAlarmRules()); got != first {
		t.Fatalf("re-seeding changed the rule count from %d to %d", first, got)
	}
}

// TestSeedAnomalyRules_RenamingAnEngineDoesNotReseedOrOrphanItsRules covers
// code review finding 6's seeding half: anomalyEngineSeedMarker used to be
// keyed on the operator's own editable display name, so renaming "Port" to
// "Port Main" and saving again (an ordinary vessel-settings save, which
// calls seedAnomalyRules every time) would look like a brand new engine --
// a second marker, a second set of 8 residual rules bound to the new name's
// path, and the original 8 left behind bound to a path nothing publishes to
// any more. The marker (and the residual path under it) must follow the
// stable Signal K instance id, so a rename re-seeds nothing at all.
func TestSeedAnomalyRules_RenamingAnEngineDoesNotReseedOrOrphanItsRules(t *testing.T) {
	withTempAlarmRules(t)
	vessel := vesselSettings{Engines: []vesselEngineSetting{
		{Instance: "port", Name: "Port"}, {Instance: "starboard", Name: "Starboard"},
	}}
	if err := seedAnomalyRules(vessel); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	first := len(listAlarmRules())

	renamed := vesselSettings{Engines: []vesselEngineSetting{
		{Instance: "port", Name: "Port Main"}, {Instance: "starboard", Name: "Starboard"},
	}}
	if err := seedAnomalyRules(renamed); err != nil {
		t.Fatalf("seed after rename: %v", err)
	}

	if got := len(listAlarmRules()); got != first {
		t.Fatalf("renaming the engine and re-seeding changed the rule count from %d to %d (a second set was seeded under the new name)", first, got)
	}
	for _, rule := range listAlarmRules() {
		if strings.Contains(rule.Path, "anomaly.engines.port main.") {
			t.Fatalf("expected no rules seeded under the renamed display name's own path, got %+v", rule)
		}
	}
}

func TestSeedAnomalyRules_AddingAnEngineLaterSeedsItsOwnRules(t *testing.T) {
	withTempAlarmRules(t)

	// Start with a single engine: only the frozen rule, no residual pair
	// yet (needs >= 2).
	if err := seedAnomalyRules(vesselSettings{Engines: []vesselEngineSetting{{Instance: "port", Name: "Port"}}}); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	beforeSecondEngine := len(listAlarmRules())

	// The operator adds starboard and saves again.
	vessel := vesselSettings{Engines: []vesselEngineSetting{
		{Instance: "port", Name: "Port"}, {Instance: "starboard", Name: "Starboard"},
	}}
	if err := seedAnomalyRules(vessel); err != nil {
		t.Fatalf("second seed: %v", err)
	}

	after := len(listAlarmRules())
	if after != beforeSecondEngine+8 {
		t.Fatalf("expected exactly 8 new residual rules once a second engine was added, got %d new (was %d, now %d)", after-beforeSecondEngine, beforeSecondEngine, after)
	}
}

func TestSeedAnomalyRules_DoesNotResurrectADeletedRule(t *testing.T) {
	withTempAlarmRules(t)

	if err := seedAnomalyRules(vesselSettings{}); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	rules := listAlarmRules()
	if err := deleteAlarmRule(rules[0].ID); err != nil {
		t.Fatalf("deleting a seeded rule: %v", err)
	}
	remaining := len(listAlarmRules())

	if err := seedAnomalyRules(vesselSettings{}); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if got := len(listAlarmRules()); got != remaining {
		t.Fatalf("re-seeding resurrected a deleted rule: got %d, want %d", got, remaining)
	}
}
