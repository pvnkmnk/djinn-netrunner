# backend/internal/database/

## Responsibility
Data access layer. GORM-based database connection, schema models, migrations, advisory locking, and LiteFS distributed deployment support.

## Design
- **connection.go**: `Connect(cfg)` → `*gorm.DB` using CGO-free SQLite (`modernc.org/sqlite`) or PostgreSQL
- **models.go**: All GORM models — `User`, `Session`, `Watchlist`, `WatchlistItem`, `Schedule`, `Job`, `JobItem`, `MonitoredArtist`, `QualityProfile`, `Library`, `Track`, `Acquisition`
- **migrate.go**: `Migrate(db)` — auto-migrates all models
- **locks.go**: `LockManager` interface with `Acquire(scope)`, `Release(scope)` — SQLite uses table-based locks, PostgreSQL uses `pg_advisory_lock`
- **litefs.go**: `LiteFSGuard` for primary detection in distributed SQLite deployments
- **models_helper.go**: Helper methods on models (e.g., `Job.IsTerminal()`)

## Flow
1. `Connect()` → open GORM connection → configure SQLite WAL mode, busy timeout
2. `Migrate()` → `db.AutoMigrate(all models)` → create/update schema
3. LockManager: `Acquire()` → INSERT into locks table (SQLite) or `pg_advisory_lock` (PostgreSQL)
4. Queries: all via GORM (`db.Where().Find()`, `db.Create()`, etc.)

## Integration
- **Consumed by**: All `internal/*` packages, all `cmd/*` entry points
- **External**: SQLite file or PostgreSQL connection
- **Invariant**: CGO-free SQLite via `modernc.org/sqlite`, WAL mode mandatory
