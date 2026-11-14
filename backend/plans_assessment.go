package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// The passage plan's verdicts (ADR 0286). Each routed leg is judged with the
// rules Mate's assess_passage_window uses (passageAssessor.cell, ADR 0191 and
// 0194) against the operator's passage limits, at the departure the operator
// set or, with none set, the best one inside the day's daylight: not a No-Go,
// Go before Marginal, then the fairest current (rankTidalDepartures). Nothing
// here asks a model: a figure that cannot be had is left out with its reason,
// never guessed.
//
// No omitempty on the fields the Forecast page binds: its TypeScript
// interface declares every one.

const (
	// planAssessMaxLegHours is the longest leg judged: a leg that runs past a
	// day is an overnight leg, which plans do not model yet.
	planAssessMaxLegHours = 24
	// planWarningDays is how many days ahead a weekday-named warning section
	// can be matched to a night.
	planWarningDays = 7
	// planDepartureStep is the grid a suggested departure sits on, the ranking's
	// own step.
	planDepartureStep = assistantTidalDepartureStep

	planNoteNoWindow     = "No daylight window"
	planNoteWindowPassed = "Today's window has passed"
	planNoteNoForecast   = "No forecast yet"
	planNoteNotRouted    = "Not routed"
	planNotePast         = "Date has passed"
	planNoteDepartPast   = "Departure time has passed"
	planNoteOvernight    = "Overnight legs are not assessed yet"
	planNoteGap          = "Forecast missing for part of the leg"
)

// planNoteKind says what sort of note a leg carries, so the table does not read
// the words.
type planNoteKind string

const (
	planKindNone         planNoteKind = ""
	planKindProblem      planNoteKind = "problem"       // the one problem said above the table for the whole plan
	planKindNoForecast   planNoteKind = "no_forecast"   // the day is past the forecast
	planKindNotRouted    planNoteKind = "not_routed"    // the leg did not route
	planKindPast         planNoteKind = "past"          // the day has gone
	planKindOvernight    planNoteKind = "overnight"     // the leg runs past a day
	planKindNoWindow     planNoteKind = "no_window"     // no departure fits the daylight
	planKindWindowPassed planNoteKind = "window_passed" // today had a daylight window and it has gone
	planKindNoDeparture  planNoteKind = "no_departure"  // the ranking gave no departure
	planKindGap          planNoteKind = "gap"           // the forecast has a hole in the leg
	planKindOther        planNoteKind = "other"
)

var errNoDepartureRanked = errors.New("no departure in the window could be ranked")

// planAssessmentDeadline bounds the whole assessment request.
var planAssessmentDeadline = 90 * time.Second

// planLegAssessment is one leg's verdict and the figures beside it.
type planLegAssessment struct {
	// Leg is the stop the leg leaves; Date is the day it leaves, at that stop.
	Leg  int    `json:"leg"`
	Date string `json:"date"`
	// Key names the leg across edits, "from|to|n": the two stops (URL-escaped) and
	// which trip between that pair it is, counting from 0. The date is not part of
	// it. The change marks keep a verdict against it (ADR 0288). Renaming a stop
	// gives the legs at it a new key.
	Key string `json:"key"`
	// Progress is "done" or "skipped" when the recorded passages say so, else "".
	// Such a leg has no verdict, departure, suggestion or note.
	Progress string `json:"progress"`
	// StillInPort is true on the leg's departure day when it is an hour past the
	// leg's set or suggested departure and the boat is still at the stop.
	StillInPort bool `json:"still_in_port"`
	// Verdict is go, marginal, no_go or no_data; "" when the leg has none, and
	// Note says why, NoteKind what sort of reason it is. Limiting names the figure
	// that decided it, "gusts 32 kn ahead".
	Verdict  string       `json:"verdict"`
	Limiting string       `json:"limiting"`
	Note     string       `json:"note"`
	NoteKind planNoteKind `json:"note_kind"`
	// Depart and Arrive are instants (RFC3339) in the zone of the stop each is
	// at, "" when there is none. DepartSuggested is true when the operator set no
	// time and Depart is the best the ranking found.
	Depart          string   `json:"depart"`
	DepartSuggested bool     `json:"depart_suggested"`
	Arrive          string   `json:"arrive"`
	Hours           *float64 `json:"hours"`
	SpeedKn         *float64 `json:"speed_kn"`
	// CurrentKn is the modelled current along the course at the departure, in
	// knots: positive helps the boat, negative hinders it. Nil when it could not
	// be read, and CurrentNote says why.
	CurrentKn   *float64 `json:"current_kn"`
	CurrentNote string   `json:"current_note"`
	NotChecked  []string `json:"not_checked"`
	// WindChanges are the changes in the wind between the departure and the ETA,
	// worked out here so the page only draws them (ADR 0287).
	WindChanges planWindChanges `json:"wind_changes"`
}

// planStopNight is an official warning in force the night the boat is at a stop.
type planStopNight struct {
	Date    string `json:"date"`
	Warning string `json:"warning"`
	URL     string `json:"url"`
}

// planStopAssessment lists a stop's nights that have a warning; a stop with
// none is left out.
type planStopAssessment struct {
	Stop   int             `json:"stop"`
	Nights []planStopNight `json:"nights"`
}

type planAssessment struct {
	// AsOf is when the assessment was made; ForecastAsOf is when the oldest
	// forecast it read was fetched, "" when no forecast was read.
	AsOf         string `json:"as_of"`
	ForecastAsOf string `json:"forecast_as_of"`
	// Problem is the one thing that stopped the verdicts for the whole plan, said
	// once: passage limits not set, the forecast down, no recorded passages.
	Problem string `json:"problem"`
	// WarningsError is why official warnings could not be read. No warnings read
	// is not the same as none in force.
	WarningsError string `json:"warnings_error"`
	// ProgressError is why the recorded passages could not be read; no leg is then
	// marked done or skipped, and the plan is judged as still to come.
	ProgressError string `json:"progress_error"`
	// Slack is the room before the tightest arrive-by, nil when no stop that is
	// still ahead has one.
	Slack *planSlack           `json:"slack"`
	Legs  []planLegAssessment  `json:"legs"`
	Stops []planStopAssessment `json:"stops"`
	// NightWinds are the wind changes on each lay-day night, one entry for every
	// night that is still ahead and inside the forecast's reach (ADR 0287).
	NightWinds []planNightWinds `json:"night_winds"`
}

// planAssessOptions narrows an assessment to what its caller needs.
type planAssessOptions struct {
	// VerdictsOnly leaves out what a verdict does not need: the current at a
	// departure the operator set, the warnings on the stops, the slack and the
	// still-in-port check, which with it never writes the day's first suggestion.
	VerdictsOnly bool
	// Progress, when not nil, is each leg's progress already read, one entry for
	// every leg; the assessment does not read the passages again.
	Progress []string
	// Env, when not nil, is the limits and speed already read, shared by several
	// assessments of one request; they are not read again.
	Env *planEnv
}

// planDepartureQuery is a leg as the ranking and the current read it: the routed
// path, the hours under way, and the window a departure may fall in.
type planDepartureQuery struct {
	Route    []assistantLatLon
	Hours    float64
	Earliest time.Time
	Latest   time.Time
}

// planWarningsReader reads the official warnings at a place.
type planWarningsReader func(lat, lon float64) (forecastWarningsBundle, error)

// planAssessDeps are the readers an assessment needs, so a test supplies fakes
// and production wires the configured providers (plans_assessment_wire.go).
type planAssessDeps struct {
	now     func() time.Time
	limits  func() (passageLimits, error)
	sources func(zone func() (string, error), now time.Time) (passageSources, error)
	// speed is the boat's cruising speed in knots, from the passages it recorded.
	speed func(ctx context.Context) (float64, error)
	sun   func(date time.Time, lat, lon float64) (rise, set time.Time, ok bool)
	dawn  func(date time.Time, lat, lon float64) (time.Time, bool)
	// departures ranks every departure in the query's window by the modelled
	// current, fairest first.
	departures func(ctx context.Context, q planDepartureQuery) ([]tidalDeparture, error)
	// currentAlong reads the modelled current for a departure the operator set.
	currentAlong func(ctx context.Context, q planDepartureQuery, dep time.Time) (planCurrentReading, error)
	// warnings resolves the warnings provider once for an assessment made at now.
	warnings func(now time.Time) (planWarningsReader, error)
	// progress is each leg's progress from the recorded passages ("" to do).
	progress func(ctx context.Context, p plan) ([]string, error)
	// underway is where the passage being recorded now began, nil when none is
	// under way. Nil reads none.
	underway func(ctx context.Context) (*planUnderway, error)
	// position is the boat's own trusted fix, or why there is none.
	position func() (lat, lon float64, err error)
	// suggested keeps the first departure suggested for a leg on a day and returns
	// the one kept (documentStore.FirstSuggestion). Nil keeps nothing.
	suggested func(planID int64, legKey, day string, t time.Time) (time.Time, error)
	// keptSuggestion reads the first suggestion kept for a leg on a day, without
	// writing one (documentStore.KeptSuggestion). Nil reads none.
	keptSuggestion func(planID int64, legKey, day string, loc *time.Location) (time.Time, bool, error)
}

// planEnv is what every leg of an assessment shares.
type planEnv struct {
	now           time.Time
	verdictsOnly  bool
	limits        passageLimits
	limitsErr     error
	limitsProblem string
	// problem is the plan's one problem from the limits or the speed, "" when none.
	problem  string
	speed    float64
	speedErr error
}

// planLegWork is a leg on its way to a verdict.
type planLegWork struct {
	res            *planLegAssessment
	i              int
	from, to       planStop
	fromLoc, toLoc *time.Location
	// depart is the operator's time, zero when none is set.
	depart time.Time
	hours  float64
	query  planDepartureQuery
	plan   passageLegPlan
	// course is the leg's course in degrees true, which a wind change is placed
	// against.
	course float64
	// changesRead is true once the leg's wind changes are worked out.
	changesRead bool
	// underway is true for the leg the boat is on. Its wind changes are read from
	// now to eta, the operator's departure time plus the hours, and windReady says
	// it has what that needs.
	underway  bool
	windReady bool
	eta       time.Time
	// cands are the departures to choose among when the operator set none,
	// fairest current first.
	cands      []tidalDeparture
	assessable bool
}

// fail leaves the leg without a verdict and says why.
func (w *planLegWork) fail(kind planNoteKind, note string) {
	w.res.Note, w.res.NoteKind = note, kind
	w.assessable = false
}

// markProgress leaves a leg the boat has done or passed by without a verdict,
// a departure, a note or any reading.
func (w *planLegWork) markProgress(progress string) {
	w.res.Progress = progress
	w.assessable = false
}

func (w *planLegWork) setDeparture(t time.Time, suggested bool) {
	w.res.Depart, w.res.DepartSuggested = fmtLeg(t, w.fromLoc), suggested
	w.res.Arrive = fmtLeg(t.Add(hoursDuration(w.hours)), w.toLoc)
}

func (w *planLegWork) setCurrent(r planCurrentReading, err error) {
	switch {
	case err != nil:
		log.Printf("plans: current not read for %s to %s: %v", w.from.Name, w.to.Name, err)
		w.res.CurrentNote = err.Error()
	case !r.Have:
		w.res.CurrentNote = r.Why
	default:
		kn := r.MeanKn
		w.res.CurrentKn = &kn
	}
}

func ceilToStep(t time.Time, step time.Duration) time.Time {
	r := t.Truncate(step)
	if r.Before(t) {
		r = r.Add(step)
	}
	return r
}

func dayStart(day string, loc *time.Location) (time.Time, error) {
	return time.ParseInLocation(planDateLayout, day, loc)
}

func fmtLeg(t time.Time, loc *time.Location) string { return t.In(loc).Format(time.RFC3339) }

// pinned is d with its clock stopped at the instant of the call. One request
// reads the clock once: a request that straddles midnight must not count the
// days behind on one day and judge the legs on the next, and a forecast kept
// during the request must not be stamped later than the readers that want it.
func (d planAssessDeps) pinned() planAssessDeps {
	now := d.now()
	d.now = func() time.Time { return now }
	return d
}

// assessPlan judges every leg of a decorated plan and reads the warnings on its
// stops. legs are the plan's routed legs, in order.
func assessPlan(ctx context.Context, p plan, legs []planLeg, d planAssessDeps, opts planAssessOptions) planAssessment {
	d = d.pinned()
	var env planEnv
	if opts.Env != nil {
		env = *opts.Env
	} else {
		env = d.readEnv(ctx, d.now(), len(p.Stops) > 1)
	}
	now := env.now
	out := planAssessment{
		AsOf:    now.UTC().Format(time.RFC3339),
		Legs:    make([]planLegAssessment, len(p.Stops)-1),
		Stops:   []planStopAssessment{},
		Problem: env.problem,
		// A night with nothing to say is left out, so the wire carries [] and not null.
		NightWinds: []planNightWinds{},
	}
	env.verdictsOnly = opts.VerdictsOnly
	progress, underway := opts.Progress, (*planUnderway)(nil)
	if progress == nil {
		progress, underway = d.readProgress(ctx, p, &out)
	}
	// Slack counts from the legs still to do; with the passages unread it would be
	// counted from a guess, so it is left out and ProgressError says why.
	if !opts.VerdictsOnly && out.ProgressError == "" {
		s, ok, _, err := planOutlook(p, progress, now, underway)
		switch {
		case err != nil:
			// The plan's own dates are not readable: say so, and leave the slack out.
			if out.Problem == "" {
				out.Problem = "Slack could not be worked out: " + err.Error()
			}
		case ok:
			out.Slack = &s
		}
	}

	byFrom := map[int]planLeg{}
	for _, l := range legs {
		byFrom[l.From] = l
	}
	// The leg the boat is on is the first still to do when the recording began at
	// the stop it leaves (the same reading planBehindDays makes).
	underwayLeg := -1
	if first := slices.Index(progress, ""); !opts.VerdictsOnly && underway != nil && first >= 0 && first < len(out.Legs) &&
		withinArrivedNm(underway.Lat, underway.Lon, p.Stops[first].Lat, p.Stops[first].Lon) {
		underwayLeg = first
	}
	work := make([]*planLegWork, len(out.Legs))
	for i := range out.Legs {
		out.Legs[i] = planLegAssessment{
			Leg: i, Date: p.Stops[i].Depart, Key: planLegKey(p, i), NotChecked: []string{},
			WindChanges: noPlanWindChanges(),
		}
		work[i] = &planLegWork{res: &out.Legs[i], i: i, from: p.Stops[i], to: p.Stops[i+1]}
		if progress[i] != "" {
			work[i].markProgress(progress[i])
			continue
		}
		d.prepareLeg(work[i], byFrom[i], env)
		if i == underwayLeg {
			// With no limits to judge by the leg says so below; nothing is read for it.
			if why := work[i].prepareUnderway(byFrom[i], env); env.limitsProblem == "" && why != "" {
				out.Legs[i].WindChanges.NotChecked, work[i].changesRead = why, true
			}
		}
	}
	d.readAndJudge(ctx, p, work, env, &out)
	if !opts.VerdictsOnly {
		d.markStillInPort(p, work, progress, now, &out)
	}

	// A leg whose note is the plan's one problem says "not checked" and leaves the
	// words to the line above the table.
	for i := range out.Legs {
		if l := &out.Legs[i]; out.Problem != "" && l.Note == out.Problem {
			l.NoteKind = planKindProblem
		}
	}
	// A leg no forecast was read for says why its wind changes were not either.
	if !opts.VerdictsOnly {
		for i, w := range work {
			if !w.changesRead {
				out.Legs[i].WindChanges = unassessedWindChanges(out.Legs[i], env, w.underway)
			}
		}
	}
	return out
}

// readEnv reads what every leg shares: the passage limits and the boat's speed.
// A limits problem is the plan's problem; a speed problem is, when nothing else
// is.
func (d planAssessDeps) readEnv(ctx context.Context, now time.Time, anyLeg bool) planEnv {
	env := planEnv{now: now}
	limits, err := d.limits()
	switch {
	case err != nil:
		env.limitsProblem = "Passage limits could not be read: " + err.Error()
	case !limits.anyWindNoGo():
		env.limitsProblem = "Passage limits aren't set: Settings → Mate → Passage limits"
	}
	env.limits, env.limitsErr = limits, err
	env.problem = env.limitsProblem
	if anyLeg {
		env.speed, env.speedErr = d.speed(ctx)
		if env.speedErr != nil && env.problem == "" {
			env.problem = "No ETA: " + env.speedErr.Error()
		}
	}
	return env
}

// prepareLeg works out what a leg's verdict needs that no provider is asked for:
// whether it is routed and still to come, the operator's departure, the ETA, the
// route as the checks read it and, with no departure set, the window one may be
// suggested in. It marks the leg assessable when it has all that, or says why in
// Note and keeps whatever it does have.
func (d planAssessDeps) prepareLeg(w *planLegWork, leg planLeg, env planEnv) {
	steps := []func() (planNoteKind, string){
		func() (planNoteKind, string) { return w.checkRouted(leg) },
		w.setZones,
		func() (planNoteKind, string) { return w.setOperatorDeparture(env.now) },
		func() (planNoteKind, string) { return w.setHours(leg, env) },
		func() (planNoteKind, string) { return w.checkHorizon(env.now) },
		func() (planNoteKind, string) { return w.setRoute(leg) },
		func() (planNoteKind, string) {
			if env.limitsProblem != "" {
				return planKindProblem, env.limitsProblem
			}
			return planKindNone, ""
		},
		func() (planNoteKind, string) { return d.setSuggestionWindow(w, env) },
	}
	for _, step := range steps {
		if kind, note := step(); note != "" {
			w.fail(kind, note)
			return
		}
	}
	w.assessable = true
}

// prepareUnderway works out what the wind changes on the leg the boat is on need,
// beyond what prepareLeg did: its route and hours when it was not assessable (its
// departure has passed), and its ETA from the operator's departure time. It
// touches nothing the verdicts or the current read. It returns why the ETA is not
// known, "" when it is.
func (w *planLegWork) prepareUnderway(leg planLeg, env planEnv) string {
	w.underway = true
	if !w.assessable {
		if _, note := w.checkRouted(leg); note != "" {
			return note
		}
		if _, note := w.setZones(); note != "" {
			return note
		}
		if env.speedErr != nil {
			return "No ETA: " + env.speedErr.Error()
		}
		w.hours = leg.DistanceNm / env.speed
		w.plan = planLegPassage(w.i, w.from, w.to, leg, w.hours)
		w.course = float64(leg.CourseDeg)
	}
	if w.from.DepartTime == "" {
		return "The leg is under way and has no departure time set, so its ETA isn't known."
	}
	h, m, ok := passageClock(w.from.DepartTime)
	if !ok {
		return "Departure time not readable: " + w.from.DepartTime
	}
	day, err := dayStart(w.from.Depart, w.fromLoc)
	if err != nil {
		return "The date is not readable: " + err.Error()
	}
	y, mo, d := day.Date()
	w.eta = time.Date(y, mo, d, h, m, 0, 0, w.fromLoc).Add(hoursDuration(w.hours))
	w.windReady = true
	return ""
}

func (w *planLegWork) checkRouted(leg planLeg) (planNoteKind, string) {
	if leg.From != w.i || leg.Error != "" || len(leg.Route) < 2 || leg.DistanceNm <= 0 {
		return planKindNotRouted, planNoteNotRouted
	}
	return planKindNone, ""
}

func (w *planLegWork) setZones() (planNoteKind, string) {
	for _, z := range []struct {
		s   planStop
		loc **time.Location
	}{{w.from, &w.fromLoc}, {w.to, &w.toLoc}} {
		zone, err := stayZoneAt(z.s.Lat, z.s.Lon)
		if err != nil {
			return planKindOther, "The place's time zone is not known: " + err.Error()
		}
		*z.loc = zone.Loc
	}
	return planKindNone, ""
}

// setOperatorDeparture refuses a day that has gone, and reads the time the
// operator set, which shows even when nothing can be judged and is never
// replaced.
func (w *planLegWork) setOperatorDeparture(now time.Time) (planNoteKind, string) {
	if w.from.Depart < now.In(w.fromLoc).Format(planDateLayout) {
		return planKindPast, planNotePast
	}
	day, err := dayStart(w.from.Depart, w.fromLoc)
	if err != nil {
		return planKindOther, "The date is not readable: " + err.Error()
	}
	if w.from.DepartTime != "" {
		h, m, ok := passageClock(w.from.DepartTime)
		if !ok {
			return planKindOther, "Departure time not readable: " + w.from.DepartTime
		}
		y, mo, dd := day.Date()
		w.depart = time.Date(y, mo, dd, h, m, 0, 0, w.fromLoc)
		w.res.Depart = fmtLeg(w.depart, w.fromLoc)
		if w.depart.Before(now) {
			return planKindPast, planNoteDepartPast
		}
	}
	return planKindNone, ""
}

// setHours works out the time under way at the boat's recorded speed, and the
// arrival at the operator's time.
func (w *planLegWork) setHours(leg planLeg, env planEnv) (planNoteKind, string) {
	if env.speedErr != nil {
		return planKindOther, "No ETA: " + env.speedErr.Error()
	}
	w.hours = leg.DistanceNm / env.speed
	w.res.Hours, w.res.SpeedKn = &w.hours, &env.speed
	// The arrival is known from the hours and the speed, so it shows even for a
	// leg that is not judged.
	if !w.depart.IsZero() {
		w.res.Arrive = fmtLeg(w.depart.Add(hoursDuration(w.hours)), w.toLoc)
	}
	if w.hours > planAssessMaxLegHours {
		return planKindOvernight, planNoteOvernight
	}
	return planKindNone, ""
}

// checkHorizon leaves a day the forecast does not reach yet without a verdict.
func (w *planLegWork) checkHorizon(now time.Time) (planNoteKind, string) {
	day, _ := dayStart(w.from.Depart, w.fromLoc)
	horizon := day.AddDate(0, 0, -assistantMaxForecastDays).Format(planDateLayout)
	if horizon > now.In(w.fromLoc).Format(planDateLayout) {
		return planKindNoForecast, planNoteNoForecast
	}
	return planKindNone, ""
}

// setRoute converts the leg's route once, for the ranking and the current and for
// the wind check. The wind check keeps no more points than
// passageWindowMaxRoutePoints (planPassageLeg).
func (w *planLegWork) setRoute(leg planLeg) (planNoteKind, string) {
	latLon := make([]assistantLatLon, len(leg.Route))
	for i, pt := range leg.Route {
		latLon[i] = assistantLatLon{Lat: pt[0], Lon: pt[1]}
	}
	w.query = planDepartureQuery{Route: latLon, Hours: w.hours}
	w.plan = planLegPassage(w.i, w.from, w.to, leg, w.hours)
	w.course = float64(leg.CourseDeg)
	return planKindNone, ""
}

// planLegPassage is a routed leg as the passage assessor reads it.
func planLegPassage(i int, from, to planStop, leg planLeg, hours float64) passageLegPlan {
	points := make([]passageRoutePoint, len(leg.Route))
	for j, pt := range leg.Route {
		points[j] = passageRoutePoint{Lat: pt[0], Lon: pt[1]}
	}
	points[0].Name, points[len(points)-1].Name = from.Name, to.Name
	return planPassageLeg(i, passageLegArgs{Name: from.Name + " → " + to.Name, Route: points, PassageHours: &hours})
}

// setSuggestionWindow is the span a departure may be suggested in, for a leg the
// operator set no time for: from first light at the leg's start to the latest
// departure that still arrives in daylight when the operator wants that, else the
// last one that leaves in it. The arrival rule is the assessor's own
// (legEndSun). None fits: no daylight window.
func (d planAssessDeps) setSuggestionWindow(w *planLegWork, env planEnv) (planNoteKind, string) {
	if !w.depart.IsZero() {
		return planKindNone, ""
	}
	earliest, latest, state := d.departureWindow(w, env)
	switch state {
	case windowNone:
		return planKindNoWindow, planNoteNoWindow
	case windowPassed:
		return planKindWindowPassed, planNoteWindowPassed
	}
	w.query.Earliest, w.query.Latest = earliest, latest
	return planKindNone, ""
}

// windowState says whether a leg's day has a departure window: it fits (the
// zero value is not used), the day never had one, or it had one and now is past it.
type windowState int

const (
	windowFits windowState = iota + 1
	windowNone
	windowPassed
)

func (d planAssessDeps) departureWindow(w *planLegWork, env planEnv) (earliest, latest time.Time, state windowState) {
	state = windowNone
	fromDay, err := dayStart(w.from.Depart, w.fromLoc)
	if err != nil {
		return
	}
	dawn, okDawn := d.dawn(fromDay, w.from.Lat, w.from.Lon)
	if !okDawn {
		return
	}
	if env.limits.DaylightArrival {
		toDay, err := dayStart(w.from.Depart, w.toLoc)
		if err != nil {
			return
		}
		_, set, okSet := legEndSun(d.sun, w.plan, toDay, w.toLoc)
		if !okSet {
			return
		}
		latest = set.Add(-hoursDuration(w.hours))
	} else {
		_, set, okSet := d.sun(fromDay, w.from.Lat, w.from.Lon)
		if !okSet {
			return
		}
		latest = set
	}
	earliest = ceilToStep(dawn, planDepartureStep)
	latest = latest.Truncate(planDepartureStep)
	if latest.Before(earliest) {
		return // the day never had a window
	}
	if soonest := ceilToStep(env.now, planDepartureStep); soonest.After(earliest) {
		earliest = soonest
	}
	if latest.Before(earliest) {
		return earliest, latest, windowPassed
	}
	return earliest, latest, windowFits
}

// readAndJudge reads what the verdicts need from the providers, all at once: the
// forecast along every assessable leg, each leg's ranking or current (three legs
// at a time) and the stops' warnings. Then it judges each leg.
func (d planAssessDeps) readAndJudge(ctx context.Context, p plan, work []*planLegWork, env planEnv, out *planAssessment) {
	var assessable []*planLegWork
	for _, w := range work {
		if w.assessable {
			assessable = append(assessable, w)
		}
	}
	// The wind changes are read along the legs judged and the one the boat is on.
	windLegs := slices.Clone(assessable)
	for _, w := range work {
		if w.windReady && !w.assessable && env.limitsProblem == "" {
			windLegs = append(windLegs, w)
		}
	}
	// The lay-day nights, and the night of arrival at each stop a leg lands at,
	// are read at their stops from the same source, once per stop. With no limits
	// to judge them by nothing is read for them.
	var nights []planNightWork
	var stopPoints []passageRoutePoint
	if !env.verdictsOnly {
		nights = layDayNights(p, env.now)
		if env.limitsProblem == "" {
			var landings []planStop
			for _, w := range windLegs {
				if holdsArrivalNight(p, w.i+1) {
					landings = append(landings, w.to)
				}
			}
			stopPoints = nightPoints(nights, landings)
		}
	}
	var (
		wg          sync.WaitGroup
		as          *passageAssessor
		forecastErr error
		stops       stopForecasts
	)
	if len(windLegs) > 0 || len(stopPoints) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			as, forecastErr, stops = d.fetchForecast(ctx, p, windLegs, stopPoints, env)
		}()
	}
	if !env.verdictsOnly {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.readWarnings(p, env.now, out)
		}()
	}
	sem := make(chan struct{}, planLegWorkers)
	for _, w := range work {
		// A leg with a departure set is read for its current alone, which a
		// verdicts-only assessment leaves out; one without is read for the ranking
		// its verdict is chosen from.
		if w.query.Route == nil || (w.depart.IsZero() && !w.assessable) || (!w.depart.IsZero() && env.verdictsOnly) {
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(w *planLegWork) {
			defer wg.Done()
			defer func() { <-sem }()
			d.readLeg(ctx, w)
		}(w)
	}
	wg.Wait()

	d.judgeLegs(ctx, as, forecastErr, assessable, out)
	if env.verdictsOnly {
		return
	}
	// The wind changes come after the verdicts, so a leg's departure is the one
	// chosen for it and its ETA follows.
	var arrivals []planArrival
	for _, w := range windLegs {
		if w.changesRead {
			continue // the leg under way has already said why its ETA is not known
		}
		w.changesRead = true
		switch {
		case forecastErr != nil:
			w.res.WindChanges = planWindChanges{Changes: []planWindChange{}, NotChecked: "The forecast could not be read: " + forecastErr.Error()}
		default:
			w.res.WindChanges = legWindChanges(as, w, env)
		}
		if eta, ok := w.arrival(); ok && holdsArrivalNight(p, w.i+1) {
			arrivals = append(arrivals, planArrival{stop: w.i + 1, eta: eta})
		}
	}
	nights = append(nights, arrivalNights(p, arrivals, env.now)...)
	out.NightWinds = nightWinds(as, stops, nights, env)
}

// arrival is the instant the leg lands at its stop, when it has one: the ETA of
// the leg under way, else the arrival the verdict was made at. A time that cannot
// be read is already said on the leg's own row.
func (w *planLegWork) arrival() (time.Time, bool) {
	if w.underway {
		return w.eta, w.windReady
	}
	if w.res.Arrive == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, w.res.Arrive)
	return t, err == nil
}

// judgeLegs judges every leg whose forecast was fetched, or fails them all with
// the reason the forecast was not.
func (d planAssessDeps) judgeLegs(ctx context.Context, as *passageAssessor, forecastErr error, assessable []*planLegWork, out *planAssessment) {
	if len(assessable) == 0 {
		return
	}
	if forecastErr != nil {
		log.Printf("plans: forecast for the verdicts failed: %v", forecastErr)
		note := "Forecast failed: " + forecastErr.Error()
		if out.Problem == "" {
			out.Problem = note
		}
		for _, w := range assessable {
			if w.assessable {
				w.fail(planKindOther, note)
			}
		}
		return
	}
	if !as.fetchedAt.IsZero() {
		out.ForecastAsOf = as.fetchedAt.UTC().Format(time.RFC3339)
	}
	for _, w := range assessable {
		if w.assessable {
			d.judgeLeg(ctx, as, w)
		}
	}
}

// fetchForecast fetches the forecast along every assessable leg once, and at the
// stops whose nights are read. A stop that is a leg's end is already in the
// assessor and is not asked for again; the rest go through the same source, whose
// memo keeps one read per place. legsErr is why the legs' forecast is missing;
// stopFetch says how the stops' went, stop by stop. The two fail apart: a stop
// that cannot be read leaves the legs' verdicts, and the other stops, alone, and
// its fetch time stays out of the forecast time the verdicts are stamped with.
func (d planAssessDeps) fetchForecast(ctx context.Context, p plan, legs []*planLegWork, stops []passageRoutePoint, env planEnv) (as *passageAssessor, legsErr error, stopFetch stopForecasts) {
	firstZone := p.Stops[0].Timezone
	src, err := d.sources(func() (string, error) { return firstZone, nil }, env.now)
	if err != nil {
		return nil, err, stopForecasts{err: err}
	}
	as = newPassageAssessor(env.limits, src.fc, src.tides, d.sun)
	as.skipWindAgainstTide = true
	if len(legs) > 0 {
		plans := make([]passageLegPlan, len(legs))
		for i, w := range legs {
			plans[i] = w.plan
		}
		legsErr = as.prefetchLegs(ctx, plans)
	}
	var missing []passageRoutePoint
	for _, pt := range stops {
		if _, have := as.cache[passagePointKey(pt)]; !have {
			missing = append(missing, pt)
		}
	}
	if len(missing) > 0 {
		failed, err := as.prefetchStops(ctx, missing)
		stopFetch = stopForecasts{err: err, failed: failed}
	}
	return as, legsErr, stopFetch
}

// readLeg reads one leg's current: at the operator's departure, or, with none
// set, along every departure the ranking tries, to be chosen among once the
// forecast is in.
func (d planAssessDeps) readLeg(ctx context.Context, w *planLegWork) {
	if !w.depart.IsZero() {
		reading, err := d.currentAlong(ctx, w.query, w.depart)
		w.setCurrent(reading, err)
		return
	}
	cands, err := d.departures(ctx, w.query)
	if err == nil && len(cands) == 0 {
		err = errNoDepartureRanked
	}
	if err != nil {
		log.Printf("plans: no departure ranked for %s to %s: %v", w.from.Name, w.to.Name, err)
		w.fail(planKindNoDeparture, "No departure suggested: "+err.Error())
		return
	}
	w.cands = cands
}

// judgeLeg judges a leg at the operator's departure, or at the best departure the
// ranking found, and writes the verdict and what is beside it.
func (d planAssessDeps) judgeLeg(ctx context.Context, as *passageAssessor, w *planLegWork) {
	var cell passageWindowCell
	var err error
	var chosen tidalDeparture
	if w.depart.IsZero() {
		chosen, cell, err = chooseDeparture(ctx, as, w)
	} else {
		cell, err = as.cell(ctx, w.i, w.plan, w.depart, w.toLoc)
	}
	if err != nil {
		log.Printf("plans: leg %s to %s could not be judged: %v", w.from.Name, w.to.Name, err)
		w.fail(planKindOther, err.Error())
		return
	}
	beyond := cell.State == "no_data" && cell.noData == passageNoDataBeyondForecast
	// A suggested departure and its current are shown only for a leg that has a
	// verdict or a gap to explain; one with no forecast yet shows neither.
	if w.depart.IsZero() && !beyond {
		w.setDeparture(chosen.Depart, true)
		w.setCurrent(chosen.Current, nil)
	}
	if cell.Limiting != nil {
		w.res.Limiting = cell.Limiting.Short
	}
	w.res.NotChecked = cell.NotChecked
	switch {
	case cell.State != "no_data":
		w.res.Verdict = cell.State
	case beyond:
		// The forecast stamps every hour; a leg that runs past the last one has no
		// forecast yet.
		w.fail(planKindNoForecast, planNoteNoForecast)
	default:
		w.res.Verdict = "no_data"
		w.res.Note, w.res.NoteKind = planNoteGap, planKindGap
	}
}

// departureOrder ranks a cell for choosing a departure: Go, then Marginal, then a
// gap inside the forecast, then a judged No-Go, then a run that goes past the
// forecast's end. A No-Go the forecast shows is never hidden behind a time it
// cannot yet judge.
func departureOrder(cell passageWindowCell) int {
	switch cell.State {
	case "go":
		return 0
	case "marginal":
		return 1
	case "no_data":
		if cell.noData == passageNoDataBeyondForecast {
			return 4
		}
		return 2
	}
	return 3
}

// chooseDeparture judges the ranked departures, fairest current first, and takes
// the best: one that is not a No-Go, Go before Marginal, the fairest current among
// equals. When every one is a No-Go it takes the one closest to its limit, and its
// verdict says No-Go.
func chooseDeparture(ctx context.Context, as *passageAssessor, w *planLegWork) (tidalDeparture, passageWindowCell, error) {
	var best tidalDeparture
	var bestCell passageWindowCell
	found := false
	for _, c := range w.cands {
		cell, err := as.cell(ctx, w.i, w.plan, c.Depart, w.toLoc)
		if err != nil {
			return tidalDeparture{}, passageWindowCell{}, err
		}
		better := !found || departureOrder(cell) < departureOrder(bestCell) ||
			(cell.State == "no_go" && bestCell.State == "no_go" && cell.ratio < bestCell.ratio)
		if better {
			best, bestCell, found = c, cell, true
		}
		if cell.State == "go" {
			// The candidates are in order of current, so the first Go is the best.
			break
		}
	}
	return best, bestCell, nil
}

// readWarnings finds the official warnings in force on the nights the boat is at
// a stop. A section names a weekday, so only nights within the coming week can
// be matched. The provider is resolved once, and a place is read once however
// many stops share it. A read that fails is said once; the stops are then left
// empty, not read as having none.
func (d planAssessDeps) readWarnings(p plan, now time.Time, out *planAssessment) {
	type stopNights struct {
		stop   int
		place  planStop
		nights []time.Time
		today  string
	}
	var wanted []stopNights
	for i, s := range p.Stops {
		if s.Nights <= 0 {
			continue
		}
		zone, err := stayZoneAt(s.Lat, s.Lon)
		if err != nil {
			out.WarningsError = "The place's time zone is not known: " + err.Error()
			return
		}
		arrive, err := dayStart(s.Arrive, zone.Loc)
		if err != nil {
			out.WarningsError = "The arrival date at " + s.Name + " could not be read: " + strconv.Quote(s.Arrive)
			return
		}
		today := now.In(zone.Loc).Format(planDateLayout)
		entry := stopNights{stop: i, place: s, today: today}
		for n := 0; n < s.Nights; n++ {
			night := arrive.AddDate(0, 0, n)
			day := night.Format(planDateLayout)
			if day < today || day >= dayPlus(today, planWarningDays) {
				continue
			}
			entry.nights = append(entry.nights, night)
		}
		if len(entry.nights) > 0 {
			wanted = append(wanted, entry)
		}
	}
	if len(wanted) == 0 {
		return
	}
	read, err := d.warnings(now)
	if err != nil {
		out.WarningsError = err.Error()
		return
	}
	bundles := map[string]forecastWarningsBundle{}
	for _, w := range wanted {
		key := passagePointKey(passageRoutePoint{Lat: w.place.Lat, Lon: w.place.Lon})
		bundle, seen := bundles[key]
		if !seen {
			if bundle, err = read(w.place.Lat, w.place.Lon); err != nil {
				out.WarningsError = err.Error()
				return
			}
			bundles[key] = bundle
		}
		entry := planStopAssessment{Stop: w.stop, Nights: []planStopNight{}}
		for _, night := range w.nights {
			if warning, url := warningOn(bundle, night, w.today); warning != "" {
				entry.Nights = append(entry.Nights, planStopNight{Date: night.Format(planDateLayout), Warning: warning, URL: url})
			}
		}
		if len(entry.Nights) > 0 {
			out.Stops = append(out.Stops, entry)
		}
	}
}

func dayPlus(day string, n int) string {
	t, err := time.Parse(planDateLayout, day)
	if err != nil {
		return day
	}
	return t.AddDate(0, 0, n).Format(planDateLayout)
}

// sectionDayIsNight is true when a warnings section's day names the night: a bare
// weekday ("Sunday"), or the weekday with its date as the BOM writes it ("Sunday 5
// July"), in which case the date must be the night's too, so the same weekday a
// week on is not matched.
func sectionDayIsNight(day string, night time.Time) bool {
	f := strings.Fields(day)
	if len(f) == 0 || !strings.EqualFold(f[0], night.Weekday().String()) {
		return false
	}
	if len(f) == 1 {
		return true
	}
	if len(f) != 3 {
		return false
	}
	dom, err := strconv.Atoi(f[1])
	month := night.Month().String()
	return err == nil && dom == night.Day() &&
		(strings.EqualFold(f[2], month) || strings.EqualFold(f[2], month[:3]))
}

// warningOn is the warning types in force on a night, joined, and the first
// bulletin's link. A section names its day as a weekday (with its date when the
// provider gives one), or today.
func warningOn(b forecastWarningsBundle, night time.Time, today string) (warning, url string) {
	var kinds []string
	seen := map[string]bool{}
	for _, bulletin := range b.Bulletins {
		for _, sec := range bulletin.Sections {
			day := strings.TrimSpace(sec.Day)
			match := sectionDayIsNight(day, night) ||
				(night.Format(planDateLayout) == today && (strings.EqualFold(day, "today") || strings.EqualFold(day, "tonight")))
			if !match || seen[sec.WarningType] {
				continue
			}
			seen[sec.WarningType] = true
			kinds = append(kinds, sec.WarningType)
			if url == "" {
				url = bulletin.DetailsURL
			}
		}
	}
	return strings.Join(kinds, "; "), url
}

// ── the endpoint ────────────────────────────────────────────────────────

func planSettingsPath() string { return getEnv("SETTINGS_FILE", "../settings.yaml") }

// planAssessDepsFor wires an assessment's readers from the tool deps whose clock
// and providers the caller runs on; a test replaces it.
var planAssessDepsFor = func(d assistantToolDeps) planAssessDeps {
	return planAssessDepsFromTool(d, planSettingsPath())
}

// pageAssessDeps are the readers the Forecast page's endpoints run on: the
// production tool deps, on the wall clock.
func pageAssessDeps() planAssessDeps {
	return planAssessDepsFor(assistantProductionToolDeps(planSettingsPath()))
}

// planAssessmentHandler is GET /api/plans/:id/assessment: each leg's verdict,
// departure, ETA and current, and the warnings on the stops' nights.
func planAssessmentHandler(c echo.Context) error {
	deps := pageAssessDeps().pinned()
	d, ok, err := loadDecoratedPlan(c, deps.now())
	if !ok {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), planAssessmentDeadline)
	defer cancel()
	legs := planLegs(ctx, d)
	return c.JSON(http.StatusOK, assessPlan(ctx, d, legs, deps, planAssessOptions{}))
}
