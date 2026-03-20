# Djinn NETRUNNER — Operations Runbook

This runbook covers day-to-day operation and common troubleshooting for NETRUNNER deployments.

## Start/stop
- Start stack:
  - docker compose up -d
- Stop stack:
  - docker compose down
- Restart only worker:
  - docker compose restart ops-worker
- Tail logs:
  - docker compose logs -f ops-worker
  - docker compose logs -f ops-web
  - docker compose logs -f slskd

## Database access
Connect:
- docker compose exec postgres psql -U musicops -d musicops

## Common health checks (SQL)
Active jobs:
select id, jobtype, state, startedat, heartbeatat
from jobs
where state = 'running'
order by startedat desc;

Stale jobs (heartbeat older than 10 minutes):
select id, heartbeatat, now() - heartbeatat as staleduration
from jobs
where state = 'running'
and heartbeatat < now() - interval '10 minutes'
order by heartbeatat asc;

Recent failures:
select id, jobtype, finishedat, summary
from jobs
where state = 'failed'
order by finishedat desc
limit 50;

## Jobs stuck in running
Expected behavior:
- Reaper should requeue stale jobs automatically once heartbeat is beyond threshold.

If jobs remain stuck:
1. Check worker logs for exceptions.
2. Check advisory locks:
select locktype, classid as namespace, objid, pid, mode, granted
from pg_locks
where locktype = 'advisory'
order by classid, objid;

Emergency manual requeue:
update jobs
set state = 'queued',
workerid = null,
heartbeatat = null
where id = :job_id;

## Console not updating
Checklist:
- Check ops-web logs for websocket errors.
- Confirm Caddy routing supports WebSockets.
- Confirm the NOTIFY fanout loop (if used) is running.

## Job completion not triggering webhook
1. Verify `NOTIFICATION_ENABLED=true` in environment
2. Verify `NOTIFICATION_WEBHOOK_URL` is set to a reachable endpoint
3. Check ops-worker logs for `[NOTIFY]` errors

## slskd not downloading
Checklist:
- Verify slskd is healthy and reachable.
- Confirm download slots in slskd config match worker cap.
- Inspect slskd logs for authentication errors and queue saturation.

## Routine maintenance
Weekly:
- Check disk space for downloads + library paths.
- Review failed jobs and error patterns.

Optional cleanup:
delete from joblogs
where ts < now() - interval '30 days';

## Notification webhook testing
```bash
curl -X POST $NOTIFICATION_WEBHOOK_URL \
  -H "Content-Type: application/json" \
  -d '{"job_id":1,"type":"sync","state":"completed","summary":"ok","completed_at":"2026-03-20T00:00:00Z"}'
```

## Spotify token refresh
Tokens are refreshed automatically every 5 minutes.
Manual refresh not exposed via CLI (background service handles it).
