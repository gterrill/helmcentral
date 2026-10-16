# Engine Cluster

One engine, read at a glance from the helm: revs, oil, boost, load, fuel burn,
coolant, fuel in the tanks, and the electrical and drivetrain figures that tell
you the engine is charging and cooling properly. Place one tile per engine.

## What the tile shows

- **Tachometer.** Revs in the middle of a ticked dial, with the engine hours
  in a tab above the card. A coloured mark on the scale appears only for a
  band you have set.
- **Four readout cards** on the inner side of the dial: oil pressure, boost,
  load and fuel flow. Each has a bar showing where the reading sits against its
  normal range. The bar stays neutral while the reading is in range. It turns
  amber or red only while the reading is in an amber or red band, and the bands
  behind it brighten for the one the reading is in.
- **Two arcs at the foot of the dial.** Coolant temperature on the left, gearbox
  oil temperature on the right. An arc you have not connected is not drawn.
- **Fuel arcs** on the outer edge: forward and aft tank, with litres in the
  tanks against capacity.
- **Footer.** Starter battery voltage; the alternator, with its current, output
  power, temperature, charge stage and a one-hour trace of current (a dashed
  line marks the battery management system's charge limit, if you have
  connected it); the gearbox oil pressure; and the exhaust temperature.

## Port and starboard

Choose **Port** or **Starboard** for each tile. A starboard tile is a mirror
image: the fuel arcs and the run/warning/alarm marker sit on the outer edge, and the
readout cards and engine hours on the side facing the other engine, so a pair placed side by
side reads symmetrically.

## Colours and alarms

Colour means state, nothing else. A reading is neutral inside its normal band.
Amber and red appear only for a reading inside an amber or red band you have
set; those same bands are the alarm thresholds, so the tile and the alarms
cannot disagree.

The supplied engine profile gives only the green normal range. It does not
guess your warning and alarm limits. A reading outside the normal range with no
limit set shows amber, never red, because Helmcentral cannot know it has
crossed a limit, only that it has left the usual range. Set your own limits from
your engine manual: they belong to the equipment's profile, so the tile and the
alarms always agree.

Exhaust on a wet exhaust (water-injected) reads tens of degrees, so its scale
runs 0 to 100 °C. Add a red band above the temperature that means the raw water
has stopped, on the exhaust equipment's profile.

## When the engine is off

A switched-off engine shows `--` in its readings and a plain border, and says
`OFF` once, in a grey marker in the tile's title (the status marker above the
card is for a running engine). See Staleness in [Tiles](../reference/tiles.md).

## Which equipment each part reads

The engine's own readings (revs, oil, boost, load, fuel flow, coolant, gearbox)
come from the engine you pick. The starter battery, the alternator and the
exhaust are separate equipment, so you choose each in the tile's settings. The
alternator follows the side: pressing **Fill slots from profile** picks the port
or starboard alternator you named under Roles in **Settings → SignalK**. If no
alternator is named there, the alternator parts stay empty until you choose one.

## What it does not do

- It shows no engine oil temperature: these engines have no sensor for it. The
  right-hand arc is the gearbox oil temperature, which is a different reading.
- Charge stage shows the regulator's reported state. A state Helmcentral does
  not recognise is shown as its number.
- The alternator trace needs history recording switched on. Without it the
  tile says so instead of drawing a flat line.

To set a tile up, see [Show equipment on a tile](../how-to/show-equipment-on-a-tile.md).
