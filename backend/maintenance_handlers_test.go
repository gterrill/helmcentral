package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// This file exercises maintenance_handlers.go (ADR 0138), the same "call
// the handler directly, not through a router" idiom every other
// *_handlers_test.go file in this package uses.

func mustCreateHandlerTestEquipment(t *testing.T, name string) equipmentItem {
	t.Helper()
	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: name, Category: "mechanical", System: "propulsion"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	return item
}

// ── rules: read ──────────────────────────────────────────────────────────

func TestListMaintenanceRulesHandler_EmptyReturnsEmptyArray(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/inventory/maintenance/rules", "", "")
	if err := listMaintenanceRulesHandler(c); err != nil {
		t.Fatalf("listMaintenanceRulesHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Rules []maintenanceRuleView `json:"rules"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Rules == nil {
		t.Fatalf("expected an empty array, got null")
	}
}

func TestListMaintenanceRulesHandler_ComputesStatusAndFiltersBySystemAndStored(t *testing.T) {
	withTestDocumentStore(t)

	engine := mustCreateHandlerTestEquipment(t, "Main engine")
	pump, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Fresh water pump", Category: "general", System: "water"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	months := 6
	if _, err := globalDocumentStore.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &engine.ID, Description: "Anode check", IntervalMonths: &months}); err != nil {
		t.Fatalf("CreateMaintenanceRule (engine): %v", err)
	}
	if _, err := globalDocumentStore.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &pump.ID, Description: "Filter check", IntervalMonths: &months}); err != nil {
		t.Fatalf("CreateMaintenanceRule (pump): %v", err)
	}
	certMonths := 12
	if _, err := globalDocumentStore.CreateMaintenanceRule(maintenanceRuleInput{Description: "Registration renewal", IntervalMonths: &certMonths}); err != nil {
		t.Fatalf("CreateMaintenanceRule (cert): %v", err)
	}

	// No filter: all 3 visible, every one "never_recorded" (nothing done yet).
	c, rec := newDocumentEchoContext(http.MethodGet, "/api/inventory/maintenance/rules", "", "")
	if err := listMaintenanceRulesHandler(c); err != nil {
		t.Fatalf("listMaintenanceRulesHandler: %v", err)
	}
	var resp struct {
		Rules []maintenanceRuleView `json:"rules"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Rules) != 3 {
		t.Fatalf("expected 3 rules, got %d: %+v", len(resp.Rules), resp.Rules)
	}
	for _, r := range resp.Rules {
		if r.Status != string(maintenanceStatusNeverRecorded) {
			t.Errorf("expected never_recorded for %q, got %q", r.Description, r.Status)
		}
	}

	// system=propulsion: only the engine's own rule, the cert rule excluded.
	c, rec = newDocumentEchoContext(http.MethodGet, "/api/inventory/maintenance/rules?system=propulsion", "", "")
	if err := listMaintenanceRulesHandler(c); err != nil {
		t.Fatalf("listMaintenanceRulesHandler: %v", err)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Rules) != 1 || resp.Rules[0].Description != "Anode check" {
		t.Fatalf("expected only the engine's rule, got %+v", resp.Rules)
	}
}

func TestGetMaintenanceRuleHandler_NotFound(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/inventory/maintenance/rules/bogus", "", "bogus")
	if err := getMaintenanceRuleHandler(c); err != nil {
		t.Fatalf("getMaintenanceRuleHandler: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── rules: create/update validation ─────────────────────────────────────

func TestCreateMaintenanceRuleHandler_RequiresDescription(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/rules", `{"interval_months":6}`, "")
	if err := createMaintenanceRuleHandler(c); err != nil {
		t.Fatalf("createMaintenanceRuleHandler: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateMaintenanceRuleHandler_RequiresAtLeastOneInterval(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/rules", `{"description":"Oil change"}`, "")
	if err := createMaintenanceRuleHandler(c); err != nil {
		t.Fatalf("createMaintenanceRuleHandler: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateMaintenanceRuleHandler_IntervalHoursRequiresEquipment(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/rules", `{"description":"Oil change","interval_hours":250}`, "")
	if err := createMaintenanceRuleHandler(c); err != nil {
		t.Fatalf("createMaintenanceRuleHandler: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateAndUpdateMaintenanceRuleHandler_RoundTrip(t *testing.T) {
	withTestDocumentStore(t)
	engine := mustCreateHandlerTestEquipment(t, "Generator")

	body := `{"equipment_id":"` + engine.ID + `","description":"Oil change","interval_hours":250}`
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/rules", body, "")
	if err := createMaintenanceRuleHandler(c); err != nil {
		t.Fatalf("createMaintenanceRuleHandler: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Rule maintenanceRuleView `json:"rule"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if created.Rule.EquipmentName != "Generator" {
		t.Fatalf("expected the joined equipment name, got %+v", created.Rule)
	}

	updateBody := `{"equipment_id":"` + engine.ID + `","description":"Oil and filter change","interval_hours":300}`
	c, rec = newDocumentEchoContext(http.MethodPut, "/api/inventory/maintenance/rules/"+created.Rule.ID, updateBody, created.Rule.ID)
	if err := updateMaintenanceRuleHandler(c); err != nil {
		t.Fatalf("updateMaintenanceRuleHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var updated struct {
		Rule maintenanceRuleView `json:"rule"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if updated.Rule.Description != "Oil and filter change" || updated.Rule.IntervalHours == nil || *updated.Rule.IntervalHours != 300 {
		t.Fatalf("expected the update to apply, got %+v", updated.Rule)
	}

	c, rec = newDocumentEchoContext(http.MethodDelete, "/api/inventory/maintenance/rules/"+created.Rule.ID, "", created.Rule.ID)
	if err := deleteMaintenanceRuleHandler(c); err != nil {
		t.Fatalf("deleteMaintenanceRuleHandler: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

// ── acknowledge / last-done / complete ──────────────────────────────────

func TestAcknowledgeMaintenanceRuleHandler_SetsAndClears(t *testing.T) {
	withTestDocumentStore(t)
	engine := mustCreateHandlerTestEquipment(t, "Generator")
	months := 6
	rule, err := globalDocumentStore.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &engine.ID, Description: "Anode", IntervalMonths: &months})
	if err != nil {
		t.Fatalf("CreateMaintenanceRule: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/rules/"+rule.ID+"/acknowledge", `{"reason":"waiting on parts"}`, rule.ID)
	if err := acknowledgeMaintenanceRuleHandler(c); err != nil {
		t.Fatalf("acknowledgeMaintenanceRuleHandler: %v", err)
	}
	var resp struct {
		Rule maintenanceRuleView `json:"rule"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Rule.Acknowledged || resp.Rule.AckReason != "waiting on parts" {
		t.Fatalf("expected acknowledged with reason, got %+v", resp.Rule)
	}
	// Still overdue/whatever it was - acknowledging never masks the status.
	if resp.Rule.Status != string(maintenanceStatusNeverRecorded) {
		t.Fatalf("expected status to be unaffected by acknowledgement, got %q", resp.Rule.Status)
	}
}

func TestSetMaintenanceRuleLastDoneHandler_ValidatesAndWritesNoLogEntry(t *testing.T) {
	withTestDocumentStore(t)
	engine := mustCreateHandlerTestEquipment(t, "Generator")
	months := 12
	rule, err := globalDocumentStore.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &engine.ID, Description: "Service", IntervalMonths: &months})
	if err != nil {
		t.Fatalf("CreateMaintenanceRule: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/rules/"+rule.ID+"/last-done", `{}`, rule.ID)
	if err := setMaintenanceRuleLastDoneHandler(c); err != nil {
		t.Fatalf("setMaintenanceRuleLastDoneHandler: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for neither field given, got %d: %s", rec.Code, rec.Body.String())
	}

	c, rec = newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/rules/"+rule.ID+"/last-done", `{"last_done_at":"2026-01-15"}`, rule.ID)
	if err := setMaintenanceRuleLastDoneHandler(c); err != nil {
		t.Fatalf("setMaintenanceRuleLastDoneHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	entries, err := globalDocumentStore.ListMaintenanceLogEntries(maintenanceLogFilter{EquipmentID: engine.ID})
	if err != nil {
		t.Fatalf("ListMaintenanceLogEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no log entry from Set-last-done, got %+v", entries)
	}
}

func TestCompleteMaintenanceRuleHandler_WritesLogAndResetsBaseline(t *testing.T) {
	withTestDocumentStore(t)
	engine := mustCreateHandlerTestEquipment(t, "Generator")
	spare, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Oil filter (spare)", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	months := 12
	rule, err := globalDocumentStore.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &engine.ID, Description: "Oil change", IntervalMonths: &months})
	if err != nil {
		t.Fatalf("CreateMaintenanceRule: %v", err)
	}

	body := `{"performed_at":"2026-06-01","description":"Changed oil","who":"Skipper","cost":45.5,"currency":"AUD","parts":[{"equipment_id":"` + spare.ID + `","quantity":1}]}`
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/rules/"+rule.ID+"/complete", body, rule.ID)
	if err := completeMaintenanceRuleHandler(c); err != nil {
		t.Fatalf("completeMaintenanceRuleHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Rule  maintenanceRuleView `json:"rule"`
		Entry maintenanceLogEntry `json:"entry"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Entry.Kind != "maintenance" {
		t.Fatalf("expected kind maintenance, got %q", resp.Entry.Kind)
	}
	if len(resp.Entry.Parts) != 1 || resp.Entry.Parts[0].EquipmentName != "Oil filter (spare)" {
		t.Fatalf("expected the part joined with its name, got %+v", resp.Entry.Parts)
	}
	if resp.Rule.LastDoneAt != "2026-06-01" {
		t.Fatalf("expected the baseline reset, got %+v", resp.Rule)
	}
	if resp.Rule.Status != string(maintenanceStatusOK) {
		t.Fatalf("expected ok status right after completion, got %q", resp.Rule.Status)
	}
}

func TestCompleteMaintenanceRuleHandler_UnknownPartReturns404(t *testing.T) {
	withTestDocumentStore(t)
	engine := mustCreateHandlerTestEquipment(t, "Generator")
	months := 12
	rule, err := globalDocumentStore.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &engine.ID, Description: "Oil change", IntervalMonths: &months})
	if err != nil {
		t.Fatalf("CreateMaintenanceRule: %v", err)
	}

	body := `{"performed_at":"2026-06-01","parts":[{"equipment_id":"does-not-exist","quantity":1}]}`
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/rules/"+rule.ID+"/complete", body, rule.ID)
	if err := completeMaintenanceRuleHandler(c); err != nil {
		t.Fatalf("completeMaintenanceRuleHandler: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── procedure note ───────────────────────────────────────────────────────

func TestCreateMaintenanceProcedureNoteHandler_CreatesAndLinks(t *testing.T) {
	withTestDocumentStore(t)
	engine := mustCreateHandlerTestEquipment(t, "Generator")
	months := 12
	rule, err := globalDocumentStore.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &engine.ID, Description: "Generator start-up check", IntervalMonths: &months})
	if err != nil {
		t.Fatalf("CreateMaintenanceRule: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/rules/"+rule.ID+"/procedure-note", "", rule.ID)
	if err := createMaintenanceProcedureNoteHandler(c); err != nil {
		t.Fatalf("createMaintenanceProcedureNoteHandler: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Rule maintenanceRuleView `json:"rule"`
		Note documentJSON        `json:"note"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Note.Title != "Generator start-up check" {
		t.Fatalf("expected the note titled from the rule, got %+v", resp.Note)
	}
	if resp.Rule.ProcedureNoteID != resp.Note.ID {
		t.Fatalf("expected the rule linked to the new note, got %+v", resp.Rule)
	}

	doc, err := globalDocumentStore.Get(resp.Note.ID)
	if err != nil {
		t.Fatalf("Get(note): %v", err)
	}
	if doc.Kind != "note" || doc.NoteType != "procedure" {
		t.Fatalf("expected a procedure-type note, got %+v", doc)
	}
}

func TestSetMaintenanceRuleProcedureNoteHandler_RejectsNonNoteDocument(t *testing.T) {
	withTestDocumentStore(t)
	engine := mustCreateHandlerTestEquipment(t, "Generator")
	months := 12
	rule, err := globalDocumentStore.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &engine.ID, Description: "Check", IntervalMonths: &months})
	if err != nil {
		t.Fatalf("CreateMaintenanceRule: %v", err)
	}
	manual, err := globalDocumentStore.Insert(document{SHA256: "manual-sha", Filename: "manual.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/maintenance/rules/"+rule.ID+"/procedure-note", `{"note_id":"`+manual.ID+`"}`, rule.ID)
	if err := setMaintenanceRuleProcedureNoteHandler(c); err != nil {
		t.Fatalf("setMaintenanceRuleProcedureNoteHandler: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a non-note document, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── copy profile schedule ────────────────────────────────────────────────

const testMaintenanceProfile = `{
  "schema_version": 1,
  "kind": "engine",
  "id": "test-engine-maintenance",
  "name": "Test Engine",
  "manufacturer": "Acme",
  "service": [
    {"id": "engine-oil", "description": "Engine oil and filter", "interval_hours": 250, "interval_months": 12, "source": "manual"},
    {"id": "impeller", "description": "Raw water impeller"}
  ]
}`

func TestCopyMaintenanceProfileScheduleHandler(t *testing.T) {
	withTestDocumentStore(t)
	setupEngineProfiles(t, map[string]string{"test-engine.json": testMaintenanceProfile})

	engine, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Main engine", Category: "mechanical", ProfileID: "test-engine-maintenance"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/equipment/"+engine.ID+"/maintenance/copy-profile-schedule", "", engine.ID)
	if err := copyMaintenanceProfileScheduleHandler(c); err != nil {
		t.Fatalf("copyMaintenanceProfileScheduleHandler: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Rules []maintenanceRuleView `json:"rules"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Rules) != 2 {
		t.Fatalf("expected 2 rules copied, got %d: %+v", len(resp.Rules), resp.Rules)
	}
	foundSlot := false
	for _, r := range resp.Rules {
		if r.Description == "Raw water impeller" {
			foundSlot = true
			if r.Status != string(maintenanceStatusIntervalNotSet) {
				t.Errorf("expected the empty-slot entry to be interval_not_set, got %q", r.Status)
			}
		}
	}
	if !foundSlot {
		t.Fatalf("expected the empty-slot service entry to be copied, got %+v", resp.Rules)
	}

	// Pressing it again must not duplicate.
	c, rec = newDocumentEchoContext(http.MethodPost, "/api/inventory/equipment/"+engine.ID+"/maintenance/copy-profile-schedule", "", engine.ID)
	if err := copyMaintenanceProfileScheduleHandler(c); err != nil {
		t.Fatalf("copyMaintenanceProfileScheduleHandler (second): %v", err)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Rules) != 0 {
		t.Fatalf("expected no new rules on the second copy, got %+v", resp.Rules)
	}
}

func TestCopyMaintenanceProfileScheduleHandler_NoProfileIsConflict(t *testing.T) {
	withTestDocumentStore(t)
	engine := mustCreateHandlerTestEquipment(t, "Main engine")

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/equipment/"+engine.ID+"/maintenance/copy-profile-schedule", "", engine.ID)
	if err := copyMaintenanceProfileScheduleHandler(c); err != nil {
		t.Fatalf("copyMaintenanceProfileScheduleHandler: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── hour meter resets ────────────────────────────────────────────────────

func TestRecordAndListHourMeterResetsHandler(t *testing.T) {
	withTestDocumentStore(t)
	engine := mustCreateHandlerTestEquipment(t, "Generator")

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/equipment/"+engine.ID+"/maintenance/meter-reset", `{"old_reading":5000,"new_reading":0,"changed_at":"not-a-date"}`, engine.ID)
	if err := recordHourMeterResetHandler(c); err != nil {
		t.Fatalf("recordHourMeterResetHandler: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a bad date, got %d: %s", rec.Code, rec.Body.String())
	}

	c, rec = newDocumentEchoContext(http.MethodPost, "/api/inventory/equipment/"+engine.ID+"/maintenance/meter-reset", `{"old_reading":5000,"new_reading":0,"changed_at":"2026-01-01"}`, engine.ID)
	if err := recordHourMeterResetHandler(c); err != nil {
		t.Fatalf("recordHourMeterResetHandler: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	c, rec = newDocumentEchoContext(http.MethodGet, "/api/inventory/equipment/"+engine.ID+"/maintenance/meter-resets", "", engine.ID)
	if err := listHourMeterResetsHandler(c); err != nil {
		t.Fatalf("listHourMeterResetsHandler: %v", err)
	}
	var resp struct {
		Resets []hourMeterReset `json:"resets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Resets) != 1 || resp.Resets[0].OldReading != 5000 {
		t.Fatalf("expected the recorded reset, got %+v", resp.Resets)
	}
}

// ── service log ──────────────────────────────────────────────────────────

func TestCreateMaintenanceLogEntryHandler_RequiresEquipmentAndKind(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/log", `{"performed_at":"2026-06-01","kind":"repair"}`, "")
	if err := createMaintenanceLogEntryHandler(c); err != nil {
		t.Fatalf("createMaintenanceLogEntryHandler: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 with no equipment_id, got %d: %s", rec.Code, rec.Body.String())
	}

	engine := mustCreateHandlerTestEquipment(t, "Generator")
	body := `{"equipment_id":"` + engine.ID + `","performed_at":"2026-06-01","kind":"bogus"}`
	c, rec = newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/log", body, "")
	if err := createMaintenanceLogEntryHandler(c); err != nil {
		t.Fatalf("createMaintenanceLogEntryHandler: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a bad kind, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestCreateMaintenanceLogEntryHandler_DuplicatePartReturns400 pins the
// code-review finding: the same equipment_id listed twice in parts used to
// reach maintenance_log_parts' own (log_entry_id, equipment_id) PRIMARY KEY
// unvalidated, so the second INSERT inside CreateMaintenanceLogEntry's
// transaction failed as a raw constraint violation and surfaced as an
// opaque 500 instead of a clean, caller-fixable 400.
func TestCreateMaintenanceLogEntryHandler_DuplicatePartReturns400(t *testing.T) {
	withTestDocumentStore(t)
	engine := mustCreateHandlerTestEquipment(t, "Generator")
	spare, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Impeller (spare)", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	body := `{"equipment_id":"` + engine.ID + `","performed_at":"2026-06-01","kind":"repair","parts":[{"equipment_id":"` + spare.ID + `","quantity":1},{"equipment_id":"` + spare.ID + `","quantity":2}]}`
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/log", body, "")
	if err := createMaintenanceLogEntryHandler(c); err != nil {
		t.Fatalf("createMaintenanceLogEntryHandler: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a duplicated part, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMaintenanceLogEntryHandlers_CRUD(t *testing.T) {
	withTestDocumentStore(t)
	engine := mustCreateHandlerTestEquipment(t, "Generator")

	body := `{"equipment_id":"` + engine.ID + `","performed_at":"2026-06-01","kind":"repair","description":"Replaced belt"}`
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/log", body, "")
	if err := createMaintenanceLogEntryHandler(c); err != nil {
		t.Fatalf("createMaintenanceLogEntryHandler: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Entry maintenanceLogEntry `json:"entry"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	updateBody := `{"equipment_id":"` + engine.ID + `","performed_at":"2026-06-02","kind":"repair","description":"Replaced belt and tensioner"}`
	c, rec = newDocumentEchoContext(http.MethodPut, "/api/inventory/maintenance/log/"+created.Entry.ID, updateBody, created.Entry.ID)
	if err := updateMaintenanceLogEntryHandler(c); err != nil {
		t.Fatalf("updateMaintenanceLogEntryHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	c, rec = newDocumentEchoContext(http.MethodGet, "/api/inventory/maintenance/log/"+created.Entry.ID, "", created.Entry.ID)
	if err := getMaintenanceLogEntryHandler(c); err != nil {
		t.Fatalf("getMaintenanceLogEntryHandler: %v", err)
	}
	var got struct {
		Entry maintenanceLogEntry `json:"entry"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Entry.Description != "Replaced belt and tensioner" {
		t.Fatalf("expected the update to apply, got %+v", got.Entry)
	}

	c, rec = newDocumentEchoContext(http.MethodDelete, "/api/inventory/maintenance/log/"+created.Entry.ID, "", created.Entry.ID)
	if err := deleteMaintenanceLogEntryHandler(c); err != nil {
		t.Fatalf("deleteMaintenanceLogEntryHandler: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

// ── log entry photos ─────────────────────────────────────────────────────

func newMaintenanceLogPhotoUploadContext(t *testing.T, id string, fields []documentUploadField) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	c, rec := newInventoryPhotoUploadContext(t, id, fields)
	// newInventoryPhotoUploadContext builds the request against the
	// equipment photo route; rewrite the request path to this feature's own
	// route before the handler ever reads it, since path itself isn't
	// consulted by echo.Context (params already set) but keeping it honest
	// avoids a request that lies about where it's going were this ever
	// logged.
	c.Request().URL.Path = "/api/inventory/maintenance/log/" + id + "/photos"
	return c, rec
}

func TestUploadAndDeleteMaintenanceLogPhotoHandler(t *testing.T) {
	withTestDocumentStore(t)
	engine := mustCreateHandlerTestEquipment(t, "Generator")
	entry, err := globalDocumentStore.CreateMaintenanceLogEntry(maintenanceLogEntryInput{EquipmentID: &engine.ID, PerformedAt: "2026-06-01", Kind: "repair"})
	if err != nil {
		t.Fatalf("CreateMaintenanceLogEntry: %v", err)
	}

	jpegBytes := []byte("\xFF\xD8\xFF\xE0fakejpegdata")
	c, rec := newMaintenanceLogPhotoUploadContext(t, entry.ID, []documentUploadField{{name: "file", filename: "before.jpg", content: jpegBytes}})
	if err := uploadMaintenanceLogPhotoHandler(c); err != nil {
		t.Fatalf("uploadMaintenanceLogPhotoHandler: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Entry maintenanceLogEntry `json:"entry"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Entry.PhotoIDs) != 1 {
		t.Fatalf("expected one linked photo, got %+v", resp.Entry.PhotoIDs)
	}
	photoID := resp.Entry.PhotoIDs[0]

	c, rec2 := newInventoryEquipmentIDContext(http.MethodDelete, "/api/inventory/maintenance/log/"+entry.ID+"/photos/"+photoID+"?delete=true", "", entry.ID, photoID)
	if err := deleteMaintenanceLogPhotoHandler(c); err != nil {
		t.Fatalf("deleteMaintenanceLogPhotoHandler: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}
	if _, err := globalDocumentStore.Get(photoID); err == nil {
		t.Fatalf("expected the orphaned photo document to be deleted")
	}
}

// ── CSV export ───────────────────────────────────────────────────────────

func TestExportMaintenanceLogCSVHandler(t *testing.T) {
	withTestDocumentStore(t)
	engine := mustCreateHandlerTestEquipment(t, "Generator")
	if _, err := globalDocumentStore.CreateMaintenanceLogEntry(maintenanceLogEntryInput{
		EquipmentID: &engine.ID, PerformedAt: "2026-06-01", Kind: "repair", Description: "Replaced belt", Who: "Skipper",
	}); err != nil {
		t.Fatalf("CreateMaintenanceLogEntry: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/inventory/maintenance/log/export.csv", "", "")
	if err := exportMaintenanceLogCSVHandler(c); err != nil {
		t.Fatalf("exportMaintenanceLogCSVHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get(echo.HeaderContentType); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("expected text/csv content type, got %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "date,item,rule,kind,hours,description,who,cost,currency,parts") {
		t.Fatalf("expected the header row, got %q", body)
	}
	if !strings.Contains(body, "2026-06-01,Generator,,repair,,Replaced belt,Skipper,,,") {
		t.Fatalf("expected the entry row, got %q", body)
	}
}
