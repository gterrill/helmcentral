package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"time"
)

var mateToolGetWindForecast = mateTool{
	name:      "get_wind_forecast",
	effect:    effectReads,
	audiences: audienceReviewsAndMCP,
	function: openRouterFunctionDef{
		Description: "Fetch the wind forecast (and wave forecast, where available) for a position, as a " +
			"per-day summary plus a three-hourly breakdown. Use find_places first to resolve a place name " +
			"to coordinates. If waves_no_data is true the wave model has no figures at that point, so say there is " +
			"no wave forecast there. If wave_fallback_model is set the wave figures come from that fallback model, " +
			"because the usual one has no data here, so say so. Where the position has charted shoreline around it, each row " +
			"carries shelter (sheltered, partial, exposed or reef) for the direction the wind comes FROM, each day carries " +
			"shelter_prevailing, and shelter_summary gives the sheltered and exposed arcs. " + ruleShelter + " If shelter_unavailable is " +
			"set and your answer judges an anchorage's shelter, say shelter could not be checked and why; for an offshore or " +
			"passage point, ignore it. wind_changes lists where one settled wind gives way to another over the " +
			"hours read, earliest first: date, time (a finished label, quote it as given), from and to (compass " +
			"points), speed_min_kn and speed_max_kn (the wind after the change), lower_confidence (more than about " +
			"three days out, so say it is less certain) and prominent (the wind after reaches the operator's lowest " +
			"marginal passage wind). An empty list means no change was found, not that the forecast is steady. " +
			"Name a change by its two directions and time, for example N to SE around 2 PM, never as a front. If " +
			"wind_changes_problem is set the changes were not judged against the operator's limits, so say that and " +
			"do not call any prominent or not. If wind_changes_checked_to is set the changes were read only up to that " +
			"time (wind_changes_checked_reason says why), so say later days were not checked. " + ruleWindForecast,
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
	execute: plainTool(assistantToolDeps.executeGetWindForecast),
	subject: func(args json.RawMessage) string {
		return fmt.Sprintf("wind forecast for %s", assistantLocationLabel(args))
	},
	describe: func(args json.RawMessage) string {
		return fmt.Sprintf("Fetching wind forecast for %s…", assistantLocationLabel(args))
	},
}

var mateToolGetTides = mateTool{
	name:      "get_tides",
	effect:    effectReads,
	audiences: audienceReviewsAndMCP,
	function: openRouterFunctionDef{
		Description: "Fetch tide predictions (high/low times and heights) for the tide station nearest a position. Use find_places first to resolve a place name to coordinates. " + ruleTides,
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
	execute: plainTool(assistantToolDeps.executeGetTides),
	subject: func(args json.RawMessage) string {
		return fmt.Sprintf("tides near %s", assistantLocationLabel(args))
	},
	describe: func(args json.RawMessage) string {
		return fmt.Sprintf("Fetching tides near %s…", assistantLocationLabel(args))
	},
}

var mateToolEstimatePassage = mateTool{
	name:      "estimate_passage",
	effect:    effectReads,
	audiences: audienceMCPOnly,
	function: openRouterFunctionDef{
		Description: "Estimate passage distance, time and fuel for a run to a destination. The time is on this " +
			"vessel's learned cruise pace, the same pace passage plans use, and eta_basis says how it was worked " +
			"out: quote it with the time. With no engine rpm recorded under way, or a pace not yet learned, there " +
			"are no hours and no_eta_reason says why: give that reason, never a time from some other speed. The fuel " +
			"is this vessel's logged burn (total fuel rate against rpm, from the last N days of telemetry) at the " +
			"learned cruise rpm; when it cannot be read, fuel_missing_reason says why and the time stands. Give " +
			"the destination coordinates (from find_places); the run starts at the " +
			"vessel's position unless you give from_lat and from_lon (a planned leg between two places). The " +
			"tool routes the run over water around islands and headlands using the offline coastline and " +
			"returns that distance as distance_nm, with course_deg (the longest leg's bearing), the legs and the waypoints; the " +
			"straight line is returned only as straight_line_nm, for comparison. If it cannot route (a " +
			"coastline square is not downloaded, an end point is on land, or no water connects the two) it " +
			"returns an error: report that error, never a straight-line distance. Omit speed to use the " +
			"learned cruise pace; a speed_kts the operator gives overrides it, and eta_basis then says it is their speed. " +
			"The pace spans whatever conditions occurred; head seas will add time and fuel.",
		Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"to_lat": {"type": "number", "description": "Destination latitude in decimal degrees."},
						"to_lon": {"type": "number", "description": "Destination longitude in decimal degrees."},
						"from_lat": {"type": "number", "description": "Start latitude. Omit both from_lat and from_lon to start at the vessel's position."},
						"from_lon": {"type": "number", "description": "Start longitude. Give both or neither."},
						"speed_kts": {
							"type": "number",
							"description": "Planned speed over ground in knots, only when the operator gave one. Omit to time the run on the vessel's learned cruise pace."
						},
						"days": {
							"type": "integer",
							"description": "Days of logged telemetry to draw the fuel burn from, 7 to 365 (default 90)."
						}
					},
					"required": ["to_lat", "to_lon"]
				}`),
	},
	execute: plainTool(assistantToolDeps.executeEstimatePassage),
	subject: func(args json.RawMessage) string {
		var a assistantEstimatePassageArgs
		if err := json.Unmarshal(args, &a); err != nil || a.ToLat == nil || a.ToLon == nil {
			return "the passage estimate"
		}
		return fmt.Sprintf("route to %.3f, %.3f", *a.ToLat, *a.ToLon)
	},
	describe: func(args json.RawMessage) string {
		var a assistantEstimatePassageArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return "Timing the passage…"
		}
		if a.ToLat == nil || a.ToLon == nil {
			return "Timing the passage…"
		}
		if a.SpeedKts != nil {
			return fmt.Sprintf("Routing to %.3f, %.3f and timing the run at %g kts…", *a.ToLat, *a.ToLon, *a.SpeedKts)
		}
		return fmt.Sprintf("Routing to %.3f, %.3f and timing the run…", *a.ToLat, *a.ToLon)
	},
}

// ── get_wind_forecast ──────────────────────────────────────────────────────────

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
	// Shelter is the anchorage's grade (sheltered, partial, exposed or reef)
	// for the bearing nearest this row's wind direction, from the Offline
	// coastline. Absent when the forecast carries no shelter, and when the
	// row's wind direction is unknown.
	Shelter string `json:"shelter,omitempty"`
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
	// ShelterPrevailing is the modal shelter label across the day's rows.
	ShelterPrevailing *string `json:"shelter_prevailing,omitempty"`
}

// assistantWindChange is one wind change as Mate reads it: the shared detector's
// output (forecastWindChanges), with the time worded on the place's clock.
type assistantWindChange struct {
	// Date is the place's calendar day the new wind sets in, YYYY-MM-DD; Time is
	// the finished label, "Sun 15 Nov 6.00 AM". Quote the label as given.
	Date string `json:"date"`
	Time string `json:"time"`
	windChangeFigures
}

func assistantWindChanges(changes []windChange, loc *time.Location) []assistantWindChange {
	out := make([]assistantWindChange, 0, len(changes))
	for _, c := range changes {
		out = append(out, assistantWindChange{
			Date: c.DayKey, Time: boatDayClock(c.At, loc), windChangeFigures: c.figures(),
		})
	}
	return out
}

type assistantWindForecastResult struct {
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	// CourseDeg echoes the model's course_deg argument back, present only
	// when it was given - the signal to the model that rel_wind/rel_wave
	// are populated below rather than silently absent.
	CourseDeg       *float64 `json:"course_deg,omitempty"`
	Timezone        string   `json:"timezone"`
	WeatherProvider string   `json:"weather_provider"`
	WaveProvider    string   `json:"wave_provider"`
	WavesError      string   `json:"waves_error,omitempty"`
	// WavesNoData is set when the wave model has no values at this point.
	// It is an answer, not a failure: say there is no wave forecast here.
	WavesNoData bool `json:"waves_no_data,omitempty"`
	// WaveFallbackModel names the model the wave figures came from when the
	// usual one has no data here.
	WaveFallbackModel string                `json:"wave_fallback_model,omitempty"`
	Cached            bool                  `json:"cached"`
	CachedAt          *string               `json:"cached_at"`
	Days              []assistantDaySummary `json:"days"`
	// WindChanges are the forecast's wind changes from now, earliest first; [] is
	// "none found" over the hours that were read (see WindChangesCheckedTo).
	WindChanges []assistantWindChange `json:"wind_changes"`
	// WindChangesProblem is why the changes were not judged against the operator's
	// passage limits, so none is marked prominent. Empty when they were.
	WindChangesProblem string `json:"wind_changes_problem,omitempty"`
	// WindChangesCheckedTo and WindChangesCheckedReason say where the reading
	// stopped when the hourly forecast ends early; both are empty when the whole
	// forecast was read.
	WindChangesCheckedTo     string              `json:"wind_changes_checked_to,omitempty"`
	WindChangesCheckedReason string              `json:"wind_changes_checked_reason,omitempty"`
	Hourly                   []assistantWindHour `json:"hourly"`
	Truncated                bool                `json:"truncated,omitempty"`
	// ShelterSummary and ShelterReading come from the Offline coastline
	// around lat/lon, present only when some bearing is not open water.
	// ShelterUnavailable is why the coastline could not be read, verbatim.
	ShelterSummary     string `json:"shelter_summary,omitempty"`
	ShelterReading     string `json:"shelter_reading,omitempty"`
	ShelterUnavailable string `json:"shelter_unavailable,omitempty"`
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
	// shelterVotes/shelterOrder are the same over each row's shelter label.
	shelterVotes map[string]int
	shelterOrder []string
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

func (a *assistantDayAggregate) addRelWind(label string) {
	if a.relWindVotes == nil {
		a.relWindVotes = map[string]int{}
	}
	if _, seen := a.relWindVotes[label]; !seen {
		a.relWindOrder = append(a.relWindOrder, label)
	}
	a.relWindVotes[label]++
}

// addRow votes one hour into the day: its wind, gust, direction, relative wind
// and shelter, each only when the row has it.
func (a *assistantDayAggregate) addRow(row assistantWindHour) {
	if row.WindKts != nil {
		a.addWind(*row.WindKts)
	}
	if row.GustKts != nil {
		a.addGust(*row.GustKts)
	}
	if row.Dir != nil {
		a.addDir(*row.Dir)
	}
	if row.RelWind != nil {
		a.addRelWind(*row.RelWind)
	}
	if row.Shelter != "" {
		a.addShelter(row.Shelter)
	}
}

func (a *assistantDayAggregate) addShelter(label string) {
	if a.shelterVotes == nil {
		a.shelterVotes = map[string]int{}
	}
	if _, seen := a.shelterVotes[label]; !seen {
		a.shelterOrder = append(a.shelterOrder, label)
	}
	a.shelterVotes[label]++
}

func (a *assistantDayAggregate) prevailingShelter() *string {
	return modeVote(a.shelterVotes, a.shelterOrder)
}

// shelterGradeAt is the grade of the 10 degree bearing nearest a wind
// direction (where the wind comes FROM).
func shelterGradeAt(bearings []shelterBearing, windFromDeg float64) string {
	want := int(math.Round(windFromDeg/10)) * 10 % 360
	for _, b := range bearings {
		if b.BearingDeg == want {
			return b.Grade
		}
	}
	return ""
}

// forecastShelter reads the shelter at a forecast position so each row can be
// labelled. It returns nil, with nothing to say, when the install has no
// Offline coastline reader, when the position is not a usable fix, when the
// operator has switched the coastline off (anchorage_details still says so if
// called) and when no bearing has any shoreline within 15 nm (open water).
// A coastline that cannot be read comes back as unavailable, verbatim.
func (d assistantToolDeps) forecastShelter(lat, lon float64) (est *shelterResult, unavailable string) {
	if d.offlineCoastline == nil || !hasUsableVesselPosition(lat, lon) {
		return nil, ""
	}
	if r := d.offlineCoastline(); r != nil && r.Enabled != nil {
		on, err := r.Enabled()
		if err != nil {
			log.Printf("assistant: get_wind_forecast: shelter unavailable at %.4f, %.4f: %v", lat, lon, err)
			return nil, err.Error()
		}
		if !on {
			return nil, ""
		}
	}
	res, err := d.shelterAt(lat, lon)
	if err != nil {
		log.Printf("assistant: get_wind_forecast: shelter unavailable at %.4f, %.4f: %v", lat, lon, err)
		return nil, err.Error()
	}
	for _, b := range res.Bearings {
		if !b.Open {
			return res, ""
		}
	}
	return nil, ""
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

// ── get_tides ──────────────────────────────────────────────────────────

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
	// Reading names the station, its distance and bearing from the place asked
	// about, and its time zone.
	Reading  string               `json:"reading"`
	Station  assistantStationInfo `json:"station"`
	Provider string               `json:"provider"`
	// HeightDatum is chart_datum or mean_sea_level: what every height_m here is
	// measured from. DatumNote is set for mean_sea_level and says what that
	// means for how the figures may be quoted.
	HeightDatum string `json:"height_datum"`
	DatumNote   string `json:"datum_note,omitempty"`
	// Modelled is true when the tide is worked out for the place with no station.
	Modelled  bool                   `json:"modelled"`
	Now       assistantTideNow       `json:"now"`
	TimeBasis string                 `json:"time_basis"`
	Extremes  []assistantTideExtreme `json:"extremes"`
	Cached    bool                   `json:"cached"`
	CachedAt  *string                `json:"cached_at"`
	Note      string                 `json:"note,omitempty"`
	Truncated bool                   `json:"truncated,omitempty"`
}

// assistantResolvedTideStation is what get_tides and plan_tidal_departure
// both need before they can read a station's extremes: the provider, the
// nearest station to the position, its fetched chart, and the zone its times
// are meaningful in.
type assistantResolvedTideStation struct {
	providerID string
	chart      tideChartResult
	loc        *time.Location
	timeBasis  string
	info       assistantStationInfo
	// datum is what the chart's heights are measured from; followsPos is true
	// when the tide is the place's own, not a station's, so no distance applies.
	datum      string
	followsPos bool
}

// toAssistantTideExtreme renders one predicted high or low water in loc.
func toAssistantTideExtreme(e tideExtremePoint, loc *time.Location) assistantTideExtreme {
	kind := "low"
	if e.High {
		kind = "high"
	}
	return assistantTideExtreme{
		Time: boatDayClock(e.Time, loc), ISO: e.Time.UTC().Format(time.RFC3339),
		Type: kind, HeightM: roundTo2(e.HeightM),
	}
}

func (d assistantToolDeps) executeGetTides(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var args assistantLocationArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse get_tides arguments: %w", err)
	}
	days := clampAssistantDays(args.Days, 3, assistantMaxTideDays)

	st, err := d.resolveTideStation(args.Lat, args.Lon)
	if err != nil {
		return "", fmt.Errorf("get_tides: %w", err)
	}
	providerID, res, loc, timeBasis, stationInfo := st.providerID, st.chart, st.loc, st.timeBasis, st.info

	now := d.now()
	localNow := now.In(loc)
	windowStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
	windowEnd := windowStart.AddDate(0, 0, days)

	extremes := make([]assistantTideExtreme, 0)
	for _, e := range res.Extremes {
		if e.Time.Before(windowStart) || !e.Time.Before(windowEnd) {
			continue
		}
		extremes = append(extremes, toAssistantTideExtreme(e, loc))
	}

	var cachedAt *string
	if !res.CachedAt.IsZero() {
		s := boatDayClock(res.CachedAt, loc)
		cachedAt = &s
	}

	result := assistantGetTidesResult{
		Reading:     readingTidesFor(st),
		Station:     stationInfo,
		Provider:    providerID,
		HeightDatum: st.datum,
		DatumNote:   tideDatumNoteFor(st.datum),
		Modelled:    st.followsPos,
		Now: assistantTideNow{
			Time:      boatDayClock(localNow, loc),
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

// ── estimate_passage ──────────────────────────────────────────────────────────

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

// assistantEstimatePassageArgs is estimate_passage's argument shape.
// SpeedKts is a pointer so "the model left it out" (nil, use the
// learned cruise pace) is distinguishable from a genuine zero, which
// is never a valid planned speed anyway.
//
// The distance is not an argument: it is routed over water from the two
// positions, so a straight line through an island cannot be passed in.
type assistantEstimatePassageArgs struct {
	ToLat    *float64 `json:"to_lat"`
	ToLon    *float64 `json:"to_lon"`
	FromLat  *float64 `json:"from_lat"`
	FromLon  *float64 `json:"from_lon"`
	SpeedKts *float64 `json:"speed_kts"`
	Days     int      `json:"days"`
}

type assistantEstimatePassageResult struct {
	assistantRoutedPassage
	// SpeedKts is the speed the time is worked out at: the learned cruise
	// speed, or the one asked for. Left out when there is no time.
	SpeedKts float64 `json:"speed_kts,omitempty"`
	// SpeedSource is "requested" when speed_kts came from the model, or
	// "learned_cruise" when it was omitted and the time is on the boat's
	// learned cruise pace (the one passage plans use).
	SpeedSource string `json:"speed_source,omitempty"`
	// Hours is nil, and NoEtaReason says why, when the boat has no learned
	// pace. EtaBasis is the sentence the hours were worked out on, the same
	// words a passage plan's leg carries.
	Hours       *float64 `json:"hours,omitempty"`
	NoEtaReason string   `json:"no_eta_reason,omitempty"`
	EtaBasis    string   `json:"eta_basis,omitempty"`
	// Litres, LPerH and RPM come from the logged history alone. When it cannot
	// be read they are left out and FuelMissingReason says why; the time never
	// depends on it.
	Litres            *float64          `json:"litres,omitempty"`
	LPerH             *float64          `json:"l_per_h,omitempty"`
	RPM               float64           `json:"rpm,omitempty"`
	FuelMissingReason string            `json:"fuel_missing_reason,omitempty"`
	HistoryDays       int               `json:"history_days,omitempty"`
	Samples           int               `json:"samples,omitempty"`
	Instances         []string          `json:"instances,omitempty"`
	Table             []performanceBand `json:"table,omitempty"`
	// FuelAboardL and FuelAfterL are omitted entirely (nil pointers,
	// omitempty) when fuelAboardM3 reports the fuel volume as not
	// currently defined - the note says so instead, rather than the model
	// seeing a 0 that reads as an empty tank.
	FuelAboardL *float64 `json:"fuel_aboard_l,omitempty"`
	FuelAfterL  *float64 `json:"fuel_after_l,omitempty"`
	Note        string   `json:"note"`
}

// prevailing returns the mode of the recorded direction votes, ties broken
// by first-seen order so the result is deterministic.
func (a *assistantDayAggregate) prevailing() *string {
	return modeVote(a.dirVotes, a.dirOrder)
}

// waveSeries is the wave forecast for a position, or why there is none.
// err is the wave provider's first error line and leaves byUnix empty; noData
// is an answer (the wave model has no figures here), not a failure.
type waveSeries struct {
	providerID    string
	err           string
	noData        bool
	fallbackModel string
	byUnix        map[int64]waveHourPoint
}

// fetchWaveSeries reads the wave forecast for get_wind_forecast and
// anchorage_details. A wave failure never fails the caller: the error rides in
// the result so the answer can say there are no wave figures and why.
func (d assistantToolDeps) fetchWaveSeries(lat, lon float64, days int) waveSeries {
	out := waveSeries{byUnix: map[int64]waveHourPoint{}}
	if d.waves == nil {
		out.err = "no wave provider is available on this install"
		return out
	}
	waveProvider, wpID, werr := d.waves()
	out.providerID = wpID
	if werr != nil {
		out.err = firstErrorLine(werr)
		return out
	}
	bundle, ferr := waveProvider.FetchWaves(lat, lon, days)
	if ferr != nil {
		out.err = firstErrorLine(ferr)
		return out
	}
	out.fallbackModel = bundle.FallbackModel
	out.noData = waveSeriesHasNoValues(bundle.Hourly)
	for _, whp := range bundle.Hourly {
		out.byUnix[whp.Time.Unix()] = whp
	}
	return out
}

// buildWindHour renders one forecast hour as the row Mate reads: wind and gust
// in knots, the direction, the wave figures when the wave model has them, the
// angles relative to course_deg when given, and the shelter grade for the wind
// direction when the position has a shelter estimate. A speed or gust the
// provider gave as zero is unknown, never a calm. A direction of zero is due
// north; an unknown direction is -1 (hasWindDirection). Shared by
// get_wind_forecast and anchorage_details so the two cannot read the same hour
// differently.
func buildWindHour(hp weatherHourPoint, loc *time.Location, waveByUnix map[int64]waveHourPoint, courseDeg *float64, shelter *shelterResult) assistantWindHour {
	row := assistantWindHour{T: boatDayClock(hp.Time, loc)}

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
	if hp.hasWindDirection() {
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
	if courseDeg != nil && dir != nil {
		relDeg := relativeAngleDeg(*courseDeg, hp.WindDirectionDeg)
		relLabel := relativeAngleLabel(relDeg)
		row.RelWindDeg, row.RelWind = &relDeg, &relLabel
	}

	if shelter != nil && dir != nil {
		row.Shelter = shelterGradeAt(shelter.Bearings, hp.WindDirectionDeg)
	}

	// A wave row needs a height. The period and direction are added only
	// when the provider gave them: an absent direction is not north, and
	// a zero period is not a period.
	if whp, ok := waveByUnix[hp.Time.Unix()]; ok && whp.WaveHeightM != nil {
		hm := roundTo1(*whp.WaveHeightM)
		row.WaveM = &hm
		if whp.WavePeriodS != nil && *whp.WavePeriodS > 0 {
			ps := roundTo1(*whp.WavePeriodS)
			row.WaveS = &ps
		}
		if whp.WaveDirectionDeg != nil {
			wd := degreesToDirection(*whp.WaveDirectionDeg)
			row.WaveDir = &wd
			if courseDeg != nil {
				relDeg := relativeAngleDeg(*courseDeg, *whp.WaveDirectionDeg)
				relLabel := relativeAngleLabel(relDeg)
				row.RelWaveDeg, row.RelWave = &relDeg, &relLabel
			}
		}
	}
	return row
}

// forecastWindow is the part of a forecast bundle a tool reports: every third
// local hour from `from` (to `until` when that is set), with the instant each
// row is for, and the day summaries over those rows.
type forecastWindow struct {
	at     []time.Time
	hourly []assistantWindHour
	days   []assistantDaySummary
	// last is the latest hour the bundle holds, inside the window or not.
	last time.Time
}

// forecastRows builds the rows and day summaries get_wind_forecast and
// anchorage_details share, so the two cannot read the same hour differently.
// pick, when set, chooses the hours kept in place of the default (every third
// local hour from `from` to `until`).
func forecastRows(hours []weatherHourPoint, loc *time.Location, from, until time.Time, pick func(time.Time) bool, waveByUnix map[int64]waveHourPoint, courseDeg *float64, shelter *shelterResult) forecastWindow {
	win := forecastWindow{at: []time.Time{}, hourly: []assistantWindHour{}, days: []assistantDaySummary{}}
	var dayOrder []string
	dayAggs := map[string]*assistantDayAggregate{}
	for _, hp := range hours {
		if hp.Time.IsZero() {
			continue
		}
		if hp.Time.After(win.last) {
			win.last = hp.Time
		}
		local := hp.Time.In(loc)
		if pick != nil {
			if !pick(hp.Time) {
				continue
			}
		} else if hp.Time.Before(from) || (!until.IsZero() && hp.Time.After(until)) || local.Hour()%3 != 0 {
			continue
		}
		row := buildWindHour(hp, loc, waveByUnix, courseDeg, shelter)
		win.at = append(win.at, hp.Time)
		win.hourly = append(win.hourly, row)

		key := local.Format("2006-01-02")
		agg, ok := dayAggs[key]
		if !ok {
			agg = &assistantDayAggregate{}
			dayAggs[key] = agg
			dayOrder = append(dayOrder, key)
		}
		agg.addRow(row)
	}
	for _, key := range dayOrder {
		agg := dayAggs[key]
		win.days = append(win.days, assistantDaySummary{
			Date: key, WindKtsMin: agg.windMin, WindKtsMax: agg.windMax, GustKtsMax: agg.gustMax,
			DirPrevailing: agg.prevailing(), RelWindPrevailing: agg.prevailingRelWind(), ShelterPrevailing: agg.prevailingShelter(),
		})
	}
	return win
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

	place, err := zoneAt(args.Lat, args.Lon)
	if err != nil {
		return "", fmt.Errorf("get_wind_forecast: %w", err)
	}
	bundle, err := provider.FetchForecast(args.Lat, args.Lon, days, place.Name)
	if err != nil {
		return "", fmt.Errorf("get_wind_forecast: weather provider %q: %w", providerID, err)
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	waves := d.fetchWaveSeries(args.Lat, args.Lon, days)
	waveProviderID, wavesError, waveFallbackModel, wavesNoData, waveByUnix := waves.providerID, waves.err, waves.fallbackModel, waves.noData, waves.byUnix

	shelter, shelterUnavailable := d.forecastShelter(args.Lat, args.Lon)

	win := forecastRows(bundle.Hourly, place.Loc, d.now().Truncate(time.Hour), time.Time{}, nil, waveByUnix, args.CourseDeg, shelter)

	var cachedAt *string
	if !bundle.CachedAt.IsZero() {
		s := boatDayClock(bundle.CachedAt, place.Loc)
		cachedAt = &s
	}

	// The one detector the forecast page and the watch report use, judged against
	// the operator's passage limits. Limits that cannot be read leave the changes
	// listed and unmarked, and the problem rides in the result.
	var limits passageLimits
	limitsErr := errors.New("passage limits are not available on this install")
	if d.passageLimits != nil {
		limits, limitsErr = d.passageLimits()
	}
	scan := forecastWindChanges(bundle.Hourly, lastDayStart(bundle.Days), d.now(), place.Loc, limits, limitsErr)
	// Mate quotes a label as given, so the point the reading stopped at is worded
	// as every other time in a tool result, not as the chart axis words it.
	checkedTo := ""
	if scan.CheckedTo != "" {
		checkedTo = boatDayClock(scan.ReadTo, place.Loc)
	}

	result := assistantWindForecastResult{
		Name:              strings.TrimSpace(args.Name),
		Lat:               args.Lat,
		Lon:               args.Lon,
		CourseDeg:         args.CourseDeg,
		Timezone:          place.Name,
		WeatherProvider:   providerID,
		WaveProvider:      waveProviderID,
		WavesError:        wavesError,
		WavesNoData:       wavesNoData,
		WaveFallbackModel: waveFallbackModel,
		Cached:            bundle.Cached,
		CachedAt:          cachedAt,
		Days:              win.days,
		Hourly:            win.hourly,

		WindChanges:              assistantWindChanges(scan.Changes, place.Loc),
		WindChangesProblem:       scan.Problem,
		WindChangesCheckedTo:     checkedTo,
		WindChangesCheckedReason: scan.CheckedReason,

		ShelterUnavailable: shelterUnavailable,
	}
	if shelter != nil {
		result.ShelterSummary, result.ShelterReading = shelter.Summary, shelter.Reading
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

// silentEnginePathError explains an empty engine series. When the engine's
// source is known never to carry RPM (it is alive in the log with other
// readings) the item is mapped to the wrong source, and the error says so and
// where to fix it. Otherwise the engine simply logged nothing in the window
// (it did not run, or the log is short) and the mapping is not blamed.
func (d assistantToolDeps) silentEnginePathError(engine vesselEngineSetting, path string, days int) error {
	if d.engineSignature(engine.Instance) == signatureMissing {
		return fmt.Errorf("nothing logged for %s in the last %d days; the engine %q is set to propulsion.%s, which has never reported RPM - check its source in Settings > SignalK",
			path, days, engine.Name, engine.Instance)
	}
	return fmt.Errorf("the engine %q logged nothing for %s in the last %d days (it may not have run in that time)",
		engine.Name, path, days)
}

// executeEstimatePassage times a routed run on the boat's learned cruise pace
// (passageTiming, the model passage plan legs use), or at the speed the model
// asked for, and reads the fuel off the boat's own logged speed over ground,
// total fuel rate and rpm (buildPerformanceTable, assistant_performance.go) at
// the learned band's rpm. Every number here is this vessel's own history, not
// a polar or a fuel curve looked up from a manufacturer spec sheet - see ADR
// 0093 section 12 and its 2026-11-26 amendment.
func (d assistantToolDeps) executeEstimatePassage(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var args assistantEstimatePassageArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse estimate_passage arguments: %w", err)
	}
	if args.ToLat == nil || args.ToLon == nil {
		return "", fmt.Errorf("estimate_passage: to_lat and to_lon are required: the distance is routed over water from coordinates, so there is no distance to pass in")
	}
	if args.SpeedKts != nil && *args.SpeedKts <= 0 {
		return "", fmt.Errorf("estimate_passage: speed_kts must be greater than zero")
	}

	// The route comes first: with no passage distance there is nothing to
	// estimate, and the straight line is never an acceptable stand-in.
	routed, err := d.routePassage(ctx, "estimate_passage", args.FromLat, args.FromLon, *args.ToLat, *args.ToLon)
	if err != nil {
		return "", err
	}

	// The time never waits on the fuel history: it is the speed asked for, or
	// the boat's learned cruise pace, and without either it is no time and a
	// reason, never a speed read off the log.
	timing, err := d.passageTiming(ctx, routed, args.SpeedKts)
	if err != nil {
		return "", err
	}

	result := assistantEstimatePassageResult{
		assistantRoutedPassage: routed,
		SpeedKts:               roundTo1(timing.speedKn),
		SpeedSource:            timing.source,
		NoEtaReason:            timing.noEta,
		EtaBasis:               timing.basis,
	}
	timed := timing.noEta == ""
	if timed {
		result.Hours = floatPtr(roundTo1(timing.hours))
	}

	// The fuel is the logged history read at the rpm the time is based on. When
	// it cannot be read, the figures are left out and the reason is given.
	perf, perr := d.loggedPerformance(ctx, args.Days)
	if perr != nil {
		if cerr := ctx.Err(); cerr != nil {
			return "", cerr
		}
		result.FuelMissingReason = perr.Error()
	} else {
		if fuel, reason := perf.fuelFor(timing); reason != "" {
			result.FuelMissingReason = reason
		} else {
			result.LPerH = floatPtr(roundTo1(fuel.lPerH))
			result.RPM = math.Round(fuel.rpm)
			if timed {
				result.Litres = floatPtr(math.Round(fuel.litres(timing.hours)))
			}
		}
	}
	result.HistoryDays = perf.days
	result.Samples = perf.samples
	result.Instances = perf.instances
	result.Table = perf.table

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
	// The note says only what holds: the time clause follows the eta_basis or the
	// no_eta_reason, the fuel clause the fuel_missing_reason.
	timeNote := "the time is the boat's learned cruise pace"
	switch {
	case !timed:
		timeNote = "no time was worked out"
	case timing.source == "requested":
		timeNote = "the time is at the speed you gave"
	}
	fuelNote := "the fuel its logged history"
	if timing.fuelRPM > 0 {
		fuelNote = "the fuel its logged burn at cruise rpm, for every hour, slow water included"
	}
	if result.FuelMissingReason != "" {
		fuelNote = "no fuel figures were read"
	}
	note := timeNote + " and " + fuelNote + ", across whatever conditions occurred; head seas add time and fuel"
	if aboardOK {
		aboard := math.Round(aboardM3 * 1000)
		fuelAboardL = &aboard
		if result.Litres != nil {
			after := aboard - *result.Litres
			fuelAfterL = &after
		}
	} else {
		note += "; fuel aboard unknown"
	}
	result.FuelAboardL, result.FuelAfterL, result.Note = fuelAboardL, fuelAfterL, note

	shrink := func() bool {
		// The route's waypoints are detail: they go before anything that
		// answers the question, so distance, time and fuel always survive.
		if result.assistantRoutedPassage.dropRoute() {
			return true
		}
		if len(result.Table) == 0 {
			return false
		}
		// Drop the least-sampled band next - it says the least about the
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

// passageTiming is how long a routed run takes. noEta says why there is no time,
// when the boat has no learned pace and no speed was asked for.
type passageTiming struct {
	hours   float64
	speedKn float64
	// source is "requested" or "learned_cruise"; basis is the sentence the hours
	// were worked out on.
	source, basis string
	noEta         string
	// fuelAtSpeed is the speed asked for, which the burn is read at; fuelRPM is the
	// middle of the learned cruise band, which it is read at otherwise. Both are 0
	// with no time.
	fuelAtSpeed, fuelRPM float64
}

// passageTiming times routed at the speed asked for, or on the boat's learned
// cruise pace along the route (the model passage plan legs use, with its slow
// water), with the same words for how the time was worked out. A pace that is
// not learned is "No ETA: <why>", the plan page's wording: never a speed read off
// the log. Only a cancelled call is an error.
func (d assistantToolDeps) passageTiming(ctx context.Context, routed assistantRoutedPassage, requested *float64) (passageTiming, error) {
	if requested != nil {
		return passageTiming{
			hours: routed.rawDistanceNm / *requested, speedKn: *requested, source: "requested",
			basis:       fmt.Sprintf("At your speed of %.1f kn.", *requested),
			fuelAtSpeed: *requested,
		}, nil
	}
	noEta := func(err error) (passageTiming, error) {
		if cerr := ctx.Err(); cerr != nil {
			return passageTiming{}, cerr
		}
		return passageTiming{noEta: "No ETA: " + err.Error()}, nil
	}
	if d.legPace == nil {
		return noEta(errPassageRecordUnavailable)
	}
	now := d.now()
	model, err := d.legPace(ctx, now)
	if err != nil {
		return noEta(err)
	}
	if model == nil {
		return noEta(errPassageRecordUnavailable)
	}
	leg := planLeg{DistanceNm: routed.rawDistanceNm, Route: make([][2]float64, len(routed.Route))}
	for i, p := range routed.Route {
		leg.Route[i] = [2]float64{p.Lat, p.Lon}
	}
	profile, err := model.profile(leg)
	if err != nil {
		return noEta(err)
	}
	tl, err := profile.pace(now)
	if err != nil {
		return noEta(err)
	}
	return passageTiming{
		hours: tl.hours(), speedKn: model.CruiseKn, source: "learned_cruise",
		basis:   etaBasisText(model.CruiseKn, hoursDuration(profile.slowHours()), ""),
		fuelRPM: (model.BandLowRPM + model.BandHighRPM) / 2,
	}, nil
}

// passageFuel is the burn rate and the rpm it was read at.
type passageFuel struct{ lPerH, rpm float64 }

// litres is the run's fuel: every hour at the one burn rate, slow water included.
func (f passageFuel) litres(hours float64) float64 {
	return hours * f.lPerH
}

// fuelFor reads the burn off the logged history at the rpm the time is based on:
// the learned band's middle. A speed asked for is not the band's, so the burn is
// read at that speed; with no time at all, at the most-sampled logged band. A
// cruise rpm, or a speed asked for, outside the range the history's bands cover
// gets no burn: it is not read off the nearest band. The reason is why the history cannot give a burn, ""
// when it does.
func (p loggedPerformance) fuelFor(t passageTiming) (passageFuel, string) {
	const unreadable = "the logged history has no burn rate to read at that speed or rpm"
	switch {
	case t.fuelAtSpeed > 0:
		lo, hi, ok := sogRange(p.table)
		if !ok {
			return passageFuel{}, unreadable
		}
		if t.fuelAtSpeed < lo || t.fuelAtSpeed > hi {
			return passageFuel{}, fmt.Sprintf("the logged fuel history's speed bands don't reach %.1f kn in the last %d days", t.fuelAtSpeed, p.days)
		}
		l, rpm, ok := estimateAtSpeed(p.table, t.fuelAtSpeed)
		if !ok {
			return passageFuel{}, unreadable
		}
		return passageFuel{lPerH: l, rpm: rpm}, ""
	case t.fuelRPM > 0:
		lo, hi, ok := rpmRange(p.table)
		if !ok {
			return passageFuel{}, unreadable
		}
		if t.fuelRPM < lo || t.fuelRPM > hi {
			return passageFuel{}, fmt.Sprintf("the logged fuel history's speed bands don't reach the cruise rpm (about %.0f rpm) in the last %d days", t.fuelRPM, p.days)
		}
		l, ok := estimateAtRPM(p.table, t.fuelRPM)
		if !ok {
			return passageFuel{}, unreadable
		}
		return passageFuel{lPerH: l, rpm: t.fuelRPM}, ""
	default:
		band, ok := mostSampledBand(p.table)
		if !ok {
			return passageFuel{}, unreadable
		}
		return passageFuel{lPerH: band.LPerH, rpm: band.RPM}, ""
	}
}

// loggedPerformance is the boat's own speed-to-burn history for estimate_passage.
// Its errors carry no tool name.
func (d assistantToolDeps) loggedPerformance(ctx context.Context, historyDays int) (loggedPerformance, error) {
	days := clampAssistantDays(historyDays, assistantEstimatePassageDefaultHistoryDays, assistantEstimatePassageMaxHistoryDays)
	if days < assistantEstimatePassageMinHistoryDays {
		days = assistantEstimatePassageMinHistoryDays
	}

	stop := d.now()
	start := stop.AddDate(0, 0, -days)

	none := loggedPerformance{}
	if d.influxRange == nil || d.fuelRateInstances == nil {
		return none, errors.New("the logged fuel history is not available on this install")
	}
	engines, instErr := d.fuelRateInstances()
	if instErr != nil {
		return none, fmt.Errorf("the vessel settings could not be read, so the engines are unknown: %w", instErr)
	}
	if len(engines) == 0 {
		return none, fmt.Errorf("engines are not set up (no engine item is assigned to a propulsion source in Settings > SignalK), so there is no per-engine fuel history to estimate from")
	}

	instances := make([]string, 0, len(engines))
	for _, engine := range engines {
		instances = append(instances, engine.Instance)
	}

	sog, err := d.influxRange("navigation.speedOverGround", start, stop, assistantPerformanceQueryEvery)
	if err != nil {
		return none, err
	}

	fuelRates := make([][]telemetryPoint, 0, len(engines))
	for _, engine := range engines {
		if err := ctx.Err(); err != nil {
			return none, err
		}
		path := fmt.Sprintf("propulsion.%s.fuel.rate", engine.Instance)
		series, ferr := d.influxRange(path, start, stop, assistantPerformanceQueryEvery)
		if ferr != nil {
			return none, ferr
		}
		if len(series) == 0 {
			return none, d.silentEnginePathError(engine, path, days)
		}
		fuelRates = append(fuelRates, series)
	}

	if err := ctx.Err(); err != nil {
		return none, err
	}

	// rpm comes from one representative instance rather than an average across
	// every engine: twin engines run matched revs under way. The learned band is a
	// mean of the engines running, so a twin run at split revs reads the wrong band
	// here.
	rpmPath := fmt.Sprintf("propulsion.%s.revolutions", engines[0].Instance)
	rpm, err := d.influxRange(rpmPath, start, stop, assistantPerformanceQueryEvery)
	if err != nil {
		return none, err
	}
	if len(rpm) == 0 {
		return none, d.silentEnginePathError(engines[0], rpmPath, days)
	}

	table := buildPerformanceTable(sog, fuelRates, rpm, assistantPerformanceMinSOGKts)
	if len(table) == 0 {
		return none, fmt.Errorf("no underway history in the last %d days", days)
	}

	totalSamples := 0
	for _, band := range table {
		totalSamples += band.Samples
	}
	return loggedPerformance{days: days, instances: instances, table: table, samples: totalSamples}, nil
}

// loggedPerformance is what loggedPerformance reads off the boat's history: the
// speed-to-burn table.
type loggedPerformance struct {
	days      int
	instances []string
	table     []performanceBand
	samples   int
}

// roundTo5 rounds a coordinate to five decimals, about a metre.
func roundTo5(v float64) float64 { return math.Round(v*1e5) / 1e5 }
