# NetRunner — Status & Roadmap

## Current Status: beta acceptance criterion met

The beta acceptance criterion — a real Soulseek download acquired, imported into
the canonical folder, tagged, scanned into the library, playable through Subsonic
by an external client, with sessions that survive a restart — is met against a
deployed stack, not a test harness.

> **The beta deployment path is retired.** The canonical assets are
> `docker-compose.release.yml`, `.env.release.example` and `docs/DEPLOYMENT.md`,
> brought up by `./scripts/deploy.sh`; `./scripts/smoke.sh` is the gate. The
> acceptance record is history, not a bring-up guide.

**The live record is [`docs/BETA_ACCEPTANCE.md`](docs/BETA_ACCEPTANCE.md)**, which
carries the observed output and environment per row. This file is the roadmap;
that one is the evidence. Where they disagree, believe that one.

The ordered work queue lives in Linear as the *NetRunner Beta Readiness* project
(`DJI-###`). Read it before trusting any list below.

> This file previously read "All Cycles Complete / v0.0.1" and listed as open
> several things that had since shipped. Reconciled on 2026-09-15.

### Shipped

#### Core platform
- [x] Go backend (Fiber + GORM + SQLite/PostgreSQL)
- [x] Music acquisition pipeline (Soulseek via slskd)
- [x] Metadata enrichment (MusicBrainz, AcoustID, Discogs, Last.fm, ListenBrainz)
- [x] Library scanning, indexing, and pruning
- [x] Quality profiles with bitrate/speed/format preferences
- [x] Operations UI (HTMX + Pongo2)
- [x] WebSocket console with per-job log streaming
- [x] MCP server for AI agent interaction
- [x] CLI management tool (Cobra)

#### Watchlist providers
- [x] Spotify (sp_dc cookie + OAuth, GraphQL Partner API)
- [x] Last.fm, ListenBrainz, Discogs (paginated, validated)
- [x] RSS/Bandcamp (proxy-aware, channel-title fallback)
- [x] Local files and directories
- [x] Lidarr (wired, config fields added)

#### Worker & orchestration
- [x] Round-robin job dispatch with heartbeat monitoring
- [x] Panic recovery, graceful shutdown, zombie job recovery
- [x] Job cancellation (`POST /api/jobs/:id/cancel`) with cooperative worker pickup
- [x] Lyrics, transcoder, and yt-dlp fallback services wired into the pipeline
- [x] Album-mode acquisition (peer directory browse, track discovery)
- [x] Release monitor background task
- [x] Recording deduplication via AcoustID/MusicBrainz

#### Acquisition hardening
- [x] Remote-queue grace period — a peer stuck in `Queued, Remotely` is dropped for
      another candidate instead of burning the full download timeout (DJI-485)
- [x] Candidate gating — implausibly small files and ffprobe-rejected audio never
      reach the library, with the reason recorded on the job item (DJI-486)
- [x] Staged downloads reclaimed on every non-import exit, including the
      recording-ID dedup branch that used to leave its file behind (DJI-490/492)
- [x] Canonical artist/album identity — case-only differences cannot split an album
      across folders, and the release-group dedup folds case the same way (DJI-489)
- [x] Fragmentation repair tooling sees case variants, not just credit variants, and
      carries a track's sidecar files when merging (DJI-475)

#### Subsonic
- [x] Stable resolvable artist ids and `getArtist`, so a client can browse
      artist → album → track (DJI-487)
- [x] `stream.view` verified to return bytes identical to the library file
- [x] No empty-named placeholder artist in `search3`

#### Security
- [x] Session-cookie auth with RBAC (user/admin)
- [x] BOLA enforcement across all handlers
- [x] WebSocket ownership validation
- [x] HTTPOnly/Secure/SameSite cookies, CSP headers (no inline scripts)
- [x] Proxy-aware HTTP client factory, auth rate limiting
- [x] Production refuses to boot on a missing `JWT_SECRET`, or Subsonic enabled
      without a password

#### Infrastructure
- [x] Docker Compose stack (PostgreSQL 16 + slskd + Caddy)
- [x] Release overlay (`docker-compose.release.yml`) with `.env.release.example`
- [x] SQLite WAL for local dev
- [x] LiteFS primary-node detection and write-forwarding middleware
- [x] PostgreSQL advisory locks with concurrent test coverage
- [x] GORM auto-migrations
- [x] Prometheus metrics, webhook notifications

#### UI & accessibility
- [x] Mobile nav toggle, keyboard navigation, modal focus trap
- [x] Role-based dashboard views (admin sees "All Users" scope)
- [x] Watchlist form with all source types, Spotify sp_dc linking UI

#### Documentation & DX
- [x] `AGENTS.md` with repo map, API reference, MCP tool schemas
- [x] Ops runbooks (backup, upgrade, DR, SQLite→Postgres migration)
- [x] `docs/DEPLOYMENT.md` — the documented deployment path
- [x] Database tier guidance, watchlist provider reference
- [x] ADR 0001: multi-node SQLite with LiteFS
- [x] ADR 0002: config-as-code evaluation (rejected — env vars stay)

#### Testing & CI
- [x] Unit tests across all packages
- [x] Integration test CI workflow (Postgres + slskd)
- [x] Performance benchmarks (job selection, item claim, metadata extraction)
- [x] `go vet` + `govulncheck` in CI
- [x] Docker build to ghcr.io
- [x] Route-table tests, including the Subsonic `.view` suffix rule
- [x] Browser suite runs: `bash scripts/e2e.sh test` (was silently never running —
      the workflow watched `main`/`develop` while the default branch is `master`)

## Known Gaps & Future Work

> Reconciled 2026-10-07 by re-probing each claim rather than inheriting it. Three
> entries below had gone stale: two are struck with what closed them, and one
> named the wrong endpoint. The ordered dev path is
> `docs/superpowers/specs/2026-10-03-release-readiness-initiative-design.md`,
> which now carries per-stage status.

### Release-scoped, still open

- [ ] **Three Subsonic endpoints report a hardcoded `duration="0"`**: 
      `GetAlbumList2` (`subsonic.go:1404`), `GetPlaylists` (:1589) and
      `GetPlaylist` (:1671). Streaming itself is correct and byte-verified; only
      the attribute is unpopulated. **Corrected twice over.** This entry previously
      read "`getAlbum` reports `duration="0"` and an empty `contentType`", and
      both halves were wrong:
      - *Wrong endpoint.* `GetAlbum` sums durations from its own song rows
        (`album.Duration += song.Duration`, `subsonic.go:709`) and is fine. The
        three zeroed sites are the two album/playlist **listings** and playlist
        **detail**; they were found by re-probing, not inherited.
      - *`contentType` is populated.* It is rendered from `track.Format` in six
        places, and `Track.Format` is written by both writers that matter —
        `import_file.go:87` (`metadata.Format = ext`) and
        `scanner_service.go:239`. An imported or scanned track carries it.

      The remaining gap is the same class as the N+1 work #284 did for
      `artistAlbums` — per-item duration is expensive to aggregate in a
      listing — so it belongs inside NR08 (DJI-568) rather than here.
- [x] ~~Playwright specs for artists, playlists, jobs, and admin are absent.~~
      **Closed by re-probe.** All four exist today: `artists.spec.ts`,
      `playlists.spec.ts`, `jobs.spec.ts`, `admin.spec.ts` (plus
      `artist-picker`, `artist-scan`, `dashboard`, `permissions`, `subsonic`).
      DJI-426/428/429/430/433/434 can be evidenced by the browser suite again.
- [ ] **The browser suite is a manual pre-release step**, not a required check. It
      builds every image and drives real Chromium, which is too slow for every PR.
      Still true, and now a deliberate documented decision rather than an
      oversight — `.github/workflows/e2e.yml` states it explicitly and runs on
      `push` to `master` plus `workflow_dispatch`.

### Post-v0.0.1 backlog

- [ ] E2E specs remaining: webhook smoke, quota warning
- [ ] Full browser-verified mobile nav and keyboard accessibility
- [ ] Admin panel with user management UI
- [ ] Horizontal worker scaling via LiteFS write forwarding
- [ ] Multi-environment config file support (if deployment complexity warrants)
- [ ] Making the browser suite a blocking required check, once its runtime is
      acceptable on PRs
