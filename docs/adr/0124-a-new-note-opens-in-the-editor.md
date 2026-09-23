# ADR 0124: A New Note Opens In The Editor

## Status

Accepted (2026-09-23). Answers a question
[ADR 0119](0119-capture-is-an-action-not-a-place.md) left implicit and
[ADR 0117](0117-markdown-is-the-storage-format-not-the-authoring-format.md)
never asked: why the WYSIWYG editor does not appear when a note is created.

## Context

Writing a note today means typing or dictating into a plain
`InputGroupTextarea` (`note-capture-sheet.tsx`), pressing **Capture**, and
getting back a classified, titled note. The Plate editor from ADR 0117,
with its headings, tables, links and task lists, only mounts afterwards,
behind the viewer's **Edit** toggle or inside a manual folder view.

That split was never argued out loud. ADR 0119 decided where capture lives
and ADR 0117 decided what the editor is, and each assumed the other had
settled the overlap. The gap shows the moment an operator writes something
structural on the first pass: an incident timeline, a spec table, a
procedure with steps. They get a plain box, so they either type raw
Markdown pipes, which is precisely the knowledge ADR 0117 says nobody
aboard should need, or they capture unformatted prose and reopen the note
to lay it out properly.

The reason to hesitate was always weight. The editor is a deliberate
`React.lazy` boundary (`note-editor.tsx` over `note-editor-impl.tsx`) so
that Plate and its Markdown serialiser stay out of the entry chunk. Built,
that dependency is the `editor-vendor` chunk: 801 KB raw, 239 KB gzipped,
against a 10 KB impl chunk of our own. Two things take the sting out of it.
The asset is served by the boat's own box over the boat's own LAN, not over
a marina uplink; and the one browser that genuinely struggles to parse a
chunk that size, the wall kiosk's WebKit, never sees capture at all, since
capture is reached from Documents (ADR 0121), and the kiosk has no
Documents panel to open it from.

So the cost is a one-off chunk fetch on a local network, and it can be paid
before the operator asks for it rather than while they wait.

## Decision

### New Note opens the editor

The capture sheet's plain textarea is replaced by the ADR 0117 editor. No
mode, no toggle, no choice presented before the first word: the operator
picks **New → Note** in Documents (the only door in, per ADR 0121) and gets
the editor, with the caret in the body and the toolbar above it.

Asking a skipper to decide between a plain field and a formatted one is
asking them to predict, before writing anything, whether what they are
about to say has a table in it. That is the wrong question to put in front
of someone mid-repair, and it was the flaw in this ADR's first draft.

### The chunk is prefetched, not awaited

`note-editor.tsx`'s lazy boundary stays exactly as it is, for the entry
chunk's sake, but the shell warms it: the editor chunk is prefetched once
the app is idle after first paint, and again on hover or focus of
Documents' **New → Note** menu item, so by the time the sheet opens the
module is in cache and mounts synchronously. If it genuinely has not
arrived, the sheet shows the editor's own loading state rather than a
plain textarea standing in for it.
There is no plain-field fallback path, per AGENTS.md: two authoring
surfaces that can each silently be the one you get is the ambiguity this
decision exists to remove.

### The mic sits in the editor and inserts at the caret

Plate has no speech plugin of its own, first-party or community, and its
`ai` packages are LLM assistance rather than audio. It does not need one.
`useDictation` (`dictation.tsx`) already owns the Web Speech session, the
interim line, the error surface and the ADR 0122 arbiter claim that keeps
"Hey Mate" out of the way; only its sink changes.

Today that sink is `setValue`, a React state updater appending to a string.
For the editor it becomes an insertion at the selection, `editor.tf.insertText`,
with a leading space when the caret is mid-sentence. `useDictation` grows an
insertion-sink option; it does not grow a second recognizer.

Two details that are the whole implementation risk:

- **Selection may be null.** The operator can press the mic without ever
  having clicked into the body. The sink focuses the editor and collapses
  the selection to the end of the document before inserting, so first
  dictation into an untouched note lands in the body rather than throwing.
- **Interim results stay out of the document.** `DictationStatus` already
  renders the in-flight transcript beside the mic. Only final results are
  inserted, so no speculative text ever enters the undo history or the
  serialised Markdown.

Native OS keyboard dictation is a different path, one that types into
`contentEditable` through composition events, and this decision does not
rely on it. The mic control here is the same in-app Web Speech session
every other field uses.

### Capture keeps one commit action

The editor is embedded without its Save button and dirty line. The sheet
already has **Capture**, which does `POST /api/notes` with the type
selection attached, and two save affordances over one unsaved buffer is the
kind of ambiguity a helm screen cannot afford. That means splitting the
editor's body out from its footer in `note-editor-impl.tsx`, which is a
real refactor rather than a prop.

The body is serialised to Markdown at Capture time through the same
`serializeNoteMarkdown` the editor's own save path uses, so the stored file
is byte-for-byte what the editor would have written anyway, and the
`Auto` type default from ADR 0119 is untouched.

## Alternatives

### A Format toggle over a plain default

This ADR's first draft, rejected on the operator's behalf. It keeps the
cold path cheap but makes formatting something the skipper has to know to
ask for, which is the same failure as the Markdown cheat sheet below,
wearing a button.

### Leave it as it is and document the second pass

What ships today. Rejected because the second pass is undiscoverable from
capture: nothing in the sheet says the note can be laid out properly later.

### A Markdown cheat sheet in the capture sheet

Rejected for the reason ADR 0117 gives in full: the fix for "most boat
owners do not write Markdown" is not to teach them Markdown in a smaller
box.

## Consequences

The capture sheet's first paint now depends on a prefetched chunk. That is
the one real regression risk, and it is the thing to watch on the boat: a
cold open on a browser that has just been restarted pays the fetch.

`note-editor-impl.tsx` gains a body/footer split, and its round-trip
behaviour stays fixture-guarded by the existing 13-fixture corpus.

`useDictation` gains an insertion sink alongside its `setValue` one. Per
AGENTS.md the tests come first: dictation with no prior selection lands at
the end of an empty note, dictation mid-note lands at the caret, interim
results never reach the document, and closing the sheet mid-dictation still
cancels the session and releases the arbiter claim.

Nothing under `backend/` changes. `POST /api/notes` already takes a body, a
title and a type, and does not care which surface rendered the Markdown.

The plain-textarea capture path is removed rather than kept beside the new
one, so there is exactly one way a note gets written.

## Related

- [ADR 0117](0117-markdown-is-the-storage-format-not-the-authoring-format.md)
  for the editor, the enabled node set and the source toggle that remains
  the escape hatch for anyone who does write Markdown.
- [ADR 0119](0119-capture-is-an-action-not-a-place.md) for R1 and why the
  kiosk has no capture control - the global header action and `Alt+N` it
  also introduced were removed by ADR 0121 below, and this ADR's own
  references to them follow that later ADR, not the 0119 text itself.
- [ADR 0121](0121-notes-are-created-in-documents.md) and
  [ADR 0122](0122-one-dictation-control.md) for the single mic instance and
  the arbiter claim the editor's mic has to keep honouring.
