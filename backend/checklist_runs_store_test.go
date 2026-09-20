package main

import (
	"errors"
	"net/http"
	"testing"
)

// This file is the store-level half of plan §3's checklist runs (see
// checklist_runs_store.go's own top comment for the shape being tested).
// Fixtures are built through the REAL handlers (mustCreateTestNote,
// patchNoteHandler via newDocumentEchoContext), the same "call the
// handler, not a hand-rolled row" idiom notes_handlers_test.go already
// uses, so a note fixture here is byte-for-byte what an operator's own
// capture/edit would produce - in particular, ReplaceNoteBody's own SHA
// dance runs for real on the "unrelated edit" test below, rather than a
// shortcut that only looks like it exercises the edit path.

func mustPatchTestNoteBody(t *testing.T, id, newBody string) {
	t.Helper()
	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/notes/"+id, `{"body":`+jsonString(newBody)+`}`, id)
	if err := patchNoteHandler(c); err != nil {
		t.Fatalf("patchNoteHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 patching note body, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── StartOrResumeChecklistRun ────────────────────────────────────────────

func TestDocumentStore_ChecklistRun_SecondStartResumesRatherThanDuplicating(t *testing.T) {
	store := withTestDocumentStore(t)
	note := mustCreateTestNote(t, "- [ ] Seacocks open\n- [ ] Check bilge", "Shutdown")

	first, resumed, err := store.StartOrResumeChecklistRun(note.ID)
	if err != nil {
		t.Fatalf("StartOrResumeChecklistRun (first): %v", err)
	}
	if resumed {
		t.Fatalf("expected the first call to start a fresh run, not resume")
	}

	second, resumed, err := store.StartOrResumeChecklistRun(note.ID)
	if err != nil {
		t.Fatalf("StartOrResumeChecklistRun (second): %v", err)
	}
	if !resumed {
		t.Fatalf("expected the second call to resume, not start a new run")
	}
	if second.ID != first.ID {
		t.Fatalf("expected the SAME run back, got %q then %q", first.ID, second.ID)
	}

	// One active run per note (the schema's own partial unique index) -
	// this is what makes "resumed" true rather than a second row existing.
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM note_checklist_runs WHERE document_id = ?`, note.ID).Scan(&count); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one run row, got %d", count)
	}
}

func TestDocumentStore_ChecklistRun_StartOnANoteWithNoChecklistItemsFails(t *testing.T) {
	store := withTestDocumentStore(t)
	note := mustCreateTestNote(t, "Just some prose about the boat.", "A note")

	_, _, err := store.StartOrResumeChecklistRun(note.ID)
	if !errors.Is(err, errNoteHasNoChecklist) {
		t.Fatalf("expected errNoteHasNoChecklist, got %v", err)
	}
}

func TestDocumentStore_ChecklistRun_StartOnANonNoteDocumentFails(t *testing.T) {
	store := withTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-plain-file", "manual.pdf", nil)

	_, _, err := store.StartOrResumeChecklistRun(doc.ID)
	if !errors.Is(err, errNotANote) {
		t.Fatalf("expected errNotANote, got %v", err)
	}
}

func TestDocumentStore_ChecklistRun_StartOnAMissingNoteFails(t *testing.T) {
	store := withTestDocumentStore(t)

	_, _, err := store.StartOrResumeChecklistRun("does-not-exist")
	if !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected errDocumentNotFound, got %v", err)
	}
}

// TestDocumentStore_ChecklistRun_XInBodyDoesNotPreTickANewRun is plan §3's
// "the note is the template; the run is the state" rule, the one test the
// plan calls out by name: a body already containing "[x]" (perhaps
// hand-authored, or copied from an old plain-Markdown checklist) must not
// pre-tick a fresh run. Every new run starts unticked, full stop.
func TestDocumentStore_ChecklistRun_XInBodyDoesNotPreTickANewRun(t *testing.T) {
	store := withTestDocumentStore(t)
	note := mustCreateTestNote(t, "- [x] Already ticked in the file\n- [ ] Not ticked", "Shutdown")

	run, _, err := store.StartOrResumeChecklistRun(note.ID)
	if err != nil {
		t.Fatalf("StartOrResumeChecklistRun: %v", err)
	}
	if run.CheckedCount != 0 {
		t.Fatalf("expected a fresh run to start with nothing ticked, got checked_count=%d", run.CheckedCount)
	}
	for _, item := range run.Items {
		if item.Checked {
			t.Fatalf("expected every item unticked on a fresh run, but %q was checked", item.Text)
		}
	}
}

// ── tick survives an edit, a changed item is reported ───────────────────

func TestDocumentStore_ChecklistRun_TickSurvivesUnrelatedEditAndChangedItemIsReported(t *testing.T) {
	store := withTestDocumentStore(t)
	note := mustCreateTestNote(t, "- [ ] Seacocks open\n- [ ] Check bilge\n- [ ] Start blower", "Shutdown")

	run, _, err := store.StartOrResumeChecklistRun(note.ID)
	if err != nil {
		t.Fatalf("StartOrResumeChecklistRun: %v", err)
	}

	var seacocksKey string
	var seacocksOccurrence int
	var bilgeKey string
	var bilgeOccurrence int
	for _, item := range run.Items {
		switch item.Text {
		case "Seacocks open":
			seacocksKey, seacocksOccurrence = item.ItemKey, item.Occurrence
		case "Check bilge":
			bilgeKey, bilgeOccurrence = item.ItemKey, item.Occurrence
		}
	}
	if seacocksKey == "" || bilgeKey == "" {
		t.Fatalf("expected to find both items by text in %+v", run.Items)
	}

	// Both items are ticked before the edit - Seacocks to prove its tick
	// SURVIVES an unrelated reorder+edit, Check bilge to prove ITS tick
	// becomes a changed entry once the edit removes the exact text it was
	// ticked against.
	if _, err := store.TickChecklistItem(run.ID, seacocksKey, seacocksOccurrence, true); err != nil {
		t.Fatalf("TickChecklistItem (seacocks): %v", err)
	}
	if _, err := store.TickChecklistItem(run.ID, bilgeKey, bilgeOccurrence, true); err != nil {
		t.Fatalf("TickChecklistItem (bilge): %v", err)
	}

	// An edit that (a) reorders the list and (b) changes "Check bilge" to
	// different text entirely - neither should touch the Seacocks tick.
	mustPatchTestNoteBody(t, note.ID, "- [ ] Start blower\n- [ ] Seacocks open\n- [ ] Check the bilge pump")

	updated, found, err := store.ActiveChecklistRun(note.ID)
	if err != nil {
		t.Fatalf("ActiveChecklistRun: %v", err)
	}
	if !found {
		t.Fatalf("expected the run still to be active")
	}

	var seacocksAfter *checklistItemView
	var bilgePumpAfter *checklistItemView
	for i := range updated.Items {
		switch updated.Items[i].Text {
		case "Seacocks open":
			seacocksAfter = &updated.Items[i]
		case "Check the bilge pump":
			bilgePumpAfter = &updated.Items[i]
		}
	}
	if seacocksAfter == nil {
		t.Fatalf("expected Seacocks open to still be a current item, got %+v", updated.Items)
	}
	if !seacocksAfter.Checked {
		t.Fatalf("expected the Seacocks open tick to survive the unrelated reorder+edit")
	}
	if bilgePumpAfter == nil {
		t.Fatalf("expected the edited line to appear as a current, unticked item, got %+v", updated.Items)
	}
	if bilgePumpAfter.Checked {
		t.Fatalf("expected the edited line to be unticked - it is a different item from the old one")
	}

	if len(updated.Changed) != 1 {
		t.Fatalf("expected exactly one changed entry, got %+v", updated.Changed)
	}
	if updated.Changed[0].Text != "Check bilge" {
		t.Fatalf("expected the changed entry to show the STORED text, got %q", updated.Changed[0].Text)
	}
	if updated.Changed[0].ItemKey != bilgeKey {
		t.Fatalf("expected the changed entry's key to be the old tick's key")
	}
}

// ── cascade on delete ─────────────────────────────────────────────────────

func TestDocumentStore_ChecklistRun_CascadesOnDocumentDelete(t *testing.T) {
	store := withTestDocumentStore(t)
	note := mustCreateTestNote(t, "- [ ] Seacocks open", "Shutdown")

	run, _, err := store.StartOrResumeChecklistRun(note.ID)
	if err != nil {
		t.Fatalf("StartOrResumeChecklistRun: %v", err)
	}
	if _, err := store.TickChecklistItem(run.ID, run.Items[0].ItemKey, run.Items[0].Occurrence, true); err != nil {
		t.Fatalf("TickChecklistItem: %v", err)
	}

	if _, err := store.Delete(note.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var runCount, tickCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM note_checklist_runs WHERE document_id = ?`, note.ID).Scan(&runCount); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM note_checklist_run_ticks WHERE run_id = ?`, run.ID).Scan(&tickCount); err != nil {
		t.Fatalf("count ticks: %v", err)
	}
	if runCount != 0 {
		t.Fatalf("expected the run to cascade-delete with its document, got %d rows", runCount)
	}
	if tickCount != 0 {
		t.Fatalf("expected ticks to cascade-delete with their run, got %d rows", tickCount)
	}
}

// ── closed runs reject further ticks ─────────────────────────────────────

func TestDocumentStore_ChecklistRun_ClosedRunRejectsFurtherTicks(t *testing.T) {
	store := withTestDocumentStore(t)
	note := mustCreateTestNote(t, "- [ ] Seacocks open", "Shutdown")

	run, _, err := store.StartOrResumeChecklistRun(note.ID)
	if err != nil {
		t.Fatalf("StartOrResumeChecklistRun: %v", err)
	}
	if _, err := store.CompleteChecklistRun(run.ID); err != nil {
		t.Fatalf("CompleteChecklistRun: %v", err)
	}

	_, err = store.TickChecklistItem(run.ID, run.Items[0].ItemKey, run.Items[0].Occurrence, true)
	if !errors.Is(err, errChecklistRunClosed) {
		t.Fatalf("expected errChecklistRunClosed, got %v", err)
	}

	// A completed run's own unique index no longer blocks a fresh start.
	fresh, resumed, err := store.StartOrResumeChecklistRun(note.ID)
	if err != nil {
		t.Fatalf("StartOrResumeChecklistRun after complete: %v", err)
	}
	if resumed {
		t.Fatalf("expected a brand new run once the old one is closed, not a resume")
	}
	if fresh.ID == run.ID {
		t.Fatalf("expected a different run id")
	}
}

func TestDocumentStore_ChecklistRun_AbandonSetsAbandonedAtAndNeverDeletesTheRow(t *testing.T) {
	store := withTestDocumentStore(t)
	note := mustCreateTestNote(t, "- [ ] Seacocks open", "Shutdown")

	run, _, err := store.StartOrResumeChecklistRun(note.ID)
	if err != nil {
		t.Fatalf("StartOrResumeChecklistRun: %v", err)
	}
	if err := store.AbandonChecklistRun(run.ID); err != nil {
		t.Fatalf("AbandonChecklistRun: %v", err)
	}

	var abandonedAt *int64
	if err := store.db.QueryRow(`SELECT abandoned_at FROM note_checklist_runs WHERE id = ?`, run.ID).Scan(&abandonedAt); err != nil {
		t.Fatalf("select abandoned_at: %v", err)
	}
	if abandonedAt == nil {
		t.Fatalf("expected abandoned_at to be set, not the row removed")
	}

	if _, found, err := store.ActiveChecklistRun(note.ID); err != nil || found {
		t.Fatalf("expected no active run after abandon: found=%v err=%v", found, err)
	}
}

// ── item/run not-found sentinels ─────────────────────────────────────────

func TestDocumentStore_ChecklistRun_TickUnknownRunFails(t *testing.T) {
	store := withTestDocumentStore(t)
	_, err := store.TickChecklistItem("does-not-exist", "somekey", 0, true)
	if !errors.Is(err, errChecklistRunNotFound) {
		t.Fatalf("expected errChecklistRunNotFound, got %v", err)
	}
}

func TestDocumentStore_ChecklistRun_TickUnknownItemKeyFails(t *testing.T) {
	store := withTestDocumentStore(t)
	note := mustCreateTestNote(t, "- [ ] Seacocks open", "Shutdown")
	run, _, err := store.StartOrResumeChecklistRun(note.ID)
	if err != nil {
		t.Fatalf("StartOrResumeChecklistRun: %v", err)
	}

	_, err = store.TickChecklistItem(run.ID, "not-a-real-item-key", 0, true)
	if !errors.Is(err, errChecklistItemNotFound) {
		t.Fatalf("expected errChecklistItemNotFound, got %v", err)
	}
}

func TestDocumentStore_ChecklistRun_UntickingRemovesTheTick(t *testing.T) {
	store := withTestDocumentStore(t)
	note := mustCreateTestNote(t, "- [ ] Seacocks open", "Shutdown")
	run, _, err := store.StartOrResumeChecklistRun(note.ID)
	if err != nil {
		t.Fatalf("StartOrResumeChecklistRun: %v", err)
	}
	item := run.Items[0]

	if _, err := store.TickChecklistItem(run.ID, item.ItemKey, item.Occurrence, true); err != nil {
		t.Fatalf("tick: %v", err)
	}
	updated, err := store.TickChecklistItem(run.ID, item.ItemKey, item.Occurrence, false)
	if err != nil {
		t.Fatalf("untick: %v", err)
	}
	if updated.Items[0].Checked {
		t.Fatalf("expected the item to be unticked")
	}
	if updated.CheckedCount != 0 {
		t.Fatalf("expected checked_count 0 after unticking, got %d", updated.CheckedCount)
	}
}
