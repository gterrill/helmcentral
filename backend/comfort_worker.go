package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"strings"
	"sync/atomic"
)

// Fitting the comfort model (ADR 0301) follows the tide calibration worker: one
// goroutine owns the published fit, requests only read it, and the fit runs when
// something it learns from has changed and not on a timer. A signal that arrives
// while a fit is pending or running collapses into one more fit.
//
// The fit only reads. It never works a passage's figures out: a passage with none
// current is waiting, and counts as such in the status, until the start-up
// recompute or the first read of it stores them, which asks for a fit. What the
// fit has judged of each passage is kept in memory under a key of what the
// judgement read, so a fit judges again only the passages whose figures, sea
// hours, fetch figures or shore have changed.

// comfortModelWorker owns the comfort model fitted from the recorded passages. It
// is the only writer of the published set, which is in memory only.
type comfortModelWorker struct {
	set  atomic.Pointer[comfortModelSet]
	wake chan struct{}
	// fits counts the fits that ran, so a test can tell one happened.
	fits atomic.Int32
	// judged counts the passages judged afresh, so a test can tell a fit reused them.
	judged atomic.Int32

	// cache and lastLog belong to the fit, which never runs twice at once.
	cache   map[string]*comfortEntry
	lastLog string
}

func newComfortModelWorker() *comfortModelWorker {
	return &comfortModelWorker{wake: make(chan struct{}, 1)}
}

// Load is the latest fit, nil until the first one has run.
func (w *comfortModelWorker) Load() *comfortModelSet { return w.set.Load() }

// signal asks for a fit.
func (w *comfortModelWorker) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// run fits each time it is signalled, until ctx ends. It makes no fit of its own
// accord: start-up, the sweeps and the passage edits ask.
func (w *comfortModelWorker) run(ctx context.Context, store *passageStore, coast func() *offlineCoastlineReader) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
			w.fit(ctx, store, coast)
		}
	}
}

// fit fits the model afresh from what the passages hold and publishes it, with one
// log line for the fit and one for each wave model, none when the text is what
// the last fit logged. A signal already waiting is answered by this fit; one that
// comes while the fit runs asks for the next.
func (w *comfortModelWorker) fit(ctx context.Context, store *passageStore, coast func() *offlineCoastlineReader) {
	if ctx.Err() != nil {
		return
	}
	select {
	case <-w.wake:
	default:
	}
	spells, rejected, waiting, err := w.gather(ctx, store, coast())
	if ctx.Err() != nil {
		return
	}
	w.fits.Add(1)
	var set *comfortModelSet
	var text string
	if err != nil {
		set = failedComfortSet(err)
		text = fmt.Sprintf("passages: comfort fit: could not read the record: %v", err)
	} else {
		set = fitComfortModels(spells, rejected, waiting)
		text = comfortLogText(set, len(spells))
	}
	w.set.Store(set)
	if text == w.lastLog {
		return
	}
	w.lastLog = text
	for _, line := range strings.Split(text, "\n") {
		if line != "" {
			log.Print(line)
		}
	}
}

// comfortLogText is what a fit says in the log: how many spells were usable, what
// set the rest aside and what waits, and one line for each wave model. Empty when
// there was nothing to say.
func comfortLogText(set *comfortModelSet, usable int) string {
	if set.Rejected.Total == 0 && set.Waiting.Passages == 0 && !set.Waiting.Plugin {
		return ""
	}
	noun := "wave models"
	if len(set.Fits) == 1 {
		noun = "wave model"
	}
	lines := []string{fmt.Sprintf("passages: comfort fit: %d of %d spells usable, %d %s", usable, set.Rejected.Total, len(set.Fits), noun)}
	if set.Rejected.count() > 0 {
		lines[0] += "; set aside: " + set.Rejected.listing()
	}
	for _, note := range set.Waiting.notes() {
		lines[0] += "; " + note
	}
	for _, id := range set.sortedModels() {
		f := set.Fits[id]
		lines = append(lines, fmt.Sprintf("passages: comfort fit: model %q: %s; %s; %s", id, f.Sway.describe(true), f.Snap.describe(true), f.Pounding.describe(true)))
	}
	return strings.Join(lines, "\n")
}

// ── what a fit keeps of each passage ─────────────────────────────────────

// comfortHead is what the passages table says of a passage without its figures:
// the range it covers and figures_gen, which moves whenever its figures are
// stored or cleared.
type comfortHead struct {
	id         string
	start, end int64
	gen        int64
}

// comfortFingerprint is everything besides the figures that a passage's judgement
// read: the sea hours and the fetch figures near it, the state of the shore at
// each of its positions, and whether the wave plugin can be asked. A judgement
// held under an equal one still stands.
type comfortFingerprint struct {
	hindcast, rays, plans, problem string
}

// comfortEntry is a passage as the last fit left it. A waiting passage holds no
// spells; failure is then the last error from working its figures out, which the
// status words instead of "waiting".
type comfortEntry struct {
	head     comfortHead
	felt     []feltSpell
	waiting  bool
	failure  string
	fp       comfortFingerprint
	judged   []comfortSpell
	rejected comfortRejections
}

// comfortHeads lists every passage that is not hidden, oldest first, as
// ComfortBands counts them. It reads no figures.
func (s *passageStore) comfortHeads() ([]comfortHead, error) {
	rows, err := s.db.Query(`SELECT id, start_ms, end_ms, figures_gen FROM passages WHERE hidden = 0 ORDER BY start_ms, id`)
	if err != nil {
		return nil, fmt.Errorf("list passages: %w", err)
	}
	defer rows.Close()
	var out []comfortHead
	for rows.Next() {
		var h comfortHead
		if err := rows.Scan(&h.id, &h.start, &h.end, &h.gen); err != nil {
			return nil, fmt.Errorf("list passages: %w", err)
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list passages: %w", err)
	}
	return out, nil
}

// gather reads what each passage holds, judges again those that changed, and
// returns every spell the model can learn from, the count of those set aside by
// reason, and what is waiting. A store that cannot be read stops the fit: a model
// fitted on part of the record would say it knew the whole, and a failed read of
// the stored fetch figures would otherwise set every spell aside as "not worked
// out yet". (The fetch sweep leaves one passage out and goes on.)
func (w *comfortModelWorker) gather(ctx context.Context, s *passageStore, reader *offlineCoastlineReader) (spells []comfortSpell, rejected comfortRejections, waiting comfortWaiting, err error) {
	heads, err := s.comfortHeads()
	if err != nil {
		return nil, rejected, waiting, err
	}
	if w.cache == nil {
		w.cache = map[string]*comfortEntry{}
	}
	loader := reader.newLoader()
	problem := s.seaProblemNow()
	waiting.Plugin = problem != ""
	live := make(map[string]bool, len(heads))
	for _, h := range heads {
		if err := ctx.Err(); err != nil {
			return nil, rejected, waiting, err
		}
		e, err := w.entryFor(s, loader, problem, h)
		if err != nil {
			return nil, rejected, waiting, fmt.Errorf("passage %s: %w", h.id, err)
		}
		if e == nil {
			// Merged or split away since the listing: it is not a head any more.
			continue
		}
		live[h.id] = true
		w.cache[h.id] = e
		if e.waiting {
			waiting.Passages++
			if e.failure != "" {
				waiting.Failed = append(waiting.Failed, e.failure)
			}
			continue
		}
		spells = append(spells, e.judged...)
		rejected.Total += e.rejected.Total
		for why, n := range e.rejected.ByReason {
			for range n {
				rejected.add(why)
			}
		}
	}
	for id := range w.cache {
		if !live[id] {
			delete(w.cache, id)
		}
	}
	s.pruneFiguresFailed(live)
	return spells, rejected, waiting, nil
}

// entryFor is the passage's entry as it stands now: the one the last fit kept when
// nothing it read has changed, else a fresh judgement. The figures are parsed only
// when the head has changed. A passage that has gone since it was listed gives no
// entry and no error.
func (w *comfortModelWorker) entryFor(s *passageStore, loader *coastlineLoader, problem string, h comfortHead) (*comfortEntry, error) {
	prev := w.cache[h.id]
	e := prev
	if prev == nil || prev.head != h {
		e = &comfortEntry{head: h}
		raw, _, err := s.readFiguresRow(h.id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if f, ok := parseFigures(raw); ok && f.current() {
			e.felt = f.Stats.FeltSpells
		} else {
			e.waiting = true
		}
	}
	var fp comfortFingerprint
	var failure string
	if e.waiting {
		failure = s.figuresFailure(h.id)
	} else {
		var err error
		if fp, err = comfortFingerprintOf(s, loader, problem, e.felt); err != nil {
			return nil, err
		}
	}
	if e == prev && e.failure == failure && e.fp == fp {
		return e, nil
	}
	var judged []comfortSpell
	var rejected comfortRejections
	if !e.waiting {
		w.judged.Add(1)
		var err error
		if judged, rejected, err = judgePassage(s, loader, h.id, e.felt); err != nil {
			return nil, err
		}
	}
	next := *e
	next.failure, next.fp, next.judged, next.rejected = failure, fp, judged, rejected
	return &next, nil
}

// judgePassage judges each spell of a passage against the sea stored behind it and
// the shore as the stored fetch figures say it, as the page does, and keeps those
// the model can learn from.
func judgePassage(s *passageStore, loader *coastlineLoader, id string, felt []feltSpell) (spells []comfortSpell, rejected comfortRejections, err error) {
	seas := s.seaLookupFor(felt)
	if seas == nil {
		return nil, rejected, nil
	}
	if seas.err != nil {
		return nil, rejected, fmt.Errorf("its hindcast rows could not be read: %w", seas.err)
	}
	view := newFetchViewWith(loader, s, felt)
	if view.rowsErr != nil {
		return nil, rejected, fmt.Errorf("%s: %w", fetchStoredUnreadable, view.rowsErr)
	}
	for _, sp := range felt {
		rejected.Total++
		sea, model := seas.forSpellWithModel(sp)
		view.judge(sp, &sea)
		if cs, why := comfortSpellFrom(id, sp, sea, model); why == "" {
			spells = append(spells, cs)
		} else {
			rejected.add(why)
		}
	}
	return spells, rejected, nil
}

// comfortFingerprintOf is what the judgement of a passage's spells reads besides
// its figures, in a form that changes when any of it does.
func comfortFingerprintOf(s *passageStore, loader *coastlineLoader, problem string, felt []feltSpell) (comfortFingerprint, error) {
	fp := comfortFingerprint{problem: problem}
	var hours struct{ first, last int64 }
	var cells struct{ minLat, maxLat, minLon, maxLon int }
	found := false
	for _, sp := range felt {
		k, ok := sp.seaKey()
		if !ok {
			continue
		}
		if !found {
			hours.first, hours.last = k.HourMs, k.HourMs
			cells.minLat, cells.maxLat, cells.minLon, cells.maxLon = k.LatC, k.LatC, k.LonC, k.LonC
			found = true
			continue
		}
		hours.first, hours.last = min(hours.first, k.HourMs), max(hours.last, k.HourMs)
		cells.minLat, cells.maxLat = min(cells.minLat, k.LatC), max(cells.maxLat, k.LatC)
		cells.minLon, cells.maxLon = min(cells.minLon, k.LonC), max(cells.maxLon, k.LonC)
	}
	if found {
		var n, sum int64
		if err := s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(fetched_ms), 0) FROM sea_hindcast_hours
			WHERE hour_ms BETWEEN ? AND ? AND lat_c BETWEEN ? AND ? AND lon_c BETWEEN ? AND ?`,
			hours.first, hours.last, cells.minLat, cells.maxLat, cells.minLon, cells.maxLon).Scan(&n, &sum); err != nil {
			return fp, fmt.Errorf("read sea hindcast: %w", err)
		}
		fp.hindcast = fmt.Sprintf("%d/%d", n, sum)
	}
	if minLat, maxLat, minLon, maxLon, ok := fetchBox(felt); ok {
		var n int64
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM sea_fetch_rays WHERE lat_e6 BETWEEN ? AND ? AND lon_e6 BETWEEN ? AND ?`,
			minLat, maxLat, minLon, maxLon).Scan(&n); err != nil {
			return fp, fmt.Errorf("%s: %w", fetchStoredUnreadable, err)
		}
		fp.rays = fmt.Sprint(n)
	}
	fp.plans = planFingerprint(loader, felt)
	return fp, nil
}

// planFingerprint is the state of the shore at each position the spells lie at: the
// stamp of the squares read there, or the reason there is none. It reads the
// squares' records and no line of coastline.
func planFingerprint(loader *coastlineLoader, felt []feltSpell) string {
	h := fnv.New64a()
	seen := map[[2]int]bool{}
	for _, sp := range felt {
		if !sp.hasPosition() {
			continue
		}
		latE6, lonE6, lat, lon := fetchPos(sp)
		at := [2]int{latE6, lonE6}
		if seen[at] {
			continue
		}
		seen[at] = true
		state := ""
		if plan, err := loader.plan(lat, lon); err != nil {
			state = coastlineReason(err)
		} else {
			state = plan.Stamp
		}
		fmt.Fprintf(h, "%d,%d:%s;", latE6, lonE6, state)
	}
	return fmt.Sprintf("%x", h.Sum64())
}
