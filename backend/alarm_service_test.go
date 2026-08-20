package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
