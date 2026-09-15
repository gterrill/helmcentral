package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// stubPlaceNameProvider is a fake placeNameProvider for tests, mirroring
// stubPOIProvider (poi_providers_test.go) plus the place-name pair. It sits
// directly in the shared POI registry (poiProviderRegistry) exactly like a
// real wasmPOIProvider would, so resolvePlaceNameProvider and the listing
// handler exercise the same registry-lookup-then-capability-check path they
// use in production.
type stubPlaceNameProvider struct {
	stubPOIProvider
	supports bool

	placeNameCalls   []int
	placeNameResults map[int]placeNameResult
	placeNameErr     error

	searchCalls  []placeSearchInput
	searchResult placeSearchResult
	searchErr    error
}

func (s *stubPlaceNameProvider) SupportsPlaceNames() bool { return s.supports }

func (s *stubPlaceNameProvider) PlaceNameAt(lat, lon float64, radiusM int) (placeNameResult, error) {
	s.placeNameCalls = append(s.placeNameCalls, radiusM)
	if s.placeNameErr != nil {
		return placeNameResult{}, s.placeNameErr
	}
	return s.placeNameResults[radiusM], nil
}

func (s *stubPlaceNameProvider) SearchPlaces(input placeSearchInput) (placeSearchResult, error) {
	s.searchCalls = append(s.searchCalls, input)
	if s.searchErr != nil {
		return placeSearchResult{}, s.searchErr
	}
	return s.searchResult, nil
}

// writePlaceNameProviderSettings writes a temp settings.yaml with
// ui.place_name_provider set to provider ("" writes no key at all, so the
// default-resolution path can be tested), mirroring writePOISettings
// (poi_providers_test.go).
func writePlaceNameProviderSettings(t *testing.T, provider string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.yaml")
	content := "signalk:\n  address: 127.0.0.1\n  port: 0\n"
	if provider != "" {
		content += fmt.Sprintf("ui:\n  place_name_provider: %s\n", provider)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test settings file: %v", err)
	}
	return path
}

func TestResolvePlaceNameProvider_BlankUsesDefaultID(t *testing.T) {
	withCleanPOIProviderRegistry(t)
	registerPOIProvider(&stubPlaceNameProvider{
		stubPOIProvider: stubPOIProvider{id: defaultPlaceNameProviderID, name: "OSM via Overpass"},
		supports:        true,
	})

	_, gotID, err := resolvePlaceNameProvider(writePlaceNameProviderSettings(t, ""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotID != defaultPlaceNameProviderID {
		t.Fatalf("got provider id %q, want default %q", gotID, defaultPlaceNameProviderID)
	}
}

func TestResolvePlaceNameProvider_ConfiguredSupportingPluginResolves(t *testing.T) {
	withCleanPOIProviderRegistry(t)
	registerPOIProvider(&stubPlaceNameProvider{stubPOIProvider: stubPOIProvider{id: "osm-overpass", name: "OSM"}, supports: true})
	registerPOIProvider(&stubPlaceNameProvider{stubPOIProvider: stubPOIProvider{id: "other-places", name: "Other"}, supports: true})

	provider, gotID, err := resolvePlaceNameProvider(writePlaceNameProviderSettings(t, "other-places"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotID != "other-places" {
		t.Fatalf("got provider id %q, want %q", gotID, "other-places")
	}
	if provider.ID() != "other-places" {
		t.Fatalf("resolved provider ID() = %q, want %q", provider.ID(), "other-places")
	}
}

// An unknown configured id must error naming it, never silently fall back
// to another registered provider (AGENTS.md's fail-fast / no-masking
// fallback policy) - mirrors resolvePOIProvider's own contract.
func TestResolvePlaceNameProvider_UnknownIDErrorsNamingIt(t *testing.T) {
	withCleanPOIProviderRegistry(t)
	registerPOIProvider(&stubPlaceNameProvider{stubPOIProvider: stubPOIProvider{id: "osm-overpass", name: "OSM"}, supports: true})

	_, _, err := resolvePlaceNameProvider(writePlaceNameProviderSettings(t, "no-such-plugin"))
	if err == nil {
		t.Fatalf("expected an error for an unknown configured provider")
	}
	if got := err.Error(); !strings.Contains(got, "no-such-plugin") {
		t.Fatalf("expected the error to name the unknown provider, got: %v", got)
	}
}

// A plugin that IS installed and configured, but does not export both
// place_name_at and search_places, must error naming it - never silently
// fall back to a different, supporting provider.
func TestResolvePlaceNameProvider_UnsupportedPluginErrorsNamingIt(t *testing.T) {
	withCleanPOIProviderRegistry(t)
	registerPOIProvider(&stubPlaceNameProvider{stubPOIProvider: stubPOIProvider{id: "google-places", name: "Google Places"}, supports: false})

	_, _, err := resolvePlaceNameProvider(writePlaceNameProviderSettings(t, "google-places"))
	if err == nil {
		t.Fatalf("expected an error for a provider that does not support place names")
	}
	if got := err.Error(); !strings.Contains(got, "google-places") {
		t.Fatalf("expected the error to name the unsupported provider, got: %v", got)
	}
}

// ── GET /api/place-name-providers ──────────────────────────────────────────

func callPlaceNameProvidersHandler(t *testing.T) []poiProviderInfo {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/place-name-providers", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if err := placeNameProvidersHandler(c); err != nil {
		t.Fatalf("placeNameProvidersHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var decoded []poiProviderInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return decoded
}

func TestPlaceNameProvidersHandler_ListsOnlySupportingPluginsInRegistrationOrder(t *testing.T) {
	withCleanPOIProviderRegistry(t)
	registerPOIProvider(&stubPlaceNameProvider{stubPOIProvider: stubPOIProvider{id: "google-places", name: "Google Places"}, supports: false})
	registerPOIProvider(&stubPlaceNameProvider{stubPOIProvider: stubPOIProvider{id: "osm-overpass", name: "OSM via Overpass"}, supports: true})
	registerPOIProvider(&stubPlaceNameProvider{stubPOIProvider: stubPOIProvider{id: "another-osm", name: "Another OSM Mirror"}, supports: true})

	got := callPlaceNameProvidersHandler(t)
	if len(got) != 2 {
		t.Fatalf("expected 2 supporting providers listed, got %d: %+v", len(got), got)
	}
	if got[0].ID != "osm-overpass" || got[1].ID != "another-osm" {
		t.Fatalf("expected registration order [osm-overpass, another-osm], got %+v", got)
	}
	for _, p := range got {
		if p.ID == "google-places" {
			t.Fatalf("expected google-places (does not support place names) to be excluded, got %+v", got)
		}
	}
}

func TestPlaceNameProvidersHandler_EmptyRegistryReturnsEmptyArray(t *testing.T) {
	withCleanPOIProviderRegistry(t)

	got := callPlaceNameProvidersHandler(t)
	if len(got) != 0 {
		t.Fatalf("expected an empty list, got %+v", got)
	}
}
