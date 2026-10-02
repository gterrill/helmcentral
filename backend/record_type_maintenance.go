package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Maintenance rules and log entries as registered record types (ADR 0158,
// converting ADR 0146's operations). A rule is created, or updated through
// the same fields the form edits plus the two baseline fields (what was last
// done, and an acknowledgement); completing a rule is creating a log entry
// against it. Deleting rules or log entries is deliberately not registered:
// ADR 0146 left it with the operator and a changeset does not widen that.
//
// Every hours figure Mate gives is a METER reading, what the operator's gauge
// shows, the same as the Maintenance forms. The commands convert it to true
// engine hours when the changeset is applied.

const (
	recordTypeMaintenanceRule = "maintenance_rule"
	recordTypeMaintenanceLog  = "maintenance_log"
)

func init() {
	defaultRecordRegistry.register(maintenanceRuleRecordType())
	defaultRecordRegistry.register(maintenanceLogRecordType())
}

// ── field helpers ───────────────────────────────────────────────────────

// fieldErr is a problem with one field of one operation, reported as
// "field: message" so Mate can correct that field. It counts as an expected
// refusal when Apply meets it again.
func fieldErr(field, format string, a ...any) error {
	return &fieldedError{Field: field, Message: fmt.Sprintf(format, a...)}
}

func floatPtrOf(v any) *float64 {
	switch n := v.(type) {
	case float64:
		return &n
	case int:
		f := float64(n)
		return &f
	}
	return nil
}

func intPtrOf(v any) *int {
	switch n := v.(type) {
	case float64:
		i := int(n)
		return &i
	case int:
		return &n
	}
	return nil
}

func stringOf(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func nilIfBlank(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nilFloat(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func nilInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func equipmentNameOf(q sqlQueryer, id string) (string, error) {
	var name string
	err := q.QueryRow(`SELECT name FROM equipment WHERE id = ?`, id).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errEquipmentNotFound
	}
	if err != nil {
		return "", fmt.Errorf("equipment name: %w", err)
	}
	return name, nil
}

// proposalRuleLabel is "Generator · Oil and filter", or just the description
// for a calendar-only rule.
func proposalRuleLabel(itemName, description string) string {
	if itemName == "" {
		return description
	}
	return itemName + " · " + description
}

// ── maintenance_rule ────────────────────────────────────────────────────

// maintenanceRuleGroup is the fields a rule's own form edits, as against the
// baseline and acknowledgement, which have their own commands.
var maintenanceRuleGroup = []string{"description", "interval_hours", "interval_months", "due_soon_hours", "due_soon_months", "fixed_due_date", "not_applicable"}

func maintenanceRuleRecordType() *recordType {
	return &recordType{
		Name:  recordTypeMaintenanceRule,
		Label: "maintenance rule",
		Summary: "A maintenance rule: one job on the schedule, either an item's own rule or a calendar-only one with no item. " +
			"A rule whose id starts with job: comes from the item's equipment profile; changing it is a per-item override of the profile's live value. " +
			"Hours in a rule are meter readings, what the gauge shows.",
		Actions: []string{changeCreate, changeUpdate},
		Filters: map[string]string{"equipment_id": "only this item's rules"},
		Fields: []recordField{
			{Name: "equipment_id", Kind: kindID, Writable: true, Nullable: true, Ref: "equipment", Description: "the item the rule is for; leave out for a calendar-only rule (certificates, renewals). Cannot be changed once the rule exists."},
			{Name: "description", Kind: kindString, Writable: true, Description: "what the job is, e.g. \"Oil and filter\""},
			{Name: "interval_hours", Kind: kindNumber, Writable: true, Nullable: true, Description: "repeat every this many meter hours (needs an item); null removes it"},
			{Name: "interval_months", Kind: kindInteger, Writable: true, Nullable: true, Description: "repeat every this many months; null removes it"},
			{Name: "due_soon_hours", Kind: kindNumber, Writable: true, Nullable: true, Description: "warn this many meter hours before due; null removes it"},
			{Name: "due_soon_months", Kind: kindInteger, Writable: true, Nullable: true, Description: "warn this many months before due; null removes it"},
			{Name: "fixed_due_date", Kind: kindDate, Writable: true, Nullable: true, Description: "a fixed due date, YYYY-MM-DD; null removes it"},
			{Name: "not_applicable", Kind: kindBoolean, Writable: true, Description: "profile jobs only (an id starting job:): true marks the job as not applying to this item, false makes it apply again"},
			{Name: "last_done_at", Kind: kindDate, Writable: true, Nullable: true, Description: "the date it was last done, YYYY-MM-DD: the baseline"},
			{Name: "last_done_meter_reading", Kind: kindNumber, Writable: true, Description: "write-only: the meter reading when it was last done (needs an item); a read does not return it"},
			{Name: "acknowledged_reason", Kind: kindString, Writable: true, Nullable: true, Description: "acknowledges the rule with this reason; null or blank clears the acknowledgement"},
		},
		Renames:  map[string]string{"last_done_hours": "last_done_meter_reading"},
		Get:      getMaintenanceRule,
		List:     listMaintenanceRules,
		Create:   createMaintenanceRuleRecord,
		Update:   updateMaintenanceRuleRecord,
		Describe: describeMaintenanceRule,
		Href:     func(recordSnapshot) string { return "/inventory/maintenance" },
	}
}

func ruleSnapshot(q sqlQueryer, eff effectiveMaintenanceRule) (recordSnapshot, error) {
	r := eff.maintenanceRule
	itemName := ""
	var equipmentID any
	if r.EquipmentID != nil {
		name, err := equipmentNameOf(q, *r.EquipmentID)
		if err != nil {
			return recordSnapshot{}, err
		}
		itemName, equipmentID = name, *r.EquipmentID
	}
	var ack any
	if r.Acknowledged {
		ack = r.AckReason
	}
	return recordSnapshot{
		ID:      r.ID,
		Label:   proposalRuleLabel(itemName, r.Description),
		Version: r.UpdatedAt.UTC().Format(time.RFC3339),
		Fields: map[string]any{
			"equipment_id":        equipmentID,
			"description":         r.Description,
			"interval_hours":      nilFloat(r.IntervalHours),
			"interval_months":     nilInt(r.IntervalMonths),
			"due_soon_hours":      nilFloat(r.DueSoonHours),
			"due_soon_months":     nilInt(r.DueSoonMonths),
			"fixed_due_date":      nilIfBlank(r.FixedDueDate),
			"not_applicable":      r.NotApplicable,
			"last_done_at":        nilIfBlank(r.LastDoneAt),
			"acknowledged_reason": ack,
		},
	}, nil
}

func getMaintenanceRule(q sqlQueryer, id string) (recordSnapshot, error) {
	// The EFFECTIVE rule: a profile job resolves by its job: id whether or not
	// it has a row yet.
	eff, err := effectiveRuleByID(q, currentMaintenanceProfiles(), id)
	if errors.Is(err, errMaintenanceRuleNotFound) {
		return recordSnapshot{}, errRecordNotFound
	}
	if err != nil {
		return recordSnapshot{}, err
	}
	return ruleSnapshot(q, eff)
}

func listMaintenanceRules(s *documentStore, filter map[string]string) ([]recordSnapshot, error) {
	sched, err := s.MaintenanceSchedule(maintenanceRuleFilter{EquipmentID: filter["equipment_id"], IncludeStored: true})
	if err != nil {
		return nil, err
	}
	var out []recordSnapshot
	err = s.Read(func(q sqlQueryer) error {
		for _, eff := range sched.Rules {
			snap, err := ruleSnapshot(q, eff)
			if err != nil {
				return err
			}
			out = append(out, snap)
		}
		return nil
	})
	return out, err
}

// ruleRequest is the form-shaped request the fields give. A null is absent.
func ruleRequest(fields fieldSet) maintenanceRuleRequest {
	req := maintenanceRuleRequest{
		Description:    stringOf(fields["description"]),
		IntervalHours:  floatPtrOf(fields["interval_hours"]),
		IntervalMonths: intPtrOf(fields["interval_months"]),
		DueSoonHours:   floatPtrOf(fields["due_soon_hours"]),
		DueSoonMonths:  intPtrOf(fields["due_soon_months"]),
		FixedDueDate:   stringOf(fields["fixed_due_date"]),
	}
	if id := stringOf(fields["equipment_id"]); id != "" {
		req.EquipmentID = &id
	}
	return req
}

func lastDoneRequest(fields fieldSet) maintenanceLastDoneRequest {
	req := maintenanceLastDoneRequest{LastDoneHours: floatPtrOf(fields["last_done_meter_reading"])}
	if v, ok := fields["last_done_at"]; ok {
		at := stringOf(v)
		req.LastDoneAt = &at
	}
	return req
}

func createMaintenanceRuleRecord(env changeEnv, fields fieldSet) (createdRecord, error) {
	req := ruleRequest(fields)
	if req.EquipmentID != nil {
		if _, err := equipmentNameOf(env.tx, *req.EquipmentID); err != nil {
			if errors.Is(err, errEquipmentNotFound) {
				return createdRecord{}, fieldErr("equipment_id", "no equipment with id %q (find it with find_equipment)", *req.EquipmentID)
			}
			return createdRecord{}, err
		}
	}
	rule, err := cmdCreateMaintenanceRule(env.tx, env.now, req)
	if err != nil {
		return createdRecord{}, err
	}
	_, hasAt := fields["last_done_at"]
	if hasAt || fields["last_done_meter_reading"] != nil {
		if fields["last_done_meter_reading"] != nil && req.EquipmentID == nil {
			return createdRecord{}, fieldErr("last_done_meter_reading", "a rule with no equipment has no hour meter to take a reading from")
		}
		if _, err := cmdSetMaintenanceRuleLastDone(env.tx, env.now, env.today, rule.ID, lastDoneRequest(fields)); err != nil {
			return createdRecord{}, err
		}
	}
	if reason := stringOf(fields["acknowledged_reason"]); reason != "" {
		if _, err := cmdAcknowledgeMaintenanceRule(env.tx, env.now, rule.ID, reason); err != nil {
			return createdRecord{}, err
		}
	}
	return createdRecord{ID: rule.ID}, nil
}

func updateMaintenanceRuleRecord(env changeEnv, before recordSnapshot, given fieldSet) error {
	if v, ok := given["equipment_id"]; ok && !sameFieldValue(before.Fields["equipment_id"], v) {
		return fieldErr("equipment_id", "a rule cannot move to another item; add a new rule for that item instead")
	}
	eff, err := effectiveRuleByID(env.tx, currentMaintenanceProfiles(), before.ID)
	if err != nil {
		return err
	}
	rule := eff.maintenanceRule

	groupChanged := false
	for _, name := range maintenanceRuleGroup {
		if v, ok := given[name]; ok {
			cur, in := before.Fields[name]
			if !in || !sameFieldValue(cur, v) || name == "not_applicable" && !isMaintenanceJobID(rule.ID) {
				groupChanged = true
			}
		}
	}
	if groupChanged {
		if isMaintenanceJobID(rule.ID) {
			in, err := jobOverridesFromFields(given)
			if err != nil {
				return err
			}
			if _, err := cmdSetMaintenanceRuleOverrides(env.tx, env.now, before.ID, in); err != nil {
				return err
			}
		} else {
			req, err := mergeRuleUpdate(rule, given)
			if err != nil {
				return err
			}
			if _, err := cmdUpdateMaintenanceRule(env.tx, env.now, before.ID, req); err != nil {
				return err
			}
		}
	}

	_, hasAt := given["last_done_at"]
	if hasAt || given["last_done_meter_reading"] != nil {
		if given["last_done_meter_reading"] != nil && rule.EquipmentID == nil {
			return fieldErr("last_done_meter_reading", "a rule with no equipment has no hour meter to take a reading from")
		}
		if _, err := cmdSetMaintenanceRuleLastDone(env.tx, env.now, env.today, before.ID, lastDoneRequest(given)); err != nil {
			return err
		}
	}
	if v, ok := given["acknowledged_reason"]; ok {
		if _, err := cmdAcknowledgeMaintenanceRule(env.tx, env.now, before.ID, stringOf(v)); err != nil {
			return err
		}
	}
	return nil
}

// mergeRuleUpdate builds the whole-rule request an update means: the rule as
// it is now, with the fields given replaced and the fields given as null
// removed. The store's update is a full replace, so a change naming only the
// interval must not blank the rest.
func mergeRuleUpdate(rule maintenanceRule, given fieldSet) (maintenanceRuleRequest, error) {
	if _, ok := given["not_applicable"]; ok {
		return maintenanceRuleRequest{}, fieldErr("not_applicable", "only a profile job (an id starting with job:) can be marked not applicable; %q is an item's own rule", rule.Description)
	}
	req := maintenanceRuleRequest{
		EquipmentID:    rule.EquipmentID,
		Description:    rule.Description,
		IntervalHours:  rule.IntervalHours,
		IntervalMonths: rule.IntervalMonths,
		DueSoonHours:   rule.DueSoonHours,
		DueSoonMonths:  rule.DueSoonMonths,
		FixedDueDate:   rule.FixedDueDate,
	}
	if v, ok := given["description"]; ok {
		req.Description = stringOf(v)
	}
	if v, ok := given["interval_hours"]; ok {
		req.IntervalHours = floatPtrOf(v)
	}
	if v, ok := given["interval_months"]; ok {
		req.IntervalMonths = intPtrOf(v)
	}
	if v, ok := given["due_soon_hours"]; ok {
		req.DueSoonHours = floatPtrOf(v)
	}
	if v, ok := given["due_soon_months"]; ok {
		req.DueSoonMonths = intPtrOf(v)
	}
	if v, ok := given["fixed_due_date"]; ok {
		req.FixedDueDate = stringOf(v)
	}
	return req, nil
}

// jobOverridesFromFields turns an update on a profile job into the command
// input it means: description and the two intervals become overrides of the
// profile; due_soon_hours, due_soon_months and fixed_due_date are plain
// per-item state. All go through cmdSetMaintenanceRuleOverrides, which checks
// them the way the overrides endpoint does. A null clears.
func jobOverridesFromFields(given fieldSet) (maintenanceOverridesInput, error) {
	var in maintenanceOverridesInput
	if v, ok := given["description"]; ok {
		d := stringOf(v)
		in.Description = &d
	}
	if v, ok := given["interval_hours"]; ok {
		in.IntervalHours = &maintenanceIntervalHoursOverride{Value: floatPtrOf(v)}
	}
	if v, ok := given["interval_months"]; ok {
		in.IntervalMonths = &maintenanceIntervalMonthsOverride{Value: intPtrOf(v)}
	}
	if v, ok := given["due_soon_hours"]; ok {
		in.DueSoonHours = &maintenanceNullableFloat{Value: floatPtrOf(v)}
	}
	if v, ok := given["due_soon_months"]; ok {
		in.DueSoonMonths = &maintenanceNullableInt{Value: intPtrOf(v)}
	}
	if v, ok := given["fixed_due_date"]; ok {
		d := stringOf(v)
		in.FixedDueDate = &d
	}
	if v, ok := given["not_applicable"]; ok {
		b, _ := v.(bool)
		in.NotApplicable = &b
	}
	return in, nil
}

// ── summaries ───────────────────────────────────────────────────────────

func sameFloat(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func sameInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func formatProposalNumber(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// formatProposalDate renders YYYY-MM-DD as "9 Jan 2025", the way the card
// speaks; anything that does not parse is shown as given.
func formatProposalDate(s string) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return t.Format("2 Jan 2006")
}

func proposalIntervalPhrase(hours *float64, months *int, fixed string, dueSoonHours *float64, dueSoonMonths *int) string {
	var parts []string
	switch {
	case hours != nil && months != nil:
		parts = append(parts, fmt.Sprintf("every %s h or %d mo", formatProposalNumber(*hours), *months))
	case hours != nil:
		parts = append(parts, fmt.Sprintf("every %s h", formatProposalNumber(*hours)))
	case months != nil:
		parts = append(parts, fmt.Sprintf("every %d mo", *months))
	}
	if fixed != "" {
		parts = append(parts, "due "+formatProposalDate(fixed))
	}
	if len(parts) == 0 {
		parts = append(parts, "no interval set")
	}
	var soon []string
	if dueSoonHours != nil {
		soon = append(soon, formatProposalNumber(*dueSoonHours)+" h")
	}
	if dueSoonMonths != nil {
		soon = append(soon, fmt.Sprintf("%d mo", *dueSoonMonths))
	}
	if len(soon) > 0 {
		parts = append(parts, "due soon at "+strings.Join(soon, " / "))
	}
	return strings.Join(parts, ", ")
}

// proposalDonePhrase is "9 Jan 2025 at 239 h (meter)": the date and/or the
// meter reading, whichever were given.
func proposalDonePhrase(at string, meter *float64) string {
	var b strings.Builder
	if at != "" {
		b.WriteString(formatProposalDate(at))
	}
	if meter != nil {
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		b.WriteString("at " + formatProposalNumber(*meter) + " h (meter)")
	}
	return b.String()
}

// snapshotInterval is a rule snapshot's intervals as the card phrases them.
func snapshotInterval(f map[string]any) string {
	return proposalIntervalPhrase(floatPtrOf(f["interval_hours"]), intPtrOf(f["interval_months"]), stringOf(f["fixed_due_date"]),
		floatPtrOf(f["due_soon_hours"]), intPtrOf(f["due_soon_months"]))
}

func describeMaintenanceRule(d describeInput) string {
	fields := d.Op.Fields
	_, hasAt := fields["last_done_at"]
	meter := floatPtrOf(fields["last_done_meter_reading"])
	hasLast := hasAt || meter != nil
	lastDone := proposalDonePhrase(stringOf(fields["last_done_at"]), meter)
	reason, hasAck := fields["acknowledged_reason"]

	if d.Op.Action == changeCreate {
		summary := "Add " + d.After.Label + ": " + snapshotInterval(d.After.Fields)
		if hasLast {
			summary += ", last done " + lastDone
		}
		if r := stringOf(reason); r != "" {
			summary += ", acknowledged: " + r
		}
		return summary
	}

	label := d.Before.Label
	groupGiven := false
	for _, name := range maintenanceRuleGroup {
		if _, ok := fields[name]; ok {
			groupGiven = true
		}
	}
	var out string
	switch {
	case groupGiven && isMaintenanceJobID(d.Before.ID):
		out = describeJobChange(label, d)
	case groupGiven:
		out = "Change " + label + ": "
		if after := stringOf(d.After.Fields["description"]); after != stringOf(d.Before.Fields["description"]) {
			out += fmt.Sprintf("rename to %q, ", after)
		}
		out += "now " + snapshotInterval(d.After.Fields)
	case hasLast:
		out = label + ": last done " + lastDone
	case hasAck:
		if stringOf(reason) == "" {
			return "Clear the acknowledgement on " + label
		}
		return "Acknowledge " + label + ": " + stringOf(reason)
	}
	if groupGiven && hasLast {
		out += ", last done " + lastDone
	}
	if hasAck && (groupGiven || hasLast) {
		if r := stringOf(reason); r == "" {
			out += "; acknowledgement cleared"
		} else {
			out += "; acknowledged: " + r
		}
	}
	return out
}

func describeJobChange(label string, d describeInput) string {
	b, a := d.Before.Fields, d.After.Fields
	onlyNA := true
	for _, name := range maintenanceRuleGroup {
		if _, ok := d.Op.Fields[name]; ok && name != "not_applicable" {
			onlyNA = false
		}
	}
	naBefore, naAfter := b["not_applicable"] == true, a["not_applicable"] == true
	if onlyNA {
		if naAfter {
			return label + ": mark not applicable to this item"
		}
		return label + ": applies to this item again"
	}
	summary := "Change " + label + " for this item: "
	if naAfter != naBefore {
		if naAfter {
			summary += "mark not applicable, "
		} else {
			summary += "applies again, "
		}
	}
	if after := stringOf(a["description"]); after != stringOf(b["description"]) {
		summary += fmt.Sprintf("rename to %q, ", after)
	}
	return summary + "now " + snapshotInterval(a)
}

// ── maintenance_log ─────────────────────────────────────────────────────

func maintenanceLogRecordType() *recordType {
	return &recordType{
		Name:  recordTypeMaintenanceLog,
		Label: "maintenance log entry",
		Summary: "An entry in the maintenance log. Creating one completes a rule: it records the job as done and resets the rule's baseline. " +
			"Existing entries cannot be changed or deleted from here.",
		Actions: []string{changeCreate},
		Filters: map[string]string{"equipment_id": "only this item's entries"},
		Fields: []recordField{
			{Name: "rule_id", Kind: kindID, Writable: true, Ref: recordTypeMaintenanceRule, Description: "the rule this completes: an id from list_maintenance"},
			{Name: "performed_at", Kind: kindDate, Writable: true, Description: "the date it was done, YYYY-MM-DD"},
			{Name: "meter_reading", Kind: kindNumber, Writable: true, Description: "write-only: the meter reading when it was done (required for an hours-based rule); a read returns engine_hours instead"},
			{Name: "description", Kind: kindString, Writable: true, Description: "what was done"},
			{Name: "who", Kind: kindString, Writable: true, Description: "who did it"},
			{Name: "cost", Kind: kindNumber, Writable: true, Description: "what it cost"},
			{Name: "new_due_date", Kind: kindDate, Writable: true, Description: "the next fixed due date, only for a fixed-date rule with no monthly interval"},
			{Name: "equipment_id", Kind: kindID, Ref: "equipment", Description: "the item the entry belongs to"},
			{Name: "engine_hours", Kind: kindNumber, Description: "cumulative engine hours when it was done, which equals the meter only if no meter replacement is recorded"},
		},
		Renames:  map[string]string{"hours": "meter_reading"},
		Get:      getMaintenanceLogEntry,
		List:     listMaintenanceLogEntries,
		Create:   createMaintenanceLogRecord,
		Watch:    watchMaintenanceLogRule,
		Describe: describeMaintenanceLog,
		Href:     func(recordSnapshot) string { return "/inventory/maintenance" },
	}
}

func logSnapshot(q sqlQueryer, e maintenanceLogEntry) (recordSnapshot, error) {
	label := e.Description
	if e.RuleID != nil {
		if eff, err := effectiveRuleByID(q, currentMaintenanceProfiles(), *e.RuleID); err == nil {
			rs, err := ruleSnapshot(q, eff)
			if err != nil {
				return recordSnapshot{}, err
			}
			label = rs.Label
		}
	}
	if label == "" {
		label = "Log entry"
	}
	var ruleID, equipmentID any
	if e.RuleID != nil {
		ruleID = *e.RuleID
	}
	if e.EquipmentID != nil {
		equipmentID = *e.EquipmentID
	}
	return recordSnapshot{
		ID:      e.ID,
		Label:   label + " (" + formatProposalDate(e.PerformedAt) + ")",
		Version: e.UpdatedAt.UTC().Format(time.RFC3339),
		Fields: map[string]any{
			"rule_id": ruleID, "equipment_id": equipmentID, "performed_at": e.PerformedAt, "engine_hours": nilFloat(e.Hours),
			"description": e.Description, "who": e.Who, "cost": nilFloat(e.Cost),
		},
	}, nil
}

func getMaintenanceLogEntry(q sqlQueryer, id string) (recordSnapshot, error) {
	e, err := maintenanceLogEntryByID(q, id)
	if errors.Is(err, errMaintenanceLogEntryNotFound) {
		return recordSnapshot{}, errRecordNotFound
	}
	if err != nil {
		return recordSnapshot{}, err
	}
	return logSnapshot(q, e)
}

func listMaintenanceLogEntries(s *documentStore, filter map[string]string) ([]recordSnapshot, error) {
	entries, err := s.ListMaintenanceLogEntries(maintenanceLogFilter{EquipmentID: filter["equipment_id"]})
	if err != nil {
		return nil, err
	}
	var out []recordSnapshot
	err = s.Read(func(q sqlQueryer) error {
		for _, e := range entries {
			snap, err := logSnapshot(q, e)
			if err != nil {
				return err
			}
			out = append(out, snap)
		}
		return nil
	})
	return out, err
}

// watchMaintenanceLogRule makes completing a rule stale if the rule changes
// before Apply: the entry's meaning (and the due date it moves) depends on
// the rule as Mate saw it.
func watchMaintenanceLogRule(q sqlQueryer, fields fieldSet) ([]recordWatch, error) {
	id := stringOf(fields["rule_id"])
	if id == "" {
		return nil, nil
	}
	snap, err := getMaintenanceRule(q, id)
	if errors.Is(err, errRecordNotFound) {
		return nil, fieldErr("rule_id", "no maintenance rule with id %q (find it with list_maintenance)", id)
	}
	if err != nil {
		return nil, err
	}
	return []recordWatch{{Type: recordTypeMaintenanceRule, ID: id, Version: snap.Version, Label: snap.Label}}, nil
}

func createMaintenanceLogRecord(env changeEnv, fields fieldSet) (createdRecord, error) {
	ruleID := stringOf(fields["rule_id"])
	if ruleID == "" {
		return createdRecord{}, fieldErr("rule_id", "is required (an id from list_maintenance)")
	}
	beforeEff, err := effectiveRuleByID(env.tx, currentMaintenanceProfiles(), ruleID)
	if errors.Is(err, errMaintenanceRuleNotFound) {
		return createdRecord{}, fieldErr("rule_id", "no maintenance rule with id %q (find it with list_maintenance)", ruleID)
	}
	if err != nil {
		return createdRecord{}, err
	}
	before := beforeEff.maintenanceRule
	if fields["meter_reading"] != nil && before.EquipmentID == nil {
		return createdRecord{}, fieldErr("meter_reading", "a rule with no equipment has no hour meter to take a reading from")
	}
	req := maintenanceLogEntryRequest{
		PerformedAt: stringOf(fields["performed_at"]),
		Hours:       floatPtrOf(fields["meter_reading"]),
		Description: stringOf(fields["description"]),
		Who:         stringOf(fields["who"]),
		Cost:        floatPtrOf(fields["cost"]),
		NewDueDate:  stringOf(fields["new_due_date"]),
	}
	after, entry, err := cmdCompleteMaintenanceRule(env.tx, env.now, ruleID, req)
	if err != nil {
		return createdRecord{}, err
	}
	itemName := ""
	if before.EquipmentID != nil {
		if itemName, err = equipmentNameOf(env.tx, *before.EquipmentID); err != nil {
			return createdRecord{}, err
		}
	}
	extra := map[string]any{"rule_label": proposalRuleLabel(itemName, before.Description)}
	if after.FixedDueDate != "" && after.FixedDueDate != before.FixedDueDate {
		extra["new_fixed_due_date"] = after.FixedDueDate
	}
	return createdRecord{ID: entry.ID, Extra: extra}, nil
}

func describeMaintenanceLog(d describeInput) string {
	f := d.Op.Fields
	label, _ := d.Extra["rule_label"].(string)
	summary := "Log " + label + " as done " + proposalDonePhrase(stringOf(f["performed_at"]), floatPtrOf(f["meter_reading"]))
	if who := stringOf(f["who"]); who != "" {
		summary += ", by " + who
	}
	if cost := floatPtrOf(f["cost"]); cost != nil {
		summary += ", cost " + formatProposalNumber(*cost)
	}
	if desc := stringOf(f["description"]); desc != "" {
		summary += fmt.Sprintf(" (%s)", desc)
	}
	// What only the dry run knows: the fixed due date a completion moves the
	// rule to.
	if next, _ := d.Extra["new_fixed_due_date"].(string); next != "" {
		summary += ", next due " + formatProposalDate(next)
	}
	return summary
}
