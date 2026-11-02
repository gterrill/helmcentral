# ADR 0249: The Nearby Map Stays Still And Points At The Highlighted Place

## Status

Accepted (2026-11-02). Supersedes [ADR 0154](0154-nearby-map-flies-to-each-highlighted-place.md).

## Context

ADR 0154 had the camera tour the ranked places: a dive to the first one, a
high arc to each next one, a pull-out after the last, and an ease back to the
boat every two seconds whenever a position fix arrived. The operator watched
this on the wall display, whose processor is weak (an ODROID running WPE
WebKit), and found the flights rendered too slowly to be worth having.

Two things make a flight expensive on that display:

- Every frame of a flight redraws the whole map, and the map draws raster
  chart tiles under a stack of DOM markers.
- A flight to street zoom loads a new set of tiles at the end of each leg. The
  place zoom was 17, two levels tighter than anywhere else in the app.

The follow ease was motion as well. It ran at most every two seconds for as
long as the boat moved, so the map was seldom still.

The performance gain was not measured on the device. The reasoning above comes
from the operator's observation and from how the map draws, not from a profile
of the old or the new behaviour.

## Decision

The map never animates. The camera only ever calls `jumpTo`, never `easeTo` or
`flyTo`, and the tour is gone on every host, not only the kiosk, so there is a
single camera path.

**Camera.**

- The first fix jumps to the overview: the boat at the centre, zoomed to fit
  the ranked list. The configured range no longer limits the zoom; it only sets
  the search radius. The fit's own maximum zoom (16) still stops a lone close
  place from zooming in without limit.
- After that the boat marker moves and the map stays still. The camera
  re-frames with one instant jump only when the boat leaves the padded frame of
  the camera it is on (44 px of clearance, capped at a quarter of the pane so a
  short pane keeps a box at least half its size and GPS jitter cannot jump it),
  or when the set of ranked places,
  the measured size of the tile or the configured range differs from the last
  framing. The set is compared as sorted ids, so two places swapping rank, which
  changes nothing on screen, does not jump. The same key resets the Summary
  cycle, so a swap does not send the arrow back to the first place. Each jump
  sets bearing and pitch to zero, since touch rotate is still enabled and the
  frame check assumes north-up.
- A plain position fix that keeps the boat inside the frame makes no camera
  call at all.
- While the position is flagged bad the camera does not jump. The framing is
  recorded only when a jump really ran, so a re-frame that fell due during the
  alert happens when it clears.

**Arrow.**

- In the map-and-list layout, a curved arrow runs from the boat to the
  highlighted place. A quadratic curve is trimmed to start clear of the boat
  marker and stop clear of the place's ring, bows by a fifth of its length (at
  most 60 px), and ends in an arrowhead. The bow takes a fixed side of the
  direction of travel, and the other side only when the first would put the
  control point off the pane. A rule that chose the side nearer the pane centre
  flipped with GPS jitter, because after a jump the boat sits at the centre. A
  place across the antimeridian is projected at the copy of its longitude
  nearest the boat.
- Under 64 px there is no room: the clearances at the two ends take 46 px and
  the remaining stub would be hardly longer than the 10 px arrowhead. No arrow is drawn and the ring
  alone marks the place.
- It is an SVG overlay above the map, not a map layer, so a change of place
  never makes the map redraw. It is a small component inside the map that
  subscribes to the map's own move event, so a pan redraws the arrow and leaves
  the tile above it alone. The POI markers are memoised on flat values, since each
  poll builds new feature objects, so a position fix renders none of them and a highlight change renders the two whose ring moves.
  The wall display cannot pan, so this matters on interactive dashboards only.
  Coordinates are whole pixels, so a small movement of the boat produces the
  same drawing and React leaves the DOM alone.
- A marker's name label is out of flow, so the projected point is the centre of
  the icon whether or not the label is showing. The arrow's clearances depend
  on that.
- It does not animate. It switches when the highlight moves.
- It is hidden in the map-only layout (nothing there says what it points at),
  under a GNSS alert (the boat's position may not be real), with no position,
  no highlight, or no WebGL2.

**Label.** In the map-and-list layout the highlighted place always wins label
decluttering, and the decluttering runs again when the highlight moves, so a
neighbour cannot hide the name of the place the arrow points at. The map-only
layout has no arrow or list to explain labels swapping, so it keeps the rank
order.

**Stacking.** The highlighted marker is raised above the others, so a neighbour
cannot cover the place the arrow points at. The arrow redraws in a layout
effect, so a frame never shows the new highlight with the old arrow.

**Description.** It stays in the highlighted list row, as before.

## Fit, not range

An earlier version of this decision kept the range as a floor on the zoom. On
the boat's own data at a 5 nm range, two of the five places were too close to
the boat for an arrow, and the overview was crowded. Fitting the map to the five
nearest places brings it about 1.75 times closer, by estimate (not measured).
The operator chose this. The trade-off is that the range no longer says how much
of the map shows: a wide range with five close places gives a close-up, and
places further out than those five can fall off the map.

## Consequences

- The map is still except for the markers, the trail and the arrow. Nothing
  loads new tiles mid-cycle unless the boat nears the edge or the list changes.
- The operator can see which place the list is on by following the arrow,
  without reading the list, which was what the tour was for.
- Reduced-motion settings no longer matter to this tile, because it has no
  motion.
- The street-level close-up is lost. A place shows at the overview zoom only.
  If it is missed, an inset showing the place close up could come back, drawn
  once per highlight instead of flown to.

## Alternatives rejected

- **Keep the tour and skip it on the kiosk.** Two camera paths to test and
  explain, for a tile whose main home is the wall display anyway.
- **A tooltip with the description at the place.** The description is already
  in the highlighted row, and a tooltip on a display nobody touches would be
  one more thing to lay out and clear.
- **Animate the arrow drawing itself.** More motion on the display that cannot
  keep up with motion.
- **Draw the arrow as a map layer.** Moving the highlight would update a
  source and redraw the map, which is the cost this change removes.
