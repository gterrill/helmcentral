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
- **Anchor Position Lost:** A frozen position can't show a dragging boat, so
  the watch raises its own alarm when it has had no trustworthy GPS fix for a
  full minute. It says the position is unknown, not that you're dragging, and
  it reaches you through the same channels as the drag alarm. A fix that drops
  out for less than a minute raises nothing. The alarm clears by itself when a
  good fix returns, or when you raise the anchor. If the drag alarm was already
  sounding, it stays up through the outage until a good fix shows the boat back
  inside the circle. If Helmcentral loses its link to the instrument network,
  you get this alarm as well as the instrument alert, because the position is
  unknown then too.
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
that says you have room, is left out, and so are the lines under it that name
where the depth comes from. A shallow-water warning always shows. The
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
- **Moving the anchor:** Because the trail is where the boat has actually been,
  moving the anchor position or changing the radius keeps it. **Undo** keeps it
  too. If you move the anchor a long way, the old swing stays on the map around
  the old position until you raise.
- **Raising the anchor:** Raising the anchor ends the trail, and the next drop
  starts a new one.
- **How much is kept:** The newest 10,000 points are kept and drawn, which
  covers days at anchor.
- **Reading it:** The line is dim yellow where the boat was longest ago and
  bright yellow where it is now, in a few steps along the whole stay. The oldest
  part stays solid, so a long swing never fades out of sight.

## The shaded swing circle

Inside the swing circle, the ground your boat hasn't crossed since the anchor
went down is shaded, and the ground it has crossed is clear. The shading is one
colour and says nothing about depth. It's there so you can see at a glance how
much of the circle has been sounded, which is the part the low-water estimate
can speak for.

- **A new drop:** The whole circle is shaded at first, because nothing has been
  sounded yet. A square clears each minute the boat swings over it, in squares
  about 2.5 m on a side. Small gaps between crossed squares are filled in, so
  the picture doesn't speckle.
- **Correcting the anchor:** The stay's readings are kept, so the squares you
  had cleared are redrawn around the corrected position straight away.
- **While you adjust:** The shading is hidden until you save or cancel.

## AIS targets on the map

Nearby AIS targets draw on the anchor-watch map as amber circles carrying the
vessel's name, range and bearing. Usually that is the 10 boats nearest to you.
While a watch is set, every boat within 500 m of your anchor draws as well. A
crowded anchorage can't hide a boat close to your anchor behind ten that are
nearer your hull. The list stops at 40 boats so a very
crowded anchorage can't swamp a phone. If more than that are near your anchor,
it keeps the ones closest to the anchor, and the anchor-watch map and the Nearby
Vessels tile both tell you how many it left out.

- **Collision alarms:** When a collision alarm is in force for one of them, its
  marker turns red. It gains a ring once the alarm reaches alarm severity. It
  stays red for as long as that alarm is active, including after you have
  acknowledged it.

## AIS tracks on the map

While a watch is set, Helmcentral also records where each target has been, so
you can see which way the boats around you have swung through the night.
Helmcentral keeps these tracks itself, and nothing needs to be installed on the
SignalK server for them.

- **Who is recorded:** Any target with an MMSI that comes within 500 m of the
  anchor. Helmcentral only sees targets between about 9 m and 5 km from your
  boat. So a boat almost touching yours isn't recorded. Neither is a target when
  you're more than 5 km from your anchor. A target that stays further out than
  500 m shows as a marker only. A target with no MMSI can't be followed from one
  report to the next, so it also shows as a marker only.
- **What is recorded:** A point each time the target reports a new position,
  stamped with the time of that report. It's no more than one point every 30
  seconds unless the target has moved 25 m. A ship sitting at anchor that
  reports every few seconds is thinned to one point every 30 seconds. A boat crossing the
  anchorage keeps a point for every 25 m, so its track reaches its marker. A
  boat that reports every few minutes is recorded at every report. A target
  that has gone quiet adds nothing.
- **Leaving the circle:** A target that moves out past 500 m stops adding points,
  but its track stays on the map for as long as the target does.
- **Gaps:** There's no line where a target left the circle and came back, or
  where more than 15 minutes passed between two reports. That includes any time
  Helmcentral itself was stopped.
- **Brightness follows age:** The last few minutes of a track are solid amber
  and the oldest part fades towards clear. The fade runs in a few steps from
  when the boat was first seen to now, so a boat seen for only a few hours
  still fades. It spreads over at least an hour and never over more than the
  24 hours that are kept. A boat that stops reporting fades as its track ages.
  The newest part is drawn on top. Age is counted on the boat's clock, so a
  screen with the wrong time still shows the same trails as the others.
- **Starting and ending:** The tracks start empty when you drop the anchor and
  end when you raise it. Moving the anchor position keeps them, as the swing
  trail does, and so does changing the radius. After a move, a boat is recorded
  while it is within 500 m of the new position. One that is now outside it adds
  no points. If it comes back inside, its track starts a new line. A watch that
  is already set when Helmcentral is updated starts recording from then.
- **Without a watch:** No tracks are recorded and none are drawn.

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

Drop is greyed out while there's no GPS fix, or while Helmcentral doesn't trust
the one it has, so you can't set a watch on a position the boat isn't at.
Wait for the fix to come back, then drop.

Your GPS antenna sits back from the bow, and the anchor goes down at the bow.
If you've set how far the antenna is from the bow in Settings → Anchor Watch,
**Drop** puts the anchor mark ahead of the GPS by that distance, in the direction
the boat is heading. The rode planner's Swing figure includes the distance
whenever you've set it. That's because the antenna can swing out to the rode plus
that distance from the anchor.

The distance is a length, not a direction, so Swing counts it even when the mark
itself hasn't been moved forward:

- **Before Drop:** the planner already includes the distance you've set.
- **A Drop with no heading, or made before you set the distance:** the mark can't
  be moved forward, so it stays at the GPS. With no heading, the readings say
  "Anchor mark is at the GPS, not the bow. There was no heading at Drop." With a
  map on screen they add "Move it with Adjust", because moving the mark with
  Adjust needs the map. The anchor is really one distance ahead of that mark, so
  Swing counts the distance twice. If you set the distance in Settings after such a Drop, Swing uses it.
- **A mark you've placed by hand in Adjust:** Helmcentral doesn't move the mark
  again, but Swing still includes the distance.

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

## Save the depth

While a watch is set, Anchor watch can give a saved place the depth under the
anchor. Mate then has a figure of your own the next time you ask about that
anchorage. The line at the foot of the rode planner shows what it will save, for
example "4.2 m at datum, from the sounder". It says whether the figure comes from
the sounder or from the depth you entered. Nothing is saved until you tap
**Save…** and confirm, and the confirm step names the saved place it will write
to. The line only shows while the planner is open.

If that saved place already holds the same depth, sounded on the day the anchor
went down, the **Save…** link gives way to "Already saved at" the place's name
and "for this stay", because saving it again would change nothing. The figures are
compared as shown, to a tenth. A place whose depth came from a chart, or from an
earlier day's sounding, still gets the **Save…** link. That lets you replace a
charted figure with the sounded one, or give the same figure a new date.

A sounder reading is the water below the transducer. Helmcentral adds how far
below the waterline the transducer sits, then takes off the tide at the drop, so
it matches a chart. That needs the **Transducer Depth** in Settings → Anchor
Watch. A depth you typed into the rode planner is taken as depth below the
surface, so it needs no transducer depth, only the tide. A tide below chart datum
counts as a real reading.

The tide has to be measured from chart datum for this to work. The worldwide
tide model Helmcentral uses until you choose a provider gives heights from mean
sea level instead, so it can't tell you the depth at chart datum. The line says
that, and there's nothing to save. Choose the Australian or US tide provider in
Settings under Plugins if it covers your waters, and drop the anchor again. The
rode planner, the tide line and the low-water clearance only use how far the
water rises and falls, so they work with either. While a watch is set, the
worldwide model takes its tide for the anchor position, so the figures stay put
as the boat swings.

The transducer depth is used here only. The rode planner and the low-water
clearance check don't use it yet.

If the depth, the tide at the drop or the transducer depth is missing, the
**Save…** link gives way to a sentence that says which. An older watch that doesn't record where
its depth came from asks you to enter the depth again in the rode planner.
Helmcentral doesn't guess a replacement. The steps are
in [Manage the places you've given Mate](../how-to/manage-saved-places.md#save-the-depth-from-your-anchor-watch).

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

The clearance is judged the way it's shown, to a tenth of a metre. So 0.46 m counts
as 0.5 m, and it meets a 0.5 m margin. A clearance below zero means the keel would
be on the bottom, so it's always too shallow, even with the margin set to 0.

The warning needs three things: a live depth reading, a draft reported by your
boat's instrument network, and a current tide reading. If any of those is
missing, it says so rather than guessing at a number. It also reads correctly
through a spring low that drops below chart datum, not just down to zero. The
depth readings used here run slightly shallow of the true figure, on the safe
side. So the warning tends to sound before the boat actually finds bottom, not
after.

Helmcentral reads the tide afresh each time it works the figures out, so it never
computes against a reading it heard a while ago. When something is missing, the
line says what:

- **No tide station:** You haven't set up a tide station, so there's no tide to
  project the depth with. The worldwide model has no station, so for it this
  shows when its plugin isn't installed.
- **Tide unavailable:** A station is set up, but Helmcentral couldn't get a usable
  tide from it, because the tide service couldn't be reached or gave no upcoming low.
  The worldwide model also reads this way when the boat has no position fix.
- **Tide forecast out of date:** The low the tide points at has already come and
  gone. The line stays this way until a new tide reading arrives.
- **No depth:** The depth reading has stopped for more than two minutes, so
  Helmcentral won't use the last figure it heard.
- **No clearance line at all:** The boat isn't anchored or moored, and no anchor
  watch is set, so there's no anchorage to check.

The figures are worked out every 15 seconds. If a refresh fails, the page keeps the
last verdict on screen, faded, with "not updated since" and the time it was worked
out. So a warning never disappears, and it never passes for a current one. If Helmcentral
hasn't managed to work the figures out even once, the line reads **Low-water
clearance unavailable**.

Helmcentral works this out on the boat's server, and Mate reads the same result,
so the page and Mate never give you two different answers. Open Mate from the
Anchor Watch page and ask something like "Why is it too shallow here?". Mate
answers for the spot the boat is on now. It gives the depth you have, the depth
it expects at the next low and the time, less your draft, compared with your
clearance. When you're short, it links to the Anchor Watch settings so you can
change the margin. Mate gives these figures when the boat is anchored or a watch is
set. Helmcentral holds no chart, so Mate never sends you to one for the depth at
your own boat.

This is a running readout, not an alarm. It doesn't sound and needs no
silencing, and it is worked out again every 15 seconds as the boat swings and the
tide moves.
There are two things to know about it:

- **It can flip:** Because it reads the boat's position right now, it can flip
  between clear and too-shallow as you swing on the rode, even when the tide and
  the anchor haven't changed at all.
- **It looks one low ahead:** It only looks as far ahead as the next low tide,
  not a lower one that might follow later in the day.

### Where the depth at low water comes from

The line under the clearance names its source, so you always know which one
you're reading.

- **Depth based on tide station prediction:** This is the depth now, less the
  fall the tide station predicts to the next low. It's what you see after you
  drop the anchor. A station can be a long way off, and the water at your
  anchorage may rise and fall by a different amount, so this is a first guess.
- **Locally calibrated depth estimate (4 h of readings):** Once Helmcentral has
  learned how the tide behaves where you are, this replaces the guess. The
  figure comes with a margin of error, for example **2.4 m ±0.2**. The hours
  count up as the readings build.

Helmcentral learns from the moment you press **Drop** until you raise the anchor.
Every minute it notes the depth and where the boat is, and sets them against
the station's predicted tide. A boat that swings over the same patch of seabed
at different heights of tide shows how much of the station's range the water
here really has. That's why it needs hours at anchor, and why it helps least
just after the drop.

It switches over by itself, with no fixed number of hours. The calibrated
estimate takes over when its margin of error is smaller than the clearance you
set in Settings → Anchor Watch. Until then the station estimate stays on screen
and the line says why:

- **Calibrating, with the hours so far:** There aren't enough readings yet.
- **Set a low-water clearance:** The clearance in Settings → Anchor Watch is
  zero, so there's nothing to judge the calibration against. Set one and it carries on.
- **Readings too scattered to calibrate yet:** The sounder is noisy, so its
  readings don't settle. A noisy sounder keeps the margin of error wide for
  longer, and the estimate stays on the station figure rather than pretend to
  be sure.
- **Readings don't fit the station's tide:** The readings disagree so badly with
  the station that Helmcentral won't use them.

Moving the anchor mark with Adjust keeps the readings, because it's the same
drop. A new **Drop** starts again from nothing. Readings are kept after you raise
the anchor, but only the stay you're on is used.

A calibrated estimate can only speak for the ground the boat has crossed. So the
warning is judged on the most cautious of three figures: the depth where the boat
lies now, the station estimate, and the shallowest spot the boat has sounded
(with 0.2 m added, because the boat may never lie on that exact spot at low water). The warning goes off when the
bottom of the margin of error on that figure is under your draft plus your
margin. When the shallowest spot is what set it off, the warning says so. The
line also shows **Shallowest sounded spot** whenever it is 0.2 m or more
shallower than the depth where you lie.

For the next high tide the line shows **High water**, with its margin of error,
and the deepest spot sounded when it's 0.2 m or more deeper. This is for
information only. It never raises a warning.

When the calibrated and station depths for the next low differ by more than your
margin, the line adds a note such as "The tide here doesn't follow Middle Island
(11 km away): it runs 64% of its range." It means the station is a poor guide
for this anchorage, and by how much. Before the calibration is ready there's no
note.

Mate gives the same figures. Ask it about the depth at low water and it names
the method, the hours of readings and the margin of error.

## The rode planner

The rode planner is a sidebar on the anchor watch page. It helps you plan
pay-out and swing radius against tide-corrected depth and gust-seeded wind. You
can use it before lowering the anchor.

It answers three questions and leaves the working out off the screen:

- **How much to pay out:** the Pay Out figure, with the scope and the swing
  radius beneath it. When the Catenary figure would fall below 3:1, the planner
  uses 3:1 and the scope reads "3.0:1 minimum".
- **Is what's out enough:** once you've entered the rode you have out, a badge
  reads "Enough rode", or "Short by" and how much more to pay out. It judges
  against the method on the tab you have open. With no rode entered, or that
  method unavailable, there's no badge.
- **What to set the alarm to:** the **Set alarm to** button at the foot shows the
  radius it will set, for example "Set alarm to 65 m", using the swing of the
  method you chose in Settings. It reads "Set alarm" and stays greyed out
  until there's a figure to set, for example when the boat length is missing.

Its Depth field shows the figure the plan is computed against: the depth at this
spot at the next high tide, plus your bow roller height above the water. That's
the depth the chain has to reach on a rising tide, not just the depth under the
keel right now. Under the field a short line says the figure is measured from the
bow roller, and either "at high tide" or "no tide station" when Helmcentral has
no tide to add. With nothing entered it asks you to type it from the bow roller. Type over it to plan against a
different figure. The field
won't accept a number that leaves no water at all once the tide and bow height
are backed out, and it says so rather than saving it.

Under each method's Pay Out figure the planner draws the rode side on. You see
the bottom, the water, your bow and the chain running up to the bow roller. It's
drawn to one scale, so a long rode looks long. The Catenary tab draws the
curve its load would give. The Ratio tab is drawn with that same load, so
you can see what its rule of thumb does in the planned conditions.

A warning shows when a rode is too short to lie on the bottom under the planned
load. On the Ratio tab the warning says the ratio rule is too short for the
planned conditions, which means the anchor would be lifted. Pay out the
Catenary figure or more.

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
