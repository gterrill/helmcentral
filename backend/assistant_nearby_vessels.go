package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// This file implements get_nearby_vessels, the assistant tool (ADR 0128)
// that lets Mate answer questions about other boats - who is around, how
// close, and how long they have been in range or on a mooring - by joining
// three sources this app already keeps for other reasons: the live AIS list
// (signalk.go's fetchSignalKNearbyVesselsLimit), the sighting log
// (nearby_contacts.go, ADR-whatever recorded it) for "in range since", and
// the SignalK History API (signalk.go's fetchSignalKPositionHistory) for a
// vessel's own logged dwell at its current position, when that history
// exists at all.

const (
	// assistantNearbyVesselsDefaultMaxResults and
	// assistantNearbyVesselsMaxMaxResults bound get_nearby_vessels'
	// max_results, the same default/ceiling shape every other assistant
	// tool's paging argument uses (find_places' max_results, etc).
	assistantNearbyVesselsDefaultMaxResults = 10
	assistantNearbyVesselsMaxMaxResults     = 25

	// assistantNearbyVesselsMaxPastSightings caps how many prior encounters
	// ride along in one vessel's past_sightings list - a skipper asking
	// "when have we crossed paths with X" wants the general pattern, not
	// this boat's entire multi-year sighting history in one tool result.
	assistantNearbyVesselsMaxPastSightings = 10

	// assistantStationaryThresholdMeters is how far a vessel's logged
	// position may drift from a reference "current position" and still
	// count as "stationary" when computeStationarySince walks its SignalK
	// position history to find when it last actually moved - wide enough to
	// absorb anchor swing, mooring-buoy movement and GPS noise, narrow
	// enough that a genuine relocation still registers.
	assistantStationaryThresholdMeters = 100.0

	// assistantStationaryConsecutiveOutliers is how many CONSECUTIVE history
	// points must each lie beyond assistantStationaryThresholdMeters from
	// the reference centre before computeStationarySince treats a run as a
	// genuine relocation rather than noise - a single stray GPS fix, or
	// ordinary mooring/anchor swing that happens to oscillate past the
	// threshold and back, never strings this many together (code-review
	// finding, 2026-09-25: the original one-far-point-is-enough rule could
	// not tell a real move apart from either of those).
	assistantStationaryConsecutiveOutliers = 3

	// assistantStationaryCentreWindow bounds how many of the most recent
	// history points are folded into the median "reference position"
	// computeStationarySince measures every point's distance against,
	// instead of the single latest fix - a median of several recent points
	// absorbs one noisy GPS reading landing on the latest fix automatically,
	// where comparing against that single fix directly would not
	// (code-review finding, 2026-09-25).
	assistantStationaryCentreWindow = 5

	// assistantPositionHistoryWindow bounds how far back get_nearby_vessels
	// looks for a vessel's own position history - two weeks is long enough
	// to answer "how long has X been on that mooring" for a typical stay
	// without an unbounded query against the SignalK History API.
	assistantPositionHistoryWindow = 14 * 24 * time.Hour

	// assistantPositionHistoryResolutionSeconds sizes the SignalK History
	// API's aggregation bucket so a 14-day window returns a manageable
	// number of points (up to ~2016 for the full window) rather than
	// per-report resolution.
	assistantPositionHistoryResolutionSeconds = 600
)

// assistantGetNearbyVesselsArgs is get_nearby_vessels' argument shape.
// IncludeHistory is a pointer so "the model left it out" (nil, apply the
// name-dependent default below) is distinguishable from an explicit false.
type assistantGetNearbyVesselsArgs struct {
	Name           string `json:"name"`
	MaxResults     int    `json:"max_results"`
	IncludeHistory *bool  `json:"include_history"`
}

// assistantVesselSighting is one row of a vessel's past_sightings list: a
// prior encounter's start, trimmed from nearbyContactRecord to what the
// model needs (nav_context - our own vessel's state at the time - has no
// use here; past_sightings is entirely about the other vessel).
type assistantVesselSighting struct {
	SeenAt  string  `json:"seen_at"`
	Geoname string  `json:"geoname,omitempty"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}

// assistantNearbyVesselOut is one live AIS target in get_nearby_vessels'
// result, joining the live fix (range, bearing, speed, CPA/TCPA when the
// collision-alarm plugin supplies them) with what the sighting log and the
// SignalK History API know about it.
type assistantNearbyVesselOut struct {
	Name         string   `json:"name"`
	Mmsi         string   `json:"mmsi,omitempty"`
	RangeNm      float64  `json:"range_nm"`
	BearingDeg   int      `json:"bearing_deg"`
	SogKts       *float64 `json:"sog_kts,omitempty"`
	CpaNm        *float64 `json:"cpa_nm,omitempty"`
	TcpaMin      *float64 `json:"tcpa_min,omitempty"`
	PositionAgeS int      `json:"position_age_s"`
	Lat          float64  `json:"lat"`
	Lon          float64  `json:"lon"`

	// InRangeSince is the sighting log's current-encounter start (RFC3339) -
	// absent when the vessel has not yet sat through the sighting log's
	// confirmation dwell, or when there is no sighting log at all. It is a
	// LOWER BOUND on how long the vessel has actually been in range: the
	// sighting log only ever records when a vessel first came within range
	// of US, so a vessel could have arrived earlier and simply not been
	// noticed until then. See the result's own Note.
	InRangeSince           *string                   `json:"in_range_since,omitempty"`
	PreviousSightingsCount int                       `json:"previous_sightings_count"`
	PastSightings          []assistantVesselSighting `json:"past_sightings,omitempty"`

	// StationarySince, HistoryCoversFrom, PositionHistory and
	// PositionHistoryError all come from the vessel's own logged position
	// history (the SignalK History API), a separate and usually more
	// precise figure than InRangeSince above - it lights up once the boat's
	// InfluxDB writer records other vessels, not just self (ADR 0128).
	// Exactly one of StationarySince, PositionHistory ("none recorded", or
	// "under way - not currently stationary" when computeStationarySince
	// finds the vessel's own most recent points are not actually settled -
	// a code-review finding, 2026-09-25) or PositionHistoryError is set,
	// whenever history was requested at all.
	StationarySince      *string `json:"stationary_since,omitempty"`
	HistoryCoversFrom    *string `json:"history_covers_from,omitempty"`
	PositionHistory      string  `json:"position_history,omitempty"`
	PositionHistoryError string  `json:"position_history_error,omitempty"`
}

// assistantVesselNotInRange is one sighting-log-only result: a vessel that
// matched a name filter but has no live AIS target right now, so all
// get_nearby_vessels can answer is "when did we last see it".
type assistantVesselNotInRange struct {
	Name       string  `json:"name"`
	Mmsi       string  `json:"mmsi"`
	LastSeenAt string  `json:"last_seen_at"`
	Geoname    string  `json:"geoname,omitempty"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
}

type assistantGetNearbyVesselsResult struct {
	Vessels    []assistantNearbyVesselOut  `json:"vessels"`
	NotInRange []assistantVesselNotInRange `json:"not_in_range,omitempty"`
	Note       string                      `json:"note"`
	Truncated  bool                        `json:"truncated,omitempty"`
}

// assistantNameOrExactIDMatches reports whether name contains query as a
// case-insensitive substring, or id equals query exactly - an identifier
// like an MMSI is compared verbatim, never case-folded, since it is a
// number rather than a word a skipper might type in any case. An empty
// query always matches (list-everything); an empty id (no identifier to
// compare, e.g. find_places' waypoints) simply never satisfies the exact
// match half.
//
// Shared by every "find this by name or id" filter across the assistant
// tools and the sighting log, so the matching rule itself lives in exactly
// one place: find_places' waypoint search (executeFindPlaces,
// assistant_tools.go - id is always "" there), matchAssistantNearbyVessels
// below (id is the vessel's MMSI), and nearbyContactStore.latestContactsByName
// (nearby_contacts.go - id is vessel_key, itself the MMSI) (code-review
// finding, 2026-09-25).
func assistantNameOrExactIDMatches(name, id, query string) bool {
	if query == "" {
		return true
	}
	if strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
		return true
	}
	return id != "" && id == query
}

// matchAssistantNearbyVessels filters live to the vessels matching nameQuery
// (case-insensitive substring on name, or an exact MMSI) - every vessel when
// nameQuery is empty. Shared by executeGetNearbyVessels' normal, capped
// search and its unlimited beyond-the-cap search.
func matchAssistantNearbyVessels(live []nearbyVessel, nameQuery string) []nearbyVessel {
	matched := make([]nearbyVessel, 0, len(live))
	for _, v := range live {
		if assistantNameOrExactIDMatches(v.Name, v.Mmsi, nameQuery) {
			matched = append(matched, v)
		}
	}
	return matched
}

// executeGetNearbyVessels answers get_nearby_vessels: the live AIS list
// (fetchSignalKNearbyVesselsLimit via d.nearbyVessels), joined with the
// sighting log (d.contacts) for in_range_since/past_sightings and the
// SignalK History API (d.signalKPositionHistory) for stationary_since. A
// name filter that matches no live target falls back to the sighting log
// alone, under not_in_range, so "when did we last see X" works for a boat
// that has left.
func (d assistantToolDeps) executeGetNearbyVessels(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var args assistantGetNearbyVesselsArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse get_nearby_vessels arguments: %w", err)
	}

	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = assistantNearbyVesselsDefaultMaxResults
	}
	if maxResults > assistantNearbyVesselsMaxMaxResults {
		maxResults = assistantNearbyVesselsMaxMaxResults
	}

	nameQuery := strings.TrimSpace(args.Name)
	// include_history defaults to true only when a name filter narrows the
	// result to (in practice) one or a few vessels - fetching each one's
	// full sighting history and querying the SignalK History API for every
	// one of up to 25 vessels on a bare "who's nearby" question would be
	// wasteful and mostly unwanted.
	includeHistory := nameQuery != ""
	if args.IncludeHistory != nil {
		includeHistory = *args.IncludeHistory
	}

	state, err := d.vesselState()
	if err != nil {
		return "", fmt.Errorf("get_nearby_vessels: %w", err)
	}
	if !hasUsableVesselPosition(state.Latitude, state.Longitude) {
		return "", fmt.Errorf("get_nearby_vessels: this vessel's own position is not currently known")
	}

	if d.nearbyVessels == nil {
		return "", fmt.Errorf("get_nearby_vessels: nearby vessel tracking is not available")
	}
	// A name/MMSI filter naming one specific vessel fetches with no cap at
	// all (nearbyVesselsUnlimited, signalk.go) rather than the ordinary
	// listing's assistantNearbyVesselsMaxMaxResults: a crowded anchorage or
	// marina with more AIS targets in range than that ceiling would
	// otherwise send a genuinely-live vessel through the not_in_range/
	// sighting-log path below, reporting a stale last-seen date for a boat
	// that is right there (code-review finding, 2026-09-25). A bare,
	// unfiltered listing keeps the ordinary cap - it never needs more than
	// max_results vessels in the first place.
	fetchLimit := assistantNearbyVesselsMaxMaxResults
	if nameQuery != "" {
		fetchLimit = nearbyVesselsUnlimited
	}
	live, err := d.nearbyVessels(state.Latitude, state.Longitude, d.now(), fetchLimit)
	if err != nil {
		return "", fmt.Errorf("get_nearby_vessels: %w", err)
	}

	matched := matchAssistantNearbyVessels(live, nameQuery)
	matchedCount := len(matched)
	if len(matched) > maxResults {
		matched = matched[:maxResults]
	}

	var store *nearbyContactStore
	if d.contacts != nil {
		store = d.contacts()
	}

	vessels := make([]assistantNearbyVesselOut, 0, len(matched))
	for _, v := range matched {
		out := assistantNearbyVesselOut{
			Name:         v.Name,
			Mmsi:         v.Mmsi,
			RangeNm:      roundTo1(v.RangeM / metersPerNauticalMile),
			BearingDeg:   int(math.Round(bearingDeg(state.Latitude, state.Longitude, v.Lat, v.Lon))),
			SogKts:       v.SogKnots,
			PositionAgeS: v.AgeSeconds,
			Lat:          v.Lat,
			Lon:          v.Lon,
		}
		if v.CpaM != nil {
			nm := roundTo2(*v.CpaM / metersPerNauticalMile)
			out.CpaNm = &nm
		}
		if v.TcpaSeconds != nil {
			minutes := roundTo1(*v.TcpaSeconds / 60)
			out.TcpaMin = &minutes
		}

		if store != nil {
			if key, ok := vesselContactKey(v.Mmsi); ok {
				if err := fillAssistantSightingHistory(store, key, v.Name, includeHistory, &out); err != nil {
					return "", fmt.Errorf("get_nearby_vessels: %w", err)
				}
			}
		}

		vessels = append(vessels, out)
	}

	// The SignalK History API lookup is a real HTTP round trip per vessel -
	// unlike past_sightings above, one cheap local SQLite read - so it only
	// ever runs once a name filter has actually narrowed the match set to
	// the vessel(s) in question, regardless of includeHistory: nothing stops
	// the model passing include_history:true on a bare, unfiltered listing,
	// and fanning that out across up to assistantNearbyVesselsMaxMaxResults
	// vessels would materially delay the whole tool call for a question that
	// never asked for it (code-review finding, 2026-09-25). Run concurrently
	// - one goroutine per matched vessel, each writing to its own distinct
	// slice index - rather than sequentially: the set is already bounded by
	// maxResults (at most assistantNearbyVesselsMaxMaxResults), so total
	// latency is that of the slowest single request instead of their sum
	// (a second code-review finding, same date).
	if includeHistory && nameQuery != "" && d.signalKPositionHistory != nil {
		var wg sync.WaitGroup
		for i := range vessels {
			if vessels[i].Mmsi == "" {
				continue
			}
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				fillAssistantPositionHistory(d, vessels[i].Mmsi, &vessels[i])
			}(i)
		}
		wg.Wait()
	}

	result := assistantGetNearbyVesselsResult{
		Vessels: vessels,
		Note: "in_range_since is when a vessel first came within 5km of us, a lower bound on how long it has " +
			"actually been there - it may have arrived earlier and simply not been noticed until then. " +
			"stationary_since, when present, comes from that vessel's own logged position history and is a " +
			"separate, usually more precise figure.",
	}
	// includeHistory's position-history half (stationary_since) only ever
	// runs with a name filter (see the gate above) - a model that explicitly
	// asked for it on a bare, unfiltered listing gets told why it's missing
	// rather than a silently absent field that looks identical to "checked,
	// nothing found" (code-review finding, 2026-09-25).
	if includeHistory && nameQuery == "" {
		result.Note += " position history (stationary_since) only runs for a specific vessel - give name to also see how long it has been sitting at its current position."
	}

	// matchedCount > maxResults means the max_results cap (not the earlier
	// fetch cap - see fetchLimit above) cut the list; that must be visible,
	// not a silent truncation indistinguishable from "this is everyone in
	// range" (a code-review finding, 2026-09-25).
	if matchedCount > maxResults {
		result.Truncated = true
		result.Note += fmt.Sprintf(" %d vessels matched; showing the nearest %d. Raise max_results (up to %d) to see more.", matchedCount, maxResults, assistantNearbyVesselsMaxMaxResults)
	}

	if nameQuery != "" && len(vessels) == 0 {
		notInRange, err := assistantSearchNotInRange(d, nameQuery, maxResults)
		if err != nil {
			return "", fmt.Errorf("get_nearby_vessels: %w", err)
		}
		result.NotInRange = notInRange
	}

	shrink := func() bool {
		// Drop the least essential thing first: one vessel's past_sightings,
		// then that vessel entirely, working from the end of the list -
		// preserves the closest (most likely relevant) vessels the longest.
		for i := len(result.Vessels) - 1; i >= 0; i-- {
			if len(result.Vessels[i].PastSightings) > 0 {
				result.Vessels[i].PastSightings = nil
				result.Truncated = true
				return true
			}
		}
		if len(result.Vessels) > 0 {
			result.Vessels = result.Vessels[:len(result.Vessels)-1]
			result.Truncated = true
			return true
		}
		if len(result.NotInRange) > 0 {
			result.NotInRange = result.NotInRange[:len(result.NotInRange)-1]
			result.Truncated = true
			return true
		}
		return false
	}
	return capToolResultJSON(&result, shrink)
}

// isPlaceholderVesselContactName reports whether name is not a genuine AIS
// static-data name at all: empty, compactVesselID's own "UNKNOWN" fallback
// (signalk.go, an empty vesselID), or the vessel_key itself - what
// compactVesselID returns for an ordinary vesselID before static data has
// arrived (the trailing "urn:mrn:imo:mmsi:<mmsi>" segment, which IS the
// MMSI/vesselKey get_nearby_vessels already keys the sighting log on).
// fillAssistantSightingHistory's own doc comment explains why a placeholder
// on either side of its name comparison must never count as a mismatch.
func isPlaceholderVesselContactName(name, vesselKey string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	if strings.EqualFold(name, "UNKNOWN") {
		return true
	}
	return strings.EqualFold(name, vesselKey)
}

// fillAssistantSightingHistory populates out's in_range_since,
// previous_sightings_count and (when includeHistory) past_sightings from the
// sighting log. A pending (unconfirmed) candidate encounter never has
// in_range_since set to the previous encounter's start - see isPending's own
// doc comment for why that distinction matters.
func fillAssistantSightingHistory(store *nearbyContactStore, vesselKey, liveName string, includeHistory bool, out *assistantNearbyVesselOut) error {
	sightings, err := store.listSightings(vesselKey)
	if err != nil {
		return fmt.Errorf("read sighting history for %s: %w", vesselKey, err)
	}
	if len(sightings) == 0 {
		return nil
	}

	// Guard against two live vessels sharing the same MMSI - a data-quality
	// fault upstream (a malformed or spoofed AIS static report), not
	// something this store can prevent on its own: the sighting log's most
	// recently recorded name for vesselKey must match the live vessel's own
	// current name before its history is trusted as this specific vessel's.
	// A mismatch means vesselKey's recorded history more plausibly belongs
	// to a different vessel that happens to report the same MMSI - silently
	// attaching it here would cross the two vessels' identities
	// (code-review finding, 2026-09-25). No error: this vessel simply gets
	// no sighting-log enrichment, the same as a vessel with no rows at all.
	//
	// A name that is itself a placeholder (empty, or the MMSI/compact id
	// recordNearbyVesselContacts falls back to - compactVesselID, signalk.go
	// - when AIS static data has not arrived yet) never counts as a
	// mismatch on EITHER side, even against a genuine name: this is the
	// NORMAL case, not the fault the guard exists for. The sighting log's
	// confirmation dwell (nearby_contacts.go) is 5 minutes; a vessel's name
	// often has not been broadcast yet the moment an encounter is confirmed
	// and gets its row's Name frozen at insert time, so the row is
	// legitimately still "this same vessel" once its real name later
	// arrives - both vessels already matched on the one identity this store
	// actually keys on, the MMSI (a code-review finding, 2026-09-25: the
	// original guard discarded a returning vessel's entire sighting history
	// on exactly this ordinary sequencing, not just the rare true collision
	// it was meant to catch).
	if !isPlaceholderVesselContactName(sightings[0].Name, vesselKey) && !isPlaceholderVesselContactName(liveName, vesselKey) &&
		!strings.EqualFold(sightings[0].Name, liveName) {
		return nil
	}

	pending := store.isPending(vesselKey)
	priorSightings := sightings
	if !pending {
		since := sightings[0].SeenAt.UTC().Format(time.RFC3339)
		out.InRangeSince = &since
		priorSightings = sightings[1:]
	}
	out.PreviousSightingsCount = len(priorSightings)

	if includeHistory {
		for i, s := range priorSightings {
			if i >= assistantNearbyVesselsMaxPastSightings {
				break
			}
			out.PastSightings = append(out.PastSightings, assistantVesselSighting{
				SeenAt:  s.SeenAt.UTC().Format(time.RFC3339),
				Geoname: s.Geoname,
				Lat:     s.Lat,
				Lon:     s.Lon,
			})
		}
	}
	return nil
}

// fillAssistantPositionHistory populates out's stationary_since (or
// position_history/position_history_error) from the SignalK History API,
// over the last assistantPositionHistoryWindow. A request failure is
// reported per-vessel rather than failing the whole tool call - one
// vessel's history being unavailable says nothing about any other vessel's
// (AGENTS.md's fallback policy: surface the failure explicitly, but scoped
// to what actually failed).
func fillAssistantPositionHistory(d assistantToolDeps, mmsi string, out *assistantNearbyVesselOut) {
	now := d.now()
	from := now.Add(-assistantPositionHistoryWindow)

	points, err := d.signalKPositionHistory(mmsi, from, now, assistantPositionHistoryResolutionSeconds)
	if err != nil {
		out.PositionHistoryError = firstErrorLine(err)
		return
	}
	if len(points) == 0 {
		out.PositionHistory = "none recorded"
		return
	}

	sinceAt, stationary := computeStationarySince(points)
	if !stationary {
		// A code-review finding, 2026-09-25: reporting stationary_since at
		// all here requires the vessel to actually still be near that
		// position - see computeStationarySince's own doc comment for why a
		// vessel genuinely under way can otherwise get a false, recent
		// stationary_since.
		out.PositionHistory = "under way - not currently stationary"
		return
	}
	since := sinceAt.UTC().Format(time.RFC3339)
	out.StationarySince = &since
	coversFrom := from.UTC().Format(time.RFC3339)
	out.HistoryCoversFrom = &coversFrom
}

// computeStationarySince walks points (in any order - they are sorted here
// first) and returns the timestamp after which the vessel has stayed within
// assistantStationaryThresholdMeters of a robust "current position"
// estimate: the median lat/lon of the most recent assistantStationaryCentreWindow
// points, not the single latest fix. A run of points farther than the
// threshold only counts as a genuine relocation once at least
// assistantStationaryConsecutiveOutliers of them occur consecutively; a
// shorter run is treated as noise (a single bad GPS fix, or mooring/anchor
// swing that happens to oscillate past the threshold and back) and ignored.
// since is only meaningful when stationary is true; when it is false, the
// caller must not report a stationary_since at all (see
// fillAssistantPositionHistory).
//
// The two robustness measures in the walk below (median centre, consecutive-
// outlier run length) fixed a code-review finding, 2026-09-25: the previous
// version measured every point against the single latest fix and treated
// any one point beyond the threshold as a confirmed move, so a single noisy
// GPS reading on the latest fix reported "just arrived," and ordinary
// mooring swing between two points genuinely more than the threshold apart
// reported a false recent relocation.
//
// The recentFar check just below the median calculation fixes a SEPARATE
// code-review finding (2026-09-25) the walk above did not catch: it took
// the median of the last assistantStationaryCentreWindow points as "current
// position" without ever confirming the vessel is STILL there. For a
// vessel underway in a roughly straight line, the median of an odd-length
// window landing almost exactly on that window's own middle sample is a
// mathematical inevitability, not a real arrival - the walk above would
// then treat that single coincidental match as the moment a still-moving
// vessel "settled," even though it is moving away again on every sample
// before and after. Requiring that at most one of the CENTRE WINDOW'S OWN
// most recent assistantStationaryConsecutiveOutliers points be far from
// that median - the same one-off tolerance the walk already extends to
// noise - catches this: a vessel genuinely still near a stable position has
// at most one recent outlier, while one still moving has most of its most
// recent points far from any single reference.
func computeStationarySince(points []signalKHistoryPoint) (since time.Time, stationary bool) {
	sorted := make([]signalKHistoryPoint, len(points))
	copy(sorted, points)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })

	windowStart := len(sorted) - assistantStationaryCentreWindow
	if windowStart < 0 {
		windowStart = 0
	}
	centreLat, centreLon := medianLatLon(sorted[windowStart:])

	recentWindowSize := assistantStationaryConsecutiveOutliers
	if recentWindowSize > len(sorted) {
		recentWindowSize = len(sorted)
	}
	recentFar := 0
	for _, p := range sorted[len(sorted)-recentWindowSize:] {
		if haversineMeters(p.Lat, p.Lon, centreLat, centreLon) > assistantStationaryThresholdMeters {
			recentFar++
		}
	}
	if recentFar*2 > recentWindowSize {
		return time.Time{}, false
	}

	stationarySince := sorted[0].Time
	consecutiveFar := 0
	for _, p := range sorted {
		if haversineMeters(p.Lat, p.Lon, centreLat, centreLon) > assistantStationaryThresholdMeters {
			consecutiveFar++
			continue
		}
		if consecutiveFar >= assistantStationaryConsecutiveOutliers {
			// p is the first point after a qualifying run of far points -
			// the moment the vessel actually settled here.
			stationarySince = p.Time
		}
		consecutiveFar = 0
	}
	return stationarySince, true
}

// medianLatLon returns the per-axis median latitude and longitude across
// points - a simple, robust "centre" estimate that ignores a minority of
// outlier points the way a mean would not, without needing genuine
// geographic centroid math for the small local distances this is used at.
func medianLatLon(points []signalKHistoryPoint) (lat, lon float64) {
	lats := make([]float64, len(points))
	lons := make([]float64, len(points))
	for i, p := range points {
		lats[i] = p.Lat
		lons[i] = p.Lon
	}
	sort.Float64s(lats)
	sort.Float64s(lons)
	return medianOfSorted(lats), medianOfSorted(lons)
}

// medianOfSorted returns the median of an already-ascending-sorted slice.
func medianOfSorted(sorted []float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// assistantSearchNotInRange answers "when did we last see X" for a name
// query that matched no live AIS target: every distinct vessel in the
// sighting log whose most recently recorded name contains the query.
func assistantSearchNotInRange(d assistantToolDeps, nameQuery string, limit int) ([]assistantVesselNotInRange, error) {
	if d.contacts == nil {
		return nil, nil
	}
	store := d.contacts()
	if store == nil {
		return nil, nil
	}

	latest, err := store.latestContactsByName(nameQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("search sighting log for %q: %w", nameQuery, err)
	}

	out := make([]assistantVesselNotInRange, 0, len(latest))
	for _, l := range latest {
		out = append(out, assistantVesselNotInRange{
			Name:       l.Name,
			Mmsi:       l.VesselKey,
			LastSeenAt: l.SeenAt.UTC().Format(time.RFC3339),
			Geoname:    l.Geoname,
			Lat:        l.Lat,
			Lon:        l.Lon,
		})
	}
	return out, nil
}
