package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/labstack/echo/v4"
)

// testDistFS stands in for an embedded frontend build. A synthetic FS keeps
// these tests independent of whether backend/dist holds a real build: in CI it
// never does, so exercising registerStaticHandler directly would hit the embed
// guard, skip registration, and make the tests vacuous.
func testDistFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":          {Data: []byte(`<!doctype html><div id="root"></div>`)},
		"assets/index-abc.js": {Data: []byte(`console.log("app")`)},
	}
}

// TestStaticHandler_ServesDashboardAtRoot is the regression guard for the
// dashboard 404: the SPA is served from "/", and Echo's "/*" wildcard does not
// match the bare root path on its own.
func TestStaticHandler_ServesDashboardAtRoot(t *testing.T) {
	sessions := newTestSessionStore(t)

	e := echo.New()
	registerAPIRoutes(e, sessions, buildAPIRoutes(sessions, newWorldImageryHTTPClient()))
	registerStaticHandlerFS(e, testDistFS())

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `id="root"`) {
		t.Errorf("GET / did not serve index.html, got %q", rec.Body.String())
	}
}

// TestStaticHandler_DeepLinksServeAppShell covers client-side routes. These
// must return the shell directly; rewriting to "/index.html" made
// http.FileServer answer with a 301 to "./" instead.
func TestStaticHandler_DeepLinksServeAppShell(t *testing.T) {
	e := echo.New()
	registerStaticHandlerFS(e, testDistFS())

	for _, path := range []string{"/anchor", "/routes", "/settings/secrets"} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 (no redirect)", path, rec.Code)
			continue
		}
		if !strings.Contains(rec.Body.String(), `id="root"`) {
			t.Errorf("GET %s did not serve the app shell", path)
		}
	}
}

// TestStaticHandler_ServesRealAssets guards the fallback from swallowing real
// files: a hashed asset must be served as itself, not as the shell.
func TestStaticHandler_ServesRealAssets(t *testing.T) {
	e := echo.New()
	registerStaticHandlerFS(e, testDistFS())

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/index-abc.js", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /assets/index-abc.js = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `console.log("app")`) {
		t.Errorf("asset was not served verbatim, got %q", rec.Body.String())
	}
}

// TestStaticHandler_APIRoutesStillWin ensures the SPA catch-all does not shadow
// the API once both are registered on the same Echo instance.
func TestStaticHandler_APIRoutesStillWin(t *testing.T) {
	sessions := newTestSessionStore(t)

	e := echo.New()
	registerAPIRoutes(e, sessions, buildAPIRoutes(sessions, newWorldImageryHTTPClient()))
	registerStaticHandlerFS(e, testDistFS())

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/health = %d, want 200", rec.Code)
	}
}

// TestStaticHandler_ShellIsNoCache guards a kiosk that runs for days and
// reloads: a cached shell would keep naming /assets/* files a later deploy
// has already deleted, a blank screen with nothing in any log to say why.
func TestStaticHandler_ShellIsNoCache(t *testing.T) {
	e := echo.New()
	registerStaticHandlerFS(e, testDistFS())

	for _, path := range []string{"/", "/anchor"} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("GET %s Cache-Control = %q, want %q", path, cc, "no-cache")
		}
	}
}

// TestStaticHandler_HashedAssetsAreNotNoCache guards the other half: a real
// asset must keep whatever caching http.FileServer already gives it, not the
// shell's no-cache header.
func TestStaticHandler_HashedAssetsAreNotNoCache(t *testing.T) {
	e := echo.New()
	registerStaticHandlerFS(e, testDistFS())

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/index-abc.js", nil))

	if cc := rec.Header().Get("Cache-Control"); cc == "no-cache" {
		t.Errorf("GET /assets/index-abc.js Cache-Control = %q, want anything but no-cache", cc)
	}
}

// TestStaticHandler_HashedAssetsAreImmutable is the regression guard for the
// kiosk-reload finding: every /assets/* file already carries a content hash
// in its filename (Vite's build output), so a reload can never be served a
// stale one under that name - it's safe, and worth 1.5-3MB of avoided
// re-fetch per reload, to tell the browser to never revalidate it.
func TestStaticHandler_HashedAssetsAreImmutable(t *testing.T) {
	e := echo.New()
	registerStaticHandlerFS(e, testDistFS())

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/index-abc.js", nil))

	want := "public, max-age=31536000, immutable"
	if cc := rec.Header().Get("Cache-Control"); cc != want {
		t.Errorf("GET /assets/index-abc.js Cache-Control = %q, want %q", cc, want)
	}
}

// TestStaticHandler_NonHashedPublicFilesAreNotImmutable guards the other
// side: sw.js, the manifest and icons ship at a fixed, unhashed path (a
// service worker in particular MUST be revalidated - browsers already cap
// its own cache lifetime at 24h - so an immutable header on it would leave
// a kiosk running a stale worker indefinitely). None of these live under
// /assets/, so they must come back with no caching header added here at
// all - whatever http.FileServer would have set on its own, unmodified.
func TestStaticHandler_NonHashedPublicFilesAreNotImmutable(t *testing.T) {
	dist := fstest.MapFS{
		"index.html":           {Data: []byte(`<!doctype html><div id="root"></div>`)},
		"assets/index-abc.js":  {Data: []byte(`console.log("app")`)},
		"sw.js":                {Data: []byte(`self.addEventListener("install", () => {});`)},
		"manifest.webmanifest": {Data: []byte(`{"name":"Helmcentral"}`)},
		"icons/icon-192.png":   {Data: []byte("not a real png, just test bytes")},
	}

	e := echo.New()
	registerStaticHandlerFS(e, dist)

	for _, path := range []string{"/sw.js", "/manifest.webmanifest", "/icons/icon-192.png"} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if cc := rec.Header().Get("Cache-Control"); cc == "public, max-age=31536000, immutable" {
			t.Errorf("GET %s Cache-Control = %q, must not be the hashed-asset immutable header", path, cc)
		}
	}
}
