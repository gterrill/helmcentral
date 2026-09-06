# Provider plugins

Tides, weather, waves, forecast warnings and the upper-air outlook use sandboxed
WASM provider plugins loaded from disk at startup. Installing a provider for
another region's government API does not require changes to Helmcentral's code
or a rebuild of its binary or frontend.

The five registries (`backend/tide_providers.go`,
`backend/weather_providers.go`, `backend/wave_providers.go`,
`backend/forecast_warnings_providers.go`, and `backend/upper_air_providers.go`)
share a single WASM host layer in `backend/wasm_plugin.go`.

| Category | Directory | Override | Bundled reference plugins |
| --- | --- | --- | --- |
| Tides | `plugins/tides/` | `PLUGINS_TIDES_DIR` | `bom` (Australia), `noaa` (US) |
| Weather | `plugins/weather/` | `PLUGINS_WEATHER_DIR` | `open-meteo` (worldwide, keyless, **default**), `weatherkit` (Apple, needs keys) |
| Waves | `plugins/waves/` | `PLUGINS_WAVES_DIR` | `open-meteo-marine` (**default**) |
| Forecast warnings | `plugins/forecast-warnings/` | `PLUGINS_FORECAST_WARNINGS_DIR` | `bom` (Australia, **default**), `nws` (US) |
| Upper air | `plugins/upper-air/` | `PLUGINS_UPPER_AIR_DIR` | `open-meteo-upper` (worldwide, keyless) |

You select the active provider for each category in Settings. All eight
reference plugins are built and installed automatically by the `plugins-builder`
Compose service during each `make dev` run and deployment.

Upper air is the one optional category: leave the plugin out and the forecast
page simply shows no 500mb section. The rest have a widget that goes empty
without a provider.

## The sandbox

Plugins run via [Extism](https://extism.org/) and [wazero](https://wazero.io/).
This provides WASM linear-memory isolation without filesystem or process access.
Network access is **default-deny**: a plugin can only reach hosts specified in a
companion `<name>.allowed_hosts.json` file located next to the `.wasm` file. If
this file is missing, the plugin cannot access the network.

If a plugin requires operator-supplied secrets (WeatherKit's signing key is
currently the only example in this codebase), it reads them from a companion
`<name>.config.json` file. The host expands `${ENV_VAR}` references in this file
using the backend environment at load time.

**The host owns all derived data.** Unit conversion, interpolation, caching,
day-bucketing into the vessel's local timezone, spring/neap classification,
summary sentences, and moon phase calculations are all executed host-side. The
plugin only returns raw, provider-native numbers.

Plugins can be written in any language that provides an Extism PDK, such as
TinyGo, Rust, Zig, C, AssemblyScript, C++, or Haskell.

## The contracts

Every plugin exports `id`, `name`, and `ttl_seconds`, along with the fetch
function for its category:

| Category | Fetch exports | Returns |
| --- | --- | --- |
| Tides | `search_stations`, `fetch_tide_chart` | Raw station and tide-extreme data |
| Weather | `fetch_forecast` | Current + multi-day + hourly, all SI units |
| Waves | `fetch_waves` | Hourly wave/swell series with per-component direction and period, optional sea-surface temperature |
| Forecast warnings | `fetch_warnings(lat, lon)` | Current, relevant bulletins only |
| Upper air | `fetch_upper_air` | Hourly 500mb and 1000mb geopotential height, 500mb wind and temperature |

The reference plugins under `docs/examples/` define the exact JSON shape for
each export. Refer to the example for your category alongside this table rather
than relying on written summaries that might diverge from the code.

### Companion files

A plugin consists of a `.wasm` file and up to three optional companion files
sharing its base name. An omitted companion file means that no hosts, configs,
or secrets are granted.

| File | Shape | Purpose |
| --- | --- | --- |
| `<name>.allowed_hosts.json` | JSON array of hostnames | The only hosts this plugin may reach, over HTTP or FTP. Absent means no network at all. |
| `<name>.config.json` | JSON object of string values | Plugin configuration. A `${VAR}` value is expanded by the host before the plugin sees it. |
| `<name>.allowed_secrets.json` | JSON array of secret names | Which stored secrets this plugin's `${VAR}` references may resolve. A secret not listed here is denied and the denial is logged. |

```jsonc
// noaa.allowed_hosts.json
["api.tidesandcurrents.noaa.gov"]
```

```jsonc
// weatherkit.config.json
{
  "key_id": "${WEATHERKIT_KEY_ID}",
  "team_id": "${WEATHERKIT_TEAM_ID}",
  "service_id": "${WEATHERKIT_SERVICE_ID}",
  "private_key": "${WEATHERKIT_PRIVATE_KEY}"
}
```

```jsonc
// weatherkit.allowed_secrets.json
["WEATHERKIT_KEY_ID", "WEATHERKIT_TEAM_ID", "WEATHERKIT_SERVICE_ID", "WEATHERKIT_PRIVATE_KEY"]
```

Both allowlists are enforced by the host rather than the WASM sandbox. They
prevent a plugin from receiving unapproved secrets or contacting unlisted hosts.
If a plugin receives approval for both a secret and an external host, it can
transmit that secret to that host. Review third-party companion files with this
in mind.

### Why warnings are the exception

For tides, weather, and waves, the host performs all derived calculations.
For forecast warnings, it does **none**: it performs no zone matching and no
filtering between active and cancelled bulletins. Each plugin resolves its own
zones for a coordinate and returns only bulletins that are currently active.

BOM's zone taxonomy (named coastal zones derived
from state bounding boxes) and NWS's taxonomy (UGC marine zone codes) use
incompatible namespaces. Determining whether a warning remains active also
differs: BOM requires parsing free-text sections, whereas NWS provides
structured CAP alert status fields. Because these models share no common
structure, this logic cannot be generalised into the host.

### The FTP host function

BOM warnings are only reliably available over anonymous FTP (`ftp.bom.gov.au`),
because the BOM website blocks automated HTTP scraping of the same content.
Because WASM guests cannot open raw network sockets, Helmcentral provides a
generic custom Extism host function, `ftp_fetch` (in `backend/wasm_ftp_fetch.go`).
This function is available to all plugin types and is controlled by the same
`allowed_hosts.json` allowlist used for HTTP. It is the only custom host
function in the codebase.

The host function lets BOM use the same plugin interface as other providers
despite its FTP requirement, instead of requiring a built-in Go provider.

## Why tides have no default

Weather and waves default to Open-Meteo and Open-Meteo Marine because both
services are free, keyless, and worldwide. A new installation receives a
functional forecast dashboard without configuration.

Tides have no equivalent global service. Tide predictions depend on physical
water-level stations rather than global numerical models, so there is no free
API with worldwide coverage to configure by default, and Helmcentral does not
include a built-in fallback provider. BOM covers Australia and NOAA covers the
United States, with no coverage beyond those jurisdictions.

Operators must therefore configure `ui.tide_provider` in Settings for their
region. If no tide plugin is installed, no tide providers exist, and
`/api/tide-today` returns an explicit error identifying the missing provider
rather than generating inaccurate predictions.

Helmcentral previously included a built-in coordinate-based provider, Storm
Glass, which offered global coverage. It was removed rather than retained as a
fallback. A global model substituting for physical station observations
generates tide predictions that appear authoritative but are inaccurate for
local waters, which is less safe than returning an explicit error. Operators who
need a coordinate-based provider can write one as a plugin by implementing
`search_stations` to return a single synthetic station for the requested
coordinates.

## Building a plugin

Reference plugins are located in `docs/examples/`. The NOAA tide plugin is a
complete TinyGo implementation for the NOAA CO-OPS API, written directly for
this interface rather than ported from BOM.

```bash
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:latest sh -c "
  cd docs/examples/tide-plugins/noaa &&
  go mod tidy &&
  tinygo build -o noaa.wasm -target wasip1 -buildmode c-shared main.go
"
```

The same command structure applies to other examples. **Note the build target:**
the NOAA tide plugin consists of a single file (`main.go`), whereas the weather,
wave, and forecast-warning plugins are multi-file packages that use `.` as the
build target:

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
- `docs/examples/upper-air-plugins/open-meteo-upper`

Each directory contains a README with installation instructions.
[docs/examples/weather-plugins/weatherkit/README.md](../examples/weather-plugins/weatherkit/README.md)
also explains how to obtain WeatherKit credentials.

## Installing a built plugin

Copy the `.wasm` file and its `<name>.allowed_hosts.json` companion file into the
appropriate category directory, then restart the service. To manually install
the pre-built archive:

```sh
curl -fsSL https://github.com/gterrill/helmcentral/releases/latest/download/helmcentral-plugins-<version>.tar.gz \
  | sudo tar -xz -C /var/lib/helmcentral/plugins
sudo systemctl restart helmcentral
```

After restarting, the plugin appears in the Settings provider dropdown for its
category. No frontend changes are required.
