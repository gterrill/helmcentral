package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// newTestWasmTideProviderWithCompanionFiles copies the "valid" tide fixture
// wasm into a fresh t.TempDir() alongside real
// <name>.allowed_hosts.json/<name>.allowed_secrets.json companion files, and
// builds a *wasmTideProvider from it - giving tests a WASM-backed provider
// with a real, disk-backed allowlist state (not just an empty default),
// same as an installed plugin would have.
func newTestWasmTideProviderWithCompanionFiles(t *testing.T, hosts, secrets []string) *wasmTideProvider {
	t.Helper()
	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")

	raw, err := os.ReadFile(validFixtureWasm)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(wasmPath, raw, 0o644); err != nil {
		t.Fatalf("write fixture copy: %v", err)
	}

	hostsJSON, _ := json.Marshal(hosts)
	if err := os.WriteFile(filepath.Join(dir, "plugin.allowed_hosts.json"), hostsJSON, 0o644); err != nil {
		t.Fatalf("write allowed_hosts.json: %v", err)
	}
	secretsJSON, _ := json.Marshal(secrets)
	if err := os.WriteFile(filepath.Join(dir, "plugin.allowed_secrets.json"), secretsJSON, 0o644); err != nil {
		t.Fatalf("write allowed_secrets.json: %v", err)
	}

	provider, err := newWasmTideProvider(wasmPath)
	if err != nil {
		t.Fatalf("newWasmTideProvider: %v", err)
	}
	return provider
}

// nonWasmTideProviderFake is a test-only tideProvider that deliberately does
// NOT implement pluginPathProvider (no Path() method), used to exercise the
// invariant-violation error path in buildPluginInfoResponse/
// postPluginOverridesHandler/deletePluginOverridesHandler
// (backend/plugin_overrides_handlers.go). In production every provider ever
// registered is WASM-backed (implements pluginPathProvider); this fake
// simulates the invariant being violated so the fail-fast behavior is
// actually tested rather than just asserted in comments.
type nonWasmTideProviderFake struct{}

func (nonWasmTideProviderFake) ID() string          { return "fake-non-wasm" }
func (nonWasmTideProviderFake) Name() string        { return "Fake Non-WASM Provider" }
func (nonWasmTideProviderFake) Description() string { return "test-only invariant-violation fixture" }
func (nonWasmTideProviderFake) TTLSeconds() int64   { return 0 }
func (nonWasmTideProviderFake) SearchStations(query string, limit int) []tideStation {
	return nil
}
func (nonWasmTideProviderFake) FetchTideChart(stationID string) (tideChartResult, error) {
	return tideChartResult{}, nil
}

func newPluginTestEchoContext(method, path string, body string, providerType, id string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("type", "id")
	c.SetParamValues(providerType, id)
	return c, rec
}

func TestGetPluginInfoHandler_WasmProviderReturnsSandboxedTrueWithFileBasedAllowlist(t *testing.T) {
	withCleanTideProviderRegistry(t)
	withTestPluginOverridesStore(t)

	provider := newTestWasmTideProviderWithCompanionFiles(t, []string{"example.com"}, []string{"SOME_SECRET"})
	registerTideProvider(provider)

	c, rec := newPluginTestEchoContext(http.MethodGet, "/api/plugins/tide/valid-fixture", "", "tide", "valid-fixture")
	if err := getPluginInfoHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp pluginInfoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if !resp.Sandboxed {
		t.Errorf("expected sandboxed=true for a WASM provider")
	}
	if resp.Type != "tide" || resp.ID != "valid-fixture" || resp.Name != "Valid Fixture Provider" {
		t.Errorf("unexpected type/id/name: %+v", resp)
	}
	if len(resp.AllowedHosts) != 1 || resp.AllowedHosts[0] != "example.com" {
		t.Errorf("expected allowed_hosts [example.com], got %+v", resp.AllowedHosts)
	}
	if len(resp.AllowedSecrets) != 1 || resp.AllowedSecrets[0] != "SOME_SECRET" {
		t.Errorf("expected allowed_secrets [SOME_SECRET], got %+v", resp.AllowedSecrets)
	}
	if resp.AllowedHostsOverridden || resp.AllowedSecretsOverridden {
		t.Errorf("expected both overridden flags false with no saved override, got %+v", resp)
	}
}

func TestGetPluginInfoHandler_NonWasmProviderReturnsInternalServerError(t *testing.T) {
	withCleanTideProviderRegistry(t)

	registerTideProvider(nonWasmTideProviderFake{})

	c, rec := newPluginTestEchoContext(http.MethodGet, "/api/plugins/tide/fake-non-wasm", "", "tide", "fake-non-wasm")
	if err := getPluginInfoHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	var errResp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if !strings.Contains(errResp["error"], "failed to read plugin allowlist state") {
		t.Errorf("expected generic 500 error message, got %+v", errResp)
	}
}

func TestGetPluginInfoHandler_UnknownTypeReturns400(t *testing.T) {
	withCleanTideProviderRegistry(t)

	c, rec := newPluginTestEchoContext(http.MethodGet, "/api/plugins/not-a-type/foo", "", "not-a-type", "foo")
	if err := getPluginInfoHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var errResp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if !strings.Contains(errResp["error"], "not-a-type") {
		t.Errorf("expected error message to name the bad type, got %+v", errResp)
	}
}

func TestGetPluginInfoHandler_UnknownIDReturns404(t *testing.T) {
	withCleanTideProviderRegistry(t)

	c, rec := newPluginTestEchoContext(http.MethodGet, "/api/plugins/tide/no-such-id", "", "tide", "no-such-id")
	if err := getPluginInfoHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
	var errResp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if !strings.Contains(errResp["error"], "no-such-id") {
		t.Errorf("expected error message to name the missing id, got %+v", errResp)
	}
}

func TestPostPluginOverridesHandler_SetsOverrideAndGetReflectsIt(t *testing.T) {
	withCleanTideProviderRegistry(t)
	withTestPluginOverridesStore(t)

	provider := newTestWasmTideProviderWithCompanionFiles(t, []string{"file.example.com"}, nil)
	registerTideProvider(provider)

	body := `{"allowed_hosts":["override.example.com"],"allowed_secrets":["OVERRIDE_SECRET"]}`
	c, rec := newPluginTestEchoContext(http.MethodPost, "/api/plugins/tide/valid-fixture/overrides", body, "tide", "valid-fixture")
	if err := postPluginOverridesHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp pluginInfoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.AllowedHosts) != 1 || resp.AllowedHosts[0] != "override.example.com" {
		t.Errorf("expected overridden allowed_hosts, got %+v", resp.AllowedHosts)
	}
	if !resp.AllowedHostsOverridden || !resp.AllowedSecretsOverridden {
		t.Errorf("expected both overridden flags true after POST, got %+v", resp)
	}

	// A fresh GET must reflect the same overridden state.
	c2, rec2 := newPluginTestEchoContext(http.MethodGet, "/api/plugins/tide/valid-fixture", "", "tide", "valid-fixture")
	if err := getPluginInfoHandler(c2); err != nil {
		t.Fatalf("GET handler returned error: %v", err)
	}
	var resp2 pluginInfoResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("unmarshal GET response: %v", err)
	}
	if len(resp2.AllowedHosts) != 1 || resp2.AllowedHosts[0] != "override.example.com" {
		t.Errorf("expected GET to reflect override, got %+v", resp2.AllowedHosts)
	}
}

func TestDeletePluginOverridesHandler_ClearsOverrideAndGetReverts(t *testing.T) {
	withCleanTideProviderRegistry(t)
	withTestPluginOverridesStore(t)

	provider := newTestWasmTideProviderWithCompanionFiles(t, []string{"file.example.com"}, nil)
	registerTideProvider(provider)

	setBody := `{"allowed_hosts":["override.example.com"],"allowed_secrets":[]}`
	setC, _ := newPluginTestEchoContext(http.MethodPost, "/api/plugins/tide/valid-fixture/overrides", setBody, "tide", "valid-fixture")
	if err := postPluginOverridesHandler(setC); err != nil {
		t.Fatalf("POST handler returned error: %v", err)
	}

	delC, delRec := newPluginTestEchoContext(http.MethodDelete, "/api/plugins/tide/valid-fixture/overrides", "", "tide", "valid-fixture")
	if err := deletePluginOverridesHandler(delC); err != nil {
		t.Fatalf("DELETE handler returned error: %v", err)
	}
	if delRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", delRec.Code, delRec.Body.String())
	}

	var resp pluginInfoResponse
	if err := json.Unmarshal(delRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.AllowedHostsOverridden || resp.AllowedSecretsOverridden {
		t.Errorf("expected overridden flags false after DELETE, got %+v", resp)
	}
	if len(resp.AllowedHosts) != 1 || resp.AllowedHosts[0] != "file.example.com" {
		t.Errorf("expected DELETE to revert to file-based allowed_hosts, got %+v", resp.AllowedHosts)
	}
}

func TestPostPluginOverridesHandler_NonWasmProviderReturnsInternalServerError(t *testing.T) {
	withCleanTideProviderRegistry(t)
	withTestPluginOverridesStore(t)

	registerTideProvider(nonWasmTideProviderFake{})

	body := `{"allowed_hosts":["example.com"],"allowed_secrets":[]}`
	c, rec := newPluginTestEchoContext(http.MethodPost, "/api/plugins/tide/fake-non-wasm/overrides", body, "tide", "fake-non-wasm")
	if err := postPluginOverridesHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	var errResp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if !strings.Contains(errResp["error"], "fake-non-wasm") || !strings.Contains(errResp["error"], "pluginPathProvider") {
		t.Errorf("expected error naming the provider and the violated invariant, got %+v", errResp)
	}
}

func TestDeletePluginOverridesHandler_NonWasmProviderReturnsInternalServerError(t *testing.T) {
	withCleanTideProviderRegistry(t)
	withTestPluginOverridesStore(t)

	registerTideProvider(nonWasmTideProviderFake{})

	c, rec := newPluginTestEchoContext(http.MethodDelete, "/api/plugins/tide/fake-non-wasm/overrides", "", "tide", "fake-non-wasm")
	if err := deletePluginOverridesHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	var errResp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if !strings.Contains(errResp["error"], "fake-non-wasm") || !strings.Contains(errResp["error"], "pluginPathProvider") {
		t.Errorf("expected error naming the provider and the violated invariant, got %+v", errResp)
	}
}

func TestPostPluginOverridesHandler_UnknownTypeReturns400(t *testing.T) {
	c, rec := newPluginTestEchoContext(http.MethodPost, "/api/plugins/not-a-type/foo/overrides", `{}`, "not-a-type", "foo")
	if err := postPluginOverridesHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetPluginInfoHandler_UnknownIDForEachValidType(t *testing.T) {
	withCleanTideProviderRegistry(t)
	withCleanWeatherProviderRegistry(t)
	withCleanWaveProviderRegistry(t)
	withCleanForecastWarningsProviderRegistry(t)

	for _, providerType := range []string{"tide", "weather", "wave", "forecast-warnings"} {
		c, rec := newPluginTestEchoContext(http.MethodGet, "/api/plugins/"+providerType+"/missing", "", providerType, "missing")
		if err := getPluginInfoHandler(c); err != nil {
			t.Fatalf("[%s] handler returned error: %v", providerType, err)
		}
		if rec.Code != http.StatusNotFound {
			t.Fatalf("[%s] expected 404, got %d: %s", providerType, rec.Code, rec.Body.String())
		}
	}
}
