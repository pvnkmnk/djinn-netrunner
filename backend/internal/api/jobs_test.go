package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/api/templates"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// jobsApp wires the job routes behind an authenticated viewer with the real
// templates mounted, so a response body can be asserted on and not only a
// status code.
func jobsApp(db *gorm.DB, user database.User) *fiber.App {
	engine := templates.NewPongo2(filepath.Join("..", "..", "..", "ops", "web", "templates"), ".html")
	app := fiber.New(fiber.Config{Views: engine})
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	})
	h := NewJobHandler(db)
	app.Post("/api/jobs/:id/cancel", h.Cancel)
	app.Post("/api/jobs/:id/retry", h.Retry)
	return app
}

// postJobAction drives one job action, optionally as htmx would.
func postJobAction(t *testing.T, app *fiber.App, path, query string, asHTMX bool) *http.Response {
	t.Helper()
	if query != "" {
		path += "?" + query
	}
	req := httptest.NewRequest("POST", path, nil)
	if asHTMX {
		req.Header.Set("HX-Request", "true")
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp
}

func cancelJob(t *testing.T, app *fiber.App, jobID uint64, query string, asHTMX bool) *http.Response {
	t.Helper()
	return postJobAction(t, app, "/api/jobs/"+strconv.FormatUint(jobID, 10)+"/cancel", query, asHTMX)
}

func body(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(raw)
}

// renderJobsRegion renders the Jobs partial for a viewer and returns the HTML.
func renderJobsHTML(t *testing.T, db *gorm.DB, user database.User, query string) string {
	t.Helper()
	engine := templates.NewPongo2(filepath.Join("..", "..", "..", "ops", "web", "templates"), ".html")
	app := fiber.New(fiber.Config{Views: engine})
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	})
	app.Get("/partials/jobs", NewStatsHandler(db).RenderJobsPartial)

	path := "/partials/jobs"
	if query != "" {
		path += "?" + query
	}
	resp, err := app.Test(httptest.NewRequest("GET", path, nil))
	require.NoError(t, err)
	html := body(t, resp)
	require.Equal(t, 200, resp.StatusCode, html)
	return html
}

// An operator whose queue is wedged by somebody else's job has to be able to
// clear it. The documented alternative used to be a shell and a direct UPDATE.
func TestCancel_AdminCancelsAnotherUsersJob(t *testing.T) {
	db, user, _, admin := setupPartialsTestDB(t)

	job := database.Job{Type: "acquisition", State: "queued", RequestedAt: time.Now(), OwnerUserID: &user.ID}
	require.NoError(t, db.Create(&job).Error)

	resp := cancelJob(t, jobsApp(db, admin), job.ID, "", false)
	assert.Equal(t, 200, resp.StatusCode)

	var after database.Job
	require.NoError(t, db.First(&after, job.ID).Error)
	assert.Equal(t, "cancelled", after.State)
}

// The scheduler's release_monitor jobs have no owner at all. They belong to the
// system rather than to a person, and were uncancellable by everyone — which is
// exactly backwards: a system job nobody can clear is a system job that wedges
// the queue with no remedy.
func TestCancel_AdminCancelsAnOwnerlessJob(t *testing.T) {
	db, _, _, admin := setupPartialsTestDB(t)

	job := database.Job{Type: "release_monitor", State: "queued", RequestedAt: time.Now()}
	require.NoError(t, db.Create(&job).Error)
	require.Nil(t, job.OwnerUserID, "this row has to be ownerless for the test to mean anything")

	resp := cancelJob(t, jobsApp(db, admin), job.ID, "", false)
	assert.Equal(t, 200, resp.StatusCode)

	var after database.Job
	require.NoError(t, db.First(&after, job.ID).Error)
	assert.Equal(t, "cancelled", after.State)
}

// Ownership is still the rule for everybody else: a non-admin may clear their
// own jobs and nobody else's.
func TestCancel_NonAdminCanCancelOwnJob(t *testing.T) {
	db, user, _, _ := setupPartialsTestDB(t)

	job := database.Job{Type: "sync", State: "queued", RequestedAt: time.Now(), OwnerUserID: &user.ID}
	require.NoError(t, db.Create(&job).Error)

	resp := cancelJob(t, jobsApp(db, user), job.ID, "", false)
	assert.Equal(t, 200, resp.StatusCode)

	var after database.Job
	require.NoError(t, db.First(&after, job.ID).Error)
	assert.Equal(t, "cancelled", after.State)
}

func TestCancel_NonAdminIsRefusedOnAnotherUsersJob(t *testing.T) {
	db, _, otherUser, _ := setupPartialsTestDB(t)

	job := database.Job{Type: "sync", State: "queued", RequestedAt: time.Now(), OwnerUserID: &otherUser.ID}
	require.NoError(t, db.Create(&job).Error)

	// A third user: not the owner, not an admin.
	intruder := database.User{Email: "intruder@test.com", PasswordHash: "xxx", Role: "user"}
	require.NoError(t, db.Create(&intruder).Error)

	resp := cancelJob(t, jobsApp(db, intruder), job.ID, "", false)
	assert.Equal(t, 403, resp.StatusCode)

	var after database.Job
	require.NoError(t, db.First(&after, job.ID).Error)
	assert.Equal(t, "queued", after.State, "a refused cancel must not touch the row")
}

// "Nobody owns it" must not read as "anybody may claim it". Ownership is the
// only thing tying a job to a person, so an ownerless job stays admin-only.
func TestCancel_NonAdminIsRefusedOnAnOwnerlessJob(t *testing.T) {
	db, user, _, _ := setupPartialsTestDB(t)

	job := database.Job{Type: "release_monitor", State: "queued", RequestedAt: time.Now()}
	require.NoError(t, db.Create(&job).Error)

	resp := cancelJob(t, jobsApp(db, user), job.ID, "", false)
	assert.Equal(t, 403, resp.StatusCode)
}

// Cancelling a *running* job is the remedy for a wedged one, including a job a
// worker died holding. The endpoint has to accept it, and the state the request
// wrote has to be the state that survives.
func TestCancel_AdminCancelsARunningJobLeftByADeadWorker(t *testing.T) {
	db, _, _, admin := setupPartialsTestDB(t)

	// A worker died mid-flight: running, with a heartbeat that stopped moving.
	stale := time.Now().Add(-30 * time.Minute)
	job := database.Job{
		Type: "acquisition", State: "running", RequestedAt: time.Now(),
		HeartbeatAt: &stale, StartedAt: &stale,
	}
	require.NoError(t, db.Create(&job).Error)

	resp := cancelJob(t, jobsApp(db, admin), job.ID, "", false)
	assert.Equal(t, 200, resp.StatusCode)

	var after database.Job
	require.NoError(t, db.First(&after, job.ID).Error)
	assert.Equal(t, "cancelled", after.State,
		"the row must be terminal from the request's point of view, dead worker or not")
	assert.NotEqual(t, "running", after.State,
		"nothing will move this row again unless the operator asks it to")
}

// A job that already reached a terminal state cannot be cancelled, and the
// request says so rather than reporting a success it did not perform.
func TestCancel_AlreadyFinishedJobIsRejected(t *testing.T) {
	db, _, _, admin := setupPartialsTestDB(t)

	job := database.Job{Type: "sync", State: "succeeded", RequestedAt: time.Now()}
	require.NoError(t, db.Create(&job).Error)

	resp := cancelJob(t, jobsApp(db, admin), job.ID, "", false)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestCancel_UnknownJobIsNotFound(t *testing.T) {
	db, _, _, admin := setupPartialsTestDB(t)

	resp := cancelJob(t, jobsApp(db, admin), 424242, "", false)
	assert.Equal(t, 404, resp.StatusCode)
}

// Retry carries the same ownership rule, so a non-admin cannot re-queue
// somebody else's failure either.
func TestRetry_NonAdminIsRefusedOnAnotherUsersJob(t *testing.T) {
	db, user, otherUser, _ := setupPartialsTestDB(t)

	job := database.Job{Type: "sync", State: "failed", RequestedAt: time.Now(), OwnerUserID: &otherUser.ID}
	require.NoError(t, db.Create(&job).Error)

	resp := postJobAction(t, jobsApp(db, user), "/api/jobs/"+strconv.FormatUint(job.ID, 10)+"/retry", "", false)
	assert.Equal(t, 403, resp.StatusCode)
}

func TestRetry_NonAdminCanRetryOwnJob(t *testing.T) {
	db, user, _, _ := setupPartialsTestDB(t)

	job := database.Job{Type: "sync", State: "failed", RequestedAt: time.Now(), OwnerUserID: &user.ID}
	require.NoError(t, db.Create(&job).Error)

	resp := postJobAction(t, jobsApp(db, user), "/api/jobs/"+strconv.FormatUint(job.ID, 10)+"/retry", "", false)
	assert.Equal(t, 200, resp.StatusCode)

	var after database.Job
	require.NoError(t, db.First(&after, job.ID).Error)
	assert.Equal(t, "queued", after.State)
}

// The Jobs page swaps this response into the region. Returning the bare JSON the
// API contract wants would paste {"status":"cancelled"} over the card, so an
// operator who had just cleared a stuck job would be left looking at a row that
// still reads "running".
func TestCancel_HtmxCallerGetsTheJobsRegionNotJSON(t *testing.T) {
	db, _, _, admin := setupPartialsTestDB(t)

	job := database.Job{Type: "acquisition", State: "queued", RequestedAt: time.Now()}
	require.NoError(t, db.Create(&job).Error)

	resp := cancelJob(t, jobsApp(db, admin), job.ID, "", true)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")

	html := body(t, resp)
	assert.Contains(t, html, "jobs-region")
	assert.Contains(t, html, "cancelled", "the swapped-in region has to show the job as cancelled")
	assert.NotContains(t, html, `{"status":"cancelled"`, "raw JSON in the region is the bug this replaces")
}

// The Cancel control has to exist on a running row. A running job is the one an
// operator most needs to clear — that is the wedged one — and it used to be the
// only state with no way to clear it from the page at all.
func TestJobsRegion_RunningJobOffersCancel(t *testing.T) {
	db, _, _, admin := setupPartialsTestDB(t)

	heartbeat := time.Now()
	running := database.Job{
		Type: "acquisition", State: "running", RequestedAt: time.Now(),
		HeartbeatAt: &heartbeat, StartedAt: &heartbeat,
	}
	require.NoError(t, db.Create(&running).Error)

	html := renderJobsHTML(t, db, admin, "")
	assert.Contains(t, html, "/api/jobs/"+strconv.FormatUint(running.ID, 10)+"/cancel",
		"a running job must be cancellable from the page")
}

// A queued row carries its place in the queue and the reason it has not started.
func TestJobsRegion_QueuedJobShowsPositionAndReason(t *testing.T) {
	db, _, _, admin := setupPartialsTestDB(t)

	queued := database.Job{Type: "scan", State: "queued", RequestedAt: time.Now()}
	require.NoError(t, db.Create(&queued).Error)

	html := renderJobsHTML(t, db, admin, "")
	assert.Contains(t, html, "#1 in queue")
	assert.Contains(t, html, "worker slot")
}

// A finished job must not be offered a cancel that could only 400.
func TestJobsRegion_FinishedJobHasNoCancel(t *testing.T) {
	db, _, _, admin := setupPartialsTestDB(t)

	done := database.Job{Type: "sync", State: "succeeded", RequestedAt: time.Now()}
	require.NoError(t, db.Create(&done).Error)

	html := renderJobsHTML(t, db, admin, "")
	assert.NotContains(t, html, "/api/jobs/"+strconv.FormatUint(done.ID, 10)+"/cancel")
}

// The filters are part of the region, so a response that dropped them would
// reset the dropdowns under the operator's cursor. The cancel response renders
// the region too and has to carry the same filters back.
func TestCancel_HtmxResponseKeepsTheActiveFilter(t *testing.T) {
	db, _, _, admin := setupPartialsTestDB(t)

	job := database.Job{Type: "acquisition", State: "queued", RequestedAt: time.Now()}
	require.NoError(t, db.Create(&job).Error)

	resp := cancelJob(t, jobsApp(db, admin), job.ID, "state=queued", true)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, body(t, resp), `value="queued" selected`,
		"the state filter the operator was looking at has to survive the swap")
}

// The filter values are echoed back into the cancel and retry URLs, and the
// template engine does not autoescape, so a filter carrying markup has to be
// URL-encoded on the way out. Without that, a quote in a filter breaks out of
// the hx-post attribute.
func TestJobsRegion_FilterValueIsURLEncodedInActionURLs(t *testing.T) {
	db, _, _, admin := setupPartialsTestDB(t)

	hostile := `<img src=x onerror=alert(1)>`
	job := database.Job{Type: hostile, State: "queued", RequestedAt: time.Now()}
	require.NoError(t, db.Create(&job).Error)

	html := renderJobsHTML(t, db, admin, "job_type="+url.QueryEscape(hostile))

	cancelURL := "/api/jobs/" + strconv.FormatUint(job.ID, 10) + "/cancel?job_type="
	require.Contains(t, html, cancelURL+"%3Cimg", "the filter must be percent-encoded in the action URL")
	assert.NotContains(t, html, "job_type=<img", "a raw angle bracket must never reach the attribute")
	assert.NotContains(t, html, `onerror=alert(1)"`, "the attribute must not be breakable")
}

// Encoding on the way out has to survive the round trip, or applying the filter
// would silently change it.
func TestJobsRegion_EncodedFilterStillMatchesItsOwnRow(t *testing.T) {
	db, _, _, admin := setupPartialsTestDB(t)

	tricky := `a&b=c`
	job := database.Job{Type: tricky, State: "queued", RequestedAt: time.Now()}
	require.NoError(t, db.Create(&job).Error)

	html := renderJobsHTML(t, db, admin, "job_type="+url.QueryEscape(tricky))
	assert.Contains(t, html, "job-"+strconv.FormatUint(job.ID, 10), "the row the filter names has to be the row it renders")
	assert.Contains(t, html, "job_type=a%26b%3Dc", "the ampersand and equals must be encoded, not passed through")
}

// A job that does not exist is a 404. A database that did not answer is a 500,
// and saying 404 would tell the operator their job is gone when the only thing
// that is gone is the connection.
func TestCancel_DatabaseFailureIsNotReportedAsNotFound(t *testing.T) {
	db, _, _, admin := setupPartialsTestDB(t)

	// Closing the pool is the closest a test gets to a database that refuses to
	// answer, without reaching for a fault injection hook.
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	resp := cancelJob(t, jobsApp(db, admin), 1, "", false)
	assert.Equal(t, 500, resp.StatusCode)
	assert.NotEqual(t, 404, resp.StatusCode)
}
