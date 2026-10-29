package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

/*
Watches in helmcentral.sqlite (ADR 0227).

Two tables beside the alarm rules. watches holds the definitions: a scheduled
watch (a cron schedule, the report sections it covers, the thresholds that
turn a figure into a finding, and what the operator wants looked out for) and
a one-off watch (a timed sample of a few readings, started from a chat). Its
scheduler columns (last_run_at, last_skip_at) are written without touching
updated_at, so a run never makes a waiting proposal stale.

watch_runs holds one row per run: the deterministic report, the findings the
rules reached, Mate's review, what the review cost, and why there was no
review when there was none.
*/

const (
	watchKindScheduled = "scheduled"
	watchKindOneOff    = "one_off"

	watchOutcomeRan     = "ran"
	watchOutcomeSkipped = "skipped"

	watchReportKindSections = "sections"
	watchReportKindSamples  = "samples"

	watchByRules = "rules"
	watchByMate  = "mate"

	// maxScheduledWatches keeps what Mate reviews each hour in reach of one
	// operator's reading.
	maxScheduledWatches = 20

	maxWatchNameChars       = 60
	maxWatchLookOutForChars = 500

	// watchRunRetention is how long a run without a finding is kept.
	watchRunRetention = 365 * 24 * time.Hour
)

// The report sections a scheduled watch can cover.
const (
	watchSectionPower       = "power"
	watchSectionAnchor      = "anchor"
	watchSectionAlarms      = "alarms"
	watchSectionTanks       = "tanks"
	watchSectionWeather     = "weather"
	watchSectionInstruments = "instruments"
)

var watchSectionOrder = []string{
	watchSectionPower, watchSectionAnchor, watchSectionAlarms,
	watchSectionTanks, watchSectionWeather, watchSectionInstruments,
}

var errWatchNotFound = errors.New("no such watch")
var errWatchRunNotFound = errors.New("no such run")

type savedWatch struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Schedule is the cron string of a scheduled watch.
	Schedule string `json:"schedule,omitempty"`
	// DurationMinutes, StartedAt, EndsAt and ConversationID describe a one-off
	// watch: how long it samples, when, and the chat that asked.
	DurationMinutes int        `json:"duration_minutes,omitempty"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	EndsAt          *time.Time `json:"ends_at,omitempty"`
	ConversationID  string     `json:"conversation_id,omitempty"`

	Sections   []string        `json:"sections"`
	Thresholds watchThresholds `json:"thresholds"`
	LookOutFor string          `json:"look_out_for"`
	Enabled    bool            `json:"enabled"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
	LastRunAt  *time.Time      `json:"last_run_at,omitempty"`
	// LastSkipAt is when a "no boat position" note was last left. A skip for
	// that reason is not a run: the run is still owed when the position
	// returns, so it is kept apart from LastRunAt.
	LastSkipAt *time.Time `json:"last_skip_at,omitempty"`
	// ScheduleChangedAt is when the watch was made or its schedule last
	// changed. A run is owed only for a cron instant after it.
	ScheduleChangedAt time.Time `json:"schedule_changed_at"`

	// Config belongs to a one-off watch: what it samples. The samples taken so
	// far are kept apart (watch_samples), saved so a restart can resume it.
	Config json.RawMessage `json:"-"`

	// Notify lists the channels a report is sent to (ntfy, smtp, webhook,
	// webpush; never signalk) and Send says when: watchSendFindings or
	// watchSendEvery.
	Notify []string `json:"notify"`
	Send   string   `json:"send"`

	// ReportedAt is when a one-off watch's end was recorded, just before its
	// report went to the chat. A watch with one is never reported again.
	ReportedAt *time.Time `json:"-"`
}

type watchFinding struct {
	Section string `json:"section"`
	Rule    string `json:"rule"`
	Text    string `json:"text"`
	By      string `json:"by"`
	// Key names the issue a finding Mate added is about, in a few lower_snake
	// words, so the same issue is the same finding from one run to the next
	// whatever Mate's wording. Empty on a finding the rules reached.
	Key string `json:"key,omitempty"`
	// ChartProblem says why a chart that was asked for could not be drawn.
	ChartProblem string `json:"chart_problem,omitempty"`
	// chart is what the finding wants drawn; it is resolved into a watchChart
	// before the run is saved and never stored.
	chart *watchChartSpec
}

type watchRun struct {
	ID         string          `json:"id"`
	WatchID    string          `json:"watch_id"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt time.Time       `json:"finished_at"`
	Outcome    string          `json:"outcome"`
	SkipReason string          `json:"skip_reason,omitempty"`
	ReportKind string          `json:"report_kind"`
	Report     json.RawMessage `json:"report,omitempty"`
	Findings   []watchFinding  `json:"findings"`
	// Charts are the charts of the findings, as data (ADR 0228). Each names
	// its finding by index. The page draws them and the notifications render
	// a PNG from them.
	Charts []watchChart `json:"charts"`
	// NotifyNotes say where a delivery was not as it was meant to be, such as
	// a phone message sent without its chart.
	NotifyNotes []string `json:"notify_notes"`
	// Review is Mate's reading of the report. ReviewProblem says why there
	// is none. Exactly one of them is set on a run that produced a report.
	Review        *string  `json:"review"`
	ReviewProblem *string  `json:"review_problem"`
	CostUSD       *float64 `json:"cost_usd"`
	CostKnown     bool     `json:"cost_known"`
	Model         string   `json:"model,omitempty"`
	// BoatTime is when the run started on the boat's clock as it was then, and
	// Timezone is that clock's offset ("UTC+12"). With no boat position they
	// are the UTC time, said to be UTC, and "": the page never shows a time on
	// the device it is read on.
	BoatTime string `json:"boat_time"`
	Timezone string `json:"timezone"`
	// Notified lists the channels a notification of this run reached.
	// NotifyPending lists the ones still being retried, with the reason the
	// last attempt failed. Neither is stored in the run: the first is a
	// column of its own, the second is read from the retry queue.
	Notified      []string             `json:"notified"`
	NotifyPending []watchPendingNotice `json:"notify_pending,omitempty"`
	// NotSent says why a report that was due went nowhere, such as no channel
	// being switched on. Empty when it was handed to a channel.
	NotSent string `json:"not_sent,omitempty"`
	// ToldKeys are the keys of this run's findings the operator is known to
	// have been told about: those the previous run had already told them, and
	// all of them once a channel accepts this run's report.
	ToldKeys []string `json:"-"`
}

func dropLegacyRoutineTables(db *sql.DB) error {
	for _, t := range []string{"mate_routines", "mate_routine_runs"} {
		if _, err := db.Exec(`DROP TABLE IF EXISTS ` + t); err != nil {
			return fmt.Errorf("drop %s: %w", t, err)
		}
	}
	return nil
}

// addWatchColumnIfMissing brings a watches table made by an earlier build of
// this branch up to date.
func addWatchColumnIfMissing(db *sql.DB, column, decl string) error {
	return addColumnIfMissing(db, "watches", column, decl)
}

func addColumnIfMissing(db *sql.DB, table, column, decl string) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('`+table+`') WHERE name = ?`, column).Scan(&n); err != nil {
		return fmt.Errorf("read the %s columns: %w", table, err)
	}
	if n > 0 {
		return nil
	}
	if _, err := db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + decl); err != nil {
		return fmt.Errorf("add %s to %s: %w", column, table, err)
	}
	return nil
}

func createWatchesSchema(db *sql.DB) error {
	// The first draft of this feature called them routines and was never
	// released; its tables go.
	if err := dropLegacyRoutineTables(db); err != nil {
		return err
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS watches (
			id                  TEXT PRIMARY KEY,
			name                TEXT NOT NULL,
			kind                TEXT NOT NULL,
			schedule            TEXT NOT NULL DEFAULT '',
			duration_minutes    INTEGER NOT NULL DEFAULT 0,
			started_at          INTEGER,
			ends_at             INTEGER,
			conversation_id     TEXT NOT NULL DEFAULT '',
			sections            TEXT NOT NULL DEFAULT '[]',
			thresholds          TEXT NOT NULL DEFAULT '{}',
			look_out_for        TEXT NOT NULL DEFAULT '',
			enabled             INTEGER NOT NULL,
			created_at          INTEGER NOT NULL,
			updated_at          INTEGER NOT NULL,
			schedule_changed_at INTEGER NOT NULL,
			last_run_at         INTEGER,
			last_skip_at        INTEGER,
			config              TEXT NOT NULL DEFAULT '',
			reported_at         INTEGER,
			notify              TEXT NOT NULL DEFAULT '[]',
			send                TEXT NOT NULL DEFAULT 'findings'
		)`); err != nil {
		return fmt.Errorf("create watches table: %w", err)
	}
	for col, decl := range map[string]string{
		"reported_at": "INTEGER",
		"notify":      "TEXT NOT NULL DEFAULT '[]'",
		"send":        "TEXT NOT NULL DEFAULT 'findings'",
	} {
		if err := addWatchColumnIfMissing(db, col, decl); err != nil {
			return err
		}
	}
	// The samples of a running one-off watch, appended in chunks so a save
	// writes only what is new.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS watch_samples (
			watch_id TEXT NOT NULL,
			seq      INTEGER NOT NULL,
			samples  TEXT NOT NULL,
			PRIMARY KEY (watch_id, seq)
		)`); err != nil {
		return fmt.Errorf("create watch_samples table: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS watch_runs (
			id            TEXT PRIMARY KEY,
			watch_id      TEXT NOT NULL,
			started_at    INTEGER NOT NULL,
			finished_at   INTEGER NOT NULL,
			outcome       TEXT NOT NULL,
			skip_reason   TEXT NOT NULL DEFAULT '',
			report_kind   TEXT NOT NULL DEFAULT 'sections',
			report        TEXT NOT NULL DEFAULT '',
			findings      TEXT NOT NULL DEFAULT '[]',
			has_finding   INTEGER NOT NULL DEFAULT 0,
			review        TEXT,
			review_problem TEXT,
			cost_usd      REAL,
			cost_known    INTEGER NOT NULL DEFAULT 0,
			model         TEXT NOT NULL DEFAULT '',
			boat_time     TEXT NOT NULL DEFAULT '',
			timezone      TEXT NOT NULL DEFAULT '',
			notified      TEXT NOT NULL DEFAULT '[]',
			told_keys     TEXT NOT NULL DEFAULT '[]',
			not_sent      TEXT NOT NULL DEFAULT '',
			charts        TEXT NOT NULL DEFAULT '[]',
			notify_notes  TEXT NOT NULL DEFAULT '[]'
		)`); err != nil {
		return fmt.Errorf("create watch_runs table: %w", err)
	}
	for _, col := range []string{"boat_time", "timezone"} {
		if err := addColumnIfMissing(db, "watch_runs", col, "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	for col, decl := range map[string]string{
		"notified":     "TEXT NOT NULL DEFAULT '[]'",
		"told_keys":    "TEXT NOT NULL DEFAULT '[]'",
		"not_sent":     "TEXT NOT NULL DEFAULT ''",
		"charts":       "TEXT NOT NULL DEFAULT '[]'",
		"notify_notes": "TEXT NOT NULL DEFAULT '[]'",
	} {
		if err := addColumnIfMissing(db, "watch_runs", col, decl); err != nil {
			return err
		}
	}
	for _, idx := range []string{
		`CREATE INDEX IF NOT EXISTS watch_runs_watch ON watch_runs (watch_id, started_at)`,
		`CREATE INDEX IF NOT EXISTS watch_runs_started ON watch_runs (started_at)`,
	} {
		if _, err := db.Exec(idx); err != nil {
			return fmt.Errorf("index watch_runs table: %w", err)
		}
	}
	return nil
}

func watchStoreTime(t time.Time) int64 { return t.UTC().UnixNano() }

func watchFromStoreTime(n int64) time.Time { return time.Unix(0, n).UTC() }

func watchTimePtr(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := watchFromStoreTime(n.Int64)
	return &t
}

func nullableWatchTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return watchStoreTime(*t)
}

const savedWatchColumns = `id, name, kind, schedule, duration_minutes, started_at, ends_at, conversation_id, sections,
	thresholds, look_out_for, enabled, created_at, updated_at, schedule_changed_at, last_run_at, last_skip_at, config, reported_at,
	notify, send`

func scanSavedWatch(row interface{ Scan(...any) error }) (savedWatch, error) {
	var (
		w                          savedWatch
		started, ends              sql.NullInt64
		lastRun, lastSkip          sql.NullInt64
		sections, thresholds       string
		config                     string
		reported                   sql.NullInt64
		notify                     string
		enabled                    int
		created, updated, schedule int64
	)
	if err := row.Scan(&w.ID, &w.Name, &w.Kind, &w.Schedule, &w.DurationMinutes, &started, &ends, &w.ConversationID,
		&sections, &thresholds, &w.LookOutFor, &enabled, &created, &updated, &schedule, &lastRun, &lastSkip, &config, &reported,
		&notify, &w.Send); err != nil {
		return savedWatch{}, err
	}
	if err := json.Unmarshal([]byte(notify), &w.Notify); err != nil {
		return savedWatch{}, fmt.Errorf("watch %q: the saved notification channels are unreadable: %w", w.Name, err)
	}
	if w.Notify == nil {
		w.Notify = []string{}
	}
	if err := json.Unmarshal([]byte(sections), &w.Sections); err != nil {
		return savedWatch{}, fmt.Errorf("watch %q: the saved sections are unreadable: %w", w.Name, err)
	}
	if w.Sections == nil {
		w.Sections = []string{}
	}
	if err := json.Unmarshal([]byte(thresholds), &w.Thresholds); err != nil {
		return savedWatch{}, fmt.Errorf("watch %q: the saved thresholds are unreadable: %w", w.Name, err)
	}
	w.Enabled = enabled != 0
	w.StartedAt, w.EndsAt = watchTimePtr(started), watchTimePtr(ends)
	w.LastRunAt, w.LastSkipAt = watchTimePtr(lastRun), watchTimePtr(lastSkip)
	w.CreatedAt, w.UpdatedAt, w.ScheduleChangedAt = watchFromStoreTime(created), watchFromStoreTime(updated), watchFromStoreTime(schedule)
	if config != "" {
		w.Config = json.RawMessage(config)
	}
	w.ReportedAt = watchTimePtr(reported)
	return w, nil
}

func insertSavedWatchTx(x alarmSQLExecer, w savedWatch) error {
	if w.ScheduleChangedAt.IsZero() {
		w.ScheduleChangedAt = w.CreatedAt
	}
	sections, err := json.Marshal(nonNilStrings(w.Sections))
	if err != nil {
		return err
	}
	thresholds, err := json.Marshal(w.Thresholds)
	if err != nil {
		return err
	}
	notify, err := json.Marshal(nonNilStrings(w.Notify))
	if err != nil {
		return err
	}
	if w.Send == "" {
		w.Send = watchSendFindings
	}
	if _, err := x.Exec(`INSERT INTO watches (`+savedWatchColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.Name, w.Kind, w.Schedule, w.DurationMinutes, nullableWatchTime(w.StartedAt), nullableWatchTime(w.EndsAt), w.ConversationID,
		string(sections), string(thresholds), w.LookOutFor, boolToInt(w.Enabled),
		watchStoreTime(w.CreatedAt), watchStoreTime(w.UpdatedAt), watchStoreTime(w.ScheduleChangedAt),
		nullableWatchTime(w.LastRunAt), nullableWatchTime(w.LastSkipAt), string(w.Config), nullableWatchTime(w.ReportedAt),
		string(notify), w.Send); err != nil {
		return fmt.Errorf("insert watch %q: %w", w.Name, err)
	}
	return nil
}

// updateSavedWatchTx writes the definition columns and updated_at, and
// nothing the scheduler owns.
func updateSavedWatchTx(x alarmSQLExecer, w savedWatch) error {
	sections, err := json.Marshal(nonNilStrings(w.Sections))
	if err != nil {
		return err
	}
	thresholds, err := json.Marshal(w.Thresholds)
	if err != nil {
		return err
	}
	notify, err := json.Marshal(nonNilStrings(w.Notify))
	if err != nil {
		return err
	}
	res, err := x.Exec(`UPDATE watches SET name = ?, schedule = ?, sections = ?, thresholds = ?, look_out_for = ?, enabled = ?,
		notify = ?, send = ?, updated_at = ?, schedule_changed_at = ? WHERE id = ?`,
		w.Name, w.Schedule, string(sections), string(thresholds), w.LookOutFor, boolToInt(w.Enabled),
		string(notify), w.Send, watchStoreTime(w.UpdatedAt), watchStoreTime(w.ScheduleChangedAt), w.ID)
	if err != nil {
		return fmt.Errorf("update watch %q: %w", w.Name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errWatchNotFound
	}
	return nil
}

// deleteSavedWatchTx removes a watch and, with it, its runs.
func deleteSavedWatchTx(x alarmSQLExecer, id string) error {
	res, err := x.Exec(`DELETE FROM watches WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete watch: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errWatchNotFound
	}
	if _, err := x.Exec(`DELETE FROM watch_runs WHERE watch_id = ?`, id); err != nil {
		return fmt.Errorf("delete the runs of watch: %w", err)
	}
	if _, err := x.Exec(`DELETE FROM watch_samples WHERE watch_id = ?`, id); err != nil {
		return fmt.Errorf("delete the samples of watch: %w", err)
	}
	return nil
}

func savedWatchByIDQ(q sqlQueryer, id string) (savedWatch, error) {
	w, err := scanSavedWatch(q.QueryRow(`SELECT `+savedWatchColumns+` FROM watches WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return savedWatch{}, errWatchNotFound
	}
	return w, err
}

// listSavedWatchesQ returns every watch in creation order, the order the
// scheduler runs them in.
func listSavedWatchesQ(q sqlQueryer) ([]savedWatch, error) {
	rows, err := q.Query(`SELECT ` + savedWatchColumns + ` FROM watches`)
	if err != nil {
		return nil, fmt.Errorf("read watches: %w", err)
	}
	defer rows.Close()
	var out []savedWatch
	for rows.Next() {
		w, err := scanSavedWatch(rows)
		if err != nil {
			return nil, fmt.Errorf("read watches: %w", err)
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read watches: %w", err)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func countScheduledWatchesQ(q sqlQueryer) (int, error) {
	var n int
	if err := q.QueryRow(`SELECT COUNT(*) FROM watches WHERE kind = ?`, watchKindScheduled).Scan(&n); err != nil {
		return 0, fmt.Errorf("count watches: %w", err)
	}
	return n, nil
}

// ── runs ────────────────────────────────────────────────────────────────

const watchRunColumns = `id, watch_id, started_at, finished_at, outcome, skip_reason, report_kind, report, findings,
	review, review_problem, cost_usd, cost_known, model, boat_time, timezone, notified, told_keys, not_sent, charts, notify_notes`

func scanWatchRun(row interface{ Scan(...any) error }) (watchRun, error) {
	var (
		r                     watchRun
		started, finished     int64
		report, findings      string
		review, reviewProblem sql.NullString
		cost                  sql.NullFloat64
		known                 int
		notified, told        string
		charts, notes         string
	)
	if err := row.Scan(&r.ID, &r.WatchID, &started, &finished, &r.Outcome, &r.SkipReason, &r.ReportKind, &report, &findings,
		&review, &reviewProblem, &cost, &known, &r.Model, &r.BoatTime, &r.Timezone, &notified, &told, &r.NotSent, &charts, &notes); err != nil {
		return watchRun{}, err
	}
	if err := json.Unmarshal([]byte(notified), &r.Notified); err != nil {
		return watchRun{}, fmt.Errorf("run %s: the saved notifications are unreadable: %w", r.ID, err)
	}
	if r.Notified == nil {
		r.Notified = []string{}
	}
	if err := json.Unmarshal([]byte(charts), &r.Charts); err != nil {
		return watchRun{}, fmt.Errorf("run %s: the saved charts are unreadable: %w", r.ID, err)
	}
	if r.Charts == nil {
		r.Charts = []watchChart{}
	}
	if err := json.Unmarshal([]byte(notes), &r.NotifyNotes); err != nil {
		return watchRun{}, fmt.Errorf("run %s: the saved delivery notes are unreadable: %w", r.ID, err)
	}
	if r.NotifyNotes == nil {
		r.NotifyNotes = []string{}
	}
	if err := json.Unmarshal([]byte(told), &r.ToldKeys); err != nil {
		return watchRun{}, fmt.Errorf("run %s: the saved told findings are unreadable: %w", r.ID, err)
	}
	r.StartedAt, r.FinishedAt = watchFromStoreTime(started), watchFromStoreTime(finished)
	if report != "" {
		r.Report = json.RawMessage(report)
	}
	if err := json.Unmarshal([]byte(findings), &r.Findings); err != nil {
		return watchRun{}, fmt.Errorf("run %s: the saved findings are unreadable: %w", r.ID, err)
	}
	if r.Findings == nil {
		r.Findings = []watchFinding{}
	}
	if review.Valid {
		r.Review = &review.String
	}
	if reviewProblem.Valid {
		r.ReviewProblem = &reviewProblem.String
	}
	if cost.Valid {
		r.CostUSD = &cost.Float64
	}
	r.CostKnown = known != 0
	if r.BoatTime == "" {
		// A run saved with no zone: say it is UTC rather than leave it blank.
		r.BoatTime = watchBoatClock(r.StartedAt, nil)
	}
	return r, nil
}

// insertWatchRunTx saves a run. A run that happened moves the watch's
// last_run_at; a skip for want of a boat position moves last_skip_at and
// leaves the run owed.
func insertWatchRunTx(x alarmSQLExecer, r watchRun, countsAsRun bool) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.ReportKind == "" {
		r.ReportKind = watchReportKindSections
	}
	if r.BoatTime == "" {
		r.BoatTime = watchBoatClock(r.StartedAt, nil)
	}
	findings, err := json.Marshal(nonNilFindings(r.Findings))
	if err != nil {
		return err
	}
	charts := r.Charts
	if charts == nil {
		charts = []watchChart{}
	}
	chartsJSON, err := json.Marshal(charts)
	if err != nil {
		return err
	}
	if _, err := x.Exec(`INSERT INTO watch_runs (id, watch_id, started_at, finished_at, outcome, skip_reason, report_kind, report,
		findings, has_finding, review, review_problem, cost_usd, cost_known, model, boat_time, timezone, charts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.WatchID, watchStoreTime(r.StartedAt), watchStoreTime(r.FinishedAt), r.Outcome, r.SkipReason, r.ReportKind, string(r.Report),
		string(findings), boolToInt(len(r.Findings) > 0), r.Review, r.ReviewProblem, r.CostUSD, boolToInt(r.CostKnown), r.Model, r.BoatTime, r.Timezone, string(chartsJSON)); err != nil {
		return fmt.Errorf("record watch run: %w", err)
	}
	column := "last_skip_at"
	if countsAsRun {
		column = "last_run_at"
	}
	res, err := x.Exec(`UPDATE watches SET `+column+` = ? WHERE id = ?`, watchStoreTime(r.StartedAt), r.WatchID)
	if err != nil {
		return fmt.Errorf("record watch run: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errWatchNotFound
	}
	return nil
}

func nonNilFindings(f []watchFinding) []watchFinding {
	if f == nil {
		return []watchFinding{}
	}
	return f
}

// watchRunsQ pages a watch's runs, newest first.
// watchRunCursor is where the next page of runs starts: the runs older than
// this one. A page is found by what it follows, never by how many came before
// it, so a run made between two page loads does not repeat or drop one.
type watchRunCursor struct {
	StartedAt time.Time
	ID        string
}

// parseWatchRunCursor reads "<started_at RFC 3339>,<run id>", the last run of
// the page already shown.
func parseWatchRunCursor(v string) (*watchRunCursor, error) {
	at, id, ok := strings.Cut(v, ",")
	if !ok || id == "" {
		return nil, fieldErr("before", "send the started_at and id of the last run shown, as started_at,id")
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return nil, fieldErr("before", "send the started_at and id of the last run shown, as started_at,id")
	}
	return &watchRunCursor{StartedAt: t, ID: id}, nil
}

// watchRunsQ pages a watch's runs, newest first, those older than before (all
// of them when it is nil).
func watchRunsQ(q sqlQueryer, watchID string, limit int, before *watchRunCursor) ([]watchRun, error) {
	query := `SELECT ` + watchRunColumns + ` FROM watch_runs WHERE watch_id = ?`
	args := []any{watchID}
	if before != nil {
		ns := watchStoreTime(before.StartedAt)
		query += ` AND (started_at < ? OR (started_at = ? AND id < ?))`
		args = append(args, ns, ns, before.ID)
	}
	query += ` ORDER BY started_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("read watch runs: %w", err)
	}
	defer rows.Close()
	out := []watchRun{}
	for rows.Next() {
		r, err := scanWatchRun(rows)
		if err != nil {
			return nil, fmt.Errorf("read watch runs: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func watchRunByIDQ(q sqlQueryer, watchID, runID string) (watchRun, error) {
	r, err := scanWatchRun(q.QueryRow(`SELECT `+watchRunColumns+` FROM watch_runs WHERE watch_id = ? AND id = ?`, watchID, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return watchRun{}, errWatchRunNotFound
	}
	return r, err
}

// watchRunAnyByIDQ finds a run by its id alone, for "Ask Mate about this run".
func watchRunAnyByIDQ(q sqlQueryer, runID string) (watchRun, error) {
	r, err := scanWatchRun(q.QueryRow(`SELECT `+watchRunColumns+` FROM watch_runs WHERE id = ?`, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return watchRun{}, errWatchRunNotFound
	}
	return r, err
}

// lastRanRunQ is the watch's latest run that produced a report, the one the
// next report is compared with.
func lastRanRunQ(q sqlQueryer, watchID string) (watchRun, bool, error) {
	r, err := scanWatchRun(q.QueryRow(`SELECT `+watchRunColumns+` FROM watch_runs WHERE watch_id = ? AND outcome = ?
		ORDER BY started_at DESC, id DESC LIMIT 1`, watchID, watchOutcomeRan))
	if errors.Is(err, sql.ErrNoRows) {
		return watchRun{}, false, nil
	}
	if err != nil {
		return watchRun{}, false, err
	}
	return r, true, nil
}

func deleteWatchRunTx(x alarmSQLExecer, watchID, runID string) error {
	res, err := x.Exec(`DELETE FROM watch_runs WHERE watch_id = ? AND id = ?`, watchID, runID)
	if err != nil {
		return fmt.Errorf("delete watch run: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errWatchRunNotFound
	}
	return nil
}

// watchRunSummary is the one line the index shows for a watch's latest run.
type watchRunSummary struct {
	ID            string    `json:"id"`
	StartedAt     time.Time `json:"started_at"`
	Outcome       string    `json:"outcome"`
	SkipReason    string    `json:"skip_reason,omitempty"`
	FindingCount  int       `json:"finding_count"`
	ReviewProblem *string   `json:"review_problem"`
}

func lastRunSummaryQ(q sqlQueryer, watchID string) (*watchRunSummary, error) {
	var (
		s        watchRunSummary
		started  int64
		problem  sql.NullString
		count    int
		findings string
	)
	err := q.QueryRow(`SELECT id, started_at, outcome, skip_reason, review_problem, findings FROM watch_runs
		WHERE watch_id = ? ORDER BY started_at DESC, id DESC LIMIT 1`, watchID).
		Scan(&s.ID, &started, &s.Outcome, &s.SkipReason, &problem, &findings)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read last watch run: %w", err)
	}
	var list []watchFinding
	if err := json.Unmarshal([]byte(findings), &list); err != nil {
		return nil, fmt.Errorf("read last watch run: findings: %w", err)
	}
	count = len(list)
	s.FindingCount = count
	s.StartedAt = watchFromStoreTime(started)
	if problem.Valid {
		s.ReviewProblem = &problem.String
	}
	return &s, nil
}

// watchCost is what runs cost in a window. UnknownRuns counts the runs whose
// cost could not be read; they are shown as "cost not known" and nothing is
// paused for them.
type watchCost struct {
	USD         float64 `json:"usd"`
	UnknownRuns int     `json:"unknown_runs"`
}

func watchCostSinceQ(q sqlQueryer, since time.Time) (watchCost, error) {
	var c watchCost
	// A run that asked no model has a known cost of nothing; cost_known is 0
	// only when the model was asked and the charge could not be read.
	if err := q.QueryRow(`SELECT COALESCE(SUM(cost_usd), 0), COALESCE(SUM(CASE WHEN cost_known = 0 THEN 1 ELSE 0 END), 0)
		FROM watch_runs WHERE started_at >= ?`, watchStoreTime(since)).Scan(&c.USD, &c.UnknownRuns); err != nil {
		return c, fmt.Errorf("read watch cost: %w", err)
	}
	return c, nil
}

// watchStorageBytesQ is the space the watch tables use, counted from the text
// they hold. It is an estimate, close enough to tell an operator whether
// history is growing large.
func watchStorageBytesQ(q sqlQueryer) (int64, error) {
	var runs, defs, samples int64
	if err := q.QueryRow(`SELECT COALESCE(SUM(length(report) + length(findings) + length(skip_reason) + length(report_kind) + length(model)
		+ COALESCE(length(review), 0) + COALESCE(length(review_problem), 0) + length(charts)), 0) FROM watch_runs`).Scan(&runs); err != nil {
		return 0, fmt.Errorf("measure watch history: %w", err)
	}
	if err := q.QueryRow(`SELECT COALESCE(SUM(length(name) + length(schedule) + length(sections) + length(thresholds) + length(look_out_for)
		+ length(config)), 0) FROM watches`).Scan(&defs); err != nil {
		return 0, fmt.Errorf("measure watches: %w", err)
	}
	if err := q.QueryRow(`SELECT COALESCE(SUM(length(samples)), 0) FROM watch_samples`).Scan(&samples); err != nil {
		return 0, fmt.Errorf("measure watch samples: %w", err)
	}
	return runs + defs + samples, nil
}

// pruneWatchRunsTx drops runs older than cutoff that found nothing, and
// one-off watches left with no runs. A run with findings stays for good. It
// returns how many runs went.
func pruneWatchRunsTx(x alarmSQLExecer, cutoff time.Time) (int64, error) {
	res, err := x.Exec(`DELETE FROM watch_runs WHERE started_at < ? AND has_finding = 0`, watchStoreTime(cutoff))
	if err != nil {
		return 0, fmt.Errorf("prune watch runs: %w", err)
	}
	n, _ := res.RowsAffected()
	if _, err := x.Exec(`DELETE FROM watches WHERE kind = ? AND enabled = 0 AND created_at < ?
		AND NOT EXISTS (SELECT 1 FROM watch_runs WHERE watch_runs.watch_id = watches.id)`, watchKindOneOff, watchStoreTime(cutoff)); err != nil {
		return n, fmt.Errorf("prune one-off watches: %w", err)
	}
	return n, nil
}

// addWatchRunNotifiedTx records that a channel accepted a run's report. The
// operator is then known to have been told every finding the run carries, so
// those are marked told, and any earlier "not sent" note no longer applies.
func addWatchRunNotifiedTx(x alarmSQLExecer, watchID, runID, channel string) error {
	var raw, findingsRaw string
	if err := x.QueryRow(`SELECT notified, findings FROM watch_runs WHERE watch_id = ? AND id = ?`, watchID, runID).Scan(&raw, &findingsRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errWatchRunNotFound
		}
		return fmt.Errorf("read notified channels: %w", err)
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return fmt.Errorf("the saved notifications of run %s are unreadable: %w", runID, err)
	}
	var findings []watchFinding
	if err := json.Unmarshal([]byte(findingsRaw), &findings); err != nil {
		return fmt.Errorf("the saved findings of run %s are unreadable: %w", runID, err)
	}
	if !containsString(list, channel) {
		list = append(list, channel)
	}
	out, err := json.Marshal(list)
	if err != nil {
		return err
	}
	told, err := json.Marshal(watchFindingKeys(findings))
	if err != nil {
		return err
	}
	if _, err := x.Exec(`UPDATE watch_runs SET notified = ?, told_keys = ?, not_sent = '' WHERE watch_id = ? AND id = ?`,
		string(out), string(told), watchID, runID); err != nil {
		return fmt.Errorf("record notified channel: %w", err)
	}
	return nil
}

// setWatchRunToldTx sets the keys of a run's findings the operator is known to
// have been told about.
func setWatchRunToldTx(x alarmSQLExecer, watchID, runID string, keys []string) error {
	raw, err := json.Marshal(nonNilStrings(keys))
	if err != nil {
		return err
	}
	if _, err := x.Exec(`UPDATE watch_runs SET told_keys = ? WHERE watch_id = ? AND id = ?`, string(raw), watchID, runID); err != nil {
		return fmt.Errorf("remember told findings: %w", err)
	}
	return nil
}

// setWatchRunNotSentTx notes why a report that was due went to no channel.
func setWatchRunNotSentTx(x alarmSQLExecer, watchID, runID, reason string) error {
	res, err := x.Exec(`UPDATE watch_runs SET not_sent = ? WHERE watch_id = ? AND id = ?`, reason, watchID, runID)
	if err != nil {
		return fmt.Errorf("record why a report was not sent: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errWatchRunNotFound
	}
	return nil
}

// lastRanRunBeforeQ is the run of a watch that produced a report just before
// the given one, the one a new report is compared with.
func lastRanRunBeforeQ(q sqlQueryer, watchID string, startedAt time.Time, runID string) (watchRun, bool, error) {
	ns := watchStoreTime(startedAt)
	r, err := scanWatchRun(q.QueryRow(`SELECT `+watchRunColumns+` FROM watch_runs WHERE watch_id = ? AND outcome = ?
		AND (started_at < ? OR (started_at = ? AND id < ?)) ORDER BY started_at DESC, id DESC LIMIT 1`,
		watchID, watchOutcomeRan, ns, ns, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return watchRun{}, false, nil
	}
	if err != nil {
		return watchRun{}, false, err
	}
	return r, true, nil
}

// addWatchRunNotifyNoteTx notes on a run where a delivery was not as meant, such
// as a message sent without its chart. The same note is not added twice.
func addWatchRunNotifyNoteTx(x alarmSQLExecer, watchID, runID, note string) error {
	var raw string
	if err := x.QueryRow(`SELECT notify_notes FROM watch_runs WHERE watch_id = ? AND id = ?`, watchID, runID).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errWatchRunNotFound
		}
		return fmt.Errorf("read the run's delivery notes: %w", err)
	}
	var notes []string
	if err := json.Unmarshal([]byte(raw), &notes); err != nil {
		return fmt.Errorf("run %s: the saved delivery notes are unreadable: %w", runID, err)
	}
	if containsString(notes, note) {
		return nil
	}
	out, err := json.Marshal(append(notes, note))
	if err != nil {
		return err
	}
	if _, err := x.Exec(`UPDATE watch_runs SET notify_notes = ? WHERE watch_id = ? AND id = ?`, string(out), watchID, runID); err != nil {
		return fmt.Errorf("record the run's delivery note: %w", err)
	}
	return nil
}
