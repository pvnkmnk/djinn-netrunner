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

## Beta release checklist (operator)
1. Automated gate:
   - `pwsh -File scripts/validate.ps1 -SkipVulnCheck`
2. Manual Docker acceptance:
   - Login/logout and registration flow.
   - Watchlist create/edit/sync/preview.
   - Library create/scan and job completion.
   - Artist + schedule CRUD.
   - Live console attach/filter/copy/clear behavior.
3. Security/tenancy checks:
   - Non-admin cannot access other users' watchlists/libraries/jobs/partials.
   - Admin can view global data and event stream as expected.
4. Notification/quotas:
   - Webhook completion payload observed.
   - Quota warning path exercised and logged.

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

NetRunner uses a **two-pronged** Spotify strategy. No Spotify Developer App is required for the primary path.

### Prong 1 — sp_dc cookie (recommended)
The sp_dc cookie is a long-lived browser cookie from Spotify's web player. It enables access to:
- Public/private playlists via the GraphQL Partner API
- Liked Songs (`spotify_liked` source type)
- Discover Weekly and Daily Mixes (`spotify_discover` source type)

**Setup:**
1. Log into [open.spotify.com](https://open.spotify.com) in a browser.
2. Open DevTools → Application → Cookies → `https://open.spotify.com`.
3. Copy the value of the `sp_dc` cookie.
4. Submit it via the UI (Settings → Spotify Connection) or the API:
   ```bash
   curl -X POST http://localhost:8080/api/auth/spotify/spdc \
     -H "Content-Type: application/json" \
     -H "Cookie: session_id=<your_session>" \
     -d '{"sp_dc": "<your_sp_dc_cookie>"}'
   ```

The sp_dc cookie typically lasts ~1 year. When it expires, repeat the steps above.

### Prong 2 — OAuth Client Credentials (fallback)
If `SPOTIFY_CLIENT_ID` and `SPOTIFY_CLIENT_SECRET` are set, OAuth is used as a fallback for public playlist data via the Spotify Web API. Tokens are cached in the database and refreshed automatically before expiry.

### GraphQL hash management
The sp_dc path uses SHA256 hashes of Spotify's web player GraphQL operations. These hashes can break when Spotify deploys new JS bundles. See `docs/watchlist-providers.md` for the hash extraction procedure.

## Grafana dashboard

A pre-built Grafana dashboard is available at `ops/grafana/netrunner-dashboard.json`.

### Import
1. Open Grafana → Dashboards → Import.
2. Upload `ops/grafana/netrunner-dashboard.json` or paste its contents.
3. Select the Prometheus data source that scrapes the NetRunner endpoints.

### Prometheus scrape config
The server exposes metrics on `:8080/metrics` and the worker on `:9090/metrics`. Add both targets to your Prometheus config:
```yaml
scrape_configs:
  - job_name: netrunner-server
    static_configs:
      - targets: ['netrunner:8080']
  - job_name: netrunner-worker
    static_configs:
      - targets: ['netrunner-worker:9090']
```

### Key panels
- **Jobs**: queued/running/succeeded/failed gauges, duration histograms
- **Acquisition**: dedup hits (hash vs recording ID), pipeline throughput
- **Worker**: heartbeat freshness, zombie recovery events
