package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

// PageData contains common page data
type PageData struct {
	Page string
}

// RenderPage renders a page with common layout
func RenderPage(c *fiber.Ctx, page string, template string, data fiber.Map) error {
	base := fiber.Map{"Page": page}
	for k, v := range data {
		base[k] = v
	}
	return c.Render(template, base)
}

// WatchlistsPage renders the watchlists page
func (h *WatchlistHandler) WatchlistsPage(c *fiber.Ctx) error {
	// Bolt Optimization: Removed redundant database queries.
	// Data is loaded asynchronously via HTMX in the template.
	if _, ok := c.Locals("user").(database.User); !ok {
		return c.Redirect("/", 302)
	}
	return RenderPage(c, "watchlists", "pages/watchlists", fiber.Map{})
}

// LibrariesPage renders the libraries page
func (h *LibraryHandler) LibrariesPage(c *fiber.Ctx) error {
	// Bolt Optimization: Removed redundant database queries.
	// Data is loaded asynchronously via HTMX in the template.
	if _, ok := c.Locals("user").(database.User); !ok {
		return c.Redirect("/", 302)
	}
	return RenderPage(c, "libraries", "pages/libraries", fiber.Map{})
}

// ProfilesPage renders the profiles page
func (h *ProfileHandler) ProfilesPage(c *fiber.Ctx) error {
	// Bolt Optimization: Removed redundant database queries.
	// Data is loaded asynchronously via HTMX in the template.
	if _, ok := c.Locals("user").(database.User); !ok {
		return c.Redirect("/", 302)
	}
	return RenderPage(c, "profiles", "pages/profiles", fiber.Map{})
}

// SchedulesPage renders the schedules page
func (h *SchedulesHandler) SchedulesPage(c *fiber.Ctx) error {
	// Bolt Optimization: Removed redundant database queries.
	// Data is loaded asynchronously via HTMX in the template.
	if _, ok := c.Locals("user").(database.User); !ok {
		return c.Redirect("/", 302)
	}
	return RenderPage(c, "schedules", "pages/schedules", fiber.Map{})
}

// ArtistsPage renders the artists page
func (h *ArtistsHandler) ArtistsPage(c *fiber.Ctx) error {
	// Bolt Optimization: Removed redundant database queries.
	// Data is loaded asynchronously via HTMX in the template.
	if _, ok := c.Locals("user").(database.User); !ok {
		return c.Redirect("/", 302)
	}
	return RenderPage(c, "artists", "pages/artists", fiber.Map{})
}

// JobsPage renders the jobs page
func (h *StatsHandler) JobsPage(c *fiber.Ctx) error {
	// Bolt Optimization: Removed redundant database queries.
	// Data is loaded asynchronously via HTMX in the template.
	if _, ok := c.Locals("user").(database.User); !ok {
		return c.Redirect("/", 302)
	}
	return RenderPage(c, "jobs", "pages/jobs", fiber.Map{})
}
