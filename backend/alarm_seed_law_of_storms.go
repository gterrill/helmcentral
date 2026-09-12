package main

import "fmt"

/*
The Law of Storms barometer rule set (ADR 0095), from R. J. Ellis, "Secret
Law of Storms" (worldstormcentral.co, rules-for-storms-and-gales page).

This replaces three of the five rules the heavy-weather set (ADR 0070,
alarm_seed_heavy_weather.go) shipped: two overlapping falling-pressure rate
rules and a three-hour tendency rule, none of which had a rising-pressure
counterpart, an absolute-pressure gate, or a window past three hours. Ellis's
own ladder reads as a tendency over a stated window -- the same three-hour
figure marine forecasts already quote, plus 12h and 24h -- and gates the
sharper falls on how low the barometer already is, since a 4mb fall means
something different starting from 1030mb than it does from 995mb.

Squash zone and Tropical barometer anomaly, the other two heavy-weather
rules, stay where they are: neither is a rate or tendency rule this ladder
covers.

The set is seeded once per installation, keyed on a marker in the rules
file, the same idiom the other two seed sets use. A rule the operator
deletes stays deleted; there is no migration that removes the three retired
heavy-weather rules automatically -- the operator does that by hand, once,
on the boat.
*/
const lawOfStormsSeedMarker = "law-of-storms-v1"

// These literal IDs give lawOfStormsSeedRules' seven rules independent
// identities before they are ever persisted, the same idiom
// forecastWindWarningRuleID (alarm_seed_forecast_warnings.go) uses: five of
// these seven rules bind only two paths between them (four share
// pressureChange3hPath), and two rules sharing a path also share an empty ID
// unless each carries one of its own. createAlarmRule overwrites these with
// a fresh UUID on disk regardless, so they only matter for code -- like
// TestLawOfStormsEngine_TwelveMillibarRiseRaisesTheUpRulesOnly
// (alarm_engine_test.go) -- that evaluates this raw slice directly.
const (
	lawOfStormsUp6mbRuleID              = "helmcentral:barometer-up-6mb-3h"
	lawOfStormsDown6mbRuleID            = "helmcentral:barometer-down-6mb-3h"
	lawOfStormsUp10mbRuleID             = "helmcentral:barometer-up-10mb-3h"
	lawOfStormsDown10mbRuleID           = "helmcentral:barometer-down-10mb-3h"
	lawOfStormsStormSignatureRuleID     = "helmcentral:storm-signature"
	lawOfStormsSevereThunderstormRuleID = "helmcentral:severe-thunderstorm-signature"
	lawOfStormsWeatherBombRuleID        = "helmcentral:weather-bomb"
)

func lawOfStormsSeedRules() []alarmRule {
	return []alarmRule{
		{
			// The page's "strong wind" tier: a 6mb move in three hours,
			// either direction, is 26-33kt. This is the rise half.
			ID:           lawOfStormsUp6mbRuleID,
			Label:        "Barometer up 6 mb in three hours",
			Enabled:      true,
			Path:         pressureChange3hPath,
			Op:           alarmOpAbove,
			Value:        6 * pascalsPerMillibar,
			Hysteresis:   0.5 * pascalsPerMillibar,
			DwellSeconds: 900,
			State:        alarmStateWarn,
		},
		{
			// The fall half of the same tier.
			ID:           lawOfStormsDown6mbRuleID,
			Label:        "Barometer down 6 mb in three hours",
			Enabled:      true,
			Path:         pressureChange3hPath,
			Op:           alarmOpBelow,
			Value:        -6 * pascalsPerMillibar,
			Hysteresis:   0.5 * pascalsPerMillibar,
			DwellSeconds: 900,
			State:        alarmStateWarn,
		},
		{
			// The page's "gale" tier: a 10mb move in three hours, either
			// direction, is 34-47kt.
			ID:           lawOfStormsUp10mbRuleID,
			Label:        "Barometer up 10 mb in three hours",
			Enabled:      true,
			Path:         pressureChange3hPath,
			Op:           alarmOpAbove,
			Value:        10 * pascalsPerMillibar,
			Hysteresis:   0.5 * pascalsPerMillibar,
			DwellSeconds: 900,
			State:        alarmStateAlarm,
		},
		{
			// The fall half of the same tier.
			ID:           lawOfStormsDown10mbRuleID,
			Label:        "Barometer down 10 mb in three hours",
			Enabled:      true,
			Path:         pressureChange3hPath,
			Op:           alarmOpBelow,
			Value:        -10 * pascalsPerMillibar,
			Hysteresis:   0.5 * pascalsPerMillibar,
			DwellSeconds: 900,
			State:        alarmStateAlarm,
		},
		{
			// The storm/thunderstorm tier: a 4mb fall in three hours with
			// the barometer already under 1009mb (stormSignature,
			// weather_trend.go). Ships disabled: the source page itself
			// hedges this tier ("3mb is the minimum, 4 a margin of
			// comfort"), and a 4mb fall under 1009mb is routine on a
			// temperate coast rather than a reliable storm signature there.
			ID:           lawOfStormsStormSignatureRuleID,
			Label:        "Storm signature",
			Enabled:      false,
			Path:         stormIndexPath,
			Op:           alarmOpAbove,
			Value:        0.5,
			DwellSeconds: 1800,
			State:        alarmStateAlert,
		},
		{
			// The severe-thunderstorm tier: the same 4mb/3h fall plus an
			// 8mb/12h fall, both under 1005mb (severeThunderstormSignature).
			// Unlike the plain storm tier, the page does not hedge this one,
			// so it ships live.
			ID:           lawOfStormsSevereThunderstormRuleID,
			Label:        "Severe thunderstorm signature",
			Enabled:      true,
			Path:         severeThunderstormIndexPath,
			Op:           alarmOpAbove,
			Value:        0.5,
			DwellSeconds: 900,
			State:        alarmStateAlarm,
		},
		{
			// The weather-bomb tier: a 24mb fall in 24 hours, the page's
			// figure (defined at 60 degrees latitude; see the ADR for why a
			// latitude-scaled threshold is not implemented here).
			ID:           lawOfStormsWeatherBombRuleID,
			Label:        "Weather bomb",
			Enabled:      true,
			Path:         pressureChange24hPath,
			Op:           alarmOpBelow,
			Value:        -24 * pascalsPerMillibar,
			Hysteresis:   1 * pascalsPerMillibar,
			DwellSeconds: 1800,
			State:        alarmStateEmergency,
		},
	}
}

// seedLawOfStormsRules creates the set once, then records that it has been
// done. Safe to call on every boot, mirroring seedHeavyWeatherRules and
// seedForecastWarningsRules.
func seedLawOfStormsRules() error {
	alarmRulesMu.RLock()
	for _, marker := range alarmRulesSeededSets {
		if marker == lawOfStormsSeedMarker {
			alarmRulesMu.RUnlock()
			return nil
		}
	}
	alarmRulesMu.RUnlock()

	for _, rule := range lawOfStormsSeedRules() {
		// Unlike the heavy-weather set (forced disabled) and the
		// forecast-warnings set (forced enabled), this loop does not
		// overwrite rule.Enabled: lawOfStormsSeedRules already sets each
		// rule's own Enabled correctly, and the whole point of this set is
		// that it is not one uniform confidence level -- six of the seven
		// rules ship live and the storm-signature rule alone ships off.
		if _, err := createAlarmRule(rule); err != nil {
			return fmt.Errorf("seeding law-of-storms rule %q: %w", rule.Label, err)
		}
	}

	alarmRulesMu.Lock()
	alarmRulesSeededSets = append(alarmRulesSeededSets, lawOfStormsSeedMarker)
	err := saveAlarmRulesLocked()
	alarmRulesMu.Unlock()
	if err != nil {
		return fmt.Errorf("recording the law-of-storms seed marker: %w", err)
	}
	return nil
}
