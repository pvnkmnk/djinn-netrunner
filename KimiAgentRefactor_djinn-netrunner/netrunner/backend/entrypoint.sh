#!/bin/sh
set -e

# Create necessary directories for safety
mkdir -p /app/config /app/data /app/logs /app/downloads /app/music

# Log which process is starting
echo "[NETRUNNER] Starting: $@"

# Execute the command (passed as args)
exec "$@"
