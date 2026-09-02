package main

import (
	"fmt"
	"time"
)

/*
The heavy-weather rule set (ADR 0070).

Every threshold below is taken from Steve and Linda Dashew's Surviving the
Storm, and none of it has been calibrated against this vessel or this coast.
That is why the whole set ships disabled: it is a starting point drawn from
one very experienced crew's numbers, published in 1999, several of them
attributed in the text to NOAA forecasters rather than derived from data the
book shows. It is a far better place to start than an empty alarm centre, and
it is not a measurement.

The set is seeded once per installation, keyed on a marker in the rules file.
A rule the operator deletes stays deleted.

One caveat that cannot be encoded and belongs in the operator's head (page
193): the same rate of fall means different things depending on whether you
are closing with the weather or it is overtaking you. A system moving at 15
knots closes 190 miles a day on a boat running with it and 528 on one heading
into it, and the barometer reacts accordingly. At anchor this does not apply.
*/
const heavyWeatherSeedMarker = "heavy-weather-v1"

// millibarsPerHourToPascalsPerSecond converts the units the book talks in to
// the units pressureRatePath reports. Getting this wrong is the difference
// between a rule that fires on a gale and one that never fires at all.
func millibarsPerHourToPascalsPerSecond(mbPerHour float64) float64 {
	return mbPerHour * pascalsPerMillibar / 3600
}

func heavyWeatherSeedRules(now time.Time) []alarmRule {
	return []alarmRule{
		{
			// Pages 82 and 85: falls of this order are what the crews in the
			// book were logging in the hours before things got serious.
			Label:        "Barometer falling",
			Path:         pressureRatePath,
			Op:           alarmOpBelow,
			Value:        millibarsPerHourToPascalsPerSecond(-1),
			Hysteresis:   millibarsPerHourToPascalsPerSecond(0.2),
			DwellSeconds: 1800,
			State:        alarmStateWarn,
		},
		{
			// Page 128, 20mb in under 12 hours, and page 459, 12mb in 4. Both
			// are around 2mb/hr sustained.
			Label:        "Barometer plummeting",
			Path:         pressureRatePath,
			Op:           alarmOpBelow,
			Value:        millibarsPerHourToPascalsPerSecond(-2),
			Hysteresis:   millibarsPerHourToPascalsPerSecond(0.2),
			DwellSeconds: 900,
			State:        alarmStateAlarm,
		},
		{
			// The three-hour tendency every marine forecast quotes, at the
			// rate page 82's 18mb-in-under-24-hours works out to.
			Label:        "Barometer down 3mb in three hours",
			Path:         pressureChange3hPath,
			Op:           alarmOpBelow,
			Value:        -3 * pascalsPerMillibar,
			Hysteresis:   0.5 * pascalsPerMillibar,
			DwellSeconds: 1800,
			State:        alarmStateWarn,
		},
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
