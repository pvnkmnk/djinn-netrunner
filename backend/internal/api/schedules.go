package api

import (
	"log"
	"strconv"

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
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var schedules []database.Schedule
	query := h.db.Preload("Watchlist")
	if user.Role != "admin" {
		query = query.Joins("JOIN watchlists ON watchlists.id = schedules.watchlist_id").Where("watchlists.owner_user_id = ?", user.ID)
	}
	if err := query.Find(&schedules).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(schedules)
}

// POST /api/schedules - Create new schedule
func (h *SchedulesHandler) Create(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var payload struct {
		WatchlistID string `json:"watchlist_id"`
		CronExpr    string `json:"cron_expr"`
		Timezone    string `json:"timezone"`
		Enabled     *bool  `json:"enabled"`
	}

	if err := c.BodyParser(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	watchlistID, err := uuid.Parse(payload.WatchlistID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid watchlist_id"})
	}

	// ✅ SECURITY: Verify watchlist ownership
	if user.Role != "admin" {
		var count int64
		h.db.Model(&database.Watchlist{}).Where("id = ? AND owner_user_id = ?", watchlistID, user.ID).Count(&count)
		if count == 0 {
			return c.Status(403).JSON(fiber.Map{"error": "forbidden: you do not own this watchlist"})
		}
	}

	// Validate cron
	if _, err := cron.ParseStandard(payload.CronExpr); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid cron expression"})
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
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id format"})
	}

	// Verify ownership before deleting
	if user.Role != "admin" {
		var count int64
		h.db.Model(&database.Schedule{}).
			Joins("JOIN watchlists ON watchlists.id = schedules.watchlist_id").
			Where("schedules.id = ? AND watchlists.owner_user_id = ?", id, user.ID).
			Count(&count)
		if count == 0 {
			return c.Status(403).JSON(fiber.Map{"error": "forbidden"})
		}
	}

	if err := h.db.Delete(&database.Schedule{}, "id = ?", id).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"status": "deleted"})
}

// PATCH /api/schedules/:id - Update schedule
func (h *SchedulesHandler) Update(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id format"})
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

	// Verify ownership before updating
	if user.Role != "admin" {
		var count int64
		h.db.Model(&database.Schedule{}).
			Joins("JOIN watchlists ON watchlists.id = schedules.watchlist_id").
			Where("schedules.id = ? AND watchlists.owner_user_id = ?", id, user.ID).
			Count(&count)
		if count == 0 {
			return c.Status(403).JSON(fiber.Map{"error": "forbidden"})
		}
	}

	if err := h.db.Model(&database.Schedule{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"status": "updated"})
}

// PATCH /api/schedules/:id/toggle - Toggle schedule enabled/disabled
func (h *SchedulesHandler) Toggle(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id format"})
	}

	var sched database.Schedule
	query := h.db.Preload("Watchlist")
	if user.Role != "admin" {
		query = query.Joins("JOIN watchlists ON watchlists.id = schedules.watchlist_id").Where("watchlists.owner_user_id = ?", user.ID)
	}

	if err := query.First(&sched, "schedules.id = ?", id).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "not found"})
	}

	// Toggle the enabled state
	sched.Enabled = !sched.Enabled
	if err := h.db.Save(&sched).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	// sched still has preloaded Watchlist from initial fetch, no need to reload

	return c.Render("partials/schedule-card", fiber.Map{
		"Schedule": sched,
	})
}

// GetForm returns the schedule form for add/edit
func (h *SchedulesHandler) GetForm(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	isHtmx := c.Get("Htmx-Request") == "true"

	if !ok {
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect("/", 302)
	}

	id := c.Query("id")

	var sched database.Schedule
	var watchlists []database.Watchlist

	wQuery := h.db.Order("name")
	if user.Role != "admin" {
		wQuery = wQuery.Where("owner_user_id = ?", user.ID)
	}
	if err := wQuery.Find(&watchlists).Error; err != nil {
		log.Printf("Error fetching watchlists for schedule form: %v", err)
		return c.SendString("<div class=\"error\">Error loading form.</div>")
	}

	if id != "" {
		sQuery := h.db.Preload("Watchlist")
		if user.Role != "admin" {
			sQuery = sQuery.Joins("JOIN watchlists ON watchlists.id = schedules.watchlist_id").Where("watchlists.owner_user_id = ?", user.ID)
		}
		if err := sQuery.First(&sched, "schedules.id = ?", id).Error; err != nil {
			return c.SendString("<div class=\"error\">Schedule not found.</div>")
		}
	}

	c.Set("HX-Trigger", "openModal")
	return c.Render("partials/schedule-form", fiber.Map{
		"ID":          sched.ID,
		"WatchlistID": sched.WatchlistID,
		"CronExpr":    sched.CronExpr,
		"Enabled":     sched.Enabled,
		"watchlists":  watchlists,
	})
}

// RenderSchedulesPartial returns schedules HTML for HTMX
func (h *SchedulesHandler) RenderSchedulesPartial(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	isHtmx := c.Get("Htmx-Request") == "true"

	if !ok {
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect("/", 302)
	}

	var schedules []database.Schedule
	query := h.db.Preload("Watchlist").Order("schedules.created_at desc")
	if user.Role != "admin" {
		query = query.Joins("JOIN watchlists ON watchlists.id = schedules.watchlist_id").Where("watchlists.owner_user_id = ?", user.ID)
	}

	if err := query.Find(&schedules).Error; err != nil {
		return c.SendString("<div class=\"error\">Error loading schedules.</div>")
	}

	return c.Render("partials/schedules", fiber.Map{
		"schedules": schedules,
	})
}
