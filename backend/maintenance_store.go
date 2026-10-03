package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// This file is the maintenance feature's own scoped half of documentStore
// (documents_store.go, schema in the "Maintenance (ADR 0138)" section) -
// service rules, the log they're completed into, and the small hour-meter
// history that lets true hours survive a meter replacement. Same per-topic
// file split as inventory_store.go/notes_store.go/manuals_store.go, and the
// same idioms throughout: a mutex-guarded single connection, a transaction
// per write, sentinel errors checked with errors.Is by
// documentErrorStatus (documents_handlers.go).
//
// What this file does NOT do: decide whether a rule is due. That is
// maintenance_status.go's job, a pure function with no store dependency -
// this file only ever stores and reads back what a rule and a log entry
// actually say, in exactly the shape they were given.

var (
	errMaintenanceRuleNotFound     = errors.New("maintenance rule not found")
	errMaintenanceLogEntryNotFound = errors.New("log entry not found")
)

// RunTx runs fn in one transaction on this store's connection,
// holding the store mutex for its duration, and commits only when fn
// returns nil. now is the store's own clock. Every maintenance and inventory
// write the HTTP handlers make goes through it, so a handler and a Mate
// changeset (ADR 0158), which runs several of the same commands in one
// transaction of its own, execute identical code. fn must not call another
// documentStore method: they take the same mutex.
func (s *documentStore) RunTx(fn func(tx *sql.Tx, now time.Time) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("maintenance write: begin: %w", err)
	}
	defer tx.Rollback()

	if err := fn(tx, s.now()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("maintenance write: commit: %w", err)
	}
	return nil
}

// ── types ────────────────────────────────────────────────────────────────

// maintenanceRule is one row of maintenance_rules. EquipmentID is nil for a
// calendar-only rule with no item (spec §7 - certificates and expiries).
// FixedDueDate/LastDoneAt are "" when unset, matching every other
// YYYY-MM-DD date column in this codebase (equipment.install_date) rather
// than a nullable time.Time - a bare date has no time-of-day or timezone to
// carry, and "" prints as nothing rather than a zero-value date if a caller
// forgets to check it.
type maintenanceRule struct {
	ID               string   `json:"id"`
	EquipmentID      *string  `json:"equipment_id"`
	Description      string   `json:"description"`
	IntervalHours    *float64 `json:"interval_hours"`
	IntervalMonths   *int     `json:"interval_months"`
	DueSoonHours     *float64 `json:"due_soon_hours"`
	DueSoonMonths    *int     `json:"due_soon_months"`
	FixedDueDate     string   `json:"fixed_due_date"`
	LastDoneAt       string   `json:"last_done_at"`
	LastDoneHours    *float64 `json:"last_done_hours"`
	ProfileServiceID string   `json:"profile_service_id"`
	ProcedureNoteID  string   `json:"procedure_note_id"`
	AckReason        string   `json:"ack_reason"`
	// OverriddenFields names the fields of a profile job this item overrides,
	// and NotApplicable is meaningful only when it is one of them. Both are
	// always empty on a hand rule. On a job row the interval and description
	// columns are NOT effective values (maintenance_schedule.go).
	OverriddenFields []string `json:"overridden_fields"`
	NotApplicable    bool     `json:"not_applicable"`
	// Acknowledged is derived from ack_at (NULL means unacknowledged) -
	// exposed as a bool rather than making every caller compare a
	// timestamp to the zero value.
	Acknowledged bool      `json:"acknowledged"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// maintenanceLogPart is one row of maintenance_log_parts, joined against
// equipment for its display name - a parts list that only carried ids would
// make the CSV export and the log view useless without a second lookup.
//
// EquipmentID is nil once the part item itself has been deleted
// (equipment_id ON DELETE SET NULL, documents_store.go's 2026-09-27
// amendment) - the row survives as pure history. EquipmentName is always
// populated either way: the equipment's own CURRENT name while still
// linked, or the part_name snapshot taken at logging time once it isn't
// (maintenancePartsForLogEntries' own COALESCE).
type maintenanceLogPart struct {
	EquipmentID   *string `json:"equipment_id"`
	EquipmentName string  `json:"equipment_name"`
	Quantity      float64 `json:"quantity"`
}

// maintenanceLogEntry is one row of maintenance_log_entries plus its joined
// parts and photo ids - GetMaintenanceLogEntry/ListMaintenanceLogEntries
// always populate both, never lazily (the same "always the keys" contract
// equipmentItem's own doc comment follows for PhotoIDs).
type maintenanceLogEntry struct {
	ID          string               `json:"id"`
	EquipmentID *string              `json:"equipment_id"`
	RuleID      *string              `json:"rule_id"`
	PerformedAt string               `json:"performed_at"`
	Hours       *float64             `json:"hours"`
	Kind        string               `json:"kind"`
	Description string               `json:"description"`
	Who         string               `json:"who"`
	Cost        *float64             `json:"cost"`
	Currency    string               `json:"currency"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
	Parts       []maintenanceLogPart `json:"parts"`
	PhotoIDs    []string             `json:"photo_ids"`
}

var validMaintenanceLogKinds = map[string]bool{"maintenance": true, "repair": true, "improvement": true}

// maintenanceRuleInput/maintenanceLogEntryInput are the editable-field
// shapes CreateMaintenanceRule/UpdateMaintenanceRule and
// CreateMaintenanceLogEntry/UpdateMaintenanceLogEntry accept - a PUT-style
// whole-record replace of a rule's own CORE fields, matching equipmentItem's
// own CreateEquipment/UpdateEquipment contract. Server-side field validation
// (validateMaintenanceRuleInput/validateMaintenanceLogEntryInput,
// maintenance_handlers.go) runs before either of these ever sees a value.
//
// Deliberately excluded, the same way equipment's own PUT excludes photos/
// documents (each has its own dedicated, separately-validated endpoint):
// LastDoneAt/LastDoneHours (SetMaintenanceRuleLastDone/
// CompleteMaintenanceRule only), ProcedureNoteID
// (SetMaintenanceRuleProcedureNote only), and ack_reason/ack_at
// (AcknowledgeMaintenanceRule only). An ordinary rule edit - fixing a typo
// in the description, widening an interval - must never accidentally reset
// a baseline, a linked note, or an acknowledgement as a side effect of
// saving the form.
type maintenanceRuleInput struct {
	EquipmentID    *string
	Description    string
	IntervalHours  *float64
	IntervalMonths *int
	DueSoonHours   *float64
	DueSoonMonths  *int
	FixedDueDate   string
}

type maintenanceLogPartInput struct {
	EquipmentID string
	Quantity    float64
}

type maintenanceLogEntryInput struct {
	EquipmentID *string
	PerformedAt string
	Hours       *float64
	Kind        string
	Description string
	Who         string
	Cost        *float64
	Currency    string
	Parts       []maintenanceLogPartInput
}

// maintenanceRuleFilter is ListMaintenanceRules' input. EquipmentID narrows
// to one item's own rules (the equipment editor's Maintenance block);
// blank lists every rule (the Maintenance section's own list), including
// every calendar-only, no-equipment rule. IncludeStored, false by default,
// controls whether a rule belonging to a stored item is included - the
// list handler filters status='stored' items out (spec §4: "Items with
// status stored are excluded from the list"); the equipment editor's own
// Maintenance block passes IncludeStored=true, since an operator editing a
// stored item's own page obviously still wants to see and manage its rules.
//
// System (2026-09-27, code-review finding), blank by default, narrows to
// one system's own gear - the Maintenance list's own filter dropdown. A
// calendar-only rule has no system to match against and is excluded
// whenever System is non-blank, the same as IncludeStored's own
// equipment-status subquery excludes a stored item's rule: filtered in SQL,
// not by the caller loading every rule's own equipment first and
// discarding the ones that don't match.
type maintenanceRuleFilter struct {
	EquipmentID   string
	IncludeStored bool
	System        string
}

// ── maintenance rules ────────────────────────────────────────────────────

const maintenanceRuleColumns = `id, equipment_id, description, interval_hours, interval_months,
	due_soon_hours, due_soon_months, fixed_due_date, last_done_at, last_done_hours,
	profile_service_id, procedure_note_id, ack_reason, ack_at, created_at, updated_at,
	overridden_fields, not_applicable`

func scanMaintenanceRule(row rowScanner) (maintenanceRule, error) {
	var r maintenanceRule
	var equipmentID, procedureNoteID sql.NullString
	var ackAt sql.NullInt64
	var createdAt, updatedAt int64
	var overridden string
	var notApplicable int

	if err := row.Scan(
		&r.ID, &equipmentID, &r.Description, &r.IntervalHours, &r.IntervalMonths,
		&r.DueSoonHours, &r.DueSoonMonths, &r.FixedDueDate, &r.LastDoneAt, &r.LastDoneHours,
		&r.ProfileServiceID, &procedureNoteID, &r.AckReason, &ackAt, &createdAt, &updatedAt,
		&overridden, &notApplicable,
	); err != nil {
		return maintenanceRule{}, err
	}
	r.OverriddenFields = decodeOverriddenFields(overridden)
	r.NotApplicable = notApplicable != 0
	if equipmentID.Valid {
		v := equipmentID.String
		r.EquipmentID = &v
	}
	if procedureNoteID.Valid {
		r.ProcedureNoteID = procedureNoteID.String
	}
	r.Acknowledged = ackAt.Valid
	r.CreatedAt = time.Unix(createdAt, 0).UTC()
	r.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return r, nil
}

func maintenanceRuleByID(q sqlQueryer, id string) (maintenanceRule, error) {
	row := q.QueryRow(`SELECT `+maintenanceRuleColumns+` FROM maintenance_rules WHERE id = ?`, id)
	r, err := scanMaintenanceRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return maintenanceRule{}, errMaintenanceRuleNotFound
	}
	if err != nil {
		return maintenanceRule{}, fmt.Errorf("get maintenance rule: %w", err)
	}
	return r, nil
}

// CreateMaintenanceRule inserts a rule by hand (spec §1's "Rules can also
// be added by hand" path - CopyProfileServiceEntries below is the other
// one). Field-level validation (at least one interval, etc.) is the
// handler's job, matching validateEquipmentInput/CreateEquipment's own
// split; this method only resolves EquipmentID (must exist if given) before
// writing.
func (s *documentStore) CreateMaintenanceRule(in maintenanceRuleInput) (maintenanceRule, error) {
	var out0 maintenanceRule
	err := s.RunTx(func(tx *sql.Tx, now time.Time) error {
		var err error
		out0, err = createMaintenanceRuleTx(tx, now, in)
		return err
	})
	if err != nil {
		return maintenanceRule{}, err
	}
	return out0, nil
}

// createMaintenanceRuleTx is CreateMaintenanceRule inside a transaction the caller owns and commits, with now
// supplied by the caller, so the HTTP handler and the Mate proposal apply run
// the same write (see RunTx).
func createMaintenanceRuleTx(tx *sql.Tx, now time.Time, in maintenanceRuleInput) (maintenanceRule, error) {

	if in.EquipmentID != nil {
		ok, err := rowExists(tx, `SELECT 1 FROM equipment WHERE id = ?`, *in.EquipmentID)
		if err != nil {
			return maintenanceRule{}, fmt.Errorf("create maintenance rule: check equipment: %w", err)
		}
		if !ok {
			return maintenanceRule{}, errEquipmentNotFound
		}
	}

	id := uuid.NewString()
	if _, err := tx.Exec(`
		INSERT INTO maintenance_rules (
			id, equipment_id, description, interval_hours, interval_months,
			due_soon_hours, due_soon_months, fixed_due_date, last_done_at, last_done_hours,
			profile_service_id, procedure_note_id, ack_reason, ack_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', NULL, '', NULL, '', NULL, ?, ?)`,
		id, nullableString(in.EquipmentID), strings.TrimSpace(in.Description), in.IntervalHours, in.IntervalMonths,
		in.DueSoonHours, in.DueSoonMonths, in.FixedDueDate,
		now.Unix(), now.Unix(),
	); err != nil {
		return maintenanceRule{}, fmt.Errorf("create maintenance rule: %w", err)
	}

	created, err := maintenanceRuleByID(tx, id)
	if err != nil {
		return maintenanceRule{}, err
	}
	return created, nil
}

// UpdateMaintenanceRule replaces id's whole editable field set - a PUT, not
// a PATCH, matching UpdateEquipment's own contract. It deliberately never
// touches ack_reason/ack_at (AcknowledgeMaintenanceRule's own job) or
// created_at.
func (s *documentStore) UpdateMaintenanceRule(id string, in maintenanceRuleInput) (maintenanceRule, error) {
	var out0 maintenanceRule
	err := s.RunTx(func(tx *sql.Tx, now time.Time) error {
		var err error
		out0, err = updateMaintenanceRuleTx(tx, now, id, in)
		return err
	})
	if err != nil {
		return maintenanceRule{}, err
	}
	return out0, nil
}

// updateMaintenanceRuleTx is UpdateMaintenanceRule inside a transaction the caller owns and commits, with now
// supplied by the caller, so the HTTP handler and the Mate proposal apply run
// the same write (see RunTx).
func updateMaintenanceRuleTx(tx *sql.Tx, now time.Time, id string, in maintenanceRuleInput) (maintenanceRule, error) {
	if isMaintenanceJobID(id) {
		return maintenanceRule{}, &inventoryValidationError{Field: "id", Message: "a profile job has no fields of its own to replace; change it for this item with overrides"}
	}

	ok, err := rowExists(tx, `SELECT 1 FROM maintenance_rules WHERE id = ?`, id)
	if err != nil {
		return maintenanceRule{}, fmt.Errorf("update maintenance rule: check exists: %w", err)
	}
	if !ok {
		return maintenanceRule{}, errMaintenanceRuleNotFound
	}

	if in.EquipmentID != nil {
		exists, err := rowExists(tx, `SELECT 1 FROM equipment WHERE id = ?`, *in.EquipmentID)
		if err != nil {
			return maintenanceRule{}, fmt.Errorf("update maintenance rule: check equipment: %w", err)
		}
		if !exists {
			return maintenanceRule{}, errEquipmentNotFound
		}
	}

	if _, err := tx.Exec(`
		UPDATE maintenance_rules SET
			equipment_id = ?, description = ?, interval_hours = ?, interval_months = ?,
			due_soon_hours = ?, due_soon_months = ?, fixed_due_date = ?,
			updated_at = ?
		WHERE id = ?`,
		nullableString(in.EquipmentID), strings.TrimSpace(in.Description), in.IntervalHours, in.IntervalMonths,
		in.DueSoonHours, in.DueSoonMonths, in.FixedDueDate,
		now.Unix(),
		id,
	); err != nil {
		return maintenanceRule{}, fmt.Errorf("update maintenance rule: %w", err)
	}

	updated, err := maintenanceRuleByID(tx, id)
	if err != nil {
		return maintenanceRule{}, err
	}
	return updated, nil
}

// DeleteMaintenanceRule removes a rule through cmdDeleteMaintenanceRule: a
// hand rule, or a profile job row whose service has left the profile. Its log
// entries survive with rule_id set to NULL (ON DELETE SET NULL, the schema's
// own doc comment) - history is never erased by deleting the rule that
// generated it.
func (s *documentStore) DeleteMaintenanceRule(id string) error {
	return s.RunTx(func(tx *sql.Tx, now time.Time) error {
		return cmdDeleteMaintenanceRule(tx, id)
	})
}

// GetMaintenanceRuleRow reads a single stored row. A profile job's interval
// and description columns are not its effective values: anything that shows or
// acts on them resolves through ResolveMaintenanceRule instead.
func (s *documentStore) GetMaintenanceRuleRow(id string) (maintenanceRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maintenanceRuleByID(s.db, id)
}

// ListMaintenanceRuleRows returns the stored rows matching filter, in the
// order the schedule sorts them. Like GetMaintenanceRuleRow it is the raw
// table, not the schedule: a profile's untouched jobs have no rows, and a job
// row's values are not effective (MaintenanceSchedule is the schedule).
func (s *documentStore) ListMaintenanceRuleRows(filter maintenanceRuleFilter) ([]maintenanceRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return queryMaintenanceRuleRows(s.db, filter)
}

// AcknowledgeMaintenanceRule sets (a non-blank reason) or clears (a blank
// one) a rule's acknowledgement - spec §5: "a rule can be acknowledged with
// a short reason". Completing the rule (CompleteMaintenanceRule below) also
// clears it, independently of this method.
func (s *documentStore) AcknowledgeMaintenanceRule(id, reason string) (maintenanceRule, error) {
	var out0 maintenanceRule
	err := s.RunTx(func(tx *sql.Tx, now time.Time) error {
		var err error
		out0, err = acknowledgeMaintenanceRuleTx(tx, now, id, reason)
		return err
	})
	if err != nil {
		return maintenanceRule{}, err
	}
	return out0, nil
}

// acknowledgeMaintenanceRuleTx is AcknowledgeMaintenanceRule inside a transaction the caller owns and commits, with now
// supplied by the caller, so the HTTP handler and the Mate proposal apply run
// the same write (see RunTx).
func acknowledgeMaintenanceRuleTx(tx *sql.Tx, now time.Time, id, reason string) (maintenanceRule, error) {

	// A profile job's row is created by its first write (this one, if nothing
	// has touched it yet).
	if _, err := prepareMaintenanceRuleWriteTx(tx, now, id); err != nil {
		return maintenanceRule{}, err
	}

	trimmed := strings.TrimSpace(reason)
	var ackAt any
	if trimmed != "" {
		ackAt = now.Unix()
	}
	if _, err := tx.Exec(`UPDATE maintenance_rules SET ack_reason = ?, ack_at = ?, updated_at = ? WHERE id = ?`,
		trimmed, ackAt, now.Unix(), id); err != nil {
		return maintenanceRule{}, fmt.Errorf("acknowledge maintenance rule: %w", err)
	}

	updated, err := maintenanceRuleByID(tx, id)
	if err != nil {
		return maintenanceRule{}, err
	}
	return updated, nil
}

// SetMaintenanceRuleLastDone records a baseline directly - spec §3's "Set
// last done" onboarding action, which must NOT write a log entry (that
// would fake a service that never happened through this app). Either
// argument may be nil to leave that half of the baseline untouched -
// "date and/or hours" (spec's own wording).
func (s *documentStore) SetMaintenanceRuleLastDone(id string, at *string, hours *float64) (maintenanceRule, error) {
	var out0 maintenanceRule
	err := s.RunTx(func(tx *sql.Tx, now time.Time) error {
		var err error
		out0, err = setMaintenanceRuleLastDoneTx(tx, now, id, at, hours)
		return err
	})
	if err != nil {
		return maintenanceRule{}, err
	}
	return out0, nil
}

// setMaintenanceRuleLastDoneTx is SetMaintenanceRuleLastDone inside a transaction the caller owns and commits, with now
// supplied by the caller, so the HTTP handler and the Mate proposal apply run
// the same write (see RunTx).
func setMaintenanceRuleLastDoneTx(tx *sql.Tx, now time.Time, id string, at *string, hours *float64) (maintenanceRule, error) {

	if _, err := prepareMaintenanceRuleWriteTx(tx, now, id); err != nil {
		return maintenanceRule{}, err
	}
	existing, err := maintenanceRuleByID(tx, id)
	if err != nil {
		return maintenanceRule{}, err
	}

	newAt := existing.LastDoneAt
	if at != nil {
		newAt = *at
	}
	newHours := existing.LastDoneHours
	if hours != nil {
		newHours = hours
	}

	if _, err := tx.Exec(`UPDATE maintenance_rules SET last_done_at = ?, last_done_hours = ?, updated_at = ? WHERE id = ?`,
		newAt, newHours, now.Unix(), id); err != nil {
		return maintenanceRule{}, fmt.Errorf("set last done: %w", err)
	}

	updated, err := maintenanceRuleByID(tx, id)
	if err != nil {
		return maintenanceRule{}, err
	}
	return updated, nil
}

// SetMaintenanceRuleProcedureNote links (a non-blank id, already validated
// by the handler to be a real note) or clears (blank) a rule's procedure
// note - spec §8.
func (s *documentStore) SetMaintenanceRuleProcedureNote(id, noteID string) (maintenanceRule, error) {
	var out maintenanceRule
	err := s.RunTx(func(tx *sql.Tx, now time.Time) error {
		if _, err := prepareMaintenanceRuleWriteTx(tx, now, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE maintenance_rules SET procedure_note_id = ?, updated_at = ? WHERE id = ?`,
			nullableString(nilIfEmpty(noteID)), now.Unix(), id); err != nil {
			return fmt.Errorf("set procedure note: %w", err)
		}
		var err error
		out, err = maintenanceRuleByID(tx, id)
		return err
	})
	return out, err
}

// ── service log ──────────────────────────────────────────────────────────

const maintenanceLogEntryColumns = `id, equipment_id, rule_id, performed_at, hours, kind, description, who, cost, currency, created_at, updated_at`

func scanMaintenanceLogEntry(row rowScanner) (maintenanceLogEntry, error) {
	var e maintenanceLogEntry
	var equipmentID, ruleID sql.NullString
	var createdAt, updatedAt int64

	if err := row.Scan(
		&e.ID, &equipmentID, &ruleID, &e.PerformedAt, &e.Hours, &e.Kind, &e.Description, &e.Who, &e.Cost, &e.Currency,
		&createdAt, &updatedAt,
	); err != nil {
		return maintenanceLogEntry{}, err
	}
	if equipmentID.Valid {
		v := equipmentID.String
		e.EquipmentID = &v
	}
	if ruleID.Valid {
		v := ruleID.String
		e.RuleID = &v
	}
	e.CreatedAt = time.Unix(createdAt, 0).UTC()
	e.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return e, nil
}

// maintenancePartsForLogEntries returns every id's own parts, joined against
// equipment for a display name - one aggregate query over the whole result
// (equipmentColumns' own doc comment gives the identical "a handful of rows
// aboard one boat" reasoning for why this is never N+1).
//
// LEFT JOIN, not JOIN: equipment_id is nullable once the part item has been
// deleted (documents_store.go's 2026-09-27 amendment) - an inner join would
// silently drop that row out of the result the instant its link is gone,
// which is exactly the data loss this schema change exists to prevent.
// COALESCE prefers the equipment's own CURRENT name while still linked,
// falling back to the part_name snapshot taken at logging time once it
// isn't.
func maintenancePartsForLogEntries(q sqlQueryer, ids []string) (map[string][]maintenanceLogPart, error) {
	out := map[string][]maintenanceLogPart{}
	if len(ids) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := q.Query(`
		SELECT lp.log_entry_id, lp.equipment_id, COALESCE(e.name, lp.part_name), lp.quantity
		FROM maintenance_log_parts lp
		LEFT JOIN equipment e ON e.id = lp.equipment_id
		WHERE lp.log_entry_id IN (`+strings.Join(placeholders, ",")+`)
		ORDER BY lp.log_entry_id, lower(COALESCE(e.name, lp.part_name))`, args...)
	if err != nil {
		return nil, fmt.Errorf("parts for log entries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var logEntryID string
		var p maintenanceLogPart
		if err := rows.Scan(&logEntryID, &p.EquipmentID, &p.EquipmentName, &p.Quantity); err != nil {
			return nil, fmt.Errorf("parts for log entries: scan: %w", err)
		}
		out[logEntryID] = append(out[logEntryID], p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("parts for log entries: %w", err)
	}
	return out, nil
}

// maintenancePhotoIDsForLogEntries mirrors photoIDsForEquipmentIDs
// (inventory_store.go) exactly, over maintenance_log_photos instead.
func maintenancePhotoIDsForLogEntries(q sqlQueryer, ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(ids) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := q.Query(`
		SELECT log_entry_id, document_id FROM maintenance_log_photos
		WHERE log_entry_id IN (`+strings.Join(placeholders, ",")+`)
		ORDER BY log_entry_id, sort_index, document_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("photo ids for log entries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var logEntryID, documentID string
		if err := rows.Scan(&logEntryID, &documentID); err != nil {
			return nil, fmt.Errorf("photo ids for log entries: scan: %w", err)
		}
		out[logEntryID] = append(out[logEntryID], documentID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photo ids for log entries: %w", err)
	}
	return out, nil
}

func maintenanceLogEntryByID(q sqlQueryer, id string) (maintenanceLogEntry, error) {
	row := q.QueryRow(`SELECT `+maintenanceLogEntryColumns+` FROM maintenance_log_entries WHERE id = ?`, id)
	e, err := scanMaintenanceLogEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return maintenanceLogEntry{}, errMaintenanceLogEntryNotFound
	}
	if err != nil {
		return maintenanceLogEntry{}, fmt.Errorf("get log entry: %w", err)
	}
	parts, err := maintenancePartsForLogEntries(q, []string{id})
	if err != nil {
		return maintenanceLogEntry{}, err
	}
	e.Parts = parts[id]
	if e.Parts == nil {
		e.Parts = []maintenanceLogPart{}
	}
	photos, err := maintenancePhotoIDsForLogEntries(q, []string{id})
	if err != nil {
		return maintenanceLogEntry{}, err
	}
	e.PhotoIDs = photos[id]
	if e.PhotoIDs == nil {
		e.PhotoIDs = []string{}
	}
	return e, nil
}

// insertMaintenanceLogPartsTx replaces id's own LINKED parts wholesale - a
// whole-set replace, not a diff, matching the log entry's own PUT-style
// UpdateMaintenanceLogEntry contract (unlike equipment_documents' PATCH
// diff, there is no separate "existing photos" concern to preserve here:
// parts are pure data, not something a concurrent upload could race with).
//
// "Linked" is the operative word (2026-09-27 amendment): a row whose own
// equipment_id has already gone to NULL (its part item was deleted -
// documents_store.go's own comment) can never be re-specified by a fresh
// parts list at all, since parts is only ever a list of {equipment_id,
// quantity} and that id no longer exists to name - so only rows that are
// STILL linked are cleared and replaced here. An orphaned history row
// therefore survives an unrelated later edit to the same log entry, not
// just the part's own deletion.
func insertMaintenanceLogPartsTx(tx *sql.Tx, logEntryID string, parts []maintenanceLogPartInput) error {
	if _, err := tx.Exec(`DELETE FROM maintenance_log_parts WHERE log_entry_id = ? AND equipment_id IS NOT NULL`, logEntryID); err != nil {
		return fmt.Errorf("replace parts: clear: %w", err)
	}
	for _, p := range parts {
		// part_name snapshots the item's name AT THE TIME IT WAS LOGGED -
		// checkMaintenancePartsExist (maintenance_handlers.go) already
		// confirmed p.EquipmentID exists before this transaction runs, so
		// this lookup failing here would mean a real race with a concurrent
		// delete, not an operator mistake - surfaced rather than guessed at.
		var name string
		if err := tx.QueryRow(`SELECT name FROM equipment WHERE id = ?`, p.EquipmentID).Scan(&name); err != nil {
			return fmt.Errorf("replace parts: look up name for %s: %w", p.EquipmentID, err)
		}
		if _, err := tx.Exec(`INSERT INTO maintenance_log_parts (id, log_entry_id, equipment_id, part_name, quantity) VALUES (?, ?, ?, ?, ?)`,
			uuid.NewString(), logEntryID, p.EquipmentID, name, p.Quantity); err != nil {
			return fmt.Errorf("replace parts: insert %s: %w", p.EquipmentID, err)
		}
	}
	return nil
}

// CreateMaintenanceLogEntry inserts a standalone entry - spec §6's "Log
// entries can also be added standalone (repairs, improvements) against an
// item with no rule": rule_id is always NULL here, never taken from the
// input, because linking a rule is what CompleteMaintenanceRule (below,
// the only path that ever sets rule_id) means.
func (s *documentStore) CreateMaintenanceLogEntry(in maintenanceLogEntryInput) (maintenanceLogEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return maintenanceLogEntry{}, fmt.Errorf("create log entry: begin: %w", err)
	}
	defer tx.Rollback()

	if in.EquipmentID != nil {
		ok, err := rowExists(tx, `SELECT 1 FROM equipment WHERE id = ?`, *in.EquipmentID)
		if err != nil {
			return maintenanceLogEntry{}, fmt.Errorf("create log entry: check equipment: %w", err)
		}
		if !ok {
			return maintenanceLogEntry{}, errEquipmentNotFound
		}
	}

	now := s.now()
	id := uuid.NewString()
	if _, err := tx.Exec(`
		INSERT INTO maintenance_log_entries (id, equipment_id, rule_id, performed_at, hours, kind, description, who, cost, currency, created_at, updated_at)
		VALUES (?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, nullableString(in.EquipmentID), in.PerformedAt, in.Hours, in.Kind, in.Description, in.Who, in.Cost, in.Currency, now.Unix(), now.Unix(),
	); err != nil {
		return maintenanceLogEntry{}, fmt.Errorf("create log entry: %w", err)
	}
	if err := insertMaintenanceLogPartsTx(tx, id, in.Parts); err != nil {
		return maintenanceLogEntry{}, err
	}

	created, err := maintenanceLogEntryByID(tx, id)
	if err != nil {
		return maintenanceLogEntry{}, err
	}
	if err := tx.Commit(); err != nil {
		return maintenanceLogEntry{}, fmt.Errorf("create log entry: commit: %w", err)
	}
	return created, nil
}

// UpdateMaintenanceLogEntry replaces id's editable fields (performed_at,
// hours, kind, description, who, cost, currency, parts) - equipment_id and
// rule_id are immutable once created (spec doesn't ask for re-parenting a
// log entry, and doing so silently would misrepresent history). Note: this
// does NOT retroactively adjust its rule's last-done baseline even when
// this is the rule's most recent entry - the baseline is only ever set by
// CompleteMaintenanceRule or SetMaintenanceRuleLastDone (a documented cut,
// not an oversight - see ADR 0138's consequences).
func (s *documentStore) UpdateMaintenanceLogEntry(id string, in maintenanceLogEntryInput) (maintenanceLogEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return maintenanceLogEntry{}, fmt.Errorf("update log entry: begin: %w", err)
	}
	defer tx.Rollback()

	ok, err := rowExists(tx, `SELECT 1 FROM maintenance_log_entries WHERE id = ?`, id)
	if err != nil {
		return maintenanceLogEntry{}, fmt.Errorf("update log entry: check exists: %w", err)
	}
	if !ok {
		return maintenanceLogEntry{}, errMaintenanceLogEntryNotFound
	}

	now := s.now()
	if _, err := tx.Exec(`
		UPDATE maintenance_log_entries SET performed_at = ?, hours = ?, kind = ?, description = ?, who = ?, cost = ?, currency = ?, updated_at = ?
		WHERE id = ?`,
		in.PerformedAt, in.Hours, in.Kind, in.Description, in.Who, in.Cost, in.Currency, now.Unix(), id,
	); err != nil {
		return maintenanceLogEntry{}, fmt.Errorf("update log entry: %w", err)
	}
	if err := insertMaintenanceLogPartsTx(tx, id, in.Parts); err != nil {
		return maintenanceLogEntry{}, err
	}

	updated, err := maintenanceLogEntryByID(tx, id)
	if err != nil {
		return maintenanceLogEntry{}, err
	}
	if err := tx.Commit(); err != nil {
		return maintenanceLogEntry{}, fmt.Errorf("update log entry: commit: %w", err)
	}
	return updated, nil
}

// DeleteMaintenanceLogEntry removes an entry. Its parts/photo links cascade
// with it; the linked photo DOCUMENTS themselves are never deleted here -
// unlink only, the same "never destroy a document as a side effect"
// discipline equipment photos follow (ADR 0127's 2026-09-25 amendment).
func (s *documentStore) DeleteMaintenanceLogEntry(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`DELETE FROM maintenance_log_entries WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete log entry: %w", err)
	}
	return checkRowsAffected(res, errMaintenanceLogEntryNotFound)
}

// GetMaintenanceLogEntry reads a single entry with its parts/photos joined.
func (s *documentStore) GetMaintenanceLogEntry(id string) (maintenanceLogEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maintenanceLogEntryByID(s.db, id)
}

// maintenanceLogFilter is ListMaintenanceLogEntries' input - blank
// EquipmentID lists the whole log (the CSV export's own "all entries"
// mode), matching equipmentFilter's own nil-means-unfiltered convention.
type maintenanceLogFilter struct {
	EquipmentID string
}

// ListMaintenanceLogEntries returns every entry matching filter, newest
// performed_at first - the CSV export and an item's own Maintenance block
// both read this, never the raw table.
func (s *documentStore) ListMaintenanceLogEntries(filter maintenanceLogFilter) ([]maintenanceLogEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `SELECT ` + maintenanceLogEntryColumns + ` FROM maintenance_log_entries WHERE 1=1`
	var args []any
	if filter.EquipmentID != "" {
		query += ` AND equipment_id = ?`
		args = append(args, filter.EquipmentID)
	}
	query += ` ORDER BY performed_at DESC, created_at DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list log entries: %w", err)
	}
	var raw []maintenanceLogEntry
	for rows.Next() {
		e, err := scanMaintenanceLogEntry(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("list log entries: scan: %w", err)
		}
		raw = append(raw, e)
	}
	scanErr := rows.Err()
	rows.Close()
	if scanErr != nil {
		return nil, fmt.Errorf("list log entries: %w", scanErr)
	}

	ids := make([]string, len(raw))
	for i, e := range raw {
		ids[i] = e.ID
	}
	parts, err := maintenancePartsForLogEntries(s.db, ids)
	if err != nil {
		return nil, err
	}
	photos, err := maintenancePhotoIDsForLogEntries(s.db, ids)
	if err != nil {
		return nil, err
	}
	out := make([]maintenanceLogEntry, len(raw))
	for i, e := range raw {
		e.Parts = parts[e.ID]
		if e.Parts == nil {
			e.Parts = []maintenanceLogPart{}
		}
		e.PhotoIDs = photos[e.ID]
		if e.PhotoIDs == nil {
			e.PhotoIDs = []string{}
		}
		out[i] = e
	}
	return out, nil
}

// CompleteMaintenanceRule is spec §6's whole completion flow in one
// transaction: write a log entry (kind is always 'maintenance' - never
// taken from the caller, because completing a rule IS the maintenance kind
// by definition), then reset the rule's own baseline to that same date/
// hours and clear any acknowledgement. Returns both the updated rule and
// the entry that was written, so the handler can answer with everything
// the frontend's Complete dialog needs re-read in one response.
//
// newFixedDueDate is the resolved next due date for a rule that carries a
// fixed_due_date (spec §7/completeMaintenanceRuleHandler's own doc
// comment): whatever the caller decided it should become (already computed
// from interval_months, or already validated as required operator input,
// by the time this runs) - never re-derived here. It is applied only when
// the rule ALREADY has a fixed_due_date and the value given is non-blank;
// a blank value, or a rule with no fixed_due_date to begin with, leaves
// fixed_due_date exactly as it was, so an ordinary hours/months rule can
// never accidentally gain one through this path.
func (s *documentStore) CompleteMaintenanceRule(ruleID string, in maintenanceLogEntryInput, newFixedDueDate string) (maintenanceRule, maintenanceLogEntry, error) {
	var out0 maintenanceRule
	var out1 maintenanceLogEntry
	err := s.RunTx(func(tx *sql.Tx, now time.Time) error {
		var err error
		out0, out1, err = completeMaintenanceRuleTx(tx, now, ruleID, in, newFixedDueDate)
		return err
	})
	if err != nil {
		return maintenanceRule{}, maintenanceLogEntry{}, err
	}
	return out0, out1, nil
}

// completeMaintenanceRuleTx is CompleteMaintenanceRule inside a transaction the caller owns and commits, with now
// supplied by the caller, so the HTTP handler and the Mate proposal apply run
// the same write (see RunTx).
func completeMaintenanceRuleTx(tx *sql.Tx, now time.Time, ruleID string, in maintenanceLogEntryInput, newFixedDueDate string) (maintenanceRule, maintenanceLogEntry, error) {

	if _, err := prepareMaintenanceRuleWriteTx(tx, now, ruleID); err != nil {
		return maintenanceRule{}, maintenanceLogEntry{}, err
	}
	rule, err := maintenanceRuleByID(tx, ruleID)
	if err != nil {
		return maintenanceRule{}, maintenanceLogEntry{}, err
	}

	entryID := uuid.NewString()
	if _, err := tx.Exec(`
		INSERT INTO maintenance_log_entries (id, equipment_id, rule_id, performed_at, hours, kind, description, who, cost, currency, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'maintenance', ?, ?, ?, ?, ?, ?)`,
		entryID, nullableString(rule.EquipmentID), ruleID, in.PerformedAt, in.Hours, in.Description, in.Who, in.Cost, in.Currency, now.Unix(), now.Unix(),
	); err != nil {
		return maintenanceRule{}, maintenanceLogEntry{}, fmt.Errorf("complete maintenance rule: insert log entry: %w", err)
	}
	if err := insertMaintenanceLogPartsTx(tx, entryID, in.Parts); err != nil {
		return maintenanceRule{}, maintenanceLogEntry{}, err
	}

	// The log entry above is written unconditionally - a back-filled old
	// service still belongs in the history. The baseline itself only ever
	// moves FORWARD, though: a completion dated before the rule's existing
	// last_done_at is an old record being added after the fact, not a new
	// most-recent service, so it must not move last_done_at/last_done_hours
	// backward, re-derive fixed_due_date from an earlier date, or clear an
	// acknowledgement that was made about the current, later baseline. A
	// rule with no baseline yet (LastDoneAt == "") always counts as older,
	// so its first completion still sets it.
	if rule.LastDoneAt == "" || in.PerformedAt >= rule.LastDoneAt {
		// Blank hours must never wipe an hours baseline the rule already
		// carries - completing a months-only rule with no hours given says
		// nothing about hours at all, and validateMaintenanceLogEntryCore's
		// own caller (completeMaintenanceRuleHandler) already refuses a
		// blank hours value outright when the rule's own IntervalHours is
		// set, so by the time this runs, in.Hours == nil only ever means
		// "this completion had nothing to say about hours," never "clear
		// it."
		lastDoneHours := rule.LastDoneHours
		if in.Hours != nil {
			lastDoneHours = in.Hours
		}

		fixedDueDate := rule.FixedDueDate
		if rule.FixedDueDate != "" && newFixedDueDate != "" {
			fixedDueDate = newFixedDueDate
		}

		if _, err := tx.Exec(`UPDATE maintenance_rules SET last_done_at = ?, last_done_hours = ?, fixed_due_date = ?, ack_reason = '', ack_at = NULL, updated_at = ? WHERE id = ?`,
			in.PerformedAt, lastDoneHours, fixedDueDate, now.Unix(), ruleID); err != nil {
			return maintenanceRule{}, maintenanceLogEntry{}, fmt.Errorf("complete maintenance rule: reset baseline: %w", err)
		}
	}

	updatedRule, err := maintenanceRuleByID(tx, ruleID)
	if err != nil {
		return maintenanceRule{}, maintenanceLogEntry{}, err
	}
	entry, err := maintenanceLogEntryByID(tx, entryID)
	if err != nil {
		return maintenanceRule{}, maintenanceLogEntry{}, err
	}

	return updatedRule, entry, nil
}

// ── log entry photos ─────────────────────────────────────────────────────
// Mirrors AddEquipmentPhoto/RemoveEquipmentPhoto (inventory_store.go)
// exactly, over maintenance_log_photos - spec §6: "photo handling mirrors
// equipment photo remove semantics". No reorder/make-cover endpoint: a log
// entry's photos are a plain, append-ordered strip (the first is shown
// first), not a cover-marked strip the way an equipment item's is - a log
// entry has no single "hero" photo concept, so that piece of ADR 0127
// intentionally isn't carried over (see ADR 0138's own scope notes).

var errMaintenanceLogPhotoNotFound = errors.New("photo not found on this log entry")

// AddMaintenanceLogPhoto links documentID to logEntryID, at the end of the
// entry's current photo order.
func (s *documentStore) AddMaintenanceLogPhoto(logEntryID, documentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("add log photo: begin: %w", err)
	}
	defer tx.Rollback()

	ok, err := rowExists(tx, `SELECT 1 FROM maintenance_log_entries WHERE id = ?`, logEntryID)
	if err != nil {
		return fmt.Errorf("add log photo: check log entry: %w", err)
	}
	if !ok {
		return errMaintenanceLogEntryNotFound
	}

	already, err := rowExists(tx, `SELECT 1 FROM maintenance_log_photos WHERE log_entry_id = ? AND document_id = ?`, logEntryID, documentID)
	if err != nil {
		return fmt.Errorf("add log photo: check existing: %w", err)
	}
	if already {
		return tx.Commit()
	}

	var maxSort sql.NullInt64
	if err := tx.QueryRow(`SELECT MAX(sort_index) FROM maintenance_log_photos WHERE log_entry_id = ?`, logEntryID).Scan(&maxSort); err != nil {
		return fmt.Errorf("add log photo: max sort: %w", err)
	}
	next := 0
	if maxSort.Valid {
		next = int(maxSort.Int64) + 1
	}

	now := s.now().Unix()
	if _, err := tx.Exec(`INSERT INTO maintenance_log_photos (log_entry_id, document_id, sort_index, created_at) VALUES (?, ?, ?, ?)`,
		logEntryID, documentID, next, now); err != nil {
		return fmt.Errorf("add log photo: link: %w", err)
	}
	return tx.Commit()
}

// RemoveMaintenanceLogPhoto unlinks documentID from logEntryID - unlink
// only, never deletes the document itself (the handler decides that
// separately, exactly like deleteEquipmentPhotoHandler does).
func (s *documentStore) RemoveMaintenanceLogPhoto(logEntryID, documentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`DELETE FROM maintenance_log_photos WHERE log_entry_id = ? AND document_id = ?`, logEntryID, documentID)
	if err != nil {
		return fmt.Errorf("remove log photo: %w", err)
	}
	return checkRowsAffected(res, errMaintenanceLogPhotoNotFound)
}

// MaintenanceLogPhotoDeletableAsOrphan mirrors
// DocumentDeletableAsOrphanPhoto (inventory_store.go) - deletable only when
// nothing else links it (checked across BOTH equipment_documents AND
// maintenance_log_photos, since a photo can be shared between an item and a
// log entry) and it meets documentDeletableAsOrphanPhotoClause's own two
// conditions.
func (s *documentStore) MaintenanceLogPhotoDeletableAsOrphan(documentID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return rowExists(s.db, `
		SELECT 1 FROM documents d
		WHERE d.id = ?
		AND `+documentDeletableAsOrphanPhotoClause+`
		AND `+documentHasNoEquipmentPhotoLinkClause+`
		AND `+documentHasNoMaintenanceLogPhotoLinkClause, documentID)
}

// ── hour meter resets ────────────────────────────────────────────────────

// RecordHourMeterReset stores a meter-replacement history row (spec §2) -
// old reading, new reading, the date it happened. This is pure history;
// offsetInForceAt (maintenance_hours.go) is what turns it into the offset a
// live reading or an operator-typed gauge reading (gaugeToTrueHours) is
// actually converted with.
func (s *documentStore) RecordHourMeterReset(equipmentID string, oldReading, newReading float64, changedAt string) (hourMeterReset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return hourMeterReset{}, fmt.Errorf("record meter reset: begin: %w", err)
	}
	defer tx.Rollback()

	ok, err := rowExists(tx, `SELECT 1 FROM equipment WHERE id = ?`, equipmentID)
	if err != nil {
		return hourMeterReset{}, fmt.Errorf("record meter reset: check equipment: %w", err)
	}
	if !ok {
		return hourMeterReset{}, errEquipmentNotFound
	}

	now := s.now()
	id := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO hour_meter_resets (id, equipment_id, old_reading, new_reading, changed_at, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, equipmentID, oldReading, newReading, changedAt, now.Unix()); err != nil {
		return hourMeterReset{}, fmt.Errorf("record meter reset: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return hourMeterReset{}, fmt.Errorf("record meter reset: commit: %w", err)
	}
	return hourMeterReset{ID: id, OldReading: oldReading, NewReading: newReading, ChangedAt: changedAt, CreatedAt: now}, nil
}

// ListHourMeterResets returns equipmentID's own meter-reset history,
// newest first BY ChangedAt (the date the replacement actually happened,
// tie-broken by id) - not by created_at, which only says when the row was
// entered and can disagree with ChangedAt for a back-filled reset. The
// equipment editor's own display of past replacements, and the input
// offsetInForceAt (maintenance_hours.go) resolves into an offset - both
// read this same order (offsetInForceAt trusts it, taking the first match
// rather than re-sorting), so the editor's own "most recent replacement"
// line can never disagree with which reset the status engine is actually
// using.
func (s *documentStore) ListHourMeterResets(equipmentID string) ([]hourMeterReset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return hourMeterResetsFor(s.db, equipmentID)
}

// hourMeterResetsFor is ListHourMeterResets' query over any connection or
// open transaction, for the maintenance commands that convert a gauge
// reading inside their own transaction.
func hourMeterResetsFor(q sqlQueryer, equipmentID string) ([]hourMeterReset, error) {
	rows, err := q.Query(`SELECT id, old_reading, new_reading, changed_at, created_at FROM hour_meter_resets WHERE equipment_id = ? ORDER BY changed_at DESC, id DESC`, equipmentID)
	if err != nil {
		return nil, fmt.Errorf("list meter resets: %w", err)
	}
	defer rows.Close()

	out := []hourMeterReset{}
	for rows.Next() {
		var r hourMeterReset
		var createdAt int64
		if err := rows.Scan(&r.ID, &r.OldReading, &r.NewReading, &r.ChangedAt, &createdAt); err != nil {
			return nil, fmt.Errorf("list meter resets: scan: %w", err)
		}
		r.CreatedAt = time.Unix(createdAt, 0).UTC()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list meter resets: %w", err)
	}
	return out, nil
}

// nilIfEmpty turns "" into a nil *string - the same "empty means cleared"
// convention trimStringPtr (inventory_handlers.go) applies to equipment's
// own zone_id/bin_id, reused here for procedure_note_id.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Read runs fn with a read-only view of the database, holding the store mutex
// for its duration. It is for code that reads through the transaction-taking
// helpers (a registered record type's Get) outside any write. fn must not
// call another documentStore method.
func (s *documentStore) Read(fn func(q sqlQueryer) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(s.db)
}
