package api

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// DJI-562: the dashboard's live console pointed htmx at /ws/jobs - a path the
// server does not register (it registers /ws/jobs/:job_id) - through
// hx-ext="ws", an extension this app's htmx build does not contain. So the
// socket never opened, nothing on the page could name a job, and
// POST /console/attach answered a constant string that read like an
// instruction.
//
// This handler is the missing half. The picker offers the jobs the caller may
// actually stream (their own queued and running ones; an admin sees everyone's,
// matching the Jobs page), and Attach resolves the picked job, checks ownership
// before a socket URL is ever rendered, and answers with the region the console
// shows: a status line, the socket element for that job, and an out-of-band
// instruction to clear the log pane it is taking over.

// consolePickerLimit caps how many jobs the picker offers. The console streams
// one job at a time; history lives on the Jobs page.
const consolePickerLimit = 25

// consoleLogTail is how many existing log lines the socket replays when it
// opens, so attaching to a job that has already logged shows where it got to
// instead of an empty pane.
const consoleLogTail = 200

// consoleStreamableStates are the states the console can usefully follow. A
// finished job's logs are read through GET /partials/job-logs?job_id= - the path
// that has always worked - not through a socket that will never speak again.
var consoleStreamableStates = []string{"queued", "running"}

// ConsoleHandler serves the live console's job selection.
type ConsoleHandler struct {
	db *gorm.DB
}

// NewConsoleHandler returns a console handler over db.
func NewConsoleHandler(db *gorm.DB) *ConsoleHandler {
	return &ConsoleHandler{db: db}
}

// RenderJobPicker answers GET /partials/console-jobs with the option list for
// #console-job-picker.
func (h *ConsoleHandler) RenderJobPicker(c fiber.Ctx) error {
	user, ok := currentUserFromLocals(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	query := h.db.Model(&database.Job{}).
		Select("id, job_type, state").
		Where("state IN ?", consoleStreamableStates).
		Order("requested_at DESC").
		Limit(consolePickerLimit)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	var jobs []database.Job
	if err := query.Find(&jobs).Error; err != nil {
		// Not a 5xx, deliberately. htmx swaps only 2xx responses, so the
		// picker would keep its stale options and look healthy, and the app's
		// own responseError handler writes a <div> into the target - which a
		// browser then drops, because a <select> keeps only <option> children.
		// The failure is stated where the operator is looking instead, and
		// logged for the operator who is not.
		slog.Error("Error fetching jobs for the console picker", "error", err)
		return c.SendString(`<option value="">Could not load jobs - see the server log</option>`)
	}

	// The selection is echoed back because the select refreshes itself: the
	// swap replaces these options, and an option list that forgot what was
	// picked would reset the control under the operator every thirty seconds.
	selected, _ := strconv.ParseUint(c.Query("job_id"), 10, 64)

	return c.Render("partials/console-jobs", fiber.Map{
		"jobs":       jobs,
		"SelectedID": selected,
	})
}

// Attach answers POST /console/attach with the console region for the picked
// job.
//
// Every refusal renders partials/console-detached with its reason as text: the
// answer belongs in the region the operator is reading, and these are states
// they can act on by picking another job. The reason is passed as data, so the
// template's own escaping is what neutralises anything the request supplied.
func (h *ConsoleHandler) Attach(c fiber.Ctx) error {
	user, ok := currentUserFromLocals(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	raw := strings.TrimSpace(c.FormValue("job_id"))
	if raw == "" {
		raw = strings.TrimSpace(c.Query("job_id"))
	}
	if raw == "" {
		return h.detached(c, "Not attached: no job is selected. Pick one above - the console streams one job at a time.")
	}

	jobID, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || jobID == 0 {
		return h.detached(c, fmt.Sprintf("Not attached: %q is not a job number.", raw))
	}

	var job database.Job
	if err := h.db.Select("id, job_type, state, owner_user_id").First(&job, jobID).Error; err != nil {
		return h.detached(c, fmt.Sprintf("Not attached: job #%d does not exist.", jobID))
	}
	if user.Role != "admin" && (job.OwnerUserID == nil || *job.OwnerUserID != user.ID) {
		return h.detached(c, fmt.Sprintf("Not attached: you do not own job #%d.", jobID))
	}

	// The socket URL is built here, from the job that was just resolved and
	// checked, and nowhere else: a template that built one would have to be
	// handed a job id that had not been checked against its owner.
	wsURL := fmt.Sprintf("/ws/jobs/%d?tail=%d", job.ID, consoleLogTail)

	return c.Render("partials/console-attach", fiber.Map{
		"JobID":    job.ID,
		"JobType":  job.Type,
		"JobState": job.State,
		"WSURL":    wsURL,
	})
}

// detached renders the console region's refusal state.
//
// It is a 200 on purpose. htmx swaps only 2xx responses, so a 4xx here would
// leave the region as it was - still naming whatever job it was attached to
// before, or still saying "Select a job to attach" after a click that plainly
// did something - which is the silent state this slice exists to remove.
func (h *ConsoleHandler) detached(c fiber.Ctx, reason string) error {
	return c.Render("partials/console-detached", fiber.Map{"Reason": reason})
}
