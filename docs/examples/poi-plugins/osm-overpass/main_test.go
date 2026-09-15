package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

// ── query building ───────────────────────────────────────────────────────

func TestBuildOverpassPOIQuery_FixedCategoryOrderRegardlessOfInputOrder(t *testing.T) {
	q1 := buildOverpassPOIQuery(-20.4467, 149.0353, 9260, []string{"trail", "anchorage", "bay"})
	q2 := buildOverpassPOIQuery(-20.4467, 149.0353, 9260, []string{"bay", "trail", "anchorage"})
	if q1 != q2 {
		t.Fatalf("expected the same query text regardless of input category order:\n%s\n---\n%s", q1, q2)
	}

	anchorageIdx := strings.Index(q1, `["seamark:type"="anchorage"]`)
	bayIdx := strings.Index(q1, `["natural"="bay"]`)
	trailIdx := strings.Index(q1, `["route"="hiking"]`)
	if !(anchorageIdx < bayIdx && bayIdx < trailIdx) {
		t.Fatalf("expected anchorage before bay before trail in the built query, got indexes %d %d %d", anchorageIdx, bayIdx, trailIdx)
	}
}

func TestBuildOverpassPOIQuery_OnlyIncludesRequestedCategories(t *testing.T) {
	q := buildOverpassPOIQuery(-20.4467, 149.0353, 9260, []string{"anchorage"})
	if strings.Contains(q, "seamark:type\"=\"mooring") {
		t.Fatalf("expected an anchorage-only query to omit the mooring clause, got:\n%s", q)
	}
	if !strings.Contains(q, `out tags center 40;`) {
		t.Fatalf("expected the anchorage cap (40) in the query, got:\n%s", q)
	}
}

func TestBuildOverpassPOIQuery_UsesRequestedPositionAndRadius(t *testing.T) {
	q := buildOverpassPOIQuery(-20.2675, 148.7176, 5556, []string{"marina"})
	if !strings.Contains(q, "(around:5556,-20.267500,148.717600)") {
		t.Fatalf("expected the around: filter to carry the requested radius/position, got:\n%s", q)
	}
}

// The host (backend/wasm_plugin.go) aborts any plugin call after
// WASM_PLUGIN_TIMEOUT_MS (15s by default), so a server-side Overpass budget
// at or above that can never actually be used - it only makes Overpass keep
// working a query the host has already abandoned. This pins the
// server-side budget below that host ceiling.
func TestBuildOverpassPOIQuery_ServerSideTimeoutStaysUnderHostBudget(t *testing.T) {
	q := buildOverpassPOIQuery(-20.4467, 149.0353, 9260, []string{"anchorage"})
	if !strings.Contains(q, "[timeout:12]") {
		t.Fatalf("expected the query to request a 12s Overpass timeout, got:\n%s", q)
	}
	if strings.Contains(q, "[timeout:60]") {
		t.Fatalf("expected the query to no longer request a 60s Overpass timeout, got:\n%s", q)
	}
}

// ── bounding box ─────────────────────────────────────────────────────────
//
// overpass.openstreetmap.fr's planner scans by tag before applying a
// clause's own around: filter, which pushes the full 11-category query past
// the host's 15s WASM plugin budget (measured: 23s wall time, 0 elements,
// "Query timed out" at Lindeman Island). A global [bbox:...] setting on the
// query header gives the planner a spatial index to start from - measured
// 2.4s for the identical 23-element result at the same position. These
// tests pin the box's geometry, not its exact decimal text, since the box
// only needs to enclose the around: circle, not match it exactly.

var bboxPattern = regexp.MustCompile(`\[bbox:(-?[0-9.]+),(-?[0-9.]+),(-?[0-9.]+),(-?[0-9.]+)\]`)

// extractBBox parses the [bbox:S,W,N,E] setting out of a built query,
// failing the test if the query carries no such setting.
func extractBBox(t *testing.T, q string) (south, west, north, east float64) {
	t.Helper()
	m := bboxPattern.FindStringSubmatch(q)
	if m == nil {
		t.Fatalf("expected a [bbox:S,W,N,E] setting in the query, got:\n%s", q)
	}
	vals := make([]float64, 4)
	for i, s := range m[1:] {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			t.Fatalf("bad bbox number %q: %v", s, err)
		}
		vals[i] = v
	}
	return vals[0], vals[1], vals[2], vals[3]
}

func TestBuildOverpassPOIQuery_BBoxEnclosesTheAroundCircle(t *testing.T) {
	lat, lon, radiusM := -20.4467, 149.0353, 9260
	q := buildOverpassPOIQuery(lat, lon, radiusM, []string{"anchorage"})
	south, west, north, east := extractBBox(t, q)

	dlat := float64(radiusM) / 111320.0
	if !(south < lat-dlat) {
		t.Fatalf("expected south (%f) < lat-dlat (%f)", south, lat-dlat)
	}
	if !(north > lat+dlat) {
		t.Fatalf("expected north (%f) > lat+dlat (%f)", north, lat+dlat)
	}
	dlon := float64(radiusM) / (111320.0 * math.Cos(lat*math.Pi/180))
	if !(lon-west >= dlon) {
		t.Fatalf("expected west to extend at least %f deg from lon, got west=%f (lon-west=%f)", dlon, west, lon-west)
	}
	if !(east-lon >= dlon) {
		t.Fatalf("expected east to extend at least %f deg from lon, got east=%f (east-lon=%f)", dlon, east, east-lon)
	}
}

func TestBuildOverpassPOIQuery_BBoxAtHighLatitudeCoversPolewardEdge(t *testing.T) {
	// At 60N the circle's own poleward edge (its north side, closer to the
	// pole than the center latitude) needs more longitude span per meter
	// than the center latitude does, because cos shrinks moving poleward. A
	// box sized from cos(lat) alone would fall short of covering that edge.
	lat, lon, radiusM := 60.0, 150.0, 9260
	q := buildOverpassPOIQuery(lat, lon, radiusM, []string{"anchorage"})
	south, west, north, east := extractBBox(t, q)

	dlat := float64(radiusM) / 111320.0
	if !(south < lat-dlat) || !(north > lat+dlat) {
		t.Fatalf("expected the box to enclose the latitude span, got south=%f north=%f (want < %f, > %f)", south, north, lat-dlat, lat+dlat)
	}

	polewardLat := lat + dlat
	dlonPoleward := float64(radiusM) / (111320.0 * math.Cos(polewardLat*math.Pi/180))
	if !(lon-west >= dlonPoleward) {
		t.Fatalf("expected west to extend at least %f deg (poleward-adjusted) from lon, got west=%f (lon-west=%f)", dlonPoleward, west, lon-west)
	}
	if !(east-lon >= dlonPoleward) {
		t.Fatalf("expected east to extend at least %f deg (poleward-adjusted) from lon, got east=%f (east-lon=%f)", dlonPoleward, east, east-lon)
	}

	// Confirm the naive cos(lat)-only span really would have been
	// insufficient, so this test would catch a regression to that formula.
	dlonCenter := float64(radiusM) / (111320.0 * math.Cos(lat*math.Pi/180))
	if dlonPoleward <= dlonCenter {
		t.Fatalf("expected the poleward-adjusted longitude delta (%f) to exceed the naive center-latitude one (%f)", dlonPoleward, dlonCenter)
	}
}

func TestBuildOverpassPOIQuery_EveryClauseStillCarriesItsOwnAroundFilter(t *testing.T) {
	categories := []string{"anchorage", "bay", "trail"}
	q := buildOverpassPOIQuery(-20.4467, 149.0353, 9260, categories)

	wantAround := "(around:9260,-20.446700,149.035300)"
	wantCount := 0
	for _, id := range categories {
		wantCount += len(poiCategoryByID[id].Clauses)
	}
	if got := strings.Count(q, wantAround); got != wantCount {
		t.Fatalf("expected %d occurrences of %q (one per clause), got %d in:\n%s", wantCount, wantAround, got, q)
	}
}

func TestBuildOverpassPOIQuery_OmitsBBoxAtAntimeridian(t *testing.T) {
	q := buildOverpassPOIQuery(-20.4467, 179.99, 9260, []string{"anchorage"})
	if strings.Contains(q, "[bbox:") {
		t.Fatalf("expected no [bbox:...] setting when the box would cross the antimeridian, got:\n%s", q)
	}
}

func TestBuildOverpassPOIQuery_OmitsBBoxNearPole(t *testing.T) {
	q := buildOverpassPOIQuery(89.99, 149.0353, 9260, []string{"anchorage"})
	if strings.Contains(q, "[bbox:") {
		t.Fatalf("expected no [bbox:...] setting when the box would cross a pole, got:\n%s", q)
	}
}

// ── classification ───────────────────────────────────────────────────────

func TestClassifyElement_MatchesEachCategoryTable(t *testing.T) {
	all := []string{"anchorage", "bay", "island", "marina", "fuel", "ramp", "mooring", "historic", "viewpoint", "dive", "trail"}

	for _, tc := range []struct {
		name string
		tags map[string]string
		want string
	}{
		{"anchorage by seamark", map[string]string{"seamark:type": "anchorage"}, "anchorage"},
		{"anchorage by tag", map[string]string{"anchorage": "yes"}, "anchorage"},
		{"named bay", map[string]string{"natural": "bay", "name": "Turtle Bay"}, "bay"},
		{"named island", map[string]string{"place": "island", "name": "Lindeman Island"}, "island"},
		{"named islet", map[string]string{"place": "islet", "name": "Cole Island"}, "island"},
		{"marina by leisure", map[string]string{"leisure": "marina"}, "marina"},
		{"marina by seamark harbour", map[string]string{"seamark:type": "harbour", "seamark:harbour:category": "marina"}, "marina"},
		{"fuel by small craft facility", map[string]string{"seamark:type": "small_craft_facility", "seamark:small_craft_facility:category": "fuel_diesel"}, "fuel"},
		{"fuel by amenity+boat", map[string]string{"amenity": "fuel", "boat": "yes"}, "fuel"},
		{"fuel by waterway", map[string]string{"waterway": "fuel"}, "fuel"},
		{"ramp by leisure", map[string]string{"leisure": "slipway"}, "ramp"},
		{"ramp by seamark", map[string]string{"seamark:type": "slipway"}, "ramp"},
		{"mooring", map[string]string{"seamark:type": "mooring"}, "mooring"},
		{"named historic", map[string]string{"historic": "monument", "name": "Old Cairn"}, "historic"},
		{"named heritage", map[string]string{"heritage": "yes", "name": "Old Wharf"}, "historic"},
		{"unnamed lighthouse", map[string]string{"man_made": "lighthouse"}, "historic"},
		{"viewpoint", map[string]string{"tourism": "viewpoint"}, "viewpoint"},
		{"dive by sport scuba", map[string]string{"sport": "scuba_diving"}, "dive"},
		{"dive by sport snorkelling", map[string]string{"sport": "snorkelling"}, "dive"},
		{"named reef", map[string]string{"natural": "reef", "name": "Bait Reef"}, "dive"},
		{"dive centre", map[string]string{"leisure": "dive_centre"}, "dive"},
		{"named hiking relation", map[string]string{"route": "hiking", "name": "Coastal Track"}, "trail"},
		{"named trailhead", map[string]string{"highway": "trailhead", "name": "North Head"}, "trail"},
		{"named path", map[string]string{"highway": "path", "name": "Ridge Path"}, "trail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := classifyElement(all, tc.tags)
			if !ok {
				t.Fatalf("expected a category match for %+v", tc.tags)
			}
			if got != tc.want {
				t.Fatalf("expected category %q, got %q", tc.want, got)
			}
		})
	}
}

func TestClassifyElement_UnnamedBayDoesNotMatch(t *testing.T) {
	if _, ok := classifyElement([]string{"bay"}, map[string]string{"natural": "bay"}); ok {
		t.Fatalf("expected an unnamed bay to not classify - bay is named-only")
	}
}

func TestClassifyElement_UnrequestedCategoryNeverMatches(t *testing.T) {
	// Tags that would classify as "mooring" must not match when the caller
	// only requested "anchorage" - the plugin must never return a category
	// the caller didn't ask for.
	if _, ok := classifyElement([]string{"anchorage"}, map[string]string{"seamark:type": "mooring"}); ok {
		t.Fatalf("expected no match for a category outside the requested set")
	}
}

func TestClassifyElement_FixedPrecedenceOnAmbiguousTags(t *testing.T) {
	// An element carrying both anchorage and mooring tags: anchorage
	// precedes mooring in poiCategories, so it must win when both are
	// requested.
	tags := map[string]string{"seamark:type": "mooring", "anchorage": "yes"}
	got, ok := classifyElement([]string{"mooring", "anchorage"}, tags)
	if !ok || got != "anchorage" {
		t.Fatalf("expected anchorage to win by fixed precedence, got %q (ok=%v)", got, ok)
	}
}

// ── element parsing / position resolution ───────────────────────────────

func TestParsePOIElements_NodeUsesLatLonDirectly(t *testing.T) {
	elements := []overpassElement{
		{Type: "node", ID: 1, Lat: ptr(-20.44), Lon: ptr(149.03), Tags: map[string]string{"seamark:type": "anchorage"}},
	}
	features, _ := parsePOIElements([]string{"anchorage"}, elements)
	if len(features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(features))
	}
	if features[0].Lat != -20.44 || features[0].Lon != 149.03 {
		t.Fatalf("expected node lat/lon to pass through, got %+v", features[0])
	}
	if features[0].ID != "node/1" {
		t.Fatalf("expected id node/1, got %q", features[0].ID)
	}
}

func TestParsePOIElements_WayUsesCenter(t *testing.T) {
	elements := []overpassElement{
		{Type: "way", ID: 42, Center: &overpassLatLon{Lat: -20.5, Lon: 149.1}, Tags: map[string]string{"place": "island", "name": "Test Island"}},
	}
	features, _ := parsePOIElements([]string{"island"}, elements)
	if len(features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(features))
	}
	if features[0].Lat != -20.5 || features[0].Lon != 149.1 {
		t.Fatalf("expected way center to be used for position, got %+v", features[0])
	}
	if features[0].Name != "Test Island" {
		t.Fatalf("expected name Test Island, got %q", features[0].Name)
	}
}

func TestParsePOIElements_DropsElementsWithNoPositionOrNoClassification(t *testing.T) {
	elements := []overpassElement{
		{Type: "way", ID: 1, Tags: map[string]string{"seamark:type": "anchorage"}},                     // no lat/lon/center
		{Type: "node", ID: 2, Lat: ptr(1.0), Lon: ptr(2.0), Tags: map[string]string{"shop": "bakery"}}, // unclassifiable
	}
	features, _ := parsePOIElements([]string{"anchorage"}, elements)
	if len(features) != 0 {
		t.Fatalf("expected 0 features, got %d: %+v", len(features), features)
	}
}

func TestBestName_PriorityOrder(t *testing.T) {
	if got := bestName(map[string]string{"name": "A", "name:en": "B", "official_name": "C"}); got != "A" {
		t.Fatalf("expected name to win, got %q", got)
	}
	if got := bestName(map[string]string{"name:en": "B", "official_name": "C"}); got != "B" {
		t.Fatalf("expected name:en to win when name is absent, got %q", got)
	}
	if got := bestName(map[string]string{"official_name": "C"}); got != "C" {
		t.Fatalf("expected official_name as last resort, got %q", got)
	}
	if got := bestName(map[string]string{}); got != "" {
		t.Fatalf("expected empty name for no name tags, got %q", got)
	}
}

// ── truncation ───────────────────────────────────────────────────────────

func TestTruncatedCategories_FlagsExactCapHits(t *testing.T) {
	counts := map[string]int{"fuel": 20, "anchorage": 3}
	got := truncatedCategories([]string{"fuel", "anchorage"}, counts)
	if len(got) != 1 || got[0] != "fuel" {
		t.Fatalf("expected only fuel (cap 20, hit exactly) to be truncated, got %v", got)
	}
}

func TestTruncatedCategories_EmptyWhenNoCategoryHitsItsCap(t *testing.T) {
	counts := map[string]int{"anchorage": 3, "bay": 1}
	got := truncatedCategories([]string{"anchorage", "bay"}, counts)
	if len(got) != 0 {
		t.Fatalf("expected no truncated categories, got %v", got)
	}
}

// ── rate limit detection ────────────────────────────────────────────────

func TestLooksLikeOverpassRateLimit_DetectsHTMLContentType(t *testing.T) {
	if !looksLikeOverpassRateLimit("text/html; charset=utf-8", []byte("<html>whatever</html>")) {
		t.Fatalf("expected an HTML content-type to be detected as a rate limit")
	}
}

func TestLooksLikeOverpassRateLimit_DetectsRateLimitedBodySubstring(t *testing.T) {
	body := []byte(`<html><body>Dispatcher_Client::request_read_and_idx::rate_limited</body></html>`)
	if !looksLikeOverpassRateLimit("", body) {
		t.Fatalf("expected the rate_limited body substring to be detected even with no content-type")
	}
}

func TestLooksLikeOverpassRateLimit_FalseForOrdinaryJSON(t *testing.T) {
	if looksLikeOverpassRateLimit("application/json", []byte(`{"elements":[]}`)) {
		t.Fatalf("expected ordinary JSON to not be flagged as a rate limit")
	}
}

// ── wikipedia enrichment ─────────────────────────────────────────────────

func TestWikipediaTitle_StripsLanguagePrefix(t *testing.T) {
	if got := wikipediaTitle("en:Lindeman Island"); got != "Lindeman Island" {
		t.Fatalf("expected title without language prefix, got %q", got)
	}
}

func TestWikipediaTitle_NoPrefixPassesThrough(t *testing.T) {
	if got := wikipediaTitle("Lindeman Island"); got != "Lindeman Island" {
		t.Fatalf("expected the whole tag as the title when there is no prefix, got %q", got)
	}
}

func TestWikipediaTitle_EmptyTagIsEmpty(t *testing.T) {
	if got := wikipediaTitle(""); got != "" {
		t.Fatalf("expected an empty tag to produce an empty title, got %q", got)
	}
}

func TestFirstSentence_CutsAtFirstPeriodSpace(t *testing.T) {
	got := firstSentence("Lindeman Island is a continental island. It is part of the Whitsundays.")
	if got != "Lindeman Island is a continental island." {
		t.Fatalf("unexpected first sentence: %q", got)
	}
}

func TestFirstSentence_FallsBackToWholeTrimmedTextWithNoBoundary(t *testing.T) {
	if got := firstSentence("  no sentence boundary here  "); got != "no sentence boundary here" {
		t.Fatalf("unexpected fallback: %q", got)
	}
}

func TestParseWikipediaSummary_ParsesExtractAndPageURL(t *testing.T) {
	body := loadFixture(t, "wikipedia_summary_lindeman_island.json")
	detail, sourceURL, err := parseWikipediaSummary(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail == "" {
		t.Fatalf("expected a non-empty detail sentence")
	}
	if !strings.Contains(sourceURL, "wikipedia.org") {
		t.Fatalf("expected a wikipedia.org source URL, got %q", sourceURL)
	}
}

func TestParseWikipediaSummary_ErrorsOnUnparseableBody(t *testing.T) {
	if _, _, err := parseWikipediaSummary([]byte("not json")); err == nil {
		t.Fatalf("expected an error for unparseable JSON")
	}
}

func TestNearestWikipediaFeatures_SortsByDistanceAndCaps(t *testing.T) {
	features := []poiFeatureOut{
		{Name: "Far", Lat: -20.6, Lon: 149.2, tags: map[string]string{"wikipedia": "en:Far"}},
		{Name: "Near", Lat: -20.45, Lon: 149.04, tags: map[string]string{"wikipedia": "en:Near"}},
		{Name: "NoTag", Lat: -20.44, Lon: 149.03, tags: map[string]string{}},
	}
	got := nearestWikipediaFeatures(features, -20.4467, 149.0353, 1)
	if len(got) != 1 || features[got[0]].Name != "Near" {
		t.Fatalf("expected only the nearest wikipedia-tagged feature, got indexes %v", got)
	}
}

func TestResolveDetailLimit_DefaultsWhenAbsent(t *testing.T) {
	if got := resolveDetailLimit("", false); got != defaultDetailLimit {
		t.Fatalf("expected default %d, got %d", defaultDetailLimit, got)
	}
}

func TestResolveDetailLimit_UsesConfiguredValue(t *testing.T) {
	if got := resolveDetailLimit("3", true); got != 3 {
		t.Fatalf("expected 3, got %d", got)
	}
}

func TestResolveDetailLimit_FallsBackOnUnparseableValue(t *testing.T) {
	if got := resolveDetailLimit("not-a-number", true); got != defaultDetailLimit {
		t.Fatalf("expected default on unparseable value, got %d", got)
	}
}

func TestResolveOverpassURL_DefaultsWhenAbsent(t *testing.T) {
	got, err := resolveOverpassURL("", false)
	if err != nil {
		t.Fatalf("expected no error when overpass_url is absent, got %v", err)
	}
	if got != defaultOverpassAPIURL {
		t.Fatalf("expected default %q, got %q", defaultOverpassAPIURL, got)
	}
}

func TestResolveOverpassURL_AcceptsValidOverride(t *testing.T) {
	const mirror = "https://overpass.kumi.systems/api/interpreter"
	got, err := resolveOverpassURL(mirror, true)
	if err != nil {
		t.Fatalf("expected a valid https override to be accepted, got error %v", err)
	}
	if got != mirror {
		t.Fatalf("expected %q, got %q", mirror, got)
	}
}

func TestResolveOverpassURL_RejectsMalformedValueRatherThanFallingBack(t *testing.T) {
	cases := []string{
		"not a url",
		"overpass.kumi.systems/api/interpreter",        // no scheme
		"http://overpass.kumi.systems/api/interpreter", // not https
		"https://", // no host
		"ftp://overpass.kumi.systems/api/interpreter", // wrong scheme
	}
	for _, raw := range cases {
		got, err := resolveOverpassURL(raw, true)
		if err == nil {
			t.Fatalf("expected an error for malformed overpass_url %q, got url %q with no error", raw, got)
		}
		if !strings.Contains(err.Error(), "overpass_url") {
			t.Fatalf("expected the error to name the overpass_url key, got %v", err)
		}
		if got != "" {
			t.Fatalf("expected no fallback to the default on a malformed value, got %q", got)
		}
	}
}

func TestResolveOverpassURL_TreatsEmptyPresentValueLikeAbsent(t *testing.T) {
	got, err := resolveOverpassURL("", true)
	if err != nil {
		t.Fatalf("expected no error for empty present value, got %v", err)
	}
	if got != defaultOverpassAPIURL {
		t.Fatalf("expected default %q, got %q", defaultOverpassAPIURL, got)
	}
}

func TestResolveOverpassURL_TreatsWhitespacePresentValueLikeAbsent(t *testing.T) {
	got, err := resolveOverpassURL("   ", true)
	if err != nil {
		t.Fatalf("expected no error for whitespace-only present value, got %v", err)
	}
	if got != defaultOverpassAPIURL {
		t.Fatalf("expected default %q, got %q", defaultOverpassAPIURL, got)
	}
}

// ── live fixtures ────────────────────────────────────────────────────────
//
// Captured from a live Overpass server against the plan's exact query
// shape (buildOverpassPOIQuery), at the coordinates backend/place_name_test.go
// already uses for Lindeman Island, and at Airlie Beach. See this
// directory's README for capture details and the one deviation (the
// public mirror used) from hitting overpass-api.de directly.

func TestLiveFixture_LindemanClassifiesKnownIslands(t *testing.T) {
	body := loadFixture(t, "overpass_poi_lindeman_9260m.json")
	var parsed overpassResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("failed to parse fixture: %v", err)
	}

	all := []string{"anchorage", "bay", "island", "marina", "fuel", "ramp", "mooring", "historic", "viewpoint", "dive", "trail"}
	features, _ := parsePOIElements(all, parsed.Elements)

	names := make(map[string]string, len(features))
	for _, f := range features {
		names[f.Name] = f.Category
	}

	if names["Lindeman Island"] != "island" {
		t.Fatalf("expected Lindeman Island classified as island, got %q (present: %v)", names["Lindeman Island"], names["Lindeman Island"] != "")
	}
	if names["Turtle Bay"] != "bay" {
		t.Fatalf("expected Turtle Bay classified as bay, got %q", names["Turtle Bay"])
	}
}

func TestLiveFixture_AirlieBeachReturnsFeatures(t *testing.T) {
	body := loadFixture(t, "overpass_poi_airlie_5556m.json")
	var parsed overpassResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("failed to parse fixture: %v", err)
	}

	all := []string{"anchorage", "bay", "island", "marina", "fuel", "ramp", "mooring", "historic", "viewpoint", "dive", "trail"}
	features, _ := parsePOIElements(all, parsed.Elements)
	if len(features) == 0 {
		t.Fatalf("expected at least one classified feature near Airlie Beach")
	}
}

func ptr(v float64) *float64 { return &v }
