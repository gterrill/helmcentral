package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// Regression: evaluateAlarmsOnce used to return early when no rules were
// configured, but evaluate is also what drops statuses for deleted rules — so
// deleting the last rule left its alarm stuck active forever.
func TestEvaluateAlarmsOnceClearsStatusesWhenLastRuleIsDeleted(t *testing.T) {
	withTempAlarmRules(t)
	withGlobalSnapshot(t, snapshotWithSelfDelta("electrical.batteries.house.voltage", 11.0, alarmNow))

	original := globalAlarmEngine
	globalAlarmEngine = newAlarmEngine()
	t.Cleanup(func() { globalAlarmEngine = original })

	rule := validRule()
	rule.DwellSeconds = 0
	created, err := createAlarmRule(rule)
	if err != nil {
		t.Fatalf("createAlarmRule: %v", err)
	}

	evaluateAlarmsOnce(alarmNow)
	if len(activeAlarms()) != 1 {
		t.Fatalf("expected the rule to be firing, got %d active", len(activeAlarms()))
	}

	if err := deleteAlarmRule(created.ID); err != nil {
		t.Fatalf("deleteAlarmRule: %v", err)
	}
	evaluateAlarmsOnce(alarmNow.Add(1 * 1e9))

	if got := len(activeAlarms()); got != 0 {
		t.Fatalf("deleting the last rule must clear its alarm, still %d active", got)
	}
	if worstAlarmState() != alarmStateNormal {
		t.Fatalf("worst state after deleting the last rule: got %q", worstAlarmState())
	}
}

// The reported bug, end to end: an alarm raised by another producer on the bus
// (here a course-provider arrival-circle notification) answered 409 "alarm is
// not acknowledgeable", because acknowledgeAlarmHandler only ever consulted
// the rule engine's status map — which bus-sourced notifications are never in.
// Acknowledging one now writes it back to SignalK with sound dropped.
func TestAcknowledgeAlarmHandlerAcknowledgesABusSourcedNotification(t *testing.T) {
	invalidateSignalKToken()
	t.Cleanup(invalidateSignalKToken)

	t.Setenv("SIGNALK_USERNAME", "helmcentral-service")
	t.Setenv("SIGNALK_PASSWORD", "service-secret")

	srv, rs := newRecordingServer(t)
	defer srv.Close()
	rs.on(http.MethodPost, "/signalk/v1/auth/login", http.StatusOK, `{"token":"service-jwt","timeToLive":86400}`)
	rs.on(http.MethodPost, signalKNotificationsAPIPath+"/"+liveNotificationID+"/acknowledge", http.StatusOK, `{"state":"COMPLETE","statusCode":200}`)
	t.Setenv("SETTINGS_FILE", settingsFileForServer(t, srv.URL))

	withGlobalSnapshot(t, snapshotWithNotification("notifications.arrivalCircleEntered",
		notificationWithStatus("alarm", liveNotificationStatus())))

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/alarms/x/acknowledge", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("notifications:arrivalCircleEntered")

	if err := acknowledgeAlarmHandler(c); err != nil {
		t.Fatalf("acknowledgeAlarmHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	var action *capturedRequest
	for i, call := range rs.calls() {
		if call.Method == http.MethodPost && strings.HasPrefix(call.Path, signalKNotificationsAPIPath) {
			action = &rs.calls()[i]
		}
	}
	if action == nil {
		t.Fatalf("acknowledging must call the Notifications API; calls were %+v", rs.calls())
	}
	if action.Path != signalKNotificationsAPIPath+"/"+liveNotificationID+"/acknowledge" {
		t.Fatalf("the action is keyed by the notification's own id, got %q", action.Path)
	}

	var status alarmStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if status.Phase != alarmPhaseAcknowledged {
		t.Fatalf("response phase: got %q, want %q", status.Phase, alarmPhaseAcknowledged)
	}
}

// An emergency still cannot be silenced, and the refusal must not reach the bus.
func TestAcknowledgeAlarmHandlerRefusesABusSourcedEmergency(t *testing.T) {
	withGlobalSnapshot(t, snapshotWithNotification("notifications.mob",
		notificationWithStatus("emergency", liveNotificationStatus())))

	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/alarms/x/acknowledge", nil), rec)
	c.SetParamNames("id")
	c.SetParamValues("notifications:mob")

	if err := acknowledgeAlarmHandler(c); err != nil {
		t.Fatalf("acknowledgeAlarmHandler: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
}

// ── a failed bus action forces an immediate resync (ADR 0086) ──────────────
//
// actOnSignalKNotification failing upstream (a 502, not one of the three 409
// refusals) means Helmcentral's copy and the server may already disagree --
// exactly the shape of the "Alarm not found!" case, where the server has
// forgotten a notification Helmcentral still shows as live. Waiting out the
// rest of the 30s sync interval would leave the stale card up for no reason;
// invalidating forces the very next tick to re-read the server instead.

func TestAlarmActionHandlerInvalidatesTheNotificationSyncOnAnUpstreamFailure(t *testing.T) {
	invalidateSignalKToken()
	t.Cleanup(invalidateSignalKToken)

	t.Setenv("SIGNALK_USERNAME", "helmcentral-service")
	t.Setenv("SIGNALK_PASSWORD", "service-secret")

	srv, rs := newRecordingServer(t)
	defer srv.Close()
	rs.on(http.MethodPost, "/signalk/v1/auth/login", http.StatusOK, `{"token":"service-jwt","timeToLive":86400}`)
	rs.on(http.MethodPost, signalKNotificationsAPIPath+"/"+liveNotificationID+"/acknowledge", http.StatusInternalServerError, `{"error":"boom"}`)
	t.Setenv("SETTINGS_FILE", settingsFileForServer(t, srv.URL))

	withGlobalSnapshot(t, snapshotWithNotification("notifications.arrivalCircleEntered",
		notificationWithStatus("alarm", liveNotificationStatus())))

	originalSyncer := globalNotificationSyncer
	globalNotificationSyncer = newNotificationSyncer(globalSignalKSnapshot)
	globalNotificationSyncer.lastRun = alarmNow // pretend a sync just ran
	t.Cleanup(func() { globalNotificationSyncer = originalSyncer })

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/alarms/x/acknowledge", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("notifications:arrivalCircleEntered")

	if err := acknowledgeAlarmHandler(c); err != nil {
		t.Fatalf("acknowledgeAlarmHandler: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status: got %d, want 502 (body %s)", rec.Code, rec.Body.String())
	}

	if !globalNotificationSyncer.lastRun.IsZero() {
		t.Fatalf("expected an upstream failure to invalidate the notification sync so the next tick re-reads the server")
	}
}

// A refusal is Helmcentral correctly declining a request the server was never
// asked to carry out -- nothing about what the server holds is in question,
// so there is nothing to resync early for.
func TestAlarmActionHandlerDoesNotInvalidateTheNotificationSyncOnARefusal(t *testing.T) {
	withGlobalSnapshot(t, snapshotWithNotification("notifications.mob",
		notificationWithStatus("emergency", liveNotificationStatus())))

	originalSyncer := globalNotificationSyncer
	globalNotificationSyncer = newNotificationSyncer(globalSignalKSnapshot)
	globalNotificationSyncer.lastRun = alarmNow
	t.Cleanup(func() { globalNotificationSyncer = originalSyncer })

	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/alarms/x/acknowledge", nil), rec)
	c.SetParamNames("id")
	c.SetParamValues("notifications:mob")

	if err := acknowledgeAlarmHandler(c); err != nil {
		t.Fatalf("acknowledgeAlarmHandler: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	if globalNotificationSyncer.lastRun.IsZero() {
		t.Fatalf("a refusal (409) must not trigger a resync -- nothing about the server's own state is in question")
	}
}

// Routing-level regression: bus-sourced ids are the first alarm ids to contain
// a character the frontend percent-encodes (the "notifications:" namespace
// colon), and Echo hands path params to the handler still escaped — it prefers
// URL.RawPath. A handler test that sets the param directly cannot see this, so
// this one goes through the router the way the browser does.
func TestAcknowledgeAlarmRouteDecodesTheNamespacedNotificationID(t *testing.T) {
	invalidateSignalKToken()
	t.Cleanup(invalidateSignalKToken)

	t.Setenv("SIGNALK_USERNAME", "helmcentral-service")
	t.Setenv("SIGNALK_PASSWORD", "service-secret")

	srv, rs := newRecordingServer(t)
	defer srv.Close()
	rs.on(http.MethodPost, "/signalk/v1/auth/login", http.StatusOK, `{"token":"service-jwt","timeToLive":86400}`)
	rs.on(http.MethodPost, signalKNotificationsAPIPath+"/"+liveNotificationID+"/acknowledge", http.StatusOK, `{"state":"COMPLETE","statusCode":200}`)
	t.Setenv("SETTINGS_FILE", settingsFileForServer(t, srv.URL))

	withGlobalSnapshot(t, snapshotWithNotification("notifications.arrivalCircleEntered",
		notificationWithStatus("alarm", liveNotificationStatus())))

	e := echo.New()
	e.POST("/api/alarms/:id/acknowledge", acknowledgeAlarmHandler)

	rec := httptest.NewRecorder()
	// Exactly what use-alarms.ts sends: encodeURIComponent("notifications:...").
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/alarms/notifications%3AarrivalCircleEntered/acknowledge", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// Silencing is a separate action with separate semantics: the alarm stops
// sounding but stays active, so it is not dropped from the banner the way an
// acknowledged one is.
func TestSilenceAlarmHandlerSilencesWithoutAcknowledging(t *testing.T) {
	invalidateSignalKToken()
	t.Cleanup(invalidateSignalKToken)

	t.Setenv("SIGNALK_USERNAME", "helmcentral-service")
	t.Setenv("SIGNALK_PASSWORD", "service-secret")

	srv, rs := newRecordingServer(t)
	defer srv.Close()
	rs.on(http.MethodPost, "/signalk/v1/auth/login", http.StatusOK, `{"token":"service-jwt","timeToLive":86400}`)
	rs.on(http.MethodPost, signalKNotificationsAPIPath+"/"+liveNotificationID+"/silence", http.StatusOK, `{"state":"COMPLETE","statusCode":200}`)
	t.Setenv("SETTINGS_FILE", settingsFileForServer(t, srv.URL))

	withGlobalSnapshot(t, snapshotWithNotification("notifications.arrivalCircleEntered",
		notificationWithStatus("alarm", liveNotificationStatus())))

	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/alarms/x/silence", nil), rec)
	c.SetParamNames("id")
	c.SetParamValues("notifications:arrivalCircleEntered")

	if err := silenceAlarmHandler(c); err != nil {
		t.Fatalf("silenceAlarmHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	var status alarmStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !status.Silenced {
		t.Fatalf("expected the alarm to report silenced")
	}
	if status.Phase != alarmPhaseActive {
		t.Fatalf("silencing is not acknowledging: phase got %q, want %q", status.Phase, alarmPhaseActive)
	}
	if !status.CanAcknowledge {
		t.Fatalf("a silenced alarm can still be acknowledged")
	}
}

// The engine has one action, so silencing a rule alarm is refused rather than
// quietly treated as an acknowledgement.
func TestSilenceAlarmHandlerRefusesARuleDrivenAlarm(t *testing.T) {
	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/alarms/x/silence", nil), rec)
	c.SetParamNames("id")
	c.SetParamValues("a3f1-locally-configured-rule")

	if err := silenceAlarmHandler(c); err != nil {
		t.Fatalf("silenceAlarmHandler: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
}

// The log constant alarmSourceSignalK existed but was never written anywhere
// until the bus watcher: recordAlarmEvent must carry a bus-sourced event's
// Source through to the log row rather than hardcoding alarmSourceRule.
func TestRecordAlarmEventUsesEventSourceForTheLogRow(t *testing.T) {
	store := newTestAlarmLog(t)
	original := globalAlarmLogStore
	globalAlarmLogStore = store
	t.Cleanup(func() { globalAlarmLogStore = original })

	event := alarmEvent{
		Kind:   alarmEventRaised,
		Source: alarmSourceSignalK,
		Rule: alarmRule{
			ID:    "notifications:electrical.batteries.0.voltage.high",
			Label: "High cell voltage",
			Path:  "electrical.batteries.0.voltage.high",
			State: alarmStateAlarm,
		},
		Status: alarmStatus{State: alarmStateAlarm, Message: "High cell voltage"},
	}
	recordAlarmEvent(event, alarmNow)

	entries, err := store.Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 || entries[0].Source != alarmSourceSignalK {
		t.Fatalf("expected the log row to carry the bus source, got %+v", entries)
	}
}

func TestRecordAlarmEventDefaultsEmptySourceToRule(t *testing.T) {
	store := newTestAlarmLog(t)
	original := globalAlarmLogStore
	globalAlarmLogStore = store
	t.Cleanup(func() { globalAlarmLogStore = original })

	event := alarmEvent{
		Kind: alarmEventRaised,
		Rule: alarmRule{
			ID:    "rule-1",
			Label: "House bank low",
			Path:  "electrical.batteries.house.voltage",
			State: alarmStateAlarm,
		},
		Status: alarmStatus{State: alarmStateAlarm, Message: "House bank low"},
	}
	recordAlarmEvent(event, alarmNow)

	entries, err := store.Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 || entries[0].Source != alarmSourceRule {
		t.Fatalf("expected an empty source to default to rule, got %+v", entries)
	}
}

// TestRecordAlarmEventDoesNotBlockOnASlowTransport is the regression guard
// for backend perf audit #9: recordAlarmEvent runs on the same goroutine as
// the 1s alarm evaluator tick, the anchor-drag watcher and the stream
// watchdog (alarm_watchdog.go), all of which call it directly, so a
// transport that is slow to answer -- or, as observed on the boat, a burst
// of transitions after a stream reconnect -- must never hold that tick
// hostage. blockingUntilReleasedTransport (alarm_notify_test.go) blocks its
// Send until released, which the old synchronous dispatch would have made
// recordAlarmEvent block on too.
func TestRecordAlarmEventDoesNotBlockOnASlowTransport(t *testing.T) {
	store := newTestAlarmLog(t)
	originalStore := globalAlarmLogStore
	globalAlarmLogStore = store
	t.Cleanup(func() { globalAlarmLogStore = originalStore })

	release := make(chan struct{})
	transport := &blockingUntilReleasedTransport{id: transportWebhook, release: release}

	dispatcher := newAlarmDispatcher()
	dispatcher.store = store
	dispatcher.transports = func() []notificationTransport { return []notificationTransport{transport} }

	originalDispatcher := globalAlarmDispatcher
	globalAlarmDispatcher = dispatcher
	t.Cleanup(func() { globalAlarmDispatcher = originalDispatcher })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go dispatcher.run(ctx)

	started := time.Now()
	recordAlarmEvent(raisedEvent(), alarmNow)
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("recordAlarmEvent took %s; a slow transport must not block it", elapsed)
	}

	close(release)
	waitFor(t, 2*time.Second, "the queued delivery to reach the slow transport", func() bool {
		return transport.count() > 0
	})
}

// Rule alarms advertise the one action the engine has, so the drawer renders
// an Acknowledge button for them and no Silence button.
func TestRuleDrivenAlarmsAdvertiseAcknowledgeOnly(t *testing.T) {
	withTempAlarmRules(t)
	withGlobalSnapshot(t, snapshotWithSelfDelta("electrical.batteries.house.voltage", 11.0, alarmNow))

	original := globalAlarmEngine
	globalAlarmEngine = newAlarmEngine()
	t.Cleanup(func() { globalAlarmEngine = original })

	rule := validRule()
	rule.DwellSeconds = 0
	if _, err := createAlarmRule(rule); err != nil {
		t.Fatalf("createAlarmRule: %v", err)
	}
	evaluateAlarmsOnce(alarmNow)

	alarms := activeAlarms()
	if len(alarms) != 1 {
		t.Fatalf("expected the rule to be firing, got %d", len(alarms))
	}
	if !alarms[0].CanAcknowledge {
		t.Fatalf("a live rule alarm can be acknowledged")
	}
	if alarms[0].CanSilence {
		t.Fatalf("the engine has no silence action, so it must not advertise one")
	}
}

// ── raised_at for bus notifications, recovered from the alarm log ──────────
//
// The bus itself carries no raised time worth reading: SignalK rewrites a
// notification's timestamp to the moment it is acknowledged, so by the time an
// operator looks at an active alarm the bus's own clock no longer says when it
// went live. The alarm log's open occurrence for "notifications:<path>" -- the
// row the bus watcher wrote when the notification first crossed its dwell -- is
// the only place that survives.

func TestActiveAlarmsFillsRaisedAtForABusNotificationFromTheOpenLogOccurrence(t *testing.T) {
	original := globalAlarmEngine
	globalAlarmEngine = newAlarmEngine()
	t.Cleanup(func() { globalAlarmEngine = original })

	store := newTestAlarmLog(t)
	originalStore := globalAlarmLogStore
	globalAlarmLogStore = store
	t.Cleanup(func() { globalAlarmLogStore = originalStore })

	ruleID := "notifications:arrivalCircleEntered"
	store.RecordRaised(alarmLogEntry{RuleID: ruleID, Label: "Arrival circle", RaisedAt: alarmNow})
	store.MarkAcknowledged(ruleID, alarmNow.Add(30*time.Second))

	withGlobalSnapshot(t, snapshotWithNotification("notifications.arrivalCircleEntered",
		notificationWithStatus("alarm", liveNotificationStatus())))

	alarms := activeAlarms()
	if len(alarms) != 1 {
		t.Fatalf("expected 1 active alarm, got %d: %+v", len(alarms), alarms)
	}
	if !alarms[0].RaisedAt.Equal(alarmNow) {
		t.Fatalf("raised_at: got %v, want %v", alarms[0].RaisedAt, alarmNow)
	}
	if alarms[0].AckedAt.IsZero() || !alarms[0].AckedAt.Equal(alarmNow.Add(30*time.Second)) {
		t.Fatalf("acked_at: got %v, want %v", alarms[0].AckedAt, alarmNow.Add(30*time.Second))
	}
}

func TestActiveAlarmsOmitsRaisedAtWhenTheLogHasNoOpenOccurrence(t *testing.T) {
	original := globalAlarmEngine
	globalAlarmEngine = newAlarmEngine()
	t.Cleanup(func() { globalAlarmEngine = original })

	store := newTestAlarmLog(t)
	originalStore := globalAlarmLogStore
	globalAlarmLogStore = store
	t.Cleanup(func() { globalAlarmLogStore = originalStore })

	withGlobalSnapshot(t, snapshotWithNotification("notifications.arrivalCircleEntered",
		notificationWithStatus("alarm", liveNotificationStatus())))

	alarms := activeAlarms()
	if len(alarms) != 1 {
		t.Fatalf("expected 1 active alarm, got %d: %+v", len(alarms), alarms)
	}
	if !alarms[0].RaisedAt.IsZero() {
		t.Fatalf("expected no raised_at with nothing logged, got %v", alarms[0].RaisedAt)
	}

	encoded, err := json.Marshal(alarms[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := payload["raised_at"]; present {
		t.Fatalf("raised_at must be absent, not a zero timestamp: %s", encoded)
	}
}

// The engine's status is the only representation of a rule alarm. When this
// engine holds one live AND the SignalK bus still carries the echo
// Helmcentral itself published for it (signalk_publish.go), activeAlarms must
// return it exactly once, under the rule's own id and label -- not a second
// time as a bus-sourced "notifications:<path>" entry, which is Helmcentral
// hearing its own voice.
func TestActiveAlarmsShowsARuleAlarmOnceWhenItsEchoIsOnTheBus(t *testing.T) {
	withTempAlarmRules(t)

	rule := validRule()
	rule.Path = pressureRatePath // "helmcentral.environment.pressureRate"
	rule.Label = "Barometer falling"
	rule.Op = alarmOpBelow
	rule.Value = -0.02
	rule.DwellSeconds = 0
	created, err := createAlarmRule(rule)
	if err != nil {
		t.Fatalf("createAlarmRule: %v", err)
	}

	original := globalAlarmEngine
	globalAlarmEngine = newAlarmEngine()
	t.Cleanup(func() { globalAlarmEngine = original })

	// Drives the engine directly with a reading below threshold, the same way
	// alarm_engine_test.go's own tests do -- this rule watches a derived path,
	// and evaluate() takes any alarmReader, so there is no need to wire up the
	// barometer history ring buffer just to make the condition true.
	globalAlarmEngine.evaluate([]alarmRule{created}, staticReader(-0.03), alarmNow)

	// The live ghost value captured off the boat: another Helmcentral
	// instance's echo of this very alarm, acknowledged, still on the bus.
	echo := map[string]any{
		"state":   "warn",
		"message": "Barometer falling: below -0.027777777777777776 (-0.0283)",
		"method":  []any{},
		"id":      "89f3b708-602d-4882-8044-182e6e24a134",
		"status": map[string]any{
			"silenced": false, "acknowledged": true,
			"canSilence": true, "canAcknowledge": true, "canClear": false,
		},
	}
	withGlobalSnapshot(t, snapshotWithNotification("notifications."+pressureRatePath, echo))

	alarms := activeAlarms()
	if len(alarms) != 1 {
		t.Fatalf("expected the rule alarm exactly once, got %d: %+v", len(alarms), alarms)
	}
	if alarms[0].RuleID != created.ID {
		t.Fatalf("rule id: got %q, want the engine's own rule id %q -- not a bus-sourced id", alarms[0].RuleID, created.ID)
	}
	if alarms[0].Label != "Barometer falling" {
		t.Fatalf("label: got %q", alarms[0].Label)
	}
}
