package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/labstack/echo/v4"
)

// ── test setup ───────────────────────────────────────────────────────────────

func setupAutopilotTest(t *testing.T) {
	t.Helper()
	t.Setenv("SIGNALK_USERNAME", "")
	t.Setenv("SIGNALK_PASSWORD", "")
	invalidateSignalKToken()
	invalidateAutopilotIDCache()
	t.Cleanup(invalidateSignalKToken)
	t.Cleanup(invalidateAutopilotIDCache)
}

// settingsFileForServerWithAutopilotID mirrors settingsFileForServer (tracks_test.go)
// but also sets ui.autopilot_id, for the two-pilot / configured-id test cases.
func settingsFileForServerWithAutopilotID(t *testing.T, serverURL, autopilotID string) string {
	t.Helper()
	content := []byte("signalk:\n  address: " + serverURL + "\n  port: 3000\nui:\n  autopilot_id: " + autopilotID + "\n")
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("could not write temp settings: %v", err)
	}
	return path
}

func newAutopilotRequest(t *testing.T, method, path string, body any) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()

	var req *http.Request
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		req = httptest.NewRequest(method, path, strings.NewReader(string(payload)))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}

	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

const (
	autopilotsListPath     = "/signalk/v2/api/vessels/self/autopilots"
	autopilotDefaultIDPath = "/signalk/v2/api/vessels/self/autopilots/_providers/_default"
)

func autopilotIDPath(id string) string { return autopilotsListPath + "/" + id }

// ── resolveAutopilotID ──────────────────────────────────────────────────────

func TestResolveAutopilotID_UsesConfiguredIDWithoutNetworkCall(t *testing.T) {
	setupAutopilotTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()
	// No responses registered at all — a network call would 404 and fail the test.

	settingsPath := settingsFileForServerWithAutopilotID(t, srv.URL, "my-pilot")

	id, err := resolveAutopilotID(srv.URL, settingsPath)
	if err != nil {
		t.Fatalf("resolveAutopilotID: unexpected error: %v", err)
	}
	if id != "my-pilot" {
		t.Fatalf("expected configured id 'my-pilot', got %q", id)
	}
	if len(rs.calls()) != 0 {
		t.Fatalf("expected no upstream calls when ui.autopilot_id is configured, got %+v", rs.calls())
	}
}

func TestResolveAutopilotID_ResolvesDefaultAndCaches(t *testing.T) {
	setupAutopilotTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodGet, autopilotsListPath, http.StatusOK, `{"pilot-a":{"provider":"p","isDefault":true}}`)
	rs.on(http.MethodGet, autopilotDefaultIDPath, http.StatusOK, `{"id":"pilot-a"}`)

	settingsPath := settingsFileForServer(t, srv.URL)

	id, err := resolveAutopilotID(srv.URL, settingsPath)
	if err != nil {
		t.Fatalf("resolveAutopilotID: unexpected error: %v", err)
	}
	if id != "pilot-a" {
		t.Fatalf("expected 'pilot-a', got %q", id)
	}

	// Second call within the cache TTL must not hit the network again.
	id2, err := resolveAutopilotID(srv.URL, settingsPath)
	if err != nil {
		t.Fatalf("resolveAutopilotID (cached): unexpected error: %v", err)
	}
	if id2 != "pilot-a" {
		t.Fatalf("expected cached 'pilot-a', got %q", id2)
	}

	calls := rs.calls()
	if len(calls) != 2 {
		t.Fatalf("expected exactly 2 upstream calls (list + default) across both resolutions, got %d: %+v", len(calls), calls)
	}
}

func TestResolveAutopilotID_NoAutopilotPresentIsExplicitAbsence(t *testing.T) {
	setupAutopilotTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodGet, autopilotsListPath, http.StatusOK, `{}`)

	settingsPath := settingsFileForServer(t, srv.URL)

	_, err := resolveAutopilotID(srv.URL, settingsPath)
	if err == nil {
		t.Fatal("expected an error when no autopilot is present")
	}
	if !isAutopilotNotPresentErr(err) {
		t.Fatalf("expected errAutopilotNotPresent, got %v", err)
	}
}

func isAutopilotNotPresentErr(err error) bool {
	return err == errAutopilotNotPresent
}

// ── GET /api/autopilot ───────────────────────────────────────────────────────

func TestGetAutopilotHandler_ExplicitAbsenceWhenNoProvider(t *testing.T) {
	setupAutopilotTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodGet, autopilotsListPath, http.StatusOK, `{}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	c, rec := newAutopilotRequest(t, http.MethodGet, "/api/autopilot", nil)
	if err := getAutopilotHandler(c); err != nil {
		t.Fatalf("getAutopilotHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("could not parse response: %v", err)
	}
	if present, _ := body["present"].(bool); present {
		t.Fatalf("expected present:false, got %+v", body)
	}
	if _, hasEngaged := body["engaged"]; hasEngaged {
		t.Fatalf("must not synthesize an engaged field when absent, got %+v", body)
	}
}

func TestGetAutopilotHandler_ProxiesUpstreamStatusVerbatim(t *testing.T) {
	setupAutopilotTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodGet, autopilotsListPath, http.StatusOK, `{"pilot-a":{"provider":"p","isDefault":true}}`)
	rs.on(http.MethodGet, autopilotDefaultIDPath, http.StatusOK, `{"id":"pilot-a"}`)
	rs.on(http.MethodGet, autopilotIDPath("pilot-a"), http.StatusOK, `{"state":"auto","mode":"compass","target":135.5,"engaged":true}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	c, rec := newAutopilotRequest(t, http.MethodGet, "/api/autopilot", nil)
	if err := getAutopilotHandler(c); err != nil {
		t.Fatalf("getAutopilotHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("could not parse response: %v", err)
	}
	if present, _ := body["present"].(bool); !present {
		t.Fatalf("expected present:true, got %+v", body)
	}
	if body["id"] != "pilot-a" {
		t.Fatalf("expected id 'pilot-a', got %v", body["id"])
	}
	if body["state"] != "auto" || body["mode"] != "compass" {
		t.Fatalf("expected upstream fields to survive the proxy verbatim, got %+v", body)
	}
	if engaged, _ := body["engaged"].(bool); !engaged {
		t.Fatalf("expected engaged:true, got %+v", body)
	}
}

func TestGetAutopilotHandler_SurfacesUpstreamErrorStatusAndBody(t *testing.T) {
	setupAutopilotTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodGet, autopilotsListPath, http.StatusOK, `{"pilot-a":{"provider":"p","isDefault":true}}`)
	rs.on(http.MethodGet, autopilotDefaultIDPath, http.StatusOK, `{"id":"pilot-a"}`)
	rs.on(http.MethodGet, autopilotIDPath("pilot-a"), http.StatusServiceUnavailable, `{"message":"provider offline"}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	c, rec := newAutopilotRequest(t, http.MethodGet, "/api/autopilot", nil)
	if err := getAutopilotHandler(c); err != nil {
		t.Fatalf("getAutopilotHandler returned error: %v", err)
	}
	// The handler must not flatten this to a generic 502 — SK's own status
	// and body are the actionable information.
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected upstream status %d to be proxied verbatim, got %d", http.StatusServiceUnavailable, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "provider offline") {
		t.Fatalf("expected signalk's raw error body to be surfaced, got %s", rec.Body.String())
	}
}

// ── control endpoints: method, path, and body-envelope shape ────────────────

func setupResolvedPilot(t *testing.T, rs *recordingServer, id string) {
	t.Helper()
	rs.on(http.MethodGet, autopilotsListPath, http.StatusOK, `{"`+id+`":{"provider":"p","isDefault":true}}`)
	rs.on(http.MethodGet, autopilotDefaultIDPath, http.StatusOK, `{"id":"`+id+`"}`)
}

func TestPostAutopilotEngageHandler_PostsWithNoBodyEnvelope(t *testing.T) {
	setupAutopilotTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()
	setupResolvedPilot(t, rs, "pilot-a")
	rs.on(http.MethodPost, autopilotIDPath("pilot-a")+"/engage", http.StatusOK, `{}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	c, rec := newAutopilotRequest(t, http.MethodPost, "/api/autopilot/engage", nil)
	if err := postAutopilotEngageHandler(c); err != nil {
		t.Fatalf("postAutopilotEngageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	calls := rs.calls()
	last := calls[len(calls)-1]
	if last.Method != http.MethodPost || last.Path != autopilotIDPath("pilot-a")+"/engage" {
		t.Fatalf("expected POST %s, got %s %s", autopilotIDPath("pilot-a")+"/engage", last.Method, last.Path)
	}
	if len(last.Body) != 0 {
		t.Fatalf("expected no request body on engage, got %s", last.Body)
	}
}

func TestPostAutopilotDisengageHandler_PostsWithNoBodyEnvelope(t *testing.T) {
	setupAutopilotTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()
	setupResolvedPilot(t, rs, "pilot-a")
	rs.on(http.MethodPost, autopilotIDPath("pilot-a")+"/disengage", http.StatusOK, `{}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	c, rec := newAutopilotRequest(t, http.MethodPost, "/api/autopilot/disengage", nil)
	if err := postAutopilotDisengageHandler(c); err != nil {
		t.Fatalf("postAutopilotDisengageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	calls := rs.calls()
	last := calls[len(calls)-1]
	if last.Method != http.MethodPost || last.Path != autopilotIDPath("pilot-a")+"/disengage" {
		t.Fatalf("expected POST .../disengage, got %s %s", last.Method, last.Path)
	}
}

func TestPutAutopilotStateHandler_WrapsBodyInValueEnvelope(t *testing.T) {
	setupAutopilotTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()
	setupResolvedPilot(t, rs, "pilot-a")
	rs.on(http.MethodPut, autopilotIDPath("pilot-a")+"/state", http.StatusOK, `{}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	c, rec := newAutopilotRequest(t, http.MethodPut, "/api/autopilot/state", map[string]any{"state": "auto"})
	if err := putAutopilotStateHandler(c); err != nil {
		t.Fatalf("putAutopilotStateHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	calls := rs.calls()
	last := calls[len(calls)-1]
	if last.Method != http.MethodPut || last.Path != autopilotIDPath("pilot-a")+"/state" {
		t.Fatalf("expected PUT .../state, got %s %s", last.Method, last.Path)
	}
	var sent map[string]any
	if err := json.Unmarshal(last.Body, &sent); err != nil {
		t.Fatalf("could not parse PUT body: %v", err)
	}
	if sent["value"] != "auto" {
		t.Fatalf("expected body {\"value\":\"auto\"}, got %+v", sent)
	}
}

func TestPutAutopilotModeHandler_WrapsBodyInValueEnvelope(t *testing.T) {
	setupAutopilotTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()
	setupResolvedPilot(t, rs, "pilot-a")
	rs.on(http.MethodPut, autopilotIDPath("pilot-a")+"/mode", http.StatusOK, `{}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	c, rec := newAutopilotRequest(t, http.MethodPut, "/api/autopilot/mode", map[string]any{"mode": "wind"})
	if err := putAutopilotModeHandler(c); err != nil {
		t.Fatalf("putAutopilotModeHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	calls := rs.calls()
	last := calls[len(calls)-1]
	var sent map[string]any
	if err := json.Unmarshal(last.Body, &sent); err != nil {
		t.Fatalf("could not parse PUT body: %v", err)
	}
	if sent["value"] != "wind" {
		t.Fatalf("expected body {\"value\":\"wind\"}, got %+v", sent)
	}
}

func TestPutAutopilotTargetHandler_SendsValueAndDegreesUnits(t *testing.T) {
	setupAutopilotTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()
	setupResolvedPilot(t, rs, "pilot-a")
	rs.on(http.MethodPut, autopilotIDPath("pilot-a")+"/target", http.StatusOK, `{}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	c, rec := newAutopilotRequest(t, http.MethodPut, "/api/autopilot/target", map[string]any{"value": 135})
	if err := putAutopilotTargetHandler(c); err != nil {
		t.Fatalf("putAutopilotTargetHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	calls := rs.calls()
	last := calls[len(calls)-1]
	var sent map[string]any
	if err := json.Unmarshal(last.Body, &sent); err != nil {
		t.Fatalf("could not parse PUT body: %v", err)
	}
	if sent["value"] != float64(135) || sent["units"] != "deg" {
		t.Fatalf("expected body {\"value\":135,\"units\":\"deg\"}, got %+v", sent)
	}
}

func TestPutAutopilotTargetAdjustHandler_SendsValueAndDegreesUnits(t *testing.T) {
	setupAutopilotTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()
	setupResolvedPilot(t, rs, "pilot-a")
	rs.on(http.MethodPut, autopilotIDPath("pilot-a")+"/target/adjust", http.StatusOK, `{}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	c, rec := newAutopilotRequest(t, http.MethodPut, "/api/autopilot/target/adjust", map[string]any{"value": -10})
	if err := putAutopilotTargetAdjustHandler(c); err != nil {
		t.Fatalf("putAutopilotTargetAdjustHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	calls := rs.calls()
	last := calls[len(calls)-1]
	if last.Path != autopilotIDPath("pilot-a")+"/target/adjust" {
		t.Fatalf("expected .../target/adjust, got %s", last.Path)
	}
	var sent map[string]any
	if err := json.Unmarshal(last.Body, &sent); err != nil {
		t.Fatalf("could not parse PUT body: %v", err)
	}
	if sent["value"] != float64(-10) || sent["units"] != "deg" {
		t.Fatalf("expected body {\"value\":-10,\"units\":\"deg\"}, got %+v", sent)
	}
}

func TestPostAutopilotTackHandler_ValidSide(t *testing.T) {
	setupAutopilotTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()
	setupResolvedPilot(t, rs, "pilot-a")
	rs.on(http.MethodPost, autopilotIDPath("pilot-a")+"/tack/port", http.StatusOK, `{}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	c, rec := newAutopilotRequest(t, http.MethodPost, "/api/autopilot/tack/port", nil)
	c.SetParamNames("side")
	c.SetParamValues("port")
	if err := postAutopilotTackHandler(c); err != nil {
		t.Fatalf("postAutopilotTackHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	calls := rs.calls()
	last := calls[len(calls)-1]
	if last.Path != autopilotIDPath("pilot-a")+"/tack/port" {
		t.Fatalf("expected .../tack/port, got %s", last.Path)
	}
}

func TestPostAutopilotTackHandler_RejectsInvalidSideWithoutCallingUpstream(t *testing.T) {
	setupAutopilotTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()
	setupResolvedPilot(t, rs, "pilot-a")

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	c, rec := newAutopilotRequest(t, http.MethodPost, "/api/autopilot/tack/sideways", nil)
	c.SetParamNames("side")
	c.SetParamValues("sideways")
	if err := postAutopilotTackHandler(c); err != nil {
		t.Fatalf("postAutopilotTackHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	for _, call := range rs.calls() {
		if strings.Contains(call.Path, "/tack/") {
			t.Fatalf("must not call upstream with an invalid side, got %+v", call)
		}
	}
}

func TestPostAutopilotGybeHandler_ValidSide(t *testing.T) {
	setupAutopilotTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()
	setupResolvedPilot(t, rs, "pilot-a")
	rs.on(http.MethodPost, autopilotIDPath("pilot-a")+"/gybe/starboard", http.StatusOK, `{}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	c, rec := newAutopilotRequest(t, http.MethodPost, "/api/autopilot/gybe/starboard", nil)
	c.SetParamNames("side")
	c.SetParamValues("starboard")
	if err := postAutopilotGybeHandler(c); err != nil {
		t.Fatalf("postAutopilotGybeHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPutAutopilotDodgeHandler_SendsValueAndDegreesUnits(t *testing.T) {
	setupAutopilotTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()
	setupResolvedPilot(t, rs, "pilot-a")
	rs.on(http.MethodPut, autopilotIDPath("pilot-a")+"/dodge", http.StatusOK, `{}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	c, rec := newAutopilotRequest(t, http.MethodPut, "/api/autopilot/dodge", map[string]any{"value": 5})
	if err := putAutopilotDodgeHandler(c); err != nil {
		t.Fatalf("putAutopilotDodgeHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	calls := rs.calls()
	last := calls[len(calls)-1]
	var sent map[string]any
	if err := json.Unmarshal(last.Body, &sent); err != nil {
		t.Fatalf("could not parse PUT body: %v", err)
	}
	if sent["value"] != float64(5) || sent["units"] != "deg" {
		t.Fatalf("expected body {\"value\":5,\"units\":\"deg\"}, got %+v", sent)
	}
}

func TestDeleteAutopilotDodgeHandler_NoBody(t *testing.T) {
	setupAutopilotTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()
	setupResolvedPilot(t, rs, "pilot-a")
	rs.on(http.MethodDelete, autopilotIDPath("pilot-a")+"/dodge", http.StatusOK, `{}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	c, rec := newAutopilotRequest(t, http.MethodDelete, "/api/autopilot/dodge", nil)
	if err := deleteAutopilotDodgeHandler(c); err != nil {
		t.Fatalf("deleteAutopilotDodgeHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	calls := rs.calls()
	last := calls[len(calls)-1]
	if last.Method != http.MethodDelete || len(last.Body) != 0 {
		t.Fatalf("expected bodyless DELETE, got method=%s body=%s", last.Method, last.Body)
	}
}

func TestAutopilotControlHandler_NotFoundWhenNoAutopilotPresent(t *testing.T) {
	setupAutopilotTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()
	rs.on(http.MethodGet, autopilotsListPath, http.StatusOK, `{}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	c, rec := newAutopilotRequest(t, http.MethodPost, "/api/autopilot/engage", nil)
	if err := postAutopilotEngageHandler(c); err != nil {
		t.Fatalf("postAutopilotEngageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when no autopilot is present, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── token-expiry retry ───────────────────────────────────────────────────────

func TestAutopilotControlHandler_RetriesTokenExactlyOnce(t *testing.T) {
	setupAutopilotTest(t)
	t.Setenv("SIGNALK_USERNAME", "u")
	t.Setenv("SIGNALK_PASSWORD", "p")

	srv, rs := newRecordingServer(t)
	defer srv.Close()
	setupResolvedPilot(t, rs, "pilot-a")

	var mu sync.Mutex
	engageCalls := 0
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/signalk/v1/auth/login" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token":"tok","timeToLive":3600}`))
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == autopilotsListPath {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"pilot-a":{"provider":"p","isDefault":true}}`))
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == autopilotDefaultIDPath {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"pilot-a"}`))
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == autopilotIDPath("pilot-a")+"/engage" {
			mu.Lock()
			engageCalls++
			n := engageCalls
			mu.Unlock()
			if n == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	})

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	c, rec := newAutopilotRequest(t, http.MethodPost, "/api/autopilot/engage", nil)
	if err := postAutopilotEngageHandler(c); err != nil {
		t.Fatalf("postAutopilotEngageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected the retry to succeed with 200, got %d: %s", rec.Code, rec.Body.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if engageCalls != 2 {
		t.Fatalf("expected exactly 2 attempts (401 then success), got %d", engageCalls)
	}
}
