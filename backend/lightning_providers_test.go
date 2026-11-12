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

func withCleanLightningProviderRegistry(t *testing.T) {
	t.Helper()
	origRegistry := lightningProviderRegistry
	origOrder := lightningProviderOrder
	lightningProviderRegistry = map[string]lightningProvider{}
	lightningProviderOrder = nil
	t.Cleanup(func() {
		lightningProviderRegistry = origRegistry
		lightningProviderOrder = origOrder
	})
}

// withLightningScheduler gives the provider a schedule that saves into a temp
// dir and runs its refreshes inline, so a request sees their result.
func withLightningScheduler(t *testing.T, id string) *lightningScheduler {
	t.Helper()
	s := newLightningScheduler(filepath.Join(t.TempDir(), "lightning.json"))
	s.spawn = func(f func()) { f() }
	lightningSchedulersMu.Lock()
	orig := lightningSchedulers
	lightningSchedulers = map[string]*lightningScheduler{id: s}
	lightningSchedulersMu.Unlock()
	t.Cleanup(func() {
		lightningSchedulersMu.Lock()
		lightningSchedulers = orig
		lightningSchedulersMu.Unlock()
	})
	return s
}

func withLightningClock(t *testing.T, now time.Time) {
	t.Helper()
	orig := lightningNow
	lightningNow = func() time.Time { return now }
	t.Cleanup(func() { lightningNow = orig })
}

// stubLightningProvider answers every span with one window per 3 hours of it,
// all at the given probability, from a fixed run.
type stubLightningProvider struct {
	id          string
	run         time.Time
	probability float64
	err         error
	// skip names window ends the run does not have.
	skip  map[time.Time]bool
	calls []stubLightningCall
}

type stubLightningCall struct {
	lat, lon float64
	from, to time.Time
	// runIn is the run the host asked to be read; zero means discover.
	runIn time.Time
}

func (s *stubLightningProvider) ID() string          { return s.id }
func (s *stubLightningProvider) Name() string        { return "Stub" }
func (s *stubLightningProvider) Description() string { return "Stub lightning provider for tests" }
func (s *stubLightningProvider) TTLSeconds() int64   { return 21600 }
func (s *stubLightningProvider) FetchLightning(lat, lon float64, from, to, run time.Time) (lightningSpan, error) {
	s.calls = append(s.calls, stubLightningCall{lat, lon, from, to, run})
	if s.err != nil {
		return lightningSpan{}, s.err
	}
	span := lightningSpan{Run: s.run}
	for end := from.Truncate(3 * time.Hour).Add(3 * time.Hour); !end.After(to); end = end.Add(3 * time.Hour) {
		if s.skip[end] {
			continue
		}
		span.Windows = append(span.Windows, lightningWindow{Start: end.Add(-3 * time.Hour), End: end, Probability: s.probability})
	}
	return span, nil
}

func writeLightningSettings(t *testing.T, provider, signalkURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.yaml")
	content := fmt.Sprintf("signalk:\n  address: %s\n  port: 0\n", signalkURL)
	if provider != "" {
		content += fmt.Sprintf("ui:\n  lightning_provider: %s\n", provider)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test settings: %v", err)
	}
	return path
}

func getLightning(t *testing.T) (*httptest.ResponseRecorder, lightningResponse) {
	t.Helper()
	e := echo.New()
	rec := httptest.NewRecorder()
	if err := lightningForecast(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/lightning", nil), rec)); err != nil {
		t.Fatalf("lightningForecast returned error: %v", err)
	}
	var payload lightningResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("failed to parse: %v", err)
		}
	}
	return rec, payload
}

func TestLightningCellIndex(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lat, lon float64
		row, col int
	}{
		{"cell centre", 0.09375, 0.140625, 480, 640},
		{"v[638][366]", 29.71875, -76.921875, 638, 366},
		{"v[197][453]", -52.96875, -52.453125, 197, 453},
		{"north pole", 90, 0, 959, 640},
		{"south pole", -90, 0, 0, 640},
		{"lon 180 wraps", 0, 180, 480, 0},
		{"lon 0..360 form", 29.71875, 360 - 76.921875, 638, 366},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row, col := lightningCellIndex(tc.lat, tc.lon)
			if row != tc.row || col != tc.col {
				t.Fatalf("lightningCellIndex(%v, %v) = (%d, %d), want (%d, %d)", tc.lat, tc.lon, row, col, tc.row, tc.col)
			}
		})
	}
	// Two positions a swing apart share a cell; that is what makes the cache key stable at anchor.
	r1, c1 := lightningCellIndex(-27.4001, 153.0001)
	r2, c2 := lightningCellIndex(-27.4004, 153.0006)
	if r1 != r2 || c1 != c2 {
		t.Fatalf("a few metres apart gave different cells: (%d,%d) and (%d,%d)", r1, c1, r2, c2)
	}
}

func TestLightningProvidersHandler(t *testing.T) {
	withCleanLightningProviderRegistry(t)
	registerLightningProvider(&stubLightningProvider{id: "stub"})

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := lightningProvidersHandler(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/lightning-providers", nil), rec)); err != nil {
		t.Fatal(err)
	}
	var got []lightningProviderInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "stub" || got[0].Name != "Stub" {
		t.Fatalf("providers = %+v", got)
	}

	withCleanLightningProviderRegistry(t)
	rec = httptest.NewRecorder()
	if err := lightningProvidersHandler(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/lightning-providers", nil), rec)); err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != "[]\n" {
		t.Fatalf("with nothing installed the list should be [], got %q", rec.Body.String())
	}
}

func TestProviderByTypeAndID_Lightning(t *testing.T) {
	withCleanLightningProviderRegistry(t)
	registerLightningProvider(&stubLightningProvider{id: "stub"})

	if p, valid, found := providerByTypeAndID("lightning", "stub"); !valid || !found || p.ID() != "stub" {
		t.Fatalf("lightning/stub: valid=%v found=%v", valid, found)
	}
	if _, valid, found := providerByTypeAndID("lightning", "missing"); !valid || found {
		t.Fatalf("lightning/missing: valid=%v found=%v", valid, found)
	}
}

// No lightning plugin installed, with the setting left alone, is a normal
// configuration: no lightning, not an error page.
func TestLightningForecast_NoProviderInstalledIsNotAnError(t *testing.T) {
	withCleanLightningProviderRegistry(t)
	t.Setenv("SETTINGS_FILE", writeLightningSettings(t, "", "127.0.0.1"))

	rec, payload := getLightning(t)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with no provider installed, got %d (%s)", rec.Code, rec.Body.String())
	}
	if payload.Windows == nil || len(payload.Windows) != 0 || payload.Provider != "" {
		t.Fatalf("expected an empty answer, got %+v", payload)
	}
	if rec.Body.String() == "" || !json.Valid(rec.Body.Bytes()) {
		t.Fatal("body is not JSON")
	}
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	if _, ok := raw["windows"].([]any); !ok {
		t.Fatalf("windows must be [] not null: %s", rec.Body.String())
	}
}

// Naming a provider that is not installed is a real mistake and says so.
func TestLightningForecast_ConfiguredButMissingProviderIs502(t *testing.T) {
	withCleanLightningProviderRegistry(t)
	t.Setenv("SETTINGS_FILE", writeLightningSettings(t, "not-installed", "127.0.0.1"))

	rec, _ := getLightning(t)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for a configured-but-absent provider, got %d", rec.Code)
	}
}

// -1,-1 is SignalK's placeholder, not a position in the Gulf of Guinea.
func TestLightningForecast_PlaceholderPositionIs502AndFetchesNothing(t *testing.T) {
	withCleanLightningProviderRegistry(t)
	stub := &stubLightningProvider{id: "stub"}
	registerLightningProvider(stub)
	withLightningScheduler(t, "stub")

	server := trustedSignalKPayloadServer(t, -1, -1)
	defer server.Close()
	t.Setenv("SETTINGS_FILE", writeLightningSettings(t, "stub", server.URL))

	rec, _ := getLightning(t)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for the -1,-1 placeholder, got %d (%s)", rec.Code, rec.Body.String())
	}
	if len(stub.calls) != 0 {
		t.Fatalf("the plugin was called %d times for a position that is not one", len(stub.calls))
	}
}

func TestLightningForecast_ServesWindowsFromTheBoatsCell(t *testing.T) {
	withCleanLightningProviderRegistry(t)
	now := time.Date(2026, 11, 12, 11, 44, 0, 0, time.UTC)
	withLightningClock(t, now)
	run := time.Date(2026, 11, 12, 6, 0, 0, 0, time.UTC)
	stub := &stubLightningProvider{id: "stub", run: run, probability: 0.25}
	registerLightningProvider(stub)
	withLightningScheduler(t, "stub")

	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()
	t.Setenv("SETTINGS_FILE", writeLightningSettings(t, "stub", server.URL))

	rec, payload := getLightning(t)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if payload.Provider != "stub" || payload.TTLSeconds != 21600 {
		t.Errorf("provider/ttl = %q/%d", payload.Provider, payload.TTLSeconds)
	}
	if len(payload.Windows) == 0 {
		t.Fatal("no windows served")
	}
	first := payload.Windows[0]
	// The window in progress at 11:44 ends at 12:00.
	if first.End != "2026-11-12T12:00:00Z" || first.Start != "2026-11-12T09:00:00Z" || first.Probability != 0.25 || first.Run != "2026-11-12T06:00:00Z" {
		t.Errorf("first window = %+v", first)
	}
	if payload.Run != "2026-11-12T06:00:00Z" {
		t.Errorf("run = %q", payload.Run)
	}
	if payload.UpdatedAt != now.Format(time.RFC3339) {
		t.Errorf("updated_at = %q", payload.UpdatedAt)
	}
	if payload.Refreshing {
		t.Error("refreshing should be over once the inline refresh finished")
	}
	// The plugin is asked about the centre of the cell holding the vessel.
	wantLat, wantLon := lightningCellCentre(lightningCellIndex(-27.4, 153.0))
	for _, c := range stub.calls {
		if c.lat != wantLat || c.lon != wantLon {
			t.Errorf("plugin asked about %v, %v, want the cell centre %v, %v", c.lat, c.lon, wantLat, wantLon)
		}
	}
}

// With nothing held but a refresh running, the page is told so rather than
// handed a 502 that hides the refresh.
func TestLightningForecast_RefreshingWithNothingHeldIs200WithTheError(t *testing.T) {
	withCleanLightningProviderRegistry(t)
	clock := time.Date(2026, 11, 12, 11, 44, 0, 0, time.UTC)
	orig := lightningNow
	lightningNow = func() time.Time { return clock }
	t.Cleanup(func() { lightningNow = orig })
	registerLightningProvider(&stubLightningProvider{id: "stub", err: fmt.Errorf("bucket unreachable")})
	s := withLightningScheduler(t, "stub")

	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()
	t.Setenv("SETTINGS_FILE", writeLightningSettings(t, "stub", server.URL))

	if rec, _ := getLightning(t); rec.Code != http.StatusBadGateway {
		t.Fatalf("first request: expected 502, got %d", rec.Code)
	}

	clock = clock.Add(lightningRetryAfterFailure + time.Minute)
	s.spawn = func(func()) {} // the retry starts and is still running
	rec, payload := getLightning(t)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 while a refresh runs, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !payload.Refreshing || payload.RefreshError == "" || payload.Windows == nil || len(payload.Windows) != 0 {
		t.Fatalf("payload = %+v, want refreshing, an error and windows []", payload)
	}
}

// A provider that is broken with nothing held fails loudly.
func TestLightningForecast_BrokenProviderWithNothingHeldIs502(t *testing.T) {
	withCleanLightningProviderRegistry(t)
	withLightningClock(t, time.Date(2026, 11, 12, 11, 44, 0, 0, time.UTC))
	registerLightningProvider(&stubLightningProvider{id: "stub", err: fmt.Errorf("bucket unreachable")})
	withLightningScheduler(t, "stub")

	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()
	t.Setenv("SETTINGS_FILE", writeLightningSettings(t, "stub", server.URL))

	rec, _ := getLightning(t)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// Moving to a new cell inside the retry wait must not turn the failure into an
// empty 200: nothing held, an error and nothing running is still a 502.
func TestLightningForecast_NewCellInsideRetryWaitIsStill502(t *testing.T) {
	withCleanLightningProviderRegistry(t)
	clock := time.Date(2026, 11, 12, 11, 44, 0, 0, time.UTC)
	orig := lightningNow
	lightningNow = func() time.Time { return clock }
	t.Cleanup(func() { lightningNow = orig })
	registerLightningProvider(&stubLightningProvider{id: "stub", err: fmt.Errorf("bucket unreachable")})
	withLightningScheduler(t, "stub")

	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()
	t.Setenv("SETTINGS_FILE", writeLightningSettings(t, "stub", server.URL))
	if rec, _ := getLightning(t); rec.Code != http.StatusBadGateway {
		t.Fatalf("first request: expected 502, got %d", rec.Code)
	}

	clock = clock.Add(time.Minute)
	moved := trustedSignalKPayloadServer(t, -27.4, 153.6)
	defer moved.Close()
	t.Setenv("SETTINGS_FILE", writeLightningSettings(t, "stub", moved.URL))
	if rec, _ := getLightning(t); rec.Code != http.StatusBadGateway {
		t.Fatalf("new cell inside the retry wait: expected 502, got %d (%s)", rec.Code, rec.Body.String())
	}
}
