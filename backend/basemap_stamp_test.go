package main

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

const (
	oldStamp = "20261001-03"
	newStamp = "20270101-01"
)

// stampedUpstream stands in for Carto while it rotates its release stamp
// (".../v/<stamp>/..."). Every asset URL carrying a stamp other than current
// answers 404, as a retired release does; style.json always describes
// current.
type stampedUpstream struct {
	mu      sync.Mutex
	t       *testing.T
	current string
	// styleBody, when set, replaces the style served (for poisoned styles).
	styleBody func(theme string) string
	urls      []string
}

func newStampedUpstream(t *testing.T, current string) (*stampedUpstream, *fakeTileFetcher) {
	u := &stampedUpstream{t: t, current: current}
	return u, &fakeTileFetcher{responder: u.respond}
}

func (u *stampedUpstream) setCurrent(s string) {
	u.mu.Lock()
	u.current = s
	u.mu.Unlock()
}

func (u *stampedUpstream) respond(req *http.Request) (*http.Response, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	url := req.URL.String()
	u.urls = append(u.urls, url)
	if strings.HasSuffix(req.URL.Path, "/style.json") {
		theme := "positron"
		if strings.Contains(url, "dark-matter") {
			theme = "dark-matter"
		}
		body := strings.ReplaceAll(cartoFixture(u.t, theme), oldStamp, u.current)
		if u.styleBody != nil {
			body = u.styleBody(theme)
		}
		return fakeUpstreamResponse(http.StatusOK, "application/json", []byte(body)), nil
	}
	if !strings.Contains(url, "/v/"+u.current+"/") {
		return fakeUpstreamResponse(http.StatusNotFound, "text/plain", []byte("gone")), nil
	}
	return fakeUpstreamResponse(http.StatusOK, "application/octet-stream", []byte("bytes")), nil
}

func (u *stampedUpstream) count(substr string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	n := 0
	for _, x := range u.urls {
		if strings.Contains(x, substr) {
			n++
		}
	}
	return n
}

func requestTile(t *testing.T, cache *tileCache, fetcher tileFetcher, z, x, y string) (int, string) {
	t.Helper()
	c, rec := newBasemapTestContext("/", []string{"z", "x", "y"}, []string{z, x, y})
	if err := basemapVectorTileHandler(cache, fetcher)(c); err != nil {
		t.Fatal(err)
	}
	return rec.Code, rec.Body.String()
}

func cachedStampOf(t *testing.T, cache *tileCache, theme string) string {
	t.Helper()
	raw, _, ok, err := cache.getBasemapAsset(cartoUpstreamAssetPath(theme))
	if err != nil || !ok {
		t.Fatalf("no cached upstream style for %s: ok=%v err=%v", theme, ok, err)
	}
	if strings.Contains(string(raw), "/v/"+newStamp+"/") {
		return newStamp
	}
	if strings.Contains(string(raw), "/v/"+oldStamp+"/") {
		return oldStamp
	}
	return "unknown"
}

func TestStampRotation_TileIsServedFromTheNewStamp(t *testing.T) {
	cache := newTestTileCache(t)
	seedCartoUpstream(t, cache) // an install that learned the old stamp
	// A tile cached under the old stamp stays valid: same v2 schema.
	if err := cache.put(cartoBasemapSource, 12, 1, 1, []byte("old-tile"), "application/x-protobuf"); err != nil {
		t.Fatal(err)
	}
	up, fetcher := newStampedUpstream(t, newStamp)

	if code, body := requestTile(t, cache, fetcher, "12", "1", "1"); code != http.StatusOK || body != "old-tile" {
		t.Fatalf("cached tile: code=%d body=%q", code, body)
	}
	if len(up.urls) != 0 {
		t.Fatalf("cached tile reached upstream: %v", up.urls)
	}

	if code, body := requestTile(t, cache, fetcher, "12", "3742", "2283"); code != http.StatusOK || body != "bytes" {
		t.Fatalf("uncached tile after rotation: code=%d body=%q", code, body)
	}
	if got := cachedStampOf(t, cache, "positron"); got != newStamp {
		t.Errorf("cached upstream style stamp = %s, want the new one", got)
	}
	// Later misses go straight to the new stamp: no further style fetch.
	if code, _ := requestTile(t, cache, fetcher, "12", "3743", "2283"); code != http.StatusOK {
		t.Fatalf("second tile: %d", code)
	}
	if n := up.count("positron-gl-style/style.json"); n != 1 {
		t.Errorf("style refetched %d times, want 1", n)
	}
	if n := up.count("/v/" + oldStamp + "/"); n != 1 {
		t.Errorf("old stamp tried %d times, want only the one failing request", n)
	}
}

func TestStampRotation_GlyphsAndSpritesRecoverToo(t *testing.T) {
	cache := newTestTileCache(t)
	seedCartoUpstream(t, cache)
	up, fetcher := newStampedUpstream(t, newStamp)

	cF, rF := newBasemapTestContext("/", []string{"fontstack", "range"}, []string{"Open Sans Italic", "0-255.pbf"})
	if err := basemapFontsHandler(cache, fetcher)(cF); err != nil {
		t.Fatal(err)
	}
	if rF.Code != http.StatusOK {
		t.Fatalf("glyphs after rotation: %d %s", rF.Code, rF.Body.String())
	}
	cS, rS := newBasemapTestContext("/", []string{"name"}, []string{"dark-matter@2x.png"})
	if err := basemapSpriteHandler(cache, fetcher)(cS); err != nil {
		t.Fatal(err)
	}
	if rS.Code != http.StatusOK {
		t.Fatalf("sprite after rotation: %d %s", rS.Code, rS.Body.String())
	}
	if got := cachedStampOf(t, cache, "dark-matter"); got != newStamp {
		t.Errorf("dark-matter upstream stamp = %s", got)
	}
	// Themes refresh independently.
	if n := up.count("dark-matter-gl-style/style.json"); n != 1 {
		t.Errorf("dark-matter style fetched %d times, want 1", n)
	}
}

func TestStampRotation_BurstOfMissesRefetchesTheStyleOnce(t *testing.T) {
	cache := newTestTileCache(t)
	seedCartoUpstream(t, cache)
	up, fetcher := newStampedUpstream(t, newStamp)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, rec := newBasemapTestContext("/", []string{"z", "x", "y"}, []string{"12", itoa(3000 + i), "2000"})
			if err := basemapVectorTileHandler(cache, fetcher)(c); err != nil {
				t.Error(err)
			}
			if rec.Code != http.StatusOK {
				t.Errorf("tile %d: %d %s", i, rec.Code, rec.Body.String())
			}
		}(i)
	}
	wg.Wait()
	if n := up.count("positron-gl-style/style.json"); n != 1 {
		t.Errorf("style refetched %d times for a burst, want 1", n)
	}
}

// If the style still names the same stamp, the 404 is real (no such tile):
// the original error stands, with no loop and no repeat style fetch inside
// the rate-limit window.
func TestStampRotation_SameStampKeepsTheOriginalError(t *testing.T) {
	cache := newTestTileCache(t)
	seedCartoUpstream(t, cache)
	up := &fakeTileFetcher{}
	up.responder = func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/style.json") {
			theme := "positron"
			return fakeUpstreamResponse(http.StatusOK, "application/json", []byte(cartoFixture(t, theme))), nil
		}
		return fakeUpstreamResponse(http.StatusNotFound, "text/plain", []byte("no tile")), nil
	}

	if code, _ := requestTile(t, cache, up, "12", "5", "5"); code != http.StatusNotFound {
		t.Fatalf("tile: %d, want 404", code)
	}
	calls := up.callCount() // one tile attempt + one style refetch, no retry
	if calls != 2 {
		t.Errorf("upstream calls = %d, want 2 (tile, style)", calls)
	}
	// Another miss inside the window: tile only, no new style fetch.
	if code, _ := requestTile(t, cache, up, "12", "6", "5"); code != http.StatusNotFound {
		t.Fatalf("tile: %d", code)
	}
	if got := up.callCount(); got != calls+1 {
		t.Errorf("upstream calls = %d, want %d (rate limit)", got, calls+1)
	}

	cF, rF := newBasemapTestContext("/", []string{"fontstack", "range"}, []string{"X", "0-255.pbf"})
	if err := basemapFontsHandler(cache, up)(cF); err != nil {
		t.Fatal(err)
	}
	if rF.Code != http.StatusBadGateway {
		t.Errorf("glyphs: %d, want 502", rF.Code)
	}
}

func TestStampRotation_RefetchedStyleFailingTheAllowlistIsRejected(t *testing.T) {
	cache := newTestTileCache(t)
	seedCartoUpstream(t, cache)
	up, fetcher := newStampedUpstream(t, newStamp)
	up.styleBody = func(theme string) string {
		return strings.ReplaceAll(cartoFixture(t, theme), "tiles.basemaps.cartocdn.com/v/"+oldStamp, "evil.example/v/"+newStamp)
	}

	if code, _ := requestTile(t, cache, fetcher, "12", "7", "7"); code != http.StatusBadGateway {
		t.Fatalf("tile: %d, want 502 (the refresh was rejected)", code)
	}
	if got := cachedStampOf(t, cache, "positron"); got != oldStamp {
		t.Errorf("a style failing the allowlist replaced the cached one (stamp %s)", got)
	}
	if n := up.count("evil.example"); n != 0 {
		t.Errorf("fetched a foreign host %d times", n)
	}
}

func itoa(i int) string { return fmt.Sprint(i) }
