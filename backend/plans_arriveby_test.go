package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// An arrive-by is an optional date and time on any stop, "YYYY-MM-DDTHH:MM" at
// that stop. Slack is the whole days between the stop's planned arrival and its
// arrive-by (ADR 0288).

func TestNormalisePlan_ArriveByIsKeptOrRefused(t *testing.T) {
	good := validPlan()
	good.Stops[4].ArriveBy = "2026-11-21T17:00"
	got, verr := normalisePlan(good)
	if verr != nil || got.Stops[4].ArriveBy != "2026-11-21T17:00" {
		t.Fatalf("arrive-by = %q, err %v", got.Stops[4].ArriveBy, verr)
	}
	// Any stop may carry one, not only the last.
	mid := validPlan()
	mid.Stops[2].ArriveBy = "2026-11-17T09:30"
	if _, verr := normalisePlan(mid); verr != nil {
		t.Fatalf("a middle stop's arrive-by: %v", verr)
	}
	for _, bad := range []string{"2026-11-21", "2026-11-21 17:00", "2026-11-21T25:00", "2026-02-30T10:00", "tomorrow"} {
		p := validPlan()
		p.Stops[4].ArriveBy = bad
		_, verr := normalisePlan(p)
		if verr == nil || verr.Field != "arrive_by" {
			t.Errorf("arrive-by %q: err = %v, want a refusal on arrive_by", bad, verr)
		}
	}
}

// The plan starts where the boat already is, so the first stop is never
// "reached" and cannot carry an arrive-by.
func TestNormalisePlan_NoArriveByOnTheFirstStop(t *testing.T) {
	p := validPlan()
	p.Stops[0].ArriveBy = "2026-11-14T09:00"
	_, verr := normalisePlan(p)
	if verr == nil || verr.Field != "arrive_by" {
		t.Fatalf("first stop arrive-by: err = %v, want a refusal on arrive_by", verr)
	}
}

func TestPlanStore_ArriveByRoundTripsAndSurvivesAPatch(t *testing.T) {
	store := newTestDocumentStore(t)
	in := validPlan()
	in.Stops[4].ArriveBy = "2026-11-21T17:00"
	saved, err := store.CreatePlan(in)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Stops[4].ArriveBy != "2026-11-21T17:00" {
		t.Fatalf("saved arrive-by = %q", saved.Stops[4].ArriveBy)
	}
	one := 3
	patched, ok, err := store.PatchPlanStop(saved.ID, 1, nil, &one)
	if err != nil || !ok {
		t.Fatalf("patch: %v %v", ok, err)
	}
	if patched.Stops[4].ArriveBy != "2026-11-21T17:00" {
		t.Fatalf("a night added upstream lost the arrive-by: %q", patched.Stops[4].ArriveBy)
	}
	listed, err := store.ListPlans()
	if err != nil || listed[0].Stops[4].ArriveBy != "2026-11-21T17:00" {
		t.Fatalf("listed = %+v, %v", listed, err)
	}
}

func TestPlanSlack_IsWholeDaysFromPlannedArrivalToArriveBy(t *testing.T) {
	p := planAt(t, "2026-11-14", 2, 1, 1, 1, 0) // arrives 14, 16, 17, 18, 19
	p.Stops[4].ArriveBy = "2026-11-20T17:00"
	s, ok, _ := planSlackOf(p, nil, 0)
	if !ok || s.Days != 1 || s.Stop != 4 {
		t.Fatalf("slack = %+v %v, want 1 day at stop 4", s, ok)
	}
	// Late: arrive-by is the day before the planned arrival.
	p.Stops[4].ArriveBy = "2026-11-18T23:00"
	s, _, _ = planSlackOf(p, nil, 0)
	if s.Days != -1 {
		t.Fatalf("late slack = %+v, want -1", s)
	}
	// With none anywhere there is no slack to speak of.
	p.Stops[4].ArriveBy = ""
	if _, ok, _ := planSlackOf(p, nil, 0); ok {
		t.Fatal("no arrive-by must give no slack")
	}
}

func TestPlanSlack_AMalformedStoredArriveByIsAProblemNotAPanic(t *testing.T) {
	for _, bad := range []string{"2026-1", "x", "2026-13-45T10:00", "not a date at all"} {
		p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
		p.Stops[2].ArriveBy = bad
		got := paAssess(t, p, legs, &paFake{})
		if got.Slack != nil || !strings.Contains(got.Problem, "arrive-by") {
			t.Fatalf("%q: slack %+v, problem %q", bad, got.Slack, got.Problem)
		}
		slips := slipsOf(context.Background(), p, legs, paDeps(&paFake{}))
		if !strings.Contains(slips.Problem, "arrive-by") || len(slips.Slips) != 0 {
			t.Fatalf("%q: slips = %+v", bad, slips)
		}
	}
}

func TestPlanSlack_TheTightestStopRulesAndAReachedStopIsLeftOut(t *testing.T) {
	p := planAt(t, "2026-11-14", 2, 1, 1, 1, 0)
	p.Stops[2].ArriveBy = "2026-11-18T12:00" // arrives 17: slack 1
	p.Stops[4].ArriveBy = "2026-11-25T12:00" // arrives 19: slack 6
	s, _, _ := planSlackOf(p, nil, 0)
	if s.Days != 1 || s.Stop != 2 {
		t.Fatalf("slack = %+v, want the tighter stop 2", s)
	}
	// Legs 0 and 1 done: the boat is at stop 2, whose arrive-by no longer binds.
	done := []string{planProgressDone, planProgressDone, "", ""}
	s, _, _ = planSlackOf(p, done, 0)
	if s.Days != 6 || s.Stop != 4 {
		t.Fatalf("slack with stop 2 reached = %+v, want stop 4's 6 days", s)
	}
}

// A plan that has fallen behind is projected forward from today (ADR 0288): the
// days between the first leg to do's planned departure and today move every leg
// still to do, and slack is counted on the moved dates.

func TestPlanBehindDays_IsTodayPastTheFirstLegToDosDeparture(t *testing.T) {
	p := planAt(t, "2026-11-14", 2, 1, 1, 1, 0) // leaves stop 0 on the 16th, stop 1 on the 17th
	// 18 Nov 01:00 at UTC+10 is still the 17th in UTC: the day is the boat's.
	cases := []struct {
		name     string
		now      time.Time
		progress []string
		want     int
	}{
		{"before the planned day", time.Date(2026, 11, 14, 22, 0, 0, 0, time.UTC), nil, 0},
		{"on the planned day", time.Date(2026, 11, 15, 22, 0, 0, 0, time.UTC), nil, 0},
		{"two days on, in the boat's zone", time.Date(2026, 11, 17, 15, 0, 0, 0, time.UTC), nil, 2},
		{"counted from the first leg still to do", time.Date(2026, 11, 17, 15, 0, 0, 0, time.UTC), []string{planProgressDone, "", "", ""}, 1},
		{"everything done", time.Date(2026, 11, 17, 15, 0, 0, 0, time.UTC), []string{planProgressDone, planProgressDone, planProgressSkipped, planProgressDone}, 0},
	}
	for _, c := range cases {
		got, err := planBehindDays(p, c.progress, c.now)
		if err != nil || got != c.want {
			t.Errorf("%s: behind %d (%v), want %d", c.name, got, err, c.want)
		}
	}
}

func TestPlanSlack_ABehindPlanIsCountedOnTheProjectedArrival(t *testing.T) {
	p := planAt(t, "2026-11-14", 2, 1, 1, 1, 0) // arrives 14, 16, 17, 18, 19
	p.Stops[4].ArriveBy = "2026-11-20T17:00"
	s, ok, _ := planSlackOf(p, nil, 2) // two days behind: arrives the 21st
	if !ok || s.Days != -1 || s.Stop != 4 || s.BehindDays != 2 {
		t.Fatalf("slack = %+v %v, want late by 1 day at stop 4, 2 days behind", s, ok)
	}
	// A stop already reached does not move with the plan.
	p.Stops[2].ArriveBy = "2026-11-17T09:00"
	done := []string{planProgressDone, planProgressDone, "", ""}
	s, _, _ = planSlackOf(p, done, 1)
	if s.Stop != 4 || s.Days != 0 {
		t.Fatalf("slack = %+v, want stop 4 on its day (arrives 20 against the 20th)", s)
	}
}

func TestPlanOutlook_SaysHowFarBehindThePlanIsAndWhetherThatMakesItLate(t *testing.T) {
	p := planAt(t, "2026-11-14", 2, 1, 1, 1, 0)
	p.Stops[4].ArriveBy = "2026-11-20T17:00"
	now := time.Date(2026, 11, 17, 15, 0, 0, 0, time.UTC) // 18 Nov at UTC+10
	s, ok, first, err := planOutlook(p, make([]string, 4), now)
	if err != nil || !ok || first != 0 || s.Days != -1 || s.BehindDays != 2 {
		t.Fatalf("outlook = %+v %v first %d (%v)", s, ok, first, err)
	}
}
