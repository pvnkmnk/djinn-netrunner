package api

import (
	"errors"
	"fmt"
	"html"
	"log/slog"
	"strings"

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
		Name             string `json:"name" form:"name"`
		MusicBrainzID    string `json:"musicbrainz_id" form:"musicbrainz_id"`
		QualityProfileID string `json:"quality_profile_id" form:"quality_profile_id"`
	}

	if err := c.BodyParser(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	if payload.Name == "" {
		return c.Status(400).JSON(fiber.Map{"error": "name is required"})
	}

	// Get quality profile
	var profileID uuid.UUID
	if payload.QualityProfileID != "" {
		var err error
		profileID, err = uuid.Parse(payload.QualityProfileID)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid quality_profile_id"})
		}
		if user.Role != "admin" {
			var count int64
			h.db.Model(&database.QualityProfile{}).
				Where("id = ? AND (owner_user_id = ? OR owner_user_id IS NULL OR is_default = ?)", profileID, user.ID, true).
				Count(&count)
			if count == 0 {
				return c.Status(403).JSON(fiber.Map{"error": "forbidden: unauthorized quality profile"})
			}
		}
	} else {
		// Get default profile. A missing default must not fall through as the
		// zero UUID: MonitoredArtist.QualityProfileID is a foreign key, so the
		// insert would fail with an opaque constraint error instead of saying
		// what to fix.
		var profile database.QualityProfile
		if err := h.db.Where("is_default = ?", true).First(&profile).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return c.Status(400).JSON(fiber.Map{"error": "no default quality profile is configured"})
			}
			slog.Error("Error fetching default profile", "error", err)
			return internalServerError(c, err)
		}
		profileID = profile.ID
	}

	// Resolve which artist was actually chosen.
	//
	// A confirmed pick arrives as a MusicBrainz ID and is re-read from
	// MusicBrainz rather than trusted from the form, so the row stored is the
	// row MusicBrainz holds for that ID. A bare name still takes the top result:
	// docs/DEPLOYMENT.md, the CLI and the MCP server all post one, and that is
	// a documented contract rather than a default we may quietly withdraw. The
	// browser never takes this path — it chooses from the candidate list first.
	var artist services.MusicBrainzArtist
	if payload.MusicBrainzID != "" {
		found, err := h.mbService.GetArtist(payload.MusicBrainzID)
		if err != nil {
			if errors.Is(err, services.ErrArtistNotFound) {
				return c.Status(404).JSON(fiber.Map{"error": "that MusicBrainz artist does not exist"})
			}
			slog.Error("Failed to resolve chosen artist", "mbid", payload.MusicBrainzID, "error", err)
			return c.Status(502).JSON(fiber.Map{"error": "could not reach MusicBrainz to confirm that choice"})
		}
		artist = *found
	} else {
		results, err := h.mbService.SearchArtist(payload.Name)
		if err != nil {
			// A search that failed is not a search that found nothing. Reporting
			// an outage as "not found" sends the operator to re-check a spelling
			// that was always right, and then gives up on a working artist.
			slog.Error("Artist search failed", "query", payload.Name, "error", err)
			return c.Status(502).JSON(fiber.Map{"error": "could not reach MusicBrainz"})
		}
		if len(results) == 0 {
			return c.Status(404).JSON(fiber.Map{"error": "artist not found in MusicBrainz"})
		}
		if len(results) > 1 {
			slog.Warn("Ambiguous artist search resolved without a choice", "query", payload.Name, "results", len(results), "selected", results[0].Name)
		}
		artist = results[0]
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

// Search returns MusicBrainz candidates for a name, and creates nothing.
//
// POST /api/artists/search
//
// The picker exists because resolving an ambiguous name silently monitored an
// artist the operator never chose: typing "Death" returned Napalm Death first
// and stored it, with a WARN in a log file as the only signal that a choice had
// been made. This handler is the other half of that fix — it does the lookup
// and hands the decision back.
//
// Every outcome answers 200 with a renderable body. htmx does not swap a 4xx,
// so an error status here renders nothing: the operator clicks Add, the modal
// stays exactly as it was, and the click appears to have done nothing at all.
func (h *ArtistsHandler) Search(c *fiber.Ctx) error {
	if _, hasAuth := currentUserFromLocals(c); !hasAuth {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	// The form posts form-encoded and the documented API posts JSON.
	// BodyParser takes both; reading FormValue alone meant a JSON caller
	// searched for the empty string and was told MusicBrainz had failed.
	var payload struct {
		Name             string `json:"name" form:"name"`
		QualityProfileID string `json:"quality_profile_id" form:"quality_profile_id"`
	}
	if err := c.BodyParser(&payload); err != nil {
		return h.renderCandidates(c, "", "", nil, errNoSearchName)
	}

	name := strings.TrimSpace(payload.Name)

	// Carried into the picker so a confirmed pick lands on the profile the
	// operator chose before they searched, not on whatever the default is by
	// the time they click.
	profileID := strings.TrimSpace(payload.QualityProfileID)

	if name == "" {
		return h.renderCandidates(c, "", profileID, nil, errNoSearchName)
	}

	candidates, err := h.mbService.SearchArtist(name)
	if err != nil {
		slog.Error("Artist candidate search failed", "query", name, "error", err)
		return h.renderCandidates(c, name, profileID, nil, err)
	}
	return h.renderCandidates(c, name, profileID, candidates, nil)
}

// errNoSearchName marks the "you sent no name" case, which is a caller error
// rather than a MusicBrainz failure. The UI reaches it by submitting the form
// empty, which is why it renders as its own state instead of a retry.
var errNoSearchName = errors.New("a name is required")

// renderCandidates renders the picker. A single result is rendered as a list of
// one, never accepted on the operator's behalf: the choice is the whole point,
// and "there was only one" is not the same as "I chose that one".
func (h *ArtistsHandler) renderCandidates(c *fiber.Ctx, name, profileID string, candidates []services.MusicBrainzArtist, searchErr error) error {
	switch {
	case errors.Is(searchErr, errNoSearchName):
		// No search ran, so this must not read as an outage.
		return c.Render("partials/artist-candidates", fiber.Map{
			"query":              name,
			"quality_profile_id": profileID,
			"noName":             true,
		})
	case searchErr != nil:
		return c.Render("partials/artist-candidates", fiber.Map{
			"query":              name,
			"quality_profile_id": profileID,
			"searchFailed":       true,
			"retryEndpoint":      "/api/artists/search",
		})
	case len(candidates) == 0:
		return c.Render("partials/artist-candidates", fiber.Map{
			"query":              name,
			"quality_profile_id": profileID,
			"noMatch":            true,
		})
	default:
		return c.Render("partials/artist-candidates", fiber.Map{
			"query":              name,
			"quality_profile_id": profileID,
			"candidates":         candidates,
		})
	}
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
		Monitored *bool `json:"monitored" form:"monitored"`
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

	// The enqueue lives in the service, not here: AddMonitoredArtist queues the
	// same scan through the same call, so an artist's first scan and an
	// operator's on-demand Sync cannot drift apart (DJI-588).
	job, alreadyActive, err := h.atService.QueueArtistScan(&artist, "user_api")
	if err != nil {
		slog.Error("Failed to queue artist sync", "artist_id", artist.ID, "error", err)
		return internalServerError(c, err)
	}

	if alreadyActive {
		c.Set("HX-Trigger", "sync-already-active")
		if isHTMXRequest(c) {
			return c.Type("html").SendString("<div class=\"scan-status\">Sync already active for artist " + html.EscapeString(artist.Name) + " (job #" + fmt.Sprintf("%d", job.ID) + ")</div>")
		}
		return c.JSON(fiber.Map{
			"status": "sync_already_active",
			"job_id": job.ID,
			"artist": artist.Name,
		})
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
