package api

import (
	"fmt"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// StatsData holds the stats for the dashboard
type StatsData struct {
	QueuedCount    int64
	RunningCount   int64
	SucceededCount int64
	FailedCount    int64
}

// getGormDB extracts the GORM database from fiber context
func getGormDB(c *fiber.Ctx) (*gorm.DB, error) {
	db := c.Locals("db")
	if db == nil {
		return nil, fmt.Errorf("DB not available")
	}
	return db.(*gorm.DB), nil
}

// RenderStatsPartial returns stats HTML for HTMX
func RenderStatsPartial(c *fiber.Ctx) error {
	gormDB, err := getGormDB(c)
	if err != nil {
		log.Printf("Error getting DB: %v", err)
		return c.SendString("<div class=\"error\">Error loading stats.</div>")
	}

	var stats StatsData

	since := time.Now().Add(-24 * time.Hour)

	// Use conditional aggregation for efficient single-query stats
	if err := gormDB.Model(&database.Job{}).Where("requested_at > ?", since).
		Select("COUNT(*) FILTER (WHERE state = 'queued') as queued_count, " +
			"COUNT(*) FILTER (WHERE state = 'running') as running_count, " +
			"COUNT(*) FILTER (WHERE state = 'succeeded') as succeeded_count, " +
			"COUNT(*) FILTER (WHERE state = 'failed') as failed_count").
		Scan(&stats).Error; err != nil {
		log.Printf("Error fetching stats: %v", err)
		return c.SendString("<div class=\"error\">Error loading stats.</div>")
	}

	return c.Render("partials/stats", fiber.Map{
		"stats": stats,
	})
}

// RenderWatchlistsPartial returns watchlists HTML for HTMX
func RenderWatchlistsPartial(c *fiber.Ctx) error {
	gormDB, err := getGormDB(c)
	if err != nil {
		return c.SendString("<div class=\"error\">Error loading watchlists.</div>")
	}

	var watchlists []database.Watchlist
	if err := gormDB.Order("name").Find(&watchlists).Error; err != nil {
		log.Printf("Error fetching watchlists: %v", err)
		return c.SendString("<div class=\"error\">Error loading watchlists.</div>")
	}

	return c.Render("partials/watchlists", fiber.Map{
		"watchlists": watchlists,
	})
}

// RenderLibrariesPartial returns libraries HTML for HTMX
func RenderLibrariesPartial(c *fiber.Ctx) error {
	gormDB, err := getGormDB(c)
	if err != nil {
		return c.SendString("<div class=\"error\">Error loading libraries.</div>")
	}

	var libraries []database.Library
	if err := gormDB.Order("name").Find(&libraries).Error; err != nil {
		log.Printf("Error fetching libraries: %v", err)
		return c.SendString("<div class=\"error\">Error loading libraries.</div>")
	}

	return c.Render("partials/libraries", fiber.Map{
		"libraries": libraries,
	})
}

// RenderSchedulesPartial returns schedules HTML for HTMX
func RenderSchedulesPartial(c *fiber.Ctx) error {
	gormDB, err := getGormDB(c)
	if err != nil {
		return c.SendString("<div class=\"error\">Error loading schedules.</div>")
	}

	var schedules []database.Schedule
	if err := gormDB.Preload("Watchlist").Order("created_at desc").Find(&schedules).Error; err != nil {
		log.Printf("Error fetching schedules: %v", err)
		return c.SendString("<div class=\"error\">Error loading schedules.</div>")
	}

	return c.Render("partials/schedules", fiber.Map{
		"schedules": schedules,
	})
}

// RenderJobsPartial returns jobs HTML for HTMX
func (h *StatsHandler) RenderJobsPartial(c *fiber.Ctx) error {
	var jobs []database.Job
	query := h.db.Order("requested_at DESC").Limit(50)

	// Apply filters if provided
	jobType := c.Query("job_type")
	state := c.Query("state")

	if jobType != "" {
		query = query.Where("job_type = ?", jobType)
	}
	if state != "" {
		query = query.Where("state = ?", state)
	}

	if err := query.Find(&jobs).Error; err != nil {
		log.Printf("Error fetching jobs: %v", err)
		return c.SendString("<div class=\"error\">Error loading jobs.</div>")
	}

	return c.Render("partials/jobs", fiber.Map{
		"jobs": jobs,
	})
}
