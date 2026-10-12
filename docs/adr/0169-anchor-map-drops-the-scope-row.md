# ADR 0169: The Anchor Watch map drops the Scope row

## Status

Accepted (2026-10-12). Supersedes §3 of [ADR 0059](0059-always-on-anchor-map-drop-raise-and-scope-method.md).

## Context

ADR 0059 §3 put the recommended rode in the map's metric overlay as a Scope row under
Current, fed by a `scopeRecommendation` prop that both the tile and the fullscreen drawer
computed. Once anchored, the operator watches the overlay for how far the boat has swung
and how much room is left: Distance, Bearing, Radius and Current. The recommended rode is
a planning figure for before the drop, and the Rode Planner already shows it with its
inputs in view. On the map it was a second copy of a number that answers a different
question.

## Decision

- The map overlay shows Distance, Bearing and Radius (with an anchor set) and Current. No
  Scope row.
- `AnchorWatchMap` loses the `scopeRecommendation` prop. The tile and the drawer stop
  computing a recommendation for it, and the tile drops the wind, forecast-band and
  planning-depth props it took only to feed that computation.
- `computeScopeRecommendation` in `rode-plan.ts`, which existed only for the row, is
  removed. The Rode Planner is the one place the recommended rode is shown.
- The forecast wind band stays hoisted in `App.tsx` so the planner keeps the operator's
  pick across closing and reopening.

## Consequences

- The `anchor.scope_method` setting now affects only the Rode Planner.
- An operator who wants the recommended rode while anchored opens the Rode Planner.
