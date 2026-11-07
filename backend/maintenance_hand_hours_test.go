package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Hand-read hours (ADR 0268): an item with no SignalK hour meter takes its
// current hours from the latest-dated gauge figure the operator has typed for
// it. These tests pin the pick, the gauge-to-true conversion, the refusal on
// an item that has a meter, and that no view ever shows a hand figure as live.

func TestHandEquipmentHours(t *testing.T) {
	now := mustParseDate(t, "2026-06-15")
	resets := []hourMeterReset{{ID: "a", OldReading: 500, NewReading: 0, ChangedAt: "2026-01-01"}}

	if got := handEquipmentHours(nil, nil, now); got.Known {
		t.Fatalf("no samples must read unknown, got %+v", got)
	}

	got := handEquipmentHours([]handHourSample{
		{ReadOn: "2026-03-01", Hours: 600},
		{ReadOn: "2026-05-01", Hours: 650},
		{ReadOn: "2026-04-01", Hours: 700},
	}, resets, now)
	if !got.Known || !got.ByHand || got.ReadOn != "2026-05-01" || got.Hours != 650 {
		t.Fatalf("the latest-dated sample wins, got %+v", got)
	}
	if got.Gauge != 150 {
		t.Fatalf("gauge is true hours less the offset in force now (500), got %v", got.Gauge)
	}
	if got.AsOf != nil {
		t.Fatalf("a hand reading has a date, not a received-at instant, got %v", got.AsOf)
	}

	future := handEquipmentHours([]handHourSample{
		{ReadOn: "2026-05-01", Hours: 650},
		{ReadOn: "2026-07-01", Hours: 900},
	}, nil, now)
	if future.Hours != 650 {
		t.Fatalf("a figure dated well ahead is not a reading yet, got %+v", future)
	}

	tie := handEquipmentHours([]handHourSample{
		{ReadOn: "2026-05-01", Hours: 640},
		{ReadOn: "2026-05-01", Hours: 650},
	}, nil, now)
	if tie.Hours != 650 {
		t.Fatalf("on the same date the larger figure wins, got %+v", tie)
	}
}

func recordHandHoursRequest(t *testing.T, equipmentID, today, body string) (int, string) {
	t.Helper()
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/equipment/"+equipmentID+"/maintenance/hour-readings?today="+today, body, equipmentID)
	if err := recordHandHourReadingHandler(c); err != nil {
		t.Fatalf("recordHandHourReadingHandler: %v", err)
	}
	return rec.Code, rec.Body.String()
}

func TestRecordHandHourReading_StoresGaugeAndTrueHours(t *testing.T) {
	withTestDocumentStore(t)
	maker := mustCreateHandlerTestEquipment(t, "Watermaker")
	if _, err := globalDocumentStore.RecordHourMeterReset(maker.ID, 500, 0, "2026-01-01"); err != nil {
		t.Fatalf("RecordHourMeterReset: %v", err)
	}

	code, body := recordHandHoursRequest(t, maker.ID, "2026-06-15", `{"read_on":"2026-02-01","gauge":50,"note":"Read at the panel"}`)
	if code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", code, body)
	}
	var resp struct {
		Reading handHourReading `json:"reading"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	r := resp.Reading
	if r.Gauge != 50 || r.Hours != 550 || r.ReadOn != "2026-02-01" || r.Note != "Read at the panel" || r.ID == "" {
		t.Fatalf("expected gauge 50 kept and true hours 550 (offset in force on that date), got %+v", r)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/inventory/equipment/"+maker.ID+"/maintenance/hour-readings", "", maker.ID)
	if err := listHandHourReadingsHandler(c); err != nil {
		t.Fatalf("listHandHourReadingsHandler: %v", err)
	}
	var list struct {
		Readings []handHourReading `json:"readings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(list.Readings) != 1 || list.Readings[0].ID != r.ID {
		t.Fatalf("expected the kept reading, got %+v", list.Readings)
	}
}

func TestRecordHandHourReading_Validation(t *testing.T) {
	withTestDocumentStore(t)
	maker := mustCreateHandlerTestEquipment(t, "Watermaker")

	cases := []struct {
		name, today, body string
		want              int
		wantField         string
	}{
		{"bad date", "2026-06-15", `{"read_on":"yesterday","gauge":10}`, http.StatusBadRequest, "read_on"},
		{"future date", "2026-06-15", `{"read_on":"2026-06-16","gauge":10}`, http.StatusBadRequest, "read_on"},
		{"negative gauge", "2026-06-15", `{"read_on":"2026-06-15","gauge":-1}`, http.StatusBadRequest, "gauge"},
		{"missing gauge", "2026-06-15", `{"read_on":"2026-06-15"}`, http.StatusBadRequest, "gauge"},
		{"no today", "", `{"read_on":"2026-06-15","gauge":10}`, http.StatusBadRequest, "today"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := recordHandHoursRequest(t, maker.ID, tc.today, tc.body)
			if code != tc.want || !strings.Contains(body, tc.wantField) {
				t.Fatalf("expected %d naming %q, got %d: %s", tc.want, tc.wantField, code, body)
			}
		})
	}

	code, body := recordHandHoursRequest(t, "no-such-item", "2026-06-15", `{"read_on":"2026-06-15","gauge":10}`)
	if code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown item, got %d: %s", code, body)
	}
}

// The fallback policy: a hand reading can never stand in for a meter that has
// gone quiet, so the API refuses one on an item that has a source.
func TestRecordHandHourReading_RefusedOnItemWithMeterSource(t *testing.T) {
	withTestDocumentStore(t)
	engine, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Main engine", Category: "mechanical", System: "propulsion", SignalKSource: "propulsion.main"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	code, body := recordHandHoursRequest(t, engine.ID, "2026-06-15", `{"read_on":"2026-06-15","gauge":10}`)
	if code != http.StatusConflict || !strings.Contains(body, "hour meter") {
		t.Fatalf("expected 409 naming the hour meter, got %d: %s", code, body)
	}
	readings, err := globalDocumentStore.ListHandHourReadings(engine.ID)
	if err != nil {
		t.Fatalf("ListHandHourReadings: %v", err)
	}
	if len(readings) != 0 {
		t.Fatalf("a refused reading must not be stored, got %+v", readings)
	}
}

func handHoursRule(t *testing.T, equipmentID string) maintenanceRule {
	t.Helper()
	interval := 100.0
	rule, err := globalDocumentStore.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &equipmentID, Description: "Filter change", IntervalHours: &interval})
	if err != nil {
		t.Fatalf("CreateMaintenanceRule: %v", err)
	}
	return rule
}

func TestRuleView_HandReadingStandsInForMissingMeter(t *testing.T) {
	withTestDocumentStore(t)
	maker := mustCreateHandlerTestEquipment(t, "Watermaker")
	rule := handHoursRule(t, maker.ID)
	today := mustParseDate(t, "2026-06-15")

	view, err := resolveMaintenanceRuleView(rule.ID, today)
	if err != nil {
		t.Fatalf("resolveMaintenanceRuleView: %v", err)
	}
	if view.CurrentHours != nil || view.HoursByHand || view.Status != string(maintenanceStatusNeverRecorded) {
		t.Fatalf("no reading yet: nothing current, got %+v", view)
	}

	at, hours := "2026-05-01", 40.0
	if _, err := globalDocumentStore.SetMaintenanceRuleLastDone(rule.ID, &at, &hours); err != nil {
		t.Fatalf("SetMaintenanceRuleLastDone: %v", err)
	}
	view, err = resolveMaintenanceRuleView(rule.ID, today)
	if err != nil {
		t.Fatalf("resolveMaintenanceRuleView: %v", err)
	}
	if !view.HoursUnknown || view.Status != string(maintenanceStatusHoursUnknown) {
		t.Fatalf("a baseline alone is not a current reading: hours stay unknown, got %+v", view)
	}

	if code, body := recordHandHoursRequest(t, maker.ID, "2026-06-15", `{"read_on":"2026-06-10","gauge":120}`); code != http.StatusCreated {
		t.Fatalf("record: %d %s", code, body)
	}
	view, err = resolveMaintenanceRuleView(rule.ID, today)
	if err != nil {
		t.Fatalf("resolveMaintenanceRuleView: %v", err)
	}
	if view.CurrentHours == nil || *view.CurrentHours != 120 || !view.HoursByHand || view.HoursReadOn != "2026-06-10" {
		t.Fatalf("expected 120 read by hand on 2026-06-10, got %+v", view)
	}
	if view.HoursAsOf != nil {
		t.Fatalf("a hand reading is never given a live received-at time, got %v", *view.HoursAsOf)
	}
	if view.HoursUnknown || view.RemainingHours == nil || *view.RemainingHours != 20 {
		t.Fatalf("40 + 100 - 120 = 20 left, got %+v", view)
	}
}

func TestRuleView_LatestDatedFigureWinsAcrossEverySource(t *testing.T) {
	withTestDocumentStore(t)
	maker := mustCreateHandlerTestEquipment(t, "Watermaker")
	rule := handHoursRule(t, maker.ID)
	today := mustParseDate(t, "2026-06-15")

	if code, body := recordHandHoursRequest(t, maker.ID, "2026-06-15", `{"read_on":"2026-03-01","gauge":100}`); code != http.StatusCreated {
		t.Fatalf("record: %d %s", code, body)
	}
	// A Complete form on a later date.
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/rules/"+rule.ID+"/complete?today=2026-06-15", `{"performed_at":"2026-04-01","hours":130,"description":"Filter"}`, rule.ID)
	if err := completeMaintenanceRuleHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("complete: %v %d %s", err, rec.Code, rec.Body.String())
	}
	assertCurrent := func(want float64, on string) {
		t.Helper()
		view, err := resolveMaintenanceRuleView(rule.ID, today)
		if err != nil {
			t.Fatalf("resolveMaintenanceRuleView: %v", err)
		}
		if view.CurrentHours == nil || *view.CurrentHours != want || view.HoursReadOn != on || !view.HoursByHand {
			t.Fatalf("expected %v read by hand on %s, got %+v (read on %q)", want, on, view.CurrentHours, view.HoursReadOn)
		}
	}
	assertCurrent(130, "2026-04-01")

	// A standalone service log entry on a later date still.
	c, rec = newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/log", `{"equipment_id":"`+maker.ID+`","performed_at":"2026-05-20","hours":150,"kind":"repair","description":"Pump"}`, "")
	if err := createMaintenanceLogEntryHandler(c); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("log entry: %v %d %s", err, rec.Code, rec.Body.String())
	}
	assertCurrent(150, "2026-05-20")

	// An older reading entered afterwards does not displace it.
	if code, body := recordHandHoursRequest(t, maker.ID, "2026-06-15", `{"read_on":"2026-05-01","gauge":999}`); code != http.StatusCreated {
		t.Fatalf("record: %d %s", code, body)
	}
	assertCurrent(150, "2026-05-20")
}

func TestSetLastDoneHours_CountsAsAHandReadingOnlyWithoutAMeter(t *testing.T) {
	withTestDocumentStore(t)
	maker := mustCreateHandlerTestEquipment(t, "Watermaker")
	rule := handHoursRule(t, maker.ID)
	at, hours := "2026-05-01", 40.0
	if _, err := globalDocumentStore.SetMaintenanceRuleLastDoneCmd(mustParseDate(t, "2026-06-15"), rule.ID, maintenanceLastDoneRequest{LastDoneAt: &at, LastDoneHours: &hours}); err != nil {
		t.Fatalf("SetMaintenanceRuleLastDoneCmd: %v", err)
	}
	view, err := resolveMaintenanceRuleView(rule.ID, mustParseDate(t, "2026-06-15"))
	if err != nil {
		t.Fatalf("resolveMaintenanceRuleView: %v", err)
	}
	if view.CurrentHours == nil || *view.CurrentHours != 40 || !view.HoursByHand || view.HoursReadOn != "2026-05-01" {
		t.Fatalf("Set last done hours are a hand reading dated that day, got %+v", view)
	}

	engine, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Main engine", Category: "mechanical", System: "propulsion", SignalKSource: "propulsion.main"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	engineRule := handHoursRule(t, engine.ID)
	if _, err := globalDocumentStore.SetMaintenanceRuleLastDoneCmd(mustParseDate(t, "2026-06-15"), engineRule.ID, maintenanceLastDoneRequest{LastDoneAt: &at, LastDoneHours: &hours}); err != nil {
		t.Fatalf("SetMaintenanceRuleLastDoneCmd: %v", err)
	}
	readings, err := globalDocumentStore.ListHandHourReadings(engine.ID)
	if err != nil {
		t.Fatalf("ListHandHourReadings: %v", err)
	}
	if len(readings) != 0 {
		t.Fatalf("an item with a meter keeps no hand readings, got %+v", readings)
	}
}

// A mistyped Set last done figure corrected the same day must lower the hours:
// the larger-figure-wins rule on one date would otherwise keep the typo.
func TestSetLastDoneHours_ASecondSetOnTheSameDayReplacesTheFirst(t *testing.T) {
	withTestDocumentStore(t)
	maker := mustCreateHandlerTestEquipment(t, "Watermaker")
	rule := handHoursRule(t, maker.ID)
	at := "2026-05-01"
	today := mustParseDate(t, "2026-06-15")
	for _, h := range []float64{1500, 150} {
		hours := h
		if _, err := globalDocumentStore.SetMaintenanceRuleLastDoneCmd(today, rule.ID, maintenanceLastDoneRequest{LastDoneAt: &at, LastDoneHours: &hours}); err != nil {
			t.Fatalf("SetMaintenanceRuleLastDoneCmd: %v", err)
		}
	}
	// A reading logged by hand the same day, with its own note, stays.
	if code, body := recordHandHoursRequest(t, maker.ID, "2026-06-15", `{"read_on":"2026-05-01","gauge":100,"note":"gauge photo"}`); code != http.StatusCreated {
		t.Fatalf("record: %d %s", code, body)
	}
	readings, err := globalDocumentStore.ListHandHourReadings(maker.ID)
	if err != nil {
		t.Fatalf("ListHandHourReadings: %v", err)
	}
	if len(readings) != 2 {
		t.Fatalf("want the corrected Set last done and the hand reading, got %+v", readings)
	}
	view, err := resolveMaintenanceRuleView(rule.ID, today)
	if err != nil {
		t.Fatalf("resolveMaintenanceRuleView: %v", err)
	}
	if view.CurrentHours == nil || *view.CurrentHours != 150 {
		t.Fatalf("the corrected 150 must stand, got %+v", view.CurrentHours)
	}
}

// Fallback policy: a quiet meter reads unknown even when hours were typed for
// the item, because that figure must never pass for the live reading.
func TestRuleView_ItemWithMeterNeverFallsBackToHandHours(t *testing.T) {
	withTestDocumentStore(t)
	withGlobalSnapshot(t, snapshotWithSelfValues(map[string]any{}))
	engine, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Main engine", Category: "mechanical", System: "propulsion", SignalKSource: "propulsion.main"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	rule := handHoursRule(t, engine.ID)
	if _, err := globalDocumentStore.CreateMaintenanceLogEntry(maintenanceLogEntryInput{EquipmentID: &engine.ID, PerformedAt: "2026-06-01", Hours: fptr(900), Kind: "repair", Description: "Hose"}); err != nil {
		t.Fatalf("CreateMaintenanceLogEntry: %v", err)
	}
	at, hours := "2026-06-01", 900.0
	if _, err := globalDocumentStore.SetMaintenanceRuleLastDone(rule.ID, &at, &hours); err != nil {
		t.Fatalf("SetMaintenanceRuleLastDone: %v", err)
	}

	view, err := resolveMaintenanceRuleView(rule.ID, mustParseDate(t, "2026-06-15"))
	if err != nil {
		t.Fatalf("resolveMaintenanceRuleView: %v", err)
	}
	if !view.HoursUnknown || view.CurrentHours != nil || view.HoursByHand || view.Status != string(maintenanceStatusHoursUnknown) {
		t.Fatalf("a silent meter must read unknown, got %+v", view)
	}
}

func TestDeleteHandHourReading(t *testing.T) {
	withTestDocumentStore(t)
	maker := mustCreateHandlerTestEquipment(t, "Watermaker")
	other := mustCreateHandlerTestEquipment(t, "Freezer")
	reading, err := globalDocumentStore.RecordHandHourReadingCmd(mustParseDate(t, "2026-06-15"), maker.ID, handHourReadingRequest{ReadOn: "2026-06-10", Gauge: fptr(120)})
	if err != nil {
		t.Fatalf("RecordHandHourReadingCmd: %v", err)
	}

	del := func(equipmentID string) int {
		c, rec := newDocumentEchoContext(http.MethodDelete, "/x", "", equipmentID)
		c.SetParamNames("id", "readingId")
		c.SetParamValues(equipmentID, reading.ID)
		if err := deleteHandHourReadingHandler(c); err != nil {
			t.Fatalf("deleteHandHourReadingHandler: %v", err)
		}
		return rec.Code
	}
	if code := del(other.ID); code != http.StatusNotFound {
		t.Fatalf("a reading belongs to its own item, got %d", code)
	}
	if code := del(maker.ID); code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", code)
	}
	readings, err := globalDocumentStore.ListHandHourReadings(maker.ID)
	if err != nil || len(readings) != 0 {
		t.Fatalf("expected the reading gone, got %+v (%v)", readings, err)
	}
}

func TestMateMaintenanceTools_MarkHandReadHours(t *testing.T) {
	deps, store := maintenanceToolDeps(t)
	withGlobalSnapshot(t, snapshotWithSelfValues(map[string]any{}))
	maker := mustToolEquipment(t, store, equipmentItem{Name: "Watermaker", System: "electrical"})
	rule := mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &maker.ID, Description: "Filter change", IntervalHours: fptr(100)})
	if _, err := store.RecordHandHourReadingCmd(mustParseDate(t, "2026-09-30"), maker.ID, handHourReadingRequest{ReadOn: "2026-09-09", Gauge: fptr(120)}); err != nil {
		t.Fatalf("RecordHandHourReadingCmd: %v", err)
	}
	at, hours := "2026-09-01", 40.0
	if _, err := store.SetMaintenanceRuleLastDone(rule.ID, &at, &hours); err != nil {
		t.Fatalf("SetMaintenanceRuleLastDone: %v", err)
	}

	var found assistantFindEquipmentResult
	if err := json.Unmarshal([]byte(runMaintenanceTool(t, deps, "find_equipment", `{}`)), &found); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	hit := found.Equipment[0]
	if !hit.HoursKnown || hit.CurrentMeterReading == nil || *hit.CurrentMeterReading != 120 || !hit.HoursReadByHand || hit.HoursReadOn != "2026-09-09" || hit.HoursAsOf != "" {
		t.Fatalf("find_equipment must mark the figure as read by hand with its date, never as live, got %+v", hit)
	}

	var listed assistantListMaintenanceResult
	if err := json.Unmarshal([]byte(runMaintenanceTool(t, deps, "list_maintenance", `{}`)), &listed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	row := listed.Rules[0]
	if row.CurrentMeterReading == nil || *row.CurrentMeterReading != 120 || !row.HoursReadByHand || row.HoursReadOn != "2026-09-09" {
		t.Fatalf("list_maintenance must mark the figure as read by hand with its date, got %+v", row)
	}
}
