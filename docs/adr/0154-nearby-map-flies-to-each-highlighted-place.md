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

The camera tours the ranked places within the Summary cycle T (3 seconds at
least). Each time the highlight moves to a place it makes one flight, and only
the last place is followed by a return to the overview.

1. **Dive.** The first place after the overview is reached by an eased flight
   (ease-in-out cubic, MapLibre's default arc, which zooms out, travels and
   zooms in) to a fixed street-level zoom of 17, never less than two levels in
   from the overview. This goes past the overview's own zoom cap of 16 on
   purpose: zoom 15 was tried first and left too much around a small place to
   see what it was. The flight ends tilted to 30 degrees, bearing unchanged.
2. **Hop.** Every later place is reached straight from the previous one, in a
   single high arc. The flight's zenith is set to the overview zoom, so
   mid-flight the camera rises to the altitude that shows the boat and the
   whole area, then drops onto the next place at the place zoom, tilted and
   eased. The hop is longer than the dive.
3. **Hold** on the place for the rest of the cycle.
4. **Pull out.** After the last ranked place only, an eased flight back to the
   boat-centred overview, level again, to where the boat is by then, timed to
   finish 1.5 seconds before the cycle wraps.
5. **Pause** on the overview, then the first place is a dive again.

The moves target 2.8 seconds for the dive, 3.5 seconds for a hop, 2.5 seconds
for the pull-out and 1.5 seconds for the pause. On the last place the hold is
what remains between arriving and the pull-out; on any other place it is the
rest of the cycle after the hop (6.5 seconds at the default 10 second cycle).
When the busiest cycle (a hop that is also the last place: hop, pull-out,
pause) would take more than 85% of T, every move shrinks by the same factor, so
a 3 second cycle still has a hold. A longer cycle only lengthens the holds. The
Nearby tile is only used on wall displays, where nobody is operating the map,
so deliberate motion is wanted. The operator asked for continuous travel
between places rather than a return to the overview after each one, and the
arc's zenith keeps the boat and the whole area in view mid-flight, so the
pause on the overview happens once per tour and the boat is centred and
visible every time round.

- The highlight already showing at mount is skipped. The first fix still jumps
  straight to the overview.
- A single ranked place never advances the cycle, so it dives in once, pulls
  out once and stays on the overview.
- From the first dive until the pull-out after the last place has finished,
  the follow-the-boat easing is suspended, since an ease mid-flight would
  cancel a flight and leave the tilt behind. It resumes afterwards, or as soon
  as the highlight clears, when it also levels the camera.
- If the ranked list changes mid-tour and the highlight lands on a place that
  is not the last, the camera simply hops to it with the same arc.
- With the position flagged as bad, the map holds still: no dive, no pull-out,
  the same rule as following the boat. If the flag rises while a place is
  held, the pull-out is skipped and the camera is levelled in place so it is
  not left tilted.
- With reduced motion requested, the camera jumps at the same points and never
  tilts.
- It applies to the non-interactive wall display as well, and to the
  map-and-list layout only. The list's own cycling is unchanged.

## Consequences

- The operator sees where each highlighted place is without reading the list.
- The overview is up for the pause once per tour, after the last place. The
  cycle length is the operator's to set; 15 to 20 seconds suits a place worth
  reading about.
- The camera now moves on a timer as well as on position fixes.

## Alternatives rejected

- **Pan to each place at the overview zoom.** The boat drifts off-centre for
  most of the cycle, and the place is still a small ring.
- **Tour from place to place without returning.** The boat is off screen for
  most of the cycle, which is the wrong thing to hide on a boat.
