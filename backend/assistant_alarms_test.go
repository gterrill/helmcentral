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
	out, _, err := deps.execute(context.Background(), "get_active_alarms", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, want := range []string{
		"electrical.batteries.1.voltage", "PortALT", "anomaly-out-of-range",
		"Impossible sensor reading: PortALT voltage 72.3 V", "raised_at", "live_same_as_raised", "sensors",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestGetActiveAlarms_EmptyIsAnAnswer(t *testing.T) {
	deps := assistantToolDeps{activeAlarms: func() []alarmStatus { return nil }}
	out, _, err := deps.execute(context.Background(), "get_active_alarms", json.RawMessage(`{}`))
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
	if _, _, err := (assistantToolDeps{}).execute(context.Background(), "get_active_alarms", json.RawMessage(`{}`)); err == nil {
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

func truncateForLog(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

func runActiveAlarms(t *testing.T, deps assistantToolDeps) string {
	t.Helper()
	out, _, err := deps.execute(context.Background(), "get_active_alarms", json.RawMessage(`{}`))
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
	if got.Truncated || got.Count != 1 || !strings.Contains(out, "sensor_count") {
		t.Fatalf("want a counted list cut and no truncated flag; got %s", truncateForLog(out))
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
	useFreshBoatZone(t)
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
	if got.Timezone != "AEST (UTC+10)" {
		t.Errorf("timezone = %q", got.Timezone)
	}
}

func TestGetActiveAlarms_TimesAreUTCAndSaySoBeforeAnyFix(t *testing.T) {
	useFreshBoatZone(t)
	deps := assistantToolDeps{activeAlarms: func() []alarmStatus { return []alarmStatus{sensorHealthAlarmForTest()} }}
	out := runActiveAlarms(t, deps)
	if !strings.Contains(out, `"timezone":"UTC (no position yet)"`) || !strings.Contains(out, "Wed 14 Oct 01:02") || !strings.Contains(out, "timezone_note") {
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

func manySensors(n int, textRepeat int) []sensorHealthEntry {
	var out []sensorHealthEntry
	for i := 0; i < n; i++ {
		out = append(out, sensorHealthEntry{
			Identifier: fmt.Sprintf("electrical.batteries.%d.voltage", i+1),
			Name:       fmt.Sprintf("Sensor %d", i+1), Quantity: "voltage",
			Text: strings.Repeat("reading text ", textRepeat),
		})
	}
	return out
}

func TestGetActiveAlarms_TrimmedResultStaysUnderCap(t *testing.T) {
	var list []alarmStatus
	for i := 0; i < 40; i++ {
		a := sensorHealthAlarmForTest()
		a.RuleID = fmt.Sprintf("rule-%d", i)
		a.Sensors = manySensors(60, 4)
		a.LiveSensors = manySensors(60, 4)
		list = append(list, a)
	}
	start := time.Now()
	out := runActiveAlarms(t, assistantToolDeps{activeAlarms: func() []alarmStatus { return list }})
	if time.Since(start) > 2*time.Second {
		t.Errorf("shrinking took %v", time.Since(start))
	}
	if len(out) > assistantMaxToolResultChars {
		t.Fatalf("trimmed result is %d chars, cap %d", len(out), assistantMaxToolResultChars)
	}
	if !strings.Contains(out, `"count":40`) {
		t.Errorf("want count 40: %s", truncateForLog(out))
	}
}

func TestGetActiveAlarms_UnshrinkableReturnsFallback(t *testing.T) {
	a := sensorHealthAlarmForTest()
	a.Message = strings.Repeat("x", assistantMaxToolResultChars*2)
	out := runActiveAlarms(t, assistantToolDeps{activeAlarms: func() []alarmStatus { return []alarmStatus{a} }})
	if len(out) > assistantMaxToolResultChars {
		t.Fatalf("want the capped fallback, got %d chars", len(out))
	}
	if !strings.Contains(out, "too large") {
		t.Errorf("want the fallback note: %s", truncateForLog(out))
	}
}

func decodeAlarms(t *testing.T, out string) []map[string]any {
	t.Helper()
	var got struct {
		Alarms []map[string]any `json:"alarms"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return got.Alarms
}

func TestGetActiveAlarms_SameLiveSensorsAreMarkedNotDropped(t *testing.T) {
	var list []alarmStatus
	for i := 0; i < 6; i++ {
		a := sensorHealthAlarmForTest()
		a.RuleID = fmt.Sprintf("rule-%d", i)
		a.Sensors = manySensors(20, 12)
		a.LiveSensors = manySensors(20, 12)
		list = append(list, a)
	}
	out := runActiveAlarms(t, assistantToolDeps{activeAlarms: func() []alarmStatus { return list }})
	alarms := decodeAlarms(t, out)
	if len(alarms) == 0 {
		t.Fatal("no alarms")
	}
	for _, a := range alarms {
		_, hasLive := a["live_sensors"]
		same, _ := a["live_same_as_raised"].(bool)
		if hasLive == same {
			t.Errorf("live state must be either listed or marked same, got %v", a)
		}
	}
}

func TestGetActiveAlarms_CutSensorListsAreCounted(t *testing.T) {
	var list []alarmStatus
	for i := 0; i < 6; i++ {
		a := sensorHealthAlarmForTest()
		a.RuleID = fmt.Sprintf("rule-%d", i)
		a.Sensors = manySensors(40, 12)
		a.LiveSensors = manySensors(35, 12)
		list = append(list, a)
	}
	out := runActiveAlarms(t, assistantToolDeps{activeAlarms: func() []alarmStatus { return list }})
	for _, a := range decodeAlarms(t, out) {
		sensors, _ := a["sensors"].([]any)
		if len(sensors) < 40 {
			if a["sensor_count"] != float64(40) {
				t.Errorf("a cut sensors list must carry sensor_count 40: %v", truncateForLog(out))
			}
		}
		if live, ok := a["live_sensors"].([]any); ok && len(live) < 35 {
			if a["live_sensor_count"] != float64(35) {
				t.Errorf("a cut live_sensors list must carry live_sensor_count 35: %v", truncateForLog(out))
			}
		}
	}
}

func TestGetActiveAlarms_InvestigatePathsPerKind(t *testing.T) {
	thr := 12.0
	rule := alarmStatus{RuleID: "r1", Label: "House Bank Low", Path: "electrical.batteries.1.voltage",
		Phase: alarmPhaseActive, State: alarmStateAlert, Op: "below", Threshold: &thr}
	bus := alarmStatus{RuleID: "notifications:propulsion.port.oilPressure", Label: "Oil", Path: "notifications.propulsion.port.oilPressure",
		Phase: alarmPhaseActive, State: alarmStateAlert}
	nodata := alarmStatus{RuleID: "notifications:vebus.lowBattery", Label: "Low", Path: "notifications.vebus.lowBattery",
		Phase: alarmPhaseActive, State: alarmStateAlert}
	health := sensorHealthAlarmForTest()
	health.LiveSensors = []sensorHealthEntry{
		{Identifier: "electrical.batteries.1.voltage"},
		{Identifier: "YachtDevices.47"},
	}
	deps := assistantToolDeps{
		activeAlarms: func() []alarmStatus { return []alarmStatus{rule, bus, nodata, health} },
		pathHasData:  func(p string) bool { return p == "propulsion.port.oilPressure" },
	}
	alarms := decodeAlarms(t, runActiveAlarms(t, deps))
	want := [][]any{
		{"electrical.batteries.1.voltage"},
		{"propulsion.port.oilPressure"},
		nil,
		{"electrical.batteries.1.voltage", "YachtDevices.47"},
	}
	for i, w := range want {
		got, _ := alarms[i]["investigate"].([]any)
		if fmt.Sprint(got) != fmt.Sprint(w) {
			t.Errorf("alarm %d investigate = %v, want %v", i, got, w)
		}
	}
}

func TestGetActiveAlarms_SensorHealthCountIsNeverAReading(t *testing.T) {
	thr := 0.0
	a := sensorHealthAlarmForTest()
	a.Sensors, a.LiveSensors = nil, nil
	a.Op, a.Threshold, a.Value = "above", &thr, 3
	out := runActiveAlarms(t, assistantToolDeps{activeAlarms: func() []alarmStatus { return []alarmStatus{a} }})
	if _, ok := decodeAlarms(t, out)[0]["value"]; ok {
		t.Errorf("a sensor-health count must not be quoted as a reading: %s", out)
	}
}

func TestGetActiveAlarms_NoVesselFetchWithoutTimes(t *testing.T) {
	calls := 0
	a := sensorHealthAlarmForTest()
	a.RaisedAt = time.Time{}
	deps := assistantToolDeps{
		activeAlarms: func() []alarmStatus { return []alarmStatus{a} },
		vesselState:  func() (vesselStateData, error) { calls++; return vesselStateData{}, nil },
	}
	runActiveAlarms(t, deps)
	if calls != 0 {
		t.Errorf("vesselState called %d times with no timestamps to format", calls)
	}
}

func TestGetActiveAlarms_ALostVesselReadKeepsTheLastFixZone(t *testing.T) {
	useFreshBoatZone(t)
	boatTimeAt(-27.5, 150, true)
	deps := assistantToolDeps{
		activeAlarms: func() []alarmStatus { return []alarmStatus{sensorHealthAlarmForTest()} },
		vesselState:  func() (vesselStateData, error) { return vesselStateData{}, fmt.Errorf("signalk down") },
	}
	out := runActiveAlarms(t, deps)
	if !strings.Contains(out, `"timezone":"AEST (UTC+10, last fix)"`) || !strings.Contains(out, "Wed 14 Oct 11:02") || strings.Contains(out, "timezone_note") {
		t.Errorf("want the last fix's zone, labelled: %s", out)
	}
}

func TestGetActiveAlarms_TimezoneErrorIsSurfaced(t *testing.T) {
	useFreshBoatZone(t)
	deps := assistantToolDeps{
		activeAlarms: func() []alarmStatus { return []alarmStatus{sensorHealthAlarmForTest()} },
		vesselState:  func() (vesselStateData, error) { return vesselStateData{}, fmt.Errorf("signalk down") },
	}
	out := runActiveAlarms(t, deps)
	if !strings.Contains(out, `"timezone":"UTC (no position yet)"`) || !strings.Contains(out, "timezone_note") || !strings.Contains(out, "not boat time") {
		t.Errorf("want UTC with a note saying it is not boat time: %s", out)
	}
}

func TestGetActiveAlarms_LiveSameMarkerIndependentOfSize(t *testing.T) {
	a := sensorHealthAlarmForTest()
	out := runActiveAlarms(t, assistantToolDeps{activeAlarms: func() []alarmStatus { return []alarmStatus{a} }})
	al := decodeAlarms(t, out)[0]
	if _, has := al["live_sensors"]; has || al["live_same_as_raised"] != true {
		t.Errorf("small result must use the marker too: %v", al)
	}
}

func TestGetActiveAlarms_TruncatedMeansAlarmsLeftOut(t *testing.T) {
	var list []alarmStatus
	for i := 0; i < 6; i++ {
		a := sensorHealthAlarmForTest()
		a.RuleID = fmt.Sprintf("rule-%d", i)
		a.Sensors = manySensors(40, 12)
		list = append(list, a)
	}
	out := runActiveAlarms(t, assistantToolDeps{activeAlarms: func() []alarmStatus { return list }})
	if strings.Contains(out, "truncated") || strings.Contains(out, `"shown"`) {
		t.Errorf("list cuts alone must not set truncated: %s", truncateForLog(out))
	}
	if !strings.Contains(out, "sensor_count") {
		t.Errorf("the cut must be signalled by sensor_count: %s", truncateForLog(out))
	}
}

func TestGetActiveAlarms_BigListDoesNotStripSmallOne(t *testing.T) {
	big := sensorHealthAlarmForTest()
	big.RuleID = "big"
	big.Sensors = manySensors(200, 40)
	small := sensorHealthAlarmForTest()
	small.RuleID = "small"
	small.Sensors = manySensors(5, 1)
	out := runActiveAlarms(t, assistantToolDeps{activeAlarms: func() []alarmStatus { return []alarmStatus{big, small} }})
	if len(out) > assistantMaxToolResultChars {
		t.Fatalf("over cap: %d", len(out))
	}
	alarms := decodeAlarms(t, out)
	if len(alarms) != 2 {
		t.Fatalf("both alarms must stay: %s", truncateForLog(out))
	}
	if got := len(alarms[1]["sensors"].([]any)); got != 5 {
		t.Errorf("small alarm lost sensors: %d", got)
	}
	if alarms[1]["sensor_count"] != nil {
		t.Errorf("small alarm must not be marked cut: %v", alarms[1]["sensor_count"])
	}
}

func TestGetActiveAlarms_TimezoneNamesBothSidesOfTheClocksChanging(t *testing.T) {
	useFreshBoatZone(t)
	a, b := sensorHealthAlarmForTest(), sensorHealthAlarmForTest()
	a.RaisedAt = time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)  // before Sydney's clocks go forward on 4 October
	b.RaisedAt = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC) // after
	deps := assistantToolDeps{
		activeAlarms: func() []alarmStatus { return []alarmStatus{a, b} },
		vesselState: func() (vesselStateData, error) {
			return vesselStateData{Latitude: -33.87, Longitude: 151.21}, nil
		},
	}
	out := runActiveAlarms(t, deps)
	if !strings.Contains(out, `"timezone":"AEST (UTC+10) then AEDT (UTC+11)"`) {
		t.Errorf("one label for two sides of the change would be wrong for one of them: %s", out)
	}
}
