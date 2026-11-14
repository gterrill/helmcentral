package main

import "testing"

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
	s, ok := planSlackOf(p, nil)
	if !ok || s.Days != 1 || s.Stop != 4 {
		t.Fatalf("slack = %+v %v, want 1 day at stop 4", s, ok)
	}
	// Late: arrive-by is the day before the planned arrival.
	p.Stops[4].ArriveBy = "2026-11-18T23:00"
	s, _ = planSlackOf(p, nil)
	if s.Days != -1 {
		t.Fatalf("late slack = %+v, want -1", s)
	}
	// With none anywhere there is no slack to speak of.
	p.Stops[4].ArriveBy = ""
	if _, ok := planSlackOf(p, nil); ok {
		t.Fatal("no arrive-by must give no slack")
	}
}

func TestPlanSlack_TheTightestStopRulesAndAReachedStopIsLeftOut(t *testing.T) {
	p := planAt(t, "2026-11-14", 2, 1, 1, 1, 0)
	p.Stops[2].ArriveBy = "2026-11-18T12:00" // arrives 17: slack 1
	p.Stops[4].ArriveBy = "2026-11-25T12:00" // arrives 19: slack 6
	s, _ := planSlackOf(p, nil)
	if s.Days != 1 || s.Stop != 2 {
		t.Fatalf("slack = %+v, want the tighter stop 2", s)
	}
	// Legs 0 and 1 done: the boat is at stop 2, whose arrive-by no longer binds.
	done := []string{planProgressDone, planProgressDone, "", ""}
	s, _ = planSlackOf(p, done)
	if s.Days != 6 || s.Stop != 4 {
		t.Fatalf("slack with stop 2 reached = %+v, want stop 4's 6 days", s)
	}
}
