# Anchor watch

Anchor watch catches a dragging anchor before it grounds the boat or fouls a
neighbour, including at 0300 with nobody on deck: trail sampling and drag
detection both run on the server, for your own vessel and for nearby AIS
targets, and keep running with every browser on the boat closed.

A drag is an ordinary alarm: it is logged, can be acknowledged, and uses the
transports you have configured. Acknowledgement happens on the server, so
silencing an alarm on your phone also silences it in a browser left open in
the saloon.

A lost GNSS fix never raises a drag. Position freezes at its last trusted value
during an outage rather than wandering, and the watchdog for a lost instrument
connection reports the outage separately, as its own alarm. A drag alert and a
lost GPS fix require different responses.

## AIS targets on the map

Nearby AIS targets draw on the anchor-watch map as amber circles carrying the
vessel's name, range and bearing. When a collision alarm is in force for one
of them, its marker turns red, gaining a ring once the alarm reaches alarm
severity, and it stays red for as long as that alarm is active, including
after you have acknowledged it. Radar contacts draw as triangles on the same
map and turn red on their own closing figures, independently of AIS, so a
boat your radar and your AIS both see can show two red markers at once with
two different numbers.

## Drop, Raise, and the instrument network

**Drop** saves the watch and publishes its anchor coordinates onto the boat's
instrument network. These are the stored coordinates, including the bow
offset when applied. Repositioning the anchor's saved position republishes
the corrected coordinates; changing the alarm radius or rode settings does
not. There is no control for repositioning yet — see
[Set an anchor watch](../how-to/set-an-anchor-watch.md).

**Raise** publishes an explicit "no anchor" state, then removes the local
watch, trail and session pins. This clears the boat's anchored navigation
state, so an autopilot or another instrument that only behaves differently at
anchor sees the vessel as under way again. Both operations confirm the change
before reporting success and use the saved instrument-network credentials,
independently of alarm transport settings.

If synchronization fails, an error is shown. A failed Drop/reposition leaves
the saved local watch active for safety; repeat Drop/reposition to synchronize.
A failed Raise publication retains the local watch; retry Raise. If the
instrument network accepted Raise but removing the local watch failed, the
error says so and Raise can be retried. Restarting Helmcentral does not
republish or clear anchor state.

If the saved watch itself can't be read back after a restart or an update —
the file behind it is damaged — the tile and this page say so plainly rather
than guessing at a position or showing a watch that isn't really there. Every
other alarm on the boat keeps running. Drop the anchor again to start a fresh
watch in its place.

## Automatic raise

The server raises the watch for you when you motor away. It runs whether or
not a screen is open. The switch is **Auto-raise anchor watch when under
way** under Settings → Anchor Watch. It is on by default, and one setting
covers every screen.

The watch comes up when all of these have held for 15 seconds straight:

- At least one main engine shows rpm above zero, from a reading less than
	30 seconds old.
- The GNSS fix is usable and puts the boat more than the watch radius plus
	4.572 metres (15 feet) from the anchor.
- Speed over ground is at least 3 knots.

If any of them drops out, the 15 seconds start again. A missing reading
counts as not met, so no rpm, no fix or no speed means the watch stays up.

The speed test lets you back down on the hook without losing the watch. You
will be outside the circle with an engine running, but the boat stops once
the rode comes tight. When you leave, you pass 3 knots within seconds. If
you idle out of the anchorage slower than that, raise the watch by hand.

Every screen shows a toast when it happens: *"Anchor watch raised
automatically: engines running, under way outside the zone."* The server log
records the rpm, distance, radius and speed it acted on.

If the raise fails, for example because the instrument network doesn't
confirm it, the server logs it and won't try again until the conditions
break and re-form. The watch is still up and the boat is outside the circle,
so the drag alarm sounds. Raise by hand.

## Low-water clearance warning

Anchor Watch checks the depth under the keel against the next low tide, so a
spot that looks comfortable right now does not turn into a grounding six
hours later with nobody watching the sounder.

It takes the live depth reading at the boat's current position, projects it
forward to the next low tide, and compares what is left under the keel
against your vessel's draft and the margin you set in Settings → Anchor
Watch (**Clearance at Low Water**, 0.5 m by default). When the water left at
that point would be less than your margin, the tile and the full-page anchor
watch view show a shallow-water warning naming the shortfall and the time of
the next low. When there is enough water, the same line quietly shows the
clearance you can expect instead.

The warning needs a live depth reading, a draft reported by your boat's
instrument network, and a current tide reading. If any of those is missing
it says so rather than guessing at a number, and it reads correctly through
a spring low that drops below chart datum, not just down to zero. Depth
readings used here run slightly shallow of the true figure, on the safe
side, so this warning tends to sound before the boat actually finds bottom,
not after.

If the tide feed goes quiet, the warning does not keep computing against
whatever it last heard. Once a tide reading is more than half an hour old,
or the low it was pointing at has already come and gone, the tile and the
full-page view show **Tide forecast out of date** instead of a number, until
the next tide update lands.

This is a running readout, not an alarm: it does not sound and needs no
silencing, and it updates continuously as the boat swings and the tide
moves. Because it reads the boat's position right now, it can flip between
clear and too-shallow as you swing on the rode without the tide or the
anchor itself changing at all, and it only looks as far ahead as the next
low tide, not a lower one that might follow later in the day.

## The rode planner

A sidebar on the anchor watch page for pay-out and swing-radius planning,
against tide-corrected depth and gust-seeded wind. It can be used before
lowering the anchor.

Its Depth field shows the figure the plan is computed against: depth at this
spot at the next high tide, plus your bow roller height above the water.
That is the depth the chain has to reach on a rising tide, not just the
depth under the keel right now. Type over it to plan against a different
figure; the field will not accept a number that leaves no water at all once
the tide and bow height are backed out, and says so rather than saving it.

Its map takes pins: click open water to mark a bombie, a mooring block or the
nearest bit of shoreline, and watch the range close as you swing. Pins are
shared across every device watching the anchorage, so marking a hazard from the
bow shows up on the tablet at the helm. They expire with the anchoring rather
than accumulating across seasons.

The map keeps the view you panned to for as long as you stay in the anchorage,
including across updates. Anchoring somewhere else recentres every device on
the new anchor.

See [Set an anchor watch](../how-to/set-an-anchor-watch.md) for dropping,
setting the alarm radius and raising the watch.
