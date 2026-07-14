#!/bin/bash
# Wait for Postgres to be ready
until docker compose --env-file ../.env.e2e -f ../docker-compose.yml -f ../docker-compose.e2e.yml exec -T postgres pg_isready -U musicops; do
  sleep 1
done

# Create test database
docker compose --env-file ../.env.e2e -f ../docker-compose.yml -f ../docker-compose.e2e.yml exec -T postgres psql -U musicops -c "DROP DATABASE IF EXISTS musicops_test;"
docker compose --env-file ../.env.e2e -f ../docker-compose.yml -f ../docker-compose.e2e.yml exec -T postgres psql -U musicops -c "CREATE DATABASE musicops_test;"

echo "Test database ready"
