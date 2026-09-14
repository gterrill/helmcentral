package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// ── fixtures & shared coordinates ───────────────────────────────────────────

// Coordinates from the plan's measured evidence: Goldsmith anchorage and
// Lindeman Island are ~28.9km apart, well inside a single 0.5-degree GeoNames
// cache cell (the original bug) but far enough apart that any reasonably
// fine grid must distinguish them.
const (
	goldsmithLat = -20.685
	goldsmithLon = 149.145
	lindemanLat  = -20.4467
	lindemanLon  = 149.0353
)

// lindeman5000NamedFeatures is every name actually present in
// overpass_lindeman_5000.json (12 named elements; a 13th, tags
// {"place":"islet"}, carries no name tag at all). Used to assert a resolved
// name is a real candidate from the fixture, never the unnamed islet (which
// by construction cannot appear here since it has no name to produce).
var lindeman5000NamedFeatures = map[string]bool{
	"Gaibirra Island":        true,
	"Kennedy Sound":          true,
	"Plantation Bay":         true,
	"Turtle Bay":             true,
	"Brush Island":           true,
	"Shaw Island":            true,
	"Pentecost Island":       true,
	"Little Lindeman Island": true,
	"Lindeman Island":        true,
	"Cole Island":            true,
	"Ann Island":             true,
	"Sidney Island":          true,
}

func loadOverpassFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	return data
}

// resetPlaceNameCache swaps in a fresh, empty placeNameCache for the
// duration of the test so cache state never leaks between tests, and
// restores the original afterward.
func resetPlaceNameCache(t *testing.T) {
	t.Helper()
	orig := placeNameCache
	placeNameCache = &placeNameCacheStore{data: make(map[string]placeNameCacheEntry)}
	t.Cleanup(func() { placeNameCache = orig })
}

// resetPlaceNameBackoff clears the package-level Overpass backoff window for
// the duration of the test, restoring the original afterward - mirrors
// resetPlaceNameCache so a failure induced by one test's fake fetcher can
// never leak a backoff window into another test (or into a later call
// within the same test that expects a real retry).
func resetPlaceNameBackoff(t *testing.T) {
	t.Helper()
	placeNameBackoff.mu.Lock()
	origUntil, origDelay := placeNameBackoff.until, placeNameBackoff.nextDelay
	placeNameBackoff.until = time.Time{}
	placeNameBackoff.nextDelay = 0
	placeNameBackoff.mu.Unlock()
	t.Cleanup(func() {
		placeNameBackoff.mu.Lock()
		placeNameBackoff.until, placeNameBackoff.nextDelay = origUntil, origDelay
		placeNameBackoff.mu.Unlock()
	})
}

// withFakeOverpassFetcher swaps the package-level overpassHTTPClient (used
// by both the tick and setAnchorWatch's async resolve) for fetcher, and
// restores the original afterward. It also resets the Overpass backoff
// window (resetPlaceNameBackoff): that state is process-wide and keyed on
// real wall-clock time (up to placeNameBackoffMax, 15 minutes), so a
// failure induced by one test's fake fetcher would otherwise silently
// suppress every other test's resolution attempts - including successful
// ones - for however much of that window the rest of the run takes.
func withFakeOverpassFetcher(t *testing.T, fetcher overpassFetcher) {
	t.Helper()
	orig := overpassHTTPClient
	overpassHTTPClient = fetcher
	t.Cleanup(func() { overpassHTTPClient = orig })
	resetPlaceNameBackoff(t)
}

// waitForCondition polls cond until it returns true or timeout elapses,
// failing the test on timeout. Used for the goroutine-based anchor-watch
// resolve path (setAnchorWatch deliberately doesn't block its HTTP response
// on the Overpass round trip).
func waitForCondition(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for condition", timeout)
}

// ── fake overpassFetcher ─────────────────────────────────────────────────────

// fakeOverpassFetcher is an injectable overpassFetcher for tests, mirroring
// fakeTileFetcher (tile_cache_test.go). Responses are keyed by the widening
// ring the request actually asked for, extracted from the posted Overpass
// QL "around:R,lat,lon" clause — so a table test can assert both *which*
// fixture wins and *how many* (and which) rings were actually queried.
type fakeOverpassFetcher struct {
	mu       sync.Mutex
	calls    []int
	fixtures map[int][]byte // radius (metres) -> canned response body
	statuses map[int]int    // radius -> HTTP status (default 200)
	types    map[int]string // radius -> Content-Type (default application/json)
	errs     map[int]error  // radius -> transport error instead of a response

	// gate, when non-nil, blocks every Do until the channel is closed, so a
	// test can hold a resolution "in flight" and assert what the caller does
	// while it is outstanding. A channel rather than a sleep: nothing races a
	// wall clock.
	gate chan struct{}
}

var overpassAroundRadiusPattern = regexp.MustCompile(`around:(\d+),`)

func (f *fakeOverpassFetcher) Do(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("fakeOverpassFetcher: read request body: %w", err)
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, fmt.Errorf("fakeOverpassFetcher: parse form body: %w", err)
	}
	query := values.Get("data")
	m := overpassAroundRadiusPattern.FindStringSubmatch(query)
	if m == nil {
		return nil, fmt.Errorf("fakeOverpassFetcher: no around: radius found in query: %s", query)
	}
	radius, err := strconv.Atoi(m[1])
	if err != nil {
		return nil, fmt.Errorf("fakeOverpassFetcher: parse radius: %w", err)
	}

	f.mu.Lock()
	f.calls = append(f.calls, radius)
	gate := f.gate
	f.mu.Unlock()

	if gate != nil {
		<-gate
	}

	if simulated, ok := f.errs[radius]; ok {
		return nil, simulated
	}

	status := http.StatusOK
	if s, ok := f.statuses[radius]; ok {
		status = s
	}
	contentType := "application/json"
	if ct, ok := f.types[radius]; ok {
		contentType = ct
	}
	respBody, ok := f.fixtures[radius]
	if !ok {
		respBody = []byte(`{"elements":[]}`)
	}

	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}, nil
}

func (f *fakeOverpassFetcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// ── Test 1: the direct regression test for the reported bug ────────────────

func TestPlaceNameCacheKeysDistinguishAnchorages(t *testing.T) {
	goldsmithKey := placeNameCacheKey(goldsmithLat, goldsmithLon)
	lindemanKey := placeNameCacheKey(lindemanLat, lindemanLon)

	if goldsmithKey == lindemanKey {
		t.Fatalf(
			"expected distinct cache keys for anchorages 28.9km apart, both got %q (this is the reported bug: a coarse cache cell serving one anchorage's name at the other)",
			goldsmithKey,
		)
	}
}

// ── Test 2: the ladder stops at the tightest non-empty ring ────────────────

func TestResolvePlaceNameStopsAtTightestRing(t *testing.T) {
	tests := []struct {
		name      string
		lat, lon  float64
		fixtures  map[int][]byte
		wantName  string
		wantCalls []int
	}{
		{
			name: "goldsmith resolves on the first 400m ring, never widens",
			lat:  goldsmithLat, lon: goldsmithLon,
			fixtures: map[int][]byte{
				400: loadOverpassFixture(t, "overpass_goldsmith_400.json"),
			},
			wantName:  "Goldsmith Island",
			wantCalls: []int{400},
		},
		{
			name: "lindeman's empty 400m ring forces widening to 1500m, and stops there",
			lat:  lindemanLat, lon: lindemanLon,
			fixtures: map[int][]byte{
				400:  loadOverpassFixture(t, "overpass_lindeman_400.json"),
				1500: loadOverpassFixture(t, "overpass_lindeman_1500.json"),
			},
			wantName:  "Lindeman Island",
			wantCalls: []int{400, 1500},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetcher := &fakeOverpassFetcher{fixtures: tt.fixtures}

			got, err := resolvePlaceName(fetcher, tt.lat, tt.lon)
			if err != nil {
				t.Fatalf("resolvePlaceName: %v", err)
			}
			if got != tt.wantName {
				t.Fatalf("expected name %q, got %q", tt.wantName, got)
			}
			if !slices.Equal(fetcher.calls, tt.wantCalls) {
				t.Fatalf(
					"expected rings queried %v, got %v (the 5000m fixture must never be requested once a tighter ring hits)",
					tt.wantCalls, fetcher.calls,
				)
			}
		})
	}
}

// ── Test 3: an unnamed feature must never win, even after widening ─────────

func TestResolvePlaceNameSkipsUnnamedFeatures(t *testing.T) {
	fetcher := &fakeOverpassFetcher{fixtures: map[int][]byte{
		400:  []byte(`{"elements":[]}`),
		1500: []byte(`{"elements":[]}`),
		5000: loadOverpassFixture(t, "overpass_lindeman_5000.json"),
	}}

	got, err := resolvePlaceName(fetcher, lindemanLat, lindemanLon)
	if err != nil {
		t.Fatalf("resolvePlaceName: %v", err)
	}
	if !slices.Equal(fetcher.calls, []int{400, 1500, 5000}) {
		t.Fatalf("expected all three rings queried in order, got %v", fetcher.calls)
	}
	if got == "" {
		t.Fatalf("expected a resolved name from the 5000m ring (12 of its 13 elements are named), got empty")
	}
	if !lindeman5000NamedFeatures[got] {
		t.Fatalf("winner %q is not one of the fixture's named features — the unnamed islet (tags: {\"place\":\"islet\"}, no name) must never win", got)
	}
}

// TestBestNamedFeatureSkipsTheUnnamedIslet is a tighter, ranking-only unit
// test for the same fixture: it fails fast (with a clear message) if the
// fixture itself ever drifts from the captured shape this feature's tests
// depend on (exactly 13 elements, exactly 1 unnamed).
func TestBestNamedFeatureSkipsTheUnnamedIslet(t *testing.T) {
	raw := loadOverpassFixture(t, "overpass_lindeman_5000.json")
	var parsed overpassResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	if len(parsed.Elements) != 13 {
		t.Fatalf("fixture drift: expected 13 elements in overpass_lindeman_5000.json, got %d", len(parsed.Elements))
	}
	unnamed := 0
	for _, el := range parsed.Elements {
		if strings.TrimSpace(el.Tags["name"]) == "" {
			unnamed++
		}
	}
	if unnamed != 1 {
		t.Fatalf("fixture drift: expected exactly 1 unnamed element, got %d", unnamed)
	}

	name, ok := bestNamedFeature(parsed.Elements, lindemanLat, lindemanLon)
	if !ok {
		t.Fatalf("expected a named winner among the 12 named candidates")
	}
	if !lindeman5000NamedFeatures[name] {
		t.Fatalf("winner %q must be one of the fixture's actual named features", name)
	}
}

// ── Test 4: a failed lookup is never cached, and retries ───────────────────

func TestPlaceNameFailureIsNotCached(t *testing.T) {
	resetPlaceNameCache(t)
	resetPlaceNameBackoff(t)

	// Overpass's real rate-limit signature: HTTP 200, text/html body
	// containing this exact marker string (captured while fetching this
	// feature's fixtures). Must be treated as a failure, not zero results.
	rateLimitedBody := []byte(`<html><body>Dispatcher_Client::request_read_and_idx::rate_limited</body></html>`)
	fetcher := &fakeOverpassFetcher{
		fixtures: map[int][]byte{400: rateLimitedBody},
		types:    map[int]string{400: "text/html"},
	}

	key := placeNameCacheKey(goldsmithLat, goldsmithLon)

	got := resolveAndCachePlaceName(fetcher, goldsmithLat, goldsmithLon)
	if got != "" {
		t.Fatalf("expected empty name on a rate-limited response, got %q", got)
	}
	if fetcher.callCount() != 1 {
		t.Fatalf("expected exactly 1 upstream call, got %d", fetcher.callCount())
	}
	if _, ok := placeNameCache.get(key); ok {
		t.Fatalf("a failed lookup must not be cached")
	}

	// The failure above just opened a backoff window; clear it to simulate
	// "enough time has passed" without an actual sleep, isolating this
	// test's own retry-after-failure assertion from the separate backoff
	// behavior covered by the Test*Backoff* tests below.
	resetPlaceNameBackoff(t)

	// A second call must retry the upstream rather than serving a cached
	// blank, and once Overpass succeeds, the resolved name is what's cached.
	fetcher2 := &fakeOverpassFetcher{fixtures: map[int][]byte{
		400: loadOverpassFixture(t, "overpass_goldsmith_400.json"),
	}}
	got2 := resolveAndCachePlaceName(fetcher2, goldsmithLat, goldsmithLon)
	if got2 != "Goldsmith Island" {
		t.Fatalf("expected the retry to resolve Goldsmith Island, got %q", got2)
	}
	if cached, ok := placeNameCache.get(key); !ok || cached != "Goldsmith Island" {
		t.Fatalf("expected the successful resolution to be cached, got %q ok=%v", cached, ok)
	}
}

// ── Empty results: cached with a shorter TTL, refetched after it ──────────

// allRingsEmptyFixtures makes every one of placeNameRadiiMeters's rings
// return zero elements, so resolvePlaceName genuinely exhausts the ladder
// with no error - the real "no named feature here" case, not a failure.
func allRingsEmptyFixtures() map[int][]byte {
	fixtures := map[int][]byte{}
	for _, radius := range placeNameRadiiMeters {
		fixtures[radius] = []byte(`{"elements":[]}`)
	}
	return fixtures
}

func TestResolveAndCachePlaceName_EmptyResultIsCachedAndNotRefetchedWithinTTL(t *testing.T) {
	resetPlaceNameCache(t)
	resetPlaceNameBackoff(t)

	fetcher := &fakeOverpassFetcher{fixtures: allRingsEmptyFixtures()}

	got := resolveAndCachePlaceName(fetcher, goldsmithLat, goldsmithLon)
	if got != "" {
		t.Fatalf("expected an empty name when every ring is genuinely empty, got %q", got)
	}
	if want := len(placeNameRadiiMeters); fetcher.callCount() != want {
		t.Fatalf("expected the ladder to walk all %d rings, got %d calls", want, fetcher.callCount())
	}

	key := placeNameCacheKey(goldsmithLat, goldsmithLon)
	if _, ok := placeNameCache.get(key); !ok {
		t.Fatalf("expected the genuine empty result to be cached (a real answer, not a masking fallback)")
	}

	// A second call within the TTL must be served from the cache: no
	// further upstream calls at all.
	got2 := resolveAndCachePlaceName(fetcher, goldsmithLat, goldsmithLon)
	if got2 != "" {
		t.Fatalf("expected the cached empty result, got %q", got2)
	}
	if fetcher.callCount() != len(placeNameRadiiMeters) {
		t.Fatalf("expected no additional upstream calls while the empty result is within its TTL, got %d total calls", fetcher.callCount())
	}
}

func TestResolveAndCachePlaceName_EmptyResultRefetchedAfterItsTTL(t *testing.T) {
	resetPlaceNameCache(t)
	resetPlaceNameBackoff(t)

	fetcher := &fakeOverpassFetcher{fixtures: allRingsEmptyFixtures()}
	if got := resolveAndCachePlaceName(fetcher, goldsmithLat, goldsmithLon); got != "" {
		t.Fatalf("expected an empty result, got %q", got)
	}

	// Backdate the cached empty entry past placeNameEmptyCacheTTL (same
	// package, unexported field access - mirrors the TTL-expiry tests
	// elsewhere in this package).
	key := placeNameCacheKey(goldsmithLat, goldsmithLon)
	placeNameCache.mu.Lock()
	entry := placeNameCache.data[key]
	entry.cachedAt = time.Now().Add(-placeNameEmptyCacheTTL - time.Minute)
	placeNameCache.data[key] = entry
	placeNameCache.mu.Unlock()

	if _, ok := placeNameCache.get(key); ok {
		t.Fatalf("expected the backdated empty entry to have expired")
	}

	// A resolve past the empty-result TTL must walk Overpass again.
	if got := resolveAndCachePlaceName(fetcher, goldsmithLat, goldsmithLon); got != "" {
		t.Fatalf("expected an empty result again, got %q", got)
	}
	if want := 2 * len(placeNameRadiiMeters); fetcher.callCount() != want {
		t.Fatalf("expected a full second ladder walk after TTL expiry, got %d total calls (want %d)", fetcher.callCount(), want)
	}
}

// TestPlaceNameCacheGet_NamedEntryUsesTheLongerTTL is the direct unit test
// that get() applies placeNameCacheTTL (not the shorter empty TTL) to a
// named entry - the regression guard for the two TTLs actually being
// selected on the right branch.
func TestPlaceNameCacheGet_NamedEntryUsesTheLongerTTL(t *testing.T) {
	store := &placeNameCacheStore{data: make(map[string]placeNameCacheEntry)}
	store.put("k", "Goldsmith Island")

	store.mu.Lock()
	entry := store.data["k"]
	entry.cachedAt = time.Now().Add(-placeNameEmptyCacheTTL - time.Minute)
	store.data["k"] = entry
	store.mu.Unlock()

	// Older than the empty-result TTL but still within the named-result
	// TTL - must still be a hit.
	if name, ok := store.get("k"); !ok || name != "Goldsmith Island" {
		t.Fatalf("expected a named entry to survive past placeNameEmptyCacheTTL (it must use the longer placeNameCacheTTL), got name=%q ok=%v", name, ok)
	}
}

// ── Backoff after error, recovery on success ───────────────────────────────

func TestResolveAndCachePlaceName_BackoffSkipsUpstreamAfterAFailure(t *testing.T) {
	resetPlaceNameCache(t)
	resetPlaceNameBackoff(t)

	rateLimitedBody := []byte(`<html><body>Dispatcher_Client::request_read_and_idx::rate_limited</body></html>`)
	fetcher := &fakeOverpassFetcher{
		fixtures: map[int][]byte{400: rateLimitedBody},
		types:    map[int]string{400: "text/html"},
	}

	if got := resolveAndCachePlaceName(fetcher, goldsmithLat, goldsmithLon); got != "" {
		t.Fatalf("expected an empty result on the failing first call, got %q", got)
	}
	if fetcher.callCount() != 1 {
		t.Fatalf("expected exactly 1 upstream call for the first (failing) attempt, got %d", fetcher.callCount())
	}

	// A different position (so the cache can't be the reason for the
	// no-op) must still be skipped entirely while backoff is active - the
	// whole point being that Overpass itself, not just this one cell, is
	// assumed unreachable for the window.
	if got := resolveAndCachePlaceName(fetcher, lindemanLat, lindemanLon); got != "" {
		t.Fatalf("expected an empty result while backed off, got %q", got)
	}
	if fetcher.callCount() != 1 {
		t.Fatalf("expected no additional upstream call while backoff is active, got %d total calls", fetcher.callCount())
	}
}

func TestPlaceNameBackoff_DoublesOnRepeatedFailureUpToMax(t *testing.T) {
	b := &placeNameBackoffState{}

	b.recordFailure()
	if b.nextDelay != placeNameBackoffInitial {
		t.Fatalf("expected the first failure to set the initial delay %s, got %s", placeNameBackoffInitial, b.nextDelay)
	}

	// Force the window open (as if still within it) so the next failure
	// doubles rather than restarting the ladder.
	b.until = time.Now().Add(time.Hour)
	b.recordFailure()
	if want := 2 * placeNameBackoffInitial; b.nextDelay != want {
		t.Fatalf("expected the second consecutive failure to double to %s, got %s", want, b.nextDelay)
	}

	// Keep doubling well past placeNameBackoffMax - it must clamp, not
	// overflow past it.
	for i := 0; i < 10; i++ {
		b.until = time.Now().Add(time.Hour)
		b.recordFailure()
	}
	if b.nextDelay != placeNameBackoffMax {
		t.Fatalf("expected the delay to clamp at placeNameBackoffMax (%s), got %s", placeNameBackoffMax, b.nextDelay)
	}
}

func TestPlaceNameBackoff_RecordSuccessClearsTheLadder(t *testing.T) {
	b := &placeNameBackoffState{}
	b.recordFailure()
	if !b.active() {
		t.Fatalf("expected backoff to be active immediately after a failure")
	}

	b.recordSuccess()
	if b.active() {
		t.Fatalf("expected recordSuccess to clear the backoff window")
	}

	// The ladder must also have reset, not merely the window - a failure
	// right after a success starts back at the initial delay.
	b.recordFailure()
	if b.nextDelay != placeNameBackoffInitial {
		t.Fatalf("expected the ladder to restart at the initial delay after a success, got %s", b.nextDelay)
	}
}

// ── HTTP client timeout vs. the query's own [timeout:N] ────────────────────

func TestOverpassTimeouts_ClientTimeoutExceedsQueryTimeout(t *testing.T) {
	if overpassTimeout <= time.Duration(overpassQueryTimeoutSeconds)*time.Second {
		t.Fatalf("expected the HTTP client timeout (%s) to exceed the query's own [timeout:%ds], it must not give up while Overpass is still working within its stated budget", overpassTimeout, overpassQueryTimeoutSeconds)
	}
}

func TestBuildOverpassQuery_EmbeddedTimeoutMatchesTheConstant(t *testing.T) {
	query := buildOverpassQuery(400, goldsmithLat, goldsmithLon)
	want := fmt.Sprintf("[timeout:%d]", overpassQueryTimeoutSeconds)
	if !strings.Contains(query, want) {
		t.Fatalf("expected the built query to embed %q, got: %s", want, query)
	}
}

// TestFetchOverpassRingTreatsHTMLRateLimitBodyAsFailure isolates the
// rate-limit detection itself: a 200 with an HTML body must surface as an
// explicit, distinctly-logged failure, never silently parsed as "zero
// elements" (which would read identically to a legitimate empty ring).
func TestFetchOverpassRingTreatsHTMLRateLimitBodyAsFailure(t *testing.T) {
	var logBuf bytes.Buffer
	origOutput := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(origOutput) })

	rateLimitedBody := []byte(`<html><body>Dispatcher_Client::request_read_and_idx::rate_limited</body></html>`)
	fetcher := &fakeOverpassFetcher{
		fixtures: map[int][]byte{400: rateLimitedBody},
		types:    map[int]string{400: "text/html"},
	}

	_, err := fetchOverpassRing(fetcher, 400, goldsmithLat, goldsmithLon)
	if err == nil {
		t.Fatalf("expected an explicit error for a rate-limited (HTTP 200, HTML body) response")
	}
	if !strings.Contains(strings.ToLower(logBuf.String()), "rate") {
		t.Fatalf("expected an explicit rate-limit log line distinct from a generic parse failure, got: %q", logBuf.String())
	}
}

// ── updateTickPlaceName: anchor-bound pin/skip and retry-fill ──────────────

func withAnchorWatchState(t *testing.T, state *anchorWatchData) {
	t.Helper()
	orig := anchorWatchState
	anchorWatchMu.Lock()
	anchorWatchState = state
	anchorWatchMu.Unlock()
	t.Cleanup(func() {
		anchorWatchMu.Lock()
		anchorWatchState = orig
		anchorWatchMu.Unlock()
	})
}

func TestUpdateTickPlaceName_SkipsLiveResolutionWhilePinned(t *testing.T) {
	withAnchorWatchState(t, &anchorWatchData{Lat: goldsmithLat, Lon: goldsmithLon, PlaceName: "Goldsmith Island"})

	fetcher := &fakeOverpassFetcher{}
	withFakeOverpassFetcher(t, fetcher)

	got := updateTickPlaceName(goldsmithLat, goldsmithLon)
	if got != "Goldsmith Island" {
		t.Fatalf("expected the pinned anchor place name, got %q", got)
	}
	if fetcher.callCount() != 0 {
		t.Fatalf("expected no live resolution while a named anchor watch is pinned, got %d calls", fetcher.callCount())
	}
}

func TestUpdateTickPlaceName_FillsUnresolvedAnchorPlaceNameOnRetry(t *testing.T) {
	anchorTestEnv(t, 0) // isolates ANCHOR_WATCH_FILE so saveAnchorWatch doesn't touch the real cache dir
	resetPlaceNameCache(t)
	resetPlaceNameTickState(t)
	withAnchorWatchState(t, &anchorWatchData{Lat: goldsmithLat, Lon: goldsmithLon, PlaceName: ""})

	fetcher := &fakeOverpassFetcher{fixtures: map[int][]byte{
		400: loadOverpassFixture(t, "overpass_goldsmith_400.json"),
	}}
	withFakeOverpassFetcher(t, fetcher)

	// The tick starts the retry in the background and returns straight away;
	// the pinned name lands once Overpass answers.
	tickOrFail(t, goldsmithLat, goldsmithLon)

	waitForCondition(t, 2*time.Second, func() bool {
		anchorWatchMu.RLock()
		defer anchorWatchMu.RUnlock()
		return anchorWatchState.PlaceName == "Goldsmith Island"
	})

	// resolveAndPinAnchorWatchPlaceName's background goroutine updates the
	// in-memory state above and THEN calls saveAnchorWatch (a real file
	// write, now fsync'd) before it returns and clears placeNameResolve's
	// in-flight flag. Without waiting for that flag to clear too, the test
	// can return - and t.TempDir() can start tearing down its directory -
	// while that write is still in flight, intermittently racing the
	// temp-dir cleanup (observed: ENOENT/EINVAL from writeJSONFileAtomic,
	// and "directory not empty" from TempDir's own cleanup).
	waitForPlaceNameResolveIdle(t)
}

// ── /api/place-name handler: pure cache read, anchor override ──────────────

func callPlaceNameHandler(t *testing.T) map[string]string {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/place-name", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if err := placeName(c); err != nil {
		t.Fatalf("placeName handler: %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return decoded
}

func TestPlaceNameHandler_ServesTickCacheWhenNoAnchorWatch(t *testing.T) {
	withAnchorWatchState(t, nil)
	origName := getCurrentPlaceName()
	t.Cleanup(func() { setCurrentPlaceName(origName) })
	setCurrentPlaceName("Urangan")

	resp := callPlaceNameHandler(t)
	if resp["name"] != "Urangan" {
		t.Fatalf("expected handler to serve the tick's cached name, got %q", resp["name"])
	}
}

func TestPlaceNameHandler_ServesAnchorBoundNameWhenActive(t *testing.T) {
	withAnchorWatchState(t, &anchorWatchData{Lat: goldsmithLat, Lon: goldsmithLon, PlaceName: "Goldsmith Island"})
	origName := getCurrentPlaceName()
	t.Cleanup(func() { setCurrentPlaceName(origName) })
	setCurrentPlaceName("Somewhere Else Entirely")

	resp := callPlaceNameHandler(t)
	if resp["name"] != "Goldsmith Island" {
		t.Fatalf("expected handler to serve the pinned anchor place name over the tick's cache, got %q", resp["name"])
	}
}

// ── The poll tick must never block on Overpass ─────────────────────────────

// gatedFetcher returns a fetcher whose every Do blocks until the returned
// release func is called, so a test can hold a resolution in flight.
func gatedFetcher(t *testing.T, fixtures map[int][]byte) (*fakeOverpassFetcher, func()) {
	t.Helper()
	gate := make(chan struct{})
	fetcher := &fakeOverpassFetcher{fixtures: fixtures, gate: gate}
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	return fetcher, release
}

// resetPlaceNameTickState clears the tick's published cell/name and the
// single-flight guard so a background resolve from an earlier test cannot
// bleed into this one, restoring the originals afterward.
func resetPlaceNameTickState(t *testing.T) {
	t.Helper()
	currentPlaceNameStore.mu.Lock()
	origName, origCell := currentPlaceNameStore.name, currentPlaceNameStore.cellKey
	currentPlaceNameStore.name, currentPlaceNameStore.cellKey = "", ""
	currentPlaceNameStore.mu.Unlock()

	placeNameResolve.mu.Lock()
	placeNameResolve.active = false
	placeNameResolve.mu.Unlock()

	t.Cleanup(func() {
		currentPlaceNameStore.mu.Lock()
		currentPlaceNameStore.name, currentPlaceNameStore.cellKey = origName, origCell
		currentPlaceNameStore.mu.Unlock()
	})
}

// tickOrFail calls updateTickPlaceName and fails the test if it does not
// return promptly, instead of hanging the suite. A regression that puts the
// Overpass round trip back on the tick must fail fast and say why.
func tickOrFail(t *testing.T, lat, lon float64) string {
	t.Helper()
	var name string
	returned := make(chan struct{})
	go func() {
		name = updateTickPlaceName(lat, lon)
		close(returned)
	}()
	select {
	case <-returned:
		return name
	case <-time.After(2 * time.Second):
		t.Fatal("updateTickPlaceName blocked on the Overpass round trip; the poll tick is sequential, so this stalls depth/wind/solar history, track points and contact recording behind it")
		return ""
	}
}

func waitForPlaceNameResolveIdle(t *testing.T) {
	t.Helper()
	waitForCondition(t, 2*time.Second, func() bool {
		placeNameResolve.mu.Lock()
		defer placeNameResolve.mu.Unlock()
		return !placeNameResolve.active
	})
}

// startTrackPoller runs sampleTracks sequentially on a single goroutine
// (`for range ticker.C`), and Go drops ticks while that receiver is busy. A
// synchronous resolve there stalls every other thing the tick samples, for
// up to three rings x overpassTimeout.
func TestUpdateTickPlaceName_DoesNotBlockTheTick(t *testing.T) {
	resetPlaceNameCache(t)
	withAnchorWatchState(t, nil)
	resetPlaceNameTickState(t)

	fetcher, release := gatedFetcher(t, map[int][]byte{
		400: loadOverpassFixture(t, "overpass_goldsmith_400.json"),
	})
	withFakeOverpassFetcher(t, fetcher)

	tickOrFail(t, goldsmithLat, goldsmithLon)

	release()
	waitForCondition(t, 2*time.Second, func() bool { return getCurrentPlaceName() == "Goldsmith Island" })
	waitForPlaceNameResolveIdle(t)
}

func TestUpdateTickPlaceName_SingleFlightPreventsGoroutinePileUp(t *testing.T) {
	resetPlaceNameCache(t)
	withAnchorWatchState(t, nil)
	resetPlaceNameTickState(t)

	fetcher, release := gatedFetcher(t, map[int][]byte{
		400: loadOverpassFixture(t, "overpass_goldsmith_400.json"),
	})
	withFakeOverpassFetcher(t, fetcher)

	tickOrFail(t, goldsmithLat, goldsmithLon)
	waitForCondition(t, 2*time.Second, func() bool { return fetcher.callCount() == 1 })

	for range 5 {
		tickOrFail(t, goldsmithLat, goldsmithLon)
	}

	if got := fetcher.callCount(); got != 1 {
		t.Fatalf("expected the single-flight guard to hold at one in-flight resolution, got %d upstream calls; a 5s tick against a 60s worst case stacks goroutines without it", got)
	}

	release()
	waitForCondition(t, 2*time.Second, func() bool { return getCurrentPlaceName() == "Goldsmith Island" })
	waitForPlaceNameResolveIdle(t)
}

// The reported bug in miniature: a lookup for one place landing after the
// vessel has moved, and being shown as where the vessel is now.
func TestUpdateTickPlaceName_StaleResolveDoesNotPublishAfterMovingCell(t *testing.T) {
	resetPlaceNameCache(t)
	withAnchorWatchState(t, nil)
	resetPlaceNameTickState(t)

	fetcher, release := gatedFetcher(t, map[int][]byte{
		400: loadOverpassFixture(t, "overpass_goldsmith_400.json"),
	})
	withFakeOverpassFetcher(t, fetcher)

	tickOrFail(t, goldsmithLat, goldsmithLon)
	waitForCondition(t, 2*time.Second, func() bool { return fetcher.callCount() == 1 })

	// The vessel moves to a different cell before that resolve comes back.
	tickOrFail(t, lindemanLat, lindemanLon)

	release()
	waitForPlaceNameResolveIdle(t)

	if got := getCurrentPlaceName(); got == "Goldsmith Island" {
		t.Fatalf("a resolve for the previous cell published %q after the vessel had already moved to another cell", got)
	}
}
