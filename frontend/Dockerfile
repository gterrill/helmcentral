# Build stage
FROM node:18-alpine AS builder

WORKDIR /app

COPY package*.json ./
RUN npm install

COPY . .
RUN npm run build

# Production stage
FROM node:18-alpine

WORKDIR /app

RUN npm install -g http-server

COPY --from=builder /app/dist ./dist

EXPOSE 5173

CMD ["http-server", "dist", "-p", "5173", "-c-1"]
