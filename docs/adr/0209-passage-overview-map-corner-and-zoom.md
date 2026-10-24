# ADR 0209: The Passage Overview Map Has a Corner and a Zoom Step

## Status

Accepted (2026-10-24)

## Context

The overview map on a passage replay (the small map of the surrounding coast)
was fixed in the top right corner at a zoom the code chose from the track's
size. That zoom suits most passages and fits none exactly: a short hop shows too
little coast, a long passage too much, and the corner covers the 3D and imagery
buttons' neighbourhood or the part of the track an operator wants to film. The
blog embed and the still images drew the same fixed overview, so a change made
for the replay had to reach the export too.

## Decision

1. **Two values, saved per passage.** A corner (top left, top right, bottom
   left, bottom right) and a zoom step, an integer from -3 to +3 added to the
   automatic zoom (one map zoom level per step, 0 meaning today's zoom). The
   step is relative, not an absolute zoom, so the choice still means "a bit
   wider" when the passage or the automatic fit changes. Unset is top right and
   0, which is what every passage looked like before.
2. **Stored like camera shots, keyed by time.** A row of `replay_inset_views` in
   the passage store holds `at_ms` (Unix milliseconds), `corner`, `zoom_offset`
   and the update time. The key is the start of the replay's first passage. A
   passage, a cruise and a selection therefore each have their own entry, and
   a cruise replay does not borrow the first leg's. Like shots (ADR 0206) the row
   carries no passage id, so renaming, hiding or re-noting a passage leaves it
   alone. Trimming or splitting changes the start instant, and the setting is
   then not found and the defaults show; this was accepted over adding a passage
   id column that merge and split would each need to reconcile.
3. **Validation is on the server and strict.** `GET /api/passages/inset-view?at=`
   (read tier) returns the saved view or the defaults. `PUT
   /api/passages/inset-view` (write tier) takes `at_ms`, `corner` and
   `zoom_offset`, all required. An unknown corner, an offset outside -3..3 or a
   non-integer, an unknown field, or an `at_ms` that falls inside no passage is a
   400 and nothing is stored; nothing is coerced.
4. **The zoom is clamped at 0 to 12.** The live map and the export both add the
   step to their own automatic zoom (the live map's differs from the export's)
   and clamp the sum to map zoom 0..12, through one function. The export decides
   whether the track box is too small to see and needs a dot from the final zoom,
   not the automatic one.
5. **The export follows.** The export input carries the corner and step, the
   prepared data carries the final zoom and the corner, and both are part of the
   cache key, so a change is never served from the previous card. The player
   places the overview by a corner class; the still image places it by the layout
   rectangle. An export waits for the saved view to load and stops with the
   server's words if it cannot be read, as it does for shots.
6. **Each corner keeps clear of what lives there.**
   - Live map: the top right overview stacks under the 3D and imagery buttons;
     the top left one stacks under the terrain notice; the bottom ones sit
     32 px above the bottom edge, clear of the map credit. The overview has its
     own slot in the map rather than being passed into the button stack.
   - Blog embed: top left sits below the zoom buttons and right of the play
     button, and moves the notice line below itself; the bottom corners sit above
     the readout while it shows (and above the credit when it does not).
   - Still images: always inside the map area, 24 px in from its edges, so the
     bottom corners are above the title, figures and credit band.
7. **The controls live on the replay bar.** A popover beside the Overview map
   button, shown while the overview is on, has the four corners and Wider and
   Closer buttons with the current step between them. A change shows at once and
   is saved; a refused save restores what was stored and says why. The overview
   on/off choice stays a browser preference (`passages.insetMap`).

## Consequences

- The live overview map is redrawn when the step changes, since a map's first
  view is applied only when it is created.
- An embed built before this change has no corner in its data. Embeds are
  generated, not stored, so there is nothing to migrate.
- A setting is attached to the start of a passage. Moving the start (a trim)
  returns the passage to the defaults until it is set again.
