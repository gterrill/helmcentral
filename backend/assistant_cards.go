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
type mateCard struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
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
// the message. A card with no kind or an invalid payload is refused rather
// than stored.
func insertAssistantCardsTx(tx *sql.Tx, messageID string, cards []mateCard, now time.Time) error {
	for i, c := range cards {
		if strings.TrimSpace(c.Kind) == "" {
			return fmt.Errorf("insert message card %d: kind is required", i)
		}
		if !json.Valid(c.Payload) {
			return fmt.Errorf("insert message card %d (%s): payload is not valid JSON", i, c.Kind)
		}
		if _, err := tx.Exec(
			`INSERT INTO assistant_message_cards (message_id, ordinal, kind, payload_json, created_at) VALUES (?, ?, ?, ?, ?)`,
			messageID, i, c.Kind, string(c.Payload), now.Unix()); err != nil {
			return fmt.Errorf("insert message card %d (%s): %w", i, c.Kind, err)
		}
	}
	return nil
}

func cardsForMessages(q sqlQueryer, messageIDs []string) (map[string][]mateCard, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(messageIDs))
	args := make([]any, len(messageIDs))
	for i, id := range messageIDs {
		placeholders[i], args[i] = "?", id
	}
	rows, err := q.Query(`SELECT message_id, kind, payload_json FROM assistant_message_cards WHERE message_id IN (`+strings.Join(placeholders, ",")+`) ORDER BY message_id, ordinal`, args...)
	if err != nil {
		return nil, fmt.Errorf("list message cards: %w", err)
	}
	defer rows.Close()
	out := map[string][]mateCard{}
	for rows.Next() {
		var messageID, payload string
		var c mateCard
		if err := rows.Scan(&messageID, &c.Kind, &payload); err != nil {
			return nil, fmt.Errorf("scan message card: %w", err)
		}
		c.Payload = json.RawMessage(payload)
		out[messageID] = append(out[messageID], c)
	}
	return out, rows.Err()
}
