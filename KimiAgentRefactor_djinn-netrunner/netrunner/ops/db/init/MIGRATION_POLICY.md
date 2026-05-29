# Database Migration Policy

## Overview
NetRunner uses two complementary migration strategies:

1. **AutoMigrate (runtime)** — Primary strategy
2. **SQL Init Scripts** — Bootstrap for fresh Postgres deployments

## AutoMigrate (Runtime)
The Go application uses GORM's `AutoMigrate` on startup (see `backend/internal/database/migrate.go`).
This is the **authoritative** migration path for all schema changes. AutoMigrate handles:
- New tables
- New columns
- Index changes

AutoMigrate is non-destructive: it only adds missing columns/tables, it never drops data.

## SQL Init Scripts (Bootstrap)
The `migrations/` directory contains SQL scripts for initializing a fresh PostgreSQL database
via Docker's `docker-entrypoint-initdb.d` mechanism. These scripts run once on first boot.

For idempotency, use `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`.

## Adding a New Column
1. Add the field to the GORM model in `backend/internal/database/models.go` — AutoMigrate picks it up
2. Create a new migration script in `migrations/` with `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`
3. Name convention: `YYYY_MM_DD_NNN_description.sql`

## Column Addition Pattern
```sql
-- Example: add nullable column to existing table
ALTER TABLE tracks ADD COLUMN IF NOT EXISTS fingerprint TEXT;
ALTER TABLE acquisitions ADD COLUMN IF NOT EXISTS acoustid_score INT DEFAULT 0;
ALTER TABLE libraries ADD COLUMN IF NOT EXISTS max_size_bytes BIGINT;
ALTER TABLE libraries ADD COLUMN IF NOT EXISTS quota_alert_at INT;
```

## Known Fields Added via AutoMigrate (Phase 8)
- `tracks.fingerprint` — AcoustID audio fingerprint
- `acquisitions.acoustid_score` — AcoustID confidence score (0-100)
- `libraries.max_size_bytes` — Optional per-library disk quota cap
- `libraries.quota_alert_at` — Alert threshold percentage (default 80)
