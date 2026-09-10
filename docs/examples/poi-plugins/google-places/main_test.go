package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("failed to load fixture %s: %v", name, err)
	}
	return data
}

// ── category mapping ─────────────────────────────────────────────────────

func TestMapCategoriesToIncludedTypes_SupportedCategories(t *testing.T) {
	includedTypes, unsupported := mapCategoriesToIncludedTypes([]string{"marina", "historic", "viewpoint", "trail"})

	want := map[string]bool{"marina": true, "historical_landmark": true, "tourist_attraction": true, "scenic_spot": true, "hiking_area": true, "park": true}
	if len(includedTypes) != len(want) {
		t.Fatalf("expected %d included types, got %d: %v", len(want), len(includedTypes), includedTypes)
	}
	for _, ty := range includedTypes {
		if !want[ty] {
			t.Fatalf("unexpected included type %q", ty)
		}
	}
	if len(unsupported) != 0 {
		t.Fatalf("expected no unsupported categories, got %v", unsupported)
	}
}

func TestMapCategoriesToIncludedTypes_ReportsUnsupportedCategories(t *testing.T) {
	includedTypes, unsupported := mapCategoriesToIncludedTypes([]string{"anchorage", "bay", "island", "fuel", "ramp", "mooring", "dive", "marina"})

	if len(includedTypes) != 1 || includedTypes[0] != "marina" {
		t.Fatalf("expected only marina to be supported, got %v", includedTypes)
	}
	wantUnsupported := []string{"anchorage", "bay", "island", "fuel", "ramp", "mooring", "dive"}
	if len(unsupported) != len(wantUnsupported) {
		t.Fatalf("expected %d unsupported categories, got %d: %v", len(wantUnsupported), len(unsupported), unsupported)
	}
}

func TestMapCategoriesToIncludedTypes_DedupesSharedTypesAcrossCategories(t *testing.T) {
	// historic alone already contributes both historical_landmark and
	// tourist_attraction - requesting it twice-over via aliasing categories
	// isn't possible in this API, but the same type appearing from more
	// than one category's list (a hypothetical future addition) must not
	// duplicate in includedTypes. This is exercised directly since the
	// current table has no such overlap yet.
	includedTypes, _ := mapCategoriesToIncludedTypes([]string{"historic"})
	seen := make(map[string]bool)
	for _, ty := range includedTypes {
		if seen[ty] {
			t.Fatalf("expected no duplicate types, got %v", includedTypes)
		}
		seen[ty] = true
	}
}

func TestMapCategoriesToIncludedTypes_UnknownCategoryIsUnsupported(t *testing.T) {
	// Defensive: a category id this table has never heard of (e.g. drift
	// against a future poiCategoryIDs addition) reports unsupported rather
	// than panicking.
	_, unsupported := mapCategoriesToIncludedTypes([]string{"not-a-real-category"})
	if len(unsupported) != 1 || unsupported[0] != "not-a-real-category" {
		t.Fatalf("expected the unknown category reported as unsupported, got %v", unsupported)
	}
}

// ── classification ───────────────────────────────────────────────────────

func TestClassifyGooglePlace_MapsBackToRequestedCategory(t *testing.T) {
	got, ok := classifyGooglePlace([]string{"marina", "trail"}, []string{"marina", "point_of_interest"})
	if !ok || got != "marina" {
		t.Fatalf("expected marina, got %q (ok=%v)", got, ok)
	}
}

func TestClassifyGooglePlace_FixedPrecedenceWhenTypesOverlapTwoRequestedCategories(t *testing.T) {
	// A place carrying both a marina type and a historic type: marina
	// precedes historic in poiCategoryOrder, so it wins.
	got, ok := classifyGooglePlace([]string{"historic", "marina"}, []string{"marina", "historical_landmark"})
	if !ok || got != "marina" {
		t.Fatalf("expected marina to win by fixed precedence, got %q (ok=%v)", got, ok)
	}
}

func TestClassifyGooglePlace_NoMatchWhenPlaceTypesDoNotIntersectRequested(t *testing.T) {
	if _, ok := classifyGooglePlace([]string{"marina"}, []string{"restaurant", "point_of_interest"}); ok {
		t.Fatalf("expected no match for unrelated place types")
	}
}

// ── request building ─────────────────────────────────────────────────────

func TestBuildSearchNearbyRequestBody_CarriesPositionRadiusAndTypes(t *testing.T) {
	body, err := buildSearchNearbyRequestBody(-20.4467, 149.0353, 9260, []string{"marina", "hiking_area"}, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("failed to parse built request body: %v", err)
	}

	includedTypes, _ := got["includedTypes"].([]any)
	if len(includedTypes) != 2 {
		t.Fatalf("expected 2 included types, got %v", got["includedTypes"])
	}

	circle := got["locationRestriction"].(map[string]any)["circle"].(map[string]any)
	center := circle["center"].(map[string]any)
	if center["latitude"] != -20.4467 || center["longitude"] != 149.0353 {
		t.Fatalf("expected center to carry the requested position, got %v", center)
	}
	if circle["radius"] != float64(9260) {
		t.Fatalf("expected radius 9260, got %v", circle["radius"])
	}
}

func TestBuildSearchNearbyRequestBody_ClampsMaxResultCountToGoogleLimit(t *testing.T) {
	body, err := buildSearchNearbyRequestBody(0, 0, 1000, []string{"marina"}, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if got["maxResultCount"] != float64(googleMaxResultCount) {
		t.Fatalf("expected maxResultCount clamped to %d, got %v", googleMaxResultCount, got["maxResultCount"])
	}
}

func TestBuildSearchNearbyRequestBody_ClampsRadiusToGoogleLimit(t *testing.T) {
	body, err := buildSearchNearbyRequestBody(0, 0, 999999, []string{"marina"}, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	circle := got["locationRestriction"].(map[string]any)["circle"].(map[string]any)
	if circle["radius"] != float64(googleMaxRadiusMeters) {
		t.Fatalf("expected radius clamped to %d, got %v", googleMaxRadiusMeters, circle["radius"])
	}
}

// ── response parsing (hand-written fixture; see testdata file header) ────

func TestParseSearchNearbyResponse_MapsFixtureFieldsCorrectly(t *testing.T) {
	body := loadFixture(t, "places_searchnearby_response.json")

	features, err := parseSearchNearbyResponse(body, []string{"marina", "historic", "trail"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(features) != 3 {
		t.Fatalf("expected 3 features, got %d: %+v", len(features), features)
	}

	marina := features[0]
	if marina.Category != "marina" || marina.Name != "Coral Sea Marina" {
		t.Errorf("unexpected marina feature: %+v", marina)
	}
	if marina.Detail != "" {
		t.Errorf("expected no detail for a place with no editorialSummary, got %q", marina.Detail)
	}

	landmark := features[1]
	if landmark.Category != "historic" || landmark.Detail == "" {
		t.Errorf("unexpected landmark feature: %+v", landmark)
	}
	if !strings.Contains(landmark.SourceURL, "maps.google.com") {
		t.Errorf("expected a maps.google.com source URL, got %q", landmark.SourceURL)
	}

	trail := features[2]
	if trail.Category != "trail" {
		t.Errorf("unexpected trail feature: %+v", trail)
	}
}

func TestParseSearchNearbyResponse_DropsPlacesOutsideRequestedCategories(t *testing.T) {
	body := loadFixture(t, "places_searchnearby_response.json")

	// Only marina requested: the landmark and trail places must not appear.
	features, err := parseSearchNearbyResponse(body, []string{"marina"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(features) != 1 || features[0].Category != "marina" {
		t.Fatalf("expected only the marina feature, got %+v", features)
	}
}

func TestParseSearchNearbyResponse_ErrorsOnUnparseableBody(t *testing.T) {
	if _, err := parseSearchNearbyResponse([]byte("not json"), []string{"marina"}); err == nil {
		t.Fatalf("expected an error for unparseable JSON")
	}
}

func TestParseSearchNearbyResponse_EmptyPlacesIsNotAnError(t *testing.T) {
	features, err := parseSearchNearbyResponse([]byte(`{"places":[]}`), []string{"marina"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(features) != 0 {
		t.Fatalf("expected 0 features, got %d", len(features))
	}
}

// ── clampInt ─────────────────────────────────────────────────────────────

func TestClampInt(t *testing.T) {
	if got := clampInt(5, 1, 20); got != 5 {
		t.Fatalf("expected 5 (in range), got %d", got)
	}
	if got := clampInt(0, 1, 20); got != 1 {
		t.Fatalf("expected clamp to min 1, got %d", got)
	}
	if got := clampInt(999, 1, 20); got != 20 {
		t.Fatalf("expected clamp to max 20, got %d", got)
	}
}
