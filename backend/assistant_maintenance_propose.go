package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// propose_maintenance_changes (ADR 0146). Mate never writes the maintenance
// schedule. This tool validates a list of operations against the same rules
// the Maintenance handlers enforce, records the state of every rule it will
// touch, renders one operator-words line per operation, and returns all of it
// as a proposal. It writes nothing: the runner saves the proposal with the
// assistant message that carries it, and the operator's Apply tap runs it
// (assistantStore.ApplyProposal, assistant_proposals.go).
//
// Every hours figure in a proposal is a METER reading, what the operator's
// gauge shows, the same as the Maintenance forms. The commands convert it to
// true engine hours when the proposal is applied.

const (
	proposalOpCreateRule     = "create_rule"
	proposalOpUpdateRule     = "update_rule"
	proposalOpSetLastDone    = "set_last_done"
	proposalOpCopyProfile    = "copy_profile_schedule"
	proposalOpCompleteRule   = "complete_rule"
	proposalOpAcknowledge    = "acknowledge"
	assistantProposalMaxOps  = 20
	assistantProposalToolTag = "propose_maintenance_changes"
)

// assistantProposalOp is one operation in a proposal: what Mate asked for
// (the fields that apply to Op), plus what propose filled in, Summary and the
// RuleUpdatedAt snapshot. Hours fields carry meter readings.
type assistantProposalOp struct {
	Op string `json:"op"`

	EquipmentID string `json:"equipment_id,omitempty"`
	RuleID      string `json:"rule_id,omitempty"`

	// Description is the rule's description on create_rule and update_rule,
	// and what was done on complete_rule.
	Description    string   `json:"description,omitempty"`
	IntervalHours  *float64 `json:"interval_hours,omitempty"`
	IntervalMonths *int     `json:"interval_months,omitempty"`
	DueSoonHours   *float64 `json:"due_soon_hours,omitempty"`
	DueSoonMonths  *int     `json:"due_soon_months,omitempty"`
	FixedDueDate   string   `json:"fixed_due_date,omitempty"`
	// Clear names the interval fields update_rule removes.
	Clear []string `json:"clear,omitempty"`

	LastDoneAt           string   `json:"last_done_at,omitempty"`
	LastDoneMeterReading *float64 `json:"last_done_meter_reading,omitempty"`

	PerformedAt  string   `json:"performed_at,omitempty"`
	MeterReading *float64 `json:"meter_reading,omitempty"`
	Who          string   `json:"who,omitempty"`
	Cost         *float64 `json:"cost,omitempty"`
	NewDueDate   string   `json:"new_due_date,omitempty"`

	Reason string `json:"reason,omitempty"`

	// Summary is the operator-words line the card shows and the history
	// replays. RuleUpdatedAt is the target rule's updated_at when Mate
	// proposed, the stale check's snapshot; empty for an operation with no
	// existing rule to protect.
	Summary       string `json:"summary"`
	RuleUpdatedAt string `json:"rule_updated_at,omitempty"`
	// CopyServiceIDs is, for copy_profile_schedule, the profile services the
	// dry run found would get a rule. Apply refuses (stale) if the copy would
	// now create a different set, so a copy the operator already did by hand
	// cannot be "applied" as a no-op that the card claims created rules.
	CopyServiceIDs []string `json:"copy_service_ids,omitempty"`
}

type assistantProposeArgs struct {
	Ops []assistantProposalOp `json:"ops"`
}

// assistantProposeResult is the tool's answer: the proposal (which the runner
// also saves) and what to tell the operator.
type assistantProposeResult struct {
	Proposal assistantProposal `json:"proposal"`
	NextStep string            `json:"next_step"`
}

// allowedFields lists, per operation, the JSON fields it accepts. A field
// outside the list is refused rather than ignored: a change Mate believes it
// proposed but that the card does not show is worse than a failed call.
var proposalOpAllowedFields = map[string][]string{
	proposalOpCreateRule:   {"equipment_id", "description", "interval_hours", "interval_months", "due_soon_hours", "due_soon_months", "fixed_due_date", "last_done_at", "last_done_meter_reading"},
	proposalOpUpdateRule:   {"rule_id", "description", "interval_hours", "interval_months", "due_soon_hours", "due_soon_months", "fixed_due_date", "clear"},
	proposalOpSetLastDone:  {"rule_id", "last_done_at", "last_done_meter_reading"},
	proposalOpCopyProfile:  {"equipment_id"},
	proposalOpCompleteRule: {"rule_id", "performed_at", "meter_reading", "description", "who", "cost", "new_due_date"},
	proposalOpAcknowledge:  {"rule_id", "reason"},
}

// setFields lists the JSON names of the input fields op actually carries.
func (op assistantProposalOp) setFields() []string {
	var out []string
	add := func(set bool, name string) {
		if set {
			out = append(out, name)
		}
	}
	add(op.EquipmentID != "", "equipment_id")
	add(op.RuleID != "", "rule_id")
	add(op.Description != "", "description")
	add(op.IntervalHours != nil, "interval_hours")
	add(op.IntervalMonths != nil, "interval_months")
	add(op.DueSoonHours != nil, "due_soon_hours")
	add(op.DueSoonMonths != nil, "due_soon_months")
	add(op.FixedDueDate != "", "fixed_due_date")
	add(len(op.Clear) > 0, "clear")
	add(op.LastDoneAt != "", "last_done_at")
	add(op.LastDoneMeterReading != nil, "last_done_meter_reading")
	add(op.PerformedAt != "", "performed_at")
	add(op.MeterReading != nil, "meter_reading")
	add(op.Who != "", "who")
	add(op.Cost != nil, "cost")
	add(op.NewDueDate != "", "new_due_date")
	add(op.Reason != "", "reason")
	return out
}

// proposalFieldError is a problem with one field of one operation, reported
// as "field: message" so Mate can correct that field.
type proposalFieldError struct {
	Field   string
	Message string
}

func (e *proposalFieldError) Error() string { return e.Field + ": " + e.Message }

func fieldErr(field, format string, a ...any) error {
	return &proposalFieldError{Field: field, Message: fmt.Sprintf(format, a...)}
}

// asProposalError turns whatever a validator returned into a field error.
//
// The handlers' validators name the hours fields hours and last_done_hours;
// in a proposal those carry meter readings, so they are named
// meter_reading and last_done_meter_reading, the words Mate wrote.
func asProposalError(err error) error {
	var verr *inventoryValidationError
	if errors.As(err, &verr) {
		field, msg := verr.Field, verr.Message
		for old, renamed := range map[string]string{"hours": "meter_reading", "last_done_hours": "last_done_meter_reading"} {
			if field == old {
				field = renamed
			}
			if strings.HasPrefix(msg, old+" ") {
				msg = renamed + msg[len(old):]
			}
		}
		return &proposalFieldError{Field: field, Message: msg}
	}
	return err
}

// ── rule request builders shared by propose and apply ───────────────────

func (op assistantProposalOp) createRequest() maintenanceRuleRequest {
	req := maintenanceRuleRequest{
		Description:    op.Description,
		IntervalHours:  op.IntervalHours,
		IntervalMonths: op.IntervalMonths,
		DueSoonHours:   op.DueSoonHours,
		DueSoonMonths:  op.DueSoonMonths,
		FixedDueDate:   op.FixedDueDate,
	}
	if id := strings.TrimSpace(op.EquipmentID); id != "" {
		req.EquipmentID = &id
	}
	return req
}

func (op assistantProposalOp) lastDoneRequest() maintenanceLastDoneRequest {
	req := maintenanceLastDoneRequest{LastDoneHours: op.LastDoneMeterReading}
	if op.LastDoneAt != "" {
		at := op.LastDoneAt
		req.LastDoneAt = &at
	}
	return req
}

func (op assistantProposalOp) completeRequest() maintenanceLogEntryRequest {
	return maintenanceLogEntryRequest{
		PerformedAt: op.PerformedAt,
		Hours:       op.MeterReading,
		Description: op.Description,
		Who:         op.Who,
		Cost:        op.Cost,
		NewDueDate:  op.NewDueDate,
	}
}

var proposalClearable = map[string]bool{
	"interval_hours": true, "interval_months": true, "due_soon_hours": true, "due_soon_months": true, "fixed_due_date": true,
}

// mergeRuleUpdate builds the whole-rule request an update_rule operation
// means: the rule as it is now, with the fields the operation gives replaced
// and the fields it names in clear removed. The store's update is a full
// replace, so an operation that named only the interval must not blank the
// rest.
func mergeRuleUpdate(rule maintenanceRule, op assistantProposalOp) (maintenanceRuleRequest, error) {
	req := maintenanceRuleRequest{
		EquipmentID:      rule.EquipmentID,
		Description:      rule.Description,
		IntervalHours:    rule.IntervalHours,
		IntervalMonths:   rule.IntervalMonths,
		DueSoonHours:     rule.DueSoonHours,
		DueSoonMonths:    rule.DueSoonMonths,
		FixedDueDate:     rule.FixedDueDate,
		ProfileServiceID: rule.ProfileServiceID,
	}
	given := map[string]bool{
		"interval_hours": op.IntervalHours != nil, "interval_months": op.IntervalMonths != nil,
		"due_soon_hours": op.DueSoonHours != nil, "due_soon_months": op.DueSoonMonths != nil,
		"fixed_due_date": op.FixedDueDate != "",
	}
	for _, name := range op.Clear {
		if !proposalClearable[name] {
			return maintenanceRuleRequest{}, fieldErr("clear", "cannot clear %q; it can only name interval_hours, interval_months, due_soon_hours, due_soon_months or fixed_due_date", name)
		}
		if given[name] {
			return maintenanceRuleRequest{}, fieldErr("clear", "%s is both set and cleared", name)
		}
		switch name {
		case "interval_hours":
			req.IntervalHours = nil
		case "interval_months":
			req.IntervalMonths = nil
		case "due_soon_hours":
			req.DueSoonHours = nil
		case "due_soon_months":
			req.DueSoonMonths = nil
		case "fixed_due_date":
			req.FixedDueDate = ""
		}
	}
	if d := strings.TrimSpace(op.Description); d != "" {
		req.Description = d
	}
	if op.IntervalHours != nil {
		req.IntervalHours = op.IntervalHours
	}
	if op.IntervalMonths != nil {
		req.IntervalMonths = op.IntervalMonths
	}
	if op.DueSoonHours != nil {
		req.DueSoonHours = op.DueSoonHours
	}
	if op.DueSoonMonths != nil {
		req.DueSoonMonths = op.DueSoonMonths
	}
	if op.FixedDueDate != "" {
		req.FixedDueDate = op.FixedDueDate
	}
	return req, nil
}

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

// ruleUnchanged reports whether req would leave rule exactly as it is.
func ruleUnchanged(rule maintenanceRule, req maintenanceRuleRequest) bool {
	return rule.Description == strings.TrimSpace(req.Description) &&
		sameFloat(rule.IntervalHours, req.IntervalHours) && sameInt(rule.IntervalMonths, req.IntervalMonths) &&
		sameFloat(rule.DueSoonHours, req.DueSoonHours) && sameInt(rule.DueSoonMonths, req.DueSoonMonths) &&
		rule.FixedDueDate == strings.TrimSpace(req.FixedDueDate)
}

// ── summaries ───────────────────────────────────────────────────────────

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

// proposalRuleLabel is "Generator · Oil and filter", or just the description
// for a calendar-only rule.
func proposalRuleLabel(itemName, description string) string {
	if itemName == "" {
		return description
	}
	return itemName + " · " + description
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

// ── propose ─────────────────────────────────────────────────────────────

func (d assistantToolDeps) executeProposeMaintenanceChanges(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	store, err := d.maintenanceStore(assistantProposalToolTag)
	if err != nil {
		return "", err
	}
	if _, err := d.requireToday(assistantProposalToolTag); err != nil {
		return "", err
	}

	// Strict decode: an unknown field is a misspelt change Mate believes it
	// proposed, so it fails the call and names the field.
	var args assistantProposeArgs
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&args); err != nil {
		return "", fmt.Errorf("parse %s arguments: %w", assistantProposalToolTag, err)
	}
	if len(args.Ops) == 0 {
		return "", fmt.Errorf("%s: ops is required and must list at least one change", assistantProposalToolTag)
	}
	if len(args.Ops) > assistantProposalMaxOps {
		return "", fmt.Errorf("%s: at most %d changes in one proposal, got %d", assistantProposalToolTag, assistantProposalMaxOps, len(args.Ops))
	}

	var profiles []engineProfile
	if d.profiles != nil {
		profiles = d.profiles()
	}

	ops := make([]assistantProposalOp, len(args.Ops))
	for i, op := range args.Ops {
		// Output-only fields are never taken from the model.
		op.Summary, op.RuleUpdatedAt = "", ""
		prepared, err := prepareProposalOp(store, profiles, op)
		if err != nil {
			return "", fmt.Errorf("%s: ops[%d] (%s): %w", assistantProposalToolTag, i, op.Op, asProposalError(err))
		}
		ops[i] = prepared
	}

	if err := dryRunProposal(store, d.today, profiles, ops); err != nil {
		return "", fmt.Errorf("%s: %w", assistantProposalToolTag, err)
	}

	result := assistantProposeResult{
		Proposal: assistantProposal{ID: uuid.NewString(), Ops: ops, Status: assistantProposalPending},
		NextStep: "This is a proposal, not a change. Nothing has been written. The operator sees it as a card under your " +
			"reply with Apply and Dismiss buttons. Tell them to tap Apply to make the change, and never say it is done.",
	}
	body, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("%s: marshal proposal: %w", assistantProposalToolTag, err)
	}
	return string(body), nil
}

// prepareProposalOp validates one operation with the commands' own
// validators and read-only lookups, and returns it with its Summary and
// RuleUpdatedAt filled in. It never writes.
func prepareProposalOp(store *documentStore, profiles []engineProfile, op assistantProposalOp) (assistantProposalOp, error) {
	allowed, ok := proposalOpAllowedFields[op.Op]
	if !ok {
		return op, fieldErr("op", "unknown op %q; valid ops are create_rule, update_rule, set_last_done, copy_profile_schedule, complete_rule and acknowledge", op.Op)
	}
	for _, f := range op.setFields() {
		if !containsString(allowed, f) {
			return op, fieldErr(f, "does not apply to %s", op.Op)
		}
	}

	// ruleFor loads the rule this operation targets and its item's name,
	// snapshotting updated_at for the stale check.
	ruleFor := func() (maintenanceRule, string, error) {
		id := strings.TrimSpace(op.RuleID)
		if id == "" {
			return maintenanceRule{}, "", fieldErr("rule_id", "is required (an id from list_maintenance)")
		}
		rule, err := store.GetMaintenanceRule(id)
		if err != nil {
			if errors.Is(err, errMaintenanceRuleNotFound) {
				return maintenanceRule{}, "", fieldErr("rule_id", "no maintenance rule with id %q (find it with list_maintenance)", id)
			}
			return maintenanceRule{}, "", err
		}
		name := ""
		if rule.EquipmentID != nil {
			eq, err := store.GetEquipment(*rule.EquipmentID)
			if err != nil {
				return maintenanceRule{}, "", err
			}
			name = eq.Name
		}
		op.RuleID = id
		op.RuleUpdatedAt = rule.UpdatedAt.UTC().Format(time.RFC3339)
		return rule, name, nil
	}
	equipmentFor := func() (equipmentItem, error) {
		id := strings.TrimSpace(op.EquipmentID)
		if id == "" {
			return equipmentItem{}, fieldErr("equipment_id", "is required (an id from find_equipment)")
		}
		eq, err := store.GetEquipment(id)
		if err != nil {
			if errors.Is(err, errEquipmentNotFound) {
				return equipmentItem{}, fieldErr("equipment_id", "no equipment with id %q (find it with find_equipment)", id)
			}
			return equipmentItem{}, err
		}
		op.EquipmentID = id
		return eq, nil
	}

	switch op.Op {
	case proposalOpCreateRule:
		itemName := ""
		if strings.TrimSpace(op.EquipmentID) != "" {
			eq, err := equipmentFor()
			if err != nil {
				return op, err
			}
			itemName = eq.Name
		}
		in, verr := validateMaintenanceRuleInput(op.createRequest())
		if verr != nil {
			return op, verr
		}
		op.Description = in.Description
		summary := "Add " + proposalRuleLabel(itemName, in.Description) + ": " +
			proposalIntervalPhrase(in.IntervalHours, in.IntervalMonths, in.FixedDueDate, in.DueSoonHours, in.DueSoonMonths)
		if op.LastDoneAt != "" || op.LastDoneMeterReading != nil {
			if op.LastDoneMeterReading != nil && strings.TrimSpace(op.EquipmentID) == "" {
				return op, fieldErr("last_done_meter_reading", "a rule with no equipment has no hour meter to take a reading from")
			}
			ld, verr := validateMaintenanceLastDone(op.lastDoneRequest())
			if verr != nil {
				return op, verr
			}
			if ld.LastDoneAt != nil {
				op.LastDoneAt = *ld.LastDoneAt
			}
			summary += ", last done " + proposalDonePhrase(op.LastDoneAt, op.LastDoneMeterReading)
		}
		op.Summary = summary

	case proposalOpUpdateRule:
		rule, itemName, err := ruleFor()
		if err != nil {
			return op, err
		}
		req, err := mergeRuleUpdate(rule, op)
		if err != nil {
			return op, err
		}
		in, verr := validateMaintenanceRuleInput(req)
		if verr != nil {
			return op, verr
		}
		if ruleUnchanged(rule, req) {
			return op, fieldErr("rule_id", "the change leaves %q exactly as it is; propose only what differs", rule.Description)
		}
		summary := "Change " + proposalRuleLabel(itemName, rule.Description) + ": "
		if in.Description != rule.Description {
			summary += fmt.Sprintf("rename to %q, ", in.Description)
		}
		summary += "now " + proposalIntervalPhrase(in.IntervalHours, in.IntervalMonths, in.FixedDueDate, in.DueSoonHours, in.DueSoonMonths)
		op.Summary = summary

	case proposalOpSetLastDone:
		rule, itemName, err := ruleFor()
		if err != nil {
			return op, err
		}
		ld, verr := validateMaintenanceLastDone(op.lastDoneRequest())
		if verr != nil {
			return op, verr
		}
		if ld.LastDoneAt != nil {
			op.LastDoneAt = *ld.LastDoneAt
		}
		if op.LastDoneMeterReading != nil && rule.EquipmentID == nil {
			return op, fieldErr("last_done_meter_reading", "a rule with no equipment has no hour meter to take a reading from")
		}
		if op.LastDoneAt == "" && op.LastDoneMeterReading == nil {
			return op, fieldErr("last_done_at", "give a date and/or a meter reading")
		}
		op.Summary = proposalRuleLabel(itemName, rule.Description) + ": last done " + proposalDonePhrase(op.LastDoneAt, op.LastDoneMeterReading)

	case proposalOpCompleteRule:
		rule, itemName, err := ruleFor()
		if err != nil {
			return op, err
		}
		in, verr := validateMaintenanceLogEntryCore(op.completeRequest())
		if verr != nil {
			return op, verr
		}
		if _, _, err := planMaintenanceCompletion(rule, in, op.NewDueDate); err != nil {
			return op, err
		}
		if op.MeterReading != nil && rule.EquipmentID == nil {
			return op, fieldErr("meter_reading", "a rule with no equipment has no hour meter to take a reading from")
		}
		op.PerformedAt = in.PerformedAt
		summary := "Log " + proposalRuleLabel(itemName, rule.Description) + " as done " + proposalDonePhrase(in.PerformedAt, op.MeterReading)
		if in.Who != "" {
			summary += ", by " + in.Who
		}
		if in.Cost != nil {
			summary += ", cost " + formatProposalNumber(*in.Cost)
		}
		if in.Description != "" {
			summary += fmt.Sprintf(" (%s)", in.Description)
		}
		op.Summary = summary

	case proposalOpAcknowledge:
		rule, itemName, err := ruleFor()
		if err != nil {
			return op, err
		}
		reason := strings.TrimSpace(op.Reason)
		if reason == "" {
			return op, fieldErr("reason", "is required to acknowledge a rule")
		}
		op.Reason = reason
		op.Summary = "Acknowledge " + proposalRuleLabel(itemName, rule.Description) + ": " + reason

	case proposalOpCopyProfile:
		eq, err := equipmentFor()
		if err != nil {
			return op, err
		}
		profile, cerr := profileForCopy(eq, profiles)
		if cerr != nil {
			return op, fieldErr("equipment_id", "%s", cerr.Message)
		}
		rules, err := store.ListMaintenanceRules(maintenanceRuleFilter{EquipmentID: eq.ID, IncludeStored: true})
		if err != nil {
			return op, err
		}
		have := map[string]bool{}
		for _, r := range rules {
			if r.ProfileServiceID != "" {
				have[r.ProfileServiceID] = true
			}
		}
		missing := 0
		for _, svc := range profile.Service {
			if !have[svc.ID] {
				missing++
			}
		}
		if missing == 0 {
			return op, fieldErr("equipment_id", "every entry in the %s schedule already has a rule on %s; there is nothing to copy", profile.Name, eq.Name)
		}
		op.Summary = fmt.Sprintf("%s: copy %d from the %s schedule", eq.Name, missing, profile.Name)
	}
	return op, nil
}

// checkMaintenanceProposalFresh is the stale check: every rule an operation
// targets must still carry the updated_at Mate saw. A rule that has since been
// deleted counts as changed.
func checkMaintenanceProposalFresh(q sqlQueryer, ops []assistantProposalOp) error {
	for _, op := range ops {
		if op.RuleUpdatedAt == "" {
			continue
		}
		rule, err := maintenanceRuleByID(q, op.RuleID)
		if errors.Is(err, errMaintenanceRuleNotFound) {
			return errAssistantProposalStale
		}
		if err != nil {
			return err
		}
		if rule.UpdatedAt.UTC().Format(time.RFC3339) != op.RuleUpdatedAt {
			return errAssistantProposalStale
		}
	}
	return nil
}

// applyMaintenanceProposalOp runs one operation with the same commands the
// Maintenance handlers use, inside the caller's transaction.
func applyMaintenanceProposalOp(tx *sql.Tx, now, today time.Time, profiles []engineProfile, op assistantProposalOp) (assistantProposalOpResult, error) {
	res := assistantProposalOpResult{Op: op.Op}
	switch op.Op {
	case proposalOpCreateRule:
		rule, err := cmdCreateMaintenanceRule(tx, now, op.createRequest())
		if err != nil {
			return res, err
		}
		if op.LastDoneAt != "" || op.LastDoneMeterReading != nil {
			if _, err := cmdSetMaintenanceRuleLastDone(tx, now, today, rule.ID, op.lastDoneRequest()); err != nil {
				return res, err
			}
		}
		res.RuleIDs = []string{rule.ID}
	case proposalOpUpdateRule:
		rule, err := maintenanceRuleByID(tx, op.RuleID)
		if err != nil {
			return res, err
		}
		req, err := mergeRuleUpdate(rule, op)
		if err != nil {
			return res, asProposalError(err)
		}
		if _, err := cmdUpdateMaintenanceRule(tx, now, op.RuleID, req); err != nil {
			return res, err
		}
		res.RuleIDs = []string{op.RuleID}
	case proposalOpSetLastDone:
		if _, err := cmdSetMaintenanceRuleLastDone(tx, now, today, op.RuleID, op.lastDoneRequest()); err != nil {
			return res, err
		}
		res.RuleIDs = []string{op.RuleID}
	case proposalOpCompleteRule:
		before, err := maintenanceRuleByID(tx, op.RuleID)
		if err != nil {
			return res, err
		}
		after, entry, err := cmdCompleteMaintenanceRule(tx, now, op.RuleID, op.completeRequest())
		if err != nil {
			return res, err
		}
		res.RuleIDs = []string{op.RuleID}
		res.EntryID = entry.ID
		if after.FixedDueDate != "" && after.FixedDueDate != before.FixedDueDate {
			res.NewFixedDueDate = after.FixedDueDate
		}
	case proposalOpAcknowledge:
		if _, err := cmdAcknowledgeMaintenanceRule(tx, now, op.RuleID, op.Reason); err != nil {
			return res, err
		}
		res.RuleIDs = []string{op.RuleID}
	case proposalOpCopyProfile:
		created, _, err := cmdCopyMaintenanceProfileSchedule(tx, now, op.EquipmentID, profiles)
		if err != nil {
			return res, err
		}
		for _, r := range created {
			res.RuleIDs = append(res.RuleIDs, r.ID)
			res.CopiedServiceIDs = append(res.CopiedServiceIDs, r.ProfileServiceID)
			res.CopiedNames = append(res.CopiedNames, r.Description)
		}
		if len(res.CopiedServiceIDs) == 0 {
			if op.CopyServiceIDs != nil {
				return res, errAssistantProposalStale
			}
			return res, fmt.Errorf("equipment_id: every entry in the profile's schedule already has a rule; there is nothing to copy")
		}
		// Dry run has no snapshot yet; a real Apply always does.
		if op.CopyServiceIDs != nil && !sameStringSet(op.CopyServiceIDs, res.CopiedServiceIDs) {
			return res, errAssistantProposalStale
		}
	default:
		return res, fmt.Errorf("unknown op %q", op.Op)
	}
	return res, nil
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, x := range a {
		seen[x]++
	}
	for _, x := range b {
		seen[x]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

// errProposalDryRunDone ends the dry run's transaction: RunMaintenanceTx
// commits only when its function returns nil, so returning this always rolls
// back. It never leaves dryRunProposal.
var errProposalDryRunDone = errors.New("dry run finished")

// dryRunProposal runs every operation, in order, through the same commands
// Apply uses, inside a transaction that is always rolled back, and then writes
// each operation's summary from what the run actually produced. Validating
// operations one at a time against the current schedule would miss what an
// earlier operation in the same proposal changes (a new hours interval that
// makes a later completion need a reading); this cannot. Nothing is
// committed, so the propose tool still writes nothing.
//
// RunMaintenanceTx holds the document store's mutex for the run, so a round's
// concurrent tool calls queue behind it rather than deadlock: the run waits on
// nothing but the database file's write lock, which a competing Apply holds
// only briefly and never while waiting on this mutex.
func dryRunProposal(store *documentStore, today time.Time, profiles []engineProfile, ops []assistantProposalOp) error {
	results := make([]assistantProposalOpResult, len(ops))
	err := store.RunMaintenanceTx(func(tx *sql.Tx, now time.Time) error {
		for i, op := range ops {
			res, err := applyMaintenanceProposalOp(tx, now, today, profiles, op)
			if err != nil {
				return fmt.Errorf("ops[%d] (%s): %w", i, op.Op, asProposalError(err))
			}
			results[i] = res
		}
		return errProposalDryRunDone
	})
	if !errors.Is(err, errProposalDryRunDone) {
		return err
	}
	for i := range ops {
		refineProposalSummary(&ops[i], results[i])
	}
	return nil
}

// refineProposalSummary adds what only the dry run knows: the fixed due date a
// completion moves a rule to, and the actual rules a profile copy creates.
func refineProposalSummary(op *assistantProposalOp, res assistantProposalOpResult) {
	switch op.Op {
	case proposalOpCompleteRule:
		if res.NewFixedDueDate != "" {
			op.Summary += ", next due " + formatProposalDate(res.NewFixedDueDate)
		}
	case proposalOpCopyProfile:
		op.CopyServiceIDs = res.CopiedServiceIDs
		noun := "entries"
		if len(res.CopiedNames) == 1 {
			noun = "entry"
		}
		// op.Summary is "<item>: copy N from the <profile> schedule".
		head := op.Summary[:strings.Index(op.Summary, ": copy ")]
		profile := op.Summary[strings.Index(op.Summary, " from the "):]
		op.Summary = fmt.Sprintf("%s: copy %d %s%s (%s)", head, len(res.CopiedNames), noun, profile, strings.Join(res.CopiedNames, ", "))
	}
}
