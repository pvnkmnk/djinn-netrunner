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
	// Try to get user from session (optional auth)
	var user *database.User
	sessionID := c.Cookies(SessionCookie)
	if sessionID != "" {
		var dbUser database.User
		err := h.db.Joins("JOIN sessions ON sessions.user_id = users.id").
			Where("sessions.session_id = ? AND sessions.expires_at > ?", sessionID, time.Now()).
			First(&dbUser).Error
		if err == nil {
			user = &dbUser
		}
	}

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

	// Get watchlists (only for authenticated users)
	var watchlists []database.Watchlist
	if user != nil {
		wQuery := h.db.Order("name")
		if user.Role != "admin" {
			wQuery = wQuery.Where("owner_user_id = ?", user.ID)
		}
		wQuery.Find(&watchlists)
	}

	// Get quality profiles
	var profiles []database.QualityProfile
	h.db.Order("name").Find(&profiles)

	// Pass auth status to template
	isAuthenticated := user != nil

	return c.Render("index", fiber.Map{
		"stats":           stats,
		"jobs":            jobs,
		"watchlists":      watchlists,
		"profiles":        profiles,
		"User":            user,
		"IsAuthenticated": isAuthenticated,
	})

}
