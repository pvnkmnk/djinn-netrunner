package api

import (
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

	artist := results[0]

	// Get quality profile
	var profileID uuid.UUID
	if payload.QualityProfileID != "" {
		profileID, _ = uuid.Parse(payload.QualityProfileID)
	} else {
		// Get default profile
		var profile database.QualityProfile
		if err := h.db.Where("is_default = ?", true).First(&profile).Error; err == nil {
			profileID = profile.ID
		}
	}

	// Create monitored artist
	monitored, err := h.atService.AddMonitoredArtist(artist.ID, profileID)
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

	return c.JSON(fiber.Map{"status": "updated"})
}
