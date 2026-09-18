package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// ── shared test helpers ────────────────────────────────────────────────

// resetDocumentQueryEmbedCache clears the package-level query-embedding LRU
// (documents_hybrid.go) for the duration of one test, restoring the prior
// cache afterward - the same package-level-var swap idiom every other
// global-store test helper in this package uses (withTestDocumentStore,
// withTestSecretsStore). The cache is deliberately process-wide (shared by
// every hybridDocumentSearch caller, HTTP handler or tool alike), so without
// this reset, two tests reusing the same query/model/dims combination would
// see each other's cached vector instead of each exercising its own doer.
func resetDocumentQueryEmbedCache(t *testing.T) {
	t.Helper()
	prev := documentQueryEmbedCacheInstance
	documentQueryEmbedCacheInstance = newDocumentQueryEmbedCache()
	t.Cleanup(func() { documentQueryEmbedCacheInstance = prev })
}

// hybridTestReadiness returns a documentSearchParams.Readiness func with
// semantic search configured and ready - Mate on, an OpenRouter key, the
// given embedding model/dimensions - mirroring documents_embed_test.go's
// own embedTestReadiness for the identical readiness shape.
func hybridTestReadiness(model string, dims int) func() (assistantReadiness, string, error) {
	return func() (assistantReadiness, string, error) {
		return assistantReadiness{
			Enabled: true, Configured: true, Model: "m",
			EmbeddingModel:      model,
			EmbeddingDimensions: dims,
		}, "sk-test", nil
	}
}

// hybridTestReadinessNotConfigured is hybridTestReadiness's opposite: ready
// otherwise, but no embedding model set - documentEmbedReadinessProblem's
// own "operator's choice to leave semantic search off" case.
func hybridTestReadinessNotConfigured() func() (assistantReadiness, string, error) {
	return func() (assistantReadiness, string, error) {
		return assistantReadiness{Enabled: true, Configured: true, Model: "m", EmbeddingModel: "", EmbeddingDimensions: 512}, "sk-test", nil
	}
}

// insertHybridDoc inserts a document with one "local" body chunk (seq 1,
// carrying text) - the minimum a search test needs from both retrievers'
// point of view (Search reads document_chunks_fts, SearchVector reads
// document_chunk_embeddings, both joined back through document_chunks).
func insertHybridDoc(t *testing.T, store *documentStore, sha, filename string, folderID *string, text string) document {
	t.Helper()
	doc, err := store.Insert(document{SHA256: sha, Filename: filename, MIME: "application/pdf", FolderID: folderID})
	if err != nil {
		t.Fatalf("Insert(%q): %v", filename, err)
	}
	if err := store.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: text},
	}); err != nil {
		t.Fatalf("ReplaceChunks(%q): %v", filename, err)
	}
	return doc
}

// embedDocChunk hand-writes a stored vector for docID's own body chunk (seq
// 1, inserted by insertHybridDoc) via SetChunkEmbeddings - the brief's own
// prescribed way to build a "vector side finds this, FTS doesn't" fixture
// without a real OpenRouter round trip.
func embedDocChunk(t *testing.T, store *documentStore, docID, model string, dims int, vec []float32) {
	t.Helper()
	chunks, err := store.ChunksFrom(docID, 1)
	if err != nil {
		t.Fatalf("ChunksFrom(%s): %v", docID, err)
	}
	if len(chunks) == 0 {
		t.Fatalf("expected at least one body chunk for %s", docID)
	}
	if err := store.SetChunkEmbeddings(model, dims, map[int64][]float32{chunks[0].ID: vec}); err != nil {
		t.Fatalf("SetChunkEmbeddings(%s): %v", docID, err)
	}
}

// queryEmbeddingResponse builds a 200 OpenRouter embeddings response
// carrying exactly one vector (index 0) - a query embedding is always a
// batch of one (hybridDocumentSearch's own contract) - via
// openRouterEmbeddingsResponse's own json tags, so it can never silently
// drift from the shape openRouterEmbeddings actually decodes (the same
// reasoning documents_embed_test.go's fakeEmbeddingsResponse gives for its
// own use of the real struct).
func queryEmbeddingResponse(t *testing.T, vec []float32) *http.Response {
	t.Helper()
	body, err := json.Marshal(openRouterEmbeddingsResponse{
		Object: "list", Model: "test-embed-model",
		Data: []openRouterEmbeddingData{{Index: 0, Embedding: vec}},
	})
	if err != nil {
		t.Fatalf("marshal query embedding response: %v", err)
	}
	return openRouterFakeResponse(200, string(body))
}

// slowOpenRouterDoer never returns on its own - it blocks until the
// request's own context is done (exactly how a real *http.Client behaves
// against a dead connection under a context deadline) and then reports that
// context's error, so a test can shrink documentsQueryEmbedTimeout and prove
// a slow uplink degrades to fts rather than hanging the search.
type slowOpenRouterDoer struct{}

func (slowOpenRouterDoer) Do(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

// ── ftsMatchQuery-not-ok short-circuit (rule 1: never spend an embedding on
// an empty query) ─────────────────────────────────────────────────────────

func TestHybridDocumentSearch_EmptyOrWhitespaceQuerySkipsSemanticEvenWhenConfigured(t *testing.T) {
	resetDocumentQueryEmbedCache(t)
	store := newTestDocumentStore(t)
	// A doer that WOULD answer if called - proving the empty-query path
	// never even reaches it is the whole point of this test.
	doer := &fakeOpenRouterDoer{responses: []*http.Response{queryEmbeddingResponse(t, []float32{1, 0, 0, 0})}}

	for _, q := range []string{"", "   ", "\t\n"} {
		outcome, err := hybridDocumentSearch(context.Background(), documentSearchParams{
			Store: store, Query: q, Limit: 10,
			Readiness: hybridTestReadiness("m", 4), Doer: doer,
		})
		if err != nil {
			t.Fatalf("query %q: %v", q, err)
		}
		if outcome.Mode != "fts" {
			t.Fatalf("query %q: expected mode fts, got %q", q, outcome.Mode)
		}
		if len(outcome.Results) != 0 {
			t.Fatalf("query %q: expected no results, got %+v", q, outcome.Results)
		}
		if outcome.SemanticProblem != "" {
			t.Fatalf("query %q: expected no semantic_problem, got %q", q, outcome.SemanticProblem)
		}
	}
	if len(doer.requests) != 0 {
		t.Fatalf("expected no embeddings call for an empty/whitespace query, got %d", len(doer.requests))
	}
}

func TestHybridDocumentSearch_EmptyWhitespacePunctuationQueriesNeverCallDoerWhenNotConfigured(t *testing.T) {
	resetDocumentQueryEmbedCache(t)
	store := newTestDocumentStore(t)
	doer := &fakeOpenRouterDoer{}

	for _, q := range []string{"", "   ", "!!!"} {
		outcome, err := hybridDocumentSearch(context.Background(), documentSearchParams{
			Store: store, Query: q, Limit: 10,
			Readiness: hybridTestReadinessNotConfigured(), Doer: doer,
		})
		if err != nil {
			t.Fatalf("query %q: %v", q, err)
		}
		if outcome.Mode != "fts" {
			t.Fatalf("query %q: expected mode fts, got %q", q, outcome.Mode)
		}
		if len(outcome.Results) != 0 {
			t.Fatalf("query %q: expected no results (nothing indexed), got %+v", q, outcome.Results)
		}
	}
	if len(doer.requests) != 0 {
		t.Fatalf("expected no embeddings call, got %d", len(doer.requests))
	}
}

// ── semantic not configured ─────────────────────────────────────────────

func TestHybridDocumentSearch_SemanticNotConfiguredGivesModeFTSOnly(t *testing.T) {
	resetDocumentQueryEmbedCache(t)
	store := newTestDocumentStore(t)
	insertHybridDoc(t, store, "sha-a", "manual.pdf", nil, "check the impeller before each season")
	doer := &fakeOpenRouterDoer{}

	outcome, err := hybridDocumentSearch(context.Background(), documentSearchParams{
		Store: store, Query: "impeller", Limit: 10,
		Readiness: hybridTestReadinessNotConfigured(), Doer: doer,
	})
	if err != nil {
		t.Fatalf("hybridDocumentSearch: %v", err)
	}
	if outcome.Mode != "fts" {
		t.Fatalf("expected mode fts, got %q", outcome.Mode)
	}
	if outcome.SemanticProblem != "" {
		t.Fatalf("expected no semantic_problem when semantic search was never configured, got %q", outcome.SemanticProblem)
	}
	if len(outcome.Results) != 1 {
		t.Fatalf("expected 1 FTS result, got %+v", outcome.Results)
	}
	if len(doer.requests) != 0 {
		t.Fatalf("expected no embeddings call, got %d", len(doer.requests))
	}
}

// nilReadinessParams (a zero-value Readiness field) is the seam a test
// double for assistantToolDeps that never wires documentSearchReadiness
// exercises - documentSemanticSearch's own nil check must treat that
// identically to "not configured", not panic.
func TestHybridDocumentSearch_NilReadinessFieldGivesModeFTSOnly(t *testing.T) {
	resetDocumentQueryEmbedCache(t)
	store := newTestDocumentStore(t)
	insertHybridDoc(t, store, "sha-a", "manual.pdf", nil, "check the impeller before each season")

	outcome, err := hybridDocumentSearch(context.Background(), documentSearchParams{
		Store: store, Query: "impeller", Limit: 10,
	})
	if err != nil {
		t.Fatalf("hybridDocumentSearch: %v", err)
	}
	if outcome.Mode != "fts" {
		t.Fatalf("expected mode fts, got %q", outcome.Mode)
	}
	if len(outcome.Results) != 1 {
		t.Fatalf("expected 1 FTS result, got %+v", outcome.Results)
	}
}

// ── configured and working ───────────────────────────────────────────────

func TestHybridDocumentSearch_ConfiguredAndWorkingFindsVectorOnlyDocument(t *testing.T) {
	resetDocumentQueryEmbedCache(t)
	store := newTestDocumentStore(t)
	const model = "openai/text-embedding-3-small"
	const dims = 4

	docA := insertHybridDoc(t, store, "sha-a", "manual.pdf", nil, "check the impeller before each season")
	docB := insertHybridDoc(t, store, "sha-b", "receipt.pdf", nil, "checked the fridge compressor wiring")

	queryVec := []float32{1, 2, 3, 4}
	// Doc B shares no keyword with "impeller" at all - only findable through
	// its stored vector, hand-written here to sit in the identical
	// direction as the query vector the fake doer returns below (cosine
	// similarity 1.0 once both sides are normalised).
	embedDocChunk(t, store, docB.ID, model, dims, queryVec)

	doer := &fakeOpenRouterDoer{responses: []*http.Response{queryEmbeddingResponse(t, queryVec)}}

	outcome, err := hybridDocumentSearch(context.Background(), documentSearchParams{
		Store: store, Query: "impeller", Limit: 10,
		Readiness: hybridTestReadiness(model, dims), Doer: doer,
	})
	if err != nil {
		t.Fatalf("hybridDocumentSearch: %v", err)
	}
	if outcome.Mode != "hybrid" {
		t.Fatalf("expected mode hybrid, got %q", outcome.Mode)
	}
	ids := map[string]bool{}
	for _, r := range outcome.Results {
		ids[r.DocumentID] = true
	}
	if !ids[docA.ID] {
		t.Fatalf("expected the FTS hit (doc A) present, got %+v", outcome.Results)
	}
	if !ids[docB.ID] {
		t.Fatalf("expected the vector-only hit (doc B, no shared keyword) present, got %+v", outcome.Results)
	}
}

func TestHybridDocumentSearch_DocFoundByBothRanksAboveFTSOnlyAndKeepsFTSSnippet(t *testing.T) {
	resetDocumentQueryEmbedCache(t)
	store := newTestDocumentStore(t)
	const model = "openai/text-embedding-3-small"
	const dims = 4

	both := insertHybridDoc(t, store, "sha-both", "both.pdf", nil, "replace the impeller every season")
	ftsOnly := insertHybridDoc(t, store, "sha-fts-only", "fts-only.pdf", nil, "impeller spare parts list")

	queryVec := []float32{1, 2, 3, 4}
	embedDocChunk(t, store, both.ID, model, dims, queryVec)
	// ftsOnly is never embedded at all - absent from document_chunk_embeddings
	// entirely, so it can only ever come from the FTS side.

	doer := &fakeOpenRouterDoer{responses: []*http.Response{queryEmbeddingResponse(t, queryVec)}}

	outcome, err := hybridDocumentSearch(context.Background(), documentSearchParams{
		Store: store, Query: "impeller", Limit: 10,
		Readiness: hybridTestReadiness(model, dims), Doer: doer,
	})
	if err != nil {
		t.Fatalf("hybridDocumentSearch: %v", err)
	}
	if outcome.Mode != "hybrid" {
		t.Fatalf("expected mode hybrid, got %q", outcome.Mode)
	}
	if len(outcome.Results) != 2 {
		t.Fatalf("expected 2 results, got %+v", outcome.Results)
	}
	if outcome.Results[0].DocumentID != both.ID {
		t.Fatalf("expected the both-sides hit to rank first (RRF), got %+v", outcome.Results)
	}
	if outcome.Results[1].DocumentID != ftsOnly.ID {
		t.Fatalf("expected the fts-only hit second, got %+v", outcome.Results)
	}
	if !strings.Contains(outcome.Results[0].Snippet, "\x02") {
		t.Fatalf("expected the both-sides hit's snippet to be the FTS one (carrying highlight markers), got %q", outcome.Results[0].Snippet)
	}
}

// ── embedding failure: degrade, don't mask ──────────────────────────────

func TestHybridDocumentSearch_EmbeddingFailureGivesModeFTSAndSemanticProblem(t *testing.T) {
	resetDocumentQueryEmbedCache(t)
	store := newTestDocumentStore(t)
	insertHybridDoc(t, store, "sha-a", "manual.pdf", nil, "check the impeller before each season")

	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(500, `{"error":{"message":"upstream exploded"}}`)}}

	outcome, err := hybridDocumentSearch(context.Background(), documentSearchParams{
		Store: store, Query: "impeller", Limit: 10,
		Readiness: hybridTestReadiness("m", 4), Doer: doer,
	})
	if err != nil {
		t.Fatalf("hybridDocumentSearch: %v", err)
	}
	if outcome.Mode != "fts" {
		t.Fatalf("expected mode fts when the embedding call fails, got %q", outcome.Mode)
	}
	if outcome.SemanticProblem == "" {
		t.Fatalf("expected a non-empty semantic_problem naming the upstream failure")
	}
	if !strings.Contains(outcome.SemanticProblem, "upstream exploded") {
		t.Fatalf("expected semantic_problem to name the upstream error, got %q", outcome.SemanticProblem)
	}
	if len(outcome.Results) != 1 {
		t.Fatalf("expected the FTS result intact, got %+v", outcome.Results)
	}
}

func TestHybridDocumentSearch_SlowDoerTimesOutGivesModeFTSAndSemanticProblemRatherThanHanging(t *testing.T) {
	resetDocumentQueryEmbedCache(t)
	prev := documentsQueryEmbedTimeout
	documentsQueryEmbedTimeout = 20 * time.Millisecond
	t.Cleanup(func() { documentsQueryEmbedTimeout = prev })

	store := newTestDocumentStore(t)
	insertHybridDoc(t, store, "sha-a", "manual.pdf", nil, "check the impeller before each season")

	done := make(chan struct{})
	var outcome documentSearchOutcome
	var err error
	go func() {
		outcome, err = hybridDocumentSearch(context.Background(), documentSearchParams{
			Store: store, Query: "impeller", Limit: 10,
			Readiness: hybridTestReadiness("m", 4), Doer: slowOpenRouterDoer{},
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("hybridDocumentSearch did not return within 5s of a %v embed timeout - it hung", documentsQueryEmbedTimeout)
	}

	if err != nil {
		t.Fatalf("hybridDocumentSearch: %v", err)
	}
	if outcome.Mode != "fts" {
		t.Fatalf("expected mode fts, got %q", outcome.Mode)
	}
	if outcome.SemanticProblem == "" {
		t.Fatalf("expected a non-empty semantic_problem naming the timeout")
	}
}

// ── query-embedding cache ───────────────────────────────────────────────

func TestHybridDocumentSearch_CacheHitsSameQueryAndMissesOnModelOrDimsChange(t *testing.T) {
	resetDocumentQueryEmbedCache(t)
	store := newTestDocumentStore(t)

	doer := &fakeOpenRouterDoer{responses: []*http.Response{
		queryEmbeddingResponse(t, []float32{1, 0, 0, 0}),
		queryEmbeddingResponse(t, []float32{1, 0, 0, 0, 0, 0, 0, 0}),
		queryEmbeddingResponse(t, []float32{1, 0, 0, 0, 0, 0, 0, 0}),
	}}

	run := func(model string, dims int) {
		t.Helper()
		if _, err := hybridDocumentSearch(context.Background(), documentSearchParams{
			Store: store, Query: "impeller", Limit: 10,
			Readiness: hybridTestReadiness(model, dims), Doer: doer,
		}); err != nil {
			t.Fatalf("hybridDocumentSearch(%s,%d): %v", model, dims, err)
		}
	}

	run("model-a", 4)
	run("model-a", 4) // same model+dims+query: cache hit
	if len(doer.requests) != 1 {
		t.Fatalf("expected exactly one embeddings call for a repeated query, got %d", len(doer.requests))
	}

	run("model-a", 8) // dimensions change: cache miss
	if len(doer.requests) != 2 {
		t.Fatalf("expected a second call after a dimensions change, got %d", len(doer.requests))
	}

	run("model-b", 8) // model change: cache miss
	if len(doer.requests) != 3 {
		t.Fatalf("expected a third call after a model change, got %d", len(doer.requests))
	}
}

func TestHybridDocumentSearch_CacheEvictsPast32DistinctQueries(t *testing.T) {
	resetDocumentQueryEmbedCache(t)
	store := newTestDocumentStore(t)

	const totalDistinct = documentQueryEmbedCacheCapacity + 1 // fills the cache, then one more to evict query 0
	responses := make([]*http.Response, 0, totalDistinct+1)
	for i := 0; i < totalDistinct+1; i++ {
		responses = append(responses, queryEmbeddingResponse(t, []float32{float32(i + 1), 0, 0, 0}))
	}
	doer := &fakeOpenRouterDoer{responses: responses}

	run := func(query string) {
		t.Helper()
		if _, err := hybridDocumentSearch(context.Background(), documentSearchParams{
			Store: store, Query: query, Limit: 10,
			Readiness: hybridTestReadiness("m", 4), Doer: doer,
		}); err != nil {
			t.Fatalf("hybridDocumentSearch(%q): %v", query, err)
		}
	}

	for i := 0; i < totalDistinct; i++ {
		run(fmt.Sprintf("distinct query %d", i))
	}
	if len(doer.requests) != totalDistinct {
		t.Fatalf("expected %d embeddings calls for %d distinct queries, got %d", totalDistinct, totalDistinct, len(doer.requests))
	}

	// "distinct query 0" was the least-recently-used entry once
	// documentQueryEmbedCacheCapacity more distinct queries followed it, so
	// it must have been evicted - re-running it costs a fresh call.
	run("distinct query 0")
	if len(doer.requests) != totalDistinct+1 {
		t.Fatalf("expected query 0 to have been evicted and re-fetched, got %d requests", len(doer.requests))
	}
}

// ── pagination applies to the fused ranking, not a re-fused page ────────

func TestHybridDocumentSearch_OffsetAndLimitAppliedAfterFusion(t *testing.T) {
	resetDocumentQueryEmbedCache(t)
	store := newTestDocumentStore(t)
	const model = "openai/text-embedding-3-small"
	const dims = 4

	docs := make([]document, 5)
	for i := range docs {
		docs[i] = insertHybridDoc(t, store, fmt.Sprintf("sha-page-%d", i), fmt.Sprintf("doc-%d.pdf", i), nil, "impeller service manual")
		embedDocChunk(t, store, docs[i].ID, model, dims, []float32{float32(i + 1), 0, 0, 0})
	}

	doer := &fakeOpenRouterDoer{responses: []*http.Response{queryEmbeddingResponse(t, []float32{1, 0, 0, 0})}}
	params := func(limit, offset int) documentSearchParams {
		return documentSearchParams{
			Store: store, Query: "impeller", Limit: limit, Offset: offset,
			Readiness: hybridTestReadiness(model, dims), Doer: doer,
		}
	}

	full, err := hybridDocumentSearch(context.Background(), params(5, 0))
	if err != nil {
		t.Fatalf("full: %v", err)
	}
	if full.Mode != "hybrid" || len(full.Results) != 5 {
		t.Fatalf("expected 5 hybrid results, got mode=%q %+v", full.Mode, full.Results)
	}

	page2, err := hybridDocumentSearch(context.Background(), params(2, 2))
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Results) != 2 {
		t.Fatalf("expected 2 results on page 2, got %+v", page2.Results)
	}
	if page2.Results[0].DocumentID != full.Results[2].DocumentID || page2.Results[1].DocumentID != full.Results[3].DocumentID {
		t.Fatalf("expected page 2 to be items 3 and 4 of the fused ranking, got %+v vs full %+v", page2.Results, full.Results)
	}
	// Both calls share the identical query/model/dims, so the second reuses
	// the cached query embedding - proof that page 2 was not built by a
	// second, independently re-fused search (each with its own new
	// embedding call), just a different slice of the same pool.
	if len(doer.requests) != 1 {
		t.Fatalf("expected the second call to reuse the cached query embedding, got %d requests", len(doer.requests))
	}
}

// ── folder/tag scoping and folder errors ─────────────────────────────────

func TestHybridDocumentSearch_FolderAndTagScopingReachBothRetrieversAndUnknownFolderErrors(t *testing.T) {
	resetDocumentQueryEmbedCache(t)
	store := newTestDocumentStore(t)
	const model = "openai/text-embedding-3-small"
	const dims = 4

	manuals, err := store.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder(Manuals): %v", err)
	}
	receipts, err := store.CreateFolder("Receipts", nil)
	if err != nil {
		t.Fatalf("CreateFolder(Receipts): %v", err)
	}

	inFolder := insertHybridDoc(t, store, "sha-in", "in-manuals.pdf", &manuals.ID, "impeller service notes")
	outFolder := insertHybridDoc(t, store, "sha-out", "in-receipts.pdf", &receipts.ID, "impeller purchase receipt")
	vec := []float32{1, 2, 3, 4}
	embedDocChunk(t, store, inFolder.ID, model, dims, vec)
	embedDocChunk(t, store, outFolder.ID, model, dims, vec)

	doer := &fakeOpenRouterDoer{responses: []*http.Response{queryEmbeddingResponse(t, vec)}}

	folderOutcome, err := hybridDocumentSearch(context.Background(), documentSearchParams{
		Store: store, Query: "impeller", Limit: 10,
		FolderID: &manuals.ID, Recursive: true,
		Readiness: hybridTestReadiness(model, dims), Doer: doer,
	})
	if err != nil {
		t.Fatalf("hybridDocumentSearch (folder): %v", err)
	}
	if len(folderOutcome.Results) != 1 || folderOutcome.Results[0].DocumentID != inFolder.ID {
		t.Fatalf("expected folder scoping to reach both retrievers (only the Manuals document), got %+v", folderOutcome.Results)
	}

	// Tag the Receipts document and confirm tag scoping (with no folder
	// filter this time) also reaches both retrievers: the same cached query
	// vector, so this costs no second embeddings call.
	if err := store.UpdateMeta(outFolder.ID, nil, nil, []string{"finance"}); err != nil {
		t.Fatalf("UpdateMeta: %v", err)
	}
	tagOutcome, err := hybridDocumentSearch(context.Background(), documentSearchParams{
		Store: store, Query: "impeller", Limit: 10, Tag: "finance",
		Readiness: hybridTestReadiness(model, dims), Doer: doer,
	})
	if err != nil {
		t.Fatalf("hybridDocumentSearch (tag): %v", err)
	}
	if len(tagOutcome.Results) != 1 || tagOutcome.Results[0].DocumentID != outFolder.ID {
		t.Fatalf("expected tag scoping to reach both retrievers, got %+v", tagOutcome.Results)
	}
	if len(doer.requests) != 1 {
		t.Fatalf("expected both scoped calls to share one cached query embedding, got %d requests", len(doer.requests))
	}

	unknown := "does-not-exist"
	if _, err := hybridDocumentSearch(context.Background(), documentSearchParams{
		Store: store, Query: "impeller", Limit: 10,
		FolderID: &unknown, Recursive: false,
		Readiness: hybridTestReadiness(model, dims), Doer: doer,
	}); !errors.Is(err, errFolderNotFound) {
		t.Fatalf("expected errFolderNotFound for an unknown folder, got %v", err)
	}
}

// ── fuseDocumentSearchResults in isolation ───────────────────────────────

func TestFuseDocumentSearchResults_PrefersFTSResultForSharedDocumentAndKeepsBothOnlyOthers(t *testing.T) {
	ftsHits := []documentSearchResult{
		{DocumentID: "shared", Snippet: "fts snippet \x02match\x03"},
		{DocumentID: "fts-only", Snippet: "fts only"},
	}
	vecHits := []documentSearchResult{
		{DocumentID: "shared", Snippet: "vector plain excerpt"},
		{DocumentID: "vec-only", Snippet: "vector only"},
	}
	fused := fuseDocumentSearchResults(ftsHits, vecHits)
	if len(fused) != 3 {
		t.Fatalf("expected 3 fused results, got %+v", fused)
	}
	byID := map[string]documentSearchResult{}
	for _, r := range fused {
		byID[r.DocumentID] = r
	}
	if byID["shared"].Snippet != "fts snippet \x02match\x03" {
		t.Fatalf("expected the shared document to keep its FTS snippet, got %q", byID["shared"].Snippet)
	}
	if _, ok := byID["fts-only"]; !ok {
		t.Fatalf("expected the fts-only document present")
	}
	if _, ok := byID["vec-only"]; !ok {
		t.Fatalf("expected the vec-only document present")
	}
	if fused[0].DocumentID != "shared" {
		t.Fatalf("expected the shared (both-sides) document to rank first, got %+v", fused)
	}
}

// TestHybridDocumentSearch_CorruptVectorRowDegradesRatherThanFailingTheSearch
// pins the choice between two ways a bad vector row could be handled. Once
// FTS has already produced a real answer, failing the whole request over the
// semantic half means one corrupt blob makes the entire library
// unsearchable, keyword search included - a far worse outcome than the
// missing half it is reporting. So a SearchVector error joins the query
// embedding's own failure path: keyword results, mode fts, and
// semantic_problem naming what went wrong. (A bad folder id never reaches
// here: Search runs first and applies the identical scoping check, so it
// errors on that before the vector side is ever asked.)
func TestHybridDocumentSearch_CorruptVectorRowDegradesRatherThanFailingTheSearch(t *testing.T) {
	resetDocumentQueryEmbedCache(t)
	store := newTestDocumentStore(t)

	doc := insertHybridDoc(t, store, "sha-hybrid-corrupt", "impeller.pdf", nil, "impeller replacement procedure")
	embedDocChunk(t, store, doc.ID, "m", 4, []float32{1, 0, 0, 0})

	chunks, err := store.ChunksFrom(doc.ID, 1)
	if err != nil || len(chunks) == 0 {
		t.Fatalf("ChunksFrom: chunks=%d err=%v", len(chunks), err)
	}
	// Bit rot in the stored blob: decodeEmbedding rejects it outright, so
	// SearchVector returns an error rather than a ranked list.
	if _, err := store.db.Exec(`UPDATE document_chunk_embeddings SET vector = ? WHERE chunk_id = ?`, []byte{1, 2, 3}, chunks[0].ID); err != nil {
		t.Fatalf("corrupt blob: %v", err)
	}

	doer := &fakeOpenRouterDoer{responses: []*http.Response{queryEmbeddingResponse(t, []float32{1, 0, 0, 0})}}
	outcome, err := hybridDocumentSearch(context.Background(), documentSearchParams{
		Store: store, Query: "impeller", Limit: 10,
		Readiness: hybridTestReadiness("m", 4), Doer: doer,
	})
	if err != nil {
		t.Fatalf("expected a degraded search, not an error: %v", err)
	}
	if outcome.Mode != "fts" {
		t.Fatalf("expected mode fts, got %q", outcome.Mode)
	}
	if outcome.SemanticProblem == "" {
		t.Fatalf("expected semantic_problem to name the corrupt vector row")
	}
	if len(outcome.Results) != 1 || outcome.Results[0].DocumentID != doc.ID {
		t.Fatalf("expected the FTS hit to survive, got %+v", outcome.Results)
	}
}
