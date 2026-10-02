package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// This file pins the maintenance schedule resolver (ADR 0148): an item's
// effective schedule is its profile's service jobs, read live, plus its own
// hand-made rules, and buildMaintenanceSchedule is the only place effective
// values come from.

const scheduleProfileA = `{
  "schema_version": 1, "kind": "engine", "id": "sched-a", "name": "Engine A", "manufacturer": "Acme",
  "service": [
    {"id": "engine-oil", "description": "Engine oil and filter", "interval_hours": 250, "interval_months": 12, "first_at_hours": 50},
    {"id": "impeller", "description": "Raw water impeller"}
  ]
}`

const scheduleProfileB = `{
  "schema_version": 1, "kind": "engine", "id": "sched-b", "name": "Engine B", "manufacturer": "Acme",
  "service": [
    {"id": "engine-oil", "description": "Engine oil and filter (B)", "interval_hours": 100, "interval_months": 6},
    {"id": "belt", "description": "Drive belt", "interval_hours": 500}
  ]
}`

// scheduleFixture is a document store with profiles A and B and one item on A.
func scheduleFixture(t *testing.T) (*documentStore, equipmentItem) {
	t.Helper()
	store := withTestDocumentStore(t)
	setupEngineProfiles(t, map[string]string{"a.json": scheduleProfileA, "b.json": scheduleProfileB})
	item, err := store.CreateEquipment(equipmentItem{Name: "Main engine", Category: "mechanical", ProfileID: "sched-a"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	return store, item
}

func runScheduleTx(t *testing.T, store *documentStore, fn func(tx *sql.Tx, now time.Time) error) error {
	t.Helper()
	return store.RunTx(fn)
}

func mustScheduleTx(t *testing.T, store *documentStore, fn func(tx *sql.Tx, now time.Time) error) {
	t.Helper()
	if err := runScheduleTx(t, store, fn); err != nil {
		t.Fatalf("maintenance tx: %v", err)
	}
}

func scheduleFor(t *testing.T, store *documentStore, filter maintenanceRuleFilter) maintenanceSchedule {
	t.Helper()
	sched, err := buildMaintenanceSchedule(store.db, currentMaintenanceProfiles(), filter)
	if err != nil {
		t.Fatalf("buildMaintenanceSchedule: %v", err)
	}
	return sched
}

func findScheduleRule(rules []effectiveMaintenanceRule, id string) *effectiveMaintenanceRule {
	for i := range rules {
		if rules[i].ID == id {
			return &rules[i]
		}
	}
	return nil
}

func countRuleRows(t *testing.T, store *documentStore) int {
	t.Helper()
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM maintenance_rules`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func f64(v float64) *float64 { return &v }
func intp(v int) *int        { return &v }

// replaceProfile rewrites a stored profile body and reloads the cache.
func replaceProfile(t *testing.T, store *profileStore, doc string) {
	t.Helper()
	p := mustProfile(t, doc)
	if err := store.inTx(func(tx *sql.Tx) error { return updateProfileTx(tx, p) }); err != nil {
		t.Fatalf("updateProfileTx: %v", err)
	}
	loadEngineProfiles()
}

func TestSchedule_UntouchedJobsListFromTheProfileWithoutRows(t *testing.T) {
	store, item := scheduleFixture(t)

	sched := scheduleFor(t, store, maintenanceRuleFilter{EquipmentID: item.ID, IncludeStored: true})
	if len(sched.Rules) != 2 || len(sched.Errors) != 0 || len(sched.Removed) != 0 {
		t.Fatalf("expected 2 rules and nothing else, got %+v", sched)
	}
	oil := findScheduleRule(sched.Rules, maintenanceJobID(item.ID, "engine-oil"))
	if oil == nil {
		t.Fatalf("missing engine-oil job in %+v", sched.Rules)
	}
	if oil.Source != "profile" || oil.ProfileID != "sched-a" || oil.Description != "Engine oil and filter" {
		t.Errorf("unexpected job: %+v", oil)
	}
	if oil.IntervalHours == nil || *oil.IntervalHours != 250 || oil.IntervalMonths == nil || *oil.IntervalMonths != 12 {
		t.Errorf("expected 250 h / 12 mo, got %+v / %+v", oil.IntervalHours, oil.IntervalMonths)
	}
	if oil.ProfileValues == nil || oil.ProfileValues.Description != "Engine oil and filter" {
		t.Errorf("expected profile_values, got %+v", oil.ProfileValues)
	}
	if oil.FirstAtHours == nil || *oil.FirstAtHours != 50 {
		t.Errorf("first_at_hours must pass through, got %+v", oil.FirstAtHours)
	}
	if len(oil.OverriddenFields) != 0 {
		t.Errorf("nothing is overridden, got %v", oil.OverriddenFields)
	}
	if n := countRuleRows(t, store); n != 0 {
		t.Fatalf("reading a schedule must not create rows, found %d", n)
	}
}

func TestSchedule_ProfileEditsShowImmediatelyAndHandRulesStayUntouched(t *testing.T) {
	store, item := scheduleFixture(t)
	months := 6
	hand, err := store.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &item.ID, Description: "Check anodes", IntervalMonths: &months})
	if err != nil {
		t.Fatal(err)
	}
	id := maintenanceJobID(item.ID, "engine-oil")
	mustScheduleTx(t, store, func(tx *sql.Tx, now time.Time) error {
		_, err := cmdAcknowledgeMaintenanceRule(tx, now, id, "later")
		return err
	})

	replaceProfile(t, globalProfileStore, strings.Replace(scheduleProfileA, `"interval_hours": 250`, `"interval_hours": 300`, 1))

	sched := scheduleFor(t, store, maintenanceRuleFilter{EquipmentID: item.ID, IncludeStored: true})
	oil := findScheduleRule(sched.Rules, id)
	if oil == nil || oil.IntervalHours == nil || *oil.IntervalHours != 300 {
		t.Fatalf("the profile edit must show at once, got %+v", oil)
	}
	if !oil.Acknowledged {
		t.Errorf("item state must survive a profile edit")
	}
	h := findScheduleRule(sched.Rules, hand.ID)
	if h == nil || h.Source != "item" || h.ProfileValues != nil {
		t.Errorf("hand rule should list as an item rule, got %+v", h)
	}
}

func TestSchedule_OverrideReplacesProfileValueAndSticksWhenProfileChanges(t *testing.T) {
	store, item := scheduleFixture(t)
	id := maintenanceJobID(item.ID, "engine-oil")
	mustScheduleTx(t, store, func(tx *sql.Tx, now time.Time) error {
		_, err := cmdSetMaintenanceRuleOverrides(tx, now, id, maintenanceOverridesInput{IntervalHours: &maintenanceIntervalHoursOverride{Value: f64(500)}})
		return err
	})
	replaceProfile(t, globalProfileStore, strings.Replace(scheduleProfileA, `"interval_hours": 250`, `"interval_hours": 300`, 1))

	sched := scheduleFor(t, store, maintenanceRuleFilter{EquipmentID: item.ID, IncludeStored: true})
	oil := findScheduleRule(sched.Rules, id)
	if oil == nil {
		t.Fatalf("job missing: %+v", sched.Rules)
	}
	if oil.IntervalHours == nil || *oil.IntervalHours != 500 {
		t.Errorf("override should win, got %+v", oil.IntervalHours)
	}
	if oil.IntervalMonths == nil || *oil.IntervalMonths != 12 {
		t.Errorf("non-overridden months should stay live, got %+v", oil.IntervalMonths)
	}
	if len(oil.OverriddenFields) != 1 || oil.OverriddenFields[0] != "interval_hours" {
		t.Errorf("overridden_fields = %v", oil.OverriddenFields)
	}
	if oil.ProfileValues.IntervalHours == nil || *oil.ProfileValues.IntervalHours != 300 {
		t.Errorf("profile_values must show the live profile figure, got %+v", oil.ProfileValues.IntervalHours)
	}
}

func TestSchedule_OverriddenNullMeansExplicitlyNone(t *testing.T) {
	store, item := scheduleFixture(t)
	id := maintenanceJobID(item.ID, "engine-oil")
	mustScheduleTx(t, store, func(tx *sql.Tx, now time.Time) error {
		_, err := cmdSetMaintenanceRuleOverrides(tx, now, id, maintenanceOverridesInput{IntervalMonths: &maintenanceIntervalMonthsOverride{Value: nil}})
		return err
	})
	oil := findScheduleRule(scheduleFor(t, store, maintenanceRuleFilter{EquipmentID: item.ID, IncludeStored: true}).Rules, id)
	if oil.IntervalMonths != nil {
		t.Fatalf("overridden to none must read as none, got %d", *oil.IntervalMonths)
	}
	if oil.IntervalHours == nil || *oil.IntervalHours != 250 {
		t.Errorf("hours should still follow the profile, got %+v", oil.IntervalHours)
	}
}

func TestSchedule_ResettingAnOverrideFollowsTheProfileAgain(t *testing.T) {
	store, item := scheduleFixture(t)
	id := maintenanceJobID(item.ID, "engine-oil")
	desc := "Oil, the boat's way"
	mustScheduleTx(t, store, func(tx *sql.Tx, now time.Time) error {
		_, err := cmdSetMaintenanceRuleOverrides(tx, now, id, maintenanceOverridesInput{Description: &desc, IntervalHours: &maintenanceIntervalHoursOverride{Value: f64(400)}})
		return err
	})
	mustScheduleTx(t, store, func(tx *sql.Tx, now time.Time) error {
		_, err := cmdResetMaintenanceRuleOverride(tx, now, id, "interval_hours")
		return err
	})
	oil := findScheduleRule(scheduleFor(t, store, maintenanceRuleFilter{EquipmentID: item.ID, IncludeStored: true}).Rules, id)
	if oil.IntervalHours == nil || *oil.IntervalHours != 250 {
		t.Errorf("reset hours should be the profile's 250, got %+v", oil.IntervalHours)
	}
	if oil.Description != desc || len(oil.OverriddenFields) != 1 || oil.OverriddenFields[0] != "description" {
		t.Errorf("description override should remain alone, got %q %v", oil.Description, oil.OverriddenFields)
	}
	// No interval value may be left in the row for a field that is not overridden.
	var hours sql.NullFloat64
	if err := store.db.QueryRow(`SELECT interval_hours FROM maintenance_rules WHERE id = ?`, id).Scan(&hours); err != nil {
		t.Fatal(err)
	}
	if hours.Valid {
		t.Errorf("a non-overridden interval column must be NULL, got %v", hours.Float64)
	}
}

func TestSchedule_MissingProfileReportsAnErrorAndRendersNoSnapshot(t *testing.T) {
	store, item := scheduleFixture(t)
	id := maintenanceJobID(item.ID, "engine-oil")
	mustScheduleTx(t, store, func(tx *sql.Tx, now time.Time) error {
		_, err := cmdAcknowledgeMaintenanceRule(tx, now, id, "later")
		return err
	})
	months := 3
	hand, err := store.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &item.ID, Description: "Check anodes", IntervalMonths: &months})
	if err != nil {
		t.Fatal(err)
	}
	// The profile vanishes behind the item's back (a corrupt restore, say).
	if _, err := store.db.Exec(`DELETE FROM equipment_profiles WHERE id = 'sched-a'`); err != nil {
		t.Fatal(err)
	}
	loadEngineProfiles()

	sched := scheduleFor(t, store, maintenanceRuleFilter{EquipmentID: item.ID, IncludeStored: true})
	if len(sched.Errors) != 1 {
		t.Fatalf("expected one schedule error, got %+v", sched.Errors)
	}
	e := sched.Errors[0]
	if e.EquipmentID != item.ID || e.EquipmentName != "Main engine" || e.ProfileID != "sched-a" || e.Error != "profile not found" {
		t.Errorf("unexpected error entry %+v", e)
	}
	if len(sched.Rules) != 1 || sched.Rules[0].ID != hand.ID {
		t.Fatalf("only the hand rule may list, got %+v", sched.Rules)
	}
	if len(sched.Removed) != 0 {
		t.Errorf("a job must not be reported removed just because its profile is unavailable: %+v", sched.Removed)
	}
	body, _ := json.Marshal(sched)
	if strings.Contains(string(body), "Engine oil and filter") {
		t.Fatalf("no snapshot of the job may appear anywhere in the output: %s", body)
	}
}

func TestSchedule_InvalidProfileReportsTheProblemText(t *testing.T) {
	store, item := scheduleFixture(t)
	seedRawProfileRow(t, globalProfileStore, "sched-broken", `{"schema_version":1,"kind":"engine","id":"sched-broken","name":"Broken"`)
	loadEngineProfiles()
	if _, err := store.db.Exec(`UPDATE equipment SET profile_id = 'sched-broken' WHERE id = ?`, item.ID); err != nil {
		t.Fatal(err)
	}
	sched := scheduleFor(t, store, maintenanceRuleFilter{EquipmentID: item.ID, IncludeStored: true})
	if len(sched.Rules) != 0 || len(sched.Errors) != 1 {
		t.Fatalf("expected no rules and one error, got %+v", sched)
	}
	_, problems := engineProfiles()
	var want string
	for _, p := range problems {
		if p.ID == "sched-broken" {
			want = p.Error
		}
	}
	if want == "" || sched.Errors[0].Error != want {
		t.Errorf("error text should be the problem's %q, got %q", want, sched.Errors[0].Error)
	}
}

func TestSchedule_JobRemovedFromTheProfileLeavesTheScheduleButIsReturnedForOneItem(t *testing.T) {
	store, item := scheduleFixture(t)
	impeller := maintenanceJobID(item.ID, "impeller")
	mustScheduleTx(t, store, func(tx *sql.Tx, now time.Time) error {
		_, err := cmdAcknowledgeMaintenanceRule(tx, now, impeller, "later")
		return err
	})
	replaceProfile(t, globalProfileStore, `{"schema_version":1,"kind":"engine","id":"sched-a","name":"Engine A","manufacturer":"Acme",
	  "service":[{"id":"engine-oil","description":"Engine oil and filter","interval_hours":250,"interval_months":12}]}`)

	one := scheduleFor(t, store, maintenanceRuleFilter{EquipmentID: item.ID, IncludeStored: true})
	if findScheduleRule(one.Rules, impeller) != nil {
		t.Fatalf("a removed job must not be in the schedule: %+v", one.Rules)
	}
	if len(one.Removed) != 1 || one.Removed[0].ID != impeller || !one.Removed[0].RemovedFromProfile {
		t.Fatalf("expected the removed group to carry the impeller, got %+v", one.Removed)
	}
	if one.Removed[0].Description != "Raw water impeller" {
		t.Errorf("removed job is labelled from its snapshot, got %q", one.Removed[0].Description)
	}

	all := scheduleFor(t, store, maintenanceRuleFilter{IncludeStored: true})
	if len(all.Removed) != 0 || findScheduleRule(all.Rules, impeller) != nil {
		t.Errorf("an all-items list must not carry removed jobs: %+v", all)
	}
}

func TestSchedule_SwitchingProfileKeepsSharedServicesAndParksTheRest(t *testing.T) {
	store, item := scheduleFixture(t)
	oil := maintenanceJobID(item.ID, "engine-oil")
	impeller := maintenanceJobID(item.ID, "impeller")
	mustScheduleTx(t, store, func(tx *sql.Tx, now time.Time) error {
		if _, err := cmdAcknowledgeMaintenanceRule(tx, now, oil, "later"); err != nil {
			return err
		}
		_, err := cmdAcknowledgeMaintenanceRule(tx, now, impeller, "later")
		return err
	})
	if _, err := store.db.Exec(`UPDATE equipment SET profile_id = 'sched-b' WHERE id = ?`, item.ID); err != nil {
		t.Fatal(err)
	}

	sched := scheduleFor(t, store, maintenanceRuleFilter{EquipmentID: item.ID, IncludeStored: true})
	kept := findScheduleRule(sched.Rules, oil)
	if kept == nil || !kept.Acknowledged || kept.ProfileID != "sched-b" || kept.Description != "Engine oil and filter (B)" {
		t.Errorf("shared service keeps its state under the new profile's values, got %+v", kept)
	}
	belt := findScheduleRule(sched.Rules, maintenanceJobID(item.ID, "belt"))
	if belt == nil || belt.LastDoneAt != "" || belt.Acknowledged {
		t.Errorf("a new service arrives never recorded, got %+v", belt)
	}
	if len(sched.Removed) != 1 || sched.Removed[0].ID != impeller {
		t.Errorf("the old profile's other service should be parked, got %+v", sched.Removed)
	}

	// Clearing the profile parks every job and invents none.
	if _, err := store.db.Exec(`UPDATE equipment SET profile_id = '' WHERE id = ?`, item.ID); err != nil {
		t.Fatal(err)
	}
	cleared := scheduleFor(t, store, maintenanceRuleFilter{EquipmentID: item.ID, IncludeStored: true})
	if len(cleared.Rules) != 0 || len(cleared.Removed) != 2 || len(cleared.Errors) != 0 {
		t.Errorf("expected nothing listed and two parked rows, got %+v", cleared)
	}
}

func TestSchedule_WritesCreateTheJobRowLazily(t *testing.T) {
	store, item := scheduleFixture(t)
	oil := maintenanceJobID(item.ID, "engine-oil")

	mustScheduleTx(t, store, func(tx *sql.Tx, now time.Time) error {
		_, err := cmdAcknowledgeMaintenanceRule(tx, now, oil, "after the haul-out")
		return err
	})
	if n := countRuleRows(t, store); n != 1 {
		t.Fatalf("expected exactly one row for the touched job, got %d", n)
	}
	var desc, svc string
	var hours sql.NullFloat64
	if err := store.db.QueryRow(`SELECT description, profile_service_id, interval_hours FROM maintenance_rules WHERE id = ?`, oil).Scan(&desc, &svc, &hours); err != nil {
		t.Fatal(err)
	}
	if desc != "Engine oil and filter" || svc != "engine-oil" || hours.Valid {
		t.Errorf("row should hold a description snapshot and no interval, got %q %q %v", desc, svc, hours)
	}

	// A completion on an untouched job creates its row and uses the PROFILE's
	// hours interval: a reading is required though the row holds no interval.
	impeller := maintenanceJobID(item.ID, "impeller")
	err := runScheduleTx(t, store, func(tx *sql.Tx, now time.Time) error {
		_, _, err := cmdCompleteMaintenanceRule(tx, now, oil, maintenanceLogEntryRequest{PerformedAt: "2026-06-01"})
		return err
	})
	var verr *inventoryValidationError
	if !errors.As(err, &verr) || verr.Field != "hours" {
		t.Fatalf("completion must demand hours from the profile's interval, got %v", err)
	}
	if n := countRuleRows(t, store); n != 1 {
		t.Errorf("a refused completion must not leave extra rows, got %d", n)
	}
	mustScheduleTx(t, store, func(tx *sql.Tx, now time.Time) error {
		_, _, err := cmdCompleteMaintenanceRule(tx, now, impeller, maintenanceLogEntryRequest{PerformedAt: "2026-06-01"})
		return err
	})
	entries, err := store.ListMaintenanceLogEntries(maintenanceLogFilter{EquipmentID: item.ID})
	if err != nil || len(entries) != 1 || entries[0].RuleID == nil || *entries[0].RuleID != impeller {
		t.Fatalf("expected a log entry linked to the job id, got %+v %v", entries, err)
	}
}

func TestSchedule_WritesToAJobAreRefusedWhileItsProfileIsUnavailable(t *testing.T) {
	store, item := scheduleFixture(t)
	oil := maintenanceJobID(item.ID, "engine-oil")
	if _, err := store.db.Exec(`UPDATE equipment SET profile_id = 'gone' WHERE id = ?`, item.ID); err != nil {
		t.Fatal(err)
	}
	err := runScheduleTx(t, store, func(tx *sql.Tx, now time.Time) error {
		_, err := cmdAcknowledgeMaintenanceRule(tx, now, oil, "later")
		return err
	})
	var cerr *maintenanceCommandError
	if !errors.As(err, &cerr) || cerr.Status != http.StatusConflict {
		t.Fatalf("expected a 409 command error, got %v", err)
	}
	if n := countRuleRows(t, store); n != 0 {
		t.Errorf("a refused write must leave no row, got %d", n)
	}
}

func TestSchedule_AJobTheProfileDoesNotHaveIsNotFound(t *testing.T) {
	store, item := scheduleFixture(t)
	err := runScheduleTx(t, store, func(tx *sql.Tx, now time.Time) error {
		_, err := cmdAcknowledgeMaintenanceRule(tx, now, maintenanceJobID(item.ID, "no-such-service"), "later")
		return err
	})
	if !errors.Is(err, errMaintenanceRuleNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestSchedule_OnlyOneRowPerItemAndService(t *testing.T) {
	store, item := scheduleFixture(t)
	insert := func(id string) error {
		_, err := store.db.Exec(`INSERT INTO maintenance_rules (id, equipment_id, description, profile_service_id, created_at, updated_at) VALUES (?, ?, 'x', 'engine-oil', 0, 0)`, id, item.ID)
		return err
	}
	if err := insert(maintenanceJobID(item.ID, "engine-oil")); err != nil {
		t.Fatal(err)
	}
	if err := insert("another-id"); err == nil {
		t.Fatal("expected the unique (equipment_id, profile_service_id) index to refuse a second row")
	}
}

func TestSchedule_ProfileChangePreview(t *testing.T) {
	store, item := scheduleFixture(t)
	mustScheduleTx(t, store, func(tx *sql.Tx, now time.Time) error {
		_, err := cmdAcknowledgeMaintenanceRule(tx, now, maintenanceJobID(item.ID, "impeller"), "later")
		return err
	})
	prev, err := previewMaintenanceProfileChange(store.db, currentMaintenanceProfiles(), item.ID, "sched-b")
	if err != nil {
		t.Fatal(err)
	}
	ids := func(entries []maintenanceProfileChangeEntry) string {
		var out []string
		for _, e := range entries {
			out = append(out, e.ServiceID)
		}
		return strings.Join(out, ",")
	}
	if ids(prev.Kept) != "engine-oil" || ids(prev.Leaving) != "impeller" || ids(prev.New) != "belt" {
		t.Errorf("kept=%s leaving=%s new=%s", ids(prev.Kept), ids(prev.Leaving), ids(prev.New))
	}
	if prev.Leaving[0].Description != "Raw water impeller" {
		t.Errorf("entries carry descriptions, got %+v", prev.Leaving[0])
	}

	cleared, err := previewMaintenanceProfileChange(store.db, currentMaintenanceProfiles(), item.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared.Kept) != 0 || len(cleared.New) != 0 || len(cleared.Leaving) != 2 {
		t.Errorf("clearing the profile: %+v", cleared)
	}
	if _, err := previewMaintenanceProfileChange(store.db, currentMaintenanceProfiles(), item.ID, "nope"); err == nil {
		t.Error("an unknown target profile must be refused")
	}
}

func TestSchedule_ExplicitProfileSetIsHonoured(t *testing.T) {
	store, item := scheduleFixture(t)
	a := mustProfile(t, scheduleProfileA)
	set := newMaintenanceProfileSet([]engineProfile{a}, nil)
	sched, err := buildMaintenanceSchedule(store.db, set, maintenanceRuleFilter{EquipmentID: item.ID, IncludeStored: true})
	if err != nil || len(sched.Rules) != 2 {
		t.Fatalf("got %+v %v", sched, err)
	}
	empty := newMaintenanceProfileSet(nil, nil)
	sched, err = buildMaintenanceSchedule(store.db, empty, maintenanceRuleFilter{EquipmentID: item.ID, IncludeStored: true})
	if err != nil || len(sched.Rules) != 0 || len(sched.Errors) != 1 {
		t.Fatalf("an empty set must report the profile missing, got %+v %v", sched, err)
	}
}

func TestSchedule_StoredItemsAndSystemFilterStillApply(t *testing.T) {
	store, item := scheduleFixture(t)
	if _, err := store.db.Exec(`UPDATE equipment SET status = 'stored' WHERE id = ?`, item.ID); err != nil {
		t.Fatal(err)
	}
	if got := scheduleFor(t, store, maintenanceRuleFilter{}); len(got.Rules) != 0 {
		t.Errorf("a stored item's jobs are hidden by default, got %+v", got.Rules)
	}
	if got := scheduleFor(t, store, maintenanceRuleFilter{IncludeStored: true}); len(got.Rules) != 2 {
		t.Errorf("include_stored should list them, got %+v", got.Rules)
	}
	if got := scheduleFor(t, store, maintenanceRuleFilter{IncludeStored: true, System: "no-such-system"}); len(got.Rules) != 0 {
		t.Errorf("system filter should exclude, got %+v", got.Rules)
	}
}
