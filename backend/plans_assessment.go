package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"slices"
	"sort"
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
// Go before Marginal, then the fairest current (departureCandidates). When the
// plan page asks for comfort, the departures of the best verdict are then ranked
// by how the leg is forecast to feel, the current breaking ties (ADR 0303). No
// verdict asks a model: a figure that cannot be had is left out with its reason,
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
	// The reasons a run has no ETA, which noEtaNote puts after "No ETA: " or "No
	// departure suggested: ". The current is in the speed, so there is no ETA without
	// it; the detail of a failed read, or of a run that could not be made, is logged.
	planReasonNoCurrent  = "the modelled current could not be read"
	planReasonNoTimeline = "the leg's timeline could not be worked out"
	planNoEtaNoCurrent   = "No ETA: " + planReasonNoCurrent
	// planReasonNoProvider is no current model to ask: the wave provider could not be
	// resolved.
	planReasonNoProvider = "no modelled current is set up"
	// planNoEtaUnderway is the leg under way whose run failed for a reason other than
	// the current.
	planNoEtaUnderway  = "No ETA: the ETA from the latest position could not be worked out"
	planNoteNoTimeline = "No departure suggested: " + planReasonNoTimeline
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
	// SpeedKn is the average speed the hours were worked out at (the leg's distance
	// over Hours), so Hours times SpeedKn is the distance. For the leg under way the
	// hours are those of its shown timeline, the start of the passage to the ETA, so
	// it is an average beside the cruise speed, not the pace of the rest. EtaBasis says in a
	// sentence what the hours were worked out from: the learned cruise speed, and any
	// minutes in slow water. Both always on the wire, null when the leg has no ETA.
	SpeedKn  *float64 `json:"speed_kn"`
	EtaBasis *string  `json:"eta_basis"`
	// CurrentKn is the modelled current along the course at the departure, in
	// knots: positive helps the boat, negative hinders it. Nil when it could not
	// be read, and CurrentNote says why.
	CurrentKn   *float64 `json:"current_kn"`
	CurrentNote string   `json:"current_note"`
	NotChecked  []string `json:"not_checked"`
	// WindChanges are the changes in the wind between the departure and the ETA,
	// worked out here so the page only draws them (ADR 0287).
	WindChanges planWindChanges `json:"wind_changes"`
	// Comfort is how the leg is forecast to feel, hour by hour, at the departure
	// chosen for it (ADR 0302). Null when it was not looked for: a verdicts-only
	// assessment, a leg with no forecast, a caller that is not the plan page.
	Comfort *planLegComfort `json:"comfort"`
	// DepartBasis says in a sentence why the suggested departure is the one chosen:
	// ranked for comfort, or why comfort could not rank it. Null unless the
	// departure is suggested, comfort was asked for and the leg had a tier of
	// departures to rank (ADR 0303).
	DepartBasis *string `json:"depart_basis"`
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
	// Comfort asks for each judged leg's comfort at its chosen departure (ADR 0302).
	// Only the plan page's own endpoint sets it: a verdict, a change mark, a Mate
	// answer or a slip never reads it.
	Comfort bool
}

// planWarningsReader reads the official warnings at a place.
type planWarningsReader func(lat, lon float64) (forecastWarningsBundle, error)

// planAssessDeps are the readers an assessment needs, so a test supplies fakes
// and production wires the configured providers (plans_assessment_wire.go).
type planAssessDeps struct {
	now     func() time.Time
	limits  func() (passageLimits, error)
	sources func(zone func() (string, error), now time.Time) (passageSources, error)
	// speed is the boat's cruise speed and slow water, learned from the passages it
	// recorded, or why it has none.
	speed func(ctx context.Context) (*legPaceModel, error)
	sun   func(date time.Time, lat, lon float64) (rise, set time.Time, ok bool)
	dawn  func(date time.Time, lat, lon float64) (time.Time, bool)
	// legCurrents reads the modelled current along a leg, once for the leg.
	legCurrents func(ctx context.Context, p *legPaceProfile) (legCurrentField, error)
	// run, when set, works out a leg's run from a departure in place of the
	// profile's own, so a test can give a departure a pace of its own.
	run func(p *legPaceProfile, depart time.Time, f legCurrentField) (legRun, error)
	// warnings resolves the warnings provider once for an assessment made at now.
	warnings func(now time.Time) (planWarningsReader, error)
	// progress is each leg's progress from the recorded passages ("" to do).
	progress func(ctx context.Context, p plan) ([]string, error)
	// underway is where the passage being recorded now began and its latest fix,
	// nil when none is under way. Nil reads none.
	underway func(ctx context.Context, now time.Time) (*planUnderway, error)
	// position is the boat's own trusted fix, or why there is none.
	position func() (lat, lon float64, err error)
	// suggested keeps the first departure suggested for a leg on a day and returns
	// the one kept (documentStore.FirstSuggestion). Nil keeps nothing.
	suggested func(planID int64, legKey, day string, t time.Time) (time.Time, error)
	// keptSuggestion reads the first suggestion kept for a leg on a day, without
	// writing one (documentStore.KeptSuggestion). Nil reads none.
	keptSuggestion func(planID int64, legKey, day string, loc *time.Location) (time.Time, bool, error)
	// comfort reads what a leg's comfort needs. Nil is comfort off, for a caller
	// that never asks for it. When a run asks for comfort, every reader must be
	// set: one that is not is reported on each leg, never left as no comfort.
	comfort *planComfortDeps
}

// planEnv is what every leg of an assessment shares.
type planEnv struct {
	now          time.Time
	verdictsOnly bool
	// comfort says each judged leg's comfort is asked for (ADR 0302).
	comfort       bool
	limits        passageLimits
	limitsErr     error
	limitsProblem string
	// problem is the plan's one problem from the limits or the speed, "" when none.
	problem string
	// pace is what was learned of the boat's cruise speed and slow water, or
	// speedErr is why nothing was.
	pace     *legPaceModel
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
	// profile is the leg walked once for the learned pace, nil until setPace has
	// run: the leg's pace for any departure, and the words on the wire, are read from
	// it. tl is the timeline of the departure the row shows (the operator's, the one
	// chosen), which the wire's hours are read from: only a run gives one, so a row
	// with no departure or no ETA has none. shown is the run it came from, whose
	// current and slow water the row's eta basis carries.
	profile *legPaceProfile
	tl      legTimeline
	shown   legRun
	// grid is the departures a suggestion is looked for among, the quarter hours from
	// first light to the latest that could still be in the window, and arriveBy is
	// the arrival the daylight limit asks for, zero when there is none. Which of them
	// are in the window is a matter of each one's own arrival (departureCandidates).
	grid     []time.Time
	arriveBy time.Time
	plan     passageLegPlan
	// fix is where the leg under way has the boat, nil for any other leg and for one
	// whose fix is not known; readLeg runs the ETA from it.
	fix *underwayFix
	// underTl is the progress of the leg under way, from the start of the passage
	// through the latest fix to the arrival.
	underTl legTimeline
	// course is the leg's course in degrees true, which a wind change is placed
	// against.
	course float64
	// changesRead is true once the leg's wind changes are worked out.
	changesRead bool
	// underway is true for the leg the boat is on. Its wind changes are read from
	// now to eta, the arrival of the leg's pace for the operator's departure, and
	// windReady says it has what that needs.
	underway  bool
	windReady bool
	// etaWhy is why a leg under way has no readable ETA, "" when it has one.
	etaWhy string
	// noPace says the leg's still-water pace failed (setHours): its note is that No ETA
	// and the current is not read for it.
	noPace bool
	// windowless says a suggested leg has everything but a daylight window: it is still
	// read for its current, so a failed read is the reason given.
	windowless bool
	eta        time.Time
	// chosenDepart and chosenArrive are the departure and arrival the leg's row
	// shows, as instants: zero until set. They are set with res.Depart and
	// res.Arrive, never apart.
	chosenDepart, chosenArrive time.Time
	// cands are the departures to choose among when the operator set none,
	// fairest current first.
	cands      []tidalDeparture
	assessable bool
	// ranking is how the request chooses a suggested departure when comfort is asked
	// for, nil when it is not. choice holds the candidates of a leg whose departure
	// comfort has still to rank.
	ranking *departureRanking
	choice  *departureChoice
}

// underwayFix is the boat's latest fix placed on the leg under way: when the passage
// began and the fix was taken, and the step of the leg the fix is on, with the share
// of that step still ahead of it.
type underwayFix struct {
	start, at time.Time
	step      int
	left      float64
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

// setPace gives a routed leg its pace: the boat's cruise speed along the route,
// slower through the cells it has learned are slow water. The route is walked once
// for the leg, the first time any step asks (setHours, or prepareUnderway for a leg
// whose hours were not worked out), and each departure's pace is worked out over
// that walk.
func (w *planLegWork) setPace(leg planLeg, m *legPaceModel) error {
	if w.profile == nil {
		p, err := m.profile(leg)
		if err != nil {
			return err
		}
		w.profile = p
	}
	return nil
}

// setDeparture shows the departure the ranking chose, on the timeline it was ranked on.
func (w *planLegWork) setDeparture(c tidalDeparture, suggested bool) {
	w.putDepart(c.Depart)
	w.res.DepartSuggested = suggested
	w.show(c.run)
}

// show makes r the run the row shows: its timeline and arrival, and the words of its
// eta basis.
func (w *planLegWork) show(r legRun) {
	w.tl, w.shown = r.tl, r
	w.putArrive(r.tl.arrive())
}

// putEtaBasis words what the hours shown were worked out on, as depart_basis words
// what the departure was chosen on. It is put once, when the row is finished, on a
// leg that shows an arrival still ahead. The words are those of the run the row
// shows: the current it met, and the slow water over the part of the leg it covers.
func (w *planLegWork) putEtaBasis() {
	if w.profile == nil {
		return // a leg with no pace has no hours to give a basis to
	}
	text := etaBasisText(w.profile.cruiseKn, hoursDuration(w.shown.slowHours), w.shown.currentWords())
	w.res.EtaBasis = &text
}

// etaBasisText is the sentence a leg's hours were worked out on: the cruise speed,
// what its run says of the current (legRun.currentWords, "" for none), and the time
// in slow water rounded to five minutes, left out when it rounds to none.
func etaBasisText(cruiseKn float64, slow time.Duration, current string) string {
	parts := []string{fmt.Sprintf("Cruise %.1f kn", cruiseKn)}
	if current != "" {
		parts = append(parts, current)
	}
	if m := int(math.Round(slow.Minutes()/5)) * 5; m > 0 {
		parts = append(parts, durationText(time.Duration(m)*time.Minute)+" in slow water")
	}
	return strings.Join(parts, ", ") + "."
}

// putDepart and putArrive set the departure and arrival the row shows, in their
// stop's clock, and keep them as the instants the row's text was made from.
func (w *planLegWork) putDepart(t time.Time) {
	w.chosenDepart = t.Truncate(time.Second)
	w.res.Depart = fmtLeg(t, w.fromLoc)
}

func (w *planLegWork) putArrive(t time.Time) {
	w.chosenArrive = t.Truncate(time.Second)
	w.res.Arrive = fmtLeg(t, w.toLoc)
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
	env.comfort = opts.Comfort
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
			if why := work[i].prepareUnderway(byFrom[i], env, underway); why != "" {
				work[i].noEta(why, env)
			}
		}
	}
	d.readAndJudge(ctx, p, work, env, &out)
	if !opts.VerdictsOnly {
		d.markStillInPort(p, work, progress, now, &out)
	}

	// A leg whose note is the plan's one problem says "not checked" and leaves the
	// words to the line above the table. Its hours are those of the timeline it
	// ended on.
	for i := range out.Legs {
		l := &out.Legs[i]
		if out.Problem != "" && l.Note == out.Problem {
			l.NoteKind = planKindProblem
		}
		if tl := work[i].tl; tl.known() {
			hours := tl.hours()
			l.Hours = &hours
			// The basis words an ETA the row shows, so only a leg with an arrival on
			// its row has one, and a leg under way only while its ETA is still ahead.
			if l.Arrive != "" && (!work[i].underway || work[i].eta.After(env.now)) {
				work[i].putEtaBasis()
			}
			// The average speed the hours were worked out at, so hours times speed_kn
			// is the leg's distance. The cruise speed is in eta_basis.
			if d := byFrom[i].DistanceNm; d > 0 && hours > 0 {
				speed := d / hours
				l.SpeedKn = &speed
			}
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
		env.pace, env.speedErr = d.speed(ctx)
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
	for i, step := range steps {
		if kind, note := step(); note != "" {
			w.fail(kind, note)
			// A suggested leg that has all else but a window is still read for its
			// current, which comes first: a failed read says why there is no departure.
			w.windowless = i == len(steps)-1 && kind == planKindNoWindow
			return
		}
	}
	w.assessable = true
}

// prepareUnderway makes the leg the boat is on a leg under way: it is given no
// suggested departure, ranking or new verdict, since its passage has begun. Its row
// shows when the passage began and, once readLeg has run the rest of the route from
// the boat's latest fix, the ETA that gives. Beyond what prepareLeg did it works out
// the route and hours when the leg was not assessable (its departure has passed), and
// places the latest fix on the route. It returns why the ETA is not known, "" when the
// fix is placed.
func (w *planLegWork) prepareUnderway(leg planLeg, env planEnv, under *planUnderway) string {
	w.underway = true
	prepared := w.assessable
	w.assessable = false
	if !prepared {
		if _, note := w.checkRouted(leg); note != "" {
			return note
		}
		if _, note := w.setZones(); note != "" {
			return note
		}
		if env.speedErr != nil {
			return "No ETA: " + env.speedErr.Error()
		}
		if err := w.setPace(leg, env.pace); err != nil {
			return "No ETA: " + err.Error()
		}
		w.plan = planLegPassage(w.i, w.from, w.to, leg, w.profile.pace)
		w.course = float64(leg.CourseDeg)
	}
	// The row shows the boat's own departure, when the passage began, and no ETA
	// until one is worked out from the fix.
	w.res.DepartSuggested = false
	if under != nil && !under.Start.IsZero() {
		w.putDepart(under.Start)
	}
	if under == nil || under.FixAt.IsZero() {
		return "No ETA: the boat's latest position fix is not known"
	}
	if age := env.now.Sub(under.FixAt); age > legFixMaxAge {
		return "No ETA: the latest position is " + durationText(age) + " old"
	}
	at := w.profile.place(under.FixLat, under.FixLon, under.FixAt.Sub(under.Start))
	if at.offNm > legFixOffRouteNm {
		return fmt.Sprintf("No ETA: the boat is %.1f nm off the planned route", at.offNm)
	}
	w.fix = &underwayFix{start: under.Start, at: under.FixAt, step: at.step, left: at.left}
	return ""
}

// noEta says why a leg has no readable ETA. A leg under way keeps the reason and its
// wind changes are not read for it. For any other leg the operator's departure stays
// on the row with no arrival, hours or timeline, and the note says why.
func (w *planLegWork) noEta(why string, env planEnv) {
	if w.underway {
		w.etaWhy = why
		if env.limitsProblem == "" {
			w.res.WindChanges.NotChecked, w.changesRead = why, true
		}
		return
	}
	w.noDeparture(planKindOther, why, env)
}

// takeEta is where a leg with a departure (the operator's, or the leg under way) gets
// its ETA from its run, or none. A run with an ETA is shown, with its current, whether
// or not the leg can be judged: an overnight run has its timeline too. Any other run
// has no ETA, and the leg says why in place of whatever note it had, since "No ETA"
// is the more specific. It says whether the leg has an ETA.
func (w *planLegWork) takeEta(r departureRun, f legCurrentField, env planEnv) bool {
	fate := r.outcome()
	if fate == fateOK || fate == fateOvernight {
		w.show(r.run)
		w.setCurrent(runReading(r.run, f), nil)
		return true
	}
	if fate == fateNotRun {
		log.Printf("plans: ETA for %s to %s not worked out: %v", w.from.Name, w.to.Name, r.err)
	}
	_, note := noEtaNote(fate, false, r.err)
	if fate == fateNotRun && w.underway {
		// A run from the fix that failed for a reason other than the current.
		note = planNoEtaUnderway
	}
	w.noEta(note, env)
	return false
}

// readUnderway runs the leg under way from its fix, at the time of the fix, with the
// current the leg read, nil when it read none (readErr says why). The row's current
// is the run's figure over the open water still ahead.
func (w *planLegWork) readUnderway(env planEnv, f legCurrentField, readErr error) {
	r := departureRun{run: legRun{fate: fateUnreadable}, err: readErr}
	if readErr == nil {
		r.run, r.err = w.profile.runFrom(w.fix.start, w.fix.at, w.fix.step, w.fix.left, f)
	}
	if !w.takeEta(r, f, env) {
		return
	}
	// The row's times are the passage's: it began at the start and arrives at the ETA
	// from the fix, so the hours are the ones between them.
	w.underTl, w.eta, w.windReady = r.run.tl, r.run.tl.arrive(), true
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
		w.putDepart(w.depart)
		if w.depart.Before(now) {
			return planKindPast, planNoteDepartPast
		}
	}
	return planKindNone, ""
}

// setHours gives the leg its pace and checks, in still water, that it is not an
// overnight leg. The still-water timeline is only that check: no row shows it, and the
// leg's ETA is a run with the current (readLeg).
func (w *planLegWork) setHours(leg planLeg, env planEnv) (planNoteKind, string) {
	if env.speedErr != nil {
		w.noPace = true
		return planKindOther, "No ETA: " + env.speedErr.Error()
	}
	ref := w.depart
	if ref.IsZero() {
		ref = env.now
	}
	if err := w.setPace(leg, env.pace); err != nil {
		w.noPace = true
		return planKindOther, "No ETA: " + err.Error()
	}
	tl, err := w.profile.pace(ref)
	if err != nil {
		w.noPace = true
		return planKindOther, "No ETA: " + err.Error()
	}
	if tl.hours() > planAssessMaxLegHours {
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

// setRoute lays the leg's route out for the wind check, which keeps no more points
// than passageWindowMaxRoutePoints (planPassageLeg), and marks the leg routed.
func (w *planLegWork) setRoute(leg planLeg) (planNoteKind, string) {
	w.plan = planLegPassage(w.i, w.from, w.to, leg, w.profile.pace)
	w.course = float64(leg.CourseDeg)
	return planKindNone, ""
}

// planLegPassage is a routed leg as the passage assessor reads it, at the pace given.
func planLegPassage(i int, from, to planStop, leg planLeg, pace legPace) passageLegPlan {
	points := make([]passageRoutePoint, len(leg.Route))
	for j, pt := range leg.Route {
		points[j] = passageRoutePoint{Lat: pt[0], Lon: pt[1]}
	}
	points[0].Name, points[len(points)-1].Name = from.Name, to.Name
	p := layOutPassageLeg(i, passageLegArgs{Name: from.Name + " → " + to.Name, Route: points})
	p.pace = pace
	return p
}

// setSuggestionWindow lays out the departures a suggestion is looked for among, for a
// leg the operator set no time for: the quarter hours from first light at the leg's
// start to an outer bound, sunset at the leg's start, or with the daylight-arrival
// limit sunset at its stop, since a departure after that cannot arrive before it. It
// is only the bound: which departures are in the window depends on when each one
// arrives, which is known once each is run (departureCandidates). The sun's times not
// known, or an outer bound before first light: no daylight window.
func (d planAssessDeps) setSuggestionWindow(w *planLegWork, env planEnv) (planNoteKind, string) {
	if !w.depart.IsZero() {
		return planKindNone, ""
	}
	fromDay, err := dayStart(w.from.Depart, w.fromLoc)
	if err != nil {
		return planKindNoWindow, planNoteNoWindow
	}
	dawn, ok := d.dawn(fromDay, w.from.Lat, w.from.Lon)
	if !ok {
		return planKindNoWindow, planNoteNoWindow
	}
	var bound time.Time
	if env.limits.DaylightArrival {
		toDay, err := dayStart(w.from.Depart, w.toLoc)
		if err != nil {
			return planKindNoWindow, planNoteNoWindow
		}
		_, set, ok := legEndSun(d.sun, w.plan, toDay, w.toLoc)
		if !ok {
			return planKindNoWindow, planNoteNoWindow
		}
		bound, w.arriveBy = set, set
	} else {
		_, set, ok := d.sun(fromDay, w.from.Lat, w.from.Lon)
		if !ok {
			return planKindNoWindow, planNoteNoWindow
		}
		bound = set
	}
	earliest, latest := ceilToStep(dawn, planDepartureStep), bound.Truncate(planDepartureStep)
	if latest.Before(earliest) {
		return planKindNoWindow, planNoteNoWindow // the day never had a window
	}
	for dep := earliest; !dep.After(latest); dep = dep.Add(planDepartureStep) {
		w.grid = append(w.grid, dep)
	}
	return planKindNone, ""
}

// readAndJudge reads what the verdicts need from the providers and judges each leg.
// Each leg's current and departure runs come first (three legs at a time, beside the
// stops' warnings), because they decide which legs need a forecast at all: a leg with
// no window, or one that has passed, never reads one. Then the forecast is read along
// the legs still to be judged and the one the boat is on.
func (d planAssessDeps) readAndJudge(ctx context.Context, p plan, work []*planLegWork, env planEnv, out *planAssessment) {
	var wg sync.WaitGroup
	if !env.verdictsOnly {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.readWarnings(p, env.now, out)
		}()
	}
	var prepared []*planLegWork
	for _, w := range work {
		if w.assessable {
			prepared = append(prepared, w)
		}
	}
	var legs sync.WaitGroup
	sem := make(chan struct{}, planLegWorkers)
	for _, w := range work {
		// A leg is read for its current and for the departures its verdict is chosen
		// from, and a leg under way for its ETA from the fix.
		if !w.readsCurrent() && w.fix == nil {
			continue
		}
		sem <- struct{}{}
		legs.Add(1)
		go func(w *planLegWork) {
			defer legs.Done()
			defer func() { <-sem }()
			d.readLeg(ctx, w, env)
		}(w)
	}
	legs.Wait()

	// The legs still to be judged. A leg readLeg gave a note (no window, window
	// passed, no timeline) needs no forecast and has had its wind changes left unread;
	// one whose ranking failed has no departure, and its wind changes are read over the
	// day.
	var assessable, windLegs []*planLegWork
	for _, w := range prepared {
		if w.assessable {
			assessable = append(assessable, w)
		}
		if w.assessable || !w.changesRead {
			windLegs = append(windLegs, w)
		}
	}
	// The one the boat is on is read too, when its ETA is known.
	for _, w := range work {
		if w.windReady && env.limitsProblem == "" {
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
		as          *passageAssessor
		forecastErr error
		stops       stopForecasts
	)
	if len(windLegs) > 0 || len(stopPoints) > 0 {
		as, forecastErr, stops = d.fetchForecast(ctx, p, windLegs, stopPoints, env)
	}
	wg.Wait() // the warnings

	if forecastErr != nil {
		log.Printf("plans: forecast failed: %v", forecastErr)
	}
	if stops.err != nil && stops.err != forecastErr {
		log.Printf("plans: stop forecasts failed: %v", stops.err)
	}
	if len(stops.failed) > 0 {
		var firstKey string
		var first error
		for key, err := range stops.failed { // any one will do: the reason is the same kind
			if first == nil || key < firstKey {
				firstKey, first = key, err
			}
		}
		log.Printf("plans: %d stop forecasts failed, first at %s: %v", len(stops.failed), firstKey, first)
	}
	// A suggested departure is ranked for comfort only when comfort is asked for,
	// wired and able to judge (newDepartureRanking). The verdicts are then made first,
	// at every candidate, and the departure, current and limiting figure are written
	// once comfort has chosen (ADR 0303). Otherwise the leg is chosen as it always was,
	// at the first Go, and says why comfort did not choose.
	var run *planComfortRun
	if env.comfort && d.comfort != nil && !env.verdictsOnly && forecastErr == nil && as != nil {
		r := newPlanComfortRun(as, env, d.comfort)
		run = &r
	}
	ranking := newDepartureRanking(run)
	for _, w := range assessable {
		w.ranking = ranking
	}
	d.judgeLegs(ctx, as, forecastErr, assessable, out)
	if env.verdictsOnly {
		return
	}
	// The ranking has a budget of its own, so the verdicts never wait on comfort.
	// Each leg that ranks gets an even share of the time left.
	deadline, left := time.Now().Add(planRankingBudget), 0
	for _, w := range assessable {
		if w.choice != nil {
			left++
		}
	}
	for _, w := range assessable {
		if w.choice == nil {
			continue
		}
		legCtx, cancelLeg := context.WithTimeout(ctx, time.Until(deadline)/time.Duration(left))
		rankDepartures(legCtx, ranking, w)
		cancelLeg()
		left--
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
			w.res.WindChanges = planWindChanges{Changes: []planWindChange{}, NotChecked: "The forecast could not be read: " + planForecastReason(forecastErr)}
		default:
			w.res.WindChanges = legWindChanges(as, w, env)
		}
		if eta, ok := w.arrival(); ok && holdsArrivalNight(p, w.i+1) {
			arrivals = append(arrivals, planArrival{stop: w.i + 1, eta: eta})
		}
	}
	nights = append(nights, arrivalNights(p, arrivals, env.now)...)
	out.NightWinds = nightWinds(as, stops, nights, env)
	// The comfort chip comes last, at the departure chosen for each leg. A suggested
	// departure's chip is the one the ranking made; any other is walked here, once.
	if run != nil {
		for _, w := range windLegs {
			if w.res.Comfort == nil {
				w.res.Comfort = run.leg(ctx, w)
			}
		}
	}
}

// planComfortRun is what the comfort of every leg of one assessment shares: the
// forecast, and the model, bands, helm height and coastline loader read once.
// problem is why no leg could be set up, when none could.
type planComfortRun struct {
	as  *passageAssessor
	env planEnv
	// ready reads the comfort readers on the first leg that needs them, once.
	ready func() (*comfortSetup, string)
}

// newPlanComfortRun reads each of the comfort readers once. A reader that is not
// wired, or that fails, is the run's problem, said on every leg that would have
// had comfort.
func newPlanComfortRun(as *passageAssessor, env planEnv, d *planComfortDeps) planComfortRun {
	return planComfortRun{as: as, env: env, ready: sync.OnceValues(func() (*comfortSetup, string) {
		if what := d.unwired(); what != "" {
			return nil, "the " + what + " reader is not wired"
		}
		set, err := d.model()
		if err != nil {
			return nil, "the comfort model couldn't be read: " + err.Error()
		}
		bands, err := d.bands()
		if err != nil {
			return nil, "the comfort bands couldn't be read: " + err.Error()
		}
		helm, helmErr := d.helmHeight()
		setup := newComfortSetup(set, bands, helm, helmErr, d.loader, d.memo)
		setup.onPredict = d.onPredict
		return setup, ""
	})}
}

// leg is a leg's comfort, or nil when the leg has none to look at: no verdict and
// not under way.
func (r planComfortRun) leg(ctx context.Context, w *planLegWork) *planLegComfort {
	switch {
	case w.underway && !w.windReady:
		// No readable ETA: say why, as the wind changes do, never that the ETA passed.
		if w.etaWhy == "" {
			return nil
		}
		return notJudged(w.etaWhy)
	case w.underway:
	case w.res.Verdict != "" && !w.chosenDepart.IsZero() && !w.chosenArrive.IsZero():
	default:
		return nil
	}
	setup, problem := r.ready()
	if problem != "" {
		return notJudged(problem)
	}
	span, why := legSpan(w, r.env)
	if why != "" {
		return &planLegComfort{Text: why + ".", Hours: []planComfortHour{}}
	}
	return legComfort(ctx, legComfortInput{
		comfortSetup: setup, as: r.as, plan: w.plan, loc: w.fromLoc,
		from: span.from, tl: span.tl, underway: w.underway,
	})
}

// planETAPassed is why a leg under way has no span left: its ETA is behind it.
const planETAPassed = "The leg is under way and its ETA has passed"

// legSpanOf is the span of a leg a reading covers. tl is the leg's progress over its
// real span, from the chosen departure to its ETA, where a reading finds the boat at
// a time; from is where the reading starts, the departure, or now for the leg under
// way.
type legSpanOf struct {
	tl   legTimeline
	from time.Time
}

// arrive is when the span ends, the zero time for the span of a leg with no
// departure, which has no timeline.
func (s legSpanOf) arrive() time.Time {
	if !s.tl.known() {
		return time.Time{}
	}
	return s.tl.arrive()
}

// departureSpan is the span of a leg on the timeline of a departure, as the row shows
// its times, read from the departure.
func departureSpan(tl legTimeline) (legSpanOf, error) {
	tl, err := tl.truncatedTo(time.Second)
	if err != nil {
		return legSpanOf{}, err
	}
	return legSpanOf{tl: tl, from: tl.depart()}, nil
}

// legSpan is a leg's span for the readings that follow its verdict (the wind
// changes and the comfort). A leg under way whose ETA has passed has no span, and
// why says so. A leg with no departure has no arrival either.
func legSpan(w *planLegWork, env planEnv) (legSpanOf, string) {
	if w.underway {
		if !w.eta.After(env.now) {
			return legSpanOf{}, planETAPassed
		}
		// The timeline is the leg's progress from the start of the passage through
		// the latest fix to the ETA.
		if !w.underTl.known() {
			return legSpanOf{}, "No ETA: the leg has no timeline"
		}
		return legSpanOf{tl: w.underTl, from: env.now}, ""
	}
	if w.chosenDepart.IsZero() {
		return legSpanOf{}, "" // an ETA with no departure is no span
	}
	// The timeline the row's times were made from: the operator's departure, or the
	// one chosen.
	if !w.tl.known() {
		return legSpanOf{}, "No ETA: the leg has no timeline"
	}
	span, err := departureSpan(w.tl)
	if err != nil {
		return legSpanOf{}, "No ETA: " + err.Error()
	}
	return span, ""
}

// arrival is the instant the leg lands at its stop, when it has one: the ETA of
// the leg under way, else the arrival the verdict was made at.
func (w *planLegWork) arrival() (time.Time, bool) {
	if w.underway {
		return w.eta, w.windReady
	}
	return w.chosenArrive, !w.chosenArrive.IsZero()
}

// judgeLegs judges every leg that waits for its forecast, or fails them all with
// the reason the forecast was not read. A leg readLeg already decided (no window,
// window passed) is not among them, so a failed forecast is not its problem.
func (d planAssessDeps) judgeLegs(ctx context.Context, as *passageAssessor, forecastErr error, assessable []*planLegWork, out *planAssessment) {
	if len(assessable) == 0 {
		return
	}
	if forecastErr != nil {
		note := "Forecast failed: " + planForecastReason(forecastErr)
		if out.Problem == "" {
			out.Problem = note
		}
		for _, w := range assessable {
			w.fail(planKindOther, note)
		}
		return
	}
	if !as.fetchedAt.IsZero() {
		out.ForecastAsOf = as.fetchedAt.UTC().Format(time.RFC3339)
	}
	for _, w := range assessable {
		d.judgeLeg(ctx, as, w)
	}
}

// planSourcesError marks a failure to set up the weather source itself: an
// unknown or missing provider, unreadable settings, a bad zone. That is the
// operator's to fix, so it is shown as it is rather than as a fetch failure.
type planSourcesError struct{ err error }

func (e *planSourcesError) Error() string { return e.err.Error() }
func (e *planSourcesError) Unwrap() error { return e.err }

// planForecastReason is the reason a plan shows for a forecast it could not
// read: a setup error as it is, any other failure as a short fixed reason.
func planForecastReason(err error) string {
	var se *planSourcesError
	if errors.As(err, &se) {
		return firstErrorLine(se.err)
	}
	return fetchFailureReason(err)
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
		err = &planSourcesError{err}
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

// runReading is the run's current as the row shows it: one figure, the mean along
// the track over the open water the model has a current for, or the reason the
// model had none.
func runReading(r legRun, f legCurrentField) planCurrentReading {
	switch {
	case r.modelledNm > 0:
		return planCurrentReading{Have: true, MeanKn: r.figure()}
	case r.openNm() <= 0:
		return planCurrentReading{} // no open water: nothing to say of a current
	}
	return planCurrentReading{Why: noCurrentWhy(f)}
}

// readsCurrent says the leg is read for its current: it has its pace, is not under
// way, and has a departure: the operator's, whether or not the leg can be judged, or
// the grid a suggestion is looked for among, which its verdict is then chosen from.
// The current is in the speed, so a verdicts-only assessment (a slip) reads it too:
// its ETA is the one the page shows.
func (w *planLegWork) readsCurrent() bool {
	// A leg under way is timed from its fix, and is given no departure.
	if w.profile == nil || w.underway || w.noPace {
		return false
	}
	return !w.depart.IsZero() || w.assessable || w.windowless
}

// departureRun is the leg run from one departure.
type departureRun struct {
	dep time.Time
	run legRun
	err error
}

// outcome is what became of the departure: its run's fate, or fateNotRun when the run
// could not be made.
func (r departureRun) outcome() runFate {
	if r.run.fate == fateOK && r.err != nil {
		return fateNotRun
	}
	return r.run.fate
}

// failed says the departure has no ETA a leg is judged on.
func (r departureRun) failed() bool { return r.outcome() != fateOK }

// noEtaNote is the note of a leg whose run ended as fate says, "" kind for a run with
// an ETA. It is the one place that words it: the leg is suggested a departure, which
// has none ("No departure suggested: ..."), or has the operator's, or is under way,
// which have no ETA ("No ETA: ..."). A run that takes more than a day says the leg
// is not assessed, either way.
func noEtaNote(fate runFate, suggested bool, readErr error) (planNoteKind, string) {
	kind, prefix := planKindOther, "No ETA: "
	if suggested {
		kind, prefix = planKindNoDeparture, "No departure suggested: "
	}
	switch fate {
	case fateOK:
		return planKindNone, ""
	case fateOvernight:
		return planKindOvernight, planNoteOvernight
	case fateUnreadable:
		return kind, prefix + currentReadReason(readErr)
	case fateStalled:
		return kind, prefix + errCurrentAgainst.Error()
	}
	return kind, prefix + planReasonNoTimeline
}

// currentReadReason words why a leg's current could not be had, by what the error is:
// a leg too long for one request never clears, no provider is a setup fault, and any
// other failure is a request that may succeed later.
func currentReadReason(err error) string {
	var long currentTooLongError
	switch {
	case errors.As(err, &long):
		return fmt.Sprintf("the leg is too long for one request to the current model (%d cells, up to %d)", long.cells, maxCurrentsPoints)
	case errors.Is(err, errCurrentNoProvider):
		return planReasonNoProvider
	}
	return planReasonNoCurrent
}

// fateCount counts the departures with no ETA by what became of them.
type fateCount struct{ stalled, overnight, other int }

func (c *fateCount) add(fate runFate) {
	switch fate {
	case fateStalled:
		c.stalled++
	case fateOvernight:
		c.overnight++
	default:
		c.other++
	}
}

func (c fateCount) total() int { return c.stalled + c.overnight + c.other }

// dominant is the most common cause; ties go to the current against the boat, then
// to a day's passage, then to the generic one.
func (c fateCount) dominant() runFate {
	switch {
	case c.stalled >= c.overnight && c.stalled >= c.other:
		return fateStalled
	case c.overnight >= c.other:
		return fateOvernight
	}
	return fateNotRun
}

func (c fateCount) String() string {
	return fmt.Sprintf("%d stalled, %d overnight, %d not run", c.stalled, c.overnight, c.other)
}

// readLeg reads one leg's current once, first, because every run needs it, then runs
// each departure once with it: the operator's alone, or the grid a suggestion is
// looked for among, every one with the current in its speed. The leg under way is run
// from its fix. A failed read leaves the operator's departure and the leg under way
// with no ETA, and a suggested leg with no departure, before any window is worked out.
func (d planAssessDeps) readLeg(ctx context.Context, w *planLegWork, env planEnv) {
	reads := w.readsCurrent()
	var field legCurrentField
	var readErr error
	// The leg under way reads the current too, for its run from the fix.
	if reads || w.fix != nil {
		if field, readErr = d.legCurrents(ctx, w.profile); readErr != nil {
			field = nil
			log.Printf("plans: current not read for %s to %s: %v", w.from.Name, w.to.Name, readErr)
		}
	}
	if w.fix != nil {
		w.readUnderway(env, field, readErr)
	}
	if !reads {
		return
	}
	if !w.depart.IsZero() {
		// The operator's departure is the one run; its timeline is the one the row
		// shows and the verdict is judged on.
		r := departureRun{dep: w.depart, run: legRun{fate: fateUnreadable}, err: readErr}
		if readErr == nil {
			r = d.runAt(w.profile, w.depart, field)
		}
		if w.takeEta(r, field, env) && r.outcome() == fateOvernight && w.assessable {
			// The arrival shows; a leg that was to be judged is not, as for one the
			// still-water check refuses. A leg that was not keeps its own note.
			w.noDeparture(planKindOvernight, planNoteOvernight, env)
		}
		return
	}
	if readErr != nil {
		// No current, no run: nothing is worked out on a still-water grid, and no
		// window or overnight note stands in for the reason.
		kind, note := noEtaNote(fateUnreadable, true, readErr)
		w.noDeparture(kind, note, env)
		return
	}
	if w.windowless {
		return // the current was read; the window's own note stands
	}
	// Every departure is run with the current, one before now too: whether the day had
	// a window is judged on the same arrivals as the suggestion.
	runs := make([]departureRun, len(w.grid))
	var gone fateCount
	for i, dep := range w.grid {
		runs[i] = d.runAt(w.profile, dep, field)
		if fate := runs[i].outcome(); fate != fateOK {
			if gone.add(fate); fate == fateNotRun && gone.other == 1 {
				log.Printf("plans: leg %s to %s could not be run from %s: %v", w.from.Name, w.to.Name, dep.Format(time.RFC3339), runs[i].err)
			}
		}
	}
	fits, kind, note := departureCandidates(runs, env.now, w.arriveBy)
	if kind != planKindNone {
		if gone.total() > 0 {
			log.Printf("plans: leg %s to %s has no departure to suggest, departures without an ETA: %v", w.from.Name, w.to.Name, gone)
		}
		w.noDeparture(kind, note, env)
		return
	}
	cands, err := orderDepartures(fits, field)
	if err != nil {
		log.Printf("plans: no departure ranked for %s to %s: %v", w.from.Name, w.to.Name, err)
		w.fail(planKindNoDeparture, "No departure suggested: "+err.Error())
		return
	}
	w.cands = cands
}

// sharedLegCurrents remembers what a leg's current read gave for one assessment of the
// slips of a plan, which run side by side: what is asked depends on the leg alone (the
// points asked for, legPaceProfile.currentKey), and the field holds only the series,
// which every slip reads along its own profile. The slips of one leg take turns, so
// the first reads and the others find its answer; the provider behind the read already
// answers a request made twice at once and keeps what it fetched. Each slip reads on
// its own context. A failure is shared for the assessment, a provider's own timeout
// included, but not one the slip's own context ended (it was cancelled or timed out):
// that is no answer for the others, and the next slip in turn reads for itself.
func sharedLegCurrents(read func(context.Context, *legPaceProfile) (legCurrentField, error)) func(context.Context, *legPaceProfile) (legCurrentField, error) {
	if read == nil {
		return nil
	}
	// turn holds one token: the slip that takes it may read or find the answer; the
	// answer is only touched with it.
	type memo struct {
		turn  chan struct{}
		done  bool
		field legCurrentField
		err   error
	}
	var (
		mu    sync.Mutex
		memos = map[string]*memo{}
	)
	return func(ctx context.Context, p *legPaceProfile) (legCurrentField, error) {
		key := p.currentKey()
		mu.Lock()
		m := memos[key]
		if m == nil {
			m = &memo{turn: make(chan struct{}, 1)}
			m.turn <- struct{}{}
			memos[key] = m
		}
		mu.Unlock()
		select {
		case <-m.turn:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		defer func() { m.turn <- struct{}{} }()
		if m.done {
			return m.field, m.err
		}
		f, err := read(ctx, p)
		// Nothing is stored when the slip's own context ended: that is its cancellation,
		// no answer for the others. A timeout the provider hit with the slip's context
		// still live is the provider's own, and the answer for every slip.
		if err != nil && ctx.Err() != nil {
			return nil, err
		}
		m.field, m.err, m.done = f, err, true
		return f, err
	}
}

// runAt runs the leg from a departure with the current.
func (d planAssessDeps) runAt(p *legPaceProfile, dep time.Time, f legCurrentField) departureRun {
	r := departureRun{dep: dep}
	if d.run != nil {
		r.run, r.err = d.run(p, dep, f)
	} else {
		r.run, r.err = p.run(dep, f)
	}
	return r
}

// noDeparture leaves a suggested leg with no window, or one that has passed, or the
// operator's with no arrival to judge. A leg that is not under way has no wind changes
// either: it is left as one that was never assessed, and its changes are not read.
func (w *planLegWork) noDeparture(kind planNoteKind, note string, env planEnv) {
	w.fail(kind, note)
	if w.fix == nil {
		// A verdicts-only assessment carries no wind changes at all: the leg keeps
		// the empty ones every leg starts with.
		if !env.verdictsOnly {
			w.res.WindChanges = unassessedWindChanges(*w.res, env, false)
		}
		w.changesRead = true
	}
}

// departureCandidates are the runs of the leg's grid that are in the window: one is
// in it when its own arrival makes the daylight the operator asked for (arriveBy,
// zero for none). A pace that depends on the time of day is not assumed to run later
// departures later, so every one is judged on its own run. A departure that cannot
// be run is never in the window, past or to come: it has no arrival to fit. The ones
// before now say only whether the day had a window: when none from now on is in it
// the kind is the window having passed, or there never having been one, with the
// note to say so; when every departure from now on has no ETA, the note names the most
// common cause (noEtaNote, fateCount.dominant). A kind of planKindNone says the leg
// has candidates.
func departureCandidates(runs []departureRun, now, arriveBy time.Time) (fits []departureRun, kind planNoteKind, note string) {
	soonest := ceilToStep(now, planDepartureStep)
	hadWindow := false
	// failedAhead counts the departures from now on that have no ETA, by what became
	// of them.
	ahead := 0
	var failedAhead fateCount
	for _, r := range runs {
		if !r.dep.Before(soonest) {
			ahead++
		}
		if r.failed() {
			if !r.dep.Before(soonest) {
				failedAhead.add(r.outcome())
			}
			continue
		}
		if !arriveBy.IsZero() && r.run.tl.arrive().After(arriveBy) {
			continue
		}
		hadWindow = true
		if !r.dep.Before(soonest) {
			fits = append(fits, r)
		}
	}
	// When every departure from now on has no ETA the note names the most common
	// cause (fateCount.dominant). The tally of fates is logged (readLeg).
	switch {
	case len(fits) > 0:
		return fits, planKindNone, ""
	case ahead > 0 && failedAhead.total() == ahead:
		kind, note := noEtaNote(failedAhead.dominant(), true, nil)
		return nil, kind, note
	case hadWindow:
		return nil, planKindWindowPassed, planNoteWindowPassed
	}
	return nil, planKindNoWindow, planNoteNoWindow
}

// orderDepartures orders the runs by their current, the fairest mean current along
// the track first and the earlier first among equals. A departure the model covers
// less than half of is left out when another is covered better, and one with no
// current is left out. The runs are ones that ran (departureCandidates leaves the
// rest out). With no current for any departure there is no ranking, and that is the
// error.
func orderDepartures(runs []departureRun, field legCurrentField) ([]tidalDeparture, error) {
	var got []departureRun
	wellCovered, openWater := false, false
	for _, r := range runs {
		if r.run.openNm() > 0 {
			openWater = true
		}
	}
	for _, r := range runs {
		if !openWater {
			// A leg wholly in slow water has no current to rank by: its departures keep
			// the grid's order and say nothing of a current.
			got = append(got, r)
			continue
		}
		if r.run.modelledNm <= 0 {
			continue
		}
		wellCovered = wellCovered || r.run.covered() >= assistantTidalMinCoveredFraction
		got = append(got, r)
	}
	var out []tidalDeparture
	sort.SliceStable(got, func(i, j int) bool { return got[i].run.alongKn > got[j].run.alongKn })
	for _, g := range got {
		if wellCovered && g.run.covered() < assistantTidalMinCoveredFraction {
			continue
		}
		out = append(out, tidalDeparture{Depart: g.dep, Current: runReading(g.run, field), run: g.run})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the Open-Meteo model has no current for the run: %s", noCurrentWhy(field))
	}
	return out, nil
}

// judgeLeg judges a leg at the operator's departure, or at the best departure the
// ranking found, and writes the verdict and what is beside it. When the leg's
// ranking can rank, a suggested leg's departures are all judged and the departure,
// its current and the figures beside the verdict wait for comfort to choose among
// them (rankDepartures); every departure of the tier it chooses in has the verdict
// this one has. Otherwise the leg stops at the first Go, and says why comfort did not
// choose when comfort was asked for.
func (d planAssessDeps) judgeLeg(ctx context.Context, as *passageAssessor, w *planLegWork) {
	var cell passageWindowCell
	var err error
	var choice *departureChoice
	if w.depart.IsZero() {
		choice, err = chooseDeparture(ctx, as, w)
		if err == nil {
			cell = choice.cells[choice.best]
		}
	} else {
		// The operator's departure is run once, for the timeline the row shows.
		cell, err = as.cellOn(ctx, w.i, w.plan, w.tl, w.toLoc)
	}
	if err != nil {
		log.Printf("plans: leg %s to %s could not be judged: %v", w.from.Name, w.to.Name, err)
		w.fail(planKindOther, err.Error())
		return
	}
	beyond := cell.beyondForecast()
	switch {
	case choice != nil && w.ranking.ranks() && choice.tier() != "":
		w.choice = choice
	case choice != nil:
		w.settle(choice.cands[choice.best], cell, beyond)
		w.res.DepartBasis = w.ranking.currentOnly(choice)
	default:
		w.settle(tidalDeparture{}, cell, beyond)
	}
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

// beyondForecast says the cell has no verdict yet because its run goes past the
// forecast's last hour.
func (c passageWindowCell) beyondForecast() bool {
	return c.State == "no_data" && c.noData == passageNoDataBeyondForecast
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

// settle writes the departure chosen for a leg and what is beside it: the departure
// and its current, for a leg the operator set no time for, then the figure that
// decided the verdict and what was not checked.
func (w *planLegWork) settle(chosen tidalDeparture, cell passageWindowCell, beyond bool) {
	// A suggested departure and its current are shown only for a leg that has a
	// verdict or a gap to explain; one with no forecast yet shows neither.
	if w.depart.IsZero() && !beyond {
		w.setDeparture(chosen, true)
		w.setCurrent(chosen.Current, nil)
	}
	if cell.Limiting != nil {
		w.res.Limiting = cell.Limiting.Short
	}
	w.res.NotChecked = cell.NotChecked
}

// cellUnjudged is the state of a departure whose cell could not be judged and was left
// out: it is neither chosen nor ranked.
const cellUnjudged = "unjudged"

// departureCell judges the leg at one departure, on the timeline that departure was
// run on. It is a variable so a test can make one departure fail.
var departureCell = func(ctx context.Context, as *passageAssessor, w *planLegWork, c tidalDeparture) (passageWindowCell, error) {
	return as.cellOn(ctx, w.i, w.plan, c.run.tl, w.toLoc)
}

// chooseDeparture judges the ranked departures, fairest current first, and finds
// the best: one that is not a No-Go, Go before Marginal, the fairest current among
// equals. When every one is a No-Go it takes the one closest to its limit, and its
// verdict says No-Go. It stops at the first Go, unless the leg's ranking ranks: then
// every departure is judged, for comfort to choose among.
func chooseDeparture(ctx context.Context, as *passageAssessor, w *planLegWork) (*departureChoice, error) {
	ch := &departureChoice{cands: w.cands, best: -1}
	foundGo := false
	for i, c := range w.cands {
		cell, err := departureCell(ctx, as, w, c)
		if err != nil {
			// Without ranking the leg would have stopped at the first Go and never
			// judged this one. A departure after a Go that cannot be judged is left
			// out, so comfort never fails a leg that had a verdict; one before it
			// fails the leg as it always did.
			if !foundGo || !w.ranking.ranks() {
				return nil, err
			}
			if ctx.Err() != nil {
				// The request ran out of time, not the forecast: stop, and count this
				// departure and the ones after it as not judged for lack of time.
				ch.unreached = len(w.cands) - i
				break
			}
			log.Printf("plans: departure %s of leg %s to %s left out of the comfort ranking: %v", c.Depart.Format(time.RFC3339), w.from.Name, w.to.Name, err)
			ch.cells = append(ch.cells, passageWindowCell{State: cellUnjudged})
			continue
		}
		ch.cells = append(ch.cells, cell)
		foundGo = foundGo || cell.State == "go"
		if ch.best < 0 {
			ch.best = i
		} else if best := ch.cells[ch.best]; departureOrder(cell) < departureOrder(best) ||
			(cell.State == "no_go" && best.State == "no_go" && cell.ratio < best.ratio) {
			ch.best = i
		}
		if cell.State == "go" && !w.ranking.ranks() {
			// The candidates are in order of current, so the first Go is the best.
			break
		}
	}
	if ch.best < 0 {
		return nil, errors.New("no departure could be judged")
	}
	return ch, nil
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

// readOnlyAssessDeps is planAssessDepsFor for a caller that must not write: a
// read tool, an outside assistant or a replay. The deps are pinned to the tool
// deps' clock and keep no first suggestions, so no such caller can forget to.
func readOnlyAssessDeps(d assistantToolDeps) planAssessDeps {
	deps := planAssessDepsFor(d).pinned()
	deps.suggested = readOnlySuggested(deps)
	return deps
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
	return c.JSON(http.StatusOK, assessPlan(ctx, d, legs, deps, planAssessOptions{Comfort: true}))
}
