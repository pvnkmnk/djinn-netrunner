# Stage 1: Builder
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache gcc musl-dev

# Set working directory
WORKDIR /app

# Copy go mod files first for dependency caching
COPY backend/go.mod backend/go.sum ./
RUN go mod download

# Copy all source code
COPY backend/ ./

# Build both binaries
RUN go build -o netrunner-server ./cmd/server/main.go && \
    go build -o netrunner-worker ./cmd/worker/main.go

# Copy web assets from ops directory
COPY ops/web/static ./static
COPY ops/web/templates ./templates

# Copy entrypoint script
COPY backend/entrypoint.sh ./entrypoint.sh
RUN chmod +x entrypoint.sh

# Stage 2: Runtime
FROM alpine:latest

# Install wget for healthcheck
RUN apk add --no-cache wget

# Create app directory
WORKDIR /app

# Copy binaries from builder
COPY --from=builder /app/netrunner-server ./
COPY --from=builder /app/netrunner-worker ./
COPY --from=builder /app/static ./static
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/entrypoint.sh /entrypoint.sh

# Make binaries and entrypoint executable
RUN chmod +x netrunner-server netrunner-worker /entrypoint.sh

# Create necessary directories with correct ownership
RUN mkdir -p /app/config /app/data /app/logs /app/downloads /app/music && \
    addgroup -S netrunner && adduser -S netrunner -G netrunner && \
    chown -R netrunner:netrunner /app

# Set environment variables
ENV STATIC_FILES_PATH=/app/static
ENV TEMPLATES_PATH=/app/templates
ENV PORT=8080

# Expose port
EXPOSE 8080

# Create volumes for persistent data
VOLUME /app/config /app/data /app/logs /app/downloads /app/music

# Healthcheck
HEALTHCHECK --interval=30s --timeout=10s --start-period=20s --retries=3 CMD wget -qO- http://localhost:8080/api/health || exit 1

# Switch to non-root user
USER netrunner

# Use entrypoint script directly (no tini needed for Go apps)
ENTRYPOINT ["/entrypoint.sh"]
