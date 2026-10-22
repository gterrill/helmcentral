package main

import (
	"bytes"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClock lets the rate-limit tests move time without sleeping.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (f *fakeClock) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}
func (f *fakeClock) advance(d time.Duration) {
	f.mu.Lock()
	f.t = f.t.Add(d)
	f.mu.Unlock()
}

func withClock(cache *tileCache) *fakeClock {
	c := &fakeClock{t: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}
	cache.now = c.now
	return c
}

func captureBasemapLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

func requestFont(t *testing.T, cache *tileCache, fetcher tileFetcher) (int, string) {
	t.Helper()
	c, rec := newBasemapTestContext("/", []string{"fontstack", "range"}, []string{"Open Sans Italic", "0-255.pbf"})
	if err := basemapFontsHandler(cache, fetcher)(c); err != nil {
		t.Fatal(err)
	}
	return rec.Code, rec.Body.String()
}

// Item 1: a failed or rejected refresh is logged (once per attempt, with the
// theme and the reason) and the returned error carries it.
func TestStampRecovery_RefreshFailureIsLoggedAndCarriedInTheError(t *testing.T) {
	cache := newTestTileCache(t)
	seedCartoUpstream(t, cache)
	buf := captureBasemapLog(t)
	up, fetcher := newStampedUpstream(t, newStamp)
	up.styleBody = func(theme string) string {
		return strings.ReplaceAll(cartoFixture(t, theme), "tiles.basemaps.cartocdn.com/v/"+oldStamp, "evil.example/v/"+newStamp)
	}

	code, body := requestFont(t, cache, fetcher)
	if code != http.StatusBadGateway {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(body, "style refresh failed") || !strings.Contains(body, "not on an allowed Carto host") {
		t.Errorf("error does not carry the refresh failure: %s", body)
	}
	if n := strings.Count(strings.TrimSpace(buf.String()), "\n") + 1; n != 1 || !strings.Contains(buf.String(), "positron") {
		t.Errorf("want one log line naming the theme, got %d:\n%s", n, buf.String())
	}
}

// Item 2: 5 minutes after a refetch that found nothing new, about 30 s after
// one that failed.
func TestStampRecovery_FailedRefreshRetriesAfterShortBackoff(t *testing.T) {
	cache := newTestTileCache(t)
	seedCartoUpstream(t, cache)
	clock := withClock(cache)
	up, fetcher := newStampedUpstream(t, newStamp)
	failing := true
	up.styleBody = nil
	base := fetcher.responder
	fetcher.responder = func(req *http.Request) (*http.Response, error) {
		if failing && strings.HasSuffix(req.URL.Path, "/style.json") {
			up.mu.Lock()
			up.urls = append(up.urls, req.URL.String())
			up.mu.Unlock()
			return fakeUpstreamResponse(http.StatusInternalServerError, "text/plain", nil), nil
		}
		return base(req)
	}

	if code, _ := requestTile(t, cache, fetcher, "12", "1", "1"); code != http.StatusBadGateway {
		t.Fatalf("first tile: %d, want 502 (refresh failed)", code)
	}
	clock.advance(10 * time.Second)
	requestTile(t, cache, fetcher, "12", "2", "1")
	if n := up.count("style.json"); n != 1 {
		t.Fatalf("style fetched %d times inside the backoff, want 1", n)
	}
	failing = false
	clock.advance(25 * time.Second) // 35 s after the failure
	if code, _ := requestTile(t, cache, fetcher, "12", "3", "1"); code != http.StatusOK {
		t.Fatalf("tile after backoff: %d, want 200 from the new stamp", code)
	}
	if n := up.count("style.json"); n != 2 {
		t.Errorf("style fetched %d times, want 2", n)
	}
}

func TestStampRecovery_UnchangedRefreshWaitsFiveMinutes(t *testing.T) {
	cache := newTestTileCache(t)
	seedCartoUpstream(t, cache)
	clock := withClock(cache)
	fetcher := &fakeTileFetcher{}
	styles := 0
	fetcher.responder = func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/style.json") {
			styles++
			return fakeUpstreamResponse(http.StatusOK, "application/json", []byte(cartoFixture(t, "positron"))), nil
		}
		return fakeUpstreamResponse(http.StatusNotFound, "text/plain", nil), nil
	}
	requestTile(t, cache, fetcher, "12", "1", "1")
	clock.advance(4 * time.Minute)
	requestTile(t, cache, fetcher, "12", "2", "1")
	if styles != 1 {
		t.Fatalf("style fetched %d times inside 5 minutes, want 1", styles)
	}
	clock.advance(2 * time.Minute)
	requestTile(t, cache, fetcher, "12", "3", "1")
	if styles != 2 {
		t.Errorf("style fetched %d times after 5 minutes, want 2", styles)
	}
}

// Item 4: templates are read from the cache once and then held in memory.
func TestCartoUpstream_TemplatesAreHeldInMemoryAndResetByClear(t *testing.T) {
	cache := newTestTileCache(t)
	seedCartoUpstream(t, cache)
	var urls []string
	fetcher := upstreamURLRecorder("application/octet-stream", "bytes", &urls)

	requestTile(t, cache, fetcher, "12", "1", "1")
	if _, err := cache.db.Exec(`DELETE FROM basemap_assets WHERE path LIKE ?`, basemapAssetPrefix+"upstream/%"); err != nil {
		t.Fatal(err)
	}
	if code, _ := requestTile(t, cache, fetcher, "12", "2", "1"); code != http.StatusOK {
		t.Fatalf("second tile: %d", code)
	}
	if len(urls) != 2 || !strings.Contains(urls[1], "/v/"+oldStamp+"/") {
		t.Errorf("second miss should use the in-memory templates with no style fetch: %v", urls)
	}

	if err := cache.clear(); err != nil {
		t.Fatal(err)
	}
	urls = nil
	requestTile(t, cache, fetcher, "12", "3", "1")
	if len(urls) == 0 || !strings.HasSuffix(urls[0], "positron-gl-style/style.json") {
		t.Errorf("after clear the style must be refetched, got %v", urls)
	}
}

// Item 5: a stamp change drops the sprite and glyph files of the old stamp
// but keeps the cached vector tiles.
func TestStampRotation_PurgesDerivedSpriteAndGlyphsButKeepsTiles(t *testing.T) {
	cache := newTestTileCache(t)
	seedCartoUpstream(t, cache)
	for _, p := range []string{"fonts/Open Sans Italic/0-255", "sprite/positron.json", "sprite/positron@2x.png", "sprite/dark-matter.json"} {
		if err := cache.putBasemapAsset(basemapAssetPrefix+p, []byte("old"), "application/json"); err != nil {
			t.Fatal(err)
		}
	}
	if err := cache.put(cartoBasemapSource, 12, 9, 9, []byte("kept"), "application/x-protobuf"); err != nil {
		t.Fatal(err)
	}
	_, fetcher := newStampedUpstream(t, newStamp)

	if code, _ := requestTile(t, cache, fetcher, "12", "1", "1"); code != http.StatusOK {
		t.Fatalf("tile: %d", code)
	}
	has := func(p string) bool {
		_, _, ok, err := cache.getBasemapAsset(basemapAssetPrefix + p)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if has("fonts/Open Sans Italic/0-255") || has("sprite/positron.json") || has("sprite/positron@2x.png") {
		t.Error("old-stamp glyph/sprite files survived the stamp change")
	}
	if !has("sprite/dark-matter.json") {
		t.Error("the other theme's sprite was purged although its style did not change")
	}
	if _, _, ok, _ := cache.get(cartoBasemapSource, 12, 9, 9); !ok {
		t.Error("cached vector tile was purged")
	}
}

// Item 6.
func TestRequestOrigin_ForwardedHostIsValidated(t *testing.T) {
	cases := []struct{ name, xfh, want string }{
		{"plain", "boat.example.net", "http://boat.example.net"},
		{"with port", "boat.example.net:8443", "http://boat.example.net:8443"},
		{"ipv4", "100.64.0.9:8080", "http://100.64.0.9:8080"},
		{"ipv6", "[fd7a::1]:8080", "http://[fd7a::1]:8080"},
		{"first of a list", "boat.example.net, evil.example", "http://boat.example.net"},
		{"list with bad first", "evil.example/x, boat.example.net", "http://example.com"},
		{"path", "evil.example/steal", "http://example.com"},
		{"userinfo", "user@evil.example", "http://example.com"},
		{"quote", `a"onload=1`, "http://example.com"},
		{"space", "a b", "http://example.com"},
		{"empty", "", "http://example.com"},
		{"bad port", "boat:99999999", "http://example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newBasemapTestContext("/", nil, nil)
			if tc.xfh != "" {
				c.Request().Header.Set("X-Forwarded-Host", tc.xfh)
			}
			if got := requestOrigin(c); got != tc.want {
				t.Errorf("origin = %q, want %q", got, tc.want)
			}
		})
	}
	c, _ := newBasemapTestContext("/", nil, nil)
	c.Request().Header.Set("X-Forwarded-Proto", "javascript")
	if got := requestOrigin(c); got != "http://example.com" {
		t.Errorf("origin with a bad proto = %q", got)
	}
	c, _ = newBasemapTestContext("/", nil, nil)
	c.Request().Header.Set("X-Forwarded-Proto", "https, http")
	if got := requestOrigin(c); got != "https://example.com" {
		t.Errorf("origin with a proto list = %q", got)
	}
}

func TestBasemapStyleHandler_ServedStyleIsPrivatelyCached(t *testing.T) {
	cache := newTestTileCache(t)
	seedCartoUpstream(t, cache)
	c, rec := newBasemapTestContext("/", []string{"name"}, []string{"positron"})
	if err := basemapStyleHandler(cache, failingFetcher())(c); err != nil {
		t.Fatal(err)
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=300" {
		t.Errorf("Cache-Control = %q: the served style embeds the request origin", got)
	}
}

// Item 7: only a genuine upstream 404/410 for the tile is a 404.
func TestBasemapVectorTileHandler_ErrorStatuses(t *testing.T) {
	statusFetcher := func(code int) *fakeTileFetcher {
		return &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
			if strings.HasSuffix(req.URL.Path, "/style.json") {
				return fakeUpstreamResponse(http.StatusOK, "application/json", []byte(cartoFixture(t, "positron"))), nil
			}
			return fakeUpstreamResponse(code, "text/plain", nil), nil
		}}
	}
	t.Run("genuine 404", func(t *testing.T) {
		cache := newTestTileCache(t)
		seedCartoUpstream(t, cache)
		if code, _ := requestTile(t, cache, statusFetcher(404), "12", "1", "1"); code != http.StatusNotFound {
			t.Errorf("%d", code)
		}
	})
	t.Run("410", func(t *testing.T) {
		cache := newTestTileCache(t)
		seedCartoUpstream(t, cache)
		if code, _ := requestTile(t, cache, statusFetcher(410), "12", "1", "1"); code != http.StatusNotFound {
			t.Errorf("%d", code)
		}
	})
	t.Run("upstream 500", func(t *testing.T) {
		cache := newTestTileCache(t)
		seedCartoUpstream(t, cache)
		if code, _ := requestTile(t, cache, statusFetcher(500), "12", "1", "1"); code != http.StatusBadGateway {
			t.Errorf("%d, want 502", code)
		}
	})
	t.Run("no style and none reachable", func(t *testing.T) {
		cache := newTestTileCache(t)
		if code, _ := requestTile(t, cache, failingFetcher(), "12", "1", "1"); code != http.StatusBadGateway {
			t.Errorf("%d, want 502", code)
		}
	})
	t.Run("unusable style", func(t *testing.T) {
		cache := newTestTileCache(t)
		f := respondingFetcher("style.json", "application/json", `{"version":8}`)
		if code, _ := requestTile(t, cache, f, "12", "1", "1"); code != http.StatusBadGateway {
			t.Errorf("%d, want 502", code)
		}
	})
	t.Run("refresh failure", func(t *testing.T) {
		cache := newTestTileCache(t)
		seedCartoUpstream(t, cache)
		f := &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
			if strings.HasSuffix(req.URL.Path, "/style.json") {
				return fakeUpstreamResponse(http.StatusInternalServerError, "text/plain", nil), nil
			}
			return fakeUpstreamResponse(http.StatusNotFound, "text/plain", nil), nil
		}}
		if code, _ := requestTile(t, cache, f, "12", "1", "1"); code != http.StatusBadGateway {
			t.Errorf("%d, want 502", code)
		}
	})
	t.Run("local database error", func(t *testing.T) {
		cache := newTestTileCache(t)
		seedCartoUpstream(t, cache)
		_ = cache.db.Close()
		if code, _ := requestTile(t, cache, statusFetcher(200), "12", "1", "1"); code != http.StatusInternalServerError {
			t.Errorf("%d, want 500", code)
		}
	})
}
