package main

// place_search_fixtures_test.go tests place_name_at's and search_places'
// real parsing/ranking against live captures under testdata/ (see this
// plugin's README fixtures section for how and when these were captured).
// These are genuine responses from overpass.openstreetmap.fr, not
// hand-shaped JSON - per AGENTS.md, a fixture must be captured from the
// real server, since an assumed shape can make a test green against a
// payload that doesn't exist.

import (
	"encoding/json"
	"testing"
)

func loadOverpassElements(t *testing.T, filename string) []overpassElement {
	t.Helper()
	raw := loadFixture(t, filename)
	var parsed overpassResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", filename, err)
	}
	return parsed.Elements
}

// ── place_name_at fixtures ────────────────────────────────────────────────

func TestLiveFixture_PlaceNameAtLindeman1500m(t *testing.T) {
	elements := loadOverpassElements(t, "overpass_place_name_lindeman_1500m.json")
	if len(elements) != 1 {
		t.Fatalf("fixture drift: expected exactly 1 element in the 1500m ring, got %d", len(elements))
	}

	winner, ok := bestNamedPlaceFeature(elements, lindemanLatForFixtures, lindemanLonForFixtures)
	if !ok {
		t.Fatalf("expected a winner from the 1500m ring")
	}
	if winner.Name != "Lindeman Island" || winner.Kind != "island" {
		t.Fatalf("expected Lindeman Island (island) to win the 1500m ring, got %+v", winner)
	}
}

func TestLiveFixture_PlaceNameAtLindeman5000m(t *testing.T) {
	elements := loadOverpassElements(t, "overpass_place_name_lindeman_5000m.json")
	if len(elements) != 13 {
		t.Fatalf("fixture drift: expected 13 elements in the 5000m ring (matches backend/place_name_test.go's own Lindeman fixture), got %d", len(elements))
	}

	// Turtle Bay (natural=bay, ~2080m) beats every island/islet in the ring
	// on rank alone (bay=1 < island=2 < islet=3), and beats the two other
	// bays (Kennedy Sound ~3331m, Plantation Bay ~2153m) on distance -
	// measured directly from the fixture's own coordinates.
	winner, ok := bestNamedPlaceFeature(elements, lindemanLatForFixtures, lindemanLonForFixtures)
	if !ok {
		t.Fatalf("expected a winner from the 5000m ring")
	}
	if winner.Name != "Turtle Bay" || winner.Kind != "bay" {
		t.Fatalf("expected Turtle Bay (bay) to win the 5000m ring, got %+v", winner)
	}
}

const (
	lindemanLatForFixtures = -20.4467
	lindemanLonForFixtures = 149.0353
)

// ── search_places fixtures ─────────────────────────────────────────────────

func TestLiveFixture_SearchPlacesExactHillInlet(t *testing.T) {
	elements := loadOverpassElements(t, "overpass_search_places_exact_hill_inlet.json")
	matches := elementsToSearchMatches(elements)
	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 exact-name match for Hill Inlet, got %d: %+v", len(matches), matches)
	}
	if matches[0].Name != "Hill Inlet" || matches[0].Kind != "bay" {
		t.Fatalf("expected Hill Inlet classified as bay (natural=bay), got %+v", matches[0])
	}
}

func TestLiveFixture_SearchPlacesExactWhitehavenBeach(t *testing.T) {
	elements := loadOverpassElements(t, "overpass_search_places_exact_whitehaven_beach.json")
	matches := elementsToSearchMatches(elements)
	if len(matches) != 2 {
		t.Fatalf("expected exactly 2 exact-name matches for Whitehaven Beach (a node and a way sharing the name), got %d: %+v", len(matches), matches)
	}
	for _, m := range matches {
		if m.Name != "Whitehaven Beach" {
			t.Fatalf("expected every match to be named Whitehaven Beach, got %+v", m)
		}
	}
	// The node carries only tourism=camp_site (none of findPlacesKind's four
	// recognised tags), so it must fall back to "feature" rather than being
	// discarded; the way carries natural=beach.
	kinds := map[string]bool{}
	for _, m := range matches {
		kinds[m.Kind] = true
	}
	if !kinds["feature"] || !kinds["beach"] {
		t.Fatalf("expected one match classified as feature (fallback) and one as beach, got kinds %v", kinds)
	}
}

// TestLiveFixture_SearchPlacesRung2Ladder exercises the full two-rung
// ladder against a real query that only resolves on rung 2: rung 1's
// exact-name variants of "whitehaven" (lowercase, partial - not the OSM tag
// "Whitehaven Beach") come back empty live, and rung 2's case-insensitive
// regex finds it and its neighbours.
func TestLiveFixture_SearchPlacesRung2Ladder(t *testing.T) {
	exactElements := loadOverpassElements(t, "overpass_search_places_exact_whitehaven_partial.json")
	if len(exactElements) != 0 {
		t.Fatalf("fixture drift: expected rung 1's exact-name search for the lowercase partial to come back empty live, got %d elements", len(exactElements))
	}

	regexElements := loadOverpassElements(t, "overpass_search_places_regex_whitehaven_partial.json")
	if len(regexElements) != 3 {
		t.Fatalf("fixture drift: expected exactly 3 elements from the live rung 2 regex capture, got %d", len(regexElements))
	}

	poster := &sequentialSearchPoster{responses: [][]overpassElement{exactElements, regexElements}}
	out, err := runSearchPlaces(poster.post, searchPlacesInput{
		Query: "whitehaven", Lat: lindemanLatForFixtures, Lon: lindemanLonForFixtures, Broad: true,
	})
	if err != nil {
		t.Fatalf("runSearchPlaces: %v", err)
	}
	if out.Search != "regex" {
		t.Fatalf("expected search=regex once rung 1 came back empty, got %q", out.Search)
	}
	if len(out.Results) != 3 {
		t.Fatalf("expected all 3 live regex matches surfaced, got %d: %+v", len(out.Results), out.Results)
	}

	names := map[string]string{}
	for _, r := range out.Results {
		names[r.Name] = r.Kind
	}
	if names["Whitehaven Bay"] != "bay" {
		t.Errorf("expected Whitehaven Bay classified as bay, got %+v", names)
	}
	if names["South Whitehaven Beach"] != "beach" {
		t.Errorf("expected South Whitehaven Beach classified as beach, got %+v", names)
	}
	if names["Whitehaven Beach"] != "beach" {
		t.Errorf("expected Whitehaven Beach (the way, natural=beach) classified as beach, got %+v", names)
	}
}
