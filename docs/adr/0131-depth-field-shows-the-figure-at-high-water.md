# ADR 0131: The Depth field shows depth at high water, not the raw reading

## Status

Accepted. Amends [ADR 0063](0063-planning-depth-captured-at-set.md), which is
otherwise unchanged: the stored pair (a raw depth plus the tide height it was
read at) is still exactly what persists, and this ADR does not touch it.

## Context

ADR 0063's Depth field held the raw sounder/recorded reading, with the
tide-corrected figure the plan actually computes against moved into the
caption underneath. That ADR's own consequences section called out the
asymmetry this produced: the number in the field and the number the badge
above it grades scope against could differ by the better part of a metre,
and the field never told you by how much.

The operator wants the opposite: the field should show the figure they are
actually planning against — depth at this spot at the next high tide, plus
bow roller height, i.e. depth-from-hawse at high water — because that is the
number that determines whether the chain paid out will still float the boat
off the bottom six hours from now. The raw sounder reading is a useful
cross-check, not the planning figure, and ADR 0063 already put the live
sounder reading on the map overlay's Depth row for exactly that cross-check;
this field does not need to repeat it.

## Decision

The Depth field now shows `planningFigureM`: the datum's tide-corrected
depth (falling back to the raw datum depth when there is no tide stamp or no
forecast, the same null ladder `maxExpectedDepthM` already uses) plus bow
roller height. Worked example: 1.0 m raw at low tide, 3.0 m of rise to the
next high, 1.5 m bow roller — the field shows 5.5.

The stored model is untouched. `PlanningDepthDatum` is still a raw depth
paired with the tide height it was read at, and that pair is still what
`onPlanningDepthChange` persists. Only the editable surface changes: the
field displays a derived figure instead of the datum's own `depthM`.

### The inversion

Seeding the field with a corrected number creates a trap ADR 0063 warned
against: if a typed override were stored as the raw reading, the next render
would tide-correct it again, and the figure would climb by the rise every
time the tide forecast refreshed. Avoiding that requires inverting the
correction exactly, not approximately.

`rawDepthFromPlanningFigureM(figureM, tideNowFt, tide, bowRollerHeightM)` is
that exact inverse of `planningFigureM`. It subtracts bow height and the
rise from `tideNowFt` — the tide at the moment of typing, which is also what
the persisted datum gets stamped with — to the next high, using the same
null-to-zero-rise rule `maxExpectedDepthM` applies (no tide reading, no
forecast, or a falling tide all clamp the rise to zero rather than
subtracting a negative number). Concretely it asks `maxExpectedDepthM` for
the rise alone, by feeding it a zero-depth datum stamped with `tideNowFt`,
rather than re-deriving that null ladder a second time.

Persisting `rawDepthFromPlanningFigureM(x, tideNow, tide, bow)` stamped with
`tideNow`, then reading `planningFigureM` back off that datum, returns `x`
exactly. That round trip is what makes typing over the field safe: the
operator edits the figure they can see and reason about, and the raw
reading the pair-rule (ADR 0063 §3) requires gets derived, not asked for.

### Typing below the floor

A typed figure at or below bow height plus the tide rise implies a raw
depth of zero or less, which is not a sensible reading — there is no tide
correction that turns "5.5 m at the hawse" into "the seabed is at or above
the roller." `rawDepthFromPlanningFigureM` returns null in that case, and
the component does not persist it. The caption reports it instead of
silently keeping the last accepted value or substituting anything, matching
the fallback policy the rest of this field already follows for a missing
reading.

## Consequences

- The caption's job changes from naming *whether* a correction happened to
  naming *what the figure in the field already includes*: "High tide + 1.5 m
  bow" when tide-corrected, "Sounder + 1.5 m bow, no tide station" when not.
  ADR 0063's rule that a missing tide station produces an explicit, visible
  wording rather than a silent substitution is unchanged.
- The map overlay's Depth row is still the raw live sounder, and the
  cross-check ADR 0063 §Consequences describes (a boat swung into a hole
  reads shallower live depth than the planning figure implies) still holds;
  only the Rode Planner's own field changed.
- A typed figure that doesn't clear bow height plus tide rise is refused,
  visibly, rather than stored as a zero or negative depth.
- `lib/rode-plan.ts` gains `planningFigureM` and `rawDepthFromPlanningFigureM`
  as the one place this display/inversion pair lives, alongside
  `maxExpectedDepthM` which both are built from.
