package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestAISAnchorTracksStoreAtMostOnePointEveryThirtySeconds(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	t0 := aisBase()
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	// A Class A boat reporting every 5 seconds for a minute.
	for s := 0; s <= 60; s += 5 {
		recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, at(s))}, at(s))
	}
	pts := aisTracksNow(t, store, at(60))[0].Points
	want := []int64{at(0).UnixMilli(), at(30).UnixMilli(), at(60).UnixMilli()}
	if len(pts) != len(want) {
		t.Fatalf("expected points at 0, 30 and 60 s, got %+v", pts)
	}
	for i, p := range pts {
		if p.TMs != want[i] {
			t.Fatalf("point %d at %d, want %d (%+v)", i, p.TMs, want[i], pts)
		}
	}
}

func TestAISAnchorTracksStoreEveryReportOfAMovingBoat(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	t0 := aisBase()
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	// About 5 m/s heading south, reporting every 5 seconds: each report is
	// just over 25 m from the last, so none is thinned.
	for i := 0; i < 8; i++ {
		recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat-float64(i)*0.00023, at(i*5))}, at(i*5))
	}
	if pts := aisTracksNow(t, store, at(40))[0].Points; len(pts) != 8 {
		t.Fatalf("a mover keeps every report, got %d points", len(pts))
	}
}

func TestAISAnchorTracksStoreAReportThatMovedTwentyFiveMetresInsideThirtySeconds(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	t0 := aisBase()
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, at(0))}, at(0))
	// About 11 m away, 10 s later: thinned.
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat-0.0001, at(10))}, at(10))
	// About 28 m from the stored point, 20 s later: stored.
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat-0.00025, at(20))}, at(20))
	pts := aisTracksNow(t, store, at(20))[0].Points
	if len(pts) != 2 || pts[1].TMs != at(20).UnixMilli() {
		t.Fatalf("expected points at 0 and 20 s, got %+v", pts)
	}
}

func TestAISAnchorTracksForgetAPendingSegmentOnceTheTargetIsSeenOutside(t *testing.T) {
	aisEnv(t)
	dropWatch(t)
	t0 := aisBase()
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, at(0))}, at(0))
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisOutsideLat, at(10))}, at(10))
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, at(20))}, at(20)) // skipped, pending
	aisTracks.mu.Lock()
	pending := len(aisTracks.pendingSegment)
	aisTracks.mu.Unlock()
	if pending != 1 {
		t.Fatalf("expected one pending target, got %d", pending)
	}
	// It clips the circle and leaves again before a point is stored.
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisOutsideLat, at(25))}, at(25))
	aisTracks.mu.Lock()
	pending = len(aisTracks.pendingSegment)
	aisTracks.mu.Unlock()
	if pending != 0 {
		t.Fatalf("a target seen outside must not stay pending, got %d", pending)
	}
}

func TestAISAnchorTracksStoreEveryReportOfASlowReporter(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	t0 := aisBase()
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	for m := 0; m <= 12; m += 3 {
		recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, at(m))}, at(m))
	}
	if pts := aisTracksNow(t, store, at(12))[0].Points; len(pts) != 5 {
		t.Fatalf("a 3 minute reporter keeps every report, got %d points", len(pts))
	}
}

func TestAISAnchorTracksASkippedReEntryReportStillFlagsTheNextStoredPoint(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	t0 := aisBase()
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, at(0))}, at(0))
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisOutsideLat, at(10))}, at(10))
	// Back inside 20 s after the last stored point: skipped by the 30 s rule.
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, at(20))}, at(20))
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat+0.0001, at(40))}, at(40))
	pts := aisTracksNow(t, store, at(40))[0].Points
	if len(pts) != 2 || pts[1].TMs != at(40).UnixMilli() {
		t.Fatalf("expected points at 0 and 40 s, got %+v", pts)
	}
	if !pts[0].NewSegment || !pts[1].NewSegment {
		t.Fatalf("the point stored after re-entry must start a new segment, got %+v", pts)
	}
	// Once stored, the flag is spent.
	recordAISAnchorTracks([]nearbyVessel{aisTarget("111111111", "ALPHA", aisInsideLat, at(75))}, at(75))
	pts = aisTracksNow(t, store, at(75))[0].Points
	if len(pts) != 3 || pts[2].NewSegment {
		t.Fatalf("the flag must clear once a point carries it, got %+v", pts)
	}
}

// Both trail endpoints say what time the server thinks it is, so the map can
// age the trails by the boat's clock rather than the browser's.
func TestTrailEndpointsCarryTheServersClock(t *testing.T) {
	aisEnv(t)
	check := func(label, raw string, beforeMs, afterMs int64) {
		t.Helper()
		var body struct {
			ServerTimeMs *int64 `json:"server_time_ms"`
		}
		if err := json.Unmarshal([]byte(raw), &body); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if body.ServerTimeMs == nil || *body.ServerTimeMs < beforeMs || *body.ServerTimeMs > afterMs {
			t.Fatalf("%s: server_time_ms missing or outside [%d, %d]: %s", label, beforeMs, afterMs, raw)
		}
	}
	getSelfRaw := func() string {
		rec := httptest.NewRecorder()
		if err := getSelfTrailHandler(echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/anchor-watch/trails/self", nil), rec)); err != nil {
			t.Fatal(err)
		}
		return rec.Body.String()
	}
	run := func(phase string) {
		before := time.Now().UnixMilli()
		_, _, aisRaw := getAISTrailsBody(t, "")
		selfRaw := getSelfRaw()
		after := time.Now().UnixMilli()
		check(phase+" ais", aisRaw, before, after)
		check(phase+" self", selfRaw, before, after)
	}
	run("no watch")
	dropWatch(t)
	run("watch")
}
