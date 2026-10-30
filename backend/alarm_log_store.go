package main

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
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

// createAlarmLogSchema creates alarm_log (and its indexes) and
// notification_queue if they do not already exist, and ALTER TABLEs
// notification_queue.rule_id onto a database that predates it. Factored out
// of newAlarmLogStore so migrateToHelmcentralDB (helmcentral_db.go) can
// create this store's tables in the combined database ahead of copying rows
// into them, without duplicating the schema itself.
func createAlarmLogSchema(db *sql.DB) error {
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
		return fmt.Errorf("create alarm log table: %w", err)
	}

	// The log is read newest-first and looked up by rule while an alarm is open.
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS alarm_log_raised_at ON alarm_log (raised_at DESC)`); err != nil {
		return fmt.Errorf("index alarm log: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS alarm_log_cleared_at ON alarm_log (cleared_at)`); err != nil {
		return fmt.Errorf("index alarm log: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS alarm_log_open ON alarm_log (rule_id, cleared_at)`); err != nil {
		return fmt.Errorf("index alarm log: %w", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS notification_queue (
			id              TEXT PRIMARY KEY,
			transport       TEXT NOT NULL,
			rule_id         TEXT NOT NULL DEFAULT '',
			payload         BLOB NOT NULL,
			attempts        INTEGER NOT NULL DEFAULT 0,
			next_attempt_at INTEGER NOT NULL,
			created_at      INTEGER NOT NULL,
			last_error      TEXT NOT NULL DEFAULT ''
		)`); err != nil {
		return fmt.Errorf("create notification queue table: %w", err)
	}

	// rule_id arrived after the table shipped, and it is what lets a newer
	// transition supersede an older queued one. Databases created before it
	// carry rows with the default empty rule, which supersede nothing -- they
	// drain on their own within notifyMaxAge.
	if _, err := db.Exec(`ALTER TABLE notification_queue ADD COLUMN rule_id TEXT NOT NULL DEFAULT ''`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("create notification queue table: %w", err)
	}

	// The navigation.state the collision profile syncer last acted on: one
	// row, kept here beside the refusal rows it settles.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS collision_profile_state (
			id    INTEGER PRIMARY KEY CHECK (id = 1),
			state TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("create collision profile state table: %w", err)
	}
	return nil
}

// LoadCollisionProfileState returns the navigation state the collision
// profile syncer last acted on, empty when none has been stored yet.
func (s *alarmLogStore) LoadCollisionProfileState() (string, error) {
	if s == nil {
		return "", fmt.Errorf("alarm log is not available")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var state string
	err := s.db.QueryRow(`SELECT state FROM collision_profile_state WHERE id = 1`).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read collision profile state: %w", err)
	}
	return state, nil
}

func (s *alarmLogStore) SaveCollisionProfileState(state string) error {
	if s == nil {
		return fmt.Errorf("alarm log is not available")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(
		`INSERT INTO collision_profile_state (id, state) VALUES (1, ?)
		 ON CONFLICT(id) DO UPDATE SET state = excluded.state`, state); err != nil {
		return fmt.Errorf("save collision profile state: %w", err)
	}
	return nil
}

func newAlarmLogStore(dbPath string) (*alarmLogStore, error) {
	db, err := openHelmcentralDB(dbPath)
	if err != nil {
		return nil, err
	}

	if err := createAlarmLogSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	return &alarmLogStore{db: db}, nil
}

func (s *alarmLogStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// RecordRaised opens a new occurrence. Each raise after a clear is its own row,
// so the log shows how often a condition recurs rather than collapsing it to
// one entry.
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

// RecordStateChange moves the open occurrence for a rule to a new severity, as
// a tank does going from warn to alarm and back (ADR 0214). One occurrence, so
// the log reads as one alarm that changed. An escalation drops the
// acknowledgement, because it is unacknowledged again; a de-escalation keeps it.
func (s *alarmLogStore) RecordStateChange(ruleID, state, message string, escalated bool) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	ackedSQL := "acked_at"
	if escalated {
		ackedSQL = "NULL"
	}
	query := fmt.Sprintf(`
		UPDATE alarm_log SET state = ?, message = ?, acked_at = %s
		WHERE id = (
			SELECT id FROM alarm_log
			WHERE rule_id = ? AND cleared_at IS NULL
			ORDER BY raised_at DESC LIMIT 1
		)`, ackedSQL)
	if _, err := s.db.Exec(query, state, message, ruleID); err != nil {
		return fmt.Errorf("record state change: %w", err)
	}
	return nil
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

// OpenOccurrence returns the newest still-open (uncleared) log row for a rule,
// if any. This is how a bus notification's raised_at is recovered: the bus
// itself carries no raised time worth reading (SignalK rewrites a
// notification's own timestamp to the moment it is acknowledged), but the log
// row the bus watcher wrote when the notification first crossed its dwell
// (alarm_bus_watch.go) still has it.
func (s *alarmLogStore) OpenOccurrence(ruleID string) (alarmLogEntry, bool, error) {
	if s == nil {
		return alarmLogEntry{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(
		`SELECT id, rule_id, source, label, path, state, message, value_at_raise, raised_at, acked_at
		 FROM alarm_log WHERE rule_id = ? AND cleared_at IS NULL ORDER BY raised_at DESC LIMIT 1`, ruleID)

	var entry alarmLogEntry
	var raised int64
	var acked sql.NullInt64
	err := row.Scan(&entry.ID, &entry.RuleID, &entry.Source, &entry.Label, &entry.Path,
		&entry.State, &entry.Message, &entry.ValueAtRaise, &raised, &acked)
	if errors.Is(err, sql.ErrNoRows) {
		return alarmLogEntry{}, false, nil
	}
	if err != nil {
		return alarmLogEntry{}, false, fmt.Errorf("read open alarm occurrence: %w", err)
	}

	entry.RaisedAt = time.Unix(raised, 0).UTC()
	if acked.Valid {
		t := time.Unix(acked.Int64, 0).UTC()
		entry.AckedAt = &t
	}
	return entry, true, nil
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

// logRowScanner is what *sql.Row and *sql.Rows share.
type logRowScanner interface{ Scan(dest ...any) error }

const alarmLogColumns = `id, rule_id, source, label, path, state, message, value_at_raise, raised_at, acked_at, cleared_at`

func scanAlarmLogRow(r logRowScanner) (alarmLogEntry, error) {
	var entry alarmLogEntry
	var raised int64
	var acked, cleared sql.NullInt64
	if err := r.Scan(&entry.ID, &entry.RuleID, &entry.Source, &entry.Label, &entry.Path,
		&entry.State, &entry.Message, &entry.ValueAtRaise, &raised, &acked, &cleared); err != nil {
		return alarmLogEntry{}, err
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
	return entry, nil
}

// Get returns one occurrence by its log id (Ask Mate about a cleared alarm).
func (s *alarmLogStore) Get(id string) (alarmLogEntry, bool, error) {
	return s.one(`SELECT `+alarmLogColumns+` FROM alarm_log WHERE id = ?`, id)
}

// LatestForRule returns the newest occurrence of a rule, open or cleared.
func (s *alarmLogStore) LatestForRule(ruleID string) (alarmLogEntry, bool, error) {
	return s.one(`SELECT `+alarmLogColumns+` FROM alarm_log WHERE rule_id = ? ORDER BY raised_at DESC, id DESC LIMIT 1`, ruleID)
}

func (s *alarmLogStore) one(query string, args ...any) (alarmLogEntry, bool, error) {
	if s == nil {
		return alarmLogEntry{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, err := scanAlarmLogRow(s.db.QueryRow(query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return alarmLogEntry{}, false, nil
	}
	if err != nil {
		return alarmLogEntry{}, false, fmt.Errorf("read alarm log: %w", err)
	}
	return entry, true, nil
}

// RaisedCount is how many times a rule was raised since the given time.
func (s *alarmLogStore) RaisedCount(ruleID string, since time.Time) (int, error) {
	if s == nil {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM alarm_log WHERE rule_id = ? AND raised_at >= ?`, ruleID, since.UTC().Unix()).Scan(&n); err != nil {
		return 0, fmt.Errorf("count alarm raises: %w", err)
	}
	return n, nil
}

// EarliestRaised is the first raise of a rule since the given time.
func (s *alarmLogStore) EarliestRaised(ruleID string, since time.Time) (time.Time, bool, error) {
	if s == nil {
		return time.Time{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var raised sql.NullInt64
	if err := s.db.QueryRow(`SELECT MIN(raised_at) FROM alarm_log WHERE rule_id = ? AND raised_at >= ?`,
		ruleID, since.UTC().Unix()).Scan(&raised); err != nil {
		return time.Time{}, false, fmt.Errorf("read alarm log: %w", err)
	}
	if !raised.Valid {
		return time.Time{}, false, nil
	}
	return time.Unix(raised.Int64, 0).UTC(), true, nil
}

// ChangedBetween lists occurrences raised or cleared within [from, to],
// oldest first, leaving out every row of exceptRuleID (empty keeps all). The
// exclusion is in SQL so the alarm being asked about, repeating, cannot use up
// the row limit and hide what else changed. Each half is an indexed range.
func (s *alarmLogStore) ChangedBetween(from, to time.Time, exceptRuleID string) ([]alarmLogEntry, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, t := from.UTC().Unix(), to.UTC().Unix()
	rows, err := s.db.Query(`SELECT * FROM (
			SELECT `+alarmLogColumns+` FROM alarm_log WHERE raised_at BETWEEN ? AND ? AND rule_id <> ?
			UNION
			SELECT `+alarmLogColumns+` FROM alarm_log WHERE cleared_at BETWEEN ? AND ? AND rule_id <> ?
		) ORDER BY raised_at ASC, id ASC LIMIT 200`, f, t, exceptRuleID, f, t, exceptRuleID)
	if err != nil {
		return nil, fmt.Errorf("read alarm log: %w", err)
	}
	defer rows.Close()
	var out []alarmLogEntry
	for rows.Next() {
		e, err := scanAlarmLogRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan alarm log: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// alarmRaisedGroup is how often one alarm title was raised in a window.
type alarmRaisedGroup struct {
	Title       string
	Count       int
	First, Last time.Time
	// Sources is how many distinct sources raised it, read from the "@context"
	// on the rule id; 0 when the alarm has no such context. SourceKind says
	// what they are when that is known, and TopSources names the busiest named
	// ones, one entry per source. A source with no name is counted but never
	// listed. All three stay empty when any row of the title has no context,
	// because "from N" would then undercount.
	Sources    int
	SourceKind alarmSourceKind
	TopSources []alarmSourceCount
}

// alarmSourceCount is one named source and how often it raised the alarm.
type alarmSourceCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// alarmTopSourcesCap is how many named sources a group lists.
const alarmTopSourcesCap = 3

// RaisedSummary counts the alarms raised within [from, to] by title, most
// often raised first and then most recent. The counting is done in SQL, so a
// window with thousands of rows is counted whole rather than cut at the
// ChangedBetween row limit.
func (s *alarmLogStore) RaisedSummary(from, to time.Time) ([]alarmRaisedGroup, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT label, COUNT(*), MIN(raised_at), MAX(raised_at) FROM alarm_log
		WHERE raised_at BETWEEN ? AND ? GROUP BY label
		ORDER BY COUNT(*) DESC, MAX(raised_at) DESC, label ASC`, from.UTC().Unix(), to.UTC().Unix())
	if err != nil {
		return nil, fmt.Errorf("summarise alarm log: %w", err)
	}
	defer rows.Close()
	var out []alarmRaisedGroup
	for rows.Next() {
		var g alarmRaisedGroup
		var first, last int64
		if err := rows.Scan(&g.Title, &g.Count, &first, &last); err != nil {
			return nil, fmt.Errorf("scan alarm log summary: %w", err)
		}
		g.First, g.Last = time.Unix(first, 0).UTC(), time.Unix(last, 0).UTC()
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.addRaisedSources(out, from, to); err != nil {
		return nil, err
	}
	return out, nil
}

// addRaisedSources fills Sources, SourceKind and TopSources for the groups whose
// every row has a rule id carrying a "@context". One SQL row comes back per
// distinct rule id and message, so the read is bounded by the sources and the
// wordings in the window, never by the alarms. A source is a rule id: two
// vessels with one name stay two. The caller holds s.mu.
func (s *alarmLogStore) addRaisedSources(groups []alarmRaisedGroup, from, to time.Time) error {
	rows, err := s.db.Query(`SELECT label, rule_id, message, COUNT(*) FROM alarm_log
		WHERE raised_at BETWEEN ? AND ? AND instr(rule_id, ?) > 0
		GROUP BY label, rule_id, message`, from.UTC().Unix(), to.UTC().Unix(), notificationVesselSeparator)
	if err != nil {
		return fmt.Errorf("summarise alarm sources: %w", err)
	}
	defer rows.Close()
	index := map[string]int{}
	for i, g := range groups {
		index[g.Title] = i
	}
	type source struct {
		count int
		names map[string]int // named wordings and how often each was raised
	}
	sources := map[string]map[string]*source{} // label, then rule id
	kinds := map[string]alarmSourceKind{}
	covered := map[string]int{} // alarms per label whose rule id has a context
	for rows.Next() {
		var label, ruleID, message string
		var n int
		if err := rows.Scan(&label, &ruleID, &message, &n); err != nil {
			return fmt.Errorf("scan alarm sources: %w", err)
		}
		if _, ok := index[label]; !ok {
			continue
		}
		if sources[label] == nil {
			sources[label] = map[string]*source{}
			kinds[label] = alarmSourceKindOf(ruleID)
		}
		src := sources[label][ruleID]
		if src == nil {
			src = &source{names: map[string]int{}}
			sources[label][ruleID] = src
		}
		src.count += n
		covered[label] += n
		if name, ok := collisionTargetName(message); ok {
			src.names[name] += n
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for label, bySource := range sources {
		g := &groups[index[label]]
		if covered[label] != g.Count {
			continue
		}
		g.Sources, g.SourceKind = len(bySource), kinds[label]
		for _, src := range bySource {
			name, best := "", 0
			for candidate, n := range src.names {
				if n > best || (n == best && candidate < name) {
					name, best = candidate, n
				}
			}
			if name != "" {
				g.TopSources = append(g.TopSources, alarmSourceCount{name, src.count})
			}
		}
		sort.Slice(g.TopSources, func(a, b int) bool {
			x, y := g.TopSources[a], g.TopSources[b]
			if x.Count != y.Count {
				return x.Count > y.Count
			}
			return x.Name < y.Name
		})
		if len(g.TopSources) > alarmTopSourcesCap {
			g.TopSources = g.TopSources[:alarmTopSourcesCap]
		}
	}
	return nil
}

// alarmLogRetention bounds how long an alarm_log row is kept. Unlike
// notification_queue (a delivery-attempt queue, retained only notifyMaxAge
// before a late send is deemed as-good-as-lost) this table is a diagnostic
// history a boat owner reads back to see how often a condition has recurred
// (RecordRaised's own doc comment), so the window is generous -- a year,
// comfortably longer than any legitimate looking-back a live-aboard
// actually does. Its purpose is not freshness, it is bounding what was
// previously UNBOUNDED growth: nothing ever deleted from this table before,
// so a flood of injected bus notifications (alarm_bus_watch.go records one
// row per raise, source=alarmSourceSignalK, and anything on the SignalK bus
// can raise one) grew it forever (K-2 adjacent finding, backend security
// audit).
const alarmLogRetention = 365 * 24 * time.Hour

// DropLogOlderThan discards alarm_log rows raised before cutoff, the same
// retention shape as DropQueuedOlderThan below for the notification queue.
// The caller logs what was dropped, matching that pattern too.
func (s *alarmLogStore) DropLogOlderThan(cutoff time.Time) (int64, error) {
	if s == nil {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`DELETE FROM alarm_log WHERE raised_at < ?`, cutoff.UTC().Unix())
	if err != nil {
		return 0, fmt.Errorf("drop stale alarm log rows: %w", err)
	}
	return result.RowsAffected()
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

func (s *alarmLogStore) Enqueue(transport, ruleID string, payload []byte, dueAt time.Time) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`INSERT INTO notification_queue (id, transport, rule_id, payload, attempts, next_attempt_at, created_at)
		 VALUES (?, ?, ?, ?, 0, ?, ?)`,
		uuid.NewString(), transport, ruleID, payload, dueAt.UTC().Unix(), time.Now().UTC().Unix())
	if err != nil {
		return fmt.Errorf("enqueue notification: %w", err)
	}
	return nil
}

// DropQueuedForRule discards deliveries a newer transition has made obsolete.
//
// A notification states what a rule's state is now, so an older queued one is
// not late news, it is wrong news: delivered after the transition that replaced
// it, it overwrites the current state with a stale one. On the SignalK
// transport that is unrecoverable without another transition, because the bus
// holds the notification object rather than a stream of events -- a queued
// raise landing after its clear leaves a false alarm nothing will clear again.
func (s *alarmLogStore) DropQueuedForRule(transport, ruleID string) (int64, error) {
	if s == nil || ruleID == "" {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
		`DELETE FROM notification_queue WHERE transport = ? AND rule_id = ?`, transport, ruleID)
	if err != nil {
		return 0, fmt.Errorf("drop superseded notifications: %w", err)
	}
	return result.RowsAffected()
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

// QueuedForKey returns the deliveries still queued under a key, oldest first.
func (s *alarmLogStore) QueuedForKey(key string) ([]queuedNotification, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT id, transport, payload, attempts, last_error FROM notification_queue WHERE rule_id = ? ORDER BY created_at ASC`, key)
	if err != nil {
		return nil, fmt.Errorf("read queued notifications: %w", err)
	}
	defer rows.Close()
	var out []queuedNotification
	for rows.Next() {
		var item queuedNotification
		if err := rows.Scan(&item.ID, &item.Transport, &item.Payload, &item.Attempts, &item.LastError); err != nil {
			return nil, fmt.Errorf("scan queued notifications: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
