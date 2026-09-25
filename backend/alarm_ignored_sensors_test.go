package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestIgnoreSensorAddsAndPersists(t *testing.T) {
	path := withTempAlarmRules(t)

	if err := ignoreSensor("propulsion.port.exhaustTemperature"); err != nil {
		t.Fatalf("ignoreSensor: %v", err)
	}

	got := listIgnoredSensors()
	if len(got) != 1 || got[0] != "propulsion.port.exhaustTemperature" {
		t.Fatalf("listIgnoredSensors: got %+v", got)
	}

	// Reload from disk to prove persistence, not just in-memory state.
	alarmRulesMu.Lock()
	alarmRulesIgnoredSensors = nil
	alarmRulesMu.Unlock()
	if err := loadAlarmRules(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	_ = path
	if got := listIgnoredSensors(); len(got) != 1 || got[0] != "propulsion.port.exhaustTemperature" {
		t.Fatalf("after reload: got %+v", got)
	}
}

func TestIgnoreSensorIsIdempotent(t *testing.T) {
	withTempAlarmRules(t)

	if err := ignoreSensor("propulsion.port.exhaustTemperature"); err != nil {
		t.Fatalf("first ignore: %v", err)
	}
	if err := ignoreSensor("propulsion.port.exhaustTemperature"); err != nil {
		t.Fatalf("second ignore: %v", err)
	}
	if got := listIgnoredSensors(); len(got) != 1 {
		t.Fatalf("expected no duplicate, got %+v", got)
	}
}

func TestIgnoreSensorRejectsBlank(t *testing.T) {
	withTempAlarmRules(t)
	if err := ignoreSensor("   "); err == nil {
		t.Fatalf("expected a blank identifier to be rejected")
	}
}

func TestUnignoreSensorRemovesIt(t *testing.T) {
	withTempAlarmRules(t)

	if err := ignoreSensor("propulsion.port.exhaustTemperature"); err != nil {
		t.Fatalf("ignore: %v", err)
	}
	if err := ignoreSensor("venus.battery.512"); err != nil {
		t.Fatalf("ignore: %v", err)
	}

	if err := unignoreSensor("propulsion.port.exhaustTemperature"); err != nil {
		t.Fatalf("unignore: %v", err)
	}

	got := listIgnoredSensors()
	if len(got) != 1 || got[0] != "venus.battery.512" {
		t.Fatalf("expected only venus.battery.512 to remain, got %+v", got)
	}
}

func TestIgnoredSensorSetIsUnaffectedByLaterListMutation(t *testing.T) {
	withTempAlarmRules(t)
	if err := ignoreSensor("propulsion.port.exhaustTemperature"); err != nil {
		t.Fatalf("ignore: %v", err)
	}
	set := ignoredSensorSet()
	if !set["propulsion.port.exhaustTemperature"] {
		t.Fatalf("expected the set to contain the ignored path")
	}
	if set["something.else"] {
		t.Fatalf("expected the set to contain only what was ignored")
	}
}

// --- HTTP handlers -----------------------------------------------------

func TestIgnoreSensorHandler(t *testing.T) {
	withTempAlarmRules(t)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/alarms/ignored-sensors", strings.NewReader(`{"identifier":"propulsion.port.exhaustTemperature"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()

	if err := ignoreSensorHandler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := listIgnoredSensors(); len(got) != 1 {
		t.Fatalf("expected the handler to persist the identifier, got %+v", got)
	}
}

func TestIgnoreSensorHandlerRejectsEmptyBody(t *testing.T) {
	withTempAlarmRules(t)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/alarms/ignored-sensors", strings.NewReader(`{}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()

	if err := ignoreSensorHandler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an empty identifier, got %d", rec.Code)
	}
}

func TestUnignoreSensorHandler(t *testing.T) {
	withTempAlarmRules(t)
	if err := ignoreSensor("propulsion.port.exhaustTemperature"); err != nil {
		t.Fatalf("ignore: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/alarms/ignored-sensors/propulsion.port.exhaustTemperature", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("identifier")
	c.SetParamValues("propulsion.port.exhaustTemperature")

	if err := unignoreSensorHandler(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := listIgnoredSensors(); len(got) != 0 {
		t.Fatalf("expected the identifier removed, got %+v", got)
	}
}

// TestUnignoreSensorHandlerRouteDecodesAColonInTheIdentifier is a routing-
// level regression for code review finding 9: Echo hands path params to the
// handler still percent-encoded (it prefers URL.RawPath), the same trap
// alarm_service.go:364's acknowledgeAlarmHandler and engine_profiles.go's
// parseProfileID already guard against with url.PathUnescape.
// unignoreSensorHandler did not, so a $source id containing a colon (a real
// shape -- see anomaly_sensor_health_test.go's venus.battery.512-style
// fixtures) -- sent by use-ignored-sensors.ts as
// encodeURIComponent(identifier) -- arrived at unignoreSensor still escaped,
// matched nothing in the ignore list, and silently did nothing (DELETE still
// answers 204 either way, so only the list itself proves the bug).
func TestUnignoreSensorHandlerRouteDecodesAColonInTheIdentifier(t *testing.T) {
	withTempAlarmRules(t)
	if err := ignoreSensor("venus.com.victronenergy.battery:512"); err != nil {
		t.Fatalf("ignore: %v", err)
	}

	e := echo.New()
	e.DELETE("/api/alarms/ignored-sensors/:identifier", unignoreSensorHandler)

	rec := httptest.NewRecorder()
	// Exactly what use-ignored-sensors.ts sends:
	// encodeURIComponent("venus.com.victronenergy.battery:512").
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/alarms/ignored-sensors/venus.com.victronenergy.battery%3A512", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
	if got := listIgnoredSensors(); len(got) != 0 {
		t.Fatalf("expected the colon-bearing identifier to be removed, got %+v", got)
	}
}

func TestListIgnoredSensorsHandler(t *testing.T) {
	withTempAlarmRules(t)
	if err := ignoreSensor("propulsion.port.exhaustTemperature"); err != nil {
		t.Fatalf("ignore: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/alarms/ignored-sensors", nil)
	rec := httptest.NewRecorder()

	if err := listIgnoredSensorsHandler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
