package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

// currentUserFromLocals extracts authenticated user context set by AuthMiddleware.
// It supports both value and pointer forms and rejects zero-value users.
func currentUserFromLocals(c *fiber.Ctx) (database.User, bool) {
	if u, ok := c.Locals("user").(database.User); ok && u.ID != 0 {
		return u, true
	}
	if u, ok := c.Locals("user").(*database.User); ok && u != nil && u.ID != 0 {
		return *u, true
	}
	return database.User{}, false
}
