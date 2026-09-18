# ADR 0108: Search by Meaning, Not Just Words

## Status

Accepted (2026-09-18). Builds the phase [ADR 0106](0106-documents-in-the-binary.md)
described and deferred ("Embeddings, deferred"), and changes two things that
section had planned. See Decision.

## Context

ADR 0106 shipped the document library with FTS5 as its only retriever.
FTS5 is a good floor: it needs no account, no connection and no money, it
answers instantly, and it is exact. It is also literal. A search for
"how often should I service the engine's cooling system impeller" finds
nothing in a manual that says "raw water pump: inspect the rubber vanes for
cracking and replace the assembly every 500 hours", because the two share no
word that matters. That is measured, not hypothetical: on the fixture pair
used to verify this work, FTS5 returned zero hits for that question, and the
vector search ranked the right manual first at 0.567 cosine against 0.215 for
the unrelated invoice.

The constraint that killed `sqlite-vec` in 0106 has not moved. The driver is
`modernc.org/sqlite`, builds are `CGO_ENABLED=0`, and one target is armv7,
so a loadable SQLite extension is still impossible. What did need checking
was the other end: whether OpenRouter has an embeddings endpoint at all,
since its documentation does not list one. It does.
`POST /api/v1/embeddings` takes the same headers as chat completions,
accepts an array of inputs, honours a `dimensions` parameter, and returns
`usage.cost` without the `usage.include` option chat completions needs. That
was probed against the live API before any of this was designed, and the real
response is committed as `backend/testdata/openrouter_embeddings.json`.

## Decision

### One table, no new stage, no new column

`document_chunk_embeddings` holds one vector per chunk, keyed by `chunk_id`
with `ON DELETE CASCADE`. That single foreign key is the entire invalidation
story. Every path that changes a chunk's text - `ReplaceChunks` for a
re-extract or a fresh OCR pass, `rebuildMetaChunkTx` for a title, tag, folder
or summary edit - deletes and reinserts the chunk row rather than updating
it, so the old vector goes with the old text. There is no "is this embedding
still current" check anywhere, because there is nothing left for it to check.

ADR 0106 planned a fourth stage on `documents.stage`, after `enrich`. That is
dropped, for two reasons. The documents schema has now shipped, and
`stage`'s CHECK constraint cannot be extended without rebuilding the table,
which this repo's one-operator policy says not to build ceremony for. More
importantly a stage would be the wrong shape: a crash mid-embed would resume
by re-running the *paid* enrich stage, buying a second OCR pass to get back
to where it already was.

Instead the work is found by query. A chunk with no `document_chunk_embeddings`
row for the currently configured model needs embedding. That is idempotent,
survives a crash untouched, needs no bookkeeping of its own, and is
automatically right after a re-extract or a metadata edit. Changing
`assistant.embedding_model` is self-correcting for the same reason: rows
under the old model stop counting, and the library re-embeds itself.

### Vectors, and what they are

Little-endian float32 BLOBs, L2-normalised on write, so cosine similarity is
a plain dot product with no division at query time. Little-endian rather than
native order because the database file moves between the boat's armv7 box and
an amd64 laptop freely, and ADR 0106's promise is that the backup unit is one
folder and one file.

512 dimensions rather than `text-embedding-3-small`'s native 1536. The whole
index is scanned in Go, and that model is trained with Matryoshka
representation learning, so a vector truncated to 512 is a usable embedding
rather than a degraded one. At 512 dimensions it also comes back already
unit-length (measured 0.999879), though normalising on write is kept anyway
because other models do not.

Search is a linear scan, decoding one row at a time and keeping at most
`limit` documents. No index, no approximate nearest neighbour. One boat's
library is small enough that scanning a few thousand vectors costs less than
maintaining a structure to avoid it, and the bounded scan is what keeps the
armv7 box from allocating the whole index to answer one query.

### A second tier on the indexer, behind the first

`Run` tries `processOne` first and only reaches for `processEmbedBatch` when
there is no pending document to index. Indexing wins deliberately: a document
nobody can find by keyword yet is a worse state to leave sitting than one
that is merely not yet searchable by meaning, so a long backfill can never
starve a fresh upload of its first pass.

One batch is up to 64 chunks in a single call, bounded at two minutes. An
upstream failure backs the loop off a minute and is logged, but never marks a
document failed. A document whose text is already in FTS5 is not broken
because its vectors are late, and a red badge over that would hide a document
that searches perfectly well.

A batch that keeps failing halves on each attempt, so whichever chunk the
upstream will not accept ends up alone, and a chunk that fails three times
alone is set aside. Without that, one unacceptable input stops every chunk
behind it from ever being embedded, silently, with nothing but a log line
every sixty seconds to say so. The set-aside list lives in memory only: a
restart gives every skipped chunk another go, which is right when the cause
was a provider having a bad day rather than the chunk itself.

A backfill also has to stop when it cannot continue. Switching Mate off or
removing the key mid-run ends it with the reason recorded, rather than
leaving it running forever against a queue nothing will ever drain.

### Consent, and a backfill that keeps no state

ADR 0106's consent rule carries over unchanged. Automatic embedding covers
documents whose `enrich` flag is set, which is the same upload-time consent
that already sent their text to OpenRouter for OCR and summarising. Embedding
that same text discloses nothing new.

Every other document - anything uploaded while Mate was off - waits for
`POST /api/documents/embeddings/backfill`. Making that call is itself the
consent, exactly as 0106 already treats an explicit reindex, and the panel's
confirmation says so in as many words: how many chunks, roughly how many
tokens, that it goes to OpenRouter, and that OpenRouter bills for it.

The backfill keeps no state on disk, and that is a consequence of finding
work by query rather than a gap. A reboot mid-backfill simply stops it;
pressing the button again resumes where it left off, because "what is left"
was never a flag to lose. `?dry_run=1` reports the counts and a token
estimate and starts nothing. It reports no dollar figure: the price per token
belongs to the model, not to this code, and a number invented here would go
stale silently.

### Reciprocal rank fusion, paged afterwards

One entry point answers both the search box and Mate's `search_documents`
tool, so an agentic search and a typed one cannot drift into two notions of
the best match. FTS5 always runs. When an embedding model is configured the
query is embedded too, and the two ranked lists are fused with reciprocal
rank fusion at k=60, the constant from the original paper, which flattens the
gap between a rank-1 and a rank-3 hit so neither retriever's confidence
dominates the other's.

Each side contributes a pool of at least 50 documents, more when the caller
asked for a page wider or deeper than that, and the caller's offset and limit
are applied to the fused ranking afterwards, never passed down to either
retriever. Fusing only `limit` results per side would make page two depend on
which documents happened to survive page one's cut. A document found by both
keeps the FTS result object, because its snippet carries the match markers
the panel renders as highlights where the vector side's is a plain excerpt.

### Degrading loudly

A search response carries `mode`, either `fts` or `hybrid`, and
`semantic_problem` when semantic search was configured and could not run: a
failed query embedding, or a corrupt vector row. The keyword results are
returned intact alongside it.

This is worth stating plainly because it looks, at a glance, like the masking
fallback AGENTS.md forbids. It is the opposite. The operator gets a real,
complete answer to "what matches these words" and is told, in the same
response, exactly what is missing from it and why. The alternative - failing
the whole request - means one corrupt blob takes the library's keyword search
down with it. What is still a hard error is a failure that makes the search
meaningless rather than partial: an FTS error, an unknown folder id, a broken
settings or secrets read.

Semantic search merely being switched off says nothing at all. That is a
setting, not a fault, and an operator who left it off is not owed an error
message on every search.

### The query embedding: cached, and bounded at eight seconds

Every search with semantic search on buys a query embedding. A 32-entry LRU
keyed by model, dimensions and the query string means a debounced search box
does not buy one per keystroke, and the call is bounded at eight seconds
because a boat's uplink dies mid-request as a matter of routine. Past that
the search degrades to keyword-only rather than hanging.

### Cost, recorded per document

One batch can span several documents, and OpenRouter bills the call, not the
input, so a batch's cost is split across its documents in proportion to the
characters each contributed. It is added with `AddEmbedCost`, never
`AddIndexCost`: the latter also sets `index_model`, and an embedding pass must
not overwrite the name of the model that did the OCR with the name of the one
that did the vectors.

The numbers are small enough to be worth stating so nobody budgets for them.
`text-embedding-3-small` bills about $0.02 per million tokens. Embedding two
short documents, four chunks in total, cost $0.0000013. A query embedding
costs about $0.00000002.

## Rejected

**A fourth `embed` stage.** See above: the CHECK constraint, and the worse
crash-resume behaviour of paying for OCR twice to finish a free step.

**A consent column on `documents`.** It would have meant an `ALTER TABLE` on
a shipped schema to record something the existing `enrich` flag plus an
explicit operator action already express.

**An approximate nearest neighbour index.** Nothing to gain at this library's
size, and every ANN structure is another thing to keep in sync with a chunk
table that deletes and reinserts rows constantly.

**A dollar figure in the dry run.** Would require either hardcoding a price
that goes stale or a second network call to the models endpoint, to tell the
operator something the confirmation already tells them qualitatively.

**Failing a search when a vector row is corrupt.** Discussed above. Keyword
search keeps working, and the response says what did not.

## Consequences

Positive:

- A question phrased in the operator's own words finds the manual page that
  answers it, which is the way anyone actually searches their own boat's
  paperwork.
- Mate's `search_documents` gets the same improvement without its own code
  path, so what Mate finds and what the panel finds stay the same thing.
- Nothing new to back up, deploy or keep running. The vectors live in the
  database file that was already the backup unit.
- Turning it off is one blank setting, and the library keeps working.

Negative:

- A search with semantic search on is a network round trip, so it is slower
  than FTS5 alone and can fail where FTS5 could not. The cache and the
  eight-second bound limit that; they do not remove it.
- Every consented document's text now leaves the boat twice: once for OCR and
  summarising, once for embedding. The second trip is far cheaper, but it is
  a second trip, and the Consent section is what makes it one the operator
  agreed to.
- Changing the embedding model silently re-embeds the whole library over the
  following hours, at the operator's expense. That is the self-correcting
  behaviour working as intended, but it is spend nobody pressed a button for.

## Related

- [ADR 0106](0106-documents-in-the-binary.md) (documents in the binary): the
  library this completes, and the source of the consent rule, the chunk
  table and the meta chunk this reuses.
- [ADR 0093](0093-onboard-assistant-over-openrouter.md) (onboard assistant
  over OpenRouter): the hand-rolled client the embeddings call extends, and
  the settings block `embedding_model` joins.
- [ADR 0065](0065-inventory-records-in-the-binary.md) (inventory records in
  the binary): §5.1, narrowed for documents by 0106 and unchanged here.
