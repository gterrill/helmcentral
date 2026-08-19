# ── Stage 1: build frontend ──────────────────────────────────────────────────
# --platform=$BUILDPLATFORM: the output is static JS/CSS, identical for every
# target arch, so this stage always runs natively on the builder. It also has
# to: node:24-alpine publishes no linux/arm/v7 manifest, so pinning it to the
# target platform breaks the armv7 leg of the multi-arch build outright.
FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend-builder

WORKDIR /app/frontend

COPY frontend/package*.json ./
RUN npm ci

COPY frontend/. ./

# Empty string makes all /api calls relative so they hit the same-origin backend
ENV VITE_API_BASE_URL=""
RUN npm run build

# ── Stage 2: build backend with embedded frontend ────────────────────────────
# --platform=$BUILDPLATFORM again: CGO is off, so the Go toolchain cross-
# compiles every target natively. Letting buildx pick the target platform here
# instead would compile Go under QEMU emulation for the two arm legs, which is
# an order of magnitude slower for an identical binary.
FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS backend-builder

WORKDIR /app

ARG APP_VERSION=dev
ARG APP_REVISION=unknown

# Supplied by buildx per target: linux/arm/v7 arrives as arm + v7.
ARG TARGETARCH
ARG TARGETVARIANT

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/. .

# Replace the empty dist placeholder with the real frontend build
RUN rm -rf dist
COPY --from=frontend-builder /app/frontend/dist ./dist

# GOARM is set only for 32-bit arm, where it selects the floating-point ABI;
# it is meaningless on amd64 and arm64, so it stays unset there.
RUN set -eu; \
	if [ "$TARGETARCH" = "arm" ]; then export GOARM="${TARGETVARIANT#v}"; fi; \
	CGO_ENABLED=0 GOOS=linux GOARCH="$TARGETARCH" go build -a -installsuffix cgo \
		-ldflags "-X main.buildVersion=${APP_VERSION} -X main.buildRevision=${APP_REVISION}" \
		-o helmcentral .

# ── Stage 3: minimal runtime image ───────────────────────────────────────────
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=backend-builder /app/helmcentral .

EXPOSE 8080

CMD ["./helmcentral"]
