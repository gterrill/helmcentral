# Helmcentral Backend

Go REST API backend for the Helmcentral dashboard.

## Features

- SignalK data integration
- InfluxDB data querying
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

Create a `.env` file in the backend directory. See `.env.example` for a template.

Environment variables:
- `PORT` - Server port (default: 8080)
- `ENVIRONMENT` - Environment name (development/production)
- `LOG_LEVEL` - Logging level (debug/info/warn/error)
- `INFLUXDB_URL` - InfluxDB connection URL
- `INFLUXDB_ORG` - InfluxDB organization
- `INFLUXDB_BUCKET` - InfluxDB bucket for time-series data
- `INFLUXDB_TOKEN` - InfluxDB API token
- `SIGNALK_URL` - SignalK server URL

## Architecture Decisions

The durable rationale for trail handling is documented in:

- [../docs/adr/0001-server-owned-trail-sampling.md](../docs/adr/0001-server-owned-trail-sampling.md)
- [../docs/adr/0002-separate-motoring-and-anchor-trails.md](../docs/adr/0002-separate-motoring-and-anchor-trails.md)
- [../docs/adr/0003-motoring-seed-downsampling.md](../docs/adr/0003-motoring-seed-downsampling.md)
- [../docs/adr/0004-gnss-validation-gate.md](../docs/adr/0004-gnss-validation-gate.md)
- [../docs/adr/0005-signalk-tracks-backed-ais-trails.md](../docs/adr/0005-signalk-tracks-backed-ais-trails.md)
- [../docs/adr/0006-manual-route-planning.md](../docs/adr/0006-manual-route-planning.md)
- [../docs/adr/0007-signalk-route-activation.md](../docs/adr/0007-signalk-route-activation.md)
- [../docs/adr/0012-configurable-bento-dashboard.md](../docs/adr/0012-configurable-bento-dashboard.md)
