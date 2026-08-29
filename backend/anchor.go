package main

import (
	"encoding/json"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

const defaultAnchorWatchRadiusMeters = 20.0
const maxTrailPoints = 1000

type anchorWatchData struct {
	Lat              float64   `json:"lat"`
	Lon              float64   `json:"lon"`
	RadiusMeters     float64   `json:"radius_meters"`
	RodeDeployedM    float64   `json:"rode_deployed_m"`
	SeaState         string    `json:"sea_state"`
	SeabedType       string    `json:"seabed_type"`
	SetAt            time.Time `json:"set_at"`
	BowOffsetM       float64   `json:"bow_offset_m"` // d actually applied
	BowOffsetApplied bool      `json:"bow_offset_applied"`
	BowOffsetReason  string    `json:"bow_offset_reason"`  // why not, when not applied
	HeadingAtSetDeg  float64   `json:"heading_at_set_deg"` // -1 when not read
	// A planning depth only means something paired with the tide height at
	// the moment it was read. Feed the rode planner a depth from one state
	// of the tide and a tide reading from another and the answer is wrong,
	// in the unsafe direction (too little chain), by however much the tide
	// has moved in between. So these are one pair, never two independent
	// numbers: PlanningDepthM/PlanningTideHeightFt is seeded from the live
	// sounder at the instant the anchor goes down, and the operator can
	// freely edit it afterwards - editing overwrites the pair in place,
	// there is no separate capture underneath it to fall back to. -1 is
	// "not captured", matching HeadingAtSetDeg above. Read predicate is >0
	// for depth and >=0 for tide (never != -1), so a pre-existing
	// anchor_watch.json with these fields absent decodes to 0 and reads
	// correctly as "not captured" - no migration.
	PlanningDepthM       float64 `json:"planning_depth_m"`
	PlanningTideHeightFt float64 `json:"planning_tide_height_ft"`
	// PlaceName is resolved once from the anchor position (place_name.go's
	// resolveAndPinAnchorWatchPlaceName), then pinned for the life of this
	// watch so it stops drifting as the boat swings - see docs/adr/0056.
	// Empty until resolution succeeds; the regular poll tick retries until
	// it does.
	PlaceName string `json:"place_name"`
}

type trailPoint struct {
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	Timestamp time.Time `json:"timestamp"`
}

type vesselTrail struct {
	points []*trailPoint
	index  int  // current write position in ring buffer
	full   bool // whether the buffer has wrapped around
}

func newVesselTrail() *vesselTrail {
	return &vesselTrail{
		points: make([]*trailPoint, maxTrailPoints),
		index:  0,
		full:   false,
	}
}

func (vt *vesselTrail) addPoint(lat, lon float64) {
	vt.addPointWithTimestamp(lat, lon, time.Now().UTC())
}

func (vt *vesselTrail) addPointWithTimestamp(lat, lon float64, ts time.Time) {
	vt.points[vt.index] = &trailPoint{
		Lat:       lat,
		Lon:       lon,
		Timestamp: ts,
	}
	vt.index = (vt.index + 1) % maxTrailPoints
	if vt.index == 0 {
		vt.full = true
	}
}

func (vt *vesselTrail) orderedPoints() []*trailPoint {
	if !vt.full && vt.index == 0 {
		return nil
	}

	result := make([]*trailPoint, 0, maxTrailPoints)
	if vt.full {
		start := vt.index
		for i := 0; i < maxTrailPoints; i++ {
			if p := vt.points[(start+i)%maxTrailPoints]; p != nil {
				result = append(result, p)
			}
		}
		return result
	}

	for i := 0; i < vt.index; i++ {
		if vt.points[i] != nil {
			result = append(result, vt.points[i])
		}
	}
	return result
}

// pointsSince returns all trail points after the given timestamp, in chronological order.
func (vt *vesselTrail) pointsSince(since time.Time) []*trailPoint {
	if !vt.full && vt.index == 0 {
		// No points yet
		return nil
	}

	var result []*trailPoint

	// If buffer is full, we have all maxTrailPoints entries
	if vt.full {
		// Start from index (oldest point) and go around
		start := vt.index
		for i := 0; i < maxTrailPoints; i++ {
			p := vt.points[(start+i)%maxTrailPoints]
			if p != nil && p.Timestamp.After(since) {
				result = append(result, p)
			}
		}
	} else {
		// Buffer not full yet, points are from 0 to index-1
		for i := 0; i < vt.index; i++ {
			if vt.points[i] != nil && vt.points[i].Timestamp.After(since) {
				result = append(result, vt.points[i])
			}
		}
	}

	return result
}

var (
	anchorWatchMu               sync.RWMutex
	anchorWatchState            *anchorWatchData
	lastAnchorWatchRadiusMeters = defaultAnchorWatchRadiusMeters

	trailMu   sync.RWMutex
	selfTrail *vesselTrail
)

func anchorWatchFilePath() string {
	return cacheFilePath("ANCHOR_WATCH_FILE", "cache/anchor_watch.json")
}

// recordSelfTrailPoint appends to post-anchor ring buffer only when active.
func recordSelfTrailPoint(lat, lon float64) {
	anchorWatchMu.RLock()
	active := anchorWatchState != nil
	anchorWatchMu.RUnlock()
	if !active {
		return
	}

	trailMu.Lock()
	defer trailMu.Unlock()
	if selfTrail != nil {
		selfTrail.addPoint(lat, lon)
	}
}

// getSelfTrailSince returns post-anchor ring-buffer points after the given timestamp.
func getSelfTrailSince(since time.Time) []*trailPoint {
	trailMu.RLock()
	defer trailMu.RUnlock()

	if selfTrail == nil {
		return nil
	}
	return selfTrail.pointsSince(since)
}

func loadAnchorWatch() {
	path := anchorWatchFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		// No file yet — start with no watch active.
		return
	}

	var loaded anchorWatchData
	if err := json.Unmarshal(data, &loaded); err != nil {
		return
	}

	anchorWatchMu.Lock()
	anchorWatchState = &loaded
	if loaded.RadiusMeters > 0 {
		lastAnchorWatchRadiusMeters = loaded.RadiusMeters
	}
	anchorWatchMu.Unlock()
}

func saveAnchorWatch(aw *anchorWatchData) error {
	return writeJSONFileAtomic(anchorWatchFilePath(), aw)
}

// GET /api/anchor-watch
func getAnchorWatch(c echo.Context) error {
	anchorWatchMu.RLock()
	state := anchorWatchState
	anchorWatchMu.RUnlock()

	if state == nil {
		return c.JSON(http.StatusOK, map[string]any{"active": false})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"active":                  true,
		"lat":                     state.Lat,
		"lon":                     state.Lon,
		"radius_meters":           state.RadiusMeters,
		"rode_deployed_m":         state.RodeDeployedM,
		"sea_state":               state.SeaState,
		"seabed_type":             state.SeabedType,
		"set_at":                  state.SetAt.Format(time.RFC3339),
		"bow_offset_m":            state.BowOffsetM,
		"bow_offset_applied":      state.BowOffsetApplied,
		"bow_offset_reason":       state.BowOffsetReason,
		"heading_at_set_deg":      state.HeadingAtSetDeg,
		"planning_depth_m":        state.PlanningDepthM,
		"planning_tide_height_ft": state.PlanningTideHeightFt,
		"place_name":              state.PlaceName,
	})
}

// POST /api/anchor-watch
func setAnchorWatch(c echo.Context) error {
	var body struct {
		Lat                  float64  `json:"lat"`
		Lon                  float64  `json:"lon"`
		RadiusMeters         *float64 `json:"radius_meters"`
		ApplyBowOffset       *bool    `json:"apply_bow_offset"`
		PlanningDepthM       *float64 `json:"planning_depth_m"`
		PlanningTideHeightFt *float64 `json:"planning_tide_height_ft"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if body.Lat < -90 || body.Lat > 90 || body.Lon < -180 || body.Lon > 180 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "lat/lon out of range"})
	}
	if body.PlanningDepthM != nil && *body.PlanningDepthM != -1 && *body.PlanningDepthM <= 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "planning_depth_m must be positive or -1"})
	}
	if body.PlanningTideHeightFt != nil && *body.PlanningTideHeightFt < -1 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "planning_tide_height_ft must be -1 or greater"})
	}

	radius := defaultAnchorWatchRadiusMeters
	anchorWatchMu.RLock()
	current := anchorWatchState
	previousRadius := lastAnchorWatchRadiusMeters
	anchorWatchMu.RUnlock()

	if body.RadiusMeters != nil && *body.RadiusMeters > 0 {
		radius = *body.RadiusMeters
	} else if current != nil && current.RadiusMeters > 0 {
		radius = current.RadiusMeters
	} else if previousRadius > 0 {
		radius = previousRadius
	}

	// POST is a full replace and a reposition drag goes through it, so
	// everything the active watch holds that the body is silent about is
	// carried forward here. Dragging the marker corrects where you believe
	// the hook lies; it does not begin a new anchorage, and it says nothing
	// about the chain, the bottom or the water. current is nil on a genuine
	// drop (Raise DELETEs first), so a new anchorage starts fresh on every
	// one of these.
	setAt := time.Now().UTC()
	rodeDeployedM := 0.0
	seaState := "calm"
	seabedType := "sand"
	planningDepthM := -1.0
	planningTideHeightFt := -1.0
	if current != nil {
		// set_at is the session's identity rather than a "last written"
		// stamp. Clients compare it against the session their map view was
		// last centred for (ADR 0064), so a stamp that moved on every drag
		// would swing every client's chart mid-anchorage.
		setAt = current.SetAt

		// Rode, sea state and seabed are the operator's own entries and
		// belong to the anchorage, not to the point. Resetting them here
		// silently emptied the Rode Planner's inputs mid-anchorage, with
		// nothing on screen to say the drag had done it. Copied straight
		// through the way patchAnchorWatch rebuilds them: PATCH is their
		// only writer and it validates the enums, so whatever the record
		// holds is what the operator chose.
		rodeDeployedM = current.RodeDeployedM
		seaState = current.SeaState
		seabedType = current.SeabedType

		// The depth pair is the reading taken at the drop (ADR 0063): a
		// reposition corrects where you think the anchor lies, not how deep
		// the water was when it went down.
		if current.PlanningDepthM > 0 {
			planningDepthM = current.PlanningDepthM
		}
		if current.PlanningTideHeightFt >= 0 {
			planningTideHeightFt = current.PlanningTideHeightFt
		}
	}
	if body.PlanningDepthM != nil {
		planningDepthM = *body.PlanningDepthM
	}
	if body.PlanningTideHeightFt != nil {
		planningTideHeightFt = *body.PlanningTideHeightFt
	}

	lat, lon := body.Lat, body.Lon
	bowOffsetM := 0.0
	bowOffsetApplied := false
	bowOffsetReason := ""
	headingAtSetDeg := -1.0

	// Only the live-GPS "set anchor here" path requests the correction; a
	// user dragging the anchor marker on the map (updatePosition) sends a
	// point that's already meant to be the anchor, and must never be shoved
	// forward by the offset again.
	if body.ApplyBowOffset != nil && *body.ApplyBowOffset {
		settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
		settings, err := readSettings(settingsPath)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read settings"})
		}
		gpsFromBowM := buildSettingsPayload(settings).Anchor.GPSFromBowM

		if gpsFromBowM <= 0 {
			bowOffsetReason = "gps_from_bow_m not configured"
		} else {
			state, err := fetchSignalKVesselState()
			if err != nil || state.HeadingTrue < 0 {
				// Never silently assume d=0 and never substitute COG for
				// heading — still set the anchor at the raw fix and say why
				// the correction didn't apply.
				bowOffsetReason = "heading unavailable"
			} else {
				headingAtSetDeg = state.HeadingTrue
				lat, lon = destinationPoint(body.Lat, body.Lon, state.HeadingTrue*math.Pi/180, gpsFromBowM)
				bowOffsetM = gpsFromBowM
				bowOffsetApplied = true
			}
		}
	}

	aw := &anchorWatchData{
		Lat:                  lat,
		Lon:                  lon,
		RadiusMeters:         radius,
		RodeDeployedM:        rodeDeployedM,
		SeaState:             seaState,
		SeabedType:           seabedType,
		SetAt:                setAt,
		BowOffsetM:           bowOffsetM,
		BowOffsetApplied:     bowOffsetApplied,
		BowOffsetReason:      bowOffsetReason,
		HeadingAtSetDeg:      headingAtSetDeg,
		PlanningDepthM:       planningDepthM,
		PlanningTideHeightFt: planningTideHeightFt,
	}

	if err := saveAnchorWatch(aw); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist"})
	}

	anchorWatchMu.Lock()
	anchorWatchState = aw
	lastAnchorWatchRadiusMeters = aw.RadiusMeters
	anchorWatchMu.Unlock()

	// Reset post-anchor ring buffer.
	trailMu.Lock()
	selfTrail = newVesselTrail()
	trailMu.Unlock()

	// Resolve the anchorage's place name once, in the background, so the
	// response above isn't held up by an Overpass round trip. A failure
	// logs explicitly (inside resolveAndCachePlaceName) and leaves
	// PlaceName empty; the regular poll tick (updateTickPlaceName) retries
	// on every subsequent tick until it succeeds, then the name is pinned
	// for the rest of this watch. See docs/adr/0056.
	startPlaceNameResolve(func() {
		resolveAndPinAnchorWatchPlaceName(aw, aw.Lat, aw.Lon)
	})

	return c.JSON(http.StatusOK, map[string]any{
		"active":                  true,
		"lat":                     aw.Lat,
		"lon":                     aw.Lon,
		"radius_meters":           aw.RadiusMeters,
		"rode_deployed_m":         aw.RodeDeployedM,
		"sea_state":               aw.SeaState,
		"seabed_type":             aw.SeabedType,
		"set_at":                  aw.SetAt.Format(time.RFC3339),
		"bow_offset_m":            aw.BowOffsetM,
		"bow_offset_applied":      aw.BowOffsetApplied,
		"bow_offset_reason":       aw.BowOffsetReason,
		"heading_at_set_deg":      aw.HeadingAtSetDeg,
		"planning_depth_m":        aw.PlanningDepthM,
		"planning_tide_height_ft": aw.PlanningTideHeightFt,
		"place_name":              aw.PlaceName,
	})
}

// PATCH /api/anchor-watch — update radius, depth, and other watch settings
//
// This handler holds anchorWatchMu.Lock() for its entire body rather than
// the more permissive read-then-write pattern, because it does a
// read-modify-write: `updated` is rebuilt field-by-field from `current`
// (see the comment on the PlanningDepthM copy below) and only afterward
// assigned back. Reading under RLock and only re-taking the lock to write
// leaves a gap in between where a second, concurrent PATCH can read the
// same stale `current` and then overwrite the first PATCH's change with its
// own full rebuild — a lost update. This is no longer just theoretical:
// the Rode Planner debounces depth on its own timer alongside the existing
// rode/sea-state/seabed timer, so changing sea state and depth inside the
// same 800ms window fires two overlapping PATCHes. saveAnchorWatch does not
// itself take anchorWatchMu (it only calls writeJSONFileAtomic), so holding
// the lock across the save call below is not re-entrant and cannot
// deadlock. Do not narrow this back to RLock+Lock.
func patchAnchorWatch(c echo.Context) error {
	anchorWatchMu.Lock()
	defer anchorWatchMu.Unlock()

	current := anchorWatchState
	if current == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "no active anchor watch"})
	}

	var body struct {
		RadiusMeters         *float64 `json:"radius_meters"`
		RodeDeployedM        *float64 `json:"rode_deployed_m"`
		SeaState             *string  `json:"sea_state"`
		SeabedType           *string  `json:"seabed_type"`
		PlanningDepthM       *float64 `json:"planning_depth_m"`
		PlanningTideHeightFt *float64 `json:"planning_tide_height_ft"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if body.RadiusMeters == nil && body.RodeDeployedM == nil && body.SeaState == nil && body.SeabedType == nil &&
		body.PlanningDepthM == nil && body.PlanningTideHeightFt == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no patch fields provided"})
	}
	if body.RadiusMeters != nil && *body.RadiusMeters <= 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "radius_meters must be positive"})
	}
	if body.RodeDeployedM != nil && *body.RodeDeployedM < 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "rode_deployed_m must be non-negative"})
	}
	if body.SeaState != nil {
		switch *body.SeaState {
		case "calm", "choppy", "rough", "storm":
		default:
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid sea_state"})
		}
	}
	if body.SeabedType != nil {
		switch *body.SeabedType {
		case "sand", "mud", "rock", "grass":
		default:
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid seabed_type"})
		}
	}
	if body.PlanningDepthM != nil && *body.PlanningDepthM != -1 && *body.PlanningDepthM <= 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "planning_depth_m must be positive or -1"})
	}
	if body.PlanningTideHeightFt != nil && *body.PlanningTideHeightFt < -1 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "planning_tide_height_ft must be -1 or greater"})
	}
	// The pair rule, enforced at the boundary: a planning depth is only
	// meaningful with the tide height at the instant it was read, so a
	// positive planning_depth_m must arrive with its tide stamp in the same
	// request. -1 (clearing the pair) is exempt — see the apply step below,
	// which clears the stamp too.
	if body.PlanningDepthM != nil && *body.PlanningDepthM > 0 && body.PlanningTideHeightFt == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "planning_tide_height_ft is required when setting planning_depth_m"})
	}
	// The other half of the pair rule: a tide stamp with no depth in the
	// same body would otherwise fall through to the apply step and re-stamp
	// the EXISTING planning depth with a tide height read at a different
	// instant — the same datum-mixing error above, arriving from the other
	// direction. There is no clear case to exempt here, unlike above:
	// clearing the stamp only ever happens by clearing the depth.
	if body.PlanningTideHeightFt != nil && body.PlanningDepthM == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "planning_depth_m is required when setting planning_tide_height_ft"})
	}

	updated := &anchorWatchData{
		Lat:              current.Lat,
		Lon:              current.Lon,
		RadiusMeters:     current.RadiusMeters,
		RodeDeployedM:    current.RodeDeployedM,
		SeaState:         current.SeaState,
		SeabedType:       current.SeabedType,
		SetAt:            current.SetAt,
		BowOffsetM:       current.BowOffsetM,
		BowOffsetApplied: current.BowOffsetApplied,
		BowOffsetReason:  current.BowOffsetReason,
		HeadingAtSetDeg:  current.HeadingAtSetDeg,
		// planning_depth_m/planning_tide_height_ft still have to be copied
		// here even though they're patchable below, or a PATCH for
		// something unrelated (sea state, say) would silently zero them out
		// and a working plan would go dark.
		PlanningDepthM:       current.PlanningDepthM,
		PlanningTideHeightFt: current.PlanningTideHeightFt,
		PlaceName:            current.PlaceName,
	}

	if body.RadiusMeters != nil {
		updated.RadiusMeters = *body.RadiusMeters
	}
	if body.RodeDeployedM != nil {
		updated.RodeDeployedM = *body.RodeDeployedM
	}
	if body.SeaState != nil {
		updated.SeaState = *body.SeaState
	}
	if body.SeabedType != nil {
		updated.SeabedType = *body.SeabedType
	}
	if body.PlanningDepthM != nil {
		updated.PlanningDepthM = *body.PlanningDepthM
		if *body.PlanningDepthM == -1 {
			// Clearing the depth clears its stamp with it — a half-cleared
			// pair is not a thing.
			updated.PlanningTideHeightFt = -1
		} else if body.PlanningTideHeightFt != nil {
			updated.PlanningTideHeightFt = *body.PlanningTideHeightFt
		}
	}
	// No else branch: the validation above rejects a lone
	// planning_tide_height_ft, so the only ways to write the stamp are
	// "with its depth" (above) or "cleared alongside its depth" (the -1
	// case above too).

	if err := saveAnchorWatch(updated); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist"})
	}

	anchorWatchState = updated
	if updated.RadiusMeters > 0 {
		lastAnchorWatchRadiusMeters = updated.RadiusMeters
	}

	return c.JSON(http.StatusOK, map[string]any{
		"active":                  true,
		"lat":                     updated.Lat,
		"lon":                     updated.Lon,
		"radius_meters":           updated.RadiusMeters,
		"rode_deployed_m":         updated.RodeDeployedM,
		"sea_state":               updated.SeaState,
		"seabed_type":             updated.SeabedType,
		"set_at":                  updated.SetAt.Format(time.RFC3339),
		"bow_offset_m":            updated.BowOffsetM,
		"bow_offset_applied":      updated.BowOffsetApplied,
		"bow_offset_reason":       updated.BowOffsetReason,
		"heading_at_set_deg":      updated.HeadingAtSetDeg,
		"planning_depth_m":        updated.PlanningDepthM,
		"planning_tide_height_ft": updated.PlanningTideHeightFt,
		"place_name":              updated.PlaceName,
	})
}

// DELETE /api/anchor-watch
func deleteAnchorWatch(c echo.Context) error {
	anchorWatchMu.Lock()
	anchorWatchState = nil
	anchorWatchMu.Unlock()

	trailMu.Lock()
	selfTrail = nil
	trailMu.Unlock()

	// Placemarks are bound to the anchoring session, so they end with it.
	clearPlacemarks()

	_ = os.Remove(anchorWatchFilePath())

	return c.JSON(http.StatusOK, map[string]any{"active": false})
}

// GET /api/anchor-watch/trails/self?since=<RFC3339-timestamp>
// Returns post-anchor ring-buffer positions since the given timestamp.
func getSelfTrailHandler(c echo.Context) error {
	sinceStr := c.QueryParam("since")
	var since time.Time
	if sinceStr != "" {
		var err error
		since, err = time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid since timestamp"})
		}
	}

	points := getSelfTrailSince(since)
	return c.JSON(http.StatusOK, map[string]any{
		"points": points,
	})
}

// GET /api/anchor-watch/trails/ais/:id?since=<RFC3339-timestamp>
func getAISTrailHandler(c echo.Context) error {
	id := c.Param("id")
	trails := fetchSignalKAISTrails(getEnv("SETTINGS_FILE", "../settings.yaml"))
	points := trails[strings.ToUpper(strings.TrimSpace(id))]
	return c.JSON(http.StatusOK, map[string]any{
		"points": points,
	})
}

// GET /api/anchor-watch/trails/ais?since=<RFC3339-timestamp>
// Returns all AIS trails with new points since the given timestamp.
func getAllAISTrailsHandler(c echo.Context) error {
	trails := fetchSignalKAISTrails(getEnv("SETTINGS_FILE", "../settings.yaml"))
	return c.JSON(http.StatusOK, map[string]any{
		"trails": trails,
	})
}
