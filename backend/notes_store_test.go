package main

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// ── migration ────────────────────────────────────────────────────────────

// preNotesDocumentsSchema is a snapshot of documents_store.go's documents/
// document_folders CREATE TABLE statements exactly as they were before the
// notes feature (ADR 0106, pre-ADR-0114) - no kind/note_type/
// note_type_source/pinned/sort_index/role columns at all. Used only to
// build a fixture database that predates applyDocumentStoreMigrations, so
// the migration's upgrade path (as opposed to its "already there, tolerate
// the duplicate" path on a fresh database) is actually exercised.
var preNotesDocumentsSchema = []string{
	`CREATE TABLE IF NOT EXISTS document_folders (
		id         TEXT PRIMARY KEY,
		parent_id  TEXT REFERENCES document_folders(id),
		name       TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS documents (
		id             TEXT PRIMARY KEY,
		sha256         TEXT NOT NULL UNIQUE,
		folder_id      TEXT REFERENCES document_folders(id) ON DELETE RESTRICT,
		filename       TEXT NOT NULL,
		title          TEXT NOT NULL DEFAULT '',
		notes          TEXT NOT NULL DEFAULT '',
		mime           TEXT NOT NULL DEFAULT '',
		size_bytes     INTEGER NOT NULL DEFAULT 0,
		page_count     INTEGER NOT NULL DEFAULT 0,
		summary        TEXT NOT NULL DEFAULT '',
		markdown       TEXT NOT NULL DEFAULT '',
		status         TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','indexed','failed')),
		stage          TEXT NOT NULL DEFAULT 'extract' CHECK (stage IN ('extract','enrich','done')),
		enrich         INTEGER NOT NULL DEFAULT 0 CHECK (enrich IN (0,1)),
		force_ocr      INTEGER NOT NULL DEFAULT 0 CHECK (force_ocr IN (0,1)),
		indexed_with   TEXT NOT NULL DEFAULT '' CHECK (indexed_with IN ('','local','mate')),
		error          TEXT NOT NULL DEFAULT '',
		index_model    TEXT NOT NULL DEFAULT '',
		index_cost_usd REAL NOT NULL DEFAULT 0,
		reindex_seq    INTEGER NOT NULL DEFAULT 0,
		created_at     INTEGER NOT NULL,
		updated_at     INTEGER NOT NULL,
		indexed_at     INTEGER
	)`,
}

func TestApplyDocumentStoreMigrations_PreMigrationRowsBackfillToKindFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "documents.sqlite")

	// Build a fixture database using the OLD schema only - no kind/
	// note_type/etc columns - and insert one row directly, bypassing
	// documentStore entirely (it doesn't exist yet in this schema).
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	for _, stmt := range preNotesDocumentsSchema {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("create pre-notes schema: %v", err)
		}
	}
	if _, err := raw.Exec(
		`INSERT INTO documents (id, sha256, filename, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"old-doc", "old-sha", "manual.pdf", time.Now().Unix(), time.Now().Unix(),
	); err != nil {
		t.Fatalf("insert pre-migration row: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw sqlite: %v", err)
	}

	// Now open it the normal way - newDocumentStore's schema loop is a
	// no-op against these already-existing tables, and
	// applyDocumentStoreMigrations is what has to actually add the new
	// columns to them.
	store, err := newDocumentStore(path)
	if err != nil {
		t.Fatalf("newDocumentStore on a pre-notes database: %v", err)
	}
	defer store.Close()

	doc, err := store.Get("old-doc")
	if err != nil {
		t.Fatalf("Get(old-doc): %v", err)
	}
	if doc.Kind != "file" {
		t.Fatalf("expected a pre-migration row to backfill to kind=%q, got %q", "file", doc.Kind)
	}
	if doc.NoteType != "" || doc.NoteTypeSource != "" || doc.Pinned || doc.SortIndex != 0 {
		t.Fatalf("expected every other new column to backfill to its zero value, got %+v", doc)
	}
}

// TestApplyDocumentStoreMigrations_PreMigrationLibraryStillListsInNameOrder
// is the Verification section's own required case for the manuals feature
// (plan §2/§3): ListFolder now orders by sort_index before name, and every
// row on a database that predates this migration backfills sort_index to
// its DEFAULT 0 (the same backfill the test above pins for kind). A boat's
// real, years-old documents.sqlite must list exactly as it always has -
// alphabetically - the very first time it's opened under the new code, not
// only after every row is individually reordered by hand.
func TestApplyDocumentStoreMigrations_PreMigrationLibraryStillListsInNameOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "documents.sqlite")

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	for _, stmt := range preNotesDocumentsSchema {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("create pre-notes schema: %v", err)
		}
	}
	now := time.Now().Unix()
	// Two folders and two documents, inserted in the OPPOSITE of
	// alphabetical order - if sort_index's absence from this pre-migration
	// schema didn't backfill cleanly to 0 for every row, this would sort
	// however insertion order (or something else) left it instead.
	if _, err := raw.Exec(`INSERT INTO document_folders (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		"folder-zebra", "Zebra", now, now); err != nil {
		t.Fatalf("insert pre-migration folder: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO document_folders (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		"folder-anchor", "Anchor", now, now); err != nil {
		t.Fatalf("insert pre-migration folder: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO documents (id, sha256, filename, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"doc-zebra", "sha-zebra", "z-doc.pdf", now, now); err != nil {
		t.Fatalf("insert pre-migration document: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO documents (id, sha256, filename, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"doc-anchor", "sha-anchor", "a-doc.pdf", now, now); err != nil {
		t.Fatalf("insert pre-migration document: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw sqlite: %v", err)
	}

	store, err := newDocumentStore(path)
	if err != nil {
		t.Fatalf("newDocumentStore on a pre-notes database: %v", err)
	}
	defer store.Close()

	folders, docs, err := store.ListFolder(nil)
	if err != nil {
		t.Fatalf("ListFolder: %v", err)
	}
	if len(folders) != 2 || folders[0].Name != "Anchor" || folders[1].Name != "Zebra" {
		t.Fatalf("expected plain alphabetical order (Anchor, Zebra), got %+v", folders)
	}
	if len(docs) != 2 || docs[0].Filename != "a-doc.pdf" || docs[1].Filename != "z-doc.pdf" {
		t.Fatalf("expected plain alphabetical order (a-doc.pdf, z-doc.pdf), got %+v", docs)
	}
}

func TestApplyDocumentStoreMigrations_IdempotentAcrossTwoOpens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "documents.sqlite")

	store1, err := newDocumentStore(path)
	if err != nil {
		t.Fatalf("newDocumentStore (1st open): %v", err)
	}
	if _, err := store1.InsertNote(document{SHA256: "sha-a", Filename: "a.md", MIME: "text/markdown"}); err != nil {
		t.Fatalf("InsertNote: %v", err)
	}
	if err := store1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Re-opening an already-migrated database must not error - every ALTER
	// TABLE in applyDocumentStoreMigrations hits "duplicate column name"
	// this time, and that has to be tolerated, not just on a fresh
	// database.
	store2, err := newDocumentStore(path)
	if err != nil {
		t.Fatalf("newDocumentStore (2nd open, already migrated): %v", err)
	}
	defer store2.Close()

	doc, ok, err := store2.GetBySHA("sha-a")
	if err != nil || !ok {
		t.Fatalf("GetBySHA after reopen: ok=%v err=%v", ok, err)
	}
	if doc.Kind != "note" {
		t.Fatalf("expected the note inserted before reopening to persist as kind=note, got %q", doc.Kind)
	}
}

// ── InsertNote ───────────────────────────────────────────────────────────

func TestInsertNote_SetsKindAndHonoursPreSetIDAndCreatedAt(t *testing.T) {
	store := newTestDocumentStore(t)

	id := "0f3b1c2e-1234-4abc-8def-1234567890ab"
	created := time.Date(2026, 9, 20, 4, 11, 7, 0, time.UTC)

	doc, err := store.InsertNote(document{
		ID:        id,
		SHA256:    "note-sha-1",
		Filename:  "Genset start-up.md",
		Title:     "Genset start-up",
		MIME:      "text/markdown",
		CreatedAt: created,
		NoteType:  "procedure",
	})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}
	if doc.ID != id {
		t.Fatalf("expected InsertNote to honour the pre-set id %q, got %q", id, doc.ID)
	}
	if !doc.CreatedAt.Equal(created) {
		t.Fatalf("expected InsertNote to honour the pre-set CreatedAt %v, got %v", created, doc.CreatedAt)
	}
	if doc.Kind != "note" {
		t.Fatalf("expected Kind=note, got %q", doc.Kind)
	}
	if doc.NoteType != "procedure" {
		t.Fatalf("expected NoteType=procedure, got %q", doc.NoteType)
	}

	got, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Kind != "note" || got.ID != id {
		t.Fatalf("expected the note to read back as kind=note with the same id, got %+v", got)
	}
}

func TestInsertNote_NoIDGeneratesOne(t *testing.T) {
	store := newTestDocumentStore(t)
	doc, err := store.InsertNote(document{SHA256: "note-sha-2", Filename: "note.md", MIME: "text/markdown"})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}
	if doc.ID == "" {
		t.Fatalf("expected InsertNote to generate an id when none was given")
	}
}

// ── ReplaceNoteBody ──────────────────────────────────────────────────────

func mustInsertTestNote(t *testing.T, store *documentStore, sha string) document {
	t.Helper()
	doc, err := store.InsertNote(document{SHA256: sha, Filename: "note.md", MIME: "text/markdown"})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}
	return doc
}

func TestReplaceNoteBody_RequeuesAndLeavesForceOCRAlone(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertTestNote(t, store, "note-sha-old")

	// Simulate a document that had already finished indexing, with
	// force_ocr set from some earlier explicit reindex - ReplaceNoteBody
	// must requeue it for a fresh extract pass WITHOUT disturbing that
	// flag either way.
	if err := store.SetIndexed(doc.ID, "local"); err != nil {
		t.Fatalf("SetIndexed: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE documents SET force_ocr = 1 WHERE id = ?`, doc.ID); err != nil {
		t.Fatalf("set force_ocr: %v", err)
	}

	oldSHA, err := store.ReplaceNoteBody(doc.ID, "note-sha-new", 42)
	if err != nil {
		t.Fatalf("ReplaceNoteBody: %v", err)
	}
	if oldSHA != "note-sha-old" {
		t.Fatalf("expected the old sha256 %q back, got %q", "note-sha-old", oldSHA)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SHA256 != "note-sha-new" {
		t.Fatalf("expected sha256 updated to %q, got %q", "note-sha-new", got.SHA256)
	}
	if got.SizeBytes != 42 {
		t.Fatalf("expected size_bytes=42, got %d", got.SizeBytes)
	}
	if got.Status != "pending" || got.Stage != "extract" {
		t.Fatalf("expected the document requeued to pending/extract, got status=%q stage=%q", got.Status, got.Stage)
	}
	if !got.ForceOCR {
		t.Fatalf("expected force_ocr to be left alone (still true), got false")
	}
	if got.ReindexSeq != doc.ReindexSeq+1 {
		t.Fatalf("expected reindex_seq bumped by exactly 1, got %d (was %d)", got.ReindexSeq, doc.ReindexSeq)
	}
}

func TestReplaceNoteBody_SameSHAIsANoOp(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertTestNote(t, store, "note-sha-same")

	before, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	oldSHA, err := store.ReplaceNoteBody(doc.ID, "note-sha-same", before.SizeBytes)
	if err != nil {
		t.Fatalf("ReplaceNoteBody: %v", err)
	}
	if oldSHA != "note-sha-same" {
		t.Fatalf("expected the unchanged sha back, got %q", oldSHA)
	}

	after, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.UpdatedAt != before.UpdatedAt {
		t.Fatalf("expected a true no-op to leave updated_at untouched, before=%v after=%v", before.UpdatedAt, after.UpdatedAt)
	}
	if after.ReindexSeq != before.ReindexSeq {
		t.Fatalf("expected a true no-op to leave reindex_seq untouched, before=%d after=%d", before.ReindexSeq, after.ReindexSeq)
	}
}

func TestReplaceNoteBody_CollisionWithAnotherRowIsADuplicate(t *testing.T) {
	store := newTestDocumentStore(t)
	other := mustInsertDocument(t, store, "existing-file-sha", "manual.pdf", nil)
	note := mustInsertTestNote(t, store, "note-sha-a")

	_, err := store.ReplaceNoteBody(note.ID, other.SHA256, 10)
	if !errors.Is(err, errDocumentDuplicate) {
		t.Fatalf("expected errDocumentDuplicate, got %v", err)
	}

	// The note's own row must be untouched by the rejected edit.
	got, err := store.Get(note.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SHA256 != "note-sha-a" {
		t.Fatalf("expected the note's sha256 unchanged after a rejected collision, got %q", got.SHA256)
	}
}

func TestReplaceNoteBody_UnknownIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	_, err := store.ReplaceNoteBody("does-not-exist", "sha", 1)
	if !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected errDocumentNotFound, got %v", err)
	}
}

func TestReplaceNoteBody_KindFileIDReturnsNotANote(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "a-file-sha", "manual.pdf", nil)

	_, err := store.ReplaceNoteBody(doc.ID, "new-sha", 1)
	if !errors.Is(err, errNotANote) {
		t.Fatalf("expected errNotANote, got %v", err)
	}
}

// ── SetNoteTypeIfNotOperator ─────────────────────────────────────────────

func TestSetNoteTypeIfNotOperator_OperatorChoiceSurvivesReclassification(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertTestNote(t, store, "note-sha-type-1")

	if err := store.SetNoteTypeIfNotOperator(doc.ID, "contact", "operator"); err != nil {
		t.Fatalf("SetNoteTypeIfNotOperator(operator): %v", err)
	}

	// A later automatic reclassification (as would run after a body edit)
	// must not overwrite the operator's own choice.
	if err := store.SetNoteTypeIfNotOperator(doc.ID, "recipe", "auto"); err != nil {
		t.Fatalf("SetNoteTypeIfNotOperator(auto): %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.NoteType != "contact" || got.NoteTypeSource != "operator" {
		t.Fatalf("expected the operator's classification to survive, got type=%q source=%q", got.NoteType, got.NoteTypeSource)
	}
}

func TestSetNoteTypeIfNotOperator_AutoWritesWhenNothingIsSet(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertTestNote(t, store, "note-sha-type-2")

	if err := store.SetNoteTypeIfNotOperator(doc.ID, "spec", "auto"); err != nil {
		t.Fatalf("SetNoteTypeIfNotOperator: %v", err)
	}
	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.NoteType != "spec" || got.NoteTypeSource != "auto" {
		t.Fatalf("expected type=spec source=auto, got type=%q source=%q", got.NoteType, got.NoteTypeSource)
	}
}

func TestSetNoteTypeIfNotOperator_MateUpgradesAuto(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertTestNote(t, store, "note-sha-type-3")

	if err := store.SetNoteTypeIfNotOperator(doc.ID, "spec", "auto"); err != nil {
		t.Fatalf("SetNoteTypeIfNotOperator(auto): %v", err)
	}
	if err := store.SetNoteTypeIfNotOperator(doc.ID, "spec", "mate"); err != nil {
		t.Fatalf("SetNoteTypeIfNotOperator(mate): %v", err)
	}
	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.NoteTypeSource != "mate" {
		t.Fatalf("expected mate to upgrade auto, got source=%q", got.NoteTypeSource)
	}
}

func TestSetNoteTypeIfNotOperator_KindFileIDReturnsNotANote(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "a-file-sha-2", "manual.pdf", nil)

	err := store.SetNoteTypeIfNotOperator(doc.ID, "spec", "auto")
	if !errors.Is(err, errNotANote) {
		t.Fatalf("expected errNotANote, got %v", err)
	}
}

// ── ListNotes ────────────────────────────────────────────────────────────

func TestListNotes_OnlyReturnsNotesNotFiles(t *testing.T) {
	store := newTestDocumentStore(t)
	mustInsertDocument(t, store, "file-sha", "manual.pdf", nil)
	note := mustInsertTestNote(t, store, "note-sha")

	notes, err := store.ListNotes(false, "", "", 50, 0)
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(notes) != 1 || notes[0].ID != note.ID {
		t.Fatalf("expected exactly the one note back, got %+v", notes)
	}
}

func TestListNotes_UnfiledOnlyIsTheInbox(t *testing.T) {
	store := newTestDocumentStore(t)
	folder, err := store.CreateFolder("Contacts", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	filed, err := store.InsertNote(document{SHA256: "note-filed", Filename: "a.md", MIME: "text/markdown", FolderID: &folder.ID})
	if err != nil {
		t.Fatalf("InsertNote (filed): %v", err)
	}
	unfiled := mustInsertTestNote(t, store, "note-unfiled")

	inbox, err := store.ListNotes(true, "", "", 50, 0)
	if err != nil {
		t.Fatalf("ListNotes(unfiledOnly): %v", err)
	}
	if len(inbox) != 1 || inbox[0].ID != unfiled.ID {
		t.Fatalf("expected only the unfiled note in the inbox, got %+v", inbox)
	}

	all, err := store.ListNotes(false, "", "", 50, 0)
	if err != nil {
		t.Fatalf("ListNotes(all): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected both notes without the inbox filter, got %d", len(all))
	}
	_ = filed
}

func TestListNotes_FiltersByTypeAndTag(t *testing.T) {
	store := newTestDocumentStore(t)
	a, err := store.InsertNote(document{SHA256: "note-a", Filename: "a.md", MIME: "text/markdown", NoteType: "procedure", OperatorTags: []string{"engine"}})
	if err != nil {
		t.Fatalf("InsertNote a: %v", err)
	}
	_, err = store.InsertNote(document{SHA256: "note-b", Filename: "b.md", MIME: "text/markdown", NoteType: "recipe"})
	if err != nil {
		t.Fatalf("InsertNote b: %v", err)
	}

	byType, err := store.ListNotes(false, "procedure", "", 50, 0)
	if err != nil {
		t.Fatalf("ListNotes(type): %v", err)
	}
	if len(byType) != 1 || byType[0].ID != a.ID {
		t.Fatalf("expected only the procedure note, got %+v", byType)
	}

	byTag, err := store.ListNotes(false, "", "engine", 50, 0)
	if err != nil {
		t.Fatalf("ListNotes(tag): %v", err)
	}
	if len(byTag) != 1 || byTag[0].ID != a.ID {
		t.Fatalf("expected only the tagged note, got %+v", byTag)
	}
}

func TestListNotes_NewestFirst(t *testing.T) {
	store := newTestDocumentStore(t)
	t0 := time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC)
	store.now = sequencedClock(t0, t0.Add(time.Minute), t0.Add(2*time.Minute))

	first, err := store.InsertNote(document{SHA256: "note-1", Filename: "1.md", MIME: "text/markdown"})
	if err != nil {
		t.Fatalf("InsertNote 1: %v", err)
	}
	second, err := store.InsertNote(document{SHA256: "note-2", Filename: "2.md", MIME: "text/markdown"})
	if err != nil {
		t.Fatalf("InsertNote 2: %v", err)
	}

	notes, err := store.ListNotes(false, "", "", 50, 0)
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(notes) != 2 || notes[0].ID != second.ID || notes[1].ID != first.ID {
		t.Fatalf("expected newest-first order [%s, %s], got %+v", second.ID, first.ID, notes)
	}
}

// ── SetNotePinned / PinnedNotes (plan §8: Mate's pinned-note prompt) ────

func TestSetNotePinned_TogglesTheFlag(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertTestNote(t, store, "note-pin-1")

	if err := store.SetNotePinned(doc.ID, true); err != nil {
		t.Fatalf("SetNotePinned(true): %v", err)
	}
	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Pinned {
		t.Fatalf("expected pinned=true after SetNotePinned(true)")
	}

	if err := store.SetNotePinned(doc.ID, false); err != nil {
		t.Fatalf("SetNotePinned(false): %v", err)
	}
	got, err = store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Pinned {
		t.Fatalf("expected pinned=false after SetNotePinned(false)")
	}
}

func TestSetNotePinned_KindFileIDReturnsNotANote(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "a-file-sha-pin", "manual.pdf", nil)

	if err := store.SetNotePinned(doc.ID, true); !errors.Is(err, errNotANote) {
		t.Fatalf("expected errNotANote, got %v", err)
	}
}

func TestSetNotePinned_UnknownIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if err := store.SetNotePinned("does-not-exist", true); !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected errDocumentNotFound, got %v", err)
	}
}

// TestPinnedNotes_OnlyPinningSelectsANote is the store-level guard the plan
// asks for explicitly: pinning is what selects a note for PinnedNotes, and
// an unpinned note - however recent, however interesting - must never come
// back from this call. assistant_prompt_test.go's
// TestCollectAssistantPromptContext_PinnedNoteReachesPromptUnpinnedDoesNot
// re-asserts the same thing one layer up, all the way into the rendered
// prompt.
func TestPinnedNotes_OnlyPinningSelectsANote(t *testing.T) {
	store := newTestDocumentStore(t)
	pinned := mustInsertTestNote(t, store, "note-pinned")
	unpinned := mustInsertTestNote(t, store, "note-unpinned")
	_ = mustInsertDocument(t, store, "file-not-a-note", "manual.pdf", nil)

	if err := store.SetNotePinned(pinned.ID, true); err != nil {
		t.Fatalf("SetNotePinned: %v", err)
	}

	got, err := store.PinnedNotes()
	if err != nil {
		t.Fatalf("PinnedNotes: %v", err)
	}
	if len(got) != 1 || got[0].ID != pinned.ID {
		t.Fatalf("expected exactly the one pinned note back, got %+v", got)
	}
	for _, d := range got {
		if d.ID == unpinned.ID {
			t.Fatalf("expected the unpinned note to never be returned by PinnedNotes")
		}
	}
}

// ── NoteClassifyBackfillCandidates (plan §9's classify backfill) ────────

func TestNoteClassifyBackfillCandidates_SkipsOperatorSource(t *testing.T) {
	store := newTestDocumentStore(t)
	operatorNote, err := store.InsertNote(document{SHA256: "note-op", Filename: "op.md", MIME: "text/markdown", NoteType: "spec", NoteTypeSource: "operator"})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}
	autoNote, err := store.InsertNote(document{SHA256: "note-auto", Filename: "auto.md", MIME: "text/markdown", NoteType: "note", NoteTypeSource: "auto"})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}
	blankNote, err := store.InsertNote(document{SHA256: "note-blank", Filename: "blank.md", MIME: "text/markdown"})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}

	got, err := store.NoteClassifyBackfillCandidates()
	if err != nil {
		t.Fatalf("NoteClassifyBackfillCandidates: %v", err)
	}
	ids := map[string]bool{}
	for _, d := range got {
		ids[d.ID] = true
	}
	if ids[operatorNote.ID] {
		t.Fatalf("expected an operator-sourced note to be excluded from backfill candidates")
	}
	if !ids[autoNote.ID] || !ids[blankNote.ID] {
		t.Fatalf("expected auto and blank-source notes to be candidates, got %+v", got)
	}
}

// ── EnrichBackfillNotes / NoteEnrichBackfillCounts (plan §9's enrich backfill) ──

func TestNoteEnrichBackfillCounts_OnlyCountsUnenrichedNotes(t *testing.T) {
	store := newTestDocumentStore(t)
	if _, err := store.InsertNote(document{SHA256: "note-unenriched", Filename: "a.md", MIME: "text/markdown", Markdown: "hello world", Enrich: false}); err != nil {
		t.Fatalf("InsertNote: %v", err)
	}
	if _, err := store.InsertNote(document{SHA256: "note-enriched", Filename: "b.md", MIME: "text/markdown", Markdown: "already enriched", Enrich: true}); err != nil {
		t.Fatalf("InsertNote: %v", err)
	}
	if _, err := store.Insert(document{SHA256: "file-unenriched", Filename: "c.pdf", MIME: "application/pdf", Markdown: "not a note", Enrich: false}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	counts, err := store.NoteEnrichBackfillCounts()
	if err != nil {
		t.Fatalf("NoteEnrichBackfillCounts: %v", err)
	}
	if counts.NotesPending != 1 {
		t.Fatalf("expected exactly one pending note, got %d", counts.NotesPending)
	}
	if counts.CharsPending != len("hello world") {
		t.Fatalf("expected CharsPending %d, got %d", len("hello world"), counts.CharsPending)
	}
}

func TestEnrichBackfillNotes_SetsEnrichAndLeavesAlreadyEnrichedAlone(t *testing.T) {
	store := newTestDocumentStore(t)
	pending, err := store.InsertNote(document{SHA256: "note-pending-enrich", Filename: "a.md", MIME: "text/markdown", Enrich: false})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}
	already, err := store.InsertNote(document{SHA256: "note-already-enrich", Filename: "b.md", MIME: "text/markdown", Enrich: true})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}

	n, err := store.EnrichBackfillNotes()
	if err != nil {
		t.Fatalf("EnrichBackfillNotes: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly one note flipped, got %d", n)
	}

	got, err := store.Get(pending.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Enrich {
		t.Fatalf("expected the pending note's enrich flag to now be set")
	}

	gotAlready, err := store.Get(already.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !gotAlready.Enrich {
		t.Fatalf("expected the already-enriched note to remain enrich=true")
	}
}

func TestNoteEnrichBackfillCounts_DryRunSetsNothing(t *testing.T) {
	store := newTestDocumentStore(t)
	doc, err := store.InsertNote(document{SHA256: "note-dry-run", Filename: "a.md", MIME: "text/markdown", Enrich: false})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}

	if _, err := store.NoteEnrichBackfillCounts(); err != nil {
		t.Fatalf("NoteEnrichBackfillCounts: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Enrich {
		t.Fatalf("expected the dry-run count query to set nothing, but enrich is now true")
	}
}

// The classify backfill rewrites a note's frontmatter (its `type:` line) so
// a portable backup never disagrees with the database. That rewrite changes
// the blob's bytes, and therefore its sha256 - but NOT one byte of what the
// indexer actually reads, because extractTextFile strips the frontmatter
// fence before extracting (documents_extract.go). Re-extracting would
// produce byte-identical chunks and byte-identical embeddings, at full
// OpenRouter cost, for every note the backfill touches.
//
// That matters because the classify backfill is the one the plan describes
// as costing nothing and which therefore ships with no "this bills you"
// confirmation, unlike the enrich backfill beside it. If it silently
// requeued indexing it would quietly bill an operator who was told it was
// free.
func TestReplaceNoteFrontmatter_DoesNotRequeueIndexing(t *testing.T) {
	store := newTestDocumentStore(t)

	doc, err := store.InsertNote(document{
		SHA256:   "sha-original",
		Filename: "Genset.md",
		Title:    "Genset",
		MIME:     "text/markdown",
		NoteType: "note",
	})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}

	// Pretend the indexer already finished with it.
	if _, err := store.db.Exec(
		`UPDATE documents SET status = 'indexed', stage = 'done', reindex_seq = 7 WHERE id = ?`, doc.ID,
	); err != nil {
		t.Fatalf("seed indexed state: %v", err)
	}

	if _, err := store.ReplaceNoteFrontmatter(doc.ID, "sha-original", "sha-reclassified", "note", 104); err != nil {
		t.Fatalf("ReplaceNoteFrontmatter: %v", err)
	}

	var sha, status, stage string
	var reindexSeq int
	if err := store.db.QueryRow(
		`SELECT sha256, status, stage, reindex_seq FROM documents WHERE id = ?`, doc.ID,
	).Scan(&sha, &status, &stage, &reindexSeq); err != nil {
		t.Fatalf("read back: %v", err)
	}

	if sha != "sha-reclassified" {
		t.Errorf("sha256 = %q, want the rewritten blob's sha - disk and DB must not drift", sha)
	}
	if status != "indexed" || stage != "done" {
		t.Errorf("status/stage = %q/%q, want indexed/done - a frontmatter-only rewrite must not requeue extraction", status, stage)
	}
	if reindexSeq != 7 {
		t.Errorf("reindex_seq = %d, want 7 - re-embedding a note whose indexed text did not change is pure spend", reindexSeq)
	}
}

// The classify backfill loops over a snapshot of candidates taken once, then
// rewrites each note's frontmatter from that snapshot. On a boat that is one
// operator clicking Classify and carrying on editing notes in the same panel
// while it runs - entirely ordinary use of the UI this ships with.
//
// Without a compare-and-swap, a note edited after the snapshot was taken gets
// its row pointed back at the stale blob AND has the blob holding the fresh
// edit handed back as "the superseded one" for the caller to delete. The edit
// is then gone from disk, not merely from the row.
//
// So the write must refuse when the note moved underfoot, and say so, rather
// than overwrite (AGENTS.md: fail fast, never mask an upstream change).
func TestReplaceNoteFrontmatter_RefusesWhenTheNoteChangedUnderfoot(t *testing.T) {
	store := newTestDocumentStore(t)

	doc, err := store.InsertNote(document{
		SHA256: "sha-original", Filename: "Genset.md", Title: "Genset",
		MIME: "text/markdown", NoteType: "note",
	})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}

	// The operator edits the note after the backfill snapshotted it.
	if _, err := store.ReplaceNoteBody(doc.ID, "sha-operator-edit", 200); err != nil {
		t.Fatalf("operator edit: %v", err)
	}

	// The backfill now arrives holding the STALE sha.
	_, err = store.ReplaceNoteFrontmatter(doc.ID, "sha-original", "sha-reclassified", "note", 104)
	if !errors.Is(err, errNoteChangedUnderfoot) {
		t.Fatalf("want errNoteChangedUnderfoot, got %v", err)
	}

	var sha string
	if err := store.db.QueryRow(`SELECT sha256 FROM documents WHERE id = ?`, doc.ID).Scan(&sha); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if sha != "sha-operator-edit" {
		t.Errorf("sha256 = %q, want the operator's edit preserved - the backfill must not revert it", sha)
	}
}

// The type update has to land in the SAME transaction as the sha swap. Two
// separately-locked calls leave a window where the blob on disk already says
// the new type while the database still says the old one - the precise
// invariant reclassifyNote exists to maintain.
func TestReplaceNoteFrontmatter_SetsTypeAtomicallyAndLeavesOperatorTypeAlone(t *testing.T) {
	store := newTestDocumentStore(t)

	auto, err := store.InsertNote(document{
		SHA256: "auto-1", Filename: "a.md", Title: "A", MIME: "text/markdown",
		NoteType: "note", NoteTypeSource: "auto",
	})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}
	if _, err := store.ReplaceNoteFrontmatter(auto.ID, "auto-1", "auto-2", "procedure", 10); err != nil {
		t.Fatalf("ReplaceNoteFrontmatter: %v", err)
	}
	var noteType, source string
	if err := store.db.QueryRow(`SELECT note_type, note_type_source FROM documents WHERE id = ?`, auto.ID).Scan(&noteType, &source); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if noteType != "procedure" || source != "auto" {
		t.Errorf("note_type/source = %q/%q, want procedure/auto", noteType, source)
	}

	op, err := store.InsertNote(document{
		SHA256: "op-1", Filename: "b.md", Title: "B", MIME: "text/markdown",
		NoteType: "quirk", NoteTypeSource: "operator",
	})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}
	if _, err := store.ReplaceNoteFrontmatter(op.ID, "op-1", "op-2", "procedure", 10); err != nil {
		t.Fatalf("ReplaceNoteFrontmatter: %v", err)
	}
	if err := store.db.QueryRow(`SELECT note_type, note_type_source FROM documents WHERE id = ?`, op.ID).Scan(&noteType, &source); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if noteType != "quirk" || source != "operator" {
		t.Errorf("note_type/source = %q/%q, want quirk/operator - an operator override is sticky", noteType, source)
	}
}
