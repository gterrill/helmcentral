package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"time"
)

// documentsDefaultExtractTimeout bounds one document's local extraction
// (documents_extract.go). A pathological PDF - one whose parser never
// returns, rather than erroring - must not stall the whole queue behind it
// forever, so runExtractStage runs extraction in its own goroutine and
// abandons it once this elapses (the goroutine itself may keep running;
// there is no way to cancel github.com/ledongthuc/pdf mid-parse).
const documentsDefaultExtractTimeout = 2 * time.Minute

// documentsDefaultOCRTimeout bounds an enrich-stage call that sends the
// whole file to the file-parser plugin (a scanned PDF). OCR is the slowest
// path by far - minutes, not seconds, for a many-page scan - so it gets its
// own, much longer timeout than a plain chat completion.
const documentsDefaultOCRTimeout = 10 * time.Minute

// documentsDefaultEnrichTimeout bounds every other enrich-stage call (text
// or image, no OCR fee): an ordinary chat completion, not a slow OCR job.
const documentsDefaultEnrichTimeout = 2 * time.Minute

// documentsIndexerErrorBackoff is how long Run waits after a store error
// before looking at the queue again.
const documentsIndexerErrorBackoff = time.Minute

// documentIndexer is the background worker behind B4: the database is the
// queue (documents.status='pending'), one document processed at a time,
// looping between local extraction (always) and Mate's paid enrichment
// (only when the document's own enrich flag - consent captured at upload
// or reindex time - is set). See documents_enrich.go for the enrich
// stage itself.
type documentIndexer struct {
	store *documentStore
	// dir is the documents directory (documentsDirPath()): files are named
	// by their sha256, so a document's bytes live at filepath.Join(dir,
	// doc.SHA256).
	dir string
	// readiness mirrors checkAssistantReadiness's signature exactly, so
	// production code just closes over assistantSettingsPath() and tests
	// substitute a canned answer without a real settings file.
	readiness func() (assistantReadiness, string, error)
	doer      openRouterDoer
	// documentModel returns the configured assistant.document_model,
	// re-read on every call (not cached at construction) so a settings
	// change takes effect on the next document without a restart. An error
	// fails the document: a paid call never goes to a model nobody chose.
	documentModel func() (string, error)

	// wake is buffered 1: Wake() never blocks a caller (an upload or
	// reindex handler) regardless of whether Run is mid-document or idle
	// waiting on this channel.
	wake chan struct{}

	extractTimeout time.Duration
	ocrTimeout     time.Duration
	enrichTimeout  time.Duration

	now func() time.Time

	// extract is extractDocumentText by default. Tests substitute a func
	// that blocks or errors for one document's path, to exercise the
	// extract-stage timeout without a genuinely pathological PDF.
	extract func(ctx context.Context, path, mimeType string) (extractedDocument, error)

	// ocrPageCap is documentsDefaultOCRPageCap by default (documents_enrich.go);
	// tests lower it so the page-cap failure path is reachable with a small
	// fixture instead of a real 201-page scan.
	ocrPageCap int
}

// newDocumentIndexer builds a documentIndexer with the production timeouts,
// OCR page cap and extraction func. main.go supplies store/dir/readiness/
// doer/documentModel; every other field is a sensible default a caller
// rarely needs to override (tests do, directly on the returned pointer).
func newDocumentIndexer(
	store *documentStore,
	dir string,
	readiness func() (assistantReadiness, string, error),
	doer openRouterDoer,
	documentModel func() (string, error),
) *documentIndexer {
	return &documentIndexer{
		store:          store,
		dir:            dir,
		readiness:      readiness,
		doer:           doer,
		documentModel:  documentModel,
		wake:           make(chan struct{}, 1),
		extractTimeout: documentsDefaultExtractTimeout,
		ocrTimeout:     documentsDefaultOCRTimeout,
		enrichTimeout:  documentsDefaultEnrichTimeout,
		now:            func() time.Time { return time.Now().UTC() },
		extract:        extractDocumentText,
		ocrPageCap:     documentsDefaultOCRPageCap,
	}
}

// Wake nudges Run to look at the queue immediately rather than waiting for
// its next iteration - a non-blocking send, since Run may be mid-document
// (not yet reading wake) or the channel may already hold a pending wake
// from an earlier, not-yet-serviced call.
func (idx *documentIndexer) Wake() {
	select {
	case idx.wake <- struct{}{}:
	default:
	}
}

// Run loops processOne until ctx is done: pop the oldest pending document,
// process it fully (extract, then enrich if consent was given), repeat
// immediately while there is more pending work, and otherwise block on
// wake or ctx.Done(). Started once at boot (main.go) so any document left
// pending by a crash or restart resumes without operator action.
func (idx *documentIndexer) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}

		processed, err := idx.processOne(ctx)
		if err != nil {
			// A store error can leave the same document pending, so retrying
			// at once would spin and flood the log. Wait for a wake or a minute.
			log.Printf("documents: indexer: %v", err)
			select {
			case <-idx.wake:
			case <-time.After(documentsIndexerErrorBackoff):
			case <-ctx.Done():
				return
			}
			continue
		}
		if processed {
			continue
		}

		select {
		case <-idx.wake:
		case <-ctx.Done():
			return
		}
	}
}

// processOne pops the single oldest pending document (documentStore.NextPending)
// and runs it through whichever stage it's at, returning ok=false when
// there was nothing pending. Exported to tests (lowercase is fine) so they
// can drive the indexer synchronously, one document at a time, without a
// goroutine or a real Wake - Run above is just this in a loop.
func (idx *documentIndexer) processOne(ctx context.Context) (bool, error) {
	doc, ok, err := idx.store.NextPending()
	if err != nil {
		return false, fmt.Errorf("documents indexer: next pending: %w", err)
	}
	if !ok {
		return false, nil
	}

	// A pending row's stage is only ever "extract" (fresh, or a reindex -
	// MarkReindex always resets it there) or "enrich" (already through
	// extraction; SetStage below is what moves it there). Only the former
	// needs runExtractStage: a document a crash left pending/enrich already
	// has its markdown/page_count/local chunks committed (finishExtract's
	// own writes, below) - re-running extraction for it would waste up to
	// idx.extractTimeout, and would wrongly fail an already-extracted
	// document on nothing more than a transient re-extraction hiccup.
	if doc.Stage == "extract" {
		if !idx.runExtractStage(ctx, doc) {
			return true, nil
		}

		// finishExtract (called from runExtractStage) may have written a
		// FailedPages warning and always writes markdown/page_count - re-read
		// so the rest of this document's processing (and, if it goes on to
		// enrich, documents_enrich.go) sees that instead of the stale copy
		// NextPending returned before extraction ran.
		updated, err := idx.store.Get(doc.ID)
		if err != nil {
			return true, fmt.Errorf("documents indexer: re-read %s after extract: %w", doc.ID, err)
		}
		doc = updated

		if !doc.Enrich {
			idx.finishIndexed(doc, "local", doc.Error)
			return true, nil
		}

		if err := idx.store.SetStage(doc.ID, "enrich"); err != nil {
			return true, fmt.Errorf("documents indexer: set stage enrich %s: %w", doc.ID, err)
		}
	}

	if err := idx.runEnrichStage(ctx, doc); err != nil {
		return true, err
	}
	return true, nil
}

// runExtractStage runs idx.extract in its own goroutine bounded by
// idx.extractTimeout, so a pathological source that never returns can't
// stall the queue: the goroutine is abandoned (it may still be running,
// with nothing left reading its result) once the timeout elapses, and the
// document is marked failed. On success, chunkDocument/ReplaceChunks/
// SetExtracted persist the result, and a partial page failure
// (extractedDocument.FailedPages) is logged and recorded as a warning that
// survives onto the finished document rather than being silently dropped.
// Returns false on any failure (already recorded via SetFailed), true once
// the document is ready for its next stage.
func (idx *documentIndexer) runExtractStage(ctx context.Context, doc document) bool {
	path := filepath.Join(idx.dir, doc.SHA256)

	type extractOutcome struct {
		ex  extractedDocument
		err error
	}
	done := make(chan extractOutcome, 1)
	extractCtx, cancel := context.WithTimeout(ctx, idx.extractTimeout)
	defer cancel()

	go func() {
		ex, err := idx.extract(extractCtx, path, doc.MIME)
		done <- extractOutcome{ex, err}
	}()

	select {
	case out := <-done:
		if out.err != nil {
			idx.failDoc(doc.ID, out.err.Error())
			return false
		}
		return idx.finishExtract(doc, out.ex)
	case <-extractCtx.Done():
		if ctx.Err() != nil {
			// The outer context is what ended, not idx.extractTimeout - a
			// shutdown, not a genuine timeout. Leave the row pending
			// (status is untouched) for the next boot to pick back up,
			// rather than mislabel it as timed out.
			return false
		}
		log.Printf("documents: indexer: extraction goroutine for %s abandoned after timing out (it may still be running)", doc.ID)
		idx.failDoc(doc.ID, fmt.Sprintf("text extraction timed out after %s", idx.extractTimeout))
		return false
	}
}

// finishExtract persists a successful extraction: local chunks, the
// flattened markdown/page_count, and - if some but not all pages failed -
// a warning naming how many and the first page's own error, logged and
// written to documents.error via SetWarning once the document reaches
// indexed (SetIndexed itself clears error unconditionally, so
// processOne/runEnrichStage re-apply doc.Error after it, not here).
func (idx *documentIndexer) finishExtract(doc document, ex extractedDocument) bool {
	chunks := chunkDocument(ex, doc.MIME)
	if err := idx.store.ReplaceChunks(doc.ID, []string{"local"}, chunks); err != nil {
		idx.failDoc(doc.ID, err.Error())
		return false
	}
	if err := idx.store.SetExtracted(doc.ID, ex.Markdown, ex.PageCount); err != nil {
		idx.failDoc(doc.ID, err.Error())
		return false
	}

	if len(ex.FailedPages) > 0 {
		warning := fmt.Sprintf("%d of %d pages unreadable: %s", len(ex.FailedPages), ex.PageCount, ex.FirstPageErr)
		log.Printf("documents: indexer: %s: %s", doc.ID, warning)
		if err := idx.store.SetWarning(doc.ID, warning); err != nil {
			log.Printf("documents: indexer: set warning %s: %v", doc.ID, err)
		}
	}
	return true
}

// finishIndexed marks doc indexed (indexedWith "local" or "mate") and, if
// warning is non-empty, restores it into documents.error afterward -
// SetIndexed's own unconditional error=” clear (a clean-finish guarantee
// pinned by TestDocumentStore_SetStageSetIndexedSetFailedTransitions) would
// otherwise wipe a FailedPages warning right at the moment it matters most:
// the document the operator is now looking at as "done".
func (idx *documentIndexer) finishIndexed(doc document, indexedWith, warning string) {
	if err := idx.store.SetIndexed(doc.ID, indexedWith); err != nil {
		log.Printf("documents: indexer: set indexed %s: %v", doc.ID, err)
		return
	}
	if warning != "" {
		if err := idx.store.SetWarning(doc.ID, warning); err != nil {
			log.Printf("documents: indexer: set warning %s: %v", doc.ID, err)
		}
	}
}

// failDoc records msg as doc's failure reason, logging (rather than losing)
// a failure to even write that - the caller has already decided the
// document failed; a broken database write on top of that is a second,
// separate problem worth its own log line rather than a silent no-op.
func (idx *documentIndexer) failDoc(id, msg string) {
	if err := idx.store.SetFailed(id, msg); err != nil {
		log.Printf("documents: indexer: set failed %s: %v", id, err)
	}
}
