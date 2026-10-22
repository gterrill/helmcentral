# ADR 0201: The Basemap Follows Carto's Protomaps Styles

## Status

Accepted. Amends [ADR 0066](0066-basemap-place-name-labels.md) and
[ADR 0067](0067-carto-basemap-proxy-and-offline-cache.md).

## Context

On 2026-10-23 Carto replaced the documents at
`https://basemaps.cartocdn.com/gl/{positron,dark-matter}-gl-style/style.json`.
They are now named "Positron (Protomaps)" and "Dark Matter (Protomaps)" and
differ from the ones ADR 0067 was written against:

- One vector source, `protomaps`, with an inline `tiles` array
  (`.../v/20261001-03/vectortiles/carto.streets/v2/{z}/{x}/{y}.mvt`, maxzoom
  15) and no `url`, so there is no TileJSON document.
- Glyphs and sprite live under the same release stamp
  (`.../v/20261001-03/fonts/...`, `.../v/20261001-03/gl/positron-gl-style/sprite`).
- The tiles use the Protomaps basemap schema, not OpenMapTiles: layers
  `earth`, `places`, `pois`, `water`, `roads`, `landuse`, `buildings`,
  `boundaries`, with `kind` / `kind_detail` in place of `class`.
- The first of 95 layers is still `background`.

`rewriteCartoStyle` required `sources.carto.url` and failed with "upstream
style has no sources.carto object". The dev and boat backends kept working only
because `basemap_assets` holds the old rewritten style forever. A new install,
or any install that cleared its map cache, got a blank chart.

## Decision

1. **Read the upstream style, do not hardcode its URLs.** The proxy finds the
   style's single vector source (an error if there are none or several) and
   takes its tile template, the glyphs template and the sprite base URL from
   it. The raw upstream style is what `basemap_assets` caches, under
   `/api/basemap/v2/upstream/<theme>`; the tile, glyph and sprite fetches read
   their templates from that record. Carto moves the release stamp when it
   republishes, so a constant would rot; the cached style is the record of
   which release this install is on.
2. **Allowlist what the style may point at.** The style is data we do not
   control. Every URL taken from it must be `https`, have no credentials or
   port, and be on `basemaps.cartocdn.com` or `tiles.basemaps.cartocdn.com`,
   checked before the style is cached and before anything is fetched from it.
   The style URLs themselves, the sprite file names (two themes by four
   suffixes) and the zoom range stay closed sets as before; glyph font stacks
   and tile coordinates are escaped or integers, as before.
3. **Serve a rewritten copy per request.** The style handler rewrites the
   source to an inline `tiles: ["<origin>/api/basemap/tiles/{z}/{x}/{y}"]`
   and the glyphs and sprite to same-origin paths, absolute under the origin
   the request arrived on (MapLibre needs absolute URLs; the boat is reached
   on several hostnames). The source id is left as `protomaps`.
4. **Remove `/api/basemap/tilejson`.** Nothing in the frontend requested it:
   the style no longer references it and `lib/basemap.ts` only names the two
   style URLs.
5. **New cache namespace, no fallback.** Tiles are stored under source
   `carto-basemap-v2`, assets under `/api/basemap/v2/...`. Opening the cache
   deletes the `carto-basemap` tiles and every asset outside that prefix, so
   an upgraded boat refetches by itself and nothing serves OpenMapTiles-shaped
   bytes to the new style. The "clear map cache" action still clears both
   tables.
6. **Labels follow the schema.** `map-place-labels.tsx` attaches to
   `protomaps` and reads: bays from `water` Points of kind `bay`; straits from
   `water` LineStrings of kind `strait` (a further layer, placed along the line);
   islands from `earth` Points of kind `island`, and smaller islands and rocks
   from `pois` of kind `coastline` (around the Whitsundays and Magnetic Island
   `earth` carries only kind `island`, and 39 of 63 named coastline pois had no
   `earth` counterpart, e.g. Daydream Island and Ross Islet); marinas and peaks from `pois`
   (kind `marina`, `peak`/`volcano`); the town top-up from `places`
   (`locality` city/town/village/hamlet, `neighbourhood` suburb). Filters were
   checked by running them over decoded real tiles. Font stacks are the ones
   the new styles reference.

## Rejected

- **Vendor the old v1 style and its tiles.** It would keep the chart working
  with no code change to the labels, but Carto's v1 tiles will be retired, and
  it would leave this install pinned to an endpoint that is going away. It
  also contradicts the fallback policy: a second, silently older chart source
  masking the real one.
- **Hardcode the v2 tile, glyph and sprite URLs.** Works today, breaks on the
  next release stamp. The proxy would 404 every tile with nothing to say why.
- **Keep a TileJSON endpoint for compatibility.** There is no consumer.

## Consequences

- A fresh install makes one extra upstream request (the style) on its first
  tile miss if no style was requested first. With no uplink and an empty
  cache, tiles fail as 404s (MapLibre draws nothing) and the style as a 502
  that names the reason.
- Carto moves its release stamp and retires old ones, so a tile, glyph or
  sprite request that comes back 404 or 410 may mean the stamp moved. The
  proxy then refetches that theme's upstream style, through the same
  allowlist validation. Misses share one refetch (single-flight). After a
  refetch that completed, the theme is not refetched for five minutes; after
  one that failed, for 30 seconds, so a bad moment cannot block recovery. If
  the refetched style names different templates, it replaces the cached one
  and the templates held in memory, the change is logged once, the cached
  sprite and glyph files of the old stamp are deleted so the new style never
  pairs with them, and the original request is retried once with the new
  templates. Cached vector tiles are kept: the schema is the same v2 and they
  are the offline chart. The templates themselves are held in memory per
  theme, loaded from the cached style on first use.
- A refetch that fails or is rejected by the allowlist is logged with the
  theme and reason, is not cached, and the request fails with an error that
  carries the reason (502). Unchanged templates mean the 404 was genuine and
  stands. Nothing stands in for the asset.
- Tile status codes: only an upstream 404 or 410 for the tile (after that
  recovery), or the upstream being unreachable (offline with the tile not
  cached, circuit breaker open), is a 404, which MapLibre treats as "no data
  here". A missing, unusable or unrefreshable upstream style, or any other
  upstream status, is a 502; a failure of the local cache database is a 500.
- The served style embeds the origin the request arrived on, so it is sent
  `Cache-Control: private, max-age=300`. `X-Forwarded-Host` is reduced to its
  first value and used only if it is a plain host[:port]; `X-Forwarded-Proto`
  only if it is http or https. Otherwise the connection's own host and scheme
  are used.
- The Protomaps data names fewer water features than the old tiles did around
  the Whitsundays: Cid Harbour, Nara Inlet and Hayman Channel are not features
  in them, so they no longer get a label. Bays and straits elsewhere do.
- Tiles reach z15, one level deeper than before.
- The passage export still loads Carto's Voyager style directly from the
  CDN (`lib/passage-export-style.ts`); this ADR does not touch it.
