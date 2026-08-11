# Helmcentral

A marine dashboard for SignalK — anchor watch, tides, weather, routes, tanks
and electrical monitoring on one screen, with optional InfluxDB for
longer-retention telemetry history.

Helmcentral is a single self-contained binary: the web UI is embedded in it,
there is no database server to run and no runtime dependencies. If you already
have a SignalK server, you are one command away.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/gterrill/helmcentral/main/install.sh | sh
```

Then open `http://<this-machine>:8080/`. On first run Helmcentral searches your
network for a SignalK server and offers what it finds — there is nothing to
edit by hand.

Other ways to install, including Docker, are [below](#other-ways-to-install).

> **Security:** Helmcentral has no authentication, and its API can control
> connected equipment (generator start/stop, CZone switching). Run it on a
> trusted boat LAN only — do not port-forward it to the internet. For remote
> access use a VPN or an authenticating reverse proxy.

## Requirements

- A SignalK server reachable on your network, with:
  - [`signalk-derived-data`](https://github.com/SignalK/signalk-derived-data) — true wind and other derived navigation data.
  - [`tracks`](https://github.com/SignalK/tracks) — vessel trails and historical path data.
  - [`signalk-venus-plugin`](https://github.com/sbender9/signalk-venus-plugin) — generator and advanced electrical state (e.g. Victron GX devices).
  - [`signalk-to-influxdb-v2`](https://github.com/tkurki/signalk-to-influxdb-v2) — optional, only for InfluxDB-backed history.
- Linux (x86-64, arm64 or armv7 — covers every Raspberry Pi), macOS, or Windows.

Secrets (SignalK credentials, InfluxDB token, GeoNames/WeatherKit keys) are
never set in files or environment variables: start with none configured, then
paste them into Settings → Secrets in the running app, where they are
encrypted at rest. See [docs/adr/0023-encrypted-secrets-store.md](docs/adr/0023-encrypted-secrets-store.md).

Full operator reference: [docs/configuration.md](docs/configuration.md).

## Project Structure

```
helmcentral/
├── backend/          # Go REST API; embeds the built frontend
├── frontend/         # React + TypeScript + Vite dashboard
├── packaging/        # systemd unit, plugin build script
├── install.sh        # One-line installer
├── .goreleaser.yaml  # Cross-platform release builds
├── docker-compose.yml      # Docker deployment (pulls GHCR image)
├── docker-compose.dev.yml  # Local build/dev workflows
└── README.md
```

## Features

- **Backend (Go)**: RESTful API for data querying and aggregation
  - SignalK integration for real-time maritime data
  - In-memory telemetry history (wind-gust-max, depth-trend/tide-detection) by default, with optional InfluxDB integration for longer-retention time-series storage and retrieval
  - Max wind gust over a selectable window (10m / 30m / 1h / 24h), picked per readout on the wind tile — see [docs/adr/0030-selectable-max-gust-windows.md](docs/adr/0030-selectable-max-gust-windows.md)
  - Serves the dashboard from the same origin as the API — one port, no CORS setup

- **Frontend (React + TypeScript)**: Modern reactive dashboard
  - Vite for fast development and optimized builds
  - Real-time data visualization
  - Responsive design, built for a helm touchscreen

- **Pluggable providers**: drop a sandboxed WASM plugin into `plugins/tides/` to add support for another region's government tide API, with no fork or rebuild required

## Other ways to install

### Install script (recommended)

```sh
curl -fsSL https://raw.githubusercontent.com/gterrill/helmcentral/main/install.sh | sh
```

Detects your platform, verifies the download against the published checksums,
installs to `/usr/local/bin`, creates `/var/lib/helmcentral` for state,
installs the reference plugin bundle, and enables a systemd service on Linux.
Re-run it any time to upgrade — your settings and data are left alone.

Pin a version or change locations with `HELMCENTRAL_VERSION`,
`HELMCENTRAL_PREFIX`, `HELMCENTRAL_STATE_DIR`.

Useful afterwards:

```sh
systemctl status helmcentral
journalctl -u helmcentral -f
```

On macOS the script installs the binary and prints how to run it (no launchd
service). On Windows, download the `.zip` from the
[releases page](https://github.com/gterrill/helmcentral/releases).

### Manual binary download

Grab the archive for your platform from the
[releases page](https://github.com/gterrill/helmcentral/releases), then:

```sh
tar -xzf helmcentral_<version>_linux_arm64.tar.gz
sudo install -m0755 helmcentral /usr/local/bin/helmcentral

# State must live somewhere explicit, or it lands in the working directory.
sudo mkdir -p /var/lib/helmcentral
HELMCENTRAL_STATE_DIR=/var/lib/helmcentral \
  SETTINGS_FILE=/var/lib/helmcentral/settings.yaml \
  helmcentral
```

The archive also contains `packaging/helmcentral.service` if you want the
systemd unit, and `settings.example.yaml` as a starting config.

### Docker

```bash
docker compose pull
docker compose up -d --force-recreate
```

- Helmcentral Dashboard/API: http://localhost:9091

The image is multi-arch (amd64, arm64, armv7), so it runs on a Raspberry Pi.
Add the reference plugins — they are deliberately not baked into the image, so
you can add or update one without repulling:

```bash
mkdir -p plugins && curl -fsSL \
  https://github.com/gterrill/helmcentral/releases/latest/download/helmcentral-plugins-<version>.tar.gz \
  | tar -xz -C plugins
```

To stop:

```bash
docker compose down
```

## Development

### Local Production-Like Build (Docker)

```bash
docker compose -f docker-compose.dev.yml --profile prod up --build -d backend frontend
```

- Backend API: http://localhost:8080
- Frontend Dashboard: http://localhost:5173

### Development Mode (Docker Profiles)

```bash
docker compose -f docker-compose.dev.yml --profile dev up -d backend-dev frontend-dev
```

- Backend Dev API: http://localhost:8080
- Frontend Dev Dashboard (Vite): http://localhost:5173

To stop dev services:

```bash
docker compose -f docker-compose.dev.yml --profile dev down
```

### Local Development

Requires Go 1.22+ and Node.js 24+.

#### Backend

```bash
cd backend
go run main.go
```

#### Frontend

```bash
cd frontend
npm install
npm run dev
```

The Vite dev server proxies `/api` to `localhost:8080`, so the frontend always
talks to the backend on the same origin — exactly as it does in a release
build, where the SPA is embedded in the binary.

#### Tests

```bash
cd backend && go test -short ./...   # -short skips live BOM FTP round-trips
cd frontend && npm test && npm run lint
```

#### Release builds

```bash
goreleaser build --snapshot --clean   # cross-compiles every published target
```

The build hooks build the frontend and stage it into `backend/dist` for the
`//go:embed`, so a snapshot binary is the real thing. Tagging `vX.Y.Z` and
pushing runs [.github/workflows/release.yml](.github/workflows/release.yml),
which publishes the archives, the WASM plugin bundle and the multi-arch image.

## API Documentation

See [backend/README.md](backend/README.md) for API endpoint documentation.

## Architecture

- **Backend**: Echo web framework with middleware for logging, recovery, and CORS
- **Frontend**: Custom Web Components with TypeScript for type safety
- **Data Storage**: In-memory telemetry history by default; InfluxDB optional for longer retention
- **Real-time Data**: SignalK integration for live maritime sensor data

### Trail Architecture

- The server owns ongoing trail sampling for self and nearby AIS vessels.
- Clients consume trail deltas from backend APIs rather than sampling SignalK directly.
- The pre-anchor motoring approach is a separate concern from post-anchor trail history.

See [docs/adr/0001-server-owned-trail-sampling.md](docs/adr/0001-server-owned-trail-sampling.md), [docs/adr/0002-separate-motoring-and-anchor-trails.md](docs/adr/0002-separate-motoring-and-anchor-trails.md), and [docs/adr/0003-motoring-seed-downsampling.md](docs/adr/0003-motoring-seed-downsampling.md) (superseded by [docs/adr/0020-in-memory-telemetry-history-optional-influxdb.md](docs/adr/0020-in-memory-telemetry-history-optional-influxdb.md)).

### Route Planning

Operators can plan multi-leg routes (waypoint sequences) with automatic per-leg distance/bearing/ETA, from the "Routes" tab in the bottom drawer or a glanceable dashboard tile. This is manual waypoint planning only — no hazard-avoidance or weather-optimized routing, and no chart licensing dependency.

See [docs/adr/0006-manual-route-planning.md](docs/adr/0006-manual-route-planning.md).

A saved route can also be Activated, which pushes it to the boat's SignalK server as the vessel's active route, so other NMEA2000 equipment (autopilots, MFDs) can follow it — the same mechanism Timezero Professional uses. HelmCentral itself still does no live navigation.

See [docs/adr/0007-signalk-route-activation.md](docs/adr/0007-signalk-route-activation.md).

### Configurable Dashboard Layout

The dashboard's 13 widgets can be rearranged, resized, shown, or hidden by operators without a code deployment. In layout mode (toggle in the header), operators can drag widgets to new positions, resize them, or remove them from the display; unplaced widgets can be added back via an "Add Widget" picker. The layout is persisted server-side and restored on the next session.

See [docs/adr/0012-configurable-bento-dashboard.md](docs/adr/0012-configurable-bento-dashboard.md).

### Tide Provider Plugins

Tide data comes from a pluggable `tideProvider` registry (`backend/tide_providers.go`) with no built-in provider at all — tides are WASM-plugin-only, the same as weather, waves, and forecast warnings. Every regional tide source, including BOM for Australia, ships as a WASM plugin, so a developer wanting to add another region's government tide API doesn't need to fork Helmcentral or touch Go at all: drop a compiled WASM plugin into `plugins/tides/` and it's picked up as a new provider in the existing Settings tide-provider dropdown on the next restart — zero frontend changes.

With no plugin installed there are no tide providers, and `/api/tide-today` says so rather than guessing — see [docs/adr/0033-remove-storm-glass-tides-plugin-only.md](docs/adr/0033-remove-storm-glass-tides-plugin-only.md) for why the last built-in provider (Storm Glass) was removed, and the recorded path for porting a position-based provider to a plugin.

A plugin is a small guest module exporting five functions (`id`, `name`, `ttl_seconds`, `search_stations`, `fetch_tide_chart`) — the host does the interpolation, caching, and spring/neap classification, so a plugin only ever returns raw station and tide-extreme data. Plugins run sandboxed via [Extism](https://extism.org/)/[wazero](https://wazero.io/) (WASM linear-memory isolation, no filesystem or process access), and can only reach the network hosts explicitly declared in a companion `<name>.allowed_hosts.json` file — no file means no network access at all.

Plugins can be authored in any language with an Extism PDK (TinyGo, Rust, Zig, C, AssemblyScript, C++, Haskell). [docs/examples/tide-plugins/noaa/](docs/examples/tide-plugins/noaa/) is a complete, working TinyGo reference — a real integration against NOAA's CO-OPS API, not BOM ported to WASM. To build it:

```bash
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:latest sh -c "
  cd docs/examples/tide-plugins/noaa &&
  go mod tidy &&
  tinygo build -o noaa.wasm -target wasip1 -buildmode c-shared main.go
"
```

See [docs/examples/tide-plugins/noaa/README.md](docs/examples/tide-plugins/noaa/README.md) for installing a built plugin, and [docs/adr/0017-wasm-plugin-tide-providers.md](docs/adr/0017-wasm-plugin-tide-providers.md) for why WASM was chosen over alternatives (including Lua) and the full plugin contract.

Unlike weather and waves (below), tides have **no universal keyless default** — tide data is tied to real physical station networks rather than a global forecast model, so there's no free API with genuinely worldwide coverage to hardcode, and no built-in fallback provider either. BOM and NOAA are both built automatically by the `plugins-builder` Compose service, but only cover Australia and the US respectively, so coverage is limited to whatever plugins are actually installed. An operator must explicitly pick `ui.tide_provider` in Settings to match their region — nothing is silently assumed on their behalf, and `/api/tide-today` returns a clear error naming what's missing until they do.

### Weather & Wave Forecast Provider Plugins

Weather and wave/swell forecasting are pluggable the same way tides are, via two sibling registries (`backend/weather_providers.go`, `backend/wave_providers.go`) sharing the same generic WASM host layer (`backend/wasm_plugin.go`) as tides. They're deliberately kept as **separate plugin types** — a weather source and a wave/marine source are usually different upstream APIs entirely, so a location can mix, e.g., Apple WeatherKit for point weather with Open-Meteo Marine for swell.

Unlike tides, neither registry has a native built-in provider — both ship exclusively as WASM plugins, discovered from `plugins/weather/` and `plugins/waves/` respectively (`PLUGINS_WEATHER_DIR`/`PLUGINS_WAVES_DIR` to override). **Open-Meteo** (weather) and **Open-Meteo Marine** (waves) are the defaults, specifically because both are free and keyless — a fresh install gets a fully working forecast dashboard with zero configuration. **Apple WeatherKit** ships as a second reference weather plugin for anyone who prefers Apple's data and already has (or is willing to get) a paid Apple Developer account — see [docs/examples/weather-plugins/weatherkit/README.md](docs/examples/weather-plugins/weatherkit/README.md) for how to obtain WeatherKit credentials.

A weather plugin exports `id`, `name`, `ttl_seconds`, and `fetch_forecast` (current conditions + multi-day + hourly, all SI units); a wave plugin exports the same first three plus `fetch_waves` (hourly wave/swell series + optional sea-surface temperature). As with tides, the host owns all derived data — unit conversion, day-bucketing into the vessel's local timezone, wind/wave/precipitation summary sentences, and moon phase — so a plugin only ever returns raw, provider-native numbers. The same sandboxing rules apply (Extism/wazero isolation, `<name>.allowed_hosts.json` default-deny network allowlist). A plugin needing operator-supplied secrets (WeatherKit's signing key, in this codebase's only example so far) reads them via a companion `<name>.config.json` file whose values are `${ENV_VAR}`-expanded from the backend's environment at load time — see [docs/adr/0018-wasm-plugin-weather-and-wave-providers.md](docs/adr/0018-wasm-plugin-weather-and-wave-providers.md) for the full contract and config-file format.

Build any of the three reference plugins the same way as the tide plugins:

```bash
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:latest sh -c "
  cd docs/examples/weather-plugins/open-meteo &&
  go mod tidy &&
  tinygo build -o open-meteo.wasm -target wasip1 -buildmode c-shared .
"
```

(swap the directory for `docs/examples/weather-plugins/weatherkit` or `docs/examples/wave-plugins/open-meteo-marine` — note both are multi-file packages, so the build target is `.`, not a single `main.go`). The bundled `plugins-builder` Compose service (see `docker-compose.yml`/`docker-compose.dev.yml`) already builds and installs all seven reference plugins (two tide, two weather, one wave, two forecast warnings) automatically on every `make dev`/deploy — see each plugin's own README for details and installation notes if building/installing manually.

**Upgrading an existing install:** if you previously ran a version of Helmcentral with the old hardcoded WeatherKit/Open-Meteo-marine integration, delete the now-orphaned `cache/weather_today_cache.json` and `cache/weather_forecast_cache.json` files (weather/wave caching moved to per-plugin files under `cache/weather_wasm_*_cache.json`/`cache/wave_wasm_*_cache.json`). If you were relying on WeatherKit, paste the four WeatherKit credentials into Settings → Secrets and select "Apple WeatherKit" under Settings → Weather — the default provider on upgrade is Open-Meteo (keyless) unless you explicitly configure otherwise.

### Forecast Warnings Provider Plugins

Marine/weather warnings are pluggable the same way tides/weather/waves are, via `backend/forecast_warnings_providers.go` on the same generic WASM host layer. Like weather and waves, there's no native built-in — both reference plugins ship as WASM, discovered from `plugins/forecast-warnings/` (`PLUGINS_FORECAST_WARNINGS_DIR` to override). **BOM** (Australia's Bureau of Meteorology) is the default (`ui.forecast_warnings_provider`, default `"bom"`); **NWS** (the US National Weather Service) ships as the second reference plugin, giving genuine non-Australian coverage.

A plugin exports `id`, `name`, `ttl_seconds`, and `fetch_warnings(lat, lon)`. Unlike tide/weather/wave, the host does **no** derivation here — no zone-matching, no active/cancelled filtering: each plugin resolves its own zone(s) for the given position and returns only bulletins/sections that are already current and relevant. This is a deliberate departure, not an oversight — BOM's zone taxonomy (named coastal zones from state bounding boxes) and NWS's (UGC marine zone codes) are incompatible namespaces, and "is this warning still active" is determined differently by each (BOM: free-text section parsing; NWS: structured CAP alert status fields) — there's nothing universal to factor out host-side. See [docs/adr/0019-ftp-host-function-and-forecast-warnings-provider.md](docs/adr/0019-ftp-host-function-and-forecast-warnings-provider.md) for the full contract.

BOM's warnings data is only reliably available over anonymous FTP (`ftp.bom.gov.au`) — BOM's website actively bot-blocks HTTP scraping of the same content. Since a WASM guest can't open raw sockets, this repo gained a new, generic **custom Extism host function** (`ftp_fetch`, in `backend/wasm_ftp_fetch.go`) that every plugin type can use, gated by the same `<name>.allowed_hosts.json` allowlist already used for HTTP. This is the first (and so far only) custom host function in this codebase — see the ADR for the full mechanism and why it was built instead of keeping BOM native.

Build either reference plugin the same way as the others (target `.`, both are multi-file packages):

```bash
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:latest sh -c "
  cd docs/examples/forecast-warnings-plugins/bom &&
  go mod tidy &&
  tinygo build -o bom.wasm -target wasip1 -buildmode c-shared .
"
```

**Upgrading an existing install:** the old `GET /api/marine-warnings` endpoint and `cache/bom_marine_warnings_cache.json` file are gone — warnings now live at `GET /api/forecast-warnings`, cached per-plugin under `cache/forecast_warnings_wasm_*_cache.json`. No environment variables are needed for the default BOM plugin (it's keyless, using BOM's own public anonymous FTP mirror).

## Roadmap

- **Authentication/authorization** — Helmcentral is currently unauthenticated
  and assumes a trusted LAN. This is the largest known gap; see the security
  note at the top.
- Runtime-configurable units and anchor geometry (both are currently
  build-time defaults in the frontend; the backend already stores them).
- mDNS-based SignalK discovery, now viable for native installs — the container
  networking that ruled it out no longer applies. See
  [docs/adr/0029-signalk-discovery.md](docs/adr/0029-signalk-discovery.md).

## Contributing

Issues and pull requests are welcome. CI runs `go vet`, the Go and frontend
test suites, lint, and a full cross-platform release build on every PR — run
those locally first (see [Development](#development)).

Durable design decisions live in [docs/adr/](docs/adr/); if a change turns on
a non-obvious trade-off, add an ADR alongside it.

## Inspiration

Dashboard design inspired by modern maritime monitoring systems.

## License

[MIT](LICENSE)
