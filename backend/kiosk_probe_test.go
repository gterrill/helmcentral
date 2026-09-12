package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// captureLogs redirects the standard logger's output to a buffer for the
// duration of the test, restoring it on cleanup. Same pattern as
// notification_sync_test.go and place_name_test.go.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })
	return &buf
}

// newRawAuthRequest builds a request with a raw, non-JSON body — for
// exercising the "reject non-JSON" validation path, which newAuthRequest
// (auth_handlers_test.go) can't produce since it always marshals JSON.
func newRawAuthRequest(t *testing.T, method, path, rawBody string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(method, path, strings.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

func TestKioskProbeHandler_ValidBodyLogsExpectedLinesAndReturns204(t *testing.T) {
	buf := captureLogs(t)

	body := map[string]any{
		"user_agent": "Mozilla/5.0 (KioskBrowser)",
		"viewport":   map[string]any{"w": 1920, "h": 360},
		"rotate":     180,
		"checks": []map[string]any{
			{"name": "WebGL2 context", "pass": true, "detail": "renderer: llvmpipe"},
			{"name": "structuredClone", "pass": false, "detail": "typeof structuredClone !== 'function'"},
		},
	}
	c, rec := newAuthRequest(t, http.MethodPost, "/api/kiosk-probe", body)

	if err := kioskProbeHandler(c); err != nil {
		t.Fatalf("kioskProbeHandler: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	got := buf.String()
	wantLines := []string{
		`kiosk probe: ua="Mozilla/5.0 (KioskBrowser)" viewport=1920x360 rotate=180`,
		`kiosk probe: [PASS] WebGL2 context: renderer: llvmpipe`,
		`kiosk probe: [FAIL] structuredClone: typeof structuredClone !== 'function'`,
		`kiosk probe: 1/2 passed`,
	}
	for _, want := range wantLines {
		if !strings.Contains(got, want) {
			t.Fatalf("expected log output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestKioskProbeHandler_LogsHeightWhenPresent(t *testing.T) {
	buf := captureLogs(t)

	body := map[string]any{
		"user_agent": "Mozilla/5.0 (KioskBrowser)",
		"viewport":   map[string]any{"w": 1920, "h": 1080},
		"rotate":     180,
		"height":     360,
		"checks": []map[string]any{
			{"name": "WebGL2 context", "pass": true, "detail": "renderer: llvmpipe"},
		},
	}
	c, rec := newAuthRequest(t, http.MethodPost, "/api/kiosk-probe", body)

	if err := kioskProbeHandler(c); err != nil {
		t.Fatalf("kioskProbeHandler: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	got := buf.String()
	want := `kiosk probe: ua="Mozilla/5.0 (KioskBrowser)" viewport=1920x1080 rotate=180 height=360`
	if !strings.Contains(got, want) {
		t.Fatalf("expected log output to contain %q, got:\n%s", want, got)
	}
}

func TestKioskProbeHandler_OmitsHeightWhenAbsent(t *testing.T) {
	buf := captureLogs(t)

	body := map[string]any{
		"user_agent": "Mozilla/5.0 (KioskBrowser)",
		"viewport":   map[string]any{"w": 1920, "h": 360},
		"rotate":     0,
		"checks": []map[string]any{
			{"name": "structuredClone", "pass": true, "detail": "ok"},
		},
	}
	c, rec := newAuthRequest(t, http.MethodPost, "/api/kiosk-probe", body)

	if err := kioskProbeHandler(c); err != nil {
		t.Fatalf("kioskProbeHandler: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	got := buf.String()
	if strings.Contains(got, "height=") {
		t.Fatalf("expected no height= in log output when height is absent, got:\n%s", got)
	}
	wantHeader := `kiosk probe: ua="Mozilla/5.0 (KioskBrowser)" viewport=1920x360 rotate=0`
	if !strings.Contains(got, wantHeader) {
		t.Fatalf("expected log output to contain %q, got:\n%s", wantHeader, got)
	}
}

func TestKioskProbeHandler_InvalidBodiesReturn400(t *testing.T) {
	longString := strings.Repeat("x", 513)

	manyChecks := make([]map[string]any, 33)
	for i := range manyChecks {
		manyChecks[i] = map[string]any{"name": "check", "pass": true, "detail": "ok"}
	}

	validViewport := map[string]any{"w": 1920, "h": 360}

	tests := []struct {
		name string
		body map[string]any
	}{
		{
			name: "missing checks",
			body: map[string]any{
				"user_agent": "ua",
				"viewport":   validViewport,
				"rotate":     0,
				"checks":     []map[string]any{},
			},
		},
		{
			name: "more than 32 checks",
			body: map[string]any{
				"user_agent": "ua",
				"viewport":   validViewport,
				"rotate":     0,
				"checks":     manyChecks,
			},
		},
		{
			name: "user_agent over 512 chars",
			body: map[string]any{
				"user_agent": longString,
				"viewport":   validViewport,
				"rotate":     0,
				"checks": []map[string]any{
					{"name": "check", "pass": true, "detail": "ok"},
				},
			},
		},
		{
			name: "check detail over 512 chars",
			body: map[string]any{
				"user_agent": "ua",
				"viewport":   validViewport,
				"rotate":     0,
				"checks": []map[string]any{
					{"name": "check", "pass": true, "detail": longString},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, rec := newAuthRequest(t, http.MethodPost, "/api/kiosk-probe", tt.body)
			if err := kioskProbeHandler(ctx); err != nil {
				t.Fatalf("kioskProbeHandler: %v", err)
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			if rec.Body.Len() == 0 {
				t.Fatalf("expected an explicit error message in the 400 body, got empty body")
			}
		})
	}

	t.Run("non-JSON body", func(t *testing.T) {
		ctx, rec := newRawAuthRequest(t, http.MethodPost, "/api/kiosk-probe", "not json")
		if err := kioskProbeHandler(ctx); err != nil {
			t.Fatalf("kioskProbeHandler: %v", err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for non-JSON body, got %d: %s", rec.Code, rec.Body.String())
		}
		if rec.Body.Len() == 0 {
			t.Fatalf("expected an explicit error message in the 400 body, got empty body")
		}
	})
}

func TestBuildAPIRoutes_KioskProbeRouteIsPublic(t *testing.T) {
	sessions := newTestSessionStore(t)

	got := map[string]apiTier{}
	for _, route := range buildAPIRoutes(sessions, newWorldImageryHTTPClient()) {
		got[route.Method+" "+route.Path] = route.Tier
	}

	tier, ok := got["POST /api/kiosk-probe"]
	if !ok {
		t.Fatalf("route POST /api/kiosk-probe missing from the production route table")
	}
	if tier != tierPublic {
		t.Fatalf("route POST /api/kiosk-probe tier = %v, want tierPublic", tier)
	}
}
