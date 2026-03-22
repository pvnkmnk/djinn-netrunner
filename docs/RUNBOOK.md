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

## Webhook Notifications

When `NOTIFICATION_ENABLED=true` and `NOTIFICATION_WEBHOOK_URL` is set, NetRunner sends a POST request to the configured URL when each job completes.

### Payload Schema

```json
{
  "job_id": 42,
  "type": "sync",
  "state": "succeeded",
  "summary": "Completed",
  "completed_at": "2026-03-20T15:30:00Z",
  "worker_id": "worker-1"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `job_id` | uint64 | Unique job identifier |
| `type` | string | Job type: `sync`, `scan`, `acquisition` |
| `state` | string | Final state: `succeeded` or `failed` |
| `summary` | string | Human-readable summary or error message |
| `completed_at` | timestamp | ISO 8601 UTC timestamp |
| `worker_id` | string | ID of the worker that processed the job |

## Notification webhook testing
```bash
curl -X POST $NOTIFICATION_WEBHOOK_URL \
  -H "Content-Type: application/json" \
  -d '{"job_id":1,"type":"sync","state":"succeeded","summary":"ok","completed_at":"2026-03-20T00:00:00Z","worker_id":"worker-1"}'
```

## Spotify access
Spotify integration uses Client Credentials OAuth with a background token refresh mechanism.
Tokens are cached in the database and refreshed automatically before expiry. No user
interaction is required. The `SpotifyAuthHandler` manages token lifecycle — operators only
need to ensure `SPOTIFY_CLIENT_ID` and `SPOTIFY_CLIENT_SECRET` are set in the environment.
