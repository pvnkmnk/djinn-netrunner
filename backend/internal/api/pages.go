package api

import (
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
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
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Redirect("/", 302)
	}

	var lists []database.Watchlist
	// Bolt Optimization: Removed redundant QualityProfile preload.
	// Also use targeted column selection to reduce memory allocation.
	// BOLA: Filter by owner_user_id for non-admin users.
	query := h.db.Order("name").Select("id", "name", "source_type", "source_uri", "enabled")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.Find(&lists).Error; err != nil {
		log.Printf("Error getting watchlists: %v", err)
	}

	var profiles []database.QualityProfile
	pQuery := h.db.Order("name").Select("id", "name")
	if user.Role != "admin" {
		pQuery = pQuery.Where("owner_user_id = ? OR is_default = ?", user.ID, true)
	}
	if err := pQuery.Find(&profiles).Error; err != nil {
		log.Printf("Error getting profiles: %v", err)
	}

	return RenderPage(c, "watchlists", "pages/watchlists", fiber.Map{
		"watchlists": lists,
		"profiles":   profiles,
	})
}

// LibrariesPage renders the libraries page
func (h *LibraryHandler) LibrariesPage(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Redirect("/", 302)
	}

	var libs []database.Library
	// Bolt Optimization: Use targeted column selection to reduce memory allocation.
	// BOLA: Filter by owner_user_id for non-admin users.
	query := h.db.Order("name").Select("id", "name", "path")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.Find(&libs).Error; err != nil {
		log.Printf("Error getting libraries: %v", err)
	}
	return RenderPage(c, "libraries", "pages/libraries", fiber.Map{"libraries": libs})
}

// ProfilesPage renders the profiles page
func (h *ProfileHandler) ProfilesPage(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Redirect("/", 302)
	}

	var profiles []database.QualityProfile
	// Bolt Optimization: Use targeted column selection to reduce memory allocation.
	// BOLA: Filter by owner_user_id or is_default for non-admin users.
	query := h.db.Order("name").Select("id", "name", "description", "prefer_lossless", "allowed_formats", "min_bitrate", "cover_art_sources", "is_default")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ? OR is_default = ?", user.ID, true)
	}
	if err := query.Find(&profiles).Error; err != nil {
		log.Printf("Error getting profiles: %v", err)
	}
	return RenderPage(c, "profiles", "pages/profiles", fiber.Map{"profiles": profiles})
}

// SchedulesPage renders the schedules page
func (h *SchedulesHandler) SchedulesPage(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Redirect("/", 302)
	}

	var scheds []database.Schedule
	// Bolt Optimization: Preload only necessary fields from the Watchlist table.
	// Also use targeted column selection for schedules to reduce memory allocation.
	// BOLA: Filter by owner_user_id through join for non-admin users.
	query := h.db.Preload("Watchlist", func(db *gorm.DB) *gorm.DB {
		return db.Select("id, name")
	}).Order("created_at desc").Select("id", "watchlist_id", "cron_expr", "next_run_at", "enabled")

	if user.Role != "admin" {
		query = query.Joins("JOIN watchlists ON watchlists.id = schedules.watchlist_id").
			Where("watchlists.owner_user_id = ?", user.ID)
	}

	if err := query.Find(&scheds).Error; err != nil {
		log.Printf("Error getting schedules: %v", err)
	}

	var watchlists []database.Watchlist
	wQuery := h.db.Order("name")
	if user.Role != "admin" {
		wQuery = wQuery.Where("owner_user_id = ?", user.ID)
	}
	if err := wQuery.Find(&watchlists).Error; err != nil {
		log.Printf("Error getting watchlists: %v", err)
	}

	return RenderPage(c, "schedules", "pages/schedules", fiber.Map{
		"schedules":  scheds,
		"watchlists": watchlists,
	})
}

// ArtistsPage renders the artists page
func (h *ArtistsHandler) ArtistsPage(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Redirect("/", 302)
	}

	var artists []database.MonitoredArtist
	// Bolt Optimization: Use targeted column selection to reduce memory allocation.
	// BOLA: Filter by owner_user_id for non-admin users.
	query := h.db.Order("name").Select("id", "name", "monitored", "music_brainz_id", "acquired_releases", "total_releases", "last_scan_date")
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
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Redirect("/", 302)
	}

	var jobs []database.Job
	// Bolt Optimization: Use .Omit("params", "error_detail") to exclude large JSON/text blobs.
	// BOLA: Filter by owner_user_id for non-admin users.
	query := h.db.Order("requested_at DESC").Limit(50).Omit("params", "error_detail")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.Find(&jobs).Error; err != nil {
		log.Printf("Error getting jobs: %v", err)
	}
	return RenderPage(c, "jobs", "pages/jobs", fiber.Map{"jobs": jobs})
}
