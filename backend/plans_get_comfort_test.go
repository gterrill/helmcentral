package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// get_plans runs the comfort ranking and the chips as the Forecast page does, for
// every caller, so Mate quotes the departure and the comfort the page shows.

// comfortToolEnv is plansToolEnv with the comfort readers of a ranking rig wired,
// and a one-leg plan saved that the rig's forecast and candidates fit.
func comfortToolEnv(t *testing.T, cfg rkCfg) (assistantToolDeps, *rkRun, plan) {
	t.Helper()
	deps, store, _ := plansToolEnv(t)
	r := rkSetup(t, cfg)
	planAssessDepsFor = func(td assistantToolDeps) planAssessDeps {
		d := r.d
		d.now = td.now
		return d
	}
	saved, err := store.CreatePlan(plan{Name: "Run", Stops: stopsOf("2026-11-14", 0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	dp, err := decoratePlan(saved, paNow)
	if err != nil {
		t.Fatal(err)
	}
	return deps, r, dp
}

// The page handler and get_plans both build their options here, so they differ only in Env.
func TestPagePlanAssessOptions_AreTheSameForPageAndGetPlansApartFromEnv(t *testing.T) {
	env := planEnv{}
	page, report := pagePlanAssessOptions(nil), pagePlanAssessOptions(&env)
	if !page.Comfort || !report.Comfort || page.VerdictsOnly || report.VerdictsOnly {
		t.Fatalf("page %+v, report %+v", page, report)
	}
	if page.Env != nil || report.Env != &env {
		t.Errorf("env not passed through: %+v, %+v", page.Env, report.Env)
	}
	report.Env = nil
	if !reflect.DeepEqual(page, report) {
		t.Errorf("options differ beyond Env: %+v vs %+v", page, report)
	}
}

// 1
func TestGetPlans_SuggestsTheDepartureAndBasisThePageDoes(t *testing.T) {
	deps, r, dp := comfortToolEnv(t, rkCfg{at: rkLively(8), cands: []time.Time{rkAt(8), rkAt(9)}})
	ctx := context.Background()
	page := assessPlan(ctx, dp, planLegs(ctx, dp), r.d.pinned(), planAssessOptions{Comfort: true}).Legs[0]
	if page.DepartBasis == nil || !strings.HasPrefix(*page.DepartBasis, "Chosen for comfort") {
		t.Fatalf("the page's own leg is not ranked for comfort: %+v", page)
	}
	wantDepart, err := planInstantLabel(page.Depart)
	if err != nil {
		t.Fatal(err)
	}
	got := callGetPlans(t, deps, `{}`).Plan.Legs[0]
	if got.Depart != wantDepart || !got.DepartSuggested {
		t.Errorf("depart = %q (suggested %v), the page's is %q", got.Depart, got.DepartSuggested, wantDepart)
	}
	if got.DepartBasis != *page.DepartBasis {
		t.Errorf("depart_basis = %q, the page's is %q", got.DepartBasis, *page.DepartBasis)
	}
}

// 2
func TestGetPlans_AComfortLegCarriesItsStretchesWithBoatClockLabelsAndPlaces(t *testing.T) {
	// 8 AM departure, five hours: Lively in the first two, then Comfortable to arrival.
	deps, _, _ := comfortToolEnv(t, rkCfg{at: rkLively(8, 9), cands: []time.Time{rkAt(8)}})
	l := callGetPlans(t, deps, `{}`).Plan.Legs[0]
	if l.Comfort == nil {
		t.Fatalf("no comfort on the leg: %+v", l)
	}
	if len(l.Comfort.Hours) != 0 {
		t.Errorf("the hour lines are the page's only: %d sent", len(l.Comfort.Hours))
	}
	s := l.Comfort.Stretches
	if len(s) != 2 {
		t.Fatalf("stretches = %+v", s)
	}
	if s[0].Band != "Lively" || s[0].UntilLabel != "10.00 AM" || s[1].Band != "Comfortable" || s[1].FromLabel != "10.00 AM" {
		t.Errorf("stretches = %+v", s)
	}
	if s[0].FromLabel != "Sat 14 Nov 8.00 AM" || s[1].UntilLabel != "1.00 PM" {
		t.Errorf("the first starts at the departure and the last ends at 1.00 PM: %+v", s)
	}
	if l.Depart != s[0].FromLabel || !strings.HasSuffix(l.Arrive, s[1].UntilLabel) {
		t.Errorf("the last stretch ends at %q, the leg arrives %q", s[1].UntilLabel, l.Arrive)
	}
	if want := l.From + " → " + l.To; s[0].Where != want || s[1].Where != want {
		t.Errorf("where = %q and %q, want the segment %q", s[0].Where, s[1].Where, want)
	}
	if s[0].Reason != "roll, swell 1.0 m 6 s on the port bow" || s[1].Reason != "" {
		t.Errorf("reasons = %q and %q", s[0].Reason, s[1].Reason)
	}
}

// A suggested departure's chip is made from the ranking's hours, and its last until_label
// reads on the arrival stop's clock as the leg's Arrive does, with the zone added, across a
// zone change.
func TestGetPlans_ASuggestedLegsLastUntilLabelReadsAsItsArriveDoesAcrossAZoneChange(t *testing.T) {
	prev := stayZoneAt
	t.Cleanup(func() { stayZoneAt = prev })
	bris, syd := wireZone(t, "Australia/Brisbane"), wireZone(t, "Australia/Sydney")
	stayZoneAt = func(lat, lon float64) (placeZone, error) {
		if lat > -26 {
			return placeZone{Loc: bris}, nil
		}
		return placeZone{Loc: syd}, nil
	}
	deps, _, _ := comfortToolEnv(t, rkCfg{at: rkLively(8), cands: []time.Time{rkAt(8), rkAt(9)}})
	l := callGetPlans(t, deps, `{}`).Plan.Legs[0]
	if !l.DepartSuggested || l.Comfort == nil || len(l.Comfort.Stretches) == 0 {
		t.Fatalf("no suggested leg with stretches: %+v", l)
	}
	s := l.Comfort.Stretches
	last := s[len(s)-1].UntilLabel
	// The leg's Arrive names no zone; across a zone change the last until adds it.
	if last != l.Arrive+" AEDT" {
		t.Errorf("the last until_label is %q, the leg arrives %q", last, l.Arrive)
	}
}

// 6
func TestGetPlans_ALegWithNoRpmHasNoEtaAndNoComfortAndSaysWhy(t *testing.T) {
	deps, store, f := plansToolEnv(t)
	f.speedErr = errors.New("no engine rpm recorded under way")
	var calls atomic.Int32
	d := lcDeps(t, f, &calls)
	planAssessDepsFor = func(td assistantToolDeps) planAssessDeps {
		d.now = td.now
		return d
	}
	if _, err := store.CreatePlan(plan{Name: "Run", Stops: stopsOf("2026-11-14", 0, 0)}); err != nil {
		t.Fatal(err)
	}
	out := callGetPlans(t, deps, `{}`)
	l := out.Plan.Legs[0]
	if l.Arrive != "" || l.Hours != 0 || l.EtaBasis != nil {
		t.Errorf("a leg with no rpm has an ETA: %+v", l)
	}
	if l.Comfort != nil {
		t.Errorf("a leg with no ETA has comfort: %+v", l.Comfort)
	}
	if !strings.Contains(out.Plan.Problem, "No ETA: no engine rpm recorded under way") {
		t.Errorf("the reason is not carried: %q", out.Plan.Problem)
	}
}

// 7
func TestGetPlans_ShrinkNeverDropsALegsComfort(t *testing.T) {
	long := strings.Repeat("a long warning text ", 40)
	rep := planReport{
		ID: 1, Name: "Plan 1", Version: "v", Status: "upcoming",
		Stops: []planReportStop{{Name: "A"}, {Name: "B"}},
		Legs: []planReportLeg{{From: "A", To: "B", Verdict: "go", Comfort: &planReportComfort{
			Text: "Lively for the first 2 h.", Motion: "roll 2 deg",
			Stretches: []planComfortStretch{{Band: "Lively", FromLabel: "8.00 AM", UntilLabel: "10.00 AM", Where: "A → B", Reason: "swell 1.0 m 6 s on the beam"}},
		}}},
		Slips: &planReportSlips{SlackDays: 20},
	}
	for i := 0; i < 20; i++ {
		rep.Slips.Slips = append(rep.Slips.Slips, planReportSlip{Days: i + 1, Leg: "A to B", Verdict: "go", Note: long})
		rep.Warnings = append(rep.Warnings, planReportWarning{Stop: "B", Date: "2026-11-14", Warning: long})
		rep.NightWinds = append(rep.NightWinds, planReportNightWinds{Stop: "B", Date: "2026-11-14"})
	}
	res := getPlansResult{Plans: []planBrief{}, Plan: &rep}
	if b, _ := json.Marshal(&res); len(b) <= assistantMaxToolResultChars {
		t.Fatalf("the fixture is only %d bytes", len(b))
	}
	text, err := capToolResultJSON(&res, res.shrink)
	if err != nil {
		t.Fatal(err)
	}
	// The stretches stay while the result fits without dropping them.
	if !strings.Contains(text, `"stretches"`) || !strings.Contains(text, "Lively for the first 2 h.") {
		t.Fatalf("the comfort did not survive the cap: %.300s", text)
	}
	// Nothing is left for shrink to drop, and the comfort text is still there.
	for res.shrink() {
	}
	if c := res.Plan.Legs[0].Comfort; c == nil || c.Text != "Lively for the first 2 h." || c.Motion != "roll 2 deg" {
		t.Fatalf("shrink dropped the comfort: %+v", c)
	}
}

// A long plan whose stretches alone outgrow the cap keeps every leg's comfort text and
// motion, drops the stretches last, and says so, rather than falling back to the stub.
func TestGetPlans_ShrinkDropsTheStretchesLastAndKeepsTheComfortText(t *testing.T) {
	long := strings.Repeat("swell 1.0 m 6 s on the port bow ", 25)
	rep := planReport{ID: 1, Name: "Plan 1", Version: "v", Status: "upcoming", Stops: []planReportStop{{Name: "A"}, {Name: "B"}}}
	for i := 0; i < 6; i++ {
		c := &planReportComfort{Text: "Lively for the first 2 h: comfort text.", Motion: "roll 2 deg"}
		for j := 0; j < 3; j++ {
			c.Stretches = append(c.Stretches, planComfortStretch{Band: "Lively", FromLabel: "8.00 AM", UntilLabel: "10.00 AM", Where: "A → B", Reason: long})
		}
		rep.Legs = append(rep.Legs, planReportLeg{From: "A", To: "B", Verdict: "go", Comfort: c})
	}
	res := getPlansResult{Plans: []planBrief{}, Plan: &rep, Note: "base."}
	if b, _ := json.Marshal(&res); len(b) <= assistantMaxToolResultChars {
		t.Fatalf("the fixture is only %d bytes", len(b))
	}
	text, err := capToolResultJSON(&res, res.shrink)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, `"truncated":true`) {
		t.Fatalf("the result fell back to the stub: %.300s", text)
	}
	var back getPlansResult
	if err := json.Unmarshal([]byte(text), &back); err != nil {
		t.Fatal(err)
	}
	for i, l := range back.Plan.Legs {
		if l.Comfort == nil || l.Comfort.Text != "Lively for the first 2 h: comfort text." || l.Comfort.Motion != "roll 2 deg" {
			t.Errorf("leg %d lost its comfort: %+v", i, l.Comfort)
		}
		if l.Comfort != nil && len(l.Comfort.Stretches) != 0 {
			t.Errorf("leg %d kept %d stretches", i, len(l.Comfort.Stretches))
		}
	}
	if !strings.Contains(back.Note, "the comfort stretches of each leg") {
		t.Errorf("the note does not say the stretches went: %q", back.Note)
	}
}
