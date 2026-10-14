package main

import (
	"context"
	"encoding/json"
	"fmt"
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

func runActiveAlarms(t *testing.T, deps assistantToolDeps) string {
	t.Helper()
	out, err := deps.execute(context.Background(), "get_active_alarms", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	return out
}

func TestGetActiveAlarms_ShrinksSensorsBeforeAlarms(t *testing.T) {
	a := sensorHealthAlarmForTest()
	a.Sensors, a.LiveSensors = nil, nil
	for i := 0; i < 60; i++ {
		e := sensorHealthEntry{
			Identifier: fmt.Sprintf("electrical.batteries.%d.voltage", i+1),
			Name:       fmt.Sprintf("Sensor %d", i+1), Quantity: "voltage",
			Text: strings.Repeat("reading text ", 12),
		}
		a.Sensors = append(a.Sensors, e)
		a.LiveSensors = append(a.LiveSensors, e)
	}
	out := runActiveAlarms(t, assistantToolDeps{activeAlarms: func() []alarmStatus { return []alarmStatus{a} }})
	var got struct {
		Count     int  `json:"count"`
		Shown     int  `json:"shown"`
		Truncated bool `json:"truncated"`
		Alarms    []struct {
			RuleID  string                 `json:"rule_id"`
			Sensors []assistantAlarmSensor `json:"sensors"`
		} `json:"alarms"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Alarms) != 1 || got.Alarms[0].RuleID != "anomaly-out-of-range" {
		t.Fatalf("want the one alarm kept, got %s", out)
	}
	if len(got.Alarms[0].Sensors) == 0 || got.Alarms[0].Sensors[0].Identifier == "" {
		t.Fatalf("want at least one identifier kept, got %s", out)
	}
	if !got.Truncated || got.Count != 1 || got.Shown != 1 {
		t.Fatalf("want truncated, count 1, shown 1; got %s", out[:200])
	}
}

func TestGetActiveAlarms_TruncatedKeepsTotalCount(t *testing.T) {
	var list []alarmStatus
	for i := 0; i < 40; i++ {
		a := sensorHealthAlarmForTest()
		a.RuleID = fmt.Sprintf("rule-%d", i)
		a.Message = strings.Repeat("long message ", 60)
		list = append(list, a)
	}
	out := runActiveAlarms(t, assistantToolDeps{activeAlarms: func() []alarmStatus { return list }})
	var got struct {
		Count     int               `json:"count"`
		Shown     int               `json:"shown"`
		Truncated bool              `json:"truncated"`
		Alarms    []json.RawMessage `json:"alarms"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.Truncated || got.Count != 40 || got.Shown != len(got.Alarms) || got.Shown >= 40 || got.Shown < 1 {
		t.Fatalf("count/shown/truncated wrong: %d %d %v %d", got.Count, got.Shown, got.Truncated, len(got.Alarms))
	}
}

func TestGetActiveAlarms_NotTruncatedOmitsFlag(t *testing.T) {
	out := runActiveAlarms(t, assistantToolDeps{activeAlarms: func() []alarmStatus { return []alarmStatus{sensorHealthAlarmForTest()} }})
	if strings.Contains(out, "truncated") || strings.Contains(out, `"shown"`) {
		t.Errorf("unexpected truncation fields: %s", out)
	}
}

func TestGetActiveAlarms_ValueOnlyForRuleReadings(t *testing.T) {
	thr := 12.0
	clear := 12.5
	rule := alarmStatus{RuleID: "r1", Label: "House Bank Low", Path: "electrical.batteries.1.voltage",
		Phase: alarmPhaseActive, State: alarmStateAlert, Value: 11.8, Op: "below", Threshold: &thr,
		ClearValue: &clear, Unit: "V"}
	bus := alarmStatus{RuleID: "bus1", Label: "Anchor Dragging", Phase: alarmPhaseActive, State: alarmStateAlert,
		Encounter: "Crossing, give way", RadarTarget: &alarmRadarTarget{BearingRad: 1, RangeM: 2000}}
	out := runActiveAlarms(t, assistantToolDeps{activeAlarms: func() []alarmStatus { return []alarmStatus{rule, bus} }})
	var got struct {
		Alarms []map[string]any `json:"alarms"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v, ok := got.Alarms[0]["value"]; !ok || v.(float64) != 11.8 {
		t.Errorf("rule alarm value missing: %v", got.Alarms[0])
	}
	if got.Alarms[0]["clear_value"] != 12.5 {
		t.Errorf("clear_value missing: %v", got.Alarms[0])
	}
	if _, ok := got.Alarms[1]["value"]; ok {
		t.Errorf("bus alarm must not carry value: %v", got.Alarms[1])
	}
	if got.Alarms[1]["encounter"] != "Crossing, give way" || got.Alarms[1]["radar_target"] == nil {
		t.Errorf("encounter/radar_target missing: %v", got.Alarms[1])
	}
}

func TestGetActiveAlarms_ZeroThresholdKeepsZeroValue(t *testing.T) {
	thr := 0.0
	a := alarmStatus{RuleID: "r0", Phase: alarmPhaseActive, State: alarmStateAlert, Op: "above", Threshold: &thr, Value: 0}
	out := runActiveAlarms(t, assistantToolDeps{activeAlarms: func() []alarmStatus { return []alarmStatus{a} }})
	if !strings.Contains(out, `"value":0`) {
		t.Errorf("rule reading of 0 must be kept: %s", out)
	}
}

func TestGetActiveAlarms_TimesInBoatLocalTime(t *testing.T) {
	a := sensorHealthAlarmForTest()
	a.AckedAt = time.Date(2026, 10, 14, 2, 30, 0, 0, time.UTC)
	deps := assistantToolDeps{
		activeAlarms: func() []alarmStatus { return []alarmStatus{a} },
		vesselState: func() (vesselStateData, error) {
			return vesselStateData{Latitude: -27.5, Longitude: 150}, nil
		},
	}
	out := runActiveAlarms(t, deps)
	var got struct {
		Timezone string `json:"timezone"`
		Alarms   []struct {
			RaisedAt string `json:"raised_at"`
			AckedAt  string `json:"acked_at"`
		} `json:"alarms"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Alarms[0].RaisedAt != "Wed 14 Oct 11:02" || got.Alarms[0].AckedAt != "Wed 14 Oct 12:30" {
		t.Errorf("want UTC+10 local times, got %+v", got.Alarms[0])
	}
	if got.Timezone != "UTC+10" {
		t.Errorf("timezone = %q", got.Timezone)
	}
}

func TestGetActiveAlarms_TimesFallBackToUTCLabelled(t *testing.T) {
	deps := assistantToolDeps{activeAlarms: func() []alarmStatus { return []alarmStatus{sensorHealthAlarmForTest()} }}
	out := runActiveAlarms(t, deps)
	if !strings.Contains(out, `"timezone":"UTC"`) || !strings.Contains(out, "Wed 14 Oct 01:02") {
		t.Errorf("want labelled UTC times: %s", out)
	}
}

func TestBuildAssistantSystemPrompt_AlarmRuleIdentifiers(t *testing.T) {
	prompt := buildAssistantSystemPrompt(basePromptContext())
	for _, want := range []string{"$source", "YachtDevices.47", "`source` filter"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if strings.Contains(prompt, "2c1b") {
		t.Error("prompt should not carry the out-of-order label")
	}
}
