package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

func withPlaceSearchDeps(t *testing.T, deps assistantToolDeps) {
	t.Helper()
	prev := placeSearchDeps
	placeSearchDeps = func() assistantToolDeps { return deps }
	t.Cleanup(func() { placeSearchDeps = prev })
}

func TestPlaceSearch_ReturnsCandidatesWithDistanceAndBearing(t *testing.T) {
	provider := &fakeSearchPlacesProvider{id: "fake-places", result: placeSearchResult{
		Search: "exact", RadiusNm: 100,
		Results: []placeSearchMatch{
			{Name: "Coral Coast Marina", Kind: "marina", Lat: -27.2, Lon: 153.1},
			{Name: "Coral Coast Marina North", Kind: "marina", Lat: -27.0, Lon: 153.1},
		},
	}}
	withPlaceSearchDeps(t, findPlacesDeps(-27.4, 153.1, provider))

	rec := serveForecastPoint(t, placeSearchHandler, "?q="+url.QueryEscape("Coral Coast Marina"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var res assistantFindPlacesResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 2 || res.Results[0].Name != "Coral Coast Marina" || res.Results[0].StraightLineNm <= 0 {
		t.Fatalf("expected nearest-first candidates with distance, got %+v", res.Results)
	}
	if res.Results[0].StraightLineBearingDeg != 0 && res.Results[0].StraightLineBearingDeg != 360 {
		t.Fatalf("the nearer marina is due north, got bearing %d", res.Results[0].StraightLineBearingDeg)
	}
	if provider.lastCall().Query != "Coral Coast Marina" {
		t.Fatalf("provider asked for %q", provider.lastCall().Query)
	}
}

func TestPlaceSearch_BadQueryIs400(t *testing.T) {
	provider := &fakeSearchPlacesProvider{id: "fake-places"}
	withPlaceSearchDeps(t, findPlacesDeps(-27.4, 153.1, provider))
	for _, q := range []string{"", "   ", "a<b>", "x{y}"} {
		rec := serveForecastPoint(t, placeSearchHandler, "?q="+url.QueryEscape(q))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("q=%q: expected 400, got %d: %s", q, rec.Code, rec.Body.String())
		}
	}
	if provider.callCount() != 0 {
		t.Fatal("a refused query must not reach the provider")
	}
}

func TestPlaceSearch_ProviderFailureIs502(t *testing.T) {
	provider := &fakeSearchPlacesProvider{id: "fake-places", err: http.ErrHandlerTimeout}
	withPlaceSearchDeps(t, findPlacesDeps(-27.4, 153.1, provider))
	rec := serveForecastPoint(t, placeSearchHandler, "?q=Bona+Bay")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rec.Code, rec.Body.String())
	}
}

// The plugin's search_places needs a centre (it searches a bounding box), so
// with no boat position the operator endpoint says so plainly instead of
// returning an empty list with a note addressed to Mate.
func TestPlaceSearch_UnknownBoatPositionIs503(t *testing.T) {
	provider := &fakeSearchPlacesProvider{id: "fake-places"}
	for _, pos := range [][2]float64{{0, 0}, {-1, -1}} {
		withPlaceSearchDeps(t, findPlacesDeps(pos[0], pos[1], provider))
		rec := serveForecastPoint(t, placeSearchHandler, "?q=Bona+Bay")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("pos %v: expected 503, got %d: %s", pos, rec.Code, rec.Body.String())
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body["error"] != placeSearchNoPositionMessage {
			t.Fatalf("unexpected error text %q", body["error"])
		}
	}
	if provider.callCount() != 0 {
		t.Fatal("no position, no provider call")
	}
}
