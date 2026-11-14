package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// get_plans (ADR 0289) replaces get_planned_stay: every plan in brief, and the
// current or named one in full with the verdicts the Forecast page's table shows.

// plansToolEnv is the Forecast page's world at paNow (Saturday 14 November 2026,
// 4.30 AM at Brisbane) with every leg routed 30 nm, and the tool's deps.
func plansToolEnv(t *testing.T) (assistantToolDeps, *documentStore, *paFake) {
	t.Helper()
	store := withTestDocumentStore(t)
	prevSeg, prevDeps := planRouteSegment, planAssessDepsFor
	t.Cleanup(func() { planRouteSegment, planAssessDepsFor = prevSeg, prevDeps })
	planRouteSegment = func(_ context.Context, aLat, aLon, bLat, bLon float64) (waterRoute, error) {
		return routeOf(30, [2]float64{aLat, aLon}, [2]float64{bLat, bLon}), nil
	}
	f := &paFake{}
	// The hook builds the readers from the tool deps it is given, as production
	// does (planAssessDepsFromTool): a fake for the providers, the deps' clock.
	planAssessDepsFor = func(td assistantToolDeps) planAssessDeps {
		d := paDeps(f)
		d.now = td.now
		return d
	}
	deps := assistantToolDeps{
		documents: func() *documentStore { return store },
		now:       func() time.Time { return paNow },
	}
	return deps, store, f
}

type getPlansWire struct {
	Plans []struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		FirstDate string `json:"first_date"`
		LastDate  string `json:"last_date"`
		Status    string `json:"status"`
	} `json:"plans"`
	Note string `json:"note"`
	Plan *struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		IsCurrent bool   `json:"is_current"`
		Version   string `json:"version"`
		Stops     []struct {
			Name       string  `json:"name"`
			Lat        float64 `json:"lat"`
			Lon        float64 `json:"lon"`
			Arrive     string  `json:"arrive"`
			Nights     int     `json:"nights"`
			Depart     string  `json:"depart"`
			DepartTime string  `json:"depart_time"`
		} `json:"stops"`
		Legs []struct {
			From       string  `json:"from"`
			To         string  `json:"to"`
			DistanceNm float64 `json:"distance_nm"`
			Verdict    string  `json:"verdict"`
			Depart     string  `json:"depart"`
			Progress   string  `json:"progress"`
		} `json:"legs"`
	} `json:"plan"`
}

func callGetPlans(t *testing.T, deps assistantToolDeps, args string) getPlansWire {
	t.Helper()
	raw, err := runRecordTool(t, deps, "get_plans", args)
	if err != nil {
		t.Fatalf("get_plans: %v", err)
	}
	var out getPlansWire
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return out
}

func TestGetPlans_ListsEveryPlanAndGivesTheCurrentOneWithItsVerdicts(t *testing.T) {
	deps, store, _ := plansToolEnv(t)
	run := plan{Name: "Home run", Stops: stopsOf("2026-11-14", 0, 1, 0)}
	run.Stops[0].DepartTime = "08:00"
	if _, err := store.CreatePlan(run); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePlan(plan{Name: "Later", Stops: stopsOf("2026-12-20", 1, 0)}); err != nil {
		t.Fatal(err)
	}

	out := callGetPlans(t, deps, `{}`)
	if len(out.Plans) != 2 || out.Plans[0].Name != "Home run" || out.Plans[1].Status != "upcoming" || out.Plans[0].FirstDate != "2026-11-14" {
		t.Fatalf("the brief list = %+v", out.Plans)
	}
	p := out.Plan
	if p == nil || p.Name != "Home run" || !p.IsCurrent || p.Version == "" {
		t.Fatalf("the current plan = %+v", p)
	}
	if len(p.Stops) != 3 || p.Stops[0].Lat == 0 || p.Stops[0].Depart != "2026-11-14" || p.Stops[1].Arrive != "2026-11-14" || p.Stops[0].DepartTime != "08:00" {
		t.Fatalf("stops = %+v", p.Stops)
	}
	if len(p.Legs) != 2 || p.Legs[0].Verdict != "go" || p.Legs[0].DistanceNm != 30 || p.Legs[0].From != p.Stops[0].Name || p.Legs[0].To != p.Stops[1].Name {
		t.Fatalf("legs = %+v", p.Legs)
	}
	// A time is a finished boat-time label, never an ISO instant.
	if !strings.HasPrefix(p.Legs[0].Depart, "Sat 14 Nov 8.00 AM") {
		t.Fatalf("depart = %q", p.Legs[0].Depart)
	}
}

func TestGetPlans_ANamedOrNumberedPlanIsGivenInFull(t *testing.T) {
	deps, store, _ := plansToolEnv(t)
	if _, err := store.CreatePlan(plan{Name: "Home run", Stops: stopsOf("2026-11-14", 0, 1, 0)}); err != nil {
		t.Fatal(err)
	}
	later, err := store.CreatePlan(plan{Name: "Later", Stops: stopsOf("2026-12-20", 1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if out := callGetPlans(t, deps, `{"name":"later"}`); out.Plan == nil || out.Plan.ID != later.ID || out.Plan.IsCurrent {
		t.Fatalf("by name = %+v", out.Plan)
	}
	if out := callGetPlans(t, deps, fmt.Sprintf(`{"id":%d}`, later.ID)); out.Plan == nil || out.Plan.Name != "Later" {
		t.Fatalf("by id = %+v", out.Plan)
	}
	_, err = runRecordTool(t, deps, "get_plans", `{"name":"Atlantic"}`)
	if err == nil || !strings.Contains(err.Error(), "Home run") || !strings.Contains(err.Error(), "Later") {
		t.Fatalf("an unknown plan should name the plans there are, got %v", err)
	}
}

func TestGetPlans_NoPlansSaidPlainly(t *testing.T) {
	deps, _, _ := plansToolEnv(t)
	out := callGetPlans(t, deps, `{}`)
	if len(out.Plans) != 0 || out.Plan != nil || !strings.Contains(out.Note, "No plan") {
		t.Fatalf("got %+v", out)
	}
}

func TestGetPlans_ALegOfTheTableAndTheToolAgree(t *testing.T) {
	// One verdict: the tool is the endpoint's assessment, not a second judgement.
	deps, store, f := plansToolEnv(t)
	f.fc = paWind(func(time.Time) float64 { return 40 })
	if _, err := store.CreatePlan(plan{Name: "Home run", Stops: stopsOf("2026-11-14", 0, 1, 0)}); err != nil {
		t.Fatal(err)
	}
	out := callGetPlans(t, deps, `{}`)
	if out.Plan == nil || len(out.Plan.Legs) == 0 || out.Plan.Legs[0].Verdict != "no_go" {
		t.Fatalf("a 40 kn headwind is a No-Go on the page and so here: %+v", out.Plan)
	}
}

// arriveByPlan is leg 0 on the 14th, a night at the second stop, leg 1 on the
// 15th, with an arrive-by at the last stop on the 16th, so slack and slips exist.
func arriveByPlan() plan {
	p := plan{Name: "Home run", Stops: stopsOf("2026-11-14", 0, 1, 0)}
	p.Stops[0].DepartTime = "06:30"
	p.Stops[1].DepartTime = "08:00"
	p.Stops[2].ArriveBy = "2026-11-16T17:00"
	return p
}

func TestGetPlans_EveryDateAndTimeCarriesAFinishedBoatTimeLabel(t *testing.T) {
	deps, store, f := plansToolEnv(t)
	f.fc = slipWind()
	night := time.Date(2026, 11, 14, 0, 0, 0, 0, paLoc).Format("Monday 2 January")
	f.warnings = func(lat, lon float64) (forecastWarningsBundle, error) { return paWarning(night, "Gale Warning"), nil }
	if _, err := store.CreatePlan(arriveByPlan()); err != nil {
		t.Fatal(err)
	}
	raw, err := runRecordTool(t, deps, "get_plans", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Plans []map[string]any `json:"plans"`
		Plan  struct {
			FirstDateLabel string           `json:"first_date_label"`
			LastDateLabel  string           `json:"last_date_label"`
			Stops          []map[string]any `json:"stops"`
			Warnings       []map[string]any `json:"warnings"`
			Slips          struct {
				Slips []map[string]any `json:"slips"`
			} `json:"slips"`
		} `json:"plan"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	if out.Plans[0]["first_date_label"] != "Sat 14 Nov" || out.Plans[0]["last_date_label"] != "Sun 15 Nov" {
		t.Fatalf("the brief list = %+v", out.Plans[0])
	}
	if out.Plan.FirstDateLabel != "Sat 14 Nov" || out.Plan.LastDateLabel != "Sun 15 Nov" {
		t.Fatalf("the plan's dates = %q to %q", out.Plan.FirstDateLabel, out.Plan.LastDateLabel)
	}
	if out.Plan.Stops[0]["depart_time_label"] != "6.30 AM" || out.Plan.Stops[2]["arrive_by_label"] != "Mon 16 Nov 5.00 PM" {
		t.Fatalf("stops = %+v", out.Plan.Stops)
	}
	if len(out.Plan.Warnings) == 0 || out.Plan.Warnings[0]["date_label"] != "Sat 14 Nov" {
		t.Fatalf("warnings = %+v", out.Plan.Warnings)
	}
	dated := 0
	for _, s := range out.Plan.Slips.Slips {
		if s["date"] != nil && s["date"] != "" {
			dated++
			if s["date_label"] == nil || s["date_label"] == "" {
				t.Fatalf("a slip with a date has no label: %+v", s)
			}
		}
	}
	if dated == 0 {
		t.Fatalf("the test plan should have dated slips: %s", raw)
	}
}

func TestGetPlans_TheClockIsTheToolDepsClock(t *testing.T) {
	deps, store, _ := plansToolEnv(t)
	if _, err := store.CreatePlan(plan{Name: "Home run", Stops: stopsOf("2026-11-14", 0, 1, 0)}); err != nil {
		t.Fatal(err)
	}
	// A replayed run is pinned three weeks on: the plan has finished.
	deps.now = func() time.Time { return paNow.AddDate(0, 0, 21) }
	out := callGetPlans(t, deps, `{}`)
	if len(out.Plans) != 1 || out.Plans[0].Status != "past" {
		t.Fatalf("status = %+v, want past on the pinned clock", out.Plans)
	}
}

func TestPlanAssessDepsFromTool_RunsOnTheToolDepsClock(t *testing.T) {
	pinned := paNow.AddDate(0, 0, 3)
	d := planAssessDepsFromTool(assistantToolDeps{now: func() time.Time { return pinned }}, "")
	if got := d.now(); !got.Equal(pinned) {
		t.Fatalf("the assessment's clock is %v, the tool deps' is %v", got, pinned)
	}
}

func TestGetPlans_HandsItsToolDepsToTheAssessmentWiring(t *testing.T) {
	deps, store, _ := plansToolEnv(t)
	if _, err := store.CreatePlan(plan{Name: "Home run", Stops: stopsOf("2026-11-14", 0, 1, 0)}); err != nil {
		t.Fatal(err)
	}
	pinned := paNow.AddDate(0, 0, 2)
	deps.now = func() time.Time { return pinned }
	var seen time.Time
	prev := planAssessDepsFor
	planAssessDepsFor = func(td assistantToolDeps) planAssessDeps {
		seen = td.now()
		return prev(td)
	}
	callGetPlans(t, deps, `{}`)
	if !seen.Equal(pinned) {
		t.Fatalf("the wiring was given a clock of %v, want %v", seen, pinned)
	}
}

func TestGetPlans_TheLimitsAndSpeedAreReadOnce(t *testing.T) {
	deps, store, f := plansToolEnv(t)
	if _, err := store.CreatePlan(arriveByPlan()); err != nil {
		t.Fatal(err)
	}
	limitReads := 0
	prev := planAssessDepsFor
	planAssessDepsFor = func(td assistantToolDeps) planAssessDeps {
		d := prev(td)
		inner := d.limits
		d.limits = func() (passageLimits, error) { limitReads++; return inner() }
		return d
	}
	if out := callGetPlans(t, deps, `{}`); out.Plan == nil {
		t.Fatal("no plan")
	}
	if f.speedReads != 1 || limitReads != 1 {
		t.Fatalf("speed read %d times and limits %d times, want once each", f.speedReads, limitReads)
	}
}

func TestGetPlans_ABigReportKeepsTheStopsVersionAndVerdictsAndSaysWhatWasLeftOut(t *testing.T) {
	long := strings.Repeat("a long warning text ", 40)
	res := getPlansResult{Plans: []planBrief{}}
	for i := 0; i < 60; i++ {
		res.Plans = append(res.Plans, planBrief{ID: int64(i + 1), Name: fmt.Sprintf("Plan %d", i+1), FirstDate: "2026-11-14", LastDate: "2026-11-15", Status: "upcoming"})
	}
	rep := planReport{
		ID: 1, Name: "Plan 1", Version: "2026-11-14T00:00:00Z#3", Status: "upcoming",
		Stops: []planReportStop{{Name: "A"}, {Name: "B"}},
		Legs:  []planReportLeg{{From: "A", To: "B", Verdict: "go", Limiting: "wind 12 kn"}},
		Slips: &planReportSlips{SlackDays: 20},
	}
	for i := 0; i < 20; i++ {
		rep.Slips.Slips = append(rep.Slips.Slips, planReportSlip{Days: i + 1, Leg: "A to B", Verdict: "go", Note: long})
		rep.Warnings = append(rep.Warnings, planReportWarning{Stop: "B", Date: "2026-11-14", Warning: long})
	}
	res.Plan = &rep
	if b, _ := json.Marshal(&res); len(b) <= assistantMaxToolResultChars {
		t.Fatalf("the fixture is only %d bytes", len(b))
	}
	text, err := capToolResultJSON(&res, res.shrink)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, `"truncated":true`) {
		t.Fatalf("the plan was lost to the stub: %.200s", text)
	}
	if !plansDeliveredVerdicts(text) {
		t.Fatalf("the verdicts did not survive: %.300s", text)
	}
	var got getPlansResult
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatal(err)
	}
	if got.Plan == nil || got.Plan.Version == "" || len(got.Plan.Stops) != 2 || got.Plan.Legs[0].Limiting != "wind 12 kn" {
		t.Fatalf("plan = %+v", got.Plan)
	}
	if !strings.Contains(got.Note, "left out") {
		t.Fatalf("the note should say what was left out: %q", got.Note)
	}
}

func TestGetPlans_CarriesTheWindChangesTheTableShows(t *testing.T) {
	deps, store, f := plansToolEnv(t)
	// Leaves Sat 8 AM, 30 nm at 6 kn: the wind goes from the north at 12 kn to the
	// south-east at 18 kn at 10 AM, mid-leg.
	f.fc = pwcStep(paAt(14, 10, 0), 0, 12, 135, 18)
	run := plan{Name: "Home run", Stops: stopsOf("2026-11-14", 0, 1, 0)}
	run.Stops[0].DepartTime = "08:00"
	if _, err := store.CreatePlan(run); err != nil {
		t.Fatal(err)
	}
	raw, err := runRecordTool(t, deps, "get_plans", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Plan struct {
			Legs []struct {
				WindChanges *struct {
					Changes []struct {
						Label      string `json:"label"`
						From       string `json:"from"`
						To         string `json:"to"`
						SpeedMaxKn int    `json:"speed_max_kn"`
						Sector     string `json:"sector"`
					} `json:"changes"`
					NotChecked string `json:"not_checked"`
				} `json:"wind_changes"`
			} `json:"legs"`
			NightWinds []struct {
				Stop        string `json:"stop"`
				Date        string `json:"date"`
				DateLabel   string `json:"date_label"`
				WindChanges *struct {
					Changes []json.RawMessage `json:"changes"`
				} `json:"wind_changes"`
			} `json:"night_winds"`
		} `json:"plan"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	legs := out.Plan.Legs
	if len(legs) == 0 || legs[0].WindChanges == nil || len(legs[0].WindChanges.Changes) != 1 {
		t.Fatalf("leg 0 should carry the one change the table shows: %s", raw)
	}
	c := legs[0].WindChanges.Changes[0]
	if c.Label != "10 AM" || c.From != "N" || c.To != "SE" || c.SpeedMaxKn != 18 || c.Sector != "ahead" {
		t.Errorf("change = %+v", c)
	}
	if len(out.Plan.NightWinds) == 0 {
		t.Fatalf("the lay-day night's wind changes are missing: %s", raw)
	}
	n := out.Plan.NightWinds[0]
	if n.Stop != "Pelican Bay" || n.Date != "2026-11-14" || n.DateLabel != "Sat 14 Nov" || n.WindChanges == nil {
		t.Errorf("night = %+v", n)
	}
}

func TestPlanReportOf_ARoutedLegIsFoundByItsLegNotItsPlace(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	legs[0].DistanceNm, legs[1].DistanceNm = 11, 22
	// An assessment that holds only the second leg: its position in the list is 0.
	a := planAssessment{Legs: []planLegAssessment{{Leg: 1, Date: "2026-11-15"}}}
	r, err := planReportOf(p, legs, a, planSlips{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Legs) != 1 || r.Legs[0].From != p.Stops[1].Name || r.Legs[0].DistanceNm == nil || *r.Legs[0].DistanceNm != 22 {
		t.Fatalf("leg = %+v, want %s's own 22 nm", r.Legs, p.Stops[1].Name)
	}
}

// get_plans is a read tool, open to an outside assistant and to a replay on a
// pinned clock. It must not write the day's first suggestion, which the Forecast
// page's still-in-port prompt is timed from.
func TestGetPlans_NeverWritesTheDaysFirstSuggestion(t *testing.T) {
	deps, store, f := plansToolEnv(t)
	f.store = store
	if _, err := store.CreatePlan(plan{Name: "Home run", Stops: stopsOf("2026-11-14", 0, 1, 0)}); err != nil {
		t.Fatal(err)
	}
	out := callGetPlans(t, deps, `{}`)
	if out.Plan == nil {
		t.Fatalf("no plan in full: %+v", out)
	}
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM plan_first_suggestions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("get_plans kept %d first suggestions (%v)", n, err)
	}
}

// With a first suggestion already kept, the tool reads it, so its still-in-port
// answer is the page's.
func TestToolSuggested_ReadsTheKeptSuggestionAndWritesNothing(t *testing.T) {
	store := newTestDocumentStore(t)
	loc := paNow.Location()
	given := paNow.Add(3 * time.Hour)
	d := planAssessDeps{keptSuggestion: store.KeptSuggestion}
	suggested := readOnlySuggested(d)
	got, err := suggested(1, "k", "2026-11-14", given)
	if err != nil || !got.Equal(given) {
		t.Fatalf("no row kept: got %v, %v; want the time given", got, err)
	}
	first := paNow.Add(time.Hour).In(loc)
	if _, err := store.FirstSuggestion(1, "k", "2026-11-14", first); err != nil {
		t.Fatal(err)
	}
	got, err = suggested(1, "k", "2026-11-14", given)
	if err != nil || !got.Equal(first) {
		t.Fatalf("row kept: got %v, %v; want %v", got, err, first)
	}
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM plan_first_suggestions`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d rows (%v), want the one planted", n, err)
	}
}
