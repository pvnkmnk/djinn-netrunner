# ops/db/

## Responsibility
Database initialization scripts and migration management for NETRUNNER's PostgreSQL database.

## Design
| File/Directory | Role |
|----------------|------|
| `init/01-schema.sql` | Core schema: jobs, jobitems, joblogs, acquisitions, sources tables |
| `init/02-functions.sql` | Helper functions: job claiming (SKIP LOCKED), notifications, logging |
| `init/MIGRATION_POLICY.md` | Documentation of migration strategy |
| `init/migrations/` | Incremental SQL migrations for schema changes |

## Flow
1. `01-schema.sql` + `02-functions.sql` run once on fresh database init (docker-entrypoint-initdb.d)
2. Runtime migrations handled by GORM AutoMigrate (see `backend/internal/database/migrate.go`)
3. Migrations in `migrations/` add columns via `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`

## Integration
- **Consumed by**: PostgreSQL container via Docker entrypoint
- **Backend**: GORM models in `backend/internal/database/models.go` define authoritative schema
- **Workers**: Use `claim_next_job()` and `claim_next_jobitem()` functions for queue processing