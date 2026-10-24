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
   the passage store holds `from_ms` and `to_ms` (Unix milliseconds), `corner`,
   `zoom_offset` and the update time, with `(from_ms, to_ms)` the key. The key is
   the replay's range: the first passage's start to the last passage's end. A
   passage and a cruise that begins with it are different ranges, so each keeps
   its own setting. Like shots (ADR 0206) the row carries no passage id, so
   renaming, hiding or re-noting a passage leaves it alone. Trimming either end,
   splitting or merging changes the range, and the defaults then show until it is
   set again; this was accepted over a passage id column that merge and split
   would each need to reconcile. The replay and the export derive the range from
   the same function over the same passages.
3. **Validation is on the server and strict.** `GET /api/passages/inset-view?from=&to=`
   (read tier) returns the saved view or the defaults. `PUT
   /api/passages/inset-view` (write tier) takes `from_ms`, `to_ms`, `corner`
   and `zoom_offset`, all required. An unknown corner, an offset outside -3..3 or
   a non-integer, an unknown field, a range that runs backwards, or an end that
   falls inside no passage is a 400 and nothing is stored; nothing is coerced. A
   database failure is a 500, not a 400.
4. **One automatic zoom, clamped at 0 to 12.** The live overview, the blog embed
   and the still images all start from the export's automatic zoom (the track's
   box about 25 times over, between zoom 2.5 and 5), add the step and clamp the
   sum to map zoom 0..12 through one function, so a step means the same framing
   everywhere. The live overview used to frame closer on its own formula; it now
   matches the export. The export decides whether the track box is too small to
   see and needs a dot from the final zoom, not the automatic one.
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
     button, and moves the notice line below itself. Both bottom corners lift to
     just above the readout while it shows, by its measured height (a night
     caption makes it taller), and use the stylesheet's own offset when it is
     hidden. Bottom right lifts too, though the readout does not always reach it,
     because the readout can span the width and a fixed rule is simpler than a
     width test.
   - Still images: always inside the map area, 24 px in from its edges, so the
     bottom corners are above the title, figures and credit band.
7. **The controls live on the replay bar.** A popover beside the Overview map
   button, shown while the overview is on, has the four corners and Wider and
   Closer buttons with the current step between them. A change shows at once and
   is saved, one write at a time so the last click is what the server keeps; a
   refused save restores what was stored and says why. While the saved view is
   being read the button is disabled; if the read fails the overview stays off,
   a message says why, and a Retry button replaces the controls. Nothing shows
   the defaults in its place. The overview
   on/off choice stays a browser preference (`passages.insetMap`).

## Consequences

- The live overview map is moved when the step changes, since a map's first
  view is applied only when it is created.
- An embed built before this change has no corner in its data. Embeds are
  generated, not stored, so there is nothing to migrate.
- A setting is attached to a range. Trimming either end of a passage, or
  replaying a different set of passages, shows the defaults until it is set
  again.
