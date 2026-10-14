package main

import (
	"testing"
	"time"
)

func appendAll(t *testing.T, s *passageStore, pts []recordedTrackPoint) {
	t.Helper()
	for _, p := range pts {
		if err := s.AppendTrackPoint(p); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRunnerStoresDetectedPassageOnce(t *testing.T) {
	store := newTestPassageStore(t)
	appendAll(t, store, simpleTrip(2*time.Hour).pts)
	for i := 0; i < 3; i++ {
		if err := runPassageDetection(store); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := store.ListPassages()
	if len(got) != 1 {
		t.Fatalf("want 1 stored passage after repeated runs, got %d", len(got))
	}
	if got[0].Hidden || got[0].Edited || got[0].CruiseID != nil || got[0].FromName != "" || got[0].ToName != "" {
		t.Errorf("fresh passage should be plain: %+v", got[0])
	}
}

func TestRunnerFollowsAPassageAcrossRuns(t *testing.T) {
	store := newTestPassageStore(t)
	b := simpleTrip(2 * time.Hour)
	appendAll(t, store, b.pts[:200])
	if err := runPassageDetection(store); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.ListPassages(); len(got) != 0 {
		t.Fatalf("a passage still under way must not be stored yet, got %d", len(got))
	}
	appendAll(t, store, b.pts[200:])
	if err := runPassageDetection(store); err != nil {
		t.Fatal(err)
	}
	got, _ := store.ListPassages()
	if len(got) != 1 || !got[0].Start.Equal(b.pts[2].At) {
		t.Fatalf("want one passage starting at the rest point, got %+v", got)
	}
}

func TestRunnerNeverRecreatesHiddenOrEditedPassage(t *testing.T) {
	for _, tc := range []struct {
		name           string
		hidden, edited bool
	}{{"hidden", true, false}, {"edited", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestPassageStore(t)
			b := simpleTrip(2 * time.Hour)
			// The operator already has this passage, trimmed to start later.
			_, err := store.InsertPassage(passageRecord{
				Start: b.pts[10].At, End: b.pts[300].At, Title: "Mine",
				Hidden: tc.hidden, Edited: tc.edited,
			})
			if err != nil {
				t.Fatal(err)
			}
			appendAll(t, store, b.pts)
			if err := runPassageDetection(store); err != nil {
				t.Fatal(err)
			}
			var n int
			store.db.QueryRow(`SELECT COUNT(*) FROM passages`).Scan(&n)
			if n != 1 {
				t.Fatalf("detector added a passage over the operator's own: %d rows", n)
			}
			p, _ := store.ListPassages()
			if !tc.hidden && (len(p) != 1 || p[0].Title != "Mine" || !p[0].Start.Equal(b.pts[10].At)) {
				t.Errorf("edited passage altered: %+v", p)
			}
		})
	}
}

func TestRunnerEmptyLogIsNotAnError(t *testing.T) {
	store := newTestPassageStore(t)
	if err := runPassageDetection(store); err != nil {
		t.Fatal(err)
	}
}
