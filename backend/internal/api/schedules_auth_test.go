package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
)

func TestSchedulesAuthorization(t *testing.T) {
	db := setupTestDB(t)
	app := fiber.New()

	auth := NewAuthHandler(db)
	schedulesHandler := NewSchedulesHandler(db)

	app.Use(auth.AuthMiddleware)
	app.Get("/api/schedules", schedulesHandler.List)
	app.Post("/api/schedules", schedulesHandler.Create)
	app.Delete("/api/schedules/:id", schedulesHandler.Delete)
	app.Patch("/api/schedules/:id", schedulesHandler.Update)

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

	// Setup quality profile
	qp := database.QualityProfile{Name: "Test Profile for Sched"}
	db.Create(&qp)

	// Setup watchlist for user1
	wl1 := database.Watchlist{
		ID:               uuid.New(),
		Name:             "User1 Watchlist",
		SourceType:       "spotify",
		SourceURI:        "spotify:playlist:1",
		QualityProfileID: qp.ID,
		OwnerUserID:      &user1.ID,
	}
	db.Create(&wl1)

	// Setup schedule for user1's watchlist
	sched1 := database.Schedule{
		WatchlistID: wl1.ID,
		CronExpr:    "0 0 * * *",
		Timezone:    "UTC",
		Enabled:     true,
	}
	db.Create(&sched1)

	// 1. User2 tries to list schedules - should NOT see User1's schedule
	req := httptest.NewRequest("GET", "/api/schedules", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess2"})
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var schedules []database.Schedule
	json.NewDecoder(resp.Body).Decode(&schedules)
	assert.Empty(t, schedules)

	// 2. User2 tries to create a schedule for User1's watchlist - should be 403
	payload := map[string]string{
		"watchlist_id": wl1.ID.String(),
		"cron_expr":    "0 0 * * *",
	}
	body, _ := json.Marshal(payload)
	req = httptest.NewRequest("POST", "/api/schedules", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess2"})
	resp, _ = app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)

	// 3. User2 tries to delete User1's schedule - should be 403
	req = httptest.NewRequest("DELETE", "/api/schedules/"+strconv.FormatUint(sched1.ID, 10), nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess2"})
	resp, _ = app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)

	// Verify it still exists
	var checkSched database.Schedule
	err := db.First(&checkSched, sched1.ID).Error
	assert.Nil(t, err)

	// 4. User2 tries to update User1's schedule - should be 403
	updatePayload := map[string]bool{"enabled": false}
	body, _ = json.Marshal(updatePayload)
	req = httptest.NewRequest("PATCH", "/api/schedules/"+strconv.FormatUint(sched1.ID, 10), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess2"})
	resp, _ = app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)

	db.First(&checkSched, sched1.ID)
	assert.True(t, checkSched.Enabled)
}
