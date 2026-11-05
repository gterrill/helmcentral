package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// The watch in these tests is dropped at -20.0, 149.0. A degree of latitude is
// about 111 km, so 0.002 is about 222 m north of it (inside the 500 m circle)
// and 0.006 about 667 m (outside).
const (
	aisTestAnchorLat = -20.0
	aisTestAnchorLon = 149.0
	aisInsideLat     = -19.998
	aisOutsideLat    = -19.994
)

func aisTarget(mmsi, name string, lat float64, seen time.Time) nearbyVessel {
	return nearbyVessel{ID: "urn:mrn:imo:mmsi:" + mmsi, Name: name, Mmsi: mmsi, Lat: lat, Lon: aisTestAnchorLon, PositionSeen: seen}
}

// resetAISAnchorTrackStateForTest stands in for a process restart.
func resetAISAnchorTrackStateForTest() {
	aisTracks.mu.Lock()
	aisTracks.inside = map[string]bool{}
	aisTracks.lastMs = map[string]int64{}
	aisTracks.pendingSegment = map[string]bool{}
	aisTracks.warmed = false
	aisTracks.notBefore = 0
	aisTracks.lastPrune = time.Time{}
	aisTracks.mu.Unlock()
	missingMMSILoggedForAISAnchorTrack = sync.Map{}
}

func aisEnv(t *testing.T) *passageStore {
	t.Helper()
	store := anchorTrailEnv(t)
	resetAISAnchorTrackStateForTest()
	t.Cleanup(resetAISAnchorTrackStateForTest)
	return store
}

func dropWatch(t *testing.T) {
	t.Helper()
	if code, resp := postAnchorWatch(t, map[string]any{"lat": aisTestAnchorLat, "lon": aisTestAnchorLon}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}
}

// aisBase is a time just after the drop, so no test report predates it.
func aisBase() time.Time {
	return time.Now().UTC().Truncate(time.Millisecond).Add(time.Second)
}

func aisTracksNow(t *testing.T, store *passageStore, now time.Time) []aisTrackWire {
	t.Helper()
	tracks, _, err := store.aisAnchorTracksSince(0, now)
	if err != nil {
		t.Fatal(err)
	}
	return tracks
}

func aisRowCount(t *testing.T, store *passageStore) int {
	t.Helper()
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM ais_anchor_track_points`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

type aisTrailsBody struct {
	TrailStartedAt *string        `json:"trail_started_at"`
	Cursor         int64          `json:"cursor"`
	Tracks         []aisTrackWire `json:"tracks"`
}

func getAISTrailsBody(t *testing.T, since string) (int, aisTrailsBody, string) {
	t.Helper()
	target := "/api/anchor-watch/trails/ais"
	if since != "" {
		target += "?since=" + since
	}
	rec := httptest.NewRecorder()
	if err := getAISTrailsHandler(echo.New().NewContext(httptest.NewRequest(http.MethodGet, target, nil), rec)); err != nil {
		t.Fatal(err)
	}
	var body aisTrailsBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body, rec.Body.String()
}

// aisTick drives one poll tick the way sampleTracks does: the target's position
// is seeded into the snapshot as last received at reportAt, the nearby list is
// fetched at now (so a report older than the freshness limit drops the target
// from the list for real), and the recorder gets that list.
func aisTick(t *testing.T, now time.Time, mmsi string, lat float64, reportAt time.Time) {
	t.Helper()
	body := fmt.Sprintf(`{"urn:mrn:imo:mmsi:%s": {"name": "ALPHA", "mmsi": %q, "navigation": {"position": {"value": {"latitude": %f, "longitude": %f}}}}}`,
		mmsi, mmsi, lat, aisTestAnchorLon)
	seedVesselTreesAged(t, body, map[string]time.Duration{"urn:mrn:imo:mmsi:" + mmsi: now.Sub(reportAt)}, now)
	nearby, err := fetchSignalKNearbyVesselsLimit(aisTestAnchorLat, aisTestAnchorLon, now, nil, nearbyVesselsUnlimited)
	if err != nil {
		t.Fatal(err)
	}
	recordAISAnchorTracks(nearby, now)
}

func TestAISAnchorTracksNotRecordedWithoutWatch(t *testing.T) {
	store := aisEnv(t)
	now := aisBase()
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, now)}, now)
	if n := aisRowCount(t, store); n != 0 {
		t.Fatalf("expected no rows without a watch, got %d", n)
	}
	code, body, raw := getAISTrailsBody(t, "")
	if code != http.StatusOK || body.TrailStartedAt != nil || body.Cursor != 0 || body.Tracks == nil || len(body.Tracks) != 0 {
		t.Fatalf("expected an empty answer with no watch, got %d %s", code, raw)
	}
}

func TestTracksHandlerServesOnlyTheSelfTrack(t *testing.T) {
	aisEnv(t)
	resetTracksStateForTest(t)
	recordTrackSelf(-25.29, 152.91)
	rec := httptest.NewRecorder()
	if err := getTracksHandler(echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/tracks", nil), rec)); err != nil {
		t.Fatal(err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["ais"]; ok || len(body) != 1 {
		t.Fatalf("/api/tracks must carry the self track only, got %s", rec.Body.String())
	}
}

func TestAISAnchorTracksRecordOnlyWithinFiveHundredMetresOfTheAnchor(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	now := aisBase()
	recordAISAnchorTracks([]nearbyVessel{
		aisTarget("111111111", "ALPHA", aisInsideLat, now),
		aisTarget("222222222", "BRAVO", aisOutsideLat, now),
	}, now)
	tracks := aisTracksNow(t, store, now)
	if len(tracks) != 1 || tracks[0].MMSI != "111111111" || len(tracks[0].Points) != 1 {
		t.Fatalf("expected only ALPHA recorded, got %+v", tracks)
	}
}

func TestAISAnchorTracksStoreOnePointPerNewReportNotPerPoll(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	t0 := aisBase()
	report1 := t0
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, report1)}, t0)
	// Two more polls, the target has not reported again.
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, report1)}, t0.Add(5*time.Second))
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, report1)}, t0.Add(10*time.Second))
	if n := aisRowCount(t, store); n != 1 {
		t.Fatalf("expected one row for one report, got %d", n)
	}
	report2 := t0.Add(40 * time.Second)
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat+0.0001, report2)}, t0.Add(40*time.Second))
	tracks := aisTracksNow(t, store, t0.Add(40*time.Second))
	if len(tracks) != 1 || len(tracks[0].Points) != 2 {
		t.Fatalf("expected two points, got %+v", tracks)
	}
	// The time stored is the report's own time, in order.
	if tracks[0].Points[0].TMs != report1.UnixMilli() || tracks[0].Points[1].TMs != report2.UnixMilli() {
		t.Fatalf("timestamps are not the reports' own: %+v", tracks[0].Points)
	}
}

func TestAISAnchorTracksSkipTargetsWithoutMMSIAndLogOncePerVessel(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	var buf strings.Builder
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	now := aisBase()
	noMMSI := aisTarget("", "NO MMSI BOAT", aisInsideLat, now)
	noMMSI.ID = "urn:mrn:imo:mmsi:unknown"
	for i := 0; i < 3; i++ {
		recordAISAnchorTracks([]nearbyVessel{noMMSI}, now.Add(time.Duration(i)*5*time.Second))
	}
	if n := aisRowCount(t, store); n != 0 {
		t.Fatalf("a target with no MMSI must not be tracked, got %d rows", n)
	}
	if got := strings.Count(buf.String(), "NO MMSI BOAT"); got != 1 {
		t.Fatalf("expected one log line across three polls, got %d:\n%s", got, buf.String())
	}
}

func TestAISAnchorTracksKeepTheTrailOfATargetThatLeavesTheCircle(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	t0 := aisBase()
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, t0)}, t0)
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisOutsideLat, t0.Add(5*time.Second))}, t0.Add(5*time.Second))
	tracks := aisTracksNow(t, store, t0.Add(5*time.Second))
	if len(tracks) != 1 || len(tracks[0].Points) != 1 {
		t.Fatalf("leaving must stop adding points but keep the trail, got %+v", tracks)
	}
}

func TestAISAnchorTracksBreakWhereATargetLeftAndCameBackEvenUnderFifteenMinutes(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	t0 := aisBase()
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, at(0))}, at(0))
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat+0.0001, at(35))}, at(35))
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisOutsideLat, at(70))}, at(70))
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, at(105))}, at(105))
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat+0.0001, at(140))}, at(140))
	pts := aisTracksNow(t, store, at(140))[0].Points
	if len(pts) != 4 {
		t.Fatalf("expected 4 points, got %+v", pts)
	}
	want := []bool{true, false, true, false}
	for i, p := range pts {
		if p.NewSegment != want[i] {
			t.Fatalf("point %d new_segment = %v, want %v (%+v)", i, p.NewSegment, want[i], pts)
		}
	}
}

func TestAISAnchorTracksBreakAfterMoreThanFifteenMinutesBetweenReports(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	t0 := aisBase()
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	for _, m := range []int{0, 10, 26} {
		recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, at(m))}, at(m))
	}
	pts := aisTracksNow(t, store, at(30))[0].Points
	want := []bool{true, false, true} // 10 minutes joins, 16 minutes breaks
	for i, p := range pts {
		if p.NewSegment != want[i] {
			t.Fatalf("point %d new_segment = %v, want %v", i, p.NewSegment, want[i])
		}
	}
}

func TestAISAnchorTracksJoinAcrossATwelveMinuteAbsenceFromTheNearbyList(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	t0 := aisBase()
	aisTick(t, t0, "111111111", aisInsideLat, t0)
	// Twelve minutes on, the position is past the ten-minute freshness limit, so
	// the target is not in the list at all.
	aisTick(t, t0.Add(12*time.Minute), "111111111", aisInsideLat, t0)
	aisTick(t, t0.Add(12*time.Minute+5*time.Second), "111111111", aisInsideLat+0.0001, t0.Add(12*time.Minute+5*time.Second))
	pts := aisTracksNow(t, store, t0.Add(13*time.Minute))[0].Points
	if len(pts) != 2 || pts[1].NewSegment {
		t.Fatalf("twelve minutes is under the 15 minute gap, so the line must join: %+v", pts)
	}
}

func TestAISAnchorTracksBreakAfterASixteenMinuteAbsence(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	t0 := aisBase()
	aisTick(t, t0, "111111111", aisInsideLat, t0)
	aisTick(t, t0.Add(16*time.Minute), "111111111", aisInsideLat+0.0001, t0.Add(16*time.Minute))
	pts := aisTracksNow(t, store, t0.Add(17*time.Minute))[0].Points
	if len(pts) != 2 || !pts[1].NewSegment {
		t.Fatalf("sixteen minutes is over the gap, so the line must break: %+v", pts)
	}
}

func TestAISAnchorTracksAnEmptiedListDoesNotBreakLines(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	t0 := aisBase()
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, at(0))}, at(0))
	// The own fix is lost for a while and the whole list comes back empty.
	recordAISAnchorTracks(nil, at(5))
	recordAISAnchorTracks(nil, at(10))
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat+0.0001, at(45))}, at(45))
	pts := aisTracksNow(t, store, at(50))[0].Points
	if len(pts) != 2 || pts[1].NewSegment {
		t.Fatalf("an empty list is not evidence the target left the circle: %+v", pts)
	}
}

func TestAISAnchorTracksSurviveARestartAndBreakAtIt(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	t0 := aisBase()
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, t0)}, t0)

	// A restart loses everything in memory; the database stays.
	resetAISAnchorTrackStateForTest()
	if tracks := aisTracksNow(t, store, t0); len(tracks) != 1 || len(tracks[0].Points) != 1 {
		t.Fatalf("the stored track must survive a restart, got %+v", tracks)
	}
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, t0.Add(40*time.Second))}, t0.Add(40*time.Second))
	pts := aisTracksNow(t, store, t0.Add(40*time.Second))[0].Points
	if len(pts) != 2 || !pts[1].NewSegment {
		t.Fatalf("the first point after a restart starts a new segment, got %+v", pts)
	}
	// A report seen again is not written twice, restart or not.
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, t0.Add(40*time.Second))}, t0.Add(45*time.Second))
	if n := aisRowCount(t, store); n != 2 {
		t.Fatalf("the stored report must not be written again, got %d rows", n)
	}
}

func TestAISAnchorTracksDropPointsOlderThanTwentyFourHours(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	now := aisBase()
	old := now.Add(-24*time.Hour - time.Minute)
	nearEdge := now.Add(-23*time.Hour - 59*time.Minute)
	// Written directly: the recorder would not be handed a report this old.
	for _, ts := range []time.Time{old, nearEdge} {
		if _, err := store.db.Exec(`INSERT INTO ais_anchor_track_points (mmsi, t_ms, lat, lon, new_segment) VALUES ('111111111', ?, ?, ?, 0)`,
			ts.UnixMilli(), aisInsideLat, aisTestAnchorLon); err != nil {
			t.Fatal(err)
		}
	}
	// Not served even before storage is pruned.
	if tracks := aisTracksNow(t, store, now); len(tracks) != 1 || len(tracks[0].Points) != 1 {
		t.Fatalf("a point older than 24 h must not be served, got %+v", tracks)
	}
	// A recording tick prunes storage.
	recordAISAnchorTracks([]nearbyVessel{aisTarget("222222222", "BRAVO", aisInsideLat, now)}, now)
	var n int
	_ = store.db.QueryRow(`SELECT COUNT(*) FROM ais_anchor_track_points WHERE mmsi = '111111111'`).Scan(&n)
	if n != 1 {
		t.Fatalf("expected the old row pruned from storage, %d rows left", n)
	}
}

func TestAISAnchorTracksPruneAtMostOncePerMinute(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	now := aisBase()
	recordAISAnchorTracks(nil, now) // prunes: none has yet
	old := now.Add(-25 * time.Hour)
	if _, err := store.db.Exec(`INSERT INTO ais_anchor_track_points (mmsi, t_ms, lat, lon, new_segment) VALUES ('111111111', ?, ?, ?, 0)`,
		old.UnixMilli(), aisInsideLat, aisTestAnchorLon); err != nil {
		t.Fatal(err)
	}
	recordAISAnchorTracks(nil, now.Add(30*time.Second))
	if aisRowCount(t, store) != 1 {
		t.Fatal("a tick within a minute of the last prune must not prune")
	}
	recordAISAnchorTracks(nil, now.Add(61*time.Second))
	if aisRowCount(t, store) != 0 {
		t.Fatal("a tick a minute on must prune")
	}
}

// The moved anchor of the next tests sits at -19.99, 149.0. The target at
// aisInsideLat is then about 890 m from it (outside); aisMovedInsideLat is
// about 55 m from it (inside).
const (
	aisMovedAnchorLat = -19.99
	aisMovedInsideLat = -19.9895
)

func TestAISAnchorTracksKeptByMovedAnchorErasedByRaise(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	now := aisBase()
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, now)}, now)
	if aisRowCount(t, store) != 1 {
		t.Fatal("expected one row before the move")
	}
	// A radius-only change keeps the tracks.
	if code, resp := patchAnchorWatchForTest(t, map[string]any{"radius_meters": 40.0}); code != http.StatusOK {
		t.Fatalf("radius patch: %d %v", code, resp)
	}
	if aisRowCount(t, store) != 1 {
		t.Fatal("a radius change must keep the tracks")
	}
	if code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": aisMovedAnchorLat, "lon": aisTestAnchorLon}); code != http.StatusOK {
		t.Fatalf("move patch: %d %v", code, resp)
	}
	if n := aisRowCount(t, store); n != 1 {
		t.Fatalf("a moved anchor must keep the tracks, got %d", n)
	}

	// A raise erases them.
	if rec := raiseAnchor(t); rec.Code != http.StatusOK {
		t.Fatalf("raise: %d", rec.Code)
	}
	if n := aisRowCount(t, store); n != 0 {
		t.Fatalf("a raise must erase the tracks, got %d", n)
	}
}

func TestAISAnchorTracksKeptByARepositionPOST(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	now := aisBase()
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, now)}, now)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": aisMovedAnchorLat, "lon": aisTestAnchorLon}); code != http.StatusOK {
		t.Fatalf("reposition: %d %v", code, resp)
	}
	if n := aisRowCount(t, store); n != 1 {
		t.Fatalf("a repeated drop on an active watch must keep the tracks, got %d", n)
	}
}

func TestAISAnchorTracksADropAfterARaiseStartsFromNothing(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	now := aisBase()
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, now)}, now)
	if rec := raiseAnchor(t); rec.Code != http.StatusOK {
		t.Fatalf("raise: %d", rec.Code)
	}
	dropWatch(t)
	if n := aisRowCount(t, store); n != 0 {
		t.Fatalf("a new drop starts with no tracks, got %d", n)
	}
}

func TestAISAnchorTracksMoveKeepsTheRowsAndTheTrailStart(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	t0 := aisBase()
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, t0)}, t0)
	_, first, raw := getAISTrailsBody(t, "")
	if first.TrailStartedAt == nil || len(first.Tracks) != 1 {
		t.Fatalf("unexpected first answer %s", raw)
	}

	time.Sleep(5 * time.Millisecond)
	if code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": aisMovedAnchorLat, "lon": aisTestAnchorLon}); code != http.StatusOK {
		t.Fatalf("move patch: %d %v", code, resp)
	}
	_, afterPatch, raw := getAISTrailsBody(t, "")
	if afterPatch.TrailStartedAt == nil || *afterPatch.TrailStartedAt != *first.TrailStartedAt || len(afterPatch.Tracks) != 1 || afterPatch.Cursor != first.Cursor {
		t.Fatalf("a PATCH move keeps the start and the rows, got %s", raw)
	}

	time.Sleep(5 * time.Millisecond)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": aisTestAnchorLat, "lon": aisTestAnchorLon}); code != http.StatusOK {
		t.Fatalf("reposition: %d %v", code, resp)
	}
	_, afterPost, raw := getAISTrailsBody(t, "")
	if afterPost.TrailStartedAt == nil || *afterPost.TrailStartedAt != *first.TrailStartedAt || len(afterPost.Tracks) != 1 {
		t.Fatalf("a POST reposition keeps the start and the rows, got %s", raw)
	}
	if aisRowCount(t, store) != 1 {
		t.Fatal("expected the one row to remain")
	}
}

func TestAISAnchorTracksAfterAMoveATargetOutsideTheNewCircleStopsRecording(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	t0 := aisBase()
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, t0)}, t0)
	if code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": aisMovedAnchorLat, "lon": aisTestAnchorLon}); code != http.StatusOK {
		t.Fatalf("move patch: %d %v", code, resp)
	}
	// The same target reports again from the same place: it was inside the old
	// circle and is about 890 m from the new mark.
	t1 := t0.Add(10 * time.Second)
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, t1)}, t1)
	if n := aisRowCount(t, store); n != 1 {
		t.Fatalf("a target outside the new circle must add no point, got %d rows", n)
	}
}

func TestAISAnchorTracksAfterAMoveATargetReEnteringTheNewCircleStartsANewSegment(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	t0 := aisBase()
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, t0)}, t0)
	if code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": aisMovedAnchorLat, "lon": aisTestAnchorLon}); code != http.StatusOK {
		t.Fatalf("move patch: %d %v", code, resp)
	}
	t1 := t0.Add(10 * time.Second)
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, t1)}, t1) // outside now
	t2 := t0.Add(40 * time.Second)
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisMovedInsideLat, t2)}, t2) // back inside
	pts := aisTracksNow(t, store, t2)
	if len(pts) != 1 || len(pts[0].Points) != 2 {
		t.Fatalf("expected the old point and one new one, got %+v", pts)
	}
	if !pts[0].Points[0].NewSegment || !pts[0].Points[1].NewSegment {
		t.Fatalf("the re-entry point must start a new segment: %+v", pts[0].Points)
	}
}

func TestAISAnchorTracksAReportReceivedBeforeTheDropIsNotStoredAfterIt(t *testing.T) {
	store := aisEnv(t)
	beforeDrop := time.Now().UTC().Add(-time.Second)
	dropWatch(t)
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, beforeDrop)}, aisBase())
	if n := aisRowCount(t, store); n != 0 {
		t.Fatalf("a pre-drop report belongs to the old anchor, got %d rows", n)
	}
	fresh := aisBase()
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, fresh)}, fresh)
	if n := aisRowCount(t, store); n != 1 {
		t.Fatalf("a report after the drop is stored, got %d rows", n)
	}
}

func TestAISAnchorTracksAFailedWriteKeepsTheSegmentFlagAndThePoint(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	var buf strings.Builder
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	t0 := aisBase()
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, at(0))}, at(0))
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisOutsideLat, at(5))}, at(5))

	if _, err := store.db.Exec(`ALTER TABLE ais_anchor_track_points RENAME TO ais_hidden`); err != nil {
		t.Fatal(err)
	}
	// The target is back inside, but the write fails.
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, at(40))}, at(40))
	if !strings.Contains(buf.String(), "ais anchor tracks:") {
		t.Fatalf("a failed write must be logged, got %q", buf.String())
	}
	if _, err := store.db.Exec(`ALTER TABLE ais_hidden RENAME TO ais_anchor_track_points`); err != nil {
		t.Fatal(err)
	}
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, at(40))}, at(45))
	pts := aisTracksNow(t, store, at(45))[0].Points
	if len(pts) != 2 || !pts[1].NewSegment {
		t.Fatalf("the retried point must still start a new segment, got %+v", pts)
	}
}

func TestAISAnchorTracksOpenNoWriteWhenNothingIsNew(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	var buf strings.Builder
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	t0 := aisBase()
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, t0)}, t0)
	// With the table gone, any write or prune would fail and log.
	if _, err := store.db.Exec(`ALTER TABLE ais_anchor_track_points RENAME TO ais_hidden`); err != nil {
		t.Fatal(err)
	}
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, t0)}, t0.Add(5*time.Second))
	if strings.Contains(buf.String(), "ais anchor tracks:") {
		t.Fatalf("a tick with nothing new must not touch the database: %s", buf.String())
	}
}

func TestAISTrailsHandlerServesRealTimesAndIncrementsByCursor(t *testing.T) {
	aisEnv(t)
	dropWatch(t)
	t0 := aisBase()
	recordAISAnchorTracks([]nearbyVessel{
		aisTarget("111111111", "ALPHA", aisInsideLat, t0),
		aisTarget("222222222", "BRAVO", aisInsideLat, t0.Add(-3*time.Millisecond)),
	}, t0)
	// The response is stable and carries no name.
	code, first, raw := getAISTrailsBody(t, "")
	if code != http.StatusOK || first.TrailStartedAt == nil || len(first.Tracks) != 2 || first.Cursor == 0 {
		t.Fatalf("unexpected first answer %d %s", code, raw)
	}
	if strings.Contains(raw, "name") {
		t.Fatalf("the wire carries no name: %s", raw)
	}
	for _, tr := range first.Tracks {
		if len(tr.Points) != 1 || !tr.Points[0].NewSegment {
			t.Fatalf("expected one first point per target: %+v", tr)
		}
	}

	// Nothing new: no tracks, same cursor.
	_, quiet, _ := getAISTrailsBody(t, strconv.FormatInt(first.Cursor, 10))
	if len(quiet.Tracks) != 0 || quiet.Cursor != first.Cursor {
		t.Fatalf("nothing new must return nothing and keep the cursor: %+v", quiet)
	}

	// One target reports again: only it comes back, with t_ms its report time.
	next := t0.Add(40 * time.Second)
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat+0.0001, next)}, next)
	_, inc, _ := getAISTrailsBody(t, strconv.FormatInt(first.Cursor, 10))
	if len(inc.Tracks) != 1 || inc.Tracks[0].MMSI != "111111111" || len(inc.Tracks[0].Points) != 1 ||
		inc.Tracks[0].Points[0].TMs != next.UnixMilli() || inc.Tracks[0].Points[0].NewSegment || inc.Cursor <= first.Cursor {
		t.Fatalf("expected only the new point, got %+v", inc)
	}

	// A moved anchor keeps the tracks: trail_started_at does not change.
	time.Sleep(5 * time.Millisecond)
	if code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": aisMovedAnchorLat, "lon": aisTestAnchorLon}); code != http.StatusOK {
		t.Fatalf("move patch: %d %v", code, resp)
	}
	_, after, _ := getAISTrailsBody(t, "")
	if after.TrailStartedAt == nil || *after.TrailStartedAt != *first.TrailStartedAt || len(after.Tracks) != 2 {
		t.Fatalf("expected the same start and both tracks, got %+v", after)
	}

	// A raise and a new drop restart: trail_started_at changes and the tracks are gone.
	if rec := raiseAnchor(t); rec.Code != http.StatusOK {
		t.Fatalf("raise: %d", rec.Code)
	}
	time.Sleep(5 * time.Millisecond)
	dropWatch(t)
	_, fresh, _ := getAISTrailsBody(t, "")
	if fresh.TrailStartedAt == nil || *fresh.TrailStartedAt == *first.TrailStartedAt || len(fresh.Tracks) != 0 {
		t.Fatalf("expected a restarted, empty answer, got %+v", fresh)
	}
}

func TestAISTrailsHandlerRejectsABadCursor(t *testing.T) {
	aisEnv(t)
	code, _, _ := getAISTrailsBody(t, "nonsense")
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", code)
	}
}

func TestAISTrailsHandlerFailsLoudlyWithoutAStore(t *testing.T) {
	aisEnv(t)
	dropWatch(t)
	previous := globalPassageStore
	globalPassageStore = nil
	t.Cleanup(func() { globalPassageStore = previous })
	if code, _, _ := getAISTrailsBody(t, ""); code != http.StatusInternalServerError {
		t.Fatalf("expected 500 with no store, got %d", code)
	}
}

func TestNearbyContactsAndAISTracksShareOneFetch(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	contacts := newTestNearbyContactStore(t)
	original := globalNearbyContactStore
	globalNearbyContactStore = contacts
	t.Cleanup(func() { globalNearbyContactStore = original })

	// Self sits on the anchor; one boat 222 m north of it.
	seedVesselTrees(t, `{
		"self": {"mmsi": "518999323", "name": "Pikorua", "navigation": {"position": {"value": {"latitude": -20.0, "longitude": 149.0}}}},
		"urn:mrn:imo:mmsi:111111111": {"name": "Alpha", "mmsi": "111111111", "navigation": {"position": {"value": {"latitude": -19.998, "longitude": 149.0}}}}
	}`)
	state := vesselStateData{Latitude: aisTestAnchorLat, Longitude: aisTestAnchorLon, Status: "anchored"}
	now := time.Now().UTC().Add(time.Second)
	nearby, err := fetchSignalKNearbyVesselsLimit(state.Latitude, state.Longitude, now, nil, nearbyVesselsUnlimited)
	if err != nil || len(nearby) != 1 {
		t.Fatalf("expected one nearby vessel, got %+v (err=%v)", nearby, err)
	}
	recordNearbyVesselContacts(nearby, state, "", now)
	recordAISAnchorTracks(nearby, now)
	if got := countRows(t, contacts, "111111111"); got != 1 {
		t.Fatalf("expected the contact recorded, got %d", got)
	}
	if tracks := aisTracksNow(t, store, now); len(tracks) != 1 || tracks[0].MMSI != "111111111" || len(tracks[0].Points) != 1 {
		t.Fatalf("expected the track recorded from the same fetch, got %+v", tracks)
	}
}

func TestSampleTracksRecordsAISTracksDuringAWatch(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)

	seedSelfTree(t, trustedSignalKPayload(aisTestAnchorLat, aisTestAnchorLon))
	globalSignalKSnapshot.contexts[vesselContextPrefix+"urn:mrn:imo:mmsi:111111111"] = map[string]any{
		"name": "Alpha", "mmsi": "111111111",
		"navigation": map[string]any{"position": map[string]any{"value": map[string]any{"latitude": aisInsideLat, "longitude": aisTestAnchorLon}}},
	}
	// The position was received a moment after the drop.
	globalSignalKSnapshot.pathSeen[vesselContextPrefix+"urn:mrn:imo:mmsi:111111111|navigation.position"] = time.Now().UTC().Add(50 * time.Millisecond)

	time.Sleep(60 * time.Millisecond)
	sampleTracks(filepath.Join(t.TempDir(), "settings.yaml"))
	if tracks := aisTracksNow(t, store, time.Now().UTC()); len(tracks) != 1 || len(tracks[0].Points) != 1 {
		t.Fatalf("expected the poll tick to record the track, got %+v", tracks)
	}
}
