package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

type LibraryHandler struct {
	db *gorm.DB
}

func NewLibraryHandler(db *gorm.DB) *LibraryHandler {
	return &LibraryHandler{db: db}
}

// ListLibraries returns all libraries
func (h *LibraryHandler) ListLibraries(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var libraries []database.Library
	query := h.db.Order("name")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.Find(&libraries).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(libraries)
}

// GetLibrary returns a single library by ID
func (h *LibraryHandler) GetLibrary(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid library ID"})
	}

	var library database.Library
	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.First(&library).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "library not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(library)
}

// CreateLibrary creates a new library
func (h *LibraryHandler) CreateLibrary(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var input struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	if input.Name == "" {
		return c.Status(400).JSON(fiber.Map{"error": "name is required"})
	}
	if input.Path == "" {
		return c.Status(400).JSON(fiber.Map{"error": "path is required"})
	}

	library := database.Library{
		ID:          uuid.New(),
		Name:        input.Name,
		Path:        input.Path,
		OwnerUserID: &user.ID,
	}

	if err := h.db.Create(&library).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(201).JSON(library)
}

// UpdateLibrary updates an existing library
func (h *LibraryHandler) UpdateLibrary(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid library ID"})
	}

	var library database.Library
	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.First(&library).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "library not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	var input struct {
		Name         *string `json:"name"`
		Path         *string `json:"path"`
		MaxSizeBytes *int64  `json:"max_size_bytes"`
		QuotaAlertAt *int    `json:"quota_alert_at"`
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	if input.Name != nil {
		if *input.Name == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name cannot be empty"})
		}
		library.Name = *input.Name
	}
	if input.Path != nil {
		if *input.Path == "" {
			return c.Status(400).JSON(fiber.Map{"error": "path cannot be empty"})
		}
		library.Path = *input.Path
	}
	// Validate QuotaAlertAt: must be between 1 and 100
	if input.QuotaAlertAt != nil && (*input.QuotaAlertAt < 1 || *input.QuotaAlertAt > 100) {
		return c.Status(400).JSON(fiber.Map{"error": "quota_alert_at must be between 1 and 100"})
	}

	// Validate MaxSizeBytes: must be non-negative (0 means "no quota")
	if input.MaxSizeBytes != nil && *input.MaxSizeBytes < 0 {
		return c.Status(400).JSON(fiber.Map{"error": "max_size_bytes must be non-negative"})
	}

	if input.MaxSizeBytes != nil {
		library.MaxSizeBytes = input.MaxSizeBytes
	}
	if input.QuotaAlertAt != nil {
		library.QuotaAlertAt = input.QuotaAlertAt
	}

	if err := h.db.Save(&library).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(library)
}

// DeleteLibrary deletes a library
func (h *LibraryHandler) DeleteLibrary(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid library ID"})
	}

	var library database.Library
	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.First(&library).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "library not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	// Delete associated tracks and library in a transaction
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&database.Track{}, "library_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Delete(&library).Error
	}); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.SendStatus(204)
}

// TriggerScan creates a scan job for the library
func (h *LibraryHandler) TriggerScan(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid library ID"})
	}

	var library database.Library
	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.First(&library).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "library not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	// Create scan job
	job := database.Job{
		Type:        "scan",
		State:       "queued",
		ScopeType:   "library",
		ScopeID:     library.ID.String(),
		RequestedAt: time.Now(),
		CreatedBy:   "api",
		OwnerUserID: &user.ID,
	}

	if err := h.db.Create(&job).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(202).JSON(fiber.Map{
		"message": "scan job queued",
		"job_id":  job.ID,
	})
}

// TriggerEnrich creates an enrich job for the library
func (h *LibraryHandler) TriggerEnrich(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid library ID"})
	}

	var library database.Library
	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.First(&library).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "library not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	// Create enrich job
	job := database.Job{
		Type:        "enrich",
		State:       "queued",
		ScopeType:   "library",
		ScopeID:     library.ID.String(),
		RequestedAt: time.Now(),
		CreatedBy:   "api",
		OwnerUserID: &user.ID,
	}

	if err := h.db.Create(&job).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(202).JSON(fiber.Map{
		"message": "enrich job queued",
		"job_id":  job.ID,
	})
}

// ListTracks returns all tracks for a library
func (h *LibraryHandler) ListTracks(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	libraryID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid library ID"})
	}

	// Verify ownership of the library before listing tracks
	var library database.Library
	query := h.db.Where("id = ?", libraryID)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.First(&library).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "library not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	var tracks []database.Track
	if err := h.db.Where("library_id = ?", libraryID).Order("artist, album, track_num").Find(&tracks).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(tracks)
}

// GetForm returns the library form for add/edit
func (h *LibraryHandler) GetForm(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	isHtmx := c.Get("Htmx-Request") == "true"

	if !ok {
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect("/", 302)
	}

	id := c.Query("id")

	var lib database.Library
	if id != "" {
		uuid, err := uuid.Parse(id)
		if err != nil {
			return c.SendString("<div class=\"error\">Invalid ID.</div>")
		}
		query := h.db.Where("id = ?", uuid)
		if user.Role != "admin" {
			query = query.Where("owner_user_id = ?", user.ID)
		}
		if err := query.First(&lib).Error; err != nil {
			return c.SendString("<div class=\"error\">Library not found.</div>")
		}
	}

	c.Set("HX-Trigger", "openModal")
	return c.Render("partials/library-form", fiber.Map{
		"ID":   lib.ID,
		"Name": lib.Name,
		"Path": lib.Path,
	})
}

// RenderLibrariesPartial returns libraries HTML for HTMX
func (h *LibraryHandler) RenderLibrariesPartial(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	isHtmx := c.Get("Htmx-Request") == "true"

	if !ok {
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect("/", 302)
	}

	var libraries []database.Library
	query := h.db.Order("name")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.Find(&libraries).Error; err != nil {
		return c.SendString("<div class=\"error\">Error loading libraries.</div>")
	}

	return c.Render("partials/libraries", fiber.Map{
		"libraries": libraries,
	})
}
