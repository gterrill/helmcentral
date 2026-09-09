package main

import "fmt"

/*
The forecast-warnings rule set (plan: fold the forecast warning into the
alarm system; ADR 0087).

Unlike the heavy-weather set (alarm_seed_heavy_weather.go), these four ship
enabled. Heavy weather's thresholds are one crew's read of a 1999 book,
untested against this boat or this coast; offering them live would assert a
confidence nobody has earned. A forecast wind or surf warning is the
opposite case: it is the official met-service call for the vessel's own
zone, already filtered down to "currently active" by the plugin before it
ever reaches the host. There is nothing here to calibrate, and shipping it
disabled by default would mean the one warning an operator most needs never
reaches the alarm centre, the log, or a transport unless they find the
toggle first.

The set is seeded once per installation, keyed on the forecast-warnings-v1
marker in the rules file, exactly like the heavy-weather set's own marker.
A rule the operator deletes or disables afterwards stays that way.
*/
const forecastWarningsSeedMarker = "forecast-warnings-v1"

// These literal IDs give forecastWarningsSeedRules' four rules independent
// identities before they are ever persisted, the same idiom anchorDragRuleID
// (alarm_anchor.go) uses for a rule the engine tracks outside the rules
// file. alarmEngine.evaluate keys its per-rule state on rule.ID, and two
// rules sharing the wind-warning path also share an empty ID unless each
// carries one of its own -- createAlarmRule (called from
// seedForecastWarningsRules below) overwrites these with a fresh UUID on
// disk regardless, so they only matter for code, like the engine-level
// fetcher tests, that evaluates this raw slice directly.
const (
	forecastWindWarningRuleID = "helmcentral:forecast-wind-warning"
	forecastGaleWarningRuleID = "helmcentral:forecast-gale-or-storm-warning"
	forecastSurfWarningRuleID = "helmcentral:forecast-surf-warning"
	forecastUnavailableRuleID = "helmcentral:forecast-warnings-unavailable"
)

func forecastWarningsSeedRules() []alarmRule {
	return []alarmRule{
		{
			ID:      forecastWindWarningRuleID,
			Label:   "Forecast wind warning",
			Enabled: true,
			Path:    forecastWindWarningLevelPath,
			Op:      alarmOpAbove,
			Value:   0.5,
			State:   alarmStateWarn,
		},
		{
			// Gale is level 2 and storm/hurricane is level 3 on the wind ladder
			// (forecast_warnings_fetcher.go); both clear "above 1.5" the same
			// way, so one rule covers either.
			ID:      forecastGaleWarningRuleID,
			Label:   "Forecast gale or storm warning",
			Enabled: true,
			Path:    forecastWindWarningLevelPath,
			Op:      alarmOpAbove,
			Value:   1.5,
			State:   alarmStateAlarm,
		},
		{
			ID:      forecastSurfWarningRuleID,
			Label:   "Forecast surf warning",
			Enabled: true,
			Path:    forecastSurfWarningPath,
			Op:      alarmOpAbove,
			Value:   0.5,
			State:   alarmStateAlert,
		},
		{
			// alarmSampleStale reports a never-present sample as stale, so
			// without a dwell this rule would fire the instant the server
			// boots, before the fetcher's first attempt has had a chance to
			// land. 120s covers that startup window without meaningfully
			// delaying a genuine outage report.
			ID:                forecastUnavailableRuleID,
			Label:             "Forecast warnings unavailable",
			Enabled:           true,
			Path:              forecastWindWarningLevelPath,
			Op:                alarmOpStale,
			StaleAfterSeconds: 1800,
			DwellSeconds:      120,
			State:             alarmStateAlert,
		},
	}
}

// seedForecastWarningsRules creates the set once, then records that it has
// been done. Safe to call on every boot, mirroring seedHeavyWeatherRules.
func seedForecastWarningsRules() error {
	alarmRulesMu.RLock()
	for _, marker := range alarmRulesSeededSets {
		if marker == forecastWarningsSeedMarker {
			alarmRulesMu.RUnlock()
			return nil
		}
	}
	alarmRulesMu.RUnlock()

	for _, rule := range forecastWarningsSeedRules() {
		// Enabled, deliberately: see this file's doc comment.
		rule.Enabled = true
		if _, err := createAlarmRule(rule); err != nil {
			return fmt.Errorf("seeding forecast-warnings rule %q: %w", rule.Label, err)
		}
	}

	alarmRulesMu.Lock()
	alarmRulesSeededSets = append(alarmRulesSeededSets, forecastWarningsSeedMarker)
	err := saveAlarmRulesLocked()
	alarmRulesMu.Unlock()
	if err != nil {
		return fmt.Errorf("recording the forecast-warnings seed marker: %w", err)
	}
	return nil
}
