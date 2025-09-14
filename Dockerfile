## Dockerfile for cinema-seat-reservation API
# Use a two-stage build to produce a small runtime image. The builder
# stage compiles the Go binary with CGO disabled for maximum
# portability. The final stage runs the compiled binary under a
# minimal Alpine Linux base. Environment variables for database,
# Redis and RabbitMQ can be overridden at runtime via docker-compose.

# ---------- Build stage ----------
# Use the latest Go version available at the time of writing.  The go.mod
# file specifies `go 1.24`, so we align the builder image with a 1.24
# series tag.  This avoids compilation issues stemming from language
# features introduced after Go 1.22 (e.g. generic improvements and
# standard library additions).  Should a newer patch release be
# available, Docker will pull it automatically.
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Cache Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the binary. The GOOS and GOARCH are set explicitly to produce a
# Linux binary. CGO is disabled so the binary is fully static.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/app ./cmd/server

# ---------- Runtime stage ----------
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata && update-ca-certificates

# Create a non-root user for running the application
RUN addgroup -S app && adduser -S -G app app
USER app

WORKDIR /home/app

# Copy the compiled binary from the builder stage
COPY --from=builder /bin/app /home/app/app

# Create the logs directory used by the queue consumer
RUN mkdir -p /home/app/logs

# Expose the API port
EXPOSE 8080

# Default environment variables (can be overridden via docker-compose)
ENV APP_ENV=dev \
    APP_PORT=8080 \
    DB_HOST=localhost \
    DB_PORT=3306 \
    DB_NAME=cinema \
    DB_USER=root \
    DB_PASS=secret \
    REDIS_HOST=localhost \
    REDIS_PORT=6379 \
    RABBITMQ_URL=amqp://guest:guest@localhost:5672/ \
    JWT_SECRET=super-secret-change-me

# Command to run the application
CMD ["/home/app/app"]