# Open-Meteo weather provider plugin

A Helmcentral weather-provider plugin backed by Open-Meteo's free, keyless
Forecast API, covering weather worldwide. It is the default weather provider
for fresh Helmcentral installs because it needs no API key or `config.json`.
Install the `.wasm` binary and
Open-Meteo appears in the weather-provider dropdown in Settings.

Open-Meteo is a free, non-commercial weather forecast API with no signup
needed, no rate limits on public usage, and global coverage. It returns
forecasts via a JSON API in SI units (Celsius, m/s, etc.). This plugin is a
reference implementation of Helmcentral's WASM weather-provider contract.

It is not a production-hardened client: it has no pagination or retry/backoff
(see the comments at the top of `main.go`). It can be adapted to another
weather source by changing the URLs and field mappings.

Written in TinyGo. The Extism plugin contract isn't TinyGo-specific: Rust, Zig, C,
AssemblyScript, C++, and Haskell PDKs all implement the same contract
identically; see [Extism's PDK list](https://extism.org/docs/concepts/pdk)
if you'd rather use one of those.

## Contract requirements every weather plugin must honour

Two rules from
[ADR 0035](../../../adr/0035-weather-local-day-boundaries.md) prevent incorrect
day boundaries and misleading precipitation values:

1. **Use the host's `timezone` input.** `fetch_forecast` receives an IANA zone
   (e.g. `Etc/GMT-10`). If your upstream API rolls hourly data up into daily
   summaries, pass this through instead of hardcoding a zone or letting the
   API pick one. The host buckets and labels its own day series in that same
   zone; rolling up on a different boundary silently shifts every day summary
   and drops the record covering local midnight to the offset. An absent
   `timezone` is rejected, not defaulted.

2. **Report absent precipitation as `-1`, never `0`.** `0%` is a legitimate
   forecast, so it cannot double as "no data". If the upstream response omits
   a chance-of-precipitation value, emit `-1` on
   `precipitation_chance_pct` so the UI can show "unavailable". Never
   substitute a value from a different field or a different time window. A
   previous substitution caused an mm/hr rainfall rate to appear as a percentage.

## Why this plugin is two files

`open-meteo.go` holds all the parsing/filtering logic and has no dependency
on `github.com/extism/go-pdk`, so `go test ./...` (below) runs on the plain
host Go toolchain with no TinyGo or wasm target needed. `main.go` holds only
the thin `//go:wasmexport` wrapper functions and is gated `//go:build tinygo`
so it's excluded from that plain host build (TinyGo defines the `tinygo`
build tag automatically; plain `go test` doesn't). Because of this split,
always build the whole package directory (`.`), not just `main.go` by name,
as described below.

## Building it

Requires only Docker (no local TinyGo install needed), pinned to
`tinygo/tinygo:0.41.1`, the same version pinned for this repo's
WASM test fixtures (see `backend/wasm_weather_provider_test.go`'s
regeneration comment), to avoid `:latest` drift. From the repo root:

```sh
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:0.41.1 sh -c "
  cd docs/examples/weather-plugins/open-meteo &&
  go mod tidy &&
  tinygo build -o open-meteo.wasm -target wasip1 -buildmode c-shared .
"
```

Use the trailing `.` to build the whole package directory. Naming `main.go`
alone would exclude `open-meteo.go` and fail with
`undefined:` errors, since Go/TinyGo's single/multi-file build mode only
compiles the files explicitly listed.

This produces `open-meteo.wasm` in this directory. Requires network access
inside the container (`go mod tidy` fetches `github.com/extism/go-pdk`) and
Docker with the `tinygo/tinygo` image available locally.

If you'd rather install TinyGo locally instead of using Docker, the
equivalent commands are:

```sh
go mod tidy
tinygo build -o open-meteo.wasm -target wasip1 -buildmode c-shared .
```

Both the Open-Meteo and other example plugins are also built and installed
automatically as part of Docker Compose startup (see the repo's compose
files). Use the manual build instructions to build or inspect this plugin
separately.

## Installing it

Helmcentral discovers weather-provider plugins by scanning `plugins/weather/`
at startup (overridable via the `PLUGINS_WEATHER_DIR` env var). To install
this plugin manually:

1. Copy the compiled `open-meteo.wasm` into your `plugins/weather/`
   directory.
2. Create `plugins/weather/open-meteo.allowed_hosts.json` next to it,
   containing:

   ```json
   ["api.open-meteo.com"]
   ```

   This file is required for network access. A plugin with no companion
   `<name>.allowed_hosts.json` file gets no network access
   (Helmcentral's default-deny sandboxing). `api.open-meteo.com` is the only
   host this plugin talks to, so it's the only host that needs to be
   allowlisted.
3. Restart the Helmcentral container (or the dev backend). "Open-Meteo"
   should now appear in the weather-provider dropdown in Settings, with no
   frontend changes required. No `config.json` is needed since it is keyless.

`plugins/weather/` lives at the repo root and is gitignored, like `backend-data/`,
because it contains operator runtime files. A fresh Helmcentral checkout ships
with no plugins active by default, to avoid contacting external APIs before a
provider is chosen.

## Testing

`main_test.go` unit-tests the WMO weather-code mapping function
(`wmoCodeToCondition`), the local-time-to-UTC conversion function
(`parseOpenMeteoLocalTime`), and the full JSON parsing logic
(`parseOpenMeteoForecast`) against synthetic fixtures, entirely on the host
Go toolchain, with no TinyGo or WASM runtime needed:

```sh
go test ./...
```

## Open-Meteo endpoints this plugin uses

- Forecast (hourly + daily): `GET https://api.open-meteo.com/v1/forecast?latitude=<lat>&longitude=<lon>&current=temperature_2m,weather_code,wind_speed_10m,wind_gusts_10m,wind_direction_10m,is_day,precipitation_probability&hourly=temperature_2m,weather_code,wind_speed_10m,wind_gusts_10m,wind_direction_10m,precipitation_probability,precipitation,uv_index,is_day&daily=weather_code,temperature_2m_max,temperature_2m_min,wind_speed_10m_max,wind_gusts_10m_max,wind_direction_10m_dominant,precipitation_probability_max,sunrise,sunset&wind_speed_unit=ms&timezone=auto&forecast_days=<days>`

See the comment block at the top of `main.go` and `open-meteo.go`'s
`parseOpenMeteoLocalTime` documentation for exact response shape details and
how they map onto Helmcentral's plugin contract (including the
UTC-offset conversion for Open-Meteo's naive local-time timestamps).
