package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
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
// and, for the actions that are really about one item
// (meter-reset, profile-change-preview), under
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
	ID               string   `json:"id"`
	EquipmentID      *string  `json:"equipment_id"`
	EquipmentName    string   `json:"equipment_name"`
	System           string   `json:"system"`
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
	Acknowledged     bool     `json:"acknowledged"`
	// CreatedAt/UpdatedAt are null for a profile job nothing has been written
	// to yet: it has no row, so no timestamps to report.
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`

	// Where the rule comes from (ADR 0148). Description and the two intervals
	// above are EFFECTIVE: for a profile job ("profile") the profile's live
	// values with this item's overrides applied, for a hand rule ("item") the
	// stored ones. ProfileValues is what Reset to profile would restore (null
	// for a hand rule); FirstAtHours and Supersedes are the profile's, shown
	// read-only. RemovedFromProfile marks a parked job: its service has left
	// the item's profile, so it has no status and is never due.
	Source             string                    `json:"source"`
	ProfileID          string                    `json:"profile_id"`
	OverriddenFields   []string                  `json:"overridden_fields"`
	NotApplicable      bool                      `json:"not_applicable"`
	ProfileValues      *maintenanceProfileValues `json:"profile_values"`
	FirstAtHours       *float64                  `json:"first_at_hours"`
	Supersedes         []string                  `json:"supersedes"`
	RemovedFromProfile bool                      `json:"removed_from_profile"`

	// Computed fresh on every response (maintenance_status.go) - never
	// stored.
	Status           string   `json:"status"`
	RemainingHours   *float64 `json:"remaining_hours"`
	RemainingDays    *int     `json:"remaining_days"`
	HoursUnknown     bool     `json:"hours_unknown"`
	HasHourMeterPath bool     `json:"has_hour_meter_path"`
	// CurrentHours is the item's own live GAUGE (raw meter) reading right
	// now (nil unless Hours.Known) - deliberately the RAW figure, not true
	// hours, because the frontend's Complete/last-done/log-entry forms
	// prefill their own "Hours" field with this and the operator always
	// works in gauge readings (2026-09-27 amendment, gaugeToTrueHours -
	// maintenance_hours.go); the server converts back to true hours, with
	// whichever offset was in force on the date given, when it stores
	// whatever the operator actually submits.
	CurrentHours *float64 `json:"current_hours"`
	// HoursAsOf (RFC3339) is set whenever CurrentHours is - the wall-clock
	// instant that live reading was last received, so the frontend can show
	// "as of <time>" (2026-09-27 amendment: an hour meter's last value is
	// always current, however old, since it only changes while its engine
	// runs - see currentEquipmentHours' own doc comment,
	// maintenance_hours.go). Null when CurrentHours itself is null, or on
	// the rare fixture with no timestamp evidence at all for an otherwise
	// live reading.
	HoursAsOf *string `json:"hours_as_of"`
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
func buildMaintenanceRuleView(eff effectiveMaintenanceRule, eq *equipmentItem, hours maintenanceHourReading, today time.Time) maintenanceRuleView {
	rule := eff.maintenanceRule
	var result maintenanceRuleStatusResult
	switch {
	case eff.RemovedFromProfile:
		// Parked: no status at all, never due.
	case rule.NotApplicable:
		result = maintenanceRuleStatusResult{Status: maintenanceStatusNotApplicable}
	default:
		result = computeMaintenanceRuleStatus(maintenanceRuleStatusInput{
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
	}

	supersedes := eff.Supersedes
	if supersedes == nil {
		supersedes = []string{}
	}
	overridden := rule.OverriddenFields
	if overridden == nil {
		overridden = []string{}
	}
	view := maintenanceRuleView{
		ID:                 rule.ID,
		EquipmentID:        rule.EquipmentID,
		Description:        rule.Description,
		IntervalHours:      rule.IntervalHours,
		IntervalMonths:     rule.IntervalMonths,
		DueSoonHours:       rule.DueSoonHours,
		DueSoonMonths:      rule.DueSoonMonths,
		FixedDueDate:       rule.FixedDueDate,
		LastDoneAt:         rule.LastDoneAt,
		LastDoneHours:      rule.LastDoneHours,
		ProfileServiceID:   rule.ProfileServiceID,
		ProcedureNoteID:    rule.ProcedureNoteID,
		AckReason:          rule.AckReason,
		Acknowledged:       rule.Acknowledged,
		CreatedAt:          timePtrUnlessZero(rule.CreatedAt),
		UpdatedAt:          timePtrUnlessZero(rule.UpdatedAt),
		Source:             eff.Source,
		ProfileID:          eff.ProfileID,
		OverriddenFields:   overridden,
		NotApplicable:      rule.NotApplicable,
		ProfileValues:      eff.ProfileValues,
		FirstAtHours:       eff.FirstAtHours,
		Supersedes:         supersedes,
		RemovedFromProfile: eff.RemovedFromProfile,
		Status:             string(result.Status),
		RemainingHours:     result.RemainingHours,
		RemainingDays:      result.RemainingDays,
		HoursUnknown:       result.HoursUnknown,
	}
	if eq != nil {
		view.EquipmentName = eq.Name
		view.System = eq.System
		view.HasHourMeterPath = strings.TrimSpace(eq.HourMeterPath) != ""
	}
	if hours.AsOf != nil {
		s := hours.AsOf.UTC().Format(time.RFC3339)
		view.HoursAsOf = &s
	}
	if hours.Known {
		g := hours.Gauge
		view.CurrentHours = &g
	}
	return view
}

// equipmentHourReading resolves eq's current true hours (live value plus
// its own currently-in-force meter-reset offset) - shared by
// resolveMaintenanceRuleView (one rule) and listMaintenanceRulesHandler's
// own per-equipment cache (many rules), so the two can never disagree about
// how an item's hours are computed.
//
// An item with no hour_meter_path at all has nothing here for the engine to
// read live (currentEquipmentHours' own doc comment) - checked BEFORE
// ListHourMeterResets runs (2026-09-27 code-review finding), not after,
// since a meter-reset history is only ever meaningful for an item with a
// live path to apply its offset to; every rule aboard a boat's own gear
// with no wired meter (a genset with a mechanical-only hour meter, say)
// used to cost a reset-history query for nothing.
func equipmentHourReading(eq equipmentItem, now time.Time) (maintenanceHourReading, error) {
	if strings.TrimSpace(eq.HourMeterPath) == "" {
		return maintenanceHourReading{}, nil
	}
	resets, err := globalDocumentStore.ListHourMeterResets(eq.ID)
	if err != nil {
		return maintenanceHourReading{}, err
	}
	offset := offsetInForceAt(resets, now)
	reader := snapshotAlarmReader(globalSignalKSnapshot)
	return currentEquipmentHours(reader, globalSignalKSnapshot, eq.HourMeterPath, offset, now), nil
}

// convertGaugeHoursToTrue looks up equipmentID's own hour-meter reset
// history and converts a GAUGE (raw meter) reading FROM atDate into true
// hours (gaugeToTrueHours, maintenance_hours.go). Every endpoint that
// accepts an operator-typed hours figure destined for a rule's own
// last_done_hours or a log entry's own hours column shares this one
// conversion (2026-09-27 amendment - docs/adr/0138) - completing a rule,
// setting its baseline by hand, and a standalone log entry's own hours
// field all mean the same thing by "hours" once this has run, so gauge and
// true can never quietly diverge into two competing meanings of the word
// between them.
func convertGaugeHoursToTrue(equipmentID string, gauge float64, atDate time.Time) (float64, error) {
	resets, err := globalDocumentStore.ListHourMeterResets(equipmentID)
	if err != nil {
		return 0, err
	}
	return gaugeToTrueHours(resets, gauge, atDate), nil
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
func resolveMaintenanceRuleView(id string, today time.Time) (maintenanceRuleView, error) {
	eff, err := globalDocumentStore.ResolveMaintenanceRule(id)
	if err != nil {
		return maintenanceRuleView{}, err
	}
	return viewForEffectiveRule(eff, today)
}

// viewForEffectiveRule is resolveMaintenanceRuleView for a rule already
// resolved by the schedule.
func viewForEffectiveRule(eff effectiveMaintenanceRule, today time.Time) (maintenanceRuleView, error) {
	now := time.Now().UTC()
	if eff.EquipmentID == nil {
		return buildMaintenanceRuleView(eff, nil, maintenanceHourReading{}, today), nil
	}
	eq, err := globalDocumentStore.GetEquipment(*eff.EquipmentID)
	if err != nil {
		return maintenanceRuleView{}, err
	}
	hours, err := equipmentHourReading(eq, now)
	if err != nil {
		return maintenanceRuleView{}, err
	}
	return buildMaintenanceRuleView(eff, &eq, hours, today), nil
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
//     required (an "interval not set" slot exists only as a profile job).
//   - profile_service_id must be blank: a rule by hand is never a profile job.
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

	// A rule made by hand never belongs to a profile service: profile jobs
	// come from the profile itself (maintenance_schedule.go), so this request
	// shape cannot name one.
	if strings.TrimSpace(req.ProfileServiceID) != "" {
		return maintenanceRuleInput{}, &inventoryValidationError{
			Field:   "profile_service_id",
			Message: "profile_service_id cannot be set: a profile's jobs come from the equipment profile, not from rules created by hand",
		}
	}
	hasInterval := req.IntervalHours != nil || req.IntervalMonths != nil || fixedDueDate != ""
	if !hasInterval {
		return maintenanceRuleInput{}, &inventoryValidationError{
			Field:   "interval_hours",
			Message: "at least one of interval_hours, interval_months or a fixed due date is required",
		}
	}

	return maintenanceRuleInput{
		EquipmentID:    equipmentID,
		Description:    description,
		IntervalHours:  req.IntervalHours,
		IntervalMonths: req.IntervalMonths,
		DueSoonHours:   req.DueSoonHours,
		DueSoonMonths:  req.DueSoonMonths,
		FixedDueDate:   fixedDueDate,
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
// filters, each with a freshly computed status. The system filter is
// applied in SQL (ListMaintenanceRules' own doc comment) - a rule outside
// the requested system is never in `rules` at all, so it never costs an
// equipment fetch or an hours/meter-reset lookup here, only to be discarded
// afterward (2026-09-27 code-review finding). Batches the per-equipment
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
		System:        c.QueryParam("system"),
	}
	sched, err := globalDocumentStore.MaintenanceSchedule(filter)
	if err != nil {
		return writeDocumentError(c, err)
	}

	now := time.Now().UTC()

	equipmentCache := map[string]equipmentItem{}
	hoursCache := map[string]maintenanceHourReading{}

	viewOf := func(rule effectiveMaintenanceRule) (maintenanceRuleView, error) {
		if rule.EquipmentID == nil {
			return buildMaintenanceRuleView(rule, nil, maintenanceHourReading{}, today), nil
		}
		id := *rule.EquipmentID
		eq, ok := equipmentCache[id]
		if !ok {
			var err error
			eq, err = globalDocumentStore.GetEquipment(id)
			if err != nil {
				// The foreign key guarantees this row exists; a failure here
				// is a real problem, not an expected "not found" - surfaced
				// rather than silently dropping the rule from the list
				// (AGENTS.md fail-fast).
				return maintenanceRuleView{}, err
			}
			equipmentCache[id] = eq
			hoursCache[id], err = equipmentHourReading(eq, now)
			if err != nil {
				return maintenanceRuleView{}, err
			}
		}
		return buildMaintenanceRuleView(rule, &eq, hoursCache[id], today), nil
	}

	views := make([]maintenanceRuleView, 0, len(sched.Rules))
	for _, rule := range sched.Rules {
		view, err := viewOf(rule)
		if err != nil {
			return writeDocumentError(c, err)
		}
		views = append(views, view)
	}
	removed := make([]maintenanceRuleView, 0, len(sched.Removed))
	for _, rule := range sched.Removed {
		view, err := viewOf(rule)
		if err != nil {
			return writeDocumentError(c, err)
		}
		removed = append(removed, view)
	}

	return c.JSON(http.StatusOK, map[string]any{"rules": views, "removed": removed, "schedule_errors": sched.Errors})
}

func getMaintenanceRuleHandler(c echo.Context) error {
	if err := decodeRuleIDParam(c); err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	view, err := resolveMaintenanceRuleView(c.Param("id"), today)
	if err != nil {
		return writeMaintenanceCommandError(c, err)
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
	var rule maintenanceRule
	err := globalDocumentStore.RunTx(func(tx *sql.Tx, now time.Time) error {
		var err error
		rule, err = cmdCreateMaintenanceRule(tx, now, req)
		return err
	})
	if err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	view, err := resolveMaintenanceRuleView(rule.ID, today)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{"rule": view})
}

func updateMaintenanceRuleHandler(c echo.Context) error {
	if err := decodeRuleIDParam(c); err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	limitNoteRequestBody(c)
	var req maintenanceRuleRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	var rule maintenanceRule
	err := globalDocumentStore.RunTx(func(tx *sql.Tx, now time.Time) error {
		var err error
		rule, err = cmdUpdateMaintenanceRule(tx, now, c.Param("id"), req)
		return err
	})
	if err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	view, err := resolveMaintenanceRuleView(rule.ID, today)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"rule": view})
}

// deleteMaintenanceRuleHandler is DELETE /api/inventory/maintenance/rules/:id:
// a hand rule, or a job row whose service has left its profile. A live profile
// job is a 400 (cmdDeleteMaintenanceRule).
func deleteMaintenanceRuleHandler(c echo.Context) error {
	if err := decodeRuleIDParam(c); err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	if err := globalDocumentStore.DeleteMaintenanceRule(c.Param("id")); err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// setMaintenanceRuleOverridesHandler is PUT
// /api/inventory/maintenance/rules/:id/overrides: any subset of
// {description, interval_hours, interval_months, not_applicable} for one
// profile job. An interval given as null overrides it to "none".
func setMaintenanceRuleOverridesHandler(c echo.Context) error {
	if err := decodeRuleIDParam(c); err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	limitNoteRequestBody(c)
	var raw map[string]json.RawMessage
	if err := c.Bind(&raw); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	in, verr := parseMaintenanceOverrides(raw)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	var rule maintenanceRule
	err := globalDocumentStore.RunTx(func(tx *sql.Tx, now time.Time) error {
		var err error
		rule, err = cmdSetMaintenanceRuleOverrides(tx, now, c.Param("id"), in)
		return err
	})
	if err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	view, err := resolveMaintenanceRuleView(rule.ID, today)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"rule": view})
}

// resetMaintenanceRuleOverrideHandler is DELETE
// /api/inventory/maintenance/rules/:id/overrides/:field.
func resetMaintenanceRuleOverrideHandler(c echo.Context) error {
	if err := decodeRuleIDParam(c); err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	var rule maintenanceRule
	err := globalDocumentStore.RunTx(func(tx *sql.Tx, now time.Time) error {
		var err error
		rule, err = cmdResetMaintenanceRuleOverride(tx, now, c.Param("id"), c.Param("field"))
		return err
	})
	if err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	view, err := resolveMaintenanceRuleView(rule.ID, today)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"rule": view})
}

// parseMaintenanceOverrides reads an overrides body. It distinguishes a field
// that is absent (left alone) from one that is null (an interval overridden to
// none), which a typed struct cannot.
func parseMaintenanceOverrides(raw map[string]json.RawMessage) (maintenanceOverridesInput, *inventoryValidationError) {
	var in maintenanceOverridesInput
	for name, value := range raw {
		bad := func(msg string) *inventoryValidationError {
			return &inventoryValidationError{Field: name, Message: name + " " + msg}
		}
		switch name {
		case overrideDescription:
			var v string
			if err := json.Unmarshal(value, &v); err != nil {
				return in, bad("must be a string")
			}
			in.Description = &v
		case overrideIntervalHours:
			var v *float64
			if err := json.Unmarshal(value, &v); err != nil {
				return in, bad("must be a number or null")
			}
			in.IntervalHours = &maintenanceIntervalHoursOverride{Value: v}
		case overrideIntervalMonths:
			var v *int
			if err := json.Unmarshal(value, &v); err != nil {
				return in, bad("must be a whole number or null")
			}
			in.IntervalMonths = &maintenanceIntervalMonthsOverride{Value: v}
		case overrideNotApplicable:
			var v bool
			if err := json.Unmarshal(value, &v); err != nil {
				return in, bad("must be true or false")
			}
			in.NotApplicable = &v
		case "due_soon_hours":
			var v *float64
			if err := json.Unmarshal(value, &v); err != nil {
				return in, bad("must be a number or null")
			}
			in.DueSoonHours = &maintenanceNullableFloat{Value: v}
		case "due_soon_months":
			var v *int
			if err := json.Unmarshal(value, &v); err != nil {
				return in, bad("must be a whole number or null")
			}
			in.DueSoonMonths = &maintenanceNullableInt{Value: v}
		case "fixed_due_date":
			var v *string
			if err := json.Unmarshal(value, &v); err != nil {
				return in, bad("must be YYYY-MM-DD or null")
			}
			blank := ""
			if v == nil {
				v = &blank
			}
			in.FixedDueDate = v
		default:
			return in, &inventoryValidationError{Field: name, Message: name + " cannot be set here; the fields are " +
				strings.Join(maintenanceOverrideFields, ", ") + ", due_soon_hours, due_soon_months, fixed_due_date"}
		}
	}
	return in, nil
}

// maintenanceProfileChangePreviewHandler is GET
// /api/inventory/equipment/:id/maintenance/profile-change-preview
// ?profile_id=<new profile, or empty to clear>: which of the item's profile
// jobs carry over, leave, or arrive if its profile were changed.
func maintenanceProfileChangePreviewHandler(c echo.Context) error {
	id := c.Param("id")
	newProfileID := strings.TrimSpace(c.QueryParam("profile_id"))
	preview, err := globalDocumentStore.PreviewMaintenanceProfileChange(id, newProfileID)
	if err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	return c.JSON(http.StatusOK, preview)
}

type maintenanceAckRequest struct {
	Reason string `json:"reason"`
}

// acknowledgeMaintenanceRuleHandler is POST
// /api/inventory/maintenance/rules/:id/acknowledge: {reason}. A blank
// reason clears the acknowledgement (AcknowledgeMaintenanceRule's own doc
// comment, maintenance_store.go).
func acknowledgeMaintenanceRuleHandler(c echo.Context) error {
	if err := decodeRuleIDParam(c); err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	limitNoteRequestBody(c)
	var req maintenanceAckRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	var rule maintenanceRule
	err := globalDocumentStore.RunTx(func(tx *sql.Tx, now time.Time) error {
		var err error
		rule, err = cmdAcknowledgeMaintenanceRule(tx, now, c.Param("id"), req.Reason)
		return err
	})
	if err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	view, err := resolveMaintenanceRuleView(rule.ID, today)
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
	if err := decodeRuleIDParam(c); err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	limitNoteRequestBody(c)
	var req maintenanceLastDoneRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	var rule maintenanceRule
	err := globalDocumentStore.RunTx(func(tx *sql.Tx, now time.Time) error {
		var err error
		rule, err = cmdSetMaintenanceRuleLastDone(tx, now, today, c.Param("id"), req)
		return err
	})
	if err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	view, err := resolveMaintenanceRuleView(rule.ID, today)
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
	if err := decodeRuleIDParam(c); err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	limitNoteRequestBody(c)
	var req maintenanceLogEntryRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	var rule maintenanceRule
	var entry maintenanceLogEntry
	err := globalDocumentStore.RunTx(func(tx *sql.Tx, now time.Time) error {
		var err error
		rule, entry, err = cmdCompleteMaintenanceRule(tx, now, c.Param("id"), req)
		return err
	})
	if err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	view, err := resolveMaintenanceRuleView(rule.ID, today)
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
	if err := decodeRuleIDParam(c); err != nil {
		return writeMaintenanceCommandError(c, err)
	}
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
		return writeMaintenanceCommandError(c, err)
	}
	view, err := resolveMaintenanceRuleView(rule.ID, today)
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
	if err := decodeRuleIDParam(c); err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	// The effective description titles the note; a job the profile cannot
	// supply refuses here, before a note is made for it.
	rule, err := globalDocumentStore.ResolveMaintenanceRule(c.Param("id"))
	if err != nil {
		return writeMaintenanceCommandError(c, err)
	}

	// Refuse before the note exists: linking a removed job 409s, and the note
	// written first would be left behind as an orphan.
	if err := refuseRemovedJobWrite(rule); err != nil {
		return writeMaintenanceCommandError(c, err)
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
		return writeMaintenanceCommandError(c, err)
	}
	view, err := resolveMaintenanceRuleView(updated.ID, today)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{"rule": view, "note": toDocumentJSON(doc)})
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

	// The operator always types a GAUGE (raw meter) reading (2026-09-27
	// amendment) - same rule as completing a rule, converted here with the
	// offset in force on this entry's own performed_at.
	if in.Hours != nil {
		performedAt, parseErr := time.Parse("2006-01-02", in.PerformedAt)
		if parseErr != nil {
			// Already passed installDatePattern in
			// validateMaintenanceLogEntryCore.
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("parse performed_at: %v", parseErr)})
		}
		trueHours, err := convertGaugeHoursToTrue(*equipmentID, *in.Hours, performedAt)
		if err != nil {
			return writeDocumentError(c, err)
		}
		in.Hours = &trueHours
	}

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

	// Same gauge-to-true conversion as create, but equipment_id is
	// immutable (this file's own doc comment above) - never taken from the
	// request, so the existing entry has to be read back to learn it.
	if in.Hours != nil {
		existing, err := globalDocumentStore.GetMaintenanceLogEntry(c.Param("id"))
		if err != nil {
			return writeDocumentError(c, err)
		}
		if existing.EquipmentID != nil {
			performedAt, parseErr := time.Parse("2006-01-02", in.PerformedAt)
			if parseErr != nil {
				// Already passed installDatePattern in
				// validateMaintenanceLogEntryCore.
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("parse performed_at: %v", parseErr)})
			}
			trueHours, err := convertGaugeHoursToTrue(*existing.EquipmentID, *in.Hours, performedAt)
			if err != nil {
				return writeDocumentError(c, err)
			}
			in.Hours = &trueHours
		}
	}

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
	type ruleLabel struct{ description, schedule string }
	ruleLabels := map[string]ruleLabel{}
	profiles := currentMaintenanceProfiles()
	for _, e := range entries {
		if e.EquipmentID != nil {
			if _, ok := itemNames[*e.EquipmentID]; !ok {
				if item, err := globalDocumentStore.GetEquipment(*e.EquipmentID); err == nil {
					itemNames[*e.EquipmentID] = item.Name
				}
			}
		}
		if e.RuleID != nil {
			if _, ok := ruleLabels[*e.RuleID]; !ok {
				description, schedule, err := globalDocumentStore.MaintenanceRuleHistoryLabel(profiles, *e.RuleID)
				if err != nil {
					return writeDocumentError(c, err)
				}
				ruleLabels[*e.RuleID] = ruleLabel{description, schedule}
			}
		}
	}

	c.Response().Header().Set(echo.HeaderContentType, "text/csv; charset=utf-8")
	c.Response().Header().Set(echo.HeaderContentDisposition, `attachment; filename="maintenance-log.csv"`)
	c.Response().WriteHeader(http.StatusOK)

	w := csv.NewWriter(c.Response())
	// "hours (true)" - every stored hours figure is always true hours
	// (gauge plus whatever meter-reset offset was in force when it was
	// logged, gaugeToTrueHours - maintenance_hours.go), never the raw
	// gauge reading the operator actually typed in; named explicitly so
	// the export is never ambiguous read back later.
	if err := w.Write([]string{"date", "item", "rule", "kind", "hours (true)", "description", "who", "cost", "currency", "parts", "schedule"}); err != nil {
		return err
	}
	for _, e := range entries {
		itemName := ""
		if e.EquipmentID != nil {
			itemName = itemNames[*e.EquipmentID]
		}
		ruleDescription, schedule := "", ""
		if e.RuleID != nil {
			ruleDescription, schedule = ruleLabels[*e.RuleID].description, ruleLabels[*e.RuleID].schedule
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
		row := []string{e.PerformedAt, itemName, ruleDescription, e.Kind, hours, e.Description, e.Who, cost, e.Currency, strings.Join(parts, "; "), schedule}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func timePtrUnlessZero(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// decodeRuleIDParam replaces the :id param with its decoded form. Echo prefers
// URL.RawPath, so a job id the browser sent through encodeURIComponent
// (job%3A<uuid>%3A<service>) arrives still escaped and matches no job. A
// malformed escape is a 400.
func decodeRuleIDParam(c echo.Context) error {
	raw := c.Param("id")
	id, err := url.PathUnescape(raw)
	if err != nil {
		return &maintenanceCommandError{Status: http.StatusBadRequest, Message: "malformed maintenance rule id"}
	}
	if id == raw {
		return nil
	}
	names := c.ParamNames()
	values := c.ParamValues()
	for i, n := range names {
		if n == "id" && i < len(values) {
			values[i] = id
		}
	}
	c.SetParamValues(values...)
	return nil
}
