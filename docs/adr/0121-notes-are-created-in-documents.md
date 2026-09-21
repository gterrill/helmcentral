# ADR 0121: Notes Are Created In Documents

## Status

Accepted (2026-09-22).

## Context

[ADR 0119](0119-capture-is-an-action-not-a-place.md) put capture behind two
doors: a header button and an `Alt+N` shortcut reachable from any screen,
and Documents' own **New → Note** menu, both opening the identical
`NoteCaptureSheet`. The reasoning at the time was that a skipper mid-repair
with something worth writing down is not already looking at a file listing,
so capture had to be reachable from wherever they actually were.

The operator has since decided otherwise: a note is a document, and it
belongs to the documents path the same way an uploaded manual or a manual
section does. Folder, Upload and Note already hang off the one **New** menu
in Documents - a note being the one thing on that list also reachable from
the header, via its own separate button and its own keyboard shortcut, was
an exception the rest of that menu never needed. One creation path is
simpler than two, and the operator would rather have it than the extra
reach.

## Decision

A note is created only from **Documents → New → Note**. The header
**Capture a note** button and the `Alt+N` shortcut are removed from
`App.tsx` entirely, along with the state that drove them
(`captureOpen`/`captureType`/`captureHasOpenedRef`) and the lazy import of
`NoteCaptureSheet` that lived there.

`documents-panel.tsx` now owns `NoteCaptureSheet` outright: its own
`captureOpen`/`captureType` state, its own `captureHasOpenedRef` latch, and
its own lazy `import()` of the sheet, mounted only once the operator has
actually opened it - the same mount-once-opened idiom `App.tsx` used to
apply to it. The **New → Note** menu's Auto item and its kind submenu call
a local `openCapture(type?)` directly; there is no `onCaptureNote` callback
prop left on `DocumentsPanel` for anything to pass in, because there is
nothing left outside the panel that needs to open it.

The sheet itself did not change. The `Auto` default, the fact that `Auto`
resolves locally and never touches the network, the mic button, the type
select - everything ADR 0119 decided about what happens once the sheet is
open still holds. This ADR only revisits where the door into it is.

Two improvements fell out of moving the sheet inside the panel, neither
possible from `App.tsx`, which had no viewer or notes list of its own to
hand the new note to. `onCaptured` now opens the freshly captured note
straight in the panel's own viewer, via the same `setViewerId` the row
list and the search results already use to open a document. It also
refreshes the panel's own `unfiled` notes list, since a captured note's
own `useNotes()` instance inside the sheet and the panel's `unfiled` one
share no cache with each other - without the explicit refresh, a new note
would open in the viewer while the **Unfiled notes** count and list beside
it stayed one behind until the next unrelated refresh.

## Rejected

### Keeping the header button as a shortcut

A note that is a document but is one click faster to start than any other
document was the exact inconsistency this decision removes. The header
button was not costing much - one icon, one keyboard listener - but it was
a second answer to "how do I write a note down" sitting next to the one
Documents already gives every other kind of document, and the operator
would rather the app have a single answer than a fast one and a
discoverable one. `Alt+N` goes with it: a shortcut for a door that no
longer exists has nothing to open.

## Consequences

`App.tsx` loses one lucide-react import (`NotebookPen`), one `useEffect`,
three pieces of state, one lazy import, one prop passed to
`DocumentsPanel`, and one rendered `<NoteCaptureSheet>`. None of it was
load-bearing for anything else in the shell.

`documents-panel.tsx` gains what `App.tsx` lost: the lazy import, the
`captureOpen`/`captureType`/`captureHasOpenedRef` state, and the rendered
sheet, wrapped in the same `Suspense` fallback-null pattern every other
lazy sheet in this codebase uses. `note-capture-sheet.tsx` stays its own
chunk either way - it was never in the entry bundle under ADR 0119 and
still is not now, just imported from one file instead of another.

`docs/features/notes-and-the-manual.md` and
`docs/how-to/write-the-boats-manual.md` are updated to describe **New →
Note** as the only way in, dropping the header-button and `Alt+N`
mentions.

## Related

- [ADR 0119](0119-capture-is-an-action-not-a-place.md) is the decision this
  one reverses, in part - the sheet, the `Auto` default and local-only
  classification it built all stand unchanged.
- [ADR 0116](0116-notes-are-documents-and-folders-are-the-manual.md) is why
  a note is a document in the first place, which is the premise this
  decision follows through on.
