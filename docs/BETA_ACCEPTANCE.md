# Beta acceptance record

Every row below was **executed against a live stack** on the date shown — no row
is inferred from reading code. Rows marked FAIL with a strikethrough were
reproduced, root-caused, and fixed on the same commit; the remaining FAIL is an
open blocker.

## Environment

| Field | Value |
|---|---|
| Commit | `d4593c1` (`beta/readiness-scanner-fix`) |
| Stack | `docker-compose.yml` + `docker-compose.beta.yml` |
| Date | 2026-09-12 |
| Host | Windows + Docker Desktop |
| Environment loaded | `ENVIRONMENT=production`, `CONFIG_ENV=production` |
| ffmpeg (app image) | 6.1.2 |
| PostgreSQL | 16.15 |
| slskd | 0.26.0.0 |
| Soulseek account | `beta_smoke_user` (auto-registered, real Soulseek login in the slskd log) |
| Go (host, for the test suite) | 1.27.0 |
| Driver | `./scripts/beta-smoke.sh` plus targeted probes |

## Matrix

| # | Check | Result | Evidence |
|---|---|---|---|
| 1 | Stack health | PASS | `ops-web` and `ops-worker` running and healthy; `/api/health` 200 |
| 2 | ffmpeg present in the app image | PASS | `ffmpeg version 6.1.2` inside `ops-web` |
| 3 | Configuration reaches the containers | PASS | `JWT_SECRET`, `SUBSONIC_ENABLED`, `SUBSONIC_PASSWORD`, `CONFIG_ENV` all present in **both** app containers via `env_file` |
| 4 | Register + login | PASS | `POST /api/auth/register` 201; `POST /api/auth/login` 302 + `Set-Cookie: session_id` |
| 5 | Session survives a restart | PASS | `/api/libraries` still 200 after `docker compose restart ops-web` (previously broken: `JWT_SECRET` never reached the process, so every restart minted a new random secret) |
| 6 | Production refuses to boot without `JWT_SECRET` | PASS | Container exits 1 with `JWT_SECRET is required in production…`, and no misleading "generated random secret" warning precedes it |
| 7 | Production refuses Subsonic without a shared password | PASS | Exits 1 with `SUBSONIC_PASSWORD is required in production when SUBSONIC_ENABLED=true` |
| 8 | Subsonic ping, account password (`p=`) | PASS | `status="ok"` |
| 9 | Subsonic ping, token auth (`t=`/`s=`) | PASS | `status="ok"`; token is `md5(md5(SUBSONIC_PASSWORD)+salt)` — double hash |
| 10 | Create a library owned by the caller | PASS | Library created with `owner_user_id` set; the earlier un-owned library was invisible to Subsonic |
| 11 | Scan indexes **every** file | ~~FAIL~~ PASS | Was 4 FLACs → 1 track row with an empty path, job still "Completed". Fixed; 3/3 fixtures indexed and the worker logs `indexed=3 failed=0` |
| 12 | Indexed tracks carry their path | ~~FAIL~~ PASS | Every row's path matches its file on disk; no `idx_tracks_path` collisions |
| 13 | Subsonic sees the owned library | PASS | `getIndexes` lists the artist; `search3` returns the track id |
| 14 | Streaming returns real audio | PASS | `stream.view` returned 20030 bytes from the container |
| 15 | Fragmented-album repair | PASS | Backup → dry run (filesystem unchanged) → apply → 2 moved, 4 dirs removed, 0 conflicts; re-detection reports none left |
| 16 | MusicBrainz discography sync | PASS | Converge's discography upserted into `tracked_releases` (one transient `503` on the first attempt, succeeded on retry) |
| 17 | Acquisition job runs | ~~FAIL~~ PASS | Was `panic: nil pointer dereference` + `50/50 items pending retry` on every attempt. Fixed; the job now processes items |
| 18 | Soulseek search reaches slskd | ~~FAIL~~ PASS | Was `ssrf: no public IP found for netrunner-slskd` on every search. Fixed; the request now reaches slskd |
| 19 | Soulseek search authenticates | **FAIL** | `slskd search initiation failed: 401 Unauthorized` — see below |
| 20 | Watchlist sync dispatches | ~~FAIL~~ PASS | Was `unsupported job type: watchlist_sync` on every sync. Fixed; the job now reaches the provider and fails only on the provider's own missing credentials |
| 21 | Unit + integration suites | PASS | `go test -count=1 ./...` all packages `ok` (services 76s); `go vet` clean; PR CI `test`, `integration`, `quality`, `review` green |

## Open blocker

**Soulseek searches return `401 Unauthorized`.** The API key is half-wired:
`.env`'s `SLSKD_API_KEY` is what the **app** sends as `X-API-Key`, but nothing
ever passes it to **slskd**, whose container receives no API-key configuration
at all. Until the same value is configured on both sides (slskd
`web.authentication.api_keys`, role `readwrite`), no search can succeed and
acquisition cannot complete end to end. This is external service configuration,
not application code.

Note that the slskd API-key **environment-variable** form is version-sensitive —
nesting the map as `..._API_KEYS_<NAME>_KEY` was rejected at startup, and the
indexed `..._API_KEYS_0_KEY` form was accepted but ignored. A mounted `slskd.yml`
or the slskd UI (Settings → API Keys) is the reliable route.

## Related fixes landed with this record

- Scanner: `Track.Path` is `not null` with a unique index, and the scanner never
  set it — the first file inserted with `path=''` and every later file died on
  `idx_tracks_path` while the job reported `succeeded`. Per-file failures are now
  returned to the job instead of being logged and dropped.
- Acquisition: the pipeline held a typed-nil `*SubsonicClient` in a
  `SubsonicClientInterface`, so its `library == nil` guard passed and `Search3`
  panicked on the nil receiver.
- Watchlist sync: the API created job type `watchlist_sync`; the worker handles
  `sync` with scope `watchlist`.
- Production config fail-fast ran before the YAML overlays, so a
  `config.yaml` with `environment: production` booted with an ephemeral secret.
- `ALLOW_PRIVATE_TARGETS` is now set for both app services; without it the SSRF
  dialer rejects the compose service name that every backend call uses.
