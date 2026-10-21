package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
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
	ConversationID string              `json:"conversation_id"`
	Question       string              `json:"question"`
	Answer         string              `json:"answer"`
	Model          string              `json:"model,omitempty"`
	PromptVersion  string              `json:"prompt_version,omitempty"`
	ToolCalls      []assistantToolCall `json:"tool_calls"`
}

// assistantPromptVersion names the stable system-prompt part an answer was
// written under: the first 12 hex digits of its SHA-256. The live part
// (vessel state, screen) changes every turn and is left out on purpose.
func assistantPromptVersion(stable string) string {
	sum := sha256.Sum256([]byte(stable))
	return hex.EncodeToString(sum[:])[:12]
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
			PRIMARY KEY (message_id, round, seq)
		)`); err != nil {
		return fmt.Errorf("create message_tool_calls table: %w", err)
	}
	return nil
}

func insertAssistantToolCallsTx(tx *sql.Tx, messageID string, calls []assistantToolCall) error {
	for _, tc := range calls {
		if _, err := tx.Exec(
			`INSERT INTO message_tool_calls (message_id, round, seq, name, args, result, elapsed_ms, error) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			messageID, tc.Round, tc.Seq, tc.Name, tc.Args, tc.Result, tc.ElapsedMs, tc.Error); err != nil {
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
		`SELECT round, seq, name, args, result, elapsed_ms, error FROM message_tool_calls WHERE message_id = ? ORDER BY round, seq`, messageID)
	if err != nil {
		return nil, fmt.Errorf("list tool calls: %w", err)
	}
	defer rows.Close()
	out := []assistantToolCall{}
	for rows.Next() {
		var tc assistantToolCall
		if err := rows.Scan(&tc.Round, &tc.Seq, &tc.Name, &tc.Args, &tc.Result, &tc.ElapsedMs, &tc.Error); err != nil {
			return nil, fmt.Errorf("scan tool call: %w", err)
		}
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

// ListFeedback returns every rated answer, most recently rated first, each
// with the question that prompted it and the calls behind it.
func (s *assistantStore) ListFeedback() ([]assistantFeedbackEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT f.message_id, f.verdict, f.note, f.created_at, f.updated_at,
		       m.conversation_id, m.content, m.model, m.prompt_version,
		       COALESCE((SELECT u.content FROM messages u
		                 WHERE u.conversation_id = m.conversation_id AND u.role = 'user' AND u.seq < m.seq
		                 ORDER BY u.seq DESC LIMIT 1), '')
		FROM message_feedback f JOIN messages m ON m.id = f.message_id
		ORDER BY f.updated_at DESC, f.message_id`)
	if err != nil {
		return nil, fmt.Errorf("list feedback: %w", err)
	}
	var out []assistantFeedbackEntry
	for rows.Next() {
		var e assistantFeedbackEntry
		var created, updated int64
		if err := rows.Scan(&e.MessageID, &e.Verdict, &e.Note, &created, &updated,
			&e.ConversationID, &e.Answer, &e.Model, &e.PromptVersion, &e.Question); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan feedback: %w", err)
		}
		e.CreatedAt = time.Unix(created, 0).UTC()
		e.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range out {
		calls, err := s.listToolCallsLocked(out[i].MessageID)
		if err != nil {
			return nil, err
		}
		out[i].ToolCalls = calls
	}
	return out, nil
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

// GET /api/assistant/feedback
func listAssistantFeedbackHandler(c echo.Context) error {
	list, err := globalAssistantStore.ListFeedback()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if list == nil {
		list = []assistantFeedbackEntry{}
	}
	return c.JSON(http.StatusOK, map[string]any{"feedback": list})
}

func writeAssistantFeedbackError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, errAssistantMessageNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, errAssistantFeedbackNotAnswer), errors.Is(err, errAssistantFeedbackInvalid):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
}
