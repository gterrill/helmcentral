package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func newTestDocumentStore(t *testing.T) *documentStore {
	t.Helper()
	store, err := newDocumentStore(filepath.Join(t.TempDir(), "documents.sqlite"))
	if err != nil {
		t.Fatalf("newDocumentStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func mustInsertDocument(t *testing.T, store *documentStore, sha, filename string, folderID *string) document {
	t.Helper()
	doc, err := store.Insert(document{SHA256: sha, Filename: filename, FolderID: folderID, MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert(%q): %v", filename, err)
	}
	return doc
}

// ── store lifecycle ─────────────────────────────────────────────────────

func TestNewDocumentStore_ForeignKeysPragmaEnabled(t *testing.T) {
	store := newTestDocumentStore(t)

	var fk int
	if err := store.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Fatalf("expected foreign_keys pragma to be 1, got %d", fk)
	}
}

func TestNewDocumentStore_ReopeningSamePathIsIdempotentAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "documents.sqlite")

	store1, err := newDocumentStore(path)
	if err != nil {
		t.Fatalf("newDocumentStore (1st open): %v", err)
	}
	if _, err := store1.Insert(document{SHA256: "aaa", Filename: "manual.pdf", MIME: "application/pdf"}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := store1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	store2, err := newDocumentStore(path)
	if err != nil {
		t.Fatalf("newDocumentStore (2nd open, same path): %v", err)
	}
	defer store2.Close()

	doc, ok, err := store2.GetBySHA("aaa")
	if err != nil || !ok {
		t.Fatalf("GetBySHA: ok=%v err=%v", ok, err)
	}
	if doc.Filename != "manual.pdf" {
		t.Fatalf("expected the document created before reopening to persist, got %+v", doc)
	}
}

// ── documents ────────────────────────────────────────────────────────────

func TestDocumentStore_InsertAndGetRoundTrip(t *testing.T) {
	store := newTestDocumentStore(t)
	t0 := time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC)
	store.now = sequencedClock(t0)

	inserted, err := store.Insert(document{
		SHA256:   "sha-engine-manual",
		Filename: "engine-manual.pdf",
		Title:    "Engine Manual",
		MIME:     "application/pdf",
		Enrich:   true,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if inserted.ID == "" {
		t.Fatalf("expected Insert to assign an ID")
	}
	if inserted.Status != "pending" || inserted.Stage != "extract" {
		t.Fatalf("expected a fresh document to be pending/extract, got status=%q stage=%q", inserted.Status, inserted.Stage)
	}
	if !inserted.CreatedAt.Equal(t0) {
		t.Fatalf("expected CreatedAt %v, got %v", t0, inserted.CreatedAt)
	}

	got, err := store.Get(inserted.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SHA256 != "sha-engine-manual" || got.Filename != "engine-manual.pdf" || got.Title != "Engine Manual" {
		t.Fatalf("Get returned unexpected document: %+v", got)
	}
	if !got.Enrich {
		t.Fatalf("expected Enrich to round-trip true")
	}
}

func TestDocumentStore_GetUnknownIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	_, err := store.Get("does-not-exist")
	if !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected errDocumentNotFound, got %v", err)
	}
}

func TestDocumentStore_GetBySHAMissReturnsNotOk(t *testing.T) {
	store := newTestDocumentStore(t)
	_, ok, err := store.GetBySHA("does-not-exist")
	if err != nil {
		t.Fatalf("expected no error for a missing sha256, got %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for a missing sha256")
	}
}

func TestDocumentStore_InsertDuplicateSHA256ReturnsExistingDocument(t *testing.T) {
	store := newTestDocumentStore(t)

	first := mustInsertDocument(t, store, "dup-sha", "receipt.jpg", nil)

	dup, err := store.Insert(document{SHA256: "dup-sha", Filename: "receipt-again.jpg", MIME: "image/jpeg"})
	if !errors.Is(err, errDocumentDuplicate) {
		t.Fatalf("expected errDocumentDuplicate, got %v", err)
	}
	if dup.ID != first.ID {
		t.Fatalf("expected the duplicate response to carry the existing document's id %q, got %q", first.ID, dup.ID)
	}
	if dup.Filename != "receipt.jpg" {
		t.Fatalf("expected the duplicate response to describe the existing document, got filename %q", dup.Filename)
	}

	all, err := store.List(nil, false, "", 0, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected the duplicate upload to not create a second row, got %d documents", len(all))
	}
}

// TestDocumentStore_InsertWritesOperatorTagsInTheSameTransaction guards
// against tags being applied by a separate call after Insert has already
// committed: a caller whose follow-up call then failed would retry into
// Insert's duplicate branch (which ignores tags/title/folder_id) and land
// untagged. doc.OperatorTags is an input to Insert itself, written in the
// same transaction as the row - so the tags are already there in Insert's
// own return value, with no follow-up call needed at all.
func TestDocumentStore_InsertWritesOperatorTagsInTheSameTransaction(t *testing.T) {
	store := newTestDocumentStore(t)

	inserted, err := store.Insert(document{
		SHA256:       "sha-insert-tags",
		Filename:     "manual.pdf",
		MIME:         "application/pdf",
		OperatorTags: []string{"engine", " diesel ", "", "engine"},
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if len(inserted.OperatorTags) != 2 || inserted.OperatorTags[0] != "diesel" || inserted.OperatorTags[1] != "engine" {
		t.Fatalf("expected Insert's own return to carry the trimmed, deduplicated, alphabetical tags, got %+v", inserted.OperatorTags)
	}

	got, err := store.Get(inserted.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.OperatorTags) != 2 || got.OperatorTags[0] != "diesel" || got.OperatorTags[1] != "engine" {
		t.Fatalf("expected the tags to have been committed by Insert itself, got %+v", got.OperatorTags)
	}
}

func TestDocumentStore_DeleteCascadesToChunksAndFTS(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-cascade", "impeller.pdf", nil)

	if err := store.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Heading: "Impeller replacement", Text: "Replace the impeller every 500 hours."},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	before, err := store.Search("impeller", nil, false, "", 10, 0)
	if err != nil {
		t.Fatalf("Search (before delete): %v", err)
	}
	if len(before) == 0 {
		t.Fatalf("expected a hit for 'impeller' before delete")
	}

	sha, err := store.Delete(doc.ID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if sha != "sha-cascade" {
		t.Fatalf("expected Delete to return the sha256 %q, got %q", "sha-cascade", sha)
	}

	after, err := store.Search("impeller", nil, false, "", 10, 0)
	if err != nil {
		t.Fatalf("Search (after delete): %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("expected no FTS matches after a cascaded delete, got %+v", after)
	}

	var chunkCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM document_chunks WHERE document_id = ?`, doc.ID).Scan(&chunkCount); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if chunkCount != 0 {
		t.Fatalf("expected the document's chunks to be gone too, found %d", chunkCount)
	}

	if _, err := store.Get(doc.ID); !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected the document itself to be gone, got %v", err)
	}
}

func TestDocumentStore_DeleteUnknownIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if _, err := store.Delete("does-not-exist"); !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected errDocumentNotFound, got %v", err)
	}
}

func TestDocumentStore_ReplaceChunksIsAtomic(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-atomic", "log.txt", nil)

	if err := store.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "first chunk"},
	}); err != nil {
		t.Fatalf("seed ReplaceChunks: %v", err)
	}

	// The second chunk has a source outside the CHECK constraint's allowed
	// set, so the insert of chunk 2 fails partway through the batch. If the
	// whole call isn't one transaction, chunk 1 below would still land even
	// though the call as a whole reports an error.
	err := store.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "replacement chunk"},
		{Seq: 2, Source: "not-a-real-source", Text: "poison chunk"},
	})
	if err == nil {
		t.Fatalf("expected ReplaceChunks to fail on an invalid chunk source")
	}

	// Filtered to source='local' so the auto-created meta chunk (seq 0,
	// source 'meta', rebuilt on every Insert) doesn't count as a second row
	// here - this test only cares about ReplaceChunks' own rollback.
	var texts []string
	rows, qerr := store.db.Query(`SELECT text FROM document_chunks WHERE document_id = ? AND source = 'local' ORDER BY seq`, doc.ID)
	if qerr != nil {
		t.Fatalf("query chunks: %v", qerr)
	}
	defer rows.Close()
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			t.Fatalf("scan: %v", err)
		}
		texts = append(texts, text)
	}

	if len(texts) != 1 || texts[0] != "first chunk" {
		t.Fatalf("expected ReplaceChunks to roll back entirely on failure, leaving the original chunk untouched; got %+v", texts)
	}
}

func TestDocumentStore_NextPendingReturnsOldestFirst(t *testing.T) {
	store := newTestDocumentStore(t)
	t0 := time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC)

	store.now = sequencedClock(t0)
	older := mustInsertDocument(t, store, "sha-older", "older.pdf", nil)

	store.now = sequencedClock(t0.Add(time.Minute))
	newer := mustInsertDocument(t, store, "sha-newer", "newer.pdf", nil)

	if err := store.SetIndexed(newer.ID, "local"); err != nil {
		t.Fatalf("SetIndexed: %v", err)
	}

	next, ok, err := store.NextPending()
	if err != nil {
		t.Fatalf("NextPending: %v", err)
	}
	if !ok {
		t.Fatalf("expected a pending document")
	}
	if next.ID != older.ID {
		t.Fatalf("expected the oldest pending document %q first, got %q", older.ID, next.ID)
	}

	if err := store.SetIndexed(older.ID, "local"); err != nil {
		t.Fatalf("SetIndexed: %v", err)
	}
	if _, ok, err := store.NextPending(); err != nil || ok {
		t.Fatalf("expected no pending documents left, ok=%v err=%v", ok, err)
	}
}

func TestDocumentStore_SetStageSetIndexedSetFailedTransitions(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-stage", "notes.txt", nil)

	if err := store.SetStage(doc.ID, "enrich"); err != nil {
		t.Fatalf("SetStage: %v", err)
	}
	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Stage != "enrich" {
		t.Fatalf("expected stage 'enrich', got %q", got.Stage)
	}

	if err := store.SetFailed(doc.ID, "mate is not ready"); err != nil {
		t.Fatalf("SetFailed: %v", err)
	}
	got, err = store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "failed" || got.Error != "mate is not ready" {
		t.Fatalf("expected failed status with error message, got %+v", got)
	}

	if err := store.SetIndexed(doc.ID, "mate"); err != nil {
		t.Fatalf("SetIndexed: %v", err)
	}
	got, err = store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "indexed" || got.Stage != "done" || got.IndexedWith != "mate" || got.Error != "" {
		t.Fatalf("expected a clean indexed/done/mate state, got %+v", got)
	}
	if got.IndexedAt == nil {
		t.Fatalf("expected IndexedAt to be set once indexed")
	}
}

func TestDocumentStore_SetStageUnknownIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if err := store.SetStage("does-not-exist", "enrich"); !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected errDocumentNotFound, got %v", err)
	}
}

func TestDocumentStore_UpdateMetaReplacesOperatorTagsAndRebuildsMetaChunk(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-meta", "invoice.pdf", nil)

	title := "Marina Invoice"
	notes := "Paid by card"
	if err := store.UpdateMeta(doc.ID, &title, &notes, []string{"finance", "marina"}); err != nil {
		t.Fatalf("UpdateMeta: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != title || got.Notes != notes {
		t.Fatalf("expected title/notes to update, got %+v", got)
	}
	if len(got.OperatorTags) != 2 {
		t.Fatalf("expected 2 operator tags, got %+v", got.OperatorTags)
	}

	hits, err := store.Search("Marina", nil, false, "", 10, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("expected the rebuilt meta chunk to be searchable by the new title")
	}

	// Replacing with a smaller tag set drops the ones no longer listed.
	if err := store.UpdateMeta(doc.ID, nil, nil, []string{"finance"}); err != nil {
		t.Fatalf("UpdateMeta (retag): %v", err)
	}
	got, err = store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.OperatorTags) != 1 || got.OperatorTags[0] != "finance" {
		t.Fatalf("expected operator tags to be replaced, got %+v", got.OperatorTags)
	}
}

func TestDocumentStore_SetSuggestedFillsEmptyTitleAndNeverOverwritesOperatorTags(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-suggest", "scan.jpg", nil)

	operatorTitle := "Receipt"
	if err := store.UpdateMeta(doc.ID, &operatorTitle, nil, []string{"receipt"}); err != nil {
		t.Fatalf("UpdateMeta: %v", err)
	}

	if err := store.SetSuggested(doc.ID, "Fuel Receipt", "A fuel dock receipt for 200L diesel.", []string{"receipt", "diesel"}); err != nil {
		t.Fatalf("SetSuggested: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != operatorTitle {
		t.Fatalf("expected an existing title to survive SetSuggested, got %q", got.Title)
	}
	if got.Summary != "A fuel dock receipt for 200L diesel." {
		t.Fatalf("expected the summary to be set, got %q", got.Summary)
	}
	if len(got.OperatorTags) != 1 || got.OperatorTags[0] != "receipt" {
		t.Fatalf("expected the operator tag to be untouched, got %+v", got.OperatorTags)
	}
	if len(got.SuggestedTags) != 1 || got.SuggestedTags[0] != "diesel" {
		t.Fatalf("expected only the new suggested tag to be added (receipt was already an operator tag), got %+v", got.SuggestedTags)
	}
}

func TestDocumentStore_SetSuggestedOnBlankTitleUsesSuggestion(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-suggest-title", "scan2.jpg", nil)

	if err := store.SetSuggested(doc.ID, "Fuel Receipt", "summary", nil); err != nil {
		t.Fatalf("SetSuggested: %v", err)
	}
	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "Fuel Receipt" {
		t.Fatalf("expected the suggested title to fill a blank title, got %q", got.Title)
	}
}

func TestDocumentStore_ListFiltersByFolderRecursiveAndTag(t *testing.T) {
	store := newTestDocumentStore(t)

	receipts, err := store.CreateFolder("Receipts", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	fuel, err := store.CreateFolder("Fuel", &receipts.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	mustInsertDocument(t, store, "sha-root", "root.pdf", nil)
	mustInsertDocument(t, store, "sha-receipts", "receipt.pdf", &receipts.ID)
	fuelDoc := mustInsertDocument(t, store, "sha-fuel", "fuel.pdf", &fuel.ID)
	tags := []string{"diesel"}
	if err := store.UpdateMeta(fuelDoc.ID, nil, nil, tags); err != nil {
		t.Fatalf("UpdateMeta: %v", err)
	}

	all, err := store.List(nil, false, "", 0, 0)
	if err != nil {
		t.Fatalf("List(all): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 documents with no filter, got %d", len(all))
	}

	nonRecursive, err := store.List(&receipts.ID, false, "", 0, 0)
	if err != nil {
		t.Fatalf("List(receipts, non-recursive): %v", err)
	}
	if len(nonRecursive) != 1 {
		t.Fatalf("expected 1 document directly in Receipts, got %d", len(nonRecursive))
	}

	recursive, err := store.List(&receipts.ID, true, "", 0, 0)
	if err != nil {
		t.Fatalf("List(receipts, recursive): %v", err)
	}
	if len(recursive) != 2 {
		t.Fatalf("expected 2 documents under Receipts recursively, got %d", len(recursive))
	}

	tagged, err := store.List(nil, false, "diesel", 0, 0)
	if err != nil {
		t.Fatalf("List(tag=diesel): %v", err)
	}
	if len(tagged) != 1 || tagged[0].ID != fuelDoc.ID {
		t.Fatalf("expected the tag filter to isolate the fuel document, got %+v", tagged)
	}

	if _, err := store.List(strPtr("does-not-exist"), false, "", 0, 0); !errors.Is(err, errFolderNotFound) {
		t.Fatalf("expected errFolderNotFound for an unknown folder id, got %v", err)
	}
}

// TestDocumentStore_ListRootSentinelFiltersDocumentsWithNoFolder covers the
// B3 API's "folder=root" query value (documents_handlers.go): List's
// folderID contract otherwise has no way to say "only documents with no
// folder at all" - nil already means "no filter, everywhere" (see
// queryDocuments' doc comment). The empty string is used as that sentinel
// instead, since "" can never be a real folder id (CreateFolder always
// assigns a uuid).
func TestDocumentStore_ListRootSentinelFiltersDocumentsWithNoFolder(t *testing.T) {
	store := newTestDocumentStore(t)

	receipts, err := store.CreateFolder("Receipts", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	mustInsertDocument(t, store, "sha-root-only", "root.pdf", nil)
	mustInsertDocument(t, store, "sha-in-receipts", "receipt.pdf", &receipts.ID)

	root := strPtr("")
	nonRecursive, err := store.List(root, false, "", 0, 0)
	if err != nil {
		t.Fatalf("List(root, non-recursive): %v", err)
	}
	if len(nonRecursive) != 1 || nonRecursive[0].Filename != "root.pdf" {
		t.Fatalf("expected only the root-level document, got %+v", nonRecursive)
	}

	recursive, err := store.List(root, true, "", 0, 0)
	if err != nil {
		t.Fatalf("List(root, recursive): %v", err)
	}
	if len(recursive) != 2 {
		t.Fatalf("expected a recursive root listing to include every document (root's subtree is the whole tree), got %d", len(recursive))
	}
}

// TestDocumentStore_ListPagesStablyWhenCreatedAtTies covers paging when
// several documents share the same created_at (whole seconds, so more than
// one insert per second is routine): queryDocuments orders by created_at
// DESC, id DESC, so ties break on id rather than on whatever order
// SQLite's table scan happens to produce - which is not a documented
// guarantee and would otherwise let a page boundary repeat a document or
// skip one entirely. store.now is pinned to a single instant for every
// insert below (the store's injectable clock) to force the tie. Beyond the
// "every id appears exactly once" contract, this also pins the
// concatenated page order to strict id-DESC among the tied rows, the
// actual tiebreaker queryDocuments applies.
func TestDocumentStore_ListPagesStablyWhenCreatedAtTies(t *testing.T) {
	store := newTestDocumentStore(t)
	t0 := time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return t0 }

	const total = 5
	want := map[string]bool{}
	var insertedIDs []string
	for i := 0; i < total; i++ {
		doc := mustInsertDocument(t, store, fmt.Sprintf("sha-tie-%d", i), fmt.Sprintf("doc-%d.pdf", i), nil)
		want[doc.ID] = true
		insertedIDs = append(insertedIDs, doc.ID)
	}

	got := map[string]bool{}
	var pageOrder []string
	for offset := 0; offset < total; offset++ {
		page, err := store.List(nil, false, "", 1, offset)
		if err != nil {
			t.Fatalf("List(offset=%d): %v", offset, err)
		}
		if len(page) != 1 {
			t.Fatalf("expected exactly 1 result at offset %d, got %d", offset, len(page))
		}
		if got[page[0].ID] {
			t.Fatalf("document %s appeared on more than one page", page[0].ID)
		}
		got[page[0].ID] = true
		pageOrder = append(pageOrder, page[0].ID)
	}

	wantOrder := append([]string{}, insertedIDs...)
	sort.Sort(sort.Reverse(sort.StringSlice(wantOrder)))
	if !reflect.DeepEqual(pageOrder, wantOrder) {
		t.Fatalf("expected ties to break by id DESC (%v), got %v", wantOrder, pageOrder)
	}
	if len(got) != len(want) {
		t.Fatalf("expected every document to appear exactly once across pages, got %d of %d", len(got), len(want))
	}
	for id := range want {
		if !got[id] {
			t.Fatalf("document %s never appeared on any page", id)
		}
	}
}

func TestDocumentStore_MoveDocumentsUpdatesFolderAndMetaChunk(t *testing.T) {
	store := newTestDocumentStore(t)
	folder, err := store.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	doc := mustInsertDocument(t, store, "sha-move", "engine.pdf", nil)

	if err := store.MoveDocuments([]string{doc.ID}, &folder.ID); err != nil {
		t.Fatalf("MoveDocuments: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.FolderID == nil || *got.FolderID != folder.ID {
		t.Fatalf("expected the document to move into %q, got %+v", folder.ID, got.FolderID)
	}

	hits, err := store.Search("Manuals", nil, false, "", 10, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("expected the moved document's meta chunk to mention its new folder path")
	}

	if err := store.MoveDocuments([]string{"does-not-exist"}, nil); !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected errDocumentNotFound for an unknown document id, got %v", err)
	}
}

// TestDocumentStore_PatchDocumentBadFolderIDLeavesTitleUnchanged guards a
// PATCH's atomicity: {"title":"New","folder_id":"bogus"} must never leave
// the title changed while the request as a whole reports 404. PatchDocument
// applies title/notes/tags and the folder move in one transaction, so a bad
// folder_id rolls the title change back too.
func TestDocumentStore_PatchDocumentBadFolderIDLeavesTitleUnchanged(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-patch-atomic", "a.pdf", nil)
	if err := store.UpdateMeta(doc.ID, strPtr("Original Title"), nil, nil); err != nil {
		t.Fatalf("UpdateMeta (seed): %v", err)
	}

	newTitle := "New Title"
	badFolder := "does-not-exist"
	_, err := store.PatchDocument(doc.ID, &newTitle, nil, nil, &badFolder, true)
	if !errors.Is(err, errFolderNotFound) {
		t.Fatalf("expected errFolderNotFound, got %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "Original Title" {
		t.Fatalf("expected the title to remain unchanged when folder_id is invalid, got %q", got.Title)
	}
}

// TestDocumentStore_PatchDocumentUnknownIDTakesPrecedenceOverBadFolderID
// pins PatchDocument's ordering: document-not-found must be reported even
// when folder_id is ALSO invalid, not the folder error.
func TestDocumentStore_PatchDocumentUnknownIDTakesPrecedenceOverBadFolderID(t *testing.T) {
	store := newTestDocumentStore(t)
	newTitle := "New Title"
	badFolder := "does-not-exist"
	_, err := store.PatchDocument("does-not-exist", &newTitle, nil, nil, &badFolder, true)
	if !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected errDocumentNotFound to take precedence over errFolderNotFound, got %v", err)
	}
}

// ── folders ──────────────────────────────────────────────────────────────

func TestDocumentStore_CreateFolderRejectsDuplicateNameCaseInsensitive(t *testing.T) {
	store := newTestDocumentStore(t)

	if _, err := store.CreateFolder("Receipts", nil); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := store.CreateFolder("receipts", nil); !errors.Is(err, errFolderNameTaken) {
		t.Fatalf("expected errFolderNameTaken at the root, got %v", err)
	}

	parent, err := store.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := store.CreateFolder("Engine", &parent.ID); err != nil {
		t.Fatalf("CreateFolder (nested): %v", err)
	}
	if _, err := store.CreateFolder("ENGINE", &parent.ID); !errors.Is(err, errFolderNameTaken) {
		t.Fatalf("expected errFolderNameTaken under a parent, got %v", err)
	}

	// Same name is fine in a different parent.
	if _, err := store.CreateFolder("Engine", nil); err != nil {
		t.Fatalf("expected the same name to be allowed under a different parent, got %v", err)
	}
}

func TestDocumentStore_CreateFolderRejectsInvalidNames(t *testing.T) {
	store := newTestDocumentStore(t)
	for _, name := range []string{"", "   ", "a/b", strings.Repeat("x", 121)} {
		if _, err := store.CreateFolder(name, nil); !errors.Is(err, errFolderNameInvalid) {
			t.Fatalf("CreateFolder(%q): expected errFolderNameInvalid, got %v", name, err)
		}
	}
}

func TestDocumentStore_MoveFolderIntoDescendantRejected(t *testing.T) {
	store := newTestDocumentStore(t)

	parent, err := store.CreateFolder("Parent", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	child, err := store.CreateFolder("Child", &parent.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	grandchild, err := store.CreateFolder("Grandchild", &child.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	if err := store.MoveFolder(parent.ID, &grandchild.ID); !errors.Is(err, errFolderCycle) {
		t.Fatalf("expected errFolderCycle moving a folder under its grandchild, got %v", err)
	}
	if err := store.MoveFolder(parent.ID, &parent.ID); !errors.Is(err, errFolderCycle) {
		t.Fatalf("expected errFolderCycle moving a folder into itself, got %v", err)
	}

	// A legitimate move (grandchild up to root) must still work.
	if err := store.MoveFolder(grandchild.ID, nil); err != nil {
		t.Fatalf("expected a non-cyclic move to succeed, got %v", err)
	}
}

func TestDocumentStore_DeleteFolderNotEmptyRejected(t *testing.T) {
	store := newTestDocumentStore(t)

	withSubfolder, err := store.CreateFolder("HasSubfolder", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := store.CreateFolder("Child", &withSubfolder.ID); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if err := store.DeleteFolder(withSubfolder.ID); !errors.Is(err, errFolderNotEmpty) {
		t.Fatalf("expected errFolderNotEmpty for a folder with a subfolder, got %v", err)
	}

	withDoc, err := store.CreateFolder("HasDocument", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	mustInsertDocument(t, store, "sha-in-folder", "file.pdf", &withDoc.ID)
	if err := store.DeleteFolder(withDoc.ID); !errors.Is(err, errFolderNotEmpty) {
		t.Fatalf("expected errFolderNotEmpty for a folder with a document, got %v", err)
	}

	empty, err := store.CreateFolder("Empty", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if err := store.DeleteFolder(empty.ID); err != nil {
		t.Fatalf("expected deleting an empty folder to succeed, got %v", err)
	}
}

func TestDocumentStore_DeleteFolderUnknownIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if err := store.DeleteFolder("does-not-exist"); !errors.Is(err, errFolderNotFound) {
		t.Fatalf("expected errFolderNotFound, got %v", err)
	}
}

func TestDocumentStore_FolderPathReturnsRootToLeaf(t *testing.T) {
	store := newTestDocumentStore(t)

	manuals, err := store.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	engine, err := store.CreateFolder("Engine", &manuals.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	path, err := store.FolderPath(engine.ID)
	if err != nil {
		t.Fatalf("FolderPath: %v", err)
	}
	if len(path) != 2 {
		t.Fatalf("expected a 2-element path, got %+v", path)
	}
	if path[0].Name != "Manuals" || path[1].Name != "Engine" {
		t.Fatalf("expected [Manuals, Engine] root to leaf, got %+v", path)
	}

	if _, err := store.FolderPath("does-not-exist"); !errors.Is(err, errFolderNotFound) {
		t.Fatalf("expected errFolderNotFound, got %v", err)
	}
}

func TestDocumentStore_ListFolderReturnsSubfoldersThenDocuments(t *testing.T) {
	store := newTestDocumentStore(t)

	manuals, err := store.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := store.CreateFolder("Engine", &manuals.ID); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	mustInsertDocument(t, store, "sha-listfolder", "overview.pdf", &manuals.ID)
	mustInsertDocument(t, store, "sha-root-doc", "root.pdf", nil)

	subfolders, docs, err := store.ListFolder(&manuals.ID)
	if err != nil {
		t.Fatalf("ListFolder: %v", err)
	}
	if len(subfolders) != 1 || subfolders[0].Name != "Engine" {
		t.Fatalf("expected the Engine subfolder, got %+v", subfolders)
	}
	if len(docs) != 1 || docs[0].Filename != "overview.pdf" {
		t.Fatalf("expected overview.pdf directly under Manuals, got %+v", docs)
	}

	rootSubfolders, rootDocs, err := store.ListFolder(nil)
	if err != nil {
		t.Fatalf("ListFolder(root): %v", err)
	}
	if len(rootSubfolders) != 1 || rootSubfolders[0].Name != "Manuals" {
		t.Fatalf("expected Manuals at the root, got %+v", rootSubfolders)
	}
	if len(rootDocs) != 1 || rootDocs[0].Filename != "root.pdf" {
		t.Fatalf("expected root.pdf at the root, got %+v", rootDocs)
	}
}

func TestDocumentStore_SearchWithinFolderIncludesDescendants(t *testing.T) {
	store := newTestDocumentStore(t)

	manuals, err := store.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	engine, err := store.CreateFolder("Engine", &manuals.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	receipts, err := store.CreateFolder("Receipts", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	inEngine := mustInsertDocument(t, store, "sha-engine-doc", "impeller.pdf", &engine.ID)
	if err := store.ReplaceChunks(inEngine.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "impeller torque spec"},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	inReceipts := mustInsertDocument(t, store, "sha-receipts-doc", "unrelated.pdf", &receipts.ID)
	if err := store.ReplaceChunks(inReceipts.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "impeller mentioned here too"},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	nonRecursive, err := store.Search("impeller", &manuals.ID, false, "", 10, 0)
	if err != nil {
		t.Fatalf("Search(manuals, non-recursive): %v", err)
	}
	if len(nonRecursive) != 0 {
		t.Fatalf("expected no direct hits in Manuals itself (the doc lives in the Engine subfolder), got %+v", nonRecursive)
	}

	recursive, err := store.Search("impeller", &manuals.ID, true, "", 10, 0)
	if err != nil {
		t.Fatalf("Search(manuals, recursive): %v", err)
	}
	if len(recursive) != 1 || recursive[0].DocumentID != inEngine.ID {
		t.Fatalf("expected exactly the Engine-folder document via recursive search, got %+v", recursive)
	}
}

// TestDocumentStore_SearchRootSentinelFiltersDocumentsWithNoFolder is
// Search's half of the "folder=root" sentinel (see
// TestDocumentStore_ListRootSentinelFiltersDocumentsWithNoFolder above).
func TestDocumentStore_SearchRootSentinelFiltersDocumentsWithNoFolder(t *testing.T) {
	store := newTestDocumentStore(t)

	receipts, err := store.CreateFolder("Receipts", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	atRoot := mustInsertDocument(t, store, "sha-root-search", "root.pdf", nil)
	if err := store.ReplaceChunks(atRoot.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "impeller torque spec"},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	inReceipts := mustInsertDocument(t, store, "sha-receipts-search", "receipt.pdf", &receipts.ID)
	if err := store.ReplaceChunks(inReceipts.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "impeller mentioned in a receipt"},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	results, err := store.Search("impeller", strPtr(""), false, "", 10, 0)
	if err != nil {
		t.Fatalf("Search(root, non-recursive): %v", err)
	}
	if len(results) != 1 || results[0].DocumentID != atRoot.ID {
		t.Fatalf("expected only the root-level document, got %+v", results)
	}
}

// TestDocumentStore_SearchOffsetPagesWithoutOverlap guards Search's offset
// parameter: three documents, one hit each, paged one at a time by offset
// must partition exactly, in bm25 score order, with no document repeated
// or skipped and nothing returned once offset runs past the hit count.
func TestDocumentStore_SearchOffsetPagesWithoutOverlap(t *testing.T) {
	store := newTestDocumentStore(t)

	const total = 3
	want := map[string]bool{}
	for i := 0; i < total; i++ {
		doc := mustInsertDocument(t, store, fmt.Sprintf("sha-search-page-%d", i), fmt.Sprintf("doc-%d.pdf", i), nil)
		if err := store.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
			{Seq: 1, Source: "local", Text: fmt.Sprintf("impeller document number %d", i)},
		}); err != nil {
			t.Fatalf("ReplaceChunks(%d): %v", i, err)
		}
		want[doc.ID] = true
	}

	got := map[string]bool{}
	for offset := 0; offset < total; offset++ {
		page, err := store.Search("impeller", nil, false, "", 1, offset)
		if err != nil {
			t.Fatalf("Search(offset=%d): %v", offset, err)
		}
		if len(page) != 1 {
			t.Fatalf("expected exactly 1 result at offset %d, got %d: %+v", offset, len(page), page)
		}
		if got[page[0].DocumentID] {
			t.Fatalf("document %s appeared on more than one page", page[0].DocumentID)
		}
		got[page[0].DocumentID] = true
	}
	if len(got) != len(want) {
		t.Fatalf("expected every document to appear exactly once across pages, got %d of %d", len(got), len(want))
	}
	for id := range want {
		if !got[id] {
			t.Fatalf("document %s never appeared on any page", id)
		}
	}

	// Past the end: no results, no error.
	empty, err := store.Search("impeller", nil, false, "", 1, total)
	if err != nil {
		t.Fatalf("Search(offset=total): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected no results once offset exceeds the hit count, got %+v", empty)
	}
}

// TestDocumentStore_SearchReachesHitsPastFormerTwoHundredChunkCap guards
// against Search stopping short: it must scan every matching raw chunk hit
// in score order before collapsing to one-per-document, so a document is
// reachable regardless of how many other documents' chunks outrank it, no
// matter how high a limit/offset is requested. Every filler document here
// repeats the query term far more than the target does, so all of them
// outrank it - putting the target's hit well past position 200 in raw
// score order.
func TestDocumentStore_SearchReachesHitsPastFormerTwoHundredChunkCap(t *testing.T) {
	store := newTestDocumentStore(t)

	const fillerCount = 205
	for i := 0; i < fillerCount; i++ {
		doc := mustInsertDocument(t, store, fmt.Sprintf("sha-search-filler-%d", i), fmt.Sprintf("filler-%d.pdf", i), nil)
		if err := store.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
			{Seq: 1, Source: "local", Text: strings.Repeat("impeller ", 30)},
		}); err != nil {
			t.Fatalf("ReplaceChunks(filler %d): %v", i, err)
		}
	}

	target := mustInsertDocument(t, store, "sha-search-target", "target.pdf", nil)
	if err := store.ReplaceChunks(target.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "impeller"},
	}); err != nil {
		t.Fatalf("ReplaceChunks(target): %v", err)
	}

	results, err := store.Search("impeller", nil, false, "", fillerCount+1, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	found := false
	for _, r := range results {
		if r.DocumentID == target.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected the target document to be reachable even with %d higher-ranked documents ahead of it, got %d results", fillerCount, len(results))
	}
}

func TestDocumentStore_RenameFolderUpdatesMetaChunkSearch(t *testing.T) {
	store := newTestDocumentStore(t)

	folder, err := store.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	mustInsertDocument(t, store, "sha-rename", "engine.pdf", &folder.ID)

	before, err := store.Search("Powerplant", nil, false, "", 10, 0)
	if err != nil {
		t.Fatalf("Search (before rename): %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("expected no hits for the new name before the rename, got %+v", before)
	}

	if err := store.RenameFolder(folder.ID, "Powerplant"); err != nil {
		t.Fatalf("RenameFolder: %v", err)
	}

	after, err := store.Search("Powerplant", nil, false, "", 10, 0)
	if err != nil {
		t.Fatalf("Search (after rename): %v", err)
	}
	if len(after) == 0 {
		t.Fatalf("expected the document's meta chunk to be reindexed under the new folder name")
	}
}

func TestDocumentStore_RenameFolderRejectsDuplicateName(t *testing.T) {
	store := newTestDocumentStore(t)
	if _, err := store.CreateFolder("Receipts", nil); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	manuals, err := store.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if err := store.RenameFolder(manuals.ID, "receipts"); !errors.Is(err, errFolderNameTaken) {
		t.Fatalf("expected errFolderNameTaken, got %v", err)
	}
}

// ── sweep ────────────────────────────────────────────────────────────────

func TestSweepDocumentsDir_RemovesAbandonedTempUploads(t *testing.T) {
	store := newTestDocumentStore(t)
	dir := t.TempDir()

	tmpPath := filepath.Join(dir, "upload-abc123.tmp")
	if err := os.WriteFile(tmpPath, []byte("partial"), 0o644); err != nil {
		t.Fatalf("write temp upload: %v", err)
	}

	result, err := sweepDocumentsDir(dir, store)
	if err != nil {
		t.Fatalf("sweepDocumentsDir: %v", err)
	}
	if result.RemovedTemp != 1 {
		t.Fatalf("expected 1 removed temp file, got %d", result.RemovedTemp)
	}
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("expected the temp upload to be removed, stat err=%v", err)
	}
}

func TestSweepDocumentsDir_WarnsWithoutDeletingOrphansAndMissingFiles(t *testing.T) {
	store := newTestDocumentStore(t)
	dir := t.TempDir()

	// A row with no file on disk.
	doc := mustInsertDocument(t, store, "sha-missing-file", "gone.pdf", nil)

	// A file on disk with no row.
	orphanPath := filepath.Join(dir, "orphan-sha-not-in-db")
	if err := os.WriteFile(orphanPath, []byte("bytes"), 0o644); err != nil {
		t.Fatalf("write orphan file: %v", err)
	}

	result, err := sweepDocumentsDir(dir, store)
	if err != nil {
		t.Fatalf("sweepDocumentsDir: %v", err)
	}
	if result.OrphanFiles != 1 {
		t.Fatalf("expected 1 orphan file, got %d", result.OrphanFiles)
	}
	if result.MissingFiles != 1 {
		t.Fatalf("expected 1 missing file, got %d", result.MissingFiles)
	}

	// Neither side is deleted - only warned about.
	if _, err := os.Stat(orphanPath); err != nil {
		t.Fatalf("expected the orphan file to remain on disk, got %v", err)
	}
	if _, err := store.Get(doc.ID); err != nil {
		t.Fatalf("expected the document row to remain, got %v", err)
	}
}

// ── B3 additions: chunk pagination, reindex, tag counts ─────────────────

func TestDocumentStore_ChunksFromReturnsBodyChunksFromSeqOnward(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-chunks-from", "manual.pdf", nil)

	if err := store.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "first"},
		{Seq: 2, Source: "local", Text: "second"},
		{Seq: 3, Source: "local", Text: "third"},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	all, err := store.ChunksFrom(doc.ID, 1)
	if err != nil {
		t.Fatalf("ChunksFrom(1): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected all 3 body chunks from seq 1, got %d: %+v", len(all), all)
	}
	// The meta chunk (seq 0, inserted by Insert itself) is never included
	// unless the caller explicitly asks for it.
	if all[0].Seq != 1 || all[0].Text != "first" {
		t.Fatalf("expected the first result to be seq 1 ('first'), got %+v", all[0])
	}

	fromTwo, err := store.ChunksFrom(doc.ID, 2)
	if err != nil {
		t.Fatalf("ChunksFrom(2): %v", err)
	}
	if len(fromTwo) != 2 || fromTwo[0].Text != "second" || fromTwo[1].Text != "third" {
		t.Fatalf("expected chunks 2 and 3 only, got %+v", fromTwo)
	}

	withMeta, err := store.ChunksFrom(doc.ID, 0)
	if err != nil {
		t.Fatalf("ChunksFrom(0): %v", err)
	}
	if len(withMeta) != 4 || withMeta[0].Source != "meta" {
		t.Fatalf("expected the meta chunk plus 3 body chunks when explicitly starting at seq 0, got %+v", withMeta)
	}
}

func TestDocumentStore_ChunksFromUnknownIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if _, err := store.ChunksFrom("does-not-exist", 1); !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected errDocumentNotFound, got %v", err)
	}
}

func TestDocumentStore_MarkReindexResetsToPendingExtractAndSetsForceOCR(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-reindex", "manual.pdf", nil)
	if err := store.SetIndexed(doc.ID, "local"); err != nil {
		t.Fatalf("SetIndexed: %v", err)
	}
	if err := store.SetFailed(doc.ID, "boom"); err != nil {
		t.Fatalf("SetFailed: %v", err)
	}

	if err := store.MarkReindex(doc.ID, true); err != nil {
		t.Fatalf("MarkReindex: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "pending" || got.Stage != "extract" {
		t.Fatalf("expected pending/extract, got status=%q stage=%q", got.Status, got.Stage)
	}
	if got.Error != "" {
		t.Fatalf("expected error to be cleared, got %q", got.Error)
	}
	if !got.ForceOCR {
		t.Fatalf("expected force_ocr to be set")
	}
	if !got.Enrich {
		t.Fatalf("expected enrich to be set to the value passed in")
	}
}

func TestDocumentStore_MarkReindexUnknownIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if err := store.MarkReindex("does-not-exist", false); !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected errDocumentNotFound, got %v", err)
	}
}

func TestDocumentStore_TagCountsCountsDistinctDocumentsPerTag(t *testing.T) {
	store := newTestDocumentStore(t)

	a := mustInsertDocument(t, store, "sha-tagcount-a", "a.pdf", nil)
	b := mustInsertDocument(t, store, "sha-tagcount-b", "b.pdf", nil)
	if err := store.UpdateMeta(a.ID, nil, nil, []string{"engine", "diesel"}); err != nil {
		t.Fatalf("UpdateMeta(a): %v", err)
	}
	if err := store.UpdateMeta(b.ID, nil, nil, []string{"engine"}); err != nil {
		t.Fatalf("UpdateMeta(b): %v", err)
	}

	counts, err := store.TagCounts()
	if err != nil {
		t.Fatalf("TagCounts: %v", err)
	}

	got := map[string]int{}
	for _, c := range counts {
		got[c.Tag] = c.Count
	}
	if got["engine"] != 2 {
		t.Fatalf("expected tag 'engine' on 2 documents, got %d (%+v)", got["engine"], counts)
	}
	if got["diesel"] != 1 {
		t.Fatalf("expected tag 'diesel' on 1 document, got %d (%+v)", got["diesel"], counts)
	}
}

func strPtr(s string) *string { return &s }
