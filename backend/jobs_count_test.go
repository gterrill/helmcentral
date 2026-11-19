package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

// GET /api/inventory/jobs/count (ADR 0293): the Jobs row's badge. It counts the
// open jobs plus the services overdue or due soon that are not acknowledged,
// and says when an item's schedule could not be read.

type jobsCountResponse struct {
	Count      int  `json:"count"`
	Incomplete bool `json:"incomplete"`
}

func getJobsCount(t *testing.T, target string) (int, jobsCountResponse) {
	t.Helper()
	rec := callHandler(t, jobsCountHandler, http.MethodGet, target, "")
	var resp jobsCountResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal %s: %v", rec.Body.String(), err)
		}
	}
	return rec.Code, resp
}

func TestJobsCount_CountsOpenJobsOverdueAndDueSoonServices(t *testing.T) {
	store := withTestDocumentStore(t)
	useBoatDay(t, "2026-07-01")
	engine := mustCreateHandlerTestEquipment(t, "Generator")

	mustOpenJob(t, store, "2026-06-01", openJobRequest{EquipmentID: &engine.ID, Title: "Leaking hatch"})
	closed := mustOpenJob(t, store, "2026-06-02", openJobReq("Dropped one"))
	if _, err := store.DropOpenJobCmd(mustParseDate(t, "2026-06-03"), closed.ID, ""); err != nil {
		t.Fatal(err)
	}

	months := 6
	overdue, err := store.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &engine.ID, Description: "Anode check", IntervalMonths: &months})
	if err != nil {
		t.Fatal(err)
	}
	dueSoon, err := store.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &engine.ID, Description: "Belt check", IntervalMonths: &months})
	if err != nil {
		t.Fatal(err)
	}
	fine, err := store.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &engine.ID, Description: "Filter check", IntervalMonths: &months})
	if err != nil {
		t.Fatal(err)
	}
	for id, at := range map[string]string{overdue.ID: "2025-12-01", dueSoon.ID: "2026-01-20", fine.ID: "2026-06-20"} {
		d := at
		if _, err := store.SetMaintenanceRuleLastDone(id, &d, nil); err != nil {
			t.Fatal(err)
		}
	}

	code, got := getJobsCount(t, "/api/inventory/jobs/count")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if got.Count != 3 || got.Incomplete {
		t.Fatalf("expected 1 open job + 1 overdue + 1 due soon = 3, complete; got %+v", got)
	}
}

func TestJobsCount_LeavesOutAcknowledgedServices(t *testing.T) {
	store := withTestDocumentStore(t)
	useBoatDay(t, "2026-07-01")
	engine := mustCreateHandlerTestEquipment(t, "Generator")
	months := 6
	rule, err := store.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &engine.ID, Description: "Anode check", IntervalMonths: &months})
	if err != nil {
		t.Fatal(err)
	}
	at := "2025-12-01"
	if _, err := store.SetMaintenanceRuleLastDone(rule.ID, &at, nil); err != nil {
		t.Fatal(err)
	}
	if _, got := getJobsCount(t, "/api/inventory/jobs/count"); got.Count != 1 {
		t.Fatalf("expected the overdue service to count, got %+v", got)
	}
	if _, err := store.AcknowledgeMaintenanceRuleCmd(rule.ID, "waiting on parts"); err != nil {
		t.Fatal(err)
	}
	if _, got := getJobsCount(t, "/api/inventory/jobs/count"); got.Count != 0 {
		t.Fatalf("an acknowledged service is deferred and must not count, got %+v", got)
	}
}

func TestJobsCount_LeavesOutStoredItems(t *testing.T) {
	store := withTestDocumentStore(t)
	useBoatDay(t, "2026-07-01")
	old, err := store.CreateEquipment(equipmentItem{Name: "Old radar", Category: "electronics", System: "navigation", Status: "stored"})
	if err != nil {
		t.Fatal(err)
	}
	mustOpenJob(t, store, "2026-06-01", openJobRequest{EquipmentID: &old.ID, Title: "Replace fuse"})
	months := 6
	rule, err := store.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &old.ID, Description: "Clean", IntervalMonths: &months})
	if err != nil {
		t.Fatal(err)
	}
	at := "2025-12-01"
	if _, err := store.SetMaintenanceRuleLastDone(rule.ID, &at, nil); err != nil {
		t.Fatal(err)
	}
	if _, got := getJobsCount(t, "/api/inventory/jobs/count"); got.Count != 0 || got.Incomplete {
		t.Fatalf("a stored item's job and service must not count, got %+v", got)
	}
}

func TestJobsCount_IncompleteWhenAnItemsProfileCannotBeRead(t *testing.T) {
	store, item := scheduleFixture(t)
	useBoatDay(t, "2026-07-01")
	mustOpenJob(t, store, "2026-06-01", openJobReq("Leaking hatch"))
	if _, got := getJobsCount(t, "/api/inventory/jobs/count"); got.Incomplete {
		t.Fatalf("a readable schedule is complete, got %+v", got)
	}
	if _, err := store.db.Exec(`UPDATE equipment SET profile_id = 'gone' WHERE id = ?`, item.ID); err != nil {
		t.Fatal(err)
	}
	_, got := getJobsCount(t, "/api/inventory/jobs/count")
	if !got.Incomplete || got.Count != 1 {
		t.Fatalf("expected the open job counted and incomplete true, got %+v", got)
	}
}

// The boat's day is the server's own: no boat day is a 503, never a 0, and a
// client-supplied ?today= does not stand in for it (ADR 0230).
func TestJobsCount_NoBoatDayAnswers503(t *testing.T) {
	withTestDocumentStore(t)
	useNoBoatDay(t)
	if code, _ := getJobsCount(t, "/api/inventory/jobs/count?today=2026-07-01"); code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 with no boat day, got %d", code)
	}
}

func TestJobsCount_IgnoresAClientSuppliedToday(t *testing.T) {
	store := withTestDocumentStore(t)
	useBoatDay(t, "2026-06-15")
	engine := mustCreateHandlerTestEquipment(t, "Generator")
	months := 6
	rule, err := store.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &engine.ID, Description: "Anode check", IntervalMonths: &months})
	if err != nil {
		t.Fatal(err)
	}
	at := "2026-02-15" // due 2026-08-15: fine on the boat's day, overdue on the client's
	if _, err := store.SetMaintenanceRuleLastDone(rule.ID, &at, nil); err != nil {
		t.Fatal(err)
	}
	if _, got := getJobsCount(t, "/api/inventory/jobs/count?today=2030-01-01"); got.Count != 0 {
		t.Fatalf("the boat's day decides, not the query; got %+v", got)
	}
}
