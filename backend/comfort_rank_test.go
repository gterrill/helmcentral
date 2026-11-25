package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A suggested departure is ranked by comfort (ADR 0303). Every leg here is 30 nm at
// 6 kn, five hours, heading about south. The swell comes from the east (100 degrees),
// on the beam: 0.2 m reads Comfortable, 1 m reads Lively (2 degrees of roll), and
// 2 m reads Lively above every rated passage. A wind of 16 kn dead ahead is Marginal
// and 24 kn is a No-Go.

// rkSpec is the sea and wind of one forecast hour.
type rkSpec struct {
	swellM  float64
	windKn  float64
	noSplit bool // the forecast has a wave height and no swell / wind-wave split
	noLabel bool // the wave plugin names no model
	// otherModel is a wave model the comfort model was not learned on (no fit).
	otherModel bool
}

var rkCalm = rkSpec{swellM: 0.2, windKn: 10}

// rkForecast is a forecast whose hour h of the 14th (0 to 23) is at(h); every other
// hour is calm.
func rkForecast(at func(h int) rkSpec) forecastSource {
	return lcForecastOf(func(t time.Time) passageHour {
		s := rkCalm
		if h := int(t.Sub(paAt(14, 0, 0)) / time.Hour); h < 24 {
			s = at(h)
		}
		x := lcHour(s.swellM, 100)
		x.WindDirDeg, x.WindMS, x.GustMS = 190, kn(s.windKn), kn(s.windKn)
		if s.noSplit {
			x.Swell = nil
		}
		if s.noLabel {
			x.WaveModel = waveModelRef{}
		}
		if s.otherModel {
			x.WaveModel = waveModelRef{ID: "other", Label: "Another model"}
		}
		return x
	})
}

// rkReader holds open water along the whole leg, each line given in the square of its
// first point.
func rkReader(t *testing.T, p plan, lines []shorelineLine) *offlineCoastlineReader {
	t.Helper()
	bySquare := map[coverageSquare][]shorelineLine{}
	for _, l := range lines {
		sq, err := squareContaining(l.Points[0].Lat, l.Points[0].Lon)
		if err != nil {
			t.Fatal(err)
		}
		bySquare[sq] = append(bySquare[sq], l)
	}
	var points [][2]float64
	for i := 0; i+1 < len(p.Stops); i++ {
		a, b := p.Stops[i], p.Stops[i+1]
		for j := 0; j <= 8; j++ {
			f := float64(j) / 8
			points = append(points, [2]float64{a.Lat + (b.Lat-a.Lat)*f, a.Lon + (b.Lon-a.Lon)*f})
		}
	}
	store := seedCoastlineAlong(t, time.Now(), points, func(sq coverageSquare) []shorelineLine { return bySquare[sq] })
	return &offlineCoastlineReader{Enabled: func() (bool, error) { return true, nil }, Store: store}
}

// rkIsland is a small island, charted as land, over the leg's position at fraction f of
// the way from its first stop to its last.
func rkIsland(p plan, f float64) shorelineLine {
	a, b := p.Stops[0], p.Stops[1]
	lat, lon := a.Lat+(b.Lat-a.Lat)*f, a.Lon+(b.Lon-a.Lon)*f
	proj := newWRProjection(lat, lon)
	pt := func(x, y float64) shorelinePoint { return proj.inv(wrPt{x, y}) }
	return fetchWay("island", pt(-0.2, 0.2), pt(-0.2, -0.2), pt(0.2, -0.2), pt(0.2, 0.2), pt(-0.2, 0.2))
}

// rkRun is a plan, its deps and what they were asked.
type rkRun struct {
	p        plan
	legs     []planLeg
	d        planAssessDeps
	models   atomic.Int32
	predicts atomic.Int32
}

type rkCfg struct {
	at     func(h int) rkSpec
	cands  []time.Time // in the order of the fairest current first
	set    *comfortModelSet
	bands  *comfortBands
	lines  []shorelineLine
	nights []int // the plan's nights; two stops, one leg, when empty
	// lastHour, when set, is the last hour of the 14th the forecast has.
	lastHour int
}

func rkSetup(t *testing.T, cfg rkCfg) *rkRun {
	t.Helper()
	nights := cfg.nights
	if len(nights) == 0 {
		nights = []int{0, 0}
	}
	r := &rkRun{}
	r.p, r.legs = paPlan(t, "2026-11-14", nights...)
	fc := rkForecast(cfg.at)
	if cfg.lastHour > 0 {
		inner, last := fc.Fetch, paAt(14, cfg.lastHour, 0).Unix()
		fc.Fetch = func(ctx context.Context, lat, lon float64) (passageForecast, error) {
			pf, err := inner(ctx, lat, lon)
			cut := map[int64]passageHour{}
			for k, v := range pf.Hours {
				if k <= last {
					cut[k] = v
				}
			}
			pf.Hours = cut
			return pf, err
		}
	}
	f := &paFake{fc: fc, departures: func() ([]tidalDeparture, error) {
		var out []tidalDeparture
		for i, c := range cfg.cands {
			out = append(out, paDeparture(c.Hour(), c.Minute(), 1-0.1*float64(i)))
		}
		return out, nil
	}}
	r.d = lcDeps(t, f, &r.models)
	reader := rkReader(t, r.p, cfg.lines)
	c := r.d.comfort
	c.loader = func() *coastlineLoader { return reader.newLoader() }
	c.onPredict = func() { r.predicts.Add(1) }
	if cfg.set != nil {
		c.model = func() (*comfortModelSet, error) {
			r.models.Add(1)
			return cfg.set, nil
		}
	}
	if cfg.bands != nil {
		c.bands = func() (comfortBands, error) { return *cfg.bands, nil }
	}
	return r
}

func (r *rkRun) assess(opts planAssessOptions) planAssessment {
	return assessPlan(context.Background(), r.p, r.legs, r.d, opts)
}

func rkAt(h int) time.Time { return paAt(14, h, 0) }

// rkLively is a forecast that is Lively (1 m) in the hours given, calm in the rest.
func rkLively(hours ...int) func(int) rkSpec {
	return func(h int) rkSpec {
		for _, x := range hours {
			if x == h {
				return rkSpec{swellM: 1, windKn: 10}
			}
		}
		return rkCalm
	}
}

func rkBasis(t *testing.T, l planLegAssessment) string {
	t.Helper()
	if l.DepartBasis == nil {
		t.Fatalf("leg has no depart_basis: %+v", l)
	}
	return *l.DepartBasis
}

func rkWantDepart(t *testing.T, l planLegAssessment, want time.Time) {
	t.Helper()
	if got := paParse(t, l.Depart); !got.Equal(want) || !l.DepartSuggested {
		t.Fatalf("depart = %s (suggested %v), want %s", l.Depart, l.DepartSuggested, want.Format(time.RFC3339))
	}
}

// 1
func TestRankComfort_AComfortableGoBeatsALivelyGoWithFairerCurrent(t *testing.T) {
	r := rkSetup(t, rkCfg{at: rkLively(8), cands: []time.Time{rkAt(8), rkAt(9)}})
	l := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	rkWantDepart(t, l, rkAt(9))
	if l.Verdict != "go" {
		t.Errorf("verdict = %q", l.Verdict)
	}
	if got, want := rkBasis(t, l), "Chosen for comfort: Comfortable throughout instead of Lively for 1 h."; got != want {
		t.Errorf("basis = %q, want %q", got, want)
	}
	// Without comfort the fairest current wins, as it did.
	plain := r.assess(planAssessOptions{}).Legs[0]
	rkWantDepart(t, plain, rkAt(8))
	if plain.DepartBasis != nil {
		t.Errorf("basis without comfort = %q", *plain.DepartBasis)
	}
}

// 2
func TestRankComfort_WithTheWorstBandEqualFewerHoursInItWins(t *testing.T) {
	r := rkSetup(t, rkCfg{at: rkLively(8, 9), cands: []time.Time{rkAt(8), rkAt(9)}})
	l := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	rkWantDepart(t, l, rkAt(9))
	if got, want := rkBasis(t, l), "Chosen for comfort: Lively for 1 h instead of 2 h."; got != want {
		t.Errorf("basis = %q, want %q", got, want)
	}
}

// 3
func TestRankComfort_WithTheKeyEqualTheFairestCurrentWins(t *testing.T) {
	r := rkSetup(t, rkCfg{at: func(int) rkSpec { return rkCalm }, cands: []time.Time{rkAt(8), rkAt(9), rkAt(10)}})
	l := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	rkWantDepart(t, l, rkAt(8))
	if got, want := rkBasis(t, l), "Chosen for comfort: Comfortable throughout, then the fairest stream of 3 alike."; got != want {
		t.Errorf("basis = %q, want %q", got, want)
	}
}

// 4
func TestRankComfort_WithNoGoTheMarginalsAreRankedAndAllNoGoKeepsTodaysChoice(t *testing.T) {
	// Hour 8 is a No-Go (24 kn), the rest Marginal (16 kn); hour 9 is Lively.
	at := func(h int) rkSpec {
		s := rkSpec{swellM: 0.2, windKn: 16}
		switch h {
		case 8:
			s.windKn = 24
		case 9:
			s.swellM = 1
		}
		return s
	}
	r := rkSetup(t, rkCfg{at: at, cands: []time.Time{rkAt(8), rkAt(9), rkAt(10)}})
	l := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	rkWantDepart(t, l, rkAt(10))
	if l.Verdict != "marginal" {
		t.Errorf("verdict = %q", l.Verdict)
	}
	if got, want := rkBasis(t, l), "Chosen for comfort: Comfortable throughout instead of Lively for 1 h."; got != want {
		t.Errorf("basis = %q, want %q", got, want)
	}

	// All No-Go: the departure closest to its limit, as before, and no ranking.
	allNoGo := func(h int) rkSpec {
		s := rkSpec{swellM: 0.2, windKn: 24}
		if h == 8 {
			s.windKn = 30
		}
		return s
	}
	r = rkSetup(t, rkCfg{at: allNoGo, cands: []time.Time{rkAt(8), rkAt(9), rkAt(10)}})
	asked := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	plain := r.assess(planAssessOptions{}).Legs[0]
	if asked.Verdict != "no_go" || asked.Depart != plain.Depart || asked.Limiting != plain.Limiting {
		t.Fatalf("no-go with comfort: %+v\nwithout:              %+v", asked, plain)
	}
	if asked.DepartBasis != nil {
		t.Errorf("a No-Go departure is not ranked for comfort, basis = %q", *asked.DepartBasis)
	}
}

// 5 is the mid-leg gap case, TestRankComfort_AGapInTheMiddleOfTheLegStillLeavesTheCandidateUnranked.

// 6
func TestRankComfort_ABerthAshoreHourOnEveryCandidateStillRanks(t *testing.T) {
	// A small island over the first stop: the first hour of every candidate is at the
	// berth, which is charted ashore, and says nothing about the sea.
	p, _ := paPlan(t, "2026-11-14", 0, 0)
	a := p.Stops[0]
	proj := newWRProjection(a.Lat, a.Lon)
	pt := func(x, y float64) shorelinePoint { return proj.inv(wrPt{x, y}) }
	island := fetchWay("island", pt(-0.2, 0.2), pt(-0.2, -0.2), pt(0.2, -0.2), pt(0.2, 0.2), pt(-0.2, 0.2))
	r := rkSetup(t, rkCfg{at: rkLively(9), cands: []time.Time{rkAt(8), rkAt(9)}, lines: []shorelineLine{island}})
	// The stop's own hour reads as a gap on the chip, which is the point of the test.
	l := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	if l.Comfort == nil || len(l.Comfort.Hours) == 0 || !strings.Contains(l.Comfort.Hours[0].Line, "Not judged: position charted ashore") {
		t.Fatalf("the first hour is not ashore: %+v", l.Comfort)
	}
	rkWantDepart(t, l, rkAt(9))
	if got, want := rkBasis(t, l), "Chosen for comfort: Comfortable throughout instead of Lively for 1 h."; got != want {
		t.Errorf("basis = %q, want %q", got, want)
	}
}

// 7
func TestRankComfort_AnAboveRatedLivelyRanksBelowAnInRecordLively(t *testing.T) {
	// 8 AM has one hour above every rated passage; 12 PM has two ordinary Lively hours.
	at := func(h int) rkSpec {
		switch h {
		case 8:
			return rkSpec{swellM: 2, windKn: 10}
		case 13, 14:
			return rkSpec{swellM: 1, windKn: 10}
		}
		return rkCalm
	}
	r := rkSetup(t, rkCfg{at: at, cands: []time.Time{rkAt(8), rkAt(12)}})
	l := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	rkWantDepart(t, l, rkAt(12))
	if got, want := rkBasis(t, l), "Chosen for comfort: Lively for 2 h instead of Lively, rougher than any recorded passage, for 1 h."; got != want {
		t.Errorf("basis = %q, want %q", got, want)
	}
}

func rkHour(band string, h float64) judgedHour {
	start := paAt(14, 8, 0)
	sway := 1.0
	return judgedHour{start: start, end: start.Add(hoursDuration(h)), band: band, swayDeg: &sway}
}

// Whole hours, rounded up, decide: a few minutes either side of the half hour do not.
func TestRankKey_CountsWholeHoursRoundedUp(t *testing.T) {
	key := func(h float64) comfortRankKey {
		t.Helper()
		k, why := rankKeyOf([]judgedHour{rkHour(bandLively, h), rkHour(bandComfortable, 4)})
		if why != "" {
			t.Fatalf("not ranked: %s", why)
		}
		return k
	}
	if a, b := key(29.0/60), key(31.0/60); !a.alike(b) {
		t.Errorf("29 min and 31 min in the same band are not alike: %d h, %d h", a.hours(), b.hours())
	}
	short, long := key(50.0/60), key(70.0/60)
	if !short.before(long) || long.before(short) {
		t.Errorf("50 min must rank before 70 min: %d h, %d h", short.hours(), long.hours())
	}
	if got, want := rankedBasis(short, &long, 1, 0, 2, ""), "Chosen for comfort: Lively for 1 h instead of 2 h."; got != want {
		t.Errorf("basis = %q, want %q", got, want)
	}
	if got, want := rankedBasis(key(29.0/60), nil, 1, 0, 1, ""), "Chosen for comfort: Lively for 1 h."; got != want {
		t.Errorf("basis = %q, want %q", got, want)
	}
}

// With ranking on, a candidate whose cell errors after a Go is found is left out; one
// that errors before any Go still fails the leg, as it did.
func TestRankComfort_ACandidateThatErrorsAfterAGoIsLeftOut(t *testing.T) {
	fail := func(bad time.Time) {
		old := departureCell
		departureCell = func(ctx context.Context, as *passageAssessor, w *planLegWork, c tidalDeparture) (passageWindowCell, error) {
			if c.Depart.Equal(bad) {
				return passageWindowCell{}, errors.New("the forecast broke")
			}
			return old(ctx, as, w, c)
		}
		t.Cleanup(func() { departureCell = old })
	}
	cands := []time.Time{rkAt(8), rkAt(9), rkAt(10)}
	r := rkSetup(t, rkCfg{at: rkLively(8), cands: cands})
	fail(rkAt(10))
	l := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	if l.Verdict != "go" {
		t.Fatalf("verdict = %q (%s), want go", l.Verdict, l.Note)
	}
	rkWantDepart(t, l, rkAt(9))
	// The left-out time counts among the times not judged.
	if got, want := rkBasis(t, l), "Chosen for comfort: Comfortable throughout instead of Lively for 1 h. 1 of 3 times not judged: the forecast couldn't be read for this time."; got != want {
		t.Errorf("basis = %q, want %q", got, want)
	}

	// The fairest current's own error still fails the leg.
	r = rkSetup(t, rkCfg{at: rkLively(8), cands: cands})
	fail(rkAt(8))
	l = r.assess(planAssessOptions{Comfort: true}).Legs[0]
	if l.Verdict != "" || l.NoteKind != planKindOther {
		t.Errorf("an error on the first candidate: %+v", l)
	}
}

// A suggested leg with no forecast yet is settled as it always was, however the
// ranking is asked for.
func TestRankComfort_ALegBeyondTheForecastIsTheSameWithRankingOnAndOff(t *testing.T) {
	r := rkSetup(t, rkCfg{at: rkLively(8), cands: []time.Time{rkAt(8), rkAt(9)}, lastHour: 10})
	on := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	off := r.assess(planAssessOptions{}).Legs[0]
	if off.NoteKind != planKindNoForecast {
		t.Fatalf("the leg is not beyond the forecast: %+v", off)
	}
	type fields struct {
		Verdict, Limiting, Note, Depart string
		NoteKind                        planNoteKind
		Suggested                       bool
		NotChecked                      []string
		Basis                           *string
	}
	of := func(l planLegAssessment) fields {
		return fields{l.Verdict, l.Limiting, l.Note, l.Depart, l.NoteKind, l.DepartSuggested, l.NotChecked, l.DepartBasis}
	}
	if a, b := of(on), of(off); !equalJSON(t, a, b) {
		t.Errorf("ranking on:  %+v\nranking off: %+v", a, b)
	}
}

// The ranking budget runs out partway: the candidates reached are ranked and the basis
// says how many were not judged.
func TestRankComfort_APartialBudgetSaysHowManyWereNotJudged(t *testing.T) {
	r := rkSetup(t, rkCfg{at: rkLively(9), cands: []time.Time{rkAt(8), rkAt(9), rkAt(10)}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The first candidate's five hours are asked for, then time runs out.
	r.d.comfort.onPredict = func() {
		if r.predicts.Add(1) == 5 {
			cancel()
		}
	}
	l := assessPlan(ctx, r.p, r.legs, r.d, planAssessOptions{Comfort: true}).Legs[0]
	rkWantDepart(t, l, rkAt(8))
	want := "Chosen for comfort: Lively for 1 h. 2 of 3 times not judged: the comfort check ran out of time."
	if got := rkBasis(t, l); got != want {
		t.Errorf("basis = %q, want %q", got, want)
	}
}

// When no candidate can be ranked, the chip is walked fresh for the departure kept, as
// before ranking: the ranking's hours are reused only for a ranked choice.
func TestRankComfort_WithNoCandidateRankedTheChipIsWalkedFresh(t *testing.T) {
	// Hour 10 is not judged: mid-leg for every candidate here.
	r := rkSetup(t, rkCfg{at: rkGapsLively([]int{10}, nil), cands: []time.Time{rkAt(8), rkAt(9)}})
	l := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	if got := rkBasis(t, l); !strings.HasPrefix(got, "Not chosen for comfort: ") {
		t.Fatalf("basis = %q", got)
	}
	if l.Comfort == nil {
		t.Fatal("the leg has no chip")
	}
	if got, want := int(r.predicts.Load()), 2*4+4; got != want {
		t.Errorf("the model was asked %d times, want %d: each candidate's four judged hours once, then the chip's own walk", got, want)
	}
}

// When the ranking's budget runs out partway through the fairest candidate, no
// candidate ranks, and the chip says nothing of the ranking's time: the request still
// has some, and the chip is walked under it.
func TestRankComfort_ABudgetThatRanOutMidWalkLeavesTheChipItsOwnWalk(t *testing.T) {
	r := rkSetup(t, rkCfg{at: rkLively(9), cands: []time.Time{rkAt(8), rkAt(9), rkAt(10)}})
	old, oldBudget := rankWalkHook, planRankingBudget
	planRankingBudget = 200 * time.Millisecond
	first := true
	rankWalkHook = func(*planLegWork) {
		if first {
			first = false
			time.Sleep(300 * time.Millisecond) // past the budget, inside the first walk
		}
	}
	t.Cleanup(func() { rankWalkHook, planRankingBudget = old, oldBudget })
	l := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	if got := rkBasis(t, l); got != "Not chosen for comfort: The comfort check ran out of time. Fairest stream." {
		t.Fatalf("basis = %q", got)
	}
	if l.Comfort == nil {
		t.Fatal("the leg has no chip")
	}
	if b, _ := json.Marshal(l.Comfort); strings.Contains(string(b), "ran out of time") {
		t.Errorf("the chip carries the ranking's lack of time: %s", b)
	}
}

// A block is read over a candidate's own hours, so one that holds back the model for
// every candidate (the wave plugin names no model) is met once by each: every candidate
// is walked once, none ranks, and the chip says why.
func TestRankComfort_ABlockOnEveryCandidateLeavesNoneRankedAndEachWalkedOnce(t *testing.T) {
	at := func(int) rkSpec { return rkSpec{swellM: 0.2, windKn: 10, noLabel: true} }
	r := rkSetup(t, rkCfg{at: at, cands: []time.Time{rkAt(8), rkAt(9), rkAt(10)}})
	walks := 0
	old := rankWalkHook
	rankWalkHook = func(*planLegWork) { walks++ }
	t.Cleanup(func() { rankWalkHook = old })
	l := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	if got := rkBasis(t, l); !strings.HasPrefix(got, "Not chosen for comfort: ") || !strings.Contains(got, "doesn't name its wave model") {
		t.Errorf("basis = %q", got)
	}
	rkWantDepart(t, l, rkAt(8))
	if walks != 3 {
		t.Errorf("%d candidates walked for the ranking, want 3", walks)
	}
	if got := r.predicts.Load(); got != 0 {
		t.Errorf("the model was asked %d times for a blocked leg", got)
	}
	if l.Comfort == nil || !strings.Contains(l.Comfort.Text, "doesn't name its wave model") {
		t.Errorf("chip = %+v", l.Comfort)
	}
}

// Each leg that ranks gets an even share of what is left of the budget, so a slow first
// leg cannot take the others' time.
func TestRankComfort_ASlowFirstLegDoesNotTakeTheOthersTime(t *testing.T) {
	r := rkSetup(t, rkCfg{at: rkLively(9), cands: []time.Time{rkAt(8), rkAt(9), rkAt(10)}, nights: []int{0, 0, 0, 0}})
	old, oldBudget := rankWalkHook, planRankingBudget
	planRankingBudget = 1500 * time.Millisecond
	rankWalkHook = func(w *planLegWork) {
		if w.i == 0 {
			time.Sleep(600 * time.Millisecond) // every walk of leg 1 takes more than its share
		}
	}
	t.Cleanup(func() { rankWalkHook, planRankingBudget = old, oldBudget })
	legs := r.assess(planAssessOptions{Comfort: true}).Legs
	if len(legs) != 3 {
		t.Fatalf("%d legs", len(legs))
	}
	for i, l := range legs[1:] {
		if got := rkBasis(t, l); !strings.HasPrefix(got, "Chosen for comfort: ") || strings.Contains(got, "not judged") {
			t.Errorf("leg %d basis = %q, want it ranked in full", i+2, got)
		}
	}
}

func TestRankKey_OrdersByBandThenHoursThenSteepnessAndSaysWhyNot(t *testing.T) {
	key := func(hs ...judgedHour) comfortRankKey {
		t.Helper()
		k, why := rankKeyOf(hs)
		if why != "" {
			t.Fatalf("not ranked: %s", why)
		}
		return k
	}
	calm := key(rkHour(bandComfortable, 5))
	lively1 := key(rkHour(bandLively, 1), rkHour(bandComfortable, 4))
	lively3 := key(rkHour(bandLively, 3), rkHour(bandComfortable, 2))
	above := rkHour(bandLively, 1)
	above.aboveRated = true
	aboveKey := key(above, rkHour(bandComfortable, 4))
	rough := key(rkHour(bandRough, 1), rkHour(bandComfortable, 4))
	order := []comfortRankKey{calm, lively1, lively3, aboveKey, rough}
	for i := range order {
		for j := range order {
			if got, want := order[i].before(order[j]), i < j; got != want {
				t.Errorf("key %d before key %d = %v, want %v", i, j, got, want)
			}
		}
	}
	// With band and hours equal, the one with no steeper-than-recorded hour goes first.
	steep := rkHour(bandLively, 1)
	steep.beyond = true
	steepKey := key(steep, rkHour(bandComfortable, 4))
	if !lively1.before(steepKey) || steepKey.before(lively1) {
		t.Errorf("a steeper-than-recorded hour must lose an otherwise equal key: %+v vs %+v", lively1, steepKey)
	}
	// Comfortable counts no hours: a Comfortable leg is Comfortable however long.
	if a, b := key(rkHour(bandComfortable, 5)), key(rkHour(bandComfortable, 4)); a.before(b) || b.before(a) {
		t.Errorf("two Comfortable legs differ: %+v %+v", a, b)
	}
	// A berth ashore is ignored, and so is a gap at an end; a gap in the middle, or no
	// judged hour at all, is not ranked.
	ashore := judgedHour{start: paAt(14, 8, 0), end: paAt(14, 9, 0), reason: comfortReasonAshore}
	if _, why := rankKeyOf([]judgedHour{ashore, rkHour(bandComfortable, 4)}); why != "" {
		t.Errorf("a berth ashore left the leg unranked: %s", why)
	}
	gap := judgedHour{start: paAt(14, 8, 0), end: paAt(14, 9, 0), reason: comfortSwellEnclosed}
	if _, why := rankKeyOf([]judgedHour{rkHour(bandComfortable, 2), gap, rkHour(bandComfortable, 2)}); why != comfortSwellEnclosed {
		t.Errorf("a gap's reason = %q", why)
	}
	if _, why := rankKeyOf([]judgedHour{ashore}); why == "" {
		t.Error("a leg with no judged hour was ranked")
	}
}

// 8
func TestRankComfort_WhenComfortCannotRankTheCurrentChoiceStandsWithTheReason(t *testing.T) {
	notLearned := learnComfortBands([]ratedFelt{{ratingComfortable, 0.5}, {ratingLively, 2}})
	cases := []struct {
		name string
		cfg  rkCfg
		// want is the whole basis, or, when it is a fragment, what the basis holds.
		want     string
		fragment bool
	}{
		{"bands not learned", rkCfg{bands: &notLearned}, "Not chosen for comfort: Comfort not learned yet. Fairest stream.", false},
		{"model held back", rkCfg{set: lcUnlearned("Needs 8 passages with motion and sea, has 2")}, "Not chosen for comfort: Needs 8 passages with motion and sea, has 2. Fairest stream.", false},
		{"no plugin label", rkCfg{at: func(int) rkSpec { return rkSpec{swellM: 0.2, windKn: 10, noLabel: true} }}, "doesn't name its wave model", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.cfg.cands = []time.Time{rkAt(8), rkAt(9)}
			if c.cfg.at == nil {
				c.cfg.at = rkLively(8)
			}
			r := rkSetup(t, c.cfg)
			l := r.assess(planAssessOptions{Comfort: true}).Legs[0]
			rkWantDepart(t, l, rkAt(8))
			got := rkBasis(t, l)
			ok := got == c.want
			if c.fragment {
				ok = strings.HasPrefix(got, "Not chosen for comfort: ") && strings.HasSuffix(got, " Fairest stream.") && strings.Contains(got, c.want)
			}
			if !ok {
				t.Errorf("basis = %q, want %q", got, c.want)
			}
		})
	}
	// A reader that fails is the reason.
	r := rkSetup(t, rkCfg{at: rkLively(8), cands: []time.Time{rkAt(8), rkAt(9)}})
	r.d.comfort.model = func() (*comfortModelSet, error) { return nil, errPassageStoreNotRunning }
	l := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	rkWantDepart(t, l, rkAt(8))
	if got := rkBasis(t, l); !strings.HasPrefix(got, "Not chosen for comfort: The comfort model couldn't be read") {
		t.Errorf("failing model reader: %q", got)
	}
}

// 9
func TestRankComfort_WithComfortOffOrNoDepsTheDepartureIsTodaysAndTheBasisIsNull(t *testing.T) {
	r := rkSetup(t, rkCfg{at: rkLively(8), cands: []time.Time{rkAt(8), rkAt(9)}})
	plain := r.assess(planAssessOptions{}).Legs[0]
	verdicts := r.assess(planAssessOptions{VerdictsOnly: true, Comfort: true}).Legs[0]
	r.d.comfort = nil
	nilDeps := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	for name, l := range map[string]planLegAssessment{"comfort off": plain, "verdicts only": verdicts, "nil deps": nilDeps} {
		rkWantDepart(t, l, rkAt(8))
		if l.DepartBasis != nil {
			t.Errorf("%s: basis = %q", name, *l.DepartBasis)
		}
	}
	// An operator's own time is not ranked, and carries no basis.
	r = rkSetup(t, rkCfg{at: rkLively(8), cands: []time.Time{rkAt(8), rkAt(9)}})
	r.p.Stops[0].DepartTime = "08:00"
	set := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	if set.DepartSuggested || set.DepartBasis != nil {
		t.Errorf("operator's departure: %+v", set)
	}
	if got := paParse(t, set.Depart); !got.Equal(rkAt(8)) {
		t.Errorf("depart = %v", got)
	}
}

// 10
func TestRankComfort_WhenTheBudgetRunsOutTheChoiceIsCurrentOrderAndTheVerdictStands(t *testing.T) {
	r := rkSetup(t, rkCfg{at: rkLively(8), cands: []time.Time{rkAt(8), rkAt(9)}})
	plain := r.assess(planAssessOptions{}).Legs[0]
	old := planRankingBudget
	planRankingBudget = time.Nanosecond
	t.Cleanup(func() { planRankingBudget = old })
	l := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	rkWantDepart(t, l, rkAt(8))
	if got, want := rkBasis(t, l), "Not chosen for comfort: The comfort check ran out of time. Fairest stream."; got != want {
		t.Errorf("basis = %q, want %q", got, want)
	}
	if l.Verdict != plain.Verdict || l.Limiting != plain.Limiting || !equalJSON(t, l.WindChanges, plain.WindChanges) {
		t.Errorf("a ranking that ran out of time changed the verdict or the wind changes:\n%+v\n%+v", l, plain)
	}
}

// 11
func TestRankComfort_TheVerdictIsTheSameWithRankingOnAndOff(t *testing.T) {
	for name, at := range map[string]func(int) rkSpec{
		"go":       rkLively(8),
		"marginal": func(h int) rkSpec { return rkSpec{swellM: 0.2 + float64(h%2), windKn: 16} },
	} {
		r := rkSetup(t, rkCfg{at: at, cands: []time.Time{rkAt(8), rkAt(9), rkAt(10)}})
		on := r.assess(planAssessOptions{Comfort: true}).Legs[0]
		off := r.assess(planAssessOptions{}).Legs[0]
		if on.Verdict != off.Verdict || on.Verdict == "" || on.Note != off.Note {
			t.Errorf("%s: verdict on %q (%s), off %q (%s)", name, on.Verdict, on.Note, off.Verdict, off.Note)
		}
	}
}

// 12
func TestRankComfort_TheChipIsTheRankingsHoursWithNoSecondWalk(t *testing.T) {
	cands := []time.Time{rkAt(8), rkAt(9), rkAt(10)}
	r := rkSetup(t, rkCfg{at: rkLively(8, 9), cands: cands})
	l := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	rkWantDepart(t, l, rkAt(10))
	if got, want := int(r.predicts.Load()), len(cands)*5; got != want {
		t.Errorf("the model was asked %d times, want %d: each candidate's five hours once, and no walk for the chip", got, want)
	}
	// The chip is what an operator-set departure at that time reads.
	r2 := rkSetup(t, rkCfg{at: rkLively(8, 9), cands: cands})
	r2.p.Stops[0].DepartTime = "10:00"
	set := r2.assess(planAssessOptions{Comfort: true}).Legs[0]
	if l.Comfort == nil || !equalJSON(t, l.Comfort, set.Comfort) {
		t.Errorf("chip\n%+v\nwant\n%+v", l.Comfort, set.Comfort)
	}
}

// 13
func TestRankComfort_TheModelIsReadOnceForTheRequest(t *testing.T) {
	r := rkSetup(t, rkCfg{at: rkLively(8), cands: []time.Time{rkAt(8), rkAt(9)}, nights: []int{0, 1, 0}})
	got := r.assess(planAssessOptions{Comfort: true})
	if len(got.Legs) != 2 || got.Legs[0].DepartBasis == nil || got.Legs[1].DepartBasis == nil {
		t.Fatalf("both legs should be ranked: %+v", got.Legs)
	}
	if n := r.models.Load(); n != 1 {
		t.Errorf("the model was read %d times, want once for the request", n)
	}
}

// 13b: what the shore said is kept between requests, so asking again builds nothing.
func TestRankComfort_AWarmThreeLegRankingBuildsNoGeometry(t *testing.T) {
	r := rkSetup(t, rkCfg{at: rkLively(9), cands: []time.Time{rkAt(8), rkAt(9), rkAt(10)}, nights: []int{0, 0, 0, 0}})
	first := r.assess(planAssessOptions{Comfort: true})
	if len(first.Legs) != 3 {
		t.Fatalf("%d legs", len(first.Legs))
	}
	for i, l := range first.Legs {
		if l.DepartBasis == nil || !strings.HasPrefix(*l.DepartBasis, "Chosen for comfort: ") {
			t.Fatalf("leg %d is not ranked: %+v", i, l.DepartBasis)
		}
	}
	memo := r.d.comfort.memo
	cold := memo.builtCount()
	if cold == 0 {
		t.Fatal("the cold run built no geometry, so the test proves nothing")
	}
	second := r.assess(planAssessOptions{Comfort: true})
	if got := memo.builtCount(); got != cold {
		t.Errorf("the warm run built %d geometries", got-cold)
	}
	if !equalJSON(t, first.Legs, second.Legs) {
		t.Error("the warm run chose differently")
	}
}

// rkGapsLively is a forecast with no swell split (so the hour is not judged) in the
// hours given, Lively in the others given, and calm in the rest.
func rkGapsLively(gaps, lively []int) func(int) rkSpec {
	return func(h int) rkSpec {
		if slices.Contains(gaps, h) {
			return rkSpec{swellM: 0.2, windKn: 10, noSplit: true}
		}
		if slices.Contains(lively, h) {
			return rkSpec{swellM: 1, windKn: 10}
		}
		return rkCalm
	}
}

// 15: the ends of a leg are harbour, and not judged there says nothing about the sea.
func TestRankComfort_NotJudgedHoursAtBothEndsOfEveryCandidateStillRank(t *testing.T) {
	// 8 AM leg: hours 8 and 12 are not judged; 11 AM leg: hours 11 and 15. 8 AM is
	// Lively at 9, so the 11 AM departure (calm between its ends) beats it.
	at := rkGapsLively([]int{8, 11, 12, 15}, []int{9})
	r := rkSetup(t, rkCfg{at: at, cands: []time.Time{rkAt(8), rkAt(11)}})
	l := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	rkWantDepart(t, l, rkAt(11))
	// No split is not the shore's reason, so the ends are named, not called harbour.
	const noSplit = "the wave plugin gave no swell and wind-wave split for this hour, so comfort can't be judged."
	if got, want := rkBasis(t, l), "Chosen for comfort: Comfortable throughout instead of Lively for 1 h. Not judged for the first 2 h: "+noSplit+" Not judged for the last 1 h: "+noSplit; got != want {
		t.Errorf("basis = %q, want %q", got, want)
	}
	// The chip still shows the hours as not judged.
	if l.Comfort == nil || !strings.Contains(l.Comfort.Hours[0].Line, "Not judged") {
		t.Errorf("the chip hides the end hours: %+v", l.Comfort)
	}
}

// 16
func TestRankComfort_AStartOnlyOrEndOnlyGapIsIgnoredAndTheBasisSaysWhich(t *testing.T) {
	cases := []struct {
		name  string
		at    func(int) rkSpec
		cands []time.Time
		want  string
	}{
		{"start only", rkGapsLively([]int{8, 12}, []int{9}), []time.Time{rkAt(8), rkAt(12)},
			"Chosen for comfort: Comfortable throughout instead of Lively for 1 h. Not judged for the first 1 h: the wave plugin gave no swell and wind-wave split for this hour, so comfort can't be judged."},
		{"end only", rkGapsLively([]int{12, 13}, []int{8}), []time.Time{rkAt(8), rkAt(9)},
			"Chosen for comfort: Comfortable throughout instead of Lively for 1 h. Not judged for the last 2 h: the wave plugin gave no swell and wind-wave split for this hour, so comfort can't be judged."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := rkSetup(t, rkCfg{at: c.at, cands: c.cands})
			l := r.assess(planAssessOptions{Comfort: true}).Legs[0]
			rkWantDepart(t, l, c.cands[1])
			if got := rkBasis(t, l); got != c.want {
				t.Errorf("basis = %q, want %q", got, c.want)
			}
		})
	}
}

// 17
func TestRankComfort_AGapInTheMiddleOfTheLegStillLeavesTheCandidateUnranked(t *testing.T) {
	// Hour 10 is not judged: mid-leg for the 8 AM departure (the fairer current), the
	// first hour of nothing else. The 12 PM departure is fully judged, though Lively.
	r := rkSetup(t, rkCfg{at: rkGapsLively([]int{10}, []int{13}), cands: []time.Time{rkAt(8), rkAt(12)}})
	l := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	rkWantDepart(t, l, rkAt(12))
	if got, want := rkBasis(t, l), "Chosen for comfort: Lively for 1 h. 1 of 2 times not judged: the wave plugin gave no swell and wind-wave split for this hour, so comfort can't be judged."; got != want {
		t.Errorf("basis = %q, want %q", got, want)
	}
}

// 17b: only the berth is ignored when it is charted ashore. A position ashore in the
// middle of the leg is a gap like any other.
func TestRankComfort_APositionChartedAshoreMidLegLeavesTheCandidateUnranked(t *testing.T) {
	p, _ := paPlan(t, "2026-11-14", 0, 0)
	// An island over the leg's two-fifths point: the 8 AM departure is over it at 10 AM.
	// The 8.30 AM departure passes at the tenths, clear of it, and is Lively for an hour.
	r := rkSetup(t, rkCfg{at: rkLively(11), cands: []time.Time{rkAt(8), paAt(14, 8, 30)}, lines: []shorelineLine{rkIsland(p, 0.4)}})
	l := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	rkWantDepart(t, l, paAt(14, 8, 30))
	if got, want := rkBasis(t, l), "Chosen for comfort: Lively for 1 h. 1 of 2 times not judged: position charted ashore."; got != want {
		t.Errorf("basis = %q, want %q", got, want)
	}
}

// 18
func TestRankComfort_AllHoursNotJudgedLeavesEveryCandidateUnranked(t *testing.T) {
	r := rkSetup(t, rkCfg{at: func(int) rkSpec { return rkSpec{swellM: 0.2, windKn: 10, noSplit: true} }, cands: []time.Time{rkAt(8), rkAt(9)}})
	l := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	rkWantDepart(t, l, rkAt(8))
	if got := rkBasis(t, l); !strings.HasPrefix(got, "Not chosen for comfort: ") || !strings.HasSuffix(got, " Fairest stream.") {
		t.Errorf("basis = %q", got)
	}
}

// 19
func TestRankKey_EndGapsAreIgnoredButOutOfTimeAndMidGapsAreNot(t *testing.T) {
	enclosed := func(h float64) judgedHour {
		return judgedHour{start: paAt(14, 8, 0), end: paAt(14, 8, 0).Add(hoursDuration(h)), reason: comfortSwellEnclosed}
	}
	calm := rkHour(bandComfortable, 3)
	k, why := rankKeyOf([]judgedHour{enclosed(1), calm, enclosed(1)})
	if why != "" || !k.startUnjudged || !k.endUnjudged {
		t.Errorf("both ends: key %+v, why %q", k, why)
	}
	if k, why = rankKeyOf([]judgedHour{enclosed(1), calm}); why != "" || !k.startUnjudged || k.endUnjudged {
		t.Errorf("start only: key %+v, why %q", k, why)
	}
	if k, why = rankKeyOf([]judgedHour{calm, enclosed(1)}); why != "" || k.startUnjudged || !k.endUnjudged {
		t.Errorf("end only: key %+v, why %q", k, why)
	}
	if _, why = rankKeyOf([]judgedHour{enclosed(1), calm, enclosed(1), calm}); why != comfortSwellEnclosed {
		t.Errorf("mid-leg gap: why %q", why)
	}
	if _, why = rankKeyOf([]judgedHour{enclosed(1), enclosed(1)}); why == "" {
		t.Error("all hours not judged was ranked")
	}
	// A run that ran out of time is not a harbour end, even when it is at the end.
	late := judgedHour{start: paAt(14, 8, 0), end: paAt(14, 9, 0), reason: comfortReasonOutOfTime}
	if _, why = rankKeyOf([]judgedHour{calm, late}); why != comfortReasonOutOfTime {
		t.Errorf("out of time at the end: why %q", why)
	}
	// A berth ashore at an end is ignored without a harbour line.
	ashore := judgedHour{start: paAt(14, 8, 0), end: paAt(14, 9, 0), reason: comfortReasonAshore}
	if k, why = rankKeyOf([]judgedHour{ashore, calm}); why != "" || k.startUnjudged {
		t.Errorf("ashore start: key %+v, why %q", k, why)
	}
	if k, why = rankKeyOf([]judgedHour{calm, ashore}); why != "" || k.endUnjudged {
		t.Errorf("ashore end: key %+v, why %q", k, why)
	}
	// Ashore with another gap at the same end still says the harbour was not judged.
	if k, why = rankKeyOf([]judgedHour{ashore, enclosed(1), calm}); why != "" || !k.startUnjudged {
		t.Errorf("ashore then enclosed at the start: key %+v, why %q", k, why)
	}
	// Ashore in the middle is a gap like any other.
	if _, why = rankKeyOf([]judgedHour{calm, ashore, calm}); why != comfortReasonAshore {
		t.Errorf("ashore mid-leg: why %q", why)
	}
}

// A block found in one candidate's own hours (here a wave model with no fit, in an hour
// only the last candidate flies over) leaves that candidate unranked and ranking goes on.
func TestRankComfort_ABlockInOneCandidatesHoursLeavesOnlyThatCandidateUnranked(t *testing.T) {
	at := func(h int) rkSpec {
		if h == 14 {
			return rkSpec{swellM: 0.2, windKn: 10, otherModel: true}
		}
		return rkCalm
	}
	r := rkSetup(t, rkCfg{at: at, cands: []time.Time{rkAt(8), rkAt(9), rkAt(10)}})
	l := r.assess(planAssessOptions{Comfort: true}).Legs[0]
	rkWantDepart(t, l, rkAt(8))
	got := rkBasis(t, l)
	if !strings.HasPrefix(got, "Chosen for comfort: Comfortable throughout, then the fairest stream of 2 alike.") ||
		!strings.Contains(got, " 1 of 3 times not judged: ") || !strings.Contains(got, "was learned on test_wave") {
		t.Errorf("basis = %q", got)
	}
}

// With ranking on, a request that ran out of time on a candidate after a Go stops the
// loop: the candidates it never reached are not judged for lack of time, and none is
// logged as a forecast that could not be read.
func TestChooseDeparture_ARunOutOfTimeStopsTheLoopAndLogsNothing(t *testing.T) {
	var buf bytes.Buffer
	oldOut := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(oldOut) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	old := departureCell
	departureCell = func(ctx context.Context, as *passageAssessor, w *planLegWork, c tidalDeparture) (passageWindowCell, error) {
		calls++
		if calls == 3 {
			cancel()
			return passageWindowCell{}, ctx.Err()
		}
		return passageWindowCell{State: "go"}, nil
	}
	t.Cleanup(func() { departureCell = old })
	w := &planLegWork{
		cands:   []tidalDeparture{{Depart: rkAt(8)}, {Depart: rkAt(9)}, {Depart: rkAt(10)}, {Depart: rkAt(11)}},
		ranking: &departureRanking{setup: &comfortSetup{}},
	}
	ch, err := chooseDeparture(ctx, nil, w)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(ch.cells) != 2 || ch.unreached != 2 {
		t.Errorf("calls %d, cells %d, unreached %d; want 3, 2, 2", calls, len(ch.cells), ch.unreached)
	}
	if buf.Len() != 0 {
		t.Errorf("logged: %s", buf.String())
	}
}

// The same through a whole assessment: the verdict stands and the basis blames time.
func TestRankComfort_ARequestThatRanOutOfTimeAfterAGoKeepsTheVerdict(t *testing.T) {
	r := rkSetup(t, rkCfg{at: rkLively(8), cands: []time.Time{rkAt(8), rkAt(9), rkAt(10)}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	old := departureCell
	departureCell = func(ctx context.Context, as *passageAssessor, w *planLegWork, c tidalDeparture) (passageWindowCell, error) {
		if calls++; calls == 3 {
			cancel()
			return passageWindowCell{}, ctx.Err()
		}
		return old(ctx, as, w, c)
	}
	t.Cleanup(func() { departureCell = old })
	l := assessPlan(ctx, r.p, r.legs, r.d, planAssessOptions{Comfort: true}).Legs[0]
	if l.Verdict != "go" {
		t.Fatalf("verdict = %q (%s)", l.Verdict, l.Note)
	}
	if got, want := rkBasis(t, l), "Not chosen for comfort: The comfort check ran out of time. Fairest stream."; got != want {
		t.Errorf("basis = %q, want %q", got, want)
	}
}

// An out-of-time sliver at the end must not vanish into an ashore end stretch.
func TestRankKey_AnOutOfTimeSliverBesideAnAshoreEndIsNotRanked(t *testing.T) {
	calm := rkHour(bandComfortable, 3)
	at := paAt(14, 11, 0)
	ashore := judgedHour{start: at, end: at.Add(time.Hour), reason: comfortReasonAshore}
	late := judgedHour{start: at.Add(time.Hour), end: at.Add(time.Hour + 5*time.Minute), reason: comfortReasonOutOfTime}
	if _, why := rankKeyOf([]judgedHour{calm, ashore, late}); why != comfortReasonOutOfTime {
		t.Errorf("why = %q, want out of time", why)
	}
}

// An end gap that is geography keeps the harbour wording; any other reason is named,
// with the hours.
func TestRankedBasis_ANonGeographicEndGapNamesItsReasonAndHours(t *testing.T) {
	calm := rkHour(bandComfortable, 3)
	at := paAt(14, 11, 0)
	gap := func(reason string, h float64) judgedHour {
		return judgedHour{start: at, end: at.Add(hoursDuration(h)), reason: reason}
	}
	basis := func(hours ...judgedHour) string {
		t.Helper()
		k, why := rankKeyOf(hours)
		if why != "" {
			t.Fatalf("not ranked: %s", why)
		}
		return rankedBasis(k, nil, 1, 0, 1, "")
	}
	for _, reason := range []string{comfortSwellEnclosed, "wind wave not checked against fetch: no coastline"} {
		if got, want := basis(gap(reason, 1), calm, gap(reason, 1)), "Chosen for comfort: Comfortable throughout. Not judged leaving and entering harbour."; got != want {
			t.Errorf("%s: basis = %q, want %q", reason, got, want)
		}
	}
	if got, want := basis(calm, gap(comfortReasonNoHour, 2)), "Chosen for comfort: Comfortable throughout. Not judged for the last 2 h: the forecast has no figure for this hour, so comfort can't be judged."; got != want {
		t.Errorf("end: basis = %q, want %q", got, want)
	}
	if got, want := basis(gap(comfortReasonNoHour, 1), calm, gap(comfortSwellEnclosed, 1)), "Chosen for comfort: Comfortable throughout. Not judged for the first 1 h: the forecast has no figure for this hour, so comfort can't be judged. Not judged entering harbour."; got != want {
		t.Errorf("mixed: basis = %q, want %q", got, want)
	}
}

// A reason that starts with an acronym keeps its capitals.
func TestRankBasis_ReasonsKeepAnAcronymsCapitals(t *testing.T) {
	why := "OSM square 12 is not downloaded"
	k, _ := rankKeyOf([]judgedHour{rkHour(bandComfortable, 3)})
	if got := rankedBasis(k, nil, 1, 1, 2, why); !strings.Contains(got, "1 of 2 times not judged: OSM square 12 is not downloaded.") {
		t.Errorf("basis = %q", got)
	}
	if got, want := currentOnlyBasis(why), "Not chosen for comfort: OSM square 12 is not downloaded. Fairest stream."; got != want {
		t.Errorf("basis = %q, want %q", got, want)
	}
	if got := reasonClause("Wave plugin gave no figure."); got != "wave plugin gave no figure" {
		t.Errorf("reasonClause = %q", got)
	}
}

// 14
func TestPlanReportLeg_SaysGetPlansDoesNotRankForComfort(t *testing.T) {
	p, _ := paPlan(t, "2026-11-14", 0, 0)
	for _, verdict := range []string{"go", "marginal"} {
		suggested, err := planReportLegOf(p, planLegAssessment{Date: "2026-11-14", DepartSuggested: true, Verdict: verdict}, planLeg{})
		if err != nil {
			t.Fatal(err)
		}
		if suggested.DepartBasis != planReportDepartBasis {
			t.Errorf("a suggested %s departure's basis = %q, want %q", verdict, suggested.DepartBasis, planReportDepartBasis)
		}
	}
	// A leg with no Go or Marginal time was chosen closest to its limit, not by the
	// fairest current.
	for _, verdict := range []string{"no_go", "no_data", ""} {
		l, err := planReportLegOf(p, planLegAssessment{Date: "2026-11-14", DepartSuggested: true, Verdict: verdict}, planLeg{})
		if err != nil {
			t.Fatal(err)
		}
		if l.DepartBasis != "" {
			t.Errorf("a %q leg's basis = %q, want none", verdict, l.DepartBasis)
		}
	}
	set, err := planReportLegOf(p, planLegAssessment{Date: "2026-11-14"}, planLeg{})
	if err != nil {
		t.Fatal(err)
	}
	if set.DepartBasis != "" {
		t.Errorf("the operator's departure has a basis: %q", set.DepartBasis)
	}
}
