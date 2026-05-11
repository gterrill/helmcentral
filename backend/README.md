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

### Docker

```bash
docker build -t helmcentral-backend .
docker run -p 8080:8080 helmcentral-backend
```

## API Endpoints

- `GET /api/health` - Health check

## Configuration

Environment variables:
- `PORT` - Server port (default: 8080)
- `INFLUXDB_URL` - InfluxDB connection URL
- `SIGNALK_URL` - SignalK server URL
