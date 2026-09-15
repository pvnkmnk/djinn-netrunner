# Beta acceptance record

Every row below was **executed against a live stack** on the date shown — no row
is inferred from reading code. Rows marked FAIL with a strikethrough were
reproduced, root-caused, and fixed on the same commit; the remaining FAIL is an
open blocker.

## Environment

| Field | Value |
|---|---|
| Commit | base `992eb84`; fixes on `beta/slskd-acquisition-wiring` |
| Stack | `docker-compose.yml` + `docker-compose.beta.yml` |
| Date | 2026-09-12 |
| Host | Windows + Docker Desktop |
| Environment loaded | `ENVIRONMENT=production`, `CONFIG_ENV=production` |
| ffmpeg (app image) | 6.1.2 |
| PostgreSQL | 16.15 |
| slskd | 0.26.0.0 |
| slskd downloads dir | `/downloads` (the shared volume, via `SLSKD_DOWNLOADS_DIR`) |
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
| 19 | Soulseek search authenticates | ~~FAIL~~ PASS | Was `slskd search initiation failed: 401 Unauthorized` on every item. Fixed; a live run logged `Found 2281 results` and selected a peer |
| 20 | Watchlist sync dispatches | ~~FAIL~~ PASS | Was `unsupported job type: watchlist_sync` on every sync. Fixed; the job now reaches the provider and fails only on the provider's own missing credentials |
| 21 | Unit + integration suites | PASS | `go test -count=1 ./...` all packages `ok` (services 76s); `go vet` clean; PR CI `test`, `integration`, `quality`, `review` green |
| 22 | slskd holds the app's API key | ~~FAIL~~ PASS | Was absent: `SLSKD_API_KEY` reached only the app containers. `netrunner-slskd` now carries the same value, answers 200 on `/api/v0/application`, and logs zero `Unknown API key` entries |
| 23 | Downloads land where the worker imports from | ~~FAIL~~ PASS | slskd defaulted its downloads to `~/downloads` — its **own** `/app/downloads` — while the worker imported from the shared volume, so every import failed with `Downloaded file not found`. Fixed via `SLSKD_DOWNLOADS_DIR=/downloads` |
| 24 | Real acquisition, end to end | PASS | 12 items imported from real Soulseek peers; 1 `failed (no results)`; 1 remote-queue timeout (see findings) |
| 25 | Canonical album-artist folders | PASS | Every import landed under `/app/music/Converge/<Album>/` — no per-credit fragmentation |
| 26 | Tag writes on acquired files | PASS | `album_artist=Converge` on the imported m4a (ffmpeg tagger) and mp3 |
| 27 | Staging sweep after import | PASS | Only the in-flight download remained in `/app/downloads` |
| 28 | Acquired tracks become visible and streamable | PASS | A scan of `/app/music` indexed 16 tracks; `getIndexes` lists Converge (12 albums); `search3`/`stream.view` returned the real bytes (7,062,156 mp3; 94,778,503 m4a, equal to the file on disk) |

### Full-artist pass — 2026-09-15

Re-executed against the same stack, driven by a full **PUP** discography
acquisition (38 items) rather than a single track.

| # | Check | Result | Evidence |
|---|---|---|---|
| 29 | Full artist sync produces an acquisition job | PASS | `artist_scan` (75) succeeded; `acquisition` (76) queued 38 items |
| 30 | Implausible results rejected before download | PASS | `Skipped 713 of 2846 results that do not look like playable audio (e.g. unsupported audio format "png")`; also `289/1621` and `280/1751` `.lrc`, `115/673` `.txt` |
| 31 | Remotely-queued peer abandoned early | PASS | `heyheyfrapfrap queued the transfer but never started sending — trying another candidate`, twice, ~45 s after queueing instead of the full `10m0s` budget |
| 32 | Download bytes validated before import | PASS | `beta-smoke.sh` asserts ffprobe is present and decodes a generated tone; unit tests reject a text file renamed `.mp3` and remove it |
| 33 | Imports land in canonical album folders | PASS | Files at `/app/music/PUP/<Album>/<nn - title>.m4a`; no per-credit artist folders |
| 34 | Tags carry a canonical album artist | PASS | `ffprobe` on an imported m4a reports `artist=PUP`, `album_artist=PUP`, `album=Morbid Stuff` |
| 35 | Scan indexes the acquired files | PASS | Scan job 77 `succeeded`; worker logs `indexed=30 failed=0` |
| 36 | `getIndexes` exposes resolvable artist ids | PASS | `<artist id="artist-PUP" name="PUP" albumCount="4">` — real id, and `albumCount` is distinct albums |
| 37 | `search3` returns no nameless placeholder artist | PASS | `<artist id="artist-PUP" name="PUP" albumCount="4">`; previously `{"id":"artist-","name":"","albumCount":0}` |
| 38 | `getIndexes` → `getArtist` round-trip | PASS | `getArtist.view?id=artist-PUP` returns a top-level `<artist>` with 4 albums, each carrying `artistId="artist-PUP"` |
| 39 | Stream returns real audio | PASS | `stream.view` HTTP 200, `29,087,926` bytes, `audio/m4a`, magic bytes `ftypM4A` |
| 40 | `scripts/beta-smoke.sh` (with the new ffprobe assertion) | PASS | All checks passed, including `ffprobe present in ops-worker and decoded a generated tone` |

## Prior blocker, resolved

**Soulseek searches returned `401 Unauthorized`.** `SLSKD_API_KEY` was what the
**app** sent as `X-API-Key`, but nothing passed it to **slskd**, which received
no API-key configuration at all. slskd accepts a *primary* key via
`SLSKD_API_KEY` (or `-k`/`--api-key`) — the documented, stable form. Treating the
`web.authentication.api_keys` map as an environment variable is the fragile
route; the primary key is not. An invalid key length is a silent-looking failure:
under 16 characters slskd logs `API key must be between 16 and 255 characters`
and exits **0**.

## Findings resolved on 2026-09-15

All three were reproduced on the live stack, fixed, and re-verified (rows 30–39).

1. **A remotely-queued peer cost the full 10-minute budget.** slskd reports
   `Queued, Remotely` for a peer that answers but never starts sending, and the
   stall detector only armed once bytes moved — so the wait ran to the full
   `WaitForDownload` timeout. `WaitForDownload` now takes `DownloadWaitOptions`
   and abandons such a transfer after `remoteQueueGrace` (45 s) — but only when
   another candidate remains. The last candidate waits the transfer out, since
   abandoning it saves nothing and can fail an item that waiting would complete.
   `stageDownloadFile` walks up to 3 candidates per item.
2. **Subsonic artist entries carried empty ids.** The `DISTINCT artist` scan
   landed in a struct field named `Name`, which GORM maps to the column `name`,
   so every artist came back with an empty name and the id degenerated to a bare
   `artist-`. Now plucked as a string list, ids come from one `artistID` helper,
   `getArtist` returns the artist at the top level with its albums, and
   `albumCount` counts distinct albums rather than tracks.
3. **No validity gate on selected downloads.** Two gates now: a pre-download
   plausibility check (size against reported bitrate/length, plus an audio
   extension allowlist) that drops junk before spending a download, and an
   `ffprobe` check of the downloaded bytes that removes anything not playable and
   moves to the next candidate. A missing ffprobe is reported as
   `ErrProbeUnavailable` and the file is imported with a warning — an
   unconfigured probe must not reject every download.

## Open findings (not blocking)

1. **A duplicate library path returns 500.** `POST /api/libraries` with an
   existing path surfaces the `idx_libraries_path` violation as
   `internal server error` rather than a 409 with a readable message.
2. **The worker runs several acquisition jobs concurrently.** Three jobs
   (`10`, `68`, `76`) were observed in `running` state with items downloading at
   the same time under one `worker_id`, so a stalled peer blocks its own item but
   not the whole queue. The earlier note that the worker runs one job at a time
   no longer describes this build; the concurrency limit should be made explicit.
3. **There is no job-cancel endpoint.** A long acquisition cannot be stopped
   through the API; the local stack was cleared by editing item rows directly.

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
