package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestStatsHandler_BOLA_Repro(t *testing.T) {
	// Initialize in-memory SQLite
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	// Auto-migrate
	err = database.Migrate(db)
	assert.NoError(t, err)

	// Create two users
	user1 := database.User{Email: "user1@example.com", Role: "user"}
	db.Create(&user1)
	user2 := database.User{Email: "user2@example.com", Role: "user"}
	db.Create(&user2)

	// Create data for user 2
	db.Create(&database.Job{Type: "sync", State: "succeeded", RequestedAt: time.Now(), OwnerUserID: &user2.ID})
	db.Create(&database.Library{Name: "User2 Lib", Path: "/tmp/u2", OwnerUserID: &user2.ID})

	handler := NewStatsHandler(db)
	app := fiber.New()

	// Mock middleware to inject user1
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user1)
		return c.Next()
	})

	app.Get("/api/stats/summary", handler.GetSummary)

	resp, err := app.Test(httptest.NewRequest("GET", "/api/stats/summary", nil))
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var stats SummaryStats
	err = json.NewDecoder(resp.Body).Decode(&stats)
	assert.NoError(t, err)

	// User 1 should NOT see User 2's jobs or libraries
	// IF BOLA IS PRESENT, these assertions will FAIL (they will be 1)
	assert.Equal(t, int64(0), stats.Jobs.Total, "User 1 should see 0 jobs")
	assert.Equal(t, int64(0), stats.Activity.Libraries, "User 1 should see 0 libraries")
}
