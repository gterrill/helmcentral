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
)

// This file implements the three read-only tools the onboard assistant's
// agentic loop (assistant_run.go) can call (ADR 0093): find_places,
// get_wind_forecast and get_tides. Every dependency is injected via
// assistantToolDeps so tests never need a live SignalK connection, a
// registered WASM plugin, or a real Overpass round trip - the same
// injectable-func idiom forecastWarningsFetcher (forecast_warnings_fetcher.go)
// already uses.

// assistantFindPlacesRadiusNm is find_places' fixed search radius. 100nm is
// generous enough to cover a multi-day passage's worth of candidate
// anchorages without the operator ever having to think about a radius
// parameter - there is deliberately no way for the model to widen or narrow
// it.
const assistantFindPlacesRadiusNm = 100.0

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

// assistantToolDeps are every real-world dependency the three tools need,
// injected so tests supply fakes/stubs instead of a live SignalK, WASM
// plugin registry or Overpass endpoint.
type assistantToolDeps struct {
	now         func() time.Time
	vesselState func() (vesselStateData, error)
	weather     func() (weatherProvider, string, error)
	waves       func() (waveProvider, string, error)
	tides       func() (tideProvider, string, error)
	overpass    overpassFetcher
	routes      func() []routeData
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
		overpass: overpassHTTPClient,
		routes:   assistantRouteSnapshot,
	}
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
					"know by name.",
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
	Centre    *assistantLatLon          `json:"centre,omitempty"`
	RadiusNm  float64                   `json:"radius_nm,omitempty"`
	Results   []assistantPlaceCandidate `json:"results"`
	Note      string                    `json:"note,omitempty"`
	Truncated bool                      `json:"truncated,omitempty"`
}

// buildOverpassNameSearchQuery builds the Overpass QL for a name search
// within radiusMeters of (lat, lon), matching any of the four tag clauses
// find_places understands: natural coastal features, place types, seamark
// facilities, and OSM's separate leisure=marina tagging (which is not part
// of the seamark vocabulary but is how most real-world marinas are tagged).
// query is regexp.QuoteMeta-escaped and then escaped again for the Overpass
// QL string literal syntax (backslash and double quote) - see
// escapeOverpassStringLiteral - so a query containing e.g. parentheses or a
// literal quote can never break out of the generated query.
func buildOverpassNameSearchQuery(query string, radiusMeters int, lat, lon float64) string {
	q := escapeOverpassStringLiteral(regexp.QuoteMeta(query))
	around := fmt.Sprintf("%d,%.6f,%.6f", radiusMeters, lat, lon)
	return fmt.Sprintf(
		`[out:json][timeout:25];(nwr["name"~"%s",i]["natural"~"^(bay|reef|beach|cape|strait|peninsula|shoal|inlet)$"](around:%s);nwr["name"~"%s",i]["place"~"^(island|islet|locality|hamlet|village|town|archipelago)$"](around:%s);nwr["name"~"%s",i]["seamark:type"~"^(anchorage|harbour|mooring|marina|small_craft_facility)$"](around:%s);nwr["name"~"%s",i]["leisure"="marina"](around:%s););out tags center 30;`,
		q, around, q, around, q, around, q, around,
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
	return ""
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
	for _, route := range d.routes() {
		for _, wp := range route.Waypoints {
			name := strings.TrimSpace(wp.Name)
			if name == "" || !strings.Contains(strings.ToLower(name), lowerQuery) {
				continue
			}
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

	radiusMeters := int(assistantFindPlacesRadiusNm * metersPerNauticalMile)
	elements, err := postOverpassQuery(d.overpass, buildOverpassNameSearchQuery(query, radiusMeters, lat, lon))
	if err != nil {
		return "", fmt.Errorf("find_places: %w", err)
	}
	for _, el := range elements {
		elLat, elLon, ok := elementLatLon(el)
		if !ok {
			continue
		}
		name := strings.TrimSpace(el.Tags["name"])
		if name == "" {
			continue
		}
		candidates = append(candidates, assistantPlaceCandidate{
			Name:       name,
			Kind:       overpassFindPlacesKind(el.Tags),
			Lat:        elLat,
			Lon:        elLon,
			DistanceNm: roundTo1(haversineMeters(lat, lon, elLat, elLon) / metersPerNauticalMile),
			BearingDeg: int(math.Round(bearingDeg(lat, lon, elLat, elLon))),
			Source:     "osm",
		})
	}

	candidates = dedupeAssistantPlaceCandidates(candidates)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].DistanceNm < candidates[j].DistanceNm })
	if len(candidates) > maxResults {
		candidates = candidates[:maxResults]
	}

	result := assistantFindPlacesResult{
		Centre:   &assistantLatLon{Lat: lat, Lon: lon},
		RadiusNm: assistantFindPlacesRadiusNm,
		Results:  candidates,
	}
	if len(candidates) == 0 {
		result.Note = fmt.Sprintf("no named feature matching %s within %g nm", query, assistantFindPlacesRadiusNm)
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
