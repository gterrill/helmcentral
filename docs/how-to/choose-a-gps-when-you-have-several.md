# Choose which GPS SignalK uses when you have several

When the boat has more than one GPS, you tell SignalK which one to use, so
that the position doesn't hop between receivers while the boat sits still.
The [anchor watch](../features/anchor-watch.md) and the boat's anchored,
moored, motoring or sailing state both depend on a steady position.

## Rank the receivers

1. Decide the order you trust your receivers in. As a rough guide, put a
   satellite compass or a dedicated GPS on the NMEA 2000 network first. Put the
   GPS inside the AIS transponder next, and a GPS that reaches SignalK over
   NMEA 0183, for example through a WiFi bridge, last.
2. Open the SignalK admin page in a browser and log in.
3. Go to **Data** then **Source Priorities**.
4. In the **Paths** column, click `navigation.position`. The sources that send
   a position light up in the **Sources** column, so you can see what you have.
5. Under **Path overrides**, add an override for `navigation.position`.
6. Pick your best receiver as source **1**, then the others in your order.
   Leave **Fan out** unticked.
7. Add the AIS transponder's echo of your own position to the list as well,
   and untick **Enabled** next to it. SignalK names it with an `AI` ending,
   such as `Vesper_Cortex.AI`. It's the position your AIS sends to other
   boats, so it's coarser and arrives late. A source left off the list can
   still slip through now and then, so list it and switch it off.
8. Leave **Fallback after** at 15000 ms. If your first choice goes quiet for
   15 seconds, SignalK moves to the next one.
9. Do the same for `navigation.datetime`, `navigation.speedOverGround` and
   `navigation.courseOverGroundTrue`, with the receivers in the same order.
   Don't skip the time. Helmcentral works out how fast the position moved
   from the GPS clock, so a clock that hops between receivers gives it false
   speeds too.
10. Click **Save**.

Use path overrides rather than dragging the sources into order at the top of
the page. That ranking applies to every path the sources share, so it would
also change where your depth, wind and pressure come from.

## Signs you need this

Each receiver sits in its own spot on the boat, and on a catamaran they can be
a whole beam apart. When SignalK passes on whichever one spoke last, the boat
seems to jump several metres at a time. Here's what that does:

- **Position held at the last good fix:** While the boat is anchored or moored
  with the engines off, Helmcentral sees it move several metres in a second.
  It stops trusting the GPS and every screen holds the last trusted position.
  **Settings** then **Logs** shows warnings like "position jump implies
  17.0 kts at anchor".
- **Boat state changing on its own:** The `signalk-autostate` plugin adds the
  jumps up as distance travelled. It can decide the boat is under way while
  it's tied to the dock, and then switch back a few minutes later.
- **Collision profile reset:** Helmcentral switches the AIS collision profile
  each time the boat state changes. A false "under way" overwrites the profile
  you chose, so you find yourself setting it again.

A single NMEA 0183 connection can carry two receivers under one name, when
both are wired into the same bridge. SignalK can't tell them apart, so a
ranking can't stop that connection hopping. That's why it goes last.

## Check it worked

- **In SignalK:** Open the **Data Browser** and find `navigation.position`.
  Its source should be the receiver you put first.
- **Boat state:** It should stay anchored or moored while the boat is still,
  rather than flicking to under way and back.
- **In Helmcentral:** While the boat is anchored or moored with the engines
  off, the "position jump" warnings in **Settings** then **Logs** should stop.
  Helmcentral only runs that check in those states, so a quiet log while the
  boat state says under way proves nothing.

When your first receiver drops out for more than 15 seconds, you may see one
jump as SignalK changes to the next. That's expected, and it jumps back once
the first receiver returns.

If SignalK flags device instance conflicts on its NMEA 2000 device page, fix
those too. Two receivers on the same instance don't confuse SignalK, but they
can confuse a chartplotter or a bridge that picks its GPS by instance. If that
bridge feeds SignalK, its hopping reaches you as well.

See [Hierarchy of sensor needs](../reference/sensor-needs.md) for what the GPS
and the autostate plugin open up in Helmcentral.
