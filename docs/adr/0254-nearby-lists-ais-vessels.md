# ADR 0254: The Nearby List Shows AIS Vessels In A Group Of Their Own

## Status

Accepted (2026-11-03).

## Context

The Nearby map draws a ship marker for every AIS contact when Show AIS traffic
is on, but the marker carries no name, and the list beside the map says nothing
about vessels. The operator wanted to read the contacts on the wall display
without opening the Nearby Vessels tile: who they are, how far, and whether
Helmcentral has met them before.

The wall tile is 272 px tall, which leaves about 225 px for a list that cannot
scroll, in a pane about 950 px wide. A taller tile, say 12 columns by 22 rows,
has far more room. Whatever the tile adds must fit the first without losing the
second.

## Decision

The list gains a **Vessels** group, drawn only in the map-and-list layout and
only when `showAis` is on. No new setting exists. The Nearby Vessels tile
supplies the wording, the alarm tiers and the ordering, and both tiles import
them from one module.

**Which vessels.** Those inside the smaller of the tile's range and the feed's
`max_range_m`. The backend's 5 km limit stays as it is. The filter applies to
the list only: every contact still gets its marker. A contact in a collision
alarm or warning is listed at any range the feed reports, so the group never
reads as empty while one is sounding. A GNSS alert still replaces the rows,
because no distance can be trusted then; the alarm banner and the red marker
still show the alarm.

**Order and size.** Alarmed vessels first, worst state first, then nearest.
Four rows, with "+N more" beside the group heading when more qualify. On the
wall tile a line of its own for it overflowed by 6 px.

**Row.** Name on the left, distance and bearing on the right, as a place row
has them. The feed gives a range but no bearing, so the tile works the bearing
out from own ship to the vessel's position and omits it when either position is
missing. A second line carries the Nearby Vessels tile's sighting text ("Seen 7x
before, last 2d ago"), the collision label, and "position 5m old" once the
report is older than 180 s. Stationary AIS reports can be about three minutes
apart, so anything younger is normal. At a count of zero the row says nothing
about sightings. The row is plain and non-interactive: the kiosk has no input.

**Layout.** Places keep ranks 1 to 5. Vessels have no rank. On a short list
pane the two groups sit side by side, vessels on the right. From 420 px of pane
height they stack. That figure is the worst case of the stacked content
(footer, five place rows, the highlighted row's three-line summary, the group
heading and four vessel rows) with a small margin, so the tile stacks only when
everything fits. The tile's grid row is fixed, so the list's content cannot
stretch the map beside it and re-frame the camera. The pane's height comes from a
ResizeObserver, as the map pane's size already does, rather than a container
query, because the test suite can drive it and can read the chosen layout from
the DOM.

**What vessels do not touch.** They are outside the highlight cycle, the arrow
and the camera framing. The ranked list, the cycle's reset key and the framing
key remain made of places alone.

**Map labels.** Ship markers show the vessel's name under them, in both
layouts, in the place label style. They join the declutter pass with a priority
below every place, ranked or not, so a place label always wins. Among vessels,
alarmed ones come first, worst state first, then the feed's order, so an alarmed
vessel yields only to places. Their ids carry a `vessel:` prefix so they cannot
collide with a place id in the suppressed set. The pass projects each contact's
real position. It runs when a key of vessel id and position rounded to about
110 m changes, since the vessel list changes every five seconds as `age_seconds`
ticks and nothing visible has moved. The grid is 110 m and not 11 m because a
contact under way crosses an 11 m boundary on nearly every 5 s tick, while 110 m
barely moves a label against the collision radius. The markers are memoised on flat values, as the place
markers are.

**States.** The group stays visible whenever it is drawn, and says one of these,
in this order:

1. "Loading…" until the first feed event. Never "No vessels".
2. "AIS unavailable" when the feed's `source` is not `signalk`. The store now
   keeps `source`, which it used to drop, and a selector gives the Nearby map the
   source and range without re-rendering on every age tick. It comes before the
   GNSS message because, when SignalK itself fails, the backend raises the GNSS
   alert as well and reports the source as `backend-fallback`; the GNSS message
   would blame the wrong fault.
3. "Vessels need a GNSS fix", with no rows. Under the -1,-1 position sentinel
   every contact's range is wrong.
4. "No vessels within X", X being the smaller range.

## Known limit

A silent AIS receiver with SignalK still running is indistinguishable from an
empty sea. The backend reports `signalk` as the source and an empty list, and the
feed's age is unknown with no contacts (ADR 0068), so the group reads "No
vessels within X". The help pages say so. Closing the gap needs the receiver's
own heartbeat, which this change does not add.

## Alternatives rejected

- **Vessels mixed into the places list by distance.** Every arrival would
  renumber the places and could push a place out of the five.
- **Vessels in the arrow cycle.** A vessel moves and ages, so the arrow would
  chase a moving target, and every arrival or departure would restart the cycle.
- **Vessels in the camera framing.** The map would jump each time a contact
  arrived or left.
- **Raising the 5 km limit.** It changes what counts as a sighting and breaks
  comparison with the history already recorded.
- **A "First sighting" label at zero.** Zero is unreliable: a vessel with no MMSI
  always reads zero, and a returning vessel reads one low until its visit is
  confirmed. No text is more honest than a wrong label.
- **Stacking only.** It does not fit the 272 px wall tile. The alternatives were
  fewer places or a taller tile.
- **A new setting for the group.** Show AIS traffic already says the operator
  wants vessels on this tile.
