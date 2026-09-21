package api

import (
	"strconv"

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
	// Try to get user from middleware locals (optional auth for landing page).
	var user database.User
	var authUserID string
	if localUser, ok := currentUserFromLocals(c); ok {
		user = localUser
		authUserID = strconv.FormatUint(user.ID, 10)
	}

	// RenderPage, not a bare c.Render: the base layout's footer renders
	// {{ Version }}, and RenderPage is what supplies it from AppVersion.
	// Rendering directly left the variable empty, so the one page every
	// signed-in user lands on showed "NetRunner v" with no version while
	// every other page showed it.
	return RenderPage(c, "dashboard", "index", fiber.Map{
		"User":       user,
		"authUserID": authUserID,
		"IsAdmin":    user.Role == "admin",
	})
}
