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

func TestDisplayProbeHandler_ValidBodyLogsExpectedLinesAndReturns204(t *testing.T) {
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
	c, rec := newAuthRequest(t, http.MethodPost, "/api/display-probe", body)

	if err := displayProbeHandler(c); err != nil {
		t.Fatalf("displayProbeHandler: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	got := buf.String()
	wantLines := []string{
		`display probe: ua="Mozilla/5.0 (KioskBrowser)" viewport=1920x360 rotate=180`,
		`display probe: [PASS] "WebGL2 context": "renderer: llvmpipe"`,
		`display probe: [FAIL] "structuredClone": "typeof structuredClone !== 'function'"`,
		`display probe: 1/2 passed`,
	}
	for _, want := range wantLines {
		if !strings.Contains(got, want) {
			t.Fatalf("expected log output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestDisplayProbeHandler_LogsHeightWhenPresent(t *testing.T) {
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
	c, rec := newAuthRequest(t, http.MethodPost, "/api/display-probe", body)

	if err := displayProbeHandler(c); err != nil {
		t.Fatalf("displayProbeHandler: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	got := buf.String()
	want := `display probe: ua="Mozilla/5.0 (KioskBrowser)" viewport=1920x1080 rotate=180 height=360`
	if !strings.Contains(got, want) {
		t.Fatalf("expected log output to contain %q, got:\n%s", want, got)
	}
}

func TestDisplayProbeHandler_OmitsHeightWhenAbsent(t *testing.T) {
	buf := captureLogs(t)

	body := map[string]any{
		"user_agent": "Mozilla/5.0 (KioskBrowser)",
		"viewport":   map[string]any{"w": 1920, "h": 360},
		"rotate":     0,
		"checks": []map[string]any{
			{"name": "structuredClone", "pass": true, "detail": "ok"},
		},
	}
	c, rec := newAuthRequest(t, http.MethodPost, "/api/display-probe", body)

	if err := displayProbeHandler(c); err != nil {
		t.Fatalf("displayProbeHandler: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	got := buf.String()
	if strings.Contains(got, "height=") {
		t.Fatalf("expected no height= in log output when height is absent, got:\n%s", got)
	}
	wantHeader := `display probe: ua="Mozilla/5.0 (KioskBrowser)" viewport=1920x360 rotate=0`
	if !strings.Contains(got, wantHeader) {
		t.Fatalf("expected log output to contain %q, got:\n%s", wantHeader, got)
	}
}

func TestDisplayProbeHandler_LogsCapturedKeysWhenPresent(t *testing.T) {
	buf := captureLogs(t)

	// The key readout exists because what a television remote emits is not
	// knowable off the device (ADR 0110 section 12), and the wall display's
	// step/pause keys have to be written against real codes. The operator
	// reads it on the probe screen, but it belongs in the log too: that is
	// where every other probe result is read from, and a code noted by eye
	// off a 55" screen is a code nobody can paste into a handler.
	body := map[string]any{
		"user_agent": "Mozilla/5.0 (webOS)",
		"viewport":   map[string]any{"w": 1920, "h": 1080},
		"rotate":     0,
		"checks": []map[string]any{
			{"name": "structuredClone", "pass": true, "detail": "ok"},
		},
		"keys": []map[string]any{
			{"key": "ArrowRight", "code": "ArrowRight", "keyCode": 39, "at": "14:03:11"},
			{"key": "Unidentified", "code": "", "keyCode": 461, "at": "14:03:14"},
		},
	}
	c, rec := newAuthRequest(t, http.MethodPost, "/api/display-probe", body)

	if err := displayProbeHandler(c); err != nil {
		t.Fatalf("displayProbeHandler: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	got := buf.String()
	for _, want := range []string{
		`display probe: key at="14:03:11" key="ArrowRight" code="ArrowRight" keyCode=39`,
		`display probe: key at="14:03:14" key="Unidentified" code="" keyCode=461`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected log output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestDisplayProbeHandler_OmitsKeyLinesWhenNoKeysCaptured(t *testing.T) {
	buf := captureLogs(t)

	body := map[string]any{
		"user_agent": "Mozilla/5.0 (KioskBrowser)",
		"viewport":   map[string]any{"w": 1920, "h": 360},
		"rotate":     180,
		"checks": []map[string]any{
			{"name": "structuredClone", "pass": true, "detail": "ok"},
		},
	}
	c, rec := newAuthRequest(t, http.MethodPost, "/api/display-probe", body)

	if err := displayProbeHandler(c); err != nil {
		t.Fatalf("displayProbeHandler: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// The strip has no input device, so its probe reports post every 60
	// seconds forever with an empty key list. A "key" line per report would
	// be noise in the one log an operator greps.
	if got := buf.String(); strings.Contains(got, "display probe: key ") {
		t.Fatalf("expected no key lines when none were captured, got:\n%s", got)
	}
}

func TestDisplayProbeHandler_RejectsTooManyKeys(t *testing.T) {
	manyKeys := make([]map[string]any, displayProbeMaxKeys+1)
	for i := range manyKeys {
		manyKeys[i] = map[string]any{"key": "a", "code": "KeyA", "keyCode": 65, "at": "14:03:11"}
	}

	body := map[string]any{
		"user_agent": "Mozilla/5.0 (KioskBrowser)",
		"viewport":   map[string]any{"w": 1920, "h": 360},
		"rotate":     0,
		"checks": []map[string]any{
			{"name": "structuredClone", "pass": true, "detail": "ok"},
		},
		"keys": manyKeys,
	}
	c, rec := newAuthRequest(t, http.MethodPost, "/api/display-probe", body)

	if err := displayProbeHandler(c); err != nil {
		t.Fatalf("displayProbeHandler: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for %d keys, got %d: %s", len(manyKeys), rec.Code, rec.Body.String())
	}
}

func TestDisplayProbeHandler_InvalidBodiesReturn400(t *testing.T) {
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
			ctx, rec := newAuthRequest(t, http.MethodPost, "/api/display-probe", tt.body)
			if err := displayProbeHandler(ctx); err != nil {
				t.Fatalf("displayProbeHandler: %v", err)
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
		ctx, rec := newRawAuthRequest(t, http.MethodPost, "/api/display-probe", "not json")
		if err := displayProbeHandler(ctx); err != nil {
			t.Fatalf("displayProbeHandler: %v", err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for non-JSON body, got %d: %s", rec.Code, rec.Body.String())
		}
		if rec.Body.Len() == 0 {
			t.Fatalf("expected an explicit error message in the 400 body, got empty body")
		}
	})
}

func TestBuildAPIRoutes_DisplayProbeRouteIsPublic(t *testing.T) {
	sessions := newTestSessionStore(t)

	got := map[string]apiTier{}
	for _, route := range buildAPIRoutes(sessions, newWorldImageryHTTPClient()) {
		got[route.Method+" "+route.Path] = route.Tier
	}

	tier, ok := got["POST /api/display-probe"]
	if !ok {
		t.Fatalf("route POST /api/display-probe missing from the production route table")
	}
	if tier != tierPublic {
		t.Fatalf("route POST /api/display-probe tier = %v, want tierPublic", tier)
	}
}

// The probe endpoint is public and unauthenticated, and its log is the
// documented way an operator reads results. Quoting the operator-supplied
// fields means a check name carrying a newline cannot forge a line that
// looks like the probe's own output.
func TestDisplayProbeHandler_QuotesFieldsSoALineCannotBeForged(t *testing.T) {
	buf := captureLogs(t)

	body := map[string]any{
		"user_agent": "probe",
		"viewport":   map[string]any{"w": 1920, "h": 360},
		"rotate":     0,
		"checks": []map[string]any{
			{"name": "ok\ndisplay probe: [PASS] forged", "pass": true, "detail": "x"},
		},
	}
	c, rec := newAuthRequest(t, http.MethodPost, "/api/display-probe", body)
	if err := displayProbeHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	got := buf.String()
	if strings.Contains(got, "\ndisplay probe: [PASS] forged") {
		t.Errorf("a newline in a check name forged a log line:\n%s", got)
	}
	if !strings.Contains(got, `\ndisplay probe:`) {
		t.Errorf("expected the newline to be escaped in the quoted output, got:\n%s", got)
	}
}
