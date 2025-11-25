# Build stage
FROM golang:1.21-bookworm AS builder

# Install build dependencies for SQLite
RUN apt-get update && apt-get install -y gcc sqlite3 libsqlite3-dev && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build with CGO enabled for SQLite
RUN CGO_ENABLED=1 go build -o spotify-voting-app .

# Runtime stage - use Debian slim for compatibility
FROM debian:bookworm-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y ca-certificates sqlite3 && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Create data directory for database
RUN mkdir -p /data

# Copy the binary from builder
COPY --from=builder /app/spotify-voting-app .

# Copy static files
COPY --from=builder /app/static ./static

# Expose port
EXPOSE 8080

# Run the application
CMD ["./spotify-voting-app"]
