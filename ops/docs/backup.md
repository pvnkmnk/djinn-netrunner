# Backup Runbook

## PostgreSQL Database

### Full dump

```bash
docker compose exec postgres pg_dump -U musicops musicops | gzip > netrunner_$(date +%Y%m%d).sql.gz
```

### Automated daily backup (cron)

```cron
0 3 * * * cd /path/to/djinn-netrunner && docker compose exec -T postgres pg_dump -U musicops musicops | gzip > /backups/netrunner_$(date +\%Y\%m\%d).sql.gz
```

### Retention policy

Keep 7 daily backups and 4 weekly backups. Example cleanup:

```bash
find /backups -name "netrunner_*.sql.gz" -mtime +30 -delete
```

## Volume Backups

Back up these named volumes for a complete restore:

| Volume | Contents | Priority |
|---|---|---|
| `netrunner-postgres-data` | Database files | Critical |
| `netrunner-music` | Imported music library | High |
| `netrunner-downloads` | Staging area for in-progress downloads | Low (transient) |
| `netrunner-slskd-data` | slskd config and state | Medium |

```bash
# Stop services before volume backup for consistency
docker compose stop

# Backup volumes (adjust paths as needed)
for vol in netrunner-postgres-data netrunner-music netrunner-slskd-data; do
  docker run --rm -v ${vol}:/data -v /backups:/backup alpine \
    tar czf /backup/${vol}_$(date +%Y%m%d).tar.gz -C /data .
done

docker compose start
```

## SQLite Database

If using SQLite instead of Postgres:

```bash
# SQLite supports online backup via .backup command
sqlite3 netrunner.db ".backup /backups/netrunner_$(date +%Y%m%d).db"
```

## Restore

### PostgreSQL

```bash
# Stop services
docker compose stop ops-web ops-worker

# Drop and recreate database
docker compose exec postgres dropdb -U musicops musicops
docker compose exec postgres createdb -U musicops musicops

# Restore from dump
gunzip -c netrunner_20260525.sql.gz | docker compose exec -T postgres psql -U musicops musicops

# Restart services
docker compose start
```

### Volume restore

```bash
docker compose stop
docker run --rm -v netrunner-music:/data -v /backups:/backup alpine \
  sh -c "rm -rf /data/* && tar xzf /backup/netrunner-music_20260525.tar.gz -C /data"
docker compose start
```

## PostgreSQL major-version upgrade

PostgreSQL cannot read a previous major version's data directory: bumping
the image tag (e.g. `postgres:16-alpine` → `postgres:17-alpine`) with a
populated `netrunner-postgres-data` volume leaves the database down until
the data is migrated. Dump from the old version, restore into a fresh
volume — never reuse the old volume across a major version.

```bash
# 1. Stop writers, then dump from the still-running old-version container
docker compose stop ops-web ops-worker
docker compose exec postgres pg_dump -U musicops musicops | gzip > netrunner_pg16_$(date +%Y%m%d).sql.gz

# 2. Back up the old volume and keep it until the new database is validated
docker run --rm -v netrunner-postgres-data:/data -v /backups:/backup alpine \
  tar czf /backup/netrunner-postgres-data-pg16_$(date +%Y%m%d).tar.gz -C /data .

# 3. Stop the stack, remove only the postgres volume, and start a fresh one
#    on the new image (do NOT use `docker compose down -v`: it removes every
#    named volume, including the music library and downloads)
docker compose down
docker volume rm netrunner-postgres-data
docker compose up -d postgres

# 4. Restore into the fresh database (same steps as "Restore" above)
gunzip -c netrunner_pg16_$(date +%Y%m%d).sql.gz | docker compose exec -T postgres psql -U musicops musicops

# 5. Validate before restarting services: spot-check row counts, then start
docker compose exec postgres psql -U musicops musicops -c "SELECT count(*) FROM tracks;"
docker compose start
```

Only delete the old volume's backup tarball after the restored database is
validated. The same dump/restore path was rehearsed for the 16 → 17 upgrade
(DJI-667): row counts identical across all tables, schema and data dumps
identical, and the Postgres gate passed 24/24 on the restored database.

For the integration stack (`docker-compose.integration.yml`), the same rule
applies to `integration-db-data`: if its test data can be discarded, reset
it with `docker compose -f docker-compose.integration.yml down -v` and let
the PG17 container create a fresh volume; otherwise migrate it with the
steps above.
