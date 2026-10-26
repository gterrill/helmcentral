# ADR 0211: Recorded Passage Decisions

## Status

Accepted (2026-10-26). Amends [ADR 0191](0191-mate-cards-come-from-tool-results.md):
a stored card gains an `id`, and a `passage_window` card gains a `decision`.
Builds on [ADR 0210](0210-passage-legs-as-join-signs.md) (passages, merge and
join signs) and [ADR 0194](0194-passage-limits-are-no-go-only.md).

## Context

Mate's weather-window card answers "which day should we leave". Nothing records
what happened next. The operator wants the card, and later the passage itself,
to show whether the plan was sailed, which row of the grid they took, or
whether they only asked out of curiosity. They also want to rate a passage once
it is over, so that later limits can be judged against how it felt.

Three facts in the code shape the design:

- A card is `{kind, payload}` with no id, and its row cascades away with the
  conversation. A decision tied to the card by foreign key would vanish when
  the chat is deleted.
- The route points exist only in the tool's arguments. The payload keeps leg
  names and verdicts, not coordinates, and the tool-call log is truncated and
  cascades too. Matching a card to a track later needs the coordinates, and
  they must be captured at the moment the card is made.
- Passages are rewritten by split, merge, trim and hide. A stored passage id
  can therefore go stale at any time.

## Decision

1. **A decision is its own table, with no foreign key to the card or the
   passage.** `passage_decisions` lives in the assistant store, created from
   `createAssistantSchema`. `card_id` is unique but unconstrained: card ids are
   AUTOINCREMENT, so a dangling id can never point at a different card. It
   keeps `message_id`, `conversation_id`, the card's creation time, a verbatim
   copy of the payload, and the routes. `passage_id` is unconstrained as well,
   because merge deletes passages. The table outlives the chat.
2. **A decision has a state and a source.** State is blank, `went`, `stayed` or
   `just_looking`. `went` carries the `row_date` the operator sailed, and only
   `went` does; the schema checks both directions. Source is blank,
   `inferred` or `operator`. A state other than blank needs a source. An
   operator blank means "cleared, don't fill", and inference leaves it alone.
   Only the operator can set `just_looking`.
3. **Routes are captured at card insert.** `mateCard` gains `ID`, `Decision`
   and an unexported `routes`, one list of points per leg. The passage window
   tool sets `routes` from its arguments, and the card carries them through the
   sink and the reply without leaving the process. `insertAssistantCardsTx`
   writes each row id back into the card, inserts a blank decision in the same
   transaction, and refuses a `passage_window` card whose routes are missing,
   whose count differs from the legs, or whose leg has fewer than two points.
   The refusal fails the whole message, so a card that could never be matched
   is never stored half-usable. Routes never go on the wire.
4. **No backfill.** Cards made before this change have no routes to recover and
   get no decision and no control. The operator accepted this.
5. **The operator records a decision with `PUT /api/passage-decisions/:id`.**
   Write tier. The body is `{state, row_date?}`. The server answers 400 for an
   unknown state, `went` without a date, a date on any other state, or a date
   that is not a row of the card's own snapshot, and 404 for an unknown id. A
   successful call sets the source to operator, and returns the decision as
   `{id, state, row_date, inferred, passage_id}`.
6. **Inference fills the rest, and never freezes.** A worker matches cards to
   passages and writes `source 'inferred'`. It reconciles on a passage being
   stored (detection or GPX import), on a split, merge, trim or hide, once an
   hour, and on every operator PUT. Wakes carry a span and coalesce, but every
   pass works on the whole set, so winners, blocks and supersession are decided
   over every card that can compete. The set is each decision with a row date in
   the last 60 days, plus any decision linked to a passage in the woken span
   (hidden ones too) or to a passage that no longer exists, so an edit to an
   older passage still reaches its card. The operator's PUT saves the choice,
   then asks the worker for an immediate reconcile, which always includes that
   decision whatever the age of its card, and waits for it for at most 5
   seconds, so the answer already shows who holds the passage. If it fails or
   times out, that is logged, the saved choice is returned, and the worker or
   the next tick catches up.
   - A passage matches a row when it is not hidden, starts within 4 hours of
     the row's planned first-leg departure, begins within 2 NM of the first route
     point, comes within 2 NM of the first leg's end point along its track (never
     across a segment break), and starts after the card was made. The 4 hours
     are elapsed time, so a daylight-saving change counts as the hours it
     really was. No calendar date is compared: a boat that planned 23:00 and
     left at 00:30 left on that row. The card's `timezone` is used only to say
     which dates a card offers, and a zone that fails to load skips the
     decision rather than falling back to UTC.
   - When one card matches two passages, the start closest to the planned
     departure wins, the earlier on a tie. When one passage matches several
     cards, the latest card made before the passage starts wins, the higher id
     on a tie, and the others reset to blank. A card that loses, or that
     matches a passage an operator `went` already holds, stays blank rather
     than becoming `stayed`, because the boat did sail.
   - An inferred `stayed` needs the date to be past, the detector to have
     processed past local midnight at the end of that date, and the card not to
     be superseded by a later card with the same row date and first-leg ends within
     2 NM. A superseded card stays blank. So does a card for which a passage
     left on the plan (the 4 hour window and the 2 NM start radius for some
     row) but none reached the first leg's end, whether the boat turned back or the
     passage was split: the operator did go out, and "stayed" would be wrong.
     The same holds when a visible passage of unknown source started within the
     4 hours of any row after the card was made: its track cannot be read, so
     nothing can say the boat stayed. Hidden passages are ignored, as the
     operator asked.
   - Each card claims its best arrived passage, latest card first. A card whose
     best passage is blocked by an operator went, or already claimed by a later
     card, tries its other arrived passages in the same order (closest start,
     then earlier) before it is left blank.
   - Inferred values are rewritten on every reconcile, so a late passage turns
     an inferred `stayed` into `went`. Writes are guarded with
     `source <> 'operator'`, so an operator PUT always wins a race. The link
     `passage_id` is always derived: hiding a passage drops its inferred `went`
     and unhiding restores it, a merge keeps the link on the earliest passage
     if it still matches, and split and trim recompute it. An operator `went`
     links to the best passage matching the row the operator chose, or to none.
7. **A passage carries a rating.** This lands in the next commit of the same
   branch and is not in the code this record first shipped with. `passages.rating`
   holds blank, `comfortable`, `lively` or `shouldnt_have_gone`, checked in Go
   and set through `PATCH /api/passages/:id`. A rating-only patch does not wake
   the reconcile. A split gives the second half no rating, and a merge keeps the
   earliest passage's rating, or the next one that has any.
8. **Each passage records its track source.** `passages.source` holds
   `recorded` or `imported`, the same values as `track_points.source`.
   Detection writes `recorded`, GPX import writes `imported`, a split gives
   both halves the source, and a merge keeps the source when every merged
   passage has the same one. When they differ, or one is unknown, the merge
   still goes ahead and the result has no source, because points of two
   sources cannot be read as one track. Such a passage is left out of matching. Matching reads only the
   passage's own source, so an overlapping import's points never become a
   recorded passage's first point or break its segments. Passages with
   no source are looked at on every start and filled only where the span holds
   points of exactly one source, so a row that could not be named at one start
   is named at a later one; a span with points of both, or of none, keeps an
   empty source. Matching leaves such a passage out and logs it once,
   instead of guessing, and it blocks an inferred stayed as above. The passage page and the stats still read the mixed
   track.
9. **Mate is unchanged here.** `list_passages` reads InfluxDB and has no
   passage ids. Letting Mate read decisions and ratings waits for the work that
   proposes limits from them.

## Rejected

- **A foreign key from the decision to the card.** It cascades away with the
  chat, and the passage page should still say "planned with Mate" after the
  conversation is deleted.
- **Storing the decision in the card payload.** The payload is verbatim tool
  output, shared by every kind, and has no place to guard against a write race.
- **Reading routes back from the tool-call log.** The log is truncated and
  cascades, so it cannot serve as a record.
- **A foreign key to the passage.** Merge deletes passages, and a constraint
  would force the merge to know about decisions.
- **Freezing inferred values once written.** A passage detected an hour late
  would leave a wrong `stayed` on the card for good.

## Consequences

- The card list and `GET` of a conversation now carry `id` on every card, and
  `decision` on a `passage_window` card that has one. Clients must keep
  accepting a card with neither.
- A deleted conversation leaves its decisions and the payload copies behind.
  The table grows by one small row per window card.
- Inferred values can change after the operator first sees them. The help page
  says so, and an operator choice is the way to stop it.
- The branch lands in steps: storage and the PUT, then inference and the
  derived passage link (both in), then the rating and the passage page's
  `planned_with_mate` list, then the frontend.
- Merge in ADR 0210 folds any number of passages into the earliest, so the
  rating and link rules above read "earliest" where an earlier draft said "a".
