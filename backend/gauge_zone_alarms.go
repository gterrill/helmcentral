package main

import (
	"fmt"
	"sort"
	"strings"
)

// Gauge zones as the alarm source (ADR 0050).
//
// Before this, a gauge rendering a red band raised nothing and an alarm firing
// coloured no gauge — two threshold systems, neither aware of the other. An
// operator setting an engine-temperature red band reasonably expects that to
// be the alarm, which is how N2KView behaves and how the three-level scan
// (glance at colour, read the gauge, check history) is meant to work.
//
// Rules are derived at evaluation time rather than materialised into
// alarm-rules.json, following gaugeBoundPaths(): the backend already owns the
// page config, so there is nothing to keep in sync and no migration when a
// zone is edited.

const zoneDerivedAlarmRuleIDPrefix = "zone:"

const (
	// A value hovering at a threshold is the reason people switch marine
	// alarms off entirely. A zone carries no opinion about either of these, so
	// both get a defensible default rather than zero.
	zoneDerivedDwellSeconds = 10
	// As a fraction of the gauge's own range, so it scales with the scale.
	zoneDerivedHysteresisFraction = 0.02
)

func isZoneDerivedAlarmRuleID(id string) bool {
	return strings.HasPrefix(id, zoneDerivedAlarmRuleIDPrefix)
}

// zoneAlarmThreshold turns a band into the single threshold an alarmRule
// carries. A band touching the bottom of the scale fires as the value falls
// past its upper edge; one touching the top fires as it rises past its lower
// edge. A band floating in the middle has no single-threshold equivalent —
// reported here as not-ok, and rejected at save time so it can never be the
// silent reason an alarm does not fire.
func zoneAlarmThreshold(zone gaugeZone, min, max float64) (op string, threshold float64, ok bool) {
	// Touching within a hair of the end counts as touching it: an operator
	// dragging a band to the end of a 0-3000 scale should not have to land on
	// exactly 0.
	epsilon := (max - min) * 0.001

	atBottom := zone.From <= min+epsilon
	atTop := zone.To >= max-epsilon

	switch {
	case atBottom && atTop:
		// The band covers the whole scale, so it is always in force. That is
		// not a threshold, it is a mistake.
		return "", 0, false
	case atBottom:
		return alarmOpBelow, zone.To, true
	case atTop:
		return alarmOpAbove, zone.From, true
	default:
		return "", 0, false
	}
}

// gaugeZoneRule builds one derived rule, or reports why it cannot.
func gaugeZoneRule(id string, gauge dashboardGaugeConfig, zone gaugeZone, zoneIndex int) (alarmRule, error) {
	if zone.State == alarmStateNormal {
		return alarmRule{}, fmt.Errorf("normal bands are not alarms")
	}

	min, max := 0.0, 100.0
	if gauge.Min != nil {
		min = *gauge.Min
	}
	if gauge.Max != nil {
		max = *gauge.Max
	}
	if max <= min {
		return alarmRule{}, fmt.Errorf("gauge range is not usable")
	}

	op, threshold, ok := zoneAlarmThreshold(zone, min, max)
	if !ok {
		return alarmRule{}, fmt.Errorf("zone %d is not anchored to either end of the scale", zoneIndex+1)
	}

	// Zones are authored in the display unit; alarmReader reads SI.
	value, err := convertToSI(threshold, gauge.Quantity, gauge.Unit)
	if err != nil {
		return alarmRule{}, err
	}
	hysteresisSI, err := zoneHysteresisSI(gauge, min, max)
	if err != nil {
		return alarmRule{}, err
	}

	label := strings.TrimSpace(gauge.Label)
	if label == "" {
		label = gauge.Path
	}

	return alarmRule{
		ID:           id,
		Enabled:      true,
		Path:         strings.TrimSpace(gauge.Path),
		Label:        label,
		Op:           op,
		Value:        value,
		Hysteresis:   hysteresisSI,
		DwellSeconds: zoneDerivedDwellSeconds,
		State:        zone.State,
		Methods:      []string{"visual", "sound"},
	}, nil
}

// zoneHysteresisSI expresses the deadband in SI. It is a span, not a point, so
// it is the difference between two converted values rather than one converted
// value — the distinction matters for temperature, where the scales are offset
// (2 °C of deadband is 2 K, not 275.15 K).
func zoneHysteresisSI(gauge dashboardGaugeConfig, min, max float64) (float64, error) {
	span := (max - min) * zoneDerivedHysteresisFraction

	zero, err := convertToSI(min, gauge.Quantity, gauge.Unit)
	if err != nil {
		return 0, err
	}
	offset, err := convertToSI(min+span, gauge.Quantity, gauge.Unit)
	if err != nil {
		return 0, err
	}

	hysteresis := offset - zero
	if hysteresis < 0 {
		hysteresis = -hysteresis
	}
	return hysteresis, nil
}

// zoneDerivedAlarmRules walks every gauge on every page — standalone widgets
// and gauge-group members alike — and returns one rule per alarm-severity zone.
//
// Zones that cannot become a rule are skipped rather than reported here:
// validateGaugeConfig rejects them at save time, so anything that reaches this
// point and still fails is a config written before that rule existed.
func zoneDerivedAlarmRules() []alarmRule {
	dashboardPagesMu.RLock()
	defer dashboardPagesMu.RUnlock()

	var rules []alarmRule
	collect := func(widgetID string, gaugeIndex int, gauge dashboardGaugeConfig) {
		for zoneIndex, zone := range gauge.Zones {
			id := fmt.Sprintf("%s%s:%d:%d", zoneDerivedAlarmRuleIDPrefix, widgetID, gaugeIndex, zoneIndex)
			rule, err := gaugeZoneRule(id, gauge, zone, zoneIndex)
			if err != nil {
				continue
			}
			rules = append(rules, rule)
		}
	}

	for _, page := range dashboardPagesState {
		for _, widget := range page.Widgets {
			if widget.Gauge != nil {
				collect(widget.ID, 0, *widget.Gauge)
			}
			if widget.GaugeGroup != nil {
				for i, gauge := range widget.GaugeGroup.Gauges {
					collect(widget.ID, i, gauge)
				}
			}
		}
	}

	// Stable order so the rule list does not reshuffle between ticks; ids are
	// unique by construction.
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	return rules
}
