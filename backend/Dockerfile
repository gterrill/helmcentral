# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -trimpath -ldflags "-s -w" -o helmcentral .

# Final stage
# Pinned rather than :latest, so a rebuild months from now doesn't silently
# pick up a different Alpine major/minor with no record of which.
FROM alpine:3.24

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /app/helmcentral .

EXPOSE 8080

CMD ["./helmcentral"]
