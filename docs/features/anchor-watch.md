# Anchor watch

The Anchor Watch monitors your position continuously to alert you the moment
your boat, or a nearby vessel, starts dragging, even when everyone is asleep
and every screen on board is switched off.

- **Always-on server monitoring:** Trail tracking and drag detection run
  directly on the server, not in your browser. They watch your own boat
  alongside nearby AIS targets around the clock, so you don't need to keep a
  device awake or a browser tab open.
- **Unified alarms and silencing:** A drag event triggers like any standard
  system alarm: it logs the event and alerts you across all your configured
  channels. Silencing the alert on your phone immediately syncs back to the
  server, quieting open screens anywhere else on the boat (like a tablet
  mounted in the saloon).
- **Failsafe GPS handling:** A lost satellite fix will never trigger a false
  drag alarm. If your GNSS signal drops, your position simply freezes at the
  last one the GPS reported reliably, rather than drifting across the screen.
- **Dedicated instrument watchdog:** Because a drifting anchor and a failed GPS
  antenna require completely different responses, a lost instrument connection
  triggers its own distinct alert instead of a drag warning.

## The map panel: depth, tide and swing

The full-page anchor watch view is the map, with nothing stacked above or below
it. The panel in the top-left corner of the map shows the readings. At the top
is the live depth under the keel, because on the hook that matters more than
how far off the anchor you've swung.

Under the depth, you'll see the tide and which way it's moving. Below that is
one line with the next high or low, its height and time, and the depth to
expect once it turns. The tide station the figures come from is named beside
the **Tide** label.

The low-water clearance warning sits in the same panel, so everything about the
water you're anchored in reads from one place. If the depth hasn't updated
recently, the reading is marked stale, so it never looks current when it isn't.

Below those, the panel shows the current set and drift. When an anchor is down,
it also shows the distance from the anchor, the bearing to it and the alarm
radius. That's where you check how far you've swung and how much room is left
before the alarm sounds.

On a phone the panel is kept short. The quiet low-water clearance line, the one
that says you have room, is left out. A shallow-water warning always shows. The
chart is centred in the part of the screen the panel leaves clear, so the boat
and anchor aren't hidden under it.

If there's no GPS fix, or you're on a display that can't draw the map (an older
tablet, say), there is no panel. The page shows the same figures in a card
above the empty map area instead.

## The swing trail

The yellow line on the anchor map is where the boat has been since the anchor
went down. A night's swing shows as a full pattern, rather than a scribble
under the boat arrow.

- **Saving points:** A point is saved whenever the boat has moved a metre, or a
  minute has passed.
- **Keeping the trail:** The server keeps it, so closing the browser, reloading
  or restarting Helmcentral doesn't lose it.
- **Moving the anchor:** Moving the anchor position starts the trail again from
  that point, because the trail is measured against where the anchor is.
  Changing only the radius keeps it.
- **Raising the anchor:** Raising the anchor ends the trail.
- **How much is kept:** The newest 10,000 points are kept and drawn, which
  covers days at anchor.

## AIS targets on the map

Nearby AIS targets draw on the anchor-watch map as amber circles carrying the
vessel's name, range and bearing.

- **Collision alarms:** When a collision alarm is in force for one of them, its
  marker turns red. It gains a ring once the alarm reaches alarm severity. It
  stays red for as long as that alarm is active, including after you have
  acknowledged it.
- **Radar contacts:** Radar contacts draw as triangles on the same map. They
  turn red on their own closing figures, independently of AIS. So a boat that
  your radar and your AIS both see can show two red markers at once, with two
  different numbers.

## Adjust: moving the anchor or changing the radius

A dropped anchor rarely needs moving far. Wind shifts, a bad set or a neighbour
swinging closer than expected sometimes call for it, though.

**Adjust** opens a focused view of the chart. The anchor sits fixed at the
centre of the screen. When you pan the chart underneath it, the anchor moves to
wherever the centre lands. A faded copy of the original position and circle
stays on screen for reference.

Pinch or scroll to zoom in and check exactly where you're about to drop the
anchor, the same as on any other chart. Zooming never changes the alarm radius.
A small − / + control sits under the Adjust icon for that, with the radius
shown plainly between the two buttons.

Nothing changes on the boat's instrument network until you confirm the change.
You can back out at any point, and the watch stays exactly as it was. If the
new position or radius would leave the boat outside the alarm circle right now,
Adjust says so and asks you to confirm before saving. After every save there's
a short undo window, in case you change your mind again straight away.

See [Set an anchor watch](../how-to/set-an-anchor-watch.md) for the steps.

## Drop, Raise, and the instrument network

**Drop** and **Raise** float over the bottom of the map. When no watch is set,
Drop sits in the centre. While a watch is set, a compact Raise sits at the
bottom left. During Adjust, Raise stays available. On a narrow screen it sits
just above Cancel and Save. Raising closes Adjust. Without a map, they sit in a
row below the page's map area.

**Drop** saves the watch and publishes its anchor coordinates onto the boat's
instrument network. These are the stored coordinates, including the bow offset
when it's applied. Moving the anchor's saved position in Adjust republishes the
corrected coordinates. Changing the alarm radius or rode settings doesn't. See
[Set an anchor watch](../how-to/set-an-anchor-watch.md) for how to open Adjust
and move the anchor or change the radius.

**Raise** publishes an explicit "no anchor" state, then removes the local
watch, trail and session pins. This clears the boat's anchored navigation
state. So an autopilot, or another instrument that only behaves differently at
anchor, sees the vessel as under way again. Both operations confirm the change
before reporting success. They use the saved instrument-network credentials,
independently of alarm transport settings.

If syncing fails, an error is shown. Here is what to do in each case:

- **Failed Drop or reposition:** The saved watch stays active for safety, so
  repeat it to sync again.
- **Failed Raise:** The saved watch is kept, so retry Raise.
- **Raise accepted but the watch not removed:** If the instrument network
  accepted Raise but removing the saved watch failed, the error says so, and you
  can retry Raise.

Restarting Helmcentral doesn't republish or clear anchor state.

Sometimes the saved watch can't be read back after a restart or an update,
because its stored record is damaged. When that happens, the tile and this page
say so plainly. They don't guess at a position, and they don't show a watch
that isn't really there. Every other alarm on the boat keeps running. Drop the
anchor again to start a fresh watch in its place.

## Automatic raise

The server raises the watch for you when you motor away. It runs whether or not
a screen is open. The switch is **Auto-raise anchor watch when under way**,
under Settings → Anchor Watch. It's on by default, and one setting covers every
screen.

The watch comes up when all of these have held for 15 seconds straight:

- **Engine running:** At least one of the engine items assigned to a propulsion
  source in Settings → SignalK shows rpm above zero, from a reading less than
  30 seconds old. If no engine is ticked, or none is reporting, it never
  raises.
- **Outside the circle:** The GNSS fix is usable and puts the boat more than the
  watch radius plus 4.572 metres (15 feet) from the anchor.
- **Under way:** Speed over ground is at least 3 knots.

If any of them drops out, the 15 seconds start again. A missing reading counts
as not met. So if there's no rpm, no fix or no speed, the watch stays up.

The speed test lets you back down on the hook without losing the watch. You'll
be outside the circle with an engine running, but the boat stops once the rode
comes tight. When you leave, you pass 3 knots within seconds. If you idle out
of the anchorage slower than that, raise the watch by hand.

Every screen shows a toast when it happens: *"Anchor watch raised
automatically: engines running, under way outside the zone."* The server log
records the rpm, distance, radius and speed it acted on.

If the raise fails, for example because the instrument network doesn't confirm
it, the server logs it. It won't try again until the conditions break and
re-form. The watch is still up and the boat is outside the circle, so the drag
alarm sounds. Raise by hand.

## Low-water clearance warning

Anchor Watch checks the depth under the keel against the next low tide. A spot
that looks comfortable right now can turn into a grounding six hours later,
when nobody is watching the sounder. This warning tells you about it now.

It takes the live depth reading at the boat's current position and projects it
forward to the next low tide. Then it compares what is left under the keel
against your vessel's draft and the margin you set in Settings → Anchor Watch
(**Clearance at Low Water**, 0.5 m by default).

- **Too shallow:** When the water left at that point would be less than your
  margin, the full-page Anchor Watch view shows a shallow-water warning in the
  map panel. It names the shortfall and the time of the next low.
- **Enough water:** When there is enough water, the same line quietly shows the
  clearance you can expect instead.

The warning needs three things: a live depth reading, a draft reported by your
boat's instrument network, and a current tide reading. If any of those is
missing, it says so rather than guessing at a number. It also reads correctly
through a spring low that drops below chart datum, not just down to zero. The
depth readings used here run slightly shallow of the true figure, on the safe
side. So the warning tends to sound before the boat actually finds bottom, not
after.

If the tide feed goes quiet, the warning doesn't keep computing against
whatever it last heard. Once a tide reading is more than half an hour old, or
the low it was pointing at has already come and gone, the full-page view shows
**Tide forecast out of date** instead of a number. That stays until the next
tide update lands.

This is a running readout, not an alarm. It doesn't sound and needs no
silencing, and it updates continuously as the boat swings and the tide moves.
There are two things to know about it:

- **It can flip:** Because it reads the boat's position right now, it can flip
  between clear and too-shallow as you swing on the rode, even when the tide and
  the anchor haven't changed at all.
- **It looks one low ahead:** It only looks as far ahead as the next low tide,
  not a lower one that might follow later in the day.

## The rode planner

The rode planner is a sidebar on the anchor watch page. It helps you plan
pay-out and swing radius against tide-corrected depth and gust-seeded wind. You
can use it before lowering the anchor.

Its Depth field shows the figure the plan is computed against: the depth at this
spot at the next high tide, plus your bow roller height above the water. That's
the depth the chain has to reach on a rising tide, not just the depth under the
keel right now. Type over it to plan against a different figure. The field
won't accept a number that leaves no water at all once the tide and bow height
are backed out, and it says so rather than saving it.

Its map takes pins. Click open water to mark a bombie, a mooring block or the
nearest bit of shoreline, and watch the range close as you swing. Pins are
shared across every device watching the anchorage, so marking a hazard from the
bow shows up on the tablet at the helm. They expire with the anchoring, rather
than accumulating across seasons.

The map keeps the view you panned to for as long as you stay in the anchorage,
including across updates. When you anchor somewhere else, every device
recentres on the new anchor.

See [Set an anchor watch](../how-to/set-an-anchor-watch.md) for dropping,
setting the alarm radius and raising the watch.
