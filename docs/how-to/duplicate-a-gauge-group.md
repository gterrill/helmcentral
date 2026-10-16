# Duplicate a gauge group onto another instance

Most machinery on a boat comes in pairs. Once you have a tile reading the port
engine, the starboard one wants the same gauges with the same scales and the
same bands, pointed at the other machine.

Duplicate works the way Save As does. You open the tile you already have, change
what differs, and press **Duplicate** instead of **Save**. Your edits go to the
new tile and the one you opened is left exactly as it was.

## A group that shows equipment

The worked example is a Port Alternator tile becoming a Starboard Alternator
tile.

1. Turn on **Layout** mode.
2. On the tile you want to copy, choose the **Configure** control, the sliders
   icon in the tile header.
3. Under **Equipment**, choose the other alternator.
4. Change **Title** to `Starboard Alternator`, or clear it to show the item's
   name.
5. Choose **Duplicate**. The new tile is added below everything else on the
   page. The Port tile keeps the equipment it had when you opened it.

Nothing else needs touching. Labels, scales and decimals come across, and the
bands come from the equipment's profile, so a warning at 100 °C on the port
alternator is the same warning on the starboard one when both follow the same
profile.

If the new tile says `Not mapped` or `Not reporting`, the item you chose has no
source or the boat is not reporting it. See
[Show equipment on a tile](show-equipment-on-a-tile.md).

## A group of paths you pick

A group set to **Paths I pick** keeps its own paths and bands. To point a copy at
another instance, change each gauge's **SignalK path**, usually one word or one
digit, then choose **Duplicate**. Check the new tile reads live values before
you leave Layout mode: a gauge showing dashes means its path is not being
published.

## If you only want a second copy, unchanged

Skip the dialog. Every tile that can exist more than once per page has a copy
button on its corner in Layout mode: gauges, gauge groups, engine clusters,
embeds, Nearby maps and lamp strips. That duplicates immediately, which is what
you want when the second tile belongs on another page rather than on another
engine.

Built-in tiles are one per page and have no copy button.

## Check the units survived

This matters for a group of paths. A gauge's **Quantity** and **Unit** decide how
its bands are converted before the alarm engine sees them. A band of 100 means
100 °C on a gauge declared as a temperature in Celsius, and it means the bare
number 100 on a gauge declared as **Raw**. Temperature arrives on the bus in
Kelvin, so a raw gauge compares 100 against a reading of 329 and raises a warning
at 56 °C that can never clear.

Changing a path only changes the quantity when the new path publishes units of
its own, which few paths on this boat do. Open one gauge on the new tile and
confirm **Quantity** and **Unit** read what you expect rather than **Raw**. A
group for equipment takes its units from the profile, so it does not have this
problem.
