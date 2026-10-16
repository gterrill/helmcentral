# Show equipment on a tile

An engine cluster, and a gauge group set to show equipment, follow a piece of
equipment you have set up rather than a list of paths. Change which engine the
tile shows by choosing another, and the readings, scales and alarm limits all
come with it. This is what keeps a port tile and a starboard tile honest when
you adjust a limit.

Before you start, the engine needs to be an equipment item mapped to what the
boat reports. See [Set up your vessel](set-up-your-vessel.md).

## Add an engine cluster

1. Turn on **Layout** mode and choose **Add tile → Engine Cluster…**.
2. Under **Engine**, choose the engine. The list shows your mapped engines. An
   item that is not mapped to anything yet is listed under **Not mapped yet** only
   if it could be an engine (it is in the Propulsion system, or follows an engine
   profile), never a spare, a tool or a consumable; see below. Two items with the
   same name are told apart by where they are and their serial number.
3. Choose **Fill slots from profile**, or pick each slot's **Reading** from the
   list of the profile's readings and the ones the boat reports for that engine. The ring, the hours, the four readout cards,
   the coolant arc and the gearbox are filled from the engine's profile. Change any slot after.
4. Choose **Engine side**, Port or Starboard. It is taken from the engine's
   source when you fill the slots, and starboard mirrors the card.
5. Under **Footer**, choose the equipment for the starter battery, the
   alternator and the exhaust, and the reading each shows. Filling the slots
   picks the alternator you named for that side under **Roles**; leave a part
   without equipment to leave it out. The **Dial arc, right** is the gearbox
   oil temperature, filled from the engine's profile like the coolant arc.
6. Leave **Title** blank to show the engine's name, or type your own.
7. To add a fuel rail, choose **Add fuel rail**, then choose a tank for each bar
   (forward and aft). The arcs sit on the outer edge for the engine's side.
   A bar can carry a **low fuel alarm**: the level it falls below, and how severe.
   Unlike engine limits, it belongs to the bar, because a tank has no profile.
8. Choose **Save**.

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

Each gauge with a scale lists its zones. Edit one and the form shows which
equipment and tiles the change applies to, for example "Applies to every tile
and alarm using Yanmar 4JH (Port engine, Starboard engine)". Saving the tile
saves the limit to the profile. There is no limit that belongs to one tile.

The reading's quantity and unit come from the profile, so the limits are always
in the profile's unit and you cannot change them on the tile.

If the profile has no gauge for a reading, the form says so and shows no zones.
Add the reading to the profile under **Inventory → Profiles** to give it some.

## Limits a converted tile still holds

A tile that was set up before you chose equipment keeps its own limits, and
they keep raising their alarms, until the equipment has a profile to hold them.
When you choose the equipment and save the tile:

- If the profile has no limit for a reading, the tile's limit moves to the
  profile and leaves the tile.
- If the profile already has its own, the form shows both and asks which to
  keep. You cannot save until you choose.
- If the equipment has no profile, the limits stay on the tile and move the next
  time you save it after you link a profile on the item's page.

## When the tile does not show readings

A tile that cannot read its equipment says why, in the tile:

- **Pick equipment.** No item is chosen. Open the tile's settings and choose one.
- **Not mapped.** The item exists but is not mapped to anything the boat
  reports. The tile links to the item.
- **Not reporting.** The item is mapped, but the boat is not reporting that
  source now. An engine that is simply switched off still shows its tile, with
  `OFF`.
- **Equipment removed.** The item was deleted. Choose another. Deleting an item
  lists the tiles that show it first; see
  [Set up your vessel](set-up-your-vessel.md).

## Map an item without leaving the tile

Choosing an item that is not mapped opens a list of what the boat reports of
that kind that no item owns. Choose one and **Map**. This changes the item
itself, for every tile and feature that uses it: its alarms, maintenance hours
and Mate all read the same source. It is the same setting as **Source** on the
item's page.
