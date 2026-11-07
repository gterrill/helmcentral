package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Fixes from the review of the Open jobs PR (ADR 0269): the date Done may
// take, what a closed job's photos block, an item's job photos on delete, and
// the boat-wide service log of lines with no item.

func doneToday(t *testing.T) time.Time { return mustParseDate(t, "2026-11-12") }

func TestCompleteOpenJob_DateMustBeRealAfterOpeningAndNotInTheFuture(t *testing.T) {
	store := withTestDocumentStore(t)
	job := mustOpenJob(t, store, "2026-11-07", openJobReq("Leaks"))
	for name, date := range map[string]string{
		"not a day":     "2026-02-31",
		"before opened": "2026-11-06",
		"in the future": "2026-11-13",
	} {
		_, _, err := store.CompleteOpenJobCmd(doneToday(t), job.ID, maintenanceLogEntryRequest{PerformedAt: date})
		var verr *inventoryValidationError
		if !errors.As(err, &verr) || verr.Field != "performed_at" {
			t.Errorf("%s (%s): expected a performed_at validation error, got %v", name, date, err)
		}
	}
	got, _ := store.GetOpenJob(job.ID)
	entries, _ := store.ListMaintenanceLogEntries(maintenanceLogFilter{})
	if got.Status != openJobOpen || len(entries) != 0 {
		t.Fatalf("a refused Done writes nothing, got %+v and %d lines", got, len(entries))
	}
	if _, _, err := store.CompleteOpenJobCmd(doneToday(t), job.ID, maintenanceLogEntryRequest{PerformedAt: "2026-11-07"}); err != nil {
		t.Fatalf("the opening day is allowed: %v", err)
	}
}

func TestOpenJobHandlers_DoneNeedsTheOperatorsDateAndAnswers400ForABadOne(t *testing.T) {
	store := withTestDocumentStore(t)
	job := mustOpenJob(t, store, "2026-11-07", openJobReq("Leaks"))

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/open-jobs/"+job.ID+"/done", `{"performed_at":"2026-11-10"}`, job.ID)
	if err := completeOpenJobHandler(c); err != nil || rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "today") {
		t.Fatalf("expected a 400 naming today, got %v, %d: %s", err, rec.Code, rec.Body.String())
	}
	c, rec = newDocumentEchoContext(http.MethodPost, "/api/inventory/open-jobs/"+job.ID+"/done?today=2026-11-12", `{"performed_at":"2026-02-31"}`, job.ID)
	if err := completeOpenJobHandler(c); err != nil || rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "performed_at") {
		t.Fatalf("expected a 400 naming performed_at, got %v, %d: %s", err, rec.Code, rec.Body.String())
	}
}

func TestMaintenanceLogEntry_ImpossibleDateIs400(t *testing.T) {
	withTestDocumentStore(t)
	engine := mustCreateHandlerTestEquipment(t, "Port engine")
	body := `{"equipment_id":"` + engine.ID + `","performed_at":"2026-02-31","kind":"repair","hours":5}`
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/log", body, "")
	if err := createMaintenanceLogEntryHandler(c); err != nil || rec.Code != http.StatusBadRequest {
		t.Fatalf("expected a 400, got %v, %d: %s", err, rec.Code, rec.Body.String())
	}
}

func TestOpenJobPhoto_DoneJobDoesNotBlockDeletingTheLogPhoto(t *testing.T) {
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
	if err := store.RemoveMaintenanceLogPhoto(entry.ID, doc.ID); err != nil {
		t.Fatalf("RemoveMaintenanceLogPhoto: %v", err)
	}
	deletable, err := store.MaintenanceLogPhotoDeletableAsOrphan(doc.ID)
	if err != nil || !deletable {
		t.Fatalf("a done job's photo link must not keep the document alive: %v, %v", deletable, err)
	}
}

func TestOpenJobPhoto_DroppedJobDoesNotBlockDeletion(t *testing.T) {
	store := withTestDocumentStore(t)
	job := mustOpenJob(t, store, "2026-11-07", openJobReq("Leaks"))
	doc := mustInsertPhotoDocument(t, store, "droppedjobsha", "leak.jpg")
	if err := store.AddOpenJobPhoto(job.ID, doc.ID); err != nil {
		t.Fatalf("AddOpenJobPhoto: %v", err)
	}
	if _, err := store.DropOpenJobCmd(mustParseDate(t, "2026-11-08"), job.ID, ""); err != nil {
		t.Fatalf("DropOpenJobCmd: %v", err)
	}
	deletable, err := store.DocumentDeletableAsOrphanPhoto(doc.ID)
	if err != nil || !deletable {
		t.Fatalf("a dropped job's photo is an orphan: %v, %v", deletable, err)
	}
}

func TestDeletingTheItemOffersItsJobPhotosForDeletion(t *testing.T) {
	store := withTestDocumentStore(t)
	pump := mustCreateHandlerTestEquipment(t, "Shower pump")
	other := mustCreateHandlerTestEquipment(t, "Bilge pump")
	own := mustInsertPhotoDocument(t, store, "ownjobphoto", "own.jpg")
	shared := mustInsertPhotoDocument(t, store, "sharedjobphoto", "shared.jpg")

	job := mustOpenJob(t, store, "2026-11-07", openJobRequest{EquipmentID: &pump.ID, Title: "Leaks"})
	otherJob := mustOpenJob(t, store, "2026-11-07", openJobRequest{EquipmentID: &other.ID, Title: "Weeps"})
	for _, link := range []struct{ job, doc string }{{job.ID, own.ID}, {job.ID, shared.ID}, {otherJob.ID, shared.ID}} {
		if err := store.AddOpenJobPhoto(link.job, link.doc); err != nil {
			t.Fatalf("AddOpenJobPhoto: %v", err)
		}
	}

	got, err := store.GetEquipment(pump.ID)
	if err != nil {
		t.Fatalf("GetEquipment: %v", err)
	}
	if len(got.ExclusivePhotoIDs) != 1 || got.ExclusivePhotoIDs[0] != own.ID {
		t.Fatalf("expected only the photo no other job shows, got %+v", got.ExclusivePhotoIDs)
	}
	if _, err := store.DeleteEquipment(pump.ID, true); err != nil {
		t.Fatalf("DeleteEquipment: %v", err)
	}
	if _, err := store.Get(own.ID); err == nil {
		t.Fatal("expected the item's job photo to be deleted with it")
	}
	if _, err := store.Get(shared.ID); err != nil {
		t.Fatalf("a photo another item's job shows must survive: %v", err)
	}
}

func TestListMaintenance_ManyOpenJobsDoNotCrowdOutEveryRule(t *testing.T) {
	deps, store := maintenanceToolDeps(t)
	withGlobalSnapshot(t, snapshotWithSelfValues(map[string]any{}))
	eq := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical"})
	months := 6
	for i := 0; i < 30; i++ {
		mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &eq.ID, Description: "Rule " + strings.Repeat("x", i+1), IntervalMonths: &months})
	}
	for i := 0; i < 80; i++ {
		mustOpenJob(t, store, "2026-09-01", openJobRequest{Title: "Job " + strings.Repeat("y", i+1), Notes: strings.Repeat("Long note ", 20)})
	}
	var result assistantListMaintenanceResult
	if err := json.Unmarshal([]byte(runMaintenanceTool(t, deps, "list_maintenance", `{}`)), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !result.Truncated || len(result.Rules) < 5 || len(result.OpenJobs) < 5 {
		t.Fatalf("expected both lists trimmed together, got truncated=%v rules=%d jobs=%d", result.Truncated, len(result.Rules), len(result.OpenJobs))
	}
}

func TestListMaintenanceLogEntries_NoItemFilterListsLinesWithNoItem(t *testing.T) {
	store := withTestDocumentStore(t)
	pump := mustCreateHandlerTestEquipment(t, "Shower pump")
	onItem := mustOpenJob(t, store, "2026-11-07", openJobRequest{EquipmentID: &pump.ID, Title: "Leaks"})
	noItem := mustOpenJob(t, store, "2026-11-07", openJobReq("Renew flares"))
	for _, id := range []string{onItem.ID, noItem.ID} {
		if _, _, err := store.CompleteOpenJobCmd(doneToday(t), id, maintenanceLogEntryRequest{PerformedAt: "2026-11-10"}); err != nil {
			t.Fatalf("CompleteOpenJobCmd: %v", err)
		}
	}
	entries, err := store.ListMaintenanceLogEntries(maintenanceLogFilter{NoItem: true})
	if err != nil || len(entries) != 1 || entries[0].EquipmentID != nil || entries[0].Description != "Renew flares" {
		t.Fatalf("expected only the item-less line, got %+v (%v)", entries, err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/inventory/maintenance/log?no_item=true", "", "")
	if err := listMaintenanceLogEntriesHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("listMaintenanceLogEntriesHandler: %v, %d", err, rec.Code)
	}
	var resp struct {
		Entries []maintenanceLogEntry `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || len(resp.Entries) != 1 {
		t.Fatalf("expected one entry from the endpoint, got %s (%v)", rec.Body.String(), err)
	}
}
