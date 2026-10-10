package api

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v3"
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
func (h *ArtistsHandler) Repoint(c fiber.Ctx) error {
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
	if err := c.Bind().Body(&payload); err != nil {
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

	// The pick is made from the candidate list, whose row swaps the whole
	// artists region (#artists-region, innerHTML) -- the same swap the Add flow
	// uses. So this answers with the same partial Add does. A lone card would
	// replace the region's innerHTML and take its .section-header Add button and
	// every other card with it.
	//
	// closeModal rides along: the picker is open over the list it just changed.
	c.Set("HX-Trigger", "closeModal")
	return h.RenderPartial(c)
}
