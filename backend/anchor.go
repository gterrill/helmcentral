package main

import (
	"encoding/json"
	"fmt"
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

// Serialize operator lifecycle writes, including their upstream confirmation.
// Never hold anchorWatchMu over network I/O: telemetry must keep flowing.
var anchorLifecycleMu sync.Mutex

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
	// Truncated to millisecond precision: the client parses timestamps
	// through JS Date (millisecond precision) and sends `since` back via
	// toISOString(), which is also millisecond precision. Storing the raw
	// nanosecond-precision time.Now() means the client can never send a
	// `since` that matches the stored value exactly, so pointsSince's
	// After comparison re-sends the newest point on every poll.
	vt.addPointWithTimestamp(lat, lon, time.Now().UTC().Truncate(time.Millisecond))
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
	return cacheFilePath("ANCHOR_WATCH_FILE", "data/anchor_watch.json")
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

// loadAnchorWatch restores the watch on startup. A missing file is the
// ordinary "fresh install, nothing ever dropped" case and is not an error, and
// so is a zero-length one — the shape an interrupted atomic write (create/
// truncate landed, the bytes didn't) leaves behind, matching loadAlarmRules'
// own `if len(data) > 0` treatment of the same situation (alarm_rules.go).
//
// A file that exists, is non-empty, and either fails to parse or parses into
// something that isn't a real watch — `{}`, a bare `null` (both decode to an
// all-zero struct with no unmarshal error), a position sitting at exactly
// 0,0, or a radius that is zero or negative and so can never trip — is a
// different case entirely. The operator's anchor alarm going silently absent,
// or silently installed at 0,0 with an alarm that can never fire, would both
// be far worse than a startup failure that says so, so all of these are
// surfaced to the caller rather than swallowed, matching loadAlarmRules's
// shape for its own corrupt-file case. Every returned error names path, so
// whatever reports it (main.go, GET /api/anchor-watch) doesn't have to guess
// which file was the problem.
func loadAnchorWatch() error {
	path := anchorWatchFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No file yet — start with no watch active.
			return nil
		}
		return fmt.Errorf("reading anchor watch state (%s): %w", path, err)
	}
	if len(data) == 0 {
		// An interrupted write, not a watch that was ever dropped.
		return nil
	}

	var loaded anchorWatchData
	if err := json.Unmarshal(data, &loaded); err != nil {
		return fmt.Errorf("parsing anchor watch state (%s): %w", path, err)
	}
	if err := loaded.validateLoaded(); err != nil {
		return fmt.Errorf("invalid anchor watch state (%s): %w", path, err)
	}

	anchorWatchMu.Lock()
	anchorWatchState = &loaded
	if loaded.RadiusMeters > 0 {
		lastAnchorWatchRadiusMeters = loaded.RadiusMeters
	}
	anchorWatchMu.Unlock()
	return nil
}

// validateLoaded catches a file that parsed without error but isn't a
// real watch. json.Unmarshal treats both `{}` and a bare `null` as "leave
// the target at its zero value, no error" — so without this check either one
// would silently become an active watch centred on 0,0 (Gulf of Guinea, "null
// island") with whatever radius happened to be in the file.
func (a anchorWatchData) validateLoaded() error {
	if a.Lat == 0 && a.Lon == 0 {
		return fmt.Errorf("no anchor position recorded (lat/lon both 0)")
	}
	if a.RadiusMeters <= 0 {
		return fmt.Errorf("radius_meters must be positive, got %v", a.RadiusMeters)
	}
	return nil
}

func saveAnchorWatch(aw *anchorWatchData) error {
	return writeJSONFileAtomic(anchorWatchFilePath(), aw)
}

// GET /api/anchor-watch
//
// Also carries last_auto_raise (anchor_auto_raise.go, ADR 0099) when the
// server has ever auto-raised a watch: every client already polls this
// endpoint, so it is the least invasive way for each of them to learn a
// raise happened without them having decided it happened.
//
// Also carries error when the persisted anchor_watch.json could not be
// loaded at startup (recordAnchorWatchLoadFailure) — naming the file path and
// the parse/validation error, never an invented or empty watch in its place.
// state is always nil in that case (loadAnchorWatch never installs one on
// failure), so active is always false alongside it.
func getAnchorWatch(c echo.Context) error {
	anchorWatchMu.RLock()
	state := anchorWatchState
	loadErr := anchorWatchLoadErr
	anchorWatchMu.RUnlock()

	if state == nil {
		resp := map[string]any{"active": false}
		if loadErr != "" {
			resp["error"] = loadErr
		}
		return c.JSON(http.StatusOK, withLastAutoRaise(resp))
	}

	return c.JSON(http.StatusOK, withLastAutoRaise(map[string]any{
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
	}))
}

// POST /api/anchor-watch
func setAnchorWatch(c echo.Context) error {
	anchorLifecycleMu.Lock()
	defer anchorLifecycleMu.Unlock()
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
	// reposition (this same POST handler, called again with an active watch
	// already set -- see current != nil above) sends a point that's already
	// meant to be the anchor, and must never be shoved forward by the offset
	// again. No frontend gesture currently drives that reposition path (ADR
	// 0133 removed the map's own drag-to-reposition in Phase 1; its Adjust
	// sheet, the planned Phase 3 replacement, has not landed), but the
	// backend contract stays in place for it to resume against unchanged.
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

	// saveAnchorWatch above just overwrote anchor_watch.json atomically,
	// whatever it held before — a bad file from a previous corrupt-state
	// error is the operator's recovery path, not something this drop needs to
	// check for first. Retract that warning now that a good file and a good
	// in-memory state both exist.
	clearAnchorWatchLoadFailure()

	// Reset post-anchor ring buffer.
	trailMu.Lock()
	selfTrail = newVesselTrail()
	trailMu.Unlock()

	// Keep the local safety watch if the upstream publish fails, but do not
	// report a successful drop/reposition. The operator can retry explicitly.
	if err := publishSignalKAnchorPosition(aw); err != nil {
		c.Logger().Errorf("anchor position publish failed: %v", err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "Anchor watch saved locally, but SignalK did not confirm the anchor position. Retry Drop or reposition to synchronize."})
	}

	// Resolve the anchorage's place name once, in the background, so the
	// response above isn't held up by a place-names provider round trip. A failure
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

// PATCH /api/anchor-watch — update radius, position, and other watch settings
//
// lat/lon are optional but must arrive together (both or neither): position
// is one atomic write alongside whatever else the body patches, never two
// fields nudged independently by accident. When present, this follows
// setAnchorWatch's own reposition path (ADR 0133) — publish
// navigation.anchor.position to SignalK, reset the post-anchor self trail —
// with one deliberate difference: a PATCH that changes position persists
// NOTHING and reports 502 if the publish fails, rather than keeping the
// local watch as a safety net the way POST/drop does. POST's "keep the local
// watch anyway" exists for the emergency case (SignalK is down but the boat
// still needs *some* local safety watch); a PATCH position change is the
// deliberate Adjust surface, so there's no reason to accept a position
// upstream never confirmed.
//
// Locking: this handler holds anchorLifecycleMu for its entire body, which
// rules out a second concurrent PATCH/POST/DELETE (see that mutex's own doc
// comment). It does NOT rule out the place-name resolver's background
// goroutine (place_name.go's resolveAndPinAnchorWatchPlaceName): that is
// gated only by its own single-flight guard, takes anchorWatchMu directly,
// and holds no lifecycle lock at all. `updated` is read once, early, from a
// `current` snapshot taken under an initial anchorWatchMu.Lock(); anchorWatchMu
// is then always released before persisting `updated` and re-acquired only
// to write it back — for a position change, specifically so the SignalK
// publish's confirmation poll (which can run for seconds) never holds
// anchorWatchMu and stalls every GET and trail-recording tick for that long;
// a field-only PATCH takes the identical release/re-acquire, just with
// nothing running in the gap. That gap — real in both cases, just far
// shorter in the field-only one — is exactly where the place-name resolver
// can run and pin a freshly resolved name onto anchorWatchState before this
// handler re-acquires the lock (code-review finding: this comment used to
// claim no gap here was reachable by anything else, which holds for a
// second PATCH/POST/DELETE but not for this resolver). The persist step
// below re-reads the live anchorWatchState.PlaceName immediately before
// writing `updated`, so a name pinned mid-gap survives instead of being
// overwritten by the stale snapshot `updated` was built from.
//
// The no-position-change path's own read-modify-write is otherwise unchanged
// from before this comment: it rebuilds `updated` field-by-field from
// `current` (see the comment on the PlanningDepthM copy below) and only
// afterward assigns back, entirely under one anchorWatchMu.Lock(). Reading
// under RLock and only re-taking the lock to write would leave a gap where a
// second, concurrent PATCH could read the same stale `current` and overwrite
// the first PATCH's change with its own full rebuild — a lost update. This
// is no longer just theoretical: the Rode Planner debounces depth on its own
// timer alongside the existing rode/sea-state/seabed timer, so changing sea
// state and depth inside the same 800ms window fires two overlapping
// PATCHes. saveAnchorWatch does not itself take anchorWatchMu (it only calls
// writeJSONFileAtomic), so holding the lock across the save call is not
// re-entrant and cannot deadlock. Do not narrow either path back to
// RLock+Lock.
func patchAnchorWatch(c echo.Context) error {
	anchorLifecycleMu.Lock()
	defer anchorLifecycleMu.Unlock()

	anchorWatchMu.RLock()
	noActiveWatch := anchorWatchState == nil
	anchorWatchMu.RUnlock()
	if noActiveWatch {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "no active anchor watch"})
	}

	var body struct {
		Lat                  *float64 `json:"lat"`
		Lon                  *float64 `json:"lon"`
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
	if body.Lat == nil && body.Lon == nil && body.RadiusMeters == nil && body.RodeDeployedM == nil && body.SeaState == nil && body.SeabedType == nil &&
		body.PlanningDepthM == nil && body.PlanningTideHeightFt == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no patch fields provided"})
	}
	if (body.Lat == nil) != (body.Lon == nil) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "lat and lon must be provided together"})
	}
	if body.Lat != nil && (*body.Lat < -90 || *body.Lat > 90 || *body.Lon < -180 || *body.Lon > 180) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "lat/lon out of range"})
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

	anchorWatchMu.Lock()
	current := anchorWatchState
	if current == nil {
		// The watch was raised between the check above and here — still
		// possible in principle (this handler serialises against every other
		// lifecycle write via anchorLifecycleMu, but not against itself
		// re-entering), so re-check rather than trust the earlier snapshot.
		anchorWatchMu.Unlock()
		return c.JSON(http.StatusNotFound, map[string]string{"error": "no active anchor watch"})
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

	if body.Lat != nil {
		updated.Lat = *body.Lat
		updated.Lon = *body.Lon
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

	positionChanged := body.Lat != nil
	anchorWatchMu.Unlock()

	// A position change publishes to SignalK outside anchorWatchMu, exactly
	// like setAnchorWatch's own reposition path — the confirmation poll can
	// run for several seconds, and holding the lock across it would stall
	// every GET and every trail-recording tick for that long. See the
	// locking note in this function's doc comment for why this cannot race a
	// concurrent PATCH/POST/DELETE.
	//
	// Unlike setAnchorWatch, a failed publish here persists NOTHING: this
	// PATCH is the deliberate Adjust write, not the "SignalK is down, keep
	// the local safety watch anyway" case POST/drop exists for. `current`
	// (and the file on disk) are simply never touched on this path.
	if positionChanged {
		if err := publishSignalKAnchorPosition(updated); err != nil {
			c.Logger().Errorf("anchor position publish failed: %v", err)
			return c.JSON(http.StatusBadGateway, map[string]string{"error": "SignalK did not confirm the new anchor position. The watch was not changed; retry Adjust."})
		}
	}

	anchorWatchMu.Lock()
	// The place-name resolver (place_name.go's resolveAndPinAnchorWatchPlaceName)
	// can run in the gap above (see this function's own doc comment,
	// code-review finding) and pin a freshly resolved name onto
	// anchorWatchState. `updated` was built from a `current` snapshot taken
	// before that gap opened, so persisting it unmodified would silently
	// stomp a name pinned in the meantime back to whatever `current.PlaceName`
	// already was. Carry the live value forward instead of the stale one.
	if live := anchorWatchState; live != nil {
		updated.PlaceName = live.PlaceName
	}
	if err := saveAnchorWatch(updated); err != nil {
		anchorWatchMu.Unlock()
		if positionChanged {
			// SignalK already has the new position at this point (the publish
			// above succeeded) — the operator must be told the two now
			// disagree, not given the same generic message a field-only PATCH
			// failure gets. Fail-fast, no silent retry: the caller (the
			// frontend's own commit hook) keeps Adjust open with its draft
			// intact and the operator explicitly retries.
			c.Logger().Errorf("anchor watch: SignalK confirmed a position change to %v,%v but the local save failed: %v", updated.Lat, updated.Lon, err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "SignalK was updated with the new anchor position, but the local anchor watch could not be saved. Retry Adjust."})
		}
		c.Logger().Errorf("anchor watch: local save failed: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist"})
	}

	anchorWatchState = updated
	if updated.RadiusMeters > 0 {
		lastAnchorWatchRadiusMeters = updated.RadiusMeters
	}
	anchorWatchMu.Unlock()

	if positionChanged {
		// Reset the post-anchor ring buffer, the same way a POST reposition
		// does: the trail is the boat's movement relative to THIS anchor
		// position, and stops meaning anything the instant the anchor moves.
		trailMu.Lock()
		selfTrail = newVesselTrail()
		trailMu.Unlock()
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
//
// The actual raise (publish, remove, clear state/trail/placemarks) lives in
// raiseAnchorWatch (anchor_raise.go), shared with the server-side auto-raise
// watcher (anchor_auto_raise.go, ADR 0099) so a human Raise and an automatic
// one go through exactly the same steps. This handler's job is just to pick
// the right HTTP response for whichever of the two ways that can fail.
func deleteAnchorWatch(c echo.Context) error {
	anchorLifecycleMu.Lock()
	defer anchorLifecycleMu.Unlock()

	if err := raiseAnchorWatch(); err != nil {
		if anchorRaiseFailureStage(err) == anchorRaiseStageRemoveFile {
			c.Logger().Errorf("anchor raise: local watch removal failed: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "SignalK anchor raised, but local watch could not be removed. Retry Raise."})
		}
		c.Logger().Errorf("anchor raise publish failed: %v", err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "SignalK did not confirm anchor raised. Local watch retained; retry Raise."})
	}

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
