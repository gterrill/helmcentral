package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Mate's proposals (ADR 0146, generalised by ADR 0158). Mate never writes a
// record: propose_changes validates a changeset and returns it as data, the
// runner saves it with the assistant message that carries it, and the
// operator's Apply tap runs it (ApplyProposal below) through the same
// commands the records' HTTP handlers use. This file is the persistence half;
// changeset.go is the type-agnostic logic and record_type_*.go the types.

const (
	assistantProposalPending   = "pending"
	assistantProposalApplied   = "applied"
	assistantProposalDismissed = "dismissed"
	// assistantProposalStale is a proposal that could no longer be applied as
	// written: a rule changed after Mate proposed it, or an operation failed
	// its validation against the schedule as it now stands. It is stored, with
	// the reason, so a reloaded card and Mate's next turn both know.
	assistantProposalStale = "stale"
)

// The refusals Apply and Dismiss report. All are expected conditions, not
// database failures: assistantProposalErrorStatus maps them to 404/409.
var (
	errAssistantProposalNotFound  = errors.New("proposal not found")
	errAssistantProposalStale     = errors.New("a record changed since Mate proposed this, so nothing was applied")
	errAssistantProposalDismissed = errors.New("this proposal was dismissed and can no longer be applied")
	errAssistantProposalApplied   = errors.New("this proposal was already applied and can no longer be dismissed")
)

// assistantProposal is one card under an assistant message: a validated
// changeset waiting for the operator. Status is pending until the operator
// taps Apply or Dismiss; Result is what Apply did (the record each operation
// wrote) and is what a repeated Apply answers with.
type assistantProposal struct {
	ID        string     `json:"id"`
	MessageID string     `json:"message_id"`
	Ops       []changeOp `json:"ops"`
	Status    string     `json:"status"`
	// StaleReason is why a stale proposal was refused, in words for the card.
	StaleReason string          `json:"stale_reason,omitempty"`
	ResolvedAt  *time.Time      `json:"resolved_at,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

// createAssistantProposalsSchema adds the proposals table to the assistant's
// half of the one business database. Called from createAssistantSchema.
//
// The foreign key to messages is real (ON DELETE CASCADE), unlike
// message_attachments' deliberate absence of one (ADR 0141): a proposal has no
// meaning without the message that carries it, and deleting a conversation
// already deletes its messages.
func createAssistantProposalsSchema(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS assistant_message_proposals (
			id          TEXT PRIMARY KEY,
			message_id  TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
			position    INTEGER NOT NULL,
			ops         TEXT NOT NULL,
			status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'applied', 'dismissed', 'stale')),
			stale_reason TEXT NOT NULL DEFAULT '',
			resolved_at INTEGER,
			result      TEXT NOT NULL DEFAULT '',
			created_at  INTEGER NOT NULL
		)`); err != nil {
		return fmt.Errorf("create assistant_message_proposals table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS assistant_message_proposals_message ON assistant_message_proposals (message_id)`); err != nil {
		return fmt.Errorf("index assistant_message_proposals table: %w", err)
	}
	return convertLegacyProposals(db)
}

const assistantProposalColumns = `id, message_id, ops, status, stale_reason, resolved_at, result, created_at`

func scanAssistantProposal(row rowScanner) (assistantProposal, error) {
	var p assistantProposal
	var opsJSON, result string
	var resolvedAt sql.NullInt64
	var createdAt int64
	if err := row.Scan(&p.ID, &p.MessageID, &opsJSON, &p.Status, &p.StaleReason, &resolvedAt, &result, &createdAt); err != nil {
		return assistantProposal{}, err
	}
	if err := json.Unmarshal([]byte(opsJSON), &p.Ops); err != nil {
		return assistantProposal{}, fmt.Errorf("decode proposal %s ops: %w", p.ID, err)
	}
	if resolvedAt.Valid {
		t := time.Unix(resolvedAt.Int64, 0).UTC()
		p.ResolvedAt = &t
	}
	if result != "" {
		p.Result = json.RawMessage(result)
	}
	p.CreatedAt = time.Unix(createdAt, 0).UTC()
	return p, nil
}

// insertAssistantProposalsTx saves a message's proposals in the transaction
// that saves the message, so a run that fails or is cancelled (which saves no
// message) saves no proposal, and a message is never saved without them. It
// fills in each proposal's MessageID, Status and CreatedAt on the slice it is
// given.
func insertAssistantProposalsTx(tx *sql.Tx, messageID string, proposals []assistantProposal, now time.Time) error {
	for i := range proposals {
		p := &proposals[i]
		if strings.TrimSpace(p.ID) == "" {
			return errors.New("insert proposal: id is required")
		}
		if len(p.Ops) == 0 {
			return fmt.Errorf("insert proposal %s: no operations", p.ID)
		}
		opsJSON, err := json.Marshal(p.Ops)
		if err != nil {
			return fmt.Errorf("encode proposal %s ops: %w", p.ID, err)
		}
		p.MessageID = messageID
		p.Status = assistantProposalPending
		p.ResolvedAt = nil
		p.Result = nil
		p.CreatedAt = now
		if _, err := tx.Exec(
			`INSERT INTO assistant_message_proposals (id, message_id, position, ops, status, resolved_at, result, created_at)
			 VALUES (?, ?, ?, ?, 'pending', NULL, '', ?)`,
			p.ID, messageID, i, string(opsJSON), now.Unix()); err != nil {
			return fmt.Errorf("insert proposal %s: %w", p.ID, err)
		}
	}
	return nil
}

// proposalsForMessages returns every proposal on the given messages, grouped
// by message id and position-ordered, with the status each has right now.
func proposalsForMessages(q sqlQueryer, messageIDs []string) (map[string][]assistantProposal, error) {
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
		`SELECT `+assistantProposalColumns+` FROM assistant_message_proposals
		 WHERE message_id IN (`+strings.Join(placeholders, ",")+`) ORDER BY message_id, position`, args...)
	if err != nil {
		return nil, fmt.Errorf("list message proposals: %w", err)
	}
	defer rows.Close()

	out := map[string][]assistantProposal{}
	for rows.Next() {
		p, err := scanAssistantProposal(rows)
		if err != nil {
			return nil, fmt.Errorf("scan message proposal: %w", err)
		}
		out[p.MessageID] = append(out[p.MessageID], p)
	}
	return out, rows.Err()
}

func assistantProposalByID(q sqlQueryer, id string) (assistantProposal, error) {
	p, err := scanAssistantProposal(q.QueryRow(`SELECT `+assistantProposalColumns+` FROM assistant_message_proposals WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return assistantProposal{}, errAssistantProposalNotFound
	}
	if err != nil {
		return assistantProposal{}, fmt.Errorf("get proposal: %w", err)
	}
	return p, nil
}

// GetProposal reads one proposal with its current status.
func (s *assistantStore) GetProposal(id string) (assistantProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return assistantProposalByID(s.db, id)
}

// DismissProposal marks a pending proposal dismissed. Dismissing a dismissed
// one is a no-op; an applied one cannot be dismissed (its writes happened) and
// neither can a stale one (there is nothing left to dismiss: it can no longer
// be applied, and the card says why).
func (s *assistantStore) DismissProposal(id string) (assistantProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return assistantProposal{}, fmt.Errorf("begin dismiss proposal: %w", err)
	}
	defer tx.Rollback()

	p, err := assistantProposalByID(tx, id)
	if err != nil {
		return assistantProposal{}, err
	}
	switch p.Status {
	case assistantProposalDismissed:
		return p, nil
	case assistantProposalApplied:
		return assistantProposal{}, errAssistantProposalApplied
	case assistantProposalStale:
		return assistantProposal{}, &staleProposalError{reason: p.StaleReason}
	}

	now := s.now()
	if _, err := tx.Exec(`UPDATE assistant_message_proposals SET status = 'dismissed', resolved_at = ? WHERE id = ?`, now.Unix(), id); err != nil {
		return assistantProposal{}, fmt.Errorf("dismiss proposal: %w", err)
	}
	p, err = assistantProposalByID(tx, id)
	if err != nil {
		return assistantProposal{}, err
	}
	if err := tx.Commit(); err != nil {
		return assistantProposal{}, fmt.Errorf("commit dismiss proposal: %w", err)
	}
	return p, nil
}

// ApplyProposal runs a pending proposal's operations, all in one transaction:
// every operation succeeds and the proposal is marked applied, or nothing is
// written at all. Before the first write it checks that every rule an
// operation targets is exactly as it was when Mate proposed (updated_at
// against the snapshot), so a proposal built on a stale picture of the
// schedule can never overwrite a later edit.
//
// When the schedule no longer matches the proposal (that check, an operation
// the commands now refuse, or a target that is gone), the transaction
// rolls back and the proposal is then marked stale, with the reason, in a
// separate transaction. The card and Mate's next turn both see that; Apply can
// never be offered again for something that can only fail the same way.
//
// An already-applied proposal returns its stored result and writes nothing, so
// a double tap or a retried request is safe; a dismissed one is refused, and a
// stale one answers with its stored reason.
//
// The transaction is opened on this store's connection but writes the
// maintenance tables, which live in the same database file (ADR 0141). That is
// what makes "the operations and the applied mark commit together" true; the
// commands read only through tx, never through globalDocumentStore, whose
// separate connection would not see the transaction's own writes.
func (s *assistantStore) ApplyProposal(id string, today time.Time) (assistantProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, err := s.applyProposalTx(id, today)
	if err == nil || !proposalGoesStale(err) {
		return p, err
	}
	stale := &staleProposalError{reason: err.Error()}
	if _, uerr := s.db.Exec(
		`UPDATE assistant_message_proposals SET status = 'stale', stale_reason = ?, resolved_at = ? WHERE id = ? AND status = 'pending'`,
		stale.reason, s.now().Unix(), id); uerr != nil {
		return assistantProposal{}, fmt.Errorf("mark proposal stale: %w (after: %v)", uerr, err)
	}
	return assistantProposal{}, stale
}

// applyProposalTx is ApplyProposal's transaction; the caller holds s.mu.
func (s *assistantStore) applyProposalTx(id string, today time.Time) (assistantProposal, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return assistantProposal{}, fmt.Errorf("begin apply proposal: %w", err)
	}
	defer tx.Rollback()

	p, err := assistantProposalByID(tx, id)
	if err != nil {
		return assistantProposal{}, err
	}
	switch p.Status {
	case assistantProposalApplied:
		return p, nil
	case assistantProposalDismissed:
		return assistantProposal{}, errAssistantProposalDismissed
	case assistantProposalStale:
		return assistantProposal{}, &staleProposalError{reason: p.StaleReason}
	}

	now := s.now()
	results, err := defaultRecordRegistry.apply(tx, now, today, p.Ops)
	if err != nil {
		var oerr *changeOpError
		if errors.As(err, &oerr) {
			// The operator reads which change failed and why; the original
			// error stays in the chain for proposalGoesStale.
			return assistantProposal{}, &appliedOpError{msg: oerr.applyMessage(len(p.Ops)), cause: err}
		}
		return assistantProposal{}, err
	}
	resultJSON, err := json.Marshal(changeResult{Ops: results})
	if err != nil {
		return assistantProposal{}, fmt.Errorf("encode proposal result: %w", err)
	}
	if _, err := tx.Exec(`UPDATE assistant_message_proposals SET status = 'applied', resolved_at = ?, result = ? WHERE id = ?`,
		now.Unix(), string(resultJSON), id); err != nil {
		return assistantProposal{}, fmt.Errorf("mark proposal applied: %w", err)
	}
	p, err = assistantProposalByID(tx, id)
	if err != nil {
		return assistantProposal{}, err
	}
	if err := tx.Commit(); err != nil {
		return assistantProposal{}, fmt.Errorf("commit apply proposal: %w", err)
	}
	defaultRecordRegistry.afterCommit(results)
	return p, nil
}

// appliedOpError is a failed operation of an Apply, worded for the card and
// keeping the underlying error for classification.
type appliedOpError struct {
	msg   string
	cause error
}

func (e *appliedOpError) Error() string { return e.msg }
func (e *appliedOpError) Unwrap() error { return e.cause }

// staleProposalError is a stale refusal carrying its stored or freshly worked
// out reason. It matches errAssistantProposalStale with errors.Is.
type staleProposalError struct{ reason string }

func (e *staleProposalError) Error() string        { return e.reason }
func (e *staleProposalError) Is(target error) bool { return target == errAssistantProposalStale }

// proposalGoesStale reports whether err means the records no longer match
// what the proposal was written against, so the proposal should be marked
// stale rather than left offering an Apply that can only fail again: the
// freshness check itself, an operation the commands now refuse (validation or
// a refusal), or a target that no longer exists. A database failure is not
// one.
func proposalGoesStale(err error) bool {
	var verr *inventoryValidationError
	var cerr *maintenanceCommandError
	var ferr *fieldedError
	var inUse *inventoryInUseError
	return errors.Is(err, errAssistantProposalStale) || errors.As(err, &verr) || errors.As(err, &cerr) ||
		errors.As(err, &ferr) || errors.As(err, &inUse) ||
		errors.Is(err, errMaintenanceRuleNotFound) || errors.Is(err, errEquipmentNotFound) || errors.Is(err, errRecordNotFound)
}

// assistantProposalErrorStatus maps the proposal sentinels to the status Apply
// and Dismiss answer with.
func assistantProposalErrorStatus(err error) (int, bool) {
	switch {
	case errors.Is(err, errAssistantProposalNotFound):
		return http.StatusNotFound, true
	case errors.Is(err, errAssistantProposalStale),
		errors.Is(err, errAssistantProposalDismissed),
		errors.Is(err, errAssistantProposalApplied):
		return http.StatusConflict, true
	}
	return 0, false
}
