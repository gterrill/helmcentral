package main

import (
	"database/sql"
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

// FirstSuggestion keeps t as the departure suggested for a leg on day, unless the
// day already has one, and returns the one kept. Days more than two before this
// one are dropped.
func (s *documentStore) FirstSuggestion(planID int64, legKey, day string, t time.Time) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var kept time.Time
	err := s.runTxLocked("save first suggestion", func(tx *sql.Tx, _ time.Time) error {
		if _, err := tx.Exec(`DELETE FROM plan_first_suggestions WHERE day < ?`, dayPlus(day, -2)); err != nil {
			return fmt.Errorf("save first suggestion: %w", err)
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO plan_first_suggestions (plan_id, leg_key, day, depart_ms) VALUES (?, ?, ?, ?)`,
			planID, legKey, day, t.UnixMilli()); err != nil {
			return fmt.Errorf("save first suggestion: %w", err)
		}
		var ms int64
		if err := tx.QueryRow(`SELECT depart_ms FROM plan_first_suggestions WHERE plan_id = ? AND leg_key = ? AND day = ?`,
			planID, legKey, day).Scan(&ms); err != nil {
			return fmt.Errorf("save first suggestion: %w", err)
		}
		kept = time.UnixMilli(ms).In(t.Location())
		return nil
	})
	return kept, err
}
