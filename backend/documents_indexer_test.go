package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ── shared test helpers ────────────────────────────────────────────────

// writeTestDocumentBytes writes raw into dir under its lowercase sha256 hex
// name (ADR 0106's on-disk naming) and returns that hash, mirroring what
// uploadDocumentHandler does after a real multipart upload.
func writeTestDocumentBytes(t *testing.T, dir string, raw []byte) string {
	t.Helper()
	sum := sha256.Sum256(raw)
	sha := hex.EncodeToString(sum[:])
	if err := os.WriteFile(filepath.Join(dir, sha), raw, 0o644); err != nil {
		t.Fatalf("write document file: %v", err)
	}
	return sha
}

// seedTestDocumentFile writes fixturePath's bytes into dir and inserts a
// matching pending/extract document row, the same state an upload leaves
// behind for the indexer to pick up.
func seedTestDocumentFile(t *testing.T, store *documentStore, dir, fixturePath, filename, mimeType string, enrich bool) document {
	t.Helper()
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixturePath, err)
	}
	sha := writeTestDocumentBytes(t, dir, raw)
	doc, err := store.Insert(document{SHA256: sha, Filename: filename, MIME: mimeType, SizeBytes: int64(len(raw)), Enrich: enrich})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	return doc
}

// buildDistinctTwoPagePDF assembles a well-formed, fully valid 2-page PDF
// (same object layout as buildTwoPagePDFPage2BrokenContentStream in
// documents_extract_test.go, but with both pages' content streams intact)
// whose text embeds marker - so two calls with different markers produce
// genuinely different bytes, and therefore different sha256 hashes.
// documentStore.Insert treats identical bytes as a duplicate of an existing
// document (ADR 0106's dedupe), so a test that wants several distinct
// pending documents can't just seed the same fixture file more than once.
func buildDistinctTwoPagePDF(t *testing.T, marker string) []byte {
	t.Helper()
	var buf strings.Builder
	offsets := make(map[int]int)

	writeObj := func(num int, body string) {
		offsets[num] = buf.Len()
		buf.WriteString(strconv.Itoa(num))
		buf.WriteString(" 0 obj\n")
		buf.WriteString(body)
		buf.WriteString("\nendobj\n")
	}

	page1Stream := "BT /F1 12 Tf 72 700 Td (PAGE ONE " + marker + ") Tj ET"
	page2Stream := "BT /F1 12 Tf 72 700 Td (PAGE TWO " + marker + ") Tj ET"

	buf.WriteString("%PDF-1.4\n")
	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids [4 0 R 6 0 R] /Count 2 >>")
	writeObj(3, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	writeObj(4, "<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 3 0 R >> >> /MediaBox [0 0 612 792] /Contents 5 0 R >>")
	writeObj(5, "<< /Length "+strconv.Itoa(len(page1Stream))+" >>\nstream\n"+page1Stream+"\nendstream")
	writeObj(6, "<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 3 0 R >> >> /MediaBox [0 0 612 792] /Contents 7 0 R >>")
	writeObj(7, "<< /Length "+strconv.Itoa(len(page2Stream))+" >>\nstream\n"+page2Stream+"\nendstream")

	xrefStart := buf.Len()
	buf.WriteString("xref\n0 8\n")
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= 7; i++ {
		buf.WriteString(pad10(offsets[i]))
		buf.WriteString(" 00000 n \n")
	}
	buf.WriteString("trailer\n<< /Size 8 /Root 1 0 R >>\nstartxref\n")
	buf.WriteString(strconv.Itoa(xrefStart))
	buf.WriteString("\n%%EOF")

	return []byte(buf.String())
}

// seedDistinctPendingPDF writes a buildDistinctTwoPagePDF(marker) and
// inserts a matching pending/extract document row.
func seedDistinctPendingPDF(t *testing.T, store *documentStore, dir, marker, filename string, enrich bool) document {
	t.Helper()
	raw := buildDistinctTwoPagePDF(t, marker)
	sha := writeTestDocumentBytes(t, dir, raw)
	doc, err := store.Insert(document{SHA256: sha, Filename: filename, MIME: "application/pdf", SizeBytes: int64(len(raw)), Enrich: enrich})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	return doc
}

// newTestDocumentIndexer builds a documentIndexer wired to a fresh
// fakeOpenRouterDoer (openrouter_client_test.go) and a readiness func that
// reports Mate on and configured by default - tests override idx.readiness,
// idx.doer, idx.ocrPageCap or idx.extract directly for the scenario they
// need, the same struct-literal-override idiom other *_test.go files in
// this package use for their own fakes.
func newTestDocumentIndexer(t *testing.T, store *documentStore, dir string) *documentIndexer {
	t.Helper()
	idx := newDocumentIndexer(
		store, dir,
		func() (assistantReadiness, string, error) {
			// DocumentModel must be set here too: runEnrichStage runs every
			// readiness result (this stub included) through
			// documentEnrichReadinessProblem, which fails the document on a
			// blank document model exactly like the chat model.
			return assistantReadiness{Enabled: true, Configured: true, Model: "m", DocumentModel: "google/gemini-2.5-flash"}, "sk-test", nil
		},
		&fakeOpenRouterDoer{},
		func() (string, error) { return "google/gemini-2.5-flash", nil },
	)
	// Short enough that a genuine bug (e.g. a select that never returns)
	// fails the test in seconds rather than hanging the suite.
	idx.extractTimeout = 2 * time.Second
	idx.ocrTimeout = 2 * time.Second
	idx.enrichTimeout = 2 * time.Second
	return idx
}

// ── Mate off ────────────────────────────────────────────────────────────

func TestDocumentIndexer_MateOffIndexesLocallyWithNoHTTPCall(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	doc := seedTestDocumentFile(t, store, dir, testdataPath("two_page.pdf"), "manual.pdf", "application/pdf", false)

	doer := &fakeOpenRouterDoer{}
	idx := newTestDocumentIndexer(t, store, dir)
	idx.doer = doer

	processed, err := idx.processOne(context.Background())
	if err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if !processed {
		t.Fatalf("expected processOne to find the pending document")
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "indexed" || got.Stage != "done" || got.IndexedWith != "local" {
		t.Fatalf("expected indexed/done/local, got %+v", got)
	}
	if len(doer.requests) != 0 {
		t.Fatalf("expected no HTTP calls with Mate off, got %d", len(doer.requests))
	}

	query, ok := ftsMatchQuery("THERMOSTATTOKEN")
	if !ok {
		t.Fatalf("ftsMatchQuery")
	}
	results, err := store.Search(query, nil, false, "", 10, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected the local chunks to be searchable, got %d results", len(results))
	}
}

func TestDocumentIndexer_ProcessOneReturnsFalseWhenNothingPending(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	idx := newTestDocumentIndexer(t, store, dir)

	processed, err := idx.processOne(context.Background())
	if err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if processed {
		t.Fatalf("expected processOne to report nothing processed on an empty queue")
	}
}

// ── octet-stream: enrich on, but nothing Mate could do with it ───────────

func TestDocumentIndexer_OctetStreamIndexesLocallyEvenWithEnrichOn(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	raw := []byte("binary-ish content with no known type")
	sha := writeTestDocumentBytes(t, dir, raw)
	doc, err := store.Insert(document{SHA256: sha, Filename: "data.bin", MIME: "application/octet-stream", SizeBytes: int64(len(raw)), Enrich: true})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	doer := &fakeOpenRouterDoer{}
	idx := newTestDocumentIndexer(t, store, dir)
	idx.doer = doer

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "indexed" || got.IndexedWith != "local" {
		t.Fatalf("expected octet-stream to index locally despite enrich=true, got %+v", got)
	}
	if len(doer.requests) != 0 {
		t.Fatalf("expected no HTTP call for octet-stream, got %d", len(doer.requests))
	}
}

// ── heic: unsupported, fails with a clear message ─────────────────────────

func TestDocumentIndexer_HEICFailsWithConvertMessage(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	raw := []byte("not really a heic file, content is irrelevant")
	sha := writeTestDocumentBytes(t, dir, raw)
	doc, err := store.Insert(document{SHA256: sha, Filename: "photo.heic", MIME: "image/heic", SizeBytes: int64(len(raw)), Enrich: true})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	doer := &fakeOpenRouterDoer{}
	idx := newTestDocumentIndexer(t, store, dir)
	idx.doer = doer

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("expected failed status, got %q", got.Status)
	}
	if got.Error != "image/heic is not supported for reading; convert to JPEG" {
		t.Fatalf("unexpected error message: %q", got.Error)
	}
	if len(doer.requests) != 0 {
		t.Fatalf("expected no HTTP call for heic, got %d", len(doer.requests))
	}
}

// ── readiness lost ─────────────────────────────────────────────────────

func TestDocumentIndexer_ReadinessLostFailsButLocalChunksStaySearchable(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	doc := seedTestDocumentFile(t, store, dir, testdataPath("two_page.pdf"), "manual.pdf", "application/pdf", true)

	idx := newTestDocumentIndexer(t, store, dir)
	idx.readiness = func() (assistantReadiness, string, error) {
		return assistantReadiness{Enabled: false, Problem: "The assistant is switched off. Enable it in Settings → Assistant."}, "", nil
	}

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("expected failed status, got %q", got.Status)
	}
	if !strings.Contains(got.Error, "switched off") {
		t.Fatalf("expected the readiness problem text in the error, got %q", got.Error)
	}

	query, ok := ftsMatchQuery("THERMOSTATTOKEN")
	if !ok {
		t.Fatalf("ftsMatchQuery")
	}
	results, err := store.Search(query, nil, false, "", 10, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected local chunks to remain searchable after a lost-readiness enrich failure, got %d", len(results))
	}
}

// ── readiness read error (not a Problem) propagates for Run's backoff ────

// TestDocumentIndexer_ReadinessReadErrorPropagatesFromProcessOne pins the
// review finding: idx.readiness() returning a genuine infra error (a broken
// settings file, a secrets-store read failure - not "Mate isn't
// configured") used to be logged and swallowed inside runEnrichStage, so
// processOne reported (true, nil). Run would then loop straight back to
// NextPending, get the same still-pending document, and repeat forever with
// no documentsIndexerErrorBackoff wait and no operator-visible failure.
// processOne must surface the error instead, so Run's own error branch
// applies the backoff, and the document must stay pending/enrich - not
// failed - for the next attempt to retry.
func TestDocumentIndexer_ReadinessReadErrorPropagatesFromProcessOne(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	doc := seedTestDocumentFile(t, store, dir, testdataPath("two_page.pdf"), "manual.pdf", "application/pdf", true)

	idx := newTestDocumentIndexer(t, store, dir)
	readinessErr := errors.New("read settings: permission denied")
	idx.readiness = func() (assistantReadiness, string, error) {
		return assistantReadiness{}, "", readinessErr
	}

	_, err := idx.processOne(context.Background())
	if err == nil {
		t.Fatalf("expected processOne to propagate the readiness read error so Run's backoff applies, got nil")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected the underlying error text in the returned error, got %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "pending" {
		t.Fatalf("expected the document to remain pending for a retry, not be marked failed, got status %q (error %q)", got.Status, got.Error)
	}
	if got.Stage != "enrich" {
		t.Fatalf("expected the document to remain at stage enrich, got %q", got.Stage)
	}
}

// ── a failed SetFailed must not spin the loop ────────────────────────────

// TestDocumentIndexer_FailedSetFailedPropagatesFromProcessOne pins the
// review finding at documents_indexer.go:175: failDoc used to swallow
// idx.store.SetFailed's own error (logged and dropped), so every caller
// still returned as if the document had been successfully marked failed. If
// SetFailed itself fails (a read-only or full database - the two realistic
// causes), the document's row stays untouched (still pending), but
// processOne still reported (true, nil) - "I did work" - so Run looped
// straight back to NextPending, popped the SAME still-pending document, and
// repeated: no documentsIndexerErrorBackoff wait, no operator-visible
// failure, just a tight spin. This is the same shape as
// TestDocumentIndexer_ReadinessReadErrorPropagatesFromProcessOne above, but
// for a write instead of a read: it chmods the sqlite database's own
// directory read-only (0o555) so NextPending's SELECT still succeeds but
// any write - including failDoc's own SetFailed, reached here via a forced
// extraction error - fails with "attempt to write a readonly database".
// Chmodding the db FILE itself is not enough to reproduce this: the
// connection's file descriptor was opened (read-write) before the chmod,
// and Unix permission checks apply at open(), not at write(), so writes
// through an already-open fd keep working regardless of the file's mode
// bits afterward - confirmed empirically against this driver before
// writing this test.
func TestDocumentIndexer_FailedSetFailedPropagatesFromProcessOne(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "documents.sqlite")
	store, err := newDocumentStore(dbPath)
	if err != nil {
		t.Fatalf("newDocumentStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	docsDir := t.TempDir()
	t.Setenv("DOCUMENTS_DIR", docsDir)
	prevStore := globalDocumentStore
	globalDocumentStore = store
	t.Cleanup(func() { globalDocumentStore = prevStore })

	doc := seedTestDocumentFile(t, store, docsDir, testdataPath("two_page.pdf"), "manual.pdf", "application/pdf", false)

	idx := newTestDocumentIndexer(t, store, docsDir)
	idx.extract = func(ctx context.Context, path, mimeType string) (extractedDocument, error) {
		return extractedDocument{}, errors.New("boom: extraction failed")
	}

	// Make every WRITE to the sqlite file fail while NextPending's own
	// SELECT still succeeds - a read-only database, per the finding's own
	// wording. See the doc comment above for why this chmods the
	// containing directory rather than the db file itself.
	if err := os.Chmod(dbDir, 0o555); err != nil {
		t.Fatalf("chmod db dir read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dbDir, 0o755) })

	_, err = idx.processOne(context.Background())
	if err == nil {
		t.Fatalf("expected processOne to propagate SetFailed's own write error so Run's backoff applies, got nil")
	}

	if err := os.Chmod(dbDir, 0o755); err != nil {
		t.Fatalf("chmod db dir writable again: %v", err)
	}
	got, getErr := store.Get(doc.ID)
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if got.Status != "pending" {
		t.Fatalf("expected the document to remain pending (SetFailed never actually wrote), got %q", got.Status)
	}
}

// ── resuming at stage=enrich after a restart skips re-extraction ─────────

// TestDocumentIndexer_ResumedAtEnrichStageSkipsReExtraction pins the review
// finding: processOne ran idx.runExtractStage unconditionally, so a document
// left pending/enrich by a crash between SetStage(...,"enrich") and
// runEnrichStage finishing got re-extracted from scratch on the next boot -
// up to idx.extractTimeout wasted, and an extraction hiccup on the retry
// would fail a document whose extraction had already genuinely succeeded.
// This seeds exactly that resumed state (SetExtracted + SetStage("enrich"),
// the same writes finishExtract/processOne make) and proves idx.extract is
// never called for it.
func TestDocumentIndexer_ResumedAtEnrichStageSkipsReExtraction(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	doc := seedTestDocumentFile(t, store, dir, testdataPath("two_page.pdf"), "manual.pdf", "application/pdf", true)

	// A text-rich markdown/page_count pair (as a real extract pass would
	// have left behind) so the enrich stage's own documentNeedsOCR check
	// takes the no-OCR-fee text branch, not the scanned-PDF branch.
	longMarkdown := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 5)
	if err := store.SetExtracted(doc.ID, longMarkdown, 1); err != nil {
		t.Fatalf("SetExtracted: %v", err)
	}
	if err := store.SetStage(doc.ID, "enrich"); err != nil {
		t.Fatalf("SetStage: %v", err)
	}

	extractCalled := false
	idx := newTestDocumentIndexer(t, store, dir)
	idx.extract = func(ctx context.Context, path, mimeType string) (extractedDocument, error) {
		extractCalled = true
		return extractedDocument{}, fmt.Errorf("extraction must not run again for a document already at stage enrich")
	}

	fixture, err := os.ReadFile("testdata/openrouter_document_pdf.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, string(fixture))}}
	idx.doer = doer

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	if extractCalled {
		t.Fatalf("expected extraction to be skipped for a document already resumed at stage enrich")
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "indexed" || got.IndexedWith != "mate" {
		t.Fatalf("expected the document to be enriched without re-extraction, got %+v", got)
	}
}

// ── extraction timeout ─────────────────────────────────────────────────

// TestDocumentIndexer_ExtractionTimeoutFailsAndNextDocumentStillProcessed
// injects an extract func that hangs forever for one document (never
// observing ctx at all - a stand-in for a pathological PDF that never
// returns from the underlying library call), proving runExtractStage
// abandons it once extractTimeout elapses rather than waiting it out, and
// that the indexer then moves on to the next pending document instead of
// getting stuck.
func TestDocumentIndexer_ExtractionTimeoutFailsAndNextDocumentStillProcessed(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	blocked := seedDistinctPendingPDF(t, store, dir, "BLOCKED", "blocked.pdf", false)
	fine := seedDistinctPendingPDF(t, store, dir, "FINE", "fine.pdf", false)

	idx := newTestDocumentIndexer(t, store, dir)
	idx.extractTimeout = 50 * time.Millisecond

	unblock := make(chan struct{})
	t.Cleanup(func() { close(unblock) })
	realExtract := extractDocumentText
	idx.extract = func(ctx context.Context, path, mimeType string) (extractedDocument, error) {
		if strings.Contains(path, blocked.SHA256) {
			<-unblock // never closed during the test body itself
			return extractedDocument{}, nil
		}
		return realExtract(ctx, path, mimeType)
	}

	for i := 0; i < 2; i++ {
		if _, err := idx.processOne(context.Background()); err != nil {
			t.Fatalf("processOne %d: %v", i, err)
		}
	}

	gotBlocked, err := store.Get(blocked.ID)
	if err != nil {
		t.Fatalf("Get blocked: %v", err)
	}
	if gotBlocked.Status != "failed" || !strings.Contains(gotBlocked.Error, "timed out") {
		t.Fatalf("expected the blocked document to fail with a timeout message, got %+v", gotBlocked)
	}

	gotFine, err := store.Get(fine.ID)
	if err != nil {
		t.Fatalf("Get fine: %v", err)
	}
	if gotFine.Status != "indexed" {
		t.Fatalf("expected the second document to still be processed and indexed, got %+v", gotFine)
	}
}

// ── FailedPages: partial extraction failure stays visible ─────────────────

func TestDocumentIndexer_FailedPagesMessageStaysVisibleOnAnIndexedDocument(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	raw := buildTwoPagePDFPage2BrokenContentStream(t)
	sha := writeTestDocumentBytes(t, dir, raw)
	doc, err := store.Insert(document{SHA256: sha, Filename: "half-broken.pdf", MIME: "application/pdf", SizeBytes: int64(len(raw)), Enrich: false})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	idx := newTestDocumentIndexer(t, store, dir)
	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "indexed" {
		t.Fatalf("expected the document to still reach indexed despite one bad page, got status %q", got.Status)
	}
	if !strings.Contains(got.Error, "1 of 2 pages unreadable") {
		t.Fatalf("expected the FailedPages warning to survive onto the indexed document, got %q", got.Error)
	}
}

// ── page cap / force_ocr ───────────────────────────────────────────────

func TestDocumentIndexer_PageCapExceededFailsWithPagesAndCost(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	doc := seedTestDocumentFile(t, store, dir, testdataPath("scanned_two_page.pdf"), "scan.pdf", "application/pdf", true)

	doer := &fakeOpenRouterDoer{}
	idx := newTestDocumentIndexer(t, store, dir)
	idx.doer = doer
	idx.ocrPageCap = 1 // scanned_two_page.pdf has 2 pages, over this cap

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("expected failed status, got %q", got.Status)
	}
	if !strings.Contains(got.Error, "2") {
		t.Fatalf("expected the page count in the error, got %q", got.Error)
	}
	if !strings.Contains(got.Error, "$") {
		t.Fatalf("expected an estimated cost in the error, got %q", got.Error)
	}
	if len(doer.requests) != 0 {
		t.Fatalf("expected no HTTP call once the page cap rejects the document, got %d", len(doer.requests))
	}
}

func TestDocumentIndexer_ForceOCRBypassesThePageCap(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	raw, err := os.ReadFile(testdataPath("scanned_two_page.pdf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	sha := writeTestDocumentBytes(t, dir, raw)
	doc, err := store.Insert(document{SHA256: sha, Filename: "scan.pdf", MIME: "application/pdf", SizeBytes: int64(len(raw)), Enrich: true, ForceOCR: true})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	fixture, err := os.ReadFile("testdata/openrouter_document_pdf_2p.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, string(fixture))}}
	idx := newTestDocumentIndexer(t, store, dir)
	idx.doer = doer
	idx.ocrPageCap = 1 // would reject without force_ocr

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	if len(doer.requests) != 1 {
		t.Fatalf("expected force_ocr to bypass the cap and place one HTTP call, got %d", len(doer.requests))
	}
	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "indexed" || got.IndexedWith != "mate" {
		t.Fatalf("expected indexed/mate once force_ocr bypasses the cap, got %+v", got)
	}
}

// TestDocumentIndexer_ByteCapExceededFailsWithoutAnHTTPCall pins the review
// finding at documents_enrich.go:377: buildScannedPDFRequest used to
// os.ReadFile and base64-encode the whole scanned PDF with no byte limit,
// unlike buildImageRequest's own documentsImageMaxBytes check - a large
// scanned PDF, still comfortably under the page cap, could peak at roughly
// 3x its own size in heap (ReadFile + base64 + json.Marshal's own copy),
// which is an OOM on the armv7 build target. idx.ocrByteCap is set below
// the fixture's real size so the cap trips well before any file is read or
// any HTTP call is placed.
func TestDocumentIndexer_ByteCapExceededFailsWithoutAnHTTPCall(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	doc := seedTestDocumentFile(t, store, dir, testdataPath("scanned_two_page.pdf"), "scan.pdf", "application/pdf", true)

	doer := &fakeOpenRouterDoer{}
	idx := newTestDocumentIndexer(t, store, dir)
	idx.doer = doer
	idx.ocrByteCap = 10 // scanned_two_page.pdf is far larger than this

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("expected failed status, got %q", got.Status)
	}
	if !strings.Contains(got.Error, fmt.Sprintf("%d bytes", doc.SizeBytes)) {
		t.Fatalf("expected the document's own byte count in the error, got %q", got.Error)
	}
	if !strings.Contains(got.Error, "10 byte OCR cap") {
		t.Fatalf("expected the configured byte cap in the error, got %q", got.Error)
	}
	if len(doer.requests) != 0 {
		t.Fatalf("expected no HTTP call once the byte cap rejects the document, got %d", len(doer.requests))
	}
}

// TestDocumentIndexer_ForceOCRDoesNotBypassTheByteCap pins the deliberate
// design decision (documentsOCRPDFMaxBytes's own doc comment,
// documents_enrich.go): unlike the page cap, force_ocr is consent to spend
// more money on an OCR call that will complete, not permission to crash the
// process trying to send a file the process cannot safely hold in memory.
// So, unlike TestDocumentIndexer_ForceOCRBypassesThePageCap above, a
// ForceOCR document must still fail the byte cap.
func TestDocumentIndexer_ForceOCRDoesNotBypassTheByteCap(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	raw, err := os.ReadFile(testdataPath("scanned_two_page.pdf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	sha := writeTestDocumentBytes(t, dir, raw)
	doc, err := store.Insert(document{SHA256: sha, Filename: "scan.pdf", MIME: "application/pdf", SizeBytes: int64(len(raw)), Enrich: true, ForceOCR: true})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	doer := &fakeOpenRouterDoer{}
	idx := newTestDocumentIndexer(t, store, dir)
	idx.doer = doer
	idx.ocrByteCap = 10 // far smaller than the fixture; force_ocr must not bypass this

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("expected force_ocr to still fail the byte cap, got status %q", got.Status)
	}
	if !strings.Contains(got.Error, "byte OCR cap") {
		t.Fatalf("expected the byte cap message in the error, got %q", got.Error)
	}
	if len(doer.requests) != 0 {
		t.Fatalf("expected force_ocr to not bypass the byte cap and place no HTTP call, got %d", len(doer.requests))
	}
}

// TestDocumentIndexer_ForceOCRSelectsOCRPathEvenWithAdequateLocalText pins
// the review finding: MarkReindex sets force_ocr=1 specifically so a
// reindex retries OCR even when the original local text looked adequate
// enough not to need it - but buildEnrichRequest picked the OCR branch from
// documentNeedsOCR(doc) alone, so ForceOCR only ever bypassed the page cap
// (TestDocumentIndexer_ForceOCRBypassesThePageCap above) and never actually
// routed a text-rich PDF through OCR. two_page.pdf is the same fixture
// TestExtractDocumentText_PDFYieldsWordsPerPage pins as NOT needing OCR, so
// this is exactly the "local text looked adequate" case force_ocr exists
// for.
func TestDocumentIndexer_ForceOCRSelectsOCRPathEvenWithAdequateLocalText(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	raw, err := os.ReadFile(testdataPath("two_page.pdf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	sha := writeTestDocumentBytes(t, dir, raw)
	doc, err := store.Insert(document{SHA256: sha, Filename: "manual.pdf", MIME: "application/pdf", SizeBytes: int64(len(raw)), Enrich: true, ForceOCR: true})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	fixture, err := os.ReadFile("testdata/openrouter_document_pdf_2p.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, string(fixture))}}
	idx := newTestDocumentIndexer(t, store, dir)
	idx.doer = doer

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	if len(doer.requests) != 1 {
		t.Fatalf("expected exactly one HTTP call, got %d", len(doer.requests))
	}
	body := decodeEnrichRequest(t, doer.bodies[0])
	if len(body.Plugins) == 0 {
		t.Fatalf("expected force_ocr to select the file-parser OCR plugin even though local text looked adequate, got no plugins on the request")
	}

	chunks, err := store.ChunksFrom(doc.ID, 1)
	if err != nil {
		t.Fatalf("ChunksFrom: %v", err)
	}
	sawOCR := false
	for _, c := range chunks {
		if c.Source == "ocr" {
			sawOCR = true
		}
	}
	if !sawOCR {
		t.Fatalf("expected an ocr-sourced chunk once force_ocr routes through OCR, got %+v", chunks)
	}
}

// ── operator edits are never overwritten ──────────────────────────────────

func TestDocumentIndexer_OperatorTitleAndTagsAreNotOverwritten(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	doc := seedTestDocumentFile(t, store, dir, testdataPath("receipt.png"), "receipt.png", "image/png", true)

	operatorTitle := "My Own Title"
	if err := store.UpdateMeta(doc.ID, &operatorTitle, nil, []string{"my-tag"}); err != nil {
		t.Fatalf("UpdateMeta: %v", err)
	}

	fixture, err := os.ReadFile("testdata/openrouter_document_image.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, string(fixture))}}
	idx := newTestDocumentIndexer(t, store, dir)
	idx.doer = doer

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != operatorTitle {
		t.Fatalf("expected the operator's title to survive enrichment, got %q", got.Title)
	}
	found := false
	for _, tg := range got.OperatorTags {
		if tg == "my-tag" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the operator tag to remain, got %+v", got.OperatorTags)
	}
	if len(got.SuggestedTags) == 0 {
		t.Fatalf("expected Mate's suggested tags to still be recorded, got none")
	}
}

// ── non-2xx / non-JSON replies ─────────────────────────────────────────

func TestDocumentIndexer_NonJSONReplyFails(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	doc := seedTestDocumentFile(t, store, dir, testdataPath("receipt.png"), "receipt.png", "image/png", true)

	body := `{"id":"gen-1","model":"m","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"sorry, here is your receipt info in plain English, not JSON"}}],"usage":{"cost":0.001}}`
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, body)}}
	idx := newTestDocumentIndexer(t, store, dir)
	idx.doer = doer

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("expected failed status for a non-JSON reply, got %q", got.Status)
	}
	if !strings.Contains(got.Error, "plain English") {
		t.Fatalf("expected the raw reply's start in the error, got %q", got.Error)
	}
}

func TestDocumentIndexer_NonSuccessStatusFailsWithUpstreamMessage(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	doc := seedTestDocumentFile(t, store, dir, testdataPath("receipt.png"), "receipt.png", "image/png", true)

	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(500, `{"error":{"message":"upstream boom"}}`)}}
	idx := newTestDocumentIndexer(t, store, dir)
	idx.doer = doer

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("expected failed status, got %q", got.Status)
	}
	if !strings.Contains(got.Error, "upstream boom") {
		t.Fatalf("expected the upstream error message, got %q", got.Error)
	}
}

// ── Run resumes pending documents after a restart ──────────────────────

func TestDocumentIndexer_RunProcessesAllPendingDocumentsAfterARestart(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()

	var docs []document
	for i := 0; i < 3; i++ {
		docs = append(docs, seedDistinctPendingPDF(t, store, dir, fmt.Sprintf("DOC-%d", i), "manual.pdf", false))
	}

	idx := newTestDocumentIndexer(t, store, dir)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		idx.Run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		allDone := true
		for _, d := range docs {
			got, err := store.Get(d.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.Status == "pending" {
				allDone = false
			}
		}
		if allDone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for Run to process every pending document")
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("Run did not return after ctx was cancelled")
	}

	for _, d := range docs {
		got, err := store.Get(d.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Status != "indexed" {
			t.Fatalf("expected document %s to be indexed after Run, got %q", d.ID, got.Status)
		}
	}
}

func TestDocumentIndexer_WakeIsNonBlockingEvenWhenNoOneIsReceiving(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	idx := newTestDocumentIndexer(t, store, dir)

	done := make(chan struct{})
	go func() {
		idx.Wake()
		idx.Wake()
		idx.Wake()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatalf("Wake blocked despite its buffered channel")
	}
}

// A settings file that can't be read must not send a paid call to a model
// the operator never chose.
func TestDocumentIndexer_UnreadableDocumentModelSettingFailsWithoutACall(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	doc := seedTestDocumentFile(t, store, dir, testdataPath("two_page.pdf"), "manual.pdf", "application/pdf", true)

	idx := newTestDocumentIndexer(t, store, dir)
	doer := &fakeOpenRouterDoer{}
	idx.doer = doer
	idx.documentModel = func() (string, error) {
		return "", errors.New("read settings: permission denied")
	}

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "failed" || !strings.Contains(got.Error, "permission denied") {
		t.Fatalf("expected failed with the settings error, got status %q error %q", got.Status, got.Error)
	}
	if len(doer.requests) != 0 {
		t.Fatalf("expected no OpenRouter call, got %d", len(doer.requests))
	}
}
