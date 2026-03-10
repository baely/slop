# Build stage
FROM golang:alpine AS builder

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev sqlite-dev

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the server binary
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o staticer-server ./cmd/server

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates sqlite-libs wget

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/staticer-server .

# Copy web assets (embedded, but keeping for reference)
COPY --from=builder /build/web ./web

# Create directories for data and sites
RUN mkdir -p /app/data /app/sites

# Expose port
EXPOSE 8080

# Health check (just verify process is running)
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD ps | grep staticer-server | grep -v grep || exit 1

# Run as non-root user
RUN addgroup -g 1000 staticer && \
    adduser -D -u 1000 -G staticer staticer && \
    chown -R staticer:staticer /app

USER staticer

CMD ["./staticer-server"]
