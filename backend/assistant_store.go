package main

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
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
	// SummaryNoteID is the note this conversation was last summarised into
	// (ADR 0162), nil until the operator saves one. The stored value can
	// outlive the note: handlers go through liveAssistantSummaryNoteID, which
	// reports a deleted note as none.
	SummaryNoteID *string `json:"summary_note_id,omitempty"`
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

	// summary_note_id (ADR 0162) arrived after the table did, and CREATE
	// TABLE IF NOT EXISTS leaves an existing table alone, so the column is
	// added by hand when a database from before it opens.
	var hasSummaryNote int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('conversations') WHERE name = 'summary_note_id'`).Scan(&hasSummaryNote); err != nil {
		return fmt.Errorf("inspect conversations table: %w", err)
	}
	if hasSummaryNote == 0 {
		if _, err := db.Exec(`ALTER TABLE conversations ADD COLUMN summary_note_id TEXT`); err != nil {
			return fmt.Errorf("add conversations.summary_note_id: %w", err)
		}
	}

	if err := createAssistantSearchSchema(db); err != nil {
		return err
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

	rows, err := s.db.Query(`SELECT id, title, created_at, updated_at, summary_note_id FROM conversations ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()

	var out []assistantConversation
	for rows.Next() {
		var conv assistantConversation
		var created, updated int64
		var noteID sql.NullString
		if err := rows.Scan(&conv.ID, &conv.Title, &created, &updated, &noteID); err != nil {
			return nil, fmt.Errorf("scan conversation: %w", err)
		}
		if noteID.Valid {
			conv.SummaryNoteID = &noteID.String
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

	row := s.db.QueryRow(`SELECT id, title, created_at, updated_at, summary_note_id FROM conversations WHERE id = ?`, id)

	var conv assistantConversation
	var created, updated int64
	var noteID sql.NullString
	err := row.Scan(&conv.ID, &conv.Title, &created, &updated, &noteID)
	if errors.Is(err, sql.ErrNoRows) {
		return assistantConversation{}, false, nil
	}
	if err != nil {
		return assistantConversation{}, false, fmt.Errorf("get conversation: %w", err)
	}
	conv.CreatedAt = time.Unix(created, 0).UTC()
	conv.UpdatedAt = time.Unix(updated, 0).UTC()
	if noteID.Valid {
		conv.SummaryNoteID = &noteID.String
	}
	return conv, true, nil
}

// SetSummaryNote records the note a conversation was summarised into (ADR
// 0162); an empty noteID clears it. Like SetTitle it does not bump
// updated_at: saving a note is not conversation activity. A missing
// conversation is errAssistantConversationNotFound.
func (s *assistantStore) SetSummaryNote(id, noteID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var value any
	if noteID != "" {
		value = noteID
	}
	result, err := s.db.Exec(`UPDATE conversations SET summary_note_id = ? WHERE id = ?`, value, id)
	if err != nil {
		return fmt.Errorf("set conversation summary note: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("set conversation summary note: %w", err)
	}
	if affected == 0 {
		return errAssistantConversationNotFound
	}
	return nil
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

// createAssistantSearchSchema creates messages_fts, the full-text index
// behind conversation search (ADR 0161), and the triggers that keep it level
// with messages. Only user and assistant rows are indexed: a watch report is
// machine output the operator never typed or asked for. The FTS table holds
// its own copy of the text (not external content) so the role filter needs no
// special rebuild. It is keyed on the message id, not the rowid: messages has
// a TEXT primary key, so its implicit rowids are not stable across VACUUM
// (the backup method, ADR 0141).
//
// A database that already holds messages gets them indexed the first time
// the table is created; once it exists the triggers carry every later write.
// An index from the earlier rowid-keyed shape (no message_id column) is
// dropped and rebuilt.
func createAssistantSearchSchema(db *sql.DB) error {
	// One transaction: a crash or error part-way must not leave an index that
	// exists but was never backfilled.
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin messages_fts setup: %w", err)
	}
	defer tx.Rollback()
	var existing int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'messages_fts'`).Scan(&existing); err != nil {
		return fmt.Errorf("check messages_fts: %w", err)
	}
	if existing > 0 {
		var hasMessageID int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('messages_fts') WHERE name = 'message_id'`).Scan(&hasMessageID); err != nil {
			return fmt.Errorf("inspect messages_fts: %w", err)
		}
		if hasMessageID == 0 {
			for _, q := range []string{
				`DROP TRIGGER IF EXISTS messages_fts_insert`,
				`DROP TRIGGER IF EXISTS messages_fts_delete`,
				`DROP TRIGGER IF EXISTS messages_fts_update`,
				`DROP TABLE messages_fts`,
			} {
				if _, err := tx.Exec(q); err != nil {
					return fmt.Errorf("replace messages_fts: %w", err)
				}
			}
			existing = 0
		}
	}

	if _, err := tx.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
		content, message_id UNINDEXED, conversation_id UNINDEXED, tokenize = 'unicode61 remove_diacritics 2')`); err != nil {
		return fmt.Errorf("create messages_fts: %w", err)
	}
	if _, err := tx.Exec(`CREATE TRIGGER IF NOT EXISTS messages_fts_insert AFTER INSERT ON messages
		WHEN new.role IN ('user', 'assistant')
		BEGIN
			INSERT INTO messages_fts (content, message_id, conversation_id) VALUES (new.content, new.id, new.conversation_id);
		END`); err != nil {
		return fmt.Errorf("create messages_fts insert trigger: %w", err)
	}
	if _, err := tx.Exec(`CREATE TRIGGER IF NOT EXISTS messages_fts_delete AFTER DELETE ON messages
		BEGIN
			DELETE FROM messages_fts WHERE message_id = old.id;
		END`); err != nil {
		return fmt.Errorf("create messages_fts delete trigger: %w", err)
	}
	if _, err := tx.Exec(`CREATE TRIGGER IF NOT EXISTS messages_fts_update AFTER UPDATE OF content, role, conversation_id ON messages
		BEGIN
			DELETE FROM messages_fts WHERE message_id = old.id;
			INSERT INTO messages_fts (content, message_id, conversation_id)
				SELECT new.content, new.id, new.conversation_id WHERE new.role IN ('user', 'assistant');
		END`); err != nil {
		return fmt.Errorf("create messages_fts update trigger: %w", err)
	}

	if existing == 0 {
		if _, err := tx.Exec(`INSERT INTO messages_fts (content, message_id, conversation_id)
			SELECT content, id, conversation_id FROM messages WHERE role IN ('user', 'assistant')`); err != nil {
			return fmt.Errorf("backfill messages_fts: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit messages_fts setup: %w", err)
	}
	return nil
}

// assistantConversationExcerpt is one matching stretch of a message.
type assistantConversationExcerpt struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// assistantConversationSearchHit is one conversation that matched a search:
// by title, by message text, or both. Excerpts is empty for a title-only
// match and holds the best-ranked matching messages first otherwise.
type assistantConversationSearchHit struct {
	ID        string                         `json:"id"`
	Title     string                         `json:"title"`
	UpdatedAt time.Time                      `json:"updated_at"`
	Excerpts  []assistantConversationExcerpt `json:"excerpts,omitempty"`
}

// assistantConversationSearchOptions tunes SearchConversations. The zero
// value is the list-panel shape: a short snippet, one per conversation.
type assistantConversationSearchOptions struct {
	Limit            int    // conversations returned, default 20
	ExcerptsPerConv  int    // default 1
	ExcerptTokens    int    // words of context around a match, default 16, max 64
	ExcludeConvID    string // a conversation to leave out (the one being asked from)
	ExcerptMaxRunes  int    // hard cap per excerpt, default 400
	CandidateMessage int    // matching messages examined, default 300
}

// SearchConversations finds conversations whose title or user/assistant
// message text contains every word of query (the last word as a prefix),
// case-insensitively, most recently active first. An empty query is an
// error: it matches everything, which is ListConversations' job.
func (s *assistantStore) SearchConversations(query string, opts assistantConversationSearchOptions) ([]assistantConversationSearchHit, error) {
	match, ok := ftsMatchQuery(query)
	if !ok {
		return nil, errors.New("search conversations: query must not be empty")
	}
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	if opts.ExcerptsPerConv <= 0 {
		opts.ExcerptsPerConv = 1
	}
	if opts.ExcerptTokens <= 0 {
		opts.ExcerptTokens = 16
	}
	if opts.ExcerptTokens > 64 {
		opts.ExcerptTokens = 64
	}
	if opts.ExcerptMaxRunes <= 0 {
		opts.ExcerptMaxRunes = 400
	}
	if opts.CandidateMessage <= 0 {
		opts.CandidateMessage = 300
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Conversations whose messages match. Grouped to the conversation before
	// any limit, so a word that matches thousands of messages elsewhere cannot
	// push a recent conversation out of the candidates.
	matched := map[string]bool{}
	rows, err := s.db.Query(
		`SELECT DISTINCT conversation_id FROM messages_fts WHERE messages_fts MATCH ? AND conversation_id <> ?`,
		match, opts.ExcludeConvID)
	if err != nil {
		return nil, fmt.Errorf("search conversation messages: %w", err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan conversation search row: %w", err)
		}
		matched[id] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	// Titles are few: load them all and match in Go with the tokenisation
	// the message index uses.
	terms := searchTerms(query)
	crows, err := s.db.Query(`SELECT id, title, updated_at FROM conversations WHERE id <> ?`, opts.ExcludeConvID)
	if err != nil {
		return nil, fmt.Errorf("search conversation titles: %w", err)
	}
	var out []assistantConversationSearchHit
	for crows.Next() {
		var h assistantConversationSearchHit
		var updated int64
		if err := crows.Scan(&h.ID, &h.Title, &updated); err != nil {
			crows.Close()
			return nil, fmt.Errorf("scan conversation title match: %w", err)
		}
		if matched[h.ID] || titleMatchesTerms(h.Title, terms) {
			h.UpdatedAt = time.Unix(updated, 0).UTC()
			out = append(out, h)
		}
	}
	if err := crows.Err(); err != nil {
		crows.Close()
		return nil, err
	}
	crows.Close()

	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].ID > out[j].ID
	})
	if len(out) > opts.Limit {
		out = out[:opts.Limit]
	}

	// Best excerpts, only for the conversations being returned.
	for i := range out {
		if !matched[out[i].ID] {
			continue
		}
		erows, err := s.db.Query(
			`SELECT m.role, m.content
			 FROM messages_fts f JOIN messages m ON m.id = f.message_id
			 WHERE messages_fts MATCH ? AND f.conversation_id = ?
			 ORDER BY rank LIMIT ?`,
			match, out[i].ID, opts.ExcerptsPerConv)
		if err != nil {
			return nil, fmt.Errorf("search conversation excerpts: %w", err)
		}
		for erows.Next() {
			var role, content string
			if err := erows.Scan(&role, &content); err != nil {
				erows.Close()
				return nil, fmt.Errorf("scan conversation excerpt: %w", err)
			}
			text := excerptAroundMatch(stripMarkdownForExcerpt(content), terms, opts.ExcerptTokens)
			if r := []rune(text); len(r) > opts.ExcerptMaxRunes {
				text = string(r[:opts.ExcerptMaxRunes]) + "…"
			}
			out[i].Excerpts = append(out[i].Excerpts, assistantConversationExcerpt{Role: role, Text: text})
		}
		if err := erows.Err(); err != nil {
			erows.Close()
			return nil, err
		}
		erows.Close()
	}
	return out, nil
}

// searchTokens splits text into lower-cased, diacritic-folded words the way
// the message index's tokenizer does: a run of letters or digits is a word,
// anything else separates words.
func searchTokens(text string) []string {
	folded := norm.NFD.String(strings.ToLower(text))
	return strings.FieldsFunc(folded, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.Is(unicode.Mn, r)
	})
}

func stripMarks(tokens []string) []string {
	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, strings.Map(func(r rune) rune {
			if unicode.Is(unicode.Mn, r) {
				return -1
			}
			return r
		}, t))
	}
	return out
}

func searchTerms(query string) []string {
	return stripMarks(searchTokens(query))
}

// titleMatchesTerms reports whether every term is a word of title, the last
// term as a prefix - the same rule the message query applies.
func titleMatchesTerms(title string, terms []string) bool {
	if len(terms) == 0 {
		return false
	}
	words := stripMarks(searchTokens(title))
	for i, term := range terms {
		found := false
		for _, w := range words {
			if w == term || (i == len(terms)-1 && strings.HasPrefix(w, term)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

var (
	excerptLinkPattern   = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	excerptLinePrefix    = regexp.MustCompile(`(?m)^[ \t]*(?:#{1,6}[ \t]+|>[ \t]*|[-*+][ \t]+|\d+[.)][ \t]+)`)
	excerptEmphasisChars = strings.NewReplacer("**", "", "__", "", "`", "", "*", "")
	excerptUnderscore    = regexp.MustCompile(`(^|[^\p{L}\p{N}])_+|_+([^\p{L}\p{N}]|$)`)
)

// stripMarkdownForExcerpt turns a message's Markdown into plain prose for a
// search excerpt: links keep their text, emphasis, code ticks, heading marks
// and list markers go, and whitespace collapses.
func stripMarkdownForExcerpt(md string) string {
	t := excerptLinkPattern.ReplaceAllString(md, "$1")
	t = excerptLinePrefix.ReplaceAllString(t, "")
	t = excerptEmphasisChars.Replace(t)
	t = excerptUnderscore.ReplaceAllString(t, "$1$2")
	return strings.Join(strings.Fields(t), " ")
}

// excerptAroundMatch returns about maxWords words of text, starting a few
// words before the first word that matches a search term so the match is
// visible at the start of a short clamp.
func excerptAroundMatch(text string, terms []string, maxWords int) string {
	if maxWords <= 0 {
		maxWords = 16
	}
	words := strings.Fields(text)
	first := 0
	for i, w := range words {
		matched := false
		for _, tok := range stripMarks(searchTokens(w)) {
			for j, term := range terms {
				if tok == term || (j == len(terms)-1 && strings.HasPrefix(tok, term)) {
					matched = true
				}
			}
		}
		if matched {
			first = i
			break
		}
	}
	start := first - 3
	if start < 0 {
		start = 0
	}
	end := start + maxWords
	if end > len(words) {
		end = len(words)
	}
	out := strings.Join(words[start:end], " ")
	if start > 0 {
		out = "…" + out
	}
	if end < len(words) {
		out += "…"
	}
	return out
}
