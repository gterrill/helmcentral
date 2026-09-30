package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Mate's maintenance read tools: find_equipment, list_maintenance and
// get_maintenance_log. They run against a real t.TempDir()-backed store; the
// status on every rule row must be the one resolveMaintenanceRuleView
// computes, never something the tool works out for itself.

func maintenanceToolDeps(t *testing.T) (assistantToolDeps, *documentStore) {
	t.Helper()
	store := withTestDocumentStore(t)
	return assistantToolDeps{
		documents: func() *documentStore { return store },
		today:     mustParseDate(t, "2026-09-30"),
	}, store
}

func mustToolEquipment(t *testing.T, store *documentStore, item equipmentItem) equipmentItem {
	t.Helper()
	if item.Category == "" {
		item.Category = "mechanical"
	}
	created, err := store.CreateEquipment(item)
	if err != nil {
		t.Fatalf("CreateEquipment(%q): %v", item.Name, err)
	}
	return created
}

func mustToolRule(t *testing.T, store *documentStore, in maintenanceRuleInput) maintenanceRule {
	t.Helper()
	rule, err := store.CreateMaintenanceRule(in)
	if err != nil {
		t.Fatalf("CreateMaintenanceRule(%q): %v", in.Description, err)
	}
	return rule
}

func runMaintenanceTool(t *testing.T, deps assistantToolDeps, name, args string) string {
	t.Helper()
	raw, err := deps.execute(context.Background(), name, json.RawMessage(args))
	if err != nil {
		t.Fatalf("execute %s: %v", name, err)
	}
	return raw
}

// ── find_equipment ──────────────────────────────────────────────────────

func TestFindEquipment_ReportsHoursAndEffectiveJobs(t *testing.T) {
	deps, store := maintenanceToolDeps(t)
	withGlobalSnapshot(t, snapshotWithSelfValues(map[string]any{"electrical.generator.0.runtime": 3600.0 * 239}))
	setupEngineProfiles(t, map[string]string{"a.json": scheduleProfileA})

	gen := mustToolEquipment(t, store, equipmentItem{Name: "Generator", Manufacturer: "Onan", Model: "MDKBH", System: "electrical",
		HourMeterPath: "electrical.generator.0.runtime", ProfileID: "sched-a"})
	mustToolEquipment(t, store, equipmentItem{Name: "Main engine", System: "propulsion"})
	oil := maintenanceJobID(gen.ID, "engine-oil")
	impeller := maintenanceJobID(gen.ID, "impeller")
	mustScheduleTx(t, store, func(tx *sql.Tx, now time.Time) error {
		if _, err := cmdSetMaintenanceRuleOverrides(tx, now, oil, maintenanceOverridesInput{
			IntervalHours: &maintenanceIntervalHoursOverride{Value: f64(300)},
		}); err != nil {
			return err
		}
		na := true
		_, err := cmdSetMaintenanceRuleOverrides(tx, now, impeller, maintenanceOverridesInput{NotApplicable: &na})
		return err
	})

	raw := runMaintenanceTool(t, deps, "find_equipment", `{"query":"onan"}`)
	if strings.Contains(raw, "profile_service") || strings.Contains(raw, "profile_missing") {
		t.Fatalf("the raw profile service list is gone: %s", raw)
	}
	var result assistantFindEquipmentResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, raw)
	}
	if len(result.Equipment) != 1 {
		t.Fatalf("expected one match for onan, got %+v", result.Equipment)
	}
	hit := result.Equipment[0]
	if hit.ID != gen.ID || hit.Name != "Generator" || hit.Manufacturer != "Onan" || hit.System != "electrical" {
		t.Fatalf("unexpected hit: %+v", hit)
	}
	if !hit.HourMeterBound || !hit.HoursKnown || hit.CurrentMeterReading == nil || *hit.CurrentMeterReading != 239 {
		t.Fatalf("expected a known 239 h reading, got %+v", hit)
	}
	if hit.RuleCount != 2 || hit.ProfileID != "sched-a" || len(hit.Rules) != 2 || len(hit.ScheduleErrors) != 0 {
		t.Fatalf("expected the profile's two jobs, got %+v", hit)
	}
	byID := map[string]assistantEquipmentRule{}
	for _, r := range hit.Rules {
		byID[r.ID] = r
	}
	o := byID[oil]
	if o.Description != "Engine oil and filter" || o.Source != "profile" || o.IntervalHours == nil || *o.IntervalHours != 300 ||
		o.IntervalMonths == nil || *o.IntervalMonths != 12 || strings.Join(o.OverriddenFields, ",") != "interval_hours" || o.NotApplicable {
		t.Fatalf("expected the effective oil job (300 h override, 12 mo from the profile), got %+v", o)
	}
	i := byID[impeller]
	if !i.NotApplicable || strings.Join(i.OverriddenFields, ",") != "not_applicable" {
		t.Fatalf("expected the impeller marked not applicable, got %+v", i)
	}
	if hit.Link != "/inventory/equipment/"+gen.ID {
		t.Fatalf("expected the equipment editor link, got %q", hit.Link)
	}
}

func TestFindEquipment_UnknownHoursAreReportedUnknownNeverZero(t *testing.T) {
	deps, store := maintenanceToolDeps(t)
	withGlobalSnapshot(t, snapshotWithSelfValues(map[string]any{}))
	mustToolEquipment(t, store, equipmentItem{Name: "Main engine", System: "propulsion"})
	mustToolEquipment(t, store, equipmentItem{Name: "Watermaker", System: "water", HourMeterPath: "watermaker.runtime"})

	raw := runMaintenanceTool(t, deps, "find_equipment", `{}`)
	if strings.Contains(raw, `"current_meter_reading"`) {
		t.Fatalf("an unknown reading must omit current_meter_reading, never report 0: %s", raw)
	}
	var result assistantFindEquipmentResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Equipment) != 2 {
		t.Fatalf("expected both items, got %+v", result.Equipment)
	}
	for _, hit := range result.Equipment {
		if hit.HoursKnown {
			t.Errorf("%s: hours_known must be false, got %+v", hit.Name, hit)
		}
		if hit.Name == "Watermaker" && !hit.HourMeterBound {
			t.Errorf("Watermaker has a bound meter path, got %+v", hit)
		}
		if hit.Name == "Main engine" && hit.HourMeterBound {
			t.Errorf("Main engine has no meter path, got %+v", hit)
		}
	}
}

func TestFindEquipment_UnavailableProfileIsAScheduleErrorNotAnEmptySchedule(t *testing.T) {
	deps, store := maintenanceToolDeps(t)
	setupEngineProfiles(t, map[string]string{"a.json": scheduleProfileA})
	gen := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical", ProfileID: "gone"})
	mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Belts", IntervalMonths: iptr(12)})
	mustToolEquipment(t, store, equipmentItem{Name: "Main engine", System: "propulsion"})

	raw := runMaintenanceTool(t, deps, "find_equipment", `{}`)
	var result assistantFindEquipmentResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, hit := range result.Equipment {
		switch hit.Name {
		case "Generator":
			if len(hit.ScheduleErrors) != 1 || hit.ScheduleErrors[0].EquipmentID != gen.ID || hit.ScheduleErrors[0].Error != "profile not found" {
				t.Fatalf("expected the profile problem on the item, got %+v", hit.ScheduleErrors)
			}
			if len(hit.Rules) != 1 || hit.Rules[0].Source != "item" {
				t.Fatalf("the hand rule still lists, got %+v", hit.Rules)
			}
		default:
			if len(hit.ScheduleErrors) != 0 {
				t.Fatalf("%s must carry no schedule error, got %+v", hit.Name, hit.ScheduleErrors)
			}
		}
	}
}

func TestFindEquipment_FiltersBySystem(t *testing.T) {
	deps, store := maintenanceToolDeps(t)
	mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical"})
	mustToolEquipment(t, store, equipmentItem{Name: "Main engine", System: "propulsion"})

	raw := runMaintenanceTool(t, deps, "find_equipment", `{"system":"propulsion"}`)
	var result assistantFindEquipmentResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Equipment) != 1 || result.Equipment[0].Name != "Main engine" {
		t.Fatalf("expected only the propulsion item, got %+v", result.Equipment)
	}
}

// ── list_maintenance ────────────────────────────────────────────────────

func seedMaintenanceRules(t *testing.T, store *documentStore) (gen, engine equipmentItem) {
	t.Helper()
	gen = mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical", HourMeterPath: "electrical.generator.0.runtime"})
	engine = mustToolEquipment(t, store, equipmentItem{Name: "Main engine", System: "propulsion", HourMeterPath: "propulsion.main.runTime"})

	months := 6
	hours := 250.0
	// Overdue on the calendar axis: done 2025-01-01, every 6 months.
	overdue := mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &engine.ID, Description: "Anode check", IntervalMonths: &months})
	at := "2025-01-01"
	if _, err := store.SetMaintenanceRuleLastDone(overdue.ID, &at, nil); err != nil {
		t.Fatalf("SetMaintenanceRuleLastDone: %v", err)
	}
	// Hours axis on an item whose bound meter reads nothing: hours unknown
	// (the calendar axis alone still says ok).
	oil := mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &engine.ID, Description: "Oil and filter", IntervalHours: &hours, IntervalMonths: &months})
	oilDone := "2026-09-01"
	if _, err := store.SetMaintenanceRuleLastDone(oil.ID, &oilDone, nil); err != nil {
		t.Fatalf("SetMaintenanceRuleLastDone: %v", err)
	}
	// Fine: done recently.
	okRule := mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Belt check", IntervalMonths: &months})
	recent := "2026-09-01"
	if _, err := store.SetMaintenanceRuleLastDone(okRule.ID, &recent, nil); err != nil {
		t.Fatalf("SetMaintenanceRuleLastDone: %v", err)
	}
	// Calendar-only certificate.
	certMonths := 12
	mustToolRule(t, store, maintenanceRuleInput{Description: "Registration renewal", IntervalMonths: &certMonths})
	return gen, engine
}

func decodeListMaintenance(t *testing.T, raw string) assistantListMaintenanceResult {
	t.Helper()
	var result assistantListMaintenanceResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, raw)
	}
	return result
}

func TestListMaintenance_StatusIsTheViewsStatusAgainstTheGivenToday(t *testing.T) {
	deps, store := maintenanceToolDeps(t)
	withGlobalSnapshot(t, snapshotWithSelfValues(map[string]any{}))
	seedMaintenanceRules(t, store)

	result := decodeListMaintenance(t, runMaintenanceTool(t, deps, "list_maintenance", `{}`))
	if len(result.Rules) != 4 {
		t.Fatalf("expected 4 rules, got %d: %+v", len(result.Rules), result.Rules)
	}

	rules, err := store.ListMaintenanceRuleRows(maintenanceRuleFilter{})
	if err != nil {
		t.Fatalf("ListMaintenanceRules: %v", err)
	}
	for _, rule := range rules {
		view, err := resolveMaintenanceRuleView(rule.ID, deps.today)
		if err != nil {
			t.Fatalf("resolveMaintenanceRuleView: %v", err)
		}
		var row *assistantMaintenanceRuleRow
		for i := range result.Rules {
			if result.Rules[i].ID == rule.ID {
				row = &result.Rules[i]
			}
		}
		if row == nil {
			t.Fatalf("rule %q missing from the result", rule.Description)
		}
		if row.Status != view.Status {
			t.Errorf("%s: tool status %q, view status %q", rule.Description, row.Status, view.Status)
		}
	}
}

func TestListMaintenance_OrdersMostUrgentFirstAndLinksToTheMaintenanceList(t *testing.T) {
	deps, store := maintenanceToolDeps(t)
	withGlobalSnapshot(t, snapshotWithSelfValues(map[string]any{}))
	seedMaintenanceRules(t, store)

	result := decodeListMaintenance(t, runMaintenanceTool(t, deps, "list_maintenance", `{}`))
	if result.Rules[0].Status != "overdue" || result.Rules[0].Description != "Anode check" {
		t.Fatalf("expected the overdue rule first, got %+v", result.Rules[0])
	}
	if result.Rules[len(result.Rules)-1].Status != "ok" {
		t.Fatalf("expected the ok rule last, got %+v", result.Rules[len(result.Rules)-1])
	}
	for _, row := range result.Rules {
		if row.Link != "/inventory/maintenance" {
			t.Errorf("%s: expected the Inventory maintenance link, got %q", row.Description, row.Link)
		}
	}
}

func TestListMaintenance_HoursUnknownIsReportedNeverAsZero(t *testing.T) {
	deps, store := maintenanceToolDeps(t)
	withGlobalSnapshot(t, snapshotWithSelfValues(map[string]any{}))
	seedMaintenanceRules(t, store)

	raw := runMaintenanceTool(t, deps, "list_maintenance", `{"system":"propulsion"}`)
	if strings.Contains(raw, `"current_meter_reading"`) || strings.Contains(raw, `"remaining_hours"`) {
		t.Fatalf("no reading means no current_hours or remaining_hours, never 0: %s", raw)
	}
	result := decodeListMaintenance(t, raw)
	var oil *assistantMaintenanceRuleRow
	for i := range result.Rules {
		if result.Rules[i].Description == "Oil and filter" {
			oil = &result.Rules[i]
		}
	}
	if oil == nil || !oil.HoursUnknown {
		t.Fatalf("expected the oil rule to be flagged hours_unknown, got %+v", oil)
	}
}

func TestListMaintenance_KnownHoursCarryTheReading(t *testing.T) {
	deps, store := maintenanceToolDeps(t)
	withGlobalSnapshot(t, snapshotWithSelfValues(map[string]any{"electrical.generator.0.runtime": 3600.0 * 239}))
	gen, _ := seedMaintenanceRules(t, store)
	hours := 250.0
	mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Oil change", IntervalHours: &hours})

	result := decodeListMaintenance(t, runMaintenanceTool(t, deps, "list_maintenance", `{"equipment_id":"`+gen.ID+`"}`))
	var row *assistantMaintenanceRuleRow
	for i := range result.Rules {
		if result.Rules[i].Description == "Oil change" {
			row = &result.Rules[i]
		}
	}
	if row == nil || row.HoursUnknown || row.CurrentMeterReading == nil || *row.CurrentMeterReading != 239 {
		t.Fatalf("expected a known 239 h reading on the rule, got %+v", row)
	}
}

func TestListMaintenance_Filters(t *testing.T) {
	deps, store := maintenanceToolDeps(t)
	withGlobalSnapshot(t, snapshotWithSelfValues(map[string]any{}))
	gen, _ := seedMaintenanceRules(t, store)

	cases := []struct {
		name string
		args string
		want []string
	}{
		{"by status", `{"status":"overdue"}`, []string{"Anode check"}},
		{"by system", `{"system":"electrical"}`, []string{"Belt check"}},
		{"by equipment", `{"equipment_id":"` + gen.ID + `"}`, []string{"Belt check"}},
		{"calendar only", `{"calendar_only":true}`, []string{"Registration renewal"}},
	}
	for _, tc := range cases {
		result := decodeListMaintenance(t, runMaintenanceTool(t, deps, "list_maintenance", tc.args))
		var got []string
		for _, row := range result.Rules {
			got = append(got, row.Description)
		}
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestListMaintenance_UnknownStatusIsAnError(t *testing.T) {
	deps, _ := maintenanceToolDeps(t)
	_, err := deps.execute(context.Background(), "list_maintenance", json.RawMessage(`{"status":"soonish"}`))
	if err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("expected an error naming the status argument, got %v", err)
	}
}

func TestListMaintenance_ProcedureNoteIsCitable(t *testing.T) {
	deps, store := maintenanceToolDeps(t)
	withGlobalSnapshot(t, snapshotWithSelfValues(map[string]any{}))
	gen := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical"})
	months := 6
	rule := mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Oil change", IntervalMonths: &months})
	note := mustCreateTestNote(t, "Drain warm.", "Generator oil change")
	if _, err := store.SetMaintenanceRuleProcedureNote(rule.ID, note.ID); err != nil {
		t.Fatalf("SetMaintenanceRuleProcedureNote: %v", err)
	}

	result := decodeListMaintenance(t, runMaintenanceTool(t, deps, "list_maintenance", `{}`))
	row := result.Rules[0]
	if row.ProcedureNote == nil || row.ProcedureNote.DocumentID != note.ID || row.ProcedureNote.Title != "Generator oil change" {
		t.Fatalf("expected the procedure note as a citation, got %+v", row.ProcedureNote)
	}
}

func TestMaintenanceTools_RequireTodayAndAStore(t *testing.T) {
	deps, _ := maintenanceToolDeps(t)

	noToday := deps
	noToday.today = time.Time{}
	if _, err := noToday.execute(context.Background(), "list_maintenance", json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "today") {
		t.Fatalf("expected list_maintenance to fail naming today, got %v", err)
	}

	noStore := assistantToolDeps{today: deps.today}
	for _, tool := range []string{"find_equipment", "list_maintenance", "get_maintenance_log"} {
		_, err := noStore.execute(context.Background(), tool, json.RawMessage(`{}`))
		if err == nil || !strings.Contains(err.Error(), "not available") {
			t.Errorf("%s: expected a store-unavailable error, got %v", tool, err)
		}
	}
	nilStore := assistantToolDeps{today: deps.today, documents: func() *documentStore { return nil }}
	if _, err := nilStore.execute(context.Background(), "list_maintenance", json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected a nil store to be an error, got %v", err)
	}
}

func TestListMaintenance_CapsAndMarksTruncation(t *testing.T) {
	deps, store := maintenanceToolDeps(t)
	withGlobalSnapshot(t, snapshotWithSelfValues(map[string]any{}))
	eq := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical"})
	months := 6
	for i := 0; i < 150; i++ {
		mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &eq.ID, Description: strings.Repeat("Long rule description ", 4) + string(rune('A'+i%26)) + strings.Repeat("x", i), IntervalMonths: &months})
	}
	raw := runMaintenanceTool(t, deps, "list_maintenance", `{}`)
	if len(raw) > assistantMaxToolResultChars {
		t.Fatalf("result exceeds the cap: %d chars", len(raw))
	}
	result := decodeListMaintenance(t, raw)
	if !result.Truncated || result.Total != 150 || len(result.Rules) >= 150 {
		t.Fatalf("expected a truncated result reporting the full total, got truncated=%v total=%d shown=%d", result.Truncated, result.Total, len(result.Rules))
	}
}

func TestListMaintenance_MeterReadingsSurviveAMeterReplacement(t *testing.T) {
	deps, store := maintenanceToolDeps(t)
	// New meter reads 50 h; the old one had 2200 h when it was replaced.
	withGlobalSnapshot(t, snapshotWithSelfValues(map[string]any{"electrical.generator.0.runtime": 3600.0 * 50}))
	gen := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical", HourMeterPath: "electrical.generator.0.runtime"})
	if _, err := store.RecordHourMeterReset(gen.ID, 2200, 0, "2026-01-01"); err != nil {
		t.Fatalf("RecordHourMeterReset: %v", err)
	}
	interval := 200.0
	rule := mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Oil change", IntervalHours: &interval})
	doneAt, doneHours := "2025-12-01", 2200.0 // engine hours (cumulative)
	if _, err := store.SetMaintenanceRuleLastDone(rule.ID, &doneAt, &doneHours); err != nil {
		t.Fatalf("SetMaintenanceRuleLastDone: %v", err)
	}

	result := decodeListMaintenance(t, runMaintenanceTool(t, deps, "list_maintenance", `{}`))
	row := result.Rules[0]
	if row.CurrentMeterReading == nil || *row.CurrentMeterReading != 50 {
		t.Fatalf("expected the meter to read 50, got %+v", row.CurrentMeterReading)
	}
	if row.LastDoneEngineHours == nil || *row.LastDoneEngineHours != 2200 {
		t.Fatalf("expected last done at 2200 engine hours, got %+v", row.LastDoneEngineHours)
	}
	if row.RemainingHours == nil || *row.RemainingHours != 150 {
		t.Fatalf("expected 150 h remaining, got %+v", row.RemainingHours)
	}
	// Due when the operator's meter reads 200, not at engine hour 2400.
	if row.NextDueMeterReading == nil || *row.NextDueMeterReading != 200 {
		t.Fatalf("expected next due at meter reading 200, got %+v", row.NextDueMeterReading)
	}
	raw := runMaintenanceTool(t, deps, "list_maintenance", `{}`)
	if strings.Contains(raw, "next_due_hours") || strings.Contains(raw, `"current_hours"`) || strings.Contains(raw, `"last_done_hours"`) {
		t.Fatalf("the old, scale-ambiguous field names must be gone: %s", raw)
	}
}

func TestListMaintenance_UnknownEquipmentIDIsAnError(t *testing.T) {
	deps, _ := maintenanceToolDeps(t)
	_, err := deps.execute(context.Background(), "list_maintenance", json.RawMessage(`{"equipment_id":"nope"}`))
	if err == nil || !strings.Contains(err.Error(), `"nope"`) || !strings.Contains(err.Error(), "find_equipment") {
		t.Fatalf("expected an error naming the id and pointing to find_equipment, got %v", err)
	}
}

// ── get_maintenance_log ─────────────────────────────────────────────────

func TestGetMaintenanceLog_FiltersByEquipmentRuleAndSince(t *testing.T) {
	deps, store := maintenanceToolDeps(t)
	gen := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical"})
	engine := mustToolEquipment(t, store, equipmentItem{Name: "Main engine", System: "propulsion"})
	filter := mustToolEquipment(t, store, equipmentItem{Name: "Oil filter", Category: "general"})
	hours := 239.0
	cost := 42.5
	if _, err := store.CreateMaintenanceLogEntry(maintenanceLogEntryInput{
		EquipmentID: &gen.ID, PerformedAt: "2025-01-09", Hours: &hours, Kind: "maintenance",
		Description: "Oil and filter", Who: "Gavin", Cost: &cost, Currency: "AUD",
		Parts: []maintenanceLogPartInput{{EquipmentID: filter.ID, Quantity: 1}},
	}); err != nil {
		t.Fatalf("CreateMaintenanceLogEntry: %v", err)
	}
	if _, err := store.CreateMaintenanceLogEntry(maintenanceLogEntryInput{
		EquipmentID: &engine.ID, PerformedAt: "2026-08-01", Kind: "repair", Description: "Replaced raw water hose",
	}); err != nil {
		t.Fatalf("CreateMaintenanceLogEntry: %v", err)
	}

	decode := func(args string) assistantMaintenanceLogResult {
		var r assistantMaintenanceLogResult
		raw := runMaintenanceTool(t, deps, "get_maintenance_log", args)
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			t.Fatalf("unmarshal: %v (%s)", err, raw)
		}
		return r
	}

	all := decode(`{}`)
	if len(all.Entries) != 2 || all.Entries[0].Description != "Replaced raw water hose" {
		t.Fatalf("expected two entries, newest first, got %+v", all.Entries)
	}
	genOnly := decode(`{"equipment_id":"` + gen.ID + `"}`)
	if len(genOnly.Entries) != 1 {
		t.Fatalf("expected one generator entry, got %+v", genOnly.Entries)
	}
	e := genOnly.Entries[0]
	if e.EquipmentName != "Generator" || e.PerformedAt != "2025-01-09" || e.EngineHours == nil || *e.EngineHours != 239 ||
		e.Kind != "maintenance" || e.Who != "Gavin" || e.Cost == nil || *e.Cost != 42.5 || e.Currency != "AUD" ||
		len(e.Parts) != 1 || e.Parts[0].Name != "Oil filter" || e.Link != "/inventory/maintenance" {
		t.Fatalf("unexpected entry: %+v", e)
	}
	since := decode(`{"since":"2026-01-01"}`)
	if len(since.Entries) != 1 || since.Entries[0].Description != "Replaced raw water hose" {
		t.Fatalf("expected only the 2026 entry, got %+v", since.Entries)
	}
	entryJSON, err := json.Marshal(since.Entries[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(entryJSON), `"engine_hours"`) {
		t.Fatalf("an entry with no hours must omit them, never report 0: %+v", since.Entries[0])
	}
}

func TestGetMaintenanceLog_RuleFilterAndBadArguments(t *testing.T) {
	deps, store := maintenanceToolDeps(t)
	gen := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical"})
	months := 6
	rule := mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Oil change", IntervalMonths: &months})
	hours := 10.0
	if _, _, err := store.CompleteMaintenanceRule(rule.ID, maintenanceLogEntryInput{
		EquipmentID: &gen.ID, PerformedAt: "2026-09-01", Hours: &hours, Description: "Oil change",
	}, ""); err != nil {
		t.Fatalf("CompleteMaintenanceRule: %v", err)
	}
	if _, err := store.CreateMaintenanceLogEntry(maintenanceLogEntryInput{EquipmentID: &gen.ID, PerformedAt: "2026-09-02", Kind: "repair", Description: "Fixed a leak"}); err != nil {
		t.Fatalf("CreateMaintenanceLogEntry: %v", err)
	}

	var r assistantMaintenanceLogResult
	if err := json.Unmarshal([]byte(runMaintenanceTool(t, deps, "get_maintenance_log", `{"rule_id":"`+rule.ID+`"}`)), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(r.Entries) != 1 || r.Entries[0].Description != "Oil change" || r.Entries[0].RuleID != rule.ID {
		t.Fatalf("expected only the entry written by completing the rule, got %+v", r.Entries)
	}

	for _, args := range []string{`{"since":"last week"}`, `{"equipment_id":"nope"}`, `{"rule_id":"nope"}`} {
		if _, err := deps.execute(context.Background(), "get_maintenance_log", json.RawMessage(args)); err == nil {
			t.Errorf("expected an error for %s", args)
		}
	}
}

// ── status lines ────────────────────────────────────────────────────────

func TestDescribeAssistantToolCall_MaintenanceTools(t *testing.T) {
	cases := []struct{ name, args, want string }{
		{"find_equipment", `{"query":"genset"}`, `Looking up equipment "genset"…`},
		{"find_equipment", `{}`, "Looking up equipment…"},
		{"list_maintenance", `{}`, "Checking the maintenance list…"},
		{"get_maintenance_log", `{}`, "Reading the maintenance log…"},
	}
	for _, tc := range cases {
		if got := describeAssistantToolCall(tc.name, json.RawMessage(tc.args)); got != tc.want {
			t.Errorf("describeAssistantToolCall(%s, %s) = %q, want %q", tc.name, tc.args, got, tc.want)
		}
	}
}

func TestListMaintenance_ProfileJobsCarrySourceAndAnUnavailableProfileIsReported(t *testing.T) {
	deps, store := maintenanceToolDeps(t)
	setupEngineProfiles(t, map[string]string{"a.json": scheduleProfileA})
	item := mustToolEquipment(t, store, equipmentItem{Name: "Main engine", System: "propulsion", ProfileID: "sched-a"})
	other := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical", ProfileID: "gone"})

	var res assistantListMaintenanceResult
	if err := json.Unmarshal([]byte(runMaintenanceTool(t, deps, "list_maintenance", `{}`)), &res); err != nil {
		t.Fatal(err)
	}
	var oil *assistantMaintenanceRuleRow
	for i := range res.Rules {
		if res.Rules[i].ID == maintenanceJobID(item.ID, "engine-oil") {
			oil = &res.Rules[i]
		}
	}
	if oil == nil || oil.Source != "profile" || oil.Description != "Engine oil and filter" || oil.IntervalHours == nil || *oil.IntervalHours != 250 {
		t.Fatalf("expected the profile job with effective values, got %+v", res.Rules)
	}
	if len(res.ScheduleErrors) != 1 || res.ScheduleErrors[0].EquipmentID != other.ID || res.ScheduleErrors[0].Error != "profile not found" {
		t.Fatalf("an unavailable profile must be reported, got %+v", res.ScheduleErrors)
	}
}
