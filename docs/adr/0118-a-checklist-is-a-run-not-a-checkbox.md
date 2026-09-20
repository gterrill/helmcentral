# ADR 0118: A Checklist Is A Run, Not A Checkbox

## Status

Accepted (2026-09-20). Phase 4 of the plan
[ADR 0116](0116-notes-are-documents-and-folders-are-the-manual.md) opened.
ADR 0116 named this ADR's number in advance ("ADR 0118 will cover checklist
runs, when that phase ships") — this is that phase.

## Context

A note's checklist is GFM task-list syntax in its body: `- [ ] Seacocks
open`. ADR 0116 already renders that read-only wherever a note is read, and
ADR 0117's editor round-trips it losslessly. Neither of those decisions
says anything about what happens when an operator actually runs the list —
taps down the seacocks one by one during a shutdown, gets called away at
item four, and comes back to it twenty minutes later.

The obvious shape — write `[x]` into the Markdown as each box is ticked —
fails the moment it is thought through:

- **The note is shared, the run is not.** Genset start-up is one note. It
  gets run every time the genset starts. If ticking a box edits the note's
  own body, every run overwrites the last one's marks, and there is no way
  to tell "started but not finished" from "finished three weeks ago" — both
  are just whatever `[x]`s happen to be in the file right now.
- **It collides with the editor.** ADR 0117's editor treats the file's
  bytes as the note's identity and re-serialises on every save. A `[x]`
  written mid-run by the operator ticking boxes, then silently reformatted
  the next time someone opens the note to fix a typo, is exactly the kind
  of quiet data loss neither ADR intended and neither would notice.
- **It cannot survive an edit.** The whole reason a checklist gets edited
  at all is that the boat itself changed — a new seacock, a reworded step.
  Position-indexed state (“item 4 is ticked”) breaks the instant the list
  reorders or gains a line above item 4. Text-indexed-but-exact state (“the
  literal string `- [ ] Seacocks open` is ticked”) breaks the instant
  someone bolds a word.

So a checklist run needed to be its own thing: separate from the note's
body, surviving edits that do not change the step's meaning, and honest
about edits that do.

## Decision

### Ticks are keyed to normalised text plus occurrence, never position

A run does not store "item 4 is ticked." It stores a set of `(item_key,
occurrence)` pairs, where `item_key` is the sha256 of the item's own
normalised plain text — leading `- [ ]`/`- [x]`/`* [ ]` marker stripped,
`*`/`_`/`` ` `` inline emphasis and code delimiters stripped, internal
whitespace collapsed, trimmed — and `occurrence` is which same-key line
this is within the body, so two genuinely identical steps (a boat with two
seacocks, both checked the same way) stay distinct rather than colliding on
one key.

`- [ ] **Seacocks** open` and `- [ ] Seacocks open` therefore key
identically. This is not a nicety; it is what stops ADR 0117's editor from
being a hazard to a live run. The WYSIWYG editor reformats a note's
Markdown on every save — a different bullet marker, a reflowed table, an
emphasis placement it prefers — and none of that is a change to what the
step *means*. Keying on the raw Markdown line would have made "fix a typo
in an unrelated line, then save through the editor" silently invalidate
every tick in a run in progress. Keying on plain text does not.

Reordering the list has the identical property for the identical reason:
the key carries no position, so moving "Start blower" above "Check bilge"
touches neither item's key.

### Two independent implementations of the same stated rule, not one importing the other

The normalisation is implemented twice: `normalizeChecklistItemText`
(`frontend/src/lib/checklist-item-text.ts`, written in Phase 2b against
this phase's eventual need) and its Go equivalent
(`normalizeChecklistItemText`, `backend/notes_format.go`). Neither imports
the other — there is no Go/TS shared module in this codebase — and both
commit to the same wording ("strip the leading task marker, strip inline
emphasis/code delimiters, collapse internal whitespace, trim") as the spec
they each implement against.

`TestNormalizeChecklistItemText` (`backend/notes_format_test.go`) runs the
identical fixture lines `checklist-item-text.test.ts` already asserted
against in Phase 2b, so the two cannot quietly drift onto different rules
while both still pass their own suite. This is the same discipline ADR
0117 already applied to this exact function for the identical reason —
see that ADR's "Checklist items key on plain text, not raw Markdown".

### An exact-match miss is surfaced, never reconciled

Every read of a run re-parses the note's *current* body into an ordered
item list, computes each item's key, and left-joins the run's tick rows
against it. Three outcomes fall out of that join, and only three:

- A current item with a matching tick: **checked**.
- A current item with no matching tick: **unchecked**.
- A tick with no matching current item: **changed** — surfaced in its own
  group, carrying the *stored* text (what the operator actually ticked),
  under the heading "edited since you ticked it — re-check."

There is no fourth outcome where a changed tick gets quietly reattached to
whichever current item looks closest. AGENTS.md's fallback policy forbids
exactly that kind of fuzzy re-matching, and here it is not an abstract
rule: the one thing worse than an operator having to re-check a step is an
operator believing a step is done because the software guessed it was
still the same step. An exact match is deterministic and testable. A
fuzzy one is a judgement call made silently, on the operator's behalf, by
code that cannot see the boat.

Nothing deletes a changed tick, either. It stays in the changed group,
under its old key, until either the operator re-ticks the corresponding
current item (which mints a fresh tick under the *new* key — the old one
is simply never referenced by anything again) or the run itself closes.
There is no sixth endpoint to acknowledge or dismiss a changed entry; the
plan cut that deliberately (see Rejected) as a nicety this phase does not
need to ship.

### `[x]` in the body is not the run's state — every new run starts unticked

`StartOrResumeChecklistRun` parses the current body for its item *list*
and never reads which markers are `[x]` versus `[ ]` to seed tick state. A
fresh run starts with nothing checked, full stop, even against a body
someone hand-authored (or imported) with boxes already ticked. The
Markdown's own `[x]`/`[ ]` is authoring syntax describing what a step
looks like before anyone has run it — a template — not a record of any
particular run. Conflating the two was the position-indexed design's
original sin (see Context), and this phase does not repeat it in a
different shape.

### One active run per note, resumed rather than duplicated

```sql
CREATE UNIQUE INDEX IF NOT EXISTS note_checklist_runs_active
    ON note_checklist_runs (document_id) WHERE completed_at IS NULL AND abandoned_at IS NULL;
```

`POST /api/notes/:id/checklist-runs` checks this before inserting: a note
with an open run gets that same run back, `200 {run, resumed:true}`,
rebuilt against whatever the body currently says (which is exactly how a
changed item gets noticed the next time the operator opens the run, even
if nobody has looked at it since the edit). A note with no open run gets a
fresh one, `201 {run, resumed:false}`. Modelled deliberately on the upload
path's `{document, duplicate:true}` (ADR 0106) rather than a 409, so the
frontend has exactly one pattern for "you already have this, here it is"
across both features, and a double-tap on "Start checklist" is a resume,
not an error.

`PATCH /api/checklist-runs/:runId/items` — a single tick or untick — always
returns the *whole* rebuilt run, never a per-item acknowledgement. The
operator ticking a box may be the last thing that happens before they walk
away from the screen to go deal with whatever the checklist was for;
handing back a delta and trusting the client to merge it into whatever it
last had would be exactly the kind of state a missed response, a stale
tab, or a second device could silently disagree with. The whole run,
every time, means the client never has to trust its own memory.

`DELETE /api/checklist-runs/:runId` sets `abandoned_at` and — this is the
whole method — touches nothing else. It does not delete the row, and it
does not touch a single tick. An abandoned run keeps its own history
exactly as a completed one does; the only difference between the two is
which timestamp is set, and closed is closed either way (a completed *or*
abandoned run rejects further ticks, `errChecklistRunClosed`).

### Run state lives in `documents.sqlite`, on purpose

`note_checklist_runs` and `note_checklist_run_ticks` are new tables in
`documentStoreSchema`, not a new store and not `assistant.sqlite` or any
other existing database. Two things follow from `ON DELETE CASCADE` on
both foreign keys (`note_checklist_runs.document_id → documents.id`,
`note_checklist_run_ticks.run_id → note_checklist_runs.id`):

First, deleting a note deletes every run and every tick it ever had, in
one cascade, with no sweep, no orphan-detection job and no manual cleanup
code anywhere in this feature. This is the entire orphan story ADR 0116
already leaned on for a note's blob-on-disk; the same foreign-key
machinery does the identical job here for a run's rows.

Second, and the reason this matters operationally rather than just
architecturally: ADR 0106's backup unit is `documents.sqlite` plus the
`DOCUMENTS_DIR` blob tree, backed up together. A checklist run living in
that same database means a half-finished shutdown checklist is *in the
backup* — a restore after a crash mid-genset-shutdown recovers exactly
where the operator was, not just the note they were reading. Splitting run
state into a separate store would have split that guarantee too, for no
offsetting benefit; the write volume here is one short single-row
statement per tick, nowhere near enough to justify a dedicated store.

### The runner is a mode of the reading view, not a panel

`frontend/src/components/documents/checklist-runner.tsx` is one component,
used unmodified from both places a note is read: `documents-panel.tsx`'s
viewer Sheet and `documents/manual-folder-view.tsx`'s reading pane (the
"one panel, not three" revision means there are exactly two, not three —
see ADR 0116's own addendum). "Start checklist" swaps `NoteMarkdown` for
`ChecklistRunner` in place; there is no route, no second Sheet and no
navigation event, consistent with ADR 0074's existing rule that a note's
reading and editing sessions stay out of the URL.

The sizing is a helm-screen argument, not a stylistic one. A checklist is
run one-handed, standing, on a boat that may be moving, often while the
other hand is on something else entirely — a fuel valve, a grab rail. Rows
are `min-h-14 p-4` with an `h-6 w-6` checkbox, well past this app's
ordinary 44px hit-target floor, because a checklist item is the one control
in this entire application most likely to be tapped without looking at it
first.

Completed rows are `line-through text-muted-foreground` and stay exactly
where they were in the list — the runner renders `run.items` in the
server's own current-body order and never re-sorts by checked state. A
list that reorders itself under a thumb mid-tap is the specific failure
this rule exists to prevent: the operator's next tap has to land on what
they were aiming at, not on whatever slid underneath it because the last
item they checked jumped to the bottom.

A sticky header carries the progress count (`3 / 11`) and elapsed time,
and — the property that makes resuming worth building at all — landing on
resume shows "Started 14:22, 3 of 11 done" and scrolls straight to the
first unchecked item, rather than making the operator scan a list they
already half-finished to find where they stopped.

## Rejected

### Reconciling a changed item back onto the nearest-looking current one

Considered and dropped before it was ever built, not cut after. Fuzzy
matching an orphaned tick onto "the item that looks most like it now" is
precisely what AGENTS.md's fallback policy names as forbidden, and the
cost of getting it wrong here is not a cosmetic glitch — it is a skipper
trusting a checked box that no longer means what they checked it for.

### An acknowledge/dismiss endpoint for changed items

A sixth endpoint that lets the operator clear a changed entry without
re-ticking the current item it used to correspond to. Cut for this cycle:
the changed group is informative, not blocking, and re-ticking the actual
current item is already exactly one tap. If boat use shows the changed
group accumulating clutter across many edited notes, this is cheap to add
later — it is one more `DELETE` on a tick row, not a schema change.

### Storing tick state in the note's own Markdown

Addressed at length in Context. The short version: a note is shared across
every run it will ever have, and `[x]`/`[ ]` in the file is authoring
syntax for what a step looks like, not a record of any one time it was
performed.

### Checklist run history

No endpoint lists a note's past runs, and no UI shows one. A run's own row
survives forever once started (nothing ever deletes it except cascading
with its document), so the data for a future "when did we last do this"
view already exists — this phase just does not build a way to see it. Cut
as out of scope, not as a design objection.

## Consequences

Five endpoints (`POST`/`GET .../active` under a note id;
`PATCH`/`POST .../complete`/`DELETE` under a run id) register in
`buildAPIRoutes` (`main.go`), the only legal place per
`TestAPIRouteCoverage_EveryRegisteredAPIRouteHasATier`. Four new sentinels
join `documentErrorStatus`: `errChecklistRunNotFound` and
`errChecklistItemNotFound` at 404, `errNoteHasNoChecklist` and
`errChecklistRunClosed` at 409.

`GET /api/notes/:id` grows two fields — `checklist` (the current body's
template items, present even with no run, so a reader can decide whether
"Start checklist" belongs on screen at all) and `active_run` (that note's
open run, or `null`) — so one call still serves both the editor and the
reader, the same contract that endpoint has carried since Phase 1.

`frontend/src/hooks/use-checklist-run.ts` is the runner's own small data
layer, the same no-react-query, one-instance-per-caller idiom
`use-notes.ts` and `use-manuals.ts` already use; every action it exposes
sets state straight from the server's response, never merged with
whatever the caller already held, which is what makes "returns the whole
run" (above) actually load-bearing rather than a documented-but-unused
guarantee.
