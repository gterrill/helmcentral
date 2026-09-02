package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/labstack/echo/v4"
)

// resetRadarCapabilitiesCache clears the package-level capabilities cache
// before and after a test, so tests do not leak cached entries into each
// other through the shared map (mirrors withGlobalRadarTargetStore,
// radar_source_test.go).
func resetRadarCapabilitiesCache(t *testing.T) {
	t.Helper()
	clear := func() {
		radarCapabilitiesMu.Lock()
		radarCapabilitiesCache = map[string]radarCapabilitiesCacheEntry{}
		radarCapabilitiesMu.Unlock()
	}
	clear()
	t.Cleanup(clear)
}

// capabilitiesFixture loads the real captured payload
// (testdata/mayara/README.md: "Captured from the live rig, not
// hand-authored"), the same fixture-loading idiom radar_targets_test.go uses
// for targets-fur6424A.json.
func capabilitiesFixture(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/mayara/capabilities-fur6424A.json")
	if err != nil {
		t.Fatalf("reading capabilities fixture: %v", err)
	}
	return body
}

// stubMayaraCapabilitiesServer serves body for any capabilities request and
// counts how many requests it actually received, so the TTL cache test can
// assert the upstream was hit exactly once.
func stubMayaraCapabilitiesServer(t *testing.T, status int, body []byte) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// radarCapabilitiesRequest builds an echo.Context/ResponseRecorder pair for
// GET /api/radar/capabilities, mirroring TestRadarTargetsHandlerReturnsPayload
// (radar_source_test.go). radarID == "" omits the query parameter entirely.
func radarCapabilitiesRequest(radarID string) (echo.Context, *httptest.ResponseRecorder) {
	target := "/api/radar/capabilities"
	if radarID != "" {
		target += "?radar=" + radarID
	}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

// TestRadarCapabilitiesHandlerProxiesFixtureVerbatim asserts the handler
// relays mayara's real capabilities payload through unmodified: legend.pixels
// keeps all 254 entries (README.md: "254 entries, not 252") and
// spokesPerRevolution stays 8192, guarding against anything silently
// reshaping the payload on its way through.
func TestRadarCapabilitiesHandlerProxiesFixtureVerbatim(t *testing.T) {
	resetRadarCapabilitiesCache(t)
	fixture := capabilitiesFixture(t)

	srv, _ := stubMayaraCapabilitiesServer(t, http.StatusOK, fixture)
	t.Setenv("SETTINGS_FILE", settingsFileForServer(t, srv.URL))

	c, rec := radarCapabilitiesRequest("fur6424A")
	if err := radarCapabilitiesHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got, want map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding handler response: %v", err)
	}
	if err := json.Unmarshal(fixture, &want); err != nil {
		t.Fatalf("decoding fixture: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("response did not survive the proxy intact:\ngot  %+v\nwant %+v", got, want)
	}

	legend, ok := got["legend"].(map[string]any)
	if !ok {
		t.Fatalf("response missing legend object: %+v", got)
	}
	pixels, ok := legend["pixels"].([]any)
	if !ok {
		t.Fatalf("legend.pixels missing or not an array: %+v", legend)
	}
	if len(pixels) != 254 {
		t.Fatalf("legend.pixels has %d entries, want 254 (testdata/mayara/README.md)", len(pixels))
	}
	if spokes, _ := got["spokesPerRevolution"].(float64); spokes != 8192 {
		t.Fatalf("spokesPerRevolution = %v, want 8192", got["spokesPerRevolution"])
	}
}

// TestRadarCapabilitiesHandlerCachesWithinTTL asserts a second call for the
// same radar within the TTL is served from cache: capabilities change only
// on a range change (plan: "Backend" section), so re-fetching on every poll
// would hit mayara for no reason.
func TestRadarCapabilitiesHandlerCachesWithinTTL(t *testing.T) {
	resetRadarCapabilitiesCache(t)
	fixture := capabilitiesFixture(t)

	srv, hits := stubMayaraCapabilitiesServer(t, http.StatusOK, fixture)
	t.Setenv("SETTINGS_FILE", settingsFileForServer(t, srv.URL))

	firstCtx, firstRec := radarCapabilitiesRequest("fur6424A")
	if err := radarCapabilitiesHandler(firstCtx); err != nil {
		t.Fatalf("first call: handler returned error: %v", err)
	}
	if firstRec.Code != http.StatusOK {
		t.Fatalf("first call: status = %d", firstRec.Code)
	}

	secondCtx, secondRec := radarCapabilitiesRequest("fur6424A")
	if err := radarCapabilitiesHandler(secondCtx); err != nil {
		t.Fatalf("second call: handler returned error: %v", err)
	}
	if secondRec.Code != http.StatusOK {
		t.Fatalf("second call: status = %d", secondRec.Code)
	}

	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("upstream hit count = %d, want 1 (second call within the TTL must be served from cache)", got)
	}
	if firstRec.Body.String() != secondRec.Body.String() {
		t.Fatalf("cached response body diverged from the first response")
	}
}

// TestRadarCapabilitiesHandlerUpstreamNotFoundSurfacesError pins the fail-fast
// policy (AGENTS.md): a 404 from the plugin must surface as an explicit
// error, never an empty or partial legend standing in for a real one.
func TestRadarCapabilitiesHandlerUpstreamNotFoundSurfacesError(t *testing.T) {
	resetRadarCapabilitiesCache(t)

	srv, _ := stubMayaraCapabilitiesServer(t, http.StatusNotFound, []byte(`{"message":"not found"}`))
	t.Setenv("SETTINGS_FILE", settingsFileForServer(t, srv.URL))

	c, rec := radarCapabilitiesRequest("fur6424A")
	if err := radarCapabilitiesHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if rec.Code == http.StatusOK {
		t.Fatalf("expected a non-2xx status on upstream 404, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `"legend"`) {
		t.Fatalf("error body must not carry a legend of any kind, got %s", rec.Body.String())
	}
}

// TestRadarCapabilitiesHandlerMissingRadarParamIsBadRequest guards the
// required query parameter: with no radar id there is nothing to proxy.
func TestRadarCapabilitiesHandlerMissingRadarParamIsBadRequest(t *testing.T) {
	resetRadarCapabilitiesCache(t)

	c, rec := radarCapabilitiesRequest("")
	if err := radarCapabilitiesHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
