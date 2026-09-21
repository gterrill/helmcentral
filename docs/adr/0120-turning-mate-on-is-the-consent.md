# ADR 0120: Turning Mate On Is The Consent

## Status

Accepted (2026-09-21). Supersedes the per-note default this ADR names
below, which [ADR 0116](0116-notes-are-documents-and-folders-are-the-manual.md)
§8 shipped and the plan's §8/§9 built the two backfill endpoints against.

## Context

The Documents toolbar carried three status banners, all describing the same
kind of backlog:

- **Embeddings.** "N chunks not yet searchable by meaning" with an **Index
  for semantic search** button (`EmbeddingsStatusRow`,
  `frontend/src/components/documents-panel.tsx`), gated behind a dry-run
  count and a confirm dialog naming the token estimate and the OpenRouter
  bill.
- **Classification.** "N notes not yet classified" with a **Classify**
  button (`NotesBackfillStatusRow`, same file).
- **Enrichment.** "N notes not yet enriched" with an **Enrich for Mate**
  button, its own dry-run/confirm dialog.

Three banners, three buttons, three confirmations, for one operator who has
already decided Mate is worth running on this boat. Each one reads as the
app asking permission again for something it was already told to do.

Two bugs sat underneath the nagging, not just the UX of it:

1. **A note was never eligible for automatic enrichment.**
   `createNoteHandler` (`backend/notes_handlers.go`) set `Enrich: false`
   unconditionally, regardless of whether Mate was configured and ready.
   Every other write path that decides this flag - `uploadDocumentHandler`
   and `reindexDocumentHandler` (`backend/documents_handlers.go`) - used
   `documentEnrichReadinessProblem(readiness) == ""`: enrich when Mate can
   actually do the work, don't when it can't. A note was the one exception,
   on the reasoning (ADR 0116 §8) that a one-tap capture at the helm isn't
   the same deliberate act as an upload. Automatic embedding only ever
   looks at `enrich = 1` documents (`processEmbedBatch`,
   `documents_embed.go`), so a boat that captures far more notes than it
   uploads files accumulated an ever-growing pile of `enrich = 0` chunks
   with no automatic path to searchable.
2. **The classify banner counted the wrong thing, and could never clear.**
   `NoteClassifyBackfillCandidates` (`backend/notes_store.go`) selected
   `note_type_source IN ('', 'auto')` - which is every note that has
   already been classified automatically, not notes waiting to be. A note
   is typed the moment it's captured (`classifyNoteType`,
   `notes_classify.go`, a pure local function with no Mate dependency at
   all) and stays `'auto'` until an operator overrides it. Clicking
   **Classify** reclassified this same set and left `note_type_source`
   exactly where it started, so the banner's count never dropped. There
   was never a real backlog behind it: classification isn't something Mate
   does, and isn't something that gets "left behind."

The operator's decision, stated once, for the whole feature: **turning
Mate on is the consent.** Enabling the assistant, configuring a document
model and (optionally) an embedding model is a deliberate act with its own
cost and cloud disclosure (see the Settings → Assistant copy this ADR also
updates). Once that's done, the system should do the rest on its own.
Repeating the question per note, per document, per feature, is not
extra safety, it's noise the operator has already answered.

## Decision

### A note's enrich flag follows the exact rule an upload already used

`createNoteHandler` now computes `enrich` the same way
`uploadDocumentHandler` and `reindexDocumentHandler` do - Mate ready, or
not. The three call sites had converged on the identical six lines
(`checkAssistantReadiness` then `documentEnrichReadinessProblem`), so that
shared shape is now `documentEnrichFlag(logCtx string)`
(`backend/documents_enrich.go`), one function all three call. A readiness-
check failure (a broken settings file, a secrets-store read error) fails
the note-create request outright, the same fail-fast AGENTS.md's fallback
policy already required of the two document paths - no silent default to
"not enriched" on an infra error. There is no separate per-note consent
step left to build: ADR 0116's "capturing a note is not a deliberate act
the way an upload is" reasoning is retired along with the banner it
justified - the deliberate act already happened, at Settings → Assistant.

### The automatic sweep: `SweepIfReady`

`(*documentIndexer).SweepIfReady()` (`backend/documents_embed.go`) is the
new home for what the three deleted buttons used to do, minus the
button. It runs once at boot (`main.go`, right after the indexer is wired
up) and again every time `updateSettingsHandler` (`backend/signalk.go`)
saves settings successfully - the moment Mate might have just become
ready. It checks readiness itself and, gated independently:

- If `documentEnrichReadinessProblem(readiness) == ""`, it calls the
  existing `EnrichBackfillNotes()` store method (`notes_store.go`, kept
  verbatim - it already did exactly the right whole-library `UPDATE`) to
  flip every `kind = 'note'` row still at `enrich = 0` and requeue it for
  extraction/enrichment.
- If `documentEmbedReadinessProblem(readiness) == ""`, it calls the
  existing `StartBackfill()` (`documents_embed.go`) - the same method the
  deleted **Index for semantic search** button used to trigger, unchanged.
  `StartBackfill` already widens `PendingEmbedChunks`'s scan to every
  document regardless of its own `enrich` flag for the duration of the
  pass; that is precisely the "operator's consent for the rest of the
  library" `StartBackfill`'s own comment already described, just granted
  automatically now instead of by a click.

Both are best-effort and non-fatal: a readiness-check error is logged and
the sweep does nothing this pass, rather than crashing boot or failing the
settings save that triggered it. Nothing is written before the check
fails, so there is nothing to roll back - the next boot or the next
settings save tries again.

Plain uploaded documents (`kind = 'file'`) are deliberately **not** swept
onto `enrich = 1` by this change - see Rejected below for why that's a
narrower, correct scope, not an oversight. `StartBackfill`'s widened
embedding pass still picks up their already-extracted text regardless,
since chunking happens locally at upload time independent of the `enrich`
flag; only the paid OCR/summarise step stays exactly as it always has,
gated on that document's own upload-time or reindex-time consent.

### One surfaced state: failure

The toolbar keeps exactly one line, `MateFailureRow`
(`frontend/src/components/documents-panel.tsx`), replacing both
`EmbeddingsStatusRow` and `NotesBackfillStatusRow`. It renders nothing at
all - not even an empty wrapper - unless `documents.embeddingsStatus`
reports `enabled: true` and a `backfill.last_error`. There is no more
"up to date" quiet line either: a healthy library has nothing to say about
itself, the same way the rest of this app stays silent about things that
are simply working. When an error is present, the line names it in plain
language and offers a button straight to Settings → Assistant
(`onOpenAssistantSettings`, wired in `App.tsx` the same
`requestNavigate('settings', …)` way `AssistantDrawer`'s own "Open Mate
settings" button already is).

`backfill.last_error` is what the embeddings status endpoint already
exposed (`documentBackfillStatus.LastError`, unrelated to this ADR); no new
field was added to surface an enrich-stage failure separately, since the
status endpoint carries nothing to name one document from another for that
stage. If a document's own enrichment fails, its `status`/`error` fields
already say so on its own row and Details page - this line covers the
library-wide embeddings pass only, the one thing left that can fail
without any per-document surface already showing it.

### What was deleted, and what was kept

Deleted (dead once their sole caller was gone, "no dead code kept just in
case"):

- `notesClassifyBackfillHandler`, its two JSON types, and
  `POST /api/notes/classify/backfill` (`main.go`).
- `NoteClassifyBackfillCandidates` (`notes_store.go`).
- `reclassifyNote` and `ReplaceNoteFrontmatter` (`notes_handlers.go`,
  `notes_store.go`) - the classify handler's only caller of each.
- `errNoteChangedUnderfoot` and its `documentErrorStatus` mapping
  (`documents_handlers.go`) - `ReplaceNoteFrontmatter`'s only producer.
- `notesEnrichBackfillHandler`, its two JSON types, and
  `POST /api/notes/enrich/backfill`.
- `NoteEnrichBackfillCounts` and `notesEnrichCounts`
  (`notes_store.go`) - only the deleted handler's dry-run read it.
- `documentsEmbeddingsBackfillHandler`, its two JSON types, and
  `POST /api/documents/embeddings/backfill`.
- `documentIndexerStartBackfill` (`documents_handlers.go`) - the
  package-level wiring for that handler alone; `SweepIfReady` calls
  `idx.StartBackfill()` directly, as a method, not through this var.
- The frontend halves: `dryRunEmbeddingsBackfill`/`startEmbeddingsBackfill`
  (`use-documents.ts`), all four notes-backfill functions and their types
  (`use-notes.ts`), and both confirm `AlertDialog`s.

Kept, because something still calls them:

- `EnrichBackfillNotes` (`notes_store.go`) - `SweepIfReady`'s own call.
- `StartBackfill`/`BackfillStatus` (`documents_embed.go`) - `SweepIfReady`
  and the still-live `GET /api/documents/embeddings` status endpoint.
- `documentIndexerBackfillStatus`/`currentDocumentBackfillStatus`
  (`documents_handlers.go`) - the same status endpoint.
- `documentsEmbeddingsStatusHandler` and `GET /api/documents/embeddings`
  itself - `MateFailureRow`'s only data source, and the poll that already
  drove `use-documents.ts`'s own pending-work refresh loop, untouched by
  this ADR.

## Rejected

### Dropping the `enrichedOnly` restriction in `processEmbedBatch` instead of calling `StartBackfill`

Automatic embedding (`processEmbedBatch`) could either keep scanning only
`enrich = 1` chunks and rely on an explicit backfill to widen it (what
shipped), or drop the restriction so every chunk is always in scope,
backfill or not. The second option is a
smaller diff in isolation but changes the steady-state meaning of a
document's `enrich` flag everywhere, forever - not just at the moment Mate
becomes ready. Calling the existing `StartBackfill` at boot and on
settings-save instead keeps `enrich` meaning exactly what it always has
(this document's own consent to leave the boat), reuses tested machinery
outright rather than touching `processEmbedBatch`'s query, and is
naturally idempotent: calling it when there's nothing pending just finishes
immediately, so there is no harm in calling it unconditionally on every
settings save rather than diffing old-readiness against new.

### Sweeping `kind = 'file'` documents onto `enrich = 1` too

The bug this ADR fixes is specific to notes: `createNoteHandler` hardcoded
`enrich = false`, while `uploadDocumentHandler` already computed it
correctly from readiness at upload time. A `kind = 'file'` document sitting
at `enrich = 0` today is not evidence of a bug, it's a document uploaded
before Mate was ever configured - a real, ordinary state, and the
per-document **Reindex** action (`reindexDocumentHandler`) already exists
as the operator's own deliberate way to ask Mate to read it now. Building a
second, library-wide `EnrichBackfillDocuments` alongside `EnrichBackfillNotes`
for a problem that was never actually observed would be exactly the kind
of speculative surface this codebase's own conventions argue against.
Their text still reaches semantic search through the widened embeddings
sweep above; only the paid OCR/summarise step waits for an explicit
Reindex, as it always has.

### A dismissable failure banner, or a retry button

`MateFailureRow` is read-only: no dismiss, no manual retry. The backfill
pass itself already retries on its own schedule (`documentsIndexerErrorBackoff`);
a manual retry button would just be a way to nudge a queue that is already
running. A dismiss would risk exactly the state the single-operator,
no-installed-base policy argues against: a real, ongoing problem quietly
hidden because someone tapped an X once. The fix for the error is almost
always in Settings → Assistant, which is one tap away already.

## Consequences

Three POST endpoints and the handlers/types behind them are gone from the
API surface; one GET endpoint and the store/indexer methods behind it are
unchanged. `frontend/src/components/documents-panel.tsx` loses two stateful
components, four pieces of `useState`, one `useEffect`, and two
`AlertDialog`s, and gains one small read-only row and one new optional
prop (`onOpenAssistantSettings`). `docs/features/documents.md` and
`docs/features/notes-and-the-manual.md` are updated alongside this ADR
(the Documentation Location Policy's rule: a behaviour change updates both
trees, never the ADR alone) to describe the automatic sweep in place of
the three manual actions. Settings → Assistant's own copy now states
plainly, once, that Mate reads documents and notes and sends their text to
OpenRouter, rather than leaving that disclosure implicit in a button label
three screens away.

## Related

- [ADR 0116](0116-notes-are-documents-and-folders-are-the-manual.md) is the
  plan §8/§9 substrate this revises - the per-note `enrich = false`
  default and the two backfill endpoints it specified.
- [ADR 0106](0106-documents-in-the-binary.md) is where `StartBackfill`,
  `EnrichBackfillNotes`'s sibling machinery, and the "operator's consent to
  reach OpenRouter" framing this ADR extends were first built.
