# ADR 0016: Cached, Deeper-Zoom Esri World Imagery + In-App Area Prefetch

## Status
Accepted

## Context
TimeZero Pro's "PhotoFusion" feature blends satellite photo imagery into the
chart at close zoom, so lagoon entrances and reef heads — often wrong or
missing on official charts, especially in remote Pacific areas — become
visible. The operator wanted this without reaching for a separate desktop
tool (Sat Planner, headless QGIS): fast, fine-grained, and built into the
existing route planner.

Checked directly against the code (not assumed) before building anything:
most of the PhotoFusion mechanic already existed. `route-planner-map.tsx`
already renders a hybrid satellite layer that fades in between a zoom
handoff band (`computeWorldImageryOpacity`, `anchor-watch-map.tsx`), with
base-style land/water fills hidden and labels re-themed for legibility over
photographic imagery. The imagery itself is Esri World Imagery, proxied
through `backend/tile_proxy.go`, capped at zoom 18 both server- and
client-side. Only a shallow 30-minute browser `Cache-Control` header
existed — nothing was cached on the backend, so every tile was re-fetched
from Esri on every view, from every device.

`backend/sat_charts.go` (ADR 0011) already covers "bring your own
higher-res MBTiles package" for cases where even deeper Esri coverage isn't
enough for a specific reef — that's a different, complementary need from
what's built here, and is unaffected by this change.

### Esri vs. Google, again
ADR 0011 already surfaced the fact that Esri's (and Google's/Bing's) terms
of service restrict "systematically requesting tiles for offline use," and
concluded that a bulk-caching feature against a live provider would be a
real licensing risk — which is why that ADR routed around it entirely
(upload-your-own MBTiles instead). This feature reopens that question
narrowly: rather than bulk-caching a whole region ahead of time with no
user in the loop, this is a *demand-driven* cache (only tiles a real
viewport actually requested get stored) plus an explicit, user-initiated,
bounded "cache this area" action (capped at 8000 tiles — one lagoon
entrance at fine zoom, not a whole cruising ground). Google's Maps
Platform Tile API is the one with the harder, explicitly-stated
caching prohibition in its ToS; Esri's World Imagery service (used here via
the same public REST tile endpoint already proxied by this codebase, with
no API key or paid plan) has reasonable-use expectations rather than a
blanket "no offline caching, ever" clause. Confirmed with the operator:
keep Esri, not Google, for this — it's already free, already working, and
zero setup, and this feature's bounded/on-demand shape keeps it well
within reasonable single-vessel use (more on this in Consequences).

## Decision

### Cache-through proxy, not blind pass-through
`backend/tile_cache.go` adds a SQLite-backed tile cache (the same
`modernc.org/sqlite` dependency `sat_charts.go` already uses), one table:
`tiles(source, z, x, y, content_type, data, fetched_at)`, primary-keyed on
`(source, z, x, y)`. `source` is a free-text key (`"esri-world-imagery"`
today) so the same table can serve other imagery sources later with no
schema change. The DB path follows the established `cacheFilePath`
convention (`TILE_CACHE_PATH`, default `data/tile-cache.sqlite`).

`proxyWorldImageryTileHandler` is now a handler *factory*
(`func(cache *tileCache, fetcher tileFetcher) echo.HandlerFunc`) rather
than a plain `echo.HandlerFunc`, specifically so tests can inject a
temp-dir-backed cache and a fake upstream instead of a real cache file or
a real Esri network call. `*http.Client` (specifically `http.DefaultClient`
at registration) satisfies the injected `tileFetcher` interface directly,
no adapter needed.

There is deliberately no TTL/expiry: satellite basemap imagery doesn't
change on human timescales, and `sat_charts.go`'s uploaded packages already
work the same way (cached forever, cleared only by explicit deletion). A
`DELETE /api/world-imagery/cache` escape hatch clears the whole cache if a
stale or blank result ever needs to go (e.g. once Esri adds coverage
somewhere that previously degraded).

### Deeper zoom with graceful degradation, not a blank tile
`worldImageryMaxZoom` moves from 18 to 20 — Esri's World Imagery layer
resolves well past 18 in many coastal/populated areas via Maxar Vivid
updates, and areas that don't have that depth need a real fallback instead
of being capped from ever asking. Previously, any non-200 upstream
response (deeper zoom not available at that location) rendered a blank
transparent tile. Now the proxy retries at the parent tile
`(z-1, x>>1, y>>1)`, then its parent, up to 4 levels coarser, before
falling back to blank — standard slippy-map degradation, and a real
improvement for a lagoon entrance sitting right at the edge of Esri's
high-res coverage: a coarser but real image beats nothing.

Whatever ends up served — success at the original zoom, a degraded
coarser tile, or the final blank fallback — is cached under the
*originally requested* key too, in addition to wherever it was cached at
its native level. A repeat request for a tile past Esri's coverage
depth hits the cache immediately rather than re-walking the 4-level
fallback chain on every view.

Cache read/write failures (an infrastructure problem, not an
upstream-imagery-availability question) are surfaced as real errors up
through the handler rather than folded into the same blank-tile path —
only the upstream-availability path degrades gracefully; a broken cache
fails loudly, per this repo's fail-fast policy.

One thing this surfaced during implementation: the prefetch worker pool
(next section) hits the same `*sql.DB` from several goroutines at once,
and `modernc.org/sqlite`'s default behavior under concurrent access is to
return `SQLITE_BUSY` ("database is locked") rather than wait — caught
directly by the job-completion test before it was fixed. `SetMaxOpenConns(1)`
on the cache's `*sql.DB` makes `database/sql` itself queue concurrent
callers through the one connection, instead of letting SQLite reject them
— the same "simplest correct choice at this scale" reasoning ADR 0011
already applied to its own SQLite usage.

### "Cache this area" prefetch
`POST /api/world-imagery/prefetch` takes a bbox
(`{west, south, east, north}`) and a zoom range (`{minZoom, maxZoom}`).
The tile count across that range is computed with plain slippy-map
arithmetic (no per-tile loop) so an absurdly large request can be rejected
cheaply. If the count exceeds 8000 tiles, the endpoint responds `400` with
the computed count, so the frontend can warn the user immediately rather
than starting something that would hammer the boat's uplink. Otherwise it
kicks off a background job — a bounded pool of 6 concurrent workers, each
running tiles through the exact same cache-through/degradation logic the
live proxy uses — and responds `202` with `{jobId, totalTiles}`.

`GET /api/world-imagery/prefetch/:jobId` returns `{total, done, complete}`,
backed by an in-memory `map[string]*prefetchJob` guarded by a
`sync.RWMutex` — the same pattern `routes.go`/`dashboard_pages.go` already
use for their own state, except intentionally *not* persisted to disk: a
prefetch job's progress has no reason to survive a server restart.
Completed tiles land in the same cache the live proxy reads, so they're
servable immediately, mid-job — there's no "wait for the whole job" gate
before any of it becomes useful.

### Frontend
`WORLD_IMAGERY_MAX_ZOOM` in `route-planner-map.tsx` moves to 20 to match
the backend cap. A new `hooks/use-imagery-prefetch.ts` mirrors
`use-sat-charts.ts`'s shape: `startPrefetch(bounds, minZoom, maxZoom)`
POSTs the request, then polls the status endpoint every second until
`complete`, exposing `{ prefetching, progress, error }`. A new button in
the map's existing control cluster (next to Zoom/Satellite/Fit-bounds)
reads the map's real current bounds and zoom (`mapRef.current?.getBounds()`
/ the tracked zoom state), is disabled until hybrid satellite mode is on,
and drives the hook. A small pill near the button cluster shows
`Caching… {done}/{total}` while a job is running, then `Cached` briefly
before self-dismissing.

## Consequences

Positive:
- Repeat views of the same cruising ground are now instant (backend cache
  hit) instead of re-fetching from Esri on every load, on every device.
- Zoom 18→20 plus graceful degradation means a lagoon entrance at the edge
  of Esri's high-res coverage now shows *something real* — degraded but
  legible imagery — instead of a blank tile.
- The prefetch feature lets the operator deliberately warm the cache for a
  specific entrance before losing signal approaching it, entirely inside
  Helmcentral.
- The `SQLITE_BUSY` concurrency bug the prefetch worker pool would have
  hit in production was caught by its own test before shipping, per this
  repo's test-first policy.

Negative / explicitly deferred:
- Esri's free World Imagery service has its own reasonable-use
  expectations, not a paid SLA. This feature's shape (demand-driven cache
  plus a capped, explicit, user-initiated prefetch — not an unbounded
  background bulk-caching job) is designed to stay well inside that for a
  single vessel or small fleet. A heavier commercial deployment serving
  many simultaneous users would outgrow this and need a paid Esri (or
  Mapbox) plan instead.
- No TTL means a blank/degraded result cached today never automatically
  retries even if Esri later adds coverage there — by design (repeat
  requests shouldn't re-walk the fallback chain forever), but it does mean
  `DELETE /api/world-imagery/cache` is a manual, whole-cache-clearing
  operation with no way to invalidate a single stale entry short of that.
- The prefetch job tracker is in-memory only; a server restart mid-job
  loses that job's progress (the cached tiles it already wrote survive
  fine — only the progress-tracking record is lost).
- No antimeridian-crossing bbox handling in the prefetch tile-count/tile-
  list math — out of scope for this pass.

## Related
- ADR 0011: In-App MBTiles Satellite Chart Upload-and-Serve — the
  complementary "bring your own higher-res package" path for reefs where
  even deeper Esri coverage still isn't enough, and the ADR whose Esri/
  Google ToS research this decision builds directly on.
