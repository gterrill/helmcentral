# Add a Nearby map

You need write access, and you do this in layout mode.

1. Toggle layout mode in the header (desktop widths only).
2. Choose **Add Tile**, then **Nearby map…**.
3. Give it a title, or leave it blank for "Nearby".
4. Set the range, 0.5 to 25 nautical miles. This is the radius searched around
   the vessel, not how much of the map you see. The map keeps the boat at the
   centre and zooms to fit the five nearest matches. A wide range with matches
   close by gives a close-up, and matches further out than those five can fall
   off the map.
5. Choose a layout. **Map only** fills the tile with the map. **Map and list**
   adds a ranked list of the five nearest matches beside it, so it needs a
   wider tile.
6. With **Map and list**, set **Summary cycle** to the number of seconds each
   match stays highlighted, with a ring on its marker, before the list moves to
   the next. The range is 3 to 120 seconds, and 10 is the default. A match's
   description shows too if it has one, though many points of interest don't.
   Leave this alone if you'd rather not think about it. Details of what the map
   shows during the cycle are below.
7. Tick the categories you want: anchorages, bays, islands, marinas, fuel, boat
   ramps, moorings, historic landmarks, lookouts, dive and snorkel spots, and
   walking trails. At least one is required.
8. Toggle **Show AIS traffic** and **Show own trail** as you want them. Both
   can be changed later.
9. Choose **Save**.

The tile starts polling immediately. The first few points may take a moment to
appear the first time a given area is searched. After that the answer is cached
for a few hours, so reopening the same stretch of coast is fast.

When SignalK is navigating a route, its line appears on the Nearby map
automatically, with the leg you're on drawn brighter and thicker than the rest.
Nothing shows while no route is active, and there's nothing to turn on.

You can add more than one Nearby map, to the same page or different pages, each
with its own range and categories. For instance, a wide "Anchorages" map at 10
nm on the passage page and a tight "Fuel and moorings" map at 3 nm on the
anchored page.

To change the settings later, reopen layout mode and choose the gear icon on
the tile's title bar. To remove the tile, use the same X every other tile uses.

## What the map does during the summary cycle

The map itself stays still, so that a slow display such as the wall screen
isn't asked to redraw it. Only the highlight moves. In the map-and-list layout
a curved arrow runs from the boat to the highlighted match, and it switches to
the next match when the list does.

- **Close to the boat:** Sometimes a match is so near that the two markers
  almost touch. There's no room for an arrow then, so only the ring shows.
- **The name:** The highlighted match always keeps its name on the map, even if
  another match sits close beside it.
- **Map only:** There's no arrow in this layout, because there's no list to say
  what it points at.
- **GNSS warning:** While the GNSS badge shows, the position may not be real, so
  the arrow is hidden and the map doesn't move.

As the boat moves, its marker moves across the map. The map reframes, with one
instant jump and no sliding, in these cases:

- **The boat nears the edge:** The map recentres on the boat.
- **The five nearest matches change:** A new place comes into the list or one
  drops out, and the map fits the new set. Two matches swapping places in the
  list doesn't count.
- **The tile changes:** You resize it, switch between **Map only** and **Map and
  list**, or change the range.

You can pan and zoom the map yourself on a normal screen, and it stays where you
left it until one of those things happens.

## Choosing a data source

The default provider is OpenStreetMap and needs no setup. If you have a Google
Places API key, an operator can switch the provider in **Settings → Tiles →
Nearby**. Google covers marinas and named attractions well but has no
equivalent for most marine categories, such as moorings or boat ramps. Those
categories show up empty rather than pretending Google has an answer.

See [POI categories](../reference/poi-categories.md) for which categories each
provider actually covers, and [Plugins](../reference/plugins.md) for installing
a different `poi` provider plugin.

OpenStreetMap's gear icon in that same provider list opens its settings,
including an **Overpass server** field. If the public `overpass-api.de`
instance is unreachable from your network, point the field at a mirror such as
`overpass.openstreetmap.fr`. The change applies on the next Nearby lookup, with
no restart.

A mirror other than that one also needs an allowlist entry, described in
[configuration.md](../reference/configuration.md#overpass). A server on the
boat's own network works too. See
[Run your own Overpass server](run-your-own-overpass-server.md).

## Place names is a separate picker

Three things come from a different setting: the name shown on the position
tile, the name on the anchor pin, and what Mate resolves when you ask about a
place by name. That setting is **Settings → Plugins → Place names**. It defaults to the same OpenStreetMap
plugin as Nearby. You can point it at a different installed plugin without
changing what Nearby uses, or the other way round.

Only a plugin that supports place-name lookups appears in that list. Not every
`poi` plugin does (Google Places, for instance, is Nearby-only). Its gear icon
opens the same settings as the matching Nearby card, since it's the same plugin
either way. The **Overpass server** field above is one and the same setting
from both tabs.

With the OpenStreetMap plugin, the name is the nearest named anchorage, marina
or harbour, bay or island. An anchorage wins over a marina, a marina over a
bay, and a bay over an island. So alongside you see the marina's name, and at
anchor you see the anchorage's. A marina only counts when the boat is within
about 500 m of it, so anchored in a bay near a port you see the bay, not the
port.

The lookup starts close to the boat and widens in steps up to about 10 km, so
open water near a coast still gets a name. Near a headland the nearest bay can
be one across the water rather than the one you are in. Towns and suburbs are
never used. If nothing named lies within about 10 km, the tile shows a dash.
