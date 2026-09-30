package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// The browser sends a job id through encodeURIComponent, so the colons of
// job:<uuid>:<service> arrive as %3A and echo hands the param over still
// encoded. These tests go through a real router for that reason: the other
// handler tests set the param directly and cannot see it.

func maintenanceRuleRouter() *echo.Echo {
	e := echo.New()
	const base = "/api/inventory/maintenance/rules/:id"
	e.GET(base, getMaintenanceRuleHandler)
	e.PUT(base+"/overrides", setMaintenanceRuleOverridesHandler)
	e.DELETE(base+"/overrides/:field", resetMaintenanceRuleOverrideHandler)
	e.POST(base+"/complete", completeMaintenanceRuleHandler)
	e.POST(base+"/acknowledge", acknowledgeMaintenanceRuleHandler)
	e.POST(base+"/last-done", setMaintenanceRuleLastDoneHandler)
	e.PUT(base+"/procedure-note", setMaintenanceRuleProcedureNoteHandler)
	e.POST(base+"/procedure-note", createMaintenanceProcedureNoteHandler)
	e.PUT(base, updateMaintenanceRuleHandler)
	e.DELETE(base, deleteMaintenanceRuleHandler)
	return e
}

func routeRule(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	rec := httptest.NewRecorder()
	maintenanceRuleRouter().ServeHTTP(rec, req)
	return rec
}

func TestRuleRoutesDecodeAnEncodedJobID(t *testing.T) {
	_, item := scheduleFixture(t)
	enc := strings.ReplaceAll(url.PathEscape(maintenanceJobID(item.ID, "engine-oil")), ":", "%3A") // what encodeURIComponent sends
	if !strings.Contains(enc, "%3A") {
		t.Fatalf("fixture: expected an encoded id, got %s", enc)
	}
	base := "/api/inventory/maintenance/rules/" + enc

	rec := routeRule(t, http.MethodGet, base+today, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body.String())
	}
	rec = routeRule(t, http.MethodPut, base+"/overrides"+today, `{"interval_hours": 400}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("overrides PUT: %d %s", rec.Code, rec.Body.String())
	}
	rec = routeRule(t, http.MethodDelete, base+"/overrides/interval_hours"+today, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("override reset: %d %s", rec.Code, rec.Body.String())
	}
	rec = routeRule(t, http.MethodPost, base+"/complete"+today, `{"performed_at":"2026-06-01","hours":100}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", rec.Code, rec.Body.String())
	}
	rec = routeRule(t, http.MethodPost, base+"/acknowledge"+today, `{"reason":"deferred"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("acknowledge: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRuleRoutesRefuseAMalformedEscape(t *testing.T) {
	scheduleFixture(t)
	// httptest.NewRequest refuses a bad escape, so plant it in RawPath the
	// way a hand-rolled client's request line would arrive.
	req := httptest.NewRequest(http.MethodGet, "/api/inventory/maintenance/rules/x"+today, nil)
	req.URL.RawPath = "/api/inventory/maintenance/rules/job%3Abad%ZZ"
	rec := httptest.NewRecorder()
	maintenanceRuleRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d %s", rec.Code, rec.Body.String())
	}
}
