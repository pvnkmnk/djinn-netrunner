package api

import (
	"log"

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
	// Bolt Optimization: AuthMiddleware already populates "user" in context
	_, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Redirect("/", 302)
	}

	lists, err := h.service.GetWatchlists()
	if err != nil {
		log.Printf("Error getting watchlists: %v", err)
		lists = []database.Watchlist{}
	}
	var profiles []database.QualityProfile
	if err := h.db.Order("name").Find(&profiles).Error; err != nil {
		log.Printf("Error getting profiles: %v", err)
	}
	return RenderPage(c, "watchlists", "pages/watchlists", fiber.Map{
		"watchlists": lists,
		"profiles":   profiles,
	})
}

// LibrariesPage renders the libraries page
func (h *LibraryHandler) LibrariesPage(c *fiber.Ctx) error {
	// Bolt Optimization: AuthMiddleware already populates "user" in context
	_, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Redirect("/", 302)
	}

	var libs []database.Library
	if err := h.db.Order("name").Find(&libs).Error; err != nil {
		log.Printf("Error getting libraries: %v", err)
	}
	return RenderPage(c, "libraries", "pages/libraries", fiber.Map{"libraries": libs})
}

// ProfilesPage renders the profiles page
func (h *ProfileHandler) ProfilesPage(c *fiber.Ctx) error {
	// Bolt Optimization: AuthMiddleware already populates "user" in context
	_, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Redirect("/", 302)
	}

	var profiles []database.QualityProfile
	if err := h.db.Order("name").Find(&profiles).Error; err != nil {
		log.Printf("Error getting profiles: %v", err)
	}
	return RenderPage(c, "profiles", "pages/profiles", fiber.Map{"profiles": profiles})
}

// SchedulesPage renders the schedules page
func (h *SchedulesHandler) SchedulesPage(c *fiber.Ctx) error {
	// Bolt Optimization: AuthMiddleware already populates "user" in context
	_, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Redirect("/", 302)
	}

	var scheds []database.Schedule
	if err := h.db.Preload("Watchlist").Order("created_at desc").Find(&scheds).Error; err != nil {
		log.Printf("Error getting schedules: %v", err)
	}
	var watchlists []database.Watchlist
	if err := h.db.Order("name").Find(&watchlists).Error; err != nil {
		log.Printf("Error getting watchlists: %v", err)
	}
	return RenderPage(c, "schedules", "pages/schedules", fiber.Map{
		"schedules":  scheds,
		"watchlists": watchlists,
	})
}

// ArtistsPage renders the artists page
func (h *ArtistsHandler) ArtistsPage(c *fiber.Ctx) error {
	// Bolt Optimization: AuthMiddleware already populates "user" in context
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Redirect("/", 302)
	}

	var artists []database.MonitoredArtist
	query := h.db.Order("name")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.Find(&artists).Error; err != nil {
		log.Printf("Error getting artists: %v", err)
	}
	return RenderPage(c, "artists", "pages/artists", fiber.Map{"artists": artists})
}

// JobsPage renders the jobs page
func (h *StatsHandler) JobsPage(c *fiber.Ctx) error {
	// Bolt Optimization: AuthMiddleware already populates "user" in context
	_, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Redirect("/", 302)
	}

	var jobs []database.Job
	if err := h.db.Order("requested_at DESC").Limit(50).Find(&jobs).Error; err != nil {
		log.Printf("Error getting jobs: %v", err)
	}
	return RenderPage(c, "jobs", "pages/jobs", fiber.Map{"jobs": jobs})
}
