package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Two SignalK contexts can carry one MMSI. The recorder keeps the newest report
// per MMSI per tick, whichever order the list holds them in.
func TestAISAnchorTracksTwoContextsOfOneMMSIStoreTheNewestReportOnly(t *testing.T) {
	for _, newestFirst := range []bool{false, true} {
		store := aisEnv(t)
		dropWatch(t)
		t0 := aisBase()
		older := aisTarget("111111111", "ALPHA", aisInsideLat, t0)
		newer := aisTarget("111111111", "ALPHA", aisInsideLat+0.0001, t0.Add(4*time.Second))
		newer.ID = "urn:mrn:imo:mmsi:111111111-second"
		list := []nearbyVessel{older, newer}
		if newestFirst {
			list = []nearbyVessel{newer, older}
		}
		recordAISAnchorTracks(list, t0.Add(5*time.Second))
		pts := aisTracksNow(t, store, t0.Add(5*time.Second))[0].Points
		if len(pts) != 1 || pts[0].TMs != newer.PositionSeen.UnixMilli() {
			t.Fatalf("newestFirst=%v: expected only the newer report, got %+v", newestFirst, pts)
		}
		// A report between the two is older than the stored one and stores nothing.
		between := aisTarget("111111111", "ALPHA", aisInsideLat, t0.Add(2*time.Second))
		recordAISAnchorTracks([]nearbyVessel{between}, t0.Add(6*time.Second))
		if n := aisRowCount(t, store); n != 1 {
			t.Fatalf("newestFirst=%v: the last report time must be the newer one, got %d rows", newestFirst, n)
		}
	}
}

func TestAISAnchorTracksASharedMMSIIsInsideIfAnyContextIsInside(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	t0 := aisBase()
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	inside := aisTarget("111111111", "ALPHA", aisInsideLat, at(0))
	outside := aisTarget("111111111", "ALPHA", aisOutsideLat, at(0))
	outside.ID = "urn:mrn:imo:mmsi:111111111-second"
	// Listed after the inside one, the outside context must not win.
	recordAISAnchorTracks([]nearbyVessel{inside, outside}, at(0))
	next := aisTarget("111111111", "ALPHA", aisInsideLat+0.0001, at(5))
	recordAISAnchorTracks([]nearbyVessel{next}, at(5))
	pts := aisTracksNow(t, store, at(5))[0].Points
	if len(pts) != 2 || pts[1].NewSegment {
		t.Fatalf("one context inside keeps the target inside, so the line joins: %+v", pts)
	}
}

// A tick that read the watch as active can reach the database after Raise
// emptied it.
func TestAISAnchorTracksAWriteLandingAfterARaiseStoresNothing(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	if rec := raiseAnchor(t); rec.Code != http.StatusOK {
		t.Fatalf("raise: %d", rec.Code)
	}
	t0 := aisBase()
	stored, err := store.appendAISAnchorSamples([]aisAnchorSample{{mmsi: "111111111", lat: aisInsideLat, lon: aisTestAnchorLon, atMs: t0.UnixMilli(), newSegment: true}})
	if err != nil || stored {
		t.Fatalf("a write with no trail running must store nothing, got stored=%v err=%v", stored, err)
	}
	if n := aisRowCount(t, store); n != 0 {
		t.Fatalf("expected no rows after a raise, got %d", n)
	}
}

// A tick that took its samples before a raise and a fresh drop can reach the
// database after both; a report from before the new drop is not the new track.
func TestAISAnchorTracksAWriteOlderThanTheTrailStartStoresNothing(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	var startedMs int64
	if err := store.db.QueryRow(`SELECT started_ms FROM anchor_trail_state WHERE id = 1`).Scan(&startedMs); err != nil {
		t.Fatal(err)
	}
	if _, err := store.appendAISAnchorSamples([]aisAnchorSample{{mmsi: "111111111", lat: aisInsideLat, lon: aisTestAnchorLon, atMs: startedMs - 1, newSegment: true}}); err != nil {
		t.Fatal(err)
	}
	if n := aisRowCount(t, store); n != 0 {
		t.Fatalf("a report from before the drop must not be stored, got %d rows", n)
	}
}

// A point already in the table (an ambiguous commit, a recorder out of step)
// must not fail the tick for every other target.
func TestAISAnchorTracksADuplicateReportDoesNotFailTheTick(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	at := time.Now().Add(time.Minute).UnixMilli()
	first := aisAnchorSample{mmsi: "111111111", lat: aisInsideLat, lon: aisTestAnchorLon, atMs: at, newSegment: true}
	if _, err := store.appendAISAnchorSamples([]aisAnchorSample{first}); err != nil {
		t.Fatal(err)
	}
	other := aisAnchorSample{mmsi: "222222222", lat: aisInsideLat, lon: aisTestAnchorLon, atMs: at, newSegment: true}
	if _, err := store.appendAISAnchorSamples([]aisAnchorSample{first, other}); err != nil {
		t.Fatalf("a duplicate must not fail the tick: %v", err)
	}
	if n := aisRowCount(t, store); n != 2 {
		t.Fatalf("expected the other target stored beside the duplicate, got %d rows", n)
	}
}

func TestNearbyFetchFailureIsLoggedOncePerChangeOfState(t *testing.T) {
	nearbyFetchFailing.Store(false)
	t.Cleanup(func() { nearbyFetchFailing.Store(false) })
	var buf strings.Builder
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	boom := errors.New("signalk vessels unavailable")
	for i := 0; i < 3; i++ {
		noteNearbyFetch(boom)
	}
	if got := strings.Count(buf.String(), "signalk vessels unavailable"); got != 1 {
		t.Fatalf("expected one failure line across three failing ticks, got %d:\n%s", got, buf.String())
	}
	noteNearbyFetch(nil)
	noteNearbyFetch(nil)
	if got := strings.Count(buf.String(), "recovered"); got != 1 {
		t.Fatalf("expected one recovery line, got %d:\n%s", got, buf.String())
	}
	noteNearbyFetch(boom)
	if got := strings.Count(buf.String(), "signalk vessels unavailable"); got != 2 {
		t.Fatalf("a new failure after recovery is logged again, got %d:\n%s", got, buf.String())
	}
}
