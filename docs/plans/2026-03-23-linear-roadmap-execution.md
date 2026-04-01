# NETRUNNER Linear Roadmap — Full Execution Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Execute all remaining Linear issues across Phases 1–4, building from security hardening through ecosystem integration to agent intelligence and v1.0.0 launch.

**Architecture:** Sequential phase execution — each phase builds on the previous. Phase 1 establishes security and test foundation, Phase 2 refactors the acquisition pipeline, Phase 3 adds new capabilities, Phase 4 polishes for launch.

**Tech Stack:** Go 1.25+, GORM, SQLite (modernc.org/sqlite), Fiber, HTMX, Docker, slskd API, MusicBrainz, AcoustID, LRCLIB, FFmpeg, yt-dlp

---

## Phase Status Summary

| Phase | Issues | Status |
|-------|--------|--------|
| Phase 0: Emergency Stabilization | 5 | ✅ All Done |
| Phase 1: Security Hardening + Test Foundation | 10 | ⬜ Backlog |
| Phase 2: Pipeline Architecture + Quality System | 5 | ⬜ Backlog |
| Phase 3: Ecosystem Integration + New Capabilities | 6 | ⬜ Backlog |
| Phase 4: Agent Intelligence + Community Launch | 4 | ⬜ Backlog |
| Onboarding (DJI-1/2/3/4) | 4 | 🗑️ Close these |

---

## Phase 1: Security Hardening + Test Foundation — Weeks 2–4

**Goal:** Close all remaining security findings, establish CI, and build test coverage to 60% on critical paths.

### Task 1: Generate cryptographic OAuth state (DJI-9)

**Files:**
- Modify: `backend/internal/services/auth_service.go` (or wherever OAuth state is generated)

**Step 1:** Locate OAuth state generation code — search for `state` parameter in OAuth flow.

**Step 2:** Replace predictable state with `crypto/rand`:
```go
import "crypto/rand"

func generateOAuthState() (string, error) {
    b := make([]byte, 32)
    if _, err := rand.Read(b); err != nil {
        return "", err
    }
    return hex.EncodeToString(b), nil
}
```

**Step 3:** Write test verifying state is cryptographically random (not sequential/predictable).

**Step 4:** Run `go test ./backend/... -run TestOAuthState -v`

**Step 5:** Commit: `fix: generate cryptographic OAuth state for CSRF protection`

---

### Task 2: Fix registration idempotency leak and session cleanup (DJI-10)

**Files:**
- Modify: `backend/cmd/server/main.go` (auth handlers)
- Modify: `backend/internal/services/auth_service.go`

**Step 1:** Review registration handler for user enumeration vectors — error messages should not reveal whether username/email exists.

**Step 2:** Ensure registration returns identical response for duplicate and new registration (idempotent).

**Step 3:** Add session cleanup on logout — delete session from DB and clear cookie.

**Step 4:** Write tests for:
- Registration with existing username returns same response as new
- Logout clears session

**Step 5:** Run `go test ./backend/... -run TestRegistration -v`

**Step 6:** Commit: `fix: prevent user enumeration in registration, clean up sessions on logout`

---

### Task 3: Add SSRF protection via safeHTTPClient (DJI-11)

**Files:**
- Create: `backend/internal/services/http_client.go`
- Create: `backend/internal/services/http_client_test.go`
- Modify: All services making outbound HTTP calls (cover art, metadata fetches)

**Step 1:** Create `safeHTTPClient` utility:
```go
package services

import (
    "net"
    "net/http"
    "net/url"
)

var privateCIDRs = []string{
    "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
    "127.0.0.0/8", "169.254.0.0/16", "::1/128", "fc00::/7",
}

func isPrivateIP(ip net.IP) bool {
    for _, cidr := range privateCIDRs {
        _, network, _ := net.ParseCIDR(cidr)
        if network.Contains(ip) {
            return true
        }
    }
    return false
}

func SafeGet(rawURL string) (*http.Response, error) {
    u, err := url.Parse(rawURL)
    if err != nil {
        return nil, err
    }
    ips, err := net.LookupIP(u.Hostname())
    if err != nil {
        return nil, err
    }
    for _, ip := range ips {
        if isPrivateIP(ip) {
            return nil, fmt.Errorf("SSRF blocked: private IP %s", ip)
        }
    }
    return http.Get(rawURL)
}
```

**Step 2:** Write tests for private IP detection and SSRF blocking.

**Step 3:** Replace `http.Get` calls in cover art and metadata services with `SafeGet`.

**Step 4:** Run `go test ./backend/... -run TestSafeHTTP -v`

**Step 5:** Commit: `feat: add SSRF protection via safeHTTPClient`

---

### Task 4: Vendor HTMX locally (DJI-12)

**Files:**
- Create: `backend/static/htmx.min.js` (or embed via Go embed)
- Modify: `backend/cmd/server/main.go` (template references)
- Modify: HTML templates referencing CDN HTMX

**Step 1:** Download pinned HTMX version (e.g., 2.0.4).

**Step 2:** Place in `backend/static/` or embed via `//go:embed`.

**Step 3:** Update all `<script>` tags from CDN URL to local path `/static/htmx.min.js`.

**Step 4:** Verify UI loads correctly without CDN.

**Step 5:** Commit: `security: vendor HTMX locally, remove CDN dependency`

---

### Task 5: Fix Caddyfile and add CSP header (DJI-13)

**Files:**
- Modify: `Caddyfile`

**Step 1:** Review current Caddyfile for correct upstream service names.

**Step 2:** Add Content-Security-Policy header:
```
header {
    Content-Security-Policy "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws: wss:"
}
```

**Step 3:** Verify HTMX and WebSocket console still work with CSP.

**Step 4:** Commit: `security: add CSP header to Caddyfile`

---

### Task 6: Replace running flag with context.Context cancellation (DJI-14)

**Files:**
- Modify: `backend/cmd/worker/main.go`
- Modify: `backend/internal/services/` (services using running flags)

**Step 1:** Search for `running` boolean flags in worker and services.

**Step 2:** Replace with `context.Context` + `context.CancelFunc`:
```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
// Pass ctx to goroutines, check ctx.Done() instead of running flag
```

**Step 3:** Ensure all goroutines respect `ctx.Done()` for clean shutdown.

**Step 4:** Write test for graceful shutdown.

**Step 5:** Commit: `refactor: replace running flags with context.Context cancellation`

---

### Task 7: Remove exposed database/service ports in docker-compose (DJI-15)

**Files:**
- Modify: `docker-compose.yml`

**Step 1:** Remove `ports:` mappings for Postgres, Gonic, slskd — keep them on internal Docker network only.

**Step 2:** Ensure Netrunner and Caddy remain the only externally exposed services.

**Step 3:** Test: `docker compose up -d` and verify internal services are not accessible from host.

**Step 4:** Commit: `security: remove exposed database/service ports from docker-compose`

---

### Task 8: Add structured logging with slog (DJI-16)

**Files:**
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/cmd/worker/main.go`
- Modify: All service files with `log.` calls

**Step 1:** Replace `log` imports with `log/slog`.

**Step 2:** Initialize structured logger with appropriate handler (JSON for production, text for dev).

**Step 3:** Replace ad-hoc `log.Printf` calls with `slog.Info`, `slog.Error`, `slog.Warn` with structured fields.

**Step 4:** Run `go build ./backend/...` to verify compilation.

**Step 5:** Commit: `refactor: standardize on log/slog for structured logging`

---

### Task 9: Set up CI with tests and linters (DJI-17)

**Files:**
- Create: `.github/workflows/ci.yml`

**Step 1:** Create GitHub Actions workflow:
```yaml
name: CI
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
      - run: go vet ./backend/...
      - run: go test ./backend/... -race -coverprofile=coverage.out
      - run: golangci-lint run ./backend/ || true
      - run: govulncheck ./backend/ || true
```

**Step 2:** Verify workflow runs on push.

**Step 3:** Commit: `ci: add GitHub Actions workflow with tests and linters`

---

### Task 10: Write tests for critical paths + reach 60% coverage (DJI-18, DJI-19)

**Files:**
- Create/Modify: Test files in `backend/internal/services/`
- Create/Modify: Test files in `backend/cmd/server/`

**Step 1:** Identify untested critical paths: ExecuteItem pipeline, auth flow, watchlist sync, library CRUD.

**Step 2:** Write unit tests using `httptest.NewServer` for external API mocking.

**Step 3:** Write integration tests for auth flow (register → login → access protected endpoint).

**Step 4:** Run `go test ./backend/... -coverprofile=coverage.out` and check coverage.

**Step 5:** Iterate until critical paths reach ~60% coverage.

**Step 6:** Commit: `test: add tests for critical paths, reach 60% coverage`

---

### Task 11: Integration harness with dockerized slskd (DJI-20)

**Files:**
- Create: `backend/internal/services/integration_test.go`
- Create: `docker-compose.test.yml` (optional)

**Step 1:** Create integration test using dockerized slskd or `httptest.NewServer` mock.

**Step 2:** Test end-to-end: search → score → download → import.

**Step 3:** Run `go test ./backend/... -run TestIntegration -v`

**Step 4:** Commit: `test: add integration harness for acquisition pipeline`

---

## Phase 2: Pipeline Architecture + Quality System — Weeks 5–8

**Goal:** Refactor the monolithic acquisition pipeline into testable stages and upgrade quality scoring.

### Task 12: Decompose ExecuteItem into pipeline stages (DJI-21)

**Files:**
- Modify: `backend/internal/services/job_handlers.go`

**Step 1:** Break `ExecuteItem` into named stages:
- `loadItemContext` — load job, item, profile from DB
- `checkGonicIndex` — check if track already in library
- `searchSoulseek` — execute search with rate limiting
- `scoreResults` — apply quality profile scoring
- `downloadFile` — download best match
- `importAndEnrich` — import to library, enrich metadata

**Step 2:** Each stage returns a typed result; pipeline composes them.

**Step 3:** Write unit tests for each stage independently.

**Step 4:** Run `go test ./backend/... -run TestPipeline -v`

**Step 5:** Commit: `refactor: decompose ExecuteItem into testable pipeline stages`

---

### Task 13: Implement album-mode acquisition (DJI-22)

**Files:**
- Modify: `backend/internal/services/job_handlers.go` (or new pipeline stage)
- Modify: `backend/internal/services/slskd_service.go`

**Step 1:** After finding a good track, call `GET /users/{username}/browse` on slskd.

**Step 2:** Inspect peer directory for full album availability.

**Step 3:** If full album available at acceptable quality, offer album-level download.

**Step 4:** Write tests with mocked slskd browse response.

**Step 5:** Commit: `feat: implement album-mode acquisition (search then browse)`

---

### Task 14: Extend QualityProfile with advanced fields (DJI-23)

**Files:**
- Modify: `backend/internal/database/models.go` (QualityProfile struct)
- Create: Migration for new fields
- Modify: Search scoring logic

**Step 1:** Add fields to QualityProfile:
```go
MinSampleRate       int
MinBitDepth         int
FormatPreferenceOrder string // JSON array
FilterMode          string // "preferred" or "required"
MaxPeerQueueDepth   int
```

**Step 2:** Create GORM migration for new columns.

**Step 3:** Wire new fields into search scoring logic.

**Step 4:** Write tests for scoring with new fields.

**Step 5:** Commit: `feat: extend QualityProfile with advanced filtering fields`

---

### Task 15: Soulseek interaction hardening (DJI-24)

**Files:**
- Modify: `backend/internal/services/slskd_service.go`
- Create: `backend/internal/database/models.go` (PeerReputation model)

**Step 1:** Implement rate limiter: 34 searches per 220 seconds.

**Step 2:** Add PeerReputation model with downrank/ignore thresholds.

**Step 3:** Detect and cancel stalled downloads.

**Step 4:** Delete searches via `DELETE /searches/{id}` after processing.

**Step 5:** Write tests for rate limiter and peer reputation.

**Step 6:** Commit: `feat: harden Soulseek interactions with rate limiting and peer reputation`

---

### Task 16: Integration harness with dockerized slskd (DJI-20 — if not done in Phase 1)

*Already covered in Task 11. Skip if completed.*

---

## Phase 3: Ecosystem Integration + New Capabilities — Weeks 9–14

**Goal:** Add transcoding, yt-dlp fallback, Lidarr bridge, lyrics, additional media servers, and tagging upgrades.

### Task 17: Implement FFmpeg-based transcoder service (DJI-25)

**Files:**
- Create: `backend/internal/services/transcoder_service.go`
- Create: `backend/internal/services/transcoder_service_test.go`
- Modify: `backend/internal/database/models.go` (add TranscodeTarget to QualityProfile)

**Step 1:** Create transcoder service that shells out to `ffmpeg`:
```go
func (s *TranscoderService) Transcode(inputPath, outputFormat string) (string, error) {
    outputPath := strings.TrimSuffix(inputPath, filepath.Ext(inputPath)) + "." + outputFormat
    cmd := exec.Command("ffmpeg", "-i", inputPath, "-y", outputPath)
    if err := cmd.Run(); err != nil {
        return "", fmt.Errorf("transcode failed: %w", err)
    }
    return outputPath, nil
}
```

**Step 2:** Add `TranscodeTarget` field to QualityProfile (e.g., "opus", "mp3", "aac").

**Step 3:** Integrate into import pipeline — transcode after download, before library placement.

**Step 4:** Optionally archive originals.

**Step 5:** Write tests with mock FFmpeg or skip if FFmpeg not available.

**Step 6:** Commit: `feat: add FFmpeg-based transcoder service`

---

### Task 18: Add yt-dlp fallback source (DJI-26)

**Files:**
- Create: `backend/internal/services/ytdlp_service.go`
- Create: `backend/internal/services/ytdlp_service_test.go`
- Modify: Acquisition pipeline to try yt-dlp as fallback

**Step 1:** Create yt-dlp service using `exec.Command` or `github.com/lrstanley/go-ytdlp`.

**Step 2:** Integrate into pipeline: try Soulseek first, fall back to yt-dlp if no result meets quality threshold.

**Step 3:** Use flags: `--extract-audio --audio-format flac`.

**Step 4:** Write tests with mocked command execution.

**Step 5:** Commit: `feat: add yt-dlp fallback source for acquisition`

---

### Task 19: Lidarr API bridge (DJI-27)

**Files:**
- Create: `backend/internal/services/lidarr_provider.go`
- Create: `backend/internal/services/lidarr_provider_test.go`

**Step 1:** Implement `WatchlistProvider` interface for Lidarr.

**Step 2:** Poll Lidarr `/api/v1/wanted/missing` and `/api/v1/wanted/cutoff`.

**Step 3:** Map albums to tracks via MusicBrainz release groups.

**Step 4:** Notify Lidarr on completed downloads via `/api/v1/command` (DownloadedTracksScan).

**Step 5:** Write tests with httptest mock of Lidarr API.

**Step 6:** Commit: `feat: add Lidarr API bridge for watchlist and reporting`

---

### Task 20: Lyrics integration via LRCLIB (DJI-28)

**Files:**
- Create: `backend/internal/services/lyrics_service.go`
- Create: `backend/internal/services/lyrics_service_test.go`
- Modify: Import pipeline to fetch and embed lyrics

**Step 1:** Create lyrics service querying LRCLIB API.

**Step 2:** Fetch synced LRC where available, plain text otherwise.

**Step 3:** Embed in tags (USLT/LYRICS) and write `{track}.lrc` next to audio files.

**Step 4:** Write tests with httptest mock of LRCLIB API.

**Step 5:** Commit: `feat: add lyrics integration via LRCLIB`

---

### Task 21: Additional media server support (DJI-29)

**Files:**
- Create: `backend/internal/services/navidrome_client.go`
- Modify: Import pipeline to trigger Plex/Jellyfin refresh

**Step 1:** Add Navidrome client using Subsonic-compatible API (reuse GonicClient pattern).

**Step 2:** Add Plex trigger: `GET /library/sections/{id}/refresh`.

**Step 3:** Add Jellyfin trigger: `POST /Library/Refresh`.

**Step 4:** Write tests with httptest mocks.

**Step 5:** Commit: `feat: add Navidrome, Plex, and Jellyfin media server support`

---

### Task 22: Tagging pipeline hardening (DJI-30)

**Files:**
- Modify: `backend/internal/services/` (tagging/enrichment code)
- Modify: `backend/internal/database/models.go` (add EnrichmentProvenance)

**Step 1:** Add confidence threshold for AcoustID matches (reject below 0.7).

**Step 2:** Enrich genres from Discogs (using existing `discogs_service.go`).

**Step 3:** Add `EnrichmentProvenance` field to Track model (which source wrote which tag fields).

**Step 4:** Migrate hashing from MD5 to SHA-256.

**Step 5:** Write tests for confidence threshold and provenance tracking.

**Step 6:** Commit: `feat: harden tagging pipeline with confidence thresholds and provenance`

---

## Phase 4: Agent Intelligence + Community Launch — Weeks 15–20

**Goal:** Make Netrunner the reference implementation for AI-driven music automation and prep v1.0.0 release.

### Task 23: MCP agent hardening (DJI-31)

**Files:**
- Modify: `backend/cmd/agent/` (MCP server)
- Create: JSON schema files for tool responses
- Create: `backend/internal/database/models.go` (AgentAuditLog)

**Step 1:** Define JSON schemas for all 18+ MCP tool responses.

**Step 2:** Add DryRun mode to all mutation tools.

**Step 3:** Introduce AgentAuditLog model (who/what/when/result).

**Step 4:** Support natural-language job descriptions.

**Step 5:** Add idempotency keys for job-creation endpoints.

**Step 6:** Write tests for DryRun mode and audit logging.

**Step 7:** Commit: `feat: harden MCP agent with schemas, DryRun, and audit logging`

---

### Task 24: Configuration as code — YAML + CLI (DJI-32)

**Files:**
- Create: `backend/cmd/cli/main.go` (or extend existing CLI)
- Create: YAML schema/parser for job definitions

**Step 1:** Support declarative YAML job definitions (watchlist, profiles, schedules).

**Step 2:** Add CLI entrypoint: `netrunner apply -f acquisition.yaml`.

**Step 3:** Ensure idempotent, version-controlled config application.

**Step 4:** Write tests for YAML parsing and idempotent apply.

**Step 5:** Commit: `feat: add configuration as code (YAML + CLI)`

---

### Task 25: Webhook system upgrade (DJI-33)

**Files:**
- Modify: `backend/internal/services/notification_service.go`
- Create: `backend/internal/database/models.go` (WebhookTarget)

**Step 1:** Introduce typed events: `job.started`, `job.completed`, `job.failed`, `item.downloaded`, `item.imported`, `artist.newrelease`, `quota.warning`.

**Step 2:** Add WebhookTarget model with per-event config.

**Step 3:** Implement retry with exponential backoff.

**Step 4:** Add HMAC-SHA256 signing for webhook payloads.

**Step 5:** Write tests for event dispatch and retry logic.

**Step 6:** Commit: `feat: upgrade webhook system with typed events and HMAC signing`

---

### Task 26: Docker deployment hardening (DJI-34)

**Files:**
- Modify: `docker-compose.yml`
- Modify: `backend/Dockerfile`
- Create: Healthcheck endpoints

**Step 1:** Add PUID/PGID env support for file permissions.

**Step 2:** Document shared volume strategy for netrunner/slskd/gonic.

**Step 3:** Add healthcheck endpoints and `unless-stopped` restart policy.

**Step 4:** Ensure internal services (Postgres, Gonic, slskd) are not exposed externally.

**Step 5:** Commit: `ops: harden Docker deployment with healthchecks and PUID/PGID`

---

### Task 27: Documentation and community launch (DJI-35)

**Files:**
- Modify: `README.md`
- Create: `CONTRIBUTING.md`
- Create: OpenAPI spec
- Create: Docker Compose examples

**Step 1:** Rewrite README with quick-start, architecture diagram, and feature matrix.

**Step 2:** Provide Docker Compose examples (SQLite minimal, full Postgres+Caddy+Lidarr).

**Step 3:** Generate OpenAPI spec for API docs.

**Step 4:** Add `CONTRIBUTING.md`.

**Step 5:** Prepare and tag v1.0.0 release.

**Step 6:** Commit: `docs: rewrite README, add CONTRIBUTING.md, prep v1.0.0 release`

---

## Cleanup Tasks

### Close Linear onboarding issues (DJI-1, DJI-2, DJI-3, DJI-4)

These are Linear's default onboarding issues and can be moved to Canceled/Done state.

### Close or repopulate Netrunner Hardening Sprint project

This project has 0 issues — either close it or move relevant issues from Phase 1 into it.

---

## Execution Order

```
Phase 1 (Weeks 2-4): Security + Tests
  ├─ DJI-9  OAuth state         ─┐
  ├─ DJI-10 Registration fix     │ Can parallelize
  ├─ DJI-11 SSRF protection     ─┘
  ├─ DJI-12 Vendor HTMX         ─┐
  ├─ DJI-13 CSP header           │ Can parallelize
  ├─ DJI-14 Context cancellation ─┘
  ├─ DJI-15 Docker ports        ─┐
  ├─ DJI-16 Structured logging   │ Can parallelize
  └─ DJI-17 CI setup            ─┘
  └─ DJI-18/19 Tests + Coverage (after CI)
  └─ DJI-20 Integration harness (after tests)

Phase 2 (Weeks 5-8): Pipeline + Quality
  ├─ DJI-21 Decompose ExecuteItem (prerequisite for 22-24)
  ├─ DJI-22 Album mode           ─┐
  ├─ DJI-23 QualityProfile       │ Can parallelize after 21
  └─ DJI-24 Soulseek hardening  ─┘

Phase 3 (Weeks 9-14): Ecosystem
  ├─ DJI-25 Transcoder           ─┐
  ├─ DJI-26 yt-dlp               │ Can parallelize
  ├─ DJI-27 Lidarr               │
  ├─ DJI-28 Lyrics               │
  ├─ DJI-29 Media servers        │
  └─ DJI-30 Tagging             ─┘

Phase 4 (Weeks 15-20): Launch
  ├─ DJI-31 MCP hardening        ─┐
  ├─ DJI-32 Config as code       │ Can parallelize
  ├─ DJI-33 Webhooks             │
  ├─ DJI-34 Docker hardening     ─┘
  └─ DJI-35 Docs + v1.0.0 (last)
```

---

## Verification Checklist (per task)

After each task:
1. `go fmt ./backend/...`
2. `goimports -local github.com/pvnkmnk/netrunner -w ./backend/`
3. `go vet ./backend/...`
4. `go test ./backend/... -run <TestName> -v`
5. Verify changes align with `docs/ARCHITECTURE.md` invariants
6. Ensure new features exposed via both CLI and MCP server
