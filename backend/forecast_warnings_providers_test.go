package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// withCleanForecastWarningsProviderRegistry saves the global forecast
// warnings provider registry, resets it to empty for the duration of the
// test, and restores the original afterwards - mirrors
// withCleanWaveProviderRegistry.
func withCleanForecastWarningsProviderRegistry(t *testing.T) {
	t.Helper()
	origRegistry := forecastWarningsProviderRegistry
	origOrder := forecastWarningsProviderOrder
	forecastWarningsProviderRegistry = map[string]forecastWarningsProvider{}
	forecastWarningsProviderOrder = nil
	t.Cleanup(func() {
		forecastWarningsProviderRegistry = origRegistry
		forecastWarningsProviderOrder = origOrder
	})
}

type stubForecastWarningsProvider struct {
	id     string
	name   string
	ttl    int64
	bundle forecastWarningsBundle
	err    error
}

func (s *stubForecastWarningsProvider) ID() string   { return s.id }
func (s *stubForecastWarningsProvider) Name() string { return s.name }
func (s *stubForecastWarningsProvider) Description() string {
	return "Stub forecast warnings provider for tests"
}
func (s *stubForecastWarningsProvider) TTLSeconds() int64 { return s.ttl }
func (s *stubForecastWarningsProvider) FetchWarnings(lat, lon float64) (forecastWarningsBundle, error) {
	if s.err != nil {
		return forecastWarningsBundle{}, s.err
	}
	return s.bundle, nil
}

func writeForecastWarningsSettings(t *testing.T, provider, signalkURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.yaml")
	content := fmt.Sprintf("signalk:\n  address: %s\n  port: 0\nui:\n  forecast_warnings_provider: %s\n", signalkURL, provider)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test settings file: %v", err)
	}
	return path
}

// trustedSignalKPayloadServer is defined in signalk_test.go and reused here.

func TestRegisterForecastWarningsProvider_PreservesRegistrationOrder(t *testing.T) {
	withCleanForecastWarningsProviderRegistry(t)

	registerForecastWarningsProvider(&stubForecastWarningsProvider{id: "b", name: "B"})
	registerForecastWarningsProvider(&stubForecastWarningsProvider{id: "a", name: "A"})

	if len(forecastWarningsProviderOrder) != 2 || forecastWarningsProviderOrder[0] != "b" || forecastWarningsProviderOrder[1] != "a" {
		t.Fatalf("expected order [b a], got %v", forecastWarningsProviderOrder)
	}

	if _, ok := getForecastWarningsProvider("a"); !ok {
		t.Fatalf("expected provider a to be registered")
	}
	if _, ok := getForecastWarningsProvider("missing"); ok {
		t.Fatalf("expected provider 'missing' to not be registered")
	}
}

func TestForecastWarningsProvidersHandler_ReturnsRegisteredProvidersInOrder(t *testing.T) {
	withCleanForecastWarningsProviderRegistry(t)
	registerForecastWarningsProvider(&stubForecastWarningsProvider{id: "bom", name: "BOM"})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/forecast-warnings-providers", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := forecastWarningsProvidersHandler(c); err != nil {
		t.Fatalf("forecastWarningsProvidersHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload []forecastWarningsProviderInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(payload) != 1 || payload[0].ID != "bom" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestForecastWarningsHandler_ReturnsBadGatewayForUnknownProvider(t *testing.T) {
	withCleanForecastWarningsProviderRegistry(t)
	settingsPath := writeForecastWarningsSettings(t, "not-a-real-provider", "http://127.0.0.1:1")
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/forecast-warnings", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := forecastWarningsHandler(c); err != nil {
		t.Fatalf("forecastWarningsHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected a non-empty error message")
	}
}

// TestForecastWarningsHandler_502sWithZeroRegisteredProviders proves that a
// freshly-booted install with no plugins/forecast-warnings/*.wasm installed
// yet (the WASM-only-registry, no-native-built-in state main.go leaves
// things in) fails loudly instead of ever returning fake data.
func TestForecastWarningsHandler_502sWithZeroRegisteredProviders(t *testing.T) {
	withCleanForecastWarningsProviderRegistry(t)
	settingsPath := writeForecastWarningsSettings(t, "bom", "http://127.0.0.1:1")
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/forecast-warnings", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := forecastWarningsHandler(c); err != nil {
		t.Fatalf("forecastWarningsHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 with no registered forecast warnings providers, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected a non-empty error message")
	}
}

func TestForecastWarningsHandler_ReturnsBadGatewayWhenProviderFetchFails(t *testing.T) {
	withCleanForecastWarningsProviderRegistry(t)
	registerForecastWarningsProvider(&stubForecastWarningsProvider{id: "bom", name: "BOM", ttl: 5400, err: fmt.Errorf("simulated upstream failure")})

	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()

	settingsPath := writeForecastWarningsSettings(t, "bom", server.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/forecast-warnings", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := forecastWarningsHandler(c); err != nil {
		t.Fatalf("forecastWarningsHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on fetch error (no fake defaults), got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestForecastWarningsHandler_ReturnsOKWithEmptyBulletins proves that an
// empty bulletins list on a successful fetch is NOT an error - "no warnings
// currently active" is a legitimate result, unlike wave/weather's
// empty-response-is-502 behavior.
func TestForecastWarningsHandler_ReturnsOKWithEmptyBulletins(t *testing.T) {
	withCleanForecastWarningsProviderRegistry(t)
	registerForecastWarningsProvider(&stubForecastWarningsProvider{
		id: "bom", name: "BOM", ttl: 5400,
		bundle: forecastWarningsBundle{Region: "QLD — Capricornia Coast", Bulletins: nil, CachedAt: time.Now().UTC()},
	})

	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()

	settingsPath := writeForecastWarningsSettings(t, "bom", server.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/forecast-warnings", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := forecastWarningsHandler(c); err != nil {
		t.Fatalf("forecastWarningsHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for an empty (but successful) bulletins list, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var payload forecastWarningsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(payload.Bulletins) != 0 {
		t.Fatalf("expected 0 bulletins, got %d", len(payload.Bulletins))
	}
	if payload.Region != "QLD — Capricornia Coast" {
		t.Fatalf("unexpected region: %q", payload.Region)
	}
}

func TestForecastWarningsHandler_ReturnsOKAndMapsBulletinFieldsIncludingIssuedAtOmission(t *testing.T) {
	withCleanForecastWarningsProviderRegistry(t)

	issuedAt := time.Date(2026, 7, 5, 11, 51, 0, 0, time.UTC)
	bundle := forecastWarningsBundle{
		Region: "QLD — Capricornia Coast",
		Bulletins: []forecastWarningBulletin{
			{
				ID:         "IDQ20085",
				Title:      "Strong Wind Warning",
				Category:   "wind",
				IssuedAt:   issuedAt,
				DetailsURL: "https://www.bom.gov.au/fwo/IDQ20085.txt",
				Sections: []forecastWarningSection{
					{Day: "Sunday", WarningType: "Strong Wind Warning"},
					{Day: "Monday", WarningType: "Strong Wind Warning"},
				},
			},
			{
				ID:         "IDQ20083",
				Title:      "Marine Wind Warning",
				Category:   "wind",
				IssuedAt:   time.Time{}, // unknown - must be omitted, not a fake epoch timestamp
				DetailsURL: "https://www.bom.gov.au/fwo/IDQ20083.txt",
				Sections: []forecastWarningSection{
					{Day: "Sunday", WarningType: "Gale Warning"},
				},
			},
		},
		Cached:   true,
		CachedAt: time.Now().UTC(),
	}
	registerForecastWarningsProvider(&stubForecastWarningsProvider{id: "bom", name: "BOM", ttl: 5400, bundle: bundle})

	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()

	settingsPath := writeForecastWarningsSettings(t, "bom", server.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/forecast-warnings", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := forecastWarningsHandler(c); err != nil {
		t.Fatalf("forecastWarningsHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if bodyContains(body, `"issued_at":""`) {
		t.Fatalf("expected issued_at to be omitted (not an empty string) when zero, got: %s", body)
	}

	var payload forecastWarningsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if payload.Provider != "bom" {
		t.Fatalf("expected provider bom, got %q", payload.Provider)
	}
	if payload.Region != "QLD — Capricornia Coast" {
		t.Fatalf("unexpected region: %q", payload.Region)
	}
	if payload.TTLSeconds != 5400 {
		t.Fatalf("expected ttl_seconds 5400, got %d", payload.TTLSeconds)
	}
	if !payload.Cached {
		t.Fatalf("expected cached true")
	}
	if len(payload.Bulletins) != 2 {
		t.Fatalf("expected 2 bulletins, got %d: %+v", len(payload.Bulletins), payload.Bulletins)
	}

	first := payload.Bulletins[0]
	if first.ID != "IDQ20085" || first.Title != "Strong Wind Warning" || first.Category != "wind" {
		t.Fatalf("unexpected first bulletin: %+v", first)
	}
	if first.IssuedAt != issuedAt.Format(time.RFC3339) {
		t.Fatalf("expected issued_at %q, got %q", issuedAt.Format(time.RFC3339), first.IssuedAt)
	}
	if len(first.Sections) != 2 {
		t.Fatalf("expected 2 sections on first bulletin, got %d", len(first.Sections))
	}
	if first.Sections[0].Day != "Sunday" || first.Sections[0].WarningType != "Strong Wind Warning" {
		t.Fatalf("unexpected first section: %+v", first.Sections[0])
	}

	second := payload.Bulletins[1]
	if second.ID != "IDQ20083" {
		t.Fatalf("unexpected second bulletin id: %q", second.ID)
	}
	if second.IssuedAt != "" {
		t.Fatalf("expected empty issued_at for the second bulletin, got %q", second.IssuedAt)
	}
	if len(second.Sections) != 1 {
		t.Fatalf("expected 1 section on second bulletin, got %d", len(second.Sections))
	}
}

func TestResolveForecastWarningsProvider_DefaultsToBom(t *testing.T) {
	withCleanForecastWarningsProviderRegistry(t)
	registerForecastWarningsProvider(&stubForecastWarningsProvider{id: "bom", name: "BOM", ttl: 5400})

	path := filepath.Join(t.TempDir(), "settings.yaml")
	if err := os.WriteFile(path, []byte("signalk:\n  address: localhost\n  port: 0\n"), 0o644); err != nil {
		t.Fatalf("failed to write settings: %v", err)
	}

	provider, configured, err := resolveForecastWarningsProvider(path)
	if err != nil {
		t.Fatalf("resolveForecastWarningsProvider returned error: %v", err)
	}
	if configured != "bom" {
		t.Fatalf("expected default configured provider 'bom', got %q", configured)
	}
	if provider.ID() != "bom" {
		t.Fatalf("expected resolved provider id 'bom', got %q", provider.ID())
	}
}
