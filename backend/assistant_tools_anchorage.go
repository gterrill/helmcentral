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

var mateToolAnchorageDetails = mateTool{
	name: "anchorage_details",
	function: openRouterFunctionDef{
		Description: "Assess an anchorage for a stay, from one call: the shelter it has from each wind direction (the charted OpenStreetMap " +
			"shoreline held on board as the Offline coastline, with the fetch for every 10 degrees up to 15 nm), the wind and wave forecast " +
			"over the stay with the shelter grade of each row, the tide at arrival and the high and low waters through the stay with their " +
			"range, and what OpenStreetMap records within a nautical mile: restricted areas and marine protected areas, public moorings " +
			"(with the vessel size limit and class where tagged), anchorage areas, seabed areas and the bay. Use it whenever you choose, " +
			"compare or assess an anchorage; resolve a name with find_places first and pass its lat and lon. Pass arrive as an RFC 3339 time " +
			"(for example 2026-11-02T15:00:00+10:00) and leave as the time the boat leaves; without leave the stay ends at the first 08:00 local time " +
			"at least six hours after arrival, and the result says so. The stay may be up to seven days. " + ruleAnchorageDetails + " Shelter must come " +
			"from this tool: if it returns an error (turned off, outside coverage, still downloading), tell the operator what it says " +
			"rather than guessing the shelter. With no depth known the swing and clearance checks are skipped.",
		Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"lat": {
							"type": "number",
							"description": "Latitude of the anchorage in decimal degrees (south negative), from find_places."
						},
						"lon": {
							"type": "number",
							"description": "Longitude of the anchorage in decimal degrees (west negative), from find_places."
						},
						"arrive": {
							"type": "string",
							"description": "When the boat arrives, an RFC 3339 time with its offset."
						},
						"leave": {
							"type": "string",
							"description": "When the boat leaves, an RFC 3339 time after arrive. Optional: default the first 08:00 local time at least six hours after arrive."
						},
						"name": {"type": "string", "description": "Optional name of the anchorage, for display only."},
						"depth_m": {"type": "number", "description": "Water depth at the anchorage in metres at chart datum, ONLY when the operator stated it in this conversation. Omit otherwise: never from your own knowledge."}
					},
					"required": ["lat", "lon", "arrive"]
				}`),
	},
	execute: plainTool(assistantToolDeps.executeAnchorageDetails),
	describe: func(args json.RawMessage) string {
		return fmt.Sprintf("Assessing the anchorage at %s…", assistantLocationLabel(args))
	},
}

type assistantAnchorageDetailsArgs struct {
	Lat    *float64 `json:"lat"`
	Lon    *float64 `json:"lon"`
	Arrive string   `json:"arrive"`
	Leave  string   `json:"leave"`
	Name   string   `json:"name"`
	DepthM *float64 `json:"depth_m"`
}

const (
	// anchorageMaxStay bounds the stay window: the forecast reaches ten days,
	// and a longer stay is several anchorages' worth of questions.
	anchorageMaxStay = 7 * 24 * time.Hour
	// anchorageMinDefaultStay is the shortest stay a default leave gives: the
	// first 08:00 at least this long after arrival.
	anchorageMinDefaultStay = 6 * time.Hour
	// anchorageDefaultLeaveHour is when the stay ends when the caller gives no
	// leave time: the boat's morning, local to the anchorage.
	anchorageDefaultLeaveHour = 8
	// anchorageNearestMoorings is how many moorings the model's copy lists,
	// nearest first; the card lists all of them.
	anchorageNearestMoorings = 10
)

// ── the sections ─────────────────────────────────────────────────────────
//
// Each of forecast, tide and features is a section that either holds its data
// or says why not. The body is an embedded pointer so a section with an error
// carries nothing else: no empty list that could be read as "nothing there".

type anchorageForecastSection struct {
	Error string `json:"error,omitempty"`
	*AnchorageForecastBody
}

type AnchorageForecastBody struct {
	Timezone          string                `json:"timezone"`
	WeatherProvider   string                `json:"weather_provider"`
	WaveProvider      string                `json:"wave_provider"`
	WavesError        string                `json:"waves_error,omitempty"`
	WavesNoData       bool                  `json:"waves_no_data,omitempty"`
	WaveFallbackModel string                `json:"wave_fallback_model,omitempty"`
	Note              string                `json:"note,omitempty"`
	Days              []assistantDaySummary `json:"days"`
	Hourly            []assistantWindHour   `json:"hourly"`
	Truncated         bool                  `json:"truncated,omitempty"`
}

type anchorageTideExtreme struct {
	assistantTideExtreme
	// InWindow is false for the extremes either side of the stay that were
	// added so a stay with fewer than two still has a range.
	InWindow bool `json:"in_window"`
}

type anchorageTideHeight struct {
	Time      string  `json:"time"`
	HeightM   float64 `json:"height_m"`
	Direction string  `json:"direction"`
}

type anchorageTideSection struct {
	Error string `json:"error,omitempty"`
	*AnchorageTideBody
}

type AnchorageTideBody struct {
	Reading   string               `json:"reading"`
	Station   assistantStationInfo `json:"station"`
	Provider  string               `json:"provider"`
	TimeBasis string               `json:"time_basis"`
	// AtArrival is nil, with HeightNote saying why, when arrival is outside
	// the predicted extremes: the height is never read off the nearest one.
	AtArrival  *anchorageTideHeight `json:"at_arrival"`
	HeightNote string               `json:"height_note,omitempty"`
	// Note is set when the prediction ends before the stay does.
	Note      string                 `json:"note,omitempty"`
	Extremes  []anchorageTideExtreme `json:"extremes"`
	RangeM    *float64               `json:"range_m"`
	RangeNote string                 `json:"range_note"`
	Truncated bool                   `json:"truncated,omitempty"`
}

// anchorageTideOut is the tide section with the sorted predictions behind it,
// from which the depth checks find the lowest and highest water in the stay.
type anchorageTideOut struct {
	section anchorageTideSection
	sorted  []tideExtremePoint
}

type anchorageMooringSize struct {
	VesselSizeM float64 `json:"vessel_size_m"`
	Count       int     `json:"count"`
}

type anchorageMooringSummary struct {
	Count int `json:"count"`
	// VesselSizes counts the moorings by size limit, smallest first.
	VesselSizes   []anchorageMooringSize   `json:"vessel_sizes"`
	UntaggedCount int                      `json:"details_untagged_count"`
	Nearest       []anchorageMooringRecord `json:"nearest"`
}

type anchorageFeaturesSection struct {
	Error string `json:"error,omitempty"`
	*AnchorageFeaturesBody
}

type AnchorageFeaturesBody struct {
	Source          string                    `json:"source"`
	RadiusNm        int                       `json:"radius_nm"`
	RestrictedAreas []anchorageRestrictedArea `json:"restricted_areas"`
	Moorings        anchorageMooringSummary   `json:"moorings"`
	AnchorageAreas  []anchorageAreaRecord     `json:"anchorage_areas"`
	SeabedAreas     []anchorageSeabedRecord   `json:"seabed_areas"`
	Bay             *anchorageBayRecord       `json:"bay"`
	DataAsOf        string                    `json:"data_as_of,omitempty"`
	Truncated       bool                      `json:"truncated,omitempty"`
	// Cached is set when the host reused an earlier lookup of this position;
	// DataAsOf is still Overpass's own time.
	Cached   bool   `json:"cached,omitempty"`
	CachedAt string `json:"cached_at,omitempty"`
	Reading  string `json:"reading"`
}

type assistantAnchorageDetailsResult struct {
	Name           string  `json:"name,omitempty"`
	Lat            float64 `json:"lat"`
	Lon            float64 `json:"lon"`
	Arrive         string  `json:"arrive"`
	Leave          string  `json:"leave"`
	LeaveDefaulted bool    `json:"leave_defaulted,omitempty"`
	Timezone       string  `json:"timezone"`
	// Shelter is required: when it cannot be made the whole call fails.
	Shelter  *shelterResult           `json:"shelter"`
	Forecast anchorageForecastSection `json:"forecast"`
	Tide     anchorageTideSection     `json:"tide"`
	Features anchorageFeaturesSection `json:"features"`
	// Depth is the depth used and what it means for the stay (anchorage_depth.go).
	Depth anchorageDepthSection `json:"depth"`
	// Card is set only where a card sink takes the anchorage card (Mate's
	// chat), so an outside assistant is not told to rely on a card it never
	// shows.
	Card    string `json:"card,omitempty"`
	Reading string `json:"reading"`
}

// ── the stay window ──────────────────────────────────────────────────────

type anchorageStay struct {
	arrive, leave  time.Time
	leaveDefaulted bool
	zone           placeZone
}

// parseAnchorageStay validates the window. The default leave is the first
// 08:00 at least six hours after arrival, in the time zone at the anchorage (the same lat/lon
// zone the forecast and the passage tools use).
func parseAnchorageStay(args assistantAnchorageDetailsArgs) (anchorageStay, error) {
	var stay anchorageStay
	if strings.TrimSpace(args.Arrive) == "" {
		return stay, fmt.Errorf("anchorage_details: arrive is required, an RFC 3339 time such as 2026-11-02T15:00:00+10:00")
	}
	arrive, err := time.Parse(time.RFC3339, strings.TrimSpace(args.Arrive))
	if err != nil {
		return stay, fmt.Errorf("anchorage_details: arrive %q is not an RFC 3339 time such as 2026-11-02T15:00:00+10:00", args.Arrive)
	}
	zone, err := zoneAt(*args.Lat, *args.Lon)
	if err != nil {
		return stay, fmt.Errorf("anchorage_details: %w", err)
	}
	stay.arrive, stay.zone = arrive, zone
	if strings.TrimSpace(args.Leave) == "" {
		local := arrive.In(zone.Loc)
		stay.leave = time.Date(local.Year(), local.Month(), local.Day(), anchorageDefaultLeaveHour, 0, 0, 0, zone.Loc)
		if stay.leave.Sub(arrive) < anchorageMinDefaultStay {
			stay.leave = time.Date(local.Year(), local.Month(), local.Day()+1, anchorageDefaultLeaveHour, 0, 0, 0, zone.Loc)
		}
		stay.leaveDefaulted = true
	} else {
		leave, err := time.Parse(time.RFC3339, strings.TrimSpace(args.Leave))
		if err != nil {
			return stay, fmt.Errorf("anchorage_details: leave %q is not an RFC 3339 time such as 2026-11-03T08:00:00+10:00", args.Leave)
		}
		stay.leave = leave
	}
	if !stay.leave.After(stay.arrive) {
		return stay, fmt.Errorf("anchorage_details: leave %s is not after arrive %s", stay.leave.Format(time.RFC3339), stay.arrive.Format(time.RFC3339))
	}
	if stay.leave.Sub(stay.arrive) > anchorageMaxStay {
		return stay, fmt.Errorf("anchorage_details: the stay is longer than seven days; ask about a shorter one")
	}
	return stay, nil
}

// ── execute ──────────────────────────────────────────────────────────────

// anchorageParts is everything the sections found, before the model's copy is
// shrunk. The card is built from it.
type anchorageParts struct {
	stay     anchorageStay
	name     string
	shelter  *shelterResult
	forecast anchorageForecastOut
	tide     anchorageTideSection
	features anchorageFeaturesOut
	depth    anchorageDepthSection
}

func (d assistantToolDeps) executeAnchorageDetails(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var args assistantAnchorageDetailsArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse anchorage_details arguments: %w", err)
	}
	if args.Lat == nil || args.Lon == nil {
		return "", fmt.Errorf("anchorage_details: lat and lon are required")
	}
	if !hasUsableVesselPosition(*args.Lat, *args.Lon) {
		return "", fmt.Errorf("anchorage_details: %g, %g is not a usable position", *args.Lat, *args.Lon)
	}
	stay, err := parseAnchorageStay(args)
	if err != nil {
		return "", err
	}
	if args.DepthM != nil && !validDatumDepthM(*args.DepthM) {
		return "", fmt.Errorf("anchorage_details: depth_m %g is not a depth in metres at chart datum between %g and %g", *args.DepthM, minSavedPlaceDepthM, maxSavedPlaceDepthM)
	}

	// Shelter is the one required section: an anchorage judged without it is a
	// guess, so its failure is the call's. It is local and quick, so it is
	// settled before any network lookup starts.
	if d.offlineCoastline == nil {
		return "", fmt.Errorf("anchorage_details: Offline coastline is not available on this install")
	}
	lat, lon := *args.Lat, *args.Lon
	shelter, err := d.shelterAt(lat, lon)
	if err != nil {
		return "", fmt.Errorf("anchorage_details: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// The four lookups run together. Each sends its one result on a buffered
	// channel, so a lookup that outlives a cancelled call finishes in the
	// background and writes nothing the caller can see.
	tideCh := make(chan anchorageTideOut, 1)
	featuresCh := make(chan anchorageFeaturesOut, 1)
	forecastCh := make(chan anchorageForecastOut, 1)
	depthCh := make(chan anchorageDepthSection, 1)
	go func() { tideCh <- d.anchorageTide(lat, lon, stay) }()
	go func() { featuresCh <- d.anchorageFeatures(lat, lon) }()
	go func() { forecastCh <- d.anchorageForecast(lat, lon, stay, shelter) }()
	go func() { depthCh <- d.resolveAnchorageDepth(lat, lon, args.DepthM) }()
	var tide anchorageTideOut
	var features anchorageFeaturesOut
	var forecast anchorageForecastOut
	var resolved anchorageDepthSection
	for got := 0; got < 4; got++ {
		select {
		case tide = <-tideCh:
		case features = <-featuresCh:
		case forecast = <-forecastCh:
		case resolved = <-depthCh:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	depth := d.anchorageDepthChecks(resolved, tide, forecast, stay)
	parts := anchorageParts{
		stay: stay, name: strings.TrimSpace(args.Name), shelter: shelter,
		forecast: forecast, tide: tide.section, features: features, depth: depth,
	}
	result := assistantAnchorageDetailsResult{
		Name: parts.name, Lat: lat, Lon: lon,
		Arrive:         stay.arrive.In(stay.zone.Loc).Format(time.RFC3339),
		Leave:          stay.leave.In(stay.zone.Loc).Format(time.RFC3339),
		LeaveDefaulted: stay.leaveDefaulted,
		Timezone:       stay.zone.Name,
		Shelter:        shelter,
		Forecast:       forecast.section,
		Tide:           tide.section,
		Features:       features.section,
		Depth:          depth,
		Reading:        readingAnchorageDetails(stay.leaveDefaulted, forecast.section.Error, tide.section.Error, features.section.Error, depth),
	}

	// The card is built from the full data before the model's copy is shrunk
	// (ADR 0242): the model's text is capped, and the operator's card is not.
	var card json.RawMessage
	if d.cards != nil {
		result.Card = anchorageCardInstruction
		if card, err = json.Marshal(buildAnchorageCard(parts)); err != nil {
			return "", fmt.Errorf("anchorage_details card: %w", err)
		}
	}

	// shrink drops the least useful detail first: the moorings listed by name,
	// the later tide extremes, the later forecast rows, then the fetch table
	// (the arcs and the summary stay). The first cut says so.
	shrink := func() bool {
		switch {
		case result.Features.AnchorageFeaturesBody != nil && len(result.Features.Moorings.Nearest) > 0:
			m := &result.Features.Moorings
			m.Nearest = m.Nearest[:len(m.Nearest)-1]
			result.Features.Truncated = true
		case result.Tide.AnchorageTideBody != nil && len(result.Tide.Extremes) > 2:
			result.Tide.Extremes = result.Tide.Extremes[:len(result.Tide.Extremes)-1]
			result.Tide.Truncated = true
		case result.Forecast.AnchorageForecastBody != nil && len(result.Forecast.Hourly) > 0:
			// Cut the rows that bring the result under the cap in one step,
			// from their encoded sizes, not one re-encoding of the result each.
			encoded, err := json.Marshal(&result)
			if err != nil {
				return false
			}
			result.Forecast.Hourly = trimTailToFit(result.Forecast.Hourly, len(encoded)-assistantMaxToolResultChars)
			result.Forecast.Truncated = true
		case len(result.Shelter.Bearings) > 0:
			// A copy: the card was built from the full estimate.
			trimmed := *result.Shelter
			trimmed.Bearings = nil
			result.Shelter = &trimmed
		default:
			return false
		}
		return true
	}
	text, err := capToolResultJSON(&result, shrink)
	if err != nil {
		return "", err
	}
	if d.cards != nil {
		d.cards.put(text, mateCard{
			Kind: anchorageCardKind, Payload: card,
			lat: shelter.Lat, lon: shelter.Lon, positioned: true,
		})
	}
	return text, nil
}

// ── forecast ─────────────────────────────────────────────────────────────

type anchorageForecastOut struct {
	section anchorageForecastSection
	// at is the instant of each row in section.Hourly, which the card stores
	// in full.
	at        []time.Time
	fetchedAt time.Time
	// stayMaxKts is the strongest wind or gust, in knots, in any hourly
	// forecast row from arrival to leaving, not only the rows the card keeps.
	// nil when no hour in the stay has a wind. lastHour is the latest hour the
	// forecast holds.
	stayMaxKts *float64
	lastHour   time.Time
}

// stayStrongestWindKts is the strongest rounded gust or wind among the hourly
// points inside the stay, rounded as the forecast rows are.
func stayStrongestWindKts(hours []weatherHourPoint, stay anchorageStay) *float64 {
	var best *float64
	for _, h := range hours {
		if h.Time.IsZero() || h.Time.Before(stay.arrive) || h.Time.After(stay.leave) {
			continue
		}
		for _, ms := range []float64{h.WindGustMS, h.WindSpeedMS} {
			if ms == 0 {
				continue
			}
			kts := math.Round(ms * metersPerSecondToKnots)
			if best == nil || kts > *best {
				best = &kts
			}
		}
	}
	return best
}

func (d assistantToolDeps) anchorageForecast(lat, lon float64, stay anchorageStay, shelter *shelterResult) anchorageForecastOut {
	fail := func(format string, args ...any) anchorageForecastOut {
		return anchorageForecastOut{section: anchorageForecastSection{Error: "Forecast unavailable: " + fmt.Sprintf(format, args...)}}
	}
	if d.weather == nil || d.now == nil {
		return fail("no weather provider is available on this install")
	}
	provider, providerID, err := d.weather()
	if err != nil {
		return fail("%v", err)
	}
	// get_wind_forecast takes a number of days from today; fetch enough to
	// cover the leave time, then keep only the stay.
	days := clampAssistantDays(int(math.Ceil(stay.leave.Sub(d.now()).Hours()/24))+1, 3, assistantMaxForecastDays)
	// The wave fetch does not need the weather; they run together.
	wavesDone := make(chan waveSeries, 1)
	go func() { wavesDone <- d.fetchWaveSeries(lat, lon, days) }()
	bundle, err := provider.FetchForecast(lat, lon, days, stay.zone.Name)
	if err != nil {
		return fail("weather provider %q: %v", providerID, err)
	}
	waves := <-wavesDone

	picked := anchorageForecastHours(bundle.Hourly, stay)
	win := forecastRows(bundle.Hourly, stay.zone.Loc, time.Time{}, time.Time{}, func(t time.Time) bool { return picked[t.Unix()] }, waves.byUnix, nil, shelter)
	if len(win.hourly) == 0 {
		if win.last.IsZero() {
			return fail("weather provider %q returned no hours", providerID)
		}
		return fail("the forecast does not cover arrival (it runs to %s)", win.last.In(stay.zone.Loc).Format(toolTimeLayout))
	}

	body := &AnchorageForecastBody{
		Timezone: stay.zone.Name, WeatherProvider: providerID, WaveProvider: waves.providerID,
		WavesError: waves.err, WavesNoData: waves.noData, WaveFallbackModel: waves.fallbackModel,
		Days: win.days, Hourly: win.hourly,
	}
	if win.last.Before(stay.leave.Add(-3 * time.Hour)) {
		body.Note = fmt.Sprintf("The forecast ends at %s, before the stay does.", win.last.In(stay.zone.Loc).Format(toolTimeLayout))
	}
	return anchorageForecastOut{
		section: anchorageForecastSection{AnchorageForecastBody: body}, at: win.at, fetchedAt: bundle.CachedAt,
		stayMaxKts: stayStrongestWindKts(bundle.Hourly, stay), lastHour: win.last,
	}
}

// anchorageForecastHours picks the forecast hours the stay is read from: the
// hour nearest arrival (the earlier when two are equally near), every three
// hours after it while inside the stay, and the hour nearest leaving. An hour
// further than half an hour from the time it stands for is not used, so a
// forecast that stops short of arrival or leaving yields nothing for it.
func anchorageForecastHours(hours []weatherHourPoint, stay anchorageStay) map[int64]bool {
	const tolerance = 30 * time.Minute
	nearest := func(target time.Time) (time.Time, bool) {
		var best time.Time
		var bestD time.Duration
		for _, h := range hours {
			if h.Time.IsZero() {
				continue
			}
			d := h.Time.Sub(target)
			if d < 0 {
				d = -d
			}
			if best.IsZero() || d < bestD || (d == bestD && h.Time.Before(best)) {
				best, bestD = h.Time, d
			}
		}
		return best, !best.IsZero() && bestD <= tolerance
	}
	have := map[int64]bool{}
	for _, h := range hours {
		have[h.Time.Unix()] = true
	}
	picked := map[int64]bool{}
	first, ok := nearest(stay.arrive)
	if !ok {
		return picked
	}
	picked[first.Unix()] = true
	for t := first.Add(3 * time.Hour); !t.After(stay.leave); t = t.Add(3 * time.Hour) {
		if have[t.Unix()] {
			picked[t.Unix()] = true
		}
	}
	if last, ok := nearest(stay.leave); ok && !last.Before(first) {
		picked[last.Unix()] = true
	}
	return picked
}

// ── tide ─────────────────────────────────────────────────────────────────

// anchorageTideRangeRule is the rule the range follows. It is stated in the
// result so the model and the card say the same thing.
const (
	anchorageTideRangeInWindow = "Range is the highest minus the lowest high and low water inside the stay."
	anchorageTideRangeBracket  = "Fewer than two high or low waters fall inside the stay, so the range includes the one before arrival and the one after leaving."
	anchorageTideRangeBefore   = "Fewer than two high or low waters fall inside the stay, so the range includes the one before arrival."
	anchorageTideRangeAfter    = "Fewer than two high or low waters fall inside the stay, so the range includes the one after leaving."
)

func (d assistantToolDeps) anchorageTide(lat, lon float64, stay anchorageStay) anchorageTideOut {
	fail := func(err error) anchorageTideOut {
		return anchorageTideOut{section: anchorageTideSection{Error: "Tide unavailable: " + firstErrorLine(err)}}
	}
	if d.tides == nil {
		return fail(errors.New("no tide provider is available on this install"))
	}
	st, err := d.resolveTideStation(lat, lon)
	if err != nil {
		return fail(err)
	}
	loc := st.loc
	sorted := append([]tideExtremePoint(nil), st.chart.Extremes...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })

	var inWindow []tideExtremePoint
	var before, after *tideExtremePoint
	for i := range sorted {
		switch e := sorted[i]; {
		case e.Time.Before(stay.arrive):
			before = &sorted[i] // the last one before arrival
		case !e.Time.After(stay.leave):
			inWindow = append(inWindow, e)
		case after == nil:
			after = &sorted[i] // the first one after leaving
		}
	}
	body := &AnchorageTideBody{
		Reading: readingTides(st.info, st.timeBasis), Station: st.info, Provider: st.providerID, TimeBasis: st.timeBasis,
	}

	if n := len(sorted); n > 0 && sorted[n-1].Time.Before(stay.leave) {
		body.Note = fmt.Sprintf("The tide prediction ends at %s, before the stay does.", sorted[n-1].Time.In(loc).Format(toolTimeLayout))
	}

	// The height at arrival is read between the extremes either side of it,
	// never off the nearest one when arrival falls outside the prediction.
	switch {
	case len(sorted) == 0:
		body.HeightNote = "The provider returned no predicted high or low waters."
	case stay.arrive.Before(sorted[0].Time) || !stay.arrive.Before(sorted[len(sorted)-1].Time):
		body.HeightNote = fmt.Sprintf("Arrival is outside the predicted tides, which run from %s to %s.",
			sorted[0].Time.In(loc).Format(toolTimeLayout), sorted[len(sorted)-1].Time.In(loc).Format(toolTimeLayout))
	default:
		h, dir := interpolateTideNow(sorted, stay.arrive)
		body.AtArrival = &anchorageTideHeight{Time: stay.arrive.In(loc).Format(toolTimeLayout), HeightM: roundTo2(h), Direction: dir}
	}

	// With fewer than two extremes inside the stay there is no range, so the
	// one before arrival and the one after leaving are added.
	bracketed := len(inWindow) < 2
	all := []anchorageTideExtreme{}
	add := func(e *tideExtremePoint, in bool) {
		if e != nil {
			all = append(all, anchorageTideExtreme{assistantTideExtreme: toAssistantTideExtreme(*e, loc), InWindow: in})
		}
	}
	if bracketed {
		add(before, false)
	}
	for i := range inWindow {
		add(&inWindow[i], true)
	}
	if bracketed {
		add(after, false)
	}
	body.Extremes = all

	if len(all) >= 2 {
		lo, hi := all[0].HeightM, all[0].HeightM
		for _, e := range all[1:] {
			lo, hi = math.Min(lo, e.HeightM), math.Max(hi, e.HeightM)
		}
		r := roundTo2(hi - lo)
		body.RangeM = &r
		body.RangeNote = anchorageTideRangeInWindow
		switch {
		case bracketed && before != nil && after != nil:
			body.RangeNote = anchorageTideRangeBracket
		case bracketed && before != nil:
			body.RangeNote = anchorageTideRangeBefore
		case bracketed && after != nil:
			body.RangeNote = anchorageTideRangeAfter
		}
	} else {
		body.RangeNote = "Fewer than two high or low waters are predicted around the stay, so there is no range."
	}
	return anchorageTideOut{section: anchorageTideSection{AnchorageTideBody: body}, sorted: sorted}
}

// ── features ─────────────────────────────────────────────────────────────

type anchorageFeaturesOut struct {
	section anchorageFeaturesSection
	found   anchorageFeatures
}

func (d assistantToolDeps) anchorageFeatures(lat, lon float64) anchorageFeaturesOut {
	fail := func(format string, args ...any) anchorageFeaturesOut {
		return anchorageFeaturesOut{section: anchorageFeaturesSection{Error: "Features unavailable: " + fmt.Sprintf(format, args...)}}
	}
	if d.placeNames == nil {
		return fail("no place-names plugin is available on this install")
	}
	pn, providerID, err := d.placeNames()
	if err != nil {
		return fail("%v", err)
	}
	provider, ok := pn.(anchorageFeaturesProvider)
	if !ok {
		return fail("plugin %q does not provide anchorage features", providerID)
	}
	found, err := provider.AnchorageFeatures(lat, lon)
	if errors.Is(err, errNoAnchorageFeaturesExport) {
		return fail("plugin %q does not provide anchorage features", providerID)
	}
	if err != nil {
		return fail("%v", err)
	}

	body := &AnchorageFeaturesBody{
		Source: "OpenStreetMap via " + providerID, RadiusNm: 1,
		RestrictedAreas: make([]anchorageRestrictedArea, 0, len(found.RestrictedAreas)),
		AnchorageAreas:  make([]anchorageAreaRecord, 0, len(found.AnchorageAreas)),
		SeabedAreas:     make([]anchorageSeabedRecord, 0, len(found.SeabedAreas)),
		Bay:             found.Bay, DataAsOf: found.DataAsOf, Truncated: found.Truncated,
	}
	var prohibited []string
	for _, r := range found.RestrictedAreas {
		body.RestrictedAreas = append(body.RestrictedAreas, r.anchorageRestrictedArea)
		if prohibitsAnchoring(r.anchorageRestrictedArea) {
			prohibited = append(prohibited, restrictedLabel(r.anchorageRestrictedArea))
		}
	}
	for _, a := range found.AnchorageAreas {
		body.AnchorageAreas = append(body.AnchorageAreas, a.anchorageAreaRecord)
	}
	for _, s := range found.SeabedAreas {
		body.SeabedAreas = append(body.SeabedAreas, s.anchorageSeabedRecord)
	}
	var cachedAt time.Time
	if found.Cached {
		cachedAt = found.CachedAt
		body.Cached, body.CachedAt = true, cachedAt.UTC().Format(time.RFC3339)
	}
	body.Moorings = summariseMoorings(found.Moorings)
	body.Reading = readingAnchorageFeatures(prohibited, found.Truncated, cachedAt)
	return anchorageFeaturesOut{section: anchorageFeaturesSection{AnchorageFeaturesBody: body}, found: found}
}

// prohibitsAnchoring says whether a mapped restriction forbids anchoring. The
// S-57 values are anchoring_prohibited and the common mapper spelling
// no_anchoring; "anchoring_restricted" is a restriction, not a prohibition.
func prohibitsAnchoring(r anchorageRestrictedArea) bool {
	if r.Source != "seamark" {
		return false
	}
	for _, v := range strings.Split(strings.ToLower(r.Restriction), ";") {
		switch strings.TrimSpace(v) {
		case "anchoring_prohibited", "no_anchoring":
			return true
		}
	}
	return false
}

func restrictedLabel(r anchorageRestrictedArea) string {
	if r.Name != "" {
		return r.Name
	}
	return r.OSM
}

func summariseMoorings(list []anchorageMooringWire) anchorageMooringSummary {
	sum := anchorageMooringSummary{Count: len(list), VesselSizes: []anchorageMooringSize{}, Nearest: []anchorageMooringRecord{}}
	sizes := map[float64]int{}
	for i, m := range list {
		if m.DetailsUntagged {
			sum.UntaggedCount++
		}
		if m.VesselSizeM != nil {
			sizes[*m.VesselSizeM]++
		}
		if i < anchorageNearestMoorings {
			sum.Nearest = append(sum.Nearest, m.anchorageMooringRecord)
		}
	}
	for size, n := range sizes {
		sum.VesselSizes = append(sum.VesselSizes, anchorageMooringSize{VesselSizeM: size, Count: n})
	}
	sort.Slice(sum.VesselSizes, func(i, j int) bool { return sum.VesselSizes[i].VesselSizeM < sum.VesselSizes[j].VesselSizeM })
	return sum
}
