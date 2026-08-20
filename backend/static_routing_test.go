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
