# ADR 0140: Collision alarms mark the Nearby Vessels tile

## Status

Accepted

Extends [ADR 0088](0088-collision-alarms-colour-the-ais-marker.md) (collision
alarms colour the AIS marker).

## Context

ADR 0088 gave the anchor-watch map's AIS markers a red mark and a ring once a
vessel's `notifications.navigation.closestApproach` alarm goes live. That
covers the chart, but the Nearby Vessels tile is a separate list, sorted
nearest first, and it read straight off the AIS feed with no awareness of the
alarm state at all. A vessel in `emergency` looked exactly like the harbour
ferry three rows down. An operator who had the tile open and not the map, or
who was scanning the tile for range and closing speed rather than looking at
the chart, had no reason to notice anything was wrong until the alarm card or
the audible alert caught up.

The tile is also list-shaped rather than marker-shaped, sorted by range. A
vessel that just started closing dangerously might sit at the bottom of a
list of a dozen contacts, below everything the operator would check first.

## Decision

### 1. Read the same alarm map the anchor-watch map reads

`App.tsx` already builds `aisCollisionAlarms` once, via
`collisionAlarmStatesByVessel(alarms)`, for the map's markers. The tile takes
the same `Map<string, AlarmState>` as a new optional prop, keyed the same way
(the SignalK vessel id, matched by plain equality against `NearbyVessel.id`,
exactly as ADR 0088 §2 established). There is no second parse of the alarm
list and no new subscription: one gated truth still feeds both displays, so
the map and the tile cannot disagree about which vessel is in alarm.

### 2. A two-tier collapse the map doesn't get to make

ADR 0088 §3 paints one red for every live state from `alert` through
`emergency`, because a map marker has exactly one colour to spend and shape
already says "this is AIS." A list row is not so constrained: it has room for
a text label next to the colour, so it keeps a distinction the map
deliberately gives up. `alert`/`warn` reads as a still-forming risk, amber,
labelled "Collision warning." `alarm`/`emergency` reads as an imminent
risk, red, labelled "Collision alarm." The alarm/warn cutoff matches the point at which
the map itself adds a ring, so an operator moving between the two displays
finds the same severity boundary in both places.

### 3. Alarmed vessels sort to the top, worst first

The tile's list is a triage tool: the reason to open it is to see what's
nearby and how fast it's closing, and an alarming vessel is the one piece of
information in that list that outranks range. Sorting rebuilds the array
rather than mutating the `vessels` prop in place, since that prop is the same
memoised array the rest of the dashboard reads. Vessels tied on tier, and
every vessel with no live alarm, keep the order the feed already gave them
(nearest first, computed server-side), broken by original index rather than
relying on `Array.prototype.sort` being stable.

### 4. Colour is not the only signal

Each alarmed row gets `role="group"` and an `aria-label` naming the vessel
and the severity ("ALARM BOAT, collision alarm"), plus the same visible
uppercase label a sighted operator reads. A screen reader, or an operator
relying on a low-contrast display in direct sun, gets the same fact the red
border is carrying.

## Rejected

**One red for every live state, matching the map exactly.** Considered and
rejected for the reason in §2: the map's single-colour constraint doesn't
apply to a list row, and throwing away the alert/warn-vs-alarm/emergency
split would discard information the row has room to show for free.

**A dedicated hook or a second read of the alarms feed for the tile.** The
map's `aisCollisionAlarms` is already memoised in `App.tsx` off the same
`alarms` array `useAlarms()` returns. A second computation would mean two
places parsing `collisionAlarmStatesByVessel`'s output, or worse, two
slightly different rules for what counts as "in alarm," and a second `Map`
identity that would fight the tile's own memoisation on every render whether
or not anything about the alarms actually changed. Passing the same `Map`
down costs nothing and keeps the invariant that the map and the tile read
one list.

**Sorting purely by severity, dropping the nearest-first tiebreak.** Within a
tier, and for every vessel with no alarm, the existing nearest-first order is
useful information the operator already relies on. Losing it to an
arbitrary or re-shuffling order every render would make the unalarmed part
of the list harder to scan for a gain that only matters to the alarmed rows.

## Consequences

- The Nearby Vessels tile and the anchor-watch map read from the same alarm
  map and can no longer show one as calm while the other is alerting.
- A vessel's row position in the tile is no longer fixed by range alone: it
  moves to the top the moment it enters `alert` and moves further up if it
  reaches `alarm` or `emergency`. That is the point of the change, not a
  stability bug, but it means the tile's row order is not purely a function
  of distance the way it used to be.
- `frontend/src/test/nearby-vessels-tile.test.tsx` pins the label text, the
  row and range colours, the sort order (including emergency above alarm
  above an unalarmed nearer vessel), and that passing no prop or an empty
  map renders exactly as the tile did before this change.
- No backend change. `NearbyVessel` and the nearby-vessels feed are
  untouched; this is wiring an existing alarm map into a second consumer.

## Related

- [ADR 0088](0088-collision-alarms-colour-the-ais-marker.md): the map marker
  behaviour this tile now matches, and the source of the vessel-id matching
  rule and the alert-through-emergency live-state definition.
