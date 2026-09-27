package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// This file serves the maintenance feature's HTTP API (ADR 0138): service
// rules and the log they're completed into, under /api/inventory/maintenance/
// and, for the two actions that are really about one item
// (copy-profile-schedule, meter-reset), under
// /api/inventory/equipment/:id/maintenance/. inventory_handlers.go is this
// file's own model throughout: validate-then-store the same two-step split
// validateEquipmentInput/CreateEquipment uses, writeDocumentError/
// writeInventoryValidationError reused wholesale rather than a parallel
// pair of helpers (maintenance is a sub-feature of Inventory, not a
// separate one), and the same upload-photo pattern
// uploadEquipmentPhotoHandler already established for log entry photos.
//
// What this file does that inventory_handlers.go's equipment routes never
// had to: every rule response is a maintenanceRuleView, not the bare stored
// row - resolveMaintenanceRuleView (single rule) and the batched loop inside
// listMaintenanceRulesHandler both compose the stored fields with a FRESH
// computeMaintenanceRuleStatus call (maintenance_status.go) and a fresh
// currentEquipmentHours read (maintenance_hours.go) on every request, so the
// list is never looking at a stale, previously-computed status.

// ── rule views ───────────────────────────────────────────────────────────

// maintenanceRuleView is what every rule-returning endpoint answers with:
// the stored row's own fields (no omitempty - ADR 0115 §7's "the browser
// binds these through a TypeScript interface that declares every key"
// reasoning, exactly like equipmentItem's own doc comment gives) plus the
// freshly computed status. EquipmentName/System/HasHourMeterPath are "" /
// false for a calendar-only rule (EquipmentID nil) - there is no item to
// join them from.
type maintenanceRuleView struct {
	ID               string    `json:"id"`
	EquipmentID      *string   `json:"equipment_id"`
	EquipmentName    string    `json:"equipment_name"`
	System           string    `json:"system"`
	Description      string    `json:"description"`
	IntervalHours    *float64  `json:"interval_hours"`
	IntervalMonths   *int      `json:"interval_months"`
	DueSoonHours     *float64  `json:"due_soon_hours"`
	DueSoonMonths    *int      `json:"due_soon_months"`
	FixedDueDate     string    `json:"fixed_due_date"`
	LastDoneAt       string    `json:"last_done_at"`
	LastDoneHours    *float64  `json:"last_done_hours"`
	ProfileServiceID string    `json:"profile_service_id"`
	ProcedureNoteID  string    `json:"procedure_note_id"`
	AckReason        string    `json:"ack_reason"`
	Acknowledged     bool      `json:"acknowledged"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`

	// Computed fresh on every response (maintenance_status.go) - never
	// stored.
	Status           string   `json:"status"`
	RemainingHours   *float64 `json:"remaining_hours"`
	RemainingDays    *int     `json:"remaining_days"`
	HoursUnknown     bool     `json:"hours_unknown"`
	HasHourMeterPath bool     `json:"has_hour_meter_path"`
	// CurrentHours is the item's own live true-hours reading right now
	// (nil unless Hours.Known) - a convenience for the frontend's Complete
	// dialog, which prefills its hours field from this rather than
	// re-deriving it from RemainingHours/LastDoneHours arithmetic.
	CurrentHours *float64 `json:"current_hours"`
	// HoursStaleSince (RFC3339) is set only when the item's hour meter path
	// IS bound but its value is older than the staleness threshold - spec's
	// own "unknown/stale since X". Blank/null both when the hours axis
	// isn't relevant to this rule at all and when nothing has ever been
	// received for the path (see currentEquipmentHours' own doc comment on
	// the difference).
	HoursStaleSince *string `json:"hours_stale_since"`
}

// requireTodayParam reads and validates the ?today=YYYY-MM-DD query param
// every rule-view-returning endpoint requires - the operator's own local
// calendar date (frontend: lib/local-date.ts's todayISO, read from the
// browser's own clock), never derived from the server's own wall clock.
// AGENTS.md fail-fast: missing or malformed is a 400 naming the field,
// never a silent fallback to the server's own time.Now() - a boat well
// east of UTC would otherwise have every due/overdue decision computed
// against the WRONG calendar date whenever it's evening in the server's own
// UTC day but already tomorrow, or still yesterday, at the helm.
func requireTodayParam(c echo.Context) (time.Time, *inventoryValidationError) {
	raw := strings.TrimSpace(c.QueryParam("today"))
	if raw == "" {
		return time.Time{}, &inventoryValidationError{Field: "today", Message: "today is required and must be YYYY-MM-DD - the operator's own local date, not the server's"}
	}
	today, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, &inventoryValidationError{Field: "today", Message: "today must be YYYY-MM-DD"}
	}
	return today, nil
}

// parseMaintenanceDate parses a YYYY-MM-DD column into a *time.Time for the
// status engine, nil for blank. A value that fails to parse is treated the
// same as blank (excluded from the calendar axis) rather than failing the
// whole request - every writer of these columns already validates the
// pattern before storing (validateMaintenanceRuleInput,
// validateMaintenanceLogEntryCore below), so a parse failure here would
// only ever mean a row written before that validation existed, not an
// upstream data problem worth failing loudly over.
func parseMaintenanceDate(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &t
}

// buildMaintenanceRuleView composes rule's stored fields with a fresh
// status computation. eq is nil for a calendar-only rule. today is the
// operator's own local calendar date (requireTodayParam) - never an
// instant, and never the server's own clock; see
// maintenanceRuleStatusInput.Today's own doc comment (maintenance_status.go)
// for why.
func buildMaintenanceRuleView(rule maintenanceRule, eq *equipmentItem, hours maintenanceHourReading, today time.Time) maintenanceRuleView {
	result := computeMaintenanceRuleStatus(maintenanceRuleStatusInput{
		IntervalHours:  rule.IntervalHours,
		IntervalMonths: rule.IntervalMonths,
		DueSoonHours:   rule.DueSoonHours,
		DueSoonMonths:  rule.DueSoonMonths,
		FixedDueDate:   parseMaintenanceDate(rule.FixedDueDate),
		LastDoneAt:     parseMaintenanceDate(rule.LastDoneAt),
		LastDoneHours:  rule.LastDoneHours,
		Hours:          hours,
		Today:          today,
	})

	view := maintenanceRuleView{
		ID:               rule.ID,
		EquipmentID:      rule.EquipmentID,
		Description:      rule.Description,
		IntervalHours:    rule.IntervalHours,
		IntervalMonths:   rule.IntervalMonths,
		DueSoonHours:     rule.DueSoonHours,
		DueSoonMonths:    rule.DueSoonMonths,
		FixedDueDate:     rule.FixedDueDate,
		LastDoneAt:       rule.LastDoneAt,
		LastDoneHours:    rule.LastDoneHours,
		ProfileServiceID: rule.ProfileServiceID,
		ProcedureNoteID:  rule.ProcedureNoteID,
		AckReason:        rule.AckReason,
		Acknowledged:     rule.Acknowledged,
		CreatedAt:        rule.CreatedAt,
		UpdatedAt:        rule.UpdatedAt,
		Status:           string(result.Status),
		RemainingHours:   result.RemainingHours,
		RemainingDays:    result.RemainingDays,
		HoursUnknown:     result.HoursUnknown,
	}
	if eq != nil {
		view.EquipmentName = eq.Name
		view.System = eq.System
		view.HasHourMeterPath = strings.TrimSpace(eq.HourMeterPath) != ""
	}
	if hours.StaleSince != nil {
		s := hours.StaleSince.UTC().Format(time.RFC3339)
		view.HoursStaleSince = &s
	}
	if hours.Known {
		h := hours.Hours
		view.CurrentHours = &h
	}
	return view
}

// equipmentHourReading resolves eq's current true hours (live value plus
// its own latest meter-reset offset) - shared by resolveMaintenanceRuleView
// (one rule) and listMaintenanceRulesHandler's own per-equipment cache (many
// rules), so the two can never disagree about how an item's hours are
// computed.
func equipmentHourReading(eq equipmentItem, now time.Time) (maintenanceHourReading, error) {
	resets, err := globalDocumentStore.ListHourMeterResets(eq.ID)
	if err != nil {
		return maintenanceHourReading{}, err
	}
	offset := latestMeterOffsetHours(resets)
	reader := snapshotAlarmReader(globalSignalKSnapshot)
	return currentEquipmentHours(reader, globalSignalKSnapshot, eq.HourMeterPath, offset, now), nil
}

// resolveMaintenanceRuleView builds the response view for a single rule -
// every create/update/acknowledge/last-done/complete/procedure-note
// endpoint answers with this, so the frontend always sees status
// recomputed against the CURRENT live reading right after its own write,
// never a value that could already be stale by the time the response
// arrives. today is the operator's own local calendar date
// (requireTodayParam), used only for the status engine's calendar axis;
// the hours axis still reads the actual wall-clock instant (time.Now()
// here) for staleness, which is a real elapsed-time question independent
// of which calendar day it is at the helm.
func resolveMaintenanceRuleView(rule maintenanceRule, today time.Time) (maintenanceRuleView, error) {
	now := time.Now().UTC()
	if rule.EquipmentID == nil {
		return buildMaintenanceRuleView(rule, nil, maintenanceHourReading{}, today), nil
	}
	eq, err := globalDocumentStore.GetEquipment(*rule.EquipmentID)
	if err != nil {
		return maintenanceRuleView{}, err
	}
	hours, err := equipmentHourReading(eq, now)
	if err != nil {
		return maintenanceRuleView{}, err
	}
	return buildMaintenanceRuleView(rule, &eq, hours, today), nil
}

// ── validation ───────────────────────────────────────────────────────────

// maintenanceRuleRequest is the JSON body POST/PUT
// /api/inventory/maintenance/rules(/:id) accept - a rule's CORE fields only
// (maintenanceRuleInput's own doc comment explains what's deliberately
// excluded and why).
type maintenanceRuleRequest struct {
	EquipmentID      *string  `json:"equipment_id"`
	Description      string   `json:"description"`
	IntervalHours    *float64 `json:"interval_hours"`
	IntervalMonths   *int     `json:"interval_months"`
	DueSoonHours     *float64 `json:"due_soon_hours"`
	DueSoonMonths    *int     `json:"due_soon_months"`
	FixedDueDate     string   `json:"fixed_due_date"`
	ProfileServiceID string   `json:"profile_service_id"`
}

// validateMaintenanceRuleInput mirrors validateEquipmentInput's own shape
// (inventory_handlers.go): one {field, message} failure at a time.
//
//   - description is required.
//   - interval_hours, when given, must be > 0, and requires an equipment_id
//     (hours make no sense without an item's own meter to read).
//   - interval_months, when given, must be >= 1.
//   - due_soon_hours/due_soon_months, when given, must be >= 0.
//   - fixed_due_date, when given, must be blank or YYYY-MM-DD.
//   - at least one of interval_hours/interval_months/fixed_due_date is
//     required UNLESS profile_service_id names a profile schedule entry
//     copied with both slots empty (spec §1's own "interval not set" rule).
//
// It does NOT check that equipment_id names a real item - the same split
// validateEquipmentInput's own doc comment gives for zone_id/bin_id:
// CreateMaintenanceRule/UpdateMaintenanceRule already do that lookup inside
// their own transaction and surface errEquipmentNotFound through the normal
// writeDocumentError path.
func validateMaintenanceRuleInput(req maintenanceRuleRequest) (maintenanceRuleInput, *inventoryValidationError) {
	description := strings.TrimSpace(req.Description)
	if description == "" {
		return maintenanceRuleInput{}, &inventoryValidationError{Field: "description", Message: "description is required"}
	}

	equipmentID := trimStringPtr(req.EquipmentID)

	if req.IntervalHours != nil {
		if *req.IntervalHours <= 0 {
			return maintenanceRuleInput{}, &inventoryValidationError{Field: "interval_hours", Message: "interval_hours must be greater than zero"}
		}
		if equipmentID == nil {
			return maintenanceRuleInput{}, &inventoryValidationError{Field: "interval_hours", Message: "interval_hours requires an equipment item with an hour meter"}
		}
	}
	if req.IntervalMonths != nil && *req.IntervalMonths < 1 {
		return maintenanceRuleInput{}, &inventoryValidationError{Field: "interval_months", Message: "interval_months must be at least 1"}
	}
	if req.DueSoonHours != nil && *req.DueSoonHours < 0 {
		return maintenanceRuleInput{}, &inventoryValidationError{Field: "due_soon_hours", Message: "due_soon_hours cannot be negative"}
	}
	if req.DueSoonMonths != nil && *req.DueSoonMonths < 0 {
		return maintenanceRuleInput{}, &inventoryValidationError{Field: "due_soon_months", Message: "due_soon_months cannot be negative"}
	}
	fixedDueDate := strings.TrimSpace(req.FixedDueDate)
	if fixedDueDate != "" && !installDatePattern.MatchString(fixedDueDate) {
		return maintenanceRuleInput{}, &inventoryValidationError{Field: "fixed_due_date", Message: "fixed_due_date must be blank or YYYY-MM-DD"}
	}

	profileServiceID := strings.TrimSpace(req.ProfileServiceID)
	hasInterval := req.IntervalHours != nil || req.IntervalMonths != nil || fixedDueDate != ""
	if !hasInterval && profileServiceID == "" {
		return maintenanceRuleInput{}, &inventoryValidationError{
			Field:   "interval_hours",
			Message: "at least one of interval_hours, interval_months or a fixed due date is required",
		}
	}

	return maintenanceRuleInput{
		EquipmentID:      equipmentID,
		Description:      description,
		IntervalHours:    req.IntervalHours,
		IntervalMonths:   req.IntervalMonths,
		DueSoonHours:     req.DueSoonHours,
		DueSoonMonths:    req.DueSoonMonths,
		FixedDueDate:     fixedDueDate,
		ProfileServiceID: profileServiceID,
	}, nil
}

// maintenanceLogPartRequest/maintenanceLogEntryRequest is the JSON body
// every service-log write accepts - createMaintenanceLogEntryHandler and
// updateMaintenanceLogEntryHandler both bind this and additionally require/
// validate kind and (create only) equipment_id;
// completeMaintenanceRuleHandler binds it too but takes kind and
// equipment_id from the rule itself, never from the request (spec:
// "completing a rule is maintenance").
type maintenanceLogPartRequest struct {
	EquipmentID string  `json:"equipment_id"`
	Quantity    float64 `json:"quantity"`
}

type maintenanceLogEntryRequest struct {
	EquipmentID *string                     `json:"equipment_id"`
	PerformedAt string                      `json:"performed_at"`
	Hours       *float64                    `json:"hours"`
	Kind        string                      `json:"kind"`
	Description string                      `json:"description"`
	Who         string                      `json:"who"`
	Cost        *float64                    `json:"cost"`
	Currency    string                      `json:"currency"`
	Parts       []maintenanceLogPartRequest `json:"parts"`
	// NewDueDate is completeMaintenanceRuleHandler's own field, meaningless
	// to the standalone create/update handlers: the next fixed_due_date for
	// a rule that has one and no interval_months to compute it from
	// (completeMaintenanceRuleHandler's own doc comment explains why the
	// two cases - computed vs operator-supplied - differ).
	NewDueDate string `json:"new_due_date"`
}

// validateMaintenanceLogEntryCore validates the fields every log-entry
// write shares (performed_at, hours, cost, parts) - kind and equipment_id
// are each caller's own job, since the three call sites (standalone
// create, update, complete) each source them differently.
func validateMaintenanceLogEntryCore(req maintenanceLogEntryRequest) (maintenanceLogEntryInput, *inventoryValidationError) {
	performedAt := strings.TrimSpace(req.PerformedAt)
	if performedAt == "" || !installDatePattern.MatchString(performedAt) {
		return maintenanceLogEntryInput{}, &inventoryValidationError{Field: "performed_at", Message: "performed_at is required and must be YYYY-MM-DD"}
	}
	if req.Hours != nil && *req.Hours < 0 {
		return maintenanceLogEntryInput{}, &inventoryValidationError{Field: "hours", Message: "hours cannot be negative"}
	}
	if req.Cost != nil && *req.Cost < 0 {
		return maintenanceLogEntryInput{}, &inventoryValidationError{Field: "cost", Message: "cost cannot be negative"}
	}

	parts := make([]maintenanceLogPartInput, 0, len(req.Parts))
	seenParts := make(map[string]bool, len(req.Parts))
	for _, p := range req.Parts {
		id := strings.TrimSpace(p.EquipmentID)
		if id == "" {
			return maintenanceLogEntryInput{}, &inventoryValidationError{Field: "parts", Message: "a part must name an equipment item"}
		}
		if p.Quantity <= 0 {
			return maintenanceLogEntryInput{}, &inventoryValidationError{Field: "parts", Message: "a part's quantity must be greater than zero"}
		}
		// maintenance_log_parts' own PRIMARY KEY is (log_entry_id,
		// equipment_id) - the same part named twice would otherwise reach
		// insertMaintenanceLogPartsTx's second INSERT as a raw UNIQUE
		// constraint violation (a 500 with no field to point the caller
		// at), rather than the clean 400 a request-shape problem deserves.
		// The frontend's own parts picker already merges a repeated pick
		// into one row with a summed quantity; this is the same rule
		// enforced server-side for any other caller.
		if seenParts[id] {
			return maintenanceLogEntryInput{}, &inventoryValidationError{Field: "parts", Message: "a part can only be listed once - combine the quantity instead"}
		}
		seenParts[id] = true
		parts = append(parts, maintenanceLogPartInput{EquipmentID: id, Quantity: p.Quantity})
	}

	return maintenanceLogEntryInput{
		PerformedAt: performedAt,
		Hours:       req.Hours,
		Description: strings.TrimSpace(req.Description),
		Who:         strings.TrimSpace(req.Who),
		Cost:        req.Cost,
		Currency:    strings.TrimSpace(req.Currency),
		Parts:       parts,
	}, nil
}

// checkMaintenancePartsExist pre-checks every part's equipment_id against
// globalDocumentStore.GetEquipment BEFORE the store write ever runs - the
// same pre-check-here/real-constraint-there split
// patchEquipmentDocumentsHandler's own doc comment explains (inventory_
// handlers.go): a clean 404 naming the specific missing id, rather than
// letting the INSERT's own foreign key failure surface as an opaque 500
// with no id attached to it.
func checkMaintenancePartsExist(parts []maintenanceLogPartInput) error {
	for _, p := range parts {
		if _, err := globalDocumentStore.GetEquipment(p.EquipmentID); err != nil {
			if errors.Is(err, errEquipmentNotFound) {
				return fmt.Errorf("part %s not found: %w", p.EquipmentID, errEquipmentNotFound)
			}
			return err
		}
	}
	return nil
}

// ── rules: read ──────────────────────────────────────────────────────────

// listMaintenanceRulesHandler is GET /api/inventory/maintenance/rules
// ?equipment=&system=&include_stored=: every rule matching the given
// filters, each with a freshly computed status. Batches the per-equipment
// hours reading and meter-reset lookup ONCE per distinct equipment_id
// among the matching rules (equipmentColumns' own "a handful of items
// aboard one boat" reasoning, inventory_store.go, applies the same way
// here to several rules sharing one engine) rather than resolving it once
// per rule the way resolveMaintenanceRuleView does for a single-rule
// response.
func listMaintenanceRulesHandler(c echo.Context) error {
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}

	filter := maintenanceRuleFilter{
		EquipmentID:   c.QueryParam("equipment"),
		IncludeStored: c.QueryParam("include_stored") == "true",
	}
	rules, err := globalDocumentStore.ListMaintenanceRules(filter)
	if err != nil {
		return writeDocumentError(c, err)
	}

	systemFilter := c.QueryParam("system")
	now := time.Now().UTC()

	equipmentCache := map[string]equipmentItem{}
	hoursCache := map[string]maintenanceHourReading{}

	views := make([]maintenanceRuleView, 0, len(rules))
	for _, rule := range rules {
		if rule.EquipmentID == nil {
			if systemFilter != "" {
				// A calendar-only rule has no system to match against - a
				// system filter is asking for one system's own gear, which
				// a certificate/expiry rule is never part of.
				continue
			}
			views = append(views, buildMaintenanceRuleView(rule, nil, maintenanceHourReading{}, today))
			continue
		}

		id := *rule.EquipmentID
		eq, ok := equipmentCache[id]
		if !ok {
			eq, err = globalDocumentStore.GetEquipment(id)
			if err != nil {
				// The foreign key guarantees this row exists; a failure here
				// is a real problem, not an expected "not found" - surfaced
				// rather than silently dropping the rule from the list
				// (AGENTS.md fail-fast).
				return writeDocumentError(c, err)
			}
			equipmentCache[id] = eq
			hoursCache[id], err = equipmentHourReading(eq, now)
			if err != nil {
				return writeDocumentError(c, err)
			}
		}

		if systemFilter != "" && eq.System != systemFilter {
			continue
		}
		views = append(views, buildMaintenanceRuleView(rule, &eq, hoursCache[id], today))
	}

	return c.JSON(http.StatusOK, map[string]any{"rules": views})
}

func getMaintenanceRuleHandler(c echo.Context) error {
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	rule, err := globalDocumentStore.GetMaintenanceRule(c.Param("id"))
	if err != nil {
		return writeDocumentError(c, err)
	}
	view, err := resolveMaintenanceRuleView(rule, today)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"rule": view})
}

// ── rules: write ─────────────────────────────────────────────────────────

func createMaintenanceRuleHandler(c echo.Context) error {
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	limitNoteRequestBody(c)
	var req maintenanceRuleRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	in, verr := validateMaintenanceRuleInput(req)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	rule, err := globalDocumentStore.CreateMaintenanceRule(in)
	if err != nil {
		return writeDocumentError(c, err)
	}
	view, err := resolveMaintenanceRuleView(rule, today)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{"rule": view})
}

func updateMaintenanceRuleHandler(c echo.Context) error {
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	limitNoteRequestBody(c)
	var req maintenanceRuleRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	in, verr := validateMaintenanceRuleInput(req)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	rule, err := globalDocumentStore.UpdateMaintenanceRule(c.Param("id"), in)
	if err != nil {
		return writeDocumentError(c, err)
	}
	view, err := resolveMaintenanceRuleView(rule, today)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"rule": view})
}

func deleteMaintenanceRuleHandler(c echo.Context) error {
	if err := globalDocumentStore.DeleteMaintenanceRule(c.Param("id")); err != nil {
		return writeDocumentError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

type maintenanceAckRequest struct {
	Reason string `json:"reason"`
}

// acknowledgeMaintenanceRuleHandler is POST
// /api/inventory/maintenance/rules/:id/acknowledge: {reason}. A blank
// reason clears the acknowledgement (AcknowledgeMaintenanceRule's own doc
// comment, maintenance_store.go).
func acknowledgeMaintenanceRuleHandler(c echo.Context) error {
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	limitNoteRequestBody(c)
	var req maintenanceAckRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	rule, err := globalDocumentStore.AcknowledgeMaintenanceRule(c.Param("id"), req.Reason)
	if err != nil {
		return writeDocumentError(c, err)
	}
	view, err := resolveMaintenanceRuleView(rule, today)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"rule": view})
}

type maintenanceLastDoneRequest struct {
	LastDoneAt    *string  `json:"last_done_at"`
	LastDoneHours *float64 `json:"last_done_hours"`
}

// setMaintenanceRuleLastDoneHandler is POST
// /api/inventory/maintenance/rules/:id/last-done: {last_done_at?,
// last_done_hours?} - spec §3's onboarding action, writing NO log entry
// (SetMaintenanceRuleLastDone's own doc comment).
func setMaintenanceRuleLastDoneHandler(c echo.Context) error {
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	limitNoteRequestBody(c)
	var req maintenanceLastDoneRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if req.LastDoneAt == nil && req.LastDoneHours == nil {
		return writeInventoryValidationError(c, &inventoryValidationError{Field: "last_done_at", Message: "give a date and/or an hours reading"})
	}
	if req.LastDoneAt != nil {
		trimmed := strings.TrimSpace(*req.LastDoneAt)
		if trimmed != "" && !installDatePattern.MatchString(trimmed) {
			return writeInventoryValidationError(c, &inventoryValidationError{Field: "last_done_at", Message: "last_done_at must be blank or YYYY-MM-DD"})
		}
		req.LastDoneAt = &trimmed
	}
	if req.LastDoneHours != nil && *req.LastDoneHours < 0 {
		return writeInventoryValidationError(c, &inventoryValidationError{Field: "last_done_hours", Message: "last_done_hours cannot be negative"})
	}

	rule, err := globalDocumentStore.SetMaintenanceRuleLastDone(c.Param("id"), req.LastDoneAt, req.LastDoneHours)
	if err != nil {
		return writeDocumentError(c, err)
	}
	view, err := resolveMaintenanceRuleView(rule, today)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"rule": view})
}

// completeMaintenanceRuleHandler is POST
// /api/inventory/maintenance/rules/:id/complete - spec §6. kind is always
// 'maintenance' and equipment_id always comes from the rule; both are
// caller-fixed, never taken from the request body.
func completeMaintenanceRuleHandler(c echo.Context) error {
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	limitNoteRequestBody(c)
	var req maintenanceLogEntryRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	in, verr := validateMaintenanceLogEntryCore(req)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}

	existingRule, err := globalDocumentStore.GetMaintenanceRule(c.Param("id"))
	if err != nil {
		return writeDocumentError(c, err)
	}
	// An hours-interval rule can never sensibly complete without a
	// reading - live when the item's meter is bound and fresh, typed in by
	// hand from the gauge otherwise (spec: never guessed at). Refusing this
	// here, before the store ever runs, also protects
	// CompleteMaintenanceRule's own "blank hours leaves the baseline
	// unchanged" rule (maintenance_store.go) from ever being asked to
	// interpret a blank hours field for a rule that actually needs one.
	if existingRule.IntervalHours != nil && in.Hours == nil {
		return writeInventoryValidationError(c, &inventoryValidationError{
			Field:   "hours",
			Message: "hours is required to complete an hours-based rule - enter the current reading if it isn't filled in automatically",
		})
	}

	// A fixed-due-date rule (spec §7's certificates/expiries, or an
	// ordinary item rule that happens to carry one) must not read overdue
	// again the instant it's completed - its own due date has to advance.
	// Two cases, mirroring maintenance_status.go's own "fixed date wins,
	// interval_months is the fallback formula" shape:
	//   - interval_months set: the next due date is computed here, from
	//     THIS completion's own date, ignoring whatever new_due_date the
	//     request happened to carry - a formula exists, so the operator
	//     is never asked to do the arithmetic themselves.
	//   - interval_months not set: there is no formula at all (a one-off
	//     expiry), so the operator's own new_due_date is required - a
	//     blank or malformed one is refused rather than silently leaving
	//     the rule's due date exactly where it was.
	newFixedDueDate := ""
	if existingRule.FixedDueDate != "" {
		if existingRule.IntervalMonths != nil {
			completedAt, parseErr := time.Parse("2006-01-02", in.PerformedAt)
			if parseErr != nil {
				// performed_at already passed installDatePattern in
				// validateMaintenanceLogEntryCore; a failure here would be
				// this code disagreeing with itself, not an operator
				// mistake - surfaced rather than silently skipped.
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("parse performed_at: %v", parseErr)})
			}
			newFixedDueDate = completedAt.AddDate(0, *existingRule.IntervalMonths, 0).Format("2006-01-02")
		} else {
			trimmed := strings.TrimSpace(req.NewDueDate)
			if trimmed == "" || !installDatePattern.MatchString(trimmed) {
				return writeInventoryValidationError(c, &inventoryValidationError{
					Field:   "new_due_date",
					Message: "new_due_date is required and must be YYYY-MM-DD to complete a fixed-date rule with no monthly interval",
				})
			}
			newFixedDueDate = trimmed
		}
	}

	if err := checkMaintenancePartsExist(in.Parts); err != nil {
		if errors.Is(err, errEquipmentNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
		}
		return writeDocumentError(c, err)
	}

	rule, entry, err := globalDocumentStore.CompleteMaintenanceRule(c.Param("id"), in, newFixedDueDate)
	if err != nil {
		return writeDocumentError(c, err)
	}
	view, err := resolveMaintenanceRuleView(rule, today)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"rule": view, "entry": entry})
}

type maintenanceProcedureNoteRequest struct {
	NoteID string `json:"note_id"`
}

// setMaintenanceRuleProcedureNoteHandler is PUT
// /api/inventory/maintenance/rules/:id/procedure-note: {note_id}, blank
// clears the link. note_id must name a document whose kind is 'note' -
// checked here, since the store has no way to express "must be a note" as
// a foreign key (documents.kind is not part of any REFERENCES target).
func setMaintenanceRuleProcedureNoteHandler(c echo.Context) error {
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	limitNoteRequestBody(c)
	var req maintenanceProcedureNoteRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	noteID := strings.TrimSpace(req.NoteID)
	if noteID != "" {
		doc, err := globalDocumentStore.Get(noteID)
		if err != nil {
			return writeDocumentError(c, err)
		}
		if doc.Kind != "note" {
			return writeInventoryValidationError(c, &inventoryValidationError{Field: "note_id", Message: "note_id must be a note"})
		}
	}
	rule, err := globalDocumentStore.SetMaintenanceRuleProcedureNote(c.Param("id"), noteID)
	if err != nil {
		return writeDocumentError(c, err)
	}
	view, err := resolveMaintenanceRuleView(rule, today)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"rule": view})
}

// createMaintenanceProcedureNote writes a brand new Procedure-type note
// titled from title and links it - the minimal subset of createNoteHandler
// (notes_handlers.go) this feature needs: no folder, no tags, no request
// body of its own beyond what the caller already decided (the rule's own
// description). Kept as its own small helper rather than refactoring
// createNoteHandler to share code, the same reasoning ADR 0127's own
// "photo upload is a second write-tier file intake... duplicating its
// narrower path was judged lower risk" gives for a first cut - if this
// drifts from note-creation's own fixes the way that one did, it is the
// same candidate for a shared intake createNoteHandler's own upload path
// eventually became.
func createMaintenanceProcedureNote(title string) (document, error) {
	body := fmt.Sprintf("# %s\n\nProcedure steps for %s.\n", title, title)

	id := uuid.NewString()
	created := time.Now().UTC().Truncate(time.Second)
	rendered := renderNoteFile(noteFileMeta{ID: id, Title: title, Type: "procedure", Created: created}, body)
	if noteRenderedTooLarge(rendered) {
		return document{}, errNoteBodyTooLarge
	}
	sha := sha256Hex(rendered)

	enrich, _, err := documentEnrichFlag("maintenance: create procedure note")
	if err != nil {
		return document{}, err
	}

	dir := documentsDirPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return document{}, fmt.Errorf("failed to prepare document storage")
	}
	tmp, err := os.CreateTemp(dir, "upload-*.tmp")
	if err != nil {
		return document{}, fmt.Errorf("failed to create temp file")
	}
	tmpPath := tmp.Name()
	removeTemp := func() {
		if tmpPath != "" {
			os.Remove(tmpPath)
		}
	}
	if _, err := tmp.Write(rendered); err != nil {
		tmp.Close()
		removeTemp()
		return document{}, fmt.Errorf("failed to write note")
	}
	if err := tmp.Close(); err != nil {
		removeTemp()
		return document{}, fmt.Errorf("failed to write note")
	}

	unlockSHA := lockDocumentSHA(sha)
	defer unlockSHA()

	if existing, ok, err := globalDocumentStore.GetBySHA(sha); err != nil {
		removeTemp()
		return document{}, err
	} else if ok {
		removeTemp()
		return document{}, fmt.Errorf("note collides with existing document %s: %w", existing.ID, errDocumentDuplicate)
	}

	finalPath := filepath.Join(dir, sha)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		removeTemp()
		return document{}, fmt.Errorf("failed to store note")
	}
	tmpPath = ""

	inserted, err := globalDocumentStore.InsertNote(document{
		ID:             id,
		SHA256:         sha,
		Filename:       title + ".md",
		Title:          title,
		MIME:           "text/markdown",
		SizeBytes:      int64(len(rendered)),
		CreatedAt:      created,
		NoteType:       "procedure",
		NoteTypeSource: "operator",
		Enrich:         enrich,
	})
	if err != nil {
		os.Remove(finalPath)
		return document{}, err
	}
	wakeDocumentIndexer()
	return inserted, nil
}

// createMaintenanceProcedureNoteHandler is POST
// /api/inventory/maintenance/rules/:id/procedure-note - spec §8's "Create
// procedure note" action: a fresh Procedure note titled from the rule,
// linked to it. The rule's own description is used verbatim as the title;
// an operator is free to retitle the note afterward from Documents like
// any other.
func createMaintenanceProcedureNoteHandler(c echo.Context) error {
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	rule, err := globalDocumentStore.GetMaintenanceRule(c.Param("id"))
	if err != nil {
		return writeDocumentError(c, err)
	}

	title := strings.TrimSpace(rule.Description)
	if title == "" {
		title = "Procedure"
	}

	doc, err := createMaintenanceProcedureNote(title)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	updated, err := globalDocumentStore.SetMaintenanceRuleProcedureNote(rule.ID, doc.ID)
	if err != nil {
		return writeDocumentError(c, err)
	}
	view, err := resolveMaintenanceRuleView(updated, today)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{"rule": view, "note": toDocumentJSON(doc)})
}

// ── copy profile schedule ────────────────────────────────────────────────

// copyMaintenanceProfileScheduleHandler is POST
// /api/inventory/equipment/:id/maintenance/copy-profile-schedule - spec
// §1's "Use profile schedule" one-action copy. 409 when the item has no
// profile, or its profile is no longer loaded (deleted since the item was
// linked to it) - a real, expected conflict, not a 404 (the ITEM exists;
// its profile reference just doesn't resolve to anything right now).
func copyMaintenanceProfileScheduleHandler(c echo.Context) error {
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	id := c.Param("id")
	item, err := globalDocumentStore.GetEquipment(id)
	if err != nil {
		return writeDocumentError(c, err)
	}
	if item.ProfileID == "" {
		return c.JSON(http.StatusConflict, map[string]string{"error": "this item has no profile to copy a schedule from"})
	}

	profiles, _ := engineProfiles()
	var profile *engineProfile
	for i := range profiles {
		if profiles[i].ID == item.ProfileID {
			profile = &profiles[i]
			break
		}
	}
	if profile == nil {
		return c.JSON(http.StatusConflict, map[string]string{"error": fmt.Sprintf("profile %q is no longer available", item.ProfileID)})
	}

	created, err := globalDocumentStore.CopyProfileServiceEntries(id, profile.Service)
	if err != nil {
		return writeDocumentError(c, err)
	}

	now := time.Now().UTC()
	hours, err := equipmentHourReading(item, now)
	if err != nil {
		return writeDocumentError(c, err)
	}
	views := make([]maintenanceRuleView, len(created))
	for i, rule := range created {
		views[i] = buildMaintenanceRuleView(rule, &item, hours, today)
	}
	return c.JSON(http.StatusCreated, map[string]any{"rules": views})
}

// ── hour meter resets ────────────────────────────────────────────────────

type maintenanceMeterResetRequest struct {
	OldReading float64 `json:"old_reading"`
	NewReading float64 `json:"new_reading"`
	ChangedAt  string  `json:"changed_at"`
}

// recordHourMeterResetHandler is POST
// /api/inventory/equipment/:id/maintenance/meter-reset - spec §2's "records
// a meter change (old reading, new reading, date)".
func recordHourMeterResetHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	var req maintenanceMeterResetRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	changedAt := strings.TrimSpace(req.ChangedAt)
	if changedAt == "" || !installDatePattern.MatchString(changedAt) {
		return writeInventoryValidationError(c, &inventoryValidationError{Field: "changed_at", Message: "changed_at is required and must be YYYY-MM-DD"})
	}
	if req.OldReading < 0 || req.NewReading < 0 {
		return writeInventoryValidationError(c, &inventoryValidationError{Field: "old_reading", Message: "readings cannot be negative"})
	}

	reset, err := globalDocumentStore.RecordHourMeterReset(c.Param("id"), req.OldReading, req.NewReading, changedAt)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{"reset": reset})
}

// listHourMeterResetsHandler is GET
// /api/inventory/equipment/:id/maintenance/meter-resets - the equipment
// editor's own display of past replacements.
func listHourMeterResetsHandler(c echo.Context) error {
	resets, err := globalDocumentStore.ListHourMeterResets(c.Param("id"))
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"resets": resets})
}

// ── service log ──────────────────────────────────────────────────────────

func listMaintenanceLogEntriesHandler(c echo.Context) error {
	entries, err := globalDocumentStore.ListMaintenanceLogEntries(maintenanceLogFilter{EquipmentID: c.QueryParam("equipment")})
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"entries": entries})
}

func getMaintenanceLogEntryHandler(c echo.Context) error {
	entry, err := globalDocumentStore.GetMaintenanceLogEntry(c.Param("id"))
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"entry": entry})
}

// createMaintenanceLogEntryHandler is POST
// /api/inventory/maintenance/log - spec §6's standalone entries (repairs,
// improvements) against an item with no rule. equipment_id and kind are
// both required here (unlike completeMaintenanceRuleHandler, which fixes
// both itself) - a standalone entry has no rule to source either from.
func createMaintenanceLogEntryHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	var req maintenanceLogEntryRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	in, verr := validateMaintenanceLogEntryCore(req)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}

	equipmentID := trimStringPtr(req.EquipmentID)
	if equipmentID == nil {
		return writeInventoryValidationError(c, &inventoryValidationError{Field: "equipment_id", Message: "equipment_id is required"})
	}
	kind := strings.TrimSpace(req.Kind)
	if !validMaintenanceLogKinds[kind] {
		return writeInventoryValidationError(c, &inventoryValidationError{Field: "kind", Message: "kind must be maintenance, repair or improvement"})
	}
	in.EquipmentID = equipmentID
	in.Kind = kind

	if err := checkMaintenancePartsExist(in.Parts); err != nil {
		if errors.Is(err, errEquipmentNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
		}
		return writeDocumentError(c, err)
	}

	entry, err := globalDocumentStore.CreateMaintenanceLogEntry(in)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{"entry": entry})
}

// updateMaintenanceLogEntryHandler is PUT
// /api/inventory/maintenance/log/:id. equipment_id and rule_id are
// immutable once created (UpdateMaintenanceLogEntry's own doc comment,
// maintenance_store.go) - only kind, and the rest of the core fields, are
// taken from the request.
func updateMaintenanceLogEntryHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	var req maintenanceLogEntryRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	in, verr := validateMaintenanceLogEntryCore(req)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	kind := strings.TrimSpace(req.Kind)
	if !validMaintenanceLogKinds[kind] {
		return writeInventoryValidationError(c, &inventoryValidationError{Field: "kind", Message: "kind must be maintenance, repair or improvement"})
	}
	in.Kind = kind

	if err := checkMaintenancePartsExist(in.Parts); err != nil {
		if errors.Is(err, errEquipmentNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
		}
		return writeDocumentError(c, err)
	}

	entry, err := globalDocumentStore.UpdateMaintenanceLogEntry(c.Param("id"), in)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"entry": entry})
}

func deleteMaintenanceLogEntryHandler(c echo.Context) error {
	if err := globalDocumentStore.DeleteMaintenanceLogEntry(c.Param("id")); err != nil {
		return writeDocumentError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// ── service log photos ───────────────────────────────────────────────────
// Mirrors uploadEquipmentPhotoHandler/deleteEquipmentPhotoHandler
// (inventory_handlers.go) exactly, over maintenance_log_photos - spec §6:
// "reuse the shared upload intake".

// respondWithUpdatedLogEntry re-reads id and answers c with {entry} - the
// common tail of every log-photo write, the same shape
// respondWithUpdatedEquipment gives equipment photo writes.
func respondWithUpdatedLogEntry(c echo.Context, id string, status int) error {
	entry, err := globalDocumentStore.GetMaintenanceLogEntry(id)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(status, map[string]any{"entry": entry})
}

// uploadMaintenanceLogPhotoHandler is POST
// /api/inventory/maintenance/log/:id/photos (multipart, one "file" part).
func uploadMaintenanceLogPhotoHandler(c echo.Context) error {
	id := c.Param("id")
	if _, err := globalDocumentStore.GetMaintenanceLogEntry(id); err != nil {
		return writeDocumentError(c, err)
	}

	dir := documentsDirPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to prepare document storage"})
	}

	up, err := receiveUploadedFile(c, dir, nil)
	if err != nil {
		return err
	}

	mimeType := detectDocumentMIME(up.head, up.filename)
	switch mimeType {
	case "image/jpeg", "image/png":
		// accepted
	case "image/heic":
		os.Remove(up.tmpPath)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": documentHEICRejectionMessage})
	default:
		os.Remove(up.tmpPath)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "only JPEG or PNG photos are accepted"})
	}

	enrich, _, err := documentEnrichFlag("maintenance: upload log photo")
	if err != nil {
		os.Remove(up.tmpPath)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	doc := document{
		Filename:  up.filename,
		MIME:      mimeType,
		SizeBytes: up.size,
		Enrich:    enrich,
	}

	return storeUploadedFile(dir, up, doc,
		func(existing document) error {
			if err := globalDocumentStore.AddMaintenanceLogPhoto(id, existing.ID); err != nil {
				return writeDocumentError(c, err)
			}
			return respondWithUpdatedLogEntry(c, id, http.StatusOK)
		},
		func(inserted document) error {
			if err := globalDocumentStore.AddMaintenanceLogPhoto(id, inserted.ID); err != nil {
				return writeDocumentError(c, err)
			}
			wakeDocumentIndexer()
			return respondWithUpdatedLogEntry(c, id, http.StatusCreated)
		},
		func(err error) error {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		},
	)
}

// deleteMaintenanceLogPhotoHandler is DELETE
// /api/inventory/maintenance/log/:id/photos/:documentId[?delete=true] -
// unlink only by default; delete=true additionally deletes the document
// once MaintenanceLogPhotoDeletableAsOrphan confirms nothing else (another
// log entry, or an equipment item's own strip) still shows it.
func deleteMaintenanceLogPhotoHandler(c echo.Context) error {
	id := c.Param("id")
	documentID := c.Param("documentId")
	deleteDoc := c.QueryParam("delete") == "true"

	doc, err := globalDocumentStore.Get(documentID)
	if err != nil {
		return writeDocumentError(c, err)
	}
	unlockSHA := lockDocumentSHA(doc.SHA256)
	defer unlockSHA()

	if err := globalDocumentStore.RemoveMaintenanceLogPhoto(id, documentID); err != nil {
		return writeDocumentError(c, err)
	}

	if deleteDoc {
		deletable, err := globalDocumentStore.MaintenanceLogPhotoDeletableAsOrphan(documentID)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("the photo was removed from the log entry, but checking whether it was safe to delete failed: %v", err),
			})
		}
		if deletable {
			sha, err := globalDocumentStore.Delete(documentID)
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{
					"error": fmt.Sprintf("the photo was removed from the log entry, but deleting it failed: %v", err),
				})
			}
			if err := removeDocumentFile(sha); err != nil {
				log.Printf("maintenance: delete log photo: failed to remove file %s: %v", filepath.Join(documentsDirPath(), sha), err)
				return c.JSON(http.StatusInternalServerError, map[string]string{
					"error": "the photo was removed from the log entry, but its file could not be removed from disk",
				})
			}
		}
	}
	return respondWithUpdatedLogEntry(c, id, http.StatusOK)
}

// ── CSV export ───────────────────────────────────────────────────────────

// exportMaintenanceLogCSVHandler is GET
// /api/inventory/maintenance/log/export.csv[?equipment=] - spec §10: every
// entry, or filtered to one item, as text/csv with columns date, item,
// rule, kind, hours, description, who, cost, currency, parts. Item/rule
// names are resolved once per distinct id among the exported entries
// (the same "batch, don't N+1" reasoning listMaintenanceRulesHandler's own
// doc comment gives) rather than once per row.
func exportMaintenanceLogCSVHandler(c echo.Context) error {
	entries, err := globalDocumentStore.ListMaintenanceLogEntries(maintenanceLogFilter{EquipmentID: c.QueryParam("equipment")})
	if err != nil {
		return writeDocumentError(c, err)
	}

	itemNames := map[string]string{}
	ruleDescriptions := map[string]string{}
	for _, e := range entries {
		if e.EquipmentID != nil {
			if _, ok := itemNames[*e.EquipmentID]; !ok {
				if item, err := globalDocumentStore.GetEquipment(*e.EquipmentID); err == nil {
					itemNames[*e.EquipmentID] = item.Name
				}
			}
		}
		if e.RuleID != nil {
			if _, ok := ruleDescriptions[*e.RuleID]; !ok {
				if rule, err := globalDocumentStore.GetMaintenanceRule(*e.RuleID); err == nil {
					ruleDescriptions[*e.RuleID] = rule.Description
				}
			}
		}
	}

	c.Response().Header().Set(echo.HeaderContentType, "text/csv; charset=utf-8")
	c.Response().Header().Set(echo.HeaderContentDisposition, `attachment; filename="maintenance-log.csv"`)
	c.Response().WriteHeader(http.StatusOK)

	w := csv.NewWriter(c.Response())
	if err := w.Write([]string{"date", "item", "rule", "kind", "hours", "description", "who", "cost", "currency", "parts"}); err != nil {
		return err
	}
	for _, e := range entries {
		itemName := ""
		if e.EquipmentID != nil {
			itemName = itemNames[*e.EquipmentID]
		}
		ruleDescription := ""
		if e.RuleID != nil {
			ruleDescription = ruleDescriptions[*e.RuleID]
		}
		hours := ""
		if e.Hours != nil {
			hours = strconv.FormatFloat(*e.Hours, 'f', -1, 64)
		}
		cost := ""
		if e.Cost != nil {
			cost = strconv.FormatFloat(*e.Cost, 'f', 2, 64)
		}
		parts := make([]string, len(e.Parts))
		for i, p := range e.Parts {
			parts[i] = fmt.Sprintf("%s x%s", p.EquipmentName, strconv.FormatFloat(p.Quantity, 'f', -1, 64))
		}
		row := []string{e.PerformedAt, itemName, ruleDescription, e.Kind, hours, e.Description, e.Who, cost, e.Currency, strings.Join(parts, "; ")}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
