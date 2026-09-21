# ADR 0116: Notes Are Documents, And Folders Are The Manual

## Status

Accepted (2026-09-20).

Extends [ADR 0106](0106-documents-in-the-binary.md), which built the document
store for uploaded files, and [ADR 0108](0108-search-by-meaning.md), which
added the semantic half of its search. Neither gave the operator anywhere to
*write*.

## Context

A skipper knows things about the boat that exist nowhere but in their head.
The shower drain pump needs its auto switch flicked off when the boat is left
more than a fortnight. The yard manager is best reached by text after four.
1850 RPM burns about six gallons an hour a side. That knowledge has to
survive: findable at 0300 with a leak, readable by crew while the skipper is
ashore, handed to the next owner with the boat.

Helmcentral had nowhere to put it. The document store holds uploaded files.
`assistant.notes` in settings holds one undifferentiated prose blob that only
Mate ever reads. Neither is somewhere an operator writes things down.

Behind the loose notes sits a larger want: a skipper-maintained operations
manual. Start-up and shut-down checklists, how to run the genset, which fuel
valve does what, a photograph of the seacock you need to find in a hurry.
Trawler owners write these, and the thing they say about writing one is that
it is overwhelming and most never start.

So the design problem was never storage. It is that the moment boat knowledge
is generated is the moment capture is most expensive. You learn the shower
pump quirk while wet, in a locker, holding a torch. You learn the yard
manager's calling preference on a dock. You learn cruise RPM with one hand on
the wheel. Any structure the system demands at capture time is a decision the
skipper cannot yet make: they do not know whether the shower pump note is
plumbing, or leaving-the-boat, or a line in a winterising checklist, and they
will not know until there are thirty more like it.

### The requirements, derived before any design

Worked bottom-up from physical constraints rather than from a feature list.
Each one is a constraint the boat imposes, not a preference.

- **R1. Capture is one action producing unstructured text, with no mandatory
  filing, titling or formatting, and voice is a first-class path.** The
  postures in which boat knowledge arrives are hostile to precise typing, and
  a folder tree is a small target under motion and glare.
- **R2. Type is pre-attentively visible at retrieval.** Retrieval under
  pressure is visual search. Position, colour and shape resolve in parallel in
  about 200ms; anything that must be read is serial and far slower per item. A
  homogeneous list of titles forces serial reading of every row.
- **R3. Structure is discovered from accumulated content, never demanded in
  advance.** A wiki does not overwhelm because it has many pages. It
  overwhelms because the *author* must hold the whole structure in mind to
  decide where a new thing goes. That is authoring load, and it is what stops
  people starting.
- **R4. Retrieval is by meaning and by multiple overlapping facets, never a
  single canonical path.** People retrieve through the model they held when
  they encoded the memory ("the thing about the shower"), not through a
  taxonomy imposed later.
- **R5. Every capture pays back immediately and visibly, while accumulating
  toward the manual, and the corpus stays readable without Helmcentral.** The
  cost is paid now by one person; the benefit arrives months later, often to
  someone else. Systems like this are not abandoned by decision, they decay by
  attrition. And the next owner cannot be assumed to run this software.

## Decision

### A note is a document

A note is a Markdown file with YAML frontmatter, stored in the existing
document store as `kind='note'` beside today's `kind='file'`. It is not a new
subsystem. It inherits chunking, FTS5, embeddings, hybrid search, folders,
tags and both of Mate's document tools without a line of new retrieval code.

That inheritance is the argument. A separate notes table would have meant a
second indexer, a second search, a second pair of assistant tools and a model
that has to decide which library to look in.

### The bytes live on disk, content-addressed, like everything else

A note's blob sits in `cacheFilePath("DOCUMENTS_DIR", "data/documents")`,
named by the sha256 of its serialised form, exactly as an uploaded file does.

The competing design was a `documents.note_body TEXT` column. It buys atomic
edits, which is a real advantage and the reason this was not obvious. It
loses on two counts.

First, `documents.sha256` is `NOT NULL UNIQUE`, so a SQLite-resident note is
still a row claiming a hash with no file behind it. `sweepDocumentsDir` would
report every note, at every boot, as a row referencing a missing file. Silencing
that means teaching Helmcentral's only disk/database drift detector to ignore
a growing class of rows. That is not literally a masking fallback, but it
narrows the exact diagnostic the fallback policy exists to protect, and it
narrows it permanently.

Second, portability is a requirement here, not a nicety. R5 says the corpus
has to be readable without Helmcentral, because the manual's audience includes
the next owner. Handed a backup, that owner gets either a folder of files
whose first twenty lines name what each one is, or a `.sqlite` they cannot
open. That is the difference between an archive and a hostage.

There is a third, smaller benefit: the indexing pipeline is already correct
for this. Write the blob, set `status='pending'` and `stage='extract'`, and
`extractTextFile` then `chunkMarkdown` then `ReplaceChunks` re-index it, with
`document_chunk_embeddings`' `ON DELETE CASCADE` invalidating the stale
vectors for free. A body column would need a parallel path doing the same
thing by hand.

### Frontmatter is what makes the hashes safe

`documents.sha256` being UNIQUE means two notes with identical bytes cannot
both exist. Frontmatter removes the possibility rather than handling it: the
`id` field *is* `documents.id`, a v4 UUID and the table's primary key, so two
distinct notes cannot serialise identically no matter how alike their prose.

```
---
id: 0f3b1c2e-8a4d-4f21-9c33-1d2e3f4a5b6c
title: Genset start-up
type: procedure
tags: [genset, engine-room]
created: 2026-09-20T04:11:07Z
---
```

`renderNoteFile` is byte-deterministic, because the bytes are the identity:
explicit field order, LF newlines, tags sorted on the way in, body
right-trimmed plus exactly one newline.

One residual collision survives that argument and is handled rather than
assumed away. An operator can download a note (`GET /:id/content` serves the
file verbatim) and re-upload it, or edit a note until its bytes match an
existing `kind='file'` row. Both now return **409** naming the colliding
document, instead of the 500 `errDocumentDuplicate` used to fall through to.
Loud and specific, for a case that only arises from a round trip the operator
performed deliberately.

Frontmatter is for portability. The database stays authoritative while it
exists, and the extract stage never writes frontmatter back into it: a
hand-edited blob must not be able to change database state quietly.

### The edit protocol, and why its lock ordering is unusual

A blob write and a row update cannot be made atomic. The ordering is what
makes every failure either a no-op or something the boot sweep already
reports:

1. Render the bytes and hash them. If the hash is unchanged, write nothing and
   re-index nothing.
2. Take `lockDocumentSHA` on **both** the old and new hashes, **in
   lexicographic order of the hex string**, once if they are equal. Every
   existing caller locks exactly one hash and cannot deadlock. Two concurrent
   note edits whose old and new hashes cross would deadlock immediately under
   any ordering but a total one.
3. Write to a temp file and rename it into place. The `upload-*.tmp` prefix is
   reused deliberately, because the boot sweep already cleans up abandoned
   files with that name.
4. `ReplaceNoteBody` in one transaction: new hash, new size, `status='pending'`,
   `stage='extract'`, cleared error, bumped `reindex_seq`, and a rebuilt meta
   chunk. It leaves `force_ocr` alone, which is why it cannot reuse
   `MarkReindex`: a note has no scanned pages and must never be queued for a
   vision call.
5. On failure, remove the new blob. Safe, because the lock is held and nothing
   else owns that hash yet.
6. On success, remove the superseded blob, and **log a failure here rather
   than returning it**. The row is already correct. A leftover blob is exactly
   the condition `sweepDocumentsDir` reports as an orphan at next boot, which
   means this needs no change to the sweep at all.

The sweep is deliberately left alone in one further respect. A superseded note
body and a genuine orphan are byte-indistinguishable, so it must keep
reporting both rather than guessing that one kind is safe to delete.

### Type is a column, derived locally, and the operator always wins

`note_type` is `contact`, `quirk`, `spec`, `procedure`, `recipe` or `note`,
rendered as an icon and colour in every list row. R2 needs it resolved before
it is read, and a tag cannot do that job: a tag is a retrieval facet, this is
a visual channel.

It is assigned by `classifyNoteType`, a pure deterministic Go function. A
phone number or email makes a contact. Numbered or checkbox lines make a
procedure. Unit-bearing numerics like "1850 RPM" make a spec. Quantity lists
make a recipe. Order matters and is fixed by test: a procedure that happens to
contain a phone number is still a procedure, and a recipe outranks a spec
because quantities are unit-bearing too.

`note_type_source` records where the type came from and enforces
`operator > mate > auto` in one method, mirroring the way `document_tags`
already refuses to let a suggested tag displace an operator one. Mate may
upgrade `auto` to `mate` during enrichment. Nothing ever overwrites
`operator`.

This is the shape of the whole feature's relationship to Mate: **Mate is an
accelerant, never a dependency.** PRODUCT.md promises no cloud account or
subscription, so an operator with no OpenRouter key is a supported user rather
than a degraded one. Capture, typing, titling, filing, ordering, reading and
keyword search all work with the assistant switched off. Only semantic search,
summaries and suggested tags need it, and all three already degrade loudly.
The moment of greatest need, reading your own shutdown checklist at 0300,
never needed Mate at all.

### Notes do not enrich themselves

`uploadDocumentHandler` sets `enrich` from readiness at upload time, on the
reasoning that uploading a document is a deliberate act carrying its own
consent. Capturing a note in one tap at the helm is not that act. A note
reading "ring Dave about the mooring, 0412 ..." is exactly what an operator
would not expect to leave the boat unprompted, so `kind='note'` defaults to
`enrich = 0`. Enrichment is offered per-note, and library-wide through a
backfill the operator has to ask for.

### Folders are the manual, and a boat has more than one

Filing is the promotion path. A top-level folder is a collection; any
top-level folder can carry `role='manual'`, which gives it ordering, section
numbering and a front-to-back reading view. Filing a note into a manual's tree
at a position makes it a section. Filing into any collection drains the inbox,
which is R5's visible accumulation made literal: the Notes count goes down as
the manual fills.

`role` is a flag, not a singleton. Boats that write manuals write more than
one, for different audiences: an operations manual for running the boat, a
crew training manual for teaching someone to. Contacts and Recipes stay plain
collections, because a linear reading order over a list of phone numbers means
nothing.

Marking the manual with a column rather than a well-known folder *name* is
what lets it survive a rename, and rather than a settings key because a
settings key puts an unenforced foreign key in YAML. Clearing the flag demotes
a manual to an ordinary collection without moving a single note.

`sort_index` sits on both `documents` and `document_folders`, because a
manual's children are a mix of subfolders, notes and filed PDFs. A
photographed data plate is a legitimate section. Ordering becomes
`ORDER BY sort_index, lower(name)`, and the default of `0` means an existing
library orders exactly as it did before the migration.

Two constraints could not be expressed in SQLite and are enforced in the store
methods instead, with the schema comment saying so rather than implying a
guarantee that is not there: that `note_type` is only meaningful when
`kind='note'`, and that `role` is only valid on a top-level folder.
`ALTER TABLE ... ADD COLUMN` cannot add table-level CHECK constraints at all,
so this was not a choice.

### Two things aboard are called a manual, and both keep the word

The document library already holds manuals in the ordinary sense: the Cummins
QSB 6.7 book, the Schenker watermaker book, the builder's own Owner's
Handbook, usually filed in a folder called Manuals. This ADR adds a second
thing also called a manual, in the same library: a folder the skipper writes
themselves.

Both keep the word. They genuinely are both manuals and that is what people
aboard call them, so renaming either one would trade a real ambiguity for an
invented vocabulary nobody uses. Handbook is not available as a
disambiguator, because the builder's document is already called one.

What carries the distinction is the surface, not the vocabulary.
Manufacturer PDFs and authored manuals are both documents, browsed through
the Documents panel and the Manuals panel respectively.

Mate gets no manual-specific tool at all, and that is a deliberate reversal
of this plan's own first draft. A `search_manual` scoped to authored manuals
would have captured "what does the manual say about the impeller" - a
question that on any boat overwhelmingly means the engine PDF - and answered
it from the one corpus that does not contain the answer. A tool name beats
its own description whenever the name matches the word the operator used,
which is the precise trap the Manual to Help rename had just finished
undoing. Partitioning the library also forces the model to choose a side
before it searches, and either choice hides half of a good answer: the
impeller question deserves the Cummins page *and* whatever the skipper wrote
about their own impeller, ranked together.

So `search_documents` stays the single entry point and spans everything. The
affordance that tells Mate an authored manual exists at all is
`manualIndexLine` in the system prompt, which lists each manual and its
sections; scoping is then an ordinary `folder` argument on the search tool
that already has one.

This is deliberately not the same judgement as the Manual to Help rename that
landed just before this work. That one resolved a collision between two things
that were not both manuals: Helmcentral's own documentation was only ever
called a manual by accident of implementation. Here, both really are.

### A folder kind, and the constraint that will bite when a second one arrives

`role` is deliberately a string rather than a boolean, because a manual is
plainly the first of several folder kinds and not a special case forever. A
recipes collection, a contacts collection and a maintenance log all want the
same treatment: a shape the folder's contents are expected to follow, and a
reading view that knows what that shape is. Nothing here forecloses that. A
second kind is a second `role` value plus whatever view knows how to render
it, and the ordering, promotion and filing machinery is already kind-agnostic.

One thing to know before that day, because it is not obvious and it is
expensive to discover late. The column shipped as
`ALTER TABLE document_folders ADD COLUMN role TEXT NOT NULL DEFAULT ''
CHECK (role IN ('','manual'))`, and **SQLite cannot alter a CHECK constraint
in place**. Adding `'recipe'` to that set means rebuilding
`document_folders` (create a new table, copy, drop, rename) on a live boat
database, or dropping the constraint and validating in Go instead, which is
already where the other two `role` invariants live because SQLite cannot
express them either.

It was left as a CHECK rather than pre-emptively widened because guessing at
the second value's name now buys nothing, and a constraint that says what is
true today is better than one loosened for a future that has not been
designed. The migration to widen it belongs in the ADR that introduces the
second kind, not in this one.

### Photographs and links carry no host

Note markdown refers to a photograph as `![Fuel manifold](hc-doc:<uuid>)` and
to another note as `[Genset start-up](hc-note:<uuid>#warm-start)`.

Both markdown renderers pass `disallowedElements={['img']}` today. The hole
that closed was never DOM injection, since react-markdown escapes raw HTML
without `rehype-raw`. It was that an `img` with an attacker-chosen `src` is an
outbound request from the boat to a host the attacker picks, issued from
content that may have been generated by Mate or extracted from an uploaded
PDF. A tracking pixel confirming the vessel is online matters more at sea than
it does on a desk, and it is the same concern [ADR 0111](0111-security-threat-model.md)
and `find_places`' outbound allowlist already address.

`hc-doc:` carries no host, no path and no query. Only a UUID, validated
against an anchored pattern before it is ever concatenated into a URL, so
there is no string an author can write that reaches anywhere but this
backend's own `/api/documents/<uuid>/content`. Anything that fails the pattern
renders as its alt text, not as an image and not as a link. A UUID that does
not resolve yields a visible "photo missing" placeholder rather than a blank.

Ordinary links out of a note run an allow-list rather than a deny-list, for
the reason ADR 0106 gave for `documentInlineMIMEAllowList`: a deny-list stays
safe only while every dangerous scheme is remembered, an allow-list fails safe
for whatever nobody thought of. `http:`, `https:`, `mailto:` and `tel:` are on
it. `tel:` earns its place because `contact` is a first-class note type and a
yard manager's number should be tappable from a helm screen. A relative href
is deliberately off it: same-origin, so not dangerous, but it would navigate
the dashboard to a path that does not exist, and refusing it surfaces the
authoring mistake instead of shipping a dead link. The scheme check runs
against a copy with ASCII tab, newline and carriage return stripped, because
browsers strip those before parsing a scheme and `java\tscript:` would
otherwise slip past a naive prefix test.

Notes therefore get their own renderer rather than a flag on the existing one.
Mate's output is untrusted and must never render an image; an `allowImages`
prop would be one careless call site away from making it do so. Two renderers,
two trust levels, no flag.

## Rejected

### A separate notes table and panel

Clean to start, and then every retrieval feature has to be built twice. FTS5,
the embedding queue, hybrid ranking, folders, tags and two more assistant
tools, all shadowing implementations that already exist. Worse, Mate would
have to choose which library a question belongs to before it could answer.

### A markdown wiki with quicksearch

Fails R1 and R3 together and for the same reason: "create page" demands a
title and a location before one character of content exists. That is the
authoring load that stops people starting, and the trawler-forum threads
describing the task as overwhelming are describing exactly it.

### A flat tagged list of notes, and nothing more

Satisfies R1 and R3 and fails R5 outright. Notes that can never become a
manual mean the effort never compounds, so there is no long-term payoff to
offset the cost of capture. It also fails R2 without a type channel: two
hundred identical-looking rows are read serially.

### Real directories for manual sections

Rejected for the same three reasons ADR 0106 rejected them for folders, which
all still hold: dedupe, drift, and moves that can fail partway through a large
tree. Nothing here changed that calculus.

## Consequences

Notes are searchable by `search_documents` and readable by `read_document`
from the moment they exist, with no Mate changes at all, and no tool is added
for them or for manuals. Mate's whole gain from this feature is context
rather than capability: pinned notes in the cached system prompt, and an
index line naming the manuals and their sections so it knows to scope a
folder.

`extractTextFile` now strips a leading frontmatter block from any
`text/markdown` document, not just notes. That is a behaviour change for
already-uploaded `.md` files carrying frontmatter, and it is intended: without
it every note's first body chunk would start with `id: 0f3b...` and pollute
the index.

Editing a note rewrites its blob under a new hash, so a note that is edited
often leaves the content-addressed store churning more than an upload-only
store ever did. The superseded blob is unlinked on the success path; when that
unlink fails, the sweep reports it.

The manual's real test is not whether it can be built but whether it gets
built. A new manual is empty, and an empty manual is the blank page R5 warns
about. Starter outlines were considered and cut for this cycle: a built-in
spine is template data to maintain in the binary, and no proforma fits every
boat. If manuals get created and left empty aboard, that is the first thing to
revisit, and it is cheap, because an outline is only a folder tree and some
placeholder notes.

## Addendum (2026-09-20): one surface, not two

Not a reversal. "A note is a document, folders are the manual" is exactly as
true today as it was above; what changed is which UI surfaces render that
substrate, described in the "Two things aboard are called a manual, and both
keep the word" section as "the Documents panel and the Manuals panel
respectively," alongside a since-deleted Notes panel for the inbox.

Phases 1, 2b and 3 shipped that as three sidebar rows: Documents for
uploaded files, Notes for the unfiled inbox, Manuals for authored manuals.
Once built, the split did not hold up against the decision this ADR already
made. The data model joined notes and manuals to the document table on
purpose — a note is `kind='note'`, a manual is `document_folders.role
='manual'` — and the UI then put the joined table back into three separate
places to browse it from. An operator learning the app had to learn that a
"document," a "note" and a "manual" were three different things worth three
different sidebar rows, when the schema underneath them says they are one
thing wearing three hats.

[ADR 0119](0119-capture-is-an-action-not-a-place.md) now covers where
capture lives once Notes stops being a panel ([ADR 0121](0121-notes-are-created-in-documents.md)
later narrowed that to Documents' own New → Note menu). What is left here: Documents
renders all three. A folder browses as a plain list or, when it carries
`role='manual'`, as the ordered tree and reading view this ADR already
specified — no second navigation surface for the same choice, since browsing
between manuals is just browsing between folders the ordinary way once a
Manual-kind folder is visually marked in the listing. A note opens through
the same viewer as any other document, gated on `kind='note'` for whether an
Edit toggle reaches the WYSIWYG editor
([ADR 0117](0117-markdown-is-the-storage-format-not-the-authoring-format.md)).
"Unfiled notes" (`kind='note' AND folder_id IS NULL`) becomes a filter chip
over that same listing rather than a place with its own inbox screen — the
drain-the-inbox loop and its visible count survive intact, because the count
is just how many rows currently match the filter.

Nothing on the backend moved. `GET /api/notes`, `/api/manuals` and their
siblings are unchanged; this addendum is a frontend composition change only,
recorded here because it is this ADR's own consequence that changed, not
because the decision it recorded did.

The plan's Risk 4 — "two new sidebar rows land right after the sidebar was
deliberately reordered," weighed against "one Notes row with a Manuals tab"
— is retired outright rather than resolved either way. Its own framing
("different objects, different rows") does not survive contact with a
Documents panel that already lists folders, uploaded PDFs and notes side by
side in one table: they were never different enough objects to need
separate rows, only different enough to need an icon (note type) and a
folder flag (manual) to tell them apart at a glance.

A note-type *filter* was built alongside that icon and then removed before
this shipped. It was the wrong axis: a library folder holds mostly
uploaded files, whose note type is empty by definition, so filtering by
type is inapplicable to most of what is on screen and empties the listing
rather than narrowing it. The type is worth **seeing** on a row and is not
a useful way to slice a mixed library; the Unfiled notes view already
covers the one case that genuinely wanted a notes-only listing.

## Related

- [ADR 0106](0106-documents-in-the-binary.md) is the store this builds on.
- [ADR 0108](0108-search-by-meaning.md) is the semantic half notes inherit.
- [ADR 0111](0111-security-threat-model.md) for the outbound-request concern
  behind `hc-doc:`.
- [ADR 0117](0117-markdown-is-the-storage-format-not-the-authoring-format.md)
  covers why the editor is WYSIWYG when the file is Markdown.
- [ADR 0119](0119-capture-is-an-action-not-a-place.md) covers where capture
  lives now that Notes is not a panel, and the `Auto` type default.
- [ADR 0121](0121-notes-are-created-in-documents.md) later removed the
  global header action and Alt+N that ADR 0119 built.
- [ADR 0118](0118-a-checklist-is-a-run-not-a-checkbox.md) covers checklist
  runs: text-keyed ticks and where a run's state lives.
