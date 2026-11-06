# Engine Cluster

One engine on one tile, readable from the helm. It shows revs, oil, boost,
load, fuel burn, coolant and fuel in the tanks. It also shows the electrical
and drivetrain figures that tell you the engine is charging and cooling
properly. You place one tile per engine.

## What the tile shows

Here's what each part of the tile tells you:

- **Tachometer.** Revs sit in the middle of a ticked dial, with the engine
  hours underneath them. A coloured mark on the scale appears only for a band
  you have set.
- **Warning or alarm.** While a running engine has a reading in an amber or red
  band, `WARN` or `ALARM` shows in the tile's title. A healthy engine shows
  nothing there.
- **Four readout cards** on the inner side of the dial: oil pressure, boost,
  load and fuel flow. Each has a bar showing where the reading sits against its
  normal range. The bar stays neutral while the reading is in range. It turns
  amber or red only while the reading is in an amber or red band, and the bands
  behind it brighten for the one the reading is in.
- **Two arcs at the foot of the dial.** Coolant temperature is on the left and
  gearbox oil temperature is on the right. If you haven't connected an arc, the
  tile doesn't draw it.
- **Fuel arcs** on the outer edge: forward and aft tank, with litres in the
  tanks against capacity. An arc turns amber and then red at the same levels the
  tank's alarms use, which you set on the tank's equipment page.
- **Footer.** It shows the starter battery voltage, the gearbox oil pressure
  and the exhaust temperature. It also shows the alternator with its current,
  output power, temperature, charge stage and a one-hour trace of current. If
  you have connected the battery management system, a dashed line on the trace
  marks its charge limit.

## Short, wide tiles

The tile picks whichever arrangement draws larger in the space it has. On a
squarer tile, such as one on the dashboard, the four footer readings sit in a
row under the dial. On a short, wide tile, such as one on a wall display, the
footer moves into a column of four cards beside the readout cards, one card
beside each readout. That column is on the side away from the dial, so the
whole card is shorter and the dial can be larger.

The card always fits inside its tile. If the tile is wider than the card needs,
the dial and the fuel arcs keep their size. The readout cards, and the footer
cards in the column, stretch to fill the extra width.

## Port and starboard

Choose **Port** or **Starboard** for each tile. A starboard tile is a mirror
image. The fuel arcs sit on the outer edge and the readout cards sit on the
side facing the other engine. That way a pair placed side by side reads
symmetrically.

## The settings dialog is the card

In Layout mode, the tile's settings gear opens a dialog with a picture of the
card in it. Every part of the picture is a button, so you click the part you
want to change and its settings open beside the picture. In a narrow window the
picture becomes a list of the same parts, and the settings open under the one
you click.

- **Across the top:** Choose the **Engine**, give the tile a **Title**, and
  pick the **Engine side**. A blank title shows the engine's name. **Fill from
  profile** replaces the ring, the hours, the two arcs, the readout cards and
  the gearbox pressure with the profile's. Any readout you added or changed is
  gone. The alternator, the starter battery, the exhaust and the fuel
  rail stay as you set them, and it fills any alternator row that is still blank.
- **The picture:** It shows the fuel rail, the ring with its hours, the two
  arcs and the readout cards. Under them sit the starter battery, the
  alternator and its five rows, the gearbox pressure and the exhaust. A
  Starboard tile shows the picture mirrored, the same as the tile.
- **Each part's outline:** A part that reads something has a plain outline. A
  part that **Save** is waiting on has an amber outline and says what it's
  missing. A part that shows nothing has a dashed outline and says **Not
  shown**. Amber words on their own, such as **Not sending**, are a warning and
  don't stop you saving.
- **Where it opens:** The dialog opens on the first part that needs you, or on
  the ring if nothing does. It stays on the part you choose, even after you fix
  it.
- **What you can set:** A part's settings show its **Reading**, its **Label**
  and, for the ring, the arcs and the readout cards, the scale. The quantity and
  unit are under **Units and quantity**, closed until you open them. They come
  from the profile for a reading the profile covers. There's no **Display**
  choice, because the tile draws each part its own way.
- **Add readout:** Below the readout cards, **Add readout** puts up another card,
  to a limit of four.
- **Zone conflicts:** If a limit on the tile disagrees with the profile's, that
  part gets an amber outline and the choice appears in its settings. You can't
  save until you make it.

If **Save** is greyed out, the line beside it names the part that's missing a
reading, and that part has the amber outline.

## Colours and alarms

Colour means state, nothing else. A reading is neutral inside its normal band.
Amber and red appear only for a reading inside an amber or red band you have
set. Those same bands are the alarm thresholds, so the tile and the alarms
can't disagree.

The supplied engine profile gives only the green normal range. It doesn't
guess your warning and alarm limits. A reading outside the normal range with no
limit set shows amber, never red. Helmcentral can't know the reading has
crossed a limit, only that it has left the usual range. Set your own limits from your
engine manual. They belong to the equipment's profile, so the tile and the
alarms always agree.

A wet (water-injected) exhaust reads tens of degrees, so its scale runs 0 to
100 °C. On the exhaust equipment's profile, add a red band above the
temperature that means the raw water has stopped.

## When the engine is off

A switched-off engine shows `--` in its readings. It says `OFF` once, in a grey
marker in the tile's title, where a running engine's `WARN` or `ALARM` would
be. The border goes plain unless a reading from other equipment, such as the
starter battery, is in a warning or alarm band, and that reading still shows.
See Staleness in [Tiles](../reference/tiles.md).

## Which equipment each part reads

The engine's own readings (revs, oil, boost, load, fuel flow, coolant, gearbox)
come from the engine you pick. The starter battery, the alternator and the
exhaust are separate equipment, so you choose each one in its own part of the
settings picture.

The alternator is chosen once, in the **Alternator** part of the picture. All
five rows beside it read from it: current, output power, temperature, charge
stage and charge current limit. Here's how the choice behaves:

- **Where it starts:** When you pick an engine, or open a tile that has no
  alternator yet, the alternator is the one you named under Roles in
  **Settings → SignalK**. It follows the **Engine side**: Starboard gets the
  starboard alternator and Port gets the port one. If the side isn't set, it
  comes from the engine. If you haven't named an alternator, or you said the
  boat has none, the alternator stays empty until you choose it.
- **If you change the side:** The alternator follows the **Engine side** you pick
  for as long as you haven't chosen the alternator yourself. Once you have, it
  stays where you put it. A starboard tile can use the port alternator, and
  Helmcentral doesn't warn you about it.
- **Which readings it fills:** Helmcentral looks at what the alternator is
  sending and fills in each part with the reading that part expects. A part the
  alternator doesn't send stays blank and is left off the tile, and its row in
  the picture says **Not sending**. The charge current limit has no expected reading,
  so it stays blank until you choose one, and its row says **Not shown**.
- **If you swap alternators:** A part keeps its reading if the new alternator
  sends it. If it doesn't, the part goes back to the reading it expects, or goes
  blank if the new alternator isn't sending that either.
- **If it sends nothing:** The alternator may be mapped but not sending any
  readings. A note under the picker then says so and tells you to check its
  source in Equipment. You can still save.
- **If you clear it:** Clearing the alternator blanks all five parts.

**Fill from profile** doesn't change the alternator. It fills any blank
alternator part from the alternator you picked, and leaves readings you've
already chosen alone.

The starter battery and the exhaust each have their own equipment, picked on
their own part. Helmcentral fills in the reading for them in the same way. Pick
the starter battery and its voltage is set. If the equipment isn't sending the
reading that part needs, the field says so and lists what it does send. You
can't save a battery or exhaust part that has equipment but no reading. Pick a
reading, or clear the equipment to leave the part out.

You can type a reading on an alternator part yourself. Helmcentral checks it
against what the alternator is sending, and against the alternator's profile.
The part's own expected reading always passes. If your reading is none of
those, you can't save, and the line beside **Save** names it. Pick one the
alternator sends, or clear it. If Helmcentral can't read what the boat is
sending, it doesn't block you.

## What it does not do

- **Engine oil temperature:** The tile shows none, because these engines have
  no sensor for it. The right-hand arc is the gearbox oil temperature, which is
  a different reading.
- **Charge stage:** It shows the state the regulator reports. If Helmcentral
  doesn't recognise a state, the tile shows it as its number.
- **Alternator trace:** It needs history recording switched on. Without that,
  the tile says so instead of drawing a flat line.

To set a tile up, see [Show equipment on a tile](../how-to/show-equipment-on-a-tile.md).
