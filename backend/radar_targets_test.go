package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestRadarTargetFromArpaIDIsRadarAndTargetID(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	arpa := mayaraArpaTarget{ID: 42, Status: "tracking"}

	got := radarTargetFromArpa("fur6424B", arpa, 0, 0, false, now)

	if got.ID != "fur6424B:42" {
		t.Fatalf("id = %q, want fur6424B:42", got.ID)
	}
	if got.RadarID != "fur6424B" || got.TargetID != 42 {
		t.Fatalf("radar_id/target_id not carried correctly: %+v", got)
	}
}

// Supplied position wins: when mayara reports a fix, it is used as-is and
// PositionDerived is false. selfOK is false here so the projection
// cross-check does not run — that path is exercised separately.
func TestRadarTargetFromArpaSuppliedPositionWins(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	lat, lon := -36.8, 174.7

	arpa := mayaraArpaTarget{ID: 7, Status: "tracking"}
	arpa.Position.Bearing = 1.0
	arpa.Position.Distance = 500
	arpa.Position.Latitude = &lat
	arpa.Position.Longitude = &lon

	got := radarTargetFromArpa("fur6424A", arpa, 0, 0, false, now)

	if got.PositionDerived {
		t.Fatalf("supplied position must set PositionDerived=false")
	}
	if got.Lat == nil || got.Lon == nil {
		t.Fatalf("supplied lat/lon must be carried through, got lat=%v lon=%v", got.Lat, got.Lon)
	}
	if *got.Lat != lat || *got.Lon != lon {
		t.Fatalf("got lat/lon (%v,%v), want supplied (%v,%v)", *got.Lat, *got.Lon, lat, lon)
	}
}

// Absent position, own-ship fix available: project with destinationPoint and
// mark PositionDerived. The result must equal destinationPoint called with
// exactly the same arguments — no separate projection logic to drift.
func TestRadarTargetFromArpaProjectsFromOwnShipWhenPositionAbsent(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	selfLat, selfLon := -36.85, 174.75

	arpa := mayaraArpaTarget{ID: 9, Status: "tracking"}
	arpa.Position.Bearing = 2.1
	arpa.Position.Distance = 1852

	got := radarTargetFromArpa("fur6424A", arpa, selfLat, selfLon, true, now)

	wantLat, wantLon := destinationPoint(selfLat, selfLon, arpa.Position.Bearing, float64(arpa.Position.Distance))

	if !got.PositionDerived {
		t.Fatalf("absent position with an own-ship fix must set PositionDerived=true")
	}
	if got.Lat == nil || got.Lon == nil {
		t.Fatalf("projected position must not be nil")
	}
	if *got.Lat != wantLat || *got.Lon != wantLon {
		t.Fatalf("projected (%v,%v) != destinationPoint (%v,%v)", *got.Lat, *got.Lon, wantLat, wantLon)
	}
}

// No own-ship fix at all: Lat/Lon stay nil, but the target is not otherwise
// degraded — bearing, range, and danger figures all survive, exactly as the
// map already treats a positionless AIS contact.
func TestRadarTargetFromArpaNoFixLeavesPositionNilButKeepsBearingRangeAndDanger(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

	arpa := mayaraArpaTarget{ID: 11, Status: "tracking"}
	arpa.Position.Bearing = 0.75
	arpa.Position.Distance = 3000
	arpa.Danger = &mayaraTargetDanger{Cpa: 400, Tcpa: 120, IsDangerous: true}

	got := radarTargetFromArpa("fur6424A", arpa, 0, 0, false, now)

	if got.Lat != nil || got.Lon != nil {
		t.Fatalf("no own-ship fix must leave Lat/Lon nil, got lat=%v lon=%v", got.Lat, got.Lon)
	}
	if got.PositionDerived {
		t.Fatalf("PositionDerived must be false when there is no position at all")
	}
	if got.BearingRad != 0.75 || got.RangeM != 3000 {
		t.Fatalf("bearing/range must survive with no position, got bearing=%v range=%v", got.BearingRad, got.RangeM)
	}
	if got.CpaM == nil || *got.CpaM != 400 || got.TcpaSeconds == nil || *got.TcpaSeconds != 120 || !got.IsDangerous {
		t.Fatalf("danger figures must survive with no position, got %+v", got)
	}
}

func TestRadarTargetFromArpaSpeedConvertsMetersPerSecondToKnots(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

	arpa := mayaraArpaTarget{ID: 3, Status: "tracking"}
	arpa.Motion = &mayaraTargetMotion{Course: 1.5, Speed: 5.0} // 5 m/s

	got := radarTargetFromArpa("fur6424A", arpa, 0, 0, false, now)

	want := roundTo1(5.0 * metersPerSecondToKnots)
	if got.SogKnots == nil || *got.SogKnots != want {
		t.Fatalf("sog knots = %v, want %v (roundTo1(5 m/s in knots))", got.SogKnots, want)
	}
	if got.CourseRad == nil || *got.CourseRad != 1.5 {
		t.Fatalf("course rad = %v, want 1.5 unchanged", got.CourseRad)
	}
}

// motion omitted vs. motion zeroed are different facts (unknown vs.
// confirmed stationary) and must stay distinguishable through pointers.
func TestRadarTargetFromArpaMotionAbsentVsZeroed(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

	noMotion := mayaraArpaTarget{ID: 1, Status: "tracking"}
	got := radarTargetFromArpa("fur6424A", noMotion, 0, 0, false, now)
	if got.CourseRad != nil || got.SogKnots != nil {
		t.Fatalf("absent motion must leave CourseRad/SogKnots nil, got course=%v sog=%v", got.CourseRad, got.SogKnots)
	}

	stationary := mayaraArpaTarget{ID: 2, Status: "tracking"}
	stationary.Motion = &mayaraTargetMotion{Course: 0, Speed: 0}
	got2 := radarTargetFromArpa("fur6424A", stationary, 0, 0, false, now)
	if got2.CourseRad == nil || got2.SogKnots == nil {
		t.Fatalf("zeroed motion must produce non-nil pointers, got course=%v sog=%v", got2.CourseRad, got2.SogKnots)
	}
	if *got2.CourseRad != 0 || *got2.SogKnots != 0 {
		t.Fatalf("zeroed motion should convert to zero, got course=%v sog=%v", *got2.CourseRad, *got2.SogKnots)
	}
}

func TestRadarTargetFromArpaDangerPassesThroughUnchanged(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

	arpa := mayaraArpaTarget{ID: 4, Status: "tracking"}
	arpa.Danger = &mayaraTargetDanger{Cpa: 926.4, Tcpa: -30.5, IsDangerous: true}

	got := radarTargetFromArpa("fur6424A", arpa, 0, 0, false, now)

	if got.CpaM == nil || *got.CpaM != 926.4 {
		t.Fatalf("cpa must pass through unchanged, got %v", got.CpaM)
	}
	// Negative tcpa means already passed (see mayaraArpaTarget's doc comment)
	// and must not be clamped or reinterpreted.
	if got.TcpaSeconds == nil || *got.TcpaSeconds != -30.5 {
		t.Fatalf("tcpa must pass through unchanged including negative = already passed, got %v", got.TcpaSeconds)
	}
	if !got.IsDangerous {
		t.Fatalf("isDangerous must pass through unchanged")
	}
}

// danger is omitted, not zeroed, when mayara considers relative motion
// undefined (e.g. an acquiring target). Nil pointers, not fabricated zeros.
func TestRadarTargetFromArpaNoDangerLeavesNilPointers(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	arpa := mayaraArpaTarget{ID: 5, Status: "acquiring"}

	got := radarTargetFromArpa("fur6424A", arpa, 0, 0, false, now)
	if got.CpaM != nil || got.TcpaSeconds != nil {
		t.Fatalf("absent danger must leave CpaM/TcpaSeconds nil, got cpa=%v tcpa=%v", got.CpaM, got.TcpaSeconds)
	}
	if got.IsDangerous {
		t.Fatalf("absent danger must leave IsDangerous false")
	}
}

func TestRadarTargetFromArpaSeenIsLocalReceiveTimeNotMayaraLastSeen(t *testing.T) {
	receiveTime := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

	arpa := mayaraArpaTarget{ID: 6, Status: "tracking"}
	arpa.LastSeen = "2020-01-01T00:00:00.000Z" // mayara's clock, must be ignored for aging

	got := radarTargetFromArpa("fur6424A", arpa, 0, 0, false, receiveTime)

	if !got.Seen.Equal(receiveTime) {
		t.Fatalf("Seen = %v, want the local receive time %v (ADR 0042: never age against the transmitter's own clock)", got.Seen, receiveTime)
	}
	if got.LastSeenAt != arpa.LastSeen {
		t.Fatalf("LastSeenAt should still carry mayara's own lastSeen for display, got %q", got.LastSeenAt)
	}
}

// Decoding the real thing. Everything above builds mayaraArpaTarget in Go,
// which proves our conversion logic but says nothing about whether the JSON
// tags match what a radar actually puts on the wire.
//
// This is the test that would have caught the is_dangerous bug: mayara's
// Rust ArpaTargetApi and TargetPositionApi carry
// #[serde(rename_all = "camelCase")] but TargetDangerApi and TargetMotionApi
// do NOT, so the danger block is snake_case while its parent is camelCase.
// Reading the source and assuming the rename applied throughout produced a
// tag that decoded IsDangerous to false on every target, forever, silently.
// A radar CPA alarm would simply never have fired.
//
// Fixture captured 2026-08-28 from the DRS4D-NXT via guard zone 1.
func TestMayaraArpaTargetDecodesCapturedDangerousDelta(t *testing.T) {
	value := decodeCapturedTargetValue(t, "testdata/mayara/target-dangerous-delta.json")

	var arpa mayaraArpaTarget
	if err := json.Unmarshal(value, &arpa); err != nil {
		t.Fatalf("decode captured target: %v", err)
	}

	if arpa.Status != "tracking" {
		t.Fatalf("status = %q, want tracking", arpa.Status)
	}
	if arpa.Danger == nil {
		t.Fatal("danger block did not decode; the captured frame has one")
	}
	if !arpa.Danger.IsDangerous {
		t.Fatal("is_dangerous decoded false from a frame where it is true: " +
			"the JSON tag does not match the wire, so no radar alarm would ever fire")
	}
	if arpa.Danger.Cpa <= 0 || arpa.Danger.Tcpa == 0 {
		t.Fatalf("cpa/tcpa did not decode: %+v", *arpa.Danger)
	}
	if arpa.Motion == nil || arpa.Motion.Speed <= 0 {
		t.Fatalf("motion did not decode: %+v", arpa.Motion)
	}
	if arpa.Position.Latitude == nil || arpa.Position.Longitude == nil {
		t.Fatal("position lat/lon did not decode; the captured frame has both")
	}
	if arpa.Position.Distance <= 0 || arpa.Position.Bearing == 0 {
		t.Fatalf("position bearing/distance did not decode: %+v", arpa.Position)
	}
	if arpa.SourceZone == nil || *arpa.SourceZone != 1 {
		t.Fatalf("sourceZone = %v, want 1 (guard zone 1 acquired it)", arpa.SourceZone)
	}
	if arpa.Acquisition != "auto" || arpa.FirstSeen == "" || arpa.LastSeen == "" {
		t.Fatalf("acquisition/timestamps did not decode: %+v", arpa)
	}
}

// The end-to-end contract stated in ADR 0062 decision 2: mayara owns the
// collision calculation and we pass its figures through untouched.
func TestRadarTargetFromArpaCarriesCapturedDangerThrough(t *testing.T) {
	value := decodeCapturedTargetValue(t, "testdata/mayara/target-dangerous-delta.json")

	var arpa mayaraArpaTarget
	if err := json.Unmarshal(value, &arpa); err != nil {
		t.Fatalf("decode captured target: %v", err)
	}

	got := radarTargetFromArpa("fur6424A", arpa, 0, 0, false, time.Now().UTC())

	if !got.IsDangerous {
		t.Fatal("IsDangerous lost between wire and domain type")
	}
	if got.CpaM == nil || *got.CpaM != arpa.Danger.Cpa {
		t.Fatalf("cpa altered: got %v, wire had %v", got.CpaM, arpa.Danger.Cpa)
	}
	if got.TcpaSeconds == nil || *got.TcpaSeconds != arpa.Danger.Tcpa {
		t.Fatalf("tcpa altered: got %v, wire had %v", got.TcpaSeconds, arpa.Danger.Tcpa)
	}
}

// A target mayara has declared lost must never reach the alarm path.
func TestMayaraArpaTargetDecodesCapturedLostDelta(t *testing.T) {
	value := decodeCapturedTargetValue(t, "testdata/mayara/target-lost-delta.json")

	var arpa mayaraArpaTarget
	if err := json.Unmarshal(value, &arpa); err != nil {
		t.Fatalf("decode captured lost target: %v", err)
	}
	if arpa.Status != "lost" {
		t.Fatalf("status = %q, want lost", arpa.Status)
	}
}

// decodeCapturedTargetValue pulls the single target value out of a captured
// delta envelope, so tests assert against bytes the radar actually sent.
func decodeCapturedTargetValue(t *testing.T, path string) []byte {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var envelope struct {
		Updates []struct {
			Values []struct {
				Path  string          `json:"path"`
				Value json.RawMessage `json:"value"`
			} `json:"values"`
		} `json:"updates"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode envelope %s: %v", path, err)
	}
	if len(envelope.Updates) != 1 || len(envelope.Updates[0].Values) != 1 {
		t.Fatalf("%s: want exactly one update with one value", path)
	}
	return envelope.Updates[0].Values[0].Value
}
