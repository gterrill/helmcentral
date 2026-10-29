package main

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// gustWindowLadder is the shared, ordered set of "max gust" windows exposed
// via the vessel-state API's max_gust_kts field (and the frontend's MAX GUST
// cards, which cycle through it shortest-to-longest). It is the single
// source of truth for these windows - do not duplicate this literal
// elsewhere in the package.
var gustWindowLadder = []string{"10m", "30m", "1h", "24h"}

type vesselStateData struct {
	Name     string
	Status   string
	Datetime time.Time
	Depth    float64
	// DepthLastUpdateAge is how many seconds old the environment.depth.
	// belowTransducer reading is, per the ADR 0068 last_update_age_s
	// mechanism (-1 when the source carries no timestamp). Scoped to
	// belowTransducer specifically, not the whole environment.depth branch,
	// so a live belowSurface/belowKeel sensor can never mask a dead
	// belowTransducer reading — belowTransducer is the value this tile
	// actually renders, and depth is the number a dead transducer leaves the
	// operator to run aground on.
	DepthLastUpdateAge float64
	// LengthOverallM is the vessel's LOA read from SignalK's design.length
	// branch (ADR 0047). It carries the lookupNumber -1 sentinel when
	// unpublished; buildVesselStatePayload nils it out rather than letting
	// -1 leak into the API response.
	LengthOverallM float64
	// DraftM is the vessel's maximum draft read from SignalK's design.draft
	// branch (ADR 0135), for the Anchor Watch low-water clearance warning.
	// Only design.draft.maximum is read - current/minimum/canoe are never
	// published on the live server this feature was verified against, and
	// maximum is the only figure that is always the worst case. Carries the
	// lookupNumber -1 sentinel when unpublished; buildVesselStatePayload
	// nils it out rather than letting -1 leak into the API response.
	DraftM                float64
	CurrentDriftKts       float64
	CurrentSetDeg         float64
	CurrentDriftImpactKts *float64
	Latitude              float64
	Longitude             float64
	// PositionLastUpdateAge is the freshest of navigation.position's and
	// navigation.gnss's own last_update_age_s (ADR 0068): the fix itself and
	// its quality/HDOP/satellite-count figures are published under two
	// different parents, and either one still updating means the fix is
	// current. -1 when neither subtree carries a timestamp.
	PositionLastUpdateAge float64
	GNSSQualityIndicator  int
	GNSSHDOP              float64
	GNSSSatellites        int
	GNSSValidationState   string
	GNSSValidationReason  string
	GNSSCriticalAlert     bool
	HeadingTrue           float64
	SpeedOverGroundKts    float64
	WindSpeedApparentKts  float64
	WindAngleApparentDeg  float64
	WindSide              string
	WindAngleRelativeDeg  float64
	// True wind: environment.wind.speedTrue/angleTrueWater/directionTrue,
	// parsed the same way as the Apparent fields above but never derived
	// from them or from each other - see fetchSignalKVesselState's true-wind
	// block. Two tiles read these: the Current Conditions tile's readout
	// (ADR 0129: speedTrue/directionTrue) and the Wind tile's True mode
	// (ADR 0130: adds angleTrueWater's WindAngleTrueDeg/WindSideTrue/
	// WindAngleTrueRelativeDeg on top). All five carry the lookupNumber -1
	// (WindSideTrue: "") sentinel when the boat publishes no true-wind
	// source, or that particular leaf's own reading has gone stale, rather
	// than falling back to 0/starboard the way the Apparent fields
	// historically do - a boat with no true-wind source must show a dash,
	// never the apparent figure relabelled (AGENTS.md fallback policy).
	WindSpeedTrueKts         float64
	WindAngleTrueDeg         float64
	WindSideTrue             string
	WindAngleTrueRelativeDeg float64
	// WindDirectionTrueDeg is the compass bearing (0-360, true north) the
	// wind is blowing FROM - environment.wind.directionTrue, already
	// absolute rather than bow-relative, so it carries no "side."
	WindDirectionTrueDeg float64
	// WindLastUpdateAge is the freshest last_update_age_s found anywhere
	// under environment.wind (ADR 0068), scoped to that subtree only —
	// environment.current is a different sensor with its own health and
	// must not be folded in. -1 when environment.wind carries no timestamp.
	// Shared by true and apparent wind: both live in the same subtree.
	WindLastUpdateAge           float64
	GeneratorState              string
	GeneratorManualStart        bool
	GeneratorManualStartTimer   float64
	GeneratorRunningByCondition string
	GeneratorRuntime            float64
	// EnginesRunning is nil when no engine is configured in Vessel settings
	// or none has a fresh reading; the wire carries null.
	EnginesRunning *bool
}

type depthTrendResponse struct {
	Points     []depthTrendPoint `json:"points"`
	Since      string            `json:"since"`
	TideType   string            `json:"tide_type,omitempty"`
	TideDepthM float64           `json:"tide_depth_m,omitempty"`
}

func depthTrend(c echo.Context) error {
	window := c.QueryParam("window")
	if window == "" {
		window = "3h"
	}
	return c.JSON(http.StatusOK, buildDepthTrend(window))
}

// buildDepthTrend is the /api/depth-trend body for a window. Mate's Depth and
// Tide tile card reads the trend through it (ADR 0229).
func buildDepthTrend(window string) depthTrendResponse {
	points := inMemoryDepthTrend(window)
	if influxTelemetryConfigured() {
		points = queryInfluxDepthTrend(window)
	}
	if points == nil {
		points = []depthTrendPoint{}
	}

	if turn, ok := findLastTideTurningPoint(points); ok {
		tideType := "low"
		if turn.IsHigh {
			tideType = "high"
		}
		return depthTrendResponse{Points: points, Since: "tide", TideType: tideType, TideDepthM: turn.DepthM}
	}

	return depthTrendResponse{Points: points, Since: "window"}
}

// computeMaxGustKtsFor picks exactly one source for the gust ladder per
// request: Influx when configured, read from the background ticker's cache
// (telemetry_influx_cache.go) rather than queried live, otherwise the
// in-memory ring buffer (inMemoryMaxWindGustKtsFor). Previously both were
// computed unconditionally and the in-memory result discarded whenever Influx
// was configured - wasted CPU/allocations on every /api/vessel-state request
// on Influx-backed deployments.
func computeMaxGustKtsFor(windows []string) map[string]float64 {
	if influxTelemetryConfigured() {
		return cachedMaxGustKtsFor(windows, time.Now())
	}
	return inMemoryMaxWindGustKtsFor(windows)
}

// computeMaxGustTrueKtsFor is computeMaxGustKtsFor's true-wind counterpart
// (ADR 0130). Unlike the apparent ladder, it has no Influx-backed path yet —
// only the in-memory ring buffer (trueWindGustHistory, recorded alongside
// the apparent one in sampleTracks) — so a true-wind gust history does not
// survive a backend restart on an Influx-configured boat the way the
// apparent one does. Revisit if that gap matters in practice.
func computeMaxGustTrueKtsFor(windows []string) map[string]float64 {
	return inMemoryMaxTrueWindGustKtsFor(windows)
}

// buildVesselStatePayload produces the /api/vessel-state body. It is separate
// from the handler so the SSE stream can emit the identical shape without the
// two drifting apart.
func buildVesselStatePayload() map[string]any {
	state := vesselStateData{
		Status:               getEnv("VESSEL_STATUS", "At Anchor"),
		Datetime:             time.Now().UTC(),
		Depth:                -1,
		LengthOverallM:       -1,
		DraftM:               -1,
		CurrentDriftKts:      -1,
		CurrentSetDeg:        -1,
		Latitude:             -1,
		Longitude:            -1,
		HeadingTrue:          -1,
		WindSpeedApparentKts: -1,
		WindAngleApparentDeg: -1,
		WindAngleRelativeDeg: -1,
		// True wind (ADR 0129 / ADR 0130) — see fetchSignalKVesselState's
		// own comment on these fields for why they default to -1/"" rather
		// than the apparent fields' 0/starboard fallback.
		WindSpeedTrueKts:         -1,
		WindAngleTrueDeg:         -1,
		WindAngleTrueRelativeDeg: -1,
		WindDirectionTrueDeg:     -1,
		// Unknown, not zero, when SignalK is unconfigured and this fallback
		// literal never gets overwritten below.
		DepthLastUpdateAge:    -1,
		PositionLastUpdateAge: -1,
		WindLastUpdateAge:     -1,
	}
	source := "backend-fallback"

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)

	if signalkURL != "" {
		signalkState, err := fetchSignalKVesselState()
		// Always adopt signalkState, even on error: fetchSignalKVesselState
		// now always returns position/GNSS fields correctly marked critical
		// (and frozen at the last trusted fix) rather than a bare sentinel,
		// so the anchor watch alarm can tell "SignalK unreachable" apart from
		// "vessel has actually moved."
		state = signalkState
		if err == nil {
			source = "signalk"
		} else {
			source = "signalk-unreachable"
		}
	} else {
		state = criticalVesselState(state, "signalk not configured")
	}

	maxGustKts := computeMaxGustKtsFor(gustWindowLadder)
	// Clamp walking the ladder shortest-to-longest: the shortest window's -1
	// sentinel (no data) clamps to 0, then each subsequent (longer) window is
	// clamped to be >= the previous, already-clamped window's value - a
	// longer window's max gust can never be less than a shorter window's,
	// generalizing the previous 10m/1h-only clamp across the full ladder.
	previous := 0.0
	for i, window := range gustWindowLadder {
		value := maxGustKts[window]
		if i == 0 && value < 0 {
			value = 0
		}
		if value < previous {
			value = previous
		}
		maxGustKts[window] = value
		previous = value
	}

	// True wind's own MAX GUST ladder (ADR 0130), same window ladder as the
	// apparent one above but NOT the same "no data clamps to 0" treatment:
	// a boat with no true-wind source, or one that just restarted with an
	// empty ring buffer, must read as unknown (-1, which the frontend maps
	// to '—'), not as a confident 0kt dead calm. So a window that itself has
	// no samples keeps its -1 sentinel untouched, and the monotonic
	// (longer-window-never-less-than-shorter) clamp only starts applying
	// once a shorter window has actually produced a real (>=0) value -
	// previousTrue itself starts at -1 (nothing to enforce yet) and is only
	// ever updated from a real value, never from an untouched sentinel.
	//
	// This ladder's own "1h" entry IS the Current Conditions tile's "obs"
	// marker too (ADR 0129), not a separate maxTrueWindKts1h field anymore:
	// that field used to read a second, independent buffer
	// (trueWindSpeedHistory, raw m/s, gated by the heavy-weather trend's own
	// staleness check) behind a DIFFERENT freshness gate than this ladder's
	// trueWindGustHistory (already-knots, gated on state.WindSpeedTrueKts
	// >= 0), so the two tiles could show two different "last hour" true wind
	// figures for the same boat at the same moment - a code-review finding,
	// 2026-09-25; see docs/adr/0130's amendment. The frontend now reads
	// max_gust_true_kts['1h'] directly for both.
	maxGustTrueKts := computeMaxGustTrueKtsFor(gustWindowLadder)
	previousTrue := -1.0
	for _, window := range gustWindowLadder {
		value := maxGustTrueKts[window]
		if value >= 0 {
			if value < previousTrue {
				value = previousTrue
			}
			previousTrue = value
		}
		maxGustTrueKts[window] = value
	}

	vesselPrefix := loadBoatVesselPrefix(settingsPath)
	if vesselPrefix == "" {
		vesselPrefix = "M/V"
	}

	// No masking fallback: an unpublished LOA must reach the frontend as
	// JSON null, not the internal lookupNumber -1 sentinel, which could be
	// mistaken for a real (negative) length.
	var lengthOverallM any
	if state.LengthOverallM > 0 {
		lengthOverallM = state.LengthOverallM
	}

	// Same no-masking-fallback treatment for draft (ADR 0135): an
	// unpublished draft must reach the frontend as JSON null, not the
	// internal -1 sentinel.
	var draftM any
	if state.DraftM > 0 {
		draftM = state.DraftM
	}

	return map[string]any{
		"name":                           state.Name,
		"vessel_prefix":                  vesselPrefix,
		"status":                         state.Status,
		"datetime":                       state.Datetime.Format(time.RFC3339),
		"timezone":                       vesselLocalTimezoneName(state.Longitude),
		"depth":                          state.Depth,
		"depth_last_update_age_s":        state.DepthLastUpdateAge,
		"length_overall_m":               lengthOverallM,
		"draft_m":                        draftM,
		"current_drift_kts":              state.CurrentDriftKts,
		"current_set_deg":                state.CurrentSetDeg,
		"current_drift_impact_kts":       state.CurrentDriftImpactKts,
		"latitude":                       state.Latitude,
		"longitude":                      state.Longitude,
		"position_last_update_age_s":     state.PositionLastUpdateAge,
		"gnss_quality_indicator":         state.GNSSQualityIndicator,
		"gnss_hdop":                      state.GNSSHDOP,
		"gnss_satellites":                state.GNSSSatellites,
		"gnss_validation_state":          state.GNSSValidationState,
		"gnss_validation_reason":         state.GNSSValidationReason,
		"gnss_critical_alert":            state.GNSSCriticalAlert,
		"heading_true":                   state.HeadingTrue,
		"speed_over_ground_kts":          state.SpeedOverGroundKts,
		"wind_speed_apparent_kts":        state.WindSpeedApparentKts,
		"wind_angle_apparent_deg":        state.WindAngleApparentDeg,
		"wind_side":                      state.WindSide,
		"wind_angle_relative_deg":        state.WindAngleRelativeDeg,
		"wind_speed_true_kts":            state.WindSpeedTrueKts,
		"wind_angle_true_deg":            state.WindAngleTrueDeg,
		"wind_side_true":                 state.WindSideTrue,
		"wind_angle_true_relative_deg":   state.WindAngleTrueRelativeDeg,
		"wind_direction_true_deg":        state.WindDirectionTrueDeg,
		"wind_last_update_age_s":         state.WindLastUpdateAge,
		"max_gust_kts":                   maxGustKts,
		"max_gust_true_kts":              maxGustTrueKts,
		"generator_state":                state.GeneratorState,
		"generator_manual_start":         state.GeneratorManualStart,
		"generator_manual_start_timer":   state.GeneratorManualStartTimer,
		"generator_running_by_condition": state.GeneratorRunningByCondition,
		"generator_runtime":              state.GeneratorRuntime,
		"engines_running":                state.EnginesRunning,
		"source":                         source,
	}
}

func vesselState(c echo.Context) error {
	return c.JSON(http.StatusOK, buildVesselStatePayload())
}

// vesselStateRoutes returns the /api routes for the vessel state payload and depth trend.
// Each row carries its own auth tier (docs/adr/0177).
func vesselStateRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/vessel-state", tierRead, vesselState},
		{http.MethodGet, "/api/depth-trend", tierRead, depthTrend},
	}
}
