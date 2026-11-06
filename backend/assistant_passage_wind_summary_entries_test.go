package main

import (
	"testing"
	"time"
)

// Every entry is judged on its own weight: two slivers, one at each end, are two
// end markers, not 0.04 h of hours to place at one end.
func TestWindSummarySliversAtBothEndsAreEndMarkers(t *testing.T) {
	depart := pwDay(20, 6).Add(27*time.Minute + 30*time.Second)
	arrive := depart.Add(2*time.Hour + 5*time.Minute)
	w := newPassageWindTally(2.0833, 1)
	w.add(pwDay(20, 6), depart, arrive, 0, 10, "ahead", "A → B")
	w.add(pwDay(20, 7), depart, arrive, 0, 10, "astern", "A → B")
	w.add(pwDay(20, 8), depart, arrive, 0, 10, "astern", "A → B")
	w.add(pwDay(20, 9), depart, arrive, 0, 10, "ahead", "A → B")
	if got, want := w.summary("Leg"), "Wind from N 10 kn: astern 2.0 h, ahead at departure and arrival"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

// Each gap is judged on its own size: two slivers are two end markers even when
// together they would print as hours.
func TestWindSummaryEachGapIsJudgedOnItsOwn(t *testing.T) {
	depart := pwDay(20, 6).Add(27*time.Minute + 30*time.Second)
	arrive := depart.Add(2*time.Hour + 5*time.Minute)
	w := newPassageWindTally(2.0833, 1)
	w.gap(pwDay(20, 6), depart, arrive)
	w.add(pwDay(20, 7), depart, arrive, 0, 10, "astern", "A → B")
	w.add(pwDay(20, 8), depart, arrive, 0, 10, "astern", "A → B")
	w.gap(pwDay(20, 9), depart, arrive)
	if got, want := w.summary("Leg"), "Wind from N 10 kn: astern 2.0 h, no forecast at departure and arrival"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

// A calm hour inside a run doesn't break it, and the position text says the run
// spans the calm hour.
func TestWindSummaryCalmInsideARunSaysSo(t *testing.T) {
	depart := pwDay(20, 6)
	arrive := depart.Add(6 * time.Hour)
	w := newPassageWindTally(6, 1)
	for i, step := range []struct {
		sector string
		kn     float64
	}{{"ahead", 10}, {"ahead", 0}, {"ahead", 10}, {"astern", 10}, {"astern", 10}, {"astern", 10}} {
		w.add(depart.Add(time.Duration(i)*time.Hour+30*time.Minute), depart, arrive, 0, step.kn, step.sector, "A → B")
	}
	want := "Wind from N 0–10 kn: astern 3.0 h, ahead 2.0 h (within the first 3.0 h), calm 1.0 h (1.0–2.0 h in)"
	if got := w.summary("Leg"); got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}
