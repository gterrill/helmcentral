package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
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

// documentsDefaultEmbedTimeout bounds one E1c embeddings-API call: a batch
// of up to maxOpenRouterEmbeddingsBatch chunk texts embedded in a single
// request (openrouter_client.go). Plain vector math on OpenRouter's side,
// nothing like OCR's multi-minute scan of a whole scanned PDF, so this
// stays as short as the enrich stage's own plain chat-completion timeout
// (documentsDefaultEnrichTimeout above) rather than needing OCR's much
// longer one.
const documentsDefaultEmbedTimeout = 2 * time.Minute

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
	// embedTimeout bounds one processEmbedBatch call's OpenRouter embeddings
	// request (documents_embed.go, E1c). A struct field initialised from
	// documentsDefaultEmbedTimeout, the same shape as the three timeouts
	// above, so a test can shrink it directly on the returned pointer
	// instead of waiting out the real 2 minutes.
	embedTimeout time.Duration

	now func() time.Time

	// embedMu guards the backfill fields below - the indexer's only mutex.
	// It exists because StartBackfill can be called from an HTTP handler's
	// own goroutine (documentsEmbeddingsBackfillHandler,
	// documents_handlers.go) while Run's goroutine is concurrently reading
	// and clearing these same fields inside processEmbedBatch
	// (documents_embed.go); every other field on this struct is either
	// read-only after construction or touched only from Run's single
	// goroutine, so it has never needed one before now.
	embedMu sync.Mutex
	// backfillRunning is true from StartBackfill until processEmbedBatch
	// finds PendingEmbedChunks(model, _, enrichedOnly=false) empty - that
	// emptiness IS completion. There is deliberately no persisted
	// counterpart on disk: a reboot mid-backfill simply stops it (this
	// field, like the rest of the process's memory, is gone), and pressing
	// the backfill button again resumes exactly where it left off, because
	// the work itself is found by query (PendingEmbedChunks) rather than
	// tracked by a flag anywhere durable. See documents_embed.go's
	// StartBackfill/BackfillStatus doc comments.
	backfillRunning bool
	// backfillGen is bumped by every StartBackfill. processEmbedBatch
	// captures it alongside its backfillRunning snapshot so it can only ever
	// finish the backfill it belongs to - see finishBackfill
	// (documents_embed.go) for the window that would otherwise let an older
	// pass cancel a newer backfill.
	backfillGen int
	// backfillChunksEmbedded counts chunks embedded by THIS backfill run
	// only (reset to 0 by StartBackfill) - not a lifetime total.
	backfillChunksEmbedded int
	backfillStartedAt      time.Time
	// backfillLastError is the most recent batch failure during a backfill,
	// if any - recorded but not fatal to the backfill itself (see
	// processEmbedBatch's own doc comment).
	backfillLastError string
	// embedBatchLimit, embedFailures and embedSkip are the embed pass's
	// poison-pill handling (documents_embed.go): the batch narrows on a
	// failure until one chunk is alone, and a chunk that keeps failing alone
	// is set aside so it stops holding the rest of the queue. All three are
	// memory only - a restart gives every set-aside chunk another chance.
	embedBatchLimit int
	embedFailures   map[int64]int
	embedSkip       map[int64]bool

	// extract is extractDocumentText by default. Tests substitute a func
	// that blocks or errors for one document's path, to exercise the
	// extract-stage timeout without a genuinely pathological PDF.
	extract func(ctx context.Context, path, mimeType string) (extractedDocument, error)

	// ocrPageCap is documentsDefaultOCRPageCap by default (documents_enrich.go);
	// tests lower it so the page-cap failure path is reachable with a small
	// fixture instead of a real 201-page scan.
	ocrPageCap int

	// ocrByteCap is documentsOCRPDFMaxBytes by default; tests lower it so
	// the byte-cap failure path is reachable with a small fixture instead of
	// a genuine 40 MB+ file.
	ocrByteCap int64
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
		embedTimeout:   documentsDefaultEmbedTimeout,
		now:            func() time.Time { return time.Now().UTC() },
		extract:        extractDocumentText,
		ocrPageCap:     documentsDefaultOCRPageCap,
		ocrByteCap:     documentsOCRPDFMaxBytes,
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

// Run loops two tiers of work until ctx is done. The first tier,
// processOne, pops the oldest pending document and processes it fully
// (extract, then enrich if consent was given). The second tier,
// processEmbedBatch (documents_embed.go, E1c), embeds one batch of chunks -
// either the automatic pass over already-enriched documents, or an
// operator-started backfill over everything else. Each iteration tries
// processOne first and only reaches for processEmbedBatch when there was no
// pending document to index; Run repeats immediately while either tier
// reports work done, and otherwise blocks on wake or ctx.Done(). Indexing
// always wins the race for a reason, not just convention: a document nobody
// can find by keyword yet (still pending/failed) is a worse state to leave
// sitting than one that is merely not yet searchable by meaning, so a large
// backfill can never starve a fresh upload of its own first pass. Started
// once at boot (main.go) so any document left pending by a crash or restart
// resumes without operator action, and so does an interrupted backfill (see
// documents_embed.go's StartBackfill/BackfillStatus doc comments for why
// that needs no recovery step of its own).
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
			if !idx.waitForWakeOrBackoff(ctx) {
				return
			}
			continue
		}
		if processed {
			continue
		}

		embedded, err := idx.processEmbedBatch(ctx)
		if err != nil {
			// Same backoff as above - an upstream embeddings failure (or a
			// store error reading/writing the embed queue) must not spin
			// the loop, exactly as processOne's own error doesn't.
			log.Printf("documents: indexer: %v", err)
			if !idx.waitForWakeOrBackoff(ctx) {
				return
			}
			continue
		}
		if embedded {
			continue
		}

		select {
		case <-idx.wake:
		case <-ctx.Done():
			return
		}
	}
}

// waitForWakeOrBackoff is Run's shared error-branch wait: a wake, a minute
// (documentsIndexerErrorBackoff), or ctx ending, whichever comes first.
// Returns false when ctx ended - the caller's cue to return rather than
// loop again - and true otherwise.
func (idx *documentIndexer) waitForWakeOrBackoff(ctx context.Context) bool {
	select {
	case <-idx.wake:
		return true
	case <-time.After(documentsIndexerErrorBackoff):
		return true
	case <-ctx.Done():
		return false
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
	// The reindex_seq this pass believes it's working on, captured before
	// anything below can re-read (and so silently refresh) doc from a
	// MarkReindex call that lands while this pass is running - finishIndexed
	// compares the DB's current reindex_seq against this exact value, not
	// doc.ReindexSeq's possibly-already-updated one, right before it would
	// mark the document done (review finding, documents_handlers.go:791).
	startSeq := doc.ReindexSeq

	// A pending row's stage is only ever "extract" (fresh, or a reindex -
	// MarkReindex always resets it there) or "enrich" (already through
	// extraction; SetStage below is what moves it there). Only the former
	// needs runExtractStage: a document a crash left pending/enrich already
	// has its markdown/page_count/local chunks committed (finishExtract's
	// own writes, below) - re-running extraction for it would waste up to
	// idx.extractTimeout, and would wrongly fail an already-extracted
	// document on nothing more than a transient re-extraction hiccup.
	if doc.Stage == "extract" {
		ok, err := idx.runExtractStage(ctx, doc)
		if err != nil {
			return true, err
		}
		if !ok {
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
			if err := idx.finishIndexed(doc, startSeq, "local", doc.Error); err != nil {
				return true, err
			}
			return true, nil
		}

		if err := idx.store.SetStage(doc.ID, "enrich"); err != nil {
			return true, fmt.Errorf("documents indexer: set stage enrich %s: %w", doc.ID, err)
		}
	}

	if err := idx.runEnrichStage(ctx, doc, startSeq); err != nil {
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
// Returns (false, nil) on any ordinary, already-recorded failure, (false,
// non-nil) when failDoc's own write itself failed (review finding - see
// failDoc's doc comment), and (true, nil) once the document is ready for
// its next stage.
//
// The goroutine below carries its own recover, deliberately defensive
// rather than evidence of a known gap: extractDocumentText's own extractors
// (documents_extract.go) already recover everything they know how to, but
// Go aborts the WHOLE PROCESS on an unrecovered panic from ANY goroutine,
// and Echo's middleware.Recover() (main.go) only wraps HTTP handlers - it
// cannot reach into a goroutine this package spawns itself. Without this,
// one extractor bug (present or future - any MIME, not just PDF) would kill
// navigation, alarms and the kiosk feed for the whole boat over a single
// uploaded document, and since the document is left pending, the indexer
// would pick it straight back up on the next restart and crash again: a
// persistent crash loop, not a one-off.
func (idx *documentIndexer) runExtractStage(ctx context.Context, doc document) (bool, error) {
	path := filepath.Join(idx.dir, doc.SHA256)

	type extractOutcome struct {
		ex  extractedDocument
		err error
	}
	done := make(chan extractOutcome, 1)
	extractCtx, cancel := context.WithTimeout(ctx, idx.extractTimeout)
	defer cancel()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				// Converted into the same extractOutcome shape as an
				// ordinary extraction error, so it flows through the select
				// below exactly like any other failure (failDoc, right
				// there) - the panic detail is preserved in the error text,
				// not swallowed, and lands in documents.error the same way
				// every other failure reason does.
				log.Printf("documents: indexer: extraction goroutine for %s panicked: %v", doc.ID, r)
				done <- extractOutcome{extractedDocument{}, fmt.Errorf("panic during text extraction: %v", r)}
			}
		}()
		ex, err := idx.extract(extractCtx, path, doc.MIME)
		done <- extractOutcome{ex, err}
	}()

	select {
	case out := <-done:
		if out.err != nil {
			if ferr := idx.failDoc(doc.ID, out.err.Error()); ferr != nil {
				return false, ferr
			}
			return false, nil
		}
		return idx.finishExtract(doc, out.ex)
	case <-extractCtx.Done():
		if ctx.Err() != nil {
			// The outer context is what ended, not idx.extractTimeout - a
			// shutdown, not a genuine timeout. Leave the row pending
			// (status is untouched) for the next boot to pick back up,
			// rather than mislabel it as timed out.
			return false, nil
		}
		log.Printf("documents: indexer: extraction goroutine for %s abandoned after timing out (it may still be running)", doc.ID)
		if ferr := idx.failDoc(doc.ID, fmt.Sprintf("text extraction timed out after %s", idx.extractTimeout)); ferr != nil {
			return false, ferr
		}
		return false, nil
	}
}

// finishExtract persists a successful extraction: local chunks, the
// flattened markdown/page_count, and - if some but not all pages failed -
// a warning naming how many and the first page's own error, logged and
// written to documents.error via SetWarning once the document reaches
// indexed (SetIndexed itself clears error unconditionally, so
// processOne/runEnrichStage re-apply doc.Error after it, not here). Returns
// (false, non-nil) only when failDoc's own write failed (review finding);
// (false, nil) for an ordinary, already-recorded failure; (true, nil) on
// success. The SetWarning call stays log-only, same reasoning as
// finishIndexed's own SetWarning: the primary write has already committed
// by then.
func (idx *documentIndexer) finishExtract(doc document, ex extractedDocument) (bool, error) {
	chunks := chunkDocument(ex, doc.MIME)
	if err := idx.store.ReplaceChunks(doc.ID, []string{"local"}, chunks); err != nil {
		if ferr := idx.failDoc(doc.ID, err.Error()); ferr != nil {
			return false, ferr
		}
		return false, nil
	}
	if err := idx.store.SetExtracted(doc.ID, ex.Markdown, ex.PageCount); err != nil {
		if ferr := idx.failDoc(doc.ID, err.Error()); ferr != nil {
			return false, ferr
		}
		return false, nil
	}

	if len(ex.FailedPages) > 0 {
		warning := fmt.Sprintf("%d of %d pages unreadable: %s", len(ex.FailedPages), ex.PageCount, ex.FirstPageErr)
		log.Printf("documents: indexer: %s: %s", doc.ID, warning)
		if err := idx.store.SetWarning(doc.ID, warning); err != nil {
			log.Printf("documents: indexer: set warning %s: %v", doc.ID, err)
		}
	}
	return true, nil
}

// finishIndexed marks doc indexed (indexedWith "local" or "mate") and, if
// warning is non-empty, restores it into documents.error afterward -
// SetIndexed's own unconditional error=” clear (a clean-finish guarantee
// pinned by TestDocumentStore_SetStageSetIndexedSetFailedTransitions) would
// otherwise wipe a FailedPages warning right at the moment it matters most:
// the document the operator is now looking at as "done". A SetIndexed
// failure is returned (not merely logged) so processOne can propagate it
// and Run's backoff applies, rather than looping straight back onto the
// same still-pending document (review finding, same shape as failDoc
// below). The SetWarning call stays log-only on purpose: by the time it
// runs, the primary status transition has already committed, so a lost
// warning doesn't leave the row stuck pending - it just loses a
// nice-to-have annotation on an already-finished document.
//
// expectedSeq is the reindex_seq this pass started with (processOne's
// startSeq, captured right after NextPending, before anything can refresh
// it). SetIndexedIfCurrent only applies the write while the document's
// reindex_seq still matches: a mismatch means MarkReindex ran while this
// pass was still working (documents_handlers.go:791's review finding), and
// the row is already back to pending/extract from that call - completing
// this stale pass over it would silently discard the reindex request, so
// finishIndexed leaves it alone instead. That's not an error: the fresh
// generation MarkReindex queued is exactly what should run next, and
// Run's normal loop (it keeps going while processOne reports work done)
// picks it straight back up.
func (idx *documentIndexer) finishIndexed(doc document, expectedSeq int, indexedWith, warning string) error {
	applied, err := idx.store.SetIndexedIfCurrent(doc.ID, expectedSeq, indexedWith)
	if err != nil {
		return fmt.Errorf("documents indexer: set indexed %s: %w", doc.ID, err)
	}
	if !applied {
		log.Printf("documents: indexer: %s was reindexed while this pass was still running; leaving the fresh request queued instead of marking it done", doc.ID)
		return nil
	}
	if warning != "" {
		if err := idx.store.SetWarning(doc.ID, warning); err != nil {
			log.Printf("documents: indexer: set warning %s: %v", doc.ID, err)
		}
	}
	return nil
}

// failDoc records msg as doc's failure reason. Its own SetFailed error is
// returned (not merely logged) so callers can propagate it: if SetFailed
// itself fails (a read-only or full database), the row stays pending, and
// a caller that swallowed that error would report "handled" back to
// processOne, which would report "did work" back to Run, which would loop
// straight back onto the SAME still-pending document with no backoff and
// no operator-visible failure (review finding).
func (idx *documentIndexer) failDoc(id, msg string) error {
	if err := idx.store.SetFailed(id, msg); err != nil {
		return fmt.Errorf("documents indexer: set failed %s: %w", id, err)
	}
	return nil
}
