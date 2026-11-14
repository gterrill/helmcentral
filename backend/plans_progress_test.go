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

func progPlan(t *testing.T) plan { return decoratedAt(t, "2026-11-14", 2, 1, 1, 1, 0) }

// decoratedAt is a plan as the handlers hand it on: with each stop's zone worked
// out.
func decoratedAt(t *testing.T, start string, nights ...int) plan {
	t.Helper()
	d, err := decoratePlan(planAt(t, start, nights...), progAt(14, 0))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// passage is a recorded passage from stop `from` of p to nm nautical miles north
// of stop `to`, ending at the given time. It begins at the stop itself.
func passage(p plan, from, to int, nm float64, at time.Time) planPassageEnd {
	return planPassageEnd{
		At: at, Lat: p.Stops[to].Lat + nm/60, Lon: p.Stops[to].Lon,
		StartLat: p.Stops[from].Lat, StartLon: p.Stops[from].Lon,
	}
}

func progAt(day, hour int) time.Time {
	return time.Date(2026, time.November, day, hour, 0, 0, 0, paLoc)
}

func sameProgress(a, b []string) bool { return sameStrings(a, b) }

func TestPlanProgress_APassageEndingAtTheDestinationMarksTheLegDone(t *testing.T) {
	p := progPlan(t)
	got, err := planProgressOf(p, []planPassageEnd{passage(p, 0, 1, 0.3, progAt(16, 15))})
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
	near, _ := planProgressOf(p, []planPassageEnd{passage(p, 0, 1, 1.4, progAt(16, 15))})
	far, _ := planProgressOf(p, []planPassageEnd{passage(p, 0, 1, 1.6, progAt(16, 15))})
	if near[0] != planProgressDone || far[0] != "" {
		t.Fatalf("1.4 nm gives %q and 1.6 nm gives %q", near[0], far[0])
	}
}

func TestPlanProgress_APassageBeforeThePlansFirstDayDoesNotCount(t *testing.T) {
	p := progPlan(t)
	// The plan starts on the 14th; an earlier visit to the same place is not this plan.
	got, _ := planProgressOf(p, []planPassageEnd{passage(p, 0, 1, 0, progAt(13, 23))})
	if got[0] != "" {
		t.Fatalf("progress = %q: a passage on the 13th must not count", got)
	}
	// The first day counts from its first minute, in the first stop's zone.
	got, _ = planProgressOf(p, []planPassageEnd{passage(p, 0, 1, 0, progAt(14, 0))})
	if got[0] != planProgressDone {
		t.Fatalf("progress = %q: a passage on the plan's first day counts", got)
	}
}

func TestPlanProgress_LeavingEarlyStillMarksTheLegDone(t *testing.T) {
	p := progPlan(t)
	// Leg 0 is planned for the 16th; the boat arrived at stop 1 on the 15th.
	got, _ := planProgressOf(p, []planPassageEnd{passage(p, 0, 1, 0.3, progAt(15, 15))})
	want := []string{planProgressDone, "", "", ""}
	if !sameProgress(got, want) {
		t.Fatalf("progress = %q, want %q", got, want)
	}
}

func TestPlanProgress_AShortcutOnTheFirstDaySkipsTheStopBetween(t *testing.T) {
	p := progPlan(t)
	got, _ := planProgressOf(p, []planPassageEnd{passage(p, 0, 2, 0.2, progAt(14, 16))})
	want := []string{planProgressSkipped, planProgressDone, "", ""}
	if !sameProgress(got, want) {
		t.Fatalf("progress = %q, want %q", got, want)
	}
}

func TestPlanProgress_APassageToAnEarlierStopAfterALaterOneDoesNotCount(t *testing.T) {
	p := progPlan(t)
	// Stop 2 is reached on the 15th, then the boat is back at stop 1 on the 16th:
	// stop 1 was passed over, not reached after the previous stop.
	got, _ := planProgressOf(p, []planPassageEnd{
		passage(p, 2, 1, 0, progAt(16, 9)),
		passage(p, 0, 2, 0, progAt(15, 9)),
	})
	want := []string{planProgressSkipped, planProgressDone, "", ""}
	if !sameProgress(got, want) {
		t.Fatalf("progress = %q, want %q", got, want)
	}
}

func TestPlanProgress_ALaterStopMarksTheStopsBetweenSkipped(t *testing.T) {
	p := progPlan(t)
	// Straight from stop 0 to stop 3: stops 1 and 2 are skipped, and so are the
	// legs into them and out of them that no passage ended.
	got, _ := planProgressOf(p, []planPassageEnd{passage(p, 0, 3, 0.2, progAt(18, 16))})
	want := []string{planProgressSkipped, planProgressSkipped, planProgressDone, ""}
	if !sameProgress(got, want) {
		t.Fatalf("progress = %q, want %q", got, want)
	}
}

func TestPlanProgress_AStopWithItsOwnPassageIsNotSkipped(t *testing.T) {
	p := progPlan(t)
	got, _ := planProgressOf(p, []planPassageEnd{
		passage(p, 0, 1, 0, progAt(16, 15)),
		passage(p, 1, 3, 0, progAt(18, 16)),
	})
	// Stop 1 was reached; stop 2 was passed by.
	want := []string{planProgressDone, planProgressSkipped, planProgressDone, ""}
	if !sameProgress(got, want) {
		t.Fatalf("progress = %q, want %q", got, want)
	}
}

// A round trip comes back to the stop it began at: A, B, A. Stop 2 is stop 0's
// place.
func roundTripPlan(t *testing.T) plan {
	p := progPlan(t)
	p.Stops[2].Lat, p.Stops[2].Lon = p.Stops[0].Lat, p.Stops[0].Lon
	return p
}

func TestPlanProgress_AnArrivalIntoTheStartHarbourOnDayOneIsNotTheRoundTripHome(t *testing.T) {
	p := roundTripPlan(t)
	// The boat comes into the start harbour from elsewhere on the plan's first day.
	// It began at stop 1's place, not at the last stop reached (stop 0), so it
	// belongs to nothing in the plan.
	got, _ := planProgressOf(p, []planPassageEnd{passage(p, 1, 2, 0.2, progAt(14, 10))})
	if !sameProgress(got, []string{"", "", "", ""}) {
		t.Fatalf("progress = %q: the round trip is not finished", got)
	}
}

func TestPlanProgress_AShortMoveInsideTheStartHarbourDoesNotCount(t *testing.T) {
	p := roundTripPlan(t)
	// Fuel dock to the berth, a mile apart: it ends within 1.5 nm of where it began.
	got, _ := planProgressOf(p, []planPassageEnd{passage(p, 0, 2, 1.0, progAt(14, 10))})
	if !sameProgress(got, []string{"", "", "", ""}) {
		t.Fatalf("progress = %q: a harbour shuffle is not the way home", got)
	}
}

func TestPlanProgress_ARoundTripIsFinishedWhenTheBoatSailsOutAndBack(t *testing.T) {
	p := roundTripPlan(t)
	got, _ := planProgressOf(p, []planPassageEnd{
		passage(p, 0, 1, 0.2, progAt(16, 10)),
		passage(p, 1, 2, 0.2, progAt(17, 10)),
	})
	want := []string{planProgressDone, planProgressDone, "", ""}
	if !sameProgress(got, want) {
		t.Fatalf("progress = %q, want %q", got, want)
	}
}

func TestPlanProgress_APassageThatBeganAwayFromTheLastStopReachedDoesNotCount(t *testing.T) {
	p := progPlan(t)
	// The boat left stop 1's place, not stop 0's, so it did not sail the plan's leg.
	got, _ := planProgressOf(p, []planPassageEnd{passage(p, 1, 2, 0, progAt(16, 10))})
	if !sameProgress(got, []string{"", "", "", ""}) {
		t.Fatalf("progress = %q", got)
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
	if got[0].StartLat != -25.0 || got[0].StartLon != 153.0 {
		t.Fatalf("start = %v, %v, want the passage's first fix", got[0].StartLat, got[0].StartLon)
	}
}

func TestReadUnderway_APassageStillBeingRecordedIsNotAnEndButIsWhereItBegan(t *testing.T) {
	store := newTestPassageStore(t)
	b := newTrackBuilder().run(10*time.Minute, time.Minute, 0).run(40*time.Minute, time.Minute, 6)
	for _, pt := range b.pts {
		if err := store.AppendTrackPoint(pt); err != nil {
			t.Fatal(err)
		}
	}
	// The run has not ended, so it is not in the passage record.
	ends, err := readPassageEnds(context.Background(), store, passageEpoch.AddDate(0, 0, -1))
	if err != nil || len(ends) != 0 {
		t.Fatalf("ends = %+v (%v): a passage still being recorded marks nothing", ends, err)
	}
	got, err := readUnderway(context.Background(), store)
	if err != nil || got == nil {
		t.Fatalf("underway = %+v (%v), want the open run", got, err)
	}
	if !withinArrivedNm(got.Lat, got.Lon, -20.0, 149.0) {
		t.Fatalf("underway began at %v, %v, want near -20, 149", got.Lat, got.Lon)
	}
}

func TestReadUnderway_ABoatAtRestIsNotUnderway(t *testing.T) {
	store := newTestPassageStore(t)
	for _, pt := range newTrackBuilder().run(30*time.Minute, time.Minute, 0).pts {
		if err := store.AppendTrackPoint(pt); err != nil {
			t.Fatal(err)
		}
	}
	got, err := readUnderway(context.Background(), store)
	if err != nil || got != nil {
		t.Fatalf("underway = %+v (%v), want none", got, err)
	}
}

func TestReadUnderway_NoStoreIsAnErrorNotNoPassage(t *testing.T) {
	if _, err := readUnderway(context.Background(), nil); err == nil {
		t.Fatal("no passage store must be an error")
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
