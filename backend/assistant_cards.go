package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// mateCard is one card under an assistant message (ADR 0191). Unlike
// proposals and form drafts it has no state of its own, so every kind shares
// one table: Kind names the shape of Payload, which the stored row keeps
// verbatim.
//
// A passage_window card also carries what the operator did with it (ADR
// 0211): ID is the card row, Decision the recorded outcome. routes is the
// tool's route points per leg, which only the server keeps, so it has no JSON
// tag and never leaves the process except into passage_decisions.
type mateCard struct {
	ID       int64                `json:"id,omitempty"`
	Kind     string               `json:"kind"`
	Payload  json.RawMessage      `json:"payload"`
	Decision *passageDecisionView `json:"decision,omitempty"`
	routes   [][]passageRoutePoint
	// created is when the tool built the card, which is when the operator saw
	// the forecast, not when the reply was saved.
	created time.Time
}

func createAssistantCardsSchema(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS assistant_message_cards (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id   TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
			ordinal      INTEGER NOT NULL,
			kind         TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			created_at   INTEGER NOT NULL
		)`); err != nil {
		return fmt.Errorf("create assistant_message_cards table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS assistant_message_cards_message ON assistant_message_cards (message_id, ordinal)`); err != nil {
		return fmt.Errorf("index assistant_message_cards table: %w", err)
	}
	return nil
}

// insertAssistantCardsTx saves a reply's cards in the transaction that saves
// the message, writing each row id back into cards. A card with no kind or an
// invalid payload is refused rather than stored, and so is a passage_window
// card whose routes are missing or do not match its legs: its blank decision
// is inserted in the same transaction (ADR 0211).
func insertAssistantCardsTx(tx *sql.Tx, conversationID, messageID string, cards []mateCard, now time.Time) error {
	for i, c := range cards {
		if strings.TrimSpace(c.Kind) == "" {
			return fmt.Errorf("insert message card %d: kind is required", i)
		}
		if !json.Valid(c.Payload) {
			return fmt.Errorf("insert message card %d (%s): payload is not valid JSON", i, c.Kind)
		}
		var routesJSON []byte
		if c.Kind == passageWindowCardKind {
			var err error
			if routesJSON, err = passageWindowRoutesJSON(c); err != nil {
				return fmt.Errorf("insert message card %d (%s): %w", i, c.Kind, err)
			}
		}
		res, err := tx.Exec(
			`INSERT INTO assistant_message_cards (message_id, ordinal, kind, payload_json, created_at) VALUES (?, ?, ?, ?, ?)`,
			messageID, i, c.Kind, string(c.Payload), now.Unix())
		if err != nil {
			return fmt.Errorf("insert message card %d (%s): %w", i, c.Kind, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("insert message card %d (%s): %w", i, c.Kind, err)
		}
		cards[i].ID = id
		if routesJSON != nil {
			view, err := insertBlankPassageDecisionTx(tx, id, messageID, conversationID, c.Payload, routesJSON, c.created)
			if err != nil {
				return fmt.Errorf("insert message card %d (%s): %w", i, c.Kind, err)
			}
			cards[i].Decision = &view
		}
	}
	return nil
}

func cardsForMessages(q sqlQueryer, messageIDs []string) (map[string][]mateCard, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	marks, args := inPlaceholders(messageIDs)
	rows, err := q.Query(`SELECT c.message_id, c.id, c.kind, c.payload_json, d.id, d.state, d.row_date, d.source, d.passage_id
		FROM assistant_message_cards c LEFT JOIN passage_decisions d ON d.card_id = c.id
		WHERE c.message_id IN (`+marks+`) ORDER BY c.message_id, c.ordinal`, args...)
	if err != nil {
		return nil, fmt.Errorf("list message cards: %w", err)
	}
	defer rows.Close()
	out := map[string][]mateCard{}
	for rows.Next() {
		var messageID, payload string
		var c mateCard
		var dID sql.NullInt64
		var dState, dSource, dRow, dPassage sql.NullString
		if err := rows.Scan(&messageID, &c.ID, &c.Kind, &payload, &dID, &dState, &dRow, &dSource, &dPassage); err != nil {
			return nil, fmt.Errorf("scan message card: %w", err)
		}
		if dID.Valid {
			c.Decision = newPassageDecisionView(dID.Int64, dState.String, dRow, dSource.String, dPassage)
		}
		c.Payload = json.RawMessage(payload)
		out[messageID] = append(out[messageID], c)
	}
	return out, rows.Err()
}
