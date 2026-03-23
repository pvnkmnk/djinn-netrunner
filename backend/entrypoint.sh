#!/bin/sh
set -e

# Default environment variables with fallbacks
DATABASE_URL="${DATABASE_URL:-}"
SLSKD_URL="${SLSKD_URL:-}"
SLSKD_API_KEY="${SLSKD_API_KEY:-}"
GONIC_URL="${GONIC_URL:-}"
PORT="${PORT:-8080}"
STATIC_FILES_PATH="${STATIC_FILES_PATH:-/app/static}"
TEMPLATES_PATH="${TEMPLATES_PATH:-/app/templates}"
DOWNLOAD_STAGING="${DOWNLOAD_STAGING:-/app/downloads}"
MUSIC_LIBRARY="${MUSIC_LIBRARY:-/app/music}"
WORKER_ID="${WORKER_ID:-}"
SPOTIFY_CLIENT_ID="${SPOTIFY_CLIENT_ID:-}"
SPOTIFY_CLIENT_SECRET="${SPOTIFY_CLIENT_SECRET:-}"

# Startup banner
echo "🚀 NetRunner Container Starting..."
echo "=========================================="

# Log environment configuration
echo "[ENV] DATABASE_URL: ${DATABASE_URL:-(not set)}"
echo "[ENV] SLSKD_URL: ${SLSKD_URL:-(not set)}"
echo "[ENV] SLSKD_API_KEY: ${SLSKD_API_KEY:+***SET***}"
echo "[ENV] GONIC_URL: ${GONIC_URL:-(not set)}"
echo "[ENV] PORT: ${PORT}"
echo "[ENV] STATIC_FILES_PATH: ${STATIC_FILES_PATH}"
echo "[ENV] TEMPLATES_PATH: ${TEMPLATES_PATH}"
echo "[ENV] DOWNLOAD_STAGING: ${DOWNLOAD_STAGING}"
echo "[ENV] MUSIC_LIBRARY: ${MUSIC_LIBRARY}"
echo "[ENV] WORKER_ID: ${WORKER_ID:-(not set)}"
echo "[ENV] SPOTIFY_CLIENT_ID: ${SPOTIFY_CLIENT_ID:+***SET***}"
echo "[ENV] SPOTIFY_CLIENT_SECRET: ${SPOTIFY_CLIENT_SECRET:+***SET***}"
echo "=========================================="

# Create necessary directories if missing
echo "[INIT] Creating directories..."
mkdir -p "${STATIC_FILES_PATH}" "${TEMPLATES_PATH}" "${DOWNLOAD_STAGING}" "${MUSIC_LIBRARY}" /app/config /app/data /app/logs

# Change to app directory
cd /app

# Start the worker in background
echo "[START] Launching worker process..."
./netrunner-worker &
WORKER_PID=$!
echo "[WORKER] Started with PID: ${WORKER_PID}"

# Start the server with exec (replaces shell process for proper signal propagation)
echo "[START] Launching server process..."
exec ./netrunner-server "$@"