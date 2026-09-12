# Beta Deployment (single machine)

Bring up NetRunner as a self-contained beta on one Docker host: PostgreSQL,
slskd (Soulseek), the API/UI, the background worker, and optionally an external
Subsonic server. NetRunner serves its own Subsonic-compatible API, so a music
client can stream from it without any extra service.

Verified against `master` for the Beta Readiness work (see Linear project
*NetRunner Beta Readiness*).

## Prerequisites

- Docker Engine / Docker Desktop with Compose v2.24+ (`env_file.required` support)
- A Soulseek account for slskd (acquisition is inert without one)
- ~5 GB free for images plus room for the music library and downloads

## 1. Configure

```bash
git clone https://github.com/pvnkmnk/djinn-netrunner.git
cd djinn-netrunner
cp .env.beta.example .env
```

Change every `change_me_` value. The four that matter most:

| Variable | Why |
|---|---|
| `JWT_SECRET` | Session signing key. Without it in production the server **refuses to start** — an ephemeral secret would silently invalidate every login on restart. Generate one: `py -c "import secrets;print(secrets.token_urlsafe(48))"` |
| `SUBSONIC_PASSWORD` | Shared password for Subsonic token auth. Production refuses to start with Subsonic enabled and no password, because the token path would be `md5("" + salt)` — forgeable. |
| `SLSKD_USERNAME` / `SLSKD_PASSWORD` | Real Soulseek credentials; searches and downloads fail without them. |
| `SLSKD_API_KEY` | The key the app sends as `X-API-Key`, and the same value compose hands to slskd as its primary key. **At least 16 characters** — slskd logs `API key must be between 16 and 255 characters` and exits (code 0) below that. The stack wires both sides automatically; a hand-written compose file must set it on the slskd service too. |

The file is read twice: for `${VAR}` substitution inside the compose files, and
as the runtime environment of the `ops-web` / `ops-worker` containers (`env_file`
in `docker-compose.yml`). Values the compose files set explicitly — `DATABASE_URL`,
`SLSKD_URL`, `MUSIC_LIBRARY`, `DOWNLOAD_STAGING`, paths — win over `.env`.

## 2. Bring it up

```bash
docker compose -f docker-compose.yml -f docker-compose.beta.yml up -d --build
```

The overlay publishes the app on `${BETA_HTTP_PORT:-8080}`, applies production
semantics, enables Subsonic, and adds restart/log policies. Caddy (the TLS edge
in the base file) is moved behind the `edge` profile so a local beta talks plain
HTTP instead of presenting a local-CA certificate; add `--profile edge` when you
want TLS on `:443`.

Optional external Subsonic server over the same music volume:

```bash
docker compose -f docker-compose.yml -f docker-compose.beta.yml --profile media-server up -d
# then set NAVIDROME_URL=http://navidrome:4533 in .env and re-run without --build
```

## 3. Verify

The fastest health check is the smoke script. It asserts the container
configuration, Subsonic auth (both schemes), session persistence across a
restart, library scanning, Subsonic visibility and real audio bytes, and exits
non-zero listing every failing check:

```bash
./scripts/beta-smoke.sh
# ...
# Beta smoke: all checks passed.
```

Add `--keep` to leave the throwaway library and audio fixtures in place for
inspection. Point it at another host with `BETA_BASE_URL=https://music.example`.

For a manual pass, start with the health endpoint:

```bash
curl -s http://localhost:8080/api/health
# {"status":"ok","checks":{"database":{"status":"ok"},"disk":{"status":"ok",...},"slskd":{"status":"ok"}}}
```

Confirm the environment actually reached the container — this was the class of
bug that made earlier betas behave inexplicably:

```bash
docker compose -f docker-compose.yml -f docker-compose.beta.yml exec ops-web env | grep -E '^(JWT_SECRET|SUBSONIC_ENABLED|CONFIG_ENV)='
```

Then create the first account. **Every state-changing request needs the CSRF
header**: any request (including `GET /`) sets a `csrf_` cookie, whose value must be echoed in
`X-CSRF-Token`.

```bash
JAR=/tmp/nr.jar; rm -f $JAR
curl -s -c $JAR -o /dev/null http://localhost:8080/
TOKEN=$(awk '/csrf_/{print $7}' $JAR | tail -1)

curl -s -b $JAR -c $JAR -X POST http://localhost:8080/api/auth/register \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $TOKEN" \
  -d '{"email":"you@example.com","password":"change-this"}'

curl -s -b $JAR -c $JAR -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $TOKEN" \
  -d '{"email":"you@example.com","password":"change-this"}'

curl -s -b $JAR -o /dev/null -w '%{http_code}\n' http://localhost:8080/api/watchlists   # 200
```

Sessions must survive a restart — this is what `JWT_SECRET` buys:

```bash
docker compose -f docker-compose.yml -f docker-compose.beta.yml restart ops-web
curl -s -b $JAR -o /dev/null -w '%{http_code}\n' http://localhost:8080/api/watchlists   # still 200
```

Streaming, both authentication styles:

```bash
# account password (u = your NetRunner email)
curl -s 'http://localhost:8080/rest/ping.view?u=you@example.com&p=change-this&v=1.16.1&c=smoke'
# <subsonicResponse status="ok" version="1.16.1" type="netrunner"></subsonicResponse>

# shared-password token auth: t = md5(md5(SUBSONIC_PASSWORD) + salt)
```

## 4. Streaming clients

| Field | Value |
|---|---|
| Server URL | `http://<host>:8080/rest` |
| Username | your NetRunner **email address** |
| Password | your account password (works with `p=` auth) |

If the client does not support token auth, the password field works as-is. If it
does, the shared `SUBSONIC_PASSWORD` is the password used for `t=`/`s=` token
auth; without a configured `SUBSONIC_PASSWORD` token requests are refused and
only the account password is accepted.

## 5. Operate

| Concern | Command |
|---|---|
| Logs | `docker compose -f docker-compose.yml -f docker-compose.beta.yml logs -f ops-web ops-worker` |
| State | `docker volume ls \| grep netrunner` (postgres, downloads, music, config, logs, slskd) |
| Upgrade | `git pull && docker compose -f docker-compose.yml -f docker-compose.beta.yml up -d --build` |
| Back up | see `ops/docs/backup.md` — back up the Postgres volume *and* the music volume together |
| Deduplicate a pre-existing library | `ops/docs/library-dedup-runbook.md` |
| Reach it from another host | The beta port binds to `127.0.0.1` because it serves plain HTTP with session cookies and Subsonic credentials. Put the `edge` profile's Caddy in front, or set `BETA_BIND_ADDR=0.0.0.0` behind your own TLS terminator. `NAVIDROME_BIND_ADDR` works the same way for the optional media server. |
| Tear down (keep data) | `docker compose ... down` |
| Destroy (lose everything) | `docker compose ... down -v` |

## 6. Troubleshooting

| Symptom | Cause and fix |
|---|---|
| Server exits, log says `JWT_SECRET is required in production` | `JWT_SECRET` empty or not injected — set it in `.env`. |
| Server exits, `SUBSONIC_PASSWORD is required in production when SUBSONIC_ENABLED=true` | Set `SUBSONIC_PASSWORD`, or set `SUBSONIC_ENABLED=false` if you do not want streaming. |
| `403` on register/login | Missing `X-CSRF-Token` header (see step 3). |
| Logins die after every restart | `JWT_SECRET` is not reaching the container: check `docker compose ... exec ops-web env \| grep JWT_SECRET`. |
| Every variable in `.env` seems ignored | The container predates the current compose file. `env_file` is applied at *create* time, so `docker compose restart` does not pick up `.env` changes — run `up -d` (add `--build` after a code change). A local override that replaces `env_file` has the same effect. `docker compose exec ops-web env` shows what actually arrived. |
| `exec /entrypoint.sh: no such file or directory` when building on Windows | The working copy has CRLF in `backend/entrypoint.sh`. The repo forces LF via `.gitattributes`; `git checkout -- backend/entrypoint.sh` (or `git config core.autocrlf input`) fixes it. |
| slskd exits immediately / downloads unwritable | `volume-init` must run before slskd; it chowns the shared volumes to UID 1000. Keep it in the stack. |
| `port is already allocated` | Another stack holds 8080/5432; set `BETA_HTTP_PORT` (or stop the other stack). |
| Music plays but nothing rescans in an external server | Set `NAVIDROME_URL` (+ user/pass) so the worker has a library client; `/api/health` then reports a `navidrome` check. |
| Every acquisition fails with `ssrf: no public IP found for netrunner-slskd` | `ALLOW_PRIVATE_TARGETS` is missing or `false`. slskd is reached by its compose service name, which resolves to a private IP and trips the SSRF guard. `docker-compose.yml` sets it for both app services; keep it if you write your own compose file. |
| Every acquisition fails with `401 Unauthorized`, and slskd logs `Unknown API key beginning with: …` | The key is half-wired: `SLSKD_API_KEY` reached the app but not slskd, so the two sides disagree. It must be set on the slskd service as well. Set it once in `.env` and let `docker-compose.yml` pass it to both. (`SLSKD_API_URL`-era guides suggest `web.authentication.api_keys`; that map is awkward to express as an env var, whereas slskd's primary key is a plain `SLSKD_API_KEY`.) |
| Imports fail with `Downloaded file not found: downloads/…` although slskd reports `Completed, Succeeded` | slskd is downloading into a directory the worker cannot see — by default `~/downloads`, i.e. its **own** `/app/downloads`, not the shared volume the worker imports from. `docker-compose.yml` sets `SLSKD_DOWNLOADS_DIR=/downloads` for this reason. Confirm with `docker compose … exec slskd ls /downloads`. |
| slskd exits at startup, log shows `API key must be between 16 and 255 characters` | `SLSKD_API_KEY` is shorter than 16 characters. The exit code is **0**, so it looks like a clean stop — check `docker inspect <slskd> --format '{{.State.ExitCode}}'`. |
| Acquisition job fails with `panic: ... nil pointer dereference` and `N/N items pending retry` | Fixed: the pipeline held a typed-nil library client. If you see it, you are on a build older than the library-client fix. |
| Watchlist "Sync" fails with `unsupported job type: watchlist_sync` | Fixed: the API created `watchlist_sync` while the worker handles `sync`. A watchlist sync now also needs its provider's credentials (e.g. `LASTFM_API_KEY`); a provider error like `last.fm api returned status: 400` means the dispatch worked and the key is missing. |
