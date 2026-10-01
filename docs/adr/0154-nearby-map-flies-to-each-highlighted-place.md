# ADR 0154: The Nearby Map Flies To Each Highlighted Place And Back

## Status

Accepted

## Context

The Nearby tile highlights one ranked place at a time, ringing its marker and
its list row, and moves on every few seconds. The map did not show where that
place was. At the overview zoom, which is fitted to hold the boat and every
ranked place, a highlighted marker is a small ring among others, and on the
wall display nobody is reading the list.

The camera is also deliberately centred on the boat. An earlier change on this
branch put it back there after a fit that sat the boat off to one side.

## Decision

Each time the highlight moves to a place, the map flies to that place at two
zoom levels closer than the overview (never past the map's maximum), holds, and
flies back to the boat-centred overview at 60% of the cycle period. The
overview is therefore on screen before the next place is highlighted.

- The highlight already showing at mount is skipped. The first fix still jumps
  straight to the overview.
- A single ranked place never advances the cycle, so it flies in once, flies
  back once and stays on the overview.
- While a place is held, the follow-the-boat easing is suspended. It resumes
  after the fly-back, which goes to where the boat is by then.
- With the position flagged as bad, the map holds still: no fly-in, no
  fly-back, the same rule as following the boat.
- With reduced motion requested, the camera jumps instead of flying.
- It applies to the non-interactive wall display as well, and to both the map
  and the map-and-list layouts. The list's own cycling is unchanged.

## Consequences

- The operator sees where each highlighted place is without reading the list.
- Between places the overview is up for the last 40% of the cycle. With a
  3 second cycle the fly-in takes half of it; the cycle length is the
  operator's to set.
- The camera now moves on a timer as well as on position fixes.

## Alternatives rejected

- **Pan to each place at the overview zoom.** The boat drifts off-centre for
  most of the cycle, and the place is still a small ring.
- **Tour from place to place without returning.** The boat is off screen for
  most of the cycle, which is the wrong thing to hide on a boat.
