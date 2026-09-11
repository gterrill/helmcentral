package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// capturingWeatherProvider is a local weatherProvider test double (like
// stubWeatherProvider in weather_providers_test.go) that additionally
// records the days argument it was called with, so get_wind_forecast tests
// can assert the clamped/default day count actually reaches the provider.
type capturingWeatherProvider struct {
	bundle weatherForecastBundle
	err    error

	gotDays int
	gotTZ   string
}

func (c *capturingWeatherProvider) ID() string   { return "capturing-weather" }
func (c *capturingWeatherProvider) Name() string { return "Capturing Weather" }
func (c *capturingWeatherProvider) Description() string {
	return "Capturing weather provider for tests"
}
func (c *capturingWeatherProvider) TTLSeconds() int64 { return 900 }
func (c *capturingWeatherProvider) FetchForecast(lat, lon float64, days int, timezone string) (weatherForecastBundle, error) {
	c.gotDays = days
	c.gotTZ = timezone
	if c.err != nil {
		return weatherForecastBundle{}, c.err
	}
	return c.bundle, nil
}

// ── two-rung Overpass ladder fake ───────────────────────────────────────

// fakeOverpassResponse is one canned answer for sequentialOverpassFetcher.
type fakeOverpassResponse struct {
	body        []byte
	contentType string
	status      int
	err         error
}

// sequentialOverpassFetcher is an injectable overpassFetcher for find_places'
// two-rung ladder. Unlike fakeOverpassFetcher (place_name_test.go), which
// keys a canned response off the query's around: radius, find_places' rungs
// are bbox-based and carry no around: clause to key off at all - so
// responses here are instead consumed one per call, in the order the ladder
// actually posts them (rung 1 first, rung 2 only if rung 1 came back
// empty). queryAt lets a test assert what each call actually asked for.
type sequentialOverpassFetcher struct {
	mu        sync.Mutex
	queries   []string
	responses []fakeOverpassResponse
}

func (f *sequentialOverpassFetcher) Do(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("sequentialOverpassFetcher: read request body: %w", err)
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, fmt.Errorf("sequentialOverpassFetcher: parse form body: %w", err)
	}

	f.mu.Lock()
	idx := len(f.queries)
	f.queries = append(f.queries, values.Get("data"))
	var resp fakeOverpassResponse
	if idx < len(f.responses) {
		resp = f.responses[idx]
	}
	f.mu.Unlock()

	if resp.err != nil {
		return nil, resp.err
	}
	status := resp.status
	if status == 0 {
		status = http.StatusOK
	}
	contentType := resp.contentType
	if contentType == "" {
		contentType = "application/json"
	}
	respBody := resp.body
	if respBody == nil {
		respBody = []byte(`{"elements":[]}`)
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}, nil
}

func (f *sequentialOverpassFetcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.queries)
}

func (f *sequentialOverpassFetcher) queryAt(i int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i < 0 || i >= len(f.queries) {
		return ""
	}
	return f.queries[i]
}

// ── find_places ─────────────────────────────────────────────────────────

func TestExecuteFindPlaces_MergesRouteWaypointAndExactRungDedupesAndSorts(t *testing.T) {
	vesselLat, vesselLon := -20.10, 149.10

	// OSM's own copy of the same Tongue Bay, ~110m from the waypoint - well
	// inside the 500m dedupe radius, so it must collapse into the waypoint
	// entry (added first) rather than appearing a second time.
	nearDupLat, nearDupLon := vesselLat+0.001, vesselLon

	// A second, distinct real-world place that happens to share the exact
	// name "Tongue Bay" (duplicate place names are common enough), ~11km
	// (~6nm) away, so sort-by-distance has something to prove.
	farLat, farLon := vesselLat+0.1, vesselLon

	overpassBody := fmt.Sprintf(`{"elements":[
		{"type":"node","id":1,"lat":%.6f,"lon":%.6f,"tags":{"name":"Tongue Bay","natural":"bay"}},
		{"type":"node","id":2,"lat":%.6f,"lon":%.6f,"tags":{"name":"Tongue Bay","seamark:type":"anchorage"}}
	]}`, nearDupLat, nearDupLon, farLat, farLon)

	fetcher := &sequentialOverpassFetcher{responses: []fakeOverpassResponse{{body: []byte(overpassBody)}}}

	deps := assistantToolDeps{
		vesselState: func() (vesselStateData, error) {
			return vesselStateData{Latitude: vesselLat, Longitude: vesselLon}, nil
		},
		overpass: fetcher,
		routes: func() []routeData {
			return []routeData{{
				Name: "Whitsundays Loop",
				Waypoints: []routeWaypoint{
					{Name: "Tongue Bay", Lat: vesselLat, Lon: vesselLon},
				},
			}}
		},
	}

	// Query exactly matches the waypoint name, so it's both a waypoint hit
	// (suppressing rung 2) and rung 1's single exact-name variant.
	raw, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Tongue Bay"}`))
	if err != nil {
		t.Fatalf("execute find_places: %v", err)
	}
	if fetcher.callCount() != 1 {
		t.Fatalf("expected only rung 1 to run (waypoint hit suppresses rung 2), got %d overpass calls", fetcher.callCount())
	}

	var result assistantFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v (raw: %s)", err, raw)
	}

	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results (the near duplicate deduped, the far one distinct), got %d: %+v", len(result.Results), result.Results)
	}
	if result.Results[0].Name != "Tongue Bay" {
		t.Errorf("expected the nearer Tongue Bay first, got %q", result.Results[0].Name)
	}
	if result.Results[0].Source != "route:Whitsundays Loop" {
		t.Errorf("expected the route waypoint to win the dedupe (added first), got source %q", result.Results[0].Source)
	}
	if result.Results[0].DistanceNm != 0 {
		t.Errorf("expected the waypoint's distance to be ~0nm, got %v", result.Results[0].DistanceNm)
	}
	if result.Results[1].DistanceNm <= result.Results[0].DistanceNm {
		t.Errorf("expected the far result to be sorted after the near one")
	}
	if result.Results[1].BearingDeg < 0 || result.Results[1].BearingDeg > 360 {
		t.Errorf("expected a valid compass bearing, got %d", result.Results[1].BearingDeg)
	}
	if result.Results[1].Source != "osm" {
		t.Errorf("expected the far OSM-only result's source to be osm, got %q", result.Results[1].Source)
	}
	if result.Results[1].Kind != "anchorage" {
		t.Errorf("expected kind=anchorage (seamark:type wins), got %q", result.Results[1].Kind)
	}
	if result.Search != "waypoints" {
		t.Errorf("expected search=waypoints, got %q", result.Search)
	}
	if result.RadiusNm != assistantFindPlacesRadiusNm {
		t.Errorf("expected radius_nm=%g (rung 1, the only rung that ran), got %v", assistantFindPlacesRadiusNm, result.RadiusNm)
	}
}

func TestExecuteFindPlaces_Rung1ExactHitSkipsRung2(t *testing.T) {
	vesselLat, vesselLon := -20.1, 149.1
	fetcher := &sequentialOverpassFetcher{
		responses: []fakeOverpassResponse{
			{body: []byte(`{"elements":[{"type":"node","id":1,"lat":-20.11,"lon":149.1,"tags":{"name":"Shag Cove","natural":"bay"}}]}`)},
		},
	}
	deps := assistantToolDeps{
		vesselState: func() (vesselStateData, error) {
			return vesselStateData{Latitude: vesselLat, Longitude: vesselLon}, nil
		},
		overpass: fetcher,
		routes:   func() []routeData { return nil },
	}

	raw, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Shag Cove"}`))
	if err != nil {
		t.Fatalf("execute find_places: %v", err)
	}
	if fetcher.callCount() != 1 {
		t.Fatalf("expected rung 2 to be skipped once rung 1 already found a match, got %d overpass calls", fetcher.callCount())
	}
	if !strings.Contains(fetcher.queryAt(0), `["name"="Shag Cove"]`) {
		t.Errorf("expected rung 1's query to search the exact name, got: %s", fetcher.queryAt(0))
	}
	if strings.Contains(fetcher.queryAt(0), "around:") {
		t.Errorf("expected rung 1's query to use a bbox, not around:, got: %s", fetcher.queryAt(0))
	}

	var result assistantFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	if len(result.Results) != 1 || result.Results[0].Name != "Shag Cove" {
		t.Fatalf("expected the rung 1 exact hit, got %+v", result.Results)
	}
	if result.Results[0].Kind != "bay" {
		t.Errorf("expected kind=bay from natural=bay, got %q", result.Results[0].Kind)
	}
	if result.Search != "exact" {
		t.Errorf("expected search=exact, got %q", result.Search)
	}
	if result.RadiusNm != assistantFindPlacesRadiusNm {
		t.Errorf("expected radius_nm=%g (rung 1), got %v", assistantFindPlacesRadiusNm, result.RadiusNm)
	}
}

func TestExecuteFindPlaces_Rung2RunsOnlyWhenRung1Empty(t *testing.T) {
	vesselLat, vesselLon := -20.1, 149.1
	fetcher := &sequentialOverpassFetcher{
		responses: []fakeOverpassResponse{
			{body: []byte(`{"elements":[]}`)}, // rung 1: exact match, nothing
			{body: []byte(`{"elements":[{"type":"node","id":2,"lat":-20.15,"lon":149.12,"tags":{"name":"Tongue Point","natural":"cape"}}]}`)}, // rung 2: partial match hit
		},
	}
	deps := assistantToolDeps{
		vesselState: func() (vesselStateData, error) {
			return vesselStateData{Latitude: vesselLat, Longitude: vesselLon}, nil
		},
		overpass: fetcher,
		routes:   func() []routeData { return nil },
	}

	raw, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Tongue"}`))
	if err != nil {
		t.Fatalf("execute find_places: %v", err)
	}
	if fetcher.callCount() != 2 {
		t.Fatalf("expected both rungs queried when rung 1 comes back empty, got %d calls", fetcher.callCount())
	}
	if !strings.Contains(fetcher.queryAt(0), `["name"="Tongue"]`) {
		t.Errorf("expected rung 1's query to be the exact-name search, got: %s", fetcher.queryAt(0))
	}
	if strings.Contains(fetcher.queryAt(0), "around:") || strings.Contains(fetcher.queryAt(0), `"natural"~`) {
		t.Errorf("expected rung 1's query to carry neither around: nor a tag filter, got: %s", fetcher.queryAt(0))
	}
	if !strings.Contains(fetcher.queryAt(1), `"name"~"Tongue"`) {
		t.Errorf("expected rung 2's query to be the partial-name regex search, got: %s", fetcher.queryAt(1))
	}
	if strings.Contains(fetcher.queryAt(1), "around:") {
		t.Errorf("expected rung 2 to search a bbox, not around:, got: %s", fetcher.queryAt(1))
	}

	var result assistantFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	if len(result.Results) != 1 || result.Results[0].Name != "Tongue Point" {
		t.Fatalf("expected the rung 2 hit, got %+v", result.Results)
	}
	if result.Search != "regex" {
		t.Errorf("expected search=regex, got %q", result.Search)
	}
	if result.RadiusNm != assistantFindPlacesRegexRadiusNm {
		t.Errorf("expected radius_nm=%g (rung 2), got %v", assistantFindPlacesRegexRadiusNm, result.RadiusNm)
	}
}

func TestExecuteFindPlaces_WaypointHitSuppressesRung2(t *testing.T) {
	vesselLat, vesselLon := -20.1, 149.1
	fetcher := &sequentialOverpassFetcher{
		responses: []fakeOverpassResponse{
			{body: []byte(`{"elements":[]}`)}, // rung 1: no exact OSM match for the query text
		},
	}
	deps := assistantToolDeps{
		vesselState: func() (vesselStateData, error) {
			return vesselStateData{Latitude: vesselLat, Longitude: vesselLon}, nil
		},
		overpass: fetcher,
		routes: func() []routeData {
			return []routeData{{
				Name: "Whitsundays Loop",
				Waypoints: []routeWaypoint{
					{Name: "Tongue Bay", Lat: vesselLat, Lon: vesselLon},
				},
			}}
		},
	}

	raw, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Tongue"}`))
	if err != nil {
		t.Fatalf("execute find_places: %v", err)
	}
	if fetcher.callCount() != 1 {
		t.Fatalf("expected a waypoint hit to suppress rung 2, got %d overpass calls", fetcher.callCount())
	}

	var result assistantFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	if len(result.Results) != 1 || result.Results[0].Source != "route:Whitsundays Loop" {
		t.Fatalf("expected only the waypoint hit, got %+v", result.Results)
	}
	if result.Search != "waypoints" {
		t.Errorf("expected search=waypoints, got %q", result.Search)
	}
	if result.RadiusNm != assistantFindPlacesRadiusNm {
		t.Errorf("expected radius_nm=%g (rung 1, the only rung that ran), got %v", assistantFindPlacesRadiusNm, result.RadiusNm)
	}
}

func TestExecuteFindPlaces_NoteMentionsBothRungsWhenNothingFound(t *testing.T) {
	fetcher := &sequentialOverpassFetcher{
		responses: []fakeOverpassResponse{
			{body: []byte(`{"elements":[]}`)},
			{body: []byte(`{"elements":[]}`)},
		},
	}
	deps := assistantToolDeps{
		vesselState: func() (vesselStateData, error) { return vesselStateData{Latitude: -20.1, Longitude: 149.1}, nil },
		overpass:    fetcher,
		routes:      func() []routeData { return nil },
	}

	raw, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Shag Cove"}`))
	if err != nil {
		t.Fatalf("execute find_places: %v", err)
	}
	var result assistantFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	want := `no named feature matching "Shag Cove" within 100 nm by exact name or within 20 nm by partial name`
	if result.Note != want {
		t.Errorf("note = %q, want %q", result.Note, want)
	}
	if result.Search != "none" {
		t.Errorf("expected search=none, got %q", result.Search)
	}
	if result.RadiusNm != assistantFindPlacesRegexRadiusNm {
		t.Errorf("expected radius_nm=%g (rung 2, the deepest rung attempted), got %v", assistantFindPlacesRegexRadiusNm, result.RadiusNm)
	}
	if fetcher.callCount() != 2 {
		t.Fatalf("expected both rungs to be attempted, got %d calls", fetcher.callCount())
	}
}

func TestExecuteFindPlaces_Rung2TransportErrorIsNeverSwallowed(t *testing.T) {
	fetcher := &sequentialOverpassFetcher{
		responses: []fakeOverpassResponse{
			{body: []byte(`{"elements":[]}`)},
			{err: fmt.Errorf("network unreachable")},
		},
	}
	deps := assistantToolDeps{
		vesselState: func() (vesselStateData, error) { return vesselStateData{Latitude: -20.1, Longitude: 149.1}, nil },
		overpass:    fetcher,
		routes:      func() []routeData { return nil },
	}

	_, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Bay"}`))
	if err == nil {
		t.Fatalf("expected rung 2's transport error to surface as an error, not be swallowed")
	}
}

func TestAssistantBoundingBox_100NmAtWhitsundayLatitude(t *testing.T) {
	south, west, north, east := assistantBoundingBox(-20.03, 148.45, 100)

	round2 := func(v float64) float64 { return math.Round(v*100) / 100 }
	if got := round2(south); got != -21.70 {
		t.Errorf("south = %v, want -21.70", got)
	}
	if got := round2(west); got != 146.68 {
		t.Errorf("west = %v, want 146.68", got)
	}
	if got := round2(north); got != -18.36 {
		t.Errorf("north = %v, want -18.36", got)
	}
	if got := round2(east); got != 150.22 {
		t.Errorf("east = %v, want 150.22", got)
	}
}

func TestBuildOverpassExactNameQuery_VariantsNoAroundNoTagFilter(t *testing.T) {
	south, west, north, east := assistantBoundingBox(-20.1, 149.1, assistantFindPlacesRadiusNm)
	got := buildOverpassExactNameQuery("bona bay", south, west, north, east)

	for _, want := range []string{`["name"="Bona Bay"]`, `["name"="bona bay"]`} {
		if !strings.Contains(got, want) {
			t.Errorf("expected the query to contain %q, got: %s", want, got)
		}
	}
	if strings.Contains(got, "around:") {
		t.Errorf("expected a bbox filter, not around:, got: %s", got)
	}
	for _, tag := range []string{`"natural"`, `"seamark:type"`, `"place"`, `"leisure"`} {
		if strings.Contains(got, tag) {
			t.Errorf("expected no tag filter (kind is derived host-side), found %q in: %s", tag, got)
		}
	}
}

func TestBuildOverpassNameSearchQuery_EscapesRegexMetacharacters(t *testing.T) {
	south, west, north, east := assistantBoundingBox(-20.1, 149.1, assistantFindPlacesRegexRadiusNm)
	got := buildOverpassNameSearchQuery("Hook Reef (outer)", south, west, north, east)

	want := `Hook Reef \\(outer\\)`
	if !strings.Contains(got, want) {
		t.Fatalf("expected the query to contain the escaped literal %q, got: %s", want, got)
	}
	if strings.Contains(got, `"Hook Reef (outer)"`) {
		t.Fatalf("expected the parentheses to be escaped rather than passed through raw: %s", got)
	}
	if strings.Contains(got, "around:") {
		t.Fatalf("expected a bbox filter, not around:, got: %s", got)
	}
}

func TestOverpassFindPlacesKind(t *testing.T) {
	cases := []struct {
		name string
		tags map[string]string
		want string
	}{
		{"natural only", map[string]string{"natural": "bay"}, "bay"},
		{"seamark:type wins over natural", map[string]string{"seamark:type": "anchorage", "natural": "bay"}, "anchorage"},
		{"no recognised tag falls back to feature", map[string]string{"building": "yes"}, "feature"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := overpassFindPlacesKind(tc.tags); got != tc.want {
				t.Errorf("overpassFindPlacesKind(%v) = %q, want %q", tc.tags, got, tc.want)
			}
		})
	}
}

func TestAssistantTitleCase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"st helens", "St Helens"},
		{"BONA BAY", "Bona Bay"},
		{"bell's beach", "Bell's Beach"},
		{"port-side bay", "Port-side Bay"},
		{"  tongue   bay  ", "Tongue Bay"},
	}
	for _, tc := range cases {
		if got := assistantTitleCase(tc.in); got != tc.want {
			t.Errorf("assistantTitleCase(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAssistantExactNameVariants_CommaQualifierIncludesBareHead(t *testing.T) {
	variants := assistantExactNameVariants("Bona Bay, Gloucester Island")
	want := "Bona Bay"
	found := false
	for _, v := range variants {
		if v == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected variants for %q to include the bare head %q, got %v", "Bona Bay, Gloucester Island", want, variants)
	}
}

func TestExecuteFindPlaces_QualifierResolvesCentresRung2AndReportsCentredOn(t *testing.T) {
	vesselLat, vesselLon := -20.1, 149.1
	qualifierLat, qualifierLon := -19.8, 148.5 // "Gloucester Island", well outside rung 2's 20nm radius from the vessel

	fetcher := &sequentialOverpassFetcher{
		responses: []fakeOverpassResponse{
			{body: []byte(`{"elements":[]}`)}, // rung 1: exact match for the full query, nothing
			{body: []byte(fmt.Sprintf(`{"elements":[{"type":"node","id":1,"lat":%.6f,"lon":%.6f,"tags":{"name":"Gloucester Island","place":"island"}}]}`, qualifierLat, qualifierLon))}, // qualifier exact lookup: one hit
			{body: []byte(`{"elements":[]}`)}, // rung 2: partial match, nothing (not the point of this test)
		},
	}
	deps := assistantToolDeps{
		vesselState: func() (vesselStateData, error) {
			return vesselStateData{Latitude: vesselLat, Longitude: vesselLon}, nil
		},
		overpass: fetcher,
		routes:   func() []routeData { return nil },
	}

	raw, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Bona Bay, Gloucester Island"}`))
	if err != nil {
		t.Fatalf("execute find_places: %v", err)
	}
	if fetcher.callCount() != 3 {
		t.Fatalf("expected 3 overpass calls (rung 1, qualifier lookup, rung 2), got %d", fetcher.callCount())
	}
	if !strings.Contains(fetcher.queryAt(1), `["name"="Gloucester Island"]`) {
		t.Errorf("expected the qualifier lookup to search the exact qualifier name, got: %s", fetcher.queryAt(1))
	}

	wantSouth, wantWest, wantNorth, wantEast := assistantBoundingBox(qualifierLat, qualifierLon, assistantFindPlacesRegexRadiusNm)
	wantBbox := overpassBoundingBoxClause(wantSouth, wantWest, wantNorth, wantEast)
	if !strings.Contains(fetcher.queryAt(2), wantBbox) {
		t.Errorf("expected rung 2's bbox to be centred on the qualifier hit (%s), got: %s", wantBbox, fetcher.queryAt(2))
	}

	var result assistantFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	if result.CentredOn != "Gloucester Island" {
		t.Errorf("expected centred_on=%q, got %q", "Gloucester Island", result.CentredOn)
	}
	if result.Centre == nil || result.Centre.Lat != qualifierLat || result.Centre.Lon != qualifierLon {
		t.Errorf("expected centre to be the qualifier hit's coordinates (%v,%v), got %+v", qualifierLat, qualifierLon, result.Centre)
	}
}

func TestExecuteFindPlaces_QualifierDoesNotResolveFallsBackToVesselCentre(t *testing.T) {
	vesselLat, vesselLon := -20.1, 149.1
	fetcher := &sequentialOverpassFetcher{
		responses: []fakeOverpassResponse{
			{body: []byte(`{"elements":[]}`)}, // rung 1: nothing
			{body: []byte(`{"elements":[]}`)}, // qualifier lookup: nothing resolves
			{body: []byte(`{"elements":[]}`)}, // rung 2: nothing
		},
	}
	deps := assistantToolDeps{
		vesselState: func() (vesselStateData, error) {
			return vesselStateData{Latitude: vesselLat, Longitude: vesselLon}, nil
		},
		overpass: fetcher,
		routes:   func() []routeData { return nil },
	}

	raw, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Bona Bay, Gloucester Island"}`))
	if err != nil {
		t.Fatalf("execute find_places: %v", err)
	}
	if fetcher.callCount() != 3 {
		t.Fatalf("expected 3 overpass calls even when the qualifier does not resolve, got %d", fetcher.callCount())
	}

	wantSouth, wantWest, wantNorth, wantEast := assistantBoundingBox(vesselLat, vesselLon, assistantFindPlacesRegexRadiusNm)
	wantBbox := overpassBoundingBoxClause(wantSouth, wantWest, wantNorth, wantEast)
	if !strings.Contains(fetcher.queryAt(2), wantBbox) {
		t.Errorf("expected rung 2's bbox to fall back to the vessel centre (%s), got: %s", wantBbox, fetcher.queryAt(2))
	}

	var result assistantFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	if result.CentredOn != "" {
		t.Errorf("expected no centred_on when the qualifier does not resolve, got %q", result.CentredOn)
	}
	if result.Centre == nil || result.Centre.Lat != vesselLat || result.Centre.Lon != vesselLon {
		t.Errorf("expected centre to remain the vessel position, got %+v", result.Centre)
	}
}

func TestExecuteFindPlaces_NoFixAndNoNearArgsReturnsNoteNotError(t *testing.T) {
	fetcher := &fakeOverpassFetcher{}
	deps := assistantToolDeps{
		vesselState: func() (vesselStateData, error) { return vesselStateData{Latitude: -1, Longitude: -1}, nil },
		overpass:    fetcher,
		routes:      func() []routeData { return nil },
	}

	raw, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Tongue Bay"}`))
	if err != nil {
		t.Fatalf("execute find_places: %v", err)
	}

	var result assistantFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.Results) != 0 {
		t.Fatalf("expected an empty results array, got %+v", result.Results)
	}
	if !strings.Contains(result.Note, "vessel position unknown") {
		t.Fatalf("expected a note about unknown vessel position, got %q", result.Note)
	}
	if fetcher.callCount() != 0 {
		t.Fatalf("expected no overpass call when there is no usable fix, got %d", fetcher.callCount())
	}
}

func TestExecuteFindPlaces_OverpassRateLimitIsError(t *testing.T) {
	fetcher := &sequentialOverpassFetcher{
		responses: []fakeOverpassResponse{
			{body: []byte(`<html><body>Dispatcher_Client::request_read_and_idx::rate_limited</body></html>`), contentType: "text/html"},
		},
	}
	deps := assistantToolDeps{
		vesselState: func() (vesselStateData, error) { return vesselStateData{Latitude: -20.1, Longitude: 149.1}, nil },
		overpass:    fetcher,
		routes:      func() []routeData { return nil },
	}

	_, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Bay"}`))
	if err == nil {
		t.Fatalf("expected an error for a rate-limited overpass response")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "rate limit") {
		t.Fatalf("expected the error to mention rate limiting, got %q", err.Error())
	}
}

// ── get_wind_forecast ───────────────────────────────────────────────────

func TestExecuteGetWindForecast_RoundsFiltersNullsAndJoinsWaves(t *testing.T) {
	// now truncates to 2026-09-12T00:00Z; the vessel's local zone at
	// lon=149.1 is UTC+10, so local hours run 10 hours ahead of these UTC
	// timestamps.
	now := time.Date(2026, 9, 12, 0, 30, 0, 0, time.UTC)
	lat, lon := -20.1, 149.1

	weatherStub := &capturingWeatherProvider{
		bundle: weatherForecastBundle{
			Hourly: []weatherHourPoint{
				// 2026-09-11T20:00Z -> local 06:00, %3==0, but before nowHour: dropped.
				{Time: time.Date(2026, 9, 11, 20, 0, 0, 0, time.UTC), WindSpeedMS: 5.0, WindGustMS: 6.0, WindDirectionDeg: 135},
				// local 10:00 and 11:00, not on a 3-hour boundary: dropped.
				{Time: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC), WindSpeedMS: 5.0},
				{Time: time.Date(2026, 9, 12, 1, 0, 0, 0, time.UTC), WindSpeedMS: 5.0},
				// local 12:00, kept: the all-zero sentinel row.
				{Time: time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC)},
				// local 15:00, kept: real wind, joined to a wave hour below.
				{Time: time.Date(2026, 9, 12, 5, 0, 0, 0, time.UTC), WindSpeedMS: 5.0, WindGustMS: 6.0, WindDirectionDeg: 135},
			},
			Cached:   true,
			CachedAt: now,
		},
	}
	waveStub := &stubWaveProvider{
		id: "open-meteo-marine",
		bundle: waveForecastBundle{
			Hourly: []waveHourPoint{
				{Time: time.Date(2026, 9, 12, 5, 0, 0, 0, time.UTC), WaveHeightM: 0.8, WavePeriodS: 6, WaveDirectionDeg: 90},
			},
		},
	}

	deps := assistantToolDeps{
		now:     func() time.Time { return now },
		weather: func() (weatherProvider, string, error) { return weatherStub, "capturing-weather", nil },
		waves:   func() (waveProvider, string, error) { return waveStub, "open-meteo-marine", nil },
	}

	raw, err := deps.execute(context.Background(), "get_wind_forecast", json.RawMessage(fmt.Sprintf(`{"lat":%f,"lon":%f,"days":2}`, lat, lon)))
	if err != nil {
		t.Fatalf("execute get_wind_forecast: %v", err)
	}

	var result assistantWindForecastResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}

	wantTZ := vesselLocalTimezoneName(lon)
	if result.Timezone != wantTZ {
		t.Errorf("expected timezone %q, got %q", wantTZ, result.Timezone)
	}
	if weatherStub.gotTZ != wantTZ {
		t.Errorf("expected the provider to be called with timezone %q, got %q", wantTZ, weatherStub.gotTZ)
	}
	if weatherStub.gotDays != 2 {
		t.Errorf("expected days=2 to pass straight through, got %d", weatherStub.gotDays)
	}

	if len(result.Hourly) != 2 {
		t.Fatalf("expected exactly 2 kept hourly rows (3-hourly, no past hours), got %d: %+v", len(result.Hourly), result.Hourly)
	}

	zeroRow := result.Hourly[0]
	if zeroRow.WindKts != nil || zeroRow.GustKts != nil || zeroRow.Dir != nil || zeroRow.DirDeg != nil {
		t.Errorf("expected the all-zero sentinel row to null every wind field, got %+v", zeroRow)
	}
	if zeroRow.WaveM != nil || zeroRow.WaveS != nil || zeroRow.WaveDir != nil {
		t.Errorf("expected no wave data for an hour with no matching wave timestamp, got %+v", zeroRow)
	}

	windyRow := result.Hourly[1]
	if windyRow.WindKts == nil || *windyRow.WindKts != 10 {
		t.Errorf("expected wind_kts=10 (5.0 m/s rounded), got %v", windyRow.WindKts)
	}
	if windyRow.GustKts == nil || *windyRow.GustKts != 12 {
		t.Errorf("expected gust_kts=12 (6.0 m/s rounded), got %v", windyRow.GustKts)
	}
	if windyRow.Dir == nil || *windyRow.Dir != "SE" {
		t.Errorf("expected dir=SE, got %v", windyRow.Dir)
	}
	if windyRow.DirDeg == nil || *windyRow.DirDeg != 135 {
		t.Errorf("expected dir_deg=135, got %v", windyRow.DirDeg)
	}
	if windyRow.WaveM == nil || *windyRow.WaveM != 0.8 {
		t.Errorf("expected wave_m=0.8 joined by unix timestamp, got %v", windyRow.WaveM)
	}
	if windyRow.WaveS == nil || *windyRow.WaveS != 6 {
		t.Errorf("expected wave_s=6, got %v", windyRow.WaveS)
	}
	if windyRow.WaveDir == nil || *windyRow.WaveDir != "E" {
		t.Errorf("expected wave_dir=E, got %v", windyRow.WaveDir)
	}

	if result.WaveProvider != "open-meteo-marine" {
		t.Errorf("expected wave_provider=open-meteo-marine, got %q", result.WaveProvider)
	}
	if result.WavesError != "" {
		t.Errorf("expected no waves_error on a successful wave fetch, got %q", result.WavesError)
	}
	if result.WeatherProvider != "capturing-weather" {
		t.Errorf("expected weather_provider=capturing-weather, got %q", result.WeatherProvider)
	}
	if !result.Cached {
		t.Errorf("expected cached=true to pass through from the bundle")
	}
}

func TestExecuteGetWindForecast_WaveErrorStillReturnsWind(t *testing.T) {
	now := time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC) // local 12:00 at lon 149.1, %3==0
	weatherStub := &capturingWeatherProvider{
		bundle: weatherForecastBundle{
			Hourly: []weatherHourPoint{
				{Time: now, WindSpeedMS: 5.0, WindGustMS: 6.0, WindDirectionDeg: 135},
			},
		},
	}
	deps := assistantToolDeps{
		now:     func() time.Time { return now },
		weather: func() (weatherProvider, string, error) { return weatherStub, "capturing-weather", nil },
		waves:   func() (waveProvider, string, error) { return nil, "", fmt.Errorf("no wave plugin installed") },
	}

	raw, err := deps.execute(context.Background(), "get_wind_forecast", json.RawMessage(`{"lat":-20.1,"lon":149.1}`))
	if err != nil {
		t.Fatalf("execute get_wind_forecast: %v", err)
	}
	var result assistantWindForecastResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.WavesError == "" {
		t.Fatalf("expected a waves_error when the wave provider fails to resolve")
	}
	if len(result.Hourly) != 1 || result.Hourly[0].WindKts == nil {
		t.Fatalf("expected wind data still present despite the wave failure: %+v", result)
	}
}

func TestExecuteGetWindForecast_ClampsDays(t *testing.T) {
	weatherStub := &capturingWeatherProvider{}
	deps := assistantToolDeps{
		now:     time.Now,
		weather: func() (weatherProvider, string, error) { return weatherStub, "capturing-weather", nil },
		waves:   func() (waveProvider, string, error) { return nil, "", fmt.Errorf("no wave plugin installed") },
	}

	cases := []struct {
		body string
		want int
	}{
		{`{"lat":-20.1,"lon":149.1}`, 3},
		{`{"lat":-20.1,"lon":149.1,"days":0}`, 3},
		{`{"lat":-20.1,"lon":149.1,"days":99}`, assistantMaxForecastDays},
		{`{"lat":-20.1,"lon":149.1,"days":5}`, 5},
	}
	for _, tc := range cases {
		if _, err := deps.execute(context.Background(), "get_wind_forecast", json.RawMessage(tc.body)); err != nil {
			t.Fatalf("execute(%s): %v", tc.body, err)
		}
		if weatherStub.gotDays != tc.want {
			t.Errorf("body %s: expected clamped days=%d, got %d", tc.body, tc.want, weatherStub.gotDays)
		}
	}
}

// ── get_tides ───────────────────────────────────────────────────────────

func TestExecuteGetTides_NearestStationWindowedExtremesAustraliaBrisbane(t *testing.T) {
	now := time.Date(2026, 9, 12, 3, 0, 0, 0, time.UTC)

	brisbane := tideStation{StationID: "BRIS", Name: "Brisbane Bar", State: "QLD", Lat: -27.36, Lon: 153.17, Timezone: "Australia/Brisbane"}
	farAway := tideStation{StationID: "FAR", Name: "Far Away Station", Lat: 10.0, Lon: 10.0, Timezone: "UTC"}

	// Australia/Brisbane is UTC+10 with no DST, so local midnight on
	// 2026-09-12 is 2026-09-11T14:00:00Z.
	localMidnightUTC := time.Date(2026, 9, 11, 14, 0, 0, 0, time.UTC)
	extremes := []tideExtremePoint{
		{Time: localMidnightUTC.Add(-1 * time.Hour), HeightM: 1.0, High: false}, // before the window: dropped
		{Time: localMidnightUTC.Add(5 * time.Hour), HeightM: 2.4, High: true},   // inside the 2-day window: kept
		{Time: localMidnightUTC.Add(48 * time.Hour), HeightM: 2.5, High: true},  // exactly at the window end: dropped
	}

	provider := &stubTideProvider{
		id:       "bom",
		stations: []tideStation{farAway, brisbane},
		result: tideChartResult{
			Extremes:       extremes,
			CurrentHeightM: 1.8,
			Direction:      "Rising",
			Cached:         true,
			CachedAt:       now,
		},
	}

	deps := assistantToolDeps{
		now:   func() time.Time { return now },
		tides: func() (tideProvider, string, error) { return provider, "bom", nil },
	}

	raw, err := deps.execute(context.Background(), "get_tides", json.RawMessage(`{"lat":-27.4,"lon":153.1,"days":2}`))
	if err != nil {
		t.Fatalf("execute get_tides: %v", err)
	}

	var result assistantGetTidesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}

	if result.Station.ID != "BRIS" {
		t.Fatalf("expected the nearest station (Brisbane), got %q", result.Station.ID)
	}
	if result.TimeBasis != "Australia/Brisbane" {
		t.Errorf("expected time_basis=Australia/Brisbane (proves tzdata resolves it), got %q", result.TimeBasis)
	}
	if len(result.Extremes) != 1 {
		t.Fatalf("expected exactly 1 extreme inside the 2-day window, got %d: %+v", len(result.Extremes), result.Extremes)
	}
	if result.Extremes[0].Type != "high" {
		t.Errorf("expected the kept extreme to be a high, got %q", result.Extremes[0].Type)
	}
	if result.Extremes[0].HeightM != 2.4 {
		t.Errorf("expected height_m=2.4, got %v", result.Extremes[0].HeightM)
	}
	if result.Now.HeightM != 1.8 || result.Now.Direction != "Rising" {
		t.Errorf("expected now to echo the current height/direction, got %+v", result.Now)
	}
	if result.Provider != "bom" {
		t.Errorf("expected provider=bom, got %q", result.Provider)
	}
	if !result.Cached {
		t.Errorf("expected cached=true to pass through")
	}
	if result.Note != "" {
		t.Errorf("expected no note when extremes are found, got %q", result.Note)
	}
}

func TestExecuteGetTides_NoStationsIsError(t *testing.T) {
	provider := &stubTideProvider{id: "bom", stations: nil}
	deps := assistantToolDeps{
		now:   time.Now,
		tides: func() (tideProvider, string, error) { return provider, "bom", nil },
	}

	_, err := deps.execute(context.Background(), "get_tides", json.RawMessage(`{"lat":-27.4,"lon":153.1}`))
	if err == nil {
		t.Fatalf("expected an error when the provider has no stations")
	}
	if !strings.Contains(err.Error(), "no stations") {
		t.Errorf("expected the error to mention no stations, got %q", err.Error())
	}
}

func TestExecuteGetTides_EmptyWindowAddsNote(t *testing.T) {
	now := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	station := tideStation{StationID: "S1", Name: "Somewhere", Lat: -27.0, Lon: 153.0, Timezone: "Australia/Brisbane"}
	provider := &stubTideProvider{
		id:       "bom",
		stations: []tideStation{station},
		result:   tideChartResult{Extremes: nil},
	}
	deps := assistantToolDeps{
		now:   func() time.Time { return now },
		tides: func() (tideProvider, string, error) { return provider, "bom", nil },
	}

	raw, err := deps.execute(context.Background(), "get_tides", json.RawMessage(`{"lat":-27.0,"lon":153.0}`))
	if err != nil {
		t.Fatalf("execute get_tides: %v", err)
	}
	var result assistantGetTidesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(result.Note, "BOM gives about 8 days") {
		t.Errorf("expected the empty-window note, got %q", result.Note)
	}
}

// ── estimate_passage ────────────────────────────────────────────────────

func TestFuelRateInstancesFromSnapshot_DiscoversInstanceNames(t *testing.T) {
	withGlobalSnapshot(t, snapshotWithSelfValues(map[string]any{
		"propulsion.port.fuel.rate":      1e-05,
		"propulsion.starboard.fuel.rate": 1e-05,
	}))

	got := fuelRateInstancesFromSnapshot()
	if len(got) != 2 || got[0] != "port" || got[1] != "starboard" {
		t.Fatalf("expected [port starboard], got %v", got)
	}
}

func TestFuelRateInstancesFromSnapshot_NoTreeIsNil(t *testing.T) {
	withGlobalSnapshot(t, newSignalKSnapshot())

	if got := fuelRateInstancesFromSnapshot(); got != nil {
		t.Fatalf("expected nil when the snapshot has no self tree, got %v", got)
	}
}

// stubInfluxRangeByPath returns an injectable influxRange func backed by a
// fixed map of path -> series, for estimate_passage tests that need no real
// InfluxDB connection at all. A path with no entry returns an empty series
// (not an error) - queryInfluxPathRange's own "no data" contract.
func stubInfluxRangeByPath(series map[string][]telemetryPoint) func(path string, start, stop time.Time, every string) ([]telemetryPoint, error) {
	return func(path string, start, stop time.Time, every string) ([]telemetryPoint, error) {
		return series[path], nil
	}
}

// singleBandPerformanceSeries builds a fixed three-sample series for sog,
// port and starboard fuel rate, and port revolutions that buildPerformanceTable
// turns into exactly one surviving band: 5.0 m/s (~9.7kts, band 9),
// 0.000013 m3/s per engine (93.6 L/h summed), 30.25 Hz (1815 rpm).
func singleBandPerformanceSeries() map[string][]telemetryPoint {
	points := []telemetryPoint{
		{Timestamp: time.Unix(0, 0).UTC(), Value: 0},
		{Timestamp: time.Unix(600, 0).UTC(), Value: 0},
		{Timestamp: time.Unix(1200, 0).UTC(), Value: 0},
	}
	sog := make([]telemetryPoint, len(points))
	fuel := make([]telemetryPoint, len(points))
	rpm := make([]telemetryPoint, len(points))
	for i, p := range points {
		sog[i] = telemetryPoint{Timestamp: p.Timestamp, Value: 5.0}
		fuel[i] = telemetryPoint{Timestamp: p.Timestamp, Value: 0.000013}
		rpm[i] = telemetryPoint{Timestamp: p.Timestamp, Value: 30.25}
	}
	return map[string][]telemetryPoint{
		"navigation.speedOverGround":     sog,
		"propulsion.port.fuel.rate":      fuel,
		"propulsion.starboard.fuel.rate": fuel,
		"propulsion.port.revolutions":    rpm,
	}
}

func TestExecuteEstimatePassage_RequestedSpeed(t *testing.T) {
	deps := assistantToolDeps{
		now:               func() time.Time { return time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC) },
		influxRange:       stubInfluxRangeByPath(singleBandPerformanceSeries()),
		fuelRateInstances: func() []string { return []string{"port", "starboard"} },
	}

	raw, err := deps.execute(context.Background(), "estimate_passage", json.RawMessage(`{"distance_nm":20,"speed_kts":10}`))
	if err != nil {
		t.Fatalf("execute estimate_passage: %v", err)
	}

	var result assistantEstimatePassageResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}

	if result.DistanceNm != 20 {
		t.Errorf("expected distance_nm=20, got %v", result.DistanceNm)
	}
	if result.SpeedKts != 10 {
		t.Errorf("expected speed_kts=10, got %v", result.SpeedKts)
	}
	if result.SpeedSource != "requested" {
		t.Errorf("expected speed_source=requested, got %q", result.SpeedSource)
	}
	if result.Hours != 2.0 {
		t.Errorf("expected hours=2.0 (20nm at 10kts), got %v", result.Hours)
	}
	if result.Litres != 187 {
		t.Errorf("expected litres=187 (2h at 93.6 L/h, rounded), got %v", result.Litres)
	}
	if result.LPerH != 93.6 {
		t.Errorf("expected l_per_h=93.6 (two engines summed), got %v", result.LPerH)
	}
	if result.RPM != 1815 {
		t.Errorf("expected rpm=1815, got %v", result.RPM)
	}
	if result.HistoryDays != 90 {
		t.Errorf("expected the default history_days=90, got %d", result.HistoryDays)
	}
	if result.Samples != 3 {
		t.Errorf("expected samples=3, got %d", result.Samples)
	}
	if len(result.Instances) != 2 || result.Instances[0] != "port" || result.Instances[1] != "starboard" {
		t.Errorf("expected instances=[port starboard], got %v", result.Instances)
	}
	if result.InstancesAssumed {
		t.Errorf("expected instances_assumed=false when the snapshot named the instances")
	}
	if len(result.Table) != 1 || result.Table[0].SOGKtsMin != 9 {
		t.Errorf("expected a single band 9 in the table, got %+v", result.Table)
	}
	if !strings.Contains(result.Note, "head seas add time and fuel") {
		t.Errorf("expected the observed-conditions note, got %q", result.Note)
	}
}

func TestExecuteEstimatePassage_OmittedSpeedUsesMostSampledBand(t *testing.T) {
	deps := assistantToolDeps{
		now:               func() time.Time { return time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC) },
		influxRange:       stubInfluxRangeByPath(singleBandPerformanceSeries()),
		fuelRateInstances: func() []string { return []string{"port", "starboard"} },
	}

	// 97nm at the single band's 9.7kts mean is exactly 10.0 hours, chosen so
	// the expected value is exact rather than needing its own rounding.
	raw, err := deps.execute(context.Background(), "estimate_passage", json.RawMessage(`{"distance_nm":97}`))
	if err != nil {
		t.Fatalf("execute estimate_passage: %v", err)
	}

	var result assistantEstimatePassageResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}

	if result.SpeedSource != "most_sampled_band" {
		t.Errorf("expected speed_source=most_sampled_band, got %q", result.SpeedSource)
	}
	if result.SpeedKts != 9.7 {
		t.Errorf("expected speed_kts=9.7 (the single band's mean), got %v", result.SpeedKts)
	}
	if result.Hours != 10.0 {
		t.Errorf("expected hours=10.0, got %v", result.Hours)
	}
	if result.Litres != 936 {
		t.Errorf("expected litres=936 (10h at 93.6 L/h), got %v", result.Litres)
	}
}

func TestExecuteEstimatePassage_NoUnderwayHistoryIsError(t *testing.T) {
	deps := assistantToolDeps{
		now:               func() time.Time { return time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC) },
		influxRange:       stubInfluxRangeByPath(map[string][]telemetryPoint{}), // every path comes back empty
		fuelRateInstances: func() []string { return []string{"port", "starboard"} },
	}

	_, err := deps.execute(context.Background(), "estimate_passage", json.RawMessage(`{"distance_nm":20,"days":30}`))
	if err == nil {
		t.Fatalf("expected an error when there is no underway history in the window")
	}
	if !strings.Contains(err.Error(), "no underway history in the last 30 days") {
		t.Errorf("expected the error to name the window, got %q", err.Error())
	}
}

func TestExecuteEstimatePassage_UnknownInstancesFallsBackAndFlagsAssumed(t *testing.T) {
	deps := assistantToolDeps{
		now:               func() time.Time { return time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC) },
		influxRange:       stubInfluxRangeByPath(singleBandPerformanceSeries()),
		fuelRateInstances: func() []string { return nil }, // snapshot has no propulsion tree yet
	}

	raw, err := deps.execute(context.Background(), "estimate_passage", json.RawMessage(`{"distance_nm":20,"speed_kts":10}`))
	if err != nil {
		t.Fatalf("execute estimate_passage: %v", err)
	}

	var result assistantEstimatePassageResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	if !result.InstancesAssumed {
		t.Errorf("expected instances_assumed=true when the snapshot named no instances")
	}
	if len(result.Instances) != 2 || result.Instances[0] != "port" || result.Instances[1] != "starboard" {
		t.Errorf("expected the port/starboard fallback, got %v", result.Instances)
	}
	// The fallback names must actually be the ones queried, not just
	// reported: singleBandPerformanceSeries only has data under
	// propulsion.port.* and propulsion.starboard.*, so a wrong guess would
	// have produced the empty-table error instead of a result.
	if len(result.Table) == 0 {
		t.Fatalf("expected a non-empty table, proving the fallback names were actually queried")
	}
}

func TestExecuteEstimatePassage_FuelAboardKnownAddsMargin(t *testing.T) {
	deps := assistantToolDeps{
		now:               func() time.Time { return time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC) },
		influxRange:       stubInfluxRangeByPath(singleBandPerformanceSeries()),
		fuelRateInstances: func() []string { return []string{"port", "starboard"} },
		fuelAboardM3:      func() (float64, bool) { return 0.6, true }, // 600 L aboard
	}

	raw, err := deps.execute(context.Background(), "estimate_passage", json.RawMessage(`{"distance_nm":20,"speed_kts":10}`))
	if err != nil {
		t.Fatalf("execute estimate_passage: %v", err)
	}

	var result assistantEstimatePassageResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}

	if result.FuelAboardL == nil || *result.FuelAboardL != 600 {
		t.Fatalf("expected fuel_aboard_l=600 (0.6 m3), got %v", result.FuelAboardL)
	}
	// litres=187 (asserted in TestExecuteEstimatePassage_RequestedSpeed for
	// the same inputs), so fuel_after_l = 600 - 187 = 413.
	if result.FuelAfterL == nil || *result.FuelAfterL != 600-result.Litres {
		t.Fatalf("expected fuel_after_l=aboard-litres (%v), got %v", 600-result.Litres, result.FuelAfterL)
	}
	if strings.Contains(result.Note, "fuel aboard unknown") {
		t.Errorf("expected no 'fuel aboard unknown' note when fuel aboard is known, got %q", result.Note)
	}
}

func TestExecuteEstimatePassage_FuelAboardUnknownOmitsFieldsAndNotesIt(t *testing.T) {
	deps := assistantToolDeps{
		now:               func() time.Time { return time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC) },
		influxRange:       stubInfluxRangeByPath(singleBandPerformanceSeries()),
		fuelRateInstances: func() []string { return []string{"port", "starboard"} },
		fuelAboardM3:      func() (float64, bool) { return 0, false },
	}

	raw, err := deps.execute(context.Background(), "estimate_passage", json.RawMessage(`{"distance_nm":20,"speed_kts":10}`))
	if err != nil {
		t.Fatalf("execute estimate_passage: %v", err)
	}

	// Check the raw JSON, not just the decoded struct, so an accidental
	// "fuel_aboard_l":0 (rather than a genuinely absent key) would fail
	// this test - omitempty on a nil *float64 must drop the key entirely.
	var raw2 map[string]any
	if err := json.Unmarshal([]byte(raw), &raw2); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, present := raw2["fuel_aboard_l"]; present {
		t.Errorf("expected fuel_aboard_l to be absent from the JSON, got %v", raw2["fuel_aboard_l"])
	}
	if _, present := raw2["fuel_after_l"]; present {
		t.Errorf("expected fuel_after_l to be absent from the JSON, got %v", raw2["fuel_after_l"])
	}

	var result assistantEstimatePassageResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	if result.FuelAboardL != nil || result.FuelAfterL != nil {
		t.Fatalf("expected both fuel fields nil, got aboard=%v after=%v", result.FuelAboardL, result.FuelAfterL)
	}
	if !strings.Contains(result.Note, "fuel aboard unknown") {
		t.Errorf("expected the note to mention fuel aboard is unknown, got %q", result.Note)
	}
}

func TestExecuteEstimatePassage_DistanceRequired(t *testing.T) {
	deps := assistantToolDeps{now: time.Now}
	_, err := deps.execute(context.Background(), "estimate_passage", json.RawMessage(`{"distance_nm":0}`))
	if err == nil {
		t.Fatalf("expected an error when distance_nm is missing/zero")
	}
}

func TestExecuteEstimatePassage_NonPositiveSpeedIsError(t *testing.T) {
	deps := assistantToolDeps{now: time.Now}
	_, err := deps.execute(context.Background(), "estimate_passage", json.RawMessage(`{"distance_nm":20,"speed_kts":0}`))
	if err == nil {
		t.Fatalf("expected an error for a non-positive speed_kts")
	}
}

// ── describeAssistantToolCall ───────────────────────────────────────────

func TestDescribeAssistantToolCall(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		{"find_places", `{"query":"Tongue Bay"}`, "Looking up Tongue Bay…"},
		{"get_wind_forecast", `{"lat":-20.1,"lon":149.1,"name":"Blue Pearl Bay"}`, "Fetching wind forecast for Blue Pearl Bay…"},
		{"get_wind_forecast", `{"lat":-20.1234,"lon":149.5678}`, "Fetching wind forecast for -20.1234,149.5678…"},
		{"get_tides", `{"lat":-20.1,"lon":149.1,"name":"Blue Pearl Bay"}`, "Fetching tides near Blue Pearl Bay…"},
		{"estimate_passage", `{"distance_nm":42,"speed_kts":8.5}`, "Estimating 42 nm at 8.5 kts from the log…"},
		{"estimate_passage", `{"distance_nm":42}`, "Estimating 42 nm at cruising speed from the log…"},
	}
	for _, tc := range cases {
		got := describeAssistantToolCall(tc.name, json.RawMessage(tc.args))
		if got != tc.want {
			t.Errorf("describeAssistantToolCall(%q, %s) = %q, want %q", tc.name, tc.args, got, tc.want)
		}
	}
}

func TestAssistantToolDefinitions_FindPlacesDescriptionMentionsBareFeatureName(t *testing.T) {
	for _, tool := range assistantToolDefinitions() {
		if tool.Function.Name != "find_places" {
			continue
		}
		if !strings.Contains(tool.Function.Description, "bare feature name") {
			t.Fatalf("expected the find_places description to mention %q, got: %s", "bare feature name", tool.Function.Description)
		}
		return
	}
	t.Fatal("find_places tool definition not found")
}

func TestAssistantToolDefinitions_FourToolsIncludingEstimatePassage(t *testing.T) {
	tools := assistantToolDefinitions()
	if len(tools) != 4 {
		t.Fatalf("expected 4 tool definitions, got %d: %+v", len(tools), tools)
	}

	var names []string
	for _, tool := range tools {
		names = append(names, tool.Function.Name)
	}
	for _, want := range []string{"find_places", "get_wind_forecast", "get_tides", "estimate_passage"} {
		found := false
		for _, name := range names {
			if name == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected a %q tool definition, got %v", want, names)
		}
	}
}

// ── dispatch / truncation ───────────────────────────────────────────────

func TestExecute_UnknownToolIsError(t *testing.T) {
	deps := assistantToolDeps{}
	_, err := deps.execute(context.Background(), "not_a_real_tool", json.RawMessage(`{}`))
	if err == nil {
		t.Fatalf("expected an error for an unknown tool name")
	}
}

func TestCapToolResultJSON_TruncatesAtTheCap(t *testing.T) {
	type item struct {
		Filler string `json:"filler"`
	}
	type payload struct {
		Items     []item `json:"items"`
		Truncated bool   `json:"truncated,omitempty"`
	}

	filler := strings.Repeat("x", 500)
	items := make([]item, 40) // ~40 * 510 bytes, comfortably over the 12000-char cap
	for i := range items {
		items[i] = item{Filler: filler}
	}
	result := payload{Items: items}

	shrinkCalls := 0
	shrink := func() bool {
		if len(result.Items) == 0 {
			return false
		}
		result.Items = result.Items[:len(result.Items)-1]
		result.Truncated = true
		shrinkCalls++
		return true
	}

	raw, err := capToolResultJSON(&result, shrink)
	if err != nil {
		t.Fatalf("capToolResultJSON: %v", err)
	}
	if len(raw) > assistantMaxToolResultChars {
		t.Fatalf("expected the result to fit within %d chars, got %d", assistantMaxToolResultChars, len(raw))
	}
	if shrinkCalls == 0 {
		t.Fatalf("expected shrink to be called at least once")
	}

	var decoded payload
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("expected valid JSON after truncation, got error: %v (raw: %s)", err, raw)
	}
	if !decoded.Truncated {
		t.Fatalf("expected truncated=true in the trimmed result")
	}
	if len(decoded.Items) >= 40 {
		t.Fatalf("expected fewer items after truncation, got %d", len(decoded.Items))
	}
}

func TestCapToolResultJSON_FallsBackWhenNothingLeftToShrink(t *testing.T) {
	type payload struct {
		Filler string `json:"filler"`
	}
	result := payload{Filler: strings.Repeat("x", assistantMaxToolResultChars*2)}

	raw, err := capToolResultJSON(&result, nil)
	if err != nil {
		t.Fatalf("capToolResultJSON: %v", err)
	}
	if len(raw) > assistantMaxToolResultChars {
		t.Fatalf("expected the fallback to fit within the cap, got %d chars", len(raw))
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("expected valid JSON fallback, got error: %v", err)
	}
	if decoded["truncated"] != true {
		t.Fatalf("expected truncated=true in the fallback, got %v", decoded)
	}
}
