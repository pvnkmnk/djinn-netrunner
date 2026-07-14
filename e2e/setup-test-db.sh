#!/bin/bash
# E2E test environment setup script
# Called by Playwright webserver to prepare the Docker stack for E2E tests

set -euo pipefail

COMPOSE="docker compose --env-file ../.env.e2e -f ../docker-compose.yml -f ../docker-compose.e2e.yml"

echo "=== Building Docker images ==="
$COMPOSE build

# Start postgres first (need the database before ops-web connects)
echo "=== Starting postgres ==="
$COMPOSE up -d postgres
until $COMPOSE exec -T postgres pg_isready -U musicops 2>/dev/null; do
  sleep 1
done

# Create fresh test database (DB must exist before ops-web starts or it will crash-loop)
echo "=== Creating musicops_test database ==="
# DROP and CREATE must be separate commands (DROP DATABASE cannot run in transaction block)
$COMPOSE exec -T postgres psql -U musicops -c "DROP DATABASE IF EXISTS musicops_test;" 2>/dev/null || true
$COMPOSE exec -T postgres psql -U musicops -c "CREATE DATABASE musicops_test;" 2>&1

# Start all other services
echo "=== Starting remaining services ==="
$COMPOSE up -d ops-web ops-worker caddy

# Wait for ops-web to be healthy (this means AutoMigrate completed successfully)
echo "=== Waiting for ops-web to be healthy ==="
for i in $(seq 1 30); do
  if curl -sf http://localhost:8080/api/health 2>/dev/null | grep -q '"status":"ok"'; then
    echo "ops-web is healthy"
    break
  fi
  if [ "$i" -eq 30 ]; then
    echo "ERROR: ops-web failed to become healthy after 60 seconds"
    $COMPOSE logs ops-web --tail=20
    exit 1
  fi
  sleep 2
done

# Seed admin user (users table created by AutoMigrate)
echo "=== Seeding admin user ==="
$COMPOSE exec -T postgres psql -U musicops -d musicops_test -c "
INSERT INTO users (email, password_hash, role, created_at, updated_at)
SELECT 'e2e-admin@netrunner.dev', '\$2a\$10\$DAbZ8zqRgGGkdgDfkV0FduOIxRBfrrqjV7q4GYC/gf1z/Wtkg672m', 'admin', NOW(), NOW()
WHERE NOT EXISTS (SELECT 1 FROM users WHERE email = 'e2e-admin@netrunner.dev');
" 2>/dev/null || echo "Warning: Could not seed admin user"

# Seed a default quality profile needed by watchlist and schedule tests
echo "=== Seeding default quality profile ==="
$COMPOSE exec -T postgres psql -U musicops -d musicops_test -c "
INSERT INTO quality_profiles (id, name, description, prefer_lossless, allowed_formats, min_bitrate, is_default, created_at, updated_at)
SELECT '11111111-1111-4111-8111-111111111111', 'Default', 'Default E2E test profile', true, 'flac,mp3,wav', 320, true, NOW(), NOW()
WHERE NOT EXISTS (SELECT 1 FROM quality_profiles WHERE name = 'Default');
" 2>/dev/null || echo "Warning: Could not seed quality profile"

echo "=== E2E setup complete ==="
