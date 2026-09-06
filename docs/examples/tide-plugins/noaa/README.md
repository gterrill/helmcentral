# NOAA CO-OPS tide provider plugin (reference example)

A reference Helmcentral tide-provider plugin backed by NOAA's public
CO-OPS (Center for Operational Oceanographic Products and Services) Tides
& Currents API, covering US stations. It demonstrates Helmcentral's WASM
plugin contract and can be adapted to another region's tide source by
changing the URLs and field mappings.

It is not a production-hardened client: it has no pagination or retry/backoff
(see the comments at the top of `main.go`). It is separate from
[../bom](../bom), the sole "bom" provider, ported from the formerly-native
`backend/tide_provider_bom.go`. See
[docs/adr/0017-wasm-plugin-tide-providers.md](../../../adr/0017-wasm-plugin-tide-providers.md)'s
"Update: BOM ported to WASM" section for why that port was made.

Written in TinyGo. The Extism plugin contract
isn't TinyGo-specific: Rust, Zig,
C, AssemblyScript, C++, and Haskell PDKs all implement the same contract
identically; see [Extism's PDK list](https://extism.org/docs/concepts/pdk)
if you'd rather use one of those.

## Building it

Requires only Docker (no local TinyGo install needed). This uses the
same build pattern as Helmcentral's test fixtures
(`backend/wasm_tide_provider_test.go`'s regeneration comment), pointed at
this directory instead. From the repo root:

```sh
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:latest sh -c "
  cd docs/examples/tide-plugins/noaa &&
  go mod tidy &&
  tinygo build -o noaa.wasm -target wasip1 -buildmode c-shared main.go
"
```

This produces `noaa.wasm` in this directory. Requires network access
inside the container (`go mod tidy` fetches `github.com/extism/go-pdk`) and
Docker with the `tinygo/tinygo` image available locally (first pull is
~350MB).

If you'd rather install TinyGo locally instead of using Docker, the
equivalent commands are:

```sh
go mod tidy
tinygo build -o noaa.wasm -target wasip1 -buildmode c-shared main.go
```

## Installing it

Helmcentral discovers tide-provider plugins by scanning `plugins/tides/`
at startup (overridable via the `PLUGINS_TIDES_DIR` env var). To install
this plugin:

1. Copy the compiled `noaa.wasm` into your `plugins/tides/` directory.
2. Create `plugins/tides/noaa.allowed_hosts.json` next to it, containing:

   ```json
   ["api.tidesandcurrents.noaa.gov"]
   ```

   This file is required for network access. A plugin with no companion
   `<name>.allowed_hosts.json` file gets no network access (Helmcentral's default-deny
   sandboxing). NOAA CO-OPS is the only host this plugin talks to, so it's
   the only host that needs to be allowlisted.
3. Restart the Helmcentral container (or the dev backend). "NOAA CO-OPS"
   should now appear in the tide-provider dropdown in Settings, with no
   frontend changes required.

`plugins/tides/` lives at the repo root and is gitignored, like `backend-data/`,
because it contains operator runtime files. A fresh Helmcentral checkout
ships with no plugins active by default, to avoid contacting external
government APIs before a provider is chosen.

## NOAA endpoints this plugin uses

- Station search: `GET https://api.tidesandcurrents.noaa.gov/mdapi/prod/webapi/stations.json?type=tidepredictions`
- Single station metadata: `GET https://api.tidesandcurrents.noaa.gov/mdapi/prod/webapi/stations/<id>.json`
- Tide predictions (high/low only): `GET https://api.tidesandcurrents.noaa.gov/api/prod/datagetter?product=predictions&datum=MLLW&interval=hilo&units=metric&time_zone=gmt&format=json&station=<id>&begin_date=<YYYYMMDD>&end_date=<YYYYMMDD>`

See the comment block at the top of `main.go` for the exact response
shapes and how they map onto Helmcentral's plugin contract.
