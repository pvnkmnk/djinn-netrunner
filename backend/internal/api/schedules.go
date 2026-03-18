package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

type SchedulesHandler struct {
	db *gorm.DB
}

func NewSchedulesHandler(db *gorm.DB) *SchedulesHandler {
	return &SchedulesHandler{db: db}
}

// GET /api/schedules - List all schedules
func (h *SchedulesHandler) List(c *fiber.Ctx) error {
	var schedules []database.Schedule
	if err := h.db.Preload("Watchlist").Find(&schedules).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(schedules)
}

// POST /api/schedules - Create new schedule
func (h *SchedulesHandler) Create(c *fiber.Ctx) error {
	var payload struct {
		WatchlistID string `json:"watchlist_id"`
		CronExpr    string `json:"cron_expr"`
		Timezone    string `json:"timezone"`
		Enabled     *bool  `json:"enabled"`
	}

	if err := c.BodyParser(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	// Validate cron
	if _, err := cron.ParseStandard(payload.CronExpr); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid cron expression"})
	}

	watchlistID, err := uuid.Parse(payload.WatchlistID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid watchlist_id"})
	}

	tz := payload.Timezone
	if tz == "" {
		tz = "UTC"
	}

	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	schedule := database.Schedule{
		WatchlistID: watchlistID,
		CronExpr:    payload.CronExpr,
		Timezone:    tz,
		Enabled:     enabled,
	}

	if err := h.db.Create(&schedule).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(201).JSON(schedule)
}

// DELETE /api/schedules/:id - Delete schedule
func (h *SchedulesHandler) Delete(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
	}

	if err := h.db.Delete(&database.Schedule{}, "id = ?", id).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"status": "deleted"})
}

// PATCH /api/schedules/:id - Update schedule
func (h *SchedulesHandler) Update(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
	}

	var payload struct {
		CronExpr *string `json:"cron_expr"`
		Timezone *string `json:"timezone"`
		Enabled  *bool   `json:"enabled"`
	}

	if err := c.BodyParser(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	updates := make(map[string]interface{})

	if payload.CronExpr != nil {
		if _, err := cron.ParseStandard(*payload.CronExpr); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid cron expression"})
		}
		updates["cron_expr"] = *payload.CronExpr
	}

	if payload.Timezone != nil {
		updates["timezone"] = *payload.Timezone
	}

	if payload.Enabled != nil {
		updates["enabled"] = *payload.Enabled
	}

	if err := h.db.Model(&database.Schedule{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"status": "updated"})
}

// PATCH /api/schedules/:id/toggle - Toggle schedule enabled/disabled
func (h *SchedulesHandler) Toggle(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
	}

	var sched database.Schedule
	if err := h.db.Preload("Watchlist").First(&sched, "id = ?", id).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "not found"})
	}

	// Toggle the enabled state
	sched.Enabled = !sched.Enabled
	if err := h.db.Save(&sched).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Render("partials/schedules", fiber.Map{
		"schedules": []database.Schedule{sched},
	})
}

// GetForm returns the schedule form for add/edit
func (h *SchedulesHandler) GetForm(c *fiber.Ctx) error {
	id := c.Query("id")

	var sched database.Schedule
	var watchlists []database.Watchlist
	h.db.Find(&watchlists)

	if id != "" {
		parsedID, err := uuid.Parse(id)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid ID"})
		}
		if err := h.db.Preload("Watchlist").First(&sched, "id = ?", parsedID).Error; err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "not found"})
		}
	}

	return c.Render("partials/schedule-form", fiber.Map{
		"ID":          sched.ID,
		"WatchlistID": sched.WatchlistID,
		"CronExpr":    sched.CronExpr,
		"Enabled":     sched.Enabled,
		"watchlists":  watchlists,
	})
}
