# BOM (Australia) tide provider plugin

A Helmcentral tide-provider plugin backed by the Australian Bureau of
Meteorology's public tide-table pages, covering Australian ports. Unlike
[../noaa](../noaa) (a teaching example backed by a clean JSON API), this is
the real, only implementation of the `"bom"` provider — ported 1:1 from the
formerly-native `backend/tide_provider_bom.go` (now deleted). See
[docs/adr/0017-wasm-plugin-tide-providers.md](../../adr/0017-wasm-plugin-tide-providers.md)'s
"Update: BOM ported to WASM" section for why this port was made and what
tradeoffs were accepted.

Existing `settings.yaml` configs with `tide_provider: bom` (and any
previously-working BOM `tide_station_id` AAC code) keep working unchanged —
this plugin uses the identical station IDs the native provider did, since
it's built from the same embedded station list (`data/bom_tide_sites.json`).

BOM has no public JSON tide API — this plugin scrapes
`https://www.bom.gov.au/australia/tides/scripts/getTidesTable.php` HTML
output with two regexes ported verbatim from the native Go version. If BOM
ever changes that page's markup, this plugin breaks until the regexes are
updated — an accepted tradeoff (debugging a WASM guest is harder than
debugging native Go with normal tooling), not a hidden risk.

Written in TinyGo — see [../noaa/README.md](../noaa/README.md) for notes on
alternative Extism PDK languages.

## Why this plugin is two files

`bom.go` holds all the parsing/filtering logic and has no dependency on
`github.com/extism/go-pdk`, so `go test ./...` (below) runs on the plain
host Go toolchain with no TinyGo or wasm target needed. `main.go` holds only
the thin `//go:wasmexport` wrapper functions and is gated
`//go:build tinygo` so it's excluded from that plain host build (TinyGo
defines the `tinygo` build tag automatically; plain `go test` doesn't).
Because of this split, always build the whole package directory (`.`), not
just `main.go` by name — see below.

## Building it

Requires only Docker (no local TinyGo install needed), pinned to
`tinygo/tinygo:0.41.1` — the same version already pinned for this repo's own
WASM test fixtures (see `backend/wasm_tide_provider_test.go`'s regeneration
comment), to avoid `:latest` drift. From the repo root:

```sh
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:0.41.1 sh -c "
  cd docs/examples/tide-plugins/bom &&
  go mod tidy &&
  tinygo build -o bom.wasm -target wasip1 -buildmode c-shared .
"
```

Note the trailing `.` (build the whole package directory), not `main.go` —
naming `main.go` alone would exclude `bom.go` and fail with `undefined:`
errors, since Go/TinyGo's single/multi-file build mode only compiles the
files explicitly listed.

This produces `bom.wasm` in this directory. Requires network access inside
the container (`go mod tidy` fetches `github.com/extism/go-pdk`) and Docker
with the `tinygo/tinygo` image available locally.

If you'd rather install TinyGo locally instead of using Docker, the
equivalent commands are:

```sh
go mod tidy
tinygo build -o bom.wasm -target wasip1 -buildmode c-shared .
```

Both the BOM and NOAA plugins are also built and installed automatically as
part of Docker Compose startup (see the repo's compose files) — manual
builds via this README are for anyone who wants to build/inspect this
plugin outside that automation.

## Installing it

Helmcentral discovers tide-provider plugins by scanning `plugins/tides/` at
startup (overridable via the `PLUGINS_TIDES_DIR` env var). To install this
plugin manually:

1. Copy the compiled `bom.wasm` into your `plugins/tides/` directory.
2. Create `plugins/tides/bom.allowed_hosts.json` next to it, containing:

   ```json
   ["www.bom.gov.au"]
   ```

   This is not optional — a plugin with no companion
   `<name>.allowed_hosts.json` file gets **no network access at all**
   (Helmcentral's default-deny sandboxing). `www.bom.gov.au` is the only
   host this plugin talks to.
3. Restart the Helmcentral container (or the dev backend). "Bureau of
   Meteorology (Australia)" should now appear in the tide-provider dropdown
   in Settings.

## Testing

`main_test.go` unit-tests the HTML tide-table parsing logic
(`parseBomTidesTable`), the embedded station-list parsing
(`parseBomStations`), and the station search filter (`searchBomStations`)
against fixtures, entirely on the host Go toolchain — no TinyGo or WASM
runtime needed:

```sh
go test ./...
```

## BOM endpoint this plugin uses

- Tide table (HTML, not JSON): `GET https://www.bom.gov.au/australia/tides/scripts/getTidesTable.php?aac=<AAC>&date=<DD-MM-YYYY>&days=8&offset=0&type=tide`

See the comment block at the top of `main.go` for exact parsing details.
