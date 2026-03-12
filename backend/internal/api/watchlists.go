package api

import (
	"github.com/gofiber/fiber/v2"
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
	var watchlist database.Watchlist

	if err := c.BodyParser(&watchlist); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	watchlist.OwnerUserID = &user.ID
	if err := h.db.Create(&watchlist).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(201).JSON(watchlist)
}

// UpdateWatchlist updates an existing watchlist
func (h *WatchlistHandler) UpdateWatchlist(c *fiber.Ctx) error {
	user := c.Locals("user").(database.User)
	id := c.Params("id")

	var watchlist database.Watchlist
	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.First(&watchlist).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "watchlist not found"})
	}

	if err := c.BodyParser(&watchlist); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	h.db.Save(&watchlist)
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
