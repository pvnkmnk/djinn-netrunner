package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
)

const previewLimit = 10

type WatchlistPreviewHandler struct {
	watchlistService *services.WatchlistService
}

func NewWatchlistPreviewHandler(watchlistService *services.WatchlistService) *WatchlistPreviewHandler {
	return &WatchlistPreviewHandler{watchlistService: watchlistService}
}

type PreviewTrack struct {
	Artist   string `json:"artist"`
	Title    string `json:"title"`
	Album    string `json:"album"`
	CoverURL string `json:"cover_art_url"`
}

func (h *WatchlistPreviewHandler) GetPreview(c *fiber.Ctx) error {
	idParam := c.Params("id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("invalid watchlist id")
	}

	watchlist, err := h.watchlistService.GetWatchlist(id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("watchlist not found")
	}

	tracks, _, err := h.watchlistService.FetchWatchlistTracks(c.Context(), watchlist)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("failed to fetch tracks: " + err.Error())
	}

	if len(tracks) > previewLimit {
		tracks = tracks[:previewLimit]
	}

	var items []PreviewTrack
	for _, t := range tracks {
		items = append(items, PreviewTrack{
			Artist:   t["artist"],
			Title:    t["title"],
			Album:    t["album"],
			CoverURL: t["cover_art_url"],
		})
	}

	return c.Render("partials/watchlist-preview", fiber.Map{
		"Tracks":      items,
		"WatchlistID": id,
		"HasMore":     len(items) >= previewLimit,
	})
}
