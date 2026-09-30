package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Mate's maintenance read tools (ADR 0145): find_equipment, list_maintenance
// and get_maintenance_log. All three only read. The status on every rule row
// is the one resolveMaintenanceRuleView computes (ADR 0138) against the
// operator's own local date, which arrives on the message POST as today;
// nothing here works out due or overdue for itself.
//
// resolveMaintenanceRuleView and equipmentHourReading read globalDocumentStore,
// so d.documents and that global are the same store in production. A test that
// injects a different store into d.documents must set the global to match.

// assistantMaintenanceLink is where a rule or log row is cited: the
// Inventory panel's Maintenance section. The section has no per-rule URL, so
// every rule and log row shares this one.
const assistantMaintenanceLink = "/inventory/maintenance"

// assistantEquipmentLink is an item's editor page, the same
// /inventory/equipment/<id> route formatAppLocation produces.
func assistantEquipmentLink(id string) string {
	return "/inventory/equipment/" + id
}

const (
	assistantFindEquipmentDefaultLimit  = 10
	assistantFindEquipmentMaxLimit      = 20
	assistantMaintenanceLogDefaultLimit = 20
	assistantMaintenanceLogMaxLimit     = 50
)

// maintenanceStore resolves the store the maintenance tools read. Rules, the
// log and the equipment registry all live in the document store's database,
// so it fails the same way search_documents does when that is unset, but in
// the maintenance tools' own words.
func (d assistantToolDeps) maintenanceStore(toolName string) (*documentStore, error) {
	if d.documents == nil {
		return nil, fmt.Errorf("%s: the maintenance data is not available", toolName)
	}
	store := d.documents()
	if store == nil {
		return nil, fmt.Errorf("%s: the maintenance data is not available", toolName)
	}
	return store, nil
}

// requireToday is the maintenance tools' own fail-fast: with no date from the
// operator's browser there is nothing to compute a status against, and the
// server's clock is not the boat's date (ADR 0138's today amendment).
func (d assistantToolDeps) requireToday(toolName string) (time.Time, error) {
	if d.today.IsZero() {
		return time.Time{}, fmt.Errorf("%s: the operator's date (today) was not supplied with this message, so no status can be worked out", toolName)
	}
	return d.today, nil
}

// ── find_equipment ──────────────────────────────────────────────────────

type assistantFindEquipmentArgs struct {
	Query  string `json:"query"`
	System string `json:"system"`
	Limit  int    `json:"limit"`
}

type assistantEquipmentHit struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty"`
	System       string `json:"system"`
	Status       string `json:"status"`
	// HourMeterBound is whether the item has a live hour-meter path at all;
	// HoursKnown is whether that path is reading right now.
	// CurrentMeterReading is what the meter itself shows (the gauge reading
	// the operator works in) and is omitted, never 0, whenever HoursKnown is
	// false.
	HourMeterBound      bool     `json:"hour_meter_bound"`
	HoursKnown          bool     `json:"hours_known"`
	CurrentMeterReading *float64 `json:"current_meter_reading,omitempty"`
	HoursAsOf           string   `json:"hours_as_of,omitempty"`
	RuleCount           int      `json:"rule_count"`
	ProfileID           string   `json:"profile_id,omitempty"`
	// Rules is the item's effective schedule from the resolver: the jobs its
	// equipment profile supplies (live, with this item's overrides applied)
	// plus the rules made by hand. Same field names as list_maintenance,
	// minus the status, which only list_maintenance works out.
	Rules []assistantEquipmentRule `json:"rules"`
	// ScheduleErrors is set when the item's profile could not supply its
	// jobs. Its profile jobs are then absent from Rules; say so rather than
	// reading the gap as "nothing to do".
	ScheduleErrors []maintenanceScheduleError `json:"schedule_errors,omitempty"`
	Link           string                     `json:"link"`
}

type assistantEquipmentRule struct {
	ID               string   `json:"id"`
	Description      string   `json:"description"`
	IntervalHours    *float64 `json:"interval_hours,omitempty"`
	IntervalMonths   *int     `json:"interval_months,omitempty"`
	Source           string   `json:"source"`
	OverriddenFields []string `json:"overridden_fields,omitempty"`
	// NotApplicable is a profile job this item has marked as not applying.
	NotApplicable bool `json:"not_applicable,omitempty"`
}

type assistantFindEquipmentResult struct {
	Equipment []assistantEquipmentHit `json:"equipment"`
	Total     int                     `json:"total"`
	Truncated bool                    `json:"truncated,omitempty"`
}

func (d assistantToolDeps) executeFindEquipment(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	store, err := d.maintenanceStore("find_equipment")
	if err != nil {
		return "", err
	}
	var args assistantFindEquipmentArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parse find_equipment arguments: %w", err)
		}
	}
	system := strings.TrimSpace(args.System)
	if system != "" && !validEquipmentSystems[system] {
		return "", fmt.Errorf("find_equipment: unknown system %q", system)
	}
	limit := args.Limit
	if limit <= 0 {
		limit = assistantFindEquipmentDefaultLimit
	}
	if limit > assistantFindEquipmentMaxLimit {
		limit = assistantFindEquipmentMaxLimit
	}

	items, err := store.ListEquipment(equipmentFilter{System: system, Query: strings.TrimSpace(args.Query)})
	if err != nil {
		return "", fmt.Errorf("find_equipment: %w", err)
	}
	total := len(items)
	if len(items) > limit {
		items = items[:limit]
	}

	sched, err := store.MaintenanceSchedule(maintenanceRuleFilter{IncludeStored: true})
	if err != nil {
		return "", fmt.Errorf("find_equipment: %w", err)
	}
	rulesByItem := map[string][]assistantEquipmentRule{}
	for _, r := range sched.Rules {
		if r.EquipmentID == nil {
			continue
		}
		rulesByItem[*r.EquipmentID] = append(rulesByItem[*r.EquipmentID], assistantEquipmentRule{
			ID: r.ID, Description: r.Description,
			IntervalHours: r.IntervalHours, IntervalMonths: r.IntervalMonths,
			Source: r.Source, OverriddenFields: r.OverriddenFields, NotApplicable: r.NotApplicable,
		})
	}
	errorsByItem := map[string][]maintenanceScheduleError{}
	for _, e := range sched.Errors {
		errorsByItem[e.EquipmentID] = append(errorsByItem[e.EquipmentID], e)
	}

	now := time.Now().UTC()
	result := assistantFindEquipmentResult{Equipment: make([]assistantEquipmentHit, 0, len(items)), Total: total}
	for _, it := range items {
		hours, err := equipmentHourReading(it, now)
		if err != nil {
			return "", fmt.Errorf("find_equipment: read hours for %q: %w", it.Name, err)
		}
		hit := assistantEquipmentHit{
			ID: it.ID, Name: it.Name, Manufacturer: it.Manufacturer, Model: it.Model,
			System: it.System, Status: it.Status,
			HourMeterBound: strings.TrimSpace(it.HourMeterPath) != "",
			HoursKnown:     hours.Known,
			Rules:          rulesByItem[it.ID],
			ScheduleErrors: errorsByItem[it.ID],
			ProfileID:      it.ProfileID,
			Link:           assistantEquipmentLink(it.ID),
		}
		if hours.Known {
			g := hours.Gauge
			hit.CurrentMeterReading = &g
			if hours.AsOf != nil {
				hit.HoursAsOf = hours.AsOf.UTC().Format(time.RFC3339)
			}
		}
		hit.RuleCount = len(hit.Rules)
		if hit.Rules == nil {
			hit.Rules = []assistantEquipmentRule{}
		}
		result.Equipment = append(result.Equipment, hit)
	}

	shrink := func() bool {
		if len(result.Equipment) <= 1 {
			return false
		}
		result.Equipment = result.Equipment[:len(result.Equipment)-1]
		result.Truncated = true
		return true
	}
	if len(result.Equipment) < total {
		result.Truncated = true
	}
	return capToolResultJSON(&result, shrink)
}

// ── list_maintenance ────────────────────────────────────────────────────

type assistantListMaintenanceArgs struct {
	EquipmentID   string `json:"equipment_id"`
	Status        string `json:"status"`
	System        string `json:"system"`
	CalendarOnly  bool   `json:"calendar_only"`
	IncludeStored bool   `json:"include_stored"`
}

// assistantMaintenanceStatusOrder lists the statuses most urgent first, the
// order list_maintenance returns rows in so a cap drops the least urgent. It
// only orders rows; every status value itself comes from the rule view.
var assistantMaintenanceStatusOrder = []string{
	string(maintenanceStatusOverdue),
	string(maintenanceStatusDueSoon),
	string(maintenanceStatusHoursUnknown),
	string(maintenanceStatusNeverRecorded),
	string(maintenanceStatusIntervalNotSet),
	string(maintenanceStatusOK),
}

type assistantProcedureNote struct {
	DocumentID string `json:"document_id"`
	Title      string `json:"title"`
}

type assistantMaintenanceRuleRow struct {
	ID            string  `json:"id"`
	EquipmentID   *string `json:"equipment_id"`
	EquipmentName string  `json:"equipment_name,omitempty"`
	System        string  `json:"system,omitempty"`
	Description   string  `json:"description"`

	IntervalHours  *float64 `json:"interval_hours,omitempty"`
	IntervalMonths *int     `json:"interval_months,omitempty"`
	DueSoonHours   *float64 `json:"due_soon_hours,omitempty"`
	DueSoonMonths  *int     `json:"due_soon_months,omitempty"`
	FixedDueDate   string   `json:"fixed_due_date,omitempty"`
	LastDoneAt     string   `json:"last_done_at,omitempty"`
	// LastDoneEngineHours is the baseline in cumulative engine hours, which
	// equals the meter only while no meter replacement is recorded.
	LastDoneEngineHours *float64 `json:"last_done_engine_hours,omitempty"`

	// Status is resolveMaintenanceRuleView's own, computed against today.
	Status string `json:"status"`
	// RemainingHours and RemainingDays are omitted when that axis could not
	// be worked out, never 0 in their place. Negative means overdue by that
	// much. NextDueDate is today plus RemainingDays.
	RemainingHours *float64 `json:"remaining_hours,omitempty"`
	RemainingDays  *int     `json:"remaining_days,omitempty"`
	NextDueDate    string   `json:"next_due_date,omitempty"`
	// NextDueMeterReading is what the operator's meter will read when the
	// job falls due: the current meter reading plus RemainingHours. Only set
	// when both are known. RemainingHours comes from the status engine and is
	// the same on either hours scale.
	NextDueMeterReading *float64 `json:"next_due_meter_reading,omitempty"`
	// HoursUnknown means the rule has an hours interval but its item's meter
	// is not reading. CurrentMeterReading is then omitted, never 0.
	HoursUnknown        bool     `json:"hours_unknown,omitempty"`
	HasHourMeterPath    bool     `json:"has_hour_meter_path"`
	CurrentMeterReading *float64 `json:"current_meter_reading,omitempty"`

	Acknowledged     bool   `json:"acknowledged,omitempty"`
	AckReason        string `json:"ack_reason,omitempty"`
	ProfileServiceID string `json:"profile_service_id,omitempty"`
	// Source is "profile" for a job the item's equipment profile supplies and
	// "item" for a rule made by hand; OverriddenFields names what this item
	// changes from the profile's own values (ADR 0148).
	Source           string                  `json:"source"`
	OverriddenFields []string                `json:"overridden_fields,omitempty"`
	ProcedureNote    *assistantProcedureNote `json:"procedure_note,omitempty"`
	Link             string                  `json:"link"`
}

type assistantListMaintenanceResult struct {
	// Today is the date every status was computed against.
	Today string                        `json:"today"`
	Rules []assistantMaintenanceRuleRow `json:"rules"`
	// ScheduleErrors lists items whose equipment profile could not supply its
	// jobs. Those items' profile jobs are absent from Rules; say so rather
	// than reading the gap as "nothing to do".
	ScheduleErrors []maintenanceScheduleError `json:"schedule_errors,omitempty"`
	Total          int                        `json:"total"`
	Truncated      bool                       `json:"truncated,omitempty"`
}

func (d assistantToolDeps) executeListMaintenance(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	store, err := d.maintenanceStore("list_maintenance")
	if err != nil {
		return "", err
	}
	today, err := d.requireToday("list_maintenance")
	if err != nil {
		return "", err
	}
	var args assistantListMaintenanceArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parse list_maintenance arguments: %w", err)
		}
	}
	statusFilter := strings.TrimSpace(args.Status)
	if statusFilter != "" && !containsString(assistantMaintenanceStatusOrder, statusFilter) {
		return "", fmt.Errorf("list_maintenance: unknown status %q; valid statuses are: %s", statusFilter, strings.Join(assistantMaintenanceStatusOrder, ", "))
	}
	system := strings.TrimSpace(args.System)
	if system != "" && !validEquipmentSystems[system] {
		return "", fmt.Errorf("list_maintenance: unknown system %q", system)
	}

	equipmentID := strings.TrimSpace(args.EquipmentID)
	if equipmentID != "" {
		// An unknown id would otherwise list zero rules, which reads as
		// "nothing due".
		if _, err := store.GetEquipment(equipmentID); err != nil {
			if errors.Is(err, errEquipmentNotFound) {
				return "", fmt.Errorf("list_maintenance: no equipment with id %q (find it with find_equipment)", equipmentID)
			}
			return "", fmt.Errorf("list_maintenance: %w", err)
		}
	}

	sched, err := store.MaintenanceSchedule(maintenanceRuleFilter{
		EquipmentID:   equipmentID,
		IncludeStored: args.IncludeStored,
		System:        system,
	})
	if err != nil {
		return "", fmt.Errorf("list_maintenance: %w", err)
	}

	rows := make([]assistantMaintenanceRuleRow, 0, len(sched.Rules))
	for _, rule := range sched.Rules {
		if args.CalendarOnly && rule.EquipmentID != nil {
			continue
		}
		view, err := viewForEffectiveRule(rule, today)
		if err != nil {
			return "", fmt.Errorf("list_maintenance: resolve %q: %w", rule.Description, err)
		}
		if statusFilter != "" && view.Status != statusFilter {
			continue
		}
		row := assistantMaintenanceRuleRow{
			ID: view.ID, EquipmentID: view.EquipmentID, EquipmentName: view.EquipmentName,
			System: view.System, Description: view.Description,
			IntervalHours: view.IntervalHours, IntervalMonths: view.IntervalMonths,
			DueSoonHours: view.DueSoonHours, DueSoonMonths: view.DueSoonMonths,
			FixedDueDate: view.FixedDueDate, LastDoneAt: view.LastDoneAt, LastDoneEngineHours: view.LastDoneHours,
			Status:         view.Status,
			RemainingHours: view.RemainingHours, RemainingDays: view.RemainingDays,
			HoursUnknown: view.HoursUnknown, HasHourMeterPath: view.HasHourMeterPath,
			CurrentMeterReading: view.CurrentHours,
			Acknowledged:        view.Acknowledged, AckReason: view.AckReason,
			ProfileServiceID: view.ProfileServiceID,
			Source:           view.Source, OverriddenFields: view.OverriddenFields,
			Link: assistantMaintenanceLink,
		}
		if view.RemainingDays != nil {
			row.NextDueDate = today.AddDate(0, 0, *view.RemainingDays).Format("2006-01-02")
		}
		if view.CurrentHours != nil && view.RemainingHours != nil {
			next := *view.CurrentHours + *view.RemainingHours
			row.NextDueMeterReading = &next
		}
		if view.ProcedureNoteID != "" {
			doc, err := store.Get(view.ProcedureNoteID)
			if err != nil {
				return "", fmt.Errorf("list_maintenance: procedure note for %q: %w", rule.Description, err)
			}
			row.ProcedureNote = &assistantProcedureNote{
				DocumentID: doc.ID,
				Title:      assistantCitationTitle(doc.Title, doc.Filename),
			}
		}
		rows = append(rows, row)
	}

	rank := func(status string) int {
		for i, s := range assistantMaintenanceStatusOrder {
			if s == status {
				return i
			}
		}
		return len(assistantMaintenanceStatusOrder)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rank(rows[i].Status) < rank(rows[j].Status) })

	result := assistantListMaintenanceResult{Today: today.Format("2006-01-02"), Rules: rows, ScheduleErrors: sched.Errors, Total: len(rows)}
	shrink := func() bool {
		if len(result.Rules) <= 1 {
			return false
		}
		result.Rules = result.Rules[:len(result.Rules)-1]
		result.Truncated = true
		return true
	}
	return capToolResultJSON(&result, shrink)
}

// ── get_maintenance_log ─────────────────────────────────────────────────

type assistantMaintenanceLogArgs struct {
	EquipmentID string `json:"equipment_id"`
	RuleID      string `json:"rule_id"`
	Since       string `json:"since"`
	Limit       int    `json:"limit"`
}

type assistantMaintenanceLogPart struct {
	Name     string  `json:"name"`
	Quantity float64 `json:"quantity"`
}

type assistantMaintenanceLogRow struct {
	ID            string  `json:"id"`
	EquipmentID   *string `json:"equipment_id"`
	EquipmentName string  `json:"equipment_name,omitempty"`
	RuleID        string  `json:"rule_id,omitempty"`
	PerformedAt   string  `json:"performed_at"`
	// EngineHours is cumulative engine hours at the time, which equals the
	// meter reading only while no meter replacement is recorded.
	EngineHours *float64                      `json:"engine_hours,omitempty"`
	Kind        string                        `json:"kind"`
	Description string                        `json:"description"`
	Who         string                        `json:"who,omitempty"`
	Cost        *float64                      `json:"cost,omitempty"`
	Currency    string                        `json:"currency,omitempty"`
	Parts       []assistantMaintenanceLogPart `json:"parts,omitempty"`
	PhotoCount  int                           `json:"photo_count,omitempty"`
	Link        string                        `json:"link"`
}

type assistantMaintenanceLogResult struct {
	Entries   []assistantMaintenanceLogRow `json:"entries"`
	Total     int                          `json:"total"`
	Truncated bool                         `json:"truncated,omitempty"`
}

func (d assistantToolDeps) executeGetMaintenanceLog(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	store, err := d.maintenanceStore("get_maintenance_log")
	if err != nil {
		return "", err
	}
	var args assistantMaintenanceLogArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parse get_maintenance_log arguments: %w", err)
		}
	}
	equipmentID := strings.TrimSpace(args.EquipmentID)
	ruleID := strings.TrimSpace(args.RuleID)
	since := strings.TrimSpace(args.Since)
	if since != "" {
		if _, err := time.Parse("2006-01-02", since); err != nil {
			return "", fmt.Errorf("get_maintenance_log: since must be YYYY-MM-DD, got %q", since)
		}
	}
	limit := args.Limit
	if limit <= 0 {
		limit = assistantMaintenanceLogDefaultLimit
	}
	if limit > assistantMaintenanceLogMaxLimit {
		limit = assistantMaintenanceLogMaxLimit
	}

	if equipmentID != "" {
		if _, err := store.GetEquipment(equipmentID); err != nil {
			if errors.Is(err, errEquipmentNotFound) {
				return "", fmt.Errorf("get_maintenance_log: no equipment with id %q (find it with find_equipment)", equipmentID)
			}
			return "", fmt.Errorf("get_maintenance_log: %w", err)
		}
	}
	if ruleID != "" {
		if _, err := store.ResolveMaintenanceRule(ruleID); err != nil {
			var unavailable *maintenanceCommandError
			switch {
			case errors.Is(err, errMaintenanceRuleNotFound):
				return "", fmt.Errorf("get_maintenance_log: no maintenance rule with id %q (find it with list_maintenance)", ruleID)
			case errors.As(err, &unavailable):
				// The job exists; its profile is unavailable. History is
				// still readable.
			default:
				return "", fmt.Errorf("get_maintenance_log: %w", err)
			}
		}
	}

	entries, err := store.ListMaintenanceLogEntries(maintenanceLogFilter{EquipmentID: equipmentID})
	if err != nil {
		return "", fmt.Errorf("get_maintenance_log: %w", err)
	}

	names := map[string]string{}
	rows := make([]assistantMaintenanceLogRow, 0, len(entries))
	total := 0
	for _, e := range entries {
		if ruleID != "" && (e.RuleID == nil || *e.RuleID != ruleID) {
			continue
		}
		// performed_at is YYYY-MM-DD, so the string comparison is a date one.
		if since != "" && e.PerformedAt < since {
			continue
		}
		total++
		if len(rows) >= limit {
			continue
		}
		row := assistantMaintenanceLogRow{
			ID: e.ID, EquipmentID: e.EquipmentID, PerformedAt: e.PerformedAt, EngineHours: e.Hours,
			Kind: e.Kind, Description: e.Description, Who: e.Who, Cost: e.Cost, Currency: e.Currency,
			PhotoCount: len(e.PhotoIDs), Link: assistantMaintenanceLink,
		}
		if e.RuleID != nil {
			row.RuleID = *e.RuleID
		}
		if e.EquipmentID != nil {
			name, ok := names[*e.EquipmentID]
			if !ok {
				eq, err := store.GetEquipment(*e.EquipmentID)
				if err != nil {
					return "", fmt.Errorf("get_maintenance_log: %w", err)
				}
				name = eq.Name
				names[*e.EquipmentID] = name
			}
			row.EquipmentName = name
		}
		for _, p := range e.Parts {
			row.Parts = append(row.Parts, assistantMaintenanceLogPart{Name: p.EquipmentName, Quantity: p.Quantity})
		}
		rows = append(rows, row)
	}

	result := assistantMaintenanceLogResult{Entries: rows, Total: total, Truncated: len(rows) < total}
	shrink := func() bool {
		if len(result.Entries) <= 1 {
			return false
		}
		result.Entries = result.Entries[:len(result.Entries)-1]
		result.Truncated = true
		return true
	}
	return capToolResultJSON(&result, shrink)
}
