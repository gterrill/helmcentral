package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// source_registry.go is Helmcentral's own record of what the boat has reported
// under the equipment sources (propulsion.<id>, electrical.batteries.<id>,
// tanks.<category>.<id> and so on), so Discovery can list an engine that is
// switched off, and so a source that carries other readings but never its
// signature can be told from one that was never seen (ADR 0196).
//
// It follows the remembered-units pattern (ADR 0193): vessel-keyed rows in
// helmcentral.sqlite on the document store's connection, written through RunTx,
// in memory for readers, batched and retried on failure. Unlike units it does
// not hook the delta stream: a goroutine looks at the live tree once a minute,
// so the stream goroutine never waits on it and a path is written at most once
// per sourceRegistryWriteInterval however often it updates.
//
// InfluxDB is never asked at request time. If it is configured and the vessel
// has not been seeded, its last-logged time per path (one query over
// sourceHistoryLookbackDays) starts the record once.

const (
	// sourceRegistryScanInterval is how often the live tree is looked at.
	sourceRegistryScanInterval = time.Minute
	// sourceRegistryWriteInterval is how stale a stored last_seen may get
	// before a path that is still reporting is written again.
	sourceRegistryWriteInterval = 5 * time.Minute
	// sourceRegistryFreshness is how recent a reading must be to count as seen
	// now. A value the tree still holds from hours ago is not a sighting.
	sourceRegistryFreshness = 10 * time.Minute
	// sourceSeedRetry is how long a failed InfluxDB seed waits before the next try.
	sourceSeedRetry = 15 * time.Minute
)

// seenRow is one path's record: when it was first and last seen reporting.
type seenRow struct {
	Path        string
	First, Last time.Time
}

// registryMeta is a vessel's tracking record. Watched is how long the tree was
// actually being fed while Helmcentral ran, which is what the seven-day rule
// counts (backend downtime and SignalK outages are not watching). SeededAt is
// set once the InfluxDB seed ran for this vessel and SeededPaths is how many
// equipment paths it returned: a seed that found nothing is remembered (so it
// is not repeated) but does not count as coverage.
type registryMeta struct {
	TrackingSince time.Time
	Watched       time.Duration
	SeededAt      time.Time
	SeededPaths   int
}

// sourceRegistryPersister is what the registry needs of the table; tests wrap
// the real store to count and fail transactions. load also says whether any
// vessel in the installation has been seeded: InfluxDB logs this boat only, so
// the seed is taken once per installation.
type sourceRegistryPersister interface {
	load(vessel string) (rows []seenRow, meta *registryMeta, installSeeded bool, err error)
	save(vessel string, rows []seenRow, meta *registryMeta) error
}

// sourceRegistryStore persists the registry in helmcentral.sqlite:
//
//	signalk_source_seen(vessel, path, first_seen, last_seen)  PRIMARY KEY (vessel, path)
//	signalk_source_tracking(vessel, tracking_since, watched_seconds, seeded_at, seeded_paths)  PRIMARY KEY (vessel)
//
// Times are Unix seconds. seeded_at and seeded_paths are NULL until seeded.
type sourceRegistryStore struct {
	ds *documentStore
}

func newSourceRegistryStore(ds *documentStore) (*sourceRegistryStore, error) {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS signalk_source_seen (
			vessel     TEXT NOT NULL,
			path       TEXT NOT NULL,
			first_seen INTEGER NOT NULL,
			last_seen  INTEGER NOT NULL,
			PRIMARY KEY (vessel, path)
		)`,
		`CREATE TABLE IF NOT EXISTS signalk_source_tracking (
			vessel          TEXT PRIMARY KEY,
			tracking_since  INTEGER NOT NULL,
			watched_seconds INTEGER NOT NULL DEFAULT 0,
			seeded_at       INTEGER,
			seeded_paths    INTEGER
		)`,
	} {
		if _, err := ds.db.Exec(stmt); err != nil {
			return nil, fmt.Errorf("create source registry tables: %w", err)
		}
	}
	return &sourceRegistryStore{ds: ds}, nil
}

func (s *sourceRegistryStore) load(vessel string) ([]seenRow, *registryMeta, bool, error) {
	var rows []seenRow
	var meta *registryMeta
	installSeeded := false
	err := s.ds.Read(func(q sqlQueryer) error {
		r, err := q.Query(`SELECT path, first_seen, last_seen FROM signalk_source_seen WHERE vessel = ?`, vessel)
		if err != nil {
			return err
		}
		defer r.Close()
		for r.Next() {
			var path string
			var first, last int64
			if err := r.Scan(&path, &first, &last); err != nil {
				return err
			}
			rows = append(rows, seenRow{Path: path, First: time.Unix(first, 0).UTC(), Last: time.Unix(last, 0).UTC()})
		}
		if err := r.Err(); err != nil {
			return err
		}
		m, err := q.Query(`SELECT vessel, tracking_since, watched_seconds, seeded_at, seeded_paths FROM signalk_source_tracking`)
		if err != nil {
			return err
		}
		defer m.Close()
		for m.Next() {
			var v string
			var since, watched int64
			var seededAt, seededPaths sql.NullInt64
			if err := m.Scan(&v, &since, &watched, &seededAt, &seededPaths); err != nil {
				return err
			}
			if seededAt.Valid {
				installSeeded = true
			}
			if v != vessel {
				continue
			}
			meta = &registryMeta{TrackingSince: time.Unix(since, 0).UTC(), Watched: time.Duration(watched) * time.Second}
			if seededAt.Valid {
				meta.SeededAt, meta.SeededPaths = time.Unix(seededAt.Int64, 0).UTC(), int(seededPaths.Int64)
			}
		}
		return m.Err()
	})
	if err != nil {
		return nil, nil, false, fmt.Errorf("source registry: load: %w", err)
	}
	return rows, meta, installSeeded, nil
}

func (s *sourceRegistryStore) save(vessel string, rows []seenRow, meta *registryMeta) error {
	return s.ds.RunTx("remember source sightings", func(tx *sql.Tx, now time.Time) error {
		for _, r := range rows {
			if _, err := tx.Exec(`INSERT INTO signalk_source_seen (vessel, path, first_seen, last_seen) VALUES (?, ?, ?, ?)
				ON CONFLICT(vessel, path) DO UPDATE SET
					first_seen = MIN(first_seen, excluded.first_seen),
					last_seen  = MAX(last_seen, excluded.last_seen)`,
				vessel, r.Path, r.First.Unix(), r.Last.Unix()); err != nil {
				return fmt.Errorf("source registry: save %s: %w", r.Path, err)
			}
		}
		if meta != nil {
			var seededAt, seededPaths any
			if !meta.SeededAt.IsZero() {
				seededAt, seededPaths = meta.SeededAt.Unix(), meta.SeededPaths
			}
			if _, err := tx.Exec(`INSERT INTO signalk_source_tracking (vessel, tracking_since, watched_seconds, seeded_at, seeded_paths) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT(vessel) DO UPDATE SET
					watched_seconds = MAX(watched_seconds, excluded.watched_seconds),
					seeded_at = COALESCE(excluded.seeded_at, seeded_at),
					seeded_paths = COALESCE(excluded.seeded_paths, seeded_paths)`,
				vessel, meta.TrackingSince.Unix(), int64(meta.Watched/time.Second), seededAt, seededPaths); err != nil {
				return fmt.Errorf("source registry: save tracking: %w", err)
			}
		}
		return nil
	})
}

// seenState is a path's in-memory record. written is the last_seen the table
// holds for it (zero when never written).
type seenState struct {
	first, last, written time.Time
}

// pendingWrite is what one vessel still has to write.
type pendingWrite struct {
	rows []seenRow
	meta *registryMeta
}

type sourceRegistry struct {
	mu     sync.Mutex
	store  sourceRegistryPersister
	vessel string
	loaded bool
	// loadErr is why the record for vessel could not be read; while set,
	// nothing is served for it and readers are told why.
	loadErr error
	paths   map[string]*seenState
	dirty   map[string]bool
	meta    registryMeta
	// metaDirty: meta has changes the table does not hold. watchedWritten is
	// the Watched the table holds.
	metaDirty      bool
	watchedWritten time.Duration
	// installSeeded: some vessel in this installation has been seeded.
	installSeeded bool
	// stash holds writes left over from a vessel the boat has since stopped
	// being, so a change of self vessel loses nothing.
	stash map[string]*pendingWrite
	// lastTick dates the watched-time credit; creditCap bounds one tick's.
	lastTick  time.Time
	lastFed   bool
	creditCap time.Duration

	seedConfigured func() bool
	seedFetch      func() (map[string]time.Time, error)
	lastSeedTry    time.Time
	seedFailing    bool
	noInfluxLogged bool
	writeFailing   bool
	loadFailing    bool

	// snap is the immutable record readers get; it is rebuilt at the end of
	// every tick and never changed after publication.
	snap atomic.Pointer[sourceRecord]
}

func newSourceRegistry(store sourceRegistryPersister, seedConfigured func() bool, seedFetch func() (map[string]time.Time, error)) *sourceRegistry {
	r := &sourceRegistry{
		store: store, seedConfigured: seedConfigured, seedFetch: seedFetch,
		paths: map[string]*seenState{}, dirty: map[string]bool{}, stash: map[string]*pendingWrite{},
		creditCap: 2 * sourceRegistryScanInterval,
	}
	r.snap.Store(&sourceRecord{})
	return r
}

// globalSourceRegistry is nil until main starts it; readers see an empty record.
var globalSourceRegistry *sourceRegistry

// run keeps the registry current until ctx ends. Production never cancels ctx.
func (r *sourceRegistry) run(ctx context.Context, snapshot *signalKSnapshot) {
	ticker := time.NewTicker(sourceRegistryScanInterval)
	defer ticker.Stop()
	for {
		r.tick(snapshot, time.Now().UTC())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// tick is one pass: switch to the self vessel's record if the vessel changed,
// note what reports now and credit the time watched, seed once from InfluxDB if
// due, write what changed, and publish a fresh immutable record for readers.
func (r *sourceRegistry) tick(snapshot *signalKSnapshot, now time.Time) {
	vessel := snapshot.selfContext()
	if vessel == "" {
		return
	}
	r.mu.Lock()
	if r.vessel != vessel {
		r.stashLocked()
		r.vessel, r.loaded = vessel, false
		r.paths, r.dirty, r.meta = map[string]*seenState{}, map[string]bool{}, registryMeta{}
		r.metaDirty, r.watchedWritten, r.lastTick, r.lastFed = false, 0, time.Time{}, false
		r.seedFailing = false
	}
	if !r.loaded && !r.loadLocked(vessel, now) {
		r.publishLocked(now)
		r.mu.Unlock()
		return
	}
	telemetry := snapshot.liveSelfTelemetry(now)
	fresh := r.observeLocked(telemetry, now)
	r.creditLocked(fresh, now)
	due := r.seedDueLocked(now)
	r.mu.Unlock()

	// The InfluxDB query runs without the lock, so nothing else waits on it.
	var history map[string]time.Time
	var err error
	if due {
		history, err = r.seedFetch()
	}

	r.mu.Lock()
	if due && r.vessel == vessel {
		r.applySeedLocked(history, err, now)
	}
	r.flushLocked()
	r.publishLocked(now)
	r.mu.Unlock()
}

// stashLocked keeps the pending writes of the vessel being left, so the change
// of self vessel loses no sighting; flushLocked retries them.
func (r *sourceRegistry) stashLocked() {
	if !r.loaded || (len(r.dirty) == 0 && !r.metaDirty) {
		return
	}
	p := &pendingWrite{}
	for path := range r.dirty {
		st := r.paths[path]
		p.rows = append(p.rows, seenRow{Path: path, First: st.first, Last: st.last})
	}
	if r.metaDirty {
		m := r.meta
		p.meta = &m
	}
	r.stash[r.vessel] = p
}

// loadLocked replaces the in-memory record with vessel's stored one. It starts
// tracking (the date Helmcentral began watching) for a vessel seen for the
// first time. A failure is remembered in loadErr, so readers are told rather
// than shown an empty record.
func (r *sourceRegistry) loadLocked(vessel string, now time.Time) bool {
	rows, meta, installSeeded, err := r.store.load(vessel)
	if err != nil {
		if !r.loadFailing {
			log.Printf("source registry: %v; Discovery lists only sources reporting now", err)
		}
		r.loadFailing = true
		r.loadErr = err
		return false
	}
	if r.loadFailing {
		log.Printf("source registry: record readable again")
	}
	r.loadFailing, r.loadErr = false, nil
	r.loaded, r.installSeeded = true, installSeeded
	r.paths, r.dirty = map[string]*seenState{}, map[string]bool{}
	for _, row := range rows {
		r.paths[row.Path] = &seenState{first: row.First, last: row.Last, written: row.Last}
	}
	if meta == nil {
		r.meta, r.metaDirty = registryMeta{TrackingSince: now}, true
		r.watchedWritten = 0
	} else {
		r.meta, r.metaDirty = *meta, false
		r.watchedWritten = meta.Watched
	}
	return true
}

// observeLocked notes every path under an equipment source that reported
// recently, dating a sighting by when the reading arrived, never by now. It
// reports whether the live tree is being fed at all (any reading fresh).
func (r *sourceRegistry) observeLocked(t telemetryReader, now time.Time) bool {
	fed := false
	for _, p := range t.Paths() {
		_, equipment := sourceOfPath(p.Path)
		if !equipment && fed {
			continue
		}
		sample := t.Sample(p.Path)
		if !sample.Present || !sample.AgeKnown || sample.Age > sourceRegistryFreshness {
			continue
		}
		fed = true
		if equipment {
			r.seeLocked(p.Path, now.Add(-sample.Age), now)
		}
	}
	return fed
}

// creditLocked adds to the time watched: the time since the last tick, but only
// when both ticks saw the tree being fed, and never more than creditCap, so a
// backend that was down or a SignalK outage does not count toward the week.
func (r *sourceRegistry) creditLocked(fed bool, now time.Time) {
	prev, prevFed := r.lastTick, r.lastFed
	r.lastTick, r.lastFed = now, fed
	// Only the interval between two fed observations counts.
	if !fed || !prevFed || prev.IsZero() || !now.After(prev) {
		return
	}
	credit := now.Sub(prev)
	if credit > r.creditCap {
		credit = r.creditCap
	}
	r.meta.Watched += credit
	if r.meta.Watched-r.watchedWritten >= sourceRegistryWriteInterval {
		r.metaDirty = true
	}
}

// seeLocked records a sighting of path at time at. A new path is written at
// once; one already known only when its stored last_seen has gone stale.
func (r *sourceRegistry) seeLocked(path string, at, now time.Time) {
	st := r.paths[path]
	if st == nil {
		if len(r.paths) >= signalKContextMaxDistinctPaths {
			return
		}
		r.paths[path] = &seenState{first: at, last: at}
		r.dirty[path] = true
		return
	}
	if at.After(st.last) {
		st.last = at
	}
	if st.last.Sub(st.written) >= sourceRegistryWriteInterval {
		r.dirty[path] = true
	}
}

// seedDueLocked reports whether the InfluxDB seed should be tried now. InfluxDB
// logs this boat only, so the seed is taken once per installation, for the
// first vessel tracked; a later vessel relies on live tracking and the
// seven-day rule. An unconfigured InfluxDB is not an error and is not retried
// by query; a failed query is retried every sourceSeedRetry.
func (r *sourceRegistry) seedDueLocked(now time.Time) bool {
	if r.installSeeded {
		return false
	}
	if !r.seedConfigured() {
		if !r.noInfluxLogged {
			log.Printf("source registry: InfluxDB is not configured; tracking from live data only")
			r.noInfluxLogged = true
		}
		return false
	}
	if r.seedFailing && now.Sub(r.lastSeedTry) < sourceSeedRetry {
		return false
	}
	r.lastSeedTry = now
	return true
}

// applySeedLocked records the result of the seed query: a failure is logged
// once per streak, a success merges the history under what is already known
// and remembers that the installation was seeded. A seed that returned no
// equipment path is remembered too (so it is not repeated) but gives no
// coverage: the seven-day rule applies.
func (r *sourceRegistry) applySeedLocked(history map[string]time.Time, err error, now time.Time) {
	if err != nil {
		if !r.seedFailing {
			log.Printf("source registry: seeding from InfluxDB failed, will retry every %v: %v", sourceSeedRetry, err)
		}
		r.seedFailing = true
		return
	}
	r.seedFailing = false
	n := 0
	for path, at := range history {
		if _, ok := sourceOfPath(path); !ok {
			continue
		}
		n++
		st := r.paths[path]
		if st == nil {
			r.paths[path] = &seenState{first: at, last: at}
		} else {
			if at.Before(st.first) {
				st.first = at
			}
			if at.After(st.last) {
				st.last = at
			}
		}
		r.dirty[path] = true
	}
	r.meta.SeededAt, r.meta.SeededPaths = now, n
	r.installSeeded, r.metaDirty = true, true
	if n == 0 {
		log.Printf("source registry: InfluxDB held no equipment history; tracking from live data only")
		return
	}
	log.Printf("source registry: seeded %d paths from InfluxDB history (last %d days)", n, sourceHistoryLookbackDays)
}

// flushLocked writes everything pending, the current vessel's and any stashed
// from a vessel left behind, each in one transaction. On failure it logs once
// per streak and leaves the rows pending for the next pass.
func (r *sourceRegistry) flushLocked() {
	failed := false
	for vessel, p := range r.stash {
		if err := r.store.save(vessel, p.rows, p.meta); err != nil {
			r.noteWriteFailure(len(p.rows), err)
			failed = true
			continue
		}
		delete(r.stash, vessel)
	}
	if len(r.dirty) > 0 || r.metaDirty {
		rows := make([]seenRow, 0, len(r.dirty))
		for path := range r.dirty {
			st := r.paths[path]
			rows = append(rows, seenRow{Path: path, First: st.first, Last: st.last})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Path < rows[j].Path })
		var meta *registryMeta
		if r.metaDirty {
			m := r.meta
			meta = &m
		}
		if err := r.store.save(r.vessel, rows, meta); err != nil {
			r.noteWriteFailure(len(rows), err)
			failed = true
		} else {
			for _, row := range rows {
				r.paths[row.Path].written = row.Last
			}
			r.dirty, r.metaDirty = map[string]bool{}, false
			r.watchedWritten = r.meta.Watched
		}
	}
	if !failed && r.writeFailing {
		log.Printf("source registry: writing again")
		r.writeFailing = false
	}
}

func (r *sourceRegistry) noteWriteFailure(n int, err error) {
	if !r.writeFailing {
		log.Printf("source registry: could not write %d sightings, will retry: %v", n, err)
	}
	r.writeFailing = true
}

// publishLocked builds the immutable record readers get. Its maps are never
// changed after this, so readers need no lock.
func (r *sourceRegistry) publishLocked(now time.Time) {
	rec := &sourceRecord{Err: r.loadErr, Now: now}
	if r.loaded {
		last := make(map[string]time.Time, len(r.paths))
		for path, st := range r.paths {
			last[path] = st.last
		}
		rec.Last, rec.Active = last, activeSources(last)
		rec.Since = r.meta.TrackingSince
		rec.Seeded = !r.meta.SeededAt.IsZero() && r.meta.SeededPaths > 0
		rec.Covers = rec.Seeded || r.meta.Watched >= sourceRecordMinTracking
	}
	r.snap.Store(rec)
}

// record is what readers see: the sightings, when watching began, and whether
// the record is enough to judge a source missing its signature. It is the
// snapshot built at the last tick.
func (r *sourceRegistry) record() sourceRecord { return *r.snap.Load() }
