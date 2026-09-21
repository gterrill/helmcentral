package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"
)

// This file is E1c: the background embedding pass and the operator's
// explicit backfill, layered onto the indexer built in documents_indexer.go
// (B4) and the store/vector-math layer built in E1a/E1b
// (documents_store.go, documents_vector.go, openrouter_client.go).
//
// There is deliberately no fourth "embed" stage alongside extract/enrich on
// documents.stage: that CHECK constraint can't be extended without a table
// rebuild now that the documents schema has shipped (AGENTS.md's
// no-migration-ceremony policy - CREATE TABLE IF NOT EXISTS only, no ALTER
// TABLE), and a stage would make a crash mid-embed resume by re-running the
// PAID enrich stage rather than merely finishing the embed. Instead the
// work is found by query - a chunk with no document_chunk_embeddings row
// for the currently configured model needs embedding (PendingEmbedChunks,
// documents_store.go) - which is naturally idempotent, survives a crash
// untouched, and is automatically correct after a re-extract or a metadata
// edit (both delete and reinsert the chunk, cascading its stale vector away
// with it) without any bookkeeping of its own.

// documentEmbedReadinessProblem is documentEnrichReadinessProblem's
// (documents_enrich.go) counterpart for the embedding pass: readiness.
// Problem, when set, always wins - a chat-level problem (assistant off, no
// key, no chat model) is reported as-is, never masked by an
// embedding-specific message - and a blank assistant.embedding_model is the
// one requirement that's ours alone to check. A blank embedding model is
// not a failure: it is the operator's own choice to leave semantic search
// off and rely on FTS5 alone, so the message below reads as a setting to
// change, not an error to fix.
func documentEmbedReadinessProblem(readiness assistantReadiness) string {
	if readiness.Problem != "" {
		return readiness.Problem
	}
	if strings.TrimSpace(readiness.EmbeddingModel) == "" {
		return "No embedding model is configured. Set one in Settings → Assistant."
	}
	return ""
}

// errDocumentBackfillAlreadyRunning is StartBackfill's sentinel for "a
// backfill is already in progress" - SweepIfReady (this file) checks for it
// and logs rather than treating it as a failure, since the exact state it
// wanted (a backfill running) already holds; it exists so a caller never
// starts a second concurrent pass over the same queue.
var errDocumentBackfillAlreadyRunning = errors.New("a backfill is already running")

// documentBackfillStatus is BackfillStatus' return value, and the JSON
// shape both embeddings HTTP endpoints report it under
// (documents_handlers.go). There is no persisted counterpart on disk - see
// this file's own top-of-file comment, and StartBackfill's, for why a
// reboot mid-backfill needs no recovery step: the work is found by query,
// not tracked by a flag anywhere durable.
type documentBackfillStatus struct {
	Running bool `json:"running"`
	// ChunksEmbedded counts chunks embedded by the CURRENT (or most recent)
	// backfill run only, reset to 0 each time StartBackfill succeeds - not
	// a lifetime total across every backfill ever run.
	ChunksEmbedded int       `json:"chunks_embedded"`
	StartedAt      time.Time `json:"started_at"`
	// LastError is the most recent batch failure during this backfill run,
	// if any. It does not stop the backfill - Running stays true, and the
	// next pass retries once Run's own backoff elapses - it is purely for
	// the operator to see that something is currently going wrong.
	LastError string `json:"last_error,omitempty"`
}

// StartBackfill marks a backfill running and wakes the indexer so Run picks
// it up on its very next iteration rather than waiting for its regular
// poll. Automatic embedding only ever touches enrich=1 documents (the same
// upload-time consent that already sends their text to OpenRouter for
// OCR/summarising, so embedding it discloses nothing new); every other
// document waits for this call. Before ADR 0120 that consent was always an
// operator's explicit click (POST /api/documents/embeddings/backfill); now
// SweepIfReady (this file) is the ordinary caller, running it the moment
// Mate is ready rather than waiting to be asked - the same reasoning ADR
// 0106 already applies to an explicit Reindex, just triggered automatically
// instead of by hand. Calling this while a backfill is already running
// returns errDocumentBackfillAlreadyRunning rather than starting a second
// concurrent pass over the same queue.
func (idx *documentIndexer) StartBackfill() error {
	idx.embedMu.Lock()
	if idx.backfillRunning {
		idx.embedMu.Unlock()
		return errDocumentBackfillAlreadyRunning
	}
	idx.backfillRunning = true
	idx.backfillGen++
	idx.backfillChunksEmbedded = 0
	idx.backfillStartedAt = idx.now()
	idx.backfillLastError = ""
	idx.embedMu.Unlock()

	idx.Wake()
	return nil
}

// BackfillStatus reports the current (or most recently finished) backfill's
// progress, or the zero value (Running false, StartedAt the zero time) when
// none has run since this process started.
func (idx *documentIndexer) BackfillStatus() documentBackfillStatus {
	idx.embedMu.Lock()
	defer idx.embedMu.Unlock()
	return documentBackfillStatus{
		Running:        idx.backfillRunning,
		ChunksEmbedded: idx.backfillChunksEmbedded,
		StartedAt:      idx.backfillStartedAt,
		LastError:      idx.backfillLastError,
	}
}

// SweepIfReady is ADR 0120's automatic counterpart to the operator's old
// manual backfill clicks (the three per-feature confirm-and-send buttons
// that ADR removed from the Documents toolbar): called once at boot
// (main.go) and again every time settings are saved into a working
// configuration (updateSettingsHandler, signalk.go), it checks whether
// Mate is ready and, if so, clears the enrich=0 backlog without the
// operator asking again. Turning Mate on IS the consent (ADR 0120); this
// method is what "the system just does it" means in code.
//
// The two halves are gated independently, mirroring documentEnrichReadinessProblem
// and documentEmbedReadinessProblem's own split everywhere else in this
// package: enrichment (EnrichBackfillNotes, which only needs a document
// model) can run while embedding (StartBackfill, which also needs an
// embedding model) stays off - e.g. an operator who has Mate summarising
// notes but semantic search still switched off.
//
// A readiness-check error (a broken settings file, a secrets-store read
// failure) is logged and this pass sweeps nothing - the same "log and
// leave it to the next pass" treatment idx.Run's own readiness checks
// already give this exact failure elsewhere in this file, rather than
// crashing the boot or failing the settings save that triggered the call.
// Nothing is written before the check fails, so there is nothing to roll
// back; the next boot or the next settings save tries again.
func (idx *documentIndexer) SweepIfReady() {
	readiness, _, err := idx.readiness()
	if err != nil {
		log.Printf("documents: ready sweep: check assistant readiness: %v", err)
		return
	}

	if documentEnrichReadinessProblem(readiness) == "" {
		n, err := idx.store.EnrichBackfillNotes()
		if err != nil {
			log.Printf("documents: ready sweep: enrich backfill notes: %v", err)
		} else if n > 0 {
			log.Printf("documents: ready sweep: queued %d note(s) for enrichment", n)
		}
	}

	if documentEmbedReadinessProblem(readiness) == "" {
		if err := idx.StartBackfill(); err != nil && !errors.Is(err, errDocumentBackfillAlreadyRunning) {
			log.Printf("documents: ready sweep: start embeddings backfill: %v", err)
		}
	}

	// Wakes the extract/enrich stage for whatever EnrichBackfillNotes just
	// queued even when the embed half above did nothing (documentEmbedReadinessProblem
	// non-empty, so StartBackfill's own Wake never ran) - the same
	// "flip the rows, then wake the pass that processes them" split
	// wakeDocumentIndexer draws for every other write path in this package.
	idx.Wake()
}

// currentBackfillGen reports the generation StartBackfill is currently on.
// processEmbedBatch captures this alongside its backfillRunning snapshot and
// hands it back to finishBackfill, so a pass can only ever finish the
// backfill it actually belongs to - the same fencing-token idea reindex_seq
// already uses to keep a slow indexing pass from completing on top of a
// reindex that landed while it was working.
func (idx *documentIndexer) currentBackfillGen() int {
	idx.embedMu.Lock()
	defer idx.embedMu.Unlock()
	return idx.backfillGen
}

// finishBackfill clears Running, but only if gen is still the generation the
// calling pass started on. Without that check, a StartBackfill landing in the
// window between processEmbedBatch's snapshot and its several store calls
// later would be cleared by the older pass and report as instantly complete,
// having embedded nothing.
//
// It is called from processEmbedBatch on the two ways a backfill ends: the
// pending queue coming back empty, which IS completion since there is nothing
// else left to find by query, and semantic search going away mid-run, which
// it cannot continue past.
func (idx *documentIndexer) finishBackfill(gen int) {
	idx.embedMu.Lock()
	if idx.backfillGen == gen {
		idx.backfillRunning = false
	}
	idx.embedMu.Unlock()
}

// stopBackfill ends the current backfill (generation-fenced, as above) with a
// reason the operator can read - the "semantic search went away mid-run" case,
// where continuing is impossible rather than merely unfinished.
func (idx *documentIndexer) stopBackfill(gen int, reason string) {
	idx.embedMu.Lock()
	if idx.backfillGen == gen {
		idx.backfillRunning = false
		idx.backfillLastError = reason
	}
	idx.embedMu.Unlock()
}

// recordBackfillError notes a batch failure in LastError without stopping
// the backfill: Running stays true, and the next pass retries once Run's
// own documentsIndexerErrorBackoff wait elapses.
func (idx *documentIndexer) recordBackfillError(err error) {
	idx.embedMu.Lock()
	idx.backfillLastError = err.Error()
	idx.embedMu.Unlock()
}

// recordBackfillProgress adds n to this run's ChunksEmbedded count and
// clears any previous LastError - a batch that just succeeded means
// whatever failed last time is no longer the backfill's current state.
func (idx *documentIndexer) recordBackfillProgress(n int) {
	idx.embedMu.Lock()
	idx.backfillChunksEmbedded += n
	idx.backfillLastError = ""
	idx.embedMu.Unlock()
}

// documentsEmbedChunkAttempts is how many times one chunk, alone in its own
// batch, is offered to the upstream before it is set aside. Three is enough
// to ride out a transient failure that happens to land on a single-chunk
// batch, and few enough that a genuinely unacceptable chunk stops holding
// the queue within a few minutes of Run's backoff.
const documentsEmbedChunkAttempts = 3

// currentEmbedBatchLimit is how many chunks the next batch may carry. It
// starts at the full batch and halves on every failure, so a batch the
// upstream rejects splits down toward the single chunk actually responsible
// rather than failing wholesale forever. A success restores it.
func (idx *documentIndexer) currentEmbedBatchLimit() int {
	idx.embedMu.Lock()
	defer idx.embedMu.Unlock()
	if idx.embedBatchLimit <= 0 {
		idx.embedBatchLimit = maxOpenRouterEmbeddingsBatch
	}
	return idx.embedBatchLimit
}

// recordEmbedBatchFailure narrows the next batch and counts the attempt
// against each chunk in this one. Once a chunk has been tried alone
// documentsEmbedChunkAttempts times it is set aside: skipped by every later
// pass, logged once, so one chunk the upstream will never accept cannot stop
// the rest of the library from ever being embedded. The skip list is memory
// only, deliberately - a restart gives every set-aside chunk another chance,
// which is the right behaviour when the cause was the provider having a bad
// day rather than the chunk itself.
func (idx *documentIndexer) recordEmbedBatchFailure(chunks []documentChunk) {
	idx.embedMu.Lock()
	defer idx.embedMu.Unlock()

	if idx.embedBatchLimit <= 0 {
		idx.embedBatchLimit = maxOpenRouterEmbeddingsBatch
	}
	if len(chunks) > 1 {
		// Narrow first. Only once a chunk has failed on its own is the chunk
		// itself the thing to blame.
		idx.embedBatchLimit = len(chunks) / 2
		return
	}

	// A single-chunk batch failed. The limit stays where it is for now:
	// springing back to the full batch here would restart the whole 64-to-1
	// narrowing search on every one of this chunk's attempts, paying six
	// failed upstream calls and six minutes of backoff each time round, with
	// the rest of the queue stopped behind it. It goes back up below, once
	// the chunk is actually set aside and the batch size is no longer what
	// is being blamed.
	setAside := false

	if idx.embedFailures == nil {
		idx.embedFailures = map[int64]int{}
	}
	for _, c := range chunks {
		idx.embedFailures[c.ID]++
		if idx.embedFailures[c.ID] < documentsEmbedChunkAttempts {
			continue
		}
		if idx.embedSkip == nil {
			idx.embedSkip = map[int64]bool{}
		}
		if !idx.embedSkip[c.ID] {
			idx.embedSkip[c.ID] = true
			log.Printf("documents: indexer: chunk %d of document %s failed to embed %d times; setting it aside until the next restart",
				c.ID, c.DocumentID, documentsEmbedChunkAttempts)
		}
		delete(idx.embedFailures, c.ID)
		setAside = true
	}
	if setAside {
		// Back to a full batch: the blame now sits with the skipped chunk,
		// not with the batch size.
		idx.embedBatchLimit = maxOpenRouterEmbeddingsBatch
	}
}

// embedFailureCount reports how many consecutive failures are currently
// recorded against a chunk. Test-facing: the counter is otherwise only ever
// read by recordEmbedBatchFailure itself.
func (idx *documentIndexer) embedFailureCount(chunkID int64) int {
	idx.embedMu.Lock()
	defer idx.embedMu.Unlock()
	return idx.embedFailures[chunkID]
}

// recordEmbedBatchSuccess restores the full batch size and clears the failure
// counts of the chunks that just went through. The counts are consecutive
// failures, not lifetime ones: a chunk that failed twice during an outage and
// then embedded cleanly has to start again from zero, or an unrelated failure
// much later sets it aside on what only looks like a third strike. Clearing
// here is also the only thing that ever removes an entry from the map on a
// healthy system, so it is what keeps it from growing without bound.
func (idx *documentIndexer) recordEmbedBatchSuccess(chunks []documentChunk) {
	idx.embedMu.Lock()
	idx.embedBatchLimit = maxOpenRouterEmbeddingsBatch
	for _, c := range chunks {
		delete(idx.embedFailures, c.ID)
	}
	idx.embedMu.Unlock()
}

// skippedEmbedCount is how many chunks are currently set aside, so
// processEmbedBatch can ask the store for that many extra rows and still fill
// a whole batch with chunks it will actually send.
func (idx *documentIndexer) skippedEmbedCount() int {
	idx.embedMu.Lock()
	defer idx.embedMu.Unlock()
	return len(idx.embedSkip)
}

// dropSkippedChunks filters out the chunks set aside by recordEmbedBatchFailure.
func (idx *documentIndexer) dropSkippedChunks(chunks []documentChunk) []documentChunk {
	idx.embedMu.Lock()
	defer idx.embedMu.Unlock()
	if len(idx.embedSkip) == 0 {
		return chunks
	}
	out := chunks[:0]
	for _, c := range chunks {
		if !idx.embedSkip[c.ID] {
			out = append(out, c)
		}
	}
	return out
}

// processEmbedBatch is Run's second-tier queue, behind processOne
// (documents_indexer.go): one batch of up to maxOpenRouterEmbeddingsBatch
// chunks (openrouter_client.go), embedded and written in a single
// OpenRouter call. Returns "did work" the same way processOne does, so Run
// can treat the two tiers identically.
//
// A readiness ERROR (a broken settings file, a secrets-store read failure)
// is returned as an error so Run's existing backoff applies - the same
// contract runEnrichStage's own readiness check has (documents_enrich.go).
// A readiness Problem, or a blank embedding model, means there is simply no
// work to do: (false, nil), silently. That is not a masked failure -
// semantic search being off is a configuration state the operator chose,
// and E1d's search API is what reports that per query, not this loop.
//
// An upstream embeddings failure is also returned as an error, so Run backs
// off a minute (documentsIndexerErrorBackoff) rather than hammering
// OpenRouter - logged once with the batch's chunk count - but unlike a
// failed extract or enrich call, it never marks any document failed. A
// document whose text is already indexed in FTS5 is not broken because its
// vectors are late; marking it failed would hide a perfectly good
// keyword-searchable document behind a red badge over what is, at worst, a
// temporary or configuration problem between here and OpenRouter.
func (idx *documentIndexer) processEmbedBatch(ctx context.Context) (bool, error) {
	idx.embedMu.Lock()
	backfilling := idx.backfillRunning
	gen := idx.backfillGen
	idx.embedMu.Unlock()

	readiness, apiKey, err := idx.readiness()
	if err != nil {
		return false, fmt.Errorf("documents indexer: embed readiness check: %w", err)
	}
	if problem := documentEmbedReadinessProblem(readiness); problem != "" {
		// Read the backfill BEFORE returning: Mate switched off, or its key
		// removed, while a backfill is running leaves that backfill unable to
		// continue, and a pass that returned here without saying so would
		// leave Running true forever - a spinner that never stops and a
		// button that stays disabled until the process restarts.
		if backfilling {
			idx.stopBackfill(gen, problem)
		}
		return false, nil
	}
	model := readiness.EmbeddingModel
	dims := readiness.EmbeddingDimensions

	// Automatic embedding only ever looks at enrich=1 documents
	// (enrichedOnly=true); a running backfill widens the scan to every
	// document (enrichedOnly=false) - StartBackfill's own call is the
	// operator's consent for the rest of the library's text to reach
	// OpenRouter too.
	// Ask for the skipped chunks' worth of extra rows on top of the batch, so
	// chunks set aside by embedSkip (below) cost the batch nothing rather
	// than eating slots in it.
	chunks, err := idx.store.PendingEmbedChunks(model, dims, maxOpenRouterEmbeddingsBatch+idx.skippedEmbedCount(), !backfilling)
	if err != nil {
		return false, fmt.Errorf("documents indexer: pending embed chunks: %w", err)
	}
	chunks = idx.dropSkippedChunks(chunks)
	if len(chunks) == 0 {
		if backfilling {
			// enrichedOnly=false came back empty: nothing left anywhere for
			// this model. The backfill is done.
			idx.finishBackfill(gen)
		}
		return false, nil
	}
	// Narrowed after a failure (see recordEmbedBatchFailure), so a batch that
	// keeps failing splits down until whichever chunk the upstream will not
	// accept is on its own and can be set aside.
	if limit := idx.currentEmbedBatchLimit(); len(chunks) > limit {
		chunks = chunks[:limit]
	}

	inputs := make([]string, len(chunks))
	for i, c := range chunks {
		inputs[i] = c.Text
	}

	embedCtx, cancel := context.WithTimeout(ctx, idx.embedTimeout)
	defer cancel()
	resp, err := openRouterEmbeddings(embedCtx, idx.doer, apiKey, openRouterEmbeddingsRequest{
		Model:      model,
		Input:      inputs,
		Dimensions: dims,
	})
	if err != nil {
		log.Printf("documents: indexer: embed batch of %d chunk(s) failed: %v", len(chunks), err)
		idx.recordEmbedBatchFailure(chunks)
		if backfilling {
			idx.recordBackfillError(err)
		}
		return false, fmt.Errorf("documents indexer: embed batch: %w", err)
	}

	// openRouterEmbeddings guarantees resp.Data holds exactly len(inputs)
	// vectors, each placed at its own input's position - so resp.Data[i] is
	// chunks[i]'s vector, the same order the request was built in above.
	vectors := make(map[int64][]float32, len(chunks))
	for i, c := range chunks {
		vectors[c.ID] = resp.Data[i].Embedding
	}
	if err := idx.store.SetChunkEmbeddings(model, dims, vectors); err != nil {
		// Also a per-batch failure, and one that can genuinely be about a
		// single chunk: normaliseEmbedding rejects a zero or non-finite
		// vector, which one bad input can produce on its own.
		idx.recordEmbedBatchFailure(chunks)
		if backfilling {
			idx.recordBackfillError(err)
		}
		return false, fmt.Errorf("documents indexer: set chunk embeddings: %w", err)
	}

	idx.recordEmbedBatchSuccess(chunks)
	idx.splitEmbedCost(chunks, resp.Usage.Cost)

	if backfilling {
		idx.recordBackfillProgress(len(chunks))
	}
	return true, nil
}

// splitEmbedCost divides cost across the distinct documents chunks came
// from, in proportion to how many characters each contributed to the
// batch - one embeddings call can span several documents at once (this is
// a batch of CHUNKS, not a batch of documents), and OpenRouter bills the
// whole call as a single usage.cost, not per input, so there is no other
// figure to attribute per document. Character counts, not token counts:
// the same rune-counting convention EmbeddingCounts.CharsPending already
// uses (documents_store.go), so a document's share here is comparable to
// what the operator sees pending before a backfill runs. A cost of zero
// (a free/local model, or OpenRouter momentarily not reporting one) writes
// nothing, rather than recording a run of zero-cost rows nobody needs to
// see. Each document's share is added with AddEmbedCost, never
// AddIndexCost: this is a different model doing a different job than
// whatever produced index_model (the enrich stage's own OCR/summarise
// call), and must never overwrite that name with the embedding model's.
func (idx *documentIndexer) splitEmbedCost(chunks []documentChunk, cost float64) {
	if cost <= 0 {
		return
	}

	charsByDoc := make(map[string]int, len(chunks))
	total := 0
	for _, c := range chunks {
		n := utf8.RuneCountInString(c.Text)
		charsByDoc[c.DocumentID] += n
		total += n
	}
	if total == 0 {
		// PendingEmbedChunks already excludes blank-text chunks, so this
		// shouldn't be reachable; skip rather than divide by zero if it
		// somehow is.
		return
	}

	for docID, chars := range charsByDoc {
		share := cost * float64(chars) / float64(total)
		if err := idx.store.AddEmbedCost(docID, share); err != nil {
			log.Printf("documents: indexer: add embed cost %s: %v", docID, err)
		}
	}
}
