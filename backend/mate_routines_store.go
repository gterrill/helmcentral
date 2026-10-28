package main

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

/*
Mate routines in helmcentral.sqlite (ADR 0227).

One table, mate_routines, beside the alarm rules. A routine is a standing
instruction ("check the boat every hour", "a morning summary at 06:00") plus
the bookkeeping the scheduler leaves behind: when it last ran and what came of
it. The definition columns (name, cadence, daily_time, focus, enabled) are
what an operator or a Mate proposal changes, and updated_at is the version a
proposal is checked against. The last_* columns are the scheduler's alone; it
writes them without touching updated_at, so a run never makes a waiting
proposal stale.

Each routine owns one Mate conversation, created in the same transaction as
the routine (the conversations table is in the same file), and
conversations.routine_id points back at it. Deleting a routine leaves the
conversation and its findings where they are.
*/

const (
	routineHourly = "hourly"
	routineDaily  = "daily"

	routineOutcomeRan     = "ran"
	routineOutcomeSkipped = "skipped"

	// maxMateRoutines keeps unattended spend easy to reason about.
	maxMateRoutines = 8
)

var errMateRoutineNotFound = errors.New("no such routine")

type mateRoutine struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Cadence        string     `json:"cadence"`
	DailyTime      string     `json:"daily_time,omitempty"`
	Focus          string     `json:"focus"`
	Enabled        bool       `json:"enabled"`
	ConversationID string     `json:"conversation_id"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	LastRunAt      *time.Time `json:"last_run_at,omitempty"`
	LastOutcome    string     `json:"last_outcome,omitempty"`
	LastNote       string     `json:"last_note,omitempty"`
	LastFindingAt  *time.Time `json:"last_finding_at,omitempty"`
	// LastSkipAt is when a "no boat position" note was last left. It is kept
	// apart from LastRunAt because that skip is not a run: the run is still
	// owed when the position returns.
	LastSkipAt *time.Time `json:"last_skip_at,omitempty"`
	// ScheduleChangedAt is when the routine was made or its cadence or time of
	// day last changed. A daily routine is due only for a time after it.
	ScheduleChangedAt time.Time `json:"schedule_changed_at"`
}

func createMateRoutinesSchema(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS mate_routines (
			id              TEXT PRIMARY KEY,
			name            TEXT NOT NULL,
			cadence         TEXT NOT NULL,
			daily_time      TEXT,
			focus           TEXT NOT NULL,
			enabled         INTEGER NOT NULL,
			conversation_id TEXT NOT NULL,
			created_at      INTEGER NOT NULL,
			updated_at      INTEGER NOT NULL,
			last_run_at     INTEGER,
			last_outcome    TEXT,
			last_note       TEXT,
			last_finding_at INTEGER,
			last_skip_at    INTEGER,
			schedule_changed_at INTEGER NOT NULL DEFAULT 0
		)`); err != nil {
		return fmt.Errorf("create mate_routines table: %w", err)
	}
	// Columns that arrived after the table did.
	for _, col := range []struct{ name, def string }{
		{"last_skip_at", "INTEGER"},
		{"schedule_changed_at", "INTEGER NOT NULL DEFAULT 0"},
	} {
		var has int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('mate_routines') WHERE name = ?`, col.name).Scan(&has); err != nil {
			return fmt.Errorf("inspect mate_routines table: %w", err)
		}
		if has == 0 {
			if _, err := db.Exec(`ALTER TABLE mate_routines ADD COLUMN ` + col.name + ` ` + col.def); err != nil {
				return fmt.Errorf("add mate_routines.%s: %w", col.name, err)
			}
		}
	}
	// The spend ledger: one row for every run that reached the model, kept
	// apart from the conversations so a run that failed (and left no reply)
	// still counts, and deleting a conversation cannot lower the total.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS mate_routine_runs (
			id         TEXT PRIMARY KEY,
			routine_id TEXT NOT NULL,
			started_at INTEGER NOT NULL,
			cost_usd   REAL NOT NULL,
			cost_known INTEGER NOT NULL,
			outcome    TEXT NOT NULL,
			note       TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("create mate_routine_runs table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS mate_routine_runs_started ON mate_routine_runs (started_at)`); err != nil {
		return fmt.Errorf("index mate_routine_runs table: %w", err)
	}
	return nil
}

// mateRoutineRunRow is one ledger row. CostKnown false means the run failed
// somewhere its charge could not be read back.
type mateRoutineRunRow struct {
	RoutineID string
	StartedAt time.Time
	CostUSD   float64
	CostKnown bool
	Outcome   string
	Note      string
}

func insertMateRoutineRunTx(x alarmSQLExecer, r mateRoutineRunRow) error {
	if _, err := x.Exec(`INSERT INTO mate_routine_runs (id, routine_id, started_at, cost_usd, cost_known, outcome, note)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, uuid.NewString(), r.RoutineID, routineStoreTime(r.StartedAt), r.CostUSD,
		boolToInt(r.CostKnown), r.Outcome, r.Note); err != nil {
		return fmt.Errorf("record routine spend: %w", err)
	}
	return nil
}

// routineSpend is what routines have spent in a window. UnknownAt is the
// latest run in it whose cost could not be read, nil when every cost is known.
type routineSpend struct {
	USD       float64
	UnknownAt *time.Time
}

func mateRoutineSpendSince(s *documentStore, since time.Time) (routineSpend, error) {
	var out routineSpend
	err := s.Read(func(q sqlQueryer) error {
		if err := q.QueryRow(`SELECT COALESCE(SUM(cost_usd), 0) FROM mate_routine_runs WHERE started_at >= ?`,
			routineStoreTime(since)).Scan(&out.USD); err != nil {
			return fmt.Errorf("read routine spend: %w", err)
		}
		var unknown sql.NullInt64
		if err := q.QueryRow(`SELECT MAX(started_at) FROM mate_routine_runs WHERE cost_known = 0 AND started_at >= ?`,
			routineStoreTime(since)).Scan(&unknown); err != nil {
			return fmt.Errorf("read routine spend: %w", err)
		}
		out.UnknownAt = routineTimePtr(unknown)
		return nil
	})
	return out, err
}

const mateRoutineColumns = `id, name, cadence, daily_time, focus, enabled, conversation_id, created_at, updated_at,
	last_run_at, last_outcome, last_note, last_finding_at, last_skip_at, schedule_changed_at`

func routineStoreTime(t time.Time) int64 { return t.UTC().UnixNano() }

func routineFromStoreTime(n int64) time.Time { return time.Unix(0, n).UTC() }

func routineTimePtr(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := routineFromStoreTime(n.Int64)
	return &t
}

func scanMateRoutine(row interface{ Scan(...any) error }) (mateRoutine, error) {
	var (
		r                    mateRoutine
		daily, outcome, note sql.NullString
		enabled              int
		created, updated     int64
		lastRun, lastFinding sql.NullInt64
		lastSkip             sql.NullInt64
		scheduleChanged      int64
	)
	if err := row.Scan(&r.ID, &r.Name, &r.Cadence, &daily, &r.Focus, &enabled, &r.ConversationID, &created, &updated,
		&lastRun, &outcome, &note, &lastFinding, &lastSkip, &scheduleChanged); err != nil {
		return mateRoutine{}, err
	}
	r.DailyTime, r.LastOutcome, r.LastNote = daily.String, outcome.String, note.String
	r.Enabled = enabled != 0
	r.CreatedAt, r.UpdatedAt = routineFromStoreTime(created), routineFromStoreTime(updated)
	r.LastRunAt, r.LastFindingAt, r.LastSkipAt = routineTimePtr(lastRun), routineTimePtr(lastFinding), routineTimePtr(lastSkip)
	r.ScheduleChangedAt = routineFromStoreTime(scheduleChanged)
	return r, nil
}

func insertMateRoutineTx(x alarmSQLExecer, r mateRoutine) error {
	var daily any
	if r.DailyTime != "" {
		daily = r.DailyTime
	}
	if r.ScheduleChangedAt.IsZero() {
		r.ScheduleChangedAt = r.CreatedAt
	}
	if _, err := x.Exec(`INSERT INTO mate_routines (id, name, cadence, daily_time, focus, enabled, conversation_id, created_at, updated_at, schedule_changed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Name, r.Cadence, daily, r.Focus, boolToInt(r.Enabled), r.ConversationID,
		routineStoreTime(r.CreatedAt), routineStoreTime(r.UpdatedAt), routineStoreTime(r.ScheduleChangedAt)); err != nil {
		return fmt.Errorf("insert routine %q: %w", r.Name, err)
	}
	return nil
}

// updateMateRoutineTx writes the definition columns and updated_at, and
// nothing the scheduler owns.
func updateMateRoutineTx(x alarmSQLExecer, r mateRoutine) error {
	var daily any
	if r.DailyTime != "" {
		daily = r.DailyTime
	}
	res, err := x.Exec(`UPDATE mate_routines SET name = ?, cadence = ?, daily_time = ?, focus = ?, enabled = ?, updated_at = ?, schedule_changed_at = ? WHERE id = ?`,
		r.Name, r.Cadence, daily, r.Focus, boolToInt(r.Enabled), routineStoreTime(r.UpdatedAt), routineStoreTime(r.ScheduleChangedAt), r.ID)
	if err != nil {
		return fmt.Errorf("update routine %q: %w", r.Name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errMateRoutineNotFound
	}
	return nil
}

func deleteMateRoutineTx(x alarmSQLExecer, id string) error {
	res, err := x.Exec(`DELETE FROM mate_routines WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete routine: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errMateRoutineNotFound
	}
	return nil
}

// recordMateRoutineRunTx leaves what a due run came to. A finding sets
// last_finding_at; a run without one keeps the previous value.
func recordMateRoutineRunTx(x alarmSQLExecer, id string, at time.Time, outcome, note string, finding bool) error {
	res, err := x.Exec(`UPDATE mate_routines SET last_run_at = ?, last_outcome = ?, last_note = ?,
		last_finding_at = CASE WHEN ? THEN ? ELSE last_finding_at END WHERE id = ?`,
		routineStoreTime(at), outcome, note, finding, routineStoreTime(at), id)
	if err != nil {
		return fmt.Errorf("record routine run: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errMateRoutineNotFound
	}
	return nil
}

// recordMateRoutineSkipTx leaves a note that does not count as a run: the
// routine stays owed, and last_run_at is not touched.
func recordMateRoutineSkipTx(x alarmSQLExecer, id string, at time.Time, note string) error {
	res, err := x.Exec(`UPDATE mate_routines SET last_skip_at = ?, last_outcome = ?, last_note = ? WHERE id = ?`,
		routineStoreTime(at), routineOutcomeSkipped, note, id)
	if err != nil {
		return fmt.Errorf("record routine skip: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errMateRoutineNotFound
	}
	return nil
}

func mateRoutineByIDQ(q sqlQueryer, id string) (mateRoutine, error) {
	r, err := scanMateRoutine(q.QueryRow(`SELECT `+mateRoutineColumns+` FROM mate_routines WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return mateRoutine{}, errMateRoutineNotFound
	}
	return r, err
}

// listMateRoutinesQ returns every routine in creation order, the order the
// scheduler runs them in.
func listMateRoutinesQ(q sqlQueryer) ([]mateRoutine, error) {
	rows, err := q.Query(`SELECT ` + mateRoutineColumns + ` FROM mate_routines`)
	if err != nil {
		return nil, fmt.Errorf("read routines: %w", err)
	}
	defer rows.Close()
	var out []mateRoutine
	for rows.Next() {
		r, err := scanMateRoutine(rows)
		if err != nil {
			return nil, fmt.Errorf("read routines: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read routines: %w", err)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func countMateRoutinesQ(q sqlQueryer) (int, error) {
	var n int
	if err := q.QueryRow(`SELECT COUNT(*) FROM mate_routines`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count routines: %w", err)
	}
	return n, nil
}
