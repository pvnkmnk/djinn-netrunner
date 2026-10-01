package api

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// StatsData holds the stats for the dashboard
type StatsData struct {
	QueuedCount    int64
	RunningCount   int64
	SucceededCount int64
	FailedCount    int64
}

// RenderStatsPartial - MOVED to StatsHandler.RenderStatsPartial
// func RenderStatsPartial(c *fiber.Ctx) error {
// 	db, ok := c.Locals("db").(*gorm.DB)
// 	if !ok || db == nil {
// 		log.Printf("Error getting DB from context")
// 		return c.SendString("<div class=\"error\">Error loading stats.</div>")
// 	}
//
// 	var stats StatsData
//
// 	since := time.Now().Add(-24 * time.Hour)
//
// 	// Use conditional aggregation for efficient single-query stats
// 	if err := db.Model(&database.Job{}).Where("requested_at > ?", since).
// 		Select("COUNT(*) FILTER (WHERE state = 'queued') as queued_count, " +
// 			"COUNT(*) FILTER (WHERE state = 'running') as running_count, " +
// 			"COUNT(*) FILTER (WHERE state = 'succeeded') as succeeded_count, " +
// 			"COUNT(*) FILTER (WHERE state = 'failed') as failed_count").
// 		Scan(&stats).Error; err != nil {
// 		log.Printf("Error fetching stats: %v", err)
// 		return c.SendString("<div class=\"error\">Error loading stats.</div>")
// 	}
//
// 	return c.Render("partials/stats", fiber.Map{
// 		"stats": stats,
// 	})
// }

// RenderWatchlistsPartial - MOVED to WatchlistHandler.RenderWatchlistsPartial
// func RenderWatchlistsPartial(c *fiber.Ctx) error {
// 	db, ok := c.Locals("db").(*gorm.DB)
// 	if !ok || db == nil {
// 		return c.SendString("<div class=\"error\">Error loading watchlists.</div>")
// 	}
//
// 	var watchlists []database.Watchlist
// 	if err := db.Order("name").Find(&watchlists).Error; err != nil {
// 		log.Printf("Error fetching watchlists: %v", err)
// 		return c.SendString("<div class=\"error\">Error loading watchlists.</div>")
// 	}
//
// 	return c.Render("partials/watchlists", fiber.Map{
// 		"watchlists": watchlists,
// 	})
// }

// RenderJobLogsPartial returns job log entries for a given job.
func (h *StatsHandler) RenderJobLogsPartial(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	jobID := c.QueryInt("job_id", 0)
	if jobID == 0 {
		return c.SendString("<p class=\"text-secondary\">Select a job to view its logs.</p>")
	}

	// Verify job ownership
	var job database.Job
	if err := h.db.Select("id, owner_user_id").First(&job, jobID).Error; err != nil {
		return c.SendString("<p class=\"text-secondary\">Job not found.</p>")
	}
	if user.Role != "admin" && (job.OwnerUserID == nil || *job.OwnerUserID != user.ID) {
		return c.Status(403).SendString("<p class=\"text-secondary\">Access denied.</p>")
	}

	var logs []database.JobLog
	if err := h.db.Where("job_id = ?", jobID).Order("created_at ASC").Find(&logs).Error; err != nil {
		slog.Error("Error fetching job logs", "error", err)
		return c.SendString("<div class=\"error\">Error loading logs.</div>")
	}

	return c.Render("partials/job-logs", fiber.Map{
		"logs":   logs,
		"job_id": jobID,
	})
}

// RenderJobsPartial returns jobs HTML for HTMX
func (h *StatsHandler) RenderJobsPartial(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}
	return renderJobsRegion(c, h.db, user)
}

// renderJobsRegion renders the Jobs list for one viewer, applying that viewer's
// ownership scope and the filters on the request.
//
// Every renderer of this partial goes through here. The region is not only a
// list of rows: it also carries the filter selects (which echo the requested
// filters back as `selected`) and the queue explanations. A caller that renders
// the partial with a hand-built context ships a region whose dropdowns silently
// reset and whose queued rows claim to be waiting for no stated reason, which is
// the exact failure this slice exists to remove.
func renderJobsRegion(c *fiber.Ctx, db *gorm.DB, user database.User) error {
	var jobs []database.Job
	// Bolt Optimization: Select only necessary columns to reduce memory allocation and database I/O.
	// summary carries the human-readable outcome ("Acquired 21/25 items…") and
	// live progress ("Waiting for retry: …") for acquisition jobs.
	query := db.Select("id, job_type, state, requested_at, created_by, error_detail, summary").Order("requested_at DESC").Limit(50)

	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	// Apply filters if provided
	jobType := c.Query("job_type")
	state := c.Query("state")

	if jobType != "" {
		query = query.Where("job_type = ?", jobType)
	}
	if state != "" {
		query = query.Where("state = ?", state)
	}

	if err := query.Find(&jobs).Error; err != nil {
		slog.Error("Error fetching jobs", "error", err)
		return c.SendString("<div class=\"error\">Error loading jobs.</div>")
	}

	// The queue explanation is additive. Losing it is a reporting problem, not
	// grounds for blanking the page an operator is reading to find out what is
	// stuck, so a failure here degrades to rows without a stated reason.
	statuses, err := queueStatus(db)
	if err != nil {
		slog.Error("Error computing job queue status", "error", err)
		statuses = map[uint64]JobQueueStatus{}
	}

	// The filters are part of this partial, so the response has to carry what
	// was asked for: without it the swap resets the dropdowns to their defaults
	// and the operator cannot tell what they are looking at.
	return c.Render("partials/jobs", fiber.Map{
		"jobs":          jobs,
		"QueueStatuses": statuses,
		"JobType":       jobType,
		"State":         state,
	})
}
