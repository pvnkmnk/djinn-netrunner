# Artist Tracking & Scheduler Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Complete the job pipeline by adding artist tracking (add artists by name, periodic discography sync, automatic acquisition jobs) and scheduler (CRUD for watchlist sync schedules).

**Architecture:**
- Artist Tracking: User provides artist name → API searches MusicBrainz → gets MBID → creates MonitoredArtist record → ReleaseMonitorService runs hourly → SyncDiscography fetches releases → creates acquisition jobs for new releases
- Scheduler: CRUD API for schedules → worker schedulerLoop() already polls every 30s → creates sync jobs when due

**Tech Stack:** Go, GORM, Fiber, MusicBrainz API, PostgreSQL/SQLite

---

## Task 1: MusicBrainz Artist Search

**Files:**
- Modify: `backend/internal/services/musicbrainz_service.go`
- Test: `backend/internal/services/musicbrainz_service_test.go`

**Step 1: Write failing test**

```go
// In musicbrainz_service_test.go
func TestSearchArtistByName(t *testing.T) {
    mb := NewMusicBrainzService(nil)
    results, err := mb.SearchArtist("Radiohead")
    require.NoError(t, err)
    require.Greater(t, len(results), 0)
    assert.Equal(t, "Radiohead", results[0].Name)
    assert.NotEmpty(t, results[0].ID) // MBID
}
```

**Step 2: Run test**

```bash
cd backend && go test ./internal/services/... -run TestSearchArtistByName -v
```

Expected: FAIL - method not defined

**Step 3: Implement SearchArtist**

Add to `musicbrainz_service.go`:

```go
type MusicBrainzArtist struct {
    ID           string `json:"id"`
    Name         string `json:"name"`
    SortName     string `json:"sort-name"`
    Disambiguation string `json:"disambiguation"`
}

// SearchArtist searches MusicBrainz for an artist by name
func (s *MusicBrainzService) SearchArtist(query string) ([]MusicBrainzArtist, error) {
    url := fmt.Sprintf("%s/ws/2/artist?query=artist:%s&fmt=json&limit=5", s.baseURL, url.QueryEscape(query))
    
    req, _ := http.NewRequest("GET", url, nil)
    req.Header.Set("User-Agent", "netrunner/1.0 (contact@example.com)")
    
    resp, err := s.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    var result struct {
        Artists []struct {
            ID           string `json:"id"`
            Name         string `json:"name"`
            SortName     string `json:"sort-name"`
            Disambiguation string `json:"disambiguation"`
        } `json:"artists"`
    }
    
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, err
    }
    
    artists := make([]MusicBrainzArtist, len(result.Artists))
    for i, a := range result.Artists {
        artists[i] = MusicBrainzArtist{
            ID:           a.ID,
            Name:         a.Name,
            SortName:     a.SortName,
            Disambiguation: a.Disambiguation,
        }
    }
    return artists, nil
}
```

**Step 4: Run test**

```bash
cd backend && go test ./internal/services/... -run TestSearchArtistByName -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add backend/internal/services/musicbrainz_service.go backend/internal/services/musicbrainz_service_test.go
git commit -m "feat: add MusicBrainz artist search by name"
```

---

## Task 2: Monitored Artists API Endpoints

**Files:**
- Create: `backend/internal/api/artists.go`
- Modify: `backend/cmd/server/main.go` (wire up routes)
- Test: `backend/internal/api/artists_test.go`

**Step 1: Write failing test**

```go
// In artists_test.go
func TestAddMonitoredArtist(t *testing.T) {
    // Test POST /api/artists with artist name
    // Should return the created artist with MBID
}
```

**Step 2: Run test**

Expected: FAIL - endpoint not defined

**Step 3: Implement Artists API**

Create `backend/internal/api/artists.go`:

```go
package api

import (
    "github.com/gofiber/fiber/v2"
    "github.com/pvnkmnk/netrunner/backend/internal/services"
    "gorm.io/gorm"
)

type ArtistsHandler struct {
    db         *gorm.DB
    atService  *services.ArtistTrackingService
    mbService  *services.MusicBrainzService
}

func NewArtistsHandler(db *gorm.DB, at *services.ArtistTrackingService, mb *services.MusicBrainzService) *ArtistsHandler {
    return &ArtistsHandler{db: db, atService: at, mbService: mb}
}

// GET /api/artists - List monitored artists
func (h *ArtistsHandler) List(c *fiber.Ctx) error {
    artists, err := h.atService.GetMonitoredArtists()
    if err != nil {
        return c.Status(500).JSON(fiber.Map{"error": err.Error()})
    }
    return c.JSON(artists)
}

// POST /api/artists - Add new artist by name
func (h *ArtistsHandler) Add(c *fiber.Ctx) error {
    var payload struct {
        Name             string `json:"name"`
        QualityProfileID string `json:"quality_profile_id"`
    }
    
    if err := c.BodyParser(&payload); err != nil {
        return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
    }
    
    if payload.Name == "" {
        return c.Status(400).JSON(fiber.Map{"error": "name is required"})
    }
    
    // Search MusicBrainz
    results, err := h.mbService.SearchArtist(payload.Name)
    if err != nil || len(results) == 0 {
        return c.Status(404).JSON(fiber.Map{"error": "artist not found in MusicBrainz"})
    }
    
    artist := results[0]
    
    // Get quality profile
    var profileID uuid.UUID
    if payload.QualityProfileID != "" {
        profileID, _ = uuid.Parse(payload.QualityProfileID)
    } else {
        // Get default profile
        var profile database.QualityProfile
        if err := h.db.Where("is_default = ?", true).First(&profile).Error; err == nil {
            profileID = profile.ID
        }
    }
    
    // Create monitored artist
    monitored, err := h.atService.AddMonitoredArtist(artist.ID, profileID)
    if err != nil {
        return c.Status(400).JSON(fiber.Map{"error": err.Error()})
    }
    
    return c.Status(201).JSON(monitored)
}

// DELETE /api/artists/:id - Remove monitored artist
func (h *ArtistsHandler) Delete(c *fiber.Ctx) error {
    id, err := uuid.Parse(c.Params("id"))
    if err != nil {
        return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
    }
    
    if err := h.atService.DeleteMonitoredArtist(id); err != nil {
        return c.Status(500).JSON(fiber.Map{"error": err.Error()})
    }
    
    return c.JSON(fiber.Map{"status": "deleted"})
}

// PATCH /api/artists/:id - Update artist monitoring settings
func (h *ArtistsHandler) Update(c *fiber.Ctx) error {
    id, err := uuid.Parse(c.Params("id"))
    if err != nil {
        return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
    }
    
    var payload struct {
        Monitored *bool `json:"monitored"`
    }
    
    if err := c.BodyParser(&payload); err != nil {
        return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
    }
    
    if payload.Monitored != nil {
        if err := h.atService.UpdateArtistStatus(id, *payload.Monitored); err != nil {
            return c.Status(500).JSON(fiber.Map{"error": err.Error()})
        }
    }
    
    return c.JSON(fiber.Map{"status": "updated"})
}
```

Add to `ArtistTrackingService`:

```go
// DeleteMonitoredArtist removes an artist from monitoring
func (s *ArtistTrackingService) DeleteMonitoredArtist(id uuid.UUID) error {
    return s.db.Delete(&database.MonitoredArtist{}, "id = ?", id).Error
}
```

**Step 4: Run tests**

```bash
cd backend && go build ./... && go test ./internal/api/... -v
```

**Step 5: Commit**

```bash
git add backend/internal/api/artists.go backend/internal/services/artist_tracking_service.go
git commit -m "feat: add monitored artists API endpoints"
```

---

## Task 3: Release → Acquisition Jobs

**Files:**
- Modify: `backend/internal/services/artist_tracking_service.go`
- Test: `backend/internal/services/artist_tracking_service_test.go`

**Step 1: Write failing test**

```go
func TestSyncDiscographyCreatesAcquisitionJob(t *testing.T) {
    // Setup: create monitored artist with quality profile
    // Call: SyncDiscography
    // Assert: acquisition job created for new releases
}
```

**Step 2: Run test**

Expected: FAIL - method not creating jobs

**Step 3: Implement job creation**

Modify `SyncDiscography` in `artist_tracking_service.go`:

```go
func (s *ArtistTrackingService) SyncDiscography(artistID uuid.UUID, profileID uuid.UUID, ownerUserID *uint64) error {
    // ... existing fetch and parse logic ...
    
    // After processing releases, create acquisition jobs for new ones
    var newReleases []database.TrackedRelease
    s.db.Where("artist_id = ? AND status = ?", artistID, "wanted").Find(&newReleases)
    
    if len(newReleases) > 0 {
        // Create acquisition job
        job := database.Job{
            Type:        "acquisition",
            State:       "queued",
            ScopeType:   "artist",
            ScopeID:     artistID.String(),
            OwnerUserID: ownerUserID,
        }
        s.db.Create(&job)
        
        // Create job items
        for i, rel := range newReleases {
            item := database.JobItem{
                JobID:           job.ID,
                Sequence:        i,
                NormalizedQuery: rel.Title,
                Artist:          artist.Name,
                Album:           rel.Title,
                Status:          "queued",
                OwnerUserID:     ownerUserID,
            }
            s.db.Create(&item)
            
            // Mark release as queued
            s.db.Model(&rel).Update("status", "queued")
        }
    }
    
    return nil
}
```

**Step 4: Run test**

```bash
cd backend && go test ./internal/services/... -run TestSyncDiscography -v
```

**Step 5: Commit**

```bash
git add backend/internal/services/artist_tracking_service.go
git commit -m "feat: create acquisition jobs for new releases from discography sync"
```

---

## Task 4: Schedule CRUD API

**Files:**
- Create: `backend/internal/api/schedules.go`
- Test: `backend/internal/api/schedules_test.go`

**Step 1: Write failing test**

```go
func TestCreateSchedule(t *testing.T) {
    // POST /api/schedules with watchlist_id, cron_expr, timezone
    // Should create schedule and return it
}
```

**Step 2: Run test**

Expected: FAIL - endpoint not defined

**Step 3: Implement Schedule API**

Create `backend/internal/api/schedules.go`:

```go
package api

import (
    "github.com/gofiber/fiber/v2"
    "github.com/pvnkmnk/netrunner/backend/internal/database"
    "github.com/robfig/cron/v3"
    "github.com/google/uuid"
    "gorm.io/gorm"
)

type SchedulesHandler struct {
    db *gorm.DB
}

func NewSchedulesHandler(db *gorm.DB) *SchedulesHandler {
    return &SchedulesHandler{db: db}
}

// GET /api/schedules - List all schedules
func (h *SchedulesHandler) List(c *fiber.Ctx) error {
    var schedules []database.Schedule
    h.db.Preload("Watchlist").Find(&schedules)
    return c.JSON(schedules)
}

// POST /api/schedules - Create new schedule
func (h *SchedulesHandler) Create(c *fiber.Ctx) error {
    var payload struct {
        WatchlistID string `json:"watchlist_id"`
        CronExpr    string `json:"cron_expr"`
        Timezone    string `json:"timezone"`
        Enabled    *bool  `json:"enabled"`
    }
    
    if err := c.BodyParser(&payload); err != nil {
        return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
    }
    
    // Validate cron
    if _, err := cron.ParseStandard(payload.CronExpr); err != nil {
        return c.Status(400).JSON(fiber.Map{"error": "invalid cron expression"})
    }
    
    watchlistID, err := uuid.Parse(payload.WatchlistID)
    if err != nil {
        return c.Status(400).JSON(fiber.Map{"error": "invalid watchlist_id"})
    }
    
    tz := payload.Timezone
    if tz == "" {
        tz = "UTC"
    }
    
    enabled := true
    if payload.Enabled != nil {
        enabled = *payload.Enabled
    }
    
    schedule := database.Schedule{
        WatchlistID: watchlistID,
        CronExpr:    payload.CronExpr,
        Timezone:    tz,
        Enabled:     enabled,
    }
    
    if err := h.db.Create(&schedule).Error; err != nil {
        return c.Status(500).JSON(fiber.Map{"error": err.Error()})
    }
    
    return c.Status(201).JSON(schedule)
}

// DELETE /api/schedules/:id - Delete schedule
func (h *SchedulesHandler) Delete(c *fiber.Ctx) error {
    id, err := uuid.Parse(c.Params("id"))
    if err != nil {
        return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
    }
    
    if err := h.db.Delete(&database.Schedule{}, "id = ?", id).Error; err != nil {
        return c.Status(500).JSON(fiber.Map{"error": err.Error()})
    }
    
    return c.JSON(fiber.Map{"status": "deleted"})
}

// PATCH /api/schedules/:id - Update schedule
func (h *SchedulesHandler) Update(c *fiber.Ctx) error {
    id, err := uuid.Parse(c.Params("id"))
    if err != nil {
        return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
    }
    
    var payload struct {
        CronExpr *string `json:"cron_expr"`
        Timezone *string `json:"timezone"`
        Enabled  *bool  `json:"enabled"`
    }
    
    if err := c.BodyParser(&payload); err != nil {
        return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
    }
    
    updates := make(map[string]interface{})
    
    if payload.CronExpr != nil {
        if _, err := cron.ParseStandard(*payload.CronExpr); err != nil {
            return c.Status(400).JSON(fiber.Map{"error": "invalid cron expression"})
        }
        updates["cron_expr"] = *payload.CronExpr
    }
    
    if payload.Timezone != nil {
        updates["timezone"] = *payload.Timezone
    }
    
    if payload.Enabled != nil {
        updates["enabled"] = *payload.Enabled
    }
    
    if err := h.db.Model(&database.Schedule{}).Where("id = ?", id).Updates(updates).Error; err != nil {
        return c.Status(500).JSON(fiber.Map{"error": err.Error()})
    }
    
    return c.JSON(fiber.Map{"status": "updated"})
}
```

**Step 4: Run tests**

```bash
cd backend && go build ./... && go test ./internal/api/... -v
```

**Step 5: Commit**

```bash
git add backend/internal/api/schedules.go
git commit -m "feat: add schedule CRUD API endpoints"
```

---

## Task 5: Wire ReleaseMonitorService in Worker

**Files:**
- Modify: `backend/cmd/worker/main.go`

**Step 1: Check current initialization**

```bash
grep -n "ReleaseMonitorService\|rmService" backend/cmd/worker/main.go
```

**Step 2: Add initialization**

In `main.go`, add to worker initialization:

```go
// After other service initialization
rmService := services.NewReleaseMonitorService(db, atService)
rmService.StartBackgroundTask()
```

**Step 3: Run build**

```bash
cd backend && go build ./...
```

**Step 4: Commit**

```bash
git add backend/cmd/worker/main.go
git commit -m "feat: wire ReleaseMonitorService background task in worker"
```

---

## Task 6: Run Full Test Suite

**Step 1: Run all tests**

```bash
cd backend && go test ./... -v 2>&1 | tail -30
```

Expected: All pass

**Step 2: Commit**

```bash
git commit -m "test: verify all tests pass after artist tracking and scheduler"
```

---

## Plan Complete

**Total Tasks:** 6
**Estimated Time:** 45-60 minutes

---

## Execution Choice

**Plan complete and saved to `docs/plans/2026-03-17-artist-tracking-scheduler.md`.**

Two execution options:

1. **Subagent-Driven (this session)** - I dispatch fresh fixer subagent per task, review between tasks, fast iteration

2. **Parallel Session (separate)** - Open new session with executing-plans skill, batch execution with checkpoints

Which approach?
