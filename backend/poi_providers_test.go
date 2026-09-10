package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"
)

// withCleanPOIProviderRegistry saves the global POI provider registry,
// resets it to empty for the duration of the test, and restores the
// original afterwards - mirrors withCleanWaveProviderRegistry.
func withCleanPOIProviderRegistry(t *testing.T) {
	t.Helper()
	origRegistry := poiProviderRegistry
	origOrder := poiProviderOrder
	poiProviderRegistry = map[string]poiProvider{}
	poiProviderOrder = nil
	t.Cleanup(func() {
		poiProviderRegistry = origRegistry
		poiProviderOrder = origOrder
	})
}

type stubPOIProvider struct {
	id     string
	name   string
	ttl    int64
	result poiFetchResult
	err    error
}

func (s *stubPOIProvider) ID() string          { return s.id }
func (s *stubPOIProvider) Name() string        { return s.name }
func (s *stubPOIProvider) Description() string { return "Stub POI provider for tests" }
func (s *stubPOIProvider) TTLSeconds() int64   { return s.ttl }
func (s *stubPOIProvider) FetchPOI(lat, lon float64, radiusM int, categories []string, limit int) (poiFetchResult, error) {
	if s.err != nil {
		return poiFetchResult{}, s.err
	}
	return s.result, nil
}

func writePOISettings(t *testing.T, provider, signalkURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.yaml")
	content := fmt.Sprintf("signalk:\n  address: %s\n  port: 0\nui:\n  poi_provider: %s\n", signalkURL, provider)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test settings file: %v", err)
	}
	return path
}

// ── registry ────────────────────────────────────────────────────────────────

func TestRegisterPOIProvider_PreservesRegistrationOrder(t *testing.T) {
	withCleanPOIProviderRegistry(t)

	registerPOIProvider(&stubPOIProvider{id: "b", name: "B"})
	registerPOIProvider(&stubPOIProvider{id: "a", name: "A"})

	if len(poiProviderOrder) != 2 || poiProviderOrder[0] != "b" || poiProviderOrder[1] != "a" {
		t.Fatalf("expected order [b a], got %v", poiProviderOrder)
	}

	if _, ok := getPOIProvider("a"); !ok {
		t.Fatalf("expected provider a to be registered")
	}
	if _, ok := getPOIProvider("missing"); ok {
		t.Fatalf("expected provider 'missing' to not be registered")
	}
}

func TestPOIProvidersHandler_ReturnsRegisteredProvidersInOrder(t *testing.T) {
	withCleanPOIProviderRegistry(t)
	registerPOIProvider(&stubPOIProvider{id: "osm-overpass", name: "OpenStreetMap (Overpass)"})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/poi-providers", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := poiProvidersHandler(c); err != nil {
		t.Fatalf("poiProvidersHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload []poiProviderInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(payload) != 1 || payload[0].ID != "osm-overpass" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestResolvePOIProvider_UnknownProviderIsActionableError(t *testing.T) {
	withCleanPOIProviderRegistry(t)
	settingsPath := writePOISettings(t, "not-a-real-provider", "http://127.0.0.1:1")

	_, id, err := resolvePOIProvider(settingsPath)
	if id != "not-a-real-provider" {
		t.Fatalf("expected configured id to be echoed back, got %q", id)
	}
	if err == nil {
		t.Fatalf("expected an error for an unregistered provider")
	}
	if !containsSubstring(err.Error(), "plugins/poi") {
		t.Fatalf("expected the error to name the plugin directory, got: %v", err)
	}
}

func TestResolvePOIProvider_DefaultsToOSMOverpassWhenUnset(t *testing.T) {
	withCleanPOIProviderRegistry(t)
	registerPOIProvider(&stubPOIProvider{id: defaultPOIProviderID, name: "OSM"})
	settingsPath := writePOISettings(t, "", "http://127.0.0.1:1")

	provider, id, err := resolvePOIProvider(settingsPath)
	if err != nil {
		t.Fatalf("resolvePOIProvider returned error: %v", err)
	}
	if id != defaultPOIProviderID {
		t.Fatalf("expected default id %q, got %q", defaultPOIProviderID, id)
	}
	if provider.ID() != defaultPOIProviderID {
		t.Fatalf("expected resolved provider %q, got %q", defaultPOIProviderID, provider.ID())
	}
}

func containsSubstring(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

// ── handler: param validation ────────────────────────────────────────────────

func TestPOINearby_ReturnsBadGatewayForUnknownProvider(t *testing.T) {
	withCleanPOIProviderRegistry(t)
	settingsPath := writePOISettings(t, "not-a-real-provider", "http://127.0.0.1:1")
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/poi?radius_nm=5&categories=anchorage", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := poiNearby(c); err != nil {
		t.Fatalf("poiNearby returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestPOINearby_RejectsMissingRadius(t *testing.T) {
	withCleanPOIProviderRegistry(t)
	registerPOIProvider(&stubPOIProvider{id: "osm-overpass", name: "OSM", ttl: 21600})
	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()
	t.Setenv("SETTINGS_FILE", writePOISettings(t, "osm-overpass", server.URL))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/poi?categories=anchorage", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := poiNearby(c); err != nil {
		t.Fatalf("poiNearby returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing radius_nm, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestPOINearby_RejectsOutOfRangeRadius(t *testing.T) {
	withCleanPOIProviderRegistry(t)
	registerPOIProvider(&stubPOIProvider{id: "osm-overpass", name: "OSM", ttl: 21600})
	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()
	t.Setenv("SETTINGS_FILE", writePOISettings(t, "osm-overpass", server.URL))

	for _, radius := range []string{"0.1", "30"} {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/poi?radius_nm="+radius+"&categories=anchorage", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		if err := poiNearby(c); err != nil {
			t.Fatalf("poiNearby returned error: %v", err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("radius_nm=%s: expected 400, got %d (body: %s)", radius, rec.Code, rec.Body.String())
		}
	}
}

func TestPOINearby_RejectsMissingOrUnknownCategories(t *testing.T) {
	withCleanPOIProviderRegistry(t)
	registerPOIProvider(&stubPOIProvider{id: "osm-overpass", name: "OSM", ttl: 21600})
	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()
	t.Setenv("SETTINGS_FILE", writePOISettings(t, "osm-overpass", server.URL))

	for _, categories := range []string{"", "not-a-real-category"} {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/poi?radius_nm=5&categories="+categories, nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		if err := poiNearby(c); err != nil {
			t.Fatalf("poiNearby returned error: %v", err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("categories=%q: expected 400, got %d (body: %s)", categories, rec.Code, rec.Body.String())
		}
	}
}

func TestPOINearby_RejectsOutOfRangeLimit(t *testing.T) {
	withCleanPOIProviderRegistry(t)
	registerPOIProvider(&stubPOIProvider{id: "osm-overpass", name: "OSM", ttl: 21600})
	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()
	t.Setenv("SETTINGS_FILE", writePOISettings(t, "osm-overpass", server.URL))

	for _, limit := range []string{"0", "101", "not-a-number"} {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/poi?radius_nm=5&categories=anchorage&limit="+limit, nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		if err := poiNearby(c); err != nil {
			t.Fatalf("poiNearby returned error: %v", err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("limit=%s: expected 400, got %d (body: %s)", limit, rec.Code, rec.Body.String())
		}
	}
}

func TestPOINearby_RejectsUnusablePosition(t *testing.T) {
	withCleanPOIProviderRegistry(t)
	registerPOIProvider(&stubPOIProvider{id: "osm-overpass", name: "OSM", ttl: 21600})
	server := trustedSignalKPayloadServer(t, -1, -1)
	defer server.Close()
	t.Setenv("SETTINGS_FILE", writePOISettings(t, "osm-overpass", server.URL))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/poi?radius_nm=5&categories=anchorage", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := poiNearby(c); err != nil {
		t.Fatalf("poiNearby returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for the -1,-1 sentinel position, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestPOINearby_ReturnsBadGatewayWhenProviderFetchFails(t *testing.T) {
	withCleanPOIProviderRegistry(t)
	registerPOIProvider(&stubPOIProvider{id: "osm-overpass", name: "OSM", ttl: 21600, err: fmt.Errorf("simulated overpass rate limit")})
	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()
	t.Setenv("SETTINGS_FILE", writePOISettings(t, "osm-overpass", server.URL))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/poi?radius_nm=5&categories=anchorage", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := poiNearby(c); err != nil {
		t.Fatalf("poiNearby returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on provider fetch error, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

// ── handler: host-side derivation ────────────────────────────────────────────

// A vessel at (-27.4, 153.0). Features are placed at known offsets so
// distance/bearing/radius-filtering/dedupe/sort/limit can all be asserted
// against values worked out by hand rather than re-deriving them from the
// same haversine/bearing code under test.
func TestPOINearby_ComputesDistanceBearingFiltersSortsAndLimits(t *testing.T) {
	withCleanPOIProviderRegistry(t)

	vesselLat, vesselLon := -27.4, 153.0

	// Due east, roughly 1852m (1nm) away - inside a 5nm radius.
	nearLat, nearLon := destinationPoint(vesselLat, vesselLon, math.Pi/2, 1852)
	// Due north, roughly 3704m (2nm) away - inside a 5nm radius, farther than "near".
	farLat, farLon := destinationPoint(vesselLat, vesselLon, 0, 3704)
	// Due south, roughly 20000m away - outside a 5nm (~9260m) radius, must be dropped.
	outsideLat, outsideLon := destinationPoint(vesselLat, vesselLon, math.Pi, 20000)

	result := poiFetchResult{
		Features: []poiFeatureRaw{
			{ID: "far", Category: "anchorage", Name: "Far Anchorage", Lat: farLat, Lon: farLon},
			{ID: "near", Category: "anchorage", Name: "Near Anchorage", Lat: nearLat, Lon: nearLon},
			{ID: "outside", Category: "anchorage", Name: "Outside Radius", Lat: outsideLat, Lon: outsideLon},
			// A near-exact duplicate of "near" (same name, position within 5dp) must be dropped.
			{ID: "near-dupe", Category: "anchorage", Name: "Near Anchorage", Lat: nearLat, Lon: nearLon},
		},
		Truncated:   []string{"mooring"},
		Unsupported: []string{"dive"},
	}
	registerPOIProvider(&stubPOIProvider{id: "osm-overpass", name: "OSM", ttl: 21600, result: result})

	server := trustedSignalKPayloadServer(t, vesselLat, vesselLon)
	defer server.Close()
	t.Setenv("SETTINGS_FILE", writePOISettings(t, "osm-overpass", server.URL))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/poi?radius_nm=5&categories=anchorage&limit=50", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := poiNearby(c); err != nil {
		t.Fatalf("poiNearby returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var payload poiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(payload.Features) != 2 {
		t.Fatalf("expected 2 features after radius-filtering and dedupe, got %d: %+v", len(payload.Features), payload.Features)
	}
	if payload.Features[0].ID != "near" || payload.Features[1].ID != "far" {
		t.Fatalf("expected sort order [near far], got [%s %s]", payload.Features[0].ID, payload.Features[1].ID)
	}
	if math.Abs(payload.Features[0].DistanceM-1852) > 5 {
		t.Fatalf("expected near feature distance ~1852m, got %v", payload.Features[0].DistanceM)
	}
	if math.Abs(payload.Features[0].BearingDeg-90) > 1 {
		t.Fatalf("expected near feature bearing ~90deg (east), got %v", payload.Features[0].BearingDeg)
	}
	if math.Abs(payload.Features[1].BearingDeg-0) > 1 {
		t.Fatalf("expected far feature bearing ~0deg (north), got %v", payload.Features[1].BearingDeg)
	}
	if len(payload.Truncated) != 1 || payload.Truncated[0] != "mooring" {
		t.Fatalf("expected truncated=[mooring] to pass through, got %v", payload.Truncated)
	}
	if len(payload.Unsupported) != 1 || payload.Unsupported[0] != "dive" {
		t.Fatalf("expected unsupported=[dive] to pass through, got %v", payload.Unsupported)
	}
	if payload.Provider != "osm-overpass" {
		t.Fatalf("expected provider osm-overpass, got %q", payload.Provider)
	}
}

func TestPOINearby_LimitCapsFeatureCountAfterSort(t *testing.T) {
	withCleanPOIProviderRegistry(t)
	vesselLat, vesselLon := -27.4, 153.0

	features := make([]poiFeatureRaw, 0, 5)
	for i := 0; i < 5; i++ {
		lat, lon := destinationPoint(vesselLat, vesselLon, math.Pi/2, float64(1000+i*100))
		features = append(features, poiFeatureRaw{
			ID: fmt.Sprintf("f%d", i), Category: "anchorage", Name: fmt.Sprintf("Anchorage %d", i), Lat: lat, Lon: lon,
		})
	}
	registerPOIProvider(&stubPOIProvider{id: "osm-overpass", name: "OSM", ttl: 21600, result: poiFetchResult{Features: features}})

	server := trustedSignalKPayloadServer(t, vesselLat, vesselLon)
	defer server.Close()
	t.Setenv("SETTINGS_FILE", writePOISettings(t, "osm-overpass", server.URL))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/poi?radius_nm=5&categories=anchorage&limit=2", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := poiNearby(c); err != nil {
		t.Fatalf("poiNearby returned error: %v", err)
	}

	var payload poiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(payload.Features) != 2 {
		t.Fatalf("expected limit=2 to cap the result at 2 features, got %d", len(payload.Features))
	}
	if payload.Features[0].ID != "f0" || payload.Features[1].ID != "f1" {
		t.Fatalf("expected the two nearest features [f0 f1], got [%s %s]", payload.Features[0].ID, payload.Features[1].ID)
	}
}

// ── bearingDeg ────────────────────────────────────────────────────────────

func TestBearingDeg_CardinalDirections(t *testing.T) {
	lat, lon := -27.4, 153.0
	for _, tc := range []struct {
		name       string
		bearingRad float64
		wantDeg    float64
	}{
		{"north", 0, 0},
		{"east", math.Pi / 2, 90},
		{"south", math.Pi, 180},
		{"west", 3 * math.Pi / 2, 270},
	} {
		t.Run(tc.name, func(t *testing.T) {
			destLat, destLon := destinationPoint(lat, lon, tc.bearingRad, 5000)
			got := bearingDeg(lat, lon, destLat, destLon)
			diff := math.Abs(got - tc.wantDeg)
			if diff > 180 {
				diff = 360 - diff
			}
			if diff > 1 {
				t.Fatalf("expected bearing ~%.0f, got %.2f", tc.wantDeg, got)
			}
		})
	}
}
