# Offline coastline

A copy of the charted coastline and reefs, kept aboard for the water around
the boat, so questions about land and shelter get an answer at anchor with no
signal. Today Mate uses it to say which wind directions an anchorage is
protected from. It is built to serve other things later, such as a land layer
for the chart and distance-to-land checks.

It is **off until you turn it on**, because filling it uses the internet. The
steps are in [Turn on the offline coastline](../how-to/turn-on-the-offline-coastline.md).

## What it holds

The map is cut into squares 1° of latitude by 1° of longitude. Each is a
**coverage square**, named by its south-west corner: the square at 21°S 148°E
runs from 21°S to 20°S and from 148°E to 149°E. In each square Helmcentral
keeps the coastline and the reefs, taken from OpenStreetMap, and the date it
was downloaded.

**Coverage** is every square any part of which lies within 200 nautical miles
of the boat, plus every square you have downloaded earlier. That is roughly
40 to 60 squares, tens of megabytes to download. As the boat moves, only squares
newly within 200 nm are fetched. Squares behind you stay, so a cruise you
sail home again needs no downloads. Only **Clear coverage** removes them.

Squares come down one at a time, nearest the boat first, with a pause between
each. A square with no coastline in it, open sea, is still a square, and is
stored as one. If a square fails, Helmcentral keeps the reason, shows it on the
page, and tries it again later, waiting longer after each failure.

With no position fix Helmcentral cannot tell which squares are near, so it
waits and the page says **Waiting for a position fix**. A fix means a position
the GPS has validated; a boat that has only an old or suspect position is not
treated as having one. Mate keeps answering from the squares already held.

## Keeping it current

Once a week Helmcentral asks whether the public map has changed inside each
square since you downloaded it, and keeps the count as that square's **edits
since**. It never replaces a square on its own. When some squares have edits
since, the page shows an **Update** button; tapping it downloads those squares
again. New coverage, by contrast, downloads by itself, since it only adds
shoreline where there was none.

The count is a hint, not a diff. It sees coastline and reef outlines that
were edited or added; it does not see a deleted outline, or a point nudged
within one that was not otherwise touched. A square with a handful of edits
from years of mapping is usually not worth the bandwidth. One with many, or
one you are about to rely on, is.

## What Mate does with it

Ask Mate about an anchorage and it estimates the shelter from this coastline
alone: a **shelter estimate**. Mate reads the shoreline out to 15 nm in every
direction and reports the **sheltered arcs**, the runs of compass directions
the spot is sheltered, partly sheltered or exposed from, with the distance to
the nearest land or reef each way. Inside a reef it reports **reef shelter**
instead, which depends on the tide and is never better than partial. See
[Mate](assistant.md) for how it uses the result.

A shelter estimate needs every square its 15 nm reaches. Near a square's edge
that includes the neighbouring squares, up to three more at a corner. If any
is not yet held, Mate says which, and why. It answers in plain terms:

- **Offline coastline is off.** Turn it on in Settings → Offline coastline.
- **Outside coverage.** The position is not in any square held, and not
  within 200 nm of the boat.
- **Still downloading.** The square is in range and on its way.
- **A square failed.** It gives the reason the download failed.

Mate never falls back to looking the coastline up over the internet at that
moment. Either the squares are aboard or it says they are not.

## Where it stops

- Coverage does not cross the 180° meridian or reach the poles. In those
  areas the page says so and downloads nothing.
- It is OpenStreetMap's coastline: only as accurate and complete as the people
  who drew it. A position drawn as open sea reads as exposed all round.
- A shelter estimate cannot see wind bending round a headland, gusts funnelling
  down a valley, or swell wrapping a point or crossing a reef. Treat it as a
  first look and compare it with the forecast.
- It needs the current OpenStreetMap place-names plugin (see
  [Provider plugins](../reference/plugins.md)). With an older copy, the page
  says the plugin needs updating and nothing downloads.
