# Open-Meteo Marine wave provider plugin

A Helmcentral wave-provider plugin backed by Open-Meteo's free, keyless Marine
API. **This is the default wave provider for fresh Helmcentral installs
specifically because it needs no API key** — no config.json required.

The plugin fetches hourly wave forecasts (wave height, period, direction, wind
swell, and swell components) and optionally sea surface temperature, converted
to SI units and RFC3339 timestamps per the plugin contract.

Written in TinyGo — the most approachable option given Helmcentral's own Go
backend. The Extism plugin contract isn't TinyGo-specific: Rust, Zig, C,
AssemblyScript, C++, and Haskell PDKs all implement the same contract
identically; see [Extism's PDK list](https://extism.org/docs/concepts/pdk) if
you'd rather use one of those.

## Two-endpoint design

The plugin makes two separate HTTP calls to the Marine API:

1. **Wave data (REQUIRED)**: Fetches hourly wave heights, periods, directions,
   wind-wave heights, and swell-wave heights. This call is pinned to NOAA's
   GFS-Wave (WaveWatch III) model via `models=ncep_gfswave025` so swell height,
   period and direction line up with other WaveWatch III-based swell forecasts
   for a given coastline. Open-Meteo's default "best_match" model blend runs
   noticeably lower/different here.

2. **Sea surface temperature (OPTIONAL)**: Fetches current sea surface
   temperature. This call deliberately does NOT pin a `models=` param (unlike
   the wave-data fetch) — the wave-specific NOAA GFS-Wave model does not carry
   sea surface temperature at all (returns null/"undefined" units); only
   Open-Meteo's default model blend does. If this fetch fails or the value is
   absent/null (confirmed live: inland coordinates return 200 with
   sea_surface_temperature:null), the whole fetch_waves call still succeeds —
   sea_surface_temperature_c is simply omitted from the output JSON entirely
   (never a masked zero placeholder).

## Why this plugin is two files

`open-meteo-marine.go` holds all the parsing/HTTP logic and has no dependency
on `github.com/extism/go-pdk`, so `go test ./...` (below) runs on the plain
host Go toolchain with no TinyGo or wasm target needed. `main.go` holds only
the thin `//go:wasmexport` wrapper functions and is gated `//go:build tinygo`
so it's excluded from that plain host build (TinyGo defines the `tinygo` build
tag automatically; plain `go test` doesn't). Because of this split, always
build the whole package directory (`.`), not just `main.go` by name — see
below.

## Building it

Requires only Docker (no local TinyGo install needed), pinned to
`tinygo/tinygo:0.41.1` — the same version already pinned for this repo's own
WASM test fixtures (see `backend/wasm_tide_provider_test.go`'s regeneration
comment), to avoid `:latest` drift. From the repo root:

```sh
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:0.41.1 sh -c "
  cd docs/examples/wave-plugins/open-meteo-marine &&
  go mod tidy &&
  tinygo build -o open-meteo-marine.wasm -target wasip1 -buildmode c-shared .
"
```

Note the trailing `.` (build the whole package directory), not `main.go` —
naming `main.go` alone would exclude `open-meteo-marine.go` and fail with
`undefined:` errors, since Go/TinyGo's single/multi-file build mode only
compiles the files explicitly listed.

This produces `open-meteo-marine.wasm` in this directory. Requires network
access inside the container (`go mod tidy` fetches `github.com/extism/go-pdk`)
and Docker with the `tinygo/tinygo` image available locally.

If you'd rather install TinyGo locally instead of using Docker, the equivalent
commands are:

```sh
go mod tidy
tinygo build -o open-meteo-marine.wasm -target wasip1 -buildmode c-shared .
```

Both the Open-Meteo and other wave plugins are also built and installed
automatically as part of Docker Compose startup (see the repo's compose files)
— manual builds via this README are for anyone who wants to build/inspect this
plugin outside that automation.

## Installing it

Helmcentral discovers wave-provider plugins by scanning `plugins/waves/` at
startup (overridable via the `PLUGINS_WAVES_DIR` env var). To install this
plugin manually:

1. Copy the compiled `open-meteo-marine.wasm` into your `plugins/waves/`
   directory.
2. Create `plugins/waves/open-meteo-marine.allowed_hosts.json` next to it,
   containing:

   ```json
   ["marine-api.open-meteo.com"]
   ```

   This is not optional — a plugin with no companion
   `<name>.allowed_hosts.json` file gets **no network access at all**
   (Helmcentral's default-deny sandboxing). `marine-api.open-meteo.com` is the
   only host this plugin talks to.
3. Restart the Helmcentral container (or the dev backend). "Open-Meteo Marine"
   should now appear in the wave-provider dropdown in Settings, with zero
   frontend changes required.

`plugins/waves/` lives at the repo root and is gitignored — it's operator
runtime content, not part of the repo, the same treatment `backend-data/`
already gets. A fresh Helmcentral checkout ships with **no** plugins active by
default, specifically so there's no surprise outbound traffic to a foreign API
on a default install.

## Testing

`main_test.go` unit-tests the parsing logic directly, entirely on the host Go
toolchain — no TinyGo or WASM runtime needed:

```sh
cd docs/examples/wave-plugins/open-meteo-marine && go mod tidy && go vet ./... && go test ./...
```

## Endpoints this plugin uses

1. Wave data: `GET https://marine-api.open-meteo.com/v1/marine?latitude=<lat>&longitude=<lon>&hourly=wave_height,wave_direction,wave_period,wind_wave_height,swell_wave_height&timezone=auto&forecast_days=<days>&models=ncep_gfswave025`

   Response includes `utc_offset_seconds` (how many seconds local time is ahead
   of UTC), hourly time arrays in naive local-time format
   ("2026-07-19T00:00"), and parallel arrays of wave measurements.

2. Sea temperature: `GET https://marine-api.open-meteo.com/v1/marine?latitude=<lat>&longitude=<lon>&current=sea_surface_temperature`

   Response includes current sea surface temperature (can be null for inland
   coordinates).

See Open-Meteo's [Marine Weather API
docs](https://open-meteo.com/en/docs/marine-weather-api) for full endpoint
details and coverage.
