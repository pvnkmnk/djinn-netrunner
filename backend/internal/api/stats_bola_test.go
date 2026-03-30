package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestStatsBOLA(t *testing.T) {
	// Setup DB
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	database.Migrate(db)

	// Create Users
	user1 := database.User{Email: "user1@example.com", Role: "user"}
	db.Create(&user1)
	user2 := database.User{Email: "user2@example.com", Role: "user"}
	db.Create(&user2)

	// User 1 Data
	db.Create(&database.Library{ID: uuid.New(), Name: "Lib 1", Path: "/lib1", OwnerUserID: &user1.ID})
	db.Create(&database.Job{Type: "sync", State: "succeeded", OwnerUserID: &user1.ID, RequestedAt: time.Now()})

	// User 2 Data
	db.Create(&database.Library{ID: uuid.New(), Name: "Lib 2", Path: "/lib2", OwnerUserID: &user2.ID})
	db.Create(&database.Job{Type: "sync", State: "failed", OwnerUserID: &user2.ID, RequestedAt: time.Now()})

	handler := NewStatsHandler(db)
	app := fiber.New()

	// Mock Auth Middleware
	authMock := func(user database.User) fiber.Handler {
		return func(c *fiber.Ctx) error {
			c.Locals("user", user)
			return c.Next()
		}
	}

	app.Get("/api/stats/summary", authMock(user1), handler.GetSummary)
	app.Get("/api/stats/jobs", authMock(user1), handler.GetJobStats)

	// Test Summary for User 1
	// Before fix, User 1 will see 2 jobs and 2 libraries (from activity stats part)
	resp, _ := app.Test(httptest.NewRequest("GET", "/api/stats/summary", nil))
	assert.Equal(t, 200, resp.StatusCode)

	var summary SummaryStats
	json.NewDecoder(resp.Body).Decode(&summary)

	// FAILURE: Currently, User 1 sees all jobs and libraries
	// These assertions represent the expected behavior AFTER the fix.
	// For now, I expect them to FAIL if the vulnerability exists.
	// However, I want a test that clearly demonstrates the issue.

	t.Run("JobStats_BOLA", func(t *testing.T) {
		assert.Equal(t, int64(1), summary.Jobs.Total, "User 1 should only see their own jobs")
	})

	t.Run("ActivityStats_BOLA", func(t *testing.T) {
		assert.Equal(t, int64(1), summary.Activity.Libraries, "User 1 should only see their own libraries")
	})
}
