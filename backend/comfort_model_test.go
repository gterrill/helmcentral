package main

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// ── the comfort model (ADR 0301) ─────────────────────────────────────────

// The truth the synthetic record is drawn from: variance is an intercept plus a
// share of each wave system's squared feature, as the fit assumes.
type truthCoef struct{ a, bs, bw float64 }

var (
	truthRoll     = truthCoef{0.8, 6000, 3000}
	truthPitch    = truthCoef{0.3, 2000, 4000}
	truthSnap     = truthCoef{25, 1e7, 4e6}
	truthPounding = truthCoef{0.002, 3, 5}
)

func (c truthCoef) value(t comfortTarget, sp comfortSpell) float64 {
	xs := comfortFeature(t, sp.Swell, sp.HeadingDeg)
	xw := comfortFeature(t, sp.WindWave, sp.HeadingDeg)
	return math.Sqrt(c.a + c.bs*xs*xs + c.bw*xw*xw)
}

// synthSpells is passages x per spells in the given model, from one seed. The
// figures follow the truth exactly, so a fit that works recovers them.
func synthSpells(model string, passages, per int, tenHz bool, seed int64) []comfortSpell {
	r := rand.New(rand.NewSource(seed))
	var out []comfortSpell
	for p := 0; p < passages; p++ {
		for i := 0; i < per; i++ {
			sp := comfortSpell{
				Passage:    fmt.Sprintf("%s-p%d", model, p),
				Model:      model,
				HeadingDeg: r.Float64() * 360,
				Swell:      &seaTrainFigures{HeightM: 0.3 + r.Float64()*2.2, PeriodS: 5 + r.Float64()*7, FromDeg: r.Float64() * 360},
				WindWave:   &seaTrainFigures{HeightM: 0.1 + r.Float64()*1.4, PeriodS: 3 + r.Float64()*3, FromDeg: r.Float64() * 360},
			}
			sp.RollRMSDeg = truthRoll.value(targetRoll, sp)
			sp.PitchRMSDeg = truthPitch.value(targetPitch, sp)
			if tenHz {
				snap := truthSnap.value(targetSnap, sp)
				pound := truthPounding.value(targetPounding, sp)
				sp.SnapDps2, sp.PoundingG = &snap, &pound
			}
			out = append(out, sp)
		}
	}
	return out
}

func closeTo(t *testing.T, what string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %g, want %g (tolerance %g)", what, got, want, tol)
	}
}

func TestTheFeaturesAreSteepnessAndTheAngleToTheHeading(t *testing.T) {
	// H 1 m, T 8 s: L = gT^2 / 2pi = 99.9 m, steepness pi H / L = 0.0315.
	tr := &seaTrainFigures{HeightM: 1, PeriodS: 8, FromDeg: 90}
	s := math.Pi * 1 / (9.80665 * 64 / (2 * math.Pi))
	closeTo(t, "roll, beam sea", comfortFeature(targetRoll, tr, 0), s, 1e-6)
	closeTo(t, "pitch, beam sea", comfortFeature(targetPitch, tr, 0), 0, 1e-9)
	closeTo(t, "pitch, head sea", comfortFeature(targetPitch, tr, 90), s, 1e-6)
	omega := 2 * math.Pi / 8
	closeTo(t, "snap, beam sea", comfortFeature(targetSnap, tr, 0), s*omega*omega, 1e-6)
	closeTo(t, "pounding, head sea", comfortFeature(targetPounding, tr, 90), s, 1e-6)
	closeTo(t, "pounding, following sea", comfortFeature(targetPounding, tr, 270), 0, 1e-9)
	closeTo(t, "no train", comfortFeature(targetRoll, nil, 0), 0, 0)
}

// ── which spells train a fit ─────────────────────────────────────────────

func usableSea() spellSea {
	return spellSea{
		Swell:    &seaTrain{seaTrainFigures: seaTrainFigures{HeightM: 1, PeriodS: 8, FromDeg: 90}},
		WindWave: &seaTrain{seaTrainFigures: seaTrainFigures{HeightM: 0.5, PeriodS: 4, FromDeg: 120}},
	}
}

func comfortFeltSpell(h *float64) feltSpell {
	var sp feltSpell
	sp.MeanHeadingDeg = h
	sp.RollRMSDeg, sp.PitchRMSDeg = 2, 1
	return sp
}

func TestASpellWithASeaAndAHeadingTrains(t *testing.T) {
	cs, why := comfortSpellFrom("p1", comfortFeltSpell(pfloat(10)), usableSea(), "gfs")
	if why != "" {
		t.Fatalf("excluded: %s", why)
	}
	if cs.Model != "gfs" || cs.Passage != "p1" || cs.HeadingDeg != 10 || cs.Swell == nil || cs.WindWave == nil {
		t.Errorf("spell: %+v", cs)
	}
}

func TestAFlatSeaTrainsTheIntercept(t *testing.T) {
	cs, why := comfortSpellFrom("p1", comfortFeltSpell(pfloat(10)), spellSea{}, "gfs")
	if why != "" || cs.Swell != nil || cs.WindWave != nil {
		t.Fatalf("a sea with neither system is flat and usable: %+v %q", cs, why)
	}
	var rows []comfortSpell
	for p := 0; p < 3; p++ {
		for i := 0; i < 4; i++ {
			rows = append(rows, comfortSpell{Passage: fmt.Sprintf("p%d", p), Model: "gfs", RollRMSDeg: 1.5, PitchRMSDeg: 0.5})
		}
	}
	fit := fitTarget(groupByPassage(rows), targetRoll, func(s comfortSpell) float64 { return s.RollRMSDeg })
	if fit == nil {
		t.Fatal("no fit from flat spells")
	}
	closeTo(t, "intercept", fit.Coef.A, 2.25, 1e-6)
	closeTo(t, "flat prediction", fit.Coef.predict(0, 0), 1.5, 1e-6)
}

func TestSpellsTheModelCannotJudgeAreLeftOut(t *testing.T) {
	enclosed := usableSea()
	enclosed.Swell.Enclosed = true
	unchecked := usableSea()
	unchecked.Swell.Unchecked = true
	windUnchecked := usableSea()
	windUnchecked.WindWave.Fetch = &fetchCheck{Check: seaCheckUnchecked, Reason: "no wind"}
	noSea := spellSea{Reason: "The sea state for this hour hasn't been fetched yet."}
	cases := []struct {
		name    string
		sp      feltSpell
		sea     spellSea
		wantWhy string
	}{
		{"enclosed swell", comfortFeltSpell(pfloat(0)), enclosed, "enclosed"},
		{"unchecked swell", comfortFeltSpell(pfloat(0)), unchecked, "swell"},
		{"unchecked wind wave", comfortFeltSpell(pfloat(0)), windUnchecked, "wind wave"},
		{"no heading", comfortFeltSpell(nil), usableSea(), "heading"},
		{"no sea state", comfortFeltSpell(pfloat(0)), noSea, "sea state"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, why := comfortSpellFrom("p1", c.sp, c.sea, "gfs")
			if why == "" || !strings.Contains(why, c.wantWhy) {
				t.Errorf("reason %q, want it to mention %q", why, c.wantWhy)
			}
		})
	}
}

func TestACappedWindWaveTrainsOnTheCappedFigures(t *testing.T) {
	sea := usableSea()
	sea.WindWave.HeightM = 0.4 // the cap, already applied by the check
	sea.WindWave.Fetch = &fetchCheck{Check: seaCheckCapped, HindcastHeightM: pfloat(1.5)}
	cs, why := comfortSpellFrom("p1", comfortFeltSpell(pfloat(0)), sea, "gfs")
	if why != "" || cs.WindWave == nil || cs.WindWave.HeightM != 0.4 {
		t.Fatalf("capped wind wave: %+v %q", cs.WindWave, why)
	}
}

func TestSnapAndPoundingTrainOnlyWhereTenHertzDataExists(t *testing.T) {
	sp := comfortFeltSpell(pfloat(0))
	sp.RollAccelPeakDps2, sp.VertAccelPeakG = pfloat(40), pfloat(0.2)
	cs, _ := comfortSpellFrom("p1", sp, usableSea(), "gfs")
	if cs.SnapDps2 == nil || *cs.SnapDps2 != 40 || cs.PoundingG == nil || *cs.PoundingG != 0.2 {
		t.Errorf("10 Hz figures: %+v", cs)
	}
	cs, _ = comfortSpellFrom("p1", comfortFeltSpell(pfloat(0)), usableSea(), "gfs")
	if cs.SnapDps2 != nil || cs.PoundingG != nil {
		t.Errorf("no 10 Hz data, no snap or pounding: %+v", cs)
	}
}

// ── the variance fit ─────────────────────────────────────────────────────

func TestTheFitRecoversTheTruthAndNoCoefficientIsNegative(t *testing.T) {
	spells := synthSpells("gfs", 10, 12, false, 1)
	fit := fitTarget(groupByPassage(spells), targetRoll, func(s comfortSpell) float64 { return s.RollRMSDeg })
	closeTo(t, "a", fit.Coef.A, truthRoll.a, 0.05)
	closeTo(t, "bs", fit.Coef.Bs/truthRoll.bs, 1, 0.02)
	closeTo(t, "bw", fit.Coef.Bw/truthRoll.bw, 1, 0.02)
}

func TestACoefficientThatWouldGoNegativeIsHeldAtZero(t *testing.T) {
	// The bigger the sea, the calmer the boat: the data wants a negative slope.
	var rows []comfortSpell
	for p := 0; p < 9; p++ {
		for i := 0; i < 8; i++ {
			h := 0.3 + float64(i)*0.25
			sp := comfortSpell{Passage: fmt.Sprintf("p%d", p), Model: "gfs", HeadingDeg: 0,
				Swell: &seaTrainFigures{HeightM: h, PeriodS: 8, FromDeg: 90}}
			sp.RollRMSDeg = 4 - h
			rows = append(rows, sp)
		}
	}
	fit := fitTarget(groupByPassage(rows), targetRoll, func(s comfortSpell) float64 { return s.RollRMSDeg })
	if fit.Coef.A < 0 || fit.Coef.Bs < 0 || fit.Coef.Bw < 0 {
		t.Fatalf("coefficients must never be negative: %+v", fit.Coef)
	}
	if fit.Coef.Bs != 0 {
		t.Errorf("the swell slope should be held at zero, got %g", fit.Coef.Bs)
	}
}

func TestTwoSystemsThatMoveTogetherStillFit(t *testing.T) {
	var rows []comfortSpell
	for p := 0; p < 9; p++ {
		for i := 0; i < 8; i++ {
			h := 0.3 + float64(i)*0.2
			tr := seaTrainFigures{HeightM: h, PeriodS: 8, FromDeg: 90}
			w := tr
			sp := comfortSpell{Passage: fmt.Sprintf("p%d", p), Model: "gfs", Swell: &tr, WindWave: &w}
			x := comfortFeature(targetRoll, &tr, 0)
			sp.RollRMSDeg = math.Sqrt(1 + 8000*x*x)
			rows = append(rows, sp)
		}
	}
	fit := fitTarget(groupByPassage(rows), targetRoll, func(s comfortSpell) float64 { return s.RollRMSDeg })
	c := fit.Coef
	for _, v := range []float64{c.A, c.Bs, c.Bw} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			t.Fatalf("collinear fit gave %+v", c)
		}
	}
	x := comfortFeature(targetRoll, rows[5].Swell, 0)
	closeTo(t, "prediction", c.predict(x, x), rows[5].RollRMSDeg, 0.05)
}

// ── the leave-one-passage-out check ──────────────────────────────────────

func TestOnePassageCannotCarryTheCheck(t *testing.T) {
	// One long passage follows the truth; seven short ones are noise. Counted
	// by spells it would score well; counted by passages it must not.
	spells := synthSpells("gfs", 1, 70, false, 3)
	r := rand.New(rand.NewSource(9))
	for p := 0; p < 7; p++ {
		noise := synthSpells("gfs", 1, 2, false, int64(100+p))
		for i := range noise {
			noise[i].Passage = fmt.Sprintf("noise%d", p)
			noise[i].RollRMSDeg = 0.5 + r.Float64()*8
			noise[i].PitchRMSDeg = 0.5 + r.Float64()*8
		}
		spells = append(spells, noise...)
	}
	set := fitSpells(spells)
	fit := set.Fits["gfs"]
	if fit == nil {
		t.Fatal("no fit for the model")
	}
	if fit.Sway.Learned {
		t.Errorf("one passage carried the check: %+v", fit.Sway)
	}
}

func TestSwayNeedsEightPassagesAndSixtySpells(t *testing.T) {
	seven := fitSpells(synthSpells("gfs", 7, 10, false, 5)).Fits["gfs"]
	if seven.Sway.Learned || !strings.Contains(seven.Sway.Reason, "needs 8 passages with motion and sea, has 7") {
		t.Errorf("seven passages: %+v", seven.Sway)
	}
	eight := fitSpells(synthSpells("gfs", 8, 10, false, 5)).Fits["gfs"]
	if !eight.Sway.Learned || len(eight.Sway.Fits) != 2 {
		t.Errorf("eight passages of exact data should learn sway: %+v", eight.Sway)
	}
	few := fitSpells(synthSpells("gfs", 8, 7, false, 5)).Fits["gfs"]
	if few.Sway.Learned || !strings.Contains(few.Sway.Reason, "needs 60 spells with motion and sea, has 56") {
		t.Errorf("56 spells: %+v", few.Sway)
	}
}

func TestSnapAndPoundingNeedFivePassagesAndThirtyTenHertzSpells(t *testing.T) {
	four := fitSpells(synthSpells("gfs", 4, 10, true, 6)).Fits["gfs"]
	if four.Snap.Learned || !strings.Contains(four.Snap.Reason, "needs 5 passages with 10 Hz motion, has 4") {
		t.Errorf("four passages: %+v", four.Snap)
	}
	five := fitSpells(synthSpells("gfs", 5, 6, true, 6)).Fits["gfs"]
	if !five.Snap.Learned || !five.Pounding.Learned || five.Snap.Passages != 5 {
		t.Errorf("five passages of 6 spells: snap %+v pounding %+v", five.Snap, five.Pounding)
	}
	thin := fitSpells(synthSpells("gfs", 5, 5, true, 6)).Fits["gfs"]
	if thin.Snap.Learned || !strings.Contains(thin.Snap.Reason, "needs 30 spells with 10 Hz motion, has 25") {
		t.Errorf("25 spells: %+v", thin.Snap)
	}
}

func TestTheSkillMustReachPointThree(t *testing.T) {
	if why := skillReason(0.29, 9); why == "" || !strings.Contains(why, "skill 0.29, needs 0.30, from 9 passages") {
		t.Errorf("0.29: %q", why)
	}
	if why := skillReason(0.30, 9); why != "" {
		t.Errorf("0.30 passes, got %q", why)
	}
}

func TestANoisyRecordDoesNotLearnAndSaysHowFarItFell(t *testing.T) {
	spells := synthSpells("gfs", 10, 10, false, 7)
	r := rand.New(rand.NewSource(11))
	for i := range spells {
		spells[i].RollRMSDeg = 0.5 + r.Float64()*8
		spells[i].PitchRMSDeg = 0.5 + r.Float64()*8
	}
	fit := fitSpells(spells).Fits["gfs"]
	if fit.Sway.Learned || !strings.Contains(fit.Sway.Reason, "doesn't yet predict recorded passages better than a fixed guess") {
		t.Errorf("noise: %+v", fit.Sway)
	}
	if len(fit.Sway.Fits) != 0 {
		t.Error("a target that failed its check must not predict")
	}
}

// ── one fit for each wave model ──────────────────────────────────────────

func TestFitsAreKeptForEachWaveModel(t *testing.T) {
	spells := append(synthSpells("gfs", 8, 10, false, 8), synthSpells("blend", 8, 10, false, 9)...)
	set := fitSpells(spells)
	if len(set.Fits) != 2 || set.Fits["gfs"] == nil || set.Fits["blend"] == nil {
		t.Fatalf("fits: %v", set.Fits)
	}
	if set.Fits["gfs"].Sway.Passages != 8 || set.Fits["blend"].Sway.Passages != 8 {
		t.Errorf("each model counts only its own passages: %+v %+v", set.Fits["gfs"].Sway, set.Fits["blend"].Sway)
	}
}

// ── predicting ───────────────────────────────────────────────────────────

func learnedSet(t *testing.T) *comfortModelSet {
	t.Helper()
	set := fitSpells(synthSpells("gfs", 8, 10, true, 12))
	f := set.Fits["gfs"]
	if f == nil || !f.Sway.Learned || !f.Snap.Learned || !f.Pounding.Learned {
		t.Fatalf("not learned: %+v", f)
	}
	return set
}

func TestPredictAnswersWithinTheRecord(t *testing.T) {
	set := learnedSet(t)
	truth := synthSpells("gfs", 1, 1, true, 12)[0]
	in := comfortHourInput{Model: gfsRef, HeadingDeg: truth.HeadingDeg, Swell: truth.Swell, WindWave: truth.WindWave}
	p := set.predict(in)
	if p.Sway.Value == nil || p.Snap.Value == nil || p.Pounding.Value == nil || p.RollDeg == nil || p.PitchDeg == nil {
		t.Fatalf("prediction: %+v", p)
	}
	closeTo(t, "sway", *p.Sway.Value, math.Hypot(truth.RollRMSDeg, truth.PitchRMSDeg), 0.05)
	closeTo(t, "snap", *p.Snap.Value, *truth.SnapDps2, 0.5)
	if p.Sway.Beyond || p.Snap.Beyond || p.Pounding.Beyond {
		t.Errorf("a point inside the record is not beyond it: %+v", p)
	}
}

// gfsRef is the wave model the synthetic record was made under.
var gfsRef = waveModelRef{ID: "gfs", Label: "GFS-Wave"}

func TestPredictForAModelWithNoFitGivesTheMismatchReason(t *testing.T) {
	set := learnedSet(t)
	blend := waveModelRef{ID: "best_match", Label: "The Open-Meteo best-match blend"}
	p := set.predict(comfortHourInput{Model: blend, Swell: &seaTrainFigures{HeightM: 1, PeriodS: 8, FromDeg: 0}})
	if p.Sway.Value != nil || p.Snap.Value != nil || p.Pounding.Value != nil {
		t.Fatalf("no fit, no figure: %+v", p)
	}
	for _, why := range []string{p.Sway.Reason, p.Snap.Reason, p.Pounding.Reason} {
		if !strings.Contains(why, "learned on gfs") || !strings.Contains(why, lowerArticle(blend.Label)) {
			t.Errorf("reason %q should name both models", why)
		}
	}
}

func TestTheMismatchSentenceNamesTheForecastModelByItsLabel(t *testing.T) {
	set := learnedSet(t)
	tm := pwDay(20, 6).UTC()
	wb, vb := splitHour(tm, nil)
	vb.Model, vb.ModelLabel = "best_match", "The Open-Meteo best-match blend"
	in, why := comfortInputFromHour(passageHoursFromBundles(wb, vb)[tm.Unix()], 10)
	if why != "" {
		t.Fatalf("hour: %q", why)
	}
	p := set.predict(in)
	if strings.Contains(p.Sway.Reason, "best_match") || !strings.Contains(p.Sway.Reason, "this forecast comes from the Open-Meteo best-match blend.") {
		t.Errorf("the sentence must use the label and never the id: %q", p.Sway.Reason)
	}
}

func TestPredictBeforeAnyFitSaysSo(t *testing.T) {
	var none *comfortModelSet
	p := none.predict(comfortHourInput{Model: gfsRef})
	if p.Sway.Value != nil || p.Sway.Reason == "" {
		t.Errorf("no set: %+v", p)
	}
}

func TestAnInputBeyondTheRecordIsFlagged(t *testing.T) {
	set := learnedSet(t)
	in := comfortHourInput{Model: gfsRef, HeadingDeg: 0,
		Swell: &seaTrainFigures{HeightM: 9, PeriodS: 6, FromDeg: 90}} // far steeper than any recorded
	p := set.predict(in)
	if p.Sway.Value == nil || !p.Sway.Beyond {
		t.Errorf("a sea steeper than any recorded is flagged: %+v", p)
	}
}

func TestAFlatForecastIsPredictedFromTheIntercept(t *testing.T) {
	set := learnedSet(t)
	p := set.predict(comfortHourInput{Model: gfsRef})
	if p.Sway.Value == nil || p.Sway.Beyond {
		t.Fatalf("flat: %+v", p)
	}
	closeTo(t, "flat sway", *p.Sway.Value, math.Hypot(math.Sqrt(truthRoll.a), math.Sqrt(truthPitch.a)), 0.05)
}

func TestAHelmHeightChangeMovesSnapWithoutARefit(t *testing.T) {
	set := learnedSet(t)
	truth := synthSpells("gfs", 1, 1, true, 12)[0]
	p := set.predict(comfortHourInput{Model: gfsRef, HeadingDeg: truth.HeadingDeg, Swell: truth.Swell, WindWave: truth.WindWave})
	low, high := p.SnapSidewaysG(1.5), p.SnapSidewaysG(3)
	if low == nil || high == nil {
		t.Fatal("no sideways g")
	}
	closeTo(t, "ratio", *high/(*low), 2, 1e-9)
	closeTo(t, "figure", *low, sidewaysGAt(1.5, *p.Snap.Value), 1e-12)
}

func TestAnHourWithAGapIsNeverPredicted(t *testing.T) {
	tm := pwDay(20, 6).UTC()
	wb, vb := splitHour(tm, func(p *waveHourPoint) {
		p.SwellWaveHeightM, p.SwellWavePeriodS = 0, 0
		p.WindWaveHeightM, p.WindWavePeriodS = 0, 0
	})
	h := passageHoursFromBundles(wb, vb)[tm.Unix()]
	_, why := comfortInputFromHour(h, 10)
	if !strings.Contains(why, "no swell and wind-wave split") {
		t.Errorf("a missing split is a reason, never calm: %q", why)
	}
	wb, vb = splitHour(tm, nil)
	in, why := comfortInputFromHour(passageHoursFromBundles(wb, vb)[tm.Unix()], 10)
	if why != "" || in.Model != (waveModelRef{ID: "ncep_gfswave025", Label: "GFS-Wave"}) || in.Swell == nil || in.WindWave == nil || in.HeadingDeg != 10 {
		t.Errorf("a full hour: %+v %q", in, why)
	}
}

// ── what the Felt row says ───────────────────────────────────────────────

func TestTheStatusLineSaysWhatIsLearnedAndWhatIsNot(t *testing.T) {
	set := fitSpells(synthSpells("gfs", 8, 10, false, 13))
	line := set.statusLine()
	if !strings.HasPrefix(line, "Comfort model: sway learned from 8 passages (skill ") {
		t.Errorf("line: %q", line)
	}
	if !strings.Contains(line, "snap and pounding not yet: needs 5 passages with 10 Hz motion, has 0") {
		t.Errorf("line: %q", line)
	}
	if !strings.HasSuffix(line, ".") {
		t.Errorf("line should end with a full stop: %q", line)
	}
}

func TestTheStatusLineBeforeAndWithoutAnyFit(t *testing.T) {
	var none *comfortModelSet
	if got := none.statusLine(); !strings.Contains(got, "not worked out yet") {
		t.Errorf("before the first fit: %q", got)
	}
	if got := fitSpells(nil).statusLine(); !strings.Contains(got, "not learned yet") {
		t.Errorf("no spells: %q", got)
	}
}

func TestTheWireCarriesTheFiguresBehindTheSentence(t *testing.T) {
	w := learnedSet(t).wire()
	if len(w.Models) != 1 || w.Models[0].Model != "gfs" || w.Summary == "" {
		t.Fatalf("wire: %+v", w)
	}
	sway := w.Models[0].Sway
	if !sway.Learned || sway.Passages != 8 || sway.Skill == nil || sway.Reason != "" {
		t.Errorf("sway: %+v", sway)
	}
	var none *comfortModelSet
	empty := none.wire()
	if empty.Models == nil || len(empty.Models) != 0 || !strings.Contains(empty.Summary, "not worked out yet") {
		t.Errorf("before any fit: %+v", empty)
	}
	failed := failedComfortSet(errors.New("disk")).wire()
	if failed.Error != "disk" || !strings.Contains(failed.Summary, "couldn't be worked out: disk") {
		t.Errorf("a fit that could not read the record: %+v", failed)
	}
}

// ── the store ────────────────────────────────────────────────────────────

func TestRefitFromAStoreWithNoSeaSaysNotLearnedAndKeepsTheStatus(t *testing.T) {
	store, _, _ := seaPassage(t)
	store.comfort.fit(t.Context(), store, noCoastline)
	set := store.comfort.Load()
	if set == nil {
		t.Fatal("the fit must leave a status")
	}
	if got := set.statusLine(); !strings.Contains(got, "not learned yet") {
		t.Errorf("line: %q", got)
	}
}

func TestSpellsWhoseWindWaveCouldNotBeCheckedAgainstTheShoreDoNotTrain(t *testing.T) {
	r := newFetchRig(t, 15, 180)
	r.store.comfort.fit(t.Context(), r.store, noCoastline)
	rejected := r.store.comfort.Load().Rejected
	if rejected.Total != 2 || rejected.count() != 2 {
		t.Errorf("with no coastline on the install every wind wave is unchecked: %+v", rejected)
	}
}

func TestThePassagePageCarriesTheComfortModelBesideThePassage(t *testing.T) {
	store := withPassageStore(t)
	b := simpleTrip(2 * time.Hour)
	appendAll(t, store, b.pts)
	rec, err := store.InsertPassage(passageRecord{Start: b.pts[2].At, End: b.pts[360].At, Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	store.comfort.fit(t.Context(), store, noCoastline)
	_, body := getJSON(t, getPassageHandler, "/api/passages/x", rec.ID)
	cm, ok := body["comfort_model"].(map[string]any)
	if !ok {
		t.Fatalf("the response has no top-level comfort_model object: %v", body)
	}
	if s, _ := cm["summary"].(string); !strings.HasPrefix(s, "Comfort model:") {
		t.Errorf("summary: %q", s)
	}
	if p, _ := body["passage"].(map[string]any); p != nil {
		if felt, _ := p["felt"].(map[string]any); felt != nil {
			if _, has := felt["comfort_model"]; has {
				t.Error("the boat-level status belongs beside the passage, not inside its Felt block")
			}
		}
	}
}

// ── review pass 1 ────────────────────────────────────────────────────────

func TestAStoreErrorReadingFetchFiguresStopsTheFitAndSaysSo(t *testing.T) {
	r := newFetchRig(t, 15, 180)
	if _, err := r.store.db.Exec(`DROP TABLE sea_fetch_rays`); err != nil {
		t.Fatal(err)
	}
	r.store.comfort.fit(t.Context(), r.store, noCoastline)
	set := r.store.comfort.Load()
	if set == nil || !strings.Contains(set.Err, "stored fetch figures couldn't be read") {
		t.Fatalf("the set must carry the error: %+v", set)
	}
	if strings.Contains(set.Summary, "not learned yet") || !strings.Contains(set.Summary, "couldn't be worked out") {
		t.Errorf("a store error must not read as not learned yet: %q", set.Summary)
	}
}

func TestTheSummaryNamesWhatIsHoldingTheFitBack(t *testing.T) {
	r := newFetchRig(t, 15, 180)
	r.store.comfort.fit(t.Context(), r.store, noCoastline)
	line := r.store.comfort.Load().Summary
	if strings.Contains(line, "no recorded passage has motion and sea together") {
		t.Errorf("spells were set aside for a reason, and the line must give it: %q", line)
	}
	if !strings.Contains(line, "not learned yet") || !strings.Contains(line, "isn't checked") || !strings.Contains(line, "isn't on this install") {
		t.Errorf("line: %q", line)
	}
}

func TestTheFitLogListsHowManySpellsWereSetAsideForEachReason(t *testing.T) {
	r := newFetchRig(t, 15, 180)
	buf := logOf(t)
	r.store.comfort.fit(t.Context(), r.store, noCoastline)
	if out := buf.String(); !strings.Contains(out, "set aside") || !strings.Contains(out, "isn't on this install") {
		t.Errorf("log: %s", out)
	}
}

func TestTheNullMedianWeighsPassagesAlikeAsTheFitDoes(t *testing.T) {
	spellsOf := func(id string, n int, y float64) []comfortSpell {
		out := make([]comfortSpell, n)
		for i := range out {
			out[i] = comfortSpell{Passage: id, RollRMSDeg: y}
		}
		return out
	}
	passages := [][]comfortSpell{
		spellsOf("long", 20, 5), spellsOf("a", 1, 1), spellsOf("b", 1, 1), spellsOf("c", 1, 1), spellsOf("d", 1, 1),
	}
	build := func([][]comfortSpell) comfortPredictor { return func(comfortSpell) float64 { return 1 } }
	skill, ok := lopoSkill(passages, build, func(s comfortSpell) float64 { return s.RollRMSDeg })
	if !ok {
		t.Fatal("no skill")
	}
	// Each short passage left out sees a null of 1 (passages alike), the long
	// one left out sees 1 too: the model, always 1, is never better than the null.
	closeTo(t, "skill against a passage-weighted null", skill, 0, 1e-9)
}

func TestTheSummaryNamesTheCommonestReasonWithItsCounts(t *testing.T) {
	var rej comfortRejections
	rej.Total = 1500
	for range 1400 {
		rej.add("their wind wave isn't checked against fetch (the Offline coastline is off)")
	}
	for range 60 {
		rej.add("they have no recorded heading")
	}
	line := fitComfortModels(nil, rej, comfortWaiting{}).Summary
	want := "Comfort model: not learned yet: 1,460 of 1,500 recorded spells are set aside, most often because their wind wave isn't checked against fetch (the Offline coastline is off)."
	if line != want {
		t.Errorf("line: %q", line)
	}
	if got := rej.listing(); !strings.Contains(got, "1400 because their wind wave") || !strings.Contains(got, "60 because they have no recorded heading") {
		t.Errorf("listing: %q", got)
	}
	if line := fitSpells(nil).Summary; !strings.Contains(line, "no passage has recorded motion") {
		t.Errorf("no spells at all: %q", line)
	}
}

func TestAnHourWithNoWaveFigureSaysSoWhenItIsAskedFor(t *testing.T) {
	if got := comfortGap(passageHour{}); got != comfortReasonNoWave {
		t.Errorf("gap for an hour with no wave: %q", got)
	}
	if _, why := comfortInputFromHour(passageHour{}, 0); why != comfortReasonNoWave {
		t.Errorf("input for an hour with no wave: %q", why)
	}
}
