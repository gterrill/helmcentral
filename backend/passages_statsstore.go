package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"
)

// A passage's figures are worked out from its track points once and kept on
// its row. They are cleared whenever the passage's range or the points inside
// it change (trim, split, merge, an import into its range), and worked out
// again the next time they are read.

// passageFigures is what is kept on the row: the stats, and the longitude of
// the first point the page uses for the boat's own time zone.
type passageFigures struct {
	// Version is the shape the figures were worked out in. Rows stored in an
	// older shape are worked out again from the points when next read.
	Version  int          `json:"version"`
	Stats    passageStats `json:"stats"`
	StartLon *float64     `json:"start_lon"`
	// StartLat is the latitude of the first point, kept with StartLon so the
	// passage's time zone can be looked up from the pair.
	StartLat *float64 `json:"start_lat"`
	// Timezone is the IANA zone at the start point, kept with the figures so a
	// list does not look it up again for every passage on every request. It is
	// worked out when the figures are, and once for a row stored before it was
	// kept. Empty when the position has no zone.
	Timezone string `json:"timezone,omitempty"`
}

// passageFiguresVersion 2 added the time of each maximum and the motion maxima;
// 3 added the felt figure, its spells, and the snap and pounding maxima; 4 gave
// snap and pounding a reason of their own in place of one shared code; 5 gave
// each spell its middle position, mean heading, roll and pitch RMS apart, and
// snap and pounding peaks (ADR 0299); 6 gave each spell its mean true wind and
// where it came from (ADR 0300).
const passageFiguresVersion = 6

// parseFigures reads a row's stats_json. ok is false for a missing or unreadable
// value.
func parseFigures(raw sql.NullString) (f passageFigures, ok bool) {
	if !raw.Valid || json.Unmarshal([]byte(raw.String), &f) != nil {
		return passageFigures{}, false
	}
	return f, true
}

// current says the figures are in the shape this build works out. Rows stored
// before the start latitude was kept have a longitude and no latitude, and are
// worked out again. One rule, for a read and for the start-up pass.
func (f passageFigures) current() bool {
	return f.Version >= passageFiguresVersion && (f.StartLat != nil || f.StartLon == nil)
}

// PassageStatsFor is the stored stats alone.
func (s *passageStore) PassageStatsFor(rec passageRecord) (passageStats, error) {
	f, err := s.PassageFiguresFor(rec)
	return f.Stats, err
}

// PassageFiguresFor returns the stored figures, computing and storing them
// first when the row has none.
//
// Readers that find one passage stale together work it out once: the current
// figures are read outside the guard, and the stale path runs in figuresFlight,
// which reads the row again first. The figures it returns are shared between its
// callers and must not be changed. A store made from points read before the row
// was cleared (an import into the range, a trim) is refused by the generation
// the row had when it was read, so the cleared row stays cleared and is worked
// out again from the new points.
func (s *passageStore) PassageFiguresFor(rec passageRecord) (passageFigures, error) {
	raw, gen, err := s.readFiguresRow(rec.ID)
	if err != nil {
		return passageFigures{}, err
	}
	if f, ok := parseFigures(raw); ok && f.current() {
		if f.Timezone == "" && f.StartLat != nil && f.StartLon != nil {
			// Stored before the zone was kept: look it up this once
			// and keep it.
			if name, _ := f.lookupStartZone(rec.ID); name != "" {
				f.Timezone = name
				s.storeFigures(rec, f, gen)
			}
		}
		return f, nil
	}
	// The generation the reader saw is part of the key, so a reader that comes after
	// a clear starts its own flight and never joins one that read the old points.
	key := fmt.Sprintf("%s|%d|%d|%d", rec.ID, rec.Start.UnixMilli(), rec.End.UnixMilli(), gen)
	v, err, _ := s.figuresFlight.Do(key, func() (any, error) {
		f, stored, err := s.workOutFigures(rec)
		// A refused store worked nothing out, so it neither records nor clears a failure.
		if err != nil || stored {
			s.noteFiguresResult(rec.ID, err)
		}
		return f, err
	})
	return v.(passageFigures), err
}

// readFiguresRow is a passage's stored stats and the generation of the row.
func (s *passageStore) readFiguresRow(id string) (raw sql.NullString, gen int64, err error) {
	if err := s.db.QueryRow(`SELECT stats_json, figures_gen FROM passages WHERE id = ?`, id).Scan(&raw, &gen); err != nil {
		return raw, 0, fmt.Errorf("read passage stats: %w", err)
	}
	return raw, gen, nil
}

// workOutFigures is the stale path of PassageFiguresFor: it reads the row again,
// since the flight before it may have stored the figures, then works them out
// from the track and stores them. stored says the figures returned are the ones
// the row holds: false when the store was refused because the row changed since
// the points were read.
func (s *passageStore) workOutFigures(rec passageRecord) (f passageFigures, stored bool, err error) {
	raw, gen, err := s.readFiguresRow(rec.ID)
	if err != nil {
		return passageFigures{}, false, err
	}
	var cached *passageFigures
	if f, ok := parseFigures(raw); ok {
		if f.current() {
			return f, true, nil
		}
		cached = &f
	}
	pts, err := s.TrackPointsBetween(rec.Start, rec.End)
	if err != nil {
		return passageFigures{}, false, err
	}
	if cached != nil && len(pts) == 0 {
		// Nothing to work it out from; the stored figures stand. Keep them at the
		// current version, or every read would look here again. A longitude kept
		// without its latitude is no use alone, so it goes. Only this passage's
		// stats row changes; no track point is touched.
		cached.Version = passageFiguresVersion
		if cached.StartLat == nil {
			cached.StartLon = nil
		}
		cached.Stats.setNoPointsReasons()
		ok, err := s.storeFigures(rec, *cached, gen)
		if err != nil {
			return *cached, false, err
		}
		return *cached, ok, nil
	}
	s.figuresComputed.Add(1)
	if s.figuresHook != nil {
		s.figuresHook()
	}
	st := passageFigures{Version: passageFiguresVersion, Stats: computePassageStats(pts), StartLon: startLon(pts), StartLat: startLat(pts)}
	st.Timezone, _ = st.lookupStartZone(rec.ID)
	ok, err := s.storeFigures(rec, st, gen)
	if err != nil {
		return st, false, err
	}
	return st, ok, nil
}

// noteFiguresResult keeps the error of a passage whose figures could not be worked
// out, for the comfort fit to word, and drops it once they are. A new or changed
// error asks for a fit, which reads it.
func (s *passageStore) noteFiguresResult(id string, err error) {
	s.figuresFailMu.Lock()
	prev, had := s.figuresFailed[id]
	switch {
	case err != nil:
		if s.figuresFailed == nil {
			s.figuresFailed = map[string]string{}
		}
		s.figuresFailed[id] = err.Error()
	case had:
		delete(s.figuresFailed, id)
	}
	changed := (err != nil && (!had || prev != err.Error())) || (err == nil && had)
	s.figuresFailMu.Unlock()
	if changed && s.comfort != nil {
		s.comfort.signal()
	}
}

// pruneFiguresFailed forgets the failures of passages that are not in live, as a
// merge or a split leaves ids behind.
func (s *passageStore) pruneFiguresFailed(live map[string]bool) {
	s.figuresFailMu.Lock()
	defer s.figuresFailMu.Unlock()
	for id := range s.figuresFailed {
		if !live[id] {
			delete(s.figuresFailed, id)
		}
	}
}

// figuresFailure is the last error from working out a passage's figures, or "".
func (s *passageStore) figuresFailure(id string) string {
	s.figuresFailMu.Lock()
	defer s.figuresFailMu.Unlock()
	return s.figuresFailed[id]
}

// Sideways g at the helm (ADR 0297): how hard the roll throws someone standing
// at the helm, worked out whenever a passage is read from the operator's helm
// height and the roll acceleration the recorder stored (ADR 0295). It is never
// stored, so changing the height changes every passage. The roll axis is taken
// to lie at the waterline, which over-reads for a boat that rolls about a point
// higher up and under-reads for one that rolls about a lower one.

// passageSidewaysG is a passage's largest sideways g at the helm and when it
// was recorded. With no figure, Reason says why.
type passageSidewaysG struct {
	PeakG  *float64 `json:"peak_g"`
	PeakAt *int64   `json:"peak_at"`
	Reason string   `json:"reason,omitempty"`
}

const (
	sidewaysReasonNoHeight   = "Helm height isn't set. Enter it under Settings, Vessel."
	sidewaysReasonUnreadable = "The helm height couldn't be read, so sideways g isn't shown."
)

// sidewaysGAt is the sideways g at a helm helmHeightM metres above the roll axis
// when the boat's roll acceleration is rollAccelDps2 degrees per second squared:
// the height times the acceleration in radians, over g. Today only the passage's
// peak (passageSidewaysGFor) uses it; spells keep their roll acceleration, not a
// sideways g, so any later use for a spell works it out here from the same
// height.
func sidewaysGAt(helmHeightM, rollAccelDps2 float64) float64 {
	return helmHeightM * (rollAccelDps2 * math.Pi / 180) / gravityMPerS2
}

// passageSidewaysGFor works the figures out for a passage's points from the
// helm height now on record: helm height in metres times roll acceleration in
// radians per second squared, over g. As in the stored stats, the first point's
// figures describe the interval before the passage, so it is left out. A height
// that cannot be read is logged and travels as the reason, so the passage is
// still returned.
func passageSidewaysGFor(id string, pts []recordedTrackPoint) passageSidewaysG {
	helmHeightM, err := currentHelmHeight()
	if err != nil {
		if helmHeightLog.allow() {
			log.Printf("passages: helm height for %s: %v (repeats are not logged for an hour)", id, err)
		}
		return passageSidewaysG{Reason: sidewaysReasonUnreadable}
	}
	if helmHeightM == nil {
		return passageSidewaysG{Reason: sidewaysReasonNoHeight}
	}
	var out passageSidewaysG
	for i, p := range pts {
		if i == 0 || p.RollAccelPeakDps2 == nil {
			continue
		}
		g := sidewaysGAt(*helmHeightM, *p.RollAccelPeakDps2)
		raiseMax(&out.PeakG, &out.PeakAt, &g, p.At)
	}
	if out.PeakG == nil {
		out.Reason = snapMissingReason(pts, figureSidewaysG)
	}
	return out
}

// helmHeightLog lets a failure to read the helm height reach the log once an
// hour, not on every passage read.
var helmHeightLog = &onceAnHour{}

// onceAnHour answers allow true at most once an hour. now is the clock, and
// time.Now when nil; tests give it their own.
type onceAnHour struct {
	mu   sync.Mutex
	now  func() time.Time
	last time.Time
}

func (o *onceAnHour) allow() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	now := time.Now()
	if o.now != nil {
		now = o.now()
	}
	if !o.last.IsZero() && now.Sub(o.last) < time.Hour {
		return false
	}
	o.last = now
	return true
}

// currentHelmHeight is the operator's helm height, nil when blank.
func currentHelmHeight() (*float64, error) {
	if globalDocumentStore == nil {
		return nil, fmt.Errorf("the vessel details store is not running")
	}
	v, err := globalDocumentStore.GetVesselParticulars()
	if err != nil {
		return nil, err
	}
	return v.HelmHeightM, nil
}

// storeFigures writes the figures to the row, only if the range is still the
// one they were worked out for and the row is still at the generation it had
// when they were read, so a trim or an import racing this read cannot leave stale
// figures behind. It says whether the row was written, and asks the comfort fit
// for a fit when it was.
func (s *passageStore) storeFigures(rec passageRecord, f passageFigures, gen int64) (stored bool, err error) {
	buf, err := json.Marshal(f)
	if err != nil {
		return false, err
	}
	// felt_deg is kept in a column of its own so the comfort bands can be learned
	// from every rated passage without reading each one's stats.
	res, err := s.db.Exec(`UPDATE passages SET stats_json = ?, felt_deg = ? WHERE id = ? AND start_ms = ? AND end_ms = ? AND figures_gen = ?`,
		string(buf), f.Stats.FeltDeg, rec.ID, rec.Start.UnixMilli(), rec.End.UnixMilli(), gen)
	if err != nil {
		return false, fmt.Errorf("store passage stats: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store passage stats: %w", err)
	}
	if n == 1 && s.comfort != nil {
		s.comfort.signal()
	}
	return n == 1, nil
}

// ListPassageSummaries is the visible passages, newest first, with their
// stored figures and the band word beside each felt figure. The bands come from
// ComfortBands, the one place the rated-passage rule lives. Each row is brought
// up to date by PassageFiguresFor inside this function, on the request path, so
// its felt column is filled before the bands are learned.
func (s *passageStore) ListPassageSummaries() ([]passageSummary, error) {
	records, err := s.ListPassages()
	if err != nil {
		return nil, err
	}
	out := make([]passageSummary, 0, len(records))
	for _, r := range records {
		f, err := s.PassageFiguresFor(r)
		if err != nil {
			return nil, err
		}
		tz, reason := f.startTimezone(r.ID)
		out = append(out, passageSummary{passageRecord: r, Stats: f.Stats.forWire(), Timezone: tz, TimezoneError: reason})
	}
	bands := s.comfortBandsForPage()
	for i := range out {
		out[i].setFelt(bands)
	}
	return out, nil
}

// staleFigureIDs are the passages, hidden or not, whose stored stats are
// missing, unreadable or not in the current shape. The stats are read and
// judged here, not in SQL, so one unreadable row cannot stop the scan.
func (s *passageStore) staleFigureIDs() ([]string, error) {
	rows, err := s.db.Query(`SELECT id, stats_json FROM passages ORDER BY start_ms`)
	if err != nil {
		return nil, fmt.Errorf("read passage stats: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		var raw sql.NullString
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, fmt.Errorf("read passage stats: %w", err)
		}
		if f, ok := parseFigures(raw); !ok || !f.current() {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read passage stats: %w", err)
	}
	return ids, nil
}

// recomputeStalePassages brings every passage whose stats are missing or older
// than passageFiguresVersion to the current version in one pass, with work, and
// logs one line: how many it recomputed and how many failed, with the first few
// errors. A failure on one passage leaves the others alone. When ctx ends the
// loop stops and the line says how many passages were left.
func recomputeStalePassages(ctx context.Context, store *passageStore, work func(passageRecord) (passageFigures, error)) (recomputed, failed int) {
	ids, err := store.staleFigureIDs()
	if err != nil {
		log.Printf("passages: start-up recompute: could not find the passages to work out: %v", err)
		return 0, 0
	}
	const maxListedFailures = 5
	var failures []string
	left := 0
	for i, id := range ids {
		if ctx.Err() != nil {
			left = len(ids) - i
			break
		}
		rec, err := store.GetPassage(id)
		if err == nil {
			_, err = work(rec)
		}
		if err != nil {
			failed++
			if len(failures) < maxListedFailures {
				failures = append(failures, fmt.Sprintf("%s: %v", id, err))
			}
			continue
		}
		recomputed++
	}
	line := fmt.Sprintf("passages: start-up recompute: recomputed %d, %d failed", recomputed, failed)
	if left > 0 {
		line += fmt.Sprintf(", stopped early with %d left", left)
	}
	if failed > 0 {
		line += " (" + strings.Join(failures, "; ")
		if failed > len(failures) {
			line += fmt.Sprintf("; and %d more", failed-len(failures))
		}
		line += ")"
	}
	log.Print(line)
	return recomputed, failed
}

// startPassageFigures is the start-up work on the passage log's motion figures.
// The order matters: the backfill (ADR 0296) fills motion into points recorded
// before the live recorder, and clears the stored figures of each passage it
// fills; only then does the pass work out every passage whose figures are
// missing or old, so none is worked out before it has been filled. main runs it
// in its own goroutine with the streams' context.
//
// The sea sweep (ADR 0299, ADR 0300) follows, so its first pass finds every
// passage's figures already worked out. It then runs every hour until ctx ends, so this
// function returns only at shutdown.
func startPassageFigures(ctx context.Context, store *passageStore, settingsPath string) {
	startPassageFiguresWith(ctx, store, settingsPath, waveProviderResolver(settingsPath), onBoardCoastline, seaSweepEvery, time.Now)
}

// startPassageFiguresWith is startPassageFigures with the sweep's wave plugin,
// coastline, interval and clock given, for tests.
func startPassageFiguresWith(ctx context.Context, store *passageStore, settingsPath string, resolve func() (waveProvider, string, error), coast func() *offlineCoastlineReader, every time.Duration, clock func() time.Time) {
	runMotionBackfillOnce(ctx, store, settingsPath, time.Now().UTC())
	if ctx.Err() != nil {
		return
	}
	recomputeStalePassages(ctx, store, store.PassageFiguresFor)
	if ctx.Err() != nil {
		return
	}
	// The start-up fit is the worker's, asked for here so it follows the recompute.
	// The sweeps start without waiting for it, and a first pass that stores
	// something asks for one more.
	store.comfort.signal()
	worker := make(chan struct{})
	go func() {
		defer close(worker)
		store.comfort.run(ctx, store, coast)
	}()
	runSeaSweeps(ctx, store, resolve, coast, every, clock)
	<-worker
}

// bandsReasonUnreadable is the reason a page carries when the bands could not be
// learned because reading the ratings failed.
const bandsReasonUnreadable = "The comfort bands couldn't be worked out from your ratings, so no band word is shown."

// comfortBandsForPage is ComfortBands for a list or a passage page: a failure is
// logged and travels as the bands' reason, with the bands not learned, so the
// page still returns its figures.
func (s *passageStore) comfortBandsForPage() comfortBands {
	bands, err := s.ComfortBands()
	if err != nil {
		log.Printf("passages: comfort bands: %v", err)
		return comfortBands{Reason: bandsReasonUnreadable}
	}
	return bands
}

// ComfortBands learns the bands from every visible rated passage. It reads only
// each passage's rating and felt_deg column and runs nothing else, so a request
// never scans the table. A passage whose figures have not been worked out yet
// has no felt_deg and joins the bands once the start-up pass, or a read of it,
// has stored them.
func (s *passageStore) ComfortBands() (comfortBands, error) {
	rows, err := s.db.Query(`SELECT rating, felt_deg FROM passages WHERE hidden = 0 AND rating != '' AND felt_deg IS NOT NULL`)
	if err != nil {
		return comfortBands{}, fmt.Errorf("read rated passages: %w", err)
	}
	defer rows.Close()
	var rated []ratedFelt
	for rows.Next() {
		var r ratedFelt
		if err := rows.Scan(&r.Rating, &r.FeltDeg); err != nil {
			return comfortBands{}, fmt.Errorf("read rated passages: %w", err)
		}
		rated = append(rated, r)
	}
	if err := rows.Err(); err != nil {
		return comfortBands{}, fmt.Errorf("read rated passages: %w", err)
	}
	return learnComfortBands(rated), nil
}
