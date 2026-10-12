package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/pvnkmnk/netrunner/backend/internal/api/templates"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// DJI-562. The console's live socket carries a job id in its path
// (/ws/jobs/:job_id), and the dashboard has no job to name - so the two things
// that have to be true are: the picker only offers jobs the viewer may stream,
// and Attach refuses to hand back a socket URL for anything else. A refusal that
// still rendered the socket element would be worse than no refusal at all: the
// request would look denied and the stream would start anyway.

// consoleTestApp wires the console's two routes the way cmd/server does, with
// the real templates and one signed-in viewer.
func consoleTestApp(t *testing.T, db *gorm.DB, user database.User) *fiber.App {
	t.Helper()
	engine := templates.NewPongo2(filepath.Join("..", "..", "..", "ops", "web", "templates"), ".html")
	app := fiber.New(fiber.Config{Views: engine})
	h := NewConsoleHandler(db)
	app.Use(withUser(user))
	app.Get("/partials/console-jobs", h.RenderJobPicker)
	app.Post("/console/attach", h.Attach)
	return app
}

// consoleJob seeds one job and returns its id.
func consoleJob(t *testing.T, db *gorm.DB, jobType, state string, owner *uint64) uint64 {
	t.Helper()
	job := database.Job{Type: jobType, State: state, RequestedAt: time.Now(), OwnerUserID: owner}
	require.NoError(t, db.Create(&job).Error)
	return job.ID
}

// attach posts a job selection the way the Attach button does.
func attach(t *testing.T, app *fiber.App, jobID string) *http.Response {
	t.Helper()
	form := url.Values{}
	if jobID != "" {
		form.Set("job_id", jobID)
	}
	req := httptest.NewRequest("POST", "/console/attach", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode,
		"the reply has to be a 2xx or htmx will not swap it into the console at all")
	return resp
}

// The picker is the only way the page can name a job, so it must not offer one
// the viewer cannot stream - and it must not offer one the console cannot
// follow either: a finished job's logs are read through /partials/job-logs.
func TestRenderJobPicker_OffersOnlyJobsTheViewerMayStream(t *testing.T) {
	db, user, other, _ := setupPartialsTestDB(t)

	ownRunning := consoleJob(t, db, "acquire", "running", &user.ID)
	ownQueued := consoleJob(t, db, "scan", "queued", &user.ID)
	othersRunning := consoleJob(t, db, "acquire", "running", &other.ID)
	systemRunning := consoleJob(t, db, "release_monitor", "running", nil)
	ownFinished := consoleJob(t, db, "acquire", "succeeded", &user.ID)

	app := consoleTestApp(t, db, user)
	resp, err := app.Test(httptest.NewRequest("GET", "/partials/console-jobs", nil))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	page := body(t, resp)

	assert.Contains(t, page, `value="`+strconv.FormatUint(ownRunning, 10)+`"`,
		"a running job of the viewer's own is exactly what the console is for")
	assert.Contains(t, page, `value="`+strconv.FormatUint(ownQueued, 10)+`"`,
		"a queued job is streamable too - the console is where its first line will appear")
	assert.NotContains(t, page, `value="`+strconv.FormatUint(othersRunning, 10)+`"`,
		"another user's job must not be offerable to stream")
	assert.NotContains(t, page, `value="`+strconv.FormatUint(systemRunning, 10)+`"`,
		"an ownerless job belongs to nobody, and the Jobs page hides it from this viewer too")
	assert.NotContains(t, page, `value="`+strconv.FormatUint(ownFinished, 10)+`"`,
		"a finished job has nothing left to stream; its logs are read through /partials/job-logs")
}

// The select refreshes itself every thirty seconds. If the response did not
// echo the current choice, the swap would reset the control under the operator
// mid-job.
func TestRenderJobPicker_EchoesTheSelectionBack(t *testing.T) {
	db, user, _, _ := setupPartialsTestDB(t)
	jobID := consoleJob(t, db, "acquire", "running", &user.ID)

	app := consoleTestApp(t, db, user)
	resp, err := app.Test(httptest.NewRequest("GET",
		"/partials/console-jobs?job_id="+strconv.FormatUint(jobID, 10), nil))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	assert.Contains(t, body(t, resp), `value="`+strconv.FormatUint(jobID, 10)+`" selected`,
		"the refreshed option list must keep the operator's selection")
}

// The defect in one assertion: the page must be handed a socket route that
// exists, with the job in it, and built by the server - because it is the
// server that resolved the job and checked that this viewer owns it.
func TestAttach_HandsBackAJobScopedSocketRoute(t *testing.T) {
	db, user, _, _ := setupPartialsTestDB(t)
	jobID := consoleJob(t, db, "acquire", "running", &user.ID)

	page := body(t, attach(t, consoleTestApp(t, db, user), strconv.FormatUint(jobID, 10)))

	assert.Contains(t, page,
		`hx-ws="connect:/ws/jobs/`+strconv.FormatUint(jobID, 10)+`?tail=200"`,
		"the reply must name the registered socket route, job and all")
	assert.NotContains(t, page, "console-placeholder",
		"an attach that resolved a job must not also render a refusal")
	assert.Contains(t, page, "Attached to job #"+strconv.FormatUint(jobID, 10),
		"the operator has to be told what the console is now showing")
}

// The refusal that matters. A viewer who asks for somebody else's job gets the
// reason and no socket element at all.
func TestAttach_RefusesAJobTheViewerDoesNotOwn(t *testing.T) {
	db, user, other, _ := setupPartialsTestDB(t)
	jobID := consoleJob(t, db, "acquire", "running", &other.ID)

	page := body(t, attach(t, consoleTestApp(t, db, user), strconv.FormatUint(jobID, 10)))

	assert.Contains(t, page, "you do not own job #"+strconv.FormatUint(jobID, 10),
		"the refusal has to name what it refused")
	assert.NotContains(t, page, "hx-ws",
		"a refused attach must not hand back a socket element - the request would "+
			"look denied while the stream started anyway")
	assert.NotContains(t, page, "/ws/jobs/")
}

// The admin role already sees everyone's jobs on the Jobs page; the console
// must not quietly be narrower than the page it sits on.
func TestAttach_AdminMayStreamAnyJob(t *testing.T) {
	db, _, other, admin := setupPartialsTestDB(t)
	jobID := consoleJob(t, db, "acquire", "running", &other.ID)

	page := body(t, attach(t, consoleTestApp(t, db, admin), strconv.FormatUint(jobID, 10)))

	assert.Contains(t, page, `hx-ws="connect:/ws/jobs/`+strconv.FormatUint(jobID, 10),
		"an admin may stream another user's job")
}

// Every way of arriving without a usable job selection is one branch of the
// same rule: say why, and render nothing that could open a socket.
func TestAttach_RendersNoSocketForAnyUnusableSelection(t *testing.T) {
	db, user, _, _ := setupPartialsTestDB(t)
	app := consoleTestApp(t, db, user)

	tests := []struct {
		name       string
		jobID      string
		wantReason string
	}{
		{name: "nothing picked", wantReason: "no job is selected"},
		{name: "not a number", jobID: "abc", wantReason: "is not a job number"},
		{name: "zero", jobID: "0", wantReason: "is not a job number"},
		{name: "no such job", jobID: "999999", wantReason: "does not exist"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page := body(t, attach(t, app, tc.jobID))

			assert.Contains(t, page, tc.wantReason,
				"the console region is where the operator is looking, so the reason belongs there")
			assert.NotContains(t, page, "hx-ws")
			assert.NotContains(t, page, "/ws/jobs/")
			assert.NotContains(t, page, "console-entry",
				"nothing was attached, so nothing may claim to be")
		})
	}
}

// The refusal quotes what the request supplied, so it has to be escaped like
// any other rendered value.
func TestAttach_EscapesTheSelectionItQuotesBack(t *testing.T) {
	db, user, _, _ := setupPartialsTestDB(t)

	page := body(t, attach(t, consoleTestApp(t, db, user), `<script>alert(1)</script>`))

	assert.Contains(t, page, "is not a job number")
	assert.NotContains(t, page, "<script",
		"the quoted selection is the request's text and must be escaped")
}

func TestAttach_UnauthenticatedIsRefused(t *testing.T) {
	db, _, _, _ := setupPartialsTestDB(t)

	engine := templates.NewPongo2(filepath.Join("..", "..", "..", "ops", "web", "templates"), ".html")
	app := fiber.New(fiber.Config{Views: engine})
	app.Post("/console/attach", NewConsoleHandler(db).Attach)

	resp, err := app.Test(httptest.NewRequest("POST", "/console/attach", nil))
	require.NoError(t, err)
	assert.Equal(t, 401, resp.StatusCode,
		"the console is user-scoped like every other surface: no session, no reply")
}
