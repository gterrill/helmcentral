package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const warningsValidFixtureWasm = "testdata/wasm_plugins/warningsvalid.wasm"

// mustNewWasmForecastWarningsProvider constructs a provider against
// warningsValidFixtureWasm with its disk-backed cache file redirected into a
// fresh t.TempDir() (via the FORECAST_WARNINGS_WASM_CACHE_FILE_<ID> env
// override newWasmForecastWarningsProviderFromBase honors) - mirrors
// mustNewWasmWaveProvider's reasoning: without this, every test would share
// and persist to the repo's real
// cache/forecast_warnings_wasm_warnings-valid-fixture_cache.json.
func mustNewWasmForecastWarningsProvider(t *testing.T, path string) *wasmForecastWarningsProvider {
	t.Helper()
	cacheFile := filepath.Join(t.TempDir(), "forecast-warnings-wasm-cache.json")
	t.Setenv("FORECAST_WARNINGS_WASM_CACHE_FILE_WARNINGS_VALID_FIXTURE", cacheFile)

	p, err := newWasmForecastWarningsProvider(path)
	if err != nil {
		t.Fatalf("newWasmForecastWarningsProvider(%q) failed: %v", path, err)
	}
	return p
}

func TestWasmForecastWarningsProvider_FetchWarnings_MapsFixtureFieldsCorrectly(t *testing.T) {
	provider := mustNewWasmForecastWarningsProvider(t, warningsValidFixtureWasm)

	bundle, err := provider.FetchWarnings(-27.4, 153.0)
	if err != nil {
		t.Fatalf("FetchWarnings returned error: %v", err)
	}

	if bundle.Region != "QLD — Capricornia Coast" {
		t.Fatalf("unexpected region: %q", bundle.Region)
	}
	if len(bundle.Bulletins) != 2 {
		t.Fatalf("expected 2 bulletins, got %d", len(bundle.Bulletins))
	}

	first := bundle.Bulletins[0]
	if first.ID != "IDQ20085" || first.Title != "Strong Wind Warning for Small Craft Operators" || first.Category != "wind" {
		t.Fatalf("unexpected first bulletin: %+v", first)
	}
	if first.IssuedAt.IsZero() {
		t.Fatalf("expected a non-zero issued_at on the first bulletin")
	}
	wantIssuedAt := time.Date(2026, 7, 19, 11, 51, 0, 0, time.UTC)
	if !first.IssuedAt.Equal(wantIssuedAt) {
		t.Fatalf("expected issued_at %v, got %v", wantIssuedAt, first.IssuedAt)
	}
	if len(first.Sections) != 2 {
		t.Fatalf("expected 2 sections on the first bulletin, got %d", len(first.Sections))
	}
	if first.Sections[0].Day != "Sunday" || first.Sections[0].WarningType != "Strong Wind Warning" {
		t.Fatalf("unexpected first section: %+v", first.Sections[0])
	}

	second := bundle.Bulletins[1]
	if second.ID != "IDQ20083" {
		t.Fatalf("unexpected second bulletin id: %q", second.ID)
	}
	if !second.IssuedAt.IsZero() {
		t.Fatalf("expected the second bulletin's issued_at to map to the zero time when empty, got %v", second.IssuedAt)
	}
	if len(second.Sections) != 1 {
		t.Fatalf("expected 1 section on the second bulletin, got %d", len(second.Sections))
	}

	if bundle.Cached {
		t.Errorf("expected a fresh (non-cached) fetch on first call")
	}
	if bundle.CachedAt.IsZero() {
		t.Errorf("expected CachedAt to be set")
	}
}

func TestWasmForecastWarningsProvider_FetchWarnings_CachesWithinTTL(t *testing.T) {
	provider := mustNewWasmForecastWarningsProvider(t, warningsValidFixtureWasm)

	first, err := provider.FetchWarnings(10.0, 20.0)
	if err != nil {
		t.Fatalf("first FetchWarnings returned error: %v", err)
	}
	if first.Cached {
		t.Fatalf("expected the first fetch to be a live (non-cached) fetch")
	}

	second, err := provider.FetchWarnings(10.0, 20.0)
	if err != nil {
		t.Fatalf("second FetchWarnings returned error: %v", err)
	}
	if !second.Cached {
		t.Fatalf("expected the second fetch within TTL to be served from cache")
	}
	if len(second.Bulletins) != len(first.Bulletins) {
		t.Fatalf("expected the cached bundle's data to match the original fetch")
	}
}

func TestWasmForecastWarningsProvider_FetchWarnings_StaleOnErrorFallback(t *testing.T) {
	provider := mustNewWasmForecastWarningsProvider(t, warningsValidFixtureWasm)

	fresh, err := provider.FetchWarnings(30.0, 40.0)
	if err != nil {
		t.Fatalf("initial FetchWarnings returned error: %v", err)
	}

	// Backdate the just-populated cache entry past its TTL directly (same
	// package, unexported field access - mirrors
	// TestWasmWaveProvider_FetchWaves_StaleOnErrorFallback) so the next
	// FetchWarnings call is forced to attempt a live call rather than serving
	// a within-TTL hit.
	cacheKey := "30.0,40.0"
	provider.cache.mu.Lock()
	entry, ok := provider.cache.data[cacheKey]
	if !ok {
		provider.cache.mu.Unlock()
		t.Fatalf("expected a cache entry for key %q after the initial fetch", cacheKey)
	}
	entry.CachedAt = time.Now().Add(-2 * provider.ttlDuration())
	provider.cache.data[cacheKey] = entry
	provider.cache.mu.Unlock()

	// Close the underlying compiled plugin so the next live call fails
	// deterministically (instance creation errors out), simulating an
	// upstream outage without needing a second, failure-simulating fixture.
	provider.compiled.Close(context.Background())

	stale, err := provider.FetchWarnings(30.0, 40.0)
	if err != nil {
		t.Fatalf("expected a stale-cache fallback instead of an error, got: %v", err)
	}
	if !stale.Cached {
		t.Fatalf("expected the stale fallback bundle to be marked Cached=true")
	}
	if len(stale.Bulletins) != len(fresh.Bulletins) {
		t.Fatalf("expected the stale fallback to carry the original fetched data")
	}
}

func TestLoadWasmForecastWarningsProviders_RegistersValidPlugin(t *testing.T) {
	withCleanForecastWarningsProviderRegistry(t)

	dir := t.TempDir()
	goodBytes, err := os.ReadFile(warningsValidFixtureWasm)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "warningsvalid.wasm"), goodBytes, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.wasm"), []byte("not a real wasm module"), 0o644); err != nil {
		t.Fatalf("write corrupt fixture: %v", err)
	}

	loadWasmForecastWarningsProviders(dir)

	provider, ok := getForecastWarningsProvider("warnings-valid-fixture")
	if !ok {
		t.Fatalf("expected the warningsvalid plugin to be registered")
	}
	if provider.Name() != "Forecast Warnings Valid Fixture Provider" {
		t.Errorf("unexpected provider name: %q", provider.Name())
	}
	if len(forecastWarningsProviderOrder) != 1 {
		t.Errorf("expected exactly 1 registered provider, got %d: %v", len(forecastWarningsProviderOrder), forecastWarningsProviderOrder)
	}
}

func TestPluginsForecastWarningsDir_DefaultsAndEnvOverride(t *testing.T) {
	if got := pluginsForecastWarningsDir(); got != "plugins/forecast-warnings" {
		t.Fatalf("expected default 'plugins/forecast-warnings', got %q", got)
	}

	t.Setenv("PLUGINS_FORECAST_WARNINGS_DIR", "/tmp/custom-forecast-warnings-plugins")
	if got := pluginsForecastWarningsDir(); got != "/tmp/custom-forecast-warnings-plugins" {
		t.Fatalf("expected env override to take effect, got %q", got)
	}
}

// TestMapWasmFetchWarningsOutput_IssuedAtEmptyMapsToZeroTime directly unit
// tests the "issued_at genuinely unknown" JSON-unmarshal-to-zero-time
// behavior described in the forecast-warnings plugin contract.
func TestMapWasmFetchWarningsOutput_IssuedAtEmptyMapsToZeroTime(t *testing.T) {
	raw := `{"region":"QLD","bulletins":[{"id":"IDQ1","title":"t","category":"wind","issued_at":"","details_url":"https://example.com","sections":[{"day":"Sunday","warning_type":"Gale Warning"}]}]}`

	var parsed wasmFetchWarningsOutput
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("failed to unmarshal fixture JSON: %v", err)
	}

	bundle, err := mapWasmFetchWarningsOutput(parsed)
	if err != nil {
		t.Fatalf("mapWasmFetchWarningsOutput returned error: %v", err)
	}
	if len(bundle.Bulletins) != 1 {
		t.Fatalf("expected 1 bulletin, got %d", len(bundle.Bulletins))
	}
	if !bundle.Bulletins[0].IssuedAt.IsZero() {
		t.Fatalf("expected IssuedAt to be the zero time when issued_at is empty, got %v", bundle.Bulletins[0].IssuedAt)
	}
}

// TestMapWasmFetchWarningsOutput_IssuedAtUnparseableIsError proves an
// unparseable (but non-empty) issued_at is a hard error - fail-fast, never
// silently zeroed - mirroring parseOptionalTime's documented semantics.
func TestMapWasmFetchWarningsOutput_IssuedAtUnparseableIsError(t *testing.T) {
	raw := `{"region":"QLD","bulletins":[{"id":"IDQ1","title":"t","category":"wind","issued_at":"not-a-timestamp","details_url":"https://example.com","sections":[]}]}`

	var parsed wasmFetchWarningsOutput
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("failed to unmarshal fixture JSON: %v", err)
	}

	if _, err := mapWasmFetchWarningsOutput(parsed); err == nil {
		t.Fatalf("expected an error for an unparseable issued_at")
	}
}

func TestForecastWarningsWasmCacheKey_RoundsToOneDecimal(t *testing.T) {
	if got := forecastWarningsWasmCacheKey(-27.44, 153.049); got != "-27.4,153.0" {
		t.Fatalf("expected rounded cache key '-27.4,153.0', got %q", got)
	}
}
