package main

import (
	"fmt"
	"time"
)

/*
The heavy-weather rule set (ADR 0070, reduced by ADR 0095).

Every threshold below is taken from Steve and Linda Dashew's Surviving the
Storm, and none of it has been calibrated against this vessel or this coast.
That is why the whole set ships disabled: it is a starting point drawn from
one very experienced crew's numbers, published in 1999, several of them
attributed in the text to NOAA forecasters rather than derived from data the
book shows. It is a far better place to start than an empty alarm centre, and
it is not a measurement.

This set originally shipped five rules. Three of them -- two rate-of-fall
rules and a three-hour tendency rule, no rising-pressure counterpart and no
absolute-pressure gate among them -- moved to the Law of Storms ladder
(alarm_seed_law_of_storms.go), which reads as a coherent tendency ladder off
the same sources marine forecasts already quote. pressureRatePath itself
stays published for gauges; nothing here binds a rule to it any longer.

The set is seeded once per installation, keyed on a marker in the rules file.
A rule the operator deletes stays deleted.

One caveat that cannot be encoded and belongs in the operator's head (page
193): the same rate of fall means different things depending on whether you
are closing with the weather or it is overtaking you. A system moving at 15
knots closes 190 miles a day on a boat running with it and 528 on one heading
into it, and the barometer reacts accordingly. At anchor this does not apply.
*/
const heavyWeatherSeedMarker = "heavy-weather-v1"

func heavyWeatherSeedRules(now time.Time) []alarmRule {
	return []alarmRule{
		{
			// Pages 89 and 188. The book calls squash zones the cause of the
			// majority of heavy-weather trouble yachts meet, and names the
			// western South Pacific around New Zealand and Australia as
			// getting more than its share.
			Label:        "Squash zone",
			Path:         squashZoneIndexPath,
			Op:           alarmOpAbove,
			Value:        0.5,
			DwellSeconds: 1800,
			State:        alarmStateWarn,
		},
		{
			// Page 187: in the tropics the barometer's normal daily swing is
			// about plus or minus 1.5mb, and "any variation from the daily
			// normal fluctuation... is a cause for concern". Only useful in
			// tropical latitudes; elsewhere it will cry wolf.
			Label:        "Tropical barometer anomaly",
			Path:         pressureChange3hPath,
			Op:           alarmOpBelow,
			Value:        -1.5 * pascalsPerMillibar,
			Hysteresis:   0.3 * pascalsPerMillibar,
			DwellSeconds: 3600,
			State:        alarmStateAlert,
		},
	}
}

// seedHeavyWeatherRules creates the set once, then records that it has been
// done. Safe to call on every boot.
func seedHeavyWeatherRules() error {
	alarmRulesMu.RLock()
	for _, marker := range alarmRulesSeededSets {
		if marker == heavyWeatherSeedMarker {
			alarmRulesMu.RUnlock()
			return nil
		}
	}
	alarmRulesMu.RUnlock()

	now := time.Now().UTC()
	for _, rule := range heavyWeatherSeedRules(now) {
		// Disabled, deliberately: see this file's doc comment.
		rule.Enabled = false
		if _, err := createAlarmRule(rule); err != nil {
			return fmt.Errorf("seeding heavy-weather rule %q: %w", rule.Label, err)
		}
	}

	alarmRulesMu.Lock()
	alarmRulesSeededSets = append(alarmRulesSeededSets, heavyWeatherSeedMarker)
	err := saveAlarmRulesLocked()
	alarmRulesMu.Unlock()
	if err != nil {
		return fmt.Errorf("recording the heavy-weather seed marker: %w", err)
	}
	return nil
}
