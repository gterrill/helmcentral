# ADR 0101: Place Names Come From a Plugin

## Status

Accepted. References ADR 0056 (place names from OSM features, the ring
ladder and cache this ADR keeps), ADR 0091 (points of interest as a plugin
kind, whose registry and settings idiom this reuses), ADR 0093 (the
assistant's `find_places` tool, whose two-rung search moves into the
plugin here) and ADR 0100 (plugins declare their own settings, which is how
osm-overpass's Overpass mirror is configured both before and after this
change).

## Context

Two backend call sites talked to Overpass directly: `place_name.go`'s
widening-ring ladder (the position tile, the anchor pin) and
`assistant_tools.go`'s `find_places` tool (Mate's two-rung exact/regex
name search). Both built their own Overpass QL, POSTed it with their own
HTTP client, parsed the same `{"elements": [...]}` response shape, and
handled the same rate-limit signature (`HTTP 200` with an HTML body). The
osm-overpass POI plugin (ADR 0091) built and parsed a third, near-identical
copy of the same thing for the Nearby widget. Three copies of one
integration is not a division of labour, it is drift waiting to happen -
and it already had: the plugin's bbox math, tag-to-kind mapping and
rate-limit detection were each slightly different from the backend's own.

The operator also had no way to choose a different place-names source
without also changing which plugin answers Nearby. `ui.poi_provider`
selects one plugin for both jobs, but they are not the same job: Nearby is
"list nearby facilities in some categories," place-name resolution is "what
is this exact point called." A plugin can be excellent at one and useless
at the other - `google-places` (ADR 0091) has no reverse-geocoding
endpoint that answers the second question, and there is no reason a future
POI plugin covering, say, fuel-dock listings only would need to as well.

## Decision

### The plugin, not the backend, owns Overpass

`place_name.go`'s Overpass client (query building, HTTP POST, rate-limit
detection, JSON parsing, the ring-ranking logic) and
`assistant_tools.go`'s duplicate copy of the same are deleted outright, not
kept as a fallback. The osm-overpass plugin
(`docs/examples/poi-plugins/osm-overpass`) is the only place that speaks
Overpass QL, HTTP and response parsing for place names now, exactly as it
already was the only place that did so for Nearby.

### Two new optional exports make a POI plugin a place-names provider

A POI plugin declares place-name support by exporting **both**
`place_name_at` and `search_places` - not a manifest flag, a fourth plugin
kind, or a new directory convention. `wasm_plugin.go`'s `newWasmPluginBase`
probes for both exports the same way it already probes for the existing
optional `description()`/`ttl_seconds()` exports, and sets
`SupportsPlaceNames()` only when both are present; a plugin exporting just
one (an authoring slip) is treated as supporting neither, never guessed at.

- `place_name_at` takes `{"lat", "lon", "radius_m"}` and returns either a
  winner - `{"name", "kind", "lat", "lon"}` - or the legitimate negative
  `{"name": ""}`. The plugin owns the ranking (anchorage, then bay, then
  island/islet/rock, ties broken by distance) that used to live in
  `place_name.go`.
- `search_places` takes `{"query", "lat", "lon", "max_results", "broad"}`
  and returns `{"search": "exact"|"regex"|"none", "radius_nm",
  "centred_on": {"name","lat","lon"}|null, "results": [{"name","kind","lat","lon"}],
  "note"}`. The plugin owns the two-rung ladder (exact name over a 100nm
  bbox, then - only when `broad` is true and the exact rung came back empty -
  a partial-name regex over a 20nm bbox, including the comma-qualifier
  centring `find_places` used to do itself). `broad` is how the host tells
  the plugin a saved route waypoint already answered the query, so the
  expensive regex rung isn't worth running - the plugin still always runs
  its own cheap exact-name rung regardless, since it's the only way the
  plugin's own copy of a waypoint's position ever reaches the model.

Both exports report upstream failure as a call error - including an
Overpass `remark` field starting with `"runtime error"` (a query that ran
out of the server-side time budget), which `parseOverpassBody` now detects
and rejects explicitly rather than returning it as a normal, silently
incomplete result. Osm-overpass's `overpass_url` (ADR 0100) applies to both
exports the same way it already applies to `fetch_poi`.

### The host keeps everything that isn't Overpass-specific

`place_name.go` keeps the widening-ring loop (`resolvePlaceName`: try 400m,
then 1500m, then 5000m, stop at the first named answer), the 0.005°-cell
cache (512-entry FIFO, 24h TTL for a name, 6h for a genuine empty result,
errors never cached), the failure backoff (1 minute doubling to 15,
resetting on success), the anchor-watch pin, the stale-cell drop on the
poll tick, and `GET /api/place-name`. None of that is Overpass-specific -
it is the same shape of problem regardless of which plugin answers
`PlaceNameAt`.

`assistant_tools.go`'s `executeFindPlaces` keeps the saved-route-waypoint
search (still tried first, still wins a name collision within 500m over
whatever the plugin returns), the dedupe, the sort-by-distance, the
`max_results` trim and the 12k-character result cap. It calls the
configured provider's `SearchPlaces` exactly once per invocation and maps
its raw matches to `distance_nm`/`bearing_deg` itself, the same
provider-returns-raw-features-host-computes-distance rule ADR 0091 already
applies to Nearby. A result's `source` field is now the configured
provider's id (e.g. `"osm-overpass"`), not the literal string `"osm"` -
accurate regardless of which plugin actually answered.

### A provider is picked independently of Nearby

`ui.place_name_provider` (`backend/place_name_provider.go`,
`resolvePlaceNameProvider`) selects the plugin, defaulting to
`osm-overpass` - mirroring `resolvePOIProvider`'s idiom exactly, down to
reading settings fresh on every call. It is a separate setting from
`ui.poi_provider`: the two commonly name the same plugin, but an operator
free to change either one independently. An id that names a plugin that
isn't installed, or one that is installed but doesn't export both
`place_name_at` and `search_places`, is a fail-fast error naming the
plugin at fault - never a silent fallback to a different provider or to
the default. `GET /api/place-name-providers` mirrors `GET /api/poi-providers`
but lists only the supporting subset of the registry, since not every
installed POI plugin necessarily answers place-name questions.

### Timing stays inside the existing WASM call budget

`search_places`' worst case - an exact-name rung, a qualifier lookup and a
regex rung, run in sequence - sums to about 13 seconds against a public
Overpass mirror (4s + 3s + 6s per-query server-side timeouts), comfortably
under the 15-second `WASM_PLUGIN_TIMEOUT_MS` every plugin call is already
bound by (`wasm_plugin.go`). Nothing about moving this logic into the
plugin changes that budget; it was already the shape `fetch_poi` operated
under.

### Settings: a new tab, the same modal

Widgets gains a "Place names" tab (`widgets-section.tsx`) alongside
Nearby, using the same `ProviderGroup` component against the new
`/api/place-name-providers` endpoint. Its gear icon opens the existing
`ProviderSettingsModal` for the underlying POI plugin (`type: "poi"`, same
plugin id) rather than a nonexistent "place-names" plugin kind - there is
only one kind of plugin here, so the Overpass server field is reachable
and identical from either tab.

## Rejected

**Reuse `ui.poi_provider` for place names too.** The two are different
jobs with different plugin requirements: a plugin can list nearby
facilities well and still have no sensible answer for "what is this bay
called," and vice versa. Sharing one setting would force a single
compromise plugin choice onto two independent decisions, and would make
`google-places` (a real, useful Nearby provider) an implicit, silent
failure the moment the position tile tried to use it for place names.
Giving place names its own setting, defaulting to the same plugin as
`poi_provider`, costs one extra field and gets the independence back.

**A fourth plugin kind, with its own directory and registry.** A
place-names provider is not a categorically different data source from a
POI provider - it is a POI provider that also knows how to answer two more
questions about a point and a name. Two additional exports on the existing
kind, gated by a capability check, is a smaller, simpler surface than a new
kind, a new `plugins/<kind>/` directory, a new registry file and a new
settings picker that would all otherwise duplicate `poi_providers.go`
almost line for line.

**A reverse-geocoding API (Google's included) instead of Overpass.**
ADR 0056 already found this failure mode once: Nominatim's plain reverse
geocode at the Goldsmith anchorage returns a different island group
entirely, because on water it falls back to the nearest indexed *point*
rather than genuine polygon containment - the same centroid trap that sank
GeoNames before it. Nothing about Google's reverse-geocoding API changes
that shape of problem; it is built for the same "nearest addressed thing"
case Nominatim and GeoNames both are, not for naming an unindexed reef or
anchorage. Overpass's `around:` filter does real geometric filtering
server-side, which is the actual reason this project stayed on it rather
than adopting a reverse-geocoding vendor of any kind.
