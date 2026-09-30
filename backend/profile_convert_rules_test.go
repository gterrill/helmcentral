package main

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// The maintenance half of convert-profile-rules (ADR 0148): copied rules
// become job rows keyed job:<equipment_id>:<service_id>, keeping only what
// differs from the profile as overrides, with the schedule proven unchanged.

type legacyFixtureEnv struct {
	path  string
	ds    *documentStore
	ps    *profileStore
	item  equipmentItem
	rules map[string]string // service id -> legacy rule id
}

func legacyFixture(t *testing.T) *legacyFixtureEnv {
	t.Helper()
	path := filepath.Join(t.TempDir(), "helmcentral.sqlite")
	ds, err := openDocumentStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ds.Close() })
	ps, err := newProfileStore(ds)
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range []string{scheduleProfileA, scheduleProfileB} {
		p := mustProfile(t, doc)
		if err := ps.inTx(func(tx *sql.Tx) error { return insertProfileTx(tx, p, profileBasedOn{}) }); err != nil {
			t.Fatal(err)
		}
	}
	item, err := ds.CreateEquipment(equipmentItem{Name: "Main engine", Category: "mechanical", ProfileID: "sched-a"})
	if err != nil {
		t.Fatal(err)
	}
	return &legacyFixtureEnv{path: path, ds: ds, ps: ps, item: item, rules: map[string]string{}}
}

// copied inserts a rule the way the old "Use profile schedule" did.
func (e *legacyFixtureEnv) copied(t *testing.T, id, service, description string, hours *float64, months *int, lastDone string) {
	t.Helper()
	if _, err := e.ds.db.Exec(`INSERT INTO maintenance_rules (id, equipment_id, description, interval_hours, interval_months, last_done_at, profile_service_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1, 1)`, id, e.item.ID, description, hours, months, lastDone, service); err != nil {
		t.Fatal(err)
	}
	e.rules[service] = id
}

func (e *legacyFixtureEnv) convert(apply bool, opts conversionOptions) (string, error) {
	var out bytes.Buffer
	err := runConversion(e.ps, convertProfileRulesSteps(filepath.Join(filepath.Dir(e.path), "no-legacy-files"), opts), &out, apply)
	return out.String(), err
}

func (e *legacyFixtureEnv) ruleIDs(t *testing.T) []string {
	t.Helper()
	rows, err := e.ds.db.Query(`SELECT id FROM maintenance_rules ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func (e *legacyFixtureEnv) logEntry(t *testing.T, ruleID string) {
	t.Helper()
	if _, err := e.ds.db.Exec(`INSERT INTO maintenance_log_entries (id, equipment_id, rule_id, performed_at, kind, created_at, updated_at) VALUES (?, ?, ?, '2026-01-01', 'maintenance', 1, 1)`,
		"log-"+ruleID, e.item.ID, ruleID); err != nil {
		t.Fatal(err)
	}
}

func (e *legacyFixtureEnv) logRuleID(t *testing.T, ruleID string) sql.NullString {
	t.Helper()
	var got sql.NullString
	if err := e.ds.db.QueryRow(`SELECT rule_id FROM maintenance_log_entries WHERE id = ?`, "log-"+ruleID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestConvertMaintenance_DryRunChangesNothing(t *testing.T) {
	e := legacyFixture(t)
	e.copied(t, "uuid-oil", "engine-oil", "Engine oil and filter", f64(250), intp(12), "2026-01-01")
	before := e.ruleIDs(t)

	out, err := e.convert(false, conversionOptions{})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !strings.Contains(out, "would convert") || !strings.Contains(out, "dry run") {
		t.Errorf("output: %s", out)
	}
	if got := e.ruleIDs(t); strings.Join(got, ",") != strings.Join(before, ",") {
		t.Fatalf("a dry run must not re-key anything: %v", got)
	}
	if err := checkForUnconvertedMaintenanceRules(e.ds.db); err == nil {
		t.Fatal("the legacy row must still be there after a dry run")
	}
}

func TestConvertMaintenance_ApplyRekeysKeepsOnlyDifferencesAndMovesHistory(t *testing.T) {
	e := legacyFixture(t)
	// Oil: interval hours edited by the operator (300 vs the profile's 250),
	// months and description as copied. Impeller: a slot the operator filled.
	e.copied(t, "uuid-oil", "engine-oil", "Engine oil and filter", f64(300), intp(12), "2026-01-01")
	e.copied(t, "uuid-imp", "impeller", "Raw water impeller", nil, intp(24), "")
	e.logEntry(t, "uuid-oil")
	if _, err := e.ds.db.Exec(`UPDATE maintenance_rules SET ack_reason = 'later', ack_at = 5, due_soon_hours = 20 WHERE id = 'uuid-oil'`); err != nil {
		t.Fatal(err)
	}

	out, err := e.convert(true, conversionOptions{})
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	if !strings.Contains(out, "converted") || !strings.Contains(out, "read the same after conversion") {
		t.Errorf("output: %s", out)
	}

	oil := maintenanceJobID(e.item.ID, "engine-oil")
	imp := maintenanceJobID(e.item.ID, "impeller")
	if got := strings.Join(e.ruleIDs(t), ","); !strings.Contains(got, oil) || !strings.Contains(got, imp) || strings.Contains(got, "uuid-") {
		t.Fatalf("rows should be re-keyed, got %s", got)
	}
	if got := e.logRuleID(t, "uuid-oil"); !got.Valid || got.String != oil {
		t.Fatalf("the log entry must follow its rule to the new id, got %+v", got)
	}

	sched, err := buildMaintenanceSchedule(e.ds.db, currentProfileSetOf(t, e), maintenanceRuleFilter{EquipmentID: e.item.ID, IncludeStored: true})
	if err != nil {
		t.Fatal(err)
	}
	o := findScheduleRule(sched.Rules, oil)
	if o == nil || strings.Join(o.OverriddenFields, ",") != "interval_hours" || *o.IntervalHours != 300 || *o.IntervalMonths != 12 {
		t.Fatalf("only the edited hours interval may become an override, got %+v", o)
	}
	if !o.Acknowledged || o.DueSoonHours == nil || *o.DueSoonHours != 20 {
		t.Errorf("state must carry over: %+v", o)
	}
	i := findScheduleRule(sched.Rules, imp)
	if i == nil || strings.Join(i.OverriddenFields, ",") != "interval_months" || *i.IntervalMonths != 24 || i.IntervalHours != nil {
		t.Fatalf("the filled-in slot becomes a months override, got %+v", i)
	}
	// Non-overridden interval columns are NULL in the row.
	var months sql.NullInt64
	if err := e.ds.db.QueryRow(`SELECT interval_months FROM maintenance_rules WHERE id = ?`, oil).Scan(&months); err != nil || months.Valid {
		t.Errorf("a non-overridden column must be NULL, got %v %v", months, err)
	}

	if err := checkForUnconvertedMaintenanceRules(e.ds.db); err != nil {
		t.Errorf("nothing should be left to convert: %v", err)
	}
	if _, err := e.ds.db.Exec(`INSERT INTO maintenance_rules (id, equipment_id, description, profile_service_id, created_at, updated_at) VALUES ('dup', ?, 'x', 'engine-oil', 0, 0)`, e.item.ID); err == nil {
		t.Error("the unique job index must exist after conversion")
	}

	// Running again finds nothing.
	if out, err := e.convert(true, conversionOptions{}); err != nil || !strings.Contains(out, "nothing to convert") {
		t.Fatalf("second run: %v %s", err, out)
	}
}

func currentProfileSetOf(t *testing.T, e *legacyFixtureEnv) maintenanceProfileSet {
	t.Helper()
	var set maintenanceProfileSet
	err := e.ps.inTx(func(tx *sql.Tx) error {
		var err error
		set, err = loadMaintenanceProfileSetTx(tx)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func TestConvertMaintenance_AServiceTheProfileNoLongerHasBecomesAHandRule(t *testing.T) {
	e := legacyFixture(t)
	e.copied(t, "uuid-old", "retired-service", "Retired service", f64(100), nil, "2026-01-01")

	if out, err := e.convert(true, conversionOptions{}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var svc string
	var hours sql.NullFloat64
	if err := e.ds.db.QueryRow(`SELECT profile_service_id, interval_hours FROM maintenance_rules WHERE id = 'uuid-old'`).Scan(&svc, &hours); err != nil {
		t.Fatalf("the rule keeps its UUID id: %v", err)
	}
	if svc != "" || !hours.Valid || hours.Float64 != 100 {
		t.Errorf("expected a hand rule with its interval kept, got %q %v", svc, hours)
	}
	sched := scheduleFromDB(t, e)
	if r := findScheduleRule(sched.Rules, "uuid-old"); r == nil || r.Source != "item" {
		t.Errorf("it should list as the item's own rule: %+v", sched.Rules)
	}
}

func scheduleFromDB(t *testing.T, e *legacyFixtureEnv) maintenanceSchedule {
	t.Helper()
	sched, err := buildMaintenanceSchedule(e.ds.db, currentProfileSetOf(t, e), maintenanceRuleFilter{EquipmentID: e.item.ID, IncludeStored: true})
	if err != nil {
		t.Fatal(err)
	}
	return sched
}

func TestConvertMaintenance_UnresolvedProfileAbortsUnlessDetaching(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, e *legacyFixtureEnv){
		"no profile":      func(t *testing.T, e *legacyFixtureEnv) { mustExec(t, e.ds.db, `UPDATE equipment SET profile_id = '' WHERE id = ?`, e.item.ID) },
		"missing profile": func(t *testing.T, e *legacyFixtureEnv) { mustExec(t, e.ds.db, `UPDATE equipment SET profile_id = 'gone' WHERE id = ?`, e.item.ID) },
		"invalid profile": func(t *testing.T, e *legacyFixtureEnv) {
			mustExec(t, e.ds.db, `INSERT INTO equipment_profiles (id, kind, body, created_at, updated_at) VALUES ('broken', 'engine', '{"id":"broken"', 0, 0)`)
			mustExec(t, e.ds.db, `UPDATE equipment SET profile_id = 'broken' WHERE id = ?`, e.item.ID)
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := legacyFixture(t)
			e.copied(t, "uuid-oil", "engine-oil", "Engine oil and filter", f64(250), intp(12), "2026-01-01")
			setup(t, e)

			out, err := e.convert(true, conversionOptions{})
			if err == nil || !strings.Contains(err.Error(), "--detach-unresolved") || !strings.Contains(err.Error(), "Engine oil and filter") {
				t.Fatalf("expected an abort naming the rule and the flag, got %v\n%s", err, out)
			}
			if got := e.ruleIDs(t); len(got) != 1 || got[0] != "uuid-oil" {
				t.Fatalf("an aborted run must change nothing, got %v", got)
			}

			if out, err := e.convert(true, conversionOptions{DetachUnresolved: true}); err != nil {
				t.Fatalf("detach run: %v\n%s", err, out)
			}
			var svc string
			var hours sql.NullFloat64
			if err := e.ds.db.QueryRow(`SELECT profile_service_id, interval_hours FROM maintenance_rules WHERE id = 'uuid-oil'`).Scan(&svc, &hours); err != nil {
				t.Fatal(err)
			}
			if svc != "" || !hours.Valid || hours.Float64 != 250 {
				t.Errorf("detached rule should be a hand rule keeping its intervals, got %q %v", svc, hours)
			}
		})
	}
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func TestConvertMaintenance_DuplicateCopiesAbortEvenWhenDetaching(t *testing.T) {
	e := legacyFixture(t)
	e.copied(t, "uuid-a", "engine-oil", "Engine oil and filter", f64(250), intp(12), "")
	e.copied(t, "uuid-b", "engine-oil", "Engine oil and filter", f64(250), intp(12), "")
	for _, opts := range []conversionOptions{{}, {DetachUnresolved: true}} {
		_, err := e.convert(true, opts)
		if err == nil || !strings.Contains(err.Error(), "two copied rules") || !strings.Contains(err.Error(), "uuid-a") {
			t.Fatalf("expected a duplicate abort, got %v", err)
		}
	}
	if got := e.ruleIDs(t); len(got) != 2 {
		t.Fatalf("nothing may change, got %v", got)
	}
}

func TestConvertMaintenance_AScheduleThatWouldReadDifferentlyRollsBack(t *testing.T) {
	e := legacyFixture(t)
	e.copied(t, "uuid-oil", "engine-oil", "Engine oil and filter", f64(250), intp(12), "2026-01-01")
	// A row in a state the job model has no place for (flagged not applicable
	// without being an override): converting it would change what it reads as.
	mustExec(t, e.ds.db, `UPDATE maintenance_rules SET not_applicable = 1 WHERE id = 'uuid-oil'`)
	e.logEntry(t, "uuid-oil")

	_, err := e.convert(true, conversionOptions{})
	if err == nil || !strings.Contains(err.Error(), "does not read the same") || !strings.Contains(err.Error(), "Engine oil and filter") {
		t.Fatalf("expected the diff, got %v", err)
	}
	if got := e.ruleIDs(t); len(got) != 1 || got[0] != "uuid-oil" {
		t.Fatalf("everything must roll back, got %v", got)
	}
	if got := e.logRuleID(t, "uuid-oil"); got.String != "uuid-oil" {
		t.Fatalf("log entry must be untouched, got %+v", got)
	}
}

func TestConvertMaintenance_RewritesRuleIDsInSavedProposals(t *testing.T) {
	e := legacyFixture(t)
	e.copied(t, "uuid-oil", "engine-oil", "Engine oil and filter", f64(250), intp(12), "")
	mustExec(t, e.ds.db, `CREATE TABLE assistant_message_proposals (id TEXT PRIMARY KEY, ops TEXT NOT NULL, status TEXT NOT NULL, result TEXT NOT NULL DEFAULT '')`)
	mustExec(t, e.ds.db, `INSERT INTO assistant_message_proposals (id, ops, status, result) VALUES ('p1', '[{"op":"acknowledge","rule_id":"uuid-oil"}]', 'pending', ''), ('p2', '[]', 'applied', '{"ops":[{"rule_ids":["uuid-oil"]}]}')`)

	if out, err := e.convert(true, conversionOptions{}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	oil := maintenanceJobID(e.item.ID, "engine-oil")
	var ops, result string
	mustScan := func(id string) {
		if err := e.ds.db.QueryRow(`SELECT ops, result FROM assistant_message_proposals WHERE id = ?`, id).Scan(&ops, &result); err != nil {
			t.Fatal(err)
		}
	}
	mustScan("p1")
	if !strings.Contains(ops, oil) || strings.Contains(ops, "uuid-oil") {
		t.Errorf("pending proposal ops: %s", ops)
	}
	mustScan("p2")
	if !strings.Contains(result, oil) {
		t.Errorf("applied proposal result: %s", result)
	}
}

func TestStartupRefusesUnconvertedMaintenanceRules(t *testing.T) {
	e := legacyFixture(t)
	e.copied(t, "uuid-oil", "engine-oil", "Engine oil and filter", f64(250), intp(12), "")
	e.copied(t, "uuid-oil2", "engine-oil", "Engine oil and filter", f64(250), intp(12), "") // would break the unique index
	e.ds.Close()

	// The store the server starts with refuses, naming the command, and does so
	// before the unique index could fail with a less helpful message.
	_, err := newDocumentStore(e.path)
	if err == nil || !strings.Contains(err.Error(), "convert-profile-rules --apply") {
		t.Fatalf("expected a refusal naming the command, got %v", err)
	}
	// The conversion's own opener does not.
	ds, err := openDocumentStore(e.path)
	if err != nil {
		t.Fatalf("the conversion must still be able to open the database: %v", err)
	}
	ds.Close()
}

func TestStartupAcceptsJobRowsAndHandRules(t *testing.T) {
	e := legacyFixture(t)
	mustExec(t, e.ds.db, `INSERT INTO maintenance_rules (id, equipment_id, description, profile_service_id, created_at, updated_at) VALUES (?, ?, 'x', 'engine-oil', 0, 0)`, maintenanceJobID(e.item.ID, "engine-oil"), e.item.ID)
	mustExec(t, e.ds.db, `INSERT INTO maintenance_rules (id, equipment_id, description, interval_months, created_at, updated_at) VALUES ('hand', ?, 'y', 6, 0, 0)`, e.item.ID)
	e.ds.Close()
	ds, err := newDocumentStore(e.path)
	if err != nil {
		t.Fatalf("job rows and hand rules are fine: %v", err)
	}
	ds.Close()
}
