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

`.env` is injected into the `ops-web` and `ops-worker` containers as well as
driving `${VAR}` substitution in the compose files, so the values below actually
reach the app. For a full single-machine beta (streaming, verification,
troubleshooting) see [docs/BETA_DEPLOYMENT.md](docs/BETA_DEPLOYMENT.md).

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

5. Register and log in using session auth. State-changing requests require the
   CSRF token from the `csrf_` cookie (a missing header returns `403`):
```bash
JAR=/tmp/nr.jar; rm -f $JAR
curl -s -c $JAR -o /dev/null http://localhost:8080/
TOKEN=$(awk '/csrf_/{print $7}' $JAR | tail -1)

curl -i -X POST http://localhost:8080/api/auth/register \
  -H "Content-Type: application/json" -H "X-CSRF-Token: $TOKEN" \
  -b $JAR -c $JAR \
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

## Stream to a music client (optional)

NetRunner serves its own Subsonic-compatible API — set `SUBSONIC_ENABLED=true`
and `SUBSONIC_PASSWORD` in `.env`, then point any Subsonic client at
`http://<host>:8080/rest` with your account email as the username. Production
refuses to start with `SUBSONIC_ENABLED=true` and no password, because the token
path would be forgeable.

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
