# Phase 4 Implementation: Metadata Enrichment

## Overview
Implement metadata enrichment features: extended track model, Discogs integration, cover art enrichment, batch re-enrichment.

## Tasks

### Task 1: Extend Track Model
**File:** `backend/internal/database/models.go`

Add fields to Track struct:
- Year *int
- Genre string  
- Composer string
- CoverURL string

---

### Task 2: Add DiscogsService
**File:** `backend/internal/services/discogs_service.go` (new)

Create DiscogsService with methods:
- SearchRelease(artist, title) - Search Discogs for release
- GetReleaseDetails(releaseID) - Get detailed release info
- GetCoverArt(releaseID) - Get cover art URL

---

### Task 3: Enhance MusicBrainzService
**File:** `backend/internal/services/musicbrainz_service.go`

Add methods:
- GetReleaseCoverArt(releaseID) - Get cover art from MusicBrainz

---

### Task 4: Add Enrichment Job Type
**Files:** 
- `backend/cmd/worker/main.go` - Add "enrich" case
- Create job logic

Add to runMonolithicJob:
```go
case "enrich":
    libraryID, _ := uuid.Parse(jc.job.ScopeID)
    // Lookup tracks and re-enrich from external sources
```

---

### Task 5: API Endpoints
**File:** `backend/internal/api/libraries.go`

Add:
- POST /api/libraries/:id/enrich - Trigger enrichment job

---

### Task 6: CLI Commands
**File:** `backend/cmd/cli/main.go`

Add to libraryCmd:
- `enrich [id]` - Trigger metadata enrichment
- `coverart [id]` - Download cover art

---

## Implementation Order
1. Task 1: Extend Track model
2. Task 2: Add DiscogsService
3. Task 3: Enhance MusicBrainzService
4. Task 4: Add Enrichment job type
5. Task 5: API endpoints
6. Task 6: CLI commands
