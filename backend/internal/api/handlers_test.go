package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func setupAPITestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test DB: %v", err)
	}
	database.Migrate(db)
	return db
}

func setupTestApp(t *testing.T, db *gorm.DB) *fiber.App {
	t.Helper()
	app := fiber.New()
	auth := NewAuthHandler(db)
	app.Post("/register", auth.Register)
	app.Post("/login", auth.Login)
	app.Post("/logout", auth.Logout)
	app.Get("/protected", auth.AuthMiddleware, func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})
	return app
}

func registerAndLogin(t *testing.T, app *fiber.App, email, password string) string {
	t.Helper()
	// Register
	regBody, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req := httptest.NewRequest("POST", "/register", bytes.NewBuffer(regBody))
	req.Header.Set("Content-Type", "application/json")
	app.Test(req)

	// Login
	loginBody, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req = httptest.NewRequest("POST", "/login", bytes.NewBuffer(loginBody))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	// Extract session cookie
	for _, c := range resp.Cookies() {
		if c.Name == SessionCookie {
			return c.Value
		}
	}
	return ""
}

func TestRegister_MissingFields_New(t *testing.T) {
	db := setupAPITestDB(t)
	app := setupTestApp(t, db)

	tests := []struct {
		name    string
		payload map[string]string
		want    int
	}{
		{"empty body", map[string]string{}, 400},
		{"missing email", map[string]string{"password": "test"}, 400},
		{"missing password", map[string]string{"email": "a@b.com"}, 400},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.payload)
			req := httptest.NewRequest("POST", "/register", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			resp, _ := app.Test(req)
			assert.Equal(t, tt.want, resp.StatusCode)
		})
	}
}

func TestRegister_Idempotent(t *testing.T) {
	db := setupAPITestDB(t)
	app := setupTestApp(t, db)

	body, _ := json.Marshal(map[string]string{"email": "dup@test.com", "password": "pass123"})
	req := httptest.NewRequest("POST", "/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp1, _ := app.Test(req)
	assert.Equal(t, 201, resp1.StatusCode)

	// Second registration should also return 201 (idempotent)
	req = httptest.NewRequest("POST", "/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp2, _ := app.Test(req)
	assert.Equal(t, 201, resp2.StatusCode)
}

func TestLogin_InvalidCredentials(t *testing.T) {
	db := setupAPITestDB(t)
	app := setupTestApp(t, db)

	body, _ := json.Marshal(map[string]string{"email": "nope@test.com", "password": "wrong"})
	req := httptest.NewRequest("POST", "/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestLogout_WithoutSession(t *testing.T) {
	db := setupAPITestDB(t)
	app := setupTestApp(t, db)

	req := httptest.NewRequest("POST", "/logout", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestAuthMiddleware_NoCookie_New(t *testing.T) {
	db := setupAPITestDB(t)
	app := setupTestApp(t, db)

	req := httptest.NewRequest("GET", "/protected", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestAuthMiddleware_InvalidCookie(t *testing.T) {
	db := setupAPITestDB(t)
	app := setupTestApp(t, db)

	req := httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "invalid-session-id"})
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestStatsHandler_GetJobStats(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewStatsHandler(db)
	app.Get("/api/stats/jobs", handler.GetJobStats)

	req := httptest.NewRequest("GET", "/api/stats/jobs", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestStatsHandler_GetLibraryStats(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewStatsHandler(db)
	app.Get("/api/stats/library", handler.GetLibraryStats)

	req := httptest.NewRequest("GET", "/api/stats/library", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestStatsHandler_GetActivityStats(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewStatsHandler(db)
	app.Get("/api/stats/activity", handler.GetActivityStats)

	req := httptest.NewRequest("GET", "/api/stats/activity", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestStatsHandler_GetSummary(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewStatsHandler(db)
	app.Get("/api/stats/summary", handler.GetSummary)

	req := httptest.NewRequest("GET", "/api/stats/summary", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestStatsHandler_GetJobTypeBreakdown(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewStatsHandler(db)
	app.Get("/api/stats/jobs/breakdown", handler.GetJobTypeBreakdown)

	req := httptest.NewRequest("GET", "/api/stats/jobs/breakdown", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestStatsHandler_GetJobTrends(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewStatsHandler(db)
	app.Get("/api/stats/jobs/trends", handler.GetJobTrends)

	req := httptest.NewRequest("GET", "/api/stats/jobs/trends", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)

	// With days param
	req = httptest.NewRequest("GET", "/api/stats/jobs/trends?days=30", nil)
	resp, _ = app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestWatchlistHandler_ListWatchlists_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewWatchlistHandler(db, nil)
	app.Get("/api/watchlists", handler.ListWatchlists)

	req := httptest.NewRequest("GET", "/api/watchlists", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestWatchlistHandler_CreateWatchlist_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewWatchlistHandler(db, nil)
	app.Post("/api/watchlists", handler.CreateWatchlist)

	req := httptest.NewRequest("POST", "/api/watchlists", bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestWatchlistHandler_DeleteWatchlist_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewWatchlistHandler(db, nil)
	app.Delete("/api/watchlists/:id", handler.DeleteWatchlist)

	req := httptest.NewRequest("DELETE", "/api/watchlists/"+uuid.New().String(), nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestWatchlistHandler_ToggleWatchlist_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewWatchlistHandler(db, nil)
	app.Post("/api/watchlists/:id/toggle", handler.ToggleWatchlist)

	req := httptest.NewRequest("POST", "/api/watchlists/"+uuid.New().String()+"/toggle", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestWatchlistHandler_ListProfiles_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewWatchlistHandler(db, nil)
	app.Get("/api/profiles", handler.ListProfiles)

	req := httptest.NewRequest("GET", "/api/profiles", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestWatchlistHandler_GetForm_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewWatchlistHandler(db, nil)
	app.Get("/api/watchlists/form", handler.GetForm)

	req := httptest.NewRequest("GET", "/api/watchlists/form", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 302, resp.StatusCode)
}

func TestWatchlistHandler_RenderPartial_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewWatchlistHandler(db, nil)
	app.Get("/api/watchlists/partial", handler.RenderWatchlistsPartial)

	req := httptest.NewRequest("GET", "/api/watchlists/partial", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 302, resp.StatusCode)
}

func TestArtistsHandler_List_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewArtistsHandler(db, nil, nil)
	app.Get("/api/artists", handler.List)

	req := httptest.NewRequest("GET", "/api/artists", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestArtistsHandler_Delete_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewArtistsHandler(db, nil, nil)
	app.Delete("/api/artists/:id", handler.Delete)

	req := httptest.NewRequest("DELETE", "/api/artists/"+uuid.New().String(), nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestArtistsHandler_Update_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewArtistsHandler(db, nil, nil)
	app.Patch("/api/artists/:id", handler.Update)

	body, _ := json.Marshal(map[string]bool{"monitored": false})
	req := httptest.NewRequest("PATCH", "/api/artists/"+uuid.New().String(), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestArtistsHandler_GetForm_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewArtistsHandler(db, nil, nil)
	app.Get("/api/artists/form", handler.GetForm)

	req := httptest.NewRequest("GET", "/api/artists/form", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 302, resp.StatusCode)
}

func TestArtistsHandler_RenderPartial_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewArtistsHandler(db, nil, nil)
	app.Get("/api/artists/partial", handler.RenderPartial)

	req := httptest.NewRequest("GET", "/api/artists/partial", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 302, resp.StatusCode)
}

func TestSchedulesHandler_List_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewSchedulesHandler(db)
	app.Get("/api/schedules", handler.List)

	req := httptest.NewRequest("GET", "/api/schedules", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestSchedulesHandler_Create_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewSchedulesHandler(db)
	app.Post("/api/schedules", handler.Create)

	body, _ := json.Marshal(map[string]string{"watchlist_id": uuid.New().String(), "cron_expr": "0 * * * *"})
	req := httptest.NewRequest("POST", "/api/schedules", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestSchedulesHandler_Delete_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewSchedulesHandler(db)
	app.Delete("/api/schedules/:id", handler.Delete)

	req := httptest.NewRequest("DELETE", "/api/schedules/1", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestSchedulesHandler_Update_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewSchedulesHandler(db)
	app.Patch("/api/schedules/:id", handler.Update)

	body, _ := json.Marshal(map[string]string{"cron_expr": "0 * * * *"})
	req := httptest.NewRequest("PATCH", "/api/schedules/1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestSchedulesHandler_Toggle_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewSchedulesHandler(db)
	app.Patch("/api/schedules/:id/toggle", handler.Toggle)

	req := httptest.NewRequest("PATCH", "/api/schedules/1/toggle", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestSchedulesHandler_GetForm_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewSchedulesHandler(db)
	app.Get("/api/schedules/form", handler.GetForm)

	req := httptest.NewRequest("GET", "/api/schedules/form", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 302, resp.StatusCode)
}

func TestSchedulesHandler_RenderPartial_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewSchedulesHandler(db)
	app.Get("/api/schedules/partial", handler.RenderSchedulesPartial)

	req := httptest.NewRequest("GET", "/api/schedules/partial", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 302, resp.StatusCode)
}

func TestLibraryHandler_List_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewLibraryHandler(db)
	app.Get("/libraries", handler.LibrariesPage)

	req := httptest.NewRequest("GET", "/libraries", nil)
	resp, _ := app.Test(req)
	// Should redirect to login
	assert.Equal(t, 302, resp.StatusCode)
}

func TestProfileHandler_List_Unauthenticated(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewProfileHandler(db)
	app.Get("/profiles", handler.ProfilesPage)

	req := httptest.NewRequest("GET", "/profiles", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 302, resp.StatusCode)
}
