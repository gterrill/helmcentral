package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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

// resetPlaceNameCache swaps in a fresh, empty placeNameCache for the
// duration of the test so cache state never leaks between tests, and
// restores the original afterward.
func resetPlaceNameCache(t *testing.T) {
	t.Helper()
	orig := placeNameCache
	placeNameCache = &placeNameCacheStore{data: make(map[string]placeNameCacheEntry)}
	t.Cleanup(func() { placeNameCache = orig })
}

// resetPlaceNameBackoff clears the package-level place-names provider
// backoff window for the duration of the test, restoring the original
// afterward - mirrors resetPlaceNameCache so a failure induced by one
// test's fake provider can never leak a backoff window into another test
// (or into a later call within the same test that expects a real retry).
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

// withFakePlaceNameProviderResolver swaps the package-level
// placeNameProviderResolve (used by both the tick and setAnchorWatch's
// async resolve) for a resolver that always returns provider, and restores
// the original afterward. It also resets the backoff window
// (resetPlaceNameBackoff): that state is process-wide and keyed on real
// wall-clock time (up to placeNameBackoffMax, 15 minutes), so a failure
// induced by one test's fake provider would otherwise silently suppress
// every other test's resolution attempts - including successful ones - for
// however much of that window the rest of the run takes.
func withFakePlaceNameProviderResolver(t *testing.T, provider placeNameProvider) {
	t.Helper()
	orig := placeNameProviderResolve
	placeNameProviderResolve = func() (placeNameProvider, string, error) { return provider, provider.ID(), nil }
	t.Cleanup(func() { placeNameProviderResolve = orig })
	resetPlaceNameBackoff(t)
}

// waitForCondition polls cond until it returns true or timeout elapses,
// failing the test on timeout. Used for the goroutine-based anchor-watch
// resolve path (setAnchorWatch deliberately doesn't block its HTTP response
// on the provider round trip).
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

// ── fake placeNameProvider ───────────────────────────────────────────────

// fakePlaceNameProvider is an injectable placeNameProvider for tests
// exercising place_name.go's ladder/cache/backoff/tick logic, independent
// of the POI provider registry (place_name_provider_test.go's
// stubPlaceNameProvider exercises resolution against that registry
// instead). Results/errors are keyed by the ring radius actually asked
// for, so a table test can assert both *which* radius wins and *how many*
// (and which) rings were actually queried - mirroring the deleted
// fakeOverpassFetcher's shape, minus everything that was really Overpass
// wire-format concern (now the plugin's problem, not the host's).
type fakePlaceNameProvider struct {
	id string

	mu      sync.Mutex
	calls   []int
	results map[int]placeNameResult
	errs    map[int]error

	// gate, when non-nil, blocks every PlaceNameAt call until the channel is
	// closed, so a test can hold a resolution "in flight" and assert what
	// the caller does while it is outstanding. A channel rather than a
	// sleep: nothing races a wall clock.
	gate chan struct{}
}

func (f *fakePlaceNameProvider) ID() string   { return f.id }
func (f *fakePlaceNameProvider) Name() string { return f.id }

func (f *fakePlaceNameProvider) PlaceNameAt(lat, lon float64, radiusM int) (placeNameResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, radiusM)
	gate := f.gate
	f.mu.Unlock()

	if gate != nil {
		<-gate
	}

	if err, ok := f.errs[radiusM]; ok {
		return placeNameResult{}, err
	}
	return f.results[radiusM], nil
}

func (f *fakePlaceNameProvider) SearchPlaces(placeSearchInput) (placeSearchResult, error) {
	return placeSearchResult{}, fmt.Errorf("fakePlaceNameProvider %q: SearchPlaces not implemented", f.id)
}

func (f *fakePlaceNameProvider) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakePlaceNameProvider) callsSnapshot() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]int, len(f.calls))
	copy(out, f.calls)
	return out
}

// gatedPlaceNameProvider returns a fakePlaceNameProvider whose every
// PlaceNameAt call blocks until the returned release func is called, so a
// test can hold a resolution in flight.
func gatedPlaceNameProvider(t *testing.T, results map[int]placeNameResult) (*fakePlaceNameProvider, func()) {
	t.Helper()
	gate := make(chan struct{})
	provider := &fakePlaceNameProvider{id: "fake-place-names", results: results, gate: gate}
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	return provider, release
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

// ── Test 2: the ladder stops at the tightest ring with a named answer ──────

func TestResolvePlaceNameStopsAtTightestRing(t *testing.T) {
	tests := []struct {
		name      string
		results   map[int]placeNameResult
		wantName  string
		wantCalls []int
	}{
		{
			name: "resolves on the first 400m ring, never widens",
			results: map[int]placeNameResult{
				400: {Name: "Goldsmith Island", Kind: "island"},
			},
			wantName:  "Goldsmith Island",
			wantCalls: []int{400},
		},
		{
			name: "an empty 400m ring forces widening to 1500m, and stops there",
			results: map[int]placeNameResult{
				1500: {Name: "Lindeman Island", Kind: "island"},
			},
			wantName:  "Lindeman Island",
			wantCalls: []int{400, 1500},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakePlaceNameProvider{id: "fake-place-names", results: tt.results}
			resolve := func() (placeNameProvider, string, error) { return provider, provider.ID(), nil }

			got, err := resolvePlaceName(resolve, goldsmithLat, goldsmithLon)
			if err != nil {
				t.Fatalf("resolvePlaceName: %v", err)
			}
			if got != tt.wantName {
				t.Fatalf("expected name %q, got %q", tt.wantName, got)
			}
			if calls := provider.callsSnapshot(); !equalInts(calls, tt.wantCalls) {
				t.Fatalf(
					"expected rings queried %v, got %v (the 5000m ring must never be requested once a tighter ring hits)",
					tt.wantCalls, calls,
				)
			}
		})
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A ladder that exhausts every ring with no named answer is a legitimate
// negative, not a failure - resolvePlaceName must walk all three rings and
// return "" with no error.
func TestResolvePlaceNameExhaustsLadderWithNoNamedAnswer(t *testing.T) {
	provider := &fakePlaceNameProvider{id: "fake-place-names"}
	resolve := func() (placeNameProvider, string, error) { return provider, provider.ID(), nil }

	got, err := resolvePlaceName(resolve, lindemanLat, lindemanLon)
	if err != nil {
		t.Fatalf("resolvePlaceName: %v", err)
	}
	if got != "" {
		t.Fatalf("expected an empty name when no ring resolves, got %q", got)
	}
	if want := placeNameRadiiMeters; !equalInts(provider.callsSnapshot(), want) {
		t.Fatalf("expected all rings queried in order %v, got %v", want, provider.callsSnapshot())
	}
}

// A provider-resolution failure (e.g. the configured plugin isn't
// installed) must surface as an error, not silently return "".
func TestResolvePlaceName_ProviderResolutionErrorSurfaces(t *testing.T) {
	resolve := func() (placeNameProvider, string, error) { return nil, "", fmt.Errorf("simulated resolution failure") }

	_, err := resolvePlaceName(resolve, goldsmithLat, goldsmithLon)
	if err == nil {
		t.Fatalf("expected an error when the place-names provider fails to resolve")
	}
}

// ── Test 4: a failed lookup is never cached, and retries ───────────────────

func TestPlaceNameFailureIsNotCached(t *testing.T) {
	resetPlaceNameCache(t)
	resetPlaceNameBackoff(t)

	provider := &fakePlaceNameProvider{id: "fake-place-names", errs: map[int]error{
		400: fmt.Errorf("simulated upstream failure"),
	}}
	resolve := func() (placeNameProvider, string, error) { return provider, provider.ID(), nil }

	key := placeNameCacheKey(goldsmithLat, goldsmithLon)

	got := resolveAndCachePlaceName(resolve, goldsmithLat, goldsmithLon)
	if got != "" {
		t.Fatalf("expected empty name on a failed response, got %q", got)
	}
	if provider.callCount() != 1 {
		t.Fatalf("expected exactly 1 upstream call, got %d", provider.callCount())
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
	// blank, and once the provider succeeds, the resolved name is what's
	// cached.
	provider2 := &fakePlaceNameProvider{id: "fake-place-names", results: map[int]placeNameResult{
		400: {Name: "Goldsmith Island", Kind: "island"},
	}}
	resolve2 := func() (placeNameProvider, string, error) { return provider2, provider2.ID(), nil }
	got2 := resolveAndCachePlaceName(resolve2, goldsmithLat, goldsmithLon)
	if got2 != "Goldsmith Island" {
		t.Fatalf("expected the retry to resolve Goldsmith Island, got %q", got2)
	}
	if cached, ok := placeNameCache.get(key); !ok || cached != "Goldsmith Island" {
		t.Fatalf("expected the successful resolution to be cached, got %q ok=%v", cached, ok)
	}
}

// ── Empty results: cached with a shorter TTL, refetched after it ──────────

func TestResolveAndCachePlaceName_EmptyResultIsCachedAndNotRefetchedWithinTTL(t *testing.T) {
	resetPlaceNameCache(t)
	resetPlaceNameBackoff(t)

	provider := &fakePlaceNameProvider{id: "fake-place-names"}
	resolve := func() (placeNameProvider, string, error) { return provider, provider.ID(), nil }

	got := resolveAndCachePlaceName(resolve, goldsmithLat, goldsmithLon)
	if got != "" {
		t.Fatalf("expected an empty name when every ring is genuinely empty, got %q", got)
	}
	if want := len(placeNameRadiiMeters); provider.callCount() != want {
		t.Fatalf("expected the ladder to walk all %d rings, got %d calls", want, provider.callCount())
	}

	key := placeNameCacheKey(goldsmithLat, goldsmithLon)
	if _, ok := placeNameCache.get(key); !ok {
		t.Fatalf("expected the genuine empty result to be cached (a real answer, not a masking fallback)")
	}

	// A second call within the TTL must be served from the cache: no
	// further upstream calls at all.
	got2 := resolveAndCachePlaceName(resolve, goldsmithLat, goldsmithLon)
	if got2 != "" {
		t.Fatalf("expected the cached empty result, got %q", got2)
	}
	if provider.callCount() != len(placeNameRadiiMeters) {
		t.Fatalf("expected no additional upstream calls while the empty result is within its TTL, got %d total calls", provider.callCount())
	}
}

func TestResolveAndCachePlaceName_EmptyResultRefetchedAfterItsTTL(t *testing.T) {
	resetPlaceNameCache(t)
	resetPlaceNameBackoff(t)

	provider := &fakePlaceNameProvider{id: "fake-place-names"}
	resolve := func() (placeNameProvider, string, error) { return provider, provider.ID(), nil }

	if got := resolveAndCachePlaceName(resolve, goldsmithLat, goldsmithLon); got != "" {
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

	// A resolve past the empty-result TTL must walk the provider again.
	if got := resolveAndCachePlaceName(resolve, goldsmithLat, goldsmithLon); got != "" {
		t.Fatalf("expected an empty result again, got %q", got)
	}
	if want := 2 * len(placeNameRadiiMeters); provider.callCount() != want {
		t.Fatalf("expected a full second ladder walk after TTL expiry, got %d total calls (want %d)", provider.callCount(), want)
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

	provider := &fakePlaceNameProvider{id: "fake-place-names", errs: map[int]error{
		400: fmt.Errorf("simulated upstream failure"),
	}}
	resolve := func() (placeNameProvider, string, error) { return provider, provider.ID(), nil }

	if got := resolveAndCachePlaceName(resolve, goldsmithLat, goldsmithLon); got != "" {
		t.Fatalf("expected an empty result on the failing first call, got %q", got)
	}
	if provider.callCount() != 1 {
		t.Fatalf("expected exactly 1 upstream call for the first (failing) attempt, got %d", provider.callCount())
	}

	// A different position (so the cache can't be the reason for the
	// no-op) must still be skipped entirely while backoff is active - the
	// whole point being that the provider itself, not just this one cell,
	// is assumed unreachable for the window.
	if got := resolveAndCachePlaceName(resolve, lindemanLat, lindemanLon); got != "" {
		t.Fatalf("expected an empty result while backed off, got %q", got)
	}
	if provider.callCount() != 1 {
		t.Fatalf("expected no additional upstream call while backoff is active, got %d total calls", provider.callCount())
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

	provider := &fakePlaceNameProvider{id: "fake-place-names"}
	withFakePlaceNameProviderResolver(t, provider)

	got := updateTickPlaceName(goldsmithLat, goldsmithLon)
	if got != "Goldsmith Island" {
		t.Fatalf("expected the pinned anchor place name, got %q", got)
	}
	if provider.callCount() != 0 {
		t.Fatalf("expected no live resolution while a named anchor watch is pinned, got %d calls", provider.callCount())
	}
}

func TestUpdateTickPlaceName_FillsUnresolvedAnchorPlaceNameOnRetry(t *testing.T) {
	anchorTestEnv(t, 0) // isolates ANCHOR_WATCH_FILE so saveAnchorWatch doesn't touch the real cache dir
	resetPlaceNameCache(t)
	resetPlaceNameTickState(t)
	withAnchorWatchState(t, &anchorWatchData{Lat: goldsmithLat, Lon: goldsmithLon, PlaceName: ""})

	provider := &fakePlaceNameProvider{id: "fake-place-names", results: map[int]placeNameResult{
		400: {Name: "Goldsmith Island", Kind: "island"},
	}}
	withFakePlaceNameProviderResolver(t, provider)

	// The tick starts the retry in the background and returns straight away;
	// the pinned name lands once the provider answers.
	tickOrFail(t, goldsmithLat, goldsmithLon)

	waitForCondition(t, 2*time.Second, func() bool {
		anchorWatchMu.RLock()
		defer anchorWatchMu.RUnlock()
		return anchorWatchState.PlaceName == "Goldsmith Island"
	})

	// resolveAndPinAnchorWatchPlaceName's background goroutine updates the
	// in-memory state and THEN calls saveAnchorWatch (writeJSONFileAtomic,
	// which now fsyncs) - both on the same background goroutine, but with
	// no signal back to this goroutine for "the save has landed too".
	// Checking the file's own content directly here (rather than racing
	// straight into the memory-wipe-and-reload below the moment the
	// in-memory flag flips) is what makes this wait actually wait for the
	// persisted write, not just the update that precedes it.
	waitForCondition(t, 2*time.Second, func() bool {
		raw, err := os.ReadFile(anchorWatchFilePath())
		return err == nil && strings.Contains(string(raw), "Goldsmith Island")
	})
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

// ── The poll tick must never block on the place-names provider ────────────

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
// provider round trip back on the tick must fail fast and say why.
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
		t.Fatal("updateTickPlaceName blocked on the place-names provider round trip; the poll tick is sequential, so this stalls depth/wind/solar history, track points and contact recording behind it")
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
// as long as the provider's ladder takes to answer.
func TestUpdateTickPlaceName_DoesNotBlockTheTick(t *testing.T) {
	resetPlaceNameCache(t)
	withAnchorWatchState(t, nil)
	resetPlaceNameTickState(t)

	provider, release := gatedPlaceNameProvider(t, map[int]placeNameResult{
		400: {Name: "Goldsmith Island", Kind: "island"},
	})
	withFakePlaceNameProviderResolver(t, provider)

	tickOrFail(t, goldsmithLat, goldsmithLon)

	release()
	waitForCondition(t, 2*time.Second, func() bool { return getCurrentPlaceName() == "Goldsmith Island" })
	waitForPlaceNameResolveIdle(t)
}

func TestUpdateTickPlaceName_SingleFlightPreventsGoroutinePileUp(t *testing.T) {
	resetPlaceNameCache(t)
	withAnchorWatchState(t, nil)
	resetPlaceNameTickState(t)

	provider, release := gatedPlaceNameProvider(t, map[int]placeNameResult{
		400: {Name: "Goldsmith Island", Kind: "island"},
	})
	withFakePlaceNameProviderResolver(t, provider)

	tickOrFail(t, goldsmithLat, goldsmithLon)
	waitForCondition(t, 2*time.Second, func() bool { return provider.callCount() == 1 })

	for range 5 {
		tickOrFail(t, goldsmithLat, goldsmithLon)
	}

	if got := provider.callCount(); got != 1 {
		t.Fatalf("expected the single-flight guard to hold at one in-flight resolution, got %d upstream calls; a 5s tick against a much slower worst case stacks goroutines without it", got)
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

	provider, release := gatedPlaceNameProvider(t, map[int]placeNameResult{
		400: {Name: "Goldsmith Island", Kind: "island"},
	})
	withFakePlaceNameProviderResolver(t, provider)

	tickOrFail(t, goldsmithLat, goldsmithLon)
	waitForCondition(t, 2*time.Second, func() bool { return provider.callCount() == 1 })

	// The vessel moves to a different cell before that resolve comes back.
	tickOrFail(t, lindemanLat, lindemanLon)

	release()
	waitForPlaceNameResolveIdle(t)

	if got := getCurrentPlaceName(); got == "Goldsmith Island" {
		t.Fatalf("a resolve for the previous cell published %q after the vessel had already moved to another cell", got)
	}
}

// ── resolveDestinationPlaceName (ADR 0125): GET /api/routes/active's
// destination name must never block, and must dedupe concurrent lookups
// for the same cell on its own guard, independent of the vessel tick's ──

// resetDestinationPlaceNameResolveState clears the destination resolver's
// single-flight guard so a resolve left in flight by an earlier test cannot
// bleed into this one.
func resetDestinationPlaceNameResolveState(t *testing.T) {
	t.Helper()
	destinationPlaceNameResolve.mu.Lock()
	destinationPlaceNameResolve.active = false
	destinationPlaceNameResolve.mu.Unlock()
}

// resolveDestinationPlaceNameOrFail calls resolveDestinationPlaceName and
// fails the test if it does not return promptly - GET /api/routes/active is
// polled every 15s and must never stall on a provider round trip.
func resolveDestinationPlaceNameOrFail(t *testing.T, lat, lon float64) string {
	t.Helper()
	var name string
	returned := make(chan struct{})
	go func() {
		name = resolveDestinationPlaceName(lat, lon)
		close(returned)
	}()
	select {
	case <-returned:
		return name
	case <-time.After(2 * time.Second):
		t.Fatal("resolveDestinationPlaceName blocked on the place-names provider round trip")
		return ""
	}
}

func waitForDestinationPlaceNameResolveIdle(t *testing.T) {
	t.Helper()
	waitForCondition(t, 2*time.Second, func() bool {
		destinationPlaceNameResolve.mu.Lock()
		defer destinationPlaceNameResolve.mu.Unlock()
		return !destinationPlaceNameResolve.active
	})
}

func TestResolveDestinationPlaceName_ServesCacheHitImmediately(t *testing.T) {
	resetPlaceNameCache(t)
	placeNameCache.put(placeNameCacheKey(goldsmithLat, goldsmithLon), "Goldsmith Island")

	if got := resolveDestinationPlaceName(goldsmithLat, goldsmithLon); got != "Goldsmith Island" {
		t.Fatalf("expected the cached name, got %q", got)
	}
}

func TestResolveDestinationPlaceName_DoesNotBlockOnACacheMiss(t *testing.T) {
	resetPlaceNameCache(t)
	resetDestinationPlaceNameResolveState(t)

	provider, release := gatedPlaceNameProvider(t, map[int]placeNameResult{
		400: {Name: "Hook Island", Kind: "island"},
	})
	withFakePlaceNameProviderResolver(t, provider)

	got := resolveDestinationPlaceNameOrFail(t, goldsmithLat, goldsmithLon)
	if got != "" {
		t.Fatalf("expected an empty name on a cache miss while the resolve is in flight, got %q", got)
	}

	release()
	waitForCondition(t, 2*time.Second, func() bool {
		name, ok := placeNameCache.get(placeNameCacheKey(goldsmithLat, goldsmithLon))
		return ok && name == "Hook Island"
	})
	waitForDestinationPlaceNameResolveIdle(t)
}

func TestResolveDestinationPlaceName_DedupesConcurrentLookupsForTheSameCell(t *testing.T) {
	resetPlaceNameCache(t)
	resetDestinationPlaceNameResolveState(t)

	provider, release := gatedPlaceNameProvider(t, map[int]placeNameResult{
		400: {Name: "Hook Island", Kind: "island"},
	})
	withFakePlaceNameProviderResolver(t, provider)

	resolveDestinationPlaceNameOrFail(t, goldsmithLat, goldsmithLon)
	waitForCondition(t, 2*time.Second, func() bool { return provider.callCount() == 1 })

	for range 5 {
		resolveDestinationPlaceNameOrFail(t, goldsmithLat, goldsmithLon)
	}

	if got := provider.callCount(); got != 1 {
		t.Fatalf("expected the single-flight guard to hold at one in-flight resolution, got %d upstream calls; GET /api/routes/active polls every 15s and a slower provider must not stack goroutines", got)
	}

	release()
	waitForCondition(t, 2*time.Second, func() bool {
		name, ok := placeNameCache.get(placeNameCacheKey(goldsmithLat, goldsmithLon))
		return ok && name == "Hook Island"
	})
	waitForDestinationPlaceNameResolveIdle(t)
}
