package api

import (
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

// RenderStatsPartial returns stats HTML for HTMX
func RenderStatsPartial(c *fiber.Ctx) error {
	db := c.Locals("db")
	if db == nil {
		return c.SendString("<div class=\"error\">DB not available</div>")
	}

	gormDB := db.(*gorm.DB)

	var stats StatsData

	since := time.Now().Add(-24 * time.Hour)

	// Count jobs by state in the last 24 hours
	gormDB.Model(&database.Job{}).Where("requested_at > ?", since).Count(&stats.QueuedCount)
	gormDB.Model(&database.Job{}).Where("requested_at > ? AND state = ?", since, "running").Count(&stats.RunningCount)
	gormDB.Model(&database.Job{}).Where("requested_at > ? AND state = ?", since, "succeeded").Count(&stats.SucceededCount)
	gormDB.Model(&database.Job{}).Where("requested_at > ? AND state = ?", since, "failed").Count(&stats.FailedCount)

	return c.Render("partials/stats", fiber.Map{
		"stats": stats,
	})
}

// RenderWatchlistsPartial returns watchlists HTML for HTMX
func RenderWatchlistsPartial(c *fiber.Ctx) error {
	db := c.Locals("db")
	if db == nil {
		return c.SendString("<div class=\"error\">DB not available</div>")
	}

	gormDB := db.(*gorm.DB)

	var watchlists []database.Watchlist
	gormDB.Order("name").Find(&watchlists)

	return c.Render("partials/watchlists", fiber.Map{
		"watchlists": watchlists,
	})
}
