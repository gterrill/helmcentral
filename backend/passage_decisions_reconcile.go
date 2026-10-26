package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// The reconcile worker keeps inferred passage decisions in step with the
// passages (ADR 0211). It wakes after a passage is stored or edited, once an
// hour, and when the operator records a went.

const (
	passageDecisionsEvery  = time.Hour
	passageDecisionsWindow = 60 * 24 * time.Hour
)

// passageDecisionsWake nudges the reconcile for a span of time. Nil until the
// worker is running.
var passageDecisionsWake func(from, to time.Time)

// passageDecisionsWorkerNow is the running worker, for the PUT to ask for an
// immediate reconcile. Nil until the worker is running.
var passageDecisionsWorkerNow *passageDecisionsWorker

func wakePassageDecisions(from, to time.Time) {
	if f := passageDecisionsWake; f != nil {
		f(from, to)
	}
}

// announcePassageSpan is announcePassage for a stored passage plus the
// decisions that might now match it.
func announcePassageSpan(rec passageRecord) {
	announcePassage(rec.ID)
	wakePassageDecisions(rec.Start, rec.End)
}

type decisionReconciler struct {
	passages  *passageStore
	assistant func() *assistantStore
	now       func() time.Time
}

// reconcile re-infers the whole working set, so winners, blocks and
// supersession are always decided over every card that can compete. The set is
// every decision with a row date in the last 60 days, plus any decision linked
// to a passage that starts in [from, to] (hidden ones too) or to a passage that
// no longer exists, so an edit to an older passage still reaches its card, plus
// the decisions named in include whatever their age. A zero from and to adds no
// span.
func (r *decisionReconciler) reconcile(from, to time.Time, include ...int64) error {
	asst := r.assistant()
	if asst == nil {
		return fmt.Errorf("assistant store is not running")
	}
	now := r.now()
	// Old cards come in only through a link: to a passage in the woken span, or
	// to one that is gone. Find those passage ids first so the query does not
	// load every decision that was ever linked.
	var callIDs []string
	passageIDs := map[string]bool{}
	if !from.IsZero() || !to.IsZero() {
		ids, err := r.passages.PassageIDsStartingIn(from, to)
		if err != nil {
			return err
		}
		for _, id := range ids {
			passageIDs[id] = true
		}
	}
	linked, err := asst.LinkedPassageIDs()
	if err != nil {
		return err
	}
	exist, err := r.passages.ExistingPassageIDs(linked)
	if err != nil {
		return err
	}
	for _, id := range linked {
		if !exist[id] {
			passageIDs[id] = true
		}
	}
	for id := range passageIDs {
		callIDs = append(callIDs, id)
	}
	rows, err := asst.PassageDecisionsForInference(now.Add(-2*passageDecisionsWindow), include, callIDs)
	if err != nil {
		return err
	}
	forced := map[int64]bool{}
	for _, id := range include {
		forced[id] = true
	}

	type parsed struct {
		row  passageDecisionRow
		snap decisionSnapshot
		last time.Time
	}
	var all []parsed
	for _, row := range rows {
		snap, err := parseDecisionSnapshot(row.PayloadJSON, row.RoutesJSON)
		if err != nil {
			noteDecisionParseFailure(row.ID, err)
			continue
		}
		_, last, ok := decisionDateRange(snap)
		if !ok {
			noteDecisionParseFailure(row.ID, fmt.Errorf("card has no rows"))
			continue
		}
		all = append(all, parsed{row, snap, last})
	}

	var inputs []decisionInput
	var winFrom, winTo time.Time
	for _, p := range all {
		row := p.row
		if !forced[row.ID] && !decisionIsRecent(p.last, now) {
			if row.PassageID == "" || !passageIDs[row.PassageID] {
				continue
			}
		}
		lo, hi := decisionDepartRange(p.snap)
		lo, hi = lo.Add(-passageDecisionDepartWindow), hi.Add(passageDecisionDepartWindow)
		if winFrom.IsZero() || lo.Before(winFrom) {
			winFrom = lo
		}
		if hi.After(winTo) {
			winTo = hi
		}
		inputs = append(inputs, decisionInput{
			ID: row.ID, CardCreated: time.UnixMilli(row.CardCreatedMs).UTC(),
			State: row.State, RowDate: row.RowDate, Source: row.Source, PassageID: row.PassageID, Snap: p.snap,
		})
	}
	if len(inputs) == 0 {
		return nil
	}
	list, err := r.passages.VisiblePassagesStartingIn(winFrom, winTo)
	if err != nil {
		return err
	}
	var passages, unknown []decisionPassage
	sources := map[string]string{}
	for _, p := range list {
		dp := decisionPassage{ID: p.ID, Start: p.Start, End: p.End}
		if passageSourceUnknown(p.Source) {
			noteUnknownPassageSource(p.ID)
			unknown = append(unknown, dp)
			continue
		}
		passages = append(passages, dp)
		sources[p.ID] = p.Source
	}
	resume, hasResume, err := r.passages.DetectorResume()
	if err != nil {
		return err
	}
	out, err := inferPassageDecisions(decisionRun{
		Decisions: inputs, Passages: passages, Unknown: unknown, DetectorResume: resume, HasResume: hasResume, Now: now,
		Track: func(p decisionPassage) ([]recordedTrackPoint, error) {
			return r.passages.PassageTrack(passageRecord{Start: p.Start, End: p.End, Source: sources[p.ID]})
		},
	})
	if err != nil {
		return err
	}
	_, err = asst.ApplyPassageDecisionUpdates(out.Updates)
	return err
}

// decisionIsRecent says a card's last row date falls within the working window.
func decisionIsRecent(lastRowDate, now time.Time) bool {
	return lastRowDate.AddDate(0, 0, 1).After(now.Add(-passageDecisionsWindow))
}

var parseFailureLogged sync.Map

// noteDecisionParseFailure logs a broken snapshot once per decision per process.
func noteDecisionParseFailure(id int64, err error) {
	if _, seen := parseFailureLogged.LoadOrStore(id, true); !seen {
		log.Printf("passage decisions: skipping decision %d: %v", id, err)
	}
}

func decisionParseFailuresLogged(id int64) int {
	if _, ok := parseFailureLogged.Load(id); ok {
		return 1
	}
	return 0
}

// ExistingPassageIDs returns which of ids are passages, in one query.
func (s *passageStore) ExistingPassageIDs(ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	marks, args := inPlaceholders(ids)
	rows, err := s.db.Query(`SELECT id FROM passages WHERE id IN (`+marks+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("check passages exist: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// decisionDepartRange is the earliest and latest planned first-leg departure.
func decisionDepartRange(snap decisionSnapshot) (lo, hi time.Time) {
	for i, r := range snap.Rows {
		if i == 0 || r.Depart.Before(lo) {
			lo = r.Depart
		}
		if i == 0 || r.Depart.After(hi) {
			hi = r.Depart
		}
	}
	return
}

// decisionDateRange is the first and last row date as local midnights.
func decisionDateRange(snap decisionSnapshot) (first, last time.Time, ok bool) {
	for _, row := range snap.Rows {
		d, err := time.ParseInLocation("2006-01-02", row.Date, snap.Loc)
		if err != nil {
			continue
		}
		if !ok || d.Before(first) {
			first = d
		}
		if !ok || d.After(last) {
			last = d
		}
		ok = true
	}
	return
}

// PassageIDsStartingIn lists the ids of every passage, hidden or not, whose
// start falls in [from, to].
func (s *passageStore) PassageIDsStartingIn(from, to time.Time) ([]string, error) {
	rows, err := s.db.Query(`SELECT id FROM passages WHERE start_ms >= ? AND start_ms <= ?`, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("list passage ids in window: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PassageTrack is a passage's own track: only the points of the source it was
// detected from. A recorded passage never sees an overlapping import's points,
// and the other way round, so the first point and the segment breaks are the
// passage's. A passage of unknown source has no track to match. Passage stats
// and the page still read the mixed track.
func (s *passageStore) PassageTrack(rec passageRecord) ([]recordedTrackPoint, error) {
	if passageSourceUnknown(rec.Source) {
		return nil, fmt.Errorf("passage track: source of the passage is unknown")
	}
	return s.trackPoints(rec.Start, rec.End, rec.Source)
}

// passageSourceUnknown says a passage has no single track source to match on:
// none recorded yet, or its span holds both.
func passageSourceUnknown(src string) bool { return src == "" || src == passageSourceMixed }

var unknownSourceLogged sync.Map

// noteUnknownPassageSource logs once per passage that it is left out of
// matching because nothing says where it came from.
func noteUnknownPassageSource(id string) {
	if _, seen := unknownSourceLogged.LoadOrStore(id, true); !seen {
		log.Printf("passage decisions: passage %s has no recorded track source, so it is not matched", id)
	}
}

func passageSourceUnknownLogged(id string) bool {
	_, ok := unknownSourceLogged.Load(id)
	return ok
}

// VisiblePassagesStartingIn lists the passages that are not hidden and whose
// start falls in [from, to].
func (s *passageStore) VisiblePassagesStartingIn(from, to time.Time) ([]passageRecord, error) {
	rows, err := s.db.Query(`SELECT `+passageColumns+` FROM passages WHERE hidden = 0 AND start_ms >= ? AND start_ms <= ? ORDER BY start_ms`,
		from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("list passages in window: %w", err)
	}
	defer rows.Close()
	var out []passageRecord
	for rows.Next() {
		p, err := scanPassage(rows)
		if err != nil {
			return nil, fmt.Errorf("scan passage: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// passageDecisionsWorker runs reconciles on wakes and a ticker. Wakes that
// arrive while one is pending merge into the union of their spans.
type passageDecisionsWorker struct {
	rec *decisionReconciler
	// reconcile is rec.reconcile unless a test swaps it.
	reconcile func(from, to time.Time, include ...int64) error
	every     time.Duration
	signal    chan struct{}
	sync      chan reconcileRequest

	mu       sync.Mutex
	pending  bool
	from, to time.Time
}

func newPassageDecisionsWorker(rec *decisionReconciler, every time.Duration) *passageDecisionsWorker {
	return &passageDecisionsWorker{rec: rec, reconcile: rec.reconcile, every: every, signal: make(chan struct{}, 1), sync: make(chan reconcileRequest)}
}

func (w *passageDecisionsWorker) wake(from, to time.Time) {
	w.mu.Lock()
	if !w.pending || from.Before(w.from) {
		w.from = from
	}
	if !w.pending || to.After(w.to) {
		w.to = to
	}
	w.pending = true
	w.mu.Unlock()
	select {
	case w.signal <- struct{}{}:
	default:
	}
}

type reconcileRequest struct {
	ctx     context.Context
	include []int64
	done    chan error
}

// ReconcileNow runs a full reconcile on the worker's own goroutine, so it is
// serialised with ticks and wakes, and waits for it. include names decisions
// to take in whatever their age. ctx bounds the wait, not
// the reconcile: a request that gives up leaves the pass to finish.
func (w *passageDecisionsWorker) ReconcileNow(ctx context.Context, include ...int64) error {
	req := reconcileRequest{ctx: ctx, include: include, done: make(chan error, 1)}
	select {
	case w.sync <- req:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-req.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// take returns and clears the pending span. all is true when nothing is
// pending, which is what a ticker scan wants.
func (w *passageDecisionsWorker) take() (from, to time.Time, pending bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.pending {
		return time.Time{}, time.Time{}, false
	}
	from, to = w.from, w.to
	w.pending = false
	return from, to, true
}

// run reconciles everything at once (this resumes the backlog after a
// restart), then on every wake and every tick, until done closes.
func (w *passageDecisionsWorker) run(done <-chan struct{}) {
	ticker := time.NewTicker(w.every)
	defer ticker.Stop()
	scanAll := func() {
		if err := w.reconcile(time.Time{}, time.Time{}); err != nil {
			log.Printf("passage decisions: reconcile: %v", err)
		}
	}
	scanAll()
	for {
		select {
		case <-done:
			return
		case <-w.signal:
			if from, to, ok := w.take(); ok {
				if err := w.reconcile(from, to); err != nil {
					log.Printf("passage decisions: reconcile %s to %s: %v", from.Format(time.RFC3339), to.Format(time.RFC3339), err)
				}
			}
		case req := <-w.sync:
			if err := req.ctx.Err(); err != nil {
				req.done <- err
				continue
			}
			req.done <- w.reconcile(time.Time{}, time.Time{}, req.include...)
		case <-ticker.C:
			scanAll()
		}
	}
}

// startPassageDecisions runs the reconcile worker for the life of the process.
// The assistant store is opened after the passage log, so it is read when
// needed rather than captured.
func startPassageDecisions(passages *passageStore) {
	w := newPassageDecisionsWorker(&decisionReconciler{
		passages:  passages,
		assistant: func() *assistantStore { return globalAssistantStore },
		now:       time.Now,
	}, passageDecisionsEvery)
	passageDecisionsWake = w.wake
	passageDecisionsWorkerNow = w
	go w.run(make(chan struct{}))
}
