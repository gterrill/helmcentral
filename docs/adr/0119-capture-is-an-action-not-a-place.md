# ADR 0119: Capture Is An Action, Not A Place

## Status

Accepted (2026-09-20). The global header action and Alt+N are superseded by
ADR 0121; the rest stands.

## Context

[ADR 0116](0116-notes-are-documents-and-folders-are-the-manual.md) built a
note as a document and a manual as a folder, then shipped both behind a
dedicated Notes panel: a sidebar row whose whole page was a capture textarea
above an unfiled-notes list. That panel's own comment called out its
guiding rule directly — "capture is global, retrieval is a panel" — and
then built capture as the top of one specific panel anyway, because at the
time that panel was also the only place retrieval happened.

Once Documents absorbed retrieval (ADR 0116's addendum), that reasoning
stopped applying. Capture sitting at the top of a Documents view is no
better than it was at the top of a Notes view: a skipper mid-repair with an
observation worth writing down is, by definition, not already looking at a
file listing. The plan's own R1 requirement — "capture is one action
producing unstructured text, with no mandatory filing, titling or
formatting" — says nothing about capture living on any particular screen,
and the postures capture actually happens in (wet, one-handed, mid-task)
argue against making the operator navigate anywhere first.

## Decision

### A global header action and a keyboard shortcut, not a panel

The header carries a **Capture a note** button (`App.tsx`, beside Help and
Ask Mate) on every screen except the wall-display kiosk, and `Alt+N` opens
it from anywhere the same way `Alt+M` already opens Mate's push-to-talk.
Both open the identical `NoteCaptureSheet` — a right-side Sheet, not a
navigation — so capturing a note never leaves whatever the operator was
looking at. Gated on `canWrite`, the same guard every other write
affordance in the header already carries; there is no read-only reason to
hide it beyond that.

Documents also carries **New → Note** in its own toolbar, opening the exact
same sheet through a callback App.tsx passes down (`onCaptureNote`), not a
second component. Two reachable paths, one surface: a returning operator
uses the header action out of habit, a new one finds it inside Documents
because that is where "make a new thing" already lives for folders.

This is the same sheet-over-navigation pattern the app already uses for
Ask Mate (open the Mate sheet without leaving the dashboard) and for Help —
ADR 0074's rule that a sheet stays out of the URL applies here unchanged:
opening the capture sheet writes nothing to `window.location`.

### A synthetic `Auto` type default

The sheet's type select defaults to a value that is not one of the six real
`note_type`s: `Auto`. This is deliberately not the same choice ADR 0116
made for the inbox row's icon, which showed a real classified type the
moment the note existed. Capture happens *before* classification does, so a
pre-filled real type would be showing a guess as though the operator had
already confirmed it — a wrong guess would be invisible, since nothing
distinguishes "the system decided" from "I decided." `Auto` says plainly
that a decision has not been made yet, and picking a real type becomes a
visible, deliberate override rather than a correction to something that
looked chosen.

`Auto` maps onto the existing `note_type_source` values with no schema
change: leaving it selected means the POST carries no `type` field at all,
and `createNoteHandler` (`backend/notes_handlers.go`) already treats an
absent `type` as "classify it" —

```go
noteType := strings.TrimSpace(req.Type)
if noteType != "" {
    // operator supplied it
} else {
    noteType = classifyNoteType(req.Body)
    // note_type_source: "auto"
}
```

— setting `note_type_source: 'auto'`. Picking an explicit type in the sheet
sends `type: "<value>"`, which the same handler records as
`note_type_source: 'operator'`, and `SetNoteTypeIfNotOperator`
(`backend/notes_store.go`) already refuses to let anything — Mate's later
enrichment included — overwrite an operator's choice. `Auto` is therefore
not a seventh value threaded through the backend; it is purely a frontend
convention for "send nothing," resolved entirely by logic ADR 0116 already
shipped.

### `Auto` resolves locally, and only ever locally, at capture time

`classifyNoteType` is pure Go running against the note body already in
memory — no network call, no OpenRouter round trip, no dependency on Mate
being configured at all. Choosing `Auto` in the sheet can never make capture
wait on anything beyond the `POST /api/notes` itself. This is not an
optimisation; it is load-bearing. ADR 0116 states the rule this feature
answers to directly: **Mate is an accelerant, never a dependency.**
PRODUCT.md promises no cloud account or subscription, so an operator with no
OpenRouter key — or simply no signal — is a fully supported user, and the
one moment this whole feature exists to serve (writing something down the
instant it happens) is exactly the moment a network call is least
acceptable to gate on.

Mate may still upgrade `auto` to `mate` later, during enrichment, precisely
as ADR 0116 §"Type is a column, derived locally, and the operator always
wins" already specifies. Nothing about `Auto`'s meaning at capture time
changes that later, optional step.

## Rejected

### Pre-filling a guessed type

Running `classifyNoteType` against whatever the operator has typed so far
and showing that type as the selected value, live, was the first design
considered. It fails the same test a moment's more thought about R2 (type
must be pre-attentively visible, "seen, not read") already answers for
retrieval: a value that looks chosen invites less scrutiny than one flagged
as provisional, so a wrong guess would ship silently far more often than an
explicit `Auto` default does. It also couples the sheet to running the
classifier on every keystroke for a benefit — seeing the type update as you
type — nobody asked for.

### Making `Auto` call Mate when it is configured

If Mate is on, why not let `Auto` ask it for a better type than the local
heuristic can manage? Rejected on two grounds, one architectural and one
about consent. Architecturally, it reverses the "never blocks on the
network" property this ADR's whole point is to guarantee — the instant
`Auto` sometimes calls out and sometimes doesn't, the operator can no longer
predict whether pressing Capture will hang on a bad connection. On consent:
ADR 0116 already set `enrich = false` as the default for a captured note,
on the reasoning that a one-tap capture at the helm is not the same
deliberate act as an upload, and *"ring Dave about the mooring, 0412…"* is
exactly the kind of note an operator would not expect to leave the boat
unprompted. Sending the body to OpenRouter merely to pick an icon reopens
that question through the back door. This is flagged rather than silently
assumed: making `Auto` genuinely AI-backed on every capture is a real
design that could be built later, but it is a consent change, not a
capture-flow tweak, and it needs its own decision, made out loud, separate
from this one. Until that decision is made, `Auto` means the local
classifier and nothing else.

### A separate capture route or panel

Keeping some dedicated `/notes/capture`-shaped surface, even a small one,
was considered as a way to give capture a bookmarkable URL. Rejected for
the same reason ADR 0074 keeps every other sheet — the reader, the editor,
Mate, Help — out of the address bar: a sheet is a mode over whatever screen
was already open, not a destination, and a URL implies the latter. Nothing
about capture benefits from being linkable; everything about it benefits
from opening instantly over whatever the operator was already doing.

## Consequences

The header gains one more icon, gated the same way the existing write
affordances already are, and `Alt+N` joins `Alt+M` as the shell's second
global shortcut — both ignored while the focus target is an editable field,
so neither fights ordinary typing.

`note-capture-sheet.tsx` is lazy-loaded like every other sheet in the shell
(`mateSheetHasOpenedRef`'s pattern, reused verbatim for
`captureHasOpenedRef`), so it costs nothing in the entry chunk and nothing
on first paint; its own `useNotes()` instance only ever fetches once the
operator has opened it for the first time.

Nothing under `backend/` changed. `Auto`'s entire behaviour is already
`createNoteHandler`'s handling of an absent `type` field, which ADR 0116
shipped; this ADR only decided what the frontend's default selection means
and documented why it must stay that way.

## Related

- [ADR 0116](0116-notes-are-documents-and-folders-are-the-manual.md) is the
  substrate this sits on top of, including `classifyNoteType`,
  `note_type_source` and why enrichment defaults off for a note.
- [ADR 0117](0117-markdown-is-the-storage-format-not-the-authoring-format.md)
  covers the editor `NoteCaptureSheet`'s own Edit toggle reaches, once a
  captured note is opened again to be worked on.
- [ADR 0074](0074-deep-links.md) (path URLs) is why this sheet, like Mate
  and Help, stays out of the URL entirely.
