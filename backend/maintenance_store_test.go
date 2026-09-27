package main

import (
	"errors"
	"testing"
	"time"
)

// This file exercises maintenance_store.go (ADR 0138): rules, the service
// log, and hour-meter resets. Written test-first per AGENTS.md - every
// method and sentinel referenced here was designed here before
// maintenance_store.go implemented it.

func mustCreateTestEquipment(t *testing.T, store *documentStore, name string) equipmentItem {
	t.Helper()
	item, err := store.CreateEquipment(equipmentItem{Name: name, Category: "mechanical"})
	if err != nil {
		t.Fatalf("CreateEquipment(%q): %v", name, err)
	}
	return item
}

func TestDocumentStore_CreateMaintenanceRuleRoundTrips(t *testing.T) {
	store := newTestDocumentStore(t)
	item := mustCreateTestEquipment(t, store, "Port engine")

	hours := 250.0
	rule, err := store.CreateMaintenanceRule(maintenanceRuleInput{
		EquipmentID:   &item.ID,
		Description:   "Engine oil and filter",
		IntervalHours: &hours,
	})
	if err != nil {
		t.Fatalf("CreateMaintenanceRule: %v", err)
	}
	if rule.ID == "" || rule.Description != "Engine oil and filter" {
		t.Fatalf("unexpected rule: %+v", rule)
	}
	if rule.EquipmentID == nil || *rule.EquipmentID != item.ID {
		t.Fatalf("expected equipment_id %q, got %+v", item.ID, rule.EquipmentID)
	}
	if rule.Acknowledged {
		t.Fatalf("a brand new rule must not be acknowledged")
	}

	got, err := store.GetMaintenanceRule(rule.ID)
	if err != nil {
		t.Fatalf("GetMaintenanceRule: %v", err)
	}
	if got.ID != rule.ID {
		t.Fatalf("GetMaintenanceRule returned a different rule")
	}
}

func TestDocumentStore_CreateMaintenanceRuleWithNoEquipmentIsCalendarOnly(t *testing.T) {
	store := newTestDocumentStore(t)
	months := 12
	rule, err := store.CreateMaintenanceRule(maintenanceRuleInput{
		Description:    "Vessel registration renewal",
		IntervalMonths: &months,
	})
	if err != nil {
		t.Fatalf("CreateMaintenanceRule: %v", err)
	}
	if rule.EquipmentID != nil {
		t.Fatalf("expected a nil equipment_id for a calendar-only rule, got %+v", rule.EquipmentID)
	}
}

func TestDocumentStore_CreateMaintenanceRuleRejectsUnknownEquipment(t *testing.T) {
	store := newTestDocumentStore(t)
	bogus := "does-not-exist"
	if _, err := store.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &bogus, Description: "x"}); !errors.Is(err, errEquipmentNotFound) {
		t.Fatalf("expected errEquipmentNotFound, got %v", err)
	}
}

func TestDocumentStore_DeleteEquipmentCascadesMaintenanceRules(t *testing.T) {
	store := newTestDocumentStore(t)
	item := mustCreateTestEquipment(t, store, "Generator")
	hours := 100.0
	rule, err := store.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &item.ID, Description: "Service", IntervalHours: &hours})
	if err != nil {
		t.Fatalf("CreateMaintenanceRule: %v", err)
	}

	if _, err := store.DeleteEquipment(item.ID, false); err != nil {
		t.Fatalf("DeleteEquipment: %v", err)
	}

	if _, err := store.GetMaintenanceRule(rule.ID); !errors.Is(err, errMaintenanceRuleNotFound) {
		t.Fatalf("expected the rule to be gone with its item (CASCADE), got %v", err)
	}
}

func TestDocumentStore_DeleteMaintenanceRuleKeepsLogHistory(t *testing.T) {
	store := newTestDocumentStore(t)
	item := mustCreateTestEquipment(t, store, "Watermaker")
	hours := 500.0
	rule, err := store.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &item.ID, Description: "Membrane clean", IntervalHours: &hours})
	if err != nil {
		t.Fatalf("CreateMaintenanceRule: %v", err)
	}
	_, entry, err := store.CompleteMaintenanceRule(rule.ID, maintenanceLogEntryInput{PerformedAt: "2026-05-01", Hours: ptrFloat(120)})
	if err != nil {
		t.Fatalf("CompleteMaintenanceRule: %v", err)
	}

	if err := store.DeleteMaintenanceRule(rule.ID); err != nil {
		t.Fatalf("DeleteMaintenanceRule: %v", err)
	}

	got, err := store.GetMaintenanceLogEntry(entry.ID)
	if err != nil {
		t.Fatalf("expected the log entry to survive the rule's deletion, got err: %v", err)
	}
	if got.RuleID != nil {
		t.Fatalf("expected rule_id to be cleared (SET NULL), got %+v", got.RuleID)
	}
}

func TestDocumentStore_CompleteMaintenanceRuleResetsBaselineAndClearsAck(t *testing.T) {
	store := newTestDocumentStore(t)
	item := mustCreateTestEquipment(t, store, "Main engine")
	hours := 250.0
	rule, err := store.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &item.ID, Description: "Oil change", IntervalHours: &hours})
	if err != nil {
		t.Fatalf("CreateMaintenanceRule: %v", err)
	}
	if _, err := store.AcknowledgeMaintenanceRule(rule.ID, "waiting on parts"); err != nil {
		t.Fatalf("AcknowledgeMaintenanceRule: %v", err)
	}

	updated, entry, err := store.CompleteMaintenanceRule(rule.ID, maintenanceLogEntryInput{
		PerformedAt: "2026-06-01",
		Hours:       ptrFloat(1234.5),
		Description: "Changed oil and filter",
		Who:         "Skipper",
	})
	if err != nil {
		t.Fatalf("CompleteMaintenanceRule: %v", err)
	}
	if entry.Kind != "maintenance" {
		t.Fatalf("expected kind 'maintenance', got %q", entry.Kind)
	}
	if entry.RuleID == nil || *entry.RuleID != rule.ID {
		t.Fatalf("expected the entry to reference the rule, got %+v", entry.RuleID)
	}
	if updated.LastDoneAt != "2026-06-01" || updated.LastDoneHours == nil || *updated.LastDoneHours != 1234.5 {
		t.Fatalf("expected the baseline to be reset, got %+v", updated)
	}
	if updated.Acknowledged || updated.AckReason != "" {
		t.Fatalf("expected completion to clear the acknowledgement, got %+v", updated)
	}
}

// TestDocumentStore_CompleteMaintenanceRuleBlankHoursLeavesBaselineHoursUnchanged
// pins the code-review finding: completing a calendar/months-based rule
// (interval_hours unset) with no hours given used to unconditionally write
// last_done_hours = NULL, wiping out an hours figure the rule already
// carried (e.g. from an earlier Set-last-done) even though nothing about
// this completion said anything about hours at all.
func TestDocumentStore_CompleteMaintenanceRuleBlankHoursLeavesBaselineHoursUnchanged(t *testing.T) {
	store := newTestDocumentStore(t)
	item := mustCreateTestEquipment(t, store, "Generator")
	months := 12
	rule, err := store.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &item.ID, Description: "Anode check", IntervalMonths: &months})
	if err != nil {
		t.Fatalf("CreateMaintenanceRule: %v", err)
	}
	priorHours := 900.0
	if _, err := store.SetMaintenanceRuleLastDone(rule.ID, ptrString("2025-06-01"), &priorHours); err != nil {
		t.Fatalf("SetMaintenanceRuleLastDone: %v", err)
	}

	updated, _, err := store.CompleteMaintenanceRule(rule.ID, maintenanceLogEntryInput{PerformedAt: "2026-06-01"})
	if err != nil {
		t.Fatalf("CompleteMaintenanceRule: %v", err)
	}
	if updated.LastDoneAt != "2026-06-01" {
		t.Fatalf("expected last_done_at to advance, got %q", updated.LastDoneAt)
	}
	if updated.LastDoneHours == nil || *updated.LastDoneHours != priorHours {
		t.Fatalf("expected last_done_hours to stay at %v (blank hours must not wipe it), got %+v", priorHours, updated.LastDoneHours)
	}
}

func ptrString(s string) *string { return &s }

func TestDocumentStore_AcknowledgeMaintenanceRuleSetsAndClears(t *testing.T) {
	store := newTestDocumentStore(t)
	item := mustCreateTestEquipment(t, store, "Bow thruster")
	months := 6
	rule, err := store.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &item.ID, Description: "Anode check", IntervalMonths: &months})
	if err != nil {
		t.Fatalf("CreateMaintenanceRule: %v", err)
	}

	acked, err := store.AcknowledgeMaintenanceRule(rule.ID, "yard")
	if err != nil {
		t.Fatalf("AcknowledgeMaintenanceRule: %v", err)
	}
	if !acked.Acknowledged || acked.AckReason != "yard" {
		t.Fatalf("expected an acknowledged rule with reason 'yard', got %+v", acked)
	}

	cleared, err := store.AcknowledgeMaintenanceRule(rule.ID, "")
	if err != nil {
		t.Fatalf("AcknowledgeMaintenanceRule (clear): %v", err)
	}
	if cleared.Acknowledged || cleared.AckReason != "" {
		t.Fatalf("expected a blank reason to clear the acknowledgement, got %+v", cleared)
	}
}

func TestDocumentStore_SetMaintenanceRuleLastDoneWritesNoLogEntry(t *testing.T) {
	store := newTestDocumentStore(t)
	item := mustCreateTestEquipment(t, store, "Fridge compressor")
	months := 12
	rule, err := store.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &item.ID, Description: "Regas check", IntervalMonths: &months})
	if err != nil {
		t.Fatalf("CreateMaintenanceRule: %v", err)
	}

	at := "2025-01-15"
	updated, err := store.SetMaintenanceRuleLastDone(rule.ID, &at, nil)
	if err != nil {
		t.Fatalf("SetMaintenanceRuleLastDone: %v", err)
	}
	if updated.LastDoneAt != at {
		t.Fatalf("expected last_done_at %q, got %q", at, updated.LastDoneAt)
	}

	entries, err := store.ListMaintenanceLogEntries(maintenanceLogFilter{EquipmentID: item.ID})
	if err != nil {
		t.Fatalf("ListMaintenanceLogEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected Set-last-done to write NO log entry (onboarding, not a fake service), got %+v", entries)
	}
}

func TestDocumentStore_CopyProfileServiceEntriesSkipsAlreadyCopied(t *testing.T) {
	store := newTestDocumentStore(t)
	item := mustCreateTestEquipment(t, store, "Main engine")

	hours := 250.0
	months := 12
	services := []engineProfileService{
		{ID: "engine-oil", Description: "Engine oil and filter", IntervalHours: &hours, IntervalMonths: &months},
		{ID: "impeller", Description: "Raw water impeller"}, // both intervals nil - a slot
	}

	created, err := store.CopyProfileServiceEntries(item.ID, services)
	if err != nil {
		t.Fatalf("CopyProfileServiceEntries: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("expected 2 rules created, got %d", len(created))
	}

	// Pressing "Use profile schedule" again must not duplicate anything.
	second, err := store.CopyProfileServiceEntries(item.ID, services)
	if err != nil {
		t.Fatalf("CopyProfileServiceEntries (second call): %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("expected no new rules on the second copy, got %+v", second)
	}

	rules, err := store.ListMaintenanceRules(maintenanceRuleFilter{EquipmentID: item.ID, IncludeStored: true})
	if err != nil {
		t.Fatalf("ListMaintenanceRules: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected exactly 2 rules total, got %d", len(rules))
	}
}

func TestDocumentStore_ListMaintenanceRulesExcludesStoredItemsByDefault(t *testing.T) {
	store := newTestDocumentStore(t)
	deployed := mustCreateTestEquipment(t, store, "Deployed engine")
	storedItem := mustCreateTestEquipment(t, store, "Spare pump")
	if _, err := store.UpdateEquipment(storedItem.ID, toEquipmentInputForTest(storedItem, "stored")); err != nil {
		t.Fatalf("UpdateEquipment: %v", err)
	}

	hours := 100.0
	if _, err := store.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &deployed.ID, Description: "A", IntervalHours: &hours}); err != nil {
		t.Fatalf("CreateMaintenanceRule: %v", err)
	}
	if _, err := store.CreateMaintenanceRule(maintenanceRuleInput{EquipmentID: &storedItem.ID, Description: "B", IntervalHours: &hours}); err != nil {
		t.Fatalf("CreateMaintenanceRule: %v", err)
	}
	months := 12
	if _, err := store.CreateMaintenanceRule(maintenanceRuleInput{Description: "Registration", IntervalMonths: &months}); err != nil {
		t.Fatalf("CreateMaintenanceRule: %v", err)
	}

	visible, err := store.ListMaintenanceRules(maintenanceRuleFilter{})
	if err != nil {
		t.Fatalf("ListMaintenanceRules: %v", err)
	}
	if len(visible) != 2 {
		t.Fatalf("expected the stored item's rule to be excluded (2 remaining: the deployed item's + the calendar-only one), got %d: %+v", len(visible), visible)
	}

	all, err := store.ListMaintenanceRules(maintenanceRuleFilter{IncludeStored: true})
	if err != nil {
		t.Fatalf("ListMaintenanceRules(IncludeStored): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected all 3 rules with IncludeStored, got %d", len(all))
	}
}

func toEquipmentInputForTest(item equipmentItem, status string) equipmentItem {
	item.Status = status
	return item
}

func TestDocumentStore_MaintenanceLogEntryCRUDWithParts(t *testing.T) {
	store := newTestDocumentStore(t)
	item := mustCreateTestEquipment(t, store, "Main engine")
	spare := mustCreateTestEquipment(t, store, "Impeller (spare)")

	entry, err := store.CreateMaintenanceLogEntry(maintenanceLogEntryInput{
		EquipmentID: &item.ID,
		PerformedAt: "2026-04-01",
		Hours:       ptrFloat(900),
		Kind:        "repair",
		Description: "Replaced raw water pump impeller",
		Who:         "Skipper",
		Cost:        ptrFloat(45.5),
		Currency:    "AUD",
		Parts:       []maintenanceLogPartInput{{EquipmentID: spare.ID, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("CreateMaintenanceLogEntry: %v", err)
	}
	if len(entry.Parts) != 1 || entry.Parts[0].EquipmentID != spare.ID || entry.Parts[0].EquipmentName != "Impeller (spare)" {
		t.Fatalf("expected the part joined with its name, got %+v", entry.Parts)
	}

	updated, err := store.UpdateMaintenanceLogEntry(entry.ID, maintenanceLogEntryInput{
		EquipmentID: &item.ID,
		PerformedAt: "2026-04-02",
		Kind:        "repair",
		Description: "Corrected date",
		Parts:       nil,
	})
	if err != nil {
		t.Fatalf("UpdateMaintenanceLogEntry: %v", err)
	}
	if updated.PerformedAt != "2026-04-02" || len(updated.Parts) != 0 {
		t.Fatalf("expected the update to apply and clear parts, got %+v", updated)
	}

	if err := store.DeleteMaintenanceLogEntry(entry.ID); err != nil {
		t.Fatalf("DeleteMaintenanceLogEntry: %v", err)
	}
	if _, err := store.GetMaintenanceLogEntry(entry.ID); !errors.Is(err, errMaintenanceLogEntryNotFound) {
		t.Fatalf("expected errMaintenanceLogEntryNotFound after delete, got %v", err)
	}
}

func TestDocumentStore_MaintenanceLogPhotosRoundTripAndOrphanCheck(t *testing.T) {
	store := newTestDocumentStore(t)
	item := mustCreateTestEquipment(t, store, "Main engine")
	entry, err := store.CreateMaintenanceLogEntry(maintenanceLogEntryInput{EquipmentID: &item.ID, PerformedAt: "2026-04-01", Kind: "repair"})
	if err != nil {
		t.Fatalf("CreateMaintenanceLogEntry: %v", err)
	}
	photo := mustInsertPhotoDocument(t, store, "logphotosha1", "before.jpg")

	if err := store.AddMaintenanceLogPhoto(entry.ID, photo.ID); err != nil {
		t.Fatalf("AddMaintenanceLogPhoto: %v", err)
	}
	got, err := store.GetMaintenanceLogEntry(entry.ID)
	if err != nil {
		t.Fatalf("GetMaintenanceLogEntry: %v", err)
	}
	if len(got.PhotoIDs) != 1 || got.PhotoIDs[0] != photo.ID {
		t.Fatalf("expected the photo linked, got %+v", got.PhotoIDs)
	}

	deletable, err := store.MaintenanceLogPhotoDeletableAsOrphan(photo.ID)
	if err != nil {
		t.Fatalf("MaintenanceLogPhotoDeletableAsOrphan: %v", err)
	}
	if deletable {
		t.Fatalf("expected the photo to NOT be deletable while still linked")
	}

	if err := store.RemoveMaintenanceLogPhoto(entry.ID, photo.ID); err != nil {
		t.Fatalf("RemoveMaintenanceLogPhoto: %v", err)
	}
	deletable, err = store.MaintenanceLogPhotoDeletableAsOrphan(photo.ID)
	if err != nil {
		t.Fatalf("MaintenanceLogPhotoDeletableAsOrphan (after unlink): %v", err)
	}
	if !deletable {
		t.Fatalf("expected the photo to be deletable once unlinked and unshared")
	}
}

func TestDocumentStore_MaintenanceLogPhotoSharedWithEquipmentIsNotOrphan(t *testing.T) {
	store := newTestDocumentStore(t)
	item := mustCreateTestEquipment(t, store, "Main engine")
	entry, err := store.CreateMaintenanceLogEntry(maintenanceLogEntryInput{EquipmentID: &item.ID, PerformedAt: "2026-04-01", Kind: "repair"})
	if err != nil {
		t.Fatalf("CreateMaintenanceLogEntry: %v", err)
	}
	photo := mustInsertPhotoDocument(t, store, "logphotosha2", "shared.jpg")

	if err := store.AddEquipmentPhoto(item.ID, photo.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto: %v", err)
	}
	if err := store.AddMaintenanceLogPhoto(entry.ID, photo.ID); err != nil {
		t.Fatalf("AddMaintenanceLogPhoto: %v", err)
	}

	// Unlinking from the equipment side must not report the photo as
	// deletable while the log entry still shows it - cross-table sharing.
	if err := store.RemoveEquipmentPhoto(item.ID, photo.ID); err != nil {
		t.Fatalf("RemoveEquipmentPhoto: %v", err)
	}
	deletable, err := store.DocumentDeletableAsOrphanPhoto(photo.ID)
	if err != nil {
		t.Fatalf("DocumentDeletableAsOrphanPhoto: %v", err)
	}
	if deletable {
		t.Fatalf("expected the photo to survive as still-linked from the log entry")
	}
}

// TestDocumentStore_DeleteEquipmentWithDeletePhotosSparesOneALogEntryStillUses
// pins the code-review finding: exclusivePhotoIDsForEquipmentID (what
// DeleteEquipment's own deletePhotos=true path uses to decide "only this
// item uses it") used to check equipment_documents alone, so a photo
// exclusive to ONE item's own strip but ALSO linked to a maintenance log
// entry - on a DIFFERENT item, so it survives the deleted item's own
// cascade - was reported as safe to delete and destroyed a document that
// log entry still needed.
func TestDocumentStore_DeleteEquipmentWithDeletePhotosSparesAPhotoALogEntryStillUses(t *testing.T) {
	store := newTestDocumentStore(t)
	item := mustCreateTestEquipment(t, store, "Item to delete")
	otherItem := mustCreateTestEquipment(t, store, "Other item")
	entry, err := store.CreateMaintenanceLogEntry(maintenanceLogEntryInput{EquipmentID: &otherItem.ID, PerformedAt: "2026-04-01", Kind: "repair"})
	if err != nil {
		t.Fatalf("CreateMaintenanceLogEntry: %v", err)
	}
	photo := mustInsertPhotoDocument(t, store, "logphotosha3", "shared-with-log.jpg")

	if err := store.AddEquipmentPhoto(item.ID, photo.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto: %v", err)
	}
	if err := store.AddMaintenanceLogPhoto(entry.ID, photo.ID); err != nil {
		t.Fatalf("AddMaintenanceLogPhoto: %v", err)
	}

	got, err := store.GetEquipment(item.ID)
	if err != nil {
		t.Fatalf("GetEquipment: %v", err)
	}
	if len(got.ExclusivePhotoIDs) != 0 {
		t.Fatalf("expected the photo to be excluded from ExclusivePhotoIDs while a log entry still shows it, got %+v", got.ExclusivePhotoIDs)
	}

	deletedSHAs, err := store.DeleteEquipment(item.ID, true)
	if err != nil {
		t.Fatalf("DeleteEquipment: %v", err)
	}
	if len(deletedSHAs) != 0 {
		t.Fatalf("expected no photo document to be deleted, got %+v", deletedSHAs)
	}
	if _, err := store.Get(photo.ID); err != nil {
		t.Fatalf("expected the shared photo document to survive, got err: %v", err)
	}
	afterEntry, err := store.GetMaintenanceLogEntry(entry.ID)
	if err != nil {
		t.Fatalf("GetMaintenanceLogEntry: %v", err)
	}
	if len(afterEntry.PhotoIDs) != 1 || afterEntry.PhotoIDs[0] != photo.ID {
		t.Fatalf("expected the log entry to keep showing the photo, got %+v", afterEntry.PhotoIDs)
	}
}

func TestDocumentStore_HourMeterResetHistoryRoundTrips(t *testing.T) {
	store := newTestDocumentStore(t)
	item := mustCreateTestEquipment(t, store, "Generator")

	t0 := mustParseDate(t, "2026-01-01")
	store.now = sequencedClock(t0, t0.Add(time.Minute))

	if _, err := store.RecordHourMeterReset(item.ID, 5000, 0, "2026-01-01"); err != nil {
		t.Fatalf("RecordHourMeterReset: %v", err)
	}
	if _, err := store.RecordHourMeterReset(item.ID, 5200, 10, "2026-03-01"); err != nil {
		t.Fatalf("RecordHourMeterReset (second): %v", err)
	}

	resets, err := store.ListHourMeterResets(item.ID)
	if err != nil {
		t.Fatalf("ListHourMeterResets: %v", err)
	}
	if len(resets) != 2 {
		t.Fatalf("expected 2 resets, got %d", len(resets))
	}
	// Newest first.
	if resets[0].OldReading != 5200 {
		t.Fatalf("expected the most recent reset first, got %+v", resets)
	}

	offset := latestMeterOffsetHours(resets)
	if offset != 5190 {
		t.Fatalf("expected offset 5190 (5200-10), got %v", offset)
	}
}

func ptrFloat(v float64) *float64 { return &v }
