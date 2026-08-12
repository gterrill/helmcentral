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

	store := &alarmLogStore{db: db}
	if err := store.ensureQueueTable(); err != nil {
		db.Close()
		return nil, fmt.Errorf("create notification queue table: %w", err)
	}
	return store, nil
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

// ── delivery queue ────────────────────────────────────────────────────────────

// A boat's internet comes and goes, so a notification that fails to send is
// queued rather than dropped. A late alarm is far better than a lost one.

type queuedNotification struct {
	ID        string
	Transport string
	Payload   []byte
	Attempts  int
	LastError string
}

func (s *alarmLogStore) ensureQueueTable() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS notification_queue (
			id              TEXT PRIMARY KEY,
			transport       TEXT NOT NULL,
			payload         BLOB NOT NULL,
			attempts        INTEGER NOT NULL DEFAULT 0,
			next_attempt_at INTEGER NOT NULL,
			created_at      INTEGER NOT NULL,
			last_error      TEXT NOT NULL DEFAULT ''
		)`)
	return err
}

func (s *alarmLogStore) Enqueue(transport string, payload []byte, dueAt time.Time) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`INSERT INTO notification_queue (id, transport, payload, attempts, next_attempt_at, created_at)
		 VALUES (?, ?, ?, 0, ?, ?)`,
		uuid.NewString(), transport, payload, dueAt.UTC().Unix(), time.Now().UTC().Unix())
	if err != nil {
		return fmt.Errorf("enqueue notification: %w", err)
	}
	return nil
}

// DueNotifications returns queued deliveries whose backoff has elapsed.
func (s *alarmLogStore) DueNotifications(now time.Time, limit int) ([]queuedNotification, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT id, transport, payload, attempts, last_error FROM notification_queue
		 WHERE next_attempt_at <= ? ORDER BY created_at ASC LIMIT ?`,
		now.UTC().Unix(), limit)
	if err != nil {
		return nil, fmt.Errorf("read notification queue: %w", err)
	}
	defer rows.Close()

	var out []queuedNotification
	for rows.Next() {
		var item queuedNotification
		if err := rows.Scan(&item.ID, &item.Transport, &item.Payload, &item.Attempts, &item.LastError); err != nil {
			return nil, fmt.Errorf("scan notification queue: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *alarmLogStore) DeleteQueued(id string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.db.Exec(`DELETE FROM notification_queue WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete queued notification: %w", err)
	}
	return nil
}

// RescheduleQueued records a failed attempt and pushes the next one out.
func (s *alarmLogStore) RescheduleQueued(id string, attempts int, nextAttempt time.Time, lastError string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`UPDATE notification_queue SET attempts = ?, next_attempt_at = ?, last_error = ? WHERE id = ?`,
		attempts, nextAttempt.UTC().Unix(), lastError, id)
	if err != nil {
		return fmt.Errorf("reschedule queued notification: %w", err)
	}
	return nil
}

// DropQueuedOlderThan discards deliveries that have been retrying for too long.
// Retrying forever would eventually deliver a day-old alarm as if it were new,
// which is its own kind of wrong; the drop is logged by the caller.
func (s *alarmLogStore) DropQueuedOlderThan(cutoff time.Time) (int64, error) {
	if s == nil {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`DELETE FROM notification_queue WHERE created_at < ?`, cutoff.UTC().Unix())
	if err != nil {
		return 0, fmt.Errorf("drop stale queued notifications: %w", err)
	}
	return result.RowsAffected()
}

func (s *alarmLogStore) QueueDepth() (int, error) {
	if s == nil {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM notification_queue`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
