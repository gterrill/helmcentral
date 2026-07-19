# NWS (US National Weather Service) forecast-warnings provider plugin

A reference Helmcentral forecast-warnings-provider plugin backed by the US
National Weather Service's public `api.weather.gov` marine zone alerts API.
It exists to show what wiring up a real government marine-warnings API into
Helmcentral's WASM plugin contract looks like, end to end, and gives real
non-Australian coverage — the region-appropriate alternative to
[../bom](../bom) (the **default**, `id: "bom"`) for US-based installs. See
[docs/adr/0019-ftp-host-function-and-forecast-warnings-provider.md](../../adr/0019-ftp-host-function-and-forecast-warnings-provider.md)
for the "each provider resolves its own zone semantics" design decision
behind the Forecast Warnings contract as a whole.

Unlike `../bom`, this plugin needs no host-function trickery: `api.weather.gov`
is a completely open, keyless, HTTP-reachable API, explicitly documented as
free to use for programmatic/automated purposes — the "normal" case Extism's
built-in `pdk.NewHTTPRequest` HTTP support was already designed for, the same
mechanism [../../tide-plugins/noaa](../../tide-plugins/noaa) uses.

## Why this plugin is (also) two files

`nws.go` holds all the JSON-mapping, event-categorization, cancellation-
filtering, and fetch-orchestration logic, and has no dependency on
`github.com/extism/go-pdk` — the HTTP fetch step is injected as a plain
`func(url string) (status int, body []byte, err error)` rather than called
directly — so `go test ./...` (below) runs on the plain host Go toolchain
with no TinyGo or wasm target needed. `main.go` holds only the thin
`//go:wasmexport` wrapper functions plus the real `pdk.NewHTTPRequest`-based
fetch implementation (including the `User-Agent` header NWS asks for — see
below), and is gated `//go:build tinygo` so it's excluded from that plain
host build (TinyGo defines the `tinygo` build tag automatically; plain
`go test` doesn't) — identical split to [../bom](../bom). Because of this
split, always build the whole package directory (`.`), not just `main.go`
by name.

## User-Agent header

NWS's API usage docs (https://www.weather.gov/documentation/services-web-api)
ask API consumers to identify themselves with a `User-Agent` header — no
fixed format is enforced, just a request for good API citizenship (the API
itself is free, keyless, and explicitly documented as open for
programmatic/automated use, with a "generous" rate limit for normal use).
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
   `GET https://api.weather.gov/zones?type=marine&point=<lat>,<lon>` — a
   GeoJSON FeatureCollection. `features[0].properties.id` is the zone code
   (e.g. `GMZ554`, `AMZ135`), `features[0].properties.name` is the
   human-readable zone name, used as the contract's `region`. An empty
   `features` array means the point isn't inside any NWS marine zone (e.g.
   outside US coastal waters) — not an error, just zero zones and therefore
   zero bulletins, the same "no coverage here" philosophy as BOM's SA/NT
   gap. Only the *first* matching zone is used — there should generally be
   exactly one for a given point; if a point resolves into multiple zones,
   the rest are silently ignored (a documented simplification, not a bug).

2. **Fetch active alerts for that zone**:
   `GET https://api.weather.gov/alerts/active?zone=<zone-id>` — another
   GeoJSON FeatureCollection. Each alert is mapped to exactly one bulletin
   (NWS alerts are point-in-time, not BOM's forward-looking multi-day
   format, so `sections` always has exactly one entry with an empty `day`).
   An alert is skipped (not surfaced) unless `properties.status == "Actual"`
   (excludes `"Test"`/`"Exercise"` alerts) and `properties.messageType !=
   "Cancel"` (excludes cancelled alerts) — mirroring BOM's own
   cancellation-section filtering.

3. **Field mapping** (see `nws.go`'s `alertToBulletin`):
   - `id` = the alert's `properties.id` (a stable `urn:oid:...` string).
   - `title` = `properties.headline`, falling back to `properties.event` if
     the headline is empty.
   - `category` = a short opaque tag derived from `properties.event` (see
     `categorizeNWSEvent`): anything containing "wind"/"gale"/"storm"/
     "craft" → `"wind"`; anything containing "surf"/"swell"/"rip" →
     `"surf"`; otherwise a lowercased, hyphenated slug of the event name
     itself (e.g. `"Special Marine Warning"` → `"special-marine-warning"`).
     This table is intentionally small — NWS defines dozens of alert
     types — not a claim of exhaustive coverage, matching BOM's own
     "wind"/"surf" vocabulary honesty.
   - `issued_at` = `properties.sent`, passed through as-is (already
     RFC3339-with-offset).
   - `details_url` = the alert's own `properties["@id"]` canonical
     `api.weather.gov` link when present (a real, resolvable URL — every
     alert observed live during planning had this populated), falling back
     to `https://alerts.weather.gov/search?zone=<zone-id>` only when it
     isn't.
   - `sections` = `[{"day": "", "warning_type": properties.event}]`.

A genuine network/parse failure on either the zone lookup or the alerts
fetch is a real error (fail-fast, no fabricated data) — this differs from
BOM's "one product fails, others still succeed" partial-failure tolerance
only because this plugin has exactly one zone lookup and one alerts call to
make, not several independent per-product fetches to partially succeed
across.

## Building it

Requires only Docker (no local TinyGo install needed), pinned to
`tinygo/tinygo:0.41.1` — the same version already pinned for this repo's own
WASM test fixtures and for `../bom`. From the repo root:

```sh
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:0.41.1 sh -c "
  cd docs/examples/forecast-warnings-plugins/nws &&
  go mod tidy &&
  tinygo build -o nws.wasm -target wasip1 -buildmode c-shared .
"
```

Note the trailing `.` (build the whole package directory), not `main.go` —
naming `main.go` alone would exclude `nws.go` and fail with `undefined:`
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

   This is not optional — a plugin with no companion
   `<name>.allowed_hosts.json` file gets **no network access at all**
   (Helmcentral's default-deny sandboxing).
3. Restart the Helmcentral container (or the dev backend). "US National
   Weather Service" should now appear in the Forecast Warnings provider
   dropdown in Settings. Set `ui.forecast_warnings_provider: nws` in
   `settings.yaml` to make it the active provider (BOM remains the default
   when unset).

## Testing

`main_test.go` unit-tests event categorization (`categorizeNWSEvent`),
alert reportability filtering (`isReportableAlert` — status/cancellation),
field mapping (`alertToBulletin` — headline/event fallback,
`@id`/search-URL fallback), zone-response and alerts-response JSON→bulletin
mapping (`mapZonesResponse`, `mapAlertsResponse`), and the full
`fetch_warnings` orchestration (`buildFetchWarningsOutput`) against an
injected fake fetcher and realistic fixtures built from the real API shapes
confirmed live during planning — including the "zero marine zone coverage"
and "zero active alerts" non-error paths and the zone-lookup/alerts-lookup
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
