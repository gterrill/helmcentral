package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// This file is the one place an item's effective maintenance schedule comes
// from (ADR 0148). An item's schedule is its equipment profile's service jobs,
// read live every time, plus the item's own hand-made rules.
//
// A profile job for an item has the deterministic id
// job:<equipment_id>:<service_id>. Its per-item state (last done,
// acknowledgement, due-soon window, procedure note, fixed due date, and any
// overrides) lives in a maintenance_rules row with that id, created the first
// time something is written to the job. A job nobody has touched has no row
// and still lists under its id. The id leaves the profile id out on purpose:
// moving an item between two profiles that share a service id keeps that
// service's state.
//
// Effective values are computed here and nowhere else. Nothing may read the
// interval or description columns of a job: row directly; for a job those
// columns are either NULL (follow the profile), an override, or a snapshot
// used only to label a job that has since left the profile.

const maintenanceJobPrefix = "job:"

func maintenanceJobID(equipmentID, serviceID string) string {
	return maintenanceJobPrefix + equipmentID + ":" + serviceID
}

func isMaintenanceJobID(id string) bool { return strings.HasPrefix(id, maintenanceJobPrefix) }

// parseMaintenanceJobID splits job:<equipment_id>:<service_id>. Neither part
// may contain a colon (equipment ids are UUIDs, service ids match
// profileIDPattern).
func parseMaintenanceJobID(id string) (equipmentID, serviceID string, ok bool) {
	if !isMaintenanceJobID(id) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(id, maintenanceJobPrefix), ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// The fields an item may override on a profile job. Stored in this order.
const (
	overrideDescription    = "description"
	overrideIntervalHours  = "interval_hours"
	overrideIntervalMonths = "interval_months"
	overrideNotApplicable  = "not_applicable"
)

var maintenanceOverrideFields = []string{overrideDescription, overrideIntervalHours, overrideIntervalMonths, overrideNotApplicable}

func isMaintenanceOverrideField(name string) bool {
	return containsString(maintenanceOverrideFields, name)
}

// encodeOverriddenFields is the stored form of a set of field names: a comma
// list in the fixed order above, "" for none.
func encodeOverriddenFields(set map[string]bool) string {
	var out []string
	for _, f := range maintenanceOverrideFields {
		if set[f] {
			out = append(out, f)
		}
	}
	return strings.Join(out, ",")
}

func decodeOverriddenFields(stored string) []string {
	out := []string{}
	if strings.TrimSpace(stored) == "" {
		return out
	}
	for _, f := range strings.Split(stored, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

func overriddenSet(fields []string) map[string]bool {
	set := make(map[string]bool, len(fields))
	for _, f := range fields {
		set[f] = true
	}
	return set
}

// ── profiles ─────────────────────────────────────────────────────────────

// maintenanceProfileSet is the profiles a schedule is resolved against: the
// valid ones by id, and the text of why each invalid one failed to load.
type maintenanceProfileSet struct {
	byID     map[string]engineProfile
	problems map[string]string
}

func newMaintenanceProfileSet(profiles []engineProfile, problems []engineProfileProblem) maintenanceProfileSet {
	set := maintenanceProfileSet{byID: map[string]engineProfile{}, problems: map[string]string{}}
	for _, p := range profiles {
		set.byID[p.ID] = p
	}
	for _, p := range problems {
		set.problems[p.ID] = p.Error
	}
	return set
}

// currentMaintenanceProfiles is the set built from the in-memory profile cache.
func currentMaintenanceProfiles() maintenanceProfileSet {
	profiles, problems := engineProfiles()
	return newMaintenanceProfileSet(profiles, problems)
}

// lookup finds a profile, or says why it cannot be used: the loader's own
// problem text for a profile that failed validation, "profile not found" for
// one that is not stored. A failure to read the store at all (a problem with
// no id) is reported instead of "not found", since it is the real reason.
func (s maintenanceProfileSet) lookup(id string) (engineProfile, string, bool) {
	if p, ok := s.byID[id]; ok {
		return p, "", true
	}
	if text, ok := s.problems[id]; ok {
		return engineProfile{}, text, false
	}
	if text, ok := s.problems[""]; ok {
		return engineProfile{}, text, false
	}
	return engineProfile{}, "profile not found", false
}

// ── types ────────────────────────────────────────────────────────────────

// maintenanceProfileValues is what an item's profile says about a job, shown
// beside an override so the operator sees what Reset would restore.
type maintenanceProfileValues struct {
	Description    string   `json:"description"`
	IntervalHours  *float64 `json:"interval_hours"`
	IntervalMonths *int     `json:"interval_months"`
}

// effectiveMaintenanceRule is one rule as the schedule shows it. The embedded
// maintenanceRule carries EFFECTIVE description and intervals: for a job, the
// profile's values with the item's overrides applied; for a hand rule, the
// stored ones. Its state fields (last done, acknowledgement, due-soon window,
// fixed due date, procedure note) are the row's.
type effectiveMaintenanceRule struct {
	maintenanceRule
	// Source is "profile" for a job and "item" for a hand rule.
	Source    string
	ProfileID string
	// ProfileValues is nil for a hand rule.
	ProfileValues *maintenanceProfileValues
	FirstAtHours  *float64
	Supersedes    []string
	// RemovedFromProfile marks a job row whose service is no longer in the
	// item's profile. It is never part of the schedule; only its snapshot
	// description and history remain.
	RemovedFromProfile bool
	// HasRow reports whether the job has a stored row yet.
	HasRow bool
}

// maintenanceScheduleError says an item's profile could not supply its jobs.
type maintenanceScheduleError struct {
	EquipmentID   string `json:"equipment_id"`
	EquipmentName string `json:"equipment_name"`
	ProfileID     string `json:"profile_id"`
	Error         string `json:"error"`
}

type maintenanceSchedule struct {
	Rules []effectiveMaintenanceRule
	// Removed is filled only when the request named one equipment item.
	Removed []effectiveMaintenanceRule
	Errors  []maintenanceScheduleError
}

// scheduleItem is the little of an equipment row the schedule needs.
type scheduleItem struct {
	ID        string
	Name      string
	ProfileID string
}

func scheduleItemByID(q sqlQueryer, id string) (scheduleItem, error) {
	var it scheduleItem
	err := q.QueryRow(`SELECT id, name, profile_id FROM equipment WHERE id = ?`, id).Scan(&it.ID, &it.Name, &it.ProfileID)
	if errors.Is(err, sql.ErrNoRows) {
		return scheduleItem{}, errEquipmentNotFound
	}
	if err != nil {
		return scheduleItem{}, fmt.Errorf("maintenance schedule: read equipment %s: %w", id, err)
	}
	return it, nil
}

// ── building effective rules ─────────────────────────────────────────────

func copyFloat(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func copyInt(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// effectiveJob is the job for svc on the item, from the profile's live values
// with the row's overrides (if any) applied and the row's state carried over.
func effectiveJob(equipmentID, profileID string, svc engineProfileService, row *maintenanceRule) effectiveMaintenanceRule {
	eqID := equipmentID
	rule := maintenanceRule{
		ID:               maintenanceJobID(equipmentID, svc.ID),
		EquipmentID:      &eqID,
		ProfileServiceID: svc.ID,
		OverriddenFields: []string{},
	}
	overridden := map[string]bool{}
	if row != nil {
		rule = *row
		overridden = overriddenSet(row.OverriddenFields)
	}
	if !overridden[overrideDescription] {
		rule.Description = svc.Description
	}
	if !overridden[overrideIntervalHours] {
		rule.IntervalHours = copyFloat(svc.IntervalHours)
	}
	if !overridden[overrideIntervalMonths] {
		rule.IntervalMonths = copyInt(svc.IntervalMonths)
	}
	if !overridden[overrideNotApplicable] {
		rule.NotApplicable = false
	}
	if rule.OverriddenFields == nil {
		rule.OverriddenFields = []string{}
	}
	var supersedes []string
	if len(svc.Supersedes) > 0 {
		supersedes = append([]string(nil), svc.Supersedes...)
	}
	return effectiveMaintenanceRule{
		maintenanceRule: rule,
		Source:          "profile",
		ProfileID:       profileID,
		ProfileValues: &maintenanceProfileValues{
			Description:    svc.Description,
			IntervalHours:  copyFloat(svc.IntervalHours),
			IntervalMonths: copyInt(svc.IntervalMonths),
		},
		FirstAtHours: copyFloat(svc.FirstAtHours),
		Supersedes:   supersedes,
		HasRow:       row != nil,
	}
}

// removedJob is a job row whose service the item's profile no longer has. Its
// intervals are unknown (the row holds none of the profile's), so none are
// shown; the description is the snapshot taken at the last write.
func removedJob(row maintenanceRule) effectiveMaintenanceRule {
	row.IntervalHours, row.IntervalMonths = nil, nil
	return effectiveMaintenanceRule{
		maintenanceRule:    row,
		Source:             "profile",
		RemovedFromProfile: true,
		HasRow:             true,
	}
}

func handRule(row maintenanceRule) effectiveMaintenanceRule {
	row.OverriddenFields = []string{}
	return effectiveMaintenanceRule{maintenanceRule: row, Source: "item", HasRow: true}
}

// checkStoredRuleShape fails on a row the job model cannot interpret: a
// legacy copied rule that convert-profile-rules has not converted, or a job
// row whose id disagrees with its columns.
func checkStoredRuleShape(r maintenanceRule) error {
	if isMaintenanceJobID(r.ID) {
		eq, svc, ok := parseMaintenanceJobID(r.ID)
		if !ok || r.EquipmentID == nil || *r.EquipmentID != eq || r.ProfileServiceID != svc {
			return fmt.Errorf("maintenance rule %s is not a well-formed profile job row", r.ID)
		}
		return nil
	}
	if r.ProfileServiceID != "" {
		return fmt.Errorf("maintenance rule %s is an unconverted copy of profile service %q; run `helmcentral convert-profile-rules --apply`", r.ID, r.ProfileServiceID)
	}
	return nil
}

// ── the resolver ─────────────────────────────────────────────────────────

// buildMaintenanceSchedule is the schedule for filter: effective rules, the
// removed group (only when filter names one item), and the items whose
// profile could not be used.
//
// It returns effective VALUES only. Status needs the live hours reading and
// the operator's date, which are the view builder's job
// (buildMaintenanceRuleView); every interval the view and the completion
// checks read comes from here.
func buildMaintenanceSchedule(q sqlQueryer, profiles maintenanceProfileSet, filter maintenanceRuleFilter) (maintenanceSchedule, error) {
	sched := maintenanceSchedule{Rules: []effectiveMaintenanceRule{}, Removed: []effectiveMaintenanceRule{}, Errors: []maintenanceScheduleError{}}

	rows, err := queryMaintenanceRuleRows(q, filter)
	if err != nil {
		return sched, err
	}

	// Stored state, by item then service. Hand rules go straight through.
	jobRows := map[string]map[string]maintenanceRule{}
	var hand []effectiveMaintenanceRule
	for _, r := range rows {
		if err := checkStoredRuleShape(r); err != nil {
			return sched, err
		}
		if !isMaintenanceJobID(r.ID) {
			hand = append(hand, handRule(r))
			continue
		}
		eq := *r.EquipmentID
		if jobRows[eq] == nil {
			jobRows[eq] = map[string]maintenanceRule{}
		}
		jobRows[eq][r.ProfileServiceID] = r
	}

	items, err := queryProfiledItems(q, filter)
	if err != nil {
		return sched, err
	}
	profiled := map[string]bool{}
	unavailable := map[string]bool{}
	var jobs []effectiveMaintenanceRule
	for _, it := range items {
		profiled[it.ID] = true
		profile, problem, ok := profiles.lookup(it.ProfileID)
		if !ok {
			unavailable[it.ID] = true
			sched.Errors = append(sched.Errors, maintenanceScheduleError{EquipmentID: it.ID, EquipmentName: it.Name, ProfileID: it.ProfileID, Error: problem})
			continue
		}
		seen := map[string]bool{}
		for _, svc := range profile.Service {
			seen[svc.ID] = true
			var row *maintenanceRule
			if r, ok := jobRows[it.ID][svc.ID]; ok {
				r := r
				row = &r
			}
			jobs = append(jobs, effectiveJob(it.ID, profile.ID, svc, row))
		}
		for svcID, r := range jobRows[it.ID] {
			if !seen[svcID] {
				sched.Removed = append(sched.Removed, removedJob(r))
			}
		}
	}
	// Rows of items with no profile at all (it was cleared): parked.
	for eqID, byService := range jobRows {
		if profiled[eqID] {
			continue
		}
		for _, r := range byService {
			sched.Removed = append(sched.Removed, removedJob(r))
		}
	}

	sched.Rules = append(sched.Rules, jobs...)
	sched.Rules = append(sched.Rules, hand...)
	sortEffectiveRules(sched.Rules)
	sortEffectiveRules(sched.Removed)
	if filter.EquipmentID == "" {
		sched.Removed = []effectiveMaintenanceRule{}
	}
	sort.Slice(sched.Errors, func(i, j int) bool { return sched.Errors[i].EquipmentID < sched.Errors[j].EquipmentID })
	return sched, nil
}

// sortEffectiveRules orders like the old list: calendar-only rules last, then
// by item, then by description.
func sortEffectiveRules(rules []effectiveMaintenanceRule) {
	sort.SliceStable(rules, func(i, j int) bool {
		a, b := rules[i], rules[j]
		an, bn := a.EquipmentID == nil, b.EquipmentID == nil
		if an != bn {
			return !an
		}
		if !an && *a.EquipmentID != *b.EquipmentID {
			return *a.EquipmentID < *b.EquipmentID
		}
		ad, bd := strings.ToLower(a.Description), strings.ToLower(b.Description)
		if ad != bd {
			return ad < bd
		}
		return a.ID < b.ID
	})
}

// maintenanceRuleFilterSQL is the WHERE clause shared by the row and item
// queries, so a job and its state always follow the same filter.
func maintenanceRuleFilterSQL(filter maintenanceRuleFilter, idColumn string) (string, []any) {
	var sb strings.Builder
	var args []any
	if filter.EquipmentID != "" {
		sb.WriteString(` AND ` + idColumn + ` = ?`)
		args = append(args, filter.EquipmentID)
	}
	if !filter.IncludeStored {
		sb.WriteString(` AND (` + idColumn + ` IS NULL OR ` + idColumn + ` IN (SELECT id FROM equipment WHERE status != 'stored'))`)
	}
	if filter.System != "" {
		sb.WriteString(` AND ` + idColumn + ` IN (SELECT id FROM equipment WHERE system = ?)`)
		args = append(args, filter.System)
	}
	return sb.String(), args
}

func queryMaintenanceRuleRows(q sqlQueryer, filter maintenanceRuleFilter) ([]maintenanceRule, error) {
	where, args := maintenanceRuleFilterSQL(filter, "equipment_id")
	rows, err := q.Query(`SELECT `+maintenanceRuleColumns+` FROM maintenance_rules WHERE 1=1`+where+` ORDER BY equipment_id IS NULL, equipment_id, lower(description)`, args...)
	if err != nil {
		return nil, fmt.Errorf("maintenance schedule: read rules: %w", err)
	}
	defer rows.Close()
	out := []maintenanceRule{}
	for rows.Next() {
		r, err := scanMaintenanceRule(rows)
		if err != nil {
			return nil, fmt.Errorf("maintenance schedule: scan rule: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func queryProfiledItems(q sqlQueryer, filter maintenanceRuleFilter) ([]scheduleItem, error) {
	where, args := maintenanceRuleFilterSQL(filter, "id")
	rows, err := q.Query(`SELECT id, name, profile_id FROM equipment WHERE profile_id != ''`+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("maintenance schedule: read items: %w", err)
	}
	defer rows.Close()
	var out []scheduleItem
	for rows.Next() {
		var it scheduleItem
		if err := rows.Scan(&it.ID, &it.Name, &it.ProfileID); err != nil {
			return nil, fmt.Errorf("maintenance schedule: scan item: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// profileUnavailableError is the 409 for touching a job whose item's profile
// cannot supply it.
func profileUnavailableError(it scheduleItem, problem string) *maintenanceCommandError {
	return &maintenanceCommandError{
		Status:  http.StatusConflict,
		Message: fmt.Sprintf("%s's profile %q is unavailable (%s), so its profile jobs cannot be read or changed until that is fixed", it.Name, it.ProfileID, problem),
	}
}

// effectiveRuleByID resolves one rule by id. A job id resolves whether or not
// it has a row yet. A job whose item's profile is unavailable is a 409
// (*maintenanceCommandError), a job the profile does not have and no row
// remembers is errMaintenanceRuleNotFound.
func effectiveRuleByID(q sqlQueryer, profiles maintenanceProfileSet, id string) (effectiveMaintenanceRule, error) {
	if !isMaintenanceJobID(id) {
		row, err := maintenanceRuleByID(q, id)
		if err != nil {
			return effectiveMaintenanceRule{}, err
		}
		if err := checkStoredRuleShape(row); err != nil {
			return effectiveMaintenanceRule{}, err
		}
		return handRule(row), nil
	}
	eqID, svcID, ok := parseMaintenanceJobID(id)
	if !ok {
		return effectiveMaintenanceRule{}, errMaintenanceRuleNotFound
	}
	it, err := scheduleItemByID(q, eqID)
	if errors.Is(err, errEquipmentNotFound) {
		return effectiveMaintenanceRule{}, errMaintenanceRuleNotFound
	}
	if err != nil {
		return effectiveMaintenanceRule{}, err
	}
	var row *maintenanceRule
	stored, err := maintenanceRuleByID(q, id)
	switch {
	case err == nil:
		if err := checkStoredRuleShape(stored); err != nil {
			return effectiveMaintenanceRule{}, err
		}
		row = &stored
	case errors.Is(err, errMaintenanceRuleNotFound):
	default:
		return effectiveMaintenanceRule{}, err
	}

	if it.ProfileID == "" {
		if row != nil {
			return removedJob(*row), nil
		}
		return effectiveMaintenanceRule{}, errMaintenanceRuleNotFound
	}
	profile, problem, ok := profiles.lookup(it.ProfileID)
	if !ok {
		return effectiveMaintenanceRule{}, profileUnavailableError(it, problem)
	}
	for _, svc := range profile.Service {
		if svc.ID == svcID {
			return effectiveJob(eqID, profile.ID, svc, row), nil
		}
	}
	if row != nil {
		return removedJob(*row), nil
	}
	return effectiveMaintenanceRule{}, errMaintenanceRuleNotFound
}

// ── writes ───────────────────────────────────────────────────────────────

// prepareMaintenanceRuleWriteTx resolves the rule a write targets and, for a
// profile job, makes sure its row exists (INSERT OR IGNORE, so the first write
// to a job creates its row inside that write's own transaction) and that its
// description snapshot follows the profile. It returns the EFFECTIVE rule:
// writes that need an interval (completion) read it from here, never from the
// row.
//
// A job that has left its profile, or whose profile is unavailable, cannot be
// written to: 409.
// refuseRemovedJobWrite is the 409 for a write to a job its profile no longer
// lists. Callers that do work before the write (creating a note) check it
// first so a refusal leaves nothing behind.
func refuseRemovedJobWrite(eff effectiveMaintenanceRule) error {
	if eff.Source != "item" && eff.RemovedFromProfile {
		return &maintenanceCommandError{
			Status:  http.StatusConflict,
			Message: fmt.Sprintf("%q is no longer in the item's profile; it can be deleted but not changed", eff.Description),
		}
	}
	return nil
}

func prepareMaintenanceRuleWriteTx(tx *sql.Tx, now time.Time, id string) (effectiveMaintenanceRule, error) {
	eff, err := effectiveRuleByID(tx, currentMaintenanceProfiles(), id)
	if err != nil {
		return effectiveMaintenanceRule{}, err
	}
	if eff.Source == "item" {
		return eff, nil
	}
	if err := refuseRemovedJobWrite(eff); err != nil {
		return effectiveMaintenanceRule{}, err
	}
	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO maintenance_rules (
			id, equipment_id, description, interval_hours, interval_months,
			due_soon_hours, due_soon_months, fixed_due_date, last_done_at, last_done_hours,
			profile_service_id, procedure_note_id, ack_reason, ack_at, created_at, updated_at,
			overridden_fields, not_applicable
		) VALUES (?, ?, ?, NULL, NULL, NULL, NULL, '', '', NULL, ?, NULL, '', NULL, ?, ?, '', 0)`,
		eff.ID, *eff.EquipmentID, eff.ProfileValues.Description, eff.ProfileServiceID, now.Unix(), now.Unix(),
	); err != nil {
		return effectiveMaintenanceRule{}, fmt.Errorf("create job row %s: %w", eff.ID, err)
	}
	if !containsString(eff.OverriddenFields, overrideDescription) {
		if _, err := tx.Exec(`UPDATE maintenance_rules SET description = ? WHERE id = ? AND description != ?`,
			eff.ProfileValues.Description, eff.ID, eff.ProfileValues.Description); err != nil {
			return effectiveMaintenanceRule{}, fmt.Errorf("refresh job snapshot %s: %w", eff.ID, err)
		}
	}
	return eff, nil
}

// maintenanceIntervalHoursOverride and ...MonthsOverride carry an override of
// an interval. A nil Value is an override to "none".
type maintenanceIntervalHoursOverride struct{ Value *float64 }
type maintenanceIntervalMonthsOverride struct{ Value *int }

// maintenanceNullableFloat and maintenanceNullableInt carry per-item state
// that may be cleared: a nil Value clears it.
type maintenanceNullableFloat struct{ Value *float64 }
type maintenanceNullableInt struct{ Value *int }

// maintenanceOverridesInput is a set of changes to apply to a job. A nil
// field is left alone. Description, IntervalHours, IntervalMonths and
// NotApplicable are overrides of the profile (recorded in overridden_fields).
// DueSoonHours, DueSoonMonths and FixedDueDate are plain per-item state: the
// profile has no value for them, so they are written to the row and never
// marked overridden, and there is no reset (a nil Value / "" clears them).
type maintenanceOverridesInput struct {
	Description    *string
	IntervalHours  *maintenanceIntervalHoursOverride
	IntervalMonths *maintenanceIntervalMonthsOverride
	NotApplicable  *bool

	DueSoonHours  *maintenanceNullableFloat
	DueSoonMonths *maintenanceNullableInt
	// FixedDueDate is YYYY-MM-DD, or "" to clear.
	FixedDueDate *string
}

func jobOverrideError(id string) error {
	return &inventoryValidationError{Field: "id", Message: "only a profile job has overrides; " + id + " is an item's own rule, edit it directly"}
}

// cmdSetMaintenanceRuleOverrides overrides fields of a profile job for this
// item. not_applicable false is the same as resetting it.
func cmdSetMaintenanceRuleOverrides(tx *sql.Tx, now time.Time, id string, in maintenanceOverridesInput) (maintenanceRule, error) {
	if !isMaintenanceJobID(id) {
		return maintenanceRule{}, jobOverrideError(id)
	}
	if in.Description != nil {
		trimmed := strings.TrimSpace(*in.Description)
		if trimmed == "" {
			return maintenanceRule{}, &inventoryValidationError{Field: "description", Message: "description cannot be blank"}
		}
		in.Description = &trimmed
	}
	if in.IntervalHours != nil && in.IntervalHours.Value != nil && *in.IntervalHours.Value <= 0 {
		return maintenanceRule{}, &inventoryValidationError{Field: "interval_hours", Message: "interval_hours must be greater than zero"}
	}
	if in.IntervalMonths != nil && in.IntervalMonths.Value != nil && *in.IntervalMonths.Value < 1 {
		return maintenanceRule{}, &inventoryValidationError{Field: "interval_months", Message: "interval_months must be at least 1"}
	}
	if in.DueSoonHours != nil && in.DueSoonHours.Value != nil && *in.DueSoonHours.Value < 0 {
		return maintenanceRule{}, &inventoryValidationError{Field: "due_soon_hours", Message: "due_soon_hours cannot be negative"}
	}
	if in.DueSoonMonths != nil && in.DueSoonMonths.Value != nil && *in.DueSoonMonths.Value < 0 {
		return maintenanceRule{}, &inventoryValidationError{Field: "due_soon_months", Message: "due_soon_months cannot be negative"}
	}
	if in.FixedDueDate != nil {
		trimmed := strings.TrimSpace(*in.FixedDueDate)
		if trimmed != "" && !installDatePattern.MatchString(trimmed) {
			return maintenanceRule{}, &inventoryValidationError{Field: "fixed_due_date", Message: "fixed_due_date must be blank or YYYY-MM-DD"}
		}
		in.FixedDueDate = &trimmed
	}
	if in.Description == nil && in.IntervalHours == nil && in.IntervalMonths == nil && in.NotApplicable == nil &&
		in.DueSoonHours == nil && in.DueSoonMonths == nil && in.FixedDueDate == nil {
		return maintenanceRule{}, &inventoryValidationError{Field: "description", Message: "give at least one of description, interval_hours, interval_months, not_applicable, due_soon_hours, due_soon_months, fixed_due_date"}
	}

	eff, err := prepareMaintenanceRuleWriteTx(tx, now, id)
	if err != nil {
		return maintenanceRule{}, err
	}
	row, err := maintenanceRuleByID(tx, id)
	if err != nil {
		return maintenanceRule{}, err
	}
	set := overriddenSet(row.OverriddenFields)
	if in.Description != nil {
		row.Description = *in.Description
		set[overrideDescription] = true
	}
	if in.IntervalHours != nil {
		row.IntervalHours = copyFloat(in.IntervalHours.Value)
		set[overrideIntervalHours] = true
	}
	if in.IntervalMonths != nil {
		row.IntervalMonths = copyInt(in.IntervalMonths.Value)
		set[overrideIntervalMonths] = true
	}
	if in.NotApplicable != nil {
		row.NotApplicable = *in.NotApplicable
		set[overrideNotApplicable] = *in.NotApplicable
	}
	if in.DueSoonHours != nil {
		row.DueSoonHours = copyFloat(in.DueSoonHours.Value)
	}
	if in.DueSoonMonths != nil {
		row.DueSoonMonths = copyInt(in.DueSoonMonths.Value)
	}
	if in.FixedDueDate != nil {
		row.FixedDueDate = *in.FixedDueDate
	}
	return writeJobOverridesTx(tx, now, eff, row, set)
}

// cmdResetMaintenanceRuleOverride puts one overridden field back to the profile.
func cmdResetMaintenanceRuleOverride(tx *sql.Tx, now time.Time, id, field string) (maintenanceRule, error) {
	if !isMaintenanceJobID(id) {
		return maintenanceRule{}, jobOverrideError(id)
	}
	if !isMaintenanceOverrideField(field) {
		return maintenanceRule{}, &inventoryValidationError{Field: "field", Message: "field must be one of " + strings.Join(maintenanceOverrideFields, ", ")}
	}
	eff, err := prepareMaintenanceRuleWriteTx(tx, now, id)
	if err != nil {
		return maintenanceRule{}, err
	}
	row, err := maintenanceRuleByID(tx, id)
	if err != nil {
		return maintenanceRule{}, err
	}
	set := overriddenSet(row.OverriddenFields)
	delete(set, field)
	return writeJobOverridesTx(tx, now, eff, row, set)
}

// writeJobOverridesTx stores row's overrides. A field that is not in set is
// written back as "follow the profile": NULL for an interval, the current
// profile text for the description snapshot, false for not_applicable.
func writeJobOverridesTx(tx *sql.Tx, now time.Time, eff effectiveMaintenanceRule, row maintenanceRule, set map[string]bool) (maintenanceRule, error) {
	if !set[overrideDescription] {
		row.Description = eff.ProfileValues.Description
	}
	if !set[overrideIntervalHours] {
		row.IntervalHours = nil
	}
	if !set[overrideIntervalMonths] {
		row.IntervalMonths = nil
	}
	if !set[overrideNotApplicable] {
		row.NotApplicable = false
	}
	na := 0
	if row.NotApplicable {
		na = 1
	}
	if _, err := tx.Exec(`UPDATE maintenance_rules SET description = ?, interval_hours = ?, interval_months = ?,
		not_applicable = ?, overridden_fields = ?, due_soon_hours = ?, due_soon_months = ?, fixed_due_date = ?,
		updated_at = ? WHERE id = ?`,
		row.Description, row.IntervalHours, row.IntervalMonths, na, encodeOverriddenFields(set),
		row.DueSoonHours, row.DueSoonMonths, row.FixedDueDate, now.Unix(), eff.ID); err != nil {
		return maintenanceRule{}, fmt.Errorf("write overrides %s: %w", eff.ID, err)
	}
	return maintenanceRuleByID(tx, eff.ID)
}

// cmdDeleteMaintenanceRule deletes a hand rule, or a job row whose service has
// left the profile (its log entries keep their place: rule_id goes NULL). A
// live profile job cannot be deleted; it is changed with overrides.
func cmdDeleteMaintenanceRule(tx *sql.Tx, id string) error {
	if isMaintenanceJobID(id) {
		eff, err := effectiveRuleByID(tx, currentMaintenanceProfiles(), id)
		if err != nil {
			return err
		}
		if !eff.RemovedFromProfile {
			return &inventoryValidationError{Field: "id", Message: "this job comes from the equipment profile and cannot be deleted; mark it not applicable with an override, or remove it from the profile"}
		}
		if !eff.HasRow {
			return errMaintenanceRuleNotFound
		}
	}
	res, err := tx.Exec(`DELETE FROM maintenance_rules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete maintenance rule: %w", err)
	}
	return checkRowsAffected(res, errMaintenanceRuleNotFound)
}

// ── store entry points ───────────────────────────────────────────────────

// MaintenanceSchedule is the schedule for filter against the live profiles.
func (s *documentStore) MaintenanceSchedule(filter maintenanceRuleFilter) (maintenanceSchedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return buildMaintenanceSchedule(s.db, currentMaintenanceProfiles(), filter)
}

// ResolveMaintenanceRule is one rule's effective form; see effectiveRuleByID.
func (s *documentStore) ResolveMaintenanceRule(id string) (effectiveMaintenanceRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return effectiveRuleByID(s.db, currentMaintenanceProfiles(), id)
}

// ── profile change preview ───────────────────────────────────────────────

type maintenanceProfileChangeEntry struct {
	ServiceID   string `json:"service_id"`
	Description string `json:"description"`
}

// maintenanceProfileChangePreview answers "what happens to this item's
// jobs if its profile becomes X": which services carry over with their state,
// which leave the schedule (their rows are kept, parked), and which are new.
type maintenanceProfileChangePreview struct {
	Kept    []maintenanceProfileChangeEntry `json:"kept"`
	Leaving []maintenanceProfileChangeEntry `json:"leaving"`
	New     []maintenanceProfileChangeEntry `json:"new"`
}

func previewMaintenanceProfileChange(q sqlQueryer, profiles maintenanceProfileSet, equipmentID, newProfileID string) (maintenanceProfileChangePreview, error) {
	preview := maintenanceProfileChangePreview{Kept: []maintenanceProfileChangeEntry{}, Leaving: []maintenanceProfileChangeEntry{}, New: []maintenanceProfileChangeEntry{}}
	it, err := scheduleItemByID(q, equipmentID)
	if err != nil {
		return preview, err
	}

	// What the item has now: the current profile's services (when it still
	// resolves) and every service it holds state for.
	current := map[string]string{}
	var currentOrder []string
	add := func(id, description string) {
		if _, ok := current[id]; !ok {
			currentOrder = append(currentOrder, id)
			current[id] = description
		}
	}
	if it.ProfileID != "" {
		if p, _, ok := profiles.lookup(it.ProfileID); ok {
			for _, svc := range p.Service {
				add(svc.ID, svc.Description)
			}
		}
	}
	rows, err := q.Query(`SELECT profile_service_id, description FROM maintenance_rules WHERE equipment_id = ? AND profile_service_id != '' ORDER BY profile_service_id`, equipmentID)
	if err != nil {
		return preview, fmt.Errorf("profile change preview: read rows: %w", err)
	}
	for rows.Next() {
		var id, description string
		if err := rows.Scan(&id, &description); err != nil {
			rows.Close()
			return preview, fmt.Errorf("profile change preview: scan: %w", err)
		}
		add(id, description)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return preview, err
	}
	rows.Close()

	incoming := map[string]bool{}
	if newProfileID != "" {
		p, problem, ok := profiles.lookup(newProfileID)
		if !ok {
			return preview, &inventoryValidationError{Field: "profile_id", Message: fmt.Sprintf("profile %q cannot be used: %s", newProfileID, problem)}
		}
		for _, svc := range p.Service {
			incoming[svc.ID] = true
			entry := maintenanceProfileChangeEntry{ServiceID: svc.ID, Description: svc.Description}
			if _, had := current[svc.ID]; had {
				preview.Kept = append(preview.Kept, entry)
			} else {
				preview.New = append(preview.New, entry)
			}
		}
	}
	for _, id := range currentOrder {
		if !incoming[id] {
			preview.Leaving = append(preview.Leaving, maintenanceProfileChangeEntry{ServiceID: id, Description: current[id]})
		}
	}
	return preview, nil
}

// ── history labels ───────────────────────────────────────────────────────

// maintenanceRuleHistoryLabel names the rule a log entry was completed
// against, for the CSV export: the effective description while the rule is in
// the schedule, the snapshot with a note once it has left its profile or its
// profile is unavailable. schedule is "profile", "item", or "" for a rule
// that has since been deleted.
func maintenanceRuleHistoryLabel(q sqlQueryer, profiles maintenanceProfileSet, ruleID string) (description, schedule string, err error) {
	eff, err := effectiveRuleByID(q, profiles, ruleID)
	switch {
	case err == nil && eff.RemovedFromProfile:
		return eff.Description + " (no longer in profile)", "profile", nil
	case err == nil:
		return eff.Description, eff.Source, nil
	case errors.Is(err, errMaintenanceRuleNotFound):
		return "", "", nil
	}
	var cerr *maintenanceCommandError
	if errors.As(err, &cerr) {
		// The row is history: its snapshot label is all that is wanted, and
		// it is marked as such rather than shown as a live value.
		row, rerr := maintenanceRuleByID(q, ruleID)
		if rerr != nil {
			return "", "", rerr
		}
		return row.Description + " (profile unavailable)", "profile", nil
	}
	return "", "", err
}

// ── schema guards ────────────────────────────────────────────────────────

// maintenanceJobIndexSQL makes (equipment_id, profile_service_id) unique for
// job rows, so an item can hold one row per profile service.
const maintenanceJobIndexSQL = `CREATE UNIQUE INDEX IF NOT EXISTS maintenance_rules_equipment_service
	ON maintenance_rules (equipment_id, profile_service_id) WHERE profile_service_id != ''`

func createMaintenanceJobIndex(db *sql.DB) error {
	if _, err := db.Exec(maintenanceJobIndexSQL); err != nil {
		return fmt.Errorf("create maintenance job index: %w", err)
	}
	return nil
}

// checkForUnconvertedMaintenanceRules is the startup guard: a rule row that
// still carries a profile_service_id under a plain UUID is a copy made by the
// old "Use profile schedule" action. The job model cannot read it (it would
// show a second, frozen copy of a service the profile now supplies live), so
// the server refuses to start until the operator converts it.
func checkForUnconvertedMaintenanceRules(db *sql.DB) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM maintenance_rules WHERE profile_service_id != '' AND id NOT LIKE 'job:%'`).Scan(&n); err != nil {
		return fmt.Errorf("check maintenance rules: %w", err)
	}
	if n == 0 {
		return nil
	}
	return fmt.Errorf(
		"found %d maintenance rule(s) copied from an equipment profile's schedule - schedules now follow the profile live. "+
			"Convert them first with `helmcentral convert-profile-rules --apply` (Docker Compose: "+
			"`docker compose run --rm helmcentral /app/helmcentral convert-profile-rules --apply`), then restart. "+
			"Run it without --apply to preview what it will do", n)
}

// MaintenanceRuleHistoryLabel is maintenanceRuleHistoryLabel on the store's
// connection.
func (s *documentStore) MaintenanceRuleHistoryLabel(profiles maintenanceProfileSet, ruleID string) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maintenanceRuleHistoryLabel(s.db, profiles, ruleID)
}

// PreviewMaintenanceProfileChange is previewMaintenanceProfileChange on the
// store's connection, against the live profiles.
func (s *documentStore) PreviewMaintenanceProfileChange(equipmentID, newProfileID string) (maintenanceProfileChangePreview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return previewMaintenanceProfileChange(s.db, currentMaintenanceProfiles(), equipmentID, newProfileID)
}
