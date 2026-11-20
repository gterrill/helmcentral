package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"sort"
	"strings"
	"syscall"
	"time"

	ihttp "github.com/influxdata/influxdb-client-go/v2/api/http"
)

// Backfilling passage motion from InfluxDB history (ADR 0296). The live
// recorder (ADR 0295) only knows what arrived while it ran; a passage recorded
// before it existed has no sway figures. InfluxDB keeps the same attitude at
// about 1 Hz, which is enough for roll and pitch RMS and heave spread and not
// enough for snap or pounding.
//
// The samples go through the same motionAccumulator the live stream feeds,
// driven by sample time, so the figures and the 8 Hz floor follow one rule
// and the rate that was found is what the logbook reports.
//
// The job is bounded by a cutoff: the earlier of the first start of the 10 Hz
// recorder, stored once before it starts, and the first point found carrying a
// column only that recorder writes (a build that ran before this one has
// already captured live). Only points recorded before it are filled, each from
// the minute of history before it. When every one of them has been tried, a
// finished marker ends the backfill for good. A live outage after the cutoff is
// never filled: it stays empty, where it can be seen.

const (
	// motionBackfillMaxGap is how long a silence the replay bridges. InfluxDB
	// holds attitude at about a second apart, which the live gap limit (0.45 s)
	// would read as a break between every pair of samples and so measure no rate
	// at all. Three seconds takes a second of jitter and a missed write, and a
	// longer silence still breaks the run.
	motionBackfillMaxGap = 3 * time.Second
	// motionBackfillWindow is the history a point's sway comes from: the minute
	// ending at the point, whatever lies between it and the previous point.
	motionBackfillWindow = time.Minute
	// motionBackfillSpanGap is the longest silence between two target points that
	// still belongs to one span, and motionBackfillSpanMax the longest span, so a
	// query reads hours and not days. One passage is usually one span.
	motionBackfillSpanGap = 30 * time.Minute
	motionBackfillSpanMax = 6 * time.Hour
	// motionBackfillRevolutionsPattern finds every engine's revolutions, whatever
	// the ids. It is anchored, so storage pushes the match down. It stays broad
	// (a path with more segments also matches) and the rows are then kept by
	// isEngineRevolutionsLeaf, the same check the live recorder uses.
	motionBackfillRevolutionsPattern = `^propulsion\..*\.revolutions$`
)

// motionBackfillField is the field the SignalK writer stores readings under. It
// is the writer's, not the depth setting's INFLUX_DEPTH_FIELD.
const motionBackfillField = "value"

const (
	motionRollPath  = trackAttitudePath + ".roll"
	motionPitchPath = trackAttitudePath + ".pitch"
)

// motionBackfillQuery runs one Flux query. A variable so tests answer it
// without InfluxDB.
var motionBackfillQuery = queryFloatRows

const (
	motionStateCaptureStart     = "capture_start"
	motionStateBackfillFinished = "backfill_finished"
)

// motionState reads one moment of the motion backfill's state.
func (s *passageStore) motionState(key string) (time.Time, bool, error) {
	var ms int64
	err := s.db.QueryRow(`SELECT at_ms FROM passage_motion_state WHERE key = ?`, key).Scan(&ms)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("read motion state %s: %w", key, err)
	}
	return time.UnixMilli(ms).UTC(), true, nil
}

// setMotionStateOnce stores a moment unless one is already there.
func (s *passageStore) setMotionStateOnce(key string, at time.Time) error {
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO passage_motion_state (key, at_ms) VALUES (?, ?)`, key, at.UnixMilli()); err != nil {
		return fmt.Errorf("store motion state %s: %w", key, err)
	}
	return nil
}

// EnsureMotionCaptureStart stores the moment live motion capture began, unless
// it is stored already, and returns the stored moment. It is the recorder's
// first start and never changes after that, with or without an attitude sensor.
func (s *passageStore) EnsureMotionCaptureStart(at time.Time) (time.Time, error) {
	if err := s.setMotionStateOnce(motionStateCaptureStart, at); err != nil {
		return time.Time{}, err
	}
	got, ok, err := s.motionState(motionStateCaptureStart)
	if err != nil {
		return time.Time{}, err
	}
	if !ok {
		return time.Time{}, errors.New("motion capture start was not stored")
	}
	return got, nil
}

// MotionCaptureStart is the stored first start; ok is false before the recorder has
// ever started.
func (s *passageStore) MotionCaptureStart() (time.Time, bool, error) {
	return s.motionState(motionStateCaptureStart)
}

// firstLiveMotionPoint is the t_ms of the first recorded point that carries any
// column the live recorder introduced (ADR 0295). Only the live recorder writes
// them to a point it has not backfilled, so this is the real start of live
// capture, whatever the wall clock said. Points the backfill stamped are left
// out: its figures are not live capture. ok is false when there is none.
var firstLiveMotionSQL = func() string {
	nonNull := make([]string, len(trackPointExtraColumns))
	for i, c := range trackPointExtraColumns {
		nonNull[i] = c.name + " IS NOT NULL"
	}
	return `SELECT MIN(t_ms) FROM track_points
		WHERE source = ? AND motion_backfill_ms IS NULL AND (` + strings.Join(nonNull, " OR ") + `)`
}()

func (s *passageStore) firstLiveMotionPoint() (time.Time, bool, error) {
	var ms sql.NullInt64
	if err := s.db.QueryRow(firstLiveMotionSQL, trackSourceRecorded).Scan(&ms); err != nil {
		return time.Time{}, false, fmt.Errorf("read first live motion point: %w", err)
	}
	if !ms.Valid {
		return time.Time{}, false, nil
	}
	return time.UnixMilli(ms.Int64).UTC(), true, nil
}

// motionCutoff is the earlier of the stored first start of the recorder and the
// first point it is seen to have written. It is worked out on each run and
// never stored later than the stored value.
func (s *passageStore) motionCutoff() (time.Time, bool, error) {
	stored, ok, err := s.MotionCaptureStart()
	if err != nil || !ok {
		return time.Time{}, false, err
	}
	live, found, err := s.firstLiveMotionPoint()
	if err != nil {
		return time.Time{}, false, err
	}
	if found && live.Before(stored) {
		return live, true, nil
	}
	return stored, true, nil
}

// MotionBackfillFinished says whether every point before the cutoff has been
// tried.
func (s *passageStore) MotionBackfillFinished() (bool, error) {
	_, ok, err := s.motionState(motionStateBackfillFinished)
	return ok, err
}

// MarkMotionBackfillFinished stores the finished marker.
func (s *passageStore) MarkMotionBackfillFinished(at time.Time) error {
	return s.setMotionStateOnce(motionStateBackfillFinished, at)
}

// buildMotionBackfillFlux reads the given measurements, and those matching
// pattern, over [start, stop), keeping the path and the source tag. Every text
// value goes through fluxStringLiteral.
func buildMotionBackfillFlux(bucket, field string, paths []string, pattern string, start, stop time.Time) (string, error) {
	head, err := buildFloatRowsFlux(bucket, field, paths, pattern, start, stop)
	if err != nil {
		return "", err
	}
	return head + ` |> keep(columns: ["_time", "_value", "_measurement", "source"])`, nil
}

// bestSource keeps the readings of the one source with the most of them. A tie
// goes to the source whose name sorts first, so a rerun picks the same one. The
// result is oldest first.
//
// This differs from chooseSender (anomaly_normal_select.go), which pins a
// reading to the sender of its latest minute because the learned normal scores
// what the boat reports from now on, and a reading handed from one sender to
// another has been reported by the new one ever since. A backfill replays one
// window of the past, where no sender is "the current one", and the source that
// reported most of the window is the one that covers it.
func bestSource(samples []normalMinuteRow) []normalMinuteRow {
	counts := map[string]int{}
	for _, s := range samples {
		counts[s.Source]++
	}
	best, bestN := "", -1
	for src, n := range counts {
		if n > bestN || (n == bestN && src < best) {
			best, bestN = src, n
		}
	}
	return fromSource(samples, best)
}

// freshSource picks the source for a context reading at the end of rows (the
// window ending at the point). Only a source with a reading within
// trackReadingMaxAge of at can supply it, as live; among those the one with the
// most readings wins, then the one heard from last, then the name that sorts
// first. With no fresh source it returns nil.
func freshSource(rows []normalMinuteRow, at time.Time) []normalMinuteRow {
	type tally struct {
		n    int
		last time.Time
	}
	by := map[string]*tally{}
	for _, r := range rows {
		if r.At.After(at) {
			continue
		}
		t := by[r.Source]
		if t == nil {
			t = &tally{}
			by[r.Source] = t
		}
		t.n++
		if r.At.After(t.last) {
			t.last = r.At
		}
	}
	best := ""
	var bt *tally
	for src, t := range by {
		if at.Sub(t.last) > trackReadingMaxAge {
			continue
		}
		switch {
		case bt == nil, t.n > bt.n,
			t.n == bt.n && t.last.After(bt.last),
			t.n == bt.n && t.last.Equal(bt.last) && src < best:
			best, bt = src, t
		}
	}
	if bt == nil {
		return nil
	}
	return fromSource(rows, best)
}

// fromSource keeps the readings of one source, oldest first.
func fromSource(samples []normalMinuteRow, source string) []normalMinuteRow {
	var out []normalMinuteRow
	for _, s := range samples {
		if s.Source == source {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// groupByPath splits rows by measurement, keeping every source, each path
// oldest first.
func groupByPath(samples []normalMinuteRow) map[string][]normalMinuteRow {
	byPath := map[string][]normalMinuteRow{}
	for _, s := range samples {
		byPath[s.Path] = append(byPath[s.Path], s)
	}
	for _, rows := range byPath {
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].At.Before(rows[j].At) })
	}
	return byPath
}

// pairedSource picks one source for roll and pitch together: the one with the
// most timestamps that have both. Chosen apart, each path's busiest source could
// differ, and the two would pair to nothing. A tie goes to the name that sorts
// first. With no source holding any pair, both come back empty.
func pairedSource(roll, pitch []normalMinuteRow) (rollOut, pitchOut []normalMinuteRow) {
	pitchAt := map[string]map[int64]struct{}{}
	for _, p := range pitch {
		if pitchAt[p.Source] == nil {
			pitchAt[p.Source] = map[int64]struct{}{}
		}
		pitchAt[p.Source][p.At.UnixMilli()] = struct{}{}
	}
	pairs := map[string]int{}
	for _, r := range roll {
		if _, ok := pitchAt[r.Source][r.At.UnixMilli()]; ok {
			pairs[r.Source]++
		}
	}
	best, bestN := "", 0
	for src, n := range pairs {
		if n > bestN || (n == bestN && src < best) {
			best, bestN = src, n
		}
	}
	if bestN == 0 {
		return nil, nil
	}
	return fromSource(roll, best), fromSource(pitch, best)
}

// latestRowAtOrBefore is the newest reading stamped at or before at and no more
// than max older. s is oldest first. The live recorder never sees a reading from
// after the point, so neither does the backfill.
func latestRowAtOrBefore(s []normalMinuteRow, at time.Time, max time.Duration) (normalMinuteRow, bool) {
	i := sort.Search(len(s), func(i int) bool { return s[i].At.After(at) })
	if i == 0 || at.Sub(s[i-1].At) > max {
		return normalMinuteRow{}, false
	}
	return s[i-1], true
}

// latestReader reads each path as the newest reading at or before at within the
// live freshness limit, as an alarmSample stamped with when it was taken, so the
// live context rules (readTrackContext) apply to InfluxDB readings unchanged.
// InfluxDB does not hold an engine's off state, so EngineOff is never set.
func latestReader(series map[string][]normalMinuteRow, at time.Time) alarmReader {
	return func(path string) alarmSample {
		r, ok := latestRowAtOrBefore(series[path], at, trackReadingMaxAge)
		if !ok {
			return alarmSample{}
		}
		return alarmSample{Value: r.Value, Present: true, LastSeen: r.At}
	}
}

// inWindow calls visit for each item stamped after from and up to to, oldest
// first, from items sorted by time.
func inWindow[T any](items []T, at func(T) time.Time, from, to time.Time, visit func(T)) {
	i := sort.Search(len(items), func(i int) bool { return at(items[i]).After(from) })
	for ; i < len(items) && !at(items[i]).After(to); i++ {
		visit(items[i])
	}
}

// rowsInWindow is the rows stamped after from and up to to, oldest first, from
// rows sorted by time.
func rowsInWindow(rows []normalMinuteRow, from, to time.Time) []normalMinuteRow {
	var out []normalMinuteRow
	inWindow(rows, func(r normalMinuteRow) time.Time { return r.At }, from, to, func(r normalMinuteRow) { out = append(out, r) })
	return out
}

type motionBackfillPoint struct {
	ID int64
	At time.Time
}

// motionBackfillSummary is what one run did.
type motionBackfillSummary struct {
	Skipped           string // why nothing ran, when nothing did
	Spans             int    // spans queried this run
	SpansNoData       int    // of those, spans where no point got anything
	SpansFailed       int    // spans whose query failed, left to the next start
	PointsWithSway    int    // points that got roll, pitch or heave figures
	PointsContextOnly int    // points that got only heading, wind angle, speed or rpm
	Complete          bool   // every point before the cutoff has now been tried
}

type attitudeEvent struct {
	At       time.Time
	RollDeg  float64
	PitchDeg float64
}

// pairAttitude pairs roll with pitch by timestamp, both oldest first. They are
// written from one attitude update, so they share a timestamp; an unpaired one is
// dropped, as the live stream needs both.
func pairAttitude(roll, pitch []normalMinuteRow) []attitudeEvent {
	var out []attitudeEvent
	for i, j := 0, 0; i < len(roll) && j < len(pitch); {
		// The same key pairedSource counts by: the millisecond.
		switch rm, pm := roll[i].At.UnixMilli(), pitch[j].At.UnixMilli(); {
		case rm < pm:
			i++
		case pm < rm:
			j++
		default:
			out = append(out, attitudeEvent{At: roll[i].At, RollDeg: roll[i].Value * 180 / math.Pi, PitchDeg: pitch[j].Value * 180 / math.Pi})
			i++
			j++
		}
	}
	return out
}

// motionBackfillTargetsSQL picks the points to fill, oldest first: recorded
// before the cutoff, and not asked about already. Imported points are another
// story and are never targets. A point after the cutoff belongs to the live
// recorder, whatever it holds.
const motionBackfillTargetsSQL = `SELECT id, t_ms FROM track_points
		WHERE t_ms < ? AND source = ? AND motion_backfill_ms IS NULL
		ORDER BY t_ms, id`

// motionBackfillTargets runs motionBackfillTargetsSQL.
func (s *passageStore) motionBackfillTargets(cutoff time.Time) ([]motionBackfillPoint, error) {
	rows, err := s.db.Query(motionBackfillTargetsSQL, cutoff.UnixMilli(), trackSourceRecorded)
	if err != nil {
		return nil, fmt.Errorf("read motion backfill targets: %w", err)
	}
	defer rows.Close()
	var out []motionBackfillPoint
	for rows.Next() {
		var p motionBackfillPoint
		var ms int64
		if err := rows.Scan(&p.ID, &ms); err != nil {
			return nil, fmt.Errorf("read motion backfill targets: %w", err)
		}
		p.At = time.UnixMilli(ms).UTC()
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read motion backfill targets: %w", err)
	}
	return out, nil
}

// groupMotionSpans cuts the targets into passage-sized runs.
func groupMotionSpans(targets []motionBackfillPoint) [][]motionBackfillPoint {
	var spans [][]motionBackfillPoint
	for _, p := range targets {
		if n := len(spans); n > 0 {
			cur := spans[n-1]
			last, first := cur[len(cur)-1].At, cur[0].At
			if p.At.Sub(last) <= motionBackfillSpanGap && p.At.Sub(first) <= motionBackfillSpanMax {
				spans[n-1] = append(cur, p)
				continue
			}
		}
		spans = append(spans, []motionBackfillPoint{p})
	}
	return spans
}

// fillMotionSQL writes a point's figures into its columns that are still null
// and stamps the point as asked about, whether or not InfluxDB had anything.
var fillMotionSQL = func() string {
	sets := make([]string, len(trackPointExtraColumns))
	for i, c := range trackPointExtraColumns {
		sets[i] = c.name + " = COALESCE(" + c.name + ", ?)"
	}
	return `UPDATE track_points SET ` + strings.Join(sets, ", ") + `, motion_backfill_ms = ? WHERE id = ? AND source = ?`
}()

// fillMotionColumns writes the figures p holds into the point's columns that
// are still null, and stamps the point with askedAt. COALESCE keeps any value
// already there, so a value the live recorder wrote since the backfill read the
// row is never replaced. A p with no figures only stamps the point.
func fillMotionColumns(stmt *sql.Stmt, id int64, p recordedTrackPoint, askedAt time.Time) error {
	args := make([]any, 0, len(trackPointExtraColumns)+3)
	for _, c := range trackPointExtraColumns {
		args = append(args, nullFloat(*c.field(&p)))
	}
	args = append(args, askedAt.UnixMilli(), id, trackSourceRecorded)
	if _, err := stmt.Exec(args...); err != nil {
		return fmt.Errorf("fill motion columns: %w", err)
	}
	return nil
}

func hasAnyExtraFigure(p recordedTrackPoint) bool {
	for _, c := range trackPointExtraColumns {
		if *c.field(&p) != nil {
			return true
		}
	}
	return false
}

func hasSwayFigure(p recordedTrackPoint) bool {
	return p.RollRMSDeg != nil || p.PitchRMSDeg != nil || p.HeaveSDM != nil
}

// motionQueryError marks a failed InfluxDB query for one span, which leaves the
// span for the next start and does not stop the run.
type motionQueryError struct{ err error }

func (e motionQueryError) Error() string { return e.err.Error() }
func (e motionQueryError) Unwrap() error { return e.err }

// motionDeadHostError marks a failure every later query would repeat: InfluxDB
// cannot be reached, refuses the token, has no such org or bucket, or fails TLS.
// It ends the run and stamps nothing.
type motionDeadHostError struct{ err error }

func (e motionDeadHostError) Error() string { return "InfluxDB cannot be queried: " + e.err.Error() }
func (e motionDeadHostError) Unwrap() error { return e.err }

// isDeadHost says whether err is a failure that ends the run: a failure to
// connect (DNS, refused, no route, a connect timeout), an HTTP 401, 403 or 404
// (token, org or bucket), or a TLS certificate error. Any other error is one
// query's and skips its span.
func isDeadHost(err error) bool {
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return true
	}
	var dns *net.DNSError
	if errors.As(err, &dns) || errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	var he *ihttp.Error
	if errors.As(err, &he) {
		switch he.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
			return true
		}
	}
	var unknownAuth x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var host x509.HostnameError
	var verify *tls.CertificateVerificationError
	var record tls.RecordHeaderError
	return errors.As(err, &unknownAuth) || errors.As(err, &invalid) || errors.As(err, &host) ||
		errors.As(err, &verify) || errors.As(err, &record)
}

// runMotionBackfill fills what InfluxDB can answer for the points recorded
// before the cutoff, at most two bounded queries per span. A query that fails
// for one span is logged and leaves that span unstamped, so the next start asks
// again, and the run goes on to the next span; a span InfluxDB had nothing for
// is stamped and not asked again. A host that cannot be reached, a store failure
// or a stopped context ends the run. When every point has been tried, the run
// stores the finished marker.
func runMotionBackfill(ctx context.Context, store *passageStore, in normalInflux, now time.Time) (motionBackfillSummary, error) {
	var sum motionBackfillSummary
	done, err := store.MotionBackfillFinished()
	if err != nil {
		return sum, err
	}
	if done {
		sum.Skipped = "the backfill is complete"
		return sum, nil
	}
	cutoff, ok, err := store.motionCutoff()
	if err != nil {
		return sum, err
	}
	if !ok {
		return sum, errors.New("live motion capture has not started, so the backfill has no cutoff")
	}
	targets, err := store.motionBackfillTargets(cutoff)
	if err != nil {
		return sum, err
	}
	for _, span := range groupMotionSpans(targets) {
		if err := ctx.Err(); err != nil {
			return sum, err
		}
		res, err := backfillMotionSpan(ctx, store, in, span, now)
		if err != nil {
			err = fmt.Errorf("%s to %s: %w", span[0].At.Format(time.RFC3339), span[len(span)-1].At.Format(time.RFC3339), err)
			var qe motionQueryError
			if !errors.As(err, &qe) || ctx.Err() != nil {
				return sum, err
			}
			log.Printf("passages: motion backfill: %v; left for the next start", err)
			sum.SpansFailed++
			continue
		}
		sum.Spans++
		sum.PointsWithSway += res.sway
		sum.PointsContextOnly += res.contextOnly
		if res.sway+res.contextOnly == 0 {
			sum.SpansNoData++
		}
	}
	if sum.SpansFailed == 0 {
		if err := store.MarkMotionBackfillFinished(now); err != nil {
			return sum, err
		}
		sum.Complete = true
	}
	return sum, nil
}

// spanResult is how many of a span's points got what.
type spanResult struct{ sway, contextOnly int }

func backfillMotionSpan(ctx context.Context, store *passageStore, in normalInflux, span []motionBackfillPoint, now time.Time) (spanResult, error) {
	var res spanResult
	start, stop := span[0].At.Add(-motionBackfillWindow), span[len(span)-1].At.Add(time.Millisecond)

	// read returns every source's rows by path, oldest first. A host that cannot
	// be reached is a motionDeadHostError and any other failure a motionQueryError.
	read := func(paths []string, pattern string) (map[string][]normalMinuteRow, error) {
		flux, err := buildMotionBackfillFlux(in.mainBucket, motionBackfillField, paths, pattern, start, stop)
		if err != nil {
			return nil, err
		}
		rows, err := motionBackfillQuery(ctx, in, flux)
		if err != nil {
			if isDeadHost(err) {
				return nil, motionDeadHostError{err}
			}
			return nil, motionQueryError{err}
		}
		return groupByPath(rows), nil
	}

	motion, err := read([]string{motionRollPath, motionPitchPath, trackHeavePath}, "")
	if err != nil {
		return res, fmt.Errorf("attitude and heave: %w", err)
	}
	// The context is read for every span: a point's heading, wind angle, speed and
	// rpm are InfluxDB's to give even where its attitude sensor was silent.
	ctxRows, err := read([]string{trackHeadingPath, trackTWAPath, trackSTWPath}, motionBackfillRevolutionsPattern)
	if err != nil {
		return res, fmt.Errorf("heading, wind angle, speed and revolutions: %w", err)
	}
	// An engine seen anywhere in the span counts for every point of it, so one
	// that went quiet while not off leaves the rpm mean empty, as live.
	var enginePaths []string
	for path := range ctxRows {
		if isEngineRevolutionsLeaf(path) {
			enginePaths = append(enginePaths, path)
		}
	}
	sort.Strings(enginePaths)

	cur := start
	acc := newMotionAccumulatorGap(func() time.Time { return cur }, motionBackfillMaxGap)

	fills := map[int64]recordedTrackPoint{}
	for _, pt := range span {
		ws := pt.At.Add(-motionBackfillWindow)

		// Each source is chosen from this point's own window, so a swap earlier in
		// the span does not blank the rest of it.
		roll, pitch := pairedSource(rowsInWindow(motion[motionRollPath], ws, pt.At), rowsInWindow(motion[motionPitchPath], ws, pt.At))
		att := pairAttitude(roll, pitch)
		heave := bestSource(rowsInWindow(motion[trackHeavePath], ws, pt.At))
		readings := map[string][]normalMinuteRow{}
		for path, rows := range ctxRows {
			if w := freshSource(rowsInWindow(rows, ws, pt.At), pt.At); len(w) > 0 {
				readings[path] = w
			}
		}

		cur = ws
		acc.TakeWindow() // opens the window at ws, discarding anything older
		// Channels start again at each window, so its first sample begins a run:
		// neither the grid points nor the interval back to an earlier window's
		// sample are counted here.
		acc.RestartAttitude()
		acc.RestartHeave()
		for _, e := range att {
			cur = e.At
			acc.AddAttitude(e.At, e.RollDeg, e.PitchDeg)
		}
		for _, r := range heave {
			cur = r.At
			acc.AddHeave(r.At, r.Value)
		}
		cur = pt.At
		figures, _ := acc.TakeWindow()

		p := recordedTrackPoint{motionFigures: figures, trackContext: readTrackContext(latestReader(readings, pt.At), enginePaths, pt.At)}
		switch {
		case hasSwayFigure(p):
			res.sway++
			fills[pt.ID] = p
		case hasAnyExtraFigure(p):
			res.contextOnly++
			fills[pt.ID] = p
		}
	}

	// Every point of the span is stamped, one that has figures and one InfluxDB
	// had nothing for alike, in one transaction.
	tx, err := store.db.Begin()
	if err != nil {
		return spanResult{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(fillMotionSQL)
	if err != nil {
		return spanResult{}, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()
	for _, pt := range span {
		if err := fillMotionColumns(stmt, pt.ID, fills[pt.ID], now); err != nil {
			return spanResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return spanResult{}, fmt.Errorf("commit: %w", err)
	}
	return res, nil
}

// runMotionBackfillOnce is the start-up run. It never fails the app: InfluxDB
// not set up is logged once and skipped, a finished backfill is noted in one
// line, and a failure is logged with what it had done and tried again at the
// next start.
func runMotionBackfillOnce(ctx context.Context, store *passageStore, settingsPath string, now time.Time) motionBackfillSummary {
	done, err := store.MotionBackfillFinished()
	if err != nil {
		log.Printf("passages: motion backfill stopped: %v; it tries again at the next start", err)
		return motionBackfillSummary{Skipped: err.Error()}
	}
	if done {
		reason := "the backfill is complete"
		log.Printf("passages: motion backfill already complete; nothing to fill")
		return motionBackfillSummary{Skipped: reason}
	}
	in, err := openNormalInflux(settingsPath)
	if err != nil {
		reason := err.Error()
		if errors.Is(err, errNormalNoInflux) {
			reason = "InfluxDB is not set up, so passages recorded before the live motion feed keep no sway figures"
		}
		log.Printf("passages: motion backfill skipped: %s", reason)
		return motionBackfillSummary{Skipped: reason}
	}
	if in.close != nil {
		defer in.close()
	}
	sum, err := runMotionBackfill(ctx, store, in, now)
	if err != nil {
		log.Printf("passages: motion backfill stopped after filling %d points: %v; it tries again at the next start", sum.PointsWithSway+sum.PointsContextOnly, err)
		return sum
	}
	tail := "; the rest is tried again at the next start"
	if sum.Complete {
		tail = "; the backfill is complete and will not run again"
	}
	log.Printf("passages: motion backfill: %d points with sway figures, %d with context readings only, %d of %d spans with nothing in InfluxDB, %d spans failed%s",
		sum.PointsWithSway, sum.PointsContextOnly, sum.SpansNoData, sum.Spans, sum.SpansFailed, tail)
	return sum
}

// startMotionBackfill runs the backfill once. main starts it in its own
// goroutine with the streams' context, so it stops at shutdown.
func startMotionBackfill(ctx context.Context, store *passageStore, settingsPath string) {
	runMotionBackfillOnce(ctx, store, settingsPath, time.Now().UTC())
}
