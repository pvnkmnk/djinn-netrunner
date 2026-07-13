package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// playlistListColumns are the columns needed for the playlist list view.
const playlistListColumns = "id, name, description, public, owner_user_id, created_at, updated_at"

type PlaylistHandler struct {
	db *gorm.DB
}

func NewPlaylistHandler(db *gorm.DB) *PlaylistHandler {
	return &PlaylistHandler{db: db}
}

// List returns all playlists for the authenticated user (admin sees all)
func (h *PlaylistHandler) List(c *fiber.Ctx) error {
	user, ok := currentUserFromLocals(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var playlists []database.Playlist
	query := h.db.Select(playlistListColumns).Order("created_at DESC")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.Find(&playlists).Error; err != nil {
		return internalServerError(c, err)
	}

	return c.JSON(playlists)
}

// Get returns a single playlist with tracks ordered by position
func (h *PlaylistHandler) Get(c *fiber.Ctx) error {
	user, ok := currentUserFromLocals(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid playlist ID"})
	}

	var playlist database.Playlist
	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.First(&playlist).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "playlist not found"})
		}
		return internalServerError(c, err)
	}

	// Load tracks ordered by position
	var playlistTracks []database.PlaylistTrack
	if err := h.db.Where("playlist_id = ?", playlist.ID).Order("position ASC").Preload("Track").Find(&playlistTracks).Error; err != nil {
		return internalServerError(c, err)
	}

	// Extract tracks from playlistTracks
	tracks := make([]database.Track, 0, len(playlistTracks))
	for _, pt := range playlistTracks {
		if pt.Track.ID != uuid.Nil {
			tracks = append(tracks, pt.Track)
		}
	}

	return c.JSON(fiber.Map{
		"playlist": playlist,
		"tracks":   tracks,
	})
}

// Create creates a new playlist
func (h *PlaylistHandler) Create(c *fiber.Ctx) error {
	user, ok := currentUserFromLocals(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Public      bool   `json:"public"`
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	if input.Name == "" {
		return c.Status(400).JSON(fiber.Map{"error": "name is required"})
	}

	playlist := database.Playlist{
		ID:          uuid.New(),
		Name:        input.Name,
		Description: input.Description,
		Public:      input.Public,
		OwnerUserID: &user.ID,
	}

	if err := h.db.Create(&playlist).Error; err != nil {
		return internalServerError(c, err)
	}

	return c.Status(201).JSON(playlist)
}

// Update updates an existing playlist
func (h *PlaylistHandler) Update(c *fiber.Ctx) error {
	user, ok := currentUserFromLocals(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid playlist ID"})
	}

	var playlist database.Playlist
	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.First(&playlist).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "playlist not found"})
		}
		return internalServerError(c, err)
	}

	// Ownership check
	if user.Role != "admin" && (playlist.OwnerUserID == nil || *playlist.OwnerUserID != user.ID) {
		return c.Status(403).JSON(fiber.Map{"error": "forbidden"})
	}

	var input struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Public      *bool   `json:"public"`
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	if input.Name != nil {
		if *input.Name == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name cannot be empty"})
		}
		playlist.Name = *input.Name
	}
	if input.Description != nil {
		playlist.Description = *input.Description
	}
	if input.Public != nil {
		playlist.Public = *input.Public
	}

	if err := h.db.Save(&playlist).Error; err != nil {
		return internalServerError(c, err)
	}

	return c.JSON(playlist)
}

// Delete deletes a playlist and its tracks
func (h *PlaylistHandler) Delete(c *fiber.Ctx) error {
	user, ok := currentUserFromLocals(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid playlist ID"})
	}

	var playlist database.Playlist
	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.First(&playlist).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "playlist not found"})
		}
		return internalServerError(c, err)
	}

	// Ownership check
	if user.Role != "admin" && (playlist.OwnerUserID == nil || *playlist.OwnerUserID != user.ID) {
		return c.Status(403).JSON(fiber.Map{"error": "forbidden"})
	}

	// Delete playlist tracks first, then the playlist
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&database.PlaylistTrack{}, "playlist_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Delete(&playlist).Error
	}); err != nil {
		return internalServerError(c, err)
	}

	return c.SendStatus(204)
}

// AddTrack adds a track to a playlist at the next position
func (h *PlaylistHandler) AddTrack(c *fiber.Ctx) error {
	user, ok := currentUserFromLocals(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	playlistID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid playlist ID"})
	}

	var playlist database.Playlist
	query := h.db.Where("id = ?", playlistID)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.First(&playlist).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "playlist not found"})
		}
		return internalServerError(c, err)
	}

	// Ownership check
	if user.Role != "admin" && (playlist.OwnerUserID == nil || *playlist.OwnerUserID != user.ID) {
		return c.Status(403).JSON(fiber.Map{"error": "forbidden"})
	}

	var input struct {
		TrackID string `json:"track_id"`
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	trackID, err := uuid.Parse(input.TrackID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid track ID"})
	}

	// Check if track exists
	var track database.Track
	if err := h.db.First(&track, "id = ?", trackID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "track not found"})
		}
		return internalServerError(c, err)
	}

	// Get the next position
	var maxPosition int
	h.db.Model(&database.PlaylistTrack{}).Where("playlist_id = ?", playlistID).Select("COALESCE(MAX(position), -1)").Scan(&maxPosition)

	playlistTrack := database.PlaylistTrack{
		PlaylistID: playlistID,
		TrackID:    trackID,
		Position:   maxPosition + 1,
	}

	if err := h.db.Create(&playlistTrack).Error; err != nil {
		return internalServerError(c, err)
	}

	return c.Status(201).JSON(fiber.Map{"message": "track added", "position": playlistTrack.Position})
}

// RemoveTrack removes a track from a playlist
func (h *PlaylistHandler) RemoveTrack(c *fiber.Ctx) error {
	user, ok := currentUserFromLocals(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	playlistID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid playlist ID"})
	}

	trackID, err := uuid.Parse(c.Params("trackId"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid track ID"})
	}

	var playlist database.Playlist
	query := h.db.Where("id = ?", playlistID)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.First(&playlist).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "playlist not found"})
		}
		return internalServerError(c, err)
	}

	// Ownership check
	if user.Role != "admin" && (playlist.OwnerUserID == nil || *playlist.OwnerUserID != user.ID) {
		return c.Status(403).JSON(fiber.Map{"error": "forbidden"})
	}

	if err := h.db.Delete(&database.PlaylistTrack{}, "playlist_id = ? AND track_id = ?", playlistID, trackID).Error; err != nil {
		return internalServerError(c, err)
	}

	return c.SendStatus(204)
}

// Reorder updates the positions of tracks in a playlist to match the provided order
func (h *PlaylistHandler) Reorder(c *fiber.Ctx) error {
	user, ok := currentUserFromLocals(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	playlistID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid playlist ID"})
	}

	var playlist database.Playlist
	query := h.db.Where("id = ?", playlistID)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.First(&playlist).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "playlist not found"})
		}
		return internalServerError(c, err)
	}

	// Ownership check
	if user.Role != "admin" && (playlist.OwnerUserID == nil || *playlist.OwnerUserID != user.ID) {
		return c.Status(403).JSON(fiber.Map{"error": "forbidden"})
	}

	var input struct {
		TrackIDs []string `json:"track_ids"`
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	// Update positions in a transaction
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		for i, trackIDStr := range input.TrackIDs {
			trackID, err := uuid.Parse(trackIDStr)
			if err != nil {
				return err
			}
			if err := tx.Model(&database.PlaylistTrack{}).Where("playlist_id = ? AND track_id = ?", playlistID, trackID).Update("position", i).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return internalServerError(c, err)
	}

	return c.JSON(fiber.Map{"message": "tracks reordered"})
}

// PlaylistsPage renders the playlists page
func (h *PlaylistHandler) PlaylistsPage(c *fiber.Ctx) error {
	_, ok, err := requirePageUser(c)
	if !ok {
		return err
	}

	return RenderPage(c, "playlists", "pages/playlists", fiber.Map{})
}

// RenderPlaylistsPartial renders the playlists partial for HTMX
func (h *PlaylistHandler) RenderPlaylistsPartial(c *fiber.Ctx) error {
	_, ok, err := requirePartialUser(c)
	if !ok {
		return err
	}

	var playlists []database.Playlist
	query := h.db.Select(playlistListColumns).Order("created_at DESC")
	user, _ := currentUserFromLocals(c)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.Find(&playlists).Error; err != nil {
		return c.SendString("<div class=\"error\">Error loading playlists.</div>")
	}

	return c.Render("partials/playlists", fiber.Map{
		"playlists": playlists,
	})
}
