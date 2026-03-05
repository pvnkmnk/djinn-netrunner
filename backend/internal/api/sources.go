package api

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

type SourceHandler struct {
	db *gorm.DB
}

func NewSourceHandler(db *gorm.DB) *SourceHandler {
	return &SourceHandler{db: db}
}

// ListSources returns all sources for the current user
func (h *SourceHandler) ListSources(c *fiber.Ctx) error {
	user := c.Locals("user").(database.User)
	var sources []database.Source

	query := h.db.Order("display_name")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.Find(&sources).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(sources)
}

// CreateSource creates a new music source
func (h *SourceHandler) CreateSource(c *fiber.Ctx) error {
	user := c.Locals("user").(database.User)
	var source database.Source

	if err := c.BodyParser(&source); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	source.OwnerUserID = &user.ID
	if err := h.db.Create(&source).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(201).JSON(source)
}

// UpdateSource updates an existing source
func (h *SourceHandler) UpdateSource(c *fiber.Ctx) error {
	user := c.Locals("user").(database.User)
	id, _ := strconv.ParseUint(c.Params("id"), 10, 64)

	var source database.Source
	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.First(&source).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "source not found"})
	}

	if err := c.BodyParser(&source); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	h.db.Save(&source)
	return c.JSON(source)
}

// DeleteSource removes a source
func (h *SourceHandler) DeleteSource(c *fiber.Ctx) error {
	user := c.Locals("user").(database.User)
	id, _ := strconv.ParseUint(c.Params("id"), 10, 64)

	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.Delete(&database.Source{}).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(204).Send(nil)
}

// Schedules

func (h *SourceHandler) ListSchedules(c *fiber.Ctx) error {
	user := c.Locals("user").(database.User)
	sourceID, _ := strconv.ParseUint(c.Params("source_id"), 10, 64)

	// Verify source ownership
	var source database.Source
	sourceQuery := h.db.Where("id = ?", sourceID)
	if user.Role != "admin" {
		sourceQuery = sourceQuery.Where("owner_user_id = ?", user.ID)
	}
	if err := sourceQuery.First(&source).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "source not found"})
	}

	var schedules []database.Schedule
	if err := h.db.Where("source_id = ?", sourceID).Find(&schedules).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(schedules)
}

func (h *SourceHandler) CreateSchedule(c *fiber.Ctx) error {
	user := c.Locals("user").(database.User)
	var schedule database.Schedule
	if err := c.BodyParser(&schedule); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	// Verify source ownership
	var source database.Source
	sourceQuery := h.db.Where("id = ?", schedule.SourceID)
	if user.Role != "admin" {
		sourceQuery = sourceQuery.Where("owner_user_id = ?", user.ID)
	}
	if err := sourceQuery.First(&source).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "source not found"})
	}

	if err := h.db.Create(&schedule).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(201).JSON(schedule)
}
