# Set an anchor watch

See [Anchor watch](../features/anchor-watch.md) for what the watch does, what
Drop and Raise publish, and what automatic raise checks for. These are the
steps for running it from the anchor watch map.

## Drop the watch

1. Wait until the anchor is on the bottom and the boat is lying to it.
2. Open the anchor watch page and choose **Drop**. The watch is set at the
   boat's current GPS position, not wherever the map happens to be centred.
3. Set the radius as below. There is no circle to adjust until the watch is
   dropped.

## Move the anchor or change the radius

Do this on the Anchor Watch page. The map itself is for watching the swing,
not for changing it: tapping, dragging or double-tapping the chart never
touches the alarm radius or the anchor's saved position while you're just
looking at it. To actually change either, open **Adjust**.

1. Tap the **Move** icon in the map's control stack (top right). On a
   display with no map (for example, an older tablet), a text **Adjust**
   button appears in the header instead.
2. On a phone, Adjust fills the whole screen: just the chart and a control
   bar at the bottom. On a tablet or a bigger display, it stays in the page
   layout you're already looking at.
3. **To move the anchor:** pan the chart. The anchor now sits fixed at the
   centre of the screen, marked with a crosshair, and dragging the chart
   underneath it moves the anchor to wherever the crosshair ends up. A faded
   copy of the original anchor and circle stays on screen with a line back to
   it, labelled with how far and which way you've moved, so you always know
   how far off the original drop you've gone.
4. **To change the radius:** pinch or use your scroll wheel to zoom the
   chart. The alarm circle stays the same size on screen; zooming changes how
   much water that circle actually covers, and the reading at the bottom
   updates to match. On a display with no map, use the **−** / **+** buttons
   in the control bar instead; hold either one down to run through several
   metres quickly.
5. Two shortcuts sit next to the radius reading: **Rode + LOA** sets the
   radius to your deployed rode plus your boat's length, and **Planner
   swing** sets it to match whatever the rode planner currently recommends.
   Either one is greyed out with a short reason if it doesn't have what it
   needs, for example no rode deployed yet, or no boat length set in
   Settings.
6. The radius can't go below 5 metres, and it can't go above your **Chain
   Onboard** setting plus your boat's length (or Chain Onboard alone if no
   boat length is known). Pinching or tapping past either limit simply stops
   there. If the radius already saved was above this ceiling — set before
   this limit existed, say — Adjust opens showing the maximum instead, with a
   note such as **Was 180 m, above the maximum** so you know why the number
   changed.
7. If what you're setting up would leave the boat outside the alarm circle
   right now, a warning appears: **Alarm would sound now**. Tapping **Set**
   the first time only arms it, changing the button to **Set anyway**; tap it
   again to confirm you mean it. The warning clears itself if you widen the
   circle or move the anchor back under the boat. With no current GPS fix,
   this can't be checked at all: instead of the warning, a muted line reads
   **No GPS fix: can't check the boat against this circle**, and Set only
   needs the one tap.
8. Tap **Set** to save, or **Cancel** to back out without changing anything.
   Nothing you did in Adjust reaches the watch until you tap **Set**.

Once you tap **Set**, a message at the bottom of the screen offers **Undo**
for a few seconds, in case you want the previous position and radius back
without opening Adjust again.

Changing only the radius (the −/+ buttons, a shortcut chip, or the whole
no-map path) never touches the anchor's saved position: it doesn't reset your
swing trail, and it doesn't need to hear back from the boat's instrument
network first. Moving the anchor itself does need that confirmation, so a
radius-only change still goes through even if the instrument network is
briefly unreachable.

You can also still use the rode planner sidebar's own **Apply as alarm
radius** button, which sets the radius to match the swing your planned rode
and boat length actually need without opening Adjust at all.

If the drop landed somewhere wrong entirely, rather than just needing a
nudge, raising the watch and dropping again once you're lying properly is
usually simpler than adjusting a long way.

## Raise the watch

Choose **Raise** and confirm. This clears the watch, the trail and any pins
placed in the rode planner for this anchoring.

To have this happen on its own when you motor away, turn on **Auto-raise
anchor watch when under way** under **Settings → Anchor Watch**. It is on by
default and needs no further setup: it fires once the boat has been running
under power, outside the circle and doing at least 3 knots for 15 seconds
straight, and idling out slower than that still needs Raise by hand.

## Mark a hazard with the rode planner

While the rode planner sidebar is open, click open water on its map to drop a
pin on a bombie, a mooring block or a stretch of shoreline you want to watch
your swing against. The pin appears on every device watching the same
anchorage, and clears itself when you raise the watch.
