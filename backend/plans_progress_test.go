package main

import (
	"context"
	"testing"
	"time"
)

// A leg is Done when a recorded passage ends within 1.5 nm of its destination
// on or after the leg's date. A passage that ends at a later stop marks the
// stops in between, and the legs into them, Skipped. Plans here arrive 14, 16,
// 17, 18 and 19 November, so the legs leave on the 16th, 17th, 18th and 19th.

func progPlan(t *testing.T) plan { return planAt(t, "2026-11-14", 2, 1, 1, 1, 0) }

// endAt is a passage ending at stop i of p, nm nautical miles north of it, at
// the given time.
func endAt(p plan, i int, nm float64, at time.Time) planPassageEnd {
	return planPassageEnd{At: at, Lat: p.Stops[i].Lat + nm/60, Lon: p.Stops[i].Lon}
}

func progAt(day, hour int) time.Time {
	return time.Date(2026, time.November, day, hour, 0, 0, 0, paLoc)
}

func sameProgress(a, b []string) bool { return sameStrings(a, b) }

func TestPlanProgress_APassageEndingAtTheDestinationMarksTheLegDone(t *testing.T) {
	p := progPlan(t)
	got, err := planProgressOf(p, []planPassageEnd{endAt(p, 1, 0.3, progAt(16, 15))})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{planProgressDone, "", "", ""}
	if !sameProgress(got, want) {
		t.Fatalf("progress = %q, want %q", got, want)
	}
}

func TestPlanProgress_WithinOneAndAHalfMilesAndNoFurther(t *testing.T) {
	p := progPlan(t)
	near, _ := planProgressOf(p, []planPassageEnd{endAt(p, 1, 1.4, progAt(16, 15))})
	far, _ := planProgressOf(p, []planPassageEnd{endAt(p, 1, 1.6, progAt(16, 15))})
	if near[0] != planProgressDone || far[0] != "" {
		t.Fatalf("1.4 nm gives %q and 1.6 nm gives %q", near[0], far[0])
	}
}

func TestPlanProgress_APassageBeforeTheLegsDateDoesNotCount(t *testing.T) {
	p := progPlan(t)
	// Leg 0 leaves on the 16th; an earlier visit to the same place is not this leg.
	got, _ := planProgressOf(p, []planPassageEnd{endAt(p, 1, 0, progAt(15, 15))})
	if got[0] != "" {
		t.Fatalf("progress = %q: a passage on the 15th must not do a leg of the 16th", got)
	}
	// The leg's own day counts, judged at the stop the leg leaves.
	got, _ = planProgressOf(p, []planPassageEnd{endAt(p, 1, 0, progAt(16, 0))})
	if got[0] != planProgressDone {
		t.Fatalf("progress = %q: a passage on the leg's day is on or after it", got)
	}
}

func TestPlanProgress_ALaterStopMarksTheStopsBetweenSkipped(t *testing.T) {
	p := progPlan(t)
	// Straight from stop 0 to stop 3: stops 1 and 2 are skipped, and so are the
	// legs into them and out of them that no passage ended.
	got, _ := planProgressOf(p, []planPassageEnd{endAt(p, 3, 0.2, progAt(18, 16))})
	want := []string{planProgressSkipped, planProgressSkipped, planProgressDone, ""}
	if !sameProgress(got, want) {
		t.Fatalf("progress = %q, want %q", got, want)
	}
}

func TestPlanProgress_AStopWithItsOwnPassageIsNotSkipped(t *testing.T) {
	p := progPlan(t)
	got, _ := planProgressOf(p, []planPassageEnd{
		endAt(p, 1, 0, progAt(16, 15)),
		endAt(p, 3, 0, progAt(18, 16)),
	})
	// Stop 1 was reached; stop 2 was passed by.
	want := []string{planProgressDone, planProgressSkipped, planProgressDone, ""}
	if !sameProgress(got, want) {
		t.Fatalf("progress = %q, want %q", got, want)
	}
}

func TestPlanProgress_NoPassagesIsNothingDone(t *testing.T) {
	p := progPlan(t)
	got, err := planProgressOf(p, nil)
	if err != nil || !sameProgress(got, []string{"", "", "", ""}) {
		t.Fatalf("progress = %q, %v", got, err)
	}
}

// ── reading the recorded passages ───────────────────────────────────────

func TestReadPassageEnds_NoStoreIsAnErrorNotNoPassages(t *testing.T) {
	if _, err := readPassageEnds(context.Background(), nil, progAt(1, 0)); err == nil {
		t.Fatal("no passage store must be an error, not an empty list")
	}
}

func TestReadPassageEnds_IsTheLastFixOfEachRecentVisiblePassage(t *testing.T) {
	store := newTestPassageStore(t)
	add := func(id string, start, end time.Time, hidden bool, endLat float64) {
		t.Helper()
		for _, pt := range []recordedTrackPoint{
			{At: start, Lat: -25.0, Lon: 153.0},
			{At: end.Add(-time.Minute), Lat: -25.1, Lon: 153.0},
			{At: end, Lat: endLat, Lon: 153.1},
		} {
			if err := store.AppendTrackPoint(pt); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := store.InsertPassage(passageRecord{ID: id, Start: start, End: end, Hidden: hidden}); err != nil {
			t.Fatal(err)
		}
	}
	add("recent", progAt(16, 8), progAt(16, 13), false, -25.5)
	add("old", progAt(2, 8), progAt(2, 13), false, -26.0)
	add("hidden", progAt(17, 8), progAt(17, 13), true, -27.0)
	got, err := readPassageEnds(context.Background(), store, progAt(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Lat != -25.5 || got[0].Lon != 153.1 || !got[0].At.Equal(progAt(16, 13)) {
		t.Fatalf("ends = %+v, want only the recent visible passage's last fix", got)
	}
}

func TestReadPassageEnds_APassageWithNoFixIsLeftOutAndSaid(t *testing.T) {
	store := newTestPassageStore(t)
	if _, err := store.InsertPassage(passageRecord{ID: "empty", Start: progAt(16, 8), End: progAt(16, 13)}); err != nil {
		t.Fatal(err)
	}
	got, err := readPassageEnds(context.Background(), store, progAt(10, 0))
	if err != nil || len(got) != 0 {
		t.Fatalf("ends = %+v, %v", got, err)
	}
}
