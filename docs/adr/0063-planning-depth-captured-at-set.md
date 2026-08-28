# ADR 0063: Seed the planning depth from the drop, and let the operator edit it

## Status

Accepted.

Amends ADR 0047 (rode planner replaces the Rode & Scope tile). Extends ADR 0059
(always-on anchor map, Drop/Raise, scope method).

## Context

Scope is rode over depth from hawse. The Rode Planner divided by the *live* sounder
reading, taken fresh from the vessel SSE feed on every render, so the scope ratio and the
OK/Low/Insufficient badge moved with the boat's swing and with the tide. Sit at anchor long
enough and the reported scope changes for reasons that have nothing to do with the chain in
the water: the boat sails around its circle, crosses a hole, and a set that was good at 4 m
reads "Scope Insufficient" over 6 m without a link of chain moving.

The chain was paid out against the depth at the drop. That is the denominator, and nothing
recorded it. `anchorWatchData` persisted rode, sea state, seabed, `set_at`, the bow offset
and the heading at set, but no depth at all. This was never the planner choosing the wrong
one of two numbers. It only ever had one, and it was the wrong one for the question.

ADR 0047 retired a badge that graded deployed rode against an input nobody entered. This is
the other half of the same problem: the input that *is* known, and was being thrown away an
instant after it mattered.

## Decision

### 1. A planning depth is a depth and the tide height it was read at

`maxExpectedDepthM` plans against the next high water by adding `high - tide_at_the_reading`
to a sounder depth. That works today only because both halves are read in the same instant.
Feed it a depth captured at 19:00 and a tide height sampled at 02:00 and the answer is wrong
by the difference between the two tide heights, and wrong in the direction that recommends too
little chain whenever the tide has risen since the reading. A safety calculation does not get
to be approximately right in the unsafe direction.

So a depth never travels alone. The unit of currency in this code is a pair:

    planningDepth = planning_depth + max(0, high_ft - planning_tide_height_ft) / 3.28084

`TideToday` carries only the current height and today's high and low, with no way to ask what
the tide was doing at an arbitrary past moment. Re-deriving the height for an older reading
would mean pulling the tide chart extremes and interpolating between them, which is a second
data source, a second failure mode, and a different hook from the one every rode-plan caller
already holds. Stamping the reading when it is taken costs one float and cannot drift.

`maxExpectedDepthM` therefore takes the datum rather than a bare number, and measures the rise
from the datum's own stamp. Nothing in `rode-plan.ts` reads `tide.current_tide_height_ft` any
more except the one helper that stamps a reading being taken now.

This pairing is invisible to the operator. They see one depth field.

### 2. The drop seeds it, the server stores it

`planning_depth_m` and `planning_tide_height_ft` ride in the existing `POST /api/anchor-watch`
body. The drop handler already holds the live depth and the live tide, so the two are
contemporaneous by construction, which is exactly the property section 1 depends on.

The backend does not fetch either one for itself. Reading depth from the SignalK snapshot would
be defensible, since `setAnchorWatch` already pulls vessel state for the bow offset. Tide is the
problem: `/api/tide-today` calls the provider on every request with no cache in front of it, and
for BOM that is an FTP fetch. Nothing with a network round trip in it belongs in the path between
pressing Drop and having a watch running, least of all something that fails exactly when the
anchorage has no signal. Splitting the capture across both tiers to save one of the two from the
client is a worse trade than taking both from the client, where they are already sitting in the
same render.

The server's job is to store what the drop handed it, and to keep it.

### 3. One editable number, not a capture plus an override

The field is seeded at the drop and the operator can type over it. Editing overwrites. That is
the whole model, and it fits in one sentence on purpose.

An earlier revision of this ADR specified something more elaborate: an immutable capture taken at
the drop, a separate override pair that shadowed it without overwriting, a three-way
`'override' | 'set' | 'live'` provenance union, captions naming which reading was in play, and a
revert control offering "Use depth at set" or "Use live depth" depending on state. It was built,
and then cut on review.

The revert was the load-bearing piece, and it was gold plating. Everything else existed only to
serve it: the capture had to stay immutable so there was something to revert *to*, which forced
the override into its own pair of fields, which forced the provenance union so the UI could say
which one was winning, which put "At set" in front of an operator who has no reason to know what
that means while anchoring a boat. Removing one button removed four concepts.

What the operator loses is a safety net on a typo. Enter 85 for 8.5 and the drop-time reading is
gone; retype it. That is a real cost and it was argued for at the time. It was judged not worth
the four concepts, which is a legitimate call: the number is one field on screen, the boat is
right there, and an operator who mistypes a depth will see a recommendation that is obviously
wrong.

Two rules survive the simplification because they are correctness, not polish:

- **No silent fallback to live depth once the anchor is down.** With a watch active and no depth
  recorded and nothing typed, the planner names the reason and shows no figure. Substituting the
  live sounder in the one state where it is known to be the wrong answer is the original bug,
  and hiding it behind a plausible number is worse than showing nothing. Before the drop there is
  no such trap: live depth is not a fallback there, it is the correct input, because you are
  standing off deciding what to pay out.
- **The pair rule, enforced at the API boundary in both directions.** A depth arriving without its
  tide stamp is refused, and so is a stamp arriving without its depth. Either one alone would let
  a new reading inherit a stale stamp, which is the datum mixing from section 1 reintroduced one
  layer down.

### 4. Repositioning carries the depth forward

`POST /api/anchor-watch` is a full replace, and dragging the anchor marker on the map goes through
it. Correcting where you think the anchor lies says nothing about how deep the water was when it
went down, so the depth pair is carried forward from the active watch when the body is silent
about it, the way the radius already is.

On a genuine drop there is no active watch to carry from, because Raise deletes it first, so
nothing stale can follow you into a new anchorage.

### 5. The value is resolved in App.tsx, once

Three surfaces plan off this number: the Rode Planner, the tile's Scope row, and the drawer's
Scope row. ADR 0059 put `computeScopeRecommendation` in `lib/rode-plan.ts` precisely because the
tile's plan and the planner's plan had already drifted apart once when each seeded its own wind.
A planner-local depth would rebuild that bug with a different input.

So the pre-drop editable value is hoisted alongside `windBandId`, and `resolvePlanningDepth` is the
single place the rule lives: anchored uses the record, unanchored uses the live sounder. Both hosts
and the planner call it and none of them re-derive it, the same arrangement `resolvePlanningWindBand`
already has.

The field stays editable before the drop as well as after. "The Depth field is editable" is a simpler
rule to hold than "editable only once the anchor is down", and ADR 0047's whole premise is that the
useful moment for this calculation is *before* the hook goes down. There is nowhere to persist a
pre-drop figure, so it lives in React state and is cleared when a watch starts, which stops a what-if
leaking into a real set.

## Consequences

- The map overlay's Depth row still shows the raw live sounder, and now that the planner shows a
  different number that is a feature rather than a duplicate. Live depth against planning depth is
  the cross-check that tells you the boat has swung somewhere shallower than where the hook went
  down, which is worth seeing and is not what scope should be graded on.
- The caption under the Depth field says what it said before this work: tide adjusted, or sounder
  only when no tide station is configured. ADR 0047's rule that a missing station produces an
  explicit visible fallback rather than a silent substitution is unchanged. No new vocabulary
  reached the operator.
- Reading that caption, note that "tide adjusted" does not mean the number moved. The rise clamps
  at zero so a falling tide never reduces the planning depth, which means a correction of exactly
  nothing still reports as adjusted. That asymmetry is deliberate and predates this ADR.
- The watch currently running on the boat has no recorded depth, so the field is empty until one is
  typed. That is the honest reading of a record that genuinely does not contain the field, and one
  number fixes it. No migration, no backfill, and no guessed value standing in for a measurement
  nobody took.
- Dropping with a dead sounder leaves the planner unable to answer until a depth is typed. Before
  the drop, live depth drove it; after, nothing does. That is intended. A planner that answers
  confidently from the wrong depth is worse than one that asks.
- `RodePlanInput.sounderDepthM` is gone and `computeScopeRecommendation` takes the resolved depth
  inputs rather than a bare `depthMeters`. Every call site is compiler-checked, which is the point
  of changing the type rather than adding an optional field beside it.
- `patchAnchorWatch` now holds one write lock for the whole handler instead of reading under
  `RLock` and writing under `Lock` with the read-modify-write in between. Depth debounces on its own
  timer alongside rode and conditions, so changing sea state and depth inside the same 800 ms window
  fires two overlapping PATCHes, and the old structure would let one silently discard the other.
