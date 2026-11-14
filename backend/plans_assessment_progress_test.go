package main

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// What the assessment adds in ADR 0288: Done and Skipped legs carry no verdict
// or suggestion, each leg has a stable key, the plan's slack is stated, and a
// leg the boat should have left is flagged while it is still in port.

func TestAssessPlan_ADoneLegHasNoVerdictNoSuggestionAndIsNotRead(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	f := &paFake{progress: []string{planProgressDone, ""}}
	got := paAssess(t, p, legs, f)
	l := got.Legs[0]
	if l.Progress != planProgressDone || l.NoteKind != planKindNone || l.Note != "" || l.Verdict != "" || l.Depart != "" || l.Arrive != "" || l.DepartSuggested {
		t.Fatalf("done leg = %+v", l)
	}
	if got.Legs[1].Verdict == "" || got.Legs[1].Progress != "" {
		t.Fatalf("the leg after it is still judged: %+v", got.Legs[1])
	}
	if len(f.rankings) != 1 {
		t.Fatalf("a done leg is not ranked: %d rankings", len(f.rankings))
	}
}

func TestAssessPlan_ASkippedLegIsMarkedSkipped(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	l := paAssess(t, p, legs, &paFake{progress: []string{planProgressSkipped, ""}}).Legs[0]
	if l.Progress != planProgressSkipped || l.NoteKind != planKindNone || l.Note != "" || l.Verdict != "" || l.Depart != "" {
		t.Fatalf("skipped leg = %+v", l)
	}
}

func TestAssessPlan_ProgressThatCannotBeReadIsSaidAndNothingIsMarkedDone(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	got := paAssess(t, p, legs, &paFake{progressErr: errors.New("the passage record could not be read: disk")})
	if got.ProgressError != "the passage record could not be read: disk" {
		t.Fatalf("progress error = %q", got.ProgressError)
	}
	if got.Legs[0].Progress != "" || got.Legs[0].Verdict == "" {
		t.Fatalf("legs are judged as still to do: %+v", got.Legs[0])
	}
}

func TestAssessPlan_EachLegHasAKeyThatSurvivesAnEditElsewhere(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	got := paAssess(t, p, legs, &paFake{})
	if got.Legs[0].Key != "Great Sandy Straits Marina → Pelican Bay|2026-11-14" {
		t.Fatalf("key = %q", got.Legs[0].Key)
	}
	if got.Legs[0].Key == got.Legs[1].Key {
		t.Fatal("two legs share a key")
	}
	// Renaming a stop elsewhere in the plan leaves this leg's key alone.
	p.Stops[2].Name = "Elsewhere"
	again := paAssess(t, p, legs, &paFake{})
	if again.Legs[0].Key != got.Legs[0].Key {
		t.Fatalf("key moved from %q to %q", got.Legs[0].Key, again.Legs[0].Key)
	}
}

func TestAssessPlan_SlackIsStatedFromTheArriveBy(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0) // reaches the last stop on the 15th
	if got := paAssess(t, p, legs, &paFake{}); got.Slack != nil {
		t.Fatalf("no arrive-by, slack = %+v", got.Slack)
	}
	p.Stops[2].ArriveBy = "2026-11-17T17:00"
	got := paAssess(t, p, legs, &paFake{})
	if got.Slack == nil || got.Slack.Days != 2 || got.Slack.Stop != 2 {
		t.Fatalf("slack = %+v", got.Slack)
	}
}

// ── still in port ───────────────────────────────────────────────────────

func atStop(p plan, i int) func() (float64, float64, error) {
	return func() (float64, float64, error) { return p.Stops[i].Lat, p.Stops[i].Lon, nil }
}

func stillAt(t *testing.T, now time.Time, f *paFake, operatorTime string) planLegAssessment {
	t.Helper()
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[0].DepartTime = operatorTime
	if f.position == nil {
		f.position = atStop(p, 0)
	}
	d := paDeps(f)
	d.now = func() time.Time { return now }
	return assessPlan(t.Context(), p, legs, d, planAssessOptions{}).Legs[0]
}

func TestAssessPlan_StillInPortAnHourAfterTheOperatorsTime(t *testing.T) {
	if l := stillAt(t, paAt(14, 9, 0), &paFake{}, "08:00"); !l.StillInPort {
		t.Fatalf("an hour past 8 AM and still at the stop: %+v", l)
	}
	if l := stillAt(t, paAt(14, 8, 59), &paFake{}, "08:00"); l.StillInPort {
		t.Fatalf("59 minutes is not an hour: %+v", l)
	}
}

func TestAssessPlan_StillInPortNeedsThePositionAtTheStop(t *testing.T) {
	away := func() (float64, float64, error) { return -26.0, 153.0, nil }
	if l := stillAt(t, paAt(14, 10, 0), &paFake{position: away}, "08:00"); l.StillInPort {
		t.Fatalf("the boat is underway: %+v", l)
	}
	none := func() (float64, float64, error) { return 0, 0, errors.New("the boat has no usable position fix yet") }
	if l := stillAt(t, paAt(14, 10, 0), &paFake{position: none}, "08:00"); l.StillInPort {
		t.Fatalf("no position, no prompt: %+v", l)
	}
}

func TestAssessPlan_StillInPortOnlyOnTheDepartureDay(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 1, 0) // leaves on the 15th
	p.Stops[0].DepartTime = "08:00"
	f := &paFake{position: atStop(p, 0)}
	d := paDeps(f)
	d.now = func() time.Time { return paAt(14, 10, 0) }
	if l := assessPlan(t.Context(), p, legs, d, planAssessOptions{}).Legs[0]; l.StillInPort {
		t.Fatalf("a day early: %+v", l)
	}
}

func TestAssessPlan_ALegAlreadyDoneIsNotStillInPort(t *testing.T) {
	f := &paFake{progress: []string{planProgressDone, ""}}
	if l := stillAt(t, paAt(14, 10, 0), f, "08:00"); l.StillInPort {
		t.Fatalf("done leg flagged: %+v", l)
	}
}

func TestAssessPlan_ASuggestedLegCountsFromTheDaysFirstSuggestionAcrossARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "documents.sqlite")
	open := func() *documentStore {
		store, err := newDocumentStore(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { store.Close() })
		return store
	}
	first := open()
	// Before first light the suggestion is 6.15 AM.
	if l := stillAt(t, paAt(14, 4, 30), &paFake{store: first}, ""); l.StillInPort || !paParse(t, l.Depart).Equal(paAt(14, 6, 15)) {
		t.Fatalf("early: %+v", l)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	// After a restart, at 7.30 AM the ranking suggests a time after now, but the
	// day's first suggestion was an hour and a quarter ago.
	if l := stillAt(t, paAt(14, 7, 30), &paFake{store: open()}, ""); !l.StillInPort {
		t.Fatalf("an hour past the day's first suggestion: %+v", l)
	}
	// A box that never saw the morning has no reference and no prompt yet.
	if l := stillAt(t, paAt(14, 7, 30), &paFake{store: newTestDocumentStore(t)}, ""); l.StillInPort {
		t.Fatalf("no first suggestion remembered: %+v", l)
	}
}

func TestFirstSuggestion_KeepsTheFirstOfADayAndPrunesOldDays(t *testing.T) {
	store := newTestDocumentStore(t)
	at := func(day, h int) time.Time { return paAt(day, h, 0) }
	if got, err := store.FirstSuggestion(1, "A → B|2026-11-14", "2026-11-14", at(14, 6)); err != nil || !got.Equal(at(14, 6)) {
		t.Fatalf("first: %v %v", got, err)
	}
	if got, _ := store.FirstSuggestion(1, "A → B|2026-11-14", "2026-11-14", at(14, 9)); !got.Equal(at(14, 6)) {
		t.Fatalf("a later suggestion replaced the first: %v", got)
	}
	if got, _ := store.FirstSuggestion(2, "A → B|2026-11-14", "2026-11-14", at(14, 9)); !got.Equal(at(14, 9)) {
		t.Fatalf("another plan shares it: %v", got)
	}
	if got, _ := store.FirstSuggestion(1, "A → B|2026-11-17", "2026-11-17", at(17, 8)); !got.Equal(at(17, 8)) {
		t.Fatalf("a new day: %v", got)
	}
	// The 14th is now three days back and gone: it starts again.
	if got, _ := store.FirstSuggestion(1, "A → B|2026-11-14", "2026-11-14", at(14, 10)); !got.Equal(at(14, 10)) {
		t.Fatalf("an old day was kept: %v", got)
	}
}

func TestAssessPlan_AWindowThatHasClosedWithTheBoatStillInPortIsPastTheHour(t *testing.T) {
	// Sunset is 5.32 PM; at 6 PM the day's window is gone.
	l := stillAt(t, paAt(14, 18, 0), &paFake{}, "")
	if l.NoteKind != planKindWindowPassed || !l.StillInPort {
		t.Fatalf("leg = %+v", l)
	}
}
