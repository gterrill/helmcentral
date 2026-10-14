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
	RaisedAt     *time.Time             `json:"raised_at,omitempty"`
	AckedAt      *time.Time             `json:"acked_at,omitempty"`
	Silenced     bool                   `json:"silenced"`
	RuleID       string                 `json:"rule_id"`
	Path         string                 `json:"path"`
	Value        float64                `json:"value"`
	Unit         string                 `json:"unit,omitempty"`
	Op           string                 `json:"op,omitempty"`
	Threshold    *float64               `json:"threshold,omitempty"`
	Evidence     string                 `json:"evidence,omitempty"`
	LiveEvidence string                 `json:"live_evidence,omitempty"`
	Sensors      []assistantAlarmSensor `json:"sensors,omitempty"`
	LiveSensors  []assistantAlarmSensor `json:"live_sensors,omitempty"`
}

type assistantActiveAlarmsResult struct {
	Count  int              `json:"count"`
	Alarms []assistantAlarm `json:"alarms"`
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
	result := assistantActiveAlarmsResult{Alarms: make([]assistantAlarm, 0)}
	for _, a := range d.activeAlarms() {
		item := assistantAlarm{
			Label: a.Label, Message: a.Message, State: a.State, Phase: string(a.Phase),
			Silenced: a.Silenced, RuleID: a.RuleID, Path: a.Path, Value: a.Value, Unit: a.Unit,
			Op: a.Op, Threshold: a.Threshold, Evidence: a.Evidence, LiveEvidence: a.LiveEvidence,
			Sensors: assistantAlarmSensors(a.Sensors), LiveSensors: assistantAlarmSensors(a.LiveSensors),
		}
		if !a.RaisedAt.IsZero() {
			t := a.RaisedAt.UTC()
			item.RaisedAt = &t
		}
		if !a.AckedAt.IsZero() {
			t := a.AckedAt.UTC()
			item.AckedAt = &t
		}
		result.Alarms = append(result.Alarms, item)
	}
	result.Count = len(result.Alarms)
	return capToolResultJSON(&result, func() bool {
		if len(result.Alarms) <= 1 {
			return false
		}
		result.Alarms = result.Alarms[:len(result.Alarms)-1]
		return true
	})
}

var mateToolGetActiveAlarms = mateTool{
	name: "get_active_alarms",
	function: openRouterFunctionDef{
		Description: "List the alarms active on the boat right now, worst first, exactly as the alarm card " +
			"shows them but with what the card leaves out: the rule id, the watched path, and for sensor-health " +
			"alarms (impossible reading, frozen, silent source) each sensor's real identifier (SignalK path or " +
			"source), its name and quantity, both as it stood when the alarm was raised (sensors) and now " +
			"(live_sensors). Call this FIRST for any question about an alarm, then investigate the identifiers " +
			"it returns, never a path guessed from a device name. An empty list means nothing is alarming.",
		Parameters: json.RawMessage(`{"type": "object", "properties": {}}`),
	},
	execute: assistantToolDeps.executeGetActiveAlarms,
	describe: func(json.RawMessage) string {
		return "Checking the active alarms…"
	},
}
