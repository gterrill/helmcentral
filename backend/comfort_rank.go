package main

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"time"
)

// Ranking a leg's suggested departures by how the leg is forecast to feel
// (ADR 0303). The departures of one tier of verdict, Go or else Marginal, are each
// judged hour by hour with the code the comfort chip uses (legComfortInput), and the
// calmest one is chosen. The key is the worst band, the time in it, whether any hour
// was steeper than any recorded passage, and last the order the current gave.

// planRankingBudget is how long the whole assessment may spend ranking suggested
// departures by comfort, shared evenly among the legs still to rank: each gets what is
// left divided by the legs left, so a leg that finishes early leaves its time to the
// rest. It is a variable so a test can shorten it.
var planRankingBudget = 10 * time.Second

// planReportDepartBasis is what get_plans says a suggested departure was chosen on: it
// ranks by the current alone, where the plan page also ranks by comfort.
const planReportDepartBasis = "Fairest current, not ranked for comfort."

// comfortReasonCellFailed is why a departure left out after a Go was not ranked.
const comfortReasonCellFailed = "the forecast couldn't be read for this time"

// The bands a candidate's worst hour can be in, calmest first: twice the band's rank
// (bandRank), so a Lively hour above every rated passage sits between Lively and
// Rough.
const (
	rankComfortable = 2
	rankLively      = 4
	rankLivelyAbove = 5
	rankRough       = 6
)

// hourRank is where an hour's band sits in that order, 0 for an hour with no band.
func hourRank(h judgedHour) int {
	r := 2 * bandRank[h.band]
	if h.aboveRated {
		r++
	}
	return r
}

// comfortRankKey is what a candidate departure is ranked by.
type comfortRankKey struct {
	worst int
	// span is the time spent in the worst band.
	span time.Duration
	// steeper is true when any judged hour was in seas steeper than any recorded.
	steeper bool
	// startUnjudged and endUnjudged say a not-judged stretch at the start or the end
	// of the leg (the harbour ends) was left out of the ranking. A position charted
	// ashore at an end is left out without saying so.
	startUnjudged, endUnjudged bool
	// startWhy and endWhy are the first reason at that end that is not geography, ""
	// when every reason there is; startSpan and endSpan are the time those stretches
	// cover. The basis names such a reason and its hours instead of saying harbour.
	startWhy, endWhy   string
	startSpan, endSpan time.Duration
}

// harbourReason says a reason is the shore's: an enclosed swell, a wind wave not checked
// against fetch, or a position charted ashore. Only those make an end "harbour".
func harbourReason(why string) bool {
	return why == comfortSwellEnclosed || why == comfortReasonAshore ||
		strings.HasPrefix(why, "wind wave not checked against fetch")
}

// hours is the whole hours spent in the worst band, rounded up so any time in it is at
// least an hour, and 0 when that band is Comfortable (a Comfortable leg is Comfortable
// however long). A few minutes either side of the half hour never decide the pick.
func (k comfortRankKey) hours() int {
	if k.worst == rankComfortable {
		return 0
	}
	return int((k.span + time.Hour - 1) / time.Hour)
}

// hoursText is the whole hours the ranking compared, in words.
func hoursText(h int) string { return fmt.Sprintf("%d h", h) }

// rankKeyOf is the key of a candidate's judged hours, or why it has none. The
// harbour ends are ignored: a stretch not judged, or charted ashore, before the first
// judged stretch or after the last, whatever the reason, and the candidate is ranked
// on the stretches between. Any other stretch that was not judged, a gap in the
// middle with judged hours on both sides, a position charted ashore there, or a run
// that ran out of time, leaves the candidate unranked, since a gap never reads as
// calm. A leg with no judged hour is unranked.
func rankKeyOf(hours []judgedHour) (comfortRankKey, string) {
	// A run that ran out of time is never a harbour end: a sliver of it must not fold
	// into the stretch beside it and be lost, so the raw hours are read first.
	for _, h := range hours {
		if h.reason == comfortReasonOutOfTime {
			return comfortRankKey{}, comfortReasonOutOfTime
		}
	}
	stretches := stretchesOf(hours)
	first, last := -1, -1
	for i, s := range stretches {
		if !s.gap() {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	var key comfortRankKey
	for i, s := range stretches {
		switch {
		case !s.gap():
		case first < 0 || (i > first && i < last):
			return comfortRankKey{}, s.reason
		case s.reason != comfortReasonAshore:
			if i < first {
				key.startUnjudged = true
				if !harbourReason(s.reason) {
					key.startWhy = cmp.Or(key.startWhy, s.reason)
					key.startSpan += s.length()
				}
			} else {
				key.endUnjudged = true
				if !harbourReason(s.reason) {
					key.endWhy = cmp.Or(key.endWhy, s.reason)
					key.endSpan += s.length()
				}
			}
		}
	}
	var span [rankRough + 1]time.Duration
	for _, s := range stretches {
		if s.gap() {
			continue
		}
		top := 0
		for _, h := range s.hours {
			c := hourRank(h)
			span[c] += h.span()
			top = max(top, c)
		}
		// The slivers folded into a stretch count toward the stretch's worst band.
		span[top] += s.slack
		key.worst = max(key.worst, top)
		if steeper, _ := s.anyBeyond(); steeper {
			key.steeper = true
		}
	}
	if key.worst == 0 {
		return comfortRankKey{}, comfortNoHourReason
	}
	key.span = span[key.worst]
	return key, ""
}

// before says whether k ranks ahead of o. Keys that are not before each other in
// either direction are alike, and the current breaks the tie.
func (k comfortRankKey) before(o comfortRankKey) bool {
	switch {
	case k.worst != o.worst:
		return k.worst < o.worst
	case k.hours() != o.hours():
		return k.hours() < o.hours()
	}
	return !k.steeper && o.steeper
}

func (k comfortRankKey) alike(o comfortRankKey) bool { return !k.before(o) && !o.before(k) }

// phrase is the key in words: "Comfortable throughout", "Lively for 2 h".
func (k comfortRankKey) phrase() string {
	var s string
	switch k.worst {
	case rankComfortable:
		s = bandWord(bandComfortable) + " throughout"
	case rankLively:
		s = bandWord(bandLively) + " for " + hoursText(k.hours())
	case rankLivelyAbove:
		s = bandWord(bandLively) + ", " + comfortBeyondRough + ", for " + hoursText(k.hours())
	default:
		s = bandWord(bandRough) + " for " + hoursText(k.hours())
	}
	if k.steeper {
		s += ", " + comfortBeyondSteep
	}
	return s
}

// insteadOf is what the chosen key beat, in words: just the time when both are in
// the same band, so "Lively for 1 h instead of 3 h".
func (k comfortRankKey) insteadOf(o comfortRankKey) string {
	if k.worst == o.worst && k.worst != rankComfortable && k.hours() != o.hours() {
		return hoursText(o.hours())
	}
	return o.phrase()
}

// rankReason is a reason as the basis says it: capitalised, with a full stop. An
// acronym such as OSM keeps its capitals.
func rankReason(why string) string {
	return endSentence(capitalise(reasonClause(why)))
}

// currentOnlyBasis is the basis of a departure chosen by the current alone because
// comfort could not rank it.
func currentOnlyBasis(why string) string {
	return "Not chosen for comfort: " + rankReason(why) + " Fairest stream."
}

// endsText says what was left out at the ends of the leg, each sentence with a leading
// space, "" when nothing was. An end whose reasons are all the shore's is harbour; an
// end with any other reason names it and the hours.
func (k comfortRankKey) endsText() string {
	harbourStart := k.startUnjudged && k.startWhy == ""
	harbourEnd := k.endUnjudged && k.endWhy == ""
	var s string
	switch {
	case harbourStart && harbourEnd:
		s += " Not judged leaving and entering harbour."
	case harbourStart:
		s += " Not judged leaving harbour."
	}
	if k.startWhy != "" {
		s += " " + endGapSentence("first", k.startSpan, k.startWhy)
	}
	if k.endWhy != "" {
		s += " " + endGapSentence("last", k.endSpan, k.endWhy)
	}
	if harbourEnd && !harbourStart {
		s += " Not judged entering harbour."
	}
	return s
}

// endGapSentence is "Not judged for the last 2 h: no forecast for the hour.", the span
// in whole hours rounded up.
func endGapSentence(which string, span time.Duration, why string) string {
	return fmt.Sprintf("Not judged for the %s %s: %s.", which, hoursText(int((span+time.Hour-1)/time.Hour)), reasonClause(why))
}

// rankedBasis is the basis of a departure chosen for comfort, with a closing sentence
// when its harbour ends were not judged. base is the key of the
// fairest-current departure, which the ranking replaces, nil when it was not ranked.
// alike counts the candidates the current had to choose among. unranked is how many of
// total candidates went unranked, and why is the first reason; when some were ranked
// and some not, the basis says so.
func rankedBasis(chosen comfortRankKey, base *comfortRankKey, alike, unranked, total int, why string) string {
	s := "Chosen for comfort: " + chosen.phrase()
	if base != nil && !base.alike(chosen) {
		s += " instead of " + chosen.insteadOf(*base)
	}
	if alike > 1 {
		s += fmt.Sprintf(", then the fairest stream of %d alike", alike)
	}
	s += "."
	s += chosen.endsText()
	if unranked > 0 {
		s += fmt.Sprintf(" %d of %d times not judged: %s.", unranked, total, reasonClause(why))
	}
	return s
}

// departureRanking is how one request chooses the departure of a suggested leg when
// comfort was asked for. With a setup, every departure of the best verdict is judged
// and ranked. With why instead, comfort cannot rank (a reader failed, or the bands
// are not learned): the departures are chosen as they were, the fairest current
// among the verdicts, and the leg says why. A nil ranking is comfort not asked for.
type departureRanking struct {
	run   planComfortRun
	setup *comfortSetup
	why   string
}

// newDepartureRanking reads the comfort readers once, for the request.
func newDepartureRanking(run *planComfortRun) *departureRanking {
	if run == nil {
		return nil
	}
	rk := &departureRanking{run: *run}
	switch setup, problem := run.ready(); {
	case problem != "":
		rk.why = problem
	case !setup.bands.Learned:
		rk.why = comfortNotLearnedReason
	default:
		rk.setup = setup
	}
	return rk
}

// ranks says every departure of a leg is judged, for comfort to choose among.
func (rk *departureRanking) ranks() bool { return rk != nil && rk.setup != nil }

// currentOnly is the basis of a leg whose departure was chosen by the current alone
// although comfort was asked for, nil when the leg has no tier to rank or comfort
// could have ranked it.
func (rk *departureRanking) currentOnly(ch *departureChoice) *string {
	if rk == nil || rk.setup != nil || ch.tier() == "" {
		return nil
	}
	basis := currentOnlyBasis(rk.why)
	return &basis
}

// departureChoice is the departures of a suggested leg, each judged for its verdict,
// held until comfort has had its say. best is today's choice: Go before Marginal, the
// fairest current among equals.
type departureChoice struct {
	cands []tidalDeparture
	cells []passageWindowCell
	best  int
	// unreached is how many candidates, the last ones, were never judged because the
	// request ran out of time.
	unreached int
}

// tier is the verdict state that is ranked for comfort: that of today's choice when
// it is Go, or Marginal when no departure is a Go. "" says there is no tier and
// today's choice stands.
func (ch *departureChoice) tier() string {
	if state := ch.cells[ch.best].State; state == "go" || state == "marginal" {
		return state
	}
	return ""
}

// rank picks the departure of the leg. basis is "" when the leg has no tier to rank;
// chip is the chosen departure's comfort, made from the hours it was ranked on, nil
// when comfort did not choose.
func (ch *departureChoice) rank(ctx context.Context, rk *departureRanking, w *planLegWork) (pick int, basis string, chip *planLegComfort) {
	state := ch.tier()
	if state == "" {
		return ch.best, "", nil
	}
	defer rk.setup.endLeg()
	// The calmest so far, with the hours its chip is made from; the other candidates'
	// hours are let go as soon as they are ranked.
	var (
		found     bool
		bestAt    int
		bestKey   comfortRankKey
		bestIn    legComfortInput
		bestHours []judgedHour
		alike     int
		base      *comfortRankKey
		firstWhy  string
		total     int
		unranked  int
	)
	for i, cell := range ch.cells {
		if cell.State == cellUnjudged {
			// Left out after a Go: it counts among the times not judged.
			total++
			unranked++
			if firstWhy == "" {
				firstWhy = comfortReasonCellFailed
			}
			continue
		}
		if cell.State != state {
			continue
		}
		total++
		// A block (a wave model with no fit, a walk that failed) is read over this
		// candidate's own hours, so it leaves this one unranked and the rest go on.
		in, hours, why := ch.judge(ctx, rk, w, i)
		var key comfortRankKey
		if why == "" {
			key, why = rankKeyOf(hours)
		}
		if why != "" {
			unranked++
			if firstWhy == "" {
				firstWhy = why
			}
			continue
		}
		if i == ch.best {
			base = &key
		}
		switch {
		case !found || key.before(bestKey):
			found, bestAt, bestKey, bestIn, bestHours, alike = true, i, key, in, hours, 1
		case !bestKey.before(key):
			alike++
		}
	}
	if ch.unreached > 0 {
		// The request ran out of time before these were judged for a verdict.
		total += ch.unreached
		unranked += ch.unreached
		firstWhy = cmp.Or(firstWhy, comfortReasonOutOfTime)
	}
	if !found {
		// No chip: the departure kept is walked for it under the request's own time.
		return ch.best, currentOnlyBasis(firstWhy), nil
	}
	return bestAt, rankedBasis(bestKey, base, alike, unranked, total, firstWhy), bestIn.comfortOf(bestHours)
}

// judge walks one candidate's hours, or says why it cannot be: the budget ran out
// before it, or the ladder blocked it. Every block is the candidate's own: the ladder
// checks the wave models over its hours, and a walk error belongs to its samples; a
// setup problem is caught before any candidate (newDepartureRanking). The span is the
// one the leg's row shows for that departure, so the chip made from these hours is the
// chip of the row.
func (ch *departureChoice) judge(ctx context.Context, rk *departureRanking, w *planLegWork, i int) (in legComfortInput, hours []judgedHour, why string) {
	span, err := departureSpan(ch.cands[i].tl)
	if err != nil {
		return legComfortInput{}, nil, "No ETA: " + err.Error()
	}
	in = legComfortInput{
		comfortSetup: rk.setup, as: rk.run.as, plan: w.plan, loc: w.fromLoc,
		from: span.from, tl: span.tl,
	}
	if ctx.Err() != nil {
		return in, nil, comfortReasonOutOfTime
	}
	if rankWalkHook != nil {
		rankWalkHook(w)
	}
	hours, blocked := in.judgedHours(ctx)
	return in, hours, blocked
}

// rankWalkHook is a test hook, nil in production: a candidate's hours are about to be
// walked for the ranking.
var rankWalkHook func(w *planLegWork)

// rankDepartures settles the departure of a leg whose departures were judged: it
// ranks them for comfort and writes the one chosen, its comfort and the basis.
func rankDepartures(ctx context.Context, rk *departureRanking, w *planLegWork) {
	ch := w.choice
	pick, basis, chip := ch.rank(ctx, rk, w)
	w.res.Comfort = chip
	cell := ch.cells[pick]
	w.settle(ch.cands[pick], cell, cell.beyondForecast())
	if basis != "" {
		w.res.DepartBasis = &basis
	}
}
