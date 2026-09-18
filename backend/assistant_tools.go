package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// This file implements the three read-only tools the onboard assistant's
// agentic loop (assistant_run.go) can call (ADR 0093): find_places,
// get_wind_forecast and get_tides. Every dependency is injected via
// assistantToolDeps so tests never need a live SignalK connection, a
// registered WASM plugin, or a real place-names provider round trip - the same
// injectable-func idiom forecastWarningsFetcher (forecast_warnings_fetcher.go)
// already uses.

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
// plugin registry, place-names provider or InfluxDB connection.
type assistantToolDeps struct {
	now         func() time.Time
	vesselState func() (vesselStateData, error)
	weather     func() (weatherProvider, string, error)
	waves       func() (waveProvider, string, error)
	tides       func() (tideProvider, string, error)
	// placeNames resolves the currently configured place-names provider
	// (ui.place_name_provider) for find_places, the same
	// placeNameProviderResolver shape place_name.go uses - read fresh on
	// every call so a Settings change takes effect on the very next lookup.
	placeNames placeNameProviderResolver
	routes     func() []routeData
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
	// manual names the embedded operator manual pages read_manual serves
	// (assistant_manual.go). Production returns globalManual, loaded once
	// at startup; tests inject a fixed slice with no embedding involved.
	manual func() []manualPage
	// documents is the boat's document library (ADR 0106), read fresh on
	// every call for search_documents and read_document - production
	// returns globalDocumentStore; tests inject a t.TempDir()-backed
	// *documentStore with no upload/indexer pipeline involved. nil (the
	// zero value in a test that never sets it) is a real possibility, not
	// just a test artefact: this store is optional the same way the manual
	// is, so both tools report a plain error rather than panicking when
	// it's unset.
	documents func() *documentStore
	// documentSearchReadiness is search_documents' seam into
	// hybridDocumentSearch's semantic side (E1d, documents_hybrid.go) -
	// checkAssistantReadiness's own signature, mirroring documentIndexer's
	// readiness field. nil (a test that never sets it, same as every other
	// field here) means semantic search is simply unavailable to this call:
	// hybridDocumentSearch treats that exactly like a Problem, not an error,
	// so every existing search_documents test that predates E1d keeps
	// working unchanged, keyword-only.
	documentSearchReadiness func() (assistantReadiness, string, error)
}

// assistantProductionToolDeps wires the real dependencies: the live vessel
// state, the settings-configured weather/wave/tide/place-names providers,
// and a snapshot of the saved routes.
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
		placeNames: func() (placeNameProvider, string, error) {
			return resolvePlaceNameProvider(settingsPath)
		},
		routes:            assistantRouteSnapshot,
		influxRange:       queryInfluxPathRange,
		fuelRateInstances: fuelRateInstancesFromSnapshot,
		fuelAboardM3:      fuelAboardM3FromDerivedPaths,
		manual:            func() []manualPage { return globalManual },
		documents:         func() *documentStore { return globalDocumentStore },
		documentSearchReadiness: func() (assistantReadiness, string, error) {
			return checkAssistantReadiness(settingsPath)
		},
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
// (routes.go) so find_places never holds that lock while it does
// place-names provider I/O or JSON work - the same "copy under the lock,
// work outside it"
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
					"route waypoints first and then the configured place-names plugin. Centres on the vessel's " +
					"current position unless near_lat/near_lon are given. Call this before any forecast or tide " +
					"tool for a place you only know by name. Pass the bare feature name (e.g. \"Bona Bay\", not " +
					"\"Bona Bay, Gloucester Island\"). For a place well away from the vessel, or when a lookup " +
					"returns nothing, pass near_lat/near_lon of a resolved nearby feature and retry.",
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
						"name": {"type": "string", "description": "Optional label for the location, for display only."},
						"course_deg": {
							"type": "number",
							"description": "The vessel's intended course over ground in degrees true, 0 to 360; when given, each row and each day summary also carries the wind and wave angle relative to the bow."
						}
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
		{
			Type: "function",
			Function: openRouterFunctionDef{
				Name: "read_manual",
				Description: "Read one page of Helmcentral's own operator manual, or one section of it by " +
					"heading. Use it before answering any question about how Helmcentral itself works, what a " +
					"panel or tile shows, or how to set something up; quote the manual rather than guessing.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"page": {
							"type": "string",
							"description": "A manual page id from the manual index, e.g. \"features/forecast\"."
						},
						"section": {
							"type": "string",
							"description": "Optional: the text of a \"## \" heading on that page, to return just that section."
						}
					},
					"required": ["page"]
				}`),
			},
		},
		{
			Type: "function",
			Function: openRouterFunctionDef{
				Name: "search_documents",
				Description: "Search the boat's document library - manuals, receipts, logs, notes and photos " +
					"(ADR 0106) - by keyword. Returns the best-matching page or section per document with a " +
					"snippet; call read_document with a result's document_id to read more of it.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"query": {
							"type": "string",
							"description": "Keywords to search for, e.g. \"impeller\" or \"haulout invoice\"."
						},
						"tag": {
							"type": "string",
							"description": "Optional: restrict results to documents carrying this exact tag."
						},
						"folder": {
							"type": "string",
							"description": "Optional: restrict to this folder and its subfolders, given as a path such as \"Receipts/2026\"."
						},
						"limit": {
							"type": "integer",
							"description": "Maximum number of documents to return (default 5, maximum 10)."
						}
					},
					"required": ["query"]
				}`),
			},
		},
		{
			Type: "function",
			Function: openRouterFunctionDef{
				Name: "read_document",
				Description: "Read a document from the boat's document library (ADR 0106), in order, from a " +
					"given point. Use search_documents first to find the document_id, or use the id of a document " +
					"attached to this conversation. Call this again with the previous result's next_chunk to keep " +
					"reading when a result comes back truncated.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"document_id": {
							"type": "string",
							"description": "A document id from a search_documents result or an attached document."
						},
						"from_chunk": {
							"type": "integer",
							"description": "Resume from this chunk sequence number (a previous result's next_chunk). Omit to start from the beginning of the document body."
						}
					},
					"required": ["document_id"]
				}`),
			},
		},
	}
}

// ── dispatch ────────────────────────────────────────────────────────────

// execute runs one tool call by name and returns its result as compact
// JSON, capped at assistantMaxToolResultChars (see capToolResultJSON). ctx
// is the tool round's context: assistant_run.go's runToolRound now runs one
// round's calls concurrently, one goroutine per call, all sharing this same
// ctx, so a cancelled run (the operator closes the tab, or the run's own
// timeout fires) is visible to every executeXxx below before it starts, and
// between its outbound calls where it makes more than one. None of the
// place-names provider (placeNameProvider.SearchPlaces), InfluxDB
// (queryInfluxPathRange) or the weather/wave/tide provider interfaces
// (weatherProvider.FetchForecast and friends) take a context today, so a
// request already in flight when ctx
// is cancelled still runs to completion - threading a context through those
// layers is out of scope here (backend performance audit, Tier 3).
func (d assistantToolDeps) execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	switch name {
	case "find_places":
		return d.executeFindPlaces(ctx, args)
	case "get_wind_forecast":
		return d.executeGetWindForecast(ctx, args)
	case "get_tides":
		return d.executeGetTides(ctx, args)
	case "estimate_passage":
		return d.executeEstimatePassage(ctx, args)
	case "read_manual":
		return d.executeReadManual(ctx, args)
	case "search_documents":
		return d.executeSearchDocuments(ctx, args)
	case "read_document":
		return d.executeReadDocument(ctx, args)
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
	case "read_manual":
		var a assistantReadManualArgs
		page := ""
		if json.Unmarshal(args, &a) == nil {
			page = strings.TrimSpace(a.Page)
		}
		if page == "" {
			page = "a page"
		}
		return fmt.Sprintf("Reading the manual: %s…", page)
	case "search_documents":
		var a assistantSearchDocumentsArgs
		query := ""
		if json.Unmarshal(args, &a) == nil {
			query = strings.TrimSpace(a.Query)
		}
		if query == "" {
			query = "the documents"
			return fmt.Sprintf("Searching %s…", query)
		}
		return fmt.Sprintf("Searching documents for %q…", query)
	case "read_document":
		// No access to the document store here (describeAssistantToolCall is
		// a pure function of name+args, the same as read_manual's case just
		// above it, which shows the manual's own page id rather than
		// resolving a title) - the document id is what's shown, not a
		// filename.
		var a assistantReadDocumentArgs
		id := ""
		if json.Unmarshal(args, &a) == nil {
			id = strings.TrimSpace(a.DocumentID)
		}
		if id == "" {
			return "Reading a document…"
		}
		return fmt.Sprintf("Reading document %s…", id)
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

// assistantWindForecastArgs is get_wind_forecast's own argument shape:
// assistantLocationArgs plus an optional course_deg, the vessel's intended
// course over ground. get_tides has no use for a course, so it keeps using
// bare assistantLocationArgs; this extra field is exclusive to
// get_wind_forecast. CourseDeg is a pointer so "the model left it out" (no
// relative-angle fields in the result at all) is distinguishable from a
// genuine course of 0 (due north).
type assistantWindForecastArgs struct {
	assistantLocationArgs
	CourseDeg *float64 `json:"course_deg"`
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
// distance/bearing, applied here to the place-names provider and to route
// waypoints alike).
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

// dedupeAssistantPlaceCandidates drops a later candidate that shares a
// case-insensitive name with, and sits within 500m of, an earlier one
// already kept - the same real-world feature returned twice (a saved
// waypoint and the place-names provider's own copy of it, most often)
// rather than two distinct features that happen to share a name. Earlier
// candidates win, which in practice means a route waypoint (added before
// the provider's results) is preferred over the provider's guess at the
// same place.
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

// executeFindPlaces searches the vessel's saved route waypoints first, then
// hands the query to the configured place-names provider's search_places
// (ADR 0101) - the provider owns the actual search ladder (an exact-name
// rung, and a broader rung it runs only when told to); this host only ever
// does what it always has for every provider: compute distance/bearing,
// dedupe against waypoints, sort, and trim to maxResults.
func (d assistantToolDeps) executeFindPlaces(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

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

	if err := ctx.Err(); err != nil {
		return "", err
	}

	provider, providerID, err := d.placeNames()
	if err != nil {
		return "", fmt.Errorf("find_places: %w", err)
	}

	// Broad tells the provider whether its broader (and more expensive)
	// search rung is worth running at all: a waypoint hit means the
	// operator's own answer is already in hand, so only the provider's
	// cheap exact-name search runs - it's still always run, even after a
	// waypoint hit, since it's the only way the provider's own position for
	// the same place ever reaches the model.
	searchResult, err := provider.SearchPlaces(placeSearchInput{
		Query:      query,
		Lat:        lat,
		Lon:        lon,
		MaxResults: maxResults,
		Broad:      !waypointHit,
	})
	if err != nil {
		return "", fmt.Errorf("find_places: %w", err)
	}

	for _, m := range searchResult.Results {
		candidates = append(candidates, assistantPlaceCandidate{
			Name:       m.Name,
			Kind:       m.Kind,
			Lat:        m.Lat,
			Lon:        m.Lon,
			DistanceNm: roundTo1(haversineMeters(lat, lon, m.Lat, m.Lon) / metersPerNauticalMile),
			BearingDeg: int(math.Round(bearingDeg(lat, lon, m.Lat, m.Lon))),
			Source:     providerID,
		})
	}

	candidates = dedupeAssistantPlaceCandidates(candidates)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].DistanceNm < candidates[j].DistanceNm })
	if len(candidates) > maxResults {
		candidates = candidates[:maxResults]
	}

	search := searchResult.Search
	if waypointHit {
		search = "waypoints"
	}

	centre := assistantLatLon{Lat: lat, Lon: lon}
	centredOn := ""
	if searchResult.CentredOn != nil {
		centre = assistantLatLon{Lat: searchResult.CentredOn.Lat, Lon: searchResult.CentredOn.Lon}
		centredOn = searchResult.CentredOn.Name
	}

	result := assistantFindPlacesResult{
		Centre:    &centre,
		CentredOn: centredOn,
		RadiusNm:  searchResult.RadiusNm,
		Search:    search,
		Results:   candidates,
		Note:      searchResult.Note,
	}
	if len(candidates) == 0 && result.Note == "" {
		result.Note = fmt.Sprintf("no named feature matching %q found within %g nm of the search centre", query, searchResult.RadiusNm)
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
	// RelWindDeg/RelWind and RelWaveDeg/RelWave are the wind/wave angle
	// relative to the course_deg the model passed to get_wind_forecast
	// (relativeAngleDeg/relativeAngleLabel, assistant_geometry.go) - the
	// host doing this arithmetic instead of the model is the whole point
	// (ADR 0093 §2). omitempty on all four: a call with no course_deg
	// leaves every one of these nil, so the result is byte-for-byte what
	// it was before this field existed. Also nil, even when course_deg is
	// given, whenever the underlying direction itself is unknown (Dir/
	// WaveDir nil) or there is no matching wave row.
	RelWindDeg *int    `json:"rel_wind_deg,omitempty"`
	RelWind    *string `json:"rel_wind,omitempty"`
	RelWaveDeg *int    `json:"rel_wave_deg,omitempty"`
	RelWave    *string `json:"rel_wave,omitempty"`
}

type assistantDaySummary struct {
	Date          string  `json:"date"`
	WindKtsMin    *int    `json:"wind_kts_min"`
	WindKtsMax    *int    `json:"wind_kts_max"`
	GustKtsMax    *int    `json:"gust_kts_max"`
	DirPrevailing *string `json:"dir_prevailing"`
	// RelWindPrevailing is the modal rel_wind label across the day's kept
	// rows (assistantDayAggregate.prevailingRelWind), present only when
	// course_deg was given - see RelWind above.
	RelWindPrevailing *string `json:"rel_wind_prevailing,omitempty"`
}

type assistantWindForecastResult struct {
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	// CourseDeg echoes the model's course_deg argument back, present only
	// when it was given - the signal to the model that rel_wind/rel_wave
	// are populated below rather than silently absent.
	CourseDeg       *float64              `json:"course_deg,omitempty"`
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
	// relWindVotes/relWindOrder are the same vote-counting shape as
	// dirVotes/dirOrder, over each kept row's rel_wind label rather than
	// its compass direction - only populated when course_deg was given.
	relWindVotes map[string]int
	relWindOrder []string
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
	return modeVote(a.dirVotes, a.dirOrder)
}

func (a *assistantDayAggregate) addRelWind(label string) {
	if a.relWindVotes == nil {
		a.relWindVotes = map[string]int{}
	}
	if _, seen := a.relWindVotes[label]; !seen {
		a.relWindOrder = append(a.relWindOrder, label)
	}
	a.relWindVotes[label]++
}

// prevailingRelWind returns the mode of the recorded rel_wind label votes -
// the day summary's rel_wind_prevailing - ties broken by first-seen order
// exactly like prevailing() above.
func (a *assistantDayAggregate) prevailingRelWind() *string {
	return modeVote(a.relWindVotes, a.relWindOrder)
}

// modeVote returns the highest-voted key in order, or nil when order is
// empty. Shared by prevailing() and prevailingRelWind(), which differ only
// in which vote map/order pair they track.
func modeVote(votes map[string]int, order []string) *string {
	best := ""
	bestCount := 0
	for _, key := range order {
		if votes[key] > bestCount {
			best = key
			bestCount = votes[key]
		}
	}
	if best == "" {
		return nil
	}
	return &best
}

func (d assistantToolDeps) executeGetWindForecast(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var args assistantWindForecastArgs
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

	if err := ctx.Err(); err != nil {
		return "", err
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

		// course_deg is the model passing along the vessel's intended
		// course over ground; the host computes the relative angle rather
		// than leaving the model to subtract bearings itself (ADR 0093
		// §2). Nil whenever the wind direction itself is unknown (dir ==
		// nil), even with course_deg given.
		if args.CourseDeg != nil && dir != nil {
			relDeg := relativeAngleDeg(*args.CourseDeg, hp.WindDirectionDeg)
			relLabel := relativeAngleLabel(relDeg)
			row.RelWindDeg, row.RelWind = &relDeg, &relLabel
		}

		if whp, ok := waveByUnix[hp.Time.Unix()]; ok && whp.WavePeriodS != 0 {
			hm := roundTo1(whp.WaveHeightM)
			ps := roundTo1(whp.WavePeriodS)
			wd := degreesToDirection(whp.WaveDirectionDeg)
			row.WaveM, row.WaveS, row.WaveDir = &hm, &ps, &wd

			if args.CourseDeg != nil {
				relDeg := relativeAngleDeg(*args.CourseDeg, whp.WaveDirectionDeg)
				relLabel := relativeAngleLabel(relDeg)
				row.RelWaveDeg, row.RelWave = &relDeg, &relLabel
			}
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
		if row.RelWind != nil {
			agg.addRelWind(*row.RelWind)
		}
	}

	days2 := make([]assistantDaySummary, 0, len(dayOrder))
	for _, dayKey := range dayOrder {
		agg := dayAggs[dayKey]
		days2 = append(days2, assistantDaySummary{
			Date:              dayKey,
			WindKtsMin:        agg.windMin,
			WindKtsMax:        agg.windMax,
			GustKtsMax:        agg.gustMax,
			DirPrevailing:     agg.prevailing(),
			RelWindPrevailing: agg.prevailingRelWind(),
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
		CourseDeg:       args.CourseDeg,
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

func (d assistantToolDeps) executeGetTides(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

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
func (d assistantToolDeps) executeEstimatePassage(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

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
		if err := ctx.Err(); err != nil {
			return "", err
		}
		series, ferr := d.influxRange(fmt.Sprintf("propulsion.%s.fuel.rate", instance), start, stop, assistantPerformanceQueryEvery)
		if ferr != nil {
			return "", fmt.Errorf("estimate_passage: %w", ferr)
		}
		fuelRates = append(fuelRates, series)
	}

	if err := ctx.Err(); err != nil {
		return "", err
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

// ── read_manual ─────────────────────────────────────────────────────────

type assistantReadManualArgs struct {
	Page    string `json:"page"`
	Section string `json:"section"`
}

type assistantReadManualResult struct {
	Page    string `json:"page"`
	Title   string `json:"title"`
	Section string `json:"section,omitempty"`
	Content string `json:"content"`
	// Truncated marks a content string capToolResultJSON's shrink had to
	// halve to fit the chat-context budget - a legible partial answer, not
	// the AGENTS.md fallback-policy kind of masked failure (see
	// capToolResultJSON's own doc comment).
	Truncated bool `json:"truncated,omitempty"`
}

func (d assistantToolDeps) executeReadManual(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var args assistantReadManualArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse read_manual arguments: %w", err)
	}

	// d.manual is a func field: assistantToolDeps' zero value (a test that
	// never sets it, same as documents below) leaves it nil, and calling a
	// nil func panics - the doc comment on the manual field promises a
	// plain error instead, so this is checked before ever calling it.
	if d.manual == nil {
		return "", fmt.Errorf("the manual is not embedded in this build (run make manual-stage)")
	}
	pages := d.manual()
	if len(pages) == 0 {
		return "", fmt.Errorf("the manual is not embedded in this build (run make manual-stage)")
	}

	page := strings.TrimSpace(args.Page)
	ids := make([]string, 0, len(pages))
	var found *manualPage
	for i := range pages {
		ids = append(ids, pages[i].ID)
		if pages[i].ID == page {
			found = &pages[i]
		}
	}
	if found == nil {
		return "", fmt.Errorf("read_manual: unknown page %q; valid ids are: %s", page, strings.Join(ids, ", "))
	}

	result := assistantReadManualResult{Page: found.ID, Title: found.Title, Content: found.Body}

	if section := strings.TrimSpace(args.Section); section != "" {
		text, ok := manualSection(found.Body, section)
		if !ok {
			headings := manualPageHeadings(found.Body)
			return "", fmt.Errorf("read_manual: unknown section %q on page %q; valid headings are: %s", section, found.ID, strings.Join(headings, ", "))
		}
		result.Section = section
		result.Content = text
	}

	// Halve the content string until the encoding fits the tool-result
	// budget, rather than one of the list-trimming shrinks the other tools
	// use above - a manual page's content is prose, not a list of rows.
	shrink := func() bool {
		runes := []rune(result.Content)
		if len(runes) == 0 {
			return false
		}
		result.Content = string(runes[:len(runes)/2])
		result.Truncated = true
		return true
	}
	return capToolResultJSON(&result, shrink)
}

// ── search_documents ────────────────────────────────────────────────────

type assistantSearchDocumentsArgs struct {
	Query  string `json:"query"`
	Tag    string `json:"tag"`
	Folder string `json:"folder"`
	Limit  int    `json:"limit"`
}

// assistantDocumentSearchHit is one row of search_documents' result: the
// best-matching chunk of one document (documentStore.Search already
// collapses several matching chunks of the same document down to its
// single best one), plus the folder path the tool resolves separately -
// documentSearchResult (documents_store.go) has no folder_path field of its
// own, since the B3 HTTP API this struct otherwise mirrors has no use for
// it (a folder-scoped request already knows what folder it asked for).
type assistantDocumentSearchHit struct {
	DocumentID string `json:"document_id"`
	Filename   string `json:"filename"`
	FolderPath string `json:"folder_path,omitempty"`
	Title      string `json:"title,omitempty"`
	Page       int    `json:"page,omitempty"`
	Snippet    string `json:"snippet"`
	Status     string `json:"status"`
}

type assistantSearchDocumentsResult struct {
	Results []assistantDocumentSearchHit `json:"results"`
}

// assistantDocumentSnippetMarkers strips the \x02/\x03 highlight markers
// documentStore.Search's snippet() call wraps a matched term in
// (documents_store.go) - stripped to nothing rather than rendered as
// e.g. "**word**" markdown (a deliberate choice between the two options
// noted where this tool was specified): a model reading a raw snippet has
// no use for "this word was highlighted" as a fact, and stripping avoids
// teaching it to echo literal ** markers back at the operator.
var assistantDocumentSnippetMarkers = strings.NewReplacer("\x02", "", "\x03", "")

// documentStore resolves d.documents into a ready *documentStore, or the
// same plain "not available" error search_documents and read_document both
// report - whichever of two ways the document library can be unset: d.documents
// itself is nil (assistantToolDeps' own zero value, a real possibility per
// its doc comment - calling a nil func panics, so this is checked first) or
// it returns a nil store (production's own globalDocumentStore, unset when
// no document library has ever been initialised).
func (d assistantToolDeps) documentStore(toolName string) (*documentStore, error) {
	if d.documents == nil {
		return nil, fmt.Errorf("%s: the document library is not available", toolName)
	}
	store := d.documents()
	if store == nil {
		return nil, fmt.Errorf("%s: the document library is not available", toolName)
	}
	return store, nil
}

// executeSearchDocuments answers search_documents: ftsMatchQuery sanitises
// the operator's free-typed query into an FTS5 MATCH string exactly the way
// the B3 HTTP API's own search does (documents_handlers.go), then
// store.Search runs it, optionally scoped to a folder (and its subtree)
// resolved from a "/"-separated path via ResolveFolderPath - the tool's own
// convenience over the HTTP API's raw folder id, since a model has no way
// to know a folder's uuid ahead of time.
func (d assistantToolDeps) executeSearchDocuments(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	store, err := d.documentStore("search_documents")
	if err != nil {
		return "", err
	}

	var args assistantSearchDocumentsArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse search_documents arguments: %w", err)
	}

	if _, ok := ftsMatchQuery(args.Query); !ok {
		return "", fmt.Errorf("search_documents: query must not be empty")
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}

	var folderID *string
	if folder := strings.TrimSpace(args.Folder); folder != "" {
		id, err := store.ResolveFolderPath(folder)
		if err != nil {
			if errors.Is(err, errFolderNotFound) {
				names, lerr := store.TopLevelFolderNames()
				if lerr != nil || len(names) == 0 {
					return "", fmt.Errorf("search_documents: unknown folder %q", folder)
				}
				return "", fmt.Errorf("search_documents: unknown folder %q; top-level folders: %s", folder, strings.Join(names, ", "))
			}
			return "", fmt.Errorf("search_documents: resolve folder %q: %w", folder, err)
		}
		folderID = &id
	}

	// recursive=true unconditionally: with no folder given, folderID is nil
	// and hybridDocumentSearch/Search both treat that as no folder filter at
	// all regardless of this flag (documentStore.Search's own doc comment),
	// so it only ever takes effect when a folder path was actually resolved
	// above - where the tool's contract (assistant_tools.go's own spec)
	// requires it anyway.
	//
	// hybridDocumentSearch (E1d, documents_hybrid.go), not a bare
	// store.Search: semantic search is worth more to a model asking "what
	// did the marine supplies receipt come to" than to a human typing a
	// part number, and it's the identical search the HTTP API's own search
	// box runs. d.documentSearchReadiness nil (a test that never wired it,
	// same as every field on assistantToolDeps) just means semantic search
	// never runs here - keyword-only, exactly this tool's behaviour before
	// E1d. mode/semantic_problem are deliberately not surfaced in this
	// tool's own result shape (search_documents' spec, this file) - a model
	// has no use for knowing which retriever found what.
	outcome, err := hybridDocumentSearch(ctx, documentSearchParams{
		Store:     store,
		Query:     args.Query,
		FolderID:  folderID,
		Recursive: true,
		Tag:       strings.TrimSpace(args.Tag),
		Limit:     limit,
		Readiness: d.documentSearchReadiness,
	})
	if err != nil {
		return "", fmt.Errorf("search_documents: %w", err)
	}
	hits := outcome.Results

	// folderPaths caches one resolved path per distinct folder id across
	// this call's hits, rather than resolving it again for every hit that
	// happens to share a folder - the fix for the N+1 this tool used to run
	// (documentFolderPathString's old signature did a Get+FolderPath per
	// hit; the folder id now rides along on the search row itself, see
	// documentSearchResult).
	result := assistantSearchDocumentsResult{Results: make([]assistantDocumentSearchHit, 0, len(hits))}
	folderPaths := map[string]string{}
	for _, h := range hits {
		var path string
		if h.FolderID != nil {
			cached, ok := folderPaths[*h.FolderID]
			if !ok {
				resolved, err := documentFolderPathString(store, *h.FolderID)
				if err != nil {
					return "", fmt.Errorf("search_documents: resolve folder path for %s: %w", h.DocumentID, err)
				}
				folderPaths[*h.FolderID] = resolved
				cached = resolved
			}
			path = cached
		}
		result.Results = append(result.Results, assistantDocumentSearchHit{
			DocumentID: h.DocumentID,
			Filename:   h.Filename,
			FolderPath: path,
			Title:      h.Title,
			Page:       h.PageStart,
			Snippet:    assistantDocumentSnippetMarkers.Replace(h.Snippet),
			Status:     h.Status,
		})
	}

	shrink := func() bool {
		if len(result.Results) == 0 {
			return false
		}
		result.Results = result.Results[:len(result.Results)-1]
		return true
	}
	return capToolResultJSON(&result, shrink)
}

// documentFolderPathString renders folderID's chain as a "/"-joined path
// ("Manuals/Engine"). Takes the folder id directly (documentSearchResult
// now carries it - see its doc comment) rather than a document id, so
// resolving it never needs its own document Get: the caller already has
// whatever document fields it needs from the search row itself. A
// store.FolderPath failure propagates rather than collapsing to "" - a
// review finding: silently swallowing it meant a real database failure
// looked identical to "this document has no folder" (AGENTS.md's fallback
// policy: fail fast, don't mask an upstream problem as an empty result).
func documentFolderPathString(store *documentStore, folderID string) (string, error) {
	chain, err := store.FolderPath(folderID)
	if err != nil {
		return "", err
	}
	names := make([]string, len(chain))
	for i, f := range chain {
		names[i] = f.Name
	}
	return strings.Join(names, "/"), nil
}

// ── read_document ───────────────────────────────────────────────────────

type assistantReadDocumentArgs struct {
	DocumentID string `json:"document_id"`
	FromChunk  int    `json:"from_chunk"`
}

// assistantDocumentChunkOut is one row of read_document's chunks list -
// documentChunk (documents_store.go) trimmed to what the model needs: no
// internal rowid, document_id (already named once at the result's top
// level) or source ("local" vs "ocr" is an indexing detail, not something
// worth spending context on).
type assistantDocumentChunkOut struct {
	Seq       int    `json:"seq"`
	PageStart int    `json:"page_start,omitempty"`
	Heading   string `json:"heading,omitempty"`
	Text      string `json:"text"`
}

type assistantReadDocumentResult struct {
	DocumentID string                      `json:"document_id"`
	Filename   string                      `json:"filename"`
	Chunks     []assistantDocumentChunkOut `json:"chunks"`
	// NextChunk is set only once capToolResultJSON's shrink has actually
	// dropped chunks to fit the budget - 0 otherwise, safe as an "absent"
	// sentinel because a real body chunk's seq is never 0 (seq 0 is always
	// the meta chunk, which FromChunk's own default skips).
	NextChunk int  `json:"next_chunk,omitempty"`
	Truncated bool `json:"truncated,omitempty"`
}

// executeReadDocument answers read_document: every chunk of doc from
// from_chunk onward (default 1, skipping the seq-0 meta chunk - a model
// reading a document's body has no use for its own title and tags restated
// as a "chunk"), shrunk to fit assistantMaxToolResultChars.
func (d assistantToolDeps) executeReadDocument(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	store, err := d.documentStore("read_document")
	if err != nil {
		return "", err
	}

	var args assistantReadDocumentArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse read_document arguments: %w", err)
	}

	docID := strings.TrimSpace(args.DocumentID)
	doc, err := store.Get(docID)
	if err != nil {
		if errors.Is(err, errDocumentNotFound) {
			return "", fmt.Errorf("read_document: unknown document %q", docID)
		}
		return "", fmt.Errorf("read_document: %w", err)
	}

	from := args.FromChunk
	if from < 1 {
		from = 1
	}
	chunks, err := store.ChunksFrom(docID, from)
	if err != nil {
		return "", fmt.Errorf("read_document: %w", err)
	}

	result := assistantReadDocumentResult{DocumentID: doc.ID, Filename: doc.Filename}
	for _, c := range chunks {
		result.Chunks = append(result.Chunks, assistantDocumentChunkOut{
			Seq: c.Seq, PageStart: c.PageStart, Heading: c.Heading, Text: c.Text,
		})
	}

	// Shrink by halving - the same strategy executeReadManual applies to its
	// (single, prose) content string above, adapted to a list: halve the
	// number of chunks kept rather than the characters of each, since a
	// chunk is already bounded to ~2000 characters by B2's own chunking
	// rule and next_chunk needs a whole chunk boundary to resume from
	// cleanly, not a byte offset partway through one.
	shrink := func() bool {
		n := len(result.Chunks)
		if n <= 1 {
			return false
		}
		keep := n / 2
		result.NextChunk = result.Chunks[keep].Seq
		result.Chunks = result.Chunks[:keep]
		result.Truncated = true
		return true
	}
	return capToolResultJSON(&result, shrink)
}
