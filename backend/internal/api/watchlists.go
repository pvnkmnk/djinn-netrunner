package api

import (
	"fmt"
	"html"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"gorm.io/gorm"
)

type WatchlistHandler struct {
	db      *gorm.DB
	service *services.WatchlistService
}

func NewWatchlistHandler(db *gorm.DB, service *services.WatchlistService) *WatchlistHandler {
	return &WatchlistHandler{
		db:      db,
		service: service,
	}
}

// ListWatchlists returns all watchlists for the current user
func (h *WatchlistHandler) ListWatchlists(c *fiber.Ctx) error {
	user, hasAuth := currentUserFromLocals(c)
	if !hasAuth {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var watchlists []database.Watchlist

	query := h.db.Order("name").Preload("QualityProfile")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.Find(&watchlists).Error; err != nil {
		return internalServerError(c, err)
	}

	return c.JSON(watchlists)
}

// CreateWatchlist creates a new automated watchlist
func (h *WatchlistHandler) CreateWatchlist(c *fiber.Ctx) error {
	user, hasAuth := currentUserFromLocals(c)
	if !hasAuth {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var input struct {
		Name             string    `json:"name"`
		SourceType       string    `json:"source_type"`
		SourceURI        string    `json:"source_uri"`
		QualityProfileID uuid.UUID `json:"quality_profile_id"`
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	watchlist, err := h.service.CreateWatchlist(input.Name, input.SourceType, input.SourceURI, input.QualityProfileID, &user.ID)
	if err != nil {
		slog.Error("Failed to create watchlist", "error", err)
		return c.Status(400).JSON(fiber.Map{"error": "failed to create watchlist"})
	}

	if isHTMXRequest(c) {
		return h.RenderWatchlistsPartial(c)
	}
	return c.Status(201).JSON(watchlist)
}

type UpdateWatchlistInput struct {
	Name             *string    `json:"name"`
	SourceType       *string    `json:"source_type"`
	SourceURI        *string    `json:"source_uri"`
	QualityProfileID *uuid.UUID `json:"quality_profile_id"`
	Enabled          *bool      `json:"enabled"`
}

// UpdateWatchlist updates an existing watchlist
func (h *WatchlistHandler) UpdateWatchlist(c *fiber.Ctx) error {
	user, hasAuth := currentUserFromLocals(c)
	if !hasAuth {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
	}

	watchlist, err := h.service.GetWatchlist(id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "watchlist not found"})
	}

	if user.Role != "admin" && (watchlist.OwnerUserID == nil || *watchlist.OwnerUserID != user.ID) {
		return c.Status(403).JSON(fiber.Map{"error": "forbidden"})
	}

	var input UpdateWatchlistInput
	if err := c.BodyParser(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	if input.Name != nil {
		if *input.Name == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name cannot be empty"})
		}
		watchlist.Name = *input.Name
	}
	if input.SourceType != nil {
		watchlist.SourceType = *input.SourceType
	}
	if input.SourceURI != nil {
		watchlist.SourceURI = *input.SourceURI
	}
	if input.QualityProfileID != nil {
		watchlist.QualityProfileID = *input.QualityProfileID
	}
	if input.Enabled != nil {
		watchlist.Enabled = *input.Enabled
	}

	// Validate again before saving
	if err := h.service.ValidateWatchlist(watchlist); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	if err := h.db.Save(watchlist).Error; err != nil {
		return internalServerError(c, err)
	}

	if isHTMXRequest(c) {
		return h.RenderWatchlistsPartial(c)
	}
	return c.JSON(watchlist)
}

// DeleteWatchlist removes a watchlist
func (h *WatchlistHandler) DeleteWatchlist(c *fiber.Ctx) error {
	user, hasAuth := currentUserFromLocals(c)
	if !hasAuth {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id := c.Params("id")

	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.Delete(&database.Watchlist{}).Error; err != nil {
		return internalServerError(c, err)
	}

	if isHTMXRequest(c) {
		return h.RenderWatchlistsPartial(c)
	}
	return c.Status(204).Send(nil)
}

// Profile endpoints

func (h *WatchlistHandler) ListProfiles(c *fiber.Ctx) error {
	user, hasAuth := currentUserFromLocals(c)
	if !hasAuth {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var profiles []database.QualityProfile
	// Bolt Optimization: Select only necessary columns for the dropdown.
	query := h.db.Select("id, name").Order("name")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.Find(&profiles).Error; err != nil {
		return internalServerError(c, err)
	}
	return c.JSON(profiles)
}

// ToggleWatchlist toggles enabled state
func (h *WatchlistHandler) ToggleWatchlist(c *fiber.Ctx) error {
	user, hasAuth := currentUserFromLocals(c)
	if !hasAuth {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid ID"})
	}

	var wl database.Watchlist
	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.First(&wl).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "not found"})
	}

	wl.Enabled = !wl.Enabled
	if err := h.db.Save(&wl).Error; err != nil {
		slog.Error("Failed to toggle watchlist enabled state", "error", err, "watchlistID", wl.ID)
		return c.Status(500).SendString("<div class=\"error\">Failed to update watchlist.</div>")
	}

	return c.Render("partials/watchlist-card", fiber.Map{"watchlist": wl})
}

// GetForm returns the watchlist form for add/edit
func (h *WatchlistHandler) GetForm(c *fiber.Ctx) error {
	user, hasAuth := currentUserFromLocals(c)

	isHtmx := c.Get("Htmx-Request") == "true"

	if !hasAuth {
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect("/", 302)
	}

	id := c.Query("id")

	var wl database.Watchlist
	if id != "" {
		uuid, err := uuid.Parse(id)
		if err != nil {
			return c.SendString("<div class=\"error\">Invalid ID.</div>")
		}
		// BOLA: Verify ownership before fetching
		query := h.db.Where("id = ?", uuid)
		if user.Role != "admin" {
			query = query.Where("owner_user_id = ?", user.ID)
		}
		if err := query.First(&wl).Error; err != nil {
			return c.SendString("<div class=\"error\">Watchlist not found.</div>")
		}
	}

	var profiles []database.QualityProfile
	query := h.db.Order("name")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.Find(&profiles).Error; err != nil {
		slog.Error("Error fetching profiles for watchlist form", "error", err)
		return c.SendString("<div class=\"error\">Error loading form.</div>")
	}

	c.Set("HX-Trigger", "openModal")
	return c.Render("partials/watchlist-form", fiber.Map{
		"ID":               wl.ID,
		"Name":             wl.Name,
		"SourceType":       wl.SourceType,
		"SourceURI":        wl.SourceURI,
		"QualityProfileID": wl.QualityProfileID,
		"Enabled":          wl.Enabled,
		"profiles":         profiles,
	})
}

// RenderWatchlistsPartial returns watchlists HTML for HTMX
func (h *WatchlistHandler) RenderWatchlistsPartial(c *fiber.Ctx) error {
	user, hasAuth := currentUserFromLocals(c)

	isHtmx := c.Get("Htmx-Request") == "true"

	if !hasAuth {
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect("/", 302)
	}

	var watchlists []database.Watchlist
	// Bolt Optimization: Select only necessary columns and remove unnecessary Preload to reduce database I/O and memory usage.
	query := h.db.Select("id, name, source_type, source_uri, enabled").Order("name")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.Find(&watchlists).Error; err != nil {
		slog.Error("Error fetching watchlists", "error", err)
		return c.SendString("<div class=\"error\">Error loading watchlists.</div>")
	}

	// Check if user has linked Spotify sp_dc cookie
	var spDcLinked bool
	var spotifyToken database.SpotifyToken
	if err := h.db.Where("user_id = ?", user.ID).First(&spotifyToken).Error; err == nil {
		spDcLinked = spotifyToken.SpDcCookie != ""
	}

	return c.Render("partials/watchlists", fiber.Map{
		"watchlists":  watchlists,
		"spDcLinked":  spDcLinked,
	})
}

// SyncWatchlist triggers a sync job for a watchlist
func (h *WatchlistHandler) SyncWatchlist(c *fiber.Ctx) error {
	user, hasAuth := currentUserFromLocals(c)
	if !hasAuth {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid ID"})
	}

	var wl database.Watchlist
	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.First(&wl).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "watchlist not found"})
		}
		return internalServerError(c, err)
	}

	// Create sync job
	job := database.Job{
		Type:        "watchlist_sync",
		State:       "queued",
		ScopeType:   "watchlist",
		ScopeID:     wl.ID.String(),
		RequestedAt: time.Now(),
		CreatedBy:   "ui",
		OwnerUserID: &user.ID,
	}

	if err := h.db.Create(&job).Error; err != nil {
		slog.Error("Failed to create watchlist sync job", "error", err, "watchlistID", wl.ID)
		return c.Status(500).Type("html").SendString(`<div class="console-entry error">Failed to trigger sync for watchlist ` + html.EscapeString(wl.Name) + `.</div>`)
	}

	return c.Type("html").SendString(`<div class="console-entry">Sync triggered for watchlist ` + html.EscapeString(wl.Name) + `... (job #` + fmt.Sprintf("%d", job.ID) + `)</div>`)
}
