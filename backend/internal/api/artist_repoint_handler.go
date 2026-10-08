package api

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/pvnkmnk/netrunner/backend/internal/database"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
)

// PATCH /api/artists/:id/repoint - move a monitored row at a different
// MusicBrainz entity.
//
// Separate from Update (PATCH /:id), which is the pause/resume toggle and must
// stay that. The picker reaches this by threading repoint_for through
// POST /api/artists/search, so the operator chooses the entity here exactly the
// way they do when adding -- and a bare POST carrying a name still takes the top
// result, deliberately, because the CLI and the MCP server both post one.
func (h *ArtistsHandler) Repoint(c *fiber.Ctx) error {
	user, hasAuth := currentUserFromLocals(c)
	if !hasAuth {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
	}

	var payload struct {
		MusicBrainzID string `json:"musicbrainz_id" form:"musicbrainz_id"`
	}
	if err := c.BodyParser(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}
	chosen := strings.TrimSpace(payload.MusicBrainzID)
	if chosen == "" {
		return c.Status(400).JSON(fiber.Map{"error": "musicbrainz_id is required"})
	}

	// Resolve the entity here, not inside the service. The row stored is the row
	// MusicBrainz holds for this ID, never the name that travelled with it -- a
	// posted name is untrusted input describing someone else's entity. Doing it
	// out here also keeps a third-party call out of the write transaction.
	found, err := h.mbService.GetArtist(chosen)
	if err != nil {
		// A failed lookup is not a successful one. Storing a blank entity here
		// would silently un-monitor a working artist and leave the card
		// claiming provenance it does not have.
		if errors.Is(err, services.ErrArtistNotFound) {
			return c.Status(404).JSON(fiber.Map{"error": "that MusicBrainz artist does not exist"})
		}
		slog.Error("Repoint could not resolve the chosen artist", "mbid", chosen, "error", err)
		return c.Status(502).JSON(fiber.Map{"error": "could not reach MusicBrainz to confirm that choice"})
	}

	if err := h.atService.RepointMonitoredArtist(
		id,
		found.ID, found.Name, found.SortName, found.Disambiguation, found.Country, found.Type,
		user.ID,
	); err != nil {
		if errors.Is(err, services.ErrArtistNotOwned) {
			// 404, not 403: whether the row exists is not this caller's business.
			// See the comment on ErrArtistNotOwned.
			return c.Status(404).JSON(fiber.Map{"error": "no such monitored artist"})
		}
		return internalServerError(c, err)
	}

	// Return the updated card. The control that triggers this swaps
	// #artist-<id> with outerHTML, so a JSON body would leave the operator
	// looking at the provenance of the entity they just replaced.
	var updated database.MonitoredArtist
	if err := h.db.Where("id = ? AND owner_user_id = ?", id, user.ID).First(&updated).Error; err != nil {
		return internalServerError(c, err)
	}
	return c.Render("partials/artist-card", fiber.Map{"Artist": updated})
}
