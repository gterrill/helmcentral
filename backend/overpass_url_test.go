package main

import (
	"bytes"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ── resolveOverpassPluginURL ─────────────────────────────────────────────
//
// Mirrors the POI plugin's resolveOverpassURL (osm-overpass.go) validation
// exactly: a blank value is the normal case and yields the default, while a
// present-but-malformed value is a mistake that must surface, never
// silently keep querying the default host (AGENTS.md's fail-fast /
// no-masking-fallback policy).

func TestResolveOverpassPluginURL_BlankUsesDefault(t *testing.T) {
	got, err := resolveOverpassPluginURL("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != defaultOverpassAPIURL {
		t.Fatalf("got %q, want default %q", got, defaultOverpassAPIURL)
	}
}

func TestResolveOverpassPluginURL_WhitespaceUsesDefault(t *testing.T) {
	got, err := resolveOverpassPluginURL("   \t\n  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != defaultOverpassAPIURL {
		t.Fatalf("got %q, want default %q", got, defaultOverpassAPIURL)
	}
}

func TestResolveOverpassPluginURL_ValidHTTPSMirrorReturnedTrimmed(t *testing.T) {
	const mirror = "https://overpass.kumi.systems/api/interpreter"
	got, err := resolveOverpassPluginURL("  " + mirror + "  \n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != mirror {
		t.Fatalf("got %q, want trimmed mirror %q", got, mirror)
	}
}

func TestResolveOverpassPluginURL_HTTPSchemeRejected(t *testing.T) {
	_, err := resolveOverpassPluginURL("http://overpass.example.com/api/interpreter")
	if err == nil {
		t.Fatalf("expected an error for a non-https scheme")
	}
	if !strings.Contains(err.Error(), "overpass_url") {
		t.Fatalf("expected error to name the overpass_url setting, got: %v", err)
	}
}

func TestResolveOverpassPluginURL_GarbageRejected(t *testing.T) {
	const garbage = "://not-a-url"
	_, err := resolveOverpassPluginURL(garbage)
	if err == nil {
		t.Fatalf("expected an error for an unparseable value")
	}
	if !strings.Contains(err.Error(), "overpass_url") {
		t.Fatalf("expected error to name the overpass_url setting, got: %v", err)
	}
}

// ── currentOverpassAPIURL reads the osm-overpass plugin's own stored
// overpass_url config value, not a global setting ──────────────────────────
//
// withOsmOverpassPOIProvider registers a stub "osm-overpass" POI provider
// (mirroring the real plugin's registered id, poi_providers.go's
// defaultPOIProviderID) whose Path() points at a throwaway wasm path, and
// points globalPluginOverridesStore at a fresh temp store - the shape
// currentOverpassAPIURL now reads (ADR 0100 rewrite: the Overpass mirror is
// the osm-overpass plugin's own setting, not an app-level one). Returns the
// wasm path so the test can seed/rewrite its stored overpass_url value.
func withOsmOverpassPOIProvider(t *testing.T) (wasmPath string, store *pluginOverridesStore) {
	t.Helper()
	withCleanPOIProviderRegistry(t)

	wasmPath = filepath.Join(t.TempDir(), "osm-overpass.wasm")
	registerPOIProvider(&overpassStubPOIProvider{
		stubPOIProvider: stubPOIProvider{id: osmOverpassPOIProviderID, name: "OpenStreetMap via Overpass"},
		path:            wasmPath,
	})

	store = withTestPluginOverridesStore(t)
	return wasmPath, store
}

// overpassStubPOIProvider adds a Path() (pluginPathProvider) to
// stubPOIProvider (poi_providers_test.go) so it satisfies the same
// WASM-backed invariant every real registered provider does -
// currentOverpassAPIURL and place_name.go's other plugin-config lookups
// type-assert on it.
type overpassStubPOIProvider struct {
	stubPOIProvider
	path string
}

func (s *overpassStubPOIProvider) Path() string { return s.path }

func TestCurrentOverpassAPIURL_NoOsmOverpassProviderRegisteredUsesDefault(t *testing.T) {
	withCleanPOIProviderRegistry(t)
	withTestPluginOverridesStore(t)

	got, err := currentOverpassAPIURL()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != defaultOverpassAPIURL {
		t.Fatalf("got %q, want default %q", got, defaultOverpassAPIURL)
	}
}

func TestCurrentOverpassAPIURL_NoStoredValueUsesDefault(t *testing.T) {
	withOsmOverpassPOIProvider(t)

	got, err := currentOverpassAPIURL()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != defaultOverpassAPIURL {
		t.Fatalf("got %q, want default %q", got, defaultOverpassAPIURL)
	}
}

func TestCurrentOverpassAPIURL_StoredMirrorIsUsed(t *testing.T) {
	wasmPath, store := withOsmOverpassPOIProvider(t)
	const mirror = "https://overpass.openstreetmap.fr/api/interpreter"
	if err := store.SetConfigValues(wasmPath, map[string]string{"overpass_url": mirror}); err != nil {
		t.Fatalf("SetConfigValues: %v", err)
	}

	got, err := currentOverpassAPIURL()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != mirror {
		t.Fatalf("got %q, want %q", got, mirror)
	}
}

// A stored value that fails resolveOverpassPluginURL's validation (only
// reachable via a POST /api/plugins/poi/osm-overpass/config that bypassed
// the handler's own validation, or direct DB manipulation) must fail the
// lookup outright, naming the setting, rather than silently falling back to
// the default (AGENTS.md's fail-fast / no-masking-fallback policy).
func TestCurrentOverpassAPIURL_MalformedStoredValueErrorsNamingSetting(t *testing.T) {
	wasmPath, store := withOsmOverpassPOIProvider(t)
	if err := store.SetConfigValues(wasmPath, map[string]string{"overpass_url": "http://insecure.example.com"}); err != nil {
		t.Fatalf("SetConfigValues: %v", err)
	}

	_, err := currentOverpassAPIURL()
	if err == nil {
		t.Fatalf("expected an error for a malformed stored overpass_url")
	}
	if !strings.Contains(err.Error(), "overpass_url") {
		t.Fatalf("expected error to name overpass_url, got: %v", err)
	}
}

// ── postOverpassQuery uses the plugin-configured mirror, not a hardcoded
// constant ───────────────────────────────────────────────────────────────

// urlCapturingOverpassFetcher is a minimal injectable overpassFetcher,
// following the same idiom as fakeOverpassFetcher (place_name_test.go)
// but capturing the request's destination URL instead of the posted
// query's around: radius, since this test only cares where the request
// went.
type urlCapturingOverpassFetcher struct {
	mu  sync.Mutex
	url string
}

func (f *urlCapturingOverpassFetcher) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.url = req.URL.String()
	f.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(`{"elements":[]}`))),
	}, nil
}

func (f *urlCapturingOverpassFetcher) requestedURL() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.url
}

func TestPostOverpassQuery_PostsToConfiguredOverpassMirror(t *testing.T) {
	wasmPath, store := withOsmOverpassPOIProvider(t)
	const mirror = "https://overpass.kumi.systems/api/interpreter"
	if err := store.SetConfigValues(wasmPath, map[string]string{"overpass_url": mirror}); err != nil {
		t.Fatalf("SetConfigValues: %v", err)
	}

	fetcher := &urlCapturingOverpassFetcher{}
	if _, err := postOverpassQuery(fetcher, "irrelevant query"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := fetcher.requestedURL(); got != mirror {
		t.Fatalf("posted to %q, want configured mirror %q", got, mirror)
	}
}

// Changing the stored value between two calls changes the destination
// without any restart or cache-busting on the caller's part.
func TestPostOverpassQuery_StoredValueChangeBetweenCallsChangesDestination(t *testing.T) {
	wasmPath, store := withOsmOverpassPOIProvider(t)
	const first = "https://overpass.kumi.systems/api/interpreter"
	const second = "https://overpass.openstreetmap.fr/api/interpreter"
	if err := store.SetConfigValues(wasmPath, map[string]string{"overpass_url": first}); err != nil {
		t.Fatalf("SetConfigValues (first): %v", err)
	}

	fetcher := &urlCapturingOverpassFetcher{}
	if _, err := postOverpassQuery(fetcher, "irrelevant query"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := fetcher.requestedURL(); got != first {
		t.Fatalf("first call posted to %q, want %q", got, first)
	}

	if err := store.SetConfigValues(wasmPath, map[string]string{"overpass_url": second}); err != nil {
		t.Fatalf("SetConfigValues (second): %v", err)
	}

	if _, err := postOverpassQuery(fetcher, "irrelevant query"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := fetcher.requestedURL(); got != second {
		t.Fatalf("second call posted to %q, want %q", got, second)
	}
}
