package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// This file implements the three read-only tools the onboard assistant's
// agentic loop (assistant_run.go) can call (ADR 0093): find_places,
// get_wind_forecast and get_tides. Every dependency is injected via
// assistantToolDeps so tests never need a live SignalK connection, a
// registered WASM plugin, or a real Overpass round trip - the same
// injectable-func idiom forecastWarningsFetcher (forecast_warnings_fetcher.go)
// already uses.

// assistantFindPlacesRadiusNm is find_places' rung 1 (exact name match)
// search radius. 100nm is generous enough to cover a multi-day passage's
// worth of candidate anchorages without the operator ever having to think
// about a radius parameter - there is deliberately no way for the model to
// widen or narrow it. Measured live against overpass.openstreetmap.fr (the
// mirror the boat actually reaches) on 2026-09-11: an untagged exact-name
// match answers in about 1s even at this radius, because Overpass can use
// its name index directly instead of scanning every element in the box
// against a regex - see ADR 0093 section 8.
const assistantFindPlacesRadiusNm = 100.0

// assistantFindPlacesRegexRadiusNm is find_places' rung 2 (partial name,
// regex) search radius - far tighter than rung 1's, because a name regex is
// not indexable and costs Overpass a full scan of every element in the box.
// The same 2026-09-11 measurement against overpass.openstreetmap.fr: the
// four-clause regex union answers in 2 to 5s at 20nm, and times out
// server-side (55 to 65s, "Query timed out") somewhere past about 20nm - see
// ADR 0093 section 8.
const assistantFindPlacesRegexRadiusNm = 20.0

// assistantMaxForecastDays bounds get_wind_forecast.days. Ten days matches
// weatherForecast's own cap (weather_providers.go).
const assistantMaxForecastDays = 10

// assistantMaxTideDays bounds get_tides.days. Eight days is what the BOM
// tide provider actually predicts ahead (see tideToday's docs and the
// get_tides note below) - asking for more would silently return a shorter
// window than requested.
const assistantMaxTideDays = 8

// assistantMaxToolResultChars caps one tool result's JSON encoding. This is
// a chat-context budget, not a correctness limit - a huge tool result eats
// into the model's window and cost for no benefit, since a skipper's
// question never needs more than a handful of days of hourly detail. See
// capToolResultJSON.
const assistantMaxToolResultChars = 12000

// assistantToolDeps are every real-world dependency the four tools need,
// injected so tests supply fakes/stubs instead of a live SignalK, WASM
// plugin registry, Overpass endpoint or InfluxDB connection.
type assistantToolDeps struct {
	now         func() time.Time
	vesselState func() (vesselStateData, error)
	weather     func() (weatherProvider, string, error)
	waves       func() (waveProvider, string, error)
	tides       func() (tideProvider, string, error)
	overpass    overpassFetcher
	routes      func() []routeData
	// influxRange is estimate_passage's only I/O dependency: any SignalK
	// path's history over an explicit range, aggregated to fixed-size
	// buckets. Production wires queryInfluxPathRange (influx.go); tests
	// supply a canned series with no InfluxDB connection at all.
	influxRange func(path string, start, stop time.Time, every string) ([]telemetryPoint, error)
	// fuelRateInstances names the propulsion instances (e.g. "port",
	// "starboard") this vessel's SignalK tree actually publishes a
	// fuel.rate for, so estimate_passage never has to hardcode engine
	// names. nil when the snapshot has none yet (no vessel tree, or a dev
	// backend with no SignalK at all) - executeEstimatePassage falls back
	// to a named pair and says so.
	fuelRateInstances func() []string
	// fuelAboardM3 is the vessel's current fuel volume (helmcentral.fuel.volume,
	// ADR 0084 - the same derived path the Tanks tile reads), for
	// estimate_passage's fuel margin. ok is false whenever the figure is
	// not currently defined (no tank reporting both a level and a
	// capacity, or the input too stale to publish - see
	// freshEnoughToPublish), which estimate_passage reports as "unknown"
	// rather than a fabricated zero.
	fuelAboardM3 func() (float64, bool)
}

// assistantProductionToolDeps wires the real dependencies: the live vessel
// state, the settings-configured weather/wave/tide providers, the shared
// Overpass HTTP client (place_name.go) and a snapshot of the saved routes.
func assistantProductionToolDeps(settingsPath string) assistantToolDeps {
	return assistantToolDeps{
		now:         time.Now,
		vesselState: fetchSignalKVesselState,
		weather: func() (weatherProvider, string, error) {
			return resolveWeatherProvider(settingsPath)
		},
		waves: func() (waveProvider, string, error) {
			return resolveWaveProvider(settingsPath)
		},
		tides: func() (tideProvider, string, error) {
			return resolveTideProvider(settingsPath)
		},
		overpass:          overpassHTTPClient,
		routes:            assistantRouteSnapshot,
		influxRange:       queryInfluxPathRange,
		fuelRateInstances: fuelRateInstancesFromSnapshot,
		fuelAboardM3:      fuelAboardM3FromDerivedPaths,
	}
}

// fuelAboardM3FromDerivedPaths reads helmcentral.fuel.volume (ADR 0084) from
// derivedPathValues, the same map-of-latest-values signalk_paths.go already
// reads from to answer the gauge-values stream. ok is false when the value
// is nil - not currently defined, never a fabricated zero (AGENTS.md's
// fallback policy).
func fuelAboardM3FromDerivedPaths() (float64, bool) {
	value := derivedPathValues()[fuelVolumePath]
	if value == nil {
		return 0, false
	}
	return *value, true
}

// fuelRateInstancesFromSnapshot names every propulsion instance the live
// SignalK tree publishes a fuel.rate for ("port", "starboard", or whatever
// this vessel actually has), the same way fuelRatePaths (derived_paths.go)
// already discovers those paths for the derived fuel-economy figures - no
// configuration, no hardcoded engine count. Returns nil when the snapshot
// has no self tree yet, which executeEstimatePassage treats as "unknown"
// rather than "no engines".
func fuelRateInstancesFromSnapshot() []string {
	tree := globalSignalKSnapshot.selfTree()
	if tree == nil {
		return nil
	}

	var instances []string
	for _, path := range fuelRatePaths(tree) {
		instance := strings.TrimSuffix(strings.TrimPrefix(path, "propulsion."), ".fuel.rate")
		if instance != "" {
			instances = append(instances, instance)
		}
	}
	return instances
}

// assistantRouteSnapshot copies the saved routes under routesMu's read lock
// (routes.go) so find_places never holds that lock while it does Overpass
// I/O or JSON work - the same "copy under the lock, work outside it"
// discipline resolveAndPinAnchorWatchPlaceName (place_name.go) already
// follows for anchorWatchMu.
func assistantRouteSnapshot() []routeData {
	routesMu.RLock()
	defer routesMu.RUnlock()

	out := make([]routeData, 0, len(routesState))
	for _, r := range routesState {
		out = append(out, *r)
	}
	return out
}

// ── tool definitions (OpenRouter/OpenAI function-calling schemas) ──────────

func assistantToolDefinitions() []openRouterTool {
	return []openRouterTool{
		{
			Type: "function",
			Function: openRouterFunctionDef{
				Name: "find_places",
				Description: "Resolve a place name (a bay, anchorage, island, reef, marina, harbour or similar) " +
					"to one or more candidate positions with distance and bearing, searching the vessel's saved " +
					"route waypoints first and then OpenStreetMap. Centres on the vessel's current position unless " +
					"near_lat/near_lon are given. Call this before any forecast or tide tool for a place you only " +
					"know by name. Pass the bare feature name (e.g. \"Bona Bay\", not \"Bona Bay, Gloucester " +
					"Island\"). For a place more than about 20 nm from the vessel, or when a lookup returns " +
					"nothing, pass near_lat/near_lon of a resolved nearby feature and retry.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"query": {
							"type": "string",
							"description": "The place name to search for, e.g. \"Tongue Bay\" or \"Blue Pearl Bay\"."
						},
						"max_results": {
							"type": "integer",
							"description": "Maximum number of candidates to return (default 5, maximum 10)."
						},
						"near_lat": {
							"type": "number",
							"description": "Latitude to search near instead of the vessel's current position. Must be given together with near_lon."
						},
						"near_lon": {
							"type": "number",
							"description": "Longitude to search near instead of the vessel's current position. Must be given together with near_lat."
						}
					},
					"required": ["query"]
				}`),
			},
		},
		{
			Type: "function",
			Function: openRouterFunctionDef{
				Name: "get_wind_forecast",
				Description: "Fetch the wind forecast (and wave forecast, where available) for a position, as a " +
					"per-day summary plus a three-hourly breakdown. Use find_places first to resolve a place name " +
					"to coordinates.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"lat": {"type": "number", "description": "Latitude of the location."},
						"lon": {"type": "number", "description": "Longitude of the location."},
						"days": {
							"type": "integer",
							"description": "Number of days to forecast, 1 to 10 (default 3). Use the fewest days you need."
						},
						"name": {"type": "string", "description": "Optional label for the location, for display only."}
					},
					"required": ["lat", "lon"]
				}`),
			},
		},
		{
			Type: "function",
			Function: openRouterFunctionDef{
				Name:        "get_tides",
				Description: "Fetch tide predictions (high/low times and heights) for the tide station nearest a position. Use find_places first to resolve a place name to coordinates.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"lat": {"type": "number", "description": "Latitude of the location."},
						"lon": {"type": "number", "description": "Longitude of the location."},
						"days": {
							"type": "integer",
							"description": "Number of days of tide predictions, 1 to 8 (default 3). Use the fewest days you need."
						},
						"name": {"type": "string", "description": "Optional label for the location, for display only."}
					},
					"required": ["lat", "lon"]
				}`),
			},
		},
		{
			Type: "function",
			Function: openRouterFunctionDef{
				Name: "estimate_passage",
				Description: "Estimate passage time and fuel from this vessel's own logged performance (speed " +
					"over ground against total fuel rate and rpm, from the last N days of telemetry). Give the " +
					"distance and the planned speed; omit speed to use the most-sampled cruising band. Observed " +
					"data spans whatever conditions occurred; head seas will add time and fuel.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"distance_nm": {"type": "number", "description": "Passage distance in nautical miles."},
						"speed_kts": {
							"type": "number",
							"description": "Planned speed over ground in knots. Omit to use the vessel's most-sampled cruising band."
						},
						"days": {
							"type": "integer",
							"description": "Days of logged telemetry to draw the speed-to-burn table from, 7 to 365 (default 90)."
						}
					},
					"required": ["distance_nm"]
				}`),
			},
		},
	}
}

// ── dispatch ────────────────────────────────────────────────────────────

// execute runs one tool call by name and returns its result as compact
// JSON, capped at assistantMaxToolResultChars (see capToolResultJSON). ctx
// is accepted for symmetry with assistant_run.go's loop (a future tool
// making its own outbound call would need it); none of the three tools
// below do any I/O that takes a context today.
func (d assistantToolDeps) execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	_ = ctx
	switch name {
	case "find_places":
		return d.executeFindPlaces(args)
	case "get_wind_forecast":
		return d.executeGetWindForecast(args)
	case "get_tides":
		return d.executeGetTides(args)
	case "estimate_passage":
		return d.executeEstimatePassage(args)
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

// describeAssistantToolCall renders the one-line status the SSE stream
// shows while a tool call is in flight (assistant_run.go), so the operator
// sees "Looking up Tongue Bay…" rather than a bare tool name.
func describeAssistantToolCall(name string, args json.RawMessage) string {
	switch name {
	case "find_places":
		var a assistantFindPlacesArgs
		query := ""
		if json.Unmarshal(args, &a) == nil {
			query = strings.TrimSpace(a.Query)
		}
		if query == "" {
			query = "a place"
		}
		return fmt.Sprintf("Looking up %s…", query)
	case "get_wind_forecast":
		return fmt.Sprintf("Fetching wind forecast for %s…", assistantLocationLabel(args))
	case "get_tides":
		return fmt.Sprintf("Fetching tides near %s…", assistantLocationLabel(args))
	case "estimate_passage":
		var a assistantEstimatePassageArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return "Estimating the passage from the log…"
		}
		if a.SpeedKts != nil {
			return fmt.Sprintf("Estimating %g nm at %g kts from the log…", a.DistanceNm, *a.SpeedKts)
		}
		return fmt.Sprintf("Estimating %g nm at cruising speed from the log…", a.DistanceNm)
	default:
		return fmt.Sprintf("Running %s…", name)
	}
}

// assistantLocationArgs is the {lat, lon, name} shape shared by
// get_wind_forecast and get_tides' arguments.
type assistantLocationArgs struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Days int     `json:"days"`
	Name string  `json:"name"`
}

// assistantLocationLabel names a location argument set for a status line:
// the model-supplied name when given, otherwise the raw coordinates.
func assistantLocationLabel(args json.RawMessage) string {
	var a assistantLocationArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "the requested position"
	}
	if name := strings.TrimSpace(a.Name); name != "" {
		return name
	}
	return fmt.Sprintf("%.4f,%.4f", a.Lat, a.Lon)
}

// clampAssistantDays applies a tool's days default/ceiling: <=0 becomes
// def, anything past max is capped at max. Never an error - a model that
// asks for 30 days of tides gets 8, not a failed call.
func clampAssistantDays(days, def, max int) int {
	if days <= 0 {
		return def
	}
	if days > max {
		return max
	}
	return days
}

// capToolResultJSON marshals result (a pointer to one of the assistant*
// Result structs below) to compact JSON and, if the encoding exceeds
// assistantMaxToolResultChars, repeatedly calls shrink - which trims one
// item from the least essential list on result (e.g. the oldest hourly row)
// and reports whether it removed anything - re-marshaling after each trim,
// until the result fits or shrink has nothing left to remove.
//
// This is a chat-context budget, not the kind of upstream data problem
// AGENTS.md's fallback policy is about: a tool result truncated with an
// explicit "truncated": true marker is a legible partial answer the model
// can still parse, not a fabricated one. If shrink can't get there (or
// there is nothing to shrink), a minimal, always-valid fallback object is
// returned rather than chopping raw JSON bytes at a byte boundary, which
// could hand the model unparseable text instead of a short one.
func capToolResultJSON(result any, shrink func() bool) (string, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal tool result: %w", err)
	}

	for len(data) > assistantMaxToolResultChars {
		if shrink == nil || !shrink() {
			return `{"truncated":true,"note":"result too large to return in full even after trimming"}`, nil
		}
		data, err = json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("marshal tool result: %w", err)
		}
	}

	return string(data), nil
}

// ── find_places ─────────────────────────────────────────────────────────

type assistantFindPlacesArgs struct {
	Query      string   `json:"query"`
	MaxResults int      `json:"max_results"`
	NearLat    *float64 `json:"near_lat"`
	NearLon    *float64 `json:"near_lon"`
}

// assistantLatLon is a bare {lat, lon} pair, used for find_places' centre.
type assistantLatLon struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// assistantPlaceCandidate is one find_places result: a named feature with
// host-computed distance and bearing from the search centre (ADR 0091's
// rule that a provider returns raw features and the host alone computes
// distance/bearing, applied here to Overpass and to route waypoints alike).
type assistantPlaceCandidate struct {
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	DistanceNm float64 `json:"distance_nm"`
	BearingDeg int     `json:"bearing_deg"`
	Source     string  `json:"source"`
}

type assistantFindPlacesResult struct {
	Centre *assistantLatLon `json:"centre,omitempty"`
	// CentredOn names the qualifier feature (e.g. "Gloucester Island") rung
	// 2's bbox was centred on, when the query carried a comma-qualifier that
	// resolved and rung 1 found nothing at the vessel/near_* centre - see
	// executeFindPlaces. Empty whenever rung 2 centred on the ordinary
	// vessel/near_* position, including when a qualifier was present but did
	// not resolve.
	CentredOn string                    `json:"centred_on,omitempty"`
	RadiusNm  float64                   `json:"radius_nm,omitempty"`
	Search    string                    `json:"search,omitempty"`
	Results   []assistantPlaceCandidate `json:"results"`
	Note      string                    `json:"note,omitempty"`
	Truncated bool                      `json:"truncated,omitempty"`
}

// assistantBoundingBox returns a bounding box radiusNm around (lat, lon), in
// the (south, west, north, east) order Overpass's bbox filter expects.
// dLat=radiusNm/60 because a nautical mile is defined as one minute of
// latitude; dLon widens by 1/cos(lat) so that the box holds radiusNm of
// longitude constant at this latitude (a degree of longitude shrinks toward
// the poles). lat is clamped to [-90,90] since nothing north of the pole or
// south of it makes sense; lon is deliberately left unwrapped across ±180 -
// this project's home waters (the Whitsundays) are nowhere near the
// antimeridian, so date-line wraparound is simply not a case worth handling.
func assistantBoundingBox(lat, lon, radiusNm float64) (south, west, north, east float64) {
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

// overpassBoundingBoxClause formats a (south, west, north, east) box as the
// Overpass QL bbox filter argument, e.g. "(south,west,north,east)".
func overpassBoundingBoxClause(south, west, north, east float64) string {
	return fmt.Sprintf("%.4f,%.4f,%.4f,%.4f", south, west, north, east)
}

// assistantTitleCase title-cases every space-separated word: its first
// letter upper-cased, the rest lower-cased. Deliberately not modelled on the
// deprecated strings.Title, which title-cases after every non-letter
// character rather than just at whitespace, so an apostrophe or hyphen
// mid-word wrongly starts a new "word" (e.g. turning "bell's beach" into
// "Bell'S Beach", or "port-side" into "Port-Side"); this only ever
// upper-cases the very first rune of each Fields-delimited word, so a
// name's internal punctuation is left exactly as typed.
func assistantTitleCase(s string) string {
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

// assistantFirstLetterUpperCase upper-cases only s's first rune, leaving
// everything else exactly as typed - a query typed as "bona bay" becomes
// "Bona bay", covering an OSM name tag capitalised on just its first word.
func assistantFirstLetterUpperCase(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// assistantExactNameVariants returns the de-duplicated set of capitalisation
// variants rung 1 searches for: the query as typed (trimmed, internal
// whitespace collapsed), title case of every word, and the query with only
// its first letter upper-cased. This covers the common capitalisation
// conventions a real OSM name tag actually uses without paying an
// unindexed regex's server cost for it - see ADR 0093 section 8.
//
// A query that carries a qualifier after a comma ("Bona Bay, Gloucester
// Island") also gets the same three variants of just the head before the
// comma ("Bona Bay") - an operator or a model asking about a named feature
// tends to keep the qualifier for clarity, but OSM's own name tag almost
// never does, so searching only the qualified string would miss the exact
// tag that is actually there.
func assistantExactNameVariants(query string) []string {
	normalized := strings.Join(strings.Fields(query), " ")
	if normalized == "" {
		return nil
	}

	candidates := []string{
		normalized,
		assistantTitleCase(normalized),
		assistantFirstLetterUpperCase(normalized),
	}

	if head, _, ok := strings.Cut(normalized, ","); ok {
		if headNormalized := strings.Join(strings.Fields(head), " "); headNormalized != "" {
			candidates = append(candidates,
				headNormalized,
				assistantTitleCase(headNormalized),
				assistantFirstLetterUpperCase(headNormalized),
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

// assistantFindPlacesQualifier returns the trimmed text after a query's
// first comma ("Bona Bay, Gloucester Island" -> "Gloucester Island"), or ""
// when the query carries no comma or the qualifier is blank. This is the
// feature find_places tries to resolve on its own, centring rung 2 on it,
// when the bare query (assistantExactNameVariants' head variants) does not
// resolve within rung 1's full radius - see executeFindPlaces.
func assistantFindPlacesQualifier(query string) string {
	_, tail, ok := strings.Cut(query, ",")
	if !ok {
		return ""
	}
	return strings.TrimSpace(tail)
}

// nearestAssistantPlaceCandidate returns the candidate with the smallest
// DistanceNm, or false when candidates is empty. Used to pick which
// resolved qualifier hit find_places should centre rung 2's search on, when
// the qualifier query returns more than one candidate.
func nearestAssistantPlaceCandidate(candidates []assistantPlaceCandidate) (assistantPlaceCandidate, bool) {
	if len(candidates) == 0 {
		return assistantPlaceCandidate{}, false
	}
	nearest := candidates[0]
	for _, c := range candidates[1:] {
		if c.DistanceNm < nearest.DistanceNm {
			nearest = c
		}
	}
	return nearest, true
}

// buildOverpassExactNameQuery builds rung 1's Overpass QL: an untagged,
// exact nwr["name"="<variant>"] clause per assistantExactNameVariants
// result, unioned together and evaluated over a bbox rather than around: -
// an exact match lets Overpass use its name index directly, which is what
// makes this rung answer in about 1s even at find_places' full 100nm radius
// (ADR 0093 section 8). There is deliberately no tag filter here: adding one
// to an exact-match query turned the same live 1s answer into 20s (measured
// 2026-09-11 against overpass.openstreetmap.fr), so kind is instead derived
// host-side from whatever tag happens to be present (see
// overpassFindPlacesKind) and nothing named is discarded for want of a
// recognised kind. Each variant is escaped for the Overpass QL string
// literal syntax (escapeOverpassStringLiteral) so a query containing a
// literal quote or backslash can never break out of the generated query.
func buildOverpassExactNameQuery(query string, south, west, north, east float64) string {
	bbox := overpassBoundingBoxClause(south, west, north, east)

	var b strings.Builder
	b.WriteString("[out:json][timeout:25];(")
	for _, variant := range assistantExactNameVariants(query) {
		fmt.Fprintf(&b, `nwr["name"="%s"](%s);`, escapeOverpassStringLiteral(variant), bbox)
	}
	b.WriteString(");out tags center 30;")
	return b.String()
}

// buildOverpassNameSearchQuery builds rung 2's Overpass QL: a partial,
// case-insensitive name search over a bbox, matching any of the four tag
// clauses find_places understands: natural coastal features, place types,
// seamark facilities, and OSM's separate leisure=marina tagging (which is
// not part of the seamark vocabulary but is how most real-world marinas are
// tagged). query is regexp.QuoteMeta-escaped and then escaped again for the
// Overpass QL string literal syntax (backslash and double quote) - see
// escapeOverpassStringLiteral - so a query containing e.g. parentheses or a
// literal quote can never break out of the generated query. This rung only
// ever runs at assistantFindPlacesRegexRadiusNm (20nm): a name regex is not
// indexable, so Overpass must scan every element in the box against it, and
// that scan times out server-side well before 100nm - see ADR 0093 section
// 8 for the live measurement.
func buildOverpassNameSearchQuery(query string, south, west, north, east float64) string {
	q := escapeOverpassStringLiteral(regexp.QuoteMeta(query))
	bbox := overpassBoundingBoxClause(south, west, north, east)
	return fmt.Sprintf(
		`[out:json][timeout:25];(nwr["name"~"%s",i]["natural"~"^(bay|reef|beach|cape|strait|peninsula|shoal|inlet)$"](%s);nwr["name"~"%s",i]["place"~"^(island|islet|locality|hamlet|village|town|archipelago)$"](%s);nwr["name"~"%s",i]["seamark:type"~"^(anchorage|harbour|mooring|marina|small_craft_facility)$"](%s);nwr["name"~"%s",i]["leisure"="marina"](%s););out tags center 30;`,
		q, bbox, q, bbox, q, bbox, q, bbox,
	)
}

// escapeOverpassStringLiteral escapes a string for embedding inside an
// Overpass QL double-quoted string literal (e.g. a ["name"~"<value>",i]
// regex filter): backslash and double quote are the two characters QL's
// own string syntax reserves, and regexp.QuoteMeta's escaping (which uses
// backslashes) would otherwise land unescaped in the generated query.
func escapeOverpassStringLiteral(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// overpassFindPlacesKind classifies a find_places Overpass element by tag
// priority: seamark:type (a human already decided this is a marine
// facility), then natural, then place, then leisure. Unlike
// place_name.go's featureKind (a fixed rank for a different, narrower tag
// set), any one of the four clauses' tags is accepted verbatim as the kind
// label - find_places is a broader search, not a winner-take-all ranking.
// Rung 1's exact-name query carries no tag filter (buildOverpassExactNameQuery),
// so an element can genuinely have none of these four tags; that falls back
// to "feature" rather than being discarded; find_places' whole point is
// surfacing every named match and letting the model judge relevance, not
// silently dropping anything whose kind it doesn't recognise.
func overpassFindPlacesKind(tags map[string]string) string {
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

// dedupeAssistantPlaceCandidates drops a later candidate that shares a
// case-insensitive name with, and sits within 500m of, an earlier one
// already kept - the same real-world feature returned twice (a saved
// waypoint and Overpass's own copy of it, most often) rather than two
// distinct features that happen to share a name. Earlier candidates win,
// which in practice means a route waypoint (added before Overpass results)
// is preferred over Overpass's guess at the same place.
func dedupeAssistantPlaceCandidates(candidates []assistantPlaceCandidate) []assistantPlaceCandidate {
	const dedupeDistanceM = 500.0

	out := make([]assistantPlaceCandidate, 0, len(candidates))
	for _, c := range candidates {
		lname := strings.ToLower(c.Name)
		duplicate := false
		for _, existing := range out {
			if strings.ToLower(existing.Name) != lname {
				continue
			}
			if haversineMeters(existing.Lat, existing.Lon, c.Lat, c.Lon) <= dedupeDistanceM {
				duplicate = true
				break
			}
		}
		if !duplicate {
			out = append(out, c)
		}
	}
	return out
}

// assistantOverpassElementsToCandidates converts a rung's raw Overpass
// elements into find_places candidates, discarding any element with no
// resolvable point or no name tag. Shared by both rungs (buildOverpassExactNameQuery's
// and buildOverpassNameSearchQuery's results alike) since the conversion -
// distance, bearing, kind, source label - is identical either way.
func assistantOverpassElementsToCandidates(elements []overpassElement, lat, lon float64) []assistantPlaceCandidate {
	var out []assistantPlaceCandidate
	for _, el := range elements {
		elLat, elLon, ok := elementLatLon(el)
		if !ok {
			continue
		}
		name := strings.TrimSpace(el.Tags["name"])
		if name == "" {
			continue
		}
		out = append(out, assistantPlaceCandidate{
			Name:       name,
			Kind:       overpassFindPlacesKind(el.Tags),
			Lat:        elLat,
			Lon:        elLon,
			DistanceNm: roundTo1(haversineMeters(lat, lon, elLat, elLon) / metersPerNauticalMile),
			BearingDeg: int(math.Round(bearingDeg(lat, lon, elLat, elLon))),
			Source:     "osm",
		})
	}
	return out
}

func (d assistantToolDeps) executeFindPlaces(raw json.RawMessage) (string, error) {
	var args assistantFindPlacesArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse find_places arguments: %w", err)
	}

	query := strings.TrimSpace(args.Query)
	if query == "" {
		return "", fmt.Errorf("find_places: query is required")
	}

	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = 5
	}
	if maxResults > 10 {
		maxResults = 10
	}

	var lat, lon float64
	haveCentre := false
	if args.NearLat != nil && args.NearLon != nil {
		lat, lon = *args.NearLat, *args.NearLon
		haveCentre = true
	} else if state, err := d.vesselState(); err == nil && hasUsableVesselPosition(state.Latitude, state.Longitude) {
		lat, lon = state.Latitude, state.Longitude
		haveCentre = true
	}

	if !haveCentre {
		result := assistantFindPlacesResult{
			Results: []assistantPlaceCandidate{},
			Note:    "vessel position unknown; ask the operator for a region or coordinates",
		}
		return capToolResultJSON(&result, nil)
	}

	var candidates []assistantPlaceCandidate
	lowerQuery := strings.ToLower(query)
	waypointHit := false
	for _, route := range d.routes() {
		for _, wp := range route.Waypoints {
			name := strings.TrimSpace(wp.Name)
			if name == "" || !strings.Contains(strings.ToLower(name), lowerQuery) {
				continue
			}
			waypointHit = true
			candidates = append(candidates, assistantPlaceCandidate{
				Name:       name,
				Kind:       "waypoint",
				Lat:        wp.Lat,
				Lon:        wp.Lon,
				DistanceNm: roundTo1(haversineMeters(lat, lon, wp.Lat, wp.Lon) / metersPerNauticalMile),
				BearingDeg: int(math.Round(bearingDeg(lat, lon, wp.Lat, wp.Lon))),
				Source:     "route:" + route.Name,
			})
		}
	}

	// Rung 1: an exact-name match, full radius. Always run, even when a
	// waypoint already matched - it's cheap (ADR 0093 section 8 measured
	// ~1s), and it's the only way OSM's own position for the same place
	// ever reaches the model.
	exactSouth, exactWest, exactNorth, exactEast := assistantBoundingBox(lat, lon, assistantFindPlacesRadiusNm)
	exactElements, err := postOverpassQuery(d.overpass, buildOverpassExactNameQuery(query, exactSouth, exactWest, exactNorth, exactEast))
	if err != nil {
		return "", fmt.Errorf("find_places: %w", err)
	}
	exactCandidates := assistantOverpassElementsToCandidates(exactElements, lat, lon)

	// Rung 2: a partial-name regex, tight radius, only when rung 1 came back
	// empty and no waypoint already matched - a waypoint hit means the
	// operator's own answer is already in hand, and rung 2's regex union is
	// the expensive query (ADR 0093 section 8: 2 to 5s at 20nm, and it times
	// out server-side well before 100nm).
	//
	// A query carrying a comma-qualifier ("Bona Bay, Gloucester Island")
	// gets one extra exact-name lookup for the qualifier alone, at rung 1's
	// full radius, before rung 2 runs: when it resolves, rung 2's tight box
	// is centred on the nearest such hit rather than the vessel/near_*
	// centre, since the named feature can sit well outside rung 2's 20nm
	// radius from the vessel (a bay named for the island it is on, tens of
	// miles away). When the qualifier does not resolve, rung 2 falls back to
	// centring on the vessel/near_* position exactly as before. This is at
	// most one extra Overpass call, and only on this path.
	var regexCandidates []assistantPlaceCandidate
	var centredOn string
	regexCentreLat, regexCentreLon := lat, lon
	ranRung2 := len(exactCandidates) == 0 && !waypointHit
	if ranRung2 {
		if qualifier := assistantFindPlacesQualifier(query); qualifier != "" {
			qSouth, qWest, qNorth, qEast := assistantBoundingBox(lat, lon, assistantFindPlacesRadiusNm)
			qElements, qerr := postOverpassQuery(d.overpass, buildOverpassExactNameQuery(qualifier, qSouth, qWest, qNorth, qEast))
			if qerr != nil {
				return "", fmt.Errorf("find_places: %w", qerr)
			}
			if nearest, ok := nearestAssistantPlaceCandidate(assistantOverpassElementsToCandidates(qElements, lat, lon)); ok {
				regexCentreLat, regexCentreLon = nearest.Lat, nearest.Lon
				centredOn = nearest.Name
			}
		}

		regexSouth, regexWest, regexNorth, regexEast := assistantBoundingBox(regexCentreLat, regexCentreLon, assistantFindPlacesRegexRadiusNm)
		regexElements, err := postOverpassQuery(d.overpass, buildOverpassNameSearchQuery(query, regexSouth, regexWest, regexNorth, regexEast))
		if err != nil {
			return "", fmt.Errorf("find_places: %w", err)
		}
		regexCandidates = assistantOverpassElementsToCandidates(regexElements, lat, lon)
	}

	osmSearch := "none"
	radiusUsed := assistantFindPlacesRadiusNm
	switch {
	case len(exactCandidates) > 0:
		osmSearch = "exact"
	case len(regexCandidates) > 0:
		osmSearch = "regex"
		radiusUsed = assistantFindPlacesRegexRadiusNm
	case ranRung2:
		radiusUsed = assistantFindPlacesRegexRadiusNm
	}
	search := osmSearch
	if waypointHit {
		search = "waypoints"
	}

	candidates = append(candidates, exactCandidates...)
	candidates = append(candidates, regexCandidates...)

	candidates = dedupeAssistantPlaceCandidates(candidates)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].DistanceNm < candidates[j].DistanceNm })
	if len(candidates) > maxResults {
		candidates = candidates[:maxResults]
	}

	centre := assistantLatLon{Lat: lat, Lon: lon}
	if centredOn != "" {
		centre = assistantLatLon{Lat: regexCentreLat, Lon: regexCentreLon}
	}
	result := assistantFindPlacesResult{
		Centre:    &centre,
		CentredOn: centredOn,
		RadiusNm:  radiusUsed,
		Search:    search,
		Results:   candidates,
	}
	if len(candidates) == 0 {
		result.Note = fmt.Sprintf(
			"no named feature matching %q within %g nm by exact name or within %g nm by partial name",
			query, assistantFindPlacesRadiusNm, assistantFindPlacesRegexRadiusNm,
		)
	}

	shrink := func() bool {
		if len(result.Results) == 0 {
			return false
		}
		result.Results = result.Results[:len(result.Results)-1]
		result.Truncated = true
		return true
	}
	return capToolResultJSON(&result, shrink)
}

// ── get_wind_forecast ───────────────────────────────────────────────────

type assistantWindHour struct {
	T       string   `json:"t"`
	WindKts *int     `json:"wind_kts"`
	GustKts *int     `json:"gust_kts"`
	Dir     *string  `json:"dir"`
	DirDeg  *int     `json:"dir_deg"`
	WaveM   *float64 `json:"wave_m"`
	WaveS   *float64 `json:"wave_s"`
	WaveDir *string  `json:"wave_dir"`
}

type assistantDaySummary struct {
	Date          string  `json:"date"`
	WindKtsMin    *int    `json:"wind_kts_min"`
	WindKtsMax    *int    `json:"wind_kts_max"`
	GustKtsMax    *int    `json:"gust_kts_max"`
	DirPrevailing *string `json:"dir_prevailing"`
}

type assistantWindForecastResult struct {
	Name            string                `json:"name"`
	Lat             float64               `json:"lat"`
	Lon             float64               `json:"lon"`
	Timezone        string                `json:"timezone"`
	WeatherProvider string                `json:"weather_provider"`
	WaveProvider    string                `json:"wave_provider"`
	WavesError      string                `json:"waves_error,omitempty"`
	Cached          bool                  `json:"cached"`
	CachedAt        *string               `json:"cached_at"`
	Days            []assistantDaySummary `json:"days"`
	Hourly          []assistantWindHour   `json:"hourly"`
	Truncated       bool                  `json:"truncated,omitempty"`
}

// assistantDayAggregate accumulates one local day's min/max wind, max gust
// and a vote count per compass direction, from which the day's prevailing
// direction (the mode) is read off once every kept hour has been seen.
type assistantDayAggregate struct {
	windMin  *int
	windMax  *int
	gustMax  *int
	dirVotes map[string]int
	dirOrder []string
}

func (a *assistantDayAggregate) addWind(v int) {
	if a.windMin == nil || v < *a.windMin {
		vv := v
		a.windMin = &vv
	}
	if a.windMax == nil || v > *a.windMax {
		vv := v
		a.windMax = &vv
	}
}

func (a *assistantDayAggregate) addGust(v int) {
	if a.gustMax == nil || v > *a.gustMax {
		vv := v
		a.gustMax = &vv
	}
}

func (a *assistantDayAggregate) addDir(dir string) {
	if a.dirVotes == nil {
		a.dirVotes = map[string]int{}
	}
	if _, seen := a.dirVotes[dir]; !seen {
		a.dirOrder = append(a.dirOrder, dir)
	}
	a.dirVotes[dir]++
}

// prevailing returns the mode of the recorded direction votes, ties broken
// by first-seen order so the result is deterministic.
func (a *assistantDayAggregate) prevailing() *string {
	best := ""
	bestCount := 0
	for _, dir := range a.dirOrder {
		if a.dirVotes[dir] > bestCount {
			best = dir
			bestCount = a.dirVotes[dir]
		}
	}
	if best == "" {
		return nil
	}
	return &best
}

func (d assistantToolDeps) executeGetWindForecast(raw json.RawMessage) (string, error) {
	var args assistantLocationArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse get_wind_forecast arguments: %w", err)
	}
	days := clampAssistantDays(args.Days, 3, assistantMaxForecastDays)

	provider, providerID, err := d.weather()
	if err != nil {
		return "", fmt.Errorf("get_wind_forecast: %w", err)
	}

	tz := vesselLocalTimezoneName(args.Lon)
	bundle, err := provider.FetchForecast(args.Lat, args.Lon, days, tz)
	if err != nil {
		return "", fmt.Errorf("get_wind_forecast: weather provider %q: %w", providerID, err)
	}

	var waveProviderID, wavesError string
	waveByUnix := map[int64]waveHourPoint{}
	if waveProvider, wpID, werr := d.waves(); werr != nil {
		waveProviderID = wpID
		wavesError = firstErrorLine(werr)
	} else {
		waveProviderID = wpID
		waveBundle, ferr := waveProvider.FetchWaves(args.Lat, args.Lon, days)
		if ferr != nil {
			wavesError = firstErrorLine(ferr)
		} else {
			for _, whp := range waveBundle.Hourly {
				waveByUnix[whp.Time.Unix()] = whp
			}
		}
	}

	loc := vesselLocalLocation(args.Lon)
	nowHour := d.now().Truncate(time.Hour)

	dayOrder := make([]string, 0)
	dayAggs := make(map[string]*assistantDayAggregate)
	hourly := make([]assistantWindHour, 0)

	for _, hp := range bundle.Hourly {
		if hp.Time.IsZero() || hp.Time.Before(nowHour) {
			continue
		}
		localTime := hp.Time.In(loc)
		if localTime.Hour()%3 != 0 {
			continue
		}

		row := assistantWindHour{T: localTime.Format("Mon 2 15:04")}

		var windKts *int
		if hp.WindSpeedMS != 0 {
			v := int(math.Round(hp.WindSpeedMS * metersPerSecondToKnots))
			windKts = &v
		}
		var gustKts *int
		if hp.WindGustMS != 0 {
			v := int(math.Round(hp.WindGustMS * metersPerSecondToKnots))
			gustKts = &v
		}
		var dir *string
		var dirDeg *int
		if hp.WindDirectionDeg != 0 {
			dv := degreesToDirection(hp.WindDirectionDeg)
			dgv := int(math.Round(hp.WindDirectionDeg))
			dir, dirDeg = &dv, &dgv
		}
		row.WindKts, row.GustKts, row.Dir, row.DirDeg = windKts, gustKts, dir, dirDeg

		if whp, ok := waveByUnix[hp.Time.Unix()]; ok && whp.WavePeriodS != 0 {
			hm := roundTo1(whp.WaveHeightM)
			ps := roundTo1(whp.WavePeriodS)
			wd := degreesToDirection(whp.WaveDirectionDeg)
			row.WaveM, row.WaveS, row.WaveDir = &hm, &ps, &wd
		}

		hourly = append(hourly, row)

		dayKey := localTime.Format("2006-01-02")
		agg, ok := dayAggs[dayKey]
		if !ok {
			agg = &assistantDayAggregate{}
			dayAggs[dayKey] = agg
			dayOrder = append(dayOrder, dayKey)
		}
		if windKts != nil {
			agg.addWind(*windKts)
		}
		if gustKts != nil {
			agg.addGust(*gustKts)
		}
		if dir != nil {
			agg.addDir(*dir)
		}
	}

	days2 := make([]assistantDaySummary, 0, len(dayOrder))
	for _, dayKey := range dayOrder {
		agg := dayAggs[dayKey]
		days2 = append(days2, assistantDaySummary{
			Date:          dayKey,
			WindKtsMin:    agg.windMin,
			WindKtsMax:    agg.windMax,
			GustKtsMax:    agg.gustMax,
			DirPrevailing: agg.prevailing(),
		})
	}

	var cachedAt *string
	if !bundle.CachedAt.IsZero() {
		s := bundle.CachedAt.UTC().Format(time.RFC3339)
		cachedAt = &s
	}

	result := assistantWindForecastResult{
		Name:            strings.TrimSpace(args.Name),
		Lat:             args.Lat,
		Lon:             args.Lon,
		Timezone:        tz,
		WeatherProvider: providerID,
		WaveProvider:    waveProviderID,
		WavesError:      wavesError,
		Cached:          bundle.Cached,
		CachedAt:        cachedAt,
		Days:            days2,
		Hourly:          hourly,
	}

	shrink := func() bool {
		if len(result.Hourly) == 0 {
			return false
		}
		result.Hourly = result.Hourly[:len(result.Hourly)-1]
		result.Truncated = true
		return true
	}
	return capToolResultJSON(&result, shrink)
}

// ── get_tides ───────────────────────────────────────────────────────────

type assistantStationInfo struct {
	Name       string  `json:"name"`
	ID         string  `json:"id"`
	State      string  `json:"state,omitempty"`
	Timezone   string  `json:"timezone,omitempty"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	DistanceNm float64 `json:"distance_nm"`
	BearingDeg int     `json:"bearing_deg"`
}

type assistantTideNow struct {
	Time      string  `json:"time"`
	HeightM   float64 `json:"height_m"`
	Direction string  `json:"direction"`
}

type assistantTideExtreme struct {
	Time    string  `json:"time"`
	ISO     string  `json:"iso"`
	Type    string  `json:"type"`
	HeightM float64 `json:"height_m"`
}

type assistantGetTidesResult struct {
	Station   assistantStationInfo   `json:"station"`
	Provider  string                 `json:"provider"`
	Now       assistantTideNow       `json:"now"`
	TimeBasis string                 `json:"time_basis"`
	Extremes  []assistantTideExtreme `json:"extremes"`
	Cached    bool                   `json:"cached"`
	CachedAt  *string                `json:"cached_at"`
	Note      string                 `json:"note,omitempty"`
	Truncated bool                   `json:"truncated,omitempty"`
}

func roundTo2(value float64) float64 { return math.Round(value*100) / 100 }

func (d assistantToolDeps) executeGetTides(raw json.RawMessage) (string, error) {
	var args assistantLocationArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse get_tides arguments: %w", err)
	}
	days := clampAssistantDays(args.Days, 3, assistantMaxTideDays)

	provider, providerID, err := d.tides()
	if err != nil {
		return "", fmt.Errorf("get_tides: %w", err)
	}

	station, ok := nearestStation(provider, args.Lat, args.Lon)
	if !ok {
		return "", fmt.Errorf("get_tides: tide provider %q has no stations", providerID)
	}

	res, err := provider.FetchTideChart(station.StationID)
	if err != nil {
		return "", fmt.Errorf("get_tides: tide provider %q: %w", providerID, err)
	}

	// The station's own timezone is preferred (it's the zone tide times are
	// actually meaningful in); vesselLocalLocation's longitude-derived fixed
	// offset is the fallback for a station whose catalog entry carries no
	// timezone or names one this binary's embedded zoneinfo (backend/tzdata.go)
	// doesn't recognise.
	var loc *time.Location
	var timeBasis string
	if tz := strings.TrimSpace(station.Timezone); tz != "" {
		if l, lerr := time.LoadLocation(tz); lerr == nil {
			loc, timeBasis = l, tz
		}
	}
	if loc == nil {
		loc = vesselLocalLocation(station.Lon)
		timeBasis = loc.String()
	}

	now := d.now()
	localNow := now.In(loc)
	windowStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
	windowEnd := windowStart.AddDate(0, 0, days)

	extremes := make([]assistantTideExtreme, 0)
	for _, e := range res.Extremes {
		if e.Time.Before(windowStart) || !e.Time.Before(windowEnd) {
			continue
		}
		extremeType := "low"
		if e.High {
			extremeType = "high"
		}
		extremes = append(extremes, assistantTideExtreme{
			Time:    e.Time.In(loc).Format("Mon 2 Jan 15:04"),
			ISO:     e.Time.UTC().Format(time.RFC3339),
			Type:    extremeType,
			HeightM: roundTo2(e.HeightM),
		})
	}

	var cachedAt *string
	if !res.CachedAt.IsZero() {
		s := res.CachedAt.UTC().Format(time.RFC3339)
		cachedAt = &s
	}

	result := assistantGetTidesResult{
		Station: assistantStationInfo{
			Name:       station.Name,
			ID:         station.StationID,
			State:      station.State,
			Timezone:   station.Timezone,
			Lat:        station.Lat,
			Lon:        station.Lon,
			DistanceNm: roundTo1(haversineMeters(args.Lat, args.Lon, station.Lat, station.Lon) / metersPerNauticalMile),
			BearingDeg: int(math.Round(bearingDeg(args.Lat, args.Lon, station.Lat, station.Lon))),
		},
		Provider: providerID,
		Now: assistantTideNow{
			Time:      localNow.Format("Mon 2 Jan 15:04"),
			HeightM:   res.CurrentHeightM,
			Direction: res.Direction,
		},
		TimeBasis: timeBasis,
		Extremes:  extremes,
		Cached:    res.Cached,
		CachedAt:  cachedAt,
	}
	if len(extremes) == 0 {
		result.Note = "provider returned no extremes in this window (BOM gives about 8 days)"
	}

	shrink := func() bool {
		if len(result.Extremes) == 0 {
			return false
		}
		result.Extremes = result.Extremes[:len(result.Extremes)-1]
		result.Truncated = true
		return true
	}
	return capToolResultJSON(&result, shrink)
}

// ── estimate_passage ────────────────────────────────────────────────────

// assistantEstimatePassageDefaultHistoryDays, ...MinHistoryDays and
// ...MaxHistoryDays bound estimate_passage's days argument: 90 by default is
// long enough to average out any one trip's conditions; below
// MinHistoryDays a single passage would masquerade as the boat's whole
// performance envelope; past MaxHistoryDays a re-engined or re-propped
// boat's stale numbers would still count.
const (
	assistantEstimatePassageDefaultHistoryDays = 90
	assistantEstimatePassageMinHistoryDays     = 7
	assistantEstimatePassageMaxHistoryDays     = 365
)

// assistantEstimatePassageFallbackInstances is what estimate_passage assumes
// when the SignalK snapshot has no propulsion tree to discover instance
// names from at all (a dev backend with no SignalK connected) - this
// vessel's two actual engines, named directly rather than guessed at any
// wider scope. A result built on this fallback carries instances_assumed:
// true so the model and operator both know it is an assumption, not a
// discovery.
var assistantEstimatePassageFallbackInstances = []string{"port", "starboard"}

// assistantEstimatePassageArgs is estimate_passage's argument shape.
// SpeedKts is a pointer so "the model left it out" (nil, use the
// most-sampled cruising band) is distinguishable from a genuine zero, which
// is never a valid planned speed anyway.
type assistantEstimatePassageArgs struct {
	DistanceNm float64  `json:"distance_nm"`
	SpeedKts   *float64 `json:"speed_kts"`
	Days       int      `json:"days"`
}

type assistantEstimatePassageResult struct {
	DistanceNm float64 `json:"distance_nm"`
	SpeedKts   float64 `json:"speed_kts"`
	// SpeedSource is "requested" when speed_kts came from the model, or
	// "most_sampled_band" when it was omitted and estimate_passage picked
	// the vessel's most-sampled cruising band instead.
	SpeedSource      string            `json:"speed_source"`
	Hours            float64           `json:"hours"`
	Litres           float64           `json:"litres"`
	LPerH            float64           `json:"l_per_h"`
	RPM              float64           `json:"rpm"`
	HistoryDays      int               `json:"history_days"`
	Samples          int               `json:"samples"`
	Instances        []string          `json:"instances"`
	InstancesAssumed bool              `json:"instances_assumed"`
	Table            []performanceBand `json:"table"`
	// FuelAboardL and FuelAfterL are omitted entirely (nil pointers,
	// omitempty) when fuelAboardM3 reports the fuel volume as not
	// currently defined - the note says so instead, rather than the model
	// seeing a 0 that reads as an empty tank.
	FuelAboardL *float64 `json:"fuel_aboard_l,omitempty"`
	FuelAfterL  *float64 `json:"fuel_after_l,omitempty"`
	Note        string   `json:"note"`
}

// executeEstimatePassage joins this vessel's own logged speed over ground,
// total fuel rate and rpm (buildPerformanceTable, assistant_performance.go)
// over the requested history window, then reads a burn rate and rpm off
// that table at the requested speed (or the most-sampled band, when the
// model left speed out) to turn a bare distance into an hours-and-litres
// estimate. Every number here is this vessel's own history, not a polar or
// a fuel curve looked up from a manufacturer spec sheet - see ADR 0093
// section 12.
func (d assistantToolDeps) executeEstimatePassage(raw json.RawMessage) (string, error) {
	var args assistantEstimatePassageArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse estimate_passage arguments: %w", err)
	}
	if args.DistanceNm <= 0 {
		return "", fmt.Errorf("estimate_passage: distance_nm is required and must be greater than zero")
	}
	if args.SpeedKts != nil && *args.SpeedKts <= 0 {
		return "", fmt.Errorf("estimate_passage: speed_kts must be greater than zero")
	}

	days := clampAssistantDays(args.Days, assistantEstimatePassageDefaultHistoryDays, assistantEstimatePassageMaxHistoryDays)
	if days < assistantEstimatePassageMinHistoryDays {
		days = assistantEstimatePassageMinHistoryDays
	}

	stop := d.now()
	start := stop.AddDate(0, 0, -days)

	instances := d.fuelRateInstances()
	instancesAssumed := false
	if len(instances) == 0 {
		instances = assistantEstimatePassageFallbackInstances
		instancesAssumed = true
	}

	sog, err := d.influxRange("navigation.speedOverGround", start, stop, assistantPerformanceQueryEvery)
	if err != nil {
		return "", fmt.Errorf("estimate_passage: %w", err)
	}

	fuelRates := make([][]telemetryPoint, 0, len(instances))
	for _, instance := range instances {
		series, ferr := d.influxRange(fmt.Sprintf("propulsion.%s.fuel.rate", instance), start, stop, assistantPerformanceQueryEvery)
		if ferr != nil {
			return "", fmt.Errorf("estimate_passage: %w", ferr)
		}
		fuelRates = append(fuelRates, series)
	}

	// rpm comes from one representative instance rather than an average
	// across every engine: this boat's twin engines run matched revs under
	// way, and averaging would need its own timestamp join - with its own
	// "one instance missing" question - for a figure that is illustrative
	// context, not the thing estimate_passage is actually estimating.
	rpm, err := d.influxRange(fmt.Sprintf("propulsion.%s.revolutions", instances[0]), start, stop, assistantPerformanceQueryEvery)
	if err != nil {
		return "", fmt.Errorf("estimate_passage: %w", err)
	}

	table := buildPerformanceTable(sog, fuelRates, rpm, assistantPerformanceMinSOGKts)
	if len(table) == 0 {
		return "", fmt.Errorf("estimate_passage: no underway history in the last %d days", days)
	}

	speedKts := 0.0
	speedSource := "requested"
	if args.SpeedKts != nil {
		speedKts = *args.SpeedKts
	} else {
		band, _ := mostSampledBand(table)
		speedKts = band.SOGKtsMean
		speedSource = "most_sampled_band"
	}

	lPerH, rpmAtSpeed, ok := estimateAtSpeed(table, speedKts)
	if !ok {
		return "", fmt.Errorf("estimate_passage: no underway history in the last %d days", days)
	}

	hours := args.DistanceNm / speedKts
	litres := hours * lPerH
	litresRounded := math.Round(litres)

	totalSamples := 0
	for _, band := range table {
		totalSamples += band.Samples
	}

	// Fuel aboard (helmcentral.fuel.volume, ADR 0084 - the same figure the
	// Tanks tile shows) turns the burn estimate into a margin. Absent
	// whenever fuelAboardM3 is unset (a test double that does not care
	// about it) or reports the figure as not currently defined - never a
	// fabricated zero that would read as an empty tank.
	var fuelAboardL, fuelAfterL *float64
	aboardM3, aboardOK := 0.0, false
	if d.fuelAboardM3 != nil {
		aboardM3, aboardOK = d.fuelAboardM3()
	}
	note := "observed across whatever conditions occurred in the window; head seas add time and fuel"
	if aboardOK {
		aboard := math.Round(aboardM3 * 1000)
		after := aboard - litresRounded
		fuelAboardL, fuelAfterL = &aboard, &after
	} else {
		note += "; fuel aboard unknown"
	}

	result := assistantEstimatePassageResult{
		DistanceNm:       args.DistanceNm,
		SpeedKts:         roundTo1(speedKts),
		SpeedSource:      speedSource,
		Hours:            roundTo1(hours),
		Litres:           litresRounded,
		LPerH:            roundTo1(lPerH),
		RPM:              math.Round(rpmAtSpeed),
		HistoryDays:      days,
		Samples:          totalSamples,
		Instances:        instances,
		InstancesAssumed: instancesAssumed,
		Table:            table,
		FuelAboardL:      fuelAboardL,
		FuelAfterL:       fuelAfterL,
		Note:             note,
	}

	shrink := func() bool {
		if len(result.Table) == 0 {
			return false
		}
		// Drop the least-sampled band first - it says the least about the
		// boat's own performance, and every summary field above (hours,
		// litres, l_per_h, rpm) was already computed before the table is
		// trimmed, so shrinking it never changes the answer already given.
		leastIdx := 0
		for i, band := range result.Table {
			if band.Samples < result.Table[leastIdx].Samples {
				leastIdx = i
			}
		}
		result.Table = append(result.Table[:leastIdx], result.Table[leastIdx+1:]...)
		return true
	}
	return capToolResultJSON(&result, shrink)
}
