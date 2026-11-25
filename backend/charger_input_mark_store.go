package main

import (
	"database/sql"
	"fmt"
	"sync"
	"time"
)

/*
The operator's marks on battery instances, in helmcentral.sqlite
(charger_input_mark.go). One row per marked instance, with the moment it was
marked. The rows are held in memory because the anomaly tick reads them on
every pass; the database is only touched by a change. The table is empty until
the operator marks something, and holds no vessel's values by default.
*/

type chargerInputMarkStore struct {
	mu     sync.RWMutex
	db     *sql.DB
	path   string
	marked map[string]time.Time
}

// globalChargerInputMarks is opened once in main(), beside the ignore lists.
var globalChargerInputMarks *chargerInputMarkStore

func newChargerInputMarkStore(dbPath string) (*chargerInputMarkStore, error) {
	db, err := openSQLite("charger_input_marks", dbPath, sqliteProfileBusiness)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS charger_input_marks (instance TEXT PRIMARY KEY, marked_at INTEGER NOT NULL)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create charger_input_marks table: %w", err)
	}
	s := &chargerInputMarkStore{db: db, path: dbPath, marked: map[string]time.Time{}}
	rows, err := db.Query(`SELECT instance, marked_at FROM charger_input_marks`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("read charger_input_marks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var at int64
		if err := rows.Scan(&id, &at); err != nil {
			db.Close()
			return nil, fmt.Errorf("read charger_input_marks: %w", err)
		}
		s.marked[id] = time.Unix(at, 0).UTC()
	}
	if err := rows.Err(); err != nil {
		db.Close()
		return nil, fmt.Errorf("read charger_input_marks: %w", err)
	}
	return s, nil
}

func (s *chargerInputMarkStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// set marks or unmarks instance id. Both are idempotent, and marking again
// keeps the original date. A failed write leaves the marks as they were.
func (s *chargerInputMarkStore) set(id string, marked bool, at time.Time) error {
	if s == nil {
		return fmt.Errorf("charger-input marks are not available")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if marked {
		if _, ok := s.marked[id]; ok {
			return nil
		}
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO charger_input_marks (instance, marked_at) VALUES (?, ?)`, id, at.UTC().Unix()); err != nil {
			return fmt.Errorf("save charger-input mark %s: %w", id, err)
		}
		s.marked[id] = time.Unix(at.UTC().Unix(), 0).UTC()
		return nil
	}
	if _, err := s.db.Exec(`DELETE FROM charger_input_marks WHERE instance = ?`, id); err != nil {
		return fmt.Errorf("save charger-input mark %s: %w", id, err)
	}
	delete(s.marked, id)
	return nil
}

func (s *chargerInputMarkStore) has(id string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.marked[id]
	return ok
}

// all returns the marked instance ids as a set.
func (s *chargerInputMarkStore) all() map[string]bool {
	out := map[string]bool{}
	if s == nil {
		return out
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for id := range s.marked {
		out[id] = true
	}
	return out
}
