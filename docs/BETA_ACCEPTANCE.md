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

0. **OPEN (DJI-489) — case-only differences split an album or artist across folders.** A full PUP
   acquisition produced both `/app/music/PUP/Who Will Look After The Dogs/` and
   `/app/music/PUP/Who Will Look After the Dogs/`, plus `/app/music/Pup/` beside
   `/app/music/PUP/` for the same artist. Peers tag the same album with different
   capitalisation, and the canonical folder is built from the tag verbatim, so
   the album fragments exactly as it did with per-track credits. Folder and
   comparison logic needs case-insensitive folding (or a canonical form) rather
   than the raw tag.
0b. **OPEN (DJI-490) — staging keeps non-empty leftovers.** 54 directories remained under
   `/app/downloads` after the run, 13 of them non-empty. `cleanupEmptyStagingDirs`
   only removes directories that are already empty, so anything left by an item
   that ended as `abandoned`, `completed (duplicate album)` or `failed (no
   results)` — or by a cancelled transfer — stays on disk indefinitely. Row 27
   recorded the sweep as clean at a moment when no such item had yet run.

1. **RESOLVED — see *Clean-slate bring-up* finding 1.** A duplicate library path used to return 500. `POST /api/libraries` with an
   existing path surfaces the `idx_libraries_path` violation as
   `internal server error` rather than a 409 with a readable message.
2. **BY DESIGN** — see *Clean-slate bring-up*: the worker runs up to `MaxConcurrentJobs`
   acquisitions at once. A stalled peer blocks its own item, not the queue. Three jobs
   (`10`, `68`, `76`) were observed in `running` state with items downloading at
   the same time under one `worker_id`, so a stalled peer blocks its own item but
   not the whole queue. The earlier note that the worker runs one job at a time
   no longer describes this build; the concurrency limit should be made explicit.
3. **RESOLVED — see *Clean-slate bring-up* findings 2-5.** The endpoint existed but no
   running worker honoured it, and manual SQL was the only way to stop a job. A long acquisition cannot be stopped
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
## Clean-slate bring-up — 2026-09-15

The thing under test here is `docs/BETA_DEPLOYMENT.md` itself. The stack was
destroyed to **zero volumes**, the repository was **cloned fresh** at the commit
below, a new `.env` was built by following step 1, and then only the documented
commands were run — acquire, import, scan, Subsonic browse, stream. Wherever a
step needed a hand edit, an undocumented variable, or manual SQL, the docs or the
code were fixed instead and the run repeated.

| Field | Value |
|---|---|
| Commit | `3d7240a` (`docs/beta-clean-slate-bringup`) |
| Stack | `docker-compose.yml` + `docker-compose.beta.yml` |
| Date | 2026-09-15 |
| Host | Windows + Docker Desktop, Docker 29.7.2 |
| Clone | fresh `git clone` into an empty directory, no inherited `.env` or volumes |
| Environment loaded | `ENVIRONMENT=production`, `CONFIG_ENV=production`, `SUBSONIC_ENABLED=true` |
| `SLSKD_API_KEY` | freshly generated, 32 chars |
| ffmpeg (app image) | 6.1.2 |
| PostgreSQL | 16.15 |
| Go (host, for the test suite) | 1.27.0 |
| Media server | none — NetRunner serves its own Subsonic API (`NAVIDROME_URL` unset) |

### Commands, with observed output

Teardown to zero, then a fresh clone:

```bash
docker compose -f docker-compose.yml -f docker-compose.beta.yml --profile media-server down -v --remove-orphans
# Volume djinn-netrunner_netrunner-music Removed ... (all six removed)
docker volume ls | grep netrunner     # ZERO

git clone --branch docs/beta-clean-slate-bringup <repo> djinn-netrunner
cd djinn-netrunner
cp .env.beta.example .env             # then change every change_me_ value
docker compose -f docker-compose.yml -f docker-compose.beta.yml up -d --build
```

Bring-up reached healthy on its own:

| Check | Observed |
|---|---|
| `GET /api/health` | `{"status":"ok","checks":{"database":{"status":"ok"},"disk":{"status":"ok",...},"slskd":{"status":"ok"}}}` |
| `exec ops-web env` | `SUBSONIC_ENABLED=true`, `CONFIG_ENV=production`, `JWT_SECRET=<set>` |
| `POST /api/auth/register` | `201` |
| `POST /api/auth/login` | `302` + `session_id` cookie (HTMX-first, not JSON) |
| `GET /api/watchlists` | `200` |
| same cookie after `restart ops-web` | `200` — sessions survive, so `JWT_SECRET` reached the process |
| `GET /rest/ping.view?u=<email>&p=<password>` | `<subsonicResponse status="ok" ...>` |

The acquisition leg, in order:

| Step | Observed |
|---|---|
| `POST /api/libraries {"path":"/app/music"}` | `201`, `owner_user_id=2` |
| the same request again | `200` with the **same** library id — idempotent, not a bare 500 |
| `POST /api/artists {"name":"PUP"}` | `201`, MusicBrainz id resolved, no API key needed |
| `POST /api/artists/<id>/sync` | `{"artist":"PUP","job_id":1,"status":"sync_queued"}` |
| job 1 `artist_scan` | `succeeded` |
| job 2 `acquisition` | `running`, 38 items |
| worker log | `Skipped 258 of 1487 results that do not look like playable audio (e.g. unsupported audio format "jpg")` |
| worker log | `Validated 01 CUDDLY.flac (flac, 22.0 MiB, 1m51.815057s)` |
| worker log | `mawst queued the transfer but never started sending — trying another candidate` (45s, not 10m) |
| worker log | `Imported: /app/music/PUP/The Dream Is Over/01 - If This Tour Doesn't Kill You, I Will.mp3` |
| `POST /api/jobs/2/cancel` | `{"job_id":2,"status":"cancelled"}` |
| job 2 after 20s | state `cancelled`, summary `Cancelled by request`, `finished_at` set, 31 queued items `cancelled`, 6 already-imported items left `imported` |
| worker log | `Download from evilnick failed: context canceled` then `Job cancelled, stopping` then `Finished job ... state=cancelled` |
| `POST /api/libraries/<id>/scan` | `{"job_id":3,"message":"scan job queued"}` |
| job 3 `scan` | `succeeded` |
| `SELECT count(*) FROM tracks` | `6` |

Browse and stream the acquisition's output:

| Step | Observed |
|---|---|
| `getIndexes.view` | `artist id="artist-PUP" albumCount="5"`, plus `artist-Pent%20Up%20Pup` (the credited album artist on that release) |
| `getArtist.view&id=artist-PUP` | 5 albums, each with `artistId="artist-PUP"` |
| `getAlbum.view&id=album-Morbid Stuff/PUP` | `<song id="21fd8648-..." title="See You At Your Funeral" ... artistId="artist-PUP" albumId="album-Morbid Stuff/PUP">` |
| `stream.view&id=21fd8648-...` | `http:200 bytes:30672869 type:audio/m4a`, magic `ftypM4A` |
| `ffprobe` on the streamed bytes | `mov,mp4,m4a,3gp,3g2,mj2`, `duration=220.133333`, `bit_rate=1114701` |
| `ffprobe` on the library file | byte-identical: same duration, same size, same bitrate |

A second sync enqueued another acquisition. Cancelling it produced no scan, which
is how a real gap surfaced — see *Findings* below; after the fix the worker
logged both halves of the recovery:

```
INFO Finalized orphaned cancellation worker_id=worker-86b5e4cd job_id=7 job_type=acquisition
INFO Queued library scan after acquisition library_id=2b050a1a-... path=/app/music job_id=10
```

### Final state

| Item | Value |
|---|---|
| Containers | `netrunner-postgres`, `netrunner-slskd`, `ops-web`, `ops-worker` — all `healthy` |
| Volumes | six, all created during this run (`postgres-data`, `music`, `downloads`, `slskd-data`, `config`, `logs`) |
| Jobs | `artist_scan` x2 `succeeded`; `acquisition` x3 `cancelled`; `scan` x2 `succeeded`; `release_monitor` x3 `succeeded` |
| Library | 8 tracks, 3 artists, 8 albums indexed; 8 audio files under `/app/music` |
| Folders | canonical album-artist folders (`/app/music/PUP/<Album>/`, `/app/music/Pent Up Pup/FURGAG/`); no per-credit fragmentation |
| Staging | 12 leftover directories in `/app/downloads` (see open findings) |

### Findings, all fixed in this commit

Every one of these was hit by the run, not found by reading code.

| # | Symptom | Cause | Fix |
|---|---|---|---|
| 1 | `POST /api/libraries` at an existing path returned a bare `500` | the unique index on `libraries.path` was left to fail, so the collision surfaced as an internal error and the operator had to re-point `owner_user_id` in SQL | resolve it in the handler: `200` with the existing row when the caller owns it, `409` naming that row otherwise |
| 2 | `POST /api/jobs/:id/cancel` returned `200` and did nothing | the endpoint wrote `cancelled`, and no running worker ever re-read that state; `finishJob` then overwrote it | the worker probes the row each tick, cancels the in-flight context, and finishes as `cancelled` without overwriting it |
| 3 | a cancelled job's aborted item showed `failed` with a retry scheduled that nothing would ever run | the item sweep treated only queued/running/downloading as pending | a `failed` item with `next_attempt_at` is pending work; cancel stops it and clears the schedule |
| 4 | cancelling an acquisition finalized it without refreshing the index, so its imports stayed invisible | `finishCancelledJob` is a second finalizer and skipped the post-acquisition work | it now runs the same release bookkeeping and index refresh as `finishJob` |
| 5 | a job cancelled after a worker restart stayed `cancelled` with no `finished_at` and no summary, forever | no worker owned it, so no job goroutine ever ran the teardown | a janitor in the tick loop finalizes `cancelled` jobs that are finished-at-less and not in this worker's active set |
| 6 | `getAlbum` returned `songCount` with no `<song>` children, so artist to album to track dead-ended with no id to stream | the album response was built without its tracks | `getAlbum` carries its songs, sharing one track-to-song rendering with `getSong` |
| 7 | an acquisition's imports never appeared without a manual scan | the post-acquisition refresh could only talk to an external media server, so on the simplest beta it always failed | with no media server configured it queues a local `scan` job for the library at `MUSIC_LIBRARY`, skipping one already pending |
| 8 | the documented CSRF refresh silently wiped the session cookie, so the next request `403`d | `curl -s -c $JAR` without `-b` rewrites the jar from scratch; harmless only where a fresh jar precedes it | both refresh lines now pass `-b $JAR -c $JAR`, and the `403` troubleshooting row explains why |
| 9 | `NAVIDROME_ADMIN_PASSWORD` you set was silently replaced by a placeholder | `.env.beta.example` declared the variable twice and compose's `env_file` takes the last occurrence | the duplicate is gone; the variable is declared once, empty, matching its comment |

Two smaller documentation gaps the run also exposed: `login` answers `302` with a
Set-Cookie rather than JSON, and a `+` in an email address is a space in a query
string, so Subsonic requests need it URL-encoded. Both are now noted.

### Open findings (not blocking)

- **Staging directories accumulate.** 12 directories remained in `/app/downloads`
  after the cancelled acquisitions. The sweep only removes directories that are
  already empty, and a cancelled transfer leaves partial files behind, so they
  persist. Tracked as DJI-490.
- **A cancelled item is reported but not retried.** By design — a cancel is the
  user's decision — but it means a partly-acquired release stays partly acquired
  until the next sync re-enqueues it.
- **`getAlbum` songs report `duration="0"` and an empty `contentType`.** The
  stream and the file are both correct; only these two attributes are unpopulated
  for tracks whose `format`/duration the scanner did not record.

## Case-canonicalised album identity — 2026-09-15

DJI-489. The library already contained the failure this fixes: one album held as two
case-variant folders, `/app/music/PUP/The Unraveling Of Puptheband/` beside
`/app/music/PUP/The Unraveling of Puptheband/`, backed by acquisitions #9 and #4.

### What changed

| Where | Before | After |
|---|---|---|
| album dedup key | `artist = ? AND album = ?` — case-sensitive in both PostgreSQL and SQLite | `LOWER(TRIM(artist)) = ? AND LOWER(TRIM(album)) = ?`, earliest row first |
| artist / album casing used for the path | whatever the file's tags happened to say | the earliest acquisition's casing, else an existing library folder, else the tag |

### Live re-acquire, through the app's own agent tool

    {"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"enqueue_acquisition",
      "arguments":{"artist":"PUP","title":"Totally Fine","album":"The Unraveling Of Puptheband"}}}
    Acquisition job #14 enqueued for: PUP - Totally Fine

Job #14 is the decisive case, because the MusicBrainz recording was **new**
(`53881f92-ff2b-40ac-952c-e05569fd3e77`) — so the recording-ID dedup that short-circuited
the earlier attempt could not fire, and only the album dedup could:

    Selected: @@znjxx\Soulseek Downloads\pup - (2022) the unraveling of puptheband\PUP - The Unraveling Of Puptheband - 02 - Totally Fine.mp3
    OK  | Download completed
    OK  | Found recording via search: 53881f92-ff2b-40ac-952c-e05569fd3e77
    OK  | Album already acquired (existing acquisition #4 at /app/music/PUP/The Unraveling of Puptheband/03 - Robot Writes a Love Song.mp3). Skipping track.

The tags and the peer's folder both say "Of"; the row it matched is #4 — the earliest,
lowercase "of" — which is the canonical folder. A case-sensitive key either matches the
later case variant or matches nothing and imports another copy.

| Evidence | Result |
|---|---|
| Item #90 | `completed (duplicate album)` |
| Item #90 `final_path` | `/app/music/PUP/The Unraveling of Puptheband/03 - Robot Writes a Love Song.mp3` — the canonical folder |
| Files under `/app/music/PUP/The Unraveling*` | unchanged: still the same two tracks in the two pre-existing folders |
| Folders under `/app/music` | unchanged: `Noriyuki Iwadare`, `PUP`, `Pent Up Pup` — no case-variant folder created |
| `acquisitions` rows | still 9; nothing recorded for *Totally Fine* |
| Staged duplicate | removed and its emptied directory swept |

An earlier re-acquire (job #12, item #89) of a track already in the library was
short-circuited by the MusicBrainz recording-ID dedup, reporting the folder of
acquisition #9. That exercises a different branch and is not evidence for the case fold.

The unit tests pin the same behaviour at the comparison level, including one asserting
that the pre-fix case-sensitive comparison cannot see the case variant at all; making
`CanonicalKey` an identity function turns seven of them red.

### Open finding this run exposed

- **The recording-ID dedup branch leaves the staged file behind.** `/app/downloads/THE UNRAVELING OF PUPTHEBAND/12 PUPTHEBAND Inc. Is Filing for Bankruptcy.mp3` survived job #12, whereas the album branch removes the staged file and sweeps the directory it emptied. Same class as DJI-490's leftovers.
## Case-variant album repair, applied to the live library - 2026-09-15

DJI-475 took two passes, because the tooling could not see the damage it was
supposed to repair. (Matrix row 15 records the earlier credit-variant repair;
this is the case-variant one, which that pass was blind to.)

### The tooling gap, found before running it

`DetectFragmentedAlbums` grouped folders by *exact* album name and
`isCreditVariantSet` only accepted a `" & "` credit suffix, while
`MergeAlbumFolders` matched source rows with exact string equality. So a
case-variant split was invisible to `detect-fragments` and unreachable by
`merge-album`: the CLI reported the library **clean** while leaving every
case-variant fragmentation in place. The detector now groups on the canonical
key and classifies credit variants separately from case variants (still
refusing the two-unrelated-bands control), and the merge matcher folds case.

### Dry run

    netrunner-cli library detect-fragments
      [case_album] The Unraveling of Puptheband
        ==> PUP/The Unraveling of Puptheband   (1 track)
            PUP/The Unraveling Of Puptheband   (1 track)

    netrunner-cli library merge-album 2b050a1a-... "The Unraveling of Puptheband" PUP
      moved: 2, duplicates removed: 0, dirs removed: 1, conflicts: 0, errors: 0

### A defect the live run exposed

The **first** apply reported `dirs removed: 0` and left the source folder on
disk. Only the audio file had moved; the album's `.lrc` lyrics sidecar stayed
behind and kept the directory alive. The merge now moves a track's sibling
files with it. The library was restored to its pre-repair state and the merge
re-run, so the fix is proven rather than observed on a half-repaired tree.

### Result

| Check | Result |
|---|---|
| Apply | `Album "The Unraveling of Puptheband" -> /app/music/PUP/The Unraveling of Puptheband [APPLIED]`, `moved: 2, dirs removed: 1` |
| Source folder | gone - `test -d .../The Unraveling Of Puptheband` reports GONE |
| Canonical folder | holds all four files (2 `.mp3` + 2 `.lrc`) |
| `tracks` rows for that album | both point at the canonical path; 0 rows match `%Unraveling Of%` |
| Rescan (job 17) | `succeeded`, summary `Completed` |
| `detect-fragments` after | `No fragmented albums or artists found.` |
| Total tracks | 8 before, 8 after - the merge moved files, it did not add or drop any |

Backups taken first: a `music` tar (159 MB) and a `pg_dump` of the `musicops`
database.

## Browser suite as the pre-release gate - 2026-09-15

`.github/workflows/e2e.yml` watched `branches: [main, develop]` while this
repo's default branch is `master`, so the workflow **never ran once** - "all
checks green" said nothing at all about the browser flows. It also started the
stack itself with `--env-file .env.e2e`, a file that is not in the repo and had
no template, so it would have failed before Playwright started even with a
correct trigger.

Now:

    bash scripts/e2e.sh test     # or: gh workflow run e2e.yml

The script materialises `.env.e2e` from the checked-in `.env.e2e.example` when
missing, installs Chromium, and runs Playwright. Playwright owns the stack
lifecycle - its `webServer` runs `e2e/setup-test-db.sh` (build, drop and
recreate `musicops_test`, start, seed) and its `globalTeardown` tears down with
`-v` when `CI=true` - so CI and local runs share one bring-up path instead of
two that drift apart.

It is not a `pull_request` gate: it builds every image and drives real
Chromium. It runs on push to `master` and on `workflow_dispatch`, which is the
manual pre-release step.

While wiring this up, `docker-compose.e2e.yml` was found to hardcode
`musicops:testpass` in `DATABASE_URL` while the postgres role takes its
password from `POSTGRES_PASSWORD` - two independent sources for one secret, so
any `.env.e2e` with a different password produced a stack that looked healthy
and then failed auth. The overlay now interpolates `${POSTGRES_PASSWORD}`, and
pins `ENVIRONMENT: development` so a developer's local `.env` (read by the base
compose's `env_file`) cannot put the e2e stack into production mode.


### First real run — 211 passed, 18 skipped, 2 failed

    https://github.com/pvnkmnk/djinn-netrunner/actions/runs/34954793696
    Run E2E suite   2 failed   18 skipped   211 passed (4.9m)

Two specs failed, both stale assertions rather than app defects — which is what
an unrun suite accumulates. Neither fix was applied to the app, and in one case
it must not be.

| Spec | Asserted | Why it was wrong | Fix |
|---|---|---|---|
| `auth.spec.ts` | `csrfCookie.httpOnly \|\| csrfCookie.secure` | The `csrf_` cookie is deliberately **not** `httpOnly`: the double-submit pattern requires the page to read it and echo it as `X-CSRF-Token` (`layouts/base.html`, `app.js`). Satisfying this assertion would have broken every state-changing request in the UI. `secure` is unreachable too — the suite runs over plain http, where a browser will not store a `Secure` cookie. | Assert `httpOnly === false` and `sameSite === 'Lax'`. |
| `dashboard.spec.ts` | `body.checks.gonic` | `GetHealth` reports every check except `database` only when its dependency is configured. This stack runs no media server, so no `gonic` key — and the repo has since moved to Navidrome. The assertion encoded one deployment's shape. | Assert `database` plus the contract: every reported check carries a `status`, and at most one media server is reported. |

Worth recording plainly: the CSRF assertion was the kind that gets "fixed" in the
wrong direction. It reads as a hardening requirement, so the tempting response
is to set `CookieHTTPOnly` on the CSRF middleware — which silently breaks the
HTMX UI while turning the test green.


## Acceptance run at the merge commit — 2026-09-15

The beta criterion, executed against `master` after PR #231 merged. Task 3
(case-variant repair) and Task 5 (hygiene) were already recorded above and were
**not** repeated.

| Field | Value |
|---|---|
| Commit | `5d29d29` (merge of #231; `d0cf0a8` #230 and `59064b7` #229 beneath it) |
| Stack | `docker-compose.yml` + `docker-compose.beta.yml` |
| Date | 2026-09-15 |
| Host | Windows + Docker Desktop |
| Clone | the live beta clone, synced from `3d7240a` to `5d29d29` |
| Volumes at start | **zero** — `down -v --remove-orphans` removed all six |
| `.env` | rebuilt from `.env.beta.example`, all six `change_me_` values replaced |
| Environment loaded | `ENVIRONMENT=production`, `CONFIG_ENV=production`, `SUBSONIC_ENABLED=true` |
| ffmpeg / ffprobe (app image) | 6.1.2 / 6.1.2 |
| PostgreSQL | 16-alpine |
| slskd | `slskd/slskd:latest`, downloads dir `/downloads` |
| Go (host, for the suite) | 1.27.0 |
| Media server | none — NetRunner serves its own Subsonic API |

### Proving the running binary came from the merge commit, not a stale image

Rebuilding is not evidence that the rebuild *took*. Two separate things were
checked, and it is worth being precise about what each one establishes.

**What the build consumed.** The clone was moved onto the merge commit and left
clean before anything was built, so the build input is identified by its own
revision rather than by a timestamp:

```
$ git rev-parse HEAD          # in the deployment clone
5d29d29...
$ git status --short
                              # empty
```

`docker compose up -d --build` then built the images from that working tree in
the same sequence, and `docker image inspect` shows them created during this run.

**What is actually running.** The literal strings introduced by #231 are present
in the worker's binary, which rules out a container still serving a pre-#231
image:

```
$ docker compose ... exec -T ops-worker sh -c "grep -ac 'Staging cleanup failed' /app/netrunner-worker"
2
$ ... "grep -ac 'Removed empty staging dir' /app/netrunner-worker"
1
$ ... "grep -ac 'canonical identity lookup failed' /app/netrunner-worker"
1
```

`ops-web` reports 0 for all three — those symbols live in the worker's
acquisition path. The component that matters for every clause below is the
worker.

**The limit of the marker check.** A string appearing in a binary does not by
itself prove *which* revision produced it: two builds whose source both contain
those literals look identical here. The provenance above rests on the clean tree
at `5d29d29` plus the build that followed it; the greps corroborate that the
running binary is not older than #231. What would make the claim airtight is an
image revision baked in at build time (`org.opencontainers.image.revision`), which
these images do not carry — worth adding, but that is a change to the build rather
than to this record.

### Matrix (continues from row 40)

| # | Check | Result | Evidence |
|---|---|---|---|
| 41 | Start from zero volumes | PASS | `docker volume ls \| grep netrunner` → ZERO; no `netrunner-*` containers left |
| 42 | Fresh `.env` from the template alone | PASS | `cp .env.beta.example .env`, all six `change_me_` values replaced; no hand edits, no undocumented variables |
| 43 | Bring-up reaches healthy unaided | PASS | `up -d --build`; all four containers `healthy` with no intervention |
| 44 | The running app is the merge commit | PASS | #231 literals present in `/app/netrunner-worker` (above) |
| 45 | Stack health | PASS | `{"status":"ok","checks":{"database":{"status":"ok"},"disk":{"status":"ok","message":"directory accessible"},"slskd":{"status":"ok"}}}` HTTP 200 |
| 46 | Configuration reaches both containers | PASS | `JWT_SECRET`, `SUBSONIC_ENABLED`, `CONFIG_ENV`, `SLSKD_API_KEY`, `ALLOW_PRIVATE_TARGETS` all present in `ops-web` **and** `ops-worker` |
| 47 | ffmpeg/ffprobe in the app image | PASS | `ffmpeg version 6.1.2`, `ffprobe version 6.1.2` inside `ops-web` |
| 48 | Register + login | PASS | register `201 {"status":"ok"}`; login `302` + `Set-Cookie: session_id` (HttpOnly, SameSite=Strict), body 0 bytes (HTMX-first) |
| 49 | Session survives a restart | PASS | same cookie jar: `/api/libraries` and `/api/watchlists` both `200` after `restart ops-web` |
| 50 | Production refuses to boot without `JWT_SECRET` | PASS | `ERROR Failed to load config error="JWT_SECRET is required in production: without it sessions are invalidated on every restart"`, exit 1 |
| 51 | Production refuses Subsonic without a shared password | PASS | `ERROR ... "SUBSONIC_PASSWORD is required in production when SUBSONIC_ENABLED=true"`, exit 1. A control with the password present got past config and failed only on a deliberately fake DB host — so the check is specific, not a generic boot failure |
| 52 | Subsonic ping, account password (`p=`) | PASS | `<subsonicResponse status="ok" version="1.16.1" type="netrunner">` |
| 53 | Subsonic ping, token auth (`t=`/`s=`) | PASS | `status="ok"`; a wrong token returns `error code="40" message="Authentication failed"` |
| 54 | Create a library owned by the caller | PASS | `201`, `"owner_user_id":1` |
| 55 | Library path reuse is not a 500 | PASS | same request → `200` with the **same** id `b864b6db-…` |
| 56 | Artist added and resolved | PASS | `201`, `MusicBrainzID=3165f5e0-44ff-446a-81d7-c09ec69661ae`, `OwnerUserID=1` |
| 57 | Sync queues the pipeline | PASS | `{"artist":"PUP","job_id":2,"status":"sync_queued"}`; job 2 `artist_scan succeeded`, job 3 `acquisition running` |
| 58 | Real acquisition imports from real peers | PASS | 9 items `imported`; worker log `Download queued from xelosnim (id: 36562ff6-…)`, `Selected: … [FLAC 16]`, `Found 50 results` |
| 59 | Imports land in canonical album folders | PASS | `/app/music/PUP/Morbid Stuff/`, `…/The Dream Is Over/`, `…/Who Will Look After the Dogs/`, `…/THE UNRAVELING OF PUPTHEBAND/` — one folder per album, no per-credit fragmentation |
| 60 | Tags carry a canonical album artist | PASS | `ffprobe` on `Morbid Stuff/06 - Closure.m4a` → `artist=PUP`, `album_artist=PUP` |
| 61 | Cancellation reaches a terminal state | PASS | `POST /api/jobs/3/cancel` → `{"job_id":3,"status":"cancelled"}`; the row became `cancelled`, summary `Cancelled by request`, `finished_at` `19:53:38`; worker log `Job cancelled, stopping … job_id=3` then `Finished job … state=cancelled` |
| 62 | A cancel keeps what it imported | PASS | 31 items `cancelled`, **7 `imported` preserved** on job 3; job 7 likewise kept its 2 imports |
| 63 | **Zero residual directories in `/app/downloads` once every job is terminal** | PASS | `directories: 0`, `files: 0`, `all entries: 0` — `find /app/downloads -mindepth 1` printed nothing, confirmed from both `ops-worker` and `ops-web`. Every jobitem terminal: `cancelled 45`, `imported 9`, `completed (duplicate album) 1`, `failed (no results) 1` |
| 64 | Post-acquisition index refresh after a cancel | PASS | job 4 `scan succeeded` at `19:53:43`, five seconds after the cancel finalized — the cancelled finalizer queued it itself |
| 65 | A duplicate re-acquisition is recognised | PASS | job 7 item 40 → `completed (duplicate album)`; `job_logs`: `Album already acquired (existing acquisition #8 at /app/music/PUP/PUP/07 - Lionheart.mp3). Skipping track.` |
| 66 | The dedup path leaves no staging residue | PASS | staging stayed 0 through the duplicate (the branch routes through `discardStagedDownload`) |
| 67 | Scan indexes every acquired file | PASS | 9 audio files on disk → `9 tracks, 3 artists, 9 albums` |
| 68 | `getIndexes` exposes resolvable ids | PASS | `artist id="artist-PUP" name="PUP" albumCount="7"`; no bare `artist-` placeholder |
| 69 | `getArtist` round-trip | PASS | `getArtist.view?id=artist-PUP` → top-level `<artist>` with 7 albums, each `artistId="artist-PUP"` |
| 70 | `getAlbum` carries its songs | PASS | `album-Morbid Stuff/PUP` → `<song id="06e18fc5-…" … artistId="artist-PUP" albumId="album-Morbid Stuff/PUP">` |
| 71 | Streaming returns real audio | PASS | `stream.view` → HTTP 200, 22,991,052 bytes, `audio/m4a`, magic `ftypM4A`, `ffprobe` → `mov,mp4,m4a,3gp,3g2,mj2`, `duration=188.41`. **SHA-256 identical** to the library file on disk |
| 72 | `scripts/beta-smoke.sh` | PASS | `Beta smoke: all checks passed.` — 28 checks including session persistence, both Subsonic schemes, and `ffprobe present in ops-worker and decoded a generated tone` |
| 73 | Unit + integration suites and vet | PASS | `go build ./...`, `go vet ./...`, `go vet -tags integration ./...` all clean; `go test -count=1 ./...` every package `ok` (services 94s, ffmpeg live) |

### The criterion, stated plainly

`/app/downloads` holding **zero** entries after both a cancellation and a
duplicate-rejection is satisfied here. That is the clause DJI-490 left open,
where the previous run recorded *12 leftover directories*. Every path that ends
an item without importing — cancel and abandon, and both dedup branches — now
routes through `discardStagedDownload`, which removes the file and then sweeps
upward, stopping at the first directory that still holds entries.

### Findings this run produced

Neither was found by reading code.

**OPEN (not blocking) — the folder is canonicalised, the tag is not, so Subsonic still splits the artist.** Acquisition #8 imported with peer tags `artist=Pup`, `album=PUP`. The folder was correctly canonicalised to `/app/music/PUP/PUP/`, but the tags written into the file kept the peer's casing:

```
$ ffprobe … "/app/music/PUP/PUP/07 - Lionheart.mp3"
TAG:artist=Pup
TAG:album_artist=Pup
TAG:album=PUP

$ ffprobe … "/app/music/PUP/Morbid Stuff/06 - Closure.m4a"   # a peer that said PUP
TAG:artist=PUP
TAG:album_artist=PUP
```

Subsonic groups by tag, so `getIndexes` lists **both** `artist-PUP` (7 albums)
and `artist-Pup` (1 album) for the same artist. DJI-489 canonicalised the
folder and the dedup key; the tag axis still carries the raw peer value. This is
the same defect class on the axis that decides what a client actually sees.

**OPEN (not blocking) — nothing checks that the downloaded file matches what was requested.** Job 3 item 1 requested `artist=PUP, album=PUP, track_title=PUP` and imported:

```
/app/music/Noriyuki Iwadare/Ace Attorney Investigations: Miles Edgeworth
Original Soundtrack/17 - Shi-Long Lang - Speak Up, Pup!.m4a
```

A peer served an unrelated track whose filename happens to contain "Pup". It
passed the plausibility and `ffprobe` gates, and the importer organised it by its
own embedded tags, leaving a second and entirely unrelated artist in the library.
The existing gates validate that a file *is playable audio*, not that it *is the
audio that was asked for*.

**Observation, not a finding:** `ffprobe` prints `[png @ …] chunk too big` for the
embedded cover on that mp3. Harmless here, but it is the same family of
malformed cover atoms that made `audiometa` unusable for MP4 `covr`, and it is
worth knowing that the files reaching the library do carry them.

**Resolved by this run:** the earlier *Open findings* entry "Staging directories
accumulate" (12 left over, DJI-490) no longer reproduces — see row 63. The
`getAlbum` song attributes still report `duration="0"`, unchanged from the
earlier record and still the one open Subsonic polish item.

### Documentation defects this run exposed, now fixed

The thing under test is `docs/BETA_DEPLOYMENT.md`, and the documented steps
themselves carried two defects. Both are fixed there; neither needed a workaround
in the run.

| # | Symptom | Cause | Fix |
|---|---|---|---|
| 1 | The documented `ARTIST_ID=$(python -c "…json.load(open('…'))['id']")` raises `KeyError: 'id'` | `POST /api/artists` returns Go's default field names (`"ID"`), while `POST /api/libraries` returns `"id"`. The doc assumed lowercase for both | Parse whichever key is present, reading the response from stdin so no temp file is needed |
| 2 | The same lines stage the response in `/tmp/*.json`, which plain `python` cannot open on the documented Windows host | Git Bash's `/tmp` is the Windows temp directory; a native Python resolves `/tmp` to `C:\tmp` | Pipe the response into the parser instead of staging it through `/tmp` |

One more, recorded rather than fixed: the compose **service** is `postgres` while
the **container** is `netrunner-postgres`, so `docker compose exec
netrunner-postgres …` fails with `service "netrunner-postgres" is not running`.
This run used `docker exec netrunner-postgres` for its SQL probes; the docs name
neither, so there was nothing wrong to correct.

### Final state

| Item | Value |
|---|---|
| Containers | `netrunner-postgres`, `netrunner-slskd`, `ops-web`, `ops-worker` — all `healthy` |
| Volumes | six, all created during this run |
| Images | `djinn-netrunner-ops-web` / `ops-worker`, built during the run from `5d29d29` |
| Jobs | `release_monitor` ×1, `artist_scan` ×2, `acquisition` ×2 (`cancelled`), `scan` ×5 — every one terminal with a `finished_at` |
| Items | `cancelled` 45, `imported` 9, `completed (duplicate album)` 1, `failed (no results)` 1 |
| Library | 9 tracks / 3 artists / 9 albums indexed; 9 audio files plus 4 `.lrc` sidecars under `/app/music` |
| Staging | **0 entries** in `/app/downloads` |
| Suites | `go build`, `go vet` (both), `go test -count=1 ./...` all green |

### Independent re-verification — 2026-09-15

The record above was written from the run. This section is a second pass over the
same live stack, made from a fresh shell after the run ended, to show the headline
clause is a durable state rather than a momentary one. Queries are reproduced
verbatim so the claim can be re-run.

```bash
$ docker exec ops-worker sh -c 'find /app/downloads -mindepth 1 | wc -l'
0

$ docker exec ops-worker sh -c 'ls -la /app/downloads'
total 8
drwxr-xr-x    2 netrunner netrunner      4096 Sep 15 19:59 .
drwxr-xr-x    1 netrunner netrunner      4096 Sep 15 19:42 ..

$ docker exec netrunner-postgres psql -U musicops -d musicops -tAc \
    "select status, count(*) from jobitems group by status order by 2 desc;"
cancelled|45
imported|9
completed (duplicate album)|1
failed (no results)|1

$ docker exec netrunner-postgres psql -U musicops -d musicops -tAc \
    "select count(*) from jobs where finished_at is null;"
0
```

The staging directory's own mtime (`19:59`) is from the last removal, not the
query, so the zero is the state the sweep left behind rather than something a
later cleanup produced.

| Check | Re-queried | Against |
|---|---|---|
| entries under `/app/downloads` | **0** | row 63 |
| `jobitems` by status | `cancelled 45`, `imported 9`, `completed (duplicate album) 1`, `failed (no results) 1` | row 63 — identical |
| jobs | `release_monitor` 1, `artist_scan` 2, `acquisition` 2 (`cancelled`), `scan` **6** | +1 `scan` vs the table above |
| jobs lacking `finished_at` | **0** | rows 61, 63 |

The one delta is a single extra `scan` job, queued by the smoke run that followed
the record being written; the snapshot table above is left as it stood at the end
of the run. `docker exec netrunner-postgres` is used rather than
`docker compose exec postgres` because the container name and the compose service
name differ — see the note above.
### False-positive control, recorded from a live run — 2026-09-15

The repair procedure's step 4 asks for the false-positive control to be confirmed
before anything is merged, and this record had no row for it: the guard was covered
by `TestDetectFragmentedAlbums_StillIgnoresUnrelatedSameNameAlbums`, but never
observed against a real library. Both halves are recorded here in one run, because
a negative alone cannot be told apart from a detector that has quietly stopped
working.

A throwaway library was seeded *beside* the real one — a separate library at
`/tmp/ctl-fixture`, so the live library was never touched — and `detect-fragments`
was run once over both:

| Pair | Expectation | Observed |
|---|---|---|
| `Band A/Greatest Hits` + `Band B/GREATEST HITS` | not flagged — unrelated artists sharing an album name | **absent from the report** |
| `Some Artist/Best Of` + `Some Artist/BEST OF` | flagged — one artist, case-only album split | `BEST OF` / `Best Of`, `Kind: case_album`, `TrackCount: 2` |
| the real library at `/app/music` | no fragments (Task 3 repaired it) | `"albums": null, "artists": null` |

The negative control is the sharp one. Grouping folds the album name, so both pairs
land in a single group and the *artist agreement* clause is the only thing that can
reject the first pair — and that clause is exactly what a bare case check would have
accepted, moving `Band B`'s track underneath `Band A`. Seeing the second pair flagged
in the same run is what proves the clause is not merely rejecting everything.

```
$ docker exec ops-web /tmp/netrunner-cli library detect-fragments --json
[
  {
    "library_id": "11111111-1111-1111-1111-111111111111",
    "library_name": "CTL fixture",
    "albums": [
      {
        "Album": "BEST OF",
        "CanonicalAlbum": "BEST OF",
        "Kind": "case_album",
        "TrackCount": 2,
        "CanonicalFolder": "Some Artist",
        "Folders": [
          { "ArtistFolder": "Some Artist", "AlbumFolder": "BEST OF", "TrackCount": 1, "IsCanonical": true },
          { "ArtistFolder": "Some Artist", "AlbumFolder": "Best Of", "TrackCount": 1, "IsCanonical": false }
        ]
      }
    ],
    "artists": null
  },
  {
    "library_id": "b864b6db-63cf-4e7c-971f-e335713afded",
    "library_name": "Music",
    "albums": null,
    "artists": null
  }
]
```

The fixture is removed afterwards — the library row and its four track rows deleted,
`/tmp/ctl-fixture` removed — leaving the stack at one library and nine tracks, exactly
as it was.

One detail worth reading deliberately rather than as a defect: `CanonicalAlbum` is
`BEST OF`, not `Best Of`. With no acquisition history to consult, resolution falls
through to the filesystem's *sorted* order, which puts the uppercase folder first.
That is the documented order (history, then filesystem, then the tag verbatim) doing
what it says; it simply has no history to prefer here.
