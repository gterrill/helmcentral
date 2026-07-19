# Helmcentral

A modern dashboard application integrating SignalK and InfluxDB for marine monitoring and visualization.

## Project Structure

```
helmcentral/
├── backend/          # Go REST API
├── frontend/         # TypeScript Web Components Dashboard
├── docker-compose.yml      # Server deployment (pulls GHCR image)
├── docker-compose.dev.yml  # Local build/dev workflows
└── README.md
```

## Features

- **Backend (Go)**: RESTful API for data querying and aggregation
  - SignalK integration for real-time maritime data
  - InfluxDB integration for time-series data storage and retrieval
  - CORS-enabled for web frontend communication

- **Frontend (TypeScript + Web Components)**: Modern reactive dashboard
  - Web Components architecture for modularity
  - Vite for fast development and optimized builds
  - Real-time data visualization
  - Responsive design

- **Pluggable tide providers**: drop a sandboxed WASM plugin into `plugins/tides/` to add support for another region's government tide API, with no fork or rebuild required

## How To Run

### Prerequisites

- Docker & Docker Compose (recommended)
- OR: Go 1.22+, Node.js 18+
- SignalK Server with the following plugins installed and enabled:
  - [`signalk-derived-data`](https://github.com/SignalK/signalk-derived-data) - Needed to calculate true wind and other derived navigation data.
  - [`tracks`](https://github.com/SignalK/tracks) - Needed for drawing vessel trails and retrieving historical path data.
  - [`signalk-venus-plugin`](https://github.com/sbender9/signalk-venus-plugin) - Needed to retrieve generator and advanced electrical states (e.g. from Victron GX devices).
  - [`signalk-to-influxdb-v2`](https://github.com/tkurki/signalk-to-influxdb-v2) - Needed to write time-series data from SignalK to InfluxDB for historical graphing and analysis.

### Server Deployment (Docker)

```bash
docker compose pull
docker compose up -d --force-recreate
```

- Helmcentral Dashboard/API: http://localhost:9091

To stop:

```bash
docker compose down
```

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

## API Documentation

See [backend/README.md](backend/README.md) for API endpoint documentation.

## Architecture

- **Backend**: Echo web framework with middleware for logging, recovery, and CORS
- **Frontend**: Custom Web Components with TypeScript for type safety
- **Data Storage**: InfluxDB for time-series data
- **Real-time Data**: SignalK integration for live maritime sensor data

### Trail Architecture

- The server owns ongoing trail sampling for self and nearby AIS vessels.
- Clients consume trail deltas from backend APIs rather than sampling SignalK directly.
- The pre-anchor motoring approach is a separate concern from post-anchor trail history.

See [docs/adr/0001-server-owned-trail-sampling.md](docs/adr/0001-server-owned-trail-sampling.md), [docs/adr/0002-separate-motoring-and-anchor-trails.md](docs/adr/0002-separate-motoring-and-anchor-trails.md), and [docs/adr/0003-motoring-seed-downsampling.md](docs/adr/0003-motoring-seed-downsampling.md).

### Route Planning

Operators can plan multi-leg routes (waypoint sequences) with automatic per-leg distance/bearing/ETA, from the "Routes" tab in the bottom drawer or a glanceable dashboard tile. This is manual waypoint planning only — no hazard-avoidance or weather-optimized routing, and no chart licensing dependency.

See [docs/adr/0006-manual-route-planning.md](docs/adr/0006-manual-route-planning.md).

A saved route can also be Activated, which pushes it to the boat's SignalK server as the vessel's active route, so other NMEA2000 equipment (autopilots, MFDs) can follow it — the same mechanism Timezero Professional uses. HelmCentral itself still does no live navigation.

See [docs/adr/0007-signalk-route-activation.md](docs/adr/0007-signalk-route-activation.md).

### Configurable Dashboard Layout

The dashboard's 13 widgets can be rearranged, resized, shown, or hidden by operators without a code deployment. In layout mode (toggle in the header), operators can drag widgets to new positions, resize them, or remove them from the display; unplaced widgets can be added back via an "Add Widget" picker. The layout is persisted server-side and restored on the next session.

See [docs/adr/0012-configurable-bento-dashboard.md](docs/adr/0012-configurable-bento-dashboard.md).

### Tide Provider Plugins

Tide data comes from a pluggable `tideProvider` registry (`backend/tide_providers.go`), with one built-in provider (Storm Glass, using the vessel's current position). Every regional tide source — including BOM for Australia — ships as a WASM plugin instead, so a developer wanting to add another region's government tide API doesn't need to fork Helmcentral or touch Go at all: drop a compiled WASM plugin into `plugins/tides/` and it's picked up as a new provider in the existing Settings tide-provider dropdown on the next restart — zero frontend changes.

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

Unlike weather and waves (below), tides have **no universal keyless default** — tide data is tied to real physical station networks rather than a global forecast model, so there's no free API with genuinely worldwide coverage to hardcode. BOM and NOAA are both built automatically by the `plugins-builder` Compose service, but only cover Australia and the US respectively; Storm Glass has true global reach (it queries the vessel's live position rather than a fixed station list) but requires a paid `STORMGLASS_API_KEY`. An operator must explicitly pick `ui.tide_provider` in Settings to match their region or budget — nothing is silently assumed on their behalf, and `/api/tide-today` returns a clear error naming what's missing until they do.

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

(swap the directory for `docs/examples/weather-plugins/weatherkit` or `docs/examples/wave-plugins/open-meteo-marine` — note both are multi-file packages, so the build target is `.`, not a single `main.go`). The bundled `plugins-builder` Compose service (see `docker-compose.yml`/`docker-compose.dev.yml`) already builds and installs all five reference plugins (two tide, two weather, one wave) automatically on every `make dev`/deploy — see each plugin's own README for details and installation notes if building/installing manually.

**Upgrading an existing install:** if you previously ran a version of Helmcentral with the old hardcoded WeatherKit/Open-Meteo-marine integration, delete the now-orphaned `cache/weather_today_cache.json` and `cache/weather_forecast_cache.json` files (weather/wave caching moved to per-plugin files under `cache/weather_wasm_*_cache.json`/`cache/wave_wasm_*_cache.json`). If you were relying on WeatherKit, set the four `WEATHERKIT_*` environment variables on the backend (see the commented-out examples in `docker-compose.yml`) and select "Apple WeatherKit" under Settings → Weather — the default provider on upgrade is Open-Meteo (keyless) unless you explicitly configure otherwise.

## Next Steps

1. Configure SignalK connection parameters
2. Set up InfluxDB database schema
3. Implement dashboard data visualization components
4. Add authentication/authorization
5. Deploy to production environment

## Inspiration

Dashboard design inspired by modern maritime monitoring systems.

## License

UNLICENSED
