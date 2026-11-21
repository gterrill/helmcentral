package main

import (
	"bytes"
	"log"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

func sidewaysPassage(t *testing.T) (*passageStore, passageRecord) {
	t.Helper()
	store := withPassageStore(t)
	b := simpleTrip(2 * time.Hour)
	b.pts[100].RollAccelPeakDps2 = pfloat(50)
	b.pts[200].RollAccelPeakDps2 = pfloat(100)
	appendAll(t, store, b.pts)
	rec, err := store.InsertPassage(passageRecord{Start: b.pts[2].At, End: b.pts[360].At, Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	return store, rec
}

func withHelmHeight(t *testing.T, height *float64) {
	t.Helper()
	docs := newTestDocumentStore(t)
	prev := globalDocumentStore
	globalDocumentStore = docs
	t.Cleanup(func() { globalDocumentStore = prev })
	if _, err := docs.SetVesselParticulars(vesselParticulars{HelmHeightM: height}); err != nil {
		t.Fatal(err)
	}
}

func sidewaysOf(t *testing.T, id string) map[string]any {
	t.Helper()
	code, body := getJSON(t, getPassageHandler, "/api/passages/x", id)
	if code != 200 {
		t.Fatal(code, body)
	}
	return body["passage"].(map[string]any)["sideways_g"].(map[string]any)
}

func TestPassageDetailSidewaysGFollowsTheHelmHeightAndIsNotStored(t *testing.T) {
	_, rec := sidewaysPassage(t)

	withHelmHeight(t, pfloat(3))
	g := sidewaysOf(t, rec.ID)
	want3 := 3 * (100 * math.Pi / 180) / 9.80665
	if v, _ := g["peak_g"].(float64); math.Abs(v-want3) > 1e-9 {
		t.Fatalf("peak at 3 m %v, want %v: %v", g["peak_g"], want3, g)
	}
	if g["peak_at"] == nil {
		t.Errorf("the peak needs its time: %v", g)
	}

	// The same passage, a different height: the figure moves, nothing was stored.
	if _, err := globalDocumentStore.SetVesselParticulars(vesselParticulars{HelmHeightM: pfloat(6)}); err != nil {
		t.Fatal(err)
	}
	g = sidewaysOf(t, rec.ID)
	if v, _ := g["peak_g"].(float64); math.Abs(v-2*want3) > 1e-9 {
		t.Errorf("peak at 6 m %v, want %v", g["peak_g"], 2*want3)
	}
}

func TestPassageDetailSidewaysGSaysWhyWhenTheHeightIsBlank(t *testing.T) {
	_, rec := sidewaysPassage(t)
	withHelmHeight(t, nil)
	g := sidewaysOf(t, rec.ID)
	if g["peak_g"] != nil {
		t.Errorf("no height, no figure: %v", g)
	}
	if r, _ := g["reason"].(string); r == "" {
		t.Errorf("a missing figure needs its reason: %v", g)
	}
}

func TestPassageDetailSidewaysGUnreadableHeightGivesPlainReasonAndLogsOncePerEpisode(t *testing.T) {
	_, rec := sidewaysPassage(t)
	prev := globalDocumentStore
	globalDocumentStore = nil // the height cannot be read
	t.Cleanup(func() { globalDocumentStore = prev })
	clock := &testClock{t: motionT0}
	prevLog := helmHeightLog
	helmHeightLog = &onceAnHour{now: clock.now}
	t.Cleanup(func() { helmHeightLog = prevLog })
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	for i := 0; i < 3; i++ {
		g := sidewaysOf(t, rec.ID)
		want := "The helm height couldn't be read, so sideways g isn't shown."
		if g["reason"] != want {
			t.Fatalf("reason %q, want %q", g["reason"], want)
		}
	}
	if n := strings.Count(buf.String(), "helm height"); n != 1 {
		t.Errorf("want one log line for the episode, got %d: %q", n, buf.String())
	}
	if !strings.Contains(buf.String(), "vessel details store is not running") {
		t.Errorf("the log keeps the error detail: %q", buf.String())
	}

	clock.advance(61 * time.Minute)
	sidewaysOf(t, rec.ID)
	if n := strings.Count(buf.String(), "helm height"); n != 2 {
		t.Errorf("an hour on, the failure is logged again: got %d lines: %q", n, buf.String())
	}
}

// sidewaysReasonFor records a passage whose points are shaped by mutate and
// returns the reason sideways g gives.
func sidewaysReasonFor(t *testing.T, mutate func(pts []recordedTrackPoint)) string {
	t.Helper()
	store := withPassageStore(t)
	b := simpleTrip(2 * time.Hour)
	mutate(b.pts)
	appendAll(t, store, b.pts)
	rec, _ := store.InsertPassage(passageRecord{Start: b.pts[2].At, End: b.pts[360].At, Title: "t"})
	withHelmHeight(t, pfloat(3))
	g := sidewaysOf(t, rec.ID)
	if g["peak_g"] != nil {
		t.Fatalf("want no figure: %v", g)
	}
	r, _ := g["reason"].(string)
	return r
}

func TestPassageDetailSidewaysGSaysNoMotionWasRecordedWhenNoPointHasAny(t *testing.T) {
	r := sidewaysReasonFor(t, func(pts []recordedTrackPoint) {})
	if !strings.Contains(r, "No motion was recorded") {
		t.Errorf("reason %q", r)
	}
}

func TestPassageDetailSidewaysGSaysTheFeedWasBelowEightHertz(t *testing.T) {
	r := sidewaysReasonFor(t, func(pts []recordedTrackPoint) {
		for i := range pts {
			pts[i].MotionRateHz = pfloat(5)
			pts[i].RollRMSDeg = pfloat(1)
		}
	})
	if !strings.Contains(r, "below 8 Hz") {
		t.Errorf("reason %q", r)
	}
}

func TestPassageDetailSidewaysGSaysAnImportedPassageHasNoMotionFigures(t *testing.T) {
	r := sidewaysReasonFor(t, func(pts []recordedTrackPoint) {
		for i := range pts {
			pts[i].Source = trackSourceImported
		}
	})
	if !strings.Contains(r, "imported") || strings.Contains(r, "attitude sensor") {
		t.Errorf("reason %q", r)
	}
}

// One slow window among ten-hertz ones is not a slow feed.
func TestPassageDetailSidewaysGCallsTheFeedSlowOnlyWhenMostRatesAreUnderTheFloor(t *testing.T) {
	r := sidewaysReasonFor(t, func(pts []recordedTrackPoint) {
		for i := range pts {
			pts[i].MotionRateHz = pfloat(10)
		}
		pts[100].MotionRateHz = pfloat(5)
	})
	if strings.Contains(r, "below 8 Hz") || !strings.Contains(r, "no point has sideways g") {
		t.Errorf("reason %q", r)
	}
}

// The first point's window is before the passage, so its rate says nothing.
func TestPassageDetailSidewaysGIgnoresTheFirstPointsRate(t *testing.T) {
	r := sidewaysReasonFor(t, func(pts []recordedTrackPoint) {
		pts[2].MotionRateHz = pfloat(5)
	})
	if !strings.Contains(r, "No motion was recorded") {
		t.Errorf("reason %q", r)
	}
}

func TestPassageDetailSidewaysGSaysItWasRecordedBeforeTenHertzCapture(t *testing.T) {
	r := sidewaysReasonFor(t, func(pts []recordedTrackPoint) {
		for i := range pts {
			pts[i].RollPeakDeg = pfloat(4)
		}
	})
	if !strings.Contains(r, "before 10 Hz") {
		t.Errorf("reason %q", r)
	}
}
