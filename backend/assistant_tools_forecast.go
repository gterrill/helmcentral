package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

var mateToolGetWindForecast = mateTool{
	name: "get_wind_forecast",
	function: openRouterFunctionDef{
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
	execute: assistantToolDeps.executeGetWindForecast,
	describe: func(args json.RawMessage) string {
		return fmt.Sprintf("Fetching wind forecast for %s…", assistantLocationLabel(args))
	},
}

var mateToolGetTides = mateTool{
	name: "get_tides",
	function: openRouterFunctionDef{
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
	execute: assistantToolDeps.executeGetTides,
	describe: func(args json.RawMessage) string {
		return fmt.Sprintf("Fetching tides near %s…", assistantLocationLabel(args))
	},
}

var mateToolEstimatePassage = mateTool{
	name: "estimate_passage",
	function: openRouterFunctionDef{
		Description: "Estimate passage distance, time and fuel for a run to a destination, from this vessel's " +
			"own logged performance (speed over ground against total fuel rate and rpm, from the last N days " +
			"of telemetry). Give the destination coordinates (from find_places); the run starts at the " +
			"vessel's position unless you give from_lat and from_lon (a planned leg between two places). The " +
			"tool routes the run over water around islands and headlands using the offline coastline and " +
			"returns that distance as distance_nm, with course_deg (the longest leg's bearing), the legs and the waypoints; the " +
			"straight line is returned only as straight_line_nm, for comparison. If it cannot route (a " +
			"coastline square is not downloaded, an end point is on land, or no water connects the two) it " +
			"returns an error: report that error, never a straight-line distance. Omit speed to use the " +
			"most-sampled cruising band. Observed data spans whatever conditions occurred; head seas will " +
			"add time and fuel.",
		Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"to_lat": {"type": "number", "description": "Destination latitude in decimal degrees."},
						"to_lon": {"type": "number", "description": "Destination longitude in decimal degrees."},
						"from_lat": {"type": "number", "description": "Start latitude. Omit both from_lat and from_lon to start at the vessel's position."},
						"from_lon": {"type": "number", "description": "Start longitude. Give both or neither."},
						"speed_kts": {
							"type": "number",
							"description": "Planned speed over ground in knots. Omit to use the vessel's most-sampled cruising band."
						},
						"days": {
							"type": "integer",
							"description": "Days of logged telemetry to draw the speed-to-burn table from, 7 to 365 (default 90)."
						}
					},
					"required": ["to_lat", "to_lon"]
				}`),
	},
	execute: assistantToolDeps.executeEstimatePassage,
	describe: func(args json.RawMessage) string {
		var a assistantEstimatePassageArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return "Estimating the passage from the log…"
		}
		if a.ToLat == nil || a.ToLon == nil {
			return "Estimating the passage from the log…"
		}
		if a.SpeedKts != nil {
			return fmt.Sprintf("Routing to %.3f, %.3f and estimating at %g kts from the log…", *a.ToLat, *a.ToLon, *a.SpeedKts)
		}
		return fmt.Sprintf("Routing to %.3f, %.3f and estimating at cruising speed from the log…", *a.ToLat, *a.ToLon)
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

	st, err := d.resolveTideStation("get_tides", args.Lat, args.Lon)
	if err != nil {
		return "", err
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
		Station:  stationInfo,
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
// most-sampled cruising band) is distinguishable from a genuine zero, which
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
	SpeedKts float64 `json:"speed_kts"`
	// SpeedSource is "requested" when speed_kts came from the model, or
	// "most_sampled_band" when it was omitted and estimate_passage picked
	// the vessel's most-sampled cruising band instead.
	SpeedSource string            `json:"speed_source"`
	Hours       float64           `json:"hours"`
	Litres      float64           `json:"litres"`
	LPerH       float64           `json:"l_per_h"`
	RPM         float64           `json:"rpm"`
	HistoryDays int               `json:"history_days"`
	Samples     int               `json:"samples"`
	Instances   []string          `json:"instances"`
	Table       []performanceBand `json:"table"`
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

	loc := place.Loc
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
				if args.CourseDeg != nil {
					relDeg := relativeAngleDeg(*args.CourseDeg, *whp.WaveDirectionDeg)
					relLabel := relativeAngleLabel(relDeg)
					row.RelWaveDeg, row.RelWave = &relDeg, &relLabel
				}
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
		Timezone:        place.Name,
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

// silentEnginePathError explains an empty engine series. When the engine's
// source is known never to carry RPM (it is alive in the log with other
// readings) the item is mapped to the wrong source, and the error says so and
// where to fix it. Otherwise the engine simply logged nothing in the window
// (it did not run, or the log is short) and the mapping is not blamed.
func (d assistantToolDeps) silentEnginePathError(engine vesselEngineSetting, path string, days int) error {
	if d.engineSignature(engine.Instance) == signatureMissing {
		return fmt.Errorf("estimate_passage: nothing logged for %s in the last %d days; the engine %q is set to propulsion.%s, which has never reported RPM - check its source in Settings > SignalK",
			path, days, engine.Name, engine.Instance)
	}
	return fmt.Errorf("estimate_passage: the engine %q logged nothing for %s in the last %d days (it may not have run in that time)",
		engine.Name, path, days)
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
	distanceNm := routed.rawDistanceNm

	days := clampAssistantDays(args.Days, assistantEstimatePassageDefaultHistoryDays, assistantEstimatePassageMaxHistoryDays)
	if days < assistantEstimatePassageMinHistoryDays {
		days = assistantEstimatePassageMinHistoryDays
	}

	stop := d.now()
	start := stop.AddDate(0, 0, -days)

	engines, instErr := d.fuelRateInstances()
	if instErr != nil {
		return "", fmt.Errorf("estimate_passage: the vessel settings could not be read, so the engines are unknown: %w", instErr)
	}
	if len(engines) == 0 {
		return "", fmt.Errorf("estimate_passage: engines are not set up (no engine item is assigned to a propulsion source in Settings > SignalK), so there is no per-engine fuel history to estimate from")
	}

	instances := make([]string, 0, len(engines))
	for _, engine := range engines {
		instances = append(instances, engine.Instance)
	}

	sog, err := d.influxRange("navigation.speedOverGround", start, stop, assistantPerformanceQueryEvery)
	if err != nil {
		return "", fmt.Errorf("estimate_passage: %w", err)
	}

	fuelRates := make([][]telemetryPoint, 0, len(engines))
	for _, engine := range engines {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		path := fmt.Sprintf("propulsion.%s.fuel.rate", engine.Instance)
		series, ferr := d.influxRange(path, start, stop, assistantPerformanceQueryEvery)
		if ferr != nil {
			return "", fmt.Errorf("estimate_passage: %w", ferr)
		}
		if len(series) == 0 {
			return "", d.silentEnginePathError(engine, path, days)
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
	rpmPath := fmt.Sprintf("propulsion.%s.revolutions", engines[0].Instance)
	rpm, err := d.influxRange(rpmPath, start, stop, assistantPerformanceQueryEvery)
	if err != nil {
		return "", fmt.Errorf("estimate_passage: %w", err)
	}
	if len(rpm) == 0 {
		return "", d.silentEnginePathError(engines[0], rpmPath, days)
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

	hours := distanceNm / speedKts
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
		assistantRoutedPassage: routed,
		SpeedKts:               roundTo1(speedKts),
		SpeedSource:            speedSource,
		Hours:                  roundTo1(hours),
		Litres:                 litresRounded,
		LPerH:                  roundTo1(lPerH),
		RPM:                    math.Round(rpmAtSpeed),
		HistoryDays:            days,
		Samples:                totalSamples,
		Instances:              instances,
		Table:                  table,
		FuelAboardL:            fuelAboardL,
		FuelAfterL:             fuelAfterL,
		Note:                   note,
	}

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

// roundTo5 rounds a coordinate to five decimals, about a metre.
func roundTo5(v float64) float64 { return math.Round(v*1e5) / 1e5 }
