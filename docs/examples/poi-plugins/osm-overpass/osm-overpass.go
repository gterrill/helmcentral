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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
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
	// Geometry and Members are only present in `out geom` responses
	// (shoreline_square): a way's own points, and a relation's members with
	// theirs.
	Geometry []*overpassLatLon `json:"geometry,omitempty"`
	Members  []overpassMember  `json:"members,omitempty"`
}

type overpassMember struct {
	Type     string            `json:"type"`
	Ref      int64             `json:"ref"`
	Role     string            `json:"role"`
	Geometry []*overpassLatLon `json:"geometry,omitempty"`
}

type overpassResponse struct {
	Elements []overpassElement `json:"elements"`
	Remark   string            `json:"remark"`
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

// parseOverpassBody parses an Overpass response, detecting rate limits and
// runtime error remarks. contentType is the response's Content-Type header;
// body is the full response body. Returns the parsed elements, or an error
// for rate limit signatures, runtime error remarks, or JSON parse failures.
func parseOverpassBody(contentType string, body []byte) ([]overpassElement, error) {
	if looksLikeOverpassRateLimit(contentType, body) {
		return nil, fmt.Errorf("overpass rate limited")
	}

	var parsed overpassResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		if looksLikeOverpassRateLimit(contentType, body) {
			return nil, fmt.Errorf("overpass rate limited")
		}
		return nil, fmt.Errorf("failed to parse overpass response: %w", err)
	}

	if strings.HasPrefix(parsed.Remark, "runtime error") {
		return nil, fmt.Errorf("overpass query failed: %s", parsed.Remark)
	}

	return parsed.Elements, nil
}

// overpassStatusError describes a non-200 Overpass answer. A Retry-After
// header (seconds, or an HTTP date) is carried in the text as "retry after
// Ns", because the host only receives the error text and needs the server's
// own wait to pace its downloads.
func overpassStatusError(status int, retryAfter, contentType string, body []byte, now time.Time) error {
	suffix := ""
	if ra := strings.TrimSpace(retryAfter); ra != "" {
		if n, err := strconv.Atoi(ra); err == nil && n > 0 {
			suffix = fmt.Sprintf(", retry after %ds", n)
		} else if at, err := time.Parse(time.RFC1123, ra); err == nil && at.After(now) {
			suffix = fmt.Sprintf(", retry after %ds", int(at.Sub(now).Seconds())+1)
		}
	}
	if looksLikeOverpassRateLimit(contentType, body) {
		return fmt.Errorf("overpass rate limited (HTTP %d with a non-JSON body%s)", status, suffix)
	}
	return fmt.Errorf("overpass returned status %d%s", status, suffix)
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
// defaultDetailLimit on an absent, blank or unparseable value. An explicit
// "0" is honored as-is rather than folded into that fallback: it is the
// operator's way of disabling Wikipedia enrichment entirely (see
// enrichNearestFeatures below and this plugin's osm-overpass.config_fields.json
// "detail_limit" field), not "no limit" and not a typo. A negative value
// makes no sense as a count and falls back to the default like any other
// malformed input.
func resolveDetailLimit(raw string, present bool) int {
	if !present {
		return defaultDetailLimit
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return defaultDetailLimit
	}
	return n
}

// resolveOverpassURL reads the optional "overpass_url" plugin config value,
// defaulting to defaultOverpassAPIURL when config.json carries no such key.
// A present value that is empty or contains only whitespace is treated like
// an absent key and returns the default - this matches the host's own
// blank-value handling for a declared config field, since "overpass_url" is
// this plugin's own operator-editable setting (its
// osm-overpass.config_fields.json sidecar - see this plugin's README and
// docs/adr/0100), resolved fresh by the host on every fetch_poi call
// (wasm_plugin.go's applyConfigValues) and dropped entirely when the
// operator hasn't set it.
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

// enrichmentBudget bounds how much of fetchPOI's total run time - measured
// from the very start of the call, before the Overpass query itself even
// runs - may pass before enrichNearestFeatures stops starting new Wikipedia
// fetches. It exists because the host aborts the *whole* fetch_poi call at
// WASM_PLUGIN_TIMEOUT_MS (backend/wasm_plugin.go, 15s default) if it runs
// long, throwing away Overpass results that were already in hand along with
// the enrichment that was still in flight - worse than returning the same
// POI data promptly with some (or all) Detail/SourceURL fields left empty.
//
// Chosen as 15s minus one worst-case Wikipedia summary (~5s, measured from
// the boat over a slow satellite link) minus 1s of headroom for JSON
// marshaling and the host round trip: 9s. That means a fetch started right
// at the deadline still finishes with about a second to spare before the
// host's own kill switch fires. On a bad link the Overpass query alone can
// take ~6s (measured; buildOverpassPOIQuery's own server-side [timeout:12]
// permits worse still), which already consumes two-thirds of this budget
// before enrichment gets a turn - by design, since the Overpass query's
// result is the actual POI data and always gets to run to completion
// (doOverpassQuery/runFetchPOI below never time it out), while enrichment is
// optional description text layered on top of data that already arrived.
const enrichmentBudget = 9 * time.Second

// enrichNearestFeatures enriches the nearest limit wikipedia-tagged features
// (nearestWikipediaFeatures picks which, nearest-first) with a Wikipedia
// summary each, fetched via fetchSummary. Before starting each fetch it
// checks now() against start.Add(enrichmentBudget) and stops - leaving every
// remaining feature's Detail/SourceURL untouched - once that deadline has
// passed. A feature enrichNearestFeatures never reaches this way carries
// exactly the same "no detail" result as a feature with no wikipedia tag, or
// one whose fetch failed (fetchSummary reports ok=false for any failure -
// network error, non-2xx, unparseable body, no extract - never a
// placeholder string): this is not a masking fallback, since the POI data
// itself (ID/Category/Name/Lat/Lon) is already complete for every feature
// either way, and only this optional descriptive text is ever skipped.
func enrichNearestFeatures(
	features []poiFeatureOut,
	lat, lon float64,
	limit int,
	start time.Time,
	now func() time.Time,
	fetchSummary func(title string) (detail, sourceURL string, ok bool),
) {
	deadline := start.Add(enrichmentBudget)
	for _, idx := range nearestWikipediaFeatures(features, lat, lon, limit) {
		title := wikipediaTitle(features[idx].tags["wikipedia"])
		if title == "" {
			continue
		}
		if !now().Before(deadline) {
			return
		}
		if detail, sourceURL, ok := fetchSummary(title); ok {
			features[idx].Detail = detail
			features[idx].SourceURL = sourceURL
		}
	}
}

// runFetchPOI is fetch_poi's host-testable orchestration: build the query,
// run it through the injected overpassQueryFunc, classify the results, and
// enrich the nearest detailLimit wikipedia-tagged features within
// enrichmentBudget of start (enrichNearestFeatures above). This mirrors
// runPlaceNameAt/runSearchPlaces's own split below: main.go's fetch_poi
// wasmexport supplies doOverpassQuery, fetchWikipediaSummary and time.Now
// for both start and now, while go test exercises this function directly
// with a fake query function (an Overpass error must surface here, before
// enrichment ever runs - never masked as an empty success) and a fake
// clock/fetchSummary (the enrichment time budget) - no network or WASM
// runtime needed for either.
func runFetchPOI(
	query overpassQueryFunc,
	lat, lon float64,
	radiusM int,
	categories []string,
	detailLimit int,
	start time.Time,
	now func() time.Time,
	fetchSummary func(title string) (detail, sourceURL string, ok bool),
) (features []poiFeatureOut, truncated []string, err error) {
	elements, err := query(buildOverpassPOIQuery(lat, lon, radiusM, categories))
	if err != nil {
		return nil, nil, err
	}
	features, truncated = classifyAndCountFeatures(categories, elements)
	enrichNearestFeatures(features, lat, lon, detailLimit, start, now, fetchSummary)
	return features, truncated, nil
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

// ── overpassQueryFunc: shared injection point for the two exports below ───

// overpassQueryFunc posts one already-built Overpass QL query to the
// configured Overpass endpoint and returns its parsed elements, or an error
// covering a transport failure, non-200 status, a detected rate limit, or a
// JSON parse failure. Injected rather than calling the pdk HTTP client
// directly, so place_name_at's and search_places' orchestration below
// (runPlaceNameAt, runSearchPlaces) - where which query to send next
// depends on the previous one's result - stays testable with `go test`,
// exactly as backend/assistant_tools.go injects an overpassFetcher. main.go's
// wasmexport functions supply doOverpassQuery, the real implementation using
// pdk.NewHTTPRequest.
type overpassQueryFunc func(query string) ([]overpassElement, error)

// ── place_name_at ──────────────────────────────────────────────────────
//
// Ports backend/place_name.go's ring query and ranking (ADR 0056), extended
// with marinas and harbours: a five-clause tag set (anchorage, marina,
// harbour, bay, island/islet/rock), ranked anchorage-first with ties broken
// by distance. Unlike place_name.go, this
// export does not itself walk a widening ladder of rings - the host passes
// one radius per call and is expected to call again with a wider radius
// when this one comes back with no name, exactly as backend/place_name.go's
// resolvePlaceName does today for placeNameRadiiMeters.

// placeFeatureRank orders candidate kinds within place_name_at's ring:
// anchorage first (a human already decided this is where you anchor), then
// marina (a named berth, and only within placeNameMarinaMaxMeters, where
// the boat is almost certainly in it), then bay, then island/islet/rock by descending size-implication.
// A tagged element whose kind isn't one of these six is discarded before
// ranking, as is any element with no name tag.
var placeFeatureRank = map[string]int{
	"anchorage": 0,
	"marina":    1,
	"bay":       2,
	"island":    3,
	"islet":     4,
	"rock":      5,
}

// placeNameMarinaMaxMeters is how close a marina or harbour must be to name
// the boat. Inside it the boat is berthed there; past it a marina ranked
// ahead of bays would name a boat anchored in a nearby bay after the port
// across the water. 500 m allows for a large marina whose centre sits a few
// hundred metres from the outer pontoons.
const placeNameMarinaMaxMeters = 500

// placeFeatureKind classifies a place_name_at element's tags into one of
// placeFeatureRank's keys, or "" if it matches none of buildPlaceNameQuery's
// clauses. leisure=marina and seamark:type=harbour are one kind, so an
// element carrying both is a single candidate.
func placeFeatureKind(tags map[string]string) string {
	if tags["seamark:type"] == "anchorage" {
		return "anchorage"
	}
	if tags["leisure"] == "marina" || tags["seamark:type"] == "harbour" {
		return "marina"
	}
	if tags["natural"] == "bay" {
		return "bay"
	}
	switch tags["place"] {
	case "island", "islet", "rock":
		return tags["place"]
	}
	return ""
}

// buildPlaceNameQuery builds the Overpass QL for one place_name_at ring at
// radiusM around (lat, lon): the anchorage, marina, harbour, bay and
// island/islet/rock clauses, plus this plugin's own
// global [bbox:...] setting (overpassBoundingBox) when the ring doesn't
// cross the antimeridian or a pole - see overpassBoundingBox's doc comment
// for why that materially speeds up some mirrors. The 12s server-side
// timeout matches buildOverpassPOIQuery's own budget below the host's 15s
// WASM plugin call ceiling (backend/wasm_plugin.go's
// WASM_PLUGIN_TIMEOUT_MS): place_name_at is a single Overpass round trip
// per call, so it can use the same budget fetch_poi does. The marina and
// harbour clauses never reach past placeNameMarinaMaxMeters, since
// bestNamedPlaceFeature discards anything further anyway. There is no
// output cap: Overpass returns elements in type-and-id order, not by rank
// or distance, so a cap could drop the winner before ranking sees it.
func buildPlaceNameQuery(lat, lon float64, radiusM int) string {
	around := fmt.Sprintf("around:%d,%.6f,%.6f", radiusM, lat, lon)
	berth := fmt.Sprintf("around:%d,%.6f,%.6f", min(radiusM, placeNameMarinaMaxMeters), lat, lon)

	var b strings.Builder
	b.WriteString("[out:json][timeout:12]")
	if south, west, north, east, ok := overpassBoundingBox(lat, lon, radiusM); ok {
		fmt.Fprintf(&b, "[bbox:%.6f,%.6f,%.6f,%.6f]", south, west, north, east)
	}
	fmt.Fprintf(&b, `;(nwr["seamark:type"="anchorage"](%s);nwr["leisure"="marina"](%s);nwr["seamark:type"="harbour"](%s);nwr["natural"="bay"](%s);nwr["place"~"^(island|islet|rock)$"](%s););out tags center;`, around, berth, berth, around, around)
	return b.String()
}

// placeNameWinner is the single named feature place_name_at reports.
type placeNameWinner struct {
	Name string
	Kind string
	Lat  float64
	Lon  float64
}

// bestNamedPlaceFeature ranks the named, tag-matched elements in a ring and
// returns the winner: lowest placeFeatureRank first, ties broken by
// great-circle distance to the query point - mirrors
// backend/place_name.go's bestNamedFeature exactly (see
// backend/place_name_test.go's ranking tests, ported above). Elements with
// no name tag are discarded before ranking, so an unnamed feature can never
// win; ok is false when nothing in elements carries both a name and one of
// the six recognised kinds, which is a legitimate empty result, not a
// failure.
func bestNamedPlaceFeature(elements []overpassElement, lat, lon float64) (placeNameWinner, bool) {
	type candidate struct {
		winner placeNameWinner
		rank   int
		dist   float64
	}

	var candidates []candidate
	for _, el := range elements {
		name := strings.TrimSpace(el.Tags["name"])
		if name == "" {
			continue
		}
		kind := placeFeatureKind(el.Tags)
		rank, known := placeFeatureRank[kind]
		if !known {
			continue
		}
		elLat, elLon, ok := elementLatLon(el)
		if !ok {
			continue
		}
		dist := haversineMeters(lat, lon, elLat, elLon)
		if kind == "marina" && dist > placeNameMarinaMaxMeters {
			continue
		}
		candidates = append(candidates, candidate{
			winner: placeNameWinner{Name: name, Kind: kind, Lat: elLat, Lon: elLon},
			rank:   rank,
			dist:   dist,
		})
	}
	if len(candidates) == 0 {
		return placeNameWinner{}, false
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank < candidates[j].rank
		}
		return candidates[i].dist < candidates[j].dist
	})
	return candidates[0].winner, true
}

// placeNameAtInput mirrors the host's place_name_at call contract.
type placeNameAtInput struct {
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	RadiusM int     `json:"radius_m"`
}

// placeNameAtOutput is place_name_at's output. Lat/Lon are pointers so a
// winner sitting at exactly 0 latitude or longitude still round-trips as a
// real coordinate rather than being confused with "no winner" - "nothing
// named" is instead represented by Kind/Lat/Lon all being absent (nil/""),
// which marshals to exactly {"name":""} per the plugin contract.
type placeNameAtOutput struct {
	Name string   `json:"name"`
	Kind string   `json:"kind,omitempty"`
	Lat  *float64 `json:"lat,omitempty"`
	Lon  *float64 `json:"lon,omitempty"`
}

// runPlaceNameAt is place_name_at's entry point: build the query, post it
// via the injected overpassQueryFunc, and rank the result. post's error
// (transport, non-200, rate limit, or parse failure - see doOverpassQuery in
// main.go) is returned as-is, never masked as an empty success; nothing
// named is a legitimate {"name": ""} result, never an error.
func runPlaceNameAt(post overpassQueryFunc, lat, lon float64, radiusM int) (placeNameAtOutput, error) {
	elements, err := post(buildPlaceNameQuery(lat, lon, radiusM))
	if err != nil {
		return placeNameAtOutput{}, err
	}

	winner, ok := bestNamedPlaceFeature(elements, lat, lon)
	if !ok {
		return placeNameAtOutput{Name: ""}, nil
	}
	return placeNameAtOutput{Name: winner.Name, Kind: winner.Kind, Lat: &winner.Lat, Lon: &winner.Lon}, nil
}

// ── search_places ──────────────────────────────────────────────────────
//
// Ports backend/assistant_tools.go's find_places two-rung ladder (ADR 0093):
// rung 1 is always an exact nwr["name"="<variant>"] match over a 100nm box
// (findPlacesExactNameVariants); rung 2 - a case-insensitive partial-name
// regex over a tighter 20nm box - only runs when rung 1 found nothing AND
// the host says to broaden the search (runSearchPlaces' broad parameter).
// The host, not this plugin, decides broad: it knows about local data this
// plugin cannot see (saved route waypoints, most importantly), and folds
// that into whether a broader OSM search is even worth running. A query
// carrying a comma-qualifier gets one extra exact-name lookup for the
// qualifier before rung 2, to re-centre rung 2's box on the qualifier's
// resolved position when it sits well outside the ordinary centre (a bay
// named for the island it is on, tens of miles away).
//
// Unlike find_places itself, this plugin never computes distance, bearing,
// dedupe or sort order - ADR 0091's "provider returns raw features, host
// computes distance/bearing" rule applies here too - so runSearchPlaces
// returns whichever rung's raw matches were found (capped at
// searchPlacesResultCap) and leaves ranking/trimming to the host.

const (
	searchPlacesExactRadiusNm = 100.0
	searchPlacesRegexRadiusNm = 20.0
)

// searchPlacesResultCap bounds how many raw matches runSearchPlaces ever
// returns, independent of the caller's max_results. The host sorts by
// distance and trims to max_results only after seeing every match, so
// capping to max_results here - before that sort - could silently drop the
// actual nearest feature whenever Overpass's own element order doesn't
// happen to correlate with distance.
const searchPlacesResultCap = 50

// Per-query server-side timeouts. Up to three Overpass round trips can
// happen in one search_places call (rung 1, the qualifier lookup, rung 2),
// so each gets its own tight budget rather than
// backend/assistant_tools.go's shared 25s - the sum (13s) must stay
// comfortably under the host's 15s WASM plugin call budget
// (backend/wasm_plugin.go's WASM_PLUGIN_TIMEOUT_MS), leaving margin for
// three separate HTTP connections and JSON parsing on top of whatever
// Overpass itself takes. Measured live against overpass.openstreetmap.fr on
// 2026-09-11 (backend/assistant_tools.go, ADR 0093 section 8): an
// exact-name match answers in about 1s even at the full 100nm rung 1 radius
// (Overpass uses its name index directly), and the rung 2 regex union
// answers in 2 to 5s at 20nm (not indexable, a full scan of the box).
const (
	searchPlacesExactTimeoutSeconds     = 4
	searchPlacesQualifierTimeoutSeconds = 3
	searchPlacesRegexTimeoutSeconds     = 6
)

// searchPlacesBoundingBox mirrors backend/assistant_tools.go's
// assistantBoundingBox exactly: dLat=radiusNm/60 because a nautical mile is
// one minute of latitude; dLon widens by 1/cos(lat) so the box holds
// radiusNm of longitude constant at this latitude. lat is clamped to
// [-90,90]; lon is deliberately left unwrapped across the antimeridian,
// same as the backend function this ports (find_places has no
// antimeridian handling either).
func searchPlacesBoundingBox(lat, lon, radiusNm float64) (south, west, north, east float64) {
	dLat := radiusNm / 60.0
	dLon := radiusNm / (60.0 * math.Cos(lat*math.Pi/180))

	south = lat - dLat
	north = lat + dLat
	if south < -90 {
		south = -90
	}
	if north > 90 {
		north = 90
	}
	west = lon - dLon
	east = lon + dLon
	return south, west, north, east
}

// searchPlacesBBoxClause formats a (south, west, north, east) box as the
// Overpass QL bbox filter argument - mirrors
// backend/assistant_tools.go's overpassBoundingBoxClause.
func searchPlacesBBoxClause(south, west, north, east float64) string {
	return fmt.Sprintf("%.4f,%.4f,%.4f,%.4f", south, west, north, east)
}

// titleCaseWords title-cases every space-separated word - mirrors
// backend/assistant_tools.go's assistantTitleCase exactly, including why it
// isn't built on the deprecated strings.Title (that function's doc comment
// explains the apostrophe/hyphen mid-word case this avoids).
func titleCaseWords(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		r := []rune(strings.ToLower(w))
		if len(r) > 0 {
			r[0] = unicode.ToUpper(r[0])
		}
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

// firstLetterUpperCase upper-cases only s's first rune, leaving everything
// else exactly as typed - mirrors backend/assistant_tools.go's
// assistantFirstLetterUpperCase.
func firstLetterUpperCase(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// findPlacesExactNameVariants mirrors backend/assistant_tools.go's
// assistantExactNameVariants exactly: the query as typed (trimmed, internal
// whitespace collapsed), title case, first-letter-only capitalised, and -
// for a query carrying a comma-qualifier ("Bona Bay, Gloucester Island") -
// the same three variants of just the head before the comma too, since
// OSM's own name tag almost never carries the qualifier.
func findPlacesExactNameVariants(query string) []string {
	normalized := strings.Join(strings.Fields(query), " ")
	if normalized == "" {
		return nil
	}

	candidates := []string{
		normalized,
		titleCaseWords(normalized),
		firstLetterUpperCase(normalized),
	}

	if head, _, ok := strings.Cut(normalized, ","); ok {
		if headNormalized := strings.Join(strings.Fields(head), " "); headNormalized != "" {
			candidates = append(candidates,
				headNormalized,
				titleCaseWords(headNormalized),
				firstLetterUpperCase(headNormalized),
			)
		}
	}

	seen := make(map[string]bool, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, v := range candidates {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// findPlacesQualifier returns the trimmed text after a query's first comma
// ("Bona Bay, Gloucester Island" -> "Gloucester Island"), or "" when the
// query carries no comma or the qualifier is blank - mirrors
// backend/assistant_tools.go's assistantFindPlacesQualifier.
func findPlacesQualifier(query string) string {
	_, tail, ok := strings.Cut(query, ",")
	if !ok {
		return ""
	}
	return strings.TrimSpace(tail)
}

// escapeOverpassQLStringLiteral escapes a string for embedding inside an
// Overpass QL double-quoted string literal - mirrors
// backend/assistant_tools.go's escapeOverpassStringLiteral.
func escapeOverpassQLStringLiteral(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// buildFindPlacesExactQuery builds rung 1's (and the qualifier lookup's)
// Overpass QL: an untagged, exact nwr["name"="<variant>"] clause per
// findPlacesExactNameVariants, unioned over a bbox - mirrors
// backend/assistant_tools.go's buildOverpassExactNameQuery, parameterised
// on timeoutSeconds instead of a fixed 25s (see the timeout constants
// above). There is deliberately no tag filter: kind is derived host-side
// afterward (findPlacesKind) from whatever tag happens to be present, and
// nothing named is discarded for want of a recognised kind.
func buildFindPlacesExactQuery(query string, south, west, north, east float64, timeoutSeconds int) string {
	bbox := searchPlacesBBoxClause(south, west, north, east)

	var b strings.Builder
	fmt.Fprintf(&b, "[out:json][timeout:%d];(", timeoutSeconds)
	for _, variant := range findPlacesExactNameVariants(query) {
		fmt.Fprintf(&b, `nwr["name"="%s"](%s);`, escapeOverpassQLStringLiteral(variant), bbox)
	}
	b.WriteString(");out tags center 30;")
	return b.String()
}

// buildFindPlacesRegexQuery builds rung 2's Overpass QL: a partial,
// case-insensitive name search over a bbox, matching the same four tag
// clauses backend/assistant_tools.go's buildOverpassNameSearchQuery does
// (natural coastal features, place types, seamark facilities, and OSM's
// separate leisure=marina tagging), parameterised on timeoutSeconds instead
// of a fixed 25s. query is regexp.QuoteMeta-escaped and then escaped again
// for the Overpass QL string literal syntax, so a query containing e.g.
// parentheses or a literal quote can never break out of the generated
// query.
func buildFindPlacesRegexQuery(query string, south, west, north, east float64, timeoutSeconds int) string {
	q := escapeOverpassQLStringLiteral(regexp.QuoteMeta(query))
	bbox := searchPlacesBBoxClause(south, west, north, east)
	return fmt.Sprintf(
		`[out:json][timeout:%d];(nwr["name"~"%s",i]["natural"~"^(bay|reef|beach|cape|strait|peninsula|shoal|inlet)$"](%s);nwr["name"~"%s",i]["place"~"^(island|islet|locality|hamlet|village|town|archipelago)$"](%s);nwr["name"~"%s",i]["seamark:type"~"^(anchorage|harbour|mooring|marina|small_craft_facility)$"](%s);nwr["name"~"%s",i]["leisure"="marina"](%s););out tags center 30;`,
		timeoutSeconds, q, bbox, q, bbox, q, bbox, q, bbox,
	)
}

// findPlacesKind classifies a search_places element by tag priority:
// seamark:type (a human already decided this is a marine facility), then
// natural, then place, then leisure - mirrors backend/assistant_tools.go's
// overpassFindPlacesKind. Rung 1's exact-name query carries no tag filter,
// so an element can genuinely have none of these four tags; that falls back
// to "feature" rather than being discarded, since search_places' whole
// point is surfacing every named match and letting the model judge
// relevance, not silently dropping anything whose kind it doesn't
// recognise.
func findPlacesKind(tags map[string]string) string {
	if v := strings.TrimSpace(tags["seamark:type"]); v != "" {
		return v
	}
	if v := strings.TrimSpace(tags["natural"]); v != "" {
		return v
	}
	if v := strings.TrimSpace(tags["place"]); v != "" {
		return v
	}
	if v := strings.TrimSpace(tags["leisure"]); v != "" {
		return v
	}
	return "feature"
}

// searchPlaceMatch is one raw search_places result - no distance, bearing
// or source label, since the host computes those itself (ADR 0091).
type searchPlaceMatch struct {
	Name string  `json:"name"`
	Kind string  `json:"kind"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

// elementsToSearchMatches converts raw Overpass elements into search_places
// matches, discarding any element with no resolvable point or no name tag -
// mirrors backend/assistant_tools.go's
// assistantOverpassElementsToCandidates minus the distance/bearing/source
// fields the host computes instead.
func elementsToSearchMatches(elements []overpassElement) []searchPlaceMatch {
	var out []searchPlaceMatch
	for _, el := range elements {
		lat, lon, ok := elementLatLon(el)
		if !ok {
			continue
		}
		name := strings.TrimSpace(el.Tags["name"])
		if name == "" {
			continue
		}
		out = append(out, searchPlaceMatch{Name: name, Kind: findPlacesKind(el.Tags), Lat: lat, Lon: lon})
	}
	return out
}

// nearestSearchMatch returns the match closest to (lat, lon), or false when
// matches is empty - used to pick which resolved qualifier hit should
// centre rung 2's search, when the qualifier query returns more than one
// candidate. Mirrors backend/assistant_tools.go's
// nearestAssistantPlaceCandidate.
func nearestSearchMatch(matches []searchPlaceMatch, lat, lon float64) (searchPlaceMatch, bool) {
	if len(matches) == 0 {
		return searchPlaceMatch{}, false
	}
	nearest := matches[0]
	nearestDist := haversineMeters(lat, lon, nearest.Lat, nearest.Lon)
	for _, m := range matches[1:] {
		if d := haversineMeters(lat, lon, m.Lat, m.Lon); d < nearestDist {
			nearest, nearestDist = m, d
		}
	}
	return nearest, true
}

// searchPlacesInput mirrors the host's search_places call contract.
// MaxResults is accepted but deliberately unused here: the host sorts every
// raw match by distance and trims to max_results only after that sort (see
// searchPlacesResultCap's doc comment), so trimming to it earlier, inside
// this plugin, could drop the actual nearest feature.
type searchPlacesInput struct {
	Query      string  `json:"query"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	MaxResults int     `json:"max_results"`
	Broad      bool    `json:"broad"`
}

// searchPlacesCentre names and locates the qualifier feature rung 2's box
// was centred on - only present when a comma-qualifier resolved (see
// runSearchPlaces).
type searchPlacesCentre struct {
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

// searchPlacesOutput is search_places' output. CentredOn is a pointer (not
// omitempty) so it marshals to JSON null, matching the plugin contract's
// "centred_on: {...} | null", rather than being silently dropped.
type searchPlacesOutput struct {
	Search    string              `json:"search"`
	RadiusNm  float64             `json:"radius_nm"`
	CentredOn *searchPlacesCentre `json:"centred_on"`
	Results   []searchPlaceMatch  `json:"results"`
	Note      string              `json:"note"`
}

// runSearchPlaces is search_places' entry point, orchestrating up to three
// Overpass round trips through the injected overpassQueryFunc (real HTTP in
// main.go's wasmexport, a fake in tests) - see this section's package doc
// comment above for the two-rung ladder this ports from
// backend/assistant_tools.go's find_places.
func runSearchPlaces(post overpassQueryFunc, input searchPlacesInput) (searchPlacesOutput, error) {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return searchPlacesOutput{}, fmt.Errorf("search_places: query is required")
	}

	exactSouth, exactWest, exactNorth, exactEast := searchPlacesBoundingBox(input.Lat, input.Lon, searchPlacesExactRadiusNm)
	exactElements, err := post(buildFindPlacesExactQuery(query, exactSouth, exactWest, exactNorth, exactEast, searchPlacesExactTimeoutSeconds))
	if err != nil {
		return searchPlacesOutput{}, fmt.Errorf("search_places: rung 1: %w", err)
	}
	exactMatches := elementsToSearchMatches(exactElements)

	var regexMatches []searchPlaceMatch
	var centredOn *searchPlacesCentre
	var note string
	centreLat, centreLon := input.Lat, input.Lon
	radiusNm := searchPlacesExactRadiusNm

	// Rung 2 only runs when rung 1 found nothing AND the host says to
	// broaden the search (input.Broad) - the host, not this plugin, knows
	// whether e.g. a saved route waypoint already answered the query, and
	// folds that into whether the expensive regex rung is worth running at
	// all.
	if len(exactMatches) == 0 && input.Broad {
		if qualifier := findPlacesQualifier(query); qualifier != "" {
			qSouth, qWest, qNorth, qEast := searchPlacesBoundingBox(input.Lat, input.Lon, searchPlacesExactRadiusNm)
			qElements, qerr := post(buildFindPlacesExactQuery(qualifier, qSouth, qWest, qNorth, qEast, searchPlacesQualifierTimeoutSeconds))
			if qerr != nil {
				return searchPlacesOutput{}, fmt.Errorf("search_places: qualifier lookup: %w", qerr)
			}
			if nearest, ok := nearestSearchMatch(elementsToSearchMatches(qElements), input.Lat, input.Lon); ok {
				centreLat, centreLon = nearest.Lat, nearest.Lon
				centredOn = &searchPlacesCentre{Name: nearest.Name, Lat: nearest.Lat, Lon: nearest.Lon}
			} else {
				note = fmt.Sprintf("qualifier %q did not resolve to a named feature; rung 2 searched from the given position instead", qualifier)
			}
		}

		regexSouth, regexWest, regexNorth, regexEast := searchPlacesBoundingBox(centreLat, centreLon, searchPlacesRegexRadiusNm)
		regexElements, err := post(buildFindPlacesRegexQuery(query, regexSouth, regexWest, regexNorth, regexEast, searchPlacesRegexTimeoutSeconds))
		if err != nil {
			return searchPlacesOutput{}, fmt.Errorf("search_places: rung 2: %w", err)
		}
		regexMatches = elementsToSearchMatches(regexElements)
		radiusNm = searchPlacesRegexRadiusNm
	}

	search := "none"
	var results []searchPlaceMatch
	switch {
	case len(exactMatches) > 0:
		search = "exact"
		results = exactMatches
	case len(regexMatches) > 0:
		search = "regex"
		results = regexMatches
	}

	if len(results) > searchPlacesResultCap {
		results = results[:searchPlacesResultCap]
	}
	if results == nil {
		results = []searchPlaceMatch{}
	}

	return searchPlacesOutput{
		Search:    search,
		RadiusNm:  radiusNm,
		CentredOn: centredOn,
		Results:   results,
		Note:      note,
	}, nil
}

// ── shoreline_square and shoreline_changes ─────────────────────────────
//
// Raw coastline and reef geometry for the host's offline coastline (ADR
// 0203), fetched one coverage square at a time. ADR 0091's rule applies: this
// returns lines, the host casts every ray and computes every distance.
//
// shoreline_square is one Overpass round trip for one box. The global
// [bbox:...] is the spatial index (a tag-first scan of natural=coastline
// times out on the mirrors). Coastline is output clipped to the box, so two
// neighbouring squares never repeat it; reefs are output whole, so a ring
// that crosses a square edge stays closed in both squares and the host
// dedupes it by id. Because the boat link is slow, every line is thinned to
// shorelineMinSpacingM between points before it leaves the plugin.
//
// shoreline_changes asks how many coastline and reef elements in the box were
// edited after a date: the host's weekly check on a square it holds.

const (
	// shorelineTimeoutSeconds is Overpass's own budget for one square. The
	// host gives a background call longer than this; the interactive 15 s
	// plugin budget is not enough for a square of coastline.
	shorelineTimeoutSeconds = 100

	// shorelineChangesTimeoutSeconds is the budget for the count query.
	shorelineChangesTimeoutSeconds = 60

	// shorelineMinSpacingM drops points closer than this to the last point
	// kept. Rays are miles long, so a metre of coastline detail is noise, but
	// the anchor is often a few hundred metres from shore, so the spacing
	// stays small. First and last points are always kept, so a ring stays a
	// ring.
	shorelineMinSpacingM = 15.0

	// shorelineMaxSpanDeg bounds a requested box. The host asks for one degree
	// squares; anything much bigger is a mistake that would time out slowly.
	shorelineMaxSpanDeg = 2.0
)

type shorelineBoxInput struct {
	South float64 `json:"south"`
	West  float64 `json:"west"`
	North float64 `json:"north"`
	East  float64 `json:"east"`
}

type shorelineChangesInput struct {
	South float64 `json:"south"`
	West  float64 `json:"west"`
	North float64 `json:"north"`
	East  float64 `json:"east"`
	Since string  `json:"since"` // RFC 3339
}

func (in shorelineChangesInput) box() shorelineBoxInput {
	return shorelineBoxInput{South: in.South, West: in.West, North: in.North, East: in.East}
}

type shorelineLineOut struct {
	ID     string       `json:"id"`
	Kind   string       `json:"kind"`
	Name   string       `json:"name"`
	Closed bool         `json:"closed"`
	Points [][2]float64 `json:"points"`
}

type shorelineSquareOutput struct {
	Lines []shorelineLineOut `json:"lines"`
}

type shorelineChangesOutput struct {
	Changed int `json:"changed"`
}

func (b shorelineBoxInput) validate() error {
	if b.South < -90 || b.North > 90 || b.West < -180 || b.East > 180 {
		return fmt.Errorf("shoreline: box %.4f,%.4f,%.4f,%.4f is outside the world (the antimeridian and the poles are not supported)", b.South, b.West, b.North, b.East)
	}
	if b.North <= b.South || b.East <= b.West {
		return fmt.Errorf("shoreline: box %.4f,%.4f,%.4f,%.4f has no area", b.South, b.West, b.North, b.East)
	}
	if b.North-b.South > shorelineMaxSpanDeg || b.East-b.West > shorelineMaxSpanDeg {
		return fmt.Errorf("shoreline: box %.4f,%.4f,%.4f,%.4f is larger than %.0f degrees", b.South, b.West, b.North, b.East, shorelineMaxSpanDeg)
	}
	return nil
}

func buildShorelineQuery(b shorelineBoxInput) (string, error) {
	if err := b.validate(); err != nil {
		return "", err
	}
	// Coastline is output clipped to the box (`out geom(bbox)`: nodes outside
	// come back as null), which is most of the payload on a long coast. Reefs
	// are output whole so a ring stays closed.
	return fmt.Sprintf(`[out:json][timeout:%d][bbox:%.6f,%.6f,%.6f,%.6f];way["natural"="coastline"]->.c;(way["natural"="reef"];relation["natural"="reef"];)->.r;.c out geom(%.6f,%.6f,%.6f,%.6f);.r out geom;`,
		shorelineTimeoutSeconds, b.South, b.West, b.North, b.East, b.South, b.West, b.North, b.East), nil
}

// buildShorelineChangesQuery counts coastline and reef elements edited after
// since. `newer` matches an element whose own timestamp is later, so it sees
// new and edited ways and relations; it cannot see a node moved inside an
// unedited way, or an element deleted.
func buildShorelineChangesQuery(b shorelineBoxInput, since time.Time) (string, error) {
	if err := b.validate(); err != nil {
		return "", err
	}
	if since.IsZero() {
		return "", fmt.Errorf("shoreline_changes: since is required")
	}
	ts := since.UTC().Format(time.RFC3339)
	return fmt.Sprintf(`[out:json][timeout:%d][bbox:%.6f,%.6f,%.6f,%.6f];(way["natural"="coastline"](newer:"%s");way["natural"="reef"](newer:"%s");relation["natural"="reef"](newer:"%s"););out count;`,
		shorelineChangesTimeoutSeconds, b.South, b.West, b.North, b.East, ts, ts, ts), nil
}

func roundShorelineCoord(v float64) float64 { return math.Round(v*1e5) / 1e5 }

// thinShoreline drops points within shorelineMinSpacingM of the last point
// kept, always keeping the first and last.
func thinShoreline(pts []overpassLatLon) [][2]float64 {
	if len(pts) == 0 {
		return nil
	}
	out := make([][2]float64, 0, len(pts))
	last := pts[0]
	out = append(out, [2]float64{roundShorelineCoord(last.Lat), roundShorelineCoord(last.Lon)})
	for i := 1; i < len(pts)-1; i++ {
		if haversineMeters(last.Lat, last.Lon, pts[i].Lat, pts[i].Lon) < shorelineMinSpacingM {
			continue
		}
		last = pts[i]
		out = append(out, [2]float64{roundShorelineCoord(last.Lat), roundShorelineCoord(last.Lon)})
	}
	if len(pts) > 1 {
		end := pts[len(pts)-1]
		out = append(out, [2]float64{roundShorelineCoord(end.Lat), roundShorelineCoord(end.Lon)})
	}
	return out
}

// shorelineRuns splits a geometry array at the nulls Overpass puts where a
// node lies outside `out geom(bbox)`, then keeps from each piece the runs of
// consecutive segments whose bounding box overlaps the search box. A segment
// that crosses the box with both ends outside it is kept, so a long straight
// coast is not lost.
func shorelineRuns(geom []*overpassLatLon, south, west, north, east float64) [][]overpassLatLon {
	var pieces [][]overpassLatLon
	var piece []overpassLatLon
	for _, p := range geom {
		if p == nil {
			if piece != nil {
				pieces = append(pieces, piece)
				piece = nil
			}
			continue
		}
		piece = append(piece, *p)
	}
	if piece != nil {
		pieces = append(pieces, piece)
	}

	var runs [][]overpassLatLon
	for _, pts := range pieces {
		var cur []overpassLatLon
		for i := 1; i < len(pts); i++ {
			a, b := pts[i-1], pts[i]
			overlaps := !(math.Max(a.Lat, b.Lat) < south || math.Min(a.Lat, b.Lat) > north ||
				math.Max(a.Lon, b.Lon) < west || math.Min(a.Lon, b.Lon) > east)
			if overlaps {
				if cur == nil {
					cur = []overpassLatLon{a}
				}
				cur = append(cur, b)
				continue
			}
			if cur != nil {
				runs = append(runs, cur)
				cur = nil
			}
		}
		if cur != nil {
			runs = append(runs, cur)
		}
	}
	return runs
}

// wholeGeometry returns geom as values, or an error if any node is null: a
// reef ring with a missing node is not a ring, and guessing would hide it.
func wholeGeometry(id string, geom []*overpassLatLon) ([]overpassLatLon, error) {
	out := make([]overpassLatLon, len(geom))
	for i, p := range geom {
		if p == nil {
			return nil, fmt.Errorf("shoreline_square: %s has a node with no position", id)
		}
		out[i] = *p
	}
	return out, nil
}

// assembleOuterRings joins the outer member ways of a multipolygon end to end
// into rings. Ways meet where an end point has exactly the same coordinates
// as another way's end (Overpass gives shared nodes identical positions), in
// either direction. A chain that does not close is returned as it stands, open,
// so the host never treats it as a ring.
func assembleOuterRings(ways [][]overpassLatLon) [][]overpassLatLon {
	used := make([]bool, len(ways))
	var rings [][]overpassLatLon
	for i := range ways {
		if used[i] {
			continue
		}
		used[i] = true
		chain := append([]overpassLatLon(nil), ways[i]...)
		for chain[0] != chain[len(chain)-1] {
			joined := false
			for j := range ways {
				if used[j] {
					continue
				}
				w := ways[j]
				end := chain[len(chain)-1]
				switch {
				case w[0] == end:
					chain = append(chain, w[1:]...)
				case w[len(w)-1] == end:
					for k := len(w) - 2; k >= 0; k-- {
						chain = append(chain, w[k])
					}
				default:
					continue
				}
				used[j] = true
				joined = true
				break
			}
			if !joined {
				break
			}
		}
		rings = append(rings, chain)
	}
	return rings
}

func runShorelineSquare(post overpassQueryFunc, box shorelineBoxInput) (shorelineSquareOutput, error) {
	query, err := buildShorelineQuery(box)
	if err != nil {
		return shorelineSquareOutput{}, err
	}
	south, west, north, east := box.South, box.West, box.North, box.East

	elements, err := post(query)
	if err != nil {
		return shorelineSquareOutput{}, fmt.Errorf("shoreline_square: %w", err)
	}

	lines := []shorelineLineOut{}
	addReef := func(id, name string, raw []*overpassLatLon) error {
		if len(raw) < 2 {
			return nil
		}
		geom, err := wholeGeometry(id, raw)
		if err != nil {
			return err
		}
		closed := geom[0] == geom[len(geom)-1]
		lines = append(lines, shorelineLineOut{ID: id, Kind: "reef", Name: name, Closed: closed, Points: thinShoreline(geom)})
		return nil
	}

	for _, el := range elements {
		kind := el.Tags["natural"]
		name := strings.TrimSpace(el.Tags["name"])
		switch {
		case el.Type == "way" && kind == "coastline":
			id := fmt.Sprintf("way/%d", el.ID)
			for _, run := range shorelineRuns(el.Geometry, south, west, north, east) {
				lines = append(lines, shorelineLineOut{
					ID: id, Kind: "coastline", Name: name,
					Closed: len(run) == len(el.Geometry) && run[0] == run[len(run)-1],
					Points: thinShoreline(run),
				})
			}
		case el.Type == "way" && kind == "reef":
			if err := addReef(fmt.Sprintf("way/%d", el.ID), name, el.Geometry); err != nil {
				return shorelineSquareOutput{}, err
			}
		case el.Type == "relation" && kind == "reef":
			var ways [][]overpassLatLon
			for i, m := range el.Members {
				if m.Type != "way" || m.Role != "outer" || len(m.Geometry) < 2 {
					continue
				}
				geom, err := wholeGeometry(fmt.Sprintf("relation/%d/%d", el.ID, i), m.Geometry)
				if err != nil {
					return shorelineSquareOutput{}, err
				}
				ways = append(ways, geom)
			}
			for i, ring := range assembleOuterRings(ways) {
				lines = append(lines, shorelineLineOut{
					ID: fmt.Sprintf("relation/%d/%d", el.ID, i), Kind: "reef", Name: name,
					Closed: len(ring) > 2 && ring[0] == ring[len(ring)-1], Points: thinShoreline(ring),
				})
			}
		}
	}
	return shorelineSquareOutput{Lines: lines}, nil
}

// runShorelineChanges returns how many coastline and reef elements in the box
// changed after since. A response with no count element is an error, never
// zero edits.
func runShorelineChanges(post overpassQueryFunc, box shorelineBoxInput, since time.Time) (shorelineChangesOutput, error) {
	query, err := buildShorelineChangesQuery(box, since)
	if err != nil {
		return shorelineChangesOutput{}, err
	}
	elements, err := post(query)
	if err != nil {
		return shorelineChangesOutput{}, fmt.Errorf("shoreline_changes: %w", err)
	}
	for _, el := range elements {
		if el.Type != "count" {
			continue
		}
		total, err := strconv.Atoi(el.Tags["total"])
		if err != nil || total < 0 {
			return shorelineChangesOutput{}, fmt.Errorf("shoreline_changes: unreadable count %q", el.Tags["total"])
		}
		return shorelineChangesOutput{Changed: total}, nil
	}
	return shorelineChangesOutput{}, fmt.Errorf("shoreline_changes: overpass returned no count")
}
