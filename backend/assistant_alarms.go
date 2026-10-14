package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// get_active_alarms (ADR 0175) hands Mate the alarm list the card and banner
// are built from. The card text names a device; the alarm itself carries the
// SignalK path behind it, which is what Mate must investigate.

type assistantAlarmSensor struct {
	Identifier string `json:"identifier"`
	Name       string `json:"name,omitempty"`
	Quantity   string `json:"quantity,omitempty"`
	Text       string `json:"text,omitempty"`
}

type assistantAlarm struct {
	Label        string                 `json:"label"`
	Message      string                 `json:"message"`
	State        string                 `json:"state"`
	Phase        string                 `json:"phase"`
	RaisedAt     string                 `json:"raised_at,omitempty"`
	AckedAt      string                 `json:"acked_at,omitempty"`
	Silenced     bool                   `json:"silenced"`
	RuleID       string                 `json:"rule_id"`
	Path         string                 `json:"path"`
	Value        *float64               `json:"value,omitempty"`
	Unit         string                 `json:"unit,omitempty"`
	Op           string                 `json:"op,omitempty"`
	Threshold    *float64               `json:"threshold,omitempty"`
	ClearValue   *float64               `json:"clear_value,omitempty"`
	Encounter    string                 `json:"encounter,omitempty"`
	RadarTarget  *alarmRadarTarget      `json:"radar_target,omitempty"`
	Evidence     string                 `json:"evidence,omitempty"`
	LiveEvidence string                 `json:"live_evidence,omitempty"`
	Sensors      []assistantAlarmSensor `json:"sensors,omitempty"`
	LiveSensors  []assistantAlarmSensor `json:"live_sensors,omitempty"`
}

type assistantActiveAlarmsResult struct {
	Count     int              `json:"count"`
	Shown     int              `json:"shown,omitempty"`
	Truncated bool             `json:"truncated,omitempty"`
	Timezone  string           `json:"timezone"`
	Alarms    []assistantAlarm `json:"alarms"`
}

func assistantAlarmSensors(in []sensorHealthEntry) []assistantAlarmSensor {
	var out []assistantAlarmSensor
	for _, e := range in {
		out = append(out, assistantAlarmSensor{Identifier: e.Identifier, Name: e.Name, Quantity: e.Quantity, Text: e.Text})
	}
	return out
}

func (d assistantToolDeps) executeGetActiveAlarms(ctx context.Context, _ json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if d.activeAlarms == nil {
		return "", fmt.Errorf("get_active_alarms: the alarm list is not available")
	}
	loc := time.UTC
	tzLabel := "UTC"
	if d.vesselState != nil {
		if state, verr := d.vesselState(); verr == nil && hasUsableVesselPosition(state.Latitude, state.Longitude) {
			loc = vesselLocalLocation(state.Longitude)
			tzLabel = assistantTimeZoneLabel(state.Longitude)
		}
	}
	result := assistantActiveAlarmsResult{Alarms: make([]assistantAlarm, 0), Timezone: tzLabel}
	for _, a := range d.activeAlarms() {
		item := assistantAlarm{
			Label: a.Label, Message: a.Message, State: a.State, Phase: string(a.Phase),
			Silenced: a.Silenced, RuleID: a.RuleID, Path: a.Path, Unit: a.Unit,
			Op: a.Op, Threshold: a.Threshold, ClearValue: a.ClearValue,
			Encounter: a.Encounter, RadarTarget: a.RadarTarget,
			Evidence: a.Evidence, LiveEvidence: a.LiveEvidence,
			Sensors: assistantAlarmSensors(a.Sensors), LiveSensors: assistantAlarmSensors(a.LiveSensors),
		}
		// A reading belongs to a rule this engine holds. Bus notifications
		// carry no op or threshold, and the sensor-health alarms' value is a
		// bare count; neither has a reading worth quoting.
		if a.Threshold != nil && len(a.Sensors) == 0 && len(a.LiveSensors) == 0 {
			v := a.Value
			item.Value = &v
		}
		if !a.RaisedAt.IsZero() {
			item.RaisedAt = a.RaisedAt.In(loc).Format("Mon 2 Jan 15:04")
		}
		if !a.AckedAt.IsZero() {
			item.AckedAt = a.AckedAt.In(loc).Format("Mon 2 Jan 15:04")
		}
		result.Alarms = append(result.Alarms, item)
	}
	total := len(result.Alarms)
	result.Count = total
	trimmed := false
	out, err := capToolResultJSON(&result, func() bool {
		trimmed = true
		// Stage 1: live_sensors that repeat sensors add nothing.
		for i := range result.Alarms {
			a := &result.Alarms[i]
			if len(a.LiveSensors) > 0 && sameAlarmSensors(a.Sensors, a.LiveSensors) {
				a.LiveSensors = nil
				return true
			}
		}
		// Stage 2: trim the longest sensor list from its end.
		var longest *[]assistantAlarmSensor
		for i := range result.Alarms {
			for _, l := range []*[]assistantAlarmSensor{&result.Alarms[i].Sensors, &result.Alarms[i].LiveSensors} {
				if len(*l) > 1 && (longest == nil || len(*l) > len(*longest)) {
					longest = l
				}
			}
		}
		if longest != nil {
			*longest = (*longest)[:len(*longest)-1]
			return true
		}
		// Stage 3: drop trailing alarms, keeping the worst.
		if len(result.Alarms) > 1 {
			result.Alarms = result.Alarms[:len(result.Alarms)-1]
			return true
		}
		return false
	})
	if err != nil || !trimmed {
		return out, err
	}
	result.Truncated = true
	result.Shown = len(result.Alarms)
	data, err := json.Marshal(&result)
	if err != nil {
		return "", fmt.Errorf("marshal tool result: %w", err)
	}
	return string(data), nil
}

func sameAlarmSensors(a, b []assistantAlarmSensor) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var mateToolGetActiveAlarms = mateTool{
	name: "get_active_alarms",
	function: openRouterFunctionDef{
		Description: "List the alarms active on the boat right now, worst first. Each has its label and " +
			"message, state, when it was raised and acknowledged (boat local time, see timezone), the rule id and " +
			"watched path, and where the rule has them the reading, threshold and clear point; collision alarms add " +
			"the encounter line and radar target figures. Sensor-health alarms (impossible reading, frozen, silent " +
			"source) also list each sensor's real identifier (a SignalK path, or a $source id for a silent source), " +
			"its name and quantity, as it stood when the alarm was raised (sensors) and now (live_sensors). If " +
			"truncated is true, shown of count alarms are listed. Call this FIRST for any question about an " +
			"alarm, then investigate the identifiers it returns, never a path guessed from a device name. An " +
			"empty list means nothing is alarming.",
		Parameters: json.RawMessage(`{"type": "object", "properties": {}}`),
	},
	execute: assistantToolDeps.executeGetActiveAlarms,
	describe: func(json.RawMessage) string {
		return "Checking the active alarms…"
	},
}
