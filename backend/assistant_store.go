package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// assistantConversation is one thread in the onboard assistant's chat
// history (ADR 0093): a list of user/assistant message rows the operator can
// return to, rename (SetTitle), or delete.
type assistantConversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// assistantMessage is one row of a conversation. User and assistant roles
// are persisted (assistant_handlers.go), plus "watch" for a finished watch's
// report (ADR 0160, assistant_watch.go) - a tool round trip inside the
// agentic loop is live status only, never written here. The model/token/cost
// fields are populated on assistant rows and left at their zero value on
// user rows.
type assistantMessage struct {
	ID               string    `json:"id"`
	ConversationID   string    `json:"conversation_id"`
	Seq              int       `json:"seq"`
	Role             string    `json:"role"`
	Content          string    `json:"content"`
	Model            string    `json:"model,omitempty"`
	PromptTokens     int       `json:"prompt_tokens,omitempty"`
	CompletionTokens int       `json:"completion_tokens,omitempty"`
	CostUSD          float64   `json:"cost_usd,omitempty"`
	ToolRounds       int       `json:"tool_rounds,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	// Attachments are the documents (ADR 0106) attached to this message,
	// position-ordered, populated by AppendMessage (from its input) and
	// ListMessages (from message_attachments). Always empty on an assistant
	// row - only a user message can carry attachments (assistant_handlers.go
	// validates and builds them on the POST path).
	Attachments []assistantAttachment `json:"attachments,omitempty"`
	// Proposals are the maintenance change cards Mate attached to this
	// assistant message (ADR 0146), position-ordered, each with the status it
	// has right now (pending, applied or dismissed). AppendMessage saves them
	// in the same transaction as the row; ListMessages reads them back. Always
	// empty on a user row.
	Proposals []assistantProposal `json:"proposals,omitempty"`
}

// assistantAttachment is one row of message_attachments: a document (ADR
// 0106) attached to a user message. Filename is copied at attach time - not
// looked up live - so a document deleted after the fact still renders in
// this conversation's history as "[attachment deleted: x.pdf]"
// (assistantHistoryMessages, assistant_run.go) rather than silently losing
// its name.
type assistantAttachment struct {
	DocumentID string `json:"document_id"`
	Filename   string `json:"filename"`
}

// assistantStore is the SQLite-backed store behind the onboard assistant's
// conversation history, modelled on alarmLogStore: a single-connection pool
// (modernc/sqlite surfaces concurrent writes as "database is locked"),
// CREATE TABLE IF NOT EXISTS so a fresh path and a pre-existing one open the
// same way, and a mutex serializing every method since the pool itself is
// one connection wide. now is overridden by tests to control timestamps
// without a real sleep.
type assistantStore struct {
	mu  sync.Mutex
	db  *sql.DB
	now func() time.Time
}

// globalAssistantStore is the process-wide assistant conversation store,
// opened once in main() alongside globalAlarmLogStore and shared by the
// /api/assistant/conversations handlers (assistant_handlers.go).
var globalAssistantStore *assistantStore

// errAssistantConversationNotFound is returned by DeleteConversation and
// AppendMessage when the conversation id does not exist, so a caller can
// distinguish "already gone" (or "never existed") from every other database
// failure via errors.Is. GetConversation instead reports a miss with an ok
// bool: it is a plain read with nothing else to do with the id, whereas
// Delete and Append both need to short-circuit a write that would otherwise
// silently drop a message on the floor or delete nothing - see AGENTS.md's
// fallback policy on surfacing rather than papering over a missing upstream
// row.
var errAssistantConversationNotFound = errors.New("assistant conversation not found")

// createAssistantSchema creates conversations, messages and
// message_attachments (and their indexes) if they do not already exist.
// Factored out of newAssistantStore so migrateToHelmcentralDB
// (helmcentral_db.go) can create this store's tables in the combined
// database ahead of copying rows into them, without duplicating the schema
// itself.
//
// message_attachments (ADR 0106) carries the documents attached to a user
// message, position-ordered, with no declared foreign key to
// messages/documents - AppendMessage and DeleteConversation manage the rows
// directly rather than relying on a cascade. That is a schema choice, not a
// pragma one: the connection this runs on has foreign_keys=1 set (every
// store sharing the combined file does, via openHelmcentralDB), it is simply
// that a table with no REFERENCES clause has nothing for the pragma to
// enforce. Per ADR 0141, a declared FK from message_attachments.document_id
// to documents is deliberately left for later, not added here.
func createAssistantSchema(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS conversations (
			id         TEXT PRIMARY KEY,
			title      TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`); err != nil {
		return fmt.Errorf("create conversations table: %w", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS messages (
			id                TEXT PRIMARY KEY,
			conversation_id   TEXT NOT NULL,
			seq               INTEGER NOT NULL,
			role              TEXT NOT NULL,
			content           TEXT NOT NULL,
			model             TEXT NOT NULL DEFAULT '',
			prompt_tokens     INTEGER NOT NULL DEFAULT 0,
			completion_tokens INTEGER NOT NULL DEFAULT 0,
			cost_usd          REAL NOT NULL DEFAULT 0,
			tool_rounds       INTEGER NOT NULL DEFAULT 0,
			created_at        INTEGER NOT NULL
		)`); err != nil {
		return fmt.Errorf("create messages table: %w", err)
	}

	// Messages are always read for one conversation in seq order
	// (ListMessages) or aggregated by conversation for the next seq
	// (AppendMessage) - this one index covers both.
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS messages_conversation_seq ON messages (conversation_id, seq)`); err != nil {
		return fmt.Errorf("index messages table: %w", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS message_attachments (
			message_id  TEXT NOT NULL,
			document_id TEXT NOT NULL,
			filename    TEXT NOT NULL,
			position    INTEGER NOT NULL,
			PRIMARY KEY (message_id, document_id)
		)`); err != nil {
		return fmt.Errorf("create message_attachments table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS message_attachments_message ON message_attachments (message_id)`); err != nil {
		return fmt.Errorf("index message_attachments table: %w", err)
	}

	return createAssistantProposalsSchema(db)
}

// newAssistantStore opens (creating if necessary) the SQLite database at
// dbPath - dbPath is helmcentralDBPath() in production, the same file
// documentStore and nearbyContactStore also open - and ensures every table
// this store needs exists. Opened through openHelmcentralDB
// (helmcentral_db.go), which is what sets foreign_keys(1) and WAL: before
// ADR 0141 this store opened its own file directly, with no pragmas beyond
// SetMaxOpenConns(1), because it had no cross-table integrity to enforce and
// no concurrent reader/writer of its own file to protect against blocking.
// Sharing the combined file changes neither fact about this store's own
// schema, but it does now inherit foreign_keys and WAL as a consequence of
// being one of three stores that opens the same file.
func newAssistantStore(dbPath string) (*assistantStore, error) {
	db, err := openHelmcentralDB(dbPath)
	if err != nil {
		return nil, err
	}

	if err := createAssistantSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	return &assistantStore{db: db, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *assistantStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// CreateConversation starts a new, empty thread. Title is stored as given -
// an empty title is not defaulted here, since the caller (the POST handler)
// is the one place that knows what a good placeholder like "New
// conversation" is.
func (s *assistantStore) CreateConversation(title string) (assistantConversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	conv := assistantConversation{
		ID:        uuid.NewString(),
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if _, err := s.db.Exec(
		`INSERT INTO conversations (id, title, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		conv.ID, conv.Title, conv.CreatedAt.Unix(), conv.UpdatedAt.Unix()); err != nil {
		return assistantConversation{}, fmt.Errorf("create conversation: %w", err)
	}
	return conv, nil
}

// ListConversations returns every conversation, most recently active first -
// the order the conversation list panel renders in.
func (s *assistantStore) ListConversations() ([]assistantConversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT id, title, created_at, updated_at FROM conversations ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()

	var out []assistantConversation
	for rows.Next() {
		var conv assistantConversation
		var created, updated int64
		if err := rows.Scan(&conv.ID, &conv.Title, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan conversation: %w", err)
		}
		conv.CreatedAt = time.Unix(created, 0).UTC()
		conv.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, conv)
	}
	return out, rows.Err()
}

// GetConversation looks up a single conversation. ok is false, with a nil
// error, when the id simply does not exist - the normal "not found" case a
// 404 handler checks for, not a database failure.
func (s *assistantStore) GetConversation(id string) (assistantConversation, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(`SELECT id, title, created_at, updated_at FROM conversations WHERE id = ?`, id)

	var conv assistantConversation
	var created, updated int64
	err := row.Scan(&conv.ID, &conv.Title, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return assistantConversation{}, false, nil
	}
	if err != nil {
		return assistantConversation{}, false, fmt.Errorf("get conversation: %w", err)
	}
	conv.CreatedAt = time.Unix(created, 0).UTC()
	conv.UpdatedAt = time.Unix(updated, 0).UTC()
	return conv, true, nil
}

// SetTitle renames a conversation, e.g. from the first user message
// (assistant_handlers.go). It deliberately does not bump updated_at:
// renaming is not conversation activity, and bumping it would reorder the
// list panel out from under an operator who just typed a message.
func (s *assistantStore) SetTitle(id, title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.db.Exec(`UPDATE conversations SET title = ? WHERE id = ?`, title, id); err != nil {
		return fmt.Errorf("set conversation title: %w", err)
	}
	return nil
}

// DeleteConversation removes a conversation and every message in it in one
// transaction, so a crash mid-delete can never leave orphaned messages
// behind. A missing id reports errAssistantConversationNotFound (checked
// with errors.Is).
func (s *assistantStore) DeleteConversation(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin delete conversation: %w", err)
	}
	defer tx.Rollback()

	// Attachment rows first, while messages (and their ids) still exist to
	// scope the delete by - message_attachments carries no conversation_id
	// of its own.
	if _, err := tx.Exec(`DELETE FROM message_attachments WHERE message_id IN (SELECT id FROM messages WHERE conversation_id = ?)`, id); err != nil {
		return fmt.Errorf("delete conversation message attachments: %w", err)
	}

	if _, err := tx.Exec(`DELETE FROM messages WHERE conversation_id = ?`, id); err != nil {
		return fmt.Errorf("delete conversation messages: %w", err)
	}

	result, err := tx.Exec(`DELETE FROM conversations WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	if affected == 0 {
		return errAssistantConversationNotFound
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete conversation: %w", err)
	}
	return nil
}

// AppendMessage adds the next message in a conversation: it assigns ID, Seq
// (one past the highest seq already in the conversation) and CreatedAt,
// inserts the row and bumps the parent conversation's updated_at, all in one
// transaction. The conversation must already exist - an assistant reply or
// user message with nowhere to live is a bug, not a state to paper over
// (AGENTS.md fallback policy), so a missing conversation is reported as
// errAssistantConversationNotFound rather than silently inserting an orphan
// row that would never appear in any ListMessages call.
func (s *assistantStore) AppendMessage(m assistantMessage) (assistantMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return assistantMessage{}, fmt.Errorf("begin append message: %w", err)
	}
	defer tx.Rollback()

	var exists int
	err = tx.QueryRow(`SELECT 1 FROM conversations WHERE id = ?`, m.ConversationID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return assistantMessage{}, errAssistantConversationNotFound
	}
	if err != nil {
		return assistantMessage{}, fmt.Errorf("check conversation exists: %w", err)
	}

	var maxSeq sql.NullInt64
	if err := tx.QueryRow(`SELECT MAX(seq) FROM messages WHERE conversation_id = ?`, m.ConversationID).Scan(&maxSeq); err != nil {
		return assistantMessage{}, fmt.Errorf("read max seq: %w", err)
	}

	now := s.now()
	m.ID = uuid.NewString()
	m.Seq = int(maxSeq.Int64) + 1
	m.CreatedAt = now

	if _, err := tx.Exec(
		`INSERT INTO messages (id, conversation_id, seq, role, content, model, prompt_tokens, completion_tokens, cost_usd, tool_rounds, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.ConversationID, m.Seq, m.Role, m.Content, m.Model, m.PromptTokens, m.CompletionTokens, m.CostUSD, m.ToolRounds, m.CreatedAt.Unix()); err != nil {
		return assistantMessage{}, fmt.Errorf("insert message: %w", err)
	}

	// Attachments, position-ordered by their index in m.Attachments - same
	// transaction as the message row itself, so a crash mid-write can never
	// leave a message with some of its attachments missing.
	for i, att := range m.Attachments {
		if _, err := tx.Exec(
			`INSERT INTO message_attachments (message_id, document_id, filename, position) VALUES (?, ?, ?, ?)`,
			m.ID, att.DocumentID, att.Filename, i); err != nil {
			return assistantMessage{}, fmt.Errorf("insert message attachment: %w", err)
		}
	}

	// Proposals (ADR 0146) ride in the same transaction as the assistant
	// row that carries them.
	if len(m.Proposals) > 0 {
		if m.Role != "assistant" {
			return assistantMessage{}, fmt.Errorf("insert message proposals: only an assistant message can carry them, got role %q", m.Role)
		}
		if err := insertAssistantProposalsTx(tx, m.ID, m.Proposals, now); err != nil {
			return assistantMessage{}, err
		}
	}

	if _, err := tx.Exec(`UPDATE conversations SET updated_at = ? WHERE id = ?`, now.Unix(), m.ConversationID); err != nil {
		return assistantMessage{}, fmt.Errorf("bump conversation updated_at: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return assistantMessage{}, fmt.Errorf("commit append message: %w", err)
	}
	return m, nil
}

// ListMessages returns every message in a conversation, oldest first - the
// order a chat thread renders in.
func (s *assistantStore) ListMessages(conversationID string) ([]assistantMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT id, conversation_id, seq, role, content, model, prompt_tokens, completion_tokens, cost_usd, tool_rounds, created_at
		 FROM messages WHERE conversation_id = ? ORDER BY seq ASC`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var out []assistantMessage
	for rows.Next() {
		var m assistantMessage
		var created int64
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Seq, &m.Role, &m.Content, &m.Model,
			&m.PromptTokens, &m.CompletionTokens, &m.CostUSD, &m.ToolRounds, &created); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		m.CreatedAt = time.Unix(created, 0).UTC()
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	ids := make([]string, len(out))
	for i, m := range out {
		ids[i] = m.ID
	}
	attachmentsByMessage, err := attachmentsForMessages(s.db, ids)
	if err != nil {
		return nil, err
	}
	proposalsByMessage, err := proposalsForMessages(s.db, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Attachments = attachmentsByMessage[out[i].ID]
		out[i].Proposals = proposalsByMessage[out[i].ID]
	}

	return out, nil
}

// attachmentsForMessages returns every message_attachments row for the
// given message ids, grouped by message_id and position-ordered - the
// batch read behind ListMessages, one query rather than one per message.
// sqlQueryer (documents_store.go) is reused here rather than redeclared:
// assistant_store.go and documents_store.go are the same package, and the
// two stores' read helpers share the same *sql.DB/*sql.Tx-compatible shape.
func attachmentsForMessages(q sqlQueryer, messageIDs []string) (map[string][]assistantAttachment, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(messageIDs))
	args := make([]any, len(messageIDs))
	for i, id := range messageIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	rows, err := q.Query(
		`SELECT message_id, document_id, filename FROM message_attachments
		 WHERE message_id IN (`+strings.Join(placeholders, ",")+`) ORDER BY message_id, position`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("list message attachments: %w", err)
	}
	defer rows.Close()

	out := map[string][]assistantAttachment{}
	for rows.Next() {
		var messageID string
		var att assistantAttachment
		if err := rows.Scan(&messageID, &att.DocumentID, &att.Filename); err != nil {
			return nil, fmt.Errorf("scan message attachment: %w", err)
		}
		out[messageID] = append(out[messageID], att)
	}
	return out, rows.Err()
}
