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
	rs.on(http.MethodPut, "/notifications/arrivalCircleEntered", http.StatusOK, `{}`)
	t.Setenv("SETTINGS_FILE", settingsFileForServer(t, srv.URL))

	withGlobalSnapshot(t, snapshotWithNotification("notifications.arrivalCircleEntered", map[string]any{
		"state":   "alarm",
		"message": "WP arrival circle entered!",
		"method":  []any{"visual", "sound"},
	}))

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

	var put *capturedRequest
	for i, call := range rs.calls() {
		if call.Method == http.MethodPut {
			put = &rs.calls()[i]
		}
	}
	if put == nil {
		t.Fatalf("acknowledging must write back to the bus; calls were %+v", rs.calls())
	}
	if !strings.Contains(string(put.Body), `"visual"`) || strings.Contains(string(put.Body), `"sound"`) {
		t.Fatalf("the acknowledgement must drop sound and keep visual, got %s", string(put.Body))
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
	withGlobalSnapshot(t, snapshotWithNotification("notifications.mob", map[string]any{
		"state":   "emergency",
		"message": "MOB",
		"method":  []any{"visual", "sound"},
	}))

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
	rs.on(http.MethodPut, "/notifications/arrivalCircleEntered", http.StatusOK, `{}`)
	t.Setenv("SETTINGS_FILE", settingsFileForServer(t, srv.URL))

	withGlobalSnapshot(t, snapshotWithNotification("notifications.arrivalCircleEntered", map[string]any{
		"state":   "alarm",
		"message": "WP arrival circle entered!",
		"method":  []any{"visual", "sound"},
	}))

	e := echo.New()
	e.POST("/api/alarms/:id/acknowledge", acknowledgeAlarmHandler)

	rec := httptest.NewRecorder()
	// Exactly what use-alarms.ts sends: encodeURIComponent("notifications:...").
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/alarms/notifications%3AarrivalCircleEntered/acknowledge", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}
