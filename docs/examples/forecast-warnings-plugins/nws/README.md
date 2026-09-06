# NWS (US National Weather Service) forecast-warnings provider plugin

A reference Helmcentral forecast-warnings-provider plugin backed by the US
National Weather Service's public `api.weather.gov` marine zone alerts API.
It demonstrates the WASM plugin contract and provides US coverage as an
alternative to
[../bom](../bom) (the **default**, `id: "bom"`) for US-based installs. See
[docs/adr/0019-ftp-host-function-and-forecast-warnings-provider.md](../../../adr/0019-ftp-host-function-and-forecast-warnings-provider.md)
for the decision to let each provider resolve its own zones.

Unlike `../bom`, this plugin uses HTTP: `api.weather.gov` is a free, keyless
API documented for programmatic access. It uses Extism's built-in
`pdk.NewHTTPRequest` HTTP support, as does
[../../tide-plugins/noaa](../../tide-plugins/noaa).

## Why this plugin is (also) two files

`nws.go` holds all the JSON-mapping, event-categorization, cancellation-
filtering, and fetch-orchestration logic, and has no dependency on
`github.com/extism/go-pdk`. The HTTP fetch step is injected as a plain
`func(url string) (status int, body []byte, err error)` rather than called
directly, so `go test ./...` (below) runs on the plain host Go toolchain
with no TinyGo or wasm target needed. `main.go` holds only the thin
`//go:wasmexport` wrapper functions plus the `pdk.NewHTTPRequest`-based
fetch implementation (including the `User-Agent` header NWS asks for; see
below), and is gated `//go:build tinygo` so it's excluded from that plain
host build (TinyGo defines the `tinygo` build tag automatically; plain
`go test` doesn't). This is the same split as [../bom](../bom). Because of this
split, always build the whole package directory (`.`), not just `main.go`
by name.

## User-Agent header

NWS's API usage docs (https://www.weather.gov/documentation/services-web-api)
ask API consumers to identify themselves with a `User-Agent` header. No fixed
format is enforced. The API is free and keyless, permits programmatic access,
and has a rate limit described by NWS as generous for normal use.
This plugin sets one via go-pdk's `HTTPRequest.SetHeader` (see `main.go`'s
`doHTTPFetch`):

```go
req := pdk.NewHTTPRequest(pdk.MethodGet, url)
req.SetHeader("User-Agent", "(helmcentral, contact-not-provided)")
res := req.Send()
```

## How zone/warning resolution works

Unlike BOM's state-bounding-box + free-text-bulletin-parsing model, NWS
exposes a structured, queryable API:

1. **Resolve the containing marine zone** for the vessel's lat/lon:
  The request `GET https://api.weather.gov/zones?type=marine&point=<lat>,<lon>`
  returns a GeoJSON FeatureCollection. `features[0].properties.id` is the zone code
   (e.g. `GMZ554`, `AMZ135`), `features[0].properties.name` is the
   human-readable zone name, used as the contract's `region`. An empty
   `features` array means the point isn't inside any NWS marine zone (e.g.
   outside US coastal waters). In that case, the plugin returns zero zones
   and zero bulletins without an error, as with BOM's SA/NT coverage gap.
   Only the first matching zone is used. There should generally be
   exactly one for a given point; if a point resolves into multiple zones,
   any additional zones are silently ignored as a documented simplification.

2. **Fetch active alerts for that zone**:
   The request `GET https://api.weather.gov/alerts/active?zone=<zone-id>` returns
   another GeoJSON FeatureCollection. Each alert is mapped to exactly one bulletin
   (NWS alerts are point-in-time, not BOM's forward-looking multi-day
   format, so `sections` always has exactly one entry with an empty `day`).
   An alert is skipped (not surfaced) unless `properties.status == "Actual"`
   (excludes `"Test"`/`"Exercise"` alerts) and `properties.messageType !=
    "Cancel"` (excludes cancelled alerts). This mirrors BOM's cancellation-section
    filtering.

3. **Field mapping** (see `nws.go`'s `alertToBulletin`):
   - `id` = the alert's `properties.id` (a stable `urn:oid:...` string).
   - `title` = `properties.headline`, falling back to `properties.event` if
     the headline is empty.
   - `category` = a short opaque tag derived from `properties.event` (see
     `categorizeNWSEvent`): anything containing "wind"/"gale"/"storm"/
     "craft" → `"wind"`; anything containing "surf"/"swell"/"rip" →
     `"surf"`; otherwise a lowercased, hyphenated slug of the event name
     itself (e.g. `"Special Marine Warning"` → `"special-marine-warning"`).
      NWS defines dozens of alert types, so this table is not exhaustive.
      Its named categories match BOM's "wind"/"surf" vocabulary.
   - `issued_at` = `properties.sent`, passed through as-is (already
     RFC3339-with-offset).
   - `details_url` = the alert's own `properties["@id"]` canonical
      `api.weather.gov` link, when present. Every alert observed live during
      planning had a resolvable URL here. The plugin falls back
     to `https://alerts.weather.gov/search?zone=<zone-id>` only when it
     isn't.
   - `sections` = `[{"day": "", "warning_type": properties.event}]`.

A network or parse failure on either the zone lookup or alerts fetch returns
an error. Unlike BOM's independent per-product fetches, this plugin has one
zone lookup and one alerts call, so it cannot return partial results if
either fails.

## Building it

Requires only Docker (no local TinyGo install needed), pinned to
`tinygo/tinygo:0.41.1`, the same version pinned for this repo's
WASM test fixtures and for `../bom`. From the repo root:

```sh
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:0.41.1 sh -c "
  cd docs/examples/forecast-warnings-plugins/nws &&
  go mod tidy &&
  tinygo build -o nws.wasm -target wasip1 -buildmode c-shared .
"
```

Use the trailing `.` to build the whole package directory. Naming `main.go`
alone would exclude `nws.go` and fail with `undefined:`
errors, since Go/TinyGo's single/multi-file build mode only compiles the
files explicitly listed.

This produces `nws.wasm` in this directory. Requires network access inside
the container (`go mod tidy` fetches `github.com/extism/go-pdk`) and Docker
with the `tinygo/tinygo` image available locally.

If you'd rather install TinyGo locally instead of using Docker, the
equivalent commands are:

```sh
go mod tidy
tinygo build -o nws.wasm -target wasip1 -buildmode c-shared .
```

## Installing it

Helmcentral discovers forecast-warnings-provider plugins by scanning
`plugins/forecast-warnings/` at startup (overridable via the
`PLUGINS_FORECAST_WARNINGS_DIR` env var). To install this plugin manually:

1. Copy the compiled `nws.wasm` into your `plugins/forecast-warnings/`
   directory.
2. Create `plugins/forecast-warnings/nws.allowed_hosts.json` next to it,
   containing:

   ```json
   ["api.weather.gov"]
   ```

    Network access requires this companion `<name>.allowed_hosts.json` file.
    Without it, Helmcentral's default-deny sandbox blocks all network access.
3. Restart the Helmcentral container (or the dev backend). "US National
   Weather Service" should now appear in the Forecast Warnings provider
   dropdown in Settings. Set `ui.forecast_warnings_provider: nws` in
   `settings.yaml` to make it the active provider (BOM remains the default
   when unset).

## Testing

`main_test.go` unit-tests event categorization (`categorizeNWSEvent`),
alert reportability filtering (`isReportableAlert`: status/cancellation),
field mapping (`alertToBulletin`: headline/event fallback,
`@id`/search-URL fallback), zone-response and alerts-response JSON→bulletin
mapping (`mapZonesResponse`, `mapAlertsResponse`), and the full
`fetch_warnings` orchestration (`buildFetchWarningsOutput`) against an
injected fake fetcher and realistic fixtures built from the real API shapes
confirmed live during planning, including the zero marine zone coverage
and zero active alerts non-error paths and the zone-lookup/alerts-lookup
network-error and non-2xx-status error paths. All of it runs on the plain
host Go toolchain, no TinyGo or WASM runtime needed:

```sh
go test ./...
```

## NWS endpoints this plugin uses

- Marine zone lookup by point (GeoJSON):
  `GET https://api.weather.gov/zones?type=marine&point=<lat>,<lon>`
- Active alerts for a zone (GeoJSON):
  `GET https://api.weather.gov/alerts/active?zone=<zone-id>`

Both are free, keyless, and part of NWS's public API
(https://www.weather.gov/documentation/services-web-api). See the comment
blocks at the top of `main.go` and `nws.go` for exact response shapes and
field mappings.
