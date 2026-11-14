package main

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// The first departure the assessment suggested for a leg on a day (ADR 0288).
// A suggestion is always for a time from now on, so by the time the boat is late
// it has moved on; the first one of the day is what the boat was expected to
// leave at. It is kept in the database so a restart does not lose it.

func createPlanFirstSuggestionSchema(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS plan_first_suggestions (
		plan_id   INTEGER NOT NULL,
		leg_key   TEXT NOT NULL,
		day       TEXT NOT NULL,
		depart_ms INTEGER NOT NULL,
		PRIMARY KEY (plan_id, leg_key, day)
	)`); err != nil {
		return fmt.Errorf("create plan first suggestions schema: %w", err)
	}
	return nil
}

// KeptSuggestion is the departure kept for a leg on day, in loc, and false when
// the day has none. It never writes.
func (s *documentStore) KeptSuggestion(planID int64, legKey, day string, loc *time.Location) (time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keptSuggestionLocked(s.db, planID, legKey, day, loc)
}

func (s *documentStore) keptSuggestionLocked(q interface {
	QueryRow(string, ...any) *sql.Row
}, planID int64, legKey, day string, loc *time.Location) (time.Time, bool, error) {
	var ms int64
	err := q.QueryRow(`SELECT depart_ms FROM plan_first_suggestions WHERE plan_id = ? AND leg_key = ? AND day = ?`,
		planID, legKey, day).Scan(&ms)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("read first suggestion: %w", err)
	}
	return time.UnixMilli(ms).In(loc), true, nil
}

// FirstSuggestion keeps t as the departure suggested for a leg on day, unless the
// day already has one, and returns the one kept. It writes only when the leg has
// no row for the day, and then drops the days more than two before this one, at
// most once for each day it is asked about.
func (s *documentStore) FirstSuggestion(planID int64, legKey, day string, t time.Time) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if kept, ok, err := s.keptSuggestionLocked(s.db, planID, legKey, day, t.Location()); err != nil || ok {
		return kept, err
	}
	err := s.runTxLocked("save first suggestion", func(tx *sql.Tx, _ time.Time) error {
		if s.firstSuggestionsPrunedOn != day {
			if _, err := tx.Exec(`DELETE FROM plan_first_suggestions WHERE day < ?`, dayPlus(day, -2)); err != nil {
				return fmt.Errorf("save first suggestion: %w", err)
			}
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO plan_first_suggestions (plan_id, leg_key, day, depart_ms) VALUES (?, ?, ?, ?)`,
			planID, legKey, day, t.UnixMilli()); err != nil {
			return fmt.Errorf("save first suggestion: %w", err)
		}
		return nil
	})
	if err != nil {
		return time.Time{}, err
	}
	s.firstSuggestionsPrunedOn = day
	kept, ok, err := s.keptSuggestionLocked(s.db, planID, legKey, day, t.Location())
	if err == nil && !ok {
		err = fmt.Errorf("save first suggestion: the row was not kept")
	}
	return kept, err
}
