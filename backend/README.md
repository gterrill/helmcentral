# Helmcentral Backend

Go REST API backend for the Helmcentral dashboard.

## Features

- SignalK data integration
- In-memory telemetry history by default; InfluxDB optional for longer retention
- RESTful API endpoints
- CORS-enabled for web frontend

## Development

### Prerequisites

- Go 1.22+

### Building

```bash
go build -o helmcentral .
```

### Running

```bash
./helmcentral
```

Or straight from source — `go run .`, not `go run main.go`: `package main`
spans every file in this directory, so naming one file fails to compile.

```bash
go run .
```

The API will be available at `http://localhost:8080`

### Dev Hot Reload

When running through Docker Compose `backend-dev`, the backend uses Air for code reloads. Air is baked into a dev image (`backend/Dockerfile.dev`), so it does not reinstall on every container start:

```bash
docker compose -f docker-compose.dev.yml --profile dev up backend-dev
```

Any changes under `backend/` will trigger a rebuild and restart automatically.

### Docker

```bash
docker build -t helmcentral-backend .
docker run -p 8080:8080 helmcentral-backend
```

## API Endpoints

- `GET /api/health` - Health check
- `POST /api/auth/login` / `POST /api/auth/logout` / `GET /api/auth/me` / `GET /api/auth/mode` - SignalK delegated login; see [../docs/adr/0040-signalk-delegated-authentication.md](../docs/adr/0040-signalk-delegated-authentication.md)
- `GET /api/weather-today` - Current weather summary for vessel position
- `GET /api/weather-forecast` - 6-day daily forecast including sustained wind and gusts. Response shape includes `days`, `cached`, `updated_at`, and `ttl_seconds`. Cached for 60 minutes per rounded vessel position; returns `502` when upstream forecast data cannot be fetched.
- `GET /api/tide-today` - Current and upcoming tide conditions
- `GET /api/tracks?since=<RFC3339>` - Incremental self and AIS trail updates sampled by the server
- `GET /api/tracks/motoring` - Motoring approach trail used by anchor reposition mode
- `GET /api/routes` - List saved routes
- `POST /api/routes` - Create a route (`name`, `waypoints`)
- `GET /api/routes/:id` - Fetch a single route
- `PATCH /api/routes/:id` - Update a route's name and/or waypoints
- `DELETE /api/routes/:id` - Delete a route
- `POST /api/routes/:id/activate` - Push a route to SignalK and set it as the vessel's active route
- `POST /api/routes/deactivate` - Clear the active route on SignalK
- `GET /api/routes/active` - Current active-route status from SignalK

## Configuration

Non-secret knobs are set as real environment variables. There is no `.env` file and nothing loads one — the backend reads these via `os.Getenv` only, so putting them in a file has no effect. Set them in your shell, the compose service's `environment:` block, or your process manager.

| Variable | Default | Purpose |
| --- | --- | --- |
| `PORT` | `8080` | HTTP listen port |
| `SETTINGS_FILE` | `../settings.yaml` | Path to the settings file |
| `SIGNALK_VESSEL_PATH` | `/signalk/v1/api/vessels/self` | Self-vessel path on the SignalK server |
| `VESSEL_STATUS` | `At Anchor` | Fallback status when SignalK reports none |
| `HELMCENTRAL_STATE_DIR` | *(unset)* | Roots all runtime state (`data/`, `cache/`) at this directory instead of the working directory. Used by the E2E stack to keep its writes out of the working tree — see [../docs/adr/0026-e2e-stack-isolation.md](../docs/adr/0026-e2e-stack-isolation.md) |
| `HELMCENTRAL_MASTER_KEY` | *(auto-generated)* | Override for the secrets-store encryption key, otherwise generated on first run |

Individual state paths (`ROUTES_FILE`, `SECRETS_DB_PATH`, `TILE_CACHE_PATH`, …) can each be overridden by name and take precedence over `HELMCENTRAL_STATE_DIR`; see `cacheFilePath` in `weather_tide.go`.

Secrets (SignalK credentials, `INFLUXDB_TOKEN`, `GEONAMES_USERNAME`, `WEATHERKIT_*`) live in an encrypted-at-rest SQLite store instead, managed via the Settings UI's Secrets panel (`GET`/`POST /api/settings/secrets`). See [../docs/adr/0023-encrypted-secrets-store.md](../docs/adr/0023-encrypted-secrets-store.md).

Wind-gust-max and depth-trend/tide-detection work out of the box from an in-memory ring buffer fed by the server's live SignalK polling — no InfluxDB required. InfluxDB is an optional enhancement (longer retention than the in-memory windows — ~24h for wind gust, ~6h for depth — and history that survives a restart) configured from the Settings UI's InfluxDB section (`enabled`/`url`/`org`/`bucket`, stored in `settings.yaml`'s `influxdb` section) plus the `INFLUXDB_TOKEN` secret. See [../docs/adr/0020-in-memory-telemetry-history-optional-influxdb.md](../docs/adr/0020-in-memory-telemetry-history-optional-influxdb.md).

## Architecture Decisions

The durable rationale for trail handling is documented in:

- [../docs/adr/0001-server-owned-trail-sampling.md](../docs/adr/0001-server-owned-trail-sampling.md)
- [../docs/adr/0002-separate-motoring-and-anchor-trails.md](../docs/adr/0002-separate-motoring-and-anchor-trails.md)
- [../docs/adr/0003-motoring-seed-downsampling.md](../docs/adr/0003-motoring-seed-downsampling.md) (superseded by ADR-0020)
- [../docs/adr/0004-gnss-validation-gate.md](../docs/adr/0004-gnss-validation-gate.md)
- [../docs/adr/0005-signalk-tracks-backed-ais-trails.md](../docs/adr/0005-signalk-tracks-backed-ais-trails.md)
- [../docs/adr/0006-manual-route-planning.md](../docs/adr/0006-manual-route-planning.md)
- [../docs/adr/0007-signalk-route-activation.md](../docs/adr/0007-signalk-route-activation.md)
- [../docs/adr/0012-configurable-bento-dashboard.md](../docs/adr/0012-configurable-bento-dashboard.md)
- [../docs/adr/0020-in-memory-telemetry-history-optional-influxdb.md](../docs/adr/0020-in-memory-telemetry-history-optional-influxdb.md)
- [../docs/adr/0023-encrypted-secrets-store.md](../docs/adr/0023-encrypted-secrets-store.md)
- [../docs/adr/0040-signalk-delegated-authentication.md](../docs/adr/0040-signalk-delegated-authentication.md)
- [../docs/adr/0024-plugin-descriptions-and-allowlist-overrides.md](../docs/adr/0024-plugin-descriptions-and-allowlist-overrides.md)
- [../docs/adr/0033-remove-storm-glass-tides-plugin-only.md](../docs/adr/0033-remove-storm-glass-tides-plugin-only.md)
