# Duplicate a gauge group onto another instance

Most machinery on a boat comes in pairs. Once you have a tile reading the port
engine, you'll want the same gauges on the starboard one. They need the same
scales and the same bands, pointed at the other machine.

Duplicate works like Save As. You open the tile you already have, change what
differs, and press **Duplicate** instead of **Save**. Your edits go to the new
tile, and the one you opened is left as it was.

## A group that shows equipment

The example here turns a Port Alternator tile into a Starboard Alternator
tile.

1. Turn on **Layout** mode.
2. On the tile you want to copy, choose the **Configure** control, the sliders
   icon in the tile header.
3. Under **Equipment**, choose the other alternator.
4. Change **Title** to `Starboard Alternator`, or clear it to show the item's
   name.
5. Choose **Duplicate**. The new tile is added below everything else on the
   page. The Port tile keeps the equipment it had when you opened it.

Nothing else needs changing. Labels, scales and decimals come across, and the
bands come from the equipment's profile. When both alternators follow the same
profile, a warning at 100 °C on the port alternator is the same warning on the
starboard one.

If the new tile says `Not mapped` or `Not reporting`, either the item you chose
has no source or the boat isn't reporting it. See
[Show equipment on a tile](show-equipment-on-a-tile.md).

## A group of paths you pick

A group set to **Paths I pick** keeps its own paths and bands. To point a copy at
another instance, change each gauge's **SignalK path** (usually one word or
one digit), then choose **Duplicate**. Before you leave Layout mode, check that
the new tile reads live values. A gauge showing dashes means its path isn't being
published.

## If you only want a second copy, unchanged

Skip the dialog. In Layout mode, every tile that can exist more than once per
page has a copy button on its corner. That covers gauges, gauge groups, engine
clusters, embeds, Nearby maps and lamp strips. It duplicates immediately. That suits a
second tile that belongs on another page, rather than on another engine.

Built-in tiles are one per page and have no copy button.

## Check the units survived

This matters for a group of paths. A gauge's **Quantity** and **Unit** decide
how its bands are converted before alarms see them. A band of 100 means 100 °C
on a gauge declared as a temperature in Celsius, and it means the bare number
100 on a gauge declared as **Raw**. Temperature arrives on the NMEA network in
Kelvin, so a raw gauge compares 100 against a reading of 329. It then raises a
warning at 56 °C that can never clear.

Changing a path only changes the quantity when the new path publishes units of
its own, and few paths on this boat do. Open one gauge on the new tile and
confirm that **Quantity** and **Unit** read what you expect, not **Raw**. A
group for equipment takes its units from the profile, so this doesn't affect
it.
