# NetRunner — Status & Roadmap

## Current Status: beta acceptance criterion met

The beta acceptance criterion — a real Soulseek download acquired, imported into
the canonical folder, tagged, scanned into the library, playable through Subsonic
by an external client, with sessions that survive a restart — is met against a
deployed stack, not a test harness.

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
- [x] Beta overlay (`docker-compose.beta.yml`) with `.env.beta.example`
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
- [x] `docs/BETA_DEPLOYMENT.md` — the documented deployment path
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

### Beta-scoped, still open

- [ ] **Subsonic `getAlbum` reports `duration="0"` and an empty `contentType`** for
      tracks whose duration/format the scanner did not record. Streaming itself is
      correct and byte-verified; only these two attributes are unpopulated.
- [ ] **Playwright specs for artists, playlists, jobs, and admin are absent**, so the
      browser suite cannot evidence DJI-426/428/429/430/433/434. Those stay open.
- [ ] **The browser suite is a manual pre-release step**, not a required check. It
      builds every image and drives real Chromium, which is too slow for every PR.

### Post-v0.0.1 backlog

- [ ] E2E specs remaining: webhook smoke, quota warning
- [ ] Full browser-verified mobile nav and keyboard accessibility
- [ ] Admin panel with user management UI
- [ ] Horizontal worker scaling via LiteFS write forwarding
- [ ] Multi-environment config file support (if deployment complexity warrants)
- [ ] Making the browser suite a blocking required check, once its runtime is
      acceptable on PRs
