package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

// TestRegisterPprofRoutes_AbsentWithoutFlag is item 8's first required
// test: with HELMCENTRAL_PPROF unset (or anything other than "1"),
// /debug/pprof/ must not be reachable at all.
func TestRegisterPprofRoutes_AbsentWithoutFlag(t *testing.T) {
	t.Setenv(helmcentralPprofEnvVar, "")
	e := echo.New()
	registerPprofRoutes(e)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for /debug/pprof/ with the flag unset, got %d", rec.Code)
	}
}

// TestRegisterPprofRoutes_PresentWithFlag is item 8's other required test:
// HELMCENTRAL_PPROF=1 makes pprof's index (and its named profile handlers)
// reachable.
func TestRegisterPprofRoutes_PresentWithFlag(t *testing.T) {
	t.Setenv(helmcentralPprofEnvVar, "1")
	e := echo.New()
	registerPprofRoutes(e)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /debug/pprof/ with HELMCENTRAL_PPROF=1, got %d: %s", rec.Code, rec.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/debug/pprof/goroutine?debug=1", nil)
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 for /debug/pprof/goroutine with HELMCENTRAL_PPROF=1, got %d", rec2.Code)
	}
}

// TestRegisterPprofRoutes_ValueOtherThanOneStaysOff guards against a typo'd
// or truthy-but-not-"1" value (e.g. "true") silently turning it on.
func TestRegisterPprofRoutes_ValueOtherThanOneStaysOff(t *testing.T) {
	t.Setenv(helmcentralPprofEnvVar, "true")
	e := echo.New()
	registerPprofRoutes(e)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for /debug/pprof/ with HELMCENTRAL_PPROF=true (not \"1\"), got %d", rec.Code)
	}
}
