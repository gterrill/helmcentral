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
