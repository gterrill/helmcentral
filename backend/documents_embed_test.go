package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	pending, err := store.PendingEmbedChunks("openai/text-embedding-3-small", 4, 10, false)
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

	pending, err := store.PendingEmbedChunks("m", 4, 10, false)
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

// openRouterDoerFunc adapts a plain func to openRouterDoer, for a fake
// upstream whose answer depends on what was actually asked - fakeOpenRouterDoer
// (openrouter_client_test.go) replays a fixed queue of responses, which
// cannot express "reject any batch containing this one input".
type openRouterDoerFunc func(req *http.Request) (*http.Response, error)

func (f openRouterDoerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

// ── a backfill that cannot finish on its own ────────────────────────────

// TestDocumentIndexer_BackfillStopsWhenSemanticSearchGoesAwayMidRun covers
// the one way a running backfill could never end. processEmbedBatch returns
// early on a readiness Problem, so if that early return skips the backfill
// bookkeeping, turning Mate off (or pulling the key) mid-backfill leaves
// Running true forever: a spinner that never stops, a button that stays
// disabled, and every later start refused with 409 until the process is
// restarted. The backfill has to stop, and say why.
func TestDocumentIndexer_BackfillStopsWhenSemanticSearchGoesAwayMidRun(t *testing.T) {
	store := newTestDocumentStore(t)
	insertChunkedDocument(t, store, "sha-backfill-lost", "unconsented.pdf", false, "some body text")

	available := true
	readiness := func() (assistantReadiness, string, error) {
		if !available {
			return assistantReadiness{Enabled: false, Problem: "Mate is switched off."}, "", nil
		}
		return embedTestReadiness("test-embed-model", 4)()
	}
	doer := &fakeOpenRouterDoer{responses: []*http.Response{fakeEmbeddingsResponse(t, 4, 1, 0)}}
	idx := newTestEmbedIndexer(store, t.TempDir(), readiness, doer)

	if err := idx.StartBackfill(); err != nil {
		t.Fatalf("StartBackfill: %v", err)
	}
	available = false

	if _, err := idx.processEmbedBatch(context.Background()); err != nil {
		t.Fatalf("processEmbedBatch: %v", err)
	}

	status := idx.BackfillStatus()
	if status.Running {
		t.Fatalf("expected the backfill to stop once semantic search went away, got %+v", status)
	}
	if status.LastError == "" {
		t.Fatalf("expected the backfill to say why it stopped")
	}
}

// TestDocumentIndexer_StartBackfillDuringAPassIsNotCancelledByIt is the
// other end of the same bookkeeping. processEmbedBatch decides whether it is
// backfilling by snapshotting the flag, then may clear it several store
// calls later. A StartBackfill that lands in that window would otherwise be
// cleared by the older pass and report as instantly complete, having
// embedded nothing.
func TestDocumentIndexer_StartBackfillDuringAPassIsNotCancelledByIt(t *testing.T) {
	store := newTestDocumentStore(t)
	idx := newTestEmbedIndexer(store, t.TempDir(), embedTestReadiness("test-embed-model", 4), &fakeOpenRouterDoer{})

	if err := idx.StartBackfill(); err != nil {
		t.Fatalf("first StartBackfill: %v", err)
	}
	// The pass this generation belongs to, captured the way processEmbedBatch
	// captures it, before a second StartBackfill supersedes it.
	stale := idx.currentBackfillGen()

	idx.finishBackfill(stale)
	if err := idx.StartBackfill(); err != nil {
		t.Fatalf("second StartBackfill: %v", err)
	}

	// The older pass finishing now must not clear the newer backfill.
	idx.finishBackfill(stale)
	if !idx.BackfillStatus().Running {
		t.Fatalf("a stale pass cancelled the backfill that superseded it")
	}
}

// ── a batch that can never succeed ──────────────────────────────────────

// TestDocumentIndexer_APermanentlyFailingChunkIsSkippedRatherThanBlockingTheQueue
// covers the poison pill. One chunk the upstream will never accept must not
// stop every other chunk in the library from ever being embedded: the pass
// narrows the batch until the bad chunk is alone, gives it a bounded number
// of tries, then sets it aside (in memory, so a restart gives it another
// chance) and moves on.
func TestDocumentIndexer_APermanentlyFailingChunkIsSkippedRatherThanBlockingTheQueue(t *testing.T) {
	store := newTestDocumentStore(t)
	// The poison chunk is inserted first so it sorts ahead of the good one
	// and would otherwise hold the whole queue behind it.
	poison := insertChunkedDocument(t, store, "sha-poison", "poison.pdf", true, "chunk the upstream always rejects")
	good := insertChunkedDocument(t, store, "sha-good", "good.pdf", true, "an ordinary chunk")

	poisonChunks, err := store.ChunksFrom(poison.ID, 1)
	if err != nil || len(poisonChunks) != 1 {
		t.Fatalf("ChunksFrom: %v", err)
	}
	poisonText := poisonChunks[0].Text

	// Rejects any batch containing the poison chunk's text; embeds anything
	// else. A real upstream refusing one specific input behaves this way.
	doer := openRouterDoerFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		var parsed openRouterEmbeddingsRequest
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, err
		}
		for _, in := range parsed.Input {
			if in == poisonText {
				return openRouterFakeResponse(400, `{"error":{"message":"input rejected","code":400}}`), nil
			}
		}
		return fakeEmbeddingsResponse(t, 4, len(parsed.Input), 0), nil
	})

	idx := newTestEmbedIndexer(store, t.TempDir(), embedTestReadiness("test-embed-model", 4), doer)

	// Run the pass well past the point where a stuck queue would keep
	// handing back the same doomed batch. It must reach the good document.
	for i := 0; i < 40; i++ {
		if _, err := idx.processEmbedBatch(context.Background()); err != nil {
			continue
		}
	}

	counts, err := store.EmbeddingCounts("test-embed-model", 4)
	if err != nil {
		t.Fatalf("EmbeddingCounts: %v", err)
	}
	if counts.ChunksEmbedded == 0 {
		t.Fatalf("nothing was embedded: the poison chunk blocked the whole queue")
	}

	goodChunks, err := store.ChunksFrom(good.ID, 0)
	if err != nil {
		t.Fatalf("ChunksFrom(good): %v", err)
	}
	for _, c := range goodChunks {
		var n int
		if err := store.db.QueryRow(`SELECT count(*) FROM document_chunk_embeddings WHERE chunk_id = ?`, c.ID).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 1 {
			t.Fatalf("the good document's chunk %d was never embedded past the poison chunk", c.Seq)
		}
	}
}

// TestDocumentIndexer_BatchNarrowingDoesNotRestartOnEverySingleChunkAttempt
// pins the cost of isolating a poison chunk. Narrowing 64 to 1 takes six
// failed calls; if the limit springs back to 64 after each single-chunk
// attempt rather than only once the chunk is actually set aside, those six
// are paid three times over - around twenty failed upstream calls and twenty
// minutes of Run's backoff with the whole queue stopped behind them.
func TestDocumentIndexer_BatchNarrowingDoesNotRestartOnEverySingleChunkAttempt(t *testing.T) {
	store := newTestDocumentStore(t)
	idx := newTestEmbedIndexer(store, t.TempDir(), embedTestReadiness("m", 4), &fakeOpenRouterDoer{})

	one := []documentChunk{{ID: 1, DocumentID: "d"}}

	// Narrow all the way down first.
	for limit := maxOpenRouterEmbeddingsBatch; limit > 1; limit /= 2 {
		batch := make([]documentChunk, limit)
		idx.recordEmbedBatchFailure(batch)
	}
	if got := idx.currentEmbedBatchLimit(); got != 1 {
		t.Fatalf("expected the batch to narrow to 1, got %d", got)
	}

	// The first two attempts of the three are not yet a verdict on the chunk,
	// so the batch must stay narrow rather than starting the search over.
	idx.recordEmbedBatchFailure(one)
	if got := idx.currentEmbedBatchLimit(); got != 1 {
		t.Fatalf("expected the batch to stay at 1 while the chunk is still being tried, got %d", got)
	}
	idx.recordEmbedBatchFailure(one)
	if got := idx.currentEmbedBatchLimit(); got != 1 {
		t.Fatalf("expected the batch to stay at 1 on the second attempt, got %d", got)
	}

	// The third sets it aside, and only then is the full batch right again:
	// the blame now sits with the skipped chunk, not with the batch size.
	idx.recordEmbedBatchFailure(one)
	if idx.skippedEmbedCount() != 1 {
		t.Fatalf("expected the chunk to be set aside after %d attempts", documentsEmbedChunkAttempts)
	}
	if got := idx.currentEmbedBatchLimit(); got != maxOpenRouterEmbeddingsBatch {
		t.Fatalf("expected the full batch back once the chunk was set aside, got %d", got)
	}
}

// TestDocumentIndexer_ASuccessClearsAChunksEarlierFailures covers the other
// half of the attempt counter. The counts are meant to be consecutive: a
// chunk that failed twice during an outage and then embedded cleanly must
// start again from zero, or a single unrelated failure months later sets it
// aside on what looks like a third strike. The map would also grow without
// bound, since nothing else ever removes an entry.
func TestDocumentIndexer_ASuccessClearsAChunksEarlierFailures(t *testing.T) {
	store := newTestDocumentStore(t)
	idx := newTestEmbedIndexer(store, t.TempDir(), embedTestReadiness("m", 4), &fakeOpenRouterDoer{})

	one := []documentChunk{{ID: 7, DocumentID: "d"}}
	idx.recordEmbedBatchFailure(one)
	idx.recordEmbedBatchFailure(one)

	idx.recordEmbedBatchSuccess(one)

	// One more failure is now the first, not the third.
	idx.recordEmbedBatchFailure(one)
	if idx.skippedEmbedCount() != 0 {
		t.Fatalf("a chunk that succeeded in between was set aside on a single later failure")
	}
	if n := idx.embedFailureCount(7); n != 1 {
		t.Fatalf("expected the failure count to restart at 1, got %d", n)
	}
}

// ── SweepIfReady (ADR 0120: turning Mate on is the consent) ─────────────

func TestDocumentIndexer_SweepIfReadyEnrichesNotesAndStartsBackfillWhenReady(t *testing.T) {
	store := withTestDocumentStore(t)
	pending, err := store.InsertNote(document{SHA256: "note-sweep-pending", Filename: "a.md", MIME: "text/markdown", Enrich: false})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}

	idx := newTestEmbedIndexer(store, documentsDirPath(), embedTestReadiness("test-embed-model", 4), &fakeOpenRouterDoer{})
	idx.SweepIfReady()

	got, err := store.Get(pending.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Enrich {
		t.Fatalf("expected the sweep to set enrich=1 on the pending note")
	}
	if !idx.BackfillStatus().Running {
		t.Fatalf("expected the sweep to start an embeddings backfill")
	}
}

func TestDocumentIndexer_SweepIfReadyDoesNothingWhenMateNotReady(t *testing.T) {
	store := withTestDocumentStore(t)
	pending, err := store.InsertNote(document{SHA256: "note-sweep-not-ready", Filename: "a.md", MIME: "text/markdown", Enrich: false})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}

	idx := newTestEmbedIndexer(store, documentsDirPath(), func() (assistantReadiness, string, error) {
		return assistantReadiness{Enabled: false, Problem: "The assistant is switched off. Enable it in Settings → Assistant."}, "", nil
	}, &fakeOpenRouterDoer{})
	idx.SweepIfReady()

	got, err := store.Get(pending.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Enrich {
		t.Fatalf("expected the sweep to leave enrich=0 while Mate is not ready")
	}
	if idx.BackfillStatus().Running {
		t.Fatalf("expected the sweep to start no backfill while Mate is not ready")
	}
}

// The two halves of the sweep are independently gated - an operator can
// have Mate summarising notes with semantic search still switched off - the
// same split documentEnrichReadinessProblem/documentEmbedReadinessProblem
// draw everywhere else in this package.
func TestDocumentIndexer_SweepIfReadyEnrichesNotesButSkipsBackfillWithNoEmbeddingModel(t *testing.T) {
	store := withTestDocumentStore(t)
	pending, err := store.InsertNote(document{SHA256: "note-sweep-no-embed", Filename: "a.md", MIME: "text/markdown", Enrich: false})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}

	idx := newTestEmbedIndexer(store, documentsDirPath(), embedTestReadiness("", 0), &fakeOpenRouterDoer{})
	idx.SweepIfReady()

	got, err := store.Get(pending.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Enrich {
		t.Fatalf("expected the sweep to enrich notes independently of the (unconfigured) embedding model")
	}
	if idx.BackfillStatus().Running {
		t.Fatalf("expected no embeddings backfill to start with no embedding model configured")
	}
}

// A readiness-check error (a broken settings file, a secrets-store read
// failure) sweeps nothing rather than crashing the caller - SweepIfReady
// runs from main()'s boot path and from a settings-save HTTP handler, and
// neither may fail because a background sweep could not read settings this
// one time.
func TestDocumentIndexer_SweepIfReadyReadinessErrorDoesNothing(t *testing.T) {
	store := withTestDocumentStore(t)
	pending, err := store.InsertNote(document{SHA256: "note-sweep-readiness-err", Filename: "a.md", MIME: "text/markdown", Enrich: false})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}

	idx := newTestEmbedIndexer(store, documentsDirPath(), func() (assistantReadiness, string, error) {
		return assistantReadiness{}, "", fmt.Errorf("read settings: permission denied")
	}, &fakeOpenRouterDoer{})
	idx.SweepIfReady()

	got, err := store.Get(pending.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Enrich {
		t.Fatalf("expected a readiness-check error to sweep nothing")
	}
	if idx.BackfillStatus().Running {
		t.Fatalf("expected a readiness-check error to start no backfill")
	}
}

// ── the failure line the Documents toolbar shows (ADR 0120) ─────────────

// With the manual backfill gone, the toolbar's only Mate signal is
// LastError, so an embedding failure has to land there whether or not a
// backfill happens to be running. Once the boot backfill finishes, every
// later embed pass is automatic; out of credit on one of those must not be
// silent.
func TestDocumentIndexer_AutomaticEmbedFailureIsReported(t *testing.T) {
	store := newTestDocumentStore(t)
	insertChunkedDocument(t, store, "sha-auto-fail", "consented.pdf", true, "some body text")
	doer := &fakeOpenRouterDoer{errs: []error{fmt.Errorf("402 insufficient credits")}}
	idx := newTestEmbedIndexer(store, t.TempDir(), embedTestReadiness("test-embed-model", 4), doer)

	if _, err := idx.processEmbedBatch(context.Background()); err == nil {
		t.Fatal("expected the failing embed pass to return an error")
	}
	if status := idx.BackfillStatus(); !strings.Contains(status.LastError, "insufficient credits") {
		t.Fatalf("expected the automatic embed failure in LastError, got %+v", status)
	}
}

// A failure is the current state only while something is still waiting to
// be embedded. Once the queue is empty nothing is failing, so a red line
// left over from an earlier batch would be a false alarm.
func TestDocumentIndexer_EmptyQueueClearsAnEarlierFailure(t *testing.T) {
	store := newTestDocumentStore(t)
	idx := newTestEmbedIndexer(store, t.TempDir(), embedTestReadiness("test-embed-model", 4), &fakeOpenRouterDoer{})
	if err := idx.StartBackfill(); err != nil {
		t.Fatalf("StartBackfill: %v", err)
	}
	idx.recordEmbedError(fmt.Errorf("an earlier batch failed"))

	if _, err := idx.processEmbedBatch(context.Background()); err != nil {
		t.Fatalf("processEmbedBatch: %v", err)
	}
	status := idx.BackfillStatus()
	if status.Running || status.LastError != "" {
		t.Fatalf("expected an empty queue to finish the backfill with no error, got %+v", status)
	}
}

// TestDocumentIndexer_SkippedChunkKeepsLastErrorRatherThanClearingIt covers
// the other side of TestDocumentIndexer_EmptyQueueClearsAnEarlierFailure: a
// batch can come back empty not because nothing is waiting, but because the
// only pending chunk (besides its own meta chunk, which embeds cleanly) was
// just set aside by recordEmbedBatchFailure after documentsEmbedChunkAttempts
// failures. That is not "nothing failing" - the document was never embedded
// - so LastError (and the toolbar's failure row it drives) must survive the
// pass, not be cleared as a false alarm.
func TestDocumentIndexer_SkippedChunkKeepsLastErrorRatherThanClearingIt(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := insertChunkedDocument(t, store, "sha-skip-keeps-error", "poison-only.pdf", true, "a chunk the upstream never accepts")

	poisonChunks, err := store.ChunksFrom(doc.ID, 1)
	if err != nil || len(poisonChunks) != 1 {
		t.Fatalf("ChunksFrom: %v", err)
	}
	poisonText := poisonChunks[0].Text

	// Rejects any batch containing the poison chunk's text; embeds anything
	// else (its own meta chunk included) - the same doer shape
	// TestDocumentIndexer_APermanentlyFailingChunkIsSkippedRatherThanBlockingTheQueue
	// uses, for the same reason: a real upstream refusing one specific
	// input behaves this way.
	doer := openRouterDoerFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		var parsed openRouterEmbeddingsRequest
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, err
		}
		for _, in := range parsed.Input {
			if in == poisonText {
				return openRouterFakeResponse(400, `{"error":{"message":"input rejected","code":400}}`), nil
			}
		}
		return fakeEmbeddingsResponse(t, 4, len(parsed.Input), 0), nil
	})

	idx := newTestEmbedIndexer(store, t.TempDir(), embedTestReadiness("test-embed-model", 4), doer)

	// Run passes until the poison chunk is set aside - the same 40-pass
	// ceiling that test uses, well past however many narrowing takes.
	for i := 0; i < 40 && idx.skippedEmbedCount() == 0; i++ {
		idx.processEmbedBatch(context.Background())
	}
	if idx.skippedEmbedCount() != 1 {
		t.Fatalf("expected the poison chunk to be set aside")
	}
	if status := idx.BackfillStatus(); status.LastError == "" {
		t.Fatalf("expected LastError to be set once the chunk failed")
	}

	// The next pass finds nothing to send - the only pending chunk left is
	// the one just set aside - but the document behind it still has no
	// vector for its body text.
	if _, err := idx.processEmbedBatch(context.Background()); err != nil {
		t.Fatalf("processEmbedBatch: %v", err)
	}
	if status := idx.BackfillStatus(); status.LastError == "" {
		t.Fatalf("expected LastError to survive a pass that is empty only because its one chunk was skipped")
	}
}

// TestDocumentIndexer_SkippedChunkDocumentDeletedClearsLastError covers the
// skip set's other blind spot: it lives for the whole process and never
// shrinks on its own, so basing "clear LastError" on skippedEmbedCount()
// alone (rather than on what THIS pass's own query actually found) leaves
// LastError stuck forever once a document behind a skipped chunk is gone -
// there is nothing left anywhere for that chunk to keep failing on, but the
// skip map still remembers its id. Deleting the document is one legitimate
// way the queue becomes genuinely empty; PendingEmbedChunks itself won't
// return the poison chunk anymore, so this pass must see the queue as
// empty and clear the error, the same as if nothing had ever been skipped.
func TestDocumentIndexer_SkippedChunkDocumentDeletedClearsLastError(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := insertChunkedDocument(t, store, "sha-skip-deleted-clears-error", "poison-only.pdf", true, "a chunk the upstream never accepts")

	poisonChunks, err := store.ChunksFrom(doc.ID, 1)
	if err != nil || len(poisonChunks) != 1 {
		t.Fatalf("ChunksFrom: %v", err)
	}
	poisonText := poisonChunks[0].Text

	doer := openRouterDoerFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		var parsed openRouterEmbeddingsRequest
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, err
		}
		for _, in := range parsed.Input {
			if in == poisonText {
				return openRouterFakeResponse(400, `{"error":{"message":"input rejected","code":400}}`), nil
			}
		}
		return fakeEmbeddingsResponse(t, 4, len(parsed.Input), 0), nil
	})

	idx := newTestEmbedIndexer(store, t.TempDir(), embedTestReadiness("test-embed-model", 4), doer)

	for i := 0; i < 40 && idx.skippedEmbedCount() == 0; i++ {
		idx.processEmbedBatch(context.Background())
	}
	if idx.skippedEmbedCount() != 1 {
		t.Fatalf("expected the poison chunk to be set aside")
	}
	if status := idx.BackfillStatus(); status.LastError == "" {
		t.Fatalf("expected LastError to be set once the chunk failed")
	}

	// The operator deletes the document behind the skipped chunk - its
	// chunk row (and any embedding) cascades away with it (documents_
	// store.go's Delete), so PendingEmbedChunks no longer has any row to
	// return for it at all, skipped or not.
	if _, err := store.Delete(doc.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := idx.processEmbedBatch(context.Background()); err != nil {
		t.Fatalf("processEmbedBatch: %v", err)
	}
	if status := idx.BackfillStatus(); status.LastError != "" {
		t.Fatalf("expected LastError to clear once the skipped chunk's document was deleted, got %q", status.LastError)
	}
}
