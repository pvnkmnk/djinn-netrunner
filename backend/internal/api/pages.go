package api

import (
	"github.com/gofiber/fiber/v2"
)

// PageData contains common page data
type PageData struct {
	Page string
}

// RenderPage renders a page with common layout
func RenderPage(c *fiber.Ctx, page string, template string, data fiber.Map) error {
	base := fiber.Map{"Page": page}
	// SECURITY: Expose CSRF token to templates for HTMX state-changing requests
	if csrf := c.Locals("csrf"); csrf != nil {
		base["CSRFToken"] = csrf
	}
	for k, v := range data {
		base[k] = v
	}
	return c.Render(template, base)
}

// WatchlistsPage renders the watchlists page shell
func (h *WatchlistHandler) WatchlistsPage(c *fiber.Ctx) error {
	if _, ok, err := requirePageUser(c); !ok {
		return err
	}

	// Bolt Optimization: Removed redundant database queries for watchlists and profiles.
	// Data is now fetched asynchronously via HTMX partials.
	return RenderPage(c, "watchlists", "pages/watchlists", fiber.Map{})
}

// LibrariesPage renders the libraries page shell
func (h *LibraryHandler) LibrariesPage(c *fiber.Ctx) error {
	if _, ok, err := requirePageUser(c); !ok {
		return err
	}

	// Bolt Optimization: Removed redundant database query for libraries.
	// Data is now fetched asynchronously via HTMX partials.
	return RenderPage(c, "libraries", "pages/libraries", fiber.Map{})
}

// ProfilesPage renders the profiles page shell
func (h *ProfileHandler) ProfilesPage(c *fiber.Ctx) error {
	if _, ok, err := requirePageUser(c); !ok {
		return err
	}

	// Bolt Optimization: Removed redundant database query for profiles.
	// Data is now fetched asynchronously via HTMX partials.
	return RenderPage(c, "profiles", "pages/profiles", fiber.Map{})
}

// SchedulesPage renders the schedules page shell
func (h *SchedulesHandler) SchedulesPage(c *fiber.Ctx) error {
	if _, ok, err := requirePageUser(c); !ok {
		return err
	}

	// Bolt Optimization: Removed redundant database queries for schedules and watchlists.
	// Data is now fetched asynchronously via HTMX partials.
	return RenderPage(c, "schedules", "pages/schedules", fiber.Map{})
}

// ArtistsPage renders the artists page shell
func (h *ArtistsHandler) ArtistsPage(c *fiber.Ctx) error {
	if _, ok, err := requirePageUser(c); !ok {
		return err
	}

	// Bolt Optimization: Removed redundant database query for artists.
	// Data is now fetched asynchronously via HTMX partials.
	return RenderPage(c, "artists", "pages/artists", fiber.Map{})
}

// JobsPage renders the jobs page shell
func (h *StatsHandler) JobsPage(c *fiber.Ctx) error {
	user, ok, err := requirePageUser(c)
	if !ok {
		return err
	}

	// Bolt Optimization: Removed redundant database query for jobs.
	// Data is now fetched asynchronously via HTMX partials.
	return RenderPage(c, "jobs", "pages/jobs", fiber.Map{"IsAdmin": user.Role == "admin"})
}
