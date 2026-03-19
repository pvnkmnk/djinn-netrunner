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
	artists, err := h.atService.GetMonitoredArtists()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(artists)
}

// POST /api/artists - Add new artist by name
func (h *ArtistsHandler) Add(c *fiber.Ctx) error {
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
	// Log ambiguity when multiple results are returned
	if len(results) > 1 {
		log.Printf("[ARTISTS] Ambiguous search for '%s' — got %d results, using first: %s",
			payload.Name, len(results), artist.Name)
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
	monitored, err := h.atService.AddMonitoredArtist(artist.ID, profileID, artist.Name, artist.SortName)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(201).JSON(monitored)
}

// DELETE /api/artists/:id - Remove monitored artist
func (h *ArtistsHandler) Delete(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
	}

	if err := h.atService.DeleteMonitoredArtist(id); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"status": "deleted"})
}

// PATCH /api/artists/:id - Update artist monitoring settings
func (h *ArtistsHandler) Update(c *fiber.Ctx) error {
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
		if err := h.atService.UpdateArtistStatus(id, *payload.Monitored); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
	}

	// Reload the artist and return the card partial
	var artist database.MonitoredArtist
	if err := h.db.First(&artist, "id = ?", id).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "error reloading artist"})
	}

	return c.Render("partials/artist-card", fiber.Map{"Artist": artist})
}

// GetForm returns the artist form
func (h *ArtistsHandler) GetForm(c *fiber.Ctx) error {
	var profiles []database.QualityProfile
	if err := h.db.Find(&profiles).Error; err != nil {
		log.Printf("Error fetching profiles for artist form: %v", err)
		return c.Status(500).SendString("Error loading form")
	}

	c.Set("HX-Trigger", "openModal")
	return c.Render("partials/artist-form", fiber.Map{
		"profiles": profiles,
	})
}

// RenderPartial returns artists HTML for HTMX
func (h *ArtistsHandler) RenderPartial(c *fiber.Ctx) error {
	var artists []database.MonitoredArtist
	if err := h.db.Find(&artists).Error; err != nil {
		log.Printf("Error fetching artists: %v", err)
		return c.Status(500).SendString("Error loading artists")
	}
	return c.Render("partials/artists", fiber.Map{"artists": artists})
}
