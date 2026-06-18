# ADR 0006: Manual Route Planning with Smart Helpers

## Status
Accepted

## Context
Operators wanted a way to plan multi-leg routes (sequences of waypoints) ahead of a passage, with automatic distance/bearing/ETA so they don't have to do chart math by hand.

A feasibility review considered three tiers of "auto-assist":
- Manual waypoints with automatic distance/bearing/ETA helpers.
- Hazard-aware route suggestions (avoid shallow water / no-go zones), which requires bathymetry data and a pathfinding algorithm.
- Full weather-optimized routing (isochrone routing using wind/current forecasts), which requires a boat polar/performance model and a routing solver.

Commercial chart providers (Navionics, C-MAP, Garmin) generally do not license their data for embedding in a third-party app, so that door is effectively closed regardless of budget. Free alternatives exist (OpenSeaMap, already integrated as a tile overlay; NOAA's public-domain ENC/RNC for US waters; GEBCO bathymetry globally) but the cruising area for this deployment is international/mixed, which rules out the US-only NOAA option as a differentiator.

Given the above, the decision was to ship the first tier only: no hazard-avoidance pathfinding, no weather-optimized routing, no chart licensing of any kind, no autopilot/active-route-following (cross-track error, bearing-to-waypoint live nav), no GPX import/export.

## Decision

### Data model and persistence
A route is `{id, name, waypoints: [{lat, lon, name?}], created_at, updated_at}`. Routes are a collection (unlike the singleton anchor-watch entity), persisted as a flat JSON file at `backend/data/routes.json` (not `cache/`, since routes are durable user data, not rebuildable/ephemeral state — `data/` already held the committed `bom_tide_sites.json` reference dataset; `data/routes.json` itself is gitignored since it's per-deployment user data). IDs are generated with `google/uuid` (already present as a transitive dependency, promoted to direct). Persistence follows the same atomic-write pattern (`writeJSONFileAtomic`) used by `anchor.go`.

### Backend API
`backend/routes.go` exposes standard CRUD under `/api/routes`: `GET` (list), `POST` (create), `GET/PATCH/DELETE /api/routes/:id`. All distance/bearing/ETA math is computed client-side; the backend only persists coordinates and names. Validation mirrors `setAnchorWatch`'s lat/lon range checks.

### Frontend architecture
- `lib/geo.ts` — `haversineMeters`, `bearingDeg`, `destinationPoint`, extracted from `anchor-watch-map.tsx` and `use-anchor-watch.ts` (previously duplicated in both) into a single shared module.
- `lib/route-calc.ts` — pure leg/total distance, bearing, and ETA calculations plus `formatNm`/`formatEtaHours` display helpers, kept out of React components so they're trivially unit-testable.
- `hooks/use-routes.ts` — CRUD hook. Unlike `use-anchor-watch.ts`, it does not poll, since routes are user-authored rather than externally mutated.
- `hooks/use-dashboard-route.ts` — tracks which single route (if any) is pinned to the dashboard tile, via `localStorage`.
- `components/route-planner-map.tsx` — a dedicated MapLibre map (not a reuse/extension of `AnchorWatchMap`, whose state machine is built around one point + one radius rather than an ordered N-waypoint list). Click adds a waypoint, native marker drag repositions it, a bottom pill offers delete; reordering happens via list controls in the summary panel rather than on the map, since drag-to-reorder on a touch dashboard is error-prone. The map reuses the same chrome as the anchor-watch map (OpenSeaMap overlay, Esri World Imagery / Himawari satellite blend toggle via the shared `computeSatelliteBlend` export, zoom controls) by duplicating the setup rather than extracting a shared base-layer component, to avoid risk to the working anchor-watch map for this first pass. The initial view centers on the vessel's current position when no waypoints exist yet (falling back to a world view only if no GPS fix is available), and auto-recenters once if the fix arrives shortly after mount.
- `components/route-summary-panel.tsx` — presentational per-leg distance/bearing/ETA list with reorder/delete, plus route totals at a user-adjustable planning speed (defaults to live SOG if available and non-zero, else 6 kts; not persisted to the route itself).
- `components/route-planner-drawer.tsx` — list/create/edit/save UI, registered as a new "Routes" tab in the existing bottom drawer (`App.tsx`).
- `components/route-tile.tsx` — an optional glanceable dashboard tile showing the pinned route's name/distance/ETA; renders nothing when no route is pinned, consistent with the "drawer is the primary surface" decision.

### Shared state lifting
`useRoutes()` and `useDashboardRouteId()` are called once in `App.tsx` and passed down as props to both `RoutePlannerDrawer` and `RouteTile`, matching the existing pattern for `useAnchorWatch`/`useTheme`/`useDarkMode`. An earlier version had each component call the hooks independently, which meant creating or pinning a route in the drawer never reached the tile's separate fetch/state — caught during manual browser verification and fixed by lifting the hooks.

## Consequences
Positive:
- No chart licensing dependency of any kind; reuses infrastructure (map chrome, geometry math, JSON persistence pattern) that was already 60-70% in place before this feature.
- Geometry math has a single source of truth (`lib/geo.ts`) instead of two diverging copies.
- Full CRUD test coverage on the backend (`routes_test.go`) and frontend (hooks, pure calculations, map interactions, summary panel, tile) without needing a browser for most of it.

Negative / explicitly deferred:
- No hazard-avoidance or weather-optimized routing; a route can be drawn straight through land or shoal water with no warning. Revisiting this would need bathymetry data (GEBCO globally, or NOAA ENC for US-only deployments) and a pathfinding/isochrone solver — a substantially larger effort, deferred until there's a concrete need.
- No active route-following (no bearing-to-waypoint or cross-track error display while underway, no autopilot integration). Routes are a planning tool only.
- No GPX import/export.
- The map duplicates ~40 lines of base-layer chrome from `AnchorWatchMap` rather than sharing it; a future refactor could extract a common `MapBaseLayers` component if a third map consumer appears.

## Related
- ADR 0004: GNSS Validation Gate for Anchor Watch (the anchor-watch persistence/CRUD pattern this feature mirrors)
