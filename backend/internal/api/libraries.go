package api

import (
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// Column lists for targeted queries.
// New DB columns will NOT be automatically included — update these constants
// when the corresponding model structs change.

// trackBrowseColumns are the columns needed for the BrowseTracks list view.
// Intentionally excludes: EnrichmentProvenance, Fingerprint (large fields unused in browse).
// Path is included because it is required for media serving logic.
const trackBrowseColumns = "id, title, artist, album, track_num, disc_num, format, file_size, path, year, genre"

// libraryListColumns are the columns needed for the library list view.
const libraryListColumns = "id, name, path"

// validateLibraryPath validates that a library path is safe to use.
// It ensures the path is absolute, resolves any traversal segments via
// filepath.Clean, and verifies the resolved path exists and is a directory.
func validateLibraryPath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("library path must be absolute")
	}

	// Resolve any . or .. segments to prevent traversal attacks.
	cleanPath := filepath.Clean(path)

	info, err := os.Stat(cleanPath)
	if err != nil {
		return fmt.Errorf("library path does not exist or is inaccessible")
	}
	if !info.IsDir() {
		return fmt.Errorf("library path must be a directory")
	}

	return nil
}

type LibraryHandler struct {
	db *gorm.DB
}

func NewLibraryHandler(db *gorm.DB) *LibraryHandler {
	return &LibraryHandler{db: db}
}

// ListLibraries returns all libraries
func (h *LibraryHandler) ListLibraries(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var libraries []database.Library
	// Bolt Optimization: Select only necessary columns to reduce database I/O and memory usage.
	query := h.db.Select(libraryListColumns).Order("name")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.Find(&libraries).Error; err != nil {
		return internalServerError(c, err)
	}

	return c.JSON(libraries)
}

// GetLibrary returns a single library by ID
func (h *LibraryHandler) GetLibrary(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid library ID"})
	}

	var library database.Library
	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.First(&library).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "library not found"})
		}
		return internalServerError(c, err)
	}

	return c.JSON(library)
}

// CreateLibrary creates a new library
func (h *LibraryHandler) CreateLibrary(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var input struct {
		Name string `json:"name" form:"name"`
		Path string `json:"path" form:"path"`
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	if input.Name == "" {
		return c.Status(400).JSON(fiber.Map{"error": "name is required"})
	}
	if input.Path == "" {
		return c.Status(400).JSON(fiber.Map{"error": "path is required"})
	}
	if err := validateLibraryPath(input.Path); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	cleanPath := filepath.Clean(input.Path)

	// Library.Path carries a unique index, so a second library on the same path
	// previously failed the insert and surfaced as a bare 500 — the bring-up
	// instructions worked around it by editing owner_user_id in Postgres by hand.
	// Resolve the collision here instead: idempotent for the same owner, an
	// explicit conflict (with the existing row) for anyone else.
	existing, found, err := h.libraryAtPath(cleanPath)
	if err != nil {
		return internalServerError(c, err)
	}
	if found {
		return h.respondWithExistingLibrary(c, existing, user)
	}

	library := database.Library{
		ID:          uuid.New(),
		Name:        input.Name,
		Path:        cleanPath,
		OwnerUserID: &user.ID,
	}

	if err := h.db.Create(&library).Error; err != nil {
		// The lookup above is not atomic with this insert, so a concurrent create
		// can win the unique index. Resolve that the same way rather than
		// reporting the constraint violation as an internal error.
		if dup, dupFound, lookupErr := h.libraryAtPath(cleanPath); lookupErr == nil && dupFound {
			return h.respondWithExistingLibrary(c, dup, user)
		}
		return internalServerError(c, err)
	}

	c.Set("HX-Trigger", "closeModal")
	if isHTMXRequest(c) {
		return h.RenderLibrariesPartial(c)
	}
	return c.Status(201).JSON(library)
}

// libraryAtPath returns the library registered at a path, if any.
func (h *LibraryHandler) libraryAtPath(path string) (database.Library, bool, error) {
	var existing database.Library
	switch err := h.db.Where("path = ?", path).First(&existing).Error; {
	case err == nil:
		return existing, true, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return database.Library{}, false, nil
	default:
		return database.Library{}, false, err
	}
}

// respondWithExistingLibrary answers a create request for a path that is already
// registered. It mirrors the success path's response shape — the HTMX partial
// when the caller came from the UI, JSON otherwise — so a second submission
// behaves like the first one did, instead of closing no modal and returning a
// body the UI cannot swap.
//
// A row with no owner is a third case, not a second one. Path uniqueness is
// global while listing is owner-scoped, so such a row is invisible to the user
// who most wants it: the empty state says "No libraries configured" while the
// row that holds their path cannot be seen and cannot be created around. It is
// also claimable by anyone, which is the point — so the answer is an offer to
// adopt, not a conflict. Collapsing this into the same 409 as a foreign-owned
// row is what left the documented path uncreatable.
func (h *LibraryHandler) respondWithExistingLibrary(c *fiber.Ctx, existing database.Library, user database.User) error {
	if existing.OwnerUserID != nil && *existing.OwnerUserID == user.ID {
		c.Set("HX-Trigger", "closeModal")
		if isHTMXRequest(c) {
			return h.RenderLibrariesPartial(c)
		}
		return c.Status(200).JSON(existing)
	}

	adoptable := existing.OwnerUserID == nil
	if isHTMXRequest(c) {
		// htmx does not swap a 4xx, so a 409 here renders nothing at all and
		// the user sees the click do nothing with the modal still open. The UI
		// needs a body it will actually swap, so the offer comes back at 200.
		return h.renderAdoptionOffer(c, existing, adoptable)
	}

	message := "a library already exists at this path"
	payload := fiber.Map{
		"error":     message,
		"adoptable": adoptable,
		"existing_library": fiber.Map{
			"id":            existing.ID,
			"name":          existing.Name,
			"path":          existing.Path,
			"owner_user_id": existing.OwnerUserID,
		},
	}
	if adoptable {
		// Hand out the route only when adopting is actually possible. Naming it
		// on a foreign-owned conflict offers the caller an action that can only
		// ever refuse them.
		message = "a library already exists at this path and has no owner; adopt it to use it"
		payload["error"] = message
		payload["adopt_path"] = fmt.Sprintf("/api/libraries/%s/adopt", existing.ID)
	}
	return c.Status(409).JSON(payload)
}

// renderAdoptionOffer renders the Libraries region with a message about the row
// already sitting at this path. When the row has no owner the message carries
// the adopt control; when someone else owns it, the message says so and offers
// nothing, because there is nothing this user may do about it.
func (h *LibraryHandler) renderAdoptionOffer(c *fiber.Ctx, existing database.Library, adoptable bool) error {
	var libraries []database.Library
	query := h.db.Select(libraryListColumns).Order("name")
	if user, ok := currentUserFromLocals(c); ok && user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.Find(&libraries).Error; err != nil {
		return internalServerError(c, err)
	}

	heading := "Someone else already registered a library at this path."
	body := "It belongs to another account, so you cannot create your own here. Ask them to add you, or choose a different path."
	if adoptable {
		heading = "A library already exists at this path and has no owner."
		body = "It is not on your list, so you cannot see it, but its path is taken. Adopt it and it becomes yours."
	}

	return c.Render("partials/libraries", fiber.Map{
		"libraries":             libraries,
		"pathConflict":          existing,
		"pathConflictAdoptable": adoptable,
		"pathConflictHeading":   heading,
		"pathConflictBody":      body,
	})
}

// AdoptLibrary claims a library row that has no owner.
//
// Ownership changes who can see, scan, enrich and delete a library, so it is a
// deliberate action with an audit record rather than a side effect of a create
// attempt. The row must still be unowned when the write lands — the check and
// the update share one statement, so two people racing for the same orphan row
// cannot both win it.
func (h *LibraryHandler) AdoptLibrary(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid library ID"})
	}

	var library database.Library
	if err := h.db.Where("id = ?", id).First(&library).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(404).JSON(fiber.Map{"error": "library not found"})
		}
		return internalServerError(c, err)
	}

	if library.OwnerUserID != nil {
		if *library.OwnerUserID == user.ID {
			// Already theirs: idempotent, same reasoning as the create path.
			if isHTMXRequest(c) {
				c.Set("HX-Trigger", "closeModal")
				return h.RenderLibrariesPartial(c)
			}
			return c.Status(200).JSON(library)
		}
		if isHTMXRequest(c) {
			// The adopt control is an htmx request, and htmx does not swap a
			// 4xx. Answering with JSON here would make the click do nothing —
			// the same defect this handler's sibling path was fixed for.
			return h.renderAdoptionOffer(c, library, false)
		}
		return c.Status(409).JSON(fiber.Map{
			"error":         "that library already has an owner",
			"adoptable":     false,
			"owner_user_id": library.OwnerUserID,
		})
	}

	// The claim and its audit entry share a transaction. Rolling the claim back
	// when the trail cannot be written is deliberate: recordAdoption's own
	// comment argues the trail is what makes an ownership change recoverable,
	// so a claim without one is the one outcome that argument rules out. The
	// caller can retry safely, because the IS NULL guard leaves the row
	// claimable and the failed transaction wrote nothing.
	var lost bool
	err = h.db.Transaction(func(tx *gorm.DB) error {
		// owner_user_id IS NULL in the WHERE, so a row claimed between the
		// read above and this write matches nothing rather than being taken
		// from its owner.
		result := tx.Model(&database.Library{}).
			Where("id = ? AND owner_user_id IS NULL", id).
			Update("owner_user_id", user.ID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			lost = true
			return nil
		}
		library.OwnerUserID = &user.ID
		return recordAdoption(tx, library, user)
	})
	if err != nil {
		return internalServerError(c, err)
	}
	if lost {
		if isHTMXRequest(c) {
			// Re-read: the row now has whoever won it, and the message should
			// say so rather than naming the caller as the loser.
			var taken database.Library
			if readErr := h.db.Where("id = ?", id).First(&taken).Error; readErr == nil {
				return h.renderAdoptionOffer(c, taken, false)
			}
		}
		return c.Status(409).JSON(fiber.Map{
			"error":     "that library was adopted by someone else",
			"adoptable": false,
		})
	}

	c.Set("HX-Trigger", "closeModal")
	if isHTMXRequest(c) {
		return h.RenderLibrariesPartial(c)
	}
	return c.Status(200).JSON(library)
}

// recordAdoption writes the audit entry inside the caller's transaction.
// Losing the owner that left a library unowned is unrecoverable — the row is
// invisible to every non-admin — so the trail has to name who took it and which
// library and path. It returns the insert error rather than logging it: a
// swallowed error here would leave an ownership change with no record of it,
// which is the outcome the comment above exists to prevent.
func recordAdoption(tx *gorm.DB, library database.Library, user database.User) error {
	metadata := fmt.Sprintf(`{"path":%q,"name":%q}`, library.Path, library.Name)
	return tx.Create(&database.AuditLog{
		Action:     "library_adopted",
		ActorID:    user.ID,
		TargetType: "library",
		TargetID:   library.ID.String(),
		Metadata:   metadata,
		CreatedAt:  time.Now(),
	}).Error
}

// UpdateLibrary updates an existing library
func (h *LibraryHandler) UpdateLibrary(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid library ID"})
	}

	var library database.Library
	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.First(&library).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "library not found"})
		}
		return internalServerError(c, err)
	}

	var input struct {
		Name         *string `json:"name" form:"name"`
		Path         *string `json:"path" form:"path"`
		MaxSizeBytes *int64  `json:"max_size_bytes" form:"max_size_bytes"`
		QuotaAlertAt *int    `json:"quota_alert_at" form:"quota_alert_at"`
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	if input.Name != nil {
		if *input.Name == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name cannot be empty"})
		}
		library.Name = *input.Name
	}
	if input.Path != nil {
		if *input.Path == "" {
			return c.Status(400).JSON(fiber.Map{"error": "path cannot be empty"})
		}
		if err := validateLibraryPath(*input.Path); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		library.Path = filepath.Clean(*input.Path)
	}
	// Validate QuotaAlertAt: must be between 1 and 100
	if input.QuotaAlertAt != nil && (*input.QuotaAlertAt < 1 || *input.QuotaAlertAt > 100) {
		return c.Status(400).JSON(fiber.Map{"error": "quota_alert_at must be between 1 and 100"})
	}

	// Validate MaxSizeBytes: must be non-negative (0 means "no quota")
	if input.MaxSizeBytes != nil && *input.MaxSizeBytes < 0 {
		return c.Status(400).JSON(fiber.Map{"error": "max_size_bytes must be non-negative"})
	}

	if input.MaxSizeBytes != nil {
		library.MaxSizeBytes = input.MaxSizeBytes
	}
	if input.QuotaAlertAt != nil {
		library.QuotaAlertAt = input.QuotaAlertAt
	}

	if err := h.db.Save(&library).Error; err != nil {
		return internalServerError(c, err)
	}

	// An edit comes from the modal: close it on success, as create does.
	c.Set("HX-Trigger", "closeModal")
	if isHTMXRequest(c) {
		return h.RenderLibrariesPartial(c)
	}
	return c.JSON(library)
}

// DeleteLibrary deletes a library
func (h *LibraryHandler) DeleteLibrary(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid library ID"})
	}

	var library database.Library
	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.First(&library).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "library not found"})
		}
		return internalServerError(c, err)
	}

	// Delete associated tracks and library in a transaction
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&database.Track{}, "library_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Delete(&library).Error
	}); err != nil {
		return internalServerError(c, err)
	}

	if isHTMXRequest(c) {
		return h.RenderLibrariesPartial(c)
	}
	return c.SendStatus(204)
}

// TriggerScan creates a scan job for the library
func (h *LibraryHandler) TriggerScan(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid library ID"})
	}

	var library database.Library
	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.First(&library).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "library not found"})
		}
		return internalServerError(c, err)
	}

	// Create scan job
	job := database.Job{
		Type:        "scan",
		State:       "queued",
		ScopeType:   "library",
		ScopeID:     library.ID.String(),
		RequestedAt: time.Now(),
		CreatedBy:   "api",
		OwnerUserID: &user.ID,
	}

	if err := h.db.Create(&job).Error; err != nil {
		return internalServerError(c, err)
	}

	if isHTMXRequest(c) {
		return c.Type("html").SendString("<div class=\"scan-status\">Scan triggered for library " + html.EscapeString(library.Name) + " (job #" + fmt.Sprintf("%d", job.ID) + ")</div>")
	}
	return c.Status(202).JSON(fiber.Map{
		"message": "scan job queued",
		"job_id":  job.ID,
	})
}

// TriggerEnrich creates an enrich job for the library
func (h *LibraryHandler) TriggerEnrich(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid library ID"})
	}

	var library database.Library
	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.First(&library).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "library not found"})
		}
		return internalServerError(c, err)
	}

	// Create enrich job
	job := database.Job{
		Type:        "enrich",
		State:       "queued",
		ScopeType:   "library",
		ScopeID:     library.ID.String(),
		RequestedAt: time.Now(),
		CreatedBy:   "api",
		OwnerUserID: &user.ID,
	}

	if err := h.db.Create(&job).Error; err != nil {
		return internalServerError(c, err)
	}

	if isHTMXRequest(c) {
		return c.Type("html").SendString("<div class=\"scan-status\">Enrich triggered for library " + html.EscapeString(library.Name) + " (job #" + fmt.Sprintf("%d", job.ID) + ")</div>")
	}
	return c.Status(202).JSON(fiber.Map{
		"message": "enrich job queued",
		"job_id":  job.ID,
	})
}

// TriggerPrune creates a prune job for the library
func (h *LibraryHandler) TriggerPrune(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid library ID"})
	}

	var library database.Library
	query := h.db.Where("id = ?", id)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.First(&library).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "library not found"})
		}
		return internalServerError(c, err)
	}

	// Create prune job
	job := database.Job{
		Type:        "prune",
		State:       "queued",
		ScopeType:   "library",
		ScopeID:     library.ID.String(),
		RequestedAt: time.Now(),
		CreatedBy:   "api",
		OwnerUserID: &user.ID,
	}

	if err := h.db.Create(&job).Error; err != nil {
		return internalServerError(c, err)
	}

	if isHTMXRequest(c) {
		return c.Type("html").SendString("<div class=\"scan-status\">Prune triggered for library " + html.EscapeString(library.Name) + " (job #" + fmt.Sprintf("%d", job.ID) + ")</div>")
	}
	return c.Status(202).JSON(fiber.Map{
		"message": "prune job queued",
		"job_id":  job.ID,
	})
}

// ListTracks returns all tracks for a library
func (h *LibraryHandler) ListTracks(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	libraryID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid library ID"})
	}

	// Verify ownership of the library before listing tracks
	var library database.Library
	query := h.db.Where("id = ?", libraryID)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.First(&library).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "library not found"})
		}
		return internalServerError(c, err)
	}

	var tracks []database.Track
	if err := h.db.Where("library_id = ?", libraryID).Order("artist, album, track_num").Find(&tracks).Error; err != nil {
		return internalServerError(c, err)
	}

	return c.JSON(tracks)
}

// GetForm returns the library form for add/edit
func (h *LibraryHandler) GetForm(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	isHtmx := isHTMXRequest(c)

	if !ok {
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect("/", 302)
	}

	id := c.Query("id")

	var lib database.Library
	if id != "" {
		uuid, err := uuid.Parse(id)
		if err != nil {
			return c.SendString("<div class=\"error\">Invalid ID.</div>")
		}
		query := h.db.Where("id = ?", uuid)
		if user.Role != "admin" {
			query = query.Where("owner_user_id = ?", user.ID)
		}
		if err := query.First(&lib).Error; err != nil {
			return c.SendString("<div class=\"error\">Library not found.</div>")
		}
	}

	c.Set("HX-Trigger", "openModal")
	// Only pass ID if it's non-zero (prevent zero UUID showing "Edit" instead of "Add")
	var templateID interface{} = lib.ID.String()
	if lib.ID == uuid.Nil {
		templateID = nil
	}
	return c.Render("partials/library-form", fiber.Map{
		"ID":   templateID,
		"Name": lib.Name,
		"Path": lib.Path,
	})
}

// RenderLibrariesPartial returns libraries HTML for HTMX
func (h *LibraryHandler) RenderLibrariesPartial(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	isHtmx := isHTMXRequest(c)

	if !ok {
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect("/", 302)
	}

	var libraries []database.Library
	// Bolt Optimization: Select only necessary columns to reduce database I/O and memory usage.
	query := h.db.Select(libraryListColumns).Order("name")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.Find(&libraries).Error; err != nil {
		return c.SendString("<div class=\"error\">Error loading libraries.</div>")
	}

	return c.Render("partials/libraries", fiber.Map{
		"libraries": libraries,
	})
}

// BrowseTracks returns HTML partial with searchable, sortable, paginated track listing
func (h *LibraryHandler) BrowseTracks(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	isHtmx := isHTMXRequest(c)
	if !ok {
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect("/", 302)
	}

	libraryID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.SendString("<div class=\"error\">Invalid library ID.</div>")
	}

	var library database.Library
	query := h.db.Where("id = ?", libraryID)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}
	if err := query.First(&library).Error; err != nil {
		return c.SendString("<div class=\"error\">Library not found.</div>")
	}

	// Query params
	search := c.Query("search", "")
	sortBy := c.Query("sort_by", "artist")
	sortDir := c.Query("sort_dir", "asc")
	page, _ := strconv.Atoi(c.Query("page", "1"))
	pageSize, _ := strconv.Atoi(c.Query("page_size", "50"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}

	// Whitelist sort columns to prevent SQL injection via column name
	allowedSorts := map[string]bool{
		"title": true, "artist": true, "album": true,
		"track_num": true, "format": true, "file_size": true,
		"year": true, "genre": true,
	}
	if !allowedSorts[sortBy] {
		sortBy = "artist"
	}
	if sortDir != "asc" && sortDir != "desc" {
		sortDir = "asc"
	}

	// Build query with search filter
	tx := h.db.Where("library_id = ?", libraryID)
	if search != "" {
		like := "%" + search + "%"
		tx = tx.Where("(LOWER(title) LIKE LOWER(?) OR LOWER(artist) LIKE LOWER(?) OR LOWER(album) LIKE LOWER(?) OR LOWER(genre) LIKE LOWER(?))", like, like, like, like)
	}

	// Count total matching tracks
	var total int64
	tx.Model(&database.Track{}).Count(&total)

	// Fetch paginated results
	offset := (page - 1) * pageSize
	order := sortBy + " " + sortDir + ", track_num"
	var tracks []database.Track
	if err := tx.Select(trackBrowseColumns).
		Order(order).Offset(offset).Limit(pageSize).Find(&tracks).Error; err != nil {
		return c.SendString("<div class=\"error\">Error loading tracks.</div>")
	}

	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	// Compute next sort direction for each column
	sortToggle := map[string]string{}
	for _, col := range []string{"title", "artist", "album", "track_num", "format", "file_size", "year", "genre"} {
		if col == sortBy {
			if sortDir == "asc" {
				sortToggle[col] = "desc"
			} else {
				sortToggle[col] = "asc"
			}
		} else {
			sortToggle[col] = "asc"
		}
	}

	return c.Render("partials/library-browse", fiber.Map{
		"library":     library,
		"tracks":      tracks,
		"search":      search,
		"sort_by":     sortBy,
		"sort_dir":    sortDir,
		"sortToggle":  sortToggle,
		"page":        page,
		"page_size":   pageSize,
		"total":       int(total),
		"total_pages": totalPages,
	})
}

// TrackDetail returns HTML partial with full track metadata (for modal display)
func (h *LibraryHandler) TrackDetail(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	isHtmx := isHTMXRequest(c)
	if !ok {
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect("/", 302)
	}

	trackID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.SendString("<div class=\"error\">Invalid track ID.</div>")
	}

	var track database.Track
	if err := h.db.Preload("Library").First(&track, "id = ?", trackID).Error; err != nil {
		return c.SendString("<div class=\"error\">Track not found.</div>")
	}

	// Non-admin users can only see tracks in their own libraries
	if user.Role != "admin" {
		if track.Library.OwnerUserID == nil || *track.Library.OwnerUserID != user.ID {
			return c.SendString("<div class=\"error\">Track not found.</div>")
		}
	}

	c.Set("HX-Trigger", "openModal")
	return c.Render("partials/track-detail", fiber.Map{
		"track": track,
	})
}
