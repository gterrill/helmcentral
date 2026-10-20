package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sort"
	"sync"
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

// registryMeta is a vessel's tracking record: when Helmcentral began watching
// it, and, once seeded, when and from how far back.
type registryMeta struct {
	TrackingSince, SeededAt, SeededFrom time.Time
}

// sourceRegistryPersister is what the registry needs of the table; tests wrap
// the real store to count and fail transactions.
type sourceRegistryPersister interface {
	load(vessel string) ([]seenRow, *registryMeta, error)
	save(vessel string, rows []seenRow, meta *registryMeta) error
}

// sourceRegistryStore persists the registry in helmcentral.sqlite:
//
//	signalk_source_seen(vessel, path, first_seen, last_seen)  PRIMARY KEY (vessel, path)
//	signalk_source_tracking(vessel, tracking_since, seeded_at, seeded_from)  PRIMARY KEY (vessel)
//
// Times are Unix seconds. seeded_at and seeded_from are NULL until seeded.
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
			vessel         TEXT PRIMARY KEY,
			tracking_since INTEGER NOT NULL,
			seeded_at      INTEGER,
			seeded_from    INTEGER
		)`,
	} {
		if _, err := ds.db.Exec(stmt); err != nil {
			return nil, fmt.Errorf("create source registry tables: %w", err)
		}
	}
	return &sourceRegistryStore{ds: ds}, nil
}

func (s *sourceRegistryStore) load(vessel string) ([]seenRow, *registryMeta, error) {
	var rows []seenRow
	var meta *registryMeta
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
		m, err := q.Query(`SELECT tracking_since, seeded_at, seeded_from FROM signalk_source_tracking WHERE vessel = ?`, vessel)
		if err != nil {
			return err
		}
		defer m.Close()
		if m.Next() {
			var since int64
			var seededAt, seededFrom sql.NullInt64
			if err := m.Scan(&since, &seededAt, &seededFrom); err != nil {
				return err
			}
			meta = &registryMeta{TrackingSince: time.Unix(since, 0).UTC()}
			if seededAt.Valid {
				meta.SeededAt = time.Unix(seededAt.Int64, 0).UTC()
				meta.SeededFrom = time.Unix(seededFrom.Int64, 0).UTC()
			}
		}
		return m.Err()
	})
	if err != nil {
		return nil, nil, fmt.Errorf("source registry: load: %w", err)
	}
	return rows, meta, nil
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
			var seededAt, seededFrom any
			if !meta.SeededAt.IsZero() {
				seededAt, seededFrom = meta.SeededAt.Unix(), meta.SeededFrom.Unix()
			}
			if _, err := tx.Exec(`INSERT INTO signalk_source_tracking (vessel, tracking_since, seeded_at, seeded_from) VALUES (?, ?, ?, ?)
				ON CONFLICT(vessel) DO UPDATE SET
					seeded_at = COALESCE(excluded.seeded_at, seeded_at),
					seeded_from = COALESCE(excluded.seeded_from, seeded_from)`,
				vessel, meta.TrackingSince.Unix(), seededAt, seededFrom); err != nil {
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

type sourceRegistry struct {
	mu     sync.Mutex
	store  sourceRegistryPersister
	vessel string
	loaded bool
	paths  map[string]*seenState
	dirty  map[string]bool
	meta   registryMeta
	// metaDirty: meta has changes the table does not hold.
	metaDirty bool

	seedConfigured func() bool
	seedFetch      func() (map[string]time.Time, error)
	lastSeedTry    time.Time
	seedFailing    bool
	noInfluxLogged bool
	writeFailing   bool
	loadFailing    bool
}

func newSourceRegistry(store sourceRegistryPersister, seedConfigured func() bool, seedFetch func() (map[string]time.Time, error)) *sourceRegistry {
	return &sourceRegistry{
		store: store, seedConfigured: seedConfigured, seedFetch: seedFetch,
		paths: map[string]*seenState{}, dirty: map[string]bool{},
	}
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

// tick is one pass: load the self vessel's record if it changed, note what
// reports now, seed once from InfluxDB if due, and write what changed.
func (r *sourceRegistry) tick(snapshot *signalKSnapshot, now time.Time) {
	vessel := snapshot.selfContext()
	if vessel == "" {
		return
	}
	r.mu.Lock()
	if !r.loaded || r.vessel != vessel {
		if !r.loadLocked(vessel, now) {
			r.mu.Unlock()
			return
		}
	}
	r.observeLocked(snapshot.liveSelfTelemetry(now), now)
	due := r.seedDueLocked(now)
	r.mu.Unlock()

	// The InfluxDB query runs without the lock, so readers are never held
	// behind it.
	var history map[string]time.Time
	var err error
	if due {
		history, err = r.seedFetch()
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if due && r.vessel == vessel {
		r.applySeedLocked(history, err, now)
	}
	r.flushLocked()
}

// loadLocked replaces the in-memory record with vessel's stored one. It starts
// tracking (the date Helmcentral began watching) for a vessel seen for the
// first time.
func (r *sourceRegistry) loadLocked(vessel string, now time.Time) bool {
	rows, meta, err := r.store.load(vessel)
	if err != nil {
		if !r.loadFailing {
			log.Printf("source registry: %v; Discovery lists only sources reporting now", err)
		}
		r.loadFailing = true
		return false
	}
	r.loadFailing = false
	r.vessel, r.loaded = vessel, true
	r.paths, r.dirty = map[string]*seenState{}, map[string]bool{}
	for _, row := range rows {
		r.paths[row.Path] = &seenState{first: row.First, last: row.Last, written: row.Last}
	}
	if meta == nil {
		r.meta, r.metaDirty = registryMeta{TrackingSince: now}, true
	} else {
		r.meta, r.metaDirty = *meta, false
	}
	return true
}

// observeLocked notes every path under an equipment source that reported
// recently. A sighting is dated by when the reading arrived, never by now.
func (r *sourceRegistry) observeLocked(t telemetryReader, now time.Time) {
	for _, p := range t.Paths() {
		if _, ok := sourceOfPath(p.Path); !ok {
			continue
		}
		sample := t.Sample(p.Path)
		if !sample.Present || !sample.AgeKnown || sample.Age > sourceRegistryFreshness {
			continue
		}
		r.seeLocked(p.Path, now.Add(-sample.Age), now)
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

// seedDueLocked reports whether the InfluxDB seed should be tried now. An
// unconfigured InfluxDB is not an error and is not retried by query; a failed
// query is retried every sourceSeedRetry.
func (r *sourceRegistry) seedDueLocked(now time.Time) bool {
	if !r.meta.SeededAt.IsZero() {
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
// and remembers that the vessel was seeded.
func (r *sourceRegistry) applySeedLocked(history map[string]time.Time, err error, now time.Time) {
	if err != nil {
		if !r.seedFailing {
			log.Printf("source registry: seeding from InfluxDB failed, will retry every %v: %v", sourceSeedRetry, err)
		}
		r.seedFailing = true
		return
	}
	r.seedFailing = false
	for path, at := range history {
		if _, ok := sourceOfPath(path); !ok {
			continue
		}
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
	r.meta.SeededAt, r.meta.SeededFrom = now, now.AddDate(0, 0, -sourceHistoryLookbackDays)
	r.metaDirty = true
	log.Printf("source registry: seeded %d paths from InfluxDB history (last %d days)", len(history), sourceHistoryLookbackDays)
}

// flushLocked writes everything pending in one transaction. On failure it
// logs once per streak and leaves the rows pending for the next pass.
func (r *sourceRegistry) flushLocked() {
	if len(r.dirty) == 0 && !r.metaDirty {
		return
	}
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
		if !r.writeFailing {
			log.Printf("source registry: could not write %d sightings, will retry: %v", len(rows), err)
		}
		r.writeFailing = true
		return
	}
	if r.writeFailing {
		log.Printf("source registry: writing again")
		r.writeFailing = false
	}
	for _, row := range rows {
		r.paths[row.Path].written = row.Last
	}
	r.dirty, r.metaDirty = map[string]bool{}, false
}

// record is what readers see: the sightings, when watching began, and whether
// that is enough to judge a source missing its signature.
func (r *sourceRegistry) record(now time.Time) sourceRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	last := make(map[string]time.Time, len(r.paths))
	for path, st := range r.paths {
		last[path] = st.last
	}
	rec := sourceRecord{Last: last, Active: activeSources(last), Seeded: !r.meta.SeededAt.IsZero()}
	if r.loaded {
		rec.Since = r.meta.TrackingSince
		rec.Covers = rec.Seeded || now.Sub(r.meta.TrackingSince) >= sourceRecordMinTracking
	}
	return rec
}
