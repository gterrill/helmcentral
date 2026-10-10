# Add a Nearby map

With write access, in layout mode:

1. Toggle layout mode in the header (desktop widths only).
2. Choose **Add Tile**, then **Nearby map…**.
3. Give it a title, or leave it blank for "Nearby".
4. Set the range, 0.5 to 25 nautical miles. This is the radius searched
   around the vessel; the map keeps the boat at the centre and zooms out to
   keep the nearest matches in view, never tighter than this range.
5. Choose a layout: **Map only** fills the tile with the map; **Map and
   list** adds the ranked list of the five nearest matches beside it, so it
   needs a wider tile to read comfortably.
6. With **Map and list**, set **Summary cycle** to how many seconds each
   match stays highlighted, with a ring on its marker, before the list moves
   to the next one - 3 to 120 seconds, 10 by default. Its description shows
   too if it has one; many points of interest don't. The map dives in to the
   first match and tilts, holds there, then flies on to each next one in a
   high arc that shows the boat and the whole area on the way. Each close-up
   keeps the boat in the picture: a place right beside you gets a street-level
   view centred on it, and for one further off the view slides toward the boat
   so both stay on screen.
   After the last
   match it pulls back out to the view centred on the boat and rests on it
   for a moment before starting again. A longer cycle, 15 to 20 seconds,
   gives each place a longer hold; a very short one shortens the moves to
   fit. With reduced motion set on the
   display it jumps instead of flying, and stays level. Leave this alone if
   you'd rather not think about it.
7. Tick the categories you want: anchorages, bays, islands, marinas, fuel,
   boat ramps, moorings, historic landmarks, lookouts, dive and snorkel
   spots, and walking trails. At least one is required.
8. Toggle **Show AIS traffic** and **Show own trail** as you want them. Both
   can be changed later.
9. Choose **Save**.

The tile starts polling immediately. The first few points may take a
moment to appear the first time a given area is searched; after that the
answer is cached for a few hours, so re-opening the same stretch of coast is
fast.

Activate a route and its line appears on the Nearby map automatically, the
leg you're on drawn brighter and thicker than the rest - nothing to turn on,
and nothing shows while no route is active.

Add more than one Nearby map to a page, or to different pages, each with its
own range and categories: a wide "Anchorages" map at 10 nm on the passage
page and a tight "Fuel and moorings" map at 3 nm on the anchored page, for
instance.

To change the settings later, reopen layout mode and choose the gear icon on
the tile's title bar. To remove it, use the same X every other tile
uses.

## Choosing a data source

The default provider is OpenStreetMap and needs no setup. If you have a
Google Places API key, an operator can switch the provider in **Settings →
Tiles → Nearby**; Google covers marinas and named attractions well but has
no equivalent for most of the marine-specific categories such as moorings or
boat ramps; those categories show up empty rather than pretending Google has
an answer for them. See [POI categories](../reference/poi-categories.md) for
which categories each provider actually covers, and
[Plugins](../reference/plugins.md) for installing a different `poi` provider
plugin.

OpenStreetMap's own gear icon in that same provider list opens its settings,
including an **Overpass server** field: if the public `overpass-api.de`
instance is unreachable from your network, point it at a mirror such as
`overpass.openstreetmap.fr` instead. This applies on the next Nearby lookup
with no restart. See
[configuration.md](../reference/configuration.md#overpass) for the allowlist
a mirror other than that one also needs.

## Place names is a separate picker

The name shown on the position tile, the anchor pin, and what Mate resolves
when you ask it about a place by name all come from a different setting:
**Settings → Plugins → Place names**. It defaults to the same OpenStreetMap
plugin as Nearby, but you can point it at a different installed plugin
without changing what Nearby uses, or vice versa. Only a plugin that
supports place-name lookups appears in that list; not every `poi` plugin
does (Google Places, for instance, is Nearby-only). Its gear icon opens the
same settings as the matching Nearby card, since it's the same plugin
either way - the **Overpass server** field above is one and the same
setting from both tabs.

With the OpenStreetMap plugin, the name is the nearest named anchorage,
marina or harbour, bay or island. An anchorage wins over a marina, a marina
over a bay, and a bay over an island, so alongside you see the marina's name
and at anchor you see the anchorage's. A marina only counts when the boat is
within about 500 m of it, so anchored in a bay near a port you see the bay,
not the port. The lookup starts close to the boat
and widens in steps up to about 10 km, so open water near a coast still gets
a name. Near a headland the nearest bay can be one across the water rather
than the one you are in. Towns and suburbs are never used: if nothing named
lies within about 10 km, the tile shows a dash.
