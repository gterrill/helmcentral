package main

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"time"
)

// The comfort chip on a plan leg (ADR 0302). Each forecast hour of the leg is
// judged with the model fitted from the boat's recorded passages (ADR 0301), the
// wind wave capped by the fetch the shore gives over the leg's own position that
// hour (ADR 0300), and the sway banded by the operator's ratings (ADR 0298). The
// hours then read as a few stretches. An hour that cannot be judged is said so and
// never reads as calm. The server words everything the chip shows; the page only
// lays it out.

// planComfortDeps are the readers a leg's comfort needs, so a test supplies fakes
// and production wires the fitted model, the learned bands, the helm height, the
// Offline coastline and the memo of shore geometries. Every reader must be set:
// a reader that is not is a programming error the assessment says so, never a
// leg quietly without comfort. Comfort is off by a nil *planComfortDeps.
type planComfortDeps struct {
	model      func() (*comfortModelSet, error)
	bands      func() (comfortBands, error)
	helmHeight func() (*float64, error)
	loader     func() *coastlineLoader
	memo       *fetchGeoMemo
}

// unwired names a reader that is not set, "" when every one is.
func (d *planComfortDeps) unwired() string {
	switch {
	case d.model == nil:
		return "comfort model"
	case d.bands == nil:
		return "comfort bands"
	case d.helmHeight == nil:
		return "helm height"
	case d.loader == nil:
		return "coastline"
	case d.memo == nil:
		return "shore geometry memo"
	}
	return ""
}

// comfortSetup is what every leg of one request shares: the model, the bands, the
// helm height, the request's one coastline loader (made when an hour first reaches
// the shore) and the shore each snapped position gave. A request judges its legs one
// after another, so the shores need no lock.
type comfortSetup struct {
	set     *comfortModelSet
	bands   comfortBands
	helm    *float64
	helmErr error
	loader  func() *coastlineLoader
	memo    *fetchGeoMemo
	shores  map[[2]int]shoreRays
	// onPredict is a test hook, nil in production: the model was asked for an hour.
	onPredict func()
}

// shoreRays is what the shore gave at one snapped position, or why it gave none.
type shoreRays struct {
	rays rayFunc
	why  string
}

func newComfortSetup(set *comfortModelSet, bands comfortBands, helm *float64, helmErr error, newLoader func() *coastlineLoader, memo *fetchGeoMemo) *comfortSetup {
	return &comfortSetup{
		set: set, bands: bands, helm: helm, helmErr: helmErr, memo: memo,
		loader: sync.OnceValue(func() *coastlineLoader {
			l := newLoader()
			l.shared = memo.lines
			return l
		}),
	}
}

// planComfortHour is one forecast hour of the leg as the page reads it: when, how
// the sea bands it, and the hour in one line.
type planComfortHour struct {
	At     string `json:"at"`
	Label  string `json:"label"`
	Band   string `json:"band"`
	Beyond bool   `json:"beyond"`
	Line   string `json:"line"`
}

// planLegComfort is what a leg's chip says.
type planLegComfort struct {
	Text   string            `json:"text"`
	Motion string            `json:"motion"`
	Hours  []planComfortHour `json:"hours"`
}

// legComfortInput is one leg to judge. depart and arrive are the real span of the
// leg; from is where the walk starts, the departure or, for the leg under way, now.
type legComfortInput struct {
	*comfortSetup
	as       *passageAssessor
	plan     passageLegPlan
	depart   time.Time
	arrive   time.Time
	from     time.Time
	underway bool
	loc      *time.Location
}

const (
	comfortReasonNoHour = "the forecast has no figure for this hour, so comfort can't be judged"
	comfortReasonAshore = "position charted ashore"
	// comfortReasonOutOfTime is every hour the request's time ran out before; the
	// assessment stops waiting for the shore at its deadline.
	comfortReasonOutOfTime = "the comfort check ran out of time"
	// comfortReasonNoFigure stands for a reason the model came back without.
	comfortReasonNoFigure = "the comfort model gave no figure for this hour"
	// comfortReasonNoWords stands for a model that is held back with no reason given.
	comfortReasonNoWords  = "The comfort model is held back, and gave no reason"
	comfortSwellEnclosed  = "swell not judged in enclosed water"
	comfortBeyondRough    = "rougher than any recorded passage"
	comfortBeyondSteep    = "in seas steeper than any recorded passage"
	comfortMaxStretches   = 3
	comfortHelmHeightNeed = "Snap needs the helm height: enter it under Settings, Vessel"
)

// judgedHour is an hour with what the text is made from, beside what the wire
// carries.
type judgedHour struct {
	start, end time.Time
	at, label  string
	// band is the learned band word, "" when the hour is a gap or the bands are not
	// learned.
	band   string
	beyond bool
	// reason is why the hour was not judged, "" when it was.
	reason                     string
	swayDeg, rollDeg, pitchDeg *float64
	snapG, poundingG           *float64
	swell, windWave            *seaTrain
	aboveRated                 bool
	lead                       comfortLead
	figures                    string
	pred                       comfortPrediction
}

// gap says the hour was not judged. Every gap carries a reason (gapReason), and a
// judged hour carries none, so a stretch and an hour tell a gap the same way.
func (h judgedHour) gap() bool { return h.reason != "" }

// comfortSliver is the shortest an hour at either end of the leg may run to be read on
// its own. A shorter one is folded into its neighbour, so a few seconds past the hour
// never read as a stretch of their own.
const comfortSliver = 10 * time.Minute

func (h judgedHour) span() time.Duration { return h.end.Sub(h.start) }

func (h judgedHour) sliver() bool { return h.span() < comfortSliver }

// wire is the hour as the page reads it.
func (h judgedHour) wire() planComfortHour {
	return planComfortHour{At: h.at, Label: h.label, Band: h.band, Beyond: h.beyond, Line: h.line()}
}

// line is the hour in one line, the sea in the logbook's words:
// "10 AM · Lively · roll 2.1°, pitch 0.8° · swell 0.6 m 6 s on the port bow".
func (h judgedHour) line() string {
	if h.gap() {
		return h.label + " · Not judged: " + h.reason
	}
	parts := []string{h.label}
	if h.band != "" {
		parts = append(parts, bandWord(h.band))
	}
	if h.rollDeg != nil && h.pitchDeg != nil {
		parts = append(parts, fmt.Sprintf("roll %.1f°, pitch %.1f°", *h.rollDeg, *h.pitchDeg))
	} else {
		parts = append(parts, fmt.Sprintf("sway %.1f°", *h.swayDeg))
	}
	if h.swell != nil {
		parts = append(parts, seaTrainText("swell", h.swell))
	}
	if h.windWave != nil {
		parts = append(parts, seaTrainText("wind wave", h.windWave))
	}
	if h.snapG != nil {
		parts = append(parts, "snap "+snapText(*h.snapG))
	}
	if h.poundingG != nil {
		parts = append(parts, "pounding "+poundingText(*h.poundingG))
	}
	if h.beyond {
		parts = append(parts, comfortBeyondSteep)
	}
	return strings.Join(parts, " · ")
}

// seaTrainText is a sea as the logbook words it (seaTrainText in
// frontend/src/lib/passages.ts): an enclosed swell as its note, else "swell 0.6 m 6 s
// on the port bow" and any note after it in brackets.
func seaTrainText(kind string, t *seaTrain) string {
	if t.Enclosed && t.Note != "" {
		return t.Note
	}
	s := seaFiguresText(kind, t)
	if t.Note != "" {
		s += " (" + t.Note + ")"
	}
	return s
}

// seaFiguresText is a sea's figures in the logbook's words, with no note: "swell 0.6 m
// 6 s on the port bow". The hour lines and the chip's sentence both say a sea this way.
func seaFiguresText(kind string, t *seaTrain) string {
	return fmt.Sprintf("%s %.1f m %d s %s", kind, t.HeightM, int(math.Round(t.PeriodS)), t.Direction)
}

// snapText and poundingText are the snap and pounding figures as the Felt row on a
// passage words them.
func snapText(g float64) string     { return fmt.Sprintf("%.2f g at the helm", g) }
func poundingText(g float64) string { return fmt.Sprintf("%.2f g", g) }

// reasonClause is a reason as it reads after a colon or inside a sentence: its
// first letter in lower case and no full stop.
func reasonClause(s string) string {
	return lowerFirst(strings.TrimSuffix(strings.TrimSpace(s), "."))
}

// notJudgedText is the chip's sentence when comfort could not be judged at all.
func notJudgedText(reason string) string {
	return endSentence("Comfort not judged yet: " + reasonClause(reason))
}

// notJudged is a leg's comfort when it could not be judged at all.
func notJudged(reason string) *planLegComfort {
	return &planLegComfort{Text: notJudgedText(reason), Hours: []planComfortHour{}}
}

// comfortSnapScale is how finely a position is snapped: to 0.001 degree, about 110 m,
// so the rays start near the real position and never across a channel from it.
const comfortSnapScale = 1000

// comfortSnapKey is a position snapped to a thousandth of a degree.
func comfortSnapKey(lat, lon float64) [2]int {
	return [2]int{int(math.Round(lat * comfortSnapScale)), int(math.Round(lon * comfortSnapScale))}
}

// raysAt is the rays over the shore at a position, snapped to 0.001 degree. A request
// reads each snapped position once, the geometry comes from the memo kept between
// requests, and a position the shore could not be read at says why every time.
func (s *comfortSetup) raysAt(ctx context.Context, lat, lon float64) raySource {
	return func() (rayFunc, string) {
		k := comfortSnapKey(lat, lon)
		if r, ok := s.shores[k]; ok {
			return r.rays, r.why
		}
		rays, why := s.readShore(ctx, k[0], k[1])
		if s.shores == nil {
			s.shores = map[[2]int]shoreRays{}
		}
		s.shores[k] = shoreRays{rays, why}
		return rays, why
	}
}

func (s *comfortSetup) readShore(ctx context.Context, latE3, lonE3 int) (rayFunc, string) {
	slat, slon := float64(latE3)/comfortSnapScale, float64(lonE3)/comfortSnapScale
	if ctx.Err() != nil {
		return nil, comfortReasonOutOfTime
	}
	loader := s.loader()
	plan, err := loader.plan(slat, slon)
	if err != nil {
		return nil, coastlineReason(err)
	}
	geo, err := s.memo.get(ctx, fetchGeoMemoKey{latE3: latE3, lonE3: lonE3, stamp: plan.Stamp}, func() (*fetchGeometry, error) {
		// The request's time may run out between one square and the next: no more
		// geometry is built for a request that has nobody waiting.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		set, err := loader.linesFor(plan.Needed)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return newFetchGeometry(slat, slon, set.Lines), nil
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, comfortReasonOutOfTime
		}
		return nil, coastlineReason(err)
	}
	return func(kind string, bearingDeg float64) (rayResult, string) {
		dd := 0
		if kind != fetchKindCalm {
			dd = bearingDD(bearingDeg)
		}
		return geo.ray(kind, dd), ""
	}, ""
}

// legComfort judges a leg hour by hour and words the result. A leg whose model is
// held back, or whose bands are not learned, says so instead of giving a word.
func legComfort(ctx context.Context, in legComfortInput) *planLegComfort {
	c, _ := in.run(ctx)
	return c
}

// comfortLadderResult is what the ladder finds for a leg: Blocked is the reason the
// leg cannot be judged at all ("" when it can), BandsNotLearned says the leg is judged
// for its sea only.
type comfortLadderResult struct {
	Blocked         string
	BandsNotLearned bool
}

// comfortLadder is the one fixed order a leg's standing is read in, before any hour is
// walked. The first rung that blocks gives the whole leg's text, naming every distinct
// reason at that rung:
//  1. the model set: not worked out, couldn't be, nothing learned, or sway held back
//     for every wave model;
//  2. each wave model the leg's forecast hours use: no label (update the plugin), no
//     fit for that model (learned on others), or its sway held back;
//  3. the bands: not learned is no block, the leg then says its roughest sea.
func comfortLadder(set *comfortModelSet, bandsLearned bool, models []waveModelRef) comfortLadderResult {
	// A rung that blocks always says why: a reason that came back with no words says that.
	words := func(why string) string {
		if why = strings.TrimSuffix(strings.TrimSpace(why), "."); why != "" {
			return why
		}
		return comfortReasonNoWords
	}
	if why := set.heldBack(); why != "" {
		return comfortLadderResult{Blocked: words(why)}
	}
	var reasons []string
	seen := map[string]bool{}
	add := func(why string) {
		why = words(why)
		if !seen[why] {
			seen[why] = true
			reasons = append(reasons, why)
		}
	}
	for _, m := range models {
		if !m.named() {
			add(comfortReasonNoLabel)
			continue
		}
		if _, why := comfortFitFor(set, m); why != "" {
			add(why)
		}
	}
	if len(reasons) > 0 {
		return comfortLadderResult{Blocked: strings.Join(reasons, ". ")}
	}
	return comfortLadderResult{BandsNotLearned: !bandsLearned}
}

// comfortFitFor is the fit that can judge a wave model's hours, or why none can: the
// set's standing for the model, with a sway held back for it counted as none.
func comfortFitFor(set *comfortModelSet, m waveModelRef) (*comfortFit, string) {
	fit, why := set.standing(m)
	if fit != nil && !fit.Sway.Learned {
		return nil, capitalise(fit.Sway.Reason) + "."
	}
	return fit, why
}

// legModels is the wave models the leg's forecast hours name, each once, in the order
// they come. An hour the forecast has none of, or that has no wave figures at all, names
// none: it is a gap of its own, not the model's.
func legModels(lhs []legHour) []waveModelRef {
	var out []waveModelRef
	seen := map[waveModelRef]bool{}
	for _, lh := range lhs {
		if !lh.have {
			continue
		}
		if why := comfortGap(lh.hour); why != "" && why != comfortReasonNoLabel {
			continue
		}
		if m := lh.hour.WaveModel; !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// run judges the leg: the ladder first, then each hour of a leg the ladder lets by.
func (in legComfortInput) run(ctx context.Context) (*planLegComfort, []judgedHour) {
	var lhs []legHour
	if err := in.as.walkLegHours(in.plan, in.depart, in.arrive, in.from, in.arrive, func(lh legHour) error {
		lhs = append(lhs, lh)
		return nil
	}); err != nil {
		return notJudged(err.Error()), nil
	}
	if rung := comfortLadder(in.set, in.bands.Learned, legModels(lhs)); rung.Blocked != "" {
		return notJudged(rung.Blocked), nil
	}
	hours := in.judge(ctx, lhs)
	return in.comfortOf(hours), hours
}

// judge judges each forecast hour of a leg the ladder let by. The request's time may
// run out partway: the hours not reached say so.
func (in legComfortInput) judge(ctx context.Context, lhs []legHour) []judgedHour {
	if ctx.Err() != nil {
		return in.outOfTime(in.from.Truncate(time.Hour))
	}
	var hours []judgedHour
	for _, lh := range lhs {
		if ctx.Err() != nil {
			return append(hours, in.outOfTime(lh.t)...)
		}
		hours = append(hours, in.judgeHour(ctx, lh))
	}
	return hours
}

// outOfTime is the hours of the leg from stamp t on, which the request's time ran out
// before they were judged.
func (in legComfortInput) outOfTime(t time.Time) []judgedHour {
	var hours []judgedHour
	for ; t.Before(in.arrive); t = t.Add(time.Hour) {
		hours = append(hours, in.gapHour(t, comfortReasonOutOfTime))
	}
	return hours
}

// hourFrom is the hour from stamp t, clipped to the span the leg is read over.
func (in legComfortInput) hourFrom(t time.Time) judgedHour {
	h := judgedHour{at: t.In(in.loc).Format(time.RFC3339), label: boatAxisClock(t, in.loc)}
	h.start, h.end = t, t.Add(time.Hour)
	if h.start.Before(in.from) {
		h.start = in.from
	}
	if h.end.After(in.arrive) {
		h.end = in.arrive
	}
	return h
}

// gapReason is a reason an hour was not judged as the hour carries it. A gap always
// carries one, so a reason that came back with no words says that instead.
func gapReason(reason string) string {
	if r := reasonClause(reason); r != "" {
		return r
	}
	return comfortReasonNoFigure
}

// gapHour is the hour from stamp t as an hour that was not judged.
func (in legComfortInput) gapHour(t time.Time, reason string) judgedHour {
	h := in.hourFrom(t)
	h.reason = gapReason(reason)
	return h
}

// comfortOf words the judged hours as the leg's chip.
func (in legComfortInput) comfortOf(hours []judgedHour) *planLegComfort {
	out := &planLegComfort{Hours: make([]planComfortHour, len(hours))}
	for i, h := range hours {
		out.Hours[i] = h.wire()
	}
	out.Motion = motionLine(hours, in.helm, in.helmErr)
	out.Text = in.text(hours, stretchesOf(hours))
	return out
}

// judgeHour judges one forecast stamp of the leg.
func (in legComfortInput) judgeHour(ctx context.Context, lh legHour) judgedHour {
	h := in.hourFrom(lh.t)
	gapped := func(reason string) judgedHour {
		h.reason = gapReason(reason)
		return h
	}
	// The leg's model was found fit to judge before any hour was walked (comfortLadder),
	// so only the hour's own gaps are found here.
	if !lh.have {
		return gapped(comfortReasonNoHour)
	}
	hour, heading := lh.hour, lh.seg.bearing
	if why := comfortGap(hour); why != "" {
		return gapped(why)
	}
	fit, why := comfortFitFor(in.set, hour.WaveModel)
	if fit == nil {
		// The ladder let this model by, so this is a bug; say so rather than judge.
		return gapped(why)
	}

	// The shore: the wind wave is capped by the fetch over this hour's position and
	// the swell is checked for enclosed water. The forecast's figures are copied, never changed.
	var sea spellSea
	if !trainAbsent(hour.Swell) {
		sea.Swell = &seaTrain{seaTrainFigures: *hour.Swell, Direction: seaWords(&heading, hour.Swell.FromDeg)}
	}
	if !trainAbsent(hour.WindWave) {
		sea.WindWave = &seaTrain{seaTrainFigures: *hour.WindWave, Direction: seaWords(&heading, hour.WindWave.FromDeg)}
	}
	kts, from := hour.WindMS*metersPerSecondToKnots, hour.WindDirDeg
	judgeSeaWind(windInput{kept: true, kts: &kts, fromDeg: &from, span: "hour"}, &sea, in.raysAt(ctx, lh.lat, lh.lon))
	h.swell, h.windWave = sea.Swell, sea.WindWave
	if ctx.Err() != nil {
		return gapped(comfortReasonOutOfTime)
	}
	if why := shoreGap(sea); why != "" {
		return gapped(why)
	}

	var swellF, windF *seaTrainFigures
	if sea.Swell != nil {
		swellF = &sea.Swell.seaTrainFigures
	}
	if sea.WindWave != nil {
		windF = &sea.WindWave.seaTrainFigures
	}
	if in.onPredict != nil {
		in.onPredict()
	}
	p := in.set.predict(comfortHourInput{Model: hour.WaveModel, HeadingDeg: heading, Swell: swellF, WindWave: windF})
	if p.Sway.Value == nil {
		return gapped(p.Sway.Reason)
	}
	sway := *p.Sway.Value
	h.swayDeg, h.rollDeg, h.pitchDeg, h.beyond, h.pred = &sway, p.RollDeg, p.PitchDeg, p.Sway.Beyond, p
	if in.helm != nil {
		h.snapG = p.SnapSidewaysG(*in.helm)
	}
	h.poundingG = p.Pounding.Value
	if in.bands.Learned {
		band, outside := in.bands.classify(sway)
		h.band, h.aboveRated = band, band == bandLively && outside == aboveRated
	}
	h.lead = fit.Sway.lead(p.swayParts, heading, swellF, windF)
	quote, kind := sea.Swell, "swell"
	if h.lead.System == "wind wave" {
		quote, kind = sea.WindWave, "wind wave"
	}
	if quote != nil {
		h.figures = seaFiguresText(kind, quote)
	}
	return h
}

// shoreGap says why the shore left the hour's sea unjudged, "" when it did not. A
// position charted ashore is its own reason; an enclosed swell comes before an
// unchecked wind wave.
func shoreGap(sea spellSea) string {
	w, s := sea.WindWave, sea.Swell
	switch {
	case (w != nil && w.Fetch != nil && w.Fetch.Ashore) || (s != nil && s.ashore):
		return comfortReasonAshore
	case s.swellVerdict() == seaSwellEnclosed:
		return comfortSwellEnclosed
	case w.windVerdict() == seaWindUnchecked:
		return "wind wave not checked against fetch: " + w.Fetch.Reason
	case s.swellVerdict() == seaSwellUnchecked:
		return "swell " + s.Note
	}
	return ""
}

var bandRank = map[string]int{"": 0, bandComfortable: 1, bandLively: 2, bandRough: 3}

// ── stretches ────────────────────────────────────────────────────────────

// stretch is neighbouring hours that read alike: the same gap reason, or the same
// band and, for Lively and Rough, the same leading word.
type stretch struct {
	hours []judgedHour
	band  string
	// lead is the leading word of a Lively or Rough stretch, "" for any other.
	lead string
	// reason is the gap reason, "" for a judged stretch.
	reason string
	// slack is the time of the slivers folded into the stretch, which count toward its
	// length and toward nothing else.
	slack time.Duration
	// before is the part of slack that lies ahead of the first hour, so the stretch
	// starts where its first sliver did and start plus length is its end.
	before time.Duration
}

func (s stretch) gap() bool { return s.reason != "" }

func (s stretch) start() time.Time { return s.hours[0].start.Add(-s.before) }
func (s stretch) end() time.Time   { return s.start().Add(s.length()) }

func (s stretch) length() time.Duration {
	d := s.slack
	for _, h := range s.hours {
		d += h.span()
	}
	return d
}

// roughest is the stretch's hour with the largest sway, the first of equals.
func (s stretch) roughest() judgedHour {
	best := s.hours[0]
	for _, h := range s.hours[1:] {
		if h.swayDeg != nil && (best.swayDeg == nil || *h.swayDeg > *best.swayDeg) {
			best = h
		}
	}
	return best
}

func (s stretch) anyBeyond() (steeper, rougher bool) {
	for _, h := range s.hours {
		steeper = steeper || h.beyond
		rougher = rougher || h.aboveRated
	}
	return steeper, rougher
}

// joinClauses joins the clauses that are not empty with commas.
func joinClauses(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, ", ")
}

// detail is what a Lively or Rough stretch says after its band: the leading word, the
// sea of its roughest hour, and whether it was steeper than any recorded.
func (s stretch) detail() string {
	steep := ""
	if steeper, _ := s.anyBeyond(); steeper {
		steep = comfortBeyondSteep
	}
	return joinClauses(s.lead, s.roughest().figures, steep)
}

// stretchesOf merges neighbouring hours that read alike.
//
// An hour shorter than comfortSliver, which only the first and last of a leg can be,
// adds its time to the stretch beside it and is not judged on its own. A leg of nothing
// but slivers keeps them as they are.
func stretchesOf(hours []judgedHour) []stretch {
	if out := foldSlivers(hours, true); len(out) > 0 {
		return out
	}
	return foldSlivers(hours, false)
}

func foldSlivers(hours []judgedHour, fold bool) []stretch {
	var out []stretch
	var carry time.Duration
	// kindAfter is whether the first hour after i that is not a sliver is a gap, and
	// whether there is one.
	kindAfter := func(i int) (gap, ok bool) {
		for _, h := range hours[i+1:] {
			if !h.sliver() {
				return h.gap(), true
			}
		}
		return false, false
	}
	for i, h := range hours {
		if fold && h.sliver() {
			// A sliver folds only into a neighbour of its own kind, a gap into a gap and
			// a judged hour into a judged one; otherwise it stands as its own stretch.
			if n := len(out); n > 0 && out[n-1].gap() == h.gap() {
				out[n-1].slack += h.span()
				continue
			} else if n == 0 {
				if gap, ok := kindAfter(i); ok && gap == h.gap() {
					carry += h.span()
					continue
				}
			}
		}
		next := stretch{band: h.band, reason: h.reason, slack: carry, before: carry}
		carry = 0
		if h.band == bandLively || h.band == bandRough {
			next.lead = h.lead.Word
		}
		if n := len(out); n > 0 && out[n-1].band == next.band && out[n-1].lead == next.lead && out[n-1].reason == next.reason {
			out[n-1].hours = append(out[n-1].hours, h)
			continue
		}
		next.hours = []judgedHour{h}
		out = append(out, next)
	}
	return out
}

// bandWord is the band as the chip says it. The ratings' own labels stop at
// Lively (Rough is no rating, the operator's word for it is "Shouldn't have gone"),
// so the word is the band's, capitalised.
func bandWord(band string) string {
	if band == "" {
		return "Not judged"
	}
	return capitalise(band)
}

// phrase is one stretch as a sentence. i says where it sits among n stretches:
// 0 is the first, n-1 the last (which runs to the arrival), anything else the middle.
func (s stretch) phrase(i, n int, underway bool) string {
	head := bandWord(s.band)
	if i > 0 {
		if s.gap() {
			head = lowerFirst(head)
		}
		head = "Then " + head
	}
	switch {
	case n == 1:
		if !s.gap() && s.band != bandComfortable {
			head += " throughout"
		}
	case i == 0:
		lead := "the first"
		if underway {
			lead = "the next"
		}
		head += " for " + lead + " " + durationText(s.length())
	case i < n-1, s.gap():
		// A gap says how long it ran even when it runs to the arrival.
		head += " for " + durationText(s.length())
	}
	steeper, rougher := s.anyBeyond()
	switch {
	case s.gap():
		return head + ": " + s.reason + "."
	case s.band == bandComfortable:
		if steeper {
			head += " " + comfortBeyondSteep
		}
		return head + "."
	}
	if rougher {
		head += ", " + comfortBeyondRough
	}
	return head + ": " + s.detail() + "."
}

// text is the first line of the chip.
func (in legComfortInput) text(hours []judgedHour, stretches []stretch) string {
	if len(hours) == 0 {
		return notJudgedText("the leg has no hour to judge")
	}
	var judged []judgedHour
	for _, h := range hours {
		if !h.gap() {
			judged = append(judged, h)
		}
	}
	if !in.bands.Learned {
		return in.unlearnedText(judged, stretches)
	}
	if len(stretches) <= comfortMaxStretches {
		parts := make([]string, len(stretches))
		for i, s := range stretches {
			parts[i] = s.phrase(i, len(stretches), in.underway)
		}
		return strings.Join(parts, " ")
	}
	return in.summary(stretches)
}

// unlearnedText is the chip before the bands are learned: no band word, the roughest
// sea of the judged hours, whether any was steeper than the record, then each gap as
// a sentence of its own with its length.
func (in legComfortInput) unlearnedText(judged []judgedHour, stretches []stretch) string {
	parts := []string{"Comfort not learned yet."}
	if len(judged) > 0 {
		all := stretch{hours: judged}
		rough := all.roughest()
		sea := "flat"
		if rough.figures != "" {
			sea = joinClauses(rough.lead.Word, rough.figures)
		}
		if steeper, _ := all.anyBeyond(); steeper {
			sea = joinClauses(sea, comfortBeyondSteep)
		}
		parts = append(parts, "Roughest sea: "+sea+".")
	}
	for _, s := range stretches {
		if s.gap() {
			parts = append(parts, fmt.Sprintf("Not judged for %s: %s.", durationText(s.length()), s.reason))
		}
	}
	return strings.Join(parts, " ")
}

// summary is the text for a leg of more than three stretches: the worst band, the
// roughest stretch, and what was not judged.
func (in legComfortInput) summary(stretches []stretch) string {
	var worst *stretch
	for i := range stretches {
		s := &stretches[i]
		if s.gap() {
			continue
		}
		if worst == nil || bandRank[s.band] > bandRank[worst.band] ||
			(bandRank[s.band] == bandRank[worst.band] && s.roughest().swayDeg != nil && *s.roughest().swayDeg > *worst.roughest().swayDeg) {
			worst = s
		}
	}
	var gapLen time.Duration
	var whys []string
	for _, s := range stretches {
		if s.gap() {
			gapLen += s.length()
			if !slices.Contains(whys, s.reason) {
				whys = append(whys, s.reason)
			}
		}
	}
	gapWhy := strings.Join(whys, "; ")
	steeperAny := false
	for _, s := range stretches {
		steeper, _ := s.anyBeyond()
		steeperAny = steeperAny || steeper
	}
	var parts []string
	switch {
	case worst == nil:
		return "Not judged: " + gapWhy + "."
	case worst.band == bandComfortable:
		head := "Comfortable where judged"
		if steeperAny {
			head += ", " + comfortBeyondSteep
		}
		parts = append(parts, head+".")
	default:
		head := fmt.Sprintf("%s at times; roughest from %s for %s", bandWord(worst.band), boatAxisClock(worst.start(), in.loc), durationText(worst.length()))
		if _, rougher := worst.anyBeyond(); rougher {
			head += ", " + comfortBeyondRough
		}
		parts = append(parts, head+": "+worst.detail()+".")
		// The steeper-than-recorded clause is the stretch's own when it holds the
		// steep hours; where they lie in another stretch, a sentence says so.
		if steeper, _ := worst.anyBeyond(); steeperAny && !steeper {
			parts = append(parts, "Other hours were "+comfortBeyondSteep+".")
		}
	}
	if gapLen > 0 {
		parts = append(parts, fmt.Sprintf("Not judged for %s in all: %s.", durationText(gapLen), gapWhy))
	}
	return strings.Join(parts, " ")
}

// ── the second line ──────────────────────────────────────────────────────

// motionLine is the greatest snap and pounding of the judged hours, with no band
// word, or the reason a figure is missing (once, when snap and pounding share it).
func motionLine(hours []judgedHour, helm *float64, helmErr error) string {
	var snapMax, poundMax *float64
	var snapWhy, poundWhy string
	judged := false
	for _, h := range hours {
		if h.gap() {
			continue
		}
		judged = true
		if h.snapG != nil && (snapMax == nil || *h.snapG > *snapMax) {
			snapMax = h.snapG
		}
		if v := h.poundingG; v != nil && (poundMax == nil || *v > *poundMax) {
			poundMax = v
		}
		if snapWhy == "" {
			snapWhy = h.pred.Snap.Reason
		}
		if poundWhy == "" {
			poundWhy = h.pred.Pounding.Reason
		}
	}
	if !judged {
		return ""
	}
	if snapMax == nil && poundMax == nil && snapWhy != "" && snapWhy == poundWhy {
		return "Snap and pounding not shown: " + reasonClause(snapWhy)
	}
	var snapPart, poundPart string
	switch {
	case snapMax != nil:
		snapPart = "Snap up to " + snapText(*snapMax)
	case helmErr != nil && snapWhy == "":
		snapPart = "Snap not shown: the helm height couldn't be read: " + reasonClause(helmErr.Error())
	case helm == nil && snapWhy == "":
		snapPart = comfortHelmHeightNeed
	default:
		snapPart = "Snap not shown: " + reasonClause(snapWhy)
	}
	if poundMax != nil {
		poundPart = "pounding up to " + poundingText(*poundMax)
	} else {
		poundPart = "pounding not shown: " + reasonClause(poundWhy)
	}
	return snapPart + " · " + poundPart
}
