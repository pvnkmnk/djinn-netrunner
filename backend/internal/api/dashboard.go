package api

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

const sessionCookieName = SessionCookie

type DashboardHandler struct {
	db *gorm.DB
}

func NewDashboardHandler(db *gorm.DB) *DashboardHandler {
	return &DashboardHandler{db: db}
}

func (h *DashboardHandler) RenderIndex(c *fiber.Ctx) error {
	// Try to get user from session (optional auth)
	var user database.User
	var authUserID string
	sessionID := c.Cookies(sessionCookieName)
	if sessionID != "" {
		err := h.db.Joins("JOIN sessions ON sessions.user_id = users.id").
			Where("sessions.session_id = ? AND sessions.expires_at > ?", sessionID, time.Now()).
			First(&user).Error
		if err == nil {
			authUserID = strconv.FormatUint(user.ID, 10)
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
	if authUserID != "" {
		wQuery := h.db.Order("name")
		if user.Role != "admin" {
			wQuery = wQuery.Where("owner_user_id = ?", user.ID)
		}
		wQuery.Find(&watchlists)
	}

	// Get quality profiles
	var profiles []database.QualityProfile
	h.db.Order("name").Find(&profiles)

	return c.Render("index", fiber.Map{
		"stats":      stats,
		"jobs":       jobs,
		"watchlists": watchlists,
		"profiles":   profiles,
		"User":       user,
		"authUserID": authUserID,
	})
}
