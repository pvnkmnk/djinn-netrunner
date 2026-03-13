package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

type DashboardHandler struct {
	db *gorm.DB
}

func NewDashboardHandler(db *gorm.DB) *DashboardHandler {
	return &DashboardHandler{db: db}
}

func (h *DashboardHandler) RenderIndex(c *fiber.Ctx) error {
	user := c.Locals("user")

	// Get job stats (last 24h)
	var stats struct {
		QueuedCount    int64
		RunningCount   int64
		SucceededCount int64
		FailedCount    int64
	}

	since := time.Now().Add(-24 * time.Hour)
	h.db.Model(&database.Job{}).Where("requested_at > ?", since).
		Select("COUNT(*) FILTER (WHERE state = 'queued') as queued_count, " +
			"COUNT(*) FILTER (WHERE state = 'running') as running_count, " +
			"COUNT(*) FILTER (WHERE state = 'succeeded') as succeeded_count, " +
			"COUNT(*) FILTER (WHERE state = 'failed') as failed_count").
		Scan(&stats)

	// Get recent jobs
	var jobs []database.Job
	h.db.Order("requested_at DESC").Limit(20).Find(&jobs)

	// Get watchlists
	var watchlists []database.Watchlist
	wQuery := h.db.Order("name")
	if u, ok := user.(database.User); ok && u.Role != "admin" {
	        wQuery = wQuery.Where("owner_user_id = ?", u.ID)
	}
	wQuery.Find(&watchlists)

	// Get quality profiles
	var profiles []database.QualityProfile
	h.db.Order("name").Find(&profiles)

	return c.Render("index", fiber.Map{
	        "stats":      stats,
	        "jobs":       jobs,
	        "watchlists": watchlists,
	        "profiles":   profiles,
	        "user":       user,
	})

}
