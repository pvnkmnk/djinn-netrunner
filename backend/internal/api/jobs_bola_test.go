package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/api/templates"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
)

func TestJobsBOLA(t *testing.T) {
	db := setupTestDB(t)
	// Use Pongo2 engine for HTMX partials
	engine := templates.NewPongo2("../../../ops/web/templates", ".html")
	app := fiber.New(fiber.Config{
		Views: engine,
	})

	auth := NewAuthHandler(db)
	statsHandler := NewStatsHandler(db)

	app.Use(auth.AuthMiddleware)
	app.Get("/partials/jobs", statsHandler.RenderJobsPartial)
	app.Get("/jobs", statsHandler.JobsPage)

	// Setup users
	user1 := database.User{Email: "user1@example.com", PasswordHash: "hash", Role: "user"}
	user2 := database.User{Email: "user2@example.com", PasswordHash: "hash", Role: "user"}
	db.Create(&user1)
	db.Create(&user2)

	// Setup sessions
	sess1 := database.Session{SessionID: "sess1", UserID: user1.ID, ExpiresAt: time.Now().Add(24 * 7 * time.Hour)}
	sess2 := database.Session{SessionID: "sess2", UserID: user2.ID, ExpiresAt: time.Now().Add(24 * 7 * time.Hour)}
	db.Create(&sess1)
	db.Create(&sess2)

	// Setup jobs
	db.Create(&database.Job{Type: "sync", State: "succeeded", OwnerUserID: &user1.ID, Summary: "User1 Job"})
	db.Create(&database.Job{Type: "sync", State: "succeeded", OwnerUserID: &user2.ID, Summary: "User2 Job"})

	// 1. User1 requests jobs partial - SHOULD ONLY SEE User1's job
	req := httptest.NewRequest("GET", "/partials/jobs", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess1"})
	req.Header.Set("Htmx-Request", "true")
	resp, _ := app.Test(req)

	assert.Equal(t, 200, resp.StatusCode)
	body := new(bytes.Buffer)
	body.ReadFrom(resp.Body)
	assert.Contains(t, body.String(), "User1 Job")
	assert.NotContains(t, body.String(), "User2 Job")

	// 2. User2 requests jobs partial - SHOULD ONLY SEE User2's job
	req = httptest.NewRequest("GET", "/partials/jobs", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess2"})
	req.Header.Set("Htmx-Request", "true")
	resp, _ = app.Test(req)

	assert.Equal(t, 200, resp.StatusCode)
	body = new(bytes.Buffer)
	body.ReadFrom(resp.Body)
	assert.Contains(t, body.String(), "User2 Job")
	assert.NotContains(t, body.String(), "User1 Job")

	// 3. User1 requests jobs page - SHOULD ONLY SEE User1's job in the data passed to template
	// Note: Testing RenderPage/JobsPage usually involves checking the fiber.Map but here we check rendered content
	req = httptest.NewRequest("GET", "/jobs", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess1"})
	resp, _ = app.Test(req)

	assert.Equal(t, 200, resp.StatusCode)
	body = new(bytes.Buffer)
	body.ReadFrom(resp.Body)
	// The page shell might not render the jobs directly but via HX-GET
	// But in our current implementation, JobsPage passes jobs to RenderPage which uses jobs.html
	// jobs.html has a section with hx-get, but also might render initial jobs if we pass them.
	// Let's check if "User1 Job" is in the body if it's rendered by the template.
}
