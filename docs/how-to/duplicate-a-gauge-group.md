# Duplicate a gauge group onto another instance

Most machinery on a boat comes in pairs. Once you have a tile reading the port
engine, the starboard one wants the same gauges with the same scales and the
same bands, pointed at a different instance. This copies the tile and moves
every path across in one edit.

The worked example is a Port Alternator tile becoming a Starboard Alternator
tile. The same steps work for engines, gensets, or anything else where the
paths differ by one word.

1. Turn on **Layout** mode.
2. On the tile you want to copy, choose the **Configure** control (the sliders
   icon in the tile header) to open the Gauge Group dialog. The copy button on
   the tile's corner also works, but it duplicates immediately without giving
   you a chance to retarget first, leaving you to edit every path by hand
   afterwards.
3. Change **Title** to `Starboard Alternator`.
4. In the **Replace** field type `port`, and in **With** type `starboard`. Use
   whatever actually differs between your two paths: for alternators it is
   usually a digit, so `alternator.0` and `alternator.1`.
5. Read the preview under the two fields. It tells you how many of the tile's
   paths will change and shows each one before and after. If it says **No
   paths match**, your search text is not in any path and nothing will happen.
   Fix it before going on.
6. Choose **Apply**. The paths change; the labels do not. A wrong bulk label
   rewrite is silent and easy to miss, so those stay yours to edit.
7. Choose **Duplicate**. The new tile is added below everything else on the
   page, and the original keeps the configuration it had before you opened the
   dialog.

The copy carries the scales, the bands and the units across. Check the new tile
reads live values before you leave Layout mode. A gauge showing dashes means
the path is not being published, which usually means a typo in step 4 or an
instance that is genuinely not on the bus.

## Why not just repick the paths

You can open each gauge and pick its path from the list instead, and for a
one-gauge tile that is quicker. For anything larger, the find and replace row
is one edit instead of one per member, and it is harder to get half-right.

Picking a path also preselects the gauge's quantity and unit from whatever
SignalK publishes as that path's metadata. On this boat only a handful of paths
declare any, so for most of them the picker leaves your existing choice alone,
which is what you want when you are retargeting a tile that was already set up
correctly.

## Check the units survived

This matters more than it sounds. A gauge's **Quantity** and **Unit** decide
how its bands are converted before the alarm engine sees them. A band of 100
means 100 °C on a gauge declared as a temperature in Celsius, and it means the
bare number 100 on a gauge declared as a raw value. Temperature arrives on the
bus in Kelvin, so a raw gauge will compare 100 against a reading of 329 and
raise a warning at 56 °C that can never clear.

After duplicating, open one gauge on the new tile and confirm **Quantity** and
**Unit** read what you expect rather than **Raw**. If a tile is raising a
warning whose numbers look far too large, that is almost always what happened.

## Duplicating other tiles

Every tile that can exist more than once per page has a copy button on its
corner in Layout mode: gauges, gauge groups, engine clusters, embeds, Nearby
maps and lamp strips. Built-in tiles are one per page and have no copy button.

Only gauge groups get the find and replace row, because only they hold a set of
paths that share a prefix.
