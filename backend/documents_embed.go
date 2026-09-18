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
// backfill is already in progress" - documentsEmbeddingsBackfillHandler
// (documents_handlers.go) turns it into 409, rather than starting a second
// concurrent pass over the same queue.
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
// document waits for this call, which is itself the operator's consent for
// THAT text to reach OpenRouter too - the same reasoning ADR 0106 already
// applies to an explicit Reindex. Calling this while a backfill is already
// running returns errDocumentBackfillAlreadyRunning rather than starting a
// second concurrent pass over the same queue.
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
	}
	// Back to a full batch: the blame now sits with the skipped chunk, not
	// with the batch size.
	idx.embedBatchLimit = maxOpenRouterEmbeddingsBatch
}

// recordEmbedBatchSuccess restores the full batch size and clears the failure
// counts of the chunks that just went through.
func (idx *documentIndexer) recordEmbedBatchSuccess() {
	idx.embedMu.Lock()
	idx.embedBatchLimit = maxOpenRouterEmbeddingsBatch
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
	chunks, err := idx.store.PendingEmbedChunks(model, maxOpenRouterEmbeddingsBatch+idx.skippedEmbedCount(), !backfilling)
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

	idx.recordEmbedBatchSuccess()
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
