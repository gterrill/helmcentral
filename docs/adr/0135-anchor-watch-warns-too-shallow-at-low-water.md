# ADR 0135: Anchor Watch warns when the boat will be too shallow at low water

## Status

Accepted.

## Context

Anchor Watch already tracks position against a swing circle and computes a
tide-corrected planning depth for the Rode Planner (ADR 0063, ADR 0131). None
of that answers a different question the operator asks constantly while
anchored: will there still be enough water under the keel when the tide
bottoms out, without moving the boat and without doing the arithmetic by
hand? A depth that reads comfortably now can be well short of the vessel's
draft six hours later, and the first anyone notices today is the keel
finding it.

Two facts from the live SignalK server this feature was built and verified
against shape the decision:

- `design.draft` only ever publishes `maximum` (nested under a `value`
  wrapper on the REST tree, same shape `design.length.overall` uses).
  `current`, `minimum` and `canoe` are never present. Reading only `maximum`
  is also the conservative choice even where a live source did publish the
  others: it is the worst case, and a warning about running aground should
  not be tuned to the shallowest number the boat could momentarily present.
- Depth is published only as `environment.depth.belowTransducer`. There is
  no `surfaceToTransducer` offset anywhere on this installation, so the true
  distance from the waterline to the seabed is somewhat more than
  `belowTransducer` reports, by however far the transducer sits below the
  surface. This warning inherits that under-read. It is conservative in the
  safe direction: the boat has at least as much water as this warning says,
  never less.

## Decision

A new pure function, `computeLowWaterClearance`, takes the live depth
(`environment.depth.belowTransducer`), the vessel's draft
(`design.draft.maximum`), today's tide, the instant to reason from (`now`),
and the operator's configured margin, and returns one of three states:

- **`unknown`**, with a reason (`no_depth`, `no_draft`, `no_tide`,
  `tide_stale`) naming exactly what is missing, checked in that order. No
  fallback value is substituted for any of the four (not a zero tide fall,
  not an assumed draft, not a stale reading treated as current), matching the
  rest of this app's fallback policy. A missing or untrustworthy input is
  reported, not guessed around.
  - A tide height counts as present when it is a finite number greater than
    -1 - not `>= 0`. Heights below chart datum are real water, not an error:
    a spring low commonly reads a small negative number, and that is exactly
    the case this warning has to catch. Only `useTideToday`'s own -1 sentinel
    (field not published this poll) means "no reading". This rule lives only
    in `computeLowWaterClearance`; `rode-plan.ts`'s `tideHeightFtOrNull`
    keeps its own `>= 0` rule for the Rode Planner's separate arithmetic and
    is deliberately not touched by this change.
  - A tide reading counts as current, not `tide_stale`, only when both:
    its own timestamp is 30 minutes old or less (`TIDE_STALE_AFTER_MINUTES`),
    and its named "next low" is still ahead of `now`. Depth is read live on
    every render; the tide is only as fresh as `useTideToday`'s last
    successful fetch, which the hook keeps serving indefinitely if every
    later poll fails. Without this check a boat could sit on a tide reading
    from hours ago, or reason about a low that has already come and gone,
    and still show a confident `ok`. An unparseable timestamp on either field
    is treated the same as stale rather than let it flow into `NaN`
    arithmetic.
- **`ok`** or **`too_shallow`**, computed as:
  1. `fall = max(0, currentTideHeight - nextLowTideHeight)`. The boat is
     between now and the *next* low, so if the tide is already at or below
     that forecast low's height, there is no further fall to plan for and
     the shortfall is zero rather than negative.
  2. `depthAtLow = liveDepth - fall`
  3. `clearance = depthAtLow - draft`
  4. `too_shallow` when `clearance` is less than the operator's configured
     margin, otherwise `ok`.

The margin is a new setting, `anchor.min_clearance_at_low_m`, in metres,
defaulting to 0.5 m. That default is a generic seamanship margin, not a
figure derived from any particular vessel's draft or hull, and the operator
is expected to set their own once they know their boat's handling in
skinny water.

This checks the *next* low only, never further ahead. `useTideToday`
surfaces one upcoming low extreme, and the diurnal tide can produce a lower
low the following cycle that this warning does not see.

This is a display warning on the Anchor Watch tile and drawer, not an alarm
rule: it does not sound, log to the alarm history, or gate anything else. It
answers "should I be worried," continuously, the same way the Scope row
already does, rather than firing once and waiting to be acknowledged.

## Consequences

- The warning only ever reasons about the *next* low tide. A boat anchored
  well ahead of a spring cycle that bottoms out two lows from now gets no
  warning until that cycle is the next one up. This is a known, accepted
  gap, not an oversight: extending it requires a forecast of more than one
  extreme, which `useTideToday` does not currently provide.
- It is a quiet, continuously-recomputed line, not an alarm: nothing sounds,
  nothing needs acknowledging, and nothing is logged. An operator who wants
  to be woken for this has to keep watching the tile, the same tradeoff the
  Scope row already makes.
- The depth it reads is the boat's position right now. A boat swinging on
  its rode moves over different bottom continuously, so the warning can
  flip between `ok` and `too_shallow` as the boat swings without the tide
  or the anchor itself having changed at all. It answers "is this spot,
  right now, going to be too shallow," not "is anywhere inside the swing
  circle too shallow."
- Because `belowTransducer` under-reads true depth by the transducer's own
  immersion, and only the vessel's maximum draft is used, every number this
  warning produces is at least as conservative as reality: it will warn at
  least as early as the true clearance does, never later.
- `useTideToday` polls on a ~10 minute cadence, so even a reading well inside
  the 30 minute freshness window can still lag the true current height by
  however far the tide moved in that gap - this is not the `tide_stale`
  case, it is the ordinary slack between polls. `fall = current - nextLow`
  uses that reading, and on a rising tide the true height right now is
  always at least as high as the stale one, which under-states `fall` and so
  slightly over-states `depthAtLow`: the one place this warning's numbers are
  not automatically conservative the way the `belowTransducer`/max-draft
  under-read above is. The operator's configured margin
  (`anchor.min_clearance_at_low_m`) needs to absorb this too, not just be set
  to the vessel's bare minimum clearance.
- Going stale (`tide_stale`) reads identically to a missing tide station on
  the tile and drawer - the operator sees "Tide forecast out of date," not a
  number computed from data that might no longer be true. It clears itself
  the moment the next successful poll lands.
- `frontend/src/lib/low-water-clearance.ts` is the one place this arithmetic
  lives, mirroring how `lib/rode-plan.ts` centralises the tide-corrected
  planning depth math it deliberately does not share with.
