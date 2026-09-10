# Google Places POI provider plugin

A Helmcentral points-of-interest plugin backed by Google's
[Places API (New)](https://developers.google.com/maps/documentation/places/web-service/op-overview),
using its Nearby Search endpoint. Unlike `osm-overpass`, this plugin needs an
API key and bills per request - see "Cost" below before switching to it.

Written in TinyGo. The Extism plugin contract isn't TinyGo-specific: Rust,
Zig, C, AssemblyScript, C++, and Haskell PDKs all implement the same contract
identically; see [Extism's PDK list](https://extism.org/docs/concepts/pdk) if
you'd rather use one of those.

## Category coverage is partial

Places models businesses and named attractions, not physical
landform/navigational features, and it has no dedicated boat-fuel, boat-ramp
or dive-shop type. Checked against Google's
[Table A type list](https://developers.google.com/maps/documentation/places/web-service/place-types)
at the time this plugin was written:

| Category | Google type(s) |
| --- | --- |
| marina | `marina` |
| historic | `historical_landmark`, `tourist_attraction` |
| viewpoint | `scenic_spot` |
| trail | `hiking_area`, `park` |
| anchorage, bay, island, fuel, ramp, mooring, dive | no equivalent |

A requested category with no mapping is reported in the response's
`unsupported` list, never silently dropped or mapped onto something
misleading - mapping "fuel" to the generic `gas_station` type, for instance,
would return car filling stations rather than boat fuel docks, which is
worse than reporting no coverage at all. If every requested category is
unsupported, this plugin skips the (billed) API call entirely.

## Request shape

One `POST https://places.googleapis.com/v1/places:searchNearby` per
`fetch_poi` call, with `locationRestriction.circle` set from the requested
position/radius (clamped to Places' documented 50km circle limit) and
`includedTypes` set to the union of Google types for the requested
categories. The FieldMask header is fixed to
`places.id,places.displayName,places.location,places.types,places.editorialSummary,places.googleMapsUri` -
deliberately no photo fields, since Google's photo URLs embed the API key in
a URL the browser would fetch directly, which this plugin's "no photo URLs"
contract rules out.

`detail` comes from `editorialSummary.text` (empty when Google has none for a
place - most places don't) and `source_url` from `googleMapsUri`.

Google's Nearby Search caps results at `maxResultCount` (at most 20) with no
"there were more" signal, the same shape problem `osm-overpass`'s per-category
cap has - see that plugin's README. Places has no per-category caps to
attribute a hit to individually, so every supported requested category is
reported in `truncated` when the flat result count reaches the cap.

## API key

Set `GOOGLE_PLACES_API_KEY` via Helmcentral's secrets store (Settings ->
this provider's card) - the same generic secrets mechanism WeatherKit uses
for its Apple credentials. `google-places.config.json` expands it into this
plugin's `api_key` config value; `google-places.allowed_secrets.json` grants
this specific plugin permission to read it. A missing key fails `fetch_poi`
immediately with a message naming `GOOGLE_PLACES_API_KEY`, the same pattern
WeatherKit uses for its own missing-credential case.

To get a key: create a Google Cloud project, enable the "Places API (New)"
(not the legacy Places API), create an API key under APIs & Services ->
Credentials, and restrict it to the Places API. Nearby Search (New) bills
per request at Google's published rate - check
[Places API (New) pricing](https://developers.google.com/maps/billing-and-pricing/pricing#nearby-search-new)
before enabling this provider, and note it has no free keyless tier the way
`osm-overpass` does.

## Cost

This plugin has no local caching of its own - it relies entirely on the
host's cache (`backend/wasm_poi_provider.go`'s 0.02 degree cell, `ttl_seconds`
= 6h) to bound how often it's actually called. A boat sitting in one place
for a day makes at most one billed request per six hours per distinct
category set requested; a boat under way accumulates a new billed request
roughly every 2km of movement. Budget accordingly.

## Why this plugin is two files

`google-places.go` holds all the request-building/parsing/category-mapping
logic and has no dependency on `github.com/extism/go-pdk`, so `go test ./...`
(below) runs on the plain host Go toolchain with no TinyGo or wasm target
needed. `main.go` holds only the thin `//go:wasmexport` wrapper functions
(including the actual HTTP call and the missing-key check, which need the
PDK) and is gated `//go:build tinygo` so it's excluded from that plain host
build. Because of this split, always build the whole package directory
(`.`), not just `main.go` by name, as described below.

## Building it

Requires only Docker (no local TinyGo install needed), pinned to
`tinygo/tinygo:0.41.1`, the same version pinned for this repo's WASM test
fixtures. From the repo root:

```sh
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:0.41.1 sh -c "
  cd docs/examples/poi-plugins/google-places &&
  go mod tidy &&
  tinygo build -o google-places.wasm -target wasip1 -buildmode c-shared .
"
```

Use the trailing `.` to build the whole package directory. Naming `main.go`
alone would exclude `google-places.go` and fail with `undefined:` errors.

## Installing it

Helmcentral discovers POI-provider plugins by scanning `plugins/poi/` at
startup (overridable via the `PLUGINS_POI_DIR` env var). To install this
plugin manually:

1. Copy the compiled `google-places.wasm` into your `plugins/poi/` directory.
2. Copy `google-places.allowed_hosts.json`, `google-places.config.json` and
   `google-places.allowed_secrets.json` alongside it.
3. Set `GOOGLE_PLACES_API_KEY` via Settings -> Secrets (or the
   `/api/settings/secrets` API directly).
4. Restart the Helmcentral container (or the dev backend), then select
   "Google Places" as the POI provider in Settings.

`plugins/poi/` lives at the repo root and is gitignored, like `backend-data/`,
because it contains operator runtime files.

## Testing

`main_test.go` unit-tests the category mapping, request building and
response parsing directly, on the plain host Go toolchain, with no TinyGo or
WASM runtime needed:

```sh
cd docs/examples/poi-plugins/google-places && go mod tidy && go vet ./... && go test ./...
```

`testdata/places_searchnearby_response.json` is a **hand-written** fixture,
not a live capture - this plugin was built in an environment with no Google
Places API key available, so there was no way to capture a real response.
It is written to match the
[documented Nearby Search (New) response shape](https://developers.google.com/maps/documentation/places/web-service/nearby-search)
as closely as possible, but has not been verified against a live API call.
Anyone adding a real API key should re-capture this fixture from a genuine
response and remove the `_comment` field's warning once it's live data.
