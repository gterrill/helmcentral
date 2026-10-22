# ADR 0199: The passage map shows satellite imagery, and so do the still images

## Status

Accepted (2026-10-22). Amends [ADR 0174](0174-passage-export-is-a-self-contained-card.md)
for the static images only.

## Context

The passage map drew the Carto basemap and OpenSeaMap seamarks, with no
satellite view, though the Anchor Watch map has had one for a long time. Looking
at a passage past an anchorage or a reef pass is exactly where aerial
photography helps.

ADR 0174 limits the export to public tiles so that nothing in a pasted card
points back at the boat. Esri World Imagery is not one of those: the public
service needs an ArcGIS key, and the boat reaches it through its own proxy.

## Decision

1. **One shared imagery module.** The tile address, the zoom 9 to 10 fade, the
   credit, the imagery source and layer and the toggle button now live in
   `lib/world-imagery.ts` and `components/world-imagery.tsx`. The Anchor Watch
   map uses them unchanged.
2. **The passage map has its own satellite button**, top right on the main map
   (not the overview), on by default and remembered under its own storage key,
   apart from the anchor map's. Layer order is basemap, imagery, seamarks, place
   names, passage lines, markers. Over imagery each line gets a white casing and
   the place names switch to the white-on-black treatment the anchor map uses.
   The fade follows the zoom through a style expression, and the layer has a
   minimum zoom of 9 so no tiles are requested while zoomed out.
3. **Still images follow the button at export time**, as the overview map
   preference does. With imagery on, the image's map gets the world imagery
   raster through the boat's proxy (made absolute against the page origin),
   with the same fade, and the burned-in credit adds "Esri, Maxar, Earthstar
   Geographics". This works because the image is drawn in the operator's
   browser, which can reach the boat. The overview map in the image stays
   chart-only: it sits at zooms where the fade is zero.
4. **A missing imagery tile fails the image.** The proxy normally covers a
   missing tile with a coarser one or a blank, which suits the live maps. The
   image asks with `?exact=1`, and the proxy then answers 502 (uncached) instead
   of a stand-in. MapLibre raises its error, the existing map error path rejects
   the render and the operator gets an error toast. Imagery goes under the
   style's first label layer so names stay on top, and the track keeps its
   white casing above it. The Esri credit is burned in only when the fitted
   zoom is 9 or more, so it appears exactly when photographs do.
5. **The blog embed and "Download embed as HTML" stay chart-only.** A reader's
   browser cannot reach the boat's proxy, so the choice was between leaving
   imagery out and putting a credential in the pasted card. No menu labels
   changed.

## Considered options

- **An ArcGIS API key for the embed.** Would let the card load Esri tiles
  directly. Rejected as too high a bar: the operator would have to open an Esri
  account, keep the key current, and publish it in every card, where anyone
  reading the page source can use it against the operator's quota.
- **Embed the imagery in the card from the boat.** Exposes the boat, which
  ADR 0174 rules out.
- **Share the anchor map's remembered choice.** Rejected: the anchor map's
  default is off and the passage map's is on, and wanting one does not mean
  wanting the other.

## Consequences

Imagery needs the boat to be online, like the other tiles. Video export is a separate job and
will need the same decision for its frames.
