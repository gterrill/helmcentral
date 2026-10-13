package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

var mateToolFindPlaces = mateTool{
	name: "find_places",
	function: openRouterFunctionDef{
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
	execute: assistantToolDeps.executeFindPlaces,
	describe: func(args json.RawMessage) string {
		var a assistantFindPlacesArgs
		query := ""
		if json.Unmarshal(args, &a) == nil {
			query = strings.TrimSpace(a.Query)
		}
		if query == "" {
			query = "a place"
		}
		return fmt.Sprintf("Looking up %s…", query)
	},
}

// ── find_places ──────────────────────────────────────────────────────────

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
	// M-4 finding: query is the only text that reaches a third-party host
	// (the configured place-names provider) verbatim, so it is bounded and
	// charset-checked before anything downstream - including the model's
	// own guidance in assistant_prompt.go - ever sees it, rather than
	// trusting the prompt alone to keep the model from passing through
	// whatever a document told it to encode here.
	if n := utf8.RuneCountInString(query); n > assistantFindPlacesMaxQueryRunes {
		return "", fmt.Errorf("find_places: query is %d characters, longer than the %d-character limit for a place name search", n, assistantFindPlacesMaxQueryRunes)
	}
	if !assistantFindPlacesQueryPattern.MatchString(query) {
		return "", fmt.Errorf("find_places: query %q contains characters not valid in a place name search", query)
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
	waypointHit := false
	for _, route := range d.routes() {
		for _, wp := range route.Waypoints {
			name := strings.TrimSpace(wp.Name)
			// A waypoint has no MMSI-like identifier of its own, so id is
			// always "" here - assistantNameOrExactIDMatches then reduces to
			// the case-insensitive substring check this line always did
			// (shared with matchAssistantNearbyVessels and
			// latestContactsByName - code-review finding, 2026-09-25).
			if !assistantNameOrExactIDMatches(name, "", query) {
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
	searchResult, err := searchPlacesCached(provider, providerID, placeSearchInput{
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
