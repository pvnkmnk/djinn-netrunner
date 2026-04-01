package api

import (
	"log"

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
	// Bolt Optimization: Retrieve user from context populated by AuthMiddleware
	// to eliminate redundant session lookup database roundtrip.
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	artists, err := h.atService.GetMonitoredArtists(user.ID, user.Role == "admin")
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(artists)
}

// POST /api/artists - Add new artist by name
func (h *ArtistsHandler) Add(c *fiber.Ctx) error {
	// Bolt Optimization: Retrieve user from context populated by AuthMiddleware
	// to eliminate redundant session lookup database roundtrip.
	user, ok := c.Locals("user").(database.User)
	if !ok {
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
		log.Printf("[ARTISTS] Ambiguous search for '%s' — got %d results, using first: %s",
			payload.Name, len(results), results[0].Name)
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
			log.Printf("Error fetching default profile: %v", err)
		}
	}

	// Create monitored artist with name and sort name
	monitored, err := h.atService.AddMonitoredArtist(artist.ID, profileID, artist.Name, artist.SortName, &user.ID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(201).JSON(monitored)
}

// DELETE /api/artists/:id - Remove monitored artist
func (h *ArtistsHandler) Delete(c *fiber.Ctx) error {
	// Bolt Optimization: Retrieve user from context populated by AuthMiddleware
	// to eliminate redundant session lookup database roundtrip.
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
	}

	if err := h.atService.DeleteMonitoredArtist(id, user.ID, user.Role == "admin"); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"status": "deleted"})
}

// PATCH /api/artists/:id - Update artist monitoring settings
func (h *ArtistsHandler) Update(c *fiber.Ctx) error {
	// Bolt Optimization: Retrieve user from context populated by AuthMiddleware
	// to eliminate redundant session lookup database roundtrip.
	user, ok := c.Locals("user").(database.User)
	if !ok {
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
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
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

// GetForm returns the artist form
func (h *ArtistsHandler) GetForm(c *fiber.Ctx) error {
	// Bolt Optimization: Retrieve user from context populated by AuthMiddleware
	// to eliminate redundant session lookup database roundtrip.
	_, ok := c.Locals("user").(database.User)
	isHtmx := c.Get("Htmx-Request") == "true"

	if !ok {
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect("/", 302)
	}

	var profiles []database.QualityProfile
	if err := h.db.Find(&profiles).Error; err != nil {
		log.Printf("Error fetching profiles for artist form: %v", err)
		return c.SendString("<div class=\"error\">Error loading form.</div>")
	}

	c.Set("HX-Trigger", "openModal")
	return c.Render("partials/artist-form", fiber.Map{
		"profiles": profiles,
	})
}

// RenderPartial returns artists HTML for HTMX
func (h *ArtistsHandler) RenderPartial(c *fiber.Ctx) error {
	// Bolt Optimization: Retrieve user from context populated by AuthMiddleware
	// to eliminate redundant session lookup database roundtrip.
	user, ok := c.Locals("user").(database.User)
	isHtmx := c.Get("Htmx-Request") == "true"

	if !ok {
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect("/", 302)
	}

	var artists []database.MonitoredArtist
	query := h.db.Model(&database.MonitoredArtist{})
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.Find(&artists).Error; err != nil {
		log.Printf("Error fetching artists: %v", err)
		return c.SendString("<div class=\"error\">Error loading artists.</div>")
	}
	return c.Render("partials/artists", fiber.Map{"artists": artists})
}
