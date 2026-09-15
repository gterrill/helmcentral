# Add a Nearby map

With write access, in layout mode:

1. Toggle layout mode in the header (desktop widths only).
2. Choose **Add Widget**, then **Nearby map…**.
3. Give it a title, or leave it blank for "Nearby".
4. Set the range, 0.5 to 25 nautical miles. This is the radius searched
   around the vessel, and roughly what the map zooms to fit.
5. Choose a layout: **Map only** fills the tile with the map; **Map and
   list** adds the ranked list of the five nearest matches beside it, so it
   needs a wider tile to read comfortably.
6. Tick the categories you want: anchorages, bays, islands, marinas, fuel,
   boat ramps, moorings, historic landmarks, lookouts, dive and snorkel
   spots, and walking trails. At least one is required.
7. Toggle **Show AIS traffic** and **Show own trail** as you want them. Both
   can be changed later.
8. Choose **Save**.

The widget starts polling immediately. The first few points may take a
moment to appear the first time a given area is searched; after that the
answer is cached for a few hours, so re-opening the same stretch of coast is
fast.

Add more than one Nearby map to a page, or to different pages, each with its
own range and categories: a wide "Anchorages" map at 10 nm on the passage
page and a tight "Fuel and moorings" map at 3 nm on the anchored page, for
instance.

To change the settings later, reopen layout mode and choose the gear icon on
the widget's title bar. To remove it, use the same X every other widget
uses.

## Choosing a data source

The default provider is OpenStreetMap and needs no setup. If you have a
Google Places API key, an operator can switch the provider in **Settings →
Widgets → Nearby**; Google covers marinas and named attractions well but has
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
