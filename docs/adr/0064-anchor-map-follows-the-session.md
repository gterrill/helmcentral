# ADR 0064: The anchor map follows the anchoring session

## Status

Accepted.

Extends ADR 0059 (always-on anchor map, Drop/Raise, scope method). Uses the session
model established by ADR 0048 (session-bound placemarks).

## Context

The anchor map remembers where you left it. Every pan writes the centre to
`localStorage` under `anchor-watch-map-center`, and the next mount opens there. That
is the right behaviour inside an anchorage: you drag the view onto the boat anchored
up-current of you, or onto the shelf astern, and you want it still there when you
reopen the drawer or reload the tab.

It is the wrong behaviour across anchorages. Motor twenty miles, drop in a new bay,
and the chart-table browser opens on last night's water. The boat, its swing circle,
the alarm ring and every AIS target are off-screen, and nothing on the map says so. An
empty chart looks identical whether you are looking at open water or at a view the boat
left this morning. The phone that was closed at the time is the same. Each client held a
centre it had no way to date, so every device had to be re-centred by hand at every new
anchorage.

The stored centre was never wrong, it was just untagged. It recorded *where* the operator
had looked and not *when*, so nothing could tell a pan made ten minutes ago from one made
in a bay two headlands back.

## Decision

### 1. `set_at` is the session's identity, not a last-written stamp

`POST /api/anchor-watch` stamped `SetAt: time.Now()` on every write. Repositioning the
anchor marker on the map is a POST too, so a drag re-minted the stamp, and "when the anchor
was set" moved to the last time anybody nudged the marker.

The session now begins at the drop and holds until Raise. `setAnchorWatch` carries `SetAt`
forward from the active record and mints a fresh one only when there is none, which is
exactly when a genuine drop happens, because Raise `DELETE`s first. This is the rule ADR
0048 already applies to placemarks: a reposition corrects where you believe the hook lies,
it does not begin a new anchorage. `set_at` now means what its name says.

### 2. A stored centre belongs to the session it was made in

The record gains a third field, the session id it was stored under:

    { latitude, longitude, sessionId }

On mount and on every change of `set_at`, the map compares the session the view is
following against the session the server reports. They match, and the stored pan stands.
They differ, and the view belongs to an anchorage the boat has left, so the map centres on
the anchor and re-tags the record.

The comparison is between two ids rather than two positions. Distance would be a guess with
a threshold in it, and the wrong guess in both directions: a drag of the marker across the
bay is the same session and should not move anybody's view, while a second night in the same
cove is a new session even though the anchor went down within a boat length of yesterday's.

Zoom is untouched. It is a preference about how much water you want to see, and it does not
go stale when the boat moves.

### 3. A null session is not evidence of a new session

`anchorSetAt` is null in two states that look identical to the map and are not: no watch is
running, and the first `/api/anchor-watch` poll has not answered yet. Treating null as a
session change would re-centre on every reload, discarding the operator's pan in the
anchorage they are still sitting in.

So the mount resolves once: with no session id to judge by, the stored centre is trusted, and
the effect corrects it the moment a real id lands. A pan made during the current session
survives the reload; a pan made during the previous one is overridden a beat later, at the
first poll.

A record written with no session (browsing the map before the first drop, or one written by a
build that predates this change) carries `sessionId: null`, which matches no live session and
is therefore re-centred by the first session that arrives. That is the correct outcome in both
cases and needs no migration.

### 4. Every client, not just the one that dropped

The session id comes from the server, so each client reaches the same conclusion on its own
poll without any coordination between them. The phone in a pocket, the tablet on the bunk and
the browser at the nav station all swing to the new anchorage as they wake, the same way they
all see the same placemarks.

## Consequences

- Dropping in a new bay re-centres every device. An operator who had deliberately panned during
  the previous anchorage loses that pan, which is the point: it was a view of somewhere else.
- Repositioning the anchor no longer moves anybody's view, including the view of the operator
  doing the dragging, who is watching the marker under their own finger.
- `set_at` is now stable for the life of a watch. Nothing else read it before this change: it
  was persisted and echoed in every response and then ignored, which is how a marker drag
  quietly took over the stamp in the first place. Anything that reads it from here on gets the
  drop rather than the last marker nudge, which is what the field claims to be. The
  motoring/post-anchor trail split does not go through it: the server rolls its own trail
  buffer on the same POST.
- The re-centre control still clears the stored record entirely, which remains the way to say
  "forget my pan" without waiting for a new anchorage.
