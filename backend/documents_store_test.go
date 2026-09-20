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
	"unicode/utf8"
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

// TestDocumentStore_SetExtractedStoresMarkdownAndPageCount pins B4's
// extract-stage store write: the indexer needs the whole flattened markdown
// (not just the chunked form ReplaceChunks stores) for the enrich stage's
// text-layer-PDF branch, which sends the first 24k characters of it.
func TestDocumentStore_SetExtractedStoresMarkdownAndPageCount(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-extracted", "manual.pdf", nil)

	if err := store.SetExtracted(doc.ID, "# Engine Manual\n\nChange the impeller yearly.", 3); err != nil {
		t.Fatalf("SetExtracted: %v", err)
	}
	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Markdown != "# Engine Manual\n\nChange the impeller yearly." {
		t.Fatalf("unexpected markdown: %q", got.Markdown)
	}
	if got.PageCount != 3 {
		t.Fatalf("expected page_count 3, got %d", got.PageCount)
	}
}

func TestDocumentStore_SetExtractedUnknownIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if err := store.SetExtracted("does-not-exist", "text", 1); !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected errDocumentNotFound, got %v", err)
	}
}

// TestDocumentStore_SetWarningSurvivesSetIndexed pins the mechanism B4's
// indexer relies on to keep a partial-page-extraction warning visible on a
// document that still reaches indexed: SetIndexed itself clears
// documents.error unconditionally (TestDocumentStore_SetStageSetIndexedSetFailedTransitions
// pins that as a clean-finish guarantee), so the indexer calls SetWarning
// again, after SetIndexed, to restore the message. This test proves that
// ordering actually leaves the warning in place rather than being wiped.
func TestDocumentStore_SetWarningSurvivesSetIndexed(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-warning", "manual.pdf", nil)

	if err := store.SetIndexed(doc.ID, "local"); err != nil {
		t.Fatalf("SetIndexed: %v", err)
	}
	if err := store.SetWarning(doc.ID, "1 of 2 pages unreadable: pdf p2: panic: bad TL"); err != nil {
		t.Fatalf("SetWarning: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "indexed" {
		t.Fatalf("expected SetWarning to leave status alone, got %q", got.Status)
	}
	if got.Error != "1 of 2 pages unreadable: pdf p2: panic: bad TL" {
		t.Fatalf("expected the warning to survive after SetIndexed, got %q", got.Error)
	}
}

func TestDocumentStore_SetWarningUnknownIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if err := store.SetWarning("does-not-exist", "x"); !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected errDocumentNotFound, got %v", err)
	}
}

// TestDocumentStore_AddIndexCostAccumulatesAcrossCalls pins that a document
// reindexed more than once keeps a running total rather than each pass
// hiding what the previous one already spent - the whole point of tracking
// real spend rather than an estimate.
func TestDocumentStore_AddIndexCostAccumulatesAcrossCalls(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-cost", "receipt.png", nil)

	if err := store.AddIndexCost(doc.ID, "google/gemini-2.5-flash", 0.0011695); err != nil {
		t.Fatalf("AddIndexCost: %v", err)
	}
	if err := store.AddIndexCost(doc.ID, "google/gemini-2.5-flash", 0.0005); err != nil {
		t.Fatalf("AddIndexCost: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.IndexModel != "google/gemini-2.5-flash" {
		t.Fatalf("unexpected index_model: %q", got.IndexModel)
	}
	const want = 0.0011695 + 0.0005
	if diff := got.IndexCostUSD - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("expected index_cost_usd to accumulate to %v, got %v", want, got.IndexCostUSD)
	}
}

func TestDocumentStore_AddIndexCostUnknownIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if err := store.AddIndexCost("does-not-exist", "m", 0.01); !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected errDocumentNotFound, got %v", err)
	}
}

// TestDocumentStore_AddEmbedCostAccumulatesWithoutTouchingIndexModel pins
// the one thing that makes AddEmbedCost a distinct method rather than a
// second call to AddIndexCost: the embedding pass (E1c) and the enrich
// stage's OCR/summarise call are two different OpenRouter models doing two
// different jobs on the same document, so recording the embedding pass's
// spend must never clobber whichever model name the enrich stage already
// wrote to index_model - while both still add into the same running
// index_cost_usd total.
func TestDocumentStore_AddEmbedCostAccumulatesWithoutTouchingIndexModel(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-embed-cost", "manual.pdf", nil)

	if err := store.AddIndexCost(doc.ID, "google/gemini-2.5-flash", 0.002); err != nil {
		t.Fatalf("AddIndexCost: %v", err)
	}
	if err := store.AddEmbedCost(doc.ID, 0.0000004); err != nil {
		t.Fatalf("AddEmbedCost: %v", err)
	}
	if err := store.AddEmbedCost(doc.ID, 0.0000006); err != nil {
		t.Fatalf("AddEmbedCost (2nd): %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.IndexModel != "google/gemini-2.5-flash" {
		t.Fatalf("expected AddEmbedCost to leave index_model untouched, got %q", got.IndexModel)
	}
	const want = 0.002 + 0.0000004 + 0.0000006
	if diff := got.IndexCostUSD - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("expected index_cost_usd to accumulate to %v, got %v", want, got.IndexCostUSD)
	}
}

func TestDocumentStore_AddEmbedCostUnknownIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if err := store.AddEmbedCost("does-not-exist", 0.01); !errors.Is(err, errDocumentNotFound) {
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

// TestDocumentStore_CountReturnsTotalDocumentCount pins the review finding
// behind collectAssistantPromptContext's DocumentCount line (assistant_prompt.go):
// it used to call List(nil, false, "", 0, 0) and take len() of the result,
// loading every document row (markdown column included) and running one tag
// query per row on a single-connection SQLite store just to learn a count.
// Count is the single SELECT count(*) that replaces it.
func TestDocumentStore_CountReturnsTotalDocumentCount(t *testing.T) {
	store := newTestDocumentStore(t)

	n, err := store.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 documents in a fresh store, got %d", n)
	}

	receipts, err := store.CreateFolder("Receipts", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	mustInsertDocument(t, store, "sha-count-1", "a.pdf", nil)
	mustInsertDocument(t, store, "sha-count-2", "b.pdf", &receipts.ID)

	n, err = store.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 documents, got %d", n)
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
	_, err := store.PatchDocument(doc.ID, &newTitle, nil, nil, &badFolder, true, nil)
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
	_, err := store.PatchDocument("does-not-exist", &newTitle, nil, nil, &badFolder, true, nil)
	if !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected errDocumentNotFound to take precedence over errFolderNotFound, got %v", err)
	}
}

// TestDocumentStore_PatchDocumentSortIndexAppliesAtomicallyWithFolderMove
// pins the notes promotion path (plan §4: "file this note into a manual at
// position 3" is PATCH /api/notes/:id {folder_id, sort_index}, which lands
// through this exact method): a bad folder_id must reject the sort_index
// change too, in the same transaction, not apply one and reject the other.
func TestDocumentStore_PatchDocumentSortIndexAppliesAtomicallyWithFolderMove(t *testing.T) {
	store := newTestDocumentStore(t)
	folder, err := store.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	doc := mustInsertDocument(t, store, "sha-patch-sortindex", "a.pdf", nil)

	sortIndex := 3
	updated, err := store.PatchDocument(doc.ID, nil, nil, nil, &folder.ID, true, &sortIndex)
	if err != nil {
		t.Fatalf("PatchDocument: %v", err)
	}
	if updated.SortIndex != 3 {
		t.Fatalf("expected sort_index 3, got %d", updated.SortIndex)
	}
	if updated.FolderID == nil || *updated.FolderID != folder.ID {
		t.Fatalf("expected the folder move to land in the same call, got %+v", updated.FolderID)
	}

	badFolder := "does-not-exist"
	otherSortIndex := 9
	_, err = store.PatchDocument(doc.ID, nil, nil, nil, &badFolder, true, &otherSortIndex)
	if !errors.Is(err, errFolderNotFound) {
		t.Fatalf("expected errFolderNotFound, got %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SortIndex != 3 {
		t.Fatalf("expected sort_index to remain 3 when the folder move is rejected, got %d", got.SortIndex)
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

func TestDocumentStore_ResolveFolderPathWalksSegmentsCaseInsensitively(t *testing.T) {
	store := newTestDocumentStore(t)

	receipts, err := store.CreateFolder("Receipts", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	year, err := store.CreateFolder("2026", &receipts.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	got, err := store.ResolveFolderPath("receipts/2026")
	if err != nil {
		t.Fatalf("ResolveFolderPath: %v", err)
	}
	if got != year.ID {
		t.Fatalf("expected %q, got %q", year.ID, got)
	}

	// Exact case, and a single segment, both still resolve.
	if got, err := store.ResolveFolderPath("Receipts/2026"); err != nil || got != year.ID {
		t.Fatalf("ResolveFolderPath(exact case) = %q, %v", got, err)
	}
	if got, err := store.ResolveFolderPath("Receipts"); err != nil || got != receipts.ID {
		t.Fatalf("ResolveFolderPath(single segment) = %q, %v", got, err)
	}
}

func TestDocumentStore_ResolveFolderPathUnknownSegmentReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if _, err := store.CreateFolder("Receipts", nil); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	if _, err := store.ResolveFolderPath("Receipts/2026"); !errors.Is(err, errFolderNotFound) {
		t.Fatalf("expected errFolderNotFound for a missing child segment, got %v", err)
	}
	if _, err := store.ResolveFolderPath("Manuals"); !errors.Is(err, errFolderNotFound) {
		t.Fatalf("expected errFolderNotFound for an unknown top-level folder, got %v", err)
	}
}

func TestDocumentStore_TopLevelFolderNamesAlphabeticalRootOnly(t *testing.T) {
	store := newTestDocumentStore(t)

	receipts, err := store.CreateFolder("Receipts", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := store.CreateFolder("Manuals", nil); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	// A nested folder must not appear in the top-level listing.
	if _, err := store.CreateFolder("2026", &receipts.ID); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	names, err := store.TopLevelFolderNames()
	if err != nil {
		t.Fatalf("TopLevelFolderNames: %v", err)
	}
	want := []string{"Manuals", "Receipts"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("expected %v, got %v", want, names)
	}
}

// TestDocumentStore_TopLevelFolderNamesIgnoresRootDocuments pins the review
// finding behind TopLevelFolderNames (documents_store.go): it used to be
// ListFolder(nil), which - alongside the folder names it actually wanted -
// also loads every root-filed document's whole row (markdown column
// included) plus a tag query per row, on every Mate question
// (collectAssistantPromptContext, assistant_prompt.go), which is exactly
// the cost Count() was added to avoid (TestDocumentStore_
// CountReturnsTotalDocumentCount above). The fix is a single query over
// document_folders; this test pins the same output - folder names only,
// alphabetical, root documents ignored - now that root documents (with
// markdown and tags, the two costs the old query paid for nothing) are
// present to prove they no longer change the answer.
func TestDocumentStore_TopLevelFolderNamesIgnoresRootDocuments(t *testing.T) {
	store := newTestDocumentStore(t)

	if _, err := store.CreateFolder("Receipts", nil); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := store.CreateFolder("Manuals", nil); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	doc := mustInsertDocument(t, store, "sha-root-doc", "engine-manual.pdf", nil)
	if err := store.SetExtracted(doc.ID, "# Engine Manual\n\nChange the impeller yearly.", 3); err != nil {
		t.Fatalf("SetExtracted: %v", err)
	}
	if err := store.UpdateMeta(doc.ID, nil, nil, []string{"diesel"}); err != nil {
		t.Fatalf("UpdateMeta: %v", err)
	}

	names, err := store.TopLevelFolderNames()
	if err != nil {
		t.Fatalf("TopLevelFolderNames: %v", err)
	}
	want := []string{"Manuals", "Receipts"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("expected %v, got %v", want, names)
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

// TestDocumentStore_ListFolderOrdersBySortIndexThenNameFallsBackToNameWhenEqual
// pins plan §2's ordering rule for ListFolder itself (the manuals feature's
// only change to a pre-existing, general-purpose method): sort_index first,
// lower(name)/lower(filename) as the tiebreak. sort_index is set with a raw
// UPDATE (the same whitebox idiom other tests in this file use, e.g.
// notes_store_test.go's force_ocr seeding) because nothing at this layer
// yet writes it directly to a folder outside the manuals feature
// (manuals_store.go's ReorderManualChildren) - this test's job is ListFolder's
// ORDER BY, not that other feature.
func TestDocumentStore_ListFolderOrdersBySortIndexThenNameFallsBackToNameWhenEqual(t *testing.T) {
	store := newTestDocumentStore(t)

	zebra, err := store.CreateFolder("Zebra", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	anchor, err := store.CreateFolder("Anchor", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE document_folders SET sort_index = 0 WHERE id = ?`, zebra.ID); err != nil {
		t.Fatalf("seed sort_index: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE document_folders SET sort_index = 1 WHERE id = ?`, anchor.ID); err != nil {
		t.Fatalf("seed sort_index: %v", err)
	}

	zebraDoc := mustInsertDocument(t, store, "sha-order-z", "z-doc.pdf", nil)
	aDoc := mustInsertDocument(t, store, "sha-order-a", "a-doc.pdf", nil)
	if _, err := store.db.Exec(`UPDATE documents SET sort_index = 1 WHERE id = ?`, zebraDoc.ID); err != nil {
		t.Fatalf("seed sort_index: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE documents SET sort_index = 0 WHERE id = ?`, aDoc.ID); err != nil {
		t.Fatalf("seed sort_index: %v", err)
	}

	subfolders, docs, err := store.ListFolder(nil)
	if err != nil {
		t.Fatalf("ListFolder: %v", err)
	}
	// Zebra (sort_index 0) must sort ahead of Anchor (sort_index 1) despite
	// the alphabetical tiebreak going the other way.
	if len(subfolders) != 2 || subfolders[0].ID != zebra.ID || subfolders[1].ID != anchor.ID {
		t.Fatalf("expected Zebra (sort_index 0) then Anchor (sort_index 1), got %+v", subfolders)
	}
	if len(docs) != 2 || docs[0].ID != aDoc.ID || docs[1].ID != zebraDoc.ID {
		t.Fatalf("expected a-doc.pdf (sort_index 0) then z-doc.pdf (sort_index 1), got %+v", docs)
	}
}

// TestDocumentStore_ListFolderOrdersByNameWhenEverySortIndexIsZero is the
// Verification section's own required case: a pre-manuals-feature library,
// where every row's sort_index is still its DEFAULT 0, must list in exactly
// the alphabetical order it always has - sort_index being first in the
// ORDER BY must never change behaviour for a library that has never
// reordered anything.
func TestDocumentStore_ListFolderOrdersByNameWhenEverySortIndexIsZero(t *testing.T) {
	store := newTestDocumentStore(t)

	if _, err := store.CreateFolder("Zebra", nil); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := store.CreateFolder("Anchor", nil); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	mustInsertDocument(t, store, "sha-namefallback-z", "z-doc.pdf", nil)
	mustInsertDocument(t, store, "sha-namefallback-a", "a-doc.pdf", nil)

	subfolders, docs, err := store.ListFolder(nil)
	if err != nil {
		t.Fatalf("ListFolder: %v", err)
	}
	if len(subfolders) != 2 || subfolders[0].Name != "Anchor" || subfolders[1].Name != "Zebra" {
		t.Fatalf("expected plain alphabetical order (Anchor, Zebra), got %+v", subfolders)
	}
	if len(docs) != 2 || docs[0].Filename != "a-doc.pdf" || docs[1].Filename != "z-doc.pdf" {
		t.Fatalf("expected plain alphabetical order (a-doc.pdf, z-doc.pdf), got %+v", docs)
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

// ── embeddings ───────────────────────────────────────────────────────────

// TestDocumentStore_ChunkEmbeddingsCascadeOnDocumentDelete guards the
// invalidation story documentStoreSchema's comment on
// document_chunk_embeddings describes: chunk_id REFERENCES document_chunks
// (id) ON DELETE CASCADE, so deleting a document's chunks (here via
// Delete's own cascade from documents to document_chunks) takes the
// embedding with it.
func TestDocumentStore_ChunkEmbeddingsCascadeOnDocumentDelete(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-embed-delete", "impeller.pdf", nil)
	if err := store.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "Replace the impeller every 500 hours."},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}
	chunks, err := store.ChunksFrom(doc.ID, 1)
	if err != nil || len(chunks) != 1 {
		t.Fatalf("ChunksFrom: chunks=%+v err=%v", chunks, err)
	}
	chunkID := chunks[0].ID

	if err := store.SetChunkEmbeddings("test-model", 3, map[int64][]float32{chunkID: {1, 0, 0}}); err != nil {
		t.Fatalf("SetChunkEmbeddings: %v", err)
	}
	var before int
	if err := store.db.QueryRow(`SELECT count(*) FROM document_chunk_embeddings WHERE chunk_id = ?`, chunkID).Scan(&before); err != nil {
		t.Fatalf("count before: %v", err)
	}
	if before != 1 {
		t.Fatalf("expected the embedding row to exist before delete, got count %d", before)
	}

	if _, err := store.Delete(doc.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var after int
	if err := store.db.QueryRow(`SELECT count(*) FROM document_chunk_embeddings WHERE chunk_id = ?`, chunkID).Scan(&after); err != nil {
		t.Fatalf("count after: %v", err)
	}
	if after != 0 {
		t.Fatalf("expected ON DELETE CASCADE to remove the embedding when its document is deleted, got count %d", after)
	}
}

// TestDocumentStore_ChunkEmbeddingsCascadeOnReplaceChunks covers the more
// common invalidation path than a delete: a re-extract. ReplaceChunks is
// always DELETE+INSERT, never UPDATE, so the old chunk row (and its
// embedding, via cascade) is gone even though the document itself, and a
// chunk at the same seq, both still exist.
func TestDocumentStore_ChunkEmbeddingsCascadeOnReplaceChunks(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-embed-replace", "impeller.pdf", nil)
	if err := store.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "Replace the impeller every 500 hours."},
	}); err != nil {
		t.Fatalf("ReplaceChunks (initial): %v", err)
	}
	chunks, err := store.ChunksFrom(doc.ID, 1)
	if err != nil || len(chunks) != 1 {
		t.Fatalf("ChunksFrom: chunks=%+v err=%v", chunks, err)
	}
	oldChunkID := chunks[0].ID

	if err := store.SetChunkEmbeddings("test-model", 3, map[int64][]float32{oldChunkID: {1, 0, 0}}); err != nil {
		t.Fatalf("SetChunkEmbeddings: %v", err)
	}

	if err := store.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "Re-extracted text, a different chunk row entirely."},
	}); err != nil {
		t.Fatalf("ReplaceChunks (re-extract): %v", err)
	}

	var count int
	if err := store.db.QueryRow(`SELECT count(*) FROM document_chunk_embeddings WHERE chunk_id = ?`, oldChunkID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected the old chunk's embedding to be gone once ReplaceChunks deleted its row, got count %d", count)
	}
}

// TestDocumentStore_MetaChunkEmbeddingDroppedOnMetadataEdit proves the
// subtle case the brief calls out: rebuildMetaChunkTx (called by
// UpdateMeta) deletes and reinserts the meta chunk (seq 0, source 'meta'),
// exactly like any other chunk replacement, and an embedding computed
// against the old title/tags/notes text must not silently go on describing
// the new text.
//
// This can't be proven by just checking "the new meta chunk has a different
// id than the old one": SQLite's ROWID allocation is free to reuse a
// just-freed id once the table (or, in production, just this document's
// slice of it) has no lower id outstanding, and in this single-document
// test that is exactly what happens - the rebuilt meta chunk comes back as
// the SAME integer id as the one that was just deleted. So the only way to
// actually prove the point is to check the current, post-edit meta chunk's
// embedding row regardless of whether its id happens to match the old one:
// there must be none, because rebuildMetaChunkTx's INSERT never carries an
// embedding forward, whatever integer the new row landed on.
func TestDocumentStore_MetaChunkEmbeddingDroppedOnMetadataEdit(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-meta-embed", "manual.pdf", nil)

	metaChunks, err := store.ChunksFrom(doc.ID, 0)
	if err != nil || len(metaChunks) != 1 || metaChunks[0].Source != "meta" {
		t.Fatalf("ChunksFrom(0): expected exactly the meta chunk, got %+v err=%v", metaChunks, err)
	}
	oldMetaChunkID := metaChunks[0].ID

	if err := store.SetChunkEmbeddings("test-model", 3, map[int64][]float32{oldMetaChunkID: {1, 0, 0}}); err != nil {
		t.Fatalf("SetChunkEmbeddings: %v", err)
	}
	var before int
	if err := store.db.QueryRow(`SELECT count(*) FROM document_chunk_embeddings WHERE chunk_id = ?`, oldMetaChunkID).Scan(&before); err != nil {
		t.Fatalf("count before: %v", err)
	}
	if before != 1 {
		t.Fatalf("expected the meta chunk's embedding to exist before the edit, got count %d", before)
	}

	if err := store.UpdateMeta(doc.ID, strPtr("New Title"), nil, nil); err != nil {
		t.Fatalf("UpdateMeta: %v", err)
	}

	newMetaChunks, err := store.ChunksFrom(doc.ID, 0)
	if err != nil || len(newMetaChunks) != 1 {
		t.Fatalf("ChunksFrom(0) after edit: chunks=%+v err=%v", newMetaChunks, err)
	}
	newMetaChunkID := newMetaChunks[0].ID

	var after int
	if err := store.db.QueryRow(`SELECT count(*) FROM document_chunk_embeddings WHERE chunk_id = ?`, newMetaChunkID).Scan(&after); err != nil {
		t.Fatalf("count after: %v", err)
	}
	if after != 0 {
		t.Fatalf("expected the rebuilt meta chunk (id %d, was %d before the edit) to carry no embedding of its own, got count %d", newMetaChunkID, oldMetaChunkID, after)
	}

	var total int
	if err := store.db.QueryRow(`SELECT count(*) FROM document_chunk_embeddings`).Scan(&total); err != nil {
		t.Fatalf("count total: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected no embedding rows to remain anywhere after the edit, got %d", total)
	}
}

// TestDocumentStore_PendingEmbedChunksFiltersUnembeddedEnrichedAndBlank
// covers PendingEmbedChunks' three filtering rules together: only chunks
// with no document_chunk_embeddings row for the given model are returned,
// enrichedOnly restricts that to documents with enrich=1, and a
// whitespace-only chunk is skipped regardless of either.
func TestDocumentStore_PendingEmbedChunksFiltersUnembeddedEnrichedAndBlank(t *testing.T) {
	store := newTestDocumentStore(t)

	enrichedDoc, err := store.Insert(document{SHA256: "sha-pending-enriched", Filename: "enriched.pdf", MIME: "application/pdf", Enrich: true})
	if err != nil {
		t.Fatalf("Insert(enriched): %v", err)
	}
	if err := store.ReplaceChunks(enrichedDoc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "Impeller replacement on the Yanmar 4JH."},
	}); err != nil {
		t.Fatalf("ReplaceChunks(enriched): %v", err)
	}

	plainDoc, err := store.Insert(document{SHA256: "sha-pending-plain", Filename: "plain.pdf", MIME: "application/pdf", Enrich: false})
	if err != nil {
		t.Fatalf("Insert(plain): %v", err)
	}
	if err := store.ReplaceChunks(plainDoc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "Whitsunday Marine Supplies receipt."},
	}); err != nil {
		t.Fatalf("ReplaceChunks(plain): %v", err)
	}

	blankDoc, err := store.Insert(document{SHA256: "sha-pending-blank", Filename: "blank.pdf", MIME: "application/pdf", Enrich: true})
	if err != nil {
		t.Fatalf("Insert(blank): %v", err)
	}
	if err := store.ReplaceChunks(blankDoc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "   \n\t  "},
	}); err != nil {
		t.Fatalf("ReplaceChunks(blank): %v", err)
	}

	embeddedDoc, err := store.Insert(document{SHA256: "sha-pending-embedded", Filename: "embedded.pdf", MIME: "application/pdf", Enrich: true})
	if err != nil {
		t.Fatalf("Insert(embedded): %v", err)
	}
	if err := store.ReplaceChunks(embeddedDoc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "Already embedded chunk."},
	}); err != nil {
		t.Fatalf("ReplaceChunks(embedded): %v", err)
	}
	embeddedChunks, err := store.ChunksFrom(embeddedDoc.ID, 1)
	if err != nil || len(embeddedChunks) != 1 {
		t.Fatalf("ChunksFrom(embedded): chunks=%+v err=%v", embeddedChunks, err)
	}
	if err := store.SetChunkEmbeddings("test-model", 3, map[int64][]float32{embeddedChunks[0].ID: {1, 0, 0}}); err != nil {
		t.Fatalf("SetChunkEmbeddings: %v", err)
	}

	all, err := store.PendingEmbedChunks("test-model", 3, 100, false)
	if err != nil {
		t.Fatalf("PendingEmbedChunks(all): %v", err)
	}
	for _, c := range all {
		if strings.TrimSpace(c.Text) == "" {
			t.Fatalf("expected PendingEmbedChunks to skip blank/whitespace-only chunks, got %+v", c)
		}
		if c.ID == embeddedChunks[0].ID {
			t.Fatalf("expected the already-embedded chunk to be excluded, got it in %+v", all)
		}
	}
	foundEnrichedBody, foundPlainBody := false, false
	for _, c := range all {
		if c.DocumentID == enrichedDoc.ID && c.Source == "local" {
			foundEnrichedBody = true
		}
		if c.DocumentID == plainDoc.ID && c.Source == "local" {
			foundPlainBody = true
		}
	}
	if !foundEnrichedBody || !foundPlainBody {
		t.Fatalf("expected both the enriched and plain documents' body chunks pending with enrichedOnly=false, got %+v", all)
	}

	enrichedOnly, err := store.PendingEmbedChunks("test-model", 3, 100, true)
	if err != nil {
		t.Fatalf("PendingEmbedChunks(enrichedOnly): %v", err)
	}
	foundEnrichedBody = false
	for _, c := range enrichedOnly {
		if c.DocumentID == plainDoc.ID {
			t.Fatalf("expected enrichedOnly to exclude the non-enriched document entirely, got %+v", c)
		}
		if c.DocumentID == enrichedDoc.ID && c.Source == "local" {
			foundEnrichedBody = true
		}
	}
	if !foundEnrichedBody {
		t.Fatalf("expected the enriched document's body chunk in the enrichedOnly result, got %+v", enrichedOnly)
	}
}

// TestDocumentStore_PendingEmbedChunksReturnsChunksAgainAfterModelChange is
// the "the setting changed" half of PendingEmbedChunks' contract: a chunk
// already embedded under one model is not pending under that model, but is
// pending again under a different model name - no backfill bookkeeping
// required, since "pending" is defined purely by the absence of a row for
// the model asked about.
func TestDocumentStore_PendingEmbedChunksReturnsChunksAgainAfterModelChange(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-model-change", "manual.pdf", nil)
	if err := store.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "Impeller replacement on the Yanmar 4JH."},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}
	chunks, err := store.ChunksFrom(doc.ID, 1)
	if err != nil || len(chunks) != 1 {
		t.Fatalf("ChunksFrom: chunks=%+v err=%v", chunks, err)
	}
	chunkID := chunks[0].ID

	if err := store.SetChunkEmbeddings("old-model", 3, map[int64][]float32{chunkID: {1, 0, 0}}); err != nil {
		t.Fatalf("SetChunkEmbeddings(old-model): %v", err)
	}

	stillOld, err := store.PendingEmbedChunks("old-model", 3, 10, false)
	if err != nil {
		t.Fatalf("PendingEmbedChunks(old-model): %v", err)
	}
	for _, c := range stillOld {
		if c.ID == chunkID {
			t.Fatalf("expected the body chunk to already be embedded under old-model, got it pending: %+v", c)
		}
	}

	underNewModel, err := store.PendingEmbedChunks("new-model", 3, 10, false)
	if err != nil {
		t.Fatalf("PendingEmbedChunks(new-model): %v", err)
	}
	found := false
	for _, c := range underNewModel {
		if c.ID == chunkID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the chunk to be pending again under a new model name, got %+v", underNewModel)
	}
}

func TestDocumentStore_SetChunkEmbeddingsRejectsWrongLengthVector(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-wrong-length", "manual.pdf", nil)
	if err := store.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "Some body text."},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}
	chunks, err := store.ChunksFrom(doc.ID, 1)
	if err != nil || len(chunks) != 1 {
		t.Fatalf("ChunksFrom: chunks=%+v err=%v", chunks, err)
	}

	err = store.SetChunkEmbeddings("test-model", 512, map[int64][]float32{chunks[0].ID: {1, 2, 3}})
	if err == nil {
		t.Fatalf("expected an error for a 3-dimensional vector against dims=512")
	}

	var count int
	if err := store.db.QueryRow(`SELECT count(*) FROM document_chunk_embeddings`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected the rejected write to leave no row behind, got %d", count)
	}
}

// TestDocumentStore_SetChunkEmbeddingsUnknownChunkIDReturnsLegibleError
// covers the foreign key path: chunk_id has no existence check of its own
// before the INSERT, so a bogus id trips document_chunk_embeddings' own
// foreign key. isForeignKeyConstraintErr's whole job is turning that into a
// message naming the id rather than surfacing modernc/sqlite's bare
// "FOREIGN KEY constraint failed" - this pins that the wrapped error still
// mentions the id so an operator (or a caller's own error log) isn't left
// staring at an opaque SQLite string.
func TestDocumentStore_SetChunkEmbeddingsUnknownChunkIDReturnsLegibleError(t *testing.T) {
	store := newTestDocumentStore(t)

	const bogusChunkID int64 = 999999
	err := store.SetChunkEmbeddings("test-model", 3, map[int64][]float32{bogusChunkID: {1, 0, 0}})
	if err == nil {
		t.Fatalf("expected an error for a chunk id that doesn't exist")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d", bogusChunkID)) {
		t.Fatalf("expected the error to name the offending chunk id %d, got %q", bogusChunkID, err.Error())
	}
}

// TestDocumentStore_EmbeddingCountsReportsExpectedTotals builds one
// document embedded under the model being asked about, one embedded under a
// different (stale) model, and one left entirely unembedded (with one
// blank chunk among its chunks, which must not count as pending), then
// checks EmbeddingCounts' five numbers against directly-queried oracles
// rather than hand-computed constants - the meta chunk's exact text is an
// implementation detail of rebuildMetaChunkTx this test has no business
// assuming.
func TestDocumentStore_EmbeddingCountsReportsExpectedTotals(t *testing.T) {
	store := newTestDocumentStore(t)

	embedAllChunks := func(docID, model string) {
		chunks, err := store.ChunksFrom(docID, 0)
		if err != nil {
			t.Fatalf("ChunksFrom(%s): %v", docID, err)
		}
		vectors := make(map[int64][]float32, len(chunks))
		for _, c := range chunks {
			vectors[c.ID] = []float32{1, 0, 0}
		}
		if err := store.SetChunkEmbeddings(model, 3, vectors); err != nil {
			t.Fatalf("SetChunkEmbeddings(%s, %s): %v", docID, model, err)
		}
	}

	current := mustInsertDocument(t, store, "sha-counts-current", "current.pdf", nil)
	if err := store.ReplaceChunks(current.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "current model body text"},
	}); err != nil {
		t.Fatalf("ReplaceChunks(current): %v", err)
	}
	embedAllChunks(current.ID, "current-model")

	stale := mustInsertDocument(t, store, "sha-counts-stale", "stale.pdf", nil)
	if err := store.ReplaceChunks(stale.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "stale model body text"},
	}); err != nil {
		t.Fatalf("ReplaceChunks(stale): %v", err)
	}
	embedAllChunks(stale.ID, "old-model")

	pending := mustInsertDocument(t, store, "sha-counts-pending", "pending.pdf", nil)
	if err := store.ReplaceChunks(pending.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "0123456789"},
		{Seq: 2, Source: "local", Text: "   "}, // blank: must not count as pending
	}); err != nil {
		t.Fatalf("ReplaceChunks(pending): %v", err)
	}

	var wantTotal int
	if err := store.db.QueryRow(`SELECT count(*) FROM document_chunks`).Scan(&wantTotal); err != nil {
		t.Fatalf("count chunks: %v", err)
	}

	oracleRows, err := store.db.Query(`
		SELECT c.text FROM document_chunks c
		WHERE NOT EXISTS (SELECT 1 FROM document_chunk_embeddings e WHERE e.chunk_id = c.id AND e.model = 'current-model' AND e.dims = 3)`)
	if err != nil {
		t.Fatalf("query pending oracle: %v", err)
	}
	var wantPending, wantChars int
	for oracleRows.Next() {
		var text string
		if err := oracleRows.Scan(&text); err != nil {
			t.Fatalf("scan pending oracle: %v", err)
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		wantPending++
		wantChars += utf8.RuneCountInString(text)
	}
	if err := oracleRows.Err(); err != nil {
		t.Fatalf("pending oracle: %v", err)
	}
	oracleRows.Close()

	counts, err := store.EmbeddingCounts("current-model", 3)
	if err != nil {
		t.Fatalf("EmbeddingCounts: %v", err)
	}

	if counts.ChunksTotal != wantTotal {
		t.Fatalf("ChunksTotal: expected %d, got %d", wantTotal, counts.ChunksTotal)
	}
	if counts.ChunksEmbedded != 2 {
		t.Fatalf("ChunksEmbedded: expected 2 (current's meta+body chunk), got %d", counts.ChunksEmbedded)
	}
	if counts.ChunksStale != 2 {
		t.Fatalf("ChunksStale: expected 2 (stale's meta+body chunk under old-model), got %d", counts.ChunksStale)
	}
	if counts.ChunksPending != wantPending {
		t.Fatalf("ChunksPending: expected %d, got %d", wantPending, counts.ChunksPending)
	}
	if counts.CharsPending != wantChars {
		t.Fatalf("CharsPending: expected %d, got %d", wantChars, counts.CharsPending)
	}
}

func TestDocumentStore_SearchVectorRanksNearerVectorFirst(t *testing.T) {
	store := newTestDocumentStore(t)

	near := mustInsertDocument(t, store, "sha-vec-near", "near.pdf", nil)
	if err := store.ReplaceChunks(near.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "Impeller replacement on the Yanmar 4JH."},
	}); err != nil {
		t.Fatalf("ReplaceChunks(near): %v", err)
	}
	far := mustInsertDocument(t, store, "sha-vec-far", "far.pdf", nil)
	if err := store.ReplaceChunks(far.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "Whitsunday Marine Supplies receipt."},
	}); err != nil {
		t.Fatalf("ReplaceChunks(far): %v", err)
	}

	nearChunks, err := store.ChunksFrom(near.ID, 1)
	if err != nil || len(nearChunks) != 1 {
		t.Fatalf("ChunksFrom(near): chunks=%+v err=%v", nearChunks, err)
	}
	farChunks, err := store.ChunksFrom(far.ID, 1)
	if err != nil || len(farChunks) != 1 {
		t.Fatalf("ChunksFrom(far): chunks=%+v err=%v", farChunks, err)
	}
	if err := store.SetChunkEmbeddings("test-model", 2, map[int64][]float32{
		nearChunks[0].ID: {1, 0},
		farChunks[0].ID:  {0, 1},
	}); err != nil {
		t.Fatalf("SetChunkEmbeddings: %v", err)
	}

	results, err := store.SearchVector([]float32{1, 0}, "test-model", nil, false, "", 10)
	if err != nil {
		t.Fatalf("SearchVector: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected both documents, got %+v", results)
	}
	if results[0].DocumentID != near.ID {
		t.Fatalf("expected the near document (cosine 1.0) ranked first, got %+v", results)
	}
	if results[0].Score <= results[1].Score {
		t.Fatalf("expected the near document's score to exceed the far document's, got %v vs %v", results[0].Score, results[1].Score)
	}
}

func TestDocumentStore_SearchVectorRespectsFolderFilter(t *testing.T) {
	store := newTestDocumentStore(t)

	manuals, err := store.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	inFolder := mustInsertDocument(t, store, "sha-vec-folder-in", "in.pdf", &manuals.ID)
	if err := store.ReplaceChunks(inFolder.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "impeller torque spec"},
	}); err != nil {
		t.Fatalf("ReplaceChunks(inFolder): %v", err)
	}
	outFolder := mustInsertDocument(t, store, "sha-vec-folder-out", "out.pdf", nil)
	if err := store.ReplaceChunks(outFolder.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "impeller torque spec too"},
	}); err != nil {
		t.Fatalf("ReplaceChunks(outFolder): %v", err)
	}

	inChunks, err := store.ChunksFrom(inFolder.ID, 1)
	if err != nil || len(inChunks) != 1 {
		t.Fatalf("ChunksFrom(inFolder): chunks=%+v err=%v", inChunks, err)
	}
	outChunks, err := store.ChunksFrom(outFolder.ID, 1)
	if err != nil || len(outChunks) != 1 {
		t.Fatalf("ChunksFrom(outFolder): chunks=%+v err=%v", outChunks, err)
	}
	if err := store.SetChunkEmbeddings("test-model", 2, map[int64][]float32{
		inChunks[0].ID:  {1, 0},
		outChunks[0].ID: {1, 0},
	}); err != nil {
		t.Fatalf("SetChunkEmbeddings: %v", err)
	}

	results, err := store.SearchVector([]float32{1, 0}, "test-model", &manuals.ID, false, "", 10)
	if err != nil {
		t.Fatalf("SearchVector: %v", err)
	}
	if len(results) != 1 || results[0].DocumentID != inFolder.ID {
		t.Fatalf("expected only the document inside Manuals, got %+v", results)
	}
}

func TestDocumentStore_SearchVectorRespectsTagFilter(t *testing.T) {
	store := newTestDocumentStore(t)

	tagged := mustInsertDocument(t, store, "sha-vec-tag-yes", "tagged.pdf", nil)
	if err := store.UpdateMeta(tagged.ID, nil, nil, []string{"engine"}); err != nil {
		t.Fatalf("UpdateMeta(tagged): %v", err)
	}
	if err := store.ReplaceChunks(tagged.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "impeller torque spec"},
	}); err != nil {
		t.Fatalf("ReplaceChunks(tagged): %v", err)
	}
	untagged := mustInsertDocument(t, store, "sha-vec-tag-no", "untagged.pdf", nil)
	if err := store.ReplaceChunks(untagged.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "impeller torque spec too"},
	}); err != nil {
		t.Fatalf("ReplaceChunks(untagged): %v", err)
	}

	taggedChunks, err := store.ChunksFrom(tagged.ID, 1)
	if err != nil || len(taggedChunks) != 1 {
		t.Fatalf("ChunksFrom(tagged): chunks=%+v err=%v", taggedChunks, err)
	}
	untaggedChunks, err := store.ChunksFrom(untagged.ID, 1)
	if err != nil || len(untaggedChunks) != 1 {
		t.Fatalf("ChunksFrom(untagged): chunks=%+v err=%v", untaggedChunks, err)
	}
	if err := store.SetChunkEmbeddings("test-model", 2, map[int64][]float32{
		taggedChunks[0].ID:   {1, 0},
		untaggedChunks[0].ID: {1, 0},
	}); err != nil {
		t.Fatalf("SetChunkEmbeddings: %v", err)
	}

	results, err := store.SearchVector([]float32{1, 0}, "test-model", nil, false, "engine", 10)
	if err != nil {
		t.Fatalf("SearchVector: %v", err)
	}
	if len(results) != 1 || results[0].DocumentID != tagged.ID {
		t.Fatalf("expected only the tagged document, got %+v", results)
	}
}

func TestDocumentStore_SearchVectorUnknownFolderReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	_, err := store.SearchVector([]float32{1, 0}, "test-model", strPtr("does-not-exist"), false, "", 10)
	if !errors.Is(err, errFolderNotFound) {
		t.Fatalf("expected errFolderNotFound, got %v", err)
	}
}

// TestDocumentStore_SearchVectorSkipsWrongDimsRow covers a library mid
// model-change: one chunk still carries a vector from the old
// assistant.embedding_dimensions setting, one carries a vector at the
// current setting. SearchVector must silently skip the mismatched row
// (PendingEmbedChunks/the backfill is what fixes it) rather than erroring
// the whole search or crashing on a length mismatch inside dotProduct.
func TestDocumentStore_SearchVectorSkipsWrongDimsRow(t *testing.T) {
	store := newTestDocumentStore(t)

	wrongDims := mustInsertDocument(t, store, "sha-vec-wrong-dims", "wrong.pdf", nil)
	if err := store.ReplaceChunks(wrongDims.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "old dimensionality chunk"},
	}); err != nil {
		t.Fatalf("ReplaceChunks(wrongDims): %v", err)
	}
	wrongChunks, err := store.ChunksFrom(wrongDims.ID, 1)
	if err != nil || len(wrongChunks) != 1 {
		t.Fatalf("ChunksFrom(wrongDims): chunks=%+v err=%v", wrongChunks, err)
	}
	if err := store.SetChunkEmbeddings("test-model", 3, map[int64][]float32{wrongChunks[0].ID: {1, 0, 0}}); err != nil {
		t.Fatalf("SetChunkEmbeddings(3-dim): %v", err)
	}

	rightDims := mustInsertDocument(t, store, "sha-vec-right-dims", "right.pdf", nil)
	if err := store.ReplaceChunks(rightDims.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "current dimensionality chunk"},
	}); err != nil {
		t.Fatalf("ReplaceChunks(rightDims): %v", err)
	}
	rightChunks, err := store.ChunksFrom(rightDims.ID, 1)
	if err != nil || len(rightChunks) != 1 {
		t.Fatalf("ChunksFrom(rightDims): chunks=%+v err=%v", rightChunks, err)
	}
	if err := store.SetChunkEmbeddings("test-model", 2, map[int64][]float32{rightChunks[0].ID: {1, 0}}); err != nil {
		t.Fatalf("SetChunkEmbeddings(2-dim): %v", err)
	}

	results, err := store.SearchVector([]float32{1, 0}, "test-model", nil, false, "", 10)
	if err != nil {
		t.Fatalf("SearchVector: %v", err)
	}
	if len(results) != 1 || results[0].DocumentID != rightDims.ID {
		t.Fatalf("expected the wrong-dims row silently skipped and only the matching-dims document returned, got %+v", results)
	}
}

// TestDocumentStore_SearchVectorErrorsOnCorruptBlob covers the other side
// of the same coin: a dims mismatch is a stale setting and is skipped, but
// a vector blob that doesn't even decode is corruption, and SearchVector
// must surface that as an error rather than skip it the same way.
func TestDocumentStore_SearchVectorErrorsOnCorruptBlob(t *testing.T) {
	store := newTestDocumentStore(t)

	doc := mustInsertDocument(t, store, "sha-vec-corrupt", "corrupt.pdf", nil)
	if err := store.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "some body text"},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}
	chunks, err := store.ChunksFrom(doc.ID, 1)
	if err != nil || len(chunks) != 1 {
		t.Fatalf("ChunksFrom: chunks=%+v err=%v", chunks, err)
	}
	if err := store.SetChunkEmbeddings("test-model", 2, map[int64][]float32{chunks[0].ID: {1, 0}}); err != nil {
		t.Fatalf("SetChunkEmbeddings: %v", err)
	}

	// Corrupt the stored blob directly, modelling on-disk bit rot or a
	// truncated write - not anything SetChunkEmbeddings itself could ever
	// produce, which is exactly why this has to be provoked by hand.
	if _, err := store.db.Exec(`UPDATE document_chunk_embeddings SET vector = ? WHERE chunk_id = ?`, []byte{1, 2, 3}, chunks[0].ID); err != nil {
		t.Fatalf("corrupt blob: %v", err)
	}

	if _, err := store.SearchVector([]float32{1, 0}, "test-model", nil, false, "", 10); err == nil {
		t.Fatalf("expected an error for a corrupt (non-multiple-of-4) vector blob, not a silently skipped row")
	}
}

// TestDocumentStore_SearchVectorErrorsOnDimsColumnLie covers the gap
// between the two tests above: a row whose dims column says 2 while its
// blob holds only one float32 passes the dims check (which compares the
// column, not the blob) and then reaches dotProduct, which indexes the
// decoded vector by the query's length. Left unguarded that is an
// out-of-range panic in the middle of a search, taking the request down
// with it; the row is corrupt, so it has to surface as an error the same
// way a blob that fails to decode at all does.
func TestDocumentStore_SearchVectorErrorsOnDimsColumnLie(t *testing.T) {
	store := newTestDocumentStore(t)

	doc := mustInsertDocument(t, store, "sha-vec-dims-lie", "dims-lie.pdf", nil)
	if err := store.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "some body text"},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}
	chunks, err := store.ChunksFrom(doc.ID, 1)
	if err != nil || len(chunks) != 1 {
		t.Fatalf("ChunksFrom: chunks=%+v err=%v", chunks, err)
	}
	if err := store.SetChunkEmbeddings("test-model", 2, map[int64][]float32{chunks[0].ID: {1, 0}}); err != nil {
		t.Fatalf("SetChunkEmbeddings: %v", err)
	}

	// A well-formed one-dimension blob under a dims column still claiming
	// two: decodable, so the corrupt-blob path never fires, but a dimension
	// short of what the query expects.
	if _, err := store.db.Exec(
		`UPDATE document_chunk_embeddings SET vector = ? WHERE chunk_id = ?`,
		encodeEmbedding([]float32{1}), chunks[0].ID,
	); err != nil {
		t.Fatalf("shorten blob: %v", err)
	}

	if _, err := store.SearchVector([]float32{1, 0}, "test-model", nil, false, "", 10); err == nil {
		t.Fatalf("expected an error for a vector blob shorter than its own dims column, not a panic or a scored row")
	}
}

// TestDocumentStore_RaisingDimensionsRequeuesEveryChunk covers the way a
// settings change could otherwise brick semantic search with no way back.
// A vector's usefulness depends on its length as much as on which model
// produced it: SearchVector skips any row whose dims don't match the query's.
// So if "already embedded" is judged on the model name alone, raising
// assistant.embedding_dimensions leaves every stored vector at the old
// length, unusable and invisible - searches keep reporting hybrid mode while
// matching nothing, the pending count reads zero, and neither the automatic
// pass nor a backfill ever repairs it, because by that predicate there is
// nothing left to do.
func TestDocumentStore_RaisingDimensionsRequeuesEveryChunk(t *testing.T) {
	store := newTestDocumentStore(t)

	doc := mustInsertDocument(t, store, "sha-dims-change", "manual.pdf", nil)
	if err := store.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "impeller replacement"},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}
	// Every chunk, meta chunk included - this test is about what the pending
	// predicate considers done, so nothing may be left outstanding for an
	// unrelated reason.
	chunks, err := store.ChunksFrom(doc.ID, 0)
	if err != nil || len(chunks) != 2 {
		t.Fatalf("ChunksFrom: chunks=%d err=%v", len(chunks), err)
	}
	vectors := map[int64][]float32{}
	for i, c := range chunks {
		vectors[c.ID] = []float32{float32(i + 1), 1}
	}
	if err := store.SetChunkEmbeddings("m", 2, vectors); err != nil {
		t.Fatalf("SetChunkEmbeddings: %v", err)
	}

	// Same model, same dimensions: nothing to do.
	same, err := store.PendingEmbedChunks("m", 2, 10, false)
	if err != nil {
		t.Fatalf("PendingEmbedChunks(same): %v", err)
	}
	if len(same) != 0 {
		t.Fatalf("expected nothing pending at the dimensions it was embedded at, got %d", len(same))
	}

	// Same model, a different vector length: every row is stale and has to
	// come back round.
	wider, err := store.PendingEmbedChunks("m", 4, 10, false)
	if err != nil {
		t.Fatalf("PendingEmbedChunks(wider): %v", err)
	}
	if len(wider) != len(chunks) {
		t.Fatalf("expected every chunk to be requeued after a dimensions change, got %d of %d", len(wider), len(chunks))
	}

	counts, err := store.EmbeddingCounts("m", 4)
	if err != nil {
		t.Fatalf("EmbeddingCounts: %v", err)
	}
	if counts.ChunksEmbedded != 0 {
		t.Fatalf("a vector of the wrong length is not embedded, got ChunksEmbedded=%d", counts.ChunksEmbedded)
	}
	if counts.ChunksStale != len(chunks) {
		t.Fatalf("expected the old-length vectors to count as stale, got %d of %d", counts.ChunksStale, len(chunks))
	}
	if counts.ChunksPending != len(chunks) {
		t.Fatalf("expected every chunk pending after the dimensions change, got %d of %d", counts.ChunksPending, len(chunks))
	}
}

// TestDocumentStore_EmbeddingCountsSeparatesAutomaticFromLibraryWide pins the
// distinction the panel's poll depends on. ChunksPending is library-wide,
// which is what the backfill offer is about; the automatic pass only ever
// touches enrich=1 documents, so a library holding anything uploaded while
// Mate was off has a permanently non-zero ChunksPending that no background
// work will ever reduce. A poll watching that number never stops. Counting
// the automatic pass's own queue separately gives it something that actually
// falls to zero.
func TestDocumentStore_EmbeddingCountsSeparatesAutomaticFromLibraryWide(t *testing.T) {
	store := newTestDocumentStore(t)

	consented, err := store.Insert(document{SHA256: "sha-auto-yes", Filename: "consented.pdf", Enrich: true})
	if err != nil {
		t.Fatalf("Insert(consented): %v", err)
	}
	if err := store.ReplaceChunks(consented.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "body text the automatic pass will embed"},
	}); err != nil {
		t.Fatalf("ReplaceChunks(consented): %v", err)
	}

	withheld, err := store.Insert(document{SHA256: "sha-auto-no", Filename: "withheld.pdf", Enrich: false})
	if err != nil {
		t.Fatalf("Insert(withheld): %v", err)
	}
	if err := store.ReplaceChunks(withheld.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "body text only a backfill will ever reach"},
	}); err != nil {
		t.Fatalf("ReplaceChunks(withheld): %v", err)
	}

	before, err := store.EmbeddingCounts("m", 2)
	if err != nil {
		t.Fatalf("EmbeddingCounts: %v", err)
	}
	if before.ChunksPending != 4 {
		t.Fatalf("expected 4 chunks pending library-wide (two documents, meta plus body), got %d", before.ChunksPending)
	}
	if before.ChunksPendingAuto != 2 {
		t.Fatalf("expected 2 chunks pending for the automatic pass, got %d", before.ChunksPendingAuto)
	}

	// Embed everything the automatic pass would: its own queue empties, the
	// library-wide count does not.
	chunks, err := store.ChunksFrom(consented.ID, 0)
	if err != nil {
		t.Fatalf("ChunksFrom: %v", err)
	}
	vectors := map[int64][]float32{}
	for i, c := range chunks {
		vectors[c.ID] = []float32{float32(i + 1), 1}
	}
	if err := store.SetChunkEmbeddings("m", 2, vectors); err != nil {
		t.Fatalf("SetChunkEmbeddings: %v", err)
	}

	after, err := store.EmbeddingCounts("m", 2)
	if err != nil {
		t.Fatalf("EmbeddingCounts: %v", err)
	}
	if after.ChunksPendingAuto != 0 {
		t.Fatalf("expected the automatic queue to empty, got %d", after.ChunksPendingAuto)
	}
	if after.ChunksPending != 2 {
		t.Fatalf("expected the unconsented document to stay pending library-wide, got %d", after.ChunksPending)
	}
}
