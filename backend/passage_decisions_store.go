package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// A passage_decisions row records what the operator did with one
// passage_window card: sailed a row's date, stayed, or was only looking
// (ADR 0211). It keeps a verbatim copy of the card's payload and the route
// points the tool was given, and holds no foreign key, so it outlives the chat
// that made it.

const passageWindowCardKind = "passage_window"

var (
	errPassageDecisionNotFound = errors.New("passage decision not found")
	// errPassageDecisionInvalid wraps every refusal that is the caller's
	// mistake rather than the store's.
	errPassageDecisionInvalid = errors.New("invalid passage decision")
)

// passageDecisionView is the decision as the client sees it.
type passageDecisionView struct {
	ID        int64   `json:"id"`
	State     string  `json:"state"`
	RowDate   *string `json:"row_date"`
	Inferred  bool    `json:"inferred"`
	PassageID *string `json:"passage_id"`
}

func newPassageDecisionView(id int64, state string, rowDate sql.NullString, source string, passageID sql.NullString) *passageDecisionView {
	v := &passageDecisionView{ID: id, State: state, Inferred: source == "inferred"}
	if rowDate.Valid {
		v.RowDate = &rowDate.String
	}
	if passageID.Valid {
		v.PassageID = &passageID.String
	}
	return v
}

func createPassageDecisionsSchema(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS passage_decisions (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			card_id         INTEGER NOT NULL UNIQUE,
			message_id      TEXT NOT NULL,
			conversation_id TEXT NOT NULL,
			card_created_ms INTEGER NOT NULL,
			payload_json    TEXT NOT NULL,
			routes_json     TEXT NOT NULL,
			state           TEXT NOT NULL DEFAULT '' CHECK (state IN ('', 'went', 'stayed', 'just_looking')),
			row_date        TEXT,
			source          TEXT NOT NULL DEFAULT '' CHECK (source IN ('', 'inferred', 'operator')),
			passage_id      TEXT,
			updated_ms      INTEGER NOT NULL,
			CHECK ((state = 'went') = (row_date IS NOT NULL)),
			CHECK (state = '' OR source <> '')
		)`); err != nil {
		return fmt.Errorf("create passage_decisions table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS passage_decisions_passage ON passage_decisions (passage_id)`); err != nil {
		return fmt.Errorf("index passage_decisions table: %w", err)
	}
	return nil
}

// passageWindowRoutesJSON checks that a passage_window card carries one route
// of at least two points per leg of its payload, and returns them encoded.
// Without them the card could never be matched to a passage, so it is
// refused rather than stored half-usable.
func passageWindowRoutesJSON(c mateCard) ([]byte, error) {
	if c.created.IsZero() {
		return nil, fmt.Errorf("passage_window card has no creation time")
	}
	var p struct {
		Legs []json.RawMessage `json:"legs"`
	}
	if err := json.Unmarshal(c.Payload, &p); err != nil {
		return nil, fmt.Errorf("passage_window payload: %w", err)
	}
	if len(c.routes) == 0 || len(c.routes) != len(p.Legs) {
		return nil, fmt.Errorf("passage_window routes: %d routes for %d legs", len(c.routes), len(p.Legs))
	}
	for i, r := range c.routes {
		if len(r) < 2 {
			return nil, fmt.Errorf("passage_window routes: leg %d has %d points", i, len(r))
		}
	}
	return json.Marshal(c.routes)
}

func insertBlankPassageDecisionTx(tx *sql.Tx, cardID int64, messageID, conversationID string, payload json.RawMessage, routesJSON []byte, created time.Time) (passageDecisionView, error) {
	res, err := tx.Exec(
		`INSERT INTO passage_decisions (card_id, message_id, conversation_id, card_created_ms, payload_json, routes_json, updated_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		cardID, messageID, conversationID, created.UnixMilli(), string(payload), string(routesJSON), created.UnixMilli())
	if err != nil {
		return passageDecisionView{}, fmt.Errorf("insert passage decision: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return passageDecisionView{}, fmt.Errorf("insert passage decision: %w", err)
	}
	return passageDecisionView{ID: id}, nil
}

// SetOperatorPassageDecision records the operator's choice. state "" is a
// deliberate blank that inference leaves alone. A went row_date must be one of
// the dates on the card the operator saw. The passage link is cleared here
// and derived afterwards (ADR 0211).
func (s *assistantStore) SetOperatorPassageDecision(id int64, state string, rowDate *string) (passageDecisionView, error) {
	invalid := func(format string, v ...any) error {
		return fmt.Errorf("%w: "+format, append([]any{errPassageDecisionInvalid}, v...)...)
	}
	switch state {
	case "", "stayed", "just_looking":
		if rowDate != nil {
			return passageDecisionView{}, invalid("row_date only goes with state went")
		}
	case "went":
		if rowDate == nil {
			return passageDecisionView{}, invalid("state went needs a row_date")
		}
	default:
		return passageDecisionView{}, invalid("unknown state %q", state)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return passageDecisionView{}, fmt.Errorf("begin passage decision update: %w", err)
	}
	defer tx.Rollback()

	var payloadJSON string
	err = tx.QueryRow(`SELECT payload_json FROM passage_decisions WHERE id = ?`, id).Scan(&payloadJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return passageDecisionView{}, errPassageDecisionNotFound
	}
	if err != nil {
		return passageDecisionView{}, fmt.Errorf("read passage decision: %w", err)
	}
	if rowDate != nil {
		var p passageWindowPayload
		if err := json.Unmarshal([]byte(payloadJSON), &p); err != nil {
			return passageDecisionView{}, fmt.Errorf("read passage decision payload: %w", err)
		}
		found := false
		for _, r := range p.Rows {
			if r.DepartDate == *rowDate {
				found = true
				break
			}
		}
		if !found {
			return passageDecisionView{}, invalid("row_date %q is not a date on the card", *rowDate)
		}
	}
	var rd any
	if rowDate != nil {
		rd = *rowDate
	}
	res, err := tx.Exec(
		`UPDATE passage_decisions SET state = ?, row_date = ?, source = 'operator', passage_id = NULL, updated_ms = ? WHERE id = ?`,
		state, rd, s.now().UnixMilli(), id)
	if err != nil {
		return passageDecisionView{}, fmt.Errorf("update passage decision: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return passageDecisionView{}, fmt.Errorf("update passage decision: %w", err)
	} else if n != 1 {
		return passageDecisionView{}, errPassageDecisionNotFound
	}
	if err := tx.Commit(); err != nil {
		return passageDecisionView{}, fmt.Errorf("commit passage decision update: %w", err)
	}
	return passageDecisionView{ID: id, State: state, RowDate: rowDate}, nil
}
