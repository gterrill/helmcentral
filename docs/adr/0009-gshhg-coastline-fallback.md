# ADR 0009: GSHHG Coastline Fallback Layer for the Route Planner Map

## Status
Accepted

## Context
ADR 0006 (Manual Route Planning) explicitly decided against any chart licensing dependency, and the route planner map currently has no land/coastline reference layer beyond whatever the underlying Carto basemap style happens to render and the OpenSeaMap seamark overlay. A route can currently be drawn straight through a landmass with zero visual cue beyond the basemap's own (non-marine-purposed) land rendering. ADR 0066 later narrows the "whatever the basemap happens to render" part for place names, by adding our own style layers on the Carto vector source. That is independent of the coastline geometry this ADR is about.

The longer-term plan is to support S-57 ENC (Electronic Navigational Chart) ingestion, with chart-coverage detection so the app can pick the best available chart for the current view. That pipeline does not exist yet: the repo has no GDAL dependency, S-57 parser, or chart-coverage catalog. Building it is a substantially larger effort than this change. The operator requested publicly available coastline geometry as an orientation aid when no chart is available, which currently applies to every view.

GSHHG (the Global Self-consistent, Hierarchical, High-resolution Geography database) is a public-domain coastline/land-polygon dataset maintained by NOAA/NGDC and distributed via GMT. It ships in 5 resolutions (crude, low, intermediate, high, full). It contains only land/water boundary polygons, not a navigational chart: no depths, hazards, aids to navigation, or datum/survey-accuracy guarantees suitable for marine navigation.

AGENTS.md's Fallback Policy disallows fallbacks that mask upstream problems by default. Its Exception Rule permits a requested fallback when gated behind a clear feature flag and logged when used. This feature follows that exception.

## Decision

### Scope: fallback layer only, not real chart-coverage detection
This ADR covers only:
1. A GSHHG coastline dataset, preprocessed once and committed as a static backend asset.
2. A backend endpoint serving it.
3. A frontend layer in `RoutePlannerMap` that renders it when charts are unavailable.
4. A stubbed `chartAvailable` flag/prop that today always evaluates to "unavailable," explicitly documented as a placeholder.

It does **not** cover real S-57/ENC ingestion, parsing, or a chart-coverage catalog (see Deferred, below). The "is a chart available for this view" condition is intentionally a hardcoded stub (`lib/chart-availability.ts`) until that future work lands.

### GSHHG resolution and preprocessing
The low-resolution (`l`) GSHHG level, L1 boundary only (continents/islands; lake/pond sub-levels excluded), was converted once, offline, from the official shapefile distribution (`gshhg-shp-2.3.7.zip`, https://www.ngdc.noaa.gov/mgg/shorelines/) to GeoJSON using `pyshp` (a pure-Python shapefile reader) in a throwaway virtualenv, with coordinates rounded to 4 decimal places (~11m precision):

```python
import json, shapefile

reader = shapefile.Reader("GSHHS_l_L1.shp")
features = [
    {"type": "Feature", "properties": {}, "geometry": sr.shape.__geo_interface__}
    for sr in reader.shapeRecords()
]
json.dump({"type": "FeatureCollection", "features": features}, open("gshhg_coastline_l.json", "w"))
```

(`ogr2ogr`/GDAL was the originally-anticipated tool for this conversion, but wasn't available locally; `pyshp` produces an equivalent result for this source shapefile — a plain `geo_interface` dump per shape record — without requiring a GDAL install. Either is a dev-machine-only, one-time tool, never added as a Go or npm runtime dependency, never run in CI.)

Low resolution, L1-only, produced a 5,706-feature, ~1.5MB GeoJSON file — comfortably small to commit and embed (smaller than initially estimated; the full multi-resolution GSHHG archive is ~140MB, but low-resolution L1 alone is a small fraction of that). This is the deliberate middle point between crude (visibly inaccurate at the coastal zoom levels this fallback targets) and high/full (unnecessary detail for a "don't run into an obvious landmass" reference). The resulting file is committed at `backend/data/gshhg_coastline_l.json`, alongside the already-committed `bom_tide_sites.json` reference dataset in that same directory (which also holds gitignored per-deployment user data like `routes.json` — selective committing of reference data within `backend/data/` is an established pattern, not a new one).

### Backend: embed and endpoint
`backend/gshhg.go` embeds the committed GeoJSON via `go:embed`, mirroring the existing pattern in `backend/tide_provider_bom.go` (`//go:embed data/bom_tide_sites.json`). A handler is registered at `GET /api/gshhg-coastline` (`backend/main.go`, next to the world-imagery proxy registration), returning the raw embedded bytes with `Content-Type: application/json`.

The cache header is `Cache-Control: public, max-age=604800, immutable` — deliberately much longer than `tile_proxy.go`'s `public, max-age=1800` for its live upstream-imagery proxy. That distinction matters: `tile_proxy.go` proxies a *live, mutable* third-party tile service, so a short cache window is the safe default. This endpoint serves a *static, build-time-embedded* asset that is byte-identical for the life of a deployed binary, so a long-lived, `immutable`-flagged cache is correct and avoids needless repeated transfer of a ~1.5MB payload.

### Frontend: stub flag, hook, and layer
`lib/chart-availability.ts` exports `isChartAvailable()`, hardcoded to always return `false`, with a doc comment making explicit that it is a placeholder for future real S-57 coverage detection. `RoutePlannerMap` has an optional `chartAvailable?: boolean` prop defaulting to this stub's result, so tests can override it directly without needing to mock the stub module.

`hooks/use-gshhg-coastline.ts` fetches `/api/gshhg-coastline` once per session (no polling — the data cannot change within a session, mirroring `hooks/use-routes.ts`'s "routes are user-authored, not externally mutated" reasoning, here even more strongly true since the data is compiled into the backend binary), with a module-scope cache so repeated mounts don't re-fetch the payload.

When `chartAvailable` is `false` and the coastline data has loaded, `RoutePlannerMap` renders a `gshhg-coastline` GeoJSON `Source`/`Layer` pair (muted fill + dashed outline, deliberately distinct from both the basemap's own land color and the route line's solid saturated blue, signaling "reference only, not a chart") plus an on-map pill badge reading "No chart data — reference coastline only," reusing the existing waypoint-hint pill's visual styling. A `useEffect` emits `console.info('[gshhg-coastline-fallback] ...')` exactly once per transition into the fallback being shown, satisfying the Exception Rule's "explicit logs ... that indicate fallback was used" requirement. This frontend has no analytics/telemetry pipeline today, so a console log with a distinguishing bracketed prefix (matching the existing convention used by `[anchor-watch-auto-close]`, `[anchor-alarm]`, `[Radar Debug]`) is the minimal consistent choice, not a new pipeline.

No manual user-facing toggle is added for this layer (unlike the satellite-imagery toggle) — it is a correctness fallback for an unmet precondition (no chart), not a user preference, so it appears and disappears automatically as `chartAvailable` changes.

### Why this does not revisit ADR 0006's licensing decision
ADR 0006 rejected chart licensing dependencies and ruled out NOAA's ENC/RNC for being US-only in an international cruising deployment. GSHHG is a public-domain global land/water boundary dataset, with no license, attribution requirement, usage restriction, per-chart terms, or claim to navigational accuracy. Like the existing OpenSeaMap overlay, it provides a free global reference layer without revisiting ADR 0006's chart-licensing decision.

### Deferred
Real S-57/ENC ingestion (GDAL-based parsing of actual navigational charts, including depths, hazards, and aids to navigation), a chart-coverage catalog (determining which areas have real chart data available, replacing the `chartAvailable` stub with a real lookup), and sourcing ENC data for international waters beyond any single national hydrographic office, are all explicitly out of scope here and left to a future ADR once that pipeline is designed.

## Consequences
Positive:
- Users see at least approximate coastline geometry in the route planner instead of nothing, in the (currently universal) case where no real chart exists — directly addressing the "can route through land with zero visual cue" gap noted as a known limitation in ADR 0006.
- No new runtime dependency: GSHHG conversion was a one-time, dev-time-only step producing a single committed static asset; no GDAL, turf, or chart-processing library is added to either `go.mod` or `package.json`.
- The fallback is controlled by the `chartAvailable` prop / `lib/chart-availability.ts` stub and logged when activated, per the AGENTS.md Exception Rule. The log helps diagnose chart-availability problems.
- The long-cache-header backend endpoint and module-scope frontend cache mean the (~1.5MB) coastline payload is fetched at most once per deployed version per browser session, not on every map mount.

Negative / explicitly deferred:
- The `chartAvailable` flag is a hardcoded stub that always reports "unavailable". Until chart-coverage detection is implemented, the fallback layer renders even if chart data becomes available for a view.
- The visual treatment (muted dashed-outline fill) is a first-pass design choice, not user-tested; it may need revisiting once a real chart layer exists alongside it and the two need to visually coexist or hand off cleanly.
- Low-resolution GSHHG is not survey-accurate; it is unsuitable for any hazard-avoidance purpose and is presented purely as an orientation aid, not a substitute for the navigational data this ADR explicitly defers.
- This change touches only `route-planner-map.tsx`; `anchor-watch-map.tsx` does not get this fallback layer in this pass. Extending it there could reuse the duplicated map chrome described in ADR 0006, but is not designed here.

## Related
- ADR 0006: Manual Route Planning with Smart Helpers — the "no chart licensing dependency of any kind" decision this ADR clarifies does not apply to GSHHG's public-domain data; also the source of the "~40 lines of intentional duplication between route-planner-map and anchor-watch-map" decision this ADR does not change.
