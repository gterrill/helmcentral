# BOM (Australia) forecast-warnings provider plugin

A Helmcentral forecast-warnings-provider plugin backed by the Australian
Bureau of Meteorology's public marine wind and hazardous surf warning
bulletins. This is the **default** (`id: "bom"`) implementation of the
pluggable Forecast Warnings provider contract, ported 1:1 from the
formerly-hardcoded `backend/bom_marine_warnings.go` +
`backend/bom_marine_zones.go` (still present in the backend until a later
phase deletes them, now that this plugin replaces their function). See
[docs/adr/0019-ftp-host-function-and-forecast-warnings-provider.md](../../adr/0019-ftp-host-function-and-forecast-warnings-provider.md)
for the full rationale behind this plugin's unusual network-access model
(below) and the "each provider resolves its own zone semantics" design
decision behind the Forecast Warnings contract as a whole.

## Why this plugin is unusual: it uses a custom `ftp_fetch` host function, not HTTP

Every other WASM plugin in this repo (tide, weather, wave) reaches the
network via Extism's built-in HTTP support (`pdk.NewHTTPRequest`). This
plugin can't: BOM's marine warning bulletins are only reliably available
over **anonymous FTP** (`ftp.bom.gov.au`). BOM's own HTTP surface actively
detects and blocks automated access to the same product text — a direct,
unspoofed request to `https://www.bom.gov.au/fwo/IDQ20085.txt` returns a
`403` with an explicit "does not support web scraping" message. Anonymous
FTP is BOM's own sanctioned public channel for this data; HTTP scraping is
not.

A TinyGo WASM guest can't open a raw FTP socket itself, so Helmcentral added
a second, generic custom host function — `ftp_fetch`
(`backend/wasm_ftp_fetch.go`) — available to every plugin type via
`newWasmPluginBase`, not special-cased to this plugin. This plugin calls it
via a hand-written `//go:wasmimport extism:host/user ftp_fetch` declaration
(see `main.go`'s `fetchOverFTP` — the same calling convention proven by
`backend/testdata/wasm_plugins/src/ftpfetch/main.go`), instead of
`pdk.NewHTTPRequest`. The request/response contract is:

```
request:  {"host": "ftp.bom.gov.au:21", "path": "/anon/gen/fwo/IDQ20085.txt"}
response: {"body": "<file contents as text>", "error": ""}
```

Host allowlisting still applies the same way it does for HTTP: this
plugin's companion `bom.allowed_hosts.json` (`["ftp.bom.gov.au"]`) is
enforced against the FTP host too — a disallowed host is refused, not
silently permitted, just via the same `manifest.AllowedHosts` file every
other plugin already has.

## Why this plugin is (also) three files

`bom_zones.go` and `bom.go` hold all the state/zone resolution, parsing,
filtering, and fetch-orchestration logic, and have no dependency on
`github.com/extism/go-pdk` — the FTP fetch step is injected as a plain
`func(host, path string) (string, error)` rather than called directly — so
`go test ./...` (below) runs on the plain host Go toolchain with no TinyGo
or wasm target needed. `main.go` holds only the thin `//go:wasmexport`
wrapper functions plus the `ftp_fetch` import/glue, and is gated
`//go:build tinygo` so it's excluded from that plain host build (TinyGo
defines the `tinygo` build tag automatically; plain `go test` doesn't) —
identical split to [../../tide-plugins/bom](../../tide-plugins/bom).
Because of this split, always build the whole package directory (`.`), not
just `main.go` by name.

## Known coverage gaps (carried forward, not fixed by this port)

This is a faithful port, including the native implementation's existing,
documented limitations — not something this plugin's job was to fix:

- **South Australia and the Northern Territory are entirely unmapped.**
  `stateForPosition` resolves lat/lon into SA/NT correctly, but no BOM
  marine wind/surf warning product ID has been found for either state yet
  (`bomMarineWarningProducts` in `bom.go` has no entry for them), so
  `fetch_warnings` always returns zero bulletins for a vessel there — not
  an error, just nothing to report.
- **Tasmania and Western Australia have registered product IDs but no zone
  tables.** `tasZoneForPosition`/`waZoneForPosition` (`bom_zones.go`) are
  stubs that always return `("", false)`. This plugin still fetches and
  parses both states' bulletins (a TAS/WA vessel isn't skipped), but since
  every section is filtered against the *matched zone* and no zone is ever
  matched for TAS/WA, no bulletin ever survives filtering — `fetch_warnings`
  returns zero bulletins for those states too, today.
- **VIC and TAS's zone bands are approximate**, not verified against BOM's
  official marine zone maps the way QLD's and NSW's are (see the comments
  above `vicZoneForPosition`/`qldZoneForPosition` in `bom_zones.go`) — only
  VIC's "East Gippsland Coast" boundary was confirmed live during the
  original research that produced these tables.

If you have verified BOM zone names/boundaries for SA, NT, TAS, or WA, the
fix is a self-contained table addition in `bom_zones.go` (and a
`bomMarineWarningProducts` entry, for SA/NT) — no other file needs to
change.

## Building it

Requires only Docker (no local TinyGo install needed), pinned to
`tinygo/tinygo:0.41.1` — the same version already pinned for this repo's own
WASM test fixtures. From the repo root:

```sh
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:0.41.1 sh -c "
  cd docs/examples/forecast-warnings-plugins/bom &&
  go mod tidy &&
  tinygo build -o bom.wasm -target wasip1 -buildmode c-shared .
"
```

Note the trailing `.` (build the whole package directory), not `main.go` —
naming `main.go` alone would exclude `bom.go`/`bom_zones.go` and fail with
`undefined:` errors, since Go/TinyGo's single/multi-file build mode only
compiles the files explicitly listed.

This produces `bom.wasm` in this directory. Requires network access inside
the container (`go mod tidy` fetches `github.com/extism/go-pdk`) and Docker
with the `tinygo/tinygo` image available locally.

If you'd rather install TinyGo locally instead of using Docker, the
equivalent commands are:

```sh
go mod tidy
tinygo build -o bom.wasm -target wasip1 -buildmode c-shared .
```

## Installing it

Helmcentral discovers forecast-warnings-provider plugins by scanning
`plugins/forecast-warnings/` at startup (overridable via the
`PLUGINS_FORECAST_WARNINGS_DIR` env var). To install this plugin manually:

1. Copy the compiled `bom.wasm` into your `plugins/forecast-warnings/`
   directory.
2. Create `plugins/forecast-warnings/bom.allowed_hosts.json` next to it,
   containing:

   ```json
   ["ftp.bom.gov.au"]
   ```

   This is not optional — a plugin with no companion
   `<name>.allowed_hosts.json` file gets **no network access at all**
   (Helmcentral's default-deny sandboxing), and that denial applies to
   `ftp_fetch` exactly the same way it applies to HTTP.
3. Restart the Helmcentral container (or the dev backend). "Bureau of
   Meteorology (Australia)" should now appear in the Forecast Warnings
   provider dropdown in Settings — it's also the default when
   `ui.forecast_warnings_provider` is unset.

## Testing

`main_test.go` unit-tests state/zone resolution (`stateForPosition`,
`qldZoneForPosition`, and the TAS/WA/SA/NT gaps above), bulletin text
parsing (`parseBomMarineWarningText`, active-vs-cancelled section
detection) against real bulletin text captured live from BOM's FTP mirror,
and the full `fetch_warnings` orchestration (`buildFetchWarningsOutput`)
against an injected fake fetcher — including the partial-failure ("one
product fails, the other succeeds → no error, fewer bulletins") and
total-failure ("every applicable product fails → error") paths. All of it
runs on the plain host Go toolchain, no TinyGo or WASM runtime needed:

```sh
go test ./...
```

## BOM endpoint this plugin uses

- Marine warning products (anonymous FTP, not HTTP):
  `RETR /anon/gen/fwo/<PRODUCT_ID>.txt` on `ftp.bom.gov.au:21`, anonymous
  login. Product IDs per state are listed in `bomMarineWarningProducts`
  (`bom.go`) — a "marine wind warning summary" and (where available) a
  "hazardous surf warning" product per state.
- Per-warning deep link (for `details_url`, HTTP, just a link — never
  fetched by this plugin): `https://www.bom.gov.au/warning/<slug>/<PRODUCT_ID>`,
  e.g. `https://www.bom.gov.au/warning/marine-wind-warning/IDQ20085`.

See the comment blocks at the top of `main.go` and `bom.go` for exact
parsing details.
