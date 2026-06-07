# ── Stage 1: build frontend ──────────────────────────────────────────────────
FROM node:24-alpine AS frontend-builder

WORKDIR /app/frontend

COPY frontend/package*.json ./
RUN npm ci

COPY frontend/. ./

# Empty string makes all /api calls relative so they hit the same-origin backend
ENV VITE_API_BASE_URL=""
RUN npm run build

# ── Stage 2: build backend with embedded frontend ────────────────────────────
FROM golang:1.22-alpine AS backend-builder

WORKDIR /app

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/. .

# Replace the empty dist placeholder with the real frontend build
RUN rm -rf dist
COPY --from=frontend-builder /app/frontend/dist ./dist

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o helmcentral .

# ── Stage 3: minimal runtime image ───────────────────────────────────────────
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=backend-builder /app/helmcentral .

EXPOSE 8080

CMD ["./helmcentral"]
