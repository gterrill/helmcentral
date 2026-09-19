# ADR 0113: Removing Uploaded Satellite Charts

## Status

Accepted. Supersedes ADR 0011 (In-App MBTiles Satellite Chart
Upload-and-Serve): the feature it introduced is deleted outright, not
amended. ADR 0011 is left as written — it is the historical record of why
the feature existed — and is not itself edited.

Also touches ADR 0074's deep-link table, which lists `/charts` as a live
route; that row is now stale prose in a historical document, and is called
out here rather than edited there.

## Context

ADR 0011 gave the route planner an upload-and-serve path for MBTiles
satellite imagery: acquire tiles on a desktop with SASPlanet, convert with
Sat2Chart, then upload the result to Helmcentral instead of importing it into
OpenCPN. The backend validated and stored the file, served its tiles with
the TMS/XYZ row-flip math MBTiles requires, and the route planner's map drew
one bounds-scoped raster layer per uploaded chart.

The operator decided the feature was not worth keeping and asked for it
removed entirely, upload path and map overlay together: with no upload UI
there is nothing that can ever put a chart in front of the route planner's
per-chart raster layer, so keeping that layer "for later" would leave a
renderer with nothing it could render. Removing only the backend and leaving
the map layer, the drawer, or the panel behind would have been the same
outcome with extra steps — dead code with no path back to being live short
of rebuilding the upload UI this ADR removes.

## Decision

### Backend: the whole subsystem goes

`backend/sat_charts.go` and its test file are deleted in full: upload
validation, on-disk storage, MBTiles tile serving with the TMS/XYZ row-flip,
and the abandoned-upload sweep that ran at boot next to the documents
store's. All four routes go with it:

- `GET /api/sat-charts` (list)
- `POST /api/sat-charts` (upload)
- `GET /api/sat-charts/:id/:z/:x/:y` (tile serve)
- `DELETE /api/sat-charts/:id` (delete)

`compression.go`'s no-compress skip list drops its entry for the tile route
along with it — there is no longer a second already-compressed raster
endpoint next to `tile_proxy.go`'s, just the one. `assistant_prompt.go`
drops "charts" from the panel-label table Mate's screen-context feature
reads, since the panel it named no longer exists. A handful of other files
(`wasm_tide_provider.go`, `tile_cache.go`, `use-imagery-prefetch.ts`,
`documents_handlers_test.go`) cited `sat_charts.go` in comments as precedent
for an idiom (skip-corrupt-keep-going, cache-forever, an env-var-override
test pattern); those comments are reworded to describe the idiom directly
rather than point at a file that no longer exists.

`SAT_CHARTS_DIR` is removed from `docs/reference/configuration.md`. The
`backend/data/sat-charts/` entries in `.gitignore` and both `.dockerignore`
files are **deliberately kept**, with a comment saying why. Deleting the
feature does not delete the files an instance already took: both Dockerfiles
copy the backend tree wholesale (`COPY backend/. .`, `COPY . .`), so an
un-ignored directory would stream leftover MBTiles (up to 4 GiB each under
the old upload cap) into the build context and bake them into the builder
layer, and a `git add -A` would stage them. The ignore entries stay until
the directory itself is gone from every deploy.

### Frontend: the panel, the hook, and the map overlay

`'charts'` is removed from `PanelId` (`app-location.ts`), which removes the
`/charts` route, the sidebar nav item, the manual deep-link entry
(`manual-links.ts`), and every switch case in `App.tsx` that branched on it.
`use-sat-charts.ts` and `sat-charts-drawer.tsx` are deleted along with their
tests.

`route-planner-map.tsx`'s per-chart raster `Source`/`Layer` block — one
bounds-scoped MapLibre raster layer per uploaded chart — is deleted, and the
`satCharts` prop is dropped from both `RoutePlannerMap` and
`RoutePlannerDrawer` (the only place that threaded it down from `App.tsx`).
With the upload UI and its backend route gone, that block could never have
rendered anything again, so it would have been dead code the moment the
rest of the feature was removed.

The layer-ordering comments in `route-planner-map.tsx` that explained where
the sat-chart layer sat relative to the invisible `raster-overlay-anchor`,
the OpenSeaMap seamark overlay, and place-name labels are reworded to
describe the layers that remain (world imagery, OpenSeaMap, route line,
GSHHG coastline fallback) without losing the reasoning: `beforeId` pins
every raster layer below the anchor so JSX mount order can't put a
later-mounted layer on top of the route line.

### Docs

`docs/features/dashboard.md` loses the `/charts` row from its deep-link
table, the "Charts" mention in the indicator-ribbon panel list, and the
"Satellite charts" section under "Beyond the grid" in full.
`docs/reference/configuration.md` loses the `SAT_CHARTS_DIR` row.
`PRODUCT.md` and `README.md` each drop "satellite charts from \[the
operator's/your own\] MBTiles" from a run-on feature sentence, without
otherwise changing the sentence's shape.

`docs/adr/0074-deep-links.md` still lists `/charts` in its URL table (line
55) as a historical record of what that ADR shipped; it is not edited,
consistent with ADRs 0016, 0017, 0034, 0040, 0066 and 0067 each keeping their
own references to ADR 0011 and `sat_charts.go` as-written.

## Consequences

- The route planner map now renders exactly the layers ADR 0067 and ADR
  0009 already described — Carto/OSM base, OpenSeaMap seamarks, the Esri
  hybrid-imagery toggle, the GSHHG coastline fallback — with no
  operator-uploaded layer in the stack.
- An operator who wants imagery for reef-spotting is back to the live Esri
  World Imagery toggle (ADR 0009/0016) or their own external chart-plotting
  tools. The SASPlanet-to-Sat2Chart-to-Helmcentral workflow ADR 0011 built
  for is no longer supported in-app.
- If a `backend/data/sat-charts/` directory exists on a deployed instance
  from before this change, it is now inert: nothing sweeps it, nothing
  serves from it, and nothing deletes it automatically. Removing it is a
  manual step for whoever runs that deploy, and until that happens the
  ignore entries above are what keep those files out of git and out of a
  Docker build context.
- `docs/adr/0074-deep-links.md`'s URL table is now one row stale. It is left
  alone per this project's rule that ADRs are a historical record, not
  edited to stay current with the code.

## Related

- ADR 0011: In-App MBTiles Satellite Chart Upload-and-Serve — the feature
  this ADR removes. Left unedited as the record of why it was built.
- ADR 0009: GSHHG Coastline Fallback Layer for the Route Planner Map — the
  other route-planner-map.tsx raster/fallback layer, unaffected by this
  removal and still the reference for `chartAvailable`'s stub behaviour.
- ADR 0016: Cached Deeper Esri Imagery and Area Prefetch — the live-imagery
  path an operator now falls back to; its own references to `sat_charts.go`
  as precedent are left as written.
- ADR 0074: Deep Links — its URL table still lists the now-removed `/charts`
  route; not edited, per the Decision section above.
