package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"
)

func historyRequest(t *testing.T, query string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/telemetry/history?"+query, nil)
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

// No InfluxDB configured must be an explicit refusal, never 200 with an empty
// array: a trend gauge rendering a flat empty line because nobody stood up a
// database is exactly the masking AGENTS.md's fallback policy forbids.
func TestTelemetryHistoryRequiresInflux(t *testing.T) {
	t.Setenv("SETTINGS_FILE", filepath.Join(t.TempDir(), "settings.yaml"))

	c, rec := historyRequest(t, "path=propulsion.port.oilPressure&window=3h")
	if err := telemetryHistoryHandler(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without InfluxDB, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if body["error"] == nil || body["error"] == "" {
		t.Fatalf("expected a reason the caller can show, got %v", body)
	}
}

func TestTelemetryHistoryRejectsBadInput(t *testing.T) {
	t.Setenv("SETTINGS_FILE", filepath.Join(t.TempDir(), "settings.yaml"))

	cases := []struct{ name, query string }{
		{"missing path", "window=3h"},
		{"blank path", "path=%20&window=3h"},
		// window is interpolated into Flux, so it is an allowlist, not a parse.
		{"unknown window", "path=a.b&window=99y"},
		{"flux injection via window", `path=a.b&window=1h)%20|%3E%20yield(`},
	}

	for _, tc := range cases {
		c, rec := historyRequest(t, tc.query)
		if err := telemetryHistoryHandler(c); err != nil {
			t.Fatalf("%s: handler error: %v", tc.name, err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d", tc.name, rec.Code)
		}
	}
}

func TestTelemetryHistoryWindowAllowlist(t *testing.T) {
	for _, window := range telemetryHistoryWindows {
		if !validTelemetryHistoryWindow(window) {
			t.Fatalf("expected %q to be allowed", window)
		}
	}
	if validTelemetryHistoryWindow("1h) |> yield(") {
		t.Fatal("expected an injection attempt to be refused")
	}
	if validTelemetryHistoryWindow("") {
		t.Fatal("expected a blank window to be refused")
	}
}
