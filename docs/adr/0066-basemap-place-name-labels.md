# ADR 0066: Place-Name Labels on the Basemap

## Status
Accepted

## Context

Sitting in the Whitsundays, the routes chart and the anchor-watch chart show no names for the
features a skipper actually navigates by: no Cid Harbour, no Nara Inlet, no Hook Island, no
Coral Sea Marina. Both maps read as plainer than the water actually is.

The cause is not missing data. Both maps load the Carto Positron / Dark Matter basemap
(`https://basemaps.cartocdn.com/gl/positron-gl-style/style.json` and the dark-matter equivalent),
whose single vector source, `carto`, already carries every one of those names. Carto's *style*
document simply never draws them. Confirmed by decoding live tiles over Cid Harbour, Hook Island,
Airlie Beach, and Gloucester Passage:

| Data already in the tiles | Style layer that would draw it | Result |
| --- | --- | --- |
| `water_name` class `bay` / `strait` (Cid Harbour, Nara Inlet, Macona Inlet, Hayman Channel, The Narrows) | none - `watername_ocean` / `watername_sea` / `watername_lake` filter to `ocean`, `sea`, `lake` only | never drawn |
| `place` class `island` (Whitsunday, Hook, Border, Cid, Dent, Hamilton) | only `place_city_dot_z7`, clamped `minzoom: 7` / `maxzoom: 8` | a bare dot, no text, for one narrow zoom band |
| `poi` class `harbor` (Coral Sea Marina, Port of Airlie Marina, Whitsunday Sailing Club) | none - `poi_stadium` and `poi_park` are the only `poi`-source-layer layers in either style | never drawn |
| `mountain_peak` (Whitsunday Peak, Hook Peak, Bolton Hill) | none - zero layers in either style reference this source-layer | never drawn |
| `place` class `town` (Airlie Beach, Cannonvale) | `place_town`, but `maxzoom: 14` | vanishes exactly where a skipper zooms in to read a chart closely |

So this is a styling gap, not a data or licensing problem. Fixing it is a handful of extra symbol
layers attached to a vector source both maps already download: no new tile server, no new
dependency, no extra network request, no attribution change, no backend change.

## Decision

### Style layers on the existing source, not a second tile source

A new component, `frontend/src/components/map-place-labels.tsx`, exports `MapPlaceLabels`, a
fragment of five bare `<Layer>` elements - deliberately with no `<Source>` wrapper, because the
`carto` source already exists in the style both maps load by the time these layers try to mount.
`@vis.gl/react-maplibre`'s `createLayer` guards on `map.getSource(props.source)` before calling
`addLayer`, and the `Layer` component re-creates itself on every `styledata` event, so the layers
survive the light/dark theme's full `setStyle` swap without any extra plumbing on our side.

We considered proxying a second vector tile source (e.g. through `backend/tile_cache.go`, whose
`source` column is free-text and needs no schema change) carrying a curated place-name layer. We
rejected it for this pass: every name we need is already in tiles the map fetches regardless, so a
second source would mean a second live dependency and a second set of glyph/sprite concerns to
pay for information already sitting on the wire.

Five layers, each `type: symbol`, attached to the existing source-layers:

| id | source-layer | filter | minzoom |
| --- | --- | --- | --- |
| `place-names-water` | `water_name` | `class` in `bay`, `strait`; Point geometry; has `name` | 9 |
| `place-names-island` | `place` | `class == 'island'`; has `name` | 9 |
| `place-names-harbor` | `poi` | `class == 'harbor'`; has `name` | 14 |
| `place-names-peak` | `mountain_peak` | `class` in `peak`, `volcano`; has `name` | 12 |
| `place-names-town-topup` | `place` | `class` in `city`, `town`, `village`, `hamlet`, `suburb`; has `name` | 14 |

`place-names-harbor` starts at zoom 14 because OpenMapTiles only generates `poi` features from
that zoom up - a lower `minzoom` would just filter nothing in below it. `place-names-town-topup`
fills the gap left by Carto's own `maxzoom` cutoffs (`place_town` 14, `place_city_r5`/`r6` 15,
`place_villages`/`place_hamlet`/`place_suburbs` 16). In the zoom band where Carto's own layer is
still drawing, MapLibre's cross-layer symbol placement runs in style order and drops the later,
higher layer on collision - identical text at an identical anchor point - so this top-up layer
never double-draws a name; it only starts showing once Carto's own layer has already stopped.

Text field is `['coalesce', ['get', 'name'], ['get', 'name_en']]` everywhere: the tile's local name
where the source has one, the English rendering otherwise. Font stacks are copied verbatim from the
Carto style documents' own place/water label layers, so these layers fetch glyphs the map is
already fetching for the basemap's own (currently unused, at these source-layers) label layers,
rather than triggering a new glyph request for a stack the style has never referenced.

Paint is memoised per `[isDarkTheme, overImagery]` and lifted from the corresponding Carto style
layers (`place_town` / `place_city_r5` for land, `watername_lake` / `watername_ocean` for water, in
each of `positron-gl-style.json` and `dark-matter-gl-style.json`), so these labels read as native
basemap type rather than an overlay bolted on top of it. When a satellite raster is showing
through (`overImagery`), paint switches to white text on a heavy black halo, matching the
`HYBRID_LABEL_*` treatment `route-planner-map.tsx` already applies to the base style's own labels
in that mode - the same convention real hybrid map styles use, applied regardless of the app's own
light/dark theme since satellite brightness has nothing to do with that setting.

### Layer ordering: text under geometry, above rasters

Neither `<MapPlaceLabels>` mount sets a `beforeId` on any of its layers. Both maps mount it
unconditionally, ahead of every conditional overlay, which is what makes the ordering work without
explicit z-index bookkeeping on the label layers themselves:

- In `route-planner-map.tsx`, it mounts immediately after `raster-overlay-anchor`, an invisible,
  sourceless background layer that exists purely to give every later raster something stable to
  pin `beforeId` against. The OpenSeaMap seamark overlay, world imagery, and any uploaded sat chart
  all pin `beforeId="raster-overlay-anchor"`, so the labels land above every one of them. The route
  line and the GSHHG coastline fallback (ADR 0009) mount later still, with no `beforeId` of their
  own, so they draw on top of the labels in turn. Text under safety-critical geometry, above raster
  imagery, in that order.
- In `anchor-watch-map.tsx`, it mounts immediately after the `alarm-circle` `Source` block. This map
  pins its rasters `beforeId="alarm-circle-fill"` - above the *whole* base style, not just above a
  background anchor - so before this change, satellite mode buried Carto's own (already mostly
  undrawn) labels under the raster entirely. Mounting the label layers right after `alarm-circle`
  is what makes names visible over imagery on this map for the first time; the `overImagery`
  white/black-halo paint here is load-bearing, not cosmetic.

### Fail-fast guard

Per the repo's fallback policy, a silent failure to render is not acceptable just because it is
visually harmless. If `map.getSource('carto')` is ever absent after a style load - a future Carto
style change renaming the source, or a differently-shaped style document - `createLayer` silently
skips every `addLayer` call above, and the map looks fine while simply missing every place name,
with nothing else in the app saying why.

Both maps call `warnIfBaseVectorSourceMissing` from their existing style-load path
(`onLoad`/`onStyleData` in `route-planner-map.tsx`, a new `onStyleData` handler in
`anchor-watch-map.tsx`, which had no style-load hook before this change), guarded by a ref so it
checks once per real style load rather than once per `styledata` event - that event fires many
times per load as tiles and sources arrive, not just once at the top. A theme switch resets the
guard, since it is a genuine `setStyle` reload with its own new style document to check. On a miss,
it logs `console.error('[map-place-labels] base vector source "carto" missing from the loaded
style - place-name labels will not render')`, in the style of the existing
`console.info('[gshhg-coastline-fallback] ...')` at `route-planner-map.tsx`.

## Consequences

- Bays, inlets, channels, islands, marina POIs, peaks, and town names are labelled on both charts,
  in light, dark, and satellite modes, with no manual toggle - the same "always on" treatment the
  OpenSeaMap seamark overlay already gets.
- No offline improvement, and none was in scope. Carto vector tiles are fetched live and are not
  proxied or cached by the Go backend, unlike Esri imagery (ADR 0016). Place names inherit exactly
  the connectivity the basemap already needed - no new failure mode, but no new resilience either.
  Proxying the vector source through `backend/tile_cache.go` remains a separate piece of work if
  offline coverage of these labels is ever wanted.
- No new tile server, dependency, network request, or attribution change. The five layers are pure
  style configuration on a source both maps already load.
- `place-names-town-topup`'s reliance on MapLibre's own collision suppression to avoid double
  labels in the 14-16 zoom band is a property of the renderer, not something this change enforces
  directly - if a future MapLibre upgrade changes cross-layer collision behavior, that band is
  where a regression would show up first.
- This supersedes the note in `docs/adr/0009-gshhg-coastline-fallback.md` that the route planner
  map has no land/coastline reference beyond whatever the underlying Carto basemap style happens to
  render: the basemap now renders substantially more of what it was already carrying.

## Related
- ADR 0006 (Manual Route Planning): the map-chrome duplication between `route-planner-map.tsx` and
  `anchor-watch-map.tsx` that this change's shared `MapPlaceLabels` component deliberately does not
  extend to the rest of either map's chrome - it is pure declarative layer config with no
  interaction or chrome of its own, so it does not run into the duplication tradeoff that ADR
  accepted.
- ADR 0009 (GSHHG Coastline Fallback): the "no land reference beyond whatever Carto happens to
  render" limitation this change addresses for place names specifically, on the same map.
- ADR 0056 (Place Names From OSM Features): a different mechanism for a different problem - that
  ADR resolves a single name for the vessel's or anchor's current position via a live Overpass
  query; this one draws every name already sitting in the basemap's own vector tiles across the
  whole visible chart, with no live lookup involved.

## Verification
`cd frontend && npm test` and `npm run lint` both pass.
