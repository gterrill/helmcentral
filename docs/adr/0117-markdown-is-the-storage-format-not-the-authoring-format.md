# ADR 0117: Markdown Is The Storage Format, Not The Authoring Format

## Status

Accepted (2026-09-20). Phase 2b of the plan
[ADR 0116](0116-notes-are-documents-and-folders-are-the-manual.md) opened:
the note editor. ADR 0116 built the substrate (a note is a Markdown file
with YAML frontmatter) and [ADR 0118](0118-a-checklist-is-a-run-not-a-checkbox.md)
covers checklist runs (Phase 4); this one covers what actually writes the
bytes.

## Context

ADR 0116 made a note a plain Markdown file for the reasons that ADR gives in
full: portability to a next owner with no Helmcentral installed, and reuse of
the document store's existing indexing pipeline. Neither reason has anything
to do with what the skipper types.

Most boat owners do not write Markdown, and the two places a raw-text editor
fails them hardest are exactly the two a manual most needs: a table (tank
capacities, cruise RPM against fuel burn) and a link between two notes (a
start-up procedure pointing at the fuel valve layout). Handing the operator a
`Textarea` and the GFM cheat sheet satisfies ADR 0116's storage decision and
fails its own R1 requirement — capture and, by extension, authoring, has to
demand nothing the skipper doesn't already know.

So the file on disk and the thing the operator edits are two different
questions with two different answers. The file is `.md` because that is
portable and because the indexer already understands it. The editor is
WYSIWYG because nobody aboard should have to know that.

## Decision

### Plate, not a hand-rolled contentEditable

[`platejs`](https://platejs.org) (Slate-based) supplies the editing model;
[`@platejs/markdown`](https://www.npmjs.com/package/@platejs/markdown)
supplies Markdown serialisation in both directions. Writing a rich-text
editor from scratch was never seriously considered — Slate/Plate's whole
value is the boring, hard parts (selection, undo, void nodes, mark
splitting) being someone else's problem, and the round-trip discipline below
is a solved problem this library ships rather than one this project would
have to invent.

### The enabled node set is the round-trip contract

In: headings (h1–h3), bold, italic, inline code, bulleted and numbered
lists, task lists, tables, links, images, blockquote, horizontal rule, code
block. Out, deliberately: footnotes, reference-style links, raw HTML,
mentions, comments, and anything else Plate can represent that Markdown
cannot carry losslessly.

This is a testable rule, not a taste. A golden-file corpus of 13 fixtures —
one per enabled node, plus a realistic combined note — round-trips
byte-identical through parse → Plate value → serialise
(`frontend/src/test/fixtures/notes/`,
`frontend/src/test/note-editor-markdown.test.ts`). **If a node does not
round-trip, the fix is to remove the node, not to patch the serialiser.**
Every one of the excluded types was excluded for exactly this reason: a
footnote reference has nowhere to put its definition once the editor
represents it as a Plate node instead of a pair of Markdown constructs; a
reference-style link (`[text][ref]` plus a `[ref]: url` definition
elsewhere in the document) has the identical problem; raw HTML re-opens the
`disallowedElements={['img']}` argument ADR 0116 already made for
`note-markdown-impl.tsx`, for a renderer that has no `disallowedElements`
escape hatch at all because it's an editable DOM tree, not a read-only
render pass.

`@platejs/list`'s list plugin uses what Plate calls the "indent list"
model, not a nested `ul`/`li` tree: a list item is an ordinary paragraph
node carrying `indent` (nesting depth) and `listStyleType`
(`disc`/`decimal`/`todo`), plus `checked` for a task item. One plugin
therefore covers bulleted, numbered *and* task lists — there is no separate
task-list plugin to register, and `@platejs/markdown`'s serialiser
understands this shape natively (verified against the corpus, not assumed).

### Plate core, this project's own chrome

Plate ships a prebuilt component kit (`@platejs/*-kit` packages, plus
individual Radix-based components such as `@platejs/toolbar`) built on
Radix UI. This project migrated off Radix onto Base UI deliberately, and
importing that kit would undo it — two primitive libraries would mean two
focus, portal and dismiss models running side by side in the same app.
`note-editor-impl.tsx` therefore imports only Plate's headless plugin
packages (`platejs`, `@platejs/basic-nodes`, `@platejs/list`,
`@platejs/table`, `@platejs/link`, `@platejs/media`, `@platejs/code-block`)
and builds the toolbar, its Link/Image popovers and every element's
rendering entirely from this repo's own `@/components/ui/*` (Base UI)
primitives — the same `Button`, `Popover` and `Input` every other panel
uses. No `@platejs/*-kit` package and no Plate prebuilt component is
imported anywhere in this feature.

**Two Radix packages arrive anyway, and that is accepted, not an oversight.**
`npm ls @radix-ui/react-slot @radix-ui/react-compose-refs` against this
dependency install resolves both to exactly one path each, transitively
through `@udecode/react-utils`, itself a `platejs` dependency:

```
helmcentral-dashboard@0.1.0
`-- platejs@53.3.14
  `-- @udecode/react-utils@52.3.4
    `-- @radix-ui/react-slot@1.3.3
      `-- @radix-ui/react-compose-refs@1.1.5
```

Both packages are pure ref- and prop-merging utilities — `Slot` composes a
component's own props onto a single child element; `composeRefs` merges
several React refs into one callback. Neither implements a portal, a
focus-trap or a dismiss layer, which is the entire thing "two primitive
libraries in one app" was worried about (the plan's own Risk 7: "two focus,
portal and dismiss models in one app"). A ref-merging helper has no focus
model to conflict with Base UI's. If this note has confused a future reader
because two Radix packages showed up in a `package-lock.json` diff for a
project that supposedly left Radix behind: this is why, and it was checked,
not missed.

`@floating-ui/react` arrives via `@platejs/floating` (`upsertLink`'s and the
table toolbar's own positioning). Base UI is itself built on Floating UI —
`@base-ui/react` already depends on `@floating-ui/react-dom` and
`@floating-ui/core` — so this is a shared foundation, not a second one.
`@floating-ui/react` (the full React-bindings package, distinct from
`@floating-ui/react-dom`) is new to this project's dependency tree, though,
and `frontend/vite.config.ts`'s `dashboard-vendor` codeSplitting group had
to be narrowed from a bare `id.includes('@floating-ui')` to explicit
`@floating-ui/core`/`@floating-ui/dom`/`@floating-ui/react-dom` path
segments so that the new `@floating-ui/react` package falls through to the
editor's own lazy chunk instead of being swept into the dashboard's eager
one — the same "manualChunks groups by module id, not import graph
reachability" trap `chart-vendor`'s own comment in that file already
documents for `recharts`.

### The serialiser configuration, measured

The bytes of a note are its identity (ADR 0116): `documents.sha256` is what
names the blob, drives re-indexing and prices an embedding. A WYSIWYG editor
that reformats a note on every save — a different bullet marker, a
different emphasis delimiter, padded table columns — turns an edit that
changed nothing about the note's meaning into a new blob, a re-index and an
embedding charge.

`@platejs/markdown`'s serialiser is built on `remark-stringify`, and its
*defaults* reformat. Measured against the same 13-fixture corpus the
round-trip test uses, `@platejs/markdown`'s own defaults failed
byte-identity on 6 of the 13: emphasis (`*italic*` → `_italic_`), bulleted,
task and nested lists (`- item` → `* item`), the horizontal rule (`---` →
`***`), and — separately, and worse — an empty table cell. That last one is
not a stylistic drift: on serialise, `@platejs/markdown`'s default
`preserveEmptyParagraphs` behaviour writes a **ZERO WIDTH SPACE (U+200B)**
into every empty table cell. That is content corruption, not formatting —
the character is invisible in every normal renderer, it reaches full-text
search and the embedding pipeline as real content, and it would greet the
next owner's plain text editor as a byte nobody can explain.

The fix is one exported constant
(`frontend/src/lib/note-editor-config.ts`), applied at every call to
`serializeMd`, nowhere else:

```ts
const REMARK_STRINGIFY_OPTIONS = {
  bullet: '-',        // GFM-canonical, and `- [ ]` is what the Phase 4
                       // checklist runner parses. Load-bearing, not cosmetic.
  emphasis: '*',
  strong: '*',
  rule: '-',
  fences: true,
  listItemIndent: 'one',
}
```

```ts
serializeMd(editor, {
  value,
  remarkPlugins: [[remarkGfm, { tablePipeAlign: false }]],
  remarkStringifyOptions: REMARK_STRINGIFY_OPTIONS,
  preserveEmptyParagraphs: false, // kills the U+200B defect above
})
```

`tablePipeAlign: false` is deliberate for the same reason: padded table
columns reflow the whole table whenever one cell's width changes, so a
one-word edit becomes a whole-table diff. With this configuration, all 13
fixtures round-trip byte-identical — that corpus is the regression guard,
run on every `npm test`, and it is written in the canonical form the
serialiser itself emits (`- ` bullets, `*emphasis*`, `---` rules, so the
fixtures double as documentation of what "correct" looks like, not just an
assertion of it).

### The source toggle is the escape hatch

A `</>` control swaps the WYSIWYG view for the raw Markdown in a plain
`Textarea`. Edits made there are **authoritative** — toggling back
re-deserialises from the text, discarding whatever the WYSIWYG view held
before the toggle. Two things follow from that: the operator who does know
Markdown always has a plain-text way to fix something the editor won't let
them express, and if the enabled-node-set discipline above is ever violated
by a future change, this is the fail-loud path rather than a silently
mangled save — the skipper can always see, and correct, the actual bytes.

### Opening a note never writes

Dirty state is tracked from user edits only — the Plate `onValueChange`
callback, guarded so a mount-time normalisation pass (Plate assigns node
ids and normalises list structure once, on construction) cannot itself set
the flag. Save is a single explicit button, disabled until something is
actually dirty, with no autosave: every save is a new blob, a re-index and
an embedding spend, so keystroke-rate churn is the wrong trade (unchanged
from the plan's own framing). Nothing about opening, reading or closing a
note — including switching between the WYSIWYG and source views — writes
anything to the server.

A note authored elsewhere (an imported `.md` file, a hand edit off a
backup) will still be reformatted the first time this editor saves it, and
that is accepted. What changed is *when* the operator finds out: the
freshly-parsed value is compared against the original bytes once, on open,
and if they differ, one line says so in the editor — not a modal, not a
repeated nag, and never a save that happens before the operator asked for
one.

### Checklist items key on plain text, not raw Markdown

The plan's own §3 already establishes that a checklist run keys
each tick to an item's normalised plain text rather than its Markdown
source, specifically so that this editor re-emitting `**Seacocks** open` as
`Seacocks **open**` — or any other cosmetically different but
semantically identical emphasis placement — cannot silently invalidate a
tick made against the same line before the edit. The frontend carries its
own copy of that normalisation
(`frontend/src/lib/checklist-item-text.ts`) so the editor's test suite can
assert the invariant without depending on the Go package Phase 4 (not yet
built) will add for the server-side key; both sides implement the same
stated rule — strip the leading GFM marker and inline emphasis/code
delimiters, collapse whitespace, trim — independently, by design, since a
line normalises identically either way that rule is expressed.

### Tables of contents are never written to the file

Plate's own table-of-contents node, if it is ever wired in, is editor-side
navigation only — it must never become a document block. A long note's
reader-facing TOC is computed at read time from its headings
(`note-markdown-impl.tsx` already assigns the same `slugifyHeading` ids a
TOC would need), so nothing resembling a `[[toc]]` marker is ever a
candidate for round-tripping through this editor, and the question of
whether it round-trips correctly never arises. This editor does not
currently build a TOC panel at all — it was judged out of scope for this
phase, since the reading view is where an operator benefits from one, not
the editing view.

## Rejected

### Plate's prebuilt kit

Fastest to ship, and exactly the Radix reintroduction the migration to Base
UI existed to prevent. See "Plate core, this project's own chrome" above.

### A formatting toolbar over the plain Markdown textarea

The documented fallback if Plate had failed either of the two checks this
task existed to run (React 19.2 support, no Radix drag-along) — a toolbar
that inserts `**`/`##`/`- ` around the cursor, no framework, no round-trip
risk because the text always *is* the source. Both checks passed: `platejs`
declares `react: >=18.0.0` and mounts cleanly under React 19.3 (this
project's resolved version), and the Radix footprint is the two utility
packages audited above. Kept as the documented fallback if a future Plate
upgrade ever fails either check again, not built.

## Consequences

`frontend/src/components/note-editor.tsx` is a `React.lazy` wrapper,
matching `note-markdown.tsx`/`help-markdown.tsx`'s own pattern
(commit `70acb53`); `note-editor-impl.tsx` and `note-editor-config.ts` carry
every Plate/Slate import between them, and nothing outside that pair
imports either package directly. `frontend/vite.config.ts` gained a
dedicated `editor-vendor` `codeSplitting` group so that dependency graph has
a stable, named chunk; `scripts/check-entry-chunk.mjs` gained a second,
narrower guard alongside its existing regex-lookbehind scan — by filename,
not by content — asserting no chunk named for `editor-vendor` is ever
reachable by a static import from the entry chunk. The wall-display kiosk
never opens a note, so it never pays for either the editor's weight or its
risk of shipping a Safari-16.0-breaking construct.

`notes-panel.tsx`'s note reader Sheet gained an Edit affordance that swaps
`NoteMarkdown` for `NoteEditor` in place; saving goes through
`use-notes.ts`'s existing `patchNote(id, { body })`, which already existed
(built in Phase 2 against this phase's eventual need) and needed no change.
`manuals-panel.tsx`'s reading pane gained the identical affordance, wired
independently (a local `PATCH` call rather than a second `useNotes`
instance, since the pane already fetches a section's body directly) because
threading the edit state up through `ManualsPanel` for one consumer would
have been prop plumbing for its own sake.
