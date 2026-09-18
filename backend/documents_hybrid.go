package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// This file is E1d: one search entry point shared by the document library's
// HTTP API (listDocumentsHandler, documents_handlers.go) and Mate's
// search_documents tool (assistant_tools.go), so a keyword search and an
// agentic one never drift into two different notions of "the best match".
//
// A search always runs FTS5 (ftsMatchQuery/Search, documents_search.go and
// documents_store.go) - that is the library's floor, present the moment a
// document is indexed, with no OpenRouter call and no settings dependency.
// Semantic search (SearchVector, documents_vector.go/documents_store.go) is
// layered on top only when the operator has configured an embedding model,
// and the two ranked lists are merged with reciprocal rank fusion
// (documents_vector.go's reciprocalRankFusion) rather than either list
// silently winning.
//
// AGENTS.md's fallback policy forbids masking an upstream failure as a
// plausible-looking result. The query-embedding failure path below is NOT
// that: on a failed embed, hybridDocumentSearch still returns the FTS
// results (a real, complete answer to "what matches this keyword") and adds
// SemanticProblem naming exactly what went missing and why. Nothing is
// hidden - the operator sees both the results they can trust and the reason
// the rest of the picture isn't there. A corrupt vector row takes the same
// path, for the same reason: FTS has already answered by then, and one bad
// blob must not take the library's keyword search down with it. What is
// still a hard error is a failure that makes the whole search meaningless -
// an FTS error, a bad folder id, a broken settings or secrets read.

// documentsFusionCandidates is the FLOOR on the candidate pool each
// retriever (FTS and vector) contributes to reciprocalRankFusion - not the
// page size a caller asked for, and not a ceiling on it either. Fusing only
// `limit` results from each side would make the second page of a search
// depend on which documents happened to make the first page's own cut from
// each retriever; fusing a wide pool once and paging the fused ranking
// afterward keeps every page consistent with the others. See
// documentFusionPoolSize for why the pool has to grow past this floor rather
// than stopping at it.
const documentsFusionCandidates = 50

// documentFusionPoolSize is how many documents each retriever is asked for,
// given the page the caller wants. It has to cover offset+limit as well as
// the fusion floor: a pool fixed at documentsFusionCandidates would truncate
// a ?limit=200 listing to 50 rows, and return an empty page for every offset
// past the 50th document - both of which the plain Search(limit, offset)
// call this replaced got right. Pulling offset+limit from each side keeps
// the fused ranking long enough to actually contain the requested page.
func documentFusionPoolSize(limit, offset int) int {
	want := limit + offset
	if want < documentsFusionCandidates {
		return documentsFusionCandidates
	}
	return want
}

// documentsQueryEmbedTimeout bounds one query-embedding call - a single
// input, so nothing like the indexer's multi-minute OCR timeout
// (documents_embed.go). A var, not a const, so a test can shrink it rather
// than waiting out 8 real seconds. A boat's uplink drops constantly; a
// search must degrade to keyword-only rather than hang waiting on a dead
// connection.
var documentsQueryEmbedTimeout = 8 * time.Second

// documentSearchParams is hybridDocumentSearch's whole input: the store to
// search (a field, not globalDocumentStore reached for internally, so a test
// can drive this with its own t.TempDir()-backed store the way
// documents_store_test.go's own tests do), the raw operator query exactly as
// typed (ftsMatchQuery does the sanitising), the same folder/recursive/tag/
// limit/offset scoping Search and SearchVector already share, and the two
// seams that make the semantic side testable without a real settings file or
// a real OpenRouter call.
type documentSearchParams struct {
	Store     *documentStore
	Query     string
	FolderID  *string
	Recursive bool
	Tag       string
	Limit     int
	Offset    int

	// Readiness mirrors checkAssistantReadiness's own signature - the same
	// shape documentIndexer.readiness (documents_indexer.go) already uses for
	// exactly this reason. nil means semantic search is simply unavailable to
	// this caller (a tool test that never wired it up, e.g.), the same as a
	// readiness Problem: fts only, no error, no SemanticProblem.
	Readiness func() (assistantReadiness, string, error)

	// Doer is the OpenRouter client the query-embedding call goes through.
	// nil falls back to openRouterHTTPClient (the production client every
	// other interactive OpenRouter call in this codebase uses) - callers only
	// need to set this in a test, never in production.
	Doer openRouterDoer
}

// documentSearchOutcome is hybridDocumentSearch's result: the fused,
// paginated hits, which retrievers actually contributed (Mode), and, when
// semantic search was configured but the query embedding itself failed, one
// line saying why (SemanticProblem - see this file's own top-of-file
// comment for why that is surfacing, not masking).
type documentSearchOutcome struct {
	Results []documentSearchResult
	// Mode is "hybrid" only when the vector side actually ran and returned
	// without error; "fts" otherwise, including every case where semantic
	// search was never attempted at all (not configured, or the query was
	// empty and never reached the semantic stage).
	Mode string
	// SemanticProblem is set only when semantic search was configured and
	// the query embedding failed - never merely because it isn't configured.
	// An operator who has left semantic search off is not owed an error
	// message on every search.
	SemanticProblem string
}

// hybridDocumentSearch is the one search entry point listDocumentsHandler
// (documents_handlers.go) and the search_documents tool
// (assistant_tools.go) both call. See this file's own top-of-file comment
// for the fallback-policy reasoning behind the query-embedding failure path.
func hybridDocumentSearch(ctx context.Context, params documentSearchParams) (documentSearchOutcome, error) {
	matchQuery, ok := ftsMatchQuery(params.Query)
	if !ok {
		// Empty (or all-whitespace) input, sanitised down to nothing: no FTS
		// call (FTS5 rejects an empty MATCH string outright - Search's own
		// contract), and no query embedding spent on a search that can never
		// match anything.
		return documentSearchOutcome{Results: []documentSearchResult{}, Mode: "fts"}, nil
	}

	pool := documentFusionPoolSize(params.Limit, params.Offset)

	ftsHits, err := params.Store.Search(matchQuery, params.FolderID, params.Recursive, params.Tag, pool, 0)
	if err != nil {
		return documentSearchOutcome{}, err
	}

	vectorHits, ran, problem, err := documentSemanticSearch(ctx, params, pool)
	if err != nil {
		return documentSearchOutcome{}, err
	}

	outcome := documentSearchOutcome{Mode: "fts", SemanticProblem: problem}
	fused := ftsHits
	if ran {
		outcome.Mode = "hybrid"
		fused = fuseDocumentSearchResults(ftsHits, vectorHits)
	}

	outcome.Results = paginateDocumentSearchResults(fused, params.Offset, params.Limit)
	return outcome, nil
}

// documentSemanticSearch runs the vector side of hybridDocumentSearch: the
// readiness check, the (cached) query embedding, and SearchVector itself.
// ran is true only once SearchVector has actually executed and returned
// without error - the one signal hybridDocumentSearch uses to decide
// "hybrid" vs "fts". err is a genuine Go error only for a broken
// settings/secrets read, which says nothing about the search itself and
// would be a lie to report as a search result. Everything that can go wrong
// with the semantic half specifically - a failed query embedding, or
// corrupt vector data under SearchVector - is reported as
// (nil, false, problem, nil): the FTS half has already answered by then,
// and see this file's own top-of-file comment for why saying so beats
// failing the whole request.
func documentSemanticSearch(ctx context.Context, params documentSearchParams, pool int) (results []documentSearchResult, ran bool, problem string, err error) {
	if params.Readiness == nil {
		return nil, false, "", nil
	}
	readiness, apiKey, err := params.Readiness()
	if err != nil {
		return nil, false, "", fmt.Errorf("document search: check assistant readiness: %w", err)
	}
	if p := documentEmbedReadinessProblem(readiness); p != "" {
		// Not configured (or chat itself isn't ready): the operator's own
		// setting, not an error - see documentSearchOutcome.SemanticProblem's
		// own doc comment.
		return nil, false, "", nil
	}
	model, dims := readiness.EmbeddingModel, readiness.EmbeddingDimensions

	vec, err := cachedQueryEmbedding(ctx, params.Doer, apiKey, model, dims, params.Query)
	if err != nil {
		log.Printf("documents: hybrid search: query embedding failed: %v", err)
		return nil, false, firstErrorLine(err), nil
	}

	vectorHits, err := params.Store.SearchVector(vec, model, params.FolderID, params.Recursive, params.Tag, pool)
	if err != nil {
		// Corrupt vector data, and nothing else: a bad folder id never gets
		// this far, because Search applies the identical scoping check first
		// and errors on it before the semantic side is ever asked. By the
		// time this runs, FTS has already produced a real answer, so failing
		// the whole request here would make one bad blob take the entire
		// library's keyword search down with it. The caller gets what works
		// and is told what doesn't, the same as a failed query embedding.
		log.Printf("documents: hybrid search: vector search failed: %v", err)
		return nil, false, firstErrorLine(err), nil
	}
	return vectorHits, true, "", nil
}

// fuseDocumentSearchResults merges ftsHits and vectorHits (both already
// ordered best-first, both already collapsed to one row per document by
// Search/SearchVector themselves) with reciprocalRankFusion
// (documents_vector.go), preferring the FTS side's own documentSearchResult
// for a document present in both: its Snippet carries the \x02/\x03 match
// markers the UI renders as highlights, where the vector side's is a plain
// excerpt with nothing marked.
func fuseDocumentSearchResults(ftsHits, vectorHits []documentSearchResult) []documentSearchResult {
	ftsByID := make(map[string]documentSearchResult, len(ftsHits))
	ftsIDs := make([]string, len(ftsHits))
	for i, r := range ftsHits {
		ftsByID[r.DocumentID] = r
		ftsIDs[i] = r.DocumentID
	}
	vecByID := make(map[string]documentSearchResult, len(vectorHits))
	vecIDs := make([]string, len(vectorHits))
	for i, r := range vectorHits {
		vecByID[r.DocumentID] = r
		vecIDs[i] = r.DocumentID
	}

	fusedIDs := reciprocalRankFusion(ftsIDs, vecIDs)
	out := make([]documentSearchResult, 0, len(fusedIDs))
	for _, id := range fusedIDs {
		if r, ok := ftsByID[id]; ok {
			out = append(out, r)
			continue
		}
		out = append(out, vecByID[id])
	}
	return out
}

// paginateDocumentSearchResults applies offset/limit to an already-fused,
// already-ordered list - the pagination step hybridDocumentSearch's own doc
// comment promises runs after fusion, never before it. Always returns a
// non-nil slice (empty rather than nil) so a caller's JSON encoding is "[]",
// never "null".
func paginateDocumentSearchResults(fused []documentSearchResult, offset, limit int) []documentSearchResult {
	if limit <= 0 || offset >= len(fused) {
		return []documentSearchResult{}
	}
	end := offset + limit
	if end > len(fused) {
		end = len(fused)
	}
	out := make([]documentSearchResult, end-offset)
	copy(out, fused[offset:end])
	return out
}

// ── query-embedding cache ───────────────────────────────────────────────

// documentQueryEmbedCacheCapacity bounds the query-embedding LRU below.
// Debounced keystrokes from one operator's own search box are the entire
// point - 32 distinct in-flight queries is far more than a single operator
// ever has open at once.
const documentQueryEmbedCacheCapacity = 32

// documentQueryEmbedCacheKey identifies one cached query vector: the
// embedding model and dimensions it was computed under, plus the raw query
// string itself. Keying on model and dimensions, not just the query text,
// means a settings change to either invalidates every cached vector rather
// than serving one computed under a different model or a different
// Matryoshka truncation back at the new setting.
type documentQueryEmbedCacheKey struct {
	model string
	dims  int
	query string
}

// documentQueryEmbedCache is a hand-rolled LRU: a map for O(1) lookup, a
// slice recording recency order (oldest first). 32 entries is far too small
// to justify a dependency, and this package already writes its own bounded
// containers by hand at this scale (SearchVector's own `best` map,
// documents_store.go). Guarded by its own mutex - reached from concurrent
// HTTP handler goroutines, one per in-flight search.
type documentQueryEmbedCache struct {
	mu      sync.Mutex
	entries map[documentQueryEmbedCacheKey][]float32
	order   []documentQueryEmbedCacheKey
}

func newDocumentQueryEmbedCache() *documentQueryEmbedCache {
	return &documentQueryEmbedCache{entries: map[documentQueryEmbedCacheKey][]float32{}}
}

// documentQueryEmbedCacheInstance is the one process-wide query-embedding
// cache, shared by every hybridDocumentSearch call regardless of caller
// (HTTP handler or Mate's tool) - the whole point is that two callers typing
// the same search moments apart share a vector rather than each paying for
// their own.
var documentQueryEmbedCacheInstance = newDocumentQueryEmbedCache()

// touch moves key to the most-recently-used end of c.order. Callers hold
// c.mu.
func (c *documentQueryEmbedCache) touch(key documentQueryEmbedCacheKey) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	c.order = append(c.order, key)
}

func (c *documentQueryEmbedCache) get(key documentQueryEmbedCacheKey) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	vec, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.touch(key)
	return vec, true
}

// put records vec under key, evicting the least-recently-used entry first
// if the cache is already at documentQueryEmbedCacheCapacity and key is new.
func (c *documentQueryEmbedCache) put(key documentQueryEmbedCacheKey, vec []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.entries[key]; exists {
		c.entries[key] = vec
		c.touch(key)
		return
	}
	if len(c.order) >= documentQueryEmbedCacheCapacity {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
	c.entries[key] = vec
	c.order = append(c.order, key)
}

// cachedQueryEmbedding returns query's embedding under model/dims, from the
// cache when present, otherwise via one openRouterEmbeddings call (a batch
// of one) bounded by documentsQueryEmbedTimeout. doer nil falls back to
// openRouterHTTPClient - see documentSearchParams.Doer's own doc comment.
func cachedQueryEmbedding(ctx context.Context, doer openRouterDoer, apiKey, model string, dims int, query string) ([]float32, error) {
	key := documentQueryEmbedCacheKey{model: model, dims: dims, query: query}
	if vec, ok := documentQueryEmbedCacheInstance.get(key); ok {
		return vec, nil
	}

	if doer == nil {
		doer = openRouterHTTPClient
	}
	embedCtx, cancel := context.WithTimeout(ctx, documentsQueryEmbedTimeout)
	defer cancel()
	resp, err := openRouterEmbeddings(embedCtx, doer, apiKey, openRouterEmbeddingsRequest{
		Model:      model,
		Input:      []string{query},
		Dimensions: dims,
	})
	if err != nil {
		return nil, err
	}

	// openRouterEmbeddings guarantees exactly len(Input) vectors, ordered by
	// each one's own Index - resp.Data[0] is this call's one and only input.
	vec := resp.Data[0].Embedding
	documentQueryEmbedCacheInstance.put(key, vec)
	return vec, nil
}
