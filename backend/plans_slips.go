package main

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// Slip cost (ADR 0288). With an arrive-by on a stop that is still ahead, the plan
// says what a slip would meet: for each N from one day to the slack plus one,
// every leg still to do moves N days later (a night added at the stop the first
// of them leaves, so the plan's own date rule shifts the rest), the plan is
// assessed as it would then stand, and the worst leg is named. The shifted plans
// exist only in memory; nothing is saved.

const (
	// planSlipMaxDays is the most slips priced. Past the forecast's reach a slip
	// has nothing more to say.
	planSlipMaxDays = assistantMaxForecastDays
	// planSlipWorkers is how many slips are assessed at once. Each assessment
	// reads its legs planLegWorkers at a time.
	planSlipWorkers = 3
)

// planSlipsDeadline bounds the whole slips request.
var planSlipsDeadline = 120 * time.Second

// planSlip is what slipping N days meets. Leg is the worst leg, Date the day it
// would leave, and Verdict, Limiting, Note and NoteKind are as the assessment has
// them for that leg after the shift.
type planSlip struct {
	Days int `json:"days"`
	// LateByDays is how far past the arrive-by the slip puts the plan, 0 while it
	// still makes it.
	LateByDays int          `json:"late_by_days"`
	Leg        int          `json:"leg"`
	Date       string       `json:"date"`
	Verdict    string       `json:"verdict"`
	Limiting   string       `json:"limiting"`
	Note       string       `json:"note"`
	NoteKind   planNoteKind `json:"note_kind"`
}

type planSlips struct {
	// Problem is the one thing that stopped the slips being priced.
	Problem string `json:"problem"`
	// SlackDays is the tightest slack the plan has; negative when it is already late.
	SlackDays int `json:"slack_days"`
	// Capped is true when the slack runs past planSlipMaxDays and the slips stop there.
	Capped bool       `json:"capped"`
	Slips  []planSlip `json:"slips"`
}

// shiftedPlan is the plan with every leg from the first one still to do moved n
// days later: n nights more at the stop that leg leaves. The input is untouched.
func shiftedPlan(p plan, firstToDo, n int) (plan, error) {
	out := p
	out.Stops = copyStops(p.Stops)
	out.Stops[firstToDo].Nights += n
	if err := rechainStops(out.Stops); err != nil {
		return plan{}, err
	}
	return out, nil
}

// slipRank orders what a leg can say, worst first: a No-Go, then no data (or no
// forecast yet, or any other reason the leg has no verdict: no daylight window,
// a gap in the forecast, an overnight leg), then Marginal, then Go. A leg that
// cannot be sailed as slipped is at least as bad as one we know nothing about.
func slipRank(l planLegAssessment) int {
	switch {
	case l.Verdict == "no_go":
		return 3
	case l.Verdict == "no_data", l.NoteKind == planKindNoForecast:
		return 2
	case l.Verdict == "marginal":
		return 1
	case l.Verdict == "go":
		return 0
	case l.Note != "":
		return 2
	}
	return -1
}

// worstSlipLeg is the worst of the legs still to do, the earliest among equals. A
// run of legs that say nothing but a note gives the first of them.
func worstSlipLeg(legs []planLegAssessment) planLegAssessment {
	var worst planLegAssessment
	best := -2
	for _, l := range legs {
		if l.Progress != "" {
			continue
		}
		if r := slipRank(l); r > best {
			worst, best = l, r
		}
	}
	return worst
}

// planSlipSetup is what pricing the slips starts from.
type planSlipSetup struct {
	progress []string
	slack    planSlack
	first    int // the first leg still to do
	count    int // how many slips to price
	capped   bool
}

// prepareSlips reads the progress and the slack, and says what is to be priced.
// done is true, and out is the answer, when there is nothing to price: no
// arrive-by ahead, a plan already late, every leg done, or progress that could
// not be read. Nothing is routed first.
func prepareSlips(ctx context.Context, p plan, d planAssessDeps) (s planSlipSetup, out planSlips, done bool) {
	out = planSlips{Slips: []planSlip{}}
	// A plan with no arrive-by has nothing to price: the passages are not read.
	if _, ok, err := planSlackOf(p, nil, 0); err != nil {
		out.Problem = "Slack could not be worked out: " + err.Error()
		return s, out, true
	} else if !ok {
		return s, out, true
	}
	var scratch planAssessment
	var underway *planUnderway
	s.progress, underway = d.readProgress(ctx, p, &scratch)
	if scratch.ProgressError != "" {
		out.Problem = "Progress could not be read: " + scratch.ProgressError
		return s, out, true
	}
	slack, ok, first, err := planOutlook(p, s.progress, d.now(), underway)
	if err != nil {
		out.Problem = "Slack could not be worked out: " + err.Error()
		return s, out, true
	}
	if !ok {
		return s, out, true
	}
	out.SlackDays = slack.Days
	if slack.Days < 0 || first < 0 {
		return s, out, true
	}
	s.slack, s.first, s.count = slack, first, slack.Days+1
	if s.count > planSlipMaxDays {
		s.count, s.capped = planSlipMaxDays, true
	}
	return s, out, false
}

// slipsFor prices the plan's slips. route gives the plan's routed legs, asked for
// only once there is something to price. d is the assessment's readers; the slips
// judge verdicts alone.
func slipsFor(ctx context.Context, p plan, d planAssessDeps, route func(context.Context) []planLeg) planSlips {
	d = d.pinned()
	setup, out, done := prepareSlips(ctx, p, d)
	if done {
		return out
	}
	return priceSlips(ctx, p, route(ctx), d, setup, out)
}

// priceSlips assesses the plan slipped one day at a time, a few at once.
func priceSlips(ctx context.Context, p plan, legs []planLeg, d planAssessDeps, s planSlipSetup, out planSlips) planSlips {
	out.Capped = s.capped
	// The limits and the boat's speed are the same for every slip: read them once.
	env := d.readEnv(ctx, d.now(), true)
	if env.problem != "" {
		out.Problem = env.problem
		return out
	}
	d.warmForecast(ctx, p, legs, s.progress)

	opts := planAssessOptions{VerdictsOnly: true, Progress: s.progress, Env: &env}
	slips := make([]planSlip, s.count)
	sem := make(chan struct{}, planSlipWorkers)
	var wg sync.WaitGroup
	for n := 1; n <= s.count; n++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			// A slip is measured on top of the plan projected from today.
			late := max(0, n-s.slack.Days)
			// A slip that cannot be checked says so in its own line and leaves the
			// others as they were priced.
			unchecked := func(why string) planSlip {
				return planSlip{Days: n, LateByDays: late, Leg: -1, Note: "could not be checked: " + why, NoteKind: planKindOther}
			}
			sp, err := shiftedPlan(p, s.first, s.slack.BehindDays+n)
			if err != nil {
				slips[n-1] = unchecked("the slipped plan could not be dated: " + err.Error())
				return
			}
			a := assessPlan(ctx, sp, legs, d, opts)
			if a.Problem != "" {
				slips[n-1] = unchecked(a.Problem)
				return
			}
			w := worstSlipLeg(a.Legs)
			slips[n-1] = planSlip{
				Days: n, LateByDays: late,
				Leg: w.Leg, Date: w.Date, Verdict: w.Verdict, Limiting: w.Limiting, Note: w.Note, NoteKind: w.NoteKind,
			}
		}()
	}
	wg.Wait()
	out.Slips = slips
	return out
}

// warmForecast reads the forecast at every distinct point of the legs still to
// do, once, before the slips fan out, so each of them finds it kept. A read that
// fails is left for the slips, which each read the points again and say so.
func (d planAssessDeps) warmForecast(ctx context.Context, p plan, legs []planLeg, progress []string) {
	var plans []passageLegPlan
	for _, leg := range legs {
		if leg.From >= len(progress) || progress[leg.From] != "" || leg.Error != "" || len(leg.Route) < 2 || leg.DistanceNm <= 0 {
			continue
		}
		// The points a leg is read at do not depend on its hours.
		plans = append(plans, planLegPassage(leg.From, p.Stops[leg.From], p.Stops[leg.From+1], leg, 1))
	}
	if len(plans) == 0 {
		return
	}
	src, err := d.sources(func() (string, error) { return p.Stops[0].Timezone, nil }, d.now())
	if err == nil {
		err = newPassageAssessor(passageLimits{}, src.fc, src.tides, d.sun).prefetchLegs(ctx, plans)
	}
	if err != nil {
		log.Printf("plans: forecast not warmed for the slips: %v", err)
	}
}

// planSlipsHandler is GET /api/plans/:id/slips: what slipping the plan a day at a
// time up to its slack plus one would meet. Asked apart from the assessment so
// the table does not wait for it.
func planSlipsHandler(c echo.Context) error {
	deps := planAssessDepsFor().pinned()
	d, ok, err := loadDecoratedPlan(c, deps.now())
	if !ok {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), planSlipsDeadline)
	defer cancel()
	return c.JSON(http.StatusOK, slipsFor(ctx, d, deps, func(ctx context.Context) []planLeg { return planLegs(ctx, d) }))
}
