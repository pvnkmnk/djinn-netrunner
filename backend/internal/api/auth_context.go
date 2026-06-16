package api

import (
	"log/slog"

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

func isHTMXRequest(c *fiber.Ctx) bool {
	return c.Get("HX-Request") == "true"
}

func requirePageUser(c *fiber.Ctx) (database.User, bool, error) {
	user, ok := currentUserFromLocals(c)
	if !ok {
		return database.User{}, false, c.Redirect("/", fiber.StatusFound)
	}
	return user, true, nil
}

func requirePartialUser(c *fiber.Ctx) (database.User, bool, error) {
	user, ok := currentUserFromLocals(c)
	if !ok {
		if isHTMXRequest(c) {
			return database.User{}, false, c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return database.User{}, false, c.Redirect("/", fiber.StatusFound)
	}
	return user, true, nil
}

// internalServerError logs the error and returns a generic 500 response.
// The actual error detail is never sent to the client — only logged server-side.
func internalServerError(c *fiber.Ctx, err error) error {
	slog.Error("Internal server error", "error", err)
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
}
