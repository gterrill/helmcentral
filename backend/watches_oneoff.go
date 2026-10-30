package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

/*
One-off watches (ADR 0227, folding in ADR 0160).

The timed watch Mate starts with start_watch is a saved watch of kind one_off.
The registry in assistant_watch.go still does the sampling in memory; this file
is how it is kept:

  - Start writes the watch row before the watch is registered, so a watch that
    cannot be saved does not run.
  - Checkpoint appends the samples taken since the last save every couple of
    minutes, from a writer of its own so a slow disk never costs a tick.
  - Ended stores the finished report as a run, and the report still goes to the
    chat that asked.
  - After a restart, resumeOneOffWatches picks up any watch not yet ended and
    carries on from its checkpoint, marking the time Helmcentral was down as a
    gap. A watch whose time ran out while Helmcentral was down is reported
    from the samples it had, and the report says so.
*/

type watchOneOffConfig struct {
	Paths         []assistantWatchPathArg `json:"paths"`
	Reason        string                  `json:"reason"`
	Today         string                  `json:"today"`
	TimezoneLabel string                  `json:"timezone_label"`
	ZoneName      string                  `json:"zone_name"`
	OffsetSeconds int                     `json:"offset_seconds"`
}

// watchSampleJSON is one sample as the checkpoint keeps it, short because a
// 30 minute watch of six paths is 10,800 of them.
type watchSampleJSON struct {
	T      int64   `json:"t"`
	Value  float64 `json:"v,omitempty"`
	OK     bool    `json:"ok,omitempty"`
	Reason string  `json:"r,omitempty"`
	Source string  `json:"s,omitempty"`
	Units  string  `json:"u,omitempty"`
}

type watchCheckpoint struct {
	Series [][]watchSampleJSON `json:"series"`
}

const watchDownReason = "Helmcentral was not running"

// watchOneOffStore keeps the registry's watches in the record store.
type watchOneOffStore struct {
	store func() *documentStore
	now   func() time.Time
	// ended holds the watches this process ended when the store would not take
	// the note, so it does not report them again.
	mu    sync.Mutex
	ended map[string]bool
}

func newWatchOneOffStore(store func() *documentStore) *watchOneOffStore {
	return &watchOneOffStore{store: store, now: func() time.Time { return time.Now().UTC() }}
}

func (s *watchOneOffStore) Start(info assistantWatchInfo) error {
	docs := s.store()
	if docs == nil {
		return errWatchesNoStore
	}
	loc := info.Location
	if loc == nil {
		loc = time.UTC
	}
	_, offset := info.StartedAt.In(loc).Zone()
	config, err := json.Marshal(watchOneOffConfig{
		Paths: info.Paths, Reason: info.Reason, Today: info.Today.Format("2006-01-02"),
		TimezoneLabel: info.TimezoneLabel, ZoneName: loc.String(), OffsetSeconds: offset,
	})
	if err != nil {
		return err
	}
	started, ends := info.StartedAt, info.EndsAt
	w := savedWatch{
		ID: info.ID, Name: truncateRunes(info.Subject, maxWatchNameChars), Kind: watchKindOneOff, DurationMinutes: info.Minutes,
		StartedAt: &started, EndsAt: &ends, ConversationID: info.ConversationID,
		Sections: []string{}, Thresholds: defaultWatchThresholds(), LookOutFor: truncateRunes(info.Reason, maxWatchLookOutForChars),
		Enabled: true, CreatedAt: info.StartedAt, UpdatedAt: info.StartedAt, ScheduleChangedAt: info.StartedAt, Config: config,
	}
	return docs.RunTx("start one-off watch", func(tx *sql.Tx, _ time.Time) error {
		return insertSavedWatchTx(tx, w)
	})
}

// Checkpoint appends the samples taken since the last save. It is called from
// the watch's writer goroutine, never from the sampler, so a slow write holds
// up nothing.
func (s *watchOneOffStore) Checkpoint(info assistantWatchInfo, batch [][]assistantWatchSample) {
	docs := s.store()
	if docs == nil {
		return
	}
	if err := docs.RunTx("checkpoint one-off watch", func(tx *sql.Tx, _ time.Time) error {
		return appendWatchSamplesTx(tx, info.ID, batch)
	}); err != nil {
		log.Printf("watches: could not save the samples of watch %s, so a restart would lose them: %v", info.ID, err)
	}
}

// appendWatchSamplesTx adds one chunk of new samples. Nothing is written once
// the watch has ended or been deleted, so a late save cannot leave samples
// behind.
func appendWatchSamplesTx(x alarmSQLExecer, id string, batch [][]assistantWatchSample) error {
	empty := true
	cp := watchCheckpoint{Series: make([][]watchSampleJSON, len(batch))}
	for i, samples := range batch {
		out := make([]watchSampleJSON, len(samples))
		for j, sm := range samples {
			out[j] = watchSampleJSON{T: sm.T.UnixNano(), Value: sm.Value, OK: sm.OK, Reason: sm.Reason, Source: sm.Source, Units: sm.Units}
			empty = false
		}
		cp.Series[i] = out
	}
	if empty {
		return nil
	}
	raw, err := json.Marshal(cp)
	if err != nil {
		return fmt.Errorf("encode the samples: %w", err)
	}
	_, err = x.Exec(`INSERT INTO watch_samples (watch_id, seq, samples)
		SELECT ?, COALESCE(MAX(seq) + 1, 0), ? FROM watch_samples WHERE watch_id = ?
		AND EXISTS (SELECT 1 FROM watches WHERE id = ? AND enabled = 1)`, id, string(raw), id, id)
	return err
}

// loadWatchSamplesQ puts a watch's saved chunks back together, one series per
// path.
func loadWatchSamplesQ(q sqlQueryer, id string, paths int) ([][]assistantWatchSample, error) {
	rows, err := q.Query(`SELECT samples FROM watch_samples WHERE watch_id = ? ORDER BY seq`, id)
	if err != nil {
		return nil, fmt.Errorf("read the saved samples: %w", err)
	}
	defer rows.Close()
	series := make([][]assistantWatchSample, paths)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		chunk, err := decodeWatchSeries(json.RawMessage(raw), paths)
		if err != nil {
			return nil, err
		}
		for i := range chunk {
			series[i] = append(series[i], chunk[i]...)
		}
	}
	return series, rows.Err()
}

// finish records the end of a one-off watch: a run, the watch switched off and
// marked reported (when it was), its samples cleared. If that cannot be saved
// the watch is still switched off and marked in a second attempt, so a restart
// does not report it again. If even that fails, this process remembers.
func (s *watchOneOffStore) finish(info assistantWatchInfo, run watchRun, countsAsRun, reported bool) {
	docs := s.store()
	if docs == nil {
		log.Printf("watches: the record store is not open, so the end of watch %s was not saved", info.ID)
		s.remember(info.ID, reported)
		return
	}
	run.WatchID, run.Findings, run.CostKnown = info.ID, []watchFinding{}, true
	run.BoatTime, run.Timezone = watchBoatClock(run.StartedAt, info.Location), watchBoatZone(run.StartedAt, info.Location)
	mark := func(tx *sql.Tx, now time.Time) error {
		_, err := tx.Exec(`UPDATE watches SET enabled = 0, reported_at = ? WHERE id = ?`, nullableReported(now, reported), info.ID)
		return err
	}
	err := docs.RunTx("finish one-off watch", func(tx *sql.Tx, now time.Time) error {
		if err := insertWatchRunTx(tx, run, countsAsRun); err != nil {
			return err
		}
		if err := mark(tx, now); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM watch_samples WHERE watch_id = ?`, info.ID)
		return err
	})
	if err == nil {
		return
	}
	log.Printf("watches: could not save the end of watch %s: %v", info.ID, err)
	if err := docs.RunTx("mark one-off watch ended", mark); err != nil {
		log.Printf("watches: ALERT: watch %s ended but neither its report nor its end could be saved (%v); this process will not report it again, but a restart might", info.ID, err)
		s.remember(info.ID, reported)
	}
}

func nullableReported(now time.Time, reported bool) any {
	if !reported {
		return nil
	}
	return watchStoreTime(now)
}

func (s *watchOneOffStore) remember(id string, reported bool) {
	if !reported {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended == nil {
		s.ended = map[string]bool{}
	}
	s.ended[id] = true
}

// Reported says whether this process has already ended (and so reported) the
// watch, whether or not the store would take the note.
func (s *watchOneOffStore) Reported(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ended[id]
}

func (s *watchOneOffStore) Ended(info assistantWatchInfo, report assistantWatchReport) {
	raw, err := json.Marshal(report)
	if err != nil {
		log.Printf("watches: could not encode the report of watch %s: %v", info.ID, err)
		s.finish(info, watchRun{StartedAt: info.StartedAt, FinishedAt: s.now(), Outcome: watchOutcomeSkipped, SkipReason: "The report could not be saved.", ReportKind: watchReportKindSamples}, false, true)
		return
	}
	s.finish(info, watchRun{
		StartedAt: info.StartedAt, FinishedAt: s.now(), Outcome: watchOutcomeRan, ReportKind: watchReportKindSamples, Report: raw,
	}, true, true)
}

func (s *watchOneOffStore) Stopped(info assistantWatchInfo) {
	at := s.now()
	s.finish(info, watchRun{
		StartedAt: info.StartedAt, FinishedAt: at, Outcome: watchOutcomeSkipped, SkipReason: "Stopped before it ended.", ReportKind: watchReportKindSamples,
	}, false, false)
}

// ── resuming ────────────────────────────────────────────────────────────

func decodeWatchSeries(raw json.RawMessage, paths int) ([][]assistantWatchSample, error) {
	series := make([][]assistantWatchSample, paths)
	if len(raw) == 0 {
		return series, nil
	}
	var cp watchCheckpoint
	if err := json.Unmarshal(raw, &cp); err != nil {
		return nil, fmt.Errorf("the saved samples are unreadable: %w", err)
	}
	if len(cp.Series) != paths {
		return nil, fmt.Errorf("the saved samples cover %d paths, the watch has %d", len(cp.Series), paths)
	}
	for i, samples := range cp.Series {
		for _, sm := range samples {
			series[i] = append(series[i], assistantWatchSample{T: time.Unix(0, sm.T).UTC(), Value: sm.Value, OK: sm.OK, Reason: sm.Reason, Source: sm.Source, Units: sm.Units})
		}
	}
	return series, nil
}

// fillWatchDowntime marks every second after the last sample up to until as a
// gap, so the report shows the time Helmcentral was not running.
func fillWatchDowntime(series [][]assistantWatchSample, from, until time.Time) {
	for i := range series {
		last := from
		if n := len(series[i]); n > 0 {
			last = series[i][n-1].T
		}
		for t := last.Add(assistantWatchSampleInterval); !t.After(until); t = t.Add(assistantWatchSampleInterval) {
			series[i] = append(series[i], assistantWatchSample{T: t, Reason: watchDownReason})
		}
	}
}

// location is the boat's clock the watch began on, loaded by the zone's IANA
// name so a label reads "AEDT" and a daylight change mid-watch is followed.
// A name the zone database does not know is an error: guessing a fixed offset
// would put the wrong zone name on the watch's report.
func (cfg watchOneOffConfig) location() (*time.Location, error) {
	if cfg.ZoneName == "" {
		return nil, errors.New("the saved boat time zone is missing")
	}
	z, err := zoneAtName(cfg.ZoneName)
	if err != nil {
		return nil, fmt.Errorf("the saved boat time zone is unreadable: %w", err)
	}
	return z.Loc, nil
}

func oneOffInfo(w savedWatch, cfg watchOneOffConfig) (assistantWatchInfo, error) {
	if w.StartedAt == nil || w.EndsAt == nil {
		return assistantWatchInfo{}, fmt.Errorf("watch %s has no start or end time", w.ID)
	}
	today, err := time.Parse("2006-01-02", cfg.Today)
	if err != nil {
		return assistantWatchInfo{}, fmt.Errorf("watch %s: the saved date is unreadable: %w", w.ID, err)
	}
	loc, err := cfg.location()
	if err != nil {
		return assistantWatchInfo{}, fmt.Errorf("watch %s: %w", w.ID, err)
	}
	labels := make([]string, len(cfg.Paths))
	for i, p := range cfg.Paths {
		labels[i] = p.Label
	}
	return assistantWatchInfo{
		ID: w.ID, ConversationID: w.ConversationID, Subject: assistantWatchSubject(labels), Labels: labels, Minutes: w.DurationMinutes,
		StartedAt: *w.StartedAt, EndsAt: *w.EndsAt, EndsAtLocal: assistantWatchClock(*w.EndsAt, loc), Status: assistantWatchStatusWatching,
		Reason: cfg.Reason, Paths: cfg.Paths, Today: today, Location: loc, TimezoneLabel: cfg.TimezoneLabel,
	}, nil
}

// resumeOneOffWatches picks up the one-off watches that had not ended when
// Helmcentral last stopped. It returns how many resumed sampling and how many
// had already run out of time. A watch that cannot be read back is an error
// naming it, and the others are still picked up.
func resumeOneOffWatches(reg *assistantWatchRegistry, docs *documentStore, now time.Time) (resumed, ended int, err error) {
	var list []savedWatch
	if err := docs.Read(func(q sqlQueryer) error {
		var err error
		list, err = listSavedWatchesQ(q)
		return err
	}); err != nil {
		return 0, 0, fmt.Errorf("read the one-off watches: %w", err)
	}
	var firstErr error
	for _, w := range list {
		if w.Kind != watchKindOneOff || !w.Enabled {
			continue
		}
		if w.ReportedAt != nil {
			// Its report reached the chat; only the note that it ended is missing.
			continue
		}
		if r, ok := reg.deps.persist.(interface{ Reported(string) bool }); ok && r.Reported(w.ID) {
			continue
		}
		if err := resumeOneOffWatch(reg, docs, w, now, &resumed, &ended); err != nil {
			log.Printf("watches: could not resume one-off watch %s: %v", w.ID, err)
			if firstErr == nil {
				firstErr = fmt.Errorf("one-off watch %s: %w", w.ID, err)
			}
		}
	}
	return resumed, ended, firstErr
}

func resumeOneOffWatch(reg *assistantWatchRegistry, docs *documentStore, w savedWatch, now time.Time, resumed, ended *int) error {
	var cfg watchOneOffConfig
	if err := json.Unmarshal(w.Config, &cfg); err != nil {
		return fmt.Errorf("the saved settings are unreadable: %w", err)
	}
	info, err := oneOffInfo(w, cfg)
	if err != nil {
		return err
	}
	var series [][]assistantWatchSample
	if err := docs.Read(func(q sqlQueryer) error {
		var err error
		series, err = loadWatchSamplesQ(q, w.ID, len(cfg.Paths))
		return err
	}); err != nil {
		return err
	}
	if info.EndsAt.After(now) {
		saved := make([]int, len(series))
		for i := range series {
			saved[i] = len(series[i])
		}
		fillWatchDowntime(series, info.StartedAt, now)
		// The gap is saved too, so a second restart does not have to guess it.
		gap := make([][]assistantWatchSample, len(series))
		for i := range series {
			gap[i] = series[i][saved[i]:]
		}
		if err := docs.RunTx("save the downtime of a one-off watch", func(tx *sql.Tx, _ time.Time) error {
			return appendWatchSamplesTx(tx, w.ID, gap)
		}); err != nil {
			return fmt.Errorf("save the time Helmcentral was down: %w", err)
		}
		reg.resume(info, series)
		*resumed++
		return nil
	}
	reg.recoverEnded(info, series, "Helmcentral was not running when this watch ended, so this report covers only the samples taken before it stopped.")
	*ended++
	return nil
}

// resume registers a watch picked up after a restart and carries on sampling.
// It does not apply the start limits: the watch was already running.
func (reg *assistantWatchRegistry) resume(info assistantWatchInfo, series [][]assistantWatchSample) {
	w := &assistantWatch{info: info, stop: make(chan struct{}), series0: series}
	reg.mu.Lock()
	reg.watches[info.ConversationID] = w
	reg.mu.Unlock()
	go reg.run(w, reg.deps.newTicker(assistantWatchSampleInterval))
	log.Printf("assistant: watch %s for conversation %s resumed after a restart, until %s", info.ID, info.ConversationID, info.EndsAt.Format(time.RFC3339))
}

// recoverEnded reports a watch whose time ran out while Helmcentral was down,
// from the samples it had.
func (reg *assistantWatchRegistry) recoverEnded(info assistantWatchInfo, series [][]assistantWatchSample, note string) {
	info.Status = assistantWatchStatusReporting
	w := &assistantWatch{info: info, stop: make(chan struct{}), series0: series, note: note}
	reg.mu.Lock()
	reg.watches[info.ConversationID] = w
	reg.mu.Unlock()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("assistant: recovering watch %s panicked: %v", info.ID, r)
				reg.remove(w)
				reg.forgetEnded(w)
				if reg.deps.persist != nil && !w.ended.Load() {
					reg.deps.persist.Stopped(info)
				}
			}
		}()
		reg.finishWatch(w, series)
	}()
}
