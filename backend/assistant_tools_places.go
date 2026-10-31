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
			"to one or more candidate positions, each with its straight-line distance and bearing from the search centre " +
			"(straight_line_nm, straight_line_bearing_deg: a line across the chart that can cross islands, never a passage " +
			"distance; use estimate_passage for that), searching the " +
			"configured place-names plugin. Centres on the vessel's " +
			"current position unless near_lat/near_lon are given. Call this before any forecast or tide " +
			"tool for a place you only know by name. Pass the bare feature name (e.g. \"Bona Bay\", not " +
			"\"Bona Bay, Gloucester Island\"). For a place well away from the vessel, or when a lookup " +
			"returns nothing, pass near_lat/near_lon of a resolved nearby feature and retry. If a name still will not resolve, " +
			"say so and use the nearest resolved feature's position, telling the operator that is what you did. " +
			"When several candidates come back, say which candidate you chose and why. A result is a charted feature, " +
			"not a recommendation. straight_line_bearing_deg is the bearing along that same straight line: never a course.",
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
// host-computed straight-line distance and bearing from the search centre
// (ADR 0091's rule that a provider returns raw features and the host alone
// computes distance/bearing, applied here to the place-names provider and to
// route waypoints alike).
//
// The figures are named for what they are. A straight line crosses islands
// and headlands, so it is a way to sort candidates and say "about", never
// the length of a passage: estimate_passage routes that over water.
type assistantPlaceCandidate struct {
	Name string  `json:"name"`
	Kind string  `json:"kind"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	// The straight-line figures are nil when there was no search centre to
	// measure from: 0 nm would be a claim.
	StraightLineNm         *float64 `json:"straight_line_nm,omitempty"`
	StraightLineBearingDeg *int     `json:"straight_line_bearing_deg,omitempty"`
	Source                 string   `json:"source"`
	// Note is the operator's own note on a saved place.
	Note string `json:"note,omitempty"`
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
// already kept - the same real-world feature returned twice rather than two
// distinct features that happen to share a name. Earlier candidates win.
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

// findPlacesQueryError is a query the search refuses to send: empty, too long
// or with characters a place name never has. The planned-stay search answers
// it as a 400; Mate sees the same text as any other tool error.
type findPlacesQueryError struct{ msg string }

func (e *findPlacesQueryError) Error() string { return e.msg }

// executeFindPlaces hands the query to the configured place-names provider's
// search_places (ADR 0101) - the provider owns the actual search ladder (an
// exact-name rung, and a broader rung it runs when told to); this host
// computes distance/bearing, dedupes, sorts, and trims to maxResults.
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
		return "", &findPlacesQueryError{msg: "find_places: query is required"}
	}
	// M-4 finding: query is the only text that reaches a third-party host
	// (the configured place-names provider) verbatim, so it is bounded and
	// charset-checked before anything downstream - including the model's
	// own guidance in assistant_prompt.go - ever sees it, rather than
	// trusting the prompt alone to keep the model from passing through
	// whatever a document told it to encode here.
	if n := utf8.RuneCountInString(query); n > assistantFindPlacesMaxQueryRunes {
		return "", &findPlacesQueryError{msg: fmt.Sprintf("find_places: query is %d characters, longer than the %d-character limit for a place name search", n, assistantFindPlacesMaxQueryRunes)}
	}
	if !assistantFindPlacesQueryPattern.MatchString(query) {
		return "", &findPlacesQueryError{msg: fmt.Sprintf("find_places: query %q contains characters not valid in a place name search", query)}
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

	// The operator's own saved places come first. A hit is their position for
	// their name, so it is the answer and the provider is not asked. It needs
	// no search centre: without one, distance and bearing are left out.
	saved, err := d.savedPlaceCandidate(query, lat, lon, haveCentre)
	if err != nil {
		return "", err
	}
	if saved != nil {
		result := assistantFindPlacesResult{
			Search:  "saved",
			Results: []assistantPlaceCandidate{*saved},
			Note:    "The operator's saved place, in the position they gave. Use it as the place.",
		}
		if haveCentre {
			result.Centre = &assistantLatLon{Lat: lat, Lon: lon}
		}
		return capToolResultJSON(&result, nil)
	}

	if !haveCentre {
		result := assistantFindPlacesResult{
			Results: []assistantPlaceCandidate{},
			Note:    "vessel position unknown; ask the operator for a region or coordinates",
		}
		return capToolResultJSON(&result, nil)
	}

	var candidates []assistantPlaceCandidate

	if err := ctx.Err(); err != nil {
		return "", err
	}

	provider, providerID, err := d.placeNames()
	if err != nil {
		return "", fmt.Errorf("find_places: %w", err)
	}

	searchResult, err := searchPlacesCached(provider, providerID, placeSearchInput{
		Query:      query,
		Lat:        lat,
		Lon:        lon,
		MaxResults: maxResults,
		Broad:      true,
	})
	if err != nil {
		return "", fmt.Errorf("find_places: %w", err)
	}

	for _, m := range searchResult.Results {
		nm, bearing := straightLineFrom(lat, lon, m.Lat, m.Lon)
		candidates = append(candidates, assistantPlaceCandidate{
			Name:                   m.Name,
			Kind:                   m.Kind,
			Lat:                    m.Lat,
			Lon:                    m.Lon,
			StraightLineNm:         &nm,
			StraightLineBearingDeg: &bearing,
			Source:                 providerID,
		})
	}

	candidates = dedupeAssistantPlaceCandidates(candidates)
	sort.Slice(candidates, func(i, j int) bool { return *candidates[i].StraightLineNm < *candidates[j].StraightLineNm })
	if len(candidates) > maxResults {
		candidates = candidates[:maxResults]
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
		Search:    searchResult.Search,
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

// straightLineFrom is the host-computed straight-line distance (nm, one
// decimal) and bearing (whole degrees) from (lat, lon) to (toLat, toLon).
func straightLineFrom(lat, lon, toLat, toLon float64) (float64, int) {
	return roundTo1(haversineMeters(lat, lon, toLat, toLon) / metersPerNauticalMile),
		int(math.Round(bearingDeg(lat, lon, toLat, toLon)))
}

// savedPlaceCandidate is the operator's saved place the query names, with
// distance and bearing from (lat, lon) as a provider result gets, or nil when
// none matches. Names match by savedPlaceKey, so there is at most one. A
// missing record store is an error, as it is for save_place.
func (d assistantToolDeps) savedPlaceCandidate(query string, lat, lon float64, haveCentre bool) (*assistantPlaceCandidate, error) {
	store, err := d.recordStore("find_places")
	if err != nil {
		return nil, err
	}
	place, err := findSavedPlaceByName(store, query)
	if err != nil {
		return nil, fmt.Errorf("find_places: %w", err)
	}
	if place == nil {
		return nil, nil
	}
	c := &assistantPlaceCandidate{
		Name: place.Name, Kind: "saved place", Lat: place.Lat, Lon: place.Lon, Source: "saved", Note: place.Note,
	}
	if haveCentre {
		nm, bearing := straightLineFrom(lat, lon, place.Lat, place.Lon)
		c.StraightLineNm, c.StraightLineBearingDeg = &nm, &bearing
	}
	return c, nil
}
