# ADR 0106: Documents in the Binary

## Status

Accepted (2026-09-18). Builds the scope [ADR 0065](0065-inventory-records-in-the-binary.md)
promised and left unbuilt ("behind it document management, are built here").
Narrows ADR 0065 §5.1 for documents specifically - see the Consent section
below.

## Context

ADR 0065 drew inventory tracking and document management into the same
binary as everything else, on the same one-operator, one-box reasoning:
three applications means three deployments, three backups, three auth
models, and a boat has one of each. It scoped inventory in full and left
documents as "behind it" - promised, not designed.

The first real use of a document store turned out not to be a documents
panel at all. It's attaching a file to a question for Mate: a haulout
invoice, a scanned page from an engine manual, a photo of a part number.
Both the dashboard's own way to list, search and browse the library (a
Documents panel) and an attach control in Mate's own composer wait on the
Mate streaming work already landing on `assistant-thread.tsx`, since
building either surface against the component that work is about to
replace would mean redoing it twice. So this ADR is the backend in full:
the store, the indexer, the HTTP API, and the message-attachment plumbing
and two tools Mate itself already uses today, over its own API, ahead of
either frontend surface existing.

Three decisions were made before any code, on 2026-09-17: one pure-Go PDF
library rather than shelling out or reaching for CGO, paid enrichment steps
running automatically once Mate is switched on rather than a per-document
button, and FTS5 full-text search first with embeddings as a later phase.
Folders were decided to be virtual, living only in SQLite, so the bytes on
disk can stay flat and dumb. The reasoning behind each of those follows.

## Decision

### A flat folder plus SQLite

Every uploaded file is written once, named by the lowercase hex of its
SHA256 with no extension, into one flat directory
(`cacheFilePath("DOCUMENTS_DIR", "data/documents")`). Identical bytes are
one file: a second upload of the same content is detected under a
per-hash lock before it ever reaches disk, and returns the existing
document with `duplicate: true` rather than a second copy and a second OCR
bill. `documents.sqlite`
(`cacheFilePath("DOCUMENTS_DB_PATH", "data/documents.sqlite")`) carries
everything else: metadata, folders, tags, chunks, and the search index.

The row a client actually addresses - in a URL, a tool call, an attachment
record - is a uuid, not the hash. The hash is what makes the store
content-addressed and lets it dedupe; the uuid is what stays a stable,
opaque handle regardless of how the bytes underneath are named or stored.

The store opens with no WAL. Every other recent store in this codebase
(`tile_cache.go`, `nearby_contacts.go`) uses one; this one deliberately
doesn't, because the whole point of the design is that the backup unit is
exactly one `.sqlite` file plus one folder of hash-named files. A WAL
sidecar (and the `-shm` file next to it) would mean a backup taken mid-write
needs both extra files understood and captured too, which is precisely the
complication a flat single-file design exists to avoid.

### Chunks from day one, one FTS5 index, and the meta chunk

`document_chunks` exists from the first commit, well before there is
anything but keyword search to rank chunks for. Each row is one span of a
document's text: `source` is `meta`, `local` (extracted text) or `ocr`
(Mate's transcription), `seq` orders them within a document, and
`page_start`/`page_end` carry PDF page numbers where they exist. This
wasn't free - it meant designing chunk boundaries and a chunking pass before
there was any consumer but a search box - but it means the day embeddings
land (see below), the unit to hang a vector off already exists and already
has the right shape: one row per embedding, one embedding per chunk, joined
straight onto `document_chunks` by its `id`.

`document_chunks_fts` is a single FTS5 table spanning the whole library,
not one per document: an *external-content* table
(`content='document_chunks', content_rowid='id'`), so the indexed text
lives exactly once, in `document_chunks` itself, and three triggers
(insert/delete/update) keep the FTS b-tree in sync rather than the
application writing to both places itself. `porter unicode61
remove_diacritics 2` tokenizes it.

Every document gets one more chunk than its content alone would produce: a
**meta chunk**, `seq` 0, `source` `meta`, holding title, filename, the full
folder path, every tag (operator and suggested together), summary, and
notes, concatenated into one text field with the title also duplicated into
FTS5's `heading` column. `bm25(fts,1.0,2.0)` weights a heading hit twice a
body hit, so a document whose *title* matches "impeller" outranks one that
merely mentions it once in passing, through the same ranking pass that
scores every other chunk - there's no separate metadata-search code path to
keep in sync with the body one. Renaming a document, moving it, editing its
tags, or renaming or moving a folder above it all rewrite the meta chunk
(`rebuildMetaChunkTx`, and `rebuildMetaChunksUnderFolderTx` for every
document under a renamed or moved folder) in the same transaction as the
edit, so the search index is never one commit behind what a browse view
would show.

### Virtual folders, not real directories

`document_folders` is its own table: uuid id, nullable `parent_id` (`NULL`
is root), a name unique per parent case-insensitively. A document's
`folder_id` is a plain foreign key. Moving a document from one folder to
another is one `UPDATE`; the file on disk never moves, is never renamed,
and never learns it has a folder at all.

Real directories - one tree on disk mirroring the folder structure - were
the obvious alternative and were rejected for three concrete reasons:

- **Dedupe.** One file can only usefully be filed in one place. A real
  directory tree would need hardlinks (fragile across the bind-mounted
  volumes this project already runs on) or symlinks (an extra failure mode:
  a dangling link the moment either side is touched by hand) to let the
  same bytes appear "in" more than one folder, or under more than one
  filename, for no reason a virtual foreign key doesn't already solve for
  free.
- **Drift.** A tree that must always agree with a table invites the two
  disagreeing: a folder renamed on disk outside the app, a file moved by
  hand, a directory deleted out from under a row that still points at it.
  That's exactly the disk/database mismatch the boot sweep
  (`sweepDocumentsDir`) already exists to catch and *report* rather than
  silently reconcile, per this codebase's fallback policy - virtual folders
  simply remove an entire category of that mismatch before it can occur.
- **Moves that touch the filesystem.** Renaming or moving a folder with
  real directories means renaming every file under it on disk, an operation
  that can fail partway through a large tree and leave it inconsistent. An
  `UPDATE document_folders SET parent_id = ?` is one atomic statement
  regardless of how many documents sit underneath.

Deleting a folder with anything still in it fails
(`errFolderNotEmpty`) - there is no recursive delete. Emptying a folder is a
deliberate, visible act, not something that happens as a side effect of
deleting its parent. `MoveFolder` rejects moving a folder into itself or
one of its own descendants (`errFolderCycle`), checked by walking the
destination's ancestor chain with a recursive CTE looking for the folder
being moved.

### The PDF library, and what it beat

CGO is off across every build target, armv7 included, the same constraint
that already ruled out `sqlite-vec` (see Embeddings, below). That alone
rules out any PDF library backed by a C renderer. Four pure-Go and
CGO-based alternatives were looked at and rejected before landing on
`github.com/ledongthuc/pdf`:

| Library | Rejected because |
| --- | --- |
| `pdfcpu` | has no text-extraction API at all - it's a manipulation/repair tool, not a reader |
| `rsc.io/pdf` | unmaintained; the upstream repository has had no real activity in years |
| `go-pdfium` | a WASM build of Pdfium, roughly 5 MB, and armv7 has no WASM JIT available to it - every page is interpreted, slowly |
| `unipdf` / `go-fitz` | `unipdf` is commercially licensed past a small free tier; `go-fitz` is CGO bindings onto MuPDF, ruled out with every other CGO option |

`ledongthuc/pdf` is pure Go, permissively licensed, and reads one page at a
time rather than loading a whole document's parsed structure up front.

It also panics by design on a malformed content stream rather than
returning an error - the library's own `NewReaderEncrypted` carries an
internal `recover`, but a single page's interpretation does not always. So
`extractPDF` wraps three layers of its own: `openPDFReader`'s own `recover`
around `pdf.Open` itself (belt and braces beyond the library's own), a hard
cap on page count (`maxPDFPages`, 2000) checked before any page is touched,
and `extractPDFPage`'s own per-page `recover`. A malformed page fails only
that page; a document fails as a whole only when *every* page failed
(`numPages > 0 && len(failedPages) == numPages`). A partial failure - some
but not all pages unreadable - is recorded as `FailedPages`/`FirstPageErr`
on the extraction result and written into `documents.error` as "N of M
pages unreadable: `<first error>`", surviving onto the finished document
even once it reaches `indexed`. The original version of this code simply
skipped a failed page's text and moved on, which meant a hole in what an
operator believed was a complete manual with nothing on screen ever saying
so - caught in review before it shipped, not after.

### The database as the queue

There is no queue library and no separate queue table. `documents.status`
(`pending`/`indexed`/`failed`) and `documents.stage`
(`extract`/`enrich`/`done`) are the whole mechanism. `NextPending` is
`ORDER BY created_at ASC, id ASC LIMIT 1` - the oldest pending row, full
stop. One worker (`documentIndexer.Run`) processes one document at a time;
a boat's own upload rate never justifies concurrency, and the store's
single-connection pool (`db.SetMaxOpenConns(1)`) already serialises every
write regardless, so concurrent workers would only contend with each other
for nothing.

`Run` loops: pop the oldest pending row, run it through whichever stage
it's actually at, and go straight back for the next one while there's more
work; once there's nothing pending, it blocks on a buffered-1 `wake`
channel or context cancellation. An upload or an explicit reindex calls
`Wake()` to skip the wait immediately, a non-blocking send so it never
matters whether the worker is mid-document or already waiting.

A document left `pending` by a crash or a restart needs no separate resume
step, because the row itself *is* the queue position: the next boot's
`Run` starts calling `NextPending` in a loop exactly as it always does, and
picks the same row straight back up. A row already at `stage=enrich` when
the process died is not re-extracted - its local chunks and markdown are
already committed - only the enrich stage runs again.

A store error (as opposed to a document-specific failure, which is
recorded on the row and never retried on its own) backs off a minute
before looking at the queue again, rather than spinning the log; a `Wake`
still cuts that wait short.

### Consent, and narrowing ADR 0065 §5.1

ADR 0065 §5.1, written for per-photo label reading, is explicit: "no
background pass over the library... no photo leaves the boat without the
operator asking for that photo to be read." Every document's enrich stage -
OCR, summary, suggested tags - runs automatically in the background the
moment it's queued, which is precisely what that rule forbade. This ADR
narrows §5.1 for documents rather than discarding it: **switching Mate on
is the ask.** `enrich` is set once, at upload time, from
`checkAssistantReadiness` evaluated at that exact moment
(`uploadDocumentHandler`) - it reflects the operator's own current
configuration, not a default baked into the feature. A document uploaded
while Mate was off gets `enrich=false` and is never revisited later on its
own; nothing in the indexer goes back over an already-indexed document
unless the operator explicitly calls **Reindex**, which re-checks readiness
at that moment and is treated as consent the same way the original upload
was (`MarkReindex`, `reindexDocumentHandler`) - because the operator may
have turned Mate on, or off, since the document first landed.

A second, cost-shaped consent gate sits on top of that: a scanned PDF over
200 pages (`documentsDefaultOCRPageCap`) fails outright with the page count
and an estimated cost - about $0.40 at the nominal $0.002/page figure used
only to word that one message - rather than running. Proceeding needs an
explicit Reindex, which sets `force_ocr` and is what actually lifts the
cap; `force_ocr` also overrides the ordinary "does this PDF's text layer
look thin enough to need OCR" heuristic, since a reindex exists specifically
to force OCR even over a local text layer that already looked adequate.

Every enrich call's real cost - `usage.cost` from OpenRouter's own reply,
never a local estimate - is added to the document's running
`index_cost_usd`, not overwritten: a document reindexed more than once
shows everything ever spent on it, not just the latest pass.

One piece of §5.1 does *not* carry over unchanged: label reading's "it
proposes, the operator confirms" held every extracted field in an editable
form until the operator explicitly saved it. A document's suggested title
and tags are written straight into the row (`SetSuggested`) the moment
enrichment finishes, live and searchable immediately - there is no
held-back confirmation step. What does carry over is that they stay
*distinguishable*: `document_tags.source` marks a tag `operator` or
`suggested`, an operator's own tag of the same text is never demoted, and a
suggested title only ever fills a blank one, never overwrites an operator's
own. The confirm step became "labelled, not blocking" rather than
disappearing outright.

### Non-streaming OCR, and the shape it actually returns

OpenRouter's `file-parser` plugin - the one that runs OCR over an uploaded
PDF - only returns its result (`message.annotations`) on a non-streaming
completion. [ADR 0105](0105-mate-streams-its-answer.md) made the existing
client always stream, so a second entry point,
`openRouterChatCompletionOnce`, was added sharing the same error handling
as the streaming call, used only by the document indexer.

Per this codebase's rule to verify a fixture against real traffic rather
than an assumed shape, a spike ran one genuine OCR call against a live key
before any code committed to a structure, captured as
`backend/testdata/openrouter_document_pdf_2p.json`. The real shape: one
file annotation per response, whose `content` array holds one text part
reading `<file name="...">`, then exactly one text part per page, then a
closing `</file>` part. `ocrPagesFromAnnotations` slices everything
strictly between the two sentinel parts and numbers what's left 1-indexed -
page *N* is the *N*th part in that middle slice, nothing more structured
than that.

The model's own `{title, summary, tags}` answer to the same request rides
in the ordinary `message.content` of that same response, and arrives
wrapped in a ` ```json ` fence more often than not despite the prompt
explicitly asking for none - `stripDocumentEnrichJSONFence` strips exactly
that one wrapping and nothing else; anything that still doesn't parse as
strict JSON after that fails the document outright (truncated to 200
characters in the failure message) rather than being coerced or
re-interpreted. Tags are capped at 8 in code
(`documentSuggestedTagsCap`) even though the prompt asks for "up to 5" -
a captured real reply asked for five tags came back with nine anyway. The
cap exists because the model's own stated limit isn't reliable, not because
five was the wrong number to ask for.

Real per-document costs, measured 2026-09-17 against the operator's own
OpenRouter account and committed as test fixtures, not estimates: a
one-page scanned invoice cost $0.0023, the same document split across two
pages cost $0.0046, and a photographed receipt read through the image
branch cost $0.0012.

### `assistant.document_model`, apart from the chat model

A new setting, `assistant.document_model`, defaults to
`google/gemini-2.5-flash`. It is deliberately independent of
`assistant.model`, the interactive chat model: the document model runs
unattended, on every single upload, with nobody watching to catch a bad
answer before it's spent - so it has to be cheap in a way a chat model
answering one question at a time doesn't, and it has to see images, which
an operator's chosen chat model may not.

A blank document model is not silently backed by the chat model, or by any
hardcoded fallback: `documentEnrichReadinessProblem` treats an empty
`DocumentModel` as its own distinct failure ("No document model is
configured. Set one in Settings → Assistant."), applied identically to
upload's consent decision, reindex's, and the enrich stage's own final
check before it spends anything - one function, three call sites, so
none of them can drift into disagreeing about whether the document model is
actually usable. It layers on top of `checkAssistantReadiness`'s own
`Problem` (assistant off, no key, no chat model configured) rather than
replacing it, and an existing chat-level problem always wins: chat's own
readiness - answering an ordinary question - must never depend on whether a
document model happens to be configured, since the two are genuinely
separate settings answering separate questions.

### Mate sees text, not images

Two tools reach the document library: `search_documents` (an FTS5 query,
sanitised through the same `ftsMatchQuery` the HTTP search endpoint uses,
optionally scoped to a folder given as a path such as `Receipts/2026` and
resolved through `ResolveFolderPath`, tag-filterable, capped at 10 results)
and `read_document` (every chunk of one document from a given point
onward, shrinking by halving the same way `executeReadManual` already
does when a reply runs long). A document attached directly to a message
gets a text preamble ahead of it in that turn's history
(`assistantAttachmentBlock`): its id, filename, MIME type, page count and
status, and - only on the *most recent* user message, never replayed on
every later turn of a long-lived conversation - up to 4000 characters of
its own extracted or OCR'd text, with an explicit pointer telling the model
to call `read_document` for the rest. A document still `pending` says so
plainly rather than fabricating a summary it doesn't have yet. A document
deleted after being attached renders as `[attachment deleted: <filename>]`,
the filename copied into `message_attachments` at attach time rather than
looked up live, so a conversation's history never loses a name it already
showed the operator.

No image content block is ever sent to the chat model for a document, in
this first version. The enrich stage's own vision call already turned a
scanned page or a photographed receipt into markdown - an OCR chunk - which
is exactly what the preamble or `read_document` hands back as plain text.
Sending the original image a second time, to the chat model this time,
would pay the vision cost of the same read twice for no benefit the OCR
text doesn't already provide.

### Serving

`GET /api/documents/:id/content` uses `http.ServeContent`, which handles
Range requests and conditional `304`s without any code of this feature's
own. Every response carries `X-Content-Type-Options: nosniff`,
`Content-Security-Policy: sandbox` (so a PDF or an SVG served from this
origin can never execute script or navigate the top frame it's embedded
in), an `ETag` set to the document's own sha256 (identical bytes always
produce the identical tag, which is what makes `Cache-Control: private,
max-age=31536000, immutable` an honest claim rather than an optimistic
one), and a `Content-Disposition` built with `mime.FormatMediaType`.

Whether that disposition is `inline` or `attachment` comes from an
**allow-list** (`documentInlineMIMEAllowList`: PDF, PNG, JPEG, GIF, WebP,
plain text, Markdown, CSV, JSON), not a deny-list. SVG and HTML - either of
which a browser will happily execute in this origin if served inline - are
always `attachment` regardless of the `?download=` query, because a
deny-list only stays safe for as long as every dangerous type is
remembered and kept current; an allow-list fails safe by construction for
whatever type nobody thought to list.

A document row whose file is missing from disk is a `500`, not a `404`:
the id the caller supplied was correct, and a missing file is a genuine
server-side problem - the same disk/database drift the boot sweep already
logs - not something the caller did wrong. The route is added to
`noCompressRoutePatterns` (`compression.go`): gzip middleware buffering a
multi-hundred-megabyte PDF in memory, or breaking Range support on it, is
exactly what that skip list already exists to prevent for other binary
payloads.

### Embeddings, deferred (landed in ADR 0108)

`sqlite-vec` needs to load as a SQLite extension, which needs CGO, which is
off. Semantic search is therefore a later phase, not part of this one, but
the schema already shipped with it in mind (see Chunks, above). The plan
for when it lands: a `document_chunk_embeddings` table keyed by `chunk_id`
(referencing `document_chunks` `ON DELETE CASCADE`, so a chunk that stops
existing can never leave an orphaned vector behind), storing float32,
little-endian, L2-normalised vectors as BLOBs - normalised so a plain dot
product is already a cosine similarity, nothing fancier needed. Search does
a brute-force scan in Go into a bounded top-N heap; no index, no
approximate nearest-neighbour structure, because one boat's whole document
library is small enough that scanning a few thousand chunk vectors linearly
costs nothing worth building an index to avoid. That ranking is merged with
FTS5's own through reciprocal rank fusion (k=60) rather than either running
alone.

That is what [ADR 0108](0108-search-by-meaning.md) built, on the same day,
and it holds to this plan apart from two changes. There is no separate
`embed` stage on `documents.stage`: the work is found by query instead, since
a chunk with no vector for the current model is exactly the queue, and a
stage would have made a crash mid-embed resume by re-running the paid enrich
step. And OpenRouter turned out to have an embeddings endpoint of its own,
which this section had not assumed either way.

## Rejected

### Paperless-NGX

Considered as an alternative to building any of this at all, and set aside
for the same reason ADR 0065 gave for folding inventory and maintenance
back into this binary: Helmcentral already committed to one deployment, one
backup, one auth model on one box. Standing up a second application for
documents specifically would undo exactly what that consolidation was for.

Left as an open follow-up, with three shapes on the table and none chosen:
Helmcentral watching an inbox folder Paperless also writes to, Helmcentral
writing processed uploads into Paperless's own consume folder, or the two
syncing over Paperless's REST API. Each answers "who owns the record" and
"who pays for OCR" differently, and deciding needs its own ADR once there's
an actual reason - a second household member's own Paperless instance, say -
to build it.

## Consequences

Positive:

- A chunk, a folder, a search hit and an enrichment cost are all designed
  once, correctly, rather than bolted onto a documents-panel-first design
  after the fact - Mate's attachment flow, the eventual Documents panel,
  and embeddings when they land all read the same rows.
- Folders being virtual means every folder operation (create, rename, move,
  delete-when-empty) is one transaction with no filesystem step that can
  partially fail.
- A crash or restart costs nothing beyond what it directly interrupted:
  `NextPending` resumes the queue with no separate recovery code, because
  the row's own `status`/`stage` already is the queue's state.

Tradeoffs:

- Documents leave the boat whenever Mate is switched on and a document is
  uploaded, reindexed, or attached to a question - the consent model in
  this ADR is a real, deliberate loosening of "nothing leaves the boat
  unasked" to "an ON switch is the ask," not a cosmetic one, and it's worth
  restating plainly rather than only in the fine print of §5.1's narrowing.
- The documents folder - flat, hash-named, holding every byte ever uploaded
  - will become the largest thing in the state directory, the same way
  ADR 0065 §7 already flagged the inventory photo library would.
  `docs/reference/configuration.md`'s backup guidance needs `data/documents/`
  named alongside `data/documents.sqlite` as one unit, the same one-file-
  plus-one-folder shape this ADR chose for exactly that reason.
- A crash mid-OCR is paid for twice. Extraction, once committed, is never
  re-run; but an interrupted OCR call is not resumable mid-flight, so the
  document is left `pending`/`enrich` (or failed outright once its timeout
  elapses) and the next attempt - the next boot's indexer, or an operator's
  own Reindex - bills OpenRouter again for the same pages. There is no
  partial credit and no refund path; an OCR call is billed on completion,
  full stop.

## Related

- [ADR 0065](0065-inventory-records-in-the-binary.md) (inventory records in
  the binary): promised documents as follow-on scope; this ADR is that
  scope, and narrows its §5.1 as described above.
- [ADR 0093](0093-onboard-assistant-over-openrouter.md) (onboard assistant
  over OpenRouter): the hand-rolled OpenRouter client this ADR's non-
  streaming call, file/image content blocks, and `document_model` setting
  all extend.
- [ADR 0105](0105-mate-streams-its-answer.md) (Mate streams its answer):
  the reason a second, non-streaming completion path was needed at all -
  the shared client now always streams by default.
- [ADR 0108](0108-search-by-meaning.md) (search by meaning, not just
  words): the embeddings phase this ADR deferred, built on this one's chunk
  table, meta chunk and consent rule.
- [ADR 0096](0096-in-app-manual.md) (in-app manual): `read_manual`, the
  existing tool whose shape `search_documents` and `read_document` follow -
  shrinking a long result by halving rather than failing it outright, and a
  live system-prompt line naming what's available rather than assuming the
  model already knows.
