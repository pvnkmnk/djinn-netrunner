# Upgrade Runbook

## Standard Upgrade

```bash
# 1. Pull latest code
cd /path/to/djinn-netrunner
git pull origin master

# 2. Pull updated container images
docker compose pull

# 3. Rebuild and restart
docker compose up -d --build
```

### What happens on startup

1. **GORM AutoMigrate** runs automatically — adds new columns, creates new tables
2. **SQL migrations** in `ops/db/init/` run on fresh Postgres databases only (via Docker init scripts)
3. **No destructive migrations** — columns are never dropped automatically; manual SQL may be needed for column renames or type changes

### Downtime expectations

| Component | Downtime |
|---|---|
| `ops-web` (API/UI) | ~10-30s during container rebuild |
| `ops-worker` (background jobs) | ~10-30s; in-progress jobs will be marked as zombies and cleaned up automatically |
| `postgres` | Zero (persistent volume, not rebuilt) |
| `slskd` / `gonic` | Zero (not rebuilt unless image updated) |

## Rollback

```bash
# 1. Checkout previous version
git checkout <previous-tag-or-commit>

# 2. Rebuild
docker compose up -d --build

# 3. If a migration added columns that the old version doesn't know about,
#    GORM will ignore them safely. If you need to drop new columns:
docker compose exec postgres psql -U musicops musicops -c "ALTER TABLE <table> DROP COLUMN <column>;"
```

## Checking Migration State

```bash
# List all tables
docker compose exec postgres psql -U musicops musicops -c "\dt"

# Check a specific table's columns
docker compose exec postgres psql -U musicops musicops -c "\d jobs"

# Check GORM migration artifacts
docker compose exec postgres psql -U musicops musicops -c "SELECT * FROM schema_migrations;" 2>/dev/null || echo "No schema_migrations table (GORM AutoMigrate only)"
```

## Version-Specific Notes

### v0.0.1

- Initial release. `AutoMigrate` creates all tables from scratch if the database is empty.
- `database.Migrate(db)` contains PostgreSQL enum-to-text conversions for job state columns — do not modify these without understanding the migration path.
