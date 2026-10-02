package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// The maintenance write commands (ADR 0146). Each is one rule write with its
// validation, its gauge-to-true hours conversion and its store call, taking
// an open transaction so the HTTP handlers (which run one command in a
// transaction of their own, via documentStore.RunTx) and Mate's
// proposal apply (which runs several in one transaction and commits only if
// every one succeeds) execute exactly the same code. A command never touches
// the store's mutex, the echo context or the wall clock: now and today come
// in as arguments, and every read goes through tx, so it is safe to call
// inside a transaction that already holds the write lock.
//
// A command fails with one of three kinds of error, all mapped by
// writeMaintenanceCommandError: *inventoryValidationError (400, names the
// field), *maintenanceCommandError (an expected refusal with its own status)
// or anything else, which is a sentinel documentErrorStatus knows or a 500.

// maintenanceCommandError is an expected, operator-facing refusal that
// carries its own HTTP status: a 409 for a profile that cannot be copied, a
// 404 that names a missing part.
type maintenanceCommandError struct {
	Status  int
	Message string
}

func (e *maintenanceCommandError) Error() string { return e.Message }

// writeMaintenanceCommandError answers c for any error a maintenance command
// returned, in the shapes the handlers always used.
func writeMaintenanceCommandError(c echo.Context, err error) error {
	var verr *inventoryValidationError
	if errors.As(err, &verr) {
		return writeInventoryValidationError(c, verr)
	}
	var cerr *maintenanceCommandError
	if errors.As(err, &cerr) {
		return c.JSON(cerr.Status, map[string]string{"error": cerr.Message})
	}
	return writeDocumentError(c, err)
}

// validateMaintenanceLastDone is set-last-done's own request validation, with
// no store access. It returns the request with last_done_at trimmed.
func validateMaintenanceLastDone(req maintenanceLastDoneRequest) (maintenanceLastDoneRequest, *inventoryValidationError) {
	if req.LastDoneAt == nil && req.LastDoneHours == nil {
		return req, &inventoryValidationError{Field: "last_done_at", Message: "give a date and/or an hours reading"}
	}
	if req.LastDoneAt != nil {
		trimmed := strings.TrimSpace(*req.LastDoneAt)
		if trimmed != "" && !installDatePattern.MatchString(trimmed) {
			return req, &inventoryValidationError{Field: "last_done_at", Message: "last_done_at must be blank or YYYY-MM-DD"}
		}
		req.LastDoneAt = &trimmed
	}
	if req.LastDoneHours != nil && *req.LastDoneHours < 0 {
		return req, &inventoryValidationError{Field: "last_done_hours", Message: "last_done_hours cannot be negative"}
	}
	return req, nil
}

// convertGaugeHoursToTrueTx is convertGaugeHoursToTrue over an open
// transaction: the same reset-history lookup and the same gaugeToTrueHours.
func convertGaugeHoursToTrueTx(q sqlQueryer, equipmentID string, gauge float64, atDate time.Time) (float64, error) {
	resets, err := hourMeterResetsFor(q, equipmentID)
	if err != nil {
		return 0, err
	}
	return gaugeToTrueHours(resets, gauge, atDate), nil
}

func cmdCreateMaintenanceRule(tx *sql.Tx, now time.Time, req maintenanceRuleRequest) (maintenanceRule, error) {
	in, verr := validateMaintenanceRuleInput(req)
	if verr != nil {
		return maintenanceRule{}, verr
	}
	return createMaintenanceRuleTx(tx, now, in)
}

// cmdUpdateMaintenanceRule replaces a hand rule's fields. A profile job has no
// fields of its own to replace: its values come from the profile and an item
// changes them with overrides, so a job id is refused.
func cmdUpdateMaintenanceRule(tx *sql.Tx, now time.Time, id string, req maintenanceRuleRequest) (maintenanceRule, error) {
	if isMaintenanceJobID(id) {
		return maintenanceRule{}, &inventoryValidationError{
			Field:   "id",
			Message: "this job comes from the equipment profile and cannot be edited as a rule; change it for this item with PUT /api/inventory/maintenance/rules/:id/overrides",
		}
	}
	in, verr := validateMaintenanceRuleInput(req)
	if verr != nil {
		return maintenanceRule{}, verr
	}
	return updateMaintenanceRuleTx(tx, now, id, in)
}

// cmdAcknowledgeMaintenanceRule sets (non-blank reason) or clears (blank) a
// rule's acknowledgement.
func cmdAcknowledgeMaintenanceRule(tx *sql.Tx, now time.Time, id, reason string) (maintenanceRule, error) {
	return acknowledgeMaintenanceRuleTx(tx, now, id, reason)
}

// cmdSetMaintenanceRuleLastDone records a baseline directly. The operator
// types a GAUGE (raw meter) reading; it is converted to true hours with the
// offset in force on the date the reading is from: the date given, or today
// for an hours-only call (there is no other date to convert it at).
func cmdSetMaintenanceRuleLastDone(tx *sql.Tx, now, today time.Time, id string, req maintenanceLastDoneRequest) (maintenanceRule, error) {
	req, verr := validateMaintenanceLastDone(req)
	if verr != nil {
		return maintenanceRule{}, verr
	}

	existingRule, err := prepareMaintenanceRuleWriteTx(tx, now, id)
	if err != nil {
		return maintenanceRule{}, err
	}
	if req.LastDoneHours != nil {
		if existingRule.EquipmentID != nil {
			atDate := today
			if req.LastDoneAt != nil && *req.LastDoneAt != "" {
				parsed, parseErr := time.Parse("2006-01-02", *req.LastDoneAt)
				if parseErr != nil {
					// Already passed installDatePattern above; a failure
					// here would be this code disagreeing with itself.
					return maintenanceRule{}, fmt.Errorf("parse last_done_at: %w", parseErr)
				}
				atDate = parsed
			}
			trueHours, err := convertGaugeHoursToTrueTx(tx, *existingRule.EquipmentID, *req.LastDoneHours, atDate)
			if err != nil {
				return maintenanceRule{}, err
			}
			req.LastDoneHours = &trueHours
		}
	}

	return setMaintenanceRuleLastDoneTx(tx, now, id, req.LastDoneAt, req.LastDoneHours)
}

// checkMaintenancePartsExistTx is checkMaintenancePartsExist over an open
// transaction, answering with the same 404 message.
func checkMaintenancePartsExistTx(q sqlQueryer, parts []maintenanceLogPartInput) error {
	for _, p := range parts {
		if _, err := equipmentByID(q, p.EquipmentID); err != nil {
			if errors.Is(err, errEquipmentNotFound) {
				return &maintenanceCommandError{Status: http.StatusNotFound, Message: fmt.Sprintf("part %s not found: %s", p.EquipmentID, errEquipmentNotFound)}
			}
			return err
		}
	}
	return nil
}

// planMaintenanceCompletion is the part of completing a rule that needs only
// the rule and the validated entry, no store access: the checks that can
// refuse it and the fixed due date it advances to. propose_maintenance_changes
// runs it at propose time so a completion Mate cannot make is refused before
// the operator sees a card.
//
// An hours-interval rule can never sensibly complete without a reading - live
// when the item's meter is bound and fresh, typed in by hand from the gauge
// otherwise (spec: never guessed at). Refusing this before the store runs
// also protects CompleteMaintenanceRule's own "blank hours leaves the
// baseline unchanged" rule from ever being asked to interpret a blank hours
// field for a rule that actually needs one.
//
// A fixed-due-date rule must not read overdue again the instant it is
// completed - its own due date has to advance. With interval_months set the
// next date is computed from this completion's date (newDueDate is ignored: a
// formula exists). Without it there is no formula (a one-off expiry), so the
// operator's own new_due_date is required.
func planMaintenanceCompletion(rule maintenanceRule, in maintenanceLogEntryInput, newDueDate string) (newFixedDueDate string, performedAt time.Time, err error) {
	if rule.IntervalHours != nil && in.Hours == nil {
		return "", time.Time{}, &inventoryValidationError{
			Field:   "hours",
			Message: "hours is required to complete an hours-based rule - enter the current reading if it isn't filled in automatically",
		}
	}

	// performed_at already passed installDatePattern in
	// validateMaintenanceLogEntryCore; a parse failure here would be this
	// code disagreeing with itself, not an operator mistake.
	performedAt, parseErr := time.Parse("2006-01-02", in.PerformedAt)
	if parseErr != nil {
		return "", time.Time{}, fmt.Errorf("parse performed_at: %w", parseErr)
	}

	if rule.FixedDueDate != "" {
		if rule.IntervalMonths != nil {
			newFixedDueDate = performedAt.AddDate(0, *rule.IntervalMonths, 0).Format("2006-01-02")
		} else {
			trimmed := strings.TrimSpace(newDueDate)
			if trimmed == "" || !installDatePattern.MatchString(trimmed) {
				return "", time.Time{}, &inventoryValidationError{
					Field:   "new_due_date",
					Message: "new_due_date is required and must be YYYY-MM-DD to complete a fixed-date rule with no monthly interval",
				}
			}
			newFixedDueDate = trimmed
		}
	}
	return newFixedDueDate, performedAt, nil
}

// cmdCompleteMaintenanceRule writes a log entry for the rule and resets its
// baseline. kind is always 'maintenance' and the equipment always comes from
// the rule; both are never taken from req. The operator types a GAUGE (raw
// meter) reading (ADR 0138's 2026-09-27 amendment): it is converted to true
// hours with the offset that was in force on THIS COMPLETION'S OWN DATE, so a
// back-filled old service converts with the offset that applied then, never
// today's.
func cmdCompleteMaintenanceRule(tx *sql.Tx, now time.Time, id string, req maintenanceLogEntryRequest) (maintenanceRule, maintenanceLogEntry, error) {
	in, verr := validateMaintenanceLogEntryCore(req)
	if verr != nil {
		return maintenanceRule{}, maintenanceLogEntry{}, verr
	}

	// The EFFECTIVE rule: a profile job's interval comes from its profile (and
	// the item's overrides), never from the row.
	existingRule, err := prepareMaintenanceRuleWriteTx(tx, now, id)
	if err != nil {
		return maintenanceRule{}, maintenanceLogEntry{}, err
	}
	newFixedDueDate, performedAt, err := planMaintenanceCompletion(existingRule.maintenanceRule, in, req.NewDueDate)
	if err != nil {
		return maintenanceRule{}, maintenanceLogEntry{}, err
	}

	if in.Hours != nil && existingRule.EquipmentID != nil {
		trueHours, err := convertGaugeHoursToTrueTx(tx, *existingRule.EquipmentID, *in.Hours, performedAt)
		if err != nil {
			return maintenanceRule{}, maintenanceLogEntry{}, err
		}
		in.Hours = &trueHours
	}

	if err := checkMaintenancePartsExistTx(tx, in.Parts); err != nil {
		return maintenanceRule{}, maintenanceLogEntry{}, err
	}

	return completeMaintenanceRuleTx(tx, now, id, in, newFixedDueDate)
}
