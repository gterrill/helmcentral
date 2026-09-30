package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// HTTP-level tests for profile jobs in the maintenance API (ADR 0148): the
// list and single-rule shapes, the 400s for editing or deleting a live job,
// overrides, lazy row creation on the state writes, the profile guards and the
// CSV export.

type paramPair struct{ name, value string }

func callHandler(t *testing.T, h echo.HandlerFunc, method, target, body string, params ...paramPair) *httptest.ResponseRecorder {
	t.Helper()
	c, rec := newDocumentEchoContext(method, target, body, "")
	if len(params) > 0 {
		names := make([]string, len(params))
		values := make([]string, len(params))
		for i, p := range params {
			names[i], values[i] = p.name, p.value
		}
		c.SetParamNames(names...)
		c.SetParamValues(values...)
	}
	if err := h(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	return rec
}

func idParam(id string) paramPair { return paramPair{"id", id} }

const today = "?today=2026-06-15"

func decodeRule(t *testing.T, rec *httptest.ResponseRecorder) maintenanceRuleView {
	t.Helper()
	var resp struct {
		Rule maintenanceRuleView `json:"rule"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal %s: %v", rec.Body.String(), err)
	}
	return resp.Rule
}

type listResponse struct {
	Rules          []maintenanceRuleView      `json:"rules"`
	Removed        []maintenanceRuleView      `json:"removed"`
	ScheduleErrors []maintenanceScheduleError `json:"schedule_errors"`
}

func listRules(t *testing.T, query string) listResponse {
	t.Helper()
	rec := callHandler(t, listMaintenanceRulesHandler, http.MethodGet, "/api/inventory/maintenance/rules"+today+query, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var resp listResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func viewByID(rules []maintenanceRuleView, id string) *maintenanceRuleView {
	for i := range rules {
		if rules[i].ID == id {
			return &rules[i]
		}
	}
	return nil
}

func TestListHandler_ShowsProfileJobsWithSourceAndStatus(t *testing.T) {
	_, item := scheduleFixture(t)
	resp := listRules(t, "&equipment="+item.ID)
	oil := viewByID(resp.Rules, maintenanceJobID(item.ID, "engine-oil"))
	imp := viewByID(resp.Rules, maintenanceJobID(item.ID, "impeller"))
	if oil == nil || imp == nil {
		t.Fatalf("both profile jobs should list: %+v", resp.Rules)
	}
	if oil.Source != "profile" || oil.ProfileID != "sched-a" || oil.Status != string(maintenanceStatusNeverRecorded) {
		t.Errorf("unexpected oil job: %+v", oil)
	}
	if oil.ProfileValues == nil || oil.FirstAtHours == nil || len(oil.OverriddenFields) != 0 || oil.Supersedes == nil {
		t.Errorf("view must carry profile_values, first_at_hours and non-null arrays: %+v", oil)
	}
	if imp.Status != string(maintenanceStatusIntervalNotSet) {
		t.Errorf("a slot reads interval_not_set, got %q", imp.Status)
	}
	if resp.Removed == nil || resp.ScheduleErrors == nil {
		t.Errorf("removed and schedule_errors must be arrays, never null")
	}
}

func TestListHandler_MissingProfileIsAScheduleErrorNotASnapshot(t *testing.T) {
	store, item := scheduleFixture(t)
	if _, err := store.db.Exec(`UPDATE equipment SET profile_id = 'gone' WHERE id = ?`, item.ID); err != nil {
		t.Fatal(err)
	}
	rec := callHandler(t, listMaintenanceRulesHandler, http.MethodGet, "/api/inventory/maintenance/rules"+today, "")
	var resp listResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Rules) != 0 || len(resp.ScheduleErrors) != 1 || resp.ScheduleErrors[0].ProfileID != "gone" {
		t.Fatalf("expected no rules and one error, got %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "Engine oil") {
		t.Fatalf("no job text may leak: %s", rec.Body.String())
	}
}

func TestGetHandler_AnUntouchedJobResolvesAndAnUnavailableOneIsA409(t *testing.T) {
	store, item := scheduleFixture(t)
	id := maintenanceJobID(item.ID, "engine-oil")
	rec := callHandler(t, getMaintenanceRuleHandler, http.MethodGet, "/x"+today, "", idParam(id))
	if rec.Code != http.StatusOK || decodeRule(t, rec).ID != id {
		t.Fatalf("expected the job, got %d %s", rec.Code, rec.Body.String())
	}
	rec = callHandler(t, getMaintenanceRuleHandler, http.MethodGet, "/x"+today, "", idParam(maintenanceJobID(item.ID, "nope")))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a job id that matches nothing, got %d", rec.Code)
	}
	if _, err := store.db.Exec(`UPDATE equipment SET profile_id = 'gone' WHERE id = ?`, item.ID); err != nil {
		t.Fatal(err)
	}
	rec = callHandler(t, getMaintenanceRuleHandler, http.MethodGet, "/x"+today, "", idParam(id))
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestPutAndDeleteOnALiveJobAreRefusedWithAPointerToOverrides(t *testing.T) {
	_, item := scheduleFixture(t)
	id := maintenanceJobID(item.ID, "engine-oil")
	rec := callHandler(t, updateMaintenanceRuleHandler, http.MethodPut, "/x"+today,
		`{"equipment_id":"`+item.ID+`","description":"x","interval_hours":10}`, idParam(id))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "overrides") {
		t.Fatalf("PUT on a job: %d %s", rec.Code, rec.Body.String())
	}
	rec = callHandler(t, deleteMaintenanceRuleHandler, http.MethodDelete, "/x", "", idParam(id))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("DELETE on a live job: %d %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteOfARemovedJobKeepsItsLogHistory(t *testing.T) {
	store, item := scheduleFixture(t)
	impeller := maintenanceJobID(item.ID, "impeller")
	rec := callHandler(t, completeMaintenanceRuleHandler, http.MethodPost, "/x"+today, `{"performed_at":"2026-06-01"}`, idParam(impeller))
	if rec.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", rec.Code, rec.Body.String())
	}
	replaceProfile(t, globalProfileStore, `{"schema_version":1,"kind":"engine","id":"sched-a","name":"Engine A","manufacturer":"Acme",
	  "service":[{"id":"engine-oil","description":"Engine oil and filter","interval_hours":250,"interval_months":12}]}`)

	resp := listRules(t, "&equipment="+item.ID)
	gone := viewByID(resp.Removed, impeller)
	if gone == nil || !gone.RemovedFromProfile || gone.Status != "" || gone.Description != "Raw water impeller" {
		t.Fatalf("expected the removed group to hold the impeller, got %+v", resp.Removed)
	}
	rec = callHandler(t, getMaintenanceRuleHandler, http.MethodGet, "/x"+today, "", idParam(impeller))
	if rec.Code != http.StatusOK || !decodeRule(t, rec).RemovedFromProfile {
		t.Fatalf("a removed job still resolves for display: %d %s", rec.Code, rec.Body.String())
	}
	// Writes to it are refused.
	rec = callHandler(t, acknowledgeMaintenanceRuleHandler, http.MethodPost, "/x"+today, `{"reason":"x"}`, idParam(impeller))
	if rec.Code != http.StatusConflict {
		t.Fatalf("writes to a removed job: %d %s", rec.Code, rec.Body.String())
	}

	rec = callHandler(t, deleteMaintenanceRuleHandler, http.MethodDelete, "/x", "", idParam(impeller))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete removed job: %d %s", rec.Code, rec.Body.String())
	}
	entries, err := store.ListMaintenanceLogEntries(maintenanceLogFilter{EquipmentID: item.ID})
	if err != nil || len(entries) != 1 || entries[0].RuleID != nil {
		t.Fatalf("the log entry must survive with no rule, got %+v %v", entries, err)
	}
}

func TestCreateRuleCannotNameAProfileService(t *testing.T) {
	_, item := scheduleFixture(t)
	rec := callHandler(t, createMaintenanceRuleHandler, http.MethodPost, "/x"+today,
		`{"equipment_id":"`+item.ID+`","description":"Sneaky","interval_months":3,"profile_service_id":"engine-oil"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "profile_service_id") {
		t.Fatalf("expected 400 naming the field, got %d %s", rec.Code, rec.Body.String())
	}
	// And a rule by hand can no longer be an empty slot.
	rec = callHandler(t, createMaintenanceRuleHandler, http.MethodPost, "/x"+today, `{"equipment_id":"`+item.ID+`","description":"Slot"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestOverridesEndpoints(t *testing.T) {
	_, item := scheduleFixture(t)
	id := maintenanceJobID(item.ID, "engine-oil")
	put := func(body string) *httptest.ResponseRecorder {
		return callHandler(t, setMaintenanceRuleOverridesHandler, http.MethodPut, "/x"+today, body, idParam(id))
	}

	rec := put(`{"interval_hours": 400, "interval_months": null, "description": "Oil, the boat's way"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put: %d %s", rec.Code, rec.Body.String())
	}
	v := decodeRule(t, rec)
	if v.IntervalHours == nil || *v.IntervalHours != 400 || v.IntervalMonths != nil || v.Description != "Oil, the boat's way" {
		t.Errorf("unexpected effective values: %+v", v)
	}
	if strings.Join(v.OverriddenFields, ",") != "description,interval_hours,interval_months" {
		t.Errorf("overridden_fields = %v", v.OverriddenFields)
	}
	if v.ProfileValues == nil || v.ProfileValues.Description != "Engine oil and filter" || *v.ProfileValues.IntervalMonths != 12 {
		t.Errorf("profile_values should keep the profile's figures: %+v", v.ProfileValues)
	}

	for _, bad := range []struct{ name, body string }{
		{"zero hours", `{"interval_hours": 0}`},
		{"zero months", `{"interval_months": 0}`},
		{"blank description", `{"description": "  "}`},
		{"unknown field", `{"bogus": 5}`},
		{"wrong type", `{"interval_hours": "lots"}`},
		{"empty body", `{}`},
	} {
		if rec := put(bad.body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d %s", bad.name, rec.Code, rec.Body.String())
		}
	}

	reset := func(field string) *httptest.ResponseRecorder {
		return callHandler(t, resetMaintenanceRuleOverrideHandler, http.MethodDelete, "/x"+today, "", idParam(id), paramPair{"field", field})
	}
	rec = reset("interval_months")
	if rec.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body.String())
	}
	v = decodeRule(t, rec)
	if v.IntervalMonths == nil || *v.IntervalMonths != 12 || strings.Join(v.OverriddenFields, ",") != "description,interval_hours" {
		t.Errorf("after reset: %+v %v", v.IntervalMonths, v.OverriddenFields)
	}
	if rec := reset("bogus"); rec.Code != http.StatusBadRequest {
		t.Errorf("reset of an unknown field: %d", rec.Code)
	}
}

func TestOverridesAreForJobsOnly(t *testing.T) {
	_, item := scheduleFixture(t)
	months := 6
	hand, err := globalDocumentStore.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &item.ID, Description: "Anodes", IntervalMonths: &months})
	if err != nil {
		t.Fatal(err)
	}
	rec := callHandler(t, setMaintenanceRuleOverridesHandler, http.MethodPut, "/x"+today, `{"interval_months": 3}`, idParam(hand.ID))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("overrides on a hand rule: %d %s", rec.Code, rec.Body.String())
	}
	rec = callHandler(t, resetMaintenanceRuleOverrideHandler, http.MethodDelete, "/x"+today, "", idParam(hand.ID), paramPair{"field", "interval_months"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reset on a hand rule: %d", rec.Code)
	}
}

func TestNotApplicableJobsListButNeverGoDue(t *testing.T) {
	_, item := scheduleFixture(t)
	id := maintenanceJobID(item.ID, "engine-oil")
	// Done long ago: would be overdue.
	rec := callHandler(t, setMaintenanceRuleLastDoneHandler, http.MethodPost, "/x"+today, `{"last_done_at":"2020-01-01"}`, idParam(id))
	if rec.Code != http.StatusOK {
		t.Fatalf("last-done: %d %s", rec.Code, rec.Body.String())
	}
	if got := decodeRule(t, rec).Status; got != string(maintenanceStatusOverdue) {
		t.Fatalf("expected overdue first, got %q", got)
	}
	rec = callHandler(t, setMaintenanceRuleOverridesHandler, http.MethodPut, "/x"+today, `{"not_applicable": true}`, idParam(id))
	v := decodeRule(t, rec)
	if v.Status != string(maintenanceStatusNotApplicable) || !v.NotApplicable {
		t.Fatalf("expected not_applicable, got %+v", v)
	}
	if viewByID(listRules(t, "&equipment="+item.ID).Rules, id) == nil {
		t.Errorf("a not-applicable job still lists")
	}
	// false is a reset.
	rec = callHandler(t, setMaintenanceRuleOverridesHandler, http.MethodPut, "/x"+today, `{"not_applicable": false}`, idParam(id))
	if v := decodeRule(t, rec); v.NotApplicable || v.Status != string(maintenanceStatusOverdue) || len(v.OverriddenFields) != 0 {
		t.Fatalf("expected the job back and overdue, got %+v", v)
	}
}

func TestStateWritesOnAnUntouchedJobCreateItsRow(t *testing.T) {
	store, item := scheduleFixture(t)
	oil := maintenanceJobID(item.ID, "engine-oil")
	impeller := maintenanceJobID(item.ID, "impeller")

	rec := callHandler(t, acknowledgeMaintenanceRuleHandler, http.MethodPost, "/x"+today, `{"reason":"after haul-out"}`, idParam(oil))
	if rec.Code != http.StatusOK || !decodeRule(t, rec).Acknowledged {
		t.Fatalf("ack: %d %s", rec.Code, rec.Body.String())
	}
	rec = callHandler(t, setMaintenanceRuleLastDoneHandler, http.MethodPost, "/x"+today, `{"last_done_at":"2026-06-01","last_done_hours":100}`, idParam(oil))
	if rec.Code != http.StatusOK {
		t.Fatalf("last-done: %d %s", rec.Code, rec.Body.String())
	}
	// Completion needs hours because the PROFILE's interval is in hours.
	rec = callHandler(t, completeMaintenanceRuleHandler, http.MethodPost, "/x"+today, `{"performed_at":"2026-06-10"}`, idParam(oil))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "hours") {
		t.Fatalf("complete without hours: %d %s", rec.Code, rec.Body.String())
	}
	rec = callHandler(t, completeMaintenanceRuleHandler, http.MethodPost, "/x"+today, `{"performed_at":"2026-06-10","hours":120}`, idParam(oil))
	if rec.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", rec.Code, rec.Body.String())
	}
	// A months-only override makes hours unnecessary.
	rec = callHandler(t, setMaintenanceRuleOverridesHandler, http.MethodPut, "/x"+today, `{"interval_hours": null}`, idParam(oil))
	if rec.Code != http.StatusOK {
		t.Fatalf("override: %d %s", rec.Code, rec.Body.String())
	}
	rec = callHandler(t, completeMaintenanceRuleHandler, http.MethodPost, "/x"+today, `{"performed_at":"2026-06-11"}`, idParam(oil))
	if rec.Code != http.StatusOK {
		t.Fatalf("complete after the override: %d %s", rec.Code, rec.Body.String())
	}

	rec = callHandler(t, createMaintenanceProcedureNoteHandler, http.MethodPost, "/x"+today, "", idParam(impeller))
	if rec.Code != http.StatusCreated {
		t.Fatalf("procedure note: %d %s", rec.Code, rec.Body.String())
	}
	v := decodeRule(t, rec)
	if v.ProcedureNoteID == "" || v.ID != impeller {
		t.Fatalf("expected the note linked to the job, got %+v", v)
	}
	var title string
	if err := store.db.QueryRow(`SELECT title FROM documents WHERE id = ?`, v.ProcedureNoteID).Scan(&title); err != nil || title != "Raw water impeller" {
		t.Fatalf("the note is titled from the effective description, got %q %v", title, err)
	}
	if n := countRuleRows(t, store); n != 2 {
		t.Fatalf("expected exactly the two touched jobs to have rows, got %d", n)
	}
}

func TestWritesTo409WhenTheProfileIsUnavailable(t *testing.T) {
	store, item := scheduleFixture(t)
	id := maintenanceJobID(item.ID, "engine-oil")
	if _, err := store.db.Exec(`UPDATE equipment SET profile_id = 'gone' WHERE id = ?`, item.ID); err != nil {
		t.Fatal(err)
	}
	for name, run := range map[string]func() *httptest.ResponseRecorder{
		"acknowledge": func() *httptest.ResponseRecorder {
			return callHandler(t, acknowledgeMaintenanceRuleHandler, http.MethodPost, "/x"+today, `{"reason":"x"}`, idParam(id))
		},
		"last-done": func() *httptest.ResponseRecorder {
			return callHandler(t, setMaintenanceRuleLastDoneHandler, http.MethodPost, "/x"+today, `{"last_done_at":"2026-06-01"}`, idParam(id))
		},
		"complete": func() *httptest.ResponseRecorder {
			return callHandler(t, completeMaintenanceRuleHandler, http.MethodPost, "/x"+today, `{"performed_at":"2026-06-01","hours":1}`, idParam(id))
		},
		"overrides": func() *httptest.ResponseRecorder {
			return callHandler(t, setMaintenanceRuleOverridesHandler, http.MethodPut, "/x"+today, `{"interval_hours":5}`, idParam(id))
		},
		"procedure-note": func() *httptest.ResponseRecorder {
			return callHandler(t, createMaintenanceProcedureNoteHandler, http.MethodPost, "/x"+today, "", idParam(id))
		},
	} {
		if rec := run(); rec.Code != http.StatusConflict {
			t.Errorf("%s: expected 409, got %d %s", name, rec.Code, rec.Body.String())
		}
	}
	if n := countRuleRows(t, store); n != 0 {
		t.Errorf("refused writes must leave no rows, got %d", n)
	}
}

func TestProfileChangePreviewHandler(t *testing.T) {
	_, item := scheduleFixture(t)
	rec := callHandler(t, maintenanceProfileChangePreviewHandler, http.MethodGet, "/x?profile_id=sched-b", "", idParam(item.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var raw map[string][]map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw["kept"]) != 1 || len(raw["leaving"]) != 1 || len(raw["new"]) != 1 || raw["new"][0]["service_id"] != "belt" {
		t.Fatalf("unexpected preview %s", rec.Body.String())
	}
	if rec := callHandler(t, maintenanceProfileChangePreviewHandler, http.MethodGet, "/x?profile_id=nope", "", idParam(item.ID)); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown profile: %d", rec.Code)
	}
	if rec := callHandler(t, maintenanceProfileChangePreviewHandler, http.MethodGet, "/x?profile_id=", "", idParam(item.ID)); rec.Code != http.StatusOK {
		t.Errorf("clearing: %d", rec.Code)
	}
	if rec := callHandler(t, maintenanceProfileChangePreviewHandler, http.MethodGet, "/x?profile_id=sched-b", "", idParam("no-such-item")); rec.Code != http.StatusNotFound {
		t.Errorf("unknown item: %d", rec.Code)
	}
}

func TestCSVExportLabelsJobsFromTheirEffectiveDescription(t *testing.T) {
	_, item := scheduleFixture(t)
	impeller := maintenanceJobID(item.ID, "impeller")
	rec := callHandler(t, completeMaintenanceRuleHandler, http.MethodPost, "/x"+today, `{"performed_at":"2026-06-01"}`, idParam(impeller))
	if rec.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", rec.Code, rec.Body.String())
	}
	csv := func() string {
		rec := callHandler(t, exportMaintenanceLogCSVHandler, http.MethodGet, "/x", "")
		return rec.Body.String()
	}
	out := csv()
	if !strings.Contains(out, "schedule") || !strings.Contains(out, "Raw water impeller,maintenance") || !strings.HasSuffix(strings.Split(strings.TrimSpace(out), "\n")[1], ",profile") {
		t.Fatalf("unexpected CSV: %s", out)
	}
	// Live profile text shows at once.
	replaceProfile(t, globalProfileStore, strings.Replace(scheduleProfileA, "Raw water impeller", "Seawater impeller", 1))
	if out := csv(); !strings.Contains(out, "Seawater impeller,maintenance") {
		t.Fatalf("CSV should use the live description: %s", out)
	}
	// Once the service leaves the profile, the row's snapshot (taken when the
	// job was last written to) is labelled as such.
	replaceProfile(t, globalProfileStore, `{"schema_version":1,"kind":"engine","id":"sched-a","name":"Engine A","manufacturer":"Acme",
	  "service":[{"id":"engine-oil","description":"Engine oil and filter","interval_hours":250,"interval_months":12}]}`)
	if out := csv(); !strings.Contains(out, "Raw water impeller (no longer in profile)") {
		t.Fatalf("CSV should label a removed job: %s", out)
	}
}

// ── profile guards ───────────────────────────────────────────────────────

func putProfile(t *testing.T, id, doc, query string) *httptest.ResponseRecorder {
	t.Helper()
	return callHandler(t, updateEquipmentProfileHandler, http.MethodPut, "/api/equipment-profiles/"+id+query, doc, idParam(id))
}

const scheduleProfileAWithoutImpeller = `{
  "schema_version": 1, "kind": "engine", "id": "sched-a", "name": "Engine A", "manufacturer": "Acme",
  "service": [{"id": "engine-oil", "description": "Engine oil and filter", "interval_hours": 250, "interval_months": 12}]
}`

func TestProfilePutThatStrandsJobStateIs409UnlessConfirmed(t *testing.T) {
	_, item := scheduleFixture(t)
	impeller := maintenanceJobID(item.ID, "impeller")
	if rec := callHandler(t, acknowledgeMaintenanceRuleHandler, http.MethodPost, "/x"+today, `{"reason":"later"}`, idParam(impeller)); rec.Code != http.StatusOK {
		t.Fatalf("ack: %d", rec.Code)
	}

	rec := putProfile(t, "sched-a", scheduleProfileAWithoutImpeller, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Affected []profileGuardEntry `json:"affected"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Affected) != 1 || body.Affected[0].EquipmentID != item.ID || body.Affected[0].ServiceID != "impeller" || body.Affected[0].EquipmentName != "Main engine" {
		t.Fatalf("the refusal must list the affected item and job, got %+v", body.Affected)
	}
	if p, _ := engineProfiles(); len(p) == 0 || len(mustFindProfile(t, p, "sched-a").Service) != 2 {
		t.Fatalf("a refused edit must change nothing")
	}

	rec = putProfile(t, "sched-a", scheduleProfileAWithoutImpeller, "?confirm_removed=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("confirmed edit: %d %s", rec.Code, rec.Body.String())
	}
	resp := listRules(t, "&equipment="+item.ID)
	if viewByID(resp.Removed, impeller) == nil {
		t.Errorf("the confirmed removal should park the job: %+v", resp)
	}
}

func mustFindProfile(t *testing.T, profiles []engineProfile, id string) engineProfile {
	t.Helper()
	for _, p := range profiles {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("profile %s not loaded", id)
	return engineProfile{}
}

func TestProfilePutRemovingAServiceNoItemHoldsStateForIsFree(t *testing.T) {
	scheduleFixture(t)
	// Nothing was ever written to the impeller job: no state, no refusal.
	if rec := putProfile(t, "sched-a", scheduleProfileAWithoutImpeller, ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestProfilePutStateOnAnotherProfilesItemsDoesNotBlock(t *testing.T) {
	store, item := scheduleFixture(t)
	if rec := callHandler(t, acknowledgeMaintenanceRuleHandler, http.MethodPost, "/x"+today, `{"reason":"later"}`, idParam(maintenanceJobID(item.ID, "impeller"))); rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
	if _, err := store.db.Exec(`UPDATE equipment SET profile_id = 'sched-b' WHERE id = ?`, item.ID); err != nil {
		t.Fatal(err)
	}
	// The item now uses B; A can lose its impeller without touching it.
	if rec := putProfile(t, "sched-a", scheduleProfileAWithoutImpeller, ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestProfileDeleteWhileAnItemUsesItIs409(t *testing.T) {
	store, item := scheduleFixture(t)
	del := func(id string) *httptest.ResponseRecorder {
		return callHandler(t, deleteEquipmentProfileHandler, http.MethodDelete, "/api/equipment-profiles/"+id, "", idParam(id))
	}
	rec := del("sched-a")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Affected []profileGuardEntry `json:"affected"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Affected) != 1 || body.Affected[0].EquipmentID != item.ID {
		t.Fatalf("the refusal must list the items, got %s", rec.Body.String())
	}
	if ps, _ := engineProfiles(); len(ps) != 2 {
		t.Fatalf("a refused delete must keep the profile")
	}
	if _, err := store.db.Exec(`UPDATE equipment SET profile_id = '' WHERE id = ?`, item.ID); err != nil {
		t.Fatal(err)
	}
	if rec := del("sched-a"); rec.Code != http.StatusNoContent {
		t.Fatalf("an unreferenced profile deletes: %d %s", rec.Code, rec.Body.String())
	}
}

func TestProfileGuardAndMaintenanceWriteShareOneTransaction(t *testing.T) {
	store, item := scheduleFixture(t)
	// The guard reads maintenance_rules and equipment inside the profile
	// store's own transaction: it is the document store's connection.
	if globalProfileStore.db != store.db {
		t.Fatal("the profile store must run on the document store's connection")
	}
	id := maintenanceJobID(item.ID, "impeller")
	err := globalProfileStore.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO maintenance_rules (id, equipment_id, description, profile_service_id, created_at, updated_at) VALUES (?, ?, 'x', 'impeller', 0, 0)`, id, item.ID); err != nil {
			return err
		}
		// Visible to the guard in the same transaction, invisible outside it until commit.
		return guardProfileUpdateTx(tx, mustProfile(t, scheduleProfileAWithoutImpeller), false)
	})
	var guard *profileGuardError
	if !errors.As(err, &guard) {
		t.Fatalf("expected the guard to see the uncommitted row, got %v", err)
	}
	if n := countRuleRows(t, store); n != 0 {
		t.Fatalf("the transaction rolled back, got %d rows", n)
	}
}

func TestOverridesAlsoCarryPerItemStateWithoutMarkingOverrides(t *testing.T) {
	store, item := scheduleFixture(t)
	id := maintenanceJobID(item.ID, "engine-oil")
	put := func(body string) *httptest.ResponseRecorder {
		return callHandler(t, setMaintenanceRuleOverridesHandler, http.MethodPut, "/x"+today, body, idParam(id))
	}
	rec := put(`{"due_soon_hours": 30, "due_soon_months": 2, "fixed_due_date": "2027-01-31"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put: %d %s", rec.Code, rec.Body.String())
	}
	v := decodeRule(t, rec)
	if v.DueSoonHours == nil || *v.DueSoonHours != 30 || v.DueSoonMonths == nil || *v.DueSoonMonths != 2 || v.FixedDueDate != "2027-01-31" {
		t.Fatalf("state not written: %+v", v)
	}
	if len(v.OverriddenFields) != 0 {
		t.Fatalf("state is not an override, got %v", v.OverriddenFields)
	}
	if n := countRuleRows(t, store); n != 1 {
		t.Fatalf("row should be created lazily, got %d", n)
	}
	// Mixed with a real override: only that one is marked.
	v = decodeRule(t, put(`{"interval_hours": 400, "due_soon_hours": null}`))
	if strings.Join(v.OverriddenFields, ",") != "interval_hours" || v.DueSoonHours != nil || v.DueSoonMonths == nil {
		t.Fatalf("got %+v", v)
	}
	// null clears the fixed date; blank does too.
	v = decodeRule(t, put(`{"fixed_due_date": null}`))
	if v.FixedDueDate != "" {
		t.Fatalf("null should clear, got %q", v.FixedDueDate)
	}
	// Resetting an override leaves the state alone; state has no reset field.
	rec = callHandler(t, resetMaintenanceRuleOverrideHandler, http.MethodDelete, "/x"+today, "", idParam(id), paramPair{"field", "due_soon_months"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no reset for state fields: %d", rec.Code)
	}
	rec = callHandler(t, resetMaintenanceRuleOverrideHandler, http.MethodDelete, "/x"+today, "", idParam(id), paramPair{"field", "interval_hours"})
	if v := decodeRule(t, rec); v.DueSoonMonths == nil || *v.DueSoonMonths != 2 {
		t.Fatalf("reset must keep state, got %+v", v.DueSoonMonths)
	}
	for _, bad := range []string{`{"due_soon_hours": -1}`, `{"due_soon_months": -1}`, `{"fixed_due_date": "tomorrow"}`, `{"due_soon_hours": "x"}`} {
		if rec := put(bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", bad, rec.Code)
		}
	}
}

func TestUntouchedJobsHaveNullTimestamps(t *testing.T) {
	_, item := scheduleFixture(t)
	rec := callHandler(t, getMaintenanceRuleHandler, http.MethodGet, "/x"+today, "", idParam(maintenanceJobID(item.ID, "engine-oil")))
	var raw struct {
		Rule map[string]any `json:"rule"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if v, ok := raw.Rule["created_at"]; !ok || v != nil {
		t.Errorf("created_at should be null, got %v", v)
	}
	if v, ok := raw.Rule["updated_at"]; !ok || v != nil {
		t.Errorf("updated_at should be null, got %v", v)
	}
	rec = callHandler(t, acknowledgeMaintenanceRuleHandler, http.MethodPost, "/x"+today, `{"reason":"x"}`, idParam(maintenanceJobID(item.ID, "engine-oil")))
	if v := decodeRule(t, rec); v.UpdatedAt == nil || v.CreatedAt == nil {
		t.Errorf("a touched job has timestamps")
	}
}

func TestCreateProcedureNoteForARemovedJobWritesNoNote(t *testing.T) {
	store, item := scheduleFixture(t)
	impeller := maintenanceJobID(item.ID, "impeller")
	// Only a job with stored state outlives its service, so give it some.
	if rec := callHandler(t, completeMaintenanceRuleHandler, http.MethodPost, "/x"+today, `{"performed_at":"2026-06-01"}`, idParam(impeller)); rec.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", rec.Code, rec.Body.String())
	}
	replaceProfile(t, globalProfileStore, `{"schema_version":1,"kind":"engine","id":"sched-a","name":"Engine A","manufacturer":"Acme",
	  "service":[{"id":"engine-oil","description":"Engine oil and filter","interval_hours":250,"interval_months":12}]}`)
	before, err := store.List(nil, true, "", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	rec := callHandler(t, createMaintenanceProcedureNoteHandler, http.MethodPost, "/x"+today, "", idParam(impeller))
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d %s", rec.Code, rec.Body.String())
	}
	after, err := store.List(nil, true, "", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("a refused note must not be created: %d documents before, %d after", len(before), len(after))
	}
}
