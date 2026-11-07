package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// Open jobs (ADR 0269): a known problem that is not fixed yet. These tests
// pin the record's rules (what it takes to open one, what Done and Dropped
// write) at the command level and then the HTTP surface over them.

func openJobReq(title string) openJobRequest { return openJobRequest{Title: title} }

func mustOpenJob(t *testing.T, store *documentStore, today string, req openJobRequest) openJob {
	t.Helper()
	job, err := store.CreateOpenJobCmd(mustParseDate(t, today), req)
	if err != nil {
		t.Fatalf("CreateOpenJobCmd(%q): %v", req.Title, err)
	}
	return job
}

// ── open ────────────────────────────────────────────────────────────────

func TestCreateOpenJob_StoresTheJobOpenWithTodayAndManualSource(t *testing.T) {
	store := withTestDocumentStore(t)
	pump := mustCreateHandlerTestEquipment(t, "Shower pump")

	job := mustOpenJob(t, store, "2026-11-07", openJobRequest{
		EquipmentID: &pump.ID, Title: "  Leaks at the outlet ", Notes: "Drip, not a spray", BeforeOn: "2026-12-01",
	})

	if job.Status != openJobOpen || job.OpenedOn != "2026-11-07" || job.BeforeOn != "2026-12-01" {
		t.Fatalf("unexpected job: %+v", job)
	}
	if job.Title != "Leaks at the outlet" || job.Notes != "Drip, not a spray" {
		t.Fatalf("title and notes should be trimmed and kept: %+v", job)
	}
	if job.Source != openJobSourceManual || job.SourceLink != "" {
		t.Fatalf("a hand-made job has the manual source and no link: %+v", job)
	}
	if job.EquipmentName != "Shower pump" || job.PhotoIDs == nil || len(job.PhotoIDs) != 0 {
		t.Fatalf("expected the item's name and an empty photo list: %+v", job)
	}
}

func TestCreateOpenJob_NeedsNoItem(t *testing.T) {
	store := withTestDocumentStore(t)
	job := mustOpenJob(t, store, "2026-11-07", openJobReq("Renew the flare kit"))
	if job.EquipmentID != nil || job.EquipmentName != "" {
		t.Fatalf("expected no item: %+v", job)
	}
}

func TestCreateOpenJob_Refusals(t *testing.T) {
	store := withTestDocumentStore(t)
	missing := "no-such-item"
	tests := []struct {
		name      string
		req       openJobRequest
		wantField string
	}{
		{"blank title", openJobRequest{Title: "  "}, "title"},
		{"long title", openJobRequest{Title: strings.Repeat("x", openJobTitleMax+1)}, "title"},
		{"long notes", openJobRequest{Title: "x", Notes: strings.Repeat("x", openJobNotesMax+1)}, "notes"},
		{"malformed before date", openJobRequest{Title: "x", BeforeOn: "next week"}, "before_on"},
		{"impossible before date", openJobRequest{Title: "x", BeforeOn: "2026-02-31"}, "before_on"},
		{"unknown source", openJobRequest{Title: "x", Source: "rumour"}, "source"},
		{"link to another site", openJobRequest{Title: "x", Source: openJobSourceAlarm, SourceLink: "https://example.com/x"}, "source_link"},
		{"script link", openJobRequest{Title: "x", Source: openJobSourceAlarm, SourceLink: "javascript:alert(1)"}, "source_link"},
		{"protocol-relative link", openJobRequest{Title: "x", Source: openJobSourceAlarm, SourceLink: "//example.com/x"}, "source_link"},
		{"manual job with a link", openJobRequest{Title: "x", SourceLink: "/alarms"}, "source_link"},
		{"long label", openJobRequest{Title: "x", Source: openJobSourceAlarm, SourceLabel: strings.Repeat("x", openJobLabelMax+1)}, "source_label"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.CreateOpenJobCmd(mustParseDate(t, "2026-11-07"), tc.req)
			var verr *inventoryValidationError
			if !errors.As(err, &verr) || verr.Field != tc.wantField {
				t.Fatalf("expected a %s validation error, got %v", tc.wantField, err)
			}
		})
	}

	_, err := store.CreateOpenJobCmd(mustParseDate(t, "2026-11-07"), openJobRequest{Title: "x", EquipmentID: &missing})
	if !errors.Is(err, errEquipmentNotFound) {
		t.Fatalf("expected errEquipmentNotFound, got %v", err)
	}
	jobs, _ := store.ListOpenJobs(openJobFilter{Status: openJobStatusAny})
	if len(jobs) != 0 {
		t.Fatalf("a refused job writes nothing, got %+v", jobs)
	}
}

func TestCreateOpenJob_KeepsAnInAppLinkBack(t *testing.T) {
	store := withTestDocumentStore(t)
	job := mustOpenJob(t, store, "2026-11-07", openJobRequest{
		Title: "Bilge pump cycling", Source: openJobSourceAlarm, SourceLink: "/alarms?alarm=abc", SourceLabel: "Bilge high",
	})
	if job.Source != openJobSourceAlarm || job.SourceLink != "/alarms?alarm=abc" || job.SourceLabel != "Bilge high" {
		t.Fatalf("expected the source and link back to be kept: %+v", job)
	}
}

// ── edit ────────────────────────────────────────────────────────────────

func TestUpdateOpenJob_ReplacesTheEditableFieldsOnly(t *testing.T) {
	store := withTestDocumentStore(t)
	pump := mustCreateHandlerTestEquipment(t, "Shower pump")
	job := mustOpenJob(t, store, "2026-11-07", openJobRequest{Title: "Leaks", Source: openJobSourceMate})

	updated, err := store.UpdateOpenJobCmd(job.ID, openJobRequest{EquipmentID: &pump.ID, Title: "Leaks at the outlet", Notes: "n", BeforeOn: "2026-12-01"})
	if err != nil {
		t.Fatalf("UpdateOpenJobCmd: %v", err)
	}
	if updated.Title != "Leaks at the outlet" || updated.EquipmentID == nil || updated.BeforeOn != "2026-12-01" {
		t.Fatalf("unexpected update: %+v", updated)
	}
	if updated.Source != openJobSourceMate || updated.OpenedOn != "2026-11-07" {
		t.Fatalf("where it came from and when it opened do not change: %+v", updated)
	}
}

func TestUpdateOpenJob_ClosedJobIsRefused(t *testing.T) {
	store := withTestDocumentStore(t)
	job := mustOpenJob(t, store, "2026-11-07", openJobReq("Leaks"))
	if _, err := store.DropOpenJobCmd(mustParseDate(t, "2026-11-08"), job.ID, ""); err != nil {
		t.Fatalf("drop: %v", err)
	}
	_, err := store.UpdateOpenJobCmd(job.ID, openJobReq("Leaks again"))
	if !errors.Is(err, errOpenJobClosed) {
		t.Fatalf("expected errOpenJobClosed, got %v", err)
	}
}

func TestUpdateOpenJob_UnknownIDIsNotFound(t *testing.T) {
	store := withTestDocumentStore(t)
	if _, err := store.UpdateOpenJobCmd("nope", openJobReq("x")); !errors.Is(err, errOpenJobNotFound) {
		t.Fatalf("expected errOpenJobNotFound, got %v", err)
	}
}

// ── drop ────────────────────────────────────────────────────────────────

func TestDropOpenJob_ClosesItWithTheReasonAndTheDate(t *testing.T) {
	store := withTestDocumentStore(t)
	job := mustOpenJob(t, store, "2026-11-07", openJobReq("Leaks"))

	dropped, err := store.DropOpenJobCmd(mustParseDate(t, "2026-11-09"), job.ID, "  Fixed itself after a tighten ")
	if err != nil {
		t.Fatalf("DropOpenJobCmd: %v", err)
	}
	if dropped.Status != openJobDropped || dropped.ClosedOn != "2026-11-09" || dropped.DropReason != "Fixed itself after a tighten" {
		t.Fatalf("unexpected dropped job: %+v", dropped)
	}
	if dropped.LogEntryID != nil {
		t.Fatalf("dropping writes no service log line: %+v", dropped)
	}
	entries, _ := store.ListMaintenanceLogEntries(maintenanceLogFilter{})
	if len(entries) != 0 {
		t.Fatalf("dropping writes no service log line, got %+v", entries)
	}
}

func TestDropOpenJob_ReasonIsOptional(t *testing.T) {
	store := withTestDocumentStore(t)
	job := mustOpenJob(t, store, "2026-11-07", openJobReq("Leaks"))
	dropped, err := store.DropOpenJobCmd(mustParseDate(t, "2026-11-09"), job.ID, "")
	if err != nil || dropped.Status != openJobDropped || dropped.DropReason != "" {
		t.Fatalf("expected a plain drop, got %+v, %v", dropped, err)
	}
}

func TestDropOpenJob_TwiceIsRefused(t *testing.T) {
	store := withTestDocumentStore(t)
	job := mustOpenJob(t, store, "2026-11-07", openJobReq("Leaks"))
	if _, err := store.DropOpenJobCmd(mustParseDate(t, "2026-11-09"), job.ID, ""); err != nil {
		t.Fatalf("first drop: %v", err)
	}
	if _, err := store.DropOpenJobCmd(mustParseDate(t, "2026-11-10"), job.ID, ""); !errors.Is(err, errOpenJobClosed) {
		t.Fatalf("expected errOpenJobClosed, got %v", err)
	}
}

func TestDropOpenJob_LongReasonIsRefused(t *testing.T) {
	store := withTestDocumentStore(t)
	job := mustOpenJob(t, store, "2026-11-07", openJobReq("Leaks"))
	_, err := store.DropOpenJobCmd(mustParseDate(t, "2026-11-09"), job.ID, strings.Repeat("x", openJobNotesMax+1))
	var verr *inventoryValidationError
	if !errors.As(err, &verr) || verr.Field != "reason" {
		t.Fatalf("expected a reason validation error, got %v", err)
	}
}

// ── done ────────────────────────────────────────────────────────────────

func TestCompleteOpenJob_WritesOneServiceLogLineAndClosesTheJob(t *testing.T) {
	store := withTestDocumentStore(t)
	pump := mustCreateHandlerTestEquipment(t, "Shower pump")
	job := mustOpenJob(t, store, "2026-11-07", openJobRequest{EquipmentID: &pump.ID, Title: "Leaks at the outlet"})

	done, entry, err := store.CompleteOpenJobCmd(doneToday(t), job.ID, maintenanceLogEntryRequest{
		PerformedAt: "2026-11-10", Description: "New outlet fitting", Who: "Gavin", Kind: "improvement",
	})
	if err != nil {
		t.Fatalf("CompleteOpenJobCmd: %v", err)
	}
	if done.Status != openJobDone || done.ClosedOn != "2026-11-10" || done.LogEntryID == nil || *done.LogEntryID != entry.ID {
		t.Fatalf("expected the job done and linked to its log line: %+v", done)
	}
	if entry.Kind != "improvement" || entry.Description != "New outlet fitting" || entry.Who != "Gavin" ||
		entry.EquipmentID == nil || *entry.EquipmentID != pump.ID || entry.RuleID != nil {
		t.Fatalf("unexpected log line: %+v", entry)
	}
	entries, _ := store.ListMaintenanceLogEntries(maintenanceLogFilter{})
	if len(entries) != 1 {
		t.Fatalf("expected exactly one service log line, got %d", len(entries))
	}
}

func TestCompleteOpenJob_KindDefaultsToRepairAndNotesToTheTitle(t *testing.T) {
	store := withTestDocumentStore(t)
	pump := mustCreateHandlerTestEquipment(t, "Shower pump")
	job := mustOpenJob(t, store, "2026-11-07", openJobRequest{EquipmentID: &pump.ID, Title: "Leaks at the outlet"})

	_, entry, err := store.CompleteOpenJobCmd(doneToday(t), job.ID, maintenanceLogEntryRequest{PerformedAt: "2026-11-10"})
	if err != nil {
		t.Fatalf("CompleteOpenJobCmd: %v", err)
	}
	if entry.Kind != "repair" || entry.Description != "Leaks at the outlet" {
		t.Fatalf("expected a repair line titled for the job, got %+v", entry)
	}
}

func TestCompleteOpenJob_ConvertsGaugeHoursLikeAnyLogLine(t *testing.T) {
	store := withTestDocumentStore(t)
	engine := mustCreateHandlerTestEquipment(t, "Port engine")
	if _, err := store.RecordHourMeterReset(engine.ID, 1000, 0, "2025-06-01"); err != nil {
		t.Fatalf("RecordHourMeterReset: %v", err)
	}
	job := mustOpenJob(t, store, "2026-11-07", openJobRequest{EquipmentID: &engine.ID, Title: "Weeping hose"})

	hours := 20.0
	_, entry, err := store.CompleteOpenJobCmd(doneToday(t), job.ID, maintenanceLogEntryRequest{PerformedAt: "2026-11-10", Hours: &hours})
	if err != nil {
		t.Fatalf("CompleteOpenJobCmd: %v", err)
	}
	if entry.Hours == nil || *entry.Hours != 1020 {
		t.Fatalf("expected 20 gauge hours to be stored as true 1020, got %+v", entry.Hours)
	}
}

func TestCompleteOpenJob_HoursNeedAnItem(t *testing.T) {
	store := withTestDocumentStore(t)
	job := mustOpenJob(t, store, "2026-11-07", openJobReq("Renew flares"))
	hours := 20.0
	_, _, err := store.CompleteOpenJobCmd(doneToday(t), job.ID, maintenanceLogEntryRequest{PerformedAt: "2026-11-10", Hours: &hours})
	var verr *inventoryValidationError
	if !errors.As(err, &verr) || verr.Field != "hours" {
		t.Fatalf("expected an hours validation error, got %v", err)
	}
}

func TestCompleteOpenJob_JobWithoutAnItemStillLogs(t *testing.T) {
	store := withTestDocumentStore(t)
	job := mustOpenJob(t, store, "2026-11-07", openJobReq("Renew flares"))
	done, entry, err := store.CompleteOpenJobCmd(doneToday(t), job.ID, maintenanceLogEntryRequest{PerformedAt: "2026-11-10"})
	if err != nil || done.Status != openJobDone || entry.EquipmentID != nil {
		t.Fatalf("expected a done job and an item-less log line, got %+v, %+v, %v", done, entry, err)
	}
}

func TestCompleteOpenJob_BadKindIsRefusedAndWritesNothing(t *testing.T) {
	store := withTestDocumentStore(t)
	job := mustOpenJob(t, store, "2026-11-07", openJobReq("Leaks"))
	_, _, err := store.CompleteOpenJobCmd(doneToday(t), job.ID, maintenanceLogEntryRequest{PerformedAt: "2026-11-10", Kind: "chore"})
	var verr *inventoryValidationError
	if !errors.As(err, &verr) || verr.Field != "kind" {
		t.Fatalf("expected a kind validation error, got %v", err)
	}
	got, _ := store.GetOpenJob(job.ID)
	if got.Status != openJobOpen {
		t.Fatalf("a refused Done leaves the job open: %+v", got)
	}
}

func TestCompleteOpenJob_UnknownPartRollsEverythingBack(t *testing.T) {
	store := withTestDocumentStore(t)
	job := mustOpenJob(t, store, "2026-11-07", openJobReq("Leaks"))
	_, _, err := store.CompleteOpenJobCmd(doneToday(t), job.ID, maintenanceLogEntryRequest{
		PerformedAt: "2026-11-10", Parts: []maintenanceLogPartRequest{{EquipmentID: "no-such-part", Quantity: 1}},
	})
	if err == nil {
		t.Fatal("expected an error for an unknown part")
	}
	got, _ := store.GetOpenJob(job.ID)
	entries, _ := store.ListMaintenanceLogEntries(maintenanceLogFilter{})
	if got.Status != openJobOpen || len(entries) != 0 {
		t.Fatalf("a refused Done writes nothing, got job %+v and %d log lines", got, len(entries))
	}
}

func TestCompleteOpenJob_TwiceIsRefused(t *testing.T) {
	store := withTestDocumentStore(t)
	job := mustOpenJob(t, store, "2026-11-07", openJobReq("Leaks"))
	if _, _, err := store.CompleteOpenJobCmd(doneToday(t), job.ID, maintenanceLogEntryRequest{PerformedAt: "2026-11-10"}); err != nil {
		t.Fatalf("first Done: %v", err)
	}
	if _, _, err := store.CompleteOpenJobCmd(doneToday(t), job.ID, maintenanceLogEntryRequest{PerformedAt: "2026-11-11"}); !errors.Is(err, errOpenJobClosed) {
		t.Fatalf("expected errOpenJobClosed, got %v", err)
	}
	entries, _ := store.ListMaintenanceLogEntries(maintenanceLogFilter{})
	if len(entries) != 1 {
		t.Fatalf("a second Done must not write a second line, got %d", len(entries))
	}
}

func TestCompleteOpenJob_CarriesThePhotosOntoTheLogLine(t *testing.T) {
	store := withTestDocumentStore(t)
	job := mustOpenJob(t, store, "2026-11-07", openJobReq("Leaks"))
	doc := mustInsertPhotoDocument(t, store, "openjobphotosha", "leak.jpg")
	if err := store.AddOpenJobPhoto(job.ID, doc.ID); err != nil {
		t.Fatalf("AddOpenJobPhoto: %v", err)
	}

	_, entry, err := store.CompleteOpenJobCmd(doneToday(t), job.ID, maintenanceLogEntryRequest{PerformedAt: "2026-11-10"})
	if err != nil {
		t.Fatalf("CompleteOpenJobCmd: %v", err)
	}
	if len(entry.PhotoIDs) != 1 || entry.PhotoIDs[0] != doc.ID {
		t.Fatalf("expected the job's photo on the log line, got %+v", entry.PhotoIDs)
	}
	got, _ := store.GetOpenJob(job.ID)
	if len(got.PhotoIDs) != 1 {
		t.Fatalf("the closed job keeps its photo too: %+v", got.PhotoIDs)
	}
}

// ── not a rule, not an alarm ────────────────────────────────────────────

func TestOpenJobs_AreNotMaintenanceRules(t *testing.T) {
	store := withTestDocumentStore(t)
	pump := mustCreateHandlerTestEquipment(t, "Shower pump")
	mustOpenJob(t, store, "2026-11-07", openJobRequest{EquipmentID: &pump.ID, Title: "Leaks"})

	rules, err := store.ListMaintenanceRuleRows(maintenanceRuleFilter{IncludeStored: true})
	if err != nil || len(rules) != 0 {
		t.Fatalf("an open job is not a rule, got %+v, %v", rules, err)
	}
	sched, err := store.MaintenanceSchedule(maintenanceRuleFilter{IncludeStored: true})
	if err != nil || len(sched.Rules) != 0 {
		t.Fatalf("an open job is not on the schedule, got %+v, %v", sched.Rules, err)
	}
}

// ── list ────────────────────────────────────────────────────────────────

func TestListOpenJobs_DefaultsToOpenAndFiltersByItemSystemAndStored(t *testing.T) {
	store := withTestDocumentStore(t)
	pump := mustCreateHandlerTestEquipment(t, "Shower pump") // propulsion, from the helper
	gen, err := store.CreateEquipment(equipmentItem{Name: "Generator", Category: "mechanical", System: "electrical"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	old, err := store.CreateEquipment(equipmentItem{Name: "Old radar", Category: "electronics", System: "navigation", Status: "stored"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	a := mustOpenJob(t, store, "2026-11-01", openJobRequest{EquipmentID: &pump.ID, Title: "A"})
	b := mustOpenJob(t, store, "2026-11-02", openJobRequest{EquipmentID: &gen.ID, Title: "B"})
	mustOpenJob(t, store, "2026-11-03", openJobRequest{EquipmentID: &old.ID, Title: "C"})
	d := mustOpenJob(t, store, "2026-11-04", openJobReq("D"))
	if _, err := store.DropOpenJobCmd(mustParseDate(t, "2026-11-05"), d.ID, ""); err != nil {
		t.Fatalf("drop: %v", err)
	}

	titles := func(f openJobFilter) string {
		jobs, err := store.ListOpenJobs(f)
		if err != nil {
			t.Fatalf("ListOpenJobs: %v", err)
		}
		var out []string
		for _, j := range jobs {
			out = append(out, j.Title)
		}
		return strings.Join(out, ",")
	}

	if got := titles(openJobFilter{}); got != "A,B" {
		t.Fatalf("default: expected the open jobs of items not in storage, oldest first, got %q", got)
	}
	if got := titles(openJobFilter{IncludeStored: true}); got != "A,B,C" {
		t.Fatalf("include stored: got %q", got)
	}
	if got := titles(openJobFilter{EquipmentID: gen.ID}); got != "B" {
		t.Fatalf("by item: got %q", got)
	}
	if got := titles(openJobFilter{System: "electrical"}); got != "B" {
		t.Fatalf("by system: got %q", got)
	}
	if got := titles(openJobFilter{Status: openJobDropped}); got != "D" {
		t.Fatalf("dropped: got %q", got)
	}
	if got := titles(openJobFilter{Status: openJobStatusAny, IncludeStored: true}); got != "A,B,C,D" {
		t.Fatalf("any: got %q", got)
	}
	_ = a
	_ = b
}

func TestDeletingTheItemRemovesItsJobs(t *testing.T) {
	store := withTestDocumentStore(t)
	pump := mustCreateHandlerTestEquipment(t, "Shower pump")
	mustOpenJob(t, store, "2026-11-07", openJobRequest{EquipmentID: &pump.ID, Title: "Leaks"})
	if _, err := store.DeleteEquipment(pump.ID, false); err != nil {
		t.Fatalf("DeleteEquipment: %v", err)
	}
	jobs, _ := store.ListOpenJobs(openJobFilter{Status: openJobStatusAny, IncludeStored: true})
	if len(jobs) != 0 {
		t.Fatalf("expected the item's jobs to go with it, got %+v", jobs)
	}
}

// ── handlers ────────────────────────────────────────────────────────────

func decodeJob(t *testing.T, body []byte) openJob {
	t.Helper()
	var resp struct {
		Job openJob `json:"job"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, body)
	}
	return resp.Job
}

func TestOpenJobHandlers_RoundTrip(t *testing.T) {
	withTestDocumentStore(t)
	pump := mustCreateHandlerTestEquipment(t, "Shower pump")

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/open-jobs?today=2026-11-07",
		`{"equipment_id":"`+pump.ID+`","title":"Leaks at the outlet","before_on":"2026-12-01"}`, "")
	if err := createOpenJobHandler(c); err != nil {
		t.Fatalf("createOpenJobHandler: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	job := decodeJob(t, rec.Body.Bytes())

	c, rec = newDocumentEchoContext(http.MethodGet, "/api/inventory/open-jobs/"+job.ID, "", job.ID)
	if err := getOpenJobHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("getOpenJobHandler: %v, %d", err, rec.Code)
	}

	c, rec = newDocumentEchoContext(http.MethodPut, "/api/inventory/open-jobs/"+job.ID, `{"equipment_id":"`+pump.ID+`","title":"Leaks at the outlet fitting"}`, job.ID)
	if err := updateOpenJobHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("updateOpenJobHandler: %v, %d: %s", err, rec.Code, rec.Body.String())
	}
	if got := decodeJob(t, rec.Body.Bytes()); got.Title != "Leaks at the outlet fitting" || got.BeforeOn != "" {
		t.Fatalf("a PUT replaces the editable fields: %+v", got)
	}

	c, rec = newDocumentEchoContext(http.MethodGet, "/api/inventory/open-jobs?equipment="+pump.ID, "", "")
	if err := listOpenJobsHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("listOpenJobsHandler: %v, %d", err, rec.Code)
	}
	var list struct {
		Jobs []openJob `json:"jobs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Jobs) != 1 {
		t.Fatalf("expected one job, got %s (%v)", rec.Body.String(), err)
	}

	c, rec = newDocumentEchoContext(http.MethodPost, "/api/inventory/open-jobs/"+job.ID+"/done?today=2026-11-12", `{"performed_at":"2026-11-10","description":"New fitting"}`, job.ID)
	if err := completeOpenJobHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("completeOpenJobHandler: %v, %d: %s", err, rec.Code, rec.Body.String())
	}
	var done struct {
		Job   openJob             `json:"job"`
		Entry maintenanceLogEntry `json:"entry"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &done); err != nil || done.Job.Status != openJobDone || done.Entry.ID == "" {
		t.Fatalf("unexpected Done response: %s (%v)", rec.Body.String(), err)
	}

	c, rec = newDocumentEchoContext(http.MethodPost, "/api/inventory/open-jobs/"+job.ID+"/done?today=2026-11-12", `{"performed_at":"2026-11-10"}`, job.ID)
	if err := completeOpenJobHandler(c); err != nil {
		t.Fatalf("completeOpenJobHandler: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("a second Done is a 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOpenJobHandlers_CreateAndDropNeedTheOperatorsDate(t *testing.T) {
	store := withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/open-jobs", `{"title":"Leaks"}`, "")
	if err := createOpenJobHandler(c); err != nil {
		t.Fatalf("createOpenJobHandler: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 with no today, got %d: %s", rec.Code, rec.Body.String())
	}
	jobs, _ := store.ListOpenJobs(openJobFilter{})
	if len(jobs) != 0 {
		t.Fatalf("a refused create writes nothing, got %+v", jobs)
	}

	job := mustOpenJob(t, store, "2026-11-07", openJobReq("Leaks"))
	c, rec = newDocumentEchoContext(http.MethodPost, "/api/inventory/open-jobs/"+job.ID+"/drop", `{"reason":"gone"}`, job.ID)
	if err := dropOpenJobHandler(c); err != nil {
		t.Fatalf("dropOpenJobHandler: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 with no today, got %d: %s", rec.Code, rec.Body.String())
	}

	c, rec = newDocumentEchoContext(http.MethodPost, "/api/inventory/open-jobs/"+job.ID+"/drop?today=2026-11-09", `{"reason":"gone"}`, job.ID)
	if err := dropOpenJobHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("dropOpenJobHandler: %v, %d: %s", err, rec.Code, rec.Body.String())
	}
	if got := decodeJob(t, rec.Body.Bytes()); got.Status != openJobDropped || got.DropReason != "gone" {
		t.Fatalf("unexpected dropped job: %+v", got)
	}
}

func TestOpenJobHandlers_UnknownIDIs404(t *testing.T) {
	withTestDocumentStore(t)
	c, rec := newDocumentEchoContext(http.MethodGet, "/api/inventory/open-jobs/nope", "", "nope")
	if err := getOpenJobHandler(c); err != nil {
		t.Fatalf("getOpenJobHandler: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestOpenJobHandlers_ValidationNamesTheField(t *testing.T) {
	withTestDocumentStore(t)
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/open-jobs?today=2026-11-07", `{"title":""}`, "")
	if err := createOpenJobHandler(c); err != nil {
		t.Fatalf("createOpenJobHandler: %v", err)
	}
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "title") {
		t.Fatalf("expected a 400 naming title, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOpenJobPhotoHandlers_UploadAndRemove(t *testing.T) {
	store := withTestDocumentStore(t)
	job := mustOpenJob(t, store, "2026-11-07", openJobReq("Leaks"))

	c, rec := newInventoryPhotoUploadContext(t, job.ID, []documentUploadField{{name: "file", filename: "leak.jpg", content: []byte("\xFF\xD8\xFF\xE0fakejpegdata")}})
	if err := uploadOpenJobPhotoHandler(c); err != nil {
		t.Fatalf("uploadOpenJobPhotoHandler: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	got := decodeJob(t, rec.Body.Bytes())
	if len(got.PhotoIDs) != 1 {
		t.Fatalf("expected one photo, got %+v", got.PhotoIDs)
	}
	photoID := got.PhotoIDs[0]

	c, rec = newInventoryEquipmentIDContext(http.MethodDelete, "/api/inventory/open-jobs/"+job.ID+"/photos/"+photoID+"?delete=true", "", job.ID, photoID)
	if err := deleteOpenJobPhotoHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("deleteOpenJobPhotoHandler: %v, %d: %s", err, rec.Code, rec.Body.String())
	}
	if _, err := globalDocumentStore.Get(photoID); err == nil {
		t.Fatal("expected the orphaned photo document to be deleted")
	}
}

func TestOpenJobPhoto_SharedWithALogLineSurvivesTheJobUnlink(t *testing.T) {
	store := withTestDocumentStore(t)
	job := mustOpenJob(t, store, "2026-11-07", openJobReq("Leaks"))
	doc := mustInsertPhotoDocument(t, store, "openjobphotosha", "leak.jpg")
	if err := store.AddOpenJobPhoto(job.ID, doc.ID); err != nil {
		t.Fatalf("AddOpenJobPhoto: %v", err)
	}
	if _, _, err := store.CompleteOpenJobCmd(doneToday(t), job.ID, maintenanceLogEntryRequest{PerformedAt: "2026-11-10"}); err != nil {
		t.Fatalf("CompleteOpenJobCmd: %v", err)
	}
	if err := store.RemoveOpenJobPhoto(job.ID, doc.ID); err != nil {
		t.Fatalf("RemoveOpenJobPhoto: %v", err)
	}
	deletable, err := store.OpenJobPhotoDeletableAsOrphan(doc.ID)
	if err != nil || deletable {
		t.Fatalf("the service log line still shows the photo, so it is not deletable: %v, %v", deletable, err)
	}
}

func TestOpenJobPhoto_StillLinkedToAJobIsNotDeletableFromTheLogSide(t *testing.T) {
	store := withTestDocumentStore(t)
	job := mustOpenJob(t, store, "2026-11-07", openJobReq("Leaks"))
	doc := mustInsertPhotoDocument(t, store, "openjobphotosha", "leak.jpg")
	if err := store.AddOpenJobPhoto(job.ID, doc.ID); err != nil {
		t.Fatalf("AddOpenJobPhoto: %v", err)
	}
	deletable, err := store.DocumentDeletableAsOrphanPhoto(doc.ID)
	if err != nil || deletable {
		t.Fatalf("a photo an open job shows is not an orphan: %v, %v", deletable, err)
	}
}
