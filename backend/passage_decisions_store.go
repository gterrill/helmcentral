package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

func newPassageDecisionView(id int64, state string, rowDate *string, source string, passageID *string) *passageDecisionView {
	return &passageDecisionView{ID: id, State: state, RowDate: rowDate, Inferred: source == "inferred", PassageID: passageID}
}

func nullStringPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	return &n.String
}

// view is the row as the client sees it; empty strings are NULLs.
func (r passageDecisionRow) view() *passageDecisionView {
	var rowDate, passageID *string
	if r.RowDate != "" {
		rowDate = &r.RowDate
	}
	if r.PassageID != "" {
		passageID = &r.PassageID
	}
	return newPassageDecisionView(r.ID, r.State, rowDate, r.Source, passageID)
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

// passageDecisionRow is a stored decision as the reconcile reads it. RowDate
// and PassageID are empty for NULL.
type passageDecisionRow struct {
	ID            int64
	CardCreatedMs int64
	PayloadJSON   string
	RoutesJSON    string
	State         string
	RowDate       string
	Source        string
	PassageID     string
}

const passageDecisionRowColumns = `id, card_created_ms, payload_json, routes_json, state, COALESCE(row_date, ''), source, COALESCE(passage_id, '')`

func scanPassageDecisionRow(sc interface{ Scan(...any) error }) (passageDecisionRow, error) {
	var r passageDecisionRow
	err := sc.Scan(&r.ID, &r.CardCreatedMs, &r.PayloadJSON, &r.RoutesJSON, &r.State, &r.RowDate, &r.Source, &r.PassageID)
	return r, err
}

// PassageDecisionRow returns one decision, or errPassageDecisionNotFound.
func (s *assistantStore) PassageDecisionRow(id int64) (passageDecisionRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := scanPassageDecisionRow(s.db.QueryRow(`SELECT `+passageDecisionRowColumns+` FROM passage_decisions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, errPassageDecisionNotFound
	}
	if err != nil {
		return r, fmt.Errorf("read passage decision: %w", err)
	}
	return r, nil
}

// PassageDecisionsForInference returns the decisions a reconcile may look at:
// every one whose card was made at or after since, plus any older one that
// holds a passage link. The caller narrows these by row date.
func (s *assistantStore) PassageDecisionsForInference(since time.Time, include []int64) ([]passageDecisionRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	where, args := `card_created_ms >= ? OR passage_id IS NOT NULL`, []any{since.UnixMilli()}
	if len(include) > 0 {
		marks := strings.TrimSuffix(strings.Repeat("?,", len(include)), ",")
		where += ` OR id IN (` + marks + `)`
		for _, id := range include {
			args = append(args, id)
		}
	}
	rows, err := s.db.Query(`SELECT `+passageDecisionRowColumns+` FROM passage_decisions WHERE `+where+` ORDER BY card_created_ms, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list passage decisions: %w", err)
	}
	defer rows.Close()
	var out []passageDecisionRow
	for rows.Next() {
		r, err := scanPassageDecisionRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan passage decision: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ApplyPassageDecisionUpdates writes a reconcile's answer and reports how many
// rows changed. An inferred write is guarded by source <> 'operator', so an
// operator answer that landed after the read always stands. A link write
// touches only an operator went that still has the state and date it was
// computed for.
func (s *assistantStore) ApplyPassageDecisionUpdates(ups []decisionUpdate) (int, error) {
	if len(ups) == 0 {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin passage decision updates: %w", err)
	}
	defer tx.Rollback()
	now := s.now().UnixMilli()
	changed := 0
	for _, u := range ups {
		var res sql.Result
		var pid, rd any
		if u.PassageID != "" {
			pid = u.PassageID
		}
		if u.RowDate != "" {
			rd = u.RowDate
		}
		if u.Link {
			res, err = tx.Exec(`UPDATE passage_decisions SET passage_id = ?, updated_ms = ?
				WHERE id = ? AND source = 'operator' AND state = ? AND COALESCE(row_date, '') = ?`,
				pid, now, u.ID, u.State, u.RowDate)
		} else {
			source := ""
			if u.State != "" {
				source = "inferred"
			}
			res, err = tx.Exec(`UPDATE passage_decisions SET state = ?, row_date = ?, source = ?, passage_id = ?, updated_ms = ?
				WHERE id = ? AND source <> 'operator'`,
				u.State, rd, source, pid, now, u.ID)
		}
		if err != nil {
			return 0, fmt.Errorf("update passage decision %d: %w", u.ID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("update passage decision %d: %w", u.ID, err)
		}
		changed += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit passage decision updates: %w", err)
	}
	return changed, nil
}

// passagePlan is one Mate plan a passage was sailed from, as the passage page
// reads it: the decision, the legs and row the operator chose, and whether the
// chat that made the card still exists.
type passagePlan struct {
	Decision       *passageDecisionView `json:"decision"`
	Legs           []passageWindowLeg   `json:"legs"`
	RowVerdict     *string              `json:"row_verdict"`
	Timezone       string               `json:"timezone"`
	ConversationID string               `json:"conversation_id"`
	ChatExists     bool                 `json:"chat_exists"`
}

// PassagePlans returns the decisions linked to a passage, oldest card first.
// A stored payload that cannot be read is an error, never a skipped entry.
func (s *assistantStore) PassagePlans(passageID string) ([]passagePlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT d.id, d.card_created_ms, d.payload_json, d.state, COALESCE(d.row_date, ''), d.source, COALESCE(d.passage_id, ''), d.conversation_id, c.id IS NOT NULL
		FROM passage_decisions d LEFT JOIN conversations c ON c.id = d.conversation_id
		WHERE d.passage_id = ? ORDER BY d.card_created_ms, d.id`, passageID)
	if err != nil {
		return nil, fmt.Errorf("list plans for passage: %w", err)
	}
	defer rows.Close()
	out := []passagePlan{}
	for rows.Next() {
		var r passageDecisionRow
		var plan passagePlan
		if err := rows.Scan(&r.ID, &r.CardCreatedMs, &r.PayloadJSON, &r.State, &r.RowDate, &r.Source, &r.PassageID, &plan.ConversationID, &plan.ChatExists); err != nil {
			return nil, fmt.Errorf("scan plan for passage: %w", err)
		}
		var p passageWindowPayload
		if err := json.Unmarshal([]byte(r.PayloadJSON), &p); err != nil {
			return nil, fmt.Errorf("plan %d payload: %w", r.ID, err)
		}
		plan.Decision = r.view()
		plan.Legs, plan.Timezone = p.Legs, p.Timezone
		if plan.Legs == nil {
			plan.Legs = []passageWindowLeg{}
		}
		if r.RowDate != "" {
			found := false
			for _, row := range p.Rows {
				if row.DepartDate == r.RowDate {
					v := row.Verdict
					plan.RowVerdict, found = &v, true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("plan %d: row date %s is not on its card", r.ID, r.RowDate)
			}
		}
		out = append(out, plan)
	}
	return out, rows.Err()
}
