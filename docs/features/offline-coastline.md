# Offline coastline

The offline coastline is a copy of the charted coastline and reefs, kept aboard
for the water around the boat. It means questions about land and shelter still
get an answer at anchor with no signal. Today Mate uses it to say which wind
directions an anchorage is protected from. It also uses it to work out how far a
passage is by water, round the islands and headlands in the way. A land layer
for the chart and distance-to-land checks could use it later.

It's **off until you turn it on**, because filling it uses the internet. The
steps are in [Turn on the offline
coastline](../how-to/turn-on-the-offline-coastline.md).

## What it holds

The map is cut into squares 1° of latitude by 1° of longitude. Each one is a
**coverage square**, named by its south-west corner. The square at 21°S 148°E
runs from 21°S to 20°S and from 148°E to 149°E. In each square Helmcentral keeps
the coastline and the reefs, taken from OpenStreetMap, and the date it was
downloaded.

**Coverage** is every square any part of which lies within 200 nautical miles of
the boat, plus every square you've downloaded earlier. Squares that are all land
are left out (see below). That's roughly 40 to 60 squares, and tens of
megabytes to download.

- **New squares only:** As the boat moves, only squares newly within 200 nm are
  fetched.
- **Squares behind you stay:** Sailing a cruise home again needs no downloads.
  Only **Clear coverage** removes them.

**Squares that are all land are skipped.** A square with no part of it within 20
nm of the sea, such as the middle of a continent, can't matter to a shelter
estimate. A shelter estimate looks no further than 15 nm from the water.
Helmcentral doesn't download such a square, and the page says how many it left out.

- **When in doubt, it downloads:** The Caspian Sea and Antarctica count as
  coastal, and so does any island too small for the land data to show.
- **Squares you already hold:** A square of that kind that you already hold
  stays.
- **Asking about an inland spot:** A shelter estimate asked for a position in an
  all-land square answers that the spot is inland, with no anchorage shelter to
  estimate.

Squares come down one at a time, nearest the boat first, with a pause between
each. They come from the Overpass server set in **Settings → Plugins →
OpenStreetMap (Overpass)**, the same one place names use. A public server can
take the better part of a minute per square in busy hours. A server aboard
answers in seconds. See [Run your own Overpass
server](../how-to/run-your-own-overpass-server.md).

A square with no coastline in it, which is open sea, is still a square, and
it's stored as one. If a square fails, Helmcentral keeps the reason, shows it on
the page, and tries it again later, waiting longer after each failure.

Helmcentral can't tell which squares are near without a position fix. It waits,
and the page says **Waiting for a position fix**. A fix means a position the GPS
has validated. An old or suspect position doesn't count. Mate keeps answering
from the squares already held.

## Keeping it current

Once a week Helmcentral asks whether the public map has changed inside each
square since you downloaded it, and keeps the count as that square's **edits
since**. It never replaces a square on its own. When some squares have edits
since, the page shows an **Update** button, and tapping it downloads those
squares again. New coverage, by contrast, downloads by itself, since it only
adds shoreline where there was none.

The count is a hint, not a diff. It sees coastline and reef outlines that were
edited or added. It doesn't see a deleted outline, or a point nudged within one
that wasn't otherwise touched. A square with a handful of edits from years of
mapping is usually not worth the bandwidth. One with many, or one you're about to
rely on, is.

## What Mate does with it

If you ask Mate about an anchorage, it estimates the shelter from this coastline
alone. That's a **shelter estimate**. Mate reads the shoreline out to 15 nm in
every direction and reports the **sheltered arcs**. These are the runs of
compass directions the spot is sheltered, partly sheltered or exposed from, with
the distance to the nearest land or reef each way. Inside a reef it reports
**reef shelter** instead, which depends on the tide and is never better than
partial. See [Mate](assistant.md) for how it uses the result.

A shelter estimate needs every square its 15 nm reaches. Near a square's edge
that includes the neighbouring squares, up to three more at a corner. If any of
them isn't held yet, Mate says which, and why:

- **Offline coastline is off.** Turn it on in Settings → Offline coastline.
- **Inland.** The position is on land well clear of the sea, so there's no
  anchorage shelter to estimate.
- **Outside coverage.** The position isn't in any square held, and it isn't
  within 200 nm of the boat.
- **Still downloading.** The square is in range and on its way.
- **A square failed.** It gives the reason the download failed.

Mate never falls back to looking the coastline up over the internet at that
moment. Either the squares are aboard or it says they aren't.

### Passage distances

When you ask how far it is to a place, Mate finds the shortest way **over
water** from the boat, or from a start you name. It does the same when you ask
for the time and fuel to get there. It goes round any island in the way. So a
place on the far side of an island gives you the real run, not the line across
the chart.

- **What it says:** The distance by water, the straight-line distance beside it
  for comparison, the course of the longest leg, each leg, and the bends it rounds.
- **Clearance:** The route keeps about 90 m (0.05 nm) off the charted shore. A
  gap between islands narrower than that counts as closed.
- **A place on the shore:** A town, jetty or marina is often charted a few
  metres inland. Mate measures to the water in front of it, up to half a mile
  out, and says it did.
- **Reefs:** A reef isn't treated as land. Mate's route can cross one, so check
  the chart.

A route needs every square between the two ends and a few miles either side, so
if one is missing Mate says which and gives no distance:

- **A square isn't downloaded.** It names the square, such as 21°S 148°E.
- **A position is on land.** Either end is more than half a mile inland.
- **No way through.** No water joins the two within 50 nm of the line.
- **Too far or too intricate.** A run over 300 nm, or a coast too broken to
  search at once, needs shorter legs.

Mate never replaces a missing route with the straight line. It says it can't
give a passage distance and why.

## Where it stops

- Coverage doesn't cross the 180° meridian or reach the poles. In those areas
  the page says so and downloads nothing.
- It's OpenStreetMap's coastline, so it's only as accurate and complete as the
  people who drew it. A position drawn as open sea reads as exposed all round.
- A route knows nothing of depth, tidal gates, restricted areas or reefs. It
  finds the shortest water the charted coastline allows, so check it against the
  chart.
- A shelter estimate can't see wind bending round a headland, gusts funnelling
  down a valley, or swell wrapping a point or crossing a reef. Treat it as a
  first look and compare it with the forecast.
- It needs the current OpenStreetMap place-names plugin (see [Provider
  plugins](../reference/plugins.md)). With an older copy, the page says the
  plugin needs updating and nothing downloads.
