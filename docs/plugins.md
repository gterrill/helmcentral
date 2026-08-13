# Provider plugins

Tides, weather, waves and forecast warnings are **not built into Helmcentral**.
Every one of them is a sandboxed WASM plugin loaded from disk at startup, so
adding support for another region's government API means dropping a `.wasm`
file into a directory — no fork, no Go, no rebuild, no frontend change.

The four registries (`backend/tide_providers.go`,
`backend/weather_providers.go`, `backend/wave_providers.go`,
`backend/forecast_warnings_providers.go`) all sit on one generic WASM host
layer, `backend/wasm_plugin.go`.

| Category | Directory | Override | Bundled reference plugins |
| --- | --- | --- | --- |
| Tides | `plugins/tides/` | `PLUGINS_TIDES_DIR` | `bom` (Australia), `noaa` (US) |
| Weather | `plugins/weather/` | `PLUGINS_WEATHER_DIR` | `open-meteo` (worldwide, keyless — **default**), `weatherkit` (Apple, needs keys) |
| Waves | `plugins/waves/` | `PLUGINS_WAVES_DIR` | `open-meteo-marine` (**default**) |
| Forecast warnings | `plugins/forecast-warnings/` | `PLUGINS_FORECAST_WARNINGS_DIR` | `bom` (Australia — **default**), `nws` (US) |

Pick the active provider per category in Settings. All seven reference plugins
are built and installed automatically by the `plugins-builder` Compose service
on every `make dev`/deploy.

## The sandbox

Plugins run via [Extism](https://extism.org/)/[wazero](https://wazero.io/):
WASM linear-memory isolation, no filesystem access, no process access. Network
access is **default-deny** — a plugin can only reach hosts named in a companion
`<name>.allowed_hosts.json` file sitting next to the `.wasm`. No file means no
network at all.

A plugin needing operator-supplied secrets (WeatherKit's signing key is the
only example in this codebase) reads them from a companion
`<name>.config.json` whose values are `${ENV_VAR}`-expanded from the backend's
environment at load time.

**The host owns all derived data.** Unit conversion, interpolation, caching,
day-bucketing into the vessel's local timezone, spring/neap classification,
summary sentences, moon phase — all of it is host-side, so a plugin only ever
returns raw, provider-native numbers.

Plugins can be authored in any language with an Extism PDK: TinyGo, Rust, Zig,
C, AssemblyScript, C++, Haskell.

## The contracts

Every plugin exports `id`, `name` and `ttl_seconds`, plus its category's
fetch function:

| Category | Fetch exports | Returns |
| --- | --- | --- |
| Tides | `search_stations`, `fetch_tide_chart` | Raw station and tide-extreme data |
| Weather | `fetch_forecast` | Current + multi-day + hourly, all SI units |
| Waves | `fetch_waves` | Hourly wave/swell series, optional sea-surface temperature |
| Forecast warnings | `fetch_warnings(lat, lon)` | Current, relevant bulletins only |

Full contracts and config-file formats:
[ADR 0017](adr/0017-wasm-plugin-tide-providers.md) (tides),
[ADR 0018](adr/0018-wasm-plugin-weather-and-wave-providers.md) (weather and
waves), [ADR 0019](adr/0019-ftp-host-function-and-forecast-warnings-provider.md)
(forecast warnings).

### Why warnings are the exception

For tides, weather and waves the host does the derivation. For forecast
warnings it does **none** — no zone-matching, no active/cancelled filtering.
Each plugin resolves its own zones for a position and returns only what is
already current.

This is deliberate. BOM's zone taxonomy (named coastal zones from state
bounding boxes) and NWS's (UGC marine zone codes) are incompatible namespaces,
and "is this warning still active" is answered differently by each — BOM by
free-text section parsing, NWS by structured CAP alert status fields. There is
nothing universal to factor out host-side.

### The FTP host function

BOM's warnings are only reliably available over anonymous FTP
(`ftp.bom.gov.au`); BOM's website actively bot-blocks HTTP scraping of the same
content. A WASM guest cannot open raw sockets, so Helmcentral provides a
generic custom Extism host function — `ftp_fetch`, in
`backend/wasm_ftp_fetch.go` — available to every plugin type and gated by the
same `allowed_hosts.json` allowlist used for HTTP. It is the only custom host
function in the codebase. See
[ADR 0019](adr/0019-ftp-host-function-and-forecast-warnings-provider.md) for
why it was built rather than keeping BOM native.

## Why tides have no default

Weather and waves default to Open-Meteo and Open-Meteo Marine because both are
free, keyless and genuinely worldwide — a fresh install gets a working forecast
dashboard with zero configuration.

Tides have no equivalent. Tide data is tied to real physical station networks
rather than a global forecast model, so there is no free API with worldwide
coverage to hardcode, and there is no built-in fallback provider either. BOM
and NOAA cover Australia and the US respectively, and nothing else.

So an operator must explicitly set `ui.tide_provider` in Settings to match
their region. With no plugin installed there are no tide providers at all, and
`/api/tide-today` returns a clear error naming what is missing rather than
guessing. See
[ADR 0033](adr/0033-remove-storm-glass-tides-plugin-only.md) for why the last
built-in provider (Storm Glass) was removed, and the recorded path for porting
a position-based provider to a plugin.

## Building a plugin

Each reference plugin lives under `docs/examples/`. The NOAA tide plugin is a
complete, working TinyGo integration against NOAA's CO-OPS API — a real
integration, not BOM ported to WASM.

```bash
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:latest sh -c "
  cd docs/examples/tide-plugins/noaa &&
  go mod tidy &&
  tinygo build -o noaa.wasm -target wasip1 -buildmode c-shared main.go
"
```

Swap the directory for any other example. **Note the build target:** the NOAA
tide plugin is a single file (`main.go`); the weather, wave and
forecast-warning examples are multi-file packages, so their target is `.`:

```bash
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:latest sh -c "
  cd docs/examples/weather-plugins/open-meteo &&
  go mod tidy &&
  tinygo build -o open-meteo.wasm -target wasip1 -buildmode c-shared .
"
```

Available example directories:

- `docs/examples/tide-plugins/bom`, `docs/examples/tide-plugins/noaa`
- `docs/examples/weather-plugins/open-meteo`, `docs/examples/weather-plugins/weatherkit`
- `docs/examples/wave-plugins/open-meteo-marine`
- `docs/examples/forecast-warnings-plugins/bom`, `docs/examples/forecast-warnings-plugins/nws`

Each has its own README with installation notes.
[docs/examples/weather-plugins/weatherkit/README.md](examples/weather-plugins/weatherkit/README.md)
also covers how to obtain WeatherKit credentials.

## Installing a built plugin

Copy the `.wasm` and its `allowed_hosts.json` sidecar into the right category
directory and restart. To install the published bundle by hand:

```sh
curl -fsSL https://github.com/gterrill/helmcentral/releases/latest/download/helmcentral-plugins-<version>.tar.gz \
  | sudo tar -xz -C /var/lib/helmcentral/plugins
sudo systemctl restart helmcentral
```

The plugin appears in the existing Settings provider dropdown for its category
on the next restart. No frontend changes are ever needed.
