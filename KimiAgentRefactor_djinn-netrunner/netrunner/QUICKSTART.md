# NetRunner - Quick Start

## Prerequisites
- Docker and Docker Compose
- Go 1.25+ (for local binary/CLI workflows)
- Soulseek account (for slskd-backed acquisition)

## Setup

1. Clone and configure:
```bash
git clone https://github.com/pvnkmnk/djinn-netrunner.git
cd djinn-netrunner
cp .env.example .env
```

2. Edit `.env` with minimum required values:
```env
POSTGRES_PASSWORD=your_secure_password
SLSKD_USERNAME=your_slsk_user
SLSKD_PASSWORD=your_slsk_pass
SLSKD_API_KEY=your_random_api_key
DATABASE_URL=postgresql://musicops:your_secure_password@postgres:5432/musicops?sslmode=disable
JWT_SECRET=replace_with_a_long_random_secret
DOMAIN=localhost
```

3. Launch stack:
```bash
docker compose up -d --build
```

4. Verify health:
```bash
curl http://localhost:8080/api/health
```

5. Register and log in using session auth:
```bash
curl -i -X POST http://localhost:8080/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"replace-me"}'
```
Then open `http://localhost` and log in.

## Add Your First Watchlist

### Via CLI
```bash
# Run CLI from local source
cd backend
go run ./cmd/cli watchlist list
go run ./cmd/cli watchlist add "My Favorites" "spotify_playlist" "spotify:playlist:..."
go run ./cmd/cli watchlist sync <watchlist-uuid>
```

### Via UI
1. Navigate to **Watchlists**.
2. Click **Add Watchlist**.
3. Fill name, source type, and source URI.
4. Trigger sync.

## Run Validation
```bash
# PowerShell
pwsh -File scripts/validate.ps1

# Bash
bash scripts/validate.sh
```

## Check System Status

```bash
docker compose ps

docker compose logs -f netrunner

docker compose logs -f netrunner-slskd

cd backend
go run ./cmd/cli status
go run ./cmd/cli stats summary
```

## Troubleshooting

**Jobs stuck in running:**
```sql
docker compose exec postgres psql -U musicops -d musicops -c \
  "SELECT id, heartbeat_at FROM jobs WHERE state='running' AND heartbeat_at < NOW() - INTERVAL '10 minutes'"
```

**Check slskd connectivity:**
```bash
cd backend
go run ./cmd/cli status
```

See `docs/RUNBOOK.md` for operational procedures.
See `docs/ARCHITECTURE.md` for system design.
