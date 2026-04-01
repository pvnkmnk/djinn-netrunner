package api

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

const sessionCookieName = "session_id"

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

	return c.Render("index", fiber.Map{
		"User":       user,
		"authUserID": authUserID,
	})
}
