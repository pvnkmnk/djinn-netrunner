package api

import (
	"errors"
	"log/slog"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/agent"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// JobHandler owns the job actions the Jobs page drives. They live here rather
// than as inline route closures so the ownership rules can be exercised: "who
// may cancel what" is the rule this slice is about, and a rule that only exists
// inside a main() closure is a rule nobody can test.
type JobHandler struct {
	db *gorm.DB
}

func NewJobHandler(db *gorm.DB) *JobHandler {
	return &JobHandler{db: db}
}

// jobIDParam parses the :id route parameter.
func jobIDParam(c *fiber.Ctx) (uint64, error) {
	return strconv.ParseUint(c.Params("id"), 10, 64)
}

// mayActOnJob reports whether this user may cancel or retry the given job.
//
// Ownership is the gate for everyone except an admin. That exception is the
// point of the slice: an operator whose queue is wedged by a job belonging to
// somebody else — or by one of the scheduler's ownerless release_monitor rows,
// which no user owns at all — otherwise has no in-product remedy, and the
// documented answer was to open a shell and write to the database by hand.
//
// A non-admin is refused on an ownerless job rather than allowed to claim it:
// ownership is the only thing tying a job to a person, and "nobody owns it" has
// to mean "no ordinary user may act on it".
func mayActOnJob(user database.User, job database.Job) bool {
	if user.Role == "admin" {
		return true
	}
	return job.OwnerUserID != nil && *job.OwnerUserID == user.ID
}

// Cancel cancels a queued or running job.
//
// Cancelling a running job is the in-product remedy for a job that is wedged —
// including one a dead worker left behind, which nothing else will ever move.
// The worker's own round-robin probe notices the new state and tears the job
// down, releasing its scope lock; when no worker owns the job at all the next
// worker start stamps it. Either way the row is terminal from the request's
// point of view, which is what the operator asked for.
func (h *JobHandler) Cancel(c *fiber.Ctx) error {
	jobID, err := jobIDParam(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid job ID"})
	}
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
	}

	var job database.Job
	if err := h.db.First(&job, jobID).Error; err != nil {
		// Only a genuinely absent job is a 404. A connection failure or a
		// timeout reported as "job not found" tells the operator the job is gone
		// when it is not, and it would otherwise go unlogged entirely.
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(404).JSON(fiber.Map{"error": "job not found"})
		}
		slog.Error("Failed to load job", "job_id", jobID, "user", user.Email, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "failed to load job"})
	}
	if !mayActOnJob(user, job) {
		return c.Status(403).JSON(fiber.Map{"error": "forbidden"})
	}

	if err := agent.CancelJob(h.db, jobID); err != nil {
		slog.Error("Failed to cancel job", "job_id", jobID, "error", err)
		return c.Status(400).JSON(fiber.Map{"error": "failed to cancel job"})
	}

	if isHTMXRequest(c) {
		// The Jobs page swaps this response into the region. Returning the bare
		// JSON the API contract wants would paste {"status":"cancelled"} over
		// the card, leaving an operator who just cleared a stuck job looking at
		// a row that still reads "running".
		return renderJobsRegion(c, h.db, user)
	}
	return c.JSON(fiber.Map{"status": "cancelled", "job_id": jobID})
}

// Retry re-queues a failed job.
func (h *JobHandler) Retry(c *fiber.Ctx) error {
	jobID, err := jobIDParam(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid job ID"})
	}
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
	}

	var job database.Job
	if err := h.db.First(&job, jobID).Error; err != nil {
		// Only a genuinely absent job is a 404. A connection failure or a
		// timeout reported as "job not found" tells the operator the job is gone
		// when it is not, and it would otherwise go unlogged entirely.
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(404).JSON(fiber.Map{"error": "job not found"})
		}
		slog.Error("Failed to load job", "job_id", jobID, "user", user.Email, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "failed to load job"})
	}
	if !mayActOnJob(user, job) {
		return c.Status(403).JSON(fiber.Map{"error": "forbidden"})
	}

	if err := agent.RetryJob(h.db, jobID); err != nil {
		slog.Error("Failed to retry job", "job_id", jobID, "error", err)
		return c.Status(400).JSON(fiber.Map{"error": "failed to retry job"})
	}

	if isHTMXRequest(c) {
		return renderJobsRegion(c, h.db, user)
	}
	return c.JSON(fiber.Map{"status": "retry_queued", "job_id": jobID})
}
