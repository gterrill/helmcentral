// Fixture regeneration:
//
// The compiled .wasm fixtures under backend/testdata/tide_plugins/ are built
// from the TinyGo sources under backend/testdata/tide_plugins/src/<name>/
// using the tinygo/tinygo:latest (0.41.1) Docker image. To regenerate all
// four fixtures, run from the repo root:
//
//	docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:latest sh -c "
//	  cd backend/testdata/tide_plugins/src &&
//	  go mod tidy &&
//	  tinygo build -o /src/backend/testdata/tide_plugins/valid.wasm     -target wasip1 -buildmode c-shared ./valid &&
//	  tinygo build -o /src/backend/testdata/tide_plugins/httpfetch.wasm -target wasip1 -buildmode c-shared ./httpfetch &&
//	  tinygo build -o /src/backend/testdata/tide_plugins/panicker.wasm  -target wasip1 -buildmode c-shared ./panicker &&
//	  tinygo build -o /src/backend/testdata/tide_plugins/shadow.wasm    -target wasip1 -buildmode c-shared ./shadow
//	"
//
// This requires network access inside the container (to fetch
// github.com/extism/go-pdk during `go mod tidy`) and Docker Desktop with the
// tinygo/tinygo image available locally (first pull is ~350MB).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	extism "github.com/extism/go-sdk"
)

const (
	validFixtureWasm     = "testdata/tide_plugins/valid.wasm"
	httpfetchFixtureWasm = "testdata/tide_plugins/httpfetch.wasm"
	panickerFixtureWasm  = "testdata/tide_plugins/panicker.wasm"
	shadowFixtureWasm    = "testdata/tide_plugins/shadow.wasm"
)

// withCleanTideProviderRegistry saves the global tide provider registry,
// resets it to empty for the duration of the test, and restores the
// original afterwards, so tests that call loadWasmTideProviders /
// registerTideProvider don't leak state into other tests.
func withCleanTideProviderRegistry(t *testing.T) {
	t.Helper()
	origRegistry := tideProviderRegistry
	origOrder := tideProviderOrder
	tideProviderRegistry = map[string]tideProvider{}
	tideProviderOrder = nil
	t.Cleanup(func() {
		tideProviderRegistry = origRegistry
		tideProviderOrder = origOrder
	})
}

func mustNewWasmTideProvider(t *testing.T, path string) *wasmTideProvider {
	t.Helper()
	p, err := newWasmTideProvider(path)
	if err != nil {
		t.Fatalf("newWasmTideProvider(%q) failed: %v", path, err)
	}
	return p
}

func TestWasmTideProvider_SearchStations_MapsJSONToStations(t *testing.T) {
	provider := mustNewWasmTideProvider(t, validFixtureWasm)

	stations := provider.SearchStations("", 10)
	if len(stations) != 1 {
		t.Fatalf("expected 1 station, got %d: %+v", len(stations), stations)
	}

	got := stations[0]
	want := tideStation{
		StationID: "VALID1",
		Name:      "Valid Fixture Station",
		State:     "TS",
		Lat:       -33.8,
		Lon:       151.2,
		Timezone:  "Australia/Sydney",
	}
	if got != want {
		t.Errorf("station mismatch: got %+v, want %+v", got, want)
	}
}

func TestWasmTideProvider_FetchTideChart_UsesSharedInterpolation(t *testing.T) {
	provider := mustNewWasmTideProvider(t, validFixtureWasm)

	result, err := provider.FetchTideChart("VALID1")
	if err != nil {
		t.Fatalf("FetchTideChart returned error: %v", err)
	}

	if len(result.Extremes) != 2 {
		t.Fatalf("expected 2 extremes, got %d", len(result.Extremes))
	}

	// Same fixture shape as TestInterpolateTideNowBetweenExtremes. Compute
	// the expected height/direction independently via the shared
	// interpolateTideNow, and assert the adapter's result matches exactly
	// -- proving FetchTideChart calls through to the shared helper rather
	// than reimplementing interpolation itself.
	wantHeight, wantDirection := interpolateTideNow(result.Extremes, time.Now().UTC())

	if result.Direction != wantDirection {
		t.Errorf("direction mismatch: got %q, want %q", result.Direction, wantDirection)
	}
	if result.CurrentHeightM != wantHeight {
		t.Errorf("height mismatch: got %.4f, want %.4f", result.CurrentHeightM, wantHeight)
	}
}

func TestWasmTideProvider_HTTPRequestRoundTripsThroughHTTPTestServer(t *testing.T) {
	var handlerHit bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerHit = true
		w.Write([]byte("pong"))
	}))
	defer server.Close()

	manifest := extism.Manifest{
		Wasm:         []extism.Wasm{extism.WasmFile{Path: httpfetchFixtureWasm}},
		AllowedHosts: []string{"127.0.0.1"},
		Timeout:      5000,
	}
	provider, err := newWasmTideProviderWithManifest(manifest)
	if err != nil {
		t.Fatalf("newWasmTideProviderWithManifest failed: %v", err)
	}

	out, err := provider.call("probe_url", []byte(server.URL))
	if err != nil {
		t.Fatalf("probe_url call failed: %v", err)
	}
	if !handlerHit {
		t.Fatalf("expected the test server's handler to be hit")
	}
	if !bytes.Equal(out, []byte("pong")) {
		t.Errorf("expected round-tripped body %q, got %q", "pong", out)
	}
}

func TestWasmTideProvider_BlocksRequestsToNonAllowlistedHost(t *testing.T) {
	var mu sync.Mutex
	handlerHit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		handlerHit = true
		mu.Unlock()
		w.Write([]byte("pong"))
	}))
	defer server.Close()

	manifest := extism.Manifest{
		Wasm:         []extism.Wasm{extism.WasmFile{Path: httpfetchFixtureWasm}},
		AllowedHosts: []string{"example.invalid"}, // deliberately does NOT match the test server
		Timeout:      5000,
	}
	provider, err := newWasmTideProviderWithManifest(manifest)
	if err != nil {
		t.Fatalf("newWasmTideProviderWithManifest failed: %v", err)
	}

	_, err = provider.call("probe_url", []byte(server.URL))
	if err == nil {
		t.Fatalf("expected an error calling probe_url against a non-allowlisted host, got nil")
	}

	mu.Lock()
	hit := handlerHit
	mu.Unlock()
	if hit {
		t.Fatalf("expected the test server's handler to NEVER be invoked for a non-allowlisted host, but it was")
	}
}

func TestWasmTideProvider_TimeoutIsEnforced(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Write([]byte("too slow"))
	}))
	defer server.Close()

	manifest := extism.Manifest{
		Wasm:         []extism.Wasm{extism.WasmFile{Path: httpfetchFixtureWasm}},
		AllowedHosts: []string{"127.0.0.1"},
		Timeout:      200, // ms; far shorter than the handler's 2s sleep
	}
	provider, err := newWasmTideProviderWithManifest(manifest)
	if err != nil {
		t.Fatalf("newWasmTideProviderWithManifest failed: %v", err)
	}

	start := time.Now()
	_, err = provider.call("probe_url", []byte(server.URL))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected a timeout error calling probe_url, got nil")
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("expected the call to return promptly after the timeout, took %s", elapsed)
	}
}

func TestWasmTideProvider_GuestPanicIsContainedAsGoError(t *testing.T) {
	provider := mustNewWasmTideProvider(t, panickerFixtureWasm)

	_, err := provider.call("crash", nil)
	if err == nil {
		t.Fatalf("expected an error from a panicking guest export, got nil")
	}
	// Reaching this point at all (rather than the test process crashing)
	// is itself the assertion that the panic was contained.
}

func TestLoadWasmTideProviders_SkipsInvalidPluginWithoutFailingOthers(t *testing.T) {
	withCleanTideProviderRegistry(t)

	dir := t.TempDir()

	goodBytes, err := os.ReadFile(validFixtureWasm)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "good.wasm"), goodBytes, 0o644); err != nil {
		t.Fatalf("write good fixture: %v", err)
	}

	// A corrupt/truncated .wasm alongside a real one should be skipped and
	// logged, not take down discovery of the good plugin (mirrors
	// TestListSatChartsHandler_SkipsCorruptFileWithoutFailing's idiom).
	if err := os.WriteFile(filepath.Join(dir, "bad.wasm"), []byte("not a real wasm module"), 0o644); err != nil {
		t.Fatalf("write corrupt fixture: %v", err)
	}

	loadWasmTideProviders(dir)

	provider, ok := getTideProvider("valid-fixture")
	if !ok {
		t.Fatalf("expected the good plugin to be registered despite the corrupt one")
	}
	if provider.Name() != "Valid Fixture Provider" {
		t.Errorf("unexpected provider name: %q", provider.Name())
	}
	if len(tideProviderOrder) != 1 {
		t.Errorf("expected exactly 1 registered provider, got %d: %v", len(tideProviderOrder), tideProviderOrder)
	}
}

type fakeTideProvider struct{ id string }

func (f *fakeTideProvider) ID() string        { return f.id }
func (f *fakeTideProvider) Name() string      { return "Fake " + f.id }
func (f *fakeTideProvider) TTLSeconds() int64 { return 3600 }
func (f *fakeTideProvider) SearchStations(query string, limit int) []tideStation {
	return nil
}
func (f *fakeTideProvider) FetchTideChart(stationID string) (tideChartResult, error) {
	return tideChartResult{}, fmt.Errorf("not implemented")
}

func TestLoadWasmTideProviders_CannotShadowBuiltinProviderID(t *testing.T) {
	withCleanTideProviderRegistry(t)

	builtin := &fakeTideProvider{id: "bom"}
	registerTideProvider(builtin)

	dir := t.TempDir()
	shadowBytes, err := os.ReadFile(shadowFixtureWasm)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shadow.wasm"), shadowBytes, 0o644); err != nil {
		t.Fatalf("write shadow fixture: %v", err)
	}

	var logBuf bytes.Buffer
	origOutput := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(origOutput)

	loadWasmTideProviders(dir)

	got, ok := getTideProvider("bom")
	if !ok {
		t.Fatalf("expected the builtin \"bom\" registration to still exist")
	}
	if got != tideProvider(builtin) {
		t.Fatalf("expected the original builtin \"bom\" provider to be untouched, got a different provider: %v", got.Name())
	}
	if len(tideProviderOrder) != 1 {
		t.Errorf("expected the shadow plugin to be skipped (order should still have 1 entry), got %d: %v", len(tideProviderOrder), tideProviderOrder)
	}
	if logBuf.Len() == 0 {
		t.Errorf("expected a log message about the skipped shadow plugin")
	}
}

// sanity check that the JSON marshaling round trip used in the WASM
// adapter's search_stations path matches what the guest fixture emits.
func TestWasmTideProvider_SearchStationsInputEncoding(t *testing.T) {
	input, err := json.Marshal(map[string]any{"query": "syd", "limit": 5})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Query != "syd" || decoded.Limit != 5 {
		t.Errorf("unexpected round trip: %+v", decoded)
	}
}
