package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// AcquireHandler handles manual acquisition requests from the web UI.
type AcquireHandler struct {
	db *gorm.DB
}

// NewAcquireHandler creates a new AcquireHandler.
func NewAcquireHandler(db *gorm.DB) *AcquireHandler {
	return &AcquireHandler{db: db}
}

// Create handles POST /api/acquire — enqueues a new acquisition job.
func (h *AcquireHandler) Create(c *fiber.Ctx) error {
	user, ok := currentUserFromLocals(c)
	if !ok {
		if c.Get("HX-Request") == "true" {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var payload struct {
		Artist           string `json:"artist" form:"artist"`
		Album            string `json:"album" form:"album"`
		Title            string `json:"title" form:"title"`
		QualityProfileID string `json:"quality_profile_id" form:"quality_profile_id"`
	}

	if err := c.BodyParser(&payload); err != nil {
		if c.Get("HX-Request") == "true" {
			return h.renderFormWithError(c, "", "", "", "Invalid request.")
		}
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	payload.Artist = strings.TrimSpace(payload.Artist)
	payload.Album = strings.TrimSpace(payload.Album)
	payload.Title = strings.TrimSpace(payload.Title)

	if payload.Artist == "" {
		if c.Get("HX-Request") == "true" {
			return h.renderFormWithError(c, "", payload.Album, payload.Title, "Artist is required.")
		}
		return c.Status(400).JSON(fiber.Map{"error": "artist is required"})
	}

	if payload.Album == "" && payload.Title == "" {
		if c.Get("HX-Request") == "true" {
			return h.renderFormWithError(c, payload.Artist, "", "", "Album or track title is required.")
		}
		return c.Status(400).JSON(fiber.Map{"error": "album or title is required"})
	}

	// Build the normalized search query
	var query string
	switch {
	case payload.Title != "":
		query = fmt.Sprintf("%s %s", payload.Artist, payload.Title)
	case payload.Album != "":
		query = fmt.Sprintf("%s %s", payload.Artist, payload.Album)
	}

	// Build optional params JSON for quality profile
	var params json.RawMessage
	if payload.QualityProfileID != "" {
		raw, _ := json.Marshal(map[string]string{"quality_profile_id": payload.QualityProfileID})
		params = raw
	}

	job := database.Job{
		Type:        "acquisition",
		State:       "queued",
		RequestedAt: time.Now(),
		OwnerUserID: &user.ID,
		CreatedBy:   "web_ui",
		Params:      params,
	}

	err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&job).Error; err != nil {
			return err
		}
		item := database.JobItem{
			JobID:           job.ID,
			Artist:          payload.Artist,
			Album:           payload.Album,
			TrackTitle:      payload.Title,
			NormalizedQuery: query,
			Status:          "queued",
			OwnerUserID:     &user.ID,
		}
		return tx.Create(&item).Error
	})
	if err != nil {
		slog.Error("Failed to create acquisition job", "error", err)
		return internalServerError(c, err)
	}

	slog.Info("Acquisition job created",
		"job_id", job.ID,
		"artist", payload.Artist,
		"album", payload.Album,
		"title", payload.Title,
		"user_id", user.ID,
	)

	// If this is an HTMX request, return the updated jobs list
	if c.Get("HX-Request") == "true" {
		c.Set("HX-Trigger", "closeModal")
		return h.renderJobsList(c, user)
	}

	return c.Status(201).JSON(fiber.Map{
		"status": "queued",
		"job_id": job.ID,
	})
}

// GetForm returns the acquisition form partial for HTMX modal.
func (h *AcquireHandler) GetForm(c *fiber.Ctx) error {
	user, ok := currentUserFromLocals(c)
	if !ok {
		if c.Get("HX-Request") == "true" {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect("/", 302)
	}

	var profiles []database.QualityProfile
	query := h.db.Order("name")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ? OR owner_user_id IS NULL OR is_default = ?", user.ID, true)
	}
	if err := query.Find(&profiles).Error; err != nil {
		slog.Error("Error fetching profiles for acquire form", "error", err)
		return c.SendString("<div class=\"error\">Error loading form.</div>")
	}

	c.Set("HX-Trigger", "openModal")
	return c.Render("partials/acquire-form", fiber.Map{
		"profiles": profiles,
	})
}

func (h *AcquireHandler) renderFormWithError(c *fiber.Ctx, artist, album, title, errMsg string) error {
	user, _ := currentUserFromLocals(c)
	var profiles []database.QualityProfile
	query := h.db.Order("name")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ? OR owner_user_id IS NULL OR is_default = ?", user.ID, true)
	}
	_ = query.Find(&profiles).Error

	c.Set("HX-Retarget", "#modal-container")
	c.Set("HX-Reswap", "innerHTML")
	return c.Render("partials/acquire-form", fiber.Map{
		"profiles": profiles,
		"error":    errMsg,
		"artist":   artist,
		"album":    album,
		"title":    title,
	})
}

func (h *AcquireHandler) renderJobsList(c *fiber.Ctx, user database.User) error {
	var jobs []database.Job
	query := h.db.Select("id, job_type, state, requested_at, created_by, error_detail, attempt, max_attempts").
		Order("requested_at DESC").Limit(50)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.Find(&jobs).Error; err != nil {
		slog.Error("Error fetching jobs", "error", err)
		return c.SendString("<div class=\"error\">Error loading jobs.</div>")
	}
	return c.Render("partials/jobs", fiber.Map{"jobs": jobs})
}
