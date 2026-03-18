package api

import (
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
	user := c.Locals("user").(database.User)
	var watchlists []database.Watchlist

	query := h.db.Order("name").Preload("QualityProfile")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.Find(&watchlists).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(watchlists)
}

// CreateWatchlist creates a new automated watchlist
func (h *WatchlistHandler) CreateWatchlist(c *fiber.Ctx) error {
	user := c.Locals("user").(database.User)
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
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(201).JSON(watchlist)
}

// UpdateWatchlist updates an existing watchlist
func (h *WatchlistHandler) UpdateWatchlist(c *fiber.Ctx) error {
	user := c.Locals("user").(database.User)
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

	if err := c.BodyParser(watchlist); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	// Validate again before saving
	if err := h.service.ValidateWatchlist(watchlist); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	if err := h.db.Save(watchlist).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(watchlist)
}

// DeleteWatchlist removes a watchlist
func (h *WatchlistHandler) DeleteWatchlist(c *fiber.Ctx) error {
	user := c.Locals("user").(database.User)
	id := c.Params("id")

	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.Delete(&database.Watchlist{}).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(204).Send(nil)
}

// Profile endpoints

func (h *WatchlistHandler) ListProfiles(c *fiber.Ctx) error {
	var profiles []database.QualityProfile
	if err := h.db.Order("name").Find(&profiles).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(profiles)
}

// ToggleWatchlist toggles enabled state
func (h *WatchlistHandler) ToggleWatchlist(c *fiber.Ctx) error {
	user := c.Locals("user").(database.User)
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
	h.db.Save(&wl)

	return c.Render("partials/watchlists", fiber.Map{"watchlists": []database.Watchlist{wl}})
}
