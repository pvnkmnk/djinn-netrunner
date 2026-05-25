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
| `netrunner-gonic-data` | Gonic database and index | Medium (rebuildable via scan) |

```bash
# Stop services before volume backup for consistency
docker compose stop

# Backup volumes (adjust paths as needed)
for vol in netrunner-postgres-data netrunner-music netrunner-slskd-data netrunner-gonic-data; do
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
