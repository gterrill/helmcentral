# Duplicate a gauge group onto another instance

Most machinery on a boat comes in pairs. Once you have a tile reading the port
engine, the starboard one wants the same gauges with the same scales and the
same bands, pointed at a different instance.

Duplicate works the way Save As does. You open the tile you already have, change
what differs, and press **Duplicate** instead of **Save**. Your edits go to the
new tile and the one you opened is left exactly as it was.

The worked example is a Port Alternator tile becoming a Starboard Alternator
tile. The same steps suit engines, gensets, or anything else where the paths
differ by one word or one digit.

1. Turn on **Layout** mode.
2. On the tile you want to copy, choose the **Configure** control, the sliders
   icon in the tile header.
3. Change **Title** to `Starboard Alternator`.
4. For each gauge in the list, change its **Path** to the other instance. For
   alternators that is usually one digit, `electrical.alternator.0.current`
   becoming `electrical.alternator.1.current`.
5. Choose **Duplicate**. The new tile is added below everything else on the
   page. The Port tile keeps the title and paths it had when you opened it.

Nothing else needs touching. Labels, scales, decimals and bands all come across,
so a warn band at 100 °C on the port alternator is a warn band at 100 °C on the
starboard one.

Check the new tile reads live values before you leave Layout mode. A gauge
showing dashes means its path is not being published, which is usually a typo in
step 4 or an instance that genuinely is not on the bus.

## If you only want a second copy, unchanged

Skip the dialog. Every tile that can exist more than once per page has a copy
button on its corner in Layout mode: gauges, gauge groups, engine clusters,
embeds, Nearby maps and lamp strips. That duplicates immediately, paths and all,
which is what you want when the second tile belongs on another page rather than
on another engine.

Built-in tiles are one per page and have no copy button.

## Check the units survived

This matters more than it sounds. A gauge's **Quantity** and **Unit** decide how
its bands are converted before the alarm engine sees them. A band of 100 means
100 °C on a gauge declared as a temperature in Celsius, and it means the bare
number 100 on a gauge declared as **Raw**. Temperature arrives on the bus in
Kelvin, so a raw gauge compares 100 against a reading of 329 and raises a warning
at 56 °C that can never clear.

Changing a path only changes the quantity when the new path publishes units of
its own, which few paths on this boat do. So a tile that was set up correctly
stays correct through a retarget. It is still worth opening one gauge on the new
tile and confirming **Quantity** and **Unit** read what you expect rather than
**Raw**.

If a tile raises a warning whose numbers look far too large for the units on the
label, that is almost always what has happened. Fix the quantity rather than the
threshold.
