package main

import (
	"fmt"
	"strings"
)

/*
The anomaly-detection rule set (anomaly-v1): sensor health, full-bank
charging and twin/N-way engine differentials, folded into ordinary seeded
alarmRules the same way every other detector in this codebase reaches the
alarm centre (ADR 0055's derived-path-plus-seeded-rule pattern).

Unlike every earlier seeded set (heavy weather, forecast warnings, Law of
Storms), this one is not one marker but several: impossible-reading and
silent-source need no vessel setup at all, so every install gets them
unconditionally; frozen needs at least one configured engine; full-bank
charging needs a complete house bank (a linked battery profile with its
full_soc and charge_warn slots filled); and each engine differential
residual pair needs that engine actually configured. Each sub-set is keyed
on its own marker (anomalyBaseSeedMarker, "anomaly-v1:frozen",
"anomaly-v1:battery", "anomaly-v1:engine:<name>") so seedAnomalyRules is
safe to call repeatedly -- at startup, and again on every vessel-settings
save -- and a detector that only becomes configured later (an engine added
after the fact) gets its rules exactly then, without re-touching anything
already seeded.

Twin residual thresholds ship disabled ("Disabled until tuned" -- the plan's
own operator decision: "Twin alarms stay up until a steady run proves them
clear"), the same reasoning the heavy-weather set uses for its own
uncalibrated numbers: a boat-specific offset has not yet been learned, so
offering these live would assert a confidence about a bias that might not
exist. Transmission oilPressure/oilTemperature carry no seeded pair at all
-- the plan sets their bands from the learned MAD after the first baseline,
not a fixed number, which is a different (later) piece of work.
*/

const anomalyBaseSeedMarker = "anomaly-v1"

func anomalyFrozenSeedMarker() string  { return "anomaly-v1:frozen" }
func anomalyBatterySeedMarker() string { return "anomaly-v1:battery" }

// anomalyEngineSeedMarker is keyed on the engine's own Signal K instance id
// (e.g. "port"), never on the operator's editable display name -- a marker
// built from the display name would look like a brand new engine the
// instant the operator renamed one, reseeding a second set of residual
// rules under the new name and leaving the first set orphaned, bound to a
// path nothing publishes to any more (code review finding 6).
func anomalyEngineSeedMarker(instance string) string {
	return "anomaly-v1:engine:" + strings.ToLower(strings.TrimSpace(instance))
}

const (
	anomalyImpossibleRuleID    = "helmcentral:anomaly-impossible-reading"
	anomalySilentSourceRuleID  = "helmcentral:anomaly-silent-source"
	anomalyFrozenRuleID        = "helmcentral:anomaly-frozen-sensor"
	anomalyFullBankWarnRuleID  = "helmcentral:anomaly-full-bank-charging-warn"
	anomalyFullBankAlarmRuleID = "helmcentral:anomaly-full-bank-charging-near-trip"
)

// anomalyBaseSeedRules: impossible-reading and silent-source, which need no
// vessel setup -- every install gets these.
func anomalyBaseSeedRules() []alarmRule {
	return []alarmRule{
		{
			ID: anomalyImpossibleRuleID, Label: "Impossible sensor reading", Enabled: true,
			Path: anomalySensorOutOfRangeCountPath, Op: alarmOpAbove, Value: 0.5,
			DwellSeconds: 30, State: alarmStateAlert,
		},
		{
			ID: anomalySilentSourceRuleID, Label: "Silent sensor source", Enabled: true,
			Path: anomalySensorSilentSourceCountPath, Op: alarmOpAbove, Value: 0.5,
			DwellSeconds: 60, State: alarmStateWarn,
		},
	}
}

// anomalyFrozenSeedRules needs at least one configured engine.
func anomalyFrozenSeedRules() []alarmRule {
	return []alarmRule{
		{
			ID: anomalyFrozenRuleID, Label: "Frozen sensor reading", Enabled: true,
			Path: anomalySensorFrozenCountPath, Op: alarmOpAbove, Value: 0.5,
			DwellSeconds: 60, State: alarmStateAlert,
		},
	}
}

// anomalyBatterySeedRules needs a complete house bank (vesselHouseBankReady).
// The warn tier ships enabled -- it is the 2026-09-21 dead-ship rule itself,
// the whole reason this cycle exists -- while the near-trip tier ships
// disabled until the operator has watched it agree with a real charge cycle.
func anomalyBatterySeedRules() []alarmRule {
	return []alarmRule{
		{
			ID: anomalyFullBankWarnRuleID, Label: "Charging into a full house bank", Enabled: true,
			Path: anomalyBatteryFullBankChargingPath, Op: alarmOpAbove, Value: 0.5,
			DwellSeconds: 20, State: alarmStateWarn,
		},
		{
			ID: anomalyFullBankAlarmRuleID, Label: "House bank overcharge risk", Enabled: false,
			Path: anomalyBatteryFullBankChargingPath, Op: alarmOpAbove, Value: 1.5,
			DwellSeconds: 10, State: alarmStateAlarm,
		},
	}
}

// anomalyResidualSeed is one of the four twin-residual quantities that gets
// a seeded (disabled) alert pair. HighLabel/LowLabel are written from this
// engine's own point of view ("Port running hotter than usual" /
// "Port running cooler than usual") -- for the two-engine case the low side
// is exactly the other engine running hotter, but the wording stays
// engine-relative rather than trying to name a specific peer, since for
// three or more engines a low residual has no single "other side" to name
// (each engine is compared against the median of the rest).
var anomalyResidualSeeds = []struct {
	Suffix      string
	IDSuffix    string
	ThresholdSI float64 // in the residual path's raw SI unit (Pa, K-equivalent, or ratio)
	HighLabel   string
	LowLabel    string
}{
	{"temperature", "coolant-temperature", 4, "running hotter than usual", "running cooler than usual"},
	{"oilPressure", "oil-pressure", 50000, "oil pressure running high", "oil pressure running low"},
	{"boostPressure", "boost-pressure", 15000, "boost pressure running high", "boost pressure running low"},
	{"engineLoad", "engine-load", 0.08, "engine load running high", "engine load running low"},
}

// anomalyEngineSeedRules builds the signed pair of alert rules for each of
// the four seeded quantities, bound to instance's own residual paths.
// displayName is used only for the rule Label text -- the path, and the
// rule ID's own slug, follow instance so a later rename of the engine's
// display name neither moves the path out from under these rules nor
// produces a fresh set of IDs for the same physical engine.
func anomalyEngineSeedRules(instance, displayName string) []alarmRule {
	rules := make([]alarmRule, 0, len(anomalyResidualSeeds)*2)
	slug := strings.ToLower(strings.TrimSpace(instance))
	for _, seed := range anomalyResidualSeeds {
		path := anomalyEngineResidualPath(instance, seed.Suffix)
		rules = append(rules,
			alarmRule{
				ID:           fmt.Sprintf("helmcentral:anomaly-%s-%s-high", seed.IDSuffix, slug),
				Label:        fmt.Sprintf("%s %s", displayName, seed.HighLabel),
				Enabled:      false,
				Path:         path,
				Op:           alarmOpAbove,
				Value:        seed.ThresholdSI,
				DwellSeconds: 60,
				State:        alarmStateAlert,
			},
			alarmRule{
				ID:           fmt.Sprintf("helmcentral:anomaly-%s-%s-low", seed.IDSuffix, slug),
				Label:        fmt.Sprintf("%s %s", displayName, seed.LowLabel),
				Enabled:      false,
				Path:         path,
				Op:           alarmOpBelow,
				Value:        -seed.ThresholdSI,
				DwellSeconds: 60,
				State:        alarmStateAlert,
			},
		)
	}
	return rules
}

// vesselHouseBankReady mirrors computeAnomalyBattery's own gate (anomaly_detector.go):
// a bank is chosen, sized and celled, AND its linked profile is a battery
// profile with full_soc and charge_warn slots actually filled. Seeding an
// alarm rule ahead of that would bind it to a path that can never carry a
// value, which is harmless but pointless -- the plan's "nothing is seeded
// or evaluated for an unconfigured detector".
func vesselHouseBankReady(hb *vesselHouseBankSetting) bool {
	if hb == nil || strings.TrimSpace(hb.Path) == "" || hb.CapacityAh <= 0 || hb.Cells <= 0 {
		return false
	}
	profile, ok := equipmentProfile(hb.EquipmentID)
	if !ok || profile.Kind != profileKindBattery {
		return false
	}
	if profile.FullSOC == nil || profile.FullSOC.Value == nil || *profile.FullSOC.Value <= 0 {
		return false
	}
	_, _, ok = batteryPackThresholds(profile, hb.Cells)
	return ok
}

// enginesToSeedResidualRulesFor is the plan's "seed rules on the first
// engine only so one fault raises one alarm" for exactly two engines (their
// residuals are exact mirror images), versus every engine for three or
// more (each is compared against the median of the rest, so each is
// independently informative).
func enginesToSeedResidualRulesFor(engines []vesselEngineSetting) []vesselEngineSetting {
	switch {
	case len(engines) < 2:
		return nil
	case len(engines) == 2:
		return engines[:1]
	default:
		return engines
	}
}

// seedAnomalySet is the one-marker-at-a-time primitive every sub-set below
// calls: create every rule in rules, then record marker, unless marker is
// already recorded -- mirrors seedForecastWarningsRules' body, generalised
// to a marker/rules pair since this file has several independent sets
// rather than one.
//
// Uses createSeededAlarmRule, not createAlarmRule: every rule this file
// builds already carries its own fixed ID (anomalyImpossibleRuleID and
// friends, or anomalyEngineSeedRules' own per-instance IDs), and that ID
// must survive onto disk rather than being replaced by a fresh UUID --
// otherwise a later cycle has no stable way to refer back to one of these
// rules, and a rename-then-reseed (anomalyEngineSeedMarker is keyed on the
// engine's stable instance id precisely so this can happen) would have no
// way to tell "the same rule, seeded again" from "a genuinely new rule"
// even if it wanted to (code review finding 7).
func seedAnomalySet(marker string, rules []alarmRule) error {
	alarmRulesMu.RLock()
	for _, m := range alarmRulesSeededSets {
		if m == marker {
			alarmRulesMu.RUnlock()
			return nil
		}
	}
	alarmRulesMu.RUnlock()

	for _, rule := range rules {
		if _, err := createSeededAlarmRule(rule); err != nil {
			// A previous seeding attempt for this same marker can fail
			// partway through (a disk-full write, a transient I/O error),
			// leaving some of its rules saved with the marker never
			// recorded -- the next call (the next restart, or the next
			// vessel-settings save) starts this loop over from rule zero
			// and immediately hits createSeededAlarmRule's own "id already
			// in use" fail-fast backstop against the rule(s) that DID save.
			// Treat that specific case -- this rule's own fixed id already
			// exists with exactly the definition seedAnomalySet would have
			// created -- as this rule already being done, not a conflict,
			// so the retry can finish the rest of the set (code review
			// finding 6). An id collision against a genuinely different
			// definition is left as the fatal error it already is.
			if existing, ok := getAlarmRule(rule.ID); ok && seededRuleMatchesExisting(existing, rule) {
				continue
			}
			return fmt.Errorf("seeding %s rule %q: %w", marker, rule.Label, err)
		}
	}

	alarmRulesMu.Lock()
	alarmRulesSeededSets = append(alarmRulesSeededSets, marker)
	err := saveAlarmRulesLocked()
	alarmRulesMu.Unlock()
	if err != nil {
		return fmt.Errorf("recording the %s seed marker: %w", marker, err)
	}
	return nil
}

// seededRuleMatchesExisting reports whether existing (already on disk, found
// under want's own fixed id) is the same seed definition as want, so
// seedAnomalySet can tell "this rule's own retry, already done" apart from
// a genuine id collision with something else entirely. Path and State are
// enough to identify a seeded rule -- the condition it watches and the
// severity it raises are what make it the rule it is; Value/Enabled/
// DwellSeconds are deliberately excluded, since an operator may have
// already tuned a successfully-seeded rule by the time a later rule in the
// same set is retried, and that is not a conflict either.
func seededRuleMatchesExisting(existing, want alarmRule) bool {
	return existing.Path == want.Path && existing.State == want.State
}

// seedAnomalyRules seeds whichever of the anomaly-v1 sub-sets vessel's
// current configuration has completed. Safe to call repeatedly -- at
// startup, and again on every vessel-settings save (main.go and the
// vessel-settings save handler) -- since each sub-set's own marker makes
// every call after the first a no-op for that sub-set.
func seedAnomalyRules(vessel vesselSettings) error {
	if err := seedAnomalySet(anomalyBaseSeedMarker, anomalyBaseSeedRules()); err != nil {
		return err
	}

	if len(vessel.Engines) >= 1 {
		if err := seedAnomalySet(anomalyFrozenSeedMarker(), anomalyFrozenSeedRules()); err != nil {
			return err
		}
	}

	if vesselHouseBankReady(vessel.HouseBank) {
		if err := seedAnomalySet(anomalyBatterySeedMarker(), anomalyBatterySeedRules()); err != nil {
			return err
		}
	}

	for _, e := range enginesToSeedResidualRulesFor(vessel.Engines) {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			name = e.Instance
		}
		if err := seedAnomalySet(anomalyEngineSeedMarker(e.Instance), anomalyEngineSeedRules(e.Instance, name)); err != nil {
			return err
		}
	}

	return nil
}
