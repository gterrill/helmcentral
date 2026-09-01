# ADR 0067: Carto Basemap Proxy and Offline Cache

## Status
Accepted

## Context

Both charts, routes and anchor-watch, load the Carto Positron / Dark Matter basemap straight
from `basemaps.cartocdn.com`. Nothing about it passes through the Go backend, so nothing about
it survives losing the uplink. Anchored somewhere with no phone coverage, which is most of the
interesting anchorages, the chart is blank.

That gap sat awkwardly next to the rest of the imagery stack. ADR 0016 already built a
cache-through proxy and a SQLite tile store for Esri World Imagery, with a user-initiated
"cache this area" prefetch on top. Satellite imagery was therefore available offline while the
basemap underneath it, the thing that draws the coastline, was not. ADR 0066 then added
place-name labels drawn from Carto's vector tiles, which made the basemap carry more of the
navigational load than before, and made its absence offline more costly.

### Proxying only the vector tiles would have achieved nothing

The obvious framing was "cache the `.mvt` tiles the way we cache Esri's `.jpeg` tiles". That
does not work, for a reason worth writing down because it is not obvious until you trace the
load order.

MapLibre fetches `style.json` first. The style is what names the sources, so until it loads
the map does not know a tile endpoint exists and never requests one. With the style still
coming from the CDN, an offline start fails at step one and no cached tile is ever asked for.
A vector-tile-only cache would have been dead code on the boat and only ever visible as a
bandwidth saving when the uplink was already working.

Serving the basemap offline therefore means serving all five asset classes the style
transitively pulls in:

| Asset | Upstream | Measured size |
| --- | --- | --- |
| `style.json` (x2, light and dark) | `basemaps.cartocdn.com/gl/{positron,dark-matter}-gl-style/style.json` | 7.9 KB each |
| TileJSON | `tiles.basemaps.cartocdn.com/vector/carto.streets/v1/tiles.json` | 30 KB |
| Vector tiles | `tiles-{a..d}.basemaps.cartocdn.com/vectortiles/carto.streets/v1/{z}/{x}/{y}.mvt` | 2 to 51 KB each |
| Glyphs | `tiles.basemaps.cartocdn.com/fonts/{fontstack}/{range}.pbf` | ~48 KB per range, 4 stacks in the style |
| Sprite | `tiles.basemaps.cartocdn.com/gl/{style}/sprite{,@2x}.{json,png}` | under 1 KB total |

Everything except the vector tiles comes to well under a megabyte, one time. The Whitsundays
at z0 to z14 is 1,829 vector tiles. The full set is small enough that the "cache all of it"
answer is clearly correct, and the "cache only part of it" answer is what needed justifying.

## Decision

Serve the entire Carto basemap from the backend, same-origin, cached in SQLite. Five
endpoints under `/api/basemap`, registered at `tierRead` alongside the existing world-imagery
routes:

    GET /api/basemap/style/:name              positron | dark-matter
    GET /api/basemap/tilejson
    GET /api/basemap/tiles/:z/:x/:y
    GET /api/basemap/fonts/:fontstack/:range
    GET /api/basemap/sprite/:name

The frontend's `STYLE_LIGHT` / `STYLE_DARK` in both `route-planner-map.tsx` and
`anchor-watch-map.tsx` point at the first of these. That is the only frontend change the
feature needs.

### Vector tiles do not get the imagery resolver

This is the one place where reusing the existing machinery would have been actively wrong, so
it gets its own resolver, `resolveCartoVectorTile`, rather than `resolveWorldImageryTile`.

`resolveWorldImageryTile` implements ADR 0016's graceful degradation: when an upstream tile is
unavailable it walks to progressively coarser zooms and, on success, caches those bytes under
the originally requested key so a repeat request does not re-walk the chain. For raster
imagery that is exactly right. The parent tile covers the same ground at lower resolution, so
a blurry tile beats a blank one.

For vector tiles it corrupts the map. MVT geometry is expressed in tile-local coordinates, not
world coordinates, so a z12 tile's bytes interpreted as a z14 tile do not render as a coarser
version of the same place. They render the wrong quarter of the parent's contents stretched
across the child's extent: coastlines in open water, islands on land, labels nowhere near what
they name. Worse, ADR 0016's design deliberately writes that result back into the cache under
the requested key, so a single degradation would persist a permanently wrong tile with no TTL
to expire it.

`resolveCartoVectorTile` therefore has no fallback chain at all:

- cache hit, serve it
- cache miss, fetch upstream, cache, serve
- upstream failure with nothing cached, return an error the handler turns into **404**

404 rather than 502 because a missing vector tile is not an outage. MapLibre already treats one
as "no data here" and draws nothing, which is the correct rendering for genuinely absent data.
The style and TileJSON documents take the opposite treatment: there, an upstream failure with
an empty cache is a real outage and returns 502 with the reason, never a fabricated empty
document that would leave both charts silently blank with no way to tell why. That split
follows the repo's fallback policy in both directions.

Carto's tiles stop at z14. `basemapVectorTileHandler` rejects `z > 14` outright rather than
clamping, because clamping here would mean serving a shallower tile's bytes under a deeper
request key, which is the same coordinate mismatch moved from the cache into the handler. A
compliant client never sends z > 14 anyway, because the TileJSON's `maxzoom` tells MapLibre to
overzoom locally instead.

Both halves of this are pinned by tests, including an explicit regression guard asserting that
a failed deep fetch with a perfectly good parent tile sitting in the cache still 404s and still
caches nothing.

### Storage split

Vector tiles go in the existing `tiles` table under a new `source` value, `carto-basemap`. That
column was left free-text in ADR 0016 for exactly this, so there is no schema change and no
migration. One source value, not two: the vector tiles are theme-independent, since both styles
read the same `carto.streets` source and differ only in how they paint it.

The other four asset classes are not `(z, x, y)` addressable, so they get a second table:

    CREATE TABLE IF NOT EXISTS basemap_assets (
        path TEXT PRIMARY KEY,
        content_type TEXT NOT NULL,
        data BLOB NOT NULL,
        fetched_at INTEGER NOT NULL
    )

keyed on the proxy's own request path. No TTL, matching the tiles table and for the same
reason.

### Rewrite the URLs, preserve everything else

The style handler decodes the upstream JSON, rewrites exactly three fields, and re-encodes:

- `sources.carto.url` to `/api/basemap/tilejson`
- `glyphs` to `/api/basemap/fonts/{fontstack}/{range}.pbf`
- `sprite` to `/api/basemap/sprite/{positron,dark-matter}`

The TileJSON handler likewise rewrites only its `tiles` array, leaving `minzoom`, `maxzoom`,
`attribution`, `bounds` and `vector_layers` untouched.

Everything else in the style document is load-bearing frontend state and must survive byte for
byte in meaning:

- **The source id `carto`.** `map-place-labels.tsx` pins `BASE_VECTOR_SOURCE_ID = 'carto'` and
  attaches all five of ADR 0066's place-name layers to a source with that exact id.
- **Every layer id.** `route-planner-map.tsx` names Carto's own layers in
  `HYBRID_HIDDEN_LAYER_IDS` and `HYBRID_LABEL_LAYER_IDS` for the hybrid-satellite restyling,
  and `BASE_STYLE_FIRST_LAYER_ID = 'background'` depends on `background` staying the first
  layer in the document.

Renaming either would break the charts silently, which is why the rewrite is a targeted
three-field edit rather than a regeneration, and why a test asserts the source id, a sample of
pinned layer ids, and `background`'s position all survive.

A missing or wrong-shaped field at any rewrite point is an error, not a best-effort partial
rewrite. Serving a style with a broken glyphs URL would produce a map with no text and no
obvious cause.

The **rewritten** bytes are what gets cached, not the upstream ones. Caching the upstream
document would mean an offline cold start serving a style that sends the browser back to the
CDN it cannot reach, which is the whole failure this ADR exists to remove.

### Relative in the cache, absolute on the wire

The rewritten URLs cannot simply be stored in their final form, because the
cache and MapLibre want opposite things.

MapLibre needs them absolute, for two different reasons:

- `sprite` is validated up front, and a relative value is rejected outright
  ("Invalid sprite URL ..., must be absolute"). That aborts the entire style
  load, so no source is ever registered and not one tile is requested.
- `glyphs` and the vector tile template are consumed inside a Web Worker,
  which has no document to resolve a relative path against. A relative value
  there fails at request construction ("Failed to construct 'Request': Failed
  to parse URL from /api/basemap/tiles/12/3742/2283") and every tile fails
  silently, leaving a basemap that draws nothing.

Both were found the same way, by loading the chart with every request to
`cartocdn.com` blocked at the browser and reading what MapLibre actually
complained about. Neither is visible from the served JSON on its own, and
the second one in particular presents as "the map is blank" with a
perfectly healthy set of 200s in the network tab.

The cache needs them relative. An absolute URL bakes in one hostname, and
this dashboard is reached on several: `localhost` in dev, the boat's LAN
IP, Tailscale. A cached absolute URL would be wrong on every host except
the one that happened to populate the cache.

So the cache stores the relative form, and the style and TileJSON handlers
absolutise at serve time against the request's own origin, taken from
`X-Forwarded-Proto` / `X-Forwarded-Host` when present and the request Host
otherwise. One cache entry stays correct on every hostname, and the only
host-dependent part of the document is resolved by the one component that
knows the answer.

This is also why the Vite dev proxy sets `changeOrigin: false`. In
production the backend serves the embedded frontend itself, so the request
Host is by construction the origin the browser used. `changeOrigin: true`
would rewrite it to the backend's own address and make dev the only
environment that disagrees, handing the browser URLs on a port it may not
be able to reach.

### SSRF guards

`:name` on both the style and sprite endpoints is looked up in a closed allowlist rather than
interpolated into an upstream URL template. Without that, either endpoint would let any caller
make the server fetch and cache an arbitrary path under the Carto host. The glyphs endpoint
cannot use a closed allowlist, since the fontstack is open-ended, so it relies on
`url.PathEscape` guaranteeing neither the fontstack nor the range can smuggle a `/` into the
upstream path. All three are covered by tests asserting the rejected values never reach the
fetcher at all.

### Prefetch

"Cache this area" now pulls basemap vector tiles alongside Esri imagery for the same bbox, so
one deliberate action makes an area genuinely usable offline rather than half-usable. Basemap
tiles are capped at z14; imagery keeps its existing range. `tileCoord` gained a `kind`
discriminator so the worker pool routes each tile to the right resolver, and an unrecognised
kind is treated as a bug rather than quietly falling through to either one.

The 8000-tile cap now applies to the combined total. The Whitsundays at z0 to z14 contributes
1,829 basemap tiles, so a typical area stays comfortably inside it. `Total` counts both sets,
so the existing progress pill stays accurate with no frontend change.

### Cache-Control

Tiles, glyphs and sprites get a one-year immutable max-age, matching what upstream itself
sends. Style and TileJSON get 300 seconds, because they are the one part of this that can
plausibly change server-side, and they are a few KB. The server-side cache never expires either
way, which is what makes offline work; these headers only govern the browser's own copy.

### Terms of service

This caches a third-party basemap, so it deserves the same explicit treatment ADR 0016 gave
Esri rather than being waved through.

CARTO offers these basemap styles publicly with no API key and no paid plan, and asks for
attribution. What is being cached here is bounded and single-vessel: demand-driven caching of
tiles a real viewport actually requested, plus the same explicit, user-initiated, capped
"cache this area" action ADR 0016 already established, on one boat. This is not bulk
redistribution and the cache is not served to anyone else. It sits in the same reasonable-use
territory ADR 0016 concluded Esri did, and deliberately not in the territory ADR 0016 ruled out
for Google, whose ToS carries an explicit caching prohibition.

Attribution is displayed. Both maps previously set `attributionControl={false}`, so the credit
was present in the served TileJSON but never rendered. Proxying the tiles ourselves makes
showing it clearly our responsibility rather than the CDN's, so the control is now enabled in
compact form on both maps.

MapLibre's own control is the right mechanism because it aggregates whatever each currently
active source declares, so the credit stays correct as layers come and go: Carto and
OpenStreetMap via the proxied TileJSON, OpenSeaMap always, Esri only while satellite imagery is
switched on, and any uploaded MBTiles chart. Hand-writing a static credit string would drift
the moment a layer was added.

It is pinned to its compact "i" icon rather than the full credit line. `compact: true` alone
does not achieve that: reading MapLibre's `_updateCompact`, the first time the attribution
becomes non-empty the control has neither the compact nor the empty class yet, so it adds
`maplibregl-compact` and `maplibregl-compact-show` together and renders fully expanded. It then
stays expanded until something collapses it, which on a real map is the first drag or pinch and
never happens at all if you only zoom with the on-screen buttons. That is why it looked like an
icon at some times and a long strip of text at others: it depended entirely on whether the map
had been touched yet.

This cannot be fixed in CSS. The auto-expanded state and the state after a deliberate tap on
the icon are identical - `open` and `maplibregl-compact-show` are always both present or both
absent - so no selector can distinguish "MapLibre opened this" from "the operator opened this".
`useCollapsedMapAttribution` collapses it on the map's `onIdle` instead, and stops permanently
the moment the operator clicks the icon themselves, since re-collapsing under someone reading
the credits would be worse than the original inconsistency.

The stock control is also a light pill with dark text, which is right on a plain light basemap
and reads as a foreign element over Dark Matter or over satellite imagery. It is restyled in
`index.css` to the same translucent-dark treatment the "No chart data" pill and the
zoom/satellite buttons already use, identical in both themes, because it sits on map imagery
rather than on an app surface. Those overrides deliberately live outside `@layer components`:
Tailwind tree-shakes that layer against the content scan, and MapLibre builds the control in
JS, so the class names never appear in the source and the rules would be dropped.

### Cache clearing

`DELETE /api/world-imagery/cache` now clears `basemap_assets` as well as `tiles`. A stale
basemap asset has exactly the same shape of problem as a stale tile and wants the same escape
hatch, and a second narrower endpoint would be one more thing to know about for no real gain.
The endpoint keeps its existing name despite the widened scope; renaming it would break the
frontend caller for no functional benefit.

## Consequences

- Both charts work with no uplink, for any area that has been viewed or prefetched. This is the
  point of the change.
- No API key, no paid plan, no new runtime dependency. The upstream endpoints are the same
  public ones the browser was already hitting.
- The backend is now on the critical path for the basemap. If it is down, the charts do not
  draw at all, where previously they would have drawn as long as the internet was up. On this
  deployment the backend and the frontend are served from the same box, so in practice the
  backend being down already means no dashboard.
- First load of a new area is marginally slower, since tiles round-trip through the backend
  rather than coming from CARTO's CDN edge. Repeat views are faster, served from local SQLite.
- The tile cache database will grow. Vector tiles are small, but the prefetch action can now
  add up to 8000 combined tiles per invocation. There is no eviction, by design, matching
  ADR 0016.
- Attribution is rendered on both maps, in compact form, and follows whichever sources are
  actually active.

## Related

- [ADR 0016](0016-cached-deeper-esri-imagery-and-area-prefetch.md) - the cache-through proxy,
  SQLite tile store, prefetch job and Esri-vs-Google ToS reasoning this builds directly on.
- [ADR 0066](0066-basemap-place-name-labels.md) - the place-name layers whose pinned source and
  layer ids constrain what the style rewrite is allowed to touch.
- [ADR 0011](0011-mbtiles-satellite-chart-upload.md) - upload-your-own MBTiles, the earlier
  answer to offline chart data.
- [ADR 0009](0009-gshhg-coastline-fallback.md) - the coastline fallback for when no chart is
  available.

## Verification

- `cd backend && go test -short ./...`
- `cd frontend && npm test`
- With the dev stack up, `curl localhost:8080/api/basemap/style/positron` returns a style whose
  `sources.carto.url`, `glyphs` and `sprite` are all `/api/basemap/...` paths, whose source id
  is still `carto`, and whose first layer is still `background`.
- `curl localhost:8080/api/basemap/style/bogus` returns 400 without any upstream request.
- Load a chart with every request to `*.cartocdn.com` blocked in the browser. The basemap, its
  labels and its glyphs all still render, and the count of blocked CDN requests is zero: nothing
  in the page talks to CARTO directly any more.
- The attribution control reads "OpenSeaMap | CARTO, OpenStreetMap" on the plain basemap, and
  gains "Source: Esri, Maxar, Earthstar Geographics" once satellite imagery is toggled on.
- Load a chart area with the uplink up, then pull the uplink and reload. The basemap, its
  labels and its glyphs all still render.
