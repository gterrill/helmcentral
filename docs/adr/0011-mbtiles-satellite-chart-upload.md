# ADR 0011: In-App MBTiles Satellite Chart Upload-and-Serve

## Status
Accepted

## Context
Cruisers commonly use satellite imagery for reef-spotting in poorly-surveyed waters (Pacific/SE Asia), since it reveals reef/shallow-water boundaries more reliably than official charts there. The established workflow for this is entirely desktop/Windows-based: SASPlanet downloads imagery from a live tile provider, Sat2Chart converts it into a chart file, and OpenCPN imports and displays it. The operator asked for a more modern way to get this into helmcentral's routing chart.

helmcentral already has live Esri World Imagery (`backend/tile_proxy.go`, toggled in `route-planner-map.tsx`), but it's always-online with no offline caching — exactly the gap SASPlanet fills. Research into "just cache it automatically" found a real blocker: Esri's (and Google's/Bing's) terms of service explicitly prohibit "systematically requesting tiles for offline use." This is precisely why SASPlanet exists as a workaround rather than a sanctioned feature, and building automatic bulk caching against any of those providers inside helmcentral would create real licensing exposure — inconsistent with this app's established licensing-conscious posture (ADR 0006, ADR 0010).

## Decision

### Split acquisition from integration
Acquisition (SASPlanet, or any other tool) stays exactly as it is today — the operator's own tool and licensing-risk choice, unchanged and out of scope for helmcentral. What changes is the second half of the workflow: Sat2Chart's conversion step and OpenCPN's import step are replaced by an in-app upload-and-serve feature. This works cleanly because Sat2Chart already exports directly to MBTiles (a standard SQLite-based tile container) — there's no new conversion step to design. helmcentral itself never fetches or bulk-caches tiles from any live provider, so it takes on no new licensing exposure; it only serves back tiles the operator already obtained and uploaded themselves.

### Backend storage, validation, and serving
A new `backend/sat_charts.go` stores uploaded files at `<SAT_CHARTS_DIR>/<uuid>.mbtiles` (default `data/sat-charts`, overridable via `SAT_CHARTS_DIR`, mirroring the existing `cacheFilePath`/`ROUTES_FILE` pattern in `routes.go`). There is no separate JSON catalog — each file's own MBTiles `metadata` table (`name`, `bounds`, `minzoom`, `maxzoom`, `format`) is the source of truth, read fresh per list request. This avoids a second source of truth that could drift from the files on disk, and is fine at this scale (a personal dashboard, a handful of charts).

- `POST /api/sat-charts` streams the upload to a temp file in the storage directory first (`io.Copy`, not buffered in memory), validates it's real MBTiles (checks for the `tiles`/`metadata` tables), then atomically renames it into place. A bad upload never occupies a real catalog slot — rejected and discarded, fail-fast per AGENTS.md.
- `GET /api/sat-charts` lists charts. One narrow, deliberate exception to fail-fast: a corrupt file is skipped and logged rather than failing the whole list, because one bad file shouldn't take down visibility of every other valid chart (the same reasoning ADR 0009 used for its own fallback exception). Upload and tile-serving stay strictly fail-fast.
- `DELETE /api/sat-charts/:id` removes a file (rejecting any `id` containing `/` or `..` before building the path).
- `GET /api/sat-charts/:id/:z/:x/:y` serves one tile. **MBTiles uses TMS row numbering (Y=0 at the bottom); MapLibre/XYZ tile requests use Y=0 at the top.** Converting requires `tmsRow = (1<<z - 1) - xyzRow`, implemented as its own named function (`xyzRowToTMSRow`) specifically so it could get dedicated tests (a hand-computed-values test plus a round-trip property test across many zoom levels) — a sign error here would silently render imagery upside-down, and only at certain zoom bands, making it an easy bug to miss without that isolation. A missing tile returns `404` (real "no data here" information about the operator's own uploaded chart, not a flaky-network case to paper over like `tile_proxy.go`'s graceful pixel fallback). A found tile gets `Cache-Control: public, max-age=604800, immutable` — static once uploaded, same reasoning as ADR 0009's GSHHG endpoint.

`modernc.org/sqlite` (pure-Go) is the SQLite driver, pinned to v1.34.4 specifically — the latest release at the time required bumping the Go toolchain directive to 1.25, which would conflict with the production `Dockerfile`'s `golang:1.22-alpine` build stage; v1.34.4 has no such requirement. `mattn/go-sqlite3` was not an option at all, since the production Dockerfile sets `CGO_ENABLED=0`.

### Fixing a discovered durability gap
The production `docker-compose.yml` mounted only `./settings.yaml:/app/settings.yaml` — `backend/data/` (which holds `routes.json`, real user route data) was not volume-mounted, so it lived in the container's ephemeral layer and would be lost on container recreation (e.g. a routine image update). This ADR adds `./backend-data:/app/data`. This was necessary for the new `sat-charts/` directory (losing a multi-hundred-MB satellite chart on the next update would defeat the point of uploading it) and incidentally fixes the same pre-existing risk for `routes.json` for free.

### Frontend
`hooks/use-sat-charts.ts` mirrors `use-routes.ts`'s fetch/mutate-then-refetch shape, with an `uploadChart(file)` mutator posting `FormData` instead of a JSON body. A new `components/sat-charts-drawer.tsx` provides the upload UI and chart list (name, bounds, zoom range, size, delete), surfaced as a new "Charts" tab in the bottom drawer next to "Routes". `route-planner-map.tsx` renders one `bounds`-scoped raster `Source`/`Layer` per uploaded chart — MapLibre's `bounds` prop means each chart only requests/renders tiles within its own coverage rectangle automatically, with no manual viewport-intersection code needed.

Uploaded charts always render when present, **not** gated by the existing Esri-imagery toggle: that toggle exists because *live* imagery has a reason to be opt-in (bandwidth/licensing); a chart the operator deliberately uploaded has neither concern, and hiding it by default would undermine the reason they uploaded it.

## Consequences
Positive:
- The Windows-only conversion+import half of the workflow (Sat2Chart, OpenCPN) is replaced by a clean in-app upload, with zero new licensing exposure since acquisition is unchanged and helmcentral never bulk-fetches from a live provider.
- `routes.json`'s pre-existing durability gap is fixed as a side effect of the same volume-mount change this feature needed anyway.
- Full test coverage including a property-based test of the TMS/XYZ row-flip math, the single highest-risk detail in this feature.

Negative / explicitly deferred:
- `anchor-watch-map.tsx` does not get this layer in this pass, consistent with ADR 0009's same scoping decision for the same file — a natural, low-risk future extension.
- No z-order/overlap handling for multiple uploaded charts that happen to cover the same area — charts for distinct reef-spotting areas aren't expected to overlap much in practice; not designed here.
- Each tile request opens a fresh SQLite connection rather than pooling/caching open handles — the simplest correct choice at this scale (personal dashboard, low concurrent request volume); an LRU of open handles is the natural follow-up if this ever becomes a measured bottleneck.
- No real chart-coverage detection exists (per ADR 0009) — an uploaded satellite chart and the GSHHG fallback layer can both render in the same view with no awareness of each other; this hasn't been an issue in practice since satellite imagery and the muted coastline fallback read as visually distinct, but isn't a coordinated design.

## Related
- ADR 0006: Manual Route Planning with Smart Helpers — the "no chart licensing dependency of any kind" decision this ADR's split (acquisition stays out of scope; helmcentral itself never bulk-fetches from a live provider) is designed to respect, not reopen.
- ADR 0009: GSHHG Coastline Fallback Layer for the Route Planner Map — precedent for scoping new map layers to `route-planner-map.tsx` first, and for the single-narrow-fallback-exception reasoning applied to the list endpoint here.
- ADR 0010: Sourcing Real Australian Chart Data (AusENC) — the other live-imagery-licensing tension explored this session, which led directly to the user's choice of the MBTiles-upload path over building automatic bulk caching against any live tile provider.
