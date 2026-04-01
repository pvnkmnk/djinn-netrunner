# ops/db/init/

## Responsibility
Initial database setup: schema, functions, and migrations for fresh PostgreSQL deployments.

## Design
| File | Role |
|------|------|
| `01-schema.sql` | Creates tables: jobs, jobitems, joblogs, acquisitions, sources + indexes |
| `02-functions.sql` | DB functions: job claiming, logging helpers, notification triggers |
| `MIGRATION_POLICY.md` | Documents dual strategy (AutoMigrate + SQL init) |
| `migrations/` | Incremental schema changes for existing databases |

## Flow
1. Runs once on first database container start (Docker entrypoint)
2. Creates enums: jobtype, jobstate, jobitemstatus
3. Sets up triggers for pg_notify on job events
4. Defines SKIP LOCKED pattern for concurrent job claiming

## Integration
- **PostgreSQL**: Initializes fresh `musicops` database
- **Backend**: Schema is source of truth for GORM models