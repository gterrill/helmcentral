package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
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
	// check_trigger and check_draft arrived after the table did: the draft a
	// correction round sent back. A row from before reads back empty.
	for _, col := range []struct{ name, decl string }{
		{"check_trigger", "TEXT NOT NULL DEFAULT ''"},
		{"check_draft", "TEXT NOT NULL DEFAULT ''"},
		// What the activity view shows (check_triggers is a JSON list). A row
		// from before reads back as not recorded: '', 0, and NULL for thought_ms.
		{"check_triggers", "TEXT NOT NULL DEFAULT ''"},
		{"reasoning", "TEXT NOT NULL DEFAULT ''"},
		{"reasoning_kind", "TEXT NOT NULL DEFAULT ''"},
		{"reasoning_bytes", "INTEGER NOT NULL DEFAULT 0"},
		{"chatter", "TEXT NOT NULL DEFAULT ''"},
		{"content_start_ms", "INTEGER"},
	} {
		if err := addColumnIfMissing(db, "assistant_run_rounds", col.name, col.decl); err != nil {
			return err
		}
	}
	// How a call ended, stored when a run is saved. A call from before reads
	// back '' and is sorted by its error text.
	if err := addColumnIfMissing(db, "assistant_run_calls", "outcome", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	// Worked out once when a run is saved, so reading one is a plain select.
	// A run from before reads thought_ms NULL, 0 lookups and no reasoning kind.
	for _, col := range []struct{ name, decl string }{
		{"thought_ms", "INTEGER"},
		{"lookup_count", "INTEGER NOT NULL DEFAULT 0"},
		{"reasoning_kind", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := addColumnIfMissing(db, "assistant_runs", col.name, col.decl); err != nil {
			return err
		}
	}
	return nil
}

// SaveRun stores a finished run with all its rounds and calls and returns it
// with its assigned id. A call's args and result are kept up to
// assistantToolCallMaxBytes each; Truncated and OriginalBytes then say so.
func (s *assistantStore) SaveRun(run assistantRunTrace) (assistantRunTrace, error) {
	run.ID = uuid.NewString()
	budget, err := json.Marshal(run.Budget)
	if err != nil {
		return assistantRunTrace{}, fmt.Errorf("encode run budget: %w", err)
	}
	// What is worked out from the rounds is set on the run before it is stored,
	// so the run SaveRun returns holds what a later read will.
	run.ThoughtMs = nil
	if run.AnswerMessageID != nil {
		run.ThoughtMs = assistantAnswerThought(run.Rounds)
	}
	run.LookupCount = assistantRunLookupCount(run.Rounds)
	run.ReasoningKind = assistantRunReasoningKind(run.Rounds)
	err = runTxUnderLock(lockAssistantSaveRun, &s.mu, s.db, time.Now, lockRecorder(), func(tx *sql.Tx) error {
		return saveRunTx(tx, run, string(budget))
	})
	if err != nil {
		return assistantRunTrace{}, err
	}
	return run, nil
}

func saveRunTx(tx *sql.Tx, run assistantRunTrace, budget string) error {
	var answer sql.NullString
	if run.AnswerMessageID != nil {
		answer = sql.NullString{String: *run.AnswerMessageID, Valid: true}
	}
	if _, err := tx.Exec(`INSERT INTO assistant_runs
		(id, conversation_id, after_seq, answer_message_id, model, started_at, duration_ms, stop_reason, error, budget, thought_ms, lookup_count, reasoning_kind)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.ConversationID, run.AfterSeq, answer, run.Model, run.StartedAt.UnixMilli(), run.DurationMs, run.StopReason, run.Error, budget,
		nullInt64(run.ThoughtMs), run.LookupCount, run.ReasoningKind); err != nil {
		return fmt.Errorf("insert run: %w", err)
	}
	roundStmt, err := tx.Prepare(`INSERT INTO assistant_run_rounds
		(run_id, idx, kind, start_offset_ms, latency_ms, prompt_tokens, completion_tokens, cached_tokens, cost_usd, forced_final, progress, error, check_trigger, check_draft,
		 check_triggers, reasoning, reasoning_kind, reasoning_bytes, chatter, content_start_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert run round: %w", err)
	}
	defer roundStmt.Close()
	callStmt, err := tx.Prepare(`INSERT INTO assistant_run_calls
		(run_id, round, seq, name, args, result, elapsed_ms, result_bytes, error, truncated, original_bytes, cache, outcome)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert run call: %w", err)
	}
	defer callStmt.Close()
	for _, r := range run.Rounds {
		// The list is the source: the JSON column holds it and the older
		// check_trigger column holds it joined, for rows and readers that
		// predate the list.
		triggers := ""
		legacyTrigger := strings.Join(r.CheckTriggers, " and ")
		if len(r.CheckTriggers) == 0 {
			// A round read back from before the list existed has only the
			// joined text; keep it.
			legacyTrigger = r.CheckTrigger
		} else {
			encoded, err := json.Marshal(r.CheckTriggers)
			if err != nil {
				return fmt.Errorf("encode check triggers of round %d: %w", r.Index, err)
			}
			triggers = string(encoded)
		}
		if _, err := roundStmt.Exec(run.ID, r.Index, r.Kind, r.StartOffsetMs, r.LatencyMs, r.PromptTokens, r.CompletionTokens, r.CachedTokens, r.CostUSD, boolToInt(r.ForcedFinal), boolToInt(r.Progress), r.Error, legacyTrigger, cutUTF8(r.CheckDraft, assistantToolCallMaxBytes),
			triggers, cutUTF8(r.Reasoning, assistantReasoningMaxBytes), r.ReasoningKind, r.ReasoningBytes, cutUTF8(r.Chatter, assistantToolCallMaxBytes), nullInt64(r.ContentStartMs)); err != nil {
			return fmt.Errorf("insert run round %d: %w", r.Index, err)
		}
		for _, c := range r.Calls {
			args, result := cutUTF8(c.Args, assistantToolCallMaxBytes), cutUTF8(c.Result, assistantToolCallMaxBytes)
			truncated := len(args) != len(c.Args) || len(result) != len(c.Result)
			var original sql.NullInt64
			if truncated {
				original = sql.NullInt64{Int64: int64(len(c.Args) + len(c.Result)), Valid: true}
			}
			if _, err := callStmt.Exec(run.ID, r.Index, c.Seq, c.Name, args, result, c.ElapsedMs, len(c.Result), c.Error, boolToInt(truncated), original, c.Cache, assistantCallOutcome(c.Error)); err != nil {
				return fmt.Errorf("insert run call %s: %w", c.Name, err)
			}
		}
	}
	return nil
}

// assistantRunColumns are a run's own columns and the number of its lookup
// rounds. thought_ms, lookup_count and reasoning_kind were worked out when the
// run was saved.
const assistantRunColumns = `id, conversation_id, after_seq, answer_message_id, model, started_at, duration_ms, stop_reason, error, budget,
	(SELECT COUNT(*) FROM assistant_run_rounds WHERE run_id = assistant_runs.id AND kind = 'lookups'),
	thought_ms, lookup_count, reasoning_kind`

type assistantRunScanner interface {
	Scan(dest ...any) error
}

func scanAssistantRun(row assistantRunScanner) (assistantRunTrace, error) {
	var run assistantRunTrace
	var answer sql.NullString
	var started int64
	var budget string
	var thought sql.NullInt64
	if err := row.Scan(&run.ID, &run.ConversationID, &run.AfterSeq, &answer, &run.Model, &started, &run.DurationMs, &run.StopReason, &run.Error, &budget, &run.RoundCount, &thought, &run.LookupCount, &run.ReasoningKind); err != nil {
		return assistantRunTrace{}, err
	}
	if thought.Valid {
		run.ThoughtMs = &thought.Int64
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
	defer lockAssistantListRuns.held(&s.mu).release()
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
const assistantCallColumns = `round, seq, name, args, result, elapsed_ms, result_bytes, error, truncated, original_bytes, cache, outcome`

func scanAssistantCall(row assistantRunScanner, extra ...any) (assistantToolCall, error) {
	var c assistantToolCall
	var truncated int
	var original sql.NullInt64
	var stored string
	if err := row.Scan(append(extra, &c.Round, &c.Seq, &c.Name, &c.Args, &c.Result, &c.ElapsedMs, &c.ResultBytes, &c.Error, &truncated, &original, &c.Cache, &stored)...); err != nil {
		return assistantToolCall{}, err
	}
	c.Truncated = truncated != 0
	c.OriginalBytes = int(original.Int64)
	c.Outcome = assistantStoredCallOutcome(stored, c.Error)
	c.Reason = assistantCallReason(c.Name, c.Outcome, c.Error)
	return c, nil
}

// GetRun returns one run with its rounds and calls, as the run details show
// it: a call's arguments are cut short and its result left out, with the
// result's size in ResultBytes. ok is false when there is no such run.
func (s *assistantStore) GetRun(id string) (assistantRunTrace, bool, error) {
	defer lockAssistantGetRun.held(&s.mu).release()
	run, err := scanAssistantRun(s.db.QueryRow(`SELECT `+assistantRunColumns+` FROM assistant_runs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return assistantRunTrace{}, false, nil
	}
	if err != nil {
		return assistantRunTrace{}, false, fmt.Errorf("get run: %w", err)
	}

	roundRows, err := s.db.Query(`SELECT idx, kind, start_offset_ms, latency_ms, prompt_tokens, completion_tokens, cached_tokens, cost_usd, forced_final, progress, error, check_trigger, check_draft,
		check_triggers, reasoning, reasoning_kind, reasoning_bytes, chatter, content_start_ms
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
		var triggers string
		var contentStart sql.NullInt64
		if err := roundRows.Scan(&r.Index, &r.Kind, &r.StartOffsetMs, &r.LatencyMs, &r.PromptTokens, &r.CompletionTokens, &r.CachedTokens, &r.CostUSD, &forced, &progress, &r.Error, &r.CheckTrigger, &r.CheckDraft, &triggers, &r.Reasoning, &r.ReasoningKind, &r.ReasoningBytes, &r.Chatter, &contentStart); err != nil {
			return assistantRunTrace{}, false, fmt.Errorf("scan run round: %w", err)
		}
		if contentStart.Valid {
			r.ContentStartMs = &contentStart.Int64
		}
		r.CheckTriggers = []string{}
		if triggers != "" {
			if err := json.Unmarshal([]byte(triggers), &r.CheckTriggers); err != nil {
				return assistantRunTrace{}, false, fmt.Errorf("decode check triggers of round %d of run %s: %w", r.Index, id, err)
			}
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
	// The arguments are read whole so the call's text is worked out from them,
	// then cut for the details.
	callRows, err := s.db.Query(`SELECT round, seq, name, args, '', elapsed_ms, result_bytes, error, truncated, original_bytes, cache, outcome
		FROM assistant_run_calls WHERE run_id = ? ORDER BY round, seq`, id)
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
		c.Text = describeAssistantToolCall(c.Name, json.RawMessage(c.Args))
		c.Subject = describeAssistantToolSubject(c.Name, json.RawMessage(c.Args))
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
	var removed int64
	err := runTxUnderLock(lockAssistantPruneRuns, &s.mu, s.db, time.Now, lockRecorder(), func(tx *sql.Tx) error {
		var err error
		removed, err = pruneRunsTx(tx, s.now())
		return err
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}

func pruneRunsTx(tx *sql.Tx, now time.Time) (int64, error) {
	// An answered run is as old as its answer, the same age the rating rule
	// uses; the run's own start stands in when the answer is gone.
	const expired = `(answer_message_id IS NULL AND started_at < ?)
		OR (answer_message_id IS NOT NULL
		    AND COALESCE((SELECT created_at * 1000 FROM messages WHERE id = answer_message_id), started_at) < ?
		    AND answer_message_id NOT IN (SELECT message_id FROM message_feedback))`
	failedCutoff, answeredCutoff := now.Add(-assistantFailedRunRetention).UnixMilli(), now.Add(-assistantRunRetention).UnixMilli()
	// Mark the answers whose run goes, so a rating knows the lookups are gone.
	if _, err := tx.Exec(`UPDATE messages SET lookups_pruned = 1 WHERE id IN (SELECT answer_message_id FROM assistant_runs WHERE `+expired+`)`, failedCutoff, answeredCutoff); err != nil {
		return 0, fmt.Errorf("mark pruned answers: %w", err)
	}
	res, err := tx.Exec(`DELETE FROM assistant_runs WHERE `+expired, failedCutoff, answeredCutoff)
	if err != nil {
		return 0, fmt.Errorf("prune runs: %w", err)
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

// markLegacyPrunedAnswers marks the answers the earlier pruner cleared. It
// cleared an answer's calls after the retention window and left no mark, so an
// answer older than the window that used tools and has no run lost its
// lookups. A newer one with no run never had them recorded, which is a
// different thing and is left alone. It does nothing once everything is
// marked, so it runs on every open.
func markLegacyPrunedAnswers(db *sql.DB, now time.Time) error {
	cutoff := now.Add(-assistantRunRetention).Unix()
	if _, err := db.Exec(`UPDATE messages SET lookups_pruned = 1
		WHERE tool_rounds > 0 AND lookups_pruned = 0 AND created_at < ?
		AND NOT EXISTS (SELECT 1 FROM assistant_runs r WHERE r.answer_message_id = messages.id)`, cutoff); err != nil {
		return fmt.Errorf("mark answers the earlier pruner cleared: %w", err)
	}
	return nil
}
