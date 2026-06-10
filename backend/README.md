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
docker compose --profile dev up backend-dev
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
- `GET /api/weather-forecast` - 6-day daily forecast including sustained wind and gusts
- `GET /api/tide-today` - Current and upcoming tide conditions
- `GET /api/tracks?since=<RFC3339>` - Incremental self and AIS trail updates sampled by the server
- `GET /api/tracks/motoring` - Motoring approach trail used by anchor reposition mode

The vessel-state payload includes GNSS validation fields that the client uses to freeze anchor-watch calculations when the fix is corrupt or lost:

- `gnss_quality_indicator` - Normalized NMEA quality/fix indicator. Common values: `1` GPS, `2` DGPS, `8` simulation.
- `gnss_hdop` - Horizontal dilution of precision.
- `gnss_validation_state` - `trusted`, `degraded`, or `critical`.
- `gnss_critical_alert` - `true` when the fix should be treated as unsafe for anchor alarm evaluation.

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
