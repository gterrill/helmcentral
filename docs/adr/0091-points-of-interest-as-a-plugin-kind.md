# ADR 0091: Points of Interest as a Plugin Kind

## Status
Accepted

## Context

The kiosk-feed plan (`~/.claude/plans/delightful-drifting-anchor.md`) calls
for a moving POI map: anchorages, bays, islands, marinas, fuel, boat ramps,
moorings, historic landmarks, dive/snorkel spots, lookouts and walking
trails, ranked by distance from the vessel. Nothing in this codebase
resolved any of that before this change.

`backend/place_name.go` already talks to Overpass, but only for one purpose:
resolve the single best-named feature near a point, for the vessel-status
place name. It has no concept of a category, a result list, or ranking more
than one candidate for display. Growing it into a POI feed would mean
overloading a file whose entire design (a ladder of widening rings, one
named winner) is built around a different problem.

Every other data source with more than one plausible upstream in this
codebase - tides, weather, waves, forecast warnings, upper air - is already
a WASM plugin kind (ADR 0017, ADR 0018, ADR 0071), for the same reason: no
single provider covers every operator's cruising ground or budget, and
swapping providers should not need a Helmcentral rebuild. POIs have the same
shape of problem, arguably more so - OpenStreetMap's coverage varies wildly
by category and region (see `docs/reference/poi-categories.md`), and an
operator who wants Google's business database instead needs a real
alternative, not a hardcoded second code path.

## Decision

### POI joins the plugin system as a sixth kind

`backend/poi_providers.go` and `backend/wasm_poi_provider.go` mirror the
wave provider's shape (`wave_providers.go`/`wasm_wave_provider.go`)
file-for-file: a `poiProvider` interface, a registry with no native
built-in, `GET /api/poi-providers`, and `resolvePOIProvider` reading
`ui.poi_provider` with the same actionable-error-on-unregistered-provider
behaviour as every other domain. `osm-overpass` is the default
(`defaultPOIProviderID`), because it needs no API key - the same reasoning
ADR 0018 gives for Open-Meteo and Open-Meteo Marine.

Two gaps in the existing plugin machinery were fixed in the same change,
having been found while wiring poi through it:
`backend/plugin_overrides_handlers.go`'s `providerByTypeAndID` switch was
missing an `upper-air` case (so `/api/plugins/upper-air/:id` 400'd as an
unknown type since upper air was added), and the backend had never needed
an initial bearing between two arbitrary points before - `bearingDeg` is
new in `poi_providers.go`, ported from `frontend/src/lib/geo.ts`'s function
of the same name, which the frontend has relied on since the anchor-watch
map existed.

### The host owns distance, bearing, ranking and dedupe

The contract (`fetch_poi`) is deliberately narrow: a plugin returns raw
features - id, category, name, position, and an optional detail string and
source URL - for the categories it was asked about, plus which categories
it had to truncate (hit its own cap) and which it doesn't support at all.
It does not compute distance, does not sort, does not dedupe.

This is the same division of labour ADR-0017/0018 established for tides and
weather (the host does day-bucketing and derived summaries; the plugin
returns raw provider-native numbers), extended to a case where the
"derived" work - haversine distance, initial bearing, a 5-decimal-place
name+position dedupe key, sort, limit - would otherwise be reimplemented
slightly differently by every provider. `osm-overpass` and `google-places`
both need the same ranking logic; writing it once in
`backend/poi_providers.go` is why the plugin contract stays this thin, and
means a future POI plugin never has to get haversine or bearing right on
its own.

### A 0.02 degree cache cell, tighter than weather's

Weather/wave/tide caches round position to 1 decimal place (~11km) or a
station lookup, because a numerical weather model's own resolution is that
coarse to begin with. A POI cache at that granularity would be nonsense - a
degree-tenth cell could span several distinct anchorages. `poiWasmCacheKey`
(`wasm_poi_provider.go`) rounds to 0.02 degrees (about 2km) instead: coarse
enough that ordinary vessel movement and anchor swing keep hitting the same
cached entry within the 6-hour TTL, fine enough that the cell doesn't erase
genuinely different local answers a couple of kilometres apart - the same
concern ADR 0056 raised about the old 0.5-degree place-name cache conflating
Goldsmith and Lindeman.

### Position comes from the server, not the client

`poiNearby` reads the vessel's position from `fetchSignalKVesselState()`,
gated by `hasUsableVesselPosition` exactly as `waveForecast` is - never a
client-supplied lat/lon. The Nearby widget calling this endpoint has no way
to lie about the boat's location, and the -1/-1 and 0,0 sentinel checks
(the same ones the anchor-watch gate needed) apply here too.

### Wikipedia is enrichment inside the OSM provider, not a separate plugin

HelmMap's prior Wikipedia code resolved article titles from OSM `wikipedia`
tags and used the same REST page-summary endpoint this plugin now uses. A
standalone "Wikipedia" POI provider was considered and rejected: Wikipedia's
geosearch API can find articles near a point, but it cannot answer "is there
a mooring here" or "is there a fuel dock here" - it has no marine-specific
category vocabulary at all, so it cannot stand in as a provider for most of
this feature's eleven categories. It is only ever useful as a description
source for a feature another source already found and named. `osm-overpass`
therefore enriches its own OSM results (nearest `detail_limit`, default 5,
Wikipedia-tagged features get a first-sentence summary) rather than the host
merging two separate providers' output, which would need to invent a rule
for matching a Wikipedia article back to an OSM feature - a problem OSM's
own `wikipedia` tag has already solved for the features that carry it.

### One Overpass host, no mirror failover

`osm-overpass.allowed_hosts.json` lists only `overpass-api.de`. A failover
list of public Overpass mirrors was considered and rejected: mirrors vary in
freshness, uptime and load characteristics an operator has no visibility
into, and silently falling back to a different data source on a timeout is
the kind of masking fallback this codebase's fail-fast policy rules out. A
slow or rate-limited Overpass instead surfaces as an explicit error (or a
stale cache hit, per the existing plugin cache's stale-on-error contract) -
the operator can tell something is wrong, rather than the plugin quietly
answering from a different, unverified source.

(This plugin's own development fixtures were captured against
`overpass.kumi.systems`, a public mirror, purely because the sandbox this
plugin was written in could not route to `overpass-api.de`'s IP addresses -
see `docs/examples/poi-plugins/osm-overpass/README.md`. That is a
capture-environment detail, not a design decision, and the shipped plugin's
`allowed_hosts.json` reflects the decision above, not the workaround.)

### `out tags center`, not `out geom`

Mirrors `backend/place_name.go`'s existing choice: `out geom` would return
full linework for a way or relation (a mainland coastline, a long trail),
which for eleven categories at once would be a large and mostly useless
payload - Nearby only ever needs a representative point per feature.
`out tags center <cap>` gives that directly, with the cap enforced
server-side per category.

### One Overpass request per call, not one per category

`buildOverpassPOIQuery` builds every requested category into a single POST,
each in its own named Overpass set with its own cap, rather than issuing one
request per category. Overpass is a shared public service the same host
already queries for place names; multiplying requests by category count
would be markedly less polite for no benefit the host actually needs, since
every category's data is wanted on every Nearby fetch. The tradeoff is a
larger, slower single query, mitigated by the 6-hour TTL and the 0.02 degree
cache cell meaning this request is infrequent in practice, and a plugin
timeout raised (implicitly, via the 15s default plugin timeout budget noted
in the kiosk plan's risk list) by keeping each category's query bounded to a
tight `around:` filter and a capped `out`.

### Rejected: per-category truncation via a second Overpass query

Overpass's `out ... <cap>` statement gives no "there were N more" signal.
An exact count would need a second `out count;` statement per category,
doubling the categories' worth of Overpass load for a number this feature
only uses to render a "there's more, zoom in" hint. `truncatedCategories`
instead reports a category as truncated when its returned count lands
exactly on the cap - an honest, if imperfect, heuristic (see
`osm-overpass.go`'s doc comment), accepted in preference to the extra load.

## Consequences

Positive:
- A POI data source is a `.wasm` file plus a companion allowlist, matching
  every other provider kind - `use-poi-providers.ts` is a direct copy of
  `use-wave-providers.ts`, and the Settings provider-card machinery needed
  only its existing per-domain tables extended, not new UI.
- `osm-overpass` needs no API key, so a fresh install gets a working Nearby
  feature immediately, the same "useful out of the box" property every
  other default provider in this codebase has.
- The `upper-air` plugin-overrides gap is fixed as a byproduct of extending
  the same switch statement for `poi`.

Negative / accepted tradeoffs:
- Truncation detection is a heuristic (exact-cap-hit), not an exact count,
  for both `osm-overpass` (Overpass has no count-without-a-second-query) and
  `google-places` (Places' `maxResultCount` has the identical shape
  problem).
- `google-places` only maps four of eleven categories onto real Google Places
  types (see `docs/reference/poi-categories.md`) - Places models businesses
  and attractions, not landform or navigational features, and there is no
  honest mapping for the rest.
- No plugin hot-reload, the same limitation every other plugin kind in this
  codebase already accepts - `plugins/poi/` is scanned once at startup.

## Related
- ADR-0017: Sandboxed WASM Plugin Tide Providers - the shared host layer
  (`wasm_plugin.go`) this plugin kind reuses unchanged.
- ADR-0018: Sandboxed WASM Plugin Weather and Wave Forecast Providers - the
  direct template for `poi_providers.go`/`wasm_poi_provider.go`'s shape and
  for the "host owns derived data" division of labour.
- ADR-0056: the 56km place-name cache-cell bug this ADR's 0.02 degree POI
  cache cell is deliberately sized away from repeating.
