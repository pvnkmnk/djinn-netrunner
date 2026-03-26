# ops/db/

## Responsibility
Database initialization scripts and SQL migrations for PostgreSQL deployments.

## Design
- `init/` — startup scripts run by Docker PostgreSQL entrypoint
  - `01-schema.sql` — base table definitions
  - `02-functions.sql` — SQL helper functions (reaper functions for zombie job cleanup)
  - `policy` — row-level security policies
- `init/migrations/` — versioned SQL migration files for schema evolution
- Migrations are safe/idempotent where feasible

## Flow
1. Docker PostgreSQL starts → runs `init/*.sql` in alphabetical order
2. Schema created → functions registered → policies applied
3. Application connects → GORM AutoMigrate handles Go-side schema updates

## Integration
- **Consumed by**: Docker Compose (PostgreSQL service), `cmd/server`/`cmd/worker` (GORM connection)
- **Complementary**: Go-side migrations via `internal/database/migrate.go`
