package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// list_alarm_rules (ADR 0225): every alarm rule the boat is evaluating, so
// Mate edits an existing rule rather than proposing a duplicate. Stored rules
// can be proposed as alarm_rule changes; derived ones (a tank's levels, a
// gauge zone) are listed read-only with where to change them. Values are in
// the operator's unit for the path, the same unit a proposal takes.

type assistantAlarmRuleEntry struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Path        string   `json:"path"`
	Op          string   `json:"op"`
	Value       float64  `json:"value"`
	ClearValue  *float64 `json:"clear_value,omitempty"`
	AlarmLevel  *float64 `json:"alarm_level,omitempty"`
	DisplayUnit string   `json:"display_unit,omitempty"`
	State       string   `json:"state"`
	DwellSecs   int      `json:"dwell_seconds"`
	StaleSecs   int      `json:"stale_after_seconds,omitempty"`
	Enabled     bool     `json:"enabled"`
	Origin      string   `json:"origin"`
	ReadOnly    bool     `json:"read_only"`
	ChangeIt    string   `json:"change_it,omitempty"`
	Version     string   `json:"version,omitempty"`
}

type assistantListAlarmRulesResult struct {
	Rules []assistantAlarmRuleEntry `json:"rules"`
	Total int                       `json:"total"`
}

func alarmRuleListEntry(r alarmRule, derived bool) assistantAlarmRuleEntry {
	d, _ := alarmDisplayFor(r.Path, r.PathRole)
	e := assistantAlarmRuleEntry{
		ID: r.ID, Label: r.Label, Path: r.Path, Op: r.Op, Value: d.toShown(r.Value), DisplayUnit: d.label,
		State: r.State, DwellSecs: r.DwellSeconds, StaleSecs: r.StaleAfterSeconds, Enabled: r.Enabled, Origin: "own",
	}
	if c := alarmClearValueFor(r); c != nil {
		v := d.toShown(*c)
		e.ClearValue = &v
	}
	if r.AlarmLevelValue != nil {
		v := d.toShown(*r.AlarmLevelValue)
		e.AlarmLevel = &v
	}
	switch {
	case derived && isTankDerivedAlarmRuleID(r.ID):
		e.Origin, e.ReadOnly = "tank levels", true
		e.ChangeIt = "set tank_warn_percent, tank_alarm_percent or tank_alarms_off on the tank's equipment record"
	case derived:
		e.Origin, e.ReadOnly = "gauge zone", true
		e.ChangeIt = "the operator edits the zone on the gauge; Mate cannot change it"
	default:
		if r.SeededBy != "" {
			e.Origin = "built-in"
		}
		e.Version = alarmRuleVersion(r)
	}
	return e
}

func (d assistantToolDeps) executeListAlarmRules(ctx context.Context, _ json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if d.documents == nil || d.documents() == nil {
		return "", errors.New("list_alarm_rules: the alarm rules are not available")
	}
	var stored []alarmRule
	if err := d.documents().Read(func(q sqlQueryer) error {
		var err error
		stored, err = listAlarmRulesQ(q)
		return err
	}); err != nil {
		return "", fmt.Errorf("list_alarm_rules: %w", err)
	}
	result := assistantListAlarmRulesResult{Rules: []assistantAlarmRuleEntry{}}
	for _, r := range stored {
		result.Rules = append(result.Rules, alarmRuleListEntry(r, false))
	}
	for _, r := range derivedAlarmRules() {
		result.Rules = append(result.Rules, alarmRuleListEntry(r, true))
	}
	result.Total = len(result.Rules)
	return capToolResultJSON(&result, func() bool {
		n := len(result.Rules)
		if n <= 10 {
			return false
		}
		result.Rules = result.Rules[:n-n/4]
		return true
	})
}

var mateToolListAlarmRules = mateTool{
	name: "list_alarm_rules",
	function: openRouterFunctionDef{
		Description: "List every alarm rule the boat is watching: id, label, path, op (above, below, equal, notEqual, stale), " +
			"value and clear_value in the operator's unit (display_unit), severity (state), dwell_seconds, enabled, and origin " +
			"(own, built-in, or derived from tank levels or a gauge zone). Derived rules are read_only and change_it says where " +
			"the change goes. Call it before proposing an alarm, so you edit the rule that already watches the path instead of " +
			"adding a second one, and so you can say what the boat already has when the operator asks what is monitored.",
		Parameters: json.RawMessage(`{"type": "object", "properties": {}}`),
	},
	execute: plainTool(assistantToolDeps.executeListAlarmRules),
	describe: func(args json.RawMessage) string {
		return "Checking the alarm rules…"
	},
}
