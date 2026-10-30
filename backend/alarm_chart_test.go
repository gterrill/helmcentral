package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Charts on alarm notifications (ADR 0235).

const chartTestTempPath = "environment.inside.engineRoom.temperature"

func withAlarmChartUnits(t *testing.T) {
	t.Helper()
	prev := alarmRuleUnitFor
	alarmRuleUnitFor = func(path string) string {
		switch path {
		case chartTestTempPath:
			return "K"
		case "tanks.fuel.0.currentLevel", "electrical.batteries.1.capacity.stateOfCharge":
			return "ratio"
		}
		return ""
	}
	t.Cleanup(func() { alarmRuleUnitFor = prev })
}

func tempRule() alarmRule {
	r := validRule()
	r.ID, r.Label, r.Path, r.Op, r.Value, r.State = "rule-temp", "Engine Room Hot", chartTestTempPath, alarmOpAbove, 343.15, alarmStateAlarm
	return r
}

func raisedFor(r alarmRule) alarmEvent {
	return alarmEvent{Kind: alarmEventRaised, Rule: r, Status: alarmStatus{RuleID: r.ID, State: r.State, Message: "Hot"}}
}

func TestAlarmChartSpec_RuleOnANumericPath(t *testing.T) {
	withAlarmChartUnits(t)
	spec, ok := alarmChartSpecFor(raisedFor(tempRule()), alarmNow)
	if !ok {
		t.Fatal("a threshold rule on a numeric path gets a chart")
	}
	if !spec.To.Equal(alarmNow) || spec.To.Sub(spec.From) != 6*time.Hour {
		t.Errorf("window %v to %v", spec.From, spec.To)
	}
	if spec.Title != "Engine Room Hot, last 6 hours" {
		t.Errorf("title %q", spec.Title)
	}
	if len(spec.Series) != 1 || spec.Series[0].Path != chartTestTempPath || spec.Series[0].Convert != "display" {
		t.Errorf("series %+v", spec.Series)
	}
	if spec.Threshold == nil || spec.Threshold.Value < 69.9 || spec.Threshold.Value > 70.1 || spec.Threshold.Label != "Alarm limit" {
		t.Errorf("the dashed line is the rule's threshold in degrees: %+v", spec.Threshold)
	}
}

func TestAlarmChartSpec_PathRoleRuleUsesTheRolesUnit(t *testing.T) {
	withAlarmChartUnits(t)
	r := validRule()
	r.ID, r.Label, r.Op, r.Value, r.State = "soc", "House Bank Low", alarmOpBelow, 0.5, alarmStateWarn
	r.Path, r.PathRole = "electrical.batteries.1.capacity.stateOfCharge", alarmPathRoleHouseBankSoC
	spec, ok := alarmChartSpecFor(raisedFor(r), alarmNow)
	if !ok || spec.Series[0].Role != alarmPathRoleHouseBankSoC || spec.Threshold.Value != 50 || spec.Threshold.Label != "Warning limit" {
		t.Fatalf("spec %+v ok %v", spec, ok)
	}
}

func TestAlarmChartSpec_TankHasWarnAndAlarmLevels(t *testing.T) {
	withAlarmChartUnits(t)
	alarm := 0.1
	r := alarmRule{ID: "tank:diesel", Label: "Diesel Port Low", Path: "tanks.fuel.0.currentLevel", Op: alarmOpBelow, Value: 0.2, AlarmLevelValue: &alarm, State: alarmStateWarn}
	spec, ok := alarmChartSpecFor(raisedFor(r), alarmNow)
	if !ok {
		t.Fatal("a tank alarm gets a chart")
	}
	if spec.Series[0].Convert != "percent" || spec.Series[0].Unit != "%" {
		t.Errorf("series %+v", spec.Series[0])
	}
	if spec.Threshold == nil || spec.Threshold.Value != 20 || spec.Threshold.Label != "Warning level" {
		t.Errorf("warn %+v", spec.Threshold)
	}
	if spec.AlarmThreshold == nil || spec.AlarmThreshold.Value != 10 || spec.AlarmThreshold.Label != "Alarm level" {
		t.Errorf("alarm %+v", spec.AlarmThreshold)
	}
}

func TestAlarmChartSpec_NoChartFor(t *testing.T) {
	withAlarmChartUnits(t)
	for name, mutate := range map[string]func(*alarmEvent){
		"a clear":                func(e *alarmEvent) { e.Kind = alarmEventCleared },
		"a change of state":      func(e *alarmEvent) { e.Kind = alarmEventChanged },
		"an escalation":          func(e *alarmEvent) { e.Kind = alarmEventEscalated },
		"a bus notification":     func(e *alarmEvent) { e.Source = alarmSourceSignalK },
		"a silence rule":         func(e *alarmEvent) { e.Rule.Op = alarmOpStale },
		"a rule with no path":    func(e *alarmEvent) { e.Rule.Path = "" },
		"anchor drag":            func(e *alarmEvent) { e.Rule.ID = anchorDragRuleID },
		"the stream watchdog":    func(e *alarmEvent) { e.Rule.ID = watchdogRuleID },
		"the collision alarm":    func(e *alarmEvent) { e.Rule.ID = collisionProfileRuleID },
		"the anchor watch state": func(e *alarmEvent) { e.Rule.ID = anchorWatchStateRuleID },
	} {
		e := raisedFor(tempRule())
		mutate(&e)
		if _, ok := alarmChartSpecFor(e, alarmNow); ok {
			t.Errorf("%s got a chart", name)
		}
	}
}

// ── what the dispatcher does with it ─────────────────────────────────────────

type capturingTransport struct {
	id   string
	fail atomic.Bool
	last atomic.Pointer[notificationMessage]
}

func (c *capturingTransport) ID() string { return c.id }
func (c *capturingTransport) Send(_ context.Context, m notificationMessage) error {
	if c.fail.Load() {
		return errors.New("down")
	}
	c.last.Store(&m)
	return nil
}

func chartDispatcher(t *testing.T, transport notificationTransport, src watchChartSources) (*alarmDispatcher, *alarmLogStore) {
	t.Helper()
	withAlarmChartUnits(t)
	d, store := testDispatcher(t, transport)
	d.chartSources = src
	d.boatLocation = func() (*time.Location, error) { return time.UTC, nil }
	return d, store
}

// freshChartDeadline is the deadline enqueue would set for an alarm queued now.
func freshChartDeadline() time.Time { return time.Now().Add(alarmChartDeadline) }

func tempHistory() []telemetryPoint {
	return chartPoints(alarmNow.Add(-6*time.Hour), 72, 5*time.Minute, func(i int) float64 { return 320 + float64(i)*0.4 })
}

func historySource(pts []telemetryPoint, calls *atomic.Int32) watchChartSources {
	return watchChartSources{Path: func(context.Context, string, time.Time, time.Time) ([]telemetryPoint, string, error) {
		if calls != nil {
			calls.Add(1)
		}
		return pts, "", nil
	}}
}

func TestDispatcher_RaisedRuleAlarmCarriesTheChartData(t *testing.T) {
	transport := &capturingTransport{id: transportWebhook}
	d, _ := chartDispatcher(t, transport, historySource(tempHistory(), nil))
	d.dispatch(raisedFor(tempRule()), "Pikorua", freshChartDeadline())
	got := transport.last.Load()
	if got == nil || len(got.Charts) != 1 {
		t.Fatalf("message %+v", got)
	}
	c := got.Charts[0]
	if c.Problem != "" || len(c.Series) != 1 || c.Unit != "°C" || c.Threshold == nil || c.Threshold.Value < 69.9 || c.PNGBase64 != "" {
		t.Errorf("chart %+v", c)
	}
	if first := c.Series[0].Points[0][1]; first < 46 || first > 48 {
		t.Errorf("first point %v is not in degrees", first)
	}
}

func TestDispatcher_NoHistoryGoesOutOnTimeWithTheReason(t *testing.T) {
	transport := &capturingTransport{id: transportNtfy}
	d, _ := chartDispatcher(t, transport, watchChartSources{}) // InfluxDB is optional: no path reader
	d.dispatch(raisedFor(tempRule()), "Pikorua", freshChartDeadline())
	got := transport.last.Load()
	if got == nil || len(got.Charts) != 1 || got.Charts[0].Problem == "" || len(got.Charts[0].Series) != 0 {
		t.Fatalf("message %+v", got)
	}
	if strings.HasPrefix(got.Charts[0].Problem, "No history") {
		t.Errorf("the reason reads after 'Chart left out:', %q", got.Charts[0].Problem)
	}
}

func TestDispatcher_ASlowHistoryReadNeverHoldsTheAlarmBack(t *testing.T) {
	prev := alarmChartDeadline
	alarmChartDeadline = 150 * time.Millisecond
	t.Cleanup(func() { alarmChartDeadline = prev })
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	src := watchChartSources{Path: func(context.Context, string, time.Time, time.Time) ([]telemetryPoint, string, error) {
		<-release // ignores its context, as a stuck driver might
		return nil, "", nil
	}}
	transport := &capturingTransport{id: transportSMTP}
	d, _ := chartDispatcher(t, transport, src)
	start := time.Now()
	d.dispatch(raisedFor(tempRule()), "Pikorua", freshChartDeadline())
	if took := time.Since(start); took > time.Second {
		t.Fatalf("the alarm waited %v", took)
	}
	got := transport.last.Load()
	if got == nil || len(got.Charts) != 1 || !strings.Contains(got.Charts[0].Problem, "in time") {
		t.Fatalf("message %+v", got)
	}
}

func TestDispatcher_TimeSpentInTheQueueCountsAgainstTheDeadline(t *testing.T) {
	var calls atomic.Int32
	transport := &capturingTransport{id: transportWebhook}
	d, _ := chartDispatcher(t, transport, historySource(tempHistory(), &calls))
	d.dispatch(raisedFor(tempRule()), "Pikorua", time.Now().Add(-time.Second))
	got := transport.last.Load()
	if got == nil || len(got.Charts) != 1 || !strings.Contains(got.Charts[0].Problem, "in time") {
		t.Fatalf("message %+v", got)
	}
}

func TestDispatcher_ClearsAndBusAlarmsAreNotCharted(t *testing.T) {
	var calls atomic.Int32
	transport := &capturingTransport{id: transportWebhook}
	d, _ := chartDispatcher(t, transport, historySource(tempHistory(), &calls))
	cleared := raisedFor(tempRule())
	cleared.Kind = alarmEventCleared
	d.dispatch(cleared, "Pikorua", freshChartDeadline())
	bus := raisedFor(tempRule())
	bus.Source = alarmSourceSignalK
	d.dispatch(bus, "Pikorua", freshChartDeadline())
	if got := transport.last.Load(); got == nil || len(got.Charts) != 0 || calls.Load() != 0 {
		t.Errorf("message %+v, history reads %d", got, calls.Load())
	}
}

func TestDispatcher_WebPushAndSignalKAloneDoNotReadHistory(t *testing.T) {
	var calls atomic.Int32
	transport := &capturingTransport{id: transportWebPush}
	d, _ := chartDispatcher(t, transport, historySource(tempHistory(), &calls))
	d.dispatch(raisedFor(tempRule()), "Pikorua", freshChartDeadline())
	if calls.Load() != 0 {
		t.Errorf("history was read %d times for a channel that cannot show a chart", calls.Load())
	}
}

// A channel that cannot show a chart is never kept waiting for one.
func TestDispatcher_WebPushIsSentBeforeTheChartReaderReturns(t *testing.T) {
	pushed := make(chan struct{})
	readerSawPush := make(chan bool, 1)
	src := watchChartSources{Path: func(context.Context, string, time.Time, time.Time) ([]telemetryPoint, string, error) {
		select {
		case <-pushed:
			readerSawPush <- true
		case <-time.After(time.Second):
			readerSawPush <- false
		}
		return tempHistory(), "", nil
	}}
	push := &signalingTransport{id: transportWebPush, sent: pushed}
	hook := &capturingTransport{id: transportWebhook}
	d, _ := chartDispatcher(t, push, src)
	d.transports = func() []notificationTransport { return []notificationTransport{hook, push} }
	d.dispatch(raisedFor(tempRule()), "Pikorua", freshChartDeadline())
	if !<-readerSawPush {
		t.Fatal("web push had not been sent when the chart reader ran")
	}
	if got := hook.last.Load(); got == nil || len(got.Charts) != 1 || got.Charts[0].Problem != "" {
		t.Errorf("the webhook still gets its chart: %+v", got)
	}
}

type signalingTransport struct {
	id   string
	sent chan struct{}
}

func (s *signalingTransport) ID() string { return s.id }
func (s *signalingTransport) Send(context.Context, notificationMessage) error {
	close(s.sent)
	return nil
}

func TestDispatcher_ARetrySendsTheSameChartData(t *testing.T) {
	transport := &capturingTransport{id: transportWebhook}
	transport.fail.Store(true)
	var calls atomic.Int32
	d, store := chartDispatcher(t, transport, historySource(tempHistory(), &calls))
	d.dispatch(raisedFor(tempRule()), "Pikorua", freshChartDeadline())
	if depth, _ := store.QueueDepth(); depth != 1 {
		t.Fatalf("depth %d", depth)
	}
	due, err := store.DueNotifications(alarmNow.Add(time.Hour), 5)
	if err != nil || len(due) != 1 {
		t.Fatalf("due %v %v", due, err)
	}
	if strings.Contains(string(due[0].Payload), "png_base64") {
		t.Error("the queue holds the data, never the picture")
	}
	var queued notificationMessage
	if err := json.Unmarshal(due[0].Payload, &queued); err != nil || len(queued.Charts) != 1 || len(queued.Charts[0].Series) != 1 {
		t.Fatalf("queued %+v %v", queued, err)
	}
	transport.fail.Store(false)
	d.now = func() time.Time { return alarmNow.Add(notifyMinBackoff + time.Second) }
	d.drain(context.Background())
	got := transport.last.Load()
	if got == nil || len(got.Charts) != 1 || got.Charts[0].Series[0].Points[0] != queued.Charts[0].Series[0].Points[0] || calls.Load() != 1 {
		t.Errorf("retry %+v, history reads %d", got, calls.Load())
	}
}

func TestRenderWatchChartPNG_DrawsTheAlarmLevelBesideTheWarnLevel(t *testing.T) {
	one := testWatchChart()
	two := testWatchChart()
	two.AlarmThreshold = &watchChartThreshold{Label: "Alarm level", Value: 1000}
	a, err := renderWatchChartPNG(one)
	if err != nil {
		t.Fatal(err)
	}
	b, err := renderWatchChartPNG(two)
	if err != nil {
		t.Fatal(err)
	}
	if countPixels(decodeChartPNG(t, b), watchChartThresholdColour) <= countPixels(decodeChartPNG(t, a), watchChartThresholdColour) {
		t.Error("the second dashed line is not drawn")
	}
}

// The history gets its whole time whatever the other channels do.
func TestDispatcher_SlowPlainChannelsDoNotEatTheChartsTime(t *testing.T) {
	src := watchChartSources{Path: func(context.Context, string, time.Time, time.Time) ([]telemetryPoint, string, error) {
		time.Sleep(200 * time.Millisecond)
		return tempHistory(), "", nil
	}}
	slow := &slowTransport{id: transportWebPush, delay: 300 * time.Millisecond}
	hook := &capturingTransport{id: transportWebhook}
	d, _ := chartDispatcher(t, slow, src)
	d.transports = func() []notificationTransport { return []notificationTransport{slow, hook} }
	d.dispatch(raisedFor(tempRule()), "Pikorua", time.Now().Add(400*time.Millisecond))
	got := hook.last.Load()
	if got == nil || len(got.Charts) != 1 || got.Charts[0].Problem != "" {
		t.Fatalf("the chart was read after the slow channel, so it ran out of time: %+v", got)
	}
}

type slowTransport struct {
	id    string
	delay time.Duration
}

func (s *slowTransport) ID() string { return s.id }
func (s *slowTransport) Send(context.Context, notificationMessage) error {
	time.Sleep(s.delay)
	return nil
}

func TestAlarmChartSpec_AWorseningGetsAChartButARepeatDoesNot(t *testing.T) {
	withAlarmChartUnits(t)
	alarm := 0.1
	r := alarmRule{ID: "tank:diesel", Label: "Diesel Port Low", Path: "tanks.fuel.0.currentLevel", Op: alarmOpBelow, Value: 0.2, AlarmLevelValue: &alarm, State: alarmStateWarn}
	worse := alarmEvent{Kind: alarmEventChanged, Rule: r, Status: alarmStatus{State: alarmStateAlarm}}
	if _, ok := alarmChartSpecFor(worse, alarmNow); !ok {
		t.Error("a tank going from warn to alarm gets a chart")
	}
	better := alarmEvent{Kind: alarmEventChanged, Rule: r, Status: alarmStatus{State: alarmStateWarn}}
	if _, ok := alarmChartSpecFor(better, alarmNow); ok {
		t.Error("an alarm easing back gets none")
	}
	reminder := alarmEvent{Kind: alarmEventEscalated, Rule: r, Status: alarmStatus{State: alarmStateAlarm}}
	if _, ok := alarmChartSpecFor(reminder, alarmNow); ok {
		t.Error("a reminder that nobody has acknowledged it is a repeat")
	}
}

func TestAlarmChartSpec_TankSeriesIsNamedForTheTank(t *testing.T) {
	withAlarmChartUnits(t)
	for label, want := range map[string]string{"Diesel Port Low": "Diesel Port", "Black Water High": "Black Water"} {
		r := alarmRule{ID: "tank:x", Label: label, Path: "tanks.fuel.0.currentLevel", Op: alarmOpBelow, Value: 0.2, State: alarmStateWarn}
		spec, ok := alarmChartSpecFor(raisedFor(r), alarmNow)
		if !ok || spec.Series[0].Label != want {
			t.Errorf("%q: series %+v", label, spec.Series)
		}
	}
}

func TestWatchChartJSON_ALeftOutChartIsTitleAndProblemOnly(t *testing.T) {
	raw, err := json.Marshal(watchChart{Title: "Engine Room Hot, last 6 hours", Problem: "history is slow"})
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]any
	if err := json.Unmarshal(raw, &keys); err != nil || len(keys) != 2 || keys["title"] == nil || keys["problem"] == nil {
		t.Errorf("%s", raw)
	}
	full, _ := json.Marshal(testWatchChart())
	if !strings.Contains(string(full), `"series"`) || !strings.Contains(string(full), `"finding_index":0`) {
		t.Errorf("a chart with data keeps every field: %s", full)
	}
}

func TestRenderWatchChartPNG_LegendWrapsInsteadOfRunningOffTheEdge(t *testing.T) {
	c := testWatchChart()
	c.Series = c.Series[:1]
	c.Series[0].Label = strings.Repeat("Diesel Port Wing Tank ", 6)
	c.Threshold = &watchChartThreshold{Label: "Warning level", Value: 1008}
	c.AlarmThreshold = &watchChartThreshold{Label: "Alarm level", Value: 1005}
	data, err := renderWatchChartPNG(c)
	if err != nil {
		t.Fatal(err)
	}
	img := decodeChartPNG(t, data)
	for y := 30; y < 90; y++ {
		for x := 784; x < 800; x++ {
			if r, g, b, _ := img.At(x, y).RGBA(); r>>8 != 255 || g>>8 != 255 || b>>8 != 255 {
				t.Fatalf("legend ink at the right edge (%d,%d)", x, y)
			}
		}
	}
	if countPixels(img, watchChartThresholdColour) == 0 {
		t.Error("the thresholds are still drawn")
	}
}
