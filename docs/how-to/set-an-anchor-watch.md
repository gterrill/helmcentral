# Set an anchor watch

[Anchor watch](../features/anchor-watch.md) covers what the watch does, what
Drop and Raise publish, and what automatic raise checks for. These are the
steps for running it from the anchor watch map.

## Drop the watch

1. Wait until the anchor is on the bottom and the boat is lying to it.
2. Open the anchor watch page and choose **Drop**. The watch is set at the
   boat's current GPS position, not wherever the map happens to be centred.
3. Set the radius as below. There is no circle to adjust until the watch is
   dropped.

## Move the anchor or change the radius

Do this on the Anchor Watch page. The map is for watching the swing, not
changing it. Tapping, dragging or double-tapping the chart never touches the
alarm radius or the anchor's saved position. To change either, open
**Adjust**.

1. Tap the **Move** icon in the map's control stack (top right). On a
   display with no map (an older tablet, say), a text **Adjust** button
   appears in the header instead. The icon stays lit while Adjust is open, and
   tapping it again is the same as **Cancel**.
2. The page stays as it was. The chart keeps its size, and the depth header,
   the rode planner and the chart's own controls stay where they are.
   **Cancel** and **Save** appear at the bottom of the chart.
3. **To move the anchor:** pan the chart. The anchor sits fixed at the centre
   of the screen, marked with a crosshair, and dragging the chart underneath
   it moves the anchor to wherever the crosshair ends up. A faded copy of the
   original anchor and circle stays on screen with a line back to it,
   labelled with how far and which way you've moved. The Distance and Bearing
   cards on the left follow the crosshair as you pan, showing where the boat
   sits from the new position.
4. **To look around:** pinch or use your scroll wheel to zoom the chart, as
   anywhere else on the map. Zooming never changes the alarm radius. The swing
   circle around the crosshair always covers the same amount of water, at any
   zoom level.
5. **To change the radius:** use the **−** / **+** control that slides out
   under the Move icon once Adjust is open (on a display with no map, it's in
   the panel that replaces the chart). Hold either button down to run through
   several metres quickly. The radius can't go below 5 metres, and it can't go
   above your **Chain Onboard** setting plus your boat's length (or Chain
   Onboard alone if no boat length is known). A note near the bottom of the
   screen names whichever limit you've hit. If the saved radius is already
   above that ceiling, Adjust opens showing the maximum instead, with a note
   such as **Was 180 m, above the maximum** so you know why the number
   changed.
6. If your change would leave the boat outside the alarm circle right now, a
   warning appears above the buttons: **Alarm would sound now**. The first tap
   on **Save** only arms it and changes the button to **Save anyway**. Tap it
   again to confirm. The warning clears if you widen the circle or move the
   anchor back under the boat. With no current GPS fix the boat can't be
   checked. A muted line reads **No GPS fix: can't check the boat against this
   circle**, and Save needs only the one tap.
7. Tap **Save** to commit, or **Cancel** to back out. Nothing you did in
   Adjust reaches the watch until you tap **Save**.

After you tap **Save**, a message at the bottom of the screen offers **Undo**
for a few seconds, to get the previous position and radius back without
reopening Adjust.

Changing only the radius (the −/+ control, or the no-map path) never touches
the anchor's saved position. It doesn't reset your swing trail, and it doesn't
need to hear back from the boat's instrument network first. Moving the anchor
does need that confirmation, so a radius-only change still goes through if the
instrument network is briefly unreachable.

The rode planner sidebar's **Apply as alarm radius** button also sets the
radius to match the swing your planned rode and boat length need, without
opening Adjust.

If the drop landed somewhere badly wrong, raise the watch and drop again once
you're lying properly. That's usually simpler than adjusting a long way.

## Raise the watch

Open the Anchor Watch page, choose **Raise** and confirm. This clears the
watch, the trail and any pins placed in the rode planner for this anchoring.
The Anchor Watch tile on a dashboard page has no Raise, so a stray touch on
the tile can't end the watch.

To have the watch raise itself when you motor away, turn on **Auto-raise
anchor watch when under way** under **Settings → Anchor Watch**. On a tablet or
larger screen, the gear icon in the header opens this section directly while
Anchor Watch is on screen. It is on by default and needs your engines ticked
under **Settings → Vessel**, so it knows when you are under power. It fires
once the boat has been running under power, outside the circle and doing at
least 3 knots for 15 seconds straight. Idling out slower than that still needs
Raise by hand.

## Mark a hazard with the rode planner

While the rode planner sidebar is open, click open water on its map to drop a
pin on a bombie, a mooring block or a stretch of shoreline you want to watch
your swing against. The pin appears on every device watching the same
anchorage and clears when you raise the watch.
