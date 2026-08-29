package main

import (
	"log"
	"strconv"
	"sync"
	"time"
)

// mayaraArpaTarget is the wire shape. Field names and optionality are
// mayara's, not ours.
//
// The nested objects are named types rather than anonymous structs on
// purpose. Go compares anonymous struct types including their tags, so an
// anonymous nesting forces every construction site to restate the tags, and
// a tag fixed in one place then silently disagrees with the others. That is
// how the is_dangerous bug below survived being written three times.
type mayaraArpaTarget struct {
	ID          uint64               `json:"id"`
	Status      string               `json:"status"`
	Position    mayaraTargetPosition `json:"position"`
	Motion      *mayaraTargetMotion  `json:"motion"`
	Danger      *mayaraTargetDanger  `json:"danger"`
	Acquisition string               `json:"acquisition"`
	SourceZone  *uint8               `json:"sourceZone"`
	FirstSeen   string               `json:"firstSeen"`
	LastSeen    string               `json:"lastSeen"`
}

type mayaraTargetPosition struct {
	Bearing   float64  `json:"bearing"`
	Distance  int      `json:"distance"`
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

type mayaraTargetMotion struct {
	Course float64 `json:"course"`
	Speed  float64 `json:"speed"`
}

// mayaraTargetDanger carries mayara's own collision figures, passed through
// untouched (ADR 0062 decision 2).
//
// is_dangerous is snake_case while its parent object is camelCase, and that
// is not a typo. mayara's ArpaTargetApi and TargetPositionApi carry
// #[serde(rename_all = "camelCase")]; TargetDangerApi and TargetMotionApi do
// not. Verified against a live DRS4D-NXT capture on 2026-08-28, see
// testdata/mayara/target-dangerous-delta.json.
//
// This was wrong here first, tagged isDangerous from reading the Rust and
// assuming the rename applied throughout. It decodes silently to false, so
// every radar target reads as safe and no CPA alarm ever fires. Pinned by
// TestMayaraArpaTargetDecodesCapturedDangerousDelta.
type mayaraTargetDanger struct {
	Cpa         float64 `json:"cpa"`
	Tcpa        float64 `json:"tcpa"`
	IsDangerous bool    `json:"is_dangerous"`
}

// radarTarget is one ARPA contact. Deliberately not nearbyVessel: a radar
// contact and an AIS contact are different observations with different
// provenance, and the UI labels them apart (ADR 0062).
type radarTarget struct {
	// ID is "<radarID>:<targetID>". mayara numbers targets per radar, so a
	// bare id collides the moment a second radar joins.
	ID       string `json:"id"`
	RadarID  string `json:"radar_id"`
	TargetID uint64 `json:"target_id"`
	Status   string `json:"status"`

	BearingRad float64 `json:"bearing_rad"`
	RangeM     float64 `json:"range_m"`

	// Pointers, not zero values. mayara's lat/lon are optional and own-ship
	// position can be absent, so "no plottable position" must stay
	// distinguishable from a target at 0,0.
	Lat             *float64 `json:"lat,omitempty"`
	Lon             *float64 `json:"lon,omitempty"`
	PositionDerived bool     `json:"position_derived"`

	// motion is omitted when unknown and zeroed when stationary. Pointers
	// keep those two apart.
	CourseRad *float64 `json:"course_rad,omitempty"`
	SogKnots  *float64 `json:"sog_knots,omitempty"`

	// Passed through unchanged. mayara owns this calculation, the same way
	// the AIS path trusts the prioritizer plugin (ADR 0057).
	CpaM        *float64 `json:"cpa_m,omitempty"`
	TcpaSeconds *float64 `json:"tcpa_seconds,omitempty"`
	IsDangerous bool     `json:"is_dangerous"`

	Acquisition string `json:"acquisition"`
	SourceZone  *uint8 `json:"source_zone,omitempty"`
	AgeSeconds  int    `json:"age_seconds"`

	// Seen is local delta receive time and is the eviction reference. Never
	// mayara's lastSeen: that is mayara's clock, and ADR 0042 already settled
	// that a transmitter-supplied timestamp is the wrong thing to age against.
	Seen time.Time `json:"-"`

	// LastSeenAt is mayara's own lastSeen, carried for display only. It
	// drives no decision.
	LastSeenAt string `json:"last_seen_at,omitempty"`
}

// radarTargetKey builds the store key "<radarID>:<targetID>". mayara numbers
// targets per radar, and dual range means one physical radar already
// presents two radar ids on day one (ADR 0062), so a bare target id collides
// the moment a second range or radar joins.
func radarTargetKey(radarID string, targetID uint64) string {
	return radarID + ":" + strconv.FormatUint(targetID, 10)
}

// radarProjectionDisagreementThresholdM is the projection cross-check's
// tolerance (ADR 0062 decision 5 / "risks and open questions"). At ARPA
// ranges a 5-10m antenna-to-GPS offset is inside ordinary plot error; 50m is
// not, and is the size of disagreement any of three real failure modes would
// produce: position.bearing being relative to the bow rather than true, the
// radar not being co-located with the GPS antenna, or mayara reading a
// different own-ship source than Helmcentral does.
const radarProjectionDisagreementThresholdM = 50.0

// radarProjectionMismatchTracker rate-limits the cross-check's log line to
// once per target: the poller re-reads every tracked target every two
// seconds, and without this a persistent disagreement would spam the log at
// that rate for as long as the target stays tracked.
//
// radar_source.go's pollOnce resets it whenever a poll observes nothing,
// which is the closest thing polling has to the stream client's reconnect:
// the radar has gone quiet, so whatever it paints next is a fresh population
// and deserves a fresh log line.
var radarProjectionMismatchTracker = struct {
	mu     sync.Mutex
	logged map[string]bool
}{logged: map[string]bool{}}

// resetRadarProjectionMismatchLog clears the dedup state. Called by pollOnce
// when a poll observes no targets.
func resetRadarProjectionMismatchLog() {
	radarProjectionMismatchTracker.mu.Lock()
	defer radarProjectionMismatchTracker.mu.Unlock()
	radarProjectionMismatchTracker.logged = map[string]bool{}
}

// logRadarProjectionMismatch reports a projection/supplied-position
// disagreement beyond radarProjectionDisagreementThresholdM. Per ADR 0062,
// this is surfaced loudly rather than silently corrected: guessing which of
// bearing-is-relative, antenna-offset, or a differing own-ship source is
// responsible would be exactly the masking behaviour AGENTS.md forbids.
func logRadarProjectionMismatch(radarID string, targetID uint64, disagreementM float64) {
	key := radarTargetKey(radarID, targetID)

	radarProjectionMismatchTracker.mu.Lock()
	alreadyLogged := radarProjectionMismatchTracker.logged[key]
	radarProjectionMismatchTracker.logged[key] = true
	radarProjectionMismatchTracker.mu.Unlock()

	if alreadyLogged {
		return
	}

	log.Printf("radar stream: target %s projected position disagrees with mayara's supplied position by %.0fm; see ADR 0062 decision 5 (bearing may be relative rather than true, radar may not be co-located with the GPS antenna, or mayara's own-ship source may differ from ours)", key, disagreementM)
}

// radarTargetFromArpa converts mayara's wire target into our domain type.
// radarID identifies which of possibly several radar keys (dual range, ADR
// 0062) reported it. selfLat/selfLon/selfOK are own ship's current fix, on
// the same guard buildNearbyVesselsPayload uses (backend/main.go): no fix
// means no projection, not a fabricated one. now is the local delta receive
// time, recorded as Seen — never mayara's own firstSeen/lastSeen (ADR 0042).
func radarTargetFromArpa(radarID string, arpa mayaraArpaTarget, selfLat, selfLon float64, selfOK bool, now time.Time) radarTarget {
	target := radarTarget{
		ID:          radarTargetKey(radarID, arpa.ID),
		RadarID:     radarID,
		TargetID:    arpa.ID,
		Status:      arpa.Status,
		BearingRad:  arpa.Position.Bearing,
		RangeM:      float64(arpa.Position.Distance),
		Acquisition: arpa.Acquisition,
		SourceZone:  arpa.SourceZone,
		LastSeenAt:  arpa.LastSeen,
		Seen:        now,
	}

	// Position derivation order (ADR 0062):
	//   1. mayara supplied lat/lon. Use them, PositionDerived = false.
	//   2. Absent, own-ship fix available. Project with destinationPoint,
	//      PositionDerived = true.
	//   3. Absent, no fix. Lat/Lon stay nil; bearing/range/danger are still
	//      valid and carried through regardless.
	//
	// The cross-check projects from bearing/range whenever a fix is
	// available, even when mayara ALSO supplied a fix, and compares the two.
	// Do not correct silently — log loudly instead (logRadarProjectionMismatch).
	var projectedLat, projectedLon float64
	var projected bool
	if selfOK {
		projectedLat, projectedLon = destinationPoint(selfLat, selfLon, arpa.Position.Bearing, target.RangeM)
		projected = true
	}

	suppliedLat, suppliedLon := arpa.Position.Latitude, arpa.Position.Longitude
	hasSupplied := suppliedLat != nil && suppliedLon != nil

	switch {
	case hasSupplied:
		lat, lon := *suppliedLat, *suppliedLon
		target.Lat = &lat
		target.Lon = &lon
		target.PositionDerived = false

		if projected {
			if disagreementM := haversineMeters(lat, lon, projectedLat, projectedLon); disagreementM > radarProjectionDisagreementThresholdM {
				logRadarProjectionMismatch(radarID, arpa.ID, disagreementM)
			}
		}
	case projected:
		lat, lon := projectedLat, projectedLon
		target.Lat = &lat
		target.Lon = &lon
		target.PositionDerived = true
	default:
		// No supplied position and no own-ship fix to project from. Lat/Lon
		// stay nil; the map already skips a target with no position exactly
		// as it does for AIS (anchor-watch-map.tsx).
	}

	if arpa.Motion != nil {
		course := arpa.Motion.Course
		sogKnots := roundTo1(arpa.Motion.Speed * metersPerSecondToKnots)
		target.CourseRad = &course
		target.SogKnots = &sogKnots
	}

	if arpa.Danger != nil {
		cpa := arpa.Danger.Cpa
		tcpa := arpa.Danger.Tcpa
		target.CpaM = &cpa
		target.TcpaSeconds = &tcpa
		target.IsDangerous = arpa.Danger.IsDangerous
	}

	return target
}
