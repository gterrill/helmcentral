# Show equipment on a tile

An engine cluster, and a gauge group set to show equipment, follow a piece of
equipment you have set up rather than a list of paths. If you choose another
engine, the readings, scales and alarm limits all come with it. That keeps a
port tile and a starboard tile honest when you adjust a limit.

Before you start, the engine needs to be an equipment item mapped to what the
boat reports. See [Set up your vessel](set-up-your-vessel.md).

## Add an engine cluster

1. Turn on **Layout** mode and choose **Add tile → Engine Cluster…**.
2. Under **Engine**, choose the engine. The list shows your mapped engines.
   An item that isn't mapped to anything yet is listed under **Not mapped yet**
   only if it could be an engine. That means it's in the Propulsion system, or
   it follows an engine profile. A spare, a tool or a consumable never appears
   there. See below for mapping. Two items with the same name are told apart by
   where they are and their serial number.
3. Leave **Title** blank to show the engine's name, or type your own.
4. Choose **Fill slots from profile**. The ring, the hours, the four readout
   cards, the coolant arc and the gearbox are filled from the engine's
   profile. You can change any of them afterward on the tabs below.
5. Check **Engine side**, Port or Starboard. Filling the slots sets it from the
   engine's source, so change it after filling if it's wrong. Starboard
   mirrors the card.
6. Each tab covers one part of the card. On a tablet or a computer, the small
   picture of the card at the top lights up the part you're editing:
   - **Dial**: the ring, the hours in the centre and the two arcs at the foot
     of the dial. The right arc is the gearbox oil temperature, filled from
     the engine's profile like the coolant arc.
   - **Readouts**: the four cards round the dial.
   - **Footer**: the starter battery, the alternator, the gearbox oil pressure
     and the exhaust. Open a row to choose its equipment and the reading it
     shows. Filling the slots picks the alternator you named for that side
     under **Roles**. A row without equipment says **Not shown** and is left
     off the card.
   - **Fuel**: choose **Add fuel rail**, then choose a tank for each bar
     (forward and aft). The arcs sit on the outer edge for the engine's side.
     A bar takes its colours from the tank's own alarm levels, so there's
     nothing to set on the bar. See [Change a tank's alarm
     levels](change-tank-alarm-levels.md).
7. Choose **Save**. If it's greyed out, the line beside it says what's
   missing, and a tab with an empty slot shows an amber dot.

For the second engine, duplicate the tile, choose the other engine and press
**Fill slots from profile** again.

## Add a gauge group for equipment

1. Choose **Add tile → Gauge Group…**.
2. Under **Shows**, choose the kind of equipment (an engine, an alternator, a
   generator), then choose the item. **Paths I pick** keeps the group as a list
   of paths of your choosing.
3. Choose **Fill gauges from profile**, or add gauges one at a time and name the
   **Reading** each shows.
4. Choose **Save**.

## Change a limit from a tile

Each gauge with a scale lists its zones. When you edit one, the form shows
which equipment and tiles the change applies to, for example "Applies to every tile
and alarm using Yanmar 4JH (Port engine, Starboard engine)". Saving the tile
saves the limit to the profile. No limit belongs to a single tile.

The reading's quantity and unit come from the profile. That means limits are
always in the profile's unit, and you can't change them on the tile.

If the profile has no gauge for a reading, the form says so and shows no zones.
Add the reading to the profile under **Inventory → Profiles** to give it some.

## Limits a converted tile still holds

A tile that was set up before you chose equipment keeps its own limits, and
they keep raising their alarms, until the equipment has a profile to hold them.
Here's what happens when you choose the equipment and save the tile:

- If the profile has no limit for a reading, the tile's limit moves to the
  profile and leaves the tile.
- If the profile already has its own, the form shows both and asks which to
  keep. You can't save until you choose.
- If the equipment has no profile, the limits stay on the tile. They move the
  next time you save it after you link a profile on the item's page.

## When the tile does not show readings

A tile that can't read its equipment says why, in the tile:

- **Pick equipment.** No item is chosen. Open the tile's settings and choose
  one.
- **Not mapped.** The item exists but isn't mapped to anything the boat
  reports. The tile links to the item.
- **Not reporting.** The item is mapped, but the boat isn't reporting that
  source now. An engine that's simply switched off still shows its tile, with
  `OFF`.
- **Equipment removed.** The item was deleted. Choose another. Deleting an item
  lists the tiles that show it first. See
  [Set up your vessel](set-up-your-vessel.md).

## Map an item without leaving the tile

When you choose an item that isn't mapped, a list opens of what the boat
reports of that kind that no item owns. Choose one and then **Map**. This
changes the item itself, for every tile and feature that uses it. Its alarms,
maintenance hours and Mate all read the same source. It's the same setting as
**Source** on the item's page.
