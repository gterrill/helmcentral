package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/labstack/echo/v4"
)

// The operator's verdict on Mate's answers, and the record of how each answer
// was made: the first step of an improvement loop. A rated answer keeps its
// question, its text, the prompt version it was written under and the tool
// calls behind it, so a later cycle can find out why an answer was good or
// poor. Nothing here changes Mate's behaviour.

const (
	assistantFeedbackUp   = "up"
	assistantFeedbackDown = "down"
	// assistantFeedbackNoteMax bounds the optional note, in characters.
	assistantFeedbackNoteMax = 1000
)

var (
	errAssistantMessageNotFound   = errors.New("message not found")
	errAssistantFeedbackNotAnswer = errors.New("only one of Mate's answers can be rated")
	errAssistantFeedbackInvalid   = errors.New("invalid feedback")
	errAssistantFeedbackTooOld    = errors.New("this answer is older than 7 days, so its lookups are no longer kept and it can't be rated again")
)

// assistantToolCall is one tool call behind an assistant message, as recorded.
// Round and Seq order the calls: Round counts the model's tool rounds from
// zero, Seq the calls within a round in the order the model asked for them.
type assistantToolCall struct {
	Round     int    `json:"round"`
	Seq       int    `json:"seq"`
	Name      string `json:"name"`
	Args      string `json:"args"`
	Result    string `json:"result"`
	ElapsedMs int64  `json:"elapsed_ms"`
	// Error is the failure text when the call failed or was not run.
	Error string `json:"error,omitempty"`
	// Truncated says Args or Result was cut to assistantToolCallMaxBytes when
	// stored; OriginalBytes is then the uncut size of Args plus Result. A
	// replay must treat such a record as incomplete.
	Truncated     bool `json:"truncated,omitempty"`
	OriginalBytes int  `json:"original_bytes,omitempty"`
}

const (
	// assistantToolCallMaxBytes bounds each of a stored call's args and result.
	assistantToolCallMaxBytes = 64 * 1024
	// assistantToolCallRetention is how long an unrated answer keeps its tool
	// calls. A rated answer keeps them until the rating is cleared.
	assistantToolCallRetention = 7 * 24 * time.Hour
	// assistantToolCallPruneEvery is how often the prune runs after startup.
	assistantToolCallPruneEvery = 6 * time.Hour
)

// cutUTF8 returns s cut to at most max bytes without splitting a character.
func cutUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// assistantFeedbackSummary is the verdict and note carried on a rated
// answer's message.
type assistantFeedbackSummary struct {
	Verdict string `json:"verdict"`
	Note    string `json:"note"`
}

// assistantFeedback is the operator's verdict on one assistant message.
type assistantFeedback struct {
	MessageID string    `json:"message_id"`
	Verdict   string    `json:"verdict"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// assistantFeedbackEntry is a rated answer with what is needed to review it.
type assistantFeedbackEntry struct {
	assistantFeedback
	ConversationID string `json:"conversation_id"`
	Question       string `json:"question"`
	// QuestionRole is who wrote Question: "user", or "watch" for a finished
	// watch's report that Mate was answering.
	QuestionRole  string `json:"question_role"`
	Answer        string `json:"answer"`
	Model         string `json:"model,omitempty"`
	PromptVersion string `json:"prompt_version,omitempty"`
	// ToolCallsRecorded is false when the calls behind this answer are not
	// on record (it predates recording, or its calls were pruned), so an
	// empty ToolCalls then does not mean "used no tools".
	ToolCallsRecorded bool                `json:"tool_calls_recorded"`
	ToolCalls         []assistantToolCall `json:"tool_calls"`
}

// assistantPromptVersion names the instructions an answer was written under:
// the first 12 hex digits of the SHA-256 of the prompt's rule text and the
// tool definitions on offer. The rule text is the stable part built from an
// empty context, so the vessel, standing notes, help index and other operator
// data (which change with settings, not with the prompt) are left out; so is
// the live part, which changes every turn. webSearch is part of it because it
// switches a rule on.
func assistantPromptVersion(webSearch bool, tools []openRouterTool) string {
	stable, _ := assistantSystemPromptParts(assistantPromptContext{WebSearch: webSearch})
	h := sha256.New()
	h.Write([]byte(stable))
	toolJSON, err := json.Marshal(tools)
	if err != nil {
		// Tool definitions are static structs; failing to encode one is a bug.
		panic(fmt.Sprintf("encode tool definitions for the prompt version: %v", err))
	}
	h.Write([]byte{0})
	h.Write(toolJSON)
	return hex.EncodeToString(h.Sum(nil))[:12]
}

func createAssistantFeedbackSchema(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS message_feedback (
			message_id TEXT PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
			verdict    TEXT NOT NULL CHECK (verdict IN ('up', 'down')),
			note       TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`); err != nil {
		return fmt.Errorf("create message_feedback table: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS message_tool_calls (
			message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
			round      INTEGER NOT NULL,
			seq        INTEGER NOT NULL,
			name       TEXT NOT NULL,
			args       TEXT NOT NULL DEFAULT '',
			result     TEXT NOT NULL DEFAULT '',
			elapsed_ms INTEGER NOT NULL DEFAULT 0,
			error      TEXT NOT NULL DEFAULT '',
			truncated  INTEGER NOT NULL DEFAULT 0,
			original_bytes INTEGER,
			PRIMARY KEY (message_id, round, seq)
		)`); err != nil {
		return fmt.Errorf("create message_tool_calls table: %w", err)
	}
	return nil
}

func insertAssistantToolCallsTx(tx *sql.Tx, messageID string, calls []assistantToolCall) error {
	for _, tc := range calls {
		args, result := cutUTF8(tc.Args, assistantToolCallMaxBytes), cutUTF8(tc.Result, assistantToolCallMaxBytes)
		var original sql.NullInt64
		truncated := 0
		if len(args) != len(tc.Args) || len(result) != len(tc.Result) {
			truncated = 1
			original = sql.NullInt64{Int64: int64(len(tc.Args) + len(tc.Result)), Valid: true}
		}
		if _, err := tx.Exec(
			`INSERT INTO message_tool_calls (message_id, round, seq, name, args, result, elapsed_ms, error, truncated, original_bytes) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			messageID, tc.Round, tc.Seq, tc.Name, args, result, tc.ElapsedMs, tc.Error, truncated, original); err != nil {
			return fmt.Errorf("insert tool call %s: %w", tc.Name, err)
		}
	}
	return nil
}

// ListToolCalls returns the calls behind one assistant message, in the order
// they were made.
func (s *assistantStore) ListToolCalls(messageID string) ([]assistantToolCall, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listToolCallsLocked(messageID)
}

func (s *assistantStore) listToolCallsLocked(messageID string) ([]assistantToolCall, error) {
	rows, err := s.db.Query(
		`SELECT round, seq, name, args, result, elapsed_ms, error, truncated, original_bytes FROM message_tool_calls WHERE message_id = ? ORDER BY round, seq`, messageID)
	if err != nil {
		return nil, fmt.Errorf("list tool calls: %w", err)
	}
	defer rows.Close()
	out := []assistantToolCall{}
	for rows.Next() {
		var tc assistantToolCall
		var truncated int
		var original sql.NullInt64
		if err := rows.Scan(&tc.Round, &tc.Seq, &tc.Name, &tc.Args, &tc.Result, &tc.ElapsedMs, &tc.Error, &truncated, &original); err != nil {
			return nil, fmt.Errorf("scan tool call: %w", err)
		}
		tc.Truncated = truncated != 0
		tc.OriginalBytes = int(original.Int64)
		out = append(out, tc)
	}
	return out, rows.Err()
}

// answerRoleTx checks the message exists and is an assistant message.
func answerRoleTx(tx *sql.Tx, messageID string) error {
	var role string
	err := tx.QueryRow(`SELECT role FROM messages WHERE id = ?`, messageID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return errAssistantMessageNotFound
	}
	if err != nil {
		return fmt.Errorf("read message: %w", err)
	}
	if role != "assistant" {
		return errAssistantFeedbackNotAnswer
	}
	return nil
}

// SetMessageFeedback records the verdict on an assistant message, replacing
// any earlier one (created_at is kept, updated_at moves).
func (s *assistantStore) SetMessageFeedback(messageID, verdict, note string) (assistantFeedback, error) {
	if verdict != assistantFeedbackUp && verdict != assistantFeedbackDown {
		return assistantFeedback{}, fmt.Errorf("%w: verdict must be %q or %q", errAssistantFeedbackInvalid, assistantFeedbackUp, assistantFeedbackDown)
	}
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > assistantFeedbackNoteMax {
		return assistantFeedback{}, fmt.Errorf("%w: the note is longer than %d characters", errAssistantFeedbackInvalid, assistantFeedbackNoteMax)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return assistantFeedback{}, fmt.Errorf("begin set feedback: %w", err)
	}
	defer tx.Rollback()
	if err := answerRoleTx(tx, messageID); err != nil {
		return assistantFeedback{}, err
	}
	now := s.now()
	// A new rating on an answer whose lookups the retention rule has already
	// cleared would list as an answer that used no tools. Replacing a
	// rating that exists is fine: its lookups were kept.
	var rated, createdAt int64
	if err := tx.QueryRow(`SELECT (SELECT COUNT(*) FROM message_feedback WHERE message_id = ?), created_at FROM messages WHERE id = ?`, messageID, messageID).Scan(&rated, &createdAt); err != nil {
		return assistantFeedback{}, fmt.Errorf("read message age: %w", err)
	}
	if rated == 0 && createdAt < now.Add(-assistantToolCallRetention).Unix() {
		return assistantFeedback{}, errAssistantFeedbackTooOld
	}
	if _, err := tx.Exec(`
		INSERT INTO message_feedback (message_id, verdict, note, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(message_id) DO UPDATE SET verdict = excluded.verdict, note = excluded.note, updated_at = excluded.updated_at`,
		messageID, verdict, note, now.Unix(), now.Unix()); err != nil {
		return assistantFeedback{}, fmt.Errorf("save feedback: %w", err)
	}
	var fb assistantFeedback
	var created, updated int64
	if err := tx.QueryRow(`SELECT message_id, verdict, note, created_at, updated_at FROM message_feedback WHERE message_id = ?`, messageID).
		Scan(&fb.MessageID, &fb.Verdict, &fb.Note, &created, &updated); err != nil {
		return assistantFeedback{}, fmt.Errorf("read feedback: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return assistantFeedback{}, fmt.Errorf("commit set feedback: %w", err)
	}
	fb.CreatedAt = time.Unix(created, 0).UTC()
	fb.UpdatedAt = time.Unix(updated, 0).UTC()
	return fb, nil
}

// ClearMessageFeedback removes the verdict on an assistant message. Clearing
// an answer that has none is not an error; an unknown id is.
func (s *assistantStore) ClearMessageFeedback(messageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin clear feedback: %w", err)
	}
	defer tx.Rollback()
	if err := answerRoleTx(tx, messageID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM message_feedback WHERE message_id = ?`, messageID); err != nil {
		return fmt.Errorf("clear feedback: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit clear feedback: %w", err)
	}
	return nil
}

const (
	assistantFeedbackDefaultLimit = 50
	assistantFeedbackMaxLimit     = 200
)

// ListFeedback returns rated answers, most recently rated first, each with
// the question that prompted it and the calls behind it. limit 0 means the
// default (50), at most 200. before is the cursor a previous call returned;
// the cursor it returns is empty on the last page.
func (s *assistantStore) ListFeedback(limit int, before string) ([]assistantFeedbackEntry, string, error) {
	if limit == 0 {
		limit = assistantFeedbackDefaultLimit
	}
	if limit < 0 || limit > assistantFeedbackMaxLimit {
		return nil, "", fmt.Errorf("%w: limit must be between 1 and %d", errAssistantFeedbackInvalid, assistantFeedbackMaxLimit)
	}
	var curUpdated int64 = 1<<62 - 1
	curID := ""
	if before != "" {
		var err error
		stamp, id, ok := strings.Cut(before, "_")
		if curUpdated, err = strconv.ParseInt(stamp, 10, 64); err != nil || !ok {
			return nil, "", fmt.Errorf("%w: unreadable before cursor", errAssistantFeedbackInvalid)
		}
		curID = id
	}

	entries, err := s.readFeedbackPage(limit+1, curUpdated, curID)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(entries) > limit {
		entries = entries[:limit]
		last := entries[limit-1]
		next = fmt.Sprintf("%d_%s", last.UpdatedAt.Unix(), last.MessageID)
	}
	return entries, next, nil
}

// readFeedbackPage does the reads, under the lock: the rated answers, then
// every one's tool calls in a single query.
func (s *assistantStore) readFeedbackPage(n int, curUpdated int64, curID string) ([]assistantFeedbackEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT f.message_id, f.verdict, f.note, f.created_at, f.updated_at,
		       m.conversation_id, m.content, m.model, m.prompt_version, m.tool_calls_recorded,
		       COALESCE((SELECT u.content FROM messages u
		                 WHERE u.conversation_id = m.conversation_id AND u.role IN ('user', 'watch') AND u.seq < m.seq
		                 ORDER BY u.seq DESC LIMIT 1), ''),
		       COALESCE((SELECT u.role FROM messages u
		                 WHERE u.conversation_id = m.conversation_id AND u.role IN ('user', 'watch') AND u.seq < m.seq
		                 ORDER BY u.seq DESC LIMIT 1), '')
		FROM message_feedback f JOIN messages m ON m.id = f.message_id
		WHERE f.updated_at < ? OR (f.updated_at = ? AND f.message_id > ?)
		ORDER BY f.updated_at DESC, f.message_id ASC
		LIMIT ?`, curUpdated, curUpdated, curID, n)
	if err != nil {
		return nil, fmt.Errorf("list feedback: %w", err)
	}
	var out []assistantFeedbackEntry
	for rows.Next() {
		var e assistantFeedbackEntry
		var created, updated int64
		var recorded int
		if err := rows.Scan(&e.MessageID, &e.Verdict, &e.Note, &created, &updated,
			&e.ConversationID, &e.Answer, &e.Model, &e.PromptVersion, &recorded, &e.Question, &e.QuestionRole); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan feedback: %w", err)
		}
		e.CreatedAt = time.Unix(created, 0).UTC()
		e.UpdatedAt = time.Unix(updated, 0).UTC()
		e.ToolCallsRecorded = recorded != 0
		e.ToolCalls = []assistantToolCall{}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(out) == 0 {
		return out, nil
	}

	// The page may be one over the limit; the extra row only signals a next
	// page, so it needs no calls.
	ids := make([]string, len(out))
	index := make(map[string]int, len(out))
	for i, e := range out {
		ids[i] = e.MessageID
		index[e.MessageID] = i
	}
	marks, args := inPlaceholders(ids)
	callRows, err := s.db.Query(
		`SELECT message_id, round, seq, name, args, result, elapsed_ms, error, truncated, original_bytes
		 FROM message_tool_calls WHERE message_id IN (`+marks+`) ORDER BY message_id, round, seq`, args...)
	if err != nil {
		return nil, fmt.Errorf("list feedback tool calls: %w", err)
	}
	defer callRows.Close()
	for callRows.Next() {
		var id string
		var tc assistantToolCall
		var truncated int
		var original sql.NullInt64
		if err := callRows.Scan(&id, &tc.Round, &tc.Seq, &tc.Name, &tc.Args, &tc.Result, &tc.ElapsedMs, &tc.Error, &truncated, &original); err != nil {
			return nil, fmt.Errorf("scan feedback tool call: %w", err)
		}
		tc.Truncated = truncated != 0
		tc.OriginalBytes = int(original.Int64)
		out[index[id]].ToolCalls = append(out[index[id]].ToolCalls, tc)
	}
	return out, callRows.Err()
}

// PUT /api/assistant/messages/:id/feedback {verdict, note}
func putAssistantFeedbackHandler(c echo.Context) error {
	var body struct {
		Verdict string `json:"verdict"`
		Note    string `json:"note"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
	}
	fb, err := globalAssistantStore.SetMessageFeedback(c.Param("id"), body.Verdict, body.Note)
	if err != nil {
		return writeAssistantFeedbackError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"feedback": fb})
}

// DELETE /api/assistant/messages/:id/feedback
func deleteAssistantFeedbackHandler(c echo.Context) error {
	if err := globalAssistantStore.ClearMessageFeedback(c.Param("id")); err != nil {
		return writeAssistantFeedbackError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// GET /api/assistant/feedback?limit=50&before=<cursor>
func listAssistantFeedbackHandler(c echo.Context) error {
	limit := 0
	if raw := c.QueryParam("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "limit must be a whole number from 1 to 200"})
		}
		limit = n
	}
	list, next, err := globalAssistantStore.ListFeedback(limit, c.QueryParam("before"))
	if err != nil {
		return writeAssistantFeedbackError(c, err)
	}
	if list == nil {
		list = []assistantFeedbackEntry{}
	}
	return c.JSON(http.StatusOK, map[string]any{"feedback": list, "next": next})
}

func writeAssistantFeedbackError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, errAssistantMessageNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, errAssistantFeedbackTooOld):
		return c.JSON(http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, errAssistantFeedbackNotAnswer), errors.Is(err, errAssistantFeedbackInvalid):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
}

// PruneToolCalls deletes the tool calls of unrated answers older than
// assistantToolCallRetention and reports how many rows went. Rated answers
// keep theirs; clearing a rating puts the answer back under this rule.
func (s *assistantStore) PruneToolCalls() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := s.now().Add(-assistantToolCallRetention).Unix()
	const unratedOld = `message_id IN (SELECT id FROM messages WHERE created_at < ?)
		  AND message_id NOT IN (SELECT message_id FROM message_feedback)`
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin prune tool calls: %w", err)
	}
	defer tx.Rollback()
	// The answers lose their calls, so they stop counting as recorded.
	if _, err := tx.Exec(`UPDATE messages SET tool_calls_recorded = 0 WHERE id IN (SELECT message_id FROM message_tool_calls WHERE `+unratedOld+`)`, cutoff); err != nil {
		return 0, fmt.Errorf("unmark pruned answers: %w", err)
	}
	res, err := tx.Exec(`DELETE FROM message_tool_calls WHERE `+unratedOld, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune tool calls: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit prune tool calls: %w", err)
	}
	return res.RowsAffected()
}

// runAssistantToolCallPruner prunes once now and then every
// assistantToolCallPruneEvery until ctx ends, logging how many rows went.
func runAssistantToolCallPruner(ctx context.Context, store *assistantStore) {
	prune := func() {
		n, err := store.PruneToolCalls()
		if err != nil {
			log.Printf("assistant: %v", err)
			return
		}
		log.Printf("assistant: pruned %d stored tool call rows from unrated answers older than %s", n, assistantToolCallRetention)
	}
	prune()
	t := time.NewTicker(assistantToolCallPruneEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			prune()
		}
	}
}

// feedbackForMessages returns the verdict and note for each rated message id.
func feedbackForMessages(q sqlQueryer, ids []string) (map[string]*assistantFeedbackSummary, error) {
	out := map[string]*assistantFeedbackSummary{}
	if len(ids) == 0 {
		return out, nil
	}
	marks, args := inPlaceholders(ids)
	rows, err := q.Query(`SELECT message_id, verdict, note FROM message_feedback WHERE message_id IN (`+marks+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("read message feedback: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var f assistantFeedbackSummary
		if err := rows.Scan(&id, &f.Verdict, &f.Note); err != nil {
			return nil, fmt.Errorf("scan message feedback: %w", err)
		}
		out[id] = &f
	}
	return out, rows.Err()
}
