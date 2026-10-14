package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sensorHealthAlarmForTest() alarmStatus {
	entry := sensorHealthEntry{
		Identifier: "electrical.batteries.1.voltage",
		Name:       "PortALT",
		Quantity:   "voltage",
		Label:      "PortALT voltage",
		Text:       "PortALT voltage 72.3 V",
	}
	return alarmStatus{
		RuleID:      "anomaly-out-of-range",
		Label:       "Impossible sensor reading",
		Path:        anomalySensorOutOfRangeCountPath,
		Phase:       alarmPhaseActive,
		State:       alarmStateAlert,
		Value:       1,
		Message:     "Impossible sensor reading: PortALT voltage 72.3 V",
		Sensors:     []sensorHealthEntry{entry},
		LiveSensors: []sensorHealthEntry{entry},
		RaisedAt:    time.Date(2026, 10, 14, 1, 2, 3, 0, time.UTC),
	}
}

func TestGetActiveAlarms_ReturnsSensorIdentifiers(t *testing.T) {
	deps := assistantToolDeps{activeAlarms: func() []alarmStatus {
		return []alarmStatus{sensorHealthAlarmForTest()}
	}}
	out, err := deps.execute(context.Background(), "get_active_alarms", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, want := range []string{
		"electrical.batteries.1.voltage", "PortALT", "anomaly-out-of-range",
		"Impossible sensor reading: PortALT voltage 72.3 V", "raised_at", "live_sensors", "sensors",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestGetActiveAlarms_EmptyIsAnAnswer(t *testing.T) {
	deps := assistantToolDeps{activeAlarms: func() []alarmStatus { return nil }}
	out, err := deps.execute(context.Background(), "get_active_alarms", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var got struct {
		Count  int               `json:"count"`
		Alarms []json.RawMessage `json:"alarms"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if got.Count != 0 || got.Alarms == nil || len(got.Alarms) != 0 {
		t.Fatalf("want count 0 and an empty list, got %s", out)
	}
}

func TestGetActiveAlarms_UnwiredFailsFast(t *testing.T) {
	if _, err := (assistantToolDeps{}).execute(context.Background(), "get_active_alarms", json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected an error when the alarm source is not wired")
	}
}

func TestBuildAssistantSystemPrompt_AlarmRule(t *testing.T) {
	prompt := buildAssistantSystemPrompt(basePromptContext())
	for _, want := range []string{"get_active_alarms", "never a path guessed from the device name", "value at raise"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}
