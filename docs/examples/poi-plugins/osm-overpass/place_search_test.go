package main

// place_search_test.go covers the two OPTIONAL exports this plugin adds on
// top of fetch_poi: place_name_at (ports backend/place_name.go's ring query
// and ranking) and search_places (ports backend/assistant_tools.go's
// find_places two-rung ladder). See osm-overpass.go's "place_name_at" and
// "search_places" sections for the implementation these tests exercise, and
// this plugin's README for the contract both exports promise the host.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// ── place_name_at: query building ──────────────────────────────────────

func TestBuildPlaceNameQuery_EmbedsRadiusPositionAndTimeout(t *testing.T) {
	q := buildPlaceNameQuery(-20.4467, 149.0353, 1500)

	if !strings.Contains(q, "[timeout:12]") {
		t.Fatalf("expected a 12s server-side timeout, got: %s", q)
	}
	if !strings.Contains(q, `nwr["seamark:type"="anchorage"](around:1500,-20.446700,149.035300)`) {
		t.Fatalf("expected the anchorage clause with the requested radius/position, got: %s", q)
	}
	if !strings.Contains(q, `nwr["natural"="bay"](around:1500,-20.446700,149.035300)`) {
		t.Fatalf("expected the bay clause, got: %s", q)
	}
	if !strings.Contains(q, `nwr["place"~"^(island|islet|rock)$"](around:1500,-20.446700,149.035300)`) {
		t.Fatalf("expected the island/islet/rock clause, got: %s", q)
	}
	if !strings.Contains(q, "out tags center 20;") {
		t.Fatalf("expected the same cap (20) backend/place_name.go uses, got: %s", q)
	}
}

func TestBuildPlaceNameQuery_CarriesGlobalBboxWhenAvailable(t *testing.T) {
	q := buildPlaceNameQuery(-20.4467, 149.0353, 1500)
	south, west, north, east, ok := overpassBoundingBox(-20.4467, 149.0353, 1500)
	if !ok {
		t.Fatalf("expected overpassBoundingBox to succeed for this position/radius")
	}
	want := fmt.Sprintf("[bbox:%.6f,%.6f,%.6f,%.6f]", south, west, north, east)
	if !strings.Contains(q, want) {
		t.Fatalf("expected the query to carry the global bbox %q, got: %s", want, q)
	}
}

// ── place_name_at: ranking (ported from backend/place_name_test.go) ─────

func TestBestNamedPlaceFeature_RanksAnchorageOverBay(t *testing.T) {
	elements := []overpassElement{
		{Type: "node", ID: 1, Lat: f64p(-20.44), Lon: f64p(149.03), Tags: map[string]string{"name": "Some Bay", "natural": "bay"}},
		{Type: "node", ID: 2, Lat: f64p(-20.45), Lon: f64p(149.04), Tags: map[string]string{"name": "Some Anchorage", "seamark:type": "anchorage"}},
	}
	winner, ok := bestNamedPlaceFeature(elements, -20.4467, 149.0353)
	if !ok {
		t.Fatalf("expected a winner")
	}
	if winner.Name != "Some Anchorage" || winner.Kind != "anchorage" {
		t.Fatalf("expected the anchorage to win over the bay, got %+v", winner)
	}
}

func TestBestNamedPlaceFeature_TiesBrokenByDistance(t *testing.T) {
	lat, lon := -20.4467, 149.0353
	elements := []overpassElement{
		{Type: "node", ID: 1, Lat: f64p(lat + 0.01), Lon: f64p(lon + 0.01), Tags: map[string]string{"name": "Far Island", "place": "island"}},
		{Type: "node", ID: 2, Lat: f64p(lat + 0.001), Lon: f64p(lon + 0.001), Tags: map[string]string{"name": "Near Island", "place": "island"}},
	}
	winner, ok := bestNamedPlaceFeature(elements, lat, lon)
	if !ok {
		t.Fatalf("expected a winner")
	}
	if winner.Name != "Near Island" {
		t.Fatalf("expected the nearer of two same-rank islands to win, got %+v", winner)
	}
}

func TestBestNamedPlaceFeature_UnnamedNeverWins(t *testing.T) {
	elements := []overpassElement{
		{Type: "node", ID: 1, Lat: f64p(-20.4467), Lon: f64p(149.0353), Tags: map[string]string{"place": "islet"}},
	}
	if _, ok := bestNamedPlaceFeature(elements, -20.4467, 149.0353); ok {
		t.Fatalf("expected an unnamed element to never win")
	}
}

func TestBestNamedPlaceFeature_UnrecognisedKindDiscarded(t *testing.T) {
	elements := []overpassElement{
		{Type: "node", ID: 1, Lat: f64p(-20.4467), Lon: f64p(149.0353), Tags: map[string]string{"name": "Some Building", "building": "yes"}},
	}
	if _, ok := bestNamedPlaceFeature(elements, -20.4467, 149.0353); ok {
		t.Fatalf("expected an element matching none of the five recognised kinds to be discarded")
	}
}

func TestBestNamedPlaceFeature_NothingFoundIsAValidEmptyResult(t *testing.T) {
	if _, ok := bestNamedPlaceFeature(nil, -20.4467, 149.0353); ok {
		t.Fatalf("expected no elements to be a valid empty (not error) result")
	}
}

// ── place_name_at: runPlaceNameAt orchestration ──────────────────────────

func TestRunPlaceNameAt_ReturnsWinnerFields(t *testing.T) {
	post := func(query string) ([]overpassElement, error) {
		return []overpassElement{
			{Type: "node", ID: 1, Lat: f64p(-20.447), Lon: f64p(149.036), Tags: map[string]string{"name": "Lindeman Island", "place": "island"}},
		}, nil
	}
	out, err := runPlaceNameAt(post, -20.4467, 149.0353, 1500)
	if err != nil {
		t.Fatalf("runPlaceNameAt: %v", err)
	}
	if out.Name != "Lindeman Island" || out.Kind != "island" {
		t.Fatalf("expected the named winner, got %+v", out)
	}
	if out.Lat == nil || out.Lon == nil {
		t.Fatalf("expected lat/lon to be set for a found winner, got %+v", out)
	}
}

func TestRunPlaceNameAt_NothingNamedIsExactlyNameEmptyString(t *testing.T) {
	post := func(query string) ([]overpassElement, error) { return nil, nil }
	out, err := runPlaceNameAt(post, -20.4467, 149.0353, 1500)
	if err != nil {
		t.Fatalf("runPlaceNameAt: %v", err)
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `{"name":""}` {
		t.Fatalf(`expected exactly {"name":""} for nothing found, got: %s`, data)
	}
}

func TestRunPlaceNameAt_PostErrorSurfacesNeverMasked(t *testing.T) {
	post := func(query string) ([]overpassElement, error) { return nil, fmt.Errorf("overpass rate limited") }
	_, err := runPlaceNameAt(post, -20.4467, 149.0353, 1500)
	if err == nil {
		t.Fatalf("expected the post error to surface, not be masked as an empty success")
	}
}

func f64p(v float64) *float64 { return &v }

// ── search_places: variants and kind labels (ported from
// backend/assistant_tools_test.go) ────────────────────────────────────────

func TestFindPlacesExactNameVariants_CommaQualifierIncludesBareHead(t *testing.T) {
	variants := findPlacesExactNameVariants("Bona Bay, Gloucester Island")
	want := "Bona Bay"
	found := false
	for _, v := range variants {
		if v == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected variants for %q to include the bare head %q, got %v", "Bona Bay, Gloucester Island", want, variants)
	}
}

func TestFindPlacesExactNameVariants_CoversCapitalisationConventions(t *testing.T) {
	variants := findPlacesExactNameVariants("bona bay")
	for _, want := range []string{"bona bay", "Bona Bay", "Bona bay"} {
		found := false
		for _, v := range variants {
			if v == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected variants for %q to include %q, got %v", "bona bay", want, variants)
		}
	}
}

func TestFindPlacesQualifier(t *testing.T) {
	if got := findPlacesQualifier("Bona Bay, Gloucester Island"); got != "Gloucester Island" {
		t.Fatalf("expected qualifier %q, got %q", "Gloucester Island", got)
	}
	if got := findPlacesQualifier("Bona Bay"); got != "" {
		t.Fatalf("expected no qualifier for a query with no comma, got %q", got)
	}
}

func TestFindPlacesKind(t *testing.T) {
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
			if got := findPlacesKind(tc.tags); got != tc.want {
				t.Errorf("findPlacesKind(%v) = %q, want %q", tc.tags, got, tc.want)
			}
		})
	}
}

// ── search_places: query shapes (ported from
// backend/assistant_tools_test.go) ────────────────────────────────────────

func TestBuildFindPlacesExactQuery_VariantsNoAroundNoTagFilter(t *testing.T) {
	south, west, north, east := searchPlacesBoundingBox(-20.1, 149.1, searchPlacesExactRadiusNm)
	got := buildFindPlacesExactQuery("bona bay", south, west, north, east, searchPlacesExactTimeoutSeconds)

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
	if !strings.Contains(got, fmt.Sprintf("[timeout:%d]", searchPlacesExactTimeoutSeconds)) {
		t.Errorf("expected the exact-rung timeout %d, got: %s", searchPlacesExactTimeoutSeconds, got)
	}
}

func TestBuildFindPlacesRegexQuery_EscapesRegexMetacharacters(t *testing.T) {
	south, west, north, east := searchPlacesBoundingBox(-20.1, 149.1, searchPlacesRegexRadiusNm)
	got := buildFindPlacesRegexQuery("Hook Reef (outer)", south, west, north, east, searchPlacesRegexTimeoutSeconds)

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
	if !strings.Contains(got, fmt.Sprintf("[timeout:%d]", searchPlacesRegexTimeoutSeconds)) {
		t.Fatalf("expected the regex-rung timeout %d, got: %s", searchPlacesRegexTimeoutSeconds, got)
	}
}

func TestSearchPlacesTimeouts_SumStaysUnderTheHostBudget(t *testing.T) {
	const hostBudgetSeconds = 15
	sum := searchPlacesExactTimeoutSeconds + searchPlacesQualifierTimeoutSeconds + searchPlacesRegexTimeoutSeconds
	if sum >= hostBudgetSeconds {
		t.Fatalf("expected the worst-case sum of all three possible round trips (%ds) to stay under the host's %ds WASM plugin call budget, leaving margin for connection overhead", sum, hostBudgetSeconds)
	}
}

// ── search_places: runSearchPlaces orchestration ─────────────────────────

// sequentialSearchPoster is an injectable overpassQueryFunc for
// runSearchPlaces tests, returning one canned response per call in order -
// mirrors backend/assistant_tools_test.go's sequentialOverpassFetcher.
type sequentialSearchPoster struct {
	responses [][]overpassElement
	errs      []error
	queries   []string
}

func (p *sequentialSearchPoster) post(query string) ([]overpassElement, error) {
	i := len(p.queries)
	p.queries = append(p.queries, query)
	var err error
	if i < len(p.errs) {
		err = p.errs[i]
	}
	if err != nil {
		return nil, err
	}
	if i < len(p.responses) {
		return p.responses[i], nil
	}
	return nil, nil
}

func TestRunSearchPlaces_Rung1ExactHitSkipsRung2(t *testing.T) {
	poster := &sequentialSearchPoster{
		responses: [][]overpassElement{
			{{Type: "node", ID: 1, Lat: f64p(-20.1), Lon: f64p(149.1), Tags: map[string]string{"name": "Shag Cove", "natural": "bay"}}},
		},
	}
	out, err := runSearchPlaces(poster.post, searchPlacesInput{Query: "Shag Cove", Lat: -20.15, Lon: 149.12, Broad: true})
	if err != nil {
		t.Fatalf("runSearchPlaces: %v", err)
	}
	if len(poster.queries) != 1 {
		t.Fatalf("expected rung 2 to be skipped once rung 1 already found a match, got %d overpass calls", len(poster.queries))
	}
	if out.Search != "exact" {
		t.Fatalf("expected search=exact, got %q", out.Search)
	}
	if out.RadiusNm != searchPlacesExactRadiusNm {
		t.Fatalf("expected radius_nm=%g (rung 1), got %v", searchPlacesExactRadiusNm, out.RadiusNm)
	}
	if len(out.Results) != 1 || out.Results[0].Name != "Shag Cove" {
		t.Fatalf("expected the rung 1 hit, got %+v", out.Results)
	}
	if out.CentredOn != nil {
		t.Fatalf("expected no centred_on when no qualifier ran, got %+v", out.CentredOn)
	}
}

func TestRunSearchPlaces_Rung2RunsOnlyWhenRung1EmptyAndBroad(t *testing.T) {
	poster := &sequentialSearchPoster{
		responses: [][]overpassElement{
			{}, // rung 1: exact match, nothing
			{{Type: "node", ID: 2, Lat: f64p(-20.15), Lon: f64p(149.12), Tags: map[string]string{"name": "Tongue Point", "natural": "cape"}}}, // rung 2: partial match hit
		},
	}
	out, err := runSearchPlaces(poster.post, searchPlacesInput{Query: "Tongue", Lat: -20.15, Lon: 149.12, Broad: true})
	if err != nil {
		t.Fatalf("runSearchPlaces: %v", err)
	}
	if len(poster.queries) != 2 {
		t.Fatalf("expected both rungs queried when rung 1 comes back empty, got %d calls", len(poster.queries))
	}
	if out.Search != "regex" {
		t.Fatalf("expected search=regex, got %q", out.Search)
	}
	if out.RadiusNm != searchPlacesRegexRadiusNm {
		t.Fatalf("expected radius_nm=%g (rung 2), got %v", searchPlacesRegexRadiusNm, out.RadiusNm)
	}
	if len(out.Results) != 1 || out.Results[0].Name != "Tongue Point" {
		t.Fatalf("expected the rung 2 hit, got %+v", out.Results)
	}
}

func TestRunSearchPlaces_NotBroadSkipsRung2EvenWhenRung1Empty(t *testing.T) {
	poster := &sequentialSearchPoster{responses: [][]overpassElement{{}}}
	out, err := runSearchPlaces(poster.post, searchPlacesInput{Query: "Tongue", Lat: -20.15, Lon: 149.12, Broad: false})
	if err != nil {
		t.Fatalf("runSearchPlaces: %v", err)
	}
	if len(poster.queries) != 1 {
		t.Fatalf("expected rung 2 to never run when broad=false, got %d calls", len(poster.queries))
	}
	if out.Search != "none" {
		t.Fatalf("expected search=none, got %q", out.Search)
	}
	if out.RadiusNm != searchPlacesExactRadiusNm {
		t.Fatalf("expected radius_nm to stay at rung 1's radius when rung 2 never ran, got %v", out.RadiusNm)
	}
	if len(out.Results) != 0 {
		t.Fatalf("expected no results, got %+v", out.Results)
	}
}

func TestRunSearchPlaces_QualifierResolvesCentresRung2AndReportsCentredOn(t *testing.T) {
	vesselLat, vesselLon := -20.1, 149.1
	qualifierLat, qualifierLon := -19.8, 148.5 // well outside rung 2's 20nm radius from the vessel

	poster := &sequentialSearchPoster{
		responses: [][]overpassElement{
			{}, // rung 1: nothing
			{{Type: "node", ID: 1, Lat: f64p(qualifierLat), Lon: f64p(qualifierLon), Tags: map[string]string{"name": "Gloucester Island", "place": "island"}}}, // qualifier lookup: one hit
			{}, // rung 2: nothing (not the point of this test)
		},
	}
	out, err := runSearchPlaces(poster.post, searchPlacesInput{Query: "Bona Bay, Gloucester Island", Lat: vesselLat, Lon: vesselLon, Broad: true})
	if err != nil {
		t.Fatalf("runSearchPlaces: %v", err)
	}
	if len(poster.queries) != 3 {
		t.Fatalf("expected 3 overpass calls (rung 1, qualifier lookup, rung 2), got %d", len(poster.queries))
	}
	if !strings.Contains(poster.queries[1], `["name"="Gloucester Island"]`) {
		t.Errorf("expected the qualifier lookup to search the exact qualifier name, got: %s", poster.queries[1])
	}
	wantSouth, wantWest, wantNorth, wantEast := searchPlacesBoundingBox(qualifierLat, qualifierLon, searchPlacesRegexRadiusNm)
	wantBbox := searchPlacesBBoxClause(wantSouth, wantWest, wantNorth, wantEast)
	if !strings.Contains(poster.queries[2], wantBbox) {
		t.Errorf("expected rung 2's bbox to be centred on the qualifier hit (%s), got: %s", wantBbox, poster.queries[2])
	}
	if out.CentredOn == nil || out.CentredOn.Name != "Gloucester Island" {
		t.Fatalf("expected centred_on to name the qualifier hit, got %+v", out.CentredOn)
	}
	if out.CentredOn.Lat != qualifierLat || out.CentredOn.Lon != qualifierLon {
		t.Fatalf("expected centred_on coordinates to match the qualifier hit, got %+v", out.CentredOn)
	}
	if out.Note != "" {
		t.Fatalf("expected no note when the qualifier resolved, got %q", out.Note)
	}
}

func TestRunSearchPlaces_QualifierDoesNotResolveNotesItAndFallsBackToGivenCentre(t *testing.T) {
	vesselLat, vesselLon := -20.1, 149.1
	poster := &sequentialSearchPoster{
		responses: [][]overpassElement{
			{}, // rung 1: nothing
			{}, // qualifier lookup: nothing resolves
			{}, // rung 2: nothing
		},
	}
	out, err := runSearchPlaces(poster.post, searchPlacesInput{Query: "Bona Bay, Gloucester Island", Lat: vesselLat, Lon: vesselLon, Broad: true})
	if err != nil {
		t.Fatalf("runSearchPlaces: %v", err)
	}
	wantSouth, wantWest, wantNorth, wantEast := searchPlacesBoundingBox(vesselLat, vesselLon, searchPlacesRegexRadiusNm)
	wantBbox := searchPlacesBBoxClause(wantSouth, wantWest, wantNorth, wantEast)
	if !strings.Contains(poster.queries[2], wantBbox) {
		t.Errorf("expected rung 2's bbox to fall back to the given centre (%s), got: %s", wantBbox, poster.queries[2])
	}
	if out.CentredOn != nil {
		t.Fatalf("expected no centred_on when the qualifier did not resolve, got %+v", out.CentredOn)
	}
	if out.Note == "" {
		t.Fatalf("expected a note explaining the qualifier did not resolve")
	}
}

func TestRunSearchPlaces_ResultCapAt50(t *testing.T) {
	var elements []overpassElement
	for i := 0; i < 80; i++ {
		lat, lon := -20.1+float64(i)*0.001, 149.1
		elements = append(elements, overpassElement{Type: "node", ID: int64(i), Lat: &lat, Lon: &lon, Tags: map[string]string{"name": fmt.Sprintf("Feature %d", i), "natural": "bay"}})
	}
	poster := &sequentialSearchPoster{responses: [][]overpassElement{elements}}
	out, err := runSearchPlaces(poster.post, searchPlacesInput{Query: "Feature", Lat: -20.1, Lon: 149.1, Broad: true})
	if err != nil {
		t.Fatalf("runSearchPlaces: %v", err)
	}
	if len(out.Results) != searchPlacesResultCap {
		t.Fatalf("expected results capped at %d, got %d", searchPlacesResultCap, len(out.Results))
	}
}

func TestRunSearchPlaces_ResultsIsNeverNullJSON(t *testing.T) {
	poster := &sequentialSearchPoster{responses: [][]overpassElement{{}}}
	out, err := runSearchPlaces(poster.post, searchPlacesInput{Query: "Nothing Here", Lat: -20.1, Lon: 149.1, Broad: false})
	if err != nil {
		t.Fatalf("runSearchPlaces: %v", err)
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"results":[]`) {
		t.Fatalf(`expected "results":[] not null, got: %s`, data)
	}
	if !strings.Contains(string(data), `"centred_on":null`) {
		t.Fatalf(`expected "centred_on":null, got: %s`, data)
	}
}

func TestRunSearchPlaces_TransportErrorIsNeverSwallowed(t *testing.T) {
	poster := &sequentialSearchPoster{errs: []error{fmt.Errorf("transport failed")}}
	_, err := runSearchPlaces(poster.post, searchPlacesInput{Query: "Bay", Lat: -20.1, Lon: 149.1, Broad: true})
	if err == nil {
		t.Fatalf("expected rung 1's transport error to surface, not be swallowed")
	}
}

func TestRunSearchPlaces_Rung2TransportErrorIsNeverSwallowed(t *testing.T) {
	poster := &sequentialSearchPoster{
		responses: [][]overpassElement{{}},
		errs:      []error{nil, fmt.Errorf("transport failed")},
	}
	_, err := runSearchPlaces(poster.post, searchPlacesInput{Query: "Bay", Lat: -20.1, Lon: 149.1, Broad: true})
	if err == nil {
		t.Fatalf("expected rung 2's transport error to surface, not be swallowed")
	}
}

func TestRunSearchPlaces_EmptyQueryIsError(t *testing.T) {
	poster := &sequentialSearchPoster{}
	_, err := runSearchPlaces(poster.post, searchPlacesInput{Query: "  ", Lat: -20.1, Lon: 149.1})
	if err == nil {
		t.Fatalf("expected a blank query to be an error")
	}
}
