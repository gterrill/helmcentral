package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"
)

// ── shared test helpers ────────────────────────────────────────────────

// embedTestReadiness returns a readiness func embedding tests use by
// default: Mate on, a document model set (unused by these tests, but
// mirrors newTestDocumentIndexer's own stub - documents_indexer_test.go),
// and the given embedding model/dimensions.
func embedTestReadiness(model string, dims int) func() (assistantReadiness, string, error) {
	return func() (assistantReadiness, string, error) {
		return assistantReadiness{
			Enabled: true, Configured: true, Model: "m",
			DocumentModel:       "google/gemini-2.5-flash",
			EmbeddingModel:      model,
			EmbeddingDimensions: dims,
		}, "sk-test", nil
	}
}

// newTestEmbedIndexer builds a documentIndexer for processEmbedBatch/
// StartBackfill tests directly (not through newTestDocumentIndexer /
// newDocumentIndexer's own default readiness, which knows nothing about
// embedding_model), with a short embedTimeout so a genuine bug can't hang
// the suite.
func newTestEmbedIndexer(store *documentStore, dir string, readiness func() (assistantReadiness, string, error), doer openRouterDoer) *documentIndexer {
	idx := newDocumentIndexer(store, dir, readiness, doer, func() (string, error) { return "", nil })
	idx.embedTimeout = documentsDefaultEmbedTimeout
	return idx
}

// insertChunkedDocument inserts a document row and its body chunks
// directly via ReplaceChunks, bypassing the extract stage entirely - these
// tests exercise processEmbedBatch on its own, not the whole extract/
// enrich/embed pipeline.
func insertChunkedDocument(t *testing.T, store *documentStore, sha, filename string, enrich bool, texts ...string) document {
	t.Helper()
	doc, err := store.Insert(document{SHA256: sha, Filename: filename, MIME: "application/pdf", Enrich: enrich})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	chunks := make([]documentChunk, len(texts))
	for i, text := range texts {
		chunks[i] = documentChunk{Seq: i + 1, Source: "local", Text: text}
	}
	if err := store.ReplaceChunks(doc.ID, []string{"local"}, chunks); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}
	return doc
}

// fakeEmbeddingsResponse builds a 200 response carrying n distinct,
// non-zero (normaliseEmbedding rejects an all-zero vector) vectors of dims
// floats each, via openRouterEmbeddingsResponse's own json tags rather than
// a hand-typed fixture string, so it can never silently drift from the
// shape openRouterEmbeddings actually decodes.
func fakeEmbeddingsResponse(t *testing.T, dims, n int, cost float64) *http.Response {
	t.Helper()
	data := make([]openRouterEmbeddingData, n)
	for i := range data {
		vec := make([]float32, dims)
		for j := range vec {
			vec[j] = float32(i + 1)
		}
		data[i] = openRouterEmbeddingData{Index: i, Embedding: vec}
	}
	body, err := json.Marshal(openRouterEmbeddingsResponse{
		Object: "list", Model: "test-embed-model", Data: data, Usage: openRouterUsage{Cost: cost},
	})
	if err != nil {
		t.Fatalf("marshal fake embeddings response: %v", err)
	}
	return openRouterFakeResponse(200, string(body))
}

// ── the batch itself ────────────────────────────────────────────────────

func TestDocumentIndexer_ProcessEmbedBatchEmbedsPendingChunksAndWritesVectors(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	doc := insertChunkedDocument(t, store, "sha-embed-basic", "manual.pdf", true,
		"Impeller replacement on the Yanmar 4JH.",
		"Whitsunday Marine Supplies receipt AUD 205.45.",
	)

	// insertChunkedDocument's store.Insert also writes the document's own
	// meta chunk (seq 0, rebuildMetaChunkTx) - PendingEmbedChunks makes no
	// source distinction, so that's a 3rd pending chunk alongside the 2
	// body ones seeded above.
	doer := &fakeOpenRouterDoer{responses: []*http.Response{fakeEmbeddingsResponse(t, 4, 3, 0)}}
	idx := newTestEmbedIndexer(store, dir, embedTestReadiness("openai/text-embedding-3-small", 4), doer)

	processed, err := idx.processEmbedBatch(context.Background())
	if err != nil {
		t.Fatalf("processEmbedBatch: %v", err)
	}
	if !processed {
		t.Fatalf("expected processEmbedBatch to report work done")
	}

	if len(doer.requests) != 1 {
		t.Fatalf("expected exactly one embeddings request, got %d", len(doer.requests))
	}
	var sent openRouterEmbeddingsRequest
	if err := json.Unmarshal(doer.bodies[0], &sent); err != nil {
		t.Fatalf("unmarshal sent request: %v", err)
	}
	if sent.Model != "openai/text-embedding-3-small" || sent.Dimensions != 4 {
		t.Fatalf("unexpected request model/dims: %+v", sent)
	}
	wantInputs := []string{"Impeller replacement on the Yanmar 4JH.", "Whitsunday Marine Supplies receipt AUD 205.45."}
	if len(sent.Input) != len(wantInputs)+1 {
		t.Fatalf("expected %d inputs (the meta chunk plus %d body chunks), got %d: %+v", len(wantInputs)+1, len(wantInputs), len(sent.Input), sent.Input)
	}
	if !strings.Contains(sent.Input[0], "manual.pdf") {
		t.Fatalf("expected the meta chunk (seq 0) to be sent first, got %q", sent.Input[0])
	}
	for i, want := range wantInputs {
		if sent.Input[i+1] != want {
			t.Fatalf("body input %d: got %q want %q", i, sent.Input[i+1], want)
		}
	}

	if _, err := store.ChunksFrom(doc.ID, 1); err != nil {
		t.Fatalf("ChunksFrom: %v", err)
	}
	pending, err := store.PendingEmbedChunks("openai/text-embedding-3-small", 10, false)
	if err != nil {
		t.Fatalf("PendingEmbedChunks: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected no chunks pending after the batch, got %+v", pending)
	}
}

func TestDocumentIndexer_ProcessEmbedBatchAutomaticPassSkipsNonConsentedDocuments(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	insertChunkedDocument(t, store, "sha-embed-plain", "plain.pdf", false, "Whitsunday Marine Supplies receipt.")

	doer := &fakeOpenRouterDoer{}
	idx := newTestEmbedIndexer(store, dir, embedTestReadiness("m", 4), doer)

	processed, err := idx.processEmbedBatch(context.Background())
	if err != nil {
		t.Fatalf("processEmbedBatch: %v", err)
	}
	if processed {
		t.Fatalf("expected nothing to embed automatically for a non-consented document")
	}
	if len(doer.requests) != 0 {
		t.Fatalf("expected no HTTP call, got %d", len(doer.requests))
	}
}

func TestDocumentIndexer_BackfillEmbedsNonConsentedDocumentsAndClearsRunningWhenDone(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	insertChunkedDocument(t, store, "sha-embed-backfill", "plain.pdf", false, "Whitsunday Marine Supplies receipt.")

	// 1 body chunk plus its meta chunk (seq 0, rebuildMetaChunkTx) - see the
	// same note in the basic embed-batch test above.
	doer := &fakeOpenRouterDoer{responses: []*http.Response{fakeEmbeddingsResponse(t, 4, 2, 0)}}
	idx := newTestEmbedIndexer(store, dir, embedTestReadiness("m", 4), doer)

	if err := idx.StartBackfill(); err != nil {
		t.Fatalf("StartBackfill: %v", err)
	}
	if !idx.BackfillStatus().Running {
		t.Fatalf("expected the backfill to be marked running")
	}

	processed, err := idx.processEmbedBatch(context.Background())
	if err != nil {
		t.Fatalf("processEmbedBatch: %v", err)
	}
	if !processed {
		t.Fatalf("expected the backfill pass to embed the pending chunks")
	}
	if len(doer.requests) != 1 {
		t.Fatalf("expected exactly one embeddings request, got %d", len(doer.requests))
	}
	status := idx.BackfillStatus()
	if !status.Running || status.ChunksEmbedded != 2 {
		t.Fatalf("expected the backfill still running with 2 chunks embedded, got %+v", status)
	}

	// The queue is now empty: the next pass must find nothing and clear
	// Running - that emptiness under enrichedOnly=false IS completion.
	processed, err = idx.processEmbedBatch(context.Background())
	if err != nil {
		t.Fatalf("processEmbedBatch (2nd): %v", err)
	}
	if processed {
		t.Fatalf("expected nothing left to embed on the second pass")
	}
	status = idx.BackfillStatus()
	if status.Running {
		t.Fatalf("expected Running to clear once the queue is empty, got %+v", status)
	}
}

func TestDocumentIndexer_ProcessEmbedBatchSplitsCostProportionallyAcrossDocuments(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	// 10 and 30 body chars - each document also carries its own meta chunk
	// (seq 0, rebuildMetaChunkTx), so the true per-document char totals
	// (computed below via ChunksFrom, the same rune-counting convention
	// splitEmbedCost itself uses) aren't quite the clean 10:30 the body
	// text alone would suggest - the point of computing rather than
	// hardcoding the expected split.
	docA := insertChunkedDocument(t, store, "sha-embed-cost-a", "a.pdf", true, strings.Repeat("a", 10))
	docB := insertChunkedDocument(t, store, "sha-embed-cost-b", "b.pdf", true, strings.Repeat("b", 30))

	charsOf := func(id string) int {
		chunks, err := store.ChunksFrom(id, 0)
		if err != nil {
			t.Fatalf("ChunksFrom: %v", err)
		}
		n := 0
		for _, c := range chunks {
			n += utf8.RuneCountInString(c.Text)
		}
		return n
	}
	charsA, charsB := charsOf(docA.ID), charsOf(docB.ID)
	total := charsA + charsB

	const cost = 0.0004
	// 2 body chunks + 2 meta chunks (one per document) = 4 pending chunks.
	doer := &fakeOpenRouterDoer{responses: []*http.Response{fakeEmbeddingsResponse(t, 4, 4, cost)}}
	idx := newTestEmbedIndexer(store, dir, embedTestReadiness("m", 4), doer)

	processed, err := idx.processEmbedBatch(context.Background())
	if err != nil {
		t.Fatalf("processEmbedBatch: %v", err)
	}
	if !processed {
		t.Fatalf("expected work done")
	}

	gotA, err := store.Get(docA.ID)
	if err != nil {
		t.Fatalf("Get(A): %v", err)
	}
	gotB, err := store.Get(docB.ID)
	if err != nil {
		t.Fatalf("Get(B): %v", err)
	}
	wantA := cost * float64(charsA) / float64(total)
	wantB := cost * float64(charsB) / float64(total)
	if diff := gotA.IndexCostUSD - wantA; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("doc A: expected cost share %v, got %v", wantA, gotA.IndexCostUSD)
	}
	if diff := gotB.IndexCostUSD - wantB; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("doc B: expected cost share %v, got %v", wantB, gotB.IndexCostUSD)
	}
	// A sanity check that the split is genuinely proportional, not merely
	// self-consistent with whatever charsOf happened to compute: B has 3x
	// A's body text and the same-shaped meta chunk, so B's share must
	// clearly exceed A's.
	if gotB.IndexCostUSD <= gotA.IndexCostUSD {
		t.Fatalf("expected doc B's larger share of text to cost more than doc A's, got A=%v B=%v", gotA.IndexCostUSD, gotB.IndexCostUSD)
	}
	if gotA.IndexModel != "" || gotB.IndexModel != "" {
		t.Fatalf("expected AddEmbedCost to leave index_model untouched, got %q and %q", gotA.IndexModel, gotB.IndexModel)
	}
}

func TestDocumentIndexer_ProcessEmbedBatchUpstreamFailureIsAnErrorAndLeavesDocumentAlone(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	doc := insertChunkedDocument(t, store, "sha-embed-fail", "manual.pdf", true, "Impeller replacement on the Yanmar 4JH.")

	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(500, `{"error":{"message":"upstream exploded"}}`)}}
	idx := newTestEmbedIndexer(store, dir, embedTestReadiness("m", 4), doer)

	if _, err := idx.processEmbedBatch(context.Background()); err == nil {
		t.Fatalf("expected an error from an upstream embeddings failure")
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status == "failed" {
		t.Fatalf("expected the document NOT to be marked failed over a late embedding, got status %q error %q", got.Status, got.Error)
	}

	pending, err := store.PendingEmbedChunks("m", 10, false)
	if err != nil {
		t.Fatalf("PendingEmbedChunks: %v", err)
	}
	if len(pending) != 2 { // the body chunk plus its meta chunk (seq 0)
		t.Fatalf("expected both chunks to remain pending (no vector written), got %+v", pending)
	}
}

// ── no work vs. a real error ────────────────────────────────────────────

func TestDocumentIndexer_ProcessEmbedBatchBlankModelIsNoWorkNotError(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	insertChunkedDocument(t, store, "sha-embed-blank-model", "manual.pdf", true, "Impeller replacement on the Yanmar 4JH.")

	doer := &fakeOpenRouterDoer{}
	idx := newTestEmbedIndexer(store, dir, embedTestReadiness("", 512), doer)

	processed, err := idx.processEmbedBatch(context.Background())
	if err != nil {
		t.Fatalf("expected no error for a blank embedding model, got %v", err)
	}
	if processed {
		t.Fatalf("expected no work done with a blank embedding model")
	}
	if len(doer.requests) != 0 {
		t.Fatalf("expected no HTTP call, got %d", len(doer.requests))
	}
}

func TestDocumentIndexer_ProcessEmbedBatchReadinessProblemIsNoWorkNotError(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	insertChunkedDocument(t, store, "sha-embed-problem", "manual.pdf", true, "Impeller replacement on the Yanmar 4JH.")

	doer := &fakeOpenRouterDoer{}
	idx := newTestEmbedIndexer(store, dir, func() (assistantReadiness, string, error) {
		return assistantReadiness{Enabled: false, Problem: "The assistant is switched off. Enable it in Settings → Assistant."}, "", nil
	}, doer)

	processed, err := idx.processEmbedBatch(context.Background())
	if err != nil {
		t.Fatalf("expected no error for a readiness Problem, got %v", err)
	}
	if processed {
		t.Fatalf("expected no work done while readiness reports a Problem")
	}
	if len(doer.requests) != 0 {
		t.Fatalf("expected no HTTP call, got %d", len(doer.requests))
	}
}

func TestDocumentIndexer_ProcessEmbedBatchReadinessErrorIsAnError(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	insertChunkedDocument(t, store, "sha-embed-readiness-err", "manual.pdf", true, "Impeller replacement on the Yanmar 4JH.")

	doer := &fakeOpenRouterDoer{}
	idx := newTestEmbedIndexer(store, dir, func() (assistantReadiness, string, error) {
		return assistantReadiness{}, "", fmt.Errorf("read settings: permission denied")
	}, doer)

	if _, err := idx.processEmbedBatch(context.Background()); err == nil {
		t.Fatalf("expected a readiness error to be returned as an error")
	}
	if len(doer.requests) != 0 {
		t.Fatalf("expected no HTTP call, got %d", len(doer.requests))
	}
}

// ── StartBackfill ────────────────────────────────────────────────────────

func TestDocumentIndexer_StartBackfillTwiceIsAnErrorTheSecondTime(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	idx := newTestEmbedIndexer(store, dir, embedTestReadiness("m", 4), &fakeOpenRouterDoer{})

	if err := idx.StartBackfill(); err != nil {
		t.Fatalf("StartBackfill (1st): %v", err)
	}
	if err := idx.StartBackfill(); err == nil {
		t.Fatalf("expected StartBackfill to refuse a second concurrent backfill")
	}
}
