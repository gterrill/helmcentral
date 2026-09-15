# OpenStreetMap (Overpass) POI provider plugin

A Helmcentral points-of-interest plugin backed by OpenStreetMap data, fetched
live from the public [Overpass API](https://overpass-api.de/). It is the
default POI provider for fresh Helmcentral installs because it needs no API
key or config.json.

The plugin answers one `fetch_poi` call with one Overpass request covering
every category the caller asked for - anchorages, bays, islands, marinas,
fuel docks, boat ramps, moorings, historic landmarks, viewpoints, dive spots
and walking trails. See
[docs/reference/poi-categories.md](../../../reference/poi-categories.md) for the
full tag table and its OpenStreetMap coverage caveats.

Written in TinyGo. The Extism plugin contract isn't TinyGo-specific: Rust,
Zig, C, AssemblyScript, C++, and Haskell PDKs all implement the same contract
identically; see [Extism's PDK list](https://extism.org/docs/concepts/pdk) if
you'd rather use one of those.

## One request, one named set per category

Overpass QL lets a single query build several independent named sets and
`out` each with its own element cap, so this plugin issues exactly one HTTP
POST per `fetch_poi` call regardless of how many categories were requested -
see `buildOverpassPOIQuery` in `osm-overpass.go`. Each category's cap comes
straight from the table in `docs/reference/poi-categories.md`.

The query header also carries a global `[bbox:...]` setting sized to enclose
every category's `around:` circle (`overpassBoundingBox` in
`osm-overpass.go`), omitted only when that box would have to cross the
antimeridian or a pole. Some mirrors, including `overpass.openstreetmap.fr`,
plan the key-only and regex tag clauses (historic, dive) by scanning the
whole database before applying their own `around:` filter; the bbox gives
the planner a spatial index to start from and cut that scan down to size,
without changing which elements the `around:` filters actually select.

Overpass's plain JSON output doesn't record which named set produced a given
element, so every returned element is reclassified from its own tags
afterwards (`classifyElement`), walking the categories in a fixed precedence
order for the rare case where an element's tags happen to satisfy more than
one requested category.

A category marked "named only" in the reference table has its `["name"]`
requirement baked directly into the Overpass clause text, not enforced as a
separate check afterward - Overpass simply never returns an unnamed element
for that clause. Historic is the one exception with a per-clause split: the
`historic`/`heritage` clauses require a name, but `man_made=lighthouse` does
not, since a lighthouse is identifiable by its light characteristic even
unnamed.

## Two further exports: place_name_at and search_places

`fetch_poi` is not this plugin's only export. Two more are OPTIONAL - a POI
provider plugin without them still works and is simply not offered as a
place-names source - but when both are present, the Helmcentral backend
calls them directly for place-name resolution (the position tile, the
anchor pin) and Mate's `find_places` tool, exactly the way `fetch_poi`
already works for Nearby. There is no backend Overpass client of its own
any more: this plugin is the only place that speaks Overpass QL, HTTP and
response parsing for either job (see
[ADR 0101](../../../adr/0101-place-names-come-from-a-plugin.md)).
`place_name_at` carries forward the ring query and ranking place-name
resolution always used, and `search_places` carries forward `find_places`'
two-rung name-search ladder. See `osm-overpass.go`'s
`place_name_at` and `search_places` sections for the implementation; both
are exercised directly by `go test` via an injected `overpassQueryFunc`, the
same host-testable/pdk-only split `fetch_poi` already uses (see "Why this
plugin is two files" below).

### place_name_at

Input `{"lat": <number>, "lon": <number>, "radius_m": <integer>}`, output
`{"name": <string>, "kind": <string>, "lat": <number>, "lon": <number>}`, or
exactly `{"name": ""}` when nothing named is in range - a real, negative
answer, never an error.

One Overpass ring query at the given radius, matching the same three tag
clauses `backend/place_name.go`'s `buildOverpassQuery` uses -
`seamark:type=anchorage`, `natural=bay`, `place` in
`island`/`islet`/`rock` - with this plugin's own global `[bbox:...]` setting
(`overpassBoundingBox`) added when the ring doesn't cross the antimeridian
or a pole, and a 12s server-side timeout (the same budget
`buildOverpassPOIQuery` uses, since this is likewise a single Overpass round
trip per call). Unlike `place_name.go`, this export does not itself walk a
widening ladder of rings: the host passes one radius per call and calls
again with a wider one if this one comes back unnamed, exactly as
`place_name.go`'s own `resolvePlaceName` does today against its three-rung
ladder (400m, 1500m, 5000m).

The winner is ranked exactly as `backend/place_name.go`'s
`bestNamedFeature` does: anchorage first (a human already decided this is
where you anchor), then bay, then island/islet/rock by descending
size-implication, ties broken by great-circle distance to the query point.
An element with no name tag, or whose tags match none of the five
recognised kinds, is discarded before ranking.

### search_places

Input
`{"query": <string>, "lat": <number>, "lon": <number>, "max_results": <integer>, "broad": <boolean>}`,
output
`{"search": "exact"|"regex"|"none", "radius_nm": <number>, "centred_on": {"name": <string>, "lat": <number>, "lon": <number>} | null, "results": [{"name": <string>, "kind": <string>, "lat": <number>, "lon": <number>}], "note": <string>}`.
`max_results` is accepted but deliberately unused: like `fetch_poi`'s
features, `search_places` returns raw matches (capped at 50) and leaves
distance, bearing, dedupe, sort order and the final trim to `max_results` to
the host (ADR 0091's "provider returns raw features, host computes
distance/bearing" rule) - trimming here, before the host's own distance
sort, could silently drop the actual nearest match.

Rung 1 always runs: an untagged, exact `nwr["name"="<variant>"]` match over
a 100nm box, tried against the query as typed, Title Case, first-letter-only
capitalised, and - for a query carrying a comma-qualifier ("Bona Bay,
Gloucester Island") - the same three variants of just the head before the
comma too, since OSM's own name tag almost never carries the qualifier
(`findPlacesExactNameVariants`, mirroring
`backend/assistant_tools.go`'s `assistantExactNameVariants`).

Rung 2 - a case-insensitive partial-name regex over a tighter 20nm box,
matching natural coastal features, place types, seamark facilities, and
OSM's separate `leisure=marina` tagging - only runs when rung 1 found
nothing **and** the caller's `broad` is true. The host decides `broad`, not
this plugin: it knows about local data this plugin cannot see (a saved
route waypoint, most importantly) and folds that into whether a broader OSM
search is worth running at all, mirroring
`backend/assistant_tools.go`'s `executeFindPlaces` skipping rung 2 whenever
a waypoint already answered the query. A comma-qualifier gets one extra
exact-name lookup for the qualifier before rung 2 runs, re-centring rung 2's
box on the qualifier's resolved position (`centred_on` in the output) when
it sits well outside the ordinary centre - a bay named for the island it is
on, tens of miles away. When the qualifier is present but does not resolve,
`centred_on` stays `null` and `note` says so; `note` is otherwise empty.

Kind is derived host-side (`findPlacesKind`) from tag priority -
`seamark:type`, then `natural`, then `place`, then `leisure` - falling back
to `"feature"` rather than discarding the match, since an exact-name hit
carries no tag filter and can genuinely have none of the four.

Three separate Overpass round trips can happen in one call (rung 1, the
qualifier lookup, rung 2), so each gets its own tight server-side timeout
rather than one shared budget: 4s, 3s and 6s respectively, summing to 13s -
comfortably under the host's 15s WASM plugin call budget
(`backend/wasm_plugin.go`'s `WASM_PLUGIN_TIMEOUT_MS`), leaving margin for
three separate HTTP connections and JSON parsing on top of whatever
Overpass itself takes. Measured live against `overpass.openstreetmap.fr` on
2026-09-11 (`backend/assistant_tools.go`, ADR 0093 section 8): an exact-name
match answers in about 1s even at the full 100nm rung 1 radius (Overpass
uses its name index directly), and the rung 2 regex union answers in 2 to
5s at 20nm (not indexable, a full scan of the box).

## Truncation is a heuristic, not an exact count

Overpass's `out ... <cap>` statement silently drops anything past the cap -
the JSON response carries no "there were N more" signal. This plugin reports
a category as truncated when its returned count lands exactly on the cap.
That is a real, if imperfect, signal: a genuine result that happens to total
exactly the cap reads as truncated too, which is the safer of the two
possible mistakes here. A precise count would need a second `out count;`
statement per category, which conflicts with the "one Overpass request per
call" goal that keeps this plugin polite to a shared public API.

## Rate limiting

Overpass signals its rate limit with an HTTP 200 response carrying an HTML
body instead of JSON, not a 429. `looksLikeOverpassRateLimit` (mirroring
`backend/place_name.go`'s function of the same name, which the same
investigation into Overpass's real behaviour originally produced) detects
this from the response's Content-Type header and, as a fallback, the
`rate_limited` substring the HTML body carries. A detected rate limit is
returned as an explicit error from `fetch_poi` - never as an empty feature
list, which the host would otherwise be unable to tell apart from a
genuinely quiet patch of water.

Overpass also sometimes returns an HTTP 200 with valid JSON but a top-level
`remark` field signaling a server-side query failure (e.g., "runtime error:
Query timed out in \"query\" at line 8 after 22 seconds."). Runtime error
remarks are detected and returned as explicit errors; informational remarks
(those not starting with "runtime error") are ignored and the response
elements are parsed normally.

If the public `overpass-api.de` instance is unreachable or persistently rate
limiting your boat's connection, point this plugin at a different Overpass
mirror instead - see "Pointing at an Overpass mirror" below.

## Pointing at an Overpass mirror

By default this plugin queries the public `overpass-api.de` instance
(`defaultOverpassAPIURL` in `osm-overpass.go`). The mirror is this plugin's
own setting, not a Helmcentral-wide one: it ships a companion
`osm-overpass.config_fields.json` sidecar declaring one operator-editable
field,

```jsonc
// osm-overpass.config_fields.json
[
  {
    "key": "overpass_url",
    "label": "Overpass server",
    "type": "url",
    "placeholder": "https://overpass-api.de/api/interpreter",
    "help": "Blank uses the public overpass-api.de. Use a mirror such as https://overpass.openstreetmap.fr/api/interpreter if your network refuses it. Any mirror other than those two must also be added to this plugin's allowed hosts."
  }
]
```

which the Settings UI reads to render an **Overpass server** field in this
plugin's own settings modal (**Settings -> Widgets -> Nearby**, the gear icon
on the OpenStreetMap provider card). Saving that field posts to
`POST /api/plugins/poi/osm-overpass/config`; the host resolves the stored
value fresh on every `fetch_poi` call (`wasm_plugin.go`'s
`applyConfigValues`), so the change takes effect on the very next call with
no plugin reload or Helmcentral restart. Leaving it blank stores nothing,
and the plugin falls back to its `overpass-api.de` default.

`resolveOverpassURL` (`osm-overpass.go`) requires the stored value to parse
as an absolute `https://` URL. A present-but-malformed value fails the
`fetch_poi` call outright, naming the `overpass_url` key in the error - it
never falls back to the default silently, since a broken setting almost
certainly wasn't meant to keep querying overpass-api.de. The Settings API
already rejects a non-URL at save time (`postPluginConfigHandler` validates
any `"url"`-typed field generically); a plugin-side malformed value is only
reachable by editing the plugin overrides database directly.

**Setting this does not by itself grant network access to the new host.**
The Extism sandbox's network allowlist is enforced from
`osm-overpass.allowed_hosts.json` (or the Settings allowlist override,
[ADR 0024](../../../adr/0024-plugin-descriptions-and-allowlist-overrides.md)),
independently of this config value - and unlike the Overpass server field,
an allowlist override only takes effect after a restart (ADR 0024).
`overpass.openstreetmap.fr` is pre-allowlisted alongside `overpass-api.de`:

```jsonc
// osm-overpass.allowed_hosts.json
["overpass-api.de", "overpass.openstreetmap.fr", "en.wikipedia.org"]
```

Point the Overpass server field at any other mirror and its host still needs
adding to the allowlist (this file, or the Settings allowlist override), or
the request fails at the sandbox boundary instead.

Place-name resolution (the position tile, the anchor pin) and Mate's
`find_places` tool read this same stored value too, when
`ui.place_name_provider` names this plugin - there is no separate backend
shim or setting for it any more. See
[ADR 0100](../../../adr/0100-plugins-declare-their-own-settings.md) for how
the setting itself works and
[ADR 0101](../../../adr/0101-place-names-come-from-a-plugin.md) for how
place names ended up calling into this plugin directly via
`place_name_at`/`search_places` (see above).

## Wikipedia enrichment

After classification, the nearest `detail_limit` features (config, default
5) carrying an OSM `wikipedia` tag are enriched with the first sentence of
the [English Wikipedia REST API](https://en.wikipedia.org/api/rest_v1/)'s
page summary. `detail_limit` bounds this deliberately: five extra HTTP round
trips inside the plugin's 15-second timeout budget is comfortable, and most
POI requests only carry a couple of Wikipedia-tagged features regardless (see
`docs/reference/poi-categories.md`'s coverage notes).

Only English Wikipedia is ever queried, regardless of the tag's language
prefix (`en:Lindeman Island`, `fr:...`, or no prefix at all) - many article
titles resolve identically across languages, and when a title doesn't
resolve on English Wikipedia the fetch simply fails, leaving `detail` and
`source_url` empty rather than a placeholder. This is why
`osm-overpass.allowed_hosts.json` lists only `en.wikipedia.org`, never a
`*.wikipedia.org` wildcard.

`firstSentence` is a plain heuristic (cut at the first `". "`), not a real
sentence splitter - it will cut early on an abbreviation like "St.". Accepted
because a full NLP sentence boundary detector is far more machinery than a
one-line POI description warrants.

## Why this plugin is two files

`osm-overpass.go` holds all the query-building/parsing/classification logic
and has no dependency on `github.com/extism/go-pdk`, so `go test ./...`
(below) runs on the plain host Go toolchain with no TinyGo or wasm target
needed. `main.go` holds only the thin `//go:wasmexport` wrapper functions
(including the actual HTTP calls, which need the PDK) and is gated
`//go:build tinygo` so it's excluded from that plain host build. Because of
this split, always build the whole package directory (`.`), not just
`main.go` by name, as described below.

## Building it

Requires only Docker (no local TinyGo install needed), pinned to
`tinygo/tinygo:0.41.1`, the same version pinned for this repo's WASM test
fixtures. From the repo root:

```sh
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:0.41.1 sh -c "
  cd docs/examples/poi-plugins/osm-overpass &&
  go mod tidy &&
  tinygo build -o osm-overpass.wasm -target wasip1 -buildmode c-shared .
"
```

Use the trailing `.` to build the whole package directory. Naming `main.go`
alone would exclude `osm-overpass.go` and fail with `undefined:` errors.

## Installing it

Helmcentral discovers POI-provider plugins by scanning `plugins/poi/` at
startup (overridable via the `PLUGINS_POI_DIR` env var). To install this
plugin manually:

1. Copy the compiled `osm-overpass.wasm` into your `plugins/poi/` directory.
2. Copy `osm-overpass.allowed_hosts.json` alongside it. This file is required
   for network access - a plugin with no companion `<name>.allowed_hosts.json`
   file gets no network access (Helmcentral's default-deny sandboxing).
3. Restart the Helmcentral container (or the dev backend). "OpenStreetMap
   (Overpass)" should now appear in the POI-provider dropdown in Settings,
   with no frontend changes required.

`plugins/poi/` lives at the repo root and is gitignored, like `backend-data/`,
because it contains operator runtime files.

## Testing

`main_test.go` unit-tests the query building, classification, truncation
detection, rate-limit detection and Wikipedia enrichment logic directly, on
the plain host Go toolchain, with no TinyGo or WASM runtime needed.
`place_search_test.go` covers `place_name_at` and `search_places` the same
way - query building, ranking, variants, kind labels and the two-rung
orchestration, each exercised through a fake `overpassQueryFunc` rather than
a real HTTP call. `place_search_fixtures_test.go` runs that same parsing and
ranking logic against the live captures listed below:

```sh
cd docs/examples/poi-plugins/osm-overpass && go mod tidy && go vet ./... && go test ./...
```

## Fixtures

`testdata/overpass_poi_lindeman_9260m.json` and
`testdata/overpass_poi_airlie_5556m.json` are live captures of this plugin's
exact query shape, at the coordinates `backend/place_name_test.go` already
uses for Lindeman Island (`-20.4467, 149.0353`, 5nm/9260m) and at Airlie
Beach (`-20.2675, 148.7176`, 3nm/5556m). `testdata/wikipedia_summary_lindeman_island.json`
is a live capture of the English Wikipedia REST summary for Lindeman Island.

The Lindeman fixture covers all eleven categories. The Airlie fixture is
**partial**: bay, island, mooring, historic, viewpoint and dive were
captured live; anchorage, marina, fuel, ramp and trail could not be, because
the mirror used to capture it (see below) rate-limited or timed out on
those specific category queries across several retries in the same session,
while the same categories succeeded moments apart for the Lindeman capture.
The fixture's own `_comment` and `_missing_categories` fields record exactly
which categories are absent and why - every element present is still
genuine live data, never backfilled or invented. Recapturing the missing
categories against a healthier Overpass endpoint (ideally the real
`overpass-api.de`) would improve this fixture's coverage; it is not required
for the test suite to pass, since `TestLiveFixture_AirlieBeachReturnsFeatures`
only asserts that live capture produced at least one real feature.

These were captured against `overpass.kumi.systems`, a public Overpass
mirror serving the same OpenStreetMap database, rather than
`overpass-api.de` directly - the sandbox this plugin was developed in could
not route to `overpass-api.de`'s IP addresses. The plugin itself, and its
`allowed_hosts.json`, still target `overpass-api.de`; the mirror was a
capture-environment workaround only, not a design decision.

`place_name_at`'s and `search_places`' fixtures were all captured live
against `overpass.openstreetmap.fr` on 2026-09-16, requests spaced about 4s
apart (the mirror returns 503 on a tighter burst):

- `testdata/overpass_place_name_lindeman_1500m.json` -
  `place_name_at`'s 1500m ring at Lindeman Island (`-20.4467, 149.0353`).
  200 OK in 1.3s, 1 element (Lindeman Island itself, `place=island`).
- `testdata/overpass_place_name_lindeman_5000m.json` -
  the same position's 5000m ring. 200 OK in 0.66s, 13 elements (12 named, 1
  unnamed islet) - the same count `backend/place_name_test.go`'s own
  Lindeman fixture carries, real OSM data captured independently. Turtle Bay
  wins the ranking here (nearest of three bays in range).
- `testdata/overpass_search_places_exact_hill_inlet.json` -
  `search_places` rung 1, exact name "Hill Inlet", centred on Lindeman
  Island. 200 OK in 0.45s, 1 element.
- `testdata/overpass_search_places_exact_whitehaven_beach.json` -
  rung 1, exact name "Whitehaven Beach", same centre. 200 OK in 0.45s, 2
  elements (a node tagged only `tourism=camp_site` - exercising the
  `findPlacesKind` "feature" fallback - and a way tagged `natural=beach`).
- `testdata/overpass_search_places_exact_whitehaven_partial.json` and
  `testdata/overpass_search_places_regex_whitehaven_partial.json` - the
  rung 2 exercise: rung 1's exact-name variants of the lowercase partial
  "whitehaven" (200 OK in 0.41s, genuinely 0 elements live, since the real
  tag is "Whitehaven Beach"), then rung 2's case-insensitive regex for the
  same query (200 OK in 1.2s, 3 elements: "Whitehaven Bay", "South
  Whitehaven Beach", and "Whitehaven Beach" again).

## Endpoints this plugin uses

1. POI data: `POST https://overpass-api.de/api/interpreter` (or the
   `overpass_url` override - see "Pointing at an Overpass mirror" above) with
   the query Overpass QL body built by `buildOverpassPOIQuery`
   (`fetch_poi`), `buildPlaceNameQuery` (`place_name_at`), or
   `buildFindPlacesExactQuery`/`buildFindPlacesRegexQuery` (`search_places`)
   - all three exports share the same endpoint resolution and POST handling
   (`doOverpassQuery` in `main.go`).
2. Wikipedia enrichment: `GET https://en.wikipedia.org/api/rest_v1/page/summary/<Title>`.

See [Overpass QL's documentation](https://wiki.openstreetmap.org/wiki/Overpass_API/Overpass_QL)
and the [Wikipedia REST API documentation](https://en.wikipedia.org/api/rest_v1/) for
full details.
