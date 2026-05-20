package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// validateLibraryPath validates that a library path is safe to use.
// It ensures the path is absolute, resolves any traversal segments via
// filepath.Clean, and verifies the resolved path exists and is a directory.
func validateLibraryPath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("library path must be absolute")
	}

	// Resolve any . or .. segments to prevent traversal attacks.
	cleanPath := filepath.Clean(path)

	info, err := os.Stat(cleanPath)
	if err != nil {
		return fmt.Errorf("library path does not exist or is inaccessible")
	}
	if !info.IsDir() {
		return fmt.Errorf("library path must be a directory")
	}

	return nil
}

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
	// Bolt Optimization: Select only necessary columns to reduce database I/O and memory usage.
	query := h.db.Select("id, name, path").Order("name")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.Find(&libraries).Error; err != nil {
		return internalServerError(c, err)
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
		return internalServerError(c, err)
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
	if err := validateLibraryPath(input.Path); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	library := database.Library{
		ID:          uuid.New(),
		Name:        input.Name,
		Path:        filepath.Clean(input.Path),
		OwnerUserID: &user.ID,
	}

	if err := h.db.Create(&library).Error; err != nil {
		return internalServerError(c, err)
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
		return internalServerError(c, err)
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
		if err := validateLibraryPath(*input.Path); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		library.Path = filepath.Clean(*input.Path)
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
		return internalServerError(c, err)
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
		return internalServerError(c, err)
	}

	// Delete associated tracks and library in a transaction
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&database.Track{}, "library_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Delete(&library).Error
	}); err != nil {
		return internalServerError(c, err)
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
		return internalServerError(c, err)
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
		return internalServerError(c, err)
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
		return internalServerError(c, err)
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
		return internalServerError(c, err)
	}

	return c.Status(202).JSON(fiber.Map{
		"message": "enrich job queued",
		"job_id":  job.ID,
	})
}

// TriggerPrune creates a prune job for the library
func (h *LibraryHandler) TriggerPrune(c *fiber.Ctx) error {
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
		return internalServerError(c, err)
	}

	// Create prune job
	job := database.Job{
		Type:        "prune",
		State:       "queued",
		ScopeType:   "library",
		ScopeID:     library.ID.String(),
		RequestedAt: time.Now(),
		CreatedBy:   "api",
		OwnerUserID: &user.ID,
	}

	if err := h.db.Create(&job).Error; err != nil {
		return internalServerError(c, err)
	}

	return c.Status(202).JSON(fiber.Map{
		"message": "prune job queued",
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
		return internalServerError(c, err)
	}

	var tracks []database.Track
	if err := h.db.Where("library_id = ?", libraryID).Order("artist, album, track_num").Find(&tracks).Error; err != nil {
		return internalServerError(c, err)
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

// BrowseTracks returns HTML partial with searchable, sortable, paginated track listing
func (h *LibraryHandler) BrowseTracks(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	isHtmx := c.Get("Htmx-Request") == "true"
	if !ok {
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect("/", 302)
	}

	libraryID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.SendString("<div class=\"error\">Invalid library ID.</div>")
	}

	var library database.Library
	query := h.db.Where("id = ?", libraryID)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.First(&library).Error; err != nil {
		return c.SendString("<div class=\"error\">Library not found.</div>")
	}

	// Query params
	search := c.Query("search", "")
	sortBy := c.Query("sort_by", "artist")
	sortDir := c.Query("sort_dir", "asc")
	page, _ := strconv.Atoi(c.Query("page", "1"))
	pageSize, _ := strconv.Atoi(c.Query("page_size", "50"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}

	// Whitelist sort columns to prevent SQL injection via column name
	allowedSorts := map[string]bool{
		"title": true, "artist": true, "album": true,
		"track_num": true, "format": true, "file_size": true,
		"year": true, "genre": true,
	}
	if !allowedSorts[sortBy] {
		sortBy = "artist"
	}
	if sortDir != "asc" && sortDir != "desc" {
		sortDir = "asc"
	}

	// Build query with search filter
	tx := h.db.Where("library_id = ?", libraryID)
	if search != "" {
		like := "%" + search + "%"
		tx = tx.Where("(LOWER(title) LIKE LOWER(?) OR LOWER(artist) LIKE LOWER(?) OR LOWER(album) LIKE LOWER(?) OR LOWER(genre) LIKE LOWER(?))", like, like, like, like)
	}

	// Count total matching tracks
	var total int64
	tx.Model(&database.Track{}).Count(&total)

	// Fetch paginated results
	offset := (page - 1) * pageSize
	order := sortBy + " " + sortDir + ", track_num"
	var tracks []database.Track
	if err := tx.Order(order).Offset(offset).Limit(pageSize).Find(&tracks).Error; err != nil {
		return c.SendString("<div class=\"error\">Error loading tracks.</div>")
	}

	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	// Compute next sort direction for each column
	sortToggle := map[string]string{}
	for _, col := range []string{"title", "artist", "album", "track_num", "format", "file_size", "year", "genre"} {
		if col == sortBy {
			if sortDir == "asc" {
				sortToggle[col] = "desc"
			} else {
				sortToggle[col] = "asc"
			}
		} else {
			sortToggle[col] = "asc"
		}
	}

	return c.Render("partials/library-browse", fiber.Map{
		"library":     library,
		"tracks":      tracks,
		"search":      search,
		"sort_by":     sortBy,
		"sort_dir":    sortDir,
		"sortToggle":  sortToggle,
		"page":        page,
		"page_size":   pageSize,
		"total":       int(total),
		"total_pages": totalPages,
	})
}

// TrackDetail returns HTML partial with full track metadata (for modal display)
func (h *LibraryHandler) TrackDetail(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	isHtmx := c.Get("Htmx-Request") == "true"
	if !ok {
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect("/", 302)
	}

	trackID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.SendString("<div class=\"error\">Invalid track ID.</div>")
	}

	var track database.Track
	if err := h.db.Preload("Library").First(&track, "id = ?", trackID).Error; err != nil {
		return c.SendString("<div class=\"error\">Track not found.</div>")
	}

	// Non-admin users can only see tracks in their own libraries
	if user.Role != "admin" {
		if track.Library.OwnerUserID == nil || *track.Library.OwnerUserID != user.ID {
			return c.SendString("<div class=\"error\">Track not found.</div>")
		}
	}

	c.Set("HX-Trigger", "openModal")
	return c.Render("partials/track-detail", fiber.Map{
		"track": track,
	})
}
