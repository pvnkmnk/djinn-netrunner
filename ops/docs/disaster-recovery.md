# Disaster Recovery Runbook

## Interrupted Acquisition (Stuck Jobs)

Symptoms: Jobs stuck in `running` state, no progress in logs.

```bash
# List stuck jobs
docker compose exec postgres psql -U musicops musicops -c \
  "SELECT id, type, state, requested_at FROM jobs WHERE state = 'running' AND updated_at < NOW() - INTERVAL '30 minutes';"

# Option 1: Wait for automatic zombie cleanup
# The worker runs a zombie cleanup loop that marks stale jobs as failed after the heartbeat timeout.

# Option 2: Manual reset via CLI
cd backend
go run ./cmd/cli jobs  # list jobs to find the stuck ID

# Option 3: Direct DB reset
docker compose exec postgres psql -U musicops musicops -c \
  "UPDATE jobs SET state = 'failed' WHERE state = 'running' AND updated_at < NOW() - INTERVAL '30 minutes';"
docker compose exec postgres psql -U musicops musicops -c \
  "UPDATE job_items SET state = 'failed' WHERE state = 'running' AND updated_at < NOW() - INTERVAL '30 minutes';"
```

## Worker Crash Recovery

Symptoms: `ops-worker` container exited, jobs may be in inconsistent state.

```bash
# 1. Check worker logs
docker compose logs --tail=100 ops-worker

# 2. Restart the worker
docker compose restart ops-worker

# 3. The worker will:
#    - Automatically detect and clean up zombie jobs (heartbeat-based)
#    - Resume polling for new queued jobs
#    - The zombie cleanup interval is configured in the worker loop
```

### Zombie cleanup timeline

- **Heartbeat interval**: Worker updates job heartbeat timestamps periodically
- **Zombie threshold**: Jobs with stale heartbeats are automatically marked as failed
- **Manual override**: Use the `cancel_job` MCP tool or direct DB update if urgent

## Database Corruption Recovery

### PostgreSQL

```bash
# 1. Stop services
docker compose stop ops-web ops-worker

# 2. Attempt repair
docker compose exec postgres pg_isready -U musicops
# If pg_isready fails, the Postgres container may need a restart:
docker compose restart postgres

# 3. If data is irrecoverable, restore from backup
# See backup.md for restore procedures

# 4. After restore, trigger a library re-scan to reconcile state
docker compose exec ops-web ./netrunner-cli library list
docker compose exec ops-web ./netrunner-cli library scan <library-id>
```

### SQLite

```bash
# 1. Check database integrity
sqlite3 netrunner.db "PRAGMA integrity_check;"

# 2. If corrupt, attempt recovery
sqlite3 netrunner.db ".dump" | sqlite3 netrunner_recovered.db
mv netrunner.db netrunner.db.corrupt
mv netrunner_recovered.db netrunner.db

# 3. Restart services
```

## slskd Credential Invalidation

Symptoms: All acquisition jobs fail with connection errors, slskd health check fails.

```bash
# 1. Verify slskd connectivity
docker compose exec ops-web wget -q -O - http://netrunner-slskd:5030/api/v0/server 2>&1 || echo "slskd unreachable"

# 2. Check slskd logs for auth errors
docker compose logs --tail=50 netrunner-slskd

# 3. If credentials are invalid:
#    - Update SLSKD_USERNAME and SLSKD_PASSWORD in .env
#    - Restart slskd: docker compose restart slskd
#    - Verify: docker compose exec ops-web wget -q -O - http://netrunner-slskd:5030/api/v0/server

# 4. Re-queue failed acquisition jobs
docker compose exec postgres psql -U musicops musicops -c \
  "UPDATE jobs SET state = 'queued' WHERE state = 'failed' AND type = 'acquisition' AND requested_at > NOW() - INTERVAL '24 hours';"
docker compose exec postgres psql -U musicops musicops -c \
  "UPDATE job_items SET state = 'queued' WHERE state = 'failed' AND job_id IN (SELECT id FROM jobs WHERE state = 'queued' AND type = 'acquisition');"
```

## Emergency Database Reset

**Warning**: This destroys all data. Use only as a last resort.

```bash
docker compose down -v  # removes all volumes
docker compose up -d    # fresh start with empty database
```
