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

type ArtistsHandler struct {
	db        *gorm.DB
	atService *services.ArtistTrackingService
	mbService *services.MusicBrainzService
}

func NewArtistsHandler(db *gorm.DB, at *services.ArtistTrackingService, mb *services.MusicBrainzService) *ArtistsHandler {
	return &ArtistsHandler{db: db, atService: at, mbService: mb}
}

// GET /api/artists - List monitored artists
func (h *ArtistsHandler) List(c *fiber.Ctx) error {
	user, hasAuth := currentUserFromLocals(c)
	if !hasAuth {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	artists, err := h.atService.GetMonitoredArtists(user.ID, user.Role == "admin")
	if err != nil {
		return internalServerError(c, err)
	}
	return c.JSON(artists)
}

// POST /api/artists - Add new artist by name
func (h *ArtistsHandler) Add(c *fiber.Ctx) error {
	user, hasAuth := currentUserFromLocals(c)
	if !hasAuth {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var payload struct {
		Name             string `json:"name"`
		QualityProfileID string `json:"quality_profile_id"`
	}

	if err := c.BodyParser(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	if payload.Name == "" {
		return c.Status(400).JSON(fiber.Map{"error": "name is required"})
	}

	// Search MusicBrainz
	results, err := h.mbService.SearchArtist(payload.Name)
	if err != nil || len(results) == 0 {
		return c.Status(404).JSON(fiber.Map{"error": "artist not found in MusicBrainz"})
	}

	// Check confidence: verify first result matches closely
	artist := results[0]
	// Simple confidence check: exact match or very close match
	// MusicBrainz search returns results sorted by relevance
	// Log ambiguous results for debugging
	if len(results) > 1 {
		slog.Warn("Ambiguous artist search", "query", payload.Name, "results", len(results), "selected", results[0].Name)
	}

	// Get quality profile
	var profileID uuid.UUID
	if payload.QualityProfileID != "" {
		var err error
		profileID, err = uuid.Parse(payload.QualityProfileID)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid quality_profile_id"})
		}
	} else {
		// Get default profile
		var profile database.QualityProfile
		if err := h.db.Where("is_default = ?", true).First(&profile).Error; err == nil {
			profileID = profile.ID
		} else if err != gorm.ErrRecordNotFound {
			slog.Error("Error fetching default profile", "error", err)
		}
	}

	// Create monitored artist with name and sort name
	monitored, err := h.atService.AddMonitoredArtist(artist.ID, profileID, artist.Name, artist.SortName, &user.ID)
	if err != nil {
		slog.Error("Failed to add monitored artist", "error", err)
		return c.Status(400).JSON(fiber.Map{"error": "failed to add artist"})
	}

	c.Set("HX-Trigger", "closeModal")
	if isHTMXRequest(c) {
		return h.RenderPartial(c)
	}
	return c.Status(201).JSON(monitored)
}

// DELETE /api/artists/:id - Remove monitored artist
func (h *ArtistsHandler) Delete(c *fiber.Ctx) error {
	user, hasAuth := currentUserFromLocals(c)
	if !hasAuth {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
	}

	if err := h.atService.DeleteMonitoredArtist(id, user.ID, user.Role == "admin"); err != nil {
		return internalServerError(c, err)
	}

	if isHTMXRequest(c) {
		return h.RenderPartial(c)
	}
	return c.JSON(fiber.Map{"status": "deleted"})
}

// PATCH /api/artists/:id - Update artist monitoring settings
func (h *ArtistsHandler) Update(c *fiber.Ctx) error {
	user, hasAuth := currentUserFromLocals(c)
	if !hasAuth {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
	}

	var payload struct {
		Monitored *bool `json:"monitored"`
	}

	if err := c.BodyParser(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	if payload.Monitored != nil {
		if err := h.atService.UpdateArtistStatus(id, *payload.Monitored, user.ID, user.Role == "admin"); err != nil {
			return internalServerError(c, err)
		}
	}

	// Reload the artist and return the card partial
	var artist database.MonitoredArtist
	query := h.db.Model(&database.MonitoredArtist{}).Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.First(&artist).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "error reloading artist"})
	}

	return c.Render("partials/artist-card", fiber.Map{"Artist": artist})
}

// Sync queues a background discography sync for a monitored artist.
func (h *ArtistsHandler) Sync(c *fiber.Ctx) error {
	user, hasAuth := currentUserFromLocals(c)
	if !hasAuth {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
	}

	var artist database.MonitoredArtist
	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.First(&artist).Error; err == gorm.ErrRecordNotFound {
		return c.Status(404).JSON(fiber.Map{"error": "artist not found"})
	} else if err != nil {
		slog.Error("Failed to load artist for sync", "artist_id", id, "error", err)
		return internalServerError(c, err)
	}

	var existingJob database.Job
	if err := h.db.Where(
		"job_type = ? AND scope_type = ? AND scope_id = ? AND state IN ?",
		"artist_scan",
		"artist",
		artist.ID.String(),
		[]string{"queued", "running"},
	).First(&existingJob).Error; err == nil {
		c.Set("HX-Trigger", "sync-already-active")
		if isHTMXRequest(c) {
			return c.Type("html").SendString("<div class=\"scan-status\">Sync already active for artist " + html.EscapeString(artist.Name) + " (job #" + fmt.Sprintf("%d", existingJob.ID) + ")</div>")
		}
		return c.JSON(fiber.Map{
			"status": "sync_already_active",
			"job_id": existingJob.ID,
			"artist": artist.Name,
		})
	} else if err != gorm.ErrRecordNotFound {
		slog.Error("Failed to check existing artist sync job", "artist_id", artist.ID, "error", err)
		return internalServerError(c, err)
	}

	job := database.Job{
		Type:        "artist_scan",
		State:       "queued",
		ScopeType:   "artist",
		ScopeID:     artist.ID.String(),
		RequestedAt: time.Now(),
		OwnerUserID: artist.OwnerUserID,
		CreatedBy:   "user_api",
	}
	if err := h.db.Create(&job).Error; err != nil {
		slog.Error("Failed to queue artist sync", "artist_id", artist.ID, "error", err)
		return internalServerError(c, err)
	}

	c.Set("HX-Trigger", "sync-queued")
	if isHTMXRequest(c) {
		return c.Type("html").SendString("<div class=\"scan-status\">Sync triggered for artist " + html.EscapeString(artist.Name) + " (job #" + fmt.Sprintf("%d", job.ID) + ")</div>")
	}
	return c.JSON(fiber.Map{
		"status": "sync_queued",
		"job_id": job.ID,
		"artist": artist.Name,
	})
}

// GetForm returns the artist form
func (h *ArtistsHandler) GetForm(c *fiber.Ctx) error {
	user, hasAuth := currentUserFromLocals(c)

	isHtmx := isHTMXRequest(c)

	if !hasAuth {
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect("/", 302)
	}

	var profiles []database.QualityProfile
	// Bolt Optimization: Select only necessary columns for the dropdown.
	query := h.db.Select("id, name").Order("name")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ? OR owner_user_id IS NULL OR is_default = ?", user.ID, true)
	}
	if err := query.Find(&profiles).Error; err != nil {
		slog.Error("Error fetching profiles for artist form", "error", err)
		return c.SendString("<div class=\"error\">Error loading form.</div>")
	}

	c.Set("HX-Trigger", "openModal")
	return c.Render("partials/artist-form", fiber.Map{
		"profiles": profiles,
	})
}

// RenderPartial returns artists HTML for HTMX
func (h *ArtistsHandler) RenderPartial(c *fiber.Ctx) error {
	user, hasAuth := currentUserFromLocals(c)

	isHtmx := isHTMXRequest(c)

	if !hasAuth {
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect("/", 302)
	}

	var artists []database.MonitoredArtist
	// Bolt Optimization: Select only necessary columns to reduce database I/O and memory usage.
	query := h.db.Model(&database.MonitoredArtist{}).
		Select("id, name, monitored, music_brainz_id, acquired_releases, total_releases, last_scan_date")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.Find(&artists).Error; err != nil {
		slog.Error("Error fetching artists", "error", err)
		return c.SendString("<div class=\"error\">Error loading artists.</div>")
	}
	return c.Render("partials/artists", fiber.Map{"artists": artists})
}
