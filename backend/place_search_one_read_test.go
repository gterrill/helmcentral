package main

import (
	"net/http"
	"testing"
)

// The handler reads the boat's position once and searches around that
// reading; a second read that lost the position must not turn the answer into
// a 200 carrying a note written for Mate.
func TestPlaceSearch_ReadsTheBoatPositionOnce(t *testing.T) {
	provider := &fakeSearchPlacesProvider{id: "fake-places", result: placeSearchResult{
		Search:  "exact",
		Results: []placeSearchMatch{{Name: "Coral Coast Marina", Kind: "marina", Lat: -27.2, Lon: 153.1}},
	}}
	deps := findPlacesDeps(-27.4, 153.1, provider)
	reads := 0
	deps.vesselState = func() (vesselStateData, error) {
		reads++
		if reads > 1 {
			return vesselStateData{Latitude: -1, Longitude: -1}, nil
		}
		return vesselStateData{Latitude: -27.4, Longitude: 153.1}, nil
	}
	withPlaceSearchDeps(t, deps)

	rec := serveForecastPoint(t, placeSearchHandler, "?q=Coral+Coast")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if reads != 1 {
		t.Fatalf("the boat position was read %d times, want once", reads)
	}
	if got := provider.lastCall(); got.Lat != -27.4 || got.Lon != 153.1 {
		t.Fatalf("searched around %v,%v, want the first reading", got.Lat, got.Lon)
	}
}
