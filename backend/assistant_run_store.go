package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
)

// The stored record of every Mate run (ADR 0240): how long each round took,
// what it cost, how the budget ended it, and every tool call with its full
// arguments and result. The run owns its calls. An answer's calls are found by
// joining on assistant_runs.answer_message_id, so a run that gave no answer, and
// so has no message, is kept in the same way.

const (
	// assistantRunRetention is how long an answered, unrated run is kept: the
	// details are for looking at an answer while it is fresh. A run behind a
	// rated answer is kept for as long as the rating stands.
	assistantRunRetention = 7 * 24 * time.Hour
	// assistantFailedRunRetention is how long a run that gave no answer is kept.
	// It is the only record of what went wrong and the operator sees it in the
	// conversation, so it outlives the answered runs; a month is long enough to
	// notice a pattern across several outings.
	assistantFailedRunRetention = 30 * 24 * time.Hour
	// assistantRunPruneEvery is how often the prune runs after startup.
	assistantRunPruneEvery = 6 * time.Hour
	// assistantToolCallMaxBytes bounds each of a stored call's args and result.
	assistantToolCallMaxBytes = 64 * 1024
)

func createAssistantRunsSchema(db *sql.DB) error {
	for _, stmt := range []struct{ what, sql string }{
		{"assistant_runs table", `
		CREATE TABLE IF NOT EXISTS assistant_runs (
			id                TEXT PRIMARY KEY,
			conversation_id   TEXT NOT NULL,
			after_seq         INTEGER NOT NULL,
			answer_message_id TEXT,
			model             TEXT NOT NULL DEFAULT '',
			started_at        INTEGER NOT NULL,
			duration_ms       INTEGER NOT NULL,
			stop_reason       TEXT NOT NULL,
			error             TEXT NOT NULL DEFAULT '',
			budget            TEXT NOT NULL
		)`},
		{"assistant_runs conversation index", `CREATE INDEX IF NOT EXISTS assistant_runs_conversation ON assistant_runs (conversation_id, started_at)`},
		{"assistant_runs answer index", `CREATE INDEX IF NOT EXISTS assistant_runs_answer ON assistant_runs (answer_message_id)`},
		{"assistant_runs prune index", `CREATE INDEX IF NOT EXISTS assistant_runs_started ON assistant_runs (started_at)`},
		{"assistant_run_rounds table", `
		CREATE TABLE IF NOT EXISTS assistant_run_rounds (
			run_id            TEXT NOT NULL REFERENCES assistant_runs(id) ON DELETE CASCADE,
			idx               INTEGER NOT NULL,
			kind              TEXT NOT NULL DEFAULT '',
			start_offset_ms   INTEGER NOT NULL,
			latency_ms        INTEGER NOT NULL,
			prompt_tokens     INTEGER NOT NULL DEFAULT 0,
			completion_tokens INTEGER NOT NULL DEFAULT 0,
			cached_tokens     INTEGER NOT NULL DEFAULT 0,
			cost_usd          REAL NOT NULL DEFAULT 0,
			forced_final      INTEGER NOT NULL DEFAULT 0,
			progress          INTEGER NOT NULL DEFAULT 0,
			error             TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (run_id, idx)
		)`},
		{"assistant_run_calls table", `
		CREATE TABLE IF NOT EXISTS assistant_run_calls (
			run_id         TEXT NOT NULL REFERENCES assistant_runs(id) ON DELETE CASCADE,
			round          INTEGER NOT NULL,
			seq            INTEGER NOT NULL,
			name           TEXT NOT NULL,
			args           TEXT NOT NULL DEFAULT '',
			result         TEXT NOT NULL DEFAULT '',
			elapsed_ms     INTEGER NOT NULL DEFAULT 0,
			result_bytes   INTEGER NOT NULL DEFAULT 0,
			error          TEXT NOT NULL DEFAULT '',
			truncated      INTEGER NOT NULL DEFAULT 0,
			original_bytes INTEGER,
			cache          TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (run_id, round, seq)
		)`},
	} {
		if _, err := db.Exec(stmt.sql); err != nil {
			return fmt.Errorf("create %s: %w", stmt.what, err)
		}
	}
	return nil
}

// SaveRun stores a finished run with all its rounds and calls and returns it
// with its assigned id. A call's args and result are kept up to
// assistantToolCallMaxBytes each; Truncated and OriginalBytes then say so.
func (s *assistantStore) SaveRun(run assistantRunTrace) (assistantRunTrace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	run.ID = uuid.NewString()
	budget, err := json.Marshal(run.Budget)
	if err != nil {
		return assistantRunTrace{}, fmt.Errorf("encode run budget: %w", err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return assistantRunTrace{}, fmt.Errorf("begin save run: %w", err)
	}
	defer tx.Rollback()

	var answer sql.NullString
	if run.AnswerMessageID != nil {
		answer = sql.NullString{String: *run.AnswerMessageID, Valid: true}
	}
	if _, err := tx.Exec(`INSERT INTO assistant_runs
		(id, conversation_id, after_seq, answer_message_id, model, started_at, duration_ms, stop_reason, error, budget)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.ConversationID, run.AfterSeq, answer, run.Model, run.StartedAt.UnixMilli(), run.DurationMs, run.StopReason, run.Error, string(budget)); err != nil {
		return assistantRunTrace{}, fmt.Errorf("insert run: %w", err)
	}
	roundStmt, err := tx.Prepare(`INSERT INTO assistant_run_rounds
		(run_id, idx, kind, start_offset_ms, latency_ms, prompt_tokens, completion_tokens, cached_tokens, cost_usd, forced_final, progress, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return assistantRunTrace{}, fmt.Errorf("prepare insert run round: %w", err)
	}
	defer roundStmt.Close()
	callStmt, err := tx.Prepare(`INSERT INTO assistant_run_calls
		(run_id, round, seq, name, args, result, elapsed_ms, result_bytes, error, truncated, original_bytes, cache)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return assistantRunTrace{}, fmt.Errorf("prepare insert run call: %w", err)
	}
	defer callStmt.Close()
	for _, r := range run.Rounds {
		if _, err := roundStmt.Exec(run.ID, r.Index, r.Kind, r.StartOffsetMs, r.LatencyMs, r.PromptTokens, r.CompletionTokens, r.CachedTokens, r.CostUSD, boolToInt(r.ForcedFinal), boolToInt(r.Progress), r.Error); err != nil {
			return assistantRunTrace{}, fmt.Errorf("insert run round %d: %w", r.Index, err)
		}
		for _, c := range r.Calls {
			args, result := cutUTF8(c.Args, assistantToolCallMaxBytes), cutUTF8(c.Result, assistantToolCallMaxBytes)
			truncated := len(args) != len(c.Args) || len(result) != len(c.Result)
			var original sql.NullInt64
			if truncated {
				original = sql.NullInt64{Int64: int64(len(c.Args) + len(c.Result)), Valid: true}
			}
			if _, err := callStmt.Exec(run.ID, r.Index, c.Seq, c.Name, args, result, c.ElapsedMs, len(c.Result), c.Error, boolToInt(truncated), original, c.Cache); err != nil {
				return assistantRunTrace{}, fmt.Errorf("insert run call %s: %w", c.Name, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return assistantRunTrace{}, fmt.Errorf("commit save run: %w", err)
	}
	return run, nil
}

const assistantRunColumns = `id, conversation_id, after_seq, answer_message_id, model, started_at, duration_ms, stop_reason, error, budget,
	(SELECT COUNT(*) FROM assistant_run_rounds WHERE run_id = assistant_runs.id AND kind = 'lookups')`

type assistantRunScanner interface {
	Scan(dest ...any) error
}

func scanAssistantRun(row assistantRunScanner) (assistantRunTrace, error) {
	var run assistantRunTrace
	var answer sql.NullString
	var started int64
	var budget string
	if err := row.Scan(&run.ID, &run.ConversationID, &run.AfterSeq, &answer, &run.Model, &started, &run.DurationMs, &run.StopReason, &run.Error, &budget, &run.RoundCount); err != nil {
		return assistantRunTrace{}, err
	}
	if err := json.Unmarshal([]byte(budget), &run.Budget); err != nil {
		return assistantRunTrace{}, fmt.Errorf("decode budget of run %s: %w", run.ID, err)
	}
	if answer.Valid {
		run.AnswerMessageID = &answer.String
	}
	run.StartedAt = time.UnixMilli(started).UTC()
	return run, nil
}

// ListRuns returns a conversation's runs in the order they started, as
// summaries: everything but the rounds, with RoundCount set.
func (s *assistantStore) ListRuns(conversationID string) ([]assistantRunTrace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT `+assistantRunColumns+` FROM assistant_runs WHERE conversation_id = ? ORDER BY started_at, rowid`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()
	out := []assistantRunTrace{}
	for rows.Next() {
		run, err := scanAssistantRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// assistantCallColumns are the columns of a stored call, in the order
// scanAssistantCall reads them.
const assistantCallColumns = `round, seq, name, args, result, elapsed_ms, result_bytes, error, truncated, original_bytes, cache`

func scanAssistantCall(row assistantRunScanner, extra ...any) (assistantToolCall, error) {
	var c assistantToolCall
	var truncated int
	var original sql.NullInt64
	if err := row.Scan(append(extra, &c.Round, &c.Seq, &c.Name, &c.Args, &c.Result, &c.ElapsedMs, &c.ResultBytes, &c.Error, &truncated, &original, &c.Cache)...); err != nil {
		return assistantToolCall{}, err
	}
	c.Truncated = truncated != 0
	c.OriginalBytes = int(original.Int64)
	return c, nil
}

// GetRun returns one run with its rounds and calls, as the run details show
// it: a call's arguments are cut short and its result left out, with the
// result's size in ResultBytes. ok is false when there is no such run.
func (s *assistantStore) GetRun(id string) (assistantRunTrace, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, err := scanAssistantRun(s.db.QueryRow(`SELECT `+assistantRunColumns+` FROM assistant_runs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return assistantRunTrace{}, false, nil
	}
	if err != nil {
		return assistantRunTrace{}, false, fmt.Errorf("get run: %w", err)
	}

	roundRows, err := s.db.Query(`SELECT idx, kind, start_offset_ms, latency_ms, prompt_tokens, completion_tokens, cached_tokens, cost_usd, forced_final, progress, error
		FROM assistant_run_rounds WHERE run_id = ? ORDER BY idx`, id)
	if err != nil {
		return assistantRunTrace{}, false, fmt.Errorf("get run rounds: %w", err)
	}
	defer roundRows.Close()
	run.Rounds = []assistantTraceRound{}
	byIndex := map[int]int{}
	for roundRows.Next() {
		var r assistantTraceRound
		var forced, progress int
		if err := roundRows.Scan(&r.Index, &r.Kind, &r.StartOffsetMs, &r.LatencyMs, &r.PromptTokens, &r.CompletionTokens, &r.CachedTokens, &r.CostUSD, &forced, &progress, &r.Error); err != nil {
			return assistantRunTrace{}, false, fmt.Errorf("scan run round: %w", err)
		}
		r.ForcedFinal, r.Progress = forced != 0, progress != 0
		r.Calls = []assistantToolCall{}
		byIndex[r.Index] = len(run.Rounds)
		run.Rounds = append(run.Rounds, r)
	}
	if err := roundRows.Err(); err != nil {
		return assistantRunTrace{}, false, err
	}

	// The details show the start of the arguments and never the result, so the
	// result column (up to 64 kB a call) is not read.
	callRows, err := s.db.Query(`SELECT round, seq, name, substr(args, 1, ?), '', elapsed_ms, result_bytes, error, truncated, original_bytes, cache
		FROM assistant_run_calls WHERE run_id = ? ORDER BY round, seq`, assistantTraceArgsMaxBytes, id)
	if err != nil {
		return assistantRunTrace{}, false, fmt.Errorf("get run calls: %w", err)
	}
	defer callRows.Close()
	for callRows.Next() {
		c, err := scanAssistantCall(callRows)
		if err != nil {
			return assistantRunTrace{}, false, fmt.Errorf("scan run call: %w", err)
		}
		i, ok := byIndex[c.Round]
		if !ok {
			return assistantRunTrace{}, false, fmt.Errorf("run %s has a call in round %d, which is not recorded", id, c.Round)
		}
		c.Args, c.Result = cutUTF8(c.Args, assistantTraceArgsMaxBytes), ""
		run.Rounds[i].Calls = append(run.Rounds[i].Calls, c)
	}
	return run, true, callRows.Err()
}

// PruneRuns deletes runs past their retention and reports how many went: an
// answered run with no rating after assistantRunRetention, a run that gave no
// answer after assistantFailedRunRetention. A run behind a rated answer is kept
// while the rating stands; clearing the rating puts it back under the rule.
func (s *assistantStore) PruneRuns() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	// An answered run is as old as its answer, the same age the rating rule
	// uses; the run's own start stands in when the answer is gone.
	const expired = `(answer_message_id IS NULL AND started_at < ?)
		OR (answer_message_id IS NOT NULL
		    AND COALESCE((SELECT created_at * 1000 FROM messages WHERE id = answer_message_id), started_at) < ?
		    AND answer_message_id NOT IN (SELECT message_id FROM message_feedback))`
	failedCutoff, answeredCutoff := now.Add(-assistantFailedRunRetention).UnixMilli(), now.Add(-assistantRunRetention).UnixMilli()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin prune runs: %w", err)
	}
	defer tx.Rollback()
	// Mark the answers whose run goes, so a rating knows the lookups are gone.
	if _, err := tx.Exec(`UPDATE messages SET lookups_pruned = 1 WHERE id IN (SELECT answer_message_id FROM assistant_runs WHERE `+expired+`)`, failedCutoff, answeredCutoff); err != nil {
		return 0, fmt.Errorf("mark pruned answers: %w", err)
	}
	res, err := tx.Exec(`DELETE FROM assistant_runs WHERE `+expired, failedCutoff, answeredCutoff)
	if err != nil {
		return 0, fmt.Errorf("prune runs: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit prune runs: %w", err)
	}
	return res.RowsAffected()
}

// runEvery runs fn once now and then every interval until ctx ends. It is the
// loop behind each of the periodic prunes.
func runEvery(ctx context.Context, interval time.Duration, fn func()) {
	fn()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn()
		}
	}
}

// runAssistantRunPruner prunes the stored runs until ctx ends.
func runAssistantRunPruner(ctx context.Context, store *assistantStore) {
	runEvery(ctx, assistantRunPruneEvery, func() {
		n, err := store.PruneRuns()
		if err != nil {
			log.Printf("assistant: %v", err)
			return
		}
		log.Printf("assistant: pruned %d stored runs (answered and unrated older than %s, unanswered older than %s)", n, assistantRunRetention, assistantFailedRunRetention)
	})
}

// migrateMessageToolCalls moves the tool calls an earlier version stored per
// answer (message_tool_calls) into runs, so no recorded call is lost, and drops
// the table. An answer that already has a run keeps it. It does nothing once
// the table is gone.
func migrateMessageToolCalls(db *sql.DB) error {
	var present int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'message_tool_calls'`).Scan(&present); err != nil {
		return fmt.Errorf("look for message_tool_calls: %w", err)
	}
	if present == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin move message_tool_calls: %w", err)
	}
	defer tx.Rollback()

	type legacy struct {
		message, conversation, model string
		seq                          int
		created                      int64
	}
	rows, err := tx.Query(`SELECT m.id, m.conversation_id, m.model, m.seq, m.created_at FROM messages m
		WHERE m.id IN (SELECT message_id FROM message_tool_calls)
		AND NOT EXISTS (SELECT 1 FROM assistant_runs r WHERE r.answer_message_id = m.id)`)
	if err != nil {
		return fmt.Errorf("read message_tool_calls: %w", err)
	}
	var todo []legacy
	for rows.Next() {
		var l legacy
		if err := rows.Scan(&l.message, &l.conversation, &l.model, &l.seq, &l.created); err != nil {
			rows.Close()
			return fmt.Errorf("scan message_tool_calls: %w", err)
		}
		todo = append(todo, l)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	// What the limits were when those answers were written: eight rounds and
	// three minutes, with no stall rule or final reserve.
	const legacyBudget = `{"max_rounds":8,"max_stalled_rounds":0,"tool_phase_ms":180000,"final_reserve_ms":0}`
	for _, l := range todo {
		id := uuid.NewString()
		if _, err := tx.Exec(`INSERT INTO assistant_runs
			(id, conversation_id, after_seq, answer_message_id, model, started_at, duration_ms, stop_reason, error, budget)
			VALUES (?, ?, ?, ?, ?, ?, 0, ?, '', ?)`,
			id, l.conversation, l.seq-1, l.message, l.model, l.created*1000, assistantStopAnswered, legacyBudget); err != nil {
			return fmt.Errorf("move run of %s: %w", l.message, err)
		}
		if _, err := tx.Exec(`INSERT INTO assistant_run_rounds (run_id, idx, kind, start_offset_ms, latency_ms)
			SELECT DISTINCT ?, round, 'lookups', 0, 0 FROM message_tool_calls WHERE message_id = ?`, id, l.message); err != nil {
			return fmt.Errorf("move rounds of %s: %w", l.message, err)
		}
		if _, err := tx.Exec(`INSERT INTO assistant_run_calls
			(run_id, round, seq, name, args, result, elapsed_ms, result_bytes, error, truncated, original_bytes, cache)
			SELECT ?, round, seq, name, args, result, elapsed_ms, CASE WHEN truncated = 1 AND original_bytes IS NOT NULL THEN MAX(length(CAST(result AS BLOB)), original_bytes - length(CAST(args AS BLOB))) ELSE length(CAST(result AS BLOB)) END, error, truncated, original_bytes, ''
			FROM message_tool_calls WHERE message_id = ?`, id, l.message); err != nil {
			return fmt.Errorf("move calls of %s: %w", l.message, err)
		}
	}
	if _, err := tx.Exec(`DROP TABLE message_tool_calls`); err != nil {
		return fmt.Errorf("drop message_tool_calls: %w", err)
	}
	return tx.Commit()
}
