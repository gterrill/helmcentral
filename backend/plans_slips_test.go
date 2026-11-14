package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// Slip cost (ADR 0288): with an arrive-by, every slip from one day to the slack
// plus one is assessed on the shifted plan, and the worst leg is named for each.
// The wind in these tests is 10 kn to the end of the 15th and 25 kn from the 16th,
// from dead ahead of the 190 degree legs, past the 20 kn ahead limit.

func slipWind() forecastSource {
	return paWind(func(t time.Time) float64 {
		if t.Before(paAt(16, 0, 0)) {
			return 10
		}
		return 25
	})
}

// slipsOf prices the slips of a plan whose legs are already routed.
func slipsOf(ctx context.Context, p plan, legs []planLeg, d planAssessDeps) planSlips {
	return slipsFor(ctx, p, d, func(context.Context) []planLeg { return legs })
}

// slipPlan2 is leg 0 on the 14th, a night at stop 1, leg 1 on the 15th, arriving
// at stop 2 on the 15th. Both legs leave at 8 AM, so nothing is ranked.
func slipPlan2(t *testing.T, arriveBy string) (plan, []planLeg) {
	t.Helper()
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[0].DepartTime = "08:00"
	p.Stops[1].DepartTime = "08:00"
	p.Stops[2].ArriveBy = arriveBy
	return p, legs
}

func TestSlipPlan_NamesTheWorstLegForEachSlipFromOneDayToSlackPlusOne(t *testing.T) {
	p, legs := slipPlan2(t, "2026-11-16T12:00") // reaches stop 2 on the 15th: slack 1
	f := &paFake{fc: slipWind()}
	got := slipsOf(context.Background(), p, legs, paDeps(f))
	if got.Problem != "" || got.SlackDays != 1 || len(got.Slips) != 2 {
		t.Fatalf("slips = %+v", got)
	}
	one, two := got.Slips[0], got.Slips[1]
	// Slip 1: leg 0 leaves on the 15th in 10 kn, leg 1 on the 16th in 25 kn.
	if one.Days != 1 || one.Leg != 1 || one.Date != "2026-11-16" || one.Verdict != "no_go" || one.LateByDays != 0 {
		t.Fatalf("slip 1 = %+v", one)
	}
	if one.Limiting == "" {
		t.Fatalf("slip 1 names no limiting factor: %+v", one)
	}
	// Slip 2: both legs are in the 25 kn; the first is named, and the plan is late.
	if two.Days != 2 || two.Leg != 0 || two.Date != "2026-11-16" || two.Verdict != "no_go" || two.LateByDays != 1 {
		t.Fatalf("slip 2 = %+v", two)
	}
}

// A plan that has fallen behind is priced from where it now stands: the days
// since the first leg to do should have left move every leg still to do, and each
// slip is measured on top of that, so no slipped leg falls on a day that has gone.
func TestSlipPlan_ABehindPlanIsProjectedFromTodayBeforeItIsSlipped(t *testing.T) {
	p, legs := slipPlan2(t, "2026-11-17T12:00") // planned to reach stop 2 on the 15th
	d := paDeps(&paFake{fc: slipWind()})
	d.now = func() time.Time { return paAt(15, 8, 0) } // leg 0 was to leave on the 14th: a day behind
	got := slipsOf(context.Background(), p, legs, d)
	// Projected arrival at stop 2 is the 16th, so the slack is 1 and two slips are priced.
	if got.Problem != "" || got.SlackDays != 1 || len(got.Slips) != 2 {
		t.Fatalf("slips = %+v", got)
	}
	// Slip 1 is two days on the plan: leg 0 leaves on the 16th, in the 25 kn.
	if s := got.Slips[0]; s.Days != 1 || s.Leg != 0 || s.Date != "2026-11-16" || s.Verdict != "no_go" || s.LateByDays != 0 {
		t.Fatalf("slip 1 = %+v", s)
	}
	if s := got.Slips[1]; s.Days != 2 || s.Leg != 0 || s.Date != "2026-11-17" || s.LateByDays != 1 {
		t.Fatalf("slip 2 = %+v", s)
	}
}

func TestSlipPlan_ABehindPlanThatTheProjectionMakesLateHasNoSlipsToPrice(t *testing.T) {
	p, legs := slipPlan2(t, "2026-11-15T12:00") // on time as stored, late once projected
	f := &paFake{}
	d := paDeps(f)
	d.now = func() time.Time { return paAt(15, 8, 0) }
	got := slipsOf(context.Background(), p, legs, d)
	if len(got.Slips) != 0 || got.SlackDays != -1 || f.fetches != 0 {
		t.Fatalf("slips = %+v after %d forecast reads", got, f.fetches)
	}
}

func TestSlipPlan_AGoSlipIsNamedToo(t *testing.T) {
	p, legs := slipPlan2(t, "2026-11-16T12:00")
	got := slipsOf(context.Background(), p, legs, paDeps(&paFake{})) // 10 kn all through
	if len(got.Slips) != 2 || got.Slips[0].Verdict != "go" || got.Slips[1].Verdict != "go" {
		t.Fatalf("slips = %+v", got.Slips)
	}
}

func TestSlipPlan_NoArriveByMeansNoSlipsAndNoAssessment(t *testing.T) {
	p, legs := slipPlan2(t, "")
	f := &paFake{}
	got := slipsOf(context.Background(), p, legs, paDeps(f))
	if len(got.Slips) != 0 || f.fetches != 0 {
		t.Fatalf("slips = %+v after %d forecast reads", got, f.fetches)
	}
}

func TestSlipPlan_APlanAlreadyLateHasNoSlipsToPrice(t *testing.T) {
	p, legs := slipPlan2(t, "2026-11-14T12:00") // reaches stop 2 a day after the arrive-by
	f := &paFake{}
	got := slipsOf(context.Background(), p, legs, paDeps(f))
	if len(got.Slips) != 0 || got.SlackDays != -1 || f.fetches != 0 {
		t.Fatalf("slips = %+v", got)
	}
}

func TestSlipPlan_ADoneLegIsLeftOutAndTheShiftStartsAtTheFirstLegToDo(t *testing.T) {
	p, legs := slipPlan2(t, "2026-11-16T12:00")
	f := &paFake{fc: slipWind(), progress: []string{planProgressDone, ""}}
	got := slipsOf(context.Background(), p, legs, paDeps(f))
	if len(got.Slips) != 2 {
		t.Fatalf("slips = %+v", got)
	}
	// Leg 1 leaves on the 15th; slipped one day it is in the 25 kn.
	if s := got.Slips[0]; s.Leg != 1 || s.Date != "2026-11-16" || s.Verdict != "no_go" {
		t.Fatalf("slip 1 = %+v", s)
	}
	if s := got.Slips[1]; s.Leg != 1 || s.Date != "2026-11-17" {
		t.Fatalf("slip 2 = %+v", s)
	}
}

func TestSlipPlan_EverythingDoneHasNothingToSlip(t *testing.T) {
	p, legs := slipPlan2(t, "2026-11-16T12:00")
	got := slipsOf(context.Background(), p, legs, paDeps(&paFake{progress: []string{planProgressDone, planProgressDone}}))
	if len(got.Slips) != 0 {
		t.Fatalf("slips = %+v", got.Slips)
	}
}

func TestSlipPlan_ALegBeyondTheForecastSaysNoForecastYet(t *testing.T) {
	p, legs := slipPlan2(t, "2026-11-26T12:00") // slack 11: checked up to the forecast's reach
	got := slipsOf(context.Background(), p, legs, paDeps(&paFake{}))
	if !got.Capped || len(got.Slips) != planSlipMaxDays {
		t.Fatalf("capped %v, %d slips", got.Capped, len(got.Slips))
	}
	last := got.Slips[len(got.Slips)-1]
	if last.Verdict != "" || last.NoteKind != planKindNoForecast || last.Note != planNoteNoForecast {
		t.Fatalf("slip %d = %+v", last.Days, last)
	}
}

func TestSlipPlan_DoesNotChangeThePlanItWasGiven(t *testing.T) {
	p, legs := slipPlan2(t, "2026-11-16T12:00")
	before := fmt.Sprint(arrivals(p.Stops), p.Stops[0].Nights, p.Stops[1].Nights)
	slipsOf(context.Background(), p, legs, paDeps(&paFake{}))
	if after := fmt.Sprint(arrivals(p.Stops), p.Stops[0].Nights, p.Stops[1].Nights); after != before {
		t.Fatalf("plan changed from %s to %s", before, after)
	}
}

func TestSlipPlan_ReadsNeitherTheCurrentNorTheWarnings(t *testing.T) {
	p, legs := slipPlan2(t, "2026-11-16T12:00")
	p.Stops[1].Nights = 1
	f := &paFake{}
	slipsOf(context.Background(), p, legs, paDeps(f))
	if len(f.currentCalls) != 0 || f.warningsOpen != 0 {
		t.Fatalf("%d current reads, %d warnings opens: a slip needs only verdicts", len(f.currentCalls), f.warningsOpen)
	}
}

func TestSlipPlan_NothingToPriceAsksForNoRoutes(t *testing.T) {
	for name, arriveBy := range map[string]string{"no arrive-by": "", "already late": "2026-11-14T12:00"} {
		p, _ := slipPlan2(t, arriveBy)
		routed := false
		slipsFor(context.Background(), p, paDeps(&paFake{}), func(context.Context) []planLeg { routed = true; return nil })
		if routed {
			t.Fatalf("%s: the legs were routed", name)
		}
	}
	p, _ := slipPlan2(t, "2026-11-16T12:00")
	routed := false
	got := slipsFor(context.Background(), p, paDeps(&paFake{progressErr: fmt.Errorf("disk")}), func(context.Context) []planLeg { routed = true; return nil })
	if routed || got.Problem == "" {
		t.Fatalf("progress error: routed %v, %+v", routed, got)
	}
}

func TestSlipPlan_NeverWritesTheDaysFirstSuggestion(t *testing.T) {
	// No operator times, so every leg is suggested a departure.
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[2].ArriveBy = "2026-11-16T12:00"
	store := newTestDocumentStore(t)
	slipsOf(context.Background(), p, legs, paDeps(&paFake{store: store}))
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM plan_first_suggestions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d suggestions kept by the slips (%v)", n, err)
	}
	paAssess(t, p, legs, &paFake{store: store})
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM plan_first_suggestions`).Scan(&n); err != nil || n == 0 {
		t.Fatalf("the assessment kept none (%v)", err)
	}
}

// slipsWithMemo prices a plan's slips with the production forecast memo between
// the plan and the fake provider, and a clock that moves on at every read. now
// is every instant the memo was asked about.
func slipsWithMemo(t *testing.T, step time.Duration, clock func(call int) time.Time) (got planSlips, f *paFake, memo *planForecastMemo, nows []time.Time) {
	t.Helper()
	p, legs := slipPlan2(t, "2026-11-20T12:00") // slack 5: six slips
	f = &paFake{fc: slipWind()}
	d := paDeps(f)
	memo = newPlanForecastMemo(planForecastMemoTTL)
	var mu sync.Mutex
	calls := 0
	d.now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return clock(calls)
	}
	d.sources = func(zone func() (string, error), now time.Time) (passageSources, error) {
		mu.Lock()
		nows = append(nows, now)
		mu.Unlock()
		return passageSources{fc: memo.wrap(f.fc, "Australia/Brisbane", now), tides: pwNoTides, fallbackModel: func() string { return "" }}, nil
	}
	got = slipsOf(context.Background(), p, legs, d)
	return got, f, memo, nows
}

func TestSlipPlan_ReusesTheWarmedForecastSoEachPointIsReadOnce(t *testing.T) {
	// The clock moves a second at every read, as a real one does.
	got, f, memo, _ := slipsWithMemo(t, time.Second, func(call int) time.Time {
		return paNow.Add(time.Duration(call) * time.Second)
	})
	if got.Problem != "" || len(got.Slips) != 6 {
		t.Fatalf("slips = %+v", got)
	}
	memo.mu.Lock()
	unique := len(memo.m)
	memo.mu.Unlock()
	if unique == 0 || f.fetches != unique {
		t.Fatalf("%d provider reads for %d distinct points: the slips must reuse the warmed forecast", f.fetches, unique)
	}
}

func TestSlipPlan_OneClockReadingForTheWholeRequestEvenAcrossMidnight(t *testing.T) {
	before := time.Date(2026, 11, 14, 23, 59, 59, 0, paLoc)
	_, _, _, nows := slipsWithMemo(t, time.Second, func(call int) time.Time {
		if call == 1 {
			return before
		}
		return before.Add(2 * time.Second) // 15 Nov, just past midnight
	})
	if len(nows) == 0 {
		t.Fatal("the forecast was never read")
	}
	for _, n := range nows {
		if !n.Equal(before) {
			t.Fatalf("the request used %v as now and %v as now: it must be one instant", before, n)
		}
	}
}

func TestSlipPlan_TheRecordedPassagesAreReadOnce(t *testing.T) {
	p, legs := slipPlan2(t, "2026-11-16T12:00")
	reads := 0
	d := paDeps(&paFake{})
	inner := d.progress
	d.progress = func(ctx context.Context, p plan) ([]string, error) { reads++; return inner(ctx, p) }
	slipsOf(context.Background(), p, legs, d)
	if reads != 1 {
		t.Fatalf("%d progress reads for 2 slips", reads)
	}
}

func TestSlipPlan_ProgressThatCannotBeReadIsAProblemNotASlipList(t *testing.T) {
	p, legs := slipPlan2(t, "2026-11-16T12:00")
	got := slipsOf(context.Background(), p, legs, paDeps(&paFake{progressErr: fmt.Errorf("disk")}))
	if got.Problem == "" || len(got.Slips) != 0 {
		t.Fatalf("slips = %+v", got)
	}
}

func TestSlipPlan_LimitsNotSetIsTheProblemOnceAndNoSlips(t *testing.T) {
	p, legs := slipPlan2(t, "2026-11-16T12:00")
	d := paDeps(&paFake{})
	d.limits = func() (passageLimits, error) { return passageLimits{}, nil }
	got := slipsOf(context.Background(), p, legs, d)
	if got.Problem == "" || len(got.Slips) != 0 {
		t.Fatalf("slips = %+v", got)
	}
}

func TestSlipPlan_LimitsAndSpeedAreReadOnceForAllTheSlips(t *testing.T) {
	p, legs := slipPlan2(t, "2026-11-20T12:00") // slack 5: six slips
	d := paDeps(&paFake{})
	var mu sync.Mutex
	limits, speeds := 0, 0
	innerLimits, innerSpeed := d.limits, d.speed
	d.limits = func() (passageLimits, error) { mu.Lock(); limits++; mu.Unlock(); return innerLimits() }
	d.speed = func(ctx context.Context) (float64, error) { mu.Lock(); speeds++; mu.Unlock(); return innerSpeed(ctx) }
	got := slipsOf(context.Background(), p, legs, d)
	if len(got.Slips) != 6 {
		t.Fatalf("slips = %d", len(got.Slips))
	}
	if limits != 1 || speeds != 1 {
		t.Fatalf("%d limits reads and %d speed reads for 6 slips, want 1 each", limits, speeds)
	}
}

func TestSlipPlan_OneSlipThatCannotBeCheckedKeepsTheOthers(t *testing.T) {
	p, legs := slipPlan2(t, "2026-11-20T12:00") // six slips
	d := paDeps(&paFake{})
	var mu sync.Mutex
	calls := 0
	inner := d.sources
	d.sources = func(zone func() (string, error), now time.Time) (passageSources, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 3 { // the first call warms the forecast; this is one slip's
			return passageSources{}, fmt.Errorf("provider down")
		}
		return inner(zone, now)
	}
	got := slipsOf(context.Background(), p, legs, d)
	if got.Problem != "" || len(got.Slips) != 6 {
		t.Fatalf("slips = %+v", got)
	}
	failed := 0
	for i, s := range got.Slips {
		if s.Days != i+1 {
			t.Fatalf("slip %d is for %d days", i+1, s.Days)
		}
		if strings.Contains(s.Note, "could not be checked") {
			failed++
			if !strings.Contains(s.Note, "provider down") || s.Verdict != "" {
				t.Fatalf("failed slip = %+v", s)
			}
		} else if s.Verdict == "" {
			t.Fatalf("slip %d lost its verdict: %+v", s.Days, s)
		}
	}
	if failed != 1 {
		t.Fatalf("%d slips could not be checked, want 1", failed)
	}
}

func TestSlipPlan_SlipsAreAssessedAFewAtATime(t *testing.T) {
	// No operator times, so every leg of every slip is ranked.
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[2].ArriveBy = "2026-11-20T12:00"
	var mu sync.Mutex
	running, most := 0, 0
	f := &paFake{departures: func(q planDepartureQuery) ([]tidalDeparture, error) {
		mu.Lock()
		running++
		most = max(most, running)
		mu.Unlock()
		time.Sleep(15 * time.Millisecond)
		mu.Lock()
		running--
		mu.Unlock()
		return []tidalDeparture{{Depart: q.Earliest, Current: planCurrentReading{Have: true}}}, nil
	}}
	got := slipsOf(context.Background(), p, legs, paDeps(f))
	if len(got.Slips) != 6 {
		t.Fatalf("slips = %d", len(got.Slips))
	}
	if limit := planSlipWorkers * planLegWorkers; most > limit {
		t.Fatalf("%d rankings at once, the bound is %d", most, limit)
	}
}

func TestWorstSlipLeg_NoGoThenNoDataThenMarginalThenGo(t *testing.T) {
	leg := func(v string) planLegAssessment { return planLegAssessment{Verdict: v} }
	cases := []struct {
		name string
		legs []planLegAssessment
		want int
	}{
		{"no-go over marginal", []planLegAssessment{leg("marginal"), leg("no_go"), leg("no_data")}, 1},
		{"no data over marginal", []planLegAssessment{leg("marginal"), leg("no_data"), leg("go")}, 1},
		{"marginal over go", []planLegAssessment{leg("go"), leg("marginal")}, 1},
		{"the first of equals", []planLegAssessment{leg("go"), leg("go")}, 0},
		{"no forecast ranks with no data", []planLegAssessment{leg("marginal"), {NoteKind: planKindNoForecast, Note: planNoteNoForecast}}, 1},
		{"a no-go beats no forecast", []planLegAssessment{{NoteKind: planKindNoForecast}, leg("no_go")}, 1},
		{"a leg with only a note is named when nothing else is", []planLegAssessment{{NoteKind: planKindOvernight, Note: planNoteOvernight}}, 0},
		{"an unsailable leg is named over a go", []planLegAssessment{leg("go"), {NoteKind: planKindNoWindow, Note: planNoteNoWindow}}, 1},
		{"an unsailable leg is named over a marginal", []planLegAssessment{leg("marginal"), {NoteKind: planKindGap, Note: planNoteGap}}, 1},
		{"a no-go still beats an unsailable leg", []planLegAssessment{{NoteKind: planKindNoWindow, Note: planNoteNoWindow}, leg("no_go")}, 1},
		{"a done leg's empty note is not a leg to name", []planLegAssessment{{Progress: planProgressDone}, leg("go")}, 1},
	}
	for _, c := range cases {
		for i := range c.legs {
			c.legs[i].Leg = i
		}
		if got := worstSlipLeg(c.legs); got.Leg != c.want {
			t.Errorf("%s: leg %d, want %d", c.name, got.Leg, c.want)
		}
	}
}

func TestPlanSlipsHandler_ServesSlipsAndTheRouteIsReadTier(t *testing.T) {
	store := withTestDocumentStore(t)
	prevSeg, prevDeps := planRouteSegment, planAssessDepsFor
	t.Cleanup(func() { planRouteSegment, planAssessDepsFor = prevSeg, prevDeps })
	planRouteSegment = func(_ context.Context, aLat, aLon, bLat, bLon float64) (waterRoute, error) {
		return routeOf(30, [2]float64{aLat, aLon}, [2]float64{bLat, bLon}), nil
	}
	planAssessDepsFor = func(assistantToolDeps) planAssessDeps { return paDeps(&paFake{}) }
	in := plan{Name: "Run", Stops: stopsOf("2026-11-14", 0, 1, 0)}
	in.Stops[2].ArriveBy = "2026-11-16T12:00"
	saved, err := store.CreatePlan(in)
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprint(saved.ID)
	c, rec := planCtx(http.MethodGet, "/api/plans/"+id+"/slips", "", id, "")
	if err := planSlipsHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("slips: %v %d %s", err, rec.Code, rec.Body.String())
	}
	var out planSlips
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Slips) != 2 {
		t.Fatalf("slips = %s (%v)", rec.Body.String(), err)
	}
	c, rec = planCtx(http.MethodGet, "/api/plans/999/slips", "", "999", "")
	if err := planSlipsHandler(c); err != nil || rec.Code != http.StatusNotFound {
		t.Fatalf("missing plan: %v %d", err, rec.Code)
	}
	for _, r := range plansRoutes() {
		if r.Path == "/api/plans/:id/slips" && (r.Method != http.MethodGet || r.Tier != tierRead) {
			t.Fatalf("route = %+v", r)
		}
	}
}
