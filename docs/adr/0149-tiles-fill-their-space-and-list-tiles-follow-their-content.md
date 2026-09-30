# ADR 0149: Fixed-Canvas Tiles Fill Their Space, and List Tiles Follow Their Content

## Status

Accepted

## Context

Two things about how tiles use the height the grid gives them were wrong.

**Fixed-canvas tiles ignored height.** Wind and the Engine Cluster draw a
fixed design canvas (corner cards cut along a circle around a ring) and scale
it with a transform. `useFitScale` measured width only and capped the scale at
1, so a tile dragged taller or wider than its design never grew past it, and
the spare room sat empty. Wind also carried a `maxWidth` on its wrapper that
pinned it to the design width on its own. Anchor Watch filled its tile because
it is a flex column with a flex-1 map; these two had no equivalent.

**List tiles kept the height of their longest day.** Nearby Vessels and Radar
Targets list contacts that come and go. The operator sizes the tile for a busy
anchorage, and in a quiet one the tile is mostly empty card. Their empty state
also said "No nearby targets" without saying how far the search reached.

## Decision

### 1. Fixed-canvas tiles are fitted to width and height

`fitScale` (lib/cluster-canvas.ts) is `min(availW / designW, availH / designH,
MAX_FIT_SCALE)`, with `MAX_FIT_SCALE` at 2. `FitCanvas` draws the canvas
absolutely positioned and centred inside a measured box, so the scaled content
never contributes to the size being measured and there is no
measure-scale-resize loop.

The box is `flex-1 min-h-0` inside the tile, which takes a new `fill` prop that
makes the tile's content a flex column. The box also carries `min-height =
designH * min(1, availW / designW)`, which depends on width alone. On the
desktop grid the tile has a definite height and the box fills it. Where the
tile has none, the box's height is that minimum and the result is the old
width-only fit. The stacked narrow layout gives each cell a minimum height
rather than a height, but the card stretches to it (a stretched grid item
counts as definite), so a tile whose minimum is taller than its content now
fills that minimum too. That is the same intent as on the desktop grid.

Wind's two canvases (phone and desktop design sizes, one hidden at each
breakpoint) are both `FitCanvas`es; the hidden one measures zero and draws
nothing. The hero row's 1.15 scale and the wall display's scale are
transforms above the tile, so the tile sees the layout size and is unaffected.

### 2. A list tile can report the height its content needs

A tile opts in with `<Tile shrinkToContent>`. It measures its content at
natural height inside a `flow-root` block (so the list's own margin is
counted) and reports `card height - content area height + block height` to a
scope the grid provides per tile. That figure does not depend on how tall the
card currently is, so shrinking the card cannot change it.

The grid keeps the reports in state and never saves them. The saved `h` is the
maximum. The rows a tile is drawn at are `min(saved h, max(2, rows for the
reported px))`, where two rows is the header plus one row, using the existing
row-to-pixel maths (and the page's row margin, so the wall display's 8px margin
is honoured). The pure functions are in lib/list-tile-height.ts.

A tile may shrink only when it is the bottom of its column:

- On the desktop grid, no other tile's x-range `[x, x+w)` overlaps it at all
  (partial overlap counts) with `y >= this.y + this.h`. Saved heights are used
  on both sides, so the answer never depends on the shrinking itself.
- On the narrow stacked layout, it is the last tile in stack order.
- The hero is neither shrunk nor counted as a blocker: it renders in its own
  row and its grid slot is only held empty.

In layout mode every tile is drawn at its saved height, so drag, resize and
keyboard moves can only commit the operator's own `h`; the shrunk height never
reaches `onLayoutSettle`.

Height changes ease over 150ms. react-grid-layout's stylesheet already
transitions height at 200ms; `dashboard-bento-grid.css` restates it at 150ms
(unlayered, since the rule it overrides is) and turns it off under
`prefers-reduced-motion`. Its own resizing and dragging rules still disable
the transition during a drag.

Opted in: Nearby Vessels and Radar Targets, whose length changes with the
traffic. Left out: CZone Switches and Tanks (a fixed set of configured
circuits or tanks, sized by the operator to show all of them; a control panel
should not move under a finger), gauge groups, lamp strips, Route and the rest
(fixed instruments), and the maps (they want space).

### 3. The search range is part of the nearby-vessels payload

`buildNearbyVesselsPayload` adds `max_range_m` (`nearbyMaxRangeMeters`). The
empty state reads "No vessels within 5 km" (kilometres, or metres under one;
nautical miles when the display is imperial, as the Nearby map tile formats
ranges). If the payload carries no range the line says "No vessels in range"
and names no figure: a number the client made up would go stale the day the
limit moved.

## Consequences

- Wind and the Engine Cluster scale up to twice design size. Their minimum
  height is unchanged: at that floor the canvas is at scale 1.
- A quiet anchorage leaves the bottom of a column empty under a short Nearby
  Vessels tile. The board does not rearrange to fill it.
- Entering layout mode makes shrunk tiles grow back to their saved height,
  animated.

## Alternatives rejected

- **Shrink any list tile.** With a tile below it, each AIS contact arriving or
  leaving would slide everything under it up and down under the operator's
  eye. The bottom-of-column rule confines the movement to space nothing else
  occupies.
- **Shrink only the card, keep the grid cell.** The gap remains, as blank grid
  rather than as blank card, and nothing is gained.
- **Persist the shrunk height.** The saved value is the operator's intent for a
  busy day; overwriting it would lose it the first quiet hour.
- **Raise the cap indefinitely.** The readings are sized for glanceability at
  a design width; beyond about twice, extra scale adds nothing and the corner
  cards outgrow the ring.
- **A default of 5 km in the client when the payload lacks a range.** It
  would be wrong the moment the backend limit changed, and silently so.
