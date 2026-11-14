package main

import (
	"errors"
	"path/filepath"
	"strings"
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

func TestAssessPlan_SlackIsLeftOutWhenTheProgressCouldNotBeRead(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[2].ArriveBy = "2026-11-20T12:00"
	got := paAssess(t, p, legs, &paFake{progressErr: errors.New("disk")})
	if got.ProgressError == "" || got.Slack != nil {
		t.Fatalf("progress error %q, slack %+v: slack must not be computed from unread progress", got.ProgressError, got.Slack)
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
	if got.Legs[0].Key != "Great+Sandy+Straits+Marina|Pelican+Bay|0" {
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

func TestAssessPlan_ABehindPlanStatesHowFarBehindAndWhetherItIsLate(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0) // reaches the last stop on the 15th
	p.Stops[2].ArriveBy = "2026-11-17T17:00"
	d := paDeps(&paFake{})
	d.now = func() time.Time { return paAt(16, 8, 0) } // leg 0 was to leave on the 14th
	got := assessPlan(t.Context(), p, legs, d, planAssessOptions{})
	// Two days behind: reaches the last stop on the 17th, the arrive-by day.
	if got.Slack == nil || got.Slack.BehindDays != 2 || got.Slack.Days != 0 {
		t.Fatalf("slack = %+v", got.Slack)
	}
	d.now = func() time.Time { return paAt(17, 8, 0) }
	got = assessPlan(t.Context(), p, legs, d, planAssessOptions{})
	if got.Slack == nil || got.Slack.BehindDays != 3 || got.Slack.Days != -1 {
		t.Fatalf("slack = %+v", got.Slack)
	}
}

func TestAssessPlan_ALegUnderWayIsNotCountedBehind(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0) // leg 0 leaves on the 14th, leg 1 on the 15th
	p.Stops[2].ArriveBy = "2026-11-17T17:00"
	d := paDeps(&paFake{underway: &planUnderway{Lat: p.Stops[0].Lat, Lon: p.Stops[0].Lon}})
	d.now = func() time.Time { return paAt(15, 8, 0) }
	got := assessPlan(t.Context(), p, legs, d, planAssessOptions{})
	if got.Slack == nil || got.Slack.BehindDays != 0 || got.Slack.Days != 2 {
		t.Fatalf("slack = %+v, want on time with 2 days", got.Slack)
	}
	d = paDeps(&paFake{})
	d.now = func() time.Time { return paAt(15, 8, 0) }
	got = assessPlan(t.Context(), p, legs, d, planAssessOptions{})
	if got.Slack == nil || got.Slack.BehindDays != 1 {
		t.Fatalf("slack = %+v, want 1 day behind with nothing under way", got.Slack)
	}
}

func TestAssessPlan_APassageUnderWayThatCannotBeReadIsSaidAndSlackIsLeftOut(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[2].ArriveBy = "2026-11-17T17:00"
	got := paAssess(t, p, legs, &paFake{underwayErr: errors.New("the passage record could not be read: disk")})
	if got.ProgressError == "" || got.Slack != nil {
		t.Fatalf("progress error %q, slack %+v", got.ProgressError, got.Slack)
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

func TestAssessPlan_StillInPortSaysSoWhenAStopsZoneCannotBeLoaded(t *testing.T) {
	p, _ := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[0].Timezone = "Mars/Olympus_Mons"
	d := paDeps(&paFake{position: atStop(p, 0)})
	out := planAssessment{Legs: make([]planLegAssessment, 2)}
	work := []*planLegWork{{res: &out.Legs[0]}, {res: &out.Legs[1]}}
	d.markStillInPort(p, work, []string{"", ""}, paAt(14, 10, 0), &out)
	if !strings.Contains(out.Problem, "time zone") || !strings.Contains(out.Problem, "Still in port") {
		t.Fatalf("problem = %q, want the zone said", out.Problem)
	}
	if out.Legs[0].StillInPort {
		t.Fatal("no prompt without a zone to read the day in")
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

// A read that finds the day's suggestion already kept writes nothing, and the old
// days are cleared once a day, not on every request.
func TestFirstSuggestion_WritesOnlyWhenTheLegHasNoRowAndPrunesOncePerDay(t *testing.T) {
	store := newTestDocumentStore(t)
	const day = "2026-11-14"
	staleRows := func() int {
		var n int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM plan_first_suggestions WHERE day < '2026-11-10'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	plantStale := func() {
		if _, err := store.db.Exec(`INSERT OR REPLACE INTO plan_first_suggestions (plan_id, leg_key, day, depart_ms) VALUES (9, 'old', '2026-11-01', 1)`); err != nil {
			t.Fatal(err)
		}
	}
	plantStale()
	if _, err := store.FirstSuggestion(1, "A → B|"+day, day, paAt(14, 6, 0)); err != nil {
		t.Fatal(err)
	}
	if staleRows() != 0 {
		t.Fatal("the first suggestion of the day did not clear the old days")
	}
	plantStale()
	if got, err := store.FirstSuggestion(1, "A → B|"+day, day, paAt(14, 9, 0)); err != nil || !got.Equal(paAt(14, 6, 0)) {
		t.Fatalf("kept = %v (%v)", got, err)
	}
	if staleRows() != 1 {
		t.Fatal("a request that found its row already kept wrote to the table")
	}
	// A leg with no row writes, but the day's pruning is done.
	if _, err := store.FirstSuggestion(1, "B → C|"+day, day, paAt(14, 9, 0)); err != nil {
		t.Fatal(err)
	}
	if staleRows() != 1 {
		t.Fatal("old days were cleared twice on one day")
	}
	// The next day clears them again.
	if _, err := store.FirstSuggestion(1, "A → B|2026-11-15", "2026-11-15", paAt(15, 6, 0)); err != nil {
		t.Fatal(err)
	}
	if staleRows() != 0 {
		t.Fatal("a new day did not clear the old days")
	}
}

func TestFirstSuggestion_KeptSuggestionReadsWithoutWriting(t *testing.T) {
	store := newTestDocumentStore(t)
	if _, ok, err := store.KeptSuggestion(1, "A → B|2026-11-14", "2026-11-14", paLoc); err != nil || ok {
		t.Fatalf("nothing kept: ok %v err %v", ok, err)
	}
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM plan_first_suggestions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a read wrote %d rows (%v)", n, err)
	}
	if _, err := store.FirstSuggestion(1, "A → B|2026-11-14", "2026-11-14", paAt(14, 6, 0)); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := store.KeptSuggestion(1, "A → B|2026-11-14", "2026-11-14", paLoc); err != nil || !ok || !got.Equal(paAt(14, 6, 0)) {
		t.Fatalf("kept = %v %v (%v)", got, ok, err)
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

func TestAssessPlan_AClosedWindowStillWaitsAnHourPastTheDaysFirstSuggestion(t *testing.T) {
	const key = "Great+Sandy+Straits+Marina|Pelican+Bay|0"
	// The boat was expected away at 5.30 PM; at 6 PM the window has gone, but the
	// hour is not up.
	store := newTestDocumentStore(t)
	if _, err := store.FirstSuggestion(1, key, "2026-11-14", paAt(14, 17, 30)); err != nil {
		t.Fatal(err)
	}
	if l := stillAt(t, paAt(14, 18, 0), &paFake{store: store}, ""); l.NoteKind != planKindWindowPassed || l.StillInPort {
		t.Fatalf("half an hour past the first suggestion: %+v", l)
	}
	// An hour on, it is.
	if l := stillAt(t, paAt(14, 18, 30), &paFake{store: store}, ""); !l.StillInPort {
		t.Fatalf("an hour past the first suggestion: %+v", l)
	}
	// With no suggestion kept today, the closed window alone counts.
	if l := stillAt(t, paAt(14, 18, 0), &paFake{store: newTestDocumentStore(t)}, ""); !l.StillInPort {
		t.Fatalf("no first suggestion kept: %+v", l)
	}
}
