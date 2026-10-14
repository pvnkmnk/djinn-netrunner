package api

import (
	"log/slog"
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

type SchedulesHandler struct {
	db *gorm.DB
}

func NewSchedulesHandler(db *gorm.DB) *SchedulesHandler {
	return &SchedulesHandler{db: db}
}

// GET /api/schedules - List all schedules
func (h *SchedulesHandler) List(c fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var schedules []database.Schedule
	query := h.db.Preload("Watchlist").Preload("Library")
	if user.Role != "admin" {
		query = ownedSchedules(query, user)
	}
	if err := query.Find(&schedules).Error; err != nil {
		return internalServerError(c, err)
	}
	return c.JSON(schedules)
}

// ownedSchedules restricts a schedules query to the caller's own scopes.
//
// A schedule has no owner column of its own: it inherits one from the watchlist
// (sync) or the library (scan, enrich) it runs against. The JOIN to `watchlists`
// this replaced could only ever see one of the two, so every library schedule
// would have been invisible to the account that owns it -- and because the same
// JOIN guarded delete, update and toggle, that account could not have changed
// one either.
func ownedSchedules(query *gorm.DB, user database.User) *gorm.DB {
	if user.Role == "admin" {
		return query
	}
	return query.Where(
		"((schedules.watchlist_id IN (SELECT id FROM watchlists WHERE owner_user_id = ?)) OR "+
			"(schedules.library_id IN (SELECT id FROM libraries WHERE owner_user_id = ?)))",
		user.ID, user.ID,
	)
}

// callerMayTouchSchedule reports whether the caller may act on a schedule id.
// Admins may act on any schedule -- that is what the list already shows them --
// and everyone else inherits the scope's owner.
func callerMayTouchSchedule(db *gorm.DB, user database.User, id uint64) (bool, error) {
	if user.Role == "admin" {
		return true, nil
	}
	var count int64
	err := ownedSchedules(db.Model(&database.Schedule{}), user).
		Where("schedules.id = ?", id).Count(&count).Error
	return count == 1, err
}

// POST /api/schedules - Create new schedule
func (h *SchedulesHandler) Create(c fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var payload struct {
		JobType     string `json:"job_type" form:"job_type"`
		WatchlistID string `json:"watchlist_id" form:"watchlist_id"`
		LibraryID   string `json:"library_id" form:"library_id"`
		CronExpr    string `json:"cron_expr" form:"cron_expr"`
		Timezone    string `json:"timezone" form:"timezone"`
		Enabled     *bool  `json:"enabled" form:"enabled"`
	}

	if err := c.Bind().Body(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	// The job type names the scope this schedule runs against: a sync runs on a
	// watchlist, a scan or enrich on a library. An absent job_type is the form
	// that predates library schedules, which only ever posted a watchlist, so it
	// keeps meaning sync rather than becoming a 400 for an existing client.
	jobType := payload.JobType
	if jobType == "" {
		jobType = "sync"
	}
	target, schedulable := database.ScheduleTargetForJobType(jobType)
	if !schedulable {
		return c.Status(400).JSON(fiber.Map{"error": "unschedulable job_type"})
	}

	var watchlistID, libraryID *uuid.UUID
	if target == database.ScheduleTargetWatchlist {
		// The other scope column is rejected rather than ignored: a row that
		// carried both would be a schedule nobody could explain.
		if payload.LibraryID != "" {
			return c.Status(400).JSON(fiber.Map{"error": "a sync schedule runs against a watchlist, not a library"})
		}
		scoped, err := uuid.Parse(payload.WatchlistID)
		// A zero UUID parses, so it is refused here too: an admin stores it and
		// Postgres then rejects the foreign key with a 500 for a value the
		// operator never typed.
		if err != nil || scoped == uuid.Nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid watchlist_id"})
		}

		// ✅ SECURITY: Verify watchlist ownership
		if user.Role != "admin" {
			var count int64
			h.db.Model(&database.Watchlist{}).Where("id = ? AND owner_user_id = ?", scoped, user.ID).Count(&count)
			if count == 0 {
				return c.Status(403).JSON(fiber.Map{"error": "forbidden: you do not own this watchlist"})
			}
		}
		watchlistID = &scoped
	} else {
		if payload.WatchlistID != "" {
			return c.Status(400).JSON(fiber.Map{"error": "a " + jobType + " schedule runs against a library, not a watchlist"})
		}
		scoped, err := uuid.Parse(payload.LibraryID)
		// Same as the watchlist branch: a zero UUID parses, and the foreign key
		// would be the thing that refused it.
		if err != nil || scoped == uuid.Nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid library_id"})
		}

		// ✅ SECURITY: Verify library ownership
		if user.Role != "admin" {
			var count int64
			h.db.Model(&database.Library{}).Where("id = ? AND owner_user_id = ?", scoped, user.ID).Count(&count)
			if count == 0 {
				return c.Status(403).JSON(fiber.Map{"error": "forbidden: you do not own this library"})
			}
		}
		libraryID = &scoped
	}

	// Validate cron
	if _, err := cron.ParseStandard(payload.CronExpr); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid cron expression"})
	}

	tz := payload.Timezone
	if tz == "" {
		tz = "UTC"
	}

	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	schedule := database.Schedule{
		JobType:     jobType,
		WatchlistID: watchlistID,
		LibraryID:   libraryID,
		CronExpr:    payload.CronExpr,
		Timezone:    tz,
		Enabled:     enabled,
	}

	if err := h.db.Create(&schedule).Error; err != nil {
		return internalServerError(c, err)
	}

	c.Set("HX-Trigger", "closeModal")
	if isHTMXRequest(c) {
		return h.RenderSchedulesPartial(c)
	}
	return c.Status(201).JSON(schedule)
}

// DELETE /api/schedules/:id - Delete schedule
func (h *SchedulesHandler) Delete(c fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id format"})
	}

	// Verify ownership before deleting: a schedule inherits its owner from the
	// watchlist or library it runs against.
	allowed, err := callerMayTouchSchedule(h.db, user, id)
	if err != nil {
		return internalServerError(c, err)
	}
	if !allowed {
		return c.Status(403).JSON(fiber.Map{"error": "forbidden"})
	}

	if err := h.db.Delete(&database.Schedule{}, "id = ?", id).Error; err != nil {
		return internalServerError(c, err)
	}

	if isHTMXRequest(c) {
		return h.RenderSchedulesPartial(c)
	}
	return c.JSON(fiber.Map{"status": "deleted"})
}

// PATCH /api/schedules/:id - Update schedule
//
// The job type and its scope are deliberately not editable: re-pointing a sync
// at a library is a different schedule, not an edit, and allowing the pair to
// change would let a half-applied write leave a row naming one scope while
// queueing work for the other. Delete the schedule and create the one you want.
func (h *SchedulesHandler) Update(c fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id format"})
	}

	var payload struct {
		CronExpr *string `json:"cron_expr" form:"cron_expr"`
		Timezone *string `json:"timezone" form:"timezone"`
		Enabled  *bool   `json:"enabled" form:"enabled"`
	}

	if err := c.Bind().Body(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	// A form omits an unchecked checkbox, so an absent flag means "disabled"
	// for a form PATCH; JSON PATCH keeps nil = leave unchanged.
	if isFormPost(c) && payload.Enabled == nil {
		off := false
		payload.Enabled = &off
	}

	updates := make(map[string]interface{})
	if payload.CronExpr != nil {
		if _, err := cron.ParseStandard(*payload.CronExpr); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid cron expression"})
		}
		updates["cron_expr"] = *payload.CronExpr
	}
	if payload.Timezone != nil {
		updates["timezone"] = *payload.Timezone
	}
	if payload.Enabled != nil {
		updates["enabled"] = *payload.Enabled
	}

	// Verify ownership before updating, across both scope kinds.
	allowed, err := callerMayTouchSchedule(h.db, user, id)
	if err != nil {
		return internalServerError(c, err)
	}
	if !allowed {
		return c.Status(403).JSON(fiber.Map{"error": "forbidden"})
	}

	if err := h.db.Model(&database.Schedule{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return internalServerError(c, err)
	}

	// An edit comes from the modal: close it on success, as create does.
	c.Set("HX-Trigger", "closeModal")
	if isHTMXRequest(c) {
		return h.RenderSchedulesPartial(c)
	}
	return c.JSON(fiber.Map{"status": "updated"})
}

// PATCH /api/schedules/:id/toggle - Toggle schedule enabled/disabled
func (h *SchedulesHandler) Toggle(c fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id format"})
	}

	var sched database.Schedule
	query := h.db.Model(&database.Schedule{})
	if user.Role != "admin" {
		query = ownedSchedules(query, user)
	}

	if err := query.Where("schedules.id = ?", id).First(&sched).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "not found"})
	}

	// Flip the one column. `Save(&sched)` used to do this, and it carried
	// whatever had been preloaded back into the database with the row, so the
	// write-back could collide on a UNIQUE index that had nothing to do with the
	// toggle. Updating the column and re-reading the scope keeps the two apart.
	enabled := !sched.Enabled
	if err := h.db.Model(&database.Schedule{}).Where("id = ?", id).
		Update("enabled", enabled).Error; err != nil {
		return internalServerError(c, err)
	}

	// The response is the card, and JobLabel/TargetLabel read the scope, so the
	// card is rendered from a read that preloads it.
	var rendered database.Schedule
	if err := h.db.Preload("Watchlist").Preload("Library").
		First(&rendered, "schedules.id = ?", id).Error; err != nil {
		return internalServerError(c, err)
	}

	return c.Render("partials/schedule-card", fiber.Map{
		"schedule": rendered,
	})
}

// GetForm returns the schedule form for add/edit
func (h *SchedulesHandler) GetForm(c fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	isHtmx := isHTMXRequest(c)

	if !ok {
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect().Status(302).To("/")
	}

	id := c.Query("id")

	var sched database.Schedule
	var watchlists []database.Watchlist
	var libraries []database.Library

	// Bolt Optimization: Select only necessary columns for the dropdowns.
	wQuery := h.db.Select("id, name").Order("name")
	if user.Role != "admin" {
		wQuery = wQuery.Where("owner_user_id = ?", user.ID)
	}
	if err := wQuery.Find(&watchlists).Error; err != nil {
		slog.Error("Error fetching watchlists for schedule form", "error", err)
		return c.SendString("<div class=\"error\">Error loading form.</div>")
	}

	// The library dropdown follows the same rule as the watchlist one: a
	// non-admin may only schedule a library they own, so the options they are
	// offered are the ones the create handler would accept.
	lQuery := h.db.Select("id, name").Order("name")
	if user.Role != "admin" {
		lQuery = lQuery.Where("owner_user_id = ?", user.ID)
	}
	if err := lQuery.Find(&libraries).Error; err != nil {
		slog.Error("Error fetching libraries for schedule form", "error", err)
		return c.SendString("<div class=\"error\">Error loading form.</div>")
	}

	if id != "" {
		sQuery := h.db.Preload("Watchlist").Preload("Library")
		if user.Role != "admin" {
			sQuery = ownedSchedules(sQuery, user)
		}
		if err := sQuery.First(&sched, "schedules.id = ?", id).Error; err != nil {
			return c.SendString("<div class=\"error\">Schedule not found.</div>")
		}
	}

	// A new schedule is created enabled (Create defaults the absent field to
	// true), so the form has to render it checked - an unchecked box would
	// post nothing and contradict the result.
	enabled := sched.Enabled
	if id == "" {
		enabled = true
	}

	c.Set("HX-Trigger", "openModal")
	return c.Render("partials/schedule-form", fiber.Map{
		"ID":          sched.ID,
		"JobType":     scheduleJobTypeOrNew(sched.JobType),
		"WatchlistID": scopeIDString(sched.WatchlistID),
		"LibraryID":   scopeIDString(sched.LibraryID),
		"JobTypes":    database.SchedulableJobTypeOptions(),
		"CronExpr":    sched.CronExpr,
		"Enabled":     enabled,
		"watchlists": scheduleScopeOptions(watchlists,
			func(w database.Watchlist) uuid.UUID { return w.ID },
			func(w database.Watchlist) string { return w.Name }),
		"libraries": scheduleScopeOptions(libraries,
			func(l database.Library) uuid.UUID { return l.ID },
			func(l database.Library) string { return l.Name }),
	})
}

// scheduleScopeOption is one row of a scope dropdown, with its id already
// rendered as a string. The form compares that id against the schedule's own,
// and a method call on a map value -- which is what the template contract tests
// feed a partial -- is not something pongo2 can do; a string needs no call.
type scheduleScopeOption struct {
	ID   string
	Name string
}

func scheduleScopeOptions[T any](items []T, id func(T) uuid.UUID, name func(T) string) []scheduleScopeOption {
	options := make([]scheduleScopeOption, 0, len(items))
	for _, item := range items {
		options = append(options, scheduleScopeOption{ID: id(item).String(), Name: name(item)})
	}
	return options
}

// scopeIDString renders a nullable scope id for the form's option comparison.
// The template compares it against a rendered id, which is a string there, and
// a nil pointer would panic on the way in.
func scopeIDString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// scheduleJobTypeOrNew picks the job type a form should show: the schedule's
// own, or the first schedulable one for a new schedule, so the select always
// has a selected option.
func scheduleJobTypeOrNew(jobType string) string {
	if jobType == "" {
		return database.SchedulableJobTypes()[0]
	}
	return jobType
}

// RenderSchedulesPartial returns schedules HTML for HTMX
func (h *SchedulesHandler) RenderSchedulesPartial(c fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	isHtmx := isHTMXRequest(c)

	if !ok {
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect().Status(302).To("/")
	}

	var schedules []database.Schedule
	query := h.db.Preload("Watchlist", func(db *gorm.DB) *gorm.DB {
		return db.Select("id, name")
	}).Preload("Library", func(db *gorm.DB) *gorm.DB {
		return db.Select("id, name")
	}).Order("schedules.created_at desc")
	if user.Role != "admin" {
		query = ownedSchedules(query, user)
	}

	if err := query.Find(&schedules).Error; err != nil {
		return c.SendString("<div class=\"error\">Error loading schedules.</div>")
	}

	return c.Render("partials/schedules", fiber.Map{
		"schedules": schedules,
	})
}
