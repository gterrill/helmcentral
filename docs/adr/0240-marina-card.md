# ADR 0240: A marina card with a drawn map

## Status

Accepted (2026-10-31). Builds on [ADR 0191](0191-mate-cards-come-from-tool-results.md)
(cards come from tool results, not from the model) and
[ADR 0238](0238-mate-evaluates-a-marina.md) (the `marina_details` tool, which kept
the map geometry out of the model's sight for this card). Follows the wind rose
card ([ADR 0221](0221-mates-charts-stack-by-unit-and-average-angles-as-vectors.md))
in how the card is stored and decoded.

## Context

`marina_details` gives Mate a compact answer: berths, fuel, arm names, the
limit check. The operator then gets prose, and a marina is a place. The
pontoon arms, the fuel dock and the ramp are easier to find on a map than in a
sentence, and the plugin already returns the outline, every pier and a shape for
each facility. ADR 0238 held those on the host for exactly this.

Two things were missing. The model had no way to say how old the map is, and
the card had nothing to show for it: OpenStreetMap is volunteer-edited, and a
marina that rebuilt its pontoons last year may still be drawn the old way.
Overpass reports the age of its data in every answer (`osm3s.timestamp_osm_base`),
and the plugin was throwing it away.

## Decision

**`marina_details` puts one `marina` card in the card sink on every successful
call.** It is a snapshot, as the passage window is: the card stores what the
tool returned, and a reopened thread draws the same card without asking the map
server again. Comparing two marinas is two calls and so two cards. A call that
errors puts none, and a call with no card sink builds none. The result tells the
model a card is shown only where one is. The MCP service clears the card sink
from the tools' dependencies before every call, so an outside assistant, which
has no card, is never told to leave the facilities out of its answer, and no
tool builds a card nobody will see.

**The card is built from what the plugin returned, before the model's copy is
shrunk.** The model's text is capped, and `shrink` cuts the ashore list, the arm
names and the facility facts to fit. That is a limit on what the model reads, so
the card does not follow it. The plugin and the host already bound the data
(60 facilities, 40 mark kinds, 30 services ashore, 60 arms, 200 pontoons,
20 outline shapes, 300 points a shape),
and the card carries that whole set. When the plugin itself cut a list, the card
says so. Cutting the map's shapes (more than 200 pontoons or 20 outline shapes)
is a different thing from cutting a list the model reads, so it has its own flag,
`geometry_truncated`. The card carries it and shows a short note that the map is
simplified; the model's result and its reading do not mention it, since the model
never sees geometry. A shape over 300 points is an error, because the plugin
promises to thin each one.

**The payload** holds the marina's name, OSM id, centre, berths, fee, power,
opening hours, contact and charted limits; the arm names; the facilities (kind,
name, count, facts); the entrance mark kinds (kind, side, colour, count); the
services ashore (kind, name, hours, distance); the limit check rows (particular,
status, the boat's figure, the limit); the data date; and the
geometry. The footer names OpenStreetMap itself, so the card carries no source
string; the model's result keeps it. Every key is always present, with `""`, `null` or `[]` for what the map
does not record, so the frontend decoder can be strict. The host rejects a polygon
that does not close (its last point must repeat its first), as a malformed
answer. The decoder rejects a
field of the wrong type or a shape the map could not draw, and ignores a key it
does not know, so a newer server can add one, and the entry becomes the
`error` card like any other malformed kind. A Go golden file
(`testdata/mate_cards/marina_coral_sea.json`) is built from the plugin's own
output for the Coral Sea Marina fixtures, and the frontend tests decode that
file, so the two sides cannot drift.

**Geometry is what the map draws and no more.** The outline and each pontoon
(a line, or a polygon, with its name and whether it is private) are kept as the
plugin gave them. A facility becomes a point with its kind and name: a point
shape as it is, a line by its middle vertex, a polygon by the mean of its
corners. A facility the marina declares about itself arrives from the plugin with no
geometry; it still gets its row but no dot, because the middle of the outline is
not where the fuel is. Services ashore are points with a kind and
a name. Entrance mark shapes are not sent.

**The marks come from the OpenSeaMap raster, not from our own drawing.** The map
uses the same raster the Anchor watch map does, which already draws lateral marks
and beacons with their proper shapes and colours. Drawing them again from the
card's tags would put a second, cruder set under the first, and where the
volunteer tags and the seamark raster disagree (the fixture's beacons mostly have
no side tagged) we would be showing two answers. The card lists the marks in a
section, as a table of what the tags say, and leaves the picture to the raster.

**The map is the Anchor watch map with the AIS and the radar taken out.** It
uses the Carto base style from the backend proxy, Esri World Imagery over it (on
by default, with a toggle) and the OpenSeaMap raster above that. The marina's
outline, the pontoons with their names and the facility dots go on top, in theme
tokens read when the map draws, as the offline coastline map does. Services
ashore are off until the operator turns them on, since forty dots would bury the
pontoons. The view fits the outline and the pontoons with padding, and the map
uses cooperative gestures so that scrolling the Mate thread does not zoom it.
The map is its own lazily loaded module, so a thread with no marina never loads
MapLibre. A card with no outline line or polygon and no pontoon line draws no map and no
placeholder; a marina mapped only as a point has nothing to draw. A line is
labelled, and a facility on a line placed, at the point half-way along its
length, not at a vertex. A browser without WebGL2 shows a line saying so
without reading the theme tokens.

**The data date is Overpass's, passed through, and never the clock.** The plugin
reads `osm3s.timestamp_osm_base` from the details answer and returns it as
`data_as_of`. The host checks it is an RFC 3339 time and fails the call if it is
not. The model's result carries it too, so Mate can say the map is a few days old
or a year old. When the server gave none, the field is empty, the model's result
omits it, and the card shows only "OpenStreetMap". It is not filled from the
current time, which would claim a freshness nobody checked.

**Words and links.** Kind slugs such as `haul_out` get operator labels. A fuel
facility with no fuel facts reads "Fuel · type unknown", since the map says the
fuel is there and not which. The limit badges follow the particular, with
"exceeds" in the alert palette, "ok" plain, and "not charted" and "particular not
set" muted with the status word, because neither means the boat fits. The website
is a link only when it parses as http or https; the map is editable by anyone, so
`javascript:` and `data:` text is shown as plain text. A phone number is a `tel:`
link only when it looks like one.

## Consequences

- The card is bigger than most: the Coral Sea card is about 20 KB of JSON,
  nearly all of it pontoon and facility geometry. A thread that reopens it reads
  it from the stored card row, not from Overpass.
- A snapshot goes stale as the map does. The data date says how stale.
- Distances to shops and services ashore follow the operator's distance units:
  metres and kilometres, or nautical miles for imperial. The boat's draft and
  length, and the charted limits, stay in metres, as the tool gives them.
- The marina's own map PDF (many marinas publish one) is not fetched. Reading it
  is a later step: Mate would save it to Documents when the operator asks, which
  needs the file store and a request the operator makes, not a download made
  quietly while answering a question.
- The plugin must be rebuilt for the card to show a data date; an older plugin
  gives a card without one, and the host accepts that.

## Rejected

- **Drawing the entrance marks ourselves.** See above: the raster already does it
  properly, and two sets would disagree.
- **Refetching the marina on reopen.** The prose beside the card was written about
  the figures the tool read. A card that changed under it would contradict it.
- **Cutting the card to the model's shrunk text.** The model's cap protects its
  context; the operator has no such limit.
- **Filling the date from the clock** when Overpass gave none. A missing date is
  shown as missing.
- **Fetching the marina's PDF map into the card.** Out of scope here, and it needs
  an operator request.
