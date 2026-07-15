***
skill: acquisition-pipeline
version: 1
repo: netrunner
language: go
tags: [domain, acquisition, slskd, metadata, jobs]
***

# Skill: Acquisition Pipeline Workflow

## Purpose
Operate and verify the end-to-end acquisition pipeline from queued job item to imported library track.

## Prerequisites
- Running worker and slskd connectivity.
- Valid quality profile and watchlist-generated job items.

## Core Concepts
- Pipeline stages: search -> score/profile filter -> download -> metadata extract -> fingerprint/enrich -> move/import.
- Job state and item state are persisted (`jobs`, `jobitems`, `acquisitions`).
- Advisory locks enforce per-scope exclusivity.

## Step-by-Step Procedures
1. Ensure acquisition jobs exist (via watchlist sync or manual enqueue).
2. Observe worker execution logs for item transitions.
3. Confirm successful items create acquisition records and final library path.
4. Validate library scan/index refresh if needed.

## Code Patterns
Acquisition handler initialization:
```go
acqHandler := services.NewAcquisitionHandler(db, cfg, slskd, mb, aid, metadata, gonic, navidrome, discogs, cache, lyrics, transcoder, ytdlp)
```

## Validation
- Job transitions to `succeeded` with sensible summary.
- `jobitems` transition from `queued` to terminal states (`imported`, `failed`, `skipped`).
- `acquisitions` row is created for imported items.

## Edge Cases & Error Handling
- Handle stale running jobs through zombie cleanup logic.
- Respect profile matching (`IsMatch`) to avoid low-quality imports.
- If external APIs fail, ensure retries/failures are logged, not silently dropped.

## References
- `backend/internal/services/job_handlers.go`
- `backend/internal/services/slskd_service.go`
- `backend/internal/services/metadata_extractor.go`
- `backend/cmd/worker/main.go`
- `backend/internal/database/models.go`