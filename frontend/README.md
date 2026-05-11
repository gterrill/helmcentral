# Helmcentral Dashboard Frontend

TypeScript + Web Components dashboard for Helmcentral.

## Features

- Modern Web Components architecture
- TypeScript for type safety
- Vite for fast development and building
- Real-time data visualization from SignalK and InfluxDB
- Responsive dashboard design

## Development

### Prerequisites

- Node.js 18+
- npm or yarn

### Installation

```bash
npm install
```

### Development Server

```bash
npm run dev
```

The dashboard will be available at `http://localhost:5173`

### Building

```bash
npm run build
```

Output files will be in the `dist/` directory.

### Components

Components are located in `src/components/` and are built as Web Components using the Custom Elements API.

#### dashboard-container

Main container component that manages the dashboard layout.

## API Integration

The frontend proxies API calls to the backend via Vite's proxy configuration. All requests to `/api` are forwarded to `http://localhost:8080`.
