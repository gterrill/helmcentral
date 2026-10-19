package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

// SignalK declares a path's unit in meta ({"units":"ratio",...}), and the
// stream sends it only when asked (sendMeta=all). Helmcentral keeps the last
// unit each path declared (ADR 0193), so an alarm on a path whose device is
// off, or whose meta has not yet been replayed after a restart, still names
// its unit. A path that never declared one has no entry: the unit is never
// guessed from the path's name.

// signalKMeta is one entry of an update's meta array.
type signalKMeta struct {
	Path  string         `json:"path"`
	Value map[string]any `json:"value"`
}

// signalKUnitStore persists vessel -> path -> last declared unit in
// helmcentral.sqlite on the document store's connection (ADR 0141), writing
// through RunTx (ADR 0184). Units are keyed by the self vessel's context id,
// so a different boat's declarations are never served as this boat's.
type signalKUnitStore struct {
	ds *documentStore
}

// unitPersister is what the snapshot needs of the store; tests wrap the real
// one to count and fail transactions.
type unitPersister interface {
	load(vessel string) (map[string]string, error)
	save(changes []rememberedUnit) error
}

type rememberedUnit struct {
	vessel, path, units string
}

// unitKey identifies one pending write.
type unitKey struct {
	vessel, path string
}

func newSignalKUnitStore(ds *documentStore) (*signalKUnitStore, error) {
	// The table is new on this branch and has no installed base: a table
	// made before units were keyed by vessel is dropped and recreated.
	rows, err := ds.db.Query(`PRAGMA table_info(signalk_path_units)`)
	if err != nil {
		return nil, fmt.Errorf("inspect signalk_path_units table: %w", err)
	}
	exists, hasVessel := false, false
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			return nil, fmt.Errorf("inspect signalk_path_units table: %w", err)
		}
		exists = true
		if name == "vessel" {
			hasVessel = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("inspect signalk_path_units table: %w", err)
	}
	rows.Close()
	if exists && !hasVessel {
		if _, err := ds.db.Exec(`DROP TABLE signalk_path_units`); err != nil {
			return nil, fmt.Errorf("drop old signalk_path_units table: %w", err)
		}
	}
	if _, err := ds.db.Exec(`CREATE TABLE IF NOT EXISTS signalk_path_units (
		vessel      TEXT NOT NULL,
		path        TEXT NOT NULL,
		units       TEXT NOT NULL,
		declared_at INTEGER NOT NULL,
		PRIMARY KEY (vessel, path)
	)`); err != nil {
		return nil, fmt.Errorf("create signalk_path_units table: %w", err)
	}
	return &signalKUnitStore{ds: ds}, nil
}

func (s *signalKUnitStore) load(vessel string) (map[string]string, error) {
	out := map[string]string{}
	err := s.ds.Read(func(q sqlQueryer) error {
		rows, err := q.Query(`SELECT path, units FROM signalk_path_units WHERE vessel = ?`, vessel)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var path, units string
			if err := rows.Scan(&path, &units); err != nil {
				return err
			}
			out[path] = units
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("signalk unit store: load: %w", err)
	}
	return out, nil
}

func (s *signalKUnitStore) save(changes []rememberedUnit) error {
	return s.ds.RunTx("remember signalk units", func(tx *sql.Tx, now time.Time) error {
		for _, c := range changes {
			if _, err := tx.Exec(`INSERT INTO signalk_path_units (vessel, path, units, declared_at) VALUES (?, ?, ?, ?)
				ON CONFLICT(vessel, path) DO UPDATE SET units = excluded.units, declared_at = excluded.declared_at`,
				c.vessel, c.path, c.units, now.Unix()); err != nil {
				return fmt.Errorf("signalk unit store: save %s: %w", c.path, err)
			}
		}
		return nil
	})
}

// unitFlushInterval is how long the first unsaved declaration waits for
// company before the flusher writes the batch. The first burst after a
// subscribe carries hundreds of declarations; they go in one transaction.
const unitFlushInterval = 2 * time.Second

// useUnitStore loads the self vessel's remembered units and attaches the
// store, so later declarations are persisted. Call it before the stream
// starts; when the self vessel becomes known or changes, setSelfContext
// reloads for that vessel.
func (s *signalKSnapshot) useUnitStore(store unitPersister) error {
	s.mu.Lock()
	s.unitStore = store
	vessel := s.selfCtx
	s.mu.Unlock()
	return s.reloadUnits(vessel)
}

// reloadUnits replaces the in-memory units with vessel's stored ones, plus
// any of its declarations still waiting to be written. It does nothing if
// vessel is no longer the self vessel by the time the load returns.
func (s *signalKSnapshot) reloadUnits(vessel string) error {
	s.mu.RLock()
	store := s.unitStore
	s.mu.RUnlock()
	if store == nil || vessel == "" {
		return nil
	}
	loaded, err := store.load(vessel)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.selfCtx != vessel {
		return nil
	}
	for key, units := range s.pendingUnits {
		if key.vessel == vessel {
			loaded[key.path] = units
		}
	}
	s.rememberedUnits = loaded
	return nil
}

// rememberedUnit is the unit the self vessel last declared for path, "" when
// it never has.
func (s *signalKSnapshot) rememberedUnit(path string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rememberedUnits[path]
}

// unitsCopy is the self vessel's unit map, copied under one read lock, for a
// view that must not touch the snapshot lock per path.
func (s *signalKSnapshot) unitsCopy() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.rememberedUnits))
	for path, units := range s.rememberedUnits {
		out[path] = units
	}
	return out
}

// applyMetaLocked records the units an update's meta declares for the self
// vessel. Meta never enters the snapshot tree: it is not a reading, it does
// not touch pathSeen, and a meta path through a scalar or a path nothing
// has published must not reshape the tree. Other vessels' meta is dropped,
// since nothing reads their units. rememberedUnits is the one map readers
// consult, and it changes at once; a declaration that differs from it is
// also queued for the flusher. Caller holds s.mu for writing.
func (s *signalKSnapshot) applyMetaLocked(context string, metas []signalKMeta) {
	if context == "" || context != s.selfCtx {
		return
	}
	queued := false
	for _, m := range metas {
		if m.Path == "" || m.Value == nil || radarTargetDeltaPath(m.Path) {
			continue
		}
		units, _ := m.Value["units"].(string)
		if units == "" {
			continue
		}
		if n := strings.Count(m.Path, ".") + 1; n > signalKDeltaMaxPathSegments {
			log.Printf("signalk snapshot: dropping meta for %s: path has %d segments, more than the %d cap: %.80s...",
				context, n, signalKDeltaMaxPathSegments, m.Path)
			continue
		}
		prev, known := s.rememberedUnits[m.Path]
		if prev == units {
			continue
		}
		if !known && len(s.rememberedUnits) >= signalKContextMaxDistinctPaths {
			continue
		}
		s.rememberedUnits[m.Path] = units
		if s.unitStore != nil {
			s.pendingUnits[unitKey{vessel: context, path: m.Path}] = units
			queued = true
		}
	}
	if queued {
		s.kickUnitFlusher()
	}
}

func (s *signalKSnapshot) kickUnitFlusher() {
	select {
	case s.unitNotify <- struct{}{}:
	default:
	}
}

// flushUnits writes every pending declaration in one transaction. An entry
// leaves the pending set only once a write that carried its current value
// succeeds. On failure it logs once, keeps the in-memory units as declared
// and leaves the entries pending, so the next flush retries them.
func (s *signalKSnapshot) flushUnits() {
	s.unitFlushMu.Lock()
	defer s.unitFlushMu.Unlock()

	s.mu.RLock()
	store := s.unitStore
	batch := make(map[unitKey]string, len(s.pendingUnits))
	for key, units := range s.pendingUnits {
		batch[key] = units
	}
	s.mu.RUnlock()
	if store == nil || len(batch) == 0 {
		return
	}

	changes := make([]rememberedUnit, 0, len(batch))
	for key, units := range batch {
		changes = append(changes, rememberedUnit{vessel: key.vessel, path: key.path, units: units})
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].vessel != changes[j].vessel {
			return changes[i].vessel < changes[j].vessel
		}
		return changes[i].path < changes[j].path
	})

	if err := store.save(changes); err != nil {
		log.Printf("signalk snapshot: could not remember %d declared units, will retry: %v", len(changes), err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, units := range batch {
		if s.pendingUnits[key] == units {
			delete(s.pendingUnits, key)
		}
	}
}

func (s *signalKSnapshot) hasPendingUnits() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.pendingUnits) > 0
}

// runUnitFlusher writes pending declarations from a goroutine of its own, so
// the stream goroutine never waits on SQLite: a declaration arms a timer, and
// when it fires everything pending meanwhile goes in one transaction. A
// failed write is retried on the next timer. Production does not cancel ctx
// (main has no signal handling), so pending units are written within about 2
// s and a restart inside that window loses them until the device declares
// them again, which SignalK does on every subscribe. The flush when ctx ends
// is kept for tests and any future shutdown path.
func (s *signalKSnapshot) runUnitFlusher(ctx context.Context) {
	delay := s.unitFlushDelay
	if delay <= 0 {
		delay = unitFlushInterval
	}
	for {
		select {
		case <-ctx.Done():
			s.flushUnits()
			return
		case <-s.unitNotify:
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			s.flushUnits()
			return
		case <-timer.C:
		}
		s.flushUnits()
		if s.hasPendingUnits() {
			s.kickUnitFlusher()
		}
	}
}
