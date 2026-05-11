# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod . ./
COPY main.go .
RUN go mod tidy

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o helmcentral .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/helmcentral .

EXPOSE 8080

CMD ["./helmcentral"]
