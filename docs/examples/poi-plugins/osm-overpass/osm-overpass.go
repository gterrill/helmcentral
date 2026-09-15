// osm-overpass.go holds the parsing, query-building and HTTP logic for the
// OpenStreetMap Overpass POI-provider plugin, kept in a separate file from
// main.go deliberately: this file has no dependency on
// "github.com/extism/go-pdk", so it (and main_test.go, which exercises it)
// can be built and tested with the plain host Go toolchain (`go test
// ./...`, no TinyGo/wasm target needed) - see main.go's doc comment for why
// that split matters. main.go's //go:wasmexport functions call straight
// into these same functions; nothing here is reimplemented or duplicated
// there.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// defaultOverpassAPIURL is the Overpass endpoint used when config.json
// carries no "overpass_url" override - see resolveOverpassURL.
const defaultOverpassAPIURL = "https://overpass-api.de/api/interpreter"
const wikipediaSummaryURLFmt = "https://en.wikipedia.org/api/rest_v1/page/summary/%s"

// defaultDetailLimit is how many of the nearest wikipedia-tagged features
// get enriched with a Wikipedia summary, when config.json carries no
// "detail_limit" override.
const defaultDetailLimit = 5

// ── category table ──────────────────────────────────────────────────────
//
// One entry per docs/reference/poi-categories.md category. Clauses are
// literal Overpass QL tag-filter fragments (element-type prefix included -
// "nwr" unless a clause states otherwise), used both to build the query
// AND, via matches(), to classify a returned element back into a category
// (Overpass's plain JSON "out" doesn't tell us which named set produced an
// element, so every element is reclassified from its own tags after the
// fetch). A "named only" category bakes its ["name"] requirement directly
// into the clause text - Overpass never returns an unnamed element for that
// clause in the first place - rather than a separate host-side check, so
// there is exactly one place a category's naming rule lives.
//
// Historic is the one category with a mixed rule: the historic/heritage
// clauses require a name, but a lighthouse is identifiable on the chart by
// its light characteristic even unnamed, so man_made=lighthouse carries no
// name requirement. Trail's category-level "named only" is applied to
// every one of its three clauses, including the highway=trailhead clause -
// the table lists that clause without an explicit ["name"], but names no
// exception the way historic does for lighthouses, so this plugin reads
// the category-level flag as binding for all three.
type overpassCategory struct {
	ID      string
	Clauses []overpassClause
	Cap     int
}

type overpassClause struct {
	// ElementType is the Overpass QL element selector: "nwr" (node/way/
	// relation, the default for every clause except trail's two
	// element-specific ones), "way", or "relation".
	ElementType string
	// TagFilter is everything after the element type, e.g.
	// `["seamark:type"="anchorage"]`.
	TagFilter string
	// Matches reports whether a returned element's tags satisfy this exact
	// clause (mirroring TagFilter's condition), used for reclassification.
	Matches func(tags map[string]string) bool
}

func hasTag(tags map[string]string, key string) bool {
	_, ok := tags[key]
	return ok
}

func tagEquals(tags map[string]string, key, value string) bool {
	return tags[key] == value
}

// poiCategories is ordered - this order is the fixed classification
// precedence used when a returned element's tags happen to satisfy more
// than one requested category's clause (rare, but tag combinations in the
// wild are messy). It also drives the order categories appear in the built
// query, which keeps captured test fixtures reproducible.
var poiCategories = []overpassCategory{
	{
		ID:  "anchorage",
		Cap: 40,
		Clauses: []overpassClause{
			{ElementType: "nwr", TagFilter: `["seamark:type"="anchorage"]`, Matches: func(t map[string]string) bool { return tagEquals(t, "seamark:type", "anchorage") }},
			{ElementType: "nwr", TagFilter: `["anchorage"="yes"]`, Matches: func(t map[string]string) bool { return tagEquals(t, "anchorage", "yes") }},
		},
	},
	{
		ID:  "bay",
		Cap: 40,
		Clauses: []overpassClause{
			{ElementType: "nwr", TagFilter: `["natural"="bay"]["name"]`, Matches: func(t map[string]string) bool { return tagEquals(t, "natural", "bay") && hasTag(t, "name") }},
		},
	},
	{
		ID:  "island",
		Cap: 60,
		Clauses: []overpassClause{
			{ElementType: "nwr", TagFilter: `["place"~"^(island|islet)$"]["name"]`, Matches: func(t map[string]string) bool {
				return (t["place"] == "island" || t["place"] == "islet") && hasTag(t, "name")
			}},
		},
	},
	{
		ID:  "marina",
		Cap: 40,
		Clauses: []overpassClause{
			{ElementType: "nwr", TagFilter: `["leisure"="marina"]`, Matches: func(t map[string]string) bool { return tagEquals(t, "leisure", "marina") }},
			{ElementType: "nwr", TagFilter: `["seamark:type"="harbour"]["seamark:harbour:category"="marina"]`, Matches: func(t map[string]string) bool {
				return tagEquals(t, "seamark:type", "harbour") && tagEquals(t, "seamark:harbour:category", "marina")
			}},
		},
	},
	{
		ID:  "fuel",
		Cap: 20,
		Clauses: []overpassClause{
			{ElementType: "nwr", TagFilter: `["seamark:type"="small_craft_facility"]["seamark:small_craft_facility:category"~"fuel"]`, Matches: func(t map[string]string) bool {
				return tagEquals(t, "seamark:type", "small_craft_facility") && strings.Contains(t["seamark:small_craft_facility:category"], "fuel")
			}},
			{ElementType: "nwr", TagFilter: `["amenity"="fuel"]["boat"="yes"]`, Matches: func(t map[string]string) bool { return tagEquals(t, "amenity", "fuel") && tagEquals(t, "boat", "yes") }},
			{ElementType: "nwr", TagFilter: `["waterway"="fuel"]`, Matches: func(t map[string]string) bool { return tagEquals(t, "waterway", "fuel") }},
		},
	},
	{
		ID:  "ramp",
		Cap: 40,
		Clauses: []overpassClause{
			{ElementType: "nwr", TagFilter: `["leisure"="slipway"]`, Matches: func(t map[string]string) bool { return tagEquals(t, "leisure", "slipway") }},
			{ElementType: "nwr", TagFilter: `["seamark:type"="slipway"]`, Matches: func(t map[string]string) bool { return tagEquals(t, "seamark:type", "slipway") }},
		},
	},
	{
		ID:  "mooring",
		Cap: 80,
		Clauses: []overpassClause{
			{ElementType: "nwr", TagFilter: `["seamark:type"="mooring"]`, Matches: func(t map[string]string) bool { return tagEquals(t, "seamark:type", "mooring") }},
		},
	},
	{
		ID:  "historic",
		Cap: 60,
		Clauses: []overpassClause{
			{ElementType: "nwr", TagFilter: `["historic"]["name"]`, Matches: func(t map[string]string) bool { return hasTag(t, "historic") && hasTag(t, "name") }},
			{ElementType: "nwr", TagFilter: `["heritage"]["name"]`, Matches: func(t map[string]string) bool { return hasTag(t, "heritage") && hasTag(t, "name") }},
			{ElementType: "nwr", TagFilter: `["man_made"="lighthouse"]`, Matches: func(t map[string]string) bool { return tagEquals(t, "man_made", "lighthouse") }},
		},
	},
	{
		ID:  "viewpoint",
		Cap: 40,
		Clauses: []overpassClause{
			{ElementType: "nwr", TagFilter: `["tourism"="viewpoint"]`, Matches: func(t map[string]string) bool { return tagEquals(t, "tourism", "viewpoint") }},
		},
	},
	{
		ID:  "dive",
		Cap: 40,
		Clauses: []overpassClause{
			{ElementType: "nwr", TagFilter: `["sport"~"^(scuba_diving|snorkelling)$"]`, Matches: func(t map[string]string) bool {
				return t["sport"] == "scuba_diving" || t["sport"] == "snorkelling"
			}},
			{ElementType: "nwr", TagFilter: `["natural"="reef"]["name"]`, Matches: func(t map[string]string) bool { return tagEquals(t, "natural", "reef") && hasTag(t, "name") }},
			{ElementType: "nwr", TagFilter: `["leisure"="dive_centre"]`, Matches: func(t map[string]string) bool { return tagEquals(t, "leisure", "dive_centre") }},
		},
	},
	{
		ID:  "trail",
		Cap: 40,
		Clauses: []overpassClause{
			{ElementType: "relation", TagFilter: `["route"="hiking"]["name"]`, Matches: func(t map[string]string) bool { return tagEquals(t, "route", "hiking") && hasTag(t, "name") }},
			{ElementType: "nwr", TagFilter: `["highway"="trailhead"]["name"]`, Matches: func(t map[string]string) bool { return tagEquals(t, "highway", "trailhead") && hasTag(t, "name") }},
			{ElementType: "way", TagFilter: `["highway"~"^(path|footway)$"]["name"]`, Matches: func(t map[string]string) bool {
				return (t["highway"] == "path" || t["highway"] == "footway") && hasTag(t, "name")
			}},
		},
	},
}

var poiCategoryByID = func() map[string]overpassCategory {
	m := make(map[string]overpassCategory, len(poiCategories))
	for _, c := range poiCategories {
		m[c.ID] = c
	}
	return m
}()

// orderRequestedCategories returns the requested categories, deduplicated
// and reordered to poiCategories' fixed precedence order - both so the
// built query text is deterministic (stable fixtures/tests regardless of
// the caller's query-string order) and so classifyElement's precedence walk
// is meaningful.
func orderRequestedCategories(requested []string) []string {
	want := make(map[string]bool, len(requested))
	for _, id := range requested {
		want[id] = true
	}

	ordered := make([]string, 0, len(requested))
	for _, c := range poiCategories {
		if want[c.ID] {
			ordered = append(ordered, c.ID)
		}
	}
	return ordered
}

// metersPerDegreeLat approximates how many meters correspond to one degree
// of latitude. This varies slightly with actual latitude (Earth is not a
// perfect sphere), but overpassBoundingBox only needs a box that is
// generously large enough to enclose a circle, not an exact one, and
// bboxPadFraction absorbs the resulting slack.
const metersPerDegreeLat = 111320.0

// bboxPadFraction pads overpassBoundingBox's computed box on every side, so
// the flat-earth approximation in metersPerDegreeLat (and ordinary
// float64 rounding) can never leave the box just short of enclosing the
// around: circle it was built to bound.
const bboxPadFraction = 0.01

// overpassBoundingBox computes a [south, west, north, east] box in degrees
// that encloses the around: circle at (lat, lon, radiusM), for use as
// Overpass QL's global [bbox:...] query setting alongside (not instead of)
// the per-clause around: filters.
//
// Why this exists: on overpass.openstreetmap.fr, this plugin's slower
// clauses are key-only or regex tag filters (nwr["historic"]["name"],
// nwr["sport"~"^(scuba_diving|snorkelling)$"], and similar) which that
// mirror's planner scans by tag across the whole database before applying
// each clause's own around: filter - there is no spatial index to start
// from. Measured against this plugin's real 11-category query at Lindeman
// Island (-20.4467, 149.0353, 9260m) on 2026-09-15: without a bbox setting,
// the query hit the mirror's own server-side timeout after 23s wall time
// with 0 elements returned - past the host's 15s WASM plugin budget
// (backend/wasm_plugin.go, WASM_PLUGIN_TIMEOUT_MS). Adding a global
// [bbox:...] setting to the query header gives the planner a spatial index
// to start from instead: the identical query, same position, returned in
// 2.4s with the exact same 23 elements. Because every point inside an
// around: circle is by construction also inside a box built to enclose that
// circle, adding this bbox can never drop or change a result - the
// per-clause around: filters remain the actual, unchanged filter.
//
// ok is false when the box would have to cross the antimeridian or a pole
// to enclose the circle. Clamping or wrapping such a box would silently
// drop real results on the other side of that seam, so the caller omits
// the [bbox:...] setting entirely and falls back to the (still correct,
// just slower without a spatial index) around: filters alone.
func overpassBoundingBox(lat, lon float64, radiusM int) (south, west, north, east float64, ok bool) {
	dlat := float64(radiusM) / metersPerDegreeLat

	// The circle's widest point in longitude is bounded using the cosine of
	// the box's poleward-most latitude - whichever of its two edges has the
	// larger absolute value. Cosine only shrinks moving away from the
	// equator, so that edge has the smallest cosine, and therefore needs
	// the largest longitude delta, of any latitude in the box. Using that
	// one delta for the whole box is deliberately generous rather than
	// exact: it guarantees the circle fits, at the cost of some extra width
	// away from that edge.
	polewardLat := math.Max(math.Abs(lat-dlat), math.Abs(lat+dlat))
	dlon := float64(radiusM) / (metersPerDegreeLat * math.Cos(polewardLat*math.Pi/180))

	dlat *= 1 + bboxPadFraction
	dlon *= 1 + bboxPadFraction

	south, north = lat-dlat, lat+dlat
	west, east = lon-dlon, lon+dlon
	if south < -90 || north > 90 || west < -180 || east > 180 {
		return 0, 0, 0, 0, false
	}
	return south, west, north, east, true
}

// buildOverpassPOIQuery builds one Overpass QL query covering every
// requested category, each in its own named set so each gets its own
// `out tags center <cap>` - see the package doc comment for why this needs
// a fixed category order and why classification still happens after the
// fact from tags rather than from which set produced an element.
func buildOverpassPOIQuery(lat, lon float64, radiusM int, categories []string) string {
	around := fmt.Sprintf("(around:%d,%.6f,%.6f)", radiusM, lat, lon)

	var b strings.Builder
	// 12s, not Overpass's usual 60s default: the host aborts this whole
	// plugin call after WASM_PLUGIN_TIMEOUT_MS (15s by default,
	// backend/wasm_plugin.go), so the server-side budget must stay under
	// that ceiling or Overpass just keeps working a query the host has
	// already abandoned.
	b.WriteString("[out:json][timeout:12]")
	if south, west, north, east, ok := overpassBoundingBox(lat, lon, radiusM); ok {
		// See overpassBoundingBox's doc comment for why this is here: it
		// gives overpass.openstreetmap.fr's planner a spatial index to
		// start from, without changing which elements the per-clause
		// around: filters below select.
		fmt.Fprintf(&b, "[bbox:%.6f,%.6f,%.6f,%.6f]", south, west, north, east)
	}
	b.WriteString(";\n")
	for i, id := range orderRequestedCategories(categories) {
		cat := poiCategoryByID[id]
		setName := fmt.Sprintf("c%d", i)

		b.WriteString("(\n")
		for _, clause := range cat.Clauses {
			b.WriteString("  ")
			b.WriteString(clause.ElementType)
			b.WriteString(clause.TagFilter)
			b.WriteString(around)
			b.WriteString(";\n")
		}
		fmt.Fprintf(&b, ")->.%s;\n", setName)
		fmt.Fprintf(&b, ".%s out tags center %d;\n", setName, cat.Cap)
	}
	return b.String()
}

// classifyElement walks categories (already filtered/ordered to the
// request) in fixed precedence order and returns the first category whose
// clause set matches the element's tags.
func classifyElement(categories []string, tags map[string]string) (string, bool) {
	for _, id := range orderRequestedCategories(categories) {
		cat := poiCategoryByID[id]
		for _, clause := range cat.Clauses {
			if clause.Matches(tags) {
				return id, true
			}
		}
	}
	return "", false
}

// ── Overpass response shapes ────────────────────────────────────────────

type overpassLatLon struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type overpassElement struct {
	Type   string            `json:"type"`
	ID     int64             `json:"id"`
	Lat    *float64          `json:"lat,omitempty"`
	Lon    *float64          `json:"lon,omitempty"`
	Center *overpassLatLon   `json:"center,omitempty"`
	Tags   map[string]string `json:"tags"`
}

type overpassResponse struct {
	Elements []overpassElement `json:"elements"`
}

func elementLatLon(el overpassElement) (lat, lon float64, ok bool) {
	if el.Lat != nil && el.Lon != nil {
		return *el.Lat, *el.Lon, true
	}
	if el.Center != nil {
		return el.Center.Lat, el.Center.Lon, true
	}
	return 0, 0, false
}

// bestName picks the first non-empty of name, name:en, official_name - the
// same priority order backend/place_name.go and this plugin's README agree
// on.
func bestName(tags map[string]string) string {
	for _, key := range []string{"name", "name:en", "official_name"} {
		if v := strings.TrimSpace(tags[key]); v != "" {
			return v
		}
	}
	return ""
}

// looksLikeOverpassRateLimit mirrors backend/place_name.go's function of
// the same name: Overpass signals its rate limit with an HTTP 200 carrying
// an HTML body (never JSON), typically containing
// Dispatcher_Client::request_read_and_idx::rate_limited. Duplicated here
// rather than imported because this file has no access to the backend
// package from inside the WASM sandbox - see this plugin's README for the
// two-file split this belongs to.
func looksLikeOverpassRateLimit(contentType string, body []byte) bool {
	if strings.Contains(strings.ToLower(contentType), "text/html") {
		return true
	}
	return bytes.Contains(body, []byte("rate_limited"))
}

func headerCaseInsensitive(headers map[string]string, name string) string {
	for k, v := range headers {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

// parseOverpassPOIResponse parses one Overpass response into raw features
// (unclassified callers get "" category from classifyElement's second
// return being false, which parsePOIElements below drops - it can only mean
// a Match predicate and an element from the same fetch disagree, itself
// only possible if this file's clause table and query text ever drift
// apart) plus a per-category count used by truncation detection.
func parsePOIElements(categories []string, elements []overpassElement) (features []poiFeatureOut, countByCategory map[string]int) {
	countByCategory = make(map[string]int)
	for _, el := range elements {
		category, ok := classifyElement(categories, el.Tags)
		if !ok {
			continue
		}
		lat, lon, ok := elementLatLon(el)
		if !ok {
			continue
		}
		name := bestName(el.Tags)
		countByCategory[category]++

		features = append(features, poiFeatureOut{
			ID:       fmt.Sprintf("%s/%d", el.Type, el.ID),
			Category: category,
			Name:     name,
			Lat:      lat,
			Lon:      lon,
			tags:     el.Tags,
		})
	}
	return features, countByCategory
}

// truncatedCategories names every requested category whose returned count
// hit its cap exactly. Overpass's `out ... <cap>` statement silently drops
// anything past the cap with no "there were more" signal in the JSON
// output, so an exact-cap count is the only client-observable proxy for
// truncation - a real result that happens to total exactly the cap reads
// as (harmlessly) truncated too. Getting a true count would need a second
// `out count;` query per category; the plan calls for one Overpass request
// per call, so this plugin accepts the false-positive-at-the-boundary
// tradeoff instead.
func truncatedCategories(categories []string, countByCategory map[string]int) []string {
	var truncated []string
	for _, id := range orderRequestedCategories(categories) {
		if countByCategory[id] == poiCategoryByID[id].Cap {
			truncated = append(truncated, id)
		}
	}
	return truncated
}

// ── Wikipedia enrichment ─────────────────────────────────────────────────

// wikipediaTitle extracts the article title from an OSM wikipedia tag,
// which is conventionally "lang:Title" (e.g. "en:Lindeman Island") but
// sometimes just "Title" with no language prefix. Only English Wikipedia is
// ever queried (see allowed_hosts.json) regardless of the tag's language
// prefix - a non-English tag's title often still resolves on English
// Wikipedia (many articles share a title across languages), and when it
// doesn't, the fetch simply fails and detail stays empty, which is the
// correct behaviour for data this plugin cannot verify.
func wikipediaTitle(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}
	if idx := strings.Index(tag, ":"); idx > 0 && idx < len(tag)-1 {
		return strings.TrimSpace(tag[idx+1:])
	}
	return tag
}

// firstSentence returns the text up to and including the first ". " (or a
// trailing "." at the very end), falling back to the whole trimmed string
// when no sentence boundary is found. This is a simple heuristic - it will
// cut early on an abbreviation like "St." - accepted here because the
// alternative (a full NLP sentence splitter) is far more machinery than a
// one-line POI description warrants.
func firstSentence(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if idx := strings.Index(text, ". "); idx >= 0 {
		return text[:idx+1]
	}
	if strings.HasSuffix(text, ".") {
		return text
	}
	return text
}

type wikipediaSummary struct {
	Extract     string `json:"extract"`
	ContentURLs struct {
		Desktop struct {
			Page string `json:"page"`
		} `json:"desktop"`
	} `json:"content_urls"`
}

func parseWikipediaSummary(body []byte) (detail, sourceURL string, err error) {
	var parsed wikipediaSummary
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", "", fmt.Errorf("parse wikipedia summary: %w", err)
	}
	sentence := firstSentence(parsed.Extract)
	if sentence == "" {
		return "", "", fmt.Errorf("wikipedia summary had no extract")
	}
	return sentence, parsed.ContentURLs.Desktop.Page, nil
}

func wikipediaSummaryURL(title string) string {
	escaped := url.PathEscape(strings.ReplaceAll(title, " ", "_"))
	return fmt.Sprintf(wikipediaSummaryURLFmt, escaped)
}

// resolveDetailLimit reads the "detail_limit" plugin config value (a plain
// string, per the shared config.json convention), defaulting to
// defaultDetailLimit on an absent, empty or unparseable value.
func resolveDetailLimit(raw string, present bool) int {
	if !present {
		return defaultDetailLimit
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return defaultDetailLimit
	}
	return n
}

// resolveOverpassURL reads the optional "overpass_url" plugin config value,
// defaulting to defaultOverpassAPIURL when config.json carries no such key.
// A present value that is empty or contains only whitespace is treated like
// an absent key and returns the default - this matches the host's
// OVERPASS_API_URL handling because config.json maps the key to that env var
// and docker-compose passes it through empty when unset.
// Unlike resolveDetailLimit above, a present-but-malformed value does NOT
// fall back to the default - a config.json edit that failed to produce a
// usable URL almost certainly did not mean "use overpass-api.de", so this
// returns an error naming the "overpass_url" key rather than masking the
// mistake. The value must parse as an absolute https URL.
//
// Pointing this at a mirror (e.g. https://overpass.openstreetmap.fr/api/interpreter)
// does not by itself grant network access to it: the mirror's host still has
// to be added to this plugin's osm-overpass.allowed_hosts.json (or the
// Settings allowlist override, docs/adr/0024-plugin-descriptions-and-allowlist-overrides.md),
// or the request fails at the sandbox boundary instead. See this plugin's
// README.
func resolveOverpassURL(raw string, present bool) (string, error) {
	if !present {
		return defaultOverpassAPIURL, nil
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return defaultOverpassAPIURL, nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", fmt.Errorf("overpass_url: must be an absolute https URL, got %q", raw)
	}
	return trimmed, nil
}

// nearestWikipediaFeatures returns the indexes of features carrying a
// wikipedia tag, nearest-first by great-circle distance from (lat, lon),
// capped at limit. Distance here is a plugin-local concern purely for
// picking which features are worth an extra HTTP round trip - the host
// (backend/poi_providers.go) computes the authoritative distance/bearing
// that reaches the operator.
func nearestWikipediaFeatures(features []poiFeatureOut, lat, lon float64, limit int) []int {
	type ranked struct {
		index    int
		distance float64
	}
	var candidates []ranked
	for i, f := range features {
		if strings.TrimSpace(f.tags["wikipedia"]) == "" {
			continue
		}
		candidates = append(candidates, ranked{index: i, distance: haversineMeters(lat, lon, f.Lat, f.Lon)})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].distance < candidates[j].distance })

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	indexes := make([]int, len(candidates))
	for i, c := range candidates {
		indexes[i] = c.index
	}
	return indexes
}

// haversineMeters is the standard great-circle distance formula, duplicated
// from the backend (backend/signalk.go) for the same "no cross-sandbox
// import" reason as looksLikeOverpassRateLimit above.
func haversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusMeters = 6371000.0
	lat1Rad := lat1 * math.Pi / 180
	lon1Rad := lon1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	lon2Rad := lon2 * math.Pi / 180
	deltaLat := lat2Rad - lat1Rad
	deltaLon := lon2Rad - lon1Rad
	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) + math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusMeters * c
}

// ── output shape (mirrors backend/wasm_poi_provider.go's wasmPOIFeatureOutput) ──

// poiFeatureOut carries tags alongside the wire fields purely so
// nearestWikipediaFeatures/enrichment can consult them after
// classification; tags is deliberately unexported and never marshaled (see
// fetchPOIOutput's own mapping in main.go).
type poiFeatureOut struct {
	ID        string
	Category  string
	Name      string
	Lat       float64
	Lon       float64
	Detail    string
	SourceURL string

	tags map[string]string
}

// classifyAndCountFeatures is the one entry point main.go's fetch_poi
// export needs into this file's parsing logic: raw Overpass elements in,
// wire-ready features (still carrying tags, stripped by main.go's mapping
// step) and the truncated-category list out.
func classifyAndCountFeatures(categories []string, elements []overpassElement) (features []poiFeatureOut, truncated []string) {
	features, countByCategory := parsePOIElements(categories, elements)
	truncated = truncatedCategories(categories, countByCategory)
	return features, truncated
}
