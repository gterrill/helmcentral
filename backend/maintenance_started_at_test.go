package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// A repair can say when the fault first showed ("Started showing"). The learned
// normal leaves the engine's readings from then until the repair out of what it
// learns (ADR 0276, Feedback).

func logEntryFrom(t *testing.T, body []byte) maintenanceLogEntry {
	t.Helper()
	var out struct {
		Entry maintenanceLogEntry `json:"entry"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", body, err)
	}
	return out.Entry
}

func postLog(t *testing.T, body string) (int, []byte) {
	t.Helper()
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/log", body, "")
	if err := createMaintenanceLogEntryHandler(c); err != nil {
		t.Fatalf("createMaintenanceLogEntryHandler: %v", err)
	}
	return rec.Code, rec.Body.Bytes()
}

func TestMaintenanceLog_ARepairKeepsWhenTheFaultStartedShowing(t *testing.T) {
	withTestDocumentStore(t)
	engine := mustCreateEngineItem(t, "Port engine", "port")
	drainWake(normalTrainWake)

	code, body := postLog(t, `{"equipment_id":"`+engine.ID+`","performed_at":"2026-11-08","kind":"repair","started_at":"2026-11-02","description":"New oil pressure sender"}`)
	if code != http.StatusCreated {
		t.Fatalf("code %d: %s", code, body)
	}
	entry := logEntryFrom(t, body)
	if entry.StartedAt == nil || *entry.StartedAt != "2026-11-02" {
		t.Fatalf("started_at = %v", entry.StartedAt)
	}
	if !drainWake(normalTrainWake) {
		t.Fatal("a repair with a fault range changes what is learned, so the trainer is woken")
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/inventory/maintenance/log/"+entry.ID, "", entry.ID)
	if err := getMaintenanceLogEntryHandler(c); err != nil {
		t.Fatal(err)
	}
	if got := logEntryFrom(t, rec.Body.Bytes()); got.StartedAt == nil || *got.StartedAt != "2026-11-02" {
		t.Fatalf("read back started_at = %v", got.StartedAt)
	}

	// Changing the date and then clearing it each wake the trainer.
	update := func(startedAt string) maintenanceLogEntry {
		t.Helper()
		b := `{"performed_at":"2026-11-08","kind":"repair","description":"New sender","started_at":"` + startedAt + `"}`
		c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/maintenance/log/"+entry.ID, b, entry.ID)
		if err := updateMaintenanceLogEntryHandler(c); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("update: %v code %d: %s", err, rec.Code, rec.Body)
		}
		return logEntryFrom(t, rec.Body.Bytes())
	}
	if got := update("2026-11-04"); got.StartedAt == nil || *got.StartedAt != "2026-11-04" || !drainWake(normalTrainWake) {
		t.Fatalf("changed: %v", got.StartedAt)
	}
	if got := update(""); got.StartedAt != nil || !drainWake(normalTrainWake) {
		t.Fatalf("a cleared date is null and wakes the trainer: %v", got.StartedAt)
	}

	// Deleting an entry that had a range wakes it too.
	update("2026-11-04")
	drainWake(normalTrainWake)
	c, rec = newDocumentEchoContext(http.MethodDelete, "/api/inventory/maintenance/log/"+entry.ID, "", entry.ID)
	if err := deleteMaintenanceLogEntryHandler(c); err != nil || rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %v code %d", err, rec.Code)
	}
	if !drainWake(normalTrainWake) {
		t.Fatal("deleting a repair with a fault range wakes the trainer")
	}
}

func TestMaintenanceLog_AnEntryWithoutAStartHasNoneAndIsNotARepairRange(t *testing.T) {
	store := withTestDocumentStore(t)
	engine := mustCreateEngineItem(t, "Port engine", "port")
	other := mustCreateHandlerTestEquipment(t, "Generator")
	drainWake(normalTrainWake)
	code, body := postLog(t, `{"equipment_id":"`+engine.ID+`","performed_at":"2026-11-08","kind":"repair"}`)
	if code != http.StatusCreated {
		t.Fatalf("code %d: %s", code, body)
	}
	if entry := logEntryFrom(t, body); entry.StartedAt != nil {
		t.Fatalf("started_at = %v, want null", *entry.StartedAt)
	}
	if !strings.Contains(string(body), `"started_at":null`) {
		t.Fatalf("the key is always there: %s", body)
	}
	if drainWake(normalTrainWake) {
		t.Fatal("an entry with no start date changes nothing the learned normal reads")
	}
	postLog(t, `{"equipment_id":"`+other.ID+`","performed_at":"2026-11-08","kind":"repair","started_at":"2026-11-03","description":"Belt"}`)
	postLog(t, `{"equipment_id":"`+other.ID+`","performed_at":"2026-11-08","kind":"maintenance"}`)
	repairs, err := store.ListMaintenanceRepairs()
	if err != nil || len(repairs) != 1 || repairs[0].EquipmentID != other.ID || repairs[0].StartedAt != "2026-11-03" ||
		repairs[0].PerformedAt != "2026-11-08" || repairs[0].Description != "Belt" {
		t.Fatalf("only a repair with a start counts: %+v err %v", repairs, err)
	}
}

func TestMaintenanceLog_StartedShowingIsValidated(t *testing.T) {
	withTestDocumentStore(t)
	engine := mustCreateEngineItem(t, "Port engine", "port")
	cases := []struct{ name, extra, field string }{
		{"not a date", `"kind":"repair","started_at":"last week"`, "started_at"},
		{"not a day on the calendar", `"kind":"repair","started_at":"2026-02-30"`, "started_at"},
		{"after the repair", `"kind":"repair","started_at":"2026-11-09"`, "started_at"},
		{"on a service", `"kind":"maintenance","started_at":"2026-11-02"`, "started_at"},
		{"on an improvement", `"kind":"improvement","started_at":"2026-11-02"`, "started_at"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, body := postLog(t, `{"equipment_id":"`+engine.ID+`","performed_at":"2026-11-08",`+c.extra+`}`)
			if code != http.StatusBadRequest || !strings.Contains(string(body), c.field) {
				t.Fatalf("code %d: %s", code, body)
			}
		})
	}
	if code, body := postLog(t, `{"equipment_id":"`+engine.ID+`","performed_at":"2026-02-30","kind":"repair"}`); code != http.StatusBadRequest || !strings.Contains(string(body), "performed_at") {
		t.Fatalf("a performed_at that is no day on the calendar: code %d: %s", code, body)
	}
	// The same day is allowed: a fault noticed and mended in one day.
	if code, body := postLog(t, `{"equipment_id":"`+engine.ID+`","performed_at":"2026-11-08","kind":"repair","started_at":"2026-11-08"}`); code != http.StatusCreated {
		t.Fatalf("code %d: %s", code, body)
	}
}

func TestMaintenanceLog_AnOpenJobDoneAsARepairKeepsItsStart(t *testing.T) {
	store := withTestDocumentStore(t)
	engine := mustCreateEngineItem(t, "Port engine", "port")
	job := mustOpenJob(t, store, "2026-11-07", openJobRequest{EquipmentID: &engine.ID, Title: "Oil pressure reads low"})
	drainWake(normalTrainWake)
	_, entry, err := store.CompleteOpenJobCmd(job.ID, maintenanceLogEntryRequest{PerformedAt: "2026-11-10", Kind: "repair", StartedAt: "2026-11-05"})
	if err != nil {
		t.Fatal(err)
	}
	if entry.StartedAt == nil || *entry.StartedAt != "2026-11-05" || !drainWake(normalTrainWake) {
		t.Fatalf("started_at = %v", entry.StartedAt)
	}

	// A job closed without a start changes nothing the learned normal reads.
	plainJob := mustOpenJob(t, store, "2026-11-07", openJobRequest{EquipmentID: &engine.ID, Title: "Squeak"})
	drainWake(normalTrainWake)
	if _, _, err := store.CompleteOpenJobCmd(plainJob.ID, maintenanceLogEntryRequest{PerformedAt: "2026-11-10", Kind: "repair"}); err != nil {
		t.Fatal(err)
	}
	if drainWake(normalTrainWake) {
		t.Fatal("no start, no wake")
	}
}

func TestMaintenanceLog_CompletingARuleTakesNoStart(t *testing.T) {
	withTestDocumentStore(t)
	engine := mustCreateEngineItem(t, "Port engine", "port")
	rule, err := globalDocumentStore.CreateMaintenanceRuleCmd(maintenanceRuleRequest{EquipmentID: &engine.ID, Description: "Oil change", IntervalMonths: ptr(6)})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = globalDocumentStore.CompleteMaintenanceRuleCmd(rule.ID, maintenanceLogEntryRequest{PerformedAt: "2026-11-08", StartedAt: "2026-11-02"})
	if err == nil || !strings.Contains(err.Error(), "started_at") {
		t.Fatalf("a service has no fault to date: %v", err)
	}
}

func TestMaintenanceLog_OnlyAnEntryThatHasOrHadAStartWakesTheTrainer(t *testing.T) {
	store := withTestDocumentStore(t)
	engine := mustCreateEngineItem(t, "Port engine", "port")
	_, body := postLog(t, `{"equipment_id":"`+engine.ID+`","performed_at":"2026-11-08","kind":"repair"}`)
	plain := logEntryFrom(t, body)
	update := func(id, startedAt string) {
		t.Helper()
		b := `{"performed_at":"2026-11-08","kind":"repair","description":"x","started_at":"` + startedAt + `"}`
		c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/maintenance/log/"+id, b, id)
		if err := updateMaintenanceLogEntryHandler(c); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("update: %v code %d: %s", err, rec.Code, rec.Body)
		}
	}
	drainWake(normalTrainWake)
	update(plain.ID, "")
	if drainWake(normalTrainWake) {
		t.Fatal("editing an entry that never had a start must not wake the trainer")
	}
	update(plain.ID, "2026-11-01")
	if !drainWake(normalTrainWake) {
		t.Fatal("giving an entry a start wakes the trainer")
	}
	update(plain.ID, "")
	if !drainWake(normalTrainWake) {
		t.Fatal("taking a start away wakes the trainer")
	}

	if err := store.DeleteMaintenanceLogEntry(plain.ID); err != nil {
		t.Fatal(err)
	}
	if drainWake(normalTrainWake) {
		t.Fatal("deleting an entry with no start must not wake the trainer")
	}
	if err := store.DeleteMaintenanceLogEntry(plain.ID); !errors.Is(err, errMaintenanceLogEntryNotFound) {
		t.Fatalf("deleting a missing entry is not found: %v", err)
	}
	c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/maintenance/log/nope", `{"performed_at":"2026-11-08","kind":"repair"}`, "nope")
	if err := updateMaintenanceLogEntryHandler(c); err != nil || rec.Code != http.StatusNotFound {
		t.Fatalf("updating a missing entry is a 404: %v %d", err, rec.Code)
	}
}

// mustCreateEngineItem is an equipment item that owns propulsion.<instance>, which
// is what makes it a configured engine.
func mustCreateEngineItem(t *testing.T, name, instance string) equipmentItem {
	t.Helper()
	item := mustCreateHandlerTestEquipment(t, name)
	item.SignalKSource = "propulsion." + instance
	updated, err := globalDocumentStore.UpdateEquipment(item.ID, item)
	if err != nil {
		t.Fatalf("UpdateEquipment: %v", err)
	}
	return updated
}

// Only a repair on a configured engine's item is a fault range the learned normal
// reads, so a repair on the watermaker wakes nothing.
func TestMaintenanceLog_ARepairOnAnItemThatIsNotAnEngineDoesNotWakeTheTrainer(t *testing.T) {
	store := withTestDocumentStore(t)
	watermaker := mustCreateHandlerTestEquipment(t, "Watermaker")
	drainWake(normalTrainWake)
	code, body := postLog(t, `{"equipment_id":"`+watermaker.ID+`","performed_at":"2026-11-08","kind":"repair","started_at":"2026-11-01"}`)
	if code != http.StatusCreated {
		t.Fatalf("code %d: %s", code, body)
	}
	entry := logEntryFrom(t, body)
	b := `{"performed_at":"2026-11-08","kind":"repair","started_at":"2026-11-02"}`
	c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/maintenance/log/"+entry.ID, b, entry.ID)
	if err := updateMaintenanceLogEntryHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("update: %v %d", err, rec.Code)
	}
	if err := store.DeleteMaintenanceLogEntry(entry.ID); err != nil {
		t.Fatal(err)
	}
	if drainWake(normalTrainWake) {
		t.Fatal("a repair on the watermaker is nothing the learned normal reads")
	}
}

// A kind that is no kind gets the kind error, whatever else is wrong with the entry.
func TestMaintenanceLog_ABadKindIsTheFirstThingSaid(t *testing.T) {
	withTestDocumentStore(t)
	engine := mustCreateEngineItem(t, "Port engine", "port")
	code, body := postLog(t, `{"equipment_id":"`+engine.ID+`","performed_at":"2026-11-08","kind":"bogus","started_at":"2026-11-01"}`)
	if code != http.StatusBadRequest || !strings.Contains(string(body), `"kind"`) || strings.Contains(string(body), "started_at only applies") {
		t.Fatalf("code %d: %s", code, body)
	}
	entry := logEntryFrom(t, func() []byte {
		_, b := postLog(t, `{"equipment_id":"`+engine.ID+`","performed_at":"2026-11-08","kind":"repair"}`)
		return b
	}())
	b := `{"performed_at":"2026-11-08","kind":"bogus","started_at":"2026-11-01"}`
	c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/maintenance/log/"+entry.ID, b, entry.ID)
	if err := updateMaintenanceLogEntryHandler(c); err != nil || rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"kind"`) {
		t.Fatalf("update: %v %d %s", err, rec.Code, rec.Body)
	}
}
