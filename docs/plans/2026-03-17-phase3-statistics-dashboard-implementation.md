# Phase 3 Implementation: Statistics/Dashboard

## Overview
Implement enhanced statistics and dashboard analytics for better operational visibility.

## Tasks

### Task 1: Add Statistics API Handler
**File:** `backend/internal/api/stats.go` (new)

Create a StatsHandler with methods:
- `GetJobStats(c *fiber.Ctx)` - Job analytics (success rate, by type, trends)
- `GetLibraryStats(c *fiber.Ctx)` - Library analytics (tracks, storage, formats)
- `GetActivityStats(c *fiber.Ctx)` - Activity metrics (downloads, artists)
- `GetSummary(c *fiber.Ctx)` - Combined overview

**Implementation:**
```go
type StatsHandler struct {
    db *gorm.DB
}

func NewStatsHandler(db *gorm.DB) *StatsHandler {
    return &StatsHandler{db: db}
}

type JobStats struct {
    Total       int64   `json:"total"`
    Queued      int64   `json:"queued"`
    Running     int64   `json:"running"`
    Succeeded   int64   `json:"succeeded"`
    Failed      int64   `json:"failed"`
    SuccessRate float64 `json:"success_rate"`
}

type JobTypeBreakdown struct {
    Type      string  `json:"type"`
    Total     int64   `json:"total"`
    Succeeded int64   `json:"succeeded"`
    Failed    int64   `json:"failed"`
}

type DailyJobTrend struct {
    Date      string  `json:"date"`
    Succeeded int64   `json:"succeeded"`
    Failed    int64   `json:"failed"`
}
```

---

### Task 2: Wire Up Stats Routes
**Files:**
- `backend/cmd/server/main.go` - Add StatsHandler
- `backend/cmd/server/main_test.go` - Update test

Add routes:
```go
statsHandler := api.NewStatsHandler(db)
// ...
statsRoutes := apiProtected.Group("/stats")
statsRoutes.Get("/jobs", statsHandler.GetJobStats)
statsRoutes.Get("/library", statsHandler.GetLibraryStats)
statsRoutes.Get("/activity", statsHandler.GetActivityStats)
statsRoutes.Get("/summary", statsHandler.GetSummary)
```

---

### Task 3: Enhance Dashboard Template
**File:** `ops/web/templates/dashboard.html` (or similar)

Add UI sections for:
- Success rate visualization
- Job trends chart
- Library storage info
- Format distribution

Note: If templates don't exist yet, skip this task.

---

### Task 4: Add CLI Stats Commands (Optional)
**File:** `backend/cmd/cli/main.go`

Add:
```go
rootCmd.AddCommand(statsCmd())

func statsCmd() *cobra.Command {
    cmd := &cobra.Command{Use: "stats", Short: "Show statistics"}
    // subcommands: summary, jobs, library
}
```

---

### Task 5: Add Tests
**File:** `backend/internal/api/stats_test.go` (new)

Test stats endpoints return valid JSON with expected fields.

---

## Implementation Order
1. Task 1: Stats API handler (new file)
2. Task 2: Wire up routes
3. Task 3: Dashboard UI (if templates exist)
4. Task 4: CLI commands (optional)
5. Task 5: Tests

## Dependencies
- None (uses existing models and DB)

## Notes
- Use GORM Scans for complex queries
- Keep queries efficient (indexed columns)
- Return sensible defaults for empty data
