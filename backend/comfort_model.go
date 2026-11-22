package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// The comfort model (ADR 0301): how much sway, snap and pounding a sea gives
// this boat, learned from the boat's own recorded spells and the sea behind each.
// It is fitted in memory for each wave model, never stored, and a target is
// used only after it predicts whole passages it was not trained on better than a
// fixed guess. comfort_worker.go says when it is fitted.

type comfortTarget string

const (
	targetRoll     comfortTarget = "roll"
	targetPitch    comfortTarget = "pitch"
	targetSnap     comfortTarget = "snap"
	targetPounding comfortTarget = "pounding"
)

const (
	comfortMinSkill       = 0.30
	comfortSplitMinWaveM  = 0.1
	comfortBeyondSlack    = 1e-9
	comfortRidgeShare     = 1e-9
	comfortReasonNoSplit  = "The wave plugin gave no swell and wind-wave split for this hour, so comfort can't be judged."
	comfortReasonNoLabel  = "The wave plugin doesn't name its wave model; update the wave plugin so the comfort model can be matched to its forecast."
	comfortNotWorkedOut   = "not worked out yet"
	comfortReasonNoWave   = "The wave forecast has no figure for this hour, so comfort can't be judged."
	comfortReasonNoLearn  = "No comfort model has been learned from recorded passages yet."
	comfortReasonTooAlike = "the recorded passages are too alike to judge a fit by"
)

// comfortSpell is one recorded spell the model learns from.
type comfortSpell struct {
	Passage     string
	Model       string
	HeadingDeg  float64
	Swell       *seaTrainFigures
	WindWave    *seaTrainFigures
	RollRMSDeg  float64
	PitchRMSDeg float64
	SnapDps2    *float64
	PoundingG   *float64
}

// comfortSpellFrom says whether a spell can train the fit, and if not, why, in
// words that finish "spells are set aside because ...". sea is the spell's sea
// after the fetch check, so a capped wind wave carries the capped figures. model
// is the id of the wave model behind the stored hour.
func comfortSpellFrom(passage string, sp feltSpell, sea spellSea, model string) (comfortSpell, string) {
	switch {
	case sea.Reason != "":
		return comfortSpell{}, comfortSeaReason(sea.Reason)
	case sp.MeanHeadingDeg == nil:
		return comfortSpell{}, "they have no recorded heading"
	}
	switch sea.Swell.swellVerdict() {
	case seaSwellEnclosed:
		return comfortSpell{}, "their swell isn't judged in enclosed water"
	case seaSwellUnchecked:
		return comfortSpell{}, withDetail("their swell isn't checked for enclosed water", strings.TrimPrefix(sea.Swell.Note, swellUncheckedNote))
	}
	if sea.WindWave.windVerdict() == seaWindUnchecked {
		return comfortSpell{}, withDetail("their wind wave isn't checked against fetch", sea.WindWave.Fetch.Reason)
	}
	cs := comfortSpell{
		Passage: passage, Model: model, HeadingDeg: *sp.MeanHeadingDeg,
		RollRMSDeg: sp.RollRMSDeg, PitchRMSDeg: sp.PitchRMSDeg,
		SnapDps2: sp.RollAccelPeakDps2, PoundingG: sp.VertAccelPeakG,
	}
	if sea.Swell != nil {
		f := sea.Swell.seaTrainFigures
		cs.Swell = &f
	}
	if sea.WindWave != nil {
		f := sea.WindWave.seaTrainFigures
		cs.WindWave = &f
	}
	return cs, ""
}

// comfortSeaReason words, to finish "spells are set aside because ...", why a
// spell has no sea state. Each way it can have none is its own reason, so the
// counts say which to fix: a position that was never recorded, an hour not
// fetched yet, a wave plugin that cannot be asked, the data source's refusal, an
// hour the model has no figure for, or a sea with no swell and wind-wave split.
func comfortSeaReason(reason string) string {
	switch {
	case reason == seaReasonNoPosition:
		return "they have no recorded position"
	case reason == seaReasonNotFetched:
		return "their sea state hasn't been fetched yet"
	case strings.HasPrefix(reason, seaReasonCantFetch):
		return "the wave plugin can't be asked for their sea state"
	case strings.HasPrefix(reason, seaRefusedPrefix):
		return "the data source refused their sea state"
	case reason == seaReasonNoFigure:
		return "the wave plugin gave no figure for their hour"
	case reason == seaReasonNoSplit:
		return "the wave plugin gave no swell and wind-wave split for their hour"
	case reason == seaReasonUnreadable:
		return "their sea state couldn't be read"
	}
	return "the wave plugin has no sea state for their hour"
}

func withDetail(what, detail string) string {
	if detail == "" {
		return what
	}
	return what + " (" + detail + ")"
}

// comfortRejections counts the recorded spells the fit read and the ones it set
// aside, by the reason each was set aside for.
type comfortRejections struct {
	Total    int
	ByReason map[string]int
}

func (r *comfortRejections) add(why string) {
	if r.ByReason == nil {
		r.ByReason = map[string]int{}
	}
	r.ByReason[why]++
}

// count is the number of spells set aside.
func (r comfortRejections) count() int {
	var n int
	for _, c := range r.ByReason {
		n += c
	}
	return n
}

// byCount lists the reasons, the commonest first and equals in alphabetical order.
func (r comfortRejections) byCount() []string {
	out := make([]string, 0, len(r.ByReason))
	for why := range r.ByReason {
		out = append(out, why)
	}
	sort.Slice(out, func(i, j int) bool {
		if r.ByReason[out[i]] != r.ByReason[out[j]] {
			return r.ByReason[out[i]] > r.ByReason[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}

// listing is every reason with its count, for the log.
func (r comfortRejections) listing() string {
	var parts []string
	for _, why := range r.byCount() {
		parts = append(parts, fmt.Sprintf("%d because %s", r.ByReason[why], why))
	}
	return strings.Join(parts, " | ")
}

// groupThousands writes n with a comma between thousands, as the operator reads it.
func groupThousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0 && s[i-1] != '-'; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// comfortFeature is one wave system's feature for a target: its steepness
// s = pi H / L, with L = g T^2 / 2 pi, times how the system lies to the heading.
// A system the model has none of gives 0.
func comfortFeature(t comfortTarget, tr *seaTrainFigures, headingDeg float64) float64 {
	if tr == nil {
		return 0
	}
	steepness, ok := waveSteepness(tr.HeightM, tr.PeriodS)
	if !ok {
		return 0
	}
	s := math.Pi * steepness
	theta := (tr.FromDeg - headingDeg) * math.Pi / 180
	switch t {
	case targetRoll:
		return s * math.Abs(math.Sin(theta))
	case targetPitch:
		return s * math.Abs(math.Cos(theta))
	case targetSnap:
		omega := 2 * math.Pi / tr.PeriodS
		return s * omega * omega * math.Abs(math.Sin(theta))
	case targetPounding:
		return s * math.Max(0, math.Cos(theta))
	}
	return 0
}

// comfortRecorded is the figure a spell recorded for a target. Only a spell with
// 10 Hz motion is asked for snap or pounding.
func comfortRecorded(t comfortTarget, s comfortSpell) float64 {
	switch t {
	case targetRoll:
		return s.RollRMSDeg
	case targetPitch:
		return s.PitchRMSDeg
	case targetSnap:
		return *s.SnapDps2
	}
	return *s.PoundingG
}

// ── the variance fit ─────────────────────────────────────────────────────

// varianceCoef is y^2 = A + Bs xs^2 + Bw xw^2, every coefficient at least 0.
type varianceCoef struct{ A, Bs, Bw float64 }

func (c varianceCoef) predict(xs, xw float64) float64 {
	return math.Sqrt(math.Max(0, c.A+c.Bs*xs*xs+c.Bw*xw*xw))
}

type varianceRow struct{ z, u1, u2, w float64 }

// solveSmall solves the n x n system a x = b (n at most 3) by elimination with
// pivoting. It reports false for a singular system.
func solveSmall(a *[3][3]float64, b *[3]float64, n int) ([3]float64, bool) {
	for c := 0; c < n; c++ {
		p := c
		for r := c + 1; r < n; r++ {
			if math.Abs(a[r][c]) > math.Abs(a[p][c]) {
				p = r
			}
		}
		if math.Abs(a[p][c]) < 1e-14 {
			return [3]float64{}, false
		}
		a[c], a[p] = a[p], a[c]
		b[c], b[p] = b[p], b[c]
		for r := c + 1; r < n; r++ {
			f := a[r][c] / a[c][c]
			for k := c; k < n; k++ {
				a[r][k] -= f * a[c][k]
			}
			b[r] -= f * b[c]
		}
	}
	var x [3]float64
	for r := n - 1; r >= 0; r-- {
		s := b[r]
		for k := r + 1; k < n; k++ {
			s -= a[r][k] * x[k]
		}
		x[r] = s / a[r][r]
	}
	return x, true
}

// fitVariance is the non-negative least squares fit of z = A + Bs u1 + Bw u2,
// each row weighted. With three coefficients every subset of them is tried and
// the best one with no negative coefficient wins, which is exact. A tiny ridge
// keeps two columns that move together solvable and nothing more.
func fitVariance(rows []varianceRow) varianceCoef {
	var scale [3]float64
	var sumW float64
	if len(rows) > 0 {
		scale[0] = 1
	}
	for _, r := range rows {
		sumW += r.w
		scale[1] = math.Max(scale[1], math.Abs(r.u1))
		scale[2] = math.Max(scale[2], math.Abs(r.u2))
	}
	// A column no row uses has no scale and is left out of every subset.
	x := make([][3]float64, len(rows))
	for i, r := range rows {
		x[i][0] = 1
		if scale[1] > 0 {
			x[i][1] = r.u1 / scale[1]
		}
		if scale[2] > 0 {
			x[i][2] = r.u2 / scale[2]
		}
	}
	best := varianceCoef{}
	bestSSE := math.Inf(1)
	for mask := 0; mask < 8; mask++ {
		c, ok := fitSubset(rows, x, scale, comfortRidgeShare*sumW, mask)
		if !ok {
			continue
		}
		if sse := varianceSSE(rows, c); sse < bestSSE {
			best, bestSSE = c, sse
		}
	}
	return best
}

// fitSubset is the least squares fit on the columns the mask names, the others
// held at zero. It reports false when the subset has no scale, cannot be solved
// or wants a negative coefficient. Mask 0 is every coefficient at zero.
func fitSubset(rows []varianceRow, x [][3]float64, scale [3]float64, ridge float64, mask int) (varianceCoef, bool) {
	var cols [3]int
	n := 0
	for j := 0; j < 3; j++ {
		if mask&(1<<j) == 0 {
			continue
		}
		if scale[j] == 0 {
			return varianceCoef{}, false
		}
		cols[n] = j
		n++
	}
	var a [3][3]float64
	var b [3]float64
	for i := 0; i < n; i++ {
		a[i][i] = ridge
	}
	for r, row := range rows {
		addNormalRow(&a, &b, x[r], cols[:n], row)
	}
	sol, ok := solveSmall(&a, &b, n)
	if !ok {
		return varianceCoef{}, false
	}
	var coef [3]float64
	for i, j := range cols[:n] {
		if sol[i] < -1e-12 {
			return varianceCoef{}, false
		}
		coef[j] = math.Max(0, sol[i]) / scale[j]
	}
	return varianceCoef{A: coef[0], Bs: coef[1], Bw: coef[2]}, true
}

// addNormalRow adds one weighted row to the normal equations of the columns used.
func addNormalRow(a *[3][3]float64, b *[3]float64, xr [3]float64, cols []int, row varianceRow) {
	for i, ji := range cols {
		b[i] += row.w * xr[ji] * row.z
		for k, jk := range cols {
			a[i][k] += row.w * xr[ji] * xr[jk]
		}
	}
}

func varianceSSE(rows []varianceRow, c varianceCoef) float64 {
	var sse float64
	for _, r := range rows {
		d := r.z - (c.A + c.Bs*r.u1 + c.Bw*r.u2)
		sse += r.w * d * d
	}
	return sse
}

// comfortTargetFit is the fit for one target. MaxXs and MaxXw are the largest
// features in the record, past which a prediction is an extrapolation.
type comfortTargetFit struct {
	Target       comfortTarget
	Coef         varianceCoef
	MaxXs, MaxXw float64
}

// groupByPassage splits spells into one slice for each passage, in the order the
// passages first appear.
func groupByPassage(spells []comfortSpell) [][]comfortSpell {
	var out [][]comfortSpell
	at := map[string]int{}
	for _, s := range spells {
		i, seen := at[s.Passage]
		if !seen {
			i = len(out)
			at[s.Passage] = i
			out = append(out, nil)
		}
		out[i] = append(out[i], s)
	}
	return out
}

// fitTarget fits one target over the passages given, each passage weighted alike
// whatever its number of spells. y is the spell's recorded figure. It is nil for
// no passages.
func fitTarget(passages [][]comfortSpell, t comfortTarget, y func(comfortSpell) float64) *comfortTargetFit {
	if len(passages) == 0 {
		return nil
	}
	var rows []varianceRow
	fit := &comfortTargetFit{Target: t}
	for _, p := range passages {
		for _, s := range p {
			xs := comfortFeature(t, s.Swell, s.HeadingDeg)
			xw := comfortFeature(t, s.WindWave, s.HeadingDeg)
			v := y(s)
			rows = append(rows, varianceRow{z: v * v, u1: xs * xs, u2: xw * xw, w: 1 / float64(len(p))})
			fit.MaxXs, fit.MaxXw = math.Max(fit.MaxXs, xs), math.Max(fit.MaxXw, xw)
		}
	}
	fit.Coef = fitVariance(rows)
	return fit
}

func (f *comfortTargetFit) at(heading float64, swell, wind *seaTrainFigures) (value float64, beyond bool) {
	xs, xw := comfortFeature(f.Target, swell, heading), comfortFeature(f.Target, wind, heading)
	beyond = xs > f.MaxXs*(1+comfortBeyondSlack) || xw > f.MaxXw*(1+comfortBeyondSlack)
	return f.Coef.predict(xs, xw), beyond
}

// ── the leave-one-passage-out check ──────────────────────────────────────

type comfortPredictor func(comfortSpell) float64

// lopoSkill is 1 - (the model's error) / (the error of the median of the
// training figures), each left-out passage counting once. ok is false when a
// fixed guess is never wrong, so no skill can be told.
func lopoSkill(passages [][]comfortSpell, build func(train [][]comfortSpell) comfortPredictor, actual func(comfortSpell) float64) (skill float64, ok bool) {
	var errModel, errNull float64
	for i, test := range passages {
		train := append(append([][]comfortSpell{}, passages[:i]...), passages[i+1:]...)
		if len(train) == 0 {
			return 0, false
		}
		pred := build(train)
		// The fixed guess weighs passages as the fit does: each passage counts once,
		// so a long rough passage cannot set what a typical one is.
		var ys, ws []float64
		for _, p := range train {
			for _, s := range p {
				ys = append(ys, actual(s))
				ws = append(ws, 1/float64(len(p)))
			}
		}
		null := weightedMedian(ys, ws)
		var em, en float64
		for _, s := range test {
			em += math.Abs(pred(s) - actual(s))
			en += math.Abs(null - actual(s))
		}
		errModel += em / float64(len(test))
		errNull += en / float64(len(test))
	}
	if errNull == 0 {
		return 0, false
	}
	return 1 - errModel/errNull, true
}

// weightedMedian is the value at which half the total weight lies on each side.
// When the halfway point falls exactly between two values it is their mean. It
// is 0 for no values.
func weightedMedian(values, weights []float64) float64 {
	idx := make([]int, len(values))
	var total float64
	for i := range idx {
		idx[i] = i
		total += weights[i]
	}
	sort.Slice(idx, func(a, b int) bool { return values[idx[a]] < values[idx[b]] })
	half := total / 2
	var cum float64
	for n, i := range idx {
		cum += weights[i]
		switch {
		case math.Abs(cum-half) <= 1e-12*total && n+1 < len(idx):
			return (values[i] + values[idx[n+1]]) / 2
		case cum > half:
			return values[i]
		}
	}
	return 0
}

func skillReason(skill float64, passages int) string {
	if skill >= comfortMinSkill {
		return ""
	}
	return fmt.Sprintf("the model doesn't yet predict recorded passages better than a fixed guess (skill %.2f, needs %.2f, from %d passages)", skill, comfortMinSkill, passages)
}

func gateReason(passages, spells, minPassages, minSpells int, what string) string {
	switch {
	case passages < minPassages:
		return fmt.Sprintf("needs %d passages with %s, has %d", minPassages, what, passages)
	case spells < minSpells:
		return fmt.Sprintf("needs %d spells with %s, has %d", minSpells, what, spells)
	}
	return ""
}

// ── the three groups ─────────────────────────────────────────────────────

// comfortGroupDef is one of sway, snap and pounding: the targets it predicts
// (sway is roll and pitch together), what it needs before it is trusted, and
// which spells it learns from.
type comfortGroupDef struct {
	name                   string
	targets                []comfortTarget
	minPassages, minSpells int
	what                   string
	has                    func(comfortSpell) bool // nil: every spell
}

// comfortGroupDefs are sway, snap and pounding, in the order a fit lists them.
var comfortGroupDefs = [3]comfortGroupDef{
	{name: "sway", targets: []comfortTarget{targetRoll, targetPitch}, minPassages: 8, minSpells: 60, what: "motion and sea"},
	{name: "snap", targets: []comfortTarget{targetSnap}, minPassages: 5, minSpells: 30, what: "10 Hz motion",
		has: func(s comfortSpell) bool { return s.SnapDps2 != nil }},
	{name: "pounding", targets: []comfortTarget{targetPounding}, minPassages: 5, minSpells: 30, what: "10 Hz motion",
		has: func(s comfortSpell) bool { return s.PoundingG != nil }},
}

// comfortGroupStatus is how one of sway, snap and pounding stands. Skill is only
// told once the group has passed its gate.
type comfortGroupStatus struct {
	Name             string
	Learned          bool
	Passages, Spells int
	Skill            float64
	Reason           string
}

// describe is "X learned ..." or "X not yet ...", for the Felt line or, with
// forLog, with more detail for the log.
func (g comfortGroupStatus) describe(forLog bool) string {
	switch {
	case g.Learned && forLog:
		return fmt.Sprintf("%s learned (%d passages, %d spells, skill %.2f)", g.Name, g.Passages, g.Spells, g.Skill)
	case g.Learned:
		return fmt.Sprintf("%s learned from %d passages (skill %.2f)", g.Name, g.Passages, g.Skill)
	case forLog:
		return fmt.Sprintf("%s not yet (%s)", g.Name, g.Reason)
	}
	return g.Name + " not yet: " + g.Reason
}

// comfortGroup is a group's status and, once it is learned, one fit for each of
// its targets. A group that is not learned has no fits and predicts nothing.
type comfortGroup struct {
	comfortGroupStatus
	Fits []*comfortTargetFit
}

// at is the group's figure for a sea: the combined figure of its targets, each
// target's own, and whether any input lay beyond the record.
func (g comfortGroup) at(heading float64, swell, wind *seaTrainFigures) (value float64, parts []float64, beyond bool) {
	for _, f := range g.Fits {
		v, b := f.at(heading, swell, wind)
		parts = append(parts, v)
		value = math.Hypot(value, v)
		beyond = beyond || b
	}
	return value, parts, beyond
}

// fitTargets fits each target of a group over the passages.
func fitTargets(def comfortGroupDef, passages [][]comfortSpell) []*comfortTargetFit {
	fits := make([]*comfortTargetFit, len(def.targets))
	for i, t := range def.targets {
		fits[i] = fitTarget(passages, t, func(s comfortSpell) float64 { return comfortRecorded(t, s) })
	}
	return fits
}

// learnGroup applies the gate and the leave-one-passage-out check to one group
// over a wave model's spells, and fits it when it passes both.
func learnGroup(def comfortGroupDef, spells []comfortSpell) comfortGroup {
	var used []comfortSpell
	for _, s := range spells {
		if def.has == nil || def.has(s) {
			used = append(used, s)
		}
	}
	passages := groupByPassage(used)
	g := comfortGroup{comfortGroupStatus: comfortGroupStatus{Name: def.name, Passages: len(passages), Spells: len(used)}}
	if why := gateReason(g.Passages, g.Spells, def.minPassages, def.minSpells, def.what); why != "" {
		g.Reason = why
		return g
	}
	actual := func(s comfortSpell) float64 {
		var v float64
		for _, t := range def.targets {
			v = math.Hypot(v, comfortRecorded(t, s))
		}
		return v
	}
	build := func(train [][]comfortSpell) comfortPredictor {
		held := comfortGroup{Fits: fitTargets(def, train)}
		return func(s comfortSpell) float64 {
			v, _, _ := held.at(s.HeadingDeg, s.Swell, s.WindWave)
			return v
		}
	}
	skill, ok := lopoSkill(passages, build, actual)
	if !ok {
		g.Reason = comfortReasonTooAlike
		return g
	}
	g.Skill = skill
	if why := skillReason(skill, g.Passages); why != "" {
		g.Reason = why
		return g
	}
	g.Learned = true
	g.Fits = fitTargets(def, passages)
	return g
}

// comfortFit is what is learned for one wave model.
type comfortFit struct {
	Sway, Snap, Pounding comfortGroup
}

// groups lists the fit's groups in the order of comfortGroupDefs.
func (f *comfortFit) groups() [3]*comfortGroup {
	return [3]*comfortGroup{&f.Sway, &f.Snap, &f.Pounding}
}

// comfortModelSet is every fit, one for each wave model, with the sentence the
// Felt row shows, worked out once when the set is made. Err is set instead when
// the training record could not be read.
type comfortModelSet struct {
	Fits     map[string]*comfortFit
	Err      string
	Rejected comfortRejections
	Waiting  comfortWaiting
	Summary  string
}

// comfortWaiting is the passages the fit could not read yet: Passages is how many
// have no current figures, and Failed the errors of those whose figures could not
// be worked out.
type comfortWaiting struct {
	Passages int
	Failed   []string
	// Plugin says new sea hours cannot be fetched now, for want of a wave plugin
	// that can be asked. The fit still learns from the hours already stored.
	Plugin bool
}

// notes are the clauses the summary and the log add for what is waiting.
func (w comfortWaiting) notes() []string {
	var out []string
	switch n := w.Passages - len(w.Failed); {
	case n == 1:
		out = append(out, "1 passage is waiting for its figures to be worked out")
	case n > 1:
		out = append(out, fmt.Sprintf("%d passages are waiting for their figures to be worked out", n))
	}
	switch len(w.Failed) {
	case 0:
	case 1:
		out = append(out, "1 passage's figures couldn't be worked out: "+w.Failed[0])
	default:
		out = append(out, fmt.Sprintf("%d passages' figures couldn't be worked out, the first: %s", len(w.Failed), w.Failed[0]))
	}
	if w.Plugin {
		out = append(out, "new sea hours are waiting for the wave plugin")
	}
	return out
}

// fitComfortModels fits each wave model from the spells made under it. rejected
// counts the spells that were read and set aside, and waiting the passages not
// read, so a fit with nothing to learn from can say why.
func fitComfortModels(spells []comfortSpell, rejected comfortRejections, waiting comfortWaiting) *comfortModelSet {
	set := &comfortModelSet{Fits: map[string]*comfortFit{}, Rejected: rejected, Waiting: waiting}
	byModel := map[string][]comfortSpell{}
	for _, s := range spells {
		byModel[s.Model] = append(byModel[s.Model], s)
	}
	for model, ms := range byModel {
		f := &comfortFit{}
		for i, g := range f.groups() {
			*g = learnGroup(comfortGroupDefs[i], ms)
		}
		set.Fits[model] = f
	}
	set.Summary = set.summarise()
	return set
}

// failedComfortSet is the set for a record that could not be read.
func failedComfortSet(err error) *comfortModelSet {
	set := &comfortModelSet{Err: err.Error()}
	set.Summary = set.summarise()
	return set
}

func (s *comfortModelSet) sortedModels() []string {
	ids := make([]string, 0, len(s.Fits))
	for id := range s.Fits {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ── predicting ───────────────────────────────────────────────────────────

// comfortHourInput is one forecast hour as the model reads it.
type comfortHourInput struct {
	Model      waveModelRef
	HeadingDeg float64
	Swell      *seaTrainFigures
	WindWave   *seaTrainFigures
}

// comfortFigure is one group's figure for an hour, or why there is none. Beyond
// says an input was larger than any in the record, so the figure is an
// extrapolation.
type comfortFigure struct {
	Value  *float64
	Reason string
	Beyond bool
}

// comfortPrediction holds a figure or its reason for each of sway, snap and
// pounding, and the roll and pitch that sway is made of.
type comfortPrediction struct {
	Sway, Snap, Pounding comfortFigure
	RollDeg, PitchDeg    *float64
}

// figures lists the prediction's figures in the order of comfortGroupDefs.
func (p *comfortPrediction) figures() [3]*comfortFigure {
	return [3]*comfortFigure{&p.Sway, &p.Snap, &p.Pounding}
}

// SnapSidewaysG is the predicted snap as sideways g at the helm height given.
// The fit holds roll acceleration, so a new height needs no refit.
func (p comfortPrediction) SnapSidewaysG(helmHeightM float64) *float64 {
	if p.Snap.Value == nil {
		return nil
	}
	g := sidewaysGAt(helmHeightM, *p.Snap.Value)
	return &g
}

// lowerArticle lowercases the leading "The", "A" or "An" of a label that now
// stands mid-sentence ("The Open-Meteo best-match blend"). A name such as
// "GFS-Wave" is left as it is.
func lowerArticle(label string) string {
	for _, a := range []string{"The ", "A ", "An "} {
		if strings.HasPrefix(label, a) {
			return strings.ToLower(a) + label[len(a):]
		}
	}
	return label
}

func (s *comfortModelSet) predict(in comfortHourInput) comfortPrediction {
	var p comfortPrediction
	figs := p.figures()
	none := func(why string) comfortPrediction {
		for _, f := range figs {
			f.Reason = why
		}
		return p
	}
	if s == nil {
		return none("The comfort model is " + comfortNotWorkedOut + ".")
	}
	if s.Err != "" {
		return none("The comfort model couldn't be worked out: " + s.Err + ".")
	}
	fit := s.Fits[in.Model.ID]
	if fit == nil {
		if len(s.Fits) == 0 {
			return none(comfortReasonNoLearn)
		}
		return none(fmt.Sprintf("The comfort model was learned on %s; this forecast comes from %s.", strings.Join(s.sortedModels(), " and "), lowerArticle(in.Model.Label)))
	}
	for i, g := range fit.groups() {
		if !g.Learned {
			figs[i].Reason = capitalise(g.Reason) + "."
			continue
		}
		v, parts, beyond := g.at(in.HeadingDeg, in.Swell, in.WindWave)
		figs[i].Value, figs[i].Beyond = &v, beyond
		if len(parts) == 2 {
			roll, pitch := parts[0], parts[1]
			p.RollDeg, p.PitchDeg = &roll, &pitch
		}
	}
	return p
}

// ── forecast hours ───────────────────────────────────────────────────────

func trainAbsent(t *seaTrainFigures) bool { return t == nil || t.HeightM <= 0 }

// seaHasNoSplit is the one rule for an hour's sea, for training and forecast
// alike: with a system present the hour is judged from its systems; with none, a
// combined height of 0.1 m or more, or none given, is a plugin that gave no
// split, and only a smaller one is a flat sea.
func seaHasNoSplit(swell, wind *seaTrainFigures, combinedM *float64) bool {
	return trainAbsent(swell) && trainAbsent(wind) && (combinedM == nil || *combinedM >= comfortSplitMinWaveM)
}

// comfortGap says why this forecast hour cannot be judged for comfort, or ""
// when it can. An hour with a system is judged from its systems; see seaHasNoSplit.
func comfortGap(h passageHour) string {
	var combined *float64
	if h.HasWave {
		combined = &h.WaveM
	}
	if seaHasNoSplit(h.Swell, h.WindWave, combined) && !h.HasWave {
		return comfortReasonNoWave
	}
	if !h.WaveModel.named() {
		return comfortReasonNoLabel
	}
	if seaHasNoSplit(h.Swell, h.WindWave, combined) {
		return comfortReasonNoSplit
	}
	return ""
}

// comfortInputFromHour is the model's input for a forecast hour on a heading, or
// the reason it has none.
func comfortInputFromHour(h passageHour, headingDeg float64) (comfortHourInput, string) {
	if why := comfortGap(h); why != "" {
		return comfortHourInput{}, why
	}
	return comfortHourInput{Model: h.WaveModel, HeadingDeg: headingDeg, Swell: h.Swell, WindWave: h.WindWave}, ""
}

// ── what the Felt row says ───────────────────────────────────────────────

// statusLine is the sentence the Felt row shows about the model.
func (s *comfortModelSet) statusLine() string {
	if s == nil {
		return "Comfort model: " + comfortNotWorkedOut + "; it is fitted at start-up and again when new sea figures come in or a passage is edited."
	}
	return s.Summary
}

// summarise is the sentence for the wave model with the most passages behind its
// sway.
func (s *comfortModelSet) summarise() string {
	if s.Err != "" {
		return "Comfort model: couldn't be worked out: " + s.Err + "."
	}
	var parts []string
	if len(s.Fits) == 0 {
		parts = []string{s.nothingToLearnFrom()}
	} else {
		var primary *comfortFit
		for _, id := range s.sortedModels() {
			if f := s.Fits[id]; primary == nil || f.Sway.Passages > primary.Sway.Passages {
				primary = f
			}
		}
		parts = []string{primary.Sway.describe(false)}
		sn, po := primary.Snap, primary.Pounding
		if !sn.Learned && !po.Learned && sn.Reason == po.Reason {
			parts = append(parts, "snap and pounding not yet: "+sn.Reason)
		} else {
			parts = append(parts, sn.describe(false), po.describe(false))
		}
	}
	parts = append(parts, s.Waiting.notes()...)
	return "Comfort model: " + strings.Join(parts, "; ") + "."
}

// nothingToLearnFrom says why no spell trained a fit: what held the most of them
// back, or that no passage has a spell yet. Passages still waiting for their
// figures are said after it, so with none read the sentence is "not learned yet"
// and the waiting.
func (s *comfortModelSet) nothingToLearnFrom() string {
	reasons := s.Rejected.byCount()
	if len(reasons) == 0 {
		if s.Waiting.Passages > 0 {
			return "not learned yet"
		}
		return "not learned yet; no passage has recorded motion yet"
	}
	top := reasons[0]
	return fmt.Sprintf("not learned yet: %s of %s recorded spells are set aside, most often because %s",
		groupThousands(s.Rejected.count()), groupThousands(s.Rejected.Total), top)
}

// comfortGroupWire is one group's standing as the passage page carries it.
type comfortGroupWire struct {
	Name     string   `json:"name"`
	Learned  bool     `json:"learned"`
	Passages int      `json:"passages"`
	Spells   int      `json:"spells"`
	Skill    *float64 `json:"skill,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

// comfortFitWire is what is learned for one wave model.
type comfortFitWire struct {
	Model    string           `json:"model"`
	Sway     comfortGroupWire `json:"sway"`
	Snap     comfortGroupWire `json:"snap"`
	Pounding comfortGroupWire `json:"pounding"`
}

// comfortModelWire is the boat-level comfort model status: the sentence the page
// shows, and the figures behind it. It is the same for every passage.
type comfortModelWire struct {
	Summary string `json:"summary"`
	Error   string `json:"error,omitempty"`
	// WaitingPassages is how many passages the fit has not read yet, for want of
	// their figures.
	WaitingPassages int              `json:"waiting_passages"`
	Models          []comfortFitWire `json:"models"`
}

func (g comfortGroup) wire() comfortGroupWire {
	w := comfortGroupWire{Name: g.Name, Learned: g.Learned, Passages: g.Passages, Spells: g.Spells, Reason: g.Reason}
	if g.Learned {
		skill := g.Skill
		w.Skill = &skill
	}
	return w
}

func (s *comfortModelSet) wire() comfortModelWire {
	w := comfortModelWire{Summary: s.statusLine(), Models: []comfortFitWire{}}
	if s == nil {
		return w
	}
	w.Error = s.Err
	w.WaitingPassages = s.Waiting.Passages
	for _, id := range s.sortedModels() {
		f := s.Fits[id]
		w.Models = append(w.Models, comfortFitWire{Model: id, Sway: f.Sway.wire(), Snap: f.Snap.wire(), Pounding: f.Pounding.wire()})
	}
	return w
}
