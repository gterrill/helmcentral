package main

import (
	"math"
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
