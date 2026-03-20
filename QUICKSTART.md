# NetRunner — Quick Start

## Prerequisites
- Docker & Docker Compose
- Soulseek account (configured via slskd)

## Setup

1. Clone and configure:
```bash
git clone https://github.com/pvnkmnk/netrunner.git
cd netrunner
cp .env.example .env
```

2. Edit `.env` with your credentials:
```env
POSTGRES_PASSWORD=your_secure_password
SLSKD_USERNAME=your_slsk_user
SLSKD_PASSWORD=your_slsk_pass
SLSKD_API_KEY=your_random_api_key
DOMAIN=localhost
```

3. Launch:
```bash
docker compose up -d
```

4. Register an admin account:
```bash
docker compose exec netrunner-api sh
# Within container:
netrunner-cli auth register admin@example.com yourpassword admin
exit
```

5. Access the UI at `http://localhost` and log in.

## Adding Your First Watchlist

### Via CLI
```bash
docker compose exec netrunner-api sh
netrunner-cli watchlist add "My Favorites" "spotify_playlist" "spotify:playlist:..."
netrunner-cli watchlist sync <uuid>
exit
```

### Via UI
1. Navigate to the **Watchlists** page
2. Click **Add Watchlist**
3. Fill in the form (name, type, URI)
4. Click **Sync** to trigger acquisition

## Checking System Status

```bash
docker compose ps              # Service health
docker compose logs -f       # All logs
docker compose logs ops-worker -f  # Worker logs only
netrunner-cli status          # Job counts, DB health
```

## Key Files

| Path | Purpose |
|------|---------|
| `docker-compose.yml` | Service definitions |
| `.env.example` | All environment variables |
| `backend/` | Go source code |
| `ops/web/` | HTMX templates + CSS |
| `ops/caddy/` | Reverse proxy config |

## Troubleshooting

**Jobs stuck in running:**
```sql
docker compose exec postgres psql -U musicops -d musicops -c \
  "SELECT id, heartbeat_at FROM jobs WHERE state='running' AND heartbeat_at < NOW() - INTERVAL '10 minutes'"
```

**Check slskd connectivity:**
```bash
netrunner-cli status  # Shows slskd connection status
```

See `docs/RUNBOOK.md` for operational procedures.
See `docs/ARCHITECTURE.md` for system design.
