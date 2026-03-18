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
	lists, _ := h.service.GetWatchlists()
	var profiles []database.QualityProfile
	h.db.Order("name").Find(&profiles)
	return RenderPage(c, "watchlists", "pages/watchlists", fiber.Map{
		"watchlists": lists,
		"profiles":   profiles,
	})
}

// LibrariesPage renders the libraries page
func (h *LibraryHandler) LibrariesPage(c *fiber.Ctx) error {
	var libs []database.Library
	h.db.Find(&libs)
	return RenderPage(c, "libraries", "pages/libraries", fiber.Map{"libraries": libs})
}

// ProfilesPage renders the profiles page
func (h *ProfileHandler) ProfilesPage(c *fiber.Ctx) error {
	var profiles []database.QualityProfile
	h.db.Find(&profiles)
	return RenderPage(c, "profiles", "pages/profiles", fiber.Map{"profiles": profiles})
}

// SchedulesPage renders the schedules page
func (h *SchedulesHandler) SchedulesPage(c *fiber.Ctx) error {
	var scheds []database.Schedule
	h.db.Preload("Watchlist").Find(&scheds)
	var watchlists []database.Watchlist
	h.db.Find(&watchlists)
	return RenderPage(c, "schedules", "pages/schedules", fiber.Map{
		"schedules":  scheds,
		"watchlists": watchlists,
	})
}

// ArtistsPage renders the artists page
func (h *ArtistsHandler) ArtistsPage(c *fiber.Ctx) error {
	var artists []database.MonitoredArtist
	h.db.Find(&artists)
	return RenderPage(c, "artists", "pages/artists", fiber.Map{"artists": artists})
}

// JobsPage renders the jobs page
func (h *StatsHandler) JobsPage(c *fiber.Ctx) error {
	var jobs []database.Job
	h.db.Order("requested_at DESC").Limit(50).Find(&jobs)
	return RenderPage(c, "jobs", "pages/jobs", fiber.Map{"jobs": jobs})
}
