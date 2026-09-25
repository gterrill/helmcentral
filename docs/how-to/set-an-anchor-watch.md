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

## Set the alarm radius after dropping

Do this on the Anchor Watch page. The map itself is for watching the swing,
not for changing it — tapping, dragging or double-tapping the chart never
touches the alarm radius or the anchor's saved position, on this page or on
the Anchor Watch tile on a dashboard page.

- Use the **−** / **+** buttons next to the radius reading to step the alarm
  radius up or down 5 m (15 ft) at a time. Each press saves immediately; if
  it can't reach the boat's instrument network, a message says so and offers
  **Retry**.
- Or, in the rode planner sidebar, choose **Apply as alarm radius** to match
  the radius to the swing your planned rode and boat length actually need.

Moving the anchor's saved position has no control yet on this page — that is
coming in a later release. If the drop landed somewhere wrong, raise the
watch and drop again once you are lying properly to the new position.

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
