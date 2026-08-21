# Build stage
FROM node:20-alpine AS builder

WORKDIR /app/frontend

COPY frontend/package*.json ./
RUN npm install

COPY frontend/. ./
COPY settings.yaml /app/settings.yaml
RUN npm run build

# Production stage
FROM node:20-alpine

WORKDIR /app

RUN npm install -g http-server

COPY --from=builder /app/frontend/dist ./dist

EXPOSE 5173

CMD ["http-server", "dist", "-p", "5173", "-c-1"]
