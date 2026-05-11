# Helmcentral

A modern dashboard application integrating SignalK and InfluxDB for marine monitoring and visualization.

## Project Structure

```
helmcentral/
├── backend/          # Go REST API
├── frontend/         # TypeScript Web Components Dashboard
├── docker-compose.yml
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

### Production Mode (Docker)

```bash
docker compose up --build -d
```

- Backend API: http://localhost:8080
- Frontend Dashboard: http://localhost:5173

To stop:

```bash
docker compose down
```

### Development Mode (Docker Profiles)

```bash
docker compose --profile dev up -d backend-dev frontend-dev
```

- Backend Dev API: http://localhost:8081
- Frontend Dev Dashboard (Vite): http://localhost:5174

To stop dev services:

```bash
docker compose --profile dev down
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
