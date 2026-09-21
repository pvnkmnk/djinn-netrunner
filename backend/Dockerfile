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

# The release version is stamped into every binary rather than hardcoded, so
# the page footer cannot drift from the tag the image was built from. Compose
# passes the tag through APP_VERSION; a build never told reports "dev".
ARG APP_VERSION=dev

# Build the operator binaries. netrunner-cli ships too: the repair runbooks
# (ops/docs/library-dedup-runbook.md, ops/docs/disaster-recovery.md) run it
# against a deployed stack, and without it here the operator has to copy a
# host binary into the container by hand.
RUN VERSION="${APP_VERSION#v}" && \
    LDFLAG="-X github.com/pvnkmnk/netrunner/backend/internal/api.AppVersion=$VERSION" && \
    go build -ldflags "$LDFLAG" -o netrunner-server ./cmd/server/main.go && \
    go build -ldflags "$LDFLAG" -o netrunner-worker ./cmd/worker/main.go && \
    go build -ldflags "$LDFLAG" -o netrunner-cli ./cmd/cli/main.go

# Copy web assets from ops directory
COPY ops/web/static ./static
COPY ops/web/templates ./templates

# Copy entrypoint script
COPY backend/entrypoint.sh ./entrypoint.sh
RUN chmod +x entrypoint.sh

# Stage 2: Runtime
FROM alpine:3.21

# Install runtime dependencies: wget, Node.js for yt-dlp JS sites, Python/pip for yt-dlp,
# ffmpeg for transcoding and M4A/OGG tag writes (FFmpegTagger)
# --break-system-packages is safe here (disposable Docker container, not shared system)
RUN apk add --no-cache wget nodejs npm python3 py3-pip ffmpeg && \
    pip3 install --break-system-packages --upgrade yt-dlp

# Create app directory
WORKDIR /app

# Copy binaries from builder
COPY --from=builder /app/netrunner-server ./
COPY --from=builder /app/netrunner-worker ./
COPY --from=builder /app/netrunner-cli ./
COPY --from=builder /app/static ./static
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/entrypoint.sh /entrypoint.sh

# Make binaries and entrypoint executable
RUN chmod +x netrunner-server netrunner-worker netrunner-cli /entrypoint.sh

# Create necessary directories with correct ownership
RUN mkdir -p /app/config /app/data /app/logs /app/downloads /app/music && \
    addgroup -S -g 1000 netrunner && adduser -S -u 1000 -G netrunner netrunner && \
    chown -R netrunner:netrunner /app

# Set environment variables
ENV STATIC_FILES_PATH=/app/static
ENV TEMPLATES_PATH=/app/templates
ENV PORT=8080
ENV YTDLP_PATH=/usr/bin/yt-dlp

# Expose port
EXPOSE 8080

# Create volumes for persistent data
VOLUME /app/config /app/data /app/logs /app/downloads /app/music

# Switch to non-root user
USER netrunner

# Entrypoint handles directory setup, then runs the command
ENTRYPOINT ["/entrypoint.sh"]

# Default command: start the server
CMD ["./netrunner-server"]
