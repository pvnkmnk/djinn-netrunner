package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

type ProfileHandler struct {
	db *gorm.DB
}

func NewProfileHandler(db *gorm.DB) *ProfileHandler {
	return &ProfileHandler{db: db}
}

// List returns all quality profiles
func (h *ProfileHandler) List(c *fiber.Ctx) error {
	var profiles []database.QualityProfile
	if err := h.db.Order("name").Find(&profiles).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(profiles)
}

// Get returns a single profile by ID
func (h *ProfileHandler) Get(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid profile ID"})
	}

	var profile database.QualityProfile
	if err := h.db.First(&profile, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "profile not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(profile)
}

// Create creates a new quality profile
func (h *ProfileHandler) Create(c *fiber.Ctx) error {
	var input struct {
		Name                string `json:"name"`
		Description         string `json:"description"`
		PreferLossless      bool   `json:"prefer_lossless"`
		AllowedFormats      string `json:"allowed_formats"`
		MinBitrate          int    `json:"min_bitrate"`
		PreferBitrate       *int   `json:"prefer_bitrate"`
		PreferSceneReleases bool   `json:"prefer_scene_releases"`
		PreferWebReleases   bool   `json:"prefer_web_releases"`
		CoverArtSources     string `json:"cover_art_sources"`
		IsDefault           bool   `json:"is_default"`
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	if input.Name == "" {
		return c.Status(400).JSON(fiber.Map{"error": "name is required"})
	}

	// If setting as default, use transaction to ensure atomicity
	if input.IsDefault {
		var profile database.QualityProfile
		err := h.db.Transaction(func(tx *gorm.DB) error {
			// Clear existing defaults
			if err := tx.Model(&database.QualityProfile{}).Where("is_default = ?", true).Update("is_default", false).Error; err != nil {
				return err
			}

			profile = database.QualityProfile{
				ID:                  uuid.New(),
				Name:                input.Name,
				Description:         input.Description,
				PreferLossless:      input.PreferLossless,
				AllowedFormats:      input.AllowedFormats,
				MinBitrate:          input.MinBitrate,
				PreferBitrate:       input.PreferBitrate,
				PreferSceneReleases: input.PreferSceneReleases,
				PreferWebReleases:   input.PreferWebReleases,
				CoverArtSources:     input.CoverArtSources,
				IsDefault:           input.IsDefault,
			}

			return tx.Create(&profile).Error
		})
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(profile)
	}

	profile := database.QualityProfile{
		ID:                  uuid.New(),
		Name:                input.Name,
		Description:         input.Description,
		PreferLossless:      input.PreferLossless,
		AllowedFormats:      input.AllowedFormats,
		MinBitrate:          input.MinBitrate,
		PreferBitrate:       input.PreferBitrate,
		PreferSceneReleases: input.PreferSceneReleases,
		PreferWebReleases:   input.PreferWebReleases,
		CoverArtSources:     input.CoverArtSources,
		IsDefault:           input.IsDefault,
	}

	if err := h.db.Create(&profile).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(201).JSON(profile)
}

// Update updates an existing profile
func (h *ProfileHandler) Update(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid profile ID"})
	}

	var profile database.QualityProfile
	if err := h.db.First(&profile, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "profile not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	var input struct {
		Name                *string `json:"name"`
		Description         *string `json:"description"`
		PreferLossless      *bool   `json:"prefer_lossless"`
		AllowedFormats      *string `json:"allowed_formats"`
		MinBitrate          *int    `json:"min_bitrate"`
		PreferBitrate       *int    `json:"prefer_bitrate"`
		PreferSceneReleases *bool   `json:"prefer_scene_releases"`
		PreferWebReleases   *bool   `json:"prefer_web_releases"`
		CoverArtSources     *string `json:"cover_art_sources"`
		IsDefault           *bool   `json:"is_default"`
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	// Handle default setting with transaction
	if input.IsDefault != nil && *input.IsDefault && !profile.IsDefault {
		if err := h.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&database.QualityProfile{}).Where("is_default = ?", true).Update("is_default", false).Error; err != nil {
				return err
			}
			profile.IsDefault = true
			return tx.Save(&profile).Error
		}); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(profile)
	}

	if input.Name != nil {
		if *input.Name == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name cannot be empty"})
		}
		profile.Name = *input.Name
	}
	if input.Description != nil {
		profile.Description = *input.Description
	}
	if input.PreferLossless != nil {
		profile.PreferLossless = *input.PreferLossless
	}
	if input.AllowedFormats != nil {
		profile.AllowedFormats = *input.AllowedFormats
	}
	if input.MinBitrate != nil {
		profile.MinBitrate = *input.MinBitrate
	}
	if input.PreferBitrate != nil {
		profile.PreferBitrate = input.PreferBitrate
	}
	if input.PreferSceneReleases != nil {
		profile.PreferSceneReleases = *input.PreferSceneReleases
	}
	if input.PreferWebReleases != nil {
		profile.PreferWebReleases = *input.PreferWebReleases
	}
	if input.CoverArtSources != nil {
		profile.CoverArtSources = *input.CoverArtSources
	}
	if input.IsDefault != nil {
		profile.IsDefault = *input.IsDefault
	}

	if err := h.db.Save(&profile).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(profile)
}

// Delete deletes a profile
func (h *ProfileHandler) Delete(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid profile ID"})
	}

	var profile database.QualityProfile
	if err := h.db.First(&profile, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "profile not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	// Check if profile is in use
	var count int64
	if err := h.db.Model(&database.Watchlist{}).Where("quality_profile_id = ?", id).Count(&count).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if count > 0 {
		return c.Status(400).JSON(fiber.Map{"error": "profile is in use by watchlists"})
	}

	if err := h.db.Model(&database.MonitoredArtist{}).Where("quality_profile_id = ?", id).Count(&count).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if count > 0 {
		return c.Status(400).JSON(fiber.Map{"error": "profile is in use by monitored artists"})
	}

	if err := h.db.Delete(&profile).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.SendStatus(204)
}

// GetForm returns the profile form for add/edit
func (h *ProfileHandler) GetForm(c *fiber.Ctx) error {
	id := c.Query("id")

	var profile database.QualityProfile
	if id != "" {
		uuid, err := uuid.Parse(id)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid ID"})
		}
		if err := h.db.First(&profile, "id = ?", uuid).Error; err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "not found"})
		}
	}

	return c.Render("partials/profile-form", fiber.Map{
		"ID":                  profile.ID,
		"Name":                profile.Name,
		"Description":         profile.Description,
		"PreferLossless":      profile.PreferLossless,
		"AllowedFormats":      profile.AllowedFormats,
		"MinBitrate":          profile.MinBitrate,
		"PreferSceneReleases": profile.PreferSceneReleases,
		"PreferWebReleases":   profile.PreferWebReleases,
		"CoverArtSources":     profile.CoverArtSources,
	})
}
