package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"gorm.io/gorm"
)

const previewLimit = 10

type WatchlistPreviewHandler struct {
	db               *gorm.DB
	watchlistService *services.WatchlistService
}

func NewWatchlistPreviewHandler(db *gorm.DB, watchlistService *services.WatchlistService) *WatchlistPreviewHandler {
	return &WatchlistPreviewHandler{db: db, watchlistService: watchlistService}
}

type PreviewTrack struct {
	Artist   string `json:"artist"`
	Title    string `json:"title"`
	Album    string `json:"album"`
	CoverURL string `json:"cover_art_url"`
}

func (h *WatchlistPreviewHandler) GetPreview(c *fiber.Ctx) error {
	// Bolt Optimization: AuthMiddleware already populates "user" in context
	_, ok := c.Locals("user").(database.User)
	isHtmx := c.Get("Htmx-Request") == "true"

	if !ok {
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect("/", 302)
	}

	idParam := c.Params("id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("invalid watchlist id")
	}

	watchlist, err := h.watchlistService.GetWatchlist(id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("watchlist not found")
	}

	allTracks, _, err := h.watchlistService.FetchWatchlistTracks(c.Context(), watchlist)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("failed to fetch tracks: " + err.Error())
	}

	total := len(allTracks)
	tracks := allTracks
	if total > previewLimit {
		tracks = allTracks[:previewLimit]
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
		"TotalCount":  total,
		"WatchlistID": id,
		"HasMore":     len(items) >= previewLimit && total > previewLimit,
		"SourceType":  watchlist.SourceType,
		"Remaining":   total - previewLimit,
	})
}
