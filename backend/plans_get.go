package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// get_plans (ADR 0289) is how Mate, a review and an outside assistant read the
// passage plans. It gives every plan in brief and the current or named one in
// full, with what the Forecast page's table shows beside each leg: the verdict,
// the departure and arrival, the current, the slack and what a slip would meet.
// Every figure comes from the code the page's endpoints run (planLegs,
// assessPlan, slipsFor); the tool judges nothing of its own, so a leg has one
// verdict wherever it is read.

type getPlansArgs struct {
	ID   *int64 `json:"id"`
	Name string `json:"name"`
}

// planBrief is one plan in the list of every plan.
type planBrief struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	FirstDate      string `json:"first_date"`
	FirstDateLabel string `json:"first_date_label"`
	LastDate       string `json:"last_date"`
	LastDateLabel  string `json:"last_date_label"`
	Status         string `json:"status"`
}

// planReportStop is a stop with the finished label of every date and time it
// carries; the raw values are what a write sends back, the labels are what Mate
// says.
type planReportStop struct {
	Name            string    `json:"name"`
	Lat             float64   `json:"lat"`
	Lon             float64   `json:"lon"`
	Arrive          string    `json:"arrive"`
	ArriveLabel     string    `json:"arrive_label"`
	Nights          int       `json:"nights"`
	Depart          string    `json:"depart"`
	DepartLabel     string    `json:"depart_label"`
	DepartTime      string    `json:"depart_time,omitempty"`
	DepartTimeLabel string    `json:"depart_time_label,omitempty"`
	ArriveBy        string    `json:"arrive_by,omitempty"`
	ArriveByLabel   string    `json:"arrive_by_label,omitempty"`
	Vias            []planVia `json:"vias"`
	Timezone        string    `json:"timezone"`
}

// planReportLeg is a routed leg with its assessment beside it. Depart and Arrive
// are finished boat-time labels in the zone of the stop each is at.
type planReportLeg struct {
	From            string   `json:"from"`
	To              string   `json:"to"`
	Date            string   `json:"date"`
	DateLabel       string   `json:"date_label"`
	DistanceNm      *float64 `json:"distance_nm,omitempty"`
	CourseDeg       *int     `json:"course_deg,omitempty"`
	RouteError      string   `json:"route_error,omitempty"`
	Progress        string   `json:"progress,omitempty"`
	StillInPort     bool     `json:"still_in_port,omitempty"`
	Verdict         string   `json:"verdict,omitempty"`
	Limiting        string   `json:"limiting,omitempty"`
	Note            string   `json:"note,omitempty"`
	Depart          string   `json:"depart,omitempty"`
	DepartSuggested bool     `json:"depart_suggested,omitempty"`
	Arrive          string   `json:"arrive,omitempty"`
	Hours           *float64 `json:"hours,omitempty"`
	SpeedKn         *float64 `json:"speed_kn,omitempty"`
	CurrentKn       *float64 `json:"current_kn,omitempty"`
	CurrentNote     string   `json:"current_note,omitempty"`
	NotChecked      []string `json:"not_checked,omitempty"`
	// WindChanges are the changes in the wind between the departure and the
	// arrival, as the table shows them: Label is already the boat clock.
	WindChanges planWindChanges `json:"wind_changes"`
}

// planReportNightWinds is the wind changes on one lay-day night at a stop, 18:00
// to 06:00 on the stop's clock.
type planReportNightWinds struct {
	Stop        string          `json:"stop"`
	Date        string          `json:"date"`
	DateLabel   string          `json:"date_label"`
	WindChanges planWindChanges `json:"wind_changes"`
}

type planReportSlack struct {
	Days       int    `json:"days"`
	Stop       string `json:"stop"`
	BehindDays int    `json:"behind_days"`
}

type planReportWarning struct {
	Stop      string `json:"stop"`
	Date      string `json:"date"`
	DateLabel string `json:"date_label"`
	Warning   string `json:"warning"`
}

type planReportSlip struct {
	Days       int    `json:"days"`
	LateByDays int    `json:"late_by_days"`
	Leg        string `json:"leg,omitempty"`
	Date       string `json:"date,omitempty"`
	DateLabel  string `json:"date_label,omitempty"`
	Verdict    string `json:"verdict,omitempty"`
	Limiting   string `json:"limiting,omitempty"`
	Note       string `json:"note,omitempty"`
}

type planReportSlips struct {
	Problem   string           `json:"problem,omitempty"`
	SlackDays int              `json:"slack_days"`
	Capped    bool             `json:"capped,omitempty"`
	Slips     []planReportSlip `json:"slips"`
}

// planReport is one plan in full.
type planReport struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Status         string `json:"status"`
	FirstDate      string `json:"first_date"`
	FirstDateLabel string `json:"first_date_label"`
	LastDate       string `json:"last_date"`
	LastDateLabel  string `json:"last_date_label"`
	// IsCurrent is true for the plan the Forecast page opens on.
	IsCurrent bool `json:"is_current"`
	// Version is the plan as last saved, for base_version on a proposal.
	Version       string              `json:"version"`
	Stops         []planReportStop    `json:"stops"`
	Legs          []planReportLeg     `json:"legs"`
	Problem       string              `json:"problem,omitempty"`
	ProgressError string              `json:"progress_error,omitempty"`
	WarningsError string              `json:"warnings_error,omitempty"`
	Slack         *planReportSlack    `json:"slack,omitempty"`
	Warnings      []planReportWarning `json:"warnings,omitempty"`
	// NightWinds are the wind changes on each lay-day night still ahead and inside
	// the forecast's reach.
	NightWinds []planReportNightWinds `json:"night_winds,omitempty"`
	Slips      *planReportSlips       `json:"slips,omitempty"`
}

type getPlansResult struct {
	Plans []planBrief `json:"plans"`
	Plan  *planReport `json:"plan,omitempty"`
	Note  string      `json:"note,omitempty"`

	// baseNote is the note before shrink added to it; omitted is what it dropped.
	baseNote string
	omitted  []string
}

// shrink drops detail a result too big for Mate cannot carry, one kind at a
// time, least needed first: the list of the other plans, the slips one by one,
// the wind changes on the nights, then the warning text. The stops, the version and every leg's verdict stay, and
// the note says what went. It reports false when there is nothing left to drop.
func (r *getPlansResult) shrink() bool {
	var dropped string
	switch {
	case r.Plan != nil && len(r.Plans) > 1:
		kept := []planBrief{}
		for _, b := range r.Plans {
			if b.ID == r.Plan.ID {
				kept = append(kept, b)
			}
		}
		r.Plans, dropped = kept, "the list of the other plans"
	case r.Plan != nil && r.Plan.Slips != nil && len(r.Plan.Slips.Slips) > 0:
		r.Plan.Slips.Slips, dropped = []planReportSlip{}, "what each slip would meet"
	case r.Plan != nil && len(r.Plan.NightWinds) > 0:
		r.Plan.NightWinds, dropped = nil, "the wind changes on the nights at stops"
	case r.Plan != nil && len(r.Plan.Warnings) > 0:
		r.Plan.Warnings, dropped = nil, "the warnings on the stops"
	default:
		return false
	}
	if len(r.omitted) == 0 {
		r.baseNote = r.Note
	}
	r.omitted = append(r.omitted, dropped)
	r.Note = strings.TrimSpace(r.baseNote + " Some detail was left out to fit: " + strings.Join(r.omitted, ", ") + ".")
	return true
}

// planDayLabel is a plan's calendar day as the operator reads it, "Mon 16 Nov".
func planDayLabel(day string) (string, error) {
	t, err := time.Parse(planDateLayout, day)
	if err != nil {
		return "", fmt.Errorf("%q is not a date: %w", day, err)
	}
	return boatDay(t, time.UTC), nil
}

// planClockLabel is a stop's "HH:MM" departure time as the operator reads it,
// "6.30 AM".
func planClockLabel(hhmm string) (string, error) {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return "", fmt.Errorf("%q is not a time of day: %w", hhmm, err)
	}
	return boatClock(t, time.UTC), nil
}

// planArriveByLabel is an "YYYY-MM-DDTHH:MM" arrive-by as "Mon 16 Nov 5.00 PM".
func planArriveByLabel(v string) (string, error) {
	t, err := time.Parse("2006-01-02T15:04", v)
	if err != nil {
		return "", fmt.Errorf("%q is not a date and time: %w", v, err)
	}
	return boatDayClock(t, time.UTC), nil
}

// optionalLabel labels a value that may be unset: "" stays "".
func optionalLabel(v string, label func(string) (string, error)) (string, error) {
	if v == "" {
		return "", nil
	}
	return label(v)
}

// planInstantLabel is an assessment's RFC3339 instant as a finished label on the
// wall clock of the zone it carries, "Sat 14 Nov 8.00 AM"; "" stays "".
func planInstantLabel(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return "", fmt.Errorf("%q is not a time: %w", s, err)
	}
	return boatDayClock(t, t.Location()), nil
}

// buildPlanReport routes, assesses and prices the slips of a decorated plan with
// the code the Forecast page's endpoints run, on the clock deps carries, which
// the caller has pinned.
func buildPlanReport(ctx context.Context, p plan, deps planAssessDeps, isCurrent bool) (planReport, error) {
	legs := planLegs(ctx, p)
	actx, cancel := context.WithTimeout(ctx, planAssessmentDeadline)
	defer cancel()
	// The limits and the speed are read once and shared with the slips, which run
	// after the assessment so that they find its forecast kept.
	env := deps.readEnv(actx, deps.now(), len(p.Stops) > 1)
	assessment := assessPlan(actx, p, legs, deps, planAssessOptions{Env: &env})
	sctx, scancel := context.WithTimeout(ctx, planSlipsDeadline)
	defer scancel()
	slips := slipsWithEnv(sctx, p, deps, &env, func(context.Context) []planLeg { return legs })
	if err := ctx.Err(); err != nil {
		return planReport{}, err
	}
	return planReportOf(p, legs, assessment, slips, isCurrent)
}

func planReportOf(p plan, legs []planLeg, a planAssessment, slips planSlips, isCurrent bool) (planReport, error) {
	r := planReport{
		ID: p.ID, Name: p.Name, Status: p.Status, FirstDate: p.FirstDate, LastDate: p.LastDate, IsCurrent: isCurrent,
		Stops: make([]planReportStop, len(p.Stops)), Legs: make([]planReportLeg, len(a.Legs)),
		Problem: a.Problem, ProgressError: a.ProgressError, WarningsError: a.WarningsError,
	}
	var err error
	if r.FirstDateLabel, err = planDayLabel(p.FirstDate); err != nil {
		return planReport{}, fmt.Errorf("first date: %w", err)
	}
	if r.LastDateLabel, err = planDayLabel(p.LastDate); err != nil {
		return planReport{}, fmt.Errorf("last date: %w", err)
	}
	r.Version = planVersion(p)
	for i, s := range p.Stops {
		arrive, err := planDayLabel(s.Arrive)
		if err != nil {
			return planReport{}, fmt.Errorf("stop %d (%s) arrival: %w", i+1, s.Name, err)
		}
		depart, err := planDayLabel(s.Depart)
		if err != nil {
			return planReport{}, fmt.Errorf("stop %d (%s) departure: %w", i+1, s.Name, err)
		}
		departTime, err := optionalLabel(s.DepartTime, planClockLabel)
		if err != nil {
			return planReport{}, fmt.Errorf("stop %d (%s) departure time: %w", i+1, s.Name, err)
		}
		arriveBy, err := optionalLabel(s.ArriveBy, planArriveByLabel)
		if err != nil {
			return planReport{}, fmt.Errorf("stop %d (%s) arrive-by: %w", i+1, s.Name, err)
		}
		r.Stops[i] = planReportStop{
			Name: s.Name, Lat: s.Lat, Lon: s.Lon, Arrive: s.Arrive, ArriveLabel: arrive, Nights: s.Nights,
			Depart: s.Depart, DepartLabel: depart, DepartTime: s.DepartTime, DepartTimeLabel: departTime,
			ArriveBy: s.ArriveBy, ArriveByLabel: arriveBy, Vias: s.Vias, Timezone: s.Timezone,
		}
	}
	// A routed leg is found by the leg it is, never by where the assessment lists it.
	byFrom := map[int]planLeg{}
	for _, l := range legs {
		byFrom[l.From] = l
	}
	for i, l := range a.Legs {
		out, err := planReportLegOf(p, l, byFrom[l.Leg])
		if err != nil {
			return planReport{}, err
		}
		r.Legs[i] = out
	}
	if a.Slack != nil {
		r.Slack = &planReportSlack{Days: a.Slack.Days, Stop: p.Stops[a.Slack.Stop].Name, BehindDays: a.Slack.BehindDays}
	}
	for _, st := range a.Stops {
		for _, n := range st.Nights {
			label, err := planDayLabel(n.Date)
			if err != nil {
				return planReport{}, fmt.Errorf("warning at %s: %w", p.Stops[st.Stop].Name, err)
			}
			r.Warnings = append(r.Warnings, planReportWarning{Stop: p.Stops[st.Stop].Name, Date: n.Date, DateLabel: label, Warning: n.Warning})
		}
	}
	for _, n := range a.NightWinds {
		label, err := planDayLabel(n.Date)
		if err != nil {
			return planReport{}, fmt.Errorf("night winds at %s: %w", p.Stops[n.Stop].Name, err)
		}
		r.NightWinds = append(r.NightWinds, planReportNightWinds{Stop: p.Stops[n.Stop].Name, Date: n.Date, DateLabel: label, WindChanges: n.planWindChanges})
	}
	if slips.Problem != "" || len(slips.Slips) > 0 {
		out := &planReportSlips{Problem: slips.Problem, SlackDays: slips.SlackDays, Capped: slips.Capped, Slips: []planReportSlip{}}
		for _, s := range slips.Slips {
			row := planReportSlip{Days: s.Days, LateByDays: s.LateByDays, Date: s.Date, Verdict: s.Verdict, Limiting: s.Limiting, Note: s.Note}
			if s.Leg >= 0 && s.Leg < len(p.Stops)-1 {
				row.Leg = p.Stops[s.Leg].Name + " to " + p.Stops[s.Leg+1].Name
			}
			label, err := optionalLabel(s.Date, planDayLabel)
			if err != nil {
				return planReport{}, fmt.Errorf("slip %d date: %w", s.Days, err)
			}
			row.DateLabel = label
			out.Slips = append(out.Slips, row)
		}
		r.Slips = out
	}
	return r, nil
}

func planReportLegOf(p plan, l planLegAssessment, routed planLeg) (planReportLeg, error) {
	date, err := planDayLabel(l.Date)
	if err != nil {
		return planReportLeg{}, fmt.Errorf("leg %d date: %w", l.Leg+1, err)
	}
	depart, err := planInstantLabel(l.Depart)
	if err != nil {
		return planReportLeg{}, fmt.Errorf("leg %d departure: %w", l.Leg+1, err)
	}
	arrive, err := planInstantLabel(l.Arrive)
	if err != nil {
		return planReportLeg{}, fmt.Errorf("leg %d arrival: %w", l.Leg+1, err)
	}
	out := planReportLeg{
		From: p.Stops[l.Leg].Name, To: p.Stops[l.Leg+1].Name, Date: l.Date, DateLabel: date,
		Progress: l.Progress, StillInPort: l.StillInPort, Verdict: l.Verdict, Limiting: l.Limiting, Note: l.Note,
		Depart: depart, DepartSuggested: l.DepartSuggested, Arrive: arrive, Hours: l.Hours, SpeedKn: l.SpeedKn,
		CurrentKn: l.CurrentKn, CurrentNote: l.CurrentNote, NotChecked: l.NotChecked, WindChanges: l.WindChanges,
	}
	if routed.Error != "" {
		out.RouteError = routed.Error
	} else if routed.DistanceNm > 0 {
		nm, course := routed.DistanceNm, routed.CourseDeg
		out.DistanceNm, out.CourseDeg = &nm, &course
	}
	return out, nil
}

// planChoice finds the plan a call asks for among the decorated plans: the
// named or numbered one, else the one the Forecast page opens on. current is
// the index of the plan the page opens on, whichever was asked for; status is
// that plan's status when it is the one chosen, and "" for a named or numbered one.
func planChoice(plans []plan, args getPlansArgs) (idx, current int, status string, err error) {
	if args.ID != nil && strings.TrimSpace(args.Name) != "" {
		return -1, -1, "", fmt.Errorf("give the plan's id or its name, not both")
	}
	current, currentStatus := selectCurrentPlan(plans)
	describe := func() string {
		names := make([]string, len(plans))
		for i, p := range plans {
			names[i] = fmt.Sprintf("%q (id %d)", p.Name, p.ID)
		}
		return strings.Join(names, ", ")
	}
	switch {
	case args.ID != nil:
		for i, p := range plans {
			if p.ID == *args.ID {
				return i, current, "", nil
			}
		}
		return -1, -1, "", fmt.Errorf("there is no plan with id %d; the plans are %s", *args.ID, describe())
	case strings.TrimSpace(args.Name) != "":
		want := strings.TrimSpace(args.Name)
		found := -1
		for i, p := range plans {
			if !strings.EqualFold(p.Name, want) {
				continue
			}
			if found >= 0 {
				return -1, -1, "", fmt.Errorf("more than one plan is called %q; name one by id: %s", want, describe())
			}
			found = i
		}
		if found < 0 {
			return -1, -1, "", fmt.Errorf("there is no plan called %q; the plans are %s", want, describe())
		}
		return found, current, "", nil
	}
	return current, current, currentStatus, nil
}

func (d assistantToolDeps) executeGetPlans(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	store, err := d.recordStore("get_plans")
	if err != nil {
		return "", err
	}
	var args getPlansArgs
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := strictDecode("get_plans", raw, &args); err != nil {
			return "", err
		}
	}
	stored, err := store.ListPlans()
	if err != nil {
		return "", fmt.Errorf("get_plans: %w", err)
	}
	// The clock and the providers are the tool deps' own, so a pinned or replayed
	// run reads the plans as it did then.
	deps := planAssessDepsFor(d).pinned()
	plans := make([]plan, 0, len(stored))
	res := getPlansResult{Plans: []planBrief{}}
	for _, p := range stored {
		dp, err := decoratePlan(p, deps.now())
		if err != nil {
			return "", fmt.Errorf("get_plans: %w", err)
		}
		first, err := planDayLabel(dp.FirstDate)
		if err != nil {
			return "", fmt.Errorf("get_plans: plan %d first date: %w", dp.ID, err)
		}
		last, err := planDayLabel(dp.LastDate)
		if err != nil {
			return "", fmt.Errorf("get_plans: plan %d last date: %w", dp.ID, err)
		}
		plans = append(plans, dp)
		res.Plans = append(res.Plans, planBrief{ID: dp.ID, Name: dp.Name, FirstDate: dp.FirstDate, FirstDateLabel: first, LastDate: dp.LastDate, LastDateLabel: last, Status: dp.Status})
	}
	if len(plans) == 0 {
		res.Note = "No plan is saved."
		return capToolResultJSON(&res, nil)
	}
	idx, current, status, err := planChoice(plans, args)
	if err != nil {
		return "", fmt.Errorf("get_plans: %w", err)
	}
	report, err := buildPlanReport(ctx, plans[idx], deps, idx == current)
	if err != nil {
		return "", fmt.Errorf("get_plans: %w", err)
	}
	res.Plan = &report
	if status == planStatusPast {
		res.Note = "Every plan has finished. This is the one that ended most recently."
	}
	return capToolResultJSON(&res, res.shrink)
}

var mateToolGetPlans = mateTool{
	name:      "get_plans",
	effect:    effectReads,
	audiences: audienceReviewsAndMCP,
	function: openRouterFunctionDef{
		Description: "Read the operator's passage plans from the Forecast page. With no arguments it lists every plan in brief (id, name, first and last date, status) " +
			"and gives the current one in full, the plan the page opens on. Pass id or name to be given another plan in full. A plan in full has its stops " +
			"(name, lat, lon, arrival, nights, departure, the departure time and arrive-by the operator set, and via places), its legs and the version to quote as base_version. " +
			"Each leg carries the plan's own verdict (go, marginal, no_go or no_data) with the figure that decided it, the departure and arrival, the modelled current, " +
			"whether it is done or skipped, and the slack before the tightest arrive-by with what slipping a day at a time would meet. " +
			"For a leg of a plan, quote the plan's own verdict and its limiting figure and never judge the leg again. Use assess_passage_window only for a date or time that is not in a plan. " +
			"Use it when the operator talks about the plan, the next stop, \"the marina\", \"our stay\" or being there, and for the times a stop is reached. " +
			"Take dates and times from the labels as given and never work out a weekday or a time yourself. Pass a stop's lat and lon to get_wind_forecast and get_tides. " + rulePlans,
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"id": {"type": "integer", "description": "The plan's id, from the list. Leave out to be given the current plan."},
				"name": {"type": "string", "description": "The plan's name, instead of the id."}
			}
		}`),
	},
	execute: plainTool(assistantToolDeps.executeGetPlans),
	describe: func(args json.RawMessage) string {
		return "Reading the plans…"
	},
}
