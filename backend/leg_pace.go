package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// A plan leg is timed by what the boat has done, learned from its own recorded
// passages and from nothing else (the SQLite passage store, never InfluxDB):
//
//   - the cruise band, the rpm window the boat spends most of its time under way in;
//   - the cruise speed over the ground in that band, current already in it;
//   - the slow water, 0.005 degree cells where the boat is repeatedly seen on low
//     revs and well under cruise speed, such as a berth, a fairway or a lock.
//
// A leg is timed at the cruise speed except through a slow cell, where it runs at
// that cell's speed. Nothing is a default: a boat with no rpm recorded under way,
// or too little time at cruising revs, has no ETA, and says why.

const (
	// legPaceMonths is how far back the passages that are learned from began.
	legPaceMonths = 12
	// legPaceBandShare is the width of the cruise band as a share of the 95th
	// percentile rpm, so it scales with the engine and not with any one boat.
	legPaceBandShare = 0.10
	// legPaceCellDeg is the side of a slow-water cell in degrees (about 0.3 nm).
	legPaceCellDeg = 0.005
	// legPaceSlowShare is the most of the cruise speed a cell's speed may be and
	// still be slow water.
	legPaceSlowShare = 0.75
	// legPaceSlowPassages is how many passages must have been through a cell.
	legPaceSlowPassages = 2
	// legPaceMinPassages and legPaceMinBandHours are the least that is learned from.
	legPaceMinPassages  = 3
	legPaceMinBandHours = 2.0
	// legPaceStepNm is the longest step the route is walked in.
	legPaceStepNm = 0.15
	// legPaceKnotEps is the share of a leg a stretch must be to get a knot of its
	// own: a run shorter than a billionth of the leg is folded into its neighbour.
	legPaceKnotEps = 1e-9
	// legPaceFitTimeout bounds one fit, which runs apart from the request that
	// asked for it.
	legPaceFitTimeout = 2 * time.Minute
)

var (
	errLegPaceNoPassages = errors.New("no recorded passages yet")
	// errLegPaceNoRecentPassages is passages on record, none of them begun in the
	// twelve months learned from.
	errLegPaceNoRecentPassages = errors.New("no passages recorded in the last twelve months")
	errLegPaceNoRPM            = errors.New("no engine rpm recorded under way")
	// errLegPaceNotLearned is the record having too little in it; the errors that
	// say how little wrap it, and their wording starts with its own.
	errLegPaceNotLearned = errors.New("cruise speed not learned yet")
)

// ── cells ────────────────────────────────────────────────────────────────

// paceCell is a square of the 0.005 degree grid. A point belongs to the cell its
// coordinates fall in by floor, so one on a boundary goes the same way every time.
type paceCell struct{ lat, lon int }

func cellOf(lat, lon float64) paceCell {
	return paceCell{int(math.Floor(lat / legPaceCellDeg)), int(math.Floor(lon / legPaceCellDeg))}
}

// ── intervals ────────────────────────────────────────────────────────────

// paceInterval is a stretch between two track points under way: how long, how fast
// over the ground, at what revs (0 for none recorded), and where.
type paceInterval struct {
	passage                         int
	sec, kn, rpm, lat, lon, bearing float64
}

// paceIntervalsOf walks a passage's points into the intervals that count. It keeps
// an interval that joins two points of one segment (legBetween) and was covered at
// one knot or better (underwayLeg), and drops one faster than a boat can go
// (trackMaxJumpKts), a jump in the fixes. Speed is from position, which every
// point has. The revs are the later point's, nil or not above zero being none.
func paceIntervalsOf(passage int, pts []paceTrackPoint) []paceInterval {
	var out []paceInterval
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		if !legBetween(
			recordedTrackPoint{At: a.At, Segment: a.Segment, Source: a.Source},
			recordedTrackPoint{At: b.At, Segment: b.Segment, Source: b.Source}) {
			continue
		}
		dt := b.At.Sub(a.At).Seconds()
		meters := haversineMeters(a.Lat, a.Lon, b.Lat, b.Lon)
		if !underwayLeg(meters, dt) {
			continue
		}
		kn := meters / dt / trackMetersPerKnot
		if kn > trackMaxJumpKts {
			continue
		}
		iv := paceInterval{
			passage: passage, sec: dt, kn: kn,
			lat: (a.Lat + b.Lat) / 2, lon: lerpLon(a.Lon, b.Lon, 0.5),
			bearing: bearingDeg(a.Lat, a.Lon, b.Lat, b.Lon),
		}
		iv.rpm = b.RPM
		out = append(out, iv)
	}
	return out
}

// ── the model ────────────────────────────────────────────────────────────

// legPaceModel is what was learned.
type legPaceModel struct {
	// CruiseKn is the speed over the ground in the cruise band. The current the
	// boat met is in it: it is a record, not a speed through the water.
	CruiseKn float64
	// Slow is each slow cell's speed.
	Slow map[paceCell]float64
	// BandLowRPM and BandHighRPM are the cruise band.
	BandLowRPM, BandHighRPM float64
	// Passages and BandHours are what the cruise speed was learned from.
	Passages  int
	BandHours float64
	// Heading is the hours in the band by compass octant of the course over the
	// ground, N first, so a record that is all one way shows in the log.
	Heading [8]float64
}

// fitLegPace learns the model from the intervals of the passages in the window.
// It is deterministic: the order of the intervals changes nothing.
func fitLegPace(ivs []paceInterval) (*legPaceModel, error) {
	var engine []paceInterval
	for _, iv := range ivs {
		if iv.rpm > 0 {
			engine = append(engine, iv)
		}
	}
	if len(engine) == 0 {
		return nil, errLegPaceNoRPM
	}

	// The band: the window of 10% of the p95 rpm that holds most time under way.
	rpms, secs := make([]float64, len(engine)), make([]float64, len(engine))
	for i, iv := range engine {
		rpms[i], secs[i] = iv.rpm, iv.sec
	}
	width := legPaceBandShare * weightedQuantile(rpms, secs, 0.95)
	lo := densestWindow(engine, width)
	hi := lo + width
	inBand := func(iv paceInterval) bool { return iv.rpm >= lo && iv.rpm <= hi }

	// First pass of the cruise speed, over everything in the band, only to judge
	// which cells are slow.
	var bandKn, bandSec []float64
	for _, iv := range engine {
		if inBand(iv) {
			bandKn, bandSec = append(bandKn, iv.kn), append(bandSec, iv.sec)
		}
	}
	if len(bandKn) == 0 {
		// The band starts at a recorded rpm, so it always holds an interval.
		return nil, errors.New("the cruise band holds no interval")
	}
	cruise0 := weightedQuantile(bandKn, bandSec, 0.5)

	type cellAcc struct {
		passages        map[int]struct{}
		rpm, kn, weight []float64
	}
	cells := map[paceCell]*cellAcc{}
	for _, iv := range engine {
		c := cellOf(iv.lat, iv.lon)
		acc := cells[c]
		if acc == nil {
			acc = &cellAcc{passages: map[int]struct{}{}}
			cells[c] = acc
		}
		acc.passages[iv.passage] = struct{}{}
		acc.rpm, acc.kn, acc.weight = append(acc.rpm, iv.rpm), append(acc.kn, iv.kn), append(acc.weight, iv.sec)
	}
	slow := map[paceCell]float64{}
	for c, acc := range cells {
		if len(acc.passages) < legPaceSlowPassages {
			continue
		}
		medRPM := weightedQuantile(acc.rpm, acc.weight, 0.5)
		medKn := weightedQuantile(acc.kn, acc.weight, 0.5)
		if medRPM < lo && medKn <= legPaceSlowShare*cruise0 {
			slow[c] = medKn
		}
	}

	// Second pass: the cruise speed again with the slow cells' intervals left out,
	// once, so a berth's crawl at cruising revs does not pull it down.
	var m legPaceModel
	var kns, ws []float64
	passages := map[int]struct{}{}
	for _, iv := range engine {
		if !inBand(iv) {
			continue
		}
		if _, isSlow := slow[cellOf(iv.lat, iv.lon)]; isSlow {
			continue
		}
		kns, ws = append(kns, iv.kn), append(ws, iv.sec)
		passages[iv.passage] = struct{}{}
		m.BandHours += iv.sec / 3600
		m.Heading[octantOf(iv.bearing)] += iv.sec / 3600
	}
	m.Passages = len(passages)
	if m.Passages < legPaceMinPassages || m.BandHours < legPaceMinBandHours {
		return nil, fmt.Errorf("%w: %.1f h at cruising revs over %d passages, needs %.0f h over %d",
			errLegPaceNotLearned, m.BandHours, m.Passages, legPaceMinBandHours, legPaceMinPassages)
	}
	m.CruiseKn = weightedQuantile(kns, ws, 0.5)
	m.Slow, m.BandLowRPM, m.BandHighRPM = slow, lo, hi
	return &m, nil
}

// densestWindow is the low edge of the rpm window of the given width that holds the
// most time under way. Windows start at a recorded rpm; a tie goes to the higher.
func densestWindow(ivs []paceInterval, width float64) float64 {
	s := append([]paceInterval(nil), ivs...)
	sort.Slice(s, func(i, j int) bool { return s[i].rpm < s[j].rpm })
	prefix := make([]float64, len(s)+1)
	for i, iv := range s {
		prefix[i+1] = prefix[i] + iv.sec
	}
	best, bestLo := -1.0, s[0].rpm
	j := 0
	for i := 0; i < len(s); i++ {
		if i > 0 && s[i].rpm == s[i-1].rpm {
			continue // the window from this rpm was judged at its first interval
		}
		if j < i {
			j = i
		}
		for j < len(s) && s[j].rpm <= s[i].rpm+width {
			j++
		}
		if w := prefix[j] - prefix[i]; w >= best {
			best, bestLo = w, s[i].rpm
		}
	}
	return bestLo
}

func octantOf(bearing float64) int {
	return int(math.Round(normalizeDegrees(bearing)/45)) % 8
}

var octantNames = [8]string{"N", "NE", "E", "SE", "S", "SW", "W", "NW"}

// logText is the one line a fit says: the band, the speed, what it was learned
// from and the mix of headings in the band.
func (m *legPaceModel) logText() string {
	var mix []string
	for i, h := range m.Heading {
		if h > 0 {
			mix = append(mix, fmt.Sprintf("%s %.1f h", octantNames[i], h))
		}
	}
	return fmt.Sprintf("passages: leg pace fit: band %.0f-%.0f rpm, cruise %.1f kn, %d slow cells, %d passages, %.1f h in band; headings %s",
		m.BandLowRPM, m.BandHighRPM, m.CruiseKn, len(m.Slow), m.Passages, m.BandHours, strings.Join(mix, ", "))
}

// ── learning from the store ──────────────────────────────────────────────

// learnLegPace reads the visible passages that began in the last twelve months and
// fits the model to them. It reads the passage store alone. A read that fails is an
// error, never a thinner record.
func learnLegPace(ctx context.Context, store *passageStore, now time.Time) (*legPaceModel, error) {
	since := now.AddDate(0, -legPaceMonths, 0)
	// Every visible passage is read, so an old record can be told from an empty one.
	records, err := visiblePassagesSince(ctx, store, time.Time{})
	if err != nil {
		return nil, err
	}
	var window []passageRecord
	for _, r := range records {
		if !r.Start.Before(since) && !r.Start.After(now) {
			window = append(window, r)
		}
	}
	// One read of the year's points, from the oldest passage's start to now, split
	// into the passages in memory: a query per passage costs more than the walk.
	var all []paceTrackPoint
	if len(window) > 0 {
		from, to := window[0].Start, now
		for _, r := range window {
			if r.Start.Before(from) {
				from = r.Start
			}
			if r.End.After(to) {
				to = r.End
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("the passage record was not read: %w", err)
		}
		if all, err = store.PaceTrackPointsBetween(ctx, from, to); err != nil {
			return nil, fmt.Errorf("the passage record could not be read: %w", err)
		}
	}
	var ivs []paceInterval
	passages := 0
	for _, r := range window {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("the passage record was not read: %w", err)
		}
		// The points are in time order, so a passage's are one run of them, ends included.
		// Compared in milliseconds, as the query compares them.
		lo := sort.Search(len(all), func(i int) bool { return all[i].At.UnixMilli() >= r.Start.UnixMilli() })
		hi := sort.Search(len(all), func(i int) bool { return all[i].At.UnixMilli() > r.End.UnixMilli() })
		if lo < hi {
			ivs = append(ivs, paceIntervalsOf(passages, all[lo:hi])...)
		}
		passages++
	}
	// A fit is never made from a record whose reading was cut short.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("the passage record was not read: %w", err)
	}
	if passages == 0 {
		if len(records) > 0 {
			return nil, errLegPaceNoRecentPassages
		}
		return nil, errLegPaceNoPassages
	}
	return fitLegPace(ivs)
}

// ── fit on demand ────────────────────────────────────────────────────────

// legPaceFit learns the model from the passages as they stand at now.
type legPaceFit func(ctx context.Context, now time.Time) (*legPaceModel, error)

// legPaceCache keeps the last fit for the plan requests that need it. Nothing runs
// in the background: a request fits when the passages changed (the counter moved)
// or the boat day did (the twelve-month window rolls daily), and otherwise reuses
// the fit. One fit runs at a time, apart from the requests: those waiting on it
// share it, each leaving on its own context, and what it makes is kept for the
// next. The day is the UTC date of the request's clock.
type legPaceCache struct {
	// changes counts the changes to what the passages hold, a change a plan's pace
	// learns from (bump).
	changes atomic.Int64
	fit     legPaceFit

	mu     sync.Mutex
	flight *legPaceFlight // the fit running now, if any
	have   bool
	seen   int64  // the counter as it stood before the kept fit began
	day    string // the day the kept fit was made on
	m      *legPaceModel
	err    error
	// lastLog keeps a fit from saying the same thing twice.
	lastLog string
}

func newLegPaceCache(fit legPaceFit) *legPaceCache { return &legPaceCache{fit: fit} }

// bump says the passages changed, so the next request fits again.
func (c *legPaceCache) bump() { c.changes.Add(1) }

// model is the fit for now, or why there is none. A fit that could not read the
// record, or was cut short, is returned and not kept, so the next request tries
// again. A record with too little in it (no passages, no rpm, too few hours) is an
// answer, kept until something changes.
func (c *legPaceCache) model(ctx context.Context, now time.Time) (*legPaceModel, error) {
	day := now.UTC().Format(time.DateOnly)
	c.mu.Lock()
	// Read before the fit, so a change that lands during it makes the next request
	// fit again.
	seen := c.changes.Load()
	if c.have && c.seen == seen && c.day == day {
		m, err := c.m, c.err
		c.mu.Unlock()
		return m, err
	}
	if err := ctx.Err(); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	f := c.flight
	if f == nil || f.seen != seen || f.day != day {
		f = &legPaceFlight{seen: seen, day: day, done: make(chan struct{})}
		c.flight = f
		// The fit belongs to every request waiting on it, not to the one that
		// started it: it runs on a context detached from that caller, with its own
		// bound, and each caller leaves early on its own context.
		fitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), legPaceFitTimeout)
		go func() {
			defer cancel()
			m, err := c.fit(fitCtx, now)
			c.mu.Lock()
			defer c.mu.Unlock()
			c.log(m, err)
			f.m, f.err = m, err
			// A fit that could not read, or hit its bound, is not kept; nor is one
			// a newer flight has replaced.
			if (err == nil || isLegPaceLearnedNot(err)) && c.flight == f {
				c.have, c.seen, c.day, c.m, c.err = true, f.seen, f.day, m, err
			} else if err != nil && !isLegPaceLearnedNot(err) && c.flight == f {
				c.have = false
			}
			if c.flight == f {
				c.flight = nil
			}
			close(f.done)
		}()
	}
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-f.done:
		if f.err != nil && !isLegPaceLearnedNot(f.err) {
			return nil, f.err
		}
		return f.m, f.err
	}
}

// legPaceFlight is a fit in progress, shared by the requests waiting on it.
type legPaceFlight struct {
	seen int64
	day  string
	done chan struct{}
	m    *legPaceModel
	err  error
}

// log says what a fit found, once for each finding.
func (c *legPaceCache) log(m *legPaceModel, err error) {
	var text string
	switch {
	case err == nil:
		text = m.logText()
	case isLegPaceLearnedNot(err):
		text = "passages: leg pace fit: " + err.Error()
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return
	default:
		text = "passages: leg pace fit: could not read the record: " + err.Error()
	}
	if text == c.lastLog {
		return
	}
	c.lastLog = text
	log.Print(text)
}

// isLegPaceLearnedNot says err is the record having too little in it, not a read
// that failed.
func isLegPaceLearnedNot(err error) bool {
	return errors.Is(err, errLegPaceNoPassages) || errors.Is(err, errLegPaceNoRecentPassages) ||
		errors.Is(err, errLegPaceNoRPM) ||
		errors.Is(err, errLegPaceNotLearned)
}

// ── the leg's pace ───────────────────────────────────────────────────────

// paceStep is a stretch of the route of at most legPaceStepNm: where its midpoint
// is, how many of the leg's own nautical miles it is, the speed the model gives it
// and whether that is a slow cell's. The steps are kept so a departure's pace can
// be worked out over them, which is where the current of that hour will be added.
type paceStep struct {
	lat, lon, nm, kn float64
	// from and to are the two ends of the step on the route, the segment a fix is
	// placed on.
	fromLat, fromLon, toLat, toLon float64
	// course is the stretch of route the step is on, which the current is read along.
	course trackCourse
	slow   bool
}

// legPaceProfile is a leg walked once: its steps and the pace over them for any
// departure. The words the hours were worked out on are not here; the plan puts
// them beside the departure it shows (planLegWork.putEtaBasis).
type legPaceProfile struct {
	pace     legPace
	cruiseKn float64
	steps    []paceStep
	// distNm is the leg's distance, the steps' nm adding up to it.
	distNm float64
	// steady says no step is in slow water, so every departure's pace is linearPace.
	steady bool
	// cells and query are what one request for the leg's current asks: the first
	// open-water step's position in each cell of the model, in route order. cellIdx
	// is each step's place in them, -1 for a step in slow water. They depend on the
	// leg alone, so the page, a slip and Mate's plan read share one cached answer.
	cells   []tidalGridPoint
	query   []currentsPoint
	cellIdx []int
}

// slowHours is the time the leg spends in slow water at the model's speeds. No
// departure changes it yet.
func (p *legPaceProfile) slowHours() float64 {
	var h float64
	for _, s := range p.steps {
		if s.slow {
			h += s.nm / s.kn
		}
	}
	return h
}

// profile walks a leg's route once, in steps of at most legPaceStepNm. A step runs
// at the speed of the slow cell its midpoint is in, else at the cruise speed, so a
// route clipping a cell is timed by about its length inside it. The steps share the
// leg's own distance in proportion to their length along the route. A departure's
// pace adds the steps up, merging runs of equal speed into one stretch, so a leg
// through no slow water keeps the two knots of a steady leg. A steady leg is
// linearPace itself, which keeps its exact minute run.
func (m *legPaceModel) profile(leg planLeg) (*legPaceProfile, error) {
	if !(m.CruiseKn > 0) || math.IsInf(m.CruiseKn, 0) {
		return nil, fmt.Errorf("the cruise speed is not a speed: %v", m.CruiseKn)
	}
	if !(leg.DistanceNm > 0) || len(leg.Route) < 2 {
		return nil, errors.New("the leg has no route to time")
	}
	var total float64
	segNm := make([]float64, len(leg.Route)-1)
	for i := range segNm {
		a, b := leg.Route[i], leg.Route[i+1]
		segNm[i] = haversineMeters(a[0], a[1], b[0], b[1]) / metersPerNauticalMile
		total += segNm[i]
	}
	if !(total > 0) {
		return nil, errors.New("the leg's route has no length to time")
	}
	scale := leg.DistanceNm / total

	p := &legPaceProfile{cruiseKn: m.CruiseKn, distNm: leg.DistanceNm, steady: true}
	p.steps = make([]paceStep, 0, int(total/legPaceStepNm)+len(segNm)+1)
	for i, s := range segNm {
		if s <= 0 {
			continue
		}
		a, b := leg.Route[i], leg.Route[i+1]
		course := courseOf(bearingDeg(a[0], a[1], b[0], b[1]))
		n := int(math.Ceil(s/legPaceStepNm - 1e-9))
		n = max(n, 1)
		for k := range n {
			u := (float64(k) + 0.5) / float64(n)
			lat := a[0] + (b[0]-a[0])*u
			lon := lerpLon(a[1], b[1], u)
			step := paceStep{lat: lat, lon: lon, nm: s / float64(n) * scale, kn: m.CruiseKn, course: course}
			u0, u1 := float64(k)/float64(n), float64(k+1)/float64(n)
			step.fromLat, step.fromLon = a[0]+(b[0]-a[0])*u0, lerpLon(a[1], b[1], u0)
			step.toLat, step.toLon = a[0]+(b[0]-a[0])*u1, lerpLon(a[1], b[1], u1)
			if cell, ok := m.Slow[cellOf(lat, lon)]; ok {
				step.kn, step.slow = cell, true
				p.steady = false
			}
			p.steps = append(p.steps, step)
		}
	}
	p.cells, p.query, p.cellIdx = distinctCellQuery(len(p.steps), func(i int) (tidalGridPoint, float64, float64, bool) {
		s := p.steps[i]
		return roundToTidalGrid(s.lat, s.lon), s.lat, s.lon, !s.slow
	})
	if p.steady {
		p.pace = linearPace(leg.DistanceNm / m.CruiseKn)
		return p, nil
	}
	p.pace = func(depart time.Time) (legTimeline, error) {
		return newLegTimeline(p.integrate(depart, 0, 1, nil).knots)
	}
	return p, nil
}

// legWalk is a leg walked from one departure by integrate.
type legWalk struct {
	// knots are where the boat is along the leg and when; consecutive steps of equal
	// speed are one stretch, so a leg through no slow water has two knots.
	knots []legKnot
	// alongKn is the mean current along the track, positive with the boat, weighted
	// by the distance of each step, over the open-water steps the field has a
	// current for. It is 0 when there are none: look at modelledNm.
	alongKn float64
	// modelledNm and unmodelledNm are the nautical miles of open water the field has
	// a current for and has none for. Slow water is in neither: the model has none
	// at a berth or a fairway and the boat is not timed by it there.
	modelledNm, unmodelledNm float64
}

// integrate walks the leg from step from, with the share left of that step still to
// go, setting out at depart. A walk from the start (from 0, left 1) is the leg's
// pace. It is the one place the steps are added up: the knots, and the current each
// open-water step meets at the time the boat enters it. With no field (nil) the
// current is left out and the walk is the still-water pace.
func (p *legPaceProfile) integrate(depart time.Time, from int, left float64, f legCurrentField) legWalk {
	steps := p.steps
	nm := 0.0
	if from > 0 || left != 1 {
		for _, s := range steps[:from] {
			nm += s.nm
		}
		nm += steps[from].nm * (1 - left)
	}
	w := legWalk{knots: make([]legKnot, 1, 4)}
	w.knots[0] = legKnot{t: depart, frac: nm / p.distNm}
	var hours, along float64
	for i := from; i < len(steps); {
		kn := steps[i].kn
		j, runNm := i, 0.0
		for ; j < len(steps) && steps[j].kn == kn; j++ {
			s := steps[j]
			stepNm := s.nm
			if j == from {
				stepNm *= left
			}
			switch {
			case s.slow:
			case f != nil:
				if cur, ok := f.alongAt(j, depart.Add(hoursDuration(hours+runNm/kn))); ok {
					w.modelledNm += stepNm
					along += stepNm * cur
				} else {
					w.unmodelledNm += stepNm
				}
			}
			runNm += stepNm
		}
		nm += runNm
		hours += runNm / kn
		frac := nm / p.distNm
		i = j
		if i == len(steps) {
			w.knots = append(w.knots, legKnot{t: depart.Add(hoursDuration(hours)), frac: 1})
			break
		}
		// A run too short to be a stretch of its own (its end is within a
		// billionth of the last knot or of the arrival) leaves no knot; its time
		// is in the next one.
		if frac >= 1-legPaceKnotEps || frac <= w.knots[len(w.knots)-1].frac+legPaceKnotEps {
			continue
		}
		w.knots = append(w.knots, legKnot{t: depart.Add(hoursDuration(hours)), frac: frac})
	}
	if w.modelledNm > 0 {
		w.alongKn = along / w.modelledNm
	}
	return w
}
