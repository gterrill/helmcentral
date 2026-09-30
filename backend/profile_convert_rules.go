package main

import (
	"database/sql"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// The maintenance half of convert-profile-rules (ADR 0148). Before this
// change "Use profile schedule" copied a profile's service entries into
// maintenance_rules under fresh UUIDs. Now the profile supplies those jobs
// live and a row only holds per-item state, keyed job:<equipment_id>:<service_id>.
// This step re-keys every copied rule to its job id, keeps only the fields that
// differ from the profile as overrides, and proves the schedule reads the same
// afterwards.

// loadMaintenanceProfileSetTx reads the profiles as they stand inside tx
// (including ones an earlier step of this run imported), validating each the
// way the loader does.
func loadMaintenanceProfileSetTx(tx *sql.Tx) (maintenanceProfileSet, error) {
	rows, err := tx.Query(`SELECT id, body FROM equipment_profiles ORDER BY id`)
	if err != nil {
		return maintenanceProfileSet{}, fmt.Errorf("read profiles: %w", err)
	}
	defer rows.Close()
	var profiles []engineProfile
	var problems []engineProfileProblem
	for rows.Next() {
		var id, body string
		if err := rows.Scan(&id, &body); err != nil {
			return maintenanceProfileSet{}, fmt.Errorf("read profiles: %w", err)
		}
		p, err := parseProfileDocument([]byte(body))
		if err == nil && p.ID != id {
			err = fmt.Errorf("stored under id %q but the document says %q", id, p.ID)
		}
		if err != nil {
			problems = append(problems, engineProfileProblem{ID: id, Error: err.Error()})
			continue
		}
		profiles = append(profiles, p)
	}
	if err := rows.Err(); err != nil {
		return maintenanceProfileSet{}, err
	}
	return newMaintenanceProfileSet(profiles, problems), nil
}

// ruleReading is what a rule reads as: the tuple the before/after check compares.
type ruleReading struct {
	Description string
	Hours       *float64
	Months      *int
	Status      maintenanceStatus
}

func (r ruleReading) String() string {
	h, m := "none", "none"
	if r.Hours != nil {
		h = fmt.Sprintf("%g h", *r.Hours)
	}
	if r.Months != nil {
		m = fmt.Sprintf("%d mo", *r.Months)
	}
	return fmt.Sprintf("%q, %s, %s, %s", r.Description, h, m, r.Status)
}

func (r ruleReading) equal(o ruleReading) bool {
	return r.Description == o.Description && sameFloat(r.Hours, o.Hours) && sameInt(r.Months, o.Months) && r.Status == o.Status
}

// readingOf computes a rule's reading with the status engine against today and
// no live hours (both sides of the check use the same, so the comparison is
// about the rule, not the meter).
func readingOf(rule maintenanceRule, today time.Time) ruleReading {
	r := ruleReading{Description: rule.Description, Hours: rule.IntervalHours, Months: rule.IntervalMonths}
	if rule.NotApplicable {
		r.Status = maintenanceStatusNotApplicable
		return r
	}
	r.Status = computeMaintenanceRuleStatus(maintenanceRuleStatusInput{
		IntervalHours:  rule.IntervalHours,
		IntervalMonths: rule.IntervalMonths,
		DueSoonHours:   rule.DueSoonHours,
		DueSoonMonths:  rule.DueSoonMonths,
		FixedDueDate:   parseMaintenanceDate(rule.FixedDueDate),
		LastDoneAt:     parseMaintenanceDate(rule.LastDoneAt),
		LastDoneHours:  rule.LastDoneHours,
		Today:          today,
	}).Status
	return r
}

func convertMaintenanceRulesTx(tx *sql.Tx, opts conversionOptions, out io.Writer, apply bool) error {
	verb := "would convert"
	if apply {
		verb = "converted"
	}

	legacy, err := queryLegacyCopiedRules(tx)
	if err != nil {
		return err
	}
	profiles, err := loadMaintenanceProfileSetTx(tx)
	if err != nil {
		return err
	}
	today := civilDateUTC(time.Now())

	type plan struct {
		rule       maintenanceRule
		newID      string // "" when the rule is detached to a hand rule
		overridden map[string]bool
		itemName   string
		before     ruleReading
	}
	var plans []plan
	var unresolved, duplicates []string
	seen := map[string]string{}

	for _, r := range legacy {
		if r.EquipmentID == nil {
			unresolved = append(unresolved, fmt.Sprintf("%q (rule %s) belongs to no equipment item", r.Description, r.ID))
			if opts.DetachUnresolved {
				plans = append(plans, plan{rule: r, before: readingOf(r, today)})
			}
			continue
		}
		key := *r.EquipmentID + "\x00" + r.ProfileServiceID
		if other, dup := seen[key]; dup {
			duplicates = append(duplicates, fmt.Sprintf("service %q on item %s has two rules, %s and %s", r.ProfileServiceID, *r.EquipmentID, other, r.ID))
			continue
		}
		seen[key] = r.ID

		it, err := scheduleItemByID(tx, *r.EquipmentID)
		if err != nil {
			return fmt.Errorf("rule %s: %w", r.ID, err)
		}
		p := plan{rule: r, itemName: it.Name, before: readingOf(r, today)}
		profile, problem, ok := profiles.lookup(it.ProfileID)
		if it.ProfileID == "" {
			unresolved = append(unresolved, fmt.Sprintf("%q on %s: the item has no profile", r.Description, it.Name))
		} else if !ok {
			unresolved = append(unresolved, fmt.Sprintf("%q on %s: its profile %q is unusable (%s)", r.Description, it.Name, it.ProfileID, problem))
		}
		if it.ProfileID == "" || !ok {
			if opts.DetachUnresolved {
				plans = append(plans, p)
			}
			continue
		}
		var svc *engineProfileService
		for i := range profile.Service {
			if profile.Service[i].ID == r.ProfileServiceID {
				svc = &profile.Service[i]
			}
		}
		if svc == nil {
			// The profile no longer has this service: nothing to follow, so
			// the rule is the operator's own from here on.
			plans = append(plans, p)
			continue
		}
		p.newID = maintenanceJobID(*r.EquipmentID, r.ProfileServiceID)
		p.overridden = map[string]bool{}
		if r.Description != svc.Description {
			p.overridden[overrideDescription] = true
		}
		if !sameFloat(r.IntervalHours, svc.IntervalHours) {
			p.overridden[overrideIntervalHours] = true
		}
		if !sameInt(r.IntervalMonths, svc.IntervalMonths) {
			p.overridden[overrideIntervalMonths] = true
		}
		plans = append(plans, p)
	}

	if len(duplicates) > 0 {
		return fmt.Errorf("an item has two copied rules for one profile service; delete the extra one by hand and run again:\n  %s", strings.Join(duplicates, "\n  "))
	}
	if len(unresolved) > 0 && !opts.DetachUnresolved {
		return fmt.Errorf("copied rules whose profile cannot supply them:\n  %s\n"+
			"Fix the item's profile and run again, or run with --detach-unresolved to turn these into ordinary rules that keep their current intervals",
			strings.Join(unresolved, "\n  "))
	}

	if len(plans) == 0 {
		fmt.Fprintln(out, "convert-profile-rules: no copied maintenance rules - nothing to convert")
	}

	// Defer the rule_id foreign key to commit: re-keying a parent row while
	// log entries still point at the old id is only a transient violation.
	if _, err := tx.Exec(`PRAGMA defer_foreign_keys = ON`); err != nil {
		return fmt.Errorf("defer foreign keys: %w", err)
	}

	rekeyed := map[string]string{}
	for _, p := range plans {
		r := p.rule
		if p.newID == "" {
			if _, err := tx.Exec(`UPDATE maintenance_rules SET profile_service_id = '', overridden_fields = '', not_applicable = 0 WHERE id = ?`, r.ID); err != nil {
				return fmt.Errorf("detach rule %s: %w", r.ID, err)
			}
			fmt.Fprintf(out, "convert-profile-rules: %s %q as an ordinary rule (no longer follows a profile service)\n", verb, r.Description)
			continue
		}
		var hours, months any
		if p.overridden[overrideIntervalHours] && r.IntervalHours != nil {
			hours = *r.IntervalHours
		}
		if p.overridden[overrideIntervalMonths] && r.IntervalMonths != nil {
			months = *r.IntervalMonths
		}
		if _, err := tx.Exec(`UPDATE maintenance_rules SET id = ?, interval_hours = ?, interval_months = ?, overridden_fields = ?, not_applicable = 0 WHERE id = ?`,
			p.newID, hours, months, encodeOverriddenFields(p.overridden), r.ID); err != nil {
			return fmt.Errorf("re-key rule %s: %w", r.ID, err)
		}
		if _, err := tx.Exec(`UPDATE maintenance_log_entries SET rule_id = ? WHERE rule_id = ?`, p.newID, r.ID); err != nil {
			return fmt.Errorf("re-key log entries of rule %s: %w", r.ID, err)
		}
		rekeyed[r.ID] = p.newID
		detail := "follows the profile"
		if fields := encodeOverriddenFields(p.overridden); fields != "" {
			detail = "follows the profile except " + strings.ReplaceAll(fields, ",", ", ") + " (kept as this item's override)"
		}
		fmt.Fprintf(out, "convert-profile-rules: %s %q on %s: %s\n", verb, r.Description, p.itemName, detail)
	}
	if err := rewriteProposalRuleIDsTx(tx, rekeyed); err != nil {
		return err
	}

	if err := createMaintenanceJobIndexTx(tx); err != nil {
		return err
	}

	// Nothing may be left dangling.
	fkRows, err := tx.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("foreign key check: %w", err)
	}
	dangling := fkRows.Next()
	fkRows.Close()
	if dangling {
		return fmt.Errorf("foreign key check failed after re-keying; nothing was written")
	}

	// The schedule must read the same as it did.
	sched, err := buildMaintenanceSchedule(tx, profiles, maintenanceRuleFilter{IncludeStored: true})
	if err != nil {
		return fmt.Errorf("read the converted schedule: %w", err)
	}
	after := map[string]ruleReading{}
	for _, r := range sched.Rules {
		after[r.ID] = readingOf(r.maintenanceRule, today)
	}
	var diffs []string
	for _, p := range plans {
		id := p.rule.ID
		if p.newID != "" {
			id = p.newID
		}
		got, ok := after[id]
		switch {
		case !ok:
			diffs = append(diffs, fmt.Sprintf("%q: was %s, now missing from the schedule", p.rule.Description, p.before))
		case !got.equal(p.before):
			diffs = append(diffs, fmt.Sprintf("%q: was %s, now %s", p.rule.Description, p.before, got))
		}
	}
	sort.Strings(diffs)
	if len(diffs) > 0 {
		return fmt.Errorf("the converted schedule does not read the same as before, so nothing was written:\n  %s", strings.Join(diffs, "\n  "))
	}
	if len(plans) > 0 {
		fmt.Fprintf(out, "convert-profile-rules: checked %d rule(s): descriptions, intervals and status read the same after conversion\n", len(plans))
	}
	return nil
}

func queryLegacyCopiedRules(tx *sql.Tx) ([]maintenanceRule, error) {
	rows, err := tx.Query(`SELECT ` + maintenanceRuleColumns + ` FROM maintenance_rules
		WHERE profile_service_id != '' AND id NOT LIKE 'job:%' ORDER BY equipment_id, profile_service_id, created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("read copied rules: %w", err)
	}
	defer rows.Close()
	var out []maintenanceRule
	for rows.Next() {
		r, err := scanMaintenanceRule(rows)
		if err != nil {
			return nil, fmt.Errorf("read copied rules: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func createMaintenanceJobIndexTx(tx *sql.Tx) error {
	if _, err := tx.Exec(maintenanceJobIndexSQL); err != nil {
		return fmt.Errorf("create maintenance job index: %w", err)
	}
	return nil
}

// rewriteProposalRuleIDsTx carries the old rule ids inside Mate's saved
// proposals (the ops a pending card would apply, and the result of an applied
// one) over to the new ones. The table exists only once Mate has been set up.
func rewriteProposalRuleIDsTx(tx *sql.Tx, rekeyed map[string]string) error {
	if len(rekeyed) == 0 {
		return nil
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'assistant_message_proposals'`).Scan(&n); err != nil {
		return fmt.Errorf("check proposals table: %w", err)
	}
	if n == 0 {
		return nil
	}
	for oldID, newID := range rekeyed {
		if _, err := tx.Exec(`UPDATE assistant_message_proposals SET ops = replace(ops, ?, ?), result = replace(result, ?, ?)
			WHERE instr(ops, ?) > 0 OR instr(result, ?) > 0`, oldID, newID, oldID, newID, oldID, oldID); err != nil {
			return fmt.Errorf("rewrite proposals for rule %s: %w", oldID, err)
		}
	}
	return nil
}
