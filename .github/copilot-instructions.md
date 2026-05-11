## Project Setup Checklist: Helmcentral Dashboard

A SignalK/InfluxDB dashboard with Go backend and TypeScript Web Components frontend.

### ✅ Project Initialization Complete

- [x] Git repository initialized
- [x] Backend (Go) scaffolded with Echo framework
- [x] Frontend (TypeScript + Web Components) scaffolded with Vite
- [x] Docker Compose configuration created
- [x] Project documentation created

### Backend Setup

- [x] Go module initialized (go.mod)
- [x] Basic server implementation with health check endpoint
- [x] CORS middleware configured
- [x] Docker support with multi-stage build
- [x] .gitignore for Go project

**To complete backend setup:**
1. Run `cd backend && go mod download` to fetch dependencies
2. See [backend/README.md](../backend/README.md) for development instructions

### Frontend Setup

- [x] Vite configuration
- [x] TypeScript configuration
- [x] Initial Web Component (dashboard-container)
- [x] HTML entry point
- [x] Development server proxy to backend API

**To complete frontend setup:**
1. Run `cd frontend && npm install`
2. Run `npm run dev` to start development server
3. See [frontend/README.md](../frontend/README.md) for more information

### Next Steps

1. **Backend Development**:
   - Implement SignalK data fetching
   - Implement InfluxDB query interfaces
   - Add API endpoints for dashboard data

2. **Frontend Development**:
   - Create data visualization components
   - Implement real-time data updates
   - Build dashboard layout and widgets

3. **Integration**:
   - Connect frontend to backend API
   - Set up data pipelines
   - Configure production deployment

### Running Locally

**Option 1: Docker Compose (Recommended)**
```bash
docker-compose up
```

**Option 2: Local Development**
```bash
# Terminal 1: Backend
cd backend && go run main.go

# Terminal 2: Frontend
cd frontend && npm install && npm run dev
```

### Useful Commands

- Backend health check: `curl http://localhost:8080/api/health`
- Frontend: http://localhost:5173
- InfluxDB admin: http://localhost:8086
