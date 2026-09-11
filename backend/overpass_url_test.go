package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// ── resolveOverpassAPIURL ────────────────────────────────────────────────
//
// Mirrors the POI plugin's resolveOverpassURL (osm-overpass.go) validation
// exactly: an unset/blank OVERPASS_API_URL is the normal case and yields the
// default, while a present-but-malformed value is a mistake that must
// surface, never silently keep querying the default host (AGENTS.md's
// fail-fast / no-masking-fallback policy).

func TestResolveOverpassAPIURL_BlankUsesDefault(t *testing.T) {
	got, err := resolveOverpassAPIURL("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != defaultOverpassAPIURL {
		t.Fatalf("got %q, want default %q", got, defaultOverpassAPIURL)
	}
}

func TestResolveOverpassAPIURL_WhitespaceUsesDefault(t *testing.T) {
	got, err := resolveOverpassAPIURL("   \t\n  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != defaultOverpassAPIURL {
		t.Fatalf("got %q, want default %q", got, defaultOverpassAPIURL)
	}
}

func TestResolveOverpassAPIURL_ValidHTTPSMirrorReturnedTrimmed(t *testing.T) {
	const mirror = "https://overpass.kumi.systems/api/interpreter"
	got, err := resolveOverpassAPIURL("  " + mirror + "  \n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != mirror {
		t.Fatalf("got %q, want trimmed mirror %q", got, mirror)
	}
}

func TestResolveOverpassAPIURL_HTTPSchemeRejected(t *testing.T) {
	_, err := resolveOverpassAPIURL("http://overpass.example.com/api/interpreter")
	if err == nil {
		t.Fatalf("expected an error for a non-https scheme")
	}
	if !strings.Contains(err.Error(), "OVERPASS_API_URL") {
		t.Fatalf("expected error to name OVERPASS_API_URL, got: %v", err)
	}
}

func TestResolveOverpassAPIURL_GarbageRejected(t *testing.T) {
	const garbage = "://not-a-url"
	_, err := resolveOverpassAPIURL(garbage)
	if err == nil {
		t.Fatalf("expected an error for an unparseable value")
	}
	if !strings.Contains(err.Error(), "OVERPASS_API_URL") {
		t.Fatalf("expected error to name OVERPASS_API_URL, got: %v", err)
	}
}

// ── postOverpassQuery uses the configured var, not a hardcoded constant ────

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

func TestPostOverpassQuery_PostsToConfiguredOverpassAPIURL(t *testing.T) {
	const mirror = "https://overpass.kumi.systems/api/interpreter"
	orig := overpassAPIURL
	overpassAPIURL = mirror
	t.Cleanup(func() { overpassAPIURL = orig })

	fetcher := &urlCapturingOverpassFetcher{}
	if _, err := postOverpassQuery(fetcher, "irrelevant query"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := fetcher.requestedURL(); got != mirror {
		t.Fatalf("posted to %q, want configured overpassAPIURL %q", got, mirror)
	}
}
