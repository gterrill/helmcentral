package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// Where an alarm came from. Rules are Helmcentral's own; signalk entries are
// notifications raised by anything else on the bus (ADR 0038).
const (
	alarmSourceRule    = "rule"
	alarmSourceSignalK = "signalk"
)

type alarmLogEntry struct {
	ID           string     `json:"id"`
	RuleID       string     `json:"rule_id"`
	Source       string     `json:"source"`
	Label        string     `json:"label"`
	Path         string     `json:"path"`
	State        string     `json:"state"`
	Message      string     `json:"message"`
	ValueAtRaise float64    `json:"value_at_raise"`
	RaisedAt     time.Time  `json:"raised_at"`
	AckedAt      *time.Time `json:"acked_at,omitempty"`
	ClearedAt    *time.Time `json:"cleared_at,omitempty"`
}

type alarmLogStore struct {
	mu sync.Mutex
	db *sql.DB
}

var globalAlarmLogStore *alarmLogStore

func alarmLogDBPath() string {
	return cacheFilePath("ALARM_LOG_DB", "data/alarm-log.sqlite")
}

func newAlarmLogStore(dbPath string) (*alarmLogStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("alarm log dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open alarm log: %w", err)
	}
	// modernc/sqlite surfaces concurrent writes as "database is locked"; the
	// other stores in this package serialize the same way.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS alarm_log (
			id             TEXT PRIMARY KEY,
			rule_id        TEXT NOT NULL,
			source         TEXT NOT NULL,
			label          TEXT NOT NULL,
			path           TEXT NOT NULL,
			state          TEXT NOT NULL,
			message        TEXT NOT NULL,
			value_at_raise REAL NOT NULL,
			raised_at      INTEGER NOT NULL,
			acked_at       INTEGER,
			cleared_at     INTEGER
		)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create alarm log table: %w", err)
	}

	// The log is read newest-first and looked up by rule while an alarm is open.
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS alarm_log_raised_at ON alarm_log (raised_at DESC)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("index alarm log: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS alarm_log_open ON alarm_log (rule_id, cleared_at)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("index alarm log: %w", err)
	}

	return &alarmLogStore{db: db}, nil
}

func (s *alarmLogStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// RecordRaised opens a new occurrence. Each raise is its own row, so the log
// shows how often a condition recurs rather than collapsing it to one entry.
func (s *alarmLogStore) RecordRaised(entry alarmLogEntry) (string, error) {
	if s == nil {
		return "", nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.Source == "" {
		entry.Source = alarmSourceRule
	}

	_, err := s.db.Exec(
		`INSERT INTO alarm_log (id, rule_id, source, label, path, state, message, value_at_raise, raised_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.RuleID, entry.Source, entry.Label, entry.Path, entry.State,
		entry.Message, entry.ValueAtRaise, entry.RaisedAt.UTC().Unix())
	if err != nil {
		return "", fmt.Errorf("record raised alarm: %w", err)
	}
	return entry.ID, nil
}

// MarkAcknowledged stamps the still-open occurrence for a rule.
func (s *alarmLogStore) MarkAcknowledged(ruleID string, at time.Time) error {
	return s.stampOpen(ruleID, "acked_at", at)
}

// MarkCleared closes the open occurrence for a rule.
func (s *alarmLogStore) MarkCleared(ruleID string, at time.Time) error {
	return s.stampOpen(ruleID, "cleared_at", at)
}

func (s *alarmLogStore) stampOpen(ruleID string, column string, at time.Time) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Only the newest open occurrence: an alarm that has already been closed
	// must not be re-stamped by a later transition of the same rule.
	query := fmt.Sprintf(`
		UPDATE alarm_log SET %s = ?
		WHERE id = (
			SELECT id FROM alarm_log
			WHERE rule_id = ? AND cleared_at IS NULL
			ORDER BY raised_at DESC LIMIT 1
		)`, column)

	if _, err := s.db.Exec(query, at.UTC().Unix(), ruleID); err != nil {
		return fmt.Errorf("stamp %s: %w", column, err)
	}
	return nil
}

// Recent returns the newest occurrences first.
func (s *alarmLogStore) Recent(limit int) ([]alarmLogEntry, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT id, rule_id, source, label, path, state, message, value_at_raise, raised_at, acked_at, cleared_at
		 FROM alarm_log ORDER BY raised_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("read alarm log: %w", err)
	}
	defer rows.Close()

	entries := make([]alarmLogEntry, 0, limit)
	for rows.Next() {
		var entry alarmLogEntry
		var raised int64
		var acked, cleared sql.NullInt64

		if err := rows.Scan(&entry.ID, &entry.RuleID, &entry.Source, &entry.Label, &entry.Path,
			&entry.State, &entry.Message, &entry.ValueAtRaise, &raised, &acked, &cleared); err != nil {
			return nil, fmt.Errorf("scan alarm log: %w", err)
		}

		entry.RaisedAt = time.Unix(raised, 0).UTC()
		if acked.Valid {
			t := time.Unix(acked.Int64, 0).UTC()
			entry.AckedAt = &t
		}
		if cleared.Valid {
			t := time.Unix(cleared.Int64, 0).UTC()
			entry.ClearedAt = &t
		}
		entries = append(entries, entry)
	}

	return entries, rows.Err()
}
